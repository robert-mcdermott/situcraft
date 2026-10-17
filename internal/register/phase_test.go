package register

import (
	"math"
	"testing"

	"situcraft/internal/random"
)

// blobs is a plane of nuclei-like spots: normal bumps of random size and brightness.
func blobs(w, h, n int, seed uint64) Plane {
	p := NewPlane(w, h)
	s := random.New(seed, 0)
	for k := 0; k < n; k++ {
		cx, cy, r, a := float64(w)*s.Float64(), float64(h)*s.Float64(), 2+4*s.Float64(), 0.5+s.Float64()
		for y := max(0, int(cy-3*r)); y < min(h, int(cy+3*r)+1); y++ {
			for x := max(0, int(cx-3*r)); x < min(w, int(cx+3*r)+1); x++ {
				dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
				p.Pix[y*w+x] += float32(a * math.Exp(-(dx*dx+dy*dy)/(2*r*r)))
			}
		}
	}
	return p
}

// The transform is the discrete Fourier transform, computed directly; and back.
func TestFFT(t *testing.T) {
	for _, n := range []int{1, 2, 8, 64} {
		re, im := make([]float64, n), make([]float64, n)
		s := random.New(uint64(n), 0)
		for i := range re {
			re[i], im[i] = s.Normal(), s.Normal()
		}
		r2, i2 := append([]float64(nil), re...), append([]float64(nil), im...)
		fft(r2, i2, false)
		for k := 0; k < n; k++ {
			var sr, si float64
			for j := 0; j < n; j++ {
				a := -2 * math.Pi * float64(j*k) / float64(n)
				sr += re[j]*math.Cos(a) - im[j]*math.Sin(a)
				si += re[j]*math.Sin(a) + im[j]*math.Cos(a)
			}
			if math.Abs(sr-r2[k]) > 1e-9 || math.Abs(si-i2[k]) > 1e-9 {
				t.Fatalf("n %d, k %d: (%v, %v), directly (%v, %v)", n, k, r2[k], i2[k], sr, si)
			}
		}
		fft(r2, i2, true)
		for j := range re {
			if math.Abs(r2[j]-re[j]) > 1e-12 || math.Abs(i2[j]-im[j]) > 1e-12 {
				t.Fatalf("n %d: back %v, not %v", n, r2[j], re[j])
			}
		}
	}
}

// A shift found to a tenth of a pixel, whole and in part, and its sign: a(x) ≈ b(x − d).
func TestPhaseCorrelate(t *testing.T) {
	a := blobs(200, 150, 120, 1)
	for _, d := range [][2]float64{{0, 0}, {7, -3}, {-12.4, 5.7}, {30.25, 20.5}} {
		b := a.Warp(Affine{A: 1, C: d[0], E: 1, F: d[1]}, 200, 150) // b(x) = a(x + d)
		dx, dy, q := PhaseCorrelate(a, b)
		// b(x) = a(x + d): a(x) = b(x − d).
		if math.Abs(dx-d[0]) > 0.12 || math.Abs(dy-d[1]) > 0.12 || q < 8 {
			t.Errorf("shift %v: found (%.2f, %.2f), quality %.1f", d, dx, dy, q)
		}
	}
	// Planes of different sizes: b a part of a.
	b := a.Warp(Affine{A: 1, C: 40, E: 1, F: 30}, 100, 80)
	dx, dy, _ := PhaseCorrelate(a, b)
	if math.Abs(dx-40) > 0.15 || math.Abs(dy-30) > 0.15 {
		t.Errorf("a part: (%.2f, %.2f), not (40, 30)", dx, dy)
	}
}

// A turn and an enlargement found from the log-polar spectra, up to half a turn.
func TestTurnAndScale(t *testing.T) {
	a := blobs(256, 256, 300, 2)
	for _, c := range []struct{ turn, scale float64 }{{0, 1}, {0.4, 1}, {-0.9, 1.3}, {1.2, 0.8}, {2.5, 1.1}} {
		// b is a turned by turn and enlarged by scale about the centre: b(S(v)) = a(v).
		sn, cs := math.Sin(c.turn), math.Cos(c.turn)
		S := Affine{A: c.scale * cs, B: -c.scale * sn, D: c.scale * sn, E: c.scale * cs}
		S.C, S.F = 128-(S.A*128+S.B*128), 128-(S.D*128+S.E*128)
		inv, _ := S.Invert()
		b := a.Warp(inv, 256, 256)
		turn, scale, q := TurnAndScale(a, b)
		dt := math.Mod(turn-c.turn+4*math.Pi, math.Pi)
		dt = math.Min(dt, math.Pi-dt)
		t.Logf("turned %.2f, scaled %.2f: found %.3f (mod π), %.3f, quality %.1f", c.turn, c.scale, turn, scale, q)
		if dt > 0.02 || math.Abs(scale/c.scale-1) > 0.02 {
			t.Errorf("turned %.2f, scaled %.2f: found %.3f, %.3f", c.turn, c.scale, turn, scale)
		}
	}
}
