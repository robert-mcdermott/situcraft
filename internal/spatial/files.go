package spatial

// A graph's files, an artifact of kind "graph" (internal/artifacts):
//
//	graph.json        what the graph is of and how it was made: the segmentation, the cells chosen
//	                  and how, the kind and its parameter, and what it holds (edges, degrees,
//	                  isolated cells, components, edge lengths, shared boundaries)
//	cells.bin "SCGC"  the cells in the graph: rows of the segmentation, and their centroids (µm)
//	edges.bin "SCGE"  each cell's neighbors (indices among the graph's cells) in compressed rows,
//	                  each edge's length (µm) and, in a contact graph, the boundary shared (µm)
//
// The binary files have SituCraft's envelope (internal/segment: magic, version, length, zlib).
// Every edge is listed from both of its cells.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
)

// The files, by role.
const (
	FileDescription = "graph.json"
	FileCells       = "cells.bin"
	FileEdges       = "edges.bin"
)

// Files lists them in order.
var Files = []string{FileDescription, FileCells, FileEdges}

// Roles name each file's role in an artifact's manifest.
var Roles = map[string]string{FileDescription: "description", FileCells: "cells", FileEdges: "edges"}

// Side names a segmentation.
type Side struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Filter names the QC filter that chose cells, with the checksum of those it keeps.
type Filter struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

// Region names a region the cells were chosen in, and its version.
type Region struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// Choice says which of the segmentation's cells were chosen, and how: its checksum is the SHA-256
// of the cells as lib/filters.js's keepChecksum writes them (the count, then a bit a cell).
type Choice struct {
	Checksum string   `json:"checksum"`
	Filter   *Filter  `json:"filter,omitempty"`
	Regions  []Region `json:"regions,omitempty"`
	// Excluded counts the cells a review excluded from analyses, left out.
	Excluded int `json:"excluded,omitempty"`
}

// Distribution summarizes values: their quantiles and a histogram of bins of Step from 0.
type Distribution struct {
	Min       float64 `json:"min"`
	Q05       float64 `json:"q05"`
	Q25       float64 `json:"q25"`
	Median    float64 `json:"median"`
	Q75       float64 `json:"q75"`
	Q95       float64 `json:"q95"`
	Max       float64 `json:"max"`
	Mean      float64 `json:"mean"`
	Step      float64 `json:"step"`
	Histogram []int   `json:"histogram"`
}

// Description is graph.json.
type Description struct {
	Format        string `json:"format"`
	Version       int    `json:"version"`
	Method        string `json:"method"`
	MethodVersion string `json:"methodVersion"`
	Segmentation  Side   `json:"segmentation"`
	Parameters    Params `json:"parameters"`
	Cells         struct {
		Segmentation int `json:"segmentation"`
		InGraph      int `json:"inGraph"`
		Choice
	} `json:"cells"`
	Edges  int `json:"edges"`
	Degree struct {
		Mean      float64 `json:"mean"`
		Median    float64 `json:"median"`
		Max       int     `json:"max"`
		Histogram []int   `json:"histogram"` // cells of each degree, 0 to Max
	} `json:"degree"`
	Isolated   int `json:"isolated"`
	Components struct {
		Count   int `json:"count"`
		Largest int `json:"largest"`
	} `json:"components"`
	Lengths    Distribution  `json:"lengths"`
	Shared     *Distribution `json:"shared,omitempty"`
	Duplicates int           `json:"duplicates,omitempty"`
	Collinear  bool          `json:"collinear,omitempty"`
}

// Summary is what the manifest says of a graph.
type Summary struct {
	Segmentation Side    `json:"segmentation"`
	Kind         string  `json:"kind"`
	Parameters   Params  `json:"parameters"`
	Cells        int     `json:"cells"`
	Edges        int     `json:"edges"`
	MeanDegree   float64 `json:"meanDegree"`
	Isolated     int     `json:"isolated"`
	Components   int     `json:"components"`
	MedianLength float64 `json:"medianLength"`
	Choice       Choice  `json:"choice"`
}

