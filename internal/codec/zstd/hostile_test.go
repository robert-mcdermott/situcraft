// Hostile and malformed input: truncated and corrupted fixtures, wrong checksums, impossible
// declared sizes, the output limit, and hand-built frames that break one rule of RFC 8878 each.
// Every case must fail with an error (never a panic, never silently wrong output when a checksum
// is present), and the error must say what is wrong.

package zstd

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Hand-built frames. A header is the magic number, the descriptor and the fields it calls for.
func header(fhd byte, fields ...byte) []byte {
	h := binary.LittleEndian.AppendUint32(nil, frameMagic)
	return append(append(h, fhd), fields...)
}

// block makes a block header and content: typ 0 raw, 1 RLE, 2 compressed, 3 reserved.
func block(typ int, last bool, size int, content []byte) []byte {
	v := uint32(size)<<3 | uint32(typ)<<1
	if last {
		v |= 1
	}
	return append([]byte{byte(v), byte(v >> 8), byte(v >> 16)}, content...)
}

func rawBlock(last bool, data []byte) []byte { return block(0, last, len(data), data) }

func compBlock(last bool, content []byte) []byte { return block(2, last, len(content), content) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// checksum is the Content_Checksum field for content.
func checksum(content []byte) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(xxh64(content)))
}

// Window descriptor 0x00 is the smallest window, 1 KiB; 0x58 is 2^21 = 2 MiB.
const (
	window1K = 0x00
	window2M = 0x58
)

// seqBlock makes a compressed block with raw literals and sequences whose three fields are all
// in RLE mode, so the bitstream holds only extra bits: bitstream is its bytes, already laid out.
func seqBlock(last bool, lits []byte, nseq int, ll, of, ml byte, bitstream ...byte) []byte {
	content := rawLiteralsHeader(len(lits))
	content = append(content, lits...)
	content = append(content, byte(nseq), 0x54, ll, of, ml)
	content = append(content, bitstream...)
	return compBlock(last, content)
}

func rawLiteralsHeader(n int) []byte {
	switch {
	case n < 32:
		return []byte{byte(n << 3)}
	case n < 4096:
		return []byte{byte(n<<4) | 0x04, byte(n >> 4)}
	}
	return []byte{byte(n<<4) | 0x0c, byte(n >> 4), byte(n >> 12)}
}

// handBuilt is a frame built by hand, with its output or the error it must give.
type handBuilt struct {
	name string
	src  []byte
	want string // the output if ok, else part of the error
	ok   bool
}

