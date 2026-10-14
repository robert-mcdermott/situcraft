// Tiles, tile-components, resolutions, sub-bands, precincts and code-blocks (T.800 Annex B), and
// the order in which a tile is decoded: tier 2 (packets), tier 1 (code-blocks, in parallel),
// dequantization (Annex E), the inverse wavelet transform (Annex F) and the output stage.

package jp2k

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
)

// Sub-band orientations, in codestream order within a resolution (B.5).
const (
	bandLL = 0
	bandHL = 1
	bandLH = 2
	bandHH = 3
)

// The total number of precinct slots one tile may have. Precinct state is allocated only when a
// packet for it arrives, but the slots are allocated up front; real images have a few hundred.
// The fuzz test lowers it.
var maxPrecinctSlots = 1 << 22

type tile struct {
	idx            int
	x0, y0, x1, y1 int
	layers         int
	prog           int
	sop, eph       bool
	mct            int
	poc            []pocEntry
	comps          []tileComp
	maxRes         int // the highest resolution count minus one over the components
	budget         int // tier-2 work left (progressionBudget)
}

type tileComp struct {
	c              int
	x0, y0, x1, y1 int
	dx, dy         int // subsampling (XRsiz, YRsiz)
	p              *compParams
	q              *quantParams
	roi            int
	res            []resolution
	top            int // the resolution decoded (NL - Reduce)
	w, h           int // its size
	fbuf           []float32
	ibuf           []int32
}

type resolution struct {
	x0, y0, x1, y1 int
	ppx, ppy       int // precinct size exponents
	pw, ph         int // precincts across and down
	px0, py0       int // index of the first precinct in the precinct grid anchored at 0
	cbw, cbh       int // code-block size exponents at this resolution
	bands          []band
	precincts      []*precinct
	next           []uint16 // per precinct, the next layer whose packet is due
}

type band struct {
	orient         int
	x0, y0, x1, y1 int
	mb             int     // magnitude bit-planes, Mb (E.1), plus the ROI shift
	step           float32 // half the quantization step size (irreversible)
	offx, offy     int     // position in the tile-component buffer (Mallat layout)
}

type precinct struct {
	bands []precBand
}

type precBand struct {
	cx0, cy0 int // index of the first code-block in the band's code-block grid
	cw, ch   int // code-blocks across and down
	incl     tagTree
	zbp      tagTree
	blocks   []codeblock
}

type codeblock struct {
	x0, y0, x1, y1 int // in sub-band coordinates
	included       bool
	numbps         int // bit-planes to decode: Mb (+ ROI shift) minus the missing ones
	lblock         int
	passes         int
	segs           []segment
}

type segment struct {
	data   []byte
	passes int
}

// decodeTiles decodes every tile in turn and hands each to the sink.
func (d *decoder) decodeTiles() error {
	for i, tp := range d.tiles {
		if tp == nil {
			return fmt.Errorf("jp2k: tile %d has no tile-parts (truncated codestream)", i)
		}
		t, err := d.setupTile(i, tp)
		if err != nil {
			return err
		}
		if err := d.decodeTile(t, tp); err != nil {
			return err
		}
		if err := d.sink(t); err != nil {
			return err
		}
	}
	return nil
}

