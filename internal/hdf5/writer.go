// Package hdf5 writes small HDF5 files: groups of datasets (fixed-length strings and integers, of
// one dimension or none) with attributes, what a 10x Genomics feature-barcode matrix
// (cell_feature_matrix.h5) holds. Written from the HDF5 File Format Specification, version 3.0
// (The HDF Group): superblock version 2, version 2 object headers, groups whose links are kept in
// their object header ("compact" storage) and contiguous datasets, uncompressed. Every HDF5
// library since 1.8 (2008) reads it, h5py among them. Checksums are Bob Jenkins' lookup3 hash
// (hashlittle), which the specification names. The same contents always give the same bytes.
package hdf5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Group is a group of datasets and groups, in the order they are added.
type Group struct {
	name     string
	children []any // *Group or *dataset
	attrs    []*dataset
	names    map[string]bool
	address  uint64
}

// File is an HDF5 file being built; its root group is the file's.
type File struct {
	Root *Group
}

// NewFile starts a file.
func NewFile() *File { return &File{Root: newGroup("")} }

func newGroup(name string) *Group { return &Group{name: name, names: map[string]bool{}} }

// datatype is an HDF5 datatype: a fixed-point integer or a fixed-length string.
type datatype struct {
	class  byte // 0 fixed-point, 3 string
	size   uint32
	signed bool
}

// dataset is a dataset, or an attribute's value.
type dataset struct {
	name    string
	typ     datatype
	dims    []uint64 // nil: a scalar
	data    []byte   // little-endian, row by row
	address uint64   // of its object header (datasets) once written
	dataAt  uint64
}

func (g *Group) claim(name string) error {
	if name == "" || name == "." || len(name) > 255 || bytes.ContainsAny([]byte(name), "/\x00") {
		return fmt.Errorf("hdf5: %q is not a name", name)
	}
	if g.names[name] {
		return fmt.Errorf("hdf5: %q is in the group twice", name)
	}
	g.names[name] = true
	return nil
}

// Group adds a group.
func (g *Group) Group(name string) (*Group, error) {
	if err := g.claim(name); err != nil {
		return nil, err
	}
	child := newGroup(name)
	g.children = append(g.children, child)
	return child, nil
}

func ints32(v []int32) []byte {
	out := make([]byte, 0, 4*len(v))
	for _, x := range v {
		out = binary.LittleEndian.AppendUint32(out, uint32(x))
	}
	return out
}

func ints64(v []int64) []byte {
	out := make([]byte, 0, 8*len(v))
	for _, x := range v {
		out = binary.LittleEndian.AppendUint64(out, uint64(x))
	}
	return out
}

// strings are fixed-length strings as long as the longest (at least 1), null-padded.
func strs(v []string) (datatype, []byte) {
	width := 1
	for _, s := range v {
		width = max(width, len(s))
	}
	out := make([]byte, width*len(v))
	for i, s := range v {
		copy(out[i*width:], s)
	}
	return datatype{class: 3, size: uint32(width)}, out
}

func (g *Group) add(name string, typ datatype, dims []uint64, data []byte) error {
	if err := g.claim(name); err != nil {
		return err
	}
	g.children = append(g.children, &dataset{name: name, typ: typ, dims: dims, data: data})
	return nil
}

// Int32s adds a one-dimensional dataset of signed 32-bit integers.
func (g *Group) Int32s(name string, v []int32) error {
	return g.add(name, datatype{0, 4, true}, []uint64{uint64(len(v))}, ints32(v))
}

// Int64s adds a one-dimensional dataset of signed 64-bit integers.
func (g *Group) Int64s(name string, v []int64) error {
	return g.add(name, datatype{0, 8, true}, []uint64{uint64(len(v))}, ints64(v))
}

// Strings adds a one-dimensional dataset of fixed-length ASCII strings (numpy's "S" type).
func (g *Group) Strings(name string, v []string) error {
	for _, s := range v {
		for i := 0; i < len(s); i++ {
			if s[i] == 0 || s[i] > 0x7e {
				return fmt.Errorf("hdf5: %s: %q is not ASCII text", name, s)
			}
		}
	}
	typ, data := strs(v)
	return g.add(name, typ, []uint64{uint64(len(v))}, data)
}