func handBuiltFrames() []handBuilt {
	hello := []byte("hello")
	big := bytes.Repeat([]byte("0123456789"), 30)
	return []handBuilt{
		{"8-byte content size", cat(header(0xc0, window2M, 5, 0, 0, 0, 0, 0, 0, 0), rawBlock(true, hello)), "hello", true},
		{"4-byte content size", cat(header(0x80, window2M, 5, 0, 0, 0), rawBlock(true, hello)), "hello", true},
		{"2-byte content size", cat(header(0x40, window2M, 300-256, 0), rawBlock(true, big)), string(big), true},
		{"single segment, 1-byte size", cat(header(0x20, 5), rawBlock(true, hello)), "hello", true},
		{"single segment, 2-byte size", cat(header(0x60, 300-256, 0), rawBlock(true, big)), string(big), true},
		{"no content size", cat(header(0x00, window2M), rawBlock(true, hello)), "hello", true},
		{"unused bit is ignored", cat(header(0x30, 5), rawBlock(true, hello)), "hello", true},
		{"dictionary id field present but zero", cat(header(0x21, 0, 5), rawBlock(true, hello)), "hello", true},
		{"checksum", cat(header(0x24, 5), rawBlock(true, hello), checksum(hello)), "hello", true},
		{"several blocks", cat(header(0x20, 10), rawBlock(false, hello), block(1, false, 3, []byte{'!'}), rawBlock(false, nil), rawBlock(true, []byte("xy"))), "hello!!!xy", true},
		{"literals only", cat(header(0x00, window2M), compBlock(true, append(append(rawLiteralsHeader(5), hello...), 0))), "hello", true},
		{"RLE literals", cat(header(0x20, 4), compBlock(true, []byte{4<<3 | 1, 'z', 0})), "zzzz", true},
		// One sequence: 4 literals "abcd", then a match of 1027 bytes at offset 1 (offset code 2,
		// value 4; match length code 46, 1027 plus 10 extra bits, all zero).
		{"overlapping match", cat(header(0x00, window2M), seqBlock(true, []byte("abcd"), 1, 4, 2, 46, 0x00, 0x10)), "abcd" + strings.Repeat("d", 1027), true},

		{"reserved descriptor bit", cat(header(0x28, 5), rawBlock(true, hello)), "reserved bit", false},
		{"dictionary required", cat(header(0x22, 0xd2, 0x04, 5), rawBlock(true, hello)), "dictionary 1234 is required (dictionaries are not supported)", false},
		{"truncated header", header(0xc0, window2M, 5, 0), "frame header is truncated", false},
		{"missing block", header(0x00, window2M), "block header is truncated", false},
		{"reserved block type", cat(header(0x20, 5), block(3, true, 5, hello)), "reserved block type", false},
		{"block beyond the window", cat(header(0x00, window1K), rawBlock(true, make([]byte, 2000))), "block size 2000 exceeds the frame's maximum of 1024", false},
		{"block beyond 128 KiB", cat(header(0x00, window2M), block(0, true, 200000, nil)), "block size 200000 exceeds the frame's maximum of 131072", false},
		{"raw block truncated", cat(header(0x00, window2M), block(0, true, 10, hello)), "raw block of 10 bytes is truncated", false},
		{"RLE block truncated", cat(header(0x00, window2M), block(1, true, 10, nil)), "RLE block is truncated", false},
		{"content shorter than declared", cat(header(0x20, 10), rawBlock(true, hello), make([]byte, 8)), "frame decompressed to 5 bytes, but its header declares 10", false},
		{"content longer than declared", cat(header(0x80, window2M, 3, 0, 0, 0), rawBlock(true, hello)), "more than its declared content size of 3 bytes", false},
		{"declared size impossible", cat(header(0x80, window2M, 0, 0, 0, 0x40), rawBlock(true, hello)), "is more than 8 bytes of frame can hold", false},
		{"checksum mismatch", cat(header(0x24, 5), rawBlock(true, hello), []byte{1, 2, 3, 4}), "content checksum mismatch", false},
		{"checksum truncated", cat(header(0x24, 5), rawBlock(true, hello), []byte{1, 2}), "content checksum is truncated", false},
		{"unknown magic", []byte{1, 2, 3, 4, 5, 6, 7, 8}, "unknown magic number 0x04030201", false},
		{"stray bytes", cat(header(0x20, 5), rawBlock(true, hello), []byte{0x28, 0xb5}), "2 stray bytes after the last frame", false},
		{"skippable truncated", []byte{0x50, 0x2a, 0x4d, 0x18, 100, 0, 0, 0, 1, 2}, "skippable frame of 100 bytes is truncated to 2", false},
		{"treeless literals first", cat(header(0x00, window2M), compBlock(true, []byte{0x03 | 5<<4, 0x40, 0x00, 0x80, 0})), "treeless literals block without a previous Huffman table", false},
		{"repeat mode first", cat(header(0x00, window2M), compBlock(true, []byte{0, 1, 0xfc, 0x80})), "repeat mode for literal lengths without a previous table", false},
		{"reserved mode bits", cat(header(0x00, window2M), compBlock(true, []byte{0, 1, 0x01, 0x80})), "reserved bits set in the sequence compression modes", false},
		{"RLE code out of range", cat(header(0x00, window2M), compBlock(true, []byte{0, 1, 0x54, 36, 0, 0, 0x80})), "literal lengths code 36 is out of range", false},
		{"stray bytes after no sequences", cat(header(0x00, window2M), compBlock(true, append(append(rawLiteralsHeader(5), hello...), 0, 7))), "1 stray bytes after an empty sequences section", false},
		{"Huffman weight too large", cat(header(0x00, window2M), compBlock(true, []byte{0x02 | 5<<4, 0xc0, 0x00, 129, 0xc1, 0x80, 0})), "Huffman weight 12 exceeds 11", false},
		// FSE-compressed weights whose table is one symbol of probability 32 at accuracy log 5:
		// every state moves on with 0 bits, so the stream never runs out.
		{"Huffman weights never end", cat(header(0x00, window2M), compBlock(true, []byte{0x52, 0xc0, 0x01, 5, 0x10, 0xf8, 0x01, 0xff, 0x07, 0x80, 0})), "Huffman tree has more than 255 weights", false},
		// Offset code 3 with extra bits 101: offset value 13, offset 10, with 3 bytes of output.
		{"offset before the frame", cat(header(0x00, window2M), seqBlock(true, []byte("abc"), 1, 3, 3, 0, 0x0d)), "match offset 10 reaches before the start of the frame (3 bytes in)", false},
		// Literal length 0 and offset value 3 mean repeat offset 1 minus one, which is 0 at the
		// start of a frame.
		{"repeat offset zero", cat(header(0x00, window2M), seqBlock(true, nil, 1, 0, 1, 0, 0x03)), "repeat offset 1 minus one is zero", false},
		{"too few literals", cat(header(0x00, window2M), seqBlock(true, []byte("abc"), 1, 5, 2, 0, 0x04)), "sequences use more literals than the block's 3", false},
		// A 1 KiB window: the 1027-byte match makes the block larger than the block maximum.
		{"block output beyond the window", cat(header(0x00, window1K), seqBlock(true, []byte("abcd"), 1, 4, 2, 46, 0x00, 0x10)), "block decompresses to more than the frame's block maximum of 1024 bytes", false},
		// Offset code 10 with extra bits 479: offset value 1503, offset 1500, within the output
		// but beyond the 1 KiB window.
		{"offset beyond the window", cat(header(0x00, window1K), rawBlock(false, make([]byte, 1024)), rawBlock(false, make([]byte, 1024)), seqBlock(true, nil, 1, 0, 10, 0, 0xdf, 0x05)), "match offset 1500 exceeds the frame's window size of 1024", false},
		{"bitstream not consumed", cat(header(0x00, window2M), seqBlock(true, []byte("abcd"), 1, 4, 2, 0, 0x00, 0x01)), "sequences bitstream is corrupt", false},
		{"bitstream without end mark", cat(header(0x00, window2M), seqBlock(true, []byte("abcd"), 1, 4, 2, 0, 0x00)), "missing final-bit-flag", false},
	}
}

