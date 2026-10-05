package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	userA   = "aaaaaaaa-0000-0000-0000-00000000000a"
	sessA   = "11111111-1111-1111-1111-111111111111"
	pubKey  = "sb_publishable_test"
	userJWT = "user.jwt.token"
)

type captured struct {
	method, path string
	query        map[string]string
	header       http.Header
	body         string
}

// fakeREST answers every request with the given status and body and records
// what it received.
func fakeREST(t *testing.T, status int, reply string) (*SupabaseStore, *captured, *atomic.Int32) {
	t.Helper()
	got := &captured{}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		*got = captured{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(b), query: map[string]string{}}
		for k, v := range r.URL.Query() {
			got.query[k] = v[0]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	st := NewSupabaseStore(srv.URL, pubKey, func(context.Context) (string, bool) { return userJWT, true })
	return st, got, &calls
}

func TestSupabase_AuthHeadersSeparateKeyFromUserToken(t *testing.T) {
	st, got, _ := fakeREST(t, 200, `[]`)
	if _, err := st.List(bg, userA); err != nil {
		t.Fatal(err)
	}
	// The publishable key identifies the app; ONLY the user's JWT may be in
	// Authorization (that is what makes Postgres apply the user's RLS).
	if got.header.Get("apikey") != pubKey {
		t.Errorf("apikey = %q, want the publishable key", got.header.Get("apikey"))
	}
	if got.header.Get("Authorization") != "Bearer "+userJWT {
		t.Errorf("Authorization = %q, want the user's bearer token", got.header.Get("Authorization"))
	}
}

func TestSupabase_NoTokenMeansNoRequest(t *testing.T) {
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Add(1) }))
	defer srv.Close()
	st := NewSupabaseStore(srv.URL, pubKey, func(context.Context) (string, bool) { return "", false })

	if _, err := st.List(bg, userA); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
	if hit.Load() != 0 {
		t.Fatal("a request was sent without a user token")
	}
}

func TestSupabase_Create(t *testing.T) {
	st, got, _ := fakeREST(t, 201, `[{"id":"`+sessA+`","module":"consult","title":"新建法律咨询","created_at":"2026-10-04T08:00:00.123456+00:00","updated_at":"2026-10-04T08:00:00.123456+00:00"}]`)
	sess, err := st.Create(bg, userA, ModuleConsult, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/rest/v1/chat_sessions" {
		t.Errorf("request = %s %s", got.method, got.path)
	}
	if got.header.Get("Prefer") != "return=representation" {
		t.Errorf("Prefer = %q", got.header.Get("Prefer"))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatal(err)
	}
	if body["module"] != "consult" || body["title"] != "新建法律咨询" {
		t.Errorf("body = %v (empty title must default)", body)
	}
	// user_id is never sent: the database defaults it to auth.uid().
	if _, sent := body["user_id"]; sent {
		t.Error("user_id must come from the JWT, not the request body")
	}
	if sess.ID != sessA || sess.UserID != userA || sess.Module != ModuleConsult || sess.CreatedAt.IsZero() {
		t.Errorf("session = %+v", sess)
	}
	if sess.Messages == nil {
		t.Error("Messages should be an empty slice, not nil, so JSON encodes []")
	}
}

func TestSupabase_GetScopesByUserAndParsesMessages(t *testing.T) {
	st, got, _ := fakeREST(t, 200, `[{"id":"`+sessA+`","module":"contract","title":"t","created_at":"2026-10-04T08:00:00+00:00","updated_at":"2026-10-04T08:01:00+00:00",
	  "chat_messages":[
	    {"id":"m1","role":"user","content":"hi","file_ids":["f1"],"created_at":"2026-10-04T08:00:01+00:00"},
	    {"id":"m2","role":"assistant","content":"yo","file_ids":[],"created_at":"2026-10-04T08:00:02+00:00"}]}]`)
	sess, err := st.Get(bg, userA, sessA)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "GET" || got.path != "/rest/v1/chat_sessions" {
		t.Errorf("request = %s %s", got.method, got.path)
	}
	if got.query["id"] != "eq."+sessA || got.query["user_id"] != "eq."+userA {
		t.Errorf("must filter on both id and user_id, got %v", got.query)
	}
	if got.query["chat_messages.order"] != "created_at.asc,id.asc" {
		t.Errorf("messages must be ordered chronologically, got %q", got.query["chat_messages.order"])
	}
	if len(sess.Messages) != 2 || sess.MessageCount != 2 {
		t.Fatalf("messages = %+v", sess.Messages)
	}
	if sess.Messages[0].FileIDs[0] != "f1" || sess.Messages[1].FileIDs != nil || sess.Messages[0].SessionID != sessA {
		t.Errorf("message mapping wrong: %+v", sess.Messages)
	}
}

