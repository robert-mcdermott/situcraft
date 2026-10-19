package main

// The Open dialog's folders (GET /api/browse): a page cannot learn where a file is on this
// computer (its own file picker hands it the file's bytes, never its place), and a bundle is read
// where it is, so the program lists this computer's folders for the page to walk. Only this
// computer may ask, as only it may open files (openPaths).

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	// The most entries a folder answers with (the rest are counted, not listed).
	maxBrowseEntries = 5000
	// The most bundles in a folder whose manifests are read for their run and region.
	maxBrowseManifests = 300
)

// browseEntry is one thing in a folder: its name and place, what SituCraft reads it as (a folder
// to walk into; a bundle, a Zarr store, an archive, a project, an image, a table, polygons or a
// matrix to open; or another file), its size and when it was changed, and for a bundle what its
// manifest says.
type browseEntry struct {
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Kind     string        `json:"kind"`
	Size     int64         `json:"size,omitempty"`
	Modified string        `json:"modified,omitempty"`
	Hidden   bool          `json:"hidden,omitempty"`
	Bundle   *browseBundle `json:"bundle,omitempty"`
	Dir      bool          `json:"dir,omitempty"`
}

// browseBundle is what a bundle's manifest (experiment.xenium) names.
type browseBundle struct {
	Run      string `json:"run,omitempty"`
	Region   string `json:"region,omitempty"`
	Panel    string `json:"panel,omitempty"`
	Software string `json:"software,omitempty"`
	Cells    *int64 `json:"cells,omitempty"`
}

