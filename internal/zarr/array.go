package zarr

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"strconv"
	"strings"

	"situcraft/internal/codec/blosc"
	"situcraft/internal/codec/lz4"
	"situcraft/internal/codec/zstd"
)

// DType is an array's element type.
type DType struct {
	// Kind is numpy's kind character: 'b' bool, 'i' signed, 'u' unsigned, 'f' float, 'c' complex,
	// 'S' bytes, 'U' UTF-32 text, 'V' raw.
	Kind byte
	// Size is the element's size in bytes.
	Size int
	// BigEndian is the stored byte order (values are returned little-endian).
	BigEndian bool
}

func (d DType) String() string {
	order := "<"
	if d.BigEndian {
		order = ">"
	}
	if d.Size == 1 || d.Kind == 'S' || d.Kind == 'V' || d.Kind == 'b' {
		order = "|"
	}
	size := d.Size
	if d.Kind == 'U' {
		size /= 4
	}
	return fmt.Sprintf("%s%c%d", order, d.Kind, size)
}

// Array is one Zarr array.
type Array struct {
	Path       string
	ZarrFormat int
	Shape      []int
	Chunks     []int
	DType      DType
	// Order is the element order within a chunk: 'C' (row-major) or 'F' (column-major).
	Order byte
	// Fill is one element's bytes (little-endian), used for chunks never written.
	Fill  []byte
	Attrs map[string]any
	// Codec names the compressor, for messages ("blosc", "zstd", "none", …).
	Codec string

	store      Store
	compressor *codecSpec
	separator  string
	v3Prefix   string // "c" for format 3's default chunk keys
	v3Codecs   []codecSpec
}

type codecSpec struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Configuration json.RawMessage `json:"configuration"`
	Endian        string          `json:"-"`
	Order         []int           `json:"-"`
}

type v2Meta struct {
	ZarrFormat         int             `json:"zarr_format"`
	Shape              []int           `json:"shape"`
	Chunks             []int           `json:"chunks"`
	DType              json.RawMessage `json:"dtype"`
	Compressor         *codecSpec      `json:"compressor"`
	FillValue          json.RawMessage `json:"fill_value"`
	Order              string          `json:"order"`
	Filters            []codecSpec     `json:"filters"`
	DimensionSeparator string          `json:"dimension_separator"`
}

type v3Meta struct {
	ZarrFormat int    `json:"zarr_format"`
	NodeType   string `json:"node_type"`
	Shape      []int  `json:"shape"`
	DataType   any    `json:"data_type"`
	ChunkGrid  struct {
		Name          string `json:"name"`
		Configuration struct {
			ChunkShape []int `json:"chunk_shape"`
		} `json:"configuration"`
	} `json:"chunk_grid"`
	ChunkKeyEncoding struct {
		Name          string `json:"name"`
		Configuration struct {
			Separator string `json:"separator"`
		} `json:"configuration"`
	} `json:"chunk_key_encoding"`
	FillValue  json.RawMessage `json:"fill_value"`
	Codecs     []codecSpec     `json:"codecs"`
	Attributes map[string]any  `json:"attributes"`
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return strings.TrimSuffix(prefix, "/") + "/" + name
}

// OpenArray reads the array at path ("" for the root) from its .zarray (format 2) or zarr.json
// (format 3).
func OpenArray(s Store, arrayPath string) (*Array, error) {
	arrayPath = strings.Trim(arrayPath, "/")
	if raw, err := s.Get(join(arrayPath, ".zarray")); err == nil {
		return openV2(s, arrayPath, raw)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	raw, err := s.Get(join(arrayPath, "zarr.json"))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("zarr: no array at %q", arrayPath)
	}
	if err != nil {
		return nil, err
	}
	return openV3(s, arrayPath, raw)
}

