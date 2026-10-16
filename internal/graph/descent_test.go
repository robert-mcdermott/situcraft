package graph

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"situcraft/internal/random"
)

// blobs are n points in dim dimensions about centres groups, each coordinate's spread falling off
// as a principal component's does (a cell's scores are like that: most variance in the first).
func blobs(n, dim, groups int, seed uint64) []float64 {
	s := random.New(seed, 0)
	centres := make([]float64, groups*dim)
	for i := range centres {
		centres[i] = 4 * s.Normal() / float64(1+i%dim/4)
	}
	x := make([]float64, n*dim)
	for i := 0; i < n; i++ {
		c := s.IntN(groups)
		for d := 0; d < dim; d++ {
			x[i*dim+d] = centres[c*dim+d] + s.Normal()/float64(1+d/4)
		}
	}
	return x
}

func recallOf(exact, found *Neighbors) float64 {
	hit := 0
	for i := 0; i < exact.N; i++ {
		row := found.Index[i*found.K : (i+1)*found.K]
		for _, j := range exact.Index[i*exact.K : (i+1)*exact.K] {
			if slices.Contains(row, j) {
				hit++
			}
		}
	}
	return float64(hit) / float64(exact.N*exact.K)
}

// descentDigest is the table's, the same on every processor.
const descentDigest = "6fbb3b9b08cfb456"

// NN-Descent finds nearly every exact neighbor, each row nearest first with its true distance, the
// same table for any number of workers, a start table honored; the rounds stop when few change.
func TestNNDescent(t *testing.T) {
	n, dim, k := 4000, 20, 14
	x := blobs(n, dim, 12, 3)
	exact, err := KNN(context.Background(), x, n, dim, k, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	wide, stats, err := NNDescent(context.Background(), x, n, dim, SearchWidth(k), DefaultDescent(7), 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := &Neighbors{N: n, K: k, Index: make([]int32, n*k), Distances: make([]float64, n*k)}
	for i := 0; i < n; i++ {
		copy(found.Index[i*k:(i+1)*k], wide.Index[i*wide.K:i*wide.K+k])
		copy(found.Distances[i*k:(i+1)*k], wide.Distances[i*wide.K:i*wide.K+k])
	}
	if r := recallOf(exact, found); r < 0.99 {
		t.Errorf("recall %.4f in %d rounds %v", r, stats.Rounds, stats.Changed)
	}
	for i := 0; i < n; i++ {
		for s := 0; s < k; s++ {
			j := int(found.Index[i*k+s])
			if j == i || found.Distances[i*k+s] != sqrt(sqDist(x, dim, i, j)) || s > 0 && found.Distances[i*k+s] < found.Distances[i*k+s-1] {
				t.Fatalf("row %d: neighbor %d (%d) out of order or at the wrong distance", i, s, j)
			}
		}
	}
	if last := stats.Changed[len(stats.Changed)-1]; stats.Rounds >= DefaultDescent(7).MaxRounds || float64(last) >= 0.001*float64(n*SearchWidth(k)) {
		t.Errorf("rounds %d, changed %v", stats.Rounds, stats.Changed)
	}
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, wide.Index)
	binary.Write(h, binary.LittleEndian, wide.Distances)
	if got := fmt.Sprintf("%x", h.Sum(nil))[:16]; got != descentDigest {
		t.Errorf("digest %s, recorded %s", got, descentDigest)
	}
	one, _, err := NNDescent(context.Background(), x, n, dim, SearchWidth(k), DefaultDescent(7), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(one.Index, wide.Index) || !slices.Equal(one.Distances, wide.Distances) {
		t.Error("the table differs with the workers")
	}
	// Started from the exact table, one round finds nothing nearer: it is kept.
	p := DefaultDescent(7)
	p.Start = exact
	kept, st, err := NNDescent(context.Background(), x, n, dim, k, p, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kept.Index, exact.Index) || st.Rounds != 1 || st.Changed[0] != 0 {
		t.Errorf("from the exact table: %d rounds, %v changed", st.Rounds, st.Changed)
	}
}

// Up to the limit the neighbors are exact and no search is reported; the sampled recall of an
// exact table is 1, of a table half of whose neighbors are wrong, a half.
func TestNearestAndSampledRecall(t *testing.T) {
	n, dim, k := 600, 5, 4
	x := blobs(n, dim, 3, 5)
	nb, search, err := Nearest(context.Background(), x, n, dim, k, 1, 1, nil, 2, nil)
	if err != nil || search != nil {
		t.Fatalf("below the limit: %v, %+v", err, search)
	}
	exact, _ := KNN(context.Background(), x, n, dim, k, 2, nil)
	if !slices.Equal(nb.Index, exact.Index) {
		t.Error("below the limit, not the exact table")
	}
	if r, _ := SampledRecall(context.Background(), x, n, dim, exact, 100, 3); r != 1 {
		t.Errorf("the exact table's recall %v", r)
	}
	half := &Neighbors{N: n, K: k, Index: slices.Clone(exact.Index), Distances: slices.Clone(exact.Distances)}
	for i := 0; i < n; i++ {
		for s := 0; s < k/2; s++ {
			// The point itself, never among its own neighbors.
			half.Index[i*k+s] = int32(i)
		}
	}
	if r, _ := SampledRecall(context.Background(), x, n, dim, half, 100, 3); r != 0.5 {
		t.Errorf("half the neighbors wrong: recall %v", r)
	}
}
