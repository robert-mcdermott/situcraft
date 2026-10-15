package zarr

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Hierarchies written in each format read back as written: groups and their attributes, numbers
// split into chunks (the last padded), a two-dimensional array in chunks, text in chunks, a
// scalar; the same values give the same files.
func TestWriterRoundTrip(t *testing.T) {
	for _, format := range []int{2, 3} {
		t.Run(fmt.Sprint("format ", format), func(t *testing.T) {
			write := func(root string) {
				w, err := NewWriter(root, format, map[string]any{"kind": "test", "list": []any{"a", "b"}})
				if err != nil {
					t.Fatal(err)
				}
				if err := w.Group("g", map[string]any{"n": 3}); err != nil {
					t.Fatal(err)
				}
				ints := make([]byte, 4*10)
				for i := 0; i < 10; i++ {
					binary.LittleEndian.PutUint32(ints[4*i:], uint32(i*i))
				}
				if err := w.WriteArray("g/ints", "<u4", []int{10}, ints, 4, map[string]any{"unit": "count"}); err != nil {
					t.Fatal(err)
				}
				if err := w.WriteArray("g/one", "<f8", []int{}, binary.LittleEndian.AppendUint64(nil, 0x400921fb54442d18), 0, nil); err != nil {
					t.Fatal(err)
				}
				if err := w.WriteArray("g/flags", "|b1", []int{3}, []byte{1, 0, 1}, 0, nil); err != nil {
					t.Fatal(err)
				}
				// A 5 × 3 matrix in 2 × 2 chunks.
				a, err := w.Array("g/m", ArraySpec{Shape: []int{5, 3}, Chunks: []int{2, 2}, DType: "<i2", Dimensions: []string{"y", "x"}})
				if err != nil {
					t.Fatal(err)
				}
				for cy := 0; cy < 3; cy++ {
					for cx := 0; cx < 2; cx++ {
						chunk := make([]byte, 8)
						for y := 0; y < 2; y++ {
							for x := 0; x < 2; x++ {
								if gy, gx := 2*cy+y, 2*cx+x; gy < 5 && gx < 3 {
									binary.LittleEndian.PutUint16(chunk[2*(2*y+x):], uint16(10*gy+gx))
								}
							}
						}
						if err := a.WriteChunk([]int{cy, cx}, chunk); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := w.WriteTextArray("g/names", []string{"α", "", "cell-3", "x"}, 3, nil, false); err != nil {
					t.Fatal(err)
				}
				if err := w.WriteTextArray("g/name", []string{"only"}, 0, nil, true); err != nil {
					t.Fatal(err)
				}
			}
			dir := t.TempDir()
			write(filepath.Join(dir, "a.zarr"))
			write(filepath.Join(dir, "b.zarr"))
			sameTrees(t, filepath.Join(dir, "a.zarr"), filepath.Join(dir, "b.zarr"))

			s := OpenDir(filepath.Join(dir, "a.zarr"))
			attrs, err := Attributes(s, "")
			if err != nil || attrs["kind"] != "test" {
				t.Fatalf("root attributes %v: %v", attrs, err)
			}
			if attrs, _ := Attributes(s, "g"); attrs["n"] != float64(3) {
				t.Fatalf("group attributes %v", attrs)
			}
			ints, err := OpenArray(s, "g/ints")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := ints.ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 10; i++ {
				if v := binary.LittleEndian.Uint32(raw[4*i:]); v != uint32(i*i) {
					t.Fatalf("ints[%d] = %d", i, v)
				}
			}
			if ints.Attrs["unit"] != "count" {
				t.Fatalf("array attributes %v", ints.Attrs)
			}
			one, err := OpenArray(s, "g/one")
			if err != nil {
				t.Fatal(err)
			}
			if raw, err := one.ReadAll(); err != nil || binary.LittleEndian.Uint64(raw) != 0x400921fb54442d18 {
				t.Fatalf("scalar %x: %v", raw, err)
			}
			flags, _ := OpenArray(s, "g/flags")
			if raw, err := flags.ReadAll(); err != nil || !bytes.Equal(raw, []byte{1, 0, 1}) {
				t.Fatalf("flags %v: %v", raw, err)
			}
			m, err := OpenArray(s, "g/m")
			if err != nil {
				t.Fatal(err)
			}
			raw, err = m.ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 5; y++ {
				for x := 0; x < 3; x++ {
					if v := binary.LittleEndian.Uint16(raw[2*(3*y+x):]); v != uint16(10*y+x) {
						t.Fatalf("m[%d,%d] = %d", y, x, v)
					}
				}
			}
			names, err := OpenArray(s, "g/names")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := names.ReadStrings(); err != nil || fmt.Sprint(got) != fmt.Sprint([]string{"α", "", "cell-3", "x"}) {
				t.Fatalf("names %q: %v", got, err)
			}
			name, err := OpenArray(s, "g/name")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := name.ReadStrings(); err != nil || len(got) != 1 || got[0] != "only" {
				t.Fatalf("name %q: %v", got, err)
			}
			list, err := Arrays(s)
			if err != nil || len(list) != 6 {
				t.Fatalf("arrays %v: %v", list, err)
			}
		})
	}
}

func sameTrees(t *testing.T, a, b string) {
	t.Helper()
	filepath.Walk(a, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(a, p)
		x, _ := os.ReadFile(p)
		y, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || !bytes.Equal(x, y) {
			t.Fatalf("%s differs between two writes of the same values", rel)
		}
		return nil
	})
}

func TestWriterRefusals(t *testing.T) {
	w, err := NewWriter(t.TempDir(), 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWriter(t.TempDir(), 4, nil); err == nil {
		t.Fatal("format 4 was written")
	}
	if _, err := w.Array("a", ArraySpec{Shape: []int{3}, DType: "<c8"}); err == nil {
		t.Fatal("a complex type was written")
	}
	a, _ := w.Array("b", ArraySpec{Shape: []int{4}, Chunks: []int{2}, DType: "<u4"})
	if err := a.WriteChunk([]int{2}, make([]byte, 8)); err == nil {
		t.Fatal("a chunk outside the array was written")
	}
	if err := a.WriteChunk([]int{0}, make([]byte, 4)); err == nil {
		t.Fatal("a short chunk was written")
	}
	if err := w.Group("../out", nil); err == nil {
		t.Fatal("a group outside the store was written")
	}
}
