package spatial

// A window: the part of the plane a point pattern was observed in, for statistics that need its area
// and its edge (Ripley's K). A window is a set of boundary segments with the area they enclose,
// inside by the even–odd rule (a point is inside when a ray from it crosses the boundary an odd
// number of times), so polygons with holes, and several polygons, are windows alike. Two kinds:
//
//   - a drawn region's polygons (µm), as the page sends them;
//   - the tissue: the pixels of the segmentation's cells on a grid of about a µm, closed by a radius
//     (dilated, then eroded, by the exact distance transform), so that gaps between cells narrower
//     than twice the radius are tissue and wider ones (airways, tears, the space beyond the section)
//     are not; its boundary the grid's pixel edges, merged along rows and columns.
//
// Segments are indexed by a grid of squares, so the boundary near a point, or a circle, is found
// without scanning all of it. Its answers are the same on every processor: each product added is
// rounded on its own (mul), and the angles of a circle's crossings are dmath's.

import (
	"context"
	"errors"
	"math"
	"sort"

	"situcraft/internal/dmath"
	"situcraft/internal/segment"
	"situcraft/internal/segment/expand"
)

// Window is a window's boundary and area.
type Window struct {
	Area           float64 // µm²
	ax, ay, bx, by []float64
	x0, y0, side   float64
	across, down   int
	cells          [][]int32 // the segments touching each square
	mark           []uint32  // per query, segments seen (one query at a time per window copy)
	stamp          uint32
	// The tissue window's pixels, when it is one: inside or not, pw × ph of side pg µm, pixel
	// (pm, pm) at the origin; and their counts summed from the top left (made by areas).
	pixels     []bool
	pw, ph, pm int
	pg         float64
	sat        []int32
}

// newWindow indexes segments.
func newWindow(ax, ay, bx, by []float64, area float64) (*Window, error) {
	if len(ax) == 0 || !(area > 0) {
		return nil, errors.New("spatial: a window with no boundary or no area")
	}
	w := &Window{Area: area, ax: ax, ay: ay, bx: bx, by: by}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for k := range ax {
		x0, x1 = math.Min(x0, math.Min(ax[k], bx[k])), math.Max(x1, math.Max(ax[k], bx[k]))
		y0, y1 = math.Min(y0, math.Min(ay[k], by[k])), math.Max(y1, math.Max(ay[k], by[k]))
	}
	// Squares holding a few segments each on average.
	span := math.Max(x1-x0, y1-y0)
	w.side = math.Max(span/math.Max(1, math.Sqrt(float64(len(ax))/2)), 1e-6)
	w.x0, w.y0 = x0, y0
	w.across = int((x1-x0)/w.side) + 1
	w.down = int((y1-y0)/w.side) + 1
	w.cells = make([][]int32, w.across*w.down)
	for k := range ax {
		c0, r0 := w.square(math.Min(ax[k], bx[k]), math.Min(ay[k], by[k]))
		c1, r1 := w.square(math.Max(ax[k], bx[k]), math.Max(ay[k], by[k]))
		for r := r0; r <= r1; r++ {
			for c := c0; c <= c1; c++ {
				w.cells[r*w.across+c] = append(w.cells[r*w.across+c], int32(k))
			}
		}
	}
	w.mark = make([]uint32, len(ax))
	return w, nil
}

// Copy is a window sharing the boundary, for another goroutine's queries.
func (w *Window) Copy() *Window {
	c := *w
	c.mark = make([]uint32, len(w.ax))
	c.stamp = 0
	return &c
}

// Segments counts the boundary's segments.
func (w *Window) Segments() int { return len(w.ax) }

func (w *Window) square(x, y float64) (int, int) {
	c := int(math.Floor((x - w.x0) / w.side))
	r := int(math.Floor((y - w.y0) / w.side))
	return min(max(c, 0), w.across-1), min(max(r, 0), w.down-1)
}

