package cluster

// k-means: points in d dimensions put into k clusters, each point in the cluster whose center (the
// mean of its points) is nearest, the sum of squared distances to the centers (the inertia) made
// as small as a local search makes it (Lloyd SP. Least squares quantization in PCM. IEEE Trans Inf
// Theory 1982;28:129–137). Each start is seeded by k-means++: the first center a point chosen at
// random, each next one a point chosen with probability proportional to its squared distance to
// the nearest center chosen (Arthur D, Vassilvitskii S. k-means++: the advantages of careful
// seeding. Proc ACM-SIAM SODA 2007:1027–1035). Lloyd's iterations then assign each point to its
// nearest center (the first, on a tie) and move each center to its points' mean, until no point
// changes cluster (or 300 iterations); a cluster left empty takes the point farthest from its own
// center. Of several starts, each from its own seeded stream, the least inertia is kept. Sums are
// made in fixed chunks of points, in order, so the result is the same on any number of workers.

import (
	"context"
	"errors"
	"math"
	"sync"

	"situcraft/internal/random"
)

// KMeans is a k-means clustering: each point's cluster, the centers (K × D, row-major), the
// clusters' sizes, the inertia, the iterations the kept start took, and each start's inertia.
type KMeans struct {
	K, D       int
	Assign     []int32
	Centers    []float64
	Sizes      []int
	Inertia    float64
	Iterations int
	Start      int
	Inertias   []float64
}

const (
	kmeansChunk      = 4096
	kmeansIterations = 300
)

// FitKMeans clusters n points (data: n × d, row-major) into k clusters, the best of starts.
func FitKMeans(ctx context.Context, data []float64, n, d, k, starts int, seed uint64, workers int) (*KMeans, error) {
	switch {
	case n < 1 || d < 1 || len(data) != n*d:
		return nil, errors.New("k-means: the points are not n × d")
	case k < 1 || k > n:
		return nil, errors.New("k-means: more clusters than points")
	case starts < 1:
		return nil, errors.New("k-means: at least one start")
	}
	if workers < 1 {
		workers = 1
	}
	var best *KMeans
	inertias := make([]float64, starts)
	for s := 0; s < starts; s++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r, err := kmeansStart(ctx, data, n, d, k, random.New(seed, uint64(s)), workers)
		if err != nil {
			return nil, err
		}
		r.Start = s
		inertias[s] = r.Inertia
		if best == nil || r.Inertia < best.Inertia {
			best = r
		}
	}
	best.Inertias = inertias
	return best, nil
}

// chunks runs do over [0, n) in fixed chunks, in parallel; each chunk's index given.
func chunks(n, workers int, do func(c, a, b int)) {
	count := (n + kmeansChunk - 1) / kmeansChunk
	var wg sync.WaitGroup
	next := make(chan int, count)
	for c := 0; c < count; c++ {
		next <- c
	}
	close(next)
	for w := 0; w < min(workers, count); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range next {
				a := c * kmeansChunk
				do(c, a, min(n, a+kmeansChunk))
			}
		}()
	}
	wg.Wait()
}

func squared(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		v := a[i] - b[i]
		s += float64(v * v)
	}
	return s
}

// kmeansPlusPlus chooses k centers' points.
func kmeansPlusPlus(data []float64, n, d, k int, st *random.Stream, workers int) []float64 {
	centers := make([]float64, 0, k*d)
	first := st.IntN(n)
	centers = append(centers, data[first*d:(first+1)*d]...)
	near := make([]float64, n)
	for i := range near {
		near[i] = squared(data[i*d:(i+1)*d], centers[:d])
	}
	count := (n + kmeansChunk - 1) / kmeansChunk
	part := make([]float64, count)
	for c := 1; c < k; c++ {
		chunks(n, workers, func(ch, a, b int) {
			s := 0.0
			for i := a; i < b; i++ {
				s += near[i]
			}
			part[ch] = s
		})
		total := 0.0
		for _, v := range part {
			total += v
		}
		var pick int
		if total > 0 {
			// The point where the cumulative squared distance passes a uniform draw of the total.
			u := st.Float64() * total
			pick = n - 1
			for ch := 0; ch < count; ch++ {
				if u >= part[ch] {
					u -= part[ch]
					continue
				}
				a := ch * kmeansChunk
				for i := a; i < min(n, a+kmeansChunk); i++ {
					if u < near[i] {
						pick = i
						break
					}
					u -= near[i]
				}
				break
			}
		} else {
			pick = st.IntN(n)
		}
		center := data[pick*d : (pick+1)*d]
		centers = append(centers, center...)
		chunks(n, workers, func(_, a, b int) {
			for i := a; i < b; i++ {
				if v := squared(data[i*d:(i+1)*d], center); v < near[i] {
					near[i] = v
				}
			}
		})
	}
	return centers
}

