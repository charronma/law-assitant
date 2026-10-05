package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"law-assistant/internal/agent"
	"law-assistant/internal/auth"
	"law-assistant/internal/config"
	"law-assistant/internal/limit"
	"law-assistant/internal/model"
	"law-assistant/internal/store"
	"law-assistant/internal/tool"
)

// Server holds all handler dependencies
type Server struct {
	cfg          *config.Config
	agentManager *agent.AgentManager
	sessionStore store.SessionRepository
	fileStore    *store.FileStore
	docParser    *tool.DocumentParser
	auth         *auth.Authenticator
	models       *model.Registry
	chatRate     *limit.Rate
	uploadRate   *limit.Rate
	chatStreams  *limit.Concurrency
}

// NewServer creates a new server with all dependencies
func NewServer(cfg *config.Config, agentMgr *agent.AgentManager, sessionStore store.SessionRepository, fileStore *store.FileStore, authn *auth.Authenticator, models *model.Registry) *Server {
	return &Server{
		cfg:          cfg,
		agentManager: agentMgr,
		sessionStore: sessionStore,
		fileStore:    fileStore,
		docParser:    tool.NewDocumentParser(),
		auth:         authn,
		models:       models,
		chatRate:     limit.NewRate(cfg.ChatRatePerMinute, 0),
		uploadRate:   limit.NewRate(cfg.UploadRatePerMin, 0),
		chatStreams:  limit.NewConcurrency(cfg.MaxConcurrentChats),
	}
}

// SetupRoutes configures all HTTP routes
func (s *Server) SetupRoutes() http.Handler {
	mux := http.NewServeMux()

	// API routes (all require authentication)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("GET /api/modules", s.handleListModules)
	mux.HandleFunc("GET /api/models", s.handleListModels)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.Handle("/api/", s.auth.Middleware(mux))

	// CORS wraps everything so that preflight requests (which carry no
	// credentials) are answered before authentication.
	return s.corsMiddleware(root)
}

// requireUser returns the authenticated user's ID, or writes a 401 and
// returns false. The auth middleware already guarantees an ID on /api routes;
// this is a second line of defence for any handler wired up without it.
func requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return "", false
	}
	return userID, true
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

// writeStoreError maps a persistence error to an HTTP response. Unexpected
// errors are logged with detail but never echoed to the client.
func writeStoreError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Session not found")
	case errors.Is(err, store.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "Unauthorized")
	case errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "Invalid request")
	default:
		log.Printf("%s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
	}
}

// handleListModels returns the models users may choose from and the default.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"models":  s.models.Models(),
		"default": s.models.Default(),
	})
}

// writeAPIError sends a structured model/request error: {"code","model","message"}.
// Codes for limits enforced by this server (as opposed to the upstream model's).
const (
	codeUserRateLimited = "USER_RATE_LIMITED"
	codeTooManyStreams  = "TOO_MANY_STREAMS"
	codeMessageTooLong  = "MESSAGE_TOO_LONG"
)

func writeAPIError(w http.ResponseWriter, e *model.APIError) {
	writeJSON(w, e.Status, e)
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