// near calls do for each segment touching the squares the box [x0, x1] × [y0, y1] touches, once.
func (w *Window) near(x0, y0, x1, y1 float64, do func(k int)) {
	w.stamp++
	if w.stamp == 0 {
		clear(w.mark)
		w.stamp = 1
	}
	c0, r0 := w.square(x0, y0)
	c1, r1 := w.square(x1, y1)
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			for _, k := range w.cells[r*w.across+c] {
				if w.mark[k] != w.stamp {
					w.mark[k] = w.stamp
					do(int(k))
				}
			}
		}
	}
}

// Contains says whether (x, y) is inside, by the even–odd rule: the boundary's crossings of a ray
// from it to the right (each segment counted when it spans y, lower end included, upper excluded).
func (w *Window) Contains(x, y float64) bool {
	if math.IsNaN(x) || math.IsNaN(y) {
		return false
	}
	if w.pixels != nil {
		px, py := int(math.Floor(x/w.pg))+w.pm, int(math.Floor(y/w.pg))+w.pm
		return px >= 0 && py >= 0 && px < w.pw && py < w.ph && w.pixels[py*w.pw+px]
	}
	inside := false
	xEnd := w.x0 + mul(float64(w.across), w.side)
	if x > xEnd || y < w.y0 || y > w.y0+mul(float64(w.down), w.side) {
		return false
	}
	w.near(x, y, xEnd, y, func(k int) {
		ay, by := w.ay[k], w.by[k]
		if (ay <= y) == (by <= y) {
			return
		}
		// Where the segment crosses the ray's line.
		t := (y - ay) / (by - ay)
		cx := w.ax[k] + mul(t, w.bx[k]-w.ax[k])
		if cx > x {
			inside = !inside
		}
	})
	return inside
}

// Distance is the distance from (x, y) to the boundary, found ring by ring of squares.
func (w *Window) Distance(x, y float64) float64 { return w.DistanceWithin(x, y, math.Inf(1)) }

// DistanceWithin is the distance from (x, y) to the boundary when it is at most limit; farther, it
// is some distance above limit (+Inf when no segment was looked at).
func (w *Window) DistanceWithin(x, y, limit float64) float64 {
	d, _, _ := w.NearestWithin(x, y, limit)
	return d
}

// NearestWithin is DistanceWithin with the boundary's point nearest (x, y) (NaN when no segment was
// looked at).
func (w *Window) NearestWithin(x, y, limit float64) (d, nx, ny float64) {
	// Squared distances compared, the root taken once (the same as the least root).
	best := math.Inf(1)
	nx, ny = math.NaN(), math.NaN()
	cx, cy := w.square(x, y)
	for ring := 0; ; ring++ {
		w.stamp++
		if w.stamp == 0 {
			clear(w.mark)
			w.stamp = 1
		}
		any := false
		for r := cy - ring; r <= cy+ring; r++ {
			for c := cx - ring; c <= cx+ring; c++ {
				if max(abs(r-cy), abs(c-cx)) != ring || r < 0 || c < 0 || r >= w.down || c >= w.across {
					continue
				}
				any = true
				for _, k := range w.cells[r*w.across+c] {
					if w.mark[k] != w.stamp {
						w.mark[k] = w.stamp
						if d2, px, py := segmentNearest(x, y, w.ax[k], w.ay[k], w.bx[k], w.by[k]); d2 < best {
							best, nx, ny = d2, px, py
						}
					}
				}
			}
		}
		// Every square of the next ring lies at least this far.
		reach := float64(ring) * w.side
		if best <= mul(reach, reach) || reach > limit || !any && ring > max(w.across, w.down) {
			return math.Sqrt(best), nx, ny
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func segmentDistance2(px, py, ax, ay, bx, by float64) float64 {
	d2, _, _ := segmentNearest(px, py, ax, ay, bx, by)
	return d2
}

// segmentNearest is the squared distance from p to the segment ab, and the segment's point nearest
// p.
func segmentNearest(px, py, ax, ay, bx, by float64) (d2, nx, ny float64) {
	dx, dy := bx-ax, by-ay
	l2 := mul(dx, dx) + mul(dy, dy)
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, (mul(px-ax, dx)+mul(py-ay, dy))/l2))
	}
	nx, ny = ax+mul(t, dx), ay+mul(t, dy)
	ex, ey := px-nx, py-ny
	return mul(ex, ex) + mul(ey, ey), nx, ny
}

