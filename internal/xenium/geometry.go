package xenium

import "math"

// polygonProblem says what is wrong with a boundary polygon, or "" when nothing is. Xenium writes
// each polygon closed (its last vertex repeats its first); a ring is checked for enough distinct
// vertices, finite coordinates, a non-zero area and simplicity (no edge crossing another).
func polygonProblem(x, y []float32) string {
	n := len(x)
	for i := 0; i < n; i++ {
		if !finite32(x[i]) || !finite32(y[i]) {
			return "non-finite"
		}
	}
	// The ring without its closing vertex.
	if n > 1 && x[0] == x[n-1] && y[0] == y[n-1] {
		n--
	}
	// Repeated consecutive vertices add zero-length edges that would make neighbours look apart.
	rx, ry := make([]float32, 0, n), make([]float32, 0, n)
	for i := 0; i < n; i++ {
		if i == 0 || x[i] != x[i-1] || y[i] != y[i-1] {
			rx, ry = append(rx, x[i]), append(ry, y[i])
		}
	}
	if len(rx) > 1 && rx[0] == rx[len(rx)-1] && ry[0] == ry[len(ry)-1] {
		rx, ry = rx[:len(rx)-1], ry[:len(ry)-1]
	}
	if len(rx) < 3 {
		return "degenerate"
	}
	if math.Abs(ringArea(rx, ry)) < 1e-9 {
		return "zero-area"
	}
	if !ringSimple(rx, ry) {
		return "self-intersecting"
	}
	return ""
}

func finite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// ringArea is the shoelace formula's signed area of a ring (vertices not repeated).
func ringArea(x, y []float32) float64 {
	var a float64
	n := len(x)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += float64(x[i])*float64(y[j]) - float64(x[j])*float64(y[i])
	}
	return a / 2
}

// ringSimple reports whether no two non-adjacent edges of the ring intersect.
func ringSimple(x, y []float32) bool {
	n := len(x)
	for i := 0; i < n; i++ {
		ax, ay, bx, by := float64(x[i]), float64(y[i]), float64(x[(i+1)%n]), float64(y[(i+1)%n])
		for j := i + 2; j < n; j++ {
			if i == 0 && j == n-1 {
				continue // the edges meeting at vertex 0
			}
			cx, cy, dx, dy := float64(x[j]), float64(y[j]), float64(x[(j+1)%n]), float64(y[(j+1)%n])
			if segmentsCross(ax, ay, bx, by, cx, cy, dx, dy) {
				return false
			}
		}
	}
	return true
}

// segmentsCross reports whether segments ab and cd share a point (collinear overlaps included).
func segmentsCross(ax, ay, bx, by, cx, cy, dx, dy float64) bool {
	d1 := orient(cx, cy, dx, dy, ax, ay)
	d2 := orient(cx, cy, dx, dy, bx, by)
	d3 := orient(ax, ay, bx, by, cx, cy)
	d4 := orient(ax, ay, bx, by, dx, dy)
	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return true
	}
	return d1 == 0 && onSegment(cx, cy, dx, dy, ax, ay) || d2 == 0 && onSegment(cx, cy, dx, dy, bx, by) ||
		d3 == 0 && onSegment(ax, ay, bx, by, cx, cy) || d4 == 0 && onSegment(ax, ay, bx, by, dx, dy)
}

func orient(ax, ay, bx, by, cx, cy float64) float64 {
	return (bx-ax)*(cy-ay) - (by-ay)*(cx-ax)
}

func onSegment(ax, ay, bx, by, px, py float64) bool {
	return math.Min(ax, bx) <= px && px <= math.Max(ax, bx) && math.Min(ay, by) <= py && py <= math.Max(ay, by)
}

// Region is a rectangle of the tissue, in µm.
type Region struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// bounds accumulates the region of a set of points.
type bounds struct {
	r   Region
	any bool
}

func (b *bounds) add(x, y float64) {
	if !b.any {
		b.r = Region{x, y, x, y}
		b.any = true
		return
	}
	b.r.X0, b.r.Y0 = math.Min(b.r.X0, x), math.Min(b.r.Y0, y)
	b.r.X1, b.r.Y1 = math.Max(b.r.X1, x), math.Max(b.r.Y1, y)
}

func (b *bounds) region() *Region {
	if !b.any {
		return nil
	}
	r := b.r
	return &r
}
