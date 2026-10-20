package query

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

// fake answers conditions over six cells: an attribute, a gene's counts, two labels and a region.
type fake struct{}

func (fake) Len() int { return 6 }

func (fake) Match(c *Condition) ([]bool, error) {
	switch c.Kind {
	case "attribute":
		return c.InRange([]float64{5, 10, 20, math.NaN(), 40, 80}), nil
	case "gene":
		return c.InRange([]float64{0, 1, 3, 0, 2, 0}), nil
	case "labels":
		codes, err := c.Codes([]string{"Tumor", "T cells"}, 0, "the label set")
		if err != nil {
			return nil, err
		}
		cells := []int32{0, 1, 1, -1, 0, 1}
		out := make([]bool, len(cells))
		for i, k := range cells {
			out[i] = k >= 0 && codes[k]
		}
		return out, nil
	case "region":
		return []bool{true, true, false, false, true, false}, nil
	}
	return nil, fmt.Errorf("no %s", c.Kind)
}

func eval(t *testing.T, tree string) string {
	t.Helper()
	n, err := Parse(json.RawMessage(tree), Cells)
	if err != nil {
		t.Fatalf("%s: %v", tree, err)
	}
	keep, err := n.Eval(fake{})
	if err != nil {
		t.Fatalf("%s: %v", tree, err)
	}
	var b strings.Builder
	for _, v := range keep {
		if v {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

func TestTreesKeepWhatTheySay(t *testing.T) {
	for _, c := range []struct{ tree, want string }{
		{`{"attribute":"transcripts","min":10}`, "011011"},
		{`{"attribute":"transcripts","min":10,"max":40}`, "011010"},
		{`{"attribute":"transcripts"}`, "111011"}, // recorded: a cell without one fails
		{`{"gene":"EPCAM"}`, "011010"},            // at least 1 when no bound
		{`{"gene":"EPCAM","max":0}`, "100101"},
		{`{"labels":"lab-1","in":["t CELLS"]}`, "011001"},
		{`{"labels":"lab-1","in":[0]}`, "100010"},
		{`{"all":[{"region":"Nest"},{"labels":"lab-1","in":["Tumor"]}]}`, "100010"},
		{`{"any":[{"gene":"EPCAM","min":3},{"attribute":"transcripts","min":80}]}`, "001001"},
		{`{"not":{"region":"Nest"}}`, "001101"},
		{`{"all":[{"not":{"region":"Nest"}},{"any":[{"gene":"EPCAM"},{"labels":"lab-1","in":["T cells"]}]}]}`, "001001"},
	} {
		if got := eval(t, c.tree); got != c.want {
			t.Errorf("%s: %s, want %s", c.tree, got, c.want)
		}
	}
}

func TestMalformedTreesAreRefusedWithWhy(t *testing.T) {
	for _, c := range []struct {
		tree string
		of   Of
		says string
	}{
		{``, Cells, "empty"},
		{`[1]`, Cells, "not an object"},
		{`{"all":[]}`, Cells, "all is empty"},
		{`{"all":[{"gene":"A"}],"any":[{"gene":"B"}]}`, Cells, "no other key"},
		{`{"not":{"gene":"A"},"region":"x"}`, Cells, "no other key"},
		{`{"attribute":"size","min":1}`, Cells, `no attribute "size"`},
		{`{"attribute":"area","in":[1]}`, Cells, "tested with min and max"},
		{`{"attribute":"method","min":1}`, Cells, "tested with in"},
		{`{"attribute":"area","min":5,"max":1}`, Cells, "min 5 is above max 1"},
		{`{"attribute":"area","min":"5"}`, Cells, "min is a number"},
		{`{"gene":"A","region":"x"}`, Cells, "names both gene and region"},
		{`{"labels":"lab-1"}`, Cells, "names what it keeps with in"},
		{`{"labels":"lab-1","in":[1.5]}`, Cells, "names and whole numbers"},
		{`{"clusters":"clu-1","in":[1],"min":3}`, Cells, "takes clusters, in, not min"},
		{`{"box":[1,2,3]}`, Cells, "box is [x0, y0, x1, y1]"},
		{`{"box":[5,5,1,9]}`, Cells, "above its x0"},
		{`{"assigned":true}`, Cells, "is not a condition of cells"},
		{`{"sql":"select *"}`, Cells, "is not a condition of cells"},
		{`{"labels":"lab-1","in":["A"]}`, Transcripts, "is not a condition of transcripts"},
		{`{"cells":{"labels":"lab-1"}}`, Transcripts, "names what it keeps with in"},
		{`{"gene":[]}`, Transcripts, "a gene's name or a list"},
		{strings.Repeat(`{"not":`, 40) + `{"gene":"A"}` + strings.Repeat(`}`, 40), Cells, "deeper than"},
	} {
		_, err := Parse(json.RawMessage(c.tree), c.of)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%.60s: %v, want it to say %q", c.tree, err, c.says)
		}
	}
	var many []string
	for i := 0; i < 1200; i++ {
		many = append(many, `{"gene":"A"}`)
	}
	if _, err := Parse(json.RawMessage(`{"any":[`+strings.Join(many, ",")+`]}`), Cells); err == nil || !strings.Contains(err.Error(), "more than 1000 nodes") {
		t.Errorf("a tree of 1,200 nodes: %v", err)
	}
}

// Transcripts: genes as a name or a list, a tree of cells inside.
func TestTranscriptTrees(t *testing.T) {
	n, err := Parse(json.RawMessage(`{"all":[{"gene":["EPCAM","KRT8"]},{"assigned":true},{"cells":{"labels":"lab-1","in":["Tumor"]}}]}`), Transcripts)
	if err != nil {
		t.Fatal(err)
	}
	if n.All[0].Cond.Name != "EPCAM, KRT8" || !n.All[1].Cond.Assigned || n.All[2].Cond.Cells.Cond.Kind != "labels" {
		t.Fatalf("%+v", n.All)
	}
	if text := n.Text(); text != "of EPCAM, KRT8 and assigned to a cell and in cells labeled Tumor by lab-1" {
		t.Fatalf("text: %s", text)
	}
}

// Which are kept, as the page writes them (lib/filters.js keepBytes and keepChecksum).
func TestKeepBytesAsThePageWritesThem(t *testing.T) {
	keep := []bool{true, false, false, true, true, false, false, false, true}
	b := KeepBytes(keep)
	if fmt.Sprintf("% x", b) != "09 00 00 00 19 01" {
		t.Fatalf("% x", b)
	}
	// The SHA-256 of 09 00 00 00 19 01 (printf '\x09\x00\x00\x00\x19\x01' | shasum -a 256).
	if got := Checksum(keep); got != "73015ae9a1d98b0175f57baca92ed58671fa3d6746531a6757909bf9cd445b7f" {
		t.Fatalf("checksum %s", got)
	}
	if Count(keep) != 4 {
		t.Fatal("count")
	}
}

// columns answers conditions over random cells: three attributes (some cells without a value) and
// a label set's labels (some cells unlabeled).
type columns struct {
	values map[string][]float64
	labels []int32
}

var labelNames = []string{"Tumor", "T cells", "Fibroblasts"}

func (c columns) Len() int { return len(c.labels) }

func (c columns) Match(cond *Condition) ([]bool, error) {
	if cond.Kind == "labels" {
		codes, err := cond.Codes(labelNames, 0, "the label set")
		if err != nil {
			return nil, err
		}
		out := make([]bool, len(c.labels))
		for i, k := range c.labels {
			out[i] = k >= 0 && codes[k]
		}
		return out, nil
	}
	return cond.InRange(c.values[cond.Name]), nil
}

// brute keeps a cell by reading the tree as written, one cell at a time.
func brute(c columns, tree map[string]any, i int) bool {
	if list, ok := tree["all"].([]any); ok {
		for _, child := range list {
			if !brute(c, child.(map[string]any), i) {
				return false
			}
		}
		return true
	}
	if list, ok := tree["any"].([]any); ok {
		for _, child := range list {
			if brute(c, child.(map[string]any), i) {
				return true
			}
		}
		return false
	}
	if child, ok := tree["not"].(map[string]any); ok {
		return !brute(c, child, i)
	}
	if _, ok := tree["labels"]; ok {
		k := c.labels[i]
		for _, v := range tree["in"].([]any) {
			if name, ok := v.(string); ok && k >= 0 && strings.EqualFold(name, labelNames[k]) {
				return true
			}
			if n, ok := v.(float64); ok && int32(n) == k {
				return true
			}
		}
		return false
	}
	v := c.values[tree["attribute"].(string)][i]
	if math.IsNaN(v) {
		return false
	}
	if lo, ok := tree["min"].(float64); ok && v < lo {
		return false
	}
	if hi, ok := tree["max"].(float64); ok && v > hi {
		return false
	}
	return true
}

// Random trees, evaluated a column at a time, keep the cells a reading of each tree one cell at a
// time keeps.
func TestTreesAgainstABruteForceEvaluator(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 3))
	const n = 300
	c := columns{values: map[string][]float64{}, labels: make([]int32, n)}
	attrs := []string{"transcripts", "area", "density"}
	for _, a := range attrs {
		v := make([]float64, n)
		for i := range v {
			v[i] = float64(rng.IntN(100))
			if rng.IntN(20) == 0 {
				v[i] = math.NaN()
			}
		}
		c.values[a] = v
	}
	for i := range c.labels {
		c.labels[i] = int32(rng.IntN(4)) - 1
	}
	var grow func(depth int) map[string]any
	grow = func(depth int) map[string]any {
		switch k := rng.IntN(6); {
		case depth < 4 && k == 0:
			return map[string]any{"not": grow(depth + 1)}
		case depth < 4 && k <= 2:
			list := []any{}
			for range 1 + rng.IntN(3) {
				list = append(list, grow(depth+1))
			}
			return map[string]any{[]string{"all", "any"}[k-1]: list}
		case k == 3:
			in := []any{labelNames[rng.IntN(3)]}
			if rng.IntN(2) == 0 {
				in = append(in, float64(rng.IntN(3)))
			}
			return map[string]any{"labels": "lab-1", "in": in}
		}
		cond := map[string]any{"attribute": attrs[rng.IntN(len(attrs))]}
		lo := float64(rng.IntN(100))
		switch rng.IntN(3) {
		case 0:
			cond["min"] = lo
		case 1:
			cond["max"] = lo
		default:
			cond["min"], cond["max"] = lo, lo+float64(rng.IntN(50))
		}
		return cond
	}
	for range 500 {
		tree := grow(0)
		raw, _ := json.Marshal(tree)
		node, err := Parse(raw, Cells)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		keep, err := node.Eval(c)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		for i := range n {
			if keep[i] != brute(c, tree, i) {
				t.Fatalf("%s: cell %d kept %v", raw, i, keep[i])
			}
		}
	}
}
