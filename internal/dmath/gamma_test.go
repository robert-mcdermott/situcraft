package dmath

import (
	"fmt"
	"math"
	"testing"
)

// Each within a few units in the last place of Go's own (exact to within one) over its range.
func TestLgammaLog1pErfinvAsGosWithinAFewUlps(t *testing.T) {
	worst := map[string]float64{}
	check := func(name string, got, want float64, limit float64) {
		u := float64(ulps(got, want))
		worst[name] = math.Max(worst[name], u)
		if !(u <= limit) {
			t.Fatalf("%s: %v, Go's %v (%.1f ulps)", name, got, want, u)
		}
	}
	for x := 1e-6; x < 1e7; x *= 1.0137 {
		want, _ := math.Lgamma(x)
		// Near its zeros (1 and 2) lgamma is small: compare absolutely there.
		if math.Abs(want) < 1e-3 {
			if d := math.Abs(Lgamma(x) - want); d > 1e-17 {
				t.Fatalf("Lgamma(%v): %v, Go's %v", x, Lgamma(x), want)
			}
			continue
		}
		check("Lgamma", Lgamma(x), want, 4)
	}
	for _, x := range []float64{0.5, 1, 2, 3, 10, 0.25, 1.5, 7.9, 8, 100.5} {
		want, _ := math.Lgamma(x)
		check("Lgamma at "+fmt.Sprint(x), Lgamma(x), want, 4)
	}
	for x := -0.999; x < 1e6; x = x*1.0213 + 1e-9 + math.Max(0, -x*0.03) {
		check("Log1p", Log1p(x), math.Log1p(x), 3)
	}
	for x := 1e-300; x < 1e-3; x *= 1.97 {
		check("Log1p tiny", Log1p(x), math.Log1p(x), 2)
		check("Log1p tiny negative", Log1p(-x), math.Log1p(-x), 2)
	}
	for x := -0.999999999; x < 1; x += 0.000731 {
		// Within 2 of Go's on amd64; Go's own fuses its polynomials on arm64, a few ulps away.
		check("Erfinv", Erfinv(x), math.Erfinv(x), 6)
	}
	t.Logf("worst: %v", worst)
	if !math.IsNaN(Lgamma(0)) || !math.IsNaN(Lgamma(-1)) || Lgamma(math.Inf(1)) != math.Inf(1) || Log1p(-1) != math.Inf(-1) || !math.IsNaN(Log1p(-2)) || Erfinv(1) != math.Inf(1) || Erfinv(-1) != math.Inf(-1) || !math.IsNaN(Erfinv(2)) {
		t.Fatal("special values")
	}
}

// The same bits on every processor: a digest of each over many arguments, pinned (checked on arm64
// and on amd64).
func TestLgammaLog1pErfinvTheSameOnEveryProcessor(t *testing.T) {
	h := uint32(0x811c9dc5)
	mix := func(v float64) {
		bits := math.Float64bits(v)
		for _, word := range []uint32{uint32(bits), uint32(bits >> 32)} {
			for b := 0; b < 32; b += 8 {
				h = (h ^ (word>>b)&0xff) * 0x01000193
			}
		}
	}
	for i := 1; i <= 20000; i++ {
		// Each argument rounded on its own too: x − 1 below would otherwise fuse with this product.
		x := mul(float64(i), 0.0137)
		mix(Lgamma(x))
		mix(Lgamma(mul(x, x) + 1e-3))
		mix(Log1p(x - 1))
		mix(Erfinv(math.Mod(x, 1.999) - 0.9995))
	}
	if got := fmt.Sprintf("%08x", h); got != pinnedGamma {
		t.Fatalf("digest %s, pinned %s", got, pinnedGamma)
	}
}

const pinnedGamma = "d2dcda56"
