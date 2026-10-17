package register

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"situcraft/internal/random"
)

// The transformation matrix of the pathology fixture's H&E (Xenium FFPE Human Breast Cancer, Rep1):
// the H&E's pixels (0.3638 µm) to the Xenium image's (0.2125 µm), mirrored top to bottom.
const rep1Matrix = "1.7125010585876286,0.008345081015717404,-10397.19948741904\n0.008345081015717404,-1.7125010585876286,37211.538978661185\n0,0,1\n"

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestAffine(t *testing.T) {
	a := Affine{A: 1.5, B: -0.2, C: 10, D: 0.3, E: 2, F: -4}
	inv, ok := a.Invert()
	if !ok {
		t.Fatal("not invertible")
	}
	for _, p := range [][2]float64{{0, 0}, {123.5, -7}, {1e4, 3e4}} {
		x, y := a.Apply(p[0], p[1])
		bx, by := inv.Apply(x, y)
		if !near(bx, p[0], 1e-9*math.Max(1, math.Abs(p[0]))) || !near(by, p[1], 1e-9*math.Max(1, math.Abs(p[1]))) {
			t.Errorf("%v → (%v, %v) → (%v, %v)", p, x, y, bx, by)
		}
		// Then applies the first transform first.
		u := Scaling(0.2125)
		cx, cy := a.Then(u).Apply(p[0], p[1])
		if !near(cx, 0.2125*x, 1e-9*math.Max(1, math.Abs(x))) || !near(cy, 0.2125*y, 1e-9*math.Max(1, math.Abs(y))) {
			t.Errorf("then: (%v, %v) against (%v, %v)", cx, cy, 0.2125*x, 0.2125*y)
		}
	}
	if _, ok := (Affine{A: 1, B: 2, D: 2, E: 4}).Invert(); ok {
		t.Error("a singular transform inverted")
	}
	raw, _ := json.Marshal(a)
	var back Affine
	if err := json.Unmarshal(raw, &back); err != nil || back != a {
		t.Errorf("JSON %s gives %v, %v", raw, back, err)
	}
}

func TestReadMatrix(t *testing.T) {
	al, err := ReadAlignment([]byte(rep1Matrix))
	if err != nil {
		t.Fatal(err)
	}
	m := al.Matrix
	if al.Kind != "matrix" || !m.Reflected() || !near(m.Scale(), 1.71252, 1e-5) || m.C != -10397.19948741904 {
		t.Errorf("%+v: scale %v, reflected %v", al, m.Scale(), m.Reflected())
	}
	for _, bad := range []string{
		"1,0,0\n0,1,0\n",
		"1,0,0\n0,1,0\n0,0,2\n",
		"1,0,0\n0,1,x\n0,0,1\n",
		"1,2,0\n2,4,0\n0,0,1\n",
		"",
	} {
		if _, err := ReadAlignment([]byte(bad)); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
}

// Keypoints placed exactly give back the similarity they were made with, reflection and all;
// placed with an error, a fit whose residuals are of that error.
func TestFitSimilarity(t *testing.T) {
	al, _ := ReadAlignment([]byte(rep1Matrix))
	want := al.Matrix
	// Rep1's matrix is very nearly a reflected similarity; make it exactly one.
	s, theta := want.Scale(), math.Atan2(want.D, want.A)
	want = Affine{A: s * math.Cos(theta), B: s * math.Sin(theta), C: want.C, D: s * math.Sin(theta), E: -s * math.Cos(theta), F: want.F}
	rnd := random.New(5, 0)
	for _, noise := range []float64{0, 2} {
		var kps []Keypoint
		for i := 0; i < 12; i++ {
			mx, my := 30000*rnd.Float64(), 24000*rnd.Float64()
			fx, fy := want.Apply(mx, my)
			kps = append(kps, Keypoint{Fixed: Point{fx + noise*rnd.Normal(), fy + noise*rnd.Normal()}, Moving: Point{mx, my}})
		}
		got, err := FitSimilarity(kps)
		if err != nil {
			t.Fatal(err)
		}
		res := Residuals(got, kps)
		rms := 0.0
		for _, r := range res {
			rms += r * r
		}
		rms = math.Sqrt(rms / float64(len(res)))
		if noise == 0 {
			for _, pair := range [][2]float64{{got.A, want.A}, {got.B, want.B}, {got.D, want.D}, {got.E, want.E}} {
				if !near(pair[0], pair[1], 1e-12) {
					t.Errorf("exact keypoints: %+v against %+v", got, want)
					break
				}
			}
			if !near(got.C, want.C, 1e-7) || !near(got.F, want.F, 1e-7) || rms > 1e-7 {
				t.Errorf("exact keypoints: translation (%v, %v) against (%v, %v), rms %v", got.C, got.F, want.C, want.F, rms)
			}
		} else if rms < noise || rms > 2*noise || !got.Reflected() {
			t.Errorf("keypoints off by %v pixels: rms %v, reflected %v", noise, rms, got.Reflected())
		}
	}
	// A keypoints file, as Xenium Explorer writes it, fitted the same way.
	var b strings.Builder
	b.WriteString("fixedX,fixedY,alignmentX,alignmentY\n")
	for _, p := range [][2]float64{{1000, 2000}, {20000, 3000}, {15000, 18000}, {4000, 16000}} {
		fx, fy := want.Apply(p[0], p[1])
		fmt.Fprintf(&b, "%.10f,%.10f,%v,%v\n", fx, fy, p[0], p[1])
	}
	file, err := ReadAlignment([]byte(b.String()))
	if err != nil || file.Kind != "keypoints" || len(file.Keypoints) != 4 || len(file.Residuals) != 4 || !near(file.Matrix.A, want.A, 1e-9) {
		t.Errorf("keypoints file: %+v, %v", file, err)
	}
	if _, err := FitSimilarity(kpsAt(Point{1, 1}, 3)); err == nil {
		t.Error("keypoints at one place fitted")
	}
	if _, err := FitSimilarity(kpsAt(Point{1, 1}, 2)); err == nil {
		t.Error("two keypoints fitted")
	}
}

func kpsAt(p Point, n int) []Keypoint {
	out := make([]Keypoint, n)
	for i := range out {
		out[i] = Keypoint{Fixed: p, Moving: p}
	}
	return out
}