func openV2(s Store, arrayPath string, raw []byte) (*Array, error) {
	var m v2Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("zarr: %s/.zarray: %w", arrayPath, err)
	}
	if m.ZarrFormat != 2 {
		return nil, fmt.Errorf("zarr: %s/.zarray has zarr_format %d", arrayPath, m.ZarrFormat)
	}
	var dtype string
	if err := json.Unmarshal(m.DType, &dtype); err != nil {
		return nil, fmt.Errorf("zarr: %s: structured dtypes are not read", arrayPath)
	}
	d, err := parseDType(dtype)
	if err != nil {
		return nil, fmt.Errorf("zarr: %s: %w", arrayPath, err)
	}
	a := &Array{Path: arrayPath, ZarrFormat: 2, Shape: m.Shape, Chunks: m.Chunks, DType: d, store: s, compressor: m.Compressor, separator: m.DimensionSeparator, Codec: "none"}
	if a.separator == "" {
		a.separator = "."
	}
	if a.separator != "." && a.separator != "/" {
		return nil, fmt.Errorf("zarr: %s: dimension separator %q", arrayPath, a.separator)
	}
	switch m.Order {
	case "C", "":
		a.Order = 'C'
	case "F":
		a.Order = 'F'
	default:
		return nil, fmt.Errorf("zarr: %s: order %q", arrayPath, m.Order)
	}
	if len(m.Filters) > 0 {
		ids := make([]string, len(m.Filters))
		for i, f := range m.Filters {
			ids[i] = f.ID
		}
		return nil, fmt.Errorf("zarr: %s: filters (%s) are not read", arrayPath, strings.Join(ids, ", "))
	}
	if m.Compressor != nil {
		a.Codec = m.Compressor.ID
	}
	if a.Fill, err = fillValue(m.FillValue, d); err != nil {
		return nil, fmt.Errorf("zarr: %s: %w", arrayPath, err)
	}
	if raw, err := s.Get(join(arrayPath, ".zattrs")); err == nil {
		json.Unmarshal(raw, &a.Attrs)
	}
	return a, a.check()
}

var v3Types = map[string]DType{
	"bool": {'b', 1, false}, "int8": {'i', 1, false}, "int16": {'i', 2, false}, "int32": {'i', 4, false}, "int64": {'i', 8, false},
	"uint8": {'u', 1, false}, "uint16": {'u', 2, false}, "uint32": {'u', 4, false}, "uint64": {'u', 8, false},
	"float16": {'f', 2, false}, "float32": {'f', 4, false}, "float64": {'f', 8, false},
	"complex64": {'c', 8, false}, "complex128": {'c', 16, false},
}

func openV3(s Store, arrayPath string, raw []byte) (*Array, error) {
	var m v3Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("zarr: %s/zarr.json: %w", arrayPath, err)
	}
	if m.ZarrFormat != 3 || m.NodeType != "array" {
		return nil, fmt.Errorf("zarr: %s/zarr.json is not a format 3 array", arrayPath)
	}
	name, _ := m.DataType.(string)
	d, ok := v3Types[name]
	if !ok {
		return nil, fmt.Errorf("zarr: %s: data type %v is not read", arrayPath, m.DataType)
	}
	if m.ChunkGrid.Name != "regular" {
		return nil, fmt.Errorf("zarr: %s: chunk grid %q is not read", arrayPath, m.ChunkGrid.Name)
	}
	a := &Array{Path: arrayPath, ZarrFormat: 3, Shape: m.Shape, Chunks: m.ChunkGrid.Configuration.ChunkShape, DType: d, Order: 'C', store: s, Attrs: m.Attributes, Codec: "none"}
	switch m.ChunkKeyEncoding.Name {
	case "default", "":
		a.v3Prefix, a.separator = "c", m.ChunkKeyEncoding.Configuration.Separator
		if a.separator == "" {
			a.separator = "/"
		}
	case "v2":
		a.separator = m.ChunkKeyEncoding.Configuration.Separator
		if a.separator == "" {
			a.separator = "."
		}
	default:
		return nil, fmt.Errorf("zarr: %s: chunk key encoding %q", arrayPath, m.ChunkKeyEncoding.Name)
	}
	for _, c := range m.Codecs {
		switch c.Name {
		case "bytes":
			var cfg struct {
				Endian string `json:"endian"`
			}
			json.Unmarshal(c.Configuration, &cfg)
			c.Endian = cfg.Endian
			a.DType.BigEndian = cfg.Endian == "big"
		case "transpose":
			var cfg struct {
				Order []int `json:"order"`
			}
			json.Unmarshal(c.Configuration, &cfg)
			c.Order = cfg.Order
			if isReverse(cfg.Order) && len(cfg.Order) > 1 {
				a.Order = 'F'
			} else if !isIdentity(cfg.Order) {
				return nil, fmt.Errorf("zarr: %s: transpose order %v is not read", arrayPath, cfg.Order)
			}
		case "blosc", "gzip", "zstd", "crc32c":
			if c.Name != "crc32c" {
				a.Codec = c.Name
			}
		case "sharding_indexed":
			return nil, fmt.Errorf("zarr: %s: sharded arrays are not read", arrayPath)
		default:
			return nil, fmt.Errorf("zarr: %s: codec %q is not read", arrayPath, c.Name)
		}
		a.v3Codecs = append(a.v3Codecs, c)
	}
	var err error
	if a.Fill, err = fillValue(m.FillValue, a.DType); err != nil {
		return nil, fmt.Errorf("zarr: %s: %w", arrayPath, err)
	}
	return a, a.check()
}

