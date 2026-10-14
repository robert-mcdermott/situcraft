// Tier 1 (T.800 Annex D): coefficient bit modelling of one code-block, with the three coding
// passes, the context tables D.1 to D.4 and every code-block style flag (Table A.19). Decoded
// magnitudes carry one extra low bit so that the reconstruction rule of E.1.1.2 with r = 1/2
// (the midpoint of the remaining uncertainty interval, the usual decoder choice) is exact.
//
// The coding passes scan stripes of four rows column by column (D.2), so the state of the
// coefficients is kept per stripe column: one 32-bit word holds the four rows' significance,
// signs, "visited" and "refined" flags, and the significance and sign of the rows just above and
// below the stripe. A coefficient's eight neighbours then come from three words (its column and
// the two beside it) that the scan carries in registers. This layout is SituCraft's choice.

package jp2k

import "math/bits"

// Code-block style flags (Table A.19).
const (
	cbBypass  = 0x01 // selective arithmetic coding bypass
	cbReset   = 0x02 // reset context probabilities on coding pass boundaries
	cbTermAll = 0x04 // termination on each coding pass
	cbVCausal = 0x08 // vertically causal context
	cbPTerm   = 0x10 // predictable termination (an encoder matter; nothing to do here)
	cbSegSym  = 0x20 // segmentation symbols
)

// Magnitudes are 32-bit with one fractional bit, so 31 bit-planes at most.
const maxBitPlanes = 31

// Stripe-column word layout. Rows are numbered -1 (the last row of the stripe above) to 4 (the
// first row of the stripe below); sig and sign hold rows -1 to 4, so row r is at bit r+1.
const (
	wSig    = 0      // bits 0-5: significance of rows -1..4
	wSign   = 6      // bits 6-11: sign (1 negative) of rows -1..4
	wVis    = 12     // bits 12-15: rows 0..3 coded in this bit-plane's significance pass
	wRef    = 16     // bits 16-19: rows 0..3 refined at least once
	sigAll  = 0x3F   // significance of rows -1..4
	sigOwn  = 0x1E   // significance of rows 0..3
	visAll  = 0xF000 // visited flags of rows 0..3
	sigRow4 = 0x20   // significance of row 4, ignored with vertically causal contexts
	// In a neighbourhood index (below), the bits of row r+1 of the three columns, ignored for
	// row 3 with vertically causal contexts.
	nbSouth = 1<<2 | 1<<5 | 1<<8
)

// Contexts (Table D.7): 0 to 8 zero coding, 9 to 13 sign coding, 14 to 16 magnitude refinement,
// 17 run-length, 18 uniform.
const (
	ctxMR0 = 14
	ctxMR1 = 15
	ctxMR2 = 16
	ctxRL  = 17
	ctxUni = 18
)

// A neighbourhood index has nine bits: rows r-1, r, r+1 of the west column (bits 0-2), of the
// coefficient's own column (bits 3-5; bit 4 is the coefficient itself) and of the east column
// (bits 6-8).
var (
	zcLUT [3][512]uint8 // zero-coding context by orientation class and neighbourhood
	scLUT [256]uint8    // sign-coding context << 1 | XOR bit, by direct neighbours (signIndex)
)

func init() {
	for nb := range 512 {
		bit := func(i int) int { return nb >> i & 1 }
		h := bit(1) + bit(7)
		v := bit(3) + bit(5)
		d := bit(0) + bit(2) + bit(6) + bit(8)
		zcLUT[0][nb] = zcHV(h, v, d)
		zcLUT[1][nb] = zcHV(v, h, d)
		zcLUT[2][nb] = zcDiag(h+v, d)
	}
	for i := range 256 {
		// signIndex bits: 0 N, 1 W, 2 S, 3 E significant; 4 to 7 the same neighbours negative.
		contrib := func(k int) int {
			if i>>k&1 == 0 {
				return 0
			}
			if i>>(k+4)&1 != 0 {
				return -1
			}
			return 1
		}
		clamp := func(x int) int { return max(-1, min(1, x)) }
		h := clamp(contrib(1) + contrib(3))
		v := clamp(contrib(0) + contrib(2))
		// Table D.3.
		var ctx, xor int
		switch {
		case h == 1:
			ctx = 12 + v
		case h == 0 && v == 0:
			ctx = 9
		case h == 0:
			ctx, xor = 10, (1-v)/2
		default: // h == -1
			ctx, xor = 12-v, 1
		}
		scLUT[i] = uint8(ctx<<1 | xor)
	}
}

