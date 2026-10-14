// Seeded pseudo-random numbers. Every stochastic analysis in SituCraft's page (embeddings,
// clustering, permutation tests on cell tables) takes a seed and records it, so a result can be
// reproduced exactly.
// Adapted from MaveScape 0.8.0 web/lib/random.js.

import { exp, log } from './dmath.js';

// xoshiro128** (Blackman & Vigna), seeded through splitmix32. Returns floats in [0, 1).
export function createRandom(seed = 1) {
  let s = (Number(seed) >>> 0) || 0x9e3779b9;
  const splitmix = () => {
    s = (s + 0x9e3779b9) >>> 0;
    let z = s;
    z = Math.imul(z ^ (z >>> 16), 0x85ebca6b) >>> 0;
    z = Math.imul(z ^ (z >>> 13), 0xc2b2ae35) >>> 0;
    return (z ^ (z >>> 16)) >>> 0;
  };
  let a = splitmix(); let b = splitmix(); let c = splitmix(); let d = splitmix();
  const next = () => {
    const result = Math.imul(rotl(Math.imul(b, 5) >>> 0, 7), 9) >>> 0;
    const t = (b << 9) >>> 0;
    c ^= a; d ^= b; b ^= c; a ^= d;
    c ^= t;
    d = rotl(d, 11);
    return result;
  };
  const random = () => next() / 4294967296;
  random.uint32 = next;
  let spare = null;
  random.gaussian = () => {
    if (spare !== null) {
      const value = spare;
      spare = null;
      return value;
    }
    let u = 0; let v = 0; let r = 0;
    do {
      u = random() * 2 - 1;
      v = random() * 2 - 1;
      r = u * u + v * v;
    } while (r === 0 || r >= 1);
    const factor = Math.sqrt((-2 * log(r)) / r);
    spare = v * factor;
    return u * factor;
  };
  // An integer in [0, n).
  random.int = (n) => Math.floor(random() * n);
  return random;
}

function rotl(x, k) {
  return ((x << k) | (x >>> (32 - k))) >>> 0;
}

// Fisher–Yates shuffle in place.
// A Poisson count with mean lambda: Knuth's product of uniforms below 30, a rounded normal above
// (counts large enough for the normal to be close).
export function poisson(random, lambda) {
  if (!(lambda > 0)) return 0;
  if (lambda < 30) {
    const limit = exp(-lambda);
    let k = 0;
    let p = random();
    while (p > limit) {
      k += 1;
      p *= random();
    }
    return k;
  }
  return Math.max(0, Math.round(lambda + Math.sqrt(lambda) * random.gaussian()));
}

export function shuffle(array, random) {
  for (let i = array.length - 1; i > 0; i -= 1) {
    const j = random.int(i + 1);
    const t = array[i];
    array[i] = array[j];
    array[j] = t;
  }
  return array;
}

// k distinct indices from 0…n−1 (or from `indices`), sorted, chosen uniformly (Floyd's method for
// small k, partial shuffle otherwise).
export function sampleIndices(n, k, random, indices = null) {
  const total = indices ? indices.length : n;
  if (k >= total) return indices ? Uint32Array.from(indices) : Uint32Array.from({ length: n }, (_, i) => i);
  let picked;
  if (k < total / 8) {
    const set = new Set();
    for (let j = total - k; j < total; j += 1) {
      const t = random.int(j + 1);
      set.add(set.has(t) ? j : t);
    }
    picked = Uint32Array.from(set);
  } else {
    const all = Uint32Array.from({ length: total }, (_, i) => i);
    for (let i = 0; i < k; i += 1) {
      const j = i + random.int(total - i);
      const t = all[i];
      all[i] = all[j];
      all[j] = t;
    }
    picked = all.slice(0, k);
  }
  picked.sort();
  if (!indices) return picked;
  const out = new Uint32Array(picked.length);
  for (let i = 0; i < picked.length; i += 1) out[i] = indices[picked[i]];
  return out;
}

// A stable 32-bit hash of a string (FNV-1a), for deriving seeds from names.
export function hashString(text) {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}
