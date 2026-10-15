package plant

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/compare"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// tissue is a 16 × 16 grid of square cells, 12 pixels a side, of two kinds in a checkerboard:
// one expresses EPCAM and KRT8, the other PTPRC and CD3E, 60 gene transcripts each.
type tissue struct {
	ids        []uint64
	x, y, qv   []float32
	gene       []string
	cellOf     []uint32
	w, h, side int
	size       float64
}

func (t *tissue) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	b := &xenium.TranscriptBatch{Rows: len(t.x), X: t.x, Y: t.y, QV: t.qv, TranscriptID: t.ids, Z: make([]float32, len(t.x))}
	for _, g := range t.gene {
		b.Feature = append(b.Feature, []byte(g))
	}
	return fn(b)
}

func makeTissue(t *testing.T) (*tissue, string) {
	const grid, side = 16, 12
	ts := &tissue{w: grid * side, h: grid * side, side: side, size: 0.5}
	rng := rand.New(rand.NewPCG(1, 2))
	r := &segment.Raster{Width: ts.w, Height: ts.h, PixelSize: ts.size, Rows: make([][]segment.Run, ts.h)}
	row := make([]uint32, ts.w)
	for y := 0; y < ts.h; y++ {
		for x := 0; x < ts.w; x++ {
			row[x] = uint32((y/side)*grid + x/side + 1)
		}
		r.Rows[y] = segment.EncodeRow(row)
	}
	n := grid * grid
	cells := make([]segment.Cell, n)
	kinds := [2][2]string{{"EPCAM", "KRT8"}, {"PTPRC", "CD3E"}}
	genes := []string{"EPCAM", "KRT8", "PTPRC", "CD3E"}
	for i := range cells {
		cx, cy := i%grid, i/grid
		cells[i] = segment.Cell{ID: fmt.Sprintf("cell-%d", i+1), Nucleus: fmt.Sprintf("n%d", i+1),
			X: float32((float64(cx*side) + side/2.0) * ts.size), Y: float32((float64(cy*side) + side/2.0) * ts.size), Area: float32(side * side * ts.size * ts.size)}
		kind := kinds[(cx+cy)%2]
		for k := 0; k < 60; k++ {
			ts.ids = append(ts.ids, uint64(len(ts.ids)+1))
			ts.x = append(ts.x, float32((float64(cx*side)+0.5+rng.Float64()*(side-1))*ts.size))
			ts.y = append(ts.y, float32((float64(cy*side)+0.5+rng.Float64()*(side-1))*ts.size))
			ts.qv = append(ts.qv, 30)
			ts.gene = append(ts.gene, kind[k%2])
			ts.cellOf = append(ts.cellOf, uint32(i+1))
		}
	}
	counts := &segment.Counts{Genes: genes, Offsets: make([]uint32, n+1)}
	for i := range cells {
		kind := kinds[(i%grid+i/grid)%2]
		for gi, g := range genes {
			if g == kind[0] || g == kind[1] {
				counts.Gene = append(counts.Gene, uint32(gi))
				counts.Count = append(counts.Count, 30)
			}
		}
		counts.Offsets[i+1] = uint32(len(counts.Gene))
		cells[i].Transcripts, cells[i].Assigned, cells[i].Genes = 60, 60, 2
	}
	stats := r.Stats(n)
	outlines := make([]segment.Outline, n)
	for i := range outlines {
		outlines[i] = r.TraceOutline(uint32(i+1), stats[i], 0.5)
	}
	dir := t.TempDir()
	d := segment.Description{Method: "test grid", Width: ts.w, Height: ts.h, PixelSize: ts.size, Cells: n, Transcripts: len(ts.x), Genes: len(genes)}
	for _, err := range []error{segment.WriteCells(dir, cells), segment.WriteMask(dir, r), segment.WriteAssignments(dir, ts.cellOf),
		segment.WriteCounts(dir, counts), segment.WriteOutlines(dir, outlines), segment.WriteDescription(dir, d)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return ts, dir
}

// Every planted error is found by the comparison as what it is, and nothing else differs; the
// planted segmentation passes the segmentation check; the same seed plants the same errors.
func TestPlantedErrorsAreFound(t *testing.T) {
	ts, src := makeTissue(t)
	p := Params{Merges: 4, Splits: 4, Missed: 3, Grown: 3, GrowUm: 1.5, Seed: 7}
	dir := t.TempDir()
	_, truth, err := Plant(context.Background(), src, "test grid", ts, dir, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range truth.Errors {
		kinds[e.Kind]++
	}
	if kinds[Merge] != 4 || kinds[Split] != 4 || kinds[Missed] != 3 || kinds[Grown] != 3 {
		t.Fatalf("planted %v", kinds)
	}
	var m artifacts.Manifest
	for _, f := range segment.Check(dir, &m) {
		if f.Severity == "fatal" {
			t.Fatalf("the planted segmentation fails its check: %s", f.Message)
		}
	}
	// The same seed, the same errors; another seed, others.
	_, again, err := Plant(context.Background(), src, "test grid", ts, t.TempDir(), p, nil)
	if err != nil || fmt.Sprint(again.Errors) != fmt.Sprint(truth.Errors) {
		t.Fatal("the same seed planted other errors")
	}
	p.Seed = 8
	if _, other, _ := Plant(context.Background(), src, "test grid", ts, t.TempDir(), p, nil); fmt.Sprint(other.Errors) == fmt.Sprint(truth.Errors) {
		t.Fatal("another seed planted the same errors")
	}

	a, err := compare.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compare.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr := &compare.Transcripts{X: ts.x, Y: ts.y, Q20Gene: make([]bool, len(ts.x))}
	for i := range tr.Q20Gene {
		tr.Q20Gene[i] = true
	}
	res, err := compare.Compare(context.Background(), compare.Inputs{A: a, B: b, Transcripts: tr, Pairs: [][2]string{{"EPCAM", "PTPRC"}}})
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]int32{}
	for i, c := range a.Cells {
		index[c.ID] = int32(i)
	}
	groupOf := map[int32]compare.Group{}
	for _, g := range res.Groups {
		for _, c := range g.A {
			groupOf[c] = g
		}
	}
	planted := map[int32]bool{}
	for _, e := range truth.Errors {
		g := groupOf[index[e.Cells[0]]]
		for _, c := range e.Cells {
			planted[index[c]] = true
		}
		want := map[string]string{Merge: compare.Merged, Split: compare.Split, Missed: compare.OnlyA, Grown: compare.Expanded}[e.Kind]
		if g.Class != want {
			t.Errorf("%s of %v (into %v) is a %s difference, not %s", e.Kind, e.Cells, e.Into, g.Class, want)
		}
		if e.Kind == Merge && (len(g.A) != 2 || g.MixedB == 0) {
			t.Errorf("the merge of %v: %d cells, %d mixed marker pairs", e.Cells, len(g.A), g.MixedB)
		}
		if e.Kind == Grown && len(e.Cells) < 2 {
			t.Errorf("the grown %s took no neighbour's pixels", e.Cells[0])
		}
	}
	for _, g := range res.Groups {
		if len(g.A) == 1 && !planted[g.A[0]] && g.Class != compare.OneToOne {
			t.Errorf("cell %s, unplanted, is a %s difference", a.Cells[g.A[0]].ID, g.Class)
		}
	}
	if res.Classes[compare.OneToOne] < 256-4*2-4-3-3*5 {
		t.Errorf("only %d cells one to one", res.Classes[compare.OneToOne])
	}
	if _, err := ReadTruth(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileTruth)); err != nil {
		t.Fatal(err)
	}
}
