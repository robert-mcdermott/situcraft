// Package linalg has the dense linear algebra SituCraft's analyses need, with results whose bits
// are the same on every processor: every product is rounded on its own (an explicit conversion,
// which the Go specification says prevents a fused multiply-add), sums are taken in a fixed order,
// and square roots are correctly rounded everywhere.
//
// SymEigen computes every eigenvalue and eigenvector of a symmetric matrix by Householder reduction
// to tridiagonal form and the QL method with implicit shifts (Wilkinson JH, Reinsch C. Handbook for
// Automatic Computation, vol. II: Linear Algebra. Springer, 1971, procedures tred2 and tql2;
// Golub GH, Van Loan CF. Matrix Computations, 4th ed. Johns Hopkins, 2013, §8.3).
package linalg

import (
	"errors"
	"math"
	"sort"
)

// mul is a product rounded on its own, never fused with an addition.
func mul(a, b float64) float64 { return float64(a * b) }

// hypot is √(a² + b²) without overflow, the same everywhere (math.Hypot's 1 + q² may be fused).
func hypot(a, b float64) float64 {
	a, b = math.Abs(a), math.Abs(b)
	if a < b {
		a, b = b, a
	}
	if a == 0 {
		return 0
	}
	q := b / a
	return mul(a, math.Sqrt(1+mul(q, q)))
}

// Errors: a matrix with a NaN or infinite entry, and an iteration that did not converge (which
// the method's convergence theory rules out for finite symmetric matrices).
var (
	ErrNotFinite     = errors.New("linalg: the matrix has an entry that is not a finite number")
	ErrNoConvergence = errors.New("linalg: the eigenvalues did not converge")
)

// SymEigen answers the eigenvalues of the symmetric n × n matrix a (row-major; its lower triangle
// is read and it is not changed) in decreasing order, ties in their order of computation, and the
// eigenvectors as the columns of an n × n row-major matrix: vectors[i*n+k] is the i-th entry of the
// k-th vector, of unit length. Signs are as the method leaves them.
func SymEigen(a []float64, n int) (values, vectors []float64, err error) {
	if n < 0 || len(a) < n*n {
		return nil, nil, errors.New("linalg: the matrix is smaller than n × n")
	}
	if n == 0 {
		return nil, nil, nil
	}
	for _, x := range a[:n*n] {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, nil, ErrNotFinite
		}
	}
	v := make([]float64, n*n)
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			v[i*n+j] = a[i*n+j]
			v[j*n+i] = a[i*n+j]
		}
	}
	d := make([]float64, n)
	e := make([]float64, n)
	tred2(v, d, e, n)
	if err := tql2(v, d, e, n); err != nil {
		return nil, nil, err
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return d[order[x]] > d[order[y]] })
	values = make([]float64, n)
	vectors = make([]float64, n*n)
	for k, j := range order {
		values[k] = d[j]
		for i := 0; i < n; i++ {
			vectors[i*n+k] = v[i*n+j]
		}
	}
	return values, vectors, nil
}

