package linalg

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

// check verifies A v = λ v for every pair, the vectors orthonormal, and the values decreasing.
func check(t *testing.T, name string, a []float64, n int, values, vectors []float64, tol float64) {
	t.Helper()
	norm := 0.0
	for _, x := range a {
		norm = math.Max(norm, math.Abs(x))
	}
	for k := 0; k < n; k++ {
		if k > 0 && values[k] > values[k-1] {
			t.Errorf("%s: value %d (%g) above value %d (%g)", name, k, values[k], k-1, values[k-1])
		}
		for i := 0; i < n; i++ {
			av := 0.0
			for j := 0; j < n; j++ {
				av += a[i*n+j] * vectors[j*n+k]
			}
			if r := math.Abs(av - values[k]*vectors[i*n+k]); r > tol*math.Max(norm, 1) {
				t.Fatalf("%s: residual %g in vector %d, row %d", name, r, k, i)
			}
		}
		for l := 0; l <= k; l++ {
			dot := 0.0
			for i := 0; i < n; i++ {
				dot += vectors[i*n+k] * vectors[i*n+l]
			}
			want := 0.0
			if k == l {
				want = 1
			}
			if math.Abs(dot-want) > tol {
				t.Fatalf("%s: vectors %d and %d have product %g", name, k, l, dot)
			}
		}
	}
}

// The second-difference matrix's eigenvalues are 2 − 2cos(kπ/(n+1)), analytically.
func TestSecondDifferenceMatrix(t *testing.T) {
	for _, n := range []int{1, 2, 3, 10, 57} {
		a := make([]float64, n*n)
		for i := 0; i < n; i++ {
			a[i*n+i] = 2
			if i > 0 {
				a[i*n+i-1], a[(i-1)*n+i] = -1, -1
			}
		}
		values, vectors, err := SymEigen(a, n)
		if err != nil {
			t.Fatal(err)
		}
		for k := 0; k < n; k++ {
			want := 2 - 2*math.Cos(float64(n-k)*math.Pi/float64(n+1))
			if math.Abs(values[k]-want) > 1e-13 {
				t.Errorf("n %d: value %d is %.16g, want %.16g", n, k, values[k], want)
			}
		}
		check(t, fmt.Sprint("second difference ", n), a, n, values, vectors, 1e-12)
	}
}

// Random symmetric matrices, covariances (positive semidefinite, rank-deficient) and matrices with
// repeated eigenvalues and zero rows.
func TestRandomAndDegenerateMatrices(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for trial, n := range []int{4, 30, 120} {
		a := make([]float64, n*n)
		for i := 0; i < n; i++ {
			for j := 0; j <= i; j++ {
				x := r.NormFloat64()
				a[i*n+j], a[j*n+i] = x, x
			}
		}
		values, vectors, err := SymEigen(a, n)
		if err != nil {
			t.Fatal(err)
		}
		check(t, fmt.Sprint("random ", trial), a, n, values, vectors, 1e-11)
		trace, sum := 0.0, 0.0
		for i := 0; i < n; i++ {
			trace += a[i*n+i]
			sum += values[i]
		}
		if math.Abs(trace-sum) > 1e-10 {
			t.Errorf("random %d: eigenvalues sum to %g, the trace is %g", trial, sum, trace)
		}
	}
	// A covariance of 5 samples in 12 dimensions: rank 4, eight zero eigenvalues.
	n, m := 12, 5
	x := make([]float64, m*n)
	for i := range x {
		x[i] = r.NormFloat64()
	}
	c := make([]float64, n*n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			mi, mj := 0.0, 0.0
			for s := 0; s < m; s++ {
				mi += x[s*n+i] / float64(m)
				mj += x[s*n+j] / float64(m)
			}
			for s := 0; s < m; s++ {
				c[i*n+j] += (x[s*n+i] - mi) * (x[s*n+j] - mj) / float64(m-1)
			}
		}
	}
	values, vectors, err := SymEigen(c, n)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "covariance", c, n, values, vectors, 1e-11)
	for k := m - 1; k < n; k++ {
		if math.Abs(values[k]) > 1e-12 {
			t.Errorf("covariance: value %d is %g, not zero", k, values[k])
		}
	}
	// The identity with a zero row and column, and a diagonal with repeats.
	d := []float64{3, 0, 0, 0, 0, 0, 0, 0, 1}
	values, vectors, err = SymEigen(d, 3)
	if err != nil || values[0] != 3 || values[1] != 1 || values[2] != 0 {
		t.Errorf("diagonal: %v %v", values, err)
	}
	check(t, "diagonal", d, 3, values, vectors, 1e-15)
	if _, _, err := SymEigen([]float64{math.NaN(), 0, 0, 1}, 2); err == nil {
		t.Error("a NaN entry converged")
	}
}

// The result's bits: the same on every processor (this digest is checked on arm64 and amd64).
func TestEigenDigest(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	n := 40
	a := make([]float64, n*n)
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			x := r.NormFloat64()
			a[i*n+j], a[j*n+i] = x, x
		}
	}
	values, vectors, err := SymEigen(a, n)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, values)
	binary.Write(h, binary.LittleEndian, vectors)
	if got := fmt.Sprintf("%x", h.Sum(nil))[:16]; got != eigenDigest {
		t.Errorf("digest %s, recorded %s", got, eigenDigest)
	}
}

const eigenDigest = "141c5a25db7ff110"
