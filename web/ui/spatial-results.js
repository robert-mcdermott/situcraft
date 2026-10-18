// A spatial analysis's results in the Spatial view (ui/mode-spatial.js): heat maps of pairs of labels
// (an enrichment's ratio of neighbors found to expected; nearest distances' median against chance's),
// on a diverging scale with the significant marked, and tables of the strongest; a co-occurrence's
// curves, one label's ratio to each label's over the distances; nearest distances' distribution and
// Ripley's L, each with its envelope from the permutations. Each pair, or each label at a distance,
// can be chosen, and its cells shown on the tissue.

import { h, formatCount } from './dom.js';
import { divergingColor, rankPairs } from '../lib/spatial.js';

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
  const swatch = (cls) => svg('svg', { class: 'sp-swatch', viewBox: '0 0 22 8', 'aria-hidden': 'true' }, svg('line', { x1: 1, x2: 21, y1: 4, y2: 4, class: cls }));
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
