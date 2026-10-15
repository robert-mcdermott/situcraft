package simulate

// The simulation's validation: what it makes follows the distributions it specifies. Each test
// draws a tissue with a fixed seed and tests what it drew (Kolmogorov–Smirnov for continuous
// quantities, χ² for counts, z for shares) at the 0.001 level, so a test always passes or always
// fails: a failure is a simulation that does not do what it says.

import (
	"math"
	"sort"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

const alpha = 1e-3

// zLimit is the two-sided 0.001 level of a standard normal.
const zLimit = 3.29

func never() bool                     { return false }
func quiet(float64, string) error     { return nil }
func pixelUm(p Params, v float32) int { return int(float64(v) / p.PixelUm) }
func interior(p Params, x, y, margin float64) bool {
	return x >= margin && y >= margin && x <= p.WidthUm-margin && y <= p.HeightUm-margin
}

// gofTissue is a tissue of 300 × 300 µm of the standard preset, made once.
var gofCache struct {
	t    *tissue
	pn   *panel
	mols []molecule
}

func gofTissue(t *testing.T) (*tissue, *panel, []molecule) {
	t.Helper()
	if gofCache.t == nil {
		p := Presets["standard"]
		p.WidthUm, p.HeightUm, p.Domains = 300, 300, 4
		tis, pn, mols, _, err := simulateTissue(p, never, quiet)
		if err != nil {
			t.Fatal(err)
		}
		gofCache.t, gofCache.pn, gofCache.mols = tis, pn, mols
	}
	return gofCache.t, gofCache.pn, gofCache.mols
}

// margin is far enough from the field's edges that no molecule of a cell within it is displaced
// out of the field (over six of the wide displacement's σ beyond the cell's reach).
func margin(c *cell, p Params) float64 { return mul(1.3, c.a) + 6*p.WideUm + 1 }

// Each cell's count of each gene is negative binomial with the mean its expected total and its
// type's profile give, and its type's dispersion: the randomized probability integral transform
// of every count is uniform.
func TestCountsAreNegativeBinomial(t *testing.T) {
	tis, pn, mols := gofTissue(t)
	p := tis.p
	counts := map[[2]int32]int{}
	for _, m := range mols {
		if m.origin == FromCell {
			counts[[2]int32{int32(m.cell), m.feature}]++
		}
	}
	jitter := random.New(1, 99)
	var pit []float64
	// The variance: (x − m)² − (m + m²/r) has mean 0; summed and studentized, it is z.
	var excess, excess2 float64
	for k := range tis.cells {
		c := &tis.cells[k]
		if !interior(p, c.x, c.y, margin(c, p)) {
			continue
		}
		total := tis.expectedTotal(k, random.New(p.Seed, streamCells+uint64(k)))
		r := p.Types[c.typ].Dispersion
		for g := 0; g < pn.genes; g++ {
			mean := mul(total, pn.profiles[c.typ][g])
			x := counts[[2]int32{int32(k + 1), int32(g)}]
			below := 0.0
			for i := 0; i < x; i++ {
				below += stats.NegativeBinomialPMF(i, mean, r)
			}
			pit = append(pit, below+jitter.Float64()*stats.NegativeBinomialPMF(x, mean, r))
			d := (float64(x)-mean)*(float64(x)-mean) - mean - mean*mean/r
			excess += d
			excess2 += d * d
		}
	}
	if len(pit) < 80000 {
		t.Fatalf("only %d counts tested", len(pit))
	}
	if d, pv := stats.KS(pit, func(u float64) float64 { return u }); pv < alpha {
		t.Errorf("counts: Kolmogorov–Smirnov D %.4f, p %.2g, over %d counts", d, pv, len(pit))
	}
	if z := excess / math.Sqrt(excess2); math.Abs(z) > zLimit {
		t.Errorf("counts' variance: z %.2f against the negative binomial's", z)
	}
}

// Each cell's molecules lie in its pixels (its nucleus's with the nuclear enrichment's weight) and
// within its depth (its nucleus's for a nuclear molecule).
func TestMoleculesLieInTheirCells(t *testing.T) {
	tis, _, mols := gofTissue(t)
	p := tis.p
	type tally struct{ n, nuclear int }
	byCell := map[uint32]*tally{}
	outside := 0
	for _, m := range mols {
		if m.origin != FromCell {
			continue
		}
		c := &tis.cells[m.cell-1]
		px, py := pixelUm(p, m.tx), pixelUm(p, m.ty)
		i := py*tis.w + px
		if tis.labels[i] != m.cell {
			// float32 can round a point within 10⁻⁵ µm of a pixel's edge into the next.
			fx, fy := float64(m.tx)/p.PixelUm, float64(m.ty)/p.PixelUm
			if math.Abs(fx-math.Round(fx)) > 1e-4 && math.Abs(fy-math.Round(fy)) > 1e-4 {
				outside++
			}
			continue
		}
		nuclear := c.nucleus && tis.nuclei[i] == m.cell
		lo, hi := c.zLo, c.zHi
		if nuclear {
			lo, hi = c.nzLo, c.nzHi
		}
		if float64(m.tz) < lo-1e-5 || float64(m.tz) > hi+1e-5 {
			t.Fatalf("a molecule of cell %d at depth %g, outside %g–%g", m.cell, m.tz, lo, hi)
		}
		if !interior(p, c.x, c.y, margin(c, p)) || !c.nucleus {
			continue
		}
		tl := byCell[m.cell]
		if tl == nil {
			tl = &tally{}
			byCell[m.cell] = tl
		}
		tl.n++
		if nuclear {
			tl.nuclear++
		}
	}
	if outside > 0 {
		t.Errorf("%d molecules lie outside their cells", outside)
	}
	// The nuclear share: each cell's molecules fall in its nucleus with probability
	// enrichment × nucleus ÷ (enrichment × nucleus + the rest of the cell).
	var observed, expected, variance float64
	for l, tl := range byCell {
		c := &tis.cells[l-1]
		nuc := mul(p.NuclearEnrichment, float64(len(c.nucleusPixel)))
		q := nuc / (nuc + float64(len(c.pixels)-len(c.nucleusPixel)))
		observed += float64(tl.nuclear)
		expected += float64(tl.n) * q
		variance += float64(tl.n) * q * (1 - q)
	}
	if z := (observed - expected) / math.Sqrt(variance); math.Abs(z) > zLimit {
		t.Errorf("nuclear molecules: %.0f, expected %.1f (z %.2f)", observed, expected, z)
	}
}

// Molecules are observed displaced by the mixture of normals: the wide share displaced with σ
// wide, the rest with σ narrow, in x and y, and with σ z in depth (within the section).
func TestDisplacementsAreTheMixture(t *testing.T) {
	tis, _, mols := gofTissue(t)
	p := tis.p
	var wide, narrow, depth []float64
	cellMolecules := 0
	for _, m := range mols {
		if m.origin != FromCell {
			continue
		}
		c := &tis.cells[m.cell-1]
		if !interior(p, c.x, c.y, margin(c, p)) {
			continue
		}
		cellMolecules++
		dx, dy := float64(m.x)-float64(m.tx), float64(m.y)-float64(m.ty)
		if m.wide {
			wide = append(wide, dx/p.WideUm, dy/p.WideUm)
		} else {
			narrow = append(narrow, dx/p.NarrowUm, dy/p.NarrowUm)
		}
		if tz := float64(m.tz); tz > 6*p.ZUm && tz < p.DepthUm-6*p.ZUm {
			depth = append(depth, (float64(m.z)-tz)/p.ZUm)
		}
		if m.z < 0 || float64(m.z) > p.DepthUm {
			t.Fatalf("a molecule observed at depth %g, outside the section", m.z)
		}
	}
	for name, v := range map[string][]float64{"wide": wide, "narrow": narrow, "depth": depth} {
		if d, pv := stats.KS(v, stats.NormalCDF); pv < alpha {
			t.Errorf("%s displacements: Kolmogorov–Smirnov D %.4f, p %.2g over %d", name, d, pv, len(v))
		}
	}
	n, k := float64(cellMolecules), float64(len(wide)/2)
	if z := (k - n*p.WideShare) / math.Sqrt(n*p.WideShare*(1-p.WideShare)); math.Abs(z) > zLimit {
		t.Errorf("wide displacements: %.0f of %.0f, a share of %.4f (z %.2f)", k, n, k/n, z)
	}
}

// Quality scores: below 20 with the low-quality share, uniform in 4–20; otherwise 40 less an
// exponential of mean 3, no lower than 20.1. Background molecules take their own low share.
func TestQualityScores(t *testing.T) {
	tis, _, mols := gofTissue(t)
	p := tis.p
	var low, high []float64
	cell, bgLow, bg := 0, 0, 0
	for _, m := range mols {
		q := float64(m.qv)
		if m.origin != FromCell {
			bg++
			if q < 20 {
				bgLow++
			}
			continue
		}
		cell++
		if q < 20 {
			low = append(low, (q-4)/16)
		} else {
			high = append(high, 40-q)
		}
	}
	if d, pv := stats.KS(low, func(u float64) float64 { return math.Min(1, math.Max(0, u)) }); pv < alpha {
		t.Errorf("low quality scores: D %.4f, p %.2g", d, pv)
	}
	// The exponential, its tail beyond 19.9 at 19.9 (float32 rounds 40 − 20.1 a little).
	cdf := func(y float64) float64 {
		if y >= 19.9-1e-5 {
			return 1
		}
		return 1 - math.Exp(-y/3)
	}
	if d, pv := stats.KS(high, cdf); pv < alpha {
		t.Errorf("high quality scores: D %.4f, p %.2g", d, pv)
	}
	share := func(name string, k, n int, q float64) {
		if z := (float64(k) - float64(n)*q) / math.Sqrt(float64(n)*q*(1-q)); math.Abs(z) > zLimit {
			t.Errorf("%s: %d of %d (z %.2f)", name, k, n, z)
		}
	}
	share("cells' low-quality molecules", len(low), cell, p.LowQuality)
	share("background's low-quality molecules", bgLow, bg, p.BackgroundLowQuality)
}

// The background: each tile's gene molecules are Poisson with the background density times its
// area, spread over the genes as the tissue's mean expression, and control molecules Poisson with
// the control density; all lie uniformly in the tile and the section's depth.
func TestBackgroundIsPoissonAndUniform(t *testing.T) {
	p := Presets["standard"]
	p.WidthUm, p.HeightUm = 2000, 2000 // 1,600 tiles; the tiles need no cells
	tis := newTissue(p)
	share := make([]float64, len(p.Types))
	for k := range share {
		share[k] = 1 / float64(len(share))
	}
	pn := makePanel(p, random.New(p.Seed, streamPanel), share)
	tiles := tis.tiles()
	geneTotals, controlTotals := make([]int, tiles), make([]int, tiles)
	byGene := make([]float64, pn.genes)
	var ux, uz []float64
	cols := int(math.Ceil(p.WidthUm / backgroundTile))
	for tile := 0; tile < tiles; tile++ {
		x0 := float64(tile%cols) * backgroundTile
		for _, m := range tis.background(tile, pn, nil) {
			if int(m.feature) < pn.genes {
				geneTotals[tile]++
				byGene[m.feature]++
			} else {
				controlTotals[tile]++
			}
			if len(ux) < 50000 {
				ux = append(ux, (float64(m.x)-x0)/backgroundTile)
				uz = append(uz, float64(m.z)/p.DepthUm)
			}
		}
	}
	area := backgroundTile * backgroundTile / 100
	poisson := func(name string, totals []int, mean float64) {
		top := 0
		hist := map[int]float64{}
		for _, n := range totals {
			hist[n]++
			top = max(top, n)
		}
		observed, expected := make([]float64, top+2), make([]float64, top+2)
		rest := 1.0
		for k := 0; k <= top; k++ {
			observed[k] = hist[k]
			pk := stats.PoissonPMF(k, mean)
			expected[k] = pk * float64(len(totals))
			rest -= pk
		}
		expected[top+1] = math.Max(rest, 0) * float64(len(totals))
		if stat, df, pv := stats.ChiSquare(observed, expected, 0); pv < alpha {
			t.Errorf("%s per tile: χ² %.1f on %d degrees of freedom, p %.2g", name, stat, df, pv)
		}
	}
	poisson("background gene molecules", geneTotals, p.Background*area)
	poisson("control molecules", controlTotals, p.ControlBg*area*float64(p.Controls+p.Codewords))
	var all float64
	for _, n := range byGene {
		all += n
	}
	expected := make([]float64, pn.genes)
	for g := range expected {
		expected[g] = pn.tissueMean[g] * all
	}
	if stat, df, pv := stats.ChiSquare(byGene, expected, 0); pv < alpha {
		t.Errorf("background genes: χ² %.1f on %d degrees of freedom, p %.2g", stat, df, pv)
	}
	uniform := func(u float64) float64 { return math.Min(1, math.Max(0, u)) }
	for name, v := range map[string][]float64{"x within the tile": ux, "depth": uz} {
		if d, pv := stats.KS(v, uniform); pv < alpha {
			t.Errorf("background %s: D %.4f, p %.2g", name, d, pv)
		}
	}
}

// Cells: each type's nominal areas are log-normal about its median with its spread; an isolated
// cell's pixels cover its nominal area; and the types are drawn in each domain's proportions.
// (A sparse tissue, so that neighbours neither crowd out large cells nor take each other's pixels.)
func TestCellsFollowTheirTypes(t *testing.T) {
	p := Presets["standard"]
	p.WidthUm, p.HeightUm, p.PixelUm, p.Fill = 1200, 1200, 0.4, 0.06
	tis := newTissue(p)
	s := random.New(p.Seed, streamLayout)
	tis.makeDomains(s)
	tis.placeCells(s)
	nominal := make([]float64, len(tis.cells))
	for k, c := range tis.cells {
		nominal[k] = mul(mul(math.Pi, c.a), c.b)
	}
	var z []float64
	observed, expected := make([]float64, len(p.Types)), make([]float64, len(p.Types))
	for k, c := range tis.cells {
		ty := p.Types[c.typ]
		z = append(z, (math.Log(nominal[k])-math.Log(ty.AreaUm2))/ty.AreaSpread)
		observed[c.typ]++
		for j, w := range tis.domainAt(c.x, c.y).mix {
			expected[j] += w
		}
	}
	if len(z) < 1000 {
		t.Fatalf("only %d cells", len(z))
	}
	if d, pv := stats.KS(z, stats.NormalCDF); pv < alpha {
		t.Errorf("standardized log areas: D %.4f, p %.2g over %d cells", d, pv, len(z))
	}
	if stat, df, pv := stats.ChiSquare(observed, expected, 0); pv < alpha {
		t.Errorf("types: χ² %.1f on %d degrees of freedom, p %.2g (observed %v, expected %v)", stat, df, pv, observed, expected)
	}
	// Isolated cells away from the edges: pixels ÷ nominal area, its median near 1 (the outline's
	// harmonics add about 0.6% on average).
	centres := append([]cell(nil), tis.cells...)
	if err := tis.rasterize(never); err != nil {
		t.Fatal(err)
	}
	var ratio []float64
	for _, c := range tis.cells {
		reach := mul(1.3, c.a)
		if !interior(p, c.x, c.y, reach+1) {
			continue
		}
		alone := true
		for _, o := range centres {
			if d := math.Hypot(o.x-c.x, o.y-c.y); d > 0 && d < reach+mul(1.3, o.a) {
				alone = false
				break
			}
		}
		if alone {
			ratio = append(ratio, float64(len(c.pixels))*p.PixelUm*p.PixelUm/(math.Pi*c.a*c.b))
		}
	}
	sort.Float64s(ratio)
	if len(ratio) < 300 {
		t.Fatalf("only %d isolated cells", len(ratio))
	}
	if m := ratio[len(ratio)/2]; m < 0.97 || m > 1.04 {
		t.Errorf("isolated cells cover %.3f of their nominal areas (median of %d)", m, len(ratio))
	}
}
