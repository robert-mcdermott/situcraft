package spatial

import (
	"context"
	"math"
	"sort"
	"testing"

	"situcraft/internal/random"
)

func nearest(t *testing.T, x, y []float32, label []int32, K int, p NearestParams, workers int) *Nearest {
	t.Helper()
	r, err := NearestDistances(context.Background(), x, y, label, K, p, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Each pair's median and mean distance against an exhaustive search; a cell with none within the
// reach counted at it.
func TestNearestDistancesAgainstAnExhaustiveSearch(t *testing.T) {
	s := random.New(61, 1)
	n, K := 700, 3
	x, y, label := make([]float32, n), make([]float32, n), make([]int32, n)
	for i := 0; i < n; i++ {
		x[i], y[i] = float32(500*s.Float64()), float32(500*s.Float64())
		label[i] = int32(s.IntN(K+1)) - 1
		if i%50 == 0 {
			label[i] = 2 // a rare label, some cells far from any
		} else if label[i] == 2 {
			label[i] = 0
		}
	}
	p := NearestParams{Permutations: 19, Seed: 1, Reach: 60, Step: 5}
	r := nearest(t, x, y, label, K, p, 4)
	for a := 0; a < K; a++ {
		for b := 0; b < K; b++ {
			var vals []float64
			beyond := 0
			for i := 0; i < n; i++ {
				if label[i] != int32(a) {
					continue
				}
				best := math.Inf(1)
				for j := 0; j < n; j++ {
					if j != i && label[j] == int32(b) {
						best = math.Min(best, float64(float32(distance(x[i], y[i], x[j], y[j]))))
					}
				}
				if best > p.Reach {
					best = p.Reach
					beyond++
				}
				vals = append(vals, best)
			}
			sort.Float64s(vals)
			sum := 0.0
			for _, v := range vals {
				sum += v
			}
			q := a*K + b
			if math.Abs(r.Median[q]-quantile(vals, 0.5)) > 1e-9 || math.Abs(r.Mean[q]-sum/float64(len(vals))) > 1e-9 || r.Beyond[q] != beyond {
				t.Errorf("pair %d→%d: median %g mean %g beyond %d; the search's %g, %g, %d", a, b, r.Median[q], r.Mean[q], r.Beyond[q], quantile(vals, 0.5), sum/float64(len(vals)), beyond)
			}
		}
	}
}

// Labels kept apart (one on the left, one on the right): each other's nearest are farther than
// chance; labels drawn together (each B a few µm from an A): nearer. Random labels: neither. The same
// on one worker and eight.
func TestNearestDistancesAgainstTheirPermutations(t *testing.T) {
	s := random.New(71, 2)
	n := 900
	x, y := make([]float32, n), make([]float32, n)
	apart, together, random := make([]int32, n), make([]int32, n), make([]int32, n)
	for i := 0; i < n; i++ {
		x[i], y[i] = float32(400*s.Float64()), float32(400*s.Float64())
		if x[i] > 200 {
			apart[i] = 1
		}
		random[i] = int32(s.IntN(2))
	}
	// Together: every tenth cell an A, a B placed 3 µm from each.
	for i := 0; i+1 < n; i += 10 {
		together[i] = 0
		x[i+1], y[i+1] = x[i]+3, y[i]
		together[i+1] = 1
		for k := i + 2; k < i+10 && k < n; k++ {
			together[k] = 2
		}
	}
	p := NearestParams{Permutations: 199, Seed: 3, Reach: 100, Step: 5}
	ap := nearest(t, x, y, apart, 2, p, 4)
	if !(ap.Z[0*2+1] > 5) || ap.P[1] != 1.0/200 {
		t.Errorf("apart: A→B median %g against %g (z %g, p %g)", ap.Median[1], ap.PermMean[1], ap.Z[1], ap.P[1])
	}
	tg := nearest(t, x, y, together, 3, p, 4)
	if !(tg.Z[0*3+1] < -5) || tg.P[1] != 1.0/200 || tg.Median[1] != 3 {
		t.Errorf("together: A→B median %g against %g (z %g, p %g)", tg.Median[1], tg.PermMean[1], tg.Z[1], tg.P[1])
	}
	rn := nearest(t, x, y, random, 2, p, 4)
	if rn.PAdj[0*2+1] < 0.05 || rn.PAdj[1*2+0] < 0.05 {
		t.Errorf("random labels: adjusted p %v", rn.PAdj)
	}
	one := nearest(t, x, y, apart, 2, p, 1)
	for q := range one.Median {
		if one.PermMean[q] != ap.PermMean[q] || one.P[q] != ap.P[q] {
			t.Fatal("nearest distances differ on one worker")
		}
	}
	for q := range one.Lo {
		if one.Lo[q] != ap.Lo[q] || one.Hi[q] != ap.Hi[q] {
			t.Fatal("the envelopes differ on one worker")
		}
	}
}
