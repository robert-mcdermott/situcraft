// XXH64 (Yann Collet), the hash behind a frame's Content_Checksum: its low 32 bits, with seed 0
// (RFC 8878, section 3.1.1). Written from the published XXH64 algorithm description.

package zstd

import (
	"encoding/binary"
	"math/bits"
)

const (
	prime64x1 uint64 = 0x9E3779B185EBCA87
	prime64x2 uint64 = 0xC2B2AE3D27D4EB4F
	prime64x3 uint64 = 0x165667B19E3779F9
	prime64x4 uint64 = 0x85EBCA77C2B2AE63
	prime64x5 uint64 = 0x27D4EB2F165667C5
)

func xxhRound(acc, lane uint64) uint64 {
	acc += lane * prime64x2
	return bits.RotateLeft64(acc, 31) * prime64x1
}

func xxhMerge(acc, v uint64) uint64 {
	acc ^= xxhRound(0, v)
	return acc*prime64x1 + prime64x4
}

// xxh64 hashes b with seed 0.
func xxh64(b []byte) uint64 {
	var seed uint64
	n := len(b)
	var h uint64
	if n >= 32 {
		// Four lanes, each consuming 8 bytes of every 32-byte stripe.
		v1 := seed + prime64x1 + prime64x2
		v2 := seed + prime64x2
		v3 := seed
		v4 := seed - prime64x1
		for len(b) >= 32 {
			v1 = xxhRound(v1, binary.LittleEndian.Uint64(b[0:]))
			v2 = xxhRound(v2, binary.LittleEndian.Uint64(b[8:]))
			v3 = xxhRound(v3, binary.LittleEndian.Uint64(b[16:]))
			v4 = xxhRound(v4, binary.LittleEndian.Uint64(b[24:]))
			b = b[32:]
		}
		h = bits.RotateLeft64(v1, 1) + bits.RotateLeft64(v2, 7) + bits.RotateLeft64(v3, 12) + bits.RotateLeft64(v4, 18)
		h = xxhMerge(h, v1)
		h = xxhMerge(h, v2)
		h = xxhMerge(h, v3)
		h = xxhMerge(h, v4)
	} else {
		h = seed + prime64x5
	}
	h += uint64(n)
	for ; len(b) >= 8; b = b[8:] {
		h ^= xxhRound(0, binary.LittleEndian.Uint64(b))
		h = bits.RotateLeft64(h, 27)*prime64x1 + prime64x4
	}
	if len(b) >= 4 {
		h ^= uint64(binary.LittleEndian.Uint32(b)) * prime64x1
		h = bits.RotateLeft64(h, 23)*prime64x2 + prime64x3
		b = b[4:]
	}
	for _, c := range b {
		h ^= uint64(c) * prime64x5
		h = bits.RotateLeft64(h, 11) * prime64x1
	}
	// Avalanche.
	h ^= h >> 33
	h *= prime64x2
	h ^= h >> 29
	h *= prime64x3
	h ^= h >> 32
	return h
}
