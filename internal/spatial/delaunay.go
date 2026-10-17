package spatial

// The Delaunay triangulation of points in the plane (Lee DT, Schachter BJ. Two algorithms for
// constructing a Delaunay triangulation. Int J Comput Inf Sci 1980;9:219–242), built by inserting
// one point at a time (Bowyer A. Computing Dirichlet tessellations. Comput J 1981;24:162–166;
// Watson DF. Computing the n-dimensional Delaunay tessellation with application to Voronoi
// polytopes. Comput J 1981;24:167–172): the triangles whose circumcircles hold the new point are
// removed, and the hole they leave is filled with triangles fanning from it. The convex hull's
// edges are closed by "ghost" triangles sharing a vertex at infinity, whose circumcircle is the
// open half-plane beyond the edge (with the open edge itself), so that a point outside the hull is
// inserted as one inside it. Points are inserted along a Hilbert curve, so that each is found by a
// short walk from the last.
//
// The predicates are exact (predicates.go), so the triangulation is Delaunay for any points: no
// point lies strictly inside any triangle's circumcircle. Where four or more points lie on one
// circle (as on a lattice), the triangulation is not unique; a triangle is replaced only when the
// new point lies strictly inside its circle, so the order of insertion, and therefore the result,
// is fixed by the points alone. Points at the same place are triangulated once (the first of them);
// the others are reported as its duplicates.

import (
	"context"
	"errors"
	"math"
	"sort"
)

const ghost = -1

// triangulation is a triangulation being built: triangle t has vertices v[3t..3t+2]
// (counter-clockwise; a ghost triangle's third vertex is the ghost) and, across the edge opposite
// its vertex i, the neighbor n[3t+i].
type triangulation struct {
	x, y []float64
	v    []int
	n    []int
	dead []bool
	free []int
	last int // a live triangle near the last point inserted
	// For one insertion: the triangles in the cavity, each marked with the insertion's stamp.
	cavity []int
	mark   []int
	stamp  int
}

func (t *triangulation) isGhost(tri int) bool { return t.v[3*tri+2] == ghost }

func (t *triangulation) add(a, b, c int) int {
	if k := len(t.free); k > 0 {
		tri := t.free[k-1]
		t.free = t.free[:k-1]
		t.v[3*tri], t.v[3*tri+1], t.v[3*tri+2] = a, b, c
		t.n[3*tri], t.n[3*tri+1], t.n[3*tri+2] = -1, -1, -1
		t.dead[tri] = false
		t.mark[tri] = 0
		return tri
	}
	tri := len(t.dead)
	t.v = append(t.v, a, b, c)
	t.n = append(t.n, -1, -1, -1)
	t.dead = append(t.dead, false)
	t.mark = append(t.mark, 0)
	return tri
}

// conflicts says whether point p lies strictly inside triangle tri's circumcircle (for a ghost
// triangle: strictly beyond its edge, or on the open edge).
func (t *triangulation) conflicts(tri, p int) bool {
	a, b, c := t.v[3*tri], t.v[3*tri+1], t.v[3*tri+2]
	px, py := t.x[p], t.y[p]
	if c == ghost {
		o := orient(t.x[a], t.y[a], t.x[b], t.y[b], px, py)
		if o != 0 {
			return o > 0
		}
		return between(t.x[a], t.y[a], t.x[b], t.y[b], px, py)
	}
	return incircle(t.x[a], t.y[a], t.x[b], t.y[b], t.x[c], t.y[c], px, py) > 0
}

// between says whether p, on the line through a and b, lies strictly between them.
func between(ax, ay, bx, by, px, py float64) bool {
	if ax != bx {
		return ax < px && px < bx || bx < px && px < ax
	}
	return ay < py && py < by || by < py && py < ay
}

// locate answers a triangle in conflict with p, found by walking from the last one: across any edge
// that p lies beyond, until p lies within (or on) a triangle, or beyond the hull in a ghost
// triangle. The walk ends on a Delaunay triangulation (Edelsbrunner H. An acyclicity theorem for
// cell complexes in d dimensions. Combinatorica 1990;10:251–260).
func (t *triangulation) locate(p int) int {
	tri := t.last
	if t.isGhost(tri) {
		tri = t.n[3*tri+2] // the real triangle across its edge
	}
	px, py := t.x[p], t.y[p]
	for steps := 0; ; steps++ {
		if t.isGhost(tri) {
			return tri
		}
		moved := false
		for i := 0; i < 3; i++ {
			a, b := t.v[3*tri+(i+1)%3], t.v[3*tri+(i+2)%3]
			if orient(t.x[a], t.y[a], t.x[b], t.y[b], px, py) < 0 {
				tri = t.n[3*tri+i]
				moved = true
				break
			}
		}
		if !moved {
			return tri
		}
		if steps > 4*len(t.dead)+16 {
			// Cannot happen on a Delaunay triangulation; searched in full rather than looped.
			for k := range t.dead {
				if !t.dead[k] && t.conflicts(k, p) {
					return k
				}
			}
			return tri
		}
	}
}

