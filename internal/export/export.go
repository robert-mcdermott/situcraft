// Package export writes a segmentation of a dataset in the formats other tools read: SpatialData
// Zarr, AnnData Zarr, Parquet and CSV tables, the Matrix Market exchange form, GeoJSON polygons,
// and the inputs of Xenium Ranger's import-segmentation with the command that would run it. An
// export reads SituCraft's segmentation files (internal/segment) and the dataset's transcripts,
// and writes one folder holding each format asked for and situcraft-export.json: what was
// exported, from what, by which SituCraft, and every file by its size and SHA-256.
//
// The folder is written beside its destination under a temporary name and renamed into place
// when complete, so a failed or stopped export leaves nothing. An existing folder is replaced only
// when asked, and only when it is a SituCraft export itself.
package export

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// The formats.
const (
	SpatialData  = "spatialdata"
	AnnData      = "anndata"
	Parquet      = "parquet"
	CSV          = "csv"
	MEX          = "mex"
	GeoJSON      = "geojson"
	XeniumRanger = "xenium-ranger"
)

// Formats lists every format, in the order they are written.
var Formats = []string{SpatialData, AnnData, Parquet, CSV, MEX, GeoJSON, XeniumRanger}

// About describes each format's files.
var About = map[string]string{
	SpatialData:  "spatialdata.zarr: a SpatialData store (Zarr format 3) with the cells as a label image and as polygons, the transcripts as points with their cells, and the counts as an AnnData table annotating the labels; coordinates in µm",
	AnnData:      "anndata.zarr: the counts as an AnnData object (Zarr format 2, anndata's default): cells by genes, the cells' measures in obs, their centroids in obsm['spatial'] (µm)",
	Parquet:      "parquet/: cells.parquet, transcripts.parquet (each transcript's cell), cell_boundaries.parquet (vertices) and counts.parquet (cell, gene, count), in Xenium's column names",
	CSV:          "csv/: the same four tables as gzipped CSV",
	MEX:          "cell_feature_matrix/: matrix.mtx.gz, features.tsv.gz and barcodes.tsv.gz, as Xenium and Cell Ranger write them (Seurat's Read10X reads them)",
	GeoJSON:      "cell_boundaries.geojson: each cell's outline as a polygon feature naming it, in µm",
	XeniumRanger: "xenium-ranger/: the cells as a label mask on the morphology image's pixels (cells.npy), the bundle's nuclei (nuclei.npy), each label's cell (cell_labels.csv) and the import-segmentation command (command.sh)",
}

// Dataset is what an export reads beside the segmentation's files.
type Dataset interface {
	Name() string
	// Transcripts calls fn with each batch of transcripts in their row order (the order of the
	// segmentation's assignments).
	Transcripts(fn func(*xenium.TranscriptBatch) error) error
	// GeneIDs maps the genes' names to their identifiers (nil when unknown).
	GeneIDs() map[string]string
	// NucleusMask opens the dataset's nucleus mask, on the segmentation's pixel grid (nil, nil
	// when it has none).
	NucleusMask() (Mask, error)
	// ImageSize is the morphology image's full-resolution size (0, 0 when unknown).
	ImageSize() (int, int)
	// BundleFolder is the folder Xenium Ranger reads ("" when the bundle is a zip or unknown), and
	// Run the run's identity, recorded in the export.
	BundleFolder() string
	Run() map[string]string
}

// Mask is a label mask read a band of rows at a time.
type Mask interface {
	Size() (int, int)
	Rows(y0, y1 int, dst []uint32) error
	Close() error
}

// Segmentation names the segmentation exported.
type Segmentation struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Method   string `json:"method"`
	Version  string `json:"version,omitempty"`
	Citation string `json:"citation,omitempty"`
}

// Options are what to export and where.
type Options struct {
	Formats []string
	// Folder is the export's folder, made by the export; an existing one is replaced only with
	// Overwrite, and only when it is a SituCraft export.
	Folder    string
	Overwrite bool
	// Software names SituCraft (name and version) in the files that record their writer.
	Software string
	// Commit is SituCraft's build, recorded in the manifest.
	Commit string
}

// Finding is something the export found that the person using it should know.
type Finding struct {
	Format   string `json:"format"`
	Severity string `json:"severity"` // "info", "warning" or "fatal"
	Code     string `json:"code"`
	Message  string `json:"message"`
	Count    int    `json:"count,omitempty"`
}

