package imports

// Baysor-style and GeoJSON imports: a file of cell polygons (GeoJSON) and a table assigning
// transcripts to cells (CSV, gzipped CSV, TSV or Parquet), either of which may be alone.
//
// The table's columns are found by their usual names: transcript_id (matched to the dataset's
// transcripts; without it, x and y with the gene are, within 0.01 µm), the cell (named by the
// options, else cell for Baysor, cell_id, cell or assignment), and Baysor's is_noise. A cell of
// "", UNASSIGNED, NA, nan, None or −1 is none; Baysor numbers its cells from 1, so its 0 is none too.

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"situcraft/internal/parquet"
)

var (
	idColumns   = []string{"transcript_id"}
	xColumns    = []string{"x", "x_location", "global_x", "x_um"}
	yColumns    = []string{"y", "y_location", "global_y", "y_um"}
	geneColumns = []string{"gene", "feature_name", "target", "gene_name"}
	cellColumns = []string{"cell_id", "cell", "assignment", "segmentation_cell", "cell_ID"}
)

var unassignedValues = map[string]bool{"": true, "UNASSIGNED": true, "NA": true, "nan": true, "NaN": true, "None": true, "none": true, "-1": true}

func isPolygonFile(p string) bool {
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".geojson") || strings.HasSuffix(l, ".json") || strings.HasSuffix(l, ".geojson.gz") || strings.HasSuffix(l, ".json.gz")
}

func isTableFile(p string) bool {
	l := strings.ToLower(p)
	for _, s := range []string{".csv", ".csv.gz", ".tsv", ".tsv.gz", ".parquet"} {
		if strings.HasSuffix(l, s) {
			return true
		}
	}
	return false
}

func readPolygonsAndTable(ctx context.Context, opt Options, scale float64) (*source, error) {
	var polygonsPath, tablePath string
	for _, p := range opt.Paths {
		switch {
		case isPolygonFile(p) && polygonsPath == "":
			polygonsPath = p
		case isTableFile(p) && tablePath == "":
			tablePath = p
		default:
			return nil, fmt.Errorf("%s is neither polygons (.geojson, .json) nor a table (.csv, .tsv, .parquet), or is one too many", filepath.Base(p))
		}
	}
	src := newSource()
	src.toolName, src.toolVer, src.citation = opt.Tool, opt.ToolVersion, opt.Citation
	if src.toolName == "" && opt.Format == Baysor {
		src.toolName = "Baysor"
	}
	if polygonsPath == "" && tablePath == "" {
		return nil, errors.New("name a polygons file, an assignment table, or both")
	}
	if polygonsPath != "" {
		raw, err := readMaybeGzip(polygonsPath)
		if err != nil {
			return nil, err
		}
		if err := src.hashFile(polygonsPath); err != nil {
			return nil, err
		}
		property := opt.CellProperty
		if property == "" && opt.Format == Baysor {
			property = "cell"
		}
		shapes, used, err := readGeoJSON(raw, property)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(polygonsPath), err)
		}
		src.read["polygons"] = filepath.Base(polygonsPath)
		src.read["cell_property"] = used
		src.polygons = [][]polygon{}
		repeated := 0
		for _, s := range shapes {
			if opt.Format == Baysor && s.id == "0" || unassignedValues[s.id] {
				continue
			}
			if _, seen := src.index[s.id]; seen {
				repeated++
			}
			k := src.cell(s.id)
			for _, p := range s.polygons {
				src.polygons[k] = append(src.polygons[k], scalePolygon(p, scale))
			}
		}
		src.find("info", "polygons.repeated", "Cells given by more than one feature (their polygons kept together)", repeated)
	}
	if tablePath != "" {
		if err := src.hashFile(tablePath); err != nil {
			return nil, err
		}
		if err := readTable(ctx, src, tablePath, opt, scale); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(tablePath), err)
		}
		src.read["table"] = filepath.Base(tablePath)
	}
	if src.polygons != nil {
		without := 0
		for _, p := range src.polygons {
			if len(p) == 0 {
				without++
			}
		}
		src.find("info", "cells.no-polygon", "Cells the table names that have no polygon (they have transcripts but no area)", without)
	}
	return src, nil
}

func scalePolygon(p polygon, s float64) polygon {
	if s == 1 {
		return p
	}
	out := make(polygon, len(p))
	for i, r := range p {
		out[i] = make([]point, len(r))
		for k, v := range r {
			out[i][k] = point{v.X * s, v.Y * s}
		}
	}
	return out
}

func readMaybeGzip(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		r = z
	}
	return io.ReadAll(io.LimitReader(r, 4<<30))
}

// tableColumns finds a table's columns by their usual names.
type tableColumns struct {
	id, x, y, gene, cell, noise int
	cellName                    string
}

func findColumns(header []string, opt Options) (tableColumns, error) {
	at := func(names []string) int {
		for _, n := range names {
			for i, h := range header {
				if h == n {
					return i
				}
			}
		}
		return -1
	}
	c := tableColumns{id: at(idColumns), x: at(xColumns), y: at(yColumns), gene: at(geneColumns), noise: at([]string{"is_noise"})}
	names := cellColumns
	if opt.Format == Baysor {
		names = append([]string{"cell"}, cellColumns...)
	}
	if opt.CellColumn != "" {
		names = []string{opt.CellColumn}
	}
	c.cell = at(names)
	if c.cell < 0 {
		return c, fmt.Errorf("no cell column (%s) among %v", strings.Join(names, ", "), header)
	}
	c.cellName = header[c.cell]
	if c.id < 0 && (c.x < 0 || c.y < 0 || c.gene < 0) {
		return c, fmt.Errorf("the table names its transcripts neither by transcript_id nor by x, y and gene (its columns: %v)", header)
	}
	return c, nil
}

