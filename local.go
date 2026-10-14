// Adapted from MaveScape 0.8.0 local.go.

package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// An item named on the command line (or by a later launch), or found in a folder named there: a
// file, or a folder SituCraft reads as one thing (a Xenium output bundle, a Zarr store). Folder is
// the name of the folder it was found in, so the page can make a group of it.
type localFile struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Folder string `json:"folder,omitempty"`
	Kind   string `json:"kind"`
	// Dir: a folder, read where it is by the bundle reader (wave 1, slice 3), never served whole.
	Dir bool `json:"dir,omitempty"`
	// Path is where it is on this computer (the page records datasets by it; it never leaves
	// this computer).
	Path  string `json:"path"`
	path  string
	isDir bool
}

type localFiles struct {
	mu    sync.RWMutex
	files []localFile
	index map[string]int
}

const (
	// A folder of bundles and their companion files rarely holds more than a few hundred items;
	// the limit only stops a mistaken folder (the home folder) from being opened.
	maxFolderFiles = 20000
	// A folder walk stops after this many entries, so the home folder does not walk for ever.
	maxFolderEntries = 200000
	// The file that makes a folder a Xenium output bundle (10x's run manifest).
	bundleManifest = "experiment.xenium"
)

func newLocalFiles() *localFiles {
	return &localFiles{index: map[string]int{}}
}

// fileKind classifies a file SituCraft can open by its name; "" means it cannot.
func fileKind(name string) string {
	lower := strings.ToLower(name)
	has := func(suffixes ...string) bool {
		for _, suffix := range suffixes {
			if strings.HasSuffix(lower, suffix) {
				return true
			}
		}
		return false
	}
	switch {
	case lower == bundleManifest:
		// The manifest stands for its bundle (dirKind).
		return "bundle"
	case has(".scz"):
		return "project"
	case has(".zarr.zip"):
		// A zipped Zarr store: a SpatialData export, or a bundle's cells.zarr.zip on its own.
		return "zarr"
	case has(".zip"):
		// A zipped output bundle, or another archive (wave 1, slice 3 says which).
		return "archive"
	case has(".ome.tif", ".ome.tiff", ".tif", ".tiff", ".ome.btf"):
		// Morphology images, post-Xenium H&E and IF.
		return "image"
	case has(".geojson", ".geojson.gz"):
		// Cell polygons, ROIs and annotations.
		return "geometry"
	case has(".parquet", ".csv", ".csv.gz", ".tsv", ".tsv.gz"):
		// Transcripts, cells, labels, assignments, metadata.
		return "table"
	case has(".mtx", ".mtx.gz"):
		return "matrix"
	}
	return ""
}

// dirKind classifies a folder SituCraft reads as one thing; "" means its contents are looked at.
func dirKind(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".zarr") {
		return "zarr"
	}
	if isFile(filepath.Join(path, bundleManifest)) {
		return "bundle"
	}
	if isFile(filepath.Join(path, ".zgroup")) || isFile(filepath.Join(path, "zarr.json")) {
		return "zarr"
	}
	return ""
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func absPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "~" || strings.HasPrefix(name, "~/") || strings.HasPrefix(name, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			name = filepath.Join(home, name[1:])
		}
	}
	return filepath.Abs(name)
}

