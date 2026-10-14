// Package compare compares two segmentations of a dataset (internal/segment's files): which cells
// correspond, how each correspondence differs, and what changes, globally and region by region.
//
// Correspondence (a documented strategy, SituCraft's): two cells, one of each segmentation,
// correspond when they share a nucleus of the dataset (a nucleus whose pixels lie mostly in both),
// or when at least half of one lies within the other (Params.Within: a fragment split off a cell
// lies within it; a neighbour whose boundary moved does not); cells without area (an import's
// cells without outlines) correspond the same way by their transcripts. Each connected set of
// corresponding cells is one difference, classified by its shape:
//
//   - one cell in each: one-to-one, or expanded (B's area over 1.25 times A's, Params.AreaChange)
//     or contracted (under 1/1.25); assignment-only when either has no area;
//   - one cell of A to several of B: split; several of A to one of B: merged; several to several:
//     rearranged;
//   - a cell with no counterpart: unmatched (only in A, or only in B).
//
// Measures: per segmentation, its cells, nuclei, assigned fraction, transcripts per cell, areas and
// geometry; between them, the transcripts whose cell changed (gained, lost or moved), and on the
// one-to-one cells the intersection over union and the boundary displacement (the area between the
// two outlines over their mean perimeter); the spurious co-expression of Jones et al. (2025) and the
// mutually exclusive co-expression rate of Hartman and Satija (2024) (coexpression.go). Region by
// region (squares of Params.RegionUm), the same counts, for a map of where they differ.
package compare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"situcraft/internal/segment"
)

// Float is a measure that may be missing (NaN), written as null.
type Float float64

func (f Float) MarshalJSON() ([]byte, error) {
	if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
		return []byte("null"), nil
	}
	return json.Marshal(float64(f))
}

func (f *Float) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = Float(math.NaN())
		return nil
	}
	var v float64
	err := json.Unmarshal(b, &v)
	*f = Float(v)
	return err
}

// Classes of differences.
const (
	OneToOne       = "one-to-one"
	Expanded       = "expanded"
	Contracted     = "contracted"
	Split          = "split"
	Merged         = "merged"
	Rearranged     = "rearranged"
	OnlyA          = "only in A"
	OnlyB          = "only in B"
	AssignmentOnly = "assignment-only"
)

// Classes lists them in order.
var Classes = []string{OneToOne, Expanded, Contracted, Split, Merged, Rearranged, OnlyA, OnlyB, AssignmentOnly}

// matchedClass: the classes whose cells are the same cell, changed or not.
func matchedClass(c string) bool { return c == OneToOne || c == Expanded || c == Contracted }

// Seg is a segmentation as the comparison reads it.
type Seg struct {
	Cells    []segment.Cell
	Mask     *segment.Raster
	Assign   []uint32
	Counts   *segment.Counts
	Outlines []segment.Outline
	Desc     *segment.Description
}

// Load reads a segmentation's files.
func Load(dir string) (*Seg, error) {
	s := &Seg{}
	var err error
	if s.Desc, err = segment.ReadDescription(dir); err != nil {
		return nil, err
	}
	if s.Cells, err = segment.ReadCells(dir); err != nil {
		return nil, err
	}
	if s.Mask, err = segment.ReadMask(dir); err != nil {
		return nil, err
	}
	if s.Assign, err = segment.ReadAssignments(dir); err != nil {
		return nil, err
	}
	if s.Counts, err = segment.ReadCounts(dir); err != nil {
		return nil, err
	}
	if s.Outlines, err = segment.ReadOutlines(dir); err != nil {
		return nil, err
	}
	return s, nil
}

// Transcripts are the dataset's transcripts, in its order: positions (µm), and whether each is a
// gene transcript of quality 20 or more (the ones counted).
type Transcripts struct {
	X, Y    []float32
	Q20Gene []bool
}

