// Package simulate makes tissues whose every cell and molecule is known, and writes each as a
// Xenium-shaped output bundle, labelled simulated, so that any segmentation, SituCraft's or
// another tool's, can be scored against the truth.
//
// A tissue is a field of cells of several types, in domains of different composition. Each cell
// grows from its nucleus to a size, elongation and irregular outline of its type, competing for
// space with its neighbours; some cells' nuclei lie outside the section and are not seen. Each
// cell expresses the panel's genes with negative-binomial counts around its type's profile, in
// proportion to its area; its molecules lie within it, in the section's depth, and some are
// observed displaced (diffused), a few far. Background molecules, ambient ones of every gene and
// false decodes of every codeword (genes and negative controls alike), fall anywhere. Every molecule has a quality score, lower for
// background. The instrument the bundle comes from segments by expanding the nuclei it sees.
//
// The simulator shares no code with SituCraft's segmentation: only the file writers and a
// polygon tracer. The same parameters and seed give the same bundle, byte for byte, on any
// processor (internal/random, internal/dmath).
package simulate

import (
	"errors"
	"fmt"
	"math"
)

// CellType is a kind of cell: its share of each domain comes from Params.Domains.
type CellType struct {
	Name string `json:"name"`
	// AreaUm2 is the median cell area (µm²) and AreaSpread the log-normal σ around it.
	AreaUm2    float64 `json:"area_um2"`
	AreaSpread float64 `json:"area_spread"`
	// Elongation is the largest ratio of a cell's long axis to its short one (drawn from 1 up).
	Elongation float64 `json:"elongation"`
	// NucleusShare is the nucleus's share of the cell's radius.
	NucleusShare float64 `json:"nucleus_share"`
	// Density is the expected gene transcripts of a cell per 100 µm² of its area.
	Density float64 `json:"density_per_100um2"`
	// Markers is how many genes mark the type, each expressed Fold times its base level.
	Markers int     `json:"markers"`
	Fold    float64 `json:"marker_fold"`
	// Dispersion is the negative binomial's r (variance m + m²/r).
	Dispersion float64 `json:"dispersion"`
	// HeightUm is the cell's extent in z.
	HeightUm float64 `json:"height_um"`
}

