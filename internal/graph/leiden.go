package graph

import (
	"context"
	"errors"
	"math"
	"sort"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// LeidenOptions are Leiden's parameters: the resolution γ of modularity, the randomness θ of the
// refinement (leidenalg's default, 0.01), the number of iterations (0: until an iteration moves no
// node, at most 50), and the seed of its one random stream.
type LeidenOptions struct {
	Resolution float64
	Randomness float64
	Iterations int
	Seed       uint64
}

// Partition is a partition of a graph's nodes: each node's community, numbered by decreasing size
// (ties by their lowest node), the sizes, the modularity at the resolution, and the iterations run.
type Partition struct {
	Community  []int32
	Sizes      []int
	Modularity float64
	Iterations int
}

// Modularity is Σ_c [in_c/2m − γ(tot_c/2m)²], in_c summing the weights of ordered pairs of nodes
// inside community c and tot_c its nodes' strengths.
func Modularity(g *CSR, community []int32, resolution float64) float64 {
	count := 0
	for _, c := range community {
		count = max(count, int(c)+1)
	}
	inside := make([]float64, count)
	tot := make([]float64, count)
	twoM := 0.0
	for i := 0; i < g.N; i++ {
		c := community[i]
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			w := g.Weights[e]
			twoM += w
			tot[c] += w
			if community[g.Targets[e]] == c {
				inside[c] += w
			}
		}
	}
	if twoM == 0 {
		return 0
	}
	q := 0.0
	for c := 0; c < count; c++ {
		t := tot[c] / twoM
		q += inside[c]/twoM - mul(resolution, mul(t, t))
	}
	return q
}

// Leiden partitions g by the Leiden algorithm (Traag et al. 2019, Algorithms A.1–A.3, as the
// authors' networkanalysis package arranges them): fast local moving of nodes, a refinement that
// merges only well-connected subsets (randomly, with probability ∝ exp(ΔQ/θ) among the merges that
// do not lower the quality), and aggregation of the refined partition, repeated until a level
// changes nothing; iterations start again from the partition found.
func Leiden(ctx context.Context, g *CSR, o LeidenOptions) (*Partition, error) {
	if o.Resolution <= 0 || math.IsNaN(o.Resolution) || o.Randomness < 0 {
		return nil, errors.New("graph: Leiden needs a positive resolution and a randomness of 0 or more")
	}
	n := g.N
	rnd := random.New(o.Seed, 0)
	level0, err := levelGraph(g)
	if err != nil {
		return nil, err
	}
	comm := make([]int32, n)
	for i := range comm {
		comm[i] = int32(i)
	}
	done := 0
	if level0.total > 0 {
		res := o.Resolution / level0.total
		maxIt := o.Iterations
		if maxIt <= 0 {
			maxIt = 50
		}
		var visit int64
		for it := 0; it < maxIt; it++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			changed := leidenIteration(level0, comm, res, o.Randomness, rnd, &visit)
			done = it + 1
			if !changed && o.Iterations <= 0 {
				break
			}
		}
	}
	p := renumberBySize(comm)
	p.Modularity = Modularity(g, p.Community, o.Resolution)
	p.Iterations = done
	return p, nil
}

// level is a graph at one level of aggregation, without self-loops; node weights (strengths,
// summed over aggregated nodes) are kept apart, so that moving node v into cluster c gains
// w(v, c) − k_v·K_c·γ/2m.
type level struct {
	n       int
	offsets []int
	targets []int32
	weights []float64
	node    []float64
	total   float64
}

func levelGraph(g *CSR) (*level, error) {
	l := &level{n: g.N, offsets: make([]int, g.N+1), node: make([]float64, g.N)}
	for i := 0; i < g.N; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			w := g.Weights[e]
			if !(w >= 0) || math.IsInf(w, 0) {
				return nil, errors.New("graph: edge weights must be finite and not negative")
			}
			l.node[i] += w
			if int(g.Targets[e]) != i && w > 0 {
				l.targets = append(l.targets, g.Targets[e])
				l.weights = append(l.weights, w)
			}
		}
		l.offsets[i+1] = len(l.targets)
	}
	for _, w := range l.node {
		l.total += w
	}
	return l, nil
}

