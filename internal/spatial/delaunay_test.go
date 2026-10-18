package spatial

import (
	"context"
	"math"
	"sort"
	"testing"

	"situcraft/internal/random"
)

// hullCount counts the points on the convex hull's boundary (those on its edges too), by Andrew's
// monotone chain with collinear points kept.
func hullCount(xy []float64, pts []int) int {
	p := append([]int(nil), pts...)
	sort.Slice(p, func(a, b int) bool {
		i, j := p[a], p[b]
		if xy[2*i] != xy[2*j] {
			return xy[2*i] < xy[2*j]
		}
		return xy[2*i+1] < xy[2*j+1]
	})
	chain := func(order []int) []int {
		var h []int
		for _, i := range order {
			for len(h) >= 2 {
				a, b := h[len(h)-2], h[len(h)-1]
				if orient(xy[2*a], xy[2*a+1], xy[2*b], xy[2*b+1], xy[2*i], xy[2*i+1]) < 0 {
					h = h[:len(h)-1]
					continue
				}
				break
			}
			h = append(h, i)
		}
		return h
	}
	lower := chain(p)
	rev := make([]int, len(p))
	for i := range p {
		rev[i] = p[len(p)-1-i]
	}
	upper := chain(rev)
	return len(lower) + len(upper) - 2
}

// checkDelaunay checks a triangulation of points: every triangle counter-clockwise, no point
// strictly inside any triangle's circumcircle, each edge in at most two triangles, and the
// number of triangles Euler's formula gives (2n − h − 2, h the points on the hull).
func checkDelaunay(t *testing.T, xy []float64, n int, tr *Triangulation) {
	t.Helper()
	var unique []int
	for i := 0; i < n; i++ {
		if int(tr.Same[i]) == i {
			unique = append(unique, i)
		}
	}
	if want := 2*len(unique) - hullCount(xy, unique) - 2; len(tr.Triangles) != want {
		t.Errorf("%d triangles of %d points, not %d", len(tr.Triangles), len(unique), want)
	}
	edges := map[[2]int32]int{}
	for _, tri := range tr.Triangles {
		a, b, c := tri[0], tri[1], tri[2]
		if orient(xy[2*a], xy[2*a+1], xy[2*b], xy[2*b+1], xy[2*c], xy[2*c+1]) <= 0 {
			t.Fatalf("triangle %v is not counter-clockwise", tri)
		}
		for k := 0; k < 3; k++ {
			u, v := tri[k], tri[(k+1)%3]
			edges[[2]int32{min(u, v), max(u, v)}]++
		}
		for _, d := range unique {
			if incircle(xy[2*a], xy[2*a+1], xy[2*b], xy[2*b+1], xy[2*c], xy[2*c+1], xy[2*d], xy[2*d+1]) > 0 {
				t.Fatalf("point %d lies inside the circumcircle of %v", d, tri)
			}
		}
	}
	for e, k := range edges {
		if k > 2 {
			t.Fatalf("edge %v in %d triangles", e, k)
		}
	}
}

func TestDelaunayOfRandomPoints(t *testing.T) {
	s := random.New(7, 1)
	for _, n := range []int{3, 4, 10, 100, 2000} {
		xy := make([]float64, 2*n)
		for i := range xy {
			xy[i] = 1000 * s.Float64()
		}
		tr, err := Triangulate(context.Background(), xy, n)
		if err != nil {
			t.Fatal(err)
		}
		checkDelaunay(t, xy, n, tr)
	}
}

// A square lattice: every four points of a square lie on one circle.
func TestDelaunayOfALattice(t *testing.T) {
	for _, side := range []int{2, 3, 30} {
		n := side * side
		xy := make([]float64, 0, 2*n)
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				xy = append(xy, 12.5*float64(x), 12.5*float64(y))
			}
		}
		tr, err := Triangulate(context.Background(), xy, n)
		if err != nil {
			t.Fatal(err)
		}
		checkDelaunay(t, xy, n, tr)
		if want := 2 * (side - 1) * (side - 1); len(tr.Triangles) != want {
			t.Errorf("%d × %d lattice: %d triangles, not %d", side, side, len(tr.Triangles), want)
		}
		again, _ := Triangulate(context.Background(), xy, n)
		for i := range tr.Triangles {
			if tr.Triangles[i] != again.Triangles[i] {
				t.Fatal("the same points triangulated otherwise")
			}
		}
	}
}

func TestDelaunayOfDegeneratePoints(t *testing.T) {
	// On a line: no triangles, the points joined along it.
	line := []float64{3, 3, 1, 1, 2, 2, 0, 0}
	tr, err := Triangulate(context.Background(), line, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Collinear || len(tr.Triangles) != 0 || len(tr.Path) != 4 || tr.Path[0] != 3 || tr.Path[3] != 0 {
		t.Errorf("collinear: %+v", tr)
	}
	// Collinear points first, then one off the line; and duplicates.
	pts := []float64{0, 0, 1, 0, 2, 0, 3, 0, 1, 0, 1.5, 2, 1.5, 2, 1.5, -2}
	tr, err = Triangulate(context.Background(), pts, 8)
	if err != nil {
		t.Fatal(err)
	}
	checkDelaunay(t, pts, 8, tr)
	if tr.Same[4] != 1 || tr.Same[6] != 5 || tr.Same[7] != 7 {
		t.Errorf("duplicates: %v", tr.Same)
	}
	if _, err := Triangulate(context.Background(), []float64{0, math.NaN()}, 1); err == nil {
		t.Error("a point not finite accepted")
	}
}
