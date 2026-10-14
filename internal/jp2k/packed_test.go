// Packed packet headers (PPM, PPT; A.7.4, A.7.5): no encoder at hand writes them, so the tests
// make them by moving the packet headers of an ordinary codestream into PPM or PPT marker
// segments, using this package's own tier-2 parse to find each header's extent.

package jp2k

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// packHeaders rewrites a codestream (one tile-part per tile, no SOP, TLM or PLT markers) with its
// packet headers in PPM marker segments of the main header (ppm) or in PPT marker segments of each
// tile-part header.
func packHeaders(cs []byte, ppm bool) ([]byte, error) {
	d, err := newDecoder(cs, Options{})
	if err != nil {
		return nil, err
	}
	firstSOT := -1
	for pos := 2; pos+4 <= len(cs); {
		m := u16(cs[pos:])
		if m == mSOT {
			firstSOT = pos
			break
		}
		pos += 2 + u16(cs[pos+2:])
	}
	if firstSOT < 0 {
		return nil, fmt.Errorf("no SOT")
	}
	type tileOut struct{ hdrs, bodies []byte }
	tiles := make([]tileOut, len(d.tiles))
	for i, tp := range d.tiles {
		if tp == nil || tp.parts != 1 {
			return nil, fmt.Errorf("tile %d: need exactly one tile-part", i)
		}
		t, err := d.setupTile(i, tp)
		if err != nil {
			return nil, err
		}
		pr := &packetReader{t: t, body: tp.bodies[0], sop: t.sop, eph: t.eph}
		err = t.progression(func(c, r, p, l int) error {
			start := pr.pos
			if err := pr.packet(c, r, p, l); err != nil {
				return err
			}
			n := 0
			for _, pi := range pr.pieces {
				n += pi.length
			}
			hdrEnd := pr.pos - n
			tiles[i].hdrs = append(tiles[i].hdrs, pr.body[start:hdrEnd]...)
			tiles[i].bodies = append(tiles[i].bodies, pr.body[hdrEnd:pr.pos]...)
			return nil
		})
		if err != nil && err != errDataEnded {
			return nil, err
		}
	}
	// segments splits data into marker segments of at most 65535 bytes, each with an index byte.
	segments := func(marker int, data []byte) []byte {
		var out []byte
		for z := 0; z == 0 || len(data) > 0; z++ {
			n := min(len(data), 65535-3)
			out = binary.BigEndian.AppendUint16(out, uint16(marker))
			out = binary.BigEndian.AppendUint16(out, uint16(n+3))
			out = append(out, byte(z))
			out = append(out, data[:n]...)
			data = data[n:]
		}
		return out
	}
	out := append([]byte(nil), cs[:firstSOT]...)
	if ppm {
		var all []byte
		for _, t := range tiles {
			all = binary.BigEndian.AppendUint32(all, uint32(len(t.hdrs)))
			all = append(all, t.hdrs...)
		}
		out = append(out, segments(mPPM, all)...)
	}
	for i, t := range tiles {
		var hdr []byte
		if !ppm {
			hdr = segments(mPPT, t.hdrs)
		}
		psot := 12 + len(hdr) + 2 + len(t.bodies)
		out = binary.BigEndian.AppendUint16(out, mSOT)
		out = binary.BigEndian.AppendUint16(out, 10)
		out = binary.BigEndian.AppendUint16(out, uint16(i))
		out = binary.BigEndian.AppendUint32(out, uint32(psot))
		out = append(out, 0, 1)
		out = append(out, hdr...)
		out = binary.BigEndian.AppendUint16(out, mSOD)
		out = append(out, t.bodies...)
	}
	return binary.BigEndian.AppendUint16(out, mEOC), nil
}

// TestPackedHeaders moves the packet headers of OpenJPEG codestreams into PPM and PPT marker
// segments and checks that the decode is unchanged and matches OpenJPEG's decode of the
// rewritten codestream.
func TestPackedHeaders(t *testing.T) {
	if opjCompress == "" || opjDecompress == "" {
		t.Skip("opj_compress or opj_decompress is not on PATH")
	}
	cases := []struct {
		pattern string
		w, h, n int
		args    []string
	}{
		{"blobs", 70, 50, 1, []string{"-r", "20,8,2", "-p", "LRCP"}},
		{"noise", 64, 64, 3, []string{"-I", "-r", "10,4", "-t", "24,40", "-n", "4", "-p", "RPCL", "-c", "[16,16]"}},
		{"edges", 97, 61, 1, []string{"-n", "4", "-b", "16,8", "-p", "PCRL", "-EPH", "-M", "5"}},
		{"gradient", 40, 33, 3, []string{"-r", "30,10,1", "-t", "16,16", "-n", "3", "-p", "CPRL", "-EPH"}},
	}
	for _, c := range cases {
		dx := make([]int, c.n)
		for i := range dx {
			dx[i] = 1
		}
		im := makeImage(c.pattern, c.w, c.h, c.n, 8, false, dx, dx, 7)
		cs, err := opjEncode(t, im, c.args, false)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Decode(cs, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, ppm := range []bool{true, false} {
			packed, err := packHeaders(cs, ppm)
			if err != nil {
				t.Fatalf("%v ppm %v: %v", c.args, ppm, err)
			}
			got, err := Decode(packed, Options{})
			if err != nil {
				t.Fatalf("%v ppm %v: %v", c.args, ppm, err)
			}
			for k := range want.Components {
				var s diffStats
				s.add(got.Components[k].Data, want.Components[k].Data)
				if s.differing > 0 {
					t.Errorf("%v ppm %v: component %d differs from the unpacked decode: %v", c.args, ppm, k, s)
				}
			}
			ref, err := opjDecode(t, packed, 0)
			if err != nil {
				t.Fatalf("%v ppm %v: %v", c.args, ppm, err)
			}
			emu, err := Decode(packed, Options{opjHighpass: true})
			if err != nil {
				t.Fatal(err)
			}
			// Bit for bit with the OpenJPEG versions whose 9/7 decode is matched (opjExact);
			// within one level otherwise.
			if s, err := compareRef(emu, ref); err != nil || (opjExact() && s.differing > 0) || s.maxAbs > 1 {
				t.Errorf("%v ppm %v: differs from OpenJPEG: %v %v", c.args, ppm, s, err)
			}
		}
	}
}