// insert adds point p: the triangles in conflict with it removed, the cavity they leave filled.
func (t *triangulation) insert(p int) {
	start := t.locate(p)
	t.stamp++
	t.cavity = append(t.cavity[:0], start)
	t.mark[start] = t.stamp
	for i := 0; i < len(t.cavity); i++ {
		tri := t.cavity[i]
		for j := 0; j < 3; j++ {
			nb := t.n[3*tri+j]
			if t.mark[nb] != t.stamp && t.conflicts(nb, p) {
				t.mark[nb] = t.stamp
				t.cavity = append(t.cavity, nb)
			}
		}
	}
	// The cavity's boundary: each edge a→b (the cavity to its left) whose outer triangle stays, and
	// which of the outer triangle's sides it is.
	type edge struct{ a, b, outer, side int }
	var boundary []edge
	for _, tri := range t.cavity {
		for j := 0; j < 3; j++ {
			nb := t.n[3*tri+j]
			if t.mark[nb] == t.stamp {
				continue
			}
			side := -1
			for k := 0; k < 3; k++ {
				if t.n[3*nb+k] == tri {
					side = k
				}
			}
			boundary = append(boundary, edge{t.v[3*tri+(j+1)%3], t.v[3*tri+(j+2)%3], nb, side})
		}
	}
	for _, tri := range t.cavity {
		t.dead[tri] = true
		t.free = append(t.free, tri)
	}
	// Fill: a triangle (a, b, p) for each boundary edge, a ghost turned to put the ghost last.
	made := make([]int, len(boundary))
	for i, e := range boundary {
		var tri int
		switch {
		case e.a == ghost:
			tri = t.add(e.b, p, ghost) // (ghost, b, p)
		case e.b == ghost:
			tri = t.add(p, e.a, ghost) // (a, ghost, p)
		default:
			tri = t.add(e.a, e.b, p)
		}
		made[i] = tri
		// The edge a–b is the one opposite p, and faces the outer triangle.
		for k := 0; k < 3; k++ {
			if t.v[3*tri+k] == p {
				t.n[3*tri+k] = e.outer
			}
		}
		t.n[3*e.outer+e.side] = tri
	}
	// The new triangles' edges through p join them to each other: the edge w→p of one meets the
	// edge p→w of another.
	from := make(map[int]int, len(made)) // w of edge p→w → its triangle
	for _, tri := range made {
		for k := 0; k < 3; k++ {
			if t.v[3*tri+k] == p {
				from[t.v[3*tri+(k+1)%3]] = tri
			}
		}
	}
	for _, tri := range made {
		for k := 0; k < 3; k++ {
			if t.v[3*tri+(k+1)%3] != p {
				continue
			}
			// Edge v[k]→p is opposite v[k+2]; in the other triangle, edge p→w is opposite its
			// third vertex.
			other := from[t.v[3*tri+k]]
			t.n[3*tri+(k+2)%3] = other
			for m := 0; m < 3; m++ {
				if t.v[3*other+m] == p {
					t.n[3*other+(m+2)%3] = tri
				}
			}
		}
	}
	t.last = made[0]
	for _, tri := range made {
		if !t.isGhost(tri) {
			t.last = tri
			break
		}
	}
}

// Triangulation is a Delaunay triangulation: its triangles (counter-clockwise, as indices of the
// points), and each point's duplicate of record (itself unless an earlier point lies at the same
// place).
type Triangulation struct {
	Triangles [][3]int32
	Same      []int32
	// Collinear: every point lies on one line, so there are no triangles; Path joins them along it.
	Collinear bool
	Path      []int32
}

