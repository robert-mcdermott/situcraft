package slide

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/tiff"
)

// level reads a whole level of a channel from its tiles: row-major, an RGB pixel's samples
// together, 16-bit samples little-endian.
func level(t *testing.T, im *Image, l, c int) []byte {
	t.Helper()
	lv := im.Levels[l]
	px := im.PixelBytes()
	out := make([]byte, lv.Width*lv.Height*px)
	for ty := 0; ty < lv.Down; ty++ {
		for tx := 0; tx < lv.Across; tx++ {
			tile, err := im.Tile(l, c, tx, ty)
			if err != nil {
				t.Fatal(err)
			}
			if len(tile) != lv.TileWidth*lv.TileHeight*px {
				t.Fatalf("tile (%d, %d) of level %d: %d bytes", tx, ty, l, len(tile))
			}
			for y := 0; y < lv.TileHeight && ty*lv.TileHeight+y < lv.Height; y++ {
				w := min(lv.TileWidth, lv.Width-tx*lv.TileWidth)
				src := tile[y*lv.TileWidth*px : (y*lv.TileWidth+w)*px]
				copy(out[((ty*lv.TileHeight+y)*lv.Width+tx*lv.TileWidth)*px:], src)
			}
		}
	}
	return out
}

// Images written by tifffile (testdata/make_fixtures.py), as Bio-Formats and tifffile write them for
// Xenium Explorer: every level of every channel, read a tile at a time, the pixels tifffile reads
// (a JPEG image's within 3 of libjpeg-turbo's).
func TestImagesFromTifffile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []struct {
			Name   string
			Levels []struct {
				Shape  []int
				SHA256 []string
			}
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Cases {
		im, err := Open(filepath.Join("testdata", c.Name+".ome.tif"))
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		rgb := strings.HasPrefix(c.Name, "he-")
		if (im.Kind == RGB) != rgb || len(im.Levels) != len(c.Levels) {
			t.Fatalf("%s: %s, %d levels", c.Name, im.Kind, len(im.Levels))
		}
		if !rgb && !slices.Equal([]string{im.Channels[0].Name, im.Channels[1].Name, im.Channels[2].Name}, []string{"CD20", "HER2", "DAPI"}) {
			t.Errorf("%s: channels %v", c.Name, im.Channels)
		}
		if rgb && im.PixelSize != 0.3638 {
			t.Errorf("%s: pixel size %v", c.Name, im.PixelSize)
		}
		for l, want := range c.Levels {
			h, w := want.Shape[0], want.Shape[1]
			if !rgb {
				h, w = want.Shape[1], want.Shape[2]
			}
			if im.Levels[l].Width != w || im.Levels[l].Height != h {
				t.Errorf("%s level %d: %d×%d, not %d×%d", c.Name, l, im.Levels[l].Width, im.Levels[l].Height, w, h)
				continue
			}
			for ch := range want.SHA256 {
				pixels := level(t, im, l, ch)
				if c.Name == "he-jpeg" {
					ref, err := os.ReadFile(filepath.Join("testdata", c.Name+"-"+strconv.Itoa(l)+".raw"))
					if err != nil {
						t.Fatal(err)
					}
					worst := 0
					for i := range ref {
						d := int(pixels[i]) - int(ref[i])
						worst = max(worst, d, -d)
					}
					if len(ref) != len(pixels) || worst > 3 {
						t.Errorf("%s level %d: at most %d from libjpeg-turbo's", c.Name, l, worst)
					}
					continue
				}
				sum := sha256.Sum256(pixels)
				if got := hex.EncodeToString(sum[:]); got != want.SHA256[ch] {
					t.Errorf("%s level %d channel %d: pixels differ", c.Name, l, ch)
				}
			}
		}
		im.Close()
	}
}

