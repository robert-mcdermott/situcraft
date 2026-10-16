package validation

// Principal components of large panels (wave 4, slice 8; research.md §5.8): the standard simulated
// tissue made with a panel of 2,000 genes, clustered by SituCraft (its components by the Lanczos
// method, the panel being above cluster.DenseGenes), against the full decomposition of the same
// covariance computed here from the dense matrix of scaled values; and with a panel of 5,000 genes,
// as Xenium Prime's, its components within a time budget.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"situcraft/internal/cluster"
	"situcraft/internal/linalg"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

// The tolerances: the variances within 10⁻⁹ of the largest; the directions of components whose
// variances stand apart (by a thousandth of the largest) parallel to 10⁻⁶ (where variances lie
// closer, as the noise's do, a direction is not determined by either method), and the components'
// subspace the same (the mean squared cosine of its principal angles at least 0.999, as the
// clustering's tolerance against scanpy); a panel of 5,000 genes' components within the budget on
// four cores.
const (
	tolPanelVariance  = 1e-9
	tolPanelDirection = 1e-6
	tolPanelSubspace  = 0.999
	panelBudget       = 2 * time.Minute
)

// panelCounts simulates the standard tissue with a panel of genes genes and answers its instrument
// segmentation's counts.
func panelCounts(t *testing.T, genes int) *segment.Counts {
	t.Helper()
	p := simulate.Presets["standard"]
	p.Genes = genes
	dir := filepath.Join(t.TempDir(), "tissue")
	if _, err := simulate.Run(context.Background(), p, dir, "validation", nil); err != nil {
		t.Fatal(err)
	}
	b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	seg := filepath.Join(t.TempDir(), "instrument")
	os.MkdirAll(seg, 0o755)
	if _, err := imports.Instrument(context.Background(), b, seg, runtime.NumCPU()); err != nil {
		t.Fatal(err)
	}
	counts, err := segment.ReadCounts(seg)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestLargePanels(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to cluster simulated tissues of large panels")
	}
	counts := panelCounts(t, 2000)
	p := cluster.DefaultParams()
	r, err := cluster.Run(context.Background(), cluster.Inputs{Counts: counts}, p, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := cluster.Write(dir, cluster.Side{ID: "s"}, nil, r); err != nil {
		t.Fatal(err)
	}
	d, err := cluster.ReadDescription(dir)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := cluster.ReadPCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.PCA.Solver == "" {
		t.Fatalf("a panel of %d genes decomposed whole", len(counts.Genes))
	}
	// The scaled values, dense, as scanpy's normalize_total, log1p and scale(max_value=10) make
	// them, and their covariance.
	n, G := len(r.Cells), len(counts.Genes)
	x := make([]float64, n*G)
	for i, c := range r.Cells {
		total := 0.0
		for k := counts.Offsets[c]; k < counts.Offsets[c+1]; k++ {
			total += float64(counts.Count[k])
		}
		for k := counts.Offsets[c]; k < counts.Offsets[c+1]; k++ {
			x[i*G+int(counts.Gene[k])] = math.Log1p(float64(counts.Count[k]) / (total / d.Fixed.TargetSum))
		}
	}
	for g := 0; g < G; g++ {
		mean, ss := 0.0, 0.0
		for i := 0; i < n; i++ {
			mean += x[i*G+g]
		}
		mean /= float64(n)
		for i := 0; i < n; i++ {
			ss += (x[i*G+g] - mean) * (x[i*G+g] - mean)
		}
		sd := math.Sqrt(ss / float64(n-1))
		if sd == 0 {
			sd = 1
		}
		for i := 0; i < n; i++ {
			x[i*G+g] = math.Max(-cluster.ClipAt, math.Min(cluster.ClipAt, (x[i*G+g]-mean)/sd))
		}
	}
	means := make([]float64, G)
	for i := 0; i < n; i++ {
		for g := 0; g < G; g++ {
			means[g] += x[i*G+g] / float64(n)
		}
	}
	for i := 0; i < n; i++ {
		for g := 0; g < G; g++ {
			x[i*G+g] -= means[g]
		}
	}
	cov := make([]float64, G*G)
	forRows(G, func(g int) {
		for h := 0; h <= g; h++ {
			s := 0.0
			for i := 0; i < n; i++ {
				s += x[i*G+g] * x[i*G+h]
			}
			cov[g*G+h], cov[h*G+g] = s/float64(n-1), s/float64(n-1)
		}
	})
	start := time.Now()
	values, vectors, err := linalg.SymEigen(cov, G)
	if err != nil {
		t.Fatal(err)
	}
	full := time.Since(start)
	k := pc.Components
	worstValue, worstCos, compared := 0.0, 1.0, 0
	for c := 0; c < k; c++ {
		worstValue = math.Max(worstValue, math.Abs(d.PCA.Variance[c]-values[c])/values[0])
		gap := math.Inf(1)
		if c > 0 {
			gap = values[c-1] - values[c]
		}
		gap = math.Min(gap, values[c]-values[c+1])
		if gap < 1e-3*values[0] {
			continue
		}
		cos := 0.0
		for g := 0; g < G; g++ {
			cos += pc.Loadings[g*k+c] * vectors[g*G+c]
		}
		worstCos = math.Min(worstCos, math.Abs(cos))
		compared++
	}
	// The subspaces: ‖VᵀW‖²_F / k, the mean squared cosine of their principal angles.
	overlap := 0.0
	for a := 0; a < k; a++ {
		for c := 0; c < k; c++ {
			dot := 0.0
			for g := 0; g < G; g++ {
				dot += pc.Loadings[g*k+a] * vectors[g*G+c]
			}
			overlap += dot * dot
		}
	}
	overlap /= float64(k)
	t.Logf("%d genes, %d cells: the components in %.1f s (%s); the full decomposition here in %.1f s; variances within %.2g of the largest, %d separated directions at |cos| ≥ %.12f, the subspace's overlap %.9f",
		G, n, r.Seconds["pca"], d.PCA.Solver, full.Seconds(), worstValue, compared, worstCos, overlap)
	if worstValue > tolPanelVariance || 1-worstCos > tolPanelDirection || compared < 3 || overlap < tolPanelSubspace {
		t.Errorf("not met: variances within %.2g (≤ %g); |cos| %.12f (≥ 1 − %g) over %d components; overlap %.9f (≥ %g)", worstValue, tolPanelVariance, worstCos, tolPanelDirection, compared, overlap, tolPanelSubspace)
	}

	// A panel of 5,000 genes, as Xenium Prime's: its components within the budget.
	prime := panelCounts(t, 5000)
	start = time.Now()
	r, err = cluster.Run(context.Background(), cluster.Inputs{Counts: prime}, p, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("5,000 genes, %d cells: clustered in %.1f s (%v)", len(r.Cells), time.Since(start).Seconds(), r.Seconds)
	if limit := panelBudget * time.Duration(max(1, 4/runtime.NumCPU())); time.Duration(r.Seconds["pca"]*float64(time.Second)) > limit {
		t.Errorf("the components of 5,000 genes took %.1f s, over the budget of %v", r.Seconds["pca"], limit)
	}
}

// forRows runs f on 0…n−1 over every core.
func forRows(n int, f func(int)) {
	workers := runtime.NumCPU()
	done := make(chan struct{})
	next := make(chan int, n)
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	for w := 0; w < workers; w++ {
		go func() {
			for i := range next {
				f(i)
			}
			done <- struct{}{}
		}()
	}
	for w := 0; w < workers; w++ {
		<-done
	}
}
