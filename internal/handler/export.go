package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"law-assistant/internal/export"
)

const (
	maxExportBodyBytes = 1 << 20
	maxExportChars     = 200_000
)

type exportRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// handleExportDocx renders Markdown (an assistant reply) as a Word document.
func (s *Server) handleExportDocx(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxExportBodyBytes)
	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "内容过长，无法导出")
			return
		}
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "Content is required")
		return
	}
	if utf8.RuneCountInString(req.Content) > maxExportChars {
		writeError(w, http.StatusRequestEntityTooLarge, "内容过长，无法导出")
		return
	}

	// A document that already opens with its own heading does not need a second title.
	title := strings.TrimSpace(req.Title)
	name := title
	if first := export.FirstHeading(req.Content); first != "" {
		if name == "" {
			name = first
		}
		if strings.HasPrefix(strings.TrimSpace(req.Content), "#") {
			title = ""
		}
	}
	if name == "" {
		name = "AI法律助手文档"
	}

	data, err := export.MarkdownToDocx(title, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "导出失败")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", contentDisposition(name+".docx"))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	_, _ = w.Write(data)
}

// contentDisposition builds an attachment header with an ASCII fallback and an
// RFC 5987 UTF-8 filename, stripping anything that could break the header.
func contentDisposition(filename string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f, strings.ContainsRune(`/\:*?"<>|;`, r):
			return '_'
		}
		return r
	}, filename)
	if r := []rune(clean); len(r) > 80 {
		clean = string(r[:76]) + ".docx"
	}
	ascii := strings.Map(func(r rune) rune {
		if r > 0x7e {
			return '_'
		}
		return r
	}, clean)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(clean))
}
