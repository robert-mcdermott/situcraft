package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Exports to a file (the export action, actions.go): the caller names an absolute path, which is
// checked before the page is asked; the page makes the file and uploads its bytes to
// /api/remote/output/{token}, which writes them there through a temporary file in the same folder,
// renamed into place when complete. Each token takes one upload and lasts as long as its action.
// An existing file is replaced only when the call says overwrite. Adapted from MaveScape 0.8.0
// output.go.

type remoteOutput struct {
	path      string
	overwrite bool
	used      bool
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
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return "", nil, fmt.Errorf("%s is a folder; give a file name", path)
		}
		if !params.Overwrite {
			return "", nil, fmt.Errorf("%s exists; pass overwrite: true to replace it", path)
		}
	}
	token := randomToken()
	h.outputs.mu.Lock()
	if h.outputs.slots == nil {
		h.outputs.slots = map[string]*remoteOutput{}
	}
	h.outputs.slots[token] = &remoteOutput{path: path, overwrite: params.Overwrite}
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
	written, err := writeOutput(slot.path, slot.overwrite, r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"path": slot.path, "bytes": written})
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
