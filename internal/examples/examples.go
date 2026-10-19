// Package examples downloads SituCraft's example datasets on request: public Xenium output bundles,
// each from its publisher's address, with its license and attribution (validation/sources.json,
// embedded in the program). An archive is checked by its size, its publisher's MD5 and its
// SHA-256; only the bundle's files are extracted, each checked against its recorded size and
// SHA-256 (a member claiming more is refused, as is a name leaving the folder); then the archive
// is deleted, so an example costs only its bundle's size. Nothing is downloaded unless asked.
//
// An example may instead be made on this computer (Generated): SituCraft's simulated tissue, with
// its truth, nothing downloaded.
package examples

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// File is one file of an example bundle.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Source is one example dataset.
type Source struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Summary describes the example to the person choosing it.
	Summary string `json:"summary"`
	Source  string `json:"source"`
	// Publisher and Page attribute the data (the methods paragraph cites them).
	Publisher  string `json:"publisher"`
	Page       string `json:"page"`
	License    string `json:"license"`
	LicenseURL string `json:"licenseUrl"`
	Archive    struct {
		URL    string `json:"url"`
		Size   int64  `json:"size"`
		MD5    string `json:"md5"`
		SHA256 string `json:"sha256"`
	} `json:"archive"`
	Files []File `json:"files"`
}

