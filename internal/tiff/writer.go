package tiff

// Writing images as tiled OME-TIFFs, laid out as Xenium lays out its morphology images: the full
// resolution in the first directory, with the OME-XML description, and its reduced resolutions
// (each half the last, by averaging) in SubIFDs. Three kinds: a single 16-bit plane (a stain), 16-bit
// channels of one size, a directory each in the main chain (an immunofluorescence image, as tifffile
// writes one), and 8-bit RGB, its samples together (an H&E, as Bio-Formats writes one). Tiles are
// compressed with deflate (compression 8) after the horizontal differencing predictor (predictor 2),
// which every TIFF reader decodes; the pixels come back exactly. Written from the TIFF 6.0
// specification, its Technical Note 1 (SubIFDs) and the OME-TIFF specification. Classic TIFF,
// little-endian: a file below 4 GB.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

// Plane16 is a single-channel 16-bit image, row by row.
type Plane16 struct {
	Width, Height int
	Pix           []uint16
}

// Halve is the plane at half the size (rounded up), each pixel the mean of up to four.
func (p *Plane16) Halve() *Plane16 {
	w, h := (p.Width+1)/2, (p.Height+1)/2
	out := &Plane16{Width: w, Height: h, Pix: make([]uint16, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum, n uint32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					if sx, sy := 2*x+dx, 2*y+dy; sx < p.Width && sy < p.Height {
						sum += uint32(p.Pix[sy*p.Width+sx])
						n++
					}
				}
			}
			out.Pix[y*w+x] = uint16((sum + n/2) / n)
		}
	}
	return out
}

// PlaneRGB is an 8-bit RGB image, row by row, each pixel's red, green and blue together.
type PlaneRGB struct {
	Width, Height int
	Pix           []uint8
}

// Halve is the image at half the size (rounded up), each sample the mean of up to four.
func (p *PlaneRGB) Halve() *PlaneRGB {
	w, h := (p.Width+1)/2, (p.Height+1)/2
	out := &PlaneRGB{Width: w, Height: h, Pix: make([]uint8, 3*w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				var sum, n uint32
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						if sx, sy := 2*x+dx, 2*y+dy; sx < p.Width && sy < p.Height {
							sum += uint32(p.Pix[3*(sy*p.Width+sx)+c])
							n++
						}
					}
				}
				out.Pix[3*(y*w+x)+c] = uint8((sum + n/2) / n)
			}
		}
	}
	return out
}

// WriteOptions are what a written TIFF says of itself.
type WriteOptions struct {
	// Description is the first directory's ImageDescription (the OME-XML).
	Description string
	Software    string
	// Tile is the tiles' side (a multiple of 16; 512 if 0).
	Tile int
	// Levels are the reduced resolutions written as SubIFDs (each half the last).
	Levels int
}

type ifdEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	data  []byte // the value, little-endian (in the entry when 4 bytes or fewer)
}

func shortEntry(tag uint16, v ...uint16) ifdEntry {
	var b []byte
	for _, x := range v {
		b = binary.LittleEndian.AppendUint16(b, x)
	}
	return ifdEntry{tag, typeShort, uint32(len(v)), b}
}

func longEntry(tag uint16, v ...uint32) ifdEntry {
	var b []byte
	for _, x := range v {
		b = binary.LittleEndian.AppendUint32(b, x)
	}
	return ifdEntry{tag, typeLong, uint32(len(v)), b}
}

func asciiEntry(tag uint16, s string) ifdEntry {
	b := append([]byte(s), 0)
	return ifdEntry{tag, typeASCII, uint32(len(b)), b}
}

type countingWriter struct {
	w   io.Writer
	pos int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.pos += int64(n)
	return n, err
}

// WriteOMETIFF writes the plane, and its reduced levels, to path.
func WriteOMETIFF(path string, p *Plane16, opt WriteOptions) error {
	return WriteChannelsOMETIFF(path, []*Plane16{p}, opt)
}

