// Features of Part 2 (ISO/IEC 15444-2) and Part 15 (high-throughput coding) must be refused with
// an error naming the feature. The OpenJPEG encoder at hand writes none of them, so these tests
// edit the marker segments of a valid Part 1 codestream.

package jp2k

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mainSegment returns the offset of the first marker segment m in the main header (its marker),
// or -1.
func mainSegment(cs []byte, m int) int {
	for pos := 2; pos+4 <= len(cs); {
		mk := u16(cs[pos:])
		if mk == m {
			return pos
		}
		if mk == mSOT {
			return -1
		}
		pos += 2 + u16(cs[pos+2:])
	}
	return -1
}

// insertSegment inserts a marker segment with the given parameters after the SIZ segment.
func insertSegment(cs []byte, m int, params ...byte) []byte {
	siz := mainSegment(cs, mSIZ)
	at := siz + 2 + u16(cs[siz+2:])
	seg := []byte{byte(m >> 8), byte(m), byte((len(params) + 2) >> 8), byte(len(params) + 2)}
	seg = append(seg, params...)
	out := append([]byte(nil), cs[:at]...)
	out = append(out, seg...)
	return append(out, cs[at:]...)
}

func TestPart2Refused(t *testing.T) {
	base, err := os.ReadFile(filepath.Join(fixtureDir, "rgb8-ict.j2k"))
	if err != nil {
		t.Fatal(err)
	}
	edit := func(f func(cs []byte)) []byte {
		cs := append([]byte(nil), base...)
		f(cs)
		return cs
	}
	cod := mainSegment(base, mCOD) + 4 // COD parameters: Scod, SGcod (4), SPcod
	cases := []struct {
		name, want string
		cs         []byte
	}{
		{"Rsiz Part 2", "Part 2 extensions", edit(func(cs []byte) { cs[6] |= 0x80 })},
		{"Rsiz Part 15", "Part 15", edit(func(cs []byte) { cs[6] |= 0x40 })},
		{"CAP marker", "CAP marker", insertSegment(base, mCAP, 0, 2, 0, 0, 0, 0)},
		{"array-based MCT", "array-based transform", edit(func(cs []byte) { cs[cod+4] = 2 })},
		{"arbitrary kernel", "arbitrary transformation kernel", edit(func(cs []byte) { cs[cod+9] = 2 })},
		{"HT code-blocks", "high-throughput", edit(func(cs []byte) { cs[cod+8] |= 0x40 })},
		{"code-block anchor", "partition origin", edit(func(cs []byte) { cs[cod] |= 0x08 })},
		{"MCT marker", "multiple component transformation (MCT marker)", insertSegment(base, 0xFF74, 0, 0)},
		{"ATK marker", "arbitrary transformation kernels (ATK marker)", insertSegment(base, 0xFF79, 0, 0)},
		{"DCO marker", "variable DC offset (DCO marker)", insertSegment(base, 0xFF70, 0, 0)},
		{"NLT marker", "non-linearity point transformation (NLT marker)", insertSegment(base, 0xFF76, 0, 0)},
		{"ROI general scaling", "general scaling", insertSegment(base, mRGN, 0, 1, 3)},
	}
	for _, c := range cases {
		_, err := Decode(c.cs, Options{})
		if err == nil {
			t.Errorf("%s: decoded without error", c.name)
			continue
		}
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q, want ErrUnsupported naming %q", c.name, err, c.want)
		}
	}
	// The unedited codestream decodes.
	if _, err := Decode(base, Options{}); err != nil {
		t.Fatal(err)
	}
}
