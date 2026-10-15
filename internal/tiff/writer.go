package tiff

// Writing a single-plane 16-bit image as a tiled OME-TIFF, laid out as Xenium lays out its
// morphology images: the full resolution in the first directory, with the OME-XML description,
// and its reduced resolutions (each half the last, by averaging) in SubIFDs. Tiles are compressed
// with deflate (compression 8) after the horizontal differencing predictor (predictor 2), which
// every TIFF reader decodes; the pixels come back exactly. Written from the TIFF 6.0
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

func shortEntry(tag uint16, v uint16) ifdEntry {
	return ifdEntry{tag, typeShort, 1, binary.LittleEndian.AppendUint16(nil, v)}
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
	if opt.Tile == 0 {
		opt.Tile = 512
	}
	if opt.Tile%16 != 0 || p.Width <= 0 || p.Height <= 0 || len(p.Pix) != p.Width*p.Height {
		return errors.New("tiff: a plane of its size, and tiles a multiple of 16")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	cw := &countingWriter{w: f}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	// The header; the first directory's offset is patched at the end.
	if _, err := cw.Write([]byte{'I', 'I', 42, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	planes := []*Plane16{p}
	for i := 0; i < opt.Levels; i++ {
		planes = append(planes, planes[len(planes)-1].Halve())
	}
	type written struct {
		offsets, counts []uint32
	}
	var tiles []written
	for _, pl := range planes {
		var t written
		for ty := 0; ty < (pl.Height+opt.Tile-1)/opt.Tile; ty++ {
			for tx := 0; tx < (pl.Width+opt.Tile-1)/opt.Tile; tx++ {
				block, err := tileBlock(pl, tx, ty, opt.Tile)
				if err != nil {
					return err
				}
				if cw.pos > 1<<32-int64(len(block))-1 {
					return errors.New("tiff: the image is too large for a classic TIFF")
				}
				t.offsets = append(t.offsets, uint32(cw.pos))
				t.counts = append(t.counts, uint32(len(block)))
				if _, err := cw.Write(block); err != nil {
					return err
				}
			}
		}
		tiles = append(tiles, t)
	}
	// The directories: the reduced levels first (their offsets go in the first's SubIFDs).
	entries := func(i int) []ifdEntry {
		pl := planes[i]
		subfile := uint32(0)
		if i > 0 {
			subfile = 1
		}
		e := []ifdEntry{
			longEntry(254, subfile),
			longEntry(256, uint32(pl.Width)),
			longEntry(257, uint32(pl.Height)),
			shortEntry(258, 16),
			shortEntry(259, CompressionDeflate),
			shortEntry(262, 1),
			shortEntry(277, 1),
			shortEntry(284, 1),
			shortEntry(317, 2),
			longEntry(322, uint32(opt.Tile)),
			longEntry(323, uint32(opt.Tile)),
			longEntry(324, tiles[i].offsets...),
			longEntry(325, tiles[i].counts...),
			shortEntry(339, 1),
		}
		if opt.Software != "" {
			e = append(e, asciiEntry(305, opt.Software))
		}
		if i == 0 && opt.Description != "" {
			e = append(e, asciiEntry(270, opt.Description))
		}
		return e
	}
	subOffsets := make([]uint32, 0, len(planes)-1)
	for i := 1; i < len(planes); i++ {
		off, err := writeIFD(cw, entries(i))
		if err != nil {
			return err
		}
		subOffsets = append(subOffsets, off)
	}
	first := entries(0)
	if len(subOffsets) > 0 {
		first = append(first, longEntry(330, subOffsets...))
	}
	off, err := writeIFD(cw, first)
	if err != nil {
		return err
	}
	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], off)
	if _, err := f.WriteAt(head[:], 4); err != nil {
		return err
	}
	ok = true
	return f.Close()
}

// tileBlock is tile (tx, ty) of a plane, padded with zeros, after the differencing predictor,
// deflated.
func tileBlock(p *Plane16, tx, ty, tile int) ([]byte, error) {
	raw := make([]byte, 2*tile*tile)
	for y := 0; y < tile; y++ {
		sy := ty*tile + y
		var prev uint16
		for x := 0; x < tile; x++ {
			sx := tx*tile + x
			var v uint16
			if sx < p.Width && sy < p.Height {
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

// writeIFD writes a directory's large values, then the directory (word-aligned), returning its
// offset. The next-directory offset is 0: one image per file.
func writeIFD(cw *countingWriter, entries []ifdEntry) (uint32, error) {
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
	b = binary.LittleEndian.AppendUint32(b, 0)
	_, err := cw.Write(b)
	return at, err
}
