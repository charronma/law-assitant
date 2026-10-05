package store

import (
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
	StoragePath   string    `json:"-"`
	ExtractedText string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

// FileStore manages uploaded files
type FileStore struct {
	mu        sync.RWMutex
	files     map[string]*UploadedFile
	uploadDir string
}

// NewFileStore creates a new file store
func NewFileStore(uploadDir string) *FileStore {
	return &FileStore{
		files:     make(map[string]*UploadedFile),
		uploadDir: uploadDir,
	}
}

// Save saves an uploaded file to disk and records metadata
func (fs *FileStore) Save(userID, sessionID, filename, contentType string, size int64, reader io.Reader) (*UploadedFile, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fileID := uuid.New().String()
	ext := filepath.Ext(filename)
	storageName := fileID + ext
	storagePath := filepath.Join(fs.uploadDir, storageName)

	// Write file to disk
	outFile, err := os.Create(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, reader); err != nil {
		os.Remove(storagePath)
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	file := &UploadedFile{
		ID:          fileID,
		UserID:      userID,
		SessionID:   sessionID,
		Filename:    filename,
		ContentType: contentType,
		Size:        size,
		StoragePath: storagePath,
		CreatedAt:   time.Now(),
	}

	fs.files[fileID] = file
	return file, nil
}

// ErrFileNotFound is returned when a file does not exist or belongs to
// another user; callers cannot (and must not) tell the two apart.
var ErrFileNotFound = errors.New("file not found")

// Get returns a snapshot of the user's file with the given ID
func (fs *FileStore) Get(userID, id string) (*UploadedFile, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	file, ok := fs.files[id]
	if !ok || file.UserID != userID {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	snapshot := *file
	return &snapshot, nil
}

// SetExtractedText updates the extracted text for a file
func (fs *FileStore) SetExtractedText(userID, id, text string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	file, ok := fs.files[id]
	if !ok || file.UserID != userID {
		return fmt.Errorf("%w: %s", ErrFileNotFound, id)
	}
	file.ExtractedText = text
	return nil
}
