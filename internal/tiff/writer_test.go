package tiff

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// A plane written with its pyramid reads back exactly, every level, with its OME-XML.
func TestWriteOMETIFFReadsBack(t *testing.T) {
	p := &Plane16{Width: 1000, Height: 700, Pix: make([]uint16, 1000*700)}
	for i := range p.Pix {
		p.Pix[i] = uint16((i*7919 + i/1000*31) % 65536)
	}
	xml := `<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"><Image ID="Image:0"><Pixels ID="Pixels:0" DimensionOrder="XYZCT" Type="uint16" SizeX="1000" SizeY="700" SizeZ="1" SizeC="1" SizeT="1" PhysicalSizeX="0.2125" PhysicalSizeY="0.2125"><Channel ID="Channel:0" Name="DAPI"/><TiffData IFD="0" PlaneCount="1"/></Pixels></Image></OME>`
	path := filepath.Join(t.TempDir(), "a.ome.tif")
	if err := WriteOMETIFF(path, p, WriteOptions{Description: xml, Software: "test", Tile: 256, Levels: 2}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	tf, err := Open(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(tf.IFDs) != 1 || len(tf.IFDs[0].SubIFDs) != 2 {
		t.Fatalf("%d directories, %d SubIFDs", len(tf.IFDs), len(tf.IFDs[0].SubIFDs))
	}
	if d := tf.IFDs[0].Description; d != xml {
		t.Fatalf("description %q", d)
	}
	if _, err := ParseOME(tf.IFDs[0].Description); err != nil {
		t.Fatal(err)
	}
	want := p
	for level, ifd := range append([]*IFD{tf.IFDs[0]}, tf.IFDs[0].SubIFDs...) {
		if level > 0 {
			want = want.Halve()
		}
		if ifd.Width != want.Width || ifd.Height != want.Height || ifd.Compression != CompressionDeflate || ifd.Predictor != 2 {
			t.Fatalf("level %d: %d × %d, compression %d, predictor %d", level, ifd.Width, ifd.Height, ifd.Compression, ifd.Predictor)
		}
		raw, err := ifd.ReadImage()
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		for i, v := range want.Pix {
			if got := binary.LittleEndian.Uint16(raw[2*i:]); got != v {
				t.Fatalf("level %d pixel %d: %d, want %d", level, i, got, v)
			}
		}
	}
}