// File is one file written.
type File struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Ranger is the prepared Xenium Ranger command.
type Ranger struct {
	ID      string   `json:"id"`
	Args    []string `json:"args"`
	Command string   `json:"command"`
}

// Manifest is situcraft-export.json.
type Manifest struct {
	Format       string            `json:"format"`
	Version      int               `json:"version"`
	Software     string            `json:"software"`
	Commit       string            `json:"commit,omitempty"`
	Created      string            `json:"created"`
	Dataset      map[string]string `json:"dataset"`
	Segmentation Segmentation      `json:"segmentation"`
	Units        Units             `json:"units"`
	Counts       Totals            `json:"counts"`
	Content      Content           `json:"content"`
	Formats      map[string]string `json:"formats"`
	Files        []File            `json:"files"`
	Findings     []Finding         `json:"findings"`
	XeniumRanger *Ranger           `json:"xeniumRanger,omitempty"`
}

// Units say how the files' coordinates are to be read.
type Units struct {
	Coordinates string  `json:"coordinates"`
	PixelSizeUm float64 `json:"pixelSizeUm"`
	// RasterSize is the label raster's (and the masks') size in pixels.
	RasterSize [2]int `json:"rasterSizePx"`
}

// Totals are what was exported: cells, genes, transcripts and the counts' entries and sum.
type Totals struct {
	Cells            int    `json:"cells"`
	CellsWithOutline int    `json:"cellsWithOutline"`
	CellsWithPixels  int    `json:"cellsWithPixels"`
	Genes            int    `json:"genes"`
	Transcripts      int    `json:"transcripts"`
	Assigned         int    `json:"assignedTranscripts"`
	Entries          int    `json:"countEntries"`
	Sum              uint64 `json:"countSum"`
}

// Content fingerprints what every format holds, whatever its encoding, so that a reader can check
// it read the segmentation exactly (each a SHA-256, in hex):
//
//	labels       the label raster as little-endian uint32, row by row (height × width)
//	counts       a line "cell_id<TAB>gene<TAB>count<LF>" for each nonzero count, the cells in the
//	             export's order and each cell's genes in the order of their names' bytes
//	assignments  a line "cell_id<LF>" for each transcript in the dataset's order, "UNASSIGNED"
//	             for a transcript in no cell
type Content struct {
	Labels      string `json:"labelsSha256"`
	Counts      string `json:"countsSha256"`
	Assignments string `json:"assignmentsSha256"`
}

// ManifestName is the manifest's file name.
const ManifestName = "situcraft-export.json"

// Progress reports how far the export is, from 0 to 1, and what it is doing.
type Progress func(float64, string)

// data is the segmentation as the formats write it.
type data struct {
	ds       Dataset
	seg      Segmentation
	desc     *segment.Description
	cells    []segment.Cell
	counts   *segment.Counts
	outlines []segment.Outline
	assign   []uint32
	folder   string // the segmentation's files
	raster   *segment.Raster
	geneIDs  map[string]string
	software string
}

func (d *data) loadRaster() (*segment.Raster, error) {
	if d.raster == nil {
		r, err := segment.ReadMask(d.folder)
		if err != nil {
			return nil, err
		}
		d.raster = r
	}
	return d.raster, nil
}

// cellID is transcript assignment k's cell id ("UNASSIGNED" for none, as Xenium writes it).
func (d *data) cellID(k uint32) string {
	if k == 0 || int(k) > len(d.cells) {
		return "UNASSIGNED"
	}
	return d.cells[k-1].ID
}

// CheckFolder checks the export's folder before a job starts: its parent exists, and the folder
// does not, unless overwrite and it is a SituCraft export.
func CheckFolder(folder string, overwrite bool) error {
	if !filepath.IsAbs(folder) {
		return fmt.Errorf("%s is not an absolute path", folder)
	}
	parent, err := os.Stat(filepath.Dir(folder))
	if err != nil || !parent.IsDir() {
		return fmt.Errorf("the folder %s does not exist", filepath.Dir(folder))
	}
	info, err := os.Stat(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is a file", folder)
	}
	if _, err := os.Stat(filepath.Join(folder, ManifestName)); err != nil {
		return fmt.Errorf("%s exists and is not a SituCraft export: choose another folder", folder)
	}
	if !overwrite {
		return fmt.Errorf("%s exists; replace it to export there again", folder)
	}
	return nil
}

