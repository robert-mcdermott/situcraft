package label

// Labels from a reference: a labeled single-cell reference (an AnnData object in Zarr, its cells'
// labels in obs) or each label's expression profile (a table of labels by genes, or of genes by
// labels), restricted to the panel's genes; each cell correlated with each label's profile, then
// fine-tuned on the leading labels' genes (Aran D, et al. Reference-based analysis of lung
// single-cell sequencing reveals a transitional profibrotic macrophage. Nat Immunol
// 2019;20:163–172, doi:10.1038/s41590-018-0276-y).
//
// SituCraft's steps:
//   - each label's profile is its reference cells' mean log-normalized expression (counts scaled
//     to the reference's median total, then log1p; log-normalized values as given);
//   - for each ordered pair of labels, the genes most higher in the first's profile than the
//     second's (500 × (2/3)^log₂(labels) of them, those higher at all); the genes compared for a
//     set of labels are the union over its pairs;
//   - a cell's score for a label is the Spearman correlation of its values and the label's profile
//     over the genes compared;
//   - the labels within Tune of the best are kept and scored again on their own genes, until one
//     is left or none drops; the cell takes the last round's best when it leads the next by the
//     margin.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"

	"situcraft/internal/anndata"
	"situcraft/internal/cluster"
	"situcraft/internal/dmath"
	"situcraft/internal/zarr"
)

// Formats of a reference.
const (
	RefAnnData  = "anndata"
	RefProfiles = "profiles"
)

// Values a reference's matrix holds.
const (
	ValuesCounts = "counts"
	ValuesLog    = "log-normalized"
	valuesScaled = "scaled"
)

// MaxReferenceLabels bounds a reference's labels (each pair's genes are compared).
const MaxReferenceLabels = 500

// RefSpec names a reference and what to read of it: an AnnData store's matrix (X, raw/X or a
// layer), its obs column of labels and its var column of gene names ("" for the index); or a table
// of profiles, labels by genes (LabelColumn names the labels, the numeric columns the genes) or
// genes by labels (GeneColumn names the genes, the numeric columns the labels).
type RefSpec struct {
	Path        string `json:"path"`
	Matrix      string `json:"matrix,omitempty"`
	LabelColumn string `json:"labelColumn,omitempty"`
	GeneColumn  string `json:"geneColumn,omitempty"`
}

// Ref is a reference restricted to a panel: its labels (in order of name), each one's reference
// cells (0 for profiles given), the shared genes (the panel's indices, increasing) and each label's
// profile over them.
type Ref struct {
	Format      string      `json:"format"`
	File        string      `json:"file"`
	SHA256      string      `json:"sha256"`
	Path        string      `json:"path,omitempty"`
	Matrix      string      `json:"matrix,omitempty"`
	LabelColumn string      `json:"labelColumn,omitempty"`
	GeneColumn  string      `json:"geneColumn,omitempty"`
	Values      string      `json:"values"`
	RefCells    int         `json:"refCells,omitempty"`
	RefGenes    int         `json:"refGenes"`
	Labels      []string    `json:"labels"`
	Cells       []int       `json:"cells"`
	Genes       []int       `json:"-"`
	Profiles    [][]float64 `json:"-"` // [label][shared gene]
	Missing     []string    `json:"missing"`
}

// labelNames are the usual names of a reference's label column, the most specific first.
var labelNames = []string{"cell_type", "celltype", "cell_type_fine", "celltype_minor", "cell_type_major", "celltype_major", "cell_ontology_class", "ann_finest_level", "ann_level_3", "ann_level_2", "ann_level_1", "label.fine", "label.main", "label", "labels", "annotation", "cell_type_original", "majority_voting", "type"}

// matchGenes matches a reference's gene names to the panel's: exactly, else ignoring case where
// that names one panel gene. Answers the reference's position of each panel gene it has.
func matchGenes(names []string, panel []string) map[int]int {
	exact := map[string]int{}
	folded := map[string][]int{}
	for i, g := range panel {
		exact[g] = i
		folded[strings.ToUpper(g)] = append(folded[strings.ToUpper(g)], i)
	}
	out := map[int]int{}
	for j, n := range names {
		n = strings.TrimSpace(n)
		g, ok := exact[n]
		if !ok {
			if f := folded[strings.ToUpper(n)]; len(f) == 1 {
				g, ok = f[0], true
			}
		}
		if _, seen := out[g]; ok && !seen {
			out[g] = j
		}
	}
	return out
}

