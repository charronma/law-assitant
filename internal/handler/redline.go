package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/model"
	"law-assistant/internal/redline"
	"law-assistant/internal/store"
)

const (
	// redlineAuthor is shown by Word as the author of every tracked change and comment.
	redlineAuthor = "AI 法律助手"

	codeNotDocx       = "NOT_DOCX"
	codeRedlinePlan   = "REDLINE_PLAN_INVALID"
	codeRedlineFormat = "REDLINE_UNSUPPORTED"

	maxRedlineEntries = 60
	maxDocxBytes      = 25 << 20
	redlineTimeout    = 4 * time.Minute
)

type redlineRequest struct {
	FileID      string `json:"file_id"`
	Instruction string `json:"instruction"`
	Model       string `json:"model,omitempty"`
}

type redlineSkip struct {
	Kind   string `json:"kind"`
	Para   int    `json:"para"`
	Find   string `json:"find,omitempty"`
	Reason string `json:"reason"`
}

type redlineChange struct {
	Kind    string `json:"kind"`
	Para    int    `json:"para"`
	Find    string `json:"find,omitempty"`
	Replace string `json:"replace,omitempty"`
	Comment string `json:"comment,omitempty"`
}

type redlineResponse struct {
	Filename   string          `json:"filename"`
	DocxBase64 string          `json:"docx_base64,omitempty"`
	Summary    string          `json:"summary"`
	Applied    []redlineChange `json:"applied"`
	Skipped    []redlineSkip   `json:"skipped"`
	Truncated  bool            `json:"truncated,omitempty"`
}

// handleGetFile returns the metadata of one of the user's uploads.
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	f, err := s.fileStore.Get(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "File not found")
		} else if errors.Is(err, store.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "Unauthorized")
		} else {
			log.Printf("get file: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to load file")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": f.ID, "filename": f.Filename, "size": f.Size, "content_type": f.ContentType,
		"chars": utf8.RuneCountInString(f.ExtractedText),
	})
}

