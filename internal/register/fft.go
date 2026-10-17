package register

// The discrete Fourier transform of power-of-two sizes (Cooley and Tukey's radix-2, iterative, in
// place), in one and two dimensions, for phase correlation. Its twiddle factors are dmath's sines
// and every product is rounded on its own, so a transform is the same on every processor; rows and
// columns are transformed in parallel, each on its own.

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"

	"situcraft/internal/dmath"
)

// fftPlan holds a size's bit reversal and twiddles: e^(−2πik/n) for k below n/2.
type fftPlan struct {
	n       int
	rev     []int
	cos, sn []float64
}

var plans sync.Map // n → *fftPlan

func planOf(n int) *fftPlan {
	if p, ok := plans.Load(n); ok {
		return p.(*fftPlan)
	}
	if n < 1 || n&(n-1) != 0 {
		panic(fmt.Sprintf("register: an FFT of %d points (a power of two is needed)", n))
	}
	p := &fftPlan{n: n, rev: make([]int, n), cos: make([]float64, n/2), sn: make([]float64, n/2)}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range p.rev {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		p.rev[i] = r
	}
	for k := range p.cos {
		s, c := dmath.SinCos(-2 * math.Pi * float64(k) / float64(n))
		p.cos[k], p.sn[k] = c, s
	}
	plans.Store(n, p)
	return p
}

// fft transforms re + i·im in place; inverse transforms back (scaled by 1/n).
func fft(re, im []float64, inverse bool) {
	n := len(re)
	p := planOf(n)
	for i, r := range p.rev {
		if i < r {
			re[i], re[r] = re[r], re[i]
			im[i], im[r] = im[r], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half, step := size/2, n/size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				c, s := p.cos[k*step], p.sn[k*step]
				if inverse {
					s = -s
				}
				a, b := start+k, start+k+half
				tr := mul(re[b], c) - mul(im[b], s)
				ti := mul(re[b], s) + mul(im[b], c)
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
			}
		}
	}
	if inverse {
		inv := 1 / float64(n)
		for i := range re {
			re[i], im[i] = mul(re[i], inv), mul(im[i], inv)
		}
	}
}

// spectrum is a w × h complex array, row-major.
type spectrum struct {
	w, h   int
	re, im []float64
}

func newSpectrum(w, h int) *spectrum {
	return &spectrum{w: w, h: h, re: make([]float64, w*h), im: make([]float64, w*h)}
}

// parallelFor runs f(i) for i in [0, n) over every core.
func parallelFor(n int, f func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	workers := min(n, runtime.GOMAXPROCS(0))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				f(i)
			}
		}()
	}
	wg.Wait()
}

// fft2 transforms in place: its rows, then (transposed, so that each is a row in memory) its
// columns.
func (s *spectrum) fft2(inverse bool) {
	rows := func(re, im []float64, w, h int) {
		parallelFor(h, func(y int) {
			fft(re[y*w:(y+1)*w], im[y*w:(y+1)*w], inverse)
		})
	}
	rows(s.re, s.im, s.w, s.h)
	tre, tim := transpose(s.re, s.w, s.h), transpose(s.im, s.w, s.h)
	rows(tre, tim, s.h, s.w)
	s.re, s.im = transpose(tre, s.h, s.w), transpose(tim, s.h, s.w)
}

// transpose is a w × h array's transpose, in blocks that stay in the cache.
func transpose(a []float64, w, h int) []float64 {
	const block = 32
	out := make([]float64, len(a))
	parallelFor((h+block-1)/block, func(by int) {
		for bx := 0; bx < w; bx += block {
			for y := by * block; y < min(h, (by+1)*block); y++ {
				for x := bx; x < min(w, bx+block); x++ {
					out[x*h+y] = a[y*w+x]
				}
			}
		}
	})
	return out
}

// nextPow2 is the least power of two at least n.
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
