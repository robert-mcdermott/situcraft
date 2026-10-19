package simulate

// The tissue: domains, cells and nuclei, drawn on the morphology image's pixel grid.
//
// Domains are the Voronoi regions of seeded centres; each has a mix of the cell types, one type
// dominating it, and a direction its elongated cells tend to follow. Cells are placed by dart
// throwing: a candidate's domain chooses its type, the type its area (log-normal), elongation and
// outline; a candidate too near a placed cell (closer than three quarters of their summed radii)
// is refused, until the cells' areas fill the field to the fill asked for. Each cell is an
// ellipse of its area and elongation whose radius varies with direction (second and third
// harmonics), and owns the pixels whose centre lies within it where its scaled distance is the
// least of the cells' that reach them: neighbours meet where their outlines compete, and gaps
// remain where none reaches. A cell keeps its largest 4-connected piece; one of fewer than 8 µm²
// is dropped. Its nucleus is a smaller, rounder ellipse about its centre, within it; a share of
// cells have their nucleus outside the section, and so none seen. Each cell spans a range of the
// section's depth, its nucleus the middle of it.

import (
	"math"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// mul is a product rounded on its own, never fused with an addition (the same on every
// processor).
func mul(a, b float64) float64 { return float64(a * b) }

type domain struct {
	x, y   float64
	mix    []float64 // the types' shares, summing to 1
	dx, dy float64   // the direction elongated cells follow
}

// cell is one true cell.
type cell struct {
	typ          int
	domain       int     // the domain that chose its type
	x, y         float64 // centre, µm
	a, b         float64 // semi-axes, µm (a along u)
	ux, uy       float64 // the long axis
	h2c, h2s     float64 // second and third harmonics of the outline
	h3c, h3s     float64
	zLo, zHi     float64
	nucleus      bool
	na, nb       float64 // the nucleus's semi-axes, µm
	nx, ny       float64 // its centre
	nzLo, nzHi   float64
	pixels       []int32 // its pixels (row-major indices), and its nucleus's
	nucleusPixel []int32
}

type tissue struct {
	p       Params
	w, h    int
	domains []domain
	cells   []cell
	// labels holds each pixel's cell (k for cells[k-1]; 0 none); nuclei each pixel's visible
	// nucleus, by its cell's label.
	labels []uint32
	nuclei []uint32
}

// unit is a random direction (a normalized pair of normals: no trigonometry).
func unit(s *random.Stream) (float64, float64) {
	for {
		x, y := s.Normal(), s.Normal()
		if r := math.Sqrt(mul(x, x) + mul(y, y)); r > 1e-9 {
			return x / r, y / r
		}
	}
}

func newTissue(p Params) *tissue {
	return &tissue{p: p, w: int(math.Ceil(p.WidthUm / p.PixelUm)), h: int(math.Ceil(p.HeightUm / p.PixelUm))}
}

func (t *tissue) makeDomains(s *random.Stream) {
	p := t.p
	// The mixes' draws: the layout's own, or, for a cohort's sample, a stream the cohort's sections
	// share (each section's domains in other places, with the same mixes).
	mixes := s
	if p.MixSeed != 0 {
		mixes = random.New(p.MixSeed, streamMixes)
	}
	for d := 0; d < p.Domains; d++ {
		dm := domain{x: mul(s.Float64(), p.WidthUm), y: mul(s.Float64(), p.HeightUm), mix: make([]float64, len(p.Types))}
		dominant := d % len(p.Types)
		var rest float64
		for k := range dm.mix {
			if k != dominant {
				dm.mix[k] = mixes.Gamma(1, 1)
				rest += dm.mix[k]
			}
		}
		for k := range dm.mix {
			if k == dominant {
				dm.mix[k] = 0.6
				if len(dm.mix) == 1 {
					dm.mix[k] = 1
				}
			} else if rest > 0 {
				dm.mix[k] = mul(0.4, dm.mix[k]/rest)
			}
		}
		dm.dx, dm.dy = unit(s)
		t.weigh(dm.mix, dominant)
		t.domains = append(t.domains, dm)
	}
}

// weigh multiplies a domain's mix by a cohort's sample's odds (its types' everywhere, and those in
// the domains its leader leads), then normalizes it; without them it leaves the mix alone.
func (t *tissue) weigh(mix []float64, leader int) {
	p := t.p
	if p.TypeOdds == nil && len(p.DomainOdds) == 0 {
		return
	}
	var sum float64
	for k := range mix {
		if p.TypeOdds != nil {
			mix[k] = mul(mix[k], p.TypeOdds[k])
		}
		for _, d := range p.DomainOdds {
			if d.Leader == leader && d.Type == k {
				mix[k] = mul(mix[k], d.Odds)
			}
		}
		sum += mix[k]
	}
	for k := range mix {
		mix[k] /= sum
	}
}

func (t *tissue) domainAt(x, y float64) *domain { return &t.domains[t.domainOf(x, y)] }

// domainOf is the domain whose center is nearest (x, y) (the first, on a tie).
func (t *tissue) domainOf(x, y float64) int {
	best, bestD := 0, math.Inf(1)
	for i, d := range t.domains {
		dx, dy := x-d.x, y-d.y
		if dd := mul(dx, dx) + mul(dy, dy); dd < bestD {
			best, bestD = i, dd
		}
	}
	return best
}

// placeCells throws darts until the cells' areas reach the fill.
func (t *tissue) placeCells(s *random.Stream) {
	p := t.p
	target := mul(mul(p.Fill, p.WidthUm), p.HeightUm)
	maxR := 0.0
	for _, ty := range p.Types {
		maxR = math.Max(maxR, math.Sqrt(ty.AreaUm2/math.Pi)*dmath.Exp(2*ty.AreaSpread))
	}
	grid := newPointGrid(p.WidthUm, p.HeightUm, 2*maxR)
	var placed float64
	misses := 0
	for placed < target && misses < 2000 {
		x, y := mul(s.Float64(), p.WidthUm), mul(s.Float64(), p.HeightUm)
		di := t.domainOf(x, y)
		d := &t.domains[di]
		k := pick(s, d.mix)
		ty := p.Types[k]
		area := s.LogNormal(dmath.Log(ty.AreaUm2), ty.AreaSpread)
		r := math.Sqrt(area / math.Pi)
		crowded := grid.crowded(x, y, r, t.cells)
		// A cohort's sample: a candidate refused tries other places in its domain, keeping its type
		// and size, so that the types placed are the types drawn, whatever their sizes.
		for try := 0; crowded && try < p.Retries; try++ {
			nx, ny := mul(s.Float64(), p.WidthUm), mul(s.Float64(), p.HeightUm)
			if t.domainOf(nx, ny) == di && !grid.crowded(nx, ny, r, t.cells) {
				x, y, crowded = nx, ny, false
			}
		}
		if crowded {
			misses++
			continue
		}
		misses = 0
		e := 1 + mul(s.Float64(), ty.Elongation-1)
		c := cell{typ: k, domain: di, x: x, y: y, a: mul(r, math.Sqrt(e)), b: r / math.Sqrt(e)}
		// Elongated cells half follow their domain's direction.
		nx, ny := unit(s)
		c.ux, c.uy = d.dx+mul(0.8, nx), d.dy+mul(0.8, ny)
		if n := math.Sqrt(mul(c.ux, c.ux) + mul(c.uy, c.uy)); n > 1e-9 {
			c.ux, c.uy = c.ux/n, c.uy/n
		} else {
			c.ux, c.uy = 1, 0
		}
		c.h2c, c.h2s = mul(0.06, s.Normal()), mul(0.06, s.Normal())
		c.h3c, c.h3s = mul(0.05, s.Normal()), mul(0.05, s.Normal())
		grid.add(x, y, len(t.cells))
		t.cells = append(t.cells, c)
		placed += area
	}
}

func pick(s *random.Stream, weights []float64) int {
	u := s.Float64()
	for k, w := range weights {
		if u < w {
			return k
		}
		u -= w
	}
	return len(weights) - 1
}

// pointGrid finds placed cells near a point.
type pointGrid struct {
	size       float64
	cols, rows int
	at         [][]int
}

func newPointGrid(w, h, size float64) *pointGrid {
	g := &pointGrid{size: size, cols: int(w/size) + 1, rows: int(h/size) + 1}
	g.at = make([][]int, g.cols*g.rows)
	return g
}

func (g *pointGrid) cellOf(x, y float64) (int, int) {
	return min(int(x/g.size), g.cols-1), min(int(y/g.size), g.rows-1)
}

func (g *pointGrid) add(x, y float64, i int) {
	c, r := g.cellOf(x, y)
	g.at[r*g.cols+c] = append(g.at[r*g.cols+c], i)
}

// crowded: a placed cell lies closer than three quarters of the summed radii.
func (g *pointGrid) crowded(x, y, r float64, cells []cell) bool {
	c, rw := g.cellOf(x, y)
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			cc, rr := c+dc, rw+dr
			if cc < 0 || rr < 0 || cc >= g.cols || rr >= g.rows {
				continue
			}
			for _, i := range g.at[rr*g.cols+cc] {
				o := &cells[i]
				dx, dy := x-o.x, y-o.y
				reach := mul(0.75, r+math.Sqrt(mul(o.a, o.b)))
				if mul(dx, dx)+mul(dy, dy) < mul(reach, reach) {
					return true
				}
			}
		}
	}
	return false
}