// Run writes the export. The segmentation's files are in folder.
func Run(ctx context.Context, ds Dataset, folder string, seg Segmentation, opt Options, progress Progress) (*Manifest, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	formats, err := chooseFormats(opt.Formats)
	if err != nil {
		return nil, err
	}
	if err := CheckFolder(opt.Folder, opt.Overwrite); err != nil {
		return nil, err
	}
	progress(0, "Reading the segmentation")
	d := &data{ds: ds, seg: seg, folder: folder, software: opt.Software}
	if d.desc, err = segment.ReadDescription(folder); err != nil {
		return nil, err
	}
	if d.cells, err = segment.ReadCells(folder); err != nil {
		return nil, err
	}
	if d.counts, err = segment.ReadCounts(folder); err != nil {
		return nil, err
	}
	if d.outlines, err = segment.ReadOutlines(folder); err != nil {
		return nil, err
	}
	if d.assign, err = segment.ReadAssignments(folder); err != nil {
		return nil, err
	}
	if len(d.counts.Offsets) != len(d.cells)+1 || len(d.outlines) != len(d.cells) {
		return nil, fmt.Errorf("the segmentation's files disagree: %d cells, %d rows of counts, %d outlines", len(d.cells), len(d.counts.Offsets)-1, len(d.outlines))
	}
	d.geneIDs = ds.GeneIDs()

	stage, err := os.MkdirTemp(filepath.Dir(opt.Folder), "."+filepath.Base(opt.Folder)+".partial-")
	if err != nil {
		return nil, err
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(stage)
		}
	}()
	m := &Manifest{
		Format: "situcraft-export", Version: 1, Software: opt.Software, Commit: opt.Commit,
		Created: time.Now().UTC().Format(time.RFC3339), Dataset: map[string]string{"name": ds.Name()},
		Segmentation: seg, Formats: map[string]string{}, Findings: []Finding{},
		Units: Units{Coordinates: "µm", PixelSizeUm: d.desc.PixelSize, RasterSize: [2]int{d.desc.Width, d.desc.Height}},
	}
	for k, v := range ds.Run() {
		m.Dataset[k] = v
	}
	progress(0.01, "Fingerprinting the segmentation")
	if m.Counts, m.Content, err = d.totals(); err != nil {
		return nil, err
	}
	for i, f := range formats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		step := func(frac float64, message string) {
			progress(0.02+0.93*(float64(i)+frac)/float64(len(formats)), message)
		}
		step(0, "Writing "+f)
		var findings []Finding
		switch f {
		case SpatialData:
			err = writeSpatialData(ctx, d, filepath.Join(stage, "spatialdata.zarr"), step)
		case AnnData:
			err = writeAnnData(d, filepath.Join(stage, "anndata.zarr"))
		case Parquet, CSV:
			err = writeTables(ctx, d, filepath.Join(stage, f), f, step)
		case MEX:
			err = writeMEX(d, filepath.Join(stage, "cell_feature_matrix"))
		case GeoJSON:
			err = writeGeoJSON(d, filepath.Join(stage, "cell_boundaries.geojson"))
		case XeniumRanger:
			var r *Ranger
			r, findings, err = writeRanger(ctx, d, stage, opt.Folder, step)
			m.XeniumRanger = r
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		m.Formats[f] = About[f]
		m.Findings = append(m.Findings, findings...)
	}
	progress(0.96, "Checksumming the files")
	if m.Files, err = checksums(stage); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, ManifestName), append(raw, '\n'), 0o644); err != nil {
		return nil, err
	}
	// Into place: an older export there (checked to be one) is set aside, then removed.
	if _, err := os.Stat(opt.Folder); err == nil {
		if err := CheckFolder(opt.Folder, opt.Overwrite); err != nil {
			return nil, err
		}
		old := stage + ".old"
		if err := os.Rename(opt.Folder, old); err != nil {
			return nil, err
		}
		defer os.RemoveAll(old)
	}
	if err := os.Rename(stage, opt.Folder); err != nil {
		return nil, err
	}
	done = true
	progress(1, "Exported")
	return m, nil
}

