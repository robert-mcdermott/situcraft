package refine

// A run: the cells to start from (a label image, each cell's seed point), sampled in stages of
// finer voxels (research.md §3.2: 4 µm and one layer, then 2 µm and two, then 1 µm and four),
// each sweep drawing the attributions and parameters, the labelling's moves and the transcripts'
// places. Each stage refines the last by whole factors, so no region changes owner.

import (
	"context"
	"errors"
	"fmt"
	"math"

	"situcraft/internal/segment"
)

// Stage is one resolution of a run: a voxel's side (µm), the layers, and the sweeps.
type Stage struct {
	VoxelUm float64 `json:"voxel_um"`
	Layers  int     `json:"layers"`
	Sweeps  int     `json:"sweeps"`
}

// Options are a run's settings, every one recorded with what it makes.
type Options struct {
	Model   Hyper    `json:"model"`
	Sampler Settings `json:"sampler"`
	Stages  []Stage  `json:"stages"`
	// BlockUm is the side of the checkerboard's blocks (µm).
	BlockUm float64 `json:"block_um"`
	// Samples is how many of the last sweeps the posterior summaries count.
	Samples int    `json:"samples"`
	Seed    uint64 `json:"seed"`
	Workers int    `json:"-"`
}

// DefaultOptions are SituCraft's defaults: one stage of 1 µm voxels in one layer, 600 sweeps,
// the last 500 summarized (research.md §3.2: on simulated tissues they recover the true cells
// best, and the boundaries' posterior is broad enough that its summary gains from every sample
// after the first hundred; a section of 5–10 µm holds about one layer of cells).
func DefaultOptions() Options {
	return Options{Model: DefaultHyper(), Sampler: DefaultSettings(), BlockUm: 40, Seed: 20250, Samples: 500,
		Stages: []Stage{{1, 1, 600}}}
}

// PublishedStages are the publication's schedule: 4 µm and one layer, then 2 µm and two, then
// 1 µm and four.
func PublishedStages() []Stage { return []Stage{{4, 1, 150}, {2, 2, 150}, {1, 4, 300}} }

// Check refuses options a run cannot use.
func (o Options) Check() error {
	if err := o.Model.Check(); err != nil {
		return err
	}
	if err := o.Sampler.Check(); err != nil {
		return err
	}
	if len(o.Stages) == 0 {
		return errors.New("refine: a run needs a stage")
	}
	for i, st := range o.Stages {
		if !(st.VoxelUm > 0) || st.Layers < 1 || st.Sweeps < 0 {
			return fmt.Errorf("refine: stage %d needs a positive voxel, a layer and sweeps", i+1)
		}
		if i > 0 {
			prev := o.Stages[i-1]
			f := prev.VoxelUm / st.VoxelUm
			if f < 1 || f != math.Round(f) || st.Layers%prev.Layers != 0 {
				return fmt.Errorf("refine: stage %d does not refine stage %d by whole factors", i+1, i)
			}
		}
	}
	if last := o.Stages[len(o.Stages)-1]; o.Samples < 1 || o.Samples > last.Sweeps || o.Samples > 65535 {
		return errors.New("refine: the samples are from 1 to the last stage's sweeps")
	}
	if !(o.BlockUm >= 2*o.Stages[0].VoxelUm) {
		return errors.New("refine: blocks are at least two of the coarsest voxels across")
	}
	return nil
}

// Inputs are what a run segments: a region (µm), its transcripts with their prior cells, and
// the cells to start from.
type Inputs struct {
	X0, Y0, Z0                 float64
	WidthUm, HeightUm, DepthUm float64
	Genes                      int
	Transcripts                Transcripts
	// Cells are the cells to start from: their label image (label k is cell k, on a grid of
	// Start.PixelSize from (X0, Y0)) and a point of each (its nucleus's centre), µm.
	Cells        int
	Start        *segment.Raster
	SeedX, SeedY []float64
	// NucleusUm2 is each cell's nuclei's area (µm²; 0 for a cell with no nucleus, whose seed is its
	// own centre).
	NucleusUm2 []float64
}

// SweepTrace is what a sweep did.
type SweepTrace struct {
	Stage         int     `json:"stage"`
	Moved         int     `json:"voxels_moved"`
	Repositioned  int     `json:"transcripts_repositioned"`
	LogLikelihood float64 `json:"log_likelihood"`
}

