// A spatial analysis's results in the Spatial view (ui/mode-spatial.js): heat maps of pairs of labels
// (an enrichment's ratio of neighbors found to expected; nearest distances' median against chance's),
// on a diverging scale with the significant marked, and tables of the strongest; a co-occurrence's
// curves, one label's ratio to each label's over the distances; nearest distances' distribution and
// Ripley's L, each with its envelope from the permutations; regions compared, their densities and
// shares with intervals and two rows' ratios as a forest plot; a label's profile across a region's
// edge, and each label's core, margin and surroundings compared; niches, their make-up against the
// tissue's as a heat map, and each one's defining labels and leading genes; spatially variable
// genes, ranked by Moran's I. Each pair, label, region, niche or gene can be chosen, and its cells
// shown on the tissue.

import { h, formatCount } from './dom.js';
import { divergingColor, nicheNames, rankPairs, rowNames } from '../lib/spatial.js';
import { categoricalColor } from '../lib/colormaps.js';

const SVG = 'http://www.w3.org/2000/svg';
function svg(tag, attrs = {}, ...children) {
  const el = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) if (v !== null && v !== undefined) el.setAttribute(k, String(v));
  for (const c of children) if (c) el.append(c);
  return el;
}

const fixed = (v, d = 2) => (Number.isFinite(v) ? v.toFixed(d) : '—');
const exact = (v) => (Number.isFinite(v) ? (Math.round(v * 100) / 100).toLocaleString('en-US') : '—');
export const pvalue = (p) => (!Number.isFinite(p) ? '—' : p < 1e-3 ? '< 0.001' : p.toFixed(3));
export const times = (r) => (!Number.isFinite(r) ? '—' : r === 0 ? 'none' : r >= 10 ? `${r.toFixed(0)}×` : `${r.toFixed(2)}×`);
const log2 = (v) => Math.log(v) / Math.LN2;

// A pair in words: "T cells – Tumor".
export const pairName = (names, a, b) => `${names[a]} – ${names[b]}`;

// A heat map of pairs of labels: rows and columns the labels, each square colored by value(a, b) (a
// log₂ ratio: red above 0, blue below, clamped at ±2), a dot where significant(a, b). onPick(a, b)
// when one is chosen (ordered pairs when not symmetric); chosen: [a, b] or null.
function pairHeatmap({ names, colors, value, title, significant, onPick, chosen = null, symmetric = true, label, scale }) {
  const K = names.length;
  const cell = Math.max(10, Math.min(40, Math.floor(420 / K)));
  const longest = Math.max(...names.map((n) => n.length));
  const margin = Math.min(170, 12 + 6.2 * longest);
  const size = margin + K * cell + 8;
  const root = svg('svg', { class: 'sp-heatmap', width: size, height: size, viewBox: `0 0 ${size} ${size}`, role: 'img', 'aria-label': label });
  names.forEach((name, k) => {
    const color = colors?.[k] ?? null;
    root.append(svg('text', { x: margin - 6, y: margin + k * cell + cell / 2 + 3.5, 'text-anchor': 'end', class: 'sp-axis' }, document.createTextNode(name)));
    root.append(svg('text', { x: 0, y: 0, transform: `translate(${margin + k * cell + cell / 2 + 3.5}, ${margin - 6}) rotate(-60)`, class: 'sp-axis' }, document.createTextNode(name)));
    if (color) {
      root.append(svg('rect', { x: margin - 4, y: margin + k * cell + 2, width: 3, height: cell - 4, fill: color }));
      root.append(svg('rect', { x: margin + k * cell + 2, y: margin - 4, width: cell - 4, height: 3, fill: color }));
    }
  });
  for (let a = 0; a < K; a += 1) {
    for (let b = 0; b < K; b += 1) {
      const v = value(a, b);
      const rect = svg('rect', { x: margin + b * cell, y: margin + a * cell, width: cell - 1, height: cell - 1, fill: Number.isFinite(v) || v === -Infinity ? divergingColor(v) : 'var(--panel-2)', class: 'sp-square', 'data-pair': `${a},${b}` });
      rect.append(svg('title', {}, document.createTextNode(title(a, b))));
      rect.addEventListener('click', () => (symmetric ? onPick(Math.min(a, b), Math.max(a, b)) : onPick(a, b)));
      root.append(rect);
      if (significant(a, b)) root.append(svg('circle', { cx: margin + b * cell + cell / 2 - 0.5, cy: margin + a * cell + cell / 2 - 0.5, r: Math.max(1.5, cell / 9), class: 'sp-significant', 'pointer-events': 'none' }));
      const hit = chosen && ((chosen[0] === a && chosen[1] === b) || (symmetric && chosen[0] === b && chosen[1] === a));
      if (hit) root.append(svg('rect', { x: margin + b * cell - 1, y: margin + a * cell - 1, width: cell + 1, height: cell + 1, class: 'sp-chosen-square', 'pointer-events': 'none' }));
    }
  }
  const bar = h('div.sp-scale', h('span', scale[0]),
    h('span.sp-scale-bar', { style: { background: `linear-gradient(to right, ${[-2, -1, -0.5, 0, 0.5, 1, 2].map((v) => divergingColor(v)).join(', ')})` } }),
    h('span', scale[1]),
    h('span.muted', ` (${scale[2]}; a dot: adjusted p below 0.05)`));
  return h('div.sp-heatmap-wrap', root, bar);
}

// The heat map of an enrichment: each pair's ratio of neighbors found to expected (red more, blue
// fewer, clamped at four times).
export function enrichmentHeatmap(analysis, { onPick, chosen = null }) {
  const names = analysis.labels.names;
  const K = names.length;
  const e = analysis.enrichment;
  const ratio = (a, b) => {
    const q = a * K + b;
    return e.expected[q] > 0 ? e.count[q] / e.expected[q] : NaN;
  };
  return pairHeatmap({
    names, colors: analysis.labels.colors, chosen, onPick,
    value: (a, b) => log2(ratio(a, b)),
    significant: (a, b) => (e.padj[a * K + b] ?? NaN) < 0.05,
    title: (a, b) => {
      const q = a * K + b;
      return `${pairName(names, a, b)}: ${formatCount(e.count[q])} found, ${formatCount(Math.round(e.expected[q] ?? NaN))} expected (${times(ratio(a, b))}); z ${e.z[q] === null ? '—' : fixed(e.z[q], 1)}, adjusted p ${pvalue(e.padj[q] ?? NaN)}`;
    },
    label: `The ratio of neighbors found to expected for each pair of ${K} labels; the table below lists the strongest`,
    scale: ['fewer', 'more neighbors than expected', '¼× to 4×'],
  });
}

