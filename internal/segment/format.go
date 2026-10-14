package segment

// The files of a segmentation artifact (internal/artifacts), SituCraft's own format, each a header
// (four magic bytes, a version and the body's length, little-endian) and the body deflated
// (zlib, RFC 1950/1951):
//
//	segmentation.json  what it is: method, parameters, sizes, the nuclei it grew from, totals
//	cells.bin    "SCSC"  each cell's id and nucleus id; centroid and areas (µm, µm²); counts
//	mask.bin     "SCMK"  the label raster: size, pixel size, each row as (label, length) runs
//	assignments.bin "SCAS"  each transcript's cell (row order of transcripts.parquet; 0 none)
//	counts.bin   "SCCT"  the cell-by-gene counts of gene transcripts of quality 20 or more (CSR)
//	outlines.bin "SCOL"  each cell's outline (µm), with its pieces and holes counted
//
// Readers check every size against the file before allocating, and refuse a body more than 1032
// times its compressed size (deflate's limit).

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

const formatVersion = 1

// Cell is one cell of a segmentation.
type Cell struct {
	ID string `json:"id"`
	// Nucleus is the id the bundle gives the nucleus the cell grew from ("" when none).
	Nucleus     string  `json:"nucleus"`
	X, Y        float32 `json:"-"` // centroid, µm
	Area        float32 `json:"-"` // µm²
	NucleusArea float32 `json:"-"` // µm²
	// Transcripts counts its gene transcripts of quality 20 or more; Assigned every transcript
	// assigned to it; Genes the genes among the first.
	Transcripts, Assigned, Genes uint32 `json:"-"`
}

// Counts is a cell-by-gene matrix in compressed sparse rows.
type Counts struct {
	Genes   []string
	Offsets []uint32 // len cells+1
	Gene    []uint32
	Count   []uint32
}

