package spatial

// A spatial analysis of a segmentation's cells, an artifact of kind "spatial" (internal/artifacts):
//
//	analysis.json     the question, what it was asked of (the segmentation; the graph, or the cells
//	                  chosen and how; the label set, its level, and its labels' names and colors;
//	                  the strata), its parameters and its results
//	cells.bin "SCSA"  the cells analyzed: rows of the segmentation, each one's label (−1 for none)
//	                  and stratum
//
// Numbers that are not defined (a z-score where the permutations never vary) are written as null.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/segment"
)

// The questions.
const (
	QuestionEnrichment   = "enrichment"
	QuestionCooccurrence = "cooccurrence"
	QuestionDistances    = "distances"
	QuestionRipley       = "ripley"
	QuestionRegions      = "regions"
	QuestionBoundary     = "boundary"
	QuestionNiches       = "niches"
)

// AnalysisVersion names this version of the analyses, as artifacts record it.
const AnalysisVersion = "1"

// The analysis's files.
const (
	FileAnalysis = "analysis.json"
	FileAnalyzed = "cells.bin"
)

// AnalysisFiles lists them in order.
var AnalysisFiles = []string{FileAnalysis, FileAnalyzed}

// AnalysisRoles name each file's role in an artifact's manifest.
var AnalysisRoles = map[string]string{FileAnalysis: "description and results", FileAnalyzed: "cells"}

// Nums are numbers written to JSON with null for NaN and infinities.
type Nums []float64

func (v Nums) MarshalJSON() ([]byte, error) {
	out := make([]byte, 0, 12*len(v)+2)
	out = append(out, '[')
	for i, x := range v {
		if i > 0 {
			out = append(out, ',')
		}
		if math.IsNaN(x) || math.IsInf(x, 0) {
			out = append(out, "null"...)
			continue
		}
		b, _ := json.Marshal(x)
		out = append(out, b...)
	}
	return append(out, ']'), nil
}

func (v *Nums) UnmarshalJSON(b []byte) error {
	var raw []*float64
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*v = make(Nums, len(raw))
	for i, x := range raw {
		if x == nil {
			(*v)[i] = math.NaN()
		} else {
			(*v)[i] = *x
		}
	}
	return nil
}

// LabelSet names the labels analyzed: the set (its artifact or project id, kind and name) and level,
// the labels' names and colors, and the checksum of each cell's label as the request sent them.
type LabelSet struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Level    int      `json:"level"`
	Names    []string `json:"names"`
	Colors   []string `json:"colors,omitempty"`
	Checksum string   `json:"checksum"`
}

// GraphRef names the graph an analysis is of.
type GraphRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Parameters Params `json:"parameters"`
}

// Strata name the regions the labels were dealt within (a cell in none is a stratum of its own).
type Strata struct {
	Regions  []Region `json:"regions"`
	Checksum string   `json:"checksum"`
}

// EnrichmentResult is an enrichment's numbers in analysis.json, K × K row-major.
type EnrichmentResult struct {
	Cells    []int `json:"cells"`
	Edges    int   `json:"edges"`
	Strata   int   `json:"strata"`
	Count    Nums  `json:"count"`
	Expected Nums  `json:"expected"`
	Mean     Nums  `json:"mean"`
	SD       Nums  `json:"sd"`
	Z        Nums  `json:"z"`
	P        Nums  `json:"p"`
	PAdj     Nums  `json:"padj"`
}

// CooccurrenceResult is a co-occurrence's numbers in analysis.json: at each distance, K × K
// row-major.
type CooccurrenceResult struct {
	Distances []float64 `json:"distances"`
	Pairs     string    `json:"pairs"`
	Cells     []int     `json:"cells"`
	Count     []Nums    `json:"count"`
	Ratio     []Nums    `json:"ratio"`
}

// NearestResult is nearest distances' numbers in analysis.json: by pair, K × K row-major, and
// distributions at Distances, (K × K) × D.
type NearestResult struct {
	Reach     float64   `json:"reach"`
	Distances []float64 `json:"distances"`
	Cells     []int     `json:"cells"`
	Beyond    []int     `json:"beyond"`
	Median    Nums      `json:"median"`
	Mean      Nums      `json:"mean"`
	PermMean  Nums      `json:"permMean"`
	PermSD    Nums      `json:"permSd"`
	Z         Nums      `json:"z"`
	P         Nums      `json:"p"`
	PAdj      Nums      `json:"padj"`
	CDF       Nums      `json:"cdf"`
	MeanCDF   Nums      `json:"meanCdf"`
	Lo        Nums      `json:"lo"`
	Hi        Nums      `json:"hi"`
}