func isIdentity(order []int) bool {
	for i, v := range order {
		if v != i {
			return false
		}
	}
	return true
}

func isReverse(order []int) bool {
	for i, v := range order {
		if v != len(order)-1-i {
			return false
		}
	}
	return true
}

func (a *Array) check() error {
	if len(a.Shape) != len(a.Chunks) {
		return fmt.Errorf("zarr: %s: shape %v and chunks %v differ in rank", a.Path, a.Shape, a.Chunks)
	}
	elements := int64(1)
	for i := range a.Shape {
		if a.Shape[i] < 0 || a.Chunks[i] <= 0 {
			return fmt.Errorf("zarr: %s: shape %v, chunks %v", a.Path, a.Shape, a.Chunks)
		}
		elements *= int64(a.Chunks[i])
		if elements*int64(a.DType.Size) > maxValueBytes {
			return fmt.Errorf("zarr: %s: chunks of %v elements are too large", a.Path, a.Chunks)
		}
	}
	return nil
}

func parseDType(s string) (DType, error) {
	if len(s) < 3 {
		return DType{}, fmt.Errorf("dtype %q", s)
	}
	d := DType{Kind: s[1], BigEndian: s[0] == '>'}
	if s[0] != '<' && s[0] != '>' && s[0] != '|' {
		return DType{}, fmt.Errorf("dtype %q", s)
	}
	n, err := strconv.Atoi(s[2:])
	if err != nil || n <= 0 || n > 1<<20 {
		return DType{}, fmt.Errorf("dtype %q", s)
	}
	switch d.Kind {
	case 'b', 'i', 'u', 'f', 'c', 'S', 'V':
		d.Size = n
	case 'U':
		d.Size = 4 * n
	default:
		return DType{}, fmt.Errorf("dtype %q is not read", s)
	}
	if d.Kind == 'b' && n != 1 || d.Kind == 'f' && n != 2 && n != 4 && n != 8 || d.Kind == 'c' && n != 8 && n != 16 || (d.Kind == 'i' || d.Kind == 'u') && n != 1 && n != 2 && n != 4 && n != 8 {
		return DType{}, fmt.Errorf("dtype %q", s)
	}
	return d, nil
}

// fillValue is one element of the fill value, little-endian.
func fillValue(raw json.RawMessage, d DType) ([]byte, error) {
	out := make([]byte, d.Size)
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && d.Kind != 'f' && d.Kind != 'c' {
		// Bytes, text and raw fills are base64 (format 2's convention).
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("fill value %q", s)
		}
		copy(out, b)
		return out, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("fill value %s", raw)
	}
	var f float64
	switch x := v.(type) {
	case bool:
		if x {
			out[0] = 1
		}
		return out, nil
	case float64:
		f = x
	case string:
		switch x {
		case "NaN":
			f = math.NaN()
		case "Infinity":
			f = math.Inf(1)
		case "-Infinity":
			f = math.Inf(-1)
		default:
			return nil, fmt.Errorf("fill value %q", x)
		}
	default:
		return nil, fmt.Errorf("fill value %s", raw)
	}
	switch d.Kind {
	case 'f':
		switch d.Size {
		case 4:
			binary.LittleEndian.PutUint32(out, math.Float32bits(float32(f)))
		case 8:
			binary.LittleEndian.PutUint64(out, math.Float64bits(f))
		}
	case 'i', 'u':
		var n int64
		if exact, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
			n = exact
		} else {
			n = int64(f)
		}
		for b := 0; b < d.Size; b++ {
			out[b] = byte(n >> (8 * b))
		}
	}
	return out, nil
}

// grid is the number of chunks along each dimension.
func (a *Array) grid() []int {
	g := make([]int, len(a.Shape))
	for i := range g {
		g[i] = (a.Shape[i] + a.Chunks[i] - 1) / a.Chunks[i]
	}
	return g
}

func (a *Array) chunkKey(idx []int) string {
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = strconv.Itoa(v)
	}
	key := strings.Join(parts, a.separator)
	if a.v3Prefix != "" {
		if len(idx) == 0 {
			key = a.v3Prefix
		} else {
			key = a.v3Prefix + a.separator + key
		}
	} else if len(idx) == 0 {
		key = "0"
	}
	return join(a.Path, key)
}

