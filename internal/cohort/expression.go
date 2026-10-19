package cohort

// Differential expression (wave 7, slice 7): each label's pseudobulk (its cells' transcripts of
// each gene, summed over each replicate unit's samples) compared between groups with the unit as
// the replicate. The genes every panel has; the rows with at least MinLabelCells cells of the label;
// genes filtered by expression, TMM's library sizes, voom's weights, each gene's weighted model of
// the design and its variance moderated (limma.go). Each contrast's log2 fold change with its
// interval and moderated t; p-values adjusted by Benjamini and Hochberg over every label, gene and
// contrast tested, the universe recorded.
//
// Files (an artifact of kind expression; docs/FORMATS.md): expression.json (Expression),
// results.csv (a row a test).

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"situcraft/internal/artifacts"
	"situcraft/internal/stats"
)

// The comparison's files, method version and the fewest cells of a label a row is tested with.
const (
	FileExpression          = "expression.json"
	FileExpressionResults   = "results.csv"
	ExpressionMethodVersion = "1"
	MinLabelCells           = 10
)

// ExpressionRoles are the files' roles.
var ExpressionRoles = map[string]string{FileExpression: "comparison", FileExpressionResults: "results"}

// ExpressionContrast is a label's tests of one contrast, one per tested gene (in the label's genes'
// order): the log2 fold change with its interval, the moderated t, p and adjusted p; and the genes
// whose adjusted p is below 0.05, higher and lower.
type ExpressionContrast struct {
	Contrast string    `json:"contrast"`
	LogFC    []float64 `json:"logFC"`
	Lo       []float64 `json:"lo"`
	Hi       []float64 `json:"hi"`
	T        []float64 `json:"t"`
	P        []float64 `json:"p"`
	PAdj     []float64 `json:"padj"`
	Up       int       `json:"up"`
	Down     int       `json:"down"`
}

// ExpressionLabel is a label's comparison: the design's rows used (those with enough of its cells)
// and each row's cells of it; the used rows' library sizes (the tested genes' transcripts) and TMM
// factors; the genes tested (indices into the comparison's genes), each one's mean log2 count per
// million; the genes too little expressed to test; voom's span, the variances' prior (its degrees
// of freedom null when infinite: the variances agree beyond chance) and the tests' degrees of
// freedom; each contrast's tests. Or, untested, why.
type ExpressionLabel struct {
	Label     string               `json:"label"`
	Tested    bool                 `json:"tested"`
	Note      string               `json:"note,omitempty"`
	Cells     []int                `json:"cells"`
	Rows      []int                `json:"rows"`
	Library   []float64            `json:"library,omitempty"`
	Factors   []float64            `json:"factors,omitempty"`
	Genes     []int                `json:"genes,omitempty"`
	AveLogCPM []float64            `json:"aveLogCPM,omitempty"`
	Filtered  int                  `json:"filtered"`
	Span      float64              `json:"span,omitempty"`
	S2Prior   float64              `json:"s2Prior,omitempty"`
	DFPrior   *float64             `json:"dfPrior"`
	DFTotal   float64              `json:"dfTotal,omitempty"`
	Contrasts []ExpressionContrast `json:"contrasts,omitempty"`
}

// Expression is expression.json.
type Expression struct {
	Format        string            `json:"format"`
	Version       int               `json:"version"`
	Method        string            `json:"method"`
	MethodVersion string            `json:"methodVersion"`
	Summary       SummaryRef        `json:"summary"`
	Labels        LabelSet          `json:"labels"`
	Design        ComparisonDesign  `json:"design"`
	Level         float64           `json:"level"`
	MinCells      int               `json:"minCells"`
	Genes         []string          `json:"genes"`
	Results       []ExpressionLabel `json:"results"`
	Tests         int               `json:"tests"`
	Universe      string            `json:"universe"`
}

// SharedGenes are the genes every sample's panel has (the summary's panels' shared genes, by their
// index in its genes): the genes a pseudobulk can be compared by.
func SharedGenes(d *Document, pseudobulk [][][]uint32) []int {
	at := map[string]int{}
	for g, name := range d.Genes {
		at[name] = g
	}
	var out []int
	if d.Panels != nil {
		for _, name := range d.Panels.Shared {
			if g, ok := at[name]; ok {
				out = append(out, g)
			}
		}
		return out
	}
	for g := range d.Genes {
		every := true
		for s := range pseudobulk {
			for k := range pseudobulk[s] {
				if pseudobulk[s][k][g] == Missing {
					every = false
				}
			}
		}
		if every {
			out = append(out, g)
		}
	}
	return out
}

