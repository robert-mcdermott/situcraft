package main

// Project archives (.scz): a project to keep or hand on, written where the user says. An archive
// is a zip holding project.json (the project document) and a README. Each dataset keeps its place
// on this computer and gains its place relative to the archive (archivePath), so that a folder
// holding the archive and its bundles can be moved or copied and still open: opening an archive
// takes a bundle from beside it first, from its recorded place otherwise. The bundles themselves
// are never in the archive.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	projectEntry = "project.json"
	// An archive is a project document (at most maxProjectBytes, as the library takes) deflated.
	maxArchiveBytes = maxProjectBytes
)

const archiveReadme = `This is a SituCraft project archive. Open it with SituCraft (situcraft <this file>, or Open a bundle
in its window). It holds the project document (project.json): its datasets, filters, saved views
and history. The datasets' bundles are not inside: each is found beside the archive where it was
when the archive was written (its archivePath), or else where the project recorded it.
`

func (a *app) registerProject(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/project-archive", a.writeProjectArchive)
	mux.HandleFunc("GET /api/local/{index}/project", a.readProjectArchive)
}

// writeProjectArchive writes {path, project, overwrite} to path (absolute, ending .scz).
func (a *app) writeProjectArchive(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may write files.")
		return
	}
	var body struct {
		Path      string                     `json:"path"`
		Project   map[string]json.RawMessage `json:"project"`
		Overwrite bool                       `json:"overwrite"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxProjectBytes)).Decode(&body); err != nil || body.Project == nil {
		writeError(w, http.StatusBadRequest, "Send {\"path\": …, \"project\": {…}}.")
		return
	}
	path := strings.TrimSpace(body.Path)
	if !filepath.IsAbs(path) {
		writeError(w, http.StatusBadRequest, "Give the archive's full path (it is written on this computer).")
		return
	}
	if !strings.EqualFold(filepath.Ext(path), ".scz") {
		path += ".scz"
	}
	if _, err := os.Stat(path); err == nil && !body.Overwrite {
		writeError(w, http.StatusConflict, path+" already exists.")
		return
	}
	project, err := withArchivePaths(body.Project, filepath.Dir(path))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{projectEntry, project}, {"README.txt", []byte(archiveReadme)}} {
		out, err := zw.Create(f.name)
		if err == nil {
			_, err = out.Write(f.data)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := writeAtomically(path, buf.Bytes()); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, map[string]any{"path": path, "bytes": buf.Len()})
}

// withArchivePaths adds to each dataset its place relative to the archive's folder.
func withArchivePaths(project map[string]json.RawMessage, dir string) ([]byte, error) {
	var datasets []map[string]any
	if raw, ok := project["datasets"]; ok {
		if err := json.Unmarshal(raw, &datasets); err != nil {
			return nil, errors.New("the project's datasets are not a list")
		}
	}
	for _, d := range datasets {
		place, _ := d["path"].(string)
		delete(d, "archivePath")
		if place == "" || !filepath.IsAbs(place) {
			continue
		}
		if rel, err := filepath.Rel(dir, place); err == nil {
			d["archivePath"] = filepath.ToSlash(rel)
		}
	}
	raw, err := json.Marshal(datasets)
	if err != nil {
		return nil, err
	}
	if datasets != nil {
		project["datasets"] = raw
	}
	return json.MarshalIndent(project, "", "  ")
}

func writeAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".situcraft-*.tmp")
	if err != nil {
		return err
	}
	// An archive is for sharing: readable as any new file is (the temporary file is private).
	if err := tmp.Chmod(0o644); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// readProjectArchive answers {path, project}: the archive's project with each dataset's place
// resolved, beside the archive when its bundle is there.
func (a *app) readProjectArchive(w http.ResponseWriter, r *http.Request) {
	file, ok := a.files.get(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "No such local item.")
		return
	}
	if file.Kind != "project" || file.isDir {
		writeError(w, http.StatusBadRequest, file.Name+" is not a SituCraft project archive.")
		return
	}
	project, err := openProjectArchive(file.path)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s: %v", file.Name, err))
		return
	}
	writeJSON(w, map[string]any{"path": file.path, "project": project})
}

func openProjectArchive(path string) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxArchiveBytes {
		return nil, errors.New("too large to be a project archive")
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("not a readable zip")
	}
	defer zr.Close()
	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == projectEntry {
			entry = f
		}
	}
	if entry == nil {
		return nil, errors.New("it holds no project.json: not a SituCraft project archive")
	}
	if entry.UncompressedSize64 > maxProjectBytes {
		return nil, errors.New("its project is too large")
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxProjectBytes+1))
	rc.Close()
	if err != nil || len(raw) > maxProjectBytes {
		return nil, errors.New("its project cannot be read")
	}
	var project map[string]any
	if err := json.Unmarshal(raw, &project); err != nil {
		return nil, errors.New("its project.json is not JSON")
	}
	datasets, _ := project["datasets"].([]any)
	dir := filepath.Dir(path)
	for _, d := range datasets {
		dataset, ok := d.(map[string]any)
		if !ok {
			continue
		}
		rel, _ := dataset["archivePath"].(string)
		if rel == "" {
			continue
		}
		beside := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(beside); err == nil {
			dataset["path"] = beside
		}
	}
	return project, nil
}
