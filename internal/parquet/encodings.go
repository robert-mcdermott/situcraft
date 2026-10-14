package parquet

// Value and level encodings, written from the Apache Parquet specification's Encodings document
// (parquet-format, Encodings.md): PLAIN, the RLE/bit-packing hybrid (levels and dictionary
// indices), DELTA_BINARY_PACKED, DELTA_LENGTH_BYTE_ARRAY, DELTA_BYTE_ARRAY and BYTE_STREAM_SPLIT.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
)

var errPage = errors.New("parquet: corrupt page")

// readHybrid decodes n values of the given bit width from the RLE/bit-packing hybrid into out.
func readHybrid(src []byte, bitWidth, n int, out []int32) error {
	if bitWidth < 0 || bitWidth > 32 {
		return fmt.Errorf("parquet: bit width %d", bitWidth)
	}
	if bitWidth == 0 {
		for i := range out[:n] {
			out[i] = 0
		}
		return nil
	}
	byteWidth := (bitWidth + 7) / 8
	i, p := 0, 0
	for i < n {
		header, used := binary.Uvarint(src[p:])
		if used <= 0 {
			return errPage
		}
		p += used
		if header&1 == 0 {
			// A run of one value repeated count times.
			count := int(header >> 1)
			if p+byteWidth > len(src) || count < 0 {
				return errPage
			}
			var v uint32
			for b := 0; b < byteWidth; b++ {
				v |= uint32(src[p+b]) << (8 * b)
			}
			p += byteWidth
			if count > n-i {
				count = n - i
			}
			for k := 0; k < count; k++ {
				out[i+k] = int32(v)
			}
			i += count
			continue
		}
		// Groups of eight bit-packed values, least significant bit first.
		if header>>1 > uint64(len(src)) {
			return errPage
		}
		groups := int(header >> 1)
		if groups <= 0 {
			return errPage
		}
		size := groups * bitWidth
		if p+size > len(src) {
			// The last group may be cut short when the page ends; take what is there.
			size = len(src) - p
		}
		count := groups * 8
		if count > n-i {
			count = n - i
		}
		unpack(src[p:p+size], bitWidth, out[i:i+count])
		p += size
		i += count
	}
	return nil
}

// unpack reads len(out) values of width bits from packed (least significant bit first).
func unpack(packed []byte, width int, out []int32) {
	mask := uint64(1)<<width - 1
	var acc uint64
	have := 0
	p := 0
	for i := range out {
		for have < width {
			var b byte
			if p < len(packed) {
				b = packed[p]
			}
			p++
			acc |= uint64(b) << have
			have += 8
		}
		out[i] = int32(acc & mask)
		acc >>= width
		have -= width
	}
}

// plain decodes n PLAIN values of the column's type from src into v at positions where (when
// positions is non-nil) the values go; it returns the bytes used.
func plain(col *Column, src []byte, n int, v *Values, positions []int) (int, error) {
	at := func(k int) int {
		if positions == nil {
			return k
		}
		return positions[k]
	}
	switch col.Type {
	case Boolean:
		if (n+7)/8 > len(src) {
			return 0, errPage
		}
		for k := 0; k < n; k++ {
			v.Bool[at(k)] = src[k>>3]>>(k&7)&1 == 1
		}
		return (n + 7) / 8, nil
	case Int32, Float:
		if 4*n > len(src) {
			return 0, errPage
		}
		for k := 0; k < n; k++ {
			u := binary.LittleEndian.Uint32(src[4*k:])
			if col.Type == Int32 {
				v.Int32[at(k)] = int32(u)
			} else {
				v.Float32[at(k)] = math.Float32frombits(u)
			}
		}
		return 4 * n, nil
	case Int64, Double:
		if 8*n > len(src) {
			return 0, errPage
		}
		for k := 0; k < n; k++ {
			u := binary.LittleEndian.Uint64(src[8*k:])
			if col.Type == Int64 {
				v.Int64[at(k)] = int64(u)
			} else {
				v.Float64[at(k)] = math.Float64frombits(u)
			}
		}
		return 8 * n, nil
	case Int96, FixedLenByteArray:
		width := 12
		if col.Type == FixedLenByteArray {
			width = col.TypeLength
		}
		if width <= 0 || width*n > len(src) {
			return 0, errPage
		}
		for k := 0; k < n; k++ {
			v.Bytes[at(k)] = src[width*k : width*(k+1) : width*(k+1)]
		}
		return width * n, nil
	case ByteArray:
		p := 0
		for k := 0; k < n; k++ {
			if p+4 > len(src) {
				return 0, errPage
			}
			length := int(binary.LittleEndian.Uint32(src[p:]))
			p += 4
			if length < 0 || length > len(src)-p {
				return 0, errPage
			}
			v.Bytes[at(k)] = src[p : p+length : p+length]
			p += length
		}
		return p, nil
	}
	return 0, fmt.Errorf("parquet: type %v is not read", col.Type)
}

