// Package refine is SituCraft's transcript-informed segmentation, after Jones DC, Elz AE,
// Hadadianpour A, Ryu H, Glass DR, Newell EW. Cell simulation as cell segmentation. Nat Methods
// 2025;22(6):1331–1342. doi:10.1038/s41592-025-02697-0: cells are regions of space whose
// molecules are Poisson processes with rates of their own, over a background, and a segmentation
// is sampled from its posterior. research.md §3.2 writes the model and its updates out; this file
// is the expression and volume model and its parameter updates, lattice.go the labelling.
//
// The updates read the cells' volumes and attributed counts and draw the parameters, in the order
// research.md gives (a partially collapsed Gibbs sampler: van Dyk DA, Park T. JASA
// 2008;103:790–796):
//   - the shapes by Chinese-restaurant-table augmentation (Zhou M, Carin L. IEEE TPAMI
//     2015;37:307–320. doi:10.1109/TPAMI.2013.211);
//   - everything else by conjugacy.
//
// SituCraft's choice: a gamma prior on each component's rate parameter (not the publication's
// logistically parameterized probability with Pólya–Gamma variables), so that every update is an
// exact draw. Every cell's, gene's and component's draws come from its own random stream, named
// by the sweep, the step and the item, so results are the same on any number of threads; every
// product is rounded on its own and logarithms come from internal/dmath, so they are the same on
// every processor.
package refine

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// mul is a product rounded on its own (never fused with an addition).
func mul(a, b float64) float64 { return float64(a * b) }

// Hyper are the model's hyperparameters (research.md §3.2's defaults table).
type Hyper struct {
	// Components is K, the number of expression states.
	Components int `json:"components"`
	// Alpha is the Dirichlet prior's parameter on the components' proportions.
	Alpha float64 `json:"alpha"`
	// ShapeA, ShapeB: the gamma prior (shape, rate) on each component's shape r.
	ShapeA float64 `json:"shape_a"`
	ShapeB float64 `json:"shape_b"`
	// RateA: the gamma prior's shape on each component's rate parameter β (its rate is the gene's
	// scale φ).
	RateA float64 `json:"rate_a"`
	// ScaleE, ScaleF: the gamma prior (shape, rate in µm³) on each gene's scale φ.
	ScaleE   float64 `json:"scale_e"`
	ScaleFUm float64 `json:"scale_f_um3"`
	// BackgroundA, BackgroundB: the gamma prior (shape, rate in µm³) on the background's rates.
	BackgroundA   float64 `json:"background_a"`
	BackgroundBUm float64 `json:"background_b_um3"`
	// The volume prior: log volume normal of mean m (prior mean VolumeMean, in log µm³; 0 for the
	// log of the median initial volume) and variance s² (inverse gamma VolumeA, VolumeB), the mean's
	// prior variance s²/VolumeKappa.
	VolumeMean  float64 `json:"volume_mean_log_um3"`
	VolumeKappa float64 `json:"volume_kappa"`
	VolumeA     float64 `json:"volume_a"`
	VolumeB     float64 `json:"volume_b"`
	// NuclearPrior is ρ, the prior probability that a transcript over a nucleus belongs to its cell.
	NuclearPrior float64 `json:"nuclear_prior"`
	// Exact holds one volume prior, its prior means, for every component: the sampler is then
	// exact (research.md §3.2, "the one approximation").
	Exact bool `json:"exact"`
}

// DefaultHyper are SituCraft's defaults.
func DefaultHyper() Hyper {
	return Hyper{Components: 10, Alpha: 1, ShapeA: 2, ShapeB: 0.5, RateA: 1, ScaleE: 1, ScaleFUm: 1,
		BackgroundA: 1, BackgroundBUm: 1, VolumeKappa: 1, VolumeA: 2, VolumeB: 0.25, NuclearPrior: 0.8}
}

// Check refuses hyperparameters the model cannot use.
func (h Hyper) Check() error {
	switch {
	case h.Components < 1 || h.Components > 1000:
		return errors.New("refine: 1 to 1,000 components")
	case !(h.Alpha > 0), !(h.ShapeA > 0), !(h.ShapeB > 0), !(h.RateA > 0), !(h.ScaleE > 0), !(h.ScaleFUm > 0),
		!(h.BackgroundA > 0), !(h.BackgroundBUm > 0), !(h.VolumeKappa > 0), !(h.VolumeA > 1), !(h.VolumeB > 0):
		return errors.New("refine: the priors' parameters are positive (the volume variance's shape above 1)")
	case !(h.NuclearPrior > 0 && h.NuclearPrior < 1):
		return errors.New("refine: the nuclear prior is between 0 and 1")
	}
	return nil
}