// WriteChannelsOMETIFF writes planes of one size, each a channel with its reduced levels, a
// directory each in the main chain (the OME-XML's TiffData should give each channel's).
func WriteChannelsOMETIFF(path string, planes []*Plane16, opt WriteOptions) error {
	if len(planes) == 0 {
		return errors.New("tiff: no plane")
	}
	for _, p := range planes {
		if p.Width <= 0 || p.Height <= 0 || len(p.Pix) != p.Width*p.Height || p.Width != planes[0].Width || p.Height != planes[0].Height {
			return errors.New("tiff: planes of one size")
		}
	}
	w, err := newWriter(path, planes[0].Width, planes[0].Height, len(planes), 1, 16, opt)
	if err != nil {
		return err
	}
	for c, p := range planes {
		levels := []*Plane16{p}
		for i := 0; i < opt.Levels; i++ {
			levels = append(levels, levels[len(levels)-1].Halve())
		}
		for level, pl := range levels {
			if err := w.eachTile(level, func(tx, ty int) ([]byte, error) {
				return tileBlockAt(pl, tx*w.opt.Tile, ty*w.opt.Tile, w.opt.Tile, pl.Width, pl.Height)
			}, c); err != nil {
				w.Abort()
				return err
			}
		}
	}
	return w.Close()
}

// WriteRGBOMETIFF writes an RGB image, and its reduced levels, to path.
func WriteRGBOMETIFF(path string, p *PlaneRGB, opt WriteOptions) error {
	if p.Width <= 0 || p.Height <= 0 || len(p.Pix) != 3*p.Width*p.Height {
		return errors.New("tiff: an image of its size")
	}
	w, err := newWriter(path, p.Width, p.Height, 1, 3, 8, opt)
	if err != nil {
		return err
	}
	levels := []*PlaneRGB{p}
	for i := 0; i < opt.Levels; i++ {
		levels = append(levels, levels[len(levels)-1].Halve())
	}
	for level, pl := range levels {
		if err := w.eachTile(level, func(tx, ty int) ([]byte, error) {
			return rgbTileBlock(pl, tx*w.opt.Tile, ty*w.opt.Tile, w.opt.Tile)
		}, 0); err != nil {
			w.Abort()
			return err
		}
	}
	return w.Close()
}

// eachTile writes every tile of a level of a channel, each block made by block.
func (w *TiledWriter) eachTile(level int, block func(tx, ty int) ([]byte, error), channel int) error {
	lw, lh := w.Size(level)
	tile := w.opt.Tile
	for ty := 0; ty < (lh+tile-1)/tile; ty++ {
		for tx := 0; tx < (lw+tile-1)/tile; tx++ {
			b, err := block(tx, ty)
			if err != nil {
				return err
			}
			if err := w.writeBlock(channel, level, tx, ty, b); err != nil {
				return err
			}
		}
	}
	return nil
}

// TiledWriter writes an OME-TIFF a tile at a time, in any order: an image too large to hold whole
// (a large simulated tissue) is written as its parts are made. Each level is half the last's size
// (rounded up), as Halve makes it; every tile of every level must be written before Close.
type TiledWriter struct {
	f   *os.File
	cw  *countingWriter
	opt WriteOptions
	// samples per pixel and bits per sample: 1 and 16 (a plane), 3 and 8 (RGB).
	samples, bits int
	sizes         [][2]int
	tiles         [][][]tileAt // per channel, per level
	closed        bool
}

type tileAt struct{ offset, count uint32 }

// NewTiledWriter starts an OME-TIFF of a width × height 16-bit plane, with opt.Levels reduced
// levels.
func NewTiledWriter(path string, width, height int, opt WriteOptions) (*TiledWriter, error) {
	return newWriter(path, width, height, 1, 1, 16, opt)
}

