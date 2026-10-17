package register

// A smooth deformation of the plane by cubic B-splines (Rueckert D, Sonoda LI, Hayes C, Hill DLG,
// Leach MO, Hawkes DJ. Nonrigid registration using free-form deformations: application to breast MR
// images. IEEE Trans Med Imaging 1999;18(8):712–721): displacements at a grid of control points,
// each point's displacement their weighted sum by the cubic B-spline of its distance from them. It
// is fitted to displacements measured at points (local matches) by least squares, with a penalty
// on its second differences that keeps it smooth where the points are few, and refitted smoother
// until it nowhere folds (the deformed plane's Jacobian determinant stays well above 0).

import (
	"errors"
	"math"
)

// BSpline is a deformation: (x, y) µm moved by the displacement At(x, y).
type BSpline struct {
	// Origin is control point (0, 0), in µm; Spacing the grid's step.
	Origin  [2]float64 `json:"origin"`
	Spacing float64    `json:"spacing"`
	NX      int        `json:"nx"`
	NY      int        `json:"ny"`
	// DX and DY are each control point's displacement (µm), row by row.
	DX []float64 `json:"dx"`
	DY []float64 `json:"dy"`
}

// basis are the cubic B-spline's four weights at t in [0, 1), for control points i − 1 … i + 2, and
// their derivatives.
func basis(t float64) (w, d [4]float64) {
	t2 := mul(t, t)
	t3 := mul(t2, t)
	s := 1 - t
	w[0] = mul(mul(s, s), s) / 6
	w[1] = (mul(3, t3) - mul(6, t2) + 4) / 6
	w[2] = (mul(-3, t3) + mul(3, t2) + mul(3, t) + 1) / 6
	w[3] = t3 / 6
	d[0] = -mul(s, s) / 2
	d[1] = (mul(9, t2) - mul(12, t)) / 6
	d[2] = (mul(-9, t2) + mul(6, t) + 3) / 6
	d[3] = t2 / 2
	return w, d
}

// cell is the control points' neighbourhood of a point: its first control column and row, and
// where it falls in its cell.
func (b *BSpline) cell(x, y float64) (i, j int, tx, ty float64) {
	u, v := (x-b.Origin[0])/b.Spacing, (y-b.Origin[1])/b.Spacing
	fi, fj := math.Floor(u), math.Floor(v)
	return int(fi) - 1, int(fj) - 1, u - fi, v - fj
}

// At is the displacement at a point (0 beyond the grid's reach).
func (b *BSpline) At(x, y float64) (dx, dy float64) {
	i0, j0, tx, ty := b.cell(x, y)
	wx, _ := basis(tx)
	wy, _ := basis(ty)
	for b2 := 0; b2 < 4; b2++ {
		j := j0 + b2
		if j < 0 || j >= b.NY {
			continue
		}
		for a := 0; a < 4; a++ {
			i := i0 + a
			if i < 0 || i >= b.NX {
				continue
			}
			w := mul(wx[a], wy[b2])
			dx += mul(w, b.DX[j*b.NX+i])
			dy += mul(w, b.DY[j*b.NX+i])
		}
	}
	return dx, dy
}

// Jacobian is the determinant of the deformed plane's Jacobian at a point: below 0 where it folds.
func (b *BSpline) Jacobian(x, y float64) float64 {
	i0, j0, tx, ty := b.cell(x, y)
	wx, dwx := basis(tx)
	wy, dwy := basis(ty)
	var xx, xy, yx, yy float64 // ∂dx/∂x, ∂dx/∂y, ∂dy/∂x, ∂dy/∂y
	for b2 := 0; b2 < 4; b2++ {
		j := j0 + b2
		if j < 0 || j >= b.NY {
			continue
		}
		for a := 0; a < 4; a++ {
			i := i0 + a
			if i < 0 || i >= b.NX {
				continue
			}
			k := j*b.NX + i
			gx, gy := mul(dwx[a], wy[b2])/b.Spacing, mul(wx[a], dwy[b2])/b.Spacing
			xx += mul(gx, b.DX[k])
			xy += mul(gy, b.DX[k])
			yx += mul(gx, b.DY[k])
			yy += mul(gy, b.DY[k])
		}
	}
	return mul(1+xx, 1+yy) - mul(xy, yx)
}

// Inverse is the point x the deformation moves to y (x + At(x) = y), by fixed-point iteration (it
// converges where the deformation's gradient is small, as a fitted one's is).
func (b *BSpline) Inverse(y0, y1 float64) (float64, float64) {
	x0, x1 := y0, y1
	for k := 0; k < 30; k++ {
		dx, dy := b.At(x0, x1)
		nx, ny := y0-dx, y1-dy
		if math.Abs(nx-x0) < 1e-9 && math.Abs(ny-x1) < 1e-9 {
			return nx, ny
		}
		x0, x1 = nx, ny
	}
	return x0, x1
}

// MaxDisplacement is the largest displacement over a rectangle (µm), on a grid a quarter of the
// spacing apart.
func (b *BSpline) MaxDisplacement(x0, y0, x1, y1 float64) float64 {
	worst := 0.0
	step := b.Spacing / 4
	for y := y0; y <= y1; y += step {
		for x := x0; x <= x1; x += step {
			dx, dy := b.At(x, y)
			worst = math.Max(worst, math.Sqrt(mul(dx, dx)+mul(dy, dy)))
		}
	}
	return worst
}

