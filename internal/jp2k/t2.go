// Tier 2 (T.800 Annex B): progression orders and progression order changes (B.12), tag trees
// (B.10.2) and packet headers (B.10), including SOP/EPH markers and packed packet headers
// (PPM/PPT).

package jp2k

import (
	"errors"
	"fmt"
	"math"
)

// progressionBudget bounds the work of one tile's tier 2: packets, empty loop steps and the
// code-blocks each non-empty packet header walks. Hostile layer, component, POC and precinct
// counts cannot make the decoder loop for ever; a real tile is far below it (a 1024×1024 tile has
// a few thousand packets and code-blocks). The fuzz test lowers it.
var progressionBudget = 1 << 28

var errProgression = errors.New("jp2k: progression visits more packets than the decoder allows")

// progression calls visit for each packet of the tile in codestream order (B.12): the tile's
// progression order or its progression order changes.
func (t *tile) progression(visit func(c, r, p, l int) error) error {
	entries := t.poc
	if len(entries) == 0 {
		entries = []pocEntry{{le: t.layers, re: t.maxRes + 1, ce: len(t.comps), prog: t.prog}}
	}
	t.budget = progressionBudget
	spend := t.spend
	// emit visits the packets of precinct p of (c, r) for the layers in [next, le), the inner
	// loop of every order: a packet already visited by an earlier POC entry is not visited again.
	emit := func(c, r, p, le int) error {
		res := &t.comps[c].res[r]
		for int(res.next[p]) < le {
			l := int(res.next[p])
			res.next[p]++
			if err := spend(1); err != nil {
				return err
			}
			if err := visit(c, r, p, l); err != nil {
				return err
			}
		}
		return nil
	}
	for _, e := range entries {
		le, re, ce := min(e.le, t.layers), min(e.re, t.maxRes+1), min(e.ce, len(t.comps))
		rs, cs := e.rs, e.cs
		if rs >= re || cs >= ce || le <= 0 {
			continue
		}
		switch e.prog {
		case 0, 1: // LRCP, RLCP: layer and resolution outermost, then component, then precinct
			nl, nr := le, re-rs
			for a := 0; a < nl*nr; a++ {
				l, r := a/nr, rs+a%nr
				if e.prog == 1 {
					r, l = rs+a/nl, a%nl
				}
				for c := cs; c < ce; c++ {
					if err := spend(1); err != nil {
						return err
					}
					tc := &t.comps[c]
					if r >= len(tc.res) {
						continue
					}
					res := &tc.res[r]
					for p := range res.next {
						if int(res.next[p]) != l {
							if err := spend(1); err != nil {
								return err
							}
							continue
						}
						if err := emit(c, r, p, l+1); err != nil {
							return err
						}
					}
				}
			}
		case 2: // RPCL: resolution, then position, then component
			for r := rs; r < re; r++ {
				var streams []*pstream
				for c := cs; c < ce; c++ {
					streams = t.addStream(streams, c, r, c)
				}
				if err := t.merge(streams, le, spend, emit); err != nil {
					return err
				}
			}
		case 3: // PCRL: position, then component, then resolution
			var streams []*pstream
			for c := cs; c < ce; c++ {
				for r := rs; r < re; r++ {
					streams = t.addStream(streams, c, r, c*64+r)
				}
			}
			if err := t.merge(streams, le, spend, emit); err != nil {
				return err
			}
		case 4: // CPRL: component, then position, then resolution
			for c := cs; c < ce; c++ {
				var streams []*pstream
				for r := rs; r < re; r++ {
					streams = t.addStream(streams, c, r, r)
				}
				if err := t.merge(streams, le, spend, emit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// spend charges n units of tier-2 work to the tile's budget.
func (t *tile) spend(n int) error {
	t.budget -= n
	if t.budget < 0 {
		return errProgression
	}
	return nil
}

// A pstream walks the precincts of one (component, resolution) in raster order, which is also the
// order of their positions on the reference grid.
type pstream struct {
	c, r, p int
	n       int   // precincts
	y, x    int64 // reference-grid position at which precinct p is visited
	tie     int   // order among streams visited at the same position
}

func (t *tile) addStream(s []*pstream, c, r, tie int) []*pstream {
	tc := &t.comps[c]
	if r >= len(tc.res) || len(tc.res[r].next) == 0 {
		return s
	}
	ps := &pstream{c: c, r: r, n: len(tc.res[r].next), tie: tie}
	t.position(ps)
	return append(s, ps)
}

// position computes where on the reference grid the position-driven loops of B.12.1.3 to B.12.1.5
// first meet precinct ps.p: at a multiple of XRsiz·2^(PPx+NL-r) (and likewise in y), or at the
// tile's corner for a first precinct that is not aligned to the precinct grid.
func (t *tile) position(ps *pstream) {
	tc := &t.comps[ps.c]
	res := &tc.res[ps.r]
	dx, dy := tc.dx, tc.dy
	sh := tc.p.levels - ps.r
	px, py := ps.p%res.pw, ps.p/res.pw
	if px == 0 && res.x0&(1<<res.ppx-1) != 0 {
		ps.x = int64(t.x0)
	} else {
		ps.x = int64(res.px0+px) << (res.ppx + sh) * int64(dx)
	}
	if py == 0 && res.y0&(1<<res.ppy-1) != 0 {
		ps.y = int64(t.y0)
	} else {
		ps.y = int64(res.py0+py) << (res.ppy + sh) * int64(dy)
	}
}

func (a *pstream) less(b *pstream) bool {
	if a.y != b.y {
		return a.y < b.y
	}
	if a.x != b.x {
		return a.x < b.x
	}
	return a.tie < b.tie
}

// merge visits the precincts of several streams in order of position (then tie), each with its
// layers, using a binary heap.
func (t *tile) merge(h []*pstream, le int, spend func(int) error, emit func(c, r, p, le int) error) error {
	down := func(i int) {
		for {
			l := 2*i + 1
			if l >= len(h) {
				return
			}
			m := l
			if r := l + 1; r < len(h) && h[r].less(h[l]) {
				m = r
			}
			if !h[m].less(h[i]) {
				return
			}
			h[i], h[m] = h[m], h[i]
			i = m
		}
	}
	for i := len(h)/2 - 1; i >= 0; i-- {
		down(i)
	}
	for len(h) > 0 {
		ps := h[0]
		if err := spend(1); err != nil {
			return err
		}
		if err := emit(ps.c, ps.r, ps.p, le); err != nil {
			return err
		}
		ps.p++
		if ps.p < ps.n {
			t.position(ps)
		} else {
			h[0] = h[len(h)-1]
			h = h[:len(h)-1]
		}
		down(0)
	}
	return nil
}

// tagTree is a tag tree (B.10.2) over a grid of code-blocks.
type tagTree struct {
	offs   []int // index of each level's first node, leaves first
	widths []int
	nodes  []ttNode
}

type ttNode struct {
	value, low int32
}

func newTagTree(w, h int) tagTree {
	var t tagTree
	n := 0
	for {
		t.offs = append(t.offs, n)
		t.widths = append(t.widths, w)
		n += w * h
		if w <= 1 && h <= 1 {
			break
		}
		w, h = (w+1)/2, (h+1)/2
	}
	t.nodes = make([]ttNode, n)
	for i := range t.nodes {
		t.nodes[i].value = math.MaxInt32
	}
	return t
}

// decode reads what the header says about leaf (x, y) relative to threshold, and returns the
// leaf's value when it is below threshold, otherwise a value of at least threshold.
func (t *tagTree) decode(br *bitReader, x, y, threshold int) (int, error) {
	var path [48]int32
	levels := len(t.offs)
	for lev := range levels {
		path[lev] = int32(t.offs[lev] + (y>>lev)*t.widths[lev] + (x >> lev))
	}
	th := int32(min(threshold, math.MaxInt32-1))
	low := int32(0)
	for lev := levels - 1; lev >= 0; lev-- {
		nd := &t.nodes[path[lev]]
		if low > nd.low {
			nd.low = low
		} else {
			low = nd.low
		}
		for low < th && low < nd.value {
			b, err := br.bit()
			if err != nil {
				return 0, err
			}
			if b == 1 {
				nd.value = low
			} else {
				low++
			}
		}
		nd.low = low
	}
	return int(t.nodes[path[0]].value), nil
}

var errHeaderEnd = errors.New("jp2k: packet header runs past the end of the data")

// errDataEnded stops the progression when a tile's data ends at a packet boundary.
var errDataEnded = errors.New("jp2k: tile data ended")

// bitReader reads packet header bits (B.10.1): most significant bit first, with a zero bit stuffed
// after every 0xFF byte.
type bitReader struct {
	data []byte
	pos  int
	cur  uint32
	last byte
	n    uint
}

func (b *bitReader) bit() (int, error) {
	if b.n == 0 {
		if b.pos >= len(b.data) {
			return 0, errHeaderEnd
		}
		c := b.data[b.pos]
		b.pos++
		b.n = 8
		if b.last == 0xFF {
			b.n = 7
		}
		b.last = c
		b.cur = uint32(c)
	}
	b.n--
	return int(b.cur>>b.n) & 1, nil
}

// bits reads an n-bit unsigned field (n <= 39). Values beyond 2^31-1, which no length within a
// codestream can have, are clamped so that they fit an int everywhere.
func (b *bitReader) bits(n int) (int, error) {
	var v uint64
	for range n {
		bit, err := b.bit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | uint64(bit)
	}
	return int(min(v, math.MaxInt32)), nil
}

// align ends a packet header: the rest of the byte is padding, and a header that ends in 0xFF is
// followed by one more (stuffed) byte.
func (b *bitReader) align() {
	b.n = 0
	if b.last == 0xFF {
		if b.pos < len(b.data) {
			b.pos++
		}
		b.last = 0
	}
}

// packetReader decodes the packets of one tile.
type packetReader struct {
	t        *tile
	body     []byte
	pos      int
	packed   bool   // packet headers are in hdr (PPM/PPT), not in the body
	hdr      []byte // packed packet headers
	hpos     int
	sop, eph bool
	pieces   []piece
}

// A piece is one code-block's contribution to one codeword segment in one packet.
type piece struct {
	cb     *codeblock
	seg    int
	length int
}

// packet decodes the header of packet (c, r, p, l) and attaches its body to the code-blocks.
func (pr *packetReader) packet(c, r, p, l int) error {
	tc := &pr.t.comps[c]
	res := &tc.res[r]
	pc := res.precincts[p]
	if pc == nil {
		pc = newPrecinct(tc, r, p)
		res.precincts[p] = pc
	}
	body := pr.body
	if pr.packed && pr.hpos >= len(pr.hdr) || !pr.packed && pr.pos >= len(body) {
		// The tile's data ended at a packet boundary: the remaining packets are empty, as when a
		// codestream is truncated after a layer. (OpenJPEG's encoder also leaves out the empty
		// packets of tile-parts it writes empty.) Data that ends inside a packet is an error.
		return errDataEnded
	}
	if pr.pos+6 <= len(body) && body[pr.pos] == 0xFF && body[pr.pos+1] == 0x91 { // SOP (A.8.1)
		pr.pos += 6
	}
	br := bitReader{data: body, pos: pr.pos}
	if pr.packed {
		br = bitReader{data: pr.hdr, pos: pr.hpos}
	}
	pr.pieces = pr.pieces[:0]
	present, err := br.bit()
	if err != nil {
		return err
	}
	if present == 1 {
		if err := pr.header(&br, tc, res, pc, l); err != nil {
			return err
		}
	}
	br.align()
	hp := br.pos
	if hp+2 <= len(br.data) && br.data[hp] == 0xFF && br.data[hp+1] == 0x92 { // EPH (A.8.2)
		hp += 2
	}
	if pr.packed {
		pr.hpos = hp
	} else {
		pr.pos = hp
	}
	keep := r <= tc.top
	for _, pi := range pr.pieces {
		if pi.length > len(body)-pr.pos {
			return fmt.Errorf("jp2k: packet data of component %d, resolution %d, layer %d runs past the end of the tile data", c, r, l)
		}
		if keep && pi.length > 0 {
			chunk := body[pr.pos : pr.pos+pi.length : pr.pos+pi.length]
			s := &pi.cb.segs[pi.seg]
			if len(s.data) == 0 {
				s.data = chunk
			} else {
				s.data = append(s.data[:len(s.data):len(s.data)], chunk...)
			}
		}
		pr.pos += pi.length
	}
	return nil
}

// header decodes the code-block contributions of a non-empty packet (B.10.3 to B.10.7).
func (pr *packetReader) header(br *bitReader, tc *tileComp, res *resolution, pc *precinct, l int) error {
	style := tc.p.style
	for bi := range pc.bands {
		pb := &pc.bands[bi]
		b := &res.bands[bi]
		if err := pr.t.spend(len(pb.blocks)); err != nil {
			return err
		}
		for k := range pb.blocks {
			cb := &pb.blocks[k]
			x, y := k%pb.cw, k/pb.cw
			var inc bool
			if !cb.included {
				v, err := pb.incl.decode(br, x, y, l+1)
				if err != nil {
					return err
				}
				inc = v <= l
			} else {
				bit, err := br.bit()
				if err != nil {
					return err
				}
				inc = bit == 1
			}
			if !inc {
				continue
			}
			if !cb.included {
				zb, err := pb.zbp.decode(br, x, y, b.mb+1)
				if err != nil {
					return err
				}
				if zb > b.mb {
					return fmt.Errorf("jp2k: code-block has more missing bit-planes than the %d of its sub-band", b.mb)
				}
				cb.included = true
				cb.lblock = 3
				cb.numbps = b.mb - zb
			}
			n, err := numPasses(br)
			if err != nil {
				return err
			}
			if cb.numbps > maxBitPlanes {
				return fmt.Errorf("jp2k: code-block has %d magnitude bit-planes, this decoder supports at most %d: %w", cb.numbps, maxBitPlanes, ErrUnsupported)
			}
			if cb.passes+n > 3*cb.numbps-2 {
				return fmt.Errorf("jp2k: code-block declares %d coding passes, its %d bit-planes allow %d", cb.passes+n, cb.numbps, max(3*cb.numbps-2, 0))
			}
			for {
				bit, err := br.bit()
				if err != nil {
					return err
				}
				if bit == 0 {
					break
				}
				cb.lblock++
				if cb.lblock > 32 {
					return fmt.Errorf("jp2k: code-block length indicator exceeds 32 bits")
				}
			}
			for i, end := cb.passes, cb.passes+n; i < end; {
				start, segEnd := segBounds(style, i)
				pe := min(segEnd, end)
				np := pe - i
				length, err := br.bits(cb.lblock + floorLog2(np))
				if err != nil {
					return err
				}
				if i == start || len(cb.segs) == 0 {
					cb.segs = append(cb.segs, segment{})
				}
				cb.segs[len(cb.segs)-1].passes += np
				pr.pieces = append(pr.pieces, piece{cb: cb, seg: len(cb.segs) - 1, length: length})
				i = pe
			}
			cb.passes += n
		}
	}
	return nil
}

// segBounds returns the first pass and the pass after the last of the codeword segment that holds
// pass i (counted from the code-block's first coding pass), which the code-block style determines
// (D.4.1, Table D.9): every pass is terminated with TERMALL; with BYPASS, the first ten passes form
// one segment, then each pair of raw significance and refinement passes and each cleanup pass.
func segBounds(style uint8, i int) (start, end int) {
	switch {
	case style&cbTermAll != 0:
		return i, i + 1
	case style&cbBypass != 0:
		if i < 10 {
			return 0, 10
		}
		m := (i - 10) % 3
		if m == 2 {
			return i, i + 1
		}
		return i - m, i - m + 2
	}
	return 0, math.MaxInt32
}

// numPasses reads the number of new coding passes (Table B.4).
func numPasses(br *bitReader) (int, error) {
	if b, err := br.bit(); err != nil || b == 0 {
		return 1, err
	}
	if b, err := br.bit(); err != nil || b == 0 {
		return 2, err
	}
	v, err := br.bits(2)
	if err != nil || v != 3 {
		return 3 + v, err
	}
	v, err = br.bits(5)
	if err != nil || v != 31 {
		return 6 + v, err
	}
	v, err = br.bits(7)
	return 37 + v, err
}

func floorLog2(n int) int {
	l := 0
	for n > 1 {
		n >>= 1
		l++
	}
	return l
}

// newPrecinct builds precinct p of resolution r: its code-blocks in each sub-band (B.6, B.7) and
// their tag trees.
func newPrecinct(tc *tileComp, r, p int) *precinct {
	res := &tc.res[r]
	kx, ky := res.px0+p%res.pw, res.py0+p/res.pw
	pc := &precinct{bands: make([]precBand, len(res.bands))}
	ppx, ppy := res.ppx, res.ppy
	if r > 0 { // a precinct covers half as many samples in each sub-band
		ppx, ppy = ppx-1, ppy-1
	}
	for bi := range res.bands {
		b := &res.bands[bi]
		pb := &pc.bands[bi]
		x0, y0 := max(kx<<ppx, b.x0), max(ky<<ppy, b.y0)
		x1, y1 := min((kx+1)<<ppx, b.x1), min((ky+1)<<ppy, b.y1)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		pb.cx0, pb.cy0 = x0>>res.cbw, y0>>res.cbh
		pb.cw, pb.ch = ceilShift(x1, res.cbw)-pb.cx0, ceilShift(y1, res.cbh)-pb.cy0
		pb.blocks = make([]codeblock, pb.cw*pb.ch)
		for j := range pb.ch {
			for i := range pb.cw {
				cb := &pb.blocks[j*pb.cw+i]
				gx, gy := pb.cx0+i, pb.cy0+j
				cb.x0, cb.y0 = max(gx<<res.cbw, x0), max(gy<<res.cbh, y0)
				cb.x1, cb.y1 = min((gx+1)<<res.cbw, x1), min((gy+1)<<res.cbh, y1)
			}
		}
		pb.incl = newTagTree(pb.cw, pb.ch)
		pb.zbp = newTagTree(pb.cw, pb.ch)
	}
	return pc
}
