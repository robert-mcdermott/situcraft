// Package tiff reads TIFF and BigTIFF files, tiled or stripped, with their pyramid levels
// (SubIFDs) and OME-XML descriptions: the container of Xenium's morphology images and of most
// whole-slide H&E and IF images. Written from the TIFF 6.0 specification (Adobe, 1992), the
// BigTIFF description (Aware Systems; 8-byte offsets, magic 43) and the OME-TIFF specification.
// Codecs are looked up by compression code: none, deflate, LZW and PackBits are built in; others
// (JPEG 2000, JPEG) are registered by the packages that implement them.
package tiff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

const (
	// Limits that no real file reaches; they stop a hostile one from allocating or looping.
	maxIFDs       = 100000
	maxEntries    = 10000
	maxBlocks     = 1 << 24
	maxBlockBytes = 1 << 30
	maxDimension  = 1 << 24
)

// Tag types (TIFF 6.0 §2, BigTIFF adds 16-18).
const (
	typeByte      = 1
	typeASCII     = 2
	typeShort     = 3
	typeLong      = 4
	typeRational  = 5
	typeSByte     = 6
	typeUndefined = 7
	typeSShort    = 8
	typeSLong     = 9
	typeSRational = 10
	typeFloat     = 11
	typeDouble    = 12
	typeIFD       = 13
	typeLong8     = 16
	typeSLong8    = 17
	typeIFD8      = 18
)

var typeSizes = map[uint16]int{
	typeByte: 1, typeASCII: 1, typeShort: 2, typeLong: 4, typeRational: 8, typeSByte: 1, typeUndefined: 1,
	typeSShort: 2, typeSLong: 4, typeSRational: 8, typeFloat: 4, typeDouble: 8, typeIFD: 4,
	typeLong8: 8, typeSLong8: 8, typeIFD8: 8,
}

// Tags SituCraft reads.
const (
	TagNewSubfileType  = 254
	TagImageWidth      = 256
	TagImageLength     = 257
	TagBitsPerSample   = 258
	TagCompression     = 259
	TagPhotometric     = 262
	TagDescription     = 270
	TagStripOffsets    = 273
	TagSamplesPerPixel = 277
	TagRowsPerStrip    = 278
	TagStripByteCounts = 279
	TagPlanarConfig    = 284
	TagSoftware        = 305
	TagPredictor       = 317
	TagTileWidth       = 322
	TagTileLength      = 323
	TagTileOffsets     = 324
	TagTileByteCounts  = 325
	TagSubIFDs         = 330
	TagSampleFormat    = 339
	TagJPEGTables      = 347
)

// Compression codes (TIFF 6.0 and registered extensions).
const (
	CompressionNone     = 1
	CompressionLZW      = 5
	CompressionJPEG     = 7
	CompressionDeflate  = 8
	CompressionPackBits = 32773
	CompressionAdobeZIP = 32946 // deflate, as Adobe registered it first
	CompressionJP2K     = 34712 // JPEG 2000 (as Xenium writes it)
)

// Tag is one directory entry: its type, count, and the bytes of its value.
type Tag struct {
	Type  uint16
	Count uint64
	Value []byte
}

// File is an open TIFF.
type File struct {
	BigTIFF   bool
	ByteOrder binary.ByteOrder
	// IFDs is the main chain of image directories (for an OME-TIFF, one per plane).
	IFDs []*IFD
	r    io.ReaderAt
	size int64
}

// IFD is one image directory.
type IFD struct {
	Offset          int64
	Width, Height   int
	BitsPerSample   []int
	SamplesPerPixel int
	Compression     int
	Photometric     int
	// PlanarConfig 1 stores a pixel's samples together, 2 each sample in its own planes.
	PlanarConfig int
	Predictor    int
	// SampleFormat 1 unsigned integer, 2 signed, 3 IEEE floating point.
	SampleFormat   int
	NewSubfileType uint32
	Tiled          bool
	// BlockWidth and BlockHeight are the tile size, or the image width and rows per strip.
	BlockWidth, BlockHeight int
	Offsets, ByteCounts     []int64
	Description             string
	Software                string
	JPEGTables              []byte
	// SubIFDs are the reduced-resolution levels of this image (a pyramid), largest first.
	SubIFDs []*IFD
	Tags    map[uint16]Tag
	file    *File
}