// The heat map of nearest distances: from each row's label to each column's, the median distance over
// chance's (red nearer, blue farther, clamped at four times).
export function distancesHeatmap(analysis, { onPick, chosen = null }) {
  const names = analysis.labels.names;
  const K = names.length;
  const r = analysis.nearest;
  const ratio = (a, b) => {
    const q = a * K + b;
    return r.permMean[q] > 0 ? r.median[q] / r.permMean[q] : NaN;
  };
  return pairHeatmap({
    names, colors: analysis.labels.colors, chosen, onPick, symmetric: false,
    value: (a, b) => -log2(ratio(a, b)),
    significant: (a, b) => (r.padj[a * K + b] ?? NaN) < 0.05,
    title: (a, b) => {
      const q = a * K + b;
      return `From ${names[a]} to the nearest ${names[b]}: median ${fixed(r.median[q], 1)} µm, chance ${fixed(r.permMean[q], 1)} µm (${times(ratio(a, b))}); z ${r.z[q] === null ? '—' : fixed(r.z[q], 1)}, adjusted p ${pvalue(r.padj[q] ?? NaN)}`;
    },
    label: `From each of ${K} labels' cells to the nearest cell of each label, the median distance against chance's; the table below lists the strongest`,
    scale: ['farther', 'nearer than chance', 'rows from, columns to; ¼× to 4×'],
  });
}

// The pairs nearer and farther than chance by the ratio of medians, among those whose adjusted p is
// below 0.05, up to `top` each; onPick(a, b).
export function distancesTable(analysis, { onPick, top = 8 }) {
  const names = analysis.labels.names;
  const K = names.length;
  const r = analysis.nearest;
  const pairs = [];
  for (let q = 0; q < K * K; q += 1) {
    if ((r.padj[q] ?? NaN) < 0.05 && Number.isFinite(r.z[q])) pairs.push({ a: Math.floor(q / K), b: q % K, median: r.median[q], chance: r.permMean[q], z: r.z[q], padj: r.padj[q] });
  }
  // Among the significant, the strongest effects first: by the ratio of medians.
  pairs.sort((x, y) => x.median / x.chance - y.median / y.chance);
  const nearer = pairs.filter((p) => p.z < 0).slice(0, top);
  const farther = pairs.filter((p) => p.z > 0).reverse().slice(0, top);
  const row = (p) => h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, 'data-pair': `${p.a},${p.b}`, onclick: () => onPick(p.a, p.b), onkeydown: (event) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onPick(p.a, p.b);
    }
  } },
  h('td', `${names[p.a]} → ${names[p.b]}`), h('td.r', `${fixed(p.median, 1)} µm`), h('td.r', `${fixed(p.chance, 1)} µm`), h('td.r', fixed(p.z, 1)), h('td.r', pvalue(p.padj)));
  const table = (list, caption) => (list.length ? h('div', h('div.section-title', caption), h('div.table-wrap', h('table.data-table.seg-table.sp-pairs',
    h('thead', h('tr', h('th', 'From → to the nearest'), h('th.r', 'Median'), h('th.r', 'Chance'), h('th.r', 'z'), h('th.r', 'Adjusted p'))),
    h('tbody', ...list.map(row))))) : null);
  return h('div.sp-pair-tables',
    table(nearer, 'Nearer than chance'),
    table(farther, 'Farther than chance'),
    !nearer.length && !farther.length ? h('p.muted.seg-note', 'No pair lies nearer or farther than dealing the labels at random gives (adjusted p below 0.05).') : null);
}

