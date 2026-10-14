// Package zstd decompresses Zstandard data, as specified by RFC 8878 (Collet & Kucherawy, 2021),
// "Zstandard Compression and the application/zstd Media Type". SituCraft meets it in Parquet
// pages and inside Blosc-compressed Zarr chunks. Frame content checksums are verified with
// XXH64 (Yann Collet). Written from those specifications, it decodes everything the RFC defines
// except dictionaries, which those formats do not use.
//
// The whole output is held in memory, so the window never needs a separate buffer: a match may
// reach anywhere back in the current frame's output, within the frame's declared window.

package zstd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
)

const (
	frameMagic         = 0xFD2FB528
	skippableMagicMask = 0xFFFFFFF0
	skippableMagic     = 0x184D2A50

	// "Block_Maximum_Size ... is the smallest of Window_Size and 128 KB" (section 3.1.1.2.4).
	maxBlockSize = 128 << 10

	// The default output limit; min keeps it representable on 32-bit platforms.
	defaultMaxSize = min(1<<31, math.MaxInt)

	// Spare bytes allocated past the output and the literals, so that short copies can move
	// 16 or 32 bytes at once.
	wildSlack = 32
)

// Decode decompresses every frame in src (Zstandard frames, possibly several concatenated, and
// skippable frames, which are ignored), appending the output to dst. It fails, without allocating
// beyond the limit, when the output would exceed maxSize bytes (maxSize <= 0 means 1 << 31).
// Decode may use dst's spare capacity past the output as scratch space. On error it returns dst
// as it was given.
func Decode(dst, src []byte, maxSize int) ([]byte, error) {
	d := decoders.Get().(*decoder)
	out, err := d.decode(dst, src, maxSize)
	d.release()
	decoders.Put(d)
	if err != nil {
		return dst, err
	}
	return out, nil
}

// FrameContentSize reports the decompressed size declared by the first frame's header, if any.
// Skippable frames before it are passed over.
func FrameContentSize(src []byte) (size uint64, ok bool) {
	for len(src) >= 4 {
		magic := binary.LittleEndian.Uint32(src)
		switch {
		case magic == frameMagic:
			h, err := readFrameHeader(src)
			if err != nil || !h.hasSize {
				return 0, false
			}
			return h.contentSize, true
		case magic&skippableMagicMask == skippableMagic:
			if len(src) < 8 {
				return 0, false
			}
			n := binary.LittleEndian.Uint32(src[4:])
			if uint64(n) > uint64(len(src)-8) {
				return 0, false
			}
			src = src[8+int(n):]
		default:
			return 0, false
		}
	}
	return 0, false
}

// decoder holds the state of one Decode call; decoders are pooled so that repeated calls reuse
// the tables and the literals buffer.
type decoder struct {
	out     []byte
	limit   int // len(out) may not pass this: len(dst) + maxSize
	maxSize int

	// Frame state.
	frameStart int
	fcs        uint64
	fcsEnd     int // where the declared content ends in out, or MaxInt
	window     int // Window_Size, clamped to MaxInt
	blockMax   int
	rep        [3]int
	huf        huffTable
	tables     [3]*seqTable // the tables in use, kept for Repeat_Mode
	own        [3]seqTable  // storage for tables built from the stream
	firstFrame bool

	lits  []byte
	stats *stats // feature counts for the tests; nil otherwise
}

var decoders = sync.Pool{New: func() any { return new(decoder) }}

// release drops references to the caller's memory before the decoder goes back to the pool.
func (d *decoder) release() {
	d.out = nil
	d.stats = nil
}

// stats counts what the decoder met, so the tests can show which parts of the format the
// fixtures exercise.
type stats struct {
	frames, skippable, checksums                 int
	rawBlocks, rleBlocks, compressedBlocks       int
	rawLiterals, rleLiterals                     int
	huffLiterals, treelessLiterals               int
	oneStream, fourStreams                       int
	directWeights, fseWeights                    int
	emptySequences, sequences                    int
	modes                                        [3][4]int // [field][mode]
	repeats                                      [4]int    // repeat offset 1, 2, 3, and offset 1 minus one
	overlaps, crossBlock, maxOffset              int
	windowDescriptor, singleSegment, unknownSize int
}

func (d *decoder) decode(dst, src []byte, maxSize int) ([]byte, error) {
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	d.out = dst
	d.maxSize = maxSize
	d.limit = math.MaxInt
	if maxSize < math.MaxInt-len(dst) {
		d.limit = len(dst) + maxSize
	}
	d.firstFrame = true
	for len(src) > 0 {
		if len(src) < 4 {
			return nil, fmt.Errorf("zstd: %d stray bytes after the last frame", len(src))
		}
		magic := binary.LittleEndian.Uint32(src)
		switch {
		case magic == frameMagic:
			n, err := d.frame(src)
			if err != nil {
				return nil, err
			}
			src = src[n:]
			d.firstFrame = false
		case magic&skippableMagicMask == skippableMagic:
			if len(src) < 8 {
				return nil, errors.New("zstd: skippable frame header is truncated")
			}
			n := binary.LittleEndian.Uint32(src[4:])
			if uint64(n) > uint64(len(src)-8) {
				return nil, fmt.Errorf("zstd: skippable frame of %d bytes is truncated to %d", n, len(src)-8)
			}
			src = src[8+int(n):]
			if d.stats != nil {
				d.stats.skippable++
			}
		default:
			return nil, fmt.Errorf("zstd: unknown magic number 0x%08x (not a Zstandard frame)", magic)
		}
	}
	return d.out, nil
}

