package label

// Labels made elsewhere, imported: a table's label column (and, optionally, a confidence and a
// color column) matched to a segmentation's cells by id. A table naming a cell the segmentation
// does not have is refused, with how many; the segmentation's cells the table does not label are
// counted. The label set is an artifact of kind "labels" with its source in place of a marker set:
// one level, no scores.

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"situcraft/internal/anndata"
)

// Source says where imported labels came from: the table's format, file and SHA-256 (of the
// file, or of an AnnData obs's files), where in it, and its columns; for the instrument's
// clusters, the tool and its version.
type Source struct {
	Format           string `json:"format"`
	File             string `json:"file"`
	SHA256           string `json:"sha256"`
	Path             string `json:"path,omitempty"`
	IDColumn         string `json:"idColumn"`
	Column           string `json:"column"`
	ConfidenceColumn string `json:"confidenceColumn,omitempty"`
	ColorColumn      string `json:"colorColumn,omitempty"`
	Rows             int    `json:"rows"`
	Tool             string `json:"tool,omitempty"`
	Version          string `json:"version,omitempty"`
	Title            string `json:"title,omitempty"`
}

// MaxImportedLabels bounds a label column's distinct values: more are not labels.
const MaxImportedLabels = 1000

// ColumnInfo describes a column for choosing what it is.
type ColumnInfo struct {
	Name     string     `json:"name"`
	Kind     string     `json:"kind"` // text or number
	Missing  int        `json:"missing"`
	Distinct int        `json:"distinct"`
	Unique   bool       `json:"unique"` // every present value distinct
	Examples []string   `json:"examples"`
	Range    [2]float64 `json:"range,omitempty"`
}

// ColumnRoles are the columns chosen for each role ("" for none).
type ColumnRoles struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Confidence string `json:"confidence"`
	Color      string `json:"color"`
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Describe describes a table's columns and suggests their roles: the id by its usual names, else
// the first text column whose values are all distinct; the label by its usual names, else the
// first text column of 2 to 1,000 values, repeated; a confidence column by its name; a color
// column of hexadecimal colors.
func Describe(t *Table) ([]ColumnInfo, ColumnRoles) {
	var out []ColumnInfo
	for _, c := range t.Columns {
		info := ColumnInfo{Name: c.Name, Kind: "text"}
		if c.Numbers != nil {
			info.Kind = "number"
			info.Range = [2]float64{math.Inf(1), math.Inf(-1)}
		}
		seen := map[string]bool{}
		for i := 0; i < c.Len(); i++ {
			if missing(c, i) {
				info.Missing++
				continue
			}
			v := c.String(i)
			if !seen[v] {
				seen[v] = true
				if len(info.Examples) < 6 {
					info.Examples = append(info.Examples, v)
				}
			}
			if c.Numbers != nil {
				info.Range[0] = math.Min(info.Range[0], c.Numbers[i])
				info.Range[1] = math.Max(info.Range[1], c.Numbers[i])
			}
		}
		info.Distinct = len(seen)
		info.Unique = info.Distinct == c.Len()-info.Missing && info.Distinct > 0
		if c.Numbers != nil && info.Distinct == 0 {
			info.Range = [2]float64{}
		}
		out = append(out, info)
	}
	byName := func(names ...string) string {
		for _, n := range names {
			for _, info := range out {
				if strings.EqualFold(info.Name, n) {
					return info.Name
				}
			}
		}
		return ""
	}
	var r ColumnRoles
	if r.ID = byName("cell_id", "Cell ID", "cell", "cellid", "cell_ID", "barcode", "barcodes", "cell_barcode", "id"); r.ID == "" {
		for _, info := range out {
			if info.Kind == "text" && info.Unique && info.Missing == 0 {
				r.ID = info.Name
				break
			}
		}
	}
	r.Color = byName("color", "colour")
	if r.Color == "" {
		for _, info := range out {
			if info.Kind == "text" && info.Distinct > 0 && allHex(info.Examples) {
				r.Color = info.Name
				break
			}
		}
	}
	if r.Label = byName("group", "cell_type", "celltype", "cell type", "cell_types", "label", "labels", "annotation", "annotations", "predicted_cell_type", "predicted.celltype", "predicted.id", "majority_voting", "cluster", "clusters", "leiden", "louvain", "cell_class", "class", "type"); r.Label == r.ID {
		r.Label = ""
	}
	if r.Label == "" {
		for _, info := range out {
			if info.Kind == "text" && info.Name != r.ID && info.Name != r.Color && info.Distinct >= 2 && info.Distinct <= MaxImportedLabels && info.Distinct*2 <= t.Rows-info.Missing {
				r.Label = info.Name
				break
			}
		}
	}
	for _, info := range out {
		n := strings.ToLower(info.Name)
		if info.Kind == "number" && info.Name != r.ID && info.Name != r.Label && (strings.Contains(n, "confidence") || strings.Contains(n, "score") || strings.Contains(n, "prob") || strings.Contains(n, "conf")) {
			r.Confidence = info.Name
			break
		}
	}
	return out, r
}