// grid is a B-spline's grid over a rectangle of µm (x0, y0)–(x1, y1), a cell beyond it each way.
func newBSpline(x0, y0, x1, y1, spacing float64) *BSpline {
	nx := int(math.Ceil((x1-x0)/spacing)) + 3
	ny := int(math.Ceil((y1-y0)/spacing)) + 3
	return &BSpline{Origin: [2]float64{x0 - spacing, y0 - spacing}, Spacing: spacing, NX: nx, NY: ny, DX: make([]float64, nx*ny), DY: make([]float64, nx*ny)}
}

// FitBSpline fits a deformation over a rectangle to displacements d measured at points p (µm), with
// a smoothness of lambda (relative to the data's weight; raised fourfold until the deformation does
// not fold, at most eight times). It answers the deformation and the smoothness used.
func FitBSpline(p, d [][2]float64, x0, y0, x1, y1, spacing, lambda float64) (*BSpline, float64, error) {
	if len(p) < 4 || len(p) != len(d) {
		return nil, 0, errors.New("a deformation is fitted to four points or more")
	}
	for try := 0; try < 8; try++ {
		b := newBSpline(x0, y0, x1, y1, spacing)
		if err := b.fit(p, d, lambda); err != nil {
			return nil, 0, err
		}
		if b.minJacobian(x0, y0, x1, y1) > 0.5 {
			return b, lambda, nil
		}
		lambda *= 4
	}
	return nil, 0, errors.New("the deformation folds however smooth it is made: the local matches disagree")
}

// fit solves the penalized least squares for each axis by Cholesky's factorization of the normal
// equations.
func (b *BSpline) fit(p, d [][2]float64, lambda float64) error {
	n := b.NX * b.NY
	ata := make([]float64, n*n)
	rhs := [2][]float64{make([]float64, n), make([]float64, n)}
	for k := range p {
		i0, j0, tx, ty := b.cell(p[k][0], p[k][1])
		wx, _ := basis(tx)
		wy, _ := basis(ty)
		var idx [16]int
		var w [16]float64
		m := 0
		for b2 := 0; b2 < 4; b2++ {
			for a := 0; a < 4; a++ {
				i, j := i0+a, j0+b2
				if i < 0 || j < 0 || i >= b.NX || j >= b.NY {
					continue
				}
				idx[m], w[m] = j*b.NX+i, mul(wx[a], wy[b2])
				m++
			}
		}
		for r := 0; r < m; r++ {
			rhs[0][idx[r]] += mul(w[r], d[k][0])
			rhs[1][idx[r]] += mul(w[r], d[k][1])
			for c := 0; c < m; c++ {
				ata[idx[r]*n+idx[c]] += mul(w[r], w[c])
			}
		}
	}
	// The penalty: each second difference along x and along y, squared, weighed so that lambda is
	// relative to the data's mean weight per control point; and a little shrinkage, so that control
	// points no data reach stay still.
	var trace float64
	for i := 0; i < n; i++ {
		trace += ata[i*n+i]
	}
	weight := mul(lambda, trace/float64(n))
	if !(weight > 0) {
		weight = lambda
	}
	add := func(ks [3]int, cs [3]float64) {
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				ata[ks[r]*n+ks[c]] += mul(weight, mul(cs[r], cs[c]))
			}
		}
	}
	for j := 0; j < b.NY; j++ {
		for i := 0; i < b.NX; i++ {
			k := j*b.NX + i
			if i > 0 && i < b.NX-1 {
				add([3]int{k - 1, k, k + 1}, [3]float64{1, -2, 1})
			}
			if j > 0 && j < b.NY-1 {
				add([3]int{k - b.NX, k, k + b.NX}, [3]float64{1, -2, 1})
			}
			ata[k*n+k] += mul(1e-3, weight)
		}
	}
	l, err := cholesky(ata, n)
	if err != nil {
		return err
	}
	b.DX, b.DY = solveCholesky(l, n, rhs[0]), solveCholesky(l, n, rhs[1])
	return nil
}

// minJacobian is the least Jacobian determinant over the rectangle, on a grid a quarter of the
// spacing apart.
func (b *BSpline) minJacobian(x0, y0, x1, y1 float64) float64 {
	worst := math.Inf(1)
	step := b.Spacing / 4
	for y := y0; y <= y1; y += step {
		for x := x0; x <= x1; x += step {
			worst = math.Min(worst, b.Jacobian(x, y))
		}
	}
	return worst
}

// cholesky is the lower-triangular L with L Lᵀ = a (symmetric positive definite, n × n).
func cholesky(a []float64, n int) ([]float64, error) {
	l := make([]float64, n*n)
	for j := 0; j < n; j++ {
		s := a[j*n+j]
		for k := 0; k < j; k++ {
			s -= mul(l[j*n+k], l[j*n+k])
		}
		if !(s > 0) {
			return nil, errors.New("the deformation's equations are singular")
		}
		d := math.Sqrt(s)
		l[j*n+j] = d
		for i := j + 1; i < n; i++ {
			s := a[i*n+j]
			for k := 0; k < j; k++ {
				s -= mul(l[i*n+k], l[j*n+k])
			}
			l[i*n+j] = s / d
		}
	}
	return l, nil
}

// solveCholesky solves L Lᵀ x = b.
func solveCholesky(l []float64, n int, b []float64) []float64 {
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		s := b[i]
		for k := 0; k < i; k++ {
			s -= mul(l[i*n+k], y[k])
		}
		y[i] = s / l[i*n+i]
	}
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := y[i]
		for k := i + 1; k < n; k++ {
			s -= mul(l[k*n+i], x[k])
		}
		x[i] = s / l[i*n+i]
	}
	return x
}
