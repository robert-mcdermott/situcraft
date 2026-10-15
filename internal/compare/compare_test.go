package compare

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
)

// Hand-auditable segmentations: rows of text, one character a pixel of 1 µm ('.' none, '1'–'9'
// a cell), and transcripts at pixel centres, each assigned to the cell holding its pixel.

func raster(rows ...string) *segment.Raster {
	r := &segment.Raster{Width: len(rows[0]), Height: len(rows), PixelSize: 1}
	for _, s := range rows {
		labels := make([]uint32, len(s))
		for i, c := range s {
			if c != '.' {
				labels[i] = uint32(c - '0')
			}
		}
		r.Rows = append(r.Rows, segment.EncodeRow(labels))
	}
	return r
}

type tx struct {
	x, y int
	gene string
}

// seg makes a segmentation of a raster and the transcripts (gene "" is ACTA2).
func seg(r *segment.Raster, txs []tx) *Seg {
	n := 0
	for _, row := range r.Rows {
		for _, run := range row {
			n = max(n, int(run.Label))
		}
	}
	stats := r.Stats(n)
	s := &Seg{Mask: r, Desc: &segment.Description{}}
	for i := 0; i < n; i++ {
		c := segment.Cell{ID: "c" + strconv.Itoa(i+1)}
		if st := stats[i]; st.Pixels > 0 {
			c.Area = float32(st.Pixels)
			c.X, c.Y = float32(st.SumX/float64(st.Pixels)), float32(st.SumY/float64(st.Pixels))
		}
		s.Cells = append(s.Cells, c)
		s.Outlines = append(s.Outlines, segment.Outline{Pieces: 1})
	}
	genes := map[string]map[int]uint32{}
	for _, t := range txs {
		label := r.At(t.x, t.y)
		s.Assign = append(s.Assign, label)
		s.Desc.Q20GeneTranscripts++
		if label == 0 {
			continue
		}
		s.Desc.Q20GeneAssigned++
		s.Cells[label-1].Transcripts++
		g := t.gene
		if g == "" {
			g = "ACTA2"
		}
		if genes[g] == nil {
			genes[g] = map[int]uint32{}
		}
		genes[g][int(label)-1]++
	}
	s.Counts = countsOf(genes, n)
	return s
}

func countsOf(genes map[string]map[int]uint32, n int) *segment.Counts {
	c := &segment.Counts{Offsets: make([]uint32, n+1)}
	for g := range genes {
		c.Genes = append(c.Genes, g)
	}
	sort.Strings(c.Genes)
	for cell := 0; cell < n; cell++ {
		for gi, g := range c.Genes {
			if k := genes[g][cell]; k > 0 {
				c.Gene = append(c.Gene, uint32(gi))
				c.Count = append(c.Count, k)
			}
		}
		c.Offsets[cell+1] = uint32(len(c.Gene))
	}
	return c
}

// everywhere puts a transcript at every pixel's centre.
func everywhere(w, h int) []tx {
	var out []tx
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out = append(out, tx{x: x, y: y})
		}
	}
	return out
}

func transcriptsOf(txs []tx) *Transcripts {
	t := &Transcripts{}
	for _, v := range txs {
		t.X, t.Y = append(t.X, float32(v.x)+0.5), append(t.Y, float32(v.y)+0.5)
		t.Q20Gene = append(t.Q20Gene, true)
	}
	return t
}

func compare(t *testing.T, a, b *Seg, txs []tx, nuclei *segment.Raster) *Result {
	t.Helper()
	p := DefaultParams()
	p.RegionUm = 4
	r, err := Compare(context.Background(), Inputs{A: a, B: b, Transcripts: transcriptsOf(txs), Nuclei: nuclei, Params: p})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("the result is not JSON: %v", err)
	}
	return r
}

