// Package spatial is SituCraft's spatial analysis of a segmentation's cells: graphs of the cells'
// neighbors (this file), and the statistics computed on them.
//
// A graph joins cells that are neighbors by one of four definitions: their centroids within a
// radius; each cell's k nearest (an edge when either cell is among the other's k); the Delaunay
// triangulation of their centroids (delaunay.go), its edges up to a longest length; or contact,
// their pixels touching (or within a gap) in the segmentation's label raster (contact.go). Graphs
// are built over the cells chosen: a cell left out is not in the graph, and is no one's neighbor.
// Distances are between centroids, in µm, computed the same on every processor (each product
// rounded on its own, square roots exact), and a graph is the same for any number of workers.
package spatial

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"

	"situcraft/internal/graph"
	"situcraft/internal/segment"
)

// MethodVersion names this version of the graphs, as artifacts record it.
const MethodVersion = "1"

// The kinds of graph.
const (
	Delaunay = "delaunay"
	KNN      = "knn"
	Radius   = "radius"
	Contact  = "contact"
)

// Kinds lists them, in the order the page offers them.
var Kinds = []string{Delaunay, KNN, Radius, Contact}

// Params are a graph's definition: its kind and that kind's parameter.
type Params struct {
	Kind string `json:"kind"`
	// Longest: a Delaunay graph's longest edge, µm (0: every edge of the triangulation).
	Longest float64 `json:"longest,omitempty"`
	// K: a k-nearest graph's neighbors of each cell.
	K int `json:"k,omitempty"`
	// Radius: a radius graph's distance, µm.
	Radius float64 `json:"radius,omitempty"`
	// Gap: a contact graph's gap, µm (0: pixels touching).
	Gap float64 `json:"gap,omitempty"`
}

// DefaultParams are each kind's defaults.
func DefaultParams(kind string) Params {
	switch kind {
	case KNN:
		return Params{Kind: KNN, K: 6}
	case Radius:
		return Params{Kind: Radius, Radius: 20}
	case Contact:
		return Params{Kind: Contact, Gap: 0}
	}
	return Params{Kind: Delaunay, Longest: 50}
}

// Check says what is wrong with p, and clears the parameters of other kinds.
func (p *Params) Check() error {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	switch p.Kind {
	case Delaunay:
		if !finite(p.Longest) || p.Longest < 0 || p.Longest > 1000 {
			return errors.New("a Delaunay graph's longest edge is 0 (none) to 1,000 µm")
		}
		*p = Params{Kind: Delaunay, Longest: p.Longest}
	case KNN:
		if p.K < 1 || p.K > 100 {
			return errors.New("a k-nearest graph's k is 1 to 100")
		}
		*p = Params{Kind: KNN, K: p.K}
	case Radius:
		if !finite(p.Radius) || p.Radius <= 0 || p.Radius > 500 {
			return errors.New("a radius graph's radius is more than 0 and at most 500 µm")
		}
		*p = Params{Kind: Radius, Radius: p.Radius}
	case Contact:
		if !finite(p.Gap) || p.Gap < 0 || p.Gap > 20 {
			return errors.New("a contact graph's gap is 0 to 20 µm")
		}
		*p = Params{Kind: Contact, Gap: p.Gap}
	default:
		return fmt.Errorf("no graph of kind %q (delaunay, knn, radius or contact)", p.Kind)
	}
	return nil
}

// Inputs are the cells a graph is built over.
type Inputs struct {
	// X, Y are every cell's centroid (µm), in the segmentation's order.
	X, Y []float32
	// Keep chooses the cells (nil: all of them).
	Keep []bool
	// Mask is the segmentation's label raster (label k is cell k-1), for contact graphs.
	Mask *segment.Raster
}

// Graph is a graph of cells: the cells in it (rows of the segmentation, ascending) with their
// centroids, and each one's neighbors (indices among them, ascending) in compressed rows, with
// each edge's length and, in a contact graph, the boundary the two cells share.
type Graph struct {
	Rows      []uint32
	X, Y      []float32
	Offsets   []uint32 // len(Rows)+1
	Neighbors []uint32
	Length    []float32 // µm, between centroids
	Shared    []float32 // µm of boundary shared (contact graphs; nil otherwise)
	// Duplicates counts cells at the same place as an earlier one (Delaunay graphs): each is joined
	// to that cell, and to its neighbors.
	Duplicates int
	// Collinear: every cell's centroid lies on one line (Delaunay graphs), so the graph joins them
	// along it.
	Collinear bool
}

// Edges counts the graph's edges (each joining two cells once).
func (g *Graph) Edges() int { return len(g.Neighbors) / 2 }

