package dmath

import (
	"fmt"
	"math"
	"testing"
)

// Within a unit or two in the last place of Go's own, over the reductions' ranges and their edges;
// the quadrants and special values as math.Atan2 gives them.
func TestAtanAtan2AsGos(t *testing.T) {
	worst := 0.0
	for x := 1e-12; x < 1e25; x *= 1.0071 {
		for _, v := range []float64{x, -x} {
			u := float64(ulps(Atan(v), math.Atan(v)))
			worst = math.Max(worst, u)
			if u > 2 {
				t.Fatalf("Atan(%v): %v, Go's %v (%.0f ulps)", v, Atan(v), math.Atan(v), u)
			}
		}
	}
	for _, x := range []float64{0.4375, 0.6875, 1.1875, 2.4375, 1 << 66, math.Nextafter(0.4375, 0), math.Nextafter(2.4375, 0)} {
		if u := ulps(Atan(x), math.Atan(x)); u > 2 {
			t.Errorf("Atan(%v): %v, Go's %v (%d ulps)", x, Atan(x), math.Atan(x), u)
		}
	}
	t.Logf("Atan: worst %.0f ulps", worst)
	for i := 0; i < 3600; i++ {
		a := mul(float64(i), math.Pi/1800) - math.Pi
		for _, r := range []float64{1e-3, 1, 37.5, 1e6} {
			s, c := SinCos(a)
			y, x := mul(r, s), mul(r, c)
			if u := ulps(Atan2(y, x), math.Atan2(y, x)); u > 3 && math.Abs(Atan2(y, x)-math.Atan2(y, x)) > 1e-15 {
				t.Fatalf("Atan2(%v, %v): %v, Go's %v", y, x, Atan2(y, x), math.Atan2(y, x))
			}
		}
	}
	inf, nan := math.Inf(1), math.NaN()
	for _, c := range [][2]float64{{0, 1}, {0, -1}, {math.Copysign(0, -1), 1}, {math.Copysign(0, -1), -1}, {0, 0}, {0, math.Copysign(0, -1)},
		{1, 0}, {-1, 0}, {inf, inf}, {inf, -inf}, {-inf, inf}, {-inf, -inf}, {1, inf}, {1, -inf}, {-1, -inf}, {inf, 1}, {-inf, 1}} {
		got, want := Atan2(c[0], c[1]), math.Atan2(c[0], c[1])
		if got != want || math.Signbit(got) != math.Signbit(want) {
			t.Errorf("Atan2(%v, %v): %v, Go's %v", c[0], c[1], got, want)
		}
	}
	if !math.IsNaN(Atan2(nan, 1)) || !math.IsNaN(Atan(nan)) || Atan(inf) != math.Pi/2 || Atan(-inf) != -math.Pi/2 {
		t.Error("special values")
	}
}

// The same bits on every processor: a digest of each over many arguments, pinned (checked on arm64
// and on amd64).
func TestAtanAtan2TheSameOnEveryProcessor(t *testing.T) {
	h := uint32(0x811c9dc5)
	mix := func(v float64) {
		bits := math.Float64bits(v)
		for b := 0; b < 64; b += 8 {
			h = (h ^ uint32(bits>>b)&0xff) * 0x01000193
		}
	}
	for i := 1; i <= 20000; i++ {
		x := mul(float64(i-10000), 0.000731)
		mix(Atan(x))
		mix(Atan(mul(x, mul(x, x))))
		mix(Atan2(x, mul(float64(i%17-8), 0.37)))
	}
	if got := fmt.Sprintf("%08x", h); got != pinnedAtan {
		t.Fatalf("digest %s, pinned %s", got, pinnedAtan)
	}
}

const pinnedAtan = "f81176e4"
