package spatial

// Across a region's edge: infiltration and exclusion (N4). Each cell's signed distance to the
// region's boundary (inside negative), and each label's density (cells per mm² of tissue) and
// share in bands of it from the reach inside to the reach outside, each band's own area in the
// tissue its denominator (areas.go), with the tissue deeper and farther as bands of their own: a
// profile of the label across the edge. Three zones, as the core and the invasive margin of a
// tumor are measured (Galon J, Costes A, Sanchez-Cabo F, et al. Type, density, and location of
// immune cells within human colorectal tumors predict clinical outcome. Science
// 2006;313:1960–1964): the core, inside deeper than the margin; the margin, within it of the edge
// on either side; and around, outside beyond the margin up to the reach. Each label's densities in
// them are compared as ratios with exact conditional intervals (internal/stats): the core over
// around (a label denser inside fills the region; sparser, it stays out) and the margin over
// both (denser than both, it gathers at the margin). The intervals treat cells as placed
// independently, so they understate the uncertainty of clustered cells: they describe this
// section.

import (
	"context"
	"errors"
	"math"
	"sort"

	"situcraft/internal/stats"
)

// BoundaryParams are the bands' reach and step, the margin's half-width (µm) and the intervals'
// level.
type BoundaryParams struct {
	Reach  float64 `json:"reach"`
	Step   float64 `json:"step"`
	Margin float64 `json:"margin"`
	Level  float64 `json:"level"`
}

// DefaultBoundary are bands of 10 µm to 100 µm either side and a margin of 20 µm.
func DefaultBoundary() BoundaryParams {
	return BoundaryParams{Reach: 100, Step: 10, Margin: 20, Level: 0.95}
}

// Check says what is wrong with the parameters.
func (p *BoundaryParams) Check() error {
	switch {
	case !(p.Reach > 0 && p.Reach <= 1000):
		return errors.New("the bands reach more than 0 and at most 1,000 µm")
	case !(p.Step > 0) || p.Reach/p.Step > 100 || math.Abs(p.Reach/p.Step-math.Round(p.Reach/p.Step)) > 1e-9:
		return errors.New("the bands' step divides the reach into at most 100")
	case !(p.Margin > 0 && p.Margin < p.Reach):
		return errors.New("the margin is more than 0 and less than the reach")
	case !(p.Level >= 0.5 && p.Level <= 0.999):
		return errors.New("the intervals' level is 0.5 to 0.999")
	}
	return nil
}

// The zones.
const (
	ZoneCore = iota
	ZoneMargin
	ZoneAround
	Zones
)

// The comparisons of zones' densities, by label [k·Comparisons + c].
const (
	CoreOverAround = iota
	MarginOverCore
	MarginOverAround
	Comparisons
)

// Boundary is the profile across a region's edge: by band [b·K + k] at the edges Edges (bands
// below the first, between each pair, from the last on); by zone [z·K + k].
type Boundary struct {
	K                             int
	Edges                         []float64
	Area                          []float64 // µm² of tissue
	Labeled, All                  []int
	Count                         []int
	Density, DensityLo, DensityHi []float64 // per mm²
	Share, ShareLo, ShareHi       []float64
	ZoneArea                      []float64
	ZoneLabeled                   []int
	ZoneCount                     []int
	ZoneDensity, ZoneLo, ZoneHi   []float64
	Ratio, RatioLo, RatioHi       []float64
	// Inside: +1 a label denser in the core than around (its interval above 1), −1 sparser, 0 not
	// shown either; AtMargin, denser at the margin than in both.
	Inside   []int
	AtMargin []bool
	Outside  int     // labeled cells outside the tissue window, left out
	Band     []int32 // each cell's band + 1; 0 outside the tissue
}

