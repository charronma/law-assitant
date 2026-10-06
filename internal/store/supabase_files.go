package store

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const uploadsBucket = "uploads"

// SupabaseFileStore persists uploads in a private Supabase Storage bucket and
// their metadata plus extracted text in Postgres (public.uploaded_files).
//
// Like SupabaseStore it acts with the signed-in user's own access token and
// the public publishable key, so Storage and table Row Level Security (see
// supabase/migrations) apply: objects live under "<user id>/" and a user can
// only reach their own folder.
type SupabaseFileStore struct {
	rows       *SupabaseStore // reuse PostgREST plumbing
	storageURL string
	apiKey     string
	client     *http.Client
	token      func(context.Context) (string, bool)
}

// NewSupabaseFileStore creates a store for the project at baseURL.
func NewSupabaseFileStore(baseURL, apiKey string, token func(context.Context) (string, bool)) *SupabaseFileStore {
	return &SupabaseFileStore{
		rows:       NewSupabaseStore(baseURL, apiKey, token),
		storageURL: strings.TrimRight(baseURL, "/") + "/storage/v1/object/",
		apiKey:     apiKey,
		client:     &http.Client{Timeout: 60 * time.Second},
		token:      token,
	}
}

type fileRow struct {
	ID            string    `json:"id"`
	SessionID     *string   `json:"session_id"`
	Filename      string    `json:"filename"`
	ContentType   string    `json:"content_type"`
	Size          int64     `json:"size"`
	StoragePath   string    `json:"storage_path,omitempty"`
	ExtractedText string    `json:"extracted_text,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r fileRow) file(userID string) *UploadedFile {
	return &UploadedFile{
		ID: r.ID, UserID: userID, SessionID: deref(r.SessionID), Filename: r.Filename,
		ContentType: r.ContentType, Size: r.Size, ExtractedText: r.ExtractedText, CreatedAt: r.CreatedAt,
	}
}

// Create uploads the bytes, then records the row. If the row cannot be
// written the object is removed again so nothing is orphaned.
func (s *SupabaseFileStore) Create(ctx context.Context, userID string, in NewFile) (*UploadedFile, error) {
	if !validUUIDs(userID) {
		return nil, fmt.Errorf("%w: bad user id", ErrUnauthorized)
	}
	id := uuid.New().String()
	key := userID + "/" + id + safeExt(in.Filename)

	if err := s.putObject(ctx, key, in.ContentType, in.Size, in.Content); err != nil {
		return nil, fmt.Errorf("upload to storage: %w", err)
	}

	row := map[string]any{
		"id": id, "filename": in.Filename, "content_type": in.ContentType, "size": in.Size,
		"storage_path": key, "extracted_text": in.Text,
	}
	if validUUIDs(in.SessionID) {
		row["session_id"] = in.SessionID
	}
	var rows []fileRow
	err := s.rows.do(ctx, http.MethodPost, "uploaded_files", nil, row, "return=representation", &rows)
	if err == nil && len(rows) != 1 {
		err = fmt.Errorf("supabase: insert file returned %d rows", len(rows))
	}
	if err != nil {
		if derr := s.deleteObject(context.WithoutCancel(ctx), key); derr != nil {
			err = fmt.Errorf("%w (and cleanup failed: %v)", err, derr)
		}
		return nil, err
	}
	f := rows[0].file(userID)
	f.ExtractedText = in.Text
	return f, nil
}

// Get returns the user's file including its extracted text.
func (s *SupabaseFileStore) Get(ctx context.Context, userID, id string) (*UploadedFile, error) {
	if !validUUIDs(userID, id) {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	q := url.Values{}
	q.Set("id", "eq."+id)
	q.Set("user_id", "eq."+userID)
	q.Set("select", "id,session_id,filename,content_type,size,extracted_text,created_at")
	var rows []fileRow
	if err := s.rows.do(ctx, http.MethodGet, "uploaded_files", q, nil, "", &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	return rows[0].file(userID), nil
}

// Delete removes the row and the stored object.
func (s *SupabaseFileStore) Delete(ctx context.Context, userID, id string) error {
	if !validUUIDs(userID, id) {
		return fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	q := url.Values{}
	q.Set("id", "eq."+id)
	q.Set("user_id", "eq."+userID)
	q.Set("select", "storage_path")
	var rows []fileRow
	if err := s.rows.do(ctx, http.MethodDelete, "uploaded_files", q, nil, "return=representation", &rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	return s.deleteObject(ctx, rows[0].StoragePath)
}

func (s *SupabaseFileStore) objectURL(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return s.storageURL + uploadsBucket + "/" + strings.Join(parts, "/")
}

func (s *SupabaseFileStore) storageDo(ctx context.Context, method, key, contentType string, size int64, body io.Reader) error {
	tok, ok := s.token(ctx)
	if !ok {
		return fmt.Errorf("%w: no access token on the request", ErrUnauthorized)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.objectURL(key), body)
	if err != nil {
		return err
	}
	if body != nil {
		req.ContentLength = size
	}
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+tok)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if method == http.MethodPost {
		req.Header.Set("x-upsert", "false")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("storage request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: storage %d: %s", ErrUnauthorized, resp.StatusCode, raw)
	default:
		return fmt.Errorf("storage %d: %s", resp.StatusCode, raw)
	}
}

func (s *SupabaseFileStore) putObject(ctx context.Context, key, contentType string, size int64, r io.Reader) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return s.storageDo(ctx, http.MethodPost, key, contentType, size, r)
}

// deleteObject removes an object.
func (s *SupabaseFileStore) deleteObject(ctx context.Context, key string) error {
	return s.storageDo(ctx, http.MethodDelete, key, "", 0, nil)
}
