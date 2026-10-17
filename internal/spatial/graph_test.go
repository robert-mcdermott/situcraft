package spatial

import (
	"context"
	"math"
	"sort"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/random"
	"situcraft/internal/segment"
)

func lattice(side int, spacing float32, hex bool) (x, y []float32) {
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			px := spacing * float32(c)
			py := spacing * float32(r)
			if hex {
				px += spacing / 2 * float32(r%2)
				py = spacing * float32(math.Sqrt(3)/2) * float32(r)
			}
			x = append(x, px)
			y = append(y, py)
		}
	}
	return x, y
}

func build(t *testing.T, in Inputs, p Params, workers int) *Graph {
	t.Helper()
	g, err := Build(context.Background(), in, p, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func degreeOf(g *Graph, i int) int { return int(g.Offsets[i+1] - g.Offsets[i]) }

// interiorDegrees counts the cells of a side × side lattice at least margin cells from its edge, by
// degree.
func interiorDegrees(g *Graph, side, margin int) map[int]int {
	out := map[int]int{}
	for r := margin; r < side-margin; r++ {
		for c := margin; c < side-margin; c++ {
			out[degreeOf(g, r*side+c)]++
		}
	}
	return out
}

// Each kind on lattices, by hand: a square lattice of 10 µm joins each inner cell to its 4 nearest
// at a radius of 10 µm and to 8 at 15 µm; Delaunay up to 10.5 µm keeps the lattice's sides (4), and
// with every edge, each square split by one diagonal; a hexagonal lattice's 6 nearest.
func TestGraphsOfLattices(t *testing.T) {
	const side = 12
	x, y := lattice(side, 10, false)
	in := Inputs{X: x, Y: y}
	inner := (side - 2) * (side - 2)
	for _, c := range []struct {
		p    Params
		want map[int]int
	}{
		{Params{Kind: Radius, Radius: 10}, map[int]int{4: inner}},
		{Params{Kind: Radius, Radius: 15}, map[int]int{8: inner}},
		{Params{Kind: Delaunay, Longest: 10.5}, map[int]int{4: inner}},
	} {
		g := build(t, in, c.p, 3)
		got := interiorDegrees(g, side, 1)
		if len(got) != len(c.want) || got[4] != c.want[4] || got[8] != c.want[8] {
			t.Errorf("%+v: inner degrees %v, not %v", c.p, got, c.want)
		}
		for k, l := range g.Length {
			if c.p.Kind != Radius || c.p.Radius == 10 {
				if l != 10 {
					t.Fatalf("%+v: an edge %d of %g µm", c.p, k, l)
				}
			}
		}
	}
	g := build(t, in, Params{Kind: Delaunay}, 1)
	if want := 2*side*(side-1) + (side-1)*(side-1); g.Edges() != want {
		t.Errorf("the lattice's triangulation has %d edges, not %d", g.Edges(), want)
	}
	// Cells on the lattice's edge lack neighbors and reach inward for their 6 nearest, joining cells
	// up to two rows in (an edge when either is among the other's k): those three rows in have 6.
	hx, hy := lattice(side, 10, true)
	g = build(t, Inputs{X: hx, Y: hy}, Params{Kind: KNN, K: 6}, 2)
	if got := interiorDegrees(g, side, 3); got[6] != (side-6)*(side-6) {
		t.Errorf("the hexagonal lattice's 6 nearest: inner degrees %v", got)
	}
	d := Describe(g, Side{ID: "seg", Name: "Lattice"}, side*side, Params{Kind: KNN, K: 6}, Choice{})
	if d.Components.Count != 1 || d.Components.Largest != side*side || d.Isolated != 0 || math.Abs(d.Lengths.Median-10) > 1e-5 {
		t.Errorf("the description: %+v", d)
	}
}

// Radius and k-nearest graphs against an exhaustive search, on random cells, some of them left
// out; Delaunay edges up to a length are the triangulation's.
func TestGraphsAgainstAnExhaustiveSearch(t *testing.T) {
	s := random.New(11, 2)
	n := 1500
	x, y := make([]float32, n), make([]float32, n)
	keep := make([]bool, n)
	for i := 0; i < n; i++ {
		x[i], y[i] = float32(400*s.Float64()), float32(300*s.Float64())
		keep[i] = s.Float64() < 0.9
	}
	in := Inputs{X: x, Y: y, Keep: keep}
	var rows []int
	for i, k := range keep {
		if k {
			rows = append(rows, i)
		}
	}
	m := len(rows)
	dist := func(a, b int) float64 { return distance(x[rows[a]], y[rows[a]], x[rows[b]], y[rows[b]]) }
	edgesOf := func(g *Graph) map[[2]int]bool {
		out := map[[2]int]bool{}
		for i := 0; i < len(g.Rows); i++ {
			for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
				out[[2]int{i, int(g.Neighbors[k])}] = true
			}
		}
		return out
	}
	same := func(name string, got, want map[[2]int]bool) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: %d edge ends, not %d", name, len(got), len(want))
			return
		}
		for e := range want {
			if !got[e] {
				t.Errorf("%s: edge %v missing", name, e)
				return
			}
		}
	}
	g := build(t, in, Params{Kind: Radius, Radius: 17}, 4)
	if len(g.Rows) != m {
		t.Fatalf("%d cells in the graph, not %d", len(g.Rows), m)
	}
	want := map[[2]int]bool{}
	for a := 0; a < m; a++ {
		for b := 0; b < m; b++ {
			if a != b && dist(a, b) <= 17 {
				want[[2]int{a, b}] = true
			}
		}
	}
	same("radius", edgesOf(g), want)

	g = build(t, in, Params{Kind: KNN, K: 5}, 4)
	want = map[[2]int]bool{}
	for a := 0; a < m; a++ {
		order := make([]int, 0, m-1)
		for b := 0; b < m; b++ {
			if b != a {
				order = append(order, b)
			}
		}
		sort.Slice(order, func(i, j int) bool {
			di, dj := dist(a, order[i]), dist(a, order[j])
			return di < dj || di == dj && order[i] < order[j]
		})
		for _, b := range order[:5] {
			want[[2]int{a, b}], want[[2]int{b, a}] = true, true
		}
	}
	same("k nearest", edgesOf(g), want)

	g = build(t, in, Params{Kind: Delaunay, Longest: 20}, 1)
	xy := make([]float64, 2*m)
	for a, r := range rows {
		xy[2*a], xy[2*a+1] = float64(x[r]), float64(y[r])
	}
	tr, err := Triangulate(context.Background(), xy, m)
	if err != nil {
		t.Fatal(err)
	}
	want = map[[2]int]bool{}
	for _, tri := range tr.Triangles {
		for k := 0; k < 3; k++ {
			a, b := int(tri[k]), int(tri[(k+1)%3])
			if dist(a, b) <= 20 {
				want[[2]int{a, b}], want[[2]int{b, a}] = true, true
			}
		}
	}
	same("Delaunay", edgesOf(g), want)
}