// Parse reads sources.json.
func Parse(raw []byte) ([]Source, error) {
	var doc struct {
		Datasets map[string]Source `json:"datasets"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var out []Source
	for name, s := range doc.Datasets {
		s.Name = name
		if s.Archive.URL == "" || len(s.Files) == 0 {
			return nil, fmt.Errorf("%s: no archive or files", name)
		}
		out = append(out, s)
	}
	// The smaller first: the Lung bundle before the Breast one.
	sort.Slice(out, func(i, j int) bool { return out[i].Archive.Size < out[j].Archive.Size })
	return out, nil
}

// Generated is an example made on this computer rather than downloaded.
type Generated struct {
	Name, Title, Summary string
	// Size is about the bytes it takes on disk.
	Size int64
	// Make writes it into dir, which must not exist yet (written beside it, then moved).
	Make func(ctx context.Context, dir string, progress Progress) error
	// Current says whether one made before is as this SituCraft makes it (nil: always); one that is
	// not is listed as outdated, with Outdated saying what making it again adds.
	Current  func(dir string) bool
	Outdated string
	// Marker is the file whose presence says it is made (experiment.xenium if empty), and Kind
	// what it is: "" a bundle, "cohort" a folder of bundles (wave 7).
	Marker, Kind string
}

// Library is the examples folder.
type Library struct {
	Dir       string
	Sources   []Source
	Generated []Generated
	Client    *http.Client
	// UserAgent names SituCraft to the publisher's server.
	UserAgent string
	// Free reports the free space in a folder (the cache's helper); nil skips the check.
	Free func(dir string) (int64, error)
}

// State is an example as the page shows it.
type State struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Source     string `json:"source"`
	Publisher  string `json:"publisher"`
	Page       string `json:"page"`
	License    string `json:"license"`
	LicenseURL string `json:"licenseUrl"`
	// ManifestSHA256 is experiment.xenium's SHA-256: a dataset whose fingerprint has it is this
	// example, wherever its bundle is.
	ManifestSHA256 string `json:"manifestSha256"`
	Download       int64  `json:"download"` // bytes to download
	Size           int64  `json:"size"`     // bytes on disk once extracted
	// State is "absent", "ready" (every file there with its size, verified by checksum when
	// downloaded) or "incomplete".
	State string `json:"state"`
	Path  string `json:"path,omitempty"`
	// Generated: made on this computer, nothing downloaded.
	Generated bool `json:"generated,omitempty"`
	// Outdated: a generated example made by an earlier SituCraft, and what making it again adds.
	Outdated string `json:"outdated,omitempty"`
	// Kind: "cohort" for a folder of bundles, a simulated cohort; absent for a bundle.
	Kind string `json:"kind,omitempty"`
}

func (s Source) diskSize() int64 {
	var n int64
	for _, f := range s.Files {
		n += f.Size
	}
	return n
}

func (l *Library) generated(name string) (Generated, bool) {
	for _, g := range l.Generated {
		if g.Name == name {
			return g, true
		}
	}
	return Generated{}, false
}

func (l *Library) source(name string) (Source, bool) {
	for _, s := range l.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

// List describes every example. A folder is "ready" when every file is there with its size
// (checking each file's SHA-256 is what downloading does).
func (l *Library) List() []State {
	out := make([]State, 0, len(l.Sources))
	for _, s := range l.Sources {
		st := State{Name: s.Name, Title: s.Title, Summary: s.Summary, Source: s.Source, Publisher: s.Publisher, Page: s.Page, License: s.License, LicenseURL: s.LicenseURL, Download: s.Archive.Size, Size: s.diskSize(), State: "absent"}
		dir := filepath.Join(l.Dir, s.Name)
		present, complete := 0, true
		for _, f := range s.Files {
			if f.Path == "experiment.xenium" {
				st.ManifestSHA256 = f.SHA256
			}
			info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err == nil && info.Size() == f.Size {
				present++
			} else {
				complete = false
			}
		}
		switch {
		case complete:
			st.State, st.Path = "ready", dir
		case present > 0:
			st.State, st.Path = "incomplete", dir
		}
		out = append(out, st)
	}
	// A generated example is ready once made; its experiment.xenium (or its marker) names it.
	for _, g := range l.Generated {
		st := State{Name: g.Name, Title: g.Title, Summary: g.Summary, Size: g.Size, State: "absent", Generated: true, Kind: g.Kind}
		dir := filepath.Join(l.Dir, g.Name)
		marker := g.Marker
		if marker == "" {
			marker = "experiment.xenium"
		}
		if raw, err := os.ReadFile(filepath.Join(dir, marker)); err == nil {
			sum := sha256.Sum256(raw)
			st.ManifestSHA256, st.State, st.Path = hex.EncodeToString(sum[:]), "ready", dir
			if g.Current != nil && !g.Current(dir) {
				st.Outdated = g.Outdated
			}
		}
		out = append(out, st)
	}
	return out
}

// Progress receives the fraction done and what is being done.
type Progress func(fraction float64, message string)

// Download fetches an example, checks it and extracts its bundle, answering the bundle's folder.
// A cancelled or failed download leaves nothing behind.
func (l *Library) Download(ctx context.Context, name string, progress Progress) (string, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if g, ok := l.generated(name); ok {
		if err := os.MkdirAll(l.Dir, 0o755); err != nil {
			return "", err
		}
		target := filepath.Join(l.Dir, name)
		if err := os.RemoveAll(target); err != nil {
			return "", err
		}
		if err := g.Make(ctx, target, progress); err != nil {
			return "", err
		}
		progress(1, "Ready")
		return target, nil
	}
	s, ok := l.source(name)
	if !ok {
		return "", fmt.Errorf("no example %q", name)
	}
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return "", err
	}
	need := s.Archive.Size + s.diskSize() + 256<<20
	if l.Free != nil {
		if free, err := l.Free(l.Dir); err == nil && free < need {
			return "", fmt.Errorf("%s needs about %d MB free in %s while it downloads; %d MB are free", s.Title, need>>20, l.Dir, free>>20)
		}
	}
	archive := filepath.Join(l.Dir, "."+name+".zip.part")
	staging := filepath.Join(l.Dir, "."+name+".part")
	defer os.Remove(archive)
	defer os.RemoveAll(staging)
	if err := l.fetch(ctx, s, archive, func(f float64) { progress(0.8*f, "Downloading") }); err != nil {
		return "", err
	}
	if err := extract(ctx, s, archive, staging, func(f float64) { progress(0.8+0.2*f, "Checking and extracting its files") }); err != nil {
		return "", err
	}
	marker, _ := json.MarshalIndent(map[string]any{"example": name, "archiveSHA256": s.Archive.SHA256, "verified": time.Now().UTC().Format(time.RFC3339), "source": s.Source, "license": s.License}, "", "  ")
	if err := os.WriteFile(filepath.Join(staging, ".situcraft-example.json"), marker, 0o644); err != nil {
		return "", err
	}
	target := filepath.Join(l.Dir, name)
	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	if err := os.Rename(staging, target); err != nil {
		return "", err
	}
	progress(1, "Ready")
	return target, nil
}

// fetch downloads the archive, checking its size, MD5 and SHA-256.
func (l *Library) fetch(ctx context.Context, s Source, dst string, progress func(float64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Archive.URL, nil)
	if err != nil {
		return err
	}
	if l.UserAgent != "" {
		req.Header.Set("User-Agent", l.UserAgent)
	}
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", s.Title, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: the server answered %s", s.Title, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	sumMD5, sumSHA := md5.New(), sha256.New()
	var got int64
	buf := make([]byte, 1<<20)
	body := io.LimitReader(resp.Body, s.Archive.Size+1)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			sumMD5.Write(buf[:n])
			sumSHA.Write(buf[:n])
			got += int64(n)
			progress(float64(got) / float64(max(s.Archive.Size, 1)))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("downloading %s: %w", s.Title, err)
		}
	}
	if got != s.Archive.Size {
		return fmt.Errorf("the archive is %d bytes, not the %d its publisher lists", got, s.Archive.Size)
	}
	if hex.EncodeToString(sumMD5.Sum(nil)) != s.Archive.MD5 || hex.EncodeToString(sumSHA.Sum(nil)) != s.Archive.SHA256 {
		return errors.New("the archive does not match its recorded checksums: it was not used")
	}
	return f.Sync()
}

// extract writes the archive's bundle files into dir, each checked against its record. The bundle
// may be at the archive's root or in one folder inside it.
func extract(ctx context.Context, s Source, archive, dir string, progress func(float64)) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("the archive cannot be read: %w", err)
	}
	defer zr.Close()
	wanted := map[string]File{}
	var total int64
	for _, f := range s.Files {
		wanted[f.Path] = f
		total += f.Size
	}
	// The folder holding the bundle: where experiment.xenium is.
	prefix := ""
	for _, m := range zr.File {
		if name := path.Clean("/" + m.Name)[1:]; path.Base(name) == "experiment.xenium" && strings.Count(name, "/") <= 1 {
			if d := path.Dir(name); d != "." {
				prefix = d + "/"
			}
		}
	}
	var done int64
	found := map[string]bool{}
	for _, m := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasSuffix(m.Name, "/") {
			continue
		}
		if strings.Contains(m.Name, "\\") || strings.HasPrefix(m.Name, "/") || strings.Split(m.Name, "/")[0] == ".." || strings.Contains(m.Name, "/../") {
			return fmt.Errorf("the archive names a file outside its folder (%s): it was not used", m.Name)
		}
		rel := strings.TrimPrefix(path.Clean(m.Name), prefix)
		f, ok := wanted[rel]
		if !ok {
			continue
		}
		if m.UncompressedSize64 != uint64(f.Size) {
			return fmt.Errorf("%s is %d bytes in the archive, not %d", rel, m.UncompressedSize64, f.Size)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyChecked(m, target, f); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		found[rel] = true
		done += f.Size
		progress(float64(done) / float64(max(total, 1)))
	}
	for _, f := range s.Files {
		if !found[f.Path] {
			return fmt.Errorf("the archive lacks %s", f.Path)
		}
	}
	return nil
}

func copyChecked(m *zip.File, target string, f File) error {
	rc, err := m.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, sum), io.LimitReader(rc, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(sum.Sum(nil)) != f.SHA256 {
		return errors.New("its size or SHA-256 does not match the record")
	}
	return nil
}

// Clean removes what an interrupted download left (its partial archive and staging folder).
func (l *Library) Clean(name string) {
	if _, ok := l.source(name); !ok {
		return
	}
	os.Remove(filepath.Join(l.Dir, "."+name+".zip.part"))
	os.RemoveAll(filepath.Join(l.Dir, "."+name+".part"))
}

// Remove deletes a downloaded example.
func (l *Library) Remove(name string) error {
	_, generated := l.generated(name)
	if _, ok := l.source(name); !ok && !generated {
		return fmt.Errorf("no example %q", name)
	}
	return os.RemoveAll(filepath.Join(l.Dir, name))
}