// ReadChunk decodes the chunk at grid index idx: every element of the chunk's full shape (chunks
// at the array's edges are padded), little-endian, in the array's order. A chunk never written
// is the fill value.
func (a *Array) ReadChunk(idx []int) ([]byte, error) {
	if len(idx) != len(a.Shape) {
		return nil, fmt.Errorf("zarr: %s: chunk index %v for rank %d", a.Path, idx, len(a.Shape))
	}
	for i, g := range a.grid() {
		if idx[i] < 0 || idx[i] >= g {
			return nil, fmt.Errorf("zarr: %s: chunk index %v outside the grid", a.Path, idx)
		}
	}
	n := a.DType.Size
	for _, c := range a.Chunks {
		n *= c
	}
	stored, err := a.store.Get(a.chunkKey(idx))
	if errors.Is(err, ErrNotFound) {
		out := make([]byte, n)
		for i := 0; i < n; i += a.DType.Size {
			copy(out[i:], a.Fill)
		}
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var data []byte
	if a.ZarrFormat == 3 {
		data, err = a.decodeV3(stored, n)
	} else {
		data, err = decodeV2(a.compressor, stored, n)
	}
	if err != nil {
		return nil, fmt.Errorf("zarr: %s chunk %v: %w", a.Path, idx, err)
	}
	if len(data) != n {
		return nil, fmt.Errorf("zarr: %s chunk %v decodes to %d bytes, not %d", a.Path, idx, len(data), n)
	}
	if a.DType.BigEndian {
		swap(data, a.DType)
	}
	return data, nil
}

func decodeV2(c *codecSpec, stored []byte, n int) ([]byte, error) {
	if c == nil {
		return stored, nil
	}
	switch c.ID {
	case "blosc":
		return blosc.Decode(stored, n)
	case "zstd":
		return zstd.Decode(make([]byte, 0, n), stored, n)
	case "zlib":
		r, err := zlib.NewReader(bytes.NewReader(stored))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(io.LimitReader(r, int64(n)+1))
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(stored))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(io.LimitReader(r, int64(n)+1))
	case "lz4":
		// numcodecs' LZ4: the decoded size as 4 little-endian bytes, then one LZ4 block.
		if len(stored) < 4 || int(binary.LittleEndian.Uint32(stored)) != n {
			return nil, errors.New("lz4 chunk without its size")
		}
		out := make([]byte, n)
		got, err := lz4.DecodeBlock(out, stored[4:])
		return out[:got], err
	}
	return nil, fmt.Errorf("compressor %q is not read", c.ID)
}

