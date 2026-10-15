// Package dmath gives logarithms and exponentials whose bits are the same on every processor, as
// web/lib/dmath.js gives them in every JavaScript engine. Go's math.Log and math.Exp are exact to
// within an ulp, but their results may differ in the last bit between processors: some have
// assembly versions, and the compiler may fuse a multiply and an add into one instruction on some
// processors (arm64) and not others (amd64), rounding once instead of twice. What SituCraft
// samples and simulates must be the same anywhere for a seed, so these are fdlibm 5.3's
// __ieee754_log and __ieee754_exp (Sun Microsystems, 1993; "Permission to use, copy, modify, and
// distribute this software is freely granted, provided that this notice is preserved"), as
// dmath.js has them, with every product rounded on its own (an explicit conversion, which the Go
// specification says prevents fusion). The error is below one unit in the last place, and the
// result is the same everywhere, and the same as the page's. math.Sqrt is correctly rounded and
// needs no replacement.
package dmath

import "math"

// mul is a product rounded on its own, never fused with an addition.
func mul(a, b float64) float64 { return float64(a * b) }

func high(x float64) int32 { return int32(math.Float64bits(x) >> 32) }
func low(x float64) uint32 { return uint32(math.Float64bits(x)) }

func withHigh(x float64, hi int32) float64 {
	return math.Float64frombits(uint64(uint32(hi))<<32 | uint64(low(x)))
}

const (
	ln2Hi = 6.93147180369123816490e-01 // 0x3fe62e42 0xfee00000
	ln2Lo = 1.90821492927058770002e-10 // 0x3dea39ef 0x35793c76
	two54 = 1.80143985094819840000e+16
	lg1   = 6.666666666666735130e-01
	lg2   = 3.999999999940941908e-01
	lg3   = 2.857142874366239149e-01
	lg4   = 2.222219843214978396e-01
	lg5   = 1.818357216161805012e-01
	lg6   = 1.531383769920937332e-01
	lg7   = 1.479819860511658591e-01
)

// Log is the natural logarithm (fdlibm e_log.c).
func Log(x float64) float64 {
	hx, lx := high(x), low(x)
	k := int32(0)
	if hx < 0x00100000 { // x < 2^-1022
		if uint32(hx&0x7fffffff)|lx == 0 {
			return math.Inf(-1) // log(±0)
		}
		if hx < 0 {
			return math.NaN() // log(negative)
		}
		k -= 54
		x = mul(x, two54) // a subnormal number, scaled up
		hx = high(x)
	}
	if hx >= 0x7ff00000 {
		return x + x // Infinity or NaN
	}
	k += (hx >> 20) - 1023
	hx &= 0x000fffff
	i := (hx + 0x95f64) & 0x100000
	x = withHigh(x, hx|(i^0x3ff00000)) // x or x/2, normalized
	k += i >> 20
	f := x - 1.0
	dk := float64(k)
	if (0x000fffff & (2 + hx)) < 3 { // |f| < 2^-20
		if f == 0 {
			if k == 0 {
				return 0
			}
			return mul(dk, ln2Hi) + mul(dk, ln2Lo)
		}
		r := mul(mul(f, f), 0.5-mul(0.33333333333333333, f))
		if k == 0 {
			return f - r
		}
		return mul(dk, ln2Hi) - ((r - mul(dk, ln2Lo)) - f)
	}
	s := f / (2.0 + f)
	z := mul(s, s)
	i = hx - 0x6147a
	w := mul(z, z)
	j := 0x6b851 - hx
	t1 := mul(w, lg2+mul(w, lg4+mul(w, lg6)))
	t2 := mul(z, lg1+mul(w, lg3+mul(w, lg5+mul(w, lg7))))
	i |= j
	r := t2 + t1
	if i > 0 {
		hfsq := mul(mul(0.5, f), f)
		if k == 0 {
			return f - (hfsq - mul(s, hfsq+r))
		}
		return mul(dk, ln2Hi) - ((hfsq - (mul(s, hfsq+r) + mul(dk, ln2Lo))) - f)
	}
	if k == 0 {
		return f - mul(s, f-r)
	}
	return mul(dk, ln2Hi) - ((mul(s, f-r) - mul(dk, ln2Lo)) - f)
}

const (
	oThreshold = 7.09782712893383973096e+02
	uThreshold = -7.45133219101941108420e+02
	invLn2     = 1.44269504088896338700e+00
	twoM1000   = 9.33263618503218878990e-302 // 2^-1000
	p1         = 1.66666666666666019037e-01
	p2         = -2.77777777770155933842e-03
	p3         = 6.61375632143793436117e-05
	p4         = -1.65339022054652515390e-06
	p5         = 4.13813679705723846039e-08
)

// Exp is e^x (fdlibm e_exp.c).
func Exp(x float64) float64 {
	hx := uint32(math.Float64bits(x) >> 32)
	xsb := (hx >> 31) & 1
	hx &= 0x7fffffff
	if hx >= 0x40862e42 { // |x| ≥ 709.78…
		if hx >= 0x7ff00000 {
			if (hx&0xfffff)|low(x) != 0 {
				return x + x // NaN
			}
			if xsb == 0 {
				return x // exp(+Infinity)
			}
			return 0 // exp(-Infinity)
		}
		if x > oThreshold {
			return math.Inf(1)
		}
		if x < uThreshold {
			return 0
		}
	}
	var hi, lo float64
	k := int32(0)
	if hx > 0x3fd62e42 { // |x| > ln2 / 2
		if hx < 0x3ff0a2b2 { // and |x| < 1.5 ln2
			if xsb == 0 {
				hi, lo, k = x-ln2Hi, ln2Lo, 1
			} else {
				hi, lo, k = x+ln2Hi, -ln2Lo, -1
			}
		} else {
			half := 0.5
			if xsb != 0 {
				half = -0.5
			}
			k = int32(math.Trunc(mul(invLn2, x) + half))
			dk := float64(k)
			hi = x - mul(dk, ln2Hi) // exact
			lo = mul(dk, ln2Lo)
		}
		x = hi - lo
	} else if hx < 0x3e300000 { // |x| < 2^-28
		return 1 + x
	}
	t := mul(x, x)
	c := x - mul(t, p1+mul(t, p2+mul(t, p3+mul(t, p4+mul(t, p5)))))
	if k == 0 {
		return 1 - (mul(x, c)/(c-2.0) - x)
	}
	y := 1 - ((lo - mul(x, c)/(2.0-c)) - hi)
	if k >= -1021 {
		return withHigh(y, high(y)+(k<<20))
	}
	return mul(withHigh(y, high(y)+((k+1000)<<20)), twoM1000)
}

// Pow is x^y for x > 0, as exp(y log x): for fractional powers, not exact integer ones.
func Pow(x, y float64) float64 {
	if y == 0 || x == 1 {
		return 1
	}
	return Exp(mul(y, Log(x)))
}
