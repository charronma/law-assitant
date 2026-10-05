package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxResponseBytes = 32 << 20

// SupabaseStore is a SessionRepository backed by Supabase Postgres, reached
// through PostgREST.
//
// Every request carries the signed-in user's own access token (forwarded from
// the already-verified request) plus the project's public publishable key, so
// Postgres runs the statement as the `authenticated` role and Row Level
// Security (see supabase/migrations) is enforced by the database itself. No
// service-role key or database password is needed or used. Each query also
// filters on user_id explicitly, so isolation does not rest on RLS alone.
type SupabaseStore struct {
	restURL string
	apiKey  string
	client  *http.Client
	token   func(context.Context) (string, bool)
}

// NewSupabaseStore creates a store for the project at baseURL
// (https://<ref>.supabase.co). apiKey is the publishable (or legacy anon) key;
// token yields the caller's access token from a request context.
func NewSupabaseStore(baseURL, apiKey string, token func(context.Context) (string, bool)) *SupabaseStore {
	return &SupabaseStore{
		restURL: strings.TrimRight(baseURL, "/") + "/rest/v1/",
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 20 * time.Second},
		token:   token,
	}
}

type messageRow struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id,omitempty"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	FileIDs   []string  `json:"file_ids"`
	CreatedAt time.Time `json:"created_at"`
}

type sessionRow struct {
	ID        string    `json:"id"`
	Module    Module    `json:"module"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r sessionRow) session(userID string) *Session {
	return &Session{
		ID: r.ID, UserID: userID, Module: r.Module, Title: r.Title,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Messages: []Message{},
	}
}

// Create inserts a session; user_id defaults to auth.uid() in the database.
func (s *SupabaseStore) Create(ctx context.Context, userID string, module Module, title string) (*Session, error) {
	if title == "" {
		title = getDefaultTitle(module)
	}
	var rows []sessionRow
	err := s.do(ctx, http.MethodPost, "chat_sessions", nil,
		map[string]any{"module": module, "title": title}, "return=representation", &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("supabase: create session returned %d rows", len(rows))
	}
	return rows[0].session(userID), nil
}

// Get returns the session with its messages in chronological order.
func (s *SupabaseStore) Get(ctx context.Context, userID, id string) (*Session, error) {
	if !validUUIDs(userID, id) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	q := url.Values{}
	q.Set("id", "eq."+id)
	q.Set("user_id", "eq."+userID)
	q.Set("select", "id,module,title,created_at,updated_at,chat_messages(id,role,content,file_ids,created_at)")
	q.Set("chat_messages.order", "created_at.asc,id.asc")

	var rows []struct {
		sessionRow
		Messages []messageRow `json:"chat_messages"`
	}
	if err := s.do(ctx, http.MethodGet, "chat_sessions", q, nil, "", &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	sess := rows[0].session(userID)
	sess.Messages = make([]Message, 0, len(rows[0].Messages))
	for _, m := range rows[0].Messages {
		sess.Messages = append(sess.Messages, Message{
			ID: m.ID, SessionID: id, Role: m.Role, Content: m.Content,
			FileIDs: nilIfEmpty(m.FileIDs), CreatedAt: m.CreatedAt,
		})
	}
	sess.MessageCount = len(sess.Messages)
	return sess, nil
}

// List returns the user's sessions, newest first, without messages.
func (s *SupabaseStore) List(ctx context.Context, userID string) ([]*Session, error) {
	if !validUUIDs(userID) {
		return []*Session{}, nil
	}
	q := url.Values{}
	q.Set("user_id", "eq."+userID)
	q.Set("select", "id,module,title,created_at,updated_at,chat_messages(count)")
	q.Set("order", "updated_at.desc,id.asc")
	q.Set("limit", "500")

	var rows []struct {
		sessionRow
		Messages []struct {
			Count int `json:"count"`
		} `json:"chat_messages"`
	}
	if err := s.do(ctx, http.MethodGet, "chat_sessions", q, nil, "", &rows); err != nil {
		return nil, err
	}

	out := make([]*Session, 0, len(rows))
	for _, r := range rows {
		sess := r.session(userID)
		sess.Messages = nil
		if len(r.Messages) > 0 {
			sess.MessageCount = r.Messages[0].Count
		}
		out = append(out, sess)
	}
	return out, nil
}

// Delete removes the session; its messages are deleted with it (ON DELETE CASCADE).
func (s *SupabaseStore) Delete(ctx context.Context, userID, id string) error {
	if !validUUIDs(userID, id) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	q := url.Values{}
	q.Set("id", "eq."+id)
	q.Set("user_id", "eq."+userID)
	q.Set("select", "id")

	var deleted []struct {
		ID string `json:"id"`
	}
	if err := s.do(ctx, http.MethodDelete, "chat_sessions", q, nil, "return=representation", &deleted); err != nil {
		return err
	}
	if len(deleted) == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

// AddMessage appends a message. The database trigger bumps the session's
// updated_at and, for the first user message, derives its title.
func (s *SupabaseStore) AddMessage(ctx context.Context, userID, sessionID, role, content string, fileIDs []string) (*Message, error) {
	if !validUUIDs(userID, sessionID) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sessionID)
	}
	if fileIDs == nil {
		fileIDs = []string{}
	}
	var rows []messageRow
	err := s.do(ctx, http.MethodPost, "chat_messages", nil, map[string]any{
		"session_id": sessionID, "role": role, "content": content, "file_ids": fileIDs,
	}, "return=representation", &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("supabase: add message returned %d rows", len(rows))
	}
	m := rows[0]
	return &Message{
		ID: m.ID, SessionID: sessionID, Role: m.Role, Content: m.Content,
		FileIDs: nilIfEmpty(m.FileIDs), CreatedAt: m.CreatedAt,
	}, nil
}

// do performs one PostgREST request as the caller and decodes the JSON reply.
func (s *SupabaseStore) do(ctx context.Context, method, table string, query url.Values, body any, prefer string, out any) error {
	token, ok := s.token(ctx)
	if !ok {
		return fmt.Errorf("%w: no access token on the request", ErrUnauthorized)
	}

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	u := s.restURL + table
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	// The publishable key identifies the app; the user's JWT (not the key!)
	// decides the Postgres role and which rows RLS lets through.
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if prefer != "" {
		req.Header.Set("Prefer", prefer)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("supabase request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("supabase response read failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return mapPostgRESTError(resp.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("supabase response decode failed: %w", err)
	}
	return nil
}

// mapPostgRESTError translates PostgREST/Postgres failures into the store's
// sentinel errors. Anything unexpected keeps its detail for the server log but
// is never shown to clients by the handlers.
func mapPostgRESTError(status int, raw []byte) error {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)

	switch {
	case status == http.StatusUnauthorized,
		strings.HasPrefix(e.Code, "PGRST30"): // PGRST301/302/303: JWT missing, expired or invalid
		return fmt.Errorf("%w: %s", ErrUnauthorized, e.Message)
	case e.Code == "42501", // RLS or privilege violation: someone else's (or no) session
		e.Code == "23503", // foreign key violation: session does not exist
		e.Code == "22P02": // invalid text representation (malformed uuid)
		return fmt.Errorf("%w: %s", ErrNotFound, e.Message)
	case e.Code == "23514": // check_violation (e.g. unknown module)
		return fmt.Errorf("%w: %s", ErrInvalidInput, e.Message)
	}
	return fmt.Errorf("supabase error %d (%s): %s", status, e.Code, e.Message)
}

func validUUIDs(ids ...string) bool {
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
	}
	return true
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

var _ SessionRepository = (*SupabaseStore)(nil)
