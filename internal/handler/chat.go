package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/agent"
	"law-assistant/internal/model"
	"law-assistant/internal/store"
)

// ChatRequest represents a chat API request
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Module    string `json:"module"`
	Message   string `json:"message"`
	// Model optionally picks the model for this turn; empty means the default.
	Model    string            `json:"model,omitempty"`
	FileIDs  []string          `json:"file_ids,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// streamWriteTimeout bounds a single streaming chat response.
const streamWriteTimeout = 10 * time.Minute

// SSEEvent represents a server-sent "token" or "done" event. Failures that
// happen after streaming has started are sent as a separate `event: error`
// carrying a model.APIError.
type SSEEvent struct {
	Type      string `json:"type"` // "token", "done"
	Content   string `json:"content,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// handleChat processes chat requests with SSE streaming response
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "Message is required")
		return
	}

	modelID, err := s.models.Resolve(req.Model)
	if err != nil {
		writeAPIError(w, &model.APIError{
			Status:  http.StatusBadRequest,
			Code:    model.CodeInvalidModel,
			Message: "不支持的模型",
		})
		return
	}

	// Auto-create session if not provided
	if req.SessionID == "" {
		module := store.Module(req.Module)
		if req.Module == "" {
			module = store.ModuleConsult
		}
		if !module.Valid() {
			writeError(w, http.StatusBadRequest, "Unknown module")
			return
		}
		created, err := s.sessionStore.Create(r.Context(), userID, module, "")
		if err != nil {
			writeStoreError(w, err, "create session")
			return
		}
		req.SessionID = created.ID
	}

	// Load the session (this also proves the caller owns it) and build the
	// conversation to send. The new user message is added in memory only; it is
	// persisted below, once the model has accepted the request.
	session, err := s.sessionStore.Get(r.Context(), userID, req.SessionID)
	if err != nil {
		writeStoreError(w, err, "load session")
		return
	}
	messages := append(buildSchemaMessages(session.Messages), schema.UserMessage(req.Message))

	// Handle file content injection for contract module
	moduleType := agent.ModuleType(session.Module)
	var streamReader *schema.StreamReader[*schema.Message]

	if moduleType == agent.ModuleContract && len(req.FileIDs) > 0 {
		// Get document content
		docContent := s.getFileContents(userID, req.FileIDs)
		contractAgent := s.agentManager.GetContractAgent()
		streamReader, err = contractAgent.HandleWithDocument(r.Context(), modelID, messages, docContent)
	} else {
		// Get the appropriate agent
		agnt, agentErr := s.agentManager.GetAgent(moduleType)
		if agentErr != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Unknown module: %s", session.Module))
			return
		}
		streamReader, err = agnt.Handle(r.Context(), modelID, messages)
	}
	if err != nil {
		s.writeModelError(w, err, modelID)
		return
	}
	defer streamReader.Close()

	// Wait for the first chunk before committing to a streaming response.
	// Providers can accept the request (HTTP 200) and still fail on the first
	// SSE chunk; failing here lets the client handle every "request rejected"
	// case the same way: a plain HTTP error with a JSON body.
	msg, recvErr := streamReader.Recv()
	if recvErr != nil && !errors.Is(recvErr, io.EOF) {
		s.writeModelError(w, recvErr, modelID)
		return
	}

	// The model took the request: record the user's turn. A rejected attempt
	// (quota, rate limit, ...) leaves no trace, so retrying with another model
	// does not duplicate the message in the history.
	if _, err := s.sessionStore.AddMessage(r.Context(), userID, req.SessionID, "user", req.Message, modelID, req.FileIDs); err != nil {
		writeStoreError(w, err, "save user message")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Session-ID", req.SessionID)

	// Long generations (e.g. contract review) can outlive the server-wide
	// WriteTimeout; extend the deadline for this streaming response only.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteTimeout)); err != nil {
		log.Printf("Failed to extend write deadline: %v", err)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Stream tokens to client
	var fullContent strings.Builder
	streamFailed := false
	for {
		if recvErr != nil {
			if !errors.Is(recvErr, io.EOF) {
				// Failure after output began: same JSON as a plain HTTP error,
				// carried by an SSE "error" event.
				apiErr := model.Classify(recvErr, modelID)
				log.Printf("Stream error (model=%s): %v", modelID, recvErr)
				sendSSEError(w, flusher, apiErr)
				streamFailed = true
			}
			break
		}

		if msg != nil && msg.Content != "" {
			fullContent.WriteString(msg.Content)
			sendSSEEvent(w, flusher, SSEEvent{Type: "token", Content: msg.Content})
		}
		msg, recvErr = streamReader.Recv()
	}

	// Save assistant message (skip empty replies so a failed stream does not
	// leave blank assistant turns in the history sent to the model)
	msgID := ""
	if fullContent.Len() > 0 {
		// Detach from the request's cancellation: a client that disconnected
		// mid-stream should not lose the reply that was already generated.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
		defer cancel()
		assistantMsg, err := s.sessionStore.AddMessage(saveCtx, userID, req.SessionID, "assistant", fullContent.String(), modelID, nil)
		if err != nil {
			log.Printf("Failed to save assistant message: %v", err)
		} else {
			msgID = assistantMsg.ID
		}
	}

	// A failed stream already ended with its error event.
	if streamFailed {
		return
	}
	sendSSEEvent(w, flusher, SSEEvent{Type: "done", MessageID: msgID})
}

// writeModelError logs the raw upstream failure and sends the classified,
// client-safe version (never the raw error) as an HTTP error.
func (s *Server) writeModelError(w http.ResponseWriter, err error, modelID string) {
	log.Printf("Model error (model=%s): %v", modelID, err)
	writeAPIError(w, model.Classify(err, modelID))
}

// sendSSEError sends a failure that happened after streaming started.
func sendSSEError(w http.ResponseWriter, flusher http.Flusher, apiErr *model.APIError) {
	data, err := json.Marshal(apiErr)
	if err != nil {
		log.Printf("Failed to marshal SSE error: %v", err)
		return
	}
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
	flusher.Flush()
}

// buildSchemaMessages converts store messages to schema messages
func buildSchemaMessages(messages []store.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		var schemaMsg *schema.Message
		switch msg.Role {
		case "user":
			schemaMsg = schema.UserMessage(msg.Content)
		case "assistant":
			schemaMsg = schema.AssistantMessage(msg.Content, nil)
		default:
			continue
		}
		result = append(result, schemaMsg)
	}
	return result
}

// sendSSEEvent sends a server-sent event
func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event SSEEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal SSE event: %v", err)
		return
	}
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(data))
	flusher.Flush()
}

// getFileContents retrieves and parses content from the user's uploaded files.
// IDs that do not exist or belong to someone else are silently skipped.
func (s *Server) getFileContents(userID string, fileIDs []string) string {
	var contents []string
	for _, fileID := range fileIDs {
		file, err := s.fileStore.Get(userID, fileID)
		if err != nil {
			continue
		}

		// Use cached extracted text if available
		if file.ExtractedText != "" {
			contents = append(contents, file.ExtractedText)
			continue
		}

		// Parse document
		text, err := s.docParser.Parse(file.StoragePath)
		if err != nil {
			log.Printf("Failed to parse file %s: %v", fileID, err)
			continue
		}

		// Cache the extracted text
		s.fileStore.SetExtractedText(userID, fileID, text)
		contents = append(contents, text)
	}

	return fmt.Sprintf("文件内容如下：\n\n%s", strings.Join(contents, "\n\n---\n\n"))
}
