// Package parquet reads Apache Parquet files with flat schemas: every column a leaf of the root,
// required or optional, as Xenium's transcripts, cells and boundaries tables and most per-cell
// tables are. Written from the Apache Parquet format specification (parquet-format: the file
// layout, parquet.thrift and Encodings.md) and checked against DuckDB's reading of the same files.
// Nested columns (lists, maps, structs) are refused with a message naming the column.
package parquet

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"situcraft/internal/codec/lz4"
	"situcraft/internal/codec/snappy"
	"situcraft/internal/codec/zstd"
)

const (
	// A footer larger than this is not a real file's.
	maxFooterSize = 256 << 20
	// A page that decodes to more than this is not a real file's (writers keep pages near 1 MB).
	maxPageSize = 1 << 30
)

// File is an open Parquet file.
type File struct {
	NumRows   int64
	Columns   []Column
	RowGroups []RowGroup
	KeyValue  map[string]string
	CreatedBy string
	r         io.ReaderAt
	size      int64
}

// Values are one column's values for a run of rows: only the slice for the column's type is set,
// with one entry per row (zero where the value is null).
type Values struct {
	Len int
	// Valid is nil when every value is present; otherwise false marks the nulls.
	Valid   []bool
	Bool    []bool
	Int32   []int32
	Int64   []int64
	Float32 []float32
	Float64 []float64
	// Bytes holds BYTE_ARRAY, FIXED_LEN_BYTE_ARRAY and INT96 values (nil where null).
	Bytes [][]byte
}

// Open reads a Parquet file's metadata.
func Open(r io.ReaderAt, size int64) (*File, error) {
	if size < 12 {
		return nil, errors.New("parquet: the file is too short to be Parquet")
	}
	tail := make([]byte, 8)
	if _, err := r.ReadAt(tail, size-8); err != nil {
		return nil, err
	}
	head := make([]byte, 4)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, err
	}
	switch {
	case string(tail[4:]) == "PARE":
		return nil, errors.New("parquet: encrypted Parquet files are not read")
	case string(tail[4:]) != "PAR1" || string(head) != "PAR1":
		return nil, errors.New("parquet: not a Parquet file (no PAR1 magic)")
	}
	footerSize := int64(binary.LittleEndian.Uint32(tail))
	if footerSize > maxFooterSize || footerSize > size-12 {
		return nil, fmt.Errorf("parquet: footer of %d bytes does not fit the file", footerSize)
	}
	footer := make([]byte, footerSize)
	if _, err := r.ReadAt(footer, size-8-footerSize); err != nil {
		return nil, err
	}
	m, err := readFileMetaData(newThrift(footer))
	if err != nil {
		return nil, fmt.Errorf("parquet: footer: %w", err)
	}
	f := &File{NumRows: m.numRows, RowGroups: m.rowGroups, KeyValue: m.keyValue, CreatedBy: m.createdBy, r: r, size: size}
	if len(m.schema) == 0 {
		return nil, errors.New("parquet: the file has no schema")
	}
	root := m.schema[0]
	for _, e := range m.schema[1:] {
		if e.numChildren > 0 || !e.hasType {
			return nil, fmt.Errorf("parquet: column %q is nested (a group); only flat schemas are read", e.name)
		}
		if e.repetition == repRepeated {
			return nil, fmt.Errorf("parquet: column %q is repeated (a list); only flat schemas are read", e.name)
		}
		if e.typ < Boolean || e.typ > FixedLenByteArray {
			return nil, fmt.Errorf("parquet: column %q has unknown type %d", e.name, int32(e.typ))
		}
		col := Column{Name: e.name, Type: e.typ, Optional: e.repetition == repOptional, TypeLength: e.typeLength, ConvertedType: e.convertedType, Logical: e.logical, IntegerBits: e.intBits, Signed: e.signed}
		if col.Logical == "" && col.ConvertedType >= 11 && col.ConvertedType <= 18 {
			// The legacy UINT_8..UINT_64 and INT_8..INT_64 annotations.
			col.Logical = "INTEGER"
			col.IntegerBits = 8 << ((col.ConvertedType - 11) % 4)
			col.Signed = col.ConvertedType >= 15
		}
		f.Columns = append(f.Columns, col)
	}
	if root.numChildren != len(f.Columns) {
		return nil, errors.New("parquet: the schema is nested; only flat schemas are read")
	}
	var rows int64
	for g, group := range f.RowGroups {
		if len(group.Chunks) != len(f.Columns) {
			return nil, fmt.Errorf("parquet: row group %d has %d columns, the schema %d", g, len(group.Chunks), len(f.Columns))
		}
		if group.NumRows < 0 {
			return nil, fmt.Errorf("parquet: row group %d has %d rows", g, group.NumRows)
		}
		rows += group.NumRows
	}
	if rows != f.NumRows {
		return nil, fmt.Errorf("parquet: the row groups hold %d rows, the footer says %d", rows, f.NumRows)
	}
	return f, nil
}

