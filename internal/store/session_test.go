package store

import (
	"sync"
	"testing"
	"time"
)

func TestSessionStore_GetReturnsSnapshot(t *testing.T) {
	s := NewSessionStore()
	sess := s.Create(ModuleConsult, "")
	if _, err := s.AddMessage(sess.ID, "user", "hello", nil); err != nil {
		t.Fatal(err)
	}

	snap, err := s.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMessage(sess.ID, "assistant", "hi", nil); err != nil {
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
	a := s.Create(ModuleConsult, "a")
	time.Sleep(2 * time.Millisecond)
	b := s.Create(ModulePleading, "b")
	time.Sleep(2 * time.Millisecond)
	c := s.Create(ModuleContract, "c")

	// Touching the oldest session must move it to the front.
	time.Sleep(2 * time.Millisecond)
	if _, err := s.AddMessage(a.ID, "user", "bump", nil); err != nil {
		t.Fatal(err)
	}

	list := s.List()
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
	sess := s.Create(ModuleConsult, "")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := s.AddMessage(sess.ID, "user", "msg", nil); err != nil {
					t.Error(err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				got, err := s.Get(sess.ID)
				if err != nil {
					t.Error(err)
					return
				}
				_ = len(got.Messages)
				_ = s.List()
			}
		}()
	}
	wg.Wait()

	got, _ := s.Get(sess.ID)
	if len(got.Messages) != 8*200 {
		t.Fatalf("got %d messages, want %d", len(got.Messages), 8*200)
	}
}