// zcHV is Table D.1 for the LL and LH sub-bands, with h and v the counts of significant horizontal
// and vertical neighbours (the HL sub-band swaps them) and d the diagonal count.
func zcHV(h, v, d int) uint8 {
	switch {
	case h == 2:
		return 8
	case h == 1 && v >= 1:
		return 7
	case h == 1 && d >= 1:
		return 6
	case h == 1:
		return 5
	case v == 2:
		return 4
	case v == 1:
		return 3
	case d >= 2:
		return 2
	case d == 1:
		return 1
	}
	return 0
}

// zcDiag is Table D.1 for the HH sub-band.
func zcDiag(hv, d int) uint8 {
	switch {
	case d >= 3:
		return 8
	case d == 2 && hv >= 1:
		return 7
	case d == 2:
		return 6
	case d == 1 && hv >= 2:
		return 5
	case d == 1 && hv == 1:
		return 4
	case d == 1:
		return 3
	case hv >= 2:
		return 2
	case hv == 1:
		return 1
	}
	return 0
}

// neighbourhood returns the neighbourhood index of row r of column c, between columns w and e.
func neighbourhood(w, c, e uint32, r uint) uint32 {
	return w>>r&7 | (c>>r&7)<<3 | (e>>r&7)<<6
}

// signIndex returns the scLUT index for row r of column c: the significance (bits 0 to 3) and
// signs (bits 4 to 7) of its north, west, south and east neighbours, gathered from the three
// words' significance and sign fields, which lie six bits apart.
func signIndex(w, c, e uint32, r uint) uint32 {
	n := c >> r & (5 | 5<<wSign) // north and south: rows r-1 and r+1
	h := w >> r & (2 | 2<<wSign) // west: row r
	v := e >> r & (2 | 2<<wSign) // east: row r
	return n&5 | n>>2&0x50 | h&2 | h>>2&0x20 | (v&2)<<2 | v&0x80
}

// signSouthMask removes the south neighbour from a signIndex (vertically causal contexts).
const signSouthMask = ^uint32(1<<2 | 1<<6)

// t1 is the scratch state of one tier-1 worker.
type t1 struct {
	w, h, ns int
	cols     int      // words per stripe: w plus a border column on each side
	st       []uint32 // (ns+2) stripes of words: a border stripe above and below
	mag      []uint32 // magnitudes with one fractional bit, four per stripe column
	ctx      [19]mqState
	mq       mqDecoder
	raw      rawDecoder
	vc       bool // vertically causal contexts
}

func (t *t1) resetContexts() {
	for i := range t.ctx {
		t.ctx[i] = mqTable[0]
	}
	t.ctx[0] = mqTable[4*2]       // all neighbours insignificant: state 4
	t.ctx[ctxRL] = mqTable[3*2]   // state 3
	t.ctx[ctxUni] = mqTable[46*2] // state 46
}

