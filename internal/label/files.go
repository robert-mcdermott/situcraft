package label

// A label set's files, an artifact of kind "labels" (internal/artifacts):
//
//	labels.json   what was labeled and how: the clustering, the marker set by its checksum, every
//	              parameter, each label's markers and control genes, the cells labeled at each
//	              level and each cluster's cells by label
//	markers.json  the marker set, as the labels were made from it
//	labels.bin "SCLB"  the cells (indices in the segmentation's cells) and their clusters; each one's score for every
//	              label and state (and its positive and negative markers' parts), its label and
//	              lead at each level, its confidence, and the states it carries
//
// labels.bin has SituCraft's binary envelope (internal/segment: magic, version, length, zlib).

import (
	"crypto/sha256"
	"encoding/hex"
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

// Files of a label set, by role.
const (
	FileDescription = "labels.json"
	FileMarkers     = "markers.json"
	FileLabels      = "labels.bin"
)

// Files lists them in order; Roles names each one's role in a manifest.
var (
	Files = []string{FileDescription, FileMarkers, FileLabels}
	Roles = map[string]string{FileDescription: "description", FileMarkers: "markers", FileLabels: "labels"}
)

// none is a level not taken, in labels.bin.
const none = math.MaxUint32

// SetInfo is a label or state as labels.json describes it: its level (−1 for a state), its parent
// (an index among the sets, −1 for none), its markers and their controls, and the cells carrying
// it (as their deepest label, or the state).
type SetInfo struct {
	Name             string   `json:"name"`
	Level            int      `json:"level"`
	Parent           int      `json:"parent"`
	State            bool     `json:"state,omitempty"`
	Positive         []string `json:"positive"`
	PositiveControls []string `json:"positiveControls"`
	Negative         []string `json:"negative,omitempty"`
	NegativeControls []string `json:"negativeControls,omitempty"`
	Cells            int      `json:"cells"`
}

// Description is labels.json.
type Description struct {
	Format       string       `json:"format"`
	Version      int          `json:"version"`
	Method       string       `json:"method"`
	Segmentation cluster.Side `json:"segmentation"`
	Clustering   cluster.Side `json:"clustering"`
	Markers      struct {
		Name     string `json:"name"`
		Checksum string `json:"checksum"`
	} `json:"markers"`
	Parameters Params `json:"parameters"`
	Fixed      struct {
		Expression string  `json:"expression"`
		TargetSum  float64 `json:"targetSum"`
		Score      string  `json:"score"`
		Negative   string  `json:"negative"`
		Assignment string  `json:"assignment"`
	} `json:"fixed"`
	Levels []string  `json:"levels"`
	Sets   []SetInfo `json:"sets"`
	Cells  struct {
		Analyzed int `json:"analyzed"`
		// Labeled are the cells taking a label at each level; Unlabeled those taking none.
		Labeled   []int `json:"labeled"`
		Unlabeled int   `json:"unlabeled"`
	} `json:"cells"`
	Clusters int `json:"clusters"`
	// Composition[level][cluster] counts a cluster's cells by the label they carry at the level
	// (indices among the sets), the last count those without a label.
	Composition [][][]int `json:"composition"`
	// States[state][cluster] counts a cluster's cells carrying each state.
	States  [][]int            `json:"states"`
	Seconds map[string]float64 `json:"seconds"`
}

// Labels is labels.bin.
type Labels struct {
	Cells      []uint32
	Cluster    []uint32
	Score      [][]float32 // [set][cell]
	Positive   [][]float32
	Negative   [][]float32 // nil for a set without negative markers
	Label      [][]int32   // [level][cell], −1 for none
	Lead       [][]float32 // NaN where the level was not reached
	Confidence []float32
	State      [][]bool // [state][cell]
}

func names(genes []string, idx []int) []string {
	out := make([]string, 0, len(idx))
	for _, g := range idx {
		out = append(out, genes[g])
	}
	return out
}

// describe makes labels.json from a result.
func describe(seg, clu cluster.Side, r *Result) *Description {
	d := &Description{Format: "situcraft-labels", Version: 1, Method: "SituCraft labels from markers " + MethodVersion, Segmentation: seg, Clustering: clu, Parameters: r.Params, Levels: r.Set.Levels, Clusters: r.Clusters, Seconds: r.Seconds}
	d.Markers.Name, d.Markers.Checksum = r.Set.Name, r.Set.Checksum()
	d.Fixed.Expression = "the clustering's: each cell's counts scaled to the target total, then log1p"
	d.Fixed.TargetSum = r.Target
	d.Fixed.Score = "the markers' mean less their control genes' mean, the controls drawn from the markers' bins of mean expression (scanpy's score_genes)"
	d.Fixed.Negative = "a label's score is its positive markers' less its negative markers' where that is above 0"
	d.Fixed.Assignment = "from the top level down: the best of the label's children when it leads the next by the margin and scores at least the minimum"
	a := r.Assignment
	n := len(r.Cells)
	carried := make([]int, len(r.Sets))
	for i := 0; i < n; i++ {
		if k := a.Deepest(i); k >= 0 {
			carried[k]++
		} else {
			d.Cells.Unlabeled++
		}
	}
	si := 0
	for k, s := range r.Sets {
		info := SetInfo{Name: s.Name, Level: s.Level, Parent: s.Parent, State: s.State, Positive: names(r.Genes, s.Positive), PositiveControls: names(r.Genes, r.Lists.PositiveControls[k]),
			Negative: names(r.Genes, s.Negative), NegativeControls: names(r.Genes, r.Lists.NegativeControls[k]), Cells: carried[k]}
		if s.State {
			for _, v := range a.State[si] {
				if v {
					info.Cells++
				}
			}
			si++
		}
		d.Sets = append(d.Sets, info)
	}
	d.Cells.Analyzed = n
	d.Composition, d.States = composition(len(r.Sets), r.Clusters, r.Cluster, a.Label, a.State)
	d.Cells.Labeled = make([]int, a.Levels)
	for l := range a.Label {
		for _, k := range a.Label[l] {
			if k >= 0 {
				d.Cells.Labeled[l]++
			}
		}
	}
	return d
}

// composition counts each cluster's cells by the label they carry at each level, and by state.
func composition(sets, clusters int, cluster []uint32, label [][]int32, state [][]bool) ([][][]int, [][]int) {
	comp := make([][][]int, len(label))
	for l := range comp {
		comp[l] = make([][]int, clusters)
		for c := range comp[l] {
			comp[l][c] = make([]int, sets+1)
		}
	}
	for i, c := range cluster {
		if int(c) >= clusters {
			continue
		}
		carried := int32(-1) // a cell that takes no label at a level keeps the one above
		for l := range label {
			if label[l][i] >= 0 {
				carried = label[l][i]
			}
			if carried < 0 {
				comp[l][c][sets]++
			} else {
				comp[l][c][carried]++
			}
		}
	}
	st := make([][]int, len(state))
	for s := range state {
		st[s] = make([]int, clusters)
		for i, v := range state[s] {
			if v && int(cluster[i]) < clusters {
				st[s][cluster[i]]++
			}
		}
	}
	return comp, st
}

// Write writes a label set's files into dir.
func Write(dir string, seg, clu cluster.Side, r *Result) error {
	d := describe(seg, clu, r)
	raw, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	set, err := json.MarshalIndent(r.Set, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileMarkers), append(set, '\n'), 0o644); err != nil {
		return err
	}
	f32 := func(v []float64) []float32 {
		out := make([]float32, len(v))
		for i, x := range v {
			out[i] = float32(x)
		}
		return out
	}
	a, sc := r.Assignment, r.Scores
	var b segment.Buffer
	b.U32(uint32(len(r.Cells)))
	b.U32s(r.Cells)
	b.U32s(r.Cluster)
	b.U32(uint32(len(r.Sets)))
	b.U32(uint32(a.Levels))
	b.U32(uint32(len(a.State)))
	for k := range r.Sets {
		b.F32s(f32(sc.Score[k]))
	}
	for k := range r.Sets {
		b.F32s(f32(sc.Positive[k]))
	}
	for k, s := range r.Sets {
		if len(s.Negative) > 0 {
			b.F32s(f32(sc.Negative[k]))
		}
	}
	for l := 0; l < a.Levels; l++ {
		for _, k := range a.Label[l] {
			if k < 0 {
				b.U32(none)
			} else {
				b.U32(uint32(k))
			}
		}
	}
	for l := 0; l < a.Levels; l++ {
		b.F32s(f32(a.Lead[l]))
	}
	b.F32s(f32(a.Confidence))
	for _, has := range a.State {
		for _, v := range has {
			if v {
				b.Byte(1)
			} else {
				b.Byte(0)
			}
		}
	}
	return segment.WriteBinary(filepath.Join(dir, FileLabels), "SCLB", &b)
}