func TestSupabase_GetMissingIsNotFound(t *testing.T) {
	st, _, _ := fakeREST(t, 200, `[]`)
	if _, err := st.Get(bg, userA, sessA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestSupabase_MalformedIDsNeverReachTheNetwork(t *testing.T) {
	st, _, calls := fakeREST(t, 200, `[]`)
	for _, bad := range []string{"", "not-a-uuid", sessA + "&user_id=neq.x", "eq.1 or 1=1"} {
		if _, err := st.Get(bg, userA, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", bad, err)
		}
		if err := st.Delete(bg, userA, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete(%q) = %v, want ErrNotFound", bad, err)
		}
		if _, err := st.AddMessage(bg, userA, bad, "user", "x", "", nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("AddMessage(%q) = %v, want ErrNotFound", bad, err)
		}
	}
	if _, err := st.Get(bg, "not-a-uuid", sessA); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad user id: %v", err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d requests sent for malformed ids; filter-injection guard failed", n)
	}
}

func TestSupabase_List(t *testing.T) {
	st, got, _ := fakeREST(t, 200, `[
	  {"id":"`+sessA+`","module":"consult","title":"a","created_at":"2026-10-04T08:00:00+00:00","updated_at":"2026-10-04T09:00:00+00:00","chat_messages":[{"count":4}]},
	  {"id":"22222222-2222-2222-2222-222222222222","module":"pleading","title":"b","created_at":"2026-10-04T07:00:00+00:00","updated_at":"2026-10-04T07:00:00+00:00","chat_messages":[{"count":0}]}]`)
	list, err := st.List(bg, userA)
	if err != nil {
		t.Fatal(err)
	}
	if got.query["user_id"] != "eq."+userA || got.query["order"] != "updated_at.desc,id.asc" {
		t.Errorf("query = %v", got.query)
	}
	if len(list) != 2 || list[0].MessageCount != 4 || list[1].MessageCount != 0 || list[0].Messages != nil {
		t.Errorf("list = %+v", list)
	}
}

