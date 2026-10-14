// Package jobs runs SituCraft's long computations (building derived data, downloading examples,
// registering segmentations; segmentation, comparison and registration later) in the host, one at
// a time, so that they survive the page being reloaded. A job has an immutable spec, a state,
// progress, an append-only log and cooperative cancellation; the page follows them through
// Subscribe.
//
// A manager opened on a folder (Open) keeps every job there, one file each, written at each change
// of state and each line of its log, so that the jobs survive the program too. A job the program
// stopped (or that was running when it crashed) is orphaned when the folder is opened again; it
// can be resumed (its kind's handler makes its work again from its spec, and work that keeps what
// it finished, as building derived data does, continues where it stopped) or discarded.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	// Orphaned: interrupted by the program stopping (or crashing); it can be resumed or discarded.
	Orphaned State = "orphaned"
)

// Finished reports whether a job in this state will not change again unless it is resumed.
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
	// Runs counts the times it was started (more than one once resumed).
	Runs   int     `json:"runs,omitempty"`
	Events []Event `json:"events,omitempty"`
	// Resumable: orphaned, and its kind can make its work again.
	Resumable bool `json:"resumable,omitempty"`
}

// ErrInvalid is wrapped by a run's error when its outputs failed their checks (the job ends
// invalid rather than failed).
var ErrInvalid = errors.New("the job's outputs failed their checks")

// Run is what a running job's function reports through.
type Run struct {
	m  *Manager
	id string
}

// ID is the job's id (for naming what it makes).
func (r *Run) ID() string { return r.id }

// Progress records the fraction done (clamped to 0–1) and what is being done ("" keeps the
// message). Progress is not written to the job's file: a resumed job reports its own again.
func (r *Run) Progress(fraction float64, message string) {
	r.m.update(r.id, false, func(j *Job) {
		j.Progress = min(max(fraction, 0), 1)
		if message != "" {
			j.Message = message
		}
	})
}

// Log appends a line to the job's log.
func (r *Run) Log(level, format string, args ...any) {
	r.m.update(r.id, true, func(j *Job) {
		j.Events = appendEvent(j.Events, Event{Time: time.Now(), Level: level, Message: fmt.Sprintf(format, args...)})
	})
}

// Validating marks the job as checking its outputs.
func (r *Run) Validating() {
	r.m.update(r.id, true, func(j *Job) { j.State = Validating })
}

func appendEvent(events []Event, e Event) []Event {
	events = append(events, e)
	if len(events) > maxEvents {
		events = append(events[:1], events[len(events)-maxEvents+1:]...)
	}
	return events
}

// Func is a job's work. It returns when done, or with ctx's error soon after ctx is cancelled.
type Func func(ctx context.Context, r *Run) error

// Handler makes a job's work again from its spec, to resume it.
type Handler func(spec json.RawMessage) (Func, error)

// kind is what the manager knows of a kind of job: how to resume one, and what to clear away when
// one is discarded.
type kind struct {
	resume  Handler
	discard func(id string, spec json.RawMessage)
}

const (
	maxEvents   = 500 // a job's log keeps its first line and its last ones
	maxFinished = 50  // finished jobs kept (the orphaned are kept until resumed or discarded)
)

type entry struct {
	job    Job
	fn     Func
	cancel context.CancelFunc
	// cancelled: the user asked it to stop (else a run cut short is the program stopping).
	cancelled bool
	done      chan struct{}
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
	kinds map[string]kind
	// dir keeps each job as <id>.json ("": jobs live in memory only).
	dir    string
	writes sync.Mutex
}

// NewManager starts a manager whose jobs live in memory only.
func NewManager() *Manager {
	ctx, stop := context.WithCancel(context.Background())
	m := &Manager{jobs: map[string]*entry{}, queue: make(chan string, 1024), ctx: ctx, stop: stop, subs: map[chan struct{}]bool{}, kinds: map[string]kind{}}
	m.wg.Add(1)
	go m.work()
	return m
}

// Open starts a manager that keeps its jobs in dir, reading those it holds: a job that had not
// finished is orphaned.
func Open(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var loaded []Job
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var j Job
		if json.Unmarshal(raw, &j) != nil || j.ID == "" || j.ID+".json" != e.Name() {
			continue
		}
		loaded = append(loaded, j)
	}
	sort.SliceStable(loaded, func(i, k int) bool { return loaded[i].Created.Before(loaded[k].Created) })
	m := NewManager()
	m.dir = dir
	m.mu.Lock()
	var orphaned []Job
	for _, j := range loaded {
		if !j.State.Finished() {
			at := j.Ended
			if at.IsZero() {
				at = time.Now()
			}
			j.State, j.Ended = Orphaned, at
			j.Events = appendEvent(j.Events, Event{Time: time.Now(), Level: "warning", Message: "Interrupted: SituCraft stopped before it finished. Resume it, or discard it."})
			orphaned = append(orphaned, j)
		}
		e := &entry{job: j, done: make(chan struct{})}
		close(e.done)
		m.jobs[j.ID] = e
		m.order = append(m.order, j.ID)
	}
	m.mu.Unlock()
	for _, j := range orphaned {
		m.write(j)
	}
	return m, nil
}

