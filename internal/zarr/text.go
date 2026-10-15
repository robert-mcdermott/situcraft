package zarr

// Format 3's sharding (the sharding_indexed codec) and text of varying length (numcodecs'
// VLenUTF8: format 2's object dtype with the vlen-utf8 filter, format 3's string data type with
// the vlen-utf8 codec), as AnnData 0.11 and later and SpatialData write them.
//
// A shard holds its inner chunks, each encoded on its own, and an index of 16 bytes for each (its
// offset and length, little-endian; both 2⁶⁴−1 for a chunk never written), at the shard's end
// unless the configuration says start, followed by its CRC-32C when the index codecs say so.
// VLenUTF8 text is the number of items (uint32), then each item's length (uint32) and bytes.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"unicode/utf8"
)

// maxTextChunk bounds a chunk of text once decoded.
const maxTextChunk = 1 << 28

// shardIndex reads a shard's index: each inner chunk's stored bytes, or nil when never written.
func (a *Array) shardIndex(stored []byte) ([][]byte, error) {
	sh := a.shard
	count := 1
	for i, c := range a.Chunks {
		count *= c / sh.inner[i]
	}
	size := 16 * count
	if sh.indexCRC {
		size += 4
	}
	if len(stored) < size {
		return nil, fmt.Errorf("a shard of %d bytes is shorter than its index (%d)", len(stored), size)
	}
	index := stored[len(stored)-size:]
	if sh.indexAtStart {
		index = stored[:size]
	}
	if sh.indexCRC {
		body, sum := index[:size-4], binary.LittleEndian.Uint32(index[size-4:])
		if crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)) != sum {
			return nil, errors.New("the shard index's CRC-32C does not match")
		}
	}
	out := make([][]byte, count)
	for i := range out {
		offset, n := binary.LittleEndian.Uint64(index[16*i:]), binary.LittleEndian.Uint64(index[16*i+8:])
		if offset == math.MaxUint64 && n == math.MaxUint64 {
			continue
		}
		if offset > uint64(len(stored)) || n > uint64(len(stored))-offset {
			return nil, fmt.Errorf("inner chunk %d lies outside its shard", i)
		}
		out[i] = stored[offset : offset+n]
	}
	return out, nil
}

// decodeShard decodes a shard into n bytes: every element of the shard's shape, in C order.
func (a *Array) decodeShard(stored []byte, n int) ([]byte, error) {
	inner, err := a.shardIndex(stored)
	if err != nil {
		return nil, err
	}
	sh := a.shard
	item := a.DType.Size
	innerN := item
	for _, c := range sh.inner {
		innerN *= c
	}
	out := make([]byte, n)
	rank := len(a.Chunks)
	grid := make([]int, rank)
	for i := range grid {
		grid[i] = a.Chunks[i] / sh.inner[i]
	}
	shardStrides, innerStrides := cStrides(a.Chunks), cStrides(sh.inner)
	idx := make([]int, rank)
	fill := make([]byte, innerN)
	for i := 0; i < innerN; i += item {
		copy(fill[i:], a.Fill)
	}
	for k := 0; ; k++ {
		data := fill
		if inner[k] != nil {
			if data, err = decodeV3(sh.codecs, inner[k], innerN); err != nil {
				return nil, fmt.Errorf("inner chunk %v: %w", idx, err)
			}
			if len(data) != innerN {
				return nil, fmt.Errorf("inner chunk %v decodes to %d bytes, not %d", idx, len(data), innerN)
			}
		}
		// Place it, one run along the last dimension at a time.
		local := make([]int, rank)
		for {
			src, dst := 0, 0
			for d := 0; d < rank; d++ {
				src += local[d] * innerStrides[d]
				dst += (idx[d]*sh.inner[d] + local[d]) * shardStrides[d]
			}
			run := sh.inner[rank-1] * item
			copy(out[dst*item:dst*item+run], data[src*item:src*item+run])
			if !advance(local[:rank-1], sh.inner[:rank-1]) {
				break
			}
		}
		if !advance(idx, grid) {
			break
		}
	}
	return out, nil
}