func TestSupabase_Delete(t *testing.T) {
	st, got, _ := fakeREST(t, 200, `[{"id":"`+sessA+`"}]`)
	if err := st.Delete(bg, userA, sessA); err != nil {
		t.Fatal(err)
	}
	if got.method != "DELETE" || got.query["id"] != "eq."+sessA || got.query["user_id"] != "eq."+userA {
		t.Errorf("request = %s %v", got.method, got.query)
	}

	// Nothing deleted (not yours / not there) must read as not found, not success.
	st2, _, _ := fakeREST(t, 200, `[]`)
	if err := st2.Delete(bg, userA, sessA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestSupabase_AddMessage(t *testing.T) {
	st, got, _ := fakeREST(t, 201, `[{"id":"m1","role":"user","content":"你好","file_ids":[],"created_at":"2026-10-04T08:00:01+00:00"}]`)
	m, err := st.AddMessage(bg, userA, sessA, "user", "你好", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(got.body), &body)
	if body["session_id"] != sessA || body["role"] != "user" || body["content"] != "你好" {
		t.Errorf("body = %v", body)
	}
	if ids, ok := body["file_ids"].([]any); !ok || len(ids) != 0 {
		t.Errorf("file_ids must be sent as [] (column is NOT NULL), got %v", body["file_ids"])
	}
	if _, sent := body["user_id"]; sent {
		t.Error("user_id must come from the JWT, not the request body")
	}
	if m.ID != "m1" || m.SessionID != sessA || m.FileIDs != nil {
		t.Errorf("message = %+v", m)
	}
}

func TestSupabase_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"RLS violation (someone else's session)", 403, `{"code":"42501","message":"new row violates row-level security policy"}`, ErrNotFound},
		{"foreign key violation (no such session)", 409, `{"code":"23503","message":"violates foreign key"}`, ErrNotFound},
		{"malformed uuid", 400, `{"code":"22P02","message":"invalid input syntax for type uuid"}`, ErrNotFound},
		{"check violation", 400, `{"code":"23514","message":"violates check constraint"}`, ErrInvalidInput},
		{"expired JWT", 401, `{"code":"PGRST301","message":"JWT expired"}`, ErrUnauthorized},
		{"invalid JWT", 401, `{"code":"PGRST303","message":"JWT invalid"}`, ErrUnauthorized},
		{"401 without a body", 401, ``, ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, _, _ := fakeREST(t, tt.status, tt.body)
			_, err := st.AddMessage(bg, userA, sessA, "user", "x", "", nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}

	// Anything else is an opaque server error that is not mistaken for a sentinel.
	st, _, _ := fakeREST(t, 500, `{"code":"XX000","message":"boom"}`)
	_, err := st.AddMessage(bg, userA, sessA, "user", "x", "", nil)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v, want a generic error", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("detail should be kept for the server log: %v", err)
	}
}

func TestSupabase_AddMessageSendsModelOnlyWhenSet(t *testing.T) {
	reply := `[{"id":"m1","role":"user","content":"x","model":"qwen3.8-max","file_ids":[],"created_at":"2026-10-04T08:00:01+00:00"}]`

	st, got, _ := fakeREST(t, 201, reply)
	m, err := st.AddMessage(bg, userA, sessA, "user", "x", "qwen3.8-max", nil)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(got.body), &body)
	if body["model"] != "qwen3.8-max" {
		t.Errorf("body = %v, want model to be sent", body)
	}
	if m.Model != "qwen3.8-max" {
		t.Errorf("Model = %q", m.Model)
	}

	// No model: the column is left NULL rather than sent as "".
	st2, got2, _ := fakeREST(t, 201, `[{"id":"m2","role":"user","content":"x","model":null,"file_ids":[],"created_at":"2026-10-04T08:00:01+00:00"}]`)
	m2, err := st2.AddMessage(bg, userA, sessA, "user", "x", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var body2 map[string]any
	_ = json.Unmarshal([]byte(got2.body), &body2)
	if _, sent := body2["model"]; sent {
		t.Errorf("empty model must not be sent: %v", body2)
	}
	if m2.Model != "" {
		t.Errorf("a NULL model should read back as empty, got %q", m2.Model)
	}
}

func TestSupabase_GetReadsModelColumn(t *testing.T) {
	st, got, _ := fakeREST(t, 200, `[{"id":"`+sessA+`","module":"consult","title":"t","created_at":"2026-10-04T08:00:00+00:00","updated_at":"2026-10-04T08:00:00+00:00",
	  "chat_messages":[
	    {"id":"m1","role":"user","content":"hi","model":"kimi-k3","file_ids":[],"created_at":"2026-10-04T08:00:01+00:00"},
	    {"id":"m2","role":"assistant","content":"yo","model":null,"file_ids":[],"created_at":"2026-10-04T08:00:02+00:00"}]}]`)
	sess, err := st.Get(bg, userA, sessA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.query["select"], "model") {
		t.Errorf("select must request the model column: %q", got.query["select"])
	}
	if sess.Messages[0].Model != "kimi-k3" || sess.Messages[1].Model != "" {
		t.Errorf("models = %q, %q", sess.Messages[0].Model, sess.Messages[1].Model)
	}
}
