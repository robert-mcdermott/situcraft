// A spatial analysis's results in the Spatial view (ui/mode-spatial.js): an enrichment's heat map of
// its pairs (the ratio of neighbors found to expected, on a diverging scale, significant pairs
// marked) and its table of the most enriched and depleted pairs; a co-occurrence's curves, one label's
// ratio to each label's over the distances. Each pair, or each label at a distance, can be chosen,
// and its cells shown on the tissue.

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

// The heat map of an enrichment: rows and columns the labels, each square the ratio's color (red
// more neighbors than expected, blue fewer, clamped at four times), a dot on those whose adjusted
// p-value is below 0.05. onPick(a, b) when one is chosen; chosen: [a, b] or null.
export function enrichmentHeatmap(analysis, { onPick, chosen = null }) {
  const names = analysis.labels.names;
  const K = names.length;
  const e = analysis.enrichment;
  const cell = Math.max(10, Math.min(40, Math.floor(420 / K)));
  const longest = Math.max(...names.map((n) => n.length));
  const margin = Math.min(170, 12 + 6.2 * longest);
  const size = margin + K * cell + 8;
  const root = svg('svg', { class: 'sp-heatmap', width: size, height: size, viewBox: `0 0 ${size} ${size}`, role: 'img', 'aria-label': `The ratio of neighbors found to expected for each pair of ${K} labels; the table below lists the strongest` });
  names.forEach((name, k) => {
    const color = analysis.labels.colors?.[k] ?? null;
    root.append(svg('text', { x: margin - 6, y: margin + k * cell + cell / 2 + 3.5, 'text-anchor': 'end', class: 'sp-axis' }, document.createTextNode(name)));
    root.append(svg('text', { x: 0, y: 0, transform: `translate(${margin + k * cell + cell / 2 + 3.5}, ${margin - 6}) rotate(-60)`, class: 'sp-axis' }, document.createTextNode(name)));
    if (color) {
      root.append(svg('rect', { x: margin - 4, y: margin + k * cell + 2, width: 3, height: cell - 4, fill: color }));
      root.append(svg('rect', { x: margin + k * cell + 2, y: margin - 4, width: cell - 4, height: 3, fill: color }));
    }
  });
  for (let a = 0; a < K; a += 1) {
    for (let b = 0; b < K; b += 1) {
      const q = a * K + b;
      const expected = e.expected[q] ?? NaN;
      const count = e.count[q] ?? NaN;
      const ratio = expected > 0 ? count / expected : NaN;
      const padj = e.padj[q] ?? NaN;
      const z = e.z[q];
      const title = `${pairName(names, a, b)}: ${formatCount(count)} found, ${formatCount(Math.round(expected))} expected (${times(ratio)}); z ${z === null ? '—' : fixed(z, 1)}, adjusted p ${pvalue(padj)}`;
      const rect = svg('rect', { x: margin + b * cell, y: margin + a * cell, width: cell - 1, height: cell - 1, fill: Number.isFinite(ratio) ? divergingColor(log2(ratio)) : 'var(--panel-2)', class: 'sp-square', 'data-pair': `${a},${b}` });
      rect.append(svg('title', {}, document.createTextNode(title)));
      rect.addEventListener('click', () => onPick(Math.min(a, b), Math.max(a, b)));
      root.append(rect);
      if (padj < 0.05) root.append(svg('circle', { cx: margin + b * cell + cell / 2 - 0.5, cy: margin + a * cell + cell / 2 - 0.5, r: Math.max(1.5, cell / 9), class: 'sp-significant', 'pointer-events': 'none' }));
      if (chosen && ((chosen[0] === a && chosen[1] === b) || (chosen[0] === b && chosen[1] === a))) root.append(svg('rect', { x: margin + b * cell - 1, y: margin + a * cell - 1, width: cell + 1, height: cell + 1, class: 'sp-chosen-square', 'pointer-events': 'none' }));
    }
  }
  const scale = h('div.sp-scale', h('span', 'fewer'),
    h('span.sp-scale-bar', { style: { background: `linear-gradient(to right, ${[-2, -1, -0.5, 0, 0.5, 1, 2].map((v) => divergingColor(v)).join(', ')})` } }),
    h('span', 'more neighbors than expected'),
    h('span.muted', ' (¼× to 4×; a dot: adjusted p below 0.05)'));
  return h('div.sp-heatmap-wrap', root, scale);
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

// The most enriched and most depleted pairs (adjusted p below 0.05), up to `top` each, as a table;
// onPick(a, b) when a row is chosen.
export function pairsTable(analysis, { onPick, top = 8 }) {
  const names = analysis.labels.names;
  const pairs = rankPairs(analysis).filter((p) => p.padj < 0.05 && Number.isFinite(p.z));
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
