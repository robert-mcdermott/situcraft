package compare

// Two measures of transcripts given to the wrong cell, read in how genes co-occur in cells.
//
// Spurious co-expression (Jones DC, Elz AE, Hadadianpour A, et al. Cell simulation as cell
// segmentation. Nat Methods. 2025;22:1331–1342. doi:10.1038/s41592-025-02697-0, Methods,
// "Testing for spurious co-expression"): nuclei misassign fewer transcripts than whole cells, so
// gene pairs co-expressed more often once cells are drawn around nuclei than in nuclei alone point
// to misassignment. The co-expression of gene i given gene j is the share of cells expressing j (at
// least one transcript) that also express i. The spurious pairs are those whose co-expression
// rises at least 1.5-fold from the nuclei to an intentionally coarse expansion of them; a
// segmentation's score for a pair is its co-expression divided by the nuclei's. Every cell is first
// down-sampled to the same 50 transcripts (cells with fewer are left out), a multinomial draw in its
// own proportions, so that assigning fewer transcripts, or splitting cells, is not rewarded.
// SituCraft's choices where the paper leaves them open: the nuclei are the dataset's nucleus mask
// and the coarse expansion grows them by 10 µm; pairs need 20 cells expressing the conditioning
// gene in both; the pairs are found on the down-sampled cells too; a segmentation's score is the
// median of its pairs' (1: as in nuclei alone).
//
// Mutually exclusive co-expression rate (Hartman A, Satija R. Comparative analysis of multiplexed
// in situ gene expression profiling technologies. bioRxiv 2024. doi:10.1101/2024.01.11.575135): for
// two genes that mark different cell types, the share of cells expressing either that express
// both; averaged over the pairs. The pairs come from a reference of known cell types: by default
// SituCraft's table of lineage markers (LineageMarkers), pairs of different lineages, as far as the
// panel has them.

import (
	"math"
	"math/rand/v2"
	"sort"

	"situcraft/internal/segment"
)

// Reference is a dataset's co-expression reference: its spurious gene pairs and their
// co-expression in nuclei.
type Reference struct {
	Genes []string
	// Pairs are ordered: Pairs[k] = (i, j), the co-expression of Genes[i] given Genes[j].
	Pairs        [][2]int
	Nuclear      []float64
	NuclearCells int
	CoarseCells  int
}

// downsampled are cells' expressed genes after down-sampling: for each cell kept, the gene
// indices (in names' order) with at least one transcript.
func downsampled(c *segment.Counts, names map[string]int, p Params) [][]int {
	var out [][]int
	for cell := 0; cell+1 < len(c.Offsets); cell++ {
		a, b := c.Offsets[cell], c.Offsets[cell+1]
		var total uint32
		for k := a; k < b; k++ {
			total += c.Count[k]
		}
		if int(total) < p.Depth || total == 0 {
			continue
		}
		// Each cell its own stream: the same cell of the same counts draws the same transcripts.
		rng := rand.New(rand.NewPCG(p.Seed, uint64(cell)))
		drawn := make([]int, b-a)
		// A multinomial draw of Depth transcripts in the cell's proportions.
		for d := 0; d < p.Depth; d++ {
			u := rng.Uint32N(total)
			var acc uint32
			for k := a; k < b; k++ {
				acc += c.Count[k]
				if u < acc {
					drawn[k-a]++
					break
				}
			}
		}
		var genes []int
		for k := a; k < b; k++ {
			if drawn[k-a] > 0 {
				if g, ok := names[c.Genes[c.Gene[k]]]; ok {
					genes = append(genes, g)
				}
			}
		}
		sort.Ints(genes)
		out = append(out, genes)
	}
	return out
}

// cooccurrence counts, over cells, those expressing each gene and each pair.
func cooccurrence(cells [][]int, n int) ([]int, []int) {
	expressing := make([]int, n)
	both := make([]int, n*n)
	for _, genes := range cells {
		for _, j := range genes {
			expressing[j]++
			for _, i := range genes {
				if i != j {
					both[i*n+j]++
				}
			}
		}
	}
	return expressing, both
}

// NewReference finds the spurious pairs from the nuclei's counts and a coarse expansion's.
func NewReference(nuclear, coarse *segment.Counts, p Params) *Reference {
	if p.Depth == 0 {
		p = DefaultParams()
	}
	names := map[string]int{}
	var genes []string
	for _, c := range []*segment.Counts{nuclear, coarse} {
		for _, g := range c.Genes {
			if _, ok := names[g]; !ok {
				names[g] = len(genes)
				genes = append(genes, g)
			}
		}
	}
	n := len(genes)
	nc := downsampled(nuclear, names, p)
	cc := downsampled(coarse, names, p)
	ne, nb := cooccurrence(nc, n)
	ce, cb := cooccurrence(cc, n)
	r := &Reference{Genes: genes, NuclearCells: len(nc), CoarseCells: len(cc)}
	for j := 0; j < n; j++ {
		if ne[j] < p.MinCells || ce[j] < p.MinCells {
			continue
		}
		for i := 0; i < n; i++ {
			if i == j || nb[i*n+j] == 0 {
				continue
			}
			rn := float64(nb[i*n+j]) / float64(ne[j])
			rc := float64(cb[i*n+j]) / float64(ce[j])
			if rc >= p.Fold*rn {
				r.Pairs = append(r.Pairs, [2]int{i, j})
				r.Nuclear = append(r.Nuclear, rn)
			}
		}
	}
	return r
}

