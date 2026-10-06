package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// UploadedFile represents an uploaded file
type UploadedFile struct {
	ID            string    `json:"id"`
	UserID        string    `json:"-"`
	SessionID     string    `json:"session_id"`
	Filename      string    `json:"filename"`
	ContentType   string    `json:"content_type"`
	Size          int64     `json:"size"`
	ExtractedText string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

// NewFile describes a file to store. Text is what was extracted from it
// (never empty: unreadable uploads are rejected before they get here).
type NewFile struct {
	SessionID   string
	Filename    string
	ContentType string
	Size        int64
	Text        string
	Content     io.Reader
}

// FileRepository stores uploads and their extracted text per user.
type FileRepository interface {
	Create(ctx context.Context, userID string, f NewFile) (*UploadedFile, error)
	// Get returns ErrFileNotFound for missing files and other users' files alike.
	Get(ctx context.Context, userID, id string) (*UploadedFile, error)
	Delete(ctx context.Context, userID, id string) error
}

// ErrFileNotFound is returned when a file does not exist or belongs to
// another user; callers cannot (and must not) tell the two apart.
var ErrFileNotFound = errors.New("file not found")

// FileStore is the local-disk, in-memory FileRepository. Metadata and text
// are lost on restart; use SupabaseFileStore to persist them.
type FileStore struct {
	mu        sync.RWMutex
	files     map[string]*localFile
	uploadDir string
}

type localFile struct {
	UploadedFile
	path string
}

// NewFileStore creates a new file store
func NewFileStore(uploadDir string) *FileStore {
	return &FileStore{files: make(map[string]*localFile), uploadDir: uploadDir}
}

// Create writes the file to disk and records its metadata and text.
func (fs *FileStore) Create(_ context.Context, userID string, in NewFile) (*UploadedFile, error) {
	id := uuid.New().String()
	path := filepath.Join(fs.uploadDir, id+safeExt(in.Filename))

	out, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	_, copyErr := io.Copy(out, in.Content)
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(path)
		return nil, fmt.Errorf("failed to write file: %w", copyErr)
	}

	f := &localFile{
		UploadedFile: UploadedFile{
			ID: id, UserID: userID, SessionID: in.SessionID, Filename: in.Filename,
			ContentType: in.ContentType, Size: in.Size, ExtractedText: in.Text, CreatedAt: time.Now(),
		},
		path: path,
	}
	fs.mu.Lock()
	fs.files[id] = f
	fs.mu.Unlock()
	snapshot := f.UploadedFile
	return &snapshot, nil
}

// Get returns a snapshot of the user's file with the given ID
func (fs *FileStore) Get(_ context.Context, userID, id string) (*UploadedFile, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	f, ok := fs.files[id]
	if !ok || f.UserID != userID {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	snapshot := f.UploadedFile
	return &snapshot, nil
}

// Delete removes the user's file from the index and from disk.
func (fs *FileStore) Delete(_ context.Context, userID, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	f, ok := fs.files[id]
	if !ok || f.UserID != userID {
		return fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	delete(fs.files, id)
	if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// safeExt returns the lower-case file extension reduced to [a-z0-9] so it is
// safe in a path or an object key.
func safeExt(filename string) string {
	ext := []rune(filepath.Ext(filename))
	out := make([]rune, 0, len(ext))
	for _, r := range ext {
		switch {
		case r == '.' && len(out) == 0:
			out = append(out, r)
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	if len(out) <= 1 {
		return ""
	}
	return string(out)
}
