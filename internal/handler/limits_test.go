package handler

import (
	"net/http"
	"strings"
	"testing"

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