func isStore(path string) bool {
	info, err := os.Stat(path)
	return err == nil && (info.IsDir() || strings.HasSuffix(strings.ToLower(path), ".zip"))
}

func openStore(path string) (zarr.Store, func(), error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return zarr.OpenDir(path), func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	s, err := zarr.OpenZip(f, info.Size())
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return s, func() { f.Close() }, nil
}

func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndex(p, "/")+1:]
}

// RefMatrix is a matrix of an AnnData reference, for choosing: its place, size and what it holds
// (counts, log-normalized values, or scaled values below 0, which are not taken).
type RefMatrix struct {
	Name   string `json:"name"`
	Rows   int    `json:"rows"`
	Cols   int    `json:"cols"`
	Values string `json:"values"`
}

// RefGeneColumn is a column of gene names ("" for a data frame's index) and the panel genes it
// names.
type RefGeneColumn struct {
	Name   string `json:"name"`
	Shared int    `json:"shared"`
}

// RefDescription says what a reference holds, for choosing what to read of it, and suggests it.
type RefDescription struct {
	Format   string          `json:"format"`
	File     string          `json:"file"`
	Rows     int             `json:"rows"`
	Matrices []RefMatrix     `json:"matrices,omitempty"`
	Labels   []ColumnInfo    `json:"labels"`
	Genes    []RefGeneColumn `json:"genes"`
	// For a table of profiles, the panel genes its numeric columns name (labels by genes).
	ColumnGenes int     `json:"columnGenes,omitempty"`
	Suggested   RefSpec `json:"suggested"`
}

// valuesOf says what a matrix's values are: counts (whole numbers, none below 0), log-normalized
// (none below 0) or scaled (some below 0).
func valuesOf(data []float64) string {
	counts := true
	for _, v := range data {
		if v < 0 {
			return valuesScaled
		}
		if v != math.Trunc(v) {
			counts = false
		}
	}
	if counts {
		return ValuesCounts
	}
	return ValuesLog
}

// refMatrices are an AnnData store's matrices SituCraft offers, in order of preference, and the
// var frame naming each one's columns.
var refMatrices = []struct{ name, varPath string }{{"layers/counts", "var"}, {"raw/X", "raw/var"}, {"X", "var"}}

// DescribeReference says what a reference holds against a panel's genes.
func DescribeReference(path string, panel []string) (*RefDescription, error) {
	if !isStore(path) {
		return describeProfiles(path, panel)
	}
	s, closeStore, err := openStore(path)
	if err != nil {
		return nil, err
	}
	defer closeStore()
	obs, err := anndata.ReadFrame(s, "obs")
	if err != nil {
		return nil, fmt.Errorf("%s is not an AnnData object (no obs): %w", baseName(path), err)
	}
	out := &RefDescription{Format: RefAnnData, File: baseName(path), Rows: obs.Index.Len(), Labels: []ColumnInfo{}, Genes: []RefGeneColumn{}}
	info, _ := Describe(&Table{Rows: obs.Index.Len(), Columns: obs.Columns})
	for _, ci := range info {
		if ci.Kind == "text" && ci.Distinct >= 2 && ci.Distinct <= MaxReferenceLabels {
			out.Labels = append(out.Labels, ci)
		}
	}
	var layers []string
	if names, err := zarr.Arrays(s); err == nil {
		for _, a := range names {
			// A layer is an array (dense) or a group holding data (sparse) under layers/.
			if rest, ok := strings.CutPrefix(a, "layers/"); ok {
				name, _, _ := strings.Cut(rest, "/")
				if l := "layers/" + name; name != "counts" && !slices.Contains(layers, l) {
					layers = append(layers, l)
				}
			}
		}
	}
	candidates := append([]struct{ name, varPath string }(nil), refMatrices...)
	for _, l := range layers {
		candidates = append(candidates, struct{ name, varPath string }{l, "var"})
	}
	for _, m := range candidates {
		if _, err := zarr.Attributes(s, m.name); err != nil {
			continue
		}
		rows, cols, sample, err := anndata.MatrixInfo(s, m.name)
		if err != nil {
			continue
		}
		rm := RefMatrix{Name: m.name, Rows: rows, Cols: cols, Values: valuesOf(sample)}
		out.Matrices = append(out.Matrices, rm)
		if out.Suggested.Matrix == "" && rm.Values != valuesScaled && rm.Rows == out.Rows {
			out.Suggested.Matrix = m.name
		}
	}
	varPath := "var"
	if out.Suggested.Matrix == "raw/X" {
		varPath = "raw/var"
	}
	if vr, err := anndata.ReadFrame(s, varPath); err == nil {
		for _, c := range append([]*anndata.Column{vr.Index}, vr.Columns...) {
			if c.Text != nil {
				name := c.Name
				if c == vr.Index {
					name = ""
				}
				out.Genes = append(out.Genes, RefGeneColumn{Name: name, Shared: len(matchGenes(c.Text, panel))})
			}
		}
	}
	sort.SliceStable(out.Genes, func(a, b int) bool { return out.Genes[a].Shared > out.Genes[b].Shared })
	if len(out.Genes) > 0 {
		out.Suggested.GeneColumn = out.Genes[0].Name
	}
	for _, n := range labelNames {
		for _, ci := range out.Labels {
			if out.Suggested.LabelColumn == "" && strings.EqualFold(ci.Name, n) {
				out.Suggested.LabelColumn = ci.Name
			}
		}
	}
	if out.Suggested.LabelColumn == "" && len(out.Labels) > 0 {
		out.Suggested.LabelColumn = out.Labels[0].Name
	}
	out.Suggested.Path = path
	return out, nil
}