// distance is the distance between two centroids, the same on every processor.
func distance(x0, y0, x1, y1 float32) float64 {
	dx, dy := float64(x1)-float64(x0), float64(y1)-float64(y0)
	return math.Sqrt(mul(dx, dx) + mul(dy, dy))
}

// Build builds the graph p defines over the cells chosen.
func Build(ctx context.Context, in Inputs, p Params, workers int, progress func(float64, string)) (*Graph, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(in.X) != len(in.Y) || in.Keep != nil && len(in.Keep) != len(in.X) {
		return nil, errors.New("spatial: the cells' centroids and choice differ in length")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	if progress == nil {
		progress = func(float64, string) {}
	}
	g := &Graph{}
	for i := range in.X {
		if in.Keep == nil || in.Keep[i] {
			if math.IsNaN(float64(in.X[i])) || math.IsNaN(float64(in.Y[i])) || math.IsInf(float64(in.X[i]), 0) || math.IsInf(float64(in.Y[i]), 0) {
				return nil, fmt.Errorf("spatial: cell %d has no finite centroid", i)
			}
			g.Rows = append(g.Rows, uint32(i))
			g.X = append(g.X, in.X[i])
			g.Y = append(g.Y, in.Y[i])
		}
	}
	var pairs []pair
	var err error
	switch p.Kind {
	case Delaunay:
		progress(0.1, "Triangulating the cells' centroids")
		pairs, err = delaunayPairs(ctx, g, p.Longest)
	case KNN:
		progress(0.1, "Finding each cell's nearest neighbors")
		pairs, err = knnPairs(ctx, g, p.K, workers)
	case Radius:
		progress(0.1, "Finding the cells within the radius")
		pairs, err = radiusPairs(ctx, g, p.Radius, workers)
	case Contact:
		if in.Mask == nil {
			return nil, errors.New("spatial: a contact graph needs the segmentation's label raster")
		}
		progress(0.1, "Finding the cells that touch")
		pairs, err = contactPairs(ctx, in, g, p.Gap, workers)
	}
	if err != nil {
		return nil, err
	}
	progress(0.8, "Gathering the edges")
	g.assemble(pairs, p.Kind == Contact)
	return g, nil
}

// pair is an edge between cells i < j (indices among the graph's), with the boundary they share
// (contact graphs).
type pair struct {
	i, j   uint32
	shared float64
}

// assemble turns pairs (each edge once, in any order, perhaps repeated) into compressed rows.
func (g *Graph) assemble(pairs []pair, shared bool) {
	sort.Slice(pairs, func(a, b int) bool {
		if pairs[a].i != pairs[b].i {
			return pairs[a].i < pairs[b].i
		}
		return pairs[a].j < pairs[b].j
	})
	unique := pairs[:0]
	for _, e := range pairs {
		if k := len(unique); k > 0 && unique[k-1].i == e.i && unique[k-1].j == e.j {
			unique[k-1].shared += e.shared
			continue
		}
		unique = append(unique, e)
	}
	n := len(g.Rows)
	degree := make([]uint32, n+1)
	for _, e := range unique {
		degree[e.i+1]++
		degree[e.j+1]++
	}
	g.Offsets = make([]uint32, n+1)
	for i := 0; i < n; i++ {
		g.Offsets[i+1] = g.Offsets[i] + degree[i+1]
	}
	g.Neighbors = make([]uint32, 2*len(unique))
	g.Length = make([]float32, 2*len(unique))
	if shared {
		g.Shared = make([]float32, 2*len(unique))
	}
	at := append([]uint32(nil), g.Offsets[:n]...)
	put := func(i, j uint32, s float64) {
		k := at[i]
		at[i]++
		g.Neighbors[k] = j
		g.Length[k] = float32(distance(g.X[i], g.Y[i], g.X[j], g.Y[j]))
		if shared {
			g.Shared[k] = float32(s)
		}
	}
	// In order of i, each cell's neighbors below it come first (from pairs where it is j), then
	// those above: put the lower ones first by walking the pairs twice.
	byJ := append([]pair(nil), unique...)
	sort.Slice(byJ, func(a, b int) bool {
		if byJ[a].j != byJ[b].j {
			return byJ[a].j < byJ[b].j
		}
		return byJ[a].i < byJ[b].i
	})
	for _, e := range byJ {
		put(e.j, e.i, e.shared)
	}
	for _, e := range unique {
		put(e.i, e.j, e.shared)
	}
}

func delaunayPairs(ctx context.Context, g *Graph, longest float64) ([]pair, error) {
	n := len(g.Rows)
	xy := make([]float64, 2*n)
	for i := 0; i < n; i++ {
		xy[2*i], xy[2*i+1] = float64(g.X[i]), float64(g.Y[i])
	}
	tr, err := Triangulate(ctx, xy, n)
	if err != nil {
		return nil, err
	}
	var pairs []pair
	edge := func(a, b int32) {
		if a == b {
			return
		}
		if longest > 0 && distance(g.X[a], g.Y[a], g.X[b], g.Y[b]) > longest {
			return
		}
		pairs = append(pairs, pair{i: uint32(min(a, b)), j: uint32(max(a, b))})
	}
	if tr.Collinear {
		g.Collinear = n > 2
		for k := 1; k < len(tr.Path); k++ {
			edge(tr.Path[k-1], tr.Path[k])
		}
	}
	for _, t := range tr.Triangles {
		edge(t[0], t[1])
		edge(t[1], t[2])
		edge(t[2], t[0])
	}
	// Cells at the same place as an earlier one: joined to it, to each other and to its neighbors.
	groups := map[int32][]int32{}
	for i, s := range tr.Same {
		if int(s) != i {
			groups[s] = append(groups[s], int32(i))
			g.Duplicates++
		}
	}
	if len(groups) > 0 {
		neighbors := map[int32][]int32{}
		for _, e := range pairs {
			neighbors[int32(e.i)] = append(neighbors[int32(e.i)], int32(e.j))
			neighbors[int32(e.j)] = append(neighbors[int32(e.j)], int32(e.i))
		}
		for s, dups := range groups {
			all := append([]int32{s}, dups...)
			for a := range all {
				for b := a + 1; b < len(all); b++ {
					edge(all[a], all[b])
				}
			}
			for _, d := range dups {
				for _, nb := range neighbors[s] {
					edge(d, nb)
				}
			}
		}
	}
	return pairs, nil
}

func knnPairs(ctx context.Context, g *Graph, k, workers int) ([]pair, error) {
	n := len(g.Rows)
	if n < 2 {
		return nil, nil
	}
	k = min(k, n-1)
	xy := make([]float64, 2*n)
	for i := 0; i < n; i++ {
		xy[2*i], xy[2*i+1] = float64(g.X[i]), float64(g.Y[i])
	}
	nb, err := graph.KNN2D(ctx, xy, n, k, workers)
	if err != nil {
		return nil, err
	}
	pairs := make([]pair, 0, n*k)
	for i := 0; i < n; i++ {
		for _, j := range nb.Index[i*nb.K : (i+1)*nb.K] {
			if int(j) != i {
				pairs = append(pairs, pair{i: uint32(min(i, int(j))), j: uint32(max(i, int(j)))})
			}
		}
	}
	return pairs, nil
}

// radiusPairs finds the cells within r of each other through a grid of squares of side r.
func radiusPairs(ctx context.Context, g *Graph, r float64, workers int) ([]pair, error) {
	n := len(g.Rows)
	if n < 2 {
		return nil, nil
	}
	x0, y0 := math.Inf(1), math.Inf(1)
	for i := 0; i < n; i++ {
		x0, y0 = math.Min(x0, float64(g.X[i])), math.Min(y0, float64(g.Y[i]))
	}
	type key struct{ x, y int64 }
	square := func(i int) key {
		return key{int64(math.Floor((float64(g.X[i]) - x0) / r)), int64(math.Floor((float64(g.Y[i]) - y0) / r))}
	}
	grid := map[key][]uint32{}
	for i := 0; i < n; i++ {
		k := square(i)
		grid[k] = append(grid[k], uint32(i))
	}
	found := make([][]pair, workers)
	errs := make([]error, workers)
	parallelFor(n, workers, func(w, a, b int) {
		for i := a; i < b; i++ {
			if (i-a)%4096 == 0 && ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			k := square(i)
			for dy := int64(-1); dy <= 1; dy++ {
				for dx := int64(-1); dx <= 1; dx++ {
					for _, j := range grid[key{k.x + dx, k.y + dy}] {
						if int(j) > i && distance(g.X[i], g.Y[i], g.X[j], g.Y[j]) <= r {
							found[w] = append(found[w], pair{i: uint32(i), j: j})
						}
					}
				}
			}
		}
	})
	var pairs []pair
	for w := range found {
		if errs[w] != nil {
			return nil, errs[w]
		}
		pairs = append(pairs, found[w]...)
	}
	return pairs, nil
}

// parallelFor splits 0..n-1 among workers in contiguous ranges, calling do(worker, a, b).
func parallelFor(n, workers int, do func(w, a, b int)) {
	if workers > n {
		workers = max(1, n)
	}
	done := make(chan struct{}, workers)
	for w := 0; w < workers; w++ {
		a, b := w*n/workers, (w+1)*n/workers
		go func(w, a, b int) {
			do(w, a, b)
			done <- struct{}{}
		}(w, a, b)
	}
	for w := 0; w < workers; w++ {
		<-done
	}
}