// ReadDescription reads labels.json.
func ReadDescription(dir string) (*Description, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileDescription))
	if err != nil {
		return nil, err
	}
	var d Description
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", FileDescription, err)
	}
	if d.Format != "situcraft-labels" || d.Version > 1 {
		return nil, fmt.Errorf("%s is not a label set SituCraft reads (%s, version %d)", FileDescription, d.Format, d.Version)
	}
	return &d, nil
}

// ReadMarkers reads markers.json.
func ReadMarkers(dir string) (*MarkerSet, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileMarkers))
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// ReadLabels reads labels.bin; negative says which sets have negative markers.
func ReadLabels(dir string, negative []bool) (*Labels, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileLabels), "SCLB")
	if err != nil {
		return nil, err
	}
	n := r.Count(8)
	out := &Labels{Cells: r.U32s(n), Cluster: r.U32s(n)}
	S := r.Count(8 * max(n, 1))
	levels := r.Count(8 * max(n, 1))
	states := r.Count(max(n, 1))
	if S != len(negative) {
		return nil, fmt.Errorf("%s holds %d labels and states; the description %d", FileLabels, S, len(negative))
	}
	for k := 0; k < S; k++ {
		out.Score = append(out.Score, r.F32s(n))
	}
	for k := 0; k < S; k++ {
		out.Positive = append(out.Positive, r.F32s(n))
	}
	out.Negative = make([][]float32, S)
	for k := 0; k < S; k++ {
		if negative[k] {
			out.Negative[k] = r.F32s(n)
		}
	}
	for l := 0; l < levels; l++ {
		lab := make([]int32, n)
		for i, k := range r.U32s(n) {
			if k == none {
				lab[i] = -1
			} else {
				lab[i] = int32(k)
			}
		}
		out.Label = append(out.Label, lab)
	}
	for l := 0; l < levels; l++ {
		out.Lead = append(out.Lead, r.F32s(n))
	}
	out.Confidence = r.F32s(n)
	for s := 0; s < states; s++ {
		has := make([]bool, n)
		for i := range has {
			has[i] = r.Byte() == 1
		}
		out.State = append(out.State, has)
	}
	return out, r.Done(FileLabels)
}

