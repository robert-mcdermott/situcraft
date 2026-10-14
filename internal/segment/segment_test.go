package segment

import (
	"math"
	"math/rand/v2"
	"testing"
)

// rasterOf makes a raster from rows of text: '.' is 0, digits are labels.
func rasterOf(rows ...string) *Raster {
	r := &Raster{Width: len(rows[0]), Height: len(rows), PixelSize: 0.5}
	for _, s := range rows {
		labels := make([]uint32, len(s))
		for i, c := range s {
			if c != '.' {
				labels[i] = uint32(c - '0')
			}
		}
		r.Rows = append(r.Rows, EncodeRow(labels))
	}
	return r
}

func TestRasterLookupsAndStats(t *testing.T) {
	r := rasterOf(
		"..11..",
		".1112.",
		"..222.",
	)
	if got := r.At(2, 1); got != 1 {
		t.Errorf("At(2,1) = %d", got)
	}
	if r.At(-1, 0) != 0 || r.At(6, 0) != 0 || r.At(4, 2) != 2 {
		t.Error("lookups at the edges")
	}
	// (1.0 µm, 0.75 µm) is pixel (2, 1).
	if got := r.AtMicrons(1.0, 0.75); got != 1 {
		t.Errorf("AtMicrons = %d", got)
	}
	s := r.Stats(2)
	if s[0].Pixels != 5 || s[1].Pixels != 4 {
		t.Fatalf("pixels %d %d", s[0].Pixels, s[1].Pixels)
	}
	// Label 1's pixel centers: (2.5,0.5) (3.5,0.5) (1.5,1.5) (2.5,1.5) (3.5,1.5).
	if s[0].SumX != 13.5 || s[0].SumY != 5.5 || s[0].MinX != 1 || s[0].MaxX != 3 || s[0].MinY != 0 || s[0].MaxY != 1 {
		t.Errorf("label 1: %+v", s[0])
	}
}

func TestOutlines(t *testing.T) {
	cases := []struct {
		name                    string
		rows                    []string
		vertices, pieces, holes int
		pixels                  int
	}{
		{"a rectangle", []string{"....", ".11.", ".11.", "...."}, 4, 1, 0, 4},
		{"an L", []string{"1..", "1..", "111"}, 6, 1, 0, 5},
		{"a ring", []string{"111", "1.1", "111"}, 4, 1, 1, 8},
		{"two pieces, the larger traced", []string{"11.1", "11..", "...."}, 4, 2, 0, 4},
		{"touching only at a corner", []string{"1.", ".1"}, 4, 2, 0, 1},
	}
	for _, c := range cases {
		r := rasterOf(c.rows...)
		s := r.Stats(1)
		o := r.TraceOutline(1, s[0], 0)
		if len(o.X) != c.vertices || o.Pieces != c.pieces || o.Holes != c.holes || o.Pixels != c.pixels {
			t.Errorf("%s: %d vertices, %d pieces, %d holes, %d pixels", c.name, len(o.X), o.Pieces, o.Holes, o.Pixels)
		}
		// The unsimplified ring encloses the piece and its holes exactly.
		want := float64(c.pixels+c.holes) * r.PixelSize * r.PixelSize
		if c.name == "a ring" {
			want = 9 * r.PixelSize * r.PixelSize
		}
		if got := RingArea(o.X, o.Y); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: ring area %v, want %v", c.name, got, want)
		}
	}
}

// Random blobs: the traced ring (unsimplified) encloses exactly the traced piece's pixels and its
// holes', which a point-in-polygon test of every pixel centre confirms; simplification keeps every
// vertex within its tolerance of the original ring.
func TestOutlinesOfRandomBlobs(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	for trial := 0; trial < 300; trial++ {
		w, h := 3+rng.IntN(14), 3+rng.IntN(14)
		labels := make([]uint32, w*h)
		// A random walk of filled pixels, then random noise.
		x, y := rng.IntN(w), rng.IntN(h)
		for k := 0; k < w*h; k++ {
			labels[y*w+x] = 1
			switch rng.IntN(4) {
			case 0:
				x = min(w-1, x+1)
			case 1:
				x = max(0, x-1)
			case 2:
				y = min(h-1, y+1)
			default:
				y = max(0, y-1)
			}
		}
		r := &Raster{Width: w, Height: h, PixelSize: 1}
		for yy := 0; yy < h; yy++ {
			r.Rows = append(r.Rows, EncodeRow(labels[yy*w:(yy+1)*w]))
		}
		s := r.Stats(1)
		o := r.TraceOutline(1, s[0], 0)
		enclosed := 0
		for yy := 0; yy < h; yy++ {
			for xx := 0; xx < w; xx++ {
				if pointIn(float64(xx)+0.5, float64(yy)+0.5, o.X, o.Y) {
					enclosed++
				}
			}
		}
		if got := RingArea(o.X, o.Y); math.Abs(got-float64(enclosed)) > 1e-9 {
			t.Fatalf("trial %d: ring area %v, %d pixel centres inside", trial, got, enclosed)
		}
		if enclosed < o.Pixels {
			t.Fatalf("trial %d: the ring encloses %d pixels, fewer than its piece's %d", trial, enclosed, o.Pixels)
		}
		simple := r.TraceOutline(1, s[0], 0.75)
		if len(simple.X) < 3 || len(simple.X) > len(o.X) {
			t.Fatalf("trial %d: %d vertices simplified from %d", trial, len(simple.X), len(o.X))
		}
		for i := range o.X {
			if d := ringDistance(float64(o.X[i]), float64(o.Y[i]), simple.X, simple.Y); d > 0.75+1e-9 {
				t.Fatalf("trial %d: a vertex is %v from the simplified ring", trial, d)
			}
		}
	}
}

func pointIn(px, py float64, xs, ys []float32) bool {
	in := false
	for i, j := 0, len(xs)-1; i < len(xs); j, i = i, i+1 {
		xi, yi, xj, yj := float64(xs[i]), float64(ys[i]), float64(xs[j]), float64(ys[j])
		if (yi > py) != (yj > py) && px < (xj-xi)*(py-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

func ringDistance(px, py float64, xs, ys []float32) float64 {
	best := math.Inf(1)
	for i, j := 0, len(xs)-1; i < len(xs); j, i = i, i+1 {
		best = math.Min(best, segmentDistance(px, py, float64(xs[j]), float64(ys[j]), float64(xs[i]), float64(ys[i])))
	}
	return best
}
