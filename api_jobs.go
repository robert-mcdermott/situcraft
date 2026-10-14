package main

// Jobs (internal/jobs) over HTTP: the list, one job with its log, cancelling, and a stream of
// server-sent events that sends the list whenever a job changes, so the page follows progress
// without polling and finds running jobs again after a reload.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// jobLogLines is how many of each job's last log lines the list and the stream carry.
const jobLogLines = 5

func (a *app) registerJobs(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"jobs": a.jobs.List(jobLogLines)})
	})
	mux.HandleFunc("GET /api/jobs/events", a.jobEvents)
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		job, ok := a.jobs.Get(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "No such job.")
			return
		}
		writeJSON(w, job)
	})
	mux.HandleFunc("POST /api/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := a.jobs.Get(id); !ok {
			writeError(w, http.StatusNotFound, "No such job.")
			return
		}
		writeJSON(w, map[string]bool{"cancelled": a.jobs.Cancel(id)})
	})
}

// Changes closer together than this are sent as one event.
const jobEventInterval = 200 * time.Millisecond

// jobEvents streams "jobs" events, each the whole list, at most every jobEventInterval, with a
// comment now and then to keep the connection open. It ends when the page goes away or the
// program stops.
func (a *app) jobEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	changes, stop := a.jobs.Subscribe()
	defer stop()
	send := func() bool {
		raw, err := json.Marshal(a.jobs.List(jobLogLines))
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: jobs\ndata: %s\n\n", raw); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send() {
		return
	}
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case <-changes:
			// Let a burst of progress settle into one event.
			select {
			case <-time.After(jobEventInterval):
			case <-r.Context().Done():
				return
			}
			select {
			case <-changes:
			default:
			}
			if !send() {
				return
			}
		}
	}
}
