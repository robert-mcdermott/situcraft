package parquet

// A reader of Apache Thrift's compact protocol, in which Parquet writes its file metadata and page
// headers. Written from the Thrift compact protocol specification (Apache Thrift,
// doc/specs/thrift-compact-protocol.md): fields carry a delta-encoded id and a type nibble,
// integers are zigzag varints, binaries and strings a varint length, lists a size-and-type header.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	tStop   = 0
	tTrue   = 1
	tFalse  = 2
	tByte   = 3
	tI16    = 4
	tI32    = 5
	tI64    = 6
	tDouble = 7
	tBinary = 8
	tList   = 9
	tSet    = 10
	tMap    = 11
	tStruct = 12
)

// Nesting deeper than this is not Parquet metadata.
const maxThriftDepth = 32

var errThrift = errors.New("parquet: corrupt metadata")

type thrift struct {
	buf   []byte
	pos   int
	depth int
	// The last field id of each open struct (ids are deltas from it), and the boolean value a
	// field header carried (the compact protocol stores booleans in the type nibble).
	last    []int16
	boolean bool
}

func newThrift(buf []byte) *thrift { return &thrift{buf: buf} }

func (t *thrift) byte() (byte, error) {
	if t.pos >= len(t.buf) {
		return 0, errThrift
	}
	b := t.buf[t.pos]
	t.pos++
	return b, nil
}

func (t *thrift) uvarint() (uint64, error) {
	v, n := binary.Uvarint(t.buf[t.pos:])
	if n <= 0 {
		return 0, errThrift
	}
	t.pos += n
	return v, nil
}

func (t *thrift) varint() (int64, error) {
	u, err := t.uvarint()
	return int64(u>>1) ^ -int64(u&1), err
}

func (t *thrift) i32() (int32, error) {
	v, err := t.varint()
	if v > math.MaxInt32 || v < math.MinInt32 {
		return 0, errThrift
	}
	return int32(v), err
}

func (t *thrift) binary() ([]byte, error) {
	n, err := t.uvarint()
	if err != nil || n > uint64(len(t.buf)-t.pos) {
		return nil, errThrift
	}
	b := t.buf[t.pos : t.pos+int(n)]
	t.pos += int(n)
	return b, nil
}

func (t *thrift) str() (string, error) {
	b, err := t.binary()
	return string(b), err
}

func (t *thrift) double() (float64, error) {
	if len(t.buf)-t.pos < 8 {
		return 0, errThrift
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(t.buf[t.pos:]))
	t.pos += 8
	return v, nil
}

// beginStruct and endStruct bracket the fields of a struct.
func (t *thrift) beginStruct() error {
	t.depth++
	if t.depth > maxThriftDepth {
		return errors.New("parquet: metadata nests too deeply")
	}
	t.last = append(t.last, 0)
	return nil
}

func (t *thrift) endStruct() {
	t.depth--
	t.last = t.last[:len(t.last)-1]
}

// field reads a field header: its id and type, or ok false at the struct's end.
func (t *thrift) field() (id int16, typ byte, ok bool, err error) {
	b, err := t.byte()
	if err != nil {
		return 0, 0, false, err
	}
	if b == tStop {
		return 0, 0, false, nil
	}
	typ = b & 0x0f
	if delta := b >> 4; delta != 0 {
		id = t.last[len(t.last)-1] + int16(delta)
	} else {
		v, err := t.varint()
		if err != nil {
			return 0, 0, false, err
		}
		id = int16(v)
	}
	t.last[len(t.last)-1] = id
	if typ == tTrue || typ == tFalse {
		t.boolean = typ == tTrue
	}
	return id, typ, true, nil
}

// list reads a list (or set) header: its size and element type.
func (t *thrift) list() (int, byte, error) {
	b, err := t.byte()
	if err != nil {
		return 0, 0, err
	}
	size := int(b >> 4)
	if size == 15 {
		n, err := t.uvarint()
		if err != nil || n > uint64(len(t.buf)) {
			return 0, 0, errThrift
		}
		size = int(n)
	}
	// Each element takes at least one byte (a list cannot be longer than what is left).
	if size > len(t.buf)-t.pos {
		return 0, 0, errThrift
	}
	return size, b & 0x0f, nil
}

// skip passes over a value of the given type.
func (t *thrift) skip(typ byte) error {
	switch typ {
	case tTrue, tFalse:
		return nil
	case tByte:
		_, err := t.byte()
		return err
	case tI16, tI32, tI64:
		_, err := t.uvarint()
		return err
	case tDouble:
		_, err := t.double()
		return err
	case tBinary:
		_, err := t.binary()
		return err
	case tList, tSet:
		n, elem, err := t.list()
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			// Booleans in a list are one byte each.
			if elem == tTrue || elem == tFalse {
				elem = tByte
			}
			if err := t.skip(elem); err != nil {
				return err
			}
		}
		return nil
	case tMap:
		n, err := t.uvarint()
		if err != nil || n > uint64(len(t.buf)-t.pos) {
			return errThrift
		}
		if n == 0 {
			return nil
		}
		kv, err := t.byte()
		if err != nil {
			return err
		}
		for i := 0; i < int(n); i++ {
			if err := t.skip(kv >> 4); err != nil {
				return err
			}
			if err := t.skip(kv & 0x0f); err != nil {
				return err
			}
		}
		return nil
	case tStruct:
		if err := t.beginStruct(); err != nil {
			return err
		}
		defer t.endStruct()
		for {
			_, ftyp, ok, err := t.field()
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			if err := t.skip(ftyp); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("parquet: unknown metadata type %d", typ)
}

// structFields calls fn for each field of a struct; fn reads the value or returns handled false
// to have it skipped.
func (t *thrift) structFields(fn func(id int16, typ byte) (handled bool, err error)) error {
	if err := t.beginStruct(); err != nil {
		return err
	}
	defer t.endStruct()
	for {
		id, typ, ok, err := t.field()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		handled, err := fn(id, typ)
		if err != nil {
			return err
		}
		if !handled {
			if err := t.skip(typ); err != nil {
				return err
			}
		}
	}
}
