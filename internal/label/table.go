package label

// Tables of labels made elsewhere: a CSV or TSV file (gzipped or not; Xenium Explorer's cell groups
// among them), a Parquet file, or an AnnData object's obs in Zarr (a folder or a zip of one; at its
// root, or a SpatialData store's table). Each is read whole into columns of text or numbers, with
// the SHA-256 of what was read.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"situcraft/internal/anndata"
	"situcraft/internal/parquet"
	"situcraft/internal/zarr"
)

// Formats of label tables.
const (
	FormatCSV        = "csv"
	FormatParquet    = "parquet"
	FormatAnnData    = "anndata"
	FormatExplorer   = "xenium-explorer"
	FormatInstrument = "instrument"
)

// MaxTableRows bounds what is read: a whole-slide table has a few million cells.
const MaxTableRows = 20_000_000

// Table is a table of labels as read: its columns (the index of an AnnData obs first, named as
// its index), its rows, and the SHA-256 of what was read.
type Table struct {
	Format  string
	File    string
	SHA256  string
	Rows    int
	Columns []*anndata.Column
	// Index names the AnnData obs's index column ("" for other formats).
	Index string
	// Path is where an AnnData obs was found in its store ("obs", "tables/table/obs").
	Path string
}

// Column is the column with this name, or nil.
func (t *Table) Column(name string) *anndata.Column {
	for _, c := range t.Columns {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ReadTable reads a table of labels, by its name: .csv, .tsv or .txt (gzipped or not), .parquet,
// or a Zarr store (a folder, or a .zip of one).
func ReadTable(path string) (*Table, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	l := strings.ToLower(path)
	switch {
	case info.IsDir():
		return readZarrTable(zarr.OpenDir(path), path, dirDigest(path))
	case strings.HasSuffix(l, ".zip"):
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		digest, err := fileDigest(path)
		if err != nil {
			return nil, err
		}
		s, err := zarr.OpenZip(f, info.Size())
		if err != nil {
			return nil, err
		}
		return readZarrTable(s, path, func() (string, error) { return digest, nil })
	case strings.HasSuffix(l, ".parquet"):
		return readParquetTable(path, info.Size())
	case strings.HasSuffix(l, ".csv"), strings.HasSuffix(l, ".csv.gz"), strings.HasSuffix(l, ".tsv"), strings.HasSuffix(l, ".tsv.gz"), strings.HasSuffix(l, ".txt"), strings.HasSuffix(l, ".txt.gz"):
		return readCSVTable(path)
	}
	return nil, fmt.Errorf("%s is not a table SituCraft reads: CSV or TSV (gzipped or not), Parquet, or AnnData in Zarr (a .zarr folder or a zip of one)", filepath.Base(path))
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dirDigest is the SHA-256 of a folder's files under a subfolder: each one's path and bytes, in
// order of path.
func dirDigest(root string) func(sub string) (string, error) {
	return func(sub string) (string, error) {
		var paths []string
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(sub)), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		sort.Strings(paths)
		h := sha256.New()
		for _, p := range paths {
			rel, _ := filepath.Rel(root, p)
			h.Write([]byte(filepath.ToSlash(rel) + "\n"))
			f, err := os.Open(p)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return "", err
			}
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
}

// readZarrTable reads an AnnData obs: the store's own (an AnnData object at its root), else its
// first SpatialData table's. digest hashes the obs's files (a zip, the whole file).
func readZarrTable(s zarr.Store, path string, digest any) (*Table, error) {
	obs := ""
	if attrs, err := zarr.Attributes(s, "obs"); err == nil && attrs["encoding-type"] == "dataframe" {
		obs = "obs"
	} else {
		keys, err := s.Keys()
		if err != nil {
			return nil, err
		}
		var tables []string
		seen := map[string]bool{}
		for _, k := range keys {
			parts := strings.Split(k, "/")
			if len(parts) >= 3 && parts[0] == "tables" && parts[2] == "obs" && !seen[parts[1]] {
				seen[parts[1]] = true
				tables = append(tables, parts[1])
			}
		}
		sort.Strings(tables)
		for _, t := range tables {
			if attrs, err := zarr.Attributes(s, "tables/"+t+"/obs"); err == nil && attrs["encoding-type"] == "dataframe" {
				obs = "tables/" + t + "/obs"
				break
			}
		}
	}
	if obs == "" {
		return nil, fmt.Errorf("%s holds no AnnData obs (at its root, or in a SpatialData table)", filepath.Base(path))
	}
	f, err := anndata.ReadFrame(s, obs)
	if err != nil {
		return nil, err
	}
	t := &Table{Format: FormatAnnData, File: filepath.Base(path), Rows: f.Index.Len(), Index: f.Index.Name, Path: obs}
	t.Columns = append([]*anndata.Column{f.Index}, f.Columns...)
	switch d := digest.(type) {
	case func() (string, error):
		t.SHA256, err = d()
	case func(string) (string, error):
		t.SHA256, err = d(obs)
	}
	if err != nil {
		return nil, err
	}
	if t.Rows > MaxTableRows {
		return nil, fmt.Errorf("%s has %d rows; SituCraft reads at most %d", t.File, t.Rows, MaxTableRows)
	}
	return t, nil
}

func readParquetTable(path string, size int64) (*Table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p, err := parquet.Open(f, size)
	if err != nil {
		return nil, err
	}
	if p.NumRows > MaxTableRows {
		return nil, fmt.Errorf("%s has %d rows; SituCraft reads at most %d", filepath.Base(path), p.NumRows, MaxTableRows)
	}
	t := &Table{Format: FormatParquet, File: filepath.Base(path), Rows: int(p.NumRows)}
	for c, col := range p.Columns {
		v, err := p.ReadAll(c)
		if err != nil {
			return nil, fmt.Errorf("%s: column %s: %w", t.File, col.Name, err)
		}
		if v.Len != t.Rows {
			return nil, fmt.Errorf("%s: column %s has %d values for %d rows", t.File, col.Name, v.Len, t.Rows)
		}
		out := &anndata.Column{Name: col.Name}
		if v.Valid != nil {
			for _, ok := range v.Valid {
				if !ok {
					out.Missing = make([]bool, t.Rows)
					break
				}
			}
		}
		switch {
		case v.Bytes != nil, v.Bool != nil:
			out.Text = make([]string, t.Rows)
		default:
			out.Numbers = make([]float64, t.Rows)
		}
		for i := 0; i < t.Rows; i++ {
			if v.Valid != nil && !v.Valid[i] {
				out.Missing[i] = true
				if out.Numbers != nil {
					out.Numbers[i] = math.NaN()
				}
				continue
			}
			switch {
			case v.Bytes != nil:
				out.Text[i] = string(v.Bytes[i])
			case v.Bool != nil:
				out.Text[i] = strconv.FormatBool(v.Bool[i])
			case v.Int64 != nil:
				if col.Logical == "INTEGER" && !col.Signed {
					out.Numbers[i] = float64(uint64(v.Int64[i]))
				} else {
					out.Numbers[i] = float64(v.Int64[i])
				}
			case v.Int32 != nil:
				if col.Logical == "INTEGER" && !col.Signed {
					out.Numbers[i] = float64(uint32(v.Int32[i]))
				} else {
					out.Numbers[i] = float64(v.Int32[i])
				}
			case v.Float64 != nil:
				out.Numbers[i] = v.Float64[i]
			case v.Float32 != nil:
				out.Numbers[i] = float64(v.Float32[i])
			}
		}
		t.Columns = append(t.Columns, out)
	}
	if t.SHA256, err = fileDigest(path); err != nil {
		return nil, err
	}
	return t, nil
}

// missingText are the values a text table writes for a missing one (pandas writes "" for NaN).
var missingText = map[string]bool{"": true, "NA": true, "N/A": true, "nan": true, "NaN": true, "None": true, "null": true, "NULL": true}

func readCSVTable(path string) (*Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(raw)
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		z, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if raw, err = io.ReadAll(io.LimitReader(z, 8<<30)); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // a byte-order mark
	first, _, _ := bufio.NewReader(bytes.NewReader(raw)).ReadLine()
	comma := ','
	if l := strings.ToLower(path); strings.Contains(l, ".tsv") || strings.Count(string(first), "\t") > strings.Count(string(first), ",") {
		comma = '\t'
	}
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s has no header row: %w", filepath.Base(path), err)
	}
	t := &Table{Format: FormatCSV, File: filepath.Base(path), SHA256: hex.EncodeToString(h[:])}
	text := make([][]string, len(header))
	names := map[string]bool{}
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" {
			name = fmt.Sprintf("column %d", i+1)
		}
		if names[name] {
			return nil, fmt.Errorf("%s names the column %q twice", t.File, name)
		}
		names[name] = true
		header[i] = name
	}
	cols := append([]string(nil), header...)
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.File, err)
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		if len(rec) != len(cols) {
			return nil, fmt.Errorf("%s, line %d: %d fields for %d columns", t.File, line, len(rec), len(cols))
		}
		if t.Rows++; t.Rows > MaxTableRows {
			return nil, fmt.Errorf("%s has more than %d rows; SituCraft reads at most that", t.File, MaxTableRows)
		}
		for i, v := range rec {
			text[i] = append(text[i], strings.TrimSpace(v))
		}
	}
	// A column of numbers wherever given (text that are all numbers or missing), else text.
	for i, name := range cols {
		c := &anndata.Column{Name: name}
		numbers := make([]float64, t.Rows)
		numeric, any := true, false
		for k, v := range text[i] {
			if missingText[v] {
				numbers[k] = math.NaN()
				continue
			}
			x, err := strconv.ParseFloat(v, 64)
			if err != nil {
				numeric = false
				break
			}
			numbers[k], any = x, true
		}
		for k, v := range text[i] {
			if missingText[v] {
				if c.Missing == nil {
					c.Missing = make([]bool, t.Rows)
				}
				c.Missing[k] = true
			}
		}
		if numeric && any {
			c.Numbers = numbers
		} else {
			c.Text = text[i]
		}
		t.Columns = append(t.Columns, c)
	}
	if t.Rows == 0 {
		return nil, errors.New(t.File + " has no rows")
	}
	if t.Column("cell_id") != nil && t.Column("group") != nil {
		t.Format = FormatExplorer
	}
	return t, nil
}
