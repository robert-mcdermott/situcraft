package spatial

// Exact geometric predicates: the sign of a determinant computed in floating point when an error
// bound says the sign is right, and exactly (rationals) when it does not. The bounds are those of
// Shewchuk's adaptive predicates (Shewchuk JR. Adaptive precision floating-point arithmetic and
// fast robust geometric predicates. Discrete Comput Geom 1997;18:305–363.
// doi:10.1007/PL00009321): a sign is trusted when the determinant exceeds its error bound times
// the permanent (the same sum with every term's absolute value). Every product is rounded on its own
// (mul), so that no processor fuses it into a multiply-add, and the answer is the same everywhere.

import (
	"math"
	"math/big"
)

// mul is a product rounded on its own, never fused with an addition.
func mul(a, b float64) float64 { return float64(a * b) }

const epsilon = 1.0 / (1 << 53) // half a unit in the last place of 1

var (
	orientBound   = (3 + 16*epsilon) * epsilon
	incircleBound = (10 + 96*epsilon) * epsilon
)

// orient is positive when a, b and c turn counter-clockwise (c to the left of a→b, x right and y
// up), negative when clockwise, and zero when they lie on a line.
func orient(ax, ay, bx, by, cx, cy float64) int {
	l := mul(ax-cx, by-cy)
	r := mul(ay-cy, bx-cx)
	det := l - r
	if bound := orientBound * (math.Abs(l) + math.Abs(r)); det > bound || -det > bound {
		return sign(det)
	}
	return orientExact(ax, ay, bx, by, cx, cy)
}

// incircle is positive when d lies inside the circle through a, b and c (counter-clockwise),
// negative when outside, and zero when on it.
func incircle(ax, ay, bx, by, cx, cy, dx, dy float64) int {
	adx, ady := ax-dx, ay-dy
	bdx, bdy := bx-dx, by-dy
	cdx, cdy := cx-dx, cy-dy
	bc, cb := mul(bdx, cdy), mul(cdx, bdy)
	ca, ac := mul(cdx, ady), mul(adx, cdy)
	ab, ba := mul(adx, bdy), mul(bdx, ady)
	alift := mul(adx, adx) + mul(ady, ady)
	blift := mul(bdx, bdx) + mul(bdy, bdy)
	clift := mul(cdx, cdx) + mul(cdy, cdy)
	det := mul(alift, bc-cb) + mul(blift, ca-ac) + mul(clift, ab-ba)
	permanent := mul(math.Abs(bc)+math.Abs(cb), alift) + mul(math.Abs(ca)+math.Abs(ac), blift) + mul(math.Abs(ab)+math.Abs(ba), clift)
	if bound := incircleBound * permanent; det > bound || -det > bound {
		return sign(det)
	}
	return incircleExact(ax, ay, bx, by, cx, cy, dx, dy)
}

func sign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func rat(v float64) *big.Rat { return new(big.Rat).SetFloat64(v) }

func sub(a, b *big.Rat) *big.Rat { return new(big.Rat).Sub(a, b) }

func prod(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }

func orientExact(ax, ay, bx, by, cx, cy float64) int {
	acx, acy := sub(rat(ax), rat(cx)), sub(rat(ay), rat(cy))
	bcx, bcy := sub(rat(bx), rat(cx)), sub(rat(by), rat(cy))
	return sub(prod(acx, bcy), prod(acy, bcx)).Sign()
}

func incircleExact(ax, ay, bx, by, cx, cy, dx, dy float64) int {
	d := [2]*big.Rat{rat(dx), rat(dy)}
	row := func(x, y float64) (*big.Rat, *big.Rat, *big.Rat) {
		px, py := sub(rat(x), d[0]), sub(rat(y), d[1])
		lift := new(big.Rat).Add(prod(px, px), prod(py, py))
		return px, py, lift
	}
	adx, ady, al := row(ax, ay)
	bdx, bdy, bl := row(bx, by)
	cdx, cdy, cl := row(cx, cy)
	det := prod(al, sub(prod(bdx, cdy), prod(cdx, bdy)))
	det.Add(det, prod(bl, sub(prod(cdx, ady), prod(adx, cdy))))
	det.Add(det, prod(cl, sub(prod(adx, bdy), prod(bdx, ady))))
	return det.Sign()
}
