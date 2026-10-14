package segment

// A cell's outline from its pixels: the largest 4-connected piece of the cell, its outer boundary
// followed along pixel edges (so the polygon encloses exactly its pixels, and its area is theirs),
// then simplified by Douglas–Peucker (Douglas DH, Peucker TK. Algorithms for the reduction of the
// number of points required to represent a digitized line or its caricature. Cartographica
// 1973;10(2):112–122. doi:10.3138/FM57-6770-U75U-7727) to a tolerance in pixels. A cell's other
// pieces and holes are counted, not drawn.

import "math"

// Outline is one cell's traced outline.
type Outline struct {
	// X, Y are the vertices in µm (the ring is closed implicitly).
	X, Y []float32
	// Pixels is the traced piece's area in pixels (with its holes, the unsimplified ring's area).
	Pixels int
	// Pieces counts the cell's 4-connected pieces; Holes the traced piece's holes.
	Pieces, Holes int
}

// TraceOutline traces label's outline within its bounding box (stats from Raster.Stats), with
// vertices simplified to within tolerance pixels. A label with no pixels has no outline.
func (r *Raster) TraceOutline(label uint32, s LabelStats, tolerance float64) Outline {
	if s.Pixels == 0 {
		return Outline{}
	}
	// The box with a margin of one pixel, as a bitmap of the label.
	x0, y0 := s.MinX-1, s.MinY-1
	w, h := s.MaxX-s.MinX+3, s.MaxY-s.MinY+3
	in := make([]bool, w*h)
	row := make([]uint32, s.MaxX-s.MinX+1)
	for y := s.MinY; y <= s.MaxY; y++ {
		r.RowRange(y, s.MinX, s.MaxX+1, row)
		for x := s.MinX; x <= s.MaxX; x++ {
			in[(y-y0)*w+x-x0] = row[x-s.MinX] == label
		}
	}
	// The 4-connected pieces; the largest is traced (the first found, of equal ones).
	piece := make([]int32, w*h)
	var sizes []int
	stack := []int{}
	for i := range in {
		if !in[i] || piece[i] != 0 {
			continue
		}
		id := int32(len(sizes) + 1)
		size := 0
		piece[i] = id
		stack = append(stack[:0], i)
		for len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			for _, k := range [4]int{j - 1, j + 1, j - w, j + w} {
				if in[k] && piece[k] == 0 {
					piece[k] = id
					stack = append(stack, k)
				}
			}
		}
		sizes = append(sizes, size)
	}
	best := 0
	for i := range sizes {
		if sizes[i] > sizes[best] {
			best = i
		}
	}
	id := int32(best + 1)
	inside := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < w && y < h && piece[y*w+x] == id
	}
	// The first pixel of the piece in raster order: its top edge is on the outer boundary.
	start := 0
	for start < len(piece) && piece[start] != id {
		start++
	}
	sx, sy := start%w, start/w
	// Follow the boundary with the piece on the right (y down): directions E, S, W, N.
	dirs := [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	cx, cy, d := sx, sy, 0
	var xs, ys []float64
	for {
		// The squares ahead of the corner (cx, cy), to the right and left of direction d.
		var rx, ry, lx, ly int
		switch d {
		case 0: // E
			rx, ry, lx, ly = cx, cy, cx, cy-1
		case 1: // S
			rx, ry, lx, ly = cx-1, cy, cx, cy
		case 2: // W
			rx, ry, lx, ly = cx-1, cy-1, cx-1, cy
		default: // N
			rx, ry, lx, ly = cx, cy-1, cx-1, cy-1
		}
		nd := d
		switch R, L := inside(rx, ry), inside(lx, ly); {
		case !R:
			nd = (d + 1) % 4 // turn right (also at a diagonal touch: pieces are 4-connected)
		case L:
			nd = (d + 3) % 4 // turn left
		}
		if nd != d || len(xs) == 0 {
			xs, ys = append(xs, float64(cx)), append(ys, float64(cy))
		}
		d = nd
		cx, cy = cx+dirs[d][0], cy+dirs[d][1]
		// The start corner touches only the first pixel (nothing above it or to its left is in
		// the piece), so the outer boundary passes it once: back there, the ring is closed.
		if cx == sx && cy == sy {
			break
		}
	}
	// Holes of the piece: background regions inside it not reached from the box's edge.
	holes := countHoles(piece, id, w, h)
	xs, ys = simplifyRing(xs, ys, tolerance)
	out := Outline{Pixels: sizes[best], Pieces: len(sizes), Holes: holes, X: make([]float32, len(xs)), Y: make([]float32, len(ys))}
	for i := range xs {
		out.X[i] = float32((xs[i] + float64(x0)) * r.PixelSize)
		out.Y[i] = float32((ys[i] + float64(y0)) * r.PixelSize)
	}
	return out
}

// countHoles counts the 4-connected background regions enclosed by piece id (not reaching the
// box's edge, which the one-pixel margin keeps outside).
func countHoles(piece []int32, id int32, w, h int) int {
	seen := make([]bool, len(piece))
	holes := 0
	stack := []int{}
	for i := range piece {
		if piece[i] == id || seen[i] {
			continue
		}
		edge := false
		seen[i] = true
		stack = append(stack[:0], i)
		for len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := j%w, j/w
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				edge = true
			}
			for _, k := range [4]int{j - 1, j + 1, j - w, j + w} {
				if k >= 0 && k < len(piece) && (k%w == j%w || k/w == j/w) && piece[k] != id && !seen[k] {
					seen[k] = true
					stack = append(stack, k)
				}
			}
		}
		if !edge {
			holes++
		}
	}
	return holes
}

// RingArea is a ring's area (shoelace), positive whatever its orientation.
func RingArea(xs, ys []float32) float64 {
	var a float64
	for i, j := 0, len(xs)-1; i < len(xs); j, i = i, i+1 {
		a += float64(xs[j])*float64(ys[i]) - float64(xs[i])*float64(ys[j])
	}
	return math.Abs(a) / 2
}

// simplifyRing simplifies a closed ring by Douglas–Peucker, keeping its first vertex and the one
// farthest from it as anchors; it keeps at least three vertices.
func simplifyRing(xs, ys []float64, eps float64) ([]float64, []float64) {
	n := len(xs)
	if n <= 3 || eps <= 0 {
		return xs, ys
	}
	far, best := 0, -1.0
	for i := 1; i < n; i++ {
		if d := math.Hypot(xs[i]-xs[0], ys[i]-ys[0]); d > best {
			far, best = i, d
		}
	}
	keep := make([]bool, n)
	keep[0], keep[far] = true, true
	var span func(a, b int)
	span = func(a, b int) {
		// The open chain a..b (b may wrap to n, meaning vertex 0).
		maxD, at := -1.0, -1
		bx, by := xs[b%n], ys[b%n]
		for i := a + 1; i < b; i++ {
			if d := segmentDistance(xs[i], ys[i], xs[a], ys[a], bx, by); d > maxD {
				maxD, at = d, i
			}
		}
		if at >= 0 && maxD > eps {
			keep[at] = true
			span(a, at)
			span(at, b)
		}
	}
	span(0, far)
	span(far, n)
	var ox, oy []float64
	for i := range xs {
		if keep[i] {
			ox, oy = append(ox, xs[i]), append(oy, ys[i])
		}
	}
	if len(ox) < 3 {
		return xs, ys
	}
	return ox, oy
}

func segmentDistance(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
	}
	return math.Hypot(ax+t*dx-px, ay+t*dy-py)
}