// AcrossEdge measures labeled cells (x, y: centroids, µm; label 0 to K − 1, or −1) across a
// region's edge in the tissue window.
func AcrossEdge(ctx context.Context, x, y []float32, label []int32, K int, tissue, region *Window, p BoundaryParams, workers int) (*Boundary, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(x) != len(y) || len(label) != len(x) {
		return nil, errors.New("spatial: the cells' centroids and labels differ in length")
	}
	if K < 1 || K > 1000 {
		return nil, errors.New("spatial: 1 to 1,000 labels")
	}
	steps := int(math.Round(p.Reach / p.Step))
	var edges []float64
	for k := -steps; k <= steps; k++ {
		edges = append(edges, math.Round(float64(k)*p.Step*1e9)/1e9)
	}
	// The zones' edges among the bands': areas found once over all of them.
	all := append([]float64{}, edges...)
	for _, e := range []float64{-p.Margin, p.Margin} {
		if i := sort.SearchFloat64s(all, e); i == len(all) || all[i] != e {
			all = append(all, e)
		}
	}
	sort.Float64s(all)
	fine, err := BandAreas(ctx, tissue, region, all, workers)
	if err != nil {
		return nil, err
	}
	zoneOf := func(s float64) int {
		switch {
		case s < -p.Margin:
			return ZoneCore
		case s < p.Margin:
			return ZoneMargin
		case s < p.Reach:
			return ZoneAround
		}
		return -1
	}
	B := len(edges) + 1
	b := &Boundary{K: K, Edges: edges, Area: make([]float64, B), Labeled: make([]int, B), All: make([]int, B), Count: make([]int, B*K),
		ZoneArea: make([]float64, Zones), ZoneLabeled: make([]int, Zones), ZoneCount: make([]int, Zones*K), Band: make([]int32, len(x))}
	for j, a := range fine {
		// A point within the fine band j.
		var s float64
		switch {
		case j == 0:
			s = all[0] - 1
		case j == len(all):
			s = all[len(all)-1] + 1
		default:
			s = (all[j-1] + all[j]) / 2
		}
		b.Area[bandOf(edges, s)] += a
		if z := zoneOf(s); z >= 0 {
			b.ZoneArea[z] += a
		}
	}
	t := tissue.Copy()
	r := region.Copy()
	limit := p.Reach + 1
	for i := range x {
		if label[i] < -1 || label[i] >= int32(K) {
			return nil, errors.New("spatial: a label beyond the label set")
		}
		px, py := float64(x[i]), float64(y[i])
		if !t.Contains(px, py) {
			if label[i] >= 0 {
				b.Outside++
			}
			continue
		}
		s := signed(r, px, py, limit)
		k := bandOf(edges, s)
		b.Band[i] = int32(k + 1)
		b.All[k]++
		if label[i] < 0 {
			continue
		}
		b.Labeled[k]++
		b.Count[k*K+int(label[i])]++
		if z := zoneOf(s); z >= 0 {
			b.ZoneLabeled[z]++
			b.ZoneCount[z*K+int(label[i])]++
		}
	}
	nan := math.NaN()
	density := func(count int, area float64) (float64, float64, float64) {
		if !(area > 0) {
			return nan, nan, nan
		}
		lo, hi := stats.PoissonInterval(count, p.Level)
		mm2 := area / 1e6
		return float64(count) / mm2, lo / mm2, hi / mm2
	}
	n := B * K
	b.Density, b.DensityLo, b.DensityHi = make([]float64, n), make([]float64, n), make([]float64, n)
	b.Share, b.ShareLo, b.ShareHi = make([]float64, n), make([]float64, n), make([]float64, n)
	for band := 0; band < B; band++ {
		for k := 0; k < K; k++ {
			q := band*K + k
			b.Density[q], b.DensityLo[q], b.DensityHi[q] = density(b.Count[q], b.Area[band])
			b.Share[q], b.ShareLo[q], b.ShareHi[q] = nan, nan, nan
			if b.Labeled[band] > 0 {
				lo, hi := stats.WilsonInterval(b.Count[q], b.Labeled[band], p.Level)
				b.Share[q], b.ShareLo[q], b.ShareHi[q] = float64(b.Count[q])/float64(b.Labeled[band]), lo, hi
			}
		}
	}
	b.ZoneDensity, b.ZoneLo, b.ZoneHi = make([]float64, Zones*K), make([]float64, Zones*K), make([]float64, Zones*K)
	for z := 0; z < Zones; z++ {
		for k := 0; k < K; k++ {
			q := z*K + k
			b.ZoneDensity[q], b.ZoneLo[q], b.ZoneHi[q] = density(b.ZoneCount[q], b.ZoneArea[z])
		}
	}
	b.Ratio, b.RatioLo, b.RatioHi = make([]float64, K*Comparisons), make([]float64, K*Comparisons), make([]float64, K*Comparisons)
	b.Inside, b.AtMargin = make([]int, K), make([]bool, K)
	pairs := [Comparisons][2]int{CoreOverAround: {ZoneCore, ZoneAround}, MarginOverCore: {ZoneMargin, ZoneCore}, MarginOverAround: {ZoneMargin, ZoneAround}}
	for k := 0; k < K; k++ {
		for c, zs := range pairs {
			q := k*Comparisons + c
			b.Ratio[q], b.RatioLo[q], b.RatioHi[q] = nan, nan, nan
			if b.ZoneArea[zs[0]] > 0 && b.ZoneArea[zs[1]] > 0 {
				b.Ratio[q], b.RatioLo[q], b.RatioHi[q] = stats.RateRatio(b.ZoneCount[zs[0]*K+k], b.ZoneArea[zs[0]], b.ZoneCount[zs[1]*K+k], b.ZoneArea[zs[1]], p.Level)
			}
		}
		q := k * Comparisons
		switch {
		case b.RatioLo[q+CoreOverAround] > 1:
			b.Inside[k] = 1
		case b.RatioHi[q+CoreOverAround] < 1:
			b.Inside[k] = -1
		}
		b.AtMargin[k] = b.RatioLo[q+MarginOverCore] > 1 && b.RatioLo[q+MarginOverAround] > 1
	}
	return b, nil
}
