package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"law-assistant/internal/config"
	"law-assistant/internal/store"
)

func TestMessageTooLongIsRejectedBeforeAnythingHappens(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.MaxMessageChars = 10 })
	alice := token(t, "alice")
	sid := e.createSession(t, alice)

	rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": strings.Repeat("法", 11)})
	if rec.Code != http.StatusRequestEntityTooLarge || decodeErr(t, rec).Code != "MESSAGE_TOO_LONG" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if got := e.llm.models(); len(got) != 0 {
		t.Errorf("model called: %v", got)
	}
	// Characters, not bytes: 10 CJK characters are fine.
	if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": strings.Repeat("法", 10)}); rec.Code != http.StatusOK {
		t.Errorf("10 chars refused: %d %s", rec.Code, rec.Body)
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	rec := e.chat(t, alice, map[string]any{"message": strings.Repeat("a", maxChatBodyBytes+1)})
	if rec.Code != http.StatusRequestEntityTooLarge || decodeErr(t, rec).Code != "MESSAGE_TOO_LONG" {
		t.Fatalf("got %d %.200s", rec.Code, rec.Body)
	}
}

func TestChatRateLimitIsPerUser(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.ChatRatePerMinute = 2 })
	alice, bob := token(t, "alice"), token(t, "bob")
	for i := 0; i < 2; i++ {
		if rec := e.chat(t, alice, map[string]any{"message": "hi"}); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	rec := e.chat(t, alice, map[string]any{"message": "hi"})
	if rec.Code != http.StatusTooManyRequests || decodeErr(t, rec).Code != "USER_RATE_LIMITED" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	if n := len(e.llm.models()); n != 2 {
		t.Errorf("limited request reached the model (%d calls)", n)
	}
	if rec := e.chat(t, bob, map[string]any{"message": "hi"}); rec.Code != http.StatusOK {
		t.Errorf("bob was limited by alice's traffic: %d", rec.Code)
	}
}

func TestUploadRateLimit(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.UploadRatePerMin = 1 })
	alice := token(t, "alice")
	e.upload(t, alice, "a.txt", []byte("内容"))
	rec := e.uploadRaw(t, alice, "b.txt", []byte("内容"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if n := storedFiles(t, e.dir); n != 1 {
		t.Errorf("limited upload was stored: %d files", n)
	}
}

func TestConcurrentStreamsArePerUserLimited(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.MaxConcurrentChats = 1 })
	alice, bob := token(t, "alice"), token(t, "bob")

	started, release := make(chan struct{}), make(chan struct{})
	e.llm.setPlan("kimi-k3", func() (*schema.StreamReader[*schema.Message], error) {
		sr, sw := schema.Pipe[*schema.Message](2)
		go func() {
			sw.Send(schema.AssistantMessage("a", nil), nil)
			close(started)
			<-release
			sw.Send(schema.AssistantMessage("b", nil), nil)
			sw.Close()
		}()
		return sr, nil
	})

	done := make(chan int)
	go func() {
		done <- e.chat(t, alice, map[string]any{"message": "slow", "model": "kimi-k3"}).Code
	}()
	<-started

	rec := e.chat(t, alice, map[string]any{"message": "second"})
	if rec.Code != http.StatusTooManyRequests || decodeErr(t, rec).Code != "TOO_MANY_STREAMS" {
		t.Errorf("second concurrent chat: %d %s", rec.Code, rec.Body)
	}
	if rec := e.chat(t, bob, map[string]any{"message": "hi"}); rec.Code != http.StatusOK {
		t.Errorf("bob blocked by alice's stream: %d", rec.Code)
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("first chat: %d", code)
	}
	if rec := e.chat(t, alice, map[string]any{"message": "third"}); rec.Code != http.StatusOK {
		t.Errorf("slot not released after the stream ended: %d %s", rec.Code, rec.Body)
	}
}

func msgs(pairs ...string) []store.Message {
	var out []store.Message
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, store.Message{Role: pairs[i], Content: pairs[i+1]})
	}
	return out
}