// scaled is a pixel centre's squared distance from a cell's centre in units of its outline's
// radius in that direction (below 1 inside).
func (c *cell) scaled(px, py float64) float64 {
	dx, dy := px-c.x, py-c.y
	u := mul(dx, c.ux) + mul(dy, c.uy)
	v := mul(dy, c.ux) - mul(dx, c.uy)
	q := mul(u/c.a, u/c.a) + mul(v/c.b, v/c.b)
	rho := math.Sqrt(mul(u, u) + mul(v, v))
	if rho == 0 {
		return q
	}
	cs, sn := u/rho, v/rho
	cos2, sin2 := mul(cs, cs)-mul(sn, sn), mul(2, mul(cs, sn))
	cos3 := mul(4, mul(mul(cs, cs), cs)) - mul(3, cs)
	sin3 := mul(3, sn) - mul(4, mul(mul(sn, sn), sn))
	w := 1 + mul(c.h2c, cos2) + mul(c.h2s, sin2) + mul(c.h3c, cos3) + mul(c.h3s, sin3)
	return q / mul(w, w)
}

// rasterize gives each pixel to the cell whose scaled distance is least below 1, keeps each
// cell's largest piece, drops cells too small, and numbers the cells kept.
func (t *tissue) rasterize(stop func() bool) error {
	ps := t.p.PixelUm
	t.labels = make([]uint32, t.w*t.h)
	best := make([]float32, t.w*t.h)
	for i := range best {
		best[i] = 1
	}
	for k := range t.cells {
		if k%256 == 0 && stop() {
			return errStopped
		}
		c := &t.cells[k]
		reach := mul(1.3, c.a) // the outline's harmonics stretch it a little
		x0, x1 := max(0, int((c.x-reach)/ps)), min(t.w-1, int((c.x+reach)/ps))
		y0, y1 := max(0, int((c.y-reach)/ps)), min(t.h-1, int((c.y+reach)/ps))
		for y := y0; y <= y1; y++ {
			py := mul(float64(y)+0.5, ps)
			for x := x0; x <= x1; x++ {
				q := float32(c.scaled(mul(float64(x)+0.5, ps), py))
				if q < best[y*t.w+x] {
					best[y*t.w+x] = q
					t.labels[y*t.w+x] = uint32(k + 1)
				}
			}
		}
	}
	// Each cell's pixels, then its largest 4-connected piece.
	pixels := make([][]int32, len(t.cells))
	for i, l := range t.labels {
		if l > 0 {
			pixels[l-1] = append(pixels[l-1], int32(i))
		}
	}
	minPixels := int(8 / mul(ps, ps))
	kept := make([]cell, 0, len(t.cells))
	relabel := make([]uint32, len(t.cells)+1)
	for k := range t.cells {
		piece := largestPiece(pixels[k], t.labels, t.w, uint32(k+1))
		if len(piece) < minPixels {
			continue
		}
		c := t.cells[k]
		c.pixels = piece
		kept = append(kept, c)
		relabel[k+1] = uint32(len(kept))
	}
	for i := range t.labels {
		t.labels[i] = 0
	}
	for k := range kept {
		for _, i := range kept[k].pixels {
			t.labels[i] = uint32(k + 1)
		}
	}
	t.cells = kept
	return nil
}

