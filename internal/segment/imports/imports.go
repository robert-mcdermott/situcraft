// Package imports reads segmentations made by other tools into SituCraft's own segmentation files
// (internal/segment), checked against the dataset they segment: SpatialData Zarr (shapes or
// labels, points with assignments, AnnData tables), Baysor-style transcript assignments with cell
// polygons, GeoJSON polygons with an optional transcript-to-cell table, and Xenium Ranger (or
// Xenium Onboard Analysis) output bundles of the same run.
//
// Every import becomes the same files as a segmentation SituCraft makes:
//   - the cells, by the tool's own ids;
//   - a label raster at the dataset's morphology image's resolution: the tool's label image
//     resampled (nearest pixel), or its polygons drawn (a pixel whose centre lies inside);
//   - each of the dataset's transcripts' cell: the tool's assignment, matched to the dataset's
//     transcripts by their ids (or by position and gene, within 0.01 µm, when the tool kept no ids),
//     else the cell holding the transcript's pixel;
//   - the counts of gene transcripts of quality 20 or more, as for any segmentation, so that
//     segmentations compare on the same terms; a count table the tool wrote is compared with them;
//   - the outlines: the tool's polygons (each cell's largest), else traced from the raster.
//
// The import is checked against the dataset: the transcripts it names exist, its cells lie within
// the dataset's extent, its ids are consistent; what it finds is recorded with the artifact.
package imports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// Formats SituCraft imports.
const (
	SpatialData = "spatialdata"
	Baysor      = "baysor"
	GeoJSON     = "geojson"
	Xenium      = "xenium"
)

// Options say what to import and how to read it.
type Options struct {
	Format string   `json:"format"`
	Paths  []string `json:"paths"`
	// Units are the coordinates' units: "um" (µm, the default) or "pixels" (of the dataset's
	// morphology image). SituCraft never guesses them from the values; the extent check says when
	// they do not fit the dataset.
	Units string `json:"units,omitempty"`
	// SpatialData: the coordinate system ("global" if not set), the element whose cells are
	// imported (shapes or labels), the points with the assignments, and the table.
	CoordinateSystem string `json:"coordinate_system,omitempty"`
	Cells            string `json:"cells,omitempty"`
	Points           string `json:"points,omitempty"`
	Table            string `json:"table,omitempty"`
	// CellColumn names the assignment's cell column; CellProperty the GeoJSON property naming each
	// polygon's cell (chosen from the usual names when not set).
	CellColumn   string `json:"cell_column,omitempty"`
	CellProperty string `json:"cell_property,omitempty"`
	// Tool and ToolVersion say what made the segmentation (recorded with it); Citation its paper.
	Tool        string `json:"tool,omitempty"`
	ToolVersion string `json:"tool_version,omitempty"`
	Citation    string `json:"citation,omitempty"`
}

// MethodVersion is the version of the import's reading, recorded with what it makes.
const MethodVersion = "1"

// Dataset is what an import is checked against and assigned on.
type Dataset interface {
	Name() string
	// Grid is the morphology image's pixel grid: its size in pixels and a pixel's size in µm.
	Grid() (width, height int, pixelSize float64)
	// Run identifies the instrument run (for imports of output bundles).
	Run() RunIdentity
	// Transcripts calls fn with each batch of transcripts, in the dataset's order.
	Transcripts(fn func(*xenium.TranscriptBatch) error) error
}

// RunIdentity is what an output bundle says of its run: the experiment's UUID, which names the
// run, and its name, region and start, which bundles shared as examples may anonymize.
type RunIdentity struct {
	ExperimentUUID, Name, Region, Start string
}

// Same reports whether two bundles are of the same run: by the experiment's UUID when both have
// one, else by the run's name, region and start together.
func (r RunIdentity) Same(o RunIdentity) bool {
	if r.ExperimentUUID != "" && o.ExperimentUUID != "" {
		return r.ExperimentUUID == o.ExperimentUUID
	}
	return r.Name == o.Name && r.Region == o.Region && r.Start == o.Start
}