// browsePlace is a place to start from: the home folder, its Desktop, Documents and Downloads,
// the examples' folder, the computer's disks.
type browsePlace struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// browseAnswer is a folder's listing.
type browseAnswer struct {
	Path      string        `json:"path"`
	Name      string        `json:"name"`
	Parent    string        `json:"parent,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	Bundle    *browseBundle `json:"bundle,omitempty"`
	Select    string        `json:"select,omitempty"`
	Entries   []browseEntry `json:"entries"`
	More      int           `json:"more,omitempty"`
	Separator string        `json:"separator"`
	Home      string        `json:"home,omitempty"`
	Places    []browsePlace `json:"places"`
}

func (a *app) browse(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may look at its folders.")
		return
	}
	q := r.URL.Query()
	answer, status, err := browseFolder(q.Get("path"), q.Get("hidden") == "1")
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	answer.Places = a.browsePlaces()
	writeJSON(w, answer)
}

// browseFolder lists a folder (the home folder when path is empty). A file's path lists its
// folder with the file chosen (Select). Hidden entries (a name starting with a dot) are left out
// unless asked for.
func browseFolder(path string, hidden bool) (*browseAnswer, int, error) {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(path) == "" {
		if home == "" {
			return nil, http.StatusNotFound, errors.New("this computer names no home folder: type a folder's path")
		}
		path = home
	}
	abs, err := absPath(path)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, browseStatus(err), browseError(abs, err)
	}
	answer := &browseAnswer{Separator: string(filepath.Separator), Home: home, Entries: []browseEntry{}}
	if !info.IsDir() {
		answer.Select = filepath.Base(abs)
		abs = filepath.Dir(abs)
	}
	answer.Path, answer.Name = abs, filepath.Base(abs)
	if parent := filepath.Dir(abs); parent != abs {
		answer.Parent = parent
	}
	if kind := dirKind(abs); kind != "" {
		answer.Kind = kind
		if kind == "bundle" {
			answer.Bundle = readBrowseBundle(abs)
		}
	}
	list, err := os.ReadDir(abs)
	if err != nil {
		return nil, browseStatus(err), browseError(abs, err)
	}
	manifests := 0
	for _, entry := range list {
		name := entry.Name()
		dot := strings.HasPrefix(name, ".")
		if dot && !hidden {
			continue
		}
		if len(answer.Entries) >= maxBrowseEntries {
			answer.More++
			continue
		}
		full := filepath.Join(abs, name)
		e := browseEntry{Name: name, Path: full, Hidden: dot}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		// A link is what it points to.
		if info.Mode()&fs.ModeSymlink != 0 {
			if target, err := os.Stat(full); err == nil {
				info = target
			}
		}
		e.Modified = info.ModTime().UTC().Format(time.RFC3339)
		if info.IsDir() {
			e.Dir = true
			e.Kind = "folder"
			if kind := dirKind(full); kind != "" {
				e.Kind = kind
				if kind == "bundle" && manifests < maxBrowseManifests {
					manifests++
					e.Bundle = readBrowseBundle(full)
				}
			}
		} else {
			e.Size = info.Size()
			e.Kind = fileKind(name)
			if e.Kind == "" || e.Kind == "bundle" {
				// experiment.xenium stands for its folder, which is opened as the bundle.
				e.Kind = "file"
			}
		}
		answer.Entries = append(answer.Entries, e)
	}
	// Folders (bundles, stores and plain folders) first, then files, each in natural order.
	sort.SliceStable(answer.Entries, func(i, j int) bool {
		a, b := answer.Entries[i], answer.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return naturalLess(a.Name, b.Name)
	})
	return answer, http.StatusOK, nil
}

// readBrowseBundle reads what a bundle's manifest names (nil when it cannot be read).
func readBrowseBundle(dir string) *browseBundle {
	f, err := os.Open(filepath.Join(dir, bundleManifest))
	if err != nil {
		return nil
	}
	defer f.Close()
	var m struct {
		RunName    string `json:"run_name"`
		RegionName string `json:"region_name"`
		PanelName  string `json:"panel_name"`
		Software   string `json:"analysis_sw_version"`
		NumCells   *int64 `json:"num_cells"`
	}
	if json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&m) != nil {
		return nil
	}
	return &browseBundle{Run: m.RunName, Region: m.RegionName, Panel: m.PanelName, Software: m.Software, Cells: m.NumCells}
}

// browseStatus is the status a folder that cannot be read answers with.
func browseStatus(err error) int {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, fs.ErrPermission):
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}

// browseError says why a folder cannot be read, and on macOS where to allow it.
func browseError(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("nothing is at " + path)
	case errors.Is(err, fs.ErrPermission):
		msg := "SituCraft may not read " + path
		if runtime.GOOS == "darwin" {
			msg += ": allow it in System Settings, Privacy & Security, Files and Folders (or Full Disk Access)"
		}
		return errors.New(msg)
	}
	return errors.New(path + ": " + err.Error())
}

// browsePlaces are the places to start from that exist on this computer.
func (a *app) browsePlaces() []browsePlace {
	var out []browsePlace
	add := func(name, path, kind string) {
		if path == "" {
			return
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			for _, p := range out {
				if p.Path == path {
					return
				}
			}
			out = append(out, browsePlace{Name: name, Path: path, Kind: kind})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		add("Home", home, "home")
		add("Desktop", filepath.Join(home, "Desktop"), "folder")
		add("Documents", filepath.Join(home, "Documents"), "folder")
		add("Downloads", filepath.Join(home, "Downloads"), "folder")
	}
	if a.examples != nil {
		add("SituCraft's examples", a.examples.Dir, "examples")
	}
	for _, disk := range disks() {
		add(disk.Name, disk.Path, "disk")
	}
	return out
}

// disks are this computer's disks and mounted volumes: the drives on Windows; the root and the
// mounted volumes elsewhere.
func disks() []browsePlace {
	var out []browsePlace
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if _, err := os.Stat(root); err == nil {
				out = append(out, browsePlace{Name: string(c) + ":", Path: root})
			}
		}
		return out
	}
	out = append(out, browsePlace{Name: "This computer", Path: "/"})
	var mounts []string
	user := os.Getenv("USER")
	switch runtime.GOOS {
	case "darwin":
		mounts = []string{"/Volumes"}
	default:
		mounts = []string{"/mnt", "/media"}
		if user != "" {
			mounts = append(mounts, filepath.Join("/media", user), filepath.Join("/run/media", user))
		}
	}
	for _, m := range mounts {
		list, err := os.ReadDir(m)
		if err != nil {
			continue
		}
		for _, e := range list {
			full := filepath.Join(m, e.Name())
			// The startup disk appears in /Volumes as a link to the root, which is listed already;
			// /media/<user> holds the user's volumes, listed from it.
			if target, err := filepath.EvalSymlinks(full); err != nil || target == "/" || strings.HasPrefix(e.Name(), ".") || (m == "/media" && e.Name() == user) {
				continue
			}
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				out = append(out, browsePlace{Name: e.Name(), Path: full})
			}
		}
	}
	return out
}
