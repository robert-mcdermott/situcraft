// Package jobs runs SituCraft's long computations (building derived data now; segmentation,
// comparison and registration later) in the host, one at a time, so that they survive the page
// being reloaded. A job has an immutable spec, a state, progress, an append-only log and
// cooperative cancellation; the page follows them through Subscribe.
//
// This is the first version (wave 1): jobs live in memory. Persisting them, and marking a job
// interrupted by a restart as orphaned, come with the artifact store (wave 2).
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// State is where a job is in its life.
type State string

const (
	Declared   State = "declared"
	Queued     State = "queued"
	Running    State = "running"
	Validating State = "validating"
	Complete   State = "complete"
	Cancelled  State = "cancelled"
	Failed     State = "failed"
	Invalid    State = "invalid"
	Orphaned   State = "orphaned"
)

// Finished reports whether a job in this state will not change again.
func (s State) Finished() bool {
	switch s {
	case Complete, Cancelled, Failed, Invalid, Orphaned:
		return true
	}
	return false
}

// Event is one line of a job's log.
type Event struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // "info", "warning" or "error"
	Message string    `json:"message"`
}

// Job is a snapshot of one job.
type Job struct {
	ID    string          `json:"id"`
	Kind  string          `json:"kind"`
	Title string          `json:"title"`
	Key   string          `json:"key,omitempty"`
	Spec  json.RawMessage `json:"spec"`
	State State           `json:"state"`
	// Progress is the fraction done, from 0 to 1; Message says what is being done.
	Progress float64   `json:"progress"`
	Message  string    `json:"message,omitempty"`
	Created  time.Time `json:"created"`
	Started  time.Time `json:"started,omitzero"`
	Ended    time.Time `json:"ended,omitzero"`
	Error    string    `json:"error,omitempty"`
	Events   []Event   `json:"events,omitempty"`
}

// ErrInvalid is wrapped by a run's error when its outputs failed their checks (the job ends
// invalid rather than failed).
var ErrInvalid = errors.New("the job's outputs failed their checks")

// Run is what a running job's function reports through.
type Run struct {
	m  *Manager
	id string
}

// Progress records the fraction done (clamped to 0–1) and what is being done ("" keeps the
// message).
func (r *Run) Progress(fraction float64, message string) {
	r.m.update(r.id, func(j *Job) {
		j.Progress = min(max(fraction, 0), 1)
		if message != "" {
			j.Message = message
		}
	})
}

// Log appends a line to the job's log.
func (r *Run) Log(level, format string, args ...any) {
	r.m.update(r.id, func(j *Job) {
		j.Events = append(j.Events, Event{Time: time.Now(), Level: level, Message: fmt.Sprintf(format, args...)})
		if len(j.Events) > maxEvents {
			j.Events = append(j.Events[:1], j.Events[len(j.Events)-maxEvents+1:]...)
		}
	})
}

// Validating marks the job as checking its outputs.
func (r *Run) Validating() {
	r.m.update(r.id, func(j *Job) { j.State = Validating })
}

// Func is a job's work. It returns when done, or with ctx's error soon after ctx is cancelled.
type Func func(ctx context.Context, r *Run) error

const (
	maxEvents   = 500 // a job's log keeps its first line and its last ones
	maxFinished = 50  // finished jobs kept for the page
)

type entry struct {
	job    Job
	fn     Func
	cancel context.CancelFunc
	done   chan struct{}
}

// Manager queues and runs jobs one at a time.
type Manager struct {
	mu    sync.Mutex
	jobs  map[string]*entry
	order []string // ids, oldest first
	queue chan string
	next  int
	ctx   context.Context
	stop  context.CancelFunc
	wg    sync.WaitGroup
	subs  map[chan struct{}]bool
}

// NewManager starts a manager with its worker.
func NewManager() *Manager {
	ctx, stop := context.WithCancel(context.Background())
	m := &Manager{jobs: map[string]*entry{}, queue: make(chan string, 1024), ctx: ctx, stop: stop, subs: map[chan struct{}]bool{}}
	m.wg.Add(1)
	go m.work()
	return m
}

