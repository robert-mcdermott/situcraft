package cluster

import "sort"

// AdjustedRand is the adjusted Rand index of two partitions of the same items (Hubert L, Arabie P.
// Comparing partitions. J Classif 1985;2:193–218): 1 when they agree, about 0 when they agree as
// much as chance would. Labels are any non-negative integers.
func AdjustedRand(a, b []int32) float64 {
	if len(a) != len(b) || len(a) < 2 {
		return 0
	}
	pairs := func(n float64) float64 { return mul(n, n-1) / 2 }
	table := map[[2]int32]float64{}
	ra, rb := map[int32]float64{}, map[int32]float64{}
	for i := range a {
		table[[2]int32{a[i], b[i]}]++
		ra[a[i]]++
		rb[b[i]]++
	}
	// Sums over maps in sorted order: the result is the same on every run.
	sum := func(m map[int32]float64) float64 {
		keys := make([]int32, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sortInt32(keys)
		s := 0.0
		for _, k := range keys {
			s += pairs(m[k])
		}
		return s
	}
	cells := make([][2]int32, 0, len(table))
	for k := range table {
		cells = append(cells, k)
	}
	sortPairs(cells)
	index := 0.0
	for _, k := range cells {
		index += pairs(table[k])
	}
	sa, sb := sum(ra), sum(rb)
	expected := mul(sa, sb) / pairs(float64(len(a)))
	maximum := (sa + sb) / 2
	if maximum == expected {
		return 1
	}
	return (index - expected) / (maximum - expected)
}

func sortInt32(v []int32) { sort.Slice(v, func(i, j int) bool { return v[i] < v[j] }) }

func sortPairs(v [][2]int32) {
	sort.Slice(v, func(i, j int) bool { return v[i][0] < v[j][0] || v[i][0] == v[j][0] && v[i][1] < v[j][1] })
}
