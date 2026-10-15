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
	// own centre); Box each cell's bounding box (µm; empty for a cell with no pixel).
	NucleusUm2 []float64
	Box        []Region
	// MinQV is the quality the transcripts reach.
	MinQV float64
}

// SweepTrace is what a sweep did.
type SweepTrace struct {
	Stage         int     `json:"stage"`
	Moved         int     `json:"voxels_moved"`
	Repositioned  int     `json:"transcripts_repositioned"`
	LogLikelihood float64 `json:"log_likelihood"`
}

// Result is one region's run: the last sample's labelling and model; the posterior summaries over
// the last sweeps (each voxel's most frequent label and its share, each transcript's most frequent
// cell and its probability); the input cells each cell is (Kept[k−1] for cell k, from 0) and
// whether it was held where it was (Frozen[k−1]); each of its transcripts' index in the inputs;
// the region refined; the free cells left out (input indices); the stage it started at; every
// sweep's trace.
type Result struct {
	State       *State
	Model       *Model
	Labels      []uint32
	LabelShare  []float32
	Assigned    []uint32
	Probability []float32
	Kept        []int
	Frozen      []bool
	Index       []int32
	Region      Region
	LeftOut     []int
	StartStage  int
	Trace       []SweepTrace
	// SplitHalf is the share of transcripts whose most frequent cell is the same over the first
	// and the second half of the samples (a check that the summary has settled), estimated on every
	// splitEvery-th transcript; Samples the sweeps summarized.
	SplitHalf float64
	Samples   int
	// Overflow counts the tallies' labels beyond their slots (left uncounted).
	Overflow int
}

