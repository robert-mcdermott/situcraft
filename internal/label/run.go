package label

import (
	"time"

	"situcraft/internal/cluster"
)

// Result is a labeling of a clustering's cells.
type Result struct {
	Set        *MarkerSet
	Sets       []Set
	Genes      []string
	Params     Params
	Lists      *Lists
	Scores     *Scores
	Assignment *Assignment
	Cells      []uint32 // in the segmentation
	Cluster    []uint32
	Clusters   int
	Target     float64
	Seconds    map[string]float64
}

// Run scores a clustering's cells for every label and state of a marker set, on the expression as
// the clustering normalized it, and assigns their labels.
func Run(e *cluster.Expression, set *MarkerSet, clusters int, target float64, p Params) (*Result, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if err := set.Check(); err != nil {
		return nil, err
	}
	start := time.Now()
	sets, err := set.Resolve(e.Genes)
	if err != nil {
		return nil, err
	}
	L, err := MakeLists(sets, e.GeneMeans(), p)
	if err != nil {
		return nil, err
	}
	sc := Combine(L, e.ListMeans(L.All()))
	scored := time.Since(start).Seconds()
	a := Assign(sets, len(set.Levels), sc, p)
	return &Result{Set: set, Sets: sets, Genes: e.Genes, Params: p, Lists: L, Scores: sc, Assignment: a, Cells: e.Cells, Cluster: e.Cluster, Clusters: clusters, Target: target,
		Seconds: map[string]float64{"scores": scored, "assignment": time.Since(start).Seconds() - scored}}, nil
}