type frameHeader struct {
	size          int // magic number and header
	window        uint64
	contentSize   uint64
	hasSize       bool
	singleSegment bool
	checksum      bool
	dictID        uint32
}

var errTruncatedHeader = errors.New("zstd: frame header is truncated")

// readFrameHeader parses the header of the frame at the start of src, magic number included
// (RFC 8878, section 3.1.1.1).
func readFrameHeader(src []byte) (frameHeader, error) {
	var h frameHeader
	if len(src) < 5 {
		return h, errTruncatedHeader
	}
	fhd := src[4]
	if fhd&0x08 != 0 {
		return h, errors.New("zstd: reserved bit set in the frame header descriptor")
	}
	h.singleSegment = fhd&0x20 != 0
	h.checksum = fhd&0x04 != 0
	dictSize := [4]int{0, 1, 2, 4}[fhd&3]
	fcsSize := [4]int{0, 2, 4, 8}[fhd>>6]
	if fcsSize == 0 && h.singleSegment {
		fcsSize = 1
	}
	n := 5 + dictSize + fcsSize
	if !h.singleSegment {
		n++
	}
	if len(src) < n {
		return h, errTruncatedHeader
	}
	p := 5
	if !h.singleSegment {
		// Window_Size = 2^(10+Exponent) plus Mantissa eighths of that.
		wd := src[p]
		p++
		base := uint64(1) << (10 + wd>>3)
		h.window = base + base/8*uint64(wd&7)
	}
	switch dictSize {
	case 1:
		h.dictID = uint32(src[p])
	case 2:
		h.dictID = uint32(binary.LittleEndian.Uint16(src[p:]))
	case 4:
		h.dictID = binary.LittleEndian.Uint32(src[p:])
	}
	p += dictSize
	switch fcsSize {
	case 1:
		h.contentSize = uint64(src[p])
	case 2:
		h.contentSize = uint64(binary.LittleEndian.Uint16(src[p:])) + 256
	case 4:
		h.contentSize = uint64(binary.LittleEndian.Uint32(src[p:]))
	case 8:
		h.contentSize = binary.LittleEndian.Uint64(src[p:])
	}
	h.hasSize = fcsSize > 0
	if h.singleSegment {
		h.window = h.contentSize
	}
	h.size = n
	return h, nil
}