// startLabels is the start's labels at a lattice (the same in every layer, the inputs' cell
// labels): each column the label most of the pixels whose centres it holds have (ties to the
// lower), each cell then its piece holding its seed (else its largest); a free cell left with no
// column takes its seed's column when that is the background's or another cell's spare. It
// answers the labels and the free cells that could not be kept (the inputs' indices).
func startLabels(in *Inputs, l Lattice, free func(c int) bool) ([]uint32, []int) {
	r := in.Start
	cols := make([]uint32, l.W*l.H)
	counts := make([]map[uint32]int, l.W)
	row := make([]uint32, r.Width)
	ps := r.PixelSize
	// The pixel columns whose centres lie in the lattice: in.X0 + (px + ½)·ps in [l.X0, l.X0 + W·s).
	px0 := max(0, int(math.Ceil((l.X0-in.X0)/ps-0.5)))
	px1 := min(r.Width, int(math.Ceil((l.X0+mul(float64(l.W), l.VoxelUm)-in.X0)/ps-0.5)))
	for cy := 0; cy < l.H; cy++ {
		for i := range counts {
			counts[i] = map[uint32]int{}
		}
		// The pixel rows whose centres lie in the lattice row.
		y0 := int(math.Ceil((l.Y0+mul(float64(cy), l.VoxelUm)-in.Y0)/ps - 0.5))
		y1 := min(r.Height, int(math.Ceil((l.Y0+mul(float64(cy+1), l.VoxelUm)-in.Y0)/ps-0.5)))
		for py := max(0, y0); py < y1 && px1 > px0; py++ {
			r.RowRange(py, px0, px1, row[:px1-px0])
			for i, label := range row[:px1-px0] {
				cx := int((in.X0 + mul(float64(px0+i), ps) + ps/2 - l.X0) / l.VoxelUm)
				if cx >= 0 && cx < l.W {
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
		x := int(math.Floor((in.SeedX[c] - l.X0) / l.VoxelUm))
		y := int(math.Floor((in.SeedY[c] - l.Y0) / l.VoxelUm))
		if x < 0 || y < 0 || x >= l.W || y >= l.H {
			return -1
		}
		return y*l.W + x
	}
	keepPieces(cols, l.W, l.H, in.Cells, seedCol)
	size := make([]int, in.Cells+1)
	for _, c := range cols {
		size[c]++
	}
	for c := 1; c <= in.Cells; c++ {
		if size[c] > 0 || !free(c-1) {
			continue
		}
		if col := seedCol(c - 1); col >= 0 {
			if holder := cols[col]; holder == 0 || size[holder] > 1 {
				size[holder]--
				cols[col] = uint32(c)
				size[c] = 1
			}
		}
	}
	// Taking a column can split its former holder: keep its pieces again.
	keepPieces(cols, l.W, l.H, in.Cells, seedCol)
	clear(size)
	for _, c := range cols {
		size[c]++
	}
	var lost []int
	for c := 1; c <= in.Cells; c++ {
		if size[c] == 0 && free(c-1) && !in.Box[c-1].Empty() && in.Box[c-1].overlaps(l.extent()) {
			lost = append(lost, c-1)
		}
	}
	labels := make([]uint32, l.Voxels())
	for z := 0; z < l.L; z++ {
		copy(labels[z*l.W*l.H:], cols)
	}
	return labels, lost
}

// extent is the lattice's rectangle, µm.
func (l Lattice) extent() Region {
	return Region{l.X0, l.Y0, l.X0 + mul(float64(l.W), l.VoxelUm), l.Y0 + mul(float64(l.H), l.VoxelUm)}
}

// keepPieces leaves each cell only its 4-connected piece holding its seed column (else its
// largest piece; ties to the first), the rest the background's. seedCol is −1 for a seed
// outside.
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
	seeded := make([]bool, cells+1)
	for c := range keep {
		keep[c] = -1
	}
	for c := 1; c <= cells; c++ {
		if s := seedCol(c - 1); s >= 0 && cols[s] == uint32(c) {
			keep[c], seeded[c] = piece[s], true
		}
	}
	for id, c := range owner {
		if !seeded[c] && (keep[c] < 0 || sizes[id] > sizes[keep[c]]) {
			keep[c] = int32(id)
		}
	}
	for i, c := range cols {
		if c != 0 && piece[i] != keep[c] {
			cols[i] = 0
		}
	}
}

// latticeOf is a stage's lattice over a rectangle (its size in whole coarsest voxels).
func (in *Inputs) latticeOf(ext Region, st Stage, coarsest Stage) Lattice {
	f := coarsest.VoxelUm / st.VoxelUm
	w := int(math.Ceil((ext.X1-ext.X0)/coarsest.VoxelUm)) * int(f)
	h := int(math.Ceil((ext.Y1-ext.Y0)/coarsest.VoxelUm)) * int(f)
	depth := math.Max(in.DepthUm, 1e-3)
	return Lattice{X0: ext.X0, Y0: ext.Y0, Z0: in.Z0, VoxelUm: st.VoxelUm, LayerUm: depth / float64(st.Layers), W: max(1, w), H: max(1, h), L: st.Layers}
}

// RunArea refines what an area covers: each region in turn, with a model of its own.
func RunArea(ctx context.Context, in *Inputs, a Area, opt Options, progress func(float64, string)) ([]*Result, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	regions, err := in.Regions(a)
	if err != nil {
		return nil, err
	}
	var out []*Result
	for i, r := range regions {
		res, err := Run(ctx, in, r, opt, func(f float64, message string) {
			if len(regions) > 1 {
				message = fmt.Sprintf("Region %d of %d: %s", i+1, len(regions), message)
			}
			progress((float64(i)+f)/float64(len(regions)), message)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

// Run refines a region of the inputs (the whole tissue when it is empty or covers it): the cells
// whose seeds lie in it move; the cells about it, in a margin, are held where they are.
func Run(ctx context.Context, in *Inputs, region Region, opt Options, progress func(float64, string)) (*Result, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if err := opt.Check(); err != nil {
		return nil, err
	}
	if in.Cells < 1 || in.Start == nil || len(in.SeedX) != in.Cells || len(in.SeedY) != in.Cells || len(in.Box) != in.Cells {
		return nil, fail(ClassPrior, remedyPrior, "a run needs cells to start from, with a seed and a box for each")
	}
	whole := in.whole()
	if region.Empty() {
		region = whole
	}
	ext := region.grown(marginUm).clipped(whole)
	free := func(c int) bool { return region.Contains(in.SeedX[c], in.SeedY[c]) }
	// The coarsest stage at which every free cell keeps a column; at the finest, those that still
	// cannot are left out.
	start := 0
	var labels []uint32
	var lost []int
	for start = 0; start < len(opt.Stages); start++ {
		labels, lost = startLabels(in, in.latticeOf(ext, opt.Stages[start], opt.Stages[0]), free)
		if len(lost) == 0 || start == len(opt.Stages)-1 {
			break
		}
	}
	// The cells in the lattice, numbered from 1 in the inputs' order.
	present := make([]bool, in.Cells+1)
	for _, c := range labels {
		present[c] = true
	}
	keep := make([]uint32, in.Cells+1)
	var kept []int
	var frozen []bool
	anyFree, anyFrozen := false, false
	for c := 1; c <= in.Cells; c++ {
		if present[c] {
			kept = append(kept, c-1)
			keep[c] = uint32(len(kept))
			f := !free(c - 1)
			frozen = append(frozen, f)
			anyFrozen = anyFrozen || f
			anyFree = anyFree || !f
		}
	}
	if !anyFree {
		return nil, fail(ClassRegion, remedyRegion, "the region (%g, %g)–(%g, %g) µm holds no cell's nucleus", region.X0, region.Y0, region.X1, region.Y1)
	}
	for i, c := range labels {
		labels[i] = keep[c]
	}
	lat := in.latticeOf(ext, opt.Stages[start], opt.Stages[0])
	// The transcripts in the lattice.
	var tr Transcripts
	var index []int32
	t := in.Transcripts
	for i := range t.Gene {
		if lat.Voxel(float64(t.X[i]), float64(t.Y[i]), float64(t.Z[i])) < 0 {
			continue
		}
		tr.X, tr.Y, tr.Z = append(tr.X, t.X[i]), append(tr.Y, t.Y[i]), append(tr.Z, t.Z[i])
		tr.Gene, tr.Prior = append(tr.Gene, t.Gene[i]), append(tr.Prior, keep[t.Prior[i]])
		index = append(index, int32(i))
	}
	if len(index) == 0 {
		return nil, fail(ClassRegion, remedyRegion, "the region (%g, %g)–(%g, %g) µm holds no transcript", region.X0, region.Y0, region.X1, region.Y1)
	}
	s, err := NewState(lat, labels, len(kept), in.Genes, tr, opt.Sampler)
	if err != nil {
		return nil, err
	}
	if anyFrozen {
		s.frozen = frozen
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
	res := &Result{Model: m, Kept: kept, Frozen: frozen, Index: index, Region: region, LeftOut: lost, StartStage: start}
	total := 0
	for _, st := range opt.Stages[start:] {
		total += st.Sweeps
	}
	done := 0
	var voxels, transcripts *tally
	var fromCell []bool
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
					n := (len(s.voxel) + splitEvery - 1) / splitEvery
					halves = [2]*tally{newTally(n), newTally(n)}
				}
				half := halves[0]
				if 2*(k-(st.Sweeps-opt.Samples)) >= opt.Samples {
					half = halves[1]
				}
				for v, c := range s.Labels {
					voxels.add(v, c)
				}
				// Each transcript counts as in its voxel's cell when it is attributed to the cell, as
				// in the background otherwise (its own attribution's draw, given this sweep's state).
				if fromCell == nil {
					fromCell = make([]bool, len(s.voxel))
				}
				s.AttributionsFor(m, fromCell)
				for i, v := range s.voxel {
					l := s.Labels[v]
					if !fromCell[i] {
						l = 0
					}
					transcripts.add(i, l)
					if i%splitEvery == 0 {
						half.add(i/splitEvery, l)
					}
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
		same, n := 0, len(halves[0].counts)
		for j := 0; j < n; j++ {
			a, _ := halves[0].mode(j)
			b, _ := halves[1].mode(j)
			if a == b {
				same++
			}
		}
		res.SplitHalf = float64(same) / float64(max(1, n))
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

// splitEvery: the split-half agreement is estimated on every eighth transcript (a share, its
// standard error about 0.1% on 50,000 of them), so that its two tallies cost an eighth of the
// transcripts' memory.
const splitEvery = 8

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
		posX: s.posX, posY: s.posY, posZ: s.posZ, voxel: make([]int32, n), fromCell: s.fromCell, frozen: s.frozen}
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