// RipleyResult is Ripley's K's numbers in analysis.json: curves (K × K) × R over Radii; tests K × K.
type RipleyResult struct {
	Radii      []float64 `json:"radii"`
	Correction string    `json:"correction"`
	Area       float64   `json:"area"`
	Cells      []int     `json:"cells"`
	Pairs      int       `json:"pairs"`
	Outside    int       `json:"outside"`
	K          Nums      `json:"k"`
	L          Nums      `json:"l"`
	Mean       Nums      `json:"mean"`
	Lo         Nums      `json:"lo"`
	Hi         Nums      `json:"hi"`
	P          Nums      `json:"p"`
	PAdj       Nums      `json:"padj"`
}

// RegionsResult is regions compared in analysis.json: rows the regions, then the tissue in none of
// them (R rows); by row and label, R × K row-major; ratios by ordered pair of rows and label,
// R × R × K, the first row's over the second's. Densities are cells per mm² of the tissue within
// the row; an upper limit beyond any finite ratio is written as null, as is a ratio with no cell
// in either row.
type RegionsResult struct {
	Regions        []Region `json:"regions"`
	Area           Nums     `json:"area"`
	Labeled        []int    `json:"labeled"`
	All            []int    `json:"all"`
	Count          []int    `json:"count"`
	Density        Nums     `json:"density"`
	DensityLo      Nums     `json:"densityLo"`
	DensityHi      Nums     `json:"densityHi"`
	Share          Nums     `json:"share"`
	ShareLo        Nums     `json:"shareLo"`
	ShareHi        Nums     `json:"shareHi"`
	DensityRatio   Nums     `json:"densityRatio"`
	DensityRatioLo Nums     `json:"densityRatioLo"`
	DensityRatioHi Nums     `json:"densityRatioHi"`
	ShareRatio     Nums     `json:"shareRatio"`
	ShareRatioLo   Nums     `json:"shareRatioLo"`
	ShareRatioHi   Nums     `json:"shareRatioHi"`
	Outside        int      `json:"outside"`
}

// BoundaryResult is the profile across a region's edge in analysis.json: by band (below the first
// edge, between each pair, from the last on: B = edges + 1) and label, B × K; by zone (the core,
// the margin, around) and label, 3 × K; the zones compared by label, K × 3 (the core over around,
// the margin over the core, the margin over around); each label's reading: inside +1 denser in the
// core than around, −1 sparser, 0 neither shown; atMargin, denser at the margin than in both.
type BoundaryResult struct {
	Region      Region    `json:"region"`
	Edges       []float64 `json:"edges"`
	Area        Nums      `json:"area"`
	Labeled     []int     `json:"labeled"`
	All         []int     `json:"all"`
	Count       []int     `json:"count"`
	Density     Nums      `json:"density"`
	DensityLo   Nums      `json:"densityLo"`
	DensityHi   Nums      `json:"densityHi"`
	Share       Nums      `json:"share"`
	ShareLo     Nums      `json:"shareLo"`
	ShareHi     Nums      `json:"shareHi"`
	ZoneArea    Nums      `json:"zoneArea"`
	ZoneLabeled []int     `json:"zoneLabeled"`
	ZoneCount   []int     `json:"zoneCount"`
	ZoneDensity Nums      `json:"zoneDensity"`
	ZoneLo      Nums      `json:"zoneLo"`
	ZoneHi      Nums      `json:"zoneHi"`
	Ratio       Nums      `json:"ratio"`
	RatioLo     Nums      `json:"ratioLo"`
	RatioHi     Nums      `json:"ratioHi"`
	Inside      []int     `json:"inside"`
	AtMargin    []bool    `json:"atMargin"`
	Outside     int       `json:"outside"`
}

// NichesResult is cellular neighborhoods in analysis.json, the niches numbered by size: by niche,
// its cells and, N × K, the mean composition of its cells' windows and its labeled cells by label;
// each label's share of the labeled cells; the niches' defining labels (those at least 1.5 times
// their share in the tissue and 5% of the windows, by how much, up to three; else the commonest);
// the cells with a window, k-means's inertia and each start's; the agreement (adjusted Rand
// index) with each further seed's niches; and each niche's leading genes against the other
// niches' cells (exploratory), from the cells with a gene transcript.
type NichesResult struct {
	Niches      int                `json:"niches"`
	Windows     int                `json:"windows"`
	Cells       []int              `json:"cells"`
	Composition Nums               `json:"composition"`
	Counts      []int              `json:"counts"`
	Tissue      Nums               `json:"tissue"`
	Defining    [][]int            `json:"defining"`
	Inertia     float64            `json:"inertia"`
	Inertias    Nums               `json:"inertias"`
	Agreement   Nums               `json:"agreement"`
	Markers     [][]cluster.Marker `json:"markers"`
	MarkerCells int                `json:"markerCells"`
}

