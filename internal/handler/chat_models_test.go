package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	goopenai "github.com/meguminnnnnnnnn/go-openai"

	"law-assistant/internal/model"
	"law-assistant/internal/store"
)

// ---- a scriptable stand-in for the upstream models ------------------------

type streamPlan func() (*schema.StreamReader[*schema.Message], error)

type fakeLLM struct {
	mu   sync.Mutex
	used []string            // model id of every call, in order
	seen [][]*schema.Message // messages each call received
	plan map[string]streamPlan
	gen  map[string]func([]*schema.Message) (string, error) // non-streaming replies
}

func newFakeLLM() *fakeLLM {
	return &fakeLLM{plan: map[string]streamPlan{}, gen: map[string]func([]*schema.Message) (string, error){}}
}

func (f *fakeLLM) setGenerate(modelID string, fn func([]*schema.Message) (string, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen[modelID] = fn
}

func (f *fakeLLM) factory(_ context.Context, id string) (einomodel.BaseChatModel, error) {
	return &fakeChatModel{llm: f, id: id}, nil
}

func (f *fakeLLM) setPlan(modelID string, p streamPlan) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plan[modelID] = p
}

func (f *fakeLLM) models() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.used...)
}

type fakeChatModel struct {
	llm *fakeLLM
	id  string
}

func (m *fakeChatModel) Generate(_ context.Context, in []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.llm.mu.Lock()
	m.llm.used = append(m.llm.used, m.id)
	m.llm.seen = append(m.llm.seen, append([]*schema.Message(nil), in...))
	fn := m.llm.gen[m.id]
	m.llm.mu.Unlock()
	if fn == nil {
		return nil, errors.New("no scripted Generate reply")
	}
	text, err := fn(in)
	if err != nil {
		return nil, err
	}
	return schema.AssistantMessage(text, nil), nil
}

func (m *fakeChatModel) Stream(_ context.Context, in []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	m.llm.mu.Lock()
	m.llm.used = append(m.llm.used, m.id)
	m.llm.seen = append(m.llm.seen, append([]*schema.Message(nil), in...))
	plan := m.llm.plan[m.id]
	m.llm.mu.Unlock()
	if plan != nil {
		return plan()
	}
	return okStream("你好", "，世界")()
}

func okStream(tokens ...string) streamPlan {
	return func() (*schema.StreamReader[*schema.Message], error) {
		msgs := make([]*schema.Message, len(tokens))
		for i, tok := range tokens {
			msgs[i] = schema.AssistantMessage(tok, nil)
		}
		return schema.StreamReaderFromArray(msgs), nil
	}
}

func failOnCreate(err error) streamPlan {
	return func() (*schema.StreamReader[*schema.Message], error) { return nil, err }
}

// failAfter streams tokens and then fails, like a provider that errors mid-stream.
func failAfter(err error, tokens ...string) streamPlan {
	return func() (*schema.StreamReader[*schema.Message], error) {
		sr, sw := schema.Pipe[*schema.Message](len(tokens) + 1)
		for _, tok := range tokens {
			sw.Send(schema.AssistantMessage(tok, nil), nil)
		}
		sw.Send(nil, err)
		sw.Close()
		return sr, nil
	}
}

const upstreamSecret = "secret-detail-xyz"

// quotaErr is DashScope's "free tier used up" failure as the eino client reports it.
func quotaErr() error {
	return fmt.Errorf("failed to create chat completion: %w", &goopenai.APIError{
		HTTPStatusCode: http.StatusForbidden, HTTPStatus: "403 Forbidden",
		Code: "AllocationQuota.FreeTierOnly", Type: "AllocationQuota.FreeTierOnly",
		Message: "The free tier of the model has been exhausted. " + upstreamSecret,
	})
}

func apiErr(status int, msg string) error {
	return fmt.Errorf("failed to create chat completion: %w", &goopenai.APIError{
		HTTPStatusCode: status, HTTPStatus: http.StatusText(status), Message: msg + " " + upstreamSecret,
	})
}

