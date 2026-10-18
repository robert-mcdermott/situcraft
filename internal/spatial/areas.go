package spatial

// Areas within the tissue: of regions, and of bands of distance from a region's boundary, the
// denominators of densities. The tissue window is cut into squares, each split in four until what
// is measured is the same over all of it (inside or outside each region; within one band), its
// area then added whole. Whether a square is wholly within a region or a band is known from the
// distance at its center: the signed distance to a boundary changes by at most the distance moved,
// so a square whose half-diagonal is less than the distance to the nearest edge of a band lies
// within it. A square still crossed by an edge at the finest size (a sixteenth of the tissue's
// grid, about 0.07 µm) is split along it, the signed distance taken as linear over the square (its
// gradient away from the boundary's nearest point), or, where two edges cross it, placed by its
// center: areas exact to about 10⁻⁶ of themselves, the error where an edge bends within such a
// square (an arc; the corner of a region's inner bands). The tissue's own pixels are counted exactly (summed counts over the grid); a drawn
// tissue window's edge splits the finest squares it crosses alike, their share inside it weighing
// what the measure adds. Squares are summed in a fixed order, so the areas are the same on any
// number of workers.

import (
	"context"
	"errors"
	"math"
	"sort"
)

// measure is what is summed over the tissue: for the square of half-side h about (x, y), whether
// all it tells apart is the same over the square, and the square's area, times the share of it in
// the tissue, added to its sums (a square of the finest size split where one edge crosses it).
// Each worker has its own.
type measure interface {
	uniform(x, y, h float64) bool
	add(x, y, h, inTissue float64, finest bool, sums []float64)
}

// belowLine is the share of the square of half-side h about the origin where gx·x + gy·y < t: the
// square clipped by the half-plane, its area by the shoelace formula.
func belowLine(gx, gy, t, h float64) float64 {
	if gx == 0 && gy == 0 {
		if t > 0 {
			return 1
		}
		return 0
	}
	corners := [4][2]float64{{-h, -h}, {h, -h}, {h, h}, {-h, h}}
	var poly [][2]float64
	for k, p := range corners {
		q := corners[(k+1)%4]
		vp, vq := mul(gx, p[0])+mul(gy, p[1])-t, mul(gx, q[0])+mul(gy, q[1])-t
		if vp < 0 {
			poly = append(poly, p)
		}
		if (vp < 0) != (vq < 0) {
			f := vp / (vp - vq)
			poly = append(poly, [2]float64{p[0] + f*(q[0]-p[0]), p[1] + f*(q[1]-p[1])})
		}
	}
	a := 0.0
	for k, p := range poly {
		q := poly[(k+1)%len(poly)]
		a += mul(p[0], q[1]) - mul(q[0], p[1])
	}
	return math.Min(1, math.Max(0, a/2/(4*h*h)))
}

// signedNearest is the signed distance to a region's boundary (inside negative), exact within
// limit, with its gradient: away from the nearest point of the boundary, outward.
func signedNearest(region *Window, x, y, limit float64) (s, gx, gy float64) {
	d, nx, ny := region.NearestWithin(x, y, limit)
	sign := 1.0
	if region.Contains(x, y) {
		sign = -1
	}
	if d > 0 && !math.IsNaN(nx) {
		gx, gy = sign*(x-nx)/d, sign*(y-ny)/d
	}
	return sign * d, gx, gy
}

// The integration grid: top squares of 2^top finest units.
const (
	areaTop   = 7  // a top square is 128 finest units
	areaUnits = 16 // finest units to a tissue pixel
)

// summedPixels makes the tissue window's summed pixel counts.
func (w *Window) summedPixels() {
	if w.pixels == nil || w.sat != nil {
		return
	}
	W, H := w.pw, w.ph
	sat := make([]int32, (W+1)*(H+1))
	for y := 0; y < H; y++ {
		run := int32(0)
		for x := 0; x < W; x++ {
			if w.pixels[y*W+x] {
				run++
			}
			sat[(y+1)*(W+1)+x+1] = sat[y*(W+1)+x+1] + run
		}
	}
	w.sat = sat
}

// pixelsIn counts the tissue pixels [x0, x1) × [y0, y1) (pixel indices, clipped).
func (w *Window) pixelsIn(x0, y0, x1, y1 int) (in, all int) {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, w.pw), min(y1, w.ph)
	if x1 <= x0 || y1 <= y0 {
		return 0, 0
	}
	W := w.pw + 1
	return int(w.sat[y1*W+x1] - w.sat[y0*W+x1] - w.sat[y1*W+x0] + w.sat[y0*W+x0]), (x1 - x0) * (y1 - y0)
}