// handleRedline asks the model for a revision plan for an uploaded .docx and
// applies it to the original file as tracked changes and comments.
func (s *Server) handleRedline(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req redlineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	modelID, err := s.models.Resolve(req.Model)
	if err != nil {
		writeAPIError(w, &model.APIError{Status: http.StatusBadRequest, Code: model.CodeInvalidModel, Message: "不支持的模型"})
		return
	}

	// This is a full model call: it counts against the same per-user limits as chat.
	if ok, retry := s.chatRate.Allow(userID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
		writeUploadError(w, http.StatusTooManyRequests, codeUserRateLimited, fmt.Sprintf("请求太频繁，请 %d 秒后再试", int(retry.Seconds())))
		return
	}
	release, ok := s.chatStreams.Acquire(userID)
	if !ok {
		writeUploadError(w, http.StatusTooManyRequests, codeTooManyStreams, "你有多个任务正在进行，请等待完成后再试")
		return
	}
	defer release()

	file, err := s.fileStore.Get(r.Context(), userID, req.FileID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrFileNotFound):
			writeUploadError(w, http.StatusUnprocessableEntity, codeFileUnavailable, "文件已失效或无法读取，请重新上传")
		case errors.Is(err, store.ErrUnauthorized):
			writeError(w, http.StatusUnauthorized, "Unauthorized")
		default:
			log.Printf("redline: get file: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to load file")
		}
		return
	}
	if !strings.EqualFold(filepath.Ext(file.Filename), ".docx") {
		writeUploadError(w, http.StatusUnprocessableEntity, codeNotDocx, "只有 Word（.docx）文件可以生成带修订痕迹的版本；PDF 和文本无法保留原有格式")
		return
	}
	rc, err := s.fileStore.Open(r.Context(), userID, req.FileID)
	if err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			writeUploadError(w, http.StatusUnprocessableEntity, codeFileUnavailable, "文件已失效或无法读取，请重新上传")
		} else {
			log.Printf("redline: open file: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to load file")
		}
		return
	}
	docx, err := io.ReadAll(io.LimitReader(rc, maxDocxBytes+1))
	rc.Close()
	if err != nil || len(docx) > maxDocxBytes {
		writeError(w, http.StatusInternalServerError, "Failed to read file")
		return
	}

	paras, err := redline.Paragraphs(docx)
	if err != nil {
		writeUploadError(w, http.StatusUnprocessableEntity, codeRedlineFormat, "这份 Word 文档的结构暂不支持生成修订版")
		return
	}
	if len(paras) == 0 {
		writeUploadError(w, http.StatusUnprocessableEntity, codeNoText, "文档中没有可审阅的文字")
		return
	}

	// The model call can take over a minute; give this response a longer write deadline.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(redlineTimeout + 30*time.Second))
	ctx, cancel := context.WithTimeout(r.Context(), redlineTimeout)
	defer cancel()

	prompt, truncated := redline.BuildUserPrompt(req.Instruction, paras, s.cfg.MaxDocumentChars)
	m, err := s.models.Get(ctx, modelID)
	if err != nil {
		s.writeModelError(w, err, modelID)
		return
	}
	reply, err := m.Generate(ctx, []*schema.Message{
		schema.SystemMessage(redline.SystemPrompt),
		schema.UserMessage(prompt),
	})
	if err != nil {
		s.writeModelError(w, err, modelID)
		return
	}

	plan, summary, err := redline.ParsePlan(reply.Content)
	if err != nil {
		log.Printf("redline: unparseable plan from %s: %v", modelID, err)
		writeAPIError(w, &model.APIError{Status: http.StatusBadGateway, Code: codeRedlinePlan, Model: modelID,
			Message: "模型返回的内容无法解析为修改清单，请重试或换一个模型"})
		return
	}
	if len(plan.Edits) > maxRedlineEntries {
		plan.Edits = plan.Edits[:maxRedlineEntries]
	}
	if len(plan.Insertions) > maxRedlineEntries {
		plan.Insertions = plan.Insertions[:maxRedlineEntries]
	}

	resp := redlineResponse{Summary: summary, Truncated: truncated, Applied: []redlineChange{}, Skipped: []redlineSkip{}}
	if len(plan.Edits)+len(plan.Insertions) == 0 {
		writeJSON(w, http.StatusOK, resp) // nothing to change: no file
		return
	}

	res, err := redline.Apply(docx, plan, redlineAuthor, time.Now())
	if err != nil {
		log.Printf("redline: apply: %v", err)
		writeUploadError(w, http.StatusUnprocessableEntity, codeRedlineFormat, "这份 Word 文档的结构暂不支持生成修订版")
		return
	}
	for _, it := range res.Applied {
		resp.Applied = append(resp.Applied, describe(plan, it))
	}
	for _, it := range res.Skipped {
		c := describe(plan, it)
		resp.Skipped = append(resp.Skipped, redlineSkip{Kind: c.Kind, Para: c.Para, Find: c.Find, Reason: it.Reason})
	}
	if len(res.Applied) > 0 {
		base := strings.TrimSuffix(file.Filename, filepath.Ext(file.Filename))
		resp.Filename = base + "-修订版.docx"
		resp.DocxBase64 = base64.StdEncoding.EncodeToString(res.Docx)
	}
	writeJSON(w, http.StatusOK, resp)
}

func describe(plan redline.Plan, it redline.Item) redlineChange {
	c := redlineChange{Kind: it.Kind, Para: it.Para}
	switch it.Kind {
	case "edit":
		e := plan.Edits[it.Index]
		c.Find, c.Comment = clip(e.Find, 80), e.Comment
		if e.Replace != nil {
			c.Replace = clip(*e.Replace, 80)
		}
	case "insertion":
		in := plan.Insertions[it.Index]
		c.Replace, c.Comment = clip(in.Text, 80), in.Comment
	}
	return c
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
