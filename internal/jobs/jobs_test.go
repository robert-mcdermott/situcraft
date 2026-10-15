package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wait(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job, err := m.Wait(ctx, id)
	if err != nil {
		t.Fatalf("waiting for %s: %v", id, err)
	}
	return job
}

func TestAJobRunsToCompletion(t *testing.T) {
	m := NewManager()
	defer m.Close()
	job, existing, err := m.Submit("test", "Count to three", "", map[string]int{"to": 3}, func(ctx context.Context, r *Run) error {
		for i := 1; i <= 3; i++ {
			r.Progress(float64(i)/3, fmt.Sprintf("step %d", i))
			r.Log("info", "step %d", i)
		}
		r.Validating()
		return nil
	})
	if err != nil || existing {
		t.Fatalf("Submit: %v, existing %v", err, existing)
	}
	if string(job.Spec) != `{"to":3}` {
		t.Errorf("spec %s", job.Spec)
	}
	done := wait(t, m, job.ID)
	if done.State != Complete || done.Progress != 1 || done.Message != "step 3" || len(done.Events) != 3 {
		t.Errorf("finished as %+v", done)
	}
	if done.Started.IsZero() || done.Ended.Before(done.Started) {
		t.Errorf("times %v %v", done.Started, done.Ended)
	}
}

func TestStatesOfFailureAndCancellation(t *testing.T) {
	m := NewManager()
	defer m.Close()
	failed, _, _ := m.Submit("test", "fails", "", nil, func(ctx context.Context, r *Run) error { return errors.New("no room") })
	invalid, _, _ := m.Submit("test", "invalid", "", nil, func(ctx context.Context, r *Run) error {
		return fmt.Errorf("tile 3: %w", ErrInvalid)
	})
	panics, _, _ := m.Submit("test", "panics", "", nil, func(ctx context.Context, r *Run) error { panic("boom") })
	started := make(chan struct{})
	running, _, _ := m.Submit("test", "runs until cancelled", "", nil, func(ctx context.Context, r *Run) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	queued, _, _ := m.Submit("test", "never starts", "", nil, func(ctx context.Context, r *Run) error {
		t.Error("a job cancelled while queued ran")
		return nil
	})
	if j := wait(t, m, failed.ID); j.State != Failed || j.Error != "no room" {
		t.Errorf("failed: %+v", j)
	}
	if j := wait(t, m, invalid.ID); j.State != Invalid {
		t.Errorf("invalid: %+v", j)
	}
	if j := wait(t, m, panics.ID); j.State != Failed || j.Error != "internal error: boom" {
		t.Errorf("panics: %+v", j)
	}
	<-started
	if !m.Cancel(queued.ID) {
		t.Error("a queued job could not be cancelled")
	}
	if !m.Cancel(running.ID) {
		t.Error("a running job could not be cancelled")
	}
	if j := wait(t, m, running.ID); j.State != Cancelled {
		t.Errorf("running: %+v", j)
	}
	if j := wait(t, m, queued.ID); j.State != Cancelled {
		t.Errorf("queued: %+v", j)
	}
	if m.Cancel(running.ID) {
		t.Error("a finished job was cancelled again")
	}
}

func TestOneUnfinishedJobPerKey(t *testing.T) {
	m := NewManager()
	defer m.Close()
	release := make(chan struct{})
	first, _, _ := m.Submit("build", "first", "dataset-1", nil, func(ctx context.Context, r *Run) error {
		<-release
		return nil
	})
	again, existing, _ := m.Submit("build", "again", "dataset-1", nil, func(ctx context.Context, r *Run) error { return nil })
	if !existing || again.ID != first.ID {
		t.Errorf("a second job with the key was made: %v %s %s", existing, again.ID, first.ID)
	}
	close(release)
	wait(t, m, first.ID)
	after, existing, _ := m.Submit("build", "after", "dataset-1", nil, func(ctx context.Context, r *Run) error { return nil })
	if existing || after.ID == first.ID {
		t.Error("a finished job blocked a new one with its key")
	}
	wait(t, m, after.ID)
}

func TestSubscribersAreToldOfChanges(t *testing.T) {
	m := NewManager()
	defer m.Close()
	changes, end := m.Subscribe()
	defer end()
	job, _, _ := m.Submit("test", "t", "", nil, func(ctx context.Context, r *Run) error { return nil })
	wait(t, m, job.ID)
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("no change was signalled")
	}
	list := m.List(0)
	if len(list) != 1 || list[0].State != Complete {
		t.Errorf("List: %+v", list)
	}
}

