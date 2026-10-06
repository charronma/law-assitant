package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFileStore_RoundTripAndIsolation(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir)
	f, err := fs.Create(bg, "alice", NewFile{Filename: "Contract.DOCX", Size: 5, Text: "正文", Content: strings.NewReader("hello")})
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get(bg, "alice", f.ID)
	if err != nil || got.ExtractedText != "正文" || got.Filename != "Contract.DOCX" {
		t.Fatalf("owner Get: %+v, %v", got, err)
	}
	if _, err := fs.Get(bg, "bob", f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Get by other user: %v", err)
	}
	if err := fs.Delete(bg, "bob", f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Delete by other user: %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 || filepath.Ext(ents[0].Name()) != ".docx" {
		t.Errorf("stored files: %v", ents)
	}
	if err := fs.Delete(bg, "alice", f.ID); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("file left on disk after Delete: %v", ents)
	}
	if _, err := fs.Get(bg, "alice", f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Get after Delete: %v", err)
	}
}

func TestSafeExt(t *testing.T) {
	for in, want := range map[string]string{
		"a.docx": ".docx", "A.PDF": ".pdf", "noext": "", "a.": "", "x.t/../x": "",
		"合同.docx": ".docx", "a.verylongextension": ".verylon", "a.b c": ".bc",
	} {
		if got := safeExt(in); got != want {
			t.Errorf("safeExt(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- Supabase: a stateful fake of Storage + PostgREST ----------------------

type fakeSupabase struct {
	mu       sync.Mutex
	objects  map[string]string         // storage key -> body
	rows     map[string]map[string]any // file id -> row (incl. user_id)
	failRows bool                      // make the table insert fail
	log      []string
	headers  []http.Header
}

func newFakeSupabase(t *testing.T) (*SupabaseFileStore, *fakeSupabase) {
	t.Helper()
	f := &fakeSupabase{objects: map[string]string{}, rows: map[string]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewSupabaseFileStore(srv.URL, pubKey, func(context.Context) (string, bool) { return userJWT, true }), f
}

func (f *fakeSupabase) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, r.Method+" "+r.URL.Path)
	f.headers = append(f.headers, r.Header.Clone())
	body, _ := io.ReadAll(r.Body)
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}

	if key, ok := strings.CutPrefix(r.URL.Path, "/storage/v1/object/authenticated/uploads/"); ok && r.Method == http.MethodGet {
		if data, found := f.objects[key]; found {
			_, _ = w.Write([]byte(data))
		} else {
			reply(404, map[string]string{"error": "not_found"})
		}
		return
	}
	if key, ok := strings.CutPrefix(r.URL.Path, "/storage/v1/object/uploads/"); ok {
		switch r.Method {
		case http.MethodPost:
			f.objects[key] = string(body)
			reply(200, map[string]string{"Key": "uploads/" + key})
		case http.MethodDelete:
			delete(f.objects, key)
			reply(200, map[string]string{"message": "Successfully deleted"})
		}
		return
	}

	if r.URL.Path != "/rest/v1/uploaded_files" {
		reply(404, map[string]string{"message": "no route"})
		return
	}
	q := r.URL.Query()
	match := func(row map[string]any) bool {
		return "eq."+row["id"].(string) == q.Get("id") && "eq."+row["user_id"].(string) == q.Get("user_id")
	}
	switch r.Method {
	case http.MethodPost:
		if f.failRows {
			reply(500, map[string]string{"code": "XX000", "message": "boom"})
			return
		}
		var row map[string]any
		_ = json.Unmarshal(body, &row)
		row["user_id"] = userA // the column default: auth.uid()
		row["created_at"] = "2026-10-05T00:00:00Z"
		f.rows[row["id"].(string)] = row
		reply(201, []map[string]any{row})
	case http.MethodGet:
		out := []map[string]any{}
		for _, row := range f.rows {
			if match(row) {
				out = append(out, row)
			}
		}
		reply(200, out)
	case http.MethodDelete:
		out := []map[string]any{}
		for id, row := range f.rows {
			if match(row) {
				out = append(out, row)
				delete(f.rows, id)
			}
		}
		reply(200, out)
	}
}

func TestSupabaseFiles_CreateGetDelete(t *testing.T) {
	st, fake := newFakeSupabase(t)
	f, err := st.Create(bg, userA, NewFile{
		SessionID: sessA, Filename: "合同 (终).DOCX", ContentType: "application/x", Size: 5,
		Text: "全文", Content: strings.NewReader("bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ExtractedText != "全文" || f.SessionID != sessA || f.Filename != "合同 (终).DOCX" {
		t.Errorf("created: %+v", f)
	}

	// The object is stored under the user's own folder; the key is ASCII-safe.
	if len(fake.objects) != 1 {
		t.Fatalf("objects: %v", fake.objects)
	}
	for key, body := range fake.objects {
		if !strings.HasPrefix(key, userA+"/"+f.ID) || !strings.HasSuffix(key, ".docx") || body != "bytes" {
			t.Errorf("object %q = %q", key, body)
		}
	}
	// Every call carries the user's token plus the publishable key.
	for i, h := range fake.headers {
		if h.Get("Authorization") != "Bearer "+userJWT || h.Get("Apikey") != pubKey {
			t.Errorf("request %d headers: %v", i, h)
		}
	}

	got, err := st.Get(bg, userA, f.ID)
	if err != nil || got.ExtractedText != "全文" {
		t.Fatalf("Get: %+v, %v", got, err)
	}
	// Another user cannot read it (the query always filters on user_id).
	other := "bbbbbbbb-0000-0000-0000-00000000000b"
	if _, err := st.Get(bg, other, f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Get by other user: %v", err)
	}
	if err := st.Delete(bg, other, f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Delete by other user: %v", err)
	}
	if len(fake.objects) != 1 {
		t.Error("another user's Delete removed the object")
	}

	if err := st.Delete(bg, userA, f.ID); err != nil {
		t.Fatal(err)
	}
	if len(fake.objects) != 0 || len(fake.rows) != 0 {
		t.Errorf("after Delete: objects=%v rows=%v", fake.objects, fake.rows)
	}
	if _, err := st.Get(bg, userA, f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Get after Delete: %v", err)
	}
}

func TestSupabaseFiles_FailedInsertRemovesTheObject(t *testing.T) {
	st, fake := newFakeSupabase(t)
	fake.failRows = true
	if _, err := st.Create(bg, userA, NewFile{Filename: "a.txt", Size: 1, Text: "x", Content: strings.NewReader("x")}); err == nil {
		t.Fatal("expected an error")
	}
	if len(fake.objects) != 0 {
		t.Errorf("orphaned object left in Storage: %v", fake.objects)
	}
}

func TestSupabaseFiles_OmitsInvalidSessionAndRejectsBadIDs(t *testing.T) {
	st, fake := newFakeSupabase(t)
	f, err := st.Create(bg, userA, NewFile{SessionID: "not-a-uuid", Filename: "a.txt", Size: 1, Text: "x", Content: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	if f.SessionID != "" {
		t.Errorf("invalid session id must be dropped, got %q", f.SessionID)
	}
	if _, ok := fake.rows[f.ID]["session_id"]; ok {
		t.Error("session_id sent for an invalid id")
	}

	n := len(fake.log)
	if _, err := st.Get(bg, userA, "../../etc/passwd"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("bad id: %v", err)
	}
	if _, err := st.Create(bg, "../x", NewFile{Filename: "a.txt", Content: strings.NewReader("x")}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bad user id: %v", err)
	}
	if len(fake.log) != n {
		t.Error("malformed ids reached the network")
	}
}

func TestSupabaseFiles_NoTokenMeansNoRequest(t *testing.T) {
	st, fake := newFakeSupabase(t)
	st.token = func(context.Context) (string, bool) { return "", false }
	st.rows.token = st.token
	if _, err := st.Create(bg, userA, NewFile{Filename: "a.txt", Content: strings.NewReader("x")}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("got %v", err)
	}
	if len(fake.log) != 0 {
		t.Errorf("unauthenticated call reached Supabase: %v", fake.log)
	}
}

func TestSupabaseFiles_OpenReadsTheOriginalBack(t *testing.T) {
	st, fake := newFakeSupabase(t)
	f, err := st.Create(bg, userA, NewFile{Filename: "合同.docx", Size: 9, Text: "x", Content: strings.NewReader("ORIGINAL!")})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := st.Open(bg, userA, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "ORIGINAL!" {
		t.Errorf("got %q", got)
	}
	// Reads use the authenticated route with the user's token.
	last := fake.headers[len(fake.headers)-1]
	if last.Get("Authorization") != "Bearer "+userJWT || last.Get("Apikey") != pubKey {
		t.Errorf("headers: %v", last)
	}
	if !strings.Contains(fake.log[len(fake.log)-1], "/storage/v1/object/authenticated/uploads/"+userA+"/"+f.ID+".docx") {
		t.Errorf("route: %s", fake.log[len(fake.log)-1])
	}

	other := "bbbbbbbb-0000-0000-0000-00000000000b"
	if _, err := st.Open(bg, other, f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("another user's file: %v", err)
	}
	if _, err := st.Open(bg, userA, "../etc/passwd"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("bad id: %v", err)
	}
	delete(fake.objects, userA+"/"+f.ID+".docx") // row exists, object gone
	if _, err := st.Open(bg, userA, f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("missing object: %v", err)
	}
}

func TestFileStore_OpenIsPerUser(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	f, err := fs.Create(bg, "alice", NewFile{Filename: "a.docx", Text: "x", Content: strings.NewReader("DATA")})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := fs.Open(bg, "alice", f.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "DATA" {
		t.Errorf("got %q", b)
	}
	if _, err := fs.Open(bg, "bob", f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("bob: %v", err)
	}
}
