// Logarithms and exponentials that give the same bits in every JavaScript engine. Math.log,
// Math.exp and Math.pow are "implementation-approximated" (ECMA-262 §21.3.2): engines and their
// versions differ in the last bit. Chrome 154 and Node 22 disagree on about 2% of logarithms and
// 10% of exponentials (found by MaveScape), so a result's hash would depend on the browser that
// computed it, and a project could stop reproducing after a browser update. These are fdlibm
// 5.3's __ieee754_log and __ieee754_exp (Sun Microsystems, 1993; "Permission to use, copy, modify,
// and distribute this software is freely granted, provided that this notice is preserved"),
// written with only +, −, ×, ÷ and bit operations, which IEEE 754 and ECMA-262 fix exactly: the
// error is below one unit in the last place, and the result is the same everywhere.
// Math.sqrt is correctly rounded by the standard and needs no replacement.
//
// Everything in the page that reaches a result, an export, QC or an example uses these (statistics,
// embeddings, clustering); drawing does not need to. Adapted from MaveScape 0.8.0 web/lib/dmath.js.

const buffer = new ArrayBuffer(8);
const f64 = new Float64Array(buffer);
const u32 = new Uint32Array(buffer);
// The high word's index: 1 on little-endian machines (all of SituCraft's platforms), 0 otherwise.
const HI = new Uint8Array(new Float64Array([1]).buffer)[7] === 0x3f ? 1 : 0;
const LO = 1 - HI;

const highWord = (x) => {
  f64[0] = x;
  return u32[HI] | 0;
};
const lowWord = (x) => {
  f64[0] = x;
  return u32[LO];
};
const withHighWord = (x, hi) => {
  f64[0] = x;
  u32[HI] = hi >>> 0;
  return f64[0];
};

const LN2_HI = 6.93147180369123816490e-01; // 0x3fe62e42 0xfee00000
const LN2_LO = 1.90821492927058770002e-10; // 0x3dea39ef 0x35793c76
const TWO54 = 1.80143985094819840000e+16;
const Lg1 = 6.666666666666735130e-01;
const Lg2 = 3.999999999940941908e-01;
const Lg3 = 2.857142874366239149e-01;
const Lg4 = 2.222219843214978396e-01;
const Lg5 = 1.818357216161805012e-01;
const Lg6 = 1.531383769920937332e-01;
const Lg7 = 1.479819860511658591e-01;

// The natural logarithm (fdlibm e_log.c).
export function log(x) {
  let hx = highWord(x);
  const lx = lowWord(x);
  let k = 0;
  if (hx < 0x00100000) { // x < 2^-1022
    if (((hx & 0x7fffffff) | lx) === 0) return -Infinity; // log(±0)
    if (hx < 0) return Number.NaN; // log(negative)
    k -= 54;
    x *= TWO54; // a subnormal number, scaled up
    hx = highWord(x);
  }
  if (hx >= 0x7ff00000) return x + x; // Infinity or NaN
  k += (hx >> 20) - 1023;
  hx &= 0x000fffff;
  let i = (hx + 0x95f64) & 0x100000;
  x = withHighWord(x, hx | (i ^ 0x3ff00000)); // x or x/2, normalized
  k += i >> 20;
  const f = x - 1.0;
  if ((0x000fffff & (2 + hx)) < 3) { // |f| < 2^-20
    if (f === 0) return k === 0 ? 0 : k * LN2_HI + k * LN2_LO;
    const R = f * f * (0.5 - 0.33333333333333333 * f);
    return k === 0 ? f - R : k * LN2_HI - ((R - k * LN2_LO) - f);
  }
  const s = f / (2.0 + f);
  const dk = k;
  const z = s * s;
  i = hx - 0x6147a;
  const w = z * z;
  const j = 0x6b851 - hx;
  const t1 = w * (Lg2 + w * (Lg4 + w * Lg6));
  const t2 = z * (Lg1 + w * (Lg3 + w * (Lg5 + w * Lg7)));
  i |= j;
  const R = t2 + t1;
  if (i > 0) {
    const hfsq = 0.5 * f * f;
    return k === 0 ? f - (hfsq - s * (hfsq + R)) : dk * LN2_HI - ((hfsq - (s * (hfsq + R) + dk * LN2_LO)) - f);
  }
  return k === 0 ? f - s * (f - R) : dk * LN2_HI - ((s * (f - R) - dk * LN2_LO) - f);
}

