// Package xenium reads 10x Genomics Xenium output bundles, as written by Xenium Onboard Analysis
// (XOA) and Xenium Ranger, from a folder or a zip, without ever writing to them: the run manifest,
// metrics and panel, the transcripts, cells, cell and nucleus boundaries, the cell-by-feature
// matrix and the morphology images. It inventories a bundle's files by role and validates it
// (validate.go). File roles and contents follow 10x Genomics' XOA output documentation and the
// bundles in hand (validation/sources.json).
package xenium

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
	"strings"
)

const (
	manifestName = "experiment.xenium"
	// SimulationFile, at a bundle's root, says the bundle is simulated (SituCraft's simulator
	// writes it): its cells and molecules were made, with their truth, not measured.
	SimulationFile = "situcraft-simulation.json"
	// A folder with more files than this is not an output bundle (a mistaken home folder).
	maxBundleFiles = 100000
	// Small text files (manifest, metrics, panel) are read whole up to this size.
	maxSmallFile = 64 << 20
)

// Options configure how a bundle is opened.
type Options struct {
	// CacheDir receives zip members that must be inflated for random access (defaults to the
	// system's temporary folder).
	CacheDir string
}

// Bundle is an open output bundle.
type Bundle struct {
	// Path is the folder or zip it was opened from.
	Path   string
	Zipped bool
	// Name is the folder's (or archive's) base name.
	Name       string
	Experiment *Experiment
	Version    Version
	// Metrics and Panel are nil when the bundle has none (or they cannot be read; see Problems).
	Metrics *Metrics
	Panel   *Panel
	// Simulation is the simulation that made the bundle (nil for a measured one).
	Simulation *Simulation
	Files      []FileInfo
	// Problems are what could not be read while opening (each also becomes a finding).
	Problems []string
	src      source
	byPath   map[string]FileInfo
}

// Open opens the bundle at path: a folder holding experiment.xenium, that file itself, or a zip of
// the folder. It fails only when there is no readable manifest.
func Open(p string, opts Options) (*Bundle, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && strings.EqualFold(info.Name(), manifestName) {
		p = filepath.Dir(p)
		if info, err = os.Stat(p); err != nil {
			return nil, err
		}
	}
	b := &Bundle{Path: p, Name: strings.TrimSuffix(info.Name(), ".zip"), byPath: map[string]FileInfo{}}
	if info.IsDir() {
		s, err := openDir(p)
		if err != nil {
			return nil, err
		}
		b.src = s
	} else {
		cache := opts.CacheDir
		if cache == "" {
			cache = os.TempDir()
		}
		s, err := openZip(p, cache)
		if err != nil {
			return nil, err
		}
		b.src, b.Zipped = s, true
	}
	b.Files = b.src.files()
	for _, f := range b.Files {
		b.byPath[f.Path] = f
	}
	raw, err := b.ReadSmall(manifestName)
	if errors.Is(err, fs.ErrNotExist) {
		b.Close()
		return nil, fmt.Errorf("%s holds no %s: not a Xenium output bundle", p, manifestName)
	}
	if err != nil {
		b.Close()
		return nil, err
	}
	if b.Experiment, err = parseExperiment(raw); err != nil {
		b.Close()
		return nil, err
	}
	b.Version, _ = ParseVersion(b.Experiment.AnalysisSoftware)
	if b.Has("metrics_summary.csv") {
		if raw, err := b.ReadSmall("metrics_summary.csv"); err != nil {
			b.Problems = append(b.Problems, err.Error())
		} else if b.Metrics, err = parseMetrics(raw); err != nil {
			b.Problems = append(b.Problems, err.Error())
		}
	}
	if b.Has(SimulationFile) {
		if raw, err := b.ReadSmall(SimulationFile); err != nil {
			b.Problems = append(b.Problems, err.Error())
		} else if b.Simulation, err = parseSimulation(raw); err != nil {
			b.Problems = append(b.Problems, err.Error())
		}
	}
	if b.Has("gene_panel.json") {
		if raw, err := b.ReadSmall("gene_panel.json"); err != nil {
			b.Problems = append(b.Problems, err.Error())
		} else if b.Panel, err = parsePanel(raw); err != nil {
			b.Problems = append(b.Problems, err.Error())
		}
	}
	return b, nil
}

