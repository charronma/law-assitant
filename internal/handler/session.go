package handler

import (
	"encoding/json"
	"net/http"

	"law-assistant/internal/agent"
	"law-assistant/internal/store"
)

// CreateSessionRequest represents a create session request
type CreateSessionRequest struct {
	Module string `json:"module"`
	Title  string `json:"title"`
}

// handleCreateSession creates a new chat session
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	module := store.Module(req.Module)
	if req.Module == "" {
		module = store.ModuleConsult
	}

	session := s.sessionStore.Create(module, req.Title)
	writeJSON(w, http.StatusCreated, session)
}

// handleListSessions returns all chat sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessionStore.List()

	// Return summary without full messages
	type SessionSummary struct {
		ID           string       `json:"id"`
		Module       store.Module `json:"module"`
		Title        string       `json:"title"`
		CreatedAt    string       `json:"created_at"`
		UpdatedAt    string       `json:"updated_at"`
		MessageCount int          `json:"message_count"`
	}

	summaries := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		summaries = append(summaries, SessionSummary{
			ID:           session.ID,
			Module:       session.Module,
			Title:        session.Title,
			CreatedAt:    session.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:    session.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			MessageCount: session.MessageCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": summaries,
	})
}

// handleGetSession returns a session with full message history
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	session, err := s.sessionStore.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// handleDeleteSession deletes a chat session
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	if err := s.sessionStore.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListModules returns all available modules
func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	modules := agent.GetAllModules()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"modules": modules,
	})
}
