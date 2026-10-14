import assert from 'node:assert/strict';
import test from 'node:test';
import { erfc, exp, fingerprint, FINGERPRINT, log, log10, normalCdf, normalPdf, normalUpper, pow, square } from './dmath.js';

// Units in the last place between two doubles.
const ulps = (a, b) => {
  const i = new BigInt64Array(new Float64Array([a, b]).buffer);
  return Number(i[0] > i[1] ? i[0] - i[1] : i[1] - i[0]);
};

test('log and exp are within one unit in the last place of Math.log and Math.exp', () => {
  let s = 777;
  let worst = 0;
  for (let i = 0; i < 50000; i += 1) {
    s = (s * 48271) % 2147483647;
    let x = s / 2147483647;
    for (let j = s % 40; j > 0; j -= 1) x *= 10;
    for (let j = 0; j < 20; j += 1) x /= 10;
    worst = Math.max(worst, ulps(log(x), Math.log(x)), ulps(exp(Math.log(x)), Math.exp(Math.log(x))));
  }
  for (let c = 0; c < 2000; c += 1) worst = Math.max(worst, ulps(log(c + 0.5), Math.log(c + 0.5)));
  assert.ok(worst <= 1, `${worst} ulps`);
});

test('special values as IEEE 754 and Math define them', () => {
  assert.equal(log(1), 0);
  assert.equal(log(0), -Infinity);
  assert.equal(log(-0), -Infinity);
  assert.ok(Number.isNaN(log(-1)) && Number.isNaN(log(Number.NaN)));
  assert.equal(log(Infinity), Infinity);
  assert.equal(log(5e-324), Math.log(5e-324));
  assert.equal(exp(0), 1);
  assert.equal(exp(-Infinity), 0);
  assert.equal(exp(Infinity), Infinity);
  assert.equal(exp(710), Infinity);
  assert.equal(exp(-746), 0);
  assert.equal(exp(-740), Math.exp(-740)); // subnormal
  assert.ok(Number.isNaN(exp(Number.NaN)));
  assert.equal(log10(1000), 2.9999999999999996); // log(1000) / ln 10, as documented
  assert.equal(pow(7, 0), 1);
  assert.equal(pow(1, 1e300), 1);
  assert.equal(square(1.1), 1.1 * 1.1);
});

test('the normal distribution: Φ to 1e-13 against Python\'s math.erfc, in the body and both tails', () => {
  // 0.5 · erfc(−z/√2) from Python 3.12's math.erfc (libm).
  const values = [[0, 0.5], [1, 0.8413447460685429], [-1, 0.15865525393145707], [-1.5, 0.06680720126885809], [-2.82, 0.0024011824741892547], [-2.83, 0.0023274002067315545], [-2.9, 0.0018658133003840378], [-3, 0.0013498980316301035], [-5, 2.866515718791939e-7], [-8, 6.22096057427178e-16], [2.5, 0.9937903346742238]];
  for (const [z, want] of values) assert.ok(Math.abs(normalCdf(z) - want) <= 1e-13 * want, `Φ(${z}) = ${normalCdf(z)}, not ${want}`);
  assert.ok(Math.abs(normalUpper(5) - 2.866515718791939e-7) <= 1e-13 * 2.866515718791939e-7);
  assert.equal(erfc(0), 1);
  assert.equal(normalCdf(-40), 0);
  assert.ok(Math.abs(normalPdf(0) - 0.3989422804014327) < 1e-16);
});

// A change here means results change in their last bits: bump SCORING_VERSION.
test('the same bits as recorded: fingerprint', () => {
  assert.equal(fingerprint(), FINGERPRINT);
});
