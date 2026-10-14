// The QC plots' numbers, without the DOM (tested in Node): histograms, box-plot statistics, 2-D
// bins for dense scatter plots, and selections from brushed ranges and rectangles.

import { log10, pow } from './dmath.js';

// Quantile q of sorted values, interpolated between neighbors (Hyndman & Fan 1996, type 7).
export function quantile(sorted, q) {
  if (!sorted.length) return NaN;
  const pos = (sorted.length - 1) * q;
  const lo = Math.floor(pos);
  const hi = Math.min(sorted.length - 1, lo + 1);
  return sorted[lo] + (sorted[hi] - sorted[lo]) * (pos - lo);
}

// The finite values (of the cells a mask keeps, when one is given), sorted.
export function finiteSorted(values, mask = null) {
  const out = [];
  for (let i = 0; i < values.length; i += 1) {
    if ((!mask || mask[i]) && Number.isFinite(values[i])) out.push(values[i]);
  }
  return Float64Array.from(out).sort();
}

// A histogram's bins over [lo, hi]: integer attributes with few values get one bin per integer;
// others `bins` equal bins, on a log10 scale when asked (values ≤ 0 then fall outside).
export function histogramBins(lo, hi, { bins = 40, integer = false, log = false } = {}) {
  if (!(hi > lo)) hi = lo + 1;
  if (integer && !log && hi - lo + 1 <= bins) {
    const edges = [];
    for (let v = lo; v <= hi + 1; v += 1) edges.push(v - 0.5);
    return { edges: Float64Array.from(edges), log: false };
  }
  if (log) {
    const a = log10(Math.max(lo, 1e-9));
    const b = log10(Math.max(hi, lo * 10, 1e-8));
    return { edges: Float64Array.from({ length: bins + 1 }, (_, k) => pow(10, a + ((b - a) * k) / bins)), log: true };
  }
  return { edges: Float64Array.from({ length: bins + 1 }, (_, k) => lo + ((hi - lo) * k) / bins), log: false };
}

// The bin a value falls in (the last bin includes its upper edge), or -1.
export function binOf(edges, v) {
  if (!(v >= edges[0]) || !(v <= edges[edges.length - 1])) return -1;
  let a = 0;
  let b = edges.length - 1;
  while (b - a > 1) {
    const m = (a + b) >> 1;
    if (v < edges[m]) b = m;
    else a = m;
  }
  return a;
}

// Counts per bin, of all cells and of the cells a mask marks.
export function histogram(values, edges, mask = null) {
  const counts = new Uint32Array(edges.length - 1);
  const marked = new Uint32Array(edges.length - 1);
  for (let i = 0; i < values.length; i += 1) {
    const k = binOf(edges, values[i]);
    if (k < 0) continue;
    counts[k] += 1;
    if (mask && mask[i]) marked[k] += 1;
  }
  return { counts, marked };
}

// Box-plot statistics of sorted values: quartiles, and whiskers at the most extreme values within
// 1.5 interquartile ranges of the box (Tukey).
export function boxStats(sorted) {
  const n = sorted.length;
  if (!n) return { n: 0 };
  const q1 = quantile(sorted, 0.25);
  const median = quantile(sorted, 0.5);
  const q3 = quantile(sorted, 0.75);
  const reach = 1.5 * (q3 - q1);
  let lo = q1;
  let hi = q3;
  for (const v of sorted) {
    if (v >= q1 - reach) {
      lo = v;
      break;
    }
  }
  for (let i = n - 1; i >= 0; i -= 1) {
    if (sorted[i] <= q3 + reach) {
      hi = sorted[i];
      break;
    }
  }
  let sum = 0;
  for (const v of sorted) sum += v;
  return { n, min: sorted[0], q1, median, q3, max: sorted[n - 1], whiskerLo: lo, whiskerHi: hi, mean: sum / n };
}

// Cells whose value lies in [lo, hi] (and, when given, that a mask keeps).
export function selectRange(values, lo, hi, mask = null) {
  const out = new Uint8Array(values.length);
  for (let i = 0; i < values.length; i += 1) {
    const v = values[i];
    if (v >= lo && v <= hi && (!mask || mask[i])) out[i] = 1;
  }
  return out;
}

// Cells whose (x, y) lies in a rectangle.
export function selectRect(xs, ys, x0, x1, y0, y1, mask = null) {
  const out = new Uint8Array(xs.length);
  const [ax, bx] = x0 <= x1 ? [x0, x1] : [x1, x0];
  const [ay, by] = y0 <= y1 ? [y0, y1] : [y1, y0];
  for (let i = 0; i < xs.length; i += 1) {
    if (xs[i] >= ax && xs[i] <= bx && ys[i] >= ay && ys[i] <= by && (!mask || mask[i])) out[i] = 1;
  }
  return out;
}

// Counts on an nx × ny grid over [x0, x1] × [y0, y1] (for scatter plots of many cells).
export function bin2d(xs, ys, nx, ny, x0, x1, y0, y1, mask = null) {
  const counts = new Uint32Array(nx * ny);
  const sx = nx / (x1 - x0 || 1);
  const sy = ny / (y1 - y0 || 1);
  for (let i = 0; i < xs.length; i += 1) {
    if (mask && !mask[i]) continue;
    const x = xs[i];
    const y = ys[i];
    if (!(x >= x0 && x <= x1 && y >= y0 && y <= y1)) continue;
    const bx = Math.min(nx - 1, Math.floor((x - x0) * sx));
    const by = Math.min(ny - 1, Math.floor((y - y0) * sy));
    counts[by * nx + bx] += 1;
  }
  return counts;
}

export function countMarked(mask) {
  let n = 0;
  for (let i = 0; i < mask.length; i += 1) n += mask[i] ? 1 : 0;
  return n;
}
