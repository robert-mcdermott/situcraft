// Package slide reads post-Xenium images: H&E and immunofluorescence images of the section,
// stained and scanned after the Xenium run, as pyramidal, tiled OME-TIFFs (the form Xenium
// Explorer requires; 10x Genomics, Xenium Explorer, "Image file conversion"). An image is read where
// it lies, never copied, a tile at a time: its levels are the full-resolution image directory and
// its SubIFDs, each tiled; an image is either RGB (an H&E, three samples per pixel, 8 bits) or one
// or more channels (an immunofluorescence image, a directory per channel, 8 or 16 bits); its tiles
// are deflate (with or without a predictor), LZW, JPEG, JPEG 2000 or uncompressed (internal/tiff).
//
// What an image's metadata says of its pixels' size is kept as it says, but an image is placed by
// its alignment, never by it: post-Xenium images are often written with the Xenium image's pixel
// size whatever theirs.
package slide

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"situcraft/internal/tiff"
)

// Kinds of image.
const (
	RGB      = "rgb"      // three samples per pixel, as an H&E
	Channels = "channels" // one or more channels, each its own plane, as an immunofluorescence image
)

// smallestUseful is the size below which a pyramid's smallest level need not go: a level this size
// shows a whole section on a screen.
const smallestUseful = 4096

// Level is one resolution of an image, full resolution first.
type Level struct {
	Width      int `json:"width"`
	Height     int `json:"height"`
	TileWidth  int `json:"tileWidth"`
	TileHeight int `json:"tileHeight"`
	Across     int `json:"across"`
	Down       int `json:"down"`
}

// Channel is one channel of an image (an RGB image has one, its colours).
type Channel struct {
	Name string `json:"name"`
}