// Model is the expression and volume model's state: the statistics its updates read (the cells'
// volumes and attributed counts, the background's) and the parameters they draw.
type Model struct {
	H       Hyper
	Genes   int
	Cells   int
	Seed    uint64
	Workers int

	// Volume is each cell's volume (µm³); Counts each cell's transcripts of each gene attributed to
	// it (cell-major: Counts[c*Genes+g]); Comp each cell's component; Rate each cell's rates λ (per
	// µm³, like Counts), RateSum their sum over genes.
	Volume  []float64
	Counts  []uint32
	Comp    []int
	Rate    []float64
	RateSum []float64

	// BgCounts are the transcripts attributed to the background, by gene; Bg its rates (per µm³);
	// Region the region's volume (µm³).
	BgCounts []uint32
	Bg       []float64
	Region   float64

	// The components: shapes r and rate parameters β (component-major, k*Genes+g), each gene's
	// scale φ, the proportions π, and each component's volume prior (mean and variance of the log
	// volume).
	Shape     []float64
	RateParam []float64
	Scale     []float64
	Pi        []float64
	VolMean   []float64
	VolVar    []float64

	// Sweep counts the sweeps drawn, naming their random streams.
	Sweep int
}

// Steps of a sweep, naming random streams.
const (
	stepAttribute = iota + 1
	stepBackground
	stepComponents
	stepShapes
	stepRates
	stepRateParams
	stepScales
	stepProportions
	stepVolumes
	stepInit
)

// stream is the random stream of an item's draws in a step of a sweep.
func (m *Model) stream(step, item int) *random.Stream {
	return random.New(m.Seed, uint64(step)<<58|uint64(m.Sweep)<<32|uint64(item))
}

// NewModel makes a model of cells (their volumes, µm³) and genes in a region (µm³), with the
// hyperparameters (VolumeMean resolved from the volumes when 0) and a seed.
func NewModel(h Hyper, volumes []float64, genes int, region float64, seed uint64) (*Model, error) {
	if err := h.Check(); err != nil {
		return nil, err
	}
	if len(volumes) == 0 || genes < 1 || !(region > 0) {
		return nil, errors.New("refine: a model needs cells, genes and a region")
	}
	for c, v := range volumes {
		if !(v > 0) {
			return nil, fmt.Errorf("refine: cell %d has no volume", c+1)
		}
	}
	if h.VolumeMean == 0 {
		v := append([]float64(nil), volumes...)
		sort.Float64s(v)
		h.VolumeMean = dmath.Log(v[len(v)/2])
	}
	n, k := len(volumes), h.Components
	m := &Model{H: h, Genes: genes, Cells: n, Seed: seed, Workers: runtime.GOMAXPROCS(0),
		Volume: append([]float64(nil), volumes...), Counts: make([]uint32, n*genes), Comp: make([]int, n),
		Rate: make([]float64, n*genes), RateSum: make([]float64, n),
		BgCounts: make([]uint32, genes), Bg: make([]float64, genes), Region: region,
		Shape: make([]float64, k*genes), RateParam: make([]float64, k*genes), Scale: make([]float64, genes),
		Pi: make([]float64, k), VolMean: make([]float64, k), VolVar: make([]float64, k)}
	return m, nil
}

// Init starts the parameters from the counts (set Counts and BgCounts first): each gene's mean
// rate over the cells sets its components' rate parameters at the prior mean shape; components
// are drawn uniformly; each cell's rates are their posterior means; the background's rate is its
// posterior mean; the volume priors are their prior means.
func (m *Model) Init() {
	h, g := m.H, m.Genes
	var volume float64
	for _, v := range m.Volume {
		volume += v
	}
	r0 := h.ShapeA / h.ShapeB
	for j := 0; j < g; j++ {
		var count float64
		for c := 0; c < m.Cells; c++ {
			count += float64(m.Counts[c*g+j])
		}
		mean := (count + 0.5) / volume
		m.Scale[j] = mul(h.RateA, mean) / r0
		for k := 0; k < h.Components; k++ {
			m.Shape[k*g+j] = r0
			m.RateParam[k*g+j] = r0 / mean
		}
		m.Bg[j] = (h.BackgroundA + float64(m.BgCounts[j])) / (h.BackgroundBUm + m.Region)
	}
	s := random.New(m.Seed, uint64(stepInit)<<58)
	for c := range m.Comp {
		m.Comp[c] = s.IntN(h.Components)
	}
	for k := range m.Pi {
		m.Pi[k] = 1 / float64(h.Components)
		m.VolMean[k], m.VolVar[k] = h.VolumeMean, h.VolumeB/(h.VolumeA-1)
	}
	for c := 0; c < m.Cells; c++ {
		k := m.Comp[c]
		var sum float64
		for j := 0; j < g; j++ {
			m.Rate[c*g+j] = (m.Shape[k*g+j] + float64(m.Counts[c*g+j])) / (m.RateParam[k*g+j] + m.Volume[c])
			sum += m.Rate[c*g+j]
		}
		m.RateSum[c] = sum
	}
}

