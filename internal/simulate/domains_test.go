package simulate

import (
	"math"
	"testing"

	"situcraft/internal/random"
)

// The domains' regions tile the field, and each cell the layout places lies in the region of the
// domain that chose its type.
func TestDomainsTileTheFieldAndHoldTheirCells(t *testing.T) {
	p := Presets["small"]
	domains, err := Domains(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != p.Domains {
		t.Fatalf("%d domains, not %d", len(domains), p.Domains)
	}
	total := 0.0
	for _, d := range domains {
		a := 0.0
		for i, q := range d.Polygon {
			r := d.Polygon[(i+1)%len(d.Polygon)]
			a += q[0]*r[1] - r[0]*q[1]
		}
		total += math.Abs(a) / 2
	}
	if math.Abs(total-p.WidthUm*p.HeightUm) > 1e-6 {
		t.Errorf("the regions cover %g µm² of %g", total, p.WidthUm*p.HeightUm)
	}
	inside := func(poly [][2]float64, x, y float64) bool {
		in := false
		for i, q := range poly {
			r := poly[(i+1)%len(poly)]
			if (q[1] <= y) != (r[1] <= y) && q[0]+(y-q[1])/(r[1]-q[1])*(r[0]-q[0]) > x {
				in = !in
			}
		}
		return in
	}
	tis := newTissue(p)
	layout := random.New(p.seed(streamLayout), streamLayout)
	tis.makeDomains(layout)
	tis.placeCells(layout)
	if len(tis.cells) < 100 {
		t.Fatalf("%d cells", len(tis.cells))
	}
	for i, c := range tis.cells {
		d := tis.domainAt(c.x, c.y)
		k := 0
		for k = range tis.domains {
			if &tis.domains[k] == d {
				break
			}
		}
		if !inside(domains[k].Polygon, c.x, c.y) {
			t.Fatalf("cell %d at (%.2f, %.2f) of domain %d lies outside its region", i, c.x, c.y, k)
		}
		if domains[k].Mix[c.typ] == 0 {
			t.Fatalf("cell %d's type has no share in its domain", i)
		}
	}
}