// A Decoder decompresses one tile or strip into its samples, in the file's byte order for samples
// wider than a byte (DecodeBlock makes them little-endian). width and height are the block's size.
type Decoder func(ifd *IFD, compressed []byte, width, height int) ([]byte, error)

var (
	decodersMu sync.RWMutex
	decoders   = map[int]Decoder{
		CompressionNone:     decodeNone,
		CompressionDeflate:  decodeDeflate,
		CompressionAdobeZIP: decodeDeflate,
		CompressionLZW:      decodeLZW,
		CompressionPackBits: decodePackBits,
	}
)

// Register makes a decoder available for a compression code (JPEG 2000 and JPEG are registered by
// the packages that read them).
func Register(compression int, d Decoder) {
	decodersMu.Lock()
	defer decodersMu.Unlock()
	decoders[compression] = d
}

// Open reads a TIFF's header and its image directories (with their SubIFDs).
func Open(r io.ReaderAt, size int64) (*File, error) {
	head := make([]byte, 16)
	if size < 8 {
		return nil, errors.New("tiff: the file is too short to be a TIFF")
	}
	n, _ := r.ReadAt(head, 0)
	if n < 8 {
		return nil, errors.New("tiff: the file is too short to be a TIFF")
	}
	f := &File{r: r, size: size}
	switch string(head[:2]) {
	case "II":
		f.ByteOrder = binary.LittleEndian
	case "MM":
		f.ByteOrder = binary.BigEndian
	default:
		return nil, errors.New("tiff: not a TIFF (no II or MM byte order mark)")
	}
	var first int64
	switch f.ByteOrder.Uint16(head[2:]) {
	case 42:
		first = int64(f.ByteOrder.Uint32(head[4:]))
	case 43:
		if n < 16 || f.ByteOrder.Uint16(head[4:]) != 8 {
			return nil, errors.New("tiff: BigTIFF header with an offset size other than 8")
		}
		f.BigTIFF = true
		first = int64(f.ByteOrder.Uint64(head[8:]))
	default:
		return nil, errors.New("tiff: not a TIFF (magic is neither 42 nor 43)")
	}
	visited := map[int64]bool{}
	for offset := first; offset != 0; {
		if len(f.IFDs) >= maxIFDs {
			return nil, errors.New("tiff: too many image directories")
		}
		ifd, next, err := f.readIFD(offset, visited, 0)
		if err != nil {
			return nil, err
		}
		f.IFDs = append(f.IFDs, ifd)
		offset = next
	}
	if len(f.IFDs) == 0 {
		return nil, errors.New("tiff: the file has no images")
	}
	return f, nil
}

func (f *File) readAt(n int, offset int64) ([]byte, error) {
	if offset < 0 || n < 0 || offset > f.size || int64(n) > f.size-offset {
		return nil, fmt.Errorf("tiff: %d bytes at %d lie outside the file", n, offset)
	}
	buf := make([]byte, n)
	if _, err := f.r.ReadAt(buf, offset); err != nil {
		return nil, err
	}
	return buf, nil
}

