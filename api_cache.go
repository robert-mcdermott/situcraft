package main

// The derived-data cache (internal/cache) over HTTP: which of a bundle's entries are built, a job
// to build the rest, and what the cache holds.

import (
	"context"
	"fmt"
	"net/http"

	"situcraft/internal/cache"
	"situcraft/internal/jobs"
	"situcraft/internal/xenium"
)

func (a *app) registerCache(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/cache", a.cacheStatus)
	mux.HandleFunc("POST /api/local/{index}/cache", a.buildCache)
	mux.HandleFunc("GET /api/cache", a.cacheUsage)
}

// cacheAnswer is a bundle's derived data: its entries and the job building them, if any.
type cacheAnswer struct {
	Path   string        `json:"path"`
	Status *cache.Status `json:"status"`
	Job    *jobs.Job     `json:"job,omitempty"`
}

// cacheJobKey names the one job that may build a bundle's derived data at a time.
func cacheJobKey(path string) string { return "cache\x00" + path }

func (a *app) needCache(w http.ResponseWriter) bool {
	if a.cache == nil {
		writeError(w, http.StatusServiceUnavailable, "SituCraft runs without a cache folder.")
		return false
	}
	return true
}

// jobFor is the unfinished job with this key, if there is one.
func (a *app) jobFor(key string) *jobs.Job {
	for _, j := range a.jobs.List(5) {
		if j.Key == key && !j.State.Finished() {
			return &j
		}
	}
	return nil
}

func (a *app) cacheStatus(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	status, err := a.cache.Status(b)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, cacheAnswer{Path: b.Path, Status: status, Job: a.jobFor(cacheJobKey(b.Path))})
}

// buildCache starts a job building what of the bundle's derived data is missing; when nothing
// is, it answers at once without one.
func (a *app) buildCache(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	status, err := a.cache.Status(b)
	path, name := b.Path, b.Name
	b.Close()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	answer := cacheAnswer{Path: path, Status: status}
	if !status.Ready {
		spec := map[string]string{"bundle": path}
		job, _, err := a.jobs.Submit("cache", "Derived data of "+name, cacheJobKey(path), spec, a.cacheJob(path))
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		answer.Job = &job
	}
	writeJSON(w, answer)
}

// cacheJob builds a bundle's derived data, then checks what it built.
func (a *app) cacheJob(path string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(path, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		r.Log("info", "Building the derived data of %s.", b.Name)
		status, err := a.cache.Build(ctx, b, func(f float64, message string) { r.Progress(f, message) }, func(format string, args ...any) {
			r.Log("info", format, args...)
		})
		if err != nil {
			return err
		}
		r.Validating()
		r.Progress(1, "Checking what was built")
		if err := a.cache.Check(status); err != nil {
			return fmt.Errorf("%w: %v", jobs.ErrInvalid, err)
		}
		r.Log("info", "The derived data of %s are ready.", b.Name)
		return nil
	}
}

func (a *app) cacheUsage(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	usage, err := a.cache.Usage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, usage)
}

// formatSize writes a size in decimal units, as --cache-limit reads them.
func formatSize(n int64) string {
	switch {
	case n >= 1e12:
		return fmt.Sprintf("%.3g TB", float64(n)/1e12)
	case n >= 1e9:
		return fmt.Sprintf("%.3g GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.3g MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%d bytes", n)
}