// decodeBlock decodes one code-block's coding passes and writes its dequantized coefficients into
// the tile-component buffer.
func (t *t1) decodeBlock(j *cbJob) {
	cb, b, tc := j.cb, j.b, j.tc
	t.w, t.h = cb.x1-cb.x0, cb.y1-cb.y0
	t.ns = (t.h + 3) / 4
	t.cols = t.w + 2
	n := t.cols * (t.ns + 2)
	m := 4 * t.w * t.ns
	if cap(t.st) < n {
		t.st = make([]uint32, n)
	}
	if cap(t.mag) < m {
		t.mag = make([]uint32, m)
	}
	t.st, t.mag = t.st[:n], t.mag[:m]
	clear(t.st)
	clear(t.mag)
	style := tc.p.style
	t.vc = style&cbVCausal != 0
	class := [4]int{0, 1, 0, 2}[b.orient]
	t.resetContexts()

	p := uint(cb.numbps - 1)
	kind := 2 // the first pass is a cleanup pass
	idx := 0
	for _, seg := range cb.segs {
		raw := style&cbBypass != 0 && idx >= 10 && kind != 2
		if raw {
			t.raw.init(seg.data)
		} else {
			t.mq.init(seg.data)
		}
		for range seg.passes {
			switch {
			case kind == 0 && raw:
				t.sigPassRaw(p)
			case kind == 0:
				t.sigPass(p, class)
			case kind == 1 && raw:
				t.refPassRaw(p)
			case kind == 1:
				t.refPass(p)
			default:
				t.cleanup(p, class)
				if style&cbSegSym != 0 {
					for range 4 {
						t.mq.decode(&t.ctx[ctxUni])
					}
				}
			}
			if style&cbReset != 0 {
				t.resetContexts()
			}
			idx++
			kind++
			if kind == 3 {
				kind = 0
				p--
			}
		}
	}
	t.store(tc, b, cb)
}

// stripe returns stripe s's words and those of the stripes above and below it.
func (t *t1) stripe(s int) (above, row, below []uint32) {
	c := t.cols
	return t.st[s*c : s*c+c], t.st[(s+1)*c : (s+1)*c+c], t.st[(s+2)*c : (s+2)*c+c]
}

// becomeSig records that row r of column word *c turned significant with the given sign (1
// negative), in its own word and in the border rows of the stripes above and below.
func becomeSig(c *uint32, above, below []uint32, x int, r uint, neg uint32) {
	*c |= 1<<(wSig+r+1) | neg<<(wSign+r+1)
	switch r {
	case 0:
		above[x] |= 1<<(wSig+5) | neg<<(wSign+5)
	case 3:
		below[x] |= 1<<wSig | neg<<wSign
	}
}

// The MQ-coded passes keep the decoder's A, C and CT registers in local variables (qa, qc, qct)
// and decode with mqDecide, inlined, and an inline renormalization; only reading bytes is a call,
// since a call in these loops spills every live register. This is the decoder's hot loop.

// sigCandidates returns, as bits 0 to 3, the rows of column c (between w and e) that the
// significance propagation pass codes: insignificant, not yet visited, with a significant
// neighbour (rows r-1 to r+1 of the columns beside, rows r-1 and r+1 of its own).
func sigCandidates(w, c, e uint32, vc bool) uint32 {
	h, v := (w|e)&sigAll, c&sigAll
	// Bit r: rows r-1..r+1 beside (bits r..r+2), rows r-1 and r+1 of its own (bits r and r+2).
	nz := h | h>>1 | h>>2 | v | v>>2
	if vc {
		// Row 3 ignores row 4.
		nz = nz&7 | (h>>3|h>>4|v>>3)&1<<3
	}
	return nz &^ (c>>(wSig+1) | c>>wVis) & 0xF
}

