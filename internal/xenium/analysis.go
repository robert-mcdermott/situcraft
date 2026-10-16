package xenium

// The instrument's own secondary analysis of a bundle: Xenium Onboard Analysis clusters the cells
// (graph-based, and k-means for 2 to 10 clusters), projects them on principal components and lays
// them out by UMAP, and writes each as CSV in analysis.tar.gz (or an analysis/ folder) under
// analysis/{clustering,pca,umap}/gene_expression_*/: clusters.csv (Barcode, Cluster) and
// projection.csv (Barcode, then one column per dimension).

import (
	"archive/tar"
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// AnalysisClustering is one of the instrument's clusterings: each listed cell's cluster, numbered
// from 1 as the instrument numbers them.
type AnalysisClustering struct {
	Name     string // gene_expression_graphclust, gene_expression_kmeans_5_clusters
	Title    string // Graph-based, K-means (5 clusters)
	Barcodes []string
	Cluster  []int32
}

// Projection is a table of each listed cell's place in some dimensions.
type Projection struct {
	Name     string
	Barcodes []string
	Dims     int
	Values   []float64 // cells × dims
}

// Analysis is the instrument's analysis, as the bundle holds it.
type Analysis struct {
	Source      string // the file or folder read
	Clusterings []AnalysisClustering
	PCA, UMAP   *Projection
}

// analysisMember is a CSV of the analysis, by its path below analysis/.
type analysisMember struct {
	kind, name, file string
}

func parseAnalysisPath(p string) (analysisMember, bool) {
	parts := strings.Split(strings.TrimPrefix(p, "./"), "/")
	if len(parts) != 4 || parts[0] != "analysis" {
		return analysisMember{}, false
	}
	kind, name, file := parts[1], parts[2], parts[3]
	switch {
	case kind == "clustering" && file == "clusters.csv", (kind == "pca" || kind == "umap") && file == "projection.csv":
		return analysisMember{kind, name, file}, true
	}
	return analysisMember{}, false
}

// clusteringTitle names an instrument clustering for people.
func clusteringTitle(name string) string {
	if strings.HasSuffix(name, "graphclust") {
		return "Graph-based"
	}
	var k int
	if _, err := fmt.Sscanf(strings.TrimPrefix(name, "gene_expression_"), "kmeans_%d_clusters", &k); err == nil {
		return fmt.Sprintf("K-means (%d clusters)", k)
	}
	return name
}

// HasAnalysis reports whether the bundle holds the instrument's analysis.
func (b *Bundle) HasAnalysis() bool {
	if b.Has("analysis.tar.gz") {
		return true
	}
	for p := range b.byPath {
		if _, ok := parseAnalysisPath(p); ok {
			return true
		}
	}
	return false
}

// ReadAnalysis reads the instrument's analysis: its clusterings (graph-based first, then k-means
// by their number of clusters), principal components and UMAP. Cells are named by their ids
// (cells.parquet's), as the files list them.
func (b *Bundle) ReadAnalysis() (*Analysis, error) {
	a := &Analysis{}
	read := func(m analysisMember, r io.Reader, where string) error {
		rows, err := csv.NewReader(r).ReadAll()
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if len(rows) < 1 {
			return fmt.Errorf("%s is empty", where)
		}
		head, body := rows[0], rows[1:]
		if m.kind == "clustering" {
			if len(head) < 2 || head[0] != "Barcode" || head[1] != "Cluster" {
				return fmt.Errorf("%s: its columns are %v, not Barcode and Cluster", where, head)
			}
			c := AnalysisClustering{Name: m.name, Title: clusteringTitle(m.name)}
			for i, row := range body {
				if len(row) < 2 {
					return fmt.Errorf("%s: row %d has %d fields", where, i+2, len(row))
				}
				k, err := strconv.ParseInt(row[1], 10, 32)
				if err != nil || k < 1 {
					return fmt.Errorf("%s: row %d's cluster is %q", where, i+2, row[1])
				}
				c.Barcodes = append(c.Barcodes, row[0])
				c.Cluster = append(c.Cluster, int32(k))
			}
			a.Clusterings = append(a.Clusterings, c)
			return nil
		}
		if len(head) < 2 || head[0] != "Barcode" {
			return fmt.Errorf("%s: its first column is not Barcode", where)
		}
		p := &Projection{Name: m.name, Dims: len(head) - 1}
		for i, row := range body {
			if len(row) != len(head) {
				return fmt.Errorf("%s: row %d has %d fields, not %d", where, i+2, len(row), len(head))
			}
			p.Barcodes = append(p.Barcodes, row[0])
			for _, f := range row[1:] {
				v, err := strconv.ParseFloat(f, 64)
				if err != nil {
					return fmt.Errorf("%s: row %d: %q is not a number", where, i+2, f)
				}
				p.Values = append(p.Values, v)
			}
		}
		if m.kind == "pca" {
			a.PCA = p
		} else {
			a.UMAP = p
		}
		return nil
	}
	if b.Has("analysis.tar.gz") {
		a.Source = "analysis.tar.gz"
		f, err := b.OpenFile("analysis.tar.gz")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(io.NewSectionReader(f, 0, f.Size()))
		if err != nil {
			return nil, fmt.Errorf("analysis.tar.gz: %w", err)
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("analysis.tar.gz: %w", err)
			}
			if h.Typeflag != tar.TypeReg {
				continue
			}
			m, ok := parseAnalysisPath(h.Name)
			if !ok {
				continue
			}
			if h.Size > 4<<30 {
				return nil, fmt.Errorf("analysis.tar.gz: %s is %d bytes", h.Name, h.Size)
			}
			if err := read(m, io.LimitReader(tr, h.Size), "analysis.tar.gz/"+path.Clean(h.Name)); err != nil {
				return nil, err
			}
		}
	} else {
		a.Source = "analysis/"
		var names []string
		for p := range b.byPath {
			if _, ok := parseAnalysisPath(p); ok {
				names = append(names, p)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("the instrument's analysis: %w", fs.ErrNotExist)
		}
		sort.Strings(names)
		for _, p := range names {
			m, _ := parseAnalysisPath(p)
			f, err := b.OpenFile(p)
			if err != nil {
				return nil, err
			}
			err = read(m, io.NewSectionReader(f, 0, f.Size()), p)
			f.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	if len(a.Clusterings) == 0 {
		return nil, errors.New("the instrument's analysis holds no clustering")
	}
	sort.SliceStable(a.Clusterings, func(i, j int) bool {
		gi, gj := strings.HasSuffix(a.Clusterings[i].Name, "graphclust"), strings.HasSuffix(a.Clusterings[j].Name, "graphclust")
		if gi != gj {
			return gi
		}
		ki, kj := kmeansK(a.Clusterings[i].Name), kmeansK(a.Clusterings[j].Name)
		if ki != kj {
			return ki < kj
		}
		return a.Clusterings[i].Name < a.Clusterings[j].Name
	})
	return a, nil
}

func kmeansK(name string) int {
	var k int
	fmt.Sscanf(strings.TrimPrefix(name, "gene_expression_"), "kmeans_%d_clusters", &k)
	return k
}
