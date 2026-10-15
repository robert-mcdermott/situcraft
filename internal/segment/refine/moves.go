package refine

// The labelling's moves (research.md §3.2, the sampler). A voxel v is chosen; with probability
// p_bubble the background is proposed for it, otherwise the label of one of its face neighbours,
// chosen uniformly (copying a neighbour's label grows that cell, or the background, into v). The
// voxel's neighbours do not change when it moves, so the proposal's probability both ways is
// local:
//
//	q(a → b) ∝ p_bubble [b = 0] + (1 − p_bubble) n_b(v) / n(v),
//
// n_b(v) the face neighbours of v labelled b among its n(v); the move is accepted with
// probability min(1, exp(Δ) q(b → a) / q(a → b)), Δ the target's change (MoveDelta).
//
// The constraints, each holding in both directions so that a move is allowed exactly when its
// reverse is (detailed balance):
//   - no cell is emptied;
//   - connectivity: among the voxels about v (its 3 × 3 × 3 neighbourhood), those of a cell the
//     move takes v from, and those of a cell it gives v to, each form one face-connected piece
//     touching v's faces. Removing v then cannot disconnect the first, adding it cannot join
//     pieces of the second (a local test of simple points; Kong TY, Rosenfeld A. Digital
//     topology: introduction and survey. Comput Vis Graph Image Process 1989;48:357–393);
//   - the perimeter bound: in each layer, a cell's perimeter (voxel edges) is at most its
//     factor times the least any shape of its area can have, 2⌈2√n⌉ for n voxels (Harary F,
//     Harborth H. Extremal animals. J Combin Inform System Sci 1976;1:1–8).

