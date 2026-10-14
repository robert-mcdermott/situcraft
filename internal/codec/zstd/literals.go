// The literals section of a compressed block (RFC 8878, section 3.1.1.3.1) and the Huffman
// coding it uses (section 4.2): tree descriptions with direct or FSE-compressed weights, and one
// or four interleaved Huffman streams.

package zstd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
)

const (
	maxHuffBits      = 11 // "This specification limits the maximum code length to 11 bits."
	maxHuffWeightLog = 6  // accuracy log limit of the FSE-compressed weights
)

// huffTable decodes a Huffman stream by looking up the next maxBits bits (the longest code's
// length). Each entry is symbol | bits<<8; a shorter code fills all the entries that start with
// it. The table is sized to the tree, since a block's table is filled anew for each block.
type huffTable struct {
	ok    bool
	shift uint8 // 64 - maxBits: the lookup takes the container's top maxBits bits
	dt    [1 << maxHuffBits]uint16
}

// readHuffTree parses a Huffman tree description at the start of b and builds the table,
// returning the bytes consumed.
func (d *decoder) readHuffTree(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("zstd: Huffman tree description is missing")
	}
	var w [256]uint8
	var nw int
	header := int(b[0])
	n := 1
	if header >= 128 {
		// Direct representation: 4 bits per weight, the first in the high nibble.
		nw = header - 127
		size := (nw + 1) / 2
		if 1+size > len(b) {
			return 0, errors.New("zstd: Huffman tree description is truncated")
		}
		for i := 0; i < nw; i++ {
			v := b[1+i/2]
			if i&1 == 0 {
				v >>= 4
			}
			w[i] = v & 15
		}
		n += size
	} else {
		size := header
		if 1+size > len(b) {
			return 0, errors.New("zstd: Huffman tree description is truncated")
		}
		var err error
		nw, err = d.readFSEWeights(b[1:1+size], &w)
		if err != nil {
			return 0, err
		}
		n += size
	}
	if err := d.huf.build(w[:nw]); err != nil {
		return 0, err
	}
	if d.stats != nil {
		if header >= 128 {
			d.stats.directWeights++
		} else {
			d.stats.fseWeights++
		}
	}
	return n, nil
}

// readFSEWeights decodes FSE-compressed Huffman weights: two states take turns over one
// backward bitstream until it runs out (RFC 8878, section 4.2.1.2).
func (d *decoder) readFSEWeights(b []byte, w *[256]uint8) (int, error) {
	var norm [256]int16
	nsym, log, n, err := readFSEDescription(b, 255, maxHuffWeightLog, &norm)
	if err != nil {
		return 0, err
	}
	if n >= len(b) {
		return 0, errors.New("zstd: Huffman weight stream is missing")
	}
	var cells [1 << maxHuffWeightLog]fseCell
	if err := buildFSECells(norm[:nsym], log, cells[:1<<log]); err != nil {
		return 0, err
	}
	br, err := newRevBits(b[n:])
	if err != nil {
		return 0, err
	}
	rb, v, nb, off := br.b, br.bits, br.nbits, br.off
	mask := uint(1)<<log - 1
	// A new reader holds at least 56 bits: enough for both initial states.
	var x uint64
	v, nb, x = readBits(v, nb, log)
	s1 := uint(x)
	v, nb, x = readBits(v, nb, log)
	s2 := uint(x)
	if remainingBits(nb, off) < 0 {
		return 0, errors.New("zstd: Huffman weight stream is truncated")
	}
	// The states never exceed the table; the mask only spares the bounds checks. A round adds
	// two weights, or three when the stream runs out in it, and nw is even at its start, so the
	// check there keeps the count within 255 (the 256th symbol's weight is implied). It also
	// ends a stream whose states take no bits and so never run out.
	nw := 0
	for {
		if nw >= 254 {
			return 0, errTooManyWeights
		}
		if off >= 8 {
			v, nb, off = refillFast(rb, v, nb, off)
		} else {
			v, nb, off = refillSlow(rb, v, nb, off)
		}
		c := cells[s1&mask]
		w[nw] = c.sym
		nw++
		v, nb, x = readBits(v, nb, uint(c.nb))
		s1 = uint(c.next) + uint(x)
		if remainingBits(nb, off) < 0 {
			w[nw] = cells[s2&mask].sym
			nw++
			break
		}
		c = cells[s2&mask]
		w[nw] = c.sym
		nw++
		v, nb, x = readBits(v, nb, uint(c.nb))
		s2 = uint(c.next) + uint(x)
		if remainingBits(nb, off) < 0 {
			w[nw] = cells[s1&mask].sym
			nw++
			break
		}
	}
	return nw, nil
}

