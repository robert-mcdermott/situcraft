package register

// Grey images for registration: a plane of float32 samples, its pixel (i, j) covering [i, i+1) ×
// [j, j+1) of its own continuous coordinates (so its centre is at (i + 0.5, j + 0.5)), and the
// operations registration needs, each a fixed sequence of rounded operations.

import (
	"math"
	"slices"

	"situcraft/internal/dmath"
)

// Plane is a grey image; Mask marks the samples that are image (nil: all of them).
type Plane struct {
	W, H int
	Pix  []float32
	Mask []bool
}

// NewPlane is a w × h plane of zeros, all of it image.
func NewPlane(w, h int) Plane {
	return Plane{W: w, H: h, Pix: make([]float32, w*h)}
}

func (p Plane) valid(i int) bool { return p.Mask == nil || p.Mask[i] }

// Sample is the plane's value at (x, y) of its continuous coordinates, interpolated between the
// four pixel centres about it; false where any of them is not image.
func (p Plane) Sample(x, y float64) (float64, bool) {
	fx, fy := x-0.5, y-0.5
	x0, y0 := math.Floor(fx), math.Floor(fy)
	ix, iy := int(x0), int(y0)
	if ix < 0 || iy < 0 || ix+1 >= p.W || iy+1 >= p.H {
		// At the outer half-pixel the nearest edge pixels stand in.
		if fx < -0.5 || fy < -0.5 || fx > float64(p.W)-0.5 || fy > float64(p.H)-0.5 || p.W < 2 || p.H < 2 {
			return 0, false
		}
		ix, iy = min(max(ix, 0), p.W-2), min(max(iy, 0), p.H-2)
		fx, fy = math.Min(math.Max(fx, 0), float64(p.W-1)), math.Min(math.Max(fy, 0), float64(p.H-1))
		x0, y0 = float64(ix), float64(iy)
	}
	i := iy*p.W + ix
	if !p.valid(i) || !p.valid(i+1) || !p.valid(i+p.W) || !p.valid(i+p.W+1) {
		return 0, false
	}
	ax, ay := fx-x0, fy-y0
	top := float64(p.Pix[i]) + mul(ax, float64(p.Pix[i+1])-float64(p.Pix[i]))
	bot := float64(p.Pix[i+p.W]) + mul(ax, float64(p.Pix[i+p.W+1])-float64(p.Pix[i+p.W]))
	return top + mul(ay, bot-top), true
}

// Downsample is the plane at 1/f of its size (f ≥ 1, rounded up), each pixel the mean of the area it
// covers (pixels partly covered weighed by the part); a pixel covering no image is not image.
func (p Plane) Downsample(f float64) Plane {
	if f <= 1 {
		return p
	}
	w, h := int(math.Ceil(float64(p.W)/f)), int(math.Ceil(float64(p.H)/f))
	type span struct {
		from, to int
		weights  []float64
	}
	spans := func(n, out int) []span {
		s := make([]span, out)
		for o := range s {
			a, b := mul(float64(o), f), math.Min(mul(float64(o+1), f), float64(n))
			s[o].from, s[o].to = int(math.Floor(a)), min(n, int(math.Ceil(b)))
			for k := s[o].from; k < s[o].to; k++ {
				s[o].weights = append(s[o].weights, math.Min(b, float64(k+1))-math.Max(a, float64(k)))
			}
		}
		return s
	}
	sx, sy := spans(p.W, w), spans(p.H, h)
	out := Plane{W: w, H: h, Pix: make([]float32, w*h), Mask: make([]bool, w*h)}
	parallelFor(h, func(j int) {
		for i := 0; i < w; i++ {
			var sum, weight float64
			for b, y := 0, sy[j].from; y < sy[j].to; b, y = b+1, y+1 {
				for a, x := 0, sx[i].from; x < sx[i].to; a, x = a+1, x+1 {
					k := y*p.W + x
					if p.valid(k) {
						wt := mul(sx[i].weights[a], sy[j].weights[b])
						sum += mul(wt, float64(p.Pix[k]))
						weight += wt
					}
				}
			}
			if weight > 0 {
				out.Pix[j*w+i], out.Mask[j*w+i] = float32(sum/weight), true
			}
		}
	})
	return out
}

