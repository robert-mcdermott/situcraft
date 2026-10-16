package cluster

import (
	"errors"
	"fmt"
	"runtime"
	"sort"

	"situcraft/internal/segment"
)

// Expression is a clustering's cells' log-normalized expression, by gene, as the clustering
// normalized it (each cell's counts scaled to the clustering's target total, then log1p): what
// colors the map and the tissue by a gene, the dot plot's means and shares, and the markers of a
// selection of cells.
type Expression struct {
	m       *matrix
	Genes   []string
	Cells   []uint32 // in the segmentation
	Cluster []uint32
	byGene  [][]int // each gene's entries in m (cell order)
}

// LoadExpression makes it from a segmentation's counts and a clustering's cells, clusters and
// target total.
func LoadExpression(c *segment.Counts, cells, clusters []uint32, target float64) (*Expression, error) {
	n := len(c.Offsets) - 1
	in := make([]bool, n)
	for _, i := range cells {
		if int(i) >= n {
			return nil, fmt.Errorf("the clustering names cell %d of a segmentation of %d", i, n)
		}
		in[i] = true
	}
	m, _, err := selectCells(c, func(i int, total uint32) bool { return in[i] })
	if err != nil {
		return nil, err
	}
	if len(m.cells) != len(cells) {
		return nil, errors.New("the clustering's cells have no gene transcript in this segmentation: it is of another one")
	}
	if !(target > 0) {
		return nil, errors.New("the clustering records no target total")
	}
	m.normalize(target)
	e := &Expression{m: m, Genes: c.Genes, Cells: cells, Cluster: clusters, byGene: make([][]int, m.genes)}
	for i := range m.cells {
		for k := m.offsets[i]; k < m.offsets[i+1]; k++ {
			e.byGene[m.gene[k]] = append(e.byGene[m.gene[k]], k)
		}
	}
	return e, nil
}

// cellOf answers the analyzed cell an entry of m belongs to.
func (e *Expression) cellOf(k int) int {
	return sort.Search(len(e.m.cells), func(i int) bool { return e.m.offsets[i+1] > k })
}

// Values answers a gene's log-normalized value in each analyzed cell (0 where it has none).
func (e *Expression) Values(gene int) []float32 {
	out := make([]float32, len(e.Cells))
	for _, k := range e.byGene[gene] {
		out[e.cellOf(k)] = float32(e.m.value[k])
	}
	return out
}

// GeneMeans answers each gene's mean log-normalized value over the analyzed cells (zeros
// included), summed in the cells' order.
func (e *Expression) GeneMeans() []float64 {
	out := make([]float64, e.m.genes)
	for k, g := range e.m.gene {
		out[g] += e.m.value[k]
	}
	n := float64(len(e.m.cells))
	for g := range out {
		out[g] /= n
	}
	return out
}

// ListMeans answers, for each list of genes (indices, each listed once), each analyzed cell's mean
// log-normalized value over the list's genes (zeros included), summed in the genes' order.
func (e *Expression) ListMeans(lists [][]int) [][]float64 {
	in := make([][]int32, e.m.genes) // the lists each gene is in
	for l, list := range lists {
		for _, g := range list {
			in[g] = append(in[g], int32(l))
		}
	}
	n := len(e.m.cells)
	out := make([][]float64, len(lists))
	for l := range out {
		out[l] = make([]float64, n)
	}
	parallel(n, runtime.NumCPU(), func(i int) {
		for k := e.m.offsets[i]; k < e.m.offsets[i+1]; k++ {
			for _, l := range in[e.m.gene[k]] {
				out[l][i] += e.m.value[k]
			}
		}
	})
	for l, list := range lists {
		if len(list) == 0 {
			continue
		}
		size := float64(len(list))
		for i := range out[l] {
			out[l][i] /= size
		}
	}
	return out
}

// Dot answers, for each cluster (0…clusters−1) and each gene asked, the mean log-normalized value
// of its cells and the share of them expressing the gene.
func (e *Expression) Dot(genes []int, clusters int) (mean, share [][]float64) {
	sizes := make([]float64, clusters)
	for _, c := range e.Cluster {
		if int(c) < clusters {
			sizes[c]++
		}
	}
	mean = make([][]float64, clusters)
	share = make([][]float64, clusters)
	for c := range mean {
		mean[c] = make([]float64, len(genes))
		share[c] = make([]float64, len(genes))
	}
	for gi, g := range genes {
		for _, k := range e.byGene[g] {
			c := e.Cluster[e.cellOf(k)]
			if int(c) >= clusters {
				continue
			}
			mean[c][gi] += e.m.value[k]
			share[c][gi]++
		}
		for c := range mean {
			if sizes[c] > 0 {
				mean[c][gi] /= sizes[c]
				share[c][gi] /= sizes[c]
			}
		}
	}
	return mean, share
}

// Contrast answers the markers of a selection of the analyzed cells (selected[i] for the i-th)
// against the rest, as each cluster's are against the rest: group 0 the selection, group 1 the
// rest.
func (e *Expression) Contrast(selected []bool, workers int) (*Markers, error) {
	if len(selected) != len(e.Cells) {
		return nil, errors.New("the selection is not of the clustering's cells")
	}
	groups := make([]int32, len(selected))
	in := 0
	for i, s := range selected {
		if s {
			in++
		} else {
			groups[i] = 1
		}
	}
	if in == 0 || in == len(selected) {
		return nil, errors.New("the selection must hold some of the cells analyzed, not all")
	}
	return e.m.markers(groups, 2, workers), nil
}