// frame decodes the Zstandard frame at the start of src and returns its length.
func (d *decoder) frame(src []byte) (int, error) {
	h, err := readFrameHeader(src)
	if err != nil {
		return 0, err
	}
	if h.dictID != 0 {
		return 0, fmt.Errorf("zstd: dictionary %d is required (dictionaries are not supported)", h.dictID)
	}
	d.window = math.MaxInt
	if h.window < uint64(math.MaxInt) {
		d.window = int(h.window)
	}
	d.blockMax = min(d.window, maxBlockSize)
	d.frameStart = len(d.out)
	d.fcs = h.contentSize
	d.fcsEnd = math.MaxInt
	if h.hasSize {
		room := uint64(d.limit - len(d.out))
		if h.contentSize > room {
			if d.firstFrame {
				return 0, fmt.Errorf("zstd: frame content size %d exceeds the limit of %d bytes", h.contentSize, d.maxSize)
			}
			return 0, fmt.Errorf("zstd: frame content size %d would take the output past the limit of %d bytes", h.contentSize, d.maxSize)
		}
		// A block that produces anything takes at least 4 bytes (a 3-byte header and a byte of
		// content), so the rest of the input cannot hold more than blockMax per 4 bytes. A
		// larger claim is corrupt, and is refused before the output is sized for it.
		if most := uint64((len(src)-h.size)/4) * uint64(d.blockMax); h.contentSize > most {
			return 0, fmt.Errorf("zstd: frame content size %d is more than %d bytes of frame can hold", h.contentSize, len(src)-h.size)
		}
		d.fcsEnd = len(d.out) + int(h.contentSize)
		if d.firstFrame {
			d.reserveExact(int(h.contentSize))
		}
	}
	d.rep = [3]int{1, 4, 8}
	d.huf.ok = false
	d.tables = [3]*seqTable{}
	if d.stats != nil {
		d.stats.frames++
		if h.checksum {
			d.stats.checksums++
		}
		if h.singleSegment {
			d.stats.singleSegment++
		} else {
			d.stats.windowDescriptor++
		}
		if !h.hasSize {
			d.stats.unknownSize++
		}
	}

	p := h.size
	for {
		if len(src)-p < 3 {
			return 0, errors.New("zstd: block header is truncated")
		}
		bh := uint32(src[p]) | uint32(src[p+1])<<8 | uint32(src[p+2])<<16
		p += 3
		last := bh&1 != 0
		size := int(bh >> 3)
		typ := (bh >> 1) & 3
		if typ == 3 {
			return 0, errors.New("zstd: reserved block type")
		}
		if size > d.blockMax {
			return 0, fmt.Errorf("zstd: block size %d exceeds the frame's maximum of %d", size, d.blockMax)
		}
		switch typ {
		case 0: // Raw_Block
			if size > len(src)-p {
				return 0, fmt.Errorf("zstd: raw block of %d bytes is truncated to %d", size, len(src)-p)
			}
			if size > d.blockRoom() {
				return 0, d.tooLarge(size)
			}
			d.reserve(size)
			d.out = append(d.out, src[p:p+size]...)
			p += size
			if d.stats != nil {
				d.stats.rawBlocks++
			}
		case 1: // RLE_Block: one byte, repeated Block_Size times
			if p >= len(src) {
				return 0, errors.New("zstd: RLE block is truncated")
			}
			if size > d.blockRoom() {
				return 0, d.tooLarge(size)
			}
			d.reserve(size)
			n := len(d.out)
			d.out = d.out[:n+size]
			fill(d.out[n:], src[p])
			p++
			if d.stats != nil {
				d.stats.rleBlocks++
			}
		case 2: // Compressed_Block
			if size > len(src)-p {
				return 0, fmt.Errorf("zstd: compressed block of %d bytes is truncated to %d", size, len(src)-p)
			}
			// The capacity reaches to the end of the input, so that copies of raw literals
			// may read past them, never past src.
			if err := d.compressedBlock(src[p : p+size : len(src)]); err != nil {
				return 0, err
			}
			p += size
			if d.stats != nil {
				d.stats.compressedBlocks++
			}
		}
		if last {
			break
		}
	}
	got := len(d.out) - d.frameStart
	if h.hasSize && uint64(got) != h.contentSize {
		return 0, fmt.Errorf("zstd: frame decompressed to %d bytes, but its header declares %d", got, h.contentSize)
	}
	if h.checksum {
		if len(src)-p < 4 {
			return 0, errors.New("zstd: content checksum is truncated")
		}
		want := binary.LittleEndian.Uint32(src[p:])
		if uint32(xxh64(d.out[d.frameStart:])) != want {
			return 0, errors.New("zstd: content checksum mismatch")
		}
		p += 4
	}
	return p, nil
}

// blockRoom is how many bytes the next block may produce: the block maximum, the rest of the
// declared content size, and the output limit all bound it.
func (d *decoder) blockRoom() int {
	room := d.blockMax
	if r := d.fcsEnd - len(d.out); r < room {
		room = r
	}
	if r := d.limit - len(d.out); r < room {
		room = r
	}
	return room
}

// tooLarge names the bound a block producing size bytes would break.
func (d *decoder) tooLarge(size int) error {
	switch {
	case size > d.blockMax:
		return fmt.Errorf("zstd: block decompresses to more than the frame's block maximum of %d bytes", d.blockMax)
	case len(d.out) > d.fcsEnd-size:
		return fmt.Errorf("zstd: frame decompresses to more than its declared content size of %d bytes", d.fcs)
	}
	return fmt.Errorf("zstd: decompressed size exceeds the limit of %d bytes", d.maxSize)
}

// reserve makes room for n more bytes, growing geometrically but never past the limit.
func (d *decoder) reserve(n int) {
	need := len(d.out) + n
	if need <= cap(d.out) {
		return
	}
	c := 2 * cap(d.out)
	if c < need || c < 0 {
		c = need
	}
	if c > d.limit {
		c = d.limit
	}
	d.grow(c)
}

// reserveExact makes room for exactly n more bytes: the frame's declared content size.
func (d *decoder) reserveExact(n int) {
	if need := len(d.out) + n; need > cap(d.out) {
		d.grow(need)
	}
}

func (d *decoder) grow(c int) {
	if c <= math.MaxInt-wildSlack {
		c += wildSlack
	}
	grown := make([]byte, len(d.out), c)
	copy(grown, d.out)
	d.out = grown
}

// compressedBlock decodes a Compressed_Block: a literals section, then a sequences section
// (RFC 8878, section 3.1.1.3).
func (d *decoder) compressedBlock(blk []byte) error {
	lits, n, err := d.decodeLiterals(blk)
	if err != nil {
		return err
	}
	room := d.blockRoom()
	d.reserve(room)
	return d.decodeSequences(blk[n:], lits, room)
}

func fill(b []byte, v byte) {
	if len(b) == 0 {
		return
	}
	b[0] = v
	for k := 1; k < len(b); k *= 2 {
		copy(b[k:], b[:k])
	}
}