// ReadStrings reads a one-dimensional array of text: of varying length (VLenUTF8), or numpy's
// fixed 'U' (UTF-32) and 'S' (bytes) types, trailing NULs removed. A zero-dimensional array is
// read as its one value.
func (a *Array) ReadStrings() ([]string, error) {
	if len(a.Shape) == 0 && a.DType.Kind == 'O' {
		items, err := a.chunkStrings(-1)
		if err != nil {
			return nil, fmt.Errorf("zarr: %s: %w", a.Path, err)
		}
		if len(items) < 1 {
			return nil, fmt.Errorf("zarr: %s holds no value", a.Path)
		}
		return items[:1], nil
	}
	if len(a.Shape) != 1 {
		return nil, fmt.Errorf("zarr: %s has %d dimensions, not one", a.Path, len(a.Shape))
	}
	n := a.Shape[0]
	switch a.DType.Kind {
	case 'U', 'S':
		raw, err := a.ReadAll()
		if err != nil {
			return nil, err
		}
		out := make([]string, n)
		size := a.DType.Size
		for i := range out {
			v := raw[i*size : (i+1)*size]
			if a.DType.Kind == 'S' {
				out[i] = string(trimNUL(v))
				continue
			}
			runes := make([]rune, 0, size/4)
			for j := 0; j+4 <= len(v); j += 4 {
				r := rune(binary.LittleEndian.Uint32(v[j:]))
				if r == 0 {
					break
				}
				runes = append(runes, r)
			}
			out[i] = string(runes)
		}
		return out, nil
	case 'O':
	default:
		return nil, fmt.Errorf("zarr: %s holds %s, not text", a.Path, a.DType)
	}
	out := make([]string, 0, n)
	for c := 0; c*a.Chunks[0] < n; c++ {
		items, err := a.chunkStrings(c)
		if err != nil {
			return nil, fmt.Errorf("zarr: %s chunk %d: %w", a.Path, c, err)
		}
		take := min(a.Chunks[0], n-c*a.Chunks[0])
		if len(items) < take {
			return nil, fmt.Errorf("zarr: %s chunk %d holds %d items, not %d", a.Path, c, len(items), take)
		}
		out = append(out, items[:take]...)
	}
	return out, nil
}

func trimNUL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

// chunkStrings decodes chunk c of a text array (a chunk never written is empty strings); c < 0
// is a zero-dimensional array's one chunk.
func (a *Array) chunkStrings(c int) ([]string, error) {
	idx, size := []int{c}, 1
	if c < 0 {
		idx = nil
	} else {
		size = a.Chunks[0]
	}
	stored, err := a.store.Get(a.chunkKey(idx))
	if errors.Is(err, ErrNotFound) {
		return make([]string, size), nil
	}
	if err != nil {
		return nil, err
	}
	switch {
	case a.shard != nil:
		inner, err := a.shardIndex(stored)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, chunk := range inner {
			if chunk == nil {
				out = append(out, make([]string, a.shard.inner[0])...)
				continue
			}
			data, err := decodeV3(a.shard.codecs, chunk, maxTextChunk)
			if err != nil {
				return nil, err
			}
			items, err := vlenUTF8(data)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
		return out, nil
	case a.ZarrFormat == 3:
		data, err := decodeV3(a.v3Codecs, stored, maxTextChunk)
		if err != nil {
			return nil, err
		}
		return vlenUTF8(data)
	default:
		data, err := decodeV2(a.compressor, stored, maxTextChunk)
		if err != nil {
			return nil, err
		}
		return vlenUTF8(data)
	}
}

// vlenUTF8 decodes VLenUTF8 text.
func vlenUTF8(data []byte) ([]string, error) {
	if len(data) < 4 {
		return nil, errors.New("text shorter than its header")
	}
	n := int(binary.LittleEndian.Uint32(data))
	data = data[4:]
	if n > len(data)/4 {
		return nil, fmt.Errorf("text claims %d items in %d bytes", n, len(data))
	}
	out := make([]string, n)
	for i := range out {
		if len(data) < 4 {
			return nil, errors.New("text ends in an item's length")
		}
		l := int(binary.LittleEndian.Uint32(data))
		data = data[4:]
		if l > len(data) {
			return nil, errors.New("text ends inside an item")
		}
		if !utf8.Valid(data[:l]) {
			return nil, fmt.Errorf("item %d is not UTF-8", i)
		}
		out[i] = string(data[:l])
		data = data[l:]
	}
	return out, nil
}
