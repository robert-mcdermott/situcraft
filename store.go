// Adapted from MaveScape 0.8.0 store.go.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// The project library: projects as JSON documents, and small files they use (imported polygons,
// label tables, reviewer annotations), stored once each under their SHA-256. Source bundles are
// never copied here: a project refers to each by its place on this computer.
type store struct {
	dir string
}

const (
	maxProjectBytes = 512 << 20
	// An imported segmentation's polygons and assignments can reach a few gigabytes.
	maxFileBytes = 4 << 30
)

var (
	projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "SituCraft")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".situcraft")
	}
	return ".situcraft"
}

func platformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func openStore(dir string) (*store, error) {
	if dir == "" {
		return nil, errors.New("no data folder")
	}
	for _, sub := range []string{"projects", "files", "trash"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	return &store{dir: dir}, nil
}

func (s *store) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/library/projects", s.listProjects)
	mux.HandleFunc("GET /api/library/projects/{id}", s.getProject)
	mux.HandleFunc("PUT /api/library/projects/{id}", s.putProject)
	mux.HandleFunc("DELETE /api/library/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/library/files/{sha}", s.getFile)
	mux.HandleFunc("HEAD /api/library/files/{sha}", s.getFile)
	mux.HandleFunc("PUT /api/library/files/{sha}", s.putFile)
	mux.HandleFunc("POST /api/library/files", s.addFile)
	mux.HandleFunc("GET /api/library/has/{sha}", s.hasFile)
	s.registerRecords(mux)
}

// hasFile answers whether the library holds a file, without the 404 a HEAD request would log.
func (s *store) hasFile(w http.ResponseWriter, r *http.Request) {
	path, ok := s.filePath(r.PathValue("sha"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid file hash.")
		return
	}
	_, err := os.Stat(path)
	writeJSON(w, map[string]bool{"exists": err == nil})
}

type projectSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Modified string `json:"modified"`
	Datasets int    `json:"datasets"`
	Size     int64  `json:"size"`
}

func (s *store) projectPath(id string) (string, bool) {
	if !projectIDPattern.MatchString(id) {
		return "", false
	}
	return filepath.Join(s.dir, "projects", id+".json"), true
}

func (s *store) listProjects(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "projects"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list := []projectSummary{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !projectIDPattern.MatchString(id) {
			continue
		}
		summary, err := readSummary(filepath.Join(s.dir, "projects", name))
		if err != nil {
			continue
		}
		summary.ID = id
		list = append(list, summary)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Modified > list[j].Modified })
	writeJSON(w, map[string]any{"projects": list})
}

func readSummary(path string) (projectSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return projectSummary{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return projectSummary{}, err
	}
	var doc struct {
		Name     string            `json:"name"`
		Modified string            `json:"modified"`
		Datasets []json.RawMessage `json:"datasets"`
	}
	if err := json.NewDecoder(file).Decode(&doc); err != nil {
		return projectSummary{}, err
	}
	modified := doc.Modified
	if modified == "" {
		modified = info.ModTime().UTC().Format(time.RFC3339)
	}
	return projectSummary{Name: doc.Name, Modified: modified, Datasets: len(doc.Datasets), Size: info.Size()}, nil
}

func (s *store) getProject(w http.ResponseWriter, r *http.Request) {
	path, ok := s.projectPath(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid project id.")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such project.")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, file)
}

func (s *store) putProject(w http.ResponseWriter, r *http.Request) {
	path, ok := s.projectPath(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid project id.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProjectBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body) > maxProjectBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "The project is too large.")
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "The project is not valid JSON.")
		return
	}
	if err := writeAtomic(path, body); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "size": len(body)})
}

// deleteProject moves the project into trash/ rather than removing it.
func (s *store) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path, ok := s.projectPath(id)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid project id.")
		return
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	target := filepath.Join(s.dir, "trash", fmt.Sprintf("%s-%s.json", id, stamp))
	if err := os.Rename(path, target); err != nil {
		writeError(w, http.StatusNotFound, "No such project.")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "trash": target})
}

func (s *store) filePath(sha string) (string, bool) {
	if !sha256Pattern.MatchString(sha) {
		return "", false
	}
	return filepath.Join(s.dir, "files", sha[:2], sha), true
}

func (s *store) getFile(w http.ResponseWriter, r *http.Request) {
	path, ok := s.filePath(r.PathValue("sha"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid file hash.")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not in the library.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	// Content-addressed: a hash names one content for ever.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

// putFile stores a file under its SHA-256, which it checks while streaming to disk.
func (s *store) putFile(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	path, ok := s.filePath(sha)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid file hash.")
		return
	}
	if _, err := os.Stat(path); err == nil {
		// The upload is read before the answer: answered first, a large one's connection is closed
		// under the browser (Go's server closes it past 256 KB unread), and its request fails.
		io.Copy(io.Discard, io.LimitReader(r.Body, maxFileBytes+1))
		writeJSON(w, map[string]any{"ok": true, "existing": true})
		return
	}
	got, written, _, err := s.storeStream(r.Body, sha)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "sha256": got, "size": written})
}

// addFile stores a file and answers with the SHA-256 it computed while writing it, so the
// browser need not hash (or hold) the file to add it.
func (s *store) addFile(w http.ResponseWriter, r *http.Request) {
	sha, written, existing, err := s.storeStream(r.Body, "")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "sha256": sha, "size": written, "existing": existing})
}

// addLocalFile copies a file named on the command line into the library, on this computer.
func (s *store) addLocalFile(w http.ResponseWriter, path string) {
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "The file can no longer be read.")
		return
	}
	defer file.Close()
	sha, written, existing, err := s.storeStream(file, "")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "sha256": sha, "size": written, "existing": existing})
}

type storeError struct {
	status  int
	message string
}

func (e storeError) Error() string { return e.message }

func writeStoreError(w http.ResponseWriter, err error) {
	if se, ok := err.(storeError); ok {
		writeError(w, se.status, se.message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// storeStream writes content to the library under its SHA-256, computed while writing; with
// `want`, the content must have that hash. A file already present is kept (existing = true).
func (s *store) storeStream(content io.Reader, want string) (sha string, written int64, existing bool, err error) {
	root := filepath.Join(s.dir, "files")
	if err = os.MkdirAll(root, 0o755); err != nil {
		return "", 0, false, err
	}
	temp, err := os.CreateTemp(root, ".upload-*")
	if err != nil {
		return "", 0, false, err
	}
	defer os.Remove(temp.Name())
	hash := sha256.New()
	written, err = io.Copy(io.MultiWriter(temp, hash), io.LimitReader(content, maxFileBytes+1))
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		return "", 0, false, storeError{http.StatusBadRequest, "The upload was interrupted."}
	}
	if written > maxFileBytes {
		return "", 0, false, storeError{http.StatusRequestEntityTooLarge, "The file is too large."}
	}
	sha = hex.EncodeToString(hash.Sum(nil))
	if want != "" && sha != want {
		return "", 0, false, storeError{http.StatusBadRequest, "The content does not match its hash."}
	}
	path, _ := s.filePath(sha)
	if _, statErr := os.Stat(path); statErr == nil {
		return sha, written, true, nil
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, false, err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return "", 0, false, err
	}
	return sha, written, false, nil
}

// writeAtomic replaces path with data so a reader never sees a half-written file.
func writeAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".save-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