// DefiningLabels are a niche's defining labels: those whose share of its windows is at least 1.5
// times their share in the tissue and at least 5%, by that ratio, up to three; else the commonest.
func DefiningLabels(composition, tissue []float64) []int {
	var out []int
	for k := range composition {
		if tissue[k] > 0 && composition[k] >= 0.05 && composition[k] >= 1.5*tissue[k] {
			out = append(out, k)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return composition[out[a]]/tissue[out[a]] > composition[out[b]]/tissue[out[b]]
	})
	if len(out) > 3 {
		out = out[:3]
	}
	if len(out) == 0 {
		best := 0
		for k := range composition {
			if composition[k] > composition[best] {
				best = k
			}
		}
		out = []int{best}
	}
	return out
}

// WindowRef says what window Ripley's K was in: the tissue (its cells' pixels on a grid, closed by
// a radius) or a region; its area and boundary.
type WindowRef struct {
	Kind     string  `json:"kind"`
	Region   *Region `json:"region,omitempty"`
	Closing  float64 `json:"closing,omitempty"`
	Grid     float64 `json:"grid,omitempty"`
	Area     float64 `json:"area"`
	Segments int     `json:"segments"`
}

// Analysis is analysis.json.
type Analysis struct {
	Format        string          `json:"format"`
	Version       int             `json:"version"`
	Method        string          `json:"method"`
	MethodVersion string          `json:"methodVersion"`
	Question      string          `json:"question"`
	Segmentation  Side            `json:"segmentation"`
	Graph         *GraphRef       `json:"graph,omitempty"`
	Cells         AnalyzedCells   `json:"cells"`
	Labels        LabelSet        `json:"labels"`
	Strata        *Strata         `json:"strata,omitempty"`
	Parameters    json.RawMessage `json:"parameters"`
	// One of these, as the question asks.
	Enrichment   *EnrichmentResult   `json:"enrichment,omitempty"`
	Cooccurrence *CooccurrenceResult `json:"cooccurrence,omitempty"`
	Nearest      *NearestResult      `json:"nearest,omitempty"`
	Ripley       *RipleyResult       `json:"ripley,omitempty"`
	Regions      *RegionsResult      `json:"regions,omitempty"`
	Boundary     *BoundaryResult     `json:"boundary,omitempty"`
	Niches       *NichesResult       `json:"niches,omitempty"`
	Window       *WindowRef          `json:"window,omitempty"`
}

// AnalyzedCells are the cells an analysis is of: of the segmentation's, those in its graph (or chosen,
// for one without a graph, with how), and those of them with a label.
type AnalyzedCells struct {
	Segmentation int `json:"segmentation"`
	Analyzed     int `json:"analyzed"`
	Labeled      int `json:"labeled"`
	Choice
}

// Analyzed are cells.bin: rows of the segmentation, labels and strata. A stratum is, for an
// enrichment, the region the labels were dealt within (0 none); for regions compared, the regions
// holding the cell (bit k for the kth; OutsideTissue outside the tissue window); across a region's
// edge, the cell's band + 1 (0 outside the tissue window); for niches, the cell's niche + 1 (0 for
// a cell without a labeled cell in its window); for the other questions, 0.
type Analyzed struct {
	Rows    []uint32
	Label   []int32
	Stratum []int32
}

// WriteAnalysis writes an analysis's files into dir.
func WriteAnalysis(dir string, a *Analysis, cells *Analyzed) error {
	raw, err := json.MarshalIndent(a, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileAnalysis), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	var b segment.Buffer
	b.U32(uint32(len(cells.Rows)))
	b.U32s(cells.Rows)
	b.U32s(int32s(cells.Label))
	b.U32s(int32s(cells.Stratum))
	return segment.WriteBinary(filepath.Join(dir, FileAnalyzed), "SCSA", &b)
}

func int32s(v []int32) []uint32 {
	out := make([]uint32, len(v))
	for i, x := range v {
		out[i] = uint32(x)
	}
	return out
}

// ReadAnalysis reads analysis.json.
func ReadAnalysis(dir string) (*Analysis, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileAnalysis))
	if err != nil {
		return nil, err
	}
	var a Analysis
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("%s: %w", FileAnalysis, err)
	}
	if a.Format != "situcraft-spatial-analysis" || a.Version != 1 {
		return nil, fmt.Errorf("%s is not a SituCraft spatial analysis of version 1", FileAnalysis)
	}
	return &a, nil
}

// ReadAnalyzed reads cells.bin.
func ReadAnalyzed(dir string) (*Analyzed, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileAnalyzed), "SCSA")
	if err != nil {
		return nil, err
	}
	n := r.Count(12)
	c := &Analyzed{Rows: r.U32s(n)}
	for _, v := range r.U32s(n) {
		c.Label = append(c.Label, int32(v))
	}
	for _, v := range r.U32s(n) {
		c.Stratum = append(c.Stratum, int32(v))
	}
	return c, r.Done(FileAnalyzed)
}

