package simulate

// The molecules.
//
// The panel: genes SIM001…, each type's markers among them; negative control probes and codewords
// named as Xenium names them (NegControlProbe_…, NegControlCodeword_…), so readers classify them.
// Each gene has a base level (gamma, shape 0.6); a type's profile is the base levels with its
// markers multiplied by its fold and the other types' markers damped to a fifth, normalized to
// sum to one.
//
// A cell's gene transcripts: its type's density times its area (per 100 µm²) times a size factor
// (log-normal, σ 0.25) is its expected total; each gene's count is negative binomial with that
// total times the gene's share of the profile as its mean and the type's dispersion. Each
// molecule lies at a uniform point of a pixel of the cell, chosen with the nucleus's pixels
// weighted by the nuclear enrichment (cells whose nucleus is outside the section have none), and
// at a uniform depth of the cell's range (the nucleus's for a nuclear molecule). It is observed
// displaced by a normal in x and y (σ wide for a share of molecules, narrow for the rest) and in
// z; one displaced outside the field is lost, one outside the section's faces is observed at the
// face.
//
// The background: in 50 µm tiles, each gene's count is Poisson with the background density times
// the tile's area times the gene's share of the tissue's mean expression; each control probe's
// and codeword's with the control density. Background molecules lie uniformly in the tile and the
// section's depth.
//
// Quality: a cell's molecule is of quality below 20 with the low-quality share (uniform in 4–20),
// else 40 less an exponential of mean 3 (no lower than 20.1); background molecules and controls
// take the background's low-quality share.

import (
	"fmt"
	"math"

	"situcraft/internal/random"
)

// Origins of molecules.
const (
	FromCell = iota
	FromBackground
	FromControlProbe
	FromControlCodeword
)

// molecule is one observed molecule with the truth about it.
type molecule struct {
	x, y, z    float32 // observed, µm
	tx, ty, tz float32 // true
	feature    int32   // genes first, then control probes, then codewords
	cell       uint32  // true cell (label), 0 for none
	origin     uint8
	wide       bool // displaced by the wide component
	qv         float32
}

type panel struct {
	names, ids, kinds []string // kinds as the Matrix Market features file names them
	genes             int
	profiles          [][]float64 // by type, over the genes
	tissueMean        []float64   // the genes' shares of the tissue's mean expression
}

// Streams: one for the layout, one for the panel, one per cell, one per background tile.
const (
	streamLayout = 1
	streamPanel  = 2
	streamCells  = 1 << 32
	streamTiles  = 1 << 33
	streamImage  = 1 << 34
)

func makePanel(p Params, s *random.Stream, typeShare []float64) *panel {
	pn := &panel{genes: p.Genes}
	for g := 0; g < p.Genes; g++ {
		pn.names = append(pn.names, fmt.Sprintf("SIM%03d", g+1))
		pn.ids = append(pn.ids, fmt.Sprintf("SIMG%07d", g+1))
		pn.kinds = append(pn.kinds, "Gene Expression")
	}
	for c := 0; c < p.Controls; c++ {
		name := fmt.Sprintf("NegControlProbe_SIM%02d", c+1)
		pn.names, pn.ids, pn.kinds = append(pn.names, name), append(pn.ids, name), append(pn.kinds, "Negative Control Probe")
	}
	for c := 0; c < p.Codewords; c++ {
		name := fmt.Sprintf("NegControlCodeword_%04d", 500+c)
		pn.names, pn.ids, pn.kinds = append(pn.names, name), append(pn.ids, name), append(pn.kinds, "Negative Control Codeword")
	}
	base := make([]float64, p.Genes)
	for g := range base {
		base[g] = s.Gamma(0.6, 1) + 1e-3
	}
	markerOf := make([]int, p.Genes)
	for g := range markerOf {
		markerOf[g] = -1
	}
	next := 0
	for k, ty := range p.Types {
		for m := 0; m < ty.Markers; m++ {
			markerOf[next] = k
			next++
		}
	}
	pn.tissueMean = make([]float64, p.Genes)
	for k, ty := range p.Types {
		prof := make([]float64, p.Genes)
		var sum float64
		for g := range prof {
			switch markerOf[g] {
			case -1:
				prof[g] = base[g]
			case k:
				prof[g] = mul(base[g]+0.5, ty.Fold)
			default:
				prof[g] = mul(base[g], 0.2)
			}
			sum += prof[g]
		}
		for g := range prof {
			prof[g] /= sum
			pn.tissueMean[g] += mul(typeShare[k], prof[g])
		}
		pn.profiles = append(pn.profiles, prof)
	}
	var total float64
	for _, v := range pn.tissueMean {
		total += v
	}
	for g := range pn.tissueMean {
		pn.tissueMean[g] /= total
	}
	return pn
}

// quality draws a molecule's quality score.
func quality(s *random.Stream, low float64) float32 {
	if s.Float64() < low {
		return float32(4 + mul(16, s.Float64()))
	}
	return float32(math.Max(20.1, 40-mul(3, s.Exponential())))
}