// Description is segmentation.json.
type Description struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Method  string `json:"method"`
	// Parameters are the method's, every one after defaults.
	Parameters  json.RawMessage `json:"parameters"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	PixelSize   float64         `json:"pixelSizeUm"`
	Cells       int             `json:"cells"`
	Transcripts int             `json:"transcripts"`
	Genes       int             `json:"genes"`
	// Nuclei says where the nuclei came from and how many there were.
	Nuclei struct {
		Source string `json:"source"`
		Count  int    `json:"count"`
	} `json:"nuclei"`
	// Totals: gene transcripts of quality 20 or more, and those assigned to cells.
	Q20GeneTranscripts int64 `json:"q20GeneTranscripts"`
	Q20GeneAssigned    int64 `json:"q20GeneAssigned"`
	// Agreement with the instrument's segmentation, when the bundle has one.
	Agreement json.RawMessage `json:"agreement,omitempty"`
}

// The files, by role.
const (
	FileDescription = "segmentation.json"
	FileCells       = "cells.bin"
	FileMask        = "mask.bin"
	FileAssignments = "assignments.bin"
	FileCounts      = "counts.bin"
	FileOutlines    = "outlines.bin"
)

// Roles name each file's role in an artifact's manifest.
var Roles = map[string]string{
	FileDescription: "description", FileCells: "cells", FileMask: "label raster",
	FileAssignments: "transcript assignments", FileCounts: "counts", FileOutlines: "cell outlines",
}

// --- Writing ---------------------------------------------------------------------------------

type buffer struct{ bytes.Buffer }

func (b *buffer) u32(v uint32)  { binary.Write(&b.Buffer, binary.LittleEndian, v) }
func (b *buffer) f32(v float32) { binary.Write(&b.Buffer, binary.LittleEndian, v) }
func (b *buffer) f64(v float64) { binary.Write(&b.Buffer, binary.LittleEndian, v) }
func (b *buffer) uvarint(v uint64) {
	var tmp [binary.MaxVarintLen64]byte
	b.Write(tmp[:binary.PutUvarint(tmp[:], v)])
}
func (b *buffer) str(s string) {
	b.uvarint(uint64(len(s)))
	b.WriteString(s)
}
func (b *buffer) u32s(v []uint32) {
	for _, x := range v {
		b.u32(x)
	}
}
func (b *buffer) f32s(v []float32) {
	for _, x := range v {
		b.f32(x)
	}
}

// writeFile writes magic, version, the body's length and the body deflated.
func writeFile(path, magic string, body []byte) error {
	var out bytes.Buffer
	out.WriteString(magic)
	binary.Write(&out, binary.LittleEndian, uint32(formatVersion))
	binary.Write(&out, binary.LittleEndian, uint64(len(body)))
	zw, err := zlib.NewWriterLevel(&out, zlib.BestSpeed)
	if err != nil {
		return err
	}
	if _, err := zw.Write(body); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// WriteCells writes cells.bin.
func WriteCells(dir string, cells []Cell) error {
	var b buffer
	b.u32(uint32(len(cells)))
	for _, c := range cells {
		b.str(c.ID)
		b.str(c.Nucleus)
	}
	for _, c := range cells {
		b.f32(c.X)
		b.f32(c.Y)
		b.f32(c.Area)
		b.f32(c.NucleusArea)
		b.u32(c.Transcripts)
		b.u32(c.Assigned)
		b.u32(c.Genes)
	}
	return writeFile(filepath.Join(dir, FileCells), "SCSC", b.Bytes())
}

// WriteMask writes mask.bin.
func WriteMask(dir string, r *Raster) error {
	var b buffer
	b.u32(uint32(r.Width))
	b.u32(uint32(r.Height))
	b.f64(r.PixelSize)
	for _, row := range r.Rows {
		b.uvarint(uint64(len(row)))
		for _, run := range row {
			b.uvarint(uint64(run.Label))
			b.uvarint(uint64(run.Length))
		}
	}
	return writeFile(filepath.Join(dir, FileMask), "SCMK", b.Bytes())
}

// WriteAssignments writes assignments.bin.
func WriteAssignments(dir string, labels []uint32) error {
	var b buffer
	b.u32(uint32(len(labels)))
	b.u32s(labels)
	return writeFile(filepath.Join(dir, FileAssignments), "SCAS", b.Bytes())
}

// WriteCounts writes counts.bin.
func WriteCounts(dir string, c *Counts) error {
	var b buffer
	b.u32(uint32(len(c.Offsets) - 1))
	b.u32(uint32(len(c.Genes)))
	for _, g := range c.Genes {
		b.str(g)
	}
	b.u32(uint32(len(c.Gene)))
	b.u32s(c.Offsets)
	b.u32s(c.Gene)
	b.u32s(c.Count)
	return writeFile(filepath.Join(dir, FileCounts), "SCCT", b.Bytes())
}

// WriteOutlines writes outlines.bin.
func WriteOutlines(dir string, outlines []Outline) error {
	var b buffer
	b.u32(uint32(len(outlines)))
	var at uint32
	b.u32(0)
	for _, o := range outlines {
		at += uint32(len(o.X))
		b.u32(at)
	}
	for _, o := range outlines {
		b.f32s(o.X)
	}
	for _, o := range outlines {
		b.f32s(o.Y)
	}
	for _, o := range outlines {
		b.u32(uint32(o.Pixels))
		b.u32(uint32(o.Pieces))
		b.u32(uint32(o.Holes))
	}
	return writeFile(filepath.Join(dir, FileOutlines), "SCOL", b.Bytes())
}

// WriteDescription writes segmentation.json.
func WriteDescription(dir string, d Description) error {
	d.Format, d.Version = "situcraft-segmentation", formatVersion
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644)
}

// --- Reading ---------------------------------------------------------------------------------

var errShort = errors.New("the file ends early")

type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil || n < 0 || n > len(r.b) {
		r.err = errShort
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}
func (r *reader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}
func (r *reader) f32() float32 { return math.Float32frombits(r.u32()) }
func (r *reader) f64() float64 {
	if b := r.take(8); b != nil {
		return math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	return 0
}
func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.err = errShort
		return 0
	}
	r.b = r.b[n:]
	return v
}
func (r *reader) str() string {
	n := r.uvarint()
	if n > uint64(len(r.b)) {
		r.err = errShort
		return ""
	}
	return string(r.take(int(n)))
}

// count reads a count of elements of size bytes each, refusing one the rest of the file cannot hold.
func (r *reader) count(size int) int {
	n := int(r.u32())
	if r.err == nil && n*size > len(r.b) {
		r.err = fmt.Errorf("it claims %d elements, more than it holds", n)
	}
	return n
}
func (r *reader) u32s(n int) []uint32 {
	b := r.take(4 * n)
	if b == nil {
		return nil
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return out
}
func (r *reader) f32s(n int) []float32 {
	u := r.u32s(n)
	out := make([]float32, len(u))
	for i, v := range u {
		out[i] = math.Float32frombits(v)
	}
	return out
}

// readFile reads a file's body, checking its magic and version.
func readFile(path, magic string) (*reader, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	if len(raw) < 16 || string(raw[:4]) != magic {
		return nil, fmt.Errorf("%s is not a SituCraft %s file", name, magic)
	}
	if v := binary.LittleEndian.Uint32(raw[4:]); v > formatVersion {
		return nil, fmt.Errorf("%s was written by a newer SituCraft (version %d)", name, v)
	}
	length := binary.LittleEndian.Uint64(raw[8:])
	if length > 1032*uint64(len(raw)) {
		return nil, fmt.Errorf("%s claims %d bytes, more than its data can hold", name, length)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[16:]))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(zr, body); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	// The stream ends there, and its checksum holds.
	if n, err := zr.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		if err == nil || err == io.EOF {
			err = errors.New("more data than its header says")
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &reader{b: body}, nil
}

func done(r *reader, name string) error {
	if r.err != nil {
		return fmt.Errorf("%s: %w", name, r.err)
	}
	if len(r.b) != 0 {
		return fmt.Errorf("%s: %d bytes left over", name, len(r.b))
	}
	return nil
}

// ReadCells reads cells.bin.
func ReadCells(dir string) ([]Cell, error) {
	r, err := readFile(filepath.Join(dir, FileCells), "SCSC")
	if err != nil {
		return nil, err
	}
	n := r.count(2)
	cells := make([]Cell, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		cells = append(cells, Cell{ID: r.str(), Nucleus: r.str()})
	}
	if r.err == nil && len(r.b) != 28*n {
		r.err = fmt.Errorf("its columns hold %d bytes, not %d", len(r.b), 28*n)
	}
	for i := 0; i < len(cells) && r.err == nil; i++ {
		c := &cells[i]
		c.X, c.Y, c.Area, c.NucleusArea = r.f32(), r.f32(), r.f32(), r.f32()
		c.Transcripts, c.Assigned, c.Genes = r.u32(), r.u32(), r.u32()
	}
	return cells, done(r, FileCells)
}

// ReadMask reads mask.bin.
func ReadMask(dir string) (*Raster, error) {
	r, err := readFile(filepath.Join(dir, FileMask), "SCMK")
	if err != nil {
		return nil, err
	}
	m := &Raster{Width: int(r.u32()), Height: int(r.u32()), PixelSize: r.f64()}
	if r.err == nil && (m.Height > len(r.b) || m.Width <= 0) {
		r.err = fmt.Errorf("a raster of %d × %d pixels cannot be this file", m.Width, m.Height)
	}
	m.Rows = make([][]Run, 0, max(0, m.Height))
	for y := 0; y < m.Height && r.err == nil; y++ {
		n := r.uvarint()
		if n > uint64(len(r.b)) {
			r.err = errShort
			break
		}
		row := make([]Run, n)
		var total uint64
		for i := range row {
			row[i] = Run{Label: uint32(r.uvarint()), Length: uint32(r.uvarint())}
			total += uint64(row[i].Length)
		}
		if r.err == nil && total != uint64(m.Width) {
			r.err = fmt.Errorf("row %d covers %d pixels, not %d", y, total, m.Width)
		}
		m.Rows = append(m.Rows, row)
	}
	return m, done(r, FileMask)
}

// ReadAssignments reads assignments.bin.
func ReadAssignments(dir string) ([]uint32, error) {
	r, err := readFile(filepath.Join(dir, FileAssignments), "SCAS")
	if err != nil {
		return nil, err
	}
	n := r.count(4)
	out := r.u32s(n)
	return out, done(r, FileAssignments)
}

// ReadCounts reads counts.bin.
func ReadCounts(dir string) (*Counts, error) {
	r, err := readFile(filepath.Join(dir, FileCounts), "SCCT")
	if err != nil {
		return nil, err
	}
	cells := r.count(4)
	genes := r.count(1)
	c := &Counts{Genes: make([]string, 0, genes)}
	for i := 0; i < genes && r.err == nil; i++ {
		c.Genes = append(c.Genes, r.str())
	}
	nnz := r.count(8)
	c.Offsets = r.u32s(cells + 1)
	c.Gene = r.u32s(nnz)
	c.Count = r.u32s(nnz)
	return c, done(r, FileCounts)
}

// ReadOutlines reads outlines.bin.
func ReadOutlines(dir string) ([]Outline, error) {
	r, err := readFile(filepath.Join(dir, FileOutlines), "SCOL")
	if err != nil {
		return nil, err
	}
	n := r.count(16)
	offsets := r.u32s(n + 1)
	if r.err == nil {
		for i := 1; i < len(offsets); i++ {
			if offsets[i] < offsets[i-1] {
				r.err = errors.New("its offsets decrease")
			}
		}
	}
	var total int
	if r.err == nil && len(offsets) > 0 {
		total = int(offsets[n])
		if 8*total+12*n > len(r.b) {
			r.err = errShort
		}
	}
	xs, ys := r.f32s(total), r.f32s(total)
	out := make([]Outline, n)
	for i := 0; i < n && r.err == nil; i++ {
		a, b := offsets[i], offsets[i+1]
		out[i] = Outline{X: xs[a:b], Y: ys[a:b], Pixels: int(r.u32()), Pieces: int(r.u32()), Holes: int(r.u32())}
	}
	return out, done(r, FileOutlines)
}

// ReadDescription reads segmentation.json.
func ReadDescription(dir string) (*Description, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileDescription))
	if err != nil {
		return nil, err
	}
	var d Description
	if err := json.Unmarshal(raw, &d); err != nil || d.Format != "situcraft-segmentation" {
		return nil, fmt.Errorf("%s is not a SituCraft segmentation description", FileDescription)
	}
	if d.Version > formatVersion {
		return nil, fmt.Errorf("%s was written by a newer SituCraft (version %d)", FileDescription, d.Version)
	}
	return &d, nil
}
