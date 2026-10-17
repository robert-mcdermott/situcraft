package register

// Colour deconvolution (Ruifrok AC, Johnston DA. Quantification of histochemical staining by color
// deconvolution. Anal Quant Cytol Histol 2001;23(4):291–299): by Beer–Lambert's law, a pixel's
// optical densities of red, green and blue, −log₁₀ of its light over the glass's, are a sum of each
// stain's amount times the stain's colour; with haematoxylin's, eosin's and a third orthogonal to
// both, the amounts are the densities times the inverse of the colours' matrix. Haematoxylin
// stains nuclei: its amount is what an H&E is registered by against DAPI.

import (
	"math"

	"situcraft/internal/dmath"
)

// Stains are an H&E's haematoxylin and eosin colours, as optical densities of red, green and blue.
type Stains [2][3]float64

// HAndE are Ruifrok and Johnston's colours of haematoxylin and eosin.
var HAndE = Stains{{0.65, 0.70, 0.29}, {0.07, 0.99, 0.11}}

// Deconvolution turns an H&E's colours into its haematoxylin's amount.
type Deconvolution struct {
	inv [3][3]float64
	// od are −log₁₀(v / background) for each channel's value v (a value of 0 taken as 1).
	od [3][256]float64
}

func unit3(v [3]float64) [3]float64 {
	n := math.Sqrt(mul(v[0], v[0]) + mul(v[1], v[1]) + mul(v[2], v[2]))
	return [3]float64{v[0] / n, v[1] / n, v[2] / n}
}

// NewDeconvolution is the deconvolution of an H&E of these stains, its glass's light background
// (each channel's value where there is no tissue).
func NewDeconvolution(st Stains, background [3]float64) Deconvolution {
	h, e := unit3(st[0]), unit3(st[1])
	r := unit3([3]float64{mul(h[1], e[2]) - mul(h[2], e[1]), mul(h[2], e[0]) - mul(h[0], e[2]), mul(h[0], e[1]) - mul(h[1], e[0])})
	m := [3][3]float64{h, e, r} // rows: each stain's densities of red, green and blue
	det := mul(m[0][0], mul(m[1][1], m[2][2])-mul(m[1][2], m[2][1])) - mul(m[0][1], mul(m[1][0], m[2][2])-mul(m[1][2], m[2][0])) + mul(m[0][2], mul(m[1][0], m[2][1])-mul(m[1][1], m[2][0]))
	cof := func(r0, c0, r1, c1 int) float64 { return mul(m[r0][c0], m[r1][c1]) - mul(m[r0][c1], m[r1][c0]) }
	var d Deconvolution
	// The inverse, as the transposed cofactors over the determinant.
	d.inv = [3][3]float64{
		{cof(1, 1, 2, 2) / det, -cof(0, 1, 2, 2) / det, cof(0, 1, 1, 2) / det},
		{-cof(1, 0, 2, 2) / det, cof(0, 0, 2, 2) / det, -cof(0, 0, 1, 2) / det},
		{cof(1, 0, 2, 1) / det, -cof(0, 0, 2, 1) / det, cof(0, 0, 1, 1) / det},
	}
	for c := 0; c < 3; c++ {
		bg := math.Max(1, background[c])
		for v := 0; v < 256; v++ {
			d.od[c][v] = -dmath.Log(math.Max(1, float64(v))/bg) / math.Ln10
		}
	}
	return d
}

// Amounts are a pixel's amounts of haematoxylin, eosin and the residue.
func (d Deconvolution) Amounts(r, g, b uint8) [3]float64 {
	od := [3]float64{d.od[0][r], d.od[1][g], d.od[2][b]}
	var out [3]float64
	for s := 0; s < 3; s++ {
		out[s] = mul(od[0], d.inv[0][s]) + mul(od[1], d.inv[1][s]) + mul(od[2], d.inv[2][s])
	}
	return out
}

// Haematoxylin is a pixel's amount of haematoxylin (no less than 0).
func (d Deconvolution) Haematoxylin(r, g, b uint8) float64 {
	od0, od1, od2 := d.od[0][r], d.od[1][g], d.od[2][b]
	return math.Max(0, mul(od0, d.inv[0][0])+mul(od1, d.inv[1][0])+mul(od2, d.inv[2][0]))
}