// A flat image too large to show whole, and a pyramid in strips, are refused, saying what to do.
func TestImagesRefused(t *testing.T) {
	for name, want := range map[string]string{"flat": "it is flat", "strips": "stored in strips"} {
		_, err := Open(filepath.Join("testdata", name+".ome.tif"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Open(filepath.Join("testdata", "expected.json")); err == nil {
		t.Error("a JSON file opened as an image")
	}
}

// The alignment files Xenium Explorer and 10x's datasets name after an image, found beside it.
func TestSiblings(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"S1_he_image.ome.tif", "S1_he_imagealignment.csv", "S1_he_keypoints.csv", "S2_if_image.ome.tif", "other.csv"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	got := Siblings(filepath.Join(dir, "S1_he_image.ome.tif"))
	if len(got) != 2 || filepath.Base(got[0]) != "S1_he_imagealignment.csv" || filepath.Base(got[1]) != "S1_he_keypoints.csv" {
		t.Errorf("siblings %v", got)
	}
	if got := Siblings(filepath.Join(dir, "S2_if_image.ome.tif")); len(got) != 0 {
		t.Errorf("siblings of an image without them: %v", got)
	}
}

// What internal/tiff writes for the simulator's post-Xenium images reads back exactly: an RGB
// pyramid, its samples together, and 16-bit channels a directory each.
func TestWrittenImagesReadBack(t *testing.T) {
	dir := t.TempDir()
	rgb := &tiff.PlaneRGB{Width: 301, Height: 207, Pix: make([]uint8, 3*301*207)}
	for i := range rgb.Pix {
		rgb.Pix[i] = uint8((i*7 + i/903*13) % 251)
	}
	ome := func(sizeC int, typ, channels string) string {
		return `<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"><Image ID="Image:0"><Pixels ID="Pixels:0" DimensionOrder="XYCZT" Type="` + typ + `" SizeX="301" SizeY="207" SizeZ="1" SizeC="` + strconv.Itoa(sizeC) + `" SizeT="1" PhysicalSizeX="0.5" PhysicalSizeXUnit="µm" PhysicalSizeY="0.5" PhysicalSizeYUnit="µm">` + channels + `</Pixels></Image></OME>`
	}
	he := filepath.Join(dir, "he.ome.tif")
	if err := tiff.WriteRGBOMETIFF(he, rgb, tiff.WriteOptions{Description: ome(3, "uint8", `<Channel ID="Channel:0:0" SamplesPerPixel="3"/><TiffData IFD="0" PlaneCount="1"/>`), Tile: 64, Levels: 2}); err != nil {
		t.Fatal(err)
	}
	im, err := Open(he)
	if err != nil {
		t.Fatal(err)
	}
	want := rgb
	for l := range im.Levels {
		if l > 0 {
			want = want.Halve()
		}
		if got := level(t, im, l, 0); string(got) != string(want.Pix) || im.Kind != RGB || im.PixelSize != 0.5 {
			t.Fatalf("RGB level %d differs (%s, %v µm)", l, im.Kind, im.PixelSize)
		}
	}
	im.Close()

	planes := make([]*tiff.Plane16, 2)
	for c := range planes {
		planes[c] = &tiff.Plane16{Width: 301, Height: 207, Pix: make([]uint16, 301*207)}
		for i := range planes[c].Pix {
			planes[c].Pix[i] = uint16((i*7919 + c*40000) % 65536)
		}
	}
	fl := filepath.Join(dir, "if.ome.tif")
	if err := tiff.WriteChannelsOMETIFF(fl, planes, tiff.WriteOptions{Description: ome(2, "uint16", `<Channel ID="Channel:0:0" Name="DAPI" SamplesPerPixel="1"/><Channel ID="Channel:0:1" Name="Membrane" SamplesPerPixel="1"/><TiffData IFD="0" FirstC="0" PlaneCount="1"/><TiffData IFD="1" FirstC="1" PlaneCount="1"/>`), Tile: 64, Levels: 2}); err != nil {
		t.Fatal(err)
	}
	im, err = Open(fl)
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	if im.Kind != Channels || len(im.Channels) != 2 || im.Channels[1].Name != "Membrane" || len(im.Levels) != 3 {
		t.Fatalf("channels: %+v", im.Description)
	}
	for c, p := range planes {
		for l := range im.Levels {
			if l > 0 {
				p = p.Halve()
			}
			got := level(t, im, l, c)
			for i, v := range p.Pix {
				if g := uint16(got[2*i]) | uint16(got[2*i+1])<<8; g != v {
					t.Fatalf("channel %d level %d pixel %d: %d, written %d", c, l, i, g, v)
				}
			}
		}
	}
}
