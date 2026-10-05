package limit

import (
	"sync"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestRateBurstThenRefill(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	r := newRate(6, 3, c.now) // 1 token / 10s, burst 3
	for i := 0; i < 3; i++ {
		if ok, _ := r.Allow("u"); !ok {
			t.Fatalf("burst request %d refused", i)
		}
	}
	ok, retry := r.Allow("u")
	if ok {
		t.Fatal("4th request must be refused")
	}
	if retry < 10*time.Second || retry > 12*time.Second {
		t.Errorf("retryAfter = %v, want ~10-11s", retry)
	}
	c.t = c.t.Add(10 * time.Second)
	if ok, _ := r.Allow("u"); !ok {
		t.Error("a token should have refilled after 10s")
	}
	if ok, _ := r.Allow("u"); ok {
		t.Error("only one token should have refilled")
	}
}

func TestRateKeysAreIndependent(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	r := newRate(1, 1, c.now)
	if ok, _ := r.Allow("a"); !ok {
		t.Fatal()
	}
	if ok, _ := r.Allow("a"); ok {
		t.Fatal("a should be limited")
	}
	if ok, _ := r.Allow("b"); !ok {
		t.Fatal("b must not be affected by a")
	}
}

func TestRateSweepForgetsIdleKeys(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	r := newRate(60, 5, c.now)
	for _, k := range []string{"a", "b", "c"} {
		r.Allow(k)
	}
	c.t = c.t.Add(10 * time.Minute)
	r.Allow("d")
	if n := len(r.buckets); n != 1 {
		t.Errorf("idle buckets not swept: %d left", n)
	}
}

func TestNilLimitersAllowEverything(t *testing.T) {
	if ok, _ := NewRate(0, 0).Allow("x"); !ok {
		t.Error("disabled rate limiter refused")
	}
	rel, ok := NewConcurrency(0).Acquire("x")
	if !ok {
		t.Fatal("disabled concurrency limiter refused")
	}
	rel()
}

func TestConcurrency(t *testing.T) {
	c := NewConcurrency(2)
	r1, ok1 := c.Acquire("u")
	_, ok2 := c.Acquire("u")
	if !ok1 || !ok2 {
		t.Fatal("two slots should be free")
	}
	if _, ok := c.Acquire("u"); ok {
		t.Fatal("third slot must be refused")
	}
	if _, ok := c.Acquire("other"); !ok {
		t.Fatal("other users are unaffected")
	}
	r1()
	r1() // idempotent: must not free a second slot
	if _, ok := c.Acquire("u"); !ok {
		t.Fatal("released slot should be reusable")
	}
	if _, ok := c.Acquire("u"); ok {
		t.Fatal("double release freed an extra slot")
	}
}

func TestConcurrencyRace(t *testing.T) {
	c := NewConcurrency(3)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rel, ok := c.Acquire("u"); ok {
				time.Sleep(time.Millisecond)
				rel()
			}
		}()
	}
	wg.Wait()
	if len(c.active) != 0 {
		t.Errorf("leaked slots: %v", c.active)
	}
}
