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
	Lat      Lattice
	Labels   []uint32
	Cells    int
	Genes    int
	Settings Settings

	cellVoxels []int
	gene       []uint32
	prior      []uint32
	// Each transcript's observed place (µm), its place now (repositioning moves it in x and y),
	// and its voxel.
	obsX, obsY []float32
	posX, posY []float64
	posZ       []float32
	voxel      []int32
	// fromCell is each transcript's attribution: to its voxel's cell (true) or the background.
	fromCell []bool
	// The transcripts of voxel v are order[start[v]:start[v+1]].
	start []int32
	order []int32
	// Each cell's voxels and perimeter (voxel edges) in each layer (c*L + z), and the factor of its
	// perimeter bound.
	area, perim []int32
	tolerance   []float64
	// frozen are the cells held where they are (nil: none): the cells about a region, whose seeds
	// lie outside it. No move takes a voxel from a frozen cell or gives it one.
	frozen []bool
}

// NewState places the transcripts on a labelled lattice (labels: each voxel's cell, 1 to cells,
// or 0). Every cell must have a voxel and be connected, and every transcript lie in the region.
// Each cell's perimeter bound is the settings' factor, or its own if it starts beyond it.
func NewState(lat Lattice, labels []uint32, cells, genes int, t Transcripts, settings Settings) (*State, error) {
	if lat.W < 1 || lat.H < 1 || lat.L < 1 || !(lat.VoxelUm > 0) || !(lat.LayerUm > 0) {
		return nil, errors.New("refine: a lattice of at least one voxel, of positive size")
	}
	if len(labels) != lat.Voxels() {
		return nil, fmt.Errorf("refine: %d labels for %d voxels", len(labels), lat.Voxels())
	}
	if err := settings.Check(); err != nil {
		return nil, err
	}
	n := len(t.Gene)
	if len(t.X) != n || len(t.Y) != n || len(t.Z) != n || len(t.Prior) != n {
		return nil, errors.New("refine: the transcripts' columns differ in length")
	}
	s := &State{Lat: lat, Labels: append([]uint32(nil), labels...), Cells: cells, Genes: genes, Settings: settings,
		cellVoxels: make([]int, cells), gene: append([]uint32(nil), t.Gene...), prior: append([]uint32(nil), t.Prior...),
		obsX: append([]float32(nil), t.X...), obsY: append([]float32(nil), t.Y...), posZ: append([]float32(nil), t.Z...),
		posX: make([]float64, n), posY: make([]float64, n), voxel: make([]int32, n), fromCell: make([]bool, n)}
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
	if c := s.disconnected(); c > 0 {
		return nil, fmt.Errorf("refine: cell %d is in pieces", c)
	}
	for i := 0; i < n; i++ {
		if int(t.Gene[i]) >= genes {
			return nil, fmt.Errorf("refine: transcript %d is of gene %d, of %d genes", i, t.Gene[i], genes)
		}
		if int(t.Prior[i]) > cells {
			return nil, fmt.Errorf("refine: transcript %d's prior cell %d, of %d cells", i, t.Prior[i], cells)
		}
		s.posX[i], s.posY[i] = float64(t.X[i]), float64(t.Y[i])
		v := lat.Voxel(s.posX[i], s.posY[i], float64(t.Z[i]))
		if v < 0 {
			return nil, fmt.Errorf("refine: transcript %d at (%g, %g, %g) µm is outside the region", i, t.X[i], t.Y[i], t.Z[i])
		}
		s.voxel[i] = int32(v)
	}
	s.reindex()
	s.measure()
	return s, nil
}

// setLabels replaces the labelling, recounting the cells' voxels, areas and perimeters (and the
// model's volumes when m is not nil) but keeping their perimeter bounds.
func (s *State) setLabels(labels []uint32, m *Model) {
	copy(s.Labels, labels)
	clear(s.cellVoxels)
	for _, c := range labels {
		if c > 0 {
			s.cellVoxels[c-1]++
		}
	}
	tolerance := s.tolerance
	s.measure()
	s.tolerance = tolerance
	if m != nil {
		copy(m.Volume, s.Volumes())
	}
}

// reindex lists the transcripts by voxel.
func (s *State) reindex() {
	count := make([]int32, s.Lat.Voxels()+1)
	for _, v := range s.voxel {
		count[v+1]++
	}
	for v := 1; v < len(count); v++ {
		count[v] += count[v-1]
	}
	s.start = count
	if len(s.order) != len(s.voxel) {
		s.order = make([]int32, len(s.voxel))
	}
	next := append([]int32(nil), count[:s.Lat.Voxels()]...)
	for i, v := range s.voxel {
		s.order[next[v]] = int32(i)
		next[v]++
	}
}

