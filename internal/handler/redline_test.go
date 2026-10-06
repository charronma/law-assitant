package handler

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/config"
	"law-assistant/internal/redline"
	"law-assistant/internal/tool"
)

func (e *testEnv) redline(t *testing.T, bearer string, body map[string]any) (int, redlineResponse, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := e.do("POST", "/api/redline", bearer, strings.NewReader(string(raw)), "application/json")
	var out redlineResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func scripted(reply string) func([]*schema.Message) (string, error) {
	return func([]*schema.Message) (string, error) { return reply, nil }
}

const penaltyPlan = `{"summary":"违约金偏高","edits":[
  {"para":%d,"find":"十万元","replace":"一万元","comment":"违约金远超实际损失，建议下调"},
  {"para":1,"find":"这段话不存在","replace":"x","comment":"c"}],
 "insertions":[{"after":1,"text":"新增：双方均应保守商业秘密。","comment":"补充保密条款"}]}`

// penaltyParagraph finds the paragraph that carries the penalty clause, numbered
// the way the model sees the document.
func penaltyParagraph(t *testing.T) int {
	t.Helper()
	ps, err := redline.Paragraphs(fixture(t, "contract.docx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if strings.Contains(p.Text, "违约金人民币十万元") {
			return p.Index
		}
	}
	t.Fatal("fixture has no penalty paragraph")
	return 0
}

func TestRedlineProducesATrackedChangesDocument(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "劳动合同.docx", fixture(t, "contract.docx"))
	n := penaltyParagraph(t)
	e.llm.setGenerate("kimi-k3", scripted(strings.Replace(penaltyPlan, "%d", itoa(n), 1)))

	code, out, raw := e.redline(t, alice, map[string]any{"file_id": up.FileID, "instruction": "我是劳动者，站在劳动者立场修改", "model": "kimi-k3"})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if out.Filename != "劳动合同-修订版.docx" || out.Summary != "违约金偏高" {
		t.Errorf("filename=%q summary=%q", out.Filename, out.Summary)
	}
	if len(out.Applied) != 2 || len(out.Skipped) != 1 || !strings.Contains(out.Skipped[0].Reason, "找到") {
		t.Fatalf("applied=%+v skipped=%+v", out.Applied, out.Skipped)
	}

	// What the model was shown.
	e.llm.mu.Lock()
	in := e.llm.seen[len(e.llm.seen)-1]
	e.llm.mu.Unlock()
	if len(in) != 2 || in[0].Role != schema.System || !strings.Contains(in[1].Content, "站在劳动者立场") ||
		!strings.Contains(in[1].Content, "[P1] 劳动合同") || !strings.Contains(in[1].Content, "违约金人民币十万元") {
		t.Errorf("prompt: %+v", in)
	}

	// The file: a valid docx whose text, with the AI's changes accepted, has the new wording.
	data, err := base64.StdEncoding.DecodeString(out.DocxBase64)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a docx: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["word/comments.xml"] || !names["word/document.xml"] {
		t.Errorf("parts: %v", names)
	}
	p := filepath.Join(t.TempDir(), "out.docx")
	_ = os.WriteFile(p, data, 0o644)
	res, err := tool.NewDocumentParser().Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "违约金人民币一万元") || strings.Contains(res.Text, "违约金人民币十万元") {
		t.Errorf("revised text not as expected:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "新增：双方均应保守商业秘密。") {
		t.Errorf("inserted clause missing")
	}
	// The stored original is untouched.
	again := e.upload(t, alice, "x.docx", fixture(t, "contract.docx"))
	_ = again
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestRedlineRejectsWhatItCannotRevise(t *testing.T) {
	e := newTestEnv(t)
	alice, bob := token(t, "alice"), token(t, "bob")
	pdf := e.upload(t, alice, "a.pdf", fixture(t, "contract.pdf"))
	txt := e.upload(t, alice, "a.txt", []byte("纯文本合同"))
	docx := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))

	cases := []struct {
		name   string
		bearer string
		body   map[string]any
		status int
		code   string
	}{
		{"pdf", alice, map[string]any{"file_id": pdf.FileID}, 422, "NOT_DOCX"},
		{"txt", alice, map[string]any{"file_id": txt.FileID}, 422, "NOT_DOCX"},
		{"unknown file", alice, map[string]any{"file_id": "nope"}, 422, "FILE_UNAVAILABLE"},
		{"someone else's file", bob, map[string]any{"file_id": docx.FileID}, 422, "FILE_UNAVAILABLE"},
		{"invalid model", alice, map[string]any{"file_id": docx.FileID, "model": "gpt-9"}, 400, "INVALID_MODEL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.body)
			rec := e.do("POST", "/api/redline", tc.bearer, strings.NewReader(string(raw)), "application/json")
			if rec.Code != tc.status || decodeErr(t, rec).Code != tc.code {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
		})
	}
	if got := e.llm.models(); len(got) != 0 {
		t.Errorf("the model must not be called for a request that cannot succeed: %v", got)
	}
	if rec := e.do("POST", "/api/redline", "", strings.NewReader(`{"file_id":"x"}`), "application/json"); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", rec.Code)
	}
}