// RowCounts are a label's counts of the genes (genes × the design's rows), each row's samples'
// transcripts summed, and each row's cells of the label.
func RowCounts(d *Document, pseudobulk [][][]uint32, design ComparisonDesign, label int, genes []int) ([][]float64, []int) {
	at := map[string]int{}
	for i, s := range d.Samples {
		at[s.Sample] = i
	}
	counts := make([][]float64, len(genes))
	for j := range counts {
		counts[j] = make([]float64, len(design.Rows))
	}
	cells := make([]int, len(design.Rows))
	for r, row := range design.Rows {
		for _, s := range row.Samples {
			i := at[s]
			cells[r] += d.Samples[i].Counts[label]
			for j, g := range genes {
				if v := pseudobulk[i][label][g]; v != Missing {
					counts[j][r] += float64(v)
				}
			}
		}
	}
	return counts, cells
}

// CompareExpression compares each label's pseudobulk between the design's groups: the rows with at
// least minCells cells of the label, the genes every panel has, each contrast's tests at the level
// (0.95 for 95% intervals).
func CompareExpression(d *Document, pseudobulk [][][]uint32, design ComparisonDesign, level float64, minCells int) (*Expression, error) {
	if err := CheckDesign(d, design); err != nil {
		return nil, err
	}
	if !(level > 0.5 && level < 1) {
		return nil, errors.New("the intervals' level is above 0.5 and below 1")
	}
	if minCells < 1 {
		minCells = MinLabelCells
	}
	genes := SharedGenes(d, pseudobulk)
	if len(genes) < 2 {
		return nil, fmt.Errorf("the samples' panels share %d genes: too few to compare", len(genes))
	}
	e := &Expression{Format: "situcraft-expression", Version: 1, Method: "SituCraft differential expression", MethodVersion: ExpressionMethodVersion, Labels: d.Labels, Design: design, Level: level, MinCells: minCells, Genes: make([]string, len(genes))}
	for j, g := range genes {
		e.Genes[j] = d.Genes[g]
	}
	for k, name := range d.Labels.Names {
		counts, cells := RowCounts(d, pseudobulk, design, k, genes)
		res := ExpressionLabel{Label: name, Cells: cells, Rows: []int{}}
		for r, c := range cells {
			if c >= minCells {
				res.Rows = append(res.Rows, r)
			}
		}
		if note := e.fitLabel(&res, counts); note != "" {
			res.Tested, res.Note = false, note
			res.Library, res.Factors, res.Genes, res.AveLogCPM, res.Contrasts = nil, nil, nil, nil, nil
			res.Span, res.S2Prior, res.DFPrior, res.DFTotal = 0, 0, nil, 0
		}
		e.Results = append(e.Results, res)
	}
	// Benjamini and Hochberg over every label, gene and contrast tested.
	var ps []float64
	for _, r := range e.Results {
		for _, c := range r.Contrasts {
			ps = append(ps, c.P...)
		}
	}
	adj := make([]float64, len(ps))
	stats.BenjaminiHochberg(ps, adj)
	at, labels, untested := 0, 0, 0
	for i := range e.Results {
		r := &e.Results[i]
		if !r.Tested {
			untested++
			continue
		}
		labels++
		for j := range r.Contrasts {
			c := &r.Contrasts[j]
			c.PAdj = adj[at : at+len(c.P)]
			at += len(c.P)
			for g, q := range c.PAdj {
				if q < 0.05 {
					if c.LogFC[g] > 0 {
						c.Up++
					} else {
						c.Down++
					}
				}
			}
		}
	}
	if labels == 0 {
		return nil, fmt.Errorf("no label could be compared: %s", e.Results[0].Note)
	}
	e.Tests = len(ps)
	left := ""
	if untested > 0 {
		left = fmt.Sprintf("; %s untested", plural(untested, "label"))
	}
	e.Universe = fmt.Sprintf("%s (the genes tested in %s × %s%s), adjusted together by Benjamini and Hochberg", plural(len(ps), "test"), plural(labels, "label"), plural(len(design.Contrasts), "contrast"), left)
	return e, nil
}