// Handle registers how jobs of a kind are resumed, and what to clear away when one is discarded
// (each optional).
func (m *Manager) Handle(kindName string, resume Handler, discard func(id string, spec json.RawMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kinds[kindName] = kind{resume: resume, discard: discard}
}

// newID names a job uniquely across runs of the program: its time and a count. m.mu is held.
func (m *Manager) newID() string {
	m.next++
	return "j" + strconv.FormatInt(time.Now().UnixMilli(), 36) + "-" + strconv.Itoa(m.next)
}

// Submit queues a job. A job with the same non-empty key that has not finished is returned
// instead of a new one (existing is then true).
func (m *Manager) Submit(kindName, title, key string, spec any, fn Func) (job Job, existing bool, err error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return Job{}, false, err
	}
	m.mu.Lock()
	if key != "" {
		if e := m.unfinished(key); e != nil {
			job = m.snapshot(e.job)
			m.mu.Unlock()
			return job, true, nil
		}
	}
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return Job{}, false, errors.New("SituCraft is stopping")
	}
	id := m.newID()
	e := &entry{job: Job{ID: id, Kind: kindName, Title: title, Key: key, Spec: raw, State: Declared, Created: time.Now()}, fn: fn, done: make(chan struct{})}
	m.jobs[id] = e
	m.order = append(m.order, id)
	e.job.State = Queued
	removed := m.prune()
	job = m.snapshot(e.job)
	m.changed()
	m.mu.Unlock()
	m.write(job)
	m.remove(removed)
	m.queue <- id
	return job, false, nil
}

// unfinished is the job with this key that has not finished. m.mu is held.
func (m *Manager) unfinished(key string) *entry {
	for _, id := range m.order {
		if e := m.jobs[id]; e.job.Key == key && !e.job.State.Finished() {
			return e
		}
	}
	return nil
}

// Resume queues an orphaned job again, its work made again by its kind's handler; it keeps its id
// and its log.
func (m *Manager) Resume(id string) (Job, error) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("no job %s", id)
	}
	if e.job.State != Orphaned {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("%s is %s: only an interrupted job can be resumed", e.job.Title, e.job.State)
	}
	k, known := m.kinds[e.job.Kind]
	if !known || k.resume == nil {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("%s cannot be resumed by this SituCraft (its kind, %s, is not one it runs)", e.job.Title, e.job.Kind)
	}
	if e.job.Key != "" && m.unfinished(e.job.Key) != nil {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("%s is already being done by another job", e.job.Title)
	}
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return Job{}, errors.New("SituCraft is stopping")
	}
	spec := e.job.Spec
	m.mu.Unlock()
	fn, err := k.resume(spec)
	if err != nil {
		return Job{}, fmt.Errorf("%s cannot be resumed: %w", e.job.Title, err)
	}
	m.mu.Lock()
	if e.job.State != Orphaned {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("%s is %s now", e.job.Title, e.job.State)
	}
	e.fn, e.cancelled, e.done = fn, false, make(chan struct{})
	e.job.State, e.job.Ended, e.job.Error = Queued, time.Time{}, ""
	e.job.Events = appendEvent(e.job.Events, Event{Time: time.Now(), Level: "info", Message: "Resumed."})
	job := m.snapshot(e.job)
	m.changed()
	m.mu.Unlock()
	m.write(job)
	m.queue <- id
	return job, nil
}

// Discard forgets a finished job (an orphaned one included), clearing away what its kind leaves
// of it (outputs never promoted).
func (m *Manager) Discard(id string) error {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("no job %s", id)
	}
	if !e.job.State.Finished() {
		m.mu.Unlock()
		return fmt.Errorf("%s is %s: cancel it first", e.job.Title, e.job.State)
	}
	k := m.kinds[e.job.Kind]
	delete(m.jobs, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.changed()
	m.mu.Unlock()
	if k.discard != nil {
		k.discard(id, e.job.Spec)
	}
	m.remove([]string{id})
	return nil
}