// decodeV3 runs format 3's bytes-to-bytes codecs in reverse (array-to-bytes and transpose were
// read into the dtype and order).
func (a *Array) decodeV3(stored []byte, n int) ([]byte, error) {
	data := stored
	for i := len(a.v3Codecs) - 1; i >= 0; i-- {
		switch c := a.v3Codecs[i]; c.Name {
		case "crc32c":
			if len(data) < 4 {
				return nil, errors.New("chunk too short for its checksum")
			}
			body, sum := data[:len(data)-4], binary.LittleEndian.Uint32(data[len(data)-4:])
			if crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)) != sum {
				return nil, errors.New("CRC-32C checksum mismatch")
			}
			data = body
		case "blosc":
			out, err := blosc.Decode(data, n)
			if err != nil {
				return nil, err
			}
			data = out
		case "zstd":
			out, err := zstd.Decode(make([]byte, 0, n), data, n)
			if err != nil {
				return nil, err
			}
			data = out
		case "gzip":
			r, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			if data, err = io.ReadAll(io.LimitReader(r, int64(n)+1)); err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

// swap makes big-endian elements little-endian (each component of a complex number separately;
// a 'U' code point is 4 bytes).
func swap(data []byte, d DType) {
	width := d.Size
	switch d.Kind {
	case 'c':
		width = d.Size / 2
	case 'U':
		width = 4
	case 'S', 'V', 'b':
		return
	}
	if width <= 1 {
		return
	}
	for i := 0; i+width <= len(data); i += width {
		for l, r := i, i+width-1; l < r; l, r = l+1, r-1 {
			data[l], data[r] = data[r], data[l]
		}
	}
}

// ReadAll decodes the whole array, row-major (C order) and little-endian.
func (a *Array) ReadAll() ([]byte, error) {
	elements := int64(1)
	for _, s := range a.Shape {
		elements *= int64(s)
	}
	size := int(elements) * a.DType.Size
	if elements*int64(a.DType.Size) > maxValueBytes {
		return nil, fmt.Errorf("zarr: %s (%v) is too large to read whole", a.Path, a.Shape)
	}
	out := make([]byte, size)
	if elements == 0 {
		return out, nil
	}
	rank := len(a.Shape)
	item := a.DType.Size
	if rank == 0 {
		chunk, err := a.ReadChunk(nil)
		if err != nil {
			return nil, err
		}
		return chunk[:item], nil
	}
	strides := cStrides(a.Shape)
	grid := a.grid()
	idx := make([]int, rank)
	for {
		chunk, err := a.ReadChunk(idx)
		if err != nil {
			return nil, err
		}
		if a.Order == 'F' {
			chunk = fToC(chunk, a.Chunks, item)
		}
		chunkStrides := cStrides(a.Chunks)
		// Copy the part of the chunk inside the array, one run along the last dimension at a time.
		origin := make([]int, rank)
		extent := make([]int, rank)
		for d := 0; d < rank; d++ {
			origin[d] = idx[d] * a.Chunks[d]
			extent[d] = min(a.Chunks[d], a.Shape[d]-origin[d])
		}
		local := make([]int, rank)
		for {
			src, dst := 0, 0
			for d := 0; d < rank; d++ {
				src += local[d] * chunkStrides[d]
				dst += (origin[d] + local[d]) * strides[d]
			}
			copy(out[dst*item:(dst+extent[rank-1])*item], chunk[src*item:])
			if !advance(local[:rank-1], extent[:rank-1]) {
				break
			}
		}
		if !advance(idx, grid) {
			break
		}
	}
	return out, nil
}

// advance steps an odometer (last dimension fastest); false when it wraps around.
func advance(idx, limits []int) bool {
	for d := len(idx) - 1; d >= 0; d-- {
		idx[d]++
		if idx[d] < limits[d] {
			return true
		}
		idx[d] = 0
	}
	return false
}

func cStrides(shape []int) []int {
	strides := make([]int, len(shape))
	stride := 1
	for d := len(shape) - 1; d >= 0; d-- {
		strides[d] = stride
		stride *= shape[d]
	}
	return strides
}

// fToC reorders a column-major chunk row-major.
func fToC(chunk []byte, shape []int, item int) []byte {
	rank := len(shape)
	out := make([]byte, len(chunk))
	cs := cStrides(shape)
	fs := make([]int, rank)
	stride := 1
	for d := 0; d < rank; d++ {
		fs[d] = stride
		stride *= shape[d]
	}
	idx := make([]int, rank)
	for {
		c, f := 0, 0
		for d := 0; d < rank; d++ {
			c += idx[d] * cs[d]
			f += idx[d] * fs[d]
		}
		copy(out[c*item:(c+1)*item], chunk[f*item:])
		if !advance(idx, shape) {
			return out
		}
	}
}

// Attributes reads the attributes of the group or array at path (.zattrs, or zarr.json's).
func Attributes(s Store, nodePath string) (map[string]any, error) {
	nodePath = strings.Trim(nodePath, "/")
	if raw, err := s.Get(join(nodePath, ".zattrs")); err == nil {
		var attrs map[string]any
		if err := json.Unmarshal(raw, &attrs); err != nil {
			return nil, fmt.Errorf("zarr: %s/.zattrs: %w", nodePath, err)
		}
		return attrs, nil
	}
	if raw, err := s.Get(join(nodePath, "zarr.json")); err == nil {
		var m struct {
			Attributes map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("zarr: %s/zarr.json: %w", nodePath, err)
		}
		return m.Attributes, nil
	}
	return map[string]any{}, nil
}

// Arrays lists the paths of every array in the store.
func Arrays(s Store) ([]string, error) {
	keys, err := s.Keys()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		switch {
		case k == ".zarray":
			out = append(out, "")
		case strings.HasSuffix(k, "/.zarray"):
			out = append(out, strings.TrimSuffix(k, "/.zarray"))
		case k == "zarr.json" || strings.HasSuffix(k, "/zarr.json"):
			raw, err := s.Get(k)
			if err != nil {
				continue
			}
			var m struct {
				NodeType string `json:"node_type"`
			}
			if json.Unmarshal(raw, &m) == nil && m.NodeType == "array" {
				out = append(out, strings.TrimSuffix(strings.TrimSuffix(k, "zarr.json"), "/"))
			}
		}
	}
	return out, nil
}