// classes lists the groups' classes with their cells, as "class:A cells/B cells".
func classes(r *Result) []string {
	var out []string
	for _, g := range r.Groups {
		out = append(out, g.Class+":"+strconv.Itoa(len(g.A))+"/"+strconv.Itoa(len(g.B)))
	}
	sort.Strings(out)
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestIdenticalAndShifted(t *testing.T) {
	same := raster(
		".111.",
		".111.",
		".111.",
	)
	txs := everywhere(5, 3)
	r := compare(t, seg(same, txs), seg(same, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"one-to-one:1/1"}) {
		t.Errorf("identical: %v", got)
	}
	if r.IoU.Median != 1 || r.Displacement.MedianUm != 0 || r.Changes.Changed != 0 || r.PixelAgreement != 1 {
		t.Errorf("identical: IoU %v, displacement %v, changed %d, pixels %v", r.IoU.Median, r.Displacement.MedianUm, r.Changes.Changed, r.PixelAgreement)
	}
	// Shifted one pixel to the right: 6 of 12 pixels in both (IoU 0.5); the left and right sides
	// move 1 µm and the top and bottom 0, so the boundary moves half a µm on average: (9 + 9 −
	// 2·6) / 12 = 0.5 µm. The transcripts of the column left behind are lost, the new one's gained.
	shifted := raster(
		"..111",
		"..111",
		"..111",
	)
	r = compare(t, seg(same, txs), seg(shifted, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"one-to-one:1/1"}) {
		t.Errorf("shifted: %v", got)
	}
	if !near(float64(r.IoU.Median), 0.5) || !near(float64(r.Displacement.MedianUm), 0.5) {
		t.Errorf("shifted: IoU %v, displacement %v", r.IoU.Median, r.Displacement.MedianUm)
	}
	if c := r.Changes; c.Changed != 6 || c.Gained != 3 || c.Lost != 3 || c.Moved != 0 || c.Transcripts != 15 {
		t.Errorf("shifted: changes %+v", c)
	}
	if !near(r.PixelAgreement, 0.5) {
		t.Errorf("shifted: pixel agreement %v", r.PixelAgreement)
	}
}

func TestBoundaryBetweenNeighbours(t *testing.T) {
	// The boundary between two cells moves one column: cell 1 loses 3 of 9 pixels (6/9 = 0.667:
	// contracted), cell 2 gains them (12/9 = 1.33: expanded); the 3 transcripts of that column
	// move from one cell to the other. The column is a third of A's cell 1 and a quarter of B's
	// cell 2, less than half of either: they do not correspond.
	a := raster("111222", "111222", "111222")
	b := raster("112222", "112222", "112222")
	txs := everywhere(6, 3)
	r := compare(t, seg(a, txs), seg(b, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"contracted:1/1", "expanded:1/1"}) {
		t.Errorf("classes %v", got)
	}
	if c := r.Changes; c.Changed != 3 || c.Moved != 3 {
		t.Errorf("changes %+v", c)
	}
	// IoUs 6/9 and 9/12: their median is their mean, 0.70833.
	if !near(float64(r.IoU.Median), (6.0/9+9.0/12)/2) {
		t.Errorf("IoU %v", r.IoU.Median)
	}
}

// A cell split in halves: each half lies wholly within it. Columns become rows: each lies half
// within each of the others.
func TestSplitsMergesAndRearranged(t *testing.T) {
	whole := raster("111111", "111111")
	halves := raster("111222", "111222")
	txs := everywhere(6, 2)
	r := compare(t, seg(whole, txs), seg(halves, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"split:1/2"}) {
		t.Errorf("split: %v", got)
	}
	// Every transcript is in a cell in both, but the cells are not the same cell: all moved.
	if c := r.Changes; c.Moved != 12 || c.Changed != 12 || r.PixelAgreement != 0 {
		t.Errorf("split: changes %+v, pixels %v", c, r.PixelAgreement)
	}
	r = compare(t, seg(halves, txs), seg(whole, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"merged:2/1"}) {
		t.Errorf("merged: %v", got)
	}
	columns := raster("1122", "1122", "1122", "1122")
	rows := raster("1111", "1111", "2222", "2222")
	txs = everywhere(4, 4)
	r = compare(t, seg(columns, txs), seg(rows, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"rearranged:2/2"}) {
		t.Errorf("rearranged: %v", got)
	}
}

func TestUnmatchedAndAssignmentOnly(t *testing.T) {
	a := raster("11..22", "11..22")
	b := raster("11....", "11....")
	txs := everywhere(6, 2)
	r := compare(t, seg(a, txs), seg(b, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"one-to-one:1/1", "only in A:1/0"}) {
		t.Errorf("classes %v", got)
	}
	if c := r.Changes; c.Lost != 4 || c.Changed != 4 {
		t.Errorf("changes %+v", c)
	}
	if r.CellsA[OnlyA] != 1 || r.A.Cells != 2 || r.B.Cells != 1 {
		t.Errorf("cells %+v, A %+v, B %+v", r.CellsA, r.A, r.B)
	}
	// B's second cell has no area (an import without its outline): it holds cell 2's transcripts,
	// so the two correspond by their transcripts.
	bs := seg(raster("11....", "11...."), txs)
	bs.Cells = append(bs.Cells, segment.Cell{ID: "c2"})
	bs.Outlines = append(bs.Outlines, segment.Outline{})
	for i, t := range txs {
		if t.x >= 4 {
			bs.Assign[i] = 2
			bs.Cells[1].Transcripts++
		}
	}
	r = compare(t, seg(a, txs), bs, txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"assignment-only:1/1", "one-to-one:1/1"}) {
		t.Errorf("assignment-only: %v", got)
	}
}