// Params are the comparison's thresholds and choices, all recorded with it.
type Params struct {
	// Within is the share of a cell's pixels (or transcripts) that must lie within another for the
	// two to correspond.
	Within float64 `json:"within"`
	// AreaChange is the ratio of areas beyond which a one-to-one cell is expanded or contracted.
	AreaChange float64 `json:"area_change"`
	// RegionUm is the side of the regions, µm.
	RegionUm float64 `json:"region_um"`
	// Coexpression: the transcripts each cell is down-sampled to, the fold rise that makes a gene
	// pair spurious, the cells expressing a gene for its pairs to count, the seed.
	Depth    int     `json:"coexpression_depth"`
	Fold     float64 `json:"coexpression_fold"`
	MinCells int     `json:"coexpression_min_cells"`
	Seed     uint64  `json:"seed"`
}

// DefaultParams are SituCraft's choices.
func DefaultParams() Params {
	return Params{Within: 0.5, AreaChange: 1.25, RegionUm: 50, Depth: 50, Fold: 1.5, MinCells: 20, Seed: 20250}
}

// Inputs are what a comparison reads.
type Inputs struct {
	A, B        *Seg
	Nuclei      *segment.Raster // the dataset's nuclei (optional: shared nuclei then go unused)
	Transcripts *Transcripts
	// Reference is the dataset's co-expression reference (optional).
	Reference *Reference
	// Pairs are mutually exclusive gene pairs for the MECR (nil: SituCraft's lineage markers).
	Pairs [][2]string
	// Runtime (seconds) and size (bytes) of each, when known.
	RuntimeA, RuntimeB float64
	SizeA, SizeB       int64
	Params             Params
}

// SegMetrics are one segmentation's measures.
type SegMetrics struct {
	Cells              int     `json:"cells"`
	EmptyCells         int     `json:"emptyCells"`
	CellsWithNucleus   int     `json:"cellsWithNucleus"`
	CellsWithNuclei    int     `json:"cellsWithSeveralNuclei"`
	MultipieceCells    int     `json:"multipieceCells"`
	Q20GeneTranscripts int64   `json:"q20GeneTranscripts"`
	Q20GeneAssigned    int64   `json:"q20GeneAssigned"`
	AssignedFraction   float64 `json:"assignedFraction"`
	MedianTranscripts  Float   `json:"medianTranscriptsPerCell"`
	MedianArea         Float   `json:"medianAreaUm2"`
	// Coexpression is the median, over the dataset's spurious gene pairs, of their co-expression
	// relative to nuclear segmentation (1: as in nuclei alone; NaN when not measured).
	Coexpression      Float   `json:"spuriousCoexpression"`
	CoexpressionCells int     `json:"coexpressionCells"`
	MECR              Float   `json:"mecr"`
	RuntimeSeconds    float64 `json:"runtimeSeconds,omitempty"`
	SizeBytes         int64   `json:"sizeBytes,omitempty"`
}

// Group is one difference: the cells of A and of B that correspond (indices from 0).
type Group struct {
	Class string
	A, B  []int32
	// IoU and Jaccard (of the transcripts) for one cell in each; displacement in µm.
	IoU, Jaccard, Displacement float32
	// Changed counts the gene transcripts (quality 20+) assigned to the group's cells whose cell
	// changed; X, Y is where the group lies (µm).
	Changed uint32
	X, Y    float32
}

// Changes count transcripts whose cell changed.
type Changes struct {
	Transcripts int64   `json:"transcripts"`
	Changed     int64   `json:"changed"`
	Gained      int64   `json:"gained"`
	Lost        int64   `json:"lost"`
	Moved       int64   `json:"moved"`
	Fraction    float64 `json:"fraction"`
}

// Regions are the measures by region, row-major (Cols × Rows squares of SizeUm).
type Regions struct {
	SizeUm      float64   `json:"sizeUm"`
	Cols        int       `json:"cols"`
	Rows        int       `json:"rows"`
	CellsA      []int32   `json:"cellsA"`
	CellsB      []int32   `json:"cellsB"`
	Transcripts []int32   `json:"q20GeneTranscripts"`
	AssignedA   []int32   `json:"assignedA"`
	AssignedB   []int32   `json:"assignedB"`
	Changed     []int32   `json:"changed"`
	Pixels      []int64   `json:"pixels"`
	Disagree    []int64   `json:"disagreeingPixels"`
	MeanIoU     []float32 `json:"meanIoU"` // −1 where no one-to-one cells lie
}

