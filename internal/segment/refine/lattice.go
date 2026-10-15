package refine

// The labelling: a lattice of voxels over the region, each voxel's cell (0: the background), the
// transcripts in their voxels with their prior cells, and each transcript's attribution to its
// cell or the background. From it come the model's statistics (the cells' volumes and attributed
// counts), the log-target the sampler moves on (the likelihood with the attributions integrated
// out, the volume prior and the nuclear prior; research.md §3.2) and the change a voxel's move
// makes to it.

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"situcraft/internal/dmath"
)

// Lattice is a grid of voxels over a region: its corner (µm), a voxel's side in x and y and its
// thickness in z (µm), and its size in voxels.
type Lattice struct {
	X0, Y0, Z0       float64
	VoxelUm, LayerUm float64
	W, H, L          int
}

// Voxels is the number of voxels.
func (l Lattice) Voxels() int { return l.W * l.H * l.L }

// VoxelVolume is a voxel's volume (µm³).
func (l Lattice) VoxelVolume() float64 { return mul(mul(l.VoxelUm, l.VoxelUm), l.LayerUm) }

// Volume is the region's volume (µm³).
func (l Lattice) Volume() float64 { return mul(float64(l.Voxels()), l.VoxelVolume()) }

// Index is the voxel at (x, y, z), counted in x, then y, then z.
func (l Lattice) Index(x, y, z int) int { return (z*l.H+y)*l.W + x }

// axis is the cell of a coordinate along an axis of n cells of a size from an origin; a point on
// the far face belongs to the last cell; −1 outside.
func axis(v, origin, size float64, n int) int {
	f := (v - origin) / size
	i := int(math.Floor(f))
	if i == n && f == float64(n) {
		i = n - 1
	}
	if i < 0 || i >= n || math.IsNaN(f) {
		return -1
	}
	return i
}

// Voxel is the voxel holding a point (µm), or −1 outside the region.
func (l Lattice) Voxel(x, y, z float64) int {
	ix, iy, iz := axis(x, l.X0, l.VoxelUm, l.W), axis(y, l.Y0, l.VoxelUm, l.H), axis(z, l.Z0, l.LayerUm, l.L)
	if ix < 0 || iy < 0 || iz < 0 {
		return -1
	}
	return l.Index(ix, iy, iz)
}

// Transcripts are the molecules a labelling is fitted to: positions (µm), genes (0 to genes − 1)
// and prior cells (the cell whose nucleus each lies over; 0 none).
type Transcripts struct {
	X, Y, Z []float32
	Gene    []uint32
	Prior   []uint32
}

// State is a labelling of a lattice with its transcripts.
type State struct {
	Lat    Lattice
	Labels []uint32
	Cells  int
	Genes  int

	cellVoxels []int
	gene       []uint32
	voxel      []int32
	prior      []uint32
	// fromCell is each transcript's attribution: to its voxel's cell (true) or the background.
	fromCell []bool
	// The transcripts of voxel v are order[start[v]:start[v+1]].
	start []int32
	order []int32
}

