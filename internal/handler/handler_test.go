package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"law-assistant/internal/auth"
	"law-assistant/internal/config"
	"law-assistant/internal/store"
)

const testSecret = "handler-test-secret-0123456789abcdef"

type testEnv struct {
	srv   *Server
	h     http.Handler
	files *store.FileStore
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	authn, err := auth.New(context.Background(), auth.Config{JWTSecret: testSecret})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{FrontendURL: "http://localhost:5173", MaxUploadSize: 1 << 20}
	files := store.NewFileStore(t.TempDir())
	// The agent manager is never reached by these tests: every chat case below
	// is rejected before an agent is selected.
	srv := NewServer(cfg, nil, store.NewSessionStore(), files, authn)
	return &testEnv{srv: srv, h: srv.SetupRoutes(), files: files}
}

func token(t *testing.T, user string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": user, "aud": "authenticated", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *testEnv) do(method, path, bearer string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) createSession(t *testing.T, bearer string) string {
	t.Helper()
	rec := e.do("POST", "/api/sessions", bearer, strings.NewReader(`{"module":"consult"}`), "application/json")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body)
	}
	var s struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || s.ID == "" {
		t.Fatalf("bad create response: %v %s", err, rec.Body)
	}
	return s.ID
}

func TestAPIRequiresAuthentication(t *testing.T) {
	e := newTestEnv(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/modules"},
		{"GET", "/api/sessions"},
		{"POST", "/api/sessions"},
		{"GET", "/api/sessions/some-id"},
		{"DELETE", "/api/sessions/some-id"},
		{"POST", "/api/chat"},
		{"POST", "/api/upload"},
	} {
		if rec := e.do(tc.method, tc.path, "", strings.NewReader("{}"), "application/json"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
		if rec := e.do(tc.method, tc.path, "garbage", strings.NewReader("{}"), "application/json"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with bad token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestHealthzIsPublic(t *testing.T) {
	e := newTestEnv(t)
	if rec := e.do("GET", "/healthz", "", nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}

func TestCORSPreflightPrecedesAuth(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do("OPTIONS", "/api/chat", "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("preflight got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("preflight must allow the Authorization header, got %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestSessionsAreIsolatedBetweenUsers(t *testing.T) {
	e := newTestEnv(t)
	alice, bob := token(t, "alice"), token(t, "bob")
	id := e.createSession(t, alice)

	if rec := e.do("GET", "/api/sessions/"+id, alice, nil, ""); rec.Code != http.StatusOK {
		t.Errorf("owner GET: got %d, want 200", rec.Code)
	}
	if rec := e.do("GET", "/api/sessions/"+id, bob, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("other user GET: got %d, want 404", rec.Code)
	}
	if rec := e.do("DELETE", "/api/sessions/"+id, bob, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("other user DELETE: got %d, want 404", rec.Code)
	}

	var list struct {
		Sessions []struct{ ID string } `json:"sessions"`
	}
	rec := e.do("GET", "/api/sessions", bob, nil, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Sessions) != 0 {
		t.Errorf("bob's list should be empty, got %s (err %v)", rec.Body, err)
	}
	rec = e.do("GET", "/api/sessions", alice, nil, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Sessions) != 1 {
		t.Errorf("alice's list should have 1 session, got %s (err %v)", rec.Body, err)
	}

	// Bob's delete attempt must not have removed alice's session.
	if rec := e.do("GET", "/api/sessions/"+id, alice, nil, ""); rec.Code != http.StatusOK {
		t.Errorf("alice's session vanished after bob's DELETE: %d", rec.Code)
	}
	if rec := e.do("DELETE", "/api/sessions/"+id, alice, nil, ""); rec.Code != http.StatusNoContent {
		t.Errorf("owner DELETE: got %d, want 204", rec.Code)
	}
}

func TestChatCannotUseAnotherUsersSession(t *testing.T) {
	e := newTestEnv(t)
	id := e.createSession(t, token(t, "alice"))

	body := `{"session_id":"` + id + `","module":"consult","message":"hi"}`
	rec := e.do("POST", "/api/chat", token(t, "bob"), strings.NewReader(body), "application/json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404 (%s)", rec.Code, rec.Body)
	}

	// The rejected message must not have been written into alice's history.
	msgs, err := e.srv.sessionStore.GetMessages("alice", id)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("alice's history was modified: %v, %v", msgs, err)
	}
}

func TestUploadedFilesAreOwnedByUploader(t *testing.T) {
	e := newTestEnv(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "contract.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("甲方与乙方的保密条款"))
	_ = mw.Close()

	rec := e.do("POST", "/api/upload", token(t, "alice"), &buf, mw.FormDataContentType())
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var up struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil || up.FileID == "" {
		t.Fatalf("bad upload response: %v %s", err, rec.Body)
	}

	if got := e.srv.getFileContents("alice", []string{up.FileID}); !strings.Contains(got, "保密条款") {
		t.Errorf("owner should read own file, got %q", got)
	}
	if got := e.srv.getFileContents("bob", []string{up.FileID}); strings.Contains(got, "保密条款") {
		t.Errorf("another user read alice's file: %q", got)
	}
}
