package cluster

// A clustering's files, an artifact of kind "clustering" (internal/artifacts):
//
//	clustering.json   what was clustered and how: the segmentation, the cells, every parameter and
//	                  fixed choice, the components' variances, the graph, the clusters and their
//	                  leading markers
//	cells.bin "SCKC"  the cells analyzed (indices in the segmentation's cells) and each one's cluster
//	pca.bin "SCPC"    each gene's mean and standard deviation, the loadings (genes × components) and
//	                  each cell's scores (cells × components)
//	neighbors.bin "SCKN"  each cell's nearest neighbors in the components (indices among the cells
//	                  analyzed) and their distances
//	markers.bin "SCMR"  each cluster's statistics for every gene: score, log₂ fold change, p-value,
//	                  adjusted p-value, shares expressing in and out
//
// The binary files have SituCraft's envelope (internal/segment: magic, version, length, zlib).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"situcraft/internal/artifacts"
	"situcraft/internal/graph"
	"situcraft/internal/segment"
)

// Files of a clustering, by role.
const (
	FileDescription = "clustering.json"
	FileCells       = "cells.bin"
	FilePCA         = "pca.bin"
	FileNeighbors   = "neighbors.bin"
	FileMarkers     = "markers.bin"
)

// Roles name each file's role in an artifact's manifest.
var Roles = map[string]string{FileDescription: "description", FileCells: "clusters", FilePCA: "components", FileNeighbors: "neighbors", FileMarkers: "markers"}

// Files lists them in order.
var Files = []string{FileDescription, FileCells, FilePCA, FileNeighbors, FileMarkers}

// FilesOf lists a clustering's files: a clustering from another tool has no neighbors of
// SituCraft's, and its pca.bin holds the genes and their scaling without components.
func FilesOf(d *Description) []string {
	if d.Source != nil {
		return []string{FileDescription, FileCells, FilePCA, FileMarkers}
	}
	return Files
}

// Side names the segmentation clustered.
type Side struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Filter names the QC filter that chose the cells, with the checksum of which it keeps.
type Filter struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

// Source says where a clustering came from when SituCraft did not make it (the instrument's
// analysis): the tool and its version, the file, and the clustering's name and title there.
type Source struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	File    string `json:"file"`
	Name    string `json:"name"`
	Title   string `json:"title"`
}

// Marker is a gene's statistics in a cluster.
type Marker struct {
	Gene   string  `json:"gene"`
	Score  float64 `json:"score"`
	LogFC  float64 `json:"logfc"`
	PAdj   float64 `json:"padj"`
	PctIn  float64 `json:"pctIn"`
	PctOut float64 `json:"pctOut"`
}

// Description is clustering.json.
type Description struct {
	Format       string  `json:"format"`
	Version      int     `json:"version"`
	Method       string  `json:"method"`
	Segmentation Side    `json:"segmentation"`
	Source       *Source `json:"source,omitempty"`
	Parameters   Params  `json:"parameters"`
	Fixed        struct {
		Normalization string  `json:"normalization"`
		TargetSum     float64 `json:"targetSum"`
		Clip          float64 `json:"clip"`
		Metric        string  `json:"metric"`
		Graph         string  `json:"graph"`
		Quality       string  `json:"quality"`
		Randomness    float64 `json:"randomness"`
		Markers       string  `json:"markers"`
	} `json:"fixed"`
	Cells struct {
		Segmentation int     `json:"segmentation"`
		Analyzed     int     `json:"analyzed"`
		Excluded     int     `json:"excluded"`
		NoTranscript int     `json:"withoutTranscripts"`
		Filter       *Filter `json:"filter,omitempty"`
	} `json:"cells"`
	Genes struct {
		Count       int `json:"count"`
		NotDetected int `json:"notDetected"`
		MedianCells int `json:"medianCellsExpressing"`
	} `json:"genes"`
	PCA struct {
		Components    int       `json:"components"`
		Variance      []float64 `json:"variance"`
		VarianceRatio []float64 `json:"varianceRatio"`
		TotalVariance float64   `json:"totalVariance"`
		// Solver says how the components were found when not by the full decomposition (panels of
		// more than DenseGenes genes).
		Solver string `json:"solver,omitempty"`
	} `json:"pca"`
	Graph struct {
		Neighbors int `json:"neighbors"`
		Edges     int `json:"edges"`
		// Search says how the neighbors were found when not exactly (more than graph.ExactLimit
		// cells): NN-Descent's parameters and its recall against exact search.
		Search *graph.Approximate `json:"search,omitempty"`
	} `json:"graph"`
	Clusters struct {
		Count      int     `json:"count"`
		Sizes      []int   `json:"sizes"`
		Modularity float64 `json:"modularity"`
		Iterations int     `json:"iterations"`
	} `json:"clusters"`
	// Top are each cluster's leading markers by score (the first ten).
	Top     [][]Marker         `json:"top"`
	Seconds map[string]float64 `json:"seconds"`
}

