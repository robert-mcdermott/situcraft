package spatial

import (
	"context"
	"math"
	"testing"

	"situcraft/internal/random"
)

func ripleys(t *testing.T, x, y []float32, label []int32, K int, w *Window, p RipleyParams, workers int) *Ripley {
	t.Helper()
	r, err := Ripleys(context.Background(), x, y, label, K, w, p, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// uniform draws n points uniformly in a window (by rejection from its bounding box).
func uniform(s *random.Stream, w *Window, n int, x0, y0, x1, y1 float64) ([]float32, []float32) {
	var x, y []float32
	for len(x) < n {
		px, py := float32(x0+(x1-x0)*s.Float64()), float32(y0+(y1-y0)*s.Float64())
		if w.Contains(float64(px), float64(py)) {
			x, y = append(x, px), append(y, py)
		}
	}
	return x, y
}

// Complete spatial randomness: K(r) = πr², averaged over patterns, in a square and in an L-shaped
// window, with the isotropic correction; without it, K falls short near the edges.
func TestRipleysKOfCompleteSpatialRandomness(t *testing.T) {
	square := rectangleWindow(t, 0, 0, 1000, 1000)
	lshape, err := PolygonWindow([][][][2]float64{{{{0, 0}, {1000, 0}, {1000, 400}, {400, 400}, {400, 1000}, {0, 1000}}}})
	if err != nil {
		t.Fatal(err)
	}
	p := RipleyParams{Radii: []float64{25, 50, 75, 100}, Correction: "isotropic", Permutations: 19}
	none := p
	none.Correction = "none"
	for _, c := range []struct {
		name string
		w    *Window
	}{{"a square", square}, {"an L", lshape}} {
		s := random.New(31, 1)
		const reps = 30
		sum := make([]float64, 4)
		sumNone := make([]float64, 4)
		for rep := 0; rep < reps; rep++ {
			x, y := uniform(s, c.w, 1000, 0, 0, 1000, 1000)
			label := make([]int32, len(x))
			r := ripleys(t, x, y, label, 1, c.w, p, 4)
			rn := ripleys(t, x, y, label, 1, c.w, none, 4)
			for k, rad := range p.Radii {
				sum[k] += r.KObs[k] / (math.Pi * rad * rad)
				sumNone[k] += rn.KObs[k] / (math.Pi * rad * rad)
			}
		}
		for k, rad := range p.Radii {
			ratio, ratioNone := sum[k]/reps, sumNone[k]/reps
			t.Logf("%s, r = %g: K / πr² %.4f (isotropic), %.4f (none)", c.name, rad, ratio, ratioNone)
			if math.Abs(ratio-1) > 0.03 {
				t.Errorf("%s, r = %g: K / πr² = %.4f with the isotropic correction", c.name, rad, ratio)
			}
			if rad >= 50 && !(ratioNone < ratio-0.02) {
				t.Errorf("%s, r = %g: without a correction K / πr² is %.4f, not short of %.4f", c.name, rad, ratioNone, ratio)
			}
		}
	}
}

// A Thomas process (parents of intensity κ, each with Poisson offspring scattered as Gaussians of
// deviation σ): K(r) = πr² + (1 − exp(−r²/4σ²))/κ (Diggle 2003).
func TestRipleysKOfAThomasProcess(t *testing.T) {
	w := rectangleWindow(t, 0, 0, 1000, 1000)
	const kappa, mu, sigma = 20.0 / 1e6, 50.0, 20.0
	p := RipleyParams{Radii: []float64{20, 40, 60, 80}, Correction: "isotropic", Permutations: 19}
	s := random.New(41, 2)
	const reps = 20
	sum := make([]float64, len(p.Radii))
	for rep := 0; rep < reps; rep++ {
		var x, y []float32
		// Parents over a margin of 4σ, so that clusters reach in from beyond the window.
		parents := s.Poisson(kappa * 1160 * 1160)
		for k := 0; k < parents; k++ {
			px, py := -80+1160*s.Float64(), -80+1160*s.Float64()
			for o := s.Poisson(mu); o > 0; o-- {
				ox, oy := px+sigma*s.Normal(), py+sigma*s.Normal()
				if ox >= 0 && ox < 1000 && oy >= 0 && oy < 1000 {
					x, y = append(x, float32(ox)), append(y, float32(oy))
				}
			}
		}
		r := ripleys(t, x, y, make([]int32, len(x)), 1, w, p, 4)
		for k := range p.Radii {
			sum[k] += r.KObs[k]
		}
	}
	for k, rad := range p.Radii {
		want := math.Pi*rad*rad + (1-math.Exp(-rad*rad/(4*sigma*sigma)))/kappa
		got := sum[k] / reps
		t.Logf("r = %g: K %.0f, the process's %.0f", rad, got, want)
		if math.Abs(got/want-1) > 0.1 {
			t.Errorf("r = %g: K %.0f, not within 10%% of %.0f", rad, got, want)
		}
	}
}

// Labels dealt at random lie within their envelope; labels apart (one on the left, one on the
// right) lie below it, their p-value the smallest. The same results on one worker and on eight.
func TestRipleysEnvelopes(t *testing.T) {
	w := rectangleWindow(t, 0, 0, 600, 600)
	s := random.New(51, 3)
	x, y := uniform(s, w, 1200, 0, 0, 600, 600)
	random := make([]int32, len(x))
	apart := make([]int32, len(x))
	for i := range x {
		random[i] = int32(s.IntN(2))
		if x[i] > 300 {
			apart[i] = 1
		}
	}
	p := RipleyParams{Radii: []float64{10, 20, 40, 60}, Correction: "isotropic", Permutations: 199, Seed: 7}
	r := ripleys(t, x, y, random, 2, w, p, 4)
	R := len(p.Radii)
	outside := 0
	for q := 0; q < 4*R; q++ {
		if r.L[q] < r.Lo[q] || r.L[q] > r.Hi[q] {
			outside++
		}
	}
	if outside > 3 || r.P[1] < 0.01 {
		t.Errorf("random labels: %d of %d points outside the envelope; the cross p-value %g", outside, 4*R, r.P[1])
	}
	a := ripleys(t, x, y, apart, 2, w, p, 4)
	if a.P[1] != 1.0/200 || !(a.L[1*R+R-1] < a.Lo[1*R+R-1]) {
		t.Errorf("labels apart: cross L %v, envelope %v to %v, p %g", a.L[R:2*R], a.Lo[R:2*R], a.Hi[R:2*R], a.P[1])
	}
	one := ripleys(t, x, y, apart, 2, w, p, 1)
	for q := range one.L {
		if one.L[q] != a.L[q] || one.Lo[q] != a.Lo[q] || one.Hi[q] != a.Hi[q] || one.Mean[q] != a.Mean[q] {
			t.Fatal("Ripley's K differs on one worker")
		}
	}
	for q := range one.P {
		if one.P[q] != a.P[q] {
			t.Fatal("Ripley's p-values differ on one worker")
		}
	}
}
