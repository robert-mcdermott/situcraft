package imports

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

func bundleDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("..", "..", "..", "validation", "cache")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err != nil {
		want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
		if want == "1" || strings.Contains(","+want+",", ","+name+",") {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

// instrument is the Lung bundle's own segmentation, as the instrument wrote it.
type instrument struct {
	ids   []string
	cell  []string // each transcript's cell_id ("" none)
	tid   []uint64
	x, y  []float32
	gene  []string
	mask  *segment.Raster
	cells *xenium.Cells
}

func readInstrument(t *testing.T, b *xenium.Bundle) *instrument {
	t.Helper()
	in := &instrument{}
	cells, err := b.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	in.cells, in.ids = cells, cells.IDs
	tr, err := b.OpenTranscripts()
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			c := string(batch.CellID[i])
			if c == "UNASSIGNED" {
				c = ""
			}
			in.cell = append(in.cell, c)
			in.tid = append(in.tid, batch.TranscriptID[i])
			in.x, in.y = append(in.x, batch.X[i]), append(in.y, batch.Y[i])
			in.gene = append(in.gene, string(batch.Feature[i]))
		}
		return nil
	})
	m, err := b.OpenMask(xenium.CellMask)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w, h := m.Size()
	in.mask = &segment.Raster{Width: w, Height: h, PixelSize: m.PixelSize, Rows: make([][]segment.Run, h)}
	row := make([]uint32, w)
	for y := 0; y < h; y++ {
		m.Rows(y, y+1, row)
		in.mask.Rows[y] = segment.EncodeRow(row)
	}
	return in
}

