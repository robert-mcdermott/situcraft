package parquet

// The file metadata (the footer's FileMetaData) and page headers, decoded from the Thrift
// definitions in the Apache Parquet format specification (parquet-format, parquet.thrift). Only
// the fields SituCraft uses are kept; the rest are skipped.

import "fmt"

// Type is a column's physical type.
type Type int32

const (
	Boolean Type = iota
	Int32
	Int64
	Int96
	Float
	Double
	ByteArray
	FixedLenByteArray
)

func (t Type) String() string {
	names := []string{"BOOLEAN", "INT32", "INT64", "INT96", "FLOAT", "DOUBLE", "BYTE_ARRAY", "FIXED_LEN_BYTE_ARRAY"}
	if t >= 0 && int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("type %d", int32(t))
}

// Codec is a column chunk's page compression.
type Codec int32

const (
	Uncompressed Codec = iota
	Snappy
	Gzip
	LZO
	Brotli
	LZ4
	Zstd
	LZ4Raw
)

func (c Codec) String() string {
	names := []string{"UNCOMPRESSED", "SNAPPY", "GZIP", "LZO", "BROTLI", "LZ4", "ZSTD", "LZ4_RAW"}
	if c >= 0 && int(c) < len(names) {
		return names[c]
	}
	return fmt.Sprintf("codec %d", int32(c))
}

// Encodings of values and levels.
const (
	encPlain                = 0
	encPlainDictionary      = 2
	encRLE                  = 3
	encBitPacked            = 4
	encDeltaBinaryPacked    = 5
	encDeltaLengthByteArray = 6
	encDeltaByteArray       = 7
	encRLEDictionary        = 8
	encByteStreamSplit      = 9
)

// Page types.
const (
	pageData       = 0
	pageIndex      = 1
	pageDictionary = 2
	pageDataV2     = 3
)

// Repetition types.
const (
	repRequired = 0
	repOptional = 1
	repRepeated = 2
)

// Column describes one leaf column of a flat schema.
type Column struct {
	Name     string
	Type     Type
	Optional bool
	// TypeLength is the width of a FIXED_LEN_BYTE_ARRAY.
	TypeLength int
	// ConvertedType is the legacy annotation (-1 when absent): 0 UTF8, 11-14 UINT_8..UINT_64,
	// 15-18 INT_8..INT_64, and the rest of parquet.thrift's ConvertedType.
	ConvertedType int
	// Logical is the logical type's name ("STRING", "INTEGER", "TIMESTAMP", …) or "".
	Logical string
	// IntegerBits and Signed annotate INTEGER logical types (and the legacy INT_n/UINT_n).
	IntegerBits int
	Signed      bool
}

// Statistics are a column chunk's recorded minimum, maximum and null count, in the column's
// PLAIN encoding (nil when absent).
type Statistics struct {
	Min, Max     []byte
	NullCount    int64
	HasNullCount bool
}

// Chunk is one column's part of a row group.
type Chunk struct {
	Codec                Codec
	Encodings            []int32
	NumValues            int64
	DataPageOffset       int64
	DictionaryPageOffset int64
	CompressedSize       int64
	UncompressedSize     int64
	Stats                Statistics
}

// RowGroup is a horizontal partition of the rows.
type RowGroup struct {
	NumRows int64
	Chunks  []Chunk
}

type schemaElement struct {
	typ           Type
	hasType       bool
	typeLength    int
	repetition    int
	name          string
	numChildren   int
	convertedType int
	logical       string
	intBits       int
	signed        bool
}

type fileMetaData struct {
	version   int32
	schema    []schemaElement
	numRows   int64
	rowGroups []RowGroup
	keyValue  map[string]string
	createdBy string
}