// fitLabel fits a label's counts of the used rows, filling res, or says why it cannot be tested.
func (e *Expression) fitLabel(res *ExpressionLabel, counts [][]float64) string {
	design := e.Design
	p := len(design.Names)
	if len(res.Rows) <= p {
		verb := "have"
		if len(res.Rows) == 1 {
			verb = "has"
		}
		return fmt.Sprintf("%s %s %d of its cells or more: too few for the design's %d columns", plural(len(res.Rows), "row"), verb, e.MinCells, p)
	}
	X := make([][]float64, len(res.Rows))
	used := make([][]float64, len(counts))
	for j := range counts {
		used[j] = make([]float64, len(res.Rows))
	}
	for i, r := range res.Rows {
		X[i] = design.Rows[r].X
		for j := range counts {
			used[j][i] = counts[j][r]
		}
	}
	if len(res.Rows) < len(design.Rows) {
		// Rows left out can leave a group (or a subject) without a row: the design not estimable.
		xtx := make([][]float64, p)
		for a := range xtx {
			xtx[a] = make([]float64, p)
			for b := range xtx[a] {
				for _, x := range X {
					xtx[a][b] += mul(x[a], x[b])
				}
			}
		}
		if _, err := invertSPD(xtx); err != nil {
			return fmt.Sprintf("with the rows of fewer than %d of its cells left out, the design cannot be fitted", e.MinCells)
		}
	}
	fit, err := FitCounts(used, X)
	if err != nil {
		return "not estimable: " + err.Error()
	}
	res.Tested = true
	for j, k := range fit.Keep {
		if k {
			res.Genes = append(res.Genes, j)
		} else {
			res.Filtered++
		}
	}
	res.Library, res.Factors, res.Span = fit.Lib, fit.Factors, fit.Voom.Span
	res.AveLogCPM = fit.Fit.Amean
	m := fit.Moderate
	res.S2Prior, res.DFTotal = m.S2Prior, m.DFTotal
	if !math.IsInf(m.DFPrior, 1) {
		v := m.DFPrior
		res.DFPrior = &v
	}
	for _, ct := range design.Contrasts {
		c := ExpressionContrast{Contrast: ct.Name}
		for _, t := range TestContrast(fit.Fit, m, ct.Vector, e.Level) {
			c.LogFC, c.Lo, c.Hi = append(c.LogFC, t.Estimate), append(c.Lo, t.Lo), append(c.Hi, t.Hi)
			c.T, c.P = append(c.T, t.T), append(c.P, t.P)
		}
		res.Contrasts = append(res.Contrasts, c)
	}
	return ""
}

// plural is n things: "1 label", "2 labels".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// ExpressionResultsCSV is results.csv: a row each test, and a row each label untested.
func ExpressionResultsCSV(e *Expression) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"label", "contrast", "gene", "tested", "aveLogCPM", "logFC", "lo", "hi", "t", "df", "p", "padj", "note"})
	for _, r := range e.Results {
		if !r.Tested {
			w.Write([]string{r.Label, "", "", "false", "", "", "", "", "", "", "", "", r.Note})
			continue
		}
		for _, c := range r.Contrasts {
			for i, g := range r.Genes {
				w.Write([]string{r.Label, c.Contrast, e.Genes[g], "true", number(r.AveLogCPM[i]), number(c.LogFC[i]), number(c.Lo[i]), number(c.Hi[i]), number(c.T[i]), number(r.DFTotal), number(c.P[i]), number(c.PAdj[i]), ""})
			}
		}
	}
	w.Flush()
	return buf.Bytes()
}

// WriteExpression writes a comparison's files into dir.
func WriteExpression(dir string, e *Expression) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileExpression), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileExpressionResults), ExpressionResultsCSV(e), 0o644)
}

// ReadExpression reads expression.json.
func ReadExpression(dir string) (*Expression, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileExpression))
	if err != nil {
		return nil, err
	}
	e := &Expression{}
	if err := json.Unmarshal(raw, e); err != nil {
		return nil, fmt.Errorf("%s: %w", FileExpression, err)
	}
	return e, nil
}

// ExpressionFinding is a test whose adjusted p is below 0.05.
type ExpressionFinding struct {
	Label    string  `json:"label"`
	Gene     string  `json:"gene"`
	Contrast string  `json:"contrast"`
	LogFC    float64 `json:"logFC"`
	PAdj     float64 `json:"padj"`
}

