// The inverse discrete wavelet transform (T.800 Annex F): the reversible 5/3 filter on integers
// and the irreversible 9/7 filter in float32, both by lifting (F.3.8) with symmetric extension
// (F.3.7). Each level is done in place in the tile-component buffer, whose sub-bands lie in the
// Mallat layout (low-pass samples first in each direction): rows first (HOR_SR), then columns
// (VER_SR), as 2D_SR orders them (F.3.2). Rows and column strips are independent, so they are
// shared among workers without changing the result.

package jp2k

import "math"

// Lifting constants of the 9/7 filter (Table F.4).
const (
	alpha97 = -1.586134342059924
	beta97  = -0.052980118572961
	gamma97 = 0.882911075530934
	delta97 = 0.443506852043971
	k97     = 1.230174104914001
)

// The column strips of the vertical pass are this many samples wide, to stay in cache.
const stripWidth = 64

// The high-pass scaling of the inverse 9/7 transform (F.3.8.2 step 2) is 1/K. OpenJPEG 2.5's
// output corresponds to half of 1.625732422 instead (13318/8192, a 13-bit fixed-point
// approximation of 2/K, 3.3e-5 smaller), and that alone accounts for the differences between the
// two decoders: ±1 in about 1% of the samples of real Xenium tiles, up to ±4 in 16-bit images with
// strong high-frequency content. The tests also decode with that value (Options.opjHighpass) to
// check every other step against OpenJPEG bit for bit; decoding uses the standard's.
const (
	invK97    = 1 / k97
	invK97OPJ = 1.625732422 / 2
)

// idwt reconstructs the tile-component from its sub-bands up to the decoded resolution.
func idwt(tc *tileComp, workers int, invK float32) {
	var ftmp [][]float32
	var itmp [][]int32
	for r := 1; r <= tc.top; r++ {
		res, lo := &tc.res[r], &tc.res[r-1]
		rw, rh := res.x1-res.x0, res.y1-res.y0
		lw, lh := lo.x1-lo.x0, lo.y1-lo.y0
		if rw == 0 || rh == 0 {
			continue
		}
		xodd, yodd := res.x0&1 == 1, res.y0&1 == 1
		n := max(rw, rh*min(stripWidth, rw)) // scratch: a row, or a column strip
		rowChunks := min(workers, rh)
		colChunks := min(workers, ceilDiv(rw, stripWidth))
		nw := max(rowChunks, colChunks)
		if tc.fbuf != nil {
			for len(ftmp) < nw {
				ftmp = append(ftmp, nil)
			}
			buf := tc.fbuf
			parallel(rowChunks, rowChunks, func(w, k int) {
				ftmp[w] = grow(ftmp[w], n)
				for y := k * rh / rowChunks; y < (k+1)*rh/rowChunks; y++ {
					row := buf[y*tc.w : y*tc.w+rw]
					lift97(row[:lw], row[lw:], xodd, invK)
					interleave(row, lw, xodd, ftmp[w])
				}
			})
			parallel(colChunks, colChunks, func(w, k int) {
				ftmp[w] = grow(ftmp[w], n)
				nstrips := ceilDiv(rw, stripWidth)
				for st := k * nstrips / colChunks; st < (k+1)*nstrips/colChunks; st++ {
					xa := st * stripWidth
					vlift97(buf, tc.w, xa, min(xa+stripWidth, rw), lh, rh-lh, yodd, invK, ftmp[w])
				}
			})
		} else {
			for len(itmp) < nw {
				itmp = append(itmp, nil)
			}
			buf := tc.ibuf
			parallel(rowChunks, rowChunks, func(w, k int) {
				itmp[w] = grow(itmp[w], n)
				for y := k * rh / rowChunks; y < (k+1)*rh/rowChunks; y++ {
					row := buf[y*tc.w : y*tc.w+rw]
					lift53(row[:lw], row[lw:], xodd)
					interleave(row, lw, xodd, itmp[w])
				}
			})
			parallel(colChunks, colChunks, func(w, k int) {
				itmp[w] = grow(itmp[w], n)
				nstrips := ceilDiv(rw, stripWidth)
				for st := k * nstrips / colChunks; st < (k+1)*nstrips/colChunks; st++ {
					xa := st * stripWidth
					vlift53(buf, tc.w, xa, min(xa+stripWidth, rw), lh, rh-lh, yodd, itmp[w])
				}
			})
		}
	}
}

