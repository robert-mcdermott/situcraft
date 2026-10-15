package refine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/random"
)

func TestVoxels(t *testing.T) {
	l := Lattice{X0: 10, Y0: 20, Z0: 0, VoxelUm: 2, LayerUm: 5, W: 4, H: 3, L: 2}
	for _, c := range []struct {
		x, y, z float64
		want    int
	}{
		{10, 20, 0, 0},
		{11.9, 20, 0, 0},
		{12, 20, 0, 1},
		{17.99, 25.9, 9.99, l.Index(3, 2, 1)},
		{18, 26, 10, l.Index(3, 2, 1)}, // the far faces belong to the last voxels
		{9.99, 20, 0, -1},
		{18.01, 20, 0, -1},
		{12, 20, 10.01, -1},
		{math.NaN(), 20, 0, -1},
	} {
		if got := l.Voxel(c.x, c.y, c.z); got != c.want {
			t.Errorf("(%g, %g, %g): voxel %d, want %d", c.x, c.y, c.z, got, c.want)
		}
	}
	if l.Voxels() != 24 || l.VoxelVolume() != 20 || l.Volume() != 480 {
		t.Errorf("%d voxels of %g µm³, %g µm³", l.Voxels(), l.VoxelVolume(), l.Volume())
	}
}