func (g *Group) attr(name string, typ datatype, dims []uint64, data []byte) {
	g.attrs = append(g.attrs, &dataset{name: name, typ: typ, dims: dims, data: data})
}

// AttrString sets a scalar fixed-length string attribute.
func (g *Group) AttrString(name, v string) {
	typ, data := strs([]string{v})
	g.attr(name, typ, nil, data)
}

// AttrStrings sets an attribute of fixed-length strings.
func (g *Group) AttrStrings(name string, v []string) {
	typ, data := strs(v)
	g.attr(name, typ, []uint64{uint64(len(v))}, data)
}

// AttrInt64 sets a scalar 64-bit integer attribute.
func (g *Group) AttrInt64(name string, v int64) {
	g.attr(name, datatype{0, 8, true}, nil, ints64([]int64{v}))
}

// AttrInt64s sets an attribute of 64-bit integers.
func (g *Group) AttrInt64s(name string, v []int64) {
	g.attr(name, datatype{0, 8, true}, []uint64{uint64(len(v))}, ints64(v))
}

// undefined is the undefined address.
const undefined = ^uint64(0)

// Message types (the specification's IV.A.2).
const (
	msgDataspace = 0x01
	msgLinkInfo  = 0x02
	msgDatatype  = 0x03
	msgFillValue = 0x05
	msgLink      = 0x06
	msgLayout    = 0x08
	msgGroupInfo = 0x0a
	msgAttribute = 0x0c
)

type message struct {
	typ   byte
	flags byte
	body  []byte
}

func le16(b []byte, v int) []byte    { return binary.LittleEndian.AppendUint16(b, uint16(v)) }
func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }
func le64(b []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(b, v) }

// dataspaceBody is a version 2 dataspace: scalar, or simple of the dims.
func dataspaceBody(dims []uint64) []byte {
	if dims == nil {
		return []byte{2, 0, 0, 0}
	}
	b := []byte{2, byte(len(dims)), 0, 1}
	for _, d := range dims {
		b = le64(b, d)
	}
	return b
}

// datatypeBody is a version 1 datatype: little-endian two's-complement integers, or null-padded
// ASCII strings.
func datatypeBody(t datatype) []byte {
	b := []byte{t.class | 1<<4, 0, 0, 0}
	switch t.class {
	case 0:
		if t.signed {
			b[1] = 1 << 3
		}
		b = le32(b, t.size)
		b = le16(b, 0)             // bit offset
		b = le16(b, int(8*t.size)) // precision
	case 3:
		b[1] = 1 // null-padded, ASCII
		b = le32(b, t.size)
	}
	return b
}

// header is a version 2 object header holding the messages, with its checksum.
func header(msgs []message) []byte {
	var chunk []byte
	for _, m := range msgs {
		chunk = append(chunk, m.typ)
		chunk = le16(chunk, len(m.body))
		chunk = append(chunk, m.flags)
		chunk = append(chunk, m.body...)
	}
	// Flags 2: the chunk's size in 4 bytes; no times, no attribute phase change values.
	b := append([]byte("OHDR"), 2, 2)
	b = le32(b, uint32(len(chunk)))
	b = append(b, chunk...)
	return le32(b, lookup3(b, 0))
}

func attributeMessage(a *dataset) message {
	dt, ds := datatypeBody(a.typ), dataspaceBody(a.dims)
	b := []byte{3, 0}
	b = le16(b, len(a.name)+1)
	b = le16(b, len(dt))
	b = le16(b, len(ds))
	b = append(b, 0) // ASCII name
	b = append(b, a.name...)
	b = append(b, 0)
	b = append(b, dt...)
	b = append(b, ds...)
	b = append(b, a.data...)
	return message{typ: msgAttribute, body: b}
}

// writer lays the file out: the superblock, every dataset's data, then the object headers,
// children before their group, so that each header knows its links' addresses.
type writer struct {
	buf bytes.Buffer
}

func (w *writer) at() uint64 { return uint64(w.buf.Len()) }

func (w *writer) data(g *Group) {
	for _, c := range g.children {
		switch c := c.(type) {
		case *Group:
			w.data(c)
		case *dataset:
			c.dataAt = undefined
			if len(c.data) > 0 {
				c.dataAt = w.at()
				w.buf.Write(c.data)
			}
		}
	}
}