// Submit queues a job. A job with the same non-empty key that has not finished is returned
// instead of a new one (existing is then true).
func (m *Manager) Submit(kind, title, key string, spec any, fn Func) (job Job, existing bool, err error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return Job{}, false, err
	}
	m.mu.Lock()
	if key != "" {
		for _, id := range m.order {
			if e := m.jobs[id]; e.job.Key == key && !e.job.State.Finished() {
				job = copyJob(e.job)
				m.mu.Unlock()
				return job, true, nil
			}
		}
	}
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return Job{}, false, errors.New("SituCraft is stopping")
	}
	m.next++
	id := "j" + strconv.Itoa(m.next)
	e := &entry{job: Job{ID: id, Kind: kind, Title: title, Key: key, Spec: raw, State: Declared, Created: time.Now()}, fn: fn, done: make(chan struct{})}
	m.jobs[id] = e
	m.order = append(m.order, id)
	e.job.State = Queued
	m.prune()
	job = copyJob(e.job)
	m.changed()
	m.mu.Unlock()
	m.queue <- id
	return job, false, nil
}

// prune forgets the oldest finished jobs beyond maxFinished. m.mu is held.
func (m *Manager) prune() {
	finished := 0
	for _, id := range m.order {
		if m.jobs[id].job.State.Finished() {
			finished++
		}
	}
	kept := m.order[:0]
	for _, id := range m.order {
		if finished > maxFinished && m.jobs[id].job.State.Finished() {
			delete(m.jobs, id)
			finished--
			continue
		}
		kept = append(kept, id)
	}
	m.order = kept
}

func (m *Manager) work() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case id := <-m.queue:
			m.run(id)
		}
	}
}

func (m *Manager) run(id string) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok || e.job.State != Queued {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	e.cancel = cancel
	e.job.State, e.job.Started = Running, time.Now()
	m.changed()
	m.mu.Unlock()

	err := safely(ctx, e.fn, &Run{m: m, id: id})
	cancel()

	m.mu.Lock()
	j := &e.job
	j.Ended = time.Now()
	switch {
	case err == nil:
		j.State, j.Progress = Complete, 1
	case ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, ctx.Err())):
		j.State = Cancelled
	case errors.Is(err, ErrInvalid):
		j.State, j.Error = Invalid, err.Error()
	default:
		j.State, j.Error = Failed, err.Error()
	}
	if j.Error != "" {
		j.Events = append(j.Events, Event{Time: j.Ended, Level: "error", Message: j.Error})
	}
	close(e.done)
	m.changed()
	m.mu.Unlock()
}

// safely runs fn, turning a panic into an error so that one bad job cannot stop the program.
func safely(ctx context.Context, fn Func, r *Run) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("internal error: %v", p)
		}
	}()
	return fn(ctx, r)
}

// Cancel asks a job to stop: a queued job is cancelled at once, a running one when its function
// next checks. It reports whether the job was still unfinished.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok || e.job.State.Finished() {
		return false
	}
	if e.job.State == Queued {
		e.job.State, e.job.Ended = Cancelled, time.Now()
		close(e.done)
		m.changed()
		return true
	}
	e.cancel()
	return true
}

// Wait blocks until the job has finished (or ctx ends) and returns its final snapshot.
func (m *Manager) Wait(ctx context.Context, id string) (Job, error) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return Job{}, fmt.Errorf("no job %s", id)
	}
	select {
	case <-e.done:
	case <-ctx.Done():
		return Job{}, ctx.Err()
	}
	job, _ := m.Get(id)
	return job, nil
}

// Get returns a snapshot of a job, with its log.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return copyJob(e.job), true
}

// List returns snapshots of every job, oldest first, each with at most its last logLines lines.
func (m *Manager) List(logLines int) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.order))
	for _, id := range m.order {
		j := copyJob(m.jobs[id].job)
		if len(j.Events) > logLines {
			j.Events = j.Events[len(j.Events)-logLines:]
		}
		out = append(out, j)
	}
	return out
}

// Subscribe returns a channel that receives a signal (coalesced) whenever any job changes, and a
// function that ends the subscription.
func (m *Manager) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.subs[ch] = true
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

// Close cancels every job and waits for the running one to return.
func (m *Manager) Close() {
	m.mu.Lock()
	for _, e := range m.jobs {
		if e.job.State == Queued {
			e.job.State, e.job.Ended = Cancelled, time.Now()
			close(e.done)
		}
	}
	m.mu.Unlock()
	m.stop()
	m.wg.Wait()
}

func (m *Manager) update(id string, change func(*Job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok {
		change(&e.job)
		m.changed()
	}
}

// changed signals subscribers. m.mu is held.
func (m *Manager) changed() {
	for ch := range m.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func copyJob(j Job) Job {
	j.Events = append([]Event(nil), j.Events...)
	return j
}
