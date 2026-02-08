package store

import (
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
	mu       sync.RWMutex
	files    map[string]*UploadedFile
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
func (fs *FileStore) Save(sessionID, filename, contentType string, size int64, reader io.Reader) (*UploadedFile, error) {
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

// Get returns a file by ID
func (fs *FileStore) Get(id string) (*UploadedFile, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	file, ok := fs.files[id]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", id)
	}
	return file, nil
}

// SetExtractedText updates the extracted text for a file
func (fs *FileStore) SetExtractedText(id, text string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	file, ok := fs.files[id]
	if !ok {
		return fmt.Errorf("file not found: %s", id)
	}
	file.ExtractedText = text
	return nil
}
