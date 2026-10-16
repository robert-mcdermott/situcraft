package simulate

// A single-cell reference of a simulated tissue's types: cells drawn from each type's expression
// over the panel (the simulation's own profiles), clean of the tissue's background and displaced
// molecules, at a depth of single-cell sequencing; for mapping the tissue's cells to a reference
// (internal/label) against their true types.

import (
	"fmt"

	"situcraft/internal/anndata"
	"situcraft/internal/random"
	"situcraft/internal/zarr"
)

// streamReference is the reference's stream (apart from the tissue's).
const streamReference = 1 << 35

// ReferenceDepth is a reference cell's expected transcripts over the panel, by default.
const ReferenceDepth = 1000.0

// ReferenceCells draws perType cells of each of the tissue's types: each cell's counts over the
// panel's genes, its type's name, and the genes' names. A cell's expected transcripts over the
// panel are depth, spread log-normally (σ 0.4); its genes' counts are negative-binomial about its
// type's profile, at its type's dispersion.
func ReferenceCells(p Params, perType int, depth float64, seed uint64) (rows [][]uint32, labels, genes []string) {
	shares := make([]float64, len(p.Types))
	for k := range shares {
		shares[k] = 1 / float64(len(shares))
	}
	pn := makePanel(p, random.New(p.seed(streamPanel), streamPanel), shares)
	genes = pn.names[:p.Genes]
	s := random.New(seed, streamReference)
	for k, ty := range p.Types {
		for i := 0; i < perType; i++ {
			total := mul(depth, s.LogNormal(0, 0.4))
			row := make([]uint32, p.Genes)
			for g := range row {
				row[g] = uint32(s.NegativeBinomial(mul(total, pn.profiles[k][g]), ty.Dispersion))
			}
			rows = append(rows, row)
			labels = append(labels, ty.Name)
		}
	}
	return rows, labels, genes
}

// WriteReference writes a reference of the tissue's types (ReferenceCells) as an AnnData object in
// Zarr at path: the cells' types in obs (cell_type), the genes in var, the counts in X.
func WriteReference(p Params, path string, perType int, depth float64, seed uint64) error {
	rows, labels, genes := ReferenceCells(p, perType, depth, seed)
	w, err := zarr.NewWriter(path, 2, nil)
	if err != nil {
		return err
	}
	o := &anndata.Object{}
	for i := range rows {
		o.Obs.Index = append(o.Obs.Index, fmt.Sprintf("ref-%06d", i+1))
	}
	o.Obs.Columns = []anndata.WriteColumn{{Name: "cell_type", Text: labels, Categorical: true}}
	o.Var.Index = genes
	o.X.Indptr = []int64{0}
	for _, row := range rows {
		for g, n := range row {
			if n > 0 {
				o.X.Indices = append(o.X.Indices, int32(g))
				o.X.Data = append(o.X.Data, float32(n))
			}
		}
		o.X.Indptr = append(o.X.Indptr, int64(len(o.X.Data)))
	}
	o.Uns = map[string]any{"situcraft": map[string]any{"about": "A single-cell reference of a simulated tissue's types, drawn from their expression profiles", "seed": float64(seed), "cellsPerType": float64(perType), "depth": depth}}
	return anndata.Write(w, "", o)
}
