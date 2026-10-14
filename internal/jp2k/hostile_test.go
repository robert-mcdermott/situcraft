// Hostile input: truncated, corrupted and absurd codestreams must give errors, never panics or
// unbounded allocation.

package jp2k

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tryDecode runs both entry points on data and reports a panic as a test failure. It returns the
// error of Decode.
func tryDecode(t *testing.T, label string, data []byte, opts Options) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: panic: %v", label, r)
			err = fmt.Errorf("panic")
		}
	}()
	_, err = Decode(data, opts)
	_, _, _, _ = DecodeGray16(data, opts)
	return err
}

func hostileBase(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestHostileHeaders checks crafted headers with absurd or inconsistent values.
func TestHostileHeaders(t *testing.T) {
	base := hostileBase(t, "gray8-lossless.j2k") // 64×48, one tile
	put32 := func(cs []byte, off int, v uint32) []byte {
		cs = append([]byte(nil), cs...)
		binary.BigEndian.PutUint32(cs[off:], v)
		return cs
	}
	put16 := func(cs []byte, off int, v uint16) []byte {
		cs = append([]byte(nil), cs...)
		binary.BigEndian.PutUint16(cs[off:], v)
		return cs
	}
	put8 := func(cs []byte, off int, v byte) []byte {
		cs = append([]byte(nil), cs...)
		cs[off] = v
		return cs
	}
	cod := mainSegment(base, mCOD) + 4
	qcd := mainSegment(base, mQCD)
	sot := mainSegment(base, mSOT)
	for pos := 2; sot < 0 && pos+4 <= len(base); pos += 2 + u16(base[pos+2:]) {
		if u16(base[pos:]) == mSOT {
			sot = pos
		}
	}
	cases := []struct {
		name, want string
		data       []byte
	}{
		{"empty", "neither", nil},
		{"one byte", "neither", []byte{0xFF}},
		{"SOC only", "ends", []byte{0xFF, 0x4F}},
		{"JP2 signature only", "no contiguous codestream", append([]byte(nil), jp2Signature...)},
		{"JP2 box too long", "declares", append(append([]byte(nil), jp2Signature...), 0, 0, 1, 0, 'j', 'p', '2', 'c')},
		{"JP2 XLBox huge", "declares", append(append([]byte(nil), jp2Signature...), 0, 0, 0, 1, 'j', 'p', '2', 'c', 0xFF, 0, 0, 0, 0, 0, 0, 0)},
		{"JP2 truncated box header", "truncated", append(append([]byte(nil), jp2Signature...), 0, 0)},
		{"no SIZ", "SIZ marker must follow SOC", []byte{0xFF, 0x4F, 0xFF, 0x52, 0, 2}},
		{"huge image", "exceeds the limit", put32(put32(put32(put32(base, 8, 0xFFFFFFF0), 12, 0xFFFFFFF0), 24, 0xFFFFFFF0), 28, 0xFFFFFFF0)},
		{"empty image", "empty image area", put32(base, 16, 64)},
		{"zero tile size", "zero tile size", put32(base, 24, 0)},
		{"too many tiles", "at most 65535", put32(put32(put32(put32(base, 8, 1000), 12, 1000), 24, 1), 28, 1)},
		{"tile origin past image origin", "does not cover", put32(base, 32, 5)},
		{"zero components", "components", put16(base, 40, 0)},
		{"39-bit samples", "38", put8(base, 42, 38)},
		{"32-bit samples", "at most 31", put8(base, 42, 31)},
		{"zero subsampling", "zero subsampling", put8(base, 43, 0)},
		{"33 levels", "decomposition levels", put8(base, cod+5, 33)},
		{"code-block 2^11", "code-block size", put8(base, cod+6, 9)},
		{"code-block area", "code-block size", put8(put8(base, cod+6, 6), cod+7, 6)},
		{"progression 5", "progression order", put8(base, cod+1, 5)},
		{"zero layers", "zero quality layers", put16(base, cod+2, 0)},
		{"unknown style", "unknown style", put8(base, cod, 0x40)},
		{"quantization style 5", "quantization style", put8(base, qcd+4, 5)},
		{"bad SOT length", "SOT", put16(base, sot+2, 9)},
		{"tile index", "tile 7", put16(base, sot+4, 7)},
		{"Psot past end", "truncated", put32(base, sot+6, 1<<30)},
		{"Psot inside header", "less than its header", put32(base, sot+6, 4)},
		{"tile-part number", "tile-part 3", put8(base, sot+10, 3)},
		{"marker length past end", "declares", put16(base, qcd+2, 0xFFFF)},
		{"marker length 1", "declares", put16(base, qcd+2, 1)},
		{"not a marker", "expected a marker", put8(base, qcd, 0x12)},
		{"reduce too far", "exceeds the 5 decomposition levels", nil},
	}
	for _, c := range cases {
		opts := Options{}
		data := c.data
		if c.name == "reduce too far" {
			data, opts.Reduce = base, 6
		}
		err := tryDecode(t, c.name, data, opts)
		if err == nil {
			t.Errorf("%s: decoded without error", c.name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q, want one containing %q", c.name, err, c.want)
		}
	}
	if err := tryDecode(t, "negative reduce", base, Options{Reduce: -1}); err == nil {
		t.Error("negative Reduce accepted")
	}
}

// minimalCS builds the headers of a codestream with no tile-parts: SOC, SIZ, COD, QCD, EOC.
func minimalCS(w, h, tw, th, ncomp int) []byte {
	be := binary.BigEndian
	cs := []byte{0xFF, 0x4F, 0xFF, 0x51}
	cs = be.AppendUint16(cs, uint16(38+3*ncomp))
	cs = be.AppendUint16(cs, 0)
	for _, v := range []int{w, h, 0, 0, tw, th, 0, 0} {
		cs = be.AppendUint32(cs, uint32(v))
	}
	cs = be.AppendUint16(cs, uint16(ncomp))
	for range ncomp {
		cs = append(cs, 7, 1, 1)
	}
	cs = append(cs, 0xFF, 0x52, 0, 12, 0, 0, 0, 1, 0, 5, 4, 4, 0, 1)
	cs = append(cs, 0xFF, 0x5C, 0, 4, 0x40, 0x40)
	return append(cs, 0xFF, 0xD9)
}

// TestHostileSizes checks declared sizes that would cost much memory or time.
func TestHostileSizes(t *testing.T) {
	cases := []struct {
		name, want string
		data       []byte
	}{
		{"tiles × components", "tile-components", minimalCS(1024, 1024, 32, 32, 16384)},
		{"no tile-parts", "no tile-parts", minimalCS(64, 64, 64, 64, 3)},
		{"many components, one tile", "no tile-parts", minimalCS(8, 8, 8, 8, 16384)},
	}
	for _, c := range cases {
		err := tryDecode(t, c.name, c.data, Options{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one containing %q", c.name, err, c.want)
		}
	}
}

// TestHostileTruncation decodes every fixture truncated at many lengths.
func TestHostileTruncation(t *testing.T) {
	for _, data := range fixtureFiles(t) {
		step := max(1, len(data)/97)
		for n := 0; n < len(data); n += step {
			tryDecode(t, fmt.Sprintf("%d bytes of %d", n, len(data)), data[:n], Options{Workers: 2})
		}
	}
}

// TestHostileCorruption decodes every fixture with random bytes changed (deterministic seeds).
func TestHostileCorruption(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	files := fixtureFiles(t)
	rounds := 40
	if testing.Short() {
		rounds = 8
	}
	for i, data := range files {
		for r := range rounds {
			b := append([]byte(nil), data...)
			for range 1 + rng.IntN(4) {
				k := rng.IntN(len(b))
				switch rng.IntN(3) {
				case 0:
					b[k] ^= 1 << rng.IntN(8)
				case 1:
					b[k] = 0xFF
				default:
					b[k] = byte(rng.IntN(256))
				}
			}
			tryDecode(t, fmt.Sprintf("file %d round %d", i, r), b, Options{Reduce: rng.IntN(3), Workers: 1 + rng.IntN(3)})
		}
	}
}

// TestDeterministicWorkers checks that the result does not depend on the number of workers.
func TestDeterministicWorkers(t *testing.T) {
	for _, name := range []string{"xenium-profile.jp2", "rgb8-ict.j2k", "tiles-97-16bit.j2k", "prog-PCRL.j2k"} {
		data := hostileBase(t, name)
		want, err := Decode(data, Options{Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range []int{2, 3, 8, 0} {
			got, err := Decode(data, Options{Workers: w})
			if err != nil {
				t.Fatal(err)
			}
			for c := range want.Components {
				var s diffStats
				s.add(got.Components[c].Data, want.Components[c].Data)
				if s.differing > 0 {
					t.Errorf("%s: %d workers differ from 1: %v", name, w, s)
				}
			}
		}
	}
}
