// Package register aligns post-Xenium images (H&E and immunofluorescence, stained and scanned after
// the Xenium run) to the Xenium image: transforms between coordinate systems, the alignments 10x
// Genomics' software writes, and the fits that make them. Every product is rounded on its own, so a
// transform is the same on every processor.
package register

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// mul is a product rounded on its own: an explicit conversion keeps the compiler from fusing it
// into a multiply-add, which some processors' would round once instead of twice.
func mul(a, b float64) float64 { return float64(a * b) }

// Affine maps a point (x, y) to (A·x + B·y + C, D·x + E·y + F). In JSON it is its two rows,
// [[A, B, C], [D, E, F]] (the third, [0, 0, 1], implied).
type Affine struct{ A, B, C, D, E, F float64 }

// Identity is the transform that changes nothing.
func Identity() Affine { return Affine{A: 1, E: 1} }

// Scaling multiplies both coordinates by s (pixels of size s µm to µm).
func Scaling(s float64) Affine { return Affine{A: s, E: s} }

// Apply maps a point.
func (t Affine) Apply(x, y float64) (float64, float64) {
	return mul(t.A, x) + mul(t.B, y) + t.C, mul(t.D, x) + mul(t.E, y) + t.F
}

// Then is t followed by u.
func (t Affine) Then(u Affine) Affine {
	return Affine{
		A: mul(u.A, t.A) + mul(u.B, t.D), B: mul(u.A, t.B) + mul(u.B, t.E), C: mul(u.A, t.C) + mul(u.B, t.F) + u.C,
		D: mul(u.D, t.A) + mul(u.E, t.D), E: mul(u.D, t.B) + mul(u.E, t.E), F: mul(u.D, t.C) + mul(u.E, t.F) + u.F,
	}
}

// Det is the determinant of the linear part: the factor areas change by, negative for a reflection.
func (t Affine) Det() float64 { return mul(t.A, t.E) - mul(t.B, t.D) }

// Invert answers the inverse transform, false when there is none.
func (t Affine) Invert() (Affine, bool) {
	det := t.Det()
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return Affine{}, false
	}
	return Affine{
		A: t.E / det, B: -t.B / det, C: (mul(t.B, t.F) - mul(t.E, t.C)) / det,
		D: -t.D / det, E: t.A / det, F: (mul(t.D, t.C) - mul(t.A, t.F)) / det,
	}, true
}

// Scale is the factor lengths change by, on average over directions (the square root of |Det|).
func (t Affine) Scale() float64 { return math.Sqrt(math.Abs(t.Det())) }

// Reflected reports whether the transform mirrors the image.
func (t Affine) Reflected() bool { return t.Det() < 0 }

// Rotation is the angle (degrees, counter-clockwise in a system whose y points down: clockwise as
// seen) the transform turns the x axis by, after any reflection of y.
func (t Affine) Rotation() float64 {
	return math.Atan2(t.D, t.A) * 180 / math.Pi
}

// Valid reports whether every coefficient is finite and the transform invertible.
func (t Affine) Valid() bool {
	for _, v := range []float64{t.A, t.B, t.C, t.D, t.E, t.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	_, ok := t.Invert()
	return ok
}

func (t Affine) MarshalJSON() ([]byte, error) {
	return json.Marshal([2][3]float64{{t.A, t.B, t.C}, {t.D, t.E, t.F}})
}

func (t *Affine) UnmarshalJSON(b []byte) error {
	var rows [][]float64
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	if len(rows) < 2 || len(rows[0]) != 3 || len(rows[1]) != 3 {
		return errors.New("an affine transform is two rows of three numbers")
	}
	*t = Affine{rows[0][0], rows[0][1], rows[0][2], rows[1][0], rows[1][1], rows[1][2]}
	if !t.Valid() {
		return fmt.Errorf("the transform %v is not invertible", rows)
	}
	return nil
}