// imported runs an import of the Lung bundle and checks what every import must be: it passes the
// segmentation check, with no fatal finding of its own.
func imported(t *testing.T, b *xenium.Bundle, opt Options) (string, *segment.Description, []artifacts.Finding, Recorded) {
	t.Helper()
	ds, err := BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	d, findings, files, err := Run(context.Background(), ds, opt, dir, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	for _, f := range append(findings, segment.Check(dir, &m)...) {
		if f.Severity == "fatal" {
			t.Fatalf("fatal: %+v", f)
		}
		t.Logf("finding: %s %s (%d)", f.Severity, f.Code, f.Count)
	}
	if len(files) == 0 {
		t.Error("no source files recorded")
	}
	var rec Recorded
	json.Unmarshal(d.Parameters, &rec)
	t.Logf("recorded: %s", d.Parameters)
	return dir, d, findings, rec
}

// sameAssignments compares the import's assignment of each transcript with the instrument's.
func sameAssignments(t *testing.T, dir string, in *instrument) {
	t.Helper()
	got, err := segment.ReadAssignments(dir)
	if err != nil {
		t.Fatal(err)
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in.cell) {
		t.Fatalf("%d assignments for %d transcripts", len(got), len(in.cell))
	}
	differ := 0
	for i, k := range got {
		id := ""
		if k > 0 {
			id = cells[k-1].ID
		}
		if id != in.cell[i] {
			differ++
			if differ <= 3 {
				t.Errorf("transcript %d: %q, the instrument's %q", in.tid[i], id, in.cell[i])
			}
		}
	}
	if differ > 0 {
		t.Errorf("%d of %d transcripts assigned otherwise than the instrument did", differ, len(got))
	}
}

// rasterAgreement is the share of pixels either labels that the import and the instrument's mask
// give the same cell.
func rasterAgreement(t *testing.T, dir string, in *instrument) float64 {
	t.Helper()
	r, err := segment.ReadMask(dir)
	if err != nil {
		t.Fatal(err)
	}
	cells, _ := segment.ReadCells(dir)
	if r.Width != in.mask.Width || r.Height != in.mask.Height {
		t.Fatalf("raster %d × %d, the mask %d × %d", r.Width, r.Height, in.mask.Width, in.mask.Height)
	}
	a, b := make([]uint32, r.Width), make([]uint32, r.Width)
	var same, union int64
	for y := 0; y < r.Height; y++ {
		r.Row(y, a)
		in.mask.Row(y, b)
		for x := range a {
			if a[x] == 0 && b[x] == 0 {
				continue
			}
			union++
			if a[x] != 0 && b[x] != 0 && cells[a[x]-1].ID == in.ids[b[x]-1] {
				same++
			}
		}
	}
	return float64(same) / float64(union)
}

// A Xenium output bundle of the same run imports as the segmentation it holds: the Lung bundle into
// itself gives the instrument's cells, mask and assignments exactly.
func TestRealXeniumBundleImportsExactly(t *testing.T) {
	dirLung := bundleDir(t, "xenium-lung-2fov")
	b, err := xenium.Open(dirLung, xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	in := readInstrument(t, b)
	dir, d, _, rec := imported(t, b, Options{Format: Xenium, Paths: []string{dirLung}})
	if d.Cells != 11898 {
		t.Errorf("%d cells", d.Cells)
	}
	if a := rasterAgreement(t, dir, in); a != 1 {
		t.Errorf("the raster agrees with the mask on %.6f of pixels, not all", a)
	}
	sameAssignments(t, dir, in)
	if rec.Read["table_agrees"] != "11898 of 11898 cells" {
		t.Errorf("cells.parquet's counts agree for %v", rec.Read["table_agrees"])
	}
}

// Polygons (GeoJSON) and a transcript-to-cell table round trip: the Lung bundle's own polygons and
// assignments, written out and imported, give the instrument's assignments exactly, matched by
// transcript id (a GeoJSON import) or by position and gene (a Baysor-style table without ids).
func TestRealPolygonsAndTablesRoundTrip(t *testing.T) {
	dirLung := bundleDir(t, "xenium-lung-2fov")
	b, err := xenium.Open(dirLung, xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	in := readInstrument(t, b)
	out := t.TempDir()
	// The polygons, as a FeatureCollection.
	p, err := b.ReadBoundaries("cell_boundaries.parquet")
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString(`{"type":"FeatureCollection","features":[`)
	for k := 0; k+1 < len(p.Offsets); k++ {
		if k > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"type":"Feature","properties":{"cell_id":%q},"geometry":{"type":"Polygon","coordinates":[[`, p.CellIDs[k])
		for v := p.Offsets[k]; v < p.Offsets[k+1]; v++ {
			if v > p.Offsets[k] {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "[%s,%s]", strconv.FormatFloat(float64(p.X[v]), 'g', -1, 32), strconv.FormatFloat(float64(p.Y[v]), 'g', -1, 32))
		}
		sb.WriteString("]]}}")
	}
	sb.WriteString("]}")
	polygons := filepath.Join(out, "cells.geojson")
	os.WriteFile(polygons, []byte(sb.String()), 0o644)
	// The assignments: with ids (gzipped CSV), and Baysor-style without (x, y, gene, cell).
	write := func(name string, header string, row func(i int) string) string {
		path := filepath.Join(out, name)
		f, _ := os.Create(path)
		z := gzip.NewWriter(f)
		fmt.Fprintln(z, header)
		for i := range in.cell {
			fmt.Fprintln(z, row(i))
		}
		z.Close()
		f.Close()
		return path
	}
	byID := write("assignments.csv.gz", "transcript_id,cell_id", func(i int) string {
		c := in.cell[i]
		if c == "" {
			c = "UNASSIGNED"
		}
		return fmt.Sprintf("%d,%s", in.tid[i], c)
	})
	byPosition := write("segmentation.csv.gz", "x,y,gene,cell,is_noise", func(i int) string {
		noise := "false"
		if in.cell[i] == "" {
			noise = "true"
		}
		return fmt.Sprintf("%s,%s,%s,%s,%s", strconv.FormatFloat(float64(in.x[i]), 'g', -1, 32), strconv.FormatFloat(float64(in.y[i]), 'g', -1, 32), in.gene[i], in.cell[i], noise)
	})

	dir, d, _, rec := imported(t, b, Options{Format: GeoJSON, Paths: []string{polygons, byID}})
	if d.Cells != 11898 || rec.Read["cell_property"] != "cell_id" || rec.Read["transcripts_by"] != "transcript_id" {
		t.Errorf("%d cells; read %v", d.Cells, rec.Read)
	}
	sameAssignments(t, dir, in)
	agree := rasterAgreement(t, dir, in)
	t.Logf("the polygons drawn agree with the instrument's mask on %.4f of pixels", agree)
	if agree < 0.85 {
		t.Errorf("the polygons drawn agree with the mask on %.4f of pixels, want at least 0.85", agree)
	}

	dir, _, _, rec = imported(t, b, Options{Format: Baysor, Paths: []string{byPosition, polygons}, CellProperty: "cell_id"})
	if rec.Read["transcripts_by"] != "position and gene" {
		t.Errorf("read %v", rec.Read)
	}
	sameAssignments(t, dir, in)
}

// A SpatialData store written by spatialdata-io from the Lung bundle (validation/spatialdata.py)
// imports as the instrument's segmentation: its cell label image, the cells named through its
// table, and its transcripts' cells. spatialdata-io's global coordinate system is in the
// morphology image's pixels; read as µm, the cells lie outside the image, which the import says.
func TestRealSpatialDataFromSpatialdataIO(t *testing.T) {
	dirLung := bundleDir(t, "xenium-lung-2fov")
	store := filepath.Join(filepath.Dir(dirLung), "xenium-lung-2fov.spatialdata.zarr")
	if v := os.Getenv("SITUCRAFT_SPATIALDATA"); v != "" {
		store = v
	}
	if _, err := os.Stat(store); err != nil {
		if os.Getenv("SITUCRAFT_REQUIRE_SPATIALDATA") != "" {
			t.Fatalf("%s is missing (validation/spatialdata.py writes it)", store)
		}
		t.Skipf("%s is missing (validation/spatialdata.py writes it)", store)
	}
	b, err := xenium.Open(dirLung, xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	in := readInstrument(t, b)
	ds, err := BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	_, findings, _, err := Run(context.Background(), ds, Options{Format: SpatialData, Paths: []string{store}}, t.TempDir(), runtime.NumCPU(), nil)
	extent := ""
	for _, f := range findings {
		if f.Code == "coordinates.extent" && f.Severity == "fatal" {
			extent = f.Message
		}
	}
	if err != nil || !strings.Contains(extent, "Read as pixels, 11898 would") {
		t.Errorf("read as µm: %v %+v", err, findings)
	}
	dir, d, _, rec := imported(t, b, Options{Format: SpatialData, Paths: []string{store}, Units: "pixels"})
	if d.Cells != 11898 || rec.Read["cells"] != "labels/cell_labels" || rec.Read["outlines"] != "shapes/cell_boundaries" {
		t.Errorf("%d cells; read %v", d.Cells, rec.Read)
	}
	if a := rasterAgreement(t, dir, in); a != 1 {
		t.Errorf("the labels agree with the mask on %.6f of pixels, not all", a)
	}
	sameAssignments(t, dir, in)
	if rec.Read["table_agrees"] != "11898 of 11898 cells" {
		t.Errorf("the table's counts agree for %v", rec.Read["table_agrees"])
	}
}

// The development bundles share anonymized run names ("run", "region") and transcript ids, but not
// their experiment: the Breast bundle is refused as a segmentation of the Lung run.
func TestRealAnotherRunIsRefused(t *testing.T) {
	dirLung := bundleDir(t, "xenium-lung-2fov")
	dirBreast := bundleDir(t, "xenium-breast-2fov")
	b, err := xenium.Open(dirLung, xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ds, err := BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = Run(context.Background(), ds, Options{Format: Xenium, Paths: []string{dirBreast}}, t.TempDir(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "another run") {
		t.Errorf("the Breast bundle into the Lung dataset: %v", err)
	}
}

// The bundle's own segmentation, written as SituCraft's files row by row, is the instrument's.
func TestRealInstrumentAsSegmentationFiles(t *testing.T) {
	dirLung := bundleDir(t, "xenium-lung-2fov")
	b, err := xenium.Open(dirLung, xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	in := readInstrument(t, b)
	dir := t.TempDir()
	d, err := Instrument(context.Background(), b, dir, runtime.NumCPU())
	if err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	for _, f := range segment.Check(dir, &m) {
		if f.Severity == "fatal" {
			t.Fatalf("fatal: %+v", f)
		}
	}
	if d.Cells != 11898 {
		t.Errorf("%d cells", d.Cells)
	}
	if a := rasterAgreement(t, dir, in); a != 1 {
		t.Errorf("the raster agrees with the mask on %.6f", a)
	}
	sameAssignments(t, dir, in)
}
