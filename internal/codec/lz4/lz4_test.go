package lz4

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"os/exec"
	"strings"
	"testing"
)

func TestHandMadeBlocks(t *testing.T) {
	cases := []struct {
		name string
		src  []byte
		want string
	}{
		{"empty", []byte{0x00}, ""},
		{"literals only", append([]byte{0x50}, "hello"...), "hello"},
		// "ab", then a match 2 back of length 6 (overlapping: it repeats "ab"), then the closing
		// sequence with no literals.
		{"overlapping match", []byte{0x22, 'a', 'b', 0x02, 0x00, 0x00}, "abababab"},
		// 15 + 3 literals (the length extended by one byte), then the end.
		{"extended literal length", append([]byte{0xf0, 0x03}, strings.Repeat("x", 18)...), strings.Repeat("x", 18)},
		// One literal, a match of 4 + 15 + 255 + 1 = 275 back 1 (a run), then one literal.
		{"extended match length", []byte{0x1f, 'z', 0x01, 0x00, 0xff, 0x01, 0x10, '!'}, strings.Repeat("z", 276) + "!"},
	}
	for _, c := range cases {
		dst := make([]byte, len(c.want))
		n, err := DecodeBlock(dst, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if string(dst[:n]) != c.want {
			t.Fatalf("%s: got %q", c.name, dst[:n])
		}
	}
}

func TestHostileBlocksAreRefused(t *testing.T) {
	cases := map[string][]byte{
		"no token":                {},
		"literals past the end":   {0x50, 'h', 'i'},
		"offset before the start": {0x10, 'a', 0x05, 0x00, 0x00},
		"zero offset":             {0x10, 'a', 0x00, 0x00, 0x00},
		"truncated offset":        {0x10, 'a', 0x01},
		"truncated length":        {0xf0, 0xff},
	}
	for name, src := range cases {
		if _, err := DecodeBlock(make([]byte, 64), src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// More output than the container recorded.
	if _, err := DecodeBlock(make([]byte, 4), []byte{0x22, 'a', 'b', 0x02, 0x00, 0x00}); err == nil {
		t.Error("a block longer than its destination was accepted")
	}
}

// Blocks the lz4 command-line tool writes (when it is installed), read back from its frames.
func TestBlocksFromTheReferenceTool(t *testing.T) {
	tool, err := exec.LookPath("lz4")
	if err != nil {
		t.Skip("lz4 is not installed")
	}
	random := rand.New(rand.NewPCG(1, 2))
	inputs := map[string][]byte{"text": bytes.Repeat([]byte("cell_id,transcript,x,y\naaaabbbb-1,EPCAM,12.5,40.25\n"), 20000)}
	floats := make([]byte, 4*200000)
	for i := 0; i < len(floats); i += 4 {
		binary.LittleEndian.PutUint32(floats[i:], uint32(random.IntN(4096))<<12)
	}
	inputs["float32"] = floats
	noise := make([]byte, 300000)
	for i := range noise {
		noise[i] = byte(random.IntN(256))
	}
	inputs["noise"] = noise
	for name, input := range inputs {
		for _, level := range []string{"-1", "-9", "--fast=8"} {
			cmd := exec.Command(tool, "-c", level, "-B4", "-BI", "--no-frame-crc")
			cmd.Stdin = bytes.NewReader(input)
			frame, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s %s: lz4: %v", name, level, err)
			}
			got, err := decodeFrame(frame)
			if err != nil {
				t.Fatalf("%s %s: %v", name, level, err)
			}
			if !bytes.Equal(got, input) {
				t.Fatalf("%s %s: decoded data differ from the input", name, level)
			}
		}
	}
}

// decodeFrame reads the LZ4 frame format around the blocks (independent blocks, no checksums),
// enough to test DecodeBlock on the tool's output.
func decodeFrame(frame []byte) ([]byte, error) {
	if binary.LittleEndian.Uint32(frame) != 0x184d2204 {
		return nil, errTruncated
	}
	flags := frame[4]
	p := 7
	if flags&0x08 != 0 {
		p += 8
	}
	var out []byte
	block := make([]byte, 64<<10)
	for {
		size := binary.LittleEndian.Uint32(frame[p:])
		p += 4
		if size == 0 {
			return out, nil
		}
		n := int(size &^ (1 << 31))
		if size&(1<<31) != 0 {
			out = append(out, frame[p:p+n]...)
		} else {
			written, err := DecodeBlock(block, frame[p:p+n])
			if err != nil {
				return nil, err
			}
			out = append(out, block[:written]...)
		}
		p += n
	}
}

func FuzzDecodeBlock(f *testing.F) {
	f.Add([]byte{0x22, 'a', 'b', 0x02, 0x00, 0x00}, 8)
	f.Add([]byte{0x1f, 'z', 0x01, 0x00, 0xff, 0x01, 0x10, '!'}, 277)
	f.Fuzz(func(t *testing.T, src []byte, size int) {
		if size < 0 || size > 1<<20 {
			return
		}
		DecodeBlock(make([]byte, size), src)
	})
}