var errTooManyWeights = errors.New("zstd: Huffman tree has more than 255 weights")

// build makes the decoding table from the weights of all symbols but the last, whose weight is
// implied: the one that completes the sum of 2^(weight-1) to a power of two.
func (h *huffTable) build(w []uint8) error {
	h.ok = false
	var count [maxHuffBits + 1]int
	total := 0
	for _, x := range w {
		if x > maxHuffBits {
			return fmt.Errorf("zstd: Huffman weight %d exceeds %d", x, maxHuffBits)
		}
		count[x]++
		if x > 0 {
			total += 1 << (x - 1)
		}
	}
	if total == 0 {
		return errors.New("zstd: Huffman tree has no symbols")
	}
	maxBits := bits.Len(uint(total))
	if maxBits > maxHuffBits {
		return fmt.Errorf("zstd: Huffman codes would be %d bits long; the limit is %d", maxBits, maxHuffBits)
	}
	rest := 1<<maxBits - total
	if rest&(rest-1) != 0 {
		return errors.New("zstd: Huffman weights do not complete to a power of two")
	}
	last := uint8(bits.Len(uint(rest)))
	count[last]++
	// Codes are assigned from the lowest weight (longest code) up, in symbol order within a
	// weight, so in the table each weight's symbols follow all those of lower weights.
	var start [maxHuffBits + 2]int
	for x := 1; x <= maxHuffBits; x++ {
		start[x+1] = start[x] + count[x]<<(x-1)
	}
	nsym := len(w) + 1
	for s := 0; s < nsym; s++ {
		x := last
		if s < len(w) {
			x = w[s]
		}
		if x == 0 {
			continue
		}
		span := 1 << (x - 1)
		e := uint16(s) | uint16(maxBits+1-int(x))<<8
		p := start[x]
		// Spans are powers of two; fill them with whole-array stores.
		switch t := h.dt[p : p+span]; span {
		case 1:
			t[0] = e
		case 2:
			*(*[2]uint16)(t) = [2]uint16{e, e}
		case 4:
			*(*[4]uint16)(t) = [4]uint16{e, e, e, e}
		default:
			e8 := [8]uint16{e, e, e, e, e, e, e, e}
			for k := 0; k+8 <= len(t); k += 8 {
				*(*[8]uint16)(t[k : k+8]) = e8
			}
		}
		start[x] = p + span
	}
	h.shift = uint8(64 - maxBits)
	h.ok = true
	return nil
}

