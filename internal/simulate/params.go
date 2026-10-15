// Package simulate makes tissues whose every cell and molecule is known, and writes each as a
// Xenium-shaped output bundle, labelled simulated, so that any segmentation, SituCraft's or
// another tool's, can be scored against the truth.
//
// A tissue is a field of cells of several types, in domains of different composition. Each cell
// grows from its nucleus to a size, elongation and irregular outline of its type, competing for
// space with its neighbours; some cells' nuclei lie outside the section and are not seen. Each
// cell expresses the panel's genes with negative-binomial counts around its type's profile, in
// proportion to its area; its molecules lie within it, in the section's depth, and some are
// observed displaced (diffused), a few far. Background molecules of every gene, and negative
// control probes and codewords, fall anywhere. Every molecule has a quality score, lower for
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
	// Background gene transcripts per 100 µm², spread over the genes in proportion to their
	// mean expression; control probes and codewords per 100 µm² each.
	Background float64 `json:"background_per_100um2"`
	ControlBg  float64 `json:"control_per_100um2"`
	// LowQuality is the share of cells' molecules with a quality below 20; background's is
	// BackgroundLowQuality.
	LowQuality           float64 `json:"low_quality"`
	BackgroundLowQuality float64 `json:"background_low_quality"`
	// ExpansionUm is how far the simulated instrument grows the nuclei it sees into cells.
	ExpansionUm float64 `json:"instrument_expansion_um"`
}

// MaxPixels is the largest image a simulation makes (its memory grows with it).
const MaxPixels = 50e6

// Presets are named starting points: small (fast, for tests and trials) and standard.
var Presets = map[string]Params{"small": small(), "standard": standard()}

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
		Background: 6, ControlBg: 0.05, LowQuality: 0.08, BackgroundLowQuality: 0.35,
		ExpansionUm: 5,
	}
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
	need(p.Background >= 0 && p.ControlBg >= 0, "background densities of 0 or more")
	need(p.LowQuality >= 0 && p.LowQuality <= 1 && p.BackgroundLowQuality >= 0 && p.BackgroundLowQuality <= 1, "quality shares of 0 to 1")
	need(p.ExpansionUm >= 0 && p.ExpansionUm <= 50, "an instrument expansion of 0 to 50 µm")
	// About 75 bytes of memory a pixel at the peak (measured): 50 million pixels, about 1.5 mm a
	// side at Xenium's 0.2125 µm, need about 4 GB.
	if pixels := math.Ceil(p.WidthUm/p.PixelUm) * math.Ceil(p.HeightUm/p.PixelUm); pixels > MaxPixels {
		bad = append(bad, fmt.Sprintf("at most %d million pixels (%.0f million asked; about 1.5 mm a side at 0.2125 µm, which needs about 4 GB of memory): larger tissues are not simulated yet", int(MaxPixels/1e6), pixels/1e6))
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
