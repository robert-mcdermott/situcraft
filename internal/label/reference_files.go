package label

// A label set from a reference is an artifact of kind "labels" (files.go) whose labels.json
// describes the reference in place of a marker set, with reference.json in place of markers.json:
//
//	reference.json  the reference as it was compared: its labels, each one's reference cells, the
//	                panel genes it shares and each label's profile over them
//
// labels.bin holds each cell's first-round correlation with every label as its score, its last
// round's correlations as its positive part (NaN for the labels not in it), its label and lead
// at the one level, and its lead as its confidence.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/segment"
)

// FileReference is a reference label set's profiles.
const FileReference = "reference.json"

// MethodRef is the version of the mapping, which changes when its results would.
const MethodRef = "1"

// RefInfo is the reference as labels.json describes it, with the mapping's parameters, the genes
// per pair, the panel genes it shares, the first round's genes, and the cells fine-tuned (scored
// more than once) and changed by it (labeled other than their first round's best).
type RefInfo struct {
	Ref
	Parameters RefParams `json:"parameters"`
	PerPair    int       `json:"perPair"`
	Shared     []string  `json:"shared"`
	Compared   []string  `json:"compared"`
	FineTuned  int       `json:"fineTuned"`
	Changed    int       `json:"changed"`
}

// RefStored is reference.json.
type RefStored struct {
	Format   string      `json:"format"`
	Version  int         `json:"version"`
	Labels   []string    `json:"labels"`
	Cells    []int       `json:"cells"`
	Genes    []string    `json:"genes"`
	Profiles [][]float64 `json:"profiles"` // [label][gene]
}

// Restore makes the reference as it was compared from reference.json and the panel's genes.
func (rp *RefStored) Restore(info *RefInfo, panel []string) (*Ref, error) {
	index := map[string]int{}
	for g, name := range panel {
		index[name] = g
	}
	ref := info.Ref
	ref.Labels, ref.Cells, ref.Profiles, ref.Genes = rp.Labels, rp.Cells, rp.Profiles, nil
	for _, name := range rp.Genes {
		g, ok := index[name]
		if !ok {
			return nil, fmt.Errorf("the reference's gene %s is not the panel's", name)
		}
		ref.Genes = append(ref.Genes, g)
	}
	return &ref, nil
}

// describeRef makes labels.json for a mapping.
func describeRef(seg, clu cluster.Side, r *RefResult) *Description {
	ref := r.Ref
	info := &RefInfo{Ref: *ref, Parameters: r.Params, PerPair: r.Per, Shared: names(r.Genes, ref.Genes)}
	for _, k := range r.Compared {
		info.Compared = append(info.Compared, r.Genes[ref.Genes[k]])
	}
	n := len(r.Cells)
	for i := 0; i < n; i++ {
		if r.Rounds[i] > 1 {
			info.FineTuned++
		}
		if best := firstBest(r.First, i); r.Label[i] >= 0 && best != int(r.Label[i]) {
			info.Changed++
		}
	}
	level := ref.LabelColumn
	if level == "" {
		level = "Reference label"
	}
	d := &Description{Format: "situcraft-labels", Version: 1, Method: "SituCraft labels from a reference " + MethodRef, Segmentation: seg, Clustering: &clu, Reference: info,
		Levels: []string{level}, Clusters: r.Clusters, Seconds: r.Seconds}
	d.Fixed.Expression = "the clustering's: each cell's counts scaled to the target total, then log1p"
	d.Fixed.TargetSum = r.Target
	d.Fixed.Score = "the Spearman correlation of the cell's values and the label's profile over the genes compared: for each ordered pair of labels, those most higher in the first's profile (Aran et al. 2019)"
	d.Fixed.Assignment = "the labels within the fine-tuning threshold of the best scored again on their own genes until one is left or none drops; the last round's best taken when it leads the next by the margin"
	carried := make([]int, len(ref.Labels))
	for _, k := range r.Label {
		if k >= 0 {
			carried[k]++
		}
	}
	for k, name := range ref.Labels {
		d.Sets = append(d.Sets, SetInfo{Name: name, Level: 0, Parent: -1, Cells: carried[k]})
	}
	d.Cells.Analyzed = n
	d.Cells.Labeled = []int{0}
	for _, k := range r.Label {
		if k >= 0 {
			d.Cells.Labeled[0]++
		}
	}
	d.Cells.Unlabeled = n - d.Cells.Labeled[0]
	d.Composition, d.States = composition(len(ref.Labels), r.Clusters, r.Cluster, [][]int32{r.Label}, nil)
	return d
}

