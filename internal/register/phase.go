package register

// Phase correlation (Kuglin CD, Hines DC. The phase correlation image alignment method. Proc IEEE
// Conf Cybernetics and Society 1975): the shift between two images is where the inverse transform
// of their normalized cross-power spectrum peaks. Its log-polar form (Reddy BS, Chatterji BN. An
// FFT-based technique for translation, rotation, and scale-invariant image registration. IEEE Trans
// Image Process 1996;5(8):1266–1271): the magnitude spectra, which a shift leaves alone, resampled in
// log-polar coordinates, where a turn and a scale become a shift, found the same way.

import (
	"math"
	"sync"

	"situcraft/internal/dmath"
)

// canvas is a plane centred in an n × n spectrum, windowed over its own extent (Hann's, each way),
// its mean over its image removed; where it is not image, 0.
func canvas(p Plane, n int) *spectrum {
	s := newSpectrum(n, n)
	var sum float64
	count := 0
	for i, v := range p.Pix {
		if p.valid(i) {
			sum += float64(v)
			count++
		}
	}
	mean := 0.0
	if count > 0 {
		mean = sum / float64(count)
	}
	wx, wy := hann(p.W), hann(p.H)
	ox, oy := (n-p.W)/2, (n-p.H)/2
	for y := 0; y < p.H; y++ {
		for x := 0; x < p.W; x++ {
			i := y*p.W + x
			if !p.valid(i) {
				continue
			}
			X, Y := x+ox, y+oy
			if X < 0 || Y < 0 || X >= n || Y >= n {
				continue
			}
			s.re[Y*n+X] = mul(mul(float64(p.Pix[i])-mean, wx[x]), wy[y])
		}
	}
	return s
}

// peak is the shift where a correlation surface (an inverse transform, origin at index 0) peaks,
// to a fraction of a sample, and how far the peak stands above the surface (its height over the
// surface's standard deviation). The fraction is Foroosh, Zerubia and Berthod's (Extension of phase
// correlation to subpixel registration. IEEE Trans Image Process 2002;11(3):188–200): the surface
// about a peak between samples is a sinc, and the peak lies toward its larger neighbour by that
// neighbour's share of the two.
func peak(c []float64, w, h int) (dx, dy, quality float64) {
	best, at := math.Inf(-1), 0
	var sum, sq float64
	for i, v := range c {
		if v > best {
			best, at = v, i
		}
		sum += v
		sq += mul(v, v)
	}
	n := float64(len(c))
	mean := sum / n
	sd := math.Sqrt(math.Max(0, sq/n-mul(mean, mean)))
	px, py := at%w, at/w
	val := func(x, y int) float64 { return c[((y+h)%h)*w+(x+w)%w] }
	sub := func(m, z, p float64) float64 {
		if z <= 0 {
			return 0
		}
		if p >= m && p > 0 {
			return p / (p + z)
		}
		if m > 0 {
			return -m / (m + z)
		}
		return 0
	}
	fx := float64(px) + sub(val(px-1, py), best, val(px+1, py))
	fy := float64(py) + sub(val(px, py-1), best, val(px, py+1))
	if fx >= float64(w)/2 {
		fx -= float64(w)
	}
	if fy >= float64(h)/2 {
		fy -= float64(h)
	}
	if sd > 0 {
		quality = (best - mean) / sd
	}
	return fx, fy, quality
}

// crossPower is the inverse transform of a's and b's normalized cross-power spectrum (both
// transformed already): it peaks at the shift d where a(x) ≈ b(x − d).
func crossPower(a, b *spectrum) []float64 {
	r := newSpectrum(a.w, a.h)
	for i := range a.re {
		re := mul(a.re[i], b.re[i]) + mul(a.im[i], b.im[i])
		im := mul(a.im[i], b.re[i]) - mul(a.re[i], b.im[i])
		m := math.Sqrt(mul(re, re) + mul(im, im))
		if m > 1e-20 {
			r.re[i], r.im[i] = re/m, im/m
		}
	}
	r.fft2(true)
	return r.re
}

// PhaseCorrelate is the shift (dx, dy), in pixels, by which b's content lies from a's (a(x) ≈
// b(x − d)), two planes centred in a canvas large enough for both, and how far its peak stands out.
// The shift is known up to the canvas's size (a correlation wraps around).
func PhaseCorrelate(a, b Plane) (dx, dy, quality float64) {
	return newTransformed(a).phaseCorrelate(b)
}

// transformed is a plane with its canvases' transforms, by canvas size, made when first asked: a
// plane correlated with many others is transformed once.
type transformed struct {
	p        Plane
	spectra  map[int]*spectrum
	logPolar map[int]*spectrum
}

func newTransformed(p Plane) *transformed {
	return &transformed{p: p, spectra: map[int]*spectrum{}, logPolar: map[int]*spectrum{}}
}

func (t *transformed) spectrum(n int) *spectrum {
	if s, ok := t.spectra[n]; ok {
		return s
	}
	s := canvas(t.p, n)
	s.fft2(false)
	t.spectra[n] = s
	return s
}

