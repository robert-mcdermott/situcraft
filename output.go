package main

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
	"strings"
	"sync"
)

// Exports to a file (the export action, actions.go): the caller names an absolute path, which is
// checked before the page is asked; the page makes the file and uploads its bytes to
// /api/remote/output/{token}, which writes them there through a temporary file in the same folder,
// renamed into place when complete. Each token takes one upload and lasts as long as its action.
// An existing file is replaced only when the call says overwrite. Adapted from MaveScape 0.8.0
// output.go.
//
// A result package (what: package) named by a path not ending in .zip is written as a folder
// (wave 8, slice 8): the page uploads its zip, whose entries lie under one folder, and they are
// written into a temporary folder beside the path, renamed into place when complete. An existing
// folder is replaced only when the call says overwrite and it holds a SituCraft package.

type remoteOutput struct {
	path      string
	overwrite bool
	used      bool
	unpack    bool
}

type outputSlots struct {
	mu    sync.Mutex
	slots map[string]*remoteOutput
}

// prepareOutput checks the action's {path, overwrite} and opens an upload slot. Returns the
// page's upload URL (relative to the page) and a function that closes the slot.
func (h *remoteHub) prepareOutput(args json.RawMessage) (string, func(), error) {
	var params struct {
		Path      string `json:"path"`
		Overwrite bool   `json:"overwrite"`
		What      string `json:"what"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.Path == "" {
		return "", nil, errors.New(`give "path": the absolute path of the file to write`)
	}
	if !filepath.IsAbs(params.Path) {
		return "", nil, fmt.Errorf("%s is not an absolute path", params.Path)
	}
	path := filepath.Clean(params.Path)
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || !dir.IsDir() {
		return "", nil, fmt.Errorf("the folder %s does not exist", filepath.Dir(path))
	}
	unpack := params.What == "package" && !strings.EqualFold(filepath.Ext(path), ".zip")
	if info, err := os.Stat(path); err == nil {
		switch {
		case unpack && !info.IsDir():
			return "", nil, fmt.Errorf("%s is a file; give a folder that does not exist yet (or a path ending in .zip)", path)
		case unpack && !params.Overwrite:
			return "", nil, fmt.Errorf("%s exists; pass overwrite: true to replace the package there", path)
		case unpack && !isPackageFolder(path):
			return "", nil, fmt.Errorf("%s is not a SituCraft package: it is not replaced", path)
		case !unpack && info.IsDir():
			return "", nil, fmt.Errorf("%s is a folder; give a file name", path)
		case !unpack && !params.Overwrite:
			return "", nil, fmt.Errorf("%s exists; pass overwrite: true to replace it", path)
		}
	}
	token := randomToken()
	h.outputs.mu.Lock()
	if h.outputs.slots == nil {
		h.outputs.slots = map[string]*remoteOutput{}
	}
	h.outputs.slots[token] = &remoteOutput{path: path, overwrite: params.Overwrite, unpack: unpack}
	h.outputs.mu.Unlock()
	release := func() {
		h.outputs.mu.Lock()
		delete(h.outputs.slots, token)
		h.outputs.mu.Unlock()
	}
	return "api/remote/output/" + token, release, nil
}

// serveOutput writes an upload to its slot's path and answers {path, bytes}.
func (h *remoteHub) serveOutput(w http.ResponseWriter, r *http.Request) {
	h.outputs.mu.Lock()
	slot := h.outputs.slots[r.PathValue("token")]
	if slot != nil {
		if slot.used {
			slot = nil
		} else {
			slot.used = true
		}
	}
	h.outputs.mu.Unlock()
	if slot == nil {
		writeError(w, http.StatusNotFound, "No export is waiting for this upload.")
		return
	}
	if slot.unpack {
		written, files, err := unpackOutput(slot.path, slot.overwrite, r.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"path": slot.path, "bytes": written, "files": files})
		return
	}
	written, err := writeOutput(slot.path, slot.overwrite, r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"path": slot.path, "bytes": written})
}

// isPackageFolder says whether a folder holds a SituCraft result package (its manifest.json says so).
func isPackageFolder(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return false
	}
	var m struct {
		Format string `json:"format"`
	}
	return json.Unmarshal(data, &m) == nil && m.Format == "situcraft-package"
}

// unpackOutput writes a package's zip (its entries under one folder) as a folder at path. Answers
// the zip's bytes and the files written.
func unpackOutput(path string, overwrite bool, body io.Reader) (int64, int, error) {
	data, err := io.ReadAll(io.LimitReader(body, 4<<30))
	if err != nil {
		return 0, 0, err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, 0, fmt.Errorf("the package is not a zip: %v", err)
	}
	temp, err := os.MkdirTemp(filepath.Dir(path), ".situcraft-export-")
	if err != nil {
		return 0, 0, fmt.Errorf("cannot write in %s: %v", filepath.Dir(path), err)
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(temp)
		}
	}()
	if err := os.Chmod(temp, 0o755); err != nil {
		return 0, 0, err
	}
	files := 0
	for _, f := range z.File {
		// Each entry lies under the package's folder, which the path names.
		_, rel, ok := strings.Cut(f.Name, "/")
		if !ok || rel == "" || f.FileInfo().IsDir() {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return 0, 0, fmt.Errorf("the package holds %s, outside its folder", f.Name)
		}
		if f.UncompressedSize64 > 1<<30 {
			return 0, 0, fmt.Errorf("the package holds %s of %d bytes", f.Name, f.UncompressedSize64)
		}
		target := filepath.Join(temp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, 0, err
		}
		if err := writeEntry(f, target); err != nil {
			return 0, 0, fmt.Errorf("%s: %v", f.Name, err)
		}
		files++
	}
	if _, err := os.Stat(path); err == nil {
		if !overwrite || !isPackageFolder(path) {
			return 0, 0, fmt.Errorf("%s was created meanwhile; not replaced", path)
		}
		old := temp + ".old"
		if err := os.Rename(path, old); err != nil {
			return 0, 0, err
		}
		defer os.RemoveAll(old)
	}
	if err := os.Rename(temp, path); err != nil {
		return 0, 0, err
	}
	keep = true
	return int64(len(data)), files, nil
}

func writeEntry(f *zip.File, target string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(r, 1<<30))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func writeOutput(path string, overwrite bool, body io.Reader) (int64, error) {
	temp, err := createOutputTemp(filepath.Dir(path))
	if err != nil {
		return 0, fmt.Errorf("cannot write in %s: %v", filepath.Dir(path), err)
	}
	name := temp.Name()
	written, err := io.Copy(temp, body)
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil && !overwrite {
		if _, statErr := os.Stat(path); statErr == nil {
			err = fmt.Errorf("%s was created meanwhile; not replaced", path)
		}
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
		return 0, err
	}
	return written, nil
}

// createOutputTemp opens a new temporary file in dir with the permissions any other new file
// gets (0666 less the umask), so the output is not left readable by its owner only, as
// os.CreateTemp's files are.
func createOutputTemp(dir string) (*os.File, error) {
	for attempt := 0; attempt < 100; attempt++ {
		name := filepath.Join(dir, ".situcraft-export-"+randomToken())
		file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil || !os.IsExist(err) {
			return file, err
		}
	}
	return nil, errors.New("could not create a temporary file")
}
