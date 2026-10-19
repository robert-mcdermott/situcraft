package spatial

// Cellular neighborhoods (niches): each cell's neighborhood, a window of the labeled cells nearest
// it (its k nearest, itself among them when it is labeled, or those within a radius), described by
// its composition (each label's share of the window's cells), and the cells grouped by these
// compositions by k-means (internal/cluster), as Goltsev et al. and Schürch et al. define cellular
// neighborhoods (Goltsev Y, Samusik N, Kennedy-Darling J, et al. Deep profiling of mouse splenic
// architecture with CODEX multiplexed imaging. Cell 2018;174:968–981; Schürch CM, Bhate SS, Barlow
// GL, et al. Coordinated cellular neighborhoods orchestrate antitumoral immunity at the colorectal
// cancer invasive front. Cell 2020;182:1341–1359). A niche is a kind of place, not a kind of cell:
// a cell unlabeled itself has the niche of the labeled cells around it. Niches are numbered by
// size, the largest first. k-means finds a local optimum from its seeding: the niches found from
// further seeds are compared with these by the adjusted Rand index, so their agreement is stated.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"situcraft/internal/cluster"
)

// NicheParams are the window (Neighbors nearest labeled cells, or those within Radius µm: one of
// them), the niches' number, k-means's starts, its seed, and the further seeds run to measure the
// agreement.
type NicheParams struct {
	Neighbors int     `json:"neighbors"`
	Radius    float64 `json:"radius"`
	Niches    int     `json:"niches"`
	Starts    int     `json:"starts"`
	Seeds     int     `json:"seeds"`
	Seed      uint64  `json:"seed"`
}

// DefaultNiches are windows of each cell's 10 nearest labeled cells, 6 niches, the best of 10
// starts, and 4 further seeds.
func DefaultNiches() NicheParams {
	return NicheParams{Neighbors: 10, Niches: 6, Starts: 10, Seeds: 4, Seed: 20250}
}

// Check says what is wrong with the parameters.
func (p *NicheParams) Check() error {
	switch {
	case (p.Neighbors > 0) == (p.Radius > 0) || p.Neighbors < 0 || p.Radius < 0 || math.IsNaN(p.Radius):
		return errors.New("a niche's window is the nearest neighbors or a radius, one of them")
	case p.Neighbors > 0 && (p.Neighbors < 2 || p.Neighbors > 200):
		return errors.New("a window is of 2 to 200 nearest labeled cells")
	case p.Radius > 500:
		return errors.New("a window's radius is at most 500 µm")
	case p.Niches < 2 || p.Niches > 30:
		return errors.New("2 to 30 niches")
	case p.Starts < 1 || p.Starts > 100:
		return errors.New("k-means's starts are 1 to 100")
	case p.Seeds < 0 || p.Seeds > 20:
		return errors.New("0 to 20 further seeds")
	}
	return nil
}

// Niches are cells' niches: each cell's (−1 when no labeled cell is in its window), by niche
// [n·K + k] the mean composition of its cells' windows (its center) and its labeled cells by
// label, each niche's cells, the labels' shares of all the labeled cells, k-means's inertia (and
// each start's), the agreement with each further seed's niches, and each cell's window composition
// (n × K; zeros without one).
type Niches struct {
	K, Count    int
	Assign      []int32
	Composition []float64
	Counts      []int
	Cells       []int
	Tissue      []float64
	Windows     int
	Inertia     float64
	Inertias    []float64
	Agreement   []float64
	Composed    []float64
}

// FindNiches finds the niches of cells (x, y: centroids, µm; label 0 to K − 1, or −1).
func FindNiches(ctx context.Context, x, y []float32, label []int32, K int, p NicheParams, workers int, progress func(float64)) (*Niches, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(x) != len(y) || len(label) != len(x) {
		return nil, errors.New("spatial: the cells' centroids and labels differ in length")
	}
	if K < 1 || K > 1000 {
		return nil, errors.New("spatial: 1 to 1,000 labels")
	}
	if workers < 1 {
		workers = 1
	}
	if progress == nil {
		progress = func(float64) {}
	}
	var labeled []int32
	out := &Niches{K: K, Count: p.Niches, Tissue: make([]float64, K)}
	for i, l := range label {
		if l < -1 || l >= int32(K) {
			return nil, errors.New("spatial: a label beyond the label set")
		}
		if l >= 0 {
			labeled = append(labeled, int32(i))
			out.Tissue[l]++
		}
	}
	if len(labeled) < 2 {
		return nil, errors.New("spatial: fewer than two labeled cells")
	}
	for k := range out.Tissue {
		out.Tissue[k] /= float64(len(labeled))
	}
	comp, has, err := nicheWindows(ctx, x, y, label, K, labeled, p, workers)
	if err != nil {
		return nil, err
	}
	progress(0.2)
	if err := fitNiches(ctx, out, comp, has, label, K, p, workers, progress); err != nil {
		return nil, err
	}
	return out, nil
}