func TestCloseInterruptsTheRunningJob(t *testing.T) {
	m := NewManager()
	started := make(chan struct{})
	job, _, _ := m.Submit("test", "t", "", nil, func(ctx context.Context, r *Run) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	queued, _, _ := m.Submit("test", "waits", "", nil, func(ctx context.Context, r *Run) error { return nil })
	<-started
	m.Close()
	for _, id := range []string{job.ID, queued.ID} {
		if j, _ := m.Get(id); j.State != Orphaned {
			t.Errorf("after Close: %s", j.State)
		}
	}
	if _, _, err := m.Submit("test", "late", "", nil, func(ctx context.Context, r *Run) error { return nil }); err == nil {
		t.Error("a job was accepted after Close")
	}
}

func TestTheLogKeepsItsFirstAndLastLines(t *testing.T) {
	m := NewManager()
	defer m.Close()
	job, _, _ := m.Submit("test", "t", "", nil, func(ctx context.Context, r *Run) error {
		for i := 0; i < maxEvents+100; i++ {
			r.Log("info", "line %d", i)
		}
		return nil
	})
	j := wait(t, m, job.ID)
	if len(j.Events) != maxEvents || j.Events[0].Message != "line 0" || j.Events[len(j.Events)-1].Message != fmt.Sprintf("line %d", maxEvents+99) {
		t.Errorf("%d lines, first %q, last %q", len(j.Events), j.Events[0].Message, j.Events[len(j.Events)-1].Message)
	}
}

// Jobs kept in a folder survive the program: finished ones are listed again with their logs, and
// one interrupted (by the program stopping, or a crash) is orphaned, to be resumed or discarded.
func TestJobsSurviveTheProgram(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	done, _, _ := m.Submit("build", "Derived data of lung", "lung", map[string]string{"path": "/data/lung"}, func(ctx context.Context, r *Run) error {
		r.Log("info", "built %d tiles", 84)
		return nil
	})
	wait(t, m, done.ID)
	started := make(chan struct{})
	steps := 0
	interrupted, _, _ := m.Submit("build", "Derived data of breast", "breast", map[string]string{"path": "/data/breast"}, func(ctx context.Context, r *Run) error {
		steps = 1
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	m.Close()

	// A job whose file says it was running when the program crashed.
	crashed := Job{ID: "jcrash-1", Kind: "build", Title: "Crashed", Key: "crashed", Spec: json.RawMessage(`{}`), State: Running, Created: time.Now()}
	raw, _ := json.Marshal(crashed)
	os.WriteFile(filepath.Join(dir, "jcrash-1.json"), raw, 0o644)

	m2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	list := m2.List(10)
	if len(list) != 3 {
		t.Fatalf("%d jobs read back", len(list))
	}
	if j, _ := m2.Get(done.ID); j.State != Complete || len(j.Events) != 1 || j.Events[0].Message != "built 84 tiles" {
		t.Errorf("the finished job: %+v", j)
	}
	for _, id := range []string{interrupted.ID, "jcrash-1"} {
		if j, _ := m2.Get(id); j.State != Orphaned || j.Resumable || !strings.Contains(j.Events[len(j.Events)-1].Message, "Interrupted") {
			t.Errorf("%s: %+v", id, j)
		}
	}
	// Without a handler for its kind, it cannot be resumed.
	if _, err := m2.Resume(interrupted.ID); err == nil {
		t.Error("resumed without a handler")
	}
	var discarded []string
	m2.Handle("build", func(spec json.RawMessage) (Func, error) {
		var p struct{ Path string }
		json.Unmarshal(spec, &p)
		return func(ctx context.Context, r *Run) error {
			steps++
			r.Log("info", "resumed at %s", p.Path)
			return nil
		}, nil
	}, func(id string, spec json.RawMessage) { discarded = append(discarded, id) })
	if j, _ := m2.Get(interrupted.ID); !j.Resumable {
		t.Error("an orphaned job of a known kind is not resumable")
	}
	if _, err := m2.Resume(done.ID); err == nil {
		t.Error("a complete job was resumed")
	}
	resumed, err := m2.Resume(interrupted.ID)
	if err != nil || resumed.ID != interrupted.ID {
		t.Fatalf("resume: %v", err)
	}
	j := wait(t, m2, interrupted.ID)
	if j.State != Complete || j.Runs != 2 || steps != 2 || j.Events[len(j.Events)-1].Message != "resumed at /data/breast" {
		t.Errorf("resumed job: %+v", j)
	}
	if err := m2.Discard("jcrash-1"); err != nil || len(discarded) != 1 {
		t.Errorf("discard: %v %v", err, discarded)
	}
	if _, err := os.Stat(filepath.Join(dir, "jcrash-1.json")); !os.IsNotExist(err) {
		t.Error("a discarded job's file is still there")
	}
	// What was written is what is read: the resumed job, complete.
	m2.Close()
	m3, _ := Open(dir)
	defer m3.Close()
	if j, _ := m3.Get(interrupted.ID); j.State != Complete || j.Runs != 2 {
		t.Errorf("read again: %+v", j)
	}
	if len(m3.List(0)) != 2 {
		t.Errorf("%d jobs after the discard", len(m3.List(0)))
	}
}

// A running job cannot be discarded, and ids stay unique across runs of the program.
func TestDiscardingAndIDs(t *testing.T) {
	dir := t.TempDir()
	m, _ := Open(dir)
	release := make(chan struct{})
	job, _, _ := m.Submit("test", "t", "", nil, func(ctx context.Context, r *Run) error {
		<-release
		return nil
	})
	if err := m.Discard(job.ID); err == nil {
		t.Error("a running job was discarded")
	}
	close(release)
	wait(t, m, job.ID)
	m.Close()
	m2, _ := Open(dir)
	defer m2.Close()
	again, _, _ := m2.Submit("test", "t", "", nil, func(ctx context.Context, r *Run) error { return nil })
	if again.ID == job.ID {
		t.Errorf("an id was used again: %s", again.ID)
	}
	wait(t, m2, again.ID)
}
