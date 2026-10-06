package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"law-assistant/internal/model"
)

const (
	maxJobsPerUser = 3
	jobRetention   = 20 * time.Minute // how long a finished job stays fetchable
)

// redlineStatus is what the client sees when it polls a job.
type redlineStatus struct {
	JobID      string           `json:"job_id"`
	State      string           `json:"state"` // running | done | error
	Phase      string           `json:"phase,omitempty"`
	Chars      int              `json:"chars"`       // characters of the model's answer so far
	EditsFound int              `json:"edits_found"` // changes the model has proposed so far
	ElapsedMS  int64            `json:"elapsed_ms"`
	Result     *redlineResponse `json:"result,omitempty"`
	Error      *model.APIError  `json:"error,omitempty"`
}

type redlineJob struct {
	id      string
	userID  string
	created time.Time
	cancel  context.CancelFunc

	mu       sync.Mutex
	state    string
	phase    string
	chars    int
	edits    int
	result   *redlineResponse
	apiErr   *model.APIError
	finished time.Time
}

func (j *redlineJob) progress(chars, edits int) {
	j.mu.Lock()
	j.chars, j.edits = chars, edits
	j.mu.Unlock()
}

func (j *redlineJob) setPhase(p string) {
	j.mu.Lock()
	j.phase = p
	j.mu.Unlock()
}

func (j *redlineJob) finish(r *redlineResponse) {
	j.mu.Lock()
	j.state, j.result, j.finished = "done", r, time.Now()
	j.mu.Unlock()
}

func (j *redlineJob) fail(e *model.APIError) {
	j.mu.Lock()
	j.state, j.apiErr, j.finished = "error", e, time.Now()
	j.mu.Unlock()
}

func (j *redlineJob) snapshot() redlineStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	end := time.Now()
	if !j.finished.IsZero() {
		end = j.finished
	}
	return redlineStatus{
		JobID: j.id, State: j.state, Phase: j.phase, Chars: j.chars, EditsFound: j.edits,
		ElapsedMS: end.Sub(j.created).Milliseconds(), Result: j.result, Error: j.apiErr,
	}
}

// redlineJobs holds jobs in memory. They live on one instance only: a restart or
// a second instance loses them, and the client is told to start again.
type redlineJobs struct {
	mu   sync.Mutex
	jobs map[string]*redlineJob
}

func newRedlineJobs() *redlineJobs { return &redlineJobs{jobs: map[string]*redlineJob{}} }

func (r *redlineJobs) add(userID string, cancel context.CancelFunc) *redlineJob {
	var b [12]byte
	_, _ = rand.Read(b[:])
	j := &redlineJob{id: hex.EncodeToString(b[:]), userID: userID, created: time.Now(), cancel: cancel, state: "running", phase: "reviewing"}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeLocked()
	// Keep at most maxJobsPerUser per user (a finished job holds a whole document in memory).
	var mine []*redlineJob
	for _, o := range r.jobs {
		if o.userID == userID {
			mine = append(mine, o)
		}
	}
	if len(mine) >= maxJobsPerUser {
		sort.Slice(mine, func(i, k int) bool { return mine[i].created.Before(mine[k].created) })
		for _, o := range mine {
			if len(mine) < maxJobsPerUser {
				break
			}
			o.mu.Lock()
			done := o.state != "running"
			o.mu.Unlock()
			if done {
				delete(r.jobs, o.id)
				mine = mine[1:]
			}
		}
	}
	r.jobs[j.id] = j
	return j
}

func (r *redlineJobs) get(userID, id string) *redlineJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeLocked()
	if j := r.jobs[id]; j != nil && j.userID == userID {
		return j
	}
	return nil
}

func (r *redlineJobs) remove(id string) {
	r.mu.Lock()
	delete(r.jobs, id)
	r.mu.Unlock()
}

func (r *redlineJobs) purgeLocked() {
	now := time.Now()
	for id, j := range r.jobs {
		j.mu.Lock()
		expired := !j.finished.IsZero() && now.Sub(j.finished) > jobRetention
		j.mu.Unlock()
		if expired {
			delete(r.jobs, id)
		}
	}
}