// Params are a simulation's every setting, recorded in situcraft-simulation.json.
type Params struct {
	Seed     uint64  `json:"seed"`
	WidthUm  float64 `json:"width_um"`
	HeightUm float64 `json:"height_um"`
	DepthUm  float64 `json:"depth_um"`
	PixelUm  float64 `json:"pixel_um"`
	// Fill is the share of the field the cells' nominal areas cover (gaps appear between them).
	Fill float64 `json:"fill"`
	// Domains is the number of tissue domains, each a Voronoi region with its own mix of types,
	// one of which dominates it.
	Domains int        `json:"domains"`
	Types   []CellType `json:"types"`
	// Genes is the panel's genes (the types' markers among them); Controls the negative control
	// probes and Codewords the negative control codewords.
	Genes     int `json:"genes"`
	Controls  int `json:"control_probes"`
	Codewords int `json:"control_codewords"`
	// HiddenNuclei is the share of cells whose nucleus lies outside the section.
	HiddenNuclei float64 `json:"hidden_nuclei"`
	// NuclearEnrichment is how much denser a cell's molecules are over its nucleus.
	NuclearEnrichment float64 `json:"nuclear_enrichment"`
	// Diffusion: a share of molecules (WideShare) are displaced by a wide normal of σ WideUm in
	// x and y, the rest by a narrow one of σ NarrowUm; z by ZUm.
	WideShare float64 `json:"diffusion_wide_share"`
	WideUm    float64 `json:"diffusion_wide_um"`
	NarrowUm  float64 `json:"diffusion_narrow_um"`
	ZUm       float64 `json:"diffusion_z_um"`
	// The background, molecules from no cell, of two kinds:
	//   - Ambient: gene molecules per 100 µm², shared among the genes in proportion to their mean
	//     expression (RNA loose in the tissue);
	//   - FalseDecodes: molecules per 100 µm² on every codeword alike, genes and controls (decoding
	//     errors and probes bound off target), which is what the negative controls measure.
	Ambient      float64 `json:"ambient_per_100um2"`
	FalseDecodes float64 `json:"false_decodes_per_100um2"`
	// LowQuality is the share of cells' molecules with a quality below 20; background's is
	// BackgroundLowQuality.
	LowQuality           float64 `json:"low_quality"`
	BackgroundLowQuality float64 `json:"background_low_quality"`
	// ExpansionUm is how far the simulated instrument grows the nuclei it sees into cells.
	ExpansionUm float64 `json:"instrument_expansion_um"`
	// A mosaic, for a tissue too large to hold whole (mosaic.go): TilesAcross × TilesDown tiles,
	// each the field above (WidthUm × HeightUm, whole 512-pixel blocks each way), a tissue of its
	// own (its own layout, cells, molecules and stain; the panel and every other parameter
	// shared), written into one bundle a tile at a time. 0 or 1: one field.
	TilesAcross int `json:"tiles_across,omitempty"`
	TilesDown   int `json:"tiles_down,omitempty"`
	// PostXenium asks for post-Xenium images of the section (postxenium.go): an H&E, an
	// immunofluorescence image, or both, each placed by a known transform.
	PostXenium *PostXenium `json:"post_xenium,omitempty"`

	// A sample of a simulated cohort (cohort.go) differs from its cohort's other sections by these;
	// each is absent for a tissue of its own.
	//   - PanelSeed: the panel's stream's seed, shared by the cohort's sections (the seed's own if 0).
	//   - MixSeed: the seed of the domains' mixes' stream, shared likewise (the layout's own if 0).
	//   - Retries: how many other places in its domain a candidate cell refused for crowding tries
	//     before it is dropped (0: dropped at once).
	//   - TypeOdds: each type's weight in every domain's mix multiplied by it, then the mix
	//     normalized: the type's odds against each other type multiplied by it, in every domain.
	//   - DomainOdds: a type's weight multiplied likewise, in the domains another type leads.
	//   - LevelFolds: each type's expected count of each gene multiplied (by type, over the genes).
	//   - RunName, RegionName, SlideID: what experiment.xenium and the metrics name the run, the
	//     region and the slide.
	PanelSeed  uint64       `json:"panel_seed,omitempty"`
	MixSeed    uint64       `json:"mix_seed,omitempty"`
	Retries    int          `json:"placement_retries,omitempty"`
	TypeOdds   []float64    `json:"type_odds,omitempty"`
	DomainOdds []DomainOdds `json:"domain_odds,omitempty"`
	LevelFolds [][]float64  `json:"level_folds,omitempty"`
	RunName    string       `json:"run_name,omitempty"`
	RegionName string       `json:"region_name,omitempty"`
	SlideID    string       `json:"slide_id,omitempty"`

	// part is a mosaic's tile, from 0: its streams' own (the first tile's, a single field's, are
	// the seed's).
	part uint64
}

// DomainOdds multiplies a type's weight in the mixes of the domains another type leads (its
// odds against each other type there).
type DomainOdds struct {
	Leader int     `json:"leader"`
	Type   int     `json:"type"`
	Odds   float64 `json:"odds"`
}

// tiles are the mosaic's tiles across and down (1 and 1 for one field).
func (p Params) tiles() (int, int) { return max(1, p.TilesAcross), max(1, p.TilesDown) }

// seed is a stream's seed: the panel's is the panel seed (the seed itself without one), every
// other stream's the tile's.
func (p Params) seed(stream uint64) uint64 {
	if stream == streamPanel && p.PanelSeed != 0 {
		return p.PanelSeed
	}
	if stream == streamPanel || p.part == 0 {
		return p.Seed
	}
	// SplitMix64's finalizer of the tile's number (Steele, Lea and Flood 2014), mixed in.
	z := p.part * 0x9e3779b97f4a7c15
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return p.Seed ^ (z ^ z>>31)
}

// MaxPixels is the largest image a simulation makes (its memory grows with it): a field's, or a
// mosaic tile's.
const MaxPixels = 50e6

// mosaicBlock is the side (pixels) a mosaic's tiles are whole multiples of: the stain's tiles'.
const mosaicBlock = 512

// Presets are named starting points: small (fast, for tests and trials) and standard.
var Presets = map[string]Params{"small": small(), "standard": standard(), "large": large()}

