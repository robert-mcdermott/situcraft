package spatial

import (
	"context"
	"math"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

// bruteSigned is the signed distance to a polygon's rings by every segment (inside by the even–odd
// rule), written apart from the windows' search.
func bruteSigned(rings [][][2]float64, x, y float64) float64 {
	best := math.Inf(1)
	inside := false
	for _, ring := range rings {
		for i, p := range ring {
			q := ring[(i+1)%len(ring)]
			best = math.Min(best, math.Sqrt(segmentDistance2(x, y, p[0], p[1], q[0], q[1])))
			if (p[1] <= y) != (q[1] <= y) && p[0]+(y-p[1])/(q[1]-p[1])*(q[0]-p[0]) > x {
				inside = !inside
			}
		}
	}
	if inside {
		return -best
	}
	return best
}

// Cells across the edge of a 64-sided near disk in a square tissue, labeled by rule: one label
// everywhere, one only inside, one only within 10 µm of the edge (none in the core or around, so
// neither denser nor sparser inside), one only outside. Each band's
// counts are those of the cells' distances found by every segment; the readings are the rules';
// the same on one worker and on eight.
func TestProfilesPlacedByRule(t *testing.T) {
	poly := regular(500, 500, 200, 64)
	region, err := PolygonWindow(poly)
	if err != nil {
		t.Fatal(err)
	}
	tissue := rectangleWindow(t, 0, 0, 1000, 1000)
	s := random.New(61, 1)
	var x, y []float32
	var label []int32
	var dist []float64
	for len(x) < 40000 {
		px, py := float32(1000*s.Float64()), float32(1000*s.Float64())
		d := bruteSigned(poly[0], float64(px), float64(py))
		l := int32(s.IntN(4))
		ok := l == 0 || l == 1 && d < 0 || l == 2 && math.Abs(d) < 10 || l == 3 && d > 0
		if !ok {
			continue
		}
		x, y, label, dist = append(x, px), append(y, py), append(label, l), append(dist, d)
	}
	p := DefaultBoundary()
	b, err := AcrossEdge(context.Background(), x, y, label, 4, tissue, region, p, 8)
	if err != nil {
		t.Fatal(err)
	}
	one, err := AcrossEdge(context.Background(), x, y, label, 4, tissue, region, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	for q := range b.Density {
		if b.Density[q] != one.Density[q] || b.Area[q/4] != one.Area[q/4] {
			t.Fatal("the profile differs on one worker")
		}
	}
	counts := make([]int, len(b.Count))
	for i, d := range dist {
		counts[bandOf(b.Edges, d)*4+int(label[i])]++
	}
	for q := range counts {
		if counts[q] != b.Count[q] {
			t.Fatalf("band %d, label %d: %d cells, by every segment %d", q/4, q%4, b.Count[q], counts[q])
		}
	}
	want := []struct {
		inside   int
		atMargin bool
	}{{0, false}, {1, false}, {0, true}, {-1, false}}
	for k, w := range want {
		if b.Inside[k] != w.inside || b.AtMargin[k] != w.atMargin {
			t.Errorf("label %d: inside %d, at the margin %v; not %d, %v (ratios %v)", k, b.Inside[k], b.AtMargin[k], w.inside, w.atMargin, b.Ratio[k*Comparisons:(k+1)*Comparisons])
		}
	}
	// The label placed everywhere: its density in each band within its interval of the whole's.
	total := 0
	for band := range b.Area {
		total += b.Count[band*4]
	}
	lambda := float64(total) / 1e6 * 1e6
	for band := 1; band < len(b.Area)-1; band++ {
		if d, lo, hi := b.Density[band*4], b.DensityLo[band*4], b.DensityHi[band*4]; !(lo <= lambda*1.15 && hi >= lambda*0.85) {
			t.Errorf("band %d: the label everywhere at %.0f (%.0f to %.0f) a mm², the whole %.0f", band, d, lo, hi, lambda)
		}
	}
}

// Cells placed uniformly, labeled at random: their densities in each band and each region alike,
// the intervals holding the true density about as often as they say.
func TestUniformCellsGiveFlatDensities(t *testing.T) {
	tissue := rectangleWindow(t, 0, 0, 800, 800)
	region, err := PolygonWindow(regular(400, 400, 150, 32))
	if err != nil {
		t.Fatal(err)
	}
	other := rectangleWindow(t, 50, 50, 250, 300)
	s := random.New(71, 2)
	const lambda = 2000.0 // a mm², of each of two labels
	held, all := 0, 0
	heldRegions, allRegions := 0, 0
	for rep := 0; rep < 15; rep++ {
		n := s.Poisson(2 * lambda * 0.64)
		x, y, label := make([]float32, n), make([]float32, n), make([]int32, n)
		for i := range x {
			x[i], y[i], label[i] = float32(800*s.Float64()), float32(800*s.Float64()), int32(s.IntN(2))
		}
		b, err := AcrossEdge(context.Background(), x, y, label, 2, tissue, region, DefaultBoundary(), 4)
		if err != nil {
			t.Fatal(err)
		}
		for q := range b.Density {
			if b.Area[q/2] > 0 {
				all++
				if b.DensityLo[q] <= lambda && lambda <= b.DensityHi[q] {
					held++
				}
			}
		}
		c, err := Compose(context.Background(), x, y, label, 2, tissue, []*Window{region, other}, DefaultRegions(), 4)
		if err != nil {
			t.Fatal(err)
		}
		for q := range c.Density {
			allRegions++
			if c.DensityLo[q] <= lambda && lambda <= c.DensityHi[q] {
				heldRegions++
			}
		}
	}
	if f := float64(held) / float64(all); f < 0.93 {
		t.Errorf("the bands' intervals held the density %.3f of the time", f)
	}
	if f := float64(heldRegions) / float64(allRegions); f < 0.9 {
		t.Errorf("the regions' intervals held the density %.3f of the time", f)
	}
}

// Regions by hand: cells on a grid in a tissue of 300 × 200 µm, two overlapping regions, one
// reaching past the tissue; counts, densities, shares, membership and a ratio worked out.
func TestComposeByHand(t *testing.T) {
	tissue := rectangleWindow(t, 0, 0, 300, 200)
	a := rectangleWindow(t, 50, 50, 150, 150)
	b := rectangleWindow(t, 100, 100, 400, 180)
	var x, y []float32
	var label []int32
	// A cell every 10 µm from 5 to 295 and 5 to 195; label 1 where x < 100, else 0; and one cell
	// beyond the tissue.
	for gy := 5; gy < 200; gy += 10 {
		for gx := 5; gx < 300; gx += 10 {
			x, y = append(x, float32(gx)), append(y, float32(gy))
			label = append(label, int32(btoi(gx < 100)))
		}
	}
	x, y, label = append(x, 350), append(y, 150), append(label, 0)
	c, err := Compose(context.Background(), x, y, label, 2, tissue, []*Window{a, b}, DefaultRegions(), 4)
	if err != nil {
		t.Fatal(err)
	}
	// a: x 55..145, y 55..145: 10 × 10 cells, label 1 for x 55..95 (5 columns).
	// b: x 105..295, y 105..175: 20 × 8 cells, all label 0.
	// elsewhere: 30 × 20 − (cells in a or b): a ∩ b holds x 105..145, y 105..145: 5 × 5.
	wantCount := []int{50, 50, 160, 0, 600 - (100 + 160 - 25) - 100 + 100, 0}
	inNone0, inNone1 := 0, 0
	for i := range x[:len(x)-1] {
		px, py := x[i], y[i]
		inA := px > 50 && px < 150 && py > 50 && py < 150
		inB := px > 100 && py > 100 && py < 180
		if !inA && !inB {
			if label[i] == 0 {
				inNone0++
			} else {
				inNone1++
			}
		}
	}
	wantCount[4], wantCount[5] = inNone0, inNone1
	for q, w := range wantCount {
		if c.Count[q] != w {
			t.Errorf("row %d, label %d: %d cells, not %d", q/2, q%2, c.Count[q], w)
		}
	}
	if c.Outside != 1 || c.Membership[len(x)-1] != OutsideTissue {
		t.Errorf("%d cells outside the tissue", c.Outside)
	}
	if got, want := c.Density[0], 50/(10000/1e6); math.Abs(got-want) > 1e-9 {
		t.Errorf("a's density of label 0: %g, not %g", got, want)
	}
	if got := c.Share[2*0+1]; got != 0.5 {
		t.Errorf("a's share of label 1: %g", got)
	}
	r, lo, hi := stats.RateRatio(50, 10000, 160, 16000, 0.95)
	q := (0*c.R+1)*2 + 0
	if c.DensityRatio[q] != r || c.DensityRatioLo[q] != lo || c.DensityRatioHi[q] != hi {
		t.Errorf("a over b, label 0: %g (%g to %g), not %g (%g to %g)", c.DensityRatio[q], c.DensityRatioLo[q], c.DensityRatioHi[q], r, lo, hi)
	}
}
