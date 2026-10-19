package main

// The example datasets (internal/examples): listed, downloaded as a job when asked, removed. The
// list of examples is validation/sources.json, the same the validation suites download.

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"situcraft/internal/examples"
	"situcraft/internal/jobs"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

//go:embed validation/sources.json
var exampleSources []byte

func newExamples(dir string) *examples.Library {
	sources, err := examples.Parse(exampleSources)
	if err != nil {
		log.Printf("the example list cannot be read: %v", err)
	}
	return &examples.Library{Dir: dir, Sources: sources, UserAgent: "SituCraft/" + version + " (+https://github.com/robert-mcdermott/situcraft)", Free: xenium.FreeBytes,
		Generated: []examples.Generated{{
			Name:    simulatedExample,
			Title:   "A simulated tissue with known truth",
			Summary: "SituCraft's standard simulated tissue: 500 × 500 µm, about 3,000 cells of four types and 440,000 transcripts, every cell, nucleus and molecule known, with an H&E and an immunofluorescence image of the section stained after the run. Made on this computer in seconds; its truth is beside it, so any segmentation, label or alignment can be scored.",
			Size:    43 << 20,
			Make: func(ctx context.Context, dir string, progress examples.Progress) error {
				p := simulate.Presets["standard"]
				p.PostXenium = simulate.DefaultPostXenium()
				_, err := simulate.Run(ctx, p, dir, version, func(f float64, message string) { progress(f, message) })
				return err
			},
			// From 0.5.0 it has post-Xenium images; made again, its run, cells and transcripts are
			// the same.
			Current: func(dir string) bool {
				_, err := os.Stat(filepath.Join(dir, simulate.PostXeniumFolder, simulate.HEImageName))
				return err == nil
			},
			Outdated: "Made by an earlier SituCraft, without the H&E and immunofluorescence image of the section: make it again to add them (its run, cells and transcripts stay the same).",
		}, {
			Name:    cohortExample,
			Title:   "A simulated cohort",
			Summary: "SituCraft's standard simulated cohort: eight sections of 250 × 250 µm, four subjects in each of two groups (control and treated) on two slides, about 6,000 cells in all. In the treated sections a cell is twice as likely to be endothelial-like (its odds against each other type, in every part of the tissue), four times as likely to be immune-like where epithelial-like cells lead, and epithelial-like cells hold 2.5 times the SIM033; every section also differs from the others by chance. Made on this computer in seconds; each sample's subject and condition are read in with it, and the truth of every effect and sample is beside it.",
			Size:    64 << 20,
			Make: func(ctx context.Context, dir string, progress examples.Progress) error {
				_, err := simulate.RunCohort(ctx, simulate.CohortPresets["standard"], dir, version, func(f float64, message string) { progress(f, message) })
				return err
			},
			Marker: simulate.CohortFile,
			Kind:   "cohort",
		}}}
}

// simulatedExample and cohortExample are the generated examples' names.
const (
	simulatedExample = "simulated-standard"
	cohortExample    = "simulated-cohort"
)

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

// exampleTitle is an example's title ("" for none), and whether it is made here rather than
// downloaded.
func (a *app) exampleTitle(name string) (title string, generated bool) {
	for _, s := range a.examples.Sources {
		if s.Name == name {
			return s.Title, false
		}
	}
	for _, g := range a.examples.Generated {
		if g.Name == name {
			return g.Title, true
		}
	}
	return "", false
}

// downloadExample starts a job downloading an example (or answers the one running).
func (a *app) downloadExample(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may download into its folders.")
		return
	}
	name := r.PathValue("name")
	title, generated := a.exampleTitle(name)
	if title == "" {
		writeError(w, http.StatusNotFound, "No such example.")
		return
	}
	fn, err := a.exampleJob(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	verb := "Download "
	if generated {
		verb = "Make "
	}
	job, _, err := a.jobs.Submit("example", verb+title, exampleJobKey(name), map[string]string{"example": name}, fn)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, job)
}

// exampleJob downloads an example, then opens its bundle.
func (a *app) exampleJob(name string) (jobs.Func, error) {
	title, made := a.exampleTitle(name)
	if title == "" {
		return nil, fmt.Errorf("no example %q", name)
	}
	return func(ctx context.Context, run *jobs.Run) error {
		if made {
			run.Log("info", "Making %s in %s.", title, a.examples.Dir)
		} else {
			run.Log("info", "Downloading %s into %s.", title, a.examples.Dir)
		}
		path, err := a.examples.Download(ctx, name, func(f float64, message string) { run.Progress(f, message) })
		if err != nil {
			return err
		}
		if made {
			run.Log("info", "%s is ready at %s.", title, path)
		} else {
			run.Log("info", "%s is ready at %s; every file checked against its recorded SHA-256.", title, path)
		}
		// The bundle is now on this computer, ready to open.
		a.files.add([]string{path})
		return nil
	}, nil
}