// Result is a run's end: the last sample's labelling and model; the posterior summaries over the
// last sweeps (each voxel's most frequent label, each transcript's most frequent cell and its
// probability); the input cells each cell is (Kept[k−1] for cell k, from 0), the stage it
// started at, and every sweep's trace.
type Result struct {
	State       *State
	Model       *Model
	Labels      []uint32
	LabelShare  []float32
	Assigned    []uint32
	Probability []float32
	Kept        []int
	StartStage  int
	Trace       []SweepTrace
	// SplitHalf is the share of transcripts whose most frequent cell is the same over the first
	// and the second half of the samples (a check that the summary has settled); Samples the
	// sweeps summarized.
	SplitHalf float64
	Samples   int
	// Overflow counts the tallies' labels beyond their slots (left uncounted).
	Overflow int
}

// startLabels is the start's labels at a lattice (the same in every layer): each column the label
// most of its pixels have (ties to the lower), each cell then its piece holding its seed (else its
// largest); a cell left with no column takes its seed's column when that is the background's or
// another cell's spare. It answers the labels and the cells that could not be kept.
func startLabels(in *Inputs, l Lattice) ([]uint32, []int) {
	r := in.Start
	cols := make([]uint32, l.W*l.H)
	counts := make([]map[uint32]int, l.W)
	row := make([]uint32, r.Width)
	ps := r.PixelSize
	for cy := 0; cy < l.H; cy++ {
		for i := range counts {
			counts[i] = map[uint32]int{}
		}
		// The pixel rows whose centres lie in the lattice row: (py + ½)·ps in [cy·s, (cy+1)·s).
		y0 := int(math.Ceil(mul(float64(cy), l.VoxelUm)/ps - 0.5))
		y1 := min(r.Height, int(math.Ceil(mul(float64(cy+1), l.VoxelUm)/ps-0.5)))
		for py := max(0, y0); py < y1; py++ {
			r.Row(py, row)
			for px, label := range row {
				// The pixel's centre's column.
				cx := int((mul(float64(px), ps) + ps/2) / l.VoxelUm)
				if cx < l.W {
					counts[cx][label]++
				}
			}
		}
		for cx, cnt := range counts {
			best, bestN := uint32(0), -1
			for label, n := range cnt {
				if n > bestN || n == bestN && label < best {
					best, bestN = label, n
				}
			}
			cols[cy*l.W+cx] = best
		}
	}
	seedCol := func(c int) int {
		x := int((in.SeedX[c] - in.X0) / l.VoxelUm)
		y := int((in.SeedY[c] - in.Y0) / l.VoxelUm)
		return min(l.H-1, max(0, y))*l.W + min(l.W-1, max(0, x))
	}
	keepPieces(cols, l.W, l.H, in.Cells, seedCol)
	size := make([]int, in.Cells+1)
	for _, c := range cols {
		size[c]++
	}
	var lost []int
	for c := 1; c <= in.Cells; c++ {
		if size[c] > 0 {
			continue
		}
		col := seedCol(c - 1)
		if holder := cols[col]; holder == 0 || size[holder] > 1 {
			size[holder]--
			cols[col] = uint32(c)
			size[c] = 1
		} else {
			lost = append(lost, c)
		}
	}
	// Taking a column can split its former holder: keep its pieces again.
	keepPieces(cols, l.W, l.H, in.Cells, seedCol)
	for c := 1; c <= in.Cells; c++ {
		size[c] = 0
	}
	for _, c := range cols {
		size[c]++
	}
	lost = lost[:0]
	for c := 1; c <= in.Cells; c++ {
		if size[c] == 0 {
			lost = append(lost, c)
		}
	}
	labels := make([]uint32, l.Voxels())
	for z := 0; z < l.L; z++ {
		copy(labels[z*l.W*l.H:], cols)
	}
	return labels, lost
}

// keepPieces leaves each cell only its 4-connected piece holding its seed column (else its
// largest piece; ties to the first), the rest the background's.
func keepPieces(cols []uint32, w, h, cells int, seedCol func(int) int) {
	piece := make([]int32, len(cols))
	for i := range piece {
		piece[i] = -1
	}
	var sizes []int
	var owner []uint32
	var stack []int
	for start, c := range cols {
		if c == 0 || piece[start] >= 0 {
			continue
		}
		id := int32(len(sizes))
		sizes, owner = append(sizes, 0), append(owner, c)
		piece[start] = id
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			sizes[id]++
			x, y := p%w, p/w
			for _, q := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if q[0] < 0 || q[1] < 0 || q[0] >= w || q[1] >= h {
					continue
				}
				n := q[1]*w + q[0]
				if cols[n] == c && piece[n] < 0 {
					piece[n] = id
					stack = append(stack, n)
				}
			}
		}
	}
	keep := make([]int32, cells+1)
	for c := range keep {
		keep[c] = -1
	}
	for c := 1; c <= cells; c++ {
		if s := seedCol(c - 1); cols[s] == uint32(c) {
			keep[c] = piece[s]
		}
	}
	for id, c := range owner {
		if keep[c] < 0 || keep[c] != int32(id) && sizes[id] > sizes[keep[c]] && cols[seedCol(int(c)-1)] != c {
			keep[c] = int32(id)
		}
	}
	for i, c := range cols {
		if c != 0 && piece[i] != keep[c] {
			cols[i] = 0
		}
	}
}

