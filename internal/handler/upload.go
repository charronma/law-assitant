package handler

import (
	"log"
	"net/http"

	"law-assistant/internal/tool"
)

// handleUpload processes file upload requests
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Limit request size
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadSize)

	if err := r.ParseMultipartForm(s.cfg.MaxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "File too large (max 20MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "No file provided")
		return
	}
	defer file.Close()

	// Validate file format
	if !tool.IsSupportedFormat(header.Filename) {
		writeError(w, http.StatusBadRequest, "Unsupported file format. Supported: .docx, .pdf, .txt, .md")
		return
	}

	sessionID := r.FormValue("session_id")

	// Save file
	uploadedFile, err := s.fileStore.Save(sessionID, header.Filename, header.Header.Get("Content-Type"), header.Size, file)
	if err != nil {
		log.Printf("Failed to save uploaded file: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	// Try to extract text immediately
	text, err := s.docParser.Parse(uploadedFile.StoragePath)
	if err != nil {
		log.Printf("Warning: Failed to parse uploaded file %s: %v", uploadedFile.Filename, err)
	} else {
		s.fileStore.SetExtractedText(uploadedFile.ID, text)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"file_id":      uploadedFile.ID,
		"filename":     uploadedFile.Filename,
		"size":         uploadedFile.Size,
		"content_type": uploadedFile.ContentType,
	})
}
