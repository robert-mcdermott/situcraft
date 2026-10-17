package register

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"situcraft/internal/random"
)

// A square of landmarks, (0, 0), (2, 0), (0, 2) and (2, 2), each placed where it is but the last,
// placed h = 2 to the right. By hand:
//   - an affine fit puts every landmark h/4 from its place (x' = (1 + h/4)x + (h/4)y − h/4), and
//     each left out is predicted h from it by the three others, which fit exactly;
//   - a similarity fit is x' = (1 + h/8)x + (h/8)y, y' = −(h/8)x + (1 + h/8)y: residuals 0,
//     h√2/4, h√2/4 and h/2; left out, the first is predicted where it is (the others' fit is the
//     same), the second and third h√2/2 away, the last h.
//
// Residuals understate the error where no landmark is: here by a factor of two to four.
func TestFitsByHand(t *testing.T) {
	const h = 2.0
	kps := []Keypoint{
		{Moving: Point{0, 0}, Fixed: Point{0, 0}},
		{Moving: Point{2, 0}, Fixed: Point{2, 0}},
		{Moving: Point{0, 2}, Fixed: Point{0, 2}},
		{Moving: Point{2, 2}, Fixed: Point{2 + h, 2}},
	}
	r2 := math.Sqrt2
	for _, c := range []struct {
		model          Model
		transform      Affine
		residuals, loo []float64
		rms, looRMS    float64
	}{
		{ModelAffine, Affine{A: 1 + h/4, B: h / 4, C: -h / 4, E: 1}, []float64{h / 4, h / 4, h / 4, h / 4}, []float64{h, h, h, h}, h / 4, h},
		{ModelSimilarity, Affine{A: 1 + h/8, B: h / 8, D: -h / 8, E: 1 + h/8}, []float64{0, h * r2 / 4, h * r2 / 4, h / 2}, []float64{0, h * r2 / 2, h * r2 / 2, h}, h * r2 / 4, math.Sqrt((2*h*h/2 + h*h) / 4)},
	} {
		f, err := FitKeypoints(c.model, kps)
		if err != nil {
			t.Fatal(err)
		}
		got := []float64{f.Transform.A, f.Transform.B, f.Transform.C, f.Transform.D, f.Transform.E, f.Transform.F}
		want := []float64{c.transform.A, c.transform.B, c.transform.C, c.transform.D, c.transform.E, c.transform.F}
		for i := range want {
			if !near(got[i], want[i], 1e-12) {
				t.Fatalf("%s: %v, by hand %v", c.model, got, want)
			}
		}
		for i := range kps {
			if !near(f.Residuals[i], c.residuals[i], 1e-12) || !near(f.LeaveOneOut[i], c.loo[i], 1e-12) {
				t.Errorf("%s: residuals %v, left out %v; by hand %v, %v", c.model, f.Residuals, f.LeaveOneOut, c.residuals, c.loo)
				break
			}
		}
		if !near(f.RMS, c.rms, 1e-12) || !near(f.LooRMS, c.looRMS, 1e-12) || !near(f.LooMax, h, 1e-12) {
			t.Errorf("%s: rms %v, left out %v (max %v); by hand %v, %v, %v", c.model, f.RMS, f.LooRMS, f.LooMax, c.rms, c.looRMS, h)
		}
	}
}

// Landmarks placed exactly give back the transform they were made with: similarities, turned and
// reflected, and affine transforms; a similarity fitted to an affine transform's landmarks leaves
// residuals, an affine fit of a similarity's is the similarity.
func TestKnownTransformsRecovered(t *testing.T) {
	rnd := random.New(11, 0)
	same := func(a, b Affine, scale float64) bool {
		return near(a.A, b.A, 1e-9) && near(a.B, b.B, 1e-9) && near(a.D, b.D, 1e-9) && near(a.E, b.E, 1e-9) &&
			near(a.C, b.C, 1e-9*scale) && near(a.F, b.F, 1e-9*scale)
	}
	for trial := 0; trial < 200; trial++ {
		n := 3 + trial%12
		s, theta := 0.2+2*rnd.Float64(), 2*math.Pi*rnd.Float64()
		sim := Affine{A: s * math.Cos(theta), B: -s * math.Sin(theta), C: 5000 * rnd.Float64(), D: s * math.Sin(theta), E: s * math.Cos(theta), F: 5000 * rnd.Float64()}
		if trial%2 == 1 {
			sim.B, sim.E = -sim.B, -sim.E // reflected
		}
		aff := Affine{A: 0.3 + rnd.Float64(), B: 0.6 * (rnd.Float64() - 0.5), C: 4000 * rnd.Float64(), D: 0.6 * (rnd.Float64() - 0.5), E: -(0.3 + rnd.Float64()), F: 4000 * rnd.Float64()}
		var kSim, kAff []Keypoint
		for i := 0; i < n; i++ {
			mx, my := 30000*rnd.Float64(), 24000*rnd.Float64()
			x, y := sim.Apply(mx, my)
			kSim = append(kSim, Keypoint{Moving: Point{mx, my}, Fixed: Point{x, y}})
			x, y = aff.Apply(mx, my)
			kAff = append(kAff, Keypoint{Moving: Point{mx, my}, Fixed: Point{x, y}})
		}
		got, err := FitSimilarity(kSim)
		if err != nil || !same(got, sim, 1e4) || got.Reflected() != sim.Reflected() {
			t.Fatalf("trial %d: a similarity %+v fitted as %+v (%v)", trial, sim, got, err)
		}
		if got, err := FitAffine(kSim); err != nil || !same(got, sim, 1e4) {
			t.Fatalf("trial %d: an affine fit of a similarity's landmarks %+v, not %+v (%v)", trial, got, sim, err)
		}
		got, err = FitAffine(kAff)
		if err != nil || !same(got, aff, 1e4) {
			t.Fatalf("trial %d: an affine transform %+v fitted as %+v (%v)", trial, aff, got, err)
		}
		if n >= 4 {
			f, _ := FitKeypoints(ModelSimilarity, kAff)
			if f.RMS < 1 {
				t.Errorf("trial %d: a similarity fits an affine transform's landmarks to %v", trial, f.RMS)
			}
		}
	}
}

