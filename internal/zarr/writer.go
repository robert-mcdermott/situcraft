package zarr

// A writer of Zarr hierarchies into a folder, in format 2 or 3, for SituCraft's exports (AnnData
// and SpatialData stores): groups with their attributes, and arrays chunk by chunk, row-major and
// little-endian, each chunk compressed with gzip (format 3's gzip codec; format 2's zlib
// compressor), which every Zarr implementation reads. Text is UTF-8 of varying length (numcodecs'
// VLenUTF8: format 2's object dtype with the vlen-utf8 filter, format 3's string data type with
// the vlen-utf8 codec). Chunk keys use "/" in both formats. Written from the Zarr storage
// specifications (format 2, and format 3's core and codecs); the same values give the same bytes.

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Writer writes a Zarr hierarchy into a folder.
type Writer struct {
	root   string
	format int
	// Level is the compression level (gzip and zlib's 1–9).
	Level int
}

// NewWriter starts a hierarchy in the folder root (made if missing) in format 2 or 3, with its
// root group's attributes.
func NewWriter(root string, format int, attrs map[string]any) (*Writer, error) {
	if format != 2 && format != 3 {
		return nil, fmt.Errorf("zarr: format %d is not written", format)
	}
	w := &Writer{root: root, format: format, Level: 5}
	return w, w.Group("", attrs)
}

// Format is the hierarchy's Zarr format.
func (w *Writer) Format() int { return w.format }

