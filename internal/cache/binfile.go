package cache

// The cache's binary files share one layout: a 32-byte header (a four-letter magic, the format
// version and six numbers that the kind of file defines), a table of 24-byte entries (one per
// tile: the offset and length of its block, two counts, and a reserved zero), then the blocks.
// Everything is little-endian. Each block is a zlib stream (RFC 1950) of the tile's columns, laid
// out so that each column starts on a multiple of its width once inflated; its inflated size
// follows from the two counts, and a reader inflates exactly that much.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	headerSize     = 32
	tableEntrySize = 24
	// maxBlock bounds what one block may declare, so that a damaged file cannot make a reader
	// allocate without limit.
	maxBlock = 1 << 30
)

var le = binary.LittleEndian

// tableEntry locates one tile's block; A and B are counts whose meaning the file kind defines.
type tableEntry struct {
	Offset uint64
	Length uint32
	A, B   uint32
}

// tableWriter writes a binary file: the header and an empty table first, the blocks as they
// come, and the table last.
type tableWriter struct {
	f     *os.File
	table []tableEntry
	pos   int64
}

func newTableWriter(f *os.File, magic string, fields [6]uint32, entries int) (*tableWriter, error) {
	head := make([]byte, headerSize+tableEntrySize*entries)
	copy(head, magic)
	le.PutUint32(head[4:], FormatVersion)
	for i, v := range fields {
		le.PutUint32(head[8+4*i:], v)
	}
	if _, err := f.Write(head); err != nil {
		return nil, err
	}
	return &tableWriter{f: f, table: make([]tableEntry, entries), pos: int64(len(head))}, nil
}

// block appends tile i's block (padded to a multiple of 4 bytes) with its two counts.
func (w *tableWriter) block(i int, data []byte, a, b uint32) error {
	if len(data) == 0 {
		w.table[i] = tableEntry{A: a, B: b}
		return nil
	}
	if len(data) > maxBlock {
		return fmt.Errorf("a block of %d bytes is too large", len(data))
	}
	w.table[i] = tableEntry{Offset: uint64(w.pos), Length: uint32(len(data)), A: a, B: b}
	if pad := (4 - len(data)%4) % 4; pad > 0 {
		data = append(data, make([]byte, pad)...)
	}
	if _, err := w.f.Write(data); err != nil {
		return err
	}
	w.pos += int64(len(data))
	return nil
}

// finish writes the table.
func (w *tableWriter) finish() error {
	buf := make([]byte, tableEntrySize*len(w.table))
	for i, e := range w.table {
		p := buf[i*tableEntrySize:]
		le.PutUint64(p, e.Offset)
		le.PutUint32(p[8:], e.Length)
		le.PutUint32(p[12:], e.A)
		le.PutUint32(p[16:], e.B)
	}
	_, err := w.f.WriteAt(buf, headerSize)
	return err
}

// tableFile is an open binary file of the cache.
type tableFile struct {
	f      *os.File
	size   int64
	Fields [6]uint32
	Table  []tableEntry
}

var errDamaged = errors.New("damaged cache file")

// openTableFile opens a binary file, checking its magic, version and table. entries computes the
// number of table entries from the header's fields.
func openTableFile(path, magic string, entries func(fields [6]uint32) (int, error)) (*tableFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	t, err := readTable(f, magic, entries)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

func readTable(f *os.File, magic string, entries func(fields [6]uint32) (int, error)) (*tableFile, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	head := make([]byte, headerSize)
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, errDamaged
	}
	if string(head[:4]) != magic {
		return nil, fmt.Errorf("%w: not a %s file", errDamaged, magic)
	}
	if v := le.Uint32(head[4:]); v != FormatVersion {
		return nil, fmt.Errorf("cache format %d, not %d", v, FormatVersion)
	}
	t := &tableFile{f: f, size: size}
	for i := range t.Fields {
		t.Fields[i] = le.Uint32(head[8+4*i:])
	}
	n, err := entries(t.Fields)
	if err != nil {
		return nil, err
	}
	if n < 0 || int64(n) > (size-headerSize)/tableEntrySize {
		return nil, fmt.Errorf("%w: its table does not fit", errDamaged)
	}
	buf := make([]byte, n*tableEntrySize)
	if _, err := f.ReadAt(buf, headerSize); err != nil {
		return nil, errDamaged
	}
	t.Table = make([]tableEntry, n)
	start := int64(headerSize + n*tableEntrySize)
	for i := range t.Table {
		p := buf[i*tableEntrySize:]
		e := tableEntry{Offset: le.Uint64(p), Length: le.Uint32(p[8:]), A: le.Uint32(p[12:]), B: le.Uint32(p[16:])}
		if e.Length > 0 && (e.Offset < uint64(start) || e.Offset%4 != 0 || e.Length > maxBlock || e.Offset+uint64(e.Length) > uint64(size)) {
			return nil, fmt.Errorf("%w: block %d lies outside the file", errDamaged, i)
		}
		t.Table[i] = e
	}
	return t, nil
}