// setupTile resolves the tile's coding parameters and computes its geometry.
func (d *decoder) setupTile(idx int, tp *tileParts) (*tile, error) {
	s := &d.siz
	cod := d.main.cod
	if tp.hdr.cod != nil {
		cod = tp.hdr.cod
	}
	px, py := idx%d.ntx, idx/d.ntx
	t := &tile{
		idx:    idx,
		x0:     max(s.tx+px*s.tw, s.x0),
		y0:     max(s.ty+py*s.th, s.y0),
		x1:     min(s.tx+(px+1)*s.tw, s.x1),
		y1:     min(s.ty+(py+1)*s.th, s.y1),
		layers: cod.layers,
		prog:   cod.prog,
		sop:    cod.scod&2 != 0,
		eph:    cod.scod&4 != 0,
		mct:    cod.mct,
		poc:    d.main.poc,
		comps:  make([]tileComp, len(s.comps)),
	}
	if len(tp.hdr.poc) > 0 {
		t.poc = tp.hdr.poc
	}
	slots := 0
	for c := range t.comps {
		tc := &t.comps[c]
		tc.c = c
		switch {
		case tp.hdr.coc[c] != nil:
			tc.p = tp.hdr.coc[c]
		case tp.hdr.cod != nil:
			tc.p = &tp.hdr.cod.comp
		case d.main.coc[c] != nil:
			tc.p = d.main.coc[c]
		default:
			tc.p = &d.main.cod.comp
		}
		switch {
		case tp.hdr.qcc[c] != nil:
			tc.q = tp.hdr.qcc[c]
		case tp.hdr.qcd != nil:
			tc.q = tp.hdr.qcd
		case d.main.qcc[c] != nil:
			tc.q = d.main.qcc[c]
		default:
			tc.q = d.main.qcd
		}
		if v, ok := tp.hdr.rgn[c]; ok {
			tc.roi = v
		} else {
			tc.roi = d.main.rgn[c]
		}
		sc := s.comps[c]
		tc.dx, tc.dy = sc.dx, sc.dy
		tc.x0, tc.y0 = ceilDiv(t.x0, sc.dx), ceilDiv(t.y0, sc.dy)
		tc.x1, tc.y1 = ceilDiv(t.x1, sc.dx), ceilDiv(t.y1, sc.dy)
		nl := tc.p.levels
		t.maxRes = max(t.maxRes, nl)
		tc.top = nl - d.opts.Reduce
		if tc.top < 0 {
			return nil, fmt.Errorf("jp2k: Reduce %d exceeds the %d decomposition levels of component %d in tile %d", d.opts.Reduce, nl, c, idx)
		}
		if tc.p.reversible && tc.q.style != 0 {
			return nil, fmt.Errorf("jp2k: component %d of tile %d combines the reversible 5/3 transform with scalar quantization", c, idx)
		}
		tc.res = make([]resolution, nl+1)
		for r := range tc.res {
			res := &tc.res[r]
			sh := nl - r
			res.x0, res.y0 = ceilShift(tc.x0, sh), ceilShift(tc.y0, sh)
			res.x1, res.y1 = ceilShift(tc.x1, sh), ceilShift(tc.y1, sh)
			res.ppx, res.ppy = int(tc.p.ppx[r]), int(tc.p.ppy[r])
			res.cbw, res.cbh = min(tc.p.cbw, res.ppx), min(tc.p.cbh, res.ppy)
			if r > 0 {
				res.cbw, res.cbh = min(tc.p.cbw, res.ppx-1), min(tc.p.cbh, res.ppy-1)
			}
			if res.x1 > res.x0 && res.y1 > res.y0 {
				res.px0, res.py0 = res.x0>>res.ppx, res.y0>>res.ppy
				res.pw = ceilShift(res.x1, res.ppx) - res.px0
				res.ph = ceilShift(res.y1, res.ppy) - res.py0
			}
			n := res.pw * res.ph
			slots += n
			if slots > maxPrecinctSlots {
				return nil, fmt.Errorf("jp2k: tile %d has more than %d precincts", idx, maxPrecinctSlots)
			}
			res.precincts = make([]*precinct, n)
			res.next = make([]uint16, n)
			if err := tc.setupBands(r, sc.prec); err != nil {
				return nil, fmt.Errorf("%w (tile %d, component %d)", err, idx, c)
			}
		}
		top := &tc.res[tc.top]
		tc.w, tc.h = top.x1-top.x0, top.y1-top.y0
	}
	return t, nil
}

