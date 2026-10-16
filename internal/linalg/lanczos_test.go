package linalg

import (
	"math"
	"math/rand/v2"
	"testing"
)

// covariance makes the n × n covariance of rows random observations whose variances fall off as
// a spectrum does (the scale of coordinate g is 1/(1 + g/4)), and the product with it.
func covariance(n, rows int, seed uint64) ([]float64, func(x, y []float64)) {
	r := rand.New(rand.NewPCG(seed, 1))
	x := make([]float64, rows*n)
	for i := range x {
		g := i % n
		x[i] = r.NormFloat64() / (1 + float64(g)/4)
	}
	// Mix the coordinates so that the eigenvectors are not the axes.
	m := make([]float64, n*n)
	for i := range m {
		m[i] = r.NormFloat64()
	}
	y := make([]float64, rows*n)
	for o := 0; o < rows; o++ {
		for c := 0; c < n; c++ {
			s := 0.0
			for g := 0; g < n; g++ {
				s += x[o*n+g] * m[g*n+c]
			}
			y[o*n+c] = s / math.Sqrt(float64(n))
		}
	}
	a := make([]float64, n*n)
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			s := 0.0
			for o := 0; o < rows; o++ {
				s += y[o*n+i] * y[o*n+j]
			}
			a[i*n+j], a[j*n+i] = s/float64(rows-1), s/float64(rows-1)
		}
	}
	return a, func(v, w []float64) {
		for i := 0; i < n; i++ {
			w[i] = dot(a[i*n:i*n+n], v)
		}
	}
}

// The Lanczos method's largest pairs are the full decomposition's: values within 10⁻¹⁰ of the
// largest, vectors parallel to 10⁻⁸ where their values stand apart.
func TestLanczosAgreesWithTheFullDecomposition(t *testing.T) {
	for _, c := range []struct{ n, k int }{{300, 50}, {120, 20}, {60, 60}} {
		a, apply := covariance(c.n, 2*c.n, uint64(c.n))
		values, vectors, err := SymEigen(a, c.n)
		if err != nil {
			t.Fatal(err)
		}
		top, tv, err := SymTop(apply, c.n, c.k, 7, 1e-11)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < c.k; i++ {
			if d := math.Abs(top[i] - values[i]); d > 1e-10*values[0] {
				t.Errorf("n %d: value %d is %g, the full decomposition's %g", c.n, i, top[i], values[i])
			}
			// Only where the value stands apart from its neighbors is its vector determined.
			gap := math.Inf(1)
			if i > 0 {
				gap = values[i-1] - values[i]
			}
			if i+1 < c.n {
				gap = math.Min(gap, values[i]-values[i+1])
			}
			if gap < 1e-3*values[0] {
				continue
			}
			cos := 0.0
			for g := 0; g < c.n; g++ {
				cos += tv[g*c.k+i] * vectors[g*c.n+i]
			}
			if math.Abs(math.Abs(cos)-1) > 1e-8 {
				t.Errorf("n %d: vector %d at |cos| %.12f to the full decomposition's", c.n, i, math.Abs(cos))
			}
		}
	}
}

// A matrix of low rank: the Krylov space closes after its rank and goes on from a new direction;
// the zero eigenvalues are found too.
func TestLanczosOnALowRankMatrix(t *testing.T) {
	n := 40
	a := make([]float64, n*n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			// Rank 2: u uᵀ + 2 w wᵀ with u = 1/√n, w alternating.
			w := 1.0
			if (i+j)%2 == 1 {
				w = -1
			}
			a[i*n+j] = 3*(1/float64(n)) + 2*w/float64(n)
		}
	}
	apply := func(v, y []float64) {
		for i := 0; i < n; i++ {
			y[i] = dot(a[i*n:i*n+n], v)
		}
	}
	top, _, err := SymTop(apply, n, 4, 3, 1e-12)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(top[0]-3) > 1e-12 || math.Abs(top[1]-2) > 1e-12 || math.Abs(top[2]) > 1e-12 || math.Abs(top[3]) > 1e-12 {
		t.Errorf("values %v, by hand 3, 2, 0, 0", top)
	}
}