// firstBest is a cell's best label in the first round (−1 when it has none).
func firstBest[T float32 | float64](first [][]T, i int) int {
	best := -1
	for k := range first {
		if v := first[k][i]; !math.IsNaN(float64(v)) && (best < 0 || v > first[best][i]) {
			best = k
		}
	}
	return best
}

// WriteReference writes a mapping's files into dir.
func WriteReference(dir string, seg, clu cluster.Side, r *RefResult) error {
	d := describeRef(seg, clu, r)
	raw, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	rp := RefStored{Format: "situcraft-reference-profiles", Version: 1, Labels: r.Ref.Labels, Cells: r.Ref.Cells, Genes: d.Reference.Shared, Profiles: r.Ref.Profiles}
	if raw, err = json.Marshal(rp); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileReference), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	f32 := func(v []float64) []float32 {
		out := make([]float32, len(v))
		for i, x := range v {
			out[i] = float32(x)
		}
		return out
	}
	L := len(r.Ref.Labels)
	var b segment.Buffer
	b.U32(uint32(len(r.Cells)))
	b.U32s(r.Cells)
	b.U32s(r.Cluster)
	b.U32(uint32(L))
	b.U32(1)
	b.U32(0)
	for k := 0; k < L; k++ {
		b.F32s(f32(r.First[k]))
	}
	for k := 0; k < L; k++ {
		b.F32s(f32(r.Last[k]))
	}
	for _, k := range r.Label {
		if k < 0 {
			b.U32(none)
		} else {
			b.U32(uint32(k))
		}
	}
	lead := f32(r.Lead)
	b.F32s(lead)
	b.F32s(lead)
	return segment.WriteBinary(filepath.Join(dir, FileLabels), "SCLB", &b)
}

// ReadReferenceProfiles reads reference.json.
func ReadReferenceProfiles(dir string) (*RefStored, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileReference))
	if err != nil {
		return nil, err
	}
	var rp RefStored
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, fmt.Errorf("%s: %w", FileReference, err)
	}
	if rp.Format != "situcraft-reference-profiles" || rp.Version > 1 {
		return nil, fmt.Errorf("%s is not a reference SituCraft reads", FileReference)
	}
	return &rp, nil
}

