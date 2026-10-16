package compare

// What the choice of a segmentation changes downstream, read through a comparison's differences:
// a label set of each segmentation's cells (from markers or a reference made on each, imported,
// curated or consensus), compared where the comparison matches cells. Labels are compared by name.
//
//   - Composition: each label's cells under each segmentation.
//   - Matched cells (one-to-one, expanded, contracted, assignment-only; covered by both sets): the
//     share labeled alike, and every pair of labels (A's, B's).
//   - Split and merged cells: whether the pieces keep the label of the cell split, or the cells
//     merged the label of the cell they became; and whether they carry two labels or more (a cell
//     of two kinds, or two cells of different kinds joined).
//   - Marker mixing by label: of each label's cells, the share expressing a pair of mutually
//     exclusive markers together, and the mutually exclusive co-expression rate among them (Hartman
//     and Satija 2024; coexpression.go).
//   - Segmentation-sensitive cells: a cell whose counterparts under the other segmentation carry
//     another label (or none), with a line saying what they carry.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"situcraft/internal/segment"
)

// SideLabels are one segmentation's labels: each cell's label (an index into Names, −1 for none)
// and whether the label set covers it (a cell it leaves out, not analyzed, is not compared).
type SideLabels struct {
	Names   []string
	Label   []int32
	Covered []bool
}

func (s *SideLabels) name(c int32) string {
	if l := s.Label[c]; l >= 0 {
		return s.Names[l]
	}
	return ""
}

// LabelCount is a label's cells under each segmentation ("" names no label).
type LabelCount struct {
	Name string `json:"name"`
	A    int    `json:"a"`
	B    int    `json:"b"`
}

// Transition counts matched cells labeled A under A and B under B ("" for no label).
type Transition struct {
	A     string `json:"a"`
	B     string `json:"b"`
	Cells int    `json:"cells"`
}

// Matched are the cells the comparison matches one to one, covered by both label sets: those
// labeled under both, labeled alike, and labeled under one only; the same by class; and every pair
// of labels, the most cells first.
type Matched struct {
	Cells       int               `json:"cells"`
	Labeled     int               `json:"labeled"`
	Same        int               `json:"same"`
	Agreement   Float             `json:"agreement"`
	LabeledOne  int               `json:"labeledOne"`
	ByClass     map[string][2]int `json:"byClass"` // class → {labeled under both, alike}
	Transitions []Transition      `json:"transitions"`
}

// Fates are the differences of a class whose cell (the one split, the one merged into) is labeled
// and some of whose counterparts are covered: every counterpart carrying its label, some, none;
// and those whose counterparts carry two labels or more.
type Fates struct {
	Groups  int `json:"groups"`
	Kept    int `json:"kept"`
	Partly  int `json:"partly"`
	Changed int `json:"changed"`
	Mixed   int `json:"mixed"`
}

// MixingLabel is marker mixing among a label's cells: its cells, those expressing a pair of
// mutually exclusive markers together, their share, and the MECR among the cells.
type MixingLabel struct {
	Name  string `json:"name"`
	Cells int    `json:"cells"`
	Mixed int    `json:"mixed"`
	Share Float  `json:"share"`
	MECR  Float  `json:"mecr"`
}

// Mixing is marker mixing by label under each segmentation, over the labeled cells (All) and each
// label's; Pairs are the exclusive marker pairs both panels have.
type Mixing struct {
	Pairs int           `json:"pairs"`
	AllA  MixingLabel   `json:"allA"`
	AllB  MixingLabel   `json:"allB"`
	A     []MixingLabel `json:"a"`
	B     []MixingLabel `json:"b"`
}

// SideCells count a segmentation's cells, those its label set covers and those it labels.
type SideCells struct {
	Cells   int `json:"cells"`
	Covered int `json:"covered"`
	Labeled int `json:"labeled"`
}

// Impact is what the choice of segmentation changes in the cells' labels.
type Impact struct {
	CellsA      SideCells    `json:"cellsA"`
	CellsB      SideCells    `json:"cellsB"`
	Composition []LabelCount `json:"composition"`
	Matched     Matched      `json:"matched"`
	Split       Fates        `json:"split"`
	Merged      Fates        `json:"merged"`
	Rearranged  Fates        `json:"rearranged"`
	// OnlyA and OnlyB count, by label, the covered cells without a counterpart.
	OnlyA []LabelCount `json:"onlyA"`
	OnlyB []LabelCount `json:"onlyB"`
	// Changed are the differences whose cells' labels change (indices among the differences), the
	// most transcripts reassigned first.
	Changed []int   `json:"changed"`
	Mixing  *Mixing `json:"mixing"`
	// SensitiveA and SensitiveB give, for each cell, what its counterparts carry when that is not
	// its own label (an index into Others), or −1.
	SensitiveA []int32  `json:"-"`
	SensitiveB []int32  `json:"-"`
	Others     []string `json:"others"`
	// Sensitive count the cells of each so flagged (labeled ones).
	Sensitive [2]int `json:"sensitive"`
}

