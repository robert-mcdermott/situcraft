package expand

// The exact Euclidean distance transform of a label image, with each pixel's nearest labelled
// pixel: Felzenszwalb PF, Huttenlocher DP. Distance Transforms of Sampled Functions. Theory of
// Computing 2012;8:415–428. doi:10.4086/toc.2012.v008a019. The 2-D transform is two 1-D passes:
// down each column, each pixel's nearest labelled pixel in its column; then along each row, the
// lower envelope of the parabolas those give, which is the nearest labelled pixel in the image.
// Distances are between pixel centers, in pixels, and exact (integer squared distances).
//
// Ties (two labelled pixels at the same distance) go to the one met first: in a column the upper
// one, along a row the left one (SituCraft's choice; the algorithm leaves it open). Columns and
// rows are independent, so they are split among workers with the same result for any number.

import (
	"math"
	"sync"
)

const none = math.MaxInt32

// nearest answers, for each pixel of a w×h image (row-major), the index of its nearest labelled
// pixel (labels[i] != 0) and the squared distance to it; -1 and MaxInt64 where there is none.
func nearest(labels []uint32, w, h, workers int) (site []int32, dist2 []int64) {
	n := w * h
	// Pass 1: down each column, the row of the nearest labelled pixel in the column.
	column := make([]int32, n) // the row of the column's nearest labelled pixel, or none
	parallel(w, workers, func(x0, x1 int) {
		for x := x0; x < x1; x++ {
			last := int32(none)
			for y := 0; y < h; y++ {
				if labels[y*w+x] != 0 {
					last = int32(y)
				}
				column[y*w+x] = last
			}
			last = none
			for y := h - 1; y >= 0; y-- {
				if labels[y*w+x] != 0 {
					last = int32(y)
				}
				up := column[y*w+x]
				// The nearer of the labelled pixels above and below; the upper on a tie.
				if last != none && (up == none || int(last)-y < y-int(up)) {
					column[y*w+x] = last
				}
			}
		}
	})
	// Pass 2: along each row, the lower envelope of the parabolas (x-q)² + f(q), f(q) the squared
	// vertical distance from (q, y) to its column's nearest labelled pixel.
	site = make([]int32, n)
	dist2 = make([]int64, n)
	parallel(h, workers, func(y0, y1 int) {
		v := make([]int, w)       // the parabolas of the envelope, by their column
		z := make([]float64, w+1) // where each takes over
		f := make([]int64, w)
		for y := y0; y < y1; y++ {
			row := y * w
			k := -1
			for q := 0; q < w; q++ {
				r := column[row+q]
				if r == none {
					continue
				}
				d := int64(int(r) - y)
				f[q] = d * d
				if k < 0 {
					k = 0
					v[0], z[0], z[1] = q, math.Inf(-1), math.Inf(1)
					continue
				}
				// Where parabola q meets the envelope's last; parabolas it meets before they take
				// over are hidden (z[0] is -∞, so the first is never popped).
				s := intersect(q, v[k], f)
				for s <= z[k] {
					k--
					s = intersect(q, v[k], f)
				}
				k++
				v[k], z[k], z[k+1] = q, s, math.Inf(1)
			}
			if k < 0 {
				for x := 0; x < w; x++ {
					site[row+x], dist2[row+x] = -1, math.MaxInt64
				}
				continue
			}
			// Each pixel's parabola; at a boundary the earlier (left) one.
			j := 0
			for x := 0; x < w; x++ {
				for z[j+1] < float64(x) {
					j++
				}
				q := v[j]
				dx := int64(x - q)
				site[row+x] = int32(int(column[row+q])*w + q)
				dist2[row+x] = dx*dx + f[q]
			}
		}
	})
	return site, dist2
}

// intersect is where the parabolas of columns q and p (p < q) meet.
func intersect(q, p int, f []int64) float64 {
	return (float64(f[q]+int64(q)*int64(q)) - float64(f[p]+int64(p)*int64(p))) / float64(2*(q-p))
}

// parallel splits [0, n) into contiguous parts, one per worker.
func parallel(n, workers int, do func(a, b int)) {
	workers = max(1, min(workers, n))
	var wg sync.WaitGroup
	for k := 0; k < workers; k++ {
		a, b := n*k/workers, n*(k+1)/workers
		wg.Add(1)
		go func() {
			defer wg.Done()
			do(a, b)
		}()
	}
	wg.Wait()
}

// SquaredDistances are, for each pixel of a w×h label image (row-major), the squared distance in
// pixels from its centre to the nearest labelled pixel's (0 on one; MaxInt64 where there is
// none): exact, and the same for any number of workers.
func SquaredDistances(labels []uint32, w, h, workers int) []int64 {
	_, d := nearest(labels, w, h, workers)
	return d
}
