package spatial

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"sort"
	"testing"
)

// regular is a regular polygon of n sides about (cx, cy) with inradius r.
func regular(cx, cy, r float64, n int) [][][][2]float64 {
	R := r / math.Cos(math.Pi/float64(n))
	ring := make([][2]float64, n)
	for k := range ring {
		a := 2 * math.Pi * (float64(k) + 0.5) / float64(n)
		ring[k] = [2]float64{cx + R*math.Cos(a), cy + R*math.Sin(a)}
	}
	return [][][][2]float64{{ring}}
}

// convexBand is the area of a convex polygon (area A, perimeter P, inradius r, similar polygons
// inward) between signed distances a and b: outward, A + P d + π d²; inward, the polygon of
// inradius r − d, area A ((r − d)/r)².
func convexBand(A, P, r, a, b float64) float64 {
	within := func(s float64) float64 {
		if s >= 0 {
			return A + P*s + math.Pi*s*s
		}
		if -s >= r {
			return 0
		}
		f := (r + s) / r
		return A * f * f
	}
	return within(b) - within(a)
}

// Band areas by hand: a square and a 64-sided polygon (a near disk) in a drawn tissue, the square in
// a drawn tissue whose edges lie between the integration grid's lines, and a square in the tissue
// of a pixel raster; the same on one worker and on eight.
func TestBandAreasByHand(t *testing.T) {
	edges := []float64{-40, -30, -20, -10, 0, 10, 20, 30, 40}
	tissue := rectangleWindow(t, 0, 0, 1000, 1000)
	square := rectangleWindow(t, 450, 450, 550, 550)
	n := 64
	disk, err := PolygonWindow(regular(500, 500, 120, n))
	if err != nil {
		t.Fatal(err)
	}
	tan := math.Tan(math.Pi / float64(n))
	unaligned := rectangleWindow(t, 3.3, 1.7, 997.71, 999.13)
	// A pixel raster of 0.5 µm, its cells over 1,000 × 1,000 µm: the tissue window on a 1 µm grid.
	rows := make([][]uint32, 2000)
	for y := range rows {
		rows[y] = make([]uint32, 2000)
		for x := range rows[y] {
			rows[y][x] = 1
		}
	}
	raster, _, err := TissueWindow(context.Background(), raster(rows, 0.5), 1, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name         string
		tissue       *Window
		region       *Window
		A, P, inside float64
	}{
		{"a square", tissue, square, 10000, 400, 50},
		{"a 64-gon", tissue, disk, float64(n) * 120 * 120 * tan, 2 * float64(n) * 120 * tan, 120},
		{"a square in a tissue off the grid", unaligned, square, 10000, 400, 50},
		{"a square in the pixels' tissue", raster, square, 10000, 400, 50},
	} {
		got, err := BandAreas(context.Background(), c.tissue, c.region, edges, 8)
		if err != nil {
			t.Fatal(err)
		}
		one, err := BandAreas(context.Background(), c.tissue, c.region, edges, 1)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]float64, len(edges)+1)
		want[0] = convexBand(c.A, c.P, c.inside, -1e9, edges[0])
		for k := 1; k < len(edges); k++ {
			want[k] = convexBand(c.A, c.P, c.inside, edges[k-1], edges[k])
		}
		want[len(edges)] = c.tissue.Area - convexBand(c.A, c.P, c.inside, -1e9, edges[len(edges)-1])
		worst := 0.0
		for k := range want {
			if one[k] != got[k] {
				t.Errorf("%s: band %d differs on one worker", c.name, k)
			}
			worst = math.Max(worst, math.Abs(got[k]-want[k])/want[k])
		}
		t.Logf("%s: the bands' areas within %.2g of the exact ones", c.name, worst)
		if worst > 1e-5 {
			t.Errorf("%s: band areas %v, not %v", c.name, got, want)
		}
	}
}