// NewState places the transcripts on a labelled lattice (labels: each voxel's cell, 1 to cells,
// or 0). Every cell must have a voxel and every transcript lie in the region.
func NewState(lat Lattice, labels []uint32, cells, genes int, t Transcripts) (*State, error) {
	if lat.W < 1 || lat.H < 1 || lat.L < 1 || !(lat.VoxelUm > 0) || !(lat.LayerUm > 0) {
		return nil, errors.New("refine: a lattice of at least one voxel, of positive size")
	}
	if len(labels) != lat.Voxels() {
		return nil, fmt.Errorf("refine: %d labels for %d voxels", len(labels), lat.Voxels())
	}
	n := len(t.Gene)
	if len(t.X) != n || len(t.Y) != n || len(t.Z) != n || len(t.Prior) != n {
		return nil, errors.New("refine: the transcripts' columns differ in length")
	}
	s := &State{Lat: lat, Labels: append([]uint32(nil), labels...), Cells: cells, Genes: genes,
		cellVoxels: make([]int, cells), gene: append([]uint32(nil), t.Gene...), prior: append([]uint32(nil), t.Prior...),
		voxel: make([]int32, n), fromCell: make([]bool, n)}
	for _, l := range labels {
		if int(l) > cells {
			return nil, fmt.Errorf("refine: a voxel of cell %d, of %d cells", l, cells)
		}
		if l > 0 {
			s.cellVoxels[l-1]++
		}
	}
	for c, v := range s.cellVoxels {
		if v == 0 {
			return nil, fmt.Errorf("refine: cell %d has no voxel", c+1)
		}
	}
	count := make([]int32, lat.Voxels()+1)
	for i := 0; i < n; i++ {
		if int(t.Gene[i]) >= genes {
			return nil, fmt.Errorf("refine: transcript %d is of gene %d, of %d genes", i, t.Gene[i], genes)
		}
		if int(t.Prior[i]) > cells {
			return nil, fmt.Errorf("refine: transcript %d's prior cell %d, of %d cells", i, t.Prior[i], cells)
		}
		v := lat.Voxel(float64(t.X[i]), float64(t.Y[i]), float64(t.Z[i]))
		if v < 0 {
			return nil, fmt.Errorf("refine: transcript %d at (%g, %g, %g) µm is outside the region", i, t.X[i], t.Y[i], t.Z[i])
		}
		s.voxel[i] = int32(v)
		count[v+1]++
	}
	for v := 1; v < len(count); v++ {
		count[v] += count[v-1]
	}
	s.start = count
	s.order = make([]int32, n)
	next := append([]int32(nil), count[:lat.Voxels()]...)
	for i := 0; i < n; i++ {
		v := s.voxel[i]
		s.order[next[v]] = int32(i)
		next[v]++
	}
	return s, nil
}

// Volumes are the cells' volumes (µm³).
func (s *State) Volumes() []float64 {
	nu := s.Lat.VoxelVolume()
	out := make([]float64, s.Cells)
	for c, n := range s.cellVoxels {
		out[c] = mul(float64(n), nu)
	}
	return out
}

// attributeChunk is how many transcripts share a random stream in the attributions.
const attributeChunk = 4096

// Attribute draws each transcript's attribution (research.md §3.2, step 1) and sets the model's
// statistics from the labelling: the cells' volumes, their attributed counts and the background's.
func (s *State) Attribute(m *Model) {
	g := m.Genes
	n := len(s.gene)
	chunks := (n + attributeChunk - 1) / attributeChunk
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < max(1, m.Workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range next {
				r := m.stream(stepAttribute, chunk)
				for i := chunk * attributeChunk; i < min(n, (chunk+1)*attributeChunk); i++ {
					c := s.Labels[s.voxel[i]]
					if c == 0 {
						s.fromCell[i] = false
						continue
					}
					l := m.Rate[int(c-1)*g+int(s.gene[i])]
					s.fromCell[i] = mul(r.Float64(), m.Bg[s.gene[i]]+l) < l
				}
			}
		}()
	}
	for chunk := 0; chunk < chunks; chunk++ {
		next <- chunk
	}
	close(next)
	wg.Wait()
	clear(m.Counts)
	clear(m.BgCounts)
	for i, own := range s.fromCell {
		if own {
			m.Counts[int(s.Labels[s.voxel[i]]-1)*g+int(s.gene[i])]++
		} else {
			m.BgCounts[s.gene[i]]++
		}
	}
	copy(m.Volume, s.Volumes())
}

// intensity is the rate of gene j at a voxel of cell c (0: the background): λ₀ + λ_c.
func intensity(m *Model, c uint32, j uint32) float64 {
	if c == 0 {
		return m.Bg[j]
	}
	return m.Bg[j] + m.Rate[int(c-1)*m.Genes+int(j)]
}