// decodeLiterals reads the literals section at the start of a compressed block. It returns the
// literals (pointing into blk for raw literals) and the bytes consumed.
func (d *decoder) decodeLiterals(blk []byte) ([]byte, int, error) {
	if len(blk) == 0 {
		return nil, 0, errors.New("zstd: compressed block is empty")
	}
	b0 := blk[0]
	typ := b0 & 3
	format := (b0 >> 2) & 3
	switch typ {
	case 0, 1: // Raw_Literals_Block, RLE_Literals_Block
		var size, hdr int
		switch format {
		case 0, 2:
			size, hdr = int(b0>>3), 1
		case 1:
			if len(blk) < 2 {
				return nil, 0, errTruncatedLiterals
			}
			size, hdr = int(b0>>4)|int(blk[1])<<4, 2
		case 3:
			if len(blk) < 3 {
				return nil, 0, errTruncatedLiterals
			}
			size, hdr = int(b0>>4)|int(blk[1])<<4|int(blk[2])<<12, 3
		}
		if size > d.blockMax {
			return nil, 0, fmt.Errorf("zstd: literals size %d exceeds the frame's block maximum of %d", size, d.blockMax)
		}
		if typ == 0 {
			if hdr+size > len(blk) {
				return nil, 0, errTruncatedLiterals
			}
			if d.stats != nil {
				d.stats.rawLiterals++
			}
			return blk[hdr : hdr+size], hdr + size, nil
		}
		if hdr >= len(blk) {
			return nil, 0, errTruncatedLiterals
		}
		lits := d.litBuffer(size)
		fill(lits, blk[hdr])
		if d.stats != nil {
			d.stats.rleLiterals++
		}
		return lits, hdr + 1, nil
	}

	// Compressed_Literals_Block (typ 2) or Treeless_Literals_Block (typ 3).
	var regen, comp, hdr int
	streams := 4
	switch format {
	case 0, 1:
		if len(blk) < 3 {
			return nil, 0, errTruncatedLiterals
		}
		h := uint32(blk[0]) | uint32(blk[1])<<8 | uint32(blk[2])<<16
		regen, comp, hdr = int(h>>4&0x3ff), int(h>>14&0x3ff), 3
		if format == 0 {
			streams = 1
		}
	case 2:
		if len(blk) < 4 {
			return nil, 0, errTruncatedLiterals
		}
		h := binary.LittleEndian.Uint32(blk)
		regen, comp, hdr = int(h>>4&0x3fff), int(h>>18&0x3fff), 4
	case 3:
		if len(blk) < 5 {
			return nil, 0, errTruncatedLiterals
		}
		h := uint64(binary.LittleEndian.Uint32(blk)) | uint64(blk[4])<<32
		regen, comp, hdr = int(h>>4&0x3ffff), int(h>>22&0x3ffff), 5
	}
	if regen > d.blockMax {
		return nil, 0, fmt.Errorf("zstd: literals size %d exceeds the frame's block maximum of %d", regen, d.blockMax)
	}
	if hdr+comp > len(blk) {
		return nil, 0, errTruncatedLiterals
	}
	data := blk[hdr : hdr+comp]
	if typ == 2 {
		n, err := d.readHuffTree(data)
		if err != nil {
			return nil, 0, err
		}
		data = data[n:]
		if d.stats != nil {
			d.stats.huffLiterals++
		}
	} else {
		if !d.huf.ok {
			return nil, 0, errors.New("zstd: treeless literals block without a previous Huffman table")
		}
		if d.stats != nil {
			d.stats.treelessLiterals++
		}
	}
	lits := d.litBuffer(regen)
	if streams == 1 {
		if d.stats != nil {
			d.stats.oneStream++
		}
		if err := d.huf.decode1(lits, data); err != nil {
			return nil, 0, err
		}
	} else {
		if d.stats != nil {
			d.stats.fourStreams++
		}
		if err := d.huf.decode4(lits, data); err != nil {
			return nil, 0, err
		}
	}
	return lits, hdr + comp, nil
}

var errTruncatedLiterals = errors.New("zstd: literals section is truncated")

// litBuffer returns a scratch buffer for n literals, reused across blocks.
func (d *decoder) litBuffer(n int) []byte {
	if cap(d.lits) < n {
		c := d.blockMax
		if c < n {
			c = n
		}
		d.lits = make([]byte, c+wildSlack)
	}
	return d.lits[:n]
}

var errHuffStream = errors.New("zstd: Huffman stream is corrupt (not consumed exactly)")

// huffStep decodes one symbol from a container (v, n): its top bits index the table. The masks
// cost nothing and spare a shift-range check and a bounds check.
func huffStep(dt *[1 << maxHuffBits]uint16, shift uint8, v uint64, n uint) (uint64, uint, byte) {
	e := dt[(v>>(shift&63))&(1<<maxHuffBits-1)]
	nb := uint(e >> 8)
	return v << nb, n - nb, uint8(e)
}

// refill tops a local container up; the decoding loops keep their readers in locals so that
// they stay in registers.
func refill(b []byte, v uint64, n uint, off int) (uint64, uint, int) {
	if off >= 8 {
		return refillFast(b, v, n, off)
	}
	return refillSlow(b, v, n, off)
}

// decode1 fills out from one Huffman stream.
func (h *huffTable) decode1(out []byte, src []byte) error {
	br, err := newRevBits(src)
	if err != nil {
		return err
	}
	dt, sh := &h.dt, h.shift
	b, v, n, off := br.b, br.bits, br.nbits, br.off
	i := 0
	// Five 11-bit codes fit in the 56 bits a refill guarantees.
	for ; i+5 <= len(out); i += 5 {
		if off >= 8 {
			v, n, off = refillFast(b, v, n, off)
		} else {
			v, n, off = refillSlow(b, v, n, off)
		}
		w := out[i : i+5 : i+5]
		v, n, w[0] = huffStep(dt, sh, v, n)
		v, n, w[1] = huffStep(dt, sh, v, n)
		v, n, w[2] = huffStep(dt, sh, v, n)
		v, n, w[3] = huffStep(dt, sh, v, n)
		v, n, w[4] = huffStep(dt, sh, v, n)
	}
	v, n, off = refill(b, v, n, off)
	for ; i < len(out); i++ {
		v, n, out[i] = huffStep(dt, sh, v, n)
	}
	if remainingBits(n, off) != 0 {
		return errHuffStream
	}
	return nil
}