func describeProfiles(path string, panel []string) (*RefDescription, error) {
	t, err := ReadTable(path)
	if err != nil {
		return nil, err
	}
	out := &RefDescription{Format: RefProfiles, File: t.File, Rows: t.Rows, Labels: []ColumnInfo{}, Genes: []RefGeneColumn{}}
	var numeric []string
	for _, c := range t.Columns {
		if c.Numbers != nil {
			numeric = append(numeric, c.Name)
		} else if c.Text != nil {
			out.Genes = append(out.Genes, RefGeneColumn{Name: c.Name, Shared: len(matchGenes(c.Text, panel))})
		}
	}
	out.ColumnGenes = len(matchGenes(numeric, panel))
	info, _ := Describe(t)
	for _, ci := range info {
		if ci.Kind == "text" {
			out.Labels = append(out.Labels, ci)
		}
	}
	sort.SliceStable(out.Genes, func(a, b int) bool { return out.Genes[a].Shared > out.Genes[b].Shared })
	out.Suggested.Path = path
	switch {
	case len(out.Genes) > 0 && out.Genes[0].Shared > out.ColumnGenes:
		out.Suggested.GeneColumn = out.Genes[0].Name
	case len(out.Labels) > 0:
		out.Suggested.LabelColumn = out.Labels[0].Name
	}
	return out, nil
}

// ReadReference reads a reference as the spec names it, restricted to the panel's genes.
func ReadReference(spec RefSpec, panel []string) (*Ref, error) {
	var ref *Ref
	var err error
	if isStore(spec.Path) {
		ref, err = readAnnData(spec, panel)
	} else {
		ref, err = readProfiles(spec, panel)
	}
	if err != nil {
		return nil, err
	}
	ref.Path = spec.Path
	return ref, nil
}

func readAnnData(spec RefSpec, panel []string) (*Ref, error) {
	s, closeStore, err := openStore(spec.Path)
	if err != nil {
		return nil, err
	}
	defer closeStore()
	matrix := spec.Matrix
	if matrix == "" {
		matrix = "X"
	}
	varPath := ""
	for _, m := range refMatrices {
		if m.name == matrix {
			varPath = m.varPath
		}
	}
	if strings.HasPrefix(matrix, "layers/") && !strings.Contains(matrix[len("layers/"):], "/") {
		varPath = "var"
	}
	if varPath == "" {
		return nil, fmt.Errorf("%q is not a matrix of an AnnData object SituCraft reads (X, raw/X or a layer)", matrix)
	}
	obs, err := anndata.ReadFrame(s, "obs")
	if err != nil {
		return nil, err
	}
	vr, err := anndata.ReadFrame(s, varPath)
	if err != nil {
		return nil, err
	}
	labels := obs.Column(spec.LabelColumn)
	if labels == nil || spec.LabelColumn == "" {
		return nil, fmt.Errorf("the reference's obs has no column %q", spec.LabelColumn)
	}
	genes := vr.Index
	if spec.GeneColumn != "" {
		if genes = vr.Column(spec.GeneColumn); genes == nil {
			return nil, fmt.Errorf("the reference's %s has no column %q", varPath, spec.GeneColumn)
		}
	}
	if genes.Text == nil {
		return nil, fmt.Errorf("the reference's %s column %q is not gene names", varPath, genes.Name)
	}
	rows, cols, _, err := anndata.MatrixInfo(s, matrix)
	if err != nil {
		return nil, err
	}
	if rows != labels.Len() || cols != genes.Len() {
		return nil, fmt.Errorf("the reference's %s is %d × %d; obs has %d rows and %s %d", matrix, rows, cols, labels.Len(), varPath, genes.Len())
	}
	names := make([]string, labels.Len())
	for i := range names {
		if !missing(labels, i) {
			names[i] = strings.TrimSpace(labels.String(i))
		}
	}
	ref, err := fromRows(func(fn func(int, []int64, []float64) error) error {
		_, _, err := anndata.EachRow(s, matrix, fn)
		return err
	}, rows, names, genes.Text, panel)
	if err != nil {
		return nil, err
	}
	ref.Format, ref.File, ref.Matrix, ref.LabelColumn, ref.GeneColumn = RefAnnData, baseName(spec.Path), matrix, spec.LabelColumn, spec.GeneColumn
	if ref.SHA256, err = referenceDigest(spec.Path); err != nil {
		return nil, err
	}
	return ref, nil
}