func chooseFormats(asked []string) ([]string, error) {
	want := map[string]bool{}
	for _, f := range asked {
		known := false
		for _, k := range Formats {
			known = known || k == f
		}
		if !known {
			return nil, fmt.Errorf("the format %q is not one of %s", f, strings.Join(Formats, ", "))
		}
		want[f] = true
	}
	if len(want) == 0 {
		return nil, errors.New("choose a format to export")
	}
	var out []string
	for _, f := range Formats {
		if want[f] {
			out = append(out, f)
		}
	}
	return out, nil
}

func (d *data) totals() (Totals, Content, error) {
	t := Totals{Cells: len(d.cells), Genes: len(d.counts.Genes), Transcripts: len(d.assign), Entries: len(d.counts.Count)}
	var c Content
	for _, o := range d.outlines {
		if len(o.X) >= 3 {
			t.CellsWithOutline++
		}
	}
	for _, k := range d.assign {
		if k != 0 {
			t.Assigned++
		}
	}
	for _, v := range d.counts.Count {
		t.Sum += uint64(v)
	}
	r, err := d.loadRaster()
	if err != nil {
		return t, c, err
	}
	for _, s := range r.Stats(len(d.cells)) {
		if s.Pixels > 0 {
			t.CellsWithPixels++
		}
	}
	h := sha256.New()
	row := make([]uint32, r.Width)
	raw := make([]byte, 4*r.Width)
	for y := 0; y < r.Height; y++ {
		r.Row(y, row)
		for x, v := range row {
			binary.LittleEndian.PutUint32(raw[4*x:], v)
		}
		h.Write(raw)
	}
	c.Labels = hex.EncodeToString(h.Sum(nil))
	h.Reset()
	w := bufio.NewWriter(h)
	type entry struct {
		gene  string
		count uint32
	}
	var entries []entry
	for i, cell := range d.cells {
		entries = entries[:0]
		for k := d.counts.Offsets[i]; k < d.counts.Offsets[i+1]; k++ {
			entries = append(entries, entry{d.counts.Genes[d.counts.Gene[k]], d.counts.Count[k]})
		}
		sort.Slice(entries, func(a, b int) bool { return entries[a].gene < entries[b].gene })
		for _, e := range entries {
			fmt.Fprintf(w, "%s\t%s\t%d\n", cell.ID, e.gene, e.count)
		}
	}
	w.Flush()
	c.Counts = hex.EncodeToString(h.Sum(nil))
	h.Reset()
	w.Reset(h)
	for _, k := range d.assign {
		w.WriteString(d.cellID(k))
		w.WriteByte('\n')
	}
	w.Flush()
	c.Assignments = hex.EncodeToString(h.Sum(nil))
	return t, c, nil
}

// checksums lists every file written, by format, with its size and SHA-256.
func checksums(stage string) ([]File, error) {
	formatOf := map[string]string{"spatialdata.zarr": SpatialData, "anndata.zarr": AnnData, "parquet": Parquet, "csv": CSV,
		"cell_feature_matrix": MEX, "cell_boundaries.geojson": GeoJSON, "xenium-ranger": XeniumRanger}
	var out []File
	err := filepath.Walk(stage, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		n, err := io.Copy(h, f)
		if err != nil {
			return err
		}
		out = append(out, File{Path: rel, Format: formatOf[strings.SplitN(rel, "/", 2)[0]], Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// ReadManifest reads an export's situcraft-export.json.
func ReadManifest(folder string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(folder, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil || m.Format != "situcraft-export" {
		return nil, fmt.Errorf("%s is not a SituCraft export's manifest", ManifestName)
	}
	return &m, nil
}

// Verify checksums an export's files again: the files whose size or SHA-256 differ from the
// manifest's, or that are missing.
func Verify(folder string) ([]string, error) {
	m, err := ReadManifest(folder)
	if err != nil {
		return nil, err
	}
	now, err := checksums(folder)
	if err != nil {
		return nil, err
	}
	have := map[string]File{}
	for _, f := range now {
		have[f.Path] = f
	}
	var bad []string
	for _, f := range m.Files {
		if g, ok := have[f.Path]; !ok || g.Size != f.Size || g.SHA256 != f.SHA256 {
			bad = append(bad, f.Path)
		}
	}
	return bad, nil
}
