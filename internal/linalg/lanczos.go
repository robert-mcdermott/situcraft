package linalg

// SymTop computes a symmetric matrix's largest eigenvalues and their eigenvectors by the Lanczos
// method (Lanczos C. An iteration method for the solution of the eigenvalue problem of linear
// differential and integral operators. J Res Natl Bur Stand 1950;45:255–282) with full
// reorthogonalization (Paige CC. Computational variants of the Lanczos method for the eigenproblem.
// IMA J Appl Math 1972;10:373–381; Golub & Van Loan 2013, §10.1): the Krylov space of a seeded start
// grows, each new vector made orthogonal to all before it twice over, until the wanted Ritz pairs'
// residuals are below a tolerance; the tridiagonal matrix's eigenpairs come from tql2. Its cost is
// a product with the matrix per step, not the cube of the size: for the covariance of thousands of
// genes, seconds where the full decomposition takes minutes.

import (
	"errors"
	"math"
	"sort"

	"situcraft/internal/random"
)

// dot is a fixed-order sum of rounded products.
func dot(a, b []float64) float64 {
	s := 0.0
	for i, v := range a {
		s += mul(v, b[i])
	}
	return s
}

// SymTop answers the k largest eigenvalues (in decreasing order) of the symmetric n × n matrix A
// that apply multiplies by (y = A·x; it must give the same bits for the same x), and their unit
// eigenvectors as the columns of an n × k row-major matrix. Each Ritz pair is converged when its
// residual ‖A·v − θ·v‖ is at most tol times the largest Ritz value; the space grows to n at most.
// Signs are as the method leaves them.
func SymTop(apply func(x, y []float64), n, k int, seed uint64, tol float64) (values, vectors []float64, err error) {
	if k < 1 || k > n {
		return nil, nil, errors.New("linalg: the eigenpairs wanted are between 1 and the size")
	}
	var q [][]float64 // the orthonormal basis
	var alpha, beta []float64
	s := random.New(seed, 0)
	start := func() []float64 {
		// A random vector, made orthogonal to the basis twice over.
		v := make([]float64, n)
		for i := range v {
			v[i] = s.Normal()
		}
		for pass := 0; pass < 2; pass++ {
			for _, b := range q {
				c := dot(v, b)
				for i := range v {
					v[i] -= mul(c, b[i])
				}
			}
		}
		norm := math.Sqrt(dot(v, v))
		if !(norm > 0) {
			return nil
		}
		for i := range v {
			v[i] /= norm
		}
		return v
	}
	v := start()
	w := make([]float64, n)
	check := min(n, 2*k+10)
	for {
		q = append(q, v)
		j := len(q) - 1
		apply(v, w)
		for _, x := range w {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, nil, ErrNotFinite
			}
		}
		alpha = append(alpha, dot(v, w))
		// w − αⱼqⱼ − βⱼ₋₁qⱼ₋₁, then orthogonal to every basis vector, twice.
		for i := range w {
			w[i] -= mul(alpha[j], v[i])
			if j > 0 {
				w[i] -= mul(beta[j-1], q[j-1][i])
			}
		}
		for pass := 0; pass < 2; pass++ {
			for _, b := range q {
				c := dot(w, b)
				for i := range w {
					w[i] -= mul(c, b[i])
				}
			}
		}
		b := math.Sqrt(dot(w, w))
		m := len(q)
		if m == check || m == n {
			theta, ritz, err := tridiagonal(alpha, beta, m)
			if err != nil {
				return nil, nil, err
			}
			converged := true
			top := math.Abs(theta[0])
			for c := 0; c < min(k, m); c++ {
				// The residual of Ritz pair c is |b · (its vector's last entry)|.
				if math.Abs(mul(b, ritz[(m-1)*m+c])) > mul(tol, top) {
					converged = false
					break
				}
			}
			if (converged && m >= k) || m == n {
				values = append([]float64(nil), theta[:k]...)
				vectors = make([]float64, n*k)
				for c := 0; c < k; c++ {
					for jj := 0; jj < m; jj++ {
						f := ritz[jj*m+c]
						row := q[jj]
						for i := 0; i < n; i++ {
							vectors[i*k+c] += mul(f, row[i])
						}
					}
				}
				return values, vectors, nil
			}
			check = min(n, m+max(10, m/4))
		}
		beta = append(beta, b)
		if b <= mul(0x1p-40, math.Abs(alpha[0])+b) {
			// An invariant subspace: go on from a new direction orthogonal to it.
			beta[j] = 0
			if v = start(); v == nil {
				return nil, nil, ErrNoConvergence
			}
			continue
		}
		v = make([]float64, n)
		for i := range v {
			v[i] = w[i] / b
		}
	}
}

// tridiagonal answers the eigenvalues of the m × m tridiagonal matrix of diagonal alpha and
// off-diagonal beta in decreasing order, and its eigenvectors as the columns of an m × m row-major
// matrix in the same order.
func tridiagonal(alpha, beta []float64, m int) ([]float64, []float64, error) {
	v := make([]float64, m*m)
	for i := 0; i < m; i++ {
		v[i*m+i] = 1
	}
	d := append([]float64(nil), alpha[:m]...)
	// tql2 reads the off-diagonal from e[1:].
	e := make([]float64, m)
	for i := 1; i < m; i++ {
		e[i] = beta[i-1]
	}
	if err := tql2(v, d, e, m); err != nil {
		return nil, nil, err
	}
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return d[order[x]] > d[order[y]] })
	values := make([]float64, m)
	vectors := make([]float64, m*m)
	for c, j := range order {
		values[c] = d[j]
		for i := 0; i < m; i++ {
			vectors[i*m+c] = v[i*m+j]
		}
	}
	return values, vectors, nil
}
