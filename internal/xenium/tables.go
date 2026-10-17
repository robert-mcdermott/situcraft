package xenium

// The bundle's tables, typed: cells, transcripts (read a row group at a time, as large bundles
// hold hundreds of millions), cell and nucleus boundaries, and the cell-by-feature matrix from its
// Zarr or Matrix Market form.

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"situcraft/internal/parquet"
	"situcraft/internal/zarr"
)

// CellIDString writes a cell ID as Xenium does: the 32-bit prefix as eight hexadecimal digits
// shifted to the letters a–p ("aaafiiei"), a hyphen, and the dataset suffix ("-1").
func CellIDString(prefix, suffix uint32) string {
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = 'a' + byte(prefix&15)
		prefix >>= 4
	}
	return string(b[:]) + "-" + strconv.FormatUint(uint64(suffix), 10)
}

// ParseCellID reads a cell ID written by CellIDString.
func ParseCellID(s string) (prefix, suffix uint32, ok bool) {
	head, tail, found := strings.Cut(s, "-")
	if !found || len(head) != 8 {
		return 0, 0, false
	}
	for i := 0; i < 8; i++ {
		c := head[i]
		if c < 'a' || c > 'p' {
			return 0, 0, false
		}
		prefix = prefix<<4 | uint32(c-'a')
	}
	n, err := strconv.ParseUint(tail, 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return prefix, uint32(n), true
}

// Feature kinds, as the Zarr matrix names them.
const (
	KindGene             = "gene"
	KindNegControlProbe  = "negative_control_probe"
	KindNegControlCode   = "negative_control_codeword"
	KindUnassignedCode   = "unassigned_codeword"
	KindDeprecatedCode   = "deprecated_codeword"
	KindGenomicControl   = "genomic_control"
	KindAggregate        = "aggregate_gene"
	unassignedCellMarker = "UNASSIGNED"
)

// FeatureKind classifies a transcript's feature name by the prefixes Xenium gives controls.
func FeatureKind(name string) string {
	switch {
	case strings.HasPrefix(name, "NegControlProbe_"):
		return KindNegControlProbe
	case strings.HasPrefix(name, "NegControlCodeword_"):
		return KindNegControlCode
	case strings.HasPrefix(name, "UnassignedCodeword_"), strings.HasPrefix(name, "BLANK_"):
		return KindUnassignedCode
	case strings.HasPrefix(name, "DeprecatedCodeword_"):
		return KindDeprecatedCode
	case strings.HasPrefix(name, "Intergenic_Region") || strings.HasPrefix(name, "GenomicControl"):
		return KindGenomicControl
	}
	return KindGene
}

// normalizeKind maps the Matrix Market features file's type names to the Zarr matrix's.
func normalizeKind(s string) string {
	switch s {
	case "Gene Expression":
		return KindGene
	case "Negative Control Probe":
		return KindNegControlProbe
	case "Negative Control Codeword":
		return KindNegControlCode
	case "Unassigned Codeword", "Blank Codeword":
		return KindUnassignedCode
	case "Deprecated Codeword":
		return KindDeprecatedCode
	case "Genomic Control":
		return KindGenomicControl
	}
	return strings.ToLower(strings.ReplaceAll(s, " ", "_"))
}

// openParquet opens one of the bundle's Parquet files.
func (b *Bundle) openParquet(name string) (*parquet.File, ReaderAt, error) {
	f, err := b.OpenFile(name)
	if err != nil {
		return nil, nil, err
	}
	p, err := parquet.Open(f, f.Size())
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return p, f, nil
}

// column is a named column's values, checked for one of the expected physical types.
func column(p *parquet.File, file, name string, types ...parquet.Type) (*parquet.Values, *parquet.Column, error) {
	i := p.Index(name)
	if i < 0 {
		return nil, nil, fmt.Errorf("%s has no column %q", file, name)
	}
	col := &p.Columns[i]
	ok := len(types) == 0
	for _, t := range types {
		ok = ok || col.Type == t
	}
	if !ok {
		return nil, nil, fmt.Errorf("%s: column %q is %v, not %v", file, name, col.Type, types)
	}
	v, err := p.ReadAll(i)
	return v, col, err
}

// floats are a numeric column's values as float64.
func floats(v *parquet.Values) []float64 {
	out := make([]float64, v.Len)
	switch {
	case v.Float64 != nil:
		copy(out, v.Float64)
	case v.Float32 != nil:
		for i, x := range v.Float32 {
			out[i] = float64(x)
		}
	case v.Int64 != nil:
		for i, x := range v.Int64 {
			out[i] = float64(x)
		}
	case v.Int32 != nil:
		for i, x := range v.Int32 {
			out[i] = float64(x)
		}
	}
	return out
}

// ints are an integer column's values as int64 (nil for other types).
func ints(v *parquet.Values) []int64 {
	switch {
	case v.Int64 != nil:
		return v.Int64
	case v.Int32 != nil:
		out := make([]int64, v.Len)
		for i, x := range v.Int32 {
			out[i] = int64(x)
		}
		return out
	}
	return nil
}

// Cells is cells.parquet.
type Cells struct {
	IDs   []string
	Index map[string]int32
	X, Y  []float64 // centroids, µm
	// Counts by kind (nil when the bundle's version does not write that column).
	TranscriptCounts, ControlProbeCounts, ControlCodewordCounts []int64
	UnassignedCodewordCounts, DeprecatedCodewordCounts          []int64
	TotalCounts                                                 []int64
	CellArea, NucleusArea                                       []float64 // µm²
	// Columns lists every column the file has.
	Columns []string
}

// ReadCells reads cells.parquet whole (a bundle holds at most a few million cells).
func (b *Bundle) ReadCells() (*Cells, error) {
	const name = "cells.parquet"
	p, f, err := b.openParquet(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Cells{Index: map[string]int32{}}
	for _, col := range p.Columns {
		c.Columns = append(c.Columns, col.Name)
	}
	ids, _, err := column(p, name, "cell_id", parquet.ByteArray, parquet.Int32, parquet.Int64)
	if err != nil {
		return nil, err
	}
	c.IDs = idStrings(ids)
	for i := range c.IDs {
		if _, dup := c.Index[c.IDs[i]]; dup {
			return nil, fmt.Errorf("%s: cell %s appears twice", name, c.IDs[i])
		}
		c.Index[c.IDs[i]] = int32(i)
	}
	for _, xy := range []struct {
		name string
		dst  *[]float64
	}{{"x_centroid", &c.X}, {"y_centroid", &c.Y}} {
		v, _, err := column(p, name, xy.name, parquet.Float, parquet.Double)
		if err != nil {
			return nil, err
		}
		*xy.dst = floats(v)
	}
	optional := func(column string) *parquet.Values {
		i := p.Index(column)
		if i < 0 {
			return nil
		}
		v, err := p.ReadAll(i)
		if err != nil {
			return nil
		}
		return v
	}
	for _, counts := range []struct {
		name string
		dst  *[]int64
	}{
		{"transcript_counts", &c.TranscriptCounts}, {"control_probe_counts", &c.ControlProbeCounts},
		{"control_codeword_counts", &c.ControlCodewordCounts}, {"unassigned_codeword_counts", &c.UnassignedCodewordCounts},
		{"deprecated_codeword_counts", &c.DeprecatedCodewordCounts}, {"total_counts", &c.TotalCounts},
	} {
		if v := optional(counts.name); v != nil {
			*counts.dst = ints(v)
		}
	}
	if v := optional("cell_area"); v != nil {
		c.CellArea = floats(v)
	}
	if v := optional("nucleus_area"); v != nil {
		c.NucleusArea = floats(v)
	}
	return c, nil
}

// TranscriptBatch is one row group of transcripts.parquet.
type TranscriptBatch struct {
	Rows            int
	CellID          [][]byte // "UNASSIGNED" when the transcript is in no cell
	Feature         [][]byte
	X, Y, Z, QV     []float32
	OverlapsNucleus []int32 // nil when absent
	// TranscriptID is each transcript's identity (nil when the file has no transcript_id column).
	TranscriptID []uint64
}

// TranscriptColumns are the columns transcripts must have.
var TranscriptColumns = []string{"cell_id", "feature_name", "x_location", "y_location", "z_location", "qv"}

// Transcripts is an open transcripts.parquet.
type Transcripts struct {
	File *parquet.File
	f    ReaderAt
	idx  map[string]int
	only map[string]bool // the columns Each decodes (nil: all)
}

// Only has Each decode just the named columns, leaving the batch's others nil: a row group's
// decoded columns are what reading it costs in memory, the text columns most.
func (t *Transcripts) Only(columns ...string) {
	t.only = map[string]bool{}
	for _, c := range columns {
		t.only[c] = true
	}
}

func (t *Transcripts) wants(column string) bool { return t.only == nil || t.only[column] }

// OpenTranscripts opens transcripts.parquet, checking it has the columns SituCraft reads.
func (b *Bundle) OpenTranscripts() (*Transcripts, error) {
	const name = "transcripts.parquet"
	p, f, err := b.openParquet(name)
	if err != nil {
		return nil, err
	}
	t := &Transcripts{File: p, f: f, idx: map[string]int{}}
	for _, c := range append(TranscriptColumns, "overlaps_nucleus", "transcript_id") {
		t.idx[c] = p.Index(c)
	}
	for _, c := range TranscriptColumns {
		if t.idx[c] < 0 {
			f.Close()
			return nil, fmt.Errorf("%s has no column %q", name, c)
		}
	}
	return t, nil
}

// Close releases the file.
func (t *Transcripts) Close() error { return t.f.Close() }

// Each calls fn with each row group in turn.
func (t *Transcripts) Each(fn func(*TranscriptBatch) error) error {
	toFloat32 := func(v *parquet.Values) []float32 {
		if v.Float32 != nil {
			return v.Float32
		}
		out := make([]float32, v.Len)
		for i, x := range floats(v) {
			out[i] = float32(x)
		}
		return out
	}
	for g := range t.File.RowGroups {
		read := func(name string) (*parquet.Values, error) {
			return t.File.ReadColumn(g, t.idx[name])
		}
		batch := &TranscriptBatch{Rows: int(t.File.RowGroups[g].NumRows)}
		if t.wants("cell_id") {
			cell, err := read("cell_id")
			if err != nil {
				return err
			}
			if cell.Bytes == nil {
				// Older bundles numbered cells; a number is still an identity.
				batch.CellID = make([][]byte, cell.Len)
				for i, n := range ints(cell) {
					batch.CellID[i] = []byte(strconv.FormatInt(n, 10))
					if n < 0 {
						batch.CellID[i] = []byte(unassignedCellMarker)
					}
				}
			} else {
				batch.CellID = cell.Bytes
			}
		}
		if t.wants("feature_name") {
			feature, err := read("feature_name")
			if err != nil {
				return err
			}
			if feature.Bytes == nil {
				return errors.New("transcripts.parquet: feature_name is not text")
			}
			batch.Feature = feature.Bytes
		}
		for _, xyz := range []struct {
			name string
			dst  *[]float32
		}{{"x_location", &batch.X}, {"y_location", &batch.Y}, {"z_location", &batch.Z}, {"qv", &batch.QV}} {
			if !t.wants(xyz.name) {
				continue
			}
			v, err := read(xyz.name)
			if err != nil {
				return err
			}
			*xyz.dst = toFloat32(v)
		}
		if t.idx["overlaps_nucleus"] >= 0 && t.wants("overlaps_nucleus") {
			v, err := read("overlaps_nucleus")
			if err != nil {
				return err
			}
			batch.OverlapsNucleus = v.Int32
		}
		if t.idx["transcript_id"] >= 0 && t.wants("transcript_id") {
			v, err := read("transcript_id")
			if err != nil {
				return err
			}
			ids := ints(v)
			batch.TranscriptID = make([]uint64, len(ids))
			for i, id := range ids {
				batch.TranscriptID[i] = uint64(id)
			}
		}
		if err := fn(batch); err != nil {
			return err
		}
	}
	return nil
}

// Polygons are cell or nucleus boundaries: polygon i has vertices Offsets[i] to Offsets[i+1].
type Polygons struct {
	CellIDs []string
	Offsets []int32
	X, Y    []float32 // µm
}

// ReadBoundaries reads cell_boundaries.parquet or nucleus_boundaries.parquet: each run of rows with
// one cell_id is one polygon (a cell with several, a new run, is another polygon).
func (b *Bundle) ReadBoundaries(name string) (*Polygons, error) {
	p, f, err := b.openParquet(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	idv, _, err := column(p, name, "cell_id", parquet.ByteArray, parquet.Int32, parquet.Int64)
	if err != nil {
		return nil, err
	}
	ids := idStrings(idv)
	xs, _, err := column(p, name, "vertex_x", parquet.Float, parquet.Double)
	if err != nil {
		return nil, err
	}
	ys, _, err := column(p, name, "vertex_y", parquet.Float, parquet.Double)
	if err != nil {
		return nil, err
	}
	poly := &Polygons{X: make([]float32, len(ids)), Y: make([]float32, len(ids))}
	x, y := floats(xs), floats(ys)
	for i := range ids {
		poly.X[i], poly.Y[i] = float32(x[i]), float32(y[i])
		if i == 0 || ids[i] != poly.CellIDs[len(poly.CellIDs)-1] {
			poly.CellIDs = append(poly.CellIDs, ids[i])
			poly.Offsets = append(poly.Offsets, int32(i))
		}
	}
	poly.Offsets = append(poly.Offsets, int32(len(ids)))
	return poly, nil
}

// idStrings are a cell_id column's values as text: XOA 1.0 numbered its cells (from 1), and a
// number is still an identity.
func idStrings(v *parquet.Values) []string {
	out := make([]string, v.Len)
	if v.Bytes != nil {
		for i, b := range v.Bytes {
			out[i] = string(b)
		}
		return out
	}
	for i, n := range ints(v) {
		out[i] = strconv.FormatInt(n, 10)
	}
	return out
}

// Feature is one row of the cell-by-feature matrix.
type Feature struct {
	ID, Name, Kind string
}

// Matrix is the cell-by-feature counts, compressed by feature: feature f's counts are
// Data[Indptr[f]:Indptr[f+1]] for the cells Indices[…].
type Matrix struct {
	Source   string // the file it was read from
	Features []Feature
	CellIDs  []string
	Indptr   []int64
	Indices  []int32
	Data     []float64
}

// ReadMatrix reads the matrix from its Zarr form, else its Matrix Market form.
func (b *Bundle) ReadMatrix() (*Matrix, error) {
	switch {
	case b.Has("cell_feature_matrix.zarr.zip"):
		return b.readZarrMatrix("cell_feature_matrix.zarr.zip")
	case b.Has("cell_feature_matrix/matrix.mtx.gz"):
		files := map[string][]byte{}
		for _, n := range []string{"matrix.mtx.gz", "features.tsv.gz", "barcodes.tsv.gz"} {
			f, err := b.OpenFile("cell_feature_matrix/" + n)
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.NewSectionReader(f, 0, f.Size()))
			f.Close()
			if err != nil {
				return nil, err
			}
			files[n] = data
		}
		return readMEX(files, "cell_feature_matrix/")
	case b.Has("cell_feature_matrix.tar.gz"):
		return b.readMEXArchive("cell_feature_matrix.tar.gz")
	}
	return nil, errors.New("the bundle has no cell-feature matrix SituCraft reads (Zarr or Matrix Market)")
}

func (b *Bundle) readZarrMatrix(name string) (*Matrix, error) {
	f, err := b.OpenFile(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	store, err := zarr.OpenZip(f, f.Size())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	group := "cell_features"
	attrs, err := zarr.Attributes(store, group)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	m := &Matrix{Source: name}
	strs := func(key string) []string {
		list, _ := attrs[key].([]any)
		out := make([]string, len(list))
		for i, v := range list {
			out[i], _ = v.(string)
		}
		return out
	}
	ids, keys, kinds := strs("feature_ids"), strs("feature_keys"), strs("feature_types")
	if len(keys) == 0 || len(ids) != len(keys) || len(kinds) != len(keys) {
		return nil, fmt.Errorf("%s: the feature lists in %s's attributes are missing or uneven", name, group)
	}
	for i := range keys {
		m.Features = append(m.Features, Feature{ID: ids[i], Name: keys[i], Kind: kinds[i]})
	}
	read := func(array string) ([]byte, *zarr.Array, error) {
		a, err := zarr.OpenArray(store, group+"/"+array)
		if err != nil {
			return nil, nil, err
		}
		data, err := a.ReadAll()
		return data, a, err
	}
	if cellIDs, a, err := read("cell_id"); err == nil {
		if len(a.Shape) != 2 || a.Shape[1] != 2 || a.DType.Size != 4 {
			return nil, fmt.Errorf("%s: cell_id has shape %v", name, a.Shape)
		}
		for i := 0; i < a.Shape[0]; i++ {
			m.CellIDs = append(m.CellIDs, CellIDString(le32(cellIDs[8*i:]), le32(cellIDs[8*i+4:])))
		}
	} else if n, ok := attrs["number_cells"].(float64); ok && n >= 0 && n == float64(int(n)) {
		// XOA 1.0 kept no cell IDs here: its cells are numbered from 1, in cells.parquet's order.
		for i := 1; i <= int(n); i++ {
			m.CellIDs = append(m.CellIDs, strconv.Itoa(i))
		}
	} else {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	indptr, a, err := read("indptr")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	m.Indptr = unsignedInts(indptr, a.DType.Size)
	indices, a, err := read("indices")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for _, v := range unsignedInts(indices, a.DType.Size) {
		m.Indices = append(m.Indices, int32(v))
	}
	data, a, err := read("data")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if a.DType.Kind == 'f' {
		return nil, fmt.Errorf("%s: floating-point counts are not read", name)
	}
	for _, v := range unsignedInts(data, a.DType.Size) {
		m.Data = append(m.Data, float64(v))
	}
	return m, m.check()
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func unsignedInts(b []byte, size int) []int64 {
	out := make([]int64, len(b)/size)
	for i := range out {
		var v uint64
		for k := size - 1; k >= 0; k-- {
			v = v<<8 | uint64(b[i*size+k])
		}
		out[i] = int64(v)
	}
	return out
}

// check makes sure the compressed layout is consistent before anything indexes with it.
func (m *Matrix) check() error {
	if len(m.Indptr) != len(m.Features)+1 || m.Indptr[0] != 0 || m.Indptr[len(m.Indptr)-1] != int64(len(m.Indices)) || len(m.Indices) != len(m.Data) {
		return fmt.Errorf("%s: the matrix's index arrays are inconsistent (%d features, %d pointers, %d entries)", m.Source, len(m.Features), len(m.Indptr), len(m.Indices))
	}
	for f := 0; f+1 < len(m.Indptr); f++ {
		if m.Indptr[f+1] < m.Indptr[f] {
			return fmt.Errorf("%s: the matrix's pointers decrease at feature %d", m.Source, f)
		}
	}
	for _, c := range m.Indices {
		if c < 0 || int(c) >= len(m.CellIDs) {
			return fmt.Errorf("%s: a count refers to cell %d of %d", m.Source, c, len(m.CellIDs))
		}
	}
	return nil
}

func (b *Bundle) readMEXArchive(name string) (*Matrix, error) {
	f, err := b.OpenFile(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(io.NewSectionReader(f, 0, f.Size()))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		base := h.Name[strings.LastIndex(h.Name, "/")+1:]
		if base == "matrix.mtx.gz" || base == "features.tsv.gz" || base == "barcodes.tsv.gz" {
			if h.Size > 1<<32 {
				return nil, fmt.Errorf("%s: %s is %d bytes", name, base, h.Size)
			}
			data, err := io.ReadAll(io.LimitReader(tr, h.Size))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			files[base] = data
		}
	}
	return readMEX(files, name)
}

// readMEX reads the Matrix Market exchange form: matrix.mtx.gz (features × cells, 1-based
// coordinates), features.tsv.gz (ID, name, type) and barcodes.tsv.gz (cell IDs).
func readMEX(files map[string][]byte, source string) (*Matrix, error) {
	lines := func(name string) ([]string, error) {
		data, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("%s: no %s", source, name)
		}
		gz, err := gzip.NewReader(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("%s%s: %w", source, name, err)
		}
		var out []string
		scanner := bufio.NewScanner(gz)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			out = append(out, scanner.Text())
		}
		return out, scanner.Err()
	}
	m := &Matrix{Source: source}
	features, err := lines("features.tsv.gz")
	if err != nil {
		return nil, err
	}
	for _, l := range features {
		parts := strings.Split(l, "\t")
		f := Feature{ID: parts[0], Name: parts[0], Kind: KindGene}
		if len(parts) > 1 {
			f.Name = parts[1]
		}
		if len(parts) > 2 {
			f.Kind = normalizeKind(parts[2])
		}
		m.Features = append(m.Features, f)
	}
	if m.CellIDs, err = lines("barcodes.tsv.gz"); err != nil {
		return nil, err
	}
	entries, err := lines("matrix.mtx.gz")
	if err != nil {
		return nil, err
	}
	var rows, cols, nnz int
	header := true
	type entry struct {
		feature, cell int32
		value         float64
	}
	var all []entry
	for n, l := range entries {
		if strings.HasPrefix(l, "%") || strings.TrimSpace(l) == "" {
			continue
		}
		fields := strings.Fields(l)
		if header {
			if len(fields) != 3 {
				return nil, fmt.Errorf("%smatrix.mtx.gz: line %d is not a size line", source, n+1)
			}
			rows, _ = strconv.Atoi(fields[0])
			cols, _ = strconv.Atoi(fields[1])
			nnz, _ = strconv.Atoi(fields[2])
			if rows != len(m.Features) || cols != len(m.CellIDs) {
				return nil, fmt.Errorf("%smatrix.mtx.gz is %d×%d but there are %d features and %d barcodes", source, rows, cols, len(m.Features), len(m.CellIDs))
			}
			if nnz < 0 || nnz > len(entries) {
				return nil, fmt.Errorf("%smatrix.mtx.gz declares %d entries", source, nnz)
			}
			all = make([]entry, 0, nnz)
			header = false
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("%smatrix.mtx.gz: line %d", source, n+1)
		}
		r, err1 := strconv.Atoi(fields[0])
		c, err2 := strconv.Atoi(fields[1])
		v, err3 := strconv.ParseFloat(fields[2], 64)
		if err1 != nil || err2 != nil || err3 != nil || r < 1 || r > rows || c < 1 || c > cols {
			return nil, fmt.Errorf("%smatrix.mtx.gz: line %d is not an entry inside the matrix", source, n+1)
		}
		all = append(all, entry{int32(r - 1), int32(c - 1), v})
	}
	if len(all) != nnz {
		return nil, fmt.Errorf("%smatrix.mtx.gz holds %d entries, not the %d it declares", source, len(all), nnz)
	}
	// Compress by feature, keeping each feature's cells in the file's order.
	m.Indptr = make([]int64, len(m.Features)+1)
	for _, e := range all {
		m.Indptr[e.feature+1]++
	}
	for f := 1; f < len(m.Indptr); f++ {
		m.Indptr[f] += m.Indptr[f-1]
	}
	m.Indices = make([]int32, len(all))
	m.Data = make([]float64, len(all))
	next := append([]int64(nil), m.Indptr[:len(m.Features)]...)
	for _, e := range all {
		m.Indices[next[e.feature]] = e.cell
		m.Data[next[e.feature]] = e.value
		next[e.feature]++
	}
	return m, m.check()
}