// CheckExpression checks a comparison's files: each tested label's arrays one per tested gene, its
// rows those with enough of its cells, each test's interval around its estimate and its adjusted p
// between its p and 1, the results the document's. Its summary: the tests, the labels untested,
// those below an adjusted p of 0.05 (the 10 lowest).
func CheckExpression(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "cohort.expression", Message: fmt.Sprintf(format, args...)})
	}
	e, err := ReadExpression(dir)
	if err != nil {
		fatal("%v", err)
		return out
	}
	sum := struct {
		Summary  string              `json:"summary"`
		Tests    int                 `json:"tests"`
		Untested []string            `json:"untested"`
		Below    int                 `json:"below"`
		Found    []ExpressionFinding `json:"found"`
	}{Summary: e.Summary.ID, Untested: []string{}, Found: []ExpressionFinding{}}
	for _, r := range e.Results {
		if !r.Tested {
			sum.Untested = append(sum.Untested, r.Label)
			continue
		}
		for _, row := range r.Rows {
			if row < 0 || row >= len(r.Cells) || r.Cells[row] < e.MinCells {
				fatal("%s: a row used without %d of its cells", r.Label, e.MinCells)
			}
		}
		G := len(r.Genes)
		if len(r.AveLogCPM) != G || len(r.Library) != len(r.Rows) || len(r.Factors) != len(r.Rows) {
			fatal("%s: its genes and rows are not consistent", r.Label)
			continue
		}
		for _, c := range r.Contrasts {
			if len(c.LogFC) != G || len(c.Lo) != G || len(c.Hi) != G || len(c.T) != G || len(c.P) != G || len(c.PAdj) != G {
				fatal("%s, %s: not a test per gene", r.Label, c.Contrast)
				continue
			}
			for i, g := range r.Genes {
				sum.Tests++
				if !(c.Lo[i] <= c.LogFC[i] && c.LogFC[i] <= c.Hi[i]) || !(c.PAdj[i] >= c.P[i]*(1-1e-12) && c.PAdj[i] <= 1) || g < 0 || g >= len(e.Genes) {
					fatal("%s, %s: the test of gene %d is not consistent", r.Label, c.Contrast, g)
					continue
				}
				if c.PAdj[i] < 0.05 {
					sum.Below++
					sum.Found = append(sum.Found, ExpressionFinding{r.Label, e.Genes[g], c.Contrast, c.LogFC[i], c.PAdj[i]})
				}
			}
		}
	}
	if sum.Tests != e.Tests {
		fatal("%d tests, not the %d it says", sum.Tests, e.Tests)
	}
	sort.SliceStable(sum.Found, func(a, b int) bool { return sum.Found[a].PAdj < sum.Found[b].PAdj })
	if len(sum.Found) > 10 {
		sum.Found = sum.Found[:10]
	}
	if raw, err := os.ReadFile(filepath.Join(dir, FileExpressionResults)); err != nil || !bytes.Equal(raw, ExpressionResultsCSV(e)) {
		fatal("%s: not the comparison's tests", FileExpressionResults)
	}
	m.Summary, _ = json.Marshal(sum)
	return out
}

// GeneValues are one gene's values in a label's rows, for its plot: each used row's count, library
// size (TMM's effective) and log2 count per million as voom has it.
type GeneValues struct {
	Label  string    `json:"label"`
	Gene   string    `json:"gene"`
	Rows   []int     `json:"rows"`
	Counts []float64 `json:"counts"`
	Lib    []float64 `json:"library"`
	LogCPM []float64 `json:"logCPM"`
}

// ValuesOf answers a gene's values in a tested label's rows from the summary it compared.
func (e *Expression) ValuesOf(d *Document, pseudobulk [][][]uint32, label, gene string) (*GeneValues, error) {
	k := -1
	for i, r := range e.Results {
		if r.Label == label {
			k = i
		}
	}
	if k < 0 || !e.Results[k].Tested {
		return nil, fmt.Errorf("no tested label %q", label)
	}
	g := -1
	for i, name := range d.Genes {
		if name == gene {
			g = i
		}
	}
	if g < 0 {
		return nil, fmt.Errorf("no gene %q", gene)
	}
	li := -1
	for i, name := range d.Labels.Names {
		if name == label {
			li = i
		}
	}
	if li < 0 {
		return nil, fmt.Errorf("the summary has no label %q", label)
	}
	r := e.Results[k]
	counts, _ := RowCounts(d, pseudobulk, e.Design, li, []int{g})
	v := &GeneValues{Label: label, Gene: gene, Rows: r.Rows}
	for i, row := range r.Rows {
		lib := mul(r.Library[i], r.Factors[i])
		c := counts[0][row]
		v.Counts = append(v.Counts, c)
		v.Lib = append(v.Lib, lib)
		v.LogCPM = append(v.LogCPM, log2((c+0.5)/(lib+1)*1e6))
	}
	return v, nil
}
