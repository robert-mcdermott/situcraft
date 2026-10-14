// Adapted from MaveScape 0.8.0 records.go.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Records the library keeps across projects, as JSON documents by kind: marker sets, segmentation
// presets, color schemes and other reusable pieces. Like projects, they are written atomically
// and deleted into trash/.

const maxRecordBytes = 64 << 20

var recordKindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

type recordSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Modified string `json:"modified"`
	Size     int64  `json:"size"`
}

func (s *store) registerRecords(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/library/records/{kind}", s.listRecords)
	mux.HandleFunc("GET /api/library/records/{kind}/{id}", s.getRecord)
	mux.HandleFunc("PUT /api/library/records/{kind}/{id}", s.putRecord)
	mux.HandleFunc("DELETE /api/library/records/{kind}/{id}", s.deleteRecord)
}

func (s *store) recordDir(kind string) (string, bool) {
	if !recordKindPattern.MatchString(kind) {
		return "", false
	}
	return filepath.Join(s.dir, "records", kind), true
}

func (s *store) recordPath(kind, id string) (string, bool) {
	dir, ok := s.recordDir(kind)
	if !ok || !projectIDPattern.MatchString(id) {
		return "", false
	}
	return filepath.Join(dir, id+".json"), true
}

func (s *store) listRecords(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.recordDir(r.PathValue("kind"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record kind.")
		return
	}
	list := []recordSummary{}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".json")
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || !projectIDPattern.MatchString(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		summary := recordSummary{ID: id, Modified: info.ModTime().UTC().Format(time.RFC3339), Size: info.Size()}
		if file, err := os.Open(filepath.Join(dir, name)); err == nil {
			var doc struct {
				Name     string `json:"name"`
				Modified string `json:"modified"`
			}
			if json.NewDecoder(file).Decode(&doc) == nil {
				summary.Name = doc.Name
				if doc.Modified != "" {
					summary.Modified = doc.Modified
				}
			}
			file.Close()
		}
		list = append(list, summary)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Modified > list[j].Modified })
	writeJSON(w, map[string]any{"records": list})
}

func (s *store) getRecord(w http.ResponseWriter, r *http.Request) {
	path, ok := s.recordPath(r.PathValue("kind"), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record.")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such record.")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, file)
}

func (s *store) putRecord(w http.ResponseWriter, r *http.Request) {
	path, ok := s.recordPath(r.PathValue("kind"), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRecordBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body) > maxRecordBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "The record is too large.")
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "The record is not valid JSON.")
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := writeAtomic(path, body); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "size": len(body)})
}

// deleteRecord moves the record into trash/ rather than removing it.
func (s *store) deleteRecord(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	path, ok := s.recordPath(kind, id)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record.")
		return
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	target := filepath.Join(s.dir, "trash", fmt.Sprintf("%s-%s-%s.json", kind, id, stamp))
	if err := os.Rename(path, target); err != nil {
		writeError(w, http.StatusNotFound, "No such record.")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "trash": target})
}