// each runs fn over [0, n) in chunks on the model's workers; fn's results must not depend on the
// chunks.
func (m *Model) each(n int, fn func(lo, hi int)) {
	workers := max(1, min(m.Workers, n))
	if workers == 1 {
		fn(0, n)
		return
	}
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < n; lo += chunk {
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			fn(lo, hi)
		}(lo, min(n, lo+chunk))
	}
	wg.Wait()
}

// members are each component's cells, in order.
func (m *Model) members() [][]int {
	out := make([][]int, m.H.Components)
	for c, k := range m.Comp {
		out[k] = append(out[k], c)
	}
	return out
}

// SweepParameters draws every parameter once, given the counts (research.md §3.2, steps 2–9;
// step 1, the attributions, is the labelling's: State.Attribute), and counts the sweep.
func (m *Model) SweepParameters() {
	m.UpdateBackground()
	m.UpdateComponents()
	m.UpdateShapes()
	m.UpdateRates()
	m.UpdateRateParams()
	m.UpdateScales()
	m.UpdateProportions()
	m.UpdateVolumes()
	m.Sweep++
}

// UpdateBackground draws the background's rates: Gamma(a₀ + n₀g, b₀ + |R|).
func (m *Model) UpdateBackground() {
	rate := m.H.BackgroundBUm + m.Region
	for g := range m.Bg {
		m.Bg[g] = m.stream(stepBackground, g).Gamma(m.H.BackgroundA+float64(m.BgCounts[g]), 1/rate)
	}
}

// logLogNormal is log LogNormal(v; mean, variance) of a volume.
func logLogNormal(v, mean, variance float64) float64 {
	y := dmath.Log(v)
	d := y - mean
	return -y - mul(0.5, dmath.Log(mul(2*math.Pi, variance))) - mul(d, d)/(2*variance)
}

// logRising is log r(r+1)⋯(r+n−1), the negative binomial's log Γ(n+r)/Γ(r).
func logRising(r float64, n uint32) float64 {
	var sum float64
	for j := uint32(0); j < n; j++ {
		sum += dmath.Log(r + float64(j))
	}
	return sum
}

// ComponentLogWeights are cell c's unnormalized log probabilities of each component, its rates
// integrated out: log π_k + log LogNormal(V_c) + Σ_g log NB(n_cg; r_kg, V_c/(V_c + β_kg)), without
// the terms common to every component.
func (m *Model) ComponentLogWeights(c int, out []float64) {
	g := m.Genes
	v := m.Volume[c]
	logV := dmath.Log(v)
	counts := m.Counts[c*g : (c+1)*g]
	for k := range out {
		w := dmath.Log(m.Pi[k]) + logLogNormal(v, m.VolMean[k], m.VolVar[k])
		for j, n := range counts {
			r, beta := m.Shape[k*g+j], m.RateParam[k*g+j]
			logVB := dmath.Log(v + beta)
			// r log(1 − p) + n log p, p = V/(V + β).
			w += mul(r, dmath.Log(beta)-logVB)
			if n > 0 {
				w += mul(float64(n), logV-logVB) + logRising(r, n)
			}
		}
		out[k] = w
	}
}

// pickLog draws an index with probabilities proportional to exp(logw).
func pickLog(s *random.Stream, logw []float64) int {
	top := math.Inf(-1)
	for _, w := range logw {
		top = math.Max(top, w)
	}
	var total float64
	p := make([]float64, len(logw))
	for k, w := range logw {
		p[k] = dmath.Exp(w - top)
		total += p[k]
	}
	u := mul(s.Float64(), total)
	for k, w := range p {
		if u < w {
			return k
		}
		u -= w
	}
	// Rounding left u at the top: the last component with weight.
	for k := len(p) - 1; k >= 0; k-- {
		if p[k] > 0 {
			return k
		}
	}
	return 0
}

// UpdateComponents draws each cell's component, its rates integrated out.
func (m *Model) UpdateComponents() {
	m.each(m.Cells, func(lo, hi int) {
		logw := make([]float64, m.H.Components)
		for c := lo; c < hi; c++ {
			m.ComponentLogWeights(c, logw)
			m.Comp[c] = pickLog(m.stream(stepComponents, c), logw)
		}
	})
}