// labelOf words a label for a line: its name, or "no label".
func labelOf(name string) string {
	if name == "" {
		return "no label"
	}
	return name
}

// tally words the labels of cells: each distinct one, the most frequent first (then by name).
func tally(names []string) string {
	count := map[string]int{}
	for _, n := range names {
		count[n]++
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if count[keys[a]] != count[keys[b]] {
			return count[keys[a]] > count[keys[b]]
		}
		return keys[a] < keys[b]
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = labelOf(k)
		if count[k] > 1 {
			parts[i] += fmt.Sprintf(" (%d)", count[k])
		}
	}
	return strings.Join(parts, ", ")
}

// CompareLabels reads two label sets through a comparison's differences: groups (the comparison's,
// in order), each side's labels, and for marker mixing each side's counts and the exclusive
// marker pairs (nil counts or no pairs: not measured).
func CompareLabels(groups []Group, a, b *SideLabels, countsA, countsB *segment.Counts, pairs [][2]string) (*Impact, error) {
	for _, s := range []*SideLabels{a, b} {
		if len(s.Covered) != len(s.Label) {
			return nil, fmt.Errorf("%d labels and %d cells covered", len(s.Label), len(s.Covered))
		}
		for _, l := range s.Label {
			if l >= int32(len(s.Names)) || l < -1 {
				return nil, fmt.Errorf("label %d of %d", l, len(s.Names))
			}
		}
	}
	for _, g := range groups {
		for _, c := range g.A {
			if int(c) >= len(a.Label) {
				return nil, fmt.Errorf("the comparison names cell %d of A's %d", c, len(a.Label))
			}
		}
		for _, c := range g.B {
			if int(c) >= len(b.Label) {
				return nil, fmt.Errorf("the comparison names cell %d of B's %d", c, len(b.Label))
			}
		}
	}
	im := &Impact{SensitiveA: make([]int32, len(a.Label)), SensitiveB: make([]int32, len(b.Label)), Others: []string{}, Changed: []int{}}
	for i := range im.SensitiveA {
		im.SensitiveA[i] = -1
	}
	for i := range im.SensitiveB {
		im.SensitiveB[i] = -1
	}
	others := map[string]int32{}
	flag := func(side []int32, cell int32, text string) {
		k, ok := others[text]
		if !ok {
			k = int32(len(im.Others))
			others[text] = k
			im.Others = append(im.Others, text)
		}
		side[cell] = k
	}

	// The cells, and the composition: each label's cells under each.
	count := func(s *SideLabels) (SideCells, map[string]int) {
		c := SideCells{Cells: len(s.Label)}
		by := map[string]int{}
		for i, l := range s.Label {
			if !s.Covered[i] {
				continue
			}
			c.Covered++
			if l >= 0 {
				c.Labeled++
			}
			by[s.name(int32(i))]++
		}
		return c, by
	}
	var byA, byB map[string]int
	im.CellsA, byA = count(a)
	im.CellsB, byB = count(b)
	seen := map[string]bool{}
	for _, names := range [][]string{a.Names, b.Names, {""}} {
		for _, n := range names {
			if !seen[n] && (byA[n] > 0 || byB[n] > 0) {
				seen[n] = true
				im.Composition = append(im.Composition, LabelCount{Name: n, A: byA[n], B: byB[n]})
			}
		}
	}

	im.Matched.ByClass = map[string][2]int{}
	transitions := map[[2]string]int{}
	onlyA, onlyB := map[string]int{}, map[string]int{}
	type changedGroup struct{ index, moved int }
	var changed []changedGroup
	covered := func(s *SideLabels, cells []int32) []int32 {
		var out []int32
		for _, c := range cells {
			if s.Covered[c] {
				out = append(out, c)
			}
		}
		return out
	}
	names := func(s *SideLabels, cells []int32) []string {
		out := make([]string, len(cells))
		for i, c := range cells {
			out[i] = s.name(c)
		}
		return out
	}
	for gi, g := range groups {
		ca, cb := covered(a, g.A), covered(b, g.B)
		switch {
		case len(g.A) == 1 && len(g.B) == 1 && (matchedClass(g.Class) || g.Class == AssignmentOnly):
			if len(ca) == 0 || len(cb) == 0 {
				continue
			}
			na, nb := a.name(ca[0]), b.name(cb[0])
			im.Matched.Cells++
			transitions[[2]string{na, nb}]++
			switch {
			case na != "" && nb != "":
				im.Matched.Labeled++
				bc := im.Matched.ByClass[g.Class]
				bc[0]++
				if na == nb {
					im.Matched.Same++
					bc[1]++
				}
				im.Matched.ByClass[g.Class] = bc
			case na != "" || nb != "":
				im.Matched.LabeledOne++
			}
			if na != nb {
				flag(im.SensitiveA, ca[0], labelOf(nb))
				flag(im.SensitiveB, cb[0], labelOf(na))
				changed = append(changed, changedGroup{gi, int(g.Changed)})
			}
		case g.Class == OnlyA:
			for _, c := range ca {
				onlyA[a.name(c)]++
			}
		case g.Class == OnlyB:
			for _, c := range cb {
				onlyB[b.name(c)]++
			}
		default:
			if len(ca) == 0 || len(cb) == 0 {
				continue
			}
			la, lb := names(a, ca), names(b, cb)
			// The fates, from the one cell's side (split: A's; merged: B's; rearranged: both).
			switch g.Class {
			case Split:
				oneToMany(&im.Split, la[0], lb)
			case Merged:
				oneToMany(&im.Merged, lb[0], la)
			default:
				manyToMany(&im.Rearranged, la, lb)
			}
			// Each cell whose counterparts carry another label, and what they carry.
			anyChange := false
			for i, c := range ca {
				if differs(la[i], lb) {
					anyChange = true
					flag(im.SensitiveA, c, groupLine(g.Class, true, len(g.A), len(g.B), lb))
				}
			}
			for i, c := range cb {
				if differs(lb[i], la) {
					anyChange = true
					flag(im.SensitiveB, c, groupLine(g.Class, false, len(g.A), len(g.B), la))
				}
			}
			if anyChange {
				changed = append(changed, changedGroup{gi, int(g.Changed)})
			}
		}
	}
	if im.Matched.Labeled > 0 {
		im.Matched.Agreement = Float(round6(float64(im.Matched.Same) / float64(im.Matched.Labeled)))
	} else {
		im.Matched.Agreement = Float(math.NaN())
	}
	for k, n := range transitions {
		im.Matched.Transitions = append(im.Matched.Transitions, Transition{A: k[0], B: k[1], Cells: n})
	}
	sort.Slice(im.Matched.Transitions, func(i, j int) bool {
		p, q := im.Matched.Transitions[i], im.Matched.Transitions[j]
		if p.Cells != q.Cells {
			return p.Cells > q.Cells
		}
		if p.A != q.A {
			return p.A < q.A
		}
		return p.B < q.B
	})
	im.OnlyA, im.OnlyB = counted(onlyA), counted(onlyB)
	sort.SliceStable(changed, func(i, j int) bool {
		if changed[i].moved != changed[j].moved {
			return changed[i].moved > changed[j].moved
		}
		return changed[i].index < changed[j].index
	})
	for _, c := range changed {
		im.Changed = append(im.Changed, c.index)
	}
	for i, k := range im.SensitiveA {
		if k >= 0 && a.Label[i] >= 0 {
			im.Sensitive[0]++
		}
	}
	for i, k := range im.SensitiveB {
		if k >= 0 && b.Label[i] >= 0 {
			im.Sensitive[1]++
		}
	}
	if countsA != nil && countsB != nil && len(pairs) > 0 {
		m := &Mixing{}
		var used int
		m.AllA, m.A, used = mixing(countsA, a, pairs)
		m.AllB, m.B, _ = mixing(countsB, b, pairs)
		m.Pairs = used
		if used > 0 {
			im.Mixing = m
		}
	}
	return im, nil
}

