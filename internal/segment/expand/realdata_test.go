package expand

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
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

// The Lung bundle's own segmentation grew its nuclei by 5 µm; SituCraft's expansion of the same
// nuclei by the same distance reproduces it within the tolerances below, and every pixel and
// transcript the two treat differently is one of three kinds (research.md §3.1 gives the measured
// numbers):
//   - the rim: a pixel within 5 µm of its nucleus that the instrument's cell does not reach (its
//     cells reach less far than 5 µm between the image's axes and diagonals);
//   - a boundary between neighbours: a pixel nearly equally far from two nuclei, given to the
//     other by the instrument;
//   - the instrument's record: a transcript whose cell_id is not its own mask's at the
//     transcript's pixel (on a pixel's edge, or outside the mask).
func TestRealExpansionReproducesTheInstruments(t *testing.T) {
	b, err := xenium.Open(bundleDir(t, "xenium-lung-2fov"), xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	dir := t.TempDir()
	d, err := FromBundle(context.Background(), b, dir, Params{DistanceUm: 5}, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	for _, f := range segment.Check(dir, &m) {
		if f.Severity == "fatal" {
			t.Fatalf("the check fails: %+v", f)
		}
		t.Logf("finding: %+v", f)
	}
	var a Agreement
	if err := json.Unmarshal(d.Agreement, &a); err != nil {
		t.Fatal(err)
	}
	pretty, _ := json.MarshalIndent(a, "", "  ")
	t.Logf("agreement: %s", pretty)
	t.Logf("summary: %s", m.Summary)

	// The tolerances.
	if c := a.Cells; c.SituCraft != 11898 || c.Instrument != 11898 || d.Nuclei.Count != 11898 || c.Paired != 11898 || c.InstrumentWithoutNucleus != 0 || c.InstrumentWithSeveral != 0 || c.NucleiOutside != 0 {
		t.Errorf("cells: %+v, nuclei %d; want 11,898 of each, paired one to one", c, d.Nuclei.Count)
	}
	if a.Pixels.Agreement < 0.98 {
		t.Errorf("pixels agree %.4f, want at least 0.98", a.Pixels.Agreement)
	}
	if a.Transcripts.Agreement < 0.985 {
		t.Errorf("transcripts agree %.4f, want at least 0.985", a.Transcripts.Agreement)
	}
	if math.Abs(a.Area.MedianRelativeDifference) > 0.005 || a.Area.Within5 < 0.96 || a.Area.Within10 < 0.995 {
		t.Errorf("areas: median difference %.4f, %.3f within 5%%, %.3f within 10%%; want under 0.5%%, at least 96%% and 99.5%%", a.Area.MedianRelativeDifference, a.Area.Within5, a.Area.Within10)
	}
	if diff := a.AssignedFraction.SituCraft - a.AssignedFraction.Instrument; diff < 0 || diff > 0.005 {
		t.Errorf("assigned fraction %.4f, the instrument's %.4f; want at most 0.005 more", a.AssignedFraction.SituCraft, a.AssignedFraction.Instrument)
	}

	// Every difference is one of the three kinds.
	const rimFrom, rimTo, gapAtMost = 4.5, 5.0, 0.4 // µm
	x := a.Differences
	if x == nil {
		t.Fatal("no differences recorded")
	}
	if a.Pixels.InstrumentOnly != 0 || x.SituCraftOnlyUm[0] < rimFrom || x.SituCraftOnlyUm[1] > rimTo || x.OtherCellGapUm > gapAtMost {
		t.Errorf("pixels: %d only the instrument's, SituCraft's alone %.3f–%.3f µm from their nuclei, boundaries %.3f µm from equidistant; want none, %.1f–%.1f and at most %.1f",
			a.Pixels.InstrumentOnly, x.SituCraftOnlyUm[0], x.SituCraftOnlyUm[1], x.OtherCellGapUm, rimFrom, rimTo, gapAtMost)
	}
	// Transcript by transcript, independently of the agreement's own tally.
	ours, err := segment.ReadAssignments(dir)
	if err != nil {
		t.Fatal(err)
	}
	cellR, err := segment.ReadMask(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, nuc := maskRaster(t, b, xenium.CellMask), maskRaster(t, b, xenium.NucleusMask)
	cells, err := b.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	spans := spansOf(nuc, len(cells.IDs), nil)
	tr, err := b.OpenTranscripts()
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	kinds := map[string]int{}
	k := 0
	ps := cellR.PixelSize
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i, k = i+1, k+1 {
			theirs := uint32(0)
			if row, ok := cells.Index[string(batch.CellID[i])]; ok {
				theirs = uint32(row) + 1
			}
			mine := ours[k]
			if mine == theirs {
				kinds["same"]++
				continue
			}
			x, y := float64(batch.X[i]), float64(batch.Y[i])
			px, py := int(math.Floor(x/ps)), int(math.Floor(y/ps))
			switch {
			case ref.AtMicrons(x, y) != theirs:
				kinds["the instrument's record"]++
			case mine != 0 && theirs == 0 && distance(spans[mine], px, py)*ps >= rimFrom && distance(spans[mine], px, py)*ps <= rimTo:
				kinds["rim"]++
			case mine != 0 && theirs != 0 && (distance(spans[theirs], px, py)-distance(spans[mine], px, py))*ps <= gapAtMost:
				kinds["boundary"]++
			default:
				kinds["unexplained"]++
				if kinds["unexplained"] <= 5 {
					t.Errorf("transcript %d at (%.3f, %.3f) µm: SituCraft's cell %d, the instrument's %d", k, x, y, mine, theirs)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("transcripts: %v", kinds)
	if kinds["unexplained"] > 0 {
		t.Errorf("%d transcripts given differently are not explained", kinds["unexplained"])
	}
	if int64(kinds["the instrument's record"]) != x.TranscriptsOffMask {
		t.Errorf("%d transcripts the instrument records off its mask; the agreement says %d", kinds["the instrument's record"], x.TranscriptsOffMask)
	}
}

// maskRaster reads one of the bundle's masks as a raster.
func maskRaster(t *testing.T, b *xenium.Bundle, index int) *segment.Raster {
	t.Helper()
	m, err := b.OpenMask(index)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w, h := m.Size()
	r := &segment.Raster{Width: w, Height: h, PixelSize: m.PixelSize, Rows: make([][]segment.Run, h)}
	row := make([]uint32, w)
	for y := 0; y < h; y++ {
		if err := m.Rows(y, y+1, row); err != nil {
			t.Fatal(err)
		}
		r.Rows[y] = segment.EncodeRow(row)
	}
	return r
}

// A bundle without a nucleus mask has its nucleus polygons drawn instead. The Lung bundle's
// polygons, drawn, cover nearly the mask's pixels (they outline them, simplified: what differs is
// where a simplified edge cuts a corner), and grow into nearly the instrument's cells.
func TestRealNucleusPolygonsDrawnNearTheMask(t *testing.T) {
	b, err := xenium.Open(bundleDir(t, "xenium-lung-2fov"), xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	cells, err := b.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	drawn, owners, size, err := rasterizeNuclei(b, cells)
	if err != nil {
		t.Fatal(err)
	}
	mask := maskRaster(t, b, xenium.NucleusMask)
	if drawn.Width != mask.Width || drawn.Height != mask.Height || math.Abs(size-mask.PixelSize) > 1e-6 {
		t.Fatalf("drawn %d × %d at %g µm; the mask %d × %d at %g", drawn.Width, drawn.Height, size, mask.Width, mask.Height, mask.PixelSize)
	}
	row := make([]uint32, mask.Width)
	var same, union int64
	for y := 0; y < mask.Height; y++ {
		mask.Row(y, row)
		for x, m := range row {
			d := owners[drawn.Labels[y*drawn.Width+x]] // the Lung bundle's nucleus k is cell k's
			if m != 0 || d != 0 {
				union++
				if m == d {
					same++
				}
			}
		}
	}
	t.Logf("drawn nuclei: %.4f of %d pixels as the mask's", float64(same)/float64(union), union)
	if float64(same)/float64(union) < 0.98 {
		t.Errorf("the drawn nuclei agree with the mask on %.4f of their pixels, want at least 0.98", float64(same)/float64(union))
	}
	r, _, err := Expand(context.Background(), drawn, Options{Distance: 5 / size, Workers: runtime.NumCPU()})
	if err != nil {
		t.Fatal(err)
	}
	ref := maskRaster(t, b, xenium.CellMask)
	theirs := make([]uint32, r.Width)
	same, union = 0, 0
	for y := 0; y < r.Height; y++ {
		r.Row(y, row)
		ref.Row(y, theirs)
		for x := range row {
			row[x] = owners[row[x]]
			if row[x] != 0 || theirs[x] != 0 {
				union++
				if row[x] == theirs[x] {
					same++
				}
			}
		}
	}
	t.Logf("grown from the drawn nuclei: %.4f of %d pixels as the instrument's cells", float64(same)/float64(union), union)
	if float64(same)/float64(union) < 0.97 {
		t.Errorf("cells grown from the drawn nuclei agree with the instrument's on %.4f of their pixels, want at least 0.97", float64(same)/float64(union))
	}
}

// The Breast bundle's instrument segmentation drew most cells from cell stains: some hold no
// nucleus, some several, and its nucleus labels are not its cells'. Each nucleus grows into a cell
// of its own, compared with the instrument's cell it lies in.
func TestRealExpansionOfStainSegmentedCells(t *testing.T) {
	b, err := xenium.Open(bundleDir(t, "xenium-breast-2fov"), xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	dir := t.TempDir()
	d, err := FromBundle(context.Background(), b, dir, Params{DistanceUm: 5}, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	for _, f := range segment.Check(dir, &m) {
		if f.Severity == "fatal" {
			t.Fatalf("the check fails: %+v", f)
		}
	}
	var a Agreement
	if err := json.Unmarshal(d.Agreement, &a); err != nil {
		t.Fatal(err)
	}
	pretty, _ := json.MarshalIndent(a, "", "  ")
	t.Logf("agreement: %s", pretty)
	c := a.Cells
	if c.SituCraft != 7312 || d.Nuclei.Count != 7312 || c.Instrument != 7275 || c.Paired+c.InstrumentWithoutNucleus+c.InstrumentWithSeveral != c.Instrument {
		t.Errorf("cells %+v, nuclei %d; want 7,312 nuclei and the instrument's 7,275 cells accounted for", c, d.Nuclei.Count)
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		t.Fatal(err)
	}
	named := 0
	for _, cell := range cells {
		if cell.Nucleus != "" {
			named++
		}
	}
	if named != c.SituCraft-c.NucleiOutside {
		t.Errorf("%d cells name the instrument's cell their nucleus lies in; want %d", named, c.SituCraft-c.NucleiOutside)
	}
	if a.Pixels.Agreement < 0.5 || a.Transcripts.Agreement < 0.5 || a.Differences == nil {
		t.Errorf("pixels agree %.3f, transcripts %.3f", a.Pixels.Agreement, a.Transcripts.Agreement)
	}
}