// sigPass is the significance propagation pass (D.3.1) at bit-plane p.
func (t *t1) sigPass(p uint, class int) {
	vc := t.vc
	one := uint32(3) << p
	zc := &zcLUT[class]
	mq, ctx := &t.mq, &t.ctx
	qa, qc, qct := mq.a, mq.c, mq.ct
	var d uint32
	for s := range t.ns {
		above, row, below := t.stripe(s)
		mag := t.mag[4*t.w*s:][:4*t.w]
		rows := uint(min(4, t.h-4*s))
		rowMask := uint32(1)<<rows - 1
		w, c := row[0], row[1]
		for x := 1; x <= t.w; x++ {
			e := row[x+1]
			if cand := sigCandidates(w, c, e, vc) & rowMask; cand != 0 {
				m := mag[4*(x-1):][:4]
				for cand != 0 {
					r := uint(bits.TrailingZeros32(cand))
					cand &= cand - 1
					nb := neighbourhood(w, c, e, r)
					south := vc && r == 3
					if south {
						nb &^= nbSouth
					}
					cx := &ctx[zc[nb]]
					st := *cx
					qa -= st.qe
					var nx uint8
					d, qa, qc, nx = mqDecide(qa, qc, st)
					*cx = mqTable[nx]
					if n := bits.LeadingZeros32(qa) - 16; n > qct {
						qa, qc, qct = mq.renorm(qa, qc, qct)
					} else {
						qa, qc, qct = qa<<n, qc<<n, qct-n
					}
					c |= 1 << (wVis + r)
					if d == 0 {
						continue
					}
					si := signIndex(w, c, e, r)
					if south {
						si &= signSouthMask
					}
					l := scLUT[si]
					cx = &ctx[l>>1]
					st = *cx
					qa -= st.qe
					d, qa, qc, nx = mqDecide(qa, qc, st)
					*cx = mqTable[nx]
					if n := bits.LeadingZeros32(qa) - 16; n > qct {
						qa, qc, qct = mq.renorm(qa, qc, qct)
					} else {
						qa, qc, qct = qa<<n, qc<<n, qct-n
					}
					becomeSig(&c, above, below, x, r, d^uint32(l&1))
					m[r] = one
					// The rows below may now have a significant neighbour.
					cand = sigCandidates(w, c, e, vc) & rowMask &^ (2<<r - 1)
				}
				row[x] = c
			}
			w, c = c, e
		}
	}
	mq.a, mq.c, mq.ct = qa, qc, qct
}

// refPass is the magnitude refinement pass (D.3.3) at bit-plane p.
func (t *t1) refPass(p uint) {
	vc := t.vc
	mq, ctx := &t.mq, &t.ctx
	qa, qc, qct := mq.a, mq.c, mq.ct
	var d uint32
	for s := range t.ns {
		_, row, _ := t.stripe(s)
		mag := t.mag[4*t.w*s:][:4*t.w]
		rows := uint(min(4, t.h-4*s))
		rowMask := uint32(1)<<rows - 1
		w, c := row[0], row[1]
		for x := 1; x <= t.w; x++ {
			e := row[x+1]
			// Rows significant before this bit-plane: significant and not visited by sigPass.
			if cand := (c >> (wSig + 1) &^ (c >> wVis)) & rowMask; cand != 0 {
				m := mag[4*(x-1):][:4]
				for cand != 0 {
					r := uint(bits.TrailingZeros32(cand))
					cand &= cand - 1
					k := ctxMR2
					if c&(1<<(wRef+r)) == 0 {
						// Any significant neighbour: rows r-1..r+1 beside, r-1 and r+1 above and
						// below.
						nb := ((w|e)>>r&7 | c>>r&5)
						if vc && r == 3 {
							nb &^= 4
						}
						k = ctxMR0
						if nb != 0 {
							k = ctxMR1
						}
					}
					cx := &ctx[k]
					st := *cx
					qa -= st.qe
					var nx uint8
					d, qa, qc, nx = mqDecide(qa, qc, st)
					*cx = mqTable[nx]
					if n := bits.LeadingZeros32(qa) - 16; n > qct {
						qa, qc, qct = mq.renorm(qa, qc, qct)
					} else {
						qa, qc, qct = qa<<n, qc<<n, qct-n
					}
					// Add or remove half of the bit's weight: the new bit, minus the old midpoint,
					// plus the new midpoint.
					m[r] += (d<<1 - 1) << p
					c |= 1 << (wRef + r)
				}
				row[x] = c
			}
			w, c = c, e
		}
	}
	mq.a, mq.c, mq.ct = qa, qc, qct
}

