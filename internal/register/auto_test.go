package register

import (
	"math"
	"testing"

	"situcraft/internal/random"
)

// Colours made by Beer–Lambert's law from known amounts of haematoxylin and eosin give the amounts
// back, to what 8-bit colours keep.
func TestDeconvolution(t *testing.T) {
	bg := [3]float64{240, 236, 242}
	d := NewDeconvolution(HAndE, bg)
	h, e := unit3(HAndE[0]), unit3(HAndE[1])
	for _, c := range [][2]float64{{0, 0}, {0.8, 0.1}, {0.2, 0.5}, {1.2, 0.3}} {
		var px [3]uint8
		for k := 0; k < 3; k++ {
			od := c[0]*h[k] + c[1]*e[k]
			px[k] = uint8(math.Round(bg[k] * math.Pow(10, -od)))
		}
		got := d.Amounts(px[0], px[1], px[2])
		if math.Abs(got[0]-c[0]) > 0.03 || math.Abs(got[1]-c[1]) > 0.03 || math.Abs(got[2]) > 0.03 {
			t.Errorf("haematoxylin %.2f, eosin %.2f: %v (%v)", c[0], c[1], got, px)
		}
		if hh := d.Haematoxylin(px[0], px[1], px[2]); math.Abs(hh-math.Max(0, got[0])) > 1e-12 {
			t.Errorf("Haematoxylin %v, Amounts %v", hh, got[0])
		}
	}
}

// Normalized mutual information is 2 for an image against itself, whatever relates their
// intensities (here its negative), and near 1 against noise.
func TestNMI(t *testing.T) {
	a := blobs(120, 100, 80, 3).Normalized(0, 1.5)
	neg := Plane{W: a.W, H: a.H, Pix: make([]float32, len(a.Pix))}
	noise := Plane{W: a.W, H: a.H, Pix: make([]float32, len(a.Pix))}
	s := random.New(9, 0)
	for i, v := range a.Pix {
		neg.Pix[i] = 1 - v
		noise.Pix[i] = float32(s.Float64())
	}
	id := Affine{A: 1, E: 1}
	same, overlap := NMIUnder(a, a, id, 32, 1)
	inverted, _ := NMIUnder(a, neg, id, 32, 1)
	unrelated, _ := NMIUnder(a, noise, id, 32, 1)
	if math.Abs(same-2) > 1e-9 || math.Abs(inverted-2) > 0.02 || unrelated > 1.1 || overlap != 1 {
		t.Errorf("itself %.4f, its negative %.4f, noise %.4f, overlap %.2f", same, inverted, unrelated, overlap)
	}
	// Shifted half off: the overlap is half.
	if _, half := NMIUnder(a, a, Affine{A: 1, C: 60, E: 1}, 32, 1); math.Abs(half-0.5) > 0.02 {
		t.Errorf("half off: overlap %.3f", half)
	}
}

// A compass search finds a quadratic's minimum to its least step.
func TestPatternSearch(t *testing.T) {
	f := func(p []float64) float64 { return (p[0]-3)*(p[0]-3) + 4*(p[1]+1)*(p[1]+1) }
	x, v, _ := patternSearch(f, []float64{0, 0}, []float64{1, 1}, []float64{1e-4, 1e-4}, 1000)
	if math.Abs(x[0]-3) > 1e-3 || math.Abs(x[1]+1) > 1e-3 || v > 1e-6 {
		t.Errorf("%v, %v", x, v)
	}
}

// A smooth displacement measured at scattered points is fitted back between them; one that would
// fold is made smoother until it does not; its inverse undoes it.
func TestBSpline(t *testing.T) {
	field := func(x, y float64) (float64, float64) {
		return 2 * math.Sin(x/90), 1.5 * math.Cos(y/70)
	}
	s := random.New(4, 0)
	var p, d [][2]float64
	for k := 0; k < 300; k++ {
		x, y := 400*s.Float64(), 300*s.Float64()
		dx, dy := field(x, y)
		p, d = append(p, [2]float64{x, y}), append(d, [2]float64{dx, dy})
	}
	b, _, err := FitBSpline(p, d, 0, 0, 400, 300, 50, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	worst := 0.0
	for y := 20.0; y < 280; y += 13 {
		for x := 20.0; x < 380; x += 17 {
			fx, fy := field(x, y)
			gx, gy := b.At(x, y)
			worst = math.Max(worst, math.Hypot(fx-gx, fy-gy))
			ix, iy := b.Inverse(x+gx, y+gy)
			if math.Hypot(ix-x, iy-y) > 1e-6 {
				t.Fatalf("the inverse of (%v, %v): (%v, %v)", x, y, ix, iy)
			}
		}
	}
	if worst > 0.1 {
		t.Errorf("fitted within %.3f µm", worst)
	}
	// Displacements that cross (two points 10 µm apart moved 30 µm past each other) would fold.
	p = [][2]float64{{100, 100}, {110, 100}, {300, 200}, {50, 250}, {350, 50}}
	d = [][2]float64{{20, 0}, {-20, 0}, {0, 0}, {0, 0}, {0, 0}}
	b, used, err := FitBSpline(p, d, 0, 0, 400, 300, 50, 0.001)
	if err != nil {
		t.Fatal(err)
	}
	if used <= 0.001 || b.minJacobian(0, 0, 400, 300) <= 0.5 {
		t.Errorf("smoothness %v, least Jacobian %v", used, b.minJacobian(0, 0, 400, 300))
	}
}
