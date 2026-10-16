package cluster

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/linalg"
	"situcraft/internal/random"
	"situcraft/internal/segment"
)

// tissue is a simulated count matrix: types cell types of equal shares, each with its own marker
// genes (ten times the expression of the rest), negative-binomial counts of mean 2 elsewhere, and
// cells of different depths.
func tissue(cells, genes, types int, seed uint64) (*segment.Counts, []int32) {
	s := random.New(seed, 0)
	c := &segment.Counts{Offsets: []uint32{0}}
	for g := 0; g < genes; g++ {
		c.Genes = append(c.Genes, "G"+string(rune('A'+g/26))+string(rune('A'+g%26)))
	}
	truth := make([]int32, cells)
	for i := 0; i < cells; i++ {
		t := i % types
		truth[i] = int32(t)
		depth := 0.5 + s.Float64()
		for g := 0; g < genes; g++ {
			mean := 0.3
			if g%types == t && g < types*6 {
				mean = 3
			}
			if n := s.NegativeBinomial(mean*depth, 2); n > 0 {
				c.Gene = append(c.Gene, uint32(g))
				c.Count = append(c.Count, uint32(n))
			}
		}
		c.Offsets = append(c.Offsets, uint32(len(c.Gene)))
	}
	return c, truth
}

// The sparse covariance's components against the dense matrix's, computed directly.
func TestPCAAgainstTheDenseCovariance(t *testing.T) {
	counts, _ := tissue(400, 30, 3, 1)
	m, _, err := selectCells(counts, func(int, uint32) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	m.normalize(median(m.total))
	mean, sd, _ := m.scaling()
	const clipAt = 1.5 // low, so that clipping matters
	p, err := m.pca(context.Background(), mean, sd, clipAt, 8, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	n, G := len(m.cells), m.genes
	x := make([]float64, n*G)
	for i := 0; i < n; i++ {
		for g := 0; g < G; g++ {
			x[i*G+g] = clip(-mean[g]/sd[g], clipAt)
		}
		for e := m.offsets[i]; e < m.offsets[i+1]; e++ {
			g := int(m.gene[e])
			x[i*G+g] = clip((m.value[e]-mean[g])/sd[g], clipAt)
		}
	}
	colMean := make([]float64, G)
	for i := 0; i < n; i++ {
		for g := 0; g < G; g++ {
			colMean[g] += x[i*G+g] / float64(n)
		}
	}
	cov := make([]float64, G*G)
	for i := 0; i < n; i++ {
		for g := 0; g < G; g++ {
			for h := 0; h < G; h++ {
				cov[g*G+h] += (x[i*G+g] - colMean[g]) * (x[i*G+h] - colMean[h]) / float64(n-1)
			}
		}
	}
	values, vectors, _ := linalg.SymEigen(cov, G)
	trace := 0.0
	for g := 0; g < G; g++ {
		trace += cov[g*G+g]
	}
	if math.Abs(trace-p.totalVariance) > 1e-10 {
		t.Errorf("total variance %g, dense %g", p.totalVariance, trace)
	}
	for c := 0; c < p.components; c++ {
		if math.Abs(values[c]-p.values[c]) > 1e-10*values[0] {
			t.Errorf("component %d: variance %g, dense %g", c, p.values[c], values[c])
		}
		dot := 0.0
		for g := 0; g < G; g++ {
			dot += vectors[g*G+c] * p.loadings[g*p.components+c]
		}
		if math.Abs(math.Abs(dot)-1) > 1e-8 {
			t.Errorf("component %d: |cos| with the dense direction %g", c, math.Abs(dot))
		}
		// The largest loading in magnitude is positive.
		at, big := 0, 0.0
		for g := 0; g < G; g++ {
			if v := math.Abs(p.loadings[g*p.components+c]); v > big {
				at, big = g, v
			}
		}
		if p.loadings[at*p.components+c] < 0 {
			t.Errorf("component %d: its largest loading is negative", c)
		}
		for i := 0; i < n; i += 37 {
			s := 0.0
			for g := 0; g < G; g++ {
				s += (x[i*G+g] - colMean[g]) * p.loadings[g*p.components+c]
			}
			if math.Abs(s-p.scores[i*p.components+c]) > 1e-10 {
				t.Errorf("cell %d, component %d: score %g, dense %g", i, c, p.scores[i*p.components+c], s)
			}
		}
	}
}

// Normalization and scaling by hand: two cells, totals 4 and 2, the median 3.
func TestNormalizationByHand(t *testing.T) {
	c := &segment.Counts{Genes: []string{"A", "B"}, Offsets: []uint32{0, 2, 3}, Gene: []uint32{1, 0, 0}, Count: []uint32{1, 3, 2}}
	m, empty, _ := selectCells(c, func(int, uint32) bool { return true })
	if empty != 0 || m.gene[0] != 0 || m.gene[1] != 1 {
		t.Fatalf("rows not ordered by gene: %v", m.gene)
	}
	target := median(m.total)
	if target != 3 {
		t.Fatalf("median %g", target)
	}
	m.normalize(target)
	want := []float64{math.Log1p(3 / (4.0 / 3)), math.Log1p(1 / (4.0 / 3)), math.Log1p(2 / (2.0 / 3))}
	for e, v := range m.value {
		if math.Abs(v-want[e]) > 1e-15 {
			t.Errorf("value %d: %.17g, by hand %.17g", e, v, want[e])
		}
	}
	mean, sd, detected := m.scaling()
	// Gene A: values want[0] and want[2]; gene B: want[1] and 0.
	ma, mb := (want[0]+want[2])/2, want[1]/2
	sa := math.Sqrt(((want[0]-ma)*(want[0]-ma) + (want[2]-ma)*(want[2]-ma)) / 1)
	sb := math.Sqrt(((want[1]-mb)*(want[1]-mb) + mb*mb) / 1)
	if math.Abs(mean[0]-ma) > 1e-15 || math.Abs(mean[1]-mb) > 1e-15 || math.Abs(sd[0]-sa) > 1e-15 || math.Abs(sd[1]-sb) > 1e-15 || detected[0] != 2 || detected[1] != 1 {
		t.Errorf("scaling %v %v %v, by hand %g %g %g %g", mean, sd, detected, ma, mb, sa, sb)
	}
}

// The rank-sum test by hand: six cells, one gene with values 0, 0, 1, 2, 2, 3 (log-normalized
// stand-ins), the first three in cluster 0.
func TestMarkersByHand(t *testing.T) {
	// Counts over equal totals rank as the values do (the ranks come from count / total, exactly).
	m := &matrix{cells: []uint32{0, 1, 2, 3, 4, 5}, genes: 1, offsets: []int{0, 0, 0, 1, 2, 3, 4}, gene: []uint32{0, 0, 0, 0},
		count: []uint32{1, 2, 2, 3}, total: []uint32{10, 10, 10, 10, 10, 10}, value: []float64{1, 2, 2, 3}}
	mk := m.markers([]int32{0, 0, 0, 1, 1, 1}, 2, 2)
	// Ranks: 1.5, 1.5, 3, 4.5, 4.5, 6. Cluster 0's sum 6, expected 3·7/2 = 10.5. Ties: two pairs,
	// Σ(t³ − t) = 12; correction 1 − 12/210. σ = √(correction · 3·3·7/12).
	tie := 1 - 12.0/210
	sd := math.Sqrt(tie * 3 * 3 * 7 / 12)
	z := (6 - 10.5) / sd
	if math.Abs(mk.Score[0]-z) > 1e-14 || math.Abs(mk.Score[1]+z) > 1e-14 {
		t.Errorf("scores %v, by hand ±%g", mk.Score, z)
	}
	if p := math.Erfc(math.Abs(z) / math.Sqrt2); math.Abs(mk.P[0]-p) > 1e-15 {
		t.Errorf("p %g, by hand %g", mk.P[0], p)
	}
	lfc := math.Log2((math.Expm1(1.0/3) + 1e-9) / (math.Expm1(7.0/3) + 1e-9))
	if math.Abs(mk.LogFC[0]-lfc) > 1e-12 || mk.PctIn[0] != 1.0/3 || mk.PctOut[0] != 1 {
		t.Errorf("log fold change %g (by hand %g), in %g, out %g", mk.LogFC[0], lfc, mk.PctIn[0], mk.PctOut[0])
	}
	// Equal ratios tie whatever their totals: 1/3 and 2/6, then 1/2.
	m = &matrix{cells: []uint32{0, 1, 2, 3}, genes: 1, offsets: []int{0, 0, 1, 2, 3}, gene: []uint32{0, 0, 0},
		count: []uint32{1, 2, 1}, total: []uint32{9, 3, 6, 2}, value: []float64{0.287682, 0.287682 + 1e-16, 0.405465}}
	mk = m.markers([]int32{0, 0, 1, 1}, 2, 1)
	// Ranks 1, 2.5, 2.5, 4; cluster 0's sum 3.5, expected 5; ties Σ(t³ − t) = 6, correction 1 − 6/60.
	if want := (3.5 - 5) / math.Sqrt(0.9*2*2*5/12); math.Abs(mk.Score[0]-want) > 1e-14 {
		t.Errorf("equal ratios: score %g, by hand %g", mk.Score[0], want)
	}
	// Benjamini–Hochberg on a known case.
	p := []float64{0.01, 0.04, 0.03, 0.005}
	adj := make([]float64, 4)
	bh(p, adj)
	for i, want := range []float64{0.02, 0.04, 0.04, 0.02} {
		if math.Abs(adj[i]-want) > 1e-15 {
			t.Errorf("adjusted %v, want 0.02 0.04 0.04 0.02", adj)
			break
		}
	}
}

func TestAdjustedRand(t *testing.T) {
	a := []int32{0, 0, 0, 1, 1, 1}
	if r := AdjustedRand(a, []int32{5, 5, 5, 2, 2, 2}); r != 1 {
		t.Errorf("relabelled: %g", r)
	}
	// scikit-learn's adjusted_rand_score([0,0,1,1],[0,0,1,2]) is 0.5714285714285715.
	if r := AdjustedRand([]int32{0, 0, 1, 1}, []int32{0, 0, 1, 2}); math.Abs(r-4.0/7) > 1e-15 {
		t.Errorf("a split: %g, want 4/7", r)
	}
}

func digest(dir string) string {
	d, err := Digest(dir)
	if err != nil {
		return err.Error()
	}
	return d[:16]
}

// Planted cell types are found, the files read back and pass the check, the same files come from
// one worker and many, and their digest is pinned (checked on arm64 and amd64).
func TestClustersFindPlantedTypes(t *testing.T) {
	counts, truth := tissue(1200, 60, 4, 20250)
	p := DefaultParams()
	p.Components = 20
	dirs := []string{}
	for _, workers := range []int{1, 7} {
		r, err := Run(context.Background(), Inputs{Counts: counts}, p, workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := make([]int32, len(r.Cells))
		want := make([]int32, len(r.Cells))
		for i, c := range r.Cells {
			found[i], want[i] = r.Partition.Community[i], truth[c]
		}
		if ari := AdjustedRand(found, want); ari < 0.95 {
			t.Errorf("%d workers: adjusted Rand index %g against the planted types (%d clusters %v)", workers, ari, len(r.Partition.Sizes), r.Partition.Sizes)
		}
		// Each type's markers lead its cluster.
		for c := 0; c < r.Markers.Clusters && workers == 1; c++ {
			top := TopMarkers(r.Markers, r.Genes, c, 3)
			if top[0].LogFC < 1 || top[0].PAdj > 1e-10 {
				t.Errorf("cluster %d's first marker: %+v", c, top[0])
			}
		}
		dir := t.TempDir()
		if err := Write(dir, Side{ID: "seg-1", Name: "Simulated"}, nil, r); err != nil {
			t.Fatal(err)
		}
		var m artifacts.Manifest
		if findings := Check(dir, &m); len(findings) != 0 {
			t.Fatalf("check: %+v", findings)
		}
		dirs = append(dirs, dir)
	}
	if a, b := digest(dirs[0]), digest(dirs[1]); a != b {
		t.Errorf("one worker %s, seven %s", a, b)
	} else if a != clusterDigest {
		t.Errorf("digest %s, recorded %s", a, clusterDigest)
	}
	// A cluster changed in the file is caught.
	cells, cluster, _ := ReadCells(dirs[0])
	cluster[0] = (cluster[0] + 1) % 4
	var b segment.Buffer
	b.U32(uint32(len(cells)))
	b.U32s(cells)
	b.U32s(cluster)
	segment.WriteBinary(filepath.Join(dirs[0], FileCells), "SCKC", &b)
	var m artifacts.Manifest
	if findings := Check(dirs[0], &m); len(findings) == 0 {
		t.Error("a cell moved to another cluster passed the check")
	}
}

// The cells a filter keeps, cells without transcripts left out and counted, and the rule.
func TestTheCellsAnalyzed(t *testing.T) {
	counts, _ := tissue(300, 30, 3, 9)
	// A cell without transcripts.
	counts.Offsets = append([]uint32{0}, counts.Offsets...)
	keep := make([]bool, 301)
	for i := range keep {
		keep[i] = i%3 != 1
	}
	p := DefaultParams()
	p.Components = 10
	r, err := Run(context.Background(), Inputs{Counts: counts, Keep: keep}, p, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.NoTranscript != 1 || len(r.Cells) != 200 || r.Excluded != 100 || r.Cells[0] != 2 {
		t.Errorf("filter: %d analyzed, %d excluded, %d without transcripts, first %d", len(r.Cells), r.Excluded, r.NoTranscript, r.Cells[0])
	}
	if _, err := Run(context.Background(), Inputs{Counts: counts, Keep: keep[:5]}, p, 2, nil); err == nil {
		t.Error("a filter of another segmentation's size")
	}
	p.MinTranscripts = 1 << 20
	if _, err := Run(context.Background(), Inputs{Counts: counts}, p, 2, nil); err == nil {
		t.Error("a minimum above 100,000 gene transcripts")
	}
	p.MinTranscripts = 100000
	if _, err := Run(context.Background(), Inputs{Counts: counts}, p, 2, nil); err == nil {
		t.Error("no cell analyzed, yet a clustering")
	}
}

const clusterDigest = "a0fdea1738c0ed18"

// Another tool's clusters: recorded with their source and numbers, markers computed as SituCraft
// computes them, the files checked; a cluster's markers are those of its cells against the rest
// (Contrast), and the expression and dot plot agree with the normalization by hand.
func TestAnotherToolsClustersAndExpression(t *testing.T) {
	counts, truth := tissue(600, 40, 3, 77)
	cells := make([]uint32, 0, 600)
	labels := make([]int32, 0, 600)
	for i := 0; i < 600; i++ {
		if i%10 == 9 {
			continue // a cell the tool did not cluster
		}
		cells = append(cells, uint32(i))
		labels = append(labels, 2-truth[i]) // its own numbers
	}
	src := Source{Tool: "Xenium Onboard Analysis", Version: "xenium-2.0.0.10", File: "analysis.tar.gz", Name: "gene_expression_graphclust", Title: "Graph-based"}
	r, err := FromLabels(counts, cells, labels, src, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cells) != 540 || r.Excluded != 60 || r.Partition.Sizes[0] != 180 {
		t.Fatalf("%d cells, %d left out, sizes %v", len(r.Cells), r.Excluded, r.Partition.Sizes)
	}
	dir := t.TempDir()
	if err := Write(dir, Side{ID: "seg-1", Name: "Instrument segmentation"}, nil, r); err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	if f := Check(dir, &m); len(f) != 0 {
		t.Fatalf("check: %+v", f)
	}
	d, _ := ReadDescription(dir)
	if d.Source == nil || d.Source.Name != "gene_expression_graphclust" || len(FilesOf(d)) != 4 || d.Method != "Xenium Onboard Analysis Graph-based clustering" {
		t.Errorf("description: %+v, method %q", d.Source, d.Method)
	}
	if _, err := Digest(dir); err != nil {
		t.Fatal(err)
	}
	clusters := make([]uint32, len(r.Cells))
	for i, c := range r.Partition.Community {
		clusters[i] = uint32(c)
	}
	e, err := LoadExpression(counts, r.Cells, clusters, r.Target)
	if err != nil {
		t.Fatal(err)
	}
	// A cluster's markers against the rest are its cells' contrast with the rest.
	selected := make([]bool, len(r.Cells))
	for i, c := range clusters {
		selected[i] = c == 1
	}
	contrast, err := e.Contrast(selected, 2)
	if err != nil {
		t.Fatal(err)
	}
	G := r.Markers.Genes
	for g := 0; g < G; g++ {
		// The rest's sum is taken in another order: the fold change may differ by a rounding.
		if contrast.Score[g] != r.Markers.Score[G+g] || math.Abs(contrast.LogFC[g]-r.Markers.LogFC[G+g]) > 1e-12 {
			t.Fatalf("gene %d: the contrast's score %g, cluster 1's %g", g, contrast.Score[g], r.Markers.Score[G+g])
		}
	}
	// Values and the dot plot by hand.
	gene := 5
	values := e.Values(gene)
	sum, expressing, size := 0.0, 0, 0
	for i, cell := range r.Cells {
		total, count := 0, 0
		for k := counts.Offsets[cell]; k < counts.Offsets[cell+1]; k++ {
			total += int(counts.Count[k])
			if int(counts.Gene[k]) == gene {
				count = int(counts.Count[k])
			}
		}
		want := 0.0
		if count > 0 {
			want = math.Log1p(float64(count) / (float64(total) / r.Target))
		}
		if math.Abs(float64(values[i])-want) > 1e-6 {
			t.Fatalf("cell %d: %g, by hand %g", cell, values[i], want)
		}
		if clusters[i] == 0 {
			size++
			sum += want
			if count > 0 {
				expressing++
			}
		}
	}
	mean, share := e.Dot([]int{gene}, 3)
	if math.Abs(mean[0][0]-sum/float64(size)) > 1e-9 || share[0][0] != float64(expressing)/float64(size) {
		t.Errorf("dot plot %g, %g; by hand %g, %g", mean[0][0], share[0][0], sum/float64(size), float64(expressing)/float64(size))
	}
	if _, err := e.Contrast(make([]bool, len(r.Cells)), 2); err == nil {
		t.Error("an empty selection's markers")
	}
}