// prune forgets the oldest finished jobs beyond maxFinished (orphaned ones wait to be resumed or
// discarded) and answers their ids. m.mu is held.
func (m *Manager) prune() []string {
	done := func(s State) bool { return s.Finished() && s != Orphaned }
	finished := 0
	for _, id := range m.order {
		if done(m.jobs[id].job.State) {
			finished++
		}
	}
	var removed []string
	kept := m.order[:0]
	for _, id := range m.order {
		if finished > maxFinished && done(m.jobs[id].job.State) {
			delete(m.jobs, id)
			removed = append(removed, id)
			finished--
			continue
		}
		kept = append(kept, id)
	}
	m.order = kept
	return removed
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
	e.job.State, e.job.Started, e.job.Progress = Running, time.Now(), 0
	e.job.Runs++
	started := m.snapshot(e.job)
	m.changed()
	m.mu.Unlock()
	m.write(started)

	err := safely(ctx, e.fn, &Run{m: m, id: id})
	cancel()

	m.mu.Lock()
	j := &e.job
	j.Ended = time.Now()
	switch {
	case err == nil:
		j.State, j.Progress = Complete, 1
	case ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, ctx.Err())):
		if e.cancelled {
			j.State = Cancelled
		} else {
			// The program is stopping: what the job finished is kept, and it can be resumed.
			j.State = Orphaned
			j.Events = appendEvent(j.Events, Event{Time: j.Ended, Level: "warning", Message: "Interrupted: SituCraft stopped before it finished. Resume it, or discard it."})
		}
	case errors.Is(err, ErrInvalid):
		j.State, j.Error = Invalid, err.Error()
	default:
		j.State, j.Error = Failed, err.Error()
	}
	if j.Error != "" {
		j.Events = appendEvent(j.Events, Event{Time: j.Ended, Level: "error", Message: j.Error})
	}
	ended := m.snapshot(*j)
	close(e.done)
	m.changed()
	m.mu.Unlock()
	m.write(ended)
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
	e, ok := m.jobs[id]
	if !ok || e.job.State.Finished() {
		m.mu.Unlock()
		return false
	}
	if e.job.State == Queued {
		e.job.State, e.job.Ended = Cancelled, time.Now()
		close(e.done)
		job := m.snapshot(e.job)
		m.changed()
		m.mu.Unlock()
		m.write(job)
		return true
	}
	e.cancelled = true
	e.cancel()
	m.mu.Unlock()
	return true
}

// Wait blocks until the job has finished (or ctx ends) and returns its final snapshot.
func (m *Manager) Wait(ctx context.Context, id string) (Job, error) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	var done chan struct{}
	if ok {
		done = e.done
	}
	m.mu.Unlock()
	if !ok {
		return Job{}, fmt.Errorf("no job %s", id)
	}
	select {
	case <-done:
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
	return m.snapshot(e.job), true
}

// List returns snapshots of every job, oldest first, each with at most its last logLines lines.
func (m *Manager) List(logLines int) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.order))
	for _, id := range m.order {
		j := m.snapshot(m.jobs[id].job)
		if len(j.Events) > logLines {
			j.Events = j.Events[len(j.Events)-logLines:]
		}
		out = append(out, j)
	}
	return out
}

// Running reports how many jobs are queued or running.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.jobs {
		if !e.job.State.Finished() {
			n++
		}
	}
	return n
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

// Close stops the manager: the running job is interrupted (orphaned, to be resumed), and so are
// the queued ones.
func (m *Manager) Close() {
	m.mu.Lock()
	var queued []Job
	for _, e := range m.jobs {
		if e.job.State == Queued {
			e.job.State, e.job.Ended = Orphaned, time.Now()
			e.job.Events = appendEvent(e.job.Events, Event{Time: e.job.Ended, Level: "warning", Message: "SituCraft stopped before it started. Resume it, or discard it."})
			close(e.done)
			queued = append(queued, m.snapshot(e.job))
		}
	}
	m.mu.Unlock()
	for _, j := range queued {
		m.write(j)
	}
	m.stop()
	m.wg.Wait()
}

func (m *Manager) update(id string, persist bool, change func(*Job)) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	change(&e.job)
	job := m.snapshot(e.job)
	m.changed()
	m.mu.Unlock()
	if persist {
		m.write(job)
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

// snapshot copies a job for a caller, with whether it can be resumed. m.mu is held.
func (m *Manager) snapshot(j Job) Job {
	j.Events = append([]Event(nil), j.Events...)
	k, ok := m.kinds[j.Kind]
	j.Resumable = j.State == Orphaned && ok && k.resume != nil
	return j
}

// write keeps a job in its file (through a temporary file renamed into place).
func (m *Manager) write(j Job) {
	if m.dir == "" {
		return
	}
	j.Resumable = false
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return
	}
	m.writes.Lock()
	defer m.writes.Unlock()
	// A job forgotten meanwhile is not written back.
	m.mu.Lock()
	_, known := m.jobs[j.ID]
	m.mu.Unlock()
	if !known {
		return
	}
	tmp, err := os.CreateTemp(m.dir, ".job-*.tmp")
	if err != nil {
		return
	}
	_, err = tmp.Write(append(raw, '\n'))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return
	}
	os.Rename(tmp.Name(), filepath.Join(m.dir, j.ID+".json"))
}

// remove deletes forgotten jobs' files.
func (m *Manager) remove(ids []string) {
	if m.dir == "" {
		return
	}
	m.writes.Lock()
	defer m.writes.Unlock()
	for _, id := range ids {
		os.Remove(filepath.Join(m.dir, id+".json"))
	}
}
