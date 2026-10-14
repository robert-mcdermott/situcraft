// Bit readers for the two directions Zstandard uses (RFC 8878, section 4.1): FSE table
// descriptions are read forward, least significant bit first; Huffman and FSE bitstreams are
// written forward and read backward, starting at the last byte's final-bit-flag.

package zstd

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

// fwdBits reads a forward bitstream. Reads past the end return zeros; the caller checks overrun,
// so that the reading loop stays simple.
type fwdBits struct {
	b   []byte
	pos uint // bits consumed
}

// peek returns the next n bits (n <= 25) without consuming them.
func (r *fwdBits) peek(n uint) uint32 {
	i := r.pos >> 3
	var v uint32
	if i+4 <= uint(len(r.b)) {
		v = binary.LittleEndian.Uint32(r.b[i:])
	} else {
		for k := uint(0); k < 4 && i+k < uint(len(r.b)); k++ {
			v |= uint32(r.b[i+k]) << (8 * k)
		}
	}
	return (v >> (r.pos & 7)) & (1<<n - 1)
}

func (r *fwdBits) read(n uint) uint32 {
	v := r.peek(n)
	r.pos += n
	return v
}

// overrun reports whether more bits were consumed than the stream holds.
func (r *fwdBits) overrun() bool { return r.pos > 8*uint(len(r.b)) }

// revBits reads a backward bitstream. The container holds the next bits left-aligned: the most
// significant bit is the next one to read. Once the start of the stream is reached it supplies
// zeros, as RFC 8878 prescribes for the end of the Huffman weight stream; every caller checks
// remaining() to tell a well-formed stream from an over-read one. Four fields, so that the
// compiler can keep a local copy in registers; the decoding loops work on such copies.
type revBits struct {
	b     []byte
	bits  uint64 // next bits, left-aligned
	nbits uint   // valid bits in the container
	// off > 0: b[:off] is not yet loaded. off <= 0: every byte is loaded, and -off zero bits
	// have been supplied past the start of the stream.
	off int
}

var errNoEndMark = errors.New("zstd: bitstream's last byte is zero (missing final-bit-flag)")

func newRevBits(b []byte) (revBits, error) {
	if len(b) == 0 {
		return revBits{}, errors.New("zstd: empty bitstream")
	}
	last := b[len(b)-1]
	if last == 0 {
		return revBits{}, errNoEndMark
	}
	r := revBits{b: b, off: len(b)}
	r.bits, r.nbits, r.off = refillSlow(b, 0, 0, len(b))
	// Skip the final-bit-flag and the zeros above it.
	skip := uint(9 - bits.Len8(last))
	r.bits <<= skip
	r.nbits -= skip
	return r, nil
}

// refill tops the container up to at least 56 valid bits.
func (r *revBits) refill() {
	if r.off >= 8 {
		r.bits, r.nbits, r.off = refillFast(r.b, r.bits, r.nbits, r.off)
	} else {
		r.bits, r.nbits, r.off = refillSlow(r.b, r.bits, r.nbits, r.off)
	}
}

// refillFast loads whole bytes while at least 8 remain (off >= 8). The 8 bytes ending at off,
// read little-endian, put b[off-1] in the top byte: the stream's order. Bits below the counted
// ones are the stream's next bits too, so ORing them in again later is harmless.
func refillFast(b []byte, v uint64, n uint, off int) (uint64, uint, int) {
	v |= binary.LittleEndian.Uint64(b[off-8:]) >> n
	k := (63 - n) >> 3
	return v, n + 8*k, off - int(k)
}

// refillSlow loads the stream's first bytes one at a time, then supplies zeros.
func refillSlow(b []byte, v uint64, n uint, off int) (uint64, uint, int) {
	for n <= 56 && off > 0 {
		off--
		v |= uint64(b[off]) << (56 - n)
		n += 8
	}
	if off <= 0 && n <= 56 {
		// Past the start: the bits below are zeros.
		off -= int(64 - n)
		n = 64
	}
	return v, n, off
}

// read consumes n bits (n <= 56 after a refill). The caller ensures enough bits are loaded.
func (r *revBits) read(n uint) uint64 {
	v := r.bits >> (64 - n) // a shift by 64 yields 0 in Go, so n == 0 reads nothing
	r.bits <<= n
	r.nbits -= n
	return v
}

// readSafe refills before reading; for the cold paths.
func (r *revBits) readSafe(n uint) uint64 {
	r.refill()
	return r.read(n)
}

// remaining is the number of the stream's own bits not yet read; negative once zeros past the
// start have been consumed.
func (r *revBits) remaining() int { return remainingBits(r.nbits, r.off) }

func remainingBits(n uint, off int) int {
	if off > 0 {
		return 8*off + int(n)
	}
	return int(n) + off
}
