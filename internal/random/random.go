// Package random gives seeded streams of pseudo-random numbers whose every draw is the same on
// every processor: the integers of PCG (math/rand/v2's generator, integer arithmetic alone),
// uniforms from their top 53 bits, and distributions computed with arithmetic, square roots and
// internal/dmath only, every product rounded on its own (a fused multiply-add, which compilers
// use on some processors, would round differently). Parallel work takes one stream per block,
// derived from the seed and the block's number, so results do not depend on the number of
// threads.
//
// The distributions:
//   - normal: Marsaglia's polar method (Marsaglia G, Bray TA. A convenient method for generating
//     normal variables. SIAM Rev 1964;6:260–264);
//   - gamma: Marsaglia and Tsang's method (Marsaglia G, Tsang WW. A simple method for generating
//     gamma variables. ACM Trans Math Softw 2000;26:363–372), with U^(1/a) for shapes below 1;
//   - Poisson: the product of uniforms (Knuth, The Art of Computer Programming, vol. 2, §3.4.1),
//     for means of 30 or less, sums of such draws above (a sum of Poisson variables is Poisson);
//   - negative binomial: a Poisson draw whose mean is gamma (the gamma–Poisson mixture);
//   - Chinese restaurant table: the tables n customers occupy in a Chinese restaurant process of
//     concentration r, a sum of Bernoulli draws (Zhou M, Carin L. Negative binomial process count
//     and mixture modeling. IEEE TPAMI 2015;37:307–320);
//   - Dirichlet: independent gamma draws, normalized.
package random

import (
	"math"
	"math/rand/v2"

	"situcraft/internal/dmath"
)

// Stream is one seeded stream.
type Stream struct {
	pcg      *rand.PCG
	spare    float64
	hasSpare bool
}

// New is the stream of a seed and a stream number (a block, a cell, a gene).
func New(seed, stream uint64) *Stream {
	return &Stream{pcg: rand.NewPCG(seed, mix(stream))}
}

// mix spreads a stream number over 64 bits (splitmix64's finalizer), so that neighbouring
// streams start far apart.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func mul(a, b float64) float64 { return float64(a * b) }

// Uint64 is the next 64 random bits.
func (s *Stream) Uint64() uint64 { return s.pcg.Uint64() }

// Float64 is uniform in [0, 1): the top 53 bits, scaled.
func (s *Stream) Float64() float64 { return float64(s.pcg.Uint64()>>11) * 0x1p-53 }

// open is uniform in (0, 1).
func (s *Stream) open() float64 {
	for {
		if u := s.Float64(); u > 0 {
			return u
		}
	}
}

// IntN is uniform in [0, n), without bias (n > 0).
func (s *Stream) IntN(n int) int {
	if n <= 0 {
		panic("random: IntN of n ≤ 0")
	}
	limit := ^uint64(0) - ^uint64(0)%uint64(n)
	for {
		if v := s.pcg.Uint64(); v < limit {
			return int(v % uint64(n))
		}
	}
}

// Normal is a standard normal draw.
func (s *Stream) Normal() float64 {
	if s.hasSpare {
		s.hasSpare = false
		return s.spare
	}
	for {
		u := mul(2, s.Float64()) - 1
		v := mul(2, s.Float64()) - 1
		r := mul(u, u) + mul(v, v)
		if r == 0 || r >= 1 {
			continue
		}
		f := math.Sqrt(mul(-2, dmath.Log(r)) / r)
		s.spare, s.hasSpare = mul(v, f), true
		return mul(u, f)
	}
}

// LogNormal is exp(mu + sigma Z).
func (s *Stream) LogNormal(mu, sigma float64) float64 {
	return dmath.Exp(mu + mul(sigma, s.Normal()))
}

// Exponential has mean 1.
func (s *Stream) Exponential() float64 { return -dmath.Log(s.open()) }

