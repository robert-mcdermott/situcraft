// Package lz4 decodes the LZ4 block format, which Blosc uses for most of the arrays in a Xenium
// bundle's Zarr stores. Written from the LZ4 Block Format Description (lz4 project, version 1.6.1):
// a block is a series of sequences, each a token (literal length in the high nibble, match length
// minus 4 in the low one, 15 meaning "more bytes follow"), the literals, and a two-byte
// little-endian offset back into the output; the last sequence has literals only.
package lz4

import (
	"errors"
	"fmt"
)

// minMatch is the shortest match the format can express (the token stores length - 4).
const minMatch = 4

var errTruncated = errors.New("lz4: block is truncated")

// DecodeBlock decodes one LZ4 block from src into dst and returns the number of bytes written. A
// block that would write past the end of dst, or reach back before its start, is an error; the
// caller checks the count against the size its container records.
func DecodeBlock(dst, src []byte) (int, error) {
	d, s := 0, 0
	for {
		if s >= len(src) {
			return 0, errTruncated
		}
		token := src[s]
		s++
		// Literals.
		literals := int(token >> 4)
		if literals == 15 {
			n, used, err := extendedLength(src[s:])
			if err != nil {
				return 0, err
			}
			literals += n
			s += used
		}
		if literals > len(src)-s {
			return 0, errTruncated
		}
		if literals > len(dst)-d {
			return 0, fmt.Errorf("lz4: block decodes to more than the %d bytes expected", len(dst))
		}
		copy(dst[d:], src[s:s+literals])
		d += literals
		s += literals
		// The last sequence ends with its literals.
		if s == len(src) {
			return d, nil
		}
		// Match.
		if len(src)-s < 2 {
			return 0, errTruncated
		}
		offset := int(src[s]) | int(src[s+1])<<8
		s += 2
		if offset == 0 || offset > d {
			return 0, fmt.Errorf("lz4: match offset %d reaches before the start of the block (at byte %d)", offset, d)
		}
		length := int(token&15) + minMatch
		if token&15 == 15 {
			n, used, err := extendedLength(src[s:])
			if err != nil {
				return 0, err
			}
			length += n
			s += used
		}
		if length > len(dst)-d {
			return 0, fmt.Errorf("lz4: block decodes to more than the %d bytes expected", len(dst))
		}
		from := d - offset
		if offset >= length {
			copy(dst[d:d+length], dst[from:from+length])
		} else {
			// Overlapping match: the copy repeats the last offset bytes, so it goes forward in
			// pieces no longer than the distance (each piece doubles what is available).
			for copied := 0; copied < length; {
				n := copy(dst[d+copied:d+length], dst[from:d+copied])
				copied += n
			}
		}
		d += length
	}
}

// extendedLength reads the bytes that extend a length nibble of 15: each adds its value, and a
// byte of 255 means another follows.
func extendedLength(src []byte) (n, used int, err error) {
	for used < len(src) {
		b := src[used]
		used++
		n += int(b)
		if n > 1<<31 {
			return 0, 0, errors.New("lz4: length is implausibly large")
		}
		if b != 255 {
			return n, used, nil
		}
	}
	return 0, 0, errTruncated
}
