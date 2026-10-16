// Package artifacts keeps SituCraft's artifacts: what a job makes (a segmentation, later labels and
// comparisons), or what it registers from a dataset's bundle (the instrument's segmentation), each
// with its lineage (parents, the job and its spec, the method and its version, the parameters, the
// software), the SHA-256 of every file it consists of, the findings of its check, and its state.
//
// An artifact is never changed once made. A job writes its outputs into a staging folder; Promote
// checks them (the kind's check) and records them: an artifact whose check finds a fatal problem
// is kept as invalid, for review, and cannot be used. The store is a folder:
//
//	<dir>/<id>/manifest.json   an artifact (its files beside it)
//	<dir>/staging/<id>/        outputs being written, not yet an artifact
//
// A file of an artifact either lies in its folder or is a file of the dataset's bundle (Location
// "bundle"), named by its path in the bundle and recorded by its SHA-256, never copied.
package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// State is what can be done with an artifact.
type State string

const (
	// Complete: checked, and usable as an input or a project's default.
	Complete State = "complete"
	// Invalid: its check found a fatal problem; kept for review, never used.
	Invalid State = "invalid"
)

// Where an artifact's file lies.
const (
	InArtifact = ""       // in the artifact's folder
	InBundle   = "bundle" // a file of the dataset's bundle, by its path there
)

// File is one file of an artifact.
type File struct {
	Path     string `json:"path"`
	Role     string `json:"role"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Location string `json:"location,omitempty"`
}

// Parent is what an artifact was made from: a dataset (by its bundle's run and fingerprint), or
// another artifact.
type Parent struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// Job is the job that made an artifact, with its spec (every effective parameter).
type Job struct {
	ID   string          `json:"id"`
	Kind string          `json:"kind"`
	Spec json.RawMessage `json:"spec,omitempty"`
}

// Method is how an artifact was made, with the publication it follows.
type Method struct {
	Name     string          `json:"name"`
	Version  string          `json:"version,omitempty"`
	Citation string          `json:"citation,omitempty"`
	Detail   json.RawMessage `json:"detail,omitempty"`
}

// Software is a program that took part in making an artifact.
type Software struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
}

// Lineage is where an artifact comes from.
type Lineage struct {
	Parents    []Parent        `json:"parents"`
	Job        *Job            `json:"job,omitempty"`
	Method     Method          `json:"method"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	Software   []Software      `json:"software"`
}

// Finding is what a check found: "fatal" makes the artifact invalid; "warning" and "info" are
// recorded with it.
type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Count    int    `json:"count,omitempty"`
}

// Manifest describes an artifact (manifest.json).
type Manifest struct {
	Format   string    `json:"format"`
	Version  int       `json:"version"`
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Name     string    `json:"name"`
	Dataset  string    `json:"dataset,omitempty"`
	State    State     `json:"state"`
	Created  time.Time `json:"created"`
	Lineage  Lineage   `json:"lineage"`
	Files    []File    `json:"files"`
	Findings []Finding `json:"findings"`
	// Summary is what the kind says of it (a segmentation's cells and assigned transcripts).
	Summary json.RawMessage `json:"summary,omitempty"`
}

const (
	manifestName = "manifest.json"
	format       = "situcraft-artifact"
	version      = 1
)

// Check examines an artifact's staged files (in dir) and its manifest before it is recorded,
// answering what it finds; it may fill the manifest's Summary.
type Check func(dir string, m *Manifest) []Finding

var (
	// ErrNotFound: no artifact with that id.
	ErrNotFound = errors.New("no such artifact")
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)
)

// Store is a folder of artifacts.
type Store struct {
	dir string
}

// Open opens (making it if needed) the store in dir.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("no folder for artifacts")
	}
	if err := os.MkdirAll(filepath.Join(dir, "staging"), 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir is the store's folder.
func (s *Store) Dir() string { return s.dir }

func checkID(id string) error {
	if !idPattern.MatchString(id) || id == "staging" {
		return fmt.Errorf("%q is not an artifact id", id)
	}
	return nil
}

// Stage makes an empty staging folder for an artifact's outputs (replacing an earlier one) and
// answers its path.
func (s *Store) Stage(id string) (string, error) {
	if err := checkID(id); err != nil {
		return "", err
	}
	dir := filepath.Join(s.dir, "staging", id)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// Restage renames a staging folder, for outputs whose artifact id is known only once they are
// written (an import, named by the checksums of what it read).
func (s *Store) Restage(from, to string) error {
	if err := checkID(from); err != nil {
		return err
	}
	if err := checkID(to); err != nil {
		return err
	}
	dst := filepath.Join(s.dir, "staging", to)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(filepath.Join(s.dir, "staging", from), dst)
}

// Discard removes an artifact's staging folder (outputs of a job that will not be promoted).
func (s *Store) Discard(id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.dir, "staging", id))
}

