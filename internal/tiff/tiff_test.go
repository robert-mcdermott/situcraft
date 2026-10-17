package tiff

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type level struct {
	Shape  []int  `json:"shape"`
	DType  string `json:"dtype"`
	SHA256 string `json:"sha256"`
}

func openFile(t testing.TB, path string) *File {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return f
}

// Files written by tifffile 2025 with imagecodecs (testdata/make_fixtures.py): both byte orders,
// BigTIFF, strips and tiles with partial edges, LZW, deflate and PackBits, both predictors, planar
// RGB, a SubIFD pyramid and a three-channel OME-TIFF; every pixel compared by SHA-256. JPEG (YCbCr,
// RGB and grey) within its tolerance of libjpeg-turbo's pixels: decoders round differently.
func TestFilesFromTifffile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []struct {
			Name      string  `json:"name"`
			Tolerance int     `json:"tolerance"`
			Levels    []level `json:"levels"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Cases {
		f := openFile(t, filepath.Join("testdata", c.Name+".tif"))
		var images []*IFD
		if len(c.Levels) > 1 {
			images = append([]*IFD{f.IFDs[0]}, f.IFDs[0].SubIFDs...)
		} else {
			images = []*IFD{nil}
		}
		for l, want := range c.Levels {
			var pixels []byte
			if images[l] == nil {
				// One level, possibly over several directories (the OME planes).
				for _, ifd := range f.IFDs {
					p, err := ifd.ReadImage()
					if err != nil {
						t.Fatalf("%s: %v", c.Name, err)
					}
					pixels = append(pixels, p...)
				}
			} else if pixels, err = images[l].ReadImage(); err != nil {
				t.Fatalf("%s level %d: %v", c.Name, l, err)
			}
			if c.Tolerance > 0 {
				ref, err := os.ReadFile(filepath.Join("testdata", c.Name+".raw"))
				if err != nil {
					t.Fatal(err)
				}
				worst := 0
				for i := range ref {
					if i < len(pixels) {
						worst = max(worst, abs(int(pixels[i])-int(ref[i])))
					}
				}
				if len(pixels) != len(ref) || worst > c.Tolerance {
					t.Errorf("%s: %d samples, %d expected; at most %d from libjpeg-turbo's (tolerance %d)", c.Name, len(pixels), len(ref), worst, c.Tolerance)
				}
				continue
			}
			sum := sha256.Sum256(pixels)
			if got := hex.EncodeToString(sum[:]); got != want.SHA256 {
				t.Errorf("%s level %d (%v %s): pixels differ (SHA-256 %s)", c.Name, l, want.Shape, want.DType, got)
			}
		}
	}
}

func TestStructureAndOME(t *testing.T) {
	f := openFile(t, filepath.Join("testdata", "bigtiff-pyramid.tif"))
	if !f.BigTIFF || len(f.IFDs) != 1 || len(f.IFDs[0].SubIFDs) != 2 {
		t.Fatalf("BigTIFF %v, %d IFDs, %d SubIFDs", f.BigTIFF, len(f.IFDs), len(f.IFDs[0].SubIFDs))
	}
	top := f.IFDs[0]
	if top.Width != 90 || top.Height != 70 || !top.Tiled || top.BlockWidth != 32 {
		t.Fatalf("top level %d×%d tiled %v %d", top.Width, top.Height, top.Tiled, top.BlockWidth)
	}
	if across, down := top.Blocks(); across != 3 || down != 3 {
		t.Fatalf("blocks %d×%d", across, down)
	}
	if s := top.SubIFDs[1]; s.Width != 23 || s.Height != 18 {
		t.Fatalf("smallest level %d×%d", s.Width, s.Height)
	}
	o := openFile(t, filepath.Join("testdata", "ome-3channels.tif"))
	ome, err := ParseOME(o.IFDs[0].Description)
	if err != nil {
		t.Fatal(err)
	}
	px := ome.Images[0].Pixels
	if px.SizeC != 3 || px.SizeX != 37 || px.PhysicalSizeX != 0.5 || len(px.Channels) != 3 || px.Channels[1].Name != "CD45" {
		t.Fatalf("pixels %+v", px)
	}
	planes, err := px.Planes(ome.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(planes) != 3 || planes[2].C != 2 || planes[2].IFD != 2 || planes[2].FileName != "" {
		t.Fatalf("planes %+v", planes)
	}
}

func TestMultiFileOMEPlanes(t *testing.T) {
	// The shape of Xenium's four morphology-focus files: each one's XML names all four.
	xml := `<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06" UUID="urn:uuid:c">
  <Image ID="Image:0"><Pixels ID="Pixels:0" DimensionOrder="XYZCT" Type="uint16" SizeX="10" SizeY="8" SizeZ="1" SizeC="3" SizeT="1" PhysicalSizeX="0.2125" PhysicalSizeY="0.2125">
    <Channel ID="Channel:0" Name="DAPI"><AnnotationRef ID="Annotation:0"/></Channel>
    <Channel ID="Channel:1" Name="Boundary"/><Channel ID="Channel:2" Name="18S"/>
    <TiffData FirstC="0" PlaneCount="1"><UUID FileName="a.ome.tif">urn:uuid:a</UUID></TiffData>
    <TiffData FirstC="1" PlaneCount="1"><UUID FileName="b.ome.tif">urn:uuid:b</UUID></TiffData>
    <TiffData FirstC="2" PlaneCount="1"><UUID FileName="c.ome.tif">urn:uuid:c</UUID></TiffData>
  </Pixels></Image>
  <StructuredAnnotations><MapAnnotation ID="Annotation:0"><Value><M K="Purpose">Nuclear</M></Value></MapAnnotation></StructuredAnnotations>
</OME>`
	ome, err := ParseOME(xml)
	if err != nil {
		t.Fatal(err)
	}
	px := ome.Images[0].Pixels
	planes, err := px.Planes(ome.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if planes[0].FileName != "a.ome.tif" || planes[1].FileName != "b.ome.tif" || planes[2].FileName != "" || planes[2].IFD != 0 {
		t.Fatalf("planes %+v", planes)
	}
	if px.Channels[0].Annotations["Purpose"] != "Nuclear" {
		t.Fatalf("channel annotations %+v", px.Channels[0].Annotations)
	}
	// A z-stack in one file: one TiffData with a plane count, planes in consecutive IFDs.
	stack, _ := ParseOME(`<OME><Image ID="i"><Pixels DimensionOrder="XYZCT" Type="uint16" SizeX="4" SizeY="4" SizeZ="11" SizeC="1" SizeT="1"><TiffData PlaneCount="11"/></Pixels></Image></OME>`)
	planes, err = stack.Images[0].Pixels.Planes("")
	if err != nil || len(planes) != 11 || planes[10].Z != 10 || planes[10].IFD != 10 {
		t.Fatalf("z-stack planes %+v %v", planes, err)
	}
	if _, err := ParseOME("<html/>"); err == nil {
		t.Error("a non-OME description was accepted")
	}
}

// A hand-written classic TIFF whose IFD points back to itself must not loop.
func TestHostileFilesAreRefused(t *testing.T) {
	loop := make([]byte, 64)
	copy(loop, "II")
	binary.LittleEndian.PutUint16(loop[2:], 42)
	binary.LittleEndian.PutUint32(loop[4:], 8)
	binary.LittleEndian.PutUint16(loop[8:], 1)
	binary.LittleEndian.PutUint16(loop[10:], TagImageWidth)
	binary.LittleEndian.PutUint16(loop[12:], typeShort)
	binary.LittleEndian.PutUint32(loop[14:], 1)
	binary.LittleEndian.PutUint16(loop[18:], 4)
	binary.LittleEndian.PutUint32(loop[22:], 8) // next IFD: itself
	if _, err := Open(bytes.NewReader(loop), int64(len(loop))); err == nil {
		t.Error("an IFD loop was accepted")
	}
	for _, bad := range [][]byte{{}, []byte("II*"), []byte("XX*\x00\x08\x00\x00\x00"), []byte("II+\x00\x04\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")} {
		if _, err := Open(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	data, _ := os.ReadFile(filepath.Join("testdata", "u16-lzw-predictor.tif"))
	for cut := 8; cut < len(data); cut += len(data) / 17 {
		f, err := Open(bytes.NewReader(data[:cut]), int64(cut))
		if err == nil {
			for _, ifd := range f.IFDs {
				ifd.ReadImage()
			}
		}
	}
}

func FuzzOpen(f *testing.F) {
	for _, name := range []string{"u16-lzw-predictor", "bigtiff-pyramid", "rgb-deflate-planar", "u8-packbits"} {
		data, err := os.ReadFile(filepath.Join("testdata", name+".tif"))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		for _, ifd := range file.IFDs {
			if ifd.Width*ifd.Height <= 1<<16 {
				ifd.ReadImage()
			}
		}
	})
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// A JPEG tile whose tables the image keeps in JPEGTables (an abbreviated stream) decodes as the
// complete stream it came from.
func TestJPEGSharedTables(t *testing.T) {
	f := openFile(t, filepath.Join("testdata", "rgb-jpeg-ycbcr-tiles.tif"))
	ifd := f.IFDs[0]
	full, err := ifd.RawBlock(0)
	if err != nil {
		t.Fatal(err)
	}
	// The DQT and DHT segments go to the tables; SOI, the frame, the scan and EOI stay.
	tables := []byte{0xFF, 0xD8}
	stream := []byte{0xFF, 0xD8}
	for p := 2; p+4 <= len(full); {
		marker, n := full[p+1], int(full[p+2])<<8|int(full[p+3])
		segment := full[p : p+2+n]
		if marker == 0xDB || marker == 0xC4 {
			tables = append(tables, segment...)
		} else {
			stream = append(stream, segment...)
		}
		if marker == 0xDA {
			stream = append(stream, full[p+2+n:]...)
			break
		}
		p += 2 + n
	}
	tables = append(tables, 0xFF, 0xD9)
	want, err := ifd.DecodeBlock(0)
	if err != nil {
		t.Fatal(err)
	}
	abbreviated := *ifd
	abbreviated.JPEGTables = tables
	width, height := ifd.BlockSize(0)
	got, err := decodeJPEG(&abbreviated, stream, width, height)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("the abbreviated stream decodes otherwise")
	}
	if _, err := decodeJPEG(ifd, stream, width, height); err == nil {
		t.Error("an abbreviated stream without its tables decoded")
	}
}