func newWriter(path string, width, height, channels, samples, bits int, opt WriteOptions) (*TiledWriter, error) {
	if opt.Tile == 0 {
		opt.Tile = 512
	}
	if opt.Tile%16 != 0 || width <= 0 || height <= 0 {
		return nil, errors.New("tiff: a plane of its size, and tiles a multiple of 16")
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &TiledWriter{f: f, cw: &countingWriter{w: f}, opt: opt, samples: samples, bits: bits}
	// The header; the first directory's offset is patched at the end.
	if _, err := w.cw.Write([]byte{'I', 'I', 42, 0, 0, 0, 0, 0}); err != nil {
		w.Abort()
		return nil, err
	}
	w.tiles = make([][][]tileAt, channels)
	for level := 0; level <= opt.Levels; level++ {
		w.sizes = append(w.sizes, [2]int{width, height})
		n := ((width + opt.Tile - 1) / opt.Tile) * ((height + opt.Tile - 1) / opt.Tile)
		for c := range w.tiles {
			w.tiles[c] = append(w.tiles[c], make([]tileAt, n))
		}
		width, height = (width+1)/2, (height+1)/2
	}
	return w, nil
}

// Size is a level's width and height.
func (w *TiledWriter) Size(level int) (int, int) { return w.sizes[level][0], w.sizes[level][1] }

// WriteTile writes tile (tx, ty) of a level, its pixels read from the level's whole plane.
func (w *TiledWriter) WriteTile(level, tx, ty int, pl *Plane16) error {
	return w.WriteTileAt(level, tx, ty, pl, 0, 0)
}

// WriteTileAt writes tile (tx, ty) of a level, its pixels read from pl, whose pixel (0, 0) is the
// level's (ox, oy); pixels beyond the level are zero.
func (w *TiledWriter) WriteTileAt(level, tx, ty int, pl *Plane16, ox, oy int) error {
	tile := w.opt.Tile
	lw, lh := w.Size(level)
	across := (lw + tile - 1) / tile
	if tx < 0 || ty < 0 || tx >= across || ty*tile >= lh {
		return fmt.Errorf("tiff: no tile (%d, %d) at level %d", tx, ty, level)
	}
	block, err := tileBlockAt(pl, tx*tile-ox, ty*tile-oy, tile, lw-ox, lh-oy)
	if err != nil {
		return err
	}
	return w.writeBlock(0, level, tx, ty, block)
}

// writeBlock writes a tile's compressed block.
func (w *TiledWriter) writeBlock(channel, level, tx, ty int, block []byte) error {
	lw, _ := w.Size(level)
	across := (lw + w.opt.Tile - 1) / w.opt.Tile
	if w.cw.pos > 1<<32-int64(len(block))-1 {
		return errors.New("tiff: the image is too large for a classic TIFF")
	}
	w.tiles[channel][level][ty*across+tx] = tileAt{uint32(w.cw.pos), uint32(len(block))}
	_, err := w.cw.Write(block)
	return err
}

// Abort removes the file.
func (w *TiledWriter) Abort() {
	if w.closed {
		return
	}
	w.closed = true
	w.f.Close()
	os.Remove(w.f.Name())
}

// Close writes the directories: each channel's reduced levels, then its full resolution (their
// offsets in its SubIFDs), the last channel's first so that each names the next in the main chain.
func (w *TiledWriter) Close() error {
	if w.closed {
		return errors.New("tiff: closed")
	}
	for c, levels := range w.tiles {
		for level, ts := range levels {
			for i, t := range ts {
				if t.count == 0 {
					w.Abort()
					return fmt.Errorf("tiff: tile %d of level %d of channel %d not written", i, level, c)
				}
			}
		}
	}
	bits := make([]uint16, w.samples)
	formats := make([]uint16, w.samples)
	for i := range bits {
		bits[i], formats[i] = uint16(w.bits), 1
	}
	photometric := uint16(1) // black is zero
	if w.samples == 3 {
		photometric = 2 // RGB
	}
	entries := func(c, i int) []ifdEntry {
		width, height := w.Size(i)
		subfile := uint32(0)
		if i > 0 {
			subfile = 1
		}
		tiles := w.tiles[c][i]
		offsets, counts := make([]uint32, len(tiles)), make([]uint32, len(tiles))
		for k, t := range tiles {
			offsets[k], counts[k] = t.offset, t.count
		}
		e := []ifdEntry{
			longEntry(254, subfile),
			longEntry(256, uint32(width)),
			longEntry(257, uint32(height)),
			shortEntry(258, bits...),
			shortEntry(259, CompressionDeflate),
			shortEntry(262, photometric),
			shortEntry(277, uint16(w.samples)),
			shortEntry(284, 1),
			shortEntry(317, 2),
			longEntry(322, uint32(w.opt.Tile)),
			longEntry(323, uint32(w.opt.Tile)),
			longEntry(324, offsets...),
			longEntry(325, counts...),
			shortEntry(339, formats...),
		}
		if w.opt.Software != "" {
			e = append(e, asciiEntry(305, w.opt.Software))
		}
		if c == 0 && i == 0 && w.opt.Description != "" {
			e = append(e, asciiEntry(270, w.opt.Description))
		}
		return e
	}
	next := uint32(0)
	for c := len(w.tiles) - 1; c >= 0; c-- {
		subOffsets := make([]uint32, 0, len(w.sizes)-1)
		for i := 1; i < len(w.sizes); i++ {
			off, err := writeIFD(w.cw, entries(c, i), 0)
			if err != nil {
				w.Abort()
				return err
			}
			subOffsets = append(subOffsets, off)
		}
		main := entries(c, 0)
		if len(subOffsets) > 0 {
			main = append(main, longEntry(330, subOffsets...))
		}
		off, err := writeIFD(w.cw, main, next)
		if err != nil {
			w.Abort()
			return err
		}
		next = off
	}
	off := next
	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], off)
	if _, err := w.f.WriteAt(head[:], 4); err != nil {
		w.Abort()
		return err
	}
	w.closed = true
	return w.f.Close()
}