// What a fit needs: three landmarks; an affine transform's, not on a line; leave-one-out errors when
// the others still fit the model.
func TestFitNeeds(t *testing.T) {
	line := []Keypoint{{Moving: Point{0, 0}, Fixed: Point{1, 1}}, {Moving: Point{1, 1}, Fixed: Point{2, 2}}, {Moving: Point{2, 2}, Fixed: Point{3, 3}}, {Moving: Point{5, 5}, Fixed: Point{6, 6}}}
	if _, err := FitAffine(line); err == nil {
		t.Error("landmarks on a line fitted as an affine transform")
	}
	if f, err := FitKeypoints(ModelSimilarity, line); err != nil || f.RMS > 1e-12 {
		t.Errorf("a similarity of landmarks on a line: %+v, %v", f, err)
	}
	three := []Keypoint{{Moving: Point{0, 0}, Fixed: Point{0, 0}}, {Moving: Point{3, 0}, Fixed: Point{3, 0}}, {Moving: Point{0, 2}, Fixed: Point{0, 2.5}}}
	sim, _ := FitKeypoints(ModelSimilarity, three)
	aff, _ := FitKeypoints(ModelAffine, three)
	if len(sim.LeaveOneOut) != 3 || aff.LeaveOneOut != nil || aff.RMS > 1e-12 {
		t.Errorf("three landmarks: a similarity's left out %v; an affine transform's %v, rms %v", sim.LeaveOneOut, aff.LeaveOneOut, aff.RMS)
	}
	if _, err := ParseModel("projective"); err == nil {
		t.Error("an unknown model parsed")
	}
}

// Xenium Explorer's files written as it writes them: the fixture's matrix back to its own text;
// keypoints back to the same numbers.
func TestXeniumExplorerFiles(t *testing.T) {
	al, err := ReadAlignment([]byte(rep1Matrix))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(MatrixCSV(al.Matrix)); got != rep1Matrix {
		t.Errorf("the matrix written:\n%s\nnot\n%s", got, rep1Matrix)
	}
	// 10x's matrix is a similarity, exactly: A = −E, B = D (reflected).
	if m := al.Matrix; m.A != -m.E || m.B != m.D {
		t.Errorf("rep1's matrix is not a reflected similarity: %+v", m)
	}
	kps := []Keypoint{{Fixed: Point{1203.25, 18000.125}, Moving: Point{0.5, 1e-7}}, {Fixed: Point{-3, 4}, Moving: Point{30786, 24241}}, {Fixed: Point{7, 9e22}, Moving: Point{1.0 / 3, 2}}}
	back, err := ReadAlignment(KeypointsCSV(kps))
	if err != nil {
		t.Fatal(err)
	}
	for i := range kps {
		if back.Keypoints[i] != kps[i] {
			t.Errorf("keypoint %d: %+v, written %+v", i, back.Keypoints[i], kps[i])
		}
	}
	if !bytes.HasPrefix(KeypointsCSV(kps), []byte("fixedX,fixedY,alignmentX,alignmentY\n")) {
		t.Error("the keypoints' header")
	}
}

// fitFingerprint is web/lib/transforms.js's checksum of 300 fits of each model (FNV-1a of every
// coefficient's, residual's and leave-one-out error's bits): the page's fits are the program's.
func fitFingerprint() string {
	s := uint64(777)
	next := func() float64 {
		s = s * 48271 % 2147483647
		return float64(s) / 2147483647
	}
	h := uint32(2166136261)
	add := func(v float64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
		for _, x := range b {
			h ^= uint32(x)
			h *= 16777619
		}
	}
	for k := 0; k < 300; k++ {
		n := 3 + k%9
		a := 0.3 + next()
		b := mul(0.4, next()-0.5)
		c := mul(2000, next())
		d := mul(0.4, next()-0.5)
		e := 0.3 + next()
		if k%2 == 0 {
			e = -e
		}
		f := mul(2000, next())
		kps := make([]Keypoint, n)
		for i := range kps {
			mx := mul(30000, next())
			my := mul(24000, next())
			nx := mul(next()-0.5, 4)
			ny := mul(next()-0.5, 4)
			kps[i] = Keypoint{Moving: Point{mx, my}, Fixed: Point{mul(a, mx) + mul(b, my) + c + nx, mul(d, mx) + mul(e, my) + f + ny}}
		}
		for _, model := range []Model{ModelSimilarity, ModelAffine} {
			fit, err := FitKeypoints(model, kps)
			if err != nil {
				add(-1)
				continue
			}
			t := fit.Transform
			for _, v := range []float64{t.A, t.B, t.C, t.D, t.E, t.F, fit.RMS, fit.Max, fit.LooRMS, fit.LooMax} {
				add(v)
			}
			for _, v := range fit.Residuals {
				add(v)
			}
			for _, v := range fit.LeaveOneOut {
				add(v)
			}
		}
	}
	return fmt.Sprintf("%08x", h)
}

func TestFitFingerprint(t *testing.T) {
	if got := fitFingerprint(); got != "5c09decb" {
		t.Fatalf("fit fingerprint %s, the page's 5c09decb", got)
	}
}