// decode4 fills out from four Huffman streams described by a 6-byte jump table; each of the
// first three regenerates (len(out)+3)/4 bytes and the fourth the rest. The streams are decoded
// in one loop so that their table lookups overlap.
func (h *huffTable) decode4(out []byte, src []byte) error {
	if len(src) < 6 {
		return errors.New("zstd: Huffman jump table is truncated")
	}
	s1 := int(binary.LittleEndian.Uint16(src[0:]))
	s2 := int(binary.LittleEndian.Uint16(src[2:]))
	s3 := int(binary.LittleEndian.Uint16(src[4:]))
	src = src[6:]
	if s1+s2+s3 > len(src) {
		return errors.New("zstd: Huffman jump table points past the literals")
	}
	seg := (len(out) + 3) / 4
	if 3*seg > len(out) {
		return fmt.Errorf("zstd: %d literals are too few for four Huffman streams", len(out))
	}
	var br [4]revBits
	for k, s := range [4][]byte{src[:s1], src[s1 : s1+s2], src[s1+s2 : s1+s2+s3], src[s1+s2+s3:]} {
		var err error
		if br[k], err = newRevBits(s); err != nil {
			return err
		}
	}
	o0 := out[0:seg]
	o1 := out[seg : 2*seg]
	o2 := out[2*seg : 3*seg]
	o3 := out[3*seg:]
	dt, sh := &h.dt, h.shift
	b0, v0, n0, f0 := br[0].b, br[0].bits, br[0].nbits, br[0].off
	b1, v1, n1, f1 := br[1].b, br[1].bits, br[1].nbits, br[1].off
	b2, v2, n2, f2 := br[2].b, br[2].bits, br[2].nbits, br[2].off
	b3, v3, n3, f3 := br[3].b, br[3].bits, br[3].nbits, br[3].off
	// The fourth segment is the shortest (up to 3 bytes shorter), so it bounds the shared loop.
	i := 0
	for ; i+5 <= len(o3); i += 5 {
		if f0 >= 8 {
			v0, n0, f0 = refillFast(b0, v0, n0, f0)
		} else {
			v0, n0, f0 = refillSlow(b0, v0, n0, f0)
		}
		if f1 >= 8 {
			v1, n1, f1 = refillFast(b1, v1, n1, f1)
		} else {
			v1, n1, f1 = refillSlow(b1, v1, n1, f1)
		}
		if f2 >= 8 {
			v2, n2, f2 = refillFast(b2, v2, n2, f2)
		} else {
			v2, n2, f2 = refillSlow(b2, v2, n2, f2)
		}
		if f3 >= 8 {
			v3, n3, f3 = refillFast(b3, v3, n3, f3)
		} else {
			v3, n3, f3 = refillSlow(b3, v3, n3, f3)
		}
		w0 := o0[i : i+5 : i+5]
		w1 := o1[i : i+5 : i+5]
		w2 := o2[i : i+5 : i+5]
		w3 := o3[i : i+5 : i+5]
		for k := range 5 {
			v0, n0, w0[k] = huffStep(dt, sh, v0, n0)
			v1, n1, w1[k] = huffStep(dt, sh, v1, n1)
			v2, n2, w2[k] = huffStep(dt, sh, v2, n2)
			v3, n3, w3[k] = huffStep(dt, sh, v3, n3)
		}
	}
	br[0].bits, br[0].nbits, br[0].off = v0, n0, f0
	br[1].bits, br[1].nbits, br[1].off = v1, n1, f1
	br[2].bits, br[2].nbits, br[2].off = v2, n2, f2
	br[3].bits, br[3].nbits, br[3].off = v3, n3, f3
	// Finish each stream on its own.
	for s, o := range [4][]byte{o0, o1, o2, o3} {
		b, v, n, off := br[s].b, br[s].bits, br[s].nbits, br[s].off
		for k := i; k < len(o); k++ {
			if n < maxHuffBits {
				v, n, off = refill(b, v, n, off)
			}
			v, n, o[k] = huffStep(dt, sh, v, n)
		}
		if remainingBits(n, off) != 0 {
			return errHuffStream
		}
	}
	return nil
}