// A curve with its envelope: x, y, the permutations' 2.5% and 97.5% (lo, hi) and mean, a reference
// line (ref, or none), axes' labels; y shifted by shift(x) (L − r).
function envelopeChart({ x, y, lo, hi, mean, ref = null, xLabel, yLabel, color, label, shift = () => 0, percent = false }) {
  const W = 560;
  const H = 260;
  const M = { left: 52, right: 12, top: 10, bottom: 34 };
  const shifted = (vs) => vs.map((v, i) => v - shift(x[i]));
  const vals = [...shifted(y), ...shifted(lo), ...shifted(hi), ...shifted(mean), ...(ref ? shifted(x.map(ref)) : [])].filter(Number.isFinite);
  let ymin = Math.min(...vals);
  let ymax = Math.max(...vals);
  if (!(ymax > ymin)) {
    ymin -= 1;
    ymax += 1;
  }
  const pad = (ymax - ymin) * 0.06;
  ymin -= pad;
  ymax += pad;
  const sx = (v) => M.left + ((v - 0) / (x.at(-1) - 0)) * (W - M.left - M.right);
  const sy = (v) => M.top + (1 - (v - ymin) / (ymax - ymin)) * (H - M.top - M.bottom);
  const root = svg('svg', { class: 'sp-chart', viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': label });
  const step = (ymax - ymin) / 4;
  const mag = 10 ** Math.floor(Math.log10(step || 1));
  const tick = [1, 2, 5, 10].map((m) => m * mag).find((t) => t >= step) ?? 10 * mag;
  for (let v = Math.ceil(ymin / tick) * tick; v <= ymax; v += tick) {
    root.append(svg('line', { x1: M.left, x2: W - M.right, y1: sy(v), y2: sy(v), class: 'sp-grid' }));
    root.append(svg('text', { x: M.left - 6, y: sy(v) + 3.5, 'text-anchor': 'end', class: 'sp-axis' }, document.createTextNode(percent ? `${Math.round(100 * v)}%` : String(Math.round(v * 100) / 100))));
  }
  for (const v of x.filter((_, i) => x.length <= 10 || i % Math.ceil(x.length / 8) === Math.ceil(x.length / 8) - 1)) {
    root.append(svg('text', { x: sx(v), y: H - M.bottom + 16, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(String(v))));
  }
  root.append(svg('text', { x: (M.left + W - M.right) / 2, y: H - 4, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(xLabel)));
  root.append(svg('text', { x: 0, y: 0, transform: `translate(12, ${(M.top + H - M.bottom) / 2}) rotate(-90)`, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(yLabel)));
  const path = (vs) => x.map((v, i) => [sx(v), vs[i] - shift(v)]).filter(([, v]) => Number.isFinite(v)).map(([px, v], i) => `${i ? 'L' : 'M'}${px.toFixed(1)},${sy(v).toFixed(1)}`).join('');
  const band = x.map((v, i) => [sx(v), lo[i] - shift(v), hi[i] - shift(v)]).filter(([, a, b]) => Number.isFinite(a) && Number.isFinite(b));
  if (band.length) root.append(svg('path', { d: `${band.map(([px, a], i) => `${i ? 'L' : 'M'}${px.toFixed(1)},${sy(a).toFixed(1)}`).join('')}${band.slice().reverse().map(([px, , b]) => `L${px.toFixed(1)},${sy(b).toFixed(1)}`).join('')}Z`, class: 'sp-envelope' }));
  if (ref) root.append(svg('path', { d: path(x.map((v) => ref(v))), class: 'sp-one' }));
  root.append(svg('path', { d: path(mean), class: 'sp-mean' }));
  root.append(svg('path', { d: path(y), stroke: color ?? 'var(--accent)', class: 'sp-line chosen' }));
  // The key's lines drawn as the chart draws them, so a dashed line and a dotted one tell apart.
  const swatch = (cls) => svg('svg', { class: 'sp-keyline', viewBox: '0 0 22 8', 'aria-hidden': 'true' }, svg('line', { x1: 1, x2: 21, y1: 4, y2: 4, class: cls }));
  return h('div.sp-chart-wrap', root, h('div.sp-scale', h('span.sp-key.observed', { style: { background: color ?? 'var(--accent)' } }), 'this section',
    h('span.sp-key.band'), 'chance: the permutations\' 2.5% to 97.5%', swatch('sp-mean'), 'their mean', ref ? [swatch('sp-one'), 'points at random'] : null));
}

// Nearest distances' distribution for a pair: the share of A's cells whose nearest B lies within each
// distance, with chance's envelope.
export function distancesChart(analysis, a, b) {
  const r = analysis.nearest;
  const K = analysis.labels.names.length;
  const D = r.distances.length;
  const q = (a * K + b) * D;
  const sl = (v) => v.slice(q, q + D).map((x) => x ?? NaN);
  return envelopeChart({ x: r.distances, y: sl(r.cdf), lo: sl(r.lo), hi: sl(r.hi), mean: sl(r.meanCdf), xLabel: 'distance to the nearest (µm)', yLabel: 'share of the cells', color: analysis.labels.colors?.[b], percent: true,
    label: `The share of ${analysis.labels.names[a]} cells whose nearest ${analysis.labels.names[b]} lies within each distance, with chance's envelope` });
}

// Ripley's L for a pair, as L(r) − r (0 for points at random), with chance's envelope.
export function ripleyChart(analysis, a, b) {
  const r = analysis.ripley;
  const K = analysis.labels.names.length;
  const R = r.radii.length;
  const q = (a * K + b) * R;
  const sl = (v) => v.slice(q, q + R).map((x) => x ?? NaN);
  return envelopeChart({ x: r.radii, y: sl(r.l), lo: sl(r.lo), hi: sl(r.hi), mean: sl(r.mean), ref: (v) => v, shift: (v) => v, xLabel: 'r (µm)', yLabel: 'L(r) − r (µm)', color: analysis.labels.colors?.[b],
    label: `Ripley's L of ${analysis.labels.names[a]} and ${analysis.labels.names[b]}, less r, with chance's envelope` });
}

// The pairs whose L leaves its envelope by the global test (adjusted p below 0.05), above and below,
// up to `top` each, with the radius where they stray farthest; onPick(a, b).
export function ripleyTable(analysis, { onPick, top = 8 }) {
  const names = analysis.labels.names;
  const K = names.length;
  const r = analysis.ripley;
  const R = r.radii.length;
  const rows = [];
  for (let q = 0; q < K * K; q += 1) {
    if (!((r.padj[q] ?? NaN) < 0.05)) continue;
    let best = -1;
    let at = 0;
    for (let k = 0; k < R; k += 1) {
      const d = Math.abs((r.l[q * R + k] ?? NaN) - (r.mean[q * R + k] ?? NaN));
      if (d > best) {
        best = d;
        at = k;
      }
    }
    rows.push({ a: Math.floor(q / K), b: q % K, radius: r.radii[at], dev: r.l[q * R + at] - r.mean[q * R + at], padj: r.padj[q] });
  }
  rows.sort((x, y) => Math.abs(y.dev) - Math.abs(x.dev));
  const row = (p) => h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, 'data-pair': `${p.a},${p.b}`, onclick: () => onPick(p.a, p.b), onkeydown: (event) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onPick(p.a, p.b);
    }
  } },
  h('td', p.a === p.b ? names[p.a] : `${names[p.a]} → ${names[p.b]}`), h('td.r', `${p.radius} µm`), h('td.r', `${p.dev > 0 ? '+' : ''}${fixed(p.dev, 1)} µm`), h('td.r', pvalue(p.padj)));
  const table = (list, caption) => (list.length ? h('div', h('div.section-title', caption), h('div.table-wrap', h('table.data-table.seg-table.sp-pairs',
    h('thead', h('tr', h('th', 'Label, or from → to'), h('th.r', 'Farthest at'), h('th.r', 'L over chance'), h('th.r', 'Adjusted p'))),
    h('tbody', ...list.map(row))))) : null);
  const above = rows.filter((p) => p.dev > 0).slice(0, top);
  const below = rows.filter((p) => p.dev < 0).slice(0, top);
  return h('div.sp-pair-tables',
    table(above, 'Gathered, or drawn together'),
    table(below, 'Spread out, or kept apart'),
    !above.length && !below.length ? h('p.muted.seg-note', 'No label\'s L leaves the envelope of the labels dealt at random (adjusted p below 0.05).') : null);
}

// Each label's neighbors by label, as shares of all its neighbors (by the boundary they share, in a
// contact graph weighed by it: the contact fractions): the found counts, each row divided by its sum.
export function sharesTable(analysis) {
  const names = analysis.labels.names;
  const K = names.length;
  const e = analysis.enrichment;
  const boundary = analysis.parameters?.weight === 'boundary';
  const rows = names.map((name, a) => {
    let sum = 0;
    for (let b = 0; b < K; b += 1) sum += e.count[a * K + b] ?? 0;
    return h('tr', h('th', { scope: 'row' }, name), ...names.map((_, b) => {
      const v = sum > 0 ? (e.count[a * K + b] ?? 0) / sum : NaN;
      return h('td.r', Number.isFinite(v) ? `${(100 * v).toFixed(v < 0.1 ? 1 : 0)}%` : '—');
    }));
  });
  return h('details.sp-shares',
    h('summary', boundary ? 'Contact fractions: each label\'s boundary, by the label it touches' : 'Each label\'s neighbors, by label'),
    h('div.table-wrap', h('table.data-table.seg-table',
      h('thead', h('tr', h('th', ''), ...names.map((n) => h('th.r', n)))),
      h('tbody', ...rows))),
    h('p.muted.seg-note', boundary ? 'Of the boundary each label\'s cells share with other cells (not with space no cell holds), the share each label holds. Rows sum to 100%.' : 'Of each label\'s cells\' neighbors, the share of each label (an edge within a label counts from both its cells). Rows sum to 100%.'));
}

// The most enriched and most depleted pairs by their ratio, among those whose adjusted p is below
// 0.05, up to `top` each, as a table; onPick(a, b) when a row is chosen.
export function pairsTable(analysis, { onPick, top = 8 }) {
  const names = analysis.labels.names;
  // Among the significant, the strongest effects first: by the ratio.
  const pairs = rankPairs(analysis).filter((p) => p.padj < 0.05 && Number.isFinite(p.z)).sort((x, y) => y.ratio - x.ratio);
  const enriched = pairs.filter((p) => p.z > 0).slice(0, top);
  const depleted = pairs.filter((p) => p.z < 0).reverse().slice(0, top);
  const row = (p) => h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, 'data-pair': `${p.a},${p.b}`, onclick: () => onPick(p.a, p.b), onkeydown: (event) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onPick(p.a, p.b);
    }
  } },
  h('td', pairName(names, p.a, p.b)), h('td.r', exact(p.count)), h('td.r', exact(Math.round(p.expected))), h('td.r', times(p.ratio)), h('td.r', fixed(p.z, 1)), h('td.r', pvalue(p.padj)));
  const table = (list, caption) => (list.length ? h('div', h('div.section-title', caption), h('div.table-wrap', h('table.data-table.seg-table.sp-pairs',
    h('thead', h('tr', h('th', 'Pair'), h('th.r', 'Found'), h('th.r', 'Expected'), h('th.r', 'Ratio'), h('th.r', 'z'), h('th.r', 'Adjusted p'))),
    h('tbody', ...list.map(row))))) : null);
  return h('div.sp-pair-tables',
    table(enriched, 'Neighbors more often than expected'),
    table(depleted, 'Neighbors less often than expected'),
    !enriched.length && !depleted.length ? h('p.muted.seg-note', 'No pair differs from what dealing the labels at random gives (adjusted p below 0.05).') : null);
}