// NicheCells are one sample's cells, for niches found across samples: centroids (µm) and labels
// (0 to K − 1, or −1).
type NicheCells struct {
	X, Y  []float32
	Label []int32
}

// FindNichesAcross finds niches across samples together (wave 7): each sample's windows of its own
// cells, never another's, and all the samples' windows grouped by one k-means, so that a niche is
// the same kind of place in every sample. The niches' cells are the samples' in order (Assign and
// Composed concatenated); offsets says where each sample's start (len(samples) + 1 values).
func FindNichesAcross(ctx context.Context, samples []NicheCells, K int, p NicheParams, workers int, progress func(float64)) (*Niches, []int, error) {
	if err := p.Check(); err != nil {
		return nil, nil, err
	}
	if len(samples) == 0 {
		return nil, nil, errors.New("spatial: no sample")
	}
	if K < 1 || K > 1000 {
		return nil, nil, errors.New("spatial: 1 to 1,000 labels")
	}
	if workers < 1 {
		workers = 1
	}
	if progress == nil {
		progress = func(float64) {}
	}
	out := &Niches{K: K, Count: p.Niches, Tissue: make([]float64, K)}
	offsets := []int{0}
	var comp []float64
	var has []bool
	var labels []int32
	var all int
	for s, sm := range samples {
		if len(sm.X) != len(sm.Y) || len(sm.Label) != len(sm.X) {
			return nil, nil, fmt.Errorf("spatial: sample %d's centroids and labels differ in length", s+1)
		}
		var labeled []int32
		for i, l := range sm.Label {
			if l < -1 || l >= int32(K) {
				return nil, nil, errors.New("spatial: a label beyond the label set")
			}
			if l >= 0 {
				labeled = append(labeled, int32(i))
				out.Tissue[l]++
			}
		}
		if len(labeled) < 2 {
			return nil, nil, fmt.Errorf("spatial: sample %d has fewer than two labeled cells", s+1)
		}
		all += len(labeled)
		c, h, err := nicheWindows(ctx, sm.X, sm.Y, sm.Label, K, labeled, p, workers)
		if err != nil {
			return nil, nil, err
		}
		comp, has, labels = append(comp, c...), append(has, h...), append(labels, sm.Label...)
		offsets = append(offsets, offsets[len(offsets)-1]+len(sm.X))
		progress(0.2 * float64(s+1) / float64(len(samples)))
	}
	for k := range out.Tissue {
		out.Tissue[k] /= float64(all)
	}
	if err := fitNiches(ctx, out, comp, has, labels, K, p, workers, progress); err != nil {
		return nil, nil, err
	}
	return out, offsets, nil
}

// fitNiches groups the cells' windows (comp: n × K, has: whether each cell has one) into p.Niches
// niches by k-means, the largest first, and fills out's assignment, compositions, counts, cells,
// inertias and agreements (out.K, out.Count and out.Tissue set before).
func fitNiches(ctx context.Context, out *Niches, comp []float64, has []bool, label []int32, K int, p NicheParams, workers int, progress func(float64)) error {
	var windowed []int
	for i := range has {
		if has[i] {
			windowed = append(windowed, i)
		}
	}
	out.Windows = len(windowed)
	if len(windowed) < p.Niches {
		return errors.New("spatial: fewer windows than niches")
	}
	data := make([]float64, len(windowed)*K)
	for r, i := range windowed {
		copy(data[r*K:(r+1)*K], comp[i*K:(i+1)*K])
	}
	km, err := cluster.FitKMeans(ctx, data, len(windowed), K, p.Niches, p.Starts, p.Seed, workers)
	if err != nil {
		return err
	}
	progress(0.5)
	for s := 1; s <= p.Seeds; s++ {
		other, err := cluster.FitKMeans(ctx, data, len(windowed), K, p.Niches, p.Starts, p.Seed+uint64(s), workers)
		if err != nil {
			return err
		}
		out.Agreement = append(out.Agreement, cluster.AdjustedRand(km.Assign, other.Assign))
		progress(0.5 + 0.45*float64(s)/float64(p.Seeds))
	}
	// The niches by size, the largest first (on a tie, k-means's order).
	order := make([]int, p.Niches)
	for c := range order {
		order[c] = c
	}
	sort.SliceStable(order, func(a, b int) bool { return km.Sizes[order[a]] > km.Sizes[order[b]] })
	rank := make([]int32, p.Niches)
	for r, c := range order {
		rank[c] = int32(r)
	}
	out.Assign = make([]int32, len(has))
	for i := range out.Assign {
		out.Assign[i] = -1
	}
	out.Composition = make([]float64, p.Niches*K)
	out.Counts = make([]int, p.Niches*K)
	out.Cells = make([]int, p.Niches)
	for r, c := range order {
		copy(out.Composition[r*K:(r+1)*K], km.Centers[c*K:(c+1)*K])
	}
	for r, i := range windowed {
		nc := rank[km.Assign[r]]
		out.Assign[i] = nc
		out.Cells[nc]++
		if label[i] >= 0 {
			out.Counts[int(nc)*K+int(label[i])]++
		}
	}
	out.Inertia, out.Inertias = km.Inertia, km.Inertias
	out.Composed = comp
	return nil
}

