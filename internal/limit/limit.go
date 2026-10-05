// Package limit provides small in-process per-key limiters: a token bucket for
// request rates and a counter for concurrent work. State is per process; with
// several replicas each enforces its own budget.
package limit

import (
	"math"
	"sync"
	"time"
)

// Rate is a per-key token bucket: up to Burst requests at once, refilled at
// PerMinute tokens per minute. A nil *Rate allows everything.
type Rate struct {
	perSec float64
	burst  float64
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	sweepAt time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRate returns nil (unlimited) when perMinute <= 0. burst defaults to perMinute.
func NewRate(perMinute, burst int) *Rate {
	return newRate(perMinute, burst, time.Now)
}

func newRate(perMinute, burst int, now func() time.Time) *Rate {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = perMinute
	}
	return &Rate{perSec: float64(perMinute) / 60, burst: float64(burst), now: now, buckets: map[string]*bucket{}}
}

// Allow takes one token for key. When it refuses, retryAfter says how long
// until a token is available.
func (r *Rate) Allow(key string) (ok bool, retryAfter time.Duration) {
	if r == nil {
		return true, 0
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweep(now)

	b := r.buckets[key]
	if b == nil {
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}
	b.tokens = math.Min(r.burst, b.tokens+now.Sub(b.last).Seconds()*r.perSec)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / r.perSec * float64(time.Second))
	return false, wait.Round(time.Second) + time.Second
}

// sweep drops buckets that have fully refilled (they carry no information),
// at most once a minute, so the map cannot grow without bound.
func (r *Rate) sweep(now time.Time) {
	if now.Before(r.sweepAt) {
		return
	}
	r.sweepAt = now.Add(time.Minute)
	for k, b := range r.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*r.perSec >= r.burst {
			delete(r.buckets, k)
		}
	}
}

// Concurrency caps in-flight work per key. A nil *Concurrency allows everything.
type Concurrency struct {
	max int

	mu     sync.Mutex
	active map[string]int
}

// NewConcurrency returns nil (unlimited) when max <= 0.
func NewConcurrency(max int) *Concurrency {
	if max <= 0 {
		return nil
	}
	return &Concurrency{max: max, active: map[string]int{}}
}

// Acquire reserves a slot for key; call the returned release exactly once
// (it is idempotent). ok is false when key is at its limit.
func (c *Concurrency) Acquire(key string) (release func(), ok bool) {
	if c == nil {
		return func() {}, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active[key] >= c.max {
		return nil, false
	}
	c.active[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.active[key]--; c.active[key] <= 0 {
				delete(c.active, key)
			}
		})
	}, true
}