// largestPiece is the largest 4-connected run of pixels labelled l among pixels (the first found
// of equal ones), in row-major order.
func largestPiece(pixels []int32, labels []uint32, w int, l uint32) []int32 {
	if len(pixels) == 0 {
		return nil
	}
	seen := map[int32]int{}
	var best []int32
	for _, start := range pixels {
		if _, ok := seen[start]; ok {
			continue
		}
		id := len(seen) + 1
		piece := []int32{start}
		seen[start] = id
		for i := 0; i < len(piece); i++ {
			p := piece[i]
			x := int(p) % w
			for _, n := range [4]int32{p - 1, p + 1, p - int32(w), p + int32(w)} {
				if n < 0 || int(n) >= len(labels) || labels[n] != l {
					continue
				}
				if (n == p-1 && x == 0) || (n == p+1 && x == w-1) {
					continue
				}
				if _, ok := seen[n]; !ok {
					seen[n] = id
					piece = append(piece, n)
				}
			}
		}
		if len(piece) > len(best) {
			best = piece
		}
	}
	sortInt32(best)
	return best
}

func sortInt32(v []int32) {
	// An insertion sort would be quadratic for large cells: a radix pass on 32-bit keys.
	if len(v) < 2 {
		return
	}
	tmp := make([]int32, len(v))
	for shift := uint(0); shift < 32; shift += 8 {
		var count [257]int
		for _, x := range v {
			count[(uint32(x)>>shift)&0xff+1]++
		}
		for i := 1; i < 257; i++ {
			count[i] += count[i-1]
		}
		for _, x := range v {
			b := (uint32(x) >> shift) & 0xff
			tmp[count[b]] = x
			count[b]++
		}
		v, tmp = tmp, v
	}
}