// oneToMany counts a split's or merge's fate: the one cell's label (none: not counted) against its
// counterparts'.
func oneToMany(f *Fates, one string, many []string) {
	if one == "" {
		return
	}
	f.Groups++
	keep := 0
	for _, n := range many {
		if n == one {
			keep++
		}
	}
	switch keep {
	case len(many):
		f.Kept++
	case 0:
		f.Changed++
	default:
		f.Partly++
	}
	if distinctLabeled(many) > 1 {
		f.Mixed++
	}
}

// manyToMany counts a rearrangement's fate: the labels of each side's cells alike, overlapping, or
// apart (a side without labels: not counted).
func manyToMany(f *Fates, la, lb []string) {
	sa, sb := set(la), set(lb)
	if len(sa) == 0 && len(sb) == 0 {
		return
	}
	f.Groups++
	shared := 0
	for n := range sa {
		if sb[n] {
			shared++
		}
	}
	switch {
	case shared == len(sa) && shared == len(sb):
		f.Kept++
	case shared == 0:
		f.Changed++
	default:
		f.Partly++
	}
	if len(sa) > 1 || len(sb) > 1 {
		f.Mixed++
	}
}

func set(names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		if n != "" {
			out[n] = true
		}
	}
	return out
}

func distinctLabeled(names []string) int { return len(set(names)) }

