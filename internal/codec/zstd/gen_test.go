// Deterministic synthetic content for the tests and the benchmark: generated text, float32
// arrays shaped like transcript tables, random bytes, runs and repeats. Every generator takes a
// seed, so a fixture's original content is regenerated here instead of being committed.

package zstd

import (
	"encoding/binary"
	"math"
	"strconv"
)

// rng is SplitMix64: tiny, and fixed forever, unlike a library generator whose streams may
// change between Go releases.
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

func (r *rng) float() float64 { return float64(r.next()>>11) / (1 << 53) }

// genText makes English-like prose from a made-up vocabulary with a skewed word frequency, with
// numbers and line breaks, so it compresses roughly as real text does.
func genText(seed uint64, n int) []byte {
	r := rng{seed}
	syll := []string{"ka", "lo", "mi", "ne", "su", "ta", "ri", "on", "el", "ra", "ve", "qu", "is", "an", "or", "th", "st", "ce", "ll", "ex"}
	vocab := make([]string, 1500)
	for i := range vocab {
		w := ""
		for k := 1 + r.intn(4); k > 0; k-- {
			w += syll[r.intn(len(syll))]
		}
		vocab[i] = w
	}
	out := make([]byte, 0, n+64)
	col := 0
	start := true
	for len(out) < n {
		u := r.float()
		w := vocab[int(float64(len(vocab))*u*u*u)]
		switch {
		case r.intn(40) == 0:
			w = strconv.Itoa(r.intn(100000))
		case start:
			w = string(w[0]-'a'+'A') + w[1:]
		}
		out = append(out, w...)
		col += len(w) + 1
		start = false
		switch {
		case r.intn(12) == 0:
			out = append(out, '.')
			start = true
		case r.intn(15) == 0:
			out = append(out, ',')
		}
		if col > 72 {
			out = append(out, '\n')
			col = 0
			if r.intn(8) == 0 {
				out = append(out, '\n')
			}
		} else {
			out = append(out, ' ')
		}
	}
	return out[:n]
}

// genFloats makes little-endian float32 columns like a transcript table's: sorted coordinates
// in microns, a small-range z, quality scores from a few values, and a noisy wave. Each value is
// one correctly rounded operation on integers, so it is the same on every architecture (a
// multiply-add may be fused on some and not on others, and math.Sin is not specified to the bit).
func genFloats(seed uint64, n int) []byte {
	r := rng{seed}
	out := make([]byte, 0, n+4)
	var x int64
	for len(out) < n {
		var v float32
		switch col := (len(out) / 4096) % 4; col {
		case 0:
			x += int64(r.intn(820))
			v = float32(x) / 1024
		case 1:
			v = float32(10<<20+r.intn(20<<20)) / (1 << 20)
		case 2:
			v = [4]float32{20, 24.5, 30, 40}[r.intn(4)]
		default:
			tri := int64(len(out)/4) % 1800
			if tri > 900 {
				tri = 1800 - tri
			}
			v = float32(tri*1000+int64(r.intn(1000))) / 900000
		}
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	return out[:n]
}

func genRandom(seed uint64, n int) []byte {
	r := rng{seed}
	out := make([]byte, n+8)
	for i := 0; i < n; i += 8 {
		binary.LittleEndian.PutUint64(out[i:], r.next())
	}
	return out[:n]
}

// genRuns makes runs of one byte, and short repeating patterns, of random lengths: overlapping
// matches with small offsets.
func genRuns(seed uint64, n int) []byte {
	r := rng{seed}
	out := make([]byte, 0, n+1024)
	for len(out) < n {
		period := 1 + r.intn(9)
		length := 1 + r.intn(600)
		pat := make([]byte, period)
		for i := range pat {
			pat[i] = byte('a' + r.intn(6))
		}
		for i := 0; i < length; i++ {
			out = append(out, pat[i%period])
		}
	}
	return out[:n]
}

// genRepeats copies earlier stretches, near and far, with small edits between them, so that
// sequences use the repeat offsets and reach back across blocks.
func genRepeats(seed uint64, n int) []byte {
	r := rng{seed}
	out := make([]byte, 0, n+4096)
	out = append(out, genRandom(seed^1, 512)...)
	for len(out) < n {
		switch r.intn(4) {
		case 0:
			for k := r.intn(20); k >= 0; k-- {
				out = append(out, byte(r.next()))
			}
		default:
			dist := 1 + r.intn(len(out))
			if r.intn(3) == 0 && len(out) > 64 {
				dist = 1 + r.intn(64)
			}
			length := 3 + r.intn(300)
			from := len(out) - dist
			for i := 0; i < length; i++ {
				out = append(out, out[from+i])
			}
		}
	}
	return out[:n]
}

// genMixed concatenates stretches of every kind.
func genMixed(seed uint64, n int) []byte {
	r := rng{seed}
	gens := []func(uint64, int) []byte{genText, genFloats, genRandom, genRuns, genRepeats}
	out := make([]byte, 0, n)
	for len(out) < n {
		k := 1 + r.intn(64<<10)
		out = append(out, gens[r.intn(len(gens))](r.next(), k)...)
	}
	return out[:n]
}

// genLong is a random stretch, a long filler of near-copies of one 4 KiB block (cheap to
// compress), then the random stretch again: the repeat is beyond the default window and only
// matched with a long window.
func genLong(seed uint64, n int) []byte {
	head := genRandom(seed, 32<<10)
	block := genText(seed+1, 4096)
	out := make([]byte, 0, n)
	out = append(out, head...)
	r := rng{seed + 2}
	for len(out) < n-len(head) {
		b := append([]byte(nil), block...)
		for k := 0; k < 4; k++ {
			b[r.intn(len(b))] = byte(r.next())
		}
		out = append(out, b...)
	}
	out = append(out, head...)
	return out
}

// realistic is the benchmark's content: mostly float32 columns, with text.
func realistic(seed uint64, n int) []byte {
	r := rng{seed}
	out := make([]byte, 0, n)
	for len(out) < n {
		k := 256<<10 + r.intn(768<<10)
		if r.intn(3) == 0 {
			out = append(out, genText(r.next(), k)...)
		} else {
			out = append(out, genFloats(r.next(), k)...)
		}
	}
	return out[:n]
}