// placeNuclei gives each cell its nucleus (or marks it outside the section) and its depth.
func (t *tissue) placeNuclei(s *random.Stream) {
	p := t.p
	ps := p.PixelUm
	t.nuclei = make([]uint32, t.w*t.h)
	minPixels := int(4 / mul(ps, ps))
	for k := range t.cells {
		c := &t.cells[k]
		ty := p.Types[c.typ]
		// The cell's depth: centred anywhere in the section, cut by its faces.
		zc := mul(s.Float64(), p.DepthUm)
		c.zLo, c.zHi = math.Max(0, zc-ty.HeightUm/2), math.Min(p.DepthUm, zc+ty.HeightUm/2)
		hidden := s.Float64() < p.HiddenNuclei
		r := math.Sqrt(mul(c.a, c.b))
		e := math.Sqrt(c.a / c.b) // the nucleus is rounder: the square root of the elongation
		rn := mul(r, ty.NucleusShare)
		c.na, c.nb = mul(rn, math.Sqrt(e)), rn/math.Sqrt(e)
		ox, oy := unit(s)
		off := mul(mul(0.12, r), s.Float64())
		c.nx, c.ny = c.x+mul(off, ox), c.y+mul(off, oy)
		hn := mul(0.6, c.zHi-c.zLo)
		mid := (c.zLo + c.zHi) / 2
		c.nzLo, c.nzHi = mid-hn/2, mid+hn/2
		if hidden {
			continue
		}
		var nucleus []int32
		for _, i := range c.pixels {
			x, y := int(i)%t.w, int(i)/t.w
			dx, dy := mul(float64(x)+0.5, ps)-c.nx, mul(float64(y)+0.5, ps)-c.ny
			u := mul(dx, c.ux) + mul(dy, c.uy)
			v := mul(dy, c.ux) - mul(dx, c.uy)
			if mul(u/c.na, u/c.na)+mul(v/c.nb, v/c.nb) < 1 {
				nucleus = append(nucleus, i)
			}
		}
		if len(nucleus) < minPixels {
			continue
		}
		c.nucleus = true
		c.nucleusPixel = nucleus
		for _, i := range nucleus {
			t.nuclei[i] = uint32(k + 1)
		}
	}
}