func allHex(v []string) bool {
	for _, s := range v {
		if !hexColor.MatchString(s) {
			return false
		}
	}
	return len(v) > 0
}

func missing(c *anndata.Column, i int) bool {
	if c.Missing != nil && c.Missing[i] {
		return true
	}
	if c.Numbers != nil {
		return math.IsNaN(c.Numbers[i])
	}
	return missingText[strings.TrimSpace(c.Text[i])]
}

// Match is how a table's ids match a segmentation's cells: its rows, those naming a cell of the
// segmentation, those naming none it has (with the first few), those naming a cell already named,
// those without an id; and of the segmentation's cells, those it does not name.
type Match struct {
	Rows              int      `json:"rows"`
	Matched           int      `json:"matched"`
	NotInSegmentation int      `json:"notInSegmentation"`
	Examples          []string `json:"examples"`
	Duplicates        int      `json:"duplicates"`
	DuplicateExamples []string `json:"duplicateExamples"`
	WithoutID         int      `json:"withoutId"`
	Cells             int      `json:"cells"`
	CellsNotNamed     int      `json:"cellsNotNamed"`
}

// MatchIDs matches a table's id column to a segmentation's cells' ids: each row's cell (an index
// among them, −1 for none), and the match.
func MatchIDs(ids *anndata.Column, cellIDs []string) ([]int32, Match) {
	index := make(map[string]int32, len(cellIDs))
	for i, id := range cellIDs {
		index[id] = int32(i)
	}
	m := Match{Rows: ids.Len(), Cells: len(cellIDs), Examples: []string{}, DuplicateExamples: []string{}}
	rows := make([]int32, ids.Len())
	named := make([]bool, len(cellIDs))
	for i := range rows {
		rows[i] = -1
		if missing(ids, i) {
			m.WithoutID++
			continue
		}
		id := ids.String(i)
		k, ok := index[id]
		if !ok {
			m.NotInSegmentation++
			if len(m.Examples) < 5 {
				m.Examples = append(m.Examples, id)
			}
			continue
		}
		if named[k] {
			m.Duplicates++
			if len(m.DuplicateExamples) < 5 {
				m.DuplicateExamples = append(m.DuplicateExamples, id)
			}
			continue
		}
		named[k] = true
		rows[i] = k
		m.Matched++
	}
	m.CellsNotNamed = len(cellIDs) - m.Matched
	return rows, m
}

// ImportSpec names the columns of an import.
type ImportSpec struct {
	IDColumn         string `json:"idColumn"`
	Column           string `json:"column"`
	ConfidenceColumn string `json:"confidenceColumn,omitempty"`
	ColorColumn      string `json:"colorColumn,omitempty"`
}

// Imported are labels imported for a segmentation's cells: the labels by decreasing cells (equal
// counts by name), their colors where the table gave them, and each cell's label (−1 for none)
// and confidence (NaN for none).
type Imported struct {
	Source     Source
	Labels     []string
	Colors     []string
	Cells      []int // cells carrying each label
	Label      []int32
	Confidence []float32
	Match      Match
}

