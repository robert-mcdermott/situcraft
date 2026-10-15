package refine

import (
	"testing"
	"time"

	"situcraft/internal/random"
)

// typedCells are counts of cells of known types: each type's markers at ten times the base rate.
func typedCells(cells, genes, types int, seed uint64) (*Model, []int) {
	s := random.New(seed, 1)
	h := DefaultHyper()
	h.Components = 2 * types
	volumes := make([]float64, cells)
	kind := make([]int, cells)
	for c := range volumes {
		kind[c] = c % types
		volumes[c] = s.LogNormal(6.5, 0.3) // about 650 µm³
	}
	m, _ := NewModel(h, volumes, genes, 1e6, seed)
	base := make([]float64, genes)
	for g := range base {
		base[g] = s.Gamma(1, 0.0003) // about 0.2 transcripts per cell per gene
	}
	for c := range volumes {
		for g := 0; g < genes; g++ {
			rate := base[g]
			if g%types == kind[c] && g < 6*types {
				rate *= 10 // six markers per type
			}
			m.Counts[c*genes+g] = uint32(s.NegativeBinomial(volumes[c]*rate, 3))
		}
	}
	for g := range m.BgCounts {
		m.BgCounts[g] = uint32(s.Poisson(1e6 * base[g] / 50))
	}
	return m, kind
}

// The components find the types: every component's cells are of one type (purity of 0.95 or
// more), from counts alone.
func TestComponentsFindCellTypes(t *testing.T) {
	m, kind := typedCells(300, 30, 3, 21)
	m.Init()
	for i := 0; i < 80; i++ {
		m.SweepParameters()
	}
	byComp := map[int]map[int]int{}
	for c, k := range m.Comp {
		if byComp[k] == nil {
			byComp[k] = map[int]int{}
		}
		byComp[k][kind[c]]++
	}
	pure := 0
	for _, types := range byComp {
		top := 0
		for _, n := range types {
			top = max(top, n)
		}
		pure += top
	}
	if purity := float64(pure) / float64(len(m.Comp)); purity < 0.95 {
		t.Errorf("components' purity %.3f: %v", purity, byComp)
	}
}

// The time a sweep takes at the standard simulation's size (about 3,000 cells, 120 genes, 10
// components), logged.
func TestSweepTime(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	m, _ := typedCells(3000, 120, 4, 22)
	m.Init()
	start := time.Now()
	for i := 0; i < 5; i++ {
		m.SweepParameters()
	}
	t.Logf("a parameter sweep of 3,000 cells × 120 genes × 10 components: %v on %d workers", time.Since(start)/5, m.Workers)
}
