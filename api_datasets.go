package main

// Xenium output bundles named on this computer (local.go): what one holds, which is quick to
// read, and its validation (internal/xenium). Bundles are read where they are and never written.

import (
	"errors"
	"io/fs"
	"net/http"

	"situcraft/internal/xenium"
)

func (a *app) registerDatasets(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/bundle", a.describeBundle)
	mux.HandleFunc("GET /api/local/{index}/validation", a.validateBundle)
}

// bundleDescription is what the page shows of a bundle before (or without) validating it.
type bundleDescription struct {
	Name           string                   `json:"name"`
	Path           string                   `json:"path"`
	Zipped         bool                     `json:"zipped"`
	Version        string                   `json:"version"`
	Support        string                   `json:"support"`
	SupportMessage string                   `json:"supportMessage"`
	Experiment     *xenium.Experiment       `json:"experiment"`
	Inventory      []xenium.Item            `json:"inventory"`
	Fingerprint    []xenium.FileFingerprint `json:"fingerprint"`
}

// openBundle opens the local item at the request's index as a bundle, or answers with why not.
func (a *app) openBundle(w http.ResponseWriter, r *http.Request) (*xenium.Bundle, bool) {
	file, ok := a.files.get(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "No such local item.")
		return nil, false
	}
	if file.Kind != "bundle" && file.Kind != "archive" {
		writeError(w, http.StatusBadRequest, file.Name+" is not a Xenium output bundle.")
		return nil, false
	}
	b, err := xenium.Open(file.path, xenium.Options{CacheDir: a.cacheDir})
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, fs.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return nil, false
	}
	return b, true
}

func (a *app) describeBundle(w http.ResponseWriter, r *http.Request) {
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	level, message := b.Support()
	writeJSON(w, bundleDescription{
		Name: b.Name, Path: b.Path, Zipped: b.Zipped, Version: b.Version.String(), Support: level, SupportMessage: message,
		Experiment: b.Experiment, Inventory: b.Inventory(), Fingerprint: b.Fingerprint(),
	})
}

func (a *app) validateBundle(w http.ResponseWriter, r *http.Request) {
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	report, err := xenium.Validate(r.Context(), b, xenium.ValidateOptions{CacheDir: a.cacheDir})
	if err != nil {
		// The request was cancelled (the page went away).
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"path": b.Path, "zipped": b.Zipped, "report": report})
}
