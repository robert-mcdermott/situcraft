package spatial

// Co-occurrence across distances (Palla G, Spitzer H, Klein M, et al. Squidpy: a scalable framework
// for spatial omics analysis. Nat Methods 2022;19:171–178. doi:10.1038/s41592-021-01358-2): over
// the pairs of labeled cells at a distance, how much likelier a cell of label B is at that distance
// from a cell of label A than from any cell: P(B | A, d) / P(B | d) = count[A][B]·total /
// (row[A]·row[B]), the pairs counted both ways (so count is symmetric and its rows are its
// columns). Above 1, B is found that near A more often than its share of the pairs; below 1, less.
// The pairs at distance d are those at most d apart ("within", as Squidpy 1.8 counts them), or
// those between the distance before and d ("between": [0, d₁], (d₁, d₂], …). Descriptive: no test,
// and it compares labels within the section only.

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sort"
)

// CooccurrenceParams are a co-occurrence's distances (µm, increasing) and how pairs are counted at
// each: within it, or between it and the one before.
type CooccurrenceParams struct {
	Distances []float64 `json:"distances"`
	Pairs     string    `json:"pairs"`
}

// The ways pairs are counted at a distance.
const (
	PairsWithin  = "within"
	PairsBetween = "between"
)

// DefaultCooccurrence are distances of 10 to 150 µm, pairs within each.
func DefaultCooccurrence() CooccurrenceParams {
	d := make([]float64, 15)
	for i := range d {
		d[i] = float64(10 * (i + 1))
	}
	return CooccurrenceParams{Distances: d, Pairs: PairsWithin}
}

// Check says what is wrong with the parameters.
func (p *CooccurrenceParams) Check() error {
	if len(p.Distances) < 1 || len(p.Distances) > 100 {
		return errors.New("a co-occurrence is at 1 to 100 distances")
	}
	for i, d := range p.Distances {
		if math.IsNaN(d) || d <= 0 || i > 0 && d <= p.Distances[i-1] {
			return errors.New("a co-occurrence's distances increase from more than 0 µm")
		}
	}
	if p.Distances[len(p.Distances)-1] > 1000 {
		return errors.New("a co-occurrence reaches at most 1,000 µm")
	}
	if p.Pairs == "" {
		p.Pairs = PairsWithin
	}
	if p.Pairs != PairsWithin && p.Pairs != PairsBetween {
		return errors.New("a co-occurrence counts the pairs within each distance or between distances")
	}
	return nil
}

// Cooccurrence is a co-occurrence's result: at each distance, the ordered pair counts and ratios,
// K × K row-major ([A*K+B]); a ratio is NaN where a label has no pair at that distance.
type Cooccurrence struct {
	K         int
	Distances []float64
	Pairs     string
	Cells     []int
	Count     [][]float64
	Ratio     [][]float64
}

// Cooccur computes the co-occurrence of labeled cells (x, y: centroids in µm; label: 0 to K−1, or −1
// for none, which is left out).
func Cooccur(ctx context.Context, x, y []float32, label []int32, K int, p CooccurrenceParams, workers int) (*Cooccurrence, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(x) != len(y) || len(label) != len(x) {
		return nil, errors.New("spatial: the cells' centroids and labels differ in length")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	nb := len(p.Distances)
	reach := p.Distances[nb-1]
	within := p.Pairs == PairsWithin
	out := &Cooccurrence{K: K, Distances: append([]float64(nil), p.Distances...), Pairs: p.Pairs, Cells: make([]int, K)}
	var cells []int32
	for i := range label {
		if label[i] >= int32(K) || label[i] < -1 {
			return nil, errors.New("spatial: a label beyond the label set")
		}
		if label[i] >= 0 {
			cells = append(cells, int32(i))
			out.Cells[label[i]]++
		}
	}
	// A grid of squares of the farthest edge: a cell's pairs lie in its square and the eight
	// around it.
	type key struct{ x, y int64 }
	x0, y0 := math.Inf(1), math.Inf(1)
	for _, i := range cells {
		x0, y0 = math.Min(x0, float64(x[i])), math.Min(y0, float64(y[i]))
	}
	side := math.Max(reach, 1e-6)
	square := func(i int32) key {
		return key{int64(math.Floor((float64(x[i]) - x0) / side)), int64(math.Floor((float64(y[i]) - y0) / side))}
	}
	grid := map[key][]int32{}
	for _, i := range cells {
		k := square(i)
		grid[k] = append(grid[k], i)
	}
	// Counts are integers, summed exactly whatever the order.
	counts := make([][]int64, workers)
	errs := make([]error, workers)
	parallelFor(len(cells), workers, func(w, a, b int) {
		c := make([]int64, nb*K*K)
		counts[w] = c
		for ci := a; ci < b; ci++ {
			if (ci-a)%2048 == 0 && ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			i := cells[ci]
			k := square(i)
			for dy := int64(-1); dy <= 1; dy++ {
				for dx := int64(-1); dx <= 1; dx++ {
					for _, j := range grid[key{k.x + dx, k.y + dy}] {
						if j <= i {
							continue
						}
						d := distance(x[i], y[i], x[j], y[j])
						if d > reach {
							continue
						}
						// The first distance at least d: the pair is between it and the one
						// before, and within it and every farther one.
						first := sort.SearchFloat64s(p.Distances, d)
						last := first
						if within {
							last = nb - 1
						}
						A, B := int(label[i]), int(label[j])
						for band := first; band <= last; band++ {
							c[band*K*K+A*K+B]++
							c[band*K*K+B*K+A]++
						}
					}
				}
			}
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	for band := 0; band < nb; band++ {
		count := make([]float64, K*K)
		for _, c := range counts {
			if c == nil {
				continue
			}
			for q := 0; q < K*K; q++ {
				count[q] += float64(c[band*K*K+q])
			}
		}
		row := make([]float64, K)
		total := 0.0
		for a := 0; a < K; a++ {
			for b := 0; b < K; b++ {
				row[a] += count[a*K+b]
			}
			total += row[a]
		}
		ratio := make([]float64, K*K)
		for a := 0; a < K; a++ {
			for b := 0; b < K; b++ {
				ratio[a*K+b] = math.NaN()
				if row[a] > 0 && row[b] > 0 {
					ratio[a*K+b] = mul(count[a*K+b], total) / mul(row[a], row[b])
				}
			}
		}
		out.Count = append(out.Count, count)
		out.Ratio = append(out.Ratio, ratio)
	}
	return out, nil
}
