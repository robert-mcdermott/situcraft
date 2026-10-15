package refine

// A sweep of the labelling, in parallel and the same on any number of threads. The region is cut
// into square blocks (all layers), coloured in a 2 × 2 checkerboard; in each of four phases the
// blocks of one colour are updated in parallel, each proposing as many moves as it has voxels, at
// voxels chosen uniformly within it, from its own random stream. Blocks of one colour are a block
// apart, so a move in one never reads a voxel another changes. A cell may move in a phase only if
// it is near exactly one of the phase's blocks (its bounding box, grown by a voxel, meets one), so
// blocks change disjoint cells: a phase is then the same as its blocks updated one after another,
// in any order. Moves within a block never change which cells it may move (they stay within it),
// so each phase keeps the target. The checkerboard is shifted each sweep, by an offset drawn from
// the sweep's stream, so that cells near a corner are not always the ones that wait.
//
// Each transcript's place is then resampled (repositioning): a place drawn from the displacement
// prior about where it was observed, accepted with probability min(1, Λ(new)/Λ(old)), Λ the
// intensity of its gene at the place's voxel times its nuclear prior (an independence sampler
// whose proposal is the prior).

import (
	"sync"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// Streams of the labelling's sweep.
const (
	stepBlocks = iota + 20
	stepMoves
	stepReposition
)

// blocks are a sweep's checkerboard: the block side (voxels) and offset.
type blocks struct {
	side, ox, oy int
	nx, ny       int
}

func (s *State) blocks(side int, r *random.Stream) blocks {
	b := blocks{side: side, ox: r.IntN(side), oy: r.IntN(side)}
	b.nx = (s.Lat.W+b.ox+side-1)/side + 1
	b.ny = (s.Lat.H+b.oy+side-1)/side + 1
	return b
}

// of is the block (bx, by) holding column (x, y).
func (b blocks) of(x, y int) (int, int) { return (x + b.ox) / b.side, (y + b.oy) / b.side }

func (b blocks) colour(bx, by int) int { return bx%2 + 2*(by%2) }

// owners gives each cell the block of the colour that may move it (its index, by·nx + bx), or −1.
func (s *State) owners(b blocks, colour int) []int32 {
	l := s.Lat
	minX, minY := make([]int, s.Cells), make([]int, s.Cells)
	maxX, maxY := make([]int, s.Cells), make([]int, s.Cells)
	for c := range minX {
		minX[c], minY[c], maxX[c], maxY[c] = l.W, l.H, -1, -1
	}
	for v, c := range s.Labels {
		if c == 0 {
			continue
		}
		x, y := v%l.W, (v/l.W)%l.H
		i := c - 1
		minX[i], maxX[i] = min(minX[i], x), max(maxX[i], x)
		minY[i], maxY[i] = min(minY[i], y), max(maxY[i], y)
	}
	out := make([]int32, s.Cells)
	for c := range out {
		out[c] = -1
		bx0, by0 := b.of(max(0, minX[c]-1), max(0, minY[c]-1))
		bx1, by1 := b.of(min(l.W-1, maxX[c]+1), min(l.H-1, maxY[c]+1))
		found := 0
		for by := by0; by <= by1; by++ {
			for bx := bx0; bx <= bx1; bx++ {
				if b.colour(bx, by) == colour {
					found++
					out[c] = int32(by*b.nx + bx)
				}
			}
		}
		if found != 1 {
			out[c] = -1
		}
	}
	return out
}

// moveSweep runs the four phases of a sweep of the labelling, answering the moves made.
func (s *State) moveSweep(m *Model, side, workers int) int {
	l := s.Lat
	b := s.blocks(side, m.stream(stepBlocks, 0))
	moved := 0
	for colour := 0; colour < 4; colour++ {
		owner := s.owners(b, colour)
		var list []int
		for by := 0; by < b.ny; by++ {
			for bx := 0; bx < b.nx; bx++ {
				if b.colour(bx, by) == colour {
					list = append(list, by*b.nx+bx)
				}
			}
		}
		counts := make([]int, len(list))
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < max(1, workers); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					id := list[i]
					bx, by := id%b.nx, id/b.nx
					x0, y0 := max(0, bx*side-b.ox), max(0, by*side-b.oy)
					x1, y1 := min(l.W, (bx+1)*side-b.ox), min(l.H, (by+1)*side-b.oy)
					if x0 >= x1 || y0 >= y1 {
						continue
					}
					r := m.stream(stepMoves, colour<<24|id)
					w, h := x1-x0, y1-y0
					voxels := w * h * l.L
					for k := 0; k < voxels; k++ {
						v := l.Index(x0+r.IntN(w), y0+r.IntN(h), r.IntN(l.L))
						if s.stepOwned(m, r, v, owner, int32(id)) {
							counts[i]++
						}
					}
				}
			}()
		}
		for i := range list {
			next <- i
		}
		close(next)
		wg.Wait()
		for _, n := range counts {
			moved += n
		}
	}
	return moved
}

// stepOwned is a step that may change only cells the block owns (the background always).
func (s *State) stepOwned(m *Model, r *random.Stream, v int, owner []int32, block int32) bool {
	return s.step(m, r, v, func(c uint32) bool { return c == 0 || owner[c-1] == block })
}

// repositionChunk is how many transcripts share a random stream in repositioning.
const repositionChunk = 4096

// reposition resamples every transcript's place, then lists them by voxel again; it answers the
// transcripts that moved to another voxel.
func (s *State) reposition(m *Model) int {
	n := len(s.gene)
	chunks := (n + repositionChunk - 1) / repositionChunk
	set := s.Settings
	logIn, logOut := dmath.Log(m.H.NuclearPrior), dmath.Log(1-m.H.NuclearPrior)
	moved := make([]int, chunks)
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < max(1, m.Workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range next {
				r := m.stream(stepReposition, chunk)
				for i := chunk * repositionChunk; i < min(n, (chunk+1)*repositionChunk); i++ {
					sigma := set.NarrowUm
					if r.Float64() < set.WideShare {
						sigma = set.WideUm
					}
					x := float64(s.obsX[i]) + mul(sigma, r.Normal())
					y := float64(s.obsY[i]) + mul(sigma, r.Normal())
					u := r.Float64()
					v := s.Lat.Voxel(x, y, float64(s.posZ[i]))
					if v < 0 {
						continue
					}
					old := int(s.voxel[i])
					logRatio := 0.0
					if v != old {
						j := s.gene[i]
						a, b := s.Labels[old], s.Labels[v]
						logRatio = dmath.Log(intensity(m, b, j)) - dmath.Log(intensity(m, a, j))
						if q := s.prior[i]; q != 0 && (a == q) != (b == q) {
							if b == q {
								logRatio += logIn - logOut
							} else {
								logRatio += logOut - logIn
							}
						}
					}
					if logRatio < 0 && !(u < dmath.Exp(logRatio)) {
						continue
					}
					s.posX[i], s.posY[i] = x, y
					if v != old {
						s.voxel[i] = int32(v)
						moved[chunk]++
					}
				}
			}
		}()
	}
	for chunk := 0; chunk < chunks; chunk++ {
		next <- chunk
	}
	close(next)
	wg.Wait()
	s.reindex()
	total := 0
	for _, k := range moved {
		total += k
	}
	return total
}