// expectedTotal is cell k's expected gene transcripts: its type's density times its area times
// its size factor, the first draw of its stream.
func (t *tissue) expectedTotal(k int, s *random.Stream) float64 {
	c := &t.cells[k]
	area := mul(float64(len(c.pixels)), mul(t.p.PixelUm, t.p.PixelUm))
	return mul(mul(t.p.Types[c.typ].Density, area/100), s.LogNormal(0, 0.25))
}

// cellMolecules draws a cell's molecules.
func (t *tissue) cellMolecules(k int, pn *panel, out []molecule) []molecule {
	p := t.p
	ps := p.PixelUm
	c := &t.cells[k]
	ty := p.Types[c.typ]
	s := random.New(p.Seed, streamCells+uint64(k))
	total := t.expectedTotal(k, s)
	// The nucleus's pixels weigh the nuclear enrichment each, the others one.
	nuclear := 0.0
	if c.nucleus {
		nuclear = mul(p.NuclearEnrichment, float64(len(c.nucleusPixel)))
	}
	cyto := float64(len(c.pixels) - len(c.nucleusPixel))
	if !c.nucleus {
		cyto = float64(len(c.pixels))
	}
	for g := 0; g < pn.genes; g++ {
		n := s.NegativeBinomial(mul(total, pn.profiles[c.typ][g]), ty.Dispersion)
		for i := 0; i < n; i++ {
			m := molecule{feature: int32(g), cell: uint32(k + 1), origin: FromCell}
			var px int32
			var zLo, zHi float64
			if c.nucleus && mul(s.Float64(), nuclear+cyto) < nuclear {
				px = c.nucleusPixel[s.IntN(len(c.nucleusPixel))]
				zLo, zHi = c.nzLo, c.nzHi
			} else {
				// A pixel of the cell outside its nucleus (or anywhere, without one).
				for {
					px = c.pixels[s.IntN(len(c.pixels))]
					if !c.nucleus || t.nuclei[px] != uint32(k+1) {
						break
					}
				}
				zLo, zHi = c.zLo, c.zHi
			}
			tx := mul(float64(int(px)%t.w)+s.Float64(), ps)
			tyy := mul(float64(int(px)/t.w)+s.Float64(), ps)
			tz := zLo + mul(s.Float64(), zHi-zLo)
			m.tx, m.ty, m.tz = float32(tx), float32(tyy), float32(tz)
			sigma := p.NarrowUm
			if s.Float64() < p.WideShare {
				sigma, m.wide = p.WideUm, true
			}
			ox, oy := tx+mul(sigma, s.Normal()), tyy+mul(sigma, s.Normal())
			oz := math.Min(p.DepthUm, math.Max(0, tz+mul(p.ZUm, s.Normal())))
			m.qv = quality(s, p.LowQuality)
			if ox < 0 || oy < 0 || ox >= p.WidthUm || oy >= p.HeightUm {
				continue // displaced out of the field: lost
			}
			m.x, m.y, m.z = float32(ox), float32(oy), float32(oz)
			out = append(out, m)
		}
	}
	return out
}

// backgroundTile is the size of the tiles background molecules are drawn in, µm.
const backgroundTile = 50.0

// background draws the background and control molecules of a tile.
func (t *tissue) background(tile int, pn *panel, out []molecule) []molecule {
	p := t.p
	cols := int(math.Ceil(p.WidthUm / backgroundTile))
	x0, y0 := mul(float64(tile%cols), backgroundTile), mul(float64(tile/cols), backgroundTile)
	w, h := math.Min(backgroundTile, p.WidthUm-x0), math.Min(backgroundTile, p.HeightUm-y0)
	s := random.New(p.Seed, streamTiles+uint64(tile))
	area := mul(w, h)
	draw := func(feature int, mean float64, origin uint8) {
		n := s.Poisson(mean)
		for i := 0; i < n; i++ {
			x, y := x0+mul(s.Float64(), w), y0+mul(s.Float64(), h)
			z := mul(s.Float64(), p.DepthUm)
			out = append(out, molecule{x: float32(x), y: float32(y), z: float32(z), tx: float32(x), ty: float32(y), tz: float32(z),
				feature: int32(feature), origin: origin, qv: quality(s, p.BackgroundLowQuality)})
		}
	}
	for g := 0; g < pn.genes; g++ {
		draw(g, mul(mul(p.Background, area/100), pn.tissueMean[g]), FromBackground)
	}
	for c := 0; c < p.Controls; c++ {
		draw(pn.genes+c, mul(p.ControlBg, area/100), FromControlProbe)
	}
	for c := 0; c < p.Codewords; c++ {
		draw(pn.genes+p.Controls+c, mul(p.ControlBg, area/100), FromControlCodeword)
	}
	return out
}

// tiles is the number of background tiles.
func (t *tissue) tiles() int {
	return int(math.Ceil(t.p.WidthUm/backgroundTile)) * int(math.Ceil(t.p.HeightUm/backgroundTile))
}
