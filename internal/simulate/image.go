package simulate

// The morphology image: a DAPI stain of the visible nuclei. Each nucleus has a brightness
// (log-normal about 1,800), brighter at its centre (down to two thirds at its edge) and grainy
// (each pixel times 1 plus a normal of σ 0.15, chromatin); the stain is blurred (two passes of a
// 3 × 3 mean, the optics), laid on a background of 80, and given each pixel's camera noise (a
// normal of σ 6), in 16 bits.

import (
	"math"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
	"situcraft/internal/tiff"
)

func (t *tissue) stain() *tiff.Plane16 {
	p := t.p
	ps := p.PixelUm
	img := make([]float32, t.w*t.h)
	for k := range t.cells {
		c := &t.cells[k]
		if !c.nucleus {
			continue
		}
		s := random.New(p.Seed, streamImage+uint64(k))
		bright := s.LogNormal(dmath.Log(1800), 0.25)
		for _, i := range c.nucleusPixel {
			x, y := int(i)%t.w, int(i)/t.w
			dx, dy := mul(float64(x)+0.5, ps)-c.nx, mul(float64(y)+0.5, ps)-c.ny
			u := mul(dx, c.ux) + mul(dy, c.uy)
			v := mul(dy, c.ux) - mul(dx, c.uy)
			q := math.Min(1, mul(u/c.na, u/c.na)+mul(v/c.nb, v/c.nb))
			img[i] = float32(mul(mul(bright, 1-mul(0.35, q)), math.Max(0, 1+mul(0.15, s.Normal()))))
		}
	}
	for pass := 0; pass < 2; pass++ {
		img = boxBlur(img, t.w, t.h)
	}
	out := &tiff.Plane16{Width: t.w, Height: t.h, Pix: make([]uint16, t.w*t.h)}
	for y := 0; y < t.h; y++ {
		s := random.New(p.Seed, streamImage+1<<30+uint64(y))
		for x := 0; x < t.w; x++ {
			v := 80 + float64(img[y*t.w+x]) + mul(6, s.Normal())
			out.Pix[y*t.w+x] = uint16(math.Max(0, math.Min(65535, math.Round(v))))
		}
	}
	return out
}

// boxBlur is the mean of each pixel's 3 × 3 neighbourhood (within the image).
func boxBlur(in []float32, w, h int) []float32 {
	out := make([]float32, len(in))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum float64
			n := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if sx, sy := x+dx, y+dy; sx >= 0 && sy >= 0 && sx < w && sy < h {
						sum += float64(in[sy*w+sx])
						n++
					}
				}
			}
			out[y*w+x] = float32(sum / float64(n))
		}
	}
	return out
}