func TestRedlineModelFailuresAreReportedClearly(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))

	e.llm.setGenerate("kimi-k3", func([]*schema.Message) (string, error) { return "", quotaErr() })
	rec := e.do("POST", "/api/redline", alice, strings.NewReader(`{"file_id":"`+up.FileID+`","model":"kimi-k3"}`), "application/json")
	if rec.Code != http.StatusPaymentRequired || decodeErr(t, rec).Code != "QUOTA_EXHAUSTED" {
		t.Errorf("quota: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), upstreamSecret) {
		t.Error("upstream detail leaked")
	}

	e.llm.setGenerate("kimi-k3", scripted("抱歉，我无法完成这个任务"))
	rec = e.do("POST", "/api/redline", alice, strings.NewReader(`{"file_id":"`+up.FileID+`","model":"kimi-k3"}`), "application/json")
	if rec.Code != http.StatusBadGateway || decodeErr(t, rec).Code != "REDLINE_PLAN_INVALID" {
		t.Errorf("garbage: %d %s", rec.Code, rec.Body)
	}
}

func TestRedlineWithNothingToChangeReturnsNoFile(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	e.llm.setGenerate("kimi-k3", scripted(`{"summary":"合同整体对您有利，无需修改","edits":[],"insertions":[]}`))
	code, out, raw := e.redline(t, alice, map[string]any{"file_id": up.FileID, "model": "kimi-k3"})
	if code != 200 || out.DocxBase64 != "" || len(out.Applied) != 0 || !strings.Contains(out.Summary, "无需修改") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestRedlineCountsAgainstTheUsersRateLimit(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.ChatRatePerMinute = 1 })
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	e.llm.setGenerate("kimi-k3", scripted(`{"summary":"x","edits":[]}`))
	body := `{"file_id":"` + up.FileID + `","model":"kimi-k3"}`
	if rec := e.do("POST", "/api/redline", alice, strings.NewReader(body), "application/json"); rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	rec := e.do("POST", "/api/redline", alice, strings.NewReader(body), "application/json")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second call: %d", rec.Code)
	}
}

func TestGetFileMetadata(t *testing.T) {
	e := newTestEnv(t)
	alice, bob := token(t, "alice"), token(t, "bob")
	up := e.upload(t, alice, "合同.docx", fixture(t, "contract.docx"))
	rec := e.do("GET", "/api/files/"+up.FileID, alice, nil, "")
	var meta struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Chars    int    `json:"chars"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &meta)
	if rec.Code != 200 || meta.Filename != "合同.docx" || meta.Chars < 100 || meta.ID != up.FileID {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := e.do("GET", "/api/files/"+up.FileID, bob, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another user's file: %d", rec.Code)
	}
	if rec := e.do("GET", "/api/files/"+up.FileID, "", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", rec.Code)
	}
}
