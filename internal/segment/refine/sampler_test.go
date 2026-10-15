package refine

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"situcraft/internal/simulate"
	"situcraft/internal/stats"
)

// Repositioning samples each transcript's place from its posterior: the voxel it lies in is u with
// probability ∝ Λ(u) × its nuclear prior at u × the displacement prior's mass over u (a mixture
// of two isotropic normals about where it was observed), computed here from normal distribution
// functions.
func TestRepositioningSamplesThePlacesPosterior(t *testing.T) {
	l := Lattice{VoxelUm: 1, LayerUm: 4, W: 6, H: 6, L: 1}
	labels := make([]uint32, l.Voxels())
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			switch {
			case x < 2:
				labels[l.Index(x, y, 0)] = 1
			case x > 3:
				labels[l.Index(x, y, 0)] = 2
			}
		}
	}
	const n = 20000
	const ox, oy = 2.3, 3.1
	tr := Transcripts{}
	for i := 0; i < n; i++ {
		tr.X, tr.Y, tr.Z = append(tr.X, ox), append(tr.Y, oy), append(tr.Z, 1)
		tr.Gene, tr.Prior = append(tr.Gene, 0), append(tr.Prior, 1)
	}
	settings := DefaultSettings()
	settings.WideUm, settings.NarrowUm, settings.WideShare = 1.5, 0.6, 0.3
	s, err := NewState(l, labels, 2, 1, tr, settings)
	if err != nil {
		t.Fatal(err)
	}
	h := DefaultHyper()
	h.Components = 1
	m, err := NewModel(h, s.Volumes(), 1, l.Volume(), 3)
	if err != nil {
		t.Fatal(err)
	}
	m.Workers = 4
	m.Rate = []float64{0.05, 0.3}
	m.Bg = []float64{0.02}
	for sweep := 0; sweep < 60; sweep++ {
		m.Sweep = sweep
		s.reposition(m)
	}
	observed := make([]float64, l.Voxels())
	for _, v := range s.voxel {
		observed[v]++
	}
	// The posterior over voxels.
	interval := func(lo, hi, centre, sigma float64) float64 {
		return stats.NormalCDF((hi-centre)/sigma) - stats.NormalCDF((lo-centre)/sigma)
	}
	rho := h.NuclearPrior
	expected := make([]float64, l.Voxels())
	var total float64
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			v := l.Index(x, y, 0)
			mass := settings.WideShare*interval(float64(x), float64(x+1), ox, settings.WideUm)*interval(float64(y), float64(y+1), oy, settings.WideUm) +
				(1-settings.WideShare)*interval(float64(x), float64(x+1), ox, settings.NarrowUm)*interval(float64(y), float64(y+1), oy, settings.NarrowUm)
			lambda, prior := m.Bg[0], 1-rho
			switch labels[v] {
			case 1:
				lambda += m.Rate[0]
				prior = rho
			case 2:
				lambda += m.Rate[1]
			}
			expected[v] = mass * lambda * prior
			total += expected[v]
		}
	}
	for v := range expected {
		expected[v] = expected[v] / total * n
	}
	if stat, df, p := stats.ChiSquare(observed, expected, 0); p < alpha {
		t.Errorf("repositioned voxels: χ² %.1f on %d degrees of freedom, p %.2g", stat, df, p)
	}
}

// A run on a simulated tissue, its options shortened.
func shortRun(t *testing.T, workers int) (*Result, *BundleIndex) {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "small")
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 80, 60
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	in, idx, err := BundleInputs(openBundle(t, folder), 20)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Stages = []Stage{{2, 1, 10}, {1, 2, 30}}
	opt.Samples, opt.Workers, opt.BlockUm = 10, workers, 16
	res, err := Run(context.Background(), in, Region{}, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res, idx
}

// digestOf hashes everything a run makes.
func digestOf(res *Result) string {
	h := sha256.New()
	put := func(h hash.Hash, v ...float64) {
		for _, x := range v {
			binary.Write(h, binary.LittleEndian, math.Float64bits(x))
		}
	}
	for _, v := range [][]uint32{res.Labels, res.Assigned, res.State.Labels} {
		binary.Write(h, binary.LittleEndian, v)
	}
	binary.Write(h, binary.LittleEndian, res.Probability)
	put(h, res.State.posX...)
	put(h, res.State.posY...)
	m := res.Model
	put(h, m.Rate...)
	put(h, m.Shape...)
	put(h, m.RateParam...)
	put(h, m.Bg...)
	put(h, m.Pi...)
	put(h, m.VolMean...)
	for _, tr := range res.Trace {
		put(h, float64(tr.Moved), float64(tr.Repositioned), tr.LogLikelihood)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// A seed gives the same run, byte for byte, on 1, 4 and 16 threads, and on every processor.
func TestRunsAreTheSameOnAnyThreadsAndProcessor(t *testing.T) {
	one, _ := shortRun(t, 1)
	want := digestOf(one)
	for _, workers := range []int{4, 16} {
		res, _ := shortRun(t, workers)
		if got := digestOf(res); got != want {
			t.Errorf("%d threads: digest %s, 1 thread %s", workers, got, want)
		}
	}
	const pinned = "5511d4da8868b0a0"
	if want != pinned {
		t.Errorf("the run's digest is %s, not %s (on %s)", want, pinned, runtime.GOARCH)
	}
}