// Describe describes a graph built over cells of a segmentation of segmentationCells cells.
func Describe(g *Graph, seg Side, segmentationCells int, p Params, choice Choice) *Description {
	d := &Description{Format: "situcraft-graph", Version: 1, Method: "SituCraft cell graph", MethodVersion: MethodVersion, Segmentation: seg, Parameters: p}
	d.Cells.Segmentation, d.Cells.InGraph, d.Cells.Choice = segmentationCells, len(g.Rows), choice
	d.Edges = g.Edges()
	d.Duplicates, d.Collinear = g.Duplicates, g.Collinear
	n := len(g.Rows)
	degrees := make([]float64, n)
	for i := 0; i < n; i++ {
		k := int(g.Offsets[i+1] - g.Offsets[i])
		degrees[i] = float64(k)
		d.Degree.Max = max(d.Degree.Max, k)
		if k == 0 {
			d.Isolated++
		}
	}
	d.Degree.Histogram = make([]int, d.Degree.Max+1)
	for _, k := range degrees {
		d.Degree.Histogram[int(k)]++
	}
	if n > 0 {
		d.Degree.Mean = float64(len(g.Neighbors)) / float64(n)
		d.Degree.Median = quantile(sorted(degrees), 0.5)
	}
	d.Components.Count, d.Components.Largest = components(g)
	// Each edge once: from its lower cell.
	var lengths, shared []float64
	for i := 0; i < n; i++ {
		for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
			if g.Neighbors[k] > uint32(i) {
				lengths = append(lengths, float64(g.Length[k]))
				if g.Shared != nil {
					shared = append(shared, float64(g.Shared[k]))
				}
			}
		}
	}
	d.Lengths = distribution(lengths)
	if g.Shared != nil {
		s := distribution(shared)
		d.Shared = &s
	}
	return d
}

func sorted(v []float64) []float64 {
	out := append([]float64(nil), v...)
	sort.Float64s(out)
	return out
}

// quantile of sorted values, interpolated linearly between order statistics (Hyndman and Fan's
// type 7, as NumPy's default). Each product is rounded on its own, so that it is the same on every
// processor: fused with the subtraction below, h's product gives another fraction.
func quantile(s []float64, q float64) float64 {
	if len(s) == 0 {
		return 0
	}
	h := mul(q, float64(len(s)-1))
	lo := int(math.Floor(h))
	if lo+1 >= len(s) {
		return s[len(s)-1]
	}
	return s[lo] + mul(h-float64(lo), s[lo+1]-s[lo])
}

// distribution summarizes values, its histogram's step a round number giving at most 60 bins to
// the 99th percentile (larger values in the last bin).
func distribution(v []float64) Distribution {
	if len(v) == 0 {
		return Distribution{Histogram: []int{}}
	}
	s := sorted(v)
	d := Distribution{Min: s[0], Q05: quantile(s, 0.05), Q25: quantile(s, 0.25), Median: quantile(s, 0.5), Q75: quantile(s, 0.75), Q95: quantile(s, 0.95), Max: s[len(s)-1]}
	sum := 0.0
	for _, x := range s {
		sum += x
	}
	d.Mean = sum / float64(len(s))
	top := quantile(s, 0.99)
	d.Step = roundStep(top / 60)
	bins := int(math.Floor(top/d.Step)) + 1
	d.Histogram = make([]int, bins)
	for _, x := range s {
		d.Histogram[min(bins-1, int(math.Floor(x/d.Step)))]++
	}
	return d
}

// roundStep is the smallest of 1, 2 or 5 times a power of ten at least v (v > 0; else 1).
func roundStep(v float64) float64 {
	if !(v > 0) {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if m*p >= v {
			return m * p
		}
	}
	return 10 * p
}

// components counts the graph's connected components and the largest one's cells (union-find).
func components(g *Graph) (count, largest int) {
	n := len(g.Rows)
	parent := make([]int32, n)
	for i := range parent {
		parent[i] = int32(i)
	}
	var find func(int32) int32
	find = func(i int32) int32 {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := 0; i < n; i++ {
		for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
			a, b := find(int32(i)), find(int32(g.Neighbors[k]))
			if a != b {
				parent[max(a, b)] = min(a, b)
			}
		}
	}
	size := make([]int, n)
	for i := 0; i < n; i++ {
		r := find(int32(i))
		if size[r] == 0 {
			count++
		}
		size[r]++
		largest = max(largest, size[r])
	}
	return count, largest
}

// Write writes a graph's files into dir.
func Write(dir string, g *Graph, d *Description) error {
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	var b segment.Buffer
	b.U32(uint32(len(g.Rows)))
	b.U32s(g.Rows)
	b.F32s(g.X)
	b.F32s(g.Y)
	if err := segment.WriteBinary(filepath.Join(dir, FileCells), "SCGC", &b); err != nil {
		return err
	}
	b = segment.Buffer{}
	b.U32(uint32(len(g.Rows)))
	b.U32(uint32(len(g.Neighbors)))
	shared := byte(0)
	if g.Shared != nil {
		shared = 1
	}
	b.Byte(shared)
	b.U32s(g.Offsets)
	b.U32s(g.Neighbors)
	b.F32s(g.Length)
	if g.Shared != nil {
		b.F32s(g.Shared)
	}
	return segment.WriteBinary(filepath.Join(dir, FileEdges), "SCGE", &b)
}