// ---- helpers ---------------------------------------------------------------

type sseEvent struct{ Name, Data string }

func parseSSE(body string) []sseEvent {
	var events []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.Name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.Data = strings.TrimPrefix(line, "data: ")
			}
		}
		if ev.Name != "" || ev.Data != "" {
			events = append(events, ev)
		}
	}
	return events
}

type errBody struct {
	Code    string `json:"code"`
	Model   string `json:"model"`
	Message string `json:"message"`
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errBody {
	t.Helper()
	var b errBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body is not the structured error JSON: %v\n%s", err, rec.Body)
	}
	return b
}

func (e *testEnv) chat(t *testing.T, bearer string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return e.do("POST", "/api/chat", bearer, strings.NewReader(string(raw)), "application/json")
}

func (e *testEnv) history(t *testing.T, user, sid string) []store.Message {
	t.Helper()
	sess, err := e.srv.sessionStore.Get(context.Background(), user, sid)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Messages
}

// ---- GET /api/models -------------------------------------------------------

func TestListModels(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do("GET", "/api/models", token(t, "alice"), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Models  []model.Info `json:"models"`
		Default string       `json:"default"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Default != "qwen3.8-max-0902" {
		t.Errorf("default = %q", out.Default)
	}
	want := model.DefaultIDs()
	if len(out.Models) != 12 || len(want) != 12 {
		t.Fatalf("got %d models, want 12", len(out.Models))
	}
	for i, m := range out.Models {
		if m.ID != want[i] {
			t.Errorf("models[%d] = %s, want %s (recommended order)", i, m.ID, want[i])
		}
		if m.Label == "" || m.Tier == "" {
			t.Errorf("%s is missing label/tier: %+v", m.ID, m)
		}
	}
	first := out.Models[0]
	if first.Label != "通义千问 3.8 Max（0902）" || first.Tier != "flagship" {
		t.Errorf("first model = %+v", first)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}

// ---- model selection -------------------------------------------------------

func TestChatUsesDefaultModelAndRecordsIt(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)

	rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "你好"})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if got := e.llm.models(); len(got) != 1 || got[0] != "qwen3.8-max-0902" {
		t.Errorf("models used = %v, want the default", got)
	}

	events := parseSSE(rec.Body.String())
	if len(events) != 3 || events[2].Name != "message" || !strings.Contains(events[2].Data, `"done"`) {
		t.Errorf("unexpected stream: %+v", events)
	}

	h := e.history(t, "alice", sid)
	if len(h) != 2 || h[0].Role != "user" || h[1].Role != "assistant" || h[1].Content != "你好，世界" {
		t.Fatalf("history = %+v", h)
	}
	if h[0].Model != "qwen3.8-max-0902" || h[1].Model != "qwen3.8-max-0902" {
		t.Errorf("the turn's model must be recorded on both messages: %q / %q", h[0].Model, h[1].Model)
	}
}

func TestChatUsesRequestedModel(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)

	if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "hi", "model": "kimi-k3"}); rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if got := e.llm.models(); len(got) != 1 || got[0] != "kimi-k3" {
		t.Errorf("models used = %v", got)
	}
	h := e.history(t, "alice", sid)
	if h[0].Model != "kimi-k3" || h[1].Model != "kimi-k3" {
		t.Errorf("recorded models = %q / %q", h[0].Model, h[1].Model)
	}
}

func TestChatCanSwitchModelBetweenTurns(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)
	for _, m := range []string{"glm-5.3", "qwen3.8-flash"} {
		if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "q", "model": m}); rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d", m, rec.Code)
		}
	}
	h := e.history(t, "alice", sid)
	if len(h) != 4 || h[0].Model != "glm-5.3" || h[2].Model != "qwen3.8-flash" {
		t.Errorf("history = %+v", h)
	}
}

func TestChatRejectsModelOutsideAllowList(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)

	for _, bad := range []string{"gpt-4", "QWEN3.8-MAX", " kimi-k3", "kimi-k3 ", "qwen-max"} {
		rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "hi", "model": bad})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("model %q: got %d, want 400", bad, rec.Code)
			continue
		}
		if b := decodeErr(t, rec); b.Code != "INVALID_MODEL" {
			t.Errorf("model %q: code = %q, want INVALID_MODEL", bad, b.Code)
		}
	}
	if got := e.llm.models(); len(got) != 0 {
		t.Errorf("an invalid model must never reach the provider, got calls: %v", got)
	}
	if h := e.history(t, "alice", sid); len(h) != 0 {
		t.Errorf("nothing may be persisted for a rejected request: %+v", h)
	}
}

func TestEveryModuleHonoursTheSelectedModel(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")

	for _, module := range []string{"consult", "pleading", "contract", "evidence_org", "evidence", "communication"} {
		sid := e.createSessionFor(t, alice, module)
		before := len(e.llm.models())
		rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "hi", "model": "deepseek-v4-pro-0813"})
		if rec.Code != http.StatusOK {
			t.Errorf("%s: got %d: %s", module, rec.Code, rec.Body)
			continue
		}
		used := e.llm.models()
		if len(used) != before+1 || used[len(used)-1] != "deepseek-v4-pro-0813" {
			t.Errorf("%s used %v, want deepseek-v4-pro-0813", module, used[before:])
		}
	}

	// Attached documents must not change which model serves the request.
	sid := e.createSessionFor(t, alice, "contract")
	up := e.upload(t, alice, "c.txt", []byte("保密条款"))
	rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "审查", "model": "glm-5.3", "file_ids": []string{up.FileID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("contract with files: got %d: %s", rec.Code, rec.Body)
	}
	if used := e.llm.models(); used[len(used)-1] != "glm-5.3" {
		t.Errorf("contract+document path ignored the model: %v", used)
	}
}

func TestHistoryAndSystemPromptReachTheModel(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)
	for _, q := range []string{"第一问", "第二问"} {
		if rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": q}); rec.Code != http.StatusOK {
			t.Fatalf("got %d", rec.Code)
		}
	}
	e.llm.mu.Lock()
	defer e.llm.mu.Unlock()
	var roles []string
	for _, m := range e.llm.seen[1] {
		roles = append(roles, string(m.Role))
	}
	if got := strings.Join(roles, ","); got != "system,user,assistant,user" {
		t.Errorf("second request roles = %s, want system,user,assistant,user", got)
	}
}

// ---- upstream failures -----------------------------------------------------

func TestChatClassifiesUpstreamFailures(t *testing.T) {
	cases := []struct {
		name       string
		plan       streamPlan
		wantStatus int
		wantCode   string
	}{
		{"free quota exhausted (403)", failOnCreate(quotaErr()), http.StatusPaymentRequired, "QUOTA_EXHAUSTED"},
		{"invalid API key (401)", failOnCreate(apiErr(401, "Incorrect API key provided")), http.StatusBadGateway, "INVALID_API_KEY"},
		{"rate limited (429)", failOnCreate(apiErr(429, "Requests too frequent")), http.StatusTooManyRequests, "RATE_LIMITED"},
		{"anything else", failOnCreate(apiErr(503, "overloaded")), http.StatusInternalServerError, "UPSTREAM_ERROR"},
		{"quota error delivered as the first stream chunk", failAfter(quotaErr()), http.StatusPaymentRequired, "QUOTA_EXHAUSTED"},
		{"rate limit delivered as the first stream chunk", failAfter(apiErr(429, "slow")), http.StatusTooManyRequests, "RATE_LIMITED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			alice := token(t, "alice")
			sid := e.createSession(t, alice)
			e.llm.setPlan("kimi-k3", tc.plan)

			rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "hi", "model": "kimi-k3"})
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("a rejected request must be a plain JSON error, got Content-Type %q", ct)
			}
			b := decodeErr(t, rec)
			if b.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", b.Code, tc.wantCode)
			}
			if tc.wantCode != "INVALID_API_KEY" && b.Model != "kimi-k3" {
				t.Errorf("model = %q, want kimi-k3", b.Model)
			}
			if b.Message == "" {
				t.Error("message must not be empty")
			}
			// Nothing from the upstream error may reach the client.
			for _, leak := range []string{upstreamSecret, "free tier of the model", "Incorrect API key", "overloaded"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("response leaks upstream detail %q: %s", leak, rec.Body)
				}
			}
			// A rejected attempt leaves no trace in the history.
			if h := e.history(t, "alice", sid); len(h) != 0 {
				t.Errorf("history must stay empty after a rejected request: %+v", h)
			}
		})
	}
}

func TestChatMidStreamFailureUsesSSEErrorEvent(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)
	e.llm.setPlan("kimi-k3", failAfter(quotaErr(), "部分", "回答"))

	rec := e.chat(t, alice, map[string]any{"session_id": sid, "message": "hi", "model": "kimi-k3"})
	if rec.Code != http.StatusOK {
		t.Fatalf("output had started, so the HTTP status stays 200; got %d", rec.Code)
	}
	events := parseSSE(rec.Body.String())
	if len(events) != 3 {
		t.Fatalf("want token, token, error; got %+v", events)
	}
	if events[2].Name != "error" {
		t.Fatalf("last event = %q, want error", events[2].Name)
	}
	var b errBody
	if err := json.Unmarshal([]byte(events[2].Data), &b); err != nil {
		t.Fatalf("error event is not the structured JSON: %v (%s)", err, events[2].Data)
	}
	if b.Code != "QUOTA_EXHAUSTED" || b.Model != "kimi-k3" {
		t.Errorf("error event = %+v", b)
	}
	for _, ev := range events {
		if ev.Name != "error" && strings.Contains(ev.Data, `"done"`) {
			t.Error("no done event may follow an error")
		}
		if strings.Contains(ev.Data, upstreamSecret) {
			t.Errorf("event leaks upstream detail: %s", ev.Data)
		}
	}

	// Output that was already produced is kept.
	h := e.history(t, "alice", sid)
	if len(h) != 2 || h[1].Role != "assistant" || h[1].Content != "部分回答" {
		t.Errorf("history = %+v, want the user turn and the partial reply", h)
	}
}

// After a quota failure the client retries the same message with another
// model: the history must contain that message exactly once.
func TestRetryWithAnotherModelDoesNotDuplicateTheMessage(t *testing.T) {
	e := newTestEnv(t)
	alice := token(t, "alice")
	sid := e.createSession(t, alice)
	e.llm.setPlan("qwen3.8-max-0902", failOnCreate(quotaErr()))

	first := e.chat(t, alice, map[string]any{"session_id": sid, "message": "劳动合同到期不续签有补偿吗"})
	if first.Code != http.StatusPaymentRequired {
		t.Fatalf("first attempt: got %d", first.Code)
	}
	second := e.chat(t, alice, map[string]any{"session_id": sid, "message": "劳动合同到期不续签有补偿吗", "model": "qwen3.8-max"})
	if second.Code != http.StatusOK {
		t.Fatalf("retry: got %d: %s", second.Code, second.Body)
	}

	h := e.history(t, "alice", sid)
	if len(h) != 2 || h[0].Role != "user" || h[1].Role != "assistant" {
		t.Fatalf("history = %+v, want exactly one user message and one reply", h)
	}
	if h[0].Model != "qwen3.8-max" {
		t.Errorf("user message model = %q", h[0].Model)
	}
	if used := e.llm.models(); len(used) != 2 || used[0] != "qwen3.8-max-0902" || used[1] != "qwen3.8-max" {
		t.Errorf("models used = %v", used)
	}
	// The retry sent the message once, not twice.
	e.llm.mu.Lock()
	defer e.llm.mu.Unlock()
	users := 0
	for _, m := range e.llm.seen[1] {
		if m.Role == schema.User {
			users++
		}
	}
	if users != 1 {
		t.Errorf("the retry sent %d user messages to the model, want 1", users)
	}
}