// add registers items and the openable items inside folders; it returns what it skipped.
func (l *localFiles) add(paths []string) []string {
	var problems []string
	var found []localFile
	for _, raw := range paths {
		if strings.TrimSpace(raw) == "" {
			// An empty path would silently mean the current folder: refuse it.
			problems = append(problems, "skipping an empty path")
			continue
		}
		path, err := absPath(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("skipping %s: %v", raw, err))
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("skipping %s: %v", raw, err))
			continue
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), bundleManifest) {
			// experiment.xenium named on its own: its bundle is meant.
			path = filepath.Dir(path)
			info, err = os.Stat(path)
			if err != nil {
				problems = append(problems, fmt.Sprintf("skipping %s: %v", raw, err))
				continue
			}
		}
		if info.IsDir() {
			if kind := dirKind(path); kind != "" {
				found = append(found, localFile{Name: info.Name(), Kind: kind, Dir: true, path: path, isDir: true})
				continue
			}
			items, problem := walkFolder(path)
			if problem != "" {
				problems = append(problems, problem)
			}
			if len(items) == 0 && problem == "" {
				problems = append(problems, fmt.Sprintf("skipping %s: no bundles or files SituCraft opens in the folder", raw))
			}
			found = append(found, items...)
			continue
		}
		kind := fileKind(info.Name())
		if kind == "" {
			problems = append(problems, fmt.Sprintf("skipping %s: not a file SituCraft opens", raw))
			continue
		}
		found = append(found, localFile{Name: info.Name(), Size: info.Size(), Kind: kind, path: path})
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, file := range found {
		if _, known := l.index[file.path]; known {
			continue
		}
		index := len(l.files)
		file.URL = "/api/local/" + strconv.Itoa(index)
		file.Path = file.path
		l.index[file.path] = index
		l.files = append(l.files, file)
	}
	return problems
}

// walkFolder finds the bundles, Zarr stores and openable files in a folder. A bundle or store is
// one item: the walk does not go inside it.
func walkFolder(dir string) ([]localFile, string) {
	folder := filepath.Base(dir)
	var files []localFile
	visited := 0
	problem := ""
	filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > maxFolderEntries {
			problem = fmt.Sprintf("%s: stopped after %d entries", dir, maxFolderEntries)
			return filepath.SkipAll
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") && path != dir {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFolderFiles {
			problem = fmt.Sprintf("%s: opened the first %d items", dir, maxFolderFiles)
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if path == dir {
				return nil
			}
			if kind := dirKind(path); kind != "" {
				files = append(files, localFile{Name: name, Kind: kind, Folder: folder, Dir: true, path: path, isDir: true})
				return filepath.SkipDir
			}
			return nil
		}
		kind := fileKind(name)
		if kind == "" || kind == "bundle" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		files = append(files, localFile{Name: name, Size: info.Size(), Kind: kind, Folder: folder, path: path})
		return nil
	})
	sort.SliceStable(files, func(i, j int) bool { return naturalLess(files[i].path, files[j].path) })
	return files, problem
}

// within answers the known items the paths name: each path's own item (a bundle by its folder or
// its experiment.xenium), or the items found inside a folder it names.
func (l *localFiles) within(paths []string) []localFile {
	var roots []string
	for _, raw := range paths {
		path, err := absPath(raw)
		if err != nil {
			continue
		}
		if strings.EqualFold(filepath.Base(path), bundleManifest) {
			path = filepath.Dir(path)
		}
		roots = append(roots, filepath.Clean(path))
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out []localFile
	for _, file := range l.files {
		for _, root := range roots {
			if file.path == root || strings.HasPrefix(file.path, root+string(filepath.Separator)) {
				out = append(out, file)
				break
			}
		}
	}
	return out
}

func (l *localFiles) list() []localFile {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]localFile, len(l.files))
	copy(out, l.files)
	return out
}

// get is the local item with this index.
func (l *localFiles) get(index string) (localFile, bool) {
	i, err := strconv.Atoi(index)
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err != nil || i < 0 || i >= len(l.files) {
		return localFile{}, false
	}
	return l.files[i], true
}

func (l *localFiles) serve(w http.ResponseWriter, r *http.Request) {
	file, ok := l.get(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "No such local file.")
		return
	}
	if file.isDir {
		writeError(w, http.StatusBadRequest, file.Name+" is a folder: SituCraft reads it where it is.")
		return
	}
	handle, err := os.Open(file.path)
	if err != nil {
		writeError(w, http.StatusNotFound, "The file can no longer be read.")
		return
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The file can no longer be read.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, file.Name, info.ModTime(), handle)
}

// naturalLess orders "region2" before "region10" and "slide 2" before "slide 10", as runs and
// samples are usually numbered.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		la, lb := lowerByte(ca), lowerByte(cb)
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}
