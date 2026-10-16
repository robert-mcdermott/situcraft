package graph

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"testing"
)

// The exact search against brute force, ties among equal distances ordered by index, and the
// same table for any number of workers.
func TestKNNIsExact(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	n, dim, k := 300, 5, 7
	x := make([]float64, n*dim)
	for i := range x {
		// Coarse values: many equal distances.
		x[i] = float64(r.IntN(4))
	}
	nb, err := KNN(context.Background(), x, n, dim, k, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		type c struct {
			d float64
			j int
		}
		var all []c
		for j := 0; j < n; j++ {
			if j == i {
				continue
			}
			d := 0.0
			for s := 0; s < dim; s++ {
				d += (x[i*dim+s] - x[j*dim+s]) * (x[i*dim+s] - x[j*dim+s])
			}
			all = append(all, c{d, j})
		}
		sort.Slice(all, func(a, b int) bool { return all[a].d < all[b].d || all[a].d == all[b].d && all[a].j < all[b].j })
		for s := 0; s < k; s++ {
			if int(nb.Index[i*k+s]) != all[s].j || nb.Distances[i*k+s] != math.Sqrt(all[s].d) {
				t.Fatalf("point %d, neighbor %d: %d at %g, brute force %d at %g", i, s, nb.Index[i*k+s], nb.Distances[i*k+s], all[s].j, math.Sqrt(all[s].d))
			}
		}
	}
	many, _ := KNN(context.Background(), x, n, dim, k, 8, nil)
	for i := range nb.Index {
		if nb.Index[i] != many.Index[i] || nb.Distances[i] != many.Distances[i] {
			t.Fatal("eight workers found another table")
		}
	}
	if _, err := KNN(context.Background(), x, n, dim, n, 1, nil); err == nil {
		t.Error("n neighbors among n points")
	}
}

// The fuzzy weights of a small table, computed by hand: three points on a line at 0, 1 and 3.
func TestFuzzyWeightsByHand(t *testing.T) {
	nb := &Neighbors{N: 3, K: 2,
		Index:     []int32{1, 2, 0, 2, 1, 0},
		Distances: []float64{1, 3, 1, 2, 2, 3}}
	g, err := Fuzzy(nb)
	if err != nil {
		t.Fatal(err)
	}
	// Each point's σ solves 1 + exp(−(d₂ − ρ)/σ) = log₂ 3 (its nearest within ρ counts 1).
	target := math.Log2(3)
	sigma := func(gap float64) float64 { return -gap / math.Log(target-1) }
	w := func(gap float64) float64 { return math.Exp(-gap / sigma(gap)) }
	// Directed: 0→1 = 1, 0→2 = w(2); 1→0 = 1, 1→2 = w(1); 2→1 = 1, 2→0 = w(1).
	want := map[[2]int]float64{
		{0, 1}: 1, {0, 2}: w(2) + w(1) - w(2)*w(1), {1, 2}: w(1) + 1 - w(1),
	}
	if g.Edges() != 3 {
		t.Fatalf("%d edges", g.Edges())
	}
	for i := 0; i < 3; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			a, b := i, int(g.Targets[e])
			if a > b {
				a, b = b, a
			}
			if math.Abs(g.Weights[e]-want[[2]int{a, b}]) > 2e-5 {
				t.Errorf("edge %d–%d: %g, by hand %g", i, g.Targets[e], g.Weights[e], want[[2]int{a, b}])
			}
		}
	}
}

func fromEdges(n int, edges [][3]float64) *CSR {
	rows := make([][]struct {
		j int32
		w float64
	}, n)
	for _, e := range edges {
		i, j := int(e[0]), int(e[1])
		rows[i] = append(rows[i], struct {
			j int32
			w float64
		}{int32(j), e[2]})
		rows[j] = append(rows[j], struct {
			j int32
			w float64
		}{int32(i), e[2]})
	}
	g := &CSR{N: n, Offsets: make([]int, n+1)}
	for i, row := range rows {
		sort.Slice(row, func(a, b int) bool { return row[a].j < row[b].j })
		for _, e := range row {
			g.Targets = append(g.Targets, e.j)
			g.Weights = append(g.Weights, e.w)
		}
		g.Offsets[i+1] = len(g.Targets)
	}
	return g
}