// UpdateShapes draws each component's shapes, the cells' rates integrated out, by
// Chinese-restaurant-table augmentation: ℓ_cg ~ CRT(n_cg, r_kg) for its cells, then
// r_kg ~ Gamma(a_r + Σℓ, b_r + Σ log(1 + V_c/β_kg)).
func (m *Model) UpdateShapes() {
	g := m.Genes
	members := m.members()
	m.each(m.H.Components*g, func(lo, hi int) {
		for item := lo; item < hi; item++ {
			k, j := item/g, item%g
			s := m.stream(stepShapes, item)
			r, beta := m.Shape[item], m.RateParam[item]
			tables := 0
			var rate float64
			for _, c := range members[k] {
				tables += s.CRT(int(m.Counts[c*g+j]), r)
				rate += dmath.Log(1 + m.Volume[c]/beta)
			}
			m.Shape[item] = s.Gamma(m.H.ShapeA+float64(tables), 1/(m.H.ShapeB+rate))
		}
	})
}

// UpdateRates draws each cell's rates: λ_cg ~ Gamma(r + n_cg, β + V_c), its component's r and β.
func (m *Model) UpdateRates() {
	g := m.Genes
	m.each(m.Cells, func(lo, hi int) {
		for c := lo; c < hi; c++ {
			s := m.stream(stepRates, c)
			k := m.Comp[c]
			var sum float64
			for j := 0; j < g; j++ {
				l := s.Gamma(m.Shape[k*g+j]+float64(m.Counts[c*g+j]), 1/(m.RateParam[k*g+j]+m.Volume[c]))
				m.Rate[c*g+j] = l
				sum += l
			}
			m.RateSum[c] = sum
		}
	})
}

// UpdateRateParams draws each component's rate parameters:
// β_kg ~ Gamma(a_β + N_k r_kg, φ_g + Σ_{c in k} λ_cg).
func (m *Model) UpdateRateParams() {
	g := m.Genes
	members := m.members()
	m.each(m.H.Components*g, func(lo, hi int) {
		for item := lo; item < hi; item++ {
			k, j := item/g, item%g
			var sum float64
			for _, c := range members[k] {
				sum += m.Rate[c*g+j]
			}
			shape := m.H.RateA + mul(float64(len(members[k])), m.Shape[item])
			m.RateParam[item] = m.stream(stepRateParams, item).Gamma(shape, 1/(m.Scale[j]+sum))
		}
	})
}

// UpdateScales draws each gene's scale: φ_g ~ Gamma(e_φ + K a_β, f_φ + Σ_k β_kg).
func (m *Model) UpdateScales() {
	g, k := m.Genes, m.H.Components
	shape := m.H.ScaleE + mul(float64(k), m.H.RateA)
	for j := 0; j < g; j++ {
		var sum float64
		for c := 0; c < k; c++ {
			sum += m.RateParam[c*g+j]
		}
		m.Scale[j] = m.stream(stepScales, j).Gamma(shape, 1/(m.H.ScaleFUm+sum))
	}
}

// UpdateProportions draws the components' proportions: π ~ Dirichlet(α + N_k).
func (m *Model) UpdateProportions() {
	alpha := make([]float64, m.H.Components)
	for k := range alpha {
		alpha[k] = m.H.Alpha
	}
	for _, k := range m.Comp {
		alpha[k]++
	}
	m.stream(stepProportions, 0).Dirichlet(alpha, m.Pi)
}

// UpdateVolumes draws each component's volume prior from its normal–inverse-gamma posterior
// given its cells' log volumes; in exact mode every component keeps the prior's means.
func (m *Model) UpdateVolumes() {
	h := m.H
	if h.Exact {
		for k := range m.VolMean {
			m.VolMean[k], m.VolVar[k] = h.VolumeMean, h.VolumeB/(h.VolumeA-1)
		}
		return
	}
	for k, cells := range m.members() {
		n := float64(len(cells))
		var sum float64
		y := make([]float64, len(cells))
		for i, c := range cells {
			y[i] = dmath.Log(m.Volume[c])
			sum += y[i]
		}
		mean := 0.0
		if len(cells) > 0 {
			mean = sum / n
		}
		var ss float64
		for _, v := range y {
			ss += mul(v-mean, v-mean)
		}
		kappa := h.VolumeKappa + n
		mn := (mul(h.VolumeKappa, h.VolumeMean) + mul(n, mean)) / kappa
		a := h.VolumeA + n/2
		d := mean - h.VolumeMean
		b := h.VolumeB + ss/2 + mul(mul(h.VolumeKappa, n), mul(d, d))/(2*kappa)
		s := m.stream(stepVolumes, k)
		variance := 1 / s.Gamma(a, 1/b)
		m.VolVar[k] = variance
		m.VolMean[k] = mn + mul(math.Sqrt(variance/kappa), s.Normal())
	}
}
