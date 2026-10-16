// Package cluster clusters a segmentation's cells by their gene expression, the steps of the
// common single-cell workflow (Wolf FA, Angerer P, Theis FJ. SCANPY: large-scale single-cell gene
// expression data analysis. Genome Biol 2018;19:15) with every parameter recorded and the same
// result on every processor for a seed:
//
//  1. the cells analyzed: those with at least a number of gene transcripts of quality 20 or more,
//     or those a QC filter keeps;
//  2. each cell's counts scaled to the median total and log-transformed (log1p);
//  3. each gene centered and scaled to unit variance, clipped at ±10;
//  4. principal components, exact (matrix.go);
//  5. each cell's nearest neighbors in the components, exact, as UMAP's fuzzy graph
//     (internal/graph);
//  6. Leiden communities of the graph at a resolution (internal/graph), numbered by size;
//  7. each cluster's markers against the rest (markers.go), exploratory.
//
// A clustering is an artifact of kind "clustering" (files.go).
package cluster

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"situcraft/internal/graph"
	"situcraft/internal/segment"
)

// MethodVersion is the version of the method, recorded with every clustering.
const MethodVersion = "1"

// Fixed choices, recorded with every clustering.
const (
	// ClipAt bounds the scaled values (scanpy's scale with max_value 10).
	ClipAt = 10
	// Randomness is the Leiden refinement's θ (leidenalg's default).
	Randomness = 0.01
)

// Params are the method's parameters.
type Params struct {
	// MinTranscripts: the cells analyzed have at least this many gene transcripts of quality 20 or
	// more (when no filter gives them).
	MinTranscripts int `json:"minTranscripts"`
	// Components: the principal components the neighbors are found in.
	Components int `json:"components"`
	// Neighbors: each cell's neighborhood, itself included (scanpy's and umap-learn's count).
	Neighbors int `json:"neighbors"`
	// Resolution of the modularity Leiden optimizes: higher, more and smaller clusters.
	Resolution float64 `json:"resolution"`
	Seed       uint64  `json:"seed"`
}

// DefaultParams are scanpy's defaults, with SituCraft's seed.
func DefaultParams() Params {
	return Params{MinTranscripts: 10, Components: 50, Neighbors: 15, Resolution: 1, Seed: 20250}
}

// Check refuses parameters out of range, saying which.
func (p Params) Check() error {
	switch {
	case p.MinTranscripts < 0 || p.MinTranscripts > 100000:
		return errors.New("the minimum of gene transcripts is between 0 and 100,000")
	case p.Components < 2 || p.Components > 200:
		return errors.New("the principal components are between 2 and 200")
	case p.Neighbors < 2 || p.Neighbors > 200:
		return errors.New("the neighbors are between 2 and 200")
	case !(p.Resolution > 0) || p.Resolution > 50 || math.IsInf(p.Resolution, 0):
		return errors.New("the resolution is above 0 and at most 50")
	}
	return nil
}

// Inputs are what a clustering is made from: a segmentation's counts, and the cells to analyze
// (nil: those with at least Params.MinTranscripts gene transcripts).
type Inputs struct {
	Counts *segment.Counts
	Keep   []bool
}

// Result is a clustering.
type Result struct {
	Params Params
	// Cells analyzed (indices in the segmentation), and the segmentation's cells.
	Cells        []uint32
	SegCells     int
	Excluded     int // left out by the rule or the filter
	NoTranscript int // kept but without a gene transcript, so not analyzed
	Genes        []string
	Detected     []int // cells expressing each gene
	Target       float64
	Mean, SD     []float64 // each gene's, of the log-normalized values
	PCA          *pcaResult
	Neighbors    *graph.Neighbors // Params.Neighbors − 1 others
	Edges        int
	Partition    *graph.Partition
	Markers      *Markers
	Seconds      map[string]float64
}

// Run clusters. progress, if not nil, is told the share done and what is under way.
func Run(ctx context.Context, in Inputs, p Params, workers int, progress func(float64, string)) (*Result, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if in.Counts == nil {
		return nil, errors.New("no counts")
	}
	say := func(f float64, msg string) {
		if progress != nil {
			progress(f, msg)
		}
	}
	segCells := len(in.Counts.Offsets) - 1
	if in.Keep != nil && len(in.Keep) != segCells {
		return nil, fmt.Errorf("the cells to keep name %d cells; the segmentation has %d", len(in.Keep), segCells)
	}
	r := &Result{Params: p, SegCells: segCells, Genes: in.Counts.Genes, Seconds: map[string]float64{}}
	clock := time.Now()
	lap := func(name string) {
		r.Seconds[name] = math.Round(time.Since(clock).Seconds()*1000) / 1000
		clock = time.Now()
	}
	say(0, "Selecting and normalizing the cells")
	keep := func(i int, total uint32) bool {
		if in.Keep != nil {
			return in.Keep[i]
		}
		return int(total) >= p.MinTranscripts
	}
	m, empty, err := selectCells(in.Counts, keep)
	if err != nil {
		return nil, err
	}
	r.Cells, r.NoTranscript = m.cells, empty
	r.Excluded = segCells - len(m.cells) - empty
	if len(m.cells) < p.Neighbors+1 || len(m.cells) < 3 {
		return nil, fmt.Errorf("%d cells are analyzed: too few for %d neighbors", len(m.cells), p.Neighbors)
	}
	r.Target = median(m.total)
	m.normalize(r.Target)
	r.Mean, r.SD, r.Detected = m.scaling()
	lap("normalize")
	say(0.05, "Principal components")
	pc, err := m.pca(ctx, r.Mean, r.SD, ClipAt, p.Components, workers, func(f float64) { say(0.05+0.3*f, "Principal components") })
	if err != nil {
		return nil, err
	}
	r.PCA = pc
	lap("pca")
	say(0.35, "Finding each cell's nearest neighbors")
	nb, err := graph.KNN(ctx, pc.scores, len(m.cells), pc.components, p.Neighbors-1, workers, func(f float64) { say(0.35+0.45*f, "Finding each cell's nearest neighbors") })
	if err != nil {
		return nil, err
	}
	r.Neighbors = nb
	g, err := graph.Fuzzy(nb)
	if err != nil {
		return nil, err
	}
	r.Edges = g.Edges()
	lap("neighbors")
	say(0.82, "Leiden clustering")
	part, err := graph.Leiden(ctx, g, graph.LeidenOptions{Resolution: p.Resolution, Randomness: Randomness, Seed: p.Seed})
	if err != nil {
		return nil, err
	}
	r.Partition = part
	lap("leiden")
	say(0.9, "Each cluster's markers")
	r.Markers = m.markers(part.Community, len(part.Sizes), workers)
	lap("markers")
	say(1, fmt.Sprintf("%d clusters of %d cells", len(part.Sizes), len(m.cells)))
	return r, nil
}