func standardTypes() []CellType {
	return []CellType{
		{Name: "epithelial-like", AreaUm2: 110, AreaSpread: 0.25, Elongation: 1.4, NucleusShare: 0.5, Density: 280, Markers: 8, Fold: 12, Dispersion: 4, HeightUm: 8},
		{Name: "fibroblast-like", AreaUm2: 90, AreaSpread: 0.3, Elongation: 3.2, NucleusShare: 0.42, Density: 160, Markers: 8, Fold: 12, Dispersion: 3, HeightUm: 6},
		{Name: "immune-like", AreaUm2: 45, AreaSpread: 0.2, Elongation: 1.15, NucleusShare: 0.62, Density: 260, Markers: 8, Fold: 14, Dispersion: 5, HeightUm: 6},
		{Name: "endothelial-like", AreaUm2: 70, AreaSpread: 0.25, Elongation: 4, NucleusShare: 0.4, Density: 150, Markers: 6, Fold: 12, Dispersion: 3, HeightUm: 5},
	}
}

func standard() Params {
	return Params{
		Seed: 20261014, WidthUm: 500, HeightUm: 500, DepthUm: 10, PixelUm: 0.2125, Fill: 0.85,
		Domains: 6, Types: standardTypes(), Genes: 120, Controls: 20, Codewords: 41,
		HiddenNuclei: 0.04, NuclearEnrichment: 1.5,
		WideShare: 0.2, WideUm: 3, NarrowUm: 0.5, ZUm: 0.5,
		Ambient: 3, FalseDecodes: 0.025, LowQuality: 0.08, BackgroundLowQuality: 0.35,
		ExpansionUm: 5,
	}
}

// large is a mosaic of 7 × 6 tiles, each the standard tissue on 4,096 pixels a side (870.4 µm):
// about 6.1 × 5.2 mm and 50 million transcripts, for measuring SituCraft at scale.
func large() Params {
	p := standard()
	p.WidthUm, p.HeightUm = 4096*p.PixelUm, 4096*p.PixelUm
	p.Domains = 12
	p.TilesAcross, p.TilesDown = 7, 6
	return p
}

func small() Params {
	p := standard()
	p.WidthUm, p.HeightUm, p.Domains = 160, 120, 3
	return p
}