func readFileMetaData(t *thrift) (*fileMetaData, error) {
	m := &fileMetaData{keyValue: map[string]string{}}
	err := t.structFields(func(id int16, typ byte) (bool, error) {
		var err error
		switch {
		case id == 1 && typ == tI32:
			m.version, err = t.i32()
		case id == 2 && typ == tList:
			var n int
			if n, _, err = t.list(); err != nil {
				return true, err
			}
			for i := 0; i < n; i++ {
				e, err := readSchemaElement(t)
				if err != nil {
					return true, err
				}
				m.schema = append(m.schema, e)
			}
		case id == 3 && typ == tI64:
			m.numRows, err = t.varint()
		case id == 4 && typ == tList:
			var n int
			if n, _, err = t.list(); err != nil {
				return true, err
			}
			for i := 0; i < n; i++ {
				g, err := readRowGroup(t)
				if err != nil {
					return true, err
				}
				m.rowGroups = append(m.rowGroups, g)
			}
		case id == 5 && typ == tList:
			var n int
			if n, _, err = t.list(); err != nil {
				return true, err
			}
			for i := 0; i < n; i++ {
				var key, value string
				err := t.structFields(func(id int16, typ byte) (bool, error) {
					var err error
					switch {
					case id == 1 && typ == tBinary:
						key, err = t.str()
					case id == 2 && typ == tBinary:
						value, err = t.str()
					default:
						return false, nil
					}
					return true, err
				})
				if err != nil {
					return true, err
				}
				m.keyValue[key] = value
			}
		case id == 6 && typ == tBinary:
			m.createdBy, err = t.str()
		default:
			return false, nil
		}
		return true, err
	})
	return m, err
}

func readSchemaElement(t *thrift) (schemaElement, error) {
	e := schemaElement{convertedType: -1}
	err := t.structFields(func(id int16, typ byte) (bool, error) {
		var err error
		var v int32
		switch {
		case id == 1 && typ == tI32:
			v, err = t.i32()
			e.typ, e.hasType = Type(v), true
		case id == 2 && typ == tI32:
			v, err = t.i32()
			e.typeLength = int(v)
		case id == 3 && typ == tI32:
			v, err = t.i32()
			e.repetition = int(v)
		case id == 4 && typ == tBinary:
			e.name, err = t.str()
		case id == 5 && typ == tI32:
			v, err = t.i32()
			e.numChildren = int(v)
		case id == 6 && typ == tI32:
			v, err = t.i32()
			e.convertedType = int(v)
		case id == 10 && typ == tStruct:
			err = readLogicalType(t, &e)
		default:
			return false, nil
		}
		return true, err
	})
	return e, err
}

// logicalNames are the members of parquet.thrift's LogicalType union, by field id.
var logicalNames = map[int16]string{
	1: "STRING", 2: "MAP", 3: "LIST", 4: "ENUM", 5: "DECIMAL", 6: "DATE", 7: "TIME", 8: "TIMESTAMP",
	10: "INTEGER", 11: "UNKNOWN", 12: "JSON", 13: "BSON", 14: "UUID", 15: "FLOAT16", 16: "VARIANT",
	17: "GEOMETRY", 18: "GEOGRAPHY",
}

func readLogicalType(t *thrift, e *schemaElement) error {
	return t.structFields(func(id int16, typ byte) (bool, error) {
		e.logical = logicalNames[id]
		if id != 10 || typ != tStruct {
			return false, nil
		}
		return true, t.structFields(func(id int16, typ byte) (bool, error) {
			switch {
			case id == 1 && typ == tByte:
				b, err := t.byte()
				e.intBits = int(int8(b))
				return true, err
			case id == 2 && (typ == tTrue || typ == tFalse):
				e.signed = t.boolean
				return true, nil
			}
			return false, nil
		})
	})
}

func readRowGroup(t *thrift) (RowGroup, error) {
	var g RowGroup
	err := t.structFields(func(id int16, typ byte) (bool, error) {
		var err error
		switch {
		case id == 1 && typ == tList:
			var n int
			if n, _, err = t.list(); err != nil {
				return true, err
			}
			for i := 0; i < n; i++ {
				c, err := readColumnChunk(t)
				if err != nil {
					return true, err
				}
				g.Chunks = append(g.Chunks, c)
			}
		case id == 3 && typ == tI64:
			g.NumRows, err = t.varint()
		default:
			return false, nil
		}
		return true, err
	})
	return g, err
}

func readColumnChunk(t *thrift) (Chunk, error) {
	var c Chunk
	err := t.structFields(func(id int16, typ byte) (bool, error) {
		switch {
		case id == 1 && typ == tBinary:
			path, err := t.str()
			if err == nil && path != "" {
				err = fmt.Errorf("parquet: column data in another file (%s) is not read", path)
			}
			return true, err
		case id == 3 && typ == tStruct:
			return true, readColumnMetaData(t, &c)
		}
		return false, nil
	})
	return c, err
}