// Description is what an image is.
type Description struct {
	Kind        string `json:"kind"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bits        int    `json:"bits"`
	Compression string `json:"compression"`
	// PixelSize is the pixels' size in µm as the image's metadata says (0 when it says none).
	PixelSize float64   `json:"pixelSize,omitempty"`
	Channels  []Channel `json:"channels"`
	Levels    []Level   `json:"levels"`
	// Notes say what of the image is not shown (planes beyond the first z or time point).
	Notes []string `json:"notes,omitempty"`
}

// Image is an open post-Xenium image.
type Image struct {
	Description
	Path   string
	f      *os.File
	file   *tiff.File
	planes [][]*tiff.IFD // per channel (one for RGB): its levels' directories, full resolution first
}

// Open opens and checks an image. It refuses what it cannot show, saying why.
func Open(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	im, err := open(f, info.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	im.Path, im.f = path, f
	return im, nil
}

func open(f *os.File, size int64) (*Image, error) {
	tf, err := tiff.Open(f, size)
	if err != nil {
		return nil, err
	}
	if len(tf.IFDs) == 0 {
		return nil, errors.New("the TIFF holds no image")
	}
	im := &Image{file: tf}
	first := tf.IFDs[0]
	var ome *tiff.OME
	if strings.Contains(first.Description, "<OME") {
		if ome, err = tiff.ParseOME(first.Description); err != nil {
			return nil, err
		}
		if len(ome.Images) == 0 {
			return nil, errors.New("its OME-XML describes no image")
		}
	}
	// The planes: an RGB image is one directory; channels, a directory each (the OME planes of the
	// first z and time point, else the main chain's full-resolution directories).
	var tops []*tiff.IFD
	var names []string
	switch {
	case first.SamplesPerPixel == 3:
		if first.PlanarConfig == 2 {
			return nil, errors.New("its colours are stored as separate planes; SituCraft reads them stored together (as Bio-Formats and tifffile write H&E)")
		}
		if b := first.BitsPerSample[0]; b != 8 {
			return nil, fmt.Errorf("an RGB image of %d bits per sample; H&E images are 8", b)
		}
		im.Kind, tops, names = RGB, []*tiff.IFD{first}, []string{"RGB"}
	case first.SamplesPerPixel == 1:
		im.Kind = Channels
		if ome != nil {
			px := &ome.Images[0].Pixels
			planes, err := px.Planes(ome.UUID)
			if err != nil {
				return nil, err
			}
			byC := map[int]tiff.Plane{}
			for _, p := range planes {
				if p.Z == 0 && p.T == 0 {
					byC[p.C] = p
				}
			}
			if px.SizeZ > 1 || px.SizeT > 1 {
				im.Notes = append(im.Notes, fmt.Sprintf("Its %d z-planes and %d time points: the first of each is shown.", px.SizeZ, px.SizeT))
			}
			for c := 0; c < px.SizeC; c++ {
				p, ok := byC[c]
				if !ok {
					return nil, fmt.Errorf("its channel %d has no plane", c)
				}
				if p.FileName != "" {
					return nil, fmt.Errorf("its channel %d is in another file (%s); SituCraft reads single-file images", c, p.FileName)
				}
				if p.IFD >= len(tf.IFDs) {
					return nil, fmt.Errorf("its channel %d is in image directory %d of %d", c, p.IFD, len(tf.IFDs))
				}
				tops = append(tops, tf.IFDs[p.IFD])
				name := fmt.Sprintf("Channel %d", c+1)
				if c < len(px.Channels) && px.Channels[c].Name != "" {
					name = px.Channels[c].Name
				}
				names = append(names, name)
			}
		} else {
			for i, ifd := range tf.IFDs {
				if ifd.NewSubfileType&1 != 0 {
					continue // a reduced image in the main chain, not a channel
				}
				tops = append(tops, ifd)
				names = append(names, fmt.Sprintf("Channel %d", i+1))
			}
		}
	default:
		return nil, fmt.Errorf("%d samples per pixel; SituCraft reads RGB images and images of one sample per channel", first.SamplesPerPixel)
	}
	for c, top := range tops {
		levels := append([]*tiff.IFD{top}, top.SubIFDs...)
		if err := im.checkLevels(c, levels); err != nil {
			return nil, err
		}
		im.planes = append(im.planes, levels)
		im.Channels = append(im.Channels, Channel{Name: names[c]})
	}
	if ome != nil {
		px := ome.Images[0].Pixels
		if px.PhysicalSizeX > 0 && (px.PhysicalSizeXUnit == "µm" || px.PhysicalSizeXUnit == "um") {
			im.PixelSize = px.PhysicalSizeX
		}
	}
	return im, nil
}

// checkLevels checks one channel's levels: tiled, of one kind of sample, each smaller than the one
// before, the same for every channel, down to a size a screen shows whole.
func (im *Image) checkLevels(c int, levels []*tiff.IFD) error {
	top := levels[0]
	if c == 0 {
		im.Width, im.Height, im.Bits = top.Width, top.Height, top.BitsPerSample[0]
		im.Compression = compressionName(top.Compression)
		if im.Bits != 8 && im.Bits != 16 {
			return fmt.Errorf("%d bits per sample; SituCraft reads 8 and 16", im.Bits)
		}
		if top.SampleFormat != 1 {
			return errors.New("its samples are not unsigned integers")
		}
	} else if top.Width != im.Width || top.Height != im.Height || top.BitsPerSample[0] != im.Bits {
		return fmt.Errorf("its channel %d is %d×%d of %d bits, its first %d×%d of %d", c+1, top.Width, top.Height, top.BitsPerSample[0], im.Width, im.Height, im.Bits)
	}
	if len(levels) == 1 && max(top.Width, top.Height) > smallestUseful {
		return fmt.Errorf("it is flat (%d×%d, no reduced levels); SituCraft shows pyramidal OME-TIFFs, as Xenium Explorer does: make one with bfconvert -pyramid-resolutions 6 -pyramid-scale 2 -tilex 1024 -tiley 1024, or with tifffile", top.Width, top.Height)
	}
	for l, ifd := range levels {
		if !ifd.Tiled {
			return fmt.Errorf("level %d is stored in strips; SituCraft reads tiled images, as Xenium Explorer does", l)
		}
		if l > 0 && (ifd.Width >= levels[l-1].Width || ifd.Height >= levels[l-1].Height) {
			return fmt.Errorf("level %d (%d×%d) is not smaller than the one before", l, ifd.Width, ifd.Height)
		}
		if ifd.SamplesPerPixel != top.SamplesPerPixel || ifd.BitsPerSample[0] != top.BitsPerSample[0] || ifd.PlanarConfig != top.PlanarConfig {
			return fmt.Errorf("level %d's samples differ from the full-resolution image's", l)
		}
		across, down := ifd.Blocks()
		lv := Level{Width: ifd.Width, Height: ifd.Height, TileWidth: ifd.BlockWidth, TileHeight: ifd.BlockHeight, Across: across, Down: down}
		if c == 0 {
			im.Levels = append(im.Levels, lv)
		} else if l >= len(im.Levels) || im.Levels[l] != lv {
			return fmt.Errorf("its channel %d's level %d differs from its first channel's", c+1, l)
		}
	}
	if c > 0 && len(levels) != len(im.Levels) {
		return fmt.Errorf("its channel %d has %d levels, its first %d", c+1, len(levels), len(im.Levels))
	}
	return nil
}

func compressionName(code int) string {
	switch code {
	case tiff.CompressionNone:
		return "none"
	case tiff.CompressionDeflate, tiff.CompressionAdobeZIP:
		return "deflate"
	case tiff.CompressionLZW:
		return "LZW"
	case tiff.CompressionJPEG:
		return "JPEG"
	case tiff.CompressionPackBits:
		return "PackBits"
	case tiff.CompressionJP2K, tiff.CompressionAperioJP2KYCbCr, tiff.CompressionAperioJP2KRGB:
		return "JPEG 2000"
	}
	return fmt.Sprintf("compression %d", code)
}

// Close releases the file.
func (im *Image) Close() error { return im.f.Close() }

// Tile decodes tile (tx, ty) of a level of a channel (0 for an RGB image): TileWidth × TileHeight
// pixels row-major, padded past the image's edges; an RGB pixel's three samples together; 16-bit
// samples little-endian.
func (im *Image) Tile(level, channel, tx, ty int) ([]byte, error) {
	if channel < 0 || channel >= len(im.planes) {
		return nil, fmt.Errorf("no channel %d", channel)
	}
	if level < 0 || level >= len(im.Levels) {
		return nil, fmt.Errorf("no level %d", level)
	}
	lv := im.Levels[level]
	if tx < 0 || ty < 0 || tx >= lv.Across || ty >= lv.Down {
		return nil, fmt.Errorf("no tile (%d, %d) at level %d", tx, ty, level)
	}
	return im.planes[channel][level].DecodeBlock(ty*lv.Across + tx)
}

// PixelBytes is the size of one decoded pixel.
func (im *Image) PixelBytes() int {
	if im.Kind == RGB {
		return 3
	}
	return im.Bits / 8
}

// Siblings lists the alignment files 10x Genomics names after an image, beside it:
// <stem>imagealignment.csv and <stem>keypoints.csv for an image <stem>image.ome.tif (also with
// "_alignment_files" folders as Xenium Explorer saves them), most likely first.
func Siblings(path string) []string {
	dir, base := filepath.Split(path)
	stem := base
	for _, suffix := range []string{".ome.tiff", ".ome.tif", ".tiff", ".tif"} {
		if strings.HasSuffix(strings.ToLower(stem), suffix) {
			stem = stem[:len(stem)-len(suffix)]
			break
		}
	}
	trimmed := strings.TrimSuffix(stem, "image")
	var out []string
	seen := map[string]bool{}
	for _, name := range []string{trimmed + "imagealignment.csv", stem + "_imagealignment.csv", trimmed + "keypoints.csv", stem + "_keypoints.csv"} {
		p := filepath.Join(dir, name)
		if seen[p] {
			continue
		}
		seen[p] = true
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return strings.HasSuffix(out[a], "imagealignment.csv") && !strings.HasSuffix(out[b], "imagealignment.csv")
	})
	return out
}