// tred2 reduces the symmetric matrix in v to tridiagonal form by Householder reflections, leaving
// the diagonal in d, the subdiagonal in e[1:], and the accumulated orthogonal transformation in v.
func tred2(v, d, e []float64, n int) {
	at := func(i, j int) *float64 { return &v[i*n+j] }
	for j := 0; j < n; j++ {
		d[j] = *at(n-1, j)
	}
	for i := n - 1; i > 0; i-- {
		scale, h := 0.0, 0.0
		for k := 0; k < i; k++ {
			scale += math.Abs(d[k])
		}
		if scale == 0 {
			e[i] = d[i-1]
			for j := 0; j < i; j++ {
				d[j] = *at(i-1, j)
				*at(i, j) = 0
				*at(j, i) = 0
			}
		} else {
			for k := 0; k < i; k++ {
				d[k] /= scale
				h += mul(d[k], d[k])
			}
			f := d[i-1]
			g := math.Sqrt(h)
			if f > 0 {
				g = -g
			}
			e[i] = mul(scale, g)
			h -= mul(f, g)
			d[i-1] = f - g
			for j := 0; j < i; j++ {
				e[j] = 0
			}
			for j := 0; j < i; j++ {
				f = d[j]
				*at(j, i) = f
				g = e[j] + mul(*at(j, j), f)
				for k := j + 1; k <= i-1; k++ {
					g += mul(*at(k, j), d[k])
					e[k] += mul(*at(k, j), f)
				}
				e[j] = g
			}
			f = 0
			for j := 0; j < i; j++ {
				e[j] /= h
				f += mul(e[j], d[j])
			}
			hh := f / (h + h)
			for j := 0; j < i; j++ {
				e[j] -= mul(hh, d[j])
			}
			for j := 0; j < i; j++ {
				f = d[j]
				g = e[j]
				for k := j; k <= i-1; k++ {
					*at(k, j) -= mul(f, e[k]) + mul(g, d[k])
				}
				d[j] = *at(i-1, j)
				*at(i, j) = 0
			}
		}
		d[i] = h
	}
	// The transformations, accumulated.
	for i := 0; i < n-1; i++ {
		*at(n-1, i) = *at(i, i)
		*at(i, i) = 1
		h := d[i+1]
		if h != 0 {
			for k := 0; k <= i; k++ {
				d[k] = *at(k, i+1) / h
			}
			for j := 0; j <= i; j++ {
				g := 0.0
				for k := 0; k <= i; k++ {
					g += mul(*at(k, i+1), *at(k, j))
				}
				for k := 0; k <= i; k++ {
					*at(k, j) -= mul(g, d[k])
				}
			}
		}
		for k := 0; k <= i; k++ {
			*at(k, i+1) = 0
		}
	}
	for j := 0; j < n; j++ {
		d[j] = *at(n-1, j)
		*at(n-1, j) = 0
	}
	*at(n-1, n-1) = 1
	e[0] = 0
}

// tql2 diagonalizes the tridiagonal matrix (d, e) by the QL method with implicit shifts, applying
// the rotations to v, so that d holds the eigenvalues and v's columns the eigenvectors.
func tql2(v, d, e []float64, n int) error {
	for i := 1; i < n; i++ {
		e[i-1] = e[i]
	}
	e[n-1] = 0
	f, tst1 := 0.0, 0.0
	const eps = 0x1p-52
	for l := 0; l < n; l++ {
		tst1 = math.Max(tst1, math.Abs(d[l])+math.Abs(e[l]))
		m := l
		for m < n && math.Abs(e[m]) > mul(eps, tst1) {
			m++
		}
		if m == n {
			m = n - 1
		}
		if m > l {
			for iter := 0; ; iter++ {
				if iter > 60 || math.IsNaN(tst1) {
					return ErrNoConvergence
				}
				g := d[l]
				p := (d[l+1] - g) / mul(2, e[l])
				r := hypot(p, 1)
				if p < 0 {
					r = -r
				}
				d[l] = e[l] / (p + r)
				d[l+1] = mul(e[l], p+r)
				dl1 := d[l+1]
				h := g - d[l]
				for i := l + 2; i < n; i++ {
					d[i] -= h
				}
				f += h
				p = d[m]
				c, c2, c3 := 1.0, 1.0, 1.0
				el1 := e[l+1]
				s, s2 := 0.0, 0.0
				for i := m - 1; i >= l; i-- {
					c3 = c2
					c2 = c
					s2 = s
					g = mul(c, e[i])
					h = mul(c, p)
					r = hypot(p, e[i])
					e[i+1] = mul(s, r)
					s = e[i] / r
					c = p / r
					p = mul(c, d[i]) - mul(s, g)
					d[i+1] = h + mul(s, mul(c, g)+mul(s, d[i]))
					for k := 0; k < n; k++ {
						h = v[k*n+i+1]
						v[k*n+i+1] = mul(s, v[k*n+i]) + mul(c, h)
						v[k*n+i] = mul(c, v[k*n+i]) - mul(s, h)
					}
				}
				p = mul(mul(mul(mul(-s, s2), c3), el1), e[l]) / dl1
				e[l] = mul(s, p)
				d[l] = mul(c, p)
				if math.Abs(e[l]) <= mul(eps, tst1) {
					break
				}
			}
		}
		d[l] += f
		e[l] = 0
	}
	return nil
}