// TopMarkers is a cluster's genes by decreasing score (ties by gene), the first n.
func TopMarkers(mk *Markers, genes []string, c, n int) []Marker {
	G := mk.Genes
	order := make([]int, G)
	for g := range order {
		order[g] = g
	}
	sort.SliceStable(order, func(a, b int) bool { return mk.Score[c*G+order[a]] > mk.Score[c*G+order[b]] })
	var out []Marker
	for _, g := range order[:min(n, G)] {
		at := c*G + g
		out = append(out, Marker{Gene: genes[g], Score: mk.Score[at], LogFC: mk.LogFC[at], PAdj: mk.PAdj[at], PctIn: mk.PctIn[at], PctOut: mk.PctOut[at]})
	}
	return out
}

// Write writes a clustering's files into dir.
func Write(dir string, seg Side, filter *Filter, r *Result) error {
	d := Description{Format: "situcraft-clustering", Version: 1, Method: "SituCraft clustering " + MethodVersion, Segmentation: seg, Source: r.Source, Parameters: r.Params, Seconds: r.Seconds}
	if r.Source != nil {
		d.Method = r.Source.Tool + " " + r.Source.Title + " clustering"
	}
	d.Fixed.Normalization = "each cell's counts scaled to the median total, then log1p"
	d.Fixed.TargetSum = r.Target
	d.Fixed.Clip = ClipAt
	d.Fixed.Metric = "euclidean"
	d.Fixed.Graph = "UMAP's fuzzy simplicial set"
	d.Fixed.Quality = "modularity"
	d.Fixed.Randomness = Randomness
	d.Fixed.Markers = "Wilcoxon rank-sum, ties corrected; Benjamini-Hochberg"
	d.Cells.Segmentation, d.Cells.Analyzed, d.Cells.Excluded, d.Cells.NoTranscript, d.Cells.Filter = r.SegCells, len(r.Cells), r.Excluded, r.NoTranscript, filter
	d.Genes.Count = len(r.Genes)
	expressing := append([]int(nil), r.Detected...)
	for _, n := range expressing {
		if n == 0 {
			d.Genes.NotDetected++
		}
	}
	sort.Ints(expressing)
	if len(expressing) > 0 {
		d.Genes.MedianCells = expressing[len(expressing)/2]
	}
	p := r.PCA
	if p != nil {
		d.PCA.Components, d.PCA.Variance, d.PCA.TotalVariance, d.PCA.Solver = p.components, p.values, p.totalVariance, p.solver
		for _, v := range p.values {
			d.PCA.VarianceRatio = append(d.PCA.VarianceRatio, v/p.totalVariance)
		}
		d.Graph.Neighbors, d.Graph.Edges, d.Graph.Search = r.Params.Neighbors, r.Edges, r.Search
	}
	d.Clusters.Count, d.Clusters.Sizes, d.Clusters.Modularity, d.Clusters.Iterations = len(r.Partition.Sizes), r.Partition.Sizes, r.Partition.Modularity, r.Partition.Iterations
	for c := 0; c < r.Markers.Clusters; c++ {
		d.Top = append(d.Top, TopMarkers(r.Markers, r.Genes, c, 10))
	}
	raw, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	n := len(r.Cells)
	var b segment.Buffer
	b.U32(uint32(n))
	b.U32s(r.Cells)
	for _, c := range r.Partition.Community {
		b.U32(uint32(c))
	}
	if err := segment.WriteBinary(filepath.Join(dir, FileCells), "SCKC", &b); err != nil {
		return err
	}
	components, loadings, scores := 0, []float64(nil), []float32(nil)
	if p != nil {
		components, loadings = p.components, p.loadings
		scores = make([]float32, len(p.scores))
		for i, v := range p.scores {
			scores[i] = float32(v)
		}
	}
	b = segment.Buffer{}
	b.U32(uint32(len(r.Genes)))
	for _, g := range r.Genes {
		b.Str(g)
	}
	b.U32(uint32(components))
	b.F64s(r.Mean)
	b.F64s(r.SD)
	b.F64s(loadings)
	b.U32(uint32(n))
	b.F32s(scores)
	if err := segment.WriteBinary(filepath.Join(dir, FilePCA), "SCPC", &b); err != nil {
		return err
	}
	if p == nil {
		return writeMarkers(dir, r.Markers)
	}
	b = segment.Buffer{}
	nb := r.Neighbors
	b.U32(uint32(nb.N))
	b.U32(uint32(nb.K))
	for _, j := range nb.Index {
		b.U32(uint32(j))
	}
	b.F64s(nb.Distances)
	if err := segment.WriteBinary(filepath.Join(dir, FileNeighbors), "SCKN", &b); err != nil {
		return err
	}
	return writeMarkers(dir, r.Markers)
}

