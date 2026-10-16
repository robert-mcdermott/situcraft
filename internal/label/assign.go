package label

import "math"

// Assignment is each cell's label at each level, from the top down: Label[level][cell] is the
// label (an index among the sets) the cell takes at that level, or −1 where it takes none and
// stays at the level above; Lead[level][cell] is how far the level's best label led the next (the
// minimum, for a label without siblings), NaN where the level was not reached (the cell took no label above, or its label
// has none below). Confidence is the lead of the label the cell carries: at its deepest level,
// or, for a cell without a label, the top level's (below the margin). State[state][cell] says
// whether the cell carries each state.
type Assignment struct {
	Levels     int
	Label      [][]int32
	Lead       [][]float64
	Confidence []float64
	State      [][]bool
}

// Assign labels the cells from their scores: at each level the candidates are the children of the
// label taken above (the top level's labels at first); the best (the first of equal scores) is
// taken when it leads the next by the margin (a label without siblings, the minimum) and scores at
// least the minimum. A state is carried where its
// score reaches the state threshold, by cells of its label (or below it) when it names one.
func Assign(sets []Set, levels int, sc *Scores, p Params) *Assignment {
	n := sc.Cells
	children := make([][]int, len(sets)+1) // the last: the top level's labels
	var states []int
	for k, s := range sets {
		switch {
		case s.State:
			states = append(states, k)
		case s.Parent < 0:
			children[len(sets)] = append(children[len(sets)], k)
		default:
			children[s.Parent] = append(children[s.Parent], k)
		}
	}
	a := &Assignment{Levels: levels, Confidence: make([]float64, n)}
	for l := 0; l < levels; l++ {
		lab := make([]int32, n)
		lead := make([]float64, n)
		for i := range lab {
			lab[i] = -1
			lead[i] = math.NaN()
		}
		a.Label = append(a.Label, lab)
		a.Lead = append(a.Lead, lead)
	}
	for i := 0; i < n; i++ {
		candidates := children[len(sets)]
		for l := 0; l < levels && len(candidates) > 0; l++ {
			best, next := -1, math.Inf(-1)
			for _, k := range candidates {
				s := sc.Score[k][i]
				switch {
				case best < 0 || s > sc.Score[best][i]:
					if best >= 0 {
						next = sc.Score[best][i]
					}
					best = k
				case s > next:
					next = s
				}
			}
			if len(candidates) == 1 {
				next = p.Minimum
			}
			lead := sc.Score[best][i] - next
			a.Lead[l][i] = lead
			take := lead >= p.Margin && sc.Score[best][i] >= p.Minimum
			if l == 0 || take {
				a.Confidence[i] = lead
			}
			if !take {
				break
			}
			a.Label[l][i] = int32(best)
			candidates = children[best]
		}
	}
	for _, k := range states {
		has := make([]bool, n)
		for i := 0; i < n; i++ {
			if !(sc.Score[k][i] >= p.State) {
				continue
			}
			if parent := sets[k].Parent; parent >= 0 && !a.carries(sets, i, parent) {
				continue
			}
			has[i] = true
		}
		a.State = append(a.State, has)
	}
	return a
}

// Deepest is the label a cell carries: its label at the deepest level it took one, or −1.
func (a *Assignment) Deepest(i int) int32 {
	out := int32(-1)
	for l := 0; l < a.Levels; l++ {
		if a.Label[l][i] < 0 {
			break
		}
		out = a.Label[l][i]
	}
	return out
}

// At is the label a cell carries at a level: its label there, or the one above where it stopped.
func (a *Assignment) At(level, i int) int32 {
	out := int32(-1)
	for l := 0; l <= level && l < a.Levels; l++ {
		if a.Label[l][i] < 0 {
			break
		}
		out = a.Label[l][i]
	}
	return out
}

// carries says whether cell i took label k at some level.
func (a *Assignment) carries(sets []Set, i, k int) bool {
	l := sets[k].Level
	return l >= 0 && l < a.Levels && a.Label[l][i] == int32(k)
}
