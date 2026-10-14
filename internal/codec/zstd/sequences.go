// The sequences section of a compressed block (RFC 8878, section 3.1.1.3.2) and sequence
// execution (section 3.1.1.4): each sequence copies literals, then a match from earlier output,
// with the three repeat offsets carried from block to block.

package zstd

import (
	"errors"
	"fmt"
)

// decodeSequences reads the sequences section and executes it, appending the block's output.
// room is how many bytes the block may produce; the caller has reserved them.
func (d *decoder) decodeSequences(blk []byte, lits []byte, room int) error {
	if len(blk) == 0 {
		return errors.New("zstd: sequences section is missing")
	}
	var nseq, p int
	switch b0 := int(blk[0]); {
	case b0 < 128:
		nseq, p = b0, 1
	case b0 < 255:
		if len(blk) < 2 {
			return errTruncatedSequences
		}
		nseq, p = (b0-128)<<8+int(blk[1]), 2
	default:
		if len(blk) < 3 {
			return errTruncatedSequences
		}
		nseq, p = (int(blk[1])|int(blk[2])<<8)+0x7f00, 3
	}
	if nseq == 0 {
		// "Decompressed content is defined entirely as Literals_Section content."
		if p != len(blk) {
			return fmt.Errorf("zstd: %d stray bytes after an empty sequences section", len(blk)-p)
		}
		if len(lits) > room {
			return d.tooLarge(len(lits))
		}
		d.out = append(d.out, lits...)
		if d.stats != nil {
			d.stats.emptySequences++
		}
		return nil
	}
	if p >= len(blk) {
		return errTruncatedSequences
	}
	modes := blk[p]
	p++
	if modes&3 != 0 {
		return errors.New("zstd: reserved bits set in the sequence compression modes")
	}
	for k := kindLL; k <= kindML; k++ {
		mode := modes >> (6 - 2*k) & 3
		n, err := d.setupTable(k, mode, blk[p:])
		if err != nil {
			return err
		}
		p += n
		if d.stats != nil {
			d.stats.modes[k][mode]++
		}
	}
	if d.stats != nil {
		d.stats.sequences += nseq
	}
	return d.execute(nseq, blk[p:], lits, room)
}

var errTruncatedSequences = errors.New("zstd: sequences section header is truncated")

// setupTable prepares the decoding table for one field per its compression mode: predefined,
// RLE, FSE-compressed, or repeated from the previous block (section 3.1.1.3.2.1).
func (d *decoder) setupTable(k seqKind, mode byte, b []byte) (int, error) {
	switch mode {
	case 0: // Predefined_Mode
		d.tables[k] = predefined[k]
		return 0, nil
	case 1: // RLE_Mode
		if len(b) == 0 {
			return 0, errTruncatedSequences
		}
		if int(b[0]) > k.maxCode() {
			return 0, fmt.Errorf("zstd: %s code %d is out of range", k, b[0])
		}
		d.own[k].rle(k, b[0])
		d.tables[k] = &d.own[k]
		return 1, nil
	case 2: // FSE_Compressed_Mode
		var norm [256]int16
		nsym, log, n, err := readFSEDescription(b, k.maxCode(), k.maxLog(), &norm)
		if err != nil {
			return 0, err
		}
		if err := d.own[k].build(k, norm[:nsym], log); err != nil {
			return 0, err
		}
		d.tables[k] = &d.own[k]
		return n, nil
	}
	// Repeat_Mode
	if d.tables[k] == nil {
		return 0, fmt.Errorf("zstd: repeat mode for %s without a previous table", k)
	}
	return 0, nil
}

// readBits takes k bits from a local container (v, n).
func readBits(v uint64, n uint, k uint) (uint64, uint, uint64) {
	return v << k, n - k, v >> (64 - k) // a shift by 64 yields 0, so k == 0 reads nothing
}