// row adds one row of the table to the records.
func (src *source) row(r *records, cols tableColumns, get func(int) string, opt Options, scale float64) error {
	cell := strings.TrimSpace(get(cols.cell))
	if cols.noise >= 0 {
		if v := strings.ToLower(get(cols.noise)); v == "true" || v == "1" {
			cell = ""
		}
	}
	k := int32(-1)
	if !(unassignedValues[cell] || opt.Format == Baysor && cell == "0") {
		k = src.cell(cell)
	}
	if r.byID {
		id, err := strconv.ParseUint(strings.TrimSpace(get(cols.id)), 10, 64)
		if err != nil {
			return fmt.Errorf("transcript_id %q is not a whole number", get(cols.id))
		}
		r.id = append(r.id, id)
	} else {
		x, err1 := strconv.ParseFloat(strings.TrimSpace(get(cols.x)), 64)
		y, err2 := strconv.ParseFloat(strings.TrimSpace(get(cols.y)), 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("x %q and y %q are not numbers", get(cols.x), get(cols.y))
		}
		r.x, r.y, r.gene = append(r.x, x*scale), append(r.y, y*scale), append(r.gene, get(cols.gene))
	}
	r.cell = append(r.cell, k)
	return nil
}

func readTable(ctx context.Context, src *source, path string, opt Options, scale float64) error {
	r := &records{}
	if strings.HasSuffix(strings.ToLower(path), ".parquet") {
		if err := readParquetTable(ctx, src, r, path, opt, scale); err != nil {
			return err
		}
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		var in io.Reader = f
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".gz") {
			z, err := gzip.NewReader(f)
			if err != nil {
				return err
			}
			in = z
		}
		cr := csv.NewReader(in)
		cr.ReuseRecord = true
		cr.LazyQuotes = true
		if strings.HasSuffix(lower, ".tsv") || strings.HasSuffix(lower, ".tsv.gz") {
			cr.Comma = '\t'
		}
		header, err := cr.Read()
		if err != nil {
			return fmt.Errorf("no header: %w", err)
		}
		header = append([]string(nil), header...)
		cols, err := findColumns(header, opt)
		if err != nil {
			return err
		}
		r.byID = cols.id >= 0
		src.read["cell_column"] = cols.cellName
		for line := 2; ; line++ {
			rec, err := cr.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if line%65536 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			get := func(i int) string {
				if i < 0 || i >= len(rec) {
					return ""
				}
				return rec[i]
			}
			if err := src.row(r, cols, get, opt, scale); err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
		}
	}
	src.records = r
	src.read["transcripts_by"] = map[bool]string{true: "transcript_id", false: "position and gene"}[r.byID]
	return nil
}

func readParquetTable(ctx context.Context, src *source, r *records, path string, opt Options, scale float64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	p, err := parquet.Open(f, info.Size())
	if err != nil {
		return err
	}
	header := make([]string, len(p.Columns))
	for i, c := range p.Columns {
		header[i] = c.Name
	}
	cols, err := findColumns(header, opt)
	if err != nil {
		return err
	}
	r.byID = cols.id >= 0
	src.read["cell_column"] = cols.cellName
	return eachParquetRow(ctx, p, []int{cols.id, cols.x, cols.y, cols.gene, cols.cell, cols.noise}, func(get func(int) string) error {
		return src.row(r, cols, get, opt, scale)
	})
}

// eachParquetRow calls fn for each row with a getter of the named columns' values as text.
func eachParquetRow(ctx context.Context, p *parquet.File, wanted []int, fn func(get func(int) string) error) error {
	for g := range p.RowGroups {
		if err := ctx.Err(); err != nil {
			return err
		}
		values := map[int]*parquet.Values{}
		for _, c := range wanted {
			if c < 0 {
				continue
			}
			v, err := p.ReadColumn(g, c)
			if err != nil {
				return err
			}
			values[c] = v
		}
		for i := 0; i < int(p.RowGroups[g].NumRows); i++ {
			get := func(c int) string {
				v := values[c]
				if v == nil {
					return ""
				}
				return valueText(v, i, p.Columns[c])
			}
			if err := fn(get); err != nil {
				return err
			}
		}
	}
	return nil
}

// valueText is a Parquet value as text ("" when null).
func valueText(v *parquet.Values, i int, col parquet.Column) string {
	if v.Valid != nil && !v.Valid[i] {
		return ""
	}
	switch {
	case v.Bytes != nil:
		return string(v.Bytes[i])
	case v.Int64 != nil:
		if col.Logical == "INTEGER" && !col.Signed {
			return strconv.FormatUint(uint64(v.Int64[i]), 10)
		}
		return strconv.FormatInt(v.Int64[i], 10)
	case v.Int32 != nil:
		if col.Logical == "INTEGER" && !col.Signed {
			return strconv.FormatUint(uint64(uint32(v.Int32[i])), 10)
		}
		return strconv.FormatInt(int64(v.Int32[i]), 10)
	case v.Float64 != nil:
		return strconv.FormatFloat(v.Float64[i], 'g', -1, 64)
	case v.Float32 != nil:
		return strconv.FormatFloat(float64(v.Float32[i]), 'g', -1, 32)
	case v.Bool != nil:
		return strconv.FormatBool(v.Bool[i])
	}
	return ""
}
