package export

// The tables (Parquet and gzipped CSV, in Xenium's column names), the Matrix Market exchange
// form and GeoJSON polygons.
//
//	cells            cell_id, x_centroid, y_centroid (µm), transcript_counts (gene transcripts of
//	                 quality 20 or more), assigned_transcripts (every transcript assigned),
//	                 detected_genes, cell_area, nucleus_area (µm²), nucleus_id
//	transcripts      transcript_id (when the dataset has them), cell_id ("UNASSIGNED" for none),
//	                 feature_name, x_location, y_location, z_location (µm), qv
//	cell_boundaries  cell_id, vertex_x, vertex_y (µm): each outline's vertices, the first repeated
//	                 last, as Xenium writes them
//	counts           cell_id, feature_name, count: the cell-by-gene counts' nonzero entries

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"situcraft/internal/parquet"
	"situcraft/internal/xenium"
)

// rowsPerGroup bounds a table's row group (and a CSV's batch).
const rowsPerGroup = 1 << 20

// sink writes one table's batches of columns.
type sink interface {
	write(cols []*parquet.Values) error
	close() error
}

type parquetSink struct {
	f   *os.File
	buf *bufio.Writer
	w   *parquet.Writer
}

func (s *parquetSink) write(cols []*parquet.Values) error { return s.w.WriteRowGroup(cols) }
func (s *parquetSink) close() error {
	err := s.w.Close()
	if e := s.buf.Flush(); err == nil {
		err = e
	}
	if e := s.f.Close(); err == nil {
		err = e
	}
	return err
}

func newParquet(path string, fields []parquet.Field, software string, meta map[string]string) (*parquetSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	buf := bufio.NewWriterSize(f, 1<<20)
	w, err := parquet.NewWriter(buf, fields, software)
	if err != nil {
		f.Close()
		return nil, err
	}
	for _, k := range sortedKeys(meta) {
		w.SetKeyValue(k, meta[k])
	}
	return &parquetSink{f: f, buf: buf, w: w}, nil
}

type csvSink struct {
	f      *os.File
	gz     *gzip.Writer
	buf    *bufio.Writer
	w      *csv.Writer
	fields []parquet.Field
	row    []string
}

func newCSV(path string, fields []parquet.Field) (*csvSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	buf := bufio.NewWriterSize(gz, 1<<20)
	s := &csvSink{f: f, gz: gz, buf: buf, w: csv.NewWriter(buf), fields: fields, row: make([]string, len(fields))}
	for i, field := range fields {
		s.row[i] = field.Name
	}
	return s, s.w.Write(s.row)
}

func (s *csvSink) write(cols []*parquet.Values) error {
	n := length(s.fields[0], cols[0])
	for r := 0; r < n; r++ {
		for i, f := range s.fields {
			s.row[i] = text(f, cols[i], r)
		}
		if err := s.w.Write(s.row); err != nil {
			return err
		}
	}
	return nil
}

func (s *csvSink) close() error {
	s.w.Flush()
	err := s.w.Error()
	if e := s.buf.Flush(); err == nil {
		err = e
	}
	if e := s.gz.Close(); err == nil {
		err = e
	}
	if e := s.f.Close(); err == nil {
		err = e
	}
	return err
}

func length(f parquet.Field, v *parquet.Values) int {
	switch f.Type {
	case parquet.Int32:
		return len(v.Int32)
	case parquet.Int64:
		return len(v.Int64)
	case parquet.Float:
		return len(v.Float32)
	case parquet.Double:
		return len(v.Float64)
	case parquet.Boolean:
		return len(v.Bool)
	default:
		return len(v.Bytes)
	}
}