func contents(ms []*schema.Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, string(m.Role)+":"+m.Content)
	}
	return out
}

func TestHistoryWindow(t *testing.T) {
	h := msgs("user", "aaaa", "assistant", "bbbb", "user", "cc", "assistant", "dd", "system", "ignored")

	if got := contents(buildSchemaMessages(h, 0)); len(got) != 4 {
		t.Errorf("no limit should keep everything but system rows: %v", got)
	}
	// budget 6: dd(2)+cc(2)=4 fits, bbbb(4) does not -> window is [cc, dd]; starts with user.
	if got := strings.Join(contents(buildSchemaMessages(h, 6)), ","); got != "user:cc,assistant:dd" {
		t.Errorf("window = %s", got)
	}
	// budget 3: only dd fits, but a window may not start with an assistant reply -> empty.
	if got := buildSchemaMessages(h, 3); len(got) != 0 {
		t.Errorf("window must not start with assistant: %v", contents(got))
	}
	// CJK counted in characters, not bytes.
	cjk := msgs("user", "合同合同合同", "assistant", "好")
	if got := buildSchemaMessages(cjk, 7); len(got) != 2 {
		t.Errorf("7 chars fit: %v", contents(got))
	}
}

func TestLongHistoryIsTrimmedBeforeTheModel(t *testing.T) {
	e := newTestEnvCfg(t, func(c *config.Config) { c.MaxHistoryChars = 20 })
	alice := token(t, "alice")
	sid := e.createSession(t, alice)
	for _, q := range []string{strings.Repeat("甲", 15), strings.Repeat("乙", 15), "最后一问"} {
		if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": q}); rec.Code != http.StatusOK {
			t.Fatalf("got %d %s", rec.Code, rec.Body)
		}
	}
	e.llm.mu.Lock()
	defer e.llm.mu.Unlock()
	last := e.llm.seen[len(e.llm.seen)-1]
	var all strings.Builder
	for _, m := range last {
		all.WriteString(m.Content)
	}
	if strings.Contains(all.String(), "甲") {
		t.Error("oldest turn should have been dropped from the model context")
	}
	if !strings.Contains(all.String(), "最后一问") {
		t.Error("the new question must always be sent")
	}
	// ...but the stored conversation is untouched.
	if h := e.history(t, "alice", sid); len(h) != 6 {
		t.Errorf("history rows = %d, want 6", len(h))
	}
}

// ---- documents stay in context for the whole conversation -------------------

func TestDocumentStaysInContextOnFollowUps(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "contract.docx", fixture(t, "contract.docx"))
	sid := e.createSessionFor(t, alice, "contract")

	for i, body := range []map[string]any{
		{"session_id": sid, "message": "审查", "file_ids": []string{up.FileID}},
		{"session_id": sid, "message": "你直接改好了吗"}, // no file attached this turn
	} {
		if rec := e.chat(t, alice, body); rec.Code != http.StatusOK {
			t.Fatalf("turn %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	e.llm.mu.Lock()
	defer e.llm.mu.Unlock()
	followUp := e.llm.seen[1]
	hits := 0
	for _, m := range followUp {
		if strings.Contains(m.Content, "违约金人民币十万元") {
			hits++
			if m.Role != "user" {
				t.Errorf("document role %q", m.Role)
			}
		}
	}
	if hits != 1 {
		t.Fatalf("follow-up request carried the document %d times, want exactly once", hits)
	}
	if last := followUp[len(followUp)-1]; last.Content != "你直接改好了吗" {
		t.Errorf("the question must stay last, got %q", last.Content)
	}
}

func TestEarlierUnreadableFileDoesNotBlockFollowUps(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	up := e.upload(t, alice, "a.txt", []byte("第一份文件"))
	sid := e.createSessionFor(t, alice, "contract")
	if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "看", "file_ids": []string{up.FileID}}); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if err := e.files.Delete(context.Background(), "alice", up.FileID); err != nil {
		t.Fatal(err)
	}
	if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "继续"}); rec.Code != http.StatusOK {
		t.Fatalf("a follow-up must not fail because an earlier file is gone: %d %s", rec.Code, rec.Body)
	}
}

