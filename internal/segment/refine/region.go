package refine

// What a refinement covers (requirements S5): the whole tissue, its sections apart, or chosen
// regions.
//   - A region is a rectangle (µm). Its cells, those whose seed (their nucleus's centre) lies in
//     it, are refined; the cells about it are held where they are, in a margin around the region,
//     so that a refined cell near the edge keeps its whole extent and the region's edge never cuts
//     a cell. The rest of the tissue keeps the instrument's segmentation.
//   - Sections are the tissue's separate pieces: the cells, grouped where their boxes come within
//     a gap of each other (single linkage), each group's box grown by the margin, and groups whose
//     boxes then overlap merged. Each is refined apart, with a model of its own: unrelated tissues
//     on one slide do not share expression states.

import (
	"errors"
	"math"
	"sort"
)

// Region is a rectangle of the tissue, µm.
type Region struct {
	X0 float64 `json:"x0_um"`
	Y0 float64 `json:"y0_um"`
	X1 float64 `json:"x1_um"`
	Y1 float64 `json:"y1_um"`
}

// Empty: no area.
func (r Region) Empty() bool { return !(r.X1 > r.X0 && r.Y1 > r.Y0) }

// Contains reports whether a point is in the region (its lower edges included, its upper ones
// not).
func (r Region) Contains(x, y float64) bool { return x >= r.X0 && y >= r.Y0 && x < r.X1 && y < r.Y1 }

// Area is the region's area, µm².
func (r Region) Area() float64 {
	if r.Empty() {
		return 0
	}
	return mul(r.X1-r.X0, r.Y1-r.Y0)
}

func (r Region) grown(by float64) Region { return Region{r.X0 - by, r.Y0 - by, r.X1 + by, r.Y1 + by} }

func (r Region) clipped(to Region) Region {
	return Region{math.Max(r.X0, to.X0), math.Max(r.Y0, to.Y0), math.Min(r.X1, to.X1), math.Min(r.Y1, to.Y1)}
}

func (r Region) overlaps(o Region) bool {
	return r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1
}

func (r Region) union(o Region) Region {
	return Region{math.Min(r.X0, o.X0), math.Min(r.Y0, o.Y0), math.Max(r.X1, o.X1), math.Max(r.Y1, o.Y1)}
}

// Area modes.
const (
	AreaWhole    = "whole"
	AreaSections = "sections"
	AreaRegions  = "regions"
)

// Area says what a refinement covers.
type Area struct {
	Mode string `json:"mode"`
	// Regions are the chosen regions (mode regions).
	Regions []Region `json:"regions,omitempty"`
	// GapUm is the gap between cells that separates sections (mode sections).
	GapUm float64 `json:"section_gap_um,omitempty"`
}

// The margin about a region holding its neighbours (µm), and the default gap between sections.
const (
	marginUm     = 20.0
	sectionGapUm = 100.0
)

// whole is the inputs' whole extent.
func (in *Inputs) whole() Region {
	return Region{in.X0, in.Y0, in.X0 + in.WidthUm, in.Y0 + in.HeightUm}
}

// Regions are the rectangles an area asks for: the whole tissue, its sections, or the chosen
// regions (clipped to the tissue, overlapping ones merged), in order.
func (in *Inputs) Regions(a Area) ([]Region, error) {
	whole := in.whole()
	switch a.Mode {
	case "", AreaWhole:
		return []Region{whole}, nil
	case AreaSections:
		gap := a.GapUm
		if gap == 0 {
			gap = sectionGapUm
		}
		if !(gap > 0) {
			return nil, fail(ClassRegion, remedyRegion, "the gap between sections is positive")
		}
		return in.sections(gap), nil
	case AreaRegions:
		var out []Region
		for _, r := range a.Regions {
			c := r.clipped(whole)
			if c.Empty() {
				return nil, fail(ClassRegion, remedyRegion, "the region (%g, %g)–(%g, %g) µm lies outside the tissue", r.X0, r.Y0, r.X1, r.Y1)
			}
			out = append(out, c)
		}
		if len(out) == 0 {
			return nil, fail(ClassRegion, remedyRegion, "no region was chosen")
		}
		return mergeOverlapping(out, marginUm), nil
	}
	return nil, errors.New("refine: the area is whole, sections or regions")
}

// mergeOverlapping merges regions whose boxes, grown by the margin, overlap (so that no two
// refined regions share a margin), ordered by their corners.
func mergeOverlapping(rs []Region, margin float64) []Region {
	out := append([]Region(nil), rs...)
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(out) && !merged; i++ {
			for j := i + 1; j < len(out); j++ {
				if out[i].grown(margin).overlaps(out[j].grown(margin)) {
					out[i] = out[i].union(out[j])
					out = append(out[:j], out[j+1:]...)
					merged = true
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y0 != out[j].Y0 {
			return out[i].Y0 < out[j].Y0
		}
		return out[i].X0 < out[j].X0
	})
	return out
}

// sections groups the cells into the tissue's separate pieces.
func (in *Inputs) sections(gap float64) []Region {
	// Union-find over cells whose boxes come within the gap, found through a grid of the gap's
	// size.
	parent := make([]int, in.Cells)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	grid := map[[2]int][]int{}
	var cells []int
	for c, b := range in.Box {
		if b.Empty() {
			continue
		}
		cells = append(cells, c)
		g := b.grown(gap / 2)
		for gy := int(math.Floor(g.Y0 / gap)); gy <= int(math.Floor(g.Y1/gap)); gy++ {
			for gx := int(math.Floor(g.X0 / gap)); gx <= int(math.Floor(g.X1/gap)); gx++ {
				key := [2]int{gx, gy}
				for _, o := range grid[key] {
					if b.grown(gap / 2).overlaps(in.Box[o].grown(gap / 2)) {
						parent[find(o)] = find(c)
					}
				}
				grid[key] = append(grid[key], c)
			}
		}
	}
	boxes := map[int]Region{}
	for _, c := range cells {
		r := find(c)
		if b, ok := boxes[r]; ok {
			boxes[r] = b.union(in.Box[c])
		} else {
			boxes[r] = in.Box[c]
		}
	}
	var out []Region
	for _, b := range boxes {
		out = append(out, b.grown(marginUm).clipped(in.whole()))
	}
	if len(out) == 0 {
		return []Region{in.whole()}
	}
	return mergeOverlapping(out, marginUm)
}