// AnalysisDigest is the SHA-256 of an analysis's files, in order.
func AnalysisDigest(dir string) (string, error) {
	h := sha256.New()
	for _, f := range AnalysisFiles {
		raw, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(raw)
		h.Write([]byte(f + " " + hex.EncodeToString(sum[:]) + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Pair is one pair of labels' numbers, for a summary.
type Pair struct {
	A     string  `json:"a"`
	B     string  `json:"b"`
	Ratio float64 `json:"ratio"`
	Z     float64 `json:"z"`
	PAdj  float64 `json:"padj"`
}

// AnalysisSummary is what the manifest says of an analysis.
type AnalysisSummary struct {
	Question     string    `json:"question"`
	Segmentation Side      `json:"segmentation"`
	Graph        *GraphRef `json:"graph,omitempty"`
	Labels       struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Level int    `json:"level"`
		Count int    `json:"count"`
	} `json:"labels"`
	Cells   int      `json:"cells"`
	Labeled int      `json:"labeled"`
	Strata  []Region `json:"strata,omitempty"`
	// Enrichment: the pairs most enriched and most depleted by their ratio, among those whose
	// adjusted p is below 0.05, up to three each; and the pairs significant.
	Enriched    []Pair `json:"enriched,omitempty"`
	Depleted    []Pair `json:"depleted,omitempty"`
	Significant int    `json:"significant,omitempty"`
	// Co-occurrence: its distances.
	Distances int `json:"distances,omitempty"`
	// Nearest distances: the pairs nearer and farther than chance by their ratio (the median over the
	// permutations'), among those whose adjusted p is below 0.05, up to three each.
	Nearer  []Pair `json:"nearer,omitempty"`
	Farther []Pair `json:"farther,omitempty"`
	// Ripley's K: the pairs above and below their envelope by the global test, up to three each.
	Above []Curve `json:"above,omitempty"`
	Below []Curve `json:"below,omitempty"`
	// The window, for Ripley's K, regions compared and across a region's edge.
	Window *WindowRef `json:"window,omitempty"`
	// Regions compared: the regions; each region's labels denser and sparser than in the tissue in
	// none of them (their intervals clear of 1), by the ratio, up to three each.
	Rows    []Region      `json:"rows,omitempty"`
	Denser  []RegionLabel `json:"denser,omitempty"`
	Sparser []RegionLabel `json:"sparser,omitempty"`
	// Across a region's edge: the region, and the labels denser inside than around, sparser, and
	// gathered at its margin.
	Edge   *Region  `json:"edge,omitempty"`
	Fill   []string `json:"fill,omitempty"`
	Avoid  []string `json:"avoid,omitempty"`
	Gather []string `json:"gather,omitempty"`
	// Niches: each one's cells, defining labels and leading genes; the least and mean agreement
	// with the further seeds' niches.
	Niches       []NicheSummary `json:"niches,omitempty"`
	AgreementMin *float64       `json:"agreementMin,omitempty"`
	AgreementAvg *float64       `json:"agreementMean,omitempty"`
}

// NicheSummary is one niche, for a summary.
type NicheSummary struct {
	Cells    int      `json:"cells"`
	Defining []string `json:"defining"`
	Genes    []string `json:"genes"`
}

// RegionLabel is a label's density in a region over the tissue in none of the regions, for a
// summary.
type RegionLabel struct {
	Region string `json:"region"`
	Label  string `json:"label"`
	Ratio  Num    `json:"ratio"`
	Lo     Num    `json:"lo"`
	Hi     Num    `json:"hi"`
}

// Num is a number written to JSON as null when it is NaN or infinite.
type Num float64

func (v Num) MarshalJSON() ([]byte, error) {
	if f := float64(v); !math.IsNaN(f) && !math.IsInf(f, 0) {
		return json.Marshal(f)
	}
	return []byte("null"), nil
}

func (v *Num) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*v = Num(math.NaN())
		return nil
	}
	var f float64
	err := json.Unmarshal(b, &f)
	*v = Num(f)
	return err
}

// Curve is one pair's Ripley curve, for a summary: where it strays farthest from the permutations'
// mean (radius, µm; L and the mean there) and its test.
type Curve struct {
	A      string  `json:"a"`
	B      string  `json:"b"`
	Radius float64 `json:"radius"`
	L      float64 `json:"l"`
	Mean   float64 `json:"mean"`
	PAdj   float64 `json:"padj"`
}

// CheckAnalysis checks an analysis's files before it is recorded and fills the manifest's summary:
// the files of one size, the cells in the segmentation's order, every label and stratum one of the
// analysis's, the results' matrices K × K, the probabilities probabilities.
func CheckAnalysis(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	a, err := ReadAnalysis(dir)
	if err != nil {
		fatal("spatial.unreadable", "%v", err)
		return out
	}
	c, err := ReadAnalyzed(dir)
	if err != nil {
		fatal("spatial.unreadable", "%v", err)
		return out
	}
	K := len(a.Labels.Names)
	n := len(c.Rows)
	if n != a.Cells.Analyzed {
		fatal("spatial.sizes", "cells.bin holds %d cells; the description says %d.", n, a.Cells.Analyzed)
		return out
	}
	labeled := 0
	for i := 0; i < n; i++ {
		if int(c.Rows[i]) >= a.Cells.Segmentation || i > 0 && c.Rows[i] <= c.Rows[i-1] {
			fatal("spatial.cells", "The cells analyzed are not the segmentation's in order (cell %d).", i)
			return out
		}
		if c.Label[i] < -1 || c.Label[i] >= int32(K) || c.Stratum[i] < 0 {
			fatal("spatial.labels", "Cell %d has label %d of %d, stratum %d.", i, c.Label[i], K, c.Stratum[i])
			return out
		}
		if c.Label[i] >= 0 {
			labeled++
		}
	}
	if labeled != a.Cells.Labeled {
		fatal("spatial.labels", "%d cells are labeled; the description says %d.", labeled, a.Cells.Labeled)
	}
	square := func(name string, v Nums) bool {
		if len(v) != K*K {
			fatal("spatial.sizes", "The %s are not %d × %d.", name, K, K)
			return false
		}
		return true
	}
	s := AnalysisSummary{Question: a.Question, Segmentation: a.Segmentation, Graph: a.Graph, Cells: n, Labeled: labeled}
	s.Labels.ID, s.Labels.Name, s.Labels.Level, s.Labels.Count = a.Labels.ID, a.Labels.Name, a.Labels.Level, K
	if a.Strata != nil {
		s.Strata = a.Strata.Regions
	}
	probability := func(name string, p, padj Nums, cells []int) bool {
		for q := range p {
			untested := len(cells) != K || cells[q/K] == 0 || cells[q%K]-btoi(q/K == q%K) <= 0
			if untested && !(math.IsNaN(p[q]) && math.IsNaN(padj[q])) || !untested && (!(p[q] > 0 && p[q] <= 1) || !(padj[q] >= p[q]*(1-1e-12) && padj[q] <= 1)) {
				fatal("spatial.results", "The %s of pair %d are out of range.", name, q)
				return false
			}
		}
		return true
	}
	switch a.Question {
	case QuestionDistances:
		r := a.Nearest
		if r == nil {
			fatal("spatial.results", "Nearest distances without their results.")
			return out
		}
		D := len(r.Distances)
		for _, v := range []Nums{r.Median, r.Mean, r.PermMean, r.PermSD, r.Z, r.P, r.PAdj} {
			if !square("results", v) {
				return out
			}
		}
		if len(r.CDF) != K*K*D || len(r.Lo) != K*K*D || len(r.Hi) != K*K*D || len(r.MeanCDF) != K*K*D || len(r.Beyond) != K*K {
			fatal("spatial.sizes", "The distributions are not %d × %d × %d.", K, K, D)
			return out
		}
		if !probability("p-values", r.P, r.PAdj, r.Cells) {
			return out
		}
		var pairs []Pair
		for q := range r.P {
			if r.PAdj[q] < 0.05 && r.PermMean[q] > 0 && !math.IsNaN(r.Z[q]) {
				pairs = append(pairs, Pair{A: a.Labels.Names[q/K], B: a.Labels.Names[q%K], Ratio: r.Median[q] / r.PermMean[q], Z: r.Z[q], PAdj: r.PAdj[q]})
			}
		}
		// The strongest effects first: the ratio of medians, among the significant.
		s.Significant = len(pairs)
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].Ratio < pairs[j].Ratio })
		for _, p := range pairs {
			if p.Z < 0 && len(s.Nearer) < 3 {
				s.Nearer = append(s.Nearer, p)
			}
		}
		for i := len(pairs) - 1; i >= 0 && len(s.Farther) < 3; i-- {
			if pairs[i].Z > 0 {
				s.Farther = append(s.Farther, pairs[i])
			}
		}
	case QuestionRipley:
		r := a.Ripley
		if r == nil || a.Window == nil {
			fatal("spatial.results", "Ripley's K without its window or results.")
			return out
		}
		R := len(r.Radii)
		for _, v := range []Nums{r.K, r.L, r.Mean, r.Lo, r.Hi} {
			if len(v) != K*K*R {
				fatal("spatial.sizes", "Ripley's curves are not %d × %d × %d.", K, K, R)
				return out
			}
		}
		if !square("p-values", r.P) || !square("adjusted p-values", r.PAdj) || !probability("p-values", r.P, r.PAdj, r.Cells) {
			return out
		}
		var curves []Curve
		var sign []float64
		for q := range r.P {
			if !(r.PAdj[q] < 0.05) {
				continue
			}
			best, at := -1.0, 0
			for k := 0; k < R; k++ {
				if d := math.Abs(r.L[q*R+k] - r.Mean[q*R+k]); d > best {
					best, at = d, k
				}
			}
			curves = append(curves, Curve{A: a.Labels.Names[q/K], B: a.Labels.Names[q%K], Radius: r.Radii[at], L: r.L[q*R+at], Mean: r.Mean[q*R+at], PAdj: r.PAdj[q]})
			sign = append(sign, r.L[q*R+at]-r.Mean[q*R+at])
		}
		s.Significant = len(curves)
		order := make([]int, len(curves))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool { return math.Abs(sign[order[i]]) > math.Abs(sign[order[j]]) })
		for _, i := range order {
			if sign[i] > 0 && len(s.Above) < 3 {
				s.Above = append(s.Above, curves[i])
			}
			if sign[i] < 0 && len(s.Below) < 3 {
				s.Below = append(s.Below, curves[i])
			}
		}
		s.Window = a.Window
	case QuestionRegions:
		r := a.Regions
		if r == nil || a.Window == nil {
			fatal("spatial.results", "Regions compared without their window or results.")
			return out
		}
		R := len(r.Regions) + 1
		if len(r.Regions) < 1 || len(r.Area) != R || len(r.Labeled) != R || len(r.All) != R || len(r.Count) != R*K {
			fatal("spatial.sizes", "The regions' rows are not %d.", R)
			return out
		}
		for _, v := range []Nums{r.Density, r.DensityLo, r.DensityHi, r.Share, r.ShareLo, r.ShareHi} {
			if len(v) != R*K {
				fatal("spatial.sizes", "The regions' numbers are not %d × %d.", R, K)
				return out
			}
		}
		for _, v := range []Nums{r.DensityRatio, r.DensityRatioLo, r.DensityRatioHi, r.ShareRatio, r.ShareRatioLo, r.ShareRatioHi} {
			if len(v) != R*R*K {
				fatal("spatial.sizes", "The regions' ratios are not %d × %d × %d.", R, R, K)
				return out
			}
		}
		// Each row's cells: its labeled ones counted by label, those of the cells.bin holding it.
		labeledIn := make([]int, R)
		for i := 0; i < n; i++ {
			m := c.Stratum[i]
			if m == OutsideTissue || c.Label[i] < 0 {
				continue
			}
			if m == 0 {
				labeledIn[R-1]++
			}
			for k := 0; k < R-1; k++ {
				if m&(1<<k) != 0 {
					labeledIn[k]++
				}
			}
		}
		for row := 0; row < R; row++ {
			sum := 0
			for k := 0; k < K; k++ {
				sum += r.Count[row*K+k]
				if !inRange(r.Share[row*K+k], r.ShareLo[row*K+k], r.ShareHi[row*K+k]) || !inRange(r.Density[row*K+k], r.DensityLo[row*K+k], r.DensityHi[row*K+k]) {
					fatal("spatial.results", "Row %d, label %d: a number beyond its interval.", row, k)
					return out
				}
			}
			if sum != r.Labeled[row] || labeledIn[row] != r.Labeled[row] || !(r.Area[row] >= 0) {
				fatal("spatial.results", "Row %d counts %d labeled cells by label and %d in cells.bin; the results say %d.", row, sum, labeledIn[row], r.Labeled[row])
				return out
			}
		}
		for q := range r.DensityRatio {
			if !inRange(r.DensityRatio[q], r.DensityRatioLo[q], r.DensityRatioHi[q]) || !inRange(r.ShareRatio[q], r.ShareRatioLo[q], r.ShareRatioHi[q]) {
				fatal("spatial.results", "Ratio %d lies beyond its interval.", q)
				return out
			}
		}
		s.Rows = r.Regions
		var denser []RegionLabel
		for row := 0; row < R-1; row++ {
			for k := 0; k < K; k++ {
				q := (row*R+R-1)*K + k
				if r.DensityRatioLo[q] > 1 || r.DensityRatioHi[q] < 1 {
					// A ratio written as null with its lower limit above 1 has no cell of the label
					// elsewhere: infinite.
					ratio := r.DensityRatio[q]
					if math.IsNaN(ratio) && r.DensityRatioLo[q] > 1 {
						ratio = math.Inf(1)
					}
					denser = append(denser, RegionLabel{Region: r.Regions[row].Name, Label: a.Labels.Names[k], Ratio: Num(ratio), Lo: Num(r.DensityRatioLo[q]), Hi: Num(r.DensityRatioHi[q])})
				}
			}
		}
		s.Significant = len(denser)
		sort.SliceStable(denser, func(i, j int) bool { return finite(float64(denser[i].Ratio)) > finite(float64(denser[j].Ratio)) })
		for _, d := range denser {
			if d.Lo > 1 && len(s.Denser) < 3 {
				s.Denser = append(s.Denser, d)
			}
		}
		for i := len(denser) - 1; i >= 0 && len(s.Sparser) < 3; i-- {
			if denser[i].Hi < 1 {
				s.Sparser = append(s.Sparser, denser[i])
			}
		}
		s.Window = a.Window
	case QuestionBoundary:
		b := a.Boundary
		if b == nil || a.Window == nil {
			fatal("spatial.results", "A profile across an edge without its window or results.")
			return out
		}
		B := len(b.Edges) + 1
		if len(b.Edges) < 2 || !sort.Float64sAreSorted(b.Edges) || len(b.Area) != B || len(b.Labeled) != B || len(b.All) != B || len(b.Count) != B*K {
			fatal("spatial.sizes", "The bands are not %d.", B)
			return out
		}
		for _, v := range []Nums{b.Density, b.DensityLo, b.DensityHi, b.Share, b.ShareLo, b.ShareHi} {
			if len(v) != B*K {
				fatal("spatial.sizes", "The bands' numbers are not %d × %d.", B, K)
				return out
			}
		}
		if len(b.ZoneArea) != Zones || len(b.ZoneLabeled) != Zones || len(b.ZoneCount) != Zones*K || len(b.ZoneDensity) != Zones*K || len(b.ZoneLo) != Zones*K || len(b.ZoneHi) != Zones*K ||
			len(b.Ratio) != K*Comparisons || len(b.RatioLo) != K*Comparisons || len(b.RatioHi) != K*Comparisons || len(b.Inside) != K || len(b.AtMargin) != K {
			fatal("spatial.sizes", "The zones' numbers are not for %d labels.", K)
			return out
		}
		inBand := make([]int, B)
		for i := 0; i < n; i++ {
			if c.Stratum[i] > int32(B) {
				fatal("spatial.labels", "Cell %d is in band %d of %d.", i, c.Stratum[i], B)
				return out
			}
			if c.Stratum[i] > 0 && c.Label[i] >= 0 {
				inBand[c.Stratum[i]-1]++
			}
		}
		for band := 0; band < B; band++ {
			sum := 0
			for k := 0; k < K; k++ {
				q := band*K + k
				sum += b.Count[q]
				if !inRange(b.Density[q], b.DensityLo[q], b.DensityHi[q]) || !inRange(b.Share[q], b.ShareLo[q], b.ShareHi[q]) {
					fatal("spatial.results", "Band %d, label %d: a number beyond its interval.", band, k)
					return out
				}
			}
			if sum != b.Labeled[band] || inBand[band] != b.Labeled[band] || !(b.Area[band] >= 0) {
				fatal("spatial.results", "Band %d counts %d labeled cells by label and %d in cells.bin; the results say %d.", band, sum, inBand[band], b.Labeled[band])
				return out
			}
		}
		for q := range b.Ratio {
			if !inRange(b.Ratio[q], b.RatioLo[q], b.RatioHi[q]) {
				fatal("spatial.results", "Zone ratio %d lies beyond its interval.", q)
				return out
			}
		}
		region := b.Region
		s.Edge = &region
		for k := 0; k < K; k++ {
			switch {
			case b.Inside[k] > 0:
				s.Fill = append(s.Fill, a.Labels.Names[k])
			case b.Inside[k] < 0:
				s.Avoid = append(s.Avoid, a.Labels.Names[k])
			}
			if b.AtMargin[k] {
				s.Gather = append(s.Gather, a.Labels.Names[k])
			}
			if b.Inside[k] != 0 || b.AtMargin[k] {
				s.Significant++
			}
		}
		s.Window = a.Window
	case QuestionNiches:
		r := a.Niches
		if r == nil {
			fatal("spatial.results", "Niches without their results.")
			return out
		}
		N := r.Niches
		if N < 2 || len(r.Cells) != N || len(r.Composition) != N*K || len(r.Counts) != N*K || len(r.Tissue) != K || len(r.Defining) != N || len(r.Markers) != N {
			fatal("spatial.sizes", "The niches' results are not for %d niches of %d labels.", N, K)
			return out
		}
		in := make([]int, N)
		labeledIn := make([]int, N*K)
		windows := 0
		for i := 0; i < n; i++ {
			v := c.Stratum[i]
			if v > int32(N) {
				fatal("spatial.labels", "Cell %d is in niche %d of %d.", i, v, N)
				return out
			}
			if v > 0 {
				windows++
				in[v-1]++
				if c.Label[i] >= 0 {
					labeledIn[int(v-1)*K+int(c.Label[i])]++
				}
			}
		}
		if windows != r.Windows {
			fatal("spatial.results", "%d cells have a niche; the results say %d.", windows, r.Windows)
			return out
		}
		for v := 0; v < N; v++ {
			sum := 0.0
			for k := 0; k < K; k++ {
				sum += r.Composition[v*K+k]
				if labeledIn[v*K+k] != r.Counts[v*K+k] {
					fatal("spatial.results", "Niche %d holds %d cells of label %d; the results say %d.", v, labeledIn[v*K+k], k, r.Counts[v*K+k])
					return out
				}
			}
			if in[v] != r.Cells[v] || math.Abs(sum-1) > 1e-6 || v > 0 && r.Cells[v] > r.Cells[v-1] {
				fatal("spatial.results", "Niche %d: %d cells (the results say %d), its composition summing to %g.", v, in[v], r.Cells[v], sum)
				return out
			}
			ns := NicheSummary{Cells: r.Cells[v]}
			for _, k := range r.Defining[v] {
				if k < 0 || k >= K {
					fatal("spatial.results", "Niche %d's defining label %d.", v, k)
					return out
				}
				ns.Defining = append(ns.Defining, a.Labels.Names[k])
			}
			for _, m := range r.Markers[v][:min(3, len(r.Markers[v]))] {
				ns.Genes = append(ns.Genes, m.Gene)
			}
			s.Niches = append(s.Niches, ns)
		}
		if len(r.Agreement) > 0 {
			lo, sum := math.Inf(1), 0.0
			for _, v := range r.Agreement {
				lo, sum = math.Min(lo, v), sum+v
			}
			avg := sum / float64(len(r.Agreement))
			s.AgreementMin, s.AgreementAvg = &lo, &avg
		}
	case QuestionEnrichment:
		e := a.Enrichment
		if e == nil || a.Graph == nil {
			fatal("spatial.results", "An enrichment without its graph or results.")
			return out
		}
		for _, v := range []struct {
			name string
			v    Nums
		}{{"counts", e.Count}, {"expectations", e.Expected}, {"means", e.Mean}, {"deviations", e.SD}, {"z-scores", e.Z}, {"p-values", e.P}, {"adjusted p-values", e.PAdj}} {
			if !square(v.name, v.v) {
				return out
			}
		}
		for q := range e.P {
			// A pair with a label no cell has has no p-value.
			untested := e.Cells == nil || len(e.Cells) != K || e.Cells[q/K] == 0 || e.Cells[q%K] == 0
			if untested && !(math.IsNaN(e.P[q]) && math.IsNaN(e.PAdj[q])) || !untested && (!(e.P[q] > 0 && e.P[q] <= 1) || !(e.PAdj[q] >= e.P[q]*(1-1e-12) && e.PAdj[q] <= 1)) || e.Count[q] < 0 || e.Expected[q] < 0 {
				fatal("spatial.results", "Pair %d's count, expectation or p-values are out of range.", q)
				return out
			}
		}
		var pairs []Pair
		for x := 0; x < K; x++ {
			for y := x; y < K; y++ {
				q := x*K + y
				if e.Expected[q] > 0 && !math.IsNaN(e.Z[q]) && e.PAdj[q] < 0.05 {
					pairs = append(pairs, Pair{A: a.Labels.Names[x], B: a.Labels.Names[y], Ratio: e.Count[q] / e.Expected[q], Z: e.Z[q], PAdj: e.PAdj[q]})
				}
			}
		}
		// The strongest effects first: the ratio, among the significant.
		s.Significant = len(pairs)
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].Ratio > pairs[j].Ratio })
		for _, p := range pairs {
			if p.Z > 0 && len(s.Enriched) < 3 {
				s.Enriched = append(s.Enriched, p)
			}
		}
		for i := len(pairs) - 1; i >= 0 && len(s.Depleted) < 3; i-- {
			if pairs[i].Z < 0 {
				s.Depleted = append(s.Depleted, pairs[i])
			}
		}
	case QuestionCooccurrence:
		co := a.Cooccurrence
		if co == nil || len(co.Count) != len(co.Distances) || len(co.Ratio) != len(co.Count) {
			fatal("spatial.results", "A co-occurrence whose distances and results differ.")
			return out
		}
		for b := range co.Count {
			if !square("counts", co.Count[b]) || !square("ratios", co.Ratio[b]) {
				return out
			}
		}
		s.Distances = len(co.Count)
	default:
		fatal("spatial.question", "No question %q.", a.Question)
		return out
	}
	m.Summary, _ = json.Marshal(s)
	return out
}

// inRange says whether v lies within [lo, hi], each NaN when not known (an unknown upper limit is
// beyond any finite one); a NaN v is in range.
func inRange(v, lo, hi float64) bool {
	if math.IsNaN(v) {
		return true
	}
	tol := 1e-9 * math.Max(1, math.Abs(v))
	return (math.IsNaN(lo) || lo <= v+tol) && (math.IsNaN(hi) || v <= hi+tol)
}

// finite is v, or the largest float for +Inf, for ordering.
func finite(v float64) float64 {
	if math.IsInf(v, 1) {
		return math.MaxFloat64
	}
	return v
}
