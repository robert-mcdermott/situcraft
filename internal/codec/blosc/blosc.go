// Package blosc decodes Blosc (version 1) chunks, the compressor of the Zarr arrays in a Xenium
// bundle (cells.zarr.zip, transcripts.zarr.zip, cell_feature_matrix.zarr.zip) and of many other
// Zarr stores. Written from the Blosc chunk format description (c-blosc, README_CHUNK_FORMAT) and
// checked against chunks written by c-blosc 1.21 (testdata/, made by make_fixtures.py with
// numcodecs) and against every chunk of the development bundles:
//
//   - a 16-byte header: version, versionlz, flags, typesize, nbytes, blocksize, cbytes;
//   - flags: 0x01 byte shuffle, 0x02 stored without compression, 0x04 bit shuffle, 0x10 blocks
//     not split into one stream per byte of the type, bits 5-7 the codec;
//   - one int32 offset per block, counted from the start of the chunk;
//   - each block as one stream, or as typesize streams (one per byte position after shuffling),
//     each preceded by its compressed size; a stream whose size equals its decoded size is stored.
//
// Shuffling (Alted 2010, the Blosc paper's byte transposition) and bit shuffling (Masui et al.
// 2015, Astronomy and Computing 12:181-190, doi:10.1016/j.ascom.2015.07.002) are undone per block.
package blosc

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"situcraft/internal/codec/lz4"
	"situcraft/internal/codec/snappy"
	"situcraft/internal/codec/zstd"
)

const (
	headerSize = 16

	flagShuffle    = 0x01
	flagMemcpyed   = 0x02
	flagBitShuffle = 0x04
	flagDontSplit  = 0x10

	// A block is split into one stream per byte of its type only for types this small and blocks
	// holding at least this many elements per stream (c-blosc's MAX_SPLITS, MIN_BUFFERSIZE).
	maxSplits     = 16
	minBufferSize = 128
)

var codecNames = []string{"blosclz", "lz4", "snappy", "zlib", "zstd", "codec 5", "codec 6", "codec 7"}

// Header is a chunk's 16-byte header.
type Header struct {
	Version, VersionLZ, Flags, TypeSize uint8
	NBytes, BlockSize, CBytes           int
}

// Codec is the name of the codec the chunk's blocks are compressed with.
func (h Header) Codec() string { return codecNames[h.Flags>>5] }

// ReadHeader reads and checks a chunk's header.
func ReadHeader(src []byte) (Header, error) {
	if len(src) < headerSize {
		return Header{}, errors.New("blosc: chunk is shorter than its header")
	}
	h := Header{
		Version:   src[0],
		VersionLZ: src[1],
		Flags:     src[2],
		TypeSize:  src[3],
		NBytes:    int(binary.LittleEndian.Uint32(src[4:])),
		BlockSize: int(binary.LittleEndian.Uint32(src[8:])),
		CBytes:    int(binary.LittleEndian.Uint32(src[12:])),
	}
	switch {
	case h.Version == 0 || h.Version > 2:
		return h, fmt.Errorf("blosc: format version %d is not read (Blosc 1 writes versions 1 and 2)", h.Version)
	case h.TypeSize == 0:
		return h, errors.New("blosc: type size is 0")
	case h.CBytes > len(src) || h.CBytes < headerSize:
		return h, fmt.Errorf("blosc: chunk declares %d bytes but %d are present", h.CBytes, len(src))
	case h.NBytes > 0 && h.BlockSize <= 0:
		return h, errors.New("blosc: block size is 0")
	}
	return h, nil
}

// Decode decompresses one chunk. It fails when the chunk declares more than maxSize bytes
// (maxSize <= 0 means 1 << 31), before allocating them.
func Decode(src []byte, maxSize int) ([]byte, error) {
	if maxSize <= 0 {
		maxSize = 1 << 31
	}
	h, err := ReadHeader(src)
	if err != nil {
		return nil, err
	}
	if h.NBytes > maxSize {
		return nil, fmt.Errorf("blosc: chunk declares %d bytes, more than the limit of %d", h.NBytes, maxSize)
	}
	src = src[:h.CBytes]
	dst := make([]byte, h.NBytes)
	if h.Flags&flagMemcpyed != 0 {
		if len(src)-headerSize < h.NBytes {
			return nil, errors.New("blosc: stored chunk is truncated")
		}
		copy(dst, src[headerSize:])
		return dst, nil
	}
	if h.NBytes == 0 {
		return dst, nil
	}
	nblocks := (h.NBytes + h.BlockSize - 1) / h.BlockSize
	if headerSize+4*nblocks > len(src) {
		return nil, errors.New("blosc: chunk is too short for its block offsets")
	}
	// Only one block of a chunk smaller than its declared block size exists: the chunk's size, not
	// the header's block size, bounds the scratch space.
	scratch := make([]byte, min(h.BlockSize, h.NBytes))
	for b := 0; b < nblocks; b++ {
		start := int(int32(binary.LittleEndian.Uint32(src[headerSize+4*b:])))
		if start < headerSize+4*nblocks || start >= len(src) {
			return nil, fmt.Errorf("blosc: block %d starts at %d, outside the chunk", b, start)
		}
		size := h.BlockSize
		if b == nblocks-1 {
			size = h.NBytes - b*h.BlockSize
		}
		out := dst[b*h.BlockSize : b*h.BlockSize+size]
		// Byte shuffling of one-byte elements changes nothing; bit shuffling still applies. Format
		// version 2 (Blosc 1) bit-shuffles only blocks of whole groups of eight elements and
		// stores the others as they are.
		bitShuffled := h.Flags&flagBitShuffle != 0 && (h.Version > 2 || (size/int(h.TypeSize))%8 == 0)
		byteShuffled := h.Flags&flagShuffle != 0 && h.Flags&flagBitShuffle == 0 && h.TypeSize > 1
		filtered := bitShuffled || byteShuffled
		target := out
		if filtered {
			target = scratch[:size]
		}
		if err := decodeBlock(h, src, start, target, b == nblocks-1 && size < h.BlockSize); err != nil {
			return nil, fmt.Errorf("blosc: block %d: %w", b, err)
		}
		switch {
		case bitShuffled:
			bitUnshuffle(out, target, int(h.TypeSize))
		case byteShuffled:
			unshuffle(out, target, int(h.TypeSize))
		}
	}
	return dst, nil
}

