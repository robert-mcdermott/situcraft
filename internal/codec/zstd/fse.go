// Finite State Entropy tables (RFC 8878, section 4.1): reading a table description, spreading the
// symbols over the states, and the predefined distributions for the sequence codes
// (section 3.1.1.3.2.2).

package zstd

import (
	"errors"
	"fmt"
	"math/bits"
)

// fseCell is one state of an FSE decoding table: the symbol it decodes, and how to reach the
// next state (next + the following nb bits).
type fseCell struct {
	sym  uint8
	nb   uint8
	next uint16
}

// readFSEDescription parses an FSE table description at the start of b into norm and returns
// the number of symbols described, the accuracy log, and the bytes consumed (a description ends
// on a byte boundary).
func readFSEDescription(b []byte, maxSym int, maxLog uint, norm *[256]int16) (nsym int, log uint, n int, err error) {
	if len(b) == 0 {
		return 0, 0, 0, errors.New("zstd: FSE table description is missing")
	}
	r := fwdBits{b: b}
	log = uint(r.read(4)) + 5
	if log > maxLog {
		return 0, 0, 0, fmt.Errorf("zstd: FSE accuracy log %d exceeds the maximum of %d", log, maxLog)
	}
	// remaining is the probability still to be distributed, plus one; threshold is the power
	// of two the next value is coded against; nb = log2(threshold) + 1.
	remaining := int32(1)<<log + 1
	threshold := int32(1) << log
	nb := log + 1
	sym := 0
	for remaining > 1 {
		if sym > maxSym {
			return 0, 0, 0, fmt.Errorf("zstd: FSE table describes more than %d symbols", maxSym+1)
		}
		// Values below lowLimit fit in nb-1 bits; "small values use 1 fewer bit".
		lowLimit := 2*threshold - 1 - remaining
		v := int32(r.peek(nb))
		var value int32
		if low := v & (threshold - 1); low < lowLimit {
			value = low
			r.pos += nb - 1
		} else {
			value = v & (2*threshold - 1)
			if value >= threshold {
				value -= lowLimit
			}
			r.pos += nb
		}
		p := value - 1 // -1 is the "less than 1" probability, which takes one state
		if p < 0 {
			remaining--
		} else {
			remaining -= p
		}
		norm[sym] = int16(p)
		sym++
		if p == 0 {
			// A zero probability is followed by 2-bit repeat flags: how many more zeros, with
			// 3 meaning "3, and another flag follows".
			for {
				rep := int(r.read(2))
				if sym+rep > maxSym+1 {
					return 0, 0, 0, fmt.Errorf("zstd: FSE table describes more than %d symbols", maxSym+1)
				}
				for k := 0; k < rep; k++ {
					norm[sym] = 0
					sym++
				}
				if rep != 3 {
					break
				}
				if r.overrun() {
					return 0, 0, 0, errors.New("zstd: FSE table description is truncated")
				}
			}
		}
		for remaining < threshold {
			nb--
			threshold >>= 1
		}
		if r.overrun() {
			return 0, 0, 0, errors.New("zstd: FSE table description is truncated")
		}
	}
	if remaining != 1 {
		return 0, 0, 0, errors.New("zstd: FSE table probabilities do not add up")
	}
	return sym, log, int((r.pos + 7) >> 3), nil
}

// spreadFSE assigns a symbol to each of the 1<<log states of a normalized distribution, as RFC
// 8878 section 4.1.1 lays out: "less than 1" symbols take the last states, the others are
// spread with a fixed step. next receives each symbol's first next-state counter.
func spreadFSE(norm []int16, log uint, sym []uint8, next *[256]uint16) error {
	size := 1 << log
	high := size - 1
	for s, p := range norm {
		if p == -1 {
			sym[high] = uint8(s)
			high--
			next[s] = 1
		} else {
			next[s] = uint16(p)
		}
	}
	pos := 0
	step := size>>1 + size>>3 + 3
	mask := size - 1
	for s, p := range norm {
		for i := int16(0); i < p; i++ {
			sym[pos] = uint8(s)
			pos = (pos + step) & mask
			for pos > high {
				pos = (pos + step) & mask
			}
		}
	}
	if pos != 0 {
		return errors.New("zstd: FSE table is corrupt")
	}
	return nil
}

// nextStep gives a state's way to the next one: a symbol's states, in order, take its
// next-state counters from its count up, and each reads as many bits as bring the counter up to
// the table size.
func nextStep(ns uint16, log uint) (nb uint8, base uint16) {
	n := log - uint(bits.Len16(ns)-1)
	return uint8(n), uint16(int(ns)<<n - 1<<log)
}

