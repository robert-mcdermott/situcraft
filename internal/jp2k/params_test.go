// Unit tests of rules the OpenJPEG encoder never exercises: the precedence of main and tile-part
// coding parameters (A.6), scalar derived quantization (E-5), tag trees (B.10.2) against an encoder
// written from the same clause, and the integer helpers.

package jp2k

import (
	"math"
	"math/rand/v2"
	"testing"
)

func quantFor(levels, exp int) *quantParams {
	q := &quantParams{style: 2, guard: 2}
	for range 3*levels + 1 {
		q.exp = append(q.exp, exp)
		q.mant = append(q.mant, 0)
	}
	return q
}

func compFor(levels int) compParams {
	c := compParams{levels: levels, cbw: 6, cbh: 6}
	for range levels + 1 {
		c.ppx, c.ppy = append(c.ppx, 15), append(c.ppy, 15)
	}
	return c
}

// TestParameterPrecedence checks A.6: tile COC > tile COD > main COC > main COD, the same for
// QCC/QCD, and a tile RGN over a main one.
func TestParameterPrecedence(t *testing.T) {
	d := &decoder{ntx: 1, nty: 1}
	d.siz = siz{x1: 64, y1: 64, tw: 64, th: 64, comps: []sizComp{{prec: 8, dx: 1, dy: 1}, {prec: 8, dx: 1, dy: 1}, {prec: 8, dx: 1, dy: 1}, {prec: 8, dx: 1, dy: 1}}}
	c5, c3, c4, c2 := compFor(5), compFor(3), compFor(4), compFor(2)
	d.main.cod = &codParams{layers: 1, comp: c5}
	d.main.coc = map[int]*compParams{1: &c3, 3: &c3}
	d.main.qcd = quantFor(5, 8)
	d.main.qcc = map[int]*quantParams{1: quantFor(5, 9), 3: quantFor(5, 9)}
	d.main.rgn = map[int]int{2: 3, 3: 4}

	// Without tile parameters the main header's apply.
	tp := &tileParts{}
	tl, err := d.setupTile(0, tp)
	if err != nil {
		t.Fatal(err)
	}
	for c, want := range []int{5, 3, 5, 3} {
		if got := tl.comps[c].p.levels; got != want {
			t.Errorf("main only: component %d has %d levels, want %d", c, got, want)
		}
	}
	for c, want := range []int{0, 0, 3, 4} {
		if got := tl.comps[c].roi; got != want {
			t.Errorf("main only: component %d ROI shift %d, want %d", c, got, want)
		}
	}

	// A tile COD overrides the main COC; a tile COC overrides the tile COD.
	tp.hdr.cod = &codParams{layers: 1, comp: c4}
	tp.hdr.coc = map[int]*compParams{2: &c2}
	tp.hdr.qcd = quantFor(5, 10)
	tp.hdr.qcc = map[int]*quantParams{2: quantFor(5, 11)}
	tp.hdr.rgn = map[int]int{3: 6}
	tl, err = d.setupTile(0, tp)
	if err != nil {
		t.Fatal(err)
	}
	for c, want := range []int{4, 4, 2, 4} {
		if got := tl.comps[c].p.levels; got != want {
			t.Errorf("tile: component %d has %d levels, want %d", c, got, want)
		}
	}
	for c, want := range []int{10, 10, 11, 10} {
		if got := tl.comps[c].q.exp[0]; got != want {
			t.Errorf("tile: component %d quantization exponent %d, want %d", c, got, want)
		}
	}
	for c, want := range []int{0, 0, 3, 6} {
		if got := tl.comps[c].roi; got != want {
			t.Errorf("tile: component %d ROI shift %d, want %d", c, got, want)
		}
	}
}

// TestDerivedQuantization checks E-5: with scalar derived quantization, sub-band b at decomposition
// level nb has exponent ε0 - NL + nb and the LL band's mantissa; Mb is G + ε - 1 (E-2) and the step
// 2^(R+gain-ε)(1+μ/2^11) (E-3).
func TestDerivedQuantization(t *testing.T) {
	d := &decoder{ntx: 1, nty: 1}
	d.siz = siz{x1: 64, y1: 64, tw: 64, th: 64, comps: []sizComp{{prec: 8, dx: 1, dy: 1}}}
	d.main.cod = &codParams{layers: 1, comp: compFor(3)}
	d.main.qcd = &quantParams{style: 1, guard: 2, exp: []int{10}, mant: []int{100}}
	tl, err := d.setupTile(0, &tileParts{})
	if err != nil {
		t.Fatal(err)
	}
	tc := &tl.comps[0]
	for r := range tc.res {
		nb := 3
		if r > 0 {
			nb = 3 - r + 1
		}
		eps := 10 - 3 + nb
		for _, b := range tc.res[r].bands {
			if b.mb != 2+eps-1 {
				t.Errorf("resolution %d band %d: Mb %d, want %d", r, b.orient, b.mb, 2+eps-1)
			}
			gain := [4]int{0, 1, 1, 2}[b.orient]
			step := math.Ldexp(1+100.0/2048, 8+gain-eps)
			if got := float64(b.step) * 2; math.Abs(got-step) > 1e-6*step {
				t.Errorf("resolution %d band %d: step %g, want %g", r, b.orient, got, step)
			}
		}
	}
}

