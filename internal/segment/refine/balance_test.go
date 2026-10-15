package refine

// The labelling's moves leave the target invariant (research.md §3.2, the sampler). On lattices
// small enough to list every labelling, with the model's parameters held fixed:
//   - detailed balance, exactly: for every state the moves reach and every move from it,
//     π(σ) K(σ → σ') = π(σ') K(σ' → σ), K built from the moves' probabilities; a move allowed one
//     way is allowed the other;
//   - every state reached keeps the constraints, checked independently (every cell non-empty and
//     face-connected, within its perimeter bound);
//   - the random steps sample it: independent chains end in each state as often as π says (χ²).

import (
	"math"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

// tinyState is a lattice of w × h × layers voxels of 2 µm (3 µm thick) with two cells, a few
// transcripts of two genes (some over nuclei) and a model with fixed parameters.
func tinyState(t *testing.T, w, h, layers int, labels []uint32, settings Settings) (*State, *Model) {
	t.Helper()
	l := Lattice{VoxelUm: 2, LayerUm: 3, W: w, H: h, L: layers}
	tr := Transcripts{}
	add := func(x, y, z float32, gene, prior uint32) {
		tr.X, tr.Y, tr.Z = append(tr.X, x), append(tr.Y, y), append(tr.Z, z)
		tr.Gene, tr.Prior = append(tr.Gene, gene), append(tr.Prior, prior)
	}
	add(1, 1, 1, 0, 1)
	add(1.5, 0.5, 1, 0, 1)
	add(float32(2*w)-1, float32(2*h)-1, float32(3*layers)-1, 1, 2)
	add(float32(w), float32(h), 1, 1, 0)
	add(float32(2*w)-1, 1, 1, 0, 0)
	add(1, float32(2*h)-1, float32(3*layers)-1, 1, 2)
	s, err := NewState(l, labels, 2, 2, tr, settings)
	if err != nil {
		t.Fatal(err)
	}
	h2 := DefaultHyper()
	h2.Components = 2
	m, err := NewModel(h2, s.Volumes(), 2, l.Volume(), 1)
	if err != nil {
		t.Fatal(err)
	}
	copy(m.Rate, []float64{0.4, 0.05, 0.03, 0.5})
	m.RateSum = []float64{0.45, 0.53}
	copy(m.Bg, []float64{0.02, 0.01})
	m.Comp = []int{0, 1}
	m.VolMean = []float64{math.Log(30), math.Log(40)}
	m.VolVar = []float64{0.3, 0.5}
	return s, m
}

func key(labels []uint32) string {
	b := make([]byte, len(labels))
	for i, c := range labels {
		b[i] = byte('0' + c)
	}
	return string(b)
}

// valid checks a labelling's constraints from scratch.
func valid(s *State) bool {
	for _, n := range s.cellVoxels {
		if n == 0 {
			return false
		}
	}
	if s.disconnected() != 0 {
		return false
	}
	for c := 0; c < s.Cells; c++ {
		for z := 0; z < s.Lat.L; z++ {
			i := c*s.Lat.L + z
			if a := s.area[i]; a > 0 && float64(s.perim[i]) > s.tolerance[c]*float64(minPerimeter(int(a)))+1e-12 {
				return false
			}
		}
	}
	return true
}

// reachable lists the states the moves reach from s's labelling, by breadth-first search.
func reachable(s *State, m *Model) [][]uint32 {
	start := append([]uint32(nil), s.Labels...)
	seen := map[string]bool{key(start): true}
	queue := [][]uint32{start}
	for i := 0; i < len(queue); i++ {
		s.setLabels(queue[i], m)
		for v := range s.Labels {
			for b := uint32(0); b <= uint32(s.Cells); b++ {
				if s.moveProbability(m, v, b) == 0 {
					continue
				}
				next := append([]uint32(nil), queue[i]...)
				next[v] = b
				if k := key(next); !seen[k] {
					seen[k] = true
					queue = append(queue, next)
				}
			}
		}
	}
	s.setLabels(start, m)
	return queue
}

func checkBalance(t *testing.T, s *State, m *Model) [][]uint32 {
	t.Helper()
	states := reachable(s, m)
	start := append([]uint32(nil), s.Labels...)
	logPi := map[string]float64{}
	for _, st := range states {
		s.setLabels(st, m)
		if !valid(s) {
			t.Fatalf("state %s breaks a constraint", key(st))
		}
		logPi[key(st)] = s.LogTarget(m)
	}
	moves := 0
	for _, st := range states {
		for v := range st {
			for b := uint32(0); b <= uint32(s.Cells); b++ {
				if b == st[v] {
					continue
				}
				s.setLabels(st, m)
				forward := s.moveProbability(m, v, b)
				next := append([]uint32(nil), st...)
				next[v] = b
				s.setLabels(next, m)
				back := 0.0
				if valid(s) {
					back = s.moveProbability(m, v, st[v])
				}
				if (forward == 0) != (back == 0) {
					t.Fatalf("%s → %s: allowed one way only (%g, %g)", key(st), key(next), forward, back)
				}
				if forward == 0 {
					continue
				}
				moves++
				lhs, rhs := logPi[key(st)]+math.Log(forward), logPi[key(next)]+math.Log(back)
				if math.Abs(lhs-rhs) > 1e-9*math.Max(1, math.Abs(lhs)) {
					t.Fatalf("%s ⇄ %s: π K %.12g one way, %.12g the other", key(st), key(next), lhs, rhs)
				}
			}
		}
	}
	s.setLabels(start, m)
	t.Logf("%d states reached, %d moves balanced", len(states), moves)
	if moves < 2*len(states) {
		t.Errorf("only %d moves among %d states", moves, len(states))
	}
	return states
}

// sampleFrequencies runs independent chains, each started from a draw of π and run steps steps
// at uniformly chosen voxels, and compares where they end with π (χ²): steps that keep π keep
// the chains distributed as π, however slowly they mix.
func sampleFrequencies(t *testing.T, s *State, m *Model, states [][]uint32, chains, steps int) {
	t.Helper()
	start := append([]uint32(nil), s.Labels...)
	index := map[string]int{}
	logPi := make([]float64, len(states))
	top := math.Inf(-1)
	for i, st := range states {
		index[key(st)] = i
		s.setLabels(st, m)
		logPi[i] = s.LogTarget(m)
		top = math.Max(top, logPi[i])
	}
	var total float64
	for i := range logPi {
		logPi[i] = math.Exp(logPi[i] - top)
		total += logPi[i]
	}
	observed := make([]float64, len(states))
	for c := 0; c < chains; c++ {
		r := random.New(31, uint64(c))
		u := r.Float64() * total
		from := 0
		for from < len(states)-1 && u >= logPi[from] {
			u -= logPi[from]
			from++
		}
		s.setLabels(states[from], m)
		for i := 0; i < steps; i++ {
			s.step(m, r, r.IntN(len(s.Labels)), nil)
		}
		observed[index[key(s.Labels)]]++
	}
	expected := make([]float64, len(states))
	for i := range expected {
		expected[i] = logPi[i] / total * float64(chains)
	}
	if stat, df, p := stats.ChiSquare(observed, expected, 0); p < alpha {
		t.Errorf("the chains' ends differ from π: χ² %.1f on %d degrees of freedom, p %.2g", stat, df, p)
	}
	s.setLabels(start, m)
}

// A 3 × 3 layer: cell 1 on the left, cell 2 on the right, the background between.
func TestMovesKeepDetailedBalanceInALayer(t *testing.T) {
	settings := DefaultSettings()
	settings.Bubble, settings.PerimeterFactor = 0.3, 1.2
	s, m := tinyState(t, 3, 3, 1, []uint32{1, 0, 2, 1, 0, 2, 1, 0, 0}, settings)
	states := checkBalance(t, s, m)
	sampleFrequencies(t, s, m, states, 20000, 200)
}

// 2 × 2 × 2: moves across layers too.
func TestMovesKeepDetailedBalanceInThreeDimensions(t *testing.T) {
	settings := DefaultSettings()
	settings.Bubble, settings.PerimeterFactor = 0.2, 2
	s, m := tinyState(t, 2, 2, 2, []uint32{1, 0, 1, 2, 1, 0, 0, 2}, settings)
	states := checkBalance(t, s, m)
	sampleFrequencies(t, s, m, states, 20000, 200)
}

// On a larger lattice with six cells, where a voxel can sit between two pieces of a cell that
// meet around it: along a random walk of the sampler, every proposed move is allowed exactly when
// its reverse is, and balances against the target computed afresh.
func TestMovesBalanceAlongAWalk(t *testing.T) {
	s, m := randomState(t, 41)
	s.Settings.Bubble = 0.1
	r := random.New(42, 1)
	checked, refusedBoth := 0, 0
	for i := 0; i < 20000; i++ {
		v := r.IntN(len(s.Labels))
		b := uint32(r.IntN(s.Cells + 1))
		a := s.Labels[v]
		if a != b {
			before := append([]uint32(nil), s.Labels...)
			forward := s.moveProbability(m, v, b)
			logBefore := s.LogTarget(m)
			after := append([]uint32(nil), before...)
			after[v] = b
			s.setLabels(after, m)
			back := 0.0
			if valid(s) {
				back = s.moveProbability(m, v, a)
			}
			logAfter := s.LogTarget(m)
			s.setLabels(before, m)
			switch {
			case (forward == 0) != (back == 0):
				t.Fatalf("step %d: voxel %d, %d → %d allowed one way only (%g, %g)", i, v, a, b, forward, back)
			case forward == 0:
				refusedBoth++
			default:
				if d := logBefore + math.Log(forward) - logAfter - math.Log(back); math.Abs(d) > 1e-9*math.Max(1, math.Abs(logBefore)) {
					t.Fatalf("step %d: voxel %d, %d → %d unbalanced by %g", i, v, a, b, d)
				}
				checked++
			}
		}
		s.step(m, r, r.IntN(len(s.Labels)), nil)
	}
	t.Logf("%d moves balanced, %d refused both ways", checked, refusedBoth)
	if checked < 2000 {
		t.Errorf("only %d moves checked", checked)
	}
}

// A voxel between two pieces of a cell that meet around it may not join it (it would close a
// loop), nor leave it: the rule holds both ways.
func TestAVoxelMayNotCloseALoop(t *testing.T) {
	// 5 × 3, one layer: cell 1 rings the middle row, cells 2 and 3 are inside it, and the centre
	// between them is the background.
	labels := []uint32{
		1, 1, 1, 1, 1,
		1, 2, 0, 3, 1,
		1, 1, 1, 1, 1,
	}
	l := Lattice{VoxelUm: 2, LayerUm: 3, W: 5, H: 3, L: 1}
	s, err := NewState(l, labels, 3, 1, Transcripts{}, DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	v := l.Index(2, 1, 0)
	if s.allowed(v, 1) {
		t.Error("the centre may join cell 1, joining the rows above and below it into a loop")
	}
	if !s.allowed(v, 2) || !s.allowed(v, 3) {
		t.Error("the centre may not join the cells beside it")
	}
}

// The parallel sweep keeps the target: on a 4 × 2 lattice in blocks of 2 voxels (several blocks
// to a phase, the checkerboard shifted each sweep), chains started from draws of π and run through
// whole sweeps end distributed as π.
func TestParallelSweepsKeepTheTarget(t *testing.T) {
	settings := DefaultSettings()
	settings.Bubble = 0.2
	s, m := tinyState(t, 4, 2, 1, []uint32{1, 1, 0, 2, 1, 0, 0, 2}, settings)
	states := reachable(s, m)
	logPi := make([]float64, len(states))
	top := math.Inf(-1)
	index := map[string]int{}
	for i, st := range states {
		index[key(st)] = i
		s.setLabels(st, m)
		logPi[i] = s.LogTarget(m)
		top = math.Max(top, logPi[i])
	}
	var total float64
	for i := range logPi {
		logPi[i] = math.Exp(logPi[i] - top)
		total += logPi[i]
	}
	const chains = 20000
	observed := make([]float64, len(states))
	for c := 0; c < chains; c++ {
		r := random.New(51, uint64(c))
		u := r.Float64() * total
		from := 0
		for from < len(states)-1 && u >= logPi[from] {
			u -= logPi[from]
			from++
		}
		s.setLabels(states[from], m)
		m.Seed = uint64(c)
		for sweep := 0; sweep < 6; sweep++ {
			m.Sweep = sweep
			s.moveSweep(m, 2, 3)
		}
		observed[index[key(s.Labels)]]++
	}
	expected := make([]float64, len(states))
	for i := range expected {
		expected[i] = logPi[i] / total * chains
	}
	if stat, df, p := stats.ChiSquare(observed, expected, 0); p < alpha {
		t.Errorf("parallel sweeps' ends differ from π: χ² %.1f on %d degrees of freedom, p %.2g (%d states)", stat, df, p, len(states))
	}
}
