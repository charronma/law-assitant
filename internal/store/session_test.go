package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionStore_GetReturnsSnapshot(t *testing.T) {
	s := NewSessionStore()
	sess := s.Create("u1", ModuleConsult, "")
	if _, err := s.AddMessage("u1", sess.ID, "user", "hello", nil); err != nil {
		t.Fatal(err)
	}

	snap, err := s.Get("u1", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMessage("u1", sess.ID, "assistant", "hi", nil); err != nil {
		t.Fatal(err)
	}

	if got := len(snap.Messages); got != 1 {
		t.Fatalf("snapshot mutated after AddMessage: got %d messages, want 1", got)
	}
	if snap.Messages[0].Content != "hello" {
		t.Fatalf("unexpected content %q", snap.Messages[0].Content)
	}
}

func TestSessionStore_ListSortedNewestFirstWithoutMessages(t *testing.T) {
	s := NewSessionStore()
	a := s.Create("u1", ModuleConsult, "a")
	time.Sleep(2 * time.Millisecond)
	b := s.Create("u1", ModulePleading, "b")
	time.Sleep(2 * time.Millisecond)
	c := s.Create("u1", ModuleContract, "c")

	// Touching the oldest session must move it to the front.
	time.Sleep(2 * time.Millisecond)
	if _, err := s.AddMessage("u1", a.ID, "user", "bump", nil); err != nil {
		t.Fatal(err)
	}

	list := s.List("u1")
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
	sess := s.Create("u1", ModuleConsult, "")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := s.AddMessage("u1", sess.ID, "user", "msg", nil); err != nil {
					t.Error(err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				got, err := s.Get("u1", sess.ID)
				if err != nil {
					t.Error(err)
					return
				}
				_ = len(got.Messages)
				_ = s.List("u1")
			}
		}()
	}
	wg.Wait()

	got, _ := s.Get("u1", sess.ID)
	if len(got.Messages) != 8*200 {
		t.Fatalf("got %d messages, want %d", len(got.Messages), 8*200)
	}
}

func TestSessionStore_UserIsolation(t *testing.T) {
	s := NewSessionStore()
	mine := s.Create("alice", ModuleConsult, "mine")
	if _, err := s.AddMessage("alice", mine.ID, "user", "secret", nil); err != nil {
		t.Fatal(err)
	}

	// Another user sees nothing of alice's session, and every operation on it
	// fails exactly like a missing session.
	if _, err := s.Get("bob", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: got %v, want ErrNotFound", err)
	}
	if _, err := s.GetMessages("bob", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMessages: got %v, want ErrNotFound", err)
	}
	if _, err := s.AddMessage("bob", mine.ID, "user", "injected", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddMessage: got %v, want ErrNotFound", err)
	}
	if err := s.Delete("bob", mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: got %v, want ErrNotFound", err)
	}
	if got := s.List("bob"); len(got) != 0 {
		t.Errorf("List(bob) = %d sessions, want 0", len(got))
	}

	// Alice's data is untouched by bob's attempts.
	got, err := s.Get("alice", mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "secret" {
		t.Errorf("alice's session was modified: %+v", got.Messages)
	}
	if got := s.List("alice"); len(got) != 1 {
		t.Errorf("List(alice) = %d sessions, want 1", len(got))
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