// Blur is the plane after three passes of a (2r + 1)-wide mean along each axis (close to a normal
// of σ = r), each pixel the mean of the image about it.
func (p Plane) Blur(r int) Plane {
	if r < 1 {
		return p
	}
	out := Plane{W: p.W, H: p.H, Pix: append([]float32(nil), p.Pix...), Mask: p.Mask}
	pass := func(n, stride, count, step int) {
		parallelFor(count, func(line int) {
			base := line * step
			vals := make([]float64, n)
			ok := make([]bool, n)
			for k := 0; k < n; k++ {
				i := base + k*stride
				vals[k], ok[k] = float64(out.Pix[i]), out.valid(i)
			}
			for k := 0; k < n; k++ {
				var sum float64
				m := 0
				for d := -r; d <= r; d++ {
					if q := k + d; q >= 0 && q < n && ok[q] {
						sum += vals[q]
						m++
					}
				}
				if m > 0 {
					out.Pix[base+k*stride] = float32(sum / float64(m))
				}
			}
		})
	}
	for t := 0; t < 3; t++ {
		pass(p.W, 1, p.H, p.W)
		pass(p.H, p.W, p.W, 1)
	}
	return out
}

// Percentile is the q-quantile (0 to 1) of the image's samples.
func (p Plane) Percentile(q float64) float64 {
	return p.Percentiles(q)[0]
}

// Percentiles are the quantiles (0 to 1) of the image's samples, from one sort.
func (p Plane) Percentiles(qs ...float64) []float64 {
	vals := make([]float32, 0, len(p.Pix))
	for i, v := range p.Pix {
		if p.valid(i) {
			vals = append(vals, v)
		}
	}
	out := make([]float64, len(qs))
	if len(vals) == 0 {
		return out
	}
	slices.Sort(vals)
	for k, q := range qs {
		out[k] = float64(vals[min(len(vals)-1, int(mul(q, float64(len(vals)))))])
	}
	return out
}

// Normalized is the plane mapped from [lo, hi] onto [0, 1], clamped.
func (p Plane) Normalized(lo, hi float64) Plane {
	out := Plane{W: p.W, H: p.H, Pix: make([]float32, len(p.Pix)), Mask: p.Mask}
	span := hi - lo
	if !(span > 0) {
		span = 1
	}
	for i, v := range p.Pix {
		out.Pix[i] = float32(math.Min(1, math.Max(0, (float64(v)-lo)/span)))
	}
	return out
}

// FlipY is the plane upside down.
func (p Plane) FlipY() Plane {
	out := Plane{W: p.W, H: p.H, Pix: make([]float32, len(p.Pix))}
	if p.Mask != nil {
		out.Mask = make([]bool, len(p.Mask))
	}
	for y := 0; y < p.H; y++ {
		copy(out.Pix[(p.H-1-y)*p.W:(p.H-y)*p.W], p.Pix[y*p.W:(y+1)*p.W])
		if p.Mask != nil {
			copy(out.Mask[(p.H-1-y)*p.W:(p.H-y)*p.W], p.Mask[y*p.W:(y+1)*p.W])
		}
	}
	return out
}

// Warp is a w × h plane whose pixel (i, j) is p at toSrc of its centre (i + 0.5, j + 0.5); not image
// where p is not.
func (p Plane) Warp(toSrc Affine, w, h int) Plane {
	out := Plane{W: w, H: h, Pix: make([]float32, w*h), Mask: make([]bool, w*h)}
	parallelFor(h, func(j int) {
		for i := 0; i < w; i++ {
			x, y := toSrc.Apply(float64(i)+0.5, float64(j)+0.5)
			if v, ok := p.Sample(x, y); ok {
				out.Pix[j*w+i], out.Mask[j*w+i] = float32(v), true
			}
		}
	})
	return out
}

// hann is Hann's window over n samples: 0 at its ends, 1 in its middle.
func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		_, c := dmath.SinCos(2 * math.Pi * float64(i) / float64(max(1, n-1)))
		w[i] = 0.5 - mul(0.5, c)
	}
	return w
}
