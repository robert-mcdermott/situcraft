// The output stage (T.800 Annex G): the inverse component transforms (RCT, G.2; ICT, G.3), the
// DC level shift (G.1.2), and rounding and clamping to each component's range.

package jp2k

import (
	"fmt"
	"math"
)

// inverseMCT applies the tile's multiple component transform to components 0 to 2. Like other
// decoders, SituCraft ignores a component transform signalled for fewer than three components
// (the standard requires at least three).
func inverseMCT(t *tile) error {
	if t.mct == 0 || len(t.comps) < 3 {
		return nil
	}
	c0, c1, c2 := &t.comps[0], &t.comps[1], &t.comps[2]
	if c0.w != c1.w || c0.w != c2.w || c0.h != c1.h || c0.h != c2.h {
		return fmt.Errorf("jp2k: tile %d: the component transform needs components 0 to 2 of equal size", t.idx)
	}
	switch {
	case c0.ibuf != nil && c1.ibuf != nil && c2.ibuf != nil: // RCT (G-6)
		y0, y1, y2 := c0.ibuf, c1.ibuf[:len(c0.ibuf)], c2.ibuf[:len(c0.ibuf)]
		for i := range y0 {
			g := y0[i] - (y2[i]+y1[i])>>2
			y0[i], y1[i], y2[i] = y2[i]+g, g, y1[i]+g
		}
	case c0.fbuf != nil && c1.fbuf != nil && c2.fbuf != nil: // ICT (G-9)
		y0, y1, y2 := c0.fbuf, c1.fbuf[:len(c0.fbuf)], c2.fbuf[:len(c0.fbuf)]
		for i := range y0 {
			y, cb, cr := y0[i], y1[i], y2[i]
			y0[i] = fms(y, -1.402, cr)
			y1[i] = fms(fms(y, 0.34413, cb), 0.71414, cr)
			y2[i] = fms(y, -1.772, cb)
		}
	default:
		return fmt.Errorf("jp2k: tile %d: the component transform needs components 0 to 2 to use the same wavelet filter", t.idx)
	}
	return nil
}

// placement returns where tile-component tc lands in its component image of size w×h.
func (d *decoder) placement(t *tile, tc *tileComp, w, h int) (ox, oy int, err error) {
	sc := d.siz.comps[tc.c]
	res := &tc.res[tc.top]
	ox = res.x0 - ceilShift(ceilDiv(d.siz.x0, sc.dx), d.opts.Reduce)
	oy = res.y0 - ceilShift(ceilDiv(d.siz.y0, sc.dy), d.opts.Reduce)
	if ox < 0 || oy < 0 || ox+tc.w > w || oy+tc.h > h {
		return 0, 0, fmt.Errorf("jp2k: internal error: tile %d does not fit component %d", t.idx, tc.c)
	}
	return ox, oy, nil
}

// sampleRange is the DC level shift (G.1.2) and the range of a component's samples.
func sampleRange(sc sizComp) (shift, lo, hi int32) {
	if sc.signed {
		return 0, -1 << (sc.prec - 1), 1<<(sc.prec-1) - 1
	}
	return 1 << (sc.prec - 1), 0, int32(uint32(1)<<sc.prec - 1)
}

// roundClamp rounds f to the nearest integer, halves to even, adds the DC level shift and clamps
// to [lo, hi]. Rounding the unshifted value (exactly, in float64) keeps the precision that adding
// a large shift in float32 would lose; ties to even is what C's lrintf does, and it matched the
// reference decoder best on the development data.
func roundClamp(f float32, shift, lo, hi int32) int32 {
	r := math.RoundToEven(float64(f)) + float64(shift)
	if !(r > float64(lo)) { // also catches NaN
		return lo
	}
	if r >= float64(hi) {
		return hi
	}
	return int32(r)
}

// storeInt32 finishes tile t (component transform, DC level shift, rounding and clamping) into
// the int32 component planes of img.
func storeInt32(d *decoder, t *tile, img *Image) error {
	if err := inverseMCT(t); err != nil {
		return err
	}
	for c := range t.comps {
		tc := &t.comps[c]
		comp := &img.Components[c]
		ox, oy, err := d.placement(t, tc, comp.Width, comp.Height)
		if err != nil {
			return err
		}
		shift, lo, hi := sampleRange(d.siz.comps[c])
		for y := range tc.h {
			dst := comp.Data[(oy+y)*comp.Width+ox:][:tc.w]
			if tc.ibuf != nil {
				src := tc.ibuf[y*tc.w:][:tc.w]
				for x, v := range src {
					dst[x] = min(max(v+shift, lo), hi)
				}
			} else {
				for x, v := range tc.fbuf[y*tc.w:][:tc.w] {
					dst[x] = roundClamp(v, shift, lo, hi)
				}
			}
		}
	}
	return nil
}

// storeGray16 finishes tile t of a one-component image of at most 16 unsigned bits into pix.
func storeGray16(d *decoder, t *tile, pix []uint16, width, height int) error {
	tc := &t.comps[0]
	ox, oy, err := d.placement(t, tc, width, height)
	if err != nil {
		return err
	}
	shift, lo, hi := sampleRange(d.siz.comps[0])
	for y := range tc.h {
		dst := pix[(oy+y)*width+ox:][:tc.w]
		if tc.ibuf != nil {
			src := tc.ibuf[y*tc.w:][:tc.w]
			for x, v := range src {
				dst[x] = uint16(min(max(v+shift, lo), hi))
			}
		} else {
			for x, v := range tc.fbuf[y*tc.w:][:tc.w] {
				dst[x] = uint16(roundClamp(v, shift, lo, hi))
			}
		}
	}
	return nil
}
