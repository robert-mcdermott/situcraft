package spatial

import (
	"context"
	"math"
	"testing"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
	"situcraft/internal/segment"
)

// rookLattice is a side × side grid of cells 1 µm apart, joined to their four nearest (a rook's moves).
func rookLattice(t *testing.T, side int) *Graph {
	t.Helper()
	var in Inputs
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			in.X, in.Y = append(in.X, float32(x)), append(in.Y, float32(y))
		}
	}
	g, err := Build(context.Background(), in, Params{Kind: Radius, Radius: 1.01}, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func moran(t *testing.T, g *Graph, values []float32, genes int, p MoranParams, workers int) *Moran {
	t.Helper()
	m, err := MoransI(context.Background(), g, values, genes, p, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// By hand on a 20 × 20 lattice: a checkerboard's I is −1, with either weights; two halves' is
// 1 − 2c/S0 (c the edges between the halves, counted both ways); a constant gene has none.
func TestMoransIByHand(t *testing.T) {
	g := rookLattice(t, 20)
	n := 400
	values := make([]float32, 3*n)
	for i := 0; i < n; i++ {
		x, y := i%20, i/20
		values[i] = float32((x + y) % 2)
		if x < 10 {
			values[n+i] = 1
		}
		values[2*n+i] = 3
	}
	for _, weights := range []string{"binary", "row"} {
		p := MoranParams{Weights: weights}
		m := moran(t, g, values, 3, p, 2)
		if math.Abs(m.I[0]+1) > 1e-12 {
			t.Errorf("%s weights: a checkerboard's I is %.15g", weights, m.I[0])
		}
		if !math.IsNaN(m.I[2]) || !math.IsNaN(m.PAdj[2]) {
			t.Errorf("%s weights: a constant gene's I %g", weights, m.I[2])
		}
		if math.Abs(m.Expected+1.0/399) > 1e-15 {
			t.Errorf("the expectation %g", m.Expected)
		}
	}
	m := moran(t, g, values, 3, MoranParams{Weights: "binary"}, 2)
	S0 := 2.0 * 2 * 20 * 19
	if want := 1 - 2*40/S0; math.Abs(m.I[1]-want) > 1e-12 || m.S0 != S0 {
		t.Errorf("two halves: I %.15g, not %.15g (S0 %g)", m.I[1], want, m.S0)
	}
	if !(m.P[1] < 1e-100) || !(m.P[0] < 1e-100) {
		t.Errorf("p-values %g and %g", m.P[0], m.P[1])
	}
}

// Under randomization the variance is the exact variance of I over every permutation: 9,999
// permutations' variance within 4% of it, their mean within its error of the expectation, for
// skewed, binary and normal values, with either weights; normal values' variance near the
// normality variance. The same on one worker and on eight.
func TestMoransVarianceUnderRandomization(t *testing.T) {
	g := rookLattice(t, 7)
	n := 49
	s := random.New(13, 4)
	values := make([]float32, 3*n)
	for i := 0; i < n; i++ {
		values[i] = float32(s.Exponential())
		values[n+i] = float32(s.IntN(2))
		values[2*n+i] = float32(s.Normal())
	}
	for _, weights := range []string{"binary", "row"} {
		p := MoranParams{Weights: weights, Permutations: 9999, Seed: 3}
		m := moran(t, g, values, 3, p, 8)
		for gene := 0; gene < 3; gene++ {
			ratio := m.PermSD[gene] * m.PermSD[gene] / m.VarRand[gene]
			se := math.Sqrt(m.VarRand[gene] / 9999)
			t.Logf("%s weights, gene %d: permutations' variance %.5g, under randomization %.5g (%.3f); under normality %.5g", weights, gene, m.PermSD[gene]*m.PermSD[gene], m.VarRand[gene], ratio, m.VarNorm[gene])
			if math.Abs(ratio-1) > 0.04 || math.Abs(m.PermMean[gene]-m.Expected) > 4*se {
				t.Errorf("%s weights, gene %d: the permutations' mean %g and variance %g; expected %g and %g", weights, gene, m.PermMean[gene], m.PermSD[gene]*m.PermSD[gene], m.Expected, m.VarRand[gene])
			}
		}
		one := moran(t, g, values, 3, p, 1)
		for gene := 0; gene < 3; gene++ {
			if one.Perm[gene] != m.Perm[gene] || one.PermSD[gene] != m.PermSD[gene] {
				t.Fatalf("%s weights: the permutations differ on one worker", weights)
			}
		}
	}
	// Normal values: kurtosis near 3, the two variances alike.
	big := rookLattice(t, 40)
	nv := make([]float32, 1600)
	for i := range nv {
		nv[i] = float32(s.Normal())
	}
	m := moran(t, big, nv, 1, MoranParams{Weights: "row"}, 2)
	if r := m.VarRand[0] / m.VarNorm[0]; math.Abs(r-1) > 0.03 {
		t.Errorf("normal values: the variances' ratio %g", r)
	}
}

// Local Moran's I: the cells' values sum to S0 times the global I; a block of high values on a
// lattice of noise is found high among high, and the noise away from it is not.
func TestLocalMoran(t *testing.T) {
	g := rookLattice(t, 30)
	n := 900
	s := random.New(17, 5)
	values := make([]float32, n)
	for i := range values {
		x, y := i%30, i/30
		values[i] = float32(s.Normal())
		if x >= 5 && x < 12 && y >= 5 && y < 12 {
			values[i] += 4
		}
	}
	p := MoranParams{Weights: "row", Local: 1, LocalPermutations: 199, Seed: 2}
	m := moran(t, g, values, 1, p, 4)
	if len(m.Local) != 1 {
		t.Fatalf("%d local results", len(m.Local))
	}
	sum := 0.0
	for _, v := range m.Local[0].I {
		sum += float64(v)
	}
	if math.Abs(sum-m.S0*m.I[0]) > 1e-3*math.Abs(sum) {
		t.Errorf("the local I sum to %g, not S0 × I = %g", sum, m.S0*m.I[0])
	}
	inner, high, far, flagged := 0, 0, 0, 0
	for i := 0; i < n; i++ {
		x, y := i%30, i/30
		if x >= 6 && x < 11 && y >= 6 && y < 11 {
			inner++
			if m.Local[0].Class[i] == LocalHighHigh {
				high++
			}
		}
		if x >= 18 && y >= 18 {
			far++
			if m.Local[0].Class[i] == LocalHighHigh {
				flagged++
			}
		}
	}
	if high < inner*9/10 || flagged > far/10 {
		t.Errorf("the block's inner cells high among high %d of %d; the noise's %d of %d", high, inner, flagged, far)
	}
	again := moran(t, g, values, 1, p, 1)
	for i := range again.Local[0].P {
		if again.Local[0].P[i] != m.Local[0].P[i] {
			t.Fatal("local Moran's I differs on one worker")
		}
	}
}

// Normalized expression by hand: each cell's counts scaled to the median total of the cells with a
// transcript, log1p; a cell without one zeros.
func TestNormalizedExpression(t *testing.T) {
	c := &segment.Counts{Genes: []string{"A", "B"}, Offsets: []uint32{0, 2, 3, 3, 4}, Gene: []uint32{0, 1, 1, 0}, Count: []uint32{4, 6, 30, 20}}
	v, target, empty, err := NormalizedExpression(c, []uint32{0, 1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if target != 20 || empty != 1 {
		t.Fatalf("target %g, %d without a transcript", target, empty)
	}
	want := []float64{dmath.Log(1 + 4/(10/20.0)), 0, 0, dmath.Log(1 + 20/(20/20.0)), dmath.Log(1 + 6/(10/20.0)), dmath.Log(1 + 30/(30/20.0)), 0, 0}
	for k := range want {
		if float64(v[k]) != float64(float32(want[k])) {
			t.Errorf("value %d: %g, not %g", k, v[k], want[k])
		}
	}
}
