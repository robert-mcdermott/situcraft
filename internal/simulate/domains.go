package simulate

// The tissue's domains as the simulator lays them out, for checking what is measured of regions
// against them: each domain's seeded center, its mix of the cell types and the type dominating it,
// and its region, the Voronoi cell of its center within the field (every point nearer its center
// than any other's: the field clipped by the half-plane toward each other center). The domains are
// the layout stream's first draws, so they are found without laying out the cells.

import (
	"errors"

	"situcraft/internal/random"
)

// Domain is one domain: its center (µm), the types' shares in it, the type dominating it, and its
// region, a convex polygon (µm), counter-clockwise with y downward as the image's.
type Domain struct {
	X, Y     float64
	Mix      []float64
	Dominant int
	Polygon  [][2]float64
}

// Domains are a tissue's domains. A mosaic's tiles each have their own: refused.
func Domains(p Params) ([]Domain, error) {
	if across, down := p.tiles(); across*down > 1 {
		return nil, errors.New("simulate: a mosaic's domains are each tile's")
	}
	if err := p.Check(); err != nil {
		return nil, err
	}
	t := newTissue(p)
	t.makeDomains(random.New(p.seed(streamLayout), streamLayout))
	out := make([]Domain, len(t.domains))
	for d, dm := range t.domains {
		poly := [][2]float64{{0, 0}, {p.WidthUm, 0}, {p.WidthUm, p.HeightUm}, {0, p.HeightUm}}
		for e, other := range t.domains {
			if e == d {
				continue
			}
			// Nearer d's center than e's: (c_e − c_d)·q ≤ (|c_e|² − |c_d|²) / 2.
			ax, ay := other.x-dm.x, other.y-dm.y
			c := (mul(other.x, other.x) + mul(other.y, other.y) - mul(dm.x, dm.x) - mul(dm.y, dm.y)) / 2
			poly = clipHalfPlane(poly, ax, ay, c)
		}
		dominant := d % len(p.Types)
		out[d] = Domain{X: dm.x, Y: dm.y, Mix: append([]float64{}, dm.mix...), Dominant: dominant, Polygon: poly}
	}
	return out, nil
}

// clipHalfPlane is a convex polygon's part where a·q ≤ c (Sutherland and Hodgman's clipping, for
// one edge).
func clipHalfPlane(poly [][2]float64, ax, ay, c float64) [][2]float64 {
	var out [][2]float64
	for i, p := range poly {
		q := poly[(i+1)%len(poly)]
		vp, vq := mul(ax, p[0])+mul(ay, p[1])-c, mul(ax, q[0])+mul(ay, q[1])-c
		if vp <= 0 {
			out = append(out, p)
		}
		if (vp <= 0) != (vq <= 0) {
			f := vp / (vp - vq)
			out = append(out, [2]float64{p[0] + f*(q[0]-p[0]), p[1] + f*(q[1]-p[1])})
		}
	}
	return out
}
