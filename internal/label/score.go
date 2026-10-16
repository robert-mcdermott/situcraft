package label

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"situcraft/internal/random"
)

// MethodVersion is the version of the method, recorded with every label set.
const MethodVersion = "1"

// Params are a labeling's settings.
type Params struct {
	// Margin is how far a level's best label must lead the next (or the minimum) for a cell to take
	// it.
	Margin float64 `json:"margin"`
	// Minimum is the score a label must pass to be taken at all (0: its markers above their
	// controls).
	Minimum float64 `json:"minimum"`
	// State is the score a state's markers must reach for a cell to carry it.
	State float64 `json:"state"`
	// Bins and Controls are scanpy's n_bins and ctrl_size: the genes are cut into bins by mean
	// expression, and a list's controls drawn from its genes' bins, Controls from each.
	Bins     int `json:"bins"`
	Controls int `json:"controls"`
	// Lineage leaves a label's lineage's markers (its ancestors' and descendants') out of its
	// controls: on a targeted panel genes of matched expression are often markers, and these the
	// label's own cells express.
	Lineage bool   `json:"lineage"`
	Seed    uint64 `json:"seed"`
}

// DefaultParams are scanpy's binning and SituCraft's margin.
func DefaultParams() Params {
	return Params{Margin: 0.1, Minimum: 0, State: 0.25, Bins: 25, Controls: 50, Lineage: true, Seed: 20250}
}

// Check says what is wrong with the parameters, if anything.
func (p Params) Check() error {
	switch {
	case !(p.Margin > 0 && p.Margin <= 5):
		return errors.New("Margin is a number above 0, at most 5")
	case !(p.Minimum >= -5 && p.Minimum <= 5):
		return errors.New("Minimum score is a number from -5 to 5")
	case !(p.State >= -5 && p.State <= 5):
		return errors.New("State threshold is a number from -5 to 5")
	case p.Bins < 2 || p.Bins > 1000:
		return errors.New("Expression bins is a whole number from 2 to 1000")
	case p.Controls < 1 || p.Controls > 10000:
		return errors.New("Control genes per bin is a whole number from 1 to 10000")
	}
	return nil
}

// roundEven is x rounded to the nearest whole number, halves to the even one (numpy's round).
func roundEven(x float64) int {
	return int(math.RoundToEven(x))
}

// Bins cuts a pool of genes (indices) into bins by mean expression as scanpy's score_genes does:
// each gene's rank among the pool's means (ties given the lowest rank, pandas' rank(method="min")),
// divided by the pool's size over bins − 1 (rounded half to even, at least 1), rounded down. Genes
// outside the pool are in no bin (−1).
func Bins(means []float64, pool []int, bins int) []int {
	order := append([]int(nil), pool...)
	sort.SliceStable(order, func(a, b int) bool { return means[order[a]] < means[order[b]] })
	items := max(roundEven(float64(len(pool))/float64(bins-1)), 1)
	out := make([]int, len(means))
	for g := range out {
		out[g] = -1
	}
	rank := 0
	for k, g := range order {
		if k == 0 || means[g] != means[order[k-1]] {
			rank = k + 1
		}
		out[g] = rank / items
	}
	return out
}

// Controls are a list's control genes as scanpy's score_genes chooses them from a pool binned by
// Bins: for each bin holding one of the list's genes (in increasing order), the bin's genes (the
// list's included), or Controls of them drawn at random when the bin holds more; their union, less
// the list's genes, in the panel's order. The draws are SituCraft's (a seeded stream per list), not
// numpy's: where no bin holds more than Controls genes (pools up to 1,200 genes at scanpy's
// defaults) there is no draw and the controls are scanpy's.
func Controls(bin []int, list []int, controls int, s *random.Stream) []int {
	inList := map[int]bool{}
	cuts := map[int]bool{}
	for _, g := range list {
		inList[g] = true
		cuts[bin[g]] = true
	}
	var order []int
	for c := range cuts {
		order = append(order, c)
	}
	sort.Ints(order)
	chosen := map[int]bool{}
	for _, c := range order {
		var genes []int
		for g, b := range bin {
			if b == c {
				genes = append(genes, g)
			}
		}
		if controls < len(genes) {
			// A partial Fisher–Yates shuffle: the first Controls of a random order.
			for k := 0; k < controls; k++ {
				j := k + s.IntN(len(genes)-k)
				genes[k], genes[j] = genes[j], genes[k]
			}
			genes = genes[:controls]
		}
		for _, g := range genes {
			chosen[g] = true
		}
	}
	var out []int
	for g := range bin {
		if chosen[g] && !inList[g] {
			out = append(out, g)
		}
	}
	return out
}

