package handler

import (
	"errors"
	"log"
	"net/http"

	"law-assistant/internal/tool"
)

// Upload error codes surfaced to the client (see ChatInput).
const (
	codeUnsupportedFormat = "UNSUPPORTED_FORMAT"
	codeNoText            = "NO_TEXT"
	codeExtractFailed     = "EXTRACT_FAILED"
	codeFileTooLarge      = "FILE_TOO_LARGE"
	codeFileUnavailable   = "FILE_UNAVAILABLE"
)

const previewRunes = 120

// writeUploadError sends {error, code, message}; "error" keeps older clients working.
func writeUploadError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code, "message": message})
}

// extractionFailure maps a parser error to an HTTP status, code and a message
// that tells the user what to do next.
func extractionFailure(err error) (int, string, string) {
	switch {
	case errors.Is(err, tool.ErrLegacyDoc):
		return http.StatusUnprocessableEntity, codeUnsupportedFormat, "暂不支持旧版 .doc 格式，请在 Word 中另存为 .docx 后重新上传"
	case errors.Is(err, tool.ErrUnsupportedFormat):
		return http.StatusUnprocessableEntity, codeUnsupportedFormat, "不支持的文件格式，请上传 .docx、.pdf、.txt 或 .md"
	case errors.Is(err, tool.ErrNoText):
		return http.StatusUnprocessableEntity, codeNoText, "未能从文件中提取到文字。如果是扫描件或图片 PDF，请先做 OCR 或直接粘贴文字内容"
	case errors.Is(err, tool.ErrTooLarge):
		return http.StatusUnprocessableEntity, codeFileTooLarge, "文件内容过大，请拆分后上传"
	case errors.Is(err, tool.ErrCorrupt):
		return http.StatusUnprocessableEntity, codeExtractFailed, "文件已损坏或格式不正确，无法解析，请检查后重新上传"
	default:
		return http.StatusInternalServerError, codeExtractFailed, "文件解析失败，请稍后重试"
	}
}

// handleUpload processes file upload requests. The file is parsed before the
// response is sent: a document the model cannot read is rejected here, never
// silently accepted.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}

	// Limit request size
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadSize)

	if err := r.ParseMultipartForm(s.cfg.MaxUploadSize); err != nil {
		writeUploadError(w, http.StatusRequestEntityTooLarge, codeFileTooLarge, "文件过大（最大 20MB）")
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
		status, code, msg := extractionFailure(tool.ErrUnsupportedFormat)
		if tool.IsLegacyDoc(header.Filename) {
			status, code, msg = extractionFailure(tool.ErrLegacyDoc)
		}
		writeUploadError(w, status, code, msg)
		return
	}

	sessionID := r.FormValue("session_id")

	// Save file
	uploadedFile, err := s.fileStore.Save(userID, sessionID, header.Filename, header.Header.Get("Content-Type"), header.Size, file)
	if err != nil {
		log.Printf("Failed to save uploaded file: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	res, err := s.docParser.Parse(uploadedFile.StoragePath)
	if err != nil {
		log.Printf("Failed to parse uploaded file %s: %v", uploadedFile.Filename, err)
		if derr := s.fileStore.Delete(userID, uploadedFile.ID); derr != nil {
			log.Printf("Failed to remove rejected upload %s: %v", uploadedFile.ID, derr)
		}
		status, code, msg := extractionFailure(err)
		writeUploadError(w, status, code, msg)
		return
	}
	if err := s.fileStore.SetExtractedText(userID, uploadedFile.ID, res.Text); err != nil {
		log.Printf("Failed to cache extracted text: %v", err)
	}

	runes := []rune(res.Text)
	preview := runes
	if len(preview) > previewRunes {
		preview = preview[:previewRunes]
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"file_id":      uploadedFile.ID,
		"filename":     uploadedFile.Filename,
		"size":         uploadedFile.Size,
		"content_type": uploadedFile.ContentType,
		"chars":        len(runes),
		"preview":      string(preview),
		"truncated":    res.Truncated,
	})
}
