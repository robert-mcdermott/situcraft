// Package snappy decodes Snappy's raw block format, the default page compression of many Parquet
// writers (Xenium's own Parquet files use Zstd; Parquet files from other tools often use Snappy).
// Written from the Snappy format description (google/snappy, format_description.txt): a varint of
// the decoded length, then elements whose tag's low two bits say literal, or copy with a 1-, 2- or
// 4-byte offset.
package snappy

import (
	"errors"
	"fmt"
)

var errCorrupt = errors.New("snappy: corrupt input")

// DecodedLen is the decoded length a block declares.
func DecodedLen(src []byte) (int, error) {
	n, _, err := uvarint(src)
	return n, err
}

// Decode decodes one Snappy block. It fails when the block declares more than maxSize bytes
// (maxSize <= 0 means 1 << 31), before allocating them.
func Decode(src []byte, maxSize int) ([]byte, error) {
	if maxSize <= 0 {
		maxSize = 1 << 31
	}
	n, s, err := uvarint(src)
	if err != nil {
		return nil, err
	}
	if n > maxSize {
		return nil, fmt.Errorf("snappy: block declares %d bytes, more than the limit of %d", n, maxSize)
	}
	dst := make([]byte, n)
	d := 0
	for s < len(src) {
		tag := src[s]
		var length, offset int
		switch tag & 3 {
		case 0: // literal
			length = int(tag>>2) + 1
			s++
			if extra := int(tag>>2) - 59; extra > 0 {
				// Lengths over 60 follow the tag in 1 to 4 little-endian bytes (length - 1).
				if len(src)-s < extra {
					return nil, errCorrupt
				}
				v := 0
				for i := 0; i < extra; i++ {
					v |= int(src[s+i]) << (8 * i)
				}
				length = v + 1
				s += extra
			}
			if length <= 0 || length > len(src)-s || length > n-d {
				return nil, errCorrupt
			}
			copy(dst[d:], src[s:s+length])
			d += length
			s += length
			continue
		case 1: // copy, 1-byte offset: length 4..11 in bits 2-4, offset's high 3 bits in 5-7
			if len(src)-s < 2 {
				return nil, errCorrupt
			}
			length = 4 + int(tag>>2)&7
			offset = int(tag&0xe0)<<3 | int(src[s+1])
			s += 2
		case 2: // copy, 2-byte offset
			if len(src)-s < 3 {
				return nil, errCorrupt
			}
			length = 1 + int(tag>>2)
			offset = int(src[s+1]) | int(src[s+2])<<8
			s += 3
		case 3: // copy, 4-byte offset
			if len(src)-s < 5 {
				return nil, errCorrupt
			}
			length = 1 + int(tag>>2)
			offset = int(src[s+1]) | int(src[s+2])<<8 | int(src[s+3])<<16 | int(src[s+4])<<24
			s += 5
		}
		if offset <= 0 || offset > d || length > n-d {
			return nil, errCorrupt
		}
		from := d - offset
		if offset >= length {
			copy(dst[d:d+length], dst[from:from+length])
		} else {
			// An overlapping copy repeats the last offset bytes; copying from the start of the
			// pattern is correct because what has been copied so far is itself periodic.
			for copied := 0; copied < length; {
				copied += copy(dst[d+copied:d+length], dst[from:d+copied])
			}
		}
		d += length
	}
	if d != n {
		return nil, fmt.Errorf("snappy: block decodes to %d bytes, not the %d it declares", d, n)
	}
	return dst, nil
}

func uvarint(src []byte) (int, int, error) {
	v := 0
	for i := 0; i < len(src) && i < 5; i++ {
		b := src[i]
		v |= int(b&0x7f) << (7 * i)
		if b < 0x80 {
			if v > 1<<32-1 {
				return 0, 0, errCorrupt
			}
			return v, i + 1, nil
		}
	}
	return 0, 0, errCorrupt
}
