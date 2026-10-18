package spatial

import (
	"context"
	"math"
	"sort"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/random"
)

// Each cell's window against every labeled cell sorted by distance (then index): its nearest 10,
// and those within 25 µm; unlabeled cells too, some beyond the labeled cells' extent.
func TestNicheWindowsAgainstEveryCell(t *testing.T) {
	s := random.New(91, 1)
	const n, K = 1500, 4
	x, y, label := make([]float32, n), make([]float32, n), make([]int32, n)
	var labeled []int32
	for i := 0; i < n; i++ {
		x[i], y[i] = float32(400*s.Float64()), float32(300*s.Float64())
		label[i] = int32(s.IntN(K+1)) - 1
		if i < 20 {
			x[i] += 450 // far to the right, unlabeled
			label[i] = -1
		}
		if label[i] >= 0 {
			labeled = append(labeled, int32(i))
		}
	}
	for _, p := range []NicheParams{{Neighbors: 10, Niches: 2, Starts: 1}, {Radius: 25, Niches: 2, Starts: 1}} {
		comp, has, err := nicheWindows(context.Background(), x, y, label, K, labeled, p, 4)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			type near struct {
				d2 float64
				j  int32
			}
			var all []near
			for _, j := range labeled {
				dx, dy := float64(x[j])-float64(x[i]), float64(y[j])-float64(y[i])
				all = append(all, near{dx*dx + dy*dy, j})
			}
			sort.Slice(all, func(a, b int) bool { return all[a].d2 < all[b].d2 || all[a].d2 == all[b].d2 && all[a].j < all[b].j })
			want := make([]float64, K)
			m := 0
			for _, c := range all {
				if p.Neighbors > 0 && m == p.Neighbors || p.Radius > 0 && c.d2 > p.Radius*p.Radius {
					break
				}
				want[label[c.j]]++
				m++
			}
			if (m > 0) != has[i] {
				t.Fatalf("cell %d: a window %v, by every cell %d cells", i, has[i], m)
			}
			for k := range want {
				if m > 0 && math.Abs(comp[i*K+k]-want[k]/float64(m)) > 1e-12 {
					t.Fatalf("cell %d (window %+v): composition %v, by every cell %v of %d", i, p, comp[i*K:(i+1)*K], want, m)
				}
			}
		}
	}
}

// Three stripes of different composition: three niches find them, the same from every seed, on one
// worker and on eight.
func TestNichesFindStripes(t *testing.T) {
	s := random.New(93, 2)
	const n, K = 6000, 4
	mixes := [3][K]float64{{0.8, 0.1, 0.1, 0}, {0.1, 0.8, 0, 0.1}, {0, 0.1, 0.45, 0.45}}
	x, y, label := make([]float32, n), make([]float32, n), make([]int32, n)
	stripe := make([]int32, n)
	for i := 0; i < n; i++ {
		x[i], y[i] = float32(600*s.Float64()), float32(400*s.Float64())
		stripe[i] = int32(x[i] / 200)
		u := s.Float64()
		label[i] = K - 1
		for k, w := range mixes[stripe[i]] {
			if u < w {
				label[i] = int32(k)
				break
			}
			u -= w
		}
		if s.Float64() < 0.2 {
			label[i] = -1
		}
	}
	p := NicheParams{Neighbors: 15, Niches: 3, Starts: 5, Seeds: 3, Seed: 7}
	got, err := FindNiches(context.Background(), x, y, label, K, p, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	one, err := FindNiches(context.Background(), x, y, label, K, p, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got.Assign {
		if got.Assign[i] != one.Assign[i] {
			t.Fatal("the niches differ on one worker")
		}
	}
	ari := cluster.AdjustedRand(got.Assign, stripe)
	t.Logf("stripes found with an adjusted Rand index of %.3f; agreement between seeds %v; sizes %v", ari, got.Agreement, got.Cells)
	if !(ari > 0.85) {
		t.Errorf("the stripes found with an adjusted Rand index of %.3f", ari)
	}
	for _, a := range got.Agreement {
		if !(a > 0.99) {
			t.Errorf("the seeds agree at %.3f", a)
		}
	}
	if !(got.Cells[0] >= got.Cells[1] && got.Cells[1] >= got.Cells[2]) || got.Windows != n {
		t.Errorf("niches of %v cells, %d windows", got.Cells, got.Windows)
	}
	// Each niche's center is its cells' mean window: the stripe's mix, nearly, and its counts sum
	// to its labeled cells.
	for c := 0; c < 3; c++ {
		sum := 0.0
		for k := 0; k < K; k++ {
			sum += got.Composition[c*K+k]
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("niche %d's composition sums to %g", c, sum)
		}
	}
}