// Read reads a label set's description, marker set and labels.
func Read(dir string) (*Description, *MarkerSet, *Labels, error) {
	d, err := ReadDescription(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	set, err := ReadMarkers(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	negative := make([]bool, len(d.Sets))
	for k, s := range d.Sets {
		negative[k] = len(s.Negative) > 0
	}
	l, err := ReadLabels(dir, negative)
	if err != nil {
		return nil, nil, nil, err
	}
	return d, set, l, nil
}

// Digest is the SHA-256 of a label set's content: its description without the run's timings, its
// marker set's checksum and its labels. The same inputs give the same digest on any computer.
func Digest(dir string) (string, error) {
	d, err := ReadDescription(dir)
	if err != nil {
		return "", err
	}
	d.Seconds = nil
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	set, err := ReadMarkers(dir)
	if err != nil {
		return "", err
	}
	bin, err := os.ReadFile(filepath.Join(dir, FileLabels))
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(raw)
	h.Write(set.Canonical())
	h.Write(bin)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Summary is what the check reports of a label set (the artifact's summary).
type Summary struct {
	Segmentation cluster.Side `json:"segmentation"`
	Clustering   cluster.Side `json:"clustering"`
	Markers      string       `json:"markers"`
	Checksum     string       `json:"checksum"`
	Cells        int          `json:"cells"`
	Levels       []string     `json:"levels"`
	Labeled      []int        `json:"labeled"`
	Unlabeled    int          `json:"unlabeled"`
	Labels       int          `json:"labels"`
	States       int          `json:"states"`
	Margin       float64      `json:"margin"`
}

// Check examines a staged label set: its files read and agree; the marker set is the one the
// description names by checksum, resolved to the same labels and markers; every cell's label at
// each level is the best of its candidates by its scores, taken only when it leads by the margin
// and scores the minimum, and left where it does not; the leads, confidences and states follow
// from the scores; every score is a finite number; the composition counts the cells.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	d, set, lab, err := Read(dir)
	if err != nil {
		fatal("labels.unreadable", "%v", err)
		return out
	}
	if set.Checksum() != d.Markers.Checksum {
		fatal("labels.markers", "markers.json's checksum %.16s is not the description's %.16s.", set.Checksum(), d.Markers.Checksum)
		return out
	}
	var all []string
	for _, s := range d.Sets {
		all = append(all, s.Positive...)
		all = append(all, s.Negative...)
	}
	sets, err := set.Resolve(uniq(all))
	if err != nil || len(sets) != len(d.Sets) || len(set.Levels) != len(d.Levels) {
		fatal("labels.markers", "The marker set does not resolve to the description's %d labels and states.", len(d.Sets))
		return out
	}
	genes := uniq(all)
	for k, s := range sets {
		info := d.Sets[k]
		if s.Name != info.Name || s.Level != info.Level || s.Parent != info.Parent || s.State != info.State ||
			!slices.Equal(names(genes, s.Positive), info.Positive) || !slices.Equal(names(genes, s.Negative), info.Negative) {
			fatal("labels.markers", "Label %q is not as the marker set defines it.", info.Name)
			return out
		}
		if len(info.PositiveControls) == 0 || (len(info.Negative) > 0) != (len(info.NegativeControls) > 0) {
			fatal("labels.markers", "Label %q's markers lack their control genes.", info.Name)
			return out
		}
	}
	n := len(lab.Cells)
	nStates := 0
	for _, s := range sets {
		if s.State {
			nStates++
		}
	}
	if n != d.Cells.Analyzed || len(lab.Label) != len(d.Levels) || len(lab.State) != nStates || len(d.Composition) != len(d.Levels) || len(d.States) != nStates {
		fatal("labels.sizes", "The files disagree in size: %d cells (the description %d), %d levels (%d), %d states (%d).", n, d.Cells.Analyzed, len(lab.Label), len(d.Levels), len(lab.State), nStates)
		return out
	}
	for i, c := range lab.Cells {
		if i > 0 && c <= lab.Cells[i-1] {
			fatal("labels.cells", "The cells are not in the segmentation's order (cell %d).", i)
			return out
		}
	}
	for k := range sets {
		for i := 0; i < n; i++ {
			if v := lab.Score[k][i]; math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				fatal("labels.values", "A score of %q is not a finite number.", sets[k].Name)
				return out
			}
		}
	}
	p := d.Parameters
	margin, minimum, state := float32(p.Margin), float32(p.Minimum), float32(p.State)
	children := make([][]int, len(sets)+1)
	for k, s := range sets {
		if s.State {
			continue
		}
		if s.Parent < 0 {
			children[len(sets)] = append(children[len(sets)], k)
		} else {
			children[s.Parent] = append(children[s.Parent], k)
		}
	}
	for i := 0; i < n; i++ {
		candidates := children[len(sets)]
		reached := true
		var conf float32
		for l := range d.Levels {
			k, lead := lab.Label[l][i], lab.Lead[l][i]
			if !reached || len(candidates) == 0 {
				if k >= 0 || !math.IsNaN(float64(lead)) {
					fatal("labels.assignment", "Cell %d has a label or lead at level %d, which it did not reach.", i, l+1)
					return out
				}
				reached = false
				continue
			}
			best, next := -1, float32(math.Inf(-1))
			for _, c := range candidates {
				s := lab.Score[c][i]
				switch {
				case best < 0 || s > lab.Score[best][i]:
					if best >= 0 {
						next = lab.Score[best][i]
					}
					best = c
				case s > next:
					next = s
				}
			}
			if len(candidates) == 1 {
				next = minimum
			}
			top := lab.Score[best][i]
			if want := top - next; math.IsNaN(float64(lead)) || math.Abs(float64(lead-want)) > 1e-5*(1+math.Abs(float64(want))) {
				fatal("labels.assignment", "Cell %d's lead at level %d is %v; its scores give %v.", i, l+1, lead, want)
				return out
			}
			if l == 0 {
				conf = lead
			}
			if k < 0 {
				if lead > margin && top > minimum {
					fatal("labels.assignment", "Cell %d stops above level %d though its best label leads by %v.", i, l+1, lead)
					return out
				}
				reached = false
				continue
			}
			if !slices.Contains(candidates, int(k)) || lab.Score[k][i] < lab.Score[best][i] || lead < margin || top < minimum {
				fatal("labels.assignment", "Cell %d's label at level %d is not its best candidate by the margin.", i, l+1)
				return out
			}
			conf = lead
			candidates = children[k]
		}
		if c := lab.Confidence[i]; c != conf {
			fatal("labels.confidence", "Cell %d's confidence %v is not the lead of the label it carries (%v).", i, c, conf)
			return out
		}
	}
	// Rounding to float32 keeps order, so a score at least the threshold stays at least it, and one
	// below it may round to it.
	si := 0
	for k, s := range sets {
		if !s.State {
			continue
		}
		for i := 0; i < n; i++ {
			carried := s.Parent < 0 || (sets[s.Parent].Level < len(lab.Label) && lab.Label[sets[s.Parent].Level][i] == int32(s.Parent))
			v := lab.Score[k][i]
			if lab.State[si][i] && !(carried && v >= state) || !lab.State[si][i] && carried && v > state {
				fatal("labels.states", "Cell %d's state %q does not follow from its score.", i, s.Name)
				return out
			}
		}
		si++
	}
	for _, c := range lab.Cluster {
		if int(c) >= d.Clusters {
			fatal("labels.composition", "A cell is in cluster %d of %d.", c, d.Clusters)
			return out
		}
	}
	comp, st := composition(len(sets), d.Clusters, lab.Cluster, lab.Label, lab.State)
	if !equal3(comp, d.Composition) || !equal2(st, d.States) {
		fatal("labels.composition", "The clusters' composition does not count the cells' labels.")
	}
	labeled := make([]int, len(d.Levels))
	unlabeled := 0
	for l := range lab.Label {
		for _, k := range lab.Label[l] {
			if k >= 0 {
				labeled[l]++
			}
		}
	}
	for i := 0; i < n; i++ {
		if lab.Label[0][i] < 0 {
			unlabeled++
		}
	}
	if !slices.Equal(labeled, d.Cells.Labeled) || unlabeled != d.Cells.Unlabeled {
		fatal("labels.sizes", "The cells labeled at each level %v (%d without) are not the description's %v (%d).", labeled, unlabeled, d.Cells.Labeled, d.Cells.Unlabeled)
	}
	labels := 0
	for _, s := range sets {
		if !s.State {
			labels++
		}
	}
	s := Summary{Segmentation: d.Segmentation, Clustering: d.Clustering, Markers: d.Markers.Name, Checksum: d.Markers.Checksum, Cells: n, Levels: d.Levels,
		Labeled: d.Cells.Labeled, Unlabeled: d.Cells.Unlabeled, Labels: labels, States: nStates, Margin: p.Margin}
	m.Summary, _ = json.Marshal(s)
	return out
}

func uniq(v []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range v {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func equal2(a, b [][]int) bool {
	return slices.EqualFunc(a, b, func(x, y []int) bool { return slices.Equal(x, y) })
}

func equal3(a, b [][][]int) bool {
	return slices.EqualFunc(a, b, equal2)
}
