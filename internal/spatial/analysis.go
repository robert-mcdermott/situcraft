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
	"situcraft/internal/segment"
)

// The questions.
const (
	QuestionEnrichment   = "enrichment"
	QuestionCooccurrence = "cooccurrence"
	QuestionDistances    = "distances"
	QuestionRipley       = "ripley"
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

// Analyzed are cells.bin: rows of the segmentation, labels and strata.
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
	// The window, for Ripley's K.
	Window *WindowRef `json:"window,omitempty"`
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
