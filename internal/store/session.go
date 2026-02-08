package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

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
	Module       Module    `json:"module"`
	Title        string    `json:"title"`
	Messages     []Message `json:"messages"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// SessionStore manages chat sessions in memory
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

// Create creates a new session
func (s *SessionStore) Create(module Module, title string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if title == "" {
		title = getDefaultTitle(module)
	}

	session := &Session{
		ID:        uuid.New().String(),
		Module:    module,
		Title:     title,
		Messages:  make([]Message, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.sessions[session.ID] = session
	return session
}

// Get returns a session by ID
func (s *SessionStore) Get(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return session, nil
}

// List returns all sessions sorted by update time (newest first)
func (s *SessionStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}

	// Sort by updated_at descending
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].UpdatedAt.After(sessions[i].UpdatedAt) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}

	return sessions
}

// Delete removes a session
func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[id]; !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	delete(s.sessions, id)
	return nil
}

// AddMessage adds a message to a session
func (s *SessionStore) AddMessage(sessionID, role, content string, fileIDs []string) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
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

// GetMessages returns all messages for a session
func (s *SessionStore) GetMessages(sessionID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return session.Messages, nil
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