// text is value r as CSV text: numbers in their shortest exact form.
func text(f parquet.Field, v *parquet.Values, r int) string {
	switch f.Type {
	case parquet.Int32:
		if f.Annotation == "uint" {
			return strconv.FormatUint(uint64(uint32(v.Int32[r])), 10)
		}
		return strconv.FormatInt(int64(v.Int32[r]), 10)
	case parquet.Int64:
		if f.Annotation == "uint" {
			return strconv.FormatUint(uint64(v.Int64[r]), 10)
		}
		return strconv.FormatInt(v.Int64[r], 10)
	case parquet.Float:
		return strconv.FormatFloat(float64(v.Float32[r]), 'g', -1, 32)
	case parquet.Double:
		return strconv.FormatFloat(v.Float64[r], 'g', -1, 64)
	case parquet.Boolean:
		return strconv.FormatBool(v.Bool[r])
	default:
		return string(v.Bytes[r])
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func str(name string) parquet.Field {
	return parquet.Field{Name: name, Type: parquet.ByteArray, Annotation: "string"}
}
func u32(name string) parquet.Field {
	return parquet.Field{Name: name, Type: parquet.Int32, Annotation: "uint"}
}
func f32(name string) parquet.Field { return parquet.Field{Name: name, Type: parquet.Float} }
func f64(name string) parquet.Field { return parquet.Field{Name: name, Type: parquet.Double} }
func u64(name string) parquet.Field {
	return parquet.Field{Name: name, Type: parquet.Int64, Annotation: "uint"}
}

// CellFields, TranscriptFields, BoundaryFields and CountFields are the tables' columns.
var (
	CellFields       = []parquet.Field{str("cell_id"), f64("x_centroid"), f64("y_centroid"), u32("transcript_counts"), u32("assigned_transcripts"), u32("detected_genes"), f64("cell_area"), f64("nucleus_area"), str("nucleus_id")}
	TranscriptFields = []parquet.Field{u64("transcript_id"), str("cell_id"), str("feature_name"), f32("x_location"), f32("y_location"), f32("z_location"), f32("qv")}
	BoundaryFields   = []parquet.Field{str("cell_id"), f32("vertex_x"), f32("vertex_y")}
	CountFields      = []parquet.Field{str("cell_id"), str("feature_name"), u32("count")}
)

func columns(fields []parquet.Field, n int) []*parquet.Values {
	out := make([]*parquet.Values, len(fields))
	for i, f := range fields {
		v := &parquet.Values{Len: n}
		switch f.Type {
		case parquet.Int32:
			v.Int32 = make([]int32, 0, n)
		case parquet.Int64:
			v.Int64 = make([]int64, 0, n)
		case parquet.Float:
			v.Float32 = make([]float32, 0, n)
		case parquet.Double:
			v.Float64 = make([]float64, 0, n)
		default:
			v.Bytes = make([][]byte, 0, n)
		}
		out[i] = v
	}
	return out
}

// cellBatches calls fn with the cells table in batches.
func (d *data) cellBatches(fn func([]*parquet.Values) error) error {
	for from := 0; from < len(d.cells) || from == 0; from += rowsPerGroup {
		to := min(from+rowsPerGroup, len(d.cells))
		c := columns(CellFields, to-from)
		for _, cell := range d.cells[from:to] {
			c[0].Bytes = append(c[0].Bytes, []byte(cell.ID))
			c[1].Float64 = append(c[1].Float64, float64(cell.X))
			c[2].Float64 = append(c[2].Float64, float64(cell.Y))
			c[3].Int32 = append(c[3].Int32, int32(cell.Transcripts))
			c[4].Int32 = append(c[4].Int32, int32(cell.Assigned))
			c[5].Int32 = append(c[5].Int32, int32(cell.Genes))
			c[6].Float64 = append(c[6].Float64, float64(cell.Area))
			c[7].Float64 = append(c[7].Float64, float64(cell.NucleusArea))
			c[8].Bytes = append(c[8].Bytes, []byte(cell.Nucleus))
		}
		if err := fn(c); err != nil {
			return err
		}
		if to == len(d.cells) {
			break
		}
	}
	return nil
}

// transcriptBatches calls fn with the transcripts table, a batch of the dataset's at a time
// (without transcript_id when the dataset has none).
func (d *data) transcriptBatches(ctx context.Context, withIDs func(bool) error, fn func([]*parquet.Values) error) error {
	at := 0
	first := true
	ids := map[uint32][]byte{}
	cellBytes := func(k uint32) []byte {
		if b, ok := ids[k]; ok {
			return b
		}
		b := []byte(d.cellID(k))
		ids[k] = b
		return b
	}
	return d.ds.Transcripts(func(b *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if first {
			first = false
			if err := withIDs(b.TranscriptID != nil); err != nil {
				return err
			}
		}
		if at+b.Rows > len(d.assign) {
			return fmt.Errorf("the dataset has more transcripts than the segmentation assigns (%d)", len(d.assign))
		}
		c := columns(TranscriptFields, b.Rows)
		for i := 0; i < b.Rows; i++ {
			if b.TranscriptID != nil {
				c[0].Int64 = append(c[0].Int64, int64(b.TranscriptID[i]))
			}
			c[1].Bytes = append(c[1].Bytes, cellBytes(d.assign[at+i]))
			c[2].Bytes = append(c[2].Bytes, b.Feature[i])
			c[3].Float32 = append(c[3].Float32, b.X[i])
			c[4].Float32 = append(c[4].Float32, b.Y[i])
			c[5].Float32 = append(c[5].Float32, b.Z[i])
			c[6].Float32 = append(c[6].Float32, b.QV[i])
		}
		at += b.Rows
		if b.TranscriptID == nil {
			c = c[1:]
		}
		return fn(c)
	})
}

func (d *data) boundaryBatches(fn func([]*parquet.Values) error) error {
	c := columns(BoundaryFields, 0)
	flush := func() error {
		if len(c[0].Bytes) == 0 {
			return nil
		}
		err := fn(c)
		c = columns(BoundaryFields, 0)
		return err
	}
	for i, o := range d.outlines {
		if len(o.X) < 3 {
			continue
		}
		id := []byte(d.cells[i].ID)
		for k := 0; k <= len(o.X); k++ {
			j := k % len(o.X)
			c[0].Bytes = append(c[0].Bytes, id)
			c[1].Float32 = append(c[1].Float32, o.X[j])
			c[2].Float32 = append(c[2].Float32, o.Y[j])
		}
		if len(c[0].Bytes) >= rowsPerGroup {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (d *data) countBatches(fn func([]*parquet.Values) error) error {
	genes := make([][]byte, len(d.counts.Genes))
	for i, g := range d.counts.Genes {
		genes[i] = []byte(g)
	}
	c := columns(CountFields, 0)
	flush := func() error {
		if len(c[0].Bytes) == 0 {
			return nil
		}
		err := fn(c)
		c = columns(CountFields, 0)
		return err
	}
	for i, cell := range d.cells {
		id := []byte(cell.ID)
		for k := d.counts.Offsets[i]; k < d.counts.Offsets[i+1]; k++ {
			c[0].Bytes = append(c[0].Bytes, id)
			c[1].Bytes = append(c[1].Bytes, genes[d.counts.Gene[k]])
			c[2].Int32 = append(c[2].Int32, int32(d.counts.Count[k]))
		}
		if len(c[0].Bytes) >= rowsPerGroup {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// writeTables writes the four tables as Parquet or gzipped CSV into dir.
func writeTables(ctx context.Context, d *data, dir, format string, step Progress) error {
	open := func(name string, fields []parquet.Field) (sink, error) {
		if format == Parquet {
			return newParquet(filepath.Join(dir, name+".parquet"), fields, d.software, nil)
		}
		return newCSV(filepath.Join(dir, name+".csv.gz"), fields)
	}
	table := func(name string, fields []parquet.Field, batches func(func([]*parquet.Values) error) error) error {
		s, err := open(name, fields)
		if err != nil {
			return err
		}
		if err := batches(s.write); err != nil {
			s.close()
			return err
		}
		return s.close()
	}
	if err := table("cells", CellFields, d.cellBatches); err != nil {
		return err
	}
	step(0.1, "Writing the transcripts")
	var s sink
	err := d.transcriptBatches(ctx, func(ids bool) error {
		fields := TranscriptFields
		if !ids {
			fields = fields[1:]
		}
		var err error
		s, err = open("transcripts", fields)
		return err
	}, func(c []*parquet.Values) error { return s.write(c) })
	if s != nil {
		if e := s.close(); err == nil {
			err = e
		}
	}
	if err != nil {
		return err
	}
	step(0.8, "Writing the outlines and counts")
	if err := table("cell_boundaries", BoundaryFields, d.boundaryBatches); err != nil {
		return err
	}
	return table("counts", CountFields, d.countBatches)
}

// gzipFile writes a gzipped text file through fn.
func gzipFile(path string, fn func(*bufio.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	w := bufio.NewWriterSize(gz, 1<<20)
	err = fn(w)
	if e := w.Flush(); err == nil {
		err = e
	}
	if e := gz.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

// writeMEX writes the counts in the Matrix Market exchange form: genes by cells, 1-based.
func writeMEX(d *data, dir string) error {
	err := gzipFile(filepath.Join(dir, "features.tsv.gz"), func(w *bufio.Writer) error {
		for _, g := range d.counts.Genes {
			id := d.geneIDs[g]
			if id == "" {
				id = g
			}
			fmt.Fprintf(w, "%s\t%s\tGene Expression\n", id, g)
		}
		return nil
	})
	if err != nil {
		return err
	}
	err = gzipFile(filepath.Join(dir, "barcodes.tsv.gz"), func(w *bufio.Writer) error {
		for _, c := range d.cells {
			w.WriteString(c.ID)
			w.WriteByte('\n')
		}
		return nil
	})
	if err != nil {
		return err
	}
	return gzipFile(filepath.Join(dir, "matrix.mtx.gz"), func(w *bufio.Writer) error {
		meta, _ := json.Marshal(map[string]string{"software_version": d.software, "segmentation": d.seg.ID, "counts": "gene transcripts of quality 20 or more"})
		fmt.Fprintf(w, "%%%%MatrixMarket matrix coordinate integer general\n%%metadata_json: %s\n", meta)
		fmt.Fprintf(w, "%d %d %d\n", len(d.counts.Genes), len(d.cells), len(d.counts.Count))
		var line []byte
		for c := range d.cells {
			for k := d.counts.Offsets[c]; k < d.counts.Offsets[c+1]; k++ {
				line = strconv.AppendUint(line[:0], uint64(d.counts.Gene[k])+1, 10)
				line = append(line, ' ')
				line = strconv.AppendUint(line, uint64(c)+1, 10)
				line = append(line, ' ')
				line = strconv.AppendUint(line, uint64(d.counts.Count[k]), 10)
				line = append(line, '\n')
				w.Write(line)
			}
		}
		return nil
	})
}

// writeGeoJSON writes each cell with an outline as a polygon feature (µm).
func writeGeoJSON(d *data, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	meta, _ := json.Marshal(map[string]any{"units": "µm", "software": d.software, "segmentation": d.seg})
	fmt.Fprintf(w, "{\"type\":\"FeatureCollection\",\"situcraft\":%s,\"features\":[", meta)
	first := true
	var num []byte
	coord := func(v float32) {
		num = strconv.AppendFloat(num[:0], float64(v), 'g', -1, 32)
		w.Write(num)
	}
	for i, o := range d.outlines {
		if len(o.X) < 3 {
			continue
		}
		c := d.cells[i]
		if !first {
			w.WriteByte(',')
		}
		first = false
		id, _ := json.Marshal(c.ID)
		w.WriteString("\n{\"type\":\"Feature\",\"id\":")
		w.Write(id)
		w.WriteString(",\"geometry\":{\"type\":\"Polygon\",\"coordinates\":[[")
		for k := 0; k <= len(o.X); k++ {
			j := k % len(o.X)
			if k > 0 {
				w.WriteByte(',')
			}
			w.WriteByte('[')
			coord(o.X[j])
			w.WriteByte(',')
			coord(o.Y[j])
			w.WriteByte(']')
		}
		props, _ := json.Marshal(map[string]any{"cell_id": c.ID, "cell_area": round(float64(c.Area)), "transcript_counts": c.Transcripts, "nucleus_id": c.Nucleus})
		w.WriteString("]]},\"properties\":")
		w.Write(props)
		w.WriteByte('}')
	}
	w.WriteString("\n]}\n")
	err = w.Flush()
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

// round keeps a float32 measure's digits (its shortest exact form).
func round(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'g', -1, 32), 64)
	return r
}