// setupBands computes the sub-bands of resolution r: their coordinates (B-15), their place in the
// tile-component buffer, and their quantization (Annex E).
func (tc *tileComp) setupBands(r, prec int) error {
	res := &tc.res[r]
	nl := tc.p.levels
	if r == 0 {
		res.bands = []band{{orient: bandLL, x0: res.x0, y0: res.y0, x1: res.x1, y1: res.y1}}
	} else {
		nb := nl - r + 1
		lo := &tc.res[r-1]
		lw, lh := lo.x1-lo.x0, lo.y1-lo.y0
		res.bands = make([]band, 3)
		for i := range res.bands {
			o := i + 1
			xob, yob := o&1, o>>1
			b := &res.bands[i]
			b.orient = o
			b.x0, b.y0 = bandEdge(tc.x0, xob, nb), bandEdge(tc.y0, yob, nb)
			b.x1, b.y1 = bandEdge(tc.x1, xob, nb), bandEdge(tc.y1, yob, nb)
			b.offx, b.offy = xob*lw, yob*lh
			// The band must fill its quadrant of the resolution exactly; the code-block writes in
			// tier 1 rely on it.
			qw, qh := lw, lh
			if xob == 1 {
				qw = res.x1 - res.x0 - lw
			}
			if yob == 1 {
				qh = res.y1 - res.y0 - lh
			}
			if b.x1-b.x0 != qw || b.y1-b.y0 != qh {
				return fmt.Errorf("jp2k: internal error: sub-band geometry at resolution %d", r)
			}
		}
	}
	for i := range res.bands {
		b := &res.bands[i]
		idx := 0
		nb := nl
		if r > 0 {
			idx = 1 + 3*(r-1) + b.orient - 1
			nb = nl - r + 1
		}
		var exp, mant int
		q := tc.q
		if q.style == 1 { // scalar derived (E-5)
			exp, mant = q.exp[0]-nl+nb, q.mant[0]
		} else {
			if idx >= len(q.exp) {
				return fmt.Errorf("jp2k: quantization gives %d step sizes, sub-band %d needs more", len(q.exp), idx)
			}
			exp, mant = q.exp[idx], q.mant[idx]
		}
		b.mb = q.guard + exp - 1 + tc.roi
		if !tc.p.reversible {
			gain := [4]int{0, 1, 1, 2}[b.orient]
			step := float64(1)
			if q.style != 0 {
				step = math.Ldexp(1+float64(mant)/2048, prec+gain-exp)
			}
			b.step = float32(step / 2)
		}
	}
	return nil
}

// bandEdge is ceil((t - ob·2^(nb-1)) / 2^nb) (B-15), a sub-band coordinate at decomposition level
// nb >= 1, in 64 bits so that 32 levels fit on every platform.
func bandEdge(t, ob, nb int) int {
	num, den := int64(t)-int64(ob)<<(nb-1), int64(1)<<nb
	q := num / den
	if num%den != 0 && num > 0 {
		q++
	}
	return int(q)
}

// decodeTile runs tier 2, tier 1, dequantization and the inverse wavelet transform.
func (d *decoder) decodeTile(t *tile, tp *tileParts) error {
	body := tp.bodies[0]
	if len(tp.bodies) > 1 {
		n := 0
		for _, b := range tp.bodies {
			n += len(b)
		}
		body = make([]byte, 0, n)
		for _, b := range tp.bodies {
			body = append(body, b...)
		}
	}
	pr := &packetReader{t: t, body: body, sop: t.sop, eph: t.eph}
	switch {
	case d.usePPM:
		pr.packed, pr.hdr = true, tp.ppm
	case len(tp.ppt) > 0:
		pr.packed, pr.hdr = true, tp.ppt
	}
	if err := t.progression(pr.packet); err != nil && err != errDataEnded {
		return fmt.Errorf("%w (tile %d)", err, t.idx)
	}

	// Tier 1: every code-block of the decoded resolutions, written into its tile-component buffer.
	var jobs []cbJob
	for c := range t.comps {
		tc := &t.comps[c]
		if tc.p.reversible {
			tc.ibuf = make([]int32, tc.w*tc.h)
		} else {
			tc.fbuf = make([]float32, tc.w*tc.h)
		}
		for r := 0; r <= tc.top; r++ {
			res := &tc.res[r]
			for _, pc := range res.precincts {
				if pc == nil {
					continue
				}
				for bi := range pc.bands {
					pb := &pc.bands[bi]
					for k := range pb.blocks {
						if pb.blocks[k].passes > 0 {
							jobs = append(jobs, cbJob{tc: tc, b: &res.bands[bi], cb: &pb.blocks[k]})
						}
					}
				}
			}
		}
	}
	workers := d.opts.workers()
	t1s := make([]*t1, min(workers, max(len(jobs), 1)))
	parallel(len(jobs), len(t1s), func(w, i int) {
		if t1s[w] == nil {
			t1s[w] = new(t1)
		}
		t1s[w].decodeBlock(&jobs[i])
	})

	invK := float32(invK97)
	if d.opts.opjHighpass {
		invK = invK97OPJ
	}
	for c := range t.comps {
		idwt(&t.comps[c], workers, invK)
	}
	return nil
}

type cbJob struct {
	tc *tileComp
	b  *band
	cb *codeblock
}

// parallel calls fn(worker, i) for i in [0, n) on up to workers goroutines. Each i is handled once;
// worker identifies the goroutine so fn can keep per-goroutine scratch state.
func parallel(n, workers int, fn func(worker, i int)) {
	workers = min(workers, n)
	if workers <= 1 {
		for i := range n {
			fn(0, i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(w, i)
			}
		}()
	}
	wg.Wait()
}