func (w *Writer) file(key string, data []byte) error {
	clean := path.Clean("/" + key)[1:]
	if clean != key && key != "" {
		return fmt.Errorf("zarr: %q is not a key", key)
	}
	p := filepath.Join(w.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func jsonDoc(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	return append(raw, '\n'), err
}

// Group writes a group at path ("" for the root) with its attributes (none when nil).
func (w *Writer) Group(p string, attrs map[string]any) error {
	if attrs == nil {
		attrs = map[string]any{}
	}
	if w.format == 3 {
		doc, err := jsonDoc(map[string]any{"zarr_format": 3, "node_type": "group", "attributes": attrs})
		if err != nil {
			return err
		}
		return w.file(join(p, "zarr.json"), doc)
	}
	if err := w.file(join(p, ".zgroup"), []byte("{\n  \"zarr_format\": 2\n}\n")); err != nil {
		return err
	}
	doc, err := jsonDoc(attrs)
	if err != nil {
		return err
	}
	return w.file(join(p, ".zattrs"), doc)
}

// ArraySpec describes an array to write.
type ArraySpec struct {
	Shape  []int
	Chunks []int // the chunk shape (the whole array when nil)
	// DType is the element type in numpy's notation ("<u4", "<i8", "<f4", "<f8", "|u1", "|i1",
	// "|b1"), or "text" for UTF-8 text of varying length.
	DType string
	Attrs map[string]any
	// Dimensions names the dimensions (format 3's dimension_names; nil for none).
	Dimensions []string
}

// ArrayWriter writes one array's chunks.
type ArrayWriter struct {
	w     *Writer
	path  string
	spec  ArraySpec
	item  int // bytes per element (0 for text)
	text  bool
	chunk int // elements per chunk
}

var v3TypeNames = map[string]string{
	"|b1": "bool", "|i1": "int8", "<i2": "int16", "<i4": "int32", "<i8": "int64",
	"|u1": "uint8", "<u2": "uint16", "<u4": "uint32", "<u8": "uint64", "<f4": "float32", "<f8": "float64",
}

// Array writes an array's metadata; its chunks follow through the ArrayWriter.
func (w *Writer) Array(p string, spec ArraySpec) (*ArrayWriter, error) {
	if spec.Chunks == nil {
		spec.Chunks = make([]int, len(spec.Shape))
		for i, n := range spec.Shape {
			spec.Chunks[i] = max(n, 1)
		}
	}
	if len(spec.Chunks) != len(spec.Shape) {
		return nil, fmt.Errorf("zarr: %s: chunks %v for shape %v", p, spec.Chunks, spec.Shape)
	}
	a := &ArrayWriter{w: w, path: p, spec: spec, chunk: 1, text: spec.DType == "text"}
	for _, c := range spec.Chunks {
		if c <= 0 {
			return nil, fmt.Errorf("zarr: %s: chunk shape %v", p, spec.Chunks)
		}
		a.chunk *= c
	}
	if !a.text {
		if _, ok := v3TypeNames[spec.DType]; !ok {
			return nil, fmt.Errorf("zarr: %s: dtype %q is not written", p, spec.DType)
		}
		a.item, _ = strconv.Atoi(spec.DType[2:])
	}
	attrs := spec.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	var doc []byte
	var err error
	if w.format == 3 {
		dtype, fill := "string", any("")
		codecs := []any{map[string]any{"name": "vlen-utf8", "configuration": map[string]any{}}}
		if !a.text {
			dtype, fill = v3TypeNames[spec.DType], any(0)
			if spec.DType == "|b1" {
				fill = false
			}
			bytesCodec := map[string]any{"name": "bytes", "configuration": map[string]any{"endian": "little"}}
			if a.item == 1 {
				bytesCodec = map[string]any{"name": "bytes"}
			}
			codecs = []any{bytesCodec}
		}
		codecs = append(codecs, map[string]any{"name": "gzip", "configuration": map[string]any{"level": w.Level}})
		meta := map[string]any{
			"zarr_format": 3, "node_type": "array", "shape": spec.Shape, "data_type": dtype,
			"chunk_grid":         map[string]any{"name": "regular", "configuration": map[string]any{"chunk_shape": spec.Chunks}},
			"chunk_key_encoding": map[string]any{"name": "default", "configuration": map[string]any{"separator": "/"}},
			"fill_value":         fill, "codecs": codecs, "attributes": attrs,
		}
		if spec.Dimensions != nil {
			meta["dimension_names"] = spec.Dimensions
		}
		if doc, err = jsonDoc(meta); err != nil {
			return nil, err
		}
		return a, w.file(join(p, "zarr.json"), doc)
	}
	meta := map[string]any{
		"zarr_format": 2, "shape": spec.Shape, "chunks": spec.Chunks, "dtype": spec.DType, "fill_value": 0,
		"order": "C", "filters": nil, "dimension_separator": "/",
		"compressor": map[string]any{"id": "zlib", "level": w.Level},
	}
	if a.text {
		meta["dtype"], meta["filters"] = "|O", []any{map[string]any{"id": "vlen-utf8"}}
	} else if spec.DType == "|b1" {
		meta["fill_value"] = false
	}
	if doc, err = jsonDoc(meta); err != nil {
		return nil, err
	}
	if err := w.file(join(p, ".zarray"), doc); err != nil {
		return nil, err
	}
	if doc, err = jsonDoc(attrs); err != nil {
		return nil, err
	}
	return a, w.file(join(p, ".zattrs"), doc)
}

func (a *ArrayWriter) key(idx []int) (string, error) {
	if len(idx) != len(a.spec.Shape) {
		return "", fmt.Errorf("zarr: %s: chunk index %v", a.path, idx)
	}
	parts := make([]string, len(idx))
	for i, v := range idx {
		if v < 0 || v*a.spec.Chunks[i] >= max(a.spec.Shape[i], 1) {
			return "", fmt.Errorf("zarr: %s: chunk %v is outside the array", a.path, idx)
		}
		parts[i] = strconv.Itoa(v)
	}
	key := strings.Join(parts, "/")
	switch {
	case a.w.format == 3 && key == "":
		key = "c"
	case a.w.format == 3:
		key = "c/" + key
	case key == "":
		key = "0"
	}
	return join(a.path, key), nil
}

func (a *ArrayWriter) compress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if a.w.format == 3 {
		z, err := gzip.NewWriterLevel(&buf, a.w.Level)
		if err != nil {
			return nil, err
		}
		z.Write(raw)
		err = z.Close()
		return buf.Bytes(), err
	}
	z, err := zlib.NewWriterLevel(&buf, a.w.Level)
	if err != nil {
		return nil, err
	}
	z.Write(raw)
	err = z.Close()
	return buf.Bytes(), err
}

// WriteChunk writes the chunk at grid index idx: its whole chunk shape of elements (an edge
// chunk padded), row-major, little-endian.
func (a *ArrayWriter) WriteChunk(idx []int, data []byte) error {
	if a.text {
		return fmt.Errorf("zarr: %s holds text", a.path)
	}
	if len(data) != a.chunk*a.item {
		return fmt.Errorf("zarr: %s: a chunk is %d bytes, not %d", a.path, a.chunk*a.item, len(data))
	}
	key, err := a.key(idx)
	if err != nil {
		return err
	}
	packed, err := a.compress(data)
	if err != nil {
		return err
	}
	return a.w.file(key, packed)
}

// WriteText writes a chunk of text (a one-dimensional array's, padded with "" at its edge).
func (a *ArrayWriter) WriteText(idx []int, values []string) error {
	if !a.text {
		return fmt.Errorf("zarr: %s does not hold text", a.path)
	}
	if len(values) > a.chunk {
		return fmt.Errorf("zarr: %s: %d values for a chunk of %d", a.path, len(values), a.chunk)
	}
	var raw bytes.Buffer
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(a.chunk))
	raw.Write(n[:])
	for i := 0; i < a.chunk; i++ {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		binary.LittleEndian.PutUint32(n[:], uint32(len(v)))
		raw.Write(n[:])
		raw.WriteString(v)
	}
	key, err := a.key(idx)
	if err != nil {
		return err
	}
	packed, err := a.compress(raw.Bytes())
	if err != nil {
		return err
	}
	return a.w.file(key, packed)
}

