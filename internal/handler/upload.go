package handler

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"law-assistant/internal/store"
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

	if ok, retry := s.uploadRate.Allow(userID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
		writeUploadError(w, http.StatusTooManyRequests, codeUserRateLimited,
			fmt.Sprintf("上传太频繁，请 %d 秒后再试", int(retry.Seconds())))
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

	// The parser works on a path, so spool the upload to a private temp file.
	tmp, err := os.CreateTemp("", "upload-*"+strings.ToLower(filepath.Ext(header.Filename)))
	if err != nil {
		log.Printf("Failed to create temp file: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, file); err != nil {
		log.Printf("Failed to spool upload: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	// Parse first: a document the model cannot read is rejected before anything is stored.
	res, err := s.docParser.Parse(tmp.Name())
	if err != nil {
		log.Printf("Failed to parse uploaded file %s: %v", header.Filename, err)
		status, code, msg := extractionFailure(err)
		writeUploadError(w, status, code, msg)
		return
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}
	uploadedFile, err := s.fileStore.Create(r.Context(), userID, store.NewFile{
		SessionID:   sessionID,
		Filename:    header.Filename,
		ContentType: header.Header.Get("Content-Type"),
		Size:        header.Size,
		Text:        res.Text,
		Content:     tmp,
	})
	if err != nil {
		log.Printf("Failed to store uploaded file: %v", err)
		if errors.Is(err, store.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "Unauthorized")
		} else {
			writeError(w, http.StatusInternalServerError, "Failed to save file")
		}
		return
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