// differs says whether any counterpart carries a label other than name (no label counting as one).
func differs(name string, counterparts []string) bool {
	for _, n := range counterparts {
		if n != name {
			return true
		}
	}
	return false
}

// groupLine words what a cell's counterparts carry in a split, merge or rearrangement.
func groupLine(class string, fromA bool, nA, nB int, counterparts []string) string {
	switch {
	case class == Split && fromA:
		return fmt.Sprintf("split into %d cells: %s", nB, tally(counterparts))
	case class == Split:
		return fmt.Sprintf("one of %d cells split from a cell labeled %s", nB, tally(counterparts))
	case class == Merged && fromA:
		return fmt.Sprintf("one of %d cells merged into a cell labeled %s", nA, tally(counterparts))
	case class == Merged:
		return fmt.Sprintf("merged from %d cells: %s", nA, tally(counterparts))
	case fromA:
		return fmt.Sprintf("rearranged among %d cells: %s", nB, tally(counterparts))
	}
	return fmt.Sprintf("rearranged among %d cells: %s", nA, tally(counterparts))
}

func counted(by map[string]int) []LabelCount {
	out := []LabelCount{}
	for n, c := range by {
		out = append(out, LabelCount{Name: n, A: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].A != out[j].A {
			return out[i].A > out[j].A
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// mixing measures marker mixing among each label's covered cells (and all labeled ones): the cells
// expressing an exclusive pair together, and the MECR among them. Answers the pairs the panel has.
func mixing(c *segment.Counts, s *SideLabels, pairs [][2]string) (MixingLabel, []MixingLabel, int) {
	index := map[string]uint32{}
	for i, g := range c.Genes {
		index[g] = uint32(i)
	}
	type pair struct{ a, b uint32 }
	var used []pair
	for _, p := range pairs {
		x, okX := index[p[0]]
		y, okY := index[p[1]]
		if okX && okY && x != y {
			used = append(used, pair{x, y})
		}
	}
	L := len(s.Names)
	cells, mixed := make([]int, L+1), make([]int, L+1) // the last: all labeled cells
	either, both := make([][]int, L+1), make([][]int, L+1)
	for k := range either {
		either[k], both[k] = make([]int, len(used)), make([]int, len(used))
	}
	has := map[uint32]bool{}
	n := min(len(c.Offsets)-1, len(s.Label))
	for cell := 0; cell < n; cell++ {
		l := s.Label[cell]
		if !s.Covered[cell] || l < 0 {
			continue
		}
		clear(has)
		for k := c.Offsets[cell]; k < c.Offsets[cell+1]; k++ {
			if c.Count[k] > 0 {
				has[c.Gene[k]] = true
			}
		}
		pairsHere := 0
		for i, p := range used {
			x, y := has[p.a], has[p.b]
			for _, k := range []int{int(l), L} {
				if x || y {
					either[k][i]++
				}
				if x && y {
					both[k][i]++
				}
			}
			if x && y {
				pairsHere++
			}
		}
		for _, k := range []int{int(l), L} {
			cells[k]++
			if pairsHere > 0 {
				mixed[k]++
			}
		}
	}
	measure := func(k int, name string) MixingLabel {
		m := MixingLabel{Name: name, Cells: cells[k], Mixed: mixed[k], Share: Float(math.NaN()), MECR: Float(math.NaN())}
		if cells[k] > 0 {
			m.Share = Float(round6(float64(mixed[k]) / float64(cells[k])))
		}
		var sum float64
		var np int
		for i := range used {
			if either[k][i] > 0 {
				sum += float64(both[k][i]) / float64(either[k][i])
				np++
			}
		}
		if np > 0 {
			m.MECR = Float(round6(sum / float64(np)))
		}
		return m
	}
	var out []MixingLabel
	for k, name := range s.Names {
		if cells[k] > 0 {
			out = append(out, measure(k, name))
		}
	}
	return measure(L, "all labeled cells"), out, len(used)
}
