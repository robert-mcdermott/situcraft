package xenium

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

type memFile struct{ *bytes.Reader }

func (memFile) Close() error { return nil }

// maskZip writes a cells.zarr.zip whose masks/0 is labels (h × w) in chunks of ch × cw, in the
// given order, uncompressed, with the transform for a pixel of 0.25 µm.
func maskZip(t *testing.T, labels []uint32, w, h, ch, cw int, order byte) ReaderAt {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name string, data []byte) {
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	put(".zgroup", []byte(`{"zarr_format":2}`))
	put("masks/.zgroup", []byte(`{"zarr_format":2}`))
	put("masks/0/.zarray", fmt.Appendf(nil, `{"zarr_format":2,"shape":[%d,%d],"chunks":[%d,%d],"dtype":"<u4","compressor":null,"fill_value":0,"order":"%c","filters":null}`, h, w, ch, cw, order))
	for r := 0; r*ch < h; r++ {
		for c := 0; c*cw < w; c++ {
			chunk := make([]byte, 4*ch*cw)
			for y := 0; y < ch; y++ {
				for x := 0; x < cw; x++ {
					gy, gx := r*ch+y, c*cw+x
					if gy >= h || gx >= w {
						continue
					}
					k := y*cw + x
					if order == 'F' {
						k = x*ch + y
					}
					binary.LittleEndian.PutUint32(chunk[4*k:], labels[gy*w+gx])
				}
			}
			put(fmt.Sprintf("masks/0/%d.%d", r, c), chunk)
		}
	}
	put("masks/homogeneous_transform/.zarray", []byte(`{"zarr_format":2,"shape":[3,3],"chunks":[3,3],"dtype":"<f4","compressor":null,"fill_value":0,"order":"C","filters":null}`))
	tr := make([]byte, 36)
	for i, v := range []float32{4, 0, 0, 0, 4, 0, 0, 0, 1} {
		binary.LittleEndian.PutUint32(tr[4*i:], math.Float32bits(v))
	}
	put("masks/homogeneous_transform/0.0", tr)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return memFile{bytes.NewReader(buf.Bytes())}
}

// A mask reads back, in any band of rows, as written: C and F order, chunks overlapping the edges.
func TestMasksReadInBands(t *testing.T) {
	const w, h = 11, 9
	labels := make([]uint32, w*h)
	for i := range labels {
		labels[i] = uint32(i*7%13 + 1)
	}
	for _, order := range []byte{'C', 'F'} {
		m, err := openMask(maskZip(t, labels, w, h, 4, 5, order), NucleusMask, 0)
		if err != nil {
			t.Fatal(err)
		}
		if gw, gh := m.Size(); gw != w || gh != h || m.PixelSize != 0.25 {
			t.Fatalf("%c: %d × %d, pixel %g", order, gw, gh, m.PixelSize)
		}
		for _, band := range [][2]int{{0, 9}, {3, 6}, {7, 9}, {0, 1}, {2, 2}} {
			got := make([]uint32, (band[1]-band[0])*w)
			if err := m.Rows(band[0], band[1], got); err != nil {
				t.Fatal(err)
			}
			for i, v := range got {
				if want := labels[band[0]*w+i]; v != want {
					t.Fatalf("%c, rows %v: pixel %d is %d, want %d", order, band, i, v, want)
				}
			}
		}
		if err := m.Rows(5, 10, make([]uint32, 5*w)); err == nil {
			t.Errorf("%c: rows past the end are read", order)
		}
	}
	if _, err := openMask(maskZip(t, labels, w, h, 4, 5, 'C'), CellMask, 0); err == nil {
		t.Error("a mask that is not there opens")
	}
	if m, err := openMask(maskZip(t, labels, w, h, 4, 5, 'C'), NucleusMask, 0.2125); err != nil || m.PixelSize != 0.2125 {
		t.Errorf("the manifest's pixel size is not preferred: %v", err)
	}
}
