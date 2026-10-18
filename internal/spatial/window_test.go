package spatial

import (
	"context"
	"math"
	"testing"

	"situcraft/internal/random"
)

func rectangleWindow(t *testing.T, x0, y0, x1, y1 float64) *Window {
	t.Helper()
	w, err := PolygonWindow([][][][2]float64{{{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}}})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A rectangle by hand: its area, what is inside, the distance to its edge, and the share of a
// circle inside it near an edge (1 − acos(h/r)/π) and in a corner (5/12 for h = r/2 from both).
func TestARectangleWindow(t *testing.T) {
	w := rectangleWindow(t, 0, 0, 100, 50)
	if w.Area != 5000 {
		t.Errorf("area %g", w.Area)
	}
	for _, c := range []struct {
		x, y float64
		in   bool
		d    float64
	}{{10, 10, true, 10}, {50, 25, true, 25}, {99, 1, true, 1}, {-1, 10, false, 1}, {120, 60, false, math.Hypot(20, 10)}} {
		if w.Contains(c.x, c.y) != c.in || math.Abs(w.Distance(c.x, c.y)-c.d) > 1e-12 {
			t.Errorf("(%g, %g): inside %v, distance %g; not %v, %g", c.x, c.y, w.Contains(c.x, c.y), w.Distance(c.x, c.y), c.in, c.d)
		}
	}
	for _, c := range []struct {
		x, y, r, want float64
	}{
		{50, 25, 10, 1},
		{5, 25, 10, 1 - math.Acos(0.5)/math.Pi},
		{5, 5, 10, 5.0 / 12},
		{50, 25, 200, 0},
		{3, 25, 20, 1 - math.Acos(3.0/20)/math.Pi},
	} {
		if got := w.ArcInside(c.x, c.y, c.r); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("the circle of %g about (%g, %g): %.15f inside, not %.15f", c.r, c.x, c.y, got, c.want)
		}
	}
}

// A square with a square hole: its area, and what is inside.
func TestAWindowWithAHole(t *testing.T) {
	w, err := PolygonWindow([][][][2]float64{{{{0, 0}, {100, 0}, {100, 100}, {0, 100}}, {{40, 40}, {40, 60}, {60, 60}, {60, 40}}}})
	if err != nil {
		t.Fatal(err)
	}
	if w.Area != 9600 || w.Contains(50, 50) || !w.Contains(10, 10) || !w.Contains(39, 50) || w.Contains(41, 59) {
		t.Errorf("area %g; inside (50, 50) %v, (10, 10) %v", w.Area, w.Contains(50, 50), w.Contains(10, 10))
	}
	// A circle about the hole's center reaching beyond it: all of it outside the hole.
	if got := w.ArcInside(50, 50, 15); math.Abs(got-1) > 1e-12 {
		t.Errorf("a circle around the hole: %g inside", got)
	}
	if got := w.ArcInside(50, 50, 5); got != 0 {
		t.Errorf("a circle in the hole: %g inside", got)
	}
}

// The tissue from its cells' pixels, closed by a radius: a gap narrower than twice it filled, a
// wider one kept, the space beyond the cells not tissue.
func TestTheTissueWindow(t *testing.T) {
	// Pixels of 0.5 µm: cells over x 0–20 µm and 23–43 µm (a 3 µm gap), and 83–103 µm (a 40 µm gap),
	// rows 0–20 µm.
	width, height := 220, 40
	rows := make([][]uint32, height)
	for y := range rows {
		rows[y] = make([]uint32, width)
		for x := range rows[y] {
			um := float64(x) * 0.5
			if um < 20 || um >= 23 && um < 43 || um >= 83 && um < 103 {
				rows[y][x] = 1
			}
		}
	}
	r := raster(rows, 0.5)
	w, g, err := TissueWindow(context.Background(), r, 1, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if g != 1 {
		t.Fatalf("the grid is %g µm", g)
	}
	// The cells' 60 µm of width and the 3 µm gap, 20 µm high: 1,260 µm², less a pixel or two where
	// the gap meets the edges (the distance transform rounds a concave corner by a pixel).
	if w.Area > 63*20 || w.Area < 63*20-8 {
		t.Errorf("the tissue's area %g, not about %d", w.Area, 63*20)
	}
	if !w.Contains(21.5, 10) || w.Contains(60, 10) || !w.Contains(90, 10) || w.Contains(10, 30) {
		t.Errorf("inside: the narrow gap %v, the wide one %v, a cell %v, beyond %v", w.Contains(21.5, 10), w.Contains(60, 10), w.Contains(90, 10), w.Contains(10, 30))
	}
}

// The share of a circle inside a rectangle against Ripley's closed form (Ripley 1977), for circles
// reaching at most two of its sides: with h and v the distances to the nearest side across and up,
// 1 − (acos(min(h, r)/r) + acos(min(v, r)/r))/π while r ≤ √(h² + v²), and 3/4 − (acos(h/r) +
// acos(v/r))/(2π) once the circle passes the corner.
func TestArcInsideARectangleAgainstRipleysFormula(t *testing.T) {
	w := rectangleWindow(t, 0, 0, 400, 300)
	s := random.New(81, 1)
	for k := 0; k < 2000; k++ {
		x, y := 400*s.Float64(), 300*s.Float64()
		r := 0.1 + 140*s.Float64()
		h := math.Min(x, 400-x)
		v := math.Min(y, 300-y)
		var want float64
		if r <= math.Hypot(h, v) {
			want = 1 - (math.Acos(math.Min(h, r)/r)+math.Acos(math.Min(v, r)/r))/math.Pi
		} else {
			want = 0.75 - (math.Acos(h/r)+math.Acos(v/r))/(2*math.Pi)
		}
		if got := w.ArcInside(x, y, r); math.Abs(got-want) > 1e-9 {
			t.Fatalf("the circle of %g about (%g, %g): %.12f inside, the formula %.12f", r, x, y, got, want)
		}
	}
}
