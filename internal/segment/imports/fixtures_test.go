package imports

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// fixture is the hand-made dataset the fixtures segment (testdata/make_fixtures.py).
type fixture struct {
	Width, Height int
	PixelSize     float64 `json:"pixel_size"`
	RunName       string  `json:"run"`
	Region        string
	List          []struct {
		ID   uint64
		X, Y float32
		Gene string
		QV   float32
	} `json:"transcripts"`
	noIDs bool
}

func (f *fixture) Name() string              { return "the fixture" }
func (f *fixture) Grid() (int, int, float64) { return f.Width, f.Height, f.PixelSize }
func (f *fixture) Run() RunIdentity          { return RunIdentity{Name: f.RunName, Region: f.Region} }
func (f *fixture) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	b := &xenium.TranscriptBatch{Rows: len(f.List)}
	for _, t := range f.List {
		b.TranscriptID = append(b.TranscriptID, t.ID)
		b.X, b.Y, b.Z, b.QV = append(b.X, t.X), append(b.Y, t.Y), append(b.Z, 0), append(b.QV, t.QV)
		b.Feature = append(b.Feature, []byte(t.Gene))
		b.CellID = append(b.CellID, []byte("UNASSIGNED"))
	}
	if f.noIDs {
		b.TranscriptID = nil
	}
	return fn(b)
}

type expectation struct {
	Cells           []string
	Pixels          map[string]int64
	Assignment      map[string]string
	Q20GeneAssigned map[string]uint32 `json:"q20_gene_assigned"`
}

func loadFixture(t *testing.T) (*fixture, *expectation) {
	t.Helper()
	var f fixture
	var e expectation
	for name, v := range map[string]any{"dataset.json": &f, "expected.json": &e} {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatal(err)
		}
	}
	return &f, &e
}

// result is an import of the fixture, as the files it wrote say.
type result struct {
	cells       []segment.Cell
	mask        *segment.Raster
	assignments []uint32
	findings    []artifacts.Finding
	rec         Recorded
}

func importFixture(t *testing.T, ds Dataset, opt Options) *result {
	t.Helper()
	for i, p := range opt.Paths {
		opt.Paths[i] = filepath.Join("testdata", p)
	}
	dir := t.TempDir()
	d, findings, files, err := Run(context.Background(), ds, opt, dir, 2, nil)
	if err != nil {
		t.Fatalf("%s: %v", opt.Format, err)
	}
	if len(files) == 0 {
		t.Errorf("%s: no files recorded", opt.Format)
	}
	r := &result{findings: findings}
	var m artifacts.Manifest
	r.findings = append(r.findings, segment.Check(dir, &m)...)
	json.Unmarshal(d.Parameters, &r.rec)
	r.cells, _ = segment.ReadCells(dir)
	r.mask, _ = segment.ReadMask(dir)
	r.assignments, _ = segment.ReadAssignments(dir)
	return r
}

func (r *result) finding(code string) *artifacts.Finding {
	for i := range r.findings {
		if r.findings[i].Code == code {
			return &r.findings[i]
		}
	}
	return nil
}

// check compares an import with what it must give: the cells (rename maps the tool's ids to the
// scene's), each cell's pixels, each transcript's cell, and no fatal finding.
func (r *result) check(t *testing.T, name string, ds *fixture, e *expectation, rename map[string]string, skip map[string]bool) {
	t.Helper()
	for _, f := range r.findings {
		if f.Severity == "fatal" {
			t.Errorf("%s: fatal %s: %s", name, f.Code, f.Message)
		}
	}
	id := func(k uint32) string {
		if k == 0 {
			return ""
		}
		c := r.cells[k-1].ID
		if v, ok := rename[c]; ok {
			return v
		}
		return c
	}
	stats := r.mask.Stats(len(r.cells))
	for i, c := range r.cells {
		scene := id(uint32(i + 1))
		if want := e.Pixels[scene]; stats[i].Pixels != want {
			t.Errorf("%s: cell %s (%s) has %d pixels, want %d", name, c.ID, scene, stats[i].Pixels, want)
		}
		if want := e.Q20GeneAssigned[scene]; !skip[scene] && c.Transcripts != want {
			t.Errorf("%s: cell %s has %d gene transcripts, want %d", name, c.ID, c.Transcripts, want)
		}
	}
	if len(r.assignments) != len(ds.List) {
		t.Fatalf("%s: %d assignments", name, len(r.assignments))
	}
	for i, k := range r.assignments {
		want := e.Assignment[jsonID(ds.List[i].ID)]
		if skip[want] {
			want = ""
		}
		if got := id(k); got != want {
			t.Errorf("%s: transcript %d in %q, want %q", name, ds.List[i].ID, got, want)
		}
	}
}