// Lists are the gene lists a labeling scores: each set's positive markers and their controls,
// and its negative markers and theirs (empty when it has none).
type Lists struct {
	Positive, PositiveControls [][]int
	Negative, NegativeControls [][]int
}

// Pool is the genes the controls of set k's list are drawn from (scanpy's gene_pool): with
// lineage, every gene but the positive markers of the label's ancestors and descendants (genes the
// same cells express by design: a type's class's, a class's types'), with the list's own; else
// every gene.
func Pool(sets []Set, genes, k int, list []int, lineage bool) []int {
	out := make([]bool, genes)
	if lineage && !sets[k].State {
		related := func(j int) bool { // j is an ancestor or a descendant of k
			for a := sets[j].Parent; a >= 0; a = sets[a].Parent {
				if a == k {
					return true
				}
			}
			for a := sets[k].Parent; a >= 0; a = sets[a].Parent {
				if a == j {
					return true
				}
			}
			return false
		}
		for j, s := range sets {
			if j != k && !s.State && related(j) {
				for _, g := range s.Positive {
					out[g] = true
				}
			}
		}
		for _, g := range list {
			out[g] = false
		}
	}
	var pool []int
	for g := 0; g < genes; g++ {
		if !out[g] {
			pool = append(pool, g)
		}
	}
	return pool
}

// MakeLists chooses every set's controls from the genes' means. Each list draws from its own
// stream (the seed and the set's place, positive or negative), so a set's controls do not depend
// on the others.
func MakeLists(sets []Set, means []float64, p Params) (*Lists, error) {
	L := &Lists{}
	for k, s := range sets {
		controls := func(list []int, stream uint64) []int {
			bin := Bins(means, Pool(sets, len(means), k, list, p.Lineage), p.Bins)
			return Controls(bin, list, p.Controls, random.New(p.Seed, stream))
		}
		pc := controls(s.Positive, uint64(2*k))
		if len(pc) == 0 {
			return nil, fmt.Errorf("%s's positive markers have no control genes: the panel is too small for %d bins", s.Name, p.Bins)
		}
		var nc []int
		if len(s.Negative) > 0 {
			if nc = controls(s.Negative, uint64(2*k+1)); len(nc) == 0 {
				return nil, fmt.Errorf("%s's negative markers have no control genes: the panel is too small for %d bins", s.Name, p.Bins)
			}
		}
		L.Positive = append(L.Positive, s.Positive)
		L.PositiveControls = append(L.PositiveControls, pc)
		L.Negative = append(L.Negative, s.Negative)
		L.NegativeControls = append(L.NegativeControls, nc)
	}
	return L, nil
}

// Scores are each cell's scores for each set: the positive markers' score (their mean less their
// controls' mean, scanpy's score_genes), the negative markers' (likewise; NaN for a set without
// negative markers), and the set's score, the positive less the negative where the negative is
// above 0 (a negative marker counts against a label only where the cell expresses it above its
// controls).
type Scores struct {
	Cells    int
	Positive [][]float64 // [set][cell]
	Negative [][]float64
	Score    [][]float64
}

// Combine makes the scores from each list's means over the cells (as cluster.Expression's
// ListMeans answers them for the lists in the order Positive, PositiveControls, Negative,
// NegativeControls).
func Combine(L *Lists, means [][]float64) *Scores {
	S := len(L.Positive)
	n := 0
	if len(means) > 0 {
		n = len(means[0])
	}
	sc := &Scores{Cells: n}
	for k := 0; k < S; k++ {
		pos, pc := means[k], means[S+k]
		neg, nc := means[2*S+k], means[3*S+k]
		p := make([]float64, n)
		q := make([]float64, n)
		s := make([]float64, n)
		for i := 0; i < n; i++ {
			p[i] = pos[i] - pc[i]
			s[i] = p[i]
			if len(L.Negative[k]) == 0 {
				q[i] = math.NaN()
				continue
			}
			q[i] = neg[i] - nc[i]
			if q[i] > 0 {
				s[i] -= q[i]
			}
		}
		sc.Positive = append(sc.Positive, p)
		sc.Negative = append(sc.Negative, q)
		sc.Score = append(sc.Score, s)
	}
	return sc
}

// All are the lists in the order Combine takes their means.
func (L *Lists) All() [][]int {
	var out [][]int
	for _, part := range [][][]int{L.Positive, L.PositiveControls, L.Negative, L.NegativeControls} {
		out = append(out, part...)
	}
	return out
}