func writeMarkers(dir string, mk *Markers) error {
	var b segment.Buffer
	b.U32(uint32(mk.Clusters))
	b.U32(uint32(mk.Genes))
	for _, col := range [][]float64{mk.Score, mk.LogFC, mk.P, mk.PAdj, mk.PctIn, mk.PctOut} {
		b.F64s(col)
	}
	return segment.WriteBinary(filepath.Join(dir, FileMarkers), "SCMR", &b)
}

// ReadDescription reads clustering.json.
func ReadDescription(dir string) (*Description, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileDescription))
	if err != nil {
		return nil, err
	}
	var d Description
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", FileDescription, err)
	}
	if d.Format != "situcraft-clustering" || d.Version > 1 {
		return nil, fmt.Errorf("%s is not a clustering SituCraft reads (%s, version %d)", FileDescription, d.Format, d.Version)
	}
	return &d, nil
}

// ReadCells reads cells.bin: the cells analyzed and each one's cluster.
func ReadCells(dir string) (cells, cluster []uint32, err error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileCells), "SCKC")
	if err != nil {
		return nil, nil, err
	}
	n := r.Count(8)
	cells, cluster = r.U32s(n), r.U32s(n)
	return cells, cluster, r.Done(FileCells)
}

// PCA is pca.bin.
type PCA struct {
	Genes      []string
	Components int
	Mean, SD   []float64
	Loadings   []float64 // genes × components
	Scores     []float32 // cells × components
}

// ReadPCA reads pca.bin.
func ReadPCA(dir string) (*PCA, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FilePCA), "SCPC")
	if err != nil {
		return nil, err
	}
	p := &PCA{}
	G := r.Count(1)
	for g := 0; g < G; g++ {
		p.Genes = append(p.Genes, r.Str())
	}
	p.Components = r.Count(8 * G)
	p.Mean, p.SD = r.F64s(G), r.F64s(G)
	if p.Components > 0 && G > 0 {
		p.Loadings = r.F64s(G * p.Components)
	}
	if p.Components == 0 {
		// Another tool's clustering: its genes alone, with the count of its cells.
		r.U32()
		return p, r.Done(FilePCA)
	}
	n := r.Count(4 * p.Components)
	p.Scores = r.F32s(n * p.Components)
	return p, r.Done(FilePCA)
}

// ReadNeighbors reads neighbors.bin.
func ReadNeighbors(dir string) (*graph.Neighbors, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileNeighbors), "SCKN")
	if err != nil {
		return nil, err
	}
	nb := &graph.Neighbors{}
	nb.N = r.Count(12)
	nb.K = r.Count(12 * max(nb.N, 1))
	for _, j := range r.U32s(nb.N * nb.K) {
		nb.Index = append(nb.Index, int32(j))
	}
	nb.Distances = r.F64s(nb.N * nb.K)
	return nb, r.Done(FileNeighbors)
}

// ReadMarkers reads markers.bin.
func ReadMarkers(dir string) (*Markers, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileMarkers), "SCMR")
	if err != nil {
		return nil, err
	}
	mk := &Markers{}
	mk.Clusters = r.Count(48)
	mk.Genes = r.Count(48 * max(mk.Clusters, 1))
	size := mk.Clusters * mk.Genes
	for _, col := range []*[]float64{&mk.Score, &mk.LogFC, &mk.P, &mk.PAdj, &mk.PctIn, &mk.PctOut} {
		*col = r.F64s(size)
	}
	return mk, r.Done(FileMarkers)
}

