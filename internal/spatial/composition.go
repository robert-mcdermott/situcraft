package spatial

// Regions compared: each region's labeled cells by label, with two denominators (N6): the area of
// the tissue within the region (densities, cells per mm²; areas.go) and the region's labeled cells
// (shares). The tissue in none of the regions is a row of its own, so that a region is compared
// with the rest of the section. Each density has Garwood's exact interval and each share Wilson's;
// two rows' densities of a label are compared as a ratio with its exact conditional interval, and
// their shares as a ratio with Koopman's score interval (internal/stats). The intervals treat
// cells as placed independently: cells cluster, so they understate the uncertainty, and the
// regions of one section are not samples of anything; they describe this section. Regions may
// overlap: a cell in two is counted in both, and the two rows are then not independent.

import (
	"context"
	"errors"
	"math"

	"situcraft/internal/stats"
)

// RegionsParams are the intervals' level.
type RegionsParams struct {
	Level float64 `json:"level"`
}

// DefaultRegions are 95% intervals.
func DefaultRegions() RegionsParams { return RegionsParams{Level: 0.95} }

// Check says what is wrong with the parameters.
func (p *RegionsParams) Check() error {
	if !(p.Level >= 0.5 && p.Level <= 0.999) {
		return errors.New("the intervals' level is 0.5 to 0.999")
	}
	return nil
}

// MaxRegions is the most regions compared at once.
const MaxRegions = 20

// OutsideTissue marks a cell outside the tissue window in Composition.Membership.
const OutsideTissue = int32(1) << 30

// Composition is regions compared: rows the regions, then the tissue in none, R of them; by row
// and label [r·K + k]; ratios by ordered pair of rows and label [(r·R + s)·K + k], row r over s.
type Composition struct {
	K, R int
	Area []float64 // µm² of tissue
	// Labeled are each row's labeled cells; All, its cells analyzed.
	Labeled, All                  []int
	Count                         []int
	Density, DensityLo, DensityHi []float64 // per mm²
	Share, ShareLo, ShareHi       []float64
	DensityRatio, DensityRatioLo  []float64
	DensityRatioHi                []float64
	ShareRatio, ShareRatioLo      []float64
	ShareRatioHi                  []float64
	Outside                       int     // cells outside the tissue window, left out
	Membership                    []int32 // each cell's regions, bit k for the kth; OutsideTissue
}

// Compose compares regions for labeled cells (x, y: centroids, µm; label 0 to K − 1, or −1) in the
// tissue window.
func Compose(ctx context.Context, x, y []float32, label []int32, K int, tissue *Window, regions []*Window, p RegionsParams, workers int) (*Composition, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(x) != len(y) || len(label) != len(x) {
		return nil, errors.New("spatial: the cells' centroids and labels differ in length")
	}
	if len(regions) < 1 || len(regions) > MaxRegions {
		return nil, errors.New("spatial: regions compared are 1 to 20")
	}
	if K < 1 || K > 1000 {
		return nil, errors.New("spatial: 1 to 1,000 labels")
	}
	areas, err := RegionAreas(ctx, tissue, regions, workers)
	if err != nil {
		return nil, err
	}
	R := len(regions) + 1
	c := &Composition{K: K, R: R, Area: areas, Labeled: make([]int, R), All: make([]int, R), Count: make([]int, R*K), Membership: make([]int32, len(x))}
	t := tissue.Copy()
	rs := make([]*Window, len(regions))
	for k, r := range regions {
		rs[k] = r.Copy()
	}
	for i := range x {
		if label[i] < -1 || label[i] >= int32(K) {
			return nil, errors.New("spatial: a label beyond the label set")
		}
		px, py := float64(x[i]), float64(y[i])
		if !t.Contains(px, py) {
			c.Membership[i] = OutsideTissue
			if label[i] >= 0 {
				c.Outside++
			}
			continue
		}
		var mask int32
		for k, r := range rs {
			if r.Contains(px, py) {
				mask |= 1 << k
			}
		}
		c.Membership[i] = mask
		for r := 0; r < R; r++ {
			if r < R-1 && mask&(1<<r) == 0 || r == R-1 && mask != 0 {
				continue
			}
			c.All[r]++
			if label[i] >= 0 {
				c.Labeled[r]++
				c.Count[r*K+int(label[i])]++
			}
		}
	}
	nan := math.NaN()
	n := R * K
	c.Density, c.DensityLo, c.DensityHi = make([]float64, n), make([]float64, n), make([]float64, n)
	c.Share, c.ShareLo, c.ShareHi = make([]float64, n), make([]float64, n), make([]float64, n)
	for r := 0; r < R; r++ {
		mm2 := c.Area[r] / 1e6
		for k := 0; k < K; k++ {
			q := r*K + k
			c.Density[q], c.DensityLo[q], c.DensityHi[q] = nan, nan, nan
			if mm2 > 0 {
				lo, hi := stats.PoissonInterval(c.Count[q], p.Level)
				c.Density[q], c.DensityLo[q], c.DensityHi[q] = float64(c.Count[q])/mm2, lo/mm2, hi/mm2
			}
			c.Share[q], c.ShareLo[q], c.ShareHi[q] = nan, nan, nan
			if c.Labeled[r] > 0 {
				lo, hi := stats.WilsonInterval(c.Count[q], c.Labeled[r], p.Level)
				c.Share[q], c.ShareLo[q], c.ShareHi[q] = float64(c.Count[q])/float64(c.Labeled[r]), lo, hi
			}
		}
	}
	m := R * R * K
	c.DensityRatio, c.DensityRatioLo, c.DensityRatioHi = make([]float64, m), make([]float64, m), make([]float64, m)
	c.ShareRatio, c.ShareRatioLo, c.ShareRatioHi = make([]float64, m), make([]float64, m), make([]float64, m)
	for r := 0; r < R; r++ {
		for s := 0; s < R; s++ {
			for k := 0; k < K; k++ {
				q := (r*R+s)*K + k
				c.DensityRatio[q], c.DensityRatioLo[q], c.DensityRatioHi[q] = nan, nan, nan
				c.ShareRatio[q], c.ShareRatioLo[q], c.ShareRatioHi[q] = nan, nan, nan
				if r == s {
					continue
				}
				if c.Area[r] > 0 && c.Area[s] > 0 {
					c.DensityRatio[q], c.DensityRatioLo[q], c.DensityRatioHi[q] = stats.RateRatio(c.Count[r*K+k], c.Area[r], c.Count[s*K+k], c.Area[s], p.Level)
				}
				if c.Labeled[r] > 0 && c.Labeled[s] > 0 {
					c.ShareRatio[q], c.ShareRatioLo[q], c.ShareRatioHi[q] = stats.ProportionRatio(c.Count[r*K+k], c.Labeled[r], c.Count[s*K+k], c.Labeled[s], p.Level)
				}
			}
		}
	}
	return c, nil
}