// buildFSECells makes a plain decoding table (for the Huffman weights).
func buildFSECells(norm []int16, log uint, cells []fseCell) error {
	var sym [1 << maxHuffWeightLog]uint8
	var next [256]uint16
	if err := spreadFSE(norm, log, sym[:1<<log], &next); err != nil {
		return err
	}
	for i := range 1 << log {
		s := sym[i]
		nb, base := nextStep(next[s], log)
		next[s]++
		cells[i] = fseCell{sym: s, nb: nb, next: base}
	}
	return nil
}

// Sequence codes (RFC 8878, section 3.1.1.3.2.1.1). A literal-length or match-length code stands
// for a baseline plus a number of extra bits; an offset code N for 1<<N plus N extra bits.
const (
	maxLLCode = 35
	maxMLCode = 52
	maxOFCode = 31

	maxLLLog = 9
	maxMLLog = 9
	maxOFLog = 8
)

var llBase = [maxLLCode + 1]uint32{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 18, 20, 22, 24, 28, 32, 40, 48, 64, 128, 256, 512, 1024, 2048, 4096,
	8192, 16384, 32768, 65536,
}

var llExtra = [maxLLCode + 1]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3, 4, 6, 7, 8, 9, 10, 11, 12,
	13, 14, 15, 16,
}

var mlBase = [maxMLCode + 1]uint32{
	3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18,
	19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34,
	35, 37, 39, 41, 43, 47, 51, 59, 67, 83, 99, 131, 259, 515, 1027, 2051,
	4099, 8195, 16387, 32771, 65539,
}

var mlExtra = [maxMLCode + 1]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3, 4, 4, 5, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16,
}

// Predefined distributions (RFC 8878, section 3.1.1.3.2.2).
var (
	llDefault = []int16{
		4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
		-1, -1, -1, -1,
	}
	mlDefault = []int16{
		1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
		-1, -1, -1, -1, -1,
	}
	ofDefault = []int16{
		1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1,
	}
)

// seqKind is one of the three fields of a sequence.
type seqKind uint8

const (
	kindLL seqKind = iota
	kindOF
	kindML
)

func (k seqKind) maxCode() int {
	switch k {
	case kindLL:
		return maxLLCode
	case kindOF:
		return maxOFCode
	}
	return maxMLCode
}

func (k seqKind) maxLog() uint {
	switch k {
	case kindLL:
		return maxLLLog
	case kindOF:
		return maxOFLog
	}
	return maxMLLog
}

func (k seqKind) String() string {
	switch k {
	case kindLL:
		return "literal lengths"
	case kindOF:
		return "offsets"
	}
	return "match lengths"
}

// seqEntry is one state of a sequence decoding table, with the decoded code already turned into
// its baseline and extra-bit count so the hot loop needs one lookup per field.
type seqEntry struct {
	base  uint32 // literal length, match length, or offset value before the extra bits
	next  uint16 // next state = next + the following nb bits
	nb    uint8
	extra uint8
}

// seqTable is a decoding table for one sequence field; 512 entries (the largest accuracy log,
// 9) so that a state masked with 511 never needs a bounds check.
type seqTable struct {
	log     uint
	entries [512]seqEntry
}

// codeEntries holds, for each field and code, the entry's baseline and extra bits.
var codeEntries = func() (t [3][64]seqEntry) {
	for c := range maxLLCode + 1 {
		t[kindLL][c] = seqEntry{base: llBase[c], extra: llExtra[c]}
	}
	for c := range maxOFCode + 1 {
		t[kindOF][c] = seqEntry{base: 1 << c, extra: uint8(c)}
	}
	for c := range maxMLCode + 1 {
		t[kindML][c] = seqEntry{base: mlBase[c], extra: mlExtra[c]}
	}
	return t
}()

// build fills the table from a normalized distribution.
func (t *seqTable) build(k seqKind, norm []int16, log uint) error {
	var sym [512]uint8
	var next [256]uint16
	if err := spreadFSE(norm, log, sym[:1<<log], &next); err != nil {
		return err
	}
	codes := &codeEntries[k]
	for i := range 1 << log {
		s := sym[i]
		e := codes[s&63] // s is at most the field's largest code, checked when it was read
		e.nb, e.next = nextStep(next[s], log)
		next[s]++
		t.entries[i] = e
	}
	t.log = log
	return nil
}

// rle makes a one-state table that always decodes code.
func (t *seqTable) rle(k seqKind, code uint8) {
	t.entries[0] = codeEntries[k][code&63]
	t.log = 0
}

var predefined = func() [3]*seqTable {
	var t [3]*seqTable
	for k, d := range [3]struct {
		norm []int16
		log  uint
	}{{llDefault, 6}, {ofDefault, 5}, {mlDefault, 6}} {
		t[k] = new(seqTable)
		if err := t[k].build(seqKind(k), d.norm, d.log); err != nil {
			panic(err) // the predefined distributions are constants
		}
	}
	return t
}()