// execute decodes nseq sequences from the backward bitstream b and carries them out. Literals
// and short matches are copied 16 bytes at a time where the buffers have room past the end
// (wildSlack), which the bounds of each copy are checked against.
func (d *decoder) execute(nseq int, stream []byte, lits []byte, room int) error {
	br, err := newRevBits(stream)
	if err != nil {
		return err
	}
	llt, oft, mlt := d.tables[kindLL], d.tables[kindOF], d.tables[kindML]
	b, v, nb, f := br.b, br.bits, br.nbits, br.off
	// Initial states: literal lengths, offsets, match lengths (9+8+9 bits at most, and a new
	// reader holds at least 56).
	var x uint64
	v, nb, x = readBits(v, nb, llt.log)
	llState := uint(x)
	v, nb, x = readBits(v, nb, oft.log)
	ofState := uint(x)
	v, nb, x = readBits(v, nb, mlt.log)
	mlState := uint(x)

	n := len(d.out)
	full := d.out[:cap(d.out)]
	litFull := lits[:cap(lits)]
	blockStart := n
	lim := n + room
	frameStart := d.frameStart
	window := d.window
	rep1, rep2, rep3 := d.rep[0], d.rep[1], d.rep[2]
	st := d.stats
	litPos := 0
	for i := 0; i < nseq; i++ {
		// After a refill at least 56 bits are loaded: enough for the extra bits of the offset
		// (up to 24 here) and of both lengths (16 each); a longer offset refills again.
		if f >= 8 {
			v, nb, f = refillFast(b, v, nb, f)
		} else {
			v, nb, f = refillSlow(b, v, nb, f)
		}
		ofE := oft.entries[ofState&511]
		mlE := mlt.entries[mlState&511]
		llE := llt.entries[llState&511]
		v, nb, x = readBits(v, nb, uint(ofE.extra))
		ofv := ofE.base + uint32(x)
		if ofE.extra > 24 {
			v, nb, f = refill(b, v, nb, f)
		}
		v, nb, x = readBits(v, nb, uint(mlE.extra))
		ml := int(mlE.base) + int(x)
		v, nb, x = readBits(v, nb, uint(llE.extra))
		ll := int(llE.base) + int(x)
		if i+1 < nseq {
			// No state update follows the last sequence.
			if f >= 8 {
				v, nb, f = refillFast(b, v, nb, f)
			} else {
				v, nb, f = refillSlow(b, v, nb, f)
			}
			v, nb, x = readBits(v, nb, uint(llE.nb))
			llState = uint(llE.next) + uint(x)
			v, nb, x = readBits(v, nb, uint(mlE.nb))
			mlState = uint(mlE.next) + uint(x)
			v, nb, x = readBits(v, nb, uint(ofE.nb))
			ofState = uint(ofE.next) + uint(x)
		}

		// Offset values 1 to 3 name the repeat offsets, shifted by one when the literal
		// length is 0 (section 3.1.1.5).
		var off int
		if ofv > 3 {
			off = int(ofv - 3)
			rep3, rep2, rep1 = rep2, rep1, off
		} else {
			r := int(ofv) - 1
			if ll == 0 {
				r++
			}
			switch r {
			case 0:
				off = rep1
			case 1:
				off = rep2
				rep2, rep1 = rep1, off
			case 2:
				off = rep3
				rep3, rep2, rep1 = rep2, rep1, off
			default:
				off = rep1 - 1
				if off == 0 {
					return errors.New("zstd: repeat offset 1 minus one is zero")
				}
				rep3, rep2, rep1 = rep2, rep1, off
			}
			if st != nil {
				st.repeats[r]++
			}
		}

		if ll > len(lits)-litPos {
			return fmt.Errorf("zstd: sequences use more literals than the block's %d", len(lits))
		}
		if ll+ml > lim-n {
			return d.tooLarge(n + ll + ml - blockStart)
		}
		if ll <= 32 && litPos+32 <= len(litFull) && n+32 <= len(full) {
			*(*[16]byte)(full[n : n+16]) = *(*[16]byte)(litFull[litPos : litPos+16])
			if ll > 16 {
				*(*[16]byte)(full[n+16 : n+32]) = *(*[16]byte)(litFull[litPos+16 : litPos+32])
			}
		} else {
			copy(full[n:n+ll], lits[litPos:litPos+ll])
		}
		n += ll
		litPos += ll

		if off > n-frameStart {
			return fmt.Errorf("zstd: match offset %d reaches before the start of the frame (%d bytes in)", off, n-frameStart)
		}
		if off > window {
			return fmt.Errorf("zstd: match offset %d exceeds the frame's window size of %d", off, window)
		}
		src := n - off
		if st != nil {
			if src < blockStart {
				st.crossBlock++
			}
			if off < ml {
				st.overlaps++
			}
			st.maxOffset = max(st.maxOffset, off)
		}
		switch {
		case off >= 16 && (ml <= 64 || off < ml) && n+ml+16 <= len(full):
			// Each 16-byte chunk's source lies wholly before its destination, so this is
			// right even when the match overlaps itself.
			for k := 0; k < ml; k += 16 {
				*(*[16]byte)(full[n+k : n+k+16]) = *(*[16]byte)(full[src+k : src+k+16])
			}
		case off >= ml:
			copy(full[n:n+ml], full[src:src+ml])
		case off == 1:
			fill(full[n:n+ml], full[src])
		case off >= 8 && n+ml+8 <= len(full):
			for k := 0; k < ml; k += 8 {
				*(*[8]byte)(full[n+k : n+k+8]) = *(*[8]byte)(full[src+k : src+k+8])
			}
		case ml <= 32:
			for k := 0; k < ml; k++ {
				full[n+k] = full[src+k]
			}
		default:
			// The source overlaps the destination: copy the period, then twice as much from
			// the start each round, which keeps the pattern because each round's start is a
			// whole number of periods on.
			end := n + ml
			for p := n; p < end; {
				p += copy(full[p:end], full[src:p])
			}
		}
		n += ml
	}
	if remainingBits(nb, f) != 0 {
		return errors.New("zstd: sequences bitstream is corrupt (not consumed exactly)")
	}
	rest := len(lits) - litPos
	if rest > lim-n {
		return d.tooLarge(n + rest - blockStart)
	}
	copy(full[n:], lits[litPos:])
	n += rest
	d.out = full[:n]
	d.rep = [3]int{rep1, rep2, rep3}
	return nil
}