// lattice is a stage's lattice over the inputs' region.
func (in *Inputs) lattice(st Stage, coarsest Stage) Lattice {
	f := coarsest.VoxelUm / st.VoxelUm
	w := int(math.Ceil(in.WidthUm/coarsest.VoxelUm)) * int(f)
	h := int(math.Ceil(in.HeightUm/coarsest.VoxelUm)) * int(f)
	depth := math.Max(in.DepthUm, 1e-3)
	return Lattice{X0: in.X0, Y0: in.Y0, Z0: in.Z0, VoxelUm: st.VoxelUm, LayerUm: depth / float64(st.Layers), W: w, H: h, L: st.Layers}
}

// refined is the state on a lattice finer by fx in x and y and fz in z: each voxel split, each
// transcript where it was, each cell's perimeter bound measured again.
func (s *State) refined(fx, fz int) (*State, error) {
	l := s.Lat
	nl := Lattice{X0: l.X0, Y0: l.Y0, Z0: l.Z0, VoxelUm: l.VoxelUm / float64(fx), LayerUm: l.LayerUm / float64(fz), W: l.W * fx, H: l.H * fx, L: l.L * fz}
	labels := make([]uint32, nl.Voxels())
	for z := 0; z < nl.L; z++ {
		for y := 0; y < nl.H; y++ {
			for x := 0; x < nl.W; x++ {
				labels[nl.Index(x, y, z)] = s.Labels[l.Index(x/fx, y/fx, z/fz)]
			}
		}
	}
	n := len(s.gene)
	out := &State{Lat: nl, Labels: labels, Cells: s.Cells, Genes: s.Genes, Settings: s.Settings,
		cellVoxels: make([]int, s.Cells), gene: s.gene, prior: s.prior, obsX: s.obsX, obsY: s.obsY,
		posX: s.posX, posY: s.posY, posZ: s.posZ, voxel: make([]int32, n), fromCell: s.fromCell}
	for _, c := range labels {
		if c > 0 {
			out.cellVoxels[c-1]++
		}
	}
	for i := 0; i < n; i++ {
		v := nl.Voxel(s.posX[i], s.posY[i], float64(s.posZ[i]))
		if v < 0 {
			return nil, fmt.Errorf("refine: transcript %d left the region", i)
		}
		out.voxel[i] = int32(v)
	}
	out.reindex()
	out.measure()
	return out, nil
}

