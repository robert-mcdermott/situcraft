package cohort

// A summary assembled one sample at a time (wave 7, slices 4, 5 and 8): the summary job reads a
// sample's cells, adds what it made of them and lets them go, so that it holds one dataset at a
// time; the cohort's tests' error-rate study and scale test assemble theirs the same way. What
// each sample leaves is small: its document, its pseudobulk over its genes, its panel, and (for
// the niches found across the samples) its chosen cells' centroids, labels and rows.

import (
	"context"
	"fmt"

	"situcraft/internal/spatial"
)

// Builder assembles a summary from its samples.
type Builder struct {
	niches   *spatial.NicheParams
	doc      *Document
	geneAt   map[string]int
	bySample [][][]uint64
	genesOf  [][]string
	cells    []spatial.NicheCells
	rows     [][]uint32
	panels   []Panel
}

// NewBuilder begins a summary of the label set's labels, with niches found across the samples
// when niches is given.
func NewBuilder(labels LabelSet, niches *spatial.NicheParams) *Builder {
	return &Builder{niches: niches, geneAt: map[string]int{},
		doc: &Document{Format: "situcraft-cohort-summary", Version: 1, Method: "SituCraft cohort summary", MethodVersion: MethodVersion, Labels: labels, Genes: []string{}}}
}

// Add adds a sample: its document, its pseudobulk over its genes (labels × genes), the genes, its
// panel, and (with niches) its chosen cells' centroids and labels and their rows.
func (b *Builder) Add(sd SampleDoc, pseudobulk [][]uint64, genes []string, panel Panel, cells spatial.NicheCells, rows []uint32) {
	for _, g := range genes {
		if _, ok := b.geneAt[g]; !ok {
			b.geneAt[g] = len(b.doc.Genes)
			b.doc.Genes = append(b.doc.Genes, g)
		}
	}
	b.bySample, b.genesOf = append(b.bySample, pseudobulk), append(b.genesOf, genes)
	b.doc.Samples = append(b.doc.Samples, sd)
	b.panels = append(b.panels, panel)
	if b.niches != nil {
		b.cells, b.rows = append(b.cells, cells), append(b.rows, rows)
	}
}

// Built is a summary assembled: its document, its pseudobulk (by sample, labels × the document's
// genes, Missing where a sample's panel has not the gene), and with niches each sample's chosen
// cells' rows and niches in order; and why there are no principal components, when there are not.
type Built struct {
	Doc        *Document
	Pseudobulk [][][]uint32
	Rows       []uint32
	Niches     []int32
	NoPCA      error
}

// Finish assembles the summary: the pseudobulk over every sample's genes, the niches across the
// samples together, the panels compared, the samples' quality side by side, and their pseudobulk's
// principal components over the genes every panel has.
func (b *Builder) Finish(ctx context.Context, workers int, progress func(float64)) (*Built, error) {
	if progress == nil {
		progress = func(float64) {}
	}
	doc := b.doc
	K, S, G := len(doc.Labels.Names), len(doc.Samples), len(doc.Genes)
	out := &Built{Doc: doc, Pseudobulk: make([][][]uint32, S)}
	for s := range out.Pseudobulk {
		out.Pseudobulk[s] = make([][]uint32, K)
		has := make([]bool, G)
		for _, g := range b.genesOf[s] {
			has[b.geneAt[g]] = true
		}
		for k := range out.Pseudobulk[s] {
			row := make([]uint32, G)
			for g := range row {
				if !has[g] {
					row[g] = Missing
				}
			}
			for j, g := range b.genesOf[s] {
				v := b.bySample[s][k][j]
				if v >= Missing {
					return nil, fmt.Errorf("%s: %d transcripts of %s in %s, more than the pseudobulk holds", doc.Samples[s].Name, v, g, doc.Labels.Names[k])
				}
				row[b.geneAt[g]] = uint32(v)
			}
			out.Pseudobulk[s][k] = row
		}
	}
	if b.niches != nil {
		p := *b.niches
		nc, offsets, err := spatial.FindNichesAcross(ctx, b.cells, K, p, workers, progress)
		if err != nil {
			return nil, fmt.Errorf("the niches: %w", err)
		}
		nd := &NichesDoc{Parameters: p, Niches: nc.Count, Composition: nc.Composition, Cells: nc.Cells, Tissue: nc.Tissue, Windows: nc.Windows, Inertia: nc.Inertia, Agreement: nc.Agreement}
		if nd.Agreement == nil {
			nd.Agreement = []float64{}
		}
		for v := 0; v < nc.Count; v++ {
			nd.Defining = append(nd.Defining, spatial.DefiningLabels(nc.Composition[v*K:(v+1)*K], nc.Tissue))
		}
		doc.Niches = nd
		for s := range doc.Samples {
			counts := make([]int, nc.Count)
			windowed := 0
			for _, v := range nc.Assign[offsets[s]:offsets[s+1]] {
				if v >= 0 {
					counts[v]++
					windowed++
				}
			}
			shares := make([]float64, nc.Count)
			for v := range shares {
				if windowed > 0 {
					shares[v] = float64(counts[v]) / float64(windowed)
				}
			}
			doc.Samples[s].Niches, doc.Samples[s].NicheShares, doc.Samples[s].NicheCells = counts, shares, windowed
			out.Rows = append(out.Rows, b.rows[s]...)
		}
		out.Niches = nc.Assign
	}
	ids := make([]string, S)
	for k := range doc.Samples {
		ids[k] = doc.Samples[k].Sample
	}
	doc.Panels = ComparePanels(ids, b.panels)
	doc.QCMetrics, doc.OutlierRule = QCMetrics, DefaultOutlierRule
	doc.Outliers, doc.Batches = Outliers(doc.Samples, doc.OutlierRule), Batches(doc.Samples)
	if S >= 2 {
		pca, err := PseudobulkPCA(SharedCounts(doc, out.Pseudobulk, doc.Panels.Shared), doc.Panels.Shared, PCATop, PCAComponents)
		if err != nil {
			out.NoPCA = err
		} else {
			doc.PCA = pca
		}
	}
	return out, nil
}