func (r RunIdentity) String() string {
	if r.ExperimentUUID != "" {
		return "experiment " + r.ExperimentUUID
	}
	return fmt.Sprintf("run %q, region %q, started %s", r.Name, r.Region, r.Start)
}

// SourceFile is a file an import read, by its SHA-256.
type SourceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// source is an import read, before it is built into a segmentation.
type source struct {
	ids   []string
	index map[string]int32
	// polygons are each cell's polygons (µm), or nil.
	polygons [][]polygon
	// labels is the tool's label image, when it gave one.
	labels *labelImage
	// records are the tool's transcript assignments, when it gave them.
	records *records
	// table is the tool's count of transcripts in each cell (by the same index), when it gave one,
	// and what it counts.
	table     []float64
	tableKind string
	findings  []artifacts.Finding
	files     []SourceFile
	read      map[string]any // what was read: elements, columns, choices
	software  []artifacts.Software
	toolName  string
	toolVer   string
	citation  string
}

func newSource() *source {
	return &source{index: map[string]int32{}, read: map[string]any{}}
}

// cell is the index of a cell by id, added when new.
func (s *source) cell(id string) int32 {
	if k, ok := s.index[id]; ok {
		return k
	}
	k := int32(len(s.ids))
	s.ids = append(s.ids, id)
	s.index[id] = k
	if s.polygons != nil {
		s.polygons = append(s.polygons, nil)
	}
	return k
}

func (s *source) find(severity, code, message string, count int) {
	if count > 0 || severity == "fatal" {
		s.findings = append(s.findings, artifacts.Finding{Severity: severity, Code: code, Message: message, Count: count})
	}
}

// records are a tool's transcript assignments: each record names a transcript (by id, or by
// position and gene) and its cell (−1: none). Records by id may carry the position too (hasXY):
// a record whose position differs from the dataset's transcript of that id names another
// transcript (of another dataset).
type records struct {
	byID  bool
	hasXY bool
	id    []uint64
	x, y  []float64 // µm
	gene  []string
	cell  []int32
}

func (r *records) len() int { return len(r.cell) }

// labelImage is a tool's label image: label values map to cells (value v → cellOf[v], or v−1
// when cellOf is nil); pixel (i, j) spans [i, i+1) × [j, j+1) in its own coordinates, which the
// affine transform maps to µm.
type labelImage struct {
	width, height int
	values        []uint32
	cellOf        map[uint32]int32
	transform     affine
}

