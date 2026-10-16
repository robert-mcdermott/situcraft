package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/graph"
	"situcraft/internal/random"
	"situcraft/internal/segment"
)

// The curve's parameters, as umap-learn 0.5.12's find_ab_params gives them (scipy's curve_fit).
func TestFindAB(t *testing.T) {
	for _, c := range []struct{ spread, minDist, a, b float64 }{
		{1, 0.1, 1.5769434603113077, 0.8950608779109733},
		{1, 0.5, 0.5830300207571723, 1.3341669921303172},
	} {
		a, b := FindAB(c.spread, c.minDist)
		if math.Abs(a-c.a) > 1e-5 || math.Abs(b-c.b) > 1e-5 {
			t.Errorf("spread %g, min_dist %g: a %.10f, b %.10f; umap-learn %.10f, %.10f", c.spread, c.minDist, a, b, c.a, c.b)
		}
	}
}

// A ring of n nodes: its Laplacian's first two nontrivial eigenvectors are cos and sin of the angle,
// so the spectral layout puts the nodes on a circle, in order.
func TestSpectralOfARing(t *testing.T) {
	n := 60
	g := &graph.CSR{N: n, Offsets: make([]int, n+1)}
	for i := 0; i < n; i++ {
		a, b := (i+n-1)%n, (i+1)%n
		if a > b {
			a, b = b, a
		}
		g.Targets = append(g.Targets, int32(a), int32(b))
		g.Weights = append(g.Weights, 1, 1)
		g.Offsets[i+1] = len(g.Targets)
	}
	xy, ok := Spectral(context.Background(), g, 2, 7)
	if !ok {
		t.Fatal("no spectral layout of a ring")
	}
	r0 := math.Hypot(xy[0], xy[1])
	for i := 0; i < n; i++ {
		if r := math.Hypot(xy[2*i], xy[2*i+1]); math.Abs(r-r0) > 1e-3*r0 {
			t.Fatalf("node %d at radius %g, node 0 at %g", i, r, r0)
		}
		// Neighbors a 6° step apart.
		j := (i + 1) % n
		cos := (xy[2*i]*xy[2*j] + xy[2*i+1]*xy[2*j+1]) / (r0 * r0)
		if math.Abs(cos-math.Cos(2*math.Pi/float64(n))) > 1e-3 {
			t.Fatalf("nodes %d and %d: cos %g", i, j, cos)
		}
	}
	// Two rings are in pieces.
	two := &graph.CSR{N: 2 * n, Offsets: make([]int, 2*n+1)}
	for i := 0; i < 2*n; i++ {
		base := i / n * n
		a, b := base+(i-base+n-1)%n, base+(i-base+1)%n
		if a > b {
			a, b = b, a
		}
		two.Targets = append(two.Targets, int32(a), int32(b))
		two.Weights = append(two.Weights, 1, 1)
		two.Offsets[i+1] = len(two.Targets)
	}
	if _, ok := Spectral(context.Background(), two, 2, 7); ok {
		t.Error("a spectral layout of a graph in pieces")
	}
}

// clusters: points in dim dimensions around four centers, with their exact neighbors' fuzzy graph.
func clusters(n, dim int, seed uint64) ([]float64, *graph.CSR, []int) {
	return clustersAt(n, dim, seed, 8)
}

func clustersAt(n, dim int, seed uint64, far float64) ([]float64, *graph.CSR, []int) {
	s := random.New(seed, 0)
	x := make([]float64, n*dim)
	label := make([]int, n)
	for i := 0; i < n; i++ {
		label[i] = i % 4
		for d := 0; d < dim; d++ {
			center := 0.0
			if d == label[i] {
				center = far
			}
			x[i*dim+d] = center + s.Normal()
		}
	}
	nb, _ := graph.KNN(context.Background(), x, n, dim, 14, 4, nil)
	g, _ := graph.Fuzzy(nb)
	return x, g, label
}