// tileBlockAt is the tile whose top-left pixel is pl's (x0, y0), padded with zeros beyond pl and
// beyond (w, h) in pl's pixels (the level's edge), after the differencing predictor, deflated.
func tileBlockAt(p *Plane16, x0, y0, tile, w, h int) ([]byte, error) {
	raw := make([]byte, 2*tile*tile)
	for y := 0; y < tile; y++ {
		sy := y0 + y
		var prev uint16
		for x := 0; x < tile; x++ {
			sx := x0 + x
			var v uint16
			if sx >= 0 && sy >= 0 && sx < p.Width && sy < p.Height && sx < w && sy < h {
				v = p.Pix[sy*p.Width+sx]
			}
			binary.LittleEndian.PutUint16(raw[2*(y*tile+x):], v-prev)
			prev = v
		}
	}
	var buf bytes.Buffer
	z, err := zlib.NewWriterLevel(&buf, zlib.DefaultCompression)
	if err != nil {
		return nil, err
	}
	z.Write(raw)
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rgbTileBlock is the tile of an RGB image whose top-left pixel is (x0, y0), padded with zeros beyond
// it, after the differencing predictor (each sample less the same sample of the pixel before),
// deflated.
func rgbTileBlock(p *PlaneRGB, x0, y0, tile int) ([]byte, error) {
	raw := make([]byte, 3*tile*tile)
	for y := 0; y < tile; y++ {
		sy := y0 + y
		var prev [3]uint8
		for x := 0; x < tile; x++ {
			sx := x0 + x
			for c := 0; c < 3; c++ {
				var v uint8
				if sx < p.Width && sy < p.Height {
					v = p.Pix[3*(sy*p.Width+sx)+c]
				}
				raw[3*(y*tile+x)+c] = v - prev[c]
				prev[c] = v
			}
		}
	}
	var buf bytes.Buffer
	z, err := zlib.NewWriterLevel(&buf, zlib.DefaultCompression)
	if err != nil {
		return nil, err
	}
	z.Write(raw)
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeIFD writes a directory's large values, then the directory (word-aligned), returning its
// offset; next is the next directory's in the main chain (0 for none).
func writeIFD(cw *countingWriter, entries []ifdEntry, next uint32) (uint32, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].tag < entries[j].tag })
	valueAt := make([]uint32, len(entries))
	for i, e := range entries {
		if len(e.data) > 4 {
			if cw.pos%2 == 1 {
				cw.Write([]byte{0})
			}
			valueAt[i] = uint32(cw.pos)
			if _, err := cw.Write(e.data); err != nil {
				return 0, err
			}
		}
	}
	if cw.pos%2 == 1 {
		cw.Write([]byte{0})
	}
	if cw.pos > 1<<32-1 {
		return 0, fmt.Errorf("tiff: the file passes 4 GB")
	}
	at := uint32(cw.pos)
	b := binary.LittleEndian.AppendUint16(nil, uint16(len(entries)))
	for i, e := range entries {
		b = binary.LittleEndian.AppendUint16(b, e.tag)
		b = binary.LittleEndian.AppendUint16(b, e.typ)
		b = binary.LittleEndian.AppendUint32(b, e.count)
		if len(e.data) > 4 {
			b = binary.LittleEndian.AppendUint32(b, valueAt[i])
		} else {
			var v [4]byte
			copy(v[:], e.data)
			b = append(b, v[:]...)
		}
	}
	b = binary.LittleEndian.AppendUint32(b, next)
	_, err := cw.Write(b)
	return at, err
}