func TestHandBuiltFrames(t *testing.T) {
	for _, c := range handBuiltFrames() {
		t.Run(c.name, func(t *testing.T) {
			out, err := Decode(nil, c.src, 0)
			if c.ok {
				if err != nil || string(out) != c.want {
					t.Fatalf("Decode = %q, %v; want %q", out, err, c.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Decode succeeded with %q; want an error containing %q", out, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q; want one containing %q", err, c.want)
			}
		})
	}
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".zst"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureContent(t testing.TB, name string) []byte {
	t.Helper()
	for _, f := range fixtures {
		if f.name == name {
			return f.content()
		}
	}
	t.Fatalf("no fixture %s", name)
	return nil
}

// Every prefix of a one-frame fixture is an error, and a prefix of a several-frame one decodes
// at most to a prefix of the content.
func TestTruncated(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			comp := readFixture(t, f.name)
			want := f.content()
			// Cuts near both ends (headers, the last block, the checksum) and evenly spaced
			// ones between; fewer for the fixtures with the most output.
			ends, cuts := 64, 200
			if len(want) > 1<<20 {
				ends, cuts = 4, 12
			}
			var at []int
			for k := 1; k < ends; k++ {
				at = append(at, k, len(comp)-k)
			}
			for k := 1; k < cuts; k++ {
				at = append(at, k*len(comp)/cuts)
			}
			multi := len(f.parts) > 1
			for _, cut := range at {
				if cut <= 0 || cut >= len(comp) {
					continue
				}
				out, err := Decode(nil, comp[:cut], 0)
				if err == nil && (!multi || !bytes.HasPrefix(want, out)) {
					t.Fatalf("cut to %d of %d bytes: decoded %d bytes without an error", cut, len(comp), len(out))
				}
			}
		})
	}
}

// Corrupted bytes give an error or, where a checksum guards the frame, the right content (the
// change hit a bit the format ignores). Never a panic.
func TestCorrupted(t *testing.T) {
	for i, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			r := rng{uint64(7 + i)}
			comp := readFixture(t, f.name)
			want := f.content()
			guarded := !strings.Contains(f.argsText(), "--no-check") && len(f.parts) == 1 && f.parts[0].content != nil
			trials := 300
			switch {
			case len(want) > 1<<20:
				trials = 10
			case len(want) > 64<<10:
				trials = 60
			}
			bad := make([]byte, len(comp))
			for range trials {
				copy(bad, comp)
				for k := 1 + r.intn(3); k > 0; k-- {
					i := r.intn(len(bad))
					if r.intn(2) == 0 {
						bad[i] ^= 1 << r.intn(8)
					} else {
						bad[i] = byte(r.next())
					}
				}
				out, err := Decode(nil, bad, 64<<20)
				if err == nil && guarded && !bytes.Equal(out, want) {
					t.Fatalf("a corrupted frame with a checksum decoded to different content without an error")
				}
			}
		})
	}
}

func TestWrongChecksum(t *testing.T) {
	for _, name := range []string{"text-l3", "fcs1", "empty", "long27"} {
		comp := append([]byte(nil), readFixture(t, name)...)
		comp[len(comp)-1] ^= 0x40
		if _, err := Decode(nil, comp, 0); err == nil || err.Error() != "zstd: content checksum mismatch" {
			t.Errorf("%s with a wrong checksum: %v", name, err)
		}
	}
}

