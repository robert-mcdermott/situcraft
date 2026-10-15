package hdf5

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// lookup3.c's own self-test values (driver5).
func TestLookup3(t *testing.T) {
	for _, c := range []struct {
		text    string
		initval uint32
		want    uint32
	}{
		{"", 0, 0xdeadbeef},
		{"", 0xdeadbeef, 0xbd5b7dde},
		{"Four score and seven years ago", 0, 0x17770551},
		{"Four score and seven years ago", 1, 0xcd628161},
	} {
		if got := lookup3([]byte(c.text), c.initval); got != c.want {
			t.Errorf("lookup3(%q, %#x) = %#x, want %#x", c.text, c.initval, got, c.want)
		}
	}
}

func sample(t *testing.T) *File {
	t.Helper()
	f := NewFile()
	f.Root.AttrString("filetype", "matrix")
	f.Root.AttrInt64("version", 2)
	m, err := f.Root.Group("matrix")
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(m.Strings("barcodes", []string{"aaaa-1", "bb-1"}))
	must(m.Int32s("data", []int32{1, -2, 3}))
	must(m.Int64s("indptr", []int64{0, 2, 3}))
	features, err := m.Group("features")
	must(err)
	must(features.Strings("name", []string{"A", "B"}))
	must(m.Int32s("empty", nil))
	return f
}

// The file's structure: the superblock and every object header carry their checksums, the
// superblock gives the file's end, and every dataset's data lie where its header says.
func TestTheFileIsWellFormed(t *testing.T) {
	b := sample(t).Bytes()
	if !bytes.Equal(b[:8], []byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n'}) || b[8] != 2 {
		t.Fatal("not a version 2 superblock")
	}
	if got := binary.LittleEndian.Uint32(b[44:]); got != lookup3(b[:44], 0) {
		t.Error("the superblock's checksum")
	}
	if end := binary.LittleEndian.Uint64(b[28:]); end != uint64(len(b)) {
		t.Errorf("end of file %d, file %d bytes", end, len(b))
	}
	// Walk the headers from the root, following links.
	seen := map[string][]byte{}
	var walk func(addr uint64, path string)
	walk = func(addr uint64, path string) {
		h := b[addr:]
		if string(h[:4]) != "OHDR" || h[4] != 2 {
			t.Fatalf("%s: no object header at %d", path, addr)
		}
		size := binary.LittleEndian.Uint32(h[6:])
		end := 10 + int(size)
		if got := binary.LittleEndian.Uint32(h[end:]); got != lookup3(h[:end], 0) {
			t.Errorf("%s: the header's checksum", path)
		}
		var layoutAt, layoutSize uint64
		for at := 10; at < end; {
			typ, n := h[at], int(binary.LittleEndian.Uint16(h[at+1:]))
			body := h[at+4 : at+4+n]
			switch typ {
			case msgLink:
				name := string(body[3 : 3+int(body[2])])
				walk(binary.LittleEndian.Uint64(body[3+int(body[2]):]), path+"/"+name)
			case msgLayout:
				layoutAt, layoutSize = binary.LittleEndian.Uint64(body[2:]), binary.LittleEndian.Uint64(body[10:])
				if layoutSize == 0 {
					seen[path] = nil
				} else {
					seen[path] = b[layoutAt : layoutAt+layoutSize]
				}
			}
			at += 4 + n
		}
	}
	walk(binary.LittleEndian.Uint64(b[36:]), "")
	want := map[string]string{
		"/matrix/barcodes":      "aaaa-1bb-1\x00\x00",
		"/matrix/features/name": "AB",
	}
	for path, data := range want {
		if string(seen[path]) != data {
			t.Errorf("%s holds %q, not %q", path, seen[path], data)
		}
	}
	if len(seen) != 5 {
		t.Errorf("%d datasets reached, not 5: %v", len(seen), seen)
	}
}

// The same contents, the same bytes.
func TestTheBytesArePinned(t *testing.T) {
	sum := sha256.Sum256(sample(t).Bytes())
	const pinned = "7ace65149aaa29a3"
	if got := hex.EncodeToString(sum[:8]); got != pinned {
		t.Errorf("the sample file's digest is %s, not %s", got, pinned)
	}
}

func TestNamesAreChecked(t *testing.T) {
	f := NewFile()
	if _, err := f.Root.Group("a/b"); err == nil {
		t.Error("a name with a slash")
	}
	if err := f.Root.Int32s("x", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Root.Int32s("x", nil); err == nil {
		t.Error("a name used twice")
	}
	if err := f.Root.Strings("s", []string{"é"}); err == nil {
		t.Error("text that is not ASCII")
	}
}