// Digest is the SHA-256 of a graph's files, in order: the same cells and definition give the same
// digest on any computer and any number of workers.
func Digest(dir string) (string, error) {
	h := sha256.New()
	for _, f := range Files {
		raw, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(raw)
		h.Write([]byte(f + " " + hex.EncodeToString(sum[:]) + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReadDescription reads graph.json.
func ReadDescription(dir string) (*Description, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileDescription))
	if err != nil {
		return nil, err
	}
	var d Description
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", FileDescription, err)
	}
	if d.Format != "situcraft-graph" || d.Version != 1 {
		return nil, fmt.Errorf("%s is not a SituCraft graph of version 1", FileDescription)
	}
	return &d, nil
}

// Read reads a graph's cells and edges.
func Read(dir string) (*Graph, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileCells), "SCGC")
	if err != nil {
		return nil, err
	}
	n := r.Count(12)
	g := &Graph{}
	g.Rows, g.X, g.Y = r.U32s(n), r.F32s(n), r.F32s(n)
	if err := r.Done(FileCells); err != nil {
		return nil, err
	}
	r, err = segment.ReadBinary(filepath.Join(dir, FileEdges), "SCGE")
	if err != nil {
		return nil, err
	}
	cells := int(r.U32())
	m := r.Count(8)
	shared := r.Byte() == 1
	if cells != n {
		return nil, errors.New("edges.bin is of another number of cells than cells.bin")
	}
	g.Offsets = r.U32s(n + 1)
	g.Neighbors, g.Length = r.U32s(m), r.F32s(m)
	if shared {
		g.Shared = r.F32s(m)
	}
	return g, r.Done(FileEdges)
}

// Check checks a graph's files before it is recorded, and fills the manifest's summary: the cells
// in the segmentation's order, every edge listed from both cells with the same length (the
// distance between their centroids) and shared boundary, no cell its own neighbor, the
// description's counts the files' own.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	d, err := ReadDescription(dir)
	if err != nil {
		fatal("graph.unreadable", "%v", err)
		return out
	}
	g, err := Read(dir)
	if err != nil {
		fatal("graph.unreadable", "%v", err)
		return out
	}
	n := len(g.Rows)
	if n != d.Cells.InGraph || len(g.Neighbors) != 2*d.Edges || (g.Shared != nil) != (d.Parameters.Kind == Contact) {
		fatal("graph.sizes", "The files hold %d cells and %d edge ends; the description says %d cells and %d edges.", n, len(g.Neighbors), d.Cells.InGraph, d.Edges)
		return out
	}
	for i := 0; i < n; i++ {
		if int(g.Rows[i]) >= d.Cells.Segmentation || i > 0 && g.Rows[i] <= g.Rows[i-1] {
			fatal("graph.cells", "The graph's cells are not the segmentation's cells in order (cell %d).", i)
			return out
		}
	}
	if g.Offsets[0] != 0 || int(g.Offsets[n]) != len(g.Neighbors) {
		fatal("graph.edges", "The edges' rows do not cover the edges.")
		return out
	}
	find := func(i, j uint32) int {
		row := g.Neighbors[g.Offsets[i]:g.Offsets[i+1]]
		k := sort.Search(len(row), func(k int) bool { return row[k] >= j })
		if k < len(row) && row[k] == j {
			return int(g.Offsets[i]) + k
		}
		return -1
	}
	for i := 0; i < n; i++ {
		if g.Offsets[i+1] < g.Offsets[i] {
			fatal("graph.edges", "Cell %d's edges end before they begin.", i)
			return out
		}
		for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
			j := g.Neighbors[k]
			if int(j) >= n || j == uint32(i) || k > g.Offsets[i] && j <= g.Neighbors[k-1] {
				fatal("graph.edges", "Cell %d's neighbors are not other cells of the graph in order.", i)
				return out
			}
			back := find(j, uint32(i))
			if back < 0 || g.Length[back] != g.Length[k] || g.Shared != nil && g.Shared[back] != g.Shared[k] {
				fatal("graph.symmetric", "The edge between cells %d and %d is not listed alike from both.", i, j)
				return out
			}
			if g.Length[k] != float32(distance(g.X[i], g.Y[i], g.X[j], g.Y[j])) {
				fatal("graph.lengths", "The edge between cells %d and %d is not as long as their centroids are apart.", i, j)
				return out
			}
		}
	}
	p := d.Parameters
	// Lengths are kept as float32: within its rounding of the limit.
	if p.Kind == Radius && d.Lengths.Max > p.Radius*(1+1e-6) || p.Kind == Delaunay && p.Longest > 0 && d.Lengths.Max > p.Longest*(1+1e-6) {
		fatal("graph.lengths", "An edge is %g µm long, longer than the graph allows.", d.Lengths.Max)
	}
	s := Summary{Segmentation: d.Segmentation, Kind: p.Kind, Parameters: p, Cells: n, Edges: d.Edges, MeanDegree: d.Degree.Mean, Isolated: d.Isolated, Components: d.Components.Count, MedianLength: d.Lengths.Median, Choice: d.Cells.Choice}
	m.Summary, _ = json.Marshal(s)
	return out
}