// nicheWindows are each cell's window composition (n × K), and whether it has one: of its nearest
// labeled cells (by distance, then index), or of those within the radius.
func nicheWindows(ctx context.Context, x, y []float32, label []int32, K int, labeled []int32, p NicheParams, workers int) ([]float64, []bool, error) {
	// The labeled cells on a grid: for the nearest k, squares holding a few windows' cells each;
	// for a radius, squares of the radius.
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, i := range labeled {
		x0, x1 = math.Min(x0, float64(x[i])), math.Max(x1, float64(x[i]))
		y0, y1 = math.Min(y0, float64(y[i])), math.Max(y1, float64(y[i]))
	}
	side := p.Radius
	k := min(p.Neighbors, len(labeled))
	if p.Neighbors > 0 {
		area := math.Max((x1-x0)*(y1-y0), 1)
		side = math.Max(math.Sqrt(area*float64(k)/float64(len(labeled))), 1e-3)
	}
	cols := int((x1-x0)/side) + 1
	rows := int((y1-y0)/side) + 1
	grid := make([][]int32, cols*rows)
	at := func(v, o float64, m int) int { return min(max(int(math.Floor((v-o)/side)), 0), m-1) }
	for _, i := range labeled {
		c := at(float64(x[i]), x0, cols) + at(float64(y[i]), y0, rows)*cols
		grid[c] = append(grid[c], i)
	}
	// Each cell's window composition, or none.
	n := len(x)
	comp := make([]float64, n*K)
	has := make([]bool, n)
	errs := make([]error, workers)
	parallelFor(n, workers, func(w, a, b int) {
		type near struct {
			d2 float64
			i  int32
		}
		best := make([]near, 0, k+1)
		for i := a; i < b; i++ {
			if (i-a)%1024 == 0 && ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			px, py := float64(x[i]), float64(y[i])
			cx, cy := int(math.Floor((px-x0)/side)), int(math.Floor((py-y0)/side))
			row := comp[i*K : (i+1)*K]
			if p.Radius > 0 {
				r2 := mul(p.Radius, p.Radius)
				count := 0
				for gy := cy - 1; gy <= cy+1; gy++ {
					for gx := cx - 1; gx <= cx+1; gx++ {
						if gx < 0 || gy < 0 || gx >= cols || gy >= rows {
							continue
						}
						for _, j := range grid[gy*cols+gx] {
							dx, dy := float64(x[j])-px, float64(y[j])-py
							if mul(dx, dx)+mul(dy, dy) <= r2 {
								row[label[j]]++
								count++
							}
						}
					}
				}
				if count > 0 {
					has[i] = true
					for l := range row {
						row[l] /= float64(count)
					}
				}
				continue
			}
			// The nearest k, ring by ring of squares: a ring is the last needed once k are found
			// nearer than any cell of the next ring can be.
			best = best[:0]
			for ring := 0; ; ring++ {
				any := false
				for gy := cy - ring; gy <= cy+ring; gy++ {
					for gx := cx - ring; gx <= cx+ring; gx++ {
						if max(abs(gx-cx), abs(gy-cy)) != ring || gx < 0 || gy < 0 || gx >= cols || gy >= rows {
							continue
						}
						any = true
						for _, j := range grid[gy*cols+gx] {
							dx, dy := float64(x[j])-px, float64(y[j])-py
							c := near{mul(dx, dx) + mul(dy, dy), j}
							if len(best) == k && (c.d2 > best[k-1].d2 || c.d2 == best[k-1].d2 && c.i > best[k-1].i) {
								continue
							}
							pos := sort.Search(len(best), func(q int) bool {
								return best[q].d2 > c.d2 || best[q].d2 == c.d2 && best[q].i > c.i
							})
							if len(best) < k {
								best = append(best, near{})
							}
							copy(best[pos+1:], best[pos:len(best)-1])
							best[pos] = c
						}
					}
				}
				reach := float64(ring) * side
				if len(best) == k && best[k-1].d2 <= mul(reach, reach) || !any && ring > max(cols, rows) {
					break
				}
			}
			for _, c := range best {
				row[label[c.i]]++
			}
			for l := range row {
				row[l] /= float64(len(best))
			}
			has[i] = true
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	return comp, has, nil
}