// measure counts each cell's voxels and perimeter in each layer, and sets each cell's perimeter
// bound: the settings' factor, or the cell's own largest ratio if it starts beyond it.
func (s *State) measure() {
	l := s.Lat
	s.area = make([]int32, s.Cells*l.L)
	s.perim = make([]int32, s.Cells*l.L)
	for z := 0; z < l.L; z++ {
		for y := 0; y < l.H; y++ {
			for x := 0; x < l.W; x++ {
				c := s.Labels[l.Index(x, y, z)]
				if c == 0 {
					continue
				}
				i := int(c-1)*l.L + z
				s.area[i]++
				for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					nx, ny := x+d[0], y+d[1]
					if nx < 0 || ny < 0 || nx >= l.W || ny >= l.H || s.Labels[l.Index(nx, ny, z)] != c {
						s.perim[i]++
					}
				}
			}
		}
	}
	s.tolerance = make([]float64, s.Cells)
	for c := 0; c < s.Cells; c++ {
		f := s.Settings.PerimeterFactor
		for z := 0; z < l.L; z++ {
			if a := s.area[c*l.L+z]; a > 0 {
				f = math.Max(f, float64(s.perim[c*l.L+z])/float64(minPerimeter(int(a))))
			}
		}
		s.tolerance[c] = f
	}
}

// disconnected is a cell in more than one face-connected piece (0: none).
func (s *State) disconnected() uint32 {
	l := s.Lat
	seen := make([]bool, l.Voxels())
	pieces := make([]int, s.Cells+1)
	var stack []int
	for v0, c := range s.Labels {
		if c == 0 || seen[v0] {
			continue
		}
		pieces[c]++
		if pieces[c] > 1 {
			return c
		}
		stack = append(stack[:0], v0)
		seen[v0] = true
		var nb [6]int
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, u := range nb[:l.faceNeighbors(v, &nb)] {
				if !seen[u] && s.Labels[u] == c {
					seen[u] = true
					stack = append(stack, u)
				}
			}
		}
	}
	return 0
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
	s.count(m)
}

// AttributionsFor draws each transcript's attribution (to its voxel's cell, or the background)
// from its conditional given the state and the model's rates, into out, without changing the
// state or the model: what a summarized sweep counts each transcript as. It draws from a stream of
// its own (stepSummary), so the chain is the same with it or without.
func (s *State) AttributionsFor(m *Model, out []bool) {
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
				r := m.stream(stepSummary, chunk)
				for i := chunk * attributeChunk; i < min(n, (chunk+1)*attributeChunk); i++ {
					c := s.Labels[s.voxel[i]]
					if c == 0 {
						out[i] = false
						continue
					}
					l := m.Rate[int(c-1)*g+int(s.gene[i])]
					out[i] = mul(r.Float64(), m.Bg[s.gene[i]]+l) < l
				}
			}
		}()
	}
	for chunk := 0; chunk < chunks; chunk++ {
		next <- chunk
	}
	close(next)
	wg.Wait()
}

// AttributeByPlace attributes every transcript in a cell to it and the rest to the background,
// and sets the model's statistics: the start, before there are rates to draw attributions from.
func (s *State) AttributeByPlace(m *Model) {
	for i, v := range s.voxel {
		s.fromCell[i] = s.Labels[v] != 0
	}
	s.count(m)
}

// count sets the model's statistics from the attributions and the labelling.
func (s *State) count(m *Model) {
	g := m.Genes
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

// Move gives voxel v to cell b (0: the background), keeping the cells' areas and perimeters
// and the model's volumes.
func (s *State) Move(m *Model, v int, b uint32) {
	a := s.Labels[v]
	if a == b {
		return
	}
	z := v / (s.Lat.W * s.Lat.H)
	na, nb := s.layerNeighbors(v, a), s.layerNeighbors(v, b)
	if a > 0 {
		i := int(a-1)*s.Lat.L + z
		s.area[i]--
		s.perim[i] += int32(2*na - 4)
	}
	if b > 0 {
		i := int(b-1)*s.Lat.L + z
		s.area[i]++
		s.perim[i] += int32(4 - 2*nb)
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
