package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var bg = context.Background()

func mustCreate(t *testing.T, s *SessionStore, user string, m Module, title string) *Session {
	t.Helper()
	sess, err := s.Create(bg, user, m, title)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func mustAdd(t *testing.T, s *SessionStore, user, sid, role, content string) {
	t.Helper()
	if _, err := s.AddMessage(bg, user, sid, role, content, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSessionStore_GetReturnsSnapshot(t *testing.T) {
	s := NewSessionStore()
	sess := mustCreate(t, s, "u1", ModuleConsult, "")
	mustAdd(t, s, "u1", sess.ID, "user", "hello")

	snap, err := s.Get(bg, "u1", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, s, "u1", sess.ID, "assistant", "hi")

	if got := len(snap.Messages); got != 1 {
		t.Fatalf("snapshot mutated after AddMessage: got %d messages, want 1", got)
	}
	if snap.Messages[0].Content != "hello" {
		t.Fatalf("unexpected content %q", snap.Messages[0].Content)
	}
}

func TestSessionStore_ListSortedNewestFirstWithoutMessages(t *testing.T) {
	s := NewSessionStore()
	a := mustCreate(t, s, "u1", ModuleConsult, "a")
	time.Sleep(2 * time.Millisecond)
	b := mustCreate(t, s, "u1", ModulePleading, "b")
	time.Sleep(2 * time.Millisecond)
	c := mustCreate(t, s, "u1", ModuleContract, "c")

	// Touching the oldest session must move it to the front.
	time.Sleep(2 * time.Millisecond)
	mustAdd(t, s, "u1", a.ID, "user", "bump")

	list, err := s.List(bg, "u1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{a.ID, c.ID, b.ID}
	if len(list) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(list), len(want))
	}
	for i, id := range want {
		if list[i].ID != id {
			t.Errorf("list[%d] = %s, want %s", i, list[i].ID, id)
		}
	}
	if list[0].Messages != nil {
		t.Error("List must not return message history")
	}
	if list[0].MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1", list[0].MessageCount)
	}
}

// Run with -race: readers and writers must not touch shared state unlocked.
func TestSessionStore_ConcurrentAccess(t *testing.T) {
	s := NewSessionStore()
	sess := mustCreate(t, s, "u1", ModuleConsult, "")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := s.AddMessage(bg, "u1", sess.ID, "user", "msg", nil); err != nil {
					t.Error(err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				got, err := s.Get(bg, "u1", sess.ID)
				if err != nil {
					t.Error(err)
					return
				}
				_ = len(got.Messages)
				if _, err := s.List(bg, "u1"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got, _ := s.Get(bg, "u1", sess.ID)
	if len(got.Messages) != 8*200 {
		t.Fatalf("got %d messages, want %d", len(got.Messages), 8*200)
	}
}

func TestSessionStore_UserIsolation(t *testing.T) {
	s := NewSessionStore()
	mine := mustCreate(t, s, "alice", ModuleConsult, "mine")
	mustAdd(t, s, "alice", mine.ID, "user", "secret")

	// Another user sees nothing of alice's session, and every operation on it
	// fails exactly like a missing session.
	if _, err := s.Get(bg, "bob", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: got %v, want ErrNotFound", err)
	}
	if _, err := s.AddMessage(bg, "bob", mine.ID, "user", "injected", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddMessage: got %v, want ErrNotFound", err)
	}
	if err := s.Delete(bg, "bob", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: got %v, want ErrNotFound", err)
	}
	if got, _ := s.List(bg, "bob"); len(got) != 0 {
		t.Errorf("List(bob) = %d sessions, want 0", len(got))
	}

	// Alice's data is untouched by bob's attempts.
	got, err := s.Get(bg, "alice", mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "secret" {
		t.Errorf("alice's session was modified: %+v", got.Messages)
	}
	if got, _ := s.List(bg, "alice"); len(got) != 1 {
		t.Errorf("List(alice) = %d sessions, want 1", len(got))
	}
}

func TestModuleValid(t *testing.T) {
	for _, m := range []Module{ModuleConsult, ModulePleading, ModuleContract, ModuleEvidenceOrg, ModuleEvidence, ModuleCommunication} {
		if !m.Valid() {
			t.Errorf("%q should be valid", m)
		}
	}
	for _, m := range []Module{"", "hacking", "CONSULT"} {
		if m.Valid() {
			t.Errorf("%q should be invalid", m)
		}
	}
}

func TestFileStore_UserIsolation(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	f, err := fs.Save("alice", "", "contract.txt", "text/plain", 5, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Get("alice", f.ID); err != nil {
		t.Errorf("owner Get: %v", err)
	}
	if _, err := fs.Get("bob", f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Get by other user: got %v, want ErrFileNotFound", err)
	}
	if err := fs.SetExtractedText("bob", f.ID, "tampered"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("SetExtractedText by other user: got %v, want ErrFileNotFound", err)
	}
	if err := fs.SetExtractedText("alice", f.ID, "ok"); err != nil {
		t.Errorf("owner SetExtractedText: %v", err)
	}
	if got, _ := fs.Get("alice", f.ID); got.ExtractedText != "ok" {
		t.Errorf("ExtractedText = %q, want %q", got.ExtractedText, "ok")
	}
}