// Two cells overlapping by less than half of either (8 pixels: a fifth of A's 40, a quarter of
// B's 32) correspond when they share a nucleus; then B's area is 0.8 times A's: one-to-one.
func TestSharedNucleus(t *testing.T) {
	a := raster(
		"1111111111......",
		"1111111111......",
		"1111111111......",
		"1111111111......",
	)
	b := raster(
		"........11111111",
		"........11111111",
		"........11111111",
		"........11111111",
	)
	nuclei := raster(
		"................",
		"........11......",
		"........11......",
		"................",
	)
	txs := everywhere(16, 4)
	r := compare(t, seg(a, txs), seg(b, txs), txs, nil)
	if got := classes(r); !reflect.DeepEqual(got, []string{"only in A:1/0", "only in B:0/1"}) {
		t.Errorf("without nuclei: %v", got)
	}
	r = compare(t, seg(a, txs), seg(b, txs), txs, nuclei)
	if got := classes(r); !reflect.DeepEqual(got, []string{"one-to-one:1/1"}) {
		t.Errorf("with the nucleus: %v", got)
	}
	if r.A.CellsWithNucleus != 1 || r.B.CellsWithNucleus != 1 {
		t.Errorf("cells with a nucleus: %d, %d", r.A.CellsWithNucleus, r.B.CellsWithNucleus)
	}
}

func TestRegions(t *testing.T) {
	// Two regions of 4 µm across: the left cell is the same, the right one shifted.
	a := raster("111..222", "111..222")
	b := raster("111...22", "111...22")
	txs := everywhere(8, 2)
	r := compare(t, seg(a, txs), seg(b, txs), txs, nil)
	g := r.Regions
	if g.Cols != 2 || g.Rows != 1 {
		t.Fatalf("regions %d × %d", g.Cols, g.Rows)
	}
	if g.Transcripts[0] != 8 || g.Transcripts[1] != 8 || g.Changed[0] != 0 || g.Changed[1] != 2 {
		t.Errorf("transcripts %v, changed %v", g.Transcripts, g.Changed)
	}
	if g.Disagree[0] != 0 || g.Disagree[1] != 2 || g.Pixels[0] != 6 || g.Pixels[1] != 6 {
		t.Errorf("pixels %v, disagreeing %v", g.Pixels, g.Disagree)
	}
	if g.MeanIoU[0] != 1 {
		t.Errorf("mean IoU %v", g.MeanIoU)
	}
}

func TestMECR(t *testing.T) {
	// Cells expressing EPCAM, PTPRC, both, neither: one of the three expressing either expresses
	// both.
	c := countsOf(map[string]map[int]uint32{"EPCAM": {0: 3, 2: 1}, "PTPRC": {1: 2, 2: 5}}, 4)
	if got := MECR(c, [][2]string{{"EPCAM", "PTPRC"}, {"EPCAM", "NOTINPANEL"}}); !near(got, 1.0/3) {
		t.Errorf("MECR %v", got)
	}
	// The third cell expresses the pair together.
	if got := MixedPairs(c, [][2]string{{"EPCAM", "PTPRC"}}); !reflect.DeepEqual(got, []uint32{0, 0, 1, 0}) {
		t.Errorf("mixed pairs %v", got)
	}
	pairs := LineagePairs([]string{"EPCAM", "PTPRC", "CD3E", "MS4A1", "VIM"})
	if !reflect.DeepEqual(pairs, [][2]string{{"MS4A1", "CD3E"}, {"MS4A1", "EPCAM"}, {"CD3E", "EPCAM"}}) {
		t.Errorf("lineage pairs %v", pairs)
	}
}