const O_THRESHOLD = 7.09782712893383973096e+02;
const U_THRESHOLD = -7.45133219101941108420e+02;
const INV_LN2 = 1.44269504088896338700e+00;
const TWO_M1000 = 9.33263618503218878990e-302; // 2^-1000
const P1 = 1.66666666666666019037e-01;
const P2 = -2.77777777770155933842e-03;
const P3 = 6.61375632143793436117e-05;
const P4 = -1.65339022054652515390e-06;
const P5 = 4.13813679705723846039e-08;

// e^x (fdlibm e_exp.c).
export function exp(x) {
  let hx = highWord(x);
  const xsb = (hx >>> 31) & 1;
  hx &= 0x7fffffff;
  if (hx >= 0x40862e42) { // |x| ≥ 709.78…
    if (hx >= 0x7ff00000) {
      if (((hx & 0xfffff) | lowWord(x)) !== 0) return x + x; // NaN
      return xsb === 0 ? x : 0; // exp(±Infinity)
    }
    if (x > O_THRESHOLD) return Infinity;
    if (x < U_THRESHOLD) return 0;
  }
  let hi = 0;
  let lo = 0;
  let k = 0;
  if (hx > 0x3fd62e42) { // |x| > ln2 / 2
    if (hx < 0x3ff0a2b2) { // and |x| < 1.5 ln2
      hi = xsb === 0 ? x - LN2_HI : x + LN2_HI;
      lo = xsb === 0 ? LN2_LO : -LN2_LO;
      k = 1 - xsb - xsb;
    } else {
      k = Math.trunc(INV_LN2 * x + (xsb === 0 ? 0.5 : -0.5));
      hi = x - k * LN2_HI; // exact
      lo = k * LN2_LO;
    }
    x = hi - lo;
  } else if (hx < 0x3e300000) { // |x| < 2^-28
    return 1 + x;
  }
  const t = x * x;
  const c = x - t * (P1 + t * (P2 + t * (P3 + t * (P4 + t * P5))));
  if (k === 0) return 1 - ((x * c) / (c - 2.0) - x);
  const y = 1 - ((lo - (x * c) / (2.0 - c)) - hi);
  if (k >= -1021) return withHighWord(y, highWord(y) + (k << 20));
  return withHighWord(y, highWord(y) + ((k + 1000) << 20)) * TWO_M1000;
}

const INV_LN10 = 0.43429448190325176; // 1 / ln 10, rounded

// log10(x), as log(x) / ln 10 (within two units in the last place; the same everywhere).
export function log10(x) {
  return log(x) * INV_LN10;
}

// x^y for x > 0 (exp(y log x); for the fractional powers of QC, not for exact integer powers).
export function pow(x, y) {
  if (y === 0) return 1;
  if (x === 1) return 1;
  return exp(y * log(x));
}

// x², exactly as x × x (the ** operator is Math.pow, which is implementation-approximated).
export const square = (x) => x * x;

const INV_SQRT_PI = 0.5641895835477563; // 1 / √π
const SQRT1_2 = 0.7071067811865476; // 1 / √2

// erfc(x), the complementary error function: by its Maclaurin series for |x| < 1 (erf, about 20
// terms), and beyond by its continued fraction (Lentz's method), which avoids the cancellation of
// 1 − erf in the tail. Relative error about 1e-15; only arithmetic and exp, so the same everywhere.
export function erfc(x) {
  if (Number.isNaN(x)) return x;
  if (x < 0) return 2 - erfc(-x);
  if (x < 1) {
    // erf(x) = 2/√π Σ (−1)ⁿ x^(2n+1) / (n! (2n + 1))
    const x2 = x * x;
    let term = x;
    let sum = x;
    for (let n = 1; n < 200; n += 1) {
      term *= -x2 / n;
      const add = term / (2 * n + 1);
      sum += add;
      if (Math.abs(add) < 1e-17 * Math.abs(sum)) break;
    }
    return 1 - 2 * INV_SQRT_PI * sum;
  }
  if (x > 27) return 0;
  // erfc(x) = exp(−x²)/√π · 1/(x + (1/2)/(x + 1/(x + (3/2)/(x + 2/(x + …)))))
  const tiny = 1e-300;
  let f = x;
  let c = x;
  let d = 0;
  for (let k = 1; k < 5000; k += 1) {
    const a = k / 2;
    d = x + a * d;
    d = Math.abs(d) < tiny ? tiny : d;
    c = x + a / c;
    c = Math.abs(c) < tiny ? tiny : c;
    d = 1 / d;
    const delta = c * d;
    f *= delta;
    if (Math.abs(delta - 1) < 1e-16) break;
  }
  return (exp(-x * x) * INV_SQRT_PI) / f;
}