// WriteArray writes a one-dimensional array (or a zero-dimensional one, of one value) whole:
// data is its elements, little-endian, split into chunks of chunk elements (0: one chunk).
func (w *Writer) WriteArray(p string, dtype string, shape []int, data []byte, chunk int, attrs map[string]any) error {
	if len(shape) > 1 {
		return w.writeDense(p, dtype, shape, data, attrs)
	}
	n := 1
	if len(shape) == 1 {
		n = shape[0]
	}
	var chunks []int
	if len(shape) == 1 {
		if chunk <= 0 {
			chunk = max(n, 1)
		}
		chunks = []int{chunk}
	} else {
		chunks = []int{}
	}
	a, err := w.Array(p, ArraySpec{Shape: shape, Chunks: chunks, DType: dtype, Attrs: attrs})
	if err != nil {
		return err
	}
	if len(data) != n*a.item {
		return fmt.Errorf("zarr: %s: %d bytes for %d elements of %d", p, len(data), n, a.item)
	}
	if len(shape) == 0 {
		return a.WriteChunk(nil, data)
	}
	if n == 0 {
		return nil
	}
	size := a.chunk * a.item
	for c := 0; c*a.chunk < n; c++ {
		part := data[c*size : min((c+1)*size, len(data))]
		if len(part) < size {
			part = append(append([]byte{}, part...), make([]byte, size-len(part))...)
		}
		if err := a.WriteChunk([]int{c}, part); err != nil {
			return err
		}
	}
	return nil
}

// writeDense writes a two-dimensional array in one chunk.
func (w *Writer) writeDense(p string, dtype string, shape []int, data []byte, attrs map[string]any) error {
	if len(shape) != 2 {
		return errors.New("zarr: only one- and two-dimensional arrays are written whole")
	}
	a, err := w.Array(p, ArraySpec{Shape: shape, DType: dtype, Attrs: attrs})
	if err != nil {
		return err
	}
	if len(data) != a.chunk*a.item {
		return fmt.Errorf("zarr: %s: %d bytes for %v elements of %d", p, len(data), shape, a.item)
	}
	if shape[0] == 0 || shape[1] == 0 {
		return nil
	}
	return a.WriteChunk([]int{0, 0}, data)
}

// WriteTextArray writes a one-dimensional array of text in chunks of chunk values (0: one chunk),
// or, with scalar, a zero-dimensional array of its one value.
func (w *Writer) WriteTextArray(p string, values []string, chunk int, attrs map[string]any, scalar bool) error {
	if scalar {
		if len(values) != 1 {
			return fmt.Errorf("zarr: %s: a scalar holds one value", p)
		}
		a, err := w.Array(p, ArraySpec{Shape: []int{}, Chunks: []int{}, DType: "text", Attrs: attrs})
		if err != nil {
			return err
		}
		return a.WriteText(nil, values)
	}
	if chunk <= 0 {
		chunk = max(len(values), 1)
	}
	a, err := w.Array(p, ArraySpec{Shape: []int{len(values)}, Chunks: []int{chunk}, DType: "text", Attrs: attrs})
	if err != nil {
		return err
	}
	for c := 0; c*chunk < len(values); c++ {
		if err := a.WriteText([]int{c}, values[c*chunk:min((c+1)*chunk, len(values))]); err != nil {
			return err
		}
	}
	return nil
}