// Two cell types with genes of their own: nuclei hold a little of their neighbour's, a coarse
// expansion much more. The pairs across types are the spurious ones; the nuclei score 1, a
// segmentation between them more, the coarse expansion most.
func TestSpuriousCoexpression(t *testing.T) {
	typeX, typeY := []string{"X1", "X2", "X3", "X4", "X5"}, []string{"Y1", "Y2", "Y3", "Y4", "Y5"}
	counts := func(cells int, leak uint32) *segment.Counts {
		genes := map[string]map[int]uint32{}
		add := func(g string, cell int, k uint32) {
			if genes[g] == nil {
				genes[g] = map[int]uint32{}
			}
			genes[g][cell] += k
		}
		for cell := 0; cell < cells; cell++ {
			own, other := typeX, typeY
			if cell%2 == 1 {
				own, other = typeY, typeX
			}
			for i, g := range own {
				add(g, cell, uint32(12+i+cell%3))
			}
			for i, g := range other {
				if leak > 0 && (cell+i)%3 == 0 {
					add(g, cell, leak)
				}
			}
		}
		return countsOf(genes, cells)
	}
	nuclear, mid, coarse := counts(400, 1), counts(400, 3), counts(400, 8)
	p := DefaultParams()
	ref := NewReference(nuclear, coarse, p)
	if len(ref.Pairs) == 0 || ref.NuclearCells != 400 {
		t.Fatalf("%d spurious pairs on %d cells", len(ref.Pairs), ref.NuclearCells)
	}
	for _, pair := range ref.Pairs {
		if gi, gj := ref.Genes[pair[0]], ref.Genes[pair[1]]; gi[0] == gj[0] {
			t.Errorf("a pair within a type is spurious: %s given %s", gi, gj)
		}
	}
	sn, cells := ref.Score(nuclear, p)
	sm, _ := ref.Score(mid, p)
	sc, _ := ref.Score(coarse, p)
	if sn != 1 || cells != 400 || !(sm > 1.2) || !(sc > sm) {
		t.Errorf("scores: the nuclei %v (%d cells), between %v, the coarse expansion %v", sn, cells, sm, sc)
	}
	// Cells with fewer transcripts than the depth are left out.
	p.Depth = 1000
	if _, cells := ref.Score(nuclear, p); cells != 0 {
		t.Errorf("%d cells of fewer than 1,000 transcripts scored", cells)
	}
	if again := NewReference(nuclear, coarse, DefaultParams()); !reflect.DeepEqual(again, ref) {
		t.Error("the reference differs between runs: the down-sampling is not deterministic")
	}
}

// A comparison's files read back as written, pass the check, and the check finds damage.
func TestFilesAndCheck(t *testing.T) {
	a := raster("111..222", "111..222")
	b := raster("111...22", "111.....")
	txs := everywhere(8, 2)
	r := compare(t, seg(a, txs), seg(b, txs), txs, nil)
	write := func(r *Result) string {
		dir := t.TempDir()
		if err := Write(dir, Side{ID: "seg-a", Name: "A"}, Side{ID: "exp-b", Name: "B"}, r); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dir := write(r)
	var m artifacts.Manifest
	if f := Check(dir, &m); len(f) > 0 {
		t.Fatalf("a comparison fails its check: %+v", f)
	}
	groups, err := ReadGroups(dir)
	if err != nil || !reflect.DeepEqual(groups, r.Groups) {
		t.Errorf("groups %+v, %v; wrote %+v", groups, err, r.Groups)
	}
	d, err := ReadDescription(dir)
	if err != nil || d.A.ID != "seg-a" || d.MetricsB.Cells != 2 || d.Classes[OneToOne] != r.Classes[OneToOne] {
		t.Errorf("description %+v, %v", d, err)
	}
	var s Summary
	json.Unmarshal(m.Summary, &s)
	if s.B.Name != "B" || s.PixelAgreement != r.PixelAgreement {
		t.Errorf("summary %s", m.Summary)
	}
	codes := func(f []artifacts.Finding) string {
		var out []string
		for _, x := range f {
			out = append(out, x.Code)
		}
		return strings.Join(out, ",")
	}
	// A difference left out: its cells are in none.
	damaged := *r
	damaged.Groups = r.Groups[1:]
	if got := codes(Check(write(&damaged), &m)); !strings.Contains(got, "groups.cells") {
		t.Errorf("a difference left out: %s", got)
	}
	// A split of one cell to one.
	damaged = *r
	damaged.Groups = append([]Group(nil), r.Groups...)
	damaged.Groups[0].Class = Split
	if got := codes(Check(write(&damaged), &m)); !strings.Contains(got, "groups.class") {
		t.Errorf("a split of one cell to one: %s", got)
	}
}