// Result is a comparison.
type Result struct {
	Params  Params         `json:"params"`
	A, B    SegMetrics     `json:"-"`
	Classes map[string]int `json:"classes"`
	// CellsByClass counts each side's cells in each class.
	CellsA map[string]int `json:"cellsA"`
	CellsB map[string]int `json:"cellsB"`
	// All transcripts, and gene transcripts of quality 20 or more.
	Changes    Changes `json:"changes"`
	ChangesQ20 Changes `json:"changesQ20"`
	IoU        struct {
		Median Float `json:"median"`
		Mean   Float `json:"mean"`
		Pairs  int   `json:"pairs"`
	} `json:"iou"`
	Displacement struct {
		MedianUm Float `json:"medianUm"`
		MeanUm   Float `json:"meanUm"`
	} `json:"displacement"`
	PixelAgreement float64 `json:"pixelAgreement"`
	Coexpression   struct {
		Pairs        int `json:"pairs"`
		NuclearCells int `json:"nuclearCells"`
	} `json:"coexpression"`
	MECRPairs [][2]string `json:"mecrPairs"`
	Regions   Regions     `json:"regions"`
	Groups    []Group     `json:"-"`
}

// Compare compares two segmentations.
func Compare(ctx context.Context, in Inputs) (*Result, error) {
	a, b := in.A, in.B
	if a == nil || b == nil || in.Transcripts == nil {
		return nil, errors.New("a comparison needs two segmentations and the transcripts")
	}
	if a.Mask.Width != b.Mask.Width || a.Mask.Height != b.Mask.Height || math.Abs(a.Mask.PixelSize-b.Mask.PixelSize) > 1e-9 {
		return nil, fmt.Errorf("the segmentations' rasters differ (%d × %d at %g µm, %d × %d at %g µm)", a.Mask.Width, a.Mask.Height, a.Mask.PixelSize, b.Mask.Width, b.Mask.Height, b.Mask.PixelSize)
	}
	if len(a.Assign) != len(in.Transcripts.X) || len(b.Assign) != len(in.Transcripts.X) {
		return nil, fmt.Errorf("the segmentations assign %d and %d transcripts; the dataset has %d", len(a.Assign), len(b.Assign), len(in.Transcripts.X))
	}
	p := in.Params
	if p.Within == 0 {
		p = DefaultParams()
	}
	r := &Result{Params: p}
	nA, nB := len(a.Cells), len(b.Cells)
	w, h, ps := a.Mask.Width, a.Mask.Height, a.Mask.PixelSize

	// Pixels: each cell's area and perimeter, and the overlaps.
	areaA, areaB := make([]int64, nA+1), make([]int64, nB+1)
	perimA, perimB := perimeters(a.Mask, nA), perimeters(b.Mask, nB)
	inter := map[uint64]int64{}
	bad := false
	for y := 0; y < h; y++ {
		if y%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		merge(a.Mask.Rows[y], b.Mask.Rows[y], func(_, n int, la, lb uint32) {
			if int(la) > nA || int(lb) > nB {
				bad = true
				return
			}
			areaA[la] += int64(n)
			areaB[lb] += int64(n)
			if la != 0 && lb != 0 {
				inter[uint64(la)<<32|uint64(lb)] += int64(n)
			}
		})
	}
	if bad {
		return nil, errors.New("a raster labels cells that do not exist")
	}

	// Transcripts: each cell's, and the shared ones.
	txA, txB := make([]int64, nA+1), make([]int64, nB+1)
	txInter := map[uint64]int64{}
	for i := range a.Assign {
		la, lb := a.Assign[i], b.Assign[i]
		if int(la) > nA || int(lb) > nB {
			return nil, fmt.Errorf("transcript %d is assigned to a cell that does not exist", i)
		}
		txA[la]++
		txB[lb]++
		if la != 0 && lb != 0 {
			txInter[uint64(la)<<32|uint64(lb)]++
		}
	}

	// Nuclei: each nucleus's cell in each segmentation (the one holding most of its pixels).
	var ownA, ownB []uint32
	if in.Nuclei != nil && in.Nuclei.Width == w && in.Nuclei.Height == h {
		ownA, ownB = owners(in.Nuclei, a.Mask), owners(in.Nuclei, b.Mask)
	}

	// Correspondence: the connected sets.
	uf := newUnionFind(nA + nB)
	node := func(side int, label uint32) int {
		if side == 0 {
			return int(label) - 1
		}
		return nA + int(label) - 1
	}
	for k, v := range inter {
		la, lb := uint32(k>>32), uint32(k)
		if float64(v) >= p.Within*float64(areaA[la]) || float64(v) >= p.Within*float64(areaB[lb]) {
			uf.union(node(0, la), node(1, lb))
		}
	}
	for n := range ownA {
		if ownA[n] != 0 && ownB[n] != 0 {
			uf.union(node(0, ownA[n]), node(1, ownB[n]))
		}
	}
	for k, v := range txInter {
		la, lb := uint32(k>>32), uint32(k)
		if areaA[la] > 0 && areaB[lb] > 0 {
			continue // cells with area correspond by their pixels and nuclei
		}
		if float64(v) >= p.Within*float64(txA[la]) || float64(v) >= p.Within*float64(txB[lb]) {
			uf.union(node(0, la), node(1, lb))
		}
	}
	members := map[int][]int{}
	for i := 0; i < nA+nB; i++ {
		root := uf.find(i)
		members[root] = append(members[root], i)
	}
	roots := make([]int, 0, len(members))
	for root := range members {
		roots = append(roots, root)
	}
	// In order of their first cell (A's before B's).
	sort.Slice(roots, func(i, j int) bool { return members[roots[i]][0] < members[roots[j]][0] })
	groupA, groupB := make([]int32, nA+1), make([]int32, nB+1)
	for i := range groupA {
		groupA[i] = -1
	}
	for i := range groupB {
		groupB[i] = -1
	}
	r.Classes, r.CellsA, r.CellsB = map[string]int{}, map[string]int{}, map[string]int{}
	var ious, displacements []float64
	for gi, root := range roots {
		g := Group{}
		for _, m := range members[root] {
			if m < nA {
				g.A = append(g.A, int32(m))
				groupA[m+1] = int32(gi)
			} else {
				g.B = append(g.B, int32(m-nA))
				groupB[m-nA+1] = int32(gi)
			}
		}
		noArea := false
		for _, c := range g.A {
			noArea = noArea || areaA[c+1] == 0
		}
		for _, c := range g.B {
			noArea = noArea || areaB[c+1] == 0
		}
		switch {
		case len(g.B) == 0:
			g.Class = OnlyA
		case len(g.A) == 0:
			g.Class = OnlyB
		case noArea:
			g.Class = AssignmentOnly
		case len(g.A) == 1 && len(g.B) == 1:
			ca, cb := g.A[0]+1, g.B[0]+1
			ratio := float64(areaB[cb]) / float64(areaA[ca])
			switch {
			case ratio > p.AreaChange:
				g.Class = Expanded
			case ratio < 1/p.AreaChange:
				g.Class = Contracted
			default:
				g.Class = OneToOne
			}
		case len(g.A) == 1:
			g.Class = Split
		case len(g.B) == 1:
			g.Class = Merged
		default:
			g.Class = Rearranged
		}
		if len(g.A) == 1 && len(g.B) == 1 {
			ca, cb := uint32(g.A[0]+1), uint32(g.B[0]+1)
			k := uint64(ca)<<32 | uint64(cb)
			in := inter[k]
			if union := areaA[ca] + areaB[cb] - in; union > 0 {
				g.IoU = float32(float64(in) / float64(union))
			}
			if tu := txA[ca] + txB[cb] - txInter[k]; tu > 0 {
				g.Jaccard = float32(float64(txInter[k]) / float64(tu))
			}
			if matchedClass(g.Class) {
				if per := (perimA[ca] + perimB[cb]) / 2; per > 0 {
					g.Displacement = float32(float64(areaA[ca]+areaB[cb]-2*in) / float64(per) * ps)
				}
				ious = append(ious, float64(g.IoU))
				displacements = append(displacements, float64(g.Displacement))
			}
		}
		// Where it lies: the mean of its cells' centroids.
		var sx, sy, sn float64
		for _, c := range g.A {
			if cell := a.Cells[c]; cell.Area > 0 {
				sx, sy, sn = sx+float64(cell.X), sy+float64(cell.Y), sn+1
			}
		}
		for _, c := range g.B {
			if cell := b.Cells[c]; cell.Area > 0 {
				sx, sy, sn = sx+float64(cell.X), sy+float64(cell.Y), sn+1
			}
		}
		if sn > 0 {
			g.X, g.Y = float32(sx/sn), float32(sy/sn)
		}
		r.Classes[g.Class]++
		r.CellsA[g.Class] += len(g.A)
		r.CellsB[g.Class] += len(g.B)
		r.Groups = append(r.Groups, g)
	}
	r.IoU.Median, r.IoU.Mean, r.IoU.Pairs = Float(round6(median(ious))), Float(round6(mean(ious))), len(ious)
	r.Displacement.MedianUm, r.Displacement.MeanUm = Float(round6(median(displacements))), Float(round6(mean(displacements)))

	// The transcripts whose cell changed.
	same := func(la, lb uint32) bool {
		if la == 0 || lb == 0 {
			return la == lb
		}
		g := groupA[la]
		return g >= 0 && g == groupB[lb] && matchedClass(r.Groups[g].Class)
	}
	t := in.Transcripts
	regions := newRegions(p.RegionUm, float64(w)*ps, float64(h)*ps)
	for i := range a.Assign {
		la, lb := a.Assign[i], b.Assign[i]
		changed := !same(la, lb)
		count := func(c *Changes) {
			c.Transcripts++
			if !changed {
				return
			}
			c.Changed++
			switch {
			case la == 0:
				c.Gained++
			case lb == 0:
				c.Lost++
			default:
				c.Moved++
			}
		}
		count(&r.Changes)
		if !t.Q20Gene[i] {
			continue
		}
		count(&r.ChangesQ20)
		k := regions.at(float64(t.X[i]), float64(t.Y[i]))
		if k >= 0 {
			regions.Transcripts[k]++
			if la != 0 {
				regions.AssignedA[k]++
			}
			if lb != 0 {
				regions.AssignedB[k]++
			}
			if changed {
				regions.Changed[k]++
			}
		}
		if changed {
			if la != 0 {
				r.Groups[groupA[la]].Changed++
			}
			if lb != 0 && (la == 0 || groupB[lb] != groupA[la]) {
				r.Groups[groupB[lb]].Changed++
			}
		}
	}
	for _, c := range []*Changes{&r.Changes, &r.ChangesQ20} {
		if c.Transcripts > 0 {
			c.Fraction = round6(float64(c.Changed) / float64(c.Transcripts))
		}
	}

	// Pixels that agree (both outside cells, or in matched cells), region by region.
	var agree, union int64
	cols := func(x int) int { return int(float64(x) * ps / regions.SizeUm) }
	for y := 0; y < h; y++ {
		row := int(float64(y) * ps / regions.SizeUm)
		merge(a.Mask.Rows[y], b.Mask.Rows[y], func(x, n int, la, lb uint32) {
			if la == 0 && lb == 0 {
				return
			}
			ok := same(la, lb)
			union += int64(n)
			if ok {
				agree += int64(n)
			}
			// Split the stretch at the regions' edges.
			for x0 := x; x0 < x+n; {
				c := cols(x0)
				end := x + n
				if next := int(math.Ceil(float64(c+1) * regions.SizeUm / ps)); next < end {
					end = max(next, x0+1)
				}
				if k := regions.index(c, row); k >= 0 {
					regions.Pixels[k] += int64(end - x0)
					if !ok {
						regions.Disagree[k] += int64(end - x0)
					}
				}
				x0 = end
			}
		})
	}
	if union > 0 {
		r.PixelAgreement = round6(float64(agree) / float64(union))
	}
	iouSum := make([]float64, len(regions.MeanIoU))
	iouN := make([]float64, len(regions.MeanIoU))
	for _, c := range a.Cells {
		if c.Area > 0 {
			if k := regions.at(float64(c.X), float64(c.Y)); k >= 0 {
				regions.CellsA[k]++
			}
		}
	}
	for _, c := range b.Cells {
		if c.Area > 0 {
			if k := regions.at(float64(c.X), float64(c.Y)); k >= 0 {
				regions.CellsB[k]++
			}
		}
	}
	for _, g := range r.Groups {
		if len(g.A) == 1 && len(g.B) == 1 && matchedClass(g.Class) {
			if k := regions.at(float64(g.X), float64(g.Y)); k >= 0 {
				iouSum[k] += float64(g.IoU)
				iouN[k]++
			}
		}
	}
	for k := range regions.MeanIoU {
		regions.MeanIoU[k] = -1 // no one-to-one cells there
		if iouN[k] > 0 {
			regions.MeanIoU[k] = float32(iouSum[k] / iouN[k])
		}
	}
	r.Regions = *regions

	// Each segmentation's own measures.
	r.A = segMetrics(a, ownA, ps)
	r.B = segMetrics(b, ownB, ps)
	r.A.RuntimeSeconds, r.B.RuntimeSeconds, r.A.SizeBytes, r.B.SizeBytes = in.RuntimeA, in.RuntimeB, in.SizeA, in.SizeB

	// Co-expression.
	r.A.Coexpression, r.B.Coexpression = Float(math.NaN()), Float(math.NaN())
	if ref := in.Reference; ref != nil {
		r.Coexpression.Pairs, r.Coexpression.NuclearCells = len(ref.Pairs), ref.NuclearCells
		var score float64
		score, r.A.CoexpressionCells = ref.Score(a.Counts, p)
		r.A.Coexpression = Float(score)
		score, r.B.CoexpressionCells = ref.Score(b.Counts, p)
		r.B.Coexpression = Float(score)
	}
	pairs := in.Pairs
	if pairs == nil {
		pairs = LineagePairs(a.Counts.Genes)
	}
	r.MECRPairs = pairs
	r.A.MECR, r.B.MECR = Float(MECR(a.Counts, pairs)), Float(MECR(b.Counts, pairs))
	return r, nil
}