// Region areas: two overlapping rectangles, one reaching past the tissue's edge, and the tissue in
// neither; in a drawn tissue and in a pixel raster's.
func TestRegionAreasByHand(t *testing.T) {
	tissue := rectangleWindow(t, 0, 0, 300, 200)
	a := rectangleWindow(t, 50, 50, 150, 150)
	b := rectangleWindow(t, 100, 100, 400, 180) // reaches past x = 300
	got, err := RegionAreas(context.Background(), tissue, []*Window{a, b}, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{10000, 200 * 80, 300*200 - 10000 - 200*80 + 50*50}
	for k := range want {
		if math.Abs(got[k]-want[k]) > 1e-6 {
			t.Errorf("area %d: %g, not %g", k, got[k], want[k])
		}
	}
	// A tissue with a hole: 300 × 200 µm of pixels less a 40 × 40 µm hole within a.
	rows := make([][]uint32, 400)
	for y := range rows {
		rows[y] = make([]uint32, 600)
		for x := range rows[y] {
			if !(x >= 160 && x < 240 && y >= 160 && y < 240) {
				rows[y][x] = 1
			}
		}
	}
	pix, _, err := TissueWindow(context.Background(), raster(rows, 0.5), 1, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	got, err = RegionAreas(context.Background(), pix, []*Window{a, b}, 4)
	if err != nil {
		t.Fatal(err)
	}
	// The hole lies within a, and over b's corner from (100, 100) to (120, 120).
	want = []float64{10000 - 1600, 200*80 - 400, 300*200 - 10000 - 200*80 + 50*50}
	for k := range want {
		if math.Abs(got[k]-want[k]) > 1e-6 {
			t.Errorf("with a hole, area %d: %g, not %g", k, got[k], want[k])
		}
	}
}

// Band and region areas, and the share of circles in a window (Ripley's edge correction), give the
// same bits on every processor: each product added is rounded on its own, never fused with the
// addition (as arm64's compiler may and amd64's does not), and the circles' angles are dmath's. The
// digest pinned is of the bands about a drawn region in a drawn tissue, the areas of two regions in
// it, and circles' shares at many places, the same on both; before, the bands' areas differed in
// their last digits between the two.
func TestAreasTheSameOnEveryProcessor(t *testing.T) {
	tissue, err := PolygonWindow([][][][2]float64{{{{12.3, 7.9}, {981.7, 31.1}, {1003.9, 877.3}, {611.3, 1011.7}, {23.9, 923.1}}}})
	if err != nil {
		t.Fatal(err)
	}
	nest, err := PolygonWindow([][][][2]float64{{{{600, 250}, {800, 260}, {790, 420}, {610, 400}}}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := PolygonWindow([][][][2]float64{{{{211.7, 533.1}, {397.3, 508.9}, {431.9, 701.3}, {187.1, 688.7}}, {{271.3, 589.9}, {347.7, 581.3}, {339.1, 641.9}}}})
	if err != nil {
		t.Fatal(err)
	}
	var edges []float64
	for k := -4; k <= 4; k++ {
		edges = append(edges, float64(k*10))
	}
	edges = append(edges, -10.5, 10.5)
	sort.Float64s(edges)
	bands, err := BandAreas(context.Background(), tissue, nest, edges, 4)
	if err != nil {
		t.Fatal(err)
	}
	regions, err := RegionAreas(context.Background(), tissue, []*Window{nest, other}, 4)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	put := func(v ...float64) {
		for _, x := range v {
			binary.Write(h, binary.LittleEndian, math.Float64bits(x))
		}
	}
	put(bands...)
	put(regions...)
	for i := 0; i < 400; i++ {
		x, y := 5+mul(float64(i%20), 51.37), 3+mul(float64(i/20), 50.93)
		for _, r := range []float64{7.5, 33.3, 101.1} {
			put(tissue.ArcInside(x, y, r), nest.ArcInside(x, y, r), other.ArcInside(x, y, r))
		}
	}
	if got := hex.EncodeToString(h.Sum(nil))[:16]; got != pinnedAreas {
		t.Fatalf("the areas have digest %s, pinned %s", got, pinnedAreas)
	}
}

const pinnedAreas = "f1b36cc568eacf15"
