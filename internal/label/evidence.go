package label

// The evidence of a cell's labels from markers: at each level its candidates' scores, the label it
// took (or the best, where it took none) and the next, and for both their markers in the cell —
// each gene's count and log-normalized value against its mean over the cells — and their control
// genes' mean in the cell.

import (
	"math"

	"situcraft/internal/cluster"
)

// GeneEvidence is a marker in a cell: its count and value there, and its mean over the cells.
type GeneEvidence struct {
	Gene  string  `json:"gene"`
	Count uint32  `json:"count"`
	Value float64 `json:"value"`
	Mean  float64 `json:"mean"`
}

// ListEvidence is a list of markers in a cell: each gene, their mean in the cell, their control
// genes' mean in it, and the difference (the list's score).
type ListEvidence struct {
	Genes    []GeneEvidence `json:"genes"`
	Mean     float64        `json:"mean"`
	Controls float64        `json:"controls"`
	Score    float64        `json:"score"`
}

// LabelEvidence is a label's evidence in a cell: its score (the positive markers' less the
// negative's where above 0, as recorded) and both lists.
type LabelEvidence struct {
	Set      int           `json:"set"`
	Name     string        `json:"name"`
	Score    float32       `json:"score"`
	Positive ListEvidence  `json:"positive"`
	Negative *ListEvidence `json:"negative,omitempty"`
}

// LevelEvidence is a level as the cell met it: the label it took (−1: none, it stopped above),
// its lead, the best candidate and the next (−1 when alone), and every candidate's score.
type LevelEvidence struct {
	Level      string          `json:"level"`
	Label      int32           `json:"label"`
	Lead       *float32        `json:"lead"`
	Best       int             `json:"best"`
	Next       int             `json:"next"`
	Candidates []int           `json:"candidates"`
	Scores     []float32       `json:"scores"`
	Evidence   []LabelEvidence `json:"evidence"`
}

// Evidence is a cell's labels from markers, with why.
type Evidence struct {
	Cell    int             `json:"cell"`
	Index   int             `json:"index"`
	Levels  []LevelEvidence `json:"levels"`
	States  []LabelEvidence `json:"states"`
	Carries []bool          `json:"carries"`
	// Also is another label's evidence asked for (the label most of the cell's neighbors carry).
	Also    *LabelEvidence `json:"also,omitempty"`
	Margin  float64        `json:"margin"`
	Minimum float64        `json:"minimum"`
	State   float64        `json:"state"`
}

// CellEvidence answers the evidence of the i-th analyzed cell's labels (a label set from markers
// made from e's clustering: its description and labels), and of label also (an index among the
// sets; −1 for none).
func CellEvidence(e *cluster.Expression, d *Description, lab *Labels, i, also int) *Evidence {
	gene := map[string]int{}
	for g, name := range e.Genes {
		gene[name] = g
	}
	means := e.GeneMeans()
	genes, counts, values := e.Row(i)
	count := map[int]uint32{}
	value := map[int]float64{}
	for k, g := range genes {
		count[g], value[g] = counts[k], values[k]
	}
	list := func(names, controls []string) ListEvidence {
		var out ListEvidence
		for _, n := range names {
			g := gene[n]
			out.Genes = append(out.Genes, GeneEvidence{Gene: n, Count: count[g], Value: value[g], Mean: means[g]})
			out.Mean += value[g]
		}
		if len(names) > 0 {
			out.Mean /= float64(len(names))
		}
		for _, n := range controls {
			out.Controls += value[gene[n]]
		}
		if len(controls) > 0 {
			out.Controls /= float64(len(controls))
		}
		out.Score = out.Mean - out.Controls
		return out
	}
	label := func(k int) LabelEvidence {
		s := d.Sets[k]
		le := LabelEvidence{Set: k, Name: s.Name, Score: lab.Score[k][i], Positive: list(s.Positive, s.PositiveControls)}
		if len(s.Negative) > 0 {
			neg := list(s.Negative, s.NegativeControls)
			le.Negative = &neg
		}
		return le
	}
	ev := &Evidence{Cell: int(lab.Cells[i]), Index: i, Margin: d.Parameters.Margin, Minimum: d.Parameters.Minimum, State: d.Parameters.State}
	children := map[int][]int{}
	for k, s := range d.Sets {
		if !s.State {
			children[s.Parent] = append(children[s.Parent], k)
		}
	}
	candidates := children[-1]
	for l, name := range d.Levels {
		if len(candidates) == 0 {
			break
		}
		lv := LevelEvidence{Level: name, Label: lab.Label[l][i], Best: -1, Next: -1, Candidates: candidates}
		if v := lab.Lead[l][i]; !math.IsNaN(float64(v)) {
			lv.Lead = &v
		}
		for _, k := range candidates {
			s := lab.Score[k][i]
			lv.Scores = append(lv.Scores, s)
			switch {
			case lv.Best < 0 || s > lab.Score[lv.Best][i]:
				lv.Next, lv.Best = lv.Best, k
			case lv.Next < 0 || s > lab.Score[lv.Next][i]:
				lv.Next = k
			}
		}
		lv.Evidence = append(lv.Evidence, label(lv.Best))
		if lv.Next >= 0 {
			lv.Evidence = append(lv.Evidence, label(lv.Next))
		}
		ev.Levels = append(ev.Levels, lv)
		if lv.Label < 0 {
			break
		}
		candidates = children[int(lv.Label)]
	}
	if also >= 0 && also < len(d.Sets) {
		a := label(also)
		ev.Also = &a
	}
	s := 0
	for k, set := range d.Sets {
		if set.State {
			ev.States = append(ev.States, label(k))
			ev.Carries = append(ev.Carries, lab.State[s][i])
			s++
		}
	}
	return ev
}