// A map of four well-separated groups keeps them apart and its neighborhoods; it is the same for a
// seed (digest pinned for arm64 and amd64) and another for another seed.
func TestLayoutKeepsGroups(t *testing.T) {
	n, dim := 800, 10
	x, g, label := clusters(n, dim, 3)
	// The groups share no neighbor, so the graph is in pieces: the map starts from the points'
	// first two coordinates, given in place of the principal components.
	fallback := make([]float64, 2*n)
	for i := 0; i < n; i++ {
		fallback[2*i], fallback[2*i+1] = x[i*dim], x[i*dim+1]
	}
	xy, info, err := Layout(context.Background(), g, fallback, DefaultParams(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Init != "principal components (the graph is in pieces)" || info.Epochs != 500 {
		t.Errorf("init %q, epochs %d", info.Init, info.Epochs)
	}
	// Every point nearer its own group's center on the map than any other's.
	var cx, cy [4]float64
	for i := 0; i < n; i++ {
		cx[label[i]] += xy[2*i] / float64(n/4)
		cy[label[i]] += xy[2*i+1] / float64(n/4)
	}
	for i := 0; i < n; i++ {
		best, at := math.Inf(1), -1
		for c := 0; c < 4; c++ {
			if d := math.Hypot(xy[2*i]-cx[c], xy[2*i+1]-cy[c]); d < best {
				best, at = d, c
			}
		}
		if at != label[i] {
			t.Fatalf("point %d of group %d lies nearer group %d's center", i, label[i], at)
		}
	}
	q, err := Assess(context.Background(), x, dim, xy, n, 4)
	if err != nil {
		t.Fatal(err)
	}
	if q.Trustworthiness < 0.85 || q.Queries != n {
		t.Errorf("trustworthiness %g over %d queries", q.Trustworthiness, q.Queries)
	}
	// Groups near enough to share neighbors: one graph, a spectral start.
	_, near, _ := clustersAt(n, dim, 4, 2)
	nearXY, nearInfo, err := Layout(context.Background(), near, nil, DefaultParams(), 0, nil)
	if err != nil || nearInfo.Init != "spectral" {
		t.Fatalf("a connected graph: %v, init %q", err, nearInfo.Init)
	}
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, nearXY)
	binary.Write(h, binary.LittleEndian, xy)
	binary.Write(h, binary.LittleEndian, q.Score)
	binary.Write(h, binary.LittleEndian, q.Trustworthiness)
	if got := fmt.Sprintf("%x", h.Sum(nil))[:16]; got != layoutDigest {
		t.Errorf("digest %s, recorded %s", got, layoutDigest)
	}
	p := DefaultParams()
	p.Seed++
	other, _, _ := Layout(context.Background(), g, fallback, p, 0, nil)
	same := true
	for i := range xy {
		same = same && xy[i] == other[i]
	}
	if same {
		t.Error("another seed, the same map")
	}
}

// Trustworthiness, by its definition, against a direct computation over every point; the map that
// is its own space scores 1.
func TestTrustworthinessByDefinition(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	n, dim, k := 300, 5, 15
	high := make([]float64, n*dim)
	for i := range high {
		high[i] = r.NormFloat64()
	}
	low := make([]float64, 2*n)
	for i := 0; i < n; i++ {
		low[2*i], low[2*i+1] = high[i*dim]+0.3*r.NormFloat64(), high[i*dim+1]+0.3*r.NormFloat64()
	}
	q, err := Assess(context.Background(), high, dim, low, n, 3)
	if err != nil {
		t.Fatal(err)
	}
	dist := func(x []float64, d, i, j int) float64 {
		s := 0.0
		for c := 0; c < d; c++ {
			s += (x[i*d+c] - x[j*d+c]) * (x[i*d+c] - x[j*d+c])
		}
		return s
	}
	order := func(x []float64, d, i int) []int {
		var o []int
		for j := 0; j < n; j++ {
			if j != i {
				o = append(o, j)
			}
		}
		sort.SliceStable(o, func(a, b int) bool { return dist(x, d, i, o[a]) < dist(x, d, i, o[b]) })
		return o
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		hi := order(high, dim, i)
		rank := map[int]int{}
		for r, j := range hi {
			rank[j] = r + 1
		}
		for _, j := range order(low, 2, i)[:k] {
			if rank[j] > k {
				sum += float64(rank[j] - k)
			}
		}
	}
	want := 1 - 2/float64(n*k*(2*n-3*k-1))*sum
	if math.Abs(q.Trustworthiness-want) > 1e-12 {
		t.Errorf("trustworthiness %.15f, by definition %.15f", q.Trustworthiness, want)
	}
	self := make([]float64, 2*n)
	copy(self, low)
	q, _ = Assess(context.Background(), self, 2, self, n, 3)
	if q.Trustworthiness != 1 || q.Precision != 1 || q.Recall != 1 {
		t.Errorf("the map that is its own space: %g, %g, %g", q.Trustworthiness, q.Precision, q.Recall)
	}
}

// A map made from a clustering's files is written, read, checked and digested; a broken place is
// caught.
func TestMapFiles(t *testing.T) {
	counts := &segment.Counts{Offsets: []uint32{0}}
	s := random.New(5, 0)
	for g := 0; g < 30; g++ {
		counts.Genes = append(counts.Genes, fmt.Sprintf("G%d", g))
	}
	for i := 0; i < 600; i++ {
		for g := 0; g < 30; g++ {
			mean := 0.3
			if g%3 == i%3 {
				mean = 3
			}
			if c := s.NegativeBinomial(mean, 2); c > 0 {
				counts.Gene = append(counts.Gene, uint32(g))
				counts.Count = append(counts.Count, uint32(c))
			}
		}
		counts.Offsets = append(counts.Offsets, uint32(len(counts.Gene)))
	}
	p := cluster.DefaultParams()
	p.Components = 10
	res, err := cluster.Run(context.Background(), cluster.Inputs{Counts: counts}, p, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	cdir := t.TempDir()
	if err := cluster.Write(cdir, cluster.Side{ID: "seg-1", Name: "Simulated"}, nil, res); err != nil {
		t.Fatal(err)
	}
	r, err := Run(context.Background(), cdir, cluster.Side{ID: "clu-1", Name: "Clusters"}, DefaultParams(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Write(dir, r); err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	if f := Check(dir, &m); len(f) != 0 {
		t.Fatalf("check: %+v", f)
	}
	mp, err := ReadMap(dir)
	if err != nil || len(mp.Cells) != len(res.Cells) || len(mp.XY) != 2*len(res.Cells) {
		t.Fatalf("read back: %v", err)
	}
	if _, err := Digest(dir); err != nil {
		t.Fatal(err)
	}
	mp.XY[3] = float32(math.NaN())
	var b segment.Buffer
	b.U32(uint32(len(mp.Cells)))
	b.U32s(mp.Cells)
	b.F32s(mp.XY)
	b.F32s(mp.Score)
	segment.WriteBinary(filepath.Join(dir, FileMap), "SCEB", &b)
	if f := Check(dir, &m); len(f) == 0 {
		t.Error("a place that is not a number passed the check")
	}
}

const layoutDigest = "9668221e199e337f"

// batchedDigest is the batched map's, the same on every processor.
const batchedDigest = "38e9588483590357"

// The batched optimization (more than graph.ExactLimit cells) on a graph of 3,000 cells in four
// groups that share neighbors: the same map for one worker and four, and as faithful as the
// optimization on one thread (trustworthiness within 0.02).
func TestBatchedOptimization(t *testing.T) {
	n, dim := 3000, 10
	x, g, _ := clustersAt(n, dim, 8, 2)
	sequential, info, err := Layout(context.Background(), g, nil, DefaultParams(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The batched epochs from the layout's spectral start.
	run := func(workers int) []float64 {
		p := DefaultParams()
		rnd := random.New(p.Seed, 0)
		spectral, _ := Spectral(context.Background(), g, 2, p.Seed)
		y := scaleStart(spectral, n, rnd)
		if err := optimizeBatched(context.Background(), g, y, info, p, workers, nil); err != nil {
			t.Fatal(err)
		}
		return y
	}
	one, four := run(1), run(4)
	for i := range one {
		if one[i] != four[i] {
			t.Fatal("the batched map differs with the workers")
		}
	}
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, four)
	if got := fmt.Sprintf("%x", h.Sum(nil))[:16]; got != batchedDigest {
		t.Errorf("digest %s, recorded %s", got, batchedDigest)
	}
	qs, err := Assess(context.Background(), x, dim, sequential, n, 4)
	if err != nil {
		t.Fatal(err)
	}
	qb, err := Assess(context.Background(), x, dim, four, n, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("trustworthiness: one thread %.4f, batched %.4f; recall %.4f, %.4f", qs.Trustworthiness, qb.Trustworthiness, qs.Recall, qb.Recall)
	if qb.Trustworthiness < qs.Trustworthiness-0.02 {
		t.Errorf("batched trustworthiness %.4f, one thread %.4f", qb.Trustworthiness, qs.Trustworthiness)
	}
}