// raster makes a label raster from rows of labels.
func raster(rows [][]uint32, pixel float64) *segment.Raster {
	r := &segment.Raster{Width: len(rows[0]), Height: len(rows), PixelSize: pixel}
	for _, row := range rows {
		r.Rows = append(r.Rows, segment.EncodeRow(row))
	}
	return r
}

// Contact on rasters drawn by hand: the pixel edges between each pair of cells, side by side or
// one above the other; a gap of two empty pixels closed by a gap of two pixels' width.
func TestContactOnRastersByHand(t *testing.T) {
	mask := raster([][]uint32{
		{1, 1, 2, 2, 0, 0, 4},
		{1, 1, 2, 2, 0, 0, 4},
		{0, 3, 3, 0, 0, 0, 4},
	}, 0.5)
	x := []float32{0.5, 1.5, 1, 3.5}
	y := []float32{0.5, 0.5, 1.25, 0.75}
	g := build(t, Inputs{X: x, Y: y, Mask: mask}, Params{Kind: Contact}, 2)
	shared := map[[2]uint32]float32{}
	for i := 0; i < len(g.Rows); i++ {
		for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
			shared[[2]uint32{uint32(i), g.Neighbors[k]}] = g.Shared[k]
		}
	}
	want := map[[2]uint32]float32{{0, 1}: 1, {1, 0}: 1, {0, 2}: 0.5, {2, 0}: 0.5, {1, 2}: 0.5, {2, 1}: 0.5}
	if len(shared) != len(want) {
		t.Fatalf("contacts %v, not %v", shared, want)
	}
	for e, v := range want {
		if shared[e] != v {
			t.Errorf("cells %v share %g µm, not %g", e, shared[e], v)
		}
	}
	// Cell 2 grown by one pixel meets cell 4 across the two empty pixels; cell 3 left out still
	// stands where it is.
	g = build(t, Inputs{X: x, Y: y, Mask: mask, Keep: []bool{true, true, false, true}}, Params{Kind: Contact, Gap: 1}, 1)
	if g.Edges() != 2 || degreeOf(g, 2) != 1 || degreeOf(g, 0) != 1 {
		t.Errorf("with a gap: %d edges, degrees %d %d %d", g.Edges(), degreeOf(g, 0), degreeOf(g, 1), degreeOf(g, 2))
	}
	if _, err := Build(context.Background(), Inputs{X: x[:2], Y: y[:2], Mask: mask}, Params{Kind: Contact}, 1, nil); err == nil {
		t.Error("labels beyond the cells accepted")
	}
}