// Two triangles joined by an edge: modularity 5/14 for the natural split, by hand.
func TestModularityByHand(t *testing.T) {
	g := fromEdges(6, [][3]float64{{0, 1, 1}, {1, 2, 1}, {0, 2, 1}, {3, 4, 1}, {4, 5, 1}, {3, 5, 1}, {2, 3, 1}})
	if q := Modularity(g, []int32{0, 0, 0, 1, 1, 1}, 1); math.Abs(q-5.0/14) > 1e-15 {
		t.Errorf("modularity %g, by hand 5/14", q)
	}
	p, err := Leiden(context.Background(), g, LeidenOptions{Resolution: 1, Randomness: 0.01, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(p.Community) != "[0 0 0 1 1 1]" || math.Abs(p.Modularity-5.0/14) > 1e-15 {
		t.Errorf("Leiden: %v, modularity %g", p.Community, p.Modularity)
	}
}

// Eight cliques of six in a ring (below modularity's resolution limit) are found exactly, and four
// planted groups of a random graph too; the partition is the same for a seed, digest recorded.
func TestLeidenFindsPlantedCommunities(t *testing.T) {
	var edges [][3]float64
	for c := 0; c < 8; c++ {
		for a := 0; a < 6; a++ {
			for b := a + 1; b < 6; b++ {
				edges = append(edges, [3]float64{float64(c*6 + a), float64(c*6 + b), 1})
			}
		}
		edges = append(edges, [3]float64{float64(c*6 + 5), float64((c + 1) % 8 * 6), 1})
	}
	ring := fromEdges(48, edges)
	p, err := Leiden(context.Background(), ring, LeidenOptions{Resolution: 1, Randomness: 0.01, Seed: 20250})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sizes) != 8 {
		t.Fatalf("%d communities in the ring of cliques: %v", len(p.Sizes), p.Sizes)
	}
	for v := range p.Community {
		if p.Community[v] != p.Community[v/6*6] {
			t.Fatalf("node %d is not with its clique", v)
		}
	}
	r := rand.New(rand.NewPCG(5, 6))
	edges = edges[:0]
	n := 200
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			p := 0.01
			if i/50 == j/50 {
				p = 0.3
			}
			if r.Float64() < p {
				edges = append(edges, [3]float64{float64(i), float64(j), 0.5 + r.Float64()})
			}
		}
	}
	planted := fromEdges(n, edges)
	p, err = Leiden(context.Background(), planted, LeidenOptions{Resolution: 1, Randomness: 0.01, Seed: 20250})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sizes) != 4 || p.Sizes[0] != 50 || p.Sizes[3] != 50 {
		t.Fatalf("planted groups found as %v", p.Sizes)
	}
	for v := range p.Community {
		if p.Community[v] != p.Community[v/50*50] {
			t.Fatalf("node %d is not with its group", v)
		}
	}
	if q := Modularity(planted, p.Community, 1); q != p.Modularity || q < 0.6 {
		t.Errorf("modularity %g, recorded %g", q, p.Modularity)
	}
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, p.Community)
	binary.Write(h, binary.LittleEndian, p.Modularity)
	// A random graph without structure: many small communities, a partition that depends on every
	// random draw, so its digest pins the algorithm on every processor.
	edges = edges[:0]
	for i := 0; i < 400; i++ {
		for s := 0; s < 4; s++ {
			if j := r.IntN(400); j != i {
				edges = append(edges, [3]float64{float64(i), float64(j), r.Float64()})
			}
		}
	}
	noise := fromEdges(400, dedupe(edges))
	q, _ := Leiden(context.Background(), noise, LeidenOptions{Resolution: 1, Randomness: 0.01, Seed: 7})
	binary.Write(h, binary.LittleEndian, q.Community)
	binary.Write(h, binary.LittleEndian, q.Modularity)
	if got := fmt.Sprintf("%x", h.Sum(nil))[:16]; got != leidenDigest {
		t.Errorf("digest %s, recorded %s (%d communities, modularity %g)", got, leidenDigest, len(q.Sizes), q.Modularity)
	}
}

func dedupe(edges [][3]float64) [][3]float64 {
	seen := map[[2]int]bool{}
	var out [][3]float64
	for _, e := range edges {
		a, b := int(e[0]), int(e[1])
		if a > b {
			a, b = b, a
		}
		if !seen[[2]int{a, b}] {
			seen[[2]int{a, b}] = true
			out = append(out, e)
		}
	}
	return out
}

const leidenDigest = "6510f8cc33e58ddd"

// The grid search finds exactly what the brute-force search finds, ties included.
func TestKNN2DIsKNN(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for _, n := range []int{20, 997, 4000} {
		xy := make([]float64, 2*n)
		for i := range xy {
			xy[i] = float64(r.IntN(60)) / 7 // coarse: ties
		}
		if n == 4000 {
			for i := 0; i < 100; i++ {
				xy[2*i] += 1000 // a far cluster
			}
		}
		k := min(25, n-1)
		want, _ := KNN(context.Background(), xy, n, 2, k, 4, nil)
		got, err := KNN2D(context.Background(), xy, n, k, 3)
		if err != nil {
			t.Fatal(err)
		}
		for i := range want.Index {
			if want.Index[i] != got.Index[i] || want.Distances[i] != got.Distances[i] {
				t.Fatalf("n %d: entry %d: grid %d at %g, brute force %d at %g", n, i, got.Index[i], got.Distances[i], want.Index[i], want.Distances[i])
			}
		}
	}
}