func readColumnMetaData(t *thrift, c *Chunk) error {
	return t.structFields(func(id int16, typ byte) (bool, error) {
		var err error
		var v int32
		switch {
		case id == 2 && typ == tList:
			var n int
			if n, _, err = t.list(); err != nil {
				return true, err
			}
			for i := 0; i < n; i++ {
				if v, err = t.i32(); err != nil {
					return true, err
				}
				c.Encodings = append(c.Encodings, v)
			}
		case id == 4 && typ == tI32:
			v, err = t.i32()
			c.Codec = Codec(v)
		case id == 5 && typ == tI64:
			c.NumValues, err = t.varint()
		case id == 6 && typ == tI64:
			c.UncompressedSize, err = t.varint()
		case id == 7 && typ == tI64:
			c.CompressedSize, err = t.varint()
		case id == 9 && typ == tI64:
			c.DataPageOffset, err = t.varint()
		case id == 11 && typ == tI64:
			c.DictionaryPageOffset, err = t.varint()
		case id == 12 && typ == tStruct:
			err = readStatistics(t, &c.Stats)
		default:
			return false, nil
		}
		return true, err
	})
}

func readStatistics(t *thrift, s *Statistics) error {
	var legacyMin, legacyMax []byte
	err := t.structFields(func(id int16, typ byte) (bool, error) {
		var err error
		switch {
		case id == 1 && typ == tBinary:
			legacyMax, err = t.binary()
		case id == 2 && typ == tBinary:
			legacyMin, err = t.binary()
		case id == 3 && typ == tI64:
			s.NullCount, err = t.varint()
			s.HasNullCount = true
		case id == 5 && typ == tBinary:
			s.Max, err = t.binary()
		case id == 6 && typ == tBinary:
			s.Min, err = t.binary()
		default:
			return false, nil
		}
		return true, err
	})
	// The deprecated min and max fields (signed byte order) only when the current ones are absent.
	if s.Min == nil && s.Max == nil {
		s.Min, s.Max = legacyMin, legacyMax
	}
	return err
}

type pageHeader struct {
	typ              int32
	uncompressedSize int
	compressedSize   int
	numValues        int
	encoding         int32
	defEncoding      int32
	// Data page v2.
	numNulls        int
	defLevelsLength int
	repLevelsLength int
	isCompressed    bool
}

func readPageHeader(t *thrift) (pageHeader, error) {
	h := pageHeader{isCompressed: true}
	err := t.structFields(func(id int16, typ byte) (bool, error) {
		var err error
		var v int32
		switch {
		case id == 1 && typ == tI32:
			h.typ, err = t.i32()
		case id == 2 && typ == tI32:
			v, err = t.i32()
			h.uncompressedSize = int(v)
		case id == 3 && typ == tI32:
			v, err = t.i32()
			h.compressedSize = int(v)
		case (id == 5 || id == 7) && typ == tStruct:
			// DataPageHeader and DictionaryPageHeader share their first two fields.
			err = t.structFields(func(id int16, typ byte) (bool, error) {
				var err error
				var v int32
				switch {
				case id == 1 && typ == tI32:
					v, err = t.i32()
					h.numValues = int(v)
				case id == 2 && typ == tI32:
					h.encoding, err = t.i32()
				case id == 3 && typ == tI32:
					h.defEncoding, err = t.i32()
				default:
					return false, nil
				}
				return true, err
			})
		case id == 8 && typ == tStruct:
			err = t.structFields(func(id int16, typ byte) (bool, error) {
				var err error
				var v int32
				switch {
				case id == 1 && typ == tI32:
					v, err = t.i32()
					h.numValues = int(v)
				case id == 2 && typ == tI32:
					v, err = t.i32()
					h.numNulls = int(v)
				case id == 4 && typ == tI32:
					h.encoding, err = t.i32()
				case id == 5 && typ == tI32:
					v, err = t.i32()
					h.defLevelsLength = int(v)
				case id == 6 && typ == tI32:
					v, err = t.i32()
					h.repLevelsLength = int(v)
				case id == 7 && (typ == tTrue || typ == tFalse):
					h.isCompressed = t.boolean
				default:
					return false, nil
				}
				return true, err
			})
		default:
			return false, nil
		}
		return true, err
	})
	if err == nil && (h.compressedSize < 0 || h.uncompressedSize < 0 || h.numValues < 0 || h.defLevelsLength < 0 || h.repLevelsLength < 0) {
		err = errThrift
	}
	return h, err
}
