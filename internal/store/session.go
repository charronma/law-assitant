package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a session does not exist or belongs to another
// user; callers cannot (and must not) tell the two apart.
var ErrNotFound = errors.New("session not found")

// ErrInvalidInput is returned when the backing store rejects a value (for
// example an unknown module).
var ErrInvalidInput = errors.New("invalid input")

// ErrUnauthorized is returned when the backing store rejects the caller's
// credentials (for example an expired access token).
var ErrUnauthorized = errors.New("unauthorized")

// SessionRepository is the persistence boundary for chat sessions. Every
// method is scoped to userID; a session owned by someone else behaves exactly
// like a missing one (ErrNotFound).
type SessionRepository interface {
	Create(ctx context.Context, userID string, module Module, title string) (*Session, error)
	// Get returns the session together with its full message history.
	Get(ctx context.Context, userID, id string) (*Session, error)
	// List returns session summaries (no messages), newest first.
	List(ctx context.Context, userID string) ([]*Session, error)
	Delete(ctx context.Context, userID, id string) error
	AddMessage(ctx context.Context, userID, sessionID, role, content string, fileIDs []string) (*Message, error)
}

// Module represents the functional module type
type Module string

const (
	ModuleConsult       Module = "consult"
	ModulePleading      Module = "pleading"
	ModuleContract      Module = "contract"
	ModuleEvidenceOrg   Module = "evidence_org"
	ModuleEvidence      Module = "evidence"
	ModuleCommunication Module = "communication"
)

// Valid reports whether m is one of the known modules.
func (m Module) Valid() bool {
	switch m {
	case ModuleConsult, ModulePleading, ModuleContract, ModuleEvidenceOrg, ModuleEvidence, ModuleCommunication:
		return true
	}
	return false
}

// Message represents a single chat message
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	FileIDs   []string  `json:"file_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Session represents a chat session
type Session struct {
	ID           string    `json:"id"`
	UserID       string    `json:"-"`
	Module       Module    `json:"module"`
	Title        string    `json:"title"`
	Messages     []Message `json:"messages"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// SessionStore is an in-memory SessionRepository. Sessions are lost on restart;
// it is used for local development and tests.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionStore creates a new session store
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session owned by userID
func (s *SessionStore) Create(_ context.Context, userID string, module Module, title string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if title == "" {
		title = getDefaultTitle(module)
	}

	session := &Session{
		ID:        uuid.New().String(),
		UserID:    userID,
		Module:    module,
		Title:     title,
		Messages:  make([]Message, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.sessions[session.ID] = session
	snapshot := *session
	return &snapshot, nil
}

// owned returns the session if it exists and belongs to userID.
// The caller must hold s.mu.
func (s *SessionStore) owned(userID, id string) (*Session, error) {
	session, ok := s.sessions[id]
	if !ok || session.UserID != userID {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return session, nil
}

// Get returns a snapshot of the user's session with the given ID. The returned
// value is a copy and is safe to read without holding the store lock.
func (s *SessionStore) Get(_ context.Context, userID, id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, err := s.owned(userID, id)
	if err != nil {
		return nil, err
	}
	snapshot := *session
	snapshot.Messages = slices.Clone(session.Messages)
	return &snapshot, nil
}

// List returns snapshots of the user's sessions sorted by update time (newest
// first). Messages are omitted; use Get to load the full history.
func (s *SessionStore) List(_ context.Context, userID string) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, 0)
	for _, session := range s.sessions {
		if session.UserID != userID {
			continue
		}
		snapshot := *session
		snapshot.Messages = nil
		sessions = append(sessions, &snapshot)
	}

	slices.SortFunc(sessions, func(a, b *Session) int {
		return cmp.Compare(b.UpdatedAt.UnixNano(), a.UpdatedAt.UnixNano())
	})

	return sessions, nil
}

// Delete removes a session
func (s *SessionStore) Delete(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.owned(userID, id); err != nil {
		return err
	}
	delete(s.sessions, id)
	return nil
}

// AddMessage adds a message to a session
func (s *SessionStore) AddMessage(_ context.Context, userID, sessionID, role, content string, fileIDs []string) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, err := s.owned(userID, sessionID)
	if err != nil {
		return nil, err
	}

	msg := Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		FileIDs:   fileIDs,
		CreatedAt: time.Now(),
	}

	session.Messages = append(session.Messages, msg)
	session.MessageCount = len(session.Messages)
	session.UpdatedAt = time.Now()

	// Auto-update title from first user message
	if role == "user" && len(session.Messages) == 1 {
		title := content
		if len([]rune(title)) > 20 {
			title = string([]rune(title)[:20]) + "..."
		}
		session.Title = title
	}

	return &msg, nil
}

func getDefaultTitle(module Module) string {
	switch module {
	case ModuleConsult:
		return "新建法律咨询"
	case ModulePleading:
		return "新建诉状撰写"
	case ModuleContract:
		return "新建合同优化"
	case ModuleEvidenceOrg:
		return "新建证据整理"
	case ModuleEvidence:
		return "新建取证指导"
	case ModuleCommunication:
		return "新建沟通话术"
	default:
		return "新建会话"
	}
}

var _ SessionRepository = (*SessionStore)(nil)