// bounds is the box holding the window's boundary.
func (w *Window) bounds() (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for k := range w.ax {
		x0, x1 = math.Min(x0, math.Min(w.ax[k], w.bx[k])), math.Max(x1, math.Max(w.ax[k], w.bx[k]))
		y0, y1 = math.Min(y0, math.Min(w.ay[k], w.by[k])), math.Max(y1, math.Max(w.ay[k], w.by[k]))
	}
	return
}

// tissueAreas sums a measure's areas (outputs of them) over the tissue window.
func tissueAreas(ctx context.Context, tissue *Window, outputs int, newMeasure func() measure, workers int) ([]float64, error) {
	if workers < 1 {
		workers = 1
	}
	bx0, by0, bx1, by1 := tissue.bounds()
	if !(bx1 > bx0) || !(by1 > by0) {
		return nil, errors.New("spatial: a window with no extent")
	}
	// The finest unit: a sixteenth of the tissue's pixel, the grid's origin the pixels'; for a drawn
	// window, a 128th of 8 µm from its corner.
	var unit, ox, oy float64
	if tissue.pixels != nil {
		tissue.summedPixels()
		unit = tissue.pg / areaUnits
	} else {
		unit = 8.0 / (1 << areaTop)
		ox, oy = bx0, by0
	}
	top := int64(1) << areaTop
	at := func(v float64) int64 { return floorDiv(int64(math.Floor(v/unit)), top) }
	i0, j0, i1, j1 := at(bx0-ox), at(by0-oy), at(bx1-ox), at(by1-oy)
	rows := int(j1 - j0 + 1)
	sums := make([][]float64, rows)
	errs := make([]error, workers)
	parallelFor(rows, workers, func(wk, a, b int) {
		t := tissue
		if t.pixels == nil {
			t = tissue.Copy()
		}
		m := newMeasure()
		for r := a; r < b; r++ {
			if ctx.Err() != nil {
				errs[wk] = ctx.Err()
				return
			}
			row := make([]float64, outputs)
			var visit func(ix, iy int64, level uint)
			visit = func(ix, iy int64, level uint) {
				n := int64(1) << level
				x0, y0 := ox+float64(ix)*unit, oy+float64(iy)*unit
				side := float64(n) * unit
				cx, cy := x0+side/2, y0+side/2
				// The tissue over the square: all, none or some of it.
				all, none := false, false
				if t.pixels != nil {
					px0, py0 := floorDiv(ix, areaUnits)+int64(t.pm), floorDiv(iy, areaUnits)+int64(t.pm)
					px1, py1 := floorDiv(ix+n-1, areaUnits)+1+int64(t.pm), floorDiv(iy+n-1, areaUnits)+1+int64(t.pm)
					in, cells := t.pixelsIn(int(px0), int(py0), int(px1), int(py1))
					total := int((px1 - px0) * (py1 - py0))
					all, none = in == total && cells == total, in == 0
				} else {
					d := t.DistanceWithin(cx, cy, side)
					inside := t.Contains(cx, cy)
					all, none = inside && d > side*math.Sqrt2/2, !inside && d > side*math.Sqrt2/2
				}
				if none {
					return
				}
				if level == 0 {
					share := 1.0
					if !all {
						// A drawn window's edge across the square: the share inside it (a pixel
						// window's squares this small are wholly in or out).
						if t.pixels != nil {
							share = 0
							if t.Contains(cx, cy) {
								share = 1
							}
						} else {
							s, gx, gy := signedNearest(t, cx, cy, side)
							share = belowLine(gx, gy, -s, side/2)
						}
					}
					if share > 0 {
						m.add(cx, cy, side/2, share, true, row)
					}
					return
				}
				if all && m.uniform(cx, cy, side/2) {
					m.add(cx, cy, side/2, 1, false, row)
					return
				}
				h := n / 2
				visit(ix, iy, level-1)
				visit(ix+h, iy, level-1)
				visit(ix, iy+h, level-1)
				visit(ix+h, iy+h, level-1)
			}
			for i := i0; i <= i1; i++ {
				visit(i*top, (j0+int64(r))*top, areaTop)
			}
			sums[r] = row
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	out := make([]float64, outputs)
	for _, row := range sums {
		for k, v := range row {
			out[k] += v
		}
	}
	return out, nil
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// signed is the signed distance to a region's boundary, inside negative, exact within limit.
func signed(region *Window, x, y, limit float64) float64 {
	d := region.DistanceWithin(x, y, limit)
	if region.Contains(x, y) {
		return -d
	}
	return d
}

// bandOf is the band a signed distance falls in: 0 below the first edge, k from edge k − 1 up to
// edge k, len(edges) at the last edge or beyond.
func bandOf(edges []float64, s float64) int {
	return sort.Search(len(edges), func(k int) bool { return edges[k] > s })
}

// bandMeasure sums areas by band of signed distance from a region's boundary.
type bandMeasure struct {
	region *Window
	edges  []float64
	limit  float64
}

func (m *bandMeasure) uniform(x, y, h float64) bool {
	half := h * math.Sqrt2 * (1 + 1e-12)
	s := signed(m.region, x, y, m.limit+half)
	k := bandOf(m.edges, s)
	if k > 0 && s-m.edges[k-1] <= half || k < len(m.edges) && m.edges[k]-s <= half {
		return false
	}
	return true
}

func (m *bandMeasure) add(x, y, h, inTissue float64, finest bool, sums []float64) {
	area := 4 * h * h * inTissue
	s := signed(m.region, x, y, m.limit+2*h)
	k := bandOf(m.edges, s)
	if finest {
		// An edge crossing the square: the square split along it.
		half := h * math.Sqrt2
		e := -1
		if k > 0 && s-m.edges[k-1] <= half {
			e = k - 1
		} else if k < len(m.edges) && m.edges[k]-s <= half {
			e = k
		}
		if e >= 0 {
			_, gx, gy := signedNearest(m.region, x, y, m.limit+2*h)
			below := belowLine(gx, gy, m.edges[e]-s, h)
			sums[e] += below * area
			sums[e+1] += (1 - below) * area
			return
		}
	}
	sums[k] += area
}

// BandAreas are the tissue's areas (µm²) by band of signed distance from a region's boundary
// (inside negative): below edges[0], between each pair of edges, and from the last on.
func BandAreas(ctx context.Context, tissue, region *Window, edges []float64, workers int) ([]float64, error) {
	if len(edges) == 0 || !sort.Float64sAreSorted(edges) {
		return nil, errors.New("spatial: bands need edges in order")
	}
	limit := math.Max(math.Abs(edges[0]), math.Abs(edges[len(edges)-1]))
	return tissueAreas(ctx, tissue, len(edges)+1, func() measure {
		return &bandMeasure{region: region.Copy(), edges: edges, limit: limit}
	}, workers)
}

// regionMeasure sums the areas of regions (each, then the tissue in none of them).
type regionMeasure struct {
	regions []*Window
}

func (m *regionMeasure) uniform(x, y, h float64) bool {
	half := h * math.Sqrt2 * (1 + 1e-12)
	for _, r := range m.regions {
		if r.DistanceWithin(x, y, half) <= half {
			return false
		}
	}
	return true
}

func (m *regionMeasure) add(x, y, h, inTissue float64, finest bool, sums []float64) {
	area := 4 * h * h * inTissue
	// At the finest size, one region's boundary crossing the square splits it; the others' (rarely
	// two cross one square) are placed by its center.
	crossing, share := -1, 0.0
	if finest {
		half := h * math.Sqrt2
		for k, r := range m.regions {
			if s, gx, gy := signedNearest(r, x, y, 2*h); math.Abs(s) <= half {
				if crossing >= 0 {
					crossing = -1
					break
				}
				crossing = k
				share = belowLine(gx, gy, -s, h)
			}
		}
	}
	elsewhere := 1.0
	for k, r := range m.regions {
		in := 0.0
		if k == crossing {
			in = share
		} else if r.Contains(x, y) {
			in = 1
		}
		sums[k] += in * area
		elsewhere = math.Min(elsewhere, 1-in)
	}
	sums[len(m.regions)] += elsewhere * area
}

// RegionAreas are the areas (µm²) of the tissue within each region, and of the tissue in none.
func RegionAreas(ctx context.Context, tissue *Window, regions []*Window, workers int) ([]float64, error) {
	return tissueAreas(ctx, tissue, len(regions)+1, func() measure {
		m := &regionMeasure{}
		for _, r := range regions {
			m.regions = append(m.regions, r.Copy())
		}
		return m
	}, workers)
}