func TestNewStateRefuses(t *testing.T) {
	l := Lattice{VoxelUm: 1, LayerUm: 1, W: 2, H: 2, L: 1}
	one := Transcripts{X: []float32{0.5}, Y: []float32{0.5}, Z: []float32{0.5}, Gene: []uint32{0}, Prior: []uint32{0}}
	for _, c := range []struct {
		name   string
		labels []uint32
		cells  int
		t      Transcripts
		want   string
	}{
		{"labels of the wrong length", []uint32{1, 1, 2}, 2, one, "labels for"},
		{"a label beyond the cells", []uint32{1, 3, 0, 0}, 2, one, "of 2 cells"},
		{"a cell without a voxel", []uint32{1, 1, 0, 0}, 2, one, "cell 2 has no voxel"},
		{"a transcript outside", []uint32{1, 2, 0, 0}, 2, Transcripts{X: []float32{3}, Y: []float32{0}, Z: []float32{0}, Gene: []uint32{0}, Prior: []uint32{0}}, "outside the region"},
		{"a gene beyond the panel", []uint32{1, 2, 0, 0}, 2, Transcripts{X: []float32{0}, Y: []float32{0}, Z: []float32{0}, Gene: []uint32{5}, Prior: []uint32{0}}, "of gene 5"},
		{"a prior cell beyond the cells", []uint32{1, 2, 0, 0}, 2, Transcripts{X: []float32{0}, Y: []float32{0}, Z: []float32{0}, Gene: []uint32{0}, Prior: []uint32{4}}, "prior cell 4"},
	} {
		if _, err := NewState(l, c.labels, c.cells, 3, c.t, DefaultSettings()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// randomState is a lattice of 9 × 8 × 2 voxels of 1 × 1 × 3 µm with 6 cells and 400 transcripts
// of 4 genes, a third over nuclei, and a model fitted to it for a few sweeps.
func randomState(t *testing.T, seed uint64) (*State, *Model) {
	t.Helper()
	l := Lattice{X0: 5, Y0: 5, VoxelUm: 1, LayerUm: 3, W: 9, H: 8, L: 2}
	s := random.New(seed, 1)
	const cells, genes, n = 6, 4, 400
	// Cells about random centres (each voxel to the nearest, within 3 voxels; the background
	// beyond), the same in both layers: connected pieces.
	labels := make([]uint32, l.Voxels())
	cx, cy := make([]int, cells), make([]int, cells)
	taken := map[[2]int]bool{}
	for c := range cx {
		for {
			cx[c], cy[c] = s.IntN(l.W), s.IntN(l.H)
			if !taken[[2]int{cx[c], cy[c]}] {
				taken[[2]int{cx[c], cy[c]}] = true
				break
			}
		}
	}
	for z := 0; z < l.L; z++ {
		for y := 0; y < l.H; y++ {
			for x := 0; x < l.W; x++ {
				best, bestD := 0, 1<<30
				for c := range cx {
					if d := (x-cx[c])*(x-cx[c]) + (y-cy[c])*(y-cy[c]); d < bestD {
						best, bestD = c+1, d
					}
				}
				if bestD <= 9 {
					labels[l.Index(x, y, z)] = uint32(best)
				}
			}
		}
	}
	tr := Transcripts{}
	for i := 0; i < n; i++ {
		tr.X = append(tr.X, float32(5+9*s.Float64()))
		tr.Y = append(tr.Y, float32(5+8*s.Float64()))
		tr.Z = append(tr.Z, float32(6*s.Float64()))
		tr.Gene = append(tr.Gene, uint32(s.IntN(genes)))
		prior := uint32(0)
		if s.Float64() < 0.33 {
			prior = uint32(1 + s.IntN(cells))
		}
		tr.Prior = append(tr.Prior, prior)
	}
	st, err := NewState(l, labels, cells, genes, tr, DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	h := DefaultHyper()
	h.Components = 3
	m, err := NewModel(h, st.Volumes(), genes, l.Volume(), seed)
	if err != nil {
		t.Fatal(err)
	}
	st.AttributeByPlace(m)
	m.Init()
	for i := 0; i < 5; i++ {
		st.Attribute(m)
		m.SweepParameters()
	}
	return st, m
}

// Every voxel move changes the target by what MoveDelta says: a random walk of moves, each
// checked against the target computed afresh.
func TestMovesChangeTheTargetAsComputed(t *testing.T) {
	st, m := randomState(t, 3)
	s := random.New(4, 1)
	before := st.LogTarget(m)
	moved, refused := 0, 0
	for i := 0; i < 2000; i++ {
		v := s.IntN(st.Lat.Voxels())
		b := uint32(s.IntN(st.Cells + 1))
		d := st.MoveDelta(m, v, b)
		if math.IsInf(d, -1) {
			if a := st.Labels[v]; a == 0 || st.cellVoxels[a-1] != 1 {
				t.Fatalf("voxel %d's move refused, but it is not its cell's last", v)
			}
			refused++
			continue
		}
		st.Move(m, v, b)
		after := st.LogTarget(m)
		if math.Abs(after-before-d) > 1e-9*math.Max(1, math.Abs(before)) {
			t.Fatalf("move %d (voxel %d to %d): the target changed by %.12g, MoveDelta said %.12g", i, v, b, after-before, d)
		}
		before = after
		moved++
	}
	// The volumes the model holds follow the moves.
	for c, v := range st.Volumes() {
		if m.Volume[c] != v {
			t.Errorf("cell %d: the model's volume %g, the lattice's %g", c+1, m.Volume[c], v)
		}
	}
	if moved < 1000 {
		t.Errorf("only %d moves (%d refused)", moved, refused)
	}
}

// A transcript in a cell is attributed to it with probability λ / (λ₀ + λ); the counts follow
// the attributions.
func TestAttributionsAreTheirShares(t *testing.T) {
	l := Lattice{VoxelUm: 10, LayerUm: 10, W: 2, H: 1, L: 1}
	const n = 20000
	tr := Transcripts{}
	for i := 0; i < n; i++ {
		x := float32(i%2*10) + 5 // half in the cell, half in the background
		tr.X, tr.Y, tr.Z = append(tr.X, x), append(tr.Y, 5), append(tr.Z, 5)
		tr.Gene, tr.Prior = append(tr.Gene, uint32(i%4/2)), append(tr.Prior, 0)
	}
	st, err := NewState(l, []uint32{1, 0}, 1, 2, tr, DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	h := DefaultHyper()
	h.Components = 1
	m, err := NewModel(h, st.Volumes(), 2, l.Volume(), 9)
	if err != nil {
		t.Fatal(err)
	}
	copy(m.Rate, []float64{0.03, 0.002})
	copy(m.Bg, []float64{0.01, 0.006})
	st.Attribute(m)
	inCell := []int{n / 4, n / 4} // per gene, transcripts in the cell
	for g, want := range []float64{0.03 / 0.04, 0.002 / 0.008} {
		got := float64(m.Counts[g])
		sd := math.Sqrt(float64(inCell[g]) * want * (1 - want))
		if z := (got - float64(inCell[g])*want) / sd; math.Abs(z) > 3.29 {
			t.Errorf("gene %d: %v of %d attributed to the cell, expected %.1f (z %.2f)", g, got, inCell[g], float64(inCell[g])*want, z)
		}
	}
	if total := m.Counts[0] + m.Counts[1] + m.BgCounts[0] + m.BgCounts[1]; total != n {
		t.Errorf("%d transcripts counted, of %d", total, n)
	}
	if m.Volume[0] != 1000 {
		t.Errorf("the cell's volume %g", m.Volume[0])
	}
}

// The attributions are the same on any number of workers.
func TestAttributionsAreTheSameOnAnyWorkers(t *testing.T) {
	a, ma := randomState(t, 5)
	b, mb := randomState(t, 5)
	ma.Workers, mb.Workers = 1, 7
	a.Attribute(ma)
	b.Attribute(mb)
	for i := range a.fromCell {
		if a.fromCell[i] != b.fromCell[i] {
			t.Fatalf("transcript %d's attribution differs", i)
		}
	}
}

// Sweeps are the same bits on every processor: a checksum of every parameter after twenty sweeps
// of attributions and parameters, and of a move's change, pinned (the same on arm64 and amd64).
func TestSweepsAreTheSameOnEveryProcessor(t *testing.T) {
	st, m := randomState(t, 13)
	for i := 0; i < 20; i++ {
		st.Attribute(m)
		m.SweepParameters()
	}
	h := sha256.New()
	put := func(v ...float64) {
		for _, x := range v {
			binary.Write(h, binary.LittleEndian, math.Float64bits(x))
		}
	}
	put(m.Rate...)
	put(m.Shape...)
	put(m.RateParam...)
	put(m.Scale...)
	put(m.Pi...)
	put(m.VolMean...)
	put(m.VolVar...)
	put(m.Bg...)
	for _, k := range m.Comp {
		put(float64(k))
	}
	put(st.LogTarget(m), st.MoveDelta(m, 17, 2))
	const pinned = "92aff03a9506b9c9"
	if got := hex.EncodeToString(h.Sum(nil))[:16]; got != pinned {
		t.Errorf("the sweeps' checksum is %s, not %s (on %s)", got, pinned, runtime.GOARCH)
	}
}