// Digest is the SHA-256 of a clustering's content: its binary files, and its description without
// the run's timings. The same inputs and parameters give the same digest on any computer.
func Digest(dir string) (string, error) {
	d, err := ReadDescription(dir)
	if err != nil {
		return "", err
	}
	d.Seconds = nil
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(raw)
	for _, f := range FilesOf(d)[1:] {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", err
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Summary is what the check reports of a clustering (the artifact's summary).
type Summary struct {
	Segmentation  Side    `json:"segmentation"`
	Cells         int     `json:"cells"`
	Clusters      int     `json:"clusters"`
	Sizes         []int   `json:"sizes"`
	Modularity    float64 `json:"modularity"`
	Components    int     `json:"components"`
	VarianceShare float64 `json:"varianceShare"`
	Resolution    float64 `json:"resolution"`
	Filter        *Filter `json:"filter,omitempty"`
	Source        *Source `json:"source,omitempty"`
}

// Check examines a staged clustering: its files read and agree in size; the cells analyzed are
// the segmentation's, in order; every one is in a cluster, numbered by decreasing size as the
// description counts them; the graph made again from the neighbors gives the recorded modularity;
// every number is finite and every p-value a probability.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	d, err := ReadDescription(dir)
	if err != nil {
		fatal("clustering.unreadable", "%v", err)
		return out
	}
	cells, cluster, err := ReadCells(dir)
	if err != nil {
		fatal("clustering.unreadable", "%v", err)
		return out
	}
	mk, err := ReadMarkers(dir)
	if err != nil {
		fatal("clustering.unreadable", "%v", err)
		return out
	}
	n := len(cells)
	own := d.Source == nil
	p, err := ReadPCA(dir)
	if err != nil {
		fatal("clustering.unreadable", "%v", err)
		return out
	}
	if !own && (p.Components != 0 || len(p.Genes) != d.Genes.Count) {
		fatal("clustering.sizes", "Another tool's clustering's pca.bin holds %d genes and %d components; it should hold the %d genes alone.", len(p.Genes), p.Components, d.Genes.Count)
		return out
	}
	var nb *graph.Neighbors
	if own {
		if nb, err = ReadNeighbors(dir); err != nil {
			fatal("clustering.unreadable", "%v", err)
			return out
		}
		if len(p.Scores) != n*p.Components || nb.N != n || nb.K != d.Parameters.Neighbors-1 || p.Components != d.PCA.Components || len(p.Genes) != d.Genes.Count {
			fatal("clustering.sizes", "The files disagree in size: %d cells analyzed, %d components (the description %d), %d × %d neighbors.", n, p.Components, d.PCA.Components, nb.N, nb.K)
			return out
		}
	}
	if n != d.Cells.Analyzed || mk.Genes != d.Genes.Count || mk.Clusters != d.Clusters.Count || len(cluster) != n {
		fatal("clustering.sizes", "The files disagree in size: %d cells analyzed (the description %d), %d clusters of markers (the description %d).", n, d.Cells.Analyzed, mk.Clusters, d.Clusters.Count)
		return out
	}
	for i, c := range cells {
		if int(c) >= d.Cells.Segmentation || i > 0 && c <= cells[i-1] {
			fatal("clustering.cells", "The cells analyzed are not the segmentation's cells in order (cell %d).", i)
			break
		}
	}
	sizes := make([]int, d.Clusters.Count)
	for _, c := range cluster {
		if int(c) >= len(sizes) {
			fatal("clustering.clusters", "A cell is in cluster %d of %d.", c, len(sizes))
			return out
		}
		sizes[c]++
	}
	for c, s := range sizes {
		// SituCraft numbers its clusters by size; another tool's keep that tool's numbers.
		if s != d.Clusters.Sizes[c] || own && (c > 0 && s > sizes[c-1] || s == 0) {
			fatal("clustering.sizes", "The clusters' sizes %v are not the description's %v.", sizes, d.Clusters.Sizes)
			break
		}
	}
	finite := func(name string, v []float64) {
		for _, x := range v {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				fatal("clustering.values", "The %s hold a value that is not a finite number.", name)
				return
			}
		}
	}
	if own {
		g, err := graph.Fuzzy(nb)
		if err != nil {
			fatal("clustering.graph", "%v", err)
		} else {
			community := make([]int32, n)
			for i, c := range cluster {
				community[i] = int32(c)
			}
			if q := graph.Modularity(g, community, d.Parameters.Resolution); q != d.Clusters.Modularity || g.Edges() != d.Graph.Edges {
				fatal("clustering.modularity", "The graph made again from the neighbors has %d edges and modularity %v; the description records %d and %v.", g.Edges(), q, d.Graph.Edges, d.Clusters.Modularity)
			}
		}
		finite("loadings", p.Loadings)
		finite("variances", d.PCA.Variance)
		finite("distances", nb.Distances)
		for _, s := range p.Scores {
			if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
				fatal("clustering.values", "A cell's component score is not a finite number.")
				break
			}
		}
	}
	finite("scores", mk.Score)
	for i := range mk.P {
		// p·m/m may round a unit below p.
		if !(mk.P[i] >= 0 && mk.P[i] <= 1) || !(mk.PAdj[i] >= mk.P[i]*(1-1e-12) && mk.PAdj[i] <= 1) {
			fatal("clustering.markers", "A marker's p-value or adjusted p-value is not a probability at least the p-value.")
			break
		}
	}
	share := 0.0
	for _, r := range d.PCA.VarianceRatio {
		share += r
	}
	s := Summary{Segmentation: d.Segmentation, Cells: n, Clusters: d.Clusters.Count, Sizes: d.Clusters.Sizes, Modularity: d.Clusters.Modularity,
		Components: d.PCA.Components, VarianceShare: share, Resolution: d.Parameters.Resolution, Filter: d.Cells.Filter, Source: d.Source}
	m.Summary, _ = json.Marshal(s)
	return out
}