// cleanup is the cleanup pass (D.3.4) at bit-plane p, with run-length coding of columns of four
// insignificant coefficients whose neighbours are all insignificant.
func (t *t1) cleanup(p uint, class int) {
	vc := t.vc
	one := uint32(3) << p
	zc := &zcLUT[class]
	mq, ctx := &t.mq, &t.ctx
	qa, qc, qct := mq.a, mq.c, mq.ct
	var d uint32
	rlMask := uint32(sigAll)
	if vc {
		rlMask &^= sigRow4
	}
	for s := range t.ns {
		above, row, below := t.stripe(s)
		mag := t.mag[4*t.w*s:][:4*t.w]
		rows := uint(min(4, t.h-4*s))
		rowMask := uint32(1)<<rows - 1
		w, c := row[0], row[1]
		for x := 1; x <= t.w; x++ {
			e := row[x+1]
			m := mag[4*(x-1):][:4]
			r := uint(0)
			rl := false
			if rows == 4 && (w|c|e)&rlMask == 0 && c&visAll == 0 {
				cx := &ctx[ctxRL]
				st := *cx
				qa -= st.qe
				var nx uint8
				d, qa, qc, nx = mqDecide(qa, qc, st)
				*cx = mqTable[nx]
				if n := bits.LeadingZeros32(qa) - 16; n > qct {
					qa, qc, qct = mq.renorm(qa, qc, qct)
				} else {
					qa, qc, qct = qa<<n, qc<<n, qct-n
				}
				if d == 0 {
					w, c = c, e
					continue
				}
				// Two uniform-context bits give the row of the first significant coefficient.
				for range 2 {
					cx := &ctx[ctxUni]
					st := *cx
					qa -= st.qe
					var nx uint8
					d, qa, qc, nx = mqDecide(qa, qc, st)
					*cx = mqTable[nx]
					if n := bits.LeadingZeros32(qa) - 16; n > qct {
						qa, qc, qct = mq.renorm(qa, qc, qct)
					} else {
						qa, qc, qct = qa<<n, qc<<n, qct-n
					}
					r = r<<1 | uint(d)
				}
				// That coefficient is significant: only its sign is decoded below.
				rl = true
			}
			// The rows still to code: insignificant and not visited, from r on.
			cand := ^(c>>(wSig+1) | c>>wVis) & rowMask &^ (1<<r - 1)
			for cand != 0 {
				r = uint(bits.TrailingZeros32(cand))
				cand &= cand - 1
				if !rl {
					nb := neighbourhood(w, c, e, r)
					if vc && r == 3 {
						nb &^= nbSouth
					}
					cx := &ctx[zc[nb]]
					st := *cx
					qa -= st.qe
					var nx uint8
					d, qa, qc, nx = mqDecide(qa, qc, st)
					*cx = mqTable[nx]
					if n := bits.LeadingZeros32(qa) - 16; n > qct {
						qa, qc, qct = mq.renorm(qa, qc, qct)
					} else {
						qa, qc, qct = qa<<n, qc<<n, qct-n
					}
					if d == 0 {
						continue
					}
				}
				rl = false
				si := signIndex(w, c, e, r)
				if vc && r == 3 {
					si &= signSouthMask
				}
				l := scLUT[si]
				cx := &ctx[l>>1]
				st := *cx
				qa -= st.qe
				var nx uint8
				d, qa, qc, nx = mqDecide(qa, qc, st)
				*cx = mqTable[nx]
				if n := bits.LeadingZeros32(qa) - 16; n > qct {
					qa, qc, qct = mq.renorm(qa, qc, qct)
				} else {
					qa, qc, qct = qa<<n, qc<<n, qct-n
				}
				becomeSig(&c, above, below, x, r, d^uint32(l&1))
				m[r] = one
			}
			c &^= visAll
			row[x] = c
			w, c = c, e
		}
	}
	mq.a, mq.c, mq.ct = qa, qc, qct
}

