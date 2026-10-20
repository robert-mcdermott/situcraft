package dmath

import "math"

// Atan is the arctangent (radians, in [−π/2, π/2]): fdlibm 5.3's s_atan.c (Sun Microsystems,
// 1993; the notice the package's comment gives), every product rounded on its own. |x| is reduced
// to below 7/16 about atan(0.5), atan(1), atan(1.5) or atan(∞), where an odd polynomial of 11
// terms converges.
func Atan(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return x
	case x == 0:
		return x // keeps its sign
	}
	neg := x < 0
	a := math.Abs(x)
	if a >= 1<<66 {
		if neg {
			return -atanHi[3] - atanLo[3]
		}
		return atanHi[3] + atanLo[3]
	}
	id := -1
	switch {
	case a < 0.4375:
		if a < 1.0/(1<<29) {
			return x
		}
		a = x // the sign kept: odd
	case a < 0.6875:
		id, a = 0, (mul(2, a)-1)/(2+a)
	case a < 1.1875:
		id, a = 1, (a-1)/(a+1)
	case a < 2.4375:
		id, a = 2, (a-1.5)/(1+mul(1.5, a))
	default:
		id, a = 3, -1/a
	}
	z := mul(a, a)
	w := mul(z, z)
	s1 := mul(z, horner(w, atanEven[:]))
	s2 := mul(w, horner(w, atanOdd[:]))
	if id < 0 {
		return a - mul(a, s1+s2)
	}
	r := atanHi[id] - ((mul(a, s1+s2) - atanLo[id]) - a)
	if neg {
		return -r
	}
	return r
}

// Atan2 is the angle of the point (x, y) from the x axis (radians, in [−π, π]), as math.Atan2
// gives it: from Atan(y/x), turned by π when x < 0.
func Atan2(y, x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case y == 0:
		if x > 0 || x == 0 && !math.Signbit(x) {
			return y
		}
		return math.Copysign(math.Pi, y)
	case x == 0:
		return math.Copysign(math.Pi/2, y)
	case math.IsInf(x, 0):
		switch {
		case math.IsInf(y, 0) && x > 0:
			return math.Copysign(math.Pi/4, y)
		case math.IsInf(y, 0):
			return math.Copysign(3*math.Pi/4, y)
		case x > 0:
			return math.Copysign(0, y)
		}
		return math.Copysign(math.Pi, y)
	case math.IsInf(y, 0):
		return math.Copysign(math.Pi/2, y)
	}
	q := Atan(y / x)
	if x < 0 {
		if q <= 0 {
			return q + math.Pi
		}
		return q - math.Pi
	}
	return q
}

// s_atan.c's atan(0.5), atan(1), atan(1.5) and atan(∞), each as a head and a tail, and its
// polynomial's coefficients (aT[0]–aT[10]), even and odd, arranged for horner.
var (
	atanHi   = [...]float64{4.63647609000806093515e-01, 7.85398163397448278999e-01, 9.82793723247329054082e-01, 1.57079632679489655800e+00}
	atanLo   = [...]float64{2.26987774529616870924e-17, 3.06161699786838301793e-17, 1.39033110312309984516e-17, 6.12323399573676603587e-17}
	atanEven = [...]float64{3.33333333333329318027e-01, 1.42857142725034663711e-01, 9.09088713343650656196e-02, 6.66107313738753120669e-02, 4.97687799461593236017e-02, 1.62858201153657823623e-02}
	atanOdd  = [...]float64{-1.99999999998764832476e-01, -1.11111104054623557880e-01, -7.69187620504482999495e-02, -5.83357013379057348645e-02, -3.65315727442169155270e-02}
)