// Promote records an artifact: the files of its staging folder (each listed with its role in
// m.Files, with Location InArtifact) are checksummed, files of the bundle are taken as m.Files
// gives them (their SHA-256 known to the caller), the kind's check runs, and the folder becomes
// the artifact, complete or (when the check finds a fatal problem) invalid. Promoting an id that
// is already an artifact with the same files answers the existing one; with other files, it is
// refused. A staging folder need not exist for an artifact of bundle files alone.
func (s *Store) Promote(m Manifest, check Check) (Manifest, error) {
	if err := checkID(m.ID); err != nil {
		return Manifest{}, err
	}
	if m.Kind == "" || m.Name == "" {
		return Manifest{}, errors.New("an artifact needs a kind and a name")
	}
	staging := filepath.Join(s.dir, "staging", m.ID)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return Manifest{}, err
	}
	m.Format, m.Version = format, version
	if m.Created.IsZero() {
		m.Created = time.Now().UTC()
	}
	m.Findings = []Finding{}
	listed := map[string]bool{}
	for i := range m.Files {
		f := &m.Files[i]
		if f.Location == InBundle {
			if f.SHA256 == "" {
				return Manifest{}, fmt.Errorf("the bundle's %s has no recorded SHA-256", f.Path)
			}
			continue
		}
		if err := checkPath(f.Path); err != nil {
			return Manifest{}, err
		}
		size, sum, err := hashFile(filepath.Join(staging, filepath.FromSlash(f.Path)))
		if err != nil {
			return Manifest{}, fmt.Errorf("the output %s: %w", f.Path, err)
		}
		f.Size, f.SHA256 = size, sum
		listed[f.Path] = true
	}
	// A staged file the manifest does not list would be an artifact's file nobody can verify.
	err := filepath.WalkDir(staging, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(staging, path)
		if !listed[filepath.ToSlash(rel)] {
			return fmt.Errorf("the output %s is not listed among the artifact's files", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	sort.SliceStable(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	if check != nil {
		m.Findings = append(m.Findings, check(staging, &m)...)
	}
	m.State = Complete
	for _, f := range m.Findings {
		if f.Severity == "fatal" {
			m.State = Invalid
		}
	}
	target := filepath.Join(s.dir, m.ID)
	if existing, err := s.Get(m.ID); err == nil {
		if sameFiles(existing.Files, m.Files) {
			os.RemoveAll(staging)
			return existing, nil
		}
		return Manifest{}, fmt.Errorf("artifact %s already exists with other files", m.ID)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := writeFile(filepath.Join(staging, manifestName), append(raw, '\n')); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(staging, target); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Get reads an artifact's manifest.
func (s *Store) Get(id string) (Manifest, error) {
	if err := checkID(id); err != nil {
		return Manifest{}, err
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, id, manifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, ErrNotFound
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil || m.Format != format {
		return Manifest{}, fmt.Errorf("artifact %s: its manifest cannot be read", id)
	}
	if m.Version > version {
		return Manifest{}, fmt.Errorf("artifact %s was made by a newer SituCraft (version %d)", id, m.Version)
	}
	return m, nil
}

// Trash moves an artifact's folder into trash (made if needed) as <id>-<time>, and answers where
// it went. Nothing else changes: a project that still names the artifact finds it missing.
func (s *Store) Trash(id, trash string, now time.Time) (string, error) {
	if err := checkID(id); err != nil {
		return "", err
	}
	from := filepath.Join(s.dir, id)
	if _, err := os.Stat(filepath.Join(from, manifestName)); errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return "", err
	}
	to := filepath.Join(trash, id+"-"+now.UTC().Format("20060102T150405Z"))
	return to, os.Rename(from, to)
}

// List reads every artifact's manifest, oldest first.
func (s *Store) List() ([]Manifest, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() || checkID(e.Name()) != nil {
			continue
		}
		if m, err := s.Get(e.Name()); err == nil {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}

// Path is where an artifact's own file lies.
func (s *Store) Path(id, file string) (string, error) {
	if err := checkID(id); err != nil {
		return "", err
	}
	if err := checkPath(file); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, id, filepath.FromSlash(file)), nil
}

// Verify checksums an artifact's files again: those in its folder, and the bundle's through
// openBundle (nil skips them). It answers what differs; nothing when all agree.
func (s *Store) Verify(id string, openBundle func(path string) (io.ReadCloser, error)) ([]Finding, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, f := range m.Files {
		var size int64
		var sum string
		switch {
		case f.Location == InBundle && openBundle == nil:
			continue
		case f.Location == InBundle:
			r, err := openBundle(f.Path)
			if err != nil {
				out = append(out, Finding{Severity: "fatal", Code: "artifact.file-missing", Message: fmt.Sprintf("The bundle's %s cannot be read: %v", f.Path, err)})
				continue
			}
			size, sum, err = hashReader(r)
			r.Close()
			if err != nil {
				return nil, err
			}
		default:
			path := filepath.Join(s.dir, id, filepath.FromSlash(f.Path))
			size, sum, err = hashFile(path)
			if err != nil {
				out = append(out, Finding{Severity: "fatal", Code: "artifact.file-missing", Message: fmt.Sprintf("%s cannot be read: %v", f.Path, err)})
				continue
			}
		}
		if sum != f.SHA256 || (f.Size > 0 && size != f.Size) {
			out = append(out, Finding{Severity: "fatal", Code: "artifact.file-changed", Message: fmt.Sprintf("%s is not the file the artifact recorded (its SHA-256 differs)", f.Path)})
		}
	}
	return out, nil
}

// checkPath refuses a file path that would leave the artifact's folder.
func checkPath(p string) error {
	if p == "" || strings.Contains(p, "\\") || strings.HasPrefix(p, "/") || p == manifestName {
		return fmt.Errorf("%q is not a file name an artifact may have", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%q is not a file name an artifact may have", p)
		}
	}
	return nil
}

func sameFiles(a, b []File) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].SHA256 != b[i].SHA256 || a[i].Location != b[i].Location {
			return false
		}
	}
	return true
}

func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	return hashReader(f)
}

func hashReader(r io.Reader) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// writeFile writes through a temporary file renamed into place.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
