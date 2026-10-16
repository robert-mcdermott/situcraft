package validation

// Approximate neighbors (wave 4, slice 8; research.md §5.8): above 50,000 cells SituCraft finds each
// cell's nearest neighbors by NN-Descent (internal/graph, descent.go). The development bundles are
// below that, so their exact neighbors are the reference: each bundle's instrument segmentation,
// clustered with the defaults, its components searched by NN-Descent as a larger tissue's would be;
// the share of the exact neighbors found, the recall SituCraft would record (over 1,000 cells)
// against it, and the clusters Leiden finds on the approximate graph against the exact graph's,
// beside Leiden's own agreement with itself across seeds.

import (
	"context"
	"os"
	"runtime"
	"slices"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/graph"
)

// The tolerances: NN-Descent finds at least 99.5% of the exact neighbors; the recall recorded is
// within 0.005 of the true one; over five seeds, the clusters of the approximate graph agree with
// the exact graph's (median adjusted Rand index) no less than Leiden's runs on the exact graph agree
// with one another, less 0.05, and the median modularity is within 0.005.
const (
	tolDescentRecall   = 0.995
	tolSampledRecall   = 0.005
	tolDescentClusters = 0.05
	tolDescentQuality  = 0.005
)

func TestApproximateNeighbors(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to search the development bundles' neighbors approximately")
	}
	for _, set := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		t.Run(set, func(t *testing.T) {
			counts := instrumentCounts(t, bundle(t, set))
			p := cluster.DefaultParams()
			r, err := cluster.Run(context.Background(), cluster.Inputs{Counts: counts}, p, runtime.NumCPU(), nil)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			if err := cluster.Write(out, cluster.Side{ID: "s"}, nil, r); err != nil {
				t.Fatal(err)
			}
			pc, err := cluster.ReadPCA(out)
			if err != nil {
				t.Fatal(err)
			}
			n, dim, k := len(r.Cells), pc.Components, p.Neighbors-1
			x := make([]float64, len(pc.Scores))
			for i, v := range pc.Scores {
				x[i] = float64(v)
			}
			// The exact neighbors of the scores as written (float32), and NN-Descent's of the same.
			exact, err := graph.KNN(context.Background(), x, n, dim, k, runtime.NumCPU(), nil)
			if err != nil {
				t.Fatal(err)
			}
			width := graph.SearchWidth(k)
			wide, stats, err := graph.NNDescent(context.Background(), x, n, dim, width, graph.DefaultDescent(p.Seed), runtime.NumCPU(), nil)
			if err != nil {
				t.Fatal(err)
			}
			approx := &graph.Neighbors{N: n, K: k, Index: make([]int32, n*k), Distances: make([]float64, n*k)}
			for i := 0; i < n; i++ {
				copy(approx.Index[i*k:(i+1)*k], wide.Index[i*width:i*width+k])
				copy(approx.Distances[i*k:(i+1)*k], wide.Distances[i*width:i*width+k])
			}
			hit := 0
			for i := 0; i < n; i++ {
				row := approx.Index[i*k : (i+1)*k]
				for _, j := range exact.Index[i*k : (i+1)*k] {
					if slices.Contains(row, j) {
						hit++
					}
				}
			}
			recall := float64(hit) / float64(n*k)
			sampled, err := graph.SampledRecall(context.Background(), x, n, dim, approx, 1000, runtime.NumCPU())
			if err != nil {
				t.Fatal(err)
			}
			leiden := func(nb *graph.Neighbors, seed uint64) (*graph.Partition, float64) {
				g, err := graph.Fuzzy(nb)
				if err != nil {
					t.Fatal(err)
				}
				part, err := graph.Leiden(context.Background(), g, graph.LeidenOptions{Resolution: p.Resolution, Randomness: cluster.Randomness, Seed: seed})
				if err != nil {
					t.Fatal(err)
				}
				return part, part.Modularity
			}
			// Five seeds on each graph: the exact graph's partitions against one another (each with the
			// next seed's), and each against the approximate graph's of the same seed; the medians.
			var exParts, apParts []*graph.Partition
			var exQs, apQs []float64
			for s := uint64(0); s < 5; s++ {
				e, q := leiden(exact, p.Seed+s)
				a, qa := leiden(approx, p.Seed+s)
				exParts, apParts = append(exParts, e), append(apParts, a)
				exQs, apQs = append(exQs, q), append(apQs, qa)
			}
			var self, cross []float64
			for s := range exParts {
				self = append(self, cluster.AdjustedRand(exParts[s].Community, exParts[(s+1)%5].Community))
				cross = append(cross, cluster.AdjustedRand(exParts[s].Community, apParts[s].Community))
			}
			agree, seeds, exQ, apQ := medianOf(cross), medianOf(self), medianOf(exQs), medianOf(apQs)
			t.Logf("%s: %d cells; NN-Descent (%d searched, %d rounds) found %.4f of the exact neighbors (%.4f over 1,000 cells); over five seeds, the clusters' ARI with the exact graph's %.3f (median; %v), the exact graph's with itself %.3f (%v); modularity %.4f and %.4f",
				set, n, width, stats.Rounds, recall, sampled, agree, rounded(cross), seeds, rounded(self), exQ, apQ)
			for _, c := range []check{
				{Name: "the exact neighbors NN-Descent finds", Tolerance: "≥", Value: recall, Limit: tolDescentRecall, Pass: recall >= tolDescentRecall},
				{Name: "the recall recorded, over 1,000 cells, against the true (|difference|)", Tolerance: "≤", Value: abs(sampled - recall), Limit: tolSampledRecall, Pass: abs(sampled-recall) <= tolSampledRecall},
				{Name: "the approximate graph's clusters against the exact's (median ARI over five seeds), against the exact graph's with itself less the margin", Tolerance: "≥", Value: agree, Limit: seeds - tolDescentClusters, Pass: agree >= seeds-tolDescentClusters},
				{Name: "the approximate graph's median modularity against the exact's (|difference|)", Tolerance: "≤", Value: abs(apQ - exQ), Limit: tolDescentQuality, Pass: abs(apQ-exQ) <= tolDescentQuality},
			} {
				if c.Pass {
					t.Logf("met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				} else {
					t.Errorf("not met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				}
			}
		})
	}
}

func medianOf(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func rounded(v []float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(int(x*1000+0.5)) / 1000
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