// ttEncoder is a tag tree encoder written from B.10.2, the mirror of tagTree.decode.
type ttEncoder struct {
	tree  tagTree
	known []bool
	bits  []int
}

func newTTEncoder(w, h int, leaves []int) *ttEncoder {
	e := &ttEncoder{tree: newTagTree(w, h)}
	e.known = make([]bool, len(e.tree.nodes))
	for i := range e.tree.nodes {
		e.tree.nodes[i].value = math.MaxInt32
	}
	for y := range h {
		for x := range w {
			v := int32(leaves[y*w+x])
			for lev := range e.tree.offs {
				n := &e.tree.nodes[e.tree.offs[lev]+(y>>lev)*e.tree.widths[lev]+(x>>lev)]
				n.value = min(n.value, v) // a parent holds the minimum of its children
			}
		}
	}
	return e
}

func (e *ttEncoder) encode(x, y, threshold int) {
	t := &e.tree
	low := int32(0)
	for lev := len(t.offs) - 1; lev >= 0; lev-- {
		i := t.offs[lev] + (y>>lev)*t.widths[lev] + (x >> lev)
		n := &t.nodes[i]
		if low > n.low {
			n.low = low
		} else {
			low = n.low
		}
		for low < int32(threshold) {
			if low >= n.value {
				if !e.known[i] {
					e.bits = append(e.bits, 1)
					e.known[i] = true
				}
				break
			}
			e.bits = append(e.bits, 0)
			low++
		}
		n.low = low
	}
}

// packBits packs bits most significant first with the packet-header bit stuffing (B.10.1).
func packBits(bits []int) []byte {
	var out []byte
	cur, n, room := 0, 0, 8
	for _, b := range bits {
		cur = cur<<1 | b
		n++
		if n == room {
			out = append(out, byte(cur))
			room = 8
			if cur == 0xFF {
				room = 7
			}
			cur, n = 0, 0
		}
	}
	if n > 0 {
		out = append(out, byte(cur<<(room-n)))
	}
	return out
}

func TestTagTree(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for trial := range 200 {
		w, h := 1+rng.IntN(9), 1+rng.IntN(9)
		leaves := make([]int, w*h)
		for i := range leaves {
			leaves[i] = rng.IntN(6)
		}
		enc := newTTEncoder(w, h, leaves)
		// Layers of inclusion-style queries: every leaf with thresholds 1, 2, ..., as packet
		// headers ask them.
		type query struct{ x, y, th int }
		var qs []query
		for th := 1; th <= 7; th++ {
			for y := range h {
				for x := range w {
					qs = append(qs, query{x, y, th})
				}
			}
		}
		for _, q := range qs {
			enc.encode(q.x, q.y, q.th)
		}
		br := &bitReader{data: packBits(enc.bits)}
		dec := newTagTree(w, h)
		for _, q := range qs {
			v, err := dec.decode(br, q.x, q.y, q.th)
			if err != nil {
				t.Fatalf("trial %d: %v", trial, err)
			}
			want := leaves[q.y*w+q.x]
			if (v < q.th) != (want < q.th) || v < q.th && v != want {
				t.Fatalf("trial %d leaf (%d,%d) threshold %d: decoded %d, value %d", trial, q.x, q.y, q.th, v, want)
			}
		}
	}
}

func TestIntegerHelpers(t *testing.T) {
	for a := -20; a <= 20; a++ {
		for b := 1; b <= 7; b++ {
			if got, want := ceilDiv(a, b), int(math.Ceil(float64(a)/float64(b))); got != want {
				t.Errorf("ceilDiv(%d, %d) = %d, want %d", a, b, got, want)
			}
		}
	}
	for a := 0; a < 100; a++ {
		for s := 0; s < 8; s++ {
			if got, want := ceilShift(a, s), ceilDiv(a, 1<<s); got != want {
				t.Errorf("ceilShift(%d, %d) = %d, want %d", a, s, got, want)
			}
		}
	}
}