// readIFD reads the directory at offset, its SubIFDs (depth limits their nesting), and returns
// the offset of the next directory in its chain.
func (f *File) readIFD(offset int64, visited map[int64]bool, depth int) (*IFD, int64, error) {
	if visited[offset] {
		return nil, 0, fmt.Errorf("tiff: image directories loop back to %d", offset)
	}
	visited[offset] = true
	countSize, entrySize, offsetSize := 2, 12, 4
	if f.BigTIFF {
		countSize, entrySize, offsetSize = 8, 20, 8
	}
	head, err := f.readAt(countSize, offset)
	if err != nil {
		return nil, 0, err
	}
	var count uint64
	if f.BigTIFF {
		count = f.ByteOrder.Uint64(head)
	} else {
		count = uint64(f.ByteOrder.Uint16(head))
	}
	if count == 0 || count > maxEntries {
		return nil, 0, fmt.Errorf("tiff: image directory at %d has %d entries", offset, count)
	}
	body, err := f.readAt(int(count)*entrySize+offsetSize, offset+int64(countSize))
	if err != nil {
		return nil, 0, err
	}
	ifd := &IFD{Offset: offset, Tags: map[uint16]Tag{}, file: f}
	for i := 0; i < int(count); i++ {
		e := body[i*entrySize : (i+1)*entrySize]
		code := f.ByteOrder.Uint16(e)
		typ := f.ByteOrder.Uint16(e[2:])
		var n uint64
		var inline []byte
		if f.BigTIFF {
			n = f.ByteOrder.Uint64(e[4:])
			inline = e[12:20]
		} else {
			n = uint64(f.ByteOrder.Uint32(e[4:]))
			inline = e[8:12]
		}
		width, known := typeSizes[typ]
		if !known {
			continue // an unknown type: readers must skip it (TIFF 6.0 §2)
		}
		if n > uint64(f.size) {
			return nil, 0, fmt.Errorf("tiff: tag %d declares %d values", code, n)
		}
		total := int(n) * width
		var value []byte
		if total <= len(inline) {
			value = append([]byte(nil), inline[:total]...)
		} else {
			var at int64
			if f.BigTIFF {
				at = int64(f.ByteOrder.Uint64(inline))
			} else {
				at = int64(f.ByteOrder.Uint32(inline))
			}
			if value, err = f.readAt(total, at); err != nil {
				return nil, 0, fmt.Errorf("tiff: tag %d: %w", code, err)
			}
		}
		ifd.Tags[code] = Tag{Type: typ, Count: n, Value: value}
	}
	var next int64
	if f.BigTIFF {
		next = int64(f.ByteOrder.Uint64(body[count*20:]))
	} else {
		next = int64(f.ByteOrder.Uint32(body[count*12:]))
	}
	if err := ifd.interpret(); err != nil {
		return nil, 0, fmt.Errorf("tiff: image directory at %d: %w", offset, err)
	}
	if subs, ok := ifd.Tags[TagSubIFDs]; ok && depth < 4 {
		for _, sub := range f.ints(subs) {
			child, _, err := f.readIFD(int64(sub), visited, depth+1)
			if err != nil {
				return nil, 0, err
			}
			ifd.SubIFDs = append(ifd.SubIFDs, child)
		}
	}
	return ifd, next, nil
}

// ints reads an integer tag's values.
func (f *File) ints(t Tag) []uint64 {
	width := typeSizes[t.Type]
	out := make([]uint64, 0, t.Count)
	for i := 0; i+width <= len(t.Value); i += width {
		v := t.Value[i:]
		switch t.Type {
		case typeByte, typeSByte, typeUndefined:
			out = append(out, uint64(v[0]))
		case typeShort, typeSShort:
			out = append(out, uint64(f.ByteOrder.Uint16(v)))
		case typeLong, typeSLong, typeIFD:
			out = append(out, uint64(f.ByteOrder.Uint32(v)))
		case typeLong8, typeSLong8, typeIFD8:
			out = append(out, f.ByteOrder.Uint64(v))
		default:
			return out
		}
	}
	return out
}

// Ints is a tag's integer values (nil when absent or not an integer type).
func (ifd *IFD) Ints(code uint16) []uint64 {
	t, ok := ifd.Tags[code]
	if !ok {
		return nil
	}
	return ifd.file.ints(t)
}

