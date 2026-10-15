package main

// The example datasets (internal/examples): listed, downloaded as a job when asked, removed. The
// list of examples is validation/sources.json, the same the validation suites download.

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"

	"situcraft/internal/examples"
	"situcraft/internal/jobs"
	"situcraft/internal/xenium"
)

//go:embed validation/sources.json
var exampleSources []byte

func newExamples(dir string) *examples.Library {
	sources, err := examples.Parse(exampleSources)
	if err != nil {
		log.Printf("the example list cannot be read: %v", err)
	}
	return &examples.Library{Dir: dir, Sources: sources, UserAgent: "SituCraft/" + version + " (+https://github.com/robert-mcdermott/situcraft)", Free: xenium.FreeBytes}
}

func (a *app) registerExamples(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/examples", func(w http.ResponseWriter, r *http.Request) {
		list := a.examples.List()
		for i := range list {
			if j := a.jobFor(exampleJobKey(list[i].Name)); j != nil {
				list[i].State = "downloading"
			}
		}
		writeJSON(w, map[string]any{"dir": a.examples.Dir, "examples": list})
	})
	mux.HandleFunc("POST /api/examples/{name}", a.downloadExample)
	mux.HandleFunc("DELETE /api/examples/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequest(r) {
			writeError(w, http.StatusForbidden, "Only this computer may remove files.")
			return
		}
		name := r.PathValue("name")
		if a.jobFor(exampleJobKey(name)) != nil {
			writeError(w, http.StatusConflict, "It is being downloaded.")
			return
		}
		if err := a.examples.Remove(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"removed": true})
	})
}

func exampleJobKey(name string) string { return "example\x00" + name }

// downloadExample starts a job downloading an example (or answers the one running).
func (a *app) downloadExample(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may download into its folders.")
		return
	}
	name := r.PathValue("name")
	var title string
	for _, s := range a.examples.Sources {
		if s.Name == name {
			title = s.Title
		}
	}
	if title == "" {
		writeError(w, http.StatusNotFound, "No such example.")
		return
	}
	fn, err := a.exampleJob(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	job, _, err := a.jobs.Submit("example", "Download "+title, exampleJobKey(name), map[string]string{"example": name}, fn)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, job)
}

// exampleJob downloads an example, then opens its bundle.
func (a *app) exampleJob(name string) (jobs.Func, error) {
	var title string
	for _, s := range a.examples.Sources {
		if s.Name == name {
			title = s.Title
		}
	}
	if title == "" {
		return nil, fmt.Errorf("no example %q", name)
	}
	return func(ctx context.Context, run *jobs.Run) error {
		run.Log("info", "Downloading %s into %s.", title, a.examples.Dir)
		path, err := a.examples.Download(ctx, name, func(f float64, message string) { run.Progress(f, message) })
		if err != nil {
			return err
		}
		run.Log("info", "%s is ready at %s; every file checked against its recorded SHA-256.", title, path)
		// The bundle is now on this computer, ready to open.
		a.files.add([]string{path})
		return nil
	}, nil
}
