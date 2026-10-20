package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// A figure written by SituCraft (wave 8, slice 1) is read back by the page, which finds its
// manifest in it (web/lib/figure-manifest.js): its SVG, PDF, PNG or JSON bytes, as they are.

// The largest figure file read back.
const maxFigureBytes = 256 << 20

var figureTypes = map[string]string{".svg": "image/svg+xml", ".pdf": "application/pdf", ".png": "image/png", ".json": "application/json"}

func (a *app) registerFigures(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/read-figure", a.readFigureFile)
}

// readFigureFile answers a figure file on this computer (SVG, PDF, PNG or JSON, by its name).
// Only this computer may ask.
func (a *app) readFigureFile(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may read its files.")
		return
	}
	path, err := namedFile(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mime, ok := figureTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, filepath.Base(path)+" is not a figure: SituCraft writes them as .svg, .pdf, .png or .json")
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if info.IsDir() || info.Size() > maxFigureBytes {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s is not a figure file of at most %d MB", filepath.Base(path), maxFigureBytes>>20))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Write(raw)
}