func jsonID(v uint64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestFixtureSpatialDataShapes(t *testing.T) {
	ds, e := loadFixture(t)
	r := importFixture(t, ds, Options{Format: SpatialData, Paths: []string{"shapes.zarr"}})
	r.check(t, "shapes.zarr", ds, e, nil, nil)
	if r.rec.Read["cells"] != "shapes/cell_boundaries" || r.rec.Read["table"] != "table" || r.rec.Read["transcripts_by"] != "transcript_id" || r.rec.Read["table_agrees"] != "6 of 6 cells" {
		t.Errorf("read %v", r.rec.Read)
	}
	if len(r.cells) != 7 || r.cells[6].ID != "G" || r.cells[6].Area != 0 {
		t.Errorf("cells %+v", r.cells)
	}
	if f := r.finding("outlines.cut"); f == nil || f.Count != 1 {
		t.Errorf("F's outline reaches beyond the image: %+v", f)
	}
}

// SpatialData 0.2 in Zarr format 2 (written by spatialdata 0.2.6, make_fixtures_v2.py).
func TestFixtureSpatialDataFormat2(t *testing.T) {
	ds, e := loadFixture(t)
	r := importFixture(t, ds, Options{Format: SpatialData, Paths: []string{"shapes_v2.zarr"}})
	r.check(t, "shapes_v2.zarr", ds, e, nil, nil)
	if r.rec.Read["table"] != "table" || r.rec.Read["table_agrees"] != "6 of 6 cells" || r.rec.Read["transcripts_by"] != "transcript_id" {
		t.Errorf("read %v", r.rec.Read)
	}
}

func TestFixtureSpatialDataLabelsInPixels(t *testing.T) {
	ds, e := loadFixture(t)
	r := importFixture(t, ds, Options{Format: SpatialData, Paths: []string{"labels.zarr"}, Units: "pixels"})
	r.check(t, "labels.zarr", ds, e, nil, nil)
	if r.rec.Read["cells"] != "labels/cell_labels" || r.rec.Read["outlines"] != "shapes/cell_boundaries" {
		t.Errorf("read %v", r.rec.Read)
	}
	// Read as µm, the cells lie in a corner of the image: four of six still overlap it, so the
	// import goes on, and says how many lie outside.
	r = importFixture(t, ds, Options{Format: SpatialData, Paths: []string{"labels.zarr"}})
	if f := r.finding("coordinates.extent"); f != nil {
		t.Errorf("an extent that overlaps: %+v", f)
	}
}

func TestFixtureGeoJSONAndTables(t *testing.T) {
	ds, e := loadFixture(t)
	r := importFixture(t, ds, Options{Format: GeoJSON, Paths: []string{"geojson/cells.geojson", "geojson/assignments.parquet"}})
	r.check(t, "GeoJSON and Parquet", ds, e, nil, nil)
	if f := r.finding("transcripts.unknown"); f == nil || f.Count != 1 || f.Severity != "warning" {
		t.Errorf("the transcript the dataset does not have: %+v", f)
	}
	if r.rec.Read["cell_property"] != "cell_id" || r.rec.Read["cell_column"] != "cell_id" {
		t.Errorf("read %v", r.rec.Read)
	}
	r = importFixture(t, ds, Options{Format: GeoJSON, Paths: []string{"geojson/cells_pixels.geojson", "geojson/assignments.csv"}, Units: "pixels"})
	r.check(t, "GeoJSON in pixels and CSV", ds, e, nil, nil)
	// Polygons alone: each transcript goes to the cell holding its pixel.
	r = importFixture(t, ds, Options{Format: GeoJSON, Paths: []string{"geojson/cells.geojson"}})
	for i, k := range r.assignments {
		tr := ds.List[i]
		if want := r.mask.AtMicrons(float64(tr.X), float64(tr.Y)); k != want {
			t.Errorf("transcript %d in %d, its pixel's cell %d", tr.ID, k, want)
		}
	}
	if !strings.Contains(r.rec.Assignment, "pixel") {
		t.Errorf("assignment %q", r.rec.Assignment)
	}
	// Pixels read as µm: the scene lies near the origin, so most cells still overlap the image, and
	// the import says how many lie outside.
	r = importFixture(t, ds, Options{Format: GeoJSON, Paths: []string{"geojson/cells_pixels.geojson"}})
	if f := r.finding("coordinates.outside"); f == nil || f.Count != 2 {
		t.Errorf("pixels read as µm: %+v", r.findings)
	}
	// Polygons of another place: none lies within the image, which is fatal.
	raw, _ := os.ReadFile(filepath.Join("testdata", "geojson", "cells.geojson"))
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	for _, f := range doc["features"].([]any) {
		g := f.(map[string]any)["geometry"].(map[string]any)
		shift(g["coordinates"])
	}
	moved, _ := json.Marshal(doc)
	path := filepath.Join(t.TempDir(), "moved.geojson")
	os.WriteFile(path, moved, 0o644)
	_, findings, _, err := Run(context.Background(), ds, Options{Format: GeoJSON, Paths: []string{path}}, t.TempDir(), 1, nil)
	fatal := false
	for _, f := range findings {
		fatal = fatal || f.Code == "coordinates.extent" && f.Severity == "fatal"
	}
	if err != nil || !fatal {
		t.Errorf("polygons of another place: %v %+v", err, findings)
	}
}

// shift moves GeoJSON coordinates 1 mm away.
func shift(v any) {
	list, _ := v.([]any)
	if len(list) == 2 {
		if x, ok := list[0].(float64); ok {
			list[0] = x + 1000
			list[1] = list[1].(float64) + 1000
			return
		}
	}
	for _, item := range list {
		shift(item)
	}
}

func TestFixtureBaysor(t *testing.T) {
	ds, e := loadFixture(t)
	rename := map[string]string{}
	for i, c := range e.Cells {
		rename[jsonID(uint64(i+1))] = c
	}
	r := importFixture(t, ds, Options{Format: Baysor, Paths: []string{"baysor/segmentation.csv", "baysor/segmentation_polygons.json"}})
	r.check(t, "Baysor", ds, e, rename, nil)
	if r.rec.Read["transcripts_by"] != "position and gene" || r.rec.Tool != "Baysor" {
		t.Errorf("read %v, tool %q", r.rec.Read, r.rec.Tool)
	}
	if f := r.finding("cells.no-polygon"); f == nil || f.Count != 1 {
		t.Errorf("G has no polygon: %+v", f)
	}
}

func TestFixtureXeniumRanger(t *testing.T) {
	ds, e := loadFixture(t)
	r := importFixture(t, ds, Options{Format: Xenium, Paths: []string{"ranger"}})
	r.check(t, "Xenium Ranger", ds, e, nil, map[string]bool{"G": true})
	if r.rec.Tool != "Xenium Ranger" || r.rec.Version != "xenium-ranger-3.1.0.4" || r.rec.Read["table_agrees"] != "6 of 6 cells" {
		t.Errorf("tool %q %q; read %v", r.rec.Tool, r.rec.Version, r.rec.Read)
	}
	other := *ds
	other.RunName = "another run"
	opt := Options{Format: Xenium, Paths: []string{filepath.Join("testdata", "ranger")}}
	if _, _, _, err := Run(context.Background(), &other, opt, t.TempDir(), 1, nil); err == nil || !strings.Contains(err.Error(), "another run") {
		t.Errorf("a bundle of another run: %v", err)
	}
	noIDs := *ds
	noIDs.noIDs = true
	if _, _, _, err := Run(context.Background(), &noIDs, opt, t.TempDir(), 1, nil); err == nil || !strings.Contains(err.Error(), "ids") {
		t.Errorf("a dataset without transcript ids: %v", err)
	}
}

// Ids are not enough: a SpatialData store's transcripts carry their positions, and those named by
// an id of the dataset's but lying elsewhere are another dataset's.
func TestFixtureTranscriptsElsewhere(t *testing.T) {
	ds, _ := loadFixture(t)
	moved := *ds
	moved.List = append(moved.List[:0:0], ds.List...)
	for i := range moved.List {
		moved.List[i].X += 1
	}
	r := importFixture(t, &moved, Options{Format: SpatialData, Paths: []string{"shapes.zarr"}})
	if f := r.finding("transcripts.elsewhere"); f == nil || f.Count != len(ds.List) {
		t.Errorf("transcripts elsewhere: %+v", r.findings)
	}
	if f := r.finding("transcripts.unknown"); f == nil || f.Severity != "fatal" {
		t.Errorf("none of the import's transcripts is the dataset's: %+v", f)
	}
}

// What cannot be read is refused plainly.
func TestImportsRefused(t *testing.T) {
	ds, _ := loadFixture(t)
	for name, opt := range map[string]Options{
		"no paths":        {Format: GeoJSON},
		"unknown format":  {Format: "cellpose", Paths: []string{"x"}},
		"bad units":       {Format: GeoJSON, Paths: []string{"testdata/geojson/cells.geojson"}, Units: "mm"},
		"not a store":     {Format: SpatialData, Paths: []string{"testdata/geojson"}},
		"no such element": {Format: SpatialData, Paths: []string{"testdata/shapes.zarr"}, Cells: "nuclei"},
		"no cell column":  {Format: GeoJSON, Paths: []string{"testdata/geojson/cells.geojson", "testdata/geojson/assignments.csv"}, CellColumn: "segment"},
		"neither file":    {Format: GeoJSON, Paths: []string{"testdata/dataset.txt"}},
	} {
		if _, _, _, err := Run(context.Background(), ds, opt, t.TempDir(), 1, nil); err == nil {
			t.Errorf("%s: imported", name)
		}
	}
}