// checkReference examines a staged label set from a reference: reference.json gives the
// description's labels and shared genes, and its pairs' genes the first round's; one level, no
// state; every cell's first-round scores correlations or none; a cell's label within the
// fine-tuning threshold of its first round's best and the best of its last round, taken by the
// margin and left below it; its lead its confidence; the composition counts the cells.
func checkReference(dir string, d *Description, lab *Labels, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	info := d.Reference
	rp, err := ReadReferenceProfiles(dir)
	if err != nil {
		fatal("labels.reference", "%v", err)
		return out
	}
	var setNames []string
	for _, s := range d.Sets {
		setNames = append(setNames, s.Name)
	}
	if !slices.Equal(rp.Labels, setNames) || !slices.Equal(rp.Labels, info.Labels) || !slices.Equal(rp.Genes, info.Shared) || len(rp.Profiles) != len(rp.Labels) || len(rp.Cells) != len(rp.Labels) {
		fatal("labels.reference", "reference.json's %d labels over %d genes are not the description's.", len(rp.Labels), len(rp.Genes))
		return out
	}
	for k, p := range rp.Profiles {
		if len(p) != len(rp.Genes) {
			fatal("labels.reference", "Label %q's profile has %d values for %d genes.", rp.Labels[k], len(p), len(rp.Genes))
			return out
		}
		for _, v := range p {
			if !(v >= 0) || math.IsInf(v, 0) {
				fatal("labels.reference", "Label %q's profile has a value that is not a number of 0 or more.", rp.Labels[k])
				return out
			}
		}
	}
	if err := info.Parameters.Check(); err != nil || info.PerPair != info.Parameters.MarkersPer(len(rp.Labels)) {
		fatal("labels.parameters", "The parameters are not a mapping's (%v; %d genes per pair).", err, info.PerPair)
		return out
	}
	ref, _ := rp.Restore(info, rp.Genes)
	pairs := ref.pairMarkers(info.PerPair)
	all := make([]int, len(rp.Labels))
	for k := range all {
		all[k] = k
	}
	var compared []string
	for _, g := range ref.compare(all, pairs).genes {
		compared = append(compared, rp.Genes[g])
	}
	if !slices.Equal(compared, info.Compared) {
		fatal("labels.reference", "The reference's pairs give %d genes compared; the description %d.", len(compared), len(info.Compared))
		return out
	}
	n, L := len(lab.Cells), len(rp.Labels)
	if n != d.Cells.Analyzed || len(d.Levels) != 1 || len(lab.Label) != 1 || len(lab.State) != 0 || len(lab.Score) != L {
		fatal("labels.sizes", "The files disagree in size: %d cells (the description %d), %d levels, %d states, %d labels scored of %d.", n, d.Cells.Analyzed, len(lab.Label), len(lab.State), len(lab.Score), L)
		return out
	}
	for i, c := range lab.Cells {
		if i > 0 && c <= lab.Cells[i-1] {
			fatal("labels.cells", "The cells are not in the segmentation's order (cell %d).", i)
			return out
		}
	}
	tune, margin := float32(info.Parameters.Tune), float32(info.Parameters.Margin)
	const eps = 1e-6 // float32's rounding of correlations and their differences
	fineTuned, changed := 0, 0
	for i := 0; i < n; i++ {
		k, lead := lab.Label[0][i], lab.Lead[0][i]
		scored, lastScored := 0, 0
		for l := 0; l < L; l++ {
			for _, v := range []float32{lab.Score[l][i], lab.Positive[l][i]} {
				if !math.IsNaN(float64(v)) && !(v >= -1-eps && v <= 1+eps) {
					fatal("labels.values", "Cell %d's correlation with %q is %v.", i, rp.Labels[l], v)
					return out
				}
			}
			if !math.IsNaN(float64(lab.Score[l][i])) {
				scored++
			}
			if !math.IsNaN(float64(lab.Positive[l][i])) {
				lastScored++
			}
		}
		if lastScored > 0 {
			fineTuned++
		}
		if c := lab.Confidence[i]; c != lead && !(math.IsNaN(float64(c)) && math.IsNaN(float64(lead))) {
			fatal("labels.confidence", "Cell %d's confidence %v is not its lead %v.", i, c, lead)
			return out
		}
		best := firstBest(lab.Score, i)
		if best < 0 {
			if k >= 0 || lastScored > 0 || !math.IsNaN(float64(lead)) {
				fatal("labels.assignment", "Cell %d has no correlation but a label, a lead or a later round.", i)
				return out
			}
			continue
		}
		// The round that decided: the last when there were more, else the first.
		round := lab.Score
		if lastScored > 0 {
			round = lab.Positive
		}
		top, next, rb := float32(math.Inf(-1)), float32(math.Inf(-1)), -1
		for l := 0; l < L; l++ {
			v := round[l][i]
			switch {
			case math.IsNaN(float64(v)):
			case rb < 0 || v > top:
				rb, top, next = l, v, top
			case v > next:
				next = v
			}
		}
		if lastScored == 1 || scored == 1 || math.Abs(float64(lead-(top-next))) > eps {
			fatal("labels.assignment", "Cell %d's lead %v is not its deciding round's best less the next (%v).", i, lead, top-next)
			return out
		}
		if k < 0 {
			if lead >= margin && lead > 0 {
				fatal("labels.assignment", "Cell %d has no label though its best leads by %v.", i, lead)
				return out
			}
			continue
		}
		if round[k][i] < top || lead < margin || !(lead > 0) || lab.Score[k][i] < lab.Score[best][i]-tune-eps {
			fatal("labels.assignment", "Cell %d's label %q is not its best by the margin, within the fine-tuning threshold of its first round's best.", i, rp.Labels[k])
			return out
		}
		if lastScored > 0 {
			for l := 0; l < L; l++ {
				if !math.IsNaN(float64(lab.Positive[l][i])) && lab.Score[l][i] < lab.Score[best][i]-tune-eps {
					fatal("labels.assignment", "Cell %d's last round compares %q, beyond the fine-tuning threshold.", i, rp.Labels[l])
					return out
				}
			}
		}
		if int(k) != best {
			changed++
		}
	}
	if fineTuned != info.FineTuned || changed != info.Changed {
		fatal("labels.sizes", "%d cells fine-tuned and %d changed by it; the description counts %d and %d.", fineTuned, changed, info.FineTuned, info.Changed)
		return out
	}
	for _, c := range lab.Cluster {
		if int(c) >= d.Clusters {
			fatal("labels.composition", "A cell is in cluster %d of %d.", c, d.Clusters)
			return out
		}
	}
	comp, _ := composition(L, d.Clusters, lab.Cluster, lab.Label, nil)
	if !equal3(comp, d.Composition) || len(d.States) != 0 {
		fatal("labels.composition", "The clusters' composition does not count the cells' labels.")
	}
	counts := make([]int, L)
	labeled := 0
	for _, k := range lab.Label[0] {
		if k >= 0 {
			counts[k]++
			labeled++
		}
	}
	for k, s := range d.Sets {
		if s.Cells != counts[k] || s.Level != 0 || s.Parent != -1 || s.State {
			fatal("labels.sets", "Label %q is not as the cells carry it (%d cells, the description %d).", s.Name, counts[k], s.Cells)
			return out
		}
	}
	if labeled != d.Cells.Labeled[0] || n-labeled != d.Cells.Unlabeled {
		fatal("labels.sizes", "%d cells labeled (%d without); the description counts %d (%d).", labeled, n-labeled, d.Cells.Labeled[0], d.Cells.Unlabeled)
	}
	s := Summary{Segmentation: d.Segmentation, Clustering: d.Clustering, Reference: &RefSummary{Format: info.Format, File: info.File, LabelColumn: info.LabelColumn, Labels: L, Shared: len(info.Shared), Missing: len(info.Missing)},
		Cells: n, Levels: d.Levels, Labeled: d.Cells.Labeled, Unlabeled: d.Cells.Unlabeled, Labels: L, Margin: info.Parameters.Margin}
	m.Summary, _ = json.Marshal(s)
	return out
}