// Run segments the inputs.
func Run(ctx context.Context, in *Inputs, opt Options, progress func(float64, string)) (*Result, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if err := opt.Check(); err != nil {
		return nil, err
	}
	if in.Cells < 1 || in.Start == nil || len(in.SeedX) != in.Cells || len(in.SeedY) != in.Cells {
		return nil, errors.New("refine: a run needs cells to start from, with a seed for each")
	}
	// The coarsest stage at which every cell keeps a column; at the finest, cells that still
	// cannot are left out.
	start := 0
	var labels []uint32
	var lost []int
	for start = 0; start < len(opt.Stages); start++ {
		labels, lost = startLabels(in, in.lattice(opt.Stages[start], opt.Stages[0]))
		if len(lost) == 0 || start == len(opt.Stages)-1 {
			break
		}
	}
	keep := make([]uint32, in.Cells+1)
	var kept []int
	isLost := map[int]bool{}
	for _, c := range lost {
		isLost[c] = true
	}
	for c := 1; c <= in.Cells; c++ {
		if !isLost[c] {
			kept = append(kept, c-1)
			keep[c] = uint32(len(kept))
		}
	}
	for i, c := range labels {
		labels[i] = keep[c]
	}
	tr := in.Transcripts
	prior := make([]uint32, len(tr.Prior))
	for i, q := range tr.Prior {
		prior[i] = keep[q]
	}
	tr.Prior = prior
	lat := in.lattice(opt.Stages[start], opt.Stages[0])
	s, err := NewState(lat, labels, len(kept), in.Genes, tr, opt.Sampler)
	if err != nil {
		return nil, err
	}
	m, err := NewModel(opt.Model, s.Volumes(), in.Genes, lat.Volume(), opt.Seed)
	if err != nil {
		return nil, err
	}
	if opt.Workers > 0 {
		m.Workers = opt.Workers
	}
	s.AttributeByPlace(m)
	m.Init()
	res := &Result{Model: m, Kept: kept, StartStage: start}
	total := 0
	for _, st := range opt.Stages[start:] {
		total += st.Sweeps
	}
	done := 0
	var voxels, transcripts *tally
	var halves [2]*tally
	for si := start; si < len(opt.Stages); si++ {
		st := opt.Stages[si]
		if si > start {
			prev := opt.Stages[si-1]
			if s, err = s.refined(int(math.Round(prev.VoxelUm/st.VoxelUm)), st.Layers/prev.Layers); err != nil {
				return nil, err
			}
		}
		side := max(2, int(math.Round(opt.BlockUm/st.VoxelUm)))
		for k := 0; k < st.Sweeps; k++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			s.Attribute(m)
			m.SweepParameters()
			t := SweepTrace{Stage: si, Moved: s.moveSweep(m, side, m.Workers)}
			if s.Settings.Reposition {
				t.Repositioned = s.reposition(m)
			}
			t.LogLikelihood = s.LogLikelihood(m)
			res.Trace = append(res.Trace, t)
			if si == len(opt.Stages)-1 && k >= st.Sweeps-opt.Samples {
				if voxels == nil {
					voxels, transcripts = newTally(len(s.Labels)), newTally(len(s.voxel))
					halves = [2]*tally{newTally(len(s.voxel)), newTally(len(s.voxel))}
				}
				half := halves[0]
				if 2*(k-(st.Sweeps-opt.Samples)) >= opt.Samples {
					half = halves[1]
				}
				for v, c := range s.Labels {
					voxels.add(v, c)
				}
				for i, v := range s.voxel {
					transcripts.add(i, s.Labels[v])
					half.add(i, s.Labels[v])
				}
				voxels.samples++
				transcripts.samples++
				half.samples++
			}
			done++
			progress(float64(done)/float64(max(1, total)), fmt.Sprintf("Sampling at %g µm (sweep %d of %d)", st.VoxelUm, k+1, st.Sweeps))
		}
	}
	res.State = s
	res.Labels = make([]uint32, len(s.Labels))
	res.LabelShare = make([]float32, len(s.Labels))
	for v := range res.Labels {
		c, p := voxels.mode(v)
		res.Labels[v], res.LabelShare[v] = c, float32(p)
	}
	if halves[1].samples > 0 {
		same := 0
		for i := range s.voxel {
			a, _ := halves[0].mode(i)
			b, _ := halves[1].mode(i)
			if a == b {
				same++
			}
		}
		res.SplitHalf = float64(same) / float64(max(1, len(s.voxel)))
	}
	res.Assigned = make([]uint32, len(s.voxel))
	res.Probability = make([]float32, len(s.voxel))
	for i := range res.Assigned {
		c, p := transcripts.mode(i)
		res.Assigned[i], res.Probability[i] = c, float32(p)
	}
	res.Overflow = voxels.overflow + transcripts.overflow
	res.Samples = voxels.samples
	return res, nil
}

// Consensus is each column's cell in a labelling of the lattice, x then y: the label holding most
// of its layers, the background counted like a cell; a tie between cells goes to the lower, and
// between a cell and the background to the cell. A column is a cell's when the cell fills at least
// as many of its layers as anything else, as a 2D segmentation of the section would draw it.
func (l Lattice) Consensus(labels []uint32) []uint32 {
	out := make([]uint32, l.W*l.H)
	count := map[uint32]int{}
	for i := range out {
		clear(count)
		for z := 0; z < l.L; z++ {
			count[labels[z*l.W*l.H+i]]++
		}
		best, bestN := uint32(0), 0
		for c, n := range count {
			better := n > bestN
			if n == bestN {
				better = best == 0 || c != 0 && c < best
			}
			if better {
				best, bestN = c, n
			}
		}
		out[i] = best
	}
	return out
}

// Assignments are each transcript's cell (by its place now; 0 the background), in the inputs'
// order.
func (s *State) Assignments() []uint32 {
	out := make([]uint32, len(s.voxel))
	for i, v := range s.voxel {
		out[i] = s.Labels[v]
	}
	return out
}

// Consensus is the state's columns' cells (Lattice.Consensus of its labels).
func (s *State) Consensus() []uint32 { return s.Lat.Consensus(s.Labels) }