import (
	"errors"
	"math"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// Settings are the sampler's settings.
type Settings struct {
	// PerimeterFactor bounds each cell's perimeter in each layer: at most this many times the
	// least perimeter a shape of its area can have (a cell starting beyond it keeps its own
	// starting ratio as its bound).
	PerimeterFactor float64 `json:"perimeter_factor"`
	// Bubble is the probability of proposing the background for a voxel (otherwise a neighbour's
	// label), which lets gaps open between cells.
	Bubble float64 `json:"bubble"`
	// Repositioning: each transcript's true place in x and y is its observed place displaced by a
	// normal of σ WideUm with probability WideShare, else of σ NarrowUm; Reposition false keeps
	// transcripts where they were observed.
	Reposition bool    `json:"reposition"`
	WideShare  float64 `json:"diffusion_wide_share"`
	WideUm     float64 `json:"diffusion_wide_um"`
	NarrowUm   float64 `json:"diffusion_narrow_um"`
}

// DefaultSettings are SituCraft's defaults (research.md §3.2).
func DefaultSettings() Settings {
	return Settings{PerimeterFactor: 2, Bubble: 0.05, Reposition: true, WideShare: 0.2, WideUm: 4, NarrowUm: 0.5}
}

// Check refuses settings the sampler cannot use.
func (s Settings) Check() error {
	switch {
	case !(s.PerimeterFactor >= 1):
		return errors.New("refine: the perimeter factor is 1 or more")
	case !(s.Bubble >= 0 && s.Bubble < 1):
		return errors.New("refine: the bubble probability is from 0 to below 1")
	case s.Reposition && !(s.WideShare >= 0 && s.WideShare <= 1 && s.WideUm > 0 && s.NarrowUm > 0):
		return errors.New("refine: repositioning's share is from 0 to 1 and its widths positive")
	}
	return nil
}

// minPerimeter is the least perimeter, in voxel edges, of n voxels: 2⌈2√n⌉.
func minPerimeter(n int) int {
	if n <= 0 {
		return 0
	}
	k := int(math.Sqrt(float64(4 * n)))
	for k*k < 4*n {
		k++
	}
	for k > 0 && (k-1)*(k-1) >= 4*n {
		k--
	}
	return 2 * k
}

// faceNeighbors fills nb with v's face neighbours in the lattice (in its layer, then above and
// below), answering how many.
func (l Lattice) faceNeighbors(v int, nb *[6]int) int {
	plane := l.W * l.H
	x, y, z := v%l.W, (v/l.W)%l.H, v/plane
	n := 0
	if x > 0 {
		nb[n] = v - 1
		n++
	}
	if x < l.W-1 {
		nb[n] = v + 1
		n++
	}
	if y > 0 {
		nb[n] = v - l.W
		n++
	}
	if y < l.H-1 {
		nb[n] = v + l.W
		n++
	}
	if z > 0 {
		nb[n] = v - plane
		n++
	}
	if z < l.L-1 {
		nb[n] = v + plane
		n++
	}
	return n
}

// layerNeighbors counts v's neighbours in its layer (4-connected) labelled c.
func (s *State) layerNeighbors(v int, c uint32) int {
	l := s.Lat
	x, y := v%l.W, (v/l.W)%l.H
	n := 0
	if x > 0 && s.Labels[v-1] == c {
		n++
	}
	if x < l.W-1 && s.Labels[v+1] == c {
		n++
	}
	if y > 0 && s.Labels[v-l.W] == c {
		n++
	}
	if y < l.H-1 && s.Labels[v+l.W] == c {
		n++
	}
	return n
}

// outside marks the cube's places beyond the lattice.
const outside = ^uint32(0)

// cubeFaces are each place's face neighbours within the 3 × 3 × 3 cube (index dz·9 + dy·3 + dx).
var cubeFaces = func() [27][]int {
	var out [27][]int
	for i := 0; i < 27; i++ {
		x, y, z := i%3, i/3%3, i/9
		for _, d := range [6][3]int{{-1, 0, 0}, {1, 0, 0}, {0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}} {
			nx, ny, nz := x+d[0], y+d[1], z+d[2]
			if nx >= 0 && ny >= 0 && nz >= 0 && nx < 3 && ny < 3 && nz < 3 {
				out[i] = append(out[i], nz*9+ny*3+nx)
			}
		}
	}
	return out
}()

// cube fills c with the labels about v (its centre, 13, included; outside beyond the lattice).
func (s *State) cube(v int, c *[27]uint32) {
	l := s.Lat
	x, y, z := v%l.W, (v/l.W)%l.H, v/(l.W*l.H)
	for i := 0; i < 27; i++ {
		nx, ny, nz := x+i%3-1, y+i/3%3-1, z+i/9-1
		if nx < 0 || ny < 0 || nz < 0 || nx >= l.W || ny >= l.H || nz >= l.L {
			c[i] = outside
		} else {
			c[i] = s.Labels[l.Index(nx, ny, nz)]
		}
	}
}

// facePieces counts the face-connected pieces of the cube's voxels labelled x, the centre left
// out, that touch the centre's faces.
func facePieces(c *[27]uint32, x uint32) int {
	var seen [27]bool
	seen[13] = true
	pieces := 0
	var stack [27]int
	for _, f := range cubeFaces[13] {
		if seen[f] || c[f] != x {
			continue
		}
		pieces++
		n := 1
		stack[0] = f
		seen[f] = true
		for n > 0 {
			n--
			p := stack[n]
			for _, q := range cubeFaces[p] {
				if !seen[q] && c[q] == x {
					seen[q] = true
					stack[n] = q
					n++
				}
			}
		}
	}
	return pieces
}

// allowed reports whether voxel v may move from its cell a to b, every constraint kept.
func (s *State) allowed(v int, b uint32) bool {
	a := s.Labels[v]
	if a == b {
		return false
	}
	if a > 0 && s.cellVoxels[a-1] < 2 {
		return false
	}
	var c [27]uint32
	s.cube(v, &c)
	if a > 0 && facePieces(&c, a) != 1 {
		return false
	}
	if b > 0 && facePieces(&c, b) != 1 {
		return false
	}
	l := s.Lat
	z := v / (l.W * l.H)
	if a > 0 {
		i := int(a-1)*l.L + z
		area, perim := int(s.area[i])-1, int(s.perim[i])+2*s.layerNeighbors(v, a)-4
		if area > 0 && float64(perim) > mul(s.tolerance[a-1], float64(minPerimeter(area))) {
			return false
		}
	}
	if b > 0 {
		i := int(b-1)*l.L + z
		area, perim := int(s.area[i])+1, int(s.perim[i])+4-2*s.layerNeighbors(v, b)
		if float64(perim) > mul(s.tolerance[b-1], float64(minPerimeter(area))) {
			return false
		}
	}
	return true
}

// proposalWeight is the probability of proposing b for v, times the number of voxels it is
// chosen among: p_bubble [b = 0] + (1 − p_bubble) n_b(v)/n(v).
func (s *State) proposalWeight(v int, b uint32) float64 {
	var nb [6]int
	n := s.Lat.faceNeighbors(v, &nb)
	if n == 0 {
		return s.Settings.Bubble
	}
	same := 0
	for _, u := range nb[:n] {
		if s.Labels[u] == b {
			same++
		}
	}
	w := mul(1-s.Settings.Bubble, float64(same)/float64(n))
	if b == 0 {
		w += s.Settings.Bubble
	}
	return w
}

// logAcceptance is the log of the move's Metropolis–Hastings ratio (−∞ for a move not allowed).
func (s *State) logAcceptance(m *Model, v int, b uint32) float64 {
	if !s.allowed(v, b) {
		return math.Inf(-1)
	}
	forward, back := s.proposalWeight(v, b), s.proposalWeight(v, s.Labels[v])
	if back == 0 {
		return math.Inf(-1)
	}
	return s.MoveDelta(m, v, b) + dmath.Log(back) - dmath.Log(forward)
}

// moveProbability is the probability that a step at v moves it to b (the proposal's weight
// times the acceptance), for the tests that build the transition matrix.
func (s *State) moveProbability(m *Model, v int, b uint32) float64 {
	if b == s.Labels[v] {
		return 0
	}
	x := s.logAcceptance(m, v, b)
	if math.IsInf(x, -1) {
		return 0
	}
	return mul(s.proposalWeight(v, b), math.Min(1, dmath.Exp(math.Min(0, x))))
}

// step proposes a move for voxel v, drawing from r, and makes it if accepted; may says which
// labels this step may change (nil: all). It answers whether v moved.
func (s *State) step(m *Model, r *random.Stream, v int, may func(uint32) bool) bool {
	a := s.Labels[v]
	var b uint32
	if r.Float64() < s.Settings.Bubble {
		b = 0
	} else {
		var nb [6]int
		n := s.Lat.faceNeighbors(v, &nb)
		if n == 0 {
			return false
		}
		b = s.Labels[nb[r.IntN(n)]]
	}
	if b == a {
		return false
	}
	if may != nil && !(may(a) && may(b)) {
		return false
	}
	x := s.logAcceptance(m, v, b)
	if math.IsInf(x, -1) || x < 0 && !(r.Float64() < dmath.Exp(x)) {
		return false
	}
	s.Move(m, v, b)
	return true
}