// ArcInside is the share of the circle of radius r about (x, y) inside the window: the circle cut
// where it crosses the boundary, each arc between crossings inside or not by its midpoint.
func (w *Window) ArcInside(x, y, r float64) float64 {
	if !(r > 0) {
		if w.Contains(x, y) {
			return 1
		}
		return 0
	}
	var angles []float64
	w.near(x-r, y-r, x+r, y+r, func(k int) {
		ax, ay := w.ax[k]-x, w.ay[k]-y
		dx, dy := w.bx[k]-w.ax[k], w.by[k]-w.ay[k]
		// |a + t d|² = r²: t² d·d + 2t a·d + a·a − r² = 0.
		A := mul(dx, dx) + mul(dy, dy)
		B := mul(ax, dx) + mul(ay, dy)
		C := mul(ax, ax) + mul(ay, ay) - mul(r, r)
		disc := mul(B, B) - mul(A, C)
		if !(A > 0) || disc < 0 {
			return
		}
		s := math.Sqrt(disc)
		for _, t := range []float64{(-B - s) / A, (-B + s) / A} {
			if t >= 0 && t < 1 {
				angles = append(angles, dmath.Atan2(ay+mul(t, dy), ax+mul(t, dx)))
			}
		}
	})
	if len(angles) == 0 {
		if w.Contains(x+r, y) {
			return 1
		}
		return 0
	}
	sort.Float64s(angles)
	inside := 0.0
	for i, a := range angles {
		b := angles[(i+1)%len(angles)]
		if i == len(angles)-1 {
			b += 2 * math.Pi
		}
		if b-a <= 0 {
			continue
		}
		m := (a + b) / 2
		sin, cos := dmath.SinCos(m)
		if w.Contains(x+mul(r, cos), y+mul(r, sin)) {
			inside += b - a
		}
	}
	return inside / (2 * math.Pi)
}

// PolygonWindow is the window of polygons (each rings of [x, y] µm, the first its outer ring, the
// others its holes; a ring not repeating its first point).
func PolygonWindow(polygons [][][][2]float64) (*Window, error) {
	var ax, ay, bx, by []float64
	var rings [][][2]float64
	for _, poly := range polygons {
		for _, ring := range poly {
			if len(ring) < 3 {
				return nil, errors.New("spatial: a window's ring has fewer than three points")
			}
			for i, p := range ring {
				q := ring[(i+1)%len(ring)]
				if math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[0], 0) || math.IsInf(p[1], 0) {
					return nil, errors.New("spatial: a window's point is not finite")
				}
				if p == q {
					continue
				}
				ax, ay, bx, by = append(ax, p[0]), append(ay, p[1]), append(bx, q[0]), append(by, q[1])
			}
			rings = append(rings, ring)
		}
	}
	// The area by the even–odd rule: each ring's area, added when it lies inside an even number of
	// the others (an outer ring), taken away when odd (a hole).
	area := 0.0
	for i, ring := range rings {
		a := 0.0
		for k, p := range ring {
			q := ring[(k+1)%len(ring)]
			a += mul(p[0], q[1]) - mul(q[0], p[1])
		}
		a = math.Abs(a) / 2
		depth := 0
		for j, other := range rings {
			if j != i && ringContains(other, ring[0][0], ring[0][1]) {
				depth++
			}
		}
		if depth%2 == 0 {
			area += a
		} else {
			area -= a
		}
	}
	return newWindow(ax, ay, bx, by, area)
}