// The standard normal distribution function Φ(z), and its upper tail 1 − Φ(z) without cancellation.
export const normalCdf = (z) => 0.5 * erfc(-z * SQRT1_2);
export const normalUpper = (z) => 0.5 * erfc(z * SQRT1_2);
// The standard normal density φ(z).
export const normalPdf = (z) => 0.3989422804014327 * exp(-0.5 * z * z);

// sin and cos of x (radians), [sin, cos], as internal/dmath's SinCos computes them, bit for bit:
// x reduced by multiples of π/2 (in two parts, Cody and Waite's reduction) to |r| ≤ π/4, then
// Taylor's series. NaN beyond 2²⁰·π/2.
const PIO2_HI = 1.57079632673412561417e+00;
const PIO2_LO = 6.07710050650619224932e-11;
const SIN_TAYLOR = [-1 / 6, 1 / 120, -1 / 5040, 1 / 362880, -1 / 39916800, 1 / 6227020800, -1 / 1307674368000, 1 / 355687428096000];
const COS_TAYLOR = [-1 / 2, 1 / 24, -1 / 720, 1 / 40320, -1 / 3628800, 1 / 479001600, -1 / 87178291200, 1 / 20922789888000, -1 / 6402373705728000];

function horner(z, c) {
  let h = c[c.length - 1];
  for (let i = c.length - 2; i >= 0; i -= 1) h = c[i] + z * h;
  return h;
}

export function sincos(x) {
  if (!Number.isFinite(x) || Math.abs(x) > 1647099) return [NaN, NaN];
  // Halves rounded away from zero, as Go's math.Round.
  const q = x / (Math.PI / 2);
  const k = q < 0 ? -Math.round(-q) : Math.round(q);
  const r = x - k * PIO2_HI - k * PIO2_LO;
  const z = r * r;
  const sin = r + r * z * horner(z, SIN_TAYLOR);
  const cos = 1 + z * horner(z, COS_TAYLOR);
  switch (k & 3) {
    case 0: return [sin, cos];
    case 1: return [cos, -sin];
    case 2: return [-sin, -cos];
    default: return [-cos, sin];
  }
}

// A checksum of sin and cos over 20,000 fixed arguments: SINCOS_FINGERPRINT in every engine and in
// Go (internal/dmath's test).
export const SINCOS_FINGERPRINT = '52c6523f';

export function sincosFingerprint() {
  let h = 0x811c9dc5;
  const mix = (v) => {
    f64[0] = v;
    for (const word of [u32[LO], u32[HI]]) {
      for (let b = 0; b < 32; b += 8) h = Math.imul(h ^ ((word >>> b) & 0xff), 0x01000193);
    }
  };
  for (let i = 0; i < 10000; i += 1) {
    const [s, c] = sincos(-2000 + i * 0.4001);
    mix(s);
    mix(c);
  }
  return (h >>> 0).toString(16).padStart(8, '0');
}

// A checksum of log, exp and pow over 30,000 fixed arguments (FNV-1a of their bits): FINGERPRINT in
// every engine, which the tests check in Node and validation/remote-session.mjs in the browser.
export const FINGERPRINT = '7bf3fe17';

export function fingerprint() {
  let h = 0x811c9dc5;
  const mix = (v) => {
    f64[0] = v;
    for (const word of [u32[LO], u32[HI]]) {
      for (let b = 0; b < 32; b += 8) h = Math.imul(h ^ ((word >>> b) & 0xff), 0x01000193);
    }
  };
  let s = 12345;
  for (let i = 0; i < 10000; i += 1) {
    s = (s * 48271) % 2147483647;
    let x = s / 2147483647;
    for (let j = s % 24; j > 0; j -= 1) x *= 10;
    for (let j = 0; j < 12; j += 1) x /= 10;
    mix(log(x));
    mix(exp((x % 1400) - 700));
    mix(pow(x, 0.37));
  }
  return (h >>> 0).toString(16).padStart(8, '0');
}
