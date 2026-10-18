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
	// Enrichment: the pairs most enriched and most depleted (adjusted p below 0.05), up to three
	// each; and the pairs significant.
	Enriched    []Pair `json:"enriched,omitempty"`
	Depleted    []Pair `json:"depleted,omitempty"`
	Significant int    `json:"significant,omitempty"`
	// Co-occurrence: its distances.
	Distances int `json:"distances,omitempty"`
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
	switch a.Question {
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
		s.Significant = len(pairs)
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].Z > pairs[j].Z })
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
