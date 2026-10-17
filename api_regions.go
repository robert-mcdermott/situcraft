package main

// Regions drawn on the tissue are kept in the project (web/lib/regions.js); the program only reads,
// for the page, a JSON file named by its path on this computer: a GeoJSON file of regions (as QuPath
// exports its annotations), as remote control's import_regions asks, or a simulation's truth, as a
// worked example asks.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// maxJSONBytes is the largest JSON file read.
const maxJSONBytes = 64 << 20

func (a *app) registerRegions(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/read-json", a.readJSONFile)
}

// readJSONFile answers a JSON file on this computer, checked to be JSON. Only this computer may ask.
func (a *app) readJSONFile(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may read its files.")
		return
	}
	path, err := namedFile(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if info.IsDir() || info.Size() > maxJSONBytes {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s is not a JSON file of at most %d MB", filepath.Base(path), maxJSONBytes>>20))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !json.Valid(raw) {
		writeError(w, http.StatusUnprocessableEntity, filepath.Base(path)+" is not JSON")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