// segMetrics are a segmentation's own measures.
func segMetrics(s *Seg, owners []uint32, ps float64) SegMetrics {
	m := SegMetrics{Cells: len(s.Cells), Q20GeneTranscripts: s.Desc.Q20GeneTranscripts, Q20GeneAssigned: s.Desc.Q20GeneAssigned}
	if m.Q20GeneTranscripts > 0 {
		m.AssignedFraction = round6(float64(m.Q20GeneAssigned) / float64(m.Q20GeneTranscripts))
	}
	var transcripts, areas []float64
	for _, c := range s.Cells {
		transcripts = append(transcripts, float64(c.Transcripts))
		if c.Area > 0 {
			areas = append(areas, float64(c.Area))
		} else {
			m.EmptyCells++
		}
	}
	m.MedianTranscripts, m.MedianArea = Float(median(transcripts)), Float(round6(median(areas)))
	for _, o := range s.Outlines {
		if o.Pieces > 1 {
			m.MultipieceCells++
		}
	}
	if owners != nil {
		per := make([]int, len(s.Cells)+1)
		for _, o := range owners {
			per[o]++
		}
		for _, n := range per[1:] {
			if n > 0 {
				m.CellsWithNucleus++
			}
			if n > 1 {
				m.CellsWithNuclei++
			}
		}
	} else {
		m.CellsWithNucleus, m.CellsWithNuclei = -1, -1
	}
	return m
}