// digestOf is the SHA-256 of a graph's files, written.
func digestOf(t *testing.T, g *Graph, p Params) string {
	t.Helper()
	dir := t.TempDir()
	if err := Write(dir, g, Describe(g, Side{ID: "seg-x", Name: "X"}, len(g.Rows), p, Choice{Checksum: "c"})); err != nil {
		t.Fatal(err)
	}
	h, err := Digest(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &artifacts.Manifest{}
	if findings := Check(dir, m); len(findings) != 0 {
		t.Errorf("%s: %v", p.Kind, findings)
	}
	return h[:16]
}

// pinnedGraphs are the four kinds' files on the cells below, the same on every processor: a change
// here is a change to the graphs, which must be deliberate.
var pinnedGraphs = map[string]string{Delaunay: "c7629d3c9b0250d1", KNN: "d7ccb129beda8b2c", Radius: "ba4fbb14b086bc54", Contact: "5404b7edc7ce0a63"}

// Every kind's files are the same on one worker and on eight, and pass their check.
func TestGraphsAreTheSameOnAnyWorkers(t *testing.T) {
	s := random.New(5, 3)
	n := 3000
	x, y := make([]float32, n), make([]float32, n)
	for i := range x {
		x[i], y[i] = float32(500*s.Float64()), float32(500*s.Float64())
	}
	rows := make([][]uint32, 100)
	for r := range rows {
		rows[r] = make([]uint32, 100)
		for c := range rows[r] {
			if (r/5+c/5)%3 != 0 {
				rows[r][c] = uint32(1 + (r/5*20+c/5)%n)
			}
		}
	}
	in := Inputs{X: x, Y: y, Mask: raster(rows, 0.25)}
	for _, kind := range Kinds {
		p := DefaultParams(kind)
		one := digestOf(t, build(t, in, p, 1), p)
		eight := digestOf(t, build(t, in, p, 8), p)
		if one != eight {
			t.Errorf("%s: %s on one worker, %s on eight", kind, one, eight)
		}
		if want, ok := pinnedGraphs[kind]; ok && one != want {
			t.Errorf("%s: the files' digest is %s, not %s", kind, one, want)
		}
		t.Logf("%s: %s", kind, one)
	}
}

func TestParamsAreChecked(t *testing.T) {
	for _, p := range []Params{{Kind: "voronoi"}, {Kind: KNN, K: 0}, {Kind: Radius}, {Kind: Radius, Radius: math.NaN()}, {Kind: Contact, Gap: -1}, {Kind: Delaunay, Longest: -1}} {
		if err := p.Check(); err == nil {
			t.Errorf("%+v accepted", p)
		}
	}
	p := Params{Kind: KNN, K: 6, Radius: 30}
	if err := p.Check(); err != nil || p.Radius != 0 {
		t.Errorf("another kind's parameter kept: %+v %v", p, err)
	}
}