// Gamma has the shape and scale given (both positive).
func (s *Stream) Gamma(shape, scale float64) float64 {
	if !(shape > 0) || !(scale > 0) {
		return 0
	}
	if shape < 1 {
		// Gamma(a) = Gamma(a + 1) U^(1/a).
		return mul(s.Gamma(shape+1, scale), dmath.Exp(dmath.Log(s.open())/shape))
	}
	d := shape - 1.0/3
	c := 1 / math.Sqrt(mul(9, d))
	for {
		x := s.Normal()
		v := 1 + mul(c, x)
		if v <= 0 {
			continue
		}
		v = mul(mul(v, v), v)
		u := s.open()
		x2 := mul(x, x)
		if u < 1-mul(0.0331, mul(x2, x2)) {
			return mul(mul(d, v), scale)
		}
		if dmath.Log(u) < mul(0.5, x2)+mul(d, 1-v+dmath.Log(v)) {
			return mul(mul(d, v), scale)
		}
	}
}

// poissonChunk is the largest mean drawn by the product of uniforms (e^-30 is far from
// underflow).
const poissonChunk = 30.0

// Poisson is a Poisson draw of the mean given.
func (s *Stream) Poisson(mean float64) int {
	if !(mean > 0) {
		return 0
	}
	n := 0
	for mean > poissonChunk {
		n += s.poissonSmall(poissonChunk)
		mean -= poissonChunk
	}
	return n + s.poissonSmall(mean)
}

func (s *Stream) poissonSmall(mean float64) int {
	limit := dmath.Exp(-mean)
	k := 0
	p := s.Float64()
	for p > limit {
		k++
		p = mul(p, s.Float64())
	}
	return k
}

// NegativeBinomial is a draw of mean m and dispersion r (variance m + m²/r): Poisson of a
// gamma-distributed mean.
func (s *Stream) NegativeBinomial(m, r float64) int {
	if !(m > 0) {
		return 0
	}
	if !(r > 0) || math.IsInf(r, 1) {
		return s.Poisson(m)
	}
	return s.Poisson(s.Gamma(r, m/r))
}

// binomialDirect is the most trials a binomial draw counts one by one.
const binomialDirect = 64

// Binomial is the number of successes in n trials of probability p, exactly: counted trial by
// trial for few trials, else by recursion on order statistics. The a-th smallest of n uniforms is
// Beta(a, n − a + 1); the trials below it are uniform under it and those above it uniform above
// it, so one beta draw halves the trials left (Devroye L. Non-Uniform Random Variate Generation.
// Springer, 1986, ch. X.4).
func (s *Stream) Binomial(n int, p float64) int {
	if n <= 0 || !(p > 0) {
		return 0
	}
	if p >= 1 {
		return n
	}
	k := 0
	for n > binomialDirect {
		a := 1 + n/2
		b := n + 1 - a
		g1, g2 := s.Gamma(float64(a), 1), s.Gamma(float64(b), 1)
		x := g1 / (g1 + g2)
		if x >= p {
			n, p = a-1, p/x
		} else {
			k += a
			n, p = b-1, (p-x)/(1-x)
		}
	}
	for i := 0; i < n; i++ {
		if s.Float64() < p {
			k++
		}
	}
	return k
}

// CRT is a draw of the Chinese restaurant table distribution: the number of tables n customers
// occupy when customer j sits at a new table with probability r/(r + j − 1).
func (s *Stream) CRT(n int, r float64) int {
	tables := 0
	for j := 1; j <= n; j++ {
		if s.Float64() < r/(r+float64(j-1)) {
			tables++
		}
	}
	return tables
}

// Dirichlet fills out (as long as alpha) with a draw of the Dirichlet distribution of parameters
// alpha, each above 0.
func (s *Stream) Dirichlet(alpha, out []float64) {
	var sum float64
	for k, a := range alpha {
		out[k] = s.Gamma(a, 1)
		sum += out[k]
	}
	if sum == 0 {
		// Every draw underflowed (parameters far below 1): the mass goes to one component, chosen
		// in proportion to the parameters, which is the distribution's limit.
		var total float64
		for _, a := range alpha {
			total += a
		}
		u := mul(s.Float64(), total)
		for k, a := range alpha {
			out[k] = 0
			if u >= 0 && u < a {
				out[k] = 1
			}
			u -= a
		}
		return
	}
	for k := range out[:len(alpha)] {
		out[k] /= sum
	}
}