// Score is a segmentation's spurious co-expression: the median, over the reference's pairs, of its
// co-expression relative to the nuclei's; and the cells it was measured on.
func (r *Reference) Score(c *segment.Counts, p Params) (float64, int) {
	if p.Depth == 0 {
		p = DefaultParams()
	}
	names := map[string]int{}
	for i, g := range r.Genes {
		names[g] = i
	}
	n := len(r.Genes)
	cells := downsampled(c, names, p)
	e, both := cooccurrence(cells, n)
	var ratios []float64
	for k, pair := range r.Pairs {
		i, j := pair[0], pair[1]
		if e[j] < p.MinCells {
			continue
		}
		ratios = append(ratios, float64(both[i*n+j])/float64(e[j])/r.Nuclear[k])
	}
	if len(ratios) == 0 {
		return math.NaN(), len(cells)
	}
	return round6(median(ratios)), len(cells)
}

// MECR is the mean, over the pairs whose genes the segmentation detects, of the share of cells
// expressing either gene that express both.
func MECR(c *segment.Counts, pairs [][2]string) float64 {
	index := map[string]uint32{}
	for i, g := range c.Genes {
		index[g] = uint32(i)
	}
	type pair struct{ a, b uint32 }
	var used []pair
	for _, p := range pairs {
		a, okA := index[p[0]]
		b, okB := index[p[1]]
		if okA && okB && a != b {
			used = append(used, pair{a, b})
		}
	}
	if len(used) == 0 {
		return math.NaN()
	}
	either, both := make([]int, len(used)), make([]int, len(used))
	has := map[uint32]bool{}
	for cell := 0; cell+1 < len(c.Offsets); cell++ {
		clear(has)
		for k := c.Offsets[cell]; k < c.Offsets[cell+1]; k++ {
			if c.Count[k] > 0 {
				has[c.Gene[k]] = true
			}
		}
		for i, p := range used {
			x, y := has[p.a], has[p.b]
			if x || y {
				either[i]++
			}
			if x && y {
				both[i]++
			}
		}
	}
	var sum float64
	var n int
	for i := range used {
		if either[i] > 0 {
			sum += float64(both[i]) / float64(either[i])
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return round6(sum / float64(n))
}

// MixedPairs counts, for each cell, the pairs of mutually exclusive markers it expresses together.
func MixedPairs(c *segment.Counts, pairs [][2]string) []uint32 {
	index := map[string]uint32{}
	for i, g := range c.Genes {
		index[g] = uint32(i)
	}
	type pair struct{ a, b uint32 }
	var used []pair
	for _, p := range pairs {
		a, okA := index[p[0]]
		b, okB := index[p[1]]
		if okA && okB && a != b {
			used = append(used, pair{a, b})
		}
	}
	out := make([]uint32, max(0, len(c.Offsets)-1))
	has := map[uint32]bool{}
	for cell := range out {
		clear(has)
		for k := c.Offsets[cell]; k < c.Offsets[cell+1]; k++ {
			if c.Count[k] > 0 {
				has[c.Gene[k]] = true
			}
		}
		for _, p := range used {
			if has[p.a] && has[p.b] {
				out[cell]++
			}
		}
	}
	return out
}

// LineageMarkers are genes that mark one lineage and not the others (SituCraft's table, from the
// cell types' canonical markers), for the MECR's pairs when no reference gives them.
var LineageMarkers = map[string][]string{
	"epithelial":    {"EPCAM", "KRT8", "KRT18", "KRT19", "KRT7", "CDH1", "ELF3", "CLDN4"},
	"T cell":        {"CD3E", "CD3D", "CD2", "TRAC", "CD247"},
	"B cell":        {"MS4A1", "CD19", "BANK1", "CD22"},
	"myeloid":       {"CD68", "CD163", "LYZ", "CD14", "C1QA", "C1QC", "MRC1"},
	"endothelial":   {"PECAM1", "VWF", "CDH5", "PLVAP", "CLDN5", "EMCN"},
	"fibroblast":    {"COL1A1", "COL1A2", "LUM", "DCN", "PDGFRA", "FBLN1"},
	"smooth muscle": {"MYH11", "DES", "CNN1"},
	"plasma cell":   {"MZB1", "JCHAIN", "IGHG1", "TNFRSF17"},
	"mast cell":     {"TPSAB1", "CPA3", "KIT"},
}

// LineagePairs are the pairs of lineage markers of different lineages that the panel (genes) has,
// in a fixed order.
func LineagePairs(genes []string) [][2]string {
	panel := map[string]bool{}
	for _, g := range genes {
		panel[g] = true
	}
	lineages := make([]string, 0, len(LineageMarkers))
	for l := range LineageMarkers {
		lineages = append(lineages, l)
	}
	sort.Strings(lineages)
	var out [][2]string
	for i, l1 := range lineages {
		for _, l2 := range lineages[i+1:] {
			for _, g1 := range LineageMarkers[l1] {
				for _, g2 := range LineageMarkers[l2] {
					if panel[g1] && panel[g2] {
						out = append(out, [2]string{g1, g2})
					}
				}
			}
		}
	}
	return out
}