// Index is the position of the column with this name, or -1.
func (f *File) Index(name string) int {
	for i, c := range f.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// ReadAll reads one column across every row group.
func (f *File) ReadAll(column int) (*Values, error) {
	if column < 0 || column >= len(f.Columns) {
		return nil, fmt.Errorf("parquet: no column %d", column)
	}
	all := newValues(&f.Columns[column], int(f.NumRows))
	var valid []bool
	at := 0
	for g := range f.RowGroups {
		v, err := f.ReadColumn(g, column)
		if err != nil {
			return nil, err
		}
		copyValues(all, v, at)
		if v.Valid != nil && valid == nil {
			// The first nulls: the rows before them were all present.
			valid = make([]bool, all.Len)
			for i := 0; i < at; i++ {
				valid[i] = true
			}
		}
		if valid != nil {
			for i := 0; i < v.Len; i++ {
				valid[at+i] = v.Valid == nil || v.Valid[i]
			}
		}
		at += v.Len
	}
	all.Valid = valid
	return all, nil
}

// ReadColumn reads one column of one row group.
func (f *File) ReadColumn(rowGroup, column int) (*Values, error) {
	if rowGroup < 0 || rowGroup >= len(f.RowGroups) || column < 0 || column >= len(f.Columns) {
		return nil, fmt.Errorf("parquet: no row group %d, column %d", rowGroup, column)
	}
	col := &f.Columns[column]
	group := f.RowGroups[rowGroup]
	chunk := group.Chunks[column]
	wrap := func(err error) error {
		return fmt.Errorf("parquet: column %q, row group %d: %w", col.Name, rowGroup, err)
	}
	if chunk.NumValues != group.NumRows {
		return nil, wrap(fmt.Errorf("%d values for %d rows", chunk.NumValues, group.NumRows))
	}
	start := chunk.DataPageOffset
	if chunk.DictionaryPageOffset > 0 && chunk.DictionaryPageOffset < start {
		start = chunk.DictionaryPageOffset
	}
	if start < 4 || chunk.CompressedSize < 0 || start+chunk.CompressedSize > f.size-8 {
		return nil, wrap(fmt.Errorf("column data at %d (%d bytes) lies outside the file", start, chunk.CompressedSize))
	}
	buf := make([]byte, chunk.CompressedSize)
	if _, err := f.r.ReadAt(buf, start); err != nil {
		return nil, wrap(err)
	}
	v := newValues(col, int(group.NumRows))
	if col.Optional {
		v.Valid = make([]bool, v.Len)
	}
	var dict *Values
	filled := 0
	for pos := 0; pos < len(buf) && filled < v.Len; {
		t := newThrift(buf[pos:])
		h, err := readPageHeader(t)
		if err != nil {
			return nil, wrap(err)
		}
		pos += t.pos
		if h.compressedSize > len(buf)-pos {
			return nil, wrap(errors.New("page runs past the column data"))
		}
		page := buf[pos : pos+h.compressedSize]
		pos += h.compressedSize
		switch h.typ {
		case pageDictionary:
			data, err := decompress(chunk.Codec, page, h.uncompressedSize)
			if err != nil {
				return nil, wrap(err)
			}
			// Every value takes at least a bit: a count beyond that is corrupt (and would allocate).
			if h.numValues > 8*len(data)+8 {
				return nil, wrap(errPage)
			}
			dict = newValues(col, h.numValues)
			if _, err := plain(col, data, h.numValues, dict, nil); err != nil {
				return nil, wrap(fmt.Errorf("dictionary: %w", err))
			}
		case pageData, pageDataV2:
			if h.numValues > v.Len-filled {
				return nil, wrap(fmt.Errorf("page holds %d values but only %d rows remain", h.numValues, v.Len-filled))
			}
			if err := f.decodeDataPage(col, chunk.Codec, h, page, v, filled, dict); err != nil {
				return nil, wrap(err)
			}
			filled += h.numValues
		case pageIndex:
		default:
			return nil, wrap(fmt.Errorf("page type %d", h.typ))
		}
	}
	if filled != v.Len {
		return nil, wrap(fmt.Errorf("pages hold %d values for %d rows", filled, v.Len))
	}
	if v.Valid != nil {
		all := true
		for _, ok := range v.Valid {
			all = all && ok
		}
		if all {
			v.Valid = nil
		}
	}
	return v, nil
}

// decodeDataPage decodes a data page's values into v from row `at`.
func (f *File) decodeDataPage(col *Column, codec Codec, h pageHeader, page []byte, v *Values, at int, dict *Values) error {
	n := h.numValues
	var data []byte
	var defLevels []byte
	if h.typ == pageDataV2 {
		// Levels are never compressed in v2 pages; values are unless is_compressed says not.
		if h.repLevelsLength+h.defLevelsLength > len(page) {
			return errPage
		}
		if h.repLevelsLength != 0 {
			return errors.New("repetition levels in a flat column")
		}
		defLevels = page[:h.defLevelsLength]
		values := page[h.repLevelsLength+h.defLevelsLength:]
		if h.isCompressed {
			var err error
			data, err = decompress(codec, values, h.uncompressedSize-h.repLevelsLength-h.defLevelsLength)
			if err != nil {
				return err
			}
		} else {
			data = values
		}
	} else {
		var err error
		data, err = decompress(codec, page, h.uncompressedSize)
		if err != nil {
			return err
		}
		if col.Optional {
			if len(data) < 4 {
				return errPage
			}
			length := int(binary.LittleEndian.Uint32(data))
			if length < 0 || length > len(data)-4 {
				return errPage
			}
			defLevels = data[4 : 4+length]
			data = data[4+length:]
		}
	}
	// Which rows of the page hold values (definition level 1); all of them in a required column.
	var positions []int
	present := n
	if col.Optional {
		levels := make([]int32, n)
		if err := readHybrid(defLevels, 1, n, levels); err != nil {
			return err
		}
		positions = make([]int, 0, n)
		for k, level := range levels {
			if level > 1 {
				return errors.New("definition level above 1 in a flat column")
			}
			if level == 1 {
				positions = append(positions, at+k)
				v.Valid[at+k] = true
			}
		}
		present = len(positions)
	} else {
		positions = make([]int, n)
		for k := range positions {
			positions[k] = at + k
		}
	}
	switch h.encoding {
	case encPlain:
		_, err := plain(col, data, present, v, positions)
		return err
	case encPlainDictionary, encRLEDictionary:
		if dict == nil {
			return errors.New("dictionary-encoded page without a dictionary page")
		}
		if len(data) == 0 {
			if present == 0 {
				return nil
			}
			return errPage
		}
		indices := make([]int32, present)
		if err := readHybrid(data[1:], int(data[0]), present, indices); err != nil {
			return err
		}
		for k, index := range indices {
			if index < 0 || int(index) >= dict.Len {
				return fmt.Errorf("dictionary index %d out of %d", index, dict.Len)
			}
			copyOne(v, positions[k], dict, int(index))
		}
		return nil
	case encRLE:
		if col.Type != Boolean || len(data) < 4 {
			return fmt.Errorf("RLE encoding of %v values", col.Type)
		}
		levels := make([]int32, present)
		if err := readHybrid(data[4:], 1, present, levels); err != nil {
			return err
		}
		for k, b := range levels {
			v.Bool[positions[k]] = b == 1
		}
		return nil
	case encDeltaBinaryPacked:
		ints, _, err := deltaBinaryPacked(data, present)
		if err != nil {
			return err
		}
		for k, x := range ints {
			switch col.Type {
			case Int32:
				v.Int32[positions[k]] = int32(x)
			case Int64:
				v.Int64[positions[k]] = x
			default:
				return fmt.Errorf("DELTA_BINARY_PACKED encoding of %v values", col.Type)
			}
		}
		return nil
	case encDeltaLengthByteArray, encDeltaByteArray:
		decode := deltaLengthByteArray
		if h.encoding == encDeltaByteArray {
			decode = deltaByteArray
		}
		if col.Type != ByteArray && col.Type != FixedLenByteArray {
			return fmt.Errorf("delta byte-array encoding of %v values", col.Type)
		}
		arrays, _, err := decode(data, present)
		if err != nil {
			return err
		}
		for k, b := range arrays {
			v.Bytes[positions[k]] = b
		}
		return nil
	case encByteStreamSplit:
		width := map[Type]int{Int32: 4, Float: 4, Int64: 8, Double: 8, FixedLenByteArray: col.TypeLength}[col.Type]
		joined, err := byteStreamSplit(data, present, width)
		if err != nil {
			return err
		}
		_, err = plain(col, joined, present, v, positions)
		return err
	}
	return fmt.Errorf("encoding %d is not read", h.encoding)
}

// decompress returns a page's data, checking it decodes to the size its header records.
func decompress(codec Codec, src []byte, size int) ([]byte, error) {
	if size < 0 || size > maxPageSize {
		return nil, fmt.Errorf("page declares %d bytes", size)
	}
	var out []byte
	var err error
	switch codec {
	case Uncompressed:
		out = src
	case Snappy:
		out, err = snappy.Decode(src, size)
	case Gzip:
		var reader *gzip.Reader
		if reader, err = gzip.NewReader(bytes.NewReader(src)); err == nil {
			out, err = io.ReadAll(io.LimitReader(reader, int64(size)+1))
		}
	case Zstd:
		out, err = zstd.Decode(make([]byte, 0, size), src, size)
	case LZ4Raw:
		out = make([]byte, size)
		var n int
		n, err = lz4.DecodeBlock(out, src)
		out = out[:n]
	case LZ4:
		out, err = hadoopLZ4(src, size)
	default:
		return nil, fmt.Errorf("%v-compressed pages are not read", codec)
	}
	if err != nil {
		return nil, err
	}
	if len(out) != size {
		return nil, fmt.Errorf("page decodes to %d bytes, not the %d its header records", len(out), size)
	}
	return out, nil
}

// hadoopLZ4 reads the deprecated LZ4 codec as Hadoop writes it (blocks each preceded by their
// big-endian decoded and encoded sizes), falling back to a bare LZ4 block as some writers did.
func hadoopLZ4(src []byte, size int) ([]byte, error) {
	out := make([]byte, 0, size)
	p := 0
	for p+8 <= len(src) && len(out) < size {
		decoded := int(binary.BigEndian.Uint32(src[p:]))
		encoded := int(binary.BigEndian.Uint32(src[p+4:]))
		if decoded <= 0 || encoded <= 0 || decoded > size-len(out) || encoded > len(src)-p-8 {
			break
		}
		block := make([]byte, decoded)
		n, err := lz4.DecodeBlock(block, src[p+8:p+8+encoded])
		if err != nil || n != decoded {
			break
		}
		out = append(out, block...)
		p += 8 + encoded
	}
	if p == len(src) && len(out) == size {
		return out, nil
	}
	block := make([]byte, size)
	n, err := lz4.DecodeBlock(block, src)
	return block[:n], err
}

func newValues(col *Column, n int) *Values {
	v := &Values{Len: n}
	switch col.Type {
	case Boolean:
		v.Bool = make([]bool, n)
	case Int32:
		v.Int32 = make([]int32, n)
	case Int64:
		v.Int64 = make([]int64, n)
	case Float:
		v.Float32 = make([]float32, n)
	case Double:
		v.Float64 = make([]float64, n)
	default:
		v.Bytes = make([][]byte, n)
	}
	return v
}

func copyOne(dst *Values, i int, src *Values, j int) {
	switch {
	case dst.Bool != nil:
		dst.Bool[i] = src.Bool[j]
	case dst.Int32 != nil:
		dst.Int32[i] = src.Int32[j]
	case dst.Int64 != nil:
		dst.Int64[i] = src.Int64[j]
	case dst.Float32 != nil:
		dst.Float32[i] = src.Float32[j]
	case dst.Float64 != nil:
		dst.Float64[i] = src.Float64[j]
	default:
		dst.Bytes[i] = src.Bytes[j]
	}
}

func copyValues(dst, src *Values, at int) {
	switch {
	case dst.Bool != nil:
		copy(dst.Bool[at:], src.Bool)
	case dst.Int32 != nil:
		copy(dst.Int32[at:], src.Int32)
	case dst.Int64 != nil:
		copy(dst.Int64[at:], src.Int64)
	case dst.Float32 != nil:
		copy(dst.Float32[at:], src.Float32)
	case dst.Float64 != nil:
		copy(dst.Float64[at:], src.Float64)
	default:
		copy(dst.Bytes[at:], src.Bytes)
	}
}