// kmeansStart is one start: seeded, then Lloyd's iterations.
func kmeansStart(ctx context.Context, data []float64, n, d, k int, st *random.Stream, workers int) (*KMeans, error) {
	centers := kmeansPlusPlus(data, n, d, k, st, workers)
	assign := make([]int32, n)
	for i := range assign {
		assign[i] = -1
	}
	dist := make([]float64, n)
	count := (n + kmeansChunk - 1) / kmeansChunk
	sums := make([][]float64, count)
	sizes := make([][]int, count)
	changes := make([]int, count)
	r := &KMeans{K: k, D: d}
	for it := 1; it <= kmeansIterations; it++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunks(n, workers, func(ch, a, b int) {
			changed := 0
			for i := a; i < b; i++ {
				p := data[i*d : (i+1)*d]
				best, bd := 0, math.Inf(1)
				for c := 0; c < k; c++ {
					if v := squared(p, centers[c*d:(c+1)*d]); v < bd {
						best, bd = c, v
					}
				}
				if assign[i] != int32(best) {
					assign[i] = int32(best)
					changed++
				}
				dist[i] = bd
			}
			changes[ch] = changed
		})
		changed := 0
		for _, c := range changes {
			changed += c
		}
		r.Iterations = it
		if changed == 0 && it > 1 {
			break
		}
		// The centers: each chunk's sums, then the chunks' in order.
		chunks(n, workers, func(ch, a, b int) {
			s := make([]float64, k*d)
			z := make([]int, k)
			for i := a; i < b; i++ {
				c := int(assign[i])
				z[c]++
				row := s[c*d : (c+1)*d]
				for j, v := range data[i*d : (i+1)*d] {
					row[j] += v
				}
			}
			sums[ch], sizes[ch] = s, z
		})
		total := make([]float64, k*d)
		size := make([]int, k)
		for ch := range sums {
			for j, v := range sums[ch] {
				total[j] += v
			}
			for c, z := range sizes[ch] {
				size[c] += z
			}
		}
		for c := 0; c < k; c++ {
			if size[c] > 0 {
				for j := 0; j < d; j++ {
					centers[c*d+j] = total[c*d+j] / float64(size[c])
				}
				continue
			}
			// An empty cluster takes the point farthest from its center, of a cluster of more than
			// one point (the first, on a tie).
			far, fd := -1, -1.0
			for i := 0; i < n; i++ {
				if size[assign[i]] > 1 && dist[i] > fd {
					far, fd = i, dist[i]
				}
			}
			if far < 0 {
				return nil, errors.New("k-means: fewer distinct points than clusters")
			}
			size[assign[far]]--
			assign[far] = int32(c)
			size[c] = 1
			dist[far] = 0
			copy(centers[c*d:(c+1)*d], data[far*d:(far+1)*d])
		}
	}
	r.Assign, r.Centers = assign, centers
	r.Sizes = make([]int, k)
	part := make([]float64, count)
	chunks(n, workers, func(ch, a, b int) {
		s := 0.0
		for i := a; i < b; i++ {
			s += squared(data[i*d:(i+1)*d], centers[int(assign[i])*d:(int(assign[i])+1)*d])
		}
		part[ch] = s
	})
	for _, v := range part {
		r.Inertia += v
	}
	for _, c := range assign {
		r.Sizes[c]++
	}
	return r, nil
}