// Import imports a table's labels for a segmentation's cells (their ids, in order). Refused: a
// column it does not have, a confidence column not of numbers, a color not hexadecimal, a row
// naming a cell the segmentation does not have or one already named, more than 1,000 labels, or
// none.
func Import(t *Table, spec ImportSpec, cellIDs []string) (*Imported, error) {
	col := func(name, what string) (*anndata.Column, error) {
		if name == "" {
			return nil, nil
		}
		c := t.Column(name)
		if c == nil {
			return nil, fmt.Errorf("%s has no column %q (%s)", t.File, name, what)
		}
		return c, nil
	}
	ids, err := col(spec.IDColumn, "the cells' ids")
	if err != nil {
		return nil, err
	}
	labels, err := col(spec.Column, "the labels")
	if err != nil {
		return nil, err
	}
	if ids == nil || labels == nil {
		return nil, errors.New("name the column of the cells' ids and the column of their labels")
	}
	if spec.IDColumn == spec.Column {
		return nil, errors.New("the labels' column is the ids' column")
	}
	conf, err := col(spec.ConfidenceColumn, "the confidence")
	if err != nil {
		return nil, err
	}
	if conf != nil && conf.Numbers == nil {
		return nil, fmt.Errorf("the confidence column %q holds text, not numbers", spec.ConfidenceColumn)
	}
	colors, err := col(spec.ColorColumn, "the colors")
	if err != nil {
		return nil, err
	}
	rows, m := MatchIDs(ids, cellIDs)
	if m.NotInSegmentation > 0 {
		return nil, fmt.Errorf("%d of %s's %d rows name cells this segmentation does not have (%s): its labels are of another segmentation or dataset", m.NotInSegmentation, t.File, m.Rows, strings.Join(m.Examples, ", "))
	}
	if m.Duplicates > 0 {
		return nil, fmt.Errorf("%d of %s's rows name a cell another row names (%s)", m.Duplicates, t.File, strings.Join(m.DuplicateExamples, ", "))
	}
	n := len(cellIDs)
	out := &Imported{Label: make([]int32, n), Confidence: make([]float32, n), Match: m}
	for i := range out.Label {
		out.Label[i] = -1
		out.Confidence[i] = float32(math.NaN())
	}
	count := map[string]int{}
	color := map[string]string{}
	for r, cell := range rows {
		if cell < 0 || missing(labels, r) {
			continue
		}
		name := strings.TrimSpace(labels.String(r))
		count[name]++
		if colors != nil && !missing(colors, r) {
			c := strings.TrimSpace(colors.String(r))
			if !hexColor.MatchString(c) {
				return nil, fmt.Errorf("the color %q (row %d) is not a hexadecimal color such as #ff0055", c, r+1)
			}
			if _, ok := color[name]; !ok {
				color[name] = strings.ToLower(c)
			}
		}
	}
	if len(count) == 0 {
		return nil, fmt.Errorf("%s labels none of the segmentation's cells", t.File)
	}
	if len(count) > MaxImportedLabels {
		return nil, fmt.Errorf("the column %q holds %d distinct values; a label column holds at most %d", spec.Column, len(count), MaxImportedLabels)
	}
	for name := range count {
		out.Labels = append(out.Labels, name)
	}
	sort.Slice(out.Labels, func(a, b int) bool {
		x, y := out.Labels[a], out.Labels[b]
		return count[x] > count[y] || count[x] == count[y] && x < y
	})
	index := map[string]int32{}
	for k, name := range out.Labels {
		index[name] = int32(k)
		out.Colors = append(out.Colors, color[name])
		out.Cells = append(out.Cells, count[name])
	}
	for r, cell := range rows {
		if cell < 0 || missing(labels, r) {
			continue
		}
		out.Label[cell] = index[strings.TrimSpace(labels.String(r))]
		if conf != nil && !math.IsNaN(conf.Numbers[r]) {
			out.Confidence[cell] = float32(conf.Numbers[r])
		}
	}
	out.Source = Source{Format: t.Format, File: t.File, SHA256: t.SHA256, Path: t.Path, IDColumn: spec.IDColumn, Column: spec.Column, ConfidenceColumn: spec.ConfidenceColumn, ColorColumn: spec.ColorColumn, Rows: t.Rows}
	return out, nil
}
