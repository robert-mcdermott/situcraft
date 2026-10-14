package jobs

import (
	"context"
	"errors"
	"fmt"
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

func TestCloseCancelsTheRunningJob(t *testing.T) {
	m := NewManager()
	started := make(chan struct{})
	job, _, _ := m.Submit("test", "t", "", nil, func(ctx context.Context, r *Run) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	m.Close()
	if j, _ := m.Get(job.ID); j.State != Cancelled {
		t.Errorf("after Close: %s", j.State)
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
