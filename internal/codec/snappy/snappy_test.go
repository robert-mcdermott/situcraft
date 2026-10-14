package snappy

import (
	"strings"
	"testing"
)

func TestHandMadeBlocks(t *testing.T) {
	long := strings.Repeat("0123456789", 10)
	cases := []struct {
		name string
		src  []byte
		want string
	}{
		{"empty", []byte{0x00}, ""},
		{"literal", []byte{0x03, 0x08, 'a', 'b', 'c'}, "abc"},
		// "abcd", then a copy of 4 bytes 4 back with a 1-byte offset.
		{"copy, 1-byte offset", []byte{0x08, 0x0c, 'a', 'b', 'c', 'd', 0x01, 0x04}, "abcdabcd"},
		// "a", then an overlapping copy of 9 bytes 1 back with a 2-byte offset.
		{"overlapping copy, 2-byte offset", []byte{0x0a, 0x00, 'a', 0x22, 0x01, 0x00}, "aaaaaaaaaa"},
		// "xyz", then a copy of 3 bytes 3 back with a 4-byte offset.
		{"copy, 4-byte offset", []byte{0x06, 0x08, 'x', 'y', 'z', 0x0b, 0x03, 0x00, 0x00, 0x00}, "xyzxyz"},
		// A 100-byte literal: tag 60 (one length byte follows), length - 1 = 99.
		{"long literal", append([]byte{0x64, 0xf0, 99}, long...), long},
	}
	for _, c := range cases {
		got, err := Decode(c.src, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if string(got) != c.want {
			t.Fatalf("%s: got %q", c.name, got)
		}
		if n, _ := DecodedLen(c.src); n != len(c.want) {
			t.Fatalf("%s: DecodedLen %d", c.name, n)
		}
	}
}

func TestHostileBlocksAreRefused(t *testing.T) {
	cases := map[string][]byte{
		"no length":               {},
		"unterminated varint":     {0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"literal past the end":    {0x05, 0x10, 'a'},
		"offset before the start": {0x04, 0x00, 'a', 0x01, 0x05},
		"too short":               {0x05, 0x00, 'a'},
		"too long":                {0x01, 0x04, 'a', 'b'},
	}
	for name, src := range cases {
		if _, err := Decode(src, 0); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Decode([]byte{0xff, 0xff, 0x7f}, 1000); err == nil {
		t.Error("a block declaring more than the limit was accepted")
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte{0x0a, 0x00, 'a', 0x22, 0x01, 0x00})
	f.Add([]byte{0x08, 0x0c, 'a', 'b', 'c', 'd', 0x01, 0x04})
	f.Fuzz(func(t *testing.T, src []byte) {
		Decode(src, 1<<20)
	})
}
