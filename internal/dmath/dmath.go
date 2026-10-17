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

// Erfc is the complementary error function (fdlibm s_erf.c's erfc, the same notice), whose upper
// tail gives the normal distribution's: P(Z > z) = Erfc(z/√2)/2.
func Erfc(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return math.NaN()
	case math.IsInf(x, 1):
		return 0
	case math.IsInf(x, -1):
		return 2
	}
	sign := x < 0
	if sign {
		x = -x
	}
	if x < 0.84375 {
		var t float64
		if x < 0x1p-56 {
			t = x
		} else {
			z := mul(x, x)
			y := horner(z, efPP[:]) / (1 + mul(z, horner(z, efQQ[:])))
			if x < 0.25 {
				t = x + mul(x, y)
			} else {
				t = 0.5 + (mul(x, y) + (x - 0.5))
			}
		}
		if sign {
			return 1 + t
		}
		return 1 - t
	}
	if x < 1.25 {
		s := x - 1
		pq := horner(s, efPA[:]) / (1 + mul(s, horner(s, efQA[:])))
		if sign {
			return 1 + efErx + pq
		}
		return 1 - efErx - pq
	}
	if x >= 28 {
		if sign {
			return 2
		}
		return 0
	}
	s := 1 / mul(x, x)
	var r, q float64
	if x < 1/0.35 {
		r, q = horner(s, efRA[:]), 1+mul(s, horner(s, efSA[:]))
	} else {
		if sign && x > 6 {
			return 2
		}
		r, q = horner(s, efRB[:]), 1+mul(s, horner(s, efSB[:]))
	}
	z := math.Float64frombits(math.Float64bits(x) & 0xffffffff00000000)
	e := mul(Exp(-mul(z, z)-0.5625), Exp(mul(z-x, z+x)+r/q))
	if sign {
		return 2 - e/x
	}
	return e / x
}

// horner is c[0] + z(c[1] + z(c[2] + …)), each product rounded on its own.
func horner(z float64, c []float64) float64 {
	h := c[len(c)-1]
	for i := len(c) - 2; i >= 0; i-- {
		h = c[i] + mul(z, h)
	}
	return h
}

// fdlibm s_erf.c's coefficients.
const efErx = 8.45062911510467529297e-01

var (
	efPP = [...]float64{1.28379167095512558561e-01, -3.25042107247001499370e-01, -2.84817495755985104766e-02, -5.77027029648944159157e-03, -2.37630166566501626084e-05}
	efQQ = [...]float64{3.97917223959155352819e-01, 6.50222499887672944485e-02, 5.08130628187576562776e-03, 1.32494738004321644526e-04, -3.96022827877536812320e-06}
	efPA = [...]float64{-2.36211856075265944077e-03, 4.14856118683748331666e-01, -3.72207876035701323847e-01, 3.18346619901161753674e-01, -1.10894694282396677476e-01, 3.54783043256182359371e-02, -2.16637559486879084300e-03}
	efQA = [...]float64{1.06420880400844228286e-01, 5.40397917702171048937e-01, 7.18286544141962662868e-02, 1.26171219808761642112e-01, 1.36370839120290507362e-02, 1.19844998467991074170e-02}
	efRA = [...]float64{-9.86494403484714822705e-03, -6.93858572707181764372e-01, -1.05586262253232909814e+01, -6.23753324503260060396e+01, -1.62396669462573470355e+02, -1.84605092906711035994e+02, -8.12874355063065934246e+01, -9.81432934416914548592e+00}
	efSA = [...]float64{1.96512716674392571292e+01, 1.37657754143519042600e+02, 4.34565877475229228821e+02, 6.45387271733267880336e+02, 4.29008140027567833386e+02, 1.08635005541779435134e+02, 6.57024977031928170135e+00, -6.04244152148580987438e-02}
	efRB = [...]float64{-9.86494292470009928597e-03, -7.99283237680523006574e-01, -1.77579549177547519889e+01, -1.60636384855821916062e+02, -6.37566443368389627722e+02, -1.02509513161107724954e+03, -4.83519191608651397019e+02}
	efSB = [...]float64{3.03380607434824582924e+01, 3.25792512996573918826e+02, 1.53672958608443695994e+03, 3.19985821950859553908e+03, 2.55305040643316442583e+03, 4.74528541206955367215e+02, -2.24409524465858183362e+01}
)

// π/2 in two parts, the first with its low 33 bits zero, so that k·pio2Hi is exact for |k| < 2²⁰
// (Cody and Waite's reduction).
const (
	pio2Hi = 1.57079632673412561417e+00
	pio2Lo = 6.07710050650619224932e-11
)

// Taylor's coefficients of sin and cos about 0 (±1/n!), to the 17th and 18th powers: on |r| ≤ π/4
// the next terms are below 1e-19 of the result.
var (
	sinTaylor = [...]float64{-1.0 / 6, 1.0 / 120, -1.0 / 5040, 1.0 / 362880, -1.0 / 39916800, 1.0 / 6227020800, -1.0 / 1307674368000, 1.0 / 355687428096000}
	cosTaylor = [...]float64{-1.0 / 2, 1.0 / 24, -1.0 / 720, 1.0 / 40320, -1.0 / 3628800, 1.0 / 479001600, -1.0 / 87178291200, 1.0 / 20922789888000, -1.0 / 6402373705728000}
)

// SinCos is the sine and cosine of x (radians), the same on every processor, within a few units in
// the last place for |x| below 10⁵ (what simulations turn and wave by; NaN beyond 2²⁰·π/2). x is
// reduced by multiples of π/2 to |r| ≤ π/4, where Taylor's series converge fast.
func SinCos(x float64) (sin, cos float64) {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > 1647099 {
		return math.NaN(), math.NaN()
	}
	k := math.Round(x / (math.Pi / 2))
	r := (x - mul(k, pio2Hi)) - mul(k, pio2Lo)
	z := mul(r, r)
	s := r + mul(mul(r, z), horner(z, sinTaylor[:]))
	c := 1 + mul(z, horner(z, cosTaylor[:]))
	switch int64(k) & 3 {
	case 0:
		return s, c
	case 1:
		return c, -s
	case 2:
		return -s, -c
	}
	return -c, s
}
