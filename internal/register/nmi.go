package register

// Normalized mutual information (Studholme C, Hill DLG, Hawkes DJ. An overlap invariant entropy
// measure of 3D medical image alignment. Pattern Recognit 1999;32(1):71–86): (H(A) + H(B)) / H(A, B)
// of two images' intensities where both are image, from their joint histogram. It is 1 for
// unrelated images and 2 for one image against itself, whatever relates their intensities (a
// stain's brightness and another's darkness alike), and does not grow with the overlap as mutual
// information does.

import (
	"sync"

	"situcraft/internal/dmath"
)

// jointHistogram counts pairs of values (each in [0, 1]) in bins × bins.
type jointHistogram struct {
	bins   int
	counts []int64
}

func (j *jointHistogram) add(a, b float64) {
	ia, ib := int(mul(a, float64(j.bins))), int(mul(b, float64(j.bins)))
	ia, ib = min(max(ia, 0), j.bins-1), min(max(ib, 0), j.bins-1)
	j.counts[ia*j.bins+ib]++
}

// nmi is the histogram's normalized mutual information (1 when it is empty).
func (j *jointHistogram) nmi() float64 {
	var n int64
	for _, c := range j.counts {
		n += c
	}
	if n == 0 {
		return 1
	}
	total := float64(n)
	entropy := func(c int64) float64 {
		if c == 0 {
			return 0
		}
		p := float64(c) / total
		return -mul(p, dmath.Log(p))
	}
	var ha, hb, hab float64
	for a := 0; a < j.bins; a++ {
		var row int64
		for b := 0; b < j.bins; b++ {
			c := j.counts[a*j.bins+b]
			row += c
			hab += entropy(c)
		}
		ha += entropy(row)
	}
	for b := 0; b < j.bins; b++ {
		var col int64
		for a := 0; a < j.bins; a++ {
			col += j.counts[a*j.bins+b]
		}
		hb += entropy(col)
	}
	if hab == 0 {
		return 1
	}
	return (ha + hb) / hab
}

// NMIUnder is the normalized mutual information of fixed and moving (both in [0, 1]) where moving
// is sampled at toMoving of fixed's pixel centres, every stride-th pixel each way, and the share of
// fixed's image those pixels cover. Counts are integers, so the answer does not depend on how the
// rows are shared among cores.
func NMIUnder(fixed, moving Plane, toMoving Affine, bins, stride int) (nmi, overlap float64) {
	stride = max(1, stride)
	rows := (fixed.H + stride - 1) / stride
	var mu sync.Mutex
	total := &jointHistogram{bins: bins, counts: make([]int64, bins*bins)}
	var inside, image int64
	parallelFor(rows, func(r int) {
		j := r * stride
		local := &jointHistogram{bins: bins, counts: make([]int64, bins*bins)}
		var in, im int64
		for i := 0; i < fixed.W; i += stride {
			k := j*fixed.W + i
			if !fixed.valid(k) {
				continue
			}
			im++
			x, y := toMoving.Apply(float64(i)+0.5, float64(j)+0.5)
			v, ok := moving.Sample(x, y)
			if !ok {
				continue
			}
			in++
			local.add(float64(fixed.Pix[k]), v)
		}
		mu.Lock()
		for b, c := range local.counts {
			total.counts[b] += c
		}
		inside += in
		image += im
		mu.Unlock()
	})
	if image > 0 {
		overlap = float64(inside) / float64(image)
	}
	return total.nmi(), overlap
}
