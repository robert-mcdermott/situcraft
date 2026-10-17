package main

// Regions drawn on the tissue are kept in the project (web/lib/regions.js); the program only reads,
// for the page, a GeoJSON file of regions (as QuPath exports its annotations) named by its path on
// this computer, as remote control's import_regions asks.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// maxGeoJSONBytes is the largest GeoJSON file read.
const maxGeoJSONBytes = 64 << 20

func (a *app) registerRegions(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/read-geojson", a.readGeoJSON)
}

// readGeoJSON answers a GeoJSON file on this computer, checked to be JSON. Only this computer may ask.
func (a *app) readGeoJSON(w http.ResponseWriter, r *http.Request) {
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
	if info.IsDir() || info.Size() > maxGeoJSONBytes {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s is not a GeoJSON file of at most %d MB", filepath.Base(path), maxGeoJSONBytes>>20))
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
