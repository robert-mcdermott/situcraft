package segment

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
)

// written writes a small segmentation that passes the check: two cells, five transcripts.
func written(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r := rasterOf(
		"..11....",
		".1111.2.",
		".111.222",
		"..1..22.",
	)
	stats := r.Stats(2)
	px2 := r.PixelSize * r.PixelSize
	cells := make([]Cell, 2)
	outlines := make([]Outline, 2)
	for i := range cells {
		s := stats[i]
		cells[i] = Cell{ID: []string{"c1", "c2"}[i], Nucleus: []string{"n1", "n2"}[i],
			X: float32(s.SumX / float64(s.Pixels) * r.PixelSize), Y: float32(s.SumY / float64(s.Pixels) * r.PixelSize),
			Area: float32(float64(s.Pixels) * px2), NucleusArea: 0.5}
		outlines[i] = r.TraceOutline(uint32(i+1), s, 0.5)
	}
	assignments := []uint32{1, 0, 2, 1, 1}
	cells[0].Assigned, cells[1].Assigned = 3, 1
	counts := &Counts{Genes: []string{"ACTA2", "EPCAM"}, Offsets: []uint32{0, 2, 3}, Gene: []uint32{0, 1, 1}, Count: []uint32{1, 1, 1}}
	cells[0].Transcripts, cells[0].Genes = 2, 2
	cells[1].Transcripts, cells[1].Genes = 1, 1
	d := Description{Method: "test 1", Parameters: json.RawMessage(`{"distance_um":1}`), Width: r.Width, Height: r.Height, PixelSize: r.PixelSize,
		Cells: 2, Transcripts: 5, Genes: 2, Q20GeneTranscripts: 4, Q20GeneAssigned: 3}
	d.Nuclei.Source, d.Nuclei.Count = "a test", 2
	for _, err := range []error{WriteCells(dir, cells), WriteMask(dir, r), WriteAssignments(dir, assignments), WriteCounts(dir, counts), WriteOutlines(dir, outlines), WriteDescription(dir, d)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func fatalCodes(f []artifacts.Finding) []string {
	var out []string
	for _, x := range f {
		if x.Severity == "fatal" {
			out = append(out, x.Code)
		}
	}
	return out
}

func TestFilesReadBackAsWritten(t *testing.T) {
	dir := written(t)
	var m artifacts.Manifest
	if f := Check(dir, &m); len(fatalCodes(f)) > 0 {
		t.Fatalf("a valid segmentation fails the check: %+v", f)
	}
	var s Summary
	if err := json.Unmarshal(m.Summary, &s); err != nil {
		t.Fatal(err)
	}
	if s.Cells != 2 || s.AssignedFraction != 0.75 || s.MedianTranscripts != 1.5 || s.Method != "test 1" {
		t.Errorf("summary %+v", s)
	}
	cells, err := ReadCells(dir)
	if err != nil || len(cells) != 2 || cells[1].ID != "c2" || cells[1].Nucleus != "n2" || cells[0].Assigned != 3 || cells[0].Genes != 2 {
		t.Errorf("cells %+v, %v", cells, err)
	}
	mask, err := ReadMask(dir)
	if err != nil || mask.At(6, 2) != 2 || mask.At(0, 0) != 0 || mask.PixelSize != 0.5 {
		t.Errorf("mask %+v, %v", mask, err)
	}
	a, err := ReadAssignments(dir)
	if err != nil || !reflect.DeepEqual(a, []uint32{1, 0, 2, 1, 1}) {
		t.Errorf("assignments %v, %v", a, err)
	}
	c, err := ReadCounts(dir)
	if err != nil || !reflect.DeepEqual(c.Genes, []string{"ACTA2", "EPCAM"}) || !reflect.DeepEqual(c.Offsets, []uint32{0, 2, 3}) {
		t.Errorf("counts %+v, %v", c, err)
	}
	o, err := ReadOutlines(dir)
	if err != nil || len(o) != 2 || len(o[0].X) < 3 || o[1].Pixels != 6 {
		t.Errorf("outlines %+v, %v", o, err)
	}
	// The boundary certainty, optional: one per cell, NaN for a cell not drawn by the method.
	if err := WriteCertainty(dir, []float32{0.875, float32(math.NaN())}); err != nil {
		t.Fatal(err)
	}
	if f := Check(dir, &m); len(fatalCodes(f)) > 0 {
		t.Fatalf("a valid boundary certainty fails the check: %+v", f)
	}
	if b, err := ReadCertainty(dir); err != nil || len(b) != 2 || b[0] != 0.875 || !math.IsNaN(float64(b[1])) {
		t.Errorf("certainty %v, %v", b, err)
	}
}

// Each kind of damage is found, and named.
func TestTheCheckFindsDamage(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, dir string)
		want   string
	}{
		{"a truncated file", func(t *testing.T, dir string) {
			raw, _ := os.ReadFile(filepath.Join(dir, FileMask))
			os.WriteFile(filepath.Join(dir, FileMask), raw[:len(raw)-6], 0o644)
		}, "segmentation.unreadable"},
		{"another file's magic", func(t *testing.T, dir string) {
			raw, _ := os.ReadFile(filepath.Join(dir, FileCounts))
			os.WriteFile(filepath.Join(dir, FileAssignments), raw, 0o644)
		}, "segmentation.unreadable"},
		{"a body claiming more than its data can hold", func(t *testing.T, dir string) {
			raw, _ := os.ReadFile(filepath.Join(dir, FileCells))
			binary.LittleEndian.PutUint64(raw[8:], 1<<40)
			os.WriteFile(filepath.Join(dir, FileCells), raw, 0o644)
		}, "segmentation.unreadable"},
		{"a cell's area", func(t *testing.T, dir string) {
			cells, _ := ReadCells(dir)
			cells[0].Area *= 1.5
			WriteCells(dir, cells)
		}, "geometry.area"},
		{"a cell's centroid", func(t *testing.T, dir string) {
			cells, _ := ReadCells(dir)
			cells[1].X += 1
			WriteCells(dir, cells)
		}, "geometry.centroid"},
		{"a transcript in a cell that does not exist", func(t *testing.T, dir string) {
			WriteAssignments(dir, []uint32{1, 0, 2, 1, 3})
		}, "assignments.unknown-cell"},
		{"a cell's assigned count", func(t *testing.T, dir string) {
			WriteAssignments(dir, []uint32{1, 0, 2, 2, 1})
		}, "assignments.counts"},
		{"a row of counts", func(t *testing.T, dir string) {
			c, _ := ReadCounts(dir)
			c.Count[0] = 4
			WriteCounts(dir, c)
		}, "counts.cells"},
		{"the description's sizes", func(t *testing.T, dir string) {
			d, _ := ReadDescription(dir)
			d.Transcripts = 6
			WriteDescription(dir, *d)
		}, "segmentation.metadata"},
		{"the description's total", func(t *testing.T, dir string) {
			d, _ := ReadDescription(dir)
			d.Q20GeneAssigned = 4
			WriteDescription(dir, *d)
		}, "counts.total"},
		{"a boundary certainty for each cell", func(t *testing.T, dir string) {
			WriteCertainty(dir, []float32{0.9})
		}, "certainty.count"},
		{"a boundary certainty above 1", func(t *testing.T, dir string) {
			WriteCertainty(dir, []float32{0.9, 1.5})
		}, "certainty.range"},
		{"an outline outside the raster", func(t *testing.T, dir string) {
			o, _ := ReadOutlines(dir)
			o[0].X[0] = 100
			WriteOutlines(dir, o)
		}, "geometry.outline"},
	}
	for _, c := range cases {
		dir := written(t)
		c.damage(t, dir)
		var m artifacts.Manifest
		got := fatalCodes(Check(dir, &m))
		if !strings.Contains(","+strings.Join(got, ",")+",", ","+c.want+",") {
			t.Errorf("%s: findings %v, want %s", c.name, got, c.want)
		}
	}
}

