import assert from 'node:assert/strict';
import test from 'node:test';
import { createRandom, hashString, sampleIndices, shuffle } from './random.js';

test('a seed gives the same sequence every time, and another seed another sequence', () => {
  const a = createRandom(20261008);
  const b = createRandom(20261008);
  const c = createRandom(20261009);
  const first = Array.from({ length: 64 }, () => a());
  assert.deepEqual(Array.from({ length: 64 }, () => b()), first);
  assert.notDeepEqual(Array.from({ length: 64 }, () => c()), first);
  assert.ok(first.every((x) => x >= 0 && x < 1));
});

test('uniform and Gaussian draws have the moments they should', () => {
  const random = createRandom(7);
  const n = 200000;
  let sum = 0;
  let sumSq = 0;
  for (let i = 0; i < n; i += 1) {
    const x = random();
    sum += x;
    sumSq += x * x;
  }
  const mean = sum / n;
  // Uniform on [0, 1): mean 1/2, variance 1/12; the tolerances are about 5 standard errors.
  assert.ok(Math.abs(mean - 0.5) < 5 * Math.sqrt(1 / 12 / n), `mean ${mean}`);
  assert.ok(Math.abs(sumSq / n - mean * mean - 1 / 12) < 0.002);
  let g = 0;
  let g2 = 0;
  for (let i = 0; i < n; i += 1) {
    const x = random.gaussian();
    g += x;
    g2 += x * x;
  }
  assert.ok(Math.abs(g / n) < 5 / Math.sqrt(n));
  assert.ok(Math.abs(g2 / n - 1) < 0.02);
});

test('integers stay in range and every value occurs', () => {
  const random = createRandom(3);
  const seen = new Uint32Array(6);
  for (let i = 0; i < 6000; i += 1) seen[random.int(6)] += 1;
  assert.ok(seen.every((count) => count > 800 && count < 1200), String(seen));
});

test('sampleIndices draws distinct sorted indices, by either method, from a range or a list', () => {
  for (const [n, k] of [[1000, 10], [1000, 600], [50, 50], [50, 80]]) {
    const picked = sampleIndices(n, k, createRandom(11));
    assert.equal(picked.length, Math.min(n, k));
    assert.equal(new Set(picked).size, picked.length);
    for (let i = 1; i < picked.length; i += 1) assert.ok(picked[i] > picked[i - 1]);
    assert.ok(picked.every((i) => i < n));
  }
  const from = Uint32Array.from([5, 9, 12, 40, 41, 77]);
  const subset = sampleIndices(from.length, 3, createRandom(2), from);
  assert.equal(subset.length, 3);
  assert.ok([...subset].every((i) => from.includes(i)));
});

test('shuffle permutes in place', () => {
  const array = Array.from({ length: 20 }, (_, i) => i);
  shuffle(array, createRandom(5));
  assert.deepEqual([...array].sort((a, b) => a - b), Array.from({ length: 20 }, (_, i) => i));
  assert.notDeepEqual(array, Array.from({ length: 20 }, (_, i) => i));
});

test('hashString is 32-bit FNV-1a (published test vectors)', () => {
  assert.equal(hashString(''), 0x811c9dc5);
  assert.equal(hashString('a'), 0xe40c292c);
  assert.equal(hashString('foobar'), 0xbf9cf968);
});