// RefSummary is what the artifact's summary says of a reference.
type RefSummary struct {
	Format      string `json:"format"`
	File        string `json:"file"`
	LabelColumn string `json:"labelColumn,omitempty"`
	Labels      int    `json:"labels"`
	Shared      int    `json:"shared"`
	Missing     int    `json:"missing"`
}

// RefLabelScore is a label's correlation with a cell in a round.
type RefLabelScore struct {
	Label int     `json:"label"`
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// RefRound is a round of a cell's mapping: the genes compared and the labels' correlations, the
// best first.
type RefRound struct {
	Genes  int             `json:"genes"`
	Scores []RefLabelScore `json:"scores"`
}

// RefGene is a gene of the deciding round in a cell: its count and value there, and its value in
// the best label's profile and the next's.
type RefGene struct {
	Gene  string  `json:"gene"`
	Count uint32  `json:"count"`
	Value float64 `json:"value"`
	Best  float64 `json:"best"`
	Next  float64 `json:"next"`
}

// RefEvidence is why a cell took its label from a reference: each round; the deciding round's best
// label and the next, with the genes most higher in each one's profile than the other's; and a
// label asked about (its first-round correlation and rank).
type RefEvidence struct {
	Cell      int            `json:"cell"`
	Index     int            `json:"index"`
	Label     int            `json:"label"`
	Lead      *float64       `json:"lead"`
	Rounds    []RefRound     `json:"rounds"`
	Best      int            `json:"best"`
	Next      int            `json:"next"`
	ForBest   []RefGene      `json:"forBest"`
	ForNext   []RefGene      `json:"forNext"`
	Also      *RefLabelScore `json:"also,omitempty"`
	AlsoRank  int            `json:"alsoRank,omitempty"`
	Recorded  []float32      `json:"recorded"` // the first round as labels.bin holds it
	Reference string         `json:"reference"`
}

// CellRefEvidence maps cell i (its index among the clustering's cells) again from reference.json,
// as it was labeled; also, a label to say where it stands (−1 for none).
func CellRefEvidence(e *cluster.Expression, d *Description, rp *RefStored, lab *Labels, i, also int) (*RefEvidence, error) {
	ref, err := rp.Restore(d.Reference, e.Genes)
	if err != nil {
		return nil, err
	}
	mp, err := NewMapper(ref, len(e.Genes), d.Reference.Parameters)
	if err != nil {
		return nil, err
	}
	genes, counts, values := e.Row(i)
	m := mp.Map(genes, values)
	out := &RefEvidence{Cell: int(e.Cells[i]), Index: i, Label: m.Label, Best: -1, Next: -1, Reference: d.Reference.File}
	for k := range lab.Score {
		out.Recorded = append(out.Recorded, lab.Score[k][i])
	}
	if !math.IsNaN(m.Lead) {
		out.Lead = &m.Lead
	}
	for _, r := range m.Rounds {
		rr := RefRound{Genes: len(r.Genes)}
		for j, l := range r.Labels {
			rr.Scores = append(rr.Scores, RefLabelScore{Label: l, Name: ref.Labels[l], Score: r.Scores[j]})
		}
		slices.SortStableFunc(rr.Scores, func(a, b RefLabelScore) int {
			switch {
			case math.IsNaN(a.Score) || a.Score < b.Score:
				return 1
			case math.IsNaN(b.Score) || a.Score > b.Score:
				return -1
			}
			return 0
		})
		out.Rounds = append(out.Rounds, rr)
	}
	if len(m.Rounds) == 0 {
		return out, nil
	}
	last := out.Rounds[len(out.Rounds)-1]
	out.Best, out.Next = last.Scores[0].Label, last.Scores[1].Label
	if also >= 0 && also < len(ref.Labels) {
		for j, s := range out.Rounds[0].Scores {
			if s.Label == also {
				out.Also, out.AlsoRank = &out.Rounds[0].Scores[j], j+1
			}
		}
	}
	// The deciding round's genes, by how much higher each is in one profile than the other.
	at := map[int]int{}
	for k, g := range genes {
		at[g] = k
	}
	deciding := m.Rounds[len(m.Rounds)-1].Genes
	gene := func(s int) RefGene {
		g := ref.Genes[s]
		rg := RefGene{Gene: e.Genes[g], Best: ref.Profiles[out.Best][s], Next: ref.Profiles[out.Next][s]}
		if k, ok := at[g]; ok {
			rg.Count, rg.Value = counts[k], values[k]
		}
		return rg
	}
	order := append([]int(nil), deciding...)
	diff := func(s int) float64 { return ref.Profiles[out.Best][s] - ref.Profiles[out.Next][s] }
	slices.SortStableFunc(order, func(a, b int) int {
		switch da, db := diff(a), diff(b); {
		case da > db:
			return -1
		case da < db:
			return 1
		}
		return 0
	})
	const shown = 8
	for _, s := range order {
		if len(out.ForBest) < shown && diff(s) > 0 {
			out.ForBest = append(out.ForBest, gene(s))
		}
	}
	for j := len(order) - 1; j >= 0; j-- {
		if s := order[j]; len(out.ForNext) < shown && diff(s) < 0 {
			out.ForNext = append(out.ForNext, gene(s))
		}
	}
	return out, nil
}