// A co-occurrence's curves: from the cells of label `from`, each label's ratio over the distances
// (log scale, 1 marked), its color the label's; onPick(b) when a label is chosen, chosen the one.
export function cooccurrenceChart(analysis, { from, chosen = null, onPick }) {
  const names = analysis.labels.names;
  const K = names.length;
  const co = analysis.cooccurrence;
  const d = co.distances;
  const W = 560;
  const H = 260;
  const M = { left: 48, right: 12, top: 10, bottom: 34 };
  let lo = 0.25;
  let hi = 4;
  for (let k = 0; k < d.length; k += 1) {
    for (let b = 0; b < K; b += 1) {
      const r = co.ratio[k][from * K + b];
      if (r > 0) {
        lo = Math.min(lo, r);
        hi = Math.max(hi, r);
      }
    }
  }
  lo = Math.max(lo, 1 / 64);
  hi = Math.min(hi, 64);
  const sx = (v) => M.left + ((v - 0) / (d.at(-1) - 0)) * (W - M.left - M.right);
  const sy = (r) => M.top + (1 - (log2(Math.max(lo, Math.min(hi, r))) - log2(lo)) / (log2(hi) - log2(lo))) * (H - M.top - M.bottom);
  const root = svg('svg', { class: 'sp-chart', viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': `How much likelier each label's cells are within each distance of ${names[from]}'s than of any cell` });
  for (let e = Math.ceil(log2(lo)); e <= Math.floor(log2(hi)); e += 1) {
    const y = sy(2 ** e);
    root.append(svg('line', { x1: M.left, x2: W - M.right, y1: y, y2: y, class: e === 0 ? 'sp-one' : 'sp-grid' }));
    root.append(svg('text', { x: M.left - 6, y: y + 3.5, 'text-anchor': 'end', class: 'sp-axis' }, document.createTextNode(e >= 0 ? `${2 ** e}×` : `1/${2 ** -e}`)));
  }
  for (const v of d.filter((_, i) => d.length <= 10 || i % Math.ceil(d.length / 8) === Math.ceil(d.length / 8) - 1)) {
    root.append(svg('text', { x: sx(v), y: H - M.bottom + 16, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(String(v))));
  }
  root.append(svg('text', { x: (M.left + W - M.right) / 2, y: H - 4, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(co.pairs === 'between' ? 'distance band, up to (µm)' : 'within (µm)')));
  for (let b = 0; b < K; b += 1) {
    const pts = d.map((v, k) => [sx(v), co.ratio[k][from * K + b]]).filter(([, r]) => Number.isFinite(r) && r > 0);
    if (!pts.length) continue;
    const color = analysis.labels.colors?.[b] ?? 'var(--text-2)';
    const path = svg('path', { d: pts.map(([x, r], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${sy(r).toFixed(1)}`).join(''), stroke: color, class: `sp-line${chosen === b ? ' chosen' : ''}`, 'data-label': b });
    path.append(svg('title', {}, document.createTextNode(names[b])));
    path.addEventListener('click', () => onPick(b));
    root.append(path);
  }
  const legend = h('div.sp-legend', ...names.map((name, b) => h(`button.sp-legend-item${chosen === b ? '.chosen' : ''}`, { type: 'button', onclick: () => onPick(b) },
    h('span.sp-swatch', { style: { background: analysis.labels.colors?.[b] ?? 'var(--text-2)' } }), name)));
  return h('div.sp-chart-wrap', root, legend);
}

// --- Regions compared and profiles across an edge ---

// A number and its interval as analysis.json writes them: null for NaN, and for an upper limit (or
// a ratio, its lower limit above 0) beyond any finite one.
function interval(v, lo, hi) {
  const L = lo ?? NaN;
  const H = hi ?? (Number.isFinite(L) ? Infinity : NaN);
  const V = v ?? (L > 0 ? Infinity : NaN);
  return [V, L, H];
}

const density = (v) => (!Number.isFinite(v) ? (v === Infinity ? '∞' : '—') : v >= 10 ? Math.round(v).toLocaleString('en-US') : v.toFixed(1));
const percent = (v) => (!Number.isFinite(v) ? '—' : `${(100 * v).toFixed(v < 0.1 ? 1 : 0)}%`);
const ratio = (v) => (v === Infinity ? '∞' : !Number.isFinite(v) ? '—' : v >= 10 ? v.toFixed(0) : v.toFixed(2));
export const ratioText = (v, lo, hi) => (Number.isFinite(lo) ? `${ratio(v)}× (${ratio(lo)} to ${ratio(hi)})` : '—');

const pickable = (attrs, onPick) => ({ ...attrs, tabIndex: 0, style: { cursor: 'pointer' }, onclick: onPick, onkeydown: (event) => {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    onPick();
  }
} });

// Regions compared: a row for each label, a column for each region (and the tissue in none), each
// the label's density (cells per mm² of tissue) or share of the labeled cells, with its interval;
// onPick(k) when a label is chosen.
export function regionsTable(analysis, { measure = 'density', onPick, chosen = null }) {
  const r = analysis.regions;
  const names = analysis.labels.names;
  const K = names.length;
  const rows = rowNames(r);
  const value = (row, k) => {
    const q = row * K + k;
    return measure === 'density' ? interval(r.density[q], r.densityLo[q], r.densityHi[q]) : interval(r.share[q], r.shareLo[q], r.shareHi[q]);
  };
  const show = measure === 'density' ? density : percent;
  return h('div.table-wrap', h('table.data-table.seg-table.sp-regions-table',
    h('thead', h('tr', h('th', 'Label'), ...rows.map((name, row) => h('th.r', name, h('div.muted.sp-sub', `${(r.area[row] / 1e6).toFixed(3)} mm² · ${formatCount(r.labeled[row])} cells`))))),
    h('tbody', ...names.map((name, k) => h(`tr${chosen === k ? '.selected' : ''}`, pickable({ 'data-label': k }, () => onPick(k)),
      h('th', { scope: 'row' }, h('span.sp-swatch', { style: { background: analysis.labels.colors?.[k] ?? 'var(--text-3)' } }), name),
      ...rows.map((_, row) => {
        const [v, lo, hi] = value(row, k);
        return h('td.r', show(v), h('div.muted.sp-sub', Number.isFinite(lo) ? `${show(lo)} to ${show(hi)}` : ''));
      }))))));
}

// Two rows of regions compared, the first's over the second's, for each label: the ratio and its
// interval on a log scale, 1 marked; onPick(k).
export function ratioForest(analysis, { r: a, s: b, measure = 'density', onPick, chosen = null }) {
  const reg = analysis.regions;
  const names = analysis.labels.names;
  const K = names.length;
  const R = reg.area.length;
  const items = names.map((_, k) => {
    const q = (a * R + b) * K + k;
    return measure === 'density' ? interval(reg.densityRatio[q], reg.densityRatioLo[q], reg.densityRatioHi[q]) : interval(reg.shareRatio[q], reg.shareRatioLo[q], reg.shareRatioHi[q]);
  });
  let lo = 0.5;
  let hi = 2;
  for (const [v, l, u] of items) {
    for (const x of [v, l, u]) {
      if (Number.isFinite(x) && x > 0) {
        lo = Math.min(lo, x);
        hi = Math.max(hi, x);
      }
    }
  }
  lo = Math.max(lo, 1 / 64);
  hi = Math.min(hi, 64);
  const longest = Math.max(...names.map((n) => n.length));
  const M = { left: Math.min(170, 14 + 6.2 * longest), right: 16, top: 8, bottom: 30 };
  const row = 20;
  const W = 560;
  const H = M.top + M.bottom + row * K;
  const sx = (v) => M.left + (log2(Math.max(lo, Math.min(hi, v))) - log2(lo)) / (log2(hi) - log2(lo)) * (W - M.left - M.right);
  const rows = rowNames(reg, true);
  const root = svg('svg', { class: 'sp-chart sp-forest', viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': `Each label's ${measure === 'density' ? 'density' : 'share'} in ${rows[a]} over ${rows[b]}, with its interval` });
  for (let e = Math.ceil(log2(lo)); e <= Math.floor(log2(hi)); e += 1) {
    const x = sx(2 ** e);
    root.append(svg('line', { x1: x, x2: x, y1: M.top, y2: H - M.bottom, class: e === 0 ? 'sp-one' : 'sp-grid' }));
    root.append(svg('text', { x, y: H - M.bottom + 16, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(e >= 0 ? `${2 ** e}×` : `1/${2 ** -e}`)));
  }
  items.forEach(([v, l, u], k) => {
    const y = M.top + row * k + row / 2;
    const label = svg('text', { x: M.left - 8, y: y + 3.5, 'text-anchor': 'end', class: `sp-axis${chosen === k ? ' chosen' : ''}` }, document.createTextNode(names[k]));
    label.addEventListener('click', () => onPick(k));
    root.append(label);
    if (!Number.isFinite(l)) return;
    const color = analysis.labels.colors?.[k] ?? 'var(--text-2)';
    const clear = l > 1 || u < 1;
    const x0 = sx(Math.max(l, lo));
    const x1 = u === Infinity ? W - M.right : sx(u);
    const g = svg('g', { class: `sp-whisker${clear ? ' clear' : ''}${chosen === k ? ' chosen' : ''}`, 'data-label': k });
    g.append(svg('line', { x1: x0, x2: x1, y1: y, y2: y, stroke: color }));
    if (u === Infinity) g.append(svg('path', { d: `M${x1 - 6},${y - 4}L${x1},${y}L${x1 - 6},${y + 4}`, stroke: color, fill: 'none' }));
    if (Number.isFinite(v) && v > 0) g.append(svg('circle', { cx: sx(v), cy: y, r: clear ? 4.5 : 3.5, fill: clear ? color : 'var(--bg)', stroke: color }));
    g.append(svg('title', {}, document.createTextNode(`${names[k]}: ${ratioText(v, l, u)}`)));
    g.addEventListener('click', () => onPick(k));
    root.append(g);
  });
  root.append(svg('text', { x: (M.left + W - M.right) / 2, y: H - 4, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(`${rows[a]} over ${rows[b]} (log scale)`)));
  return h('div.sp-chart-wrap', root);
}

// The middle of each band of a profile within the reach (the deepest and farthest left out).
function bandMiddles(edges) {
  return edges.slice(1).map((e, i) => (edges[i] + e) / 2);
}

// A label's profile across a region's edge: its density (or share) in each band within the reach,
// with the intervals as a band, its level over all those bands dashed, the edge and the margin
// marked.
export function profileChart(analysis, k, { measure = 'density' } = {}) {
  const b = analysis.boundary;
  const names = analysis.labels.names;
  const K = names.length;
  const margin = analysis.parameters.margin;
  const x = bandMiddles(b.edges);
  const pick = (v) => x.map((_, i) => v[(i + 1) * K + k] ?? NaN);
  const y = measure === 'density' ? pick(b.density) : pick(b.share);
  const lo = measure === 'density' ? pick(b.densityLo) : pick(b.shareLo);
  const hi = measure === 'density' ? pick(b.densityHi) : pick(b.shareHi);
  // The label's level over the bands within the reach.
  let count = 0;
  let base = 0;
  for (let i = 1; i < b.edges.length; i += 1) {
    count += b.count[i * K + k];
    base += measure === 'density' ? b.area[i] / 1e6 : b.labeled[i];
  }
  const level = base > 0 ? count / base : NaN;
  const W = 560;
  const H = 260;
  const M = { left: 56, right: 12, top: 10, bottom: 34 };
  // A band with little tissue (under a quarter of the bands' median: deep inside a small region,
  // or mostly off the tissue) is drawn hollow, apart from the line and the scale; intervals reaching
  // above the scale are cut at the top.
  const areas = x.map((_, i) => b.area[i + 1]).filter((a) => a > 0).sort((p, q) => p - q);
  const typical = areas.length ? areas[Math.floor(areas.length / 2)] : 0;
  const thin = x.map((_, i) => !(b.area[i + 1] >= 0.25 * typical));
  const vals = [...y.filter((_, i) => !thin[i]), level].filter(Number.isFinite);
  const ymax = (vals.length ? Math.max(...vals) : 0) * 1.3 || 1;
  const top = (v) => Math.min(ymax, v);
  const x0 = b.edges[0];
  const x1 = b.edges.at(-1);
  const sx = (v) => M.left + (v - x0) / (x1 - x0) * (W - M.left - M.right);
  const sy = (v) => M.top + (1 - v / ymax) * (H - M.top - M.bottom);
  const root = svg('svg', { class: 'sp-chart sp-profile', viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': `${names[k]} cells ${measure === 'density' ? 'per mm²' : 'as a share of the labeled cells'} by distance from the edge of ${b.region.name}, inside negative` });
  root.append(svg('rect', { x: sx(-margin), y: M.top, width: sx(margin) - sx(-margin), height: H - M.top - M.bottom, class: 'sp-margin' }));
  const step = ymax / 4;
  const mag = 10 ** Math.floor(Math.log10(step || 1));
  const tick = [1, 2, 5, 10].map((m) => m * mag).find((t) => t >= step) ?? 10 * mag;
  for (let v = 0; v <= ymax; v += tick) {
    root.append(svg('line', { x1: M.left, x2: W - M.right, y1: sy(v), y2: sy(v), class: 'sp-grid' }));
    root.append(svg('text', { x: M.left - 6, y: sy(v) + 3.5, 'text-anchor': 'end', class: 'sp-axis' }, document.createTextNode(measure === 'density' ? (Math.round(v * 100) / 100).toLocaleString('en-US') : `${Math.round(100 * v)}%`)));
  }
  // Labels at a round multiple of the step, at most about ten of them, 0 among them.
  const bandStep = analysis.parameters.step;
  const every = [1, 2, 5, 10, 20, 50, 100].map((m) => m * bandStep).find((c) => (x1 - x0) / c <= 10) ?? x1 - x0;
  for (const e of b.edges) {
    if (Math.abs(e / every - Math.round(e / every)) < 1e-9) root.append(svg('text', { x: sx(e), y: H - M.bottom + 16, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(String(e))));
  }
  root.append(svg('line', { x1: sx(0), x2: sx(0), y1: M.top, y2: H - M.bottom, class: 'sp-edge' }));
  root.append(svg('text', { x: sx(0) + 4, y: M.top + 11, class: 'sp-axis' }, document.createTextNode('edge')));
  root.append(svg('text', { x: (M.left + W - M.right) / 2, y: H - 4, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode('distance from the edge (µm): inside < 0 < outside')));
  root.append(svg('text', { x: 0, y: 0, transform: `translate(12, ${(M.top + H - M.bottom) / 2}) rotate(-90)`, 'text-anchor': 'middle', class: 'sp-axis' }, document.createTextNode(measure === 'density' ? 'cells per mm²' : 'share of the labeled cells')));
  const band = x.map((v, i) => [sx(v), top(lo[i]), top(hi[i])]).filter(([, a, c], i) => !thin[i] && Number.isFinite(a) && Number.isFinite(c));
  if (band.length) root.append(svg('path', { d: `${band.map(([px, a], i) => `${i ? 'L' : 'M'}${px.toFixed(1)},${sy(a).toFixed(1)}`).join('')}${band.slice().reverse().map(([px, , c]) => `L${px.toFixed(1)},${sy(c).toFixed(1)}`).join('')}Z`, class: 'sp-envelope' }));
  if (Number.isFinite(level)) root.append(svg('line', { x1: M.left, x2: W - M.right, y1: sy(level), y2: sy(level), class: 'sp-mean' }));
  const color = analysis.labels.colors?.[k] ?? 'var(--accent)';
  const pts = x.map((v, i) => [sx(v), y[i], thin[i]]).filter(([, v]) => Number.isFinite(v));
  const solid = pts.filter(([, , t]) => !t);
  root.append(svg('path', { d: solid.map(([px, v], i) => `${i ? 'L' : 'M'}${px.toFixed(1)},${sy(v).toFixed(1)}`).join(''), stroke: color, class: 'sp-line chosen' }));
  for (const [px, v, t] of pts) {
    const dot = svg('circle', { cx: px, cy: sy(top(v)), r: 3, fill: t ? 'var(--bg)' : color, stroke: color });
    if (t) dot.append(svg('title', {}, document.createTextNode('little tissue in this band')));
    root.append(dot);
  }
  const swatch = (cls) => svg('svg', { class: 'sp-keyline', viewBox: '0 0 22 8', 'aria-hidden': 'true' }, svg('line', { x1: 1, x2: 21, y1: 4, y2: 4, class: cls }));
  return h('div.sp-chart-wrap', root, h('div.sp-scale', h('span.sp-key.observed', { style: { background: color } }), `${names[k]}, each band`,
    h('span.sp-key.band'), `its ${Math.round(100 * (analysis.parameters.level ?? 0.95))}% interval`, swatch('sp-mean'), `over all ${b.edges.length - 1} bands`, h('span.sp-key.margin-key'), `the margin (${margin} µm either side)`, thin.some(Boolean) ? [h('span.sp-key.hollow', { style: { borderColor: color } }), 'a band with little tissue'] : null));
}

// Each label's core, margin and surroundings across a region's edge: their densities, the core over
// around and the margin over each, and what they say; onPick(k).
export function readingsTable(analysis, { onPick, chosen = null }) {
  const b = analysis.boundary;
  const names = analysis.labels.names;
  const K = names.length;
  const C = 3;
  const reading = (k) => [b.inside[k] > 0 ? 'fills the region' : b.inside[k] < 0 ? 'stays out' : null, b.atMargin[k] ? 'gathers at the margin' : null].filter(Boolean).join('; ');
  const order = names.map((_, k) => k).sort((p, q) => Number(Boolean(reading(q))) - Number(Boolean(reading(p))) || (interval(b.ratio[q * C], b.ratioLo[q * C], b.ratioHi[q * C])[0] || 0) - (interval(b.ratio[p * C], b.ratioLo[p * C], b.ratioHi[p * C])[0] || 0));
  const zone = (z, k) => {
    const [v] = interval(b.zoneDensity[z * K + k], b.zoneLo[z * K + k], b.zoneHi[z * K + k]);
    return h('td.r', density(v));
  };
  const cmp = (k, c) => h('td.r', ratioText(...interval(b.ratio[k * C + c], b.ratioLo[k * C + c], b.ratioHi[k * C + c])));
  return h('div.table-wrap', h('table.data-table.seg-table.sp-readings',
    h('thead', h('tr', h('th', 'Label'), h('th.r', 'Core'), h('th.r', 'Margin'), h('th.r', 'Around'), h('th.r', 'Core over around'), h('th.r', 'Margin over core'), h('th.r', 'Margin over around'), h('th', 'Reading'))),
    h('tbody', ...order.map((k) => h(`tr${chosen === k ? '.selected' : ''}`, pickable({ 'data-label': k }, () => onPick(k)),
      h('th', { scope: 'row' }, h('span.sp-swatch', { style: { background: analysis.labels.colors?.[k] ?? 'var(--text-3)' } }), names[k]),
      zone(0, k), zone(1, k), zone(2, k), cmp(k, 0), cmp(k, 1), cmp(k, 2), h('td', reading(k) || h('span.muted', '—')))))));
}

// --- Niches ---

// Niches' make-up: a row for each niche, a column for each label, each square its share of the
// niche's windows over its share of the tissue's labeled cells (log₂: red above, blue below, clamped
// at ±2); onPick(v) when a niche is chosen.
export function nicheHeatmap(analysis, { onPick, chosen = null }) {
  const r = analysis.niches;
  const names = analysis.labels.names;
  const K = names.length;
  const rows = nicheNames(r, names);
  const cell = Math.max(14, Math.min(36, Math.floor(480 / K)));
  const left = Math.min(230, 14 + 6.2 * Math.max(...rows.map((n) => n.length)));
  const top = Math.min(150, 12 + 6.2 * Math.max(...names.map((n) => n.length)));
  const W = left + K * cell + 8;
  const H = top + r.niches * cell + 8;
  const root = svg('svg', { class: 'sp-heatmap', width: W, height: H, viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': 'Each niche\'s share of each label over the tissue\'s' });
  names.forEach((name, k) => {
    root.append(svg('text', { x: 0, y: 0, transform: `translate(${left + k * cell + cell / 2 + 3.5}, ${top - 6}) rotate(-60)`, class: 'sp-axis' }, document.createTextNode(name)));
  });
  rows.forEach((name, v) => {
    const label = svg('text', { x: left - 8, y: top + v * cell + cell / 2 + 3.5, 'text-anchor': 'end', class: `sp-axis${chosen === v ? ' chosen' : ''}`, 'data-niche': v }, document.createTextNode(name));
    label.addEventListener('click', () => onPick(v));
    root.append(label);
    for (let k = 0; k < K; k += 1) {
      const share = r.composition[v * K + k] ?? NaN;
      const ratio = share / r.tissue[k];
      const sq = svg('rect', { x: left + k * cell, y: top + v * cell, width: cell - 1, height: cell - 1, fill: Number.isFinite(ratio) ? divergingColor(ratio > 0 ? log2(ratio) : -Infinity) : 'transparent', class: 'sp-square', 'data-niche': v });
      sq.append(svg('title', {}, document.createTextNode(`${name}: ${names[k]} ${(100 * share).toFixed(1)}% of its windows, ${(100 * r.tissue[k]).toFixed(1)}% of the tissue's labeled cells (${times(ratio)})`)));
      sq.addEventListener('click', () => onPick(v));
      root.append(sq);
    }
    root.append(svg('rect', { x: 2, y: top + v * cell + (cell - 10) / 2, width: 10, height: 10, rx: 2, fill: categoricalColor(v) }));
  });
  if (chosen !== null) root.append(svg('rect', { x: left - 1, y: top + chosen * cell - 1, width: K * cell + 1, height: cell + 1, class: 'sp-chosen-square' }));
  return h('div.sp-heatmap-wrap', root, h('div.sp-scale', 'less than in the tissue', h('span.sp-scale-bar', { style: { background: `linear-gradient(90deg, ${[-2, -1, 0, 1, 2].map((v) => divergingColor(v)).join(', ')})` } }), 'more (¼× to 4×)'));
}

// Niches as a table: each one's cells, its defining labels (their share of its windows, and how many
// times the tissue's) and its leading genes; onPick(v).
export function nichesTable(analysis, { onPick, chosen = null }) {
  const r = analysis.niches;
  const names = analysis.labels.names;
  const K = names.length;
  const rows = nicheNames(r, names);
  const total = r.cells.reduce((a, b) => a + b, 0);
  return h('div.table-wrap', h('table.data-table.seg-table.sp-niches',
    h('thead', h('tr', h('th', 'Niche'), h('th.r', 'Cells'), h('th', 'Defined by'), h('th', 'Leading genes (exploratory)'))),
    h('tbody', ...rows.map((name, v) => h(`tr${chosen === v ? '.selected' : ''}`, pickable({ 'data-niche': v }, () => onPick(v)),
      h('th', { scope: 'row' }, h('span.sp-swatch', { style: { background: categoricalColor(v) } }), name),
      h('td.r', formatCount(r.cells[v]), h('div.muted.sp-sub', percent(r.cells[v] / total))),
      h('td', r.defining[v].map((k) => `${names[k]} ${percent(r.composition[v * K + k])} (${times(r.composition[v * K + k] / r.tissue[k])})`).join(', ')),
      h('td', (r.markers[v] ?? []).slice(0, 5).map((m) => m.gene).join(', ') || h('span.muted', '—')))))));
}

// --- Spatially variable genes ---

// Genes by Moran's I: those in a pattern (adjusted p below 0.05, I above its expectation), the
// greatest I first, up to `top`, and those dispersed (below it); onPick(g) when a gene is chosen;
// each one's I, z, adjusted p and the cells expressing it, marked when its local Moran's I is kept.
export function moranTable(analysis, { onPick, chosen = null, top = 15 }) {
  const r = analysis.moran;
  const G = r.genes.length;
  const local = new Set(r.local);
  const rows = [];
  for (let g = 0; g < G; g += 1) if ((r.padj[g] ?? NaN) < 0.05 && Number.isFinite(r.i[g])) rows.push(g);
  rows.sort((a, b) => r.i[b] - r.i[a]);
  const up = rows.filter((g) => r.i[g] > r.expected).slice(0, top);
  const down = rows.filter((g) => r.i[g] < r.expected).reverse().slice(0, 5);
  const row = (g) => h(`tr${chosen === g ? '.selected' : ''}`, pickable({ 'data-gene': r.genes[g] }, () => onPick(g)),
    h('td', h('b', r.genes[g]), local.has(g) ? h('span.muted', ' · local') : null),
    h('td.r', fixed(r.i[g], 3)), h('td.r', fixed(r.z[g], 1)), h('td.r', pvalue(r.padj[g])),
    h('td.r', percent(r.detected[g] / r.cells)));
  const table = (list, caption) => (list.length ? h('div', h('div.section-title', caption), h('div.table-wrap', h('table.data-table.seg-table.sp-genes',
    h('thead', h('tr', h('th', 'Gene'), h('th.r', 'Moran\'s I'), h('th.r', 'z'), h('th.r', 'Adjusted p'), h('th.r', 'Cells expressing'))),
    h('tbody', ...list.map(row))))) : null);
  return h('div.sp-pair-tables',
    table(up, `In a pattern: ${rows.filter((g) => r.i[g] > r.expected).length.toLocaleString('en-US')} of ${G.toLocaleString('en-US')} genes`),
    table(down, 'Dispersed: neighbors unlike'),
    !up.length && !down.length ? h('p.muted.seg-note', 'No gene\'s Moran\'s I differs from what values dealt at random give (adjusted p below 0.05).') : null);
}