func referenceDigest(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return dirDigest(path)("")
	}
	return fileDigest(path)
}

// FromMatrix makes a reference from its cells' expression (x: cells by the reference's genes,
// counts or log-normalized values), each cell's label ("" for none) and the genes' names: each
// label's profile is its cells' mean log-normalized value of each panel gene the reference has.
func FromMatrix(x *anndata.Matrix, labels, genes, panel []string) (*Ref, error) {
	if x.Rows != len(labels) || x.Cols != len(genes) {
		return nil, fmt.Errorf("the reference's matrix is %d × %d; it has %d labels and %d genes", x.Rows, x.Cols, len(labels), len(genes))
	}
	return fromRows(func(fn func(int, []int64, []float64) error) error {
		for i := 0; i < x.Rows; i++ {
			lo, hi := x.Indptr[i], x.Indptr[i+1]
			if err := fn(i, x.Indices[lo:hi], x.Data[lo:hi]); err != nil {
				return err
			}
		}
		return nil
	}, x.Rows, labels, genes, panel)
}

// fromRows makes a reference from its cells' rows, walked twice (each cell's genes and values):
// first for each cell's total and what the values are, then for each label's sums.
func fromRows(walk func(func(row int, cols []int64, vals []float64) error) error, rows int, labels, genes, panel []string) (*Ref, error) {
	if rows != len(labels) {
		return nil, fmt.Errorf("the reference's matrix has %d rows for %d cells", rows, len(labels))
	}
	ref := &Ref{RefCells: rows, RefGenes: len(genes), Values: ValuesCounts}
	match := matchGenes(genes, panel)
	ref.restrict(match, panel)
	if len(ref.Genes) < 2 {
		return nil, fmt.Errorf("the reference shares %d of the panel's genes; two or more are needed", len(ref.Genes))
	}
	totals := make([]float64, rows)
	err := walk(func(i int, _ []int64, vals []float64) error {
		for _, v := range vals {
			switch {
			case v < 0:
				return errors.New("the reference's matrix holds values below 0 (scaled): choose its counts or log-normalized values")
			case v != math.Trunc(v):
				ref.Values = ValuesLog
			}
			totals[i] += v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	target := 0.0
	if ref.Values == ValuesCounts {
		sorted := append([]float64(nil), totals...)
		sort.Float64s(sorted)
		if n := len(sorted); n > 0 {
			target = sorted[n/2]
			if n%2 == 0 {
				target = (sorted[n/2-1] + sorted[n/2]) / 2
			}
		}
	}
	col := make([]int, len(genes)) // the reference's column → shared gene, or −1
	for j := range col {
		col[j] = -1
	}
	for k, g := range ref.Genes {
		col[match[g]] = k
	}
	index := map[string]int{}
	var sums [][]float64
	err = walk(func(i int, cols []int64, vals []float64) error {
		name := labels[i]
		if name == "" {
			return nil
		}
		k, ok := index[name]
		if !ok {
			if len(ref.Labels) == MaxReferenceLabels {
				return fmt.Errorf("the reference holds more than %d labels; SituCraft maps to at most %d", MaxReferenceLabels, MaxReferenceLabels)
			}
			k = len(ref.Labels)
			index[name] = k
			ref.Labels = append(ref.Labels, name)
			ref.Cells = append(ref.Cells, 0)
			sums = append(sums, make([]float64, len(ref.Genes)))
		}
		ref.Cells[k]++
		for e, j := range cols {
			c := col[j]
			if c < 0 {
				continue
			}
			v := vals[e]
			if ref.Values == ValuesCounts {
				v = 0
				if totals[i] > 0 {
					v = dmath.Log(1 + vals[e]/(totals[i]/target))
				}
			}
			sums[k][c] += v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := checkLabelCount(len(ref.Labels)); err != nil {
		return nil, err
	}
	ref.sortLabels(sums, func(k int) float64 { return float64(ref.Cells[k]) })
	return ref, nil
}

func checkLabelCount(n int) error {
	switch {
	case n < 2:
		return fmt.Errorf("the reference holds %d labels; two or more are needed", n)
	case n > MaxReferenceLabels:
		return fmt.Errorf("the reference holds %d labels; SituCraft maps to at most %d", n, MaxReferenceLabels)
	}
	return nil
}

// restrict keeps the panel's genes the reference has, in the panel's order, and lists the others.
func (ref *Ref) restrict(match map[int]int, panel []string) {
	ref.Missing = []string{}
	for g := range panel {
		if _, ok := match[g]; ok {
			ref.Genes = append(ref.Genes, g)
		} else {
			ref.Missing = append(ref.Missing, panel[g])
		}
	}
}

// sortLabels orders the labels by name and makes each one's profile (its sums divided by n).
func (ref *Ref) sortLabels(sums [][]float64, n func(int) float64) {
	order := make([]int, len(ref.Labels))
	for k := range order {
		order[k] = k
	}
	sort.SliceStable(order, func(a, b int) bool { return ref.Labels[order[a]] < ref.Labels[order[b]] })
	labels, cells, profiles := make([]string, len(order)), make([]int, len(order)), make([][]float64, len(order))
	for r, k := range order {
		labels[r], cells[r] = ref.Labels[k], ref.Cells[k]
		p := make([]float64, len(sums[k]))
		for g, v := range sums[k] {
			p[g] = v / n(k)
		}
		profiles[r] = p
	}
	ref.Labels, ref.Cells, ref.Profiles = labels, cells, profiles
}

// readProfiles reads a table of profiles: labels by genes (a text column of labels, the genes the
// numeric columns named for them) or genes by labels (a text column of genes, the labels the
// numeric columns). An empty value is 0.
func readProfiles(spec RefSpec, panel []string) (*Ref, error) {
	t, err := ReadTable(spec.Path)
	if err != nil {
		return nil, err
	}
	ref := &Ref{Format: RefProfiles, File: t.File, SHA256: t.SHA256, LabelColumn: spec.LabelColumn, GeneColumn: spec.GeneColumn}
	var labels []string
	var values [][]float64 // [label][the reference's gene]
	var genes []string
	if spec.GeneColumn != "" {
		gc := t.Column(spec.GeneColumn)
		if gc == nil || gc.Text == nil {
			return nil, fmt.Errorf("%s has no text column %q of gene names", t.File, spec.GeneColumn)
		}
		genes = gc.Text
		for _, c := range t.Columns {
			if c.Numbers != nil {
				labels = append(labels, strings.TrimSpace(c.Name))
				values = append(values, c.Numbers)
			}
		}
		ref.LabelColumn = ""
	} else {
		lc := t.Column(spec.LabelColumn)
		if lc == nil || spec.LabelColumn == "" {
			return nil, fmt.Errorf("%s has no column %q of labels", t.File, spec.LabelColumn)
		}
		var cols []*anndata.Column
		for _, c := range t.Columns {
			if c.Numbers != nil {
				genes = append(genes, c.Name)
				cols = append(cols, c)
			}
		}
		for i := 0; i < t.Rows; i++ {
			if missing(lc, i) {
				continue
			}
			row := make([]float64, len(cols))
			for j, c := range cols {
				row[j] = c.Numbers[i]
			}
			labels = append(labels, strings.TrimSpace(lc.String(i)))
			values = append(values, row)
		}
	}
	ref.RefGenes = len(genes)
	seen := map[string]bool{}
	for _, l := range labels {
		if seen[l] {
			return nil, fmt.Errorf("%s gives the label %q twice", t.File, l)
		}
		seen[l] = true
	}
	if err := checkLabelCount(len(labels)); err != nil {
		return nil, err
	}
	match := matchGenes(genes, panel)
	ref.restrict(match, panel)
	if len(ref.Genes) < 2 {
		return nil, fmt.Errorf("%s shares %d of the panel's genes; two or more are needed", t.File, len(ref.Genes))
	}
	sums := make([][]float64, len(labels))
	var all []float64
	for k := range labels {
		sums[k] = make([]float64, len(ref.Genes))
		for s, g := range ref.Genes {
			if v := values[k][match[g]]; !math.IsNaN(v) {
				sums[k][s] = v
				all = append(all, v)
			}
		}
	}
	if valuesOf(all) == valuesScaled {
		return nil, fmt.Errorf("%s holds profiles below 0 (scaled): give each label's mean expression", t.File)
	}
	ref.Values = "as given" // a profile's ranks are what is compared
	ref.Labels, ref.Cells = labels, make([]int, len(labels))
	ref.sortLabels(sums, func(int) float64 { return 1 })
	return ref, nil
}

// RefParams are a mapping's settings: Tune, the labels kept for fine-tuning (within it of the best
// correlation); Margin, the lead a cell's label needs over the next; Markers, the genes taken for
// each ordered pair of labels (0: 500 × (2/3)^log₂(labels), at least 1).
type RefParams struct {
	Tune    float64 `json:"tune"`
	Margin  float64 `json:"margin"`
	Markers int     `json:"markers"`
}

// DefaultRefParams fine-tune within 0.05, take a cell's best label (no margin), and take the
// genes per pair by the labels' number.
func DefaultRefParams() RefParams { return RefParams{Tune: 0.05} }

// Check says what is wrong with the parameters, if anything.
func (p RefParams) Check() error {
	switch {
	case !(p.Tune >= 0 && p.Tune <= 1):
		return errors.New("Fine-tuning is a correlation from 0 to 1")
	case !(p.Margin >= 0 && p.Margin <= 1):
		return errors.New("Margin is a correlation from 0 to 1")
	case p.Markers < 0 || p.Markers > 10000:
		return errors.New("Genes per pair is a whole number from 0 (by the labels' number) to 10000")
	}
	return nil
}

// MarkersPer is the genes taken for each pair of a reference's labels.
func (p RefParams) MarkersPer(labels int) int {
	if p.Markers > 0 {
		return p.Markers
	}
	return max(1, int(math.Round(500*dmath.Pow(2.0/3.0, dmath.Log(float64(labels))/dmath.Log(2)))))
}

// mul is a product, rounded: never fused with a sum, so results are the same on every processor.
func mul(a, b float64) float64 { return float64(a * b) }

// rankAvg answers the ranks (1…n; equal values their mean rank) of values.
func rankAvg(v []float64) []float64 {
	order := make([]int, len(v))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return v[order[a]] < v[order[b]] })
	r := make([]float64, len(v))
	for i := 0; i < len(order); {
		j := i
		for j+1 < len(order) && v[order[j+1]] == v[order[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			r[order[k]] = avg
		}
		i = j + 1
	}
	return r
}

// pairMarkers are, for each ordered pair (a, b), the shared genes most higher in a's profile than
// b's, at most n of them: by the difference, then the panel's order.
func (ref *Ref) pairMarkers(n int) [][][]int {
	L, G := len(ref.Labels), len(ref.Genes)
	out := make([][][]int, L)
	diff := make([]float64, G)
	order := make([]int, G)
	for a := 0; a < L; a++ {
		out[a] = make([][]int, L)
		for b := 0; b < L; b++ {
			if a == b {
				continue
			}
			for g := 0; g < G; g++ {
				diff[g] = ref.Profiles[a][g] - ref.Profiles[b][g]
				order[g] = g
			}
			sort.SliceStable(order, func(x, y int) bool { return diff[order[x]] > diff[order[y]] })
			for _, g := range order[:min(n, G)] {
				if diff[g] > 0 {
					out[a][b] = append(out[a][b], g)
				}
			}
		}
	}
	return out
}

// compared is a set of labels, the genes compared for it, and each label's centred ranks over them.
type compared struct {
	labels []int       // the reference's labels, increasing
	genes  []int       // shared genes, increasing
	pos    []int       // shared gene → position among genes, or −1
	ranks  [][]float64 // [label of the set][position]: rank less the mean rank
	scale  []float64   // each label's √Σ(rank − mean)²
}

func (ref *Ref) compare(labels []int, pairs [][][]int) *compared {
	G := len(ref.Genes)
	in := make([]bool, G)
	for _, a := range labels {
		for _, b := range labels {
			if a != b {
				for _, g := range pairs[a][b] {
					in[g] = true
				}
			}
		}
	}
	c := &compared{labels: labels, pos: make([]int, G)}
	for g := 0; g < G; g++ {
		c.pos[g] = -1
		if in[g] {
			c.pos[g] = len(c.genes)
			c.genes = append(c.genes, g)
		}
	}
	m := float64(len(c.genes)+1) / 2
	vals := make([]float64, len(c.genes))
	for _, l := range labels {
		for k, g := range c.genes {
			vals[k] = ref.Profiles[l][g]
		}
		r := rankAvg(vals)
		ss := 0.0
		for k := range r {
			r[k] -= m
			ss += mul(r[k], r[k])
		}
		c.ranks = append(c.ranks, r)
		c.scale = append(c.scale, math.Sqrt(ss))
	}
	return c
}

// correlate answers a cell's Spearman correlation with each of the set's labels, from its values
// over the shared genes it expresses (genes increasing, values above 0): the set's genes it does
// not express tie at the lowest ranks. NaN where the cell's or the profile's ranks are all equal.
func (c *compared) correlate(genes []int, values []float64, out []float64) {
	G := len(c.genes)
	var pos []int
	var vals []float64
	for k, g := range genes {
		if at := c.pos[g]; at >= 0 && values[k] > 0 {
			pos = append(pos, at)
			vals = append(vals, values[k])
		}
	}
	z := float64(G - len(pos)) // genes at 0
	m := float64(G+1) / 2
	zero := (z+1)/2 - m // the zeros' centred rank
	r := rankAvg(vals)
	ss := mul(z, mul(zero, zero))
	for k := range r {
		r[k] += z - m
		ss += mul(r[k], r[k])
	}
	sc := math.Sqrt(ss)
	for l := range c.labels {
		if sc == 0 || c.scale[l] == 0 {
			out[l] = math.NaN()
			continue
		}
		// The profile's centred ranks sum to 0: those at the cell's zero genes, to less the rest's.
		dot, rest := 0.0, 0.0
		for k, at := range pos {
			dot += mul(r[k], c.ranks[l][at])
			rest += c.ranks[l][at]
		}
		dot -= mul(zero, rest)
		out[l] = dot / mul(sc, c.scale[l])
	}
}

// RefResult is a mapping of a clustering's cells: each cell's first-round correlation with every
// label, its last round's correlations (NaN for the labels not in it, all NaN when there was one
// round), its label (−1 for none), its lead, and its rounds.
type RefResult struct {
	Ref      *Ref
	Params   RefParams
	Per      int         // genes per pair
	Compared []int       // the first round's genes (shared-gene indices)
	First    [][]float64 // [label][cell]
	Last     [][]float64
	Label    []int32
	Lead     []float64
	Rounds   []uint8
	Cells    []uint32
	Cluster  []uint32
	Clusters int
	Target   float64
	Genes    []string // the panel's
	Seconds  map[string]float64
}

// Mapper maps cells to a reference: its pairs' genes, and the sets of labels compared (made once
// for each set fine-tuning reaches, and kept).
type Mapper struct {
	ref    *Ref
	p      RefParams
	pairs  [][][]int
	shared []int // panel gene → shared gene, or −1
	first  *compared
	mu     sync.Mutex
	sets   map[string]*compared
}

// NewMapper prepares a mapping to a reference on a panel of genes.
func NewMapper(ref *Ref, panel int, p RefParams) (*Mapper, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	mp := &Mapper{ref: ref, p: p, pairs: ref.pairMarkers(p.MarkersPer(len(ref.Labels))), shared: make([]int, panel), sets: map[string]*compared{}}
	for g := range mp.shared {
		mp.shared[g] = -1
	}
	for k, g := range ref.Genes {
		if g >= panel {
			return nil, fmt.Errorf("the reference names gene %d of a panel of %d", g, panel)
		}
		mp.shared[g] = k
	}
	all := make([]int, len(ref.Labels))
	for k := range all {
		all[k] = k
	}
	mp.first = mp.set(all)
	if len(mp.first.genes) < 2 {
		return nil, errors.New("the reference's labels differ in fewer than two of the panel's genes")
	}
	return mp, nil
}

// Compared are the first round's genes (shared-gene indices).
func (mp *Mapper) Compared() []int { return mp.first.genes }

func (mp *Mapper) set(labels []int) *compared {
	key := fmt.Sprint(labels)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if c, ok := mp.sets[key]; ok {
		return c
	}
	c := mp.ref.compare(labels, mp.pairs)
	mp.sets[key] = c
	return c
}

// CellMapping is one cell's mapping: its rounds (none when it expresses too little of the genes
// compared to rank them), its label (−1 for none) and the last round's lead.
type CellMapping struct {
	Rounds []Round
	Label  int
	Lead   float64
}

// Round is a round of a cell's fine-tuning: the labels compared, their correlations and the genes
// compared (shared genes).
type Round struct {
	Labels []int
	Scores []float64
	Genes  []int
}

// Map maps one cell from its expression (the panel's genes it expresses, increasing, and their
// log-normalized values).
func (mp *Mapper) Map(genes []int, values []float64) CellMapping {
	var g []int
	var v []float64
	for k, gene := range genes {
		if s := mp.shared[gene]; s >= 0 {
			g = append(g, s)
			v = append(v, values[k])
		}
	}
	out := CellMapping{Label: -1, Lead: math.NaN()}
	c := mp.first
	for {
		scores := make([]float64, len(c.labels))
		c.correlate(g, v, scores)
		best, top, next := -1, math.Inf(-1), math.Inf(-1)
		for j, s := range scores {
			switch {
			case math.IsNaN(s):
			case best < 0 || s > top:
				best, top, next = j, s, top
			case s > next:
				next = s
			}
		}
		if best < 0 || math.IsInf(next, -1) {
			break // the round before stands
		}
		out.Rounds = append(out.Rounds, Round{Labels: c.labels, Scores: scores, Genes: c.genes})
		out.Label, out.Lead = c.labels[best], top-next
		var keep []int
		for j, s := range scores {
			if !math.IsNaN(s) && s >= top-mp.p.Tune {
				keep = append(keep, c.labels[j])
			}
		}
		if len(keep) <= 1 || len(keep) == len(c.labels) || len(out.Rounds) == 255 {
			break
		}
		c = mp.set(keep)
	}
	if !(out.Lead >= mp.p.Margin && out.Lead > 0) {
		out.Label = -1
	}
	return out
}

// MapReference labels a clustering's cells from a reference.
func MapReference(ctx context.Context, e *cluster.Expression, ref *Ref, p RefParams, clusters int, target float64, workers int) (*RefResult, error) {
	mp, err := NewMapper(ref, len(e.Genes), p)
	if err != nil {
		return nil, err
	}
	n, L := len(e.Cells), len(ref.Labels)
	res := &RefResult{Ref: ref, Params: p, Per: p.MarkersPer(L), Compared: mp.first.genes, Label: make([]int32, n), Lead: make([]float64, n), Rounds: make([]uint8, n),
		Cells: e.Cells, Cluster: e.Cluster, Clusters: clusters, Target: target, Genes: e.Genes}
	for k := 0; k < L; k++ {
		first, last := make([]float64, n), make([]float64, n)
		for i := range last {
			first[i], last[i] = math.NaN(), math.NaN()
		}
		res.First, res.Last = append(res.First, first), append(res.Last, last)
	}
	workers = max(workers, 1)
	chunk := max((n+workers-1)/workers, 1)
	var wg sync.WaitGroup
	var stop sync.Once
	var stopped error
	for lo := 0; lo < n; lo += chunk {
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				if i%1024 == 0 && ctx.Err() != nil {
					stop.Do(func() { stopped = ctx.Err() })
					return
				}
				genes, _, values := e.Row(i)
				m := mp.Map(genes, values)
				res.Rounds[i] = uint8(len(m.Rounds))
				res.Lead[i] = m.Lead
				res.Label[i] = int32(m.Label)
				if len(m.Rounds) == 0 {
					continue
				}
				for k, s := range m.Rounds[0].Scores {
					res.First[k][i] = s
				}
				if last := m.Rounds[len(m.Rounds)-1]; len(m.Rounds) > 1 {
					for j, l := range last.Labels {
						res.Last[l][i] = last.Scores[j]
					}
				}
			}
		}(lo, min(n, lo+chunk))
	}
	wg.Wait()
	return res, stopped
}