func TestRowRangeIsRowsSlice(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for trial := 0; trial < 200; trial++ {
		w, h := 1+rng.IntN(40), 1+rng.IntN(5)
		r := &Raster{Width: w, Height: h, PixelSize: 1}
		for y := 0; y < h; y++ {
			row := make([]uint32, w)
			for x := range row {
				if x > 0 && rng.IntN(3) > 0 {
					row[x] = row[x-1]
				} else {
					row[x] = uint32(rng.IntN(4))
				}
			}
			r.Rows = append(r.Rows, EncodeRow(row))
		}
		full := make([]uint32, w)
		for y := 0; y < h; y++ {
			r.Row(y, full)
			x0 := rng.IntN(w)
			x1 := x0 + 1 + rng.IntN(w-x0)
			got := make([]uint32, x1-x0)
			r.RowRange(y, x0, x1, got)
			if !reflect.DeepEqual(got, full[x0:x1]) {
				t.Fatalf("row %v, range %d..%d: %v", full, x0, x1, got)
			}
		}
	}
}

// A column streamed a block at a time reads what was written, and a damaged one fails.
func TestColumnsStreamAsWritten(t *testing.T) {
	dir := t.TempDir()
	labels := make([]uint32, 100_003)
	prob := make([]float32, len(labels))
	for i := range labels {
		labels[i] = uint32(i*7919) % 1000
		prob[i] = float32(i%97) / 97
	}
	prob[5] = float32(math.NaN())
	if err := WriteAssignments(dir, labels); err != nil {
		t.Fatal(err)
	}
	if err := WriteProbabilities(dir, prob); err != nil {
		t.Fatal(err)
	}
	for _, block := range []int{1, 4096, 1 << 20} {
		a, err := OpenColumn(dir, FileAssignments)
		if err != nil {
			t.Fatal(err)
		}
		p, err := OpenColumn(dir, FileProbabilities)
		if err != nil {
			t.Fatal(err)
		}
		if a.Len() != len(labels) || p.Len() != len(prob) {
			t.Fatalf("lengths %d, %d", a.Len(), p.Len())
		}
		bufA, bufP := make([]uint32, block), make([]uint32, block)
		at := 0
		for {
			k, err := a.Next(bufA)
			if err != nil {
				t.Fatal(err)
			}
			if j, err := p.Next(bufP); err != nil || j != k {
				t.Fatalf("probabilities: %d values, %v", j, err)
			}
			if k == 0 {
				break
			}
			for i := range k {
				if bufA[i] != labels[at+i] || bufP[i] != math.Float32bits(prob[at+i]) {
					t.Fatalf("block %d: value %d differs", block, at+i)
				}
			}
			at += k
		}
		if at != len(labels) {
			t.Errorf("block %d: %d values read", block, at)
		}
		a.Close()
		p.Close()
	}
	if _, err := OpenColumn(dir, FileCells); err == nil {
		t.Error("cells.bin opened as a column")
	}
	// Cut short, the stream fails before its end.
	raw, _ := os.ReadFile(filepath.Join(dir, FileAssignments))
	os.WriteFile(filepath.Join(dir, FileAssignments), raw[:len(raw)/2], 0o644)
	c, err := OpenColumn(dir, FileAssignments)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	buf := make([]uint32, 1<<16)
	for {
		k, err := c.Next(buf)
		if err != nil {
			break
		}
		if k == 0 {
			t.Fatal("a file cut short read to its end")
		}
	}
}
