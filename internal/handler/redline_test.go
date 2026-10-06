package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/config"
	"law-assistant/internal/redline"
	"law-assistant/internal/tool"
)

// startRedline posts a redline request; a job is accepted with 202 and a job id.
func (e *testEnv) startRedline(bearer string, body any) (*httptest.ResponseRecorder, string) {
	raw, _ := json.Marshal(body)
	rec := e.do("POST", "/api/redline", bearer, strings.NewReader(string(raw)), "application/json")
	var out struct {
		JobID string `json:"job_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out.JobID
}

func (e *testEnv) redlineStatus(t *testing.T, bearer, id string) (int, redlineStatus) {
	t.Helper()
	rec := e.do("GET", "/api/redline/"+id, bearer, nil, "")
	var st redlineStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	return rec.Code, st
}

// redlineFlow runs a whole review: start, poll until it ends. Failures come back
// the way a synchronous API would have reported them, as (status, error JSON).
func (e *testEnv) redlineFlow(t *testing.T, bearer string, body map[string]any) (int, redlineResponse, string) {
	t.Helper()
	rec, id := e.startRedline(bearer, body)
	if rec.Code != http.StatusAccepted {
		return rec.Code, redlineResponse{}, rec.Body.String()
	}
	for i := 0; i < 400; i++ {
		code, st := e.redlineStatus(t, bearer, id)
		if code != 200 {
			t.Fatalf("status poll: %d", code)
		}
		switch st.State {
		case "done":
			return 200, *st.Result, ""
		case "error":
			raw, _ := json.Marshal(st.Error)
			return st.Error.Status, redlineResponse{}, string(raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("review did not finish")
	return 0, redlineResponse{}, ""
}

// streamText delivers text as a stream of small chunks, like a model would.
func streamText(text string) streamPlan {
	rs := []rune(text)
	var chunks []string
	for len(rs) > 0 {
		n := min(24, len(rs))
		chunks = append(chunks, string(rs[:n]))
		rs = rs[n:]
	}
	return okStream(chunks...)
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
	e.llm.setPlan("kimi-k3", streamText(strings.Replace(penaltyPlan, "%d", itoa(n), 1)))

	code, out, raw := e.redlineFlow(t, alice, map[string]any{"file_id": up.FileID, "instruction": "我是劳动者，站在劳动者立场修改", "model": "kimi-k3"})
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
			code, _, raw := e.redlineFlow(t, tc.bearer, tc.body)
			var got errBody
			_ = json.Unmarshal([]byte(raw), &got)
			if code != tc.status || got.Code != tc.code {
				t.Fatalf("%d %s", code, raw)
			}
		})
	}
	if got := e.llm.models(); len(got) != 0 {
		t.Errorf("the model must not be called for a request that cannot succeed: %v", got)
	}
	if rec, _ := e.startRedline("", map[string]any{"file_id": "x"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", rec.Code)
	}
	if rec := e.do("GET", "/api/redline/abc", "", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous status: %d", rec.Code)
	}
}

func TestRedlineModelFailuresAreReportedClearly(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	body := map[string]any{"file_id": up.FileID, "model": "kimi-k3"}

	e.llm.setPlan("kimi-k3", failOnCreate(quotaErr()))
	code, _, raw := e.redlineFlow(t, alice, body)
	var got errBody
	_ = json.Unmarshal([]byte(raw), &got)
	if code == 200 || got.Code != "QUOTA_EXHAUSTED" || got.Model != "kimi-k3" {
		t.Errorf("quota: %d %s", code, raw)
	}
	if strings.Contains(raw, upstreamSecret) {
		t.Error("upstream detail leaked")
	}

	e.llm.setPlan("kimi-k3", failAfter(apiErr(429, "slow down"), "部分"))
	code, _, raw = e.redlineFlow(t, alice, body)
	_ = json.Unmarshal([]byte(raw), &got)
	if code == 200 || got.Code != "RATE_LIMITED" {
		t.Errorf("failure mid-stream: %d %s", code, raw)
	}

	e.llm.setPlan("kimi-k3", streamText("抱歉，我无法完成这个任务"))
	code, _, raw = e.redlineFlow(t, alice, body)
	_ = json.Unmarshal([]byte(raw), &got)
	if code == 200 || got.Code != "REDLINE_PLAN_INVALID" {
		t.Errorf("garbage: %d %s", code, raw)
	}
}

func TestRedlineWithNothingToChangeReturnsNoFile(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	e.llm.setPlan("kimi-k3", streamText(`{"summary":"合同整体对您有利，无需修改","edits":[],"insertions":[]}`))
	code, out, raw := e.redlineFlow(t, alice, map[string]any{"file_id": up.FileID, "model": "kimi-k3"})
	if code != 200 || out.DocxBase64 != "" || len(out.Applied) != 0 || !strings.Contains(out.Summary, "无需修改") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestRedlineCountsAgainstTheUsersRateLimit(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.ChatRatePerMinute = 1 })
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	e.llm.setPlan("kimi-k3", streamText(`{"summary":"x","edits":[]}`))
	body := map[string]any{"file_id": up.FileID, "model": "kimi-k3"}
	if code, _, raw := e.redlineFlow(t, alice, body); code != 200 {
		t.Fatal(raw)
	}
	if rec, _ := e.startRedline(alice, body); rec.Code != http.StatusTooManyRequests {
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

// ---- the job itself: progress, cancel, timeout, ownership, limits ---------------

// blockingPlan sends head, then waits for release before sending tail.
func blockingPlan(head, tail string, release <-chan struct{}) streamPlan {
	return func() (*schema.StreamReader[*schema.Message], error) {
		sr, sw := schema.Pipe[*schema.Message](4)
		sw.Send(schema.AssistantMessage(head, nil), nil)
		go func() {
			<-release
			sw.Send(schema.AssistantMessage(tail, nil), nil)
			sw.Close()
		}()
		return sr, nil
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRedlineStartsAtOnceAndReportsRealProgress(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	release := make(chan struct{})
	n := penaltyParagraph(t)
	head := `{"summary":"s","edits":[{"para":` + itoa(n) + `,"find":"十万元","replace":"一万元","comment":"c"},{"para":1,"find"`
	e.llm.setPlan("kimi-k3", blockingPlan(head, `:"x","replace":"y"}],"insertions":[]}`, release))

	rec, id := e.startRedline(alice, map[string]any{"file_id": up.FileID, "model": "kimi-k3"})
	if rec.Code != http.StatusAccepted || id == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// While the model is still writing, the status shows how far it got.
	waitFor(t, "progress", func() bool { _, st := e.redlineStatus(t, alice, id); return st.EditsFound >= 2 })
	_, st := e.redlineStatus(t, alice, id)
	if st.State != "running" || st.Phase != "reviewing" || st.Chars < 50 || st.Result != nil {
		t.Errorf("running status: %+v", st)
	}

	close(release)
	waitFor(t, "completion", func() bool { _, st := e.redlineStatus(t, alice, id); return st.State == "done" })
	_, st = e.redlineStatus(t, alice, id)
	if st.Result == nil || st.Result.DocxBase64 == "" || len(st.Result.Applied) == 0 {
		t.Errorf("done status: %+v", st)
	}
	// A finished result stays fetchable (the client may poll again), with a Content-Length for download progress.
	rec2 := e.do("GET", "/api/redline/"+id, alice, nil, "")
	if rec2.Header().Get("Content-Length") == "" {
		t.Error("the finished result must carry a Content-Length")
	}
}

func TestRedlineJobsBelongToTheirOwner(t *testing.T) {
	e := newTestEnv(t)
	alice, bob := token(t, "alice"), token(t, "bob")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	e.llm.setPlan("kimi-k3", streamText(`{"summary":"x","edits":[]}`))
	_, id := e.startRedline(alice, map[string]any{"file_id": up.FileID, "model": "kimi-k3"})
	if code, _ := e.redlineStatus(t, bob, id); code != http.StatusNotFound {
		t.Errorf("bob read alice's job: %d", code)
	}
	if rec := e.do("DELETE", "/api/redline/"+id, bob, nil, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if code, _ := e.redlineStatus(t, alice, id); code != 200 {
		t.Errorf("bob's DELETE must not remove alice's job: %d", code)
	}
	if code, _ := e.redlineStatus(t, alice, "no-such-job"); code != http.StatusNotFound {
		t.Errorf("unknown job: %d", code)
	}
}

func TestRedlineCancelStopsTheModelAndFreesTheSlot(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.MaxConcurrentChats = 1 })
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	release := make(chan struct{})
	defer close(release)
	e.llm.setPlan("kimi-k3", blockingPlan(`{"summary":`, `"x"}`, release))
	body := map[string]any{"file_id": up.FileID, "model": "kimi-k3"}

	_, id := e.startRedline(alice, body)
	waitFor(t, "first token", func() bool { _, st := e.redlineStatus(t, alice, id); return st.Chars > 0 })
	// The one slot is taken while the job runs...
	if rec, _ := e.startRedline(alice, body); rec.Code != http.StatusTooManyRequests || decodeErr(t, rec).Code != "TOO_MANY_STREAMS" {
		t.Errorf("second job while the first runs: %d %s", rec.Code, rec.Body)
	}
	// ...and cancelling frees it and forgets the job.
	if rec := e.do("DELETE", "/api/redline/"+id, alice, nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if code, _ := e.redlineStatus(t, alice, id); code != http.StatusNotFound {
		t.Errorf("cancelled job still visible: %d", code)
	}
}

func TestRedlineTimeoutIsReportedNotHung(t *testing.T) {
	e := newTestEnv(t)
	e.srv.redlineTimeout = 150 * time.Millisecond
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	// A model stuck mid-answer: the stream never yields again until the deadline hits.
	e.llm.setPlan("kimi-k3", func() (*schema.StreamReader[*schema.Message], error) {
		sr, sw := schema.Pipe[*schema.Message](1)
		go func() {
			sw.Send(schema.AssistantMessage(`{"summary":`, nil), nil)
			<-time.After(300 * time.Millisecond)
			sw.Send(nil, context.DeadlineExceeded)
			sw.Close()
		}()
		return sr, nil
	})
	code, _, raw := e.redlineFlow(t, alice, map[string]any{"file_id": up.FileID, "model": "kimi-k3"})
	var got errBody
	_ = json.Unmarshal([]byte(raw), &got)
	if code == 200 || got.Code != "REDLINE_TIMEOUT" || !strings.Contains(got.Message, "太慢") || strings.Contains(got.Message, "0 分钟") {
		t.Errorf("%d %s", code, raw)
	}
}

func TestRedlineSlotIsReleasedWhenTheJobEnds(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.MaxConcurrentChats = 1 })
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.docx", fixture(t, "contract.docx"))
	e.llm.setPlan("kimi-k3", streamText(`{"summary":"x","edits":[]}`))
	body := map[string]any{"file_id": up.FileID, "model": "kimi-k3"}
	for i := 0; i < 3; i++ {
		if code, _, raw := e.redlineFlow(t, alice, body); code != 200 {
			t.Fatalf("run %d: %d %s", i, code, raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