// sigPassRaw is the significance propagation pass in a bypass segment (D.6): the same scan, with
// the significance and sign bits read raw.
func (t *t1) sigPassRaw(p uint) {
	vc := t.vc
	one := uint32(3) << p
	for s := range t.ns {
		above, row, below := t.stripe(s)
		mag := t.mag[4*t.w*s:][:4*t.w]
		rows := uint(min(4, t.h-4*s))
		w, c := row[0], row[1]
		for x := 1; x <= t.w; x++ {
			e := row[x+1]
			if (w|c|e)&sigAll != 0 && c&sigOwn != sigOwn {
				m := mag[4*(x-1):][:4]
				for r := range rows {
					if c&(1<<(wSig+r+1)|1<<(wVis+r)) != 0 {
						continue
					}
					nb := neighbourhood(w, c, e, r)
					if vc && r == 3 {
						nb &^= nbSouth
					}
					if nb == 0 {
						continue
					}
					if t.raw.bit() != 0 {
						becomeSig(&c, above, below, x, r, t.raw.bit())
						m[r] = one
					}
					c |= 1 << (wVis + r)
				}
				row[x] = c
			}
			w, c = c, e
		}
	}
}

// refPassRaw is the magnitude refinement pass in a bypass segment.
func (t *t1) refPassRaw(p uint) {
	half := uint32(1) << p
	for s := range t.ns {
		_, row, _ := t.stripe(s)
		mag := t.mag[4*t.w*s:][:4*t.w]
		rows := uint(min(4, t.h-4*s))
		for x := 1; x <= t.w; x++ {
			c := row[x]
			if c&sigOwn == 0 {
				continue
			}
			m := mag[4*(x-1):][:4]
			for r := range rows {
				if c&(1<<(wSig+r+1)|1<<(wVis+r)) != 1<<(wSig+r+1) {
					continue
				}
				if t.raw.bit() != 0 {
					m[r] += half
				} else {
					m[r] -= half
				}
				c |= 1 << (wRef + r)
			}
			row[x] = c
		}
	}
}

// store applies the ROI downshift (Annex H, maximum shift) and dequantization (Annex E) and
// writes the code-block into its sub-band's place in the tile-component buffer.
func (t *t1) store(tc *tileComp, b *band, cb *codeblock) {
	ox := b.offx + cb.x0 - b.x0
	oy := b.offy + cb.y0 - b.y0
	roi := uint(tc.roi)
	thresh := ^uint32(0)
	if roi > 0 && roi < 31 {
		thresh = 1 << (roi + 1) // 2^roi in magnitude units with the fractional bit
	}
	for s := range t.ns {
		_, row, _ := t.stripe(s)
		mag := t.mag[4*t.w*s:][:4*t.w]
		rows := min(4, t.h-4*s)
		for r := range rows {
			y := oy + 4*s + r
			signBit := uint(wSign + r + 1)
			if tc.ibuf != nil {
				dst := tc.ibuf[y*tc.w+ox:][:t.w]
				for x := range dst {
					v := mag[4*x+r]
					if v >= thresh {
						v >>= roi
					}
					q := int32(v >> 1)
					if row[x+1]>>signBit&1 != 0 {
						q = -q
					}
					dst[x] = q
				}
			} else {
				dst := tc.fbuf[y*tc.w+ox:][:t.w]
				for x := range dst {
					v := mag[4*x+r]
					if v >= thresh {
						v >>= roi
					}
					f := float32(v) * b.step
					if row[x+1]>>signBit&1 != 0 {
						f = -f
					}
					dst[x] = f
				}
			}
		}
	}
}
