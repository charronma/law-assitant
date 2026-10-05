package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/agent"
	"law-assistant/internal/store"
)

// ChatRequest represents a chat API request
type ChatRequest struct {
	SessionID string            `json:"session_id"`
	Module    string            `json:"module"`
	Message   string            `json:"message"`
	FileIDs   []string          `json:"file_ids,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// streamWriteTimeout bounds a single streaming chat response.
const streamWriteTimeout = 10 * time.Minute

// SSEEvent represents a server-sent event
type SSEEvent struct {
	Type      string `json:"type"` // "token", "done", "error"
	Content   string `json:"content,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
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

	// Save user message
	_, err := s.sessionStore.AddMessage(r.Context(), userID, req.SessionID, "user", req.Message, req.FileIDs)
	if err != nil {
		writeStoreError(w, err, "save user message")
		return
	}

	// Get session (with history) to determine module
	session, err := s.sessionStore.Get(r.Context(), userID, req.SessionID)
	if err != nil {
		writeStoreError(w, err, "load session")
		return
	}

	// Build message history for the agent
	messages := buildSchemaMessages(session.Messages)

	// Handle file content injection for contract module
	moduleType := agent.ModuleType(session.Module)
	var streamReader *schema.StreamReader[*schema.Message]

	if moduleType == agent.ModuleContract && len(req.FileIDs) > 0 {
		// Get document content
		docContent := s.getFileContents(userID, req.FileIDs)
		contractAgent := s.agentManager.GetContractAgent()
		streamReader, err = contractAgent.HandleWithDocument(r.Context(), messages, docContent)
	} else {
		// Get the appropriate agent
		agnt, agentErr := s.agentManager.GetAgent(moduleType)
		if agentErr != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Unknown module: %s", session.Module))
			return
		}
		streamReader, err = agnt.Handle(r.Context(), messages)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to process request")
		log.Printf("Agent error: %v", err)
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
	defer streamReader.Close()

	for {
		msg, err := streamReader.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			// Send error event
			sendSSEEvent(w, flusher, SSEEvent{Type: "error", Error: err.Error()})
			log.Printf("Stream error: %v", err)
			break
		}

		content := msg.Content
		if content != "" {
			fullContent.WriteString(content)
			sendSSEEvent(w, flusher, SSEEvent{Type: "token", Content: content})
		}
	}

	// Save assistant message (skip empty replies so a failed stream does not
	// leave blank assistant turns in the history sent to the model)
	msgID := ""
	if fullContent.Len() > 0 {
		// Detach from the request's cancellation: a client that disconnected
		// mid-stream should not lose the reply that was already generated.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
		defer cancel()
		assistantMsg, err := s.sessionStore.AddMessage(saveCtx, userID, req.SessionID, "assistant", fullContent.String(), nil)
		if err != nil {
			log.Printf("Failed to save assistant message: %v", err)
		} else {
			msgID = assistantMsg.ID
		}
	}

	// Send done event
	sendSSEEvent(w, flusher, SSEEvent{Type: "done", MessageID: msgID})
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