func (w *writer) headers(g *Group) {
	var links []message
	for _, c := range g.children {
		var name string
		var addr uint64
		switch c := c.(type) {
		case *Group:
			w.headers(c)
			name, addr = c.name, c.address
		case *dataset:
			c.address = w.at()
			layout := []byte{3, 1}
			layout = le64(layout, c.dataAt)
			layout = le64(layout, uint64(len(c.data)))
			w.buf.Write(header([]message{
				{typ: msgDataspace, body: dataspaceBody(c.dims)},
				{typ: msgDatatype, flags: 1, body: datatypeBody(c.typ)},
				// Fill value, version 3: allocated late, written if set, none set.
				{typ: msgFillValue, flags: 1, body: []byte{3, 2 | 2<<2}},
				{typ: msgLayout, body: layout},
			}))
			name, addr = c.name, c.address
		}
		link := []byte{1, 0, byte(len(name))}
		link = append(link, name...)
		links = append(links, message{typ: msgLink, body: le64(link, addr)})
	}
	info := []byte{0, 0}
	info = le64(info, undefined) // no fractal heap: the links are in the header
	info = le64(info, undefined)
	msgs := []message{{typ: msgLinkInfo, body: info}, {typ: msgGroupInfo, body: []byte{0, 0}}}
	msgs = append(msgs, links...)
	for _, a := range g.attrs {
		msgs = append(msgs, attributeMessage(a))
	}
	g.address = w.at()
	w.buf.Write(header(msgs))
}

// Bytes is the file.
func (f *File) Bytes() []byte {
	w := &writer{}
	w.buf.Write(make([]byte, 48)) // the superblock, written last
	w.data(f.Root)
	w.headers(f.Root)
	out := w.buf.Bytes()
	sb := append([]byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n'}, 2, 8, 8, 0)
	sb = le64(sb, 0)         // base address
	sb = le64(sb, undefined) // no superblock extension
	sb = le64(sb, uint64(len(out)))
	sb = le64(sb, f.Root.address)
	sb = le32(sb, lookup3(sb, 0))
	copy(out, sb)
	return out
}

// WriteTo writes the file to w.
func (f *File) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(f.Bytes())
	return int64(n), err
}

// Write writes the file to path.
func (f *File) Write(path string) error {
	if f.Root == nil {
		return errors.New("hdf5: no root group")
	}
	return os.WriteFile(path, f.Bytes(), 0o644)
}

// lookup3 is Bob Jenkins' hashlittle (lookup3.c, 2006, public domain), the checksum of HDF5's
// metadata.
func lookup3(k []byte, initval uint32) uint32 {
	rot := func(x uint32, n uint) uint32 { return x<<n | x>>(32-n) }
	a := 0xdeadbeef + uint32(len(k)) + initval
	b, c := a, a
	for len(k) > 12 {
		a += binary.LittleEndian.Uint32(k)
		b += binary.LittleEndian.Uint32(k[4:])
		c += binary.LittleEndian.Uint32(k[8:])
		a -= c
		a ^= rot(c, 4)
		c += b
		b -= a
		b ^= rot(a, 6)
		a += c
		c -= b
		c ^= rot(b, 8)
		b += a
		a -= c
		a ^= rot(c, 16)
		c += b
		b -= a
		b ^= rot(a, 19)
		a += c
		c -= b
		c ^= rot(b, 4)
		b += a
		k = k[12:]
	}
	if len(k) == 0 {
		return c
	}
	var tail [12]byte
	copy(tail[:], k)
	a += binary.LittleEndian.Uint32(tail[:])
	b += binary.LittleEndian.Uint32(tail[4:])
	c += binary.LittleEndian.Uint32(tail[8:])
	c ^= b
	c -= rot(b, 14)
	a ^= c
	a -= rot(c, 11)
	b ^= a
	b -= rot(a, 25)
	c ^= b
	c -= rot(b, 16)
	a ^= c
	a -= rot(c, 4)
	b ^= a
	b -= rot(a, 14)
	c ^= b
	c -= rot(b, 24)
	return c
}