// LogLikelihood is the transcripts' log-likelihood given the labelling and the rates, their
// attributions integrated out: Σ_i log(λ₀ + λ_σ(i)) − |R| Σ λ₀ − Σ_c V_c Λ_c.
func (s *State) LogLikelihood(m *Model) float64 {
	var ll float64
	for i, v := range s.voxel {
		ll += dmath.Log(intensity(m, s.Labels[v], s.gene[i]))
	}
	var bg float64
	for _, l := range m.Bg {
		bg += l
	}
	ll -= mul(s.Lat.Volume(), bg)
	for c, v := range s.Volumes() {
		ll -= mul(v, m.RateSum[c])
	}
	return ll
}

// LogLabelPrior is the labelling's log prior, but for its constraints: each cell's volume under
// its component's log-normal, and each transcript over a nucleus weighed by ρ in its prior cell,
// 1 − ρ elsewhere.
func (s *State) LogLabelPrior(m *Model) float64 {
	var lp float64
	for c, v := range s.Volumes() {
		k := m.Comp[c]
		lp += logLogNormal(v, m.VolMean[k], m.VolVar[k])
	}
	in, out := dmath.Log(m.H.NuclearPrior), dmath.Log(1-m.H.NuclearPrior)
	for i, q := range s.prior {
		switch {
		case q == 0:
		case s.Labels[s.voxel[i]] == q:
			lp += in
		default:
			lp += out
		}
	}
	return lp
}

// LogTarget is what the sampler moves on: the log-likelihood and the labelling's log prior.
func (s *State) LogTarget(m *Model) float64 { return s.LogLikelihood(m) + s.LogLabelPrior(m) }

// MoveDelta is the change in LogTarget if voxel v moved to cell b (0: the background); −∞ if the
// move would empty a cell.
func (s *State) MoveDelta(m *Model, v int, b uint32) float64 {
	a := s.Labels[v]
	if a == b {
		return 0
	}
	if a > 0 && s.cellVoxels[a-1] == 1 {
		return math.Inf(-1)
	}
	nu := s.Lat.VoxelVolume()
	logIn := dmath.Log(m.H.NuclearPrior) - dmath.Log(1-m.H.NuclearPrior)
	var d float64
	for _, i := range s.order[s.start[v]:s.start[v+1]] {
		j := s.gene[i]
		d += dmath.Log(intensity(m, b, j)) - dmath.Log(intensity(m, a, j))
		switch q := s.prior[i]; {
		case q == 0:
		case q == b:
			d += logIn
		case q == a:
			d -= logIn
		}
	}
	// Volumes as Volumes computes them, so that the change is the target's to the last bit.
	if a > 0 {
		k, n := m.Comp[a-1], float64(s.cellVoxels[a-1])
		d += mul(nu, m.RateSum[a-1])
		d += logLogNormal(mul(n-1, nu), m.VolMean[k], m.VolVar[k]) - logLogNormal(mul(n, nu), m.VolMean[k], m.VolVar[k])
	}
	if b > 0 {
		k, n := m.Comp[b-1], float64(s.cellVoxels[b-1])
		d -= mul(nu, m.RateSum[b-1])
		d += logLogNormal(mul(n+1, nu), m.VolMean[k], m.VolVar[k]) - logLogNormal(mul(n, nu), m.VolMean[k], m.VolVar[k])
	}
	return d
}

// Move gives voxel v to cell b (0: the background), keeping the model's volumes.
func (s *State) Move(m *Model, v int, b uint32) {
	a := s.Labels[v]
	if a == b {
		return
	}
	nu := s.Lat.VoxelVolume()
	if a > 0 {
		s.cellVoxels[a-1]--
		m.Volume[a-1] = mul(float64(s.cellVoxels[a-1]), nu)
	}
	if b > 0 {
		s.cellVoxels[b-1]++
		m.Volume[b-1] = mul(float64(s.cellVoxels[b-1]), nu)
	}
	s.Labels[v] = b
}
