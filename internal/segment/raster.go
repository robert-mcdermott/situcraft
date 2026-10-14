// Package segment holds what SituCraft's segmentations are made of: a label raster (each pixel the
// cell it belongs to, run-length encoded by row), the cells' outlines traced from it, the
// transcripts' assignments to cells, and the cell-by-gene counts, with the artifact files that
// keep them (format.go) and the check a segmentation passes before it can be used (check.go). The
// methods that make segmentations live in its subpackages (expand: nucleus expansion).
package segment

import (
	"math"
	"sort"
)

// Run is a stretch of a row's pixels with one label (0: no cell).
type Run struct {
	Label  uint32
	Length uint32
}

// Raster is a label image of Width × Height pixels of PixelSize µm, row by row as runs that cover
// each row exactly. Label k > 0 is cell k-1 of the segmentation.
type Raster struct {
	Width, Height int
	PixelSize     float64
	Rows          [][]Run
	starts        [][]uint32 // each run's first column, for lookups
}

// EncodeRow turns a row of labels into runs.
func EncodeRow(labels []uint32) []Run {
	var runs []Run
	for x := 0; x < len(labels); {
		l := labels[x]
		n := 1
		for x+n < len(labels) && labels[x+n] == l {
			n++
		}
		runs = append(runs, Run{l, uint32(n)})
		x += n
	}
	return runs
}

func (r *Raster) index() {
	if r.starts != nil {
		return
	}
	r.starts = make([][]uint32, len(r.Rows))
	for y, row := range r.Rows {
		s := make([]uint32, len(row))
		var x uint32
		for i, run := range row {
			s[i] = x
			x += run.Length
		}
		r.starts[y] = s
	}
}

// At is the label of pixel (x, y); 0 outside the raster.
func (r *Raster) At(x, y int) uint32 {
	if x < 0 || y < 0 || x >= r.Width || y >= r.Height {
		return 0
	}
	r.index()
	s := r.starts[y]
	i := sort.Search(len(s), func(i int) bool { return s[i] > uint32(x) }) - 1
	if i < 0 {
		return 0
	}
	return r.Rows[y][i].Label
}

// AtMicrons is the label of the pixel holding the point (x, y) µm: pixel ⌊x / PixelSize⌋,
// ⌊y / PixelSize⌋, as the instrument's masks assign transcripts.
func (r *Raster) AtMicrons(x, y float64) uint32 {
	return r.At(int(math.Floor(x/r.PixelSize)), int(math.Floor(y/r.PixelSize)))
}

// Row decodes row y into labels (len Width).
func (r *Raster) Row(y int, labels []uint32) {
	x := 0
	for _, run := range r.Rows[y] {
		for k := uint32(0); k < run.Length; k++ {
			labels[x] = run.Label
			x++
		}
	}
}

// RowRange decodes pixels x0..x1-1 of row y into dst (len x1-x0).
func (r *Raster) RowRange(y, x0, x1 int, dst []uint32) {
	r.index()
	s := r.starts[y]
	i := max(0, sort.Search(len(s), func(i int) bool { return s[i] > uint32(x0) })-1)
	row := r.Rows[y]
	for x := x0; x < x1; {
		for i+1 < len(row) && s[i+1] <= uint32(x) {
			i++
		}
		end := min(x1, int(s[i]+row[i].Length))
		for ; x < end; x++ {
			dst[x-x0] = row[i].Label
		}
	}
}

// LabelStats is what a raster says of one label: its pixels, their sums (for the centroid) and
// bounding box (pixels, inclusive).
type LabelStats struct {
	Pixels     int64
	SumX, SumY float64
	MinX, MinY int
	MaxX, MaxY int
}

// Stats summarizes labels 1..n.
func (r *Raster) Stats(n int) []LabelStats {
	out := make([]LabelStats, n)
	for i := range out {
		out[i].MinX, out[i].MinY = math.MaxInt, math.MaxInt
		out[i].MaxX, out[i].MaxY = -1, -1
	}
	for y, row := range r.Rows {
		x := 0
		for _, run := range row {
			if run.Label > 0 && int(run.Label) <= n {
				s := &out[run.Label-1]
				l := int(run.Length)
				s.Pixels += int64(l)
				// The sum of the pixel centers' x over the run: l·x + l(l−1)/2 + l/2.
				s.SumX += float64(l)*float64(x) + float64(l)*float64(l)/2
				s.SumY += float64(l) * (float64(y) + 0.5)
				s.MinX, s.MaxX = min(s.MinX, x), max(s.MaxX, x+l-1)
				s.MinY, s.MaxY = min(s.MinY, y), max(s.MaxY, y)
			}
			x += int(run.Length)
		}
	}
	return out
}

func sortFloats(v []float64) { sort.Float64s(v) }
