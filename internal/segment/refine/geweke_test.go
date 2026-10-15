package refine

// The whole sweep, tested jointly (Geweke J. Getting it right: joint distribution tests of
// posterior simulators. J Am Stat Assoc 2004;99:799–804. doi:10.1198/016214504000001132): draw
// parameters from their prior and data from the model; then alternate a sweep of the parameters
// given the data with fresh data given the parameters. If every update draws from its
// conditional, in a valid order, the parameters stay distributed as the prior; an error in any
// update, or in the order of the partially collapsed steps, moves them away. Each of thousands of
// independent chains is started from the joint distribution, so its last state is a draw of the
// prior when the sweep is right, and the chains' last states are compared with independent prior
// draws (two-sample Kolmogorov–Smirnov).

import (
	"math"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

// gewekeHyper keeps counts moderate (rates about 0.1 per µm³, volumes about 30 µm³).
func gewekeHyper() Hyper {
	h := DefaultHyper()
	h.Components = 2
	h.RateA = 3
	h.ScaleE, h.ScaleFUm = 4, 40
	h.BackgroundA, h.BackgroundBUm = 2, 100
	h.VolumeMean, h.VolumeKappa, h.VolumeA, h.VolumeB = math.Log(30), 1, 3, 0.5
	return h
}

const (
	gewekeCells  = 8
	gewekeGenes  = 3
	gewekeRegion = 1000.0
)

// drawPrior sets every parameter of m from the prior.
func drawPrior(m *Model, s *random.Stream) {
	h, g := m.H, m.Genes
	for j := 0; j < g; j++ {
		m.Scale[j] = s.Gamma(h.ScaleE, 1/h.ScaleFUm)
		m.Bg[j] = s.Gamma(h.BackgroundA, 1/h.BackgroundBUm)
	}
	for k := 0; k < h.Components; k++ {
		for j := 0; j < g; j++ {
			m.Shape[k*g+j] = s.Gamma(h.ShapeA, 1/h.ShapeB)
			m.RateParam[k*g+j] = s.Gamma(h.RateA, 1/m.Scale[j])
		}
		m.VolVar[k] = 1 / s.Gamma(h.VolumeA, 1/h.VolumeB)
		m.VolMean[k] = h.VolumeMean + math.Sqrt(m.VolVar[k]/h.VolumeKappa)*s.Normal()
	}
	alpha := make([]float64, h.Components)
	for k := range alpha {
		alpha[k] = h.Alpha
	}
	s.Dirichlet(alpha, m.Pi)
	for c := range m.Comp {
		u, k := s.Float64(), 0
		for k < h.Components-1 && u >= m.Pi[k] {
			u -= m.Pi[k]
			k++
		}
		m.Comp[c] = k
		var sum float64
		for j := 0; j < g; j++ {
			m.Rate[c*g+j] = s.Gamma(m.Shape[k*g+j], 1/m.RateParam[k*g+j])
			sum += m.Rate[c*g+j]
		}
		m.RateSum[c] = sum
	}
}

// drawData sets the volumes and counts from the parameters.
func drawData(m *Model, s *random.Stream) {
	g := m.Genes
	for c, k := range m.Comp {
		m.Volume[c] = math.Exp(m.VolMean[k] + math.Sqrt(m.VolVar[k])*s.Normal())
		for j := 0; j < g; j++ {
			m.Counts[c*g+j] = uint32(s.Poisson(m.Volume[c] * m.Rate[c*g+j]))
		}
	}
	for j := 0; j < g; j++ {
		m.BgCounts[j] = uint32(s.Poisson(m.Region * m.Bg[j]))
	}
}

// watched are the quantities compared.
var watched = []struct {
	name string
	of   func(m *Model) float64
}{
	{"shape r₀₀", func(m *Model) float64 { return m.Shape[0] }},
	{"shape r₁₂", func(m *Model) float64 { return m.Shape[gewekeGenes+2] }},
	{"rate parameter β₀₁", func(m *Model) float64 { return m.RateParam[1] }},
	{"gene scale φ₀", func(m *Model) float64 { return m.Scale[0] }},
	{"cell 0's rate λ₀₀", func(m *Model) float64 { return m.Rate[0] }},
	{"cell 3's rate sum", func(m *Model) float64 { return m.RateSum[3] }},
	{"background λ₀", func(m *Model) float64 { return m.Bg[0] }},
	{"proportion π₀", func(m *Model) float64 { return m.Pi[0] }},
	{"volume mean m₁", func(m *Model) float64 { return m.VolMean[1] }},
	{"volume variance s₀²", func(m *Model) float64 { return m.VolVar[0] }},
	{"cells in component 0", func(m *Model) float64 {
		n := 0.0
		for _, k := range m.Comp {
			if k == 0 {
				n++
			}
		}
		return n
	}},
}

func TestTheSweepKeepsTheJointDistribution(t *testing.T) {
	const chains, steps = 4000, 40
	h := gewekeHyper()
	volumes := make([]float64, gewekeCells)
	for i := range volumes {
		volumes[i] = 30
	}
	m, err := NewModel(h, volumes, gewekeGenes, gewekeRegion, 1)
	if err != nil {
		t.Fatal(err)
	}
	m.Workers = 1
	prior := make([][]float64, len(watched))
	sampled := make([][]float64, len(watched))
	for j := 0; j < chains; j++ {
		// Independent prior draws.
		drawPrior(m, random.New(77, uint64(j)))
		for w, q := range watched {
			prior[w] = append(prior[w], q.of(m))
		}
		// A chain from the joint distribution.
		s := random.New(78, uint64(j))
		drawPrior(m, s)
		drawData(m, s)
		m.Seed, m.Sweep = uint64(j), 0
		for it := 0; it < steps; it++ {
			m.SweepParameters()
			drawData(m, s)
		}
		for w, q := range watched {
			sampled[w] = append(sampled[w], q.of(m))
		}
	}
	for w, q := range watched {
		if d, p := stats.KS2(prior[w], sampled[w]); p < alpha {
			t.Errorf("%s: the sweep's distribution differs from the prior's (D %.4f, p %.2g)", q.name, d, p)
		}
	}
}