func TestOutputLimit(t *testing.T) {
	for _, name := range []string{"text-l3", "text-nosize", "rle", "multi", "long27"} {
		comp := readFixture(t, name)
		want := fixtureContent(t, name)
		// Exactly the size is fine; one byte less is not.
		if out, err := Decode(nil, comp, len(want)); err != nil || !bytes.Equal(out, want) {
			t.Errorf("%s with the limit at its size: %v", name, err)
		}
		_, err := Decode(nil, comp, len(want)-1)
		if err == nil || !strings.Contains(err.Error(), "limit") {
			t.Errorf("%s with the limit 1 byte short: %v; want a limit error", name, err)
		}
	}
	// The limit counts the output of this call, not what dst already holds.
	comp := readFixture(t, "fcs1")
	if _, err := Decode(make([]byte, 1000), comp, 100); err != nil {
		t.Errorf("limit with a non-empty dst: %v", err)
	}
	// On error, dst comes back as it was.
	dst := []byte("keep")
	out, err := Decode(dst, readFixture(t, "text-l3"), 1000)
	if err == nil || string(out) != "keep" {
		t.Errorf("Decode on error returned %q, %v; want the original dst", out, err)
	}
}

// A frame that declares a large size, or a stream that runs long, is refused before the memory
// is allocated: what is allocated stays near the limit.
func TestLimitBoundsAllocation(t *testing.T) {
	huge := cat(header(0xc0, window2M, 0, 0, 0, 0, 0, 0, 0, 1), rawBlock(true, []byte("tiny")))
	if allocated(func() { Decode(nil, huge, 0) }) > 1<<20 {
		t.Error("a frame declaring 2^56 bytes made Decode allocate more than 1 MiB")
	}
	// Within the limit, but more than the frame could hold.
	claim := cat(header(0x80, window2M, 0, 0, 0, 0x40), rawBlock(true, []byte("tiny")))
	if allocated(func() { Decode(nil, claim, 0) }) > 1<<20 {
		t.Error("a frame declaring 1 GiB in 7 bytes made Decode allocate more than 1 MiB")
	}
	// No declared size: the output grows until it reaches the limit, and no further.
	comp := readFixture(t, "long27")
	const limit = 2 << 20
	if a := allocated(func() { Decode(nil, comp, limit) }); a > 3*limit {
		t.Errorf("decoding to a %d-byte limit allocated %d bytes", limit, a)
	}
}

func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestFrameContentSize(t *testing.T) {
	for _, f := range fixtures {
		comp := readFixture(t, f.name)
		size, ok := FrameContentSize(comp)
		first := f.parts[0]
		if first.content == nil {
			first = f.parts[1] // FrameContentSize passes over the skippable frame
		}
		wantOK := !first.stdin
		if ok != wantOK || ok && size != uint64(len(first.content())) {
			t.Errorf("%s: FrameContentSize = %d, %v; want %d, %v", f.name, size, ok, len(first.content()), wantOK)
		}
	}
	for _, src := range [][]byte{nil, {0x28, 0xb5, 0x2f}, {1, 2, 3, 4, 5}, header(0xc0, window2M, 1, 2)} {
		if size, ok := FrameContentSize(src); ok {
			t.Errorf("FrameContentSize(%x) = %d, true; want unknown", src, size)
		}
	}
	if size, ok := FrameContentSize(header(0xc0, window2M, 1, 2, 3, 4, 5, 6, 7, 8)); !ok || size != 0x0807060504030201 {
		t.Errorf("8-byte content size: %x, %v", size, ok)
	}
}

func TestEmptyInput(t *testing.T) {
	out, err := Decode([]byte("x"), nil, 0)
	if err != nil || string(out) != "x" {
		t.Errorf("Decode of no input = %q, %v; want dst unchanged", out, err)
	}
}

func TestXXH64(t *testing.T) {
	// The published XXH64 value for no input with seed 0.
	if got := xxh64(nil); got != 0xEF46DB3751D8E999 {
		t.Errorf("xxh64(empty) = %#x", got)
	}
}

// The zstd tool stores the low 32 bits of XXH64 in each frame; check every tail length around
// the 32-byte stripe and the 8-, 4- and 1-byte steps.
func TestXXH64AgainstCLI(t *testing.T) {
	zstdCLI(t)
	data := genText(99, 300)
	lengths := []int{100, 127, 128, 129, 255, 256, 300}
	for n := 0; n <= 72; n++ {
		lengths = append(lengths, n)
	}
	for _, n := range lengths {
		comp := cliCompress(t, data[:n], []string{"-1"}, false)
		want := binary.LittleEndian.Uint32(comp[len(comp)-4:])
		if got := uint32(xxh64(data[:n])); got != want {
			t.Fatalf("xxh64 of %d bytes: low 32 bits %#x; zstd stored %#x", n, got, want)
		}
	}
}