// Check refuses parameters that cannot make a tissue, saying which.
func (p *Params) Check() error {
	var bad []string
	need := func(ok bool, what string) {
		if !ok {
			bad = append(bad, what)
		}
	}
	need(p.WidthUm >= 20 && p.HeightUm >= 20 && p.WidthUm <= 20000 && p.HeightUm <= 20000, "the field is 20 µm to 20 mm a side")
	need(p.DepthUm > 0 && p.DepthUm <= 100, "the depth is above 0 and at most 100 µm")
	need(p.PixelUm >= 0.05 && p.PixelUm <= 2, "the pixel is 0.05 to 2 µm")
	need(p.Fill > 0 && p.Fill <= 1.5, "the fill is above 0 and at most 1.5")
	need(p.Domains >= 1 && p.Domains <= 1000, "1 to 1,000 domains")
	need(len(p.Types) >= 1 && len(p.Types) <= 50, "1 to 50 cell types")
	markers := 0
	for _, t := range p.Types {
		markers += t.Markers
		need(t.Name != "", "every cell type has a name")
		need(t.AreaUm2 >= 4 && t.AreaUm2 <= 10000, fmt.Sprintf("%s: an area of 4 to 10,000 µm²", t.Name))
		need(t.AreaSpread >= 0 && t.AreaSpread <= 2, fmt.Sprintf("%s: an area spread of 0 to 2", t.Name))
		need(t.Elongation >= 1 && t.Elongation <= 10, fmt.Sprintf("%s: an elongation of 1 to 10", t.Name))
		need(t.NucleusShare > 0 && t.NucleusShare < 1, fmt.Sprintf("%s: a nucleus share between 0 and 1", t.Name))
		need(t.Density >= 0 && t.Density <= 10000, fmt.Sprintf("%s: a density of 0 to 10,000", t.Name))
		need(t.Markers >= 0 && t.Fold >= 1, fmt.Sprintf("%s: markers and a fold of 1 or more", t.Name))
		need(t.Dispersion > 0, fmt.Sprintf("%s: a positive dispersion", t.Name))
		need(t.HeightUm > 0, fmt.Sprintf("%s: a positive height", t.Name))
	}
	need(p.Genes >= markers && p.Genes >= 1 && p.Genes <= 20000, "at least as many genes as markers, at most 20,000")
	need(p.Controls >= 0 && p.Codewords >= 0 && p.Controls+p.Codewords <= 1000, "0 to 1,000 controls")
	need(p.HiddenNuclei >= 0 && p.HiddenNuclei < 1, "a hidden-nucleus share below 1")
	need(p.NuclearEnrichment >= 0.1 && p.NuclearEnrichment <= 20, "a nuclear enrichment of 0.1 to 20")
	need(p.WideShare >= 0 && p.WideShare <= 1 && p.WideUm >= 0 && p.NarrowUm >= 0 && p.ZUm >= 0, "diffusion shares and widths of 0 or more")
	need(p.Ambient >= 0 && p.FalseDecodes >= 0, "background densities of 0 or more")
	need(p.LowQuality >= 0 && p.LowQuality <= 1 && p.BackgroundLowQuality >= 0 && p.BackgroundLowQuality <= 1, "quality shares of 0 to 1")
	need(p.ExpansionUm >= 0 && p.ExpansionUm <= 50, "an instrument expansion of 0 to 50 µm")
	// About 75 bytes of memory a pixel at the peak (measured): 50 million pixels, about 1.5 mm a
	// side at Xenium's 0.2125 µm, need about 4 GB.
	if pixels := math.Ceil(p.WidthUm/p.PixelUm) * math.Ceil(p.HeightUm/p.PixelUm); pixels > MaxPixels {
		bad = append(bad, fmt.Sprintf("at most %d million pixels a field (%.0f million asked; about 1.5 mm a side at 0.2125 µm, which needs about 4 GB of memory): a larger tissue is a mosaic of fields (tiles_across, tiles_down)", int(MaxPixels/1e6), pixels/1e6))
	}
	need(p.TilesAcross >= 0 && p.TilesDown >= 0 && p.TilesAcross <= 100 && p.TilesDown <= 100, "0 to 100 tiles each way")
	if across, down := p.tiles(); across*down > 1 {
		w, h := int(math.Ceil(p.WidthUm/p.PixelUm)), int(math.Ceil(p.HeightUm/p.PixelUm))
		need(w%mosaicBlock == 0 && h%mosaicBlock == 0, fmt.Sprintf("a mosaic's tiles whole %d-pixel blocks each way (%d × %d pixels asked; %.1f µm is %d at %g µm)", mosaicBlock, w, h, float64(mosaicBlock)*8*p.PixelUm, 8*mosaicBlock, p.PixelUm))
	}
	need(p.Retries >= 0 && p.Retries <= 10000, "0 to 10,000 placement retries")
	positive := func(v float64) bool { return v > 0 && !math.IsInf(v, 0) }
	if p.TypeOdds != nil {
		need(len(p.TypeOdds) == len(p.Types), "type odds for every cell type")
		for _, v := range p.TypeOdds {
			need(positive(v), "type odds above 0")
		}
	}
	for _, d := range p.DomainOdds {
		need(d.Leader >= 0 && d.Leader < len(p.Types) && d.Type >= 0 && d.Type < len(p.Types) && positive(d.Odds), "domain odds of a leading type and a type, above 0")
	}
	if p.LevelFolds != nil {
		need(len(p.LevelFolds) == len(p.Types), "level folds for every cell type")
		for _, row := range p.LevelFolds {
			need(len(row) == p.Genes, "level folds for every gene")
			for _, v := range row {
				need(positive(v), "level folds above 0")
			}
		}
	}
	if (p.TypeOdds != nil || len(p.DomainOdds) > 0 || p.LevelFolds != nil) && (p.TilesAcross > 1 || p.TilesDown > 1) {
		bad = append(bad, "a cohort's sample is one field, not a mosaic")
	}
	if px := p.PostXenium; px != nil {
		across, down := p.tiles()
		need(across*down == 1, "post-Xenium images of a single field (not a mosaic)")
		need(px.HE != nil || px.IF != nil, "post-Xenium images: an H&E, an immunofluorescence image, or both")
		if px.HE != nil {
			px.HE.check("the H&E", need)
		}
		if px.IF != nil {
			px.IF.check("the immunofluorescence image", need)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("the simulation's parameters: %s", joinAnd(bad))
	}
	return nil
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	out := ""
	for i, s := range items {
		switch {
		case i == 0:
			out = s
		case i == len(items)-1:
			out += "; and " + s
		default:
			out += "; " + s
		}
	}
	return out
}

var errStopped = errors.New("the simulation was stopped")
