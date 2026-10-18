package cluster

import (
	"context"
	"math"
	"testing"

	"situcraft/internal/random"
)

// bestPartition is the least inertia over every way of putting the points into k nonempty
// clusters, by trying them all.
func bestPartition(data []float64, n, d, k int) float64 {
	assign := make([]int, n)
	best := math.Inf(1)
	var try func(i, used int)
	try = func(i, used int) {
		if i == n {
			if used < k {
				return
			}
			sse := 0.0
			for c := 0; c < k; c++ {
				mean := make([]float64, d)
				m := 0
				for p := 0; p < n; p++ {
					if assign[p] == c {
						m++
						for j := 0; j < d; j++ {
							mean[j] += data[p*d+j]
						}
					}
				}
				for p := 0; p < n; p++ {
					if assign[p] == c {
						for j := 0; j < d; j++ {
							v := data[p*d+j] - mean[j]/float64(m)
							sse += v * v
						}
					}
				}
			}
			best = math.Min(best, sse)
			return
		}
		// A new cluster only as the next number, so each partition is tried once.
		for c := 0; c <= min(used, k-1); c++ {
			assign[i] = c
			next := used
			if c == used {
				next++
			}
			try(i+1, next)
		}
	}
	try(0, 0)
	return best
}

// On small sets of points the best of 20 starts is the exact optimum.
func TestKMeansFindsTheOptimumOfSmallSets(t *testing.T) {
	s := random.New(3, 1)
	for rep := 0; rep < 12; rep++ {
		n, d, k := 9, 2, 2+rep%2
		data := make([]float64, n*d)
		for i := range data {
			data[i] = s.Normal()
		}
		want := bestPartition(data, n, d, k)
		got, err := FitKMeans(context.Background(), data, n, d, k, 20, uint64(rep), 2)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(got.Inertia-want) > 1e-9*math.Max(1, want) {
			t.Errorf("set %d, k = %d: inertia %.12g, the optimum %.12g", rep, k, got.Inertia, want)
		}
	}
}

// Well-separated clusters are found exactly; the result is the same on one worker and on eight
// (over several chunks of points); a cluster left empty takes a point.
func TestKMeansSeparatedClustersAndWorkers(t *testing.T) {
	s := random.New(5, 2)
	const n, d, k = 12000, 3, 4
	data := make([]float64, n*d)
	truth := make([]int32, n)
	for i := 0; i < n; i++ {
		c := s.IntN(k)
		truth[i] = int32(c)
		for j := 0; j < d; j++ {
			data[i*d+j] = 20*float64(c) + float64(j%2)*5*float64(c%2) + s.Normal()
		}
	}
	many, err := FitKMeans(context.Background(), data, n, d, k, 5, 9, 8)
	if err != nil {
		t.Fatal(err)
	}
	one, err := FitKMeans(context.Background(), data, n, d, k, 5, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ari := AdjustedRand(many.Assign, truth); ari != 1 {
		t.Errorf("separated clusters found with an adjusted Rand index of %g", ari)
	}
	if one.Inertia != many.Inertia || one.Iterations != many.Iterations {
		t.Errorf("one worker: inertia %g in %d iterations; eight: %g in %d", one.Inertia, one.Iterations, many.Inertia, many.Iterations)
	}
	for i := range one.Assign {
		if one.Assign[i] != many.Assign[i] {
			t.Fatal("the clusters differ on one worker")
		}
	}
	// Four points at two places: two clusters, and three (one place split), each nonempty.
	dup := []float64{0, 0, 0, 0, 1, 1, 1, 1}
	for _, k := range []int{2, 3} {
		r, err := FitKMeans(context.Background(), dup, 4, 2, k, 3, 1, 1)
		if err != nil || r.Inertia != 0 {
			t.Fatalf("%d clusters of two places: %v, %+v", k, err, r)
		}
		for c, z := range r.Sizes {
			if z == 0 {
				t.Errorf("%d clusters of two places: cluster %d empty", k, c)
			}
		}
	}
}