func TestSeveralDocumentsAreAllSentOldestFirst(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	a := e.upload(t, alice, "a.txt", []byte("甲文件内容"))
	b := e.upload(t, alice, "b.txt", []byte("乙文件内容"))
	sid := e.createSessionFor(t, alice, "contract")
	e.chat(t, alice, map[string]any{"session_id": sid, "message": "1", "file_ids": []string{a.FileID}})
	e.chat(t, alice, map[string]any{"session_id": sid, "message": "2", "file_ids": []string{b.FileID}})
	e.llm.mu.Lock()
	defer e.llm.mu.Unlock()
	var doc string
	for _, m := range e.llm.seen[1] {
		if strings.Contains(m.Content, "<document") {
			doc = m.Content
		}
	}
	ia, ib := strings.Index(doc, "甲文件内容"), strings.Index(doc, "乙文件内容")
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("both documents expected, oldest first: %q", doc)
	}
}

func TestLimitDocuments(t *testing.T) {
	docs := []string{"aaaa", "bbbb", "cc"}
	if got := strings.Join(limitDocuments(docs, 0), ","); got != "aaaa,bbbb,cc" {
		t.Errorf("no limit: %s", got)
	}
	if got := strings.Join(limitDocuments(docs, 6), ","); got != "bbbb,cc" {
		t.Errorf("newest that fit: %s", got)
	}
	if got := strings.Join(limitDocuments([]string{"aaaa", "bbbbbbbb"}, 5), ","); got != "bbbbb" {
		t.Errorf("the newest document is always kept, cut to fit: %s", got)
	}
}

// syncRecorder lets a test read the body while the handler is still writing it.
type syncRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *syncRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func (r *syncRecorder) Flush() {}

func (r *syncRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Body.String()
}

// ---- stopping a reply -------------------------------------------------------

func TestStoppedReplyIsSavedWithTheMarker(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)

	release := make(chan struct{})
	e.llm.setPlan("kimi-k3", func() (*schema.StreamReader[*schema.Message], error) {
		sr, sw := schema.Pipe[*schema.Message](4)
		sw.Send(schema.AssistantMessage("回答到一半", nil), nil)
		go func() {
			<-release
			sw.Send(nil, context.Canceled) // what the provider client reports when the request is cancelled
			sw.Close()
		}()
		return sr, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	raw := `{"session_id":"` + sid + `","message":"长问题","model":"kimi-k3"}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(raw)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+alice)
	req.Header.Set("Content-Type", "application/json")
	rec := &syncRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() { e.h.ServeHTTP(rec, req); close(done) }()

	// Wait until the first token went out, then the user presses Stop.
	for i := 0; i < 200 && !strings.Contains(rec.text(), "回答到一半"); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	close(release)
	<-done

	h := e.history(t, "alice", sid)
	if len(h) != 2 || h[1].Role != "assistant" || h[1].Content != "回答到一半"+StoppedMarker {
		t.Fatalf("history = %+v", h)
	}
	if strings.Contains(rec.text(), "event: error") {
		t.Errorf("a client-side stop must not be reported as a model error:\n%s", rec.Body)
	}
}

func TestUpstreamErrorStillReportedWhenTheClientStaysConnected(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)
	e.llm.setPlan("kimi-k3", failAfter(quotaErr(), "部分"))
	rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "q", "model": "kimi-k3"})
	if !strings.Contains(rec.Body.String(), "event: error") {
		t.Errorf("expected an SSE error event:\n%s", rec.Body)
	}
	for _, m := range e.history(t, "alice", sid) {
		if strings.Contains(m.Content, StoppedMarker) {
			t.Errorf("an upstream failure must not be labelled as stopped by the user: %+v", m)
		}
	}
}