func permutation(n int, rnd *random.Stream) []int32 {
	order := make([]int32, n)
	for i := range order {
		order[i] = int32(i)
	}
	for i := n - 1; i > 0; i-- {
		j := rnd.IntN(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	return order
}

// renumber relabels to 0…count−1 in order of first appearance, answering the count.
func renumber(labels []int32) int {
	m := make([]int32, len(labels))
	for i := range m {
		m[i] = -1
	}
	count := int32(0)
	for i, c := range labels {
		if m[c] < 0 {
			m[c] = count
			count++
		}
		labels[i] = m[c]
	}
	return int(count)
}

// mover is local moving's scratch on a level of n nodes.
type mover struct {
	clusterWeight []float64
	clusterSize   []int32
	unused        []int32
	edgeWeight    []float64
	seen          []int64
	neighbors     []int32
	nUnused       int
}

func newMover(n int) *mover {
	m := &mover{clusterWeight: make([]float64, n), clusterSize: make([]int32, n), unused: make([]int32, n),
		edgeWeight: make([]float64, n), seen: make([]int64, n), neighbors: make([]int32, n+1)}
	for i := range m.seen {
		m.seen[i] = -1
	}
	return m
}

func (m *mover) init(l *level, comm []int32) {
	clear(m.clusterWeight)
	clear(m.clusterSize)
	for v := 0; v < l.n; v++ {
		m.clusterWeight[comm[v]] += l.node[v]
		m.clusterSize[comm[v]]++
	}
	m.nUnused = 0
	for c := l.n - 1; c >= 0; c-- {
		if m.clusterSize[c] == 0 {
			m.unused[m.nUnused] = int32(c)
			m.nUnused++
		}
	}
}

// move puts v in the cluster of largest gain (an empty one included); ties keep it where it is.
func (m *mover) move(l *level, comm []int32, v int, res float64, visit int64) int32 {
	current := comm[v]
	kv := l.node[v]
	m.clusterWeight[current] -= kv
	m.clusterSize[current]--
	if m.clusterSize[current] == 0 {
		m.unused[m.nUnused] = current
		m.nUnused++
	}
	nn := 0
	empty := m.unused[m.nUnused-1]
	m.neighbors[nn] = empty
	nn++
	m.seen[empty] = visit
	m.edgeWeight[empty] = 0
	if m.seen[current] != visit {
		m.seen[current] = visit
		m.edgeWeight[current] = 0
		m.neighbors[nn] = current
		nn++
	}
	for e := l.offsets[v]; e < l.offsets[v+1]; e++ {
		c := comm[l.targets[e]]
		if m.seen[c] != visit {
			m.seen[c] = visit
			m.edgeWeight[c] = 0
			m.neighbors[nn] = c
			nn++
		}
		m.edgeWeight[c] += l.weights[e]
	}
	best := current
	bestGain := m.edgeWeight[current] - mul(mul(kv, m.clusterWeight[current]), res)
	for a := 0; a < nn; a++ {
		c := m.neighbors[a]
		if gain := m.edgeWeight[c] - mul(mul(kv, m.clusterWeight[c]), res); gain > bestGain {
			best, bestGain = c, gain
		}
	}
	m.clusterWeight[best] += kv
	m.clusterSize[best]++
	if best == m.unused[m.nUnused-1] {
		m.nUnused--
	}
	comm[v] = best
	return best
}

// moveFast is Leiden's fast local moving: a queue of every node in random order, a moved node's
// neighbors outside its new cluster queued again.
func moveFast(l *level, comm []int32, res float64, rnd *random.Stream, visit *int64) bool {
	n := l.n
	m := newMover(n)
	m.init(l, comm)
	queue := permutation(n, rnd)
	inQueue := make([]bool, n)
	for i := range inQueue {
		inQueue[i] = true
	}
	head, size := 0, n
	changed := false
	for size > 0 {
		v := int(queue[head])
		head++
		if head == n {
			head = 0
		}
		size--
		inQueue[v] = false
		old := comm[v]
		*visit++
		best := m.move(l, comm, v, res, *visit)
		if best != old {
			changed = true
			for e := l.offsets[v]; e < l.offsets[v+1]; e++ {
				u := l.targets[e]
				if !inQueue[u] && comm[u] != best {
					tail := head + size
					if tail >= n {
						tail -= n
					}
					queue[tail] = u
					size++
					inQueue[u] = true
				}
			}
		}
	}
	return changed
}

// refine is Leiden's refinement (Traag et al. 2019, Algorithm A.2): inside each cluster S of comm,
// singletons well connected to S merge, in random order, into well-connected subsets of S chosen
// at random with probability ∝ exp(ΔQ/θ) among merges with ΔQ ≥ 0.
func refine(l *level, comm []int32, res, randomness float64, rnd *random.Stream, visit *int64) ([]int32, int) {
	n := l.n
	refined := make([]int32, n)
	for v := range refined {
		refined[v] = int32(v)
	}
	clusterTotal := make([]float64, n)
	for v := 0; v < n; v++ {
		clusterTotal[comm[v]] += l.node[v]
	}
	weight := append([]float64(nil), l.node...)
	external := make([]float64, n)
	for v := 0; v < n; v++ {
		for e := l.offsets[v]; e < l.offsets[v+1]; e++ {
			if comm[l.targets[e]] == comm[v] {
				external[v] += l.weights[e]
			}
		}
	}
	singleton := make([]bool, n)
	for i := range singleton {
		singleton[i] = true
	}
	order := permutation(n, rnd)
	edgeWeight := make([]float64, n)
	seen := make([]int64, n)
	for i := range seen {
		seen[i] = -1
	}
	neighbors := make([]int32, n)
	gains := make([]float64, n)
	wellConnected := func(r int32, s int32) bool {
		return external[r] >= mul(mul(weight[r], clusterTotal[s]-weight[r]), res)
	}
	for _, vv := range order {
		v := int(vv)
		if !singleton[v] {
			continue
		}
		s := comm[v]
		kv := l.node[v]
		if !wellConnected(int32(v), s) {
			continue
		}
		weight[v] = 0
		external[v] = 0
		*visit++
		nn := 0
		neighbors[nn] = int32(v)
		nn++
		seen[v] = *visit
		edgeWeight[v] = 0
		for e := l.offsets[v]; e < l.offsets[v+1]; e++ {
			u := l.targets[e]
			if comm[u] != s {
				continue
			}
			r := refined[u]
			if seen[r] != *visit {
				seen[r] = *visit
				edgeWeight[r] = 0
				neighbors[nn] = r
				nn++
			}
			edgeWeight[r] += l.weights[e]
		}
		best := int32(v)
		maxGain := 0.0
		for b := 0; b < nn; b++ {
			r := neighbors[b]
			if wellConnected(r, s) {
				gain := edgeWeight[r] - mul(mul(kv, weight[r]), res)
				gains[b] = gain
				if gain > maxGain {
					maxGain, best = gain, r
				}
			} else {
				gains[b] = math.Inf(-1)
			}
		}
		chosen := best
		if randomness > 0 {
			// exp((ΔQ − ΔQmax)/θ) gives exp(ΔQ/θ)'s probabilities without overflow.
			total := 0.0
			for b := 0; b < nn; b++ {
				if gains[b] >= 0 {
					total += dmath.Exp((gains[b] - maxGain) / randomness)
				}
			}
			r := mul(total, rnd.Float64())
			for b := 0; b < nn; b++ {
				if !(gains[b] >= 0) {
					continue
				}
				r -= dmath.Exp((gains[b] - maxGain) / randomness)
				chosen = neighbors[b]
				if r < 0 {
					break
				}
			}
		}
		weight[chosen] += kv
		for e := l.offsets[v]; e < l.offsets[v+1]; e++ {
			u := l.targets[e]
			if comm[u] != s {
				continue
			}
			if refined[u] == chosen {
				external[chosen] -= l.weights[e]
			} else {
				external[chosen] += l.weights[e]
			}
		}
		if chosen != int32(v) {
			refined[v] = chosen
			singleton[chosen] = false
		}
	}
	return refined, renumber(refined)
}

// aggregate collapses each cluster of labels (0…count−1) to a node; edges between clusters are
// summed (in the order of the clusters' members), and those within a cluster dropped (their weight
// lives on in the node weights).
func aggregate(l *level, labels []int32, count int) *level {
	start := make([]int, count+1)
	for v := 0; v < l.n; v++ {
		start[labels[v]+1]++
	}
	for c := 0; c < count; c++ {
		start[c+1] += start[c]
	}
	members := make([]int32, l.n)
	cursor := append([]int(nil), start[:count]...)
	for v := 0; v < l.n; v++ {
		members[cursor[labels[v]]] = int32(v)
		cursor[labels[v]]++
	}
	out := &level{n: count, offsets: make([]int, count+1), node: make([]float64, count), total: l.total}
	for v := 0; v < l.n; v++ {
		out.node[labels[v]] += l.node[v]
	}
	acc := make([]float64, count)
	mark := make([]int32, count)
	for i := range mark {
		mark[i] = -1
	}
	list := make([]int32, 0, count)
	for c := 0; c < count; c++ {
		list = list[:0]
		for _, v := range members[start[c]:start[c+1]] {
			for e := l.offsets[v]; e < l.offsets[v+1]; e++ {
				d := labels[l.targets[e]]
				if int(d) == c {
					continue
				}
				if mark[d] != int32(c) {
					mark[d] = int32(c)
					acc[d] = 0
					list = append(list, d)
				}
				acc[d] += l.weights[e]
			}
		}
		for _, d := range list {
			out.targets = append(out.targets, d)
			out.weights = append(out.weights, acc[d])
		}
		out.offsets[c+1] = len(out.targets)
	}
	return out
}

// leidenIteration runs one Leiden iteration from comm (a partition of the level-0 graph, changed in
// place), answering whether any node moved.
func leidenIteration(l0 *level, comm []int32, res, randomness float64, rnd *random.Stream, visit *int64) bool {
	l := l0
	current := comm
	var maps [][]int32
	changed := false
	for {
		if moveFast(l, current, res, rnd, visit) {
			changed = true
		}
		count := renumber(current)
		if count == l.n {
			break
		}
		refined, nRefined := refine(l, current, res, randomness, rnd, visit)
		var m, next []int32
		var nl *level
		if nRefined < l.n {
			// Aggregate on the refined partition; each aggregate node starts in its parent cluster.
			nl = aggregate(l, refined, nRefined)
			next = make([]int32, nRefined)
			for v := 0; v < l.n; v++ {
				next[refined[v]] = current[v]
			}
			m = refined
		} else {
			// The refinement merged nothing: aggregate on the clusters, so the graph shrinks.
			nl = aggregate(l, current, count)
			next = make([]int32, count)
			for i := range next {
				next[i] = int32(i)
			}
			m = current
		}
		maps = append(maps, m)
		l = nl
		current = next
	}
	labels := current
	for i := len(maps) - 1; i >= 0; i-- {
		lower := make([]int32, len(maps[i]))
		for v := range lower {
			lower[v] = labels[maps[i][v]]
		}
		labels = lower
	}
	copy(comm, labels)
	return changed
}

// renumberBySize numbers communities by decreasing size, ties by their lowest node.
func renumberBySize(labels []int32) *Partition {
	count := 0
	for _, c := range labels {
		count = max(count, int(c)+1)
	}
	size := make([]int, count)
	first := make([]int, count)
	for i := range first {
		first[i] = len(labels)
	}
	for i, c := range labels {
		size[c]++
		if first[c] == len(labels) {
			first[c] = i
		}
	}
	var used []int
	for c := 0; c < count; c++ {
		if size[c] > 0 {
			used = append(used, c)
		}
	}
	sort.SliceStable(used, func(a, b int) bool {
		if size[used[a]] != size[used[b]] {
			return size[used[a]] > size[used[b]]
		}
		return first[used[a]] < first[used[b]]
	})
	m := make([]int32, count)
	p := &Partition{Community: make([]int32, len(labels)), Sizes: make([]int, len(used))}
	for rank, c := range used {
		m[c] = int32(rank)
		p.Sizes[rank] = size[c]
	}
	for i, c := range labels {
		p.Community[i] = m[c]
	}
	return p
}
