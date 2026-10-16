package label

// Reviewing labels: which cells to look at first. A cell's label is spatially implausible when few
// of its nearest labeled cells in the tissue carry it (cells of a kind sit with cells of their kind
// more often than not, so a lone label among others is often a mislabel, a doublet, or a cell
// whose outline holds parts of two); segmentation-sensitive when its counterparts under another
// segmentation carry another label (internal/compare, impact.go); a label of low confidence led
// its next by little. The queue ranks the cells any of these flags.

import (
	"context"
	"math"
	"sort"

	"situcraft/internal/graph"
)

// Agreement answers, for each labeled cell (label ≥ 0), the share of its k nearest labeled cells
// (by centroid, exactly; equal distances by index) carrying its label, and the label most of them
// carry (equal counts: the lower index). Cells without a label: NaN and −1. xy holds each cell's
// centroid (x, y interleaved).
func Agreement(ctx context.Context, xy []float64, label []int32, k, workers int) ([]float32, []int32, error) {
	n := len(label)
	share := make([]float32, n)
	dominant := make([]int32, n)
	var cells []int
	for i := range label {
		share[i] = float32(math.NaN())
		dominant[i] = -1
		if label[i] >= 0 {
			cells = append(cells, i)
		}
	}
	if len(cells) < 2 {
		return share, dominant, nil
	}
	k = min(k, len(cells)-1)
	pts := make([]float64, 2*len(cells))
	for j, i := range cells {
		pts[2*j], pts[2*j+1] = xy[2*i], xy[2*i+1]
	}
	nb, err := graph.KNN2D(ctx, pts, len(cells), k, workers)
	if err != nil {
		return nil, nil, err
	}
	count := map[int32]int{}
	for j, i := range cells {
		clear(count)
		same := 0
		for _, o := range nb.Index[j*nb.K : (j+1)*nb.K] {
			l := label[cells[o]]
			count[l]++
			if l == label[i] {
				same++
			}
		}
		share[i] = float32(same) / float32(nb.K)
		best, most := int32(-1), 0
		for l, c := range count {
			if c > most || c == most && l < best {
				best, most = l, c
			}
		}
		dominant[i] = best
	}
	return share, dominant, nil
}

// Reasons a cell is in the review queue.
const (
	ReasonImplausible   = "implausible"
	ReasonSensitive     = "segmentation-sensitive"
	ReasonLowConfidence = "low-confidence"
)

// QueueOptions say which cells to review: those whose label at most Implausible of their
// neighbors share (and most share another); those Other flags (what their counterparts carry under
// another segmentation, an index into Others, or −1; nil: none); and those whose confidence is
// below LowConfidence (NaN: none); at most Limit of them, and none of Reviewed.
type QueueOptions struct {
	Implausible   float64
	LowConfidence float64
	Other         []int32
	Others        []string
	Limit         int
	Reviewed      map[int]bool
}

// QueueItem is a cell to review: why, its neighbors' agreement and their label, what its
// counterparts carry under another segmentation, its confidence.
type QueueItem struct {
	Cell       int      `json:"cell"`
	Reasons    []string `json:"reasons"`
	Share      float32  `json:"share"`
	Dominant   int32    `json:"dominant"`
	Other      string   `json:"other,omitempty"`
	Confidence *float32 `json:"confidence"`
}

// QueueCounts count the cells each reason flags (the reviewed left out), those flagged for any
// (Flagged) and for two reasons or more (Both).
type QueueCounts struct {
	Labeled       int `json:"labeled"`
	Flagged       int `json:"flagged"`
	Implausible   int `json:"implausible"`
	Sensitive     int `json:"sensitive"`
	LowConfidence int `json:"lowConfidence"`
	Both          int `json:"both"`
	Reviewed      int `json:"reviewed"`
}

// Queue ranks the cells to review: those flagged for more reasons first; then the implausible, the
// fewest neighbors agreeing first; then the segmentation-sensitive; then the least confident;
// equal ones by index.
func Queue(label []int32, share []float32, dominant []int32, confidence []float32, o QueueOptions) ([]QueueItem, QueueCounts) {
	var items []QueueItem
	var c QueueCounts
	for i, l := range label {
		if l < 0 {
			continue
		}
		c.Labeled++
		if o.Reviewed[i] {
			c.Reviewed++
			continue
		}
		it := QueueItem{Cell: i, Share: share[i], Dominant: dominant[i]}
		if confidence != nil && !math.IsNaN(float64(confidence[i])) {
			v := confidence[i]
			it.Confidence = &v
		}
		// Compared as the shares are kept, in float32 (2/10 is float32(0.2)).
		if !math.IsNaN(float64(share[i])) && share[i] <= float32(o.Implausible) && dominant[i] != l {
			it.Reasons = append(it.Reasons, ReasonImplausible)
			c.Implausible++
		}
		if o.Other != nil && o.Other[i] >= 0 && int(o.Other[i]) < len(o.Others) {
			it.Reasons = append(it.Reasons, ReasonSensitive)
			it.Other = o.Others[o.Other[i]]
			c.Sensitive++
		}
		if it.Confidence != nil && *it.Confidence < float32(o.LowConfidence) {
			it.Reasons = append(it.Reasons, ReasonLowConfidence)
			c.LowConfidence++
		}
		if len(it.Reasons) >= 2 {
			c.Both++
		}
		if len(it.Reasons) > 0 {
			items = append(items, it)
			c.Flagged++
		}
	}
	key := func(it QueueItem) (int, float64, int, float64) {
		s := 2.0
		if len(it.Reasons) > 0 && it.Reasons[0] == ReasonImplausible {
			s = float64(it.Share)
		}
		sensitive := 1
		if it.Other != "" {
			sensitive = 0
		}
		conf := math.Inf(1)
		if it.Confidence != nil {
			conf = float64(*it.Confidence)
		}
		return -len(it.Reasons), s, sensitive, conf
	}
	sort.SliceStable(items, func(a, b int) bool {
		ra, sa, xa, ca := key(items[a])
		rb, sb, xb, cb := key(items[b])
		if ra != rb {
			return ra < rb
		}
		if sa != sb {
			return sa < sb
		}
		if xa != xb {
			return xa < xb
		}
		if ca != cb {
			return ca < cb
		}
		return items[a].Cell < items[b].Cell
	})
	if o.Limit > 0 && len(items) > o.Limit {
		items = items[:o.Limit]
	}
	return items, c
}