// Run reads an import, checks it against the dataset and writes the segmentation's files into dir.
// It answers the segmentation's description, what the reading and the checks found, and the files
// read.
func Run(ctx context.Context, ds Dataset, opt Options, dir string, workers int, progress func(float64, string)) (*segment.Description, []artifacts.Finding, []SourceFile, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if opt.Units == "" {
		opt.Units = "um"
	}
	if opt.Units != "um" && opt.Units != "pixels" {
		return nil, nil, nil, fmt.Errorf("units are um or pixels, not %q", opt.Units)
	}
	if len(opt.Paths) == 0 {
		return nil, nil, nil, errors.New("name the files to import")
	}
	_, _, pixelSize := ds.Grid()
	scale := 1.0
	if opt.Units == "pixels" {
		scale = pixelSize
	}
	progress(0, "Reading the segmentation")
	var src *source
	var err error
	switch opt.Format {
	case SpatialData:
		src, err = readSpatialData(ctx, opt, scale)
	case Baysor, GeoJSON:
		src, err = readPolygonsAndTable(ctx, opt, scale)
	case Xenium:
		src, err = readXenium(ctx, ds, opt)
	default:
		return nil, nil, nil, fmt.Errorf("the format %q is not imported (%s, %s, %s or %s)", opt.Format, SpatialData, Baysor, GeoJSON, Xenium)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if len(src.ids) == 0 {
		return nil, nil, nil, errors.New("it holds no cells")
	}
	d, err := build(ctx, ds, opt, src, dir, workers, progress)
	if err != nil {
		return nil, nil, nil, err
	}
	return d, src.findings, src.files, nil
}

// Recorded are what the artifact records of an import: its options and what was read.
type Recorded struct {
	Format     string         `json:"format"`
	Units      string         `json:"units"`
	Read       map[string]any `json:"read"`
	Raster     string         `json:"raster"`
	Assignment string         `json:"assignment"`
	Outlines   string         `json:"outlines"`
	Counts     string         `json:"counts"`
	Tool       string         `json:"tool,omitempty"`
	Version    string         `json:"tool_version,omitempty"`
	Citation   string         `json:"citation,omitempty"`
}

// build turns an import into a segmentation's files.
func build(ctx context.Context, ds Dataset, opt Options, src *source, dir string, workers int, progress func(float64, string)) (*segment.Description, error) {
	w, h, ps := ds.Grid()
	n := len(src.ids)
	rec := Recorded{Format: opt.Format, Units: opt.Units, Read: src.read, Counts: "gene transcripts of quality 20 or more in each cell, from the assignment", Tool: src.toolName, Version: src.toolVer, Citation: src.citation}

	// The raster.
	progress(0.15, "Drawing the cells")
	labels := make([]uint32, w*h)
	switch {
	case src.labels != nil:
		outside := resample(src.labels, labels, w, h, ps)
		rec.Raster = "the tool's label image, resampled to the morphology image's pixels (nearest)"
		src.find("info", "labels.outside", "Labelled pixels outside the dataset's image", outside)
	case src.polygons != nil:
		overlap := 0
		for k, polys := range src.polygons {
			overlap += draw(labels, w, h, ps, polys, uint32(k+1))
		}
		rec.Raster = "the tool's polygons drawn at the morphology image's resolution: a pixel whose centre lies inside"
		src.find("info", "polygons.overlap", "Pixels inside two cells' polygons, given to the later", overlap)
	default:
		rec.Raster = "none: the tool gave no outlines, so its cells have no area"
		src.find("warning", "cells.no-outlines", "The import has no polygons or labels: its cells have transcripts but no area", n)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raster := &segment.Raster{Width: w, Height: h, PixelSize: ps, Rows: make([][]segment.Run, h)}
	for y := 0; y < h; y++ {
		raster.Rows[y] = segment.EncodeRow(labels[y*w : (y+1)*w])
	}
	labels = nil
	stats := raster.Stats(n)

	// The extent: the cells lie within the dataset's image.
	if src.labels != nil || src.polygons != nil {
		drawn := 0
		for _, s := range stats {
			if s.Pixels > 0 {
				drawn++
			}
		}
		withShape := n
		if src.polygons != nil {
			withShape = 0
			for _, p := range src.polygons {
				if len(p) > 0 {
					withShape++
				}
			}
		}
		if withShape > 0 && drawn < (withShape+1)/2 {
			// Coordinates read as µm would be pixels × the pixel size; read as pixels, µm ÷ it.
			other, factor := "pixels", ps
			if opt.Units == "pixels" {
				other, factor = "µm", 1/ps
			}
			hint := ""
			if fit := src.within(factor, float64(w)*ps, float64(h)*ps); fit*2 > withShape {
				hint = fmt.Sprintf(" Read as %s, %d would.", other, fit)
			}
			src.find("fatal", "coordinates.extent", fmt.Sprintf("Only %d of the %d cells with an outline lie within %s's image (%.1f × %.1f µm), their coordinates read as %s: they are in other units, or of another dataset.%s", drawn, withShape, ds.Name(), float64(w)*ps, float64(h)*ps, unitName(opt.Units), hint), 0)
		} else {
			src.find("warning", "coordinates.outside", "Cells with an outline wholly outside the dataset's image", withShape-drawn)
		}
	}

	// The transcripts.
	progress(0.35, "Assigning the transcripts")
	match, err := newMatcher(src.records)
	if err != nil {
		return nil, err
	}
	if src.records != nil {
		rec.Assignment = "the tool's assignment, matched to the dataset's transcripts by their ids"
		if !src.records.byID {
			rec.Assignment = "the tool's assignment, matched to the dataset's transcripts by position and gene (within 0.01 µm)"
		}
	} else {
		rec.Assignment = "the cell holding the pixel each transcript lies in"
	}
	cells := make([]segment.Cell, n)
	assignments := []uint32{}
	geneIndex := map[string]uint32{}
	var geneNames []string
	pairs := map[uint64]uint32{}
	var q20, q20Assigned int64
	noID := false
	err = ds.Transcripts(func(b *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if src.records != nil && src.records.byID && b.TranscriptID == nil {
			noID = true
			return errors.New("the dataset's transcripts have no ids to match the import's")
		}
		for i := 0; i < b.Rows; i++ {
			label := uint32(0)
			if src.records != nil {
				var k int32 = -1
				if src.records.byID {
					k = match.byID(b.TranscriptID[i], float64(b.X[i]), float64(b.Y[i]))
				} else {
					k = match.byPosition(float64(b.X[i]), float64(b.Y[i]), string(b.Feature[i]))
				}
				if k >= 0 {
					label = uint32(k + 1)
				}
			} else {
				label = raster.AtMicrons(float64(b.X[i]), float64(b.Y[i]))
			}
			assignments = append(assignments, label)
			if label > 0 {
				cells[label-1].Assigned++
			}
			name := string(b.Feature[i])
			if b.QV[i] < 20 || xenium.FeatureKind(name) != xenium.KindGene {
				continue
			}
			q20++
			if label == 0 {
				continue
			}
			g, ok := geneIndex[name]
			if !ok {
				g = uint32(len(geneNames))
				geneIndex[name] = g
				geneNames = append(geneNames, name)
			}
			pairs[uint64(label-1)<<32|uint64(g)]++
			cells[label-1].Transcripts++
			q20Assigned++
		}
		return nil
	})
	if err != nil && !noID {
		return nil, err
	}
	if noID {
		return nil, errors.New("the import names transcripts by their ids, which the dataset's transcripts.parquet does not have")
	}
	if src.records != nil {
		matched := match.matched()
		unmatched := src.records.len() - matched
		if src.records.len() > 0 && matched*2 < src.records.len() {
			src.find("fatal", "transcripts.unknown", fmt.Sprintf("Only %d of the import's %d transcripts are %s's: it assigns another dataset's transcripts.", matched, src.records.len(), ds.Name()), 0)
		} else {
			src.find("warning", "transcripts.unknown", "Transcripts the import names that the dataset does not have", unmatched)
		}
		src.find("info", "transcripts.not-in-import", "Transcripts of the dataset the import does not name (left unassigned)", len(assignments)-match.covered())
		src.find("warning", "transcripts.ambiguous", "Transcripts the import names twice, or that match two of the dataset's (the first is used)", match.duplicates)
		src.find("warning", "transcripts.elsewhere", "Transcripts the import names by an id of the dataset's, but elsewhere (more than 0.05 µm away): another transcript", match.elsewhere)
	}

	// The cells.
	px2 := ps * ps
	for i := range cells {
		c := &cells[i]
		c.ID = src.ids[i]
		if s := stats[i]; s.Pixels > 0 {
			c.Area = float32(float64(s.Pixels) * px2)
			c.X, c.Y = float32(s.SumX/float64(s.Pixels)*ps), float32(s.SumY/float64(s.Pixels)*ps)
		}
	}
	counts := countsOf(pairs, geneNames, n)
	for i := range cells {
		cells[i].Genes = counts.Offsets[i+1] - counts.Offsets[i]
	}
	// The tool's own counts, against the assignment's.
	if src.table != nil {
		same, total := 0, 0
		for i, v := range src.table {
			if i >= n || math.IsNaN(v) {
				continue
			}
			total++
			var mine float64
			if src.tableKind == "all" {
				mine = float64(cells[i].Assigned)
			} else {
				mine = float64(cells[i].Transcripts)
			}
			if math.Abs(v-mine) < 0.5 {
				same++
			}
		}
		rec.Read["table_agrees"] = fmt.Sprintf("%d of %d cells", same, total)
		src.find("info", "table.differs", "Cells whose count in the tool's table differs from the transcripts assigned to them", total-same)
	}

	// The outlines.
	progress(0.7, "Outlining the cells")
	outlines := make([]segment.Outline, n)
	if src.polygons != nil {
		rec.Outlines = "the tool's polygons (each cell's largest)"
		extentX, extentY := float64(w)*ps, float64(h)*ps
		cut := 0
		for i := range outlines {
			o, clipped := outlineOf(src.polygons[i], stats[i], extentX, extentY)
			outlines[i] = o
			if clipped && stats[i].Pixels > 0 {
				cut++
			}
		}
		src.find("info", "outlines.cut", "Outlines reaching beyond the dataset's image, cut at its edge", cut)
	} else {
		rec.Outlines = "traced around each cell's largest piece of the raster"
		raster.RowRange(0, 0, 1, make([]uint32, 1)) // index the raster before the workers share it
		parallel(n, workers, func(a, z int) {
			for i := a; i < z; i++ {
				outlines[i] = raster.TraceOutline(uint32(i+1), stats[i], 0.5)
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Ids: unique, as segment files require; empty or repeated ids cannot reach here (the index).
	progress(0.9, "Writing the segmentation")
	params, _ := json.Marshal(rec)
	d := segment.Description{Method: "imported " + opt.Format + " " + MethodVersion, Parameters: params, Width: w, Height: h, PixelSize: ps,
		Cells: n, Transcripts: len(assignments), Genes: len(counts.Genes), Q20GeneTranscripts: q20, Q20GeneAssigned: q20Assigned}
	d.Nuclei.Source = "none: imported"
	for _, write := range []func() error{
		func() error { return segment.WriteCells(dir, cells) },
		func() error { return segment.WriteMask(dir, raster) },
		func() error { return segment.WriteAssignments(dir, assignments) },
		func() error { return segment.WriteCounts(dir, counts) },
		func() error { return segment.WriteOutlines(dir, outlines) },
		func() error { return segment.WriteDescription(dir, d) },
	} {
		if err := write(); err != nil {
			return nil, err
		}
	}
	progress(1, "Written")
	return &d, nil
}

// within counts the cells whose outline (its first vertex, or a label image's pixels) would lie
// within the extent were the coordinates scaled by factor: a fact for the message, never applied.
func (s *source) within(factor, extentX, extentY float64) int {
	in := func(x, y float64) bool { return x >= 0 && y >= 0 && x < extentX && y < extentY }
	n := 0
	if s.labels != nil {
		img := s.labels
		t := img.transform.scaled(factor)
		seen := map[int32]bool{}
		for j := 0; j < img.height; j += 4 {
			for i := 0; i < img.width; i += 4 {
				v := img.values[j*img.width+i]
				if v == 0 {
					continue
				}
				k, ok := img.cell(v)
				if !ok || seen[k] {
					continue
				}
				if x, y := t.apply(float64(i)+0.5, float64(j)+0.5); in(x, y) {
					seen[k] = true
					n++
				}
			}
		}
		return n
	}
	for _, polys := range s.polygons {
		if len(polys) > 0 && len(polys[0]) > 0 && len(polys[0][0]) > 0 {
			if v := polys[0][0][0]; in(v.X*factor, v.Y*factor) {
				n++
			}
		}
	}
	return n
}

func unitName(u string) string {
	if u == "pixels" {
		return "pixels"
	}
	return "µm"
}

// outlineOf is a cell's outline from its polygons: the exterior of the largest, in µm, its vertices
// kept within the image (clipped answers whether any had to be moved).
func outlineOf(polys []polygon, s segment.LabelStats, extentX, extentY float64) (segment.Outline, bool) {
	o := segment.Outline{Pixels: int(s.Pixels), Pieces: len(polys)}
	best, bestArea := -1, -1.0
	for i, p := range polys {
		if a := polygonArea(p); a > bestArea {
			best, bestArea = i, a
		}
	}
	if best < 0 || s.Pixels == 0 {
		return o, false
	}
	p := polys[best]
	o.Holes = len(p) - 1
	ring := p[0]
	if len(ring) > 3 && ring[0] == ring[len(ring)-1] {
		ring = ring[:len(ring)-1] // closed explicitly; SituCraft's outlines close implicitly
	}
	clipped := false
	for _, v := range ring {
		x, y := math.Min(math.Max(v.X, 0), extentX), math.Min(math.Max(v.Y, 0), extentY)
		if x != v.X || y != v.Y {
			clipped = true
		}
		o.X = append(o.X, float32(x))
		o.Y = append(o.Y, float32(y))
	}
	return o, clipped
}

// resample draws a label image into the dataset's grid: each pixel takes the label at its centre
// (nearest). It answers how many labelled pixels of the tool's lie outside the dataset's image.
func resample(img *labelImage, dst []uint32, w, h int, size float64) int {
	inv, ok := img.transform.invert()
	if !ok {
		return 0
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			u, v := inv.apply((float64(x)+0.5)*size, (float64(y)+0.5)*size)
			i, j := int(math.Floor(u)), int(math.Floor(v))
			if i < 0 || j < 0 || i >= img.width || j >= img.height {
				continue
			}
			value := img.values[j*img.width+i]
			if value == 0 {
				continue
			}
			k, ok := img.cell(value)
			if ok {
				dst[y*w+x] = uint32(k + 1)
			}
		}
	}
	// The tool's labelled pixels whose centres fall outside the image.
	outside := 0
	for j := 0; j < img.height; j++ {
		for i := 0; i < img.width; i++ {
			if img.values[j*img.width+i] == 0 {
				continue
			}
			x, y := img.transform.apply(float64(i)+0.5, float64(j)+0.5)
			if x < 0 || y < 0 || x >= float64(w)*size || y >= float64(h)*size {
				outside++
			}
		}
	}
	return outside
}

func (img *labelImage) cell(v uint32) (int32, bool) {
	if img.cellOf == nil {
		return int32(v) - 1, true
	}
	k, ok := img.cellOf[v]
	return k, ok
}

// --- Matching the import's transcripts to the dataset's -----------------------------------------

type matcher struct {
	recs       *records
	ids        []uint64 // sorted
	rows       []int32  // the record of each
	grid       map[[2]int32][]int32
	used       []bool
	duplicates int
	elsewhere  int
}

// sameTranscriptTolerance is how near (µm) a record's position must be to that of the dataset's
// transcript of the same id.
const sameTranscriptTolerance = 0.05

func newMatcher(r *records) (*matcher, error) {
	m := &matcher{recs: r}
	if r == nil {
		return m, nil
	}
	m.used = make([]bool, r.len())
	if r.byID {
		order := make([]int32, r.len())
		for i := range order {
			order[i] = int32(i)
		}
		sort.Slice(order, func(a, b int) bool { return r.id[order[a]] < r.id[order[b]] })
		for _, k := range order {
			if n := len(m.ids); n > 0 && m.ids[n-1] == r.id[k] {
				m.duplicates++
				continue
			}
			m.ids = append(m.ids, r.id[k])
			m.rows = append(m.rows, k)
		}
		return m, nil
	}
	m.grid = map[[2]int32][]int32{}
	for i := 0; i < r.len(); i++ {
		key := [2]int32{int32(math.Floor(r.x[i])), int32(math.Floor(r.y[i]))}
		m.grid[key] = append(m.grid[key], int32(i))
	}
	return m, nil
}

// byID is the cell of the record naming transcript id, at (x, y) µm (−1: none, or no record).
func (m *matcher) byID(id uint64, x, y float64) int32 {
	i := sort.Search(len(m.ids), func(i int) bool { return m.ids[i] >= id })
	if i == len(m.ids) || m.ids[i] != id {
		return -1
	}
	k := m.rows[i]
	if m.used[k] {
		m.duplicates++
		return -1
	}
	if m.recs.hasXY {
		if dx, dy := m.recs.x[k]-x, m.recs.y[k]-y; dx*dx+dy*dy > sameTranscriptTolerance*sameTranscriptTolerance {
			m.elsewhere++
			return -1
		}
	}
	m.used[k] = true
	return m.recs.cell[k]
}

// positionTolerance is how near (µm) a record must lie to a transcript to name it.
const positionTolerance = 0.01

// byPosition is the cell of the nearest unused record of the same gene within the tolerance.
func (m *matcher) byPosition(x, y float64, gene string) int32 {
	best, bestD := int32(-1), positionTolerance*positionTolerance
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for _, k := range m.grid[[2]int32{int32(math.Floor(x)) + dx, int32(math.Floor(y)) + dy}] {
				if m.used[k] || m.recs.gene[k] != gene {
					continue
				}
				d := (m.recs.x[k]-x)*(m.recs.x[k]-x) + (m.recs.y[k]-y)*(m.recs.y[k]-y)
				if d <= bestD {
					best, bestD = k, d
				}
			}
		}
	}
	if best < 0 {
		return -1
	}
	m.used[best] = true
	return m.recs.cell[best]
}

// matched counts the records that named one of the dataset's transcripts.
func (m *matcher) matched() int {
	n := 0
	for _, u := range m.used {
		if u {
			n++
		}
	}
	return n
}

// covered is how many of the dataset's transcripts a record named.
func (m *matcher) covered() int { return m.matched() }

// --- Counts and work --------------------------------------------------------------------------

// countsOf turns (cell, gene) counts into a matrix in compressed sparse rows, genes in name order.
func countsOf(pairs map[uint64]uint32, genes []string, n int) *segment.Counts {
	order := make([]int, len(genes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return genes[order[a]] < genes[order[b]] })
	rank := make([]uint32, len(genes))
	sorted := make([]string, len(genes))
	for r, g := range order {
		rank[g] = uint32(r)
		sorted[r] = genes[g]
	}
	type entry struct {
		cell, gene, count uint32
	}
	entries := make([]entry, 0, len(pairs))
	for k, v := range pairs {
		entries = append(entries, entry{uint32(k >> 32), rank[uint32(k)], v})
	}
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].cell != entries[b].cell {
			return entries[a].cell < entries[b].cell
		}
		return entries[a].gene < entries[b].gene
	})
	c := &segment.Counts{Genes: sorted, Offsets: make([]uint32, n+1)}
	for _, e := range entries {
		c.Gene = append(c.Gene, e.gene)
		c.Count = append(c.Count, e.count)
		c.Offsets[e.cell+1]++
	}
	for i := 1; i <= n; i++ {
		c.Offsets[i] += c.Offsets[i-1]
	}
	return c
}

// parallel splits 0..n among workers.
func parallel(n, workers int, do func(a, b int)) {
	workers = max(1, min(workers, n))
	done := make(chan struct{}, workers)
	for k := 0; k < workers; k++ {
		go func(k int) {
			do(k*n/workers, (k+1)*n/workers)
			done <- struct{}{}
		}(k)
	}
	for k := 0; k < workers; k++ {
		<-done
	}
}

// --- Files read -------------------------------------------------------------------------------

// hashFile records a file read, by its SHA-256.
func (s *source) hashFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	s.files = append(s.files, SourceFile{Path: path, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
	return nil
}

// hashTree records every file under dir (a store's elements), in path order.
func (s *source) hashTree(dir string) error {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !strings.HasPrefix(d.Name(), ".DS_Store") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := s.hashFile(p); err != nil {
			return err
		}
	}
	return nil
}

// Digest names a set of files read: their SHA-256s, without their places (the same files moved
// are the same import).
func Digest(files []SourceFile, root func(string) string) string {
	lines := make([]string, len(files))
	for i, f := range files {
		lines[i] = root(f.Path) + "=" + f.SHA256
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:])
}