// decodeBlock decodes the streams of one block into out.
func decodeBlock(h Header, src []byte, start int, out []byte, leftover bool) error {
	splits := 1
	typeSize := int(h.TypeSize)
	if h.Flags&flagDontSplit == 0 && typeSize <= maxSplits && len(out)/typeSize >= minBufferSize && !leftover {
		splits = typeSize
	}
	streamSize := len(out) / splits
	p := start
	for s := 0; s < splits; s++ {
		if p+4 > len(src) {
			return errors.New("truncated stream size")
		}
		csize := int(int32(binary.LittleEndian.Uint32(src[p:])))
		p += 4
		if csize < 0 || csize > len(src)-p {
			return fmt.Errorf("stream %d declares %d bytes, beyond the chunk", s, csize)
		}
		stream := src[p : p+csize]
		target := out[s*streamSize : (s+1)*streamSize]
		if s == splits-1 {
			target = out[s*streamSize:]
		}
		p += csize
		if csize == len(target) {
			// Stored: compressing would not have made it smaller.
			copy(target, stream)
			continue
		}
		if err := decompress(h, stream, target); err != nil {
			return fmt.Errorf("stream %d: %w", s, err)
		}
	}
	return nil
}

func decompress(h Header, stream, target []byte) error {
	var n int
	switch h.Codec() {
	case "lz4":
		var err error
		if n, err = lz4.DecodeBlock(target, stream); err != nil {
			return err
		}
	case "snappy":
		out, err := snappy.Decode(stream, len(target))
		if err != nil {
			return err
		}
		n = len(out)
		copy(target, out)
	case "zlib":
		reader, err := zlib.NewReader(bytes.NewReader(stream))
		if err != nil {
			return err
		}
		if n, err = io.ReadFull(reader, target); err != nil {
			return fmt.Errorf("zlib: %w", err)
		}
		if extra, _ := reader.Read(make([]byte, 1)); extra > 0 {
			n++
		}
	case "zstd":
		// Decoded straight into target: its capacity is the limit.
		out, err := zstd.Decode(target[:0:len(target)], stream, len(target))
		if err != nil {
			return err
		}
		n = len(out)
		if n > 0 && &out[0] != &target[0] {
			copy(target, out)
		}
	default:
		return fmt.Errorf("%s-compressed chunks are not read", h.Codec())
	}
	if n != len(target) {
		return fmt.Errorf("decodes to %d bytes, not the %d expected", n, len(target))
	}
	return nil
}

// unshuffle undoes Blosc's byte shuffle: the block holds byte 0 of every element, then byte 1 of
// every element, and so on; bytes past the last whole element are stored as they were.
func unshuffle(dst, src []byte, typeSize int) {
	n := len(src) / typeSize
	for j := 0; j < typeSize; j++ {
		plane := src[j*n : (j+1)*n]
		for i, b := range plane {
			dst[i*typeSize+j] = b
		}
	}
	copy(dst[n*typeSize:], src[n*typeSize:])
}

// bitUnshuffle undoes bit shuffling: elements are taken in groups of eight; the block holds, for
// each bit of each byte of the type, that bit of every element packed eight to a byte (least
// significant bit first). Elements past the last group of eight, and bytes past the last whole
// element, are stored as they were.
func bitUnshuffle(dst, src []byte, typeSize int) {
	n := len(src) / typeSize
	n8 := n - n%8
	rowBytes := n8 / 8
	for i := range dst[:n8*typeSize] {
		dst[i] = 0
	}
	for j := 0; j < typeSize; j++ {
		for k := 0; k < 8; k++ {
			row := src[(j*8+k)*rowBytes : (j*8+k+1)*rowBytes]
			for i := 0; i < n8; i++ {
				bit := row[i>>3] >> (i & 7) & 1
				dst[i*typeSize+j] |= bit << k
			}
		}
	}
	copy(dst[n8*typeSize:], src[n8*typeSize:])
}