func grow[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// interleave turns a row of low-pass samples (the first nl) followed by high-pass samples into
// the interleaved signal; odd means the signal starts at an odd coordinate (a high-pass sample).
func interleave[T int32 | float32](row []T, nl int, odd bool, tmp []T) {
	s, d := row[:nl], row[nl:]
	tmp = tmp[:len(row)]
	if odd {
		for i, v := range d {
			tmp[2*i] = v
		}
		for i, v := range s {
			tmp[2*i+1] = v
		}
	} else {
		for i, v := range s {
			tmp[2*i] = v
		}
		for i, v := range d {
			tmp[2*i+1] = v
		}
	}
	copy(row, tmp)
}

// lift97 runs the 9/7 inverse lifting steps (F.3.8.2) on one signal held as its low-pass samples s
// and high-pass samples d. At each end the missing neighbour of a sample is its other neighbour,
// which is what symmetric extension gives for these symmetric filters.
func lift97(s, d []float32, odd bool, invK float32) {
	if len(s)+len(d) == 1 {
		if len(d) == 1 { // a lone odd sample was doubled by the forward transform (F.3.7)
			d[0] /= 2
		}
		return
	}
	for i := range s {
		s[i] *= k97
	}
	for i := range d {
		d[i] *= invK
	}
	if odd {
		liftR97(s, d, delta97)
		liftL97(d, s, gamma97)
		liftR97(s, d, beta97)
		liftL97(d, s, alpha97)
	} else {
		liftL97(s, d, delta97)
		liftR97(d, s, gamma97)
		liftL97(s, d, beta97)
		liftR97(d, s, alpha97)
	}
}

// liftL97 is a[i] -= c·(b[i-1] + b[i]), for len(a) of len(b) or len(b)+1.
func liftL97(a, b []float32, c float32) {
	nb := len(b)
	a[0] = fms(a[0], c, b[0]+b[0])
	for i := 1; i < nb && i < len(a); i++ {
		a[i] = fms(a[i], c, b[i-1]+b[i])
	}
	if len(a) > nb {
		a[nb] = fms(a[nb], c, b[nb-1]+b[nb-1])
	}
}

// liftR97 is a[i] -= c·(b[i] + b[i+1]), for len(a) of len(b) or len(b)-1.
func liftR97(a, b []float32, c float32) {
	nb := len(b)
	for i := 0; i < len(a) && i+1 < nb; i++ {
		a[i] = fms(a[i], c, b[i]+b[i+1])
	}
	if len(a) == nb {
		a[nb-1] = fms(a[nb-1], c, b[nb-1]+b[nb-1])
	}
}

// lift53 runs the 5/3 inverse lifting steps (F.3.8.1, equations F-5 and F-6).
func lift53(s, d []int32, odd bool) {
	if len(s)+len(d) == 1 {
		if len(d) == 1 {
			// A lone odd sample was doubled by the forward transform (F.3.7), so halving is exact
			// for conformant codestreams. Otherwise SituCraft truncates toward zero, as C's
			// integer division (and OpenJPEG) does.
			d[0] /= 2
		}
		return
	}
	if odd {
		// s[i] -= floor((d[i] + d[i+1] + 2) / 4); d[i] += floor((s[i-1] + s[i]) / 2)
		nd, ns := len(d), len(s)
		for i := range s {
			s[i] -= (d[i] + d[min(i+1, nd-1)] + 2) >> 2
		}
		d[0] += (s[0] + s[0]) >> 1
		for i := 1; i < nd; i++ {
			d[i] += (s[i-1] + s[min(i, ns-1)]) >> 1
		}
	} else {
		// s[i] -= floor((d[i-1] + d[i] + 2) / 4); d[i] += floor((s[i] + s[i+1]) / 2)
		nd, ns := len(d), len(s)
		s[0] -= (d[0] + d[0] + 2) >> 2
		for i := 1; i < ns; i++ {
			s[i] -= (d[i-1] + d[min(i, nd-1)] + 2) >> 2
		}
		for i := range d {
			d[i] += (s[i] + s[min(i+1, ns-1)]) >> 1
		}
	}
}

// vlift97 runs the 9/7 inverse lifting on columns [xa, xb) of a region whose first ns rows are
// low-pass and next nd rows high-pass, then interleaves the rows. Each step works along rows, so
// memory is read in order.
func vlift97(buf []float32, stride, xa, xb, ns, nd int, odd bool, invK float32, tmp []float32) {
	w := xb - xa
	row := func(i int) []float32 { return buf[i*stride+xa : i*stride+xb] }
	if ns+nd == 1 {
		if nd == 1 {
			for x, r := 0, row(0); x < w; x++ {
				r[x] /= 2
			}
		}
		return
	}
	for i := range ns {
		r := row(i)
		for x := range r {
			r[x] *= k97
		}
	}
	for i := ns; i < ns+nd; i++ {
		r := row(i)
		for x := range r {
			r[x] *= invK
		}
	}
	// step lifts rows a0+i (i < na) from their two neighbours among rows b0..b0+nb-1.
	step := func(a0, na, b0, nb int, c float32, left bool) {
		for i := range na {
			l, r := neighbours(i, nb, left)
			ra, rl, rr := row(a0+i), row(b0+l), row(b0+r)
			rl, rr = rl[:len(ra)], rr[:len(ra)]
			for x := range ra {
				ra[x] = fms(ra[x], c, rl[x]+rr[x])
			}
		}
	}
	if odd {
		step(0, ns, ns, nd, delta97, false)
		step(ns, nd, 0, ns, gamma97, true)
		step(0, ns, ns, nd, beta97, false)
		step(ns, nd, 0, ns, alpha97, true)
	} else {
		step(0, ns, ns, nd, delta97, true)
		step(ns, nd, 0, ns, gamma97, false)
		step(0, ns, ns, nd, beta97, true)
		step(ns, nd, 0, ns, alpha97, false)
	}
	interleaveRows(buf, stride, xa, xb, ns, nd, odd, tmp)
}

// vlift53 is vlift97 for the 5/3 filter.
func vlift53(buf []int32, stride, xa, xb, ns, nd int, odd bool, tmp []int32) {
	w := xb - xa
	row := func(i int) []int32 { return buf[i*stride+xa : i*stride+xb] }
	if ns+nd == 1 {
		if nd == 1 { // a lone odd row: see lift53
			for x, r := 0, row(0); x < w; x++ {
				r[x] /= 2
			}
		}
		return
	}
	sStep := func(leftType bool) { // s[i] -= floor((d[l] + d[r] + 2) / 4)
		for i := range ns {
			l, r := neighbours(i, nd, leftType)
			ra, rl, rr := row(i), row(ns+l), row(ns+r)
			rl, rr = rl[:len(ra)], rr[:len(ra)]
			for x := range ra {
				ra[x] -= (rl[x] + rr[x] + 2) >> 2
			}
		}
	}
	dStep := func(leftType bool) { // d[i] += floor((s[l] + s[r]) / 2)
		for i := range nd {
			l, r := neighbours(i, ns, leftType)
			ra, rl, rr := row(ns+i), row(l), row(r)
			rl, rr = rl[:len(ra)], rr[:len(ra)]
			for x := range ra {
				ra[x] += (rl[x] + rr[x]) >> 1
			}
		}
	}
	if odd {
		sStep(false)
		dStep(true)
	} else {
		sStep(true)
		dStep(false)
	}
	interleaveRows(buf, stride, xa, xb, ns, nd, odd, tmp)
}

// interleaveRows reorders rows [0, ns+nd) of columns [xa, xb) from low-pass-first to interleaved.
func interleaveRows[T int32 | float32](buf []T, stride, xa, xb, ns, nd int, odd bool, tmp []T) {
	w := xb - xa
	n := ns + nd
	tmp = tmp[:n*w]
	for i := range n {
		src := i / 2 // the row that lands at position i: low-pass, or high-pass below
		if (i&1 == 1) != odd {
			src += ns
		}
		copy(tmp[i*w:(i+1)*w], buf[src*stride+xa:src*stride+xb])
	}
	for i := range n {
		copy(buf[i*stride+xa:i*stride+xb], tmp[i*w:(i+1)*w])
	}
}

// neighbours returns the indices of the two neighbours of sample i among n samples of the other
// kind: (i-1, i) when left, else (i, i+1), with a missing neighbour replaced by the other one
// (symmetric extension).
func neighbours(i, n int, left bool) (l, r int) {
	l, r = i, i+1
	if left {
		l, r = i-1, i
	}
	if l < 0 {
		l = r
	}
	if r >= n {
		r = l
	}
	return min(l, n-1), min(r, n-1)
}

// fms is a - c·s with a single rounding, as a fused multiply-subtract computes it. It is written
// out so that every platform rounds alike: Go fuses a - c*s on some architectures and not on
// others. The exact product of two float32 values fits a float64, so this is the float32 fused
// result except in vanishingly rare double-rounding cases, which are themselves deterministic.
func fms(a, c, s float32) float32 {
	return float32(math.FMA(float64(-c), float64(s), float64(a)))
}
