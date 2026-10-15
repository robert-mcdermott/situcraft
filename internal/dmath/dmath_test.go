package dmath

import (
	"fmt"
	"math"
	"testing"
)

// fingerprint is web/lib/dmath.js's checksum of log, exp and pow over the same 30,000 arguments
// (FNV-1a of their bits): the same in Go as in every JavaScript engine.
func fingerprint() string {
	h := uint32(0x811c9dc5)
	mix := func(v float64) {
		bits := math.Float64bits(v)
		for _, word := range []uint32{uint32(bits), uint32(bits >> 32)} {
			for b := 0; b < 32; b += 8 {
				h = (h ^ (word>>b)&0xff) * 0x01000193
			}
		}
	}
	s := int64(12345)
	for i := 0; i < 10000; i++ {
		s = (s * 48271) % 2147483647
		x := float64(s) / 2147483647
		for j := s % 24; j > 0; j-- {
			x *= 10
		}
		for j := 0; j < 12; j++ {
			x /= 10
		}
		mix(Log(x))
		mix(Exp(math.Mod(x, 1400) - 700))
		mix(Pow(x, 0.37))
	}
	return fmt.Sprintf("%08x", h)
}

func TestTheSameBitsAsThePage(t *testing.T) {
	if got := fingerprint(); got != "7bf3fe17" {
		t.Fatalf("fingerprint %s, the page's 7bf3fe17", got)
	}
}

func ulps(a, b float64) uint64 {
	x, y := math.Float64bits(a), math.Float64bits(b)
	if x > y {
		return x - y
	}
	return y - x
}

func TestCloseToGo(t *testing.T) {
	worst := map[string]uint64{}
	for i := 0; i < 200000; i++ {
		x := math.Ldexp(1+float64(i%9973)/9973, i%2000-1000)
		if d := ulps(Log(x), math.Log(x)); d > worst["log"] {
			worst["log"] = d
		}
		y := float64(i)/200000*1400 - 700
		if d := ulps(Exp(y), math.Exp(y)); d > worst["exp"] {
			worst["exp"] = d
		}
	}
	// Each is within an ulp of the true value; Go's own are too (its assembly on some
	// processors), so the two may be two apart.
	if worst["log"] > 2 || worst["exp"] > 2 {
		t.Fatalf("more than two ulps from Go's: %v", worst)
	}
}

func TestSpecialValues(t *testing.T) {
	for _, c := range []struct {
		got, want float64
	}{
		{Log(1), 0}, {Log(math.E), 1}, {Exp(0), 1}, {Exp(1), math.E}, {Exp(-1000), 0},
		{Log(math.SmallestNonzeroFloat64), -744.4400719213812}, // -1074 ln 2
	} {
		if c.got != c.want && ulps(c.got, c.want) > 1 {
			t.Fatalf("%v, want %v", c.got, c.want)
		}
	}
	if !math.IsInf(Log(0), -1) || !math.IsNaN(Log(-1)) || !math.IsInf(Exp(1000), 1) || !math.IsNaN(Exp(math.NaN())) || Exp(math.Inf(-1)) != 0 || !math.IsInf(Exp(math.Inf(1)), 1) {
		t.Fatal("special values")
	}
}