// deltaBinaryPacked decodes n integers of DELTA_BINARY_PACKED; it returns them and the bytes used.
func deltaBinaryPacked(src []byte, n int) ([]int64, int, error) {
	p := 0
	uvar := func() (uint64, error) {
		v, used := binary.Uvarint(src[p:])
		if used <= 0 {
			return 0, errPage
		}
		p += used
		return v, nil
	}
	blockSize, err := uvar()
	if err != nil {
		return nil, 0, err
	}
	miniblocks, err := uvar()
	if err != nil {
		return nil, 0, err
	}
	total, err := uvar()
	if err != nil {
		return nil, 0, err
	}
	firstZig, err := uvar()
	if err != nil {
		return nil, 0, err
	}
	if blockSize == 0 || blockSize%128 != 0 || miniblocks == 0 || blockSize%miniblocks != 0 || (blockSize/miniblocks)%32 != 0 || blockSize > 1<<20 {
		return nil, 0, errPage
	}
	// The header counts the values the page holds; anything else is corrupt (and a large count
	// with zero-width miniblocks would otherwise loop without reading a byte).
	if total != uint64(n) {
		return nil, 0, fmt.Errorf("parquet: delta encoding holds %d values, not the %d expected", total, n)
	}
	perMini := int(blockSize / miniblocks)
	out := make([]int64, 0, n)
	last := int64(firstZig>>1) ^ -int64(firstZig&1)
	if n > 0 {
		out = append(out, last)
	}
	scratch := make([]int32, perMini)
	for uint64(len(out)) < total {
		minZig, err := uvar()
		if err != nil {
			return nil, 0, err
		}
		minDelta := int64(minZig>>1) ^ -int64(minZig&1)
		if p+int(miniblocks) > len(src) {
			return nil, 0, errPage
		}
		widths := src[p : p+int(miniblocks)]
		p += int(miniblocks)
		for m := 0; m < int(miniblocks) && uint64(len(out)) < total; m++ {
			w := int(widths[m])
			if w > 64 {
				return nil, 0, errPage
			}
			size := perMini * w / 8
			if p+size > len(src) {
				return nil, 0, errPage
			}
			if w <= 32 {
				unpack(src[p:p+size], w, scratch)
				for k := 0; k < perMini && uint64(len(out)) < total; k++ {
					last += minDelta + int64(uint32(scratch[k]))
					out = append(out, last)
				}
			} else {
				unpack64(src[p:p+size], w, perMini, func(k int, d uint64) {
					if uint64(len(out)) < total {
						last += minDelta + int64(d)
						out = append(out, last)
					}
				})
			}
			p += size
		}
	}
	return out[:n], p, nil
}

// unpack64 reads count values wider than 32 bits.
func unpack64(packed []byte, width, count int, fn func(int, uint64)) {
	bit := 0
	for k := 0; k < count; k++ {
		var v uint64
		for b := 0; b < width; b++ {
			byteIndex := (bit + b) >> 3
			if packed[byteIndex]>>((bit+b)&7)&1 == 1 {
				v |= 1 << b
			}
		}
		bit += width
		fn(k, v)
	}
}

// deltaLengthByteArray decodes n byte arrays of DELTA_LENGTH_BYTE_ARRAY.
func deltaLengthByteArray(src []byte, n int) ([][]byte, int, error) {
	lengths, p, err := deltaBinaryPacked(src, n)
	if err != nil {
		return nil, 0, err
	}
	out := make([][]byte, n)
	for k, length := range lengths {
		if length < 0 || length > int64(len(src)-p) {
			return nil, 0, errPage
		}
		out[k] = src[p : p+int(length) : p+int(length)]
		p += int(length)
	}
	return out, p, nil
}

// deltaByteArray decodes n byte arrays of DELTA_BYTE_ARRAY (incremental: each a prefix of the
// previous one and a suffix).
func deltaByteArray(src []byte, n int) ([][]byte, int, error) {
	prefixes, p, err := deltaBinaryPacked(src, n)
	if err != nil {
		return nil, 0, err
	}
	suffixes, used, err := deltaLengthByteArray(src[p:], n)
	if err != nil {
		return nil, 0, err
	}
	out := make([][]byte, n)
	var previous []byte
	for k := range out {
		prefix := prefixes[k]
		if prefix < 0 || prefix > int64(len(previous)) {
			return nil, 0, errPage
		}
		value := make([]byte, 0, int(prefix)+len(suffixes[k]))
		value = append(value, previous[:prefix]...)
		value = append(value, suffixes[k]...)
		out[k] = value
		previous = value
	}
	return out, p + used, nil
}

// byteStreamSplit decodes n values of width bytes stored as width streams (byte 0 of every value,
// then byte 1, …).
func byteStreamSplit(src []byte, n, width int) ([]byte, error) {
	if width <= 0 || n*width > len(src) {
		return nil, errPage
	}
	out := make([]byte, n*width)
	for b := 0; b < width; b++ {
		stream := src[b*n : (b+1)*n]
		for k, v := range stream {
			out[k*width+b] = v
		}
	}
	return out, nil
}

// bitWidth is the number of bits needed for values up to max.
func bitWidth(max int) int { return bits.Len(uint(max)) }