// Close releases the bundle's files.
func (b *Bundle) Close() error {
	if b.src == nil {
		return nil
	}
	return b.src.close()
}

// Has reports whether the bundle holds a file.
func (b *Bundle) Has(name string) bool {
	_, ok := b.byPath[name]
	return ok
}

// Stat describes one of the bundle's files.
func (b *Bundle) Stat(name string) (FileInfo, bool) {
	f, ok := b.byPath[name]
	return f, ok
}

// OpenFile opens one of the bundle's files for random access.
func (b *Bundle) OpenFile(name string) (ReaderAt, error) {
	if !b.Has(name) {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	return b.src.open(name)
}

// FileIdentity names the current state of one of the bundle's files without reading it: its
// place, size and modification time (for a zipped bundle, the archive's, and the member's size
// and CRC-32). The same identity means the same bytes unless something rewrote the file and
// reset its time; the derived-data cache keys its record of each file's checksum on it.
func (b *Bundle) FileIdentity(name string) (string, error) {
	if !b.Has(name) {
		return "", fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	return b.src.identity(name)
}

// ReadSmall reads a small file whole.
func (b *Bundle) ReadSmall(name string) ([]byte, error) {
	f, err := b.OpenFile(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if f.Size() > maxSmallFile {
		return nil, fmt.Errorf("%s is %d bytes, too large to be what its name says", name, f.Size())
	}
	return io.ReadAll(io.NewSectionReader(f, 0, f.Size()))
}

// Support says how far SituCraft reads this bundle's version.
func (b *Bundle) Support() (level, message string) {
	v := b.Version
	switch {
	case b.Simulation != nil:
		return "simulated", fmt.Sprintf("Simulated by %s: every cell and molecule was made, not measured, and the files are laid out as Xenium Onboard Analysis 2 lays out its bundles. The truth is in %s/.", b.Simulation.Software, b.Simulation.Truth)
	case v.Raw == "":
		return "unknown", "The bundle does not say which software wrote it (analysis_sw_version); it is read as XOA 2.x."
	case v.Major == 2:
		return "supported", fmt.Sprintf("Written by %s %s: read and checked against XOA 2.0 bundles.", softwareName(v), v)
	case v.Major == 1 || v.Major >= 3:
		return "best-effort", fmt.Sprintf("Written by %s %s: read best-effort; SituCraft has been checked on XOA 2.0 bundles only so far.", softwareName(v), v)
	}
	return "best-effort", fmt.Sprintf("Written by %s: read best-effort.", v.Raw)
}

func softwareName(v Version) string {
	if strings.Contains(v.Software, "ranger") {
		return "Xenium Ranger"
	}
	return "Xenium Onboard Analysis"
}

// Item is one role a bundle's files play, and which files fill it.
type Item struct {
	Role        string   `json:"role"`        // "transcripts", "cells", …
	Description string   `json:"description"` // what it is, in plain words
	Required    bool     `json:"required"`    // the bundle cannot be used without it
	Files       []string `json:"files"`       // the bundle's files filling the role (empty when missing)
	Size        int64    `json:"size"`
	// Format names the form found ("Parquet", "Zarr", "Matrix Market in a .tar.gz", …).
	Format string `json:"format,omitempty"`
	// Note says what SituCraft makes of a form it reads only partly or not at all.
	Note string `json:"note,omitempty"`
}

// Inventory lists the roles of a bundle's files, required and optional, and the files nothing
// claims ("Other").
func (b *Bundle) Inventory() []Item {
	claimed := map[string]bool{}
	var items []Item
	add := func(role, description string, required bool, candidates ...string) *Item {
		item := Item{Role: role, Description: description, Required: required}
		for _, c := range candidates {
			if b.Has(c) {
				item.Files = append(item.Files, c)
				item.Size += b.byPath[c].Size
				claimed[c] = true
			}
		}
		items = append(items, item)
		return &items[len(items)-1]
	}
	add("manifest", "Run manifest (experiment.xenium)", true, manifestName).Format = "JSON"
	add("metrics", "Run metrics", false, "metrics_summary.csv").Format = "CSV"
	add("panel", "Gene panel", false, "gene_panel.json").Format = "JSON"
	t := add("transcripts", "Decoded transcripts", true, "transcripts.parquet")
	t.Format = "Parquet"
	if len(t.Files) == 0 && b.Has("transcripts.csv.gz") {
		t.Files, t.Format, t.Note = []string{"transcripts.csv.gz"}, "CSV", "Only the CSV form is present; SituCraft reads the Parquet form."
	}
	c := add("cells", "Cells (centroids, counts, areas)", true, "cells.parquet")
	c.Format = "Parquet"
	if len(c.Files) == 0 && b.Has("cells.csv.gz") {
		c.Files, c.Format, c.Note = []string{"cells.csv.gz"}, "CSV", "Only the CSV form is present; SituCraft reads the Parquet form."
	}
	add("cell_boundaries", "Cell boundaries", true, "cell_boundaries.parquet").Format = "Parquet"
	add("nucleus_boundaries", "Nucleus boundaries", true, "nucleus_boundaries.parquet").Format = "Parquet"
	m := add("matrix", "Cell-by-feature counts", true)
	switch {
	case b.Has("cell_feature_matrix.zarr.zip"):
		m.Files, m.Format = []string{"cell_feature_matrix.zarr.zip"}, "Zarr"
	case b.Has("cell_feature_matrix/matrix.mtx.gz"):
		m.Files, m.Format = []string{"cell_feature_matrix/matrix.mtx.gz", "cell_feature_matrix/features.tsv.gz", "cell_feature_matrix/barcodes.tsv.gz"}, "Matrix Market"
	case b.Has("cell_feature_matrix.tar.gz"):
		m.Files, m.Format = []string{"cell_feature_matrix.tar.gz"}, "Matrix Market in a .tar.gz"
	case b.Has("cell_feature_matrix.h5"):
		m.Files, m.Format, m.Note = []string{"cell_feature_matrix.h5"}, "HDF5", "Only the HDF5 form is present; SituCraft reads the Zarr and Matrix Market forms."
	}
	for _, f := range m.Files {
		m.Size += b.byPath[f].Size
	}
	for _, f := range []string{"cell_feature_matrix.zarr.zip", "cell_feature_matrix.tar.gz", "cell_feature_matrix.h5"} {
		if b.Has(f) {
			claimed[f] = true
		}
	}
	for _, f := range b.Files {
		if strings.HasPrefix(f.Path, "cell_feature_matrix/") {
			claimed[f.Path] = true
		}
	}
	focus := add("morphology_focus", "Morphology images, in focus (DAPI and stains)", true)
	focus.Format = "OME-TIFF (JPEG 2000)"
	if b.Simulation != nil {
		focus.Format = "OME-TIFF (deflate)"
	}
	for _, f := range b.focusFiles() {
		focus.Files = append(focus.Files, f)
		focus.Size += b.byPath[f].Size
		claimed[f] = true
	}
	add("morphology", "Morphology z-stack", false, b.experimentImage(b.Experiment.Images.Morphology, "morphology.ome.tif")).Format = "OME-TIFF (JPEG 2000)"
	add("morphology_mip", "Maximum-intensity projection", false, "morphology_mip.ome.tif").Format = "OME-TIFF"
	add("cells_zarr", "Cells and segmentation masks (Xenium Explorer)", false, "cells.zarr.zip").Format = "Zarr"
	add("transcripts_zarr", "Transcripts by location (Xenium Explorer)", false, "transcripts.zarr.zip").Format = "Zarr"
	add("analysis", "Secondary analysis (clusters, projections, differential expression)", false, "analysis.zarr.zip", "analysis.tar.gz")
	add("summary", "Web summary", false, "analysis_summary.html").Format = "HTML"
	add("csv", "CSV copies of the tables", false, "transcripts.csv.gz", "cells.csv.gz", "cell_boundaries.csv.gz", "nucleus_boundaries.csv.gz").Format = "CSV"
	add("aux", "Auxiliary outputs", false, "aux_outputs.tar.gz")
	if b.Simulation != nil {
		add("simulation", "The simulation's settings and what it made", false, SimulationFile).Format = "JSON"
		truth := add("truth", "The simulation's truth (true cells, their types, every molecule's origin)", false)
		prefix := strings.TrimSuffix(b.Simulation.Truth, "/") + "/"
		for _, f := range b.Files {
			if strings.HasPrefix(f.Path, prefix) {
				truth.Files = append(truth.Files, f.Path)
				truth.Size += f.Size
				claimed[f.Path] = true
			}
		}
	}
	other := Item{Role: "other", Description: "Other files"}
	for _, f := range b.Files {
		if !claimed[f.Path] && !strings.HasPrefix(f.Path, "analysis/") && !strings.HasPrefix(f.Path, "aux_outputs/") {
			other.Files = append(other.Files, f.Path)
			other.Size += f.Size
		}
	}
	if len(other.Files) > 0 {
		items = append(items, other)
	}
	return items
}

// Simulation is situcraft-simulation.json: what made a simulated bundle.
type Simulation struct {
	Format    string `json:"format"`
	Simulated bool   `json:"simulated"`
	Software  string `json:"software"`
	// Truth is the folder (within the bundle) holding the truth, laid out as an output bundle of
	// the same run.
	Truth string `json:"truth"`
	// Seed is the simulation's seed (in its parameters).
	Parameters struct {
		Seed uint64 `json:"seed"`
	} `json:"parameters"`
}

func parseSimulation(raw []byte) (*Simulation, error) {
	var s Simulation
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", SimulationFile, err)
	}
	if s.Format != "situcraft-simulation" || !s.Simulated {
		return nil, fmt.Errorf("%s does not describe a SituCraft simulation", SimulationFile)
	}
	if s.Truth == "" || strings.Contains(s.Truth, "..") || strings.HasPrefix(s.Truth, "/") {
		s.Truth = "truth"
	}
	return &s, nil
}

// experimentImage is the image file the manifest names, if the bundle has it, else the default.
func (b *Bundle) experimentImage(named, fallback string) string {
	if named != "" && b.Has(named) {
		return named
	}
	return fallback
}

// focusFiles are the morphology-focus image files: XOA 2 writes morphology_focus/ with one file per
// channel; XOA 1 one morphology_focus.ome.tif.
func (b *Bundle) focusFiles() []string {
	var out []string
	for _, f := range b.Files {
		if strings.HasPrefix(f.Path, "morphology_focus/") && strings.HasSuffix(strings.ToLower(f.Path), ".ome.tif") {
			out = append(out, f.Path)
		}
	}
	if len(out) == 0 && b.Has("morphology_focus.ome.tif") {
		out = append(out, "morphology_focus.ome.tif")
	}
	return out
}

// FileFingerprint identifies one file of a bundle as it was when added to a project.
type FileFingerprint struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	// SHA256 of the small description files (manifest, metrics, panel), which name the run.
	SHA256 string `json:"sha256,omitempty"`
}

// Fingerprint identifies the bundle: every file's size and time, and the description files'
// SHA-256 (a project notices a bundle that changed or moved; slice 4's cache keys on it).
func (b *Bundle) Fingerprint() []FileFingerprint {
	out := make([]FileFingerprint, 0, len(b.Files))
	for _, f := range b.Files {
		fp := FileFingerprint{Path: f.Path, Size: f.Size, ModTime: f.ModTime}
		switch f.Path {
		case manifestName, "metrics_summary.csv", "gene_panel.json":
			if raw, err := b.ReadSmall(f.Path); err == nil {
				sum := sha256.Sum256(raw)
				fp.SHA256 = hex.EncodeToString(sum[:])
			}
		}
		out = append(out, fp)
	}
	return out
}