// Floats is a rational or floating-point tag's values.
func (ifd *IFD) Floats(code uint16) []float64 {
	t, ok := ifd.Tags[code]
	if !ok {
		return nil
	}
	bo := ifd.file.ByteOrder
	var out []float64
	for i := 0; i+typeSizes[t.Type] <= len(t.Value); i += typeSizes[t.Type] {
		v := t.Value[i:]
		switch t.Type {
		case typeRational:
			out = append(out, float64(bo.Uint32(v))/float64(bo.Uint32(v[4:])))
		case typeSRational:
			out = append(out, float64(int32(bo.Uint32(v)))/float64(int32(bo.Uint32(v[4:]))))
		case typeFloat:
			out = append(out, float64(math.Float32frombits(bo.Uint32(v))))
		case typeDouble:
			out = append(out, math.Float64frombits(bo.Uint64(v)))
		default:
			for _, x := range ifd.file.ints(t) {
				out = append(out, float64(x))
			}
			return out
		}
	}
	return out
}

func (ifd *IFD) one(code uint16, fallback int) int {
	if v := ifd.Ints(code); len(v) > 0 {
		return int(v[0])
	}
	return fallback
}

func (ifd *IFD) interpret() error {
	ifd.Width = ifd.one(TagImageWidth, 0)
	ifd.Height = ifd.one(TagImageLength, 0)
	if ifd.Width <= 0 || ifd.Height <= 0 || ifd.Width > maxDimension || ifd.Height > maxDimension {
		return fmt.Errorf("image size %d×%d", ifd.Width, ifd.Height)
	}
	ifd.SamplesPerPixel = ifd.one(TagSamplesPerPixel, 1)
	if ifd.SamplesPerPixel <= 0 || ifd.SamplesPerPixel > 64 {
		return fmt.Errorf("%d samples per pixel", ifd.SamplesPerPixel)
	}
	for _, b := range ifd.Ints(TagBitsPerSample) {
		ifd.BitsPerSample = append(ifd.BitsPerSample, int(b))
	}
	if len(ifd.BitsPerSample) == 0 {
		ifd.BitsPerSample = []int{1}
	}
	for len(ifd.BitsPerSample) < ifd.SamplesPerPixel {
		ifd.BitsPerSample = append(ifd.BitsPerSample, ifd.BitsPerSample[0])
	}
	ifd.Compression = ifd.one(TagCompression, CompressionNone)
	ifd.Photometric = ifd.one(TagPhotometric, -1)
	ifd.PlanarConfig = ifd.one(TagPlanarConfig, 1)
	ifd.Predictor = ifd.one(TagPredictor, 1)
	ifd.SampleFormat = ifd.one(TagSampleFormat, 1)
	ifd.NewSubfileType = uint32(ifd.one(TagNewSubfileType, 0))
	if t, ok := ifd.Tags[TagDescription]; ok {
		ifd.Description = cString(t.Value)
	}
	if t, ok := ifd.Tags[TagSoftware]; ok {
		ifd.Software = cString(t.Value)
	}
	if t, ok := ifd.Tags[TagJPEGTables]; ok {
		ifd.JPEGTables = t.Value
	}
	offsets, counts := ifd.Ints(TagTileOffsets), ifd.Ints(TagTileByteCounts)
	if offsets != nil {
		ifd.Tiled = true
		ifd.BlockWidth = ifd.one(TagTileWidth, 0)
		ifd.BlockHeight = ifd.one(TagTileLength, 0)
		if ifd.BlockWidth <= 0 || ifd.BlockHeight <= 0 || ifd.BlockWidth > maxDimension || ifd.BlockHeight > maxDimension {
			return fmt.Errorf("tile size %d×%d", ifd.BlockWidth, ifd.BlockHeight)
		}
	} else {
		offsets, counts = ifd.Ints(TagStripOffsets), ifd.Ints(TagStripByteCounts)
		ifd.BlockWidth = ifd.Width
		ifd.BlockHeight = ifd.one(TagRowsPerStrip, ifd.Height)
		if ifd.BlockHeight <= 0 || ifd.BlockHeight > ifd.Height {
			ifd.BlockHeight = ifd.Height
		}
	}
	across, down := ifd.Blocks()
	want := across * down
	if ifd.PlanarConfig == 2 {
		want *= ifd.SamplesPerPixel
	}
	if want > maxBlocks {
		return fmt.Errorf("%d tiles or strips", want)
	}
	if len(offsets) != want || len(counts) != want {
		return fmt.Errorf("%d tile or strip offsets and %d byte counts for %d blocks", len(offsets), len(counts), want)
	}
	ifd.Offsets = make([]int64, want)
	ifd.ByteCounts = make([]int64, want)
	for i := range offsets {
		ifd.Offsets[i] = int64(offsets[i])
		ifd.ByteCounts[i] = int64(counts[i])
	}
	return nil
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// Blocks is the number of tiles (or strips) across and down one plane.
func (ifd *IFD) Blocks() (across, down int) {
	return (ifd.Width + ifd.BlockWidth - 1) / ifd.BlockWidth, (ifd.Height + ifd.BlockHeight - 1) / ifd.BlockHeight
}

// RawBlock is the stored (compressed) bytes of tile or strip i.
func (ifd *IFD) RawBlock(i int) ([]byte, error) {
	if i < 0 || i >= len(ifd.Offsets) {
		return nil, fmt.Errorf("tiff: no tile or strip %d", i)
	}
	if ifd.ByteCounts[i] > maxBlockBytes {
		return nil, fmt.Errorf("tiff: tile or strip %d declares %d bytes", i, ifd.ByteCounts[i])
	}
	return ifd.file.readAt(int(ifd.ByteCounts[i]), ifd.Offsets[i])
}

// BlockSize is the size in pixels of tile or strip i (strips at the bottom are shorter; tiles are
// always whole, padded past the image's edges).
func (ifd *IFD) BlockSize(i int) (width, height int) {
	if ifd.Tiled {
		return ifd.BlockWidth, ifd.BlockHeight
	}
	_, down := ifd.Blocks()
	row := i % down
	height = ifd.BlockHeight
	if rest := ifd.Height - row*ifd.BlockHeight; rest < height {
		height = rest
	}
	return ifd.Width, height
}

// DecodeBlock decompresses tile or strip i, undoes its predictor, and returns its samples
// row-major with samples wider than a byte in little-endian order.
func (ifd *IFD) DecodeBlock(i int) ([]byte, error) {
	raw, err := ifd.RawBlock(i)
	if err != nil {
		return nil, err
	}
	decodersMu.RLock()
	decode := decoders[ifd.Compression]
	decodersMu.RUnlock()
	if decode == nil {
		return nil, fmt.Errorf("tiff: compression %d is not read", ifd.Compression)
	}
	width, height := ifd.BlockSize(i)
	// A declared tile size, not the data, sets what a codec allocates: check it first.
	if bytes := int64(width) * int64(height) * int64(ifd.samples()) * int64(ifd.BitsPerSample[0]) / 8; bytes > maxBlockBytes || bytes <= 0 {
		return nil, fmt.Errorf("tiff: a %d×%d tile or strip is too large to decode", width, height)
	}
	out, err := decode(ifd, raw, width, height)
	if err != nil {
		return nil, fmt.Errorf("tiff: tile or strip %d: %w", i, err)
	}
	return ifd.finish(out, width, height)
}

// samples is the number of samples per pixel in one block (1 when each sample has its own planes).
func (ifd *IFD) samples() int {
	if ifd.PlanarConfig == 2 {
		return 1
	}
	return ifd.SamplesPerPixel
}

// BlockBytes is the decoded size of a block of this width and height.
func (ifd *IFD) BlockBytes(width, height int) int {
	return width * height * ifd.samples() * ifd.BitsPerSample[0] / 8
}

// finish checks a decoded block's size, undoes the predictor and makes samples little-endian.
func (ifd *IFD) finish(out []byte, width, height int) ([]byte, error) {
	bits := ifd.BitsPerSample[0]
	if bits%8 != 0 && bits != 1 {
		return nil, fmt.Errorf("tiff: %d bits per sample is not read", bits)
	}
	rowBytes := (width*ifd.samples()*bits + 7) / 8
	want := rowBytes * height
	if len(out) < want {
		return nil, fmt.Errorf("tiff: block decodes to %d bytes, not the %d expected", len(out), want)
	}
	out = out[:want]
	// Samples a decoder left in the file's byte order (built-in codecs) become little-endian;
	// the differencing predictor works on them in that order. The floating-point predictor's
	// planes are in significance order whatever the file's, and it writes little-endian itself.
	if ifd.file.ByteOrder == binary.BigEndian && bits > 8 && !ifd.decoderOrdersLittle() && ifd.Predictor != 3 {
		swapBytes(out, bits/8)
	}
	switch ifd.Predictor {
	case 1:
	case 2:
		undoDifferences(out, width, height, ifd.samples(), bits/8)
	case 3:
		if err := undoFloatPredictor(out, width, height, ifd.samples(), bits/8); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("tiff: predictor %d is not read", ifd.Predictor)
	}
	return out, nil
}

// decoderOrdersLittle reports whether this image's codec returns little-endian samples itself
// (image codecs such as JPEG 2000 return sample values, not the file's bytes).
func (ifd *IFD) decoderOrdersLittle() bool {
	switch ifd.Compression {
	case CompressionNone, CompressionDeflate, CompressionAdobeZIP, CompressionLZW, CompressionPackBits:
		return false
	}
	return true
}

func swapBytes(b []byte, width int) {
	for i := 0; i+width <= len(b); i += width {
		for l, r := i, i+width-1; l < r; l, r = l+1, r-1 {
			b[l], b[r] = b[r], b[l]
		}
	}
}

// ReadImage decodes a whole image: row-major, samples of a pixel together (planar images are
// interleaved), samples wider than a byte little-endian. It is for images small enough to hold at
// once (pyramid levels, thumbnails, test files); large ones are read a tile at a time.
func (ifd *IFD) ReadImage() ([]byte, error) {
	bits := ifd.BitsPerSample[0]
	if bits%8 != 0 {
		return nil, fmt.Errorf("tiff: %d bits per sample is not read", bits)
	}
	bytesPerSample := bits / 8
	pixelBytes := bytesPerSample * ifd.SamplesPerPixel
	if int64(ifd.Width)*int64(ifd.Height)*int64(pixelBytes) > maxBlockBytes {
		return nil, fmt.Errorf("tiff: a %d×%d image is too large to read whole", ifd.Width, ifd.Height)
	}
	out := make([]byte, ifd.Width*ifd.Height*pixelBytes)
	across, down := ifd.Blocks()
	planes, perPlane := 1, ifd.SamplesPerPixel
	if ifd.PlanarConfig == 2 {
		planes, perPlane = ifd.SamplesPerPixel, 1
	}
	for plane := 0; plane < planes; plane++ {
		for by := 0; by < down; by++ {
			for bx := 0; bx < across; bx++ {
				i := plane*across*down + by*across + bx
				block, err := ifd.DecodeBlock(i)
				if err != nil {
					return nil, err
				}
				width, height := ifd.BlockSize(i)
				rowBytes := width * perPlane * bytesPerSample
				for y := 0; y < height && by*ifd.BlockHeight+y < ifd.Height; y++ {
					row := block[y*rowBytes:]
					for x := 0; x < width && bx*ifd.BlockWidth+x < ifd.Width; x++ {
						px, py := bx*ifd.BlockWidth+x, by*ifd.BlockHeight+y
						dst := out[(py*ifd.Width+px)*pixelBytes+plane*bytesPerSample:]
						copy(dst[:perPlane*bytesPerSample], row[x*perPlane*bytesPerSample:])
					}
				}
			}
		}
	}
	return out, nil
}