func ringContains(ring [][2]float64, x, y float64) bool {
	inside := false
	for i, p := range ring {
		q := ring[(i+1)%len(ring)]
		if (p[1] <= y) != (q[1] <= y) {
			if p[0]+mul((y-p[1])/(q[1]-p[1]), q[0]-p[0]) > x {
				inside = !inside
			}
		}
	}
	return inside
}

// TissueWindow is the tissue's window from a segmentation's label raster: its cells' pixels on a
// grid of about grid µm, closed by closing µm. It answers the window and the grid's side.
func TissueWindow(ctx context.Context, mask *segment.Raster, grid, closing float64, workers int) (*Window, float64, error) {
	if !(mask.PixelSize > 0) || mask.Width <= 0 || mask.Height <= 0 {
		return nil, 0, errors.New("spatial: the label raster has no pixels")
	}
	if !(grid > 0) || closing < 0 {
		return nil, 0, errors.New("spatial: a tissue window's grid and closing radius")
	}
	f := max(1, int(math.Round(grid/mask.PixelSize)))
	g := float64(f) * mask.PixelSize
	// A margin of the closing radius and a pixel all around: the space beyond the raster is not
	// tissue.
	m := int(math.Ceil(closing/g)) + 2
	W := (mask.Width+f-1)/f + 2*m
	H := (mask.Height+f-1)/f + 2*m
	occupied := make([]uint32, W*H)
	for y, row := range mask.Rows {
		cy := y/f + m
		x := 0
		for _, run := range row {
			if run.Label != 0 {
				for cx := x / f; cx <= (x+int(run.Length)-1)/f; cx++ {
					occupied[cy*W+cx+m] = 1
				}
			}
			x += int(run.Length)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r2 := mul(closing/g, closing/g)
	// Dilated: within the radius of a cell's pixel. Closed: farther than the radius from every
	// pixel the dilation left out.
	d := expand.SquaredDistances(occupied, W, H, workers)
	outside := make([]uint32, W*H)
	for i := range d {
		if float64(d[i]) > r2 {
			outside[i] = 1
		}
	}
	e := expand.SquaredDistances(outside, W, H, workers)
	in := make([]bool, W*H)
	pixels := 0
	for i := range e {
		if outside[i] == 0 && float64(e[i]) > r2 {
			in[i] = true
			pixels++
		}
	}
	if pixels == 0 {
		return nil, 0, errors.New("spatial: the tissue window holds no pixel")
	}
	// The boundary: pixel edges between in and out, merged along each row and column.
	var ax, ay, bx, by []float64
	at := func(px, py int) (float64, float64) { return float64(px-m) * g, float64(py-m) * g }
	inAt := func(x, y int) bool { return x >= 0 && y >= 0 && x < W && y < H && in[y*W+x] }
	for y := 0; y <= H; y++ {
		start := -1
		for x := 0; x <= W; x++ {
			edge := x < W && inAt(x, y-1) != inAt(x, y)
			if edge && start < 0 {
				start = x
			}
			if (!edge || x == W) && start >= 0 {
				x0, y0 := at(start, y)
				x1, _ := at(x, y)
				ax, ay, bx, by = append(ax, x0), append(ay, y0), append(bx, x1), append(by, y0)
				start = -1
			}
		}
	}
	for x := 0; x <= W; x++ {
		start := -1
		for y := 0; y <= H; y++ {
			edge := y < H && inAt(x-1, y) != inAt(x, y)
			if edge && start < 0 {
				start = y
			}
			if (!edge || y == H) && start >= 0 {
				x0, y0 := at(x, start)
				_, y1 := at(x, y)
				ax, ay, bx, by = append(ax, x0), append(ay, y0), append(bx, x0), append(by, y1)
				start = -1
			}
		}
	}
	w, err := newWindow(ax, ay, bx, by, float64(pixels)*g*g)
	if err != nil {
		return nil, 0, err
	}
	w.pixels, w.pw, w.ph, w.pm, w.pg = in, W, H, m, g
	return w, g, nil
}
