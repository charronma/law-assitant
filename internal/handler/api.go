package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"law-assistant/internal/agent"
	"law-assistant/internal/config"
	"law-assistant/internal/store"
	"law-assistant/internal/tool"
)

// Server holds all handler dependencies
type Server struct {
	cfg          *config.Config
	agentManager *agent.AgentManager
	sessionStore *store.SessionStore
	fileStore    *store.FileStore
	docParser    *tool.DocumentParser
}

// NewServer creates a new server with all dependencies
func NewServer(cfg *config.Config, agentMgr *agent.AgentManager, sessionStore *store.SessionStore, fileStore *store.FileStore) *Server {
	return &Server{
		cfg:          cfg,
		agentManager: agentMgr,
		sessionStore: sessionStore,
		fileStore:    fileStore,
		docParser:    tool.NewDocumentParser(),
	}
}

// SetupRoutes configures all HTTP routes
func (s *Server) SetupRoutes() http.Handler {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("GET /api/modules", s.handleListModules)

	// Apply CORS middleware
	return s.corsMiddleware(mux)
}

// corsMiddleware handles CORS headers
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.FrontendURL)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error writing JSON response: %v", err)
	}
}

// writeError writes an error response
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