// block reads table entry i's block.
func (t *tableFile) block(i int) ([]byte, tableEntry, error) {
	if i < 0 || i >= len(t.Table) {
		return nil, tableEntry{}, fmt.Errorf("no tile %d", i)
	}
	e := t.Table[i]
	if e.Length == 0 {
		return nil, e, nil
	}
	buf := make([]byte, e.Length)
	if _, err := t.f.ReadAt(buf, int64(e.Offset)); err != nil {
		return nil, e, errDamaged
	}
	return buf, e, nil
}

func (t *tableFile) Close() error { return t.f.Close() }

// columns appends little-endian columns to a block.
type columns struct{ buf []byte }

func (c *columns) u32(v []uint32) {
	at := len(c.buf)
	c.buf = append(c.buf, make([]byte, 4*len(v))...)
	for i, x := range v {
		le.PutUint32(c.buf[at+4*i:], x)
	}
}

func (c *columns) f32(v []float32) {
	at := len(c.buf)
	c.buf = append(c.buf, make([]byte, 4*len(v))...)
	for i, x := range v {
		le.PutUint32(c.buf[at+4*i:], math.Float32bits(x))
	}
}

func (c *columns) u16(v []uint16) {
	at := len(c.buf)
	c.buf = append(c.buf, make([]byte, 2*len(v))...)
	for i, x := range v {
		le.PutUint16(c.buf[at+2*i:], x)
	}
}

func (c *columns) u8(v []uint8) { c.buf = append(c.buf, v...) }

// pad aligns the next column to 4 bytes.
func (c *columns) pad() {
	for len(c.buf)%4 != 0 {
		c.buf = append(c.buf, 0)
	}
}

// reader takes columns from a block; a block too short leaves err set and returns empty columns.
type reader struct {
	buf []byte
	pos int
	err error
}

func (r *reader) take(n, size int) []byte {
	if r.err != nil || n < 0 || n > (len(r.buf)-r.pos)/size {
		r.err = errDamaged
		return nil
	}
	b := r.buf[r.pos : r.pos+n*size]
	r.pos += n * size
	return b
}

func (r *reader) u32(n int) []uint32 {
	b := r.take(n, 4)
	if b == nil {
		return nil
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = le.Uint32(b[4*i:])
	}
	return out
}

func (r *reader) f32(n int) []float32 {
	b := r.take(n, 4)
	if b == nil {
		return nil
	}
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(le.Uint32(b[4*i:]))
	}
	return out
}

func (r *reader) u16(n int) []uint16 {
	b := r.take(n, 2)
	if b == nil {
		return nil
	}
	out := make([]uint16, n)
	for i := range out {
		out[i] = le.Uint16(b[2*i:])
	}
	return out
}

func (r *reader) u8(n int) []uint8 {
	b := r.take(n, 1)
	if b == nil {
		return nil
	}
	return append([]uint8(nil), b...)
}

func (r *reader) pad() {
	if r.err == nil {
		r.pos = min(len(r.buf), (r.pos+3)&^3)
	}
}

// end checks that the block held exactly its columns.
func (r *reader) end() error {
	if r.err != nil {
		return r.err
	}
	if r.pos != len(r.buf) {
		return fmt.Errorf("%w: %d bytes past a block's columns", errDamaged, len(r.buf)-r.pos)
	}
	return nil
}

// deflate compresses a block's columns.
func deflate(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, zlib.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// inflate decompresses a block that must hold exactly size bytes. Deflate expands at most about
// 1032 times, so a block claiming more than that of its stored bytes is refused before anything
// is allocated.
func inflate(block []byte, size int64) ([]byte, error) {
	if size <= 0 || size > maxBlock || size > 1032*int64(len(block))+64 {
		return nil, errDamaged
	}
	zr, err := zlib.NewReader(bytes.NewReader(block))
	if err != nil {
		return nil, errDamaged
	}
	out := make([]byte, size)
	if _, err := io.ReadFull(zr, out); err != nil {
		return nil, errDamaged
	}
	if n, err := zr.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		return nil, errDamaged
	}
	return out, nil
}

func pad4(n int64) int64 { return (n + 3) &^ 3 }

// offsets checks CSR offsets: from 0, never decreasing, ending at total.
func offsets(starts []uint32, total int) error {
	if len(starts) == 0 || starts[0] != 0 || int(starts[len(starts)-1]) != total {
		return errDamaged
	}
	for i := 1; i < len(starts); i++ {
		if starts[i] < starts[i-1] {
			return errDamaged
		}
	}
	return nil
}