// logPolarSpectrum is the transform of the log-polar form of the canvas's magnitude spectrum.
func (t *transformed) logPolarSpectrum(n int) *spectrum {
	if s, ok := t.logPolar[n]; ok {
		return s
	}
	s := canvasRaw(logPolar(t.spectrum(n)))
	s.fft2(false)
	t.logPolar[n] = s
	return s
}

func (t *transformed) phaseCorrelate(b Plane) (dx, dy, quality float64) {
	a := t.p
	n := nextPow2(max(a.W, a.H, b.W, b.H))
	sb := canvas(b, n)
	sb.fft2(false)
	dx, dy, quality = peak(crossPower(t.spectrum(n), sb), n, n)
	// The canvases' shift less where each plane was placed in its canvas.
	dx -= float64((n-a.W)/2 - (n-b.W)/2)
	dy -= float64((n-a.H)/2 - (n-b.H)/2)
	return dx, dy, quality
}

// polarTables are what a canvas size's log-polar forms need: the high-pass filter's cosines along
// each axis, and the turns' cosines and sines and the radii sampled.
type polarTables struct {
	cosAxis          []float64
	cosTurn, sinTurn []float64
	radius           []float64
	logMax           float64
}

var polar sync.Map // n → *polarTables

func polarOf(n int) *polarTables {
	if t, ok := polar.Load(n); ok {
		return t.(*polarTables)
	}
	nθ, nρ := n/2, n/2
	t := &polarTables{cosAxis: make([]float64, n), cosTurn: make([]float64, nθ), sinTurn: make([]float64, nθ), radius: make([]float64, nρ), logMax: dmath.Log(float64(n)/2 - 1)}
	for i := range t.cosAxis {
		f := float64((i+n/2)%n-n/2) / float64(n)
		_, t.cosAxis[i] = dmath.SinCos(mul(math.Pi, f))
	}
	for k := range t.cosTurn {
		t.sinTurn[k], t.cosTurn[k] = dmath.SinCos(mul(math.Pi, float64(k)/float64(nθ)))
	}
	for k := range t.radius {
		t.radius[k] = dmath.Exp(mul(t.logMax, float64(k)/float64(nρ)))
	}
	polar.Store(n, t)
	return t
}

// logPolar is a transformed canvas's magnitude spectrum, emphasized at high frequencies (Reddy and
// Chatterji's filter, which also hides the window's and the edges' low frequencies), resampled at
// n/2 turns over half a turn (the spectrum of a real image is symmetric) and n/2 radii spaced
// evenly in their logarithm from 1 to n/2 − 1.
func logPolar(s *spectrum) Plane {
	n := s.w
	tab := polarOf(n)
	src := Plane{W: n, H: n, Pix: make([]float32, n*n)} // shifted: the zero frequency at (n/2, n/2)
	parallelFor(n, func(y int) {
		cy := tab.cosAxis[y]
		for x := 0; x < n; x++ {
			X := mul(tab.cosAxis[x], cy)
			i := y*n + x
			m := math.Sqrt(mul(s.re[i], s.re[i]) + mul(s.im[i], s.im[i]))
			src.Pix[((y+n/2)%n)*n+(x+n/2)%n] = float32(mul(mul(1-X, 2-X), m))
		}
	})
	nθ, nρ := n/2, n/2
	out := NewPlane(nθ, nρ)
	parallelFor(nρ, func(k int) {
		r := tab.radius[k]
		for t := 0; t < nθ; t++ {
			// Pixel centres: the zero frequency is the centre of pixel (n/2, n/2).
			if v, ok := src.Sample(float64(n/2)+0.5+mul(r, tab.cosTurn[t]), float64(n/2)+0.5+mul(r, tab.sinTurn[t])); ok {
				out.Pix[k*nθ+t] = float32(v)
			}
		}
	})
	return out
}

// TurnAndScale is how b is turned and scaled from a (planes of one pixel size): b is a turned by
// turn radians (counter-clockwise as the planes' x and y axes go, known only up to half a turn,
// since magnitude spectra cannot tell) and enlarged by scale, as the log-polar forms of their
// magnitude spectra correlate; with how far the peak stands out.
func TurnAndScale(a, b Plane) (turn, scale, quality float64) {
	return newTransformed(a).turnAndScale(b)
}

func (t *transformed) turnAndScale(b Plane) (turn, scale, quality float64) {
	n := nextPow2(max(t.p.W, t.p.H, b.W, b.H))
	nθ, nρ := n/2, n/2
	dθ, dρ, q := peak(crossPower(t.logPolarSpectrum(n), newTransformed(b).logPolarSpectrum(n)), nθ, nρ)
	logMax := polarOf(n).logMax
	// b's log-polar form lies from a's by the turn back and the enlargement's shrinking.
	turn = -mul(math.Pi, dθ/float64(nθ))
	scale = dmath.Exp(mul(logMax, dρ/float64(nρ)))
	return turn, scale, q
}

// canvasRaw is a plane as a spectrum of its own size, its mean removed, not windowed (a log-polar
// form wraps around in its turn).
func canvasRaw(p Plane) *spectrum {
	s := newSpectrum(p.W, p.H)
	var sum float64
	for _, v := range p.Pix {
		sum += float64(v)
	}
	mean := sum / float64(len(p.Pix))
	for i, v := range p.Pix {
		s.re[i] = float64(v) - mean
	}
	return s
}