// Triangulate makes the Delaunay triangulation of n points (xy, n × 2).
func Triangulate(ctx context.Context, xy []float64, n int) (*Triangulation, error) {
	if n < 0 || len(xy) < 2*n {
		return nil, errors.New("spatial: the points do not fill an n × 2 matrix")
	}
	out := &Triangulation{Same: make([]int32, n)}
	for i := 0; i < n; i++ {
		if math.IsNaN(xy[2*i]) || math.IsNaN(xy[2*i+1]) || math.IsInf(xy[2*i], 0) || math.IsInf(xy[2*i+1], 0) {
			return nil, errors.New("spatial: a point is not finite")
		}
		out.Same[i] = int32(i)
	}
	// Points at the same place: the first stands for the others.
	byPlace := make([]int32, n)
	for i := range byPlace {
		byPlace[i] = int32(i)
	}
	sort.Slice(byPlace, func(a, b int) bool {
		i, j := byPlace[a], byPlace[b]
		if xy[2*i] != xy[2*j] {
			return xy[2*i] < xy[2*j]
		}
		if xy[2*i+1] != xy[2*j+1] {
			return xy[2*i+1] < xy[2*j+1]
		}
		return i < j
	})
	var unique []int32
	for k, i := range byPlace {
		if k > 0 {
			j := byPlace[k-1]
			if xy[2*i] == xy[2*j] && xy[2*i+1] == xy[2*j+1] {
				out.Same[i] = out.Same[j]
				continue
			}
		}
		unique = append(unique, i)
	}
	if len(unique) < 3 {
		out.Collinear = true
		out.Path = sortedIndices(unique)
		return out, nil
	}
	order := hilbertOrder(xy, unique)
	t := &triangulation{x: make([]float64, n), y: make([]float64, n), v: make([]int, 0, 18*len(unique)), n: make([]int, 0, 18*len(unique))}
	for i := 0; i < n; i++ {
		t.x[i], t.y[i] = xy[2*i], xy[2*i+1]
	}
	// The first triangle: the first two points and the first point not on their line.
	a, b := order[0], order[1]
	third := -1
	for k := 2; k < len(order); k++ {
		if orient(t.x[a], t.y[a], t.x[b], t.y[b], t.x[order[k]], t.y[order[k]]) != 0 {
			third = k
			break
		}
	}
	if third < 0 {
		out.Collinear = true
		out.Path = alongLine(xy, unique)
		return out, nil
	}
	c := order[third]
	order = append(append([]int32{a, b, c}, order[2:third]...), order[third+1:]...)
	if orient(t.x[a], t.y[a], t.x[b], t.y[b], t.x[c], t.y[c]) < 0 {
		b, c = c, b
	}
	t0 := t.add(int(a), int(b), int(c))
	g0 := t.add(int(b), int(a), ghost) // beyond a→b
	g1 := t.add(int(c), int(b), ghost) // beyond b→c
	g2 := t.add(int(a), int(c), ghost) // beyond c→a
	t.n[3*t0+0], t.n[3*t0+1], t.n[3*t0+2] = g1, g2, g0
	// Ghost (u, v, G): opposite u is edge v–G, opposite v is edge G–u, opposite G is u–v.
	t.n[3*g0+2], t.n[3*g1+2], t.n[3*g2+2] = t0, t0, t0
	t.n[3*g0+0], t.n[3*g0+1] = g2, g1 // g0 = (b, a, G): across a–G is g2 = (a, c, G); across G–b is g1
	t.n[3*g1+0], t.n[3*g1+1] = g0, g2 // g1 = (c, b, G): across b–G is g0; across G–c is g2
	t.n[3*g2+0], t.n[3*g2+1] = g1, g0 // g2 = (a, c, G): across c–G is g1; across G–a is g0
	t.last = t0
	for k := 3; k < len(order); k++ {
		if k%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		t.insert(int(order[k]))
	}
	for tri := range t.dead {
		if !t.dead[tri] && !t.isGhost(tri) {
			out.Triangles = append(out.Triangles, [3]int32{int32(t.v[3*tri]), int32(t.v[3*tri+1]), int32(t.v[3*tri+2])})
		}
	}
	sort.Slice(out.Triangles, func(i, j int) bool {
		p, q := out.Triangles[i], out.Triangles[j]
		for k := 0; k < 3; k++ {
			if p[k] != q[k] {
				return p[k] < q[k]
			}
		}
		return false
	})
	return out, nil
}

func sortedIndices(v []int32) []int32 {
	out := append([]int32(nil), v...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// alongLine orders points lying on one line along it.
func alongLine(xy []float64, pts []int32) []int32 {
	out := append([]int32(nil), pts...)
	sort.Slice(out, func(a, b int) bool {
		i, j := out[a], out[b]
		if xy[2*i] != xy[2*j] {
			return xy[2*i] < xy[2*j]
		}
		return xy[2*i+1] < xy[2*j+1]
	})
	return out
}

// hilbertOrder orders points along a Hilbert curve over their bounding box (2¹⁶ cells a side),
// equal positions on the curve by index.
func hilbertOrder(xy []float64, pts []int32) []int32 {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, i := range pts {
		x0, y0 = math.Min(x0, xy[2*i]), math.Min(y0, xy[2*i+1])
		x1, y1 = math.Max(x1, xy[2*i]), math.Max(y1, xy[2*i+1])
	}
	const side = 1 << 16
	span := math.Max(x1-x0, y1-y0)
	if !(span > 0) {
		span = 1
	}
	key := make(map[int32]uint64, len(pts))
	for _, i := range pts {
		gx := uint32(math.Min(side-1, math.Floor((xy[2*i]-x0)/span*side)))
		gy := uint32(math.Min(side-1, math.Floor((xy[2*i+1]-y0)/span*side)))
		key[i] = hilbert(gx, gy, side)
	}
	out := append([]int32(nil), pts...)
	sort.Slice(out, func(a, b int) bool {
		ka, kb := key[out[a]], key[out[b]]
		if ka != kb {
			return ka < kb
		}
		return out[a] < out[b]
	})
	return out
}

// hilbert is the distance along the Hilbert curve of a cell of an n × n grid (n a power of two).
func hilbert(x, y, n uint32) uint64 {
	var d uint64
	for s := n / 2; s > 0; s /= 2 {
		var rx, ry uint32
		if x&s != 0 {
			rx = 1
		}
		if y&s != 0 {
			ry = 1
		}
		d += uint64(s) * uint64(s) * uint64((3*rx)^ry)
		if ry == 0 {
			if rx == 1 {
				x, y = n-1-x, n-1-y
			}
			x, y = y, x
		}
	}
	return d
}