// merge calls fn for each stretch of a row along which both rows' labels are constant.
func merge(a, b []segment.Run, fn func(x, n int, la, lb uint32)) {
	if len(a) == 0 || len(b) == 0 {
		return
	}
	i, j := 0, 0
	leftA, leftB := int(a[0].Length), int(b[0].Length)
	x := 0
	for i < len(a) && j < len(b) {
		n := min(leftA, leftB)
		if n > 0 {
			fn(x, n, a[i].Label, b[j].Label)
		}
		x += n
		leftA -= n
		leftB -= n
		if leftA == 0 {
			if i++; i < len(a) {
				leftA = int(a[i].Length)
			}
		}
		if leftB == 0 {
			if j++; j < len(b) {
				leftB = int(b[j].Length)
			}
		}
	}
}

// perimeters are each label's perimeter, in pixel edges (edges with another label or the image's
// border).
func perimeters(r *segment.Raster, n int) []int64 {
	out := make([]int64, n+1)
	add := func(l uint32, k int64) {
		if l != 0 && int(l) <= n {
			out[l] += k
		}
	}
	for y, row := range r.Rows {
		for i, run := range row {
			if i == 0 {
				add(run.Label, 1)
			} else if row[i-1].Label != run.Label {
				add(run.Label, 1)
				add(row[i-1].Label, 1)
			}
			if i == len(row)-1 {
				add(run.Label, 1)
			}
			if y == 0 || y == len(r.Rows)-1 {
				add(run.Label, int64(run.Length))
			}
		}
		if y > 0 {
			merge(r.Rows[y-1], row, func(_, k int, up, down uint32) {
				if up != down {
					add(up, int64(k))
					add(down, int64(k))
				}
			})
		}
	}
	return out
}

// owners is, for each nucleus, the cell holding most of its pixels (0: none; the lower label of a
// tie).
func owners(nuclei, cells *segment.Raster) []uint32 {
	votes := map[uint64]int64{}
	top := uint32(0)
	for y := range nuclei.Rows {
		merge(nuclei.Rows[y], cells.Rows[y], func(_, n int, nl, cl uint32) {
			if nl != 0 {
				votes[uint64(nl)<<32|uint64(cl)] += int64(n)
				top = max(top, nl)
			}
		})
	}
	keys := make([]uint64, 0, len(votes))
	for k := range votes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	own := make([]uint32, top+1)
	best := make([]int64, top+1)
	for _, k := range keys {
		nl, cl := k>>32, uint32(k)
		if v := votes[k]; v > best[nl] {
			best[nl], own[nl] = v, cl
		}
	}
	return own[1:]
}

type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
	u := &unionFind{parent: make([]int, n)}
	for i := range u.parent {
		u.parent[i] = i
	}
	return u
}

func (u *unionFind) find(i int) int {
	for u.parent[i] != i {
		u.parent[i] = u.parent[u.parent[i]]
		i = u.parent[i]
	}
	return i
}

func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra < rb {
		u.parent[rb] = ra
	} else if rb < ra {
		u.parent[ra] = rb
	}
}

func newRegions(size, width, height float64) *Regions {
	g := &Regions{SizeUm: size, Cols: int(math.Ceil(width / size)), Rows: int(math.Ceil(height / size))}
	n := g.Cols * g.Rows
	g.CellsA, g.CellsB, g.Transcripts = make([]int32, n), make([]int32, n), make([]int32, n)
	g.AssignedA, g.AssignedB, g.Changed = make([]int32, n), make([]int32, n), make([]int32, n)
	g.Pixels, g.Disagree, g.MeanIoU = make([]int64, n), make([]int64, n), make([]float32, n)
	return g
}

func (g *Regions) index(c, r int) int {
	if c < 0 || r < 0 || c >= g.Cols || r >= g.Rows {
		return -1
	}
	return r*g.Cols + c
}

func (g *Regions) at(x, y float64) int {
	if math.IsNaN(x) || math.IsNaN(y) {
		return -1
	}
	return g.index(int(math.Floor(x/g.SizeUm)), int(math.Floor(y/g.SizeUm)))
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
