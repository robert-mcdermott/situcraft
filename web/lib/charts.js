// Charts as marks (wave 8, slice 2; requirement E6): the plots a figure's result panels draw, in
// points at a figure's text size, the same in Node as in the page (lib/draw.js writes them as SVG
// and PDF). Pure: each takes its data and a frame { x, y, width, height, text } and returns
// { marks, description, drawn } (drawn: what it placed, for checks).
// The axes, ticks, horizontal bars and legend are adapted from MaveScape 0.8.0's web/lib/plots.js
// (Apache-2.0, the same author); the histogram, scatter, heat map, forest, volcano and dots by
// group are SituCraft's.

import { ellipsizeText, textWidth } from './draw.js';
import { log10, pow } from './dmath.js';

// The colors charts print in (the light theme's, web/styles.css).
export const THEME = {
  text: '#171b26', label: '#4b5468', muted: '#636c7e', axis: '#b8c0ce', grid: '#e3e7ef',
  accent: '#5b4ce6', found: '#d63b4a', point: '#5b6478',
};

const finite = (v) => Number.isFinite(v);
// No text below a journal's usual minimum (pt).
const MIN_TEXT = 6;
const atLeast = (size) => Math.max(MIN_TEXT, size);

// Tick values for a range: about n "nice" numbers, each a whole multiple of the step.
export function ticks(lo, hi, n = 5) {
  if (!(hi > lo)) return [lo];
  const raw = (hi - lo) / n;
  const mag = pow(10, Math.floor(log10(raw)));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw * (1 - 1e-9)) ?? raw;
  const out = [];
  for (let k = Math.ceil(lo / step - 1e-9); k * step <= hi + step * 1e-9; k += 1) out.push(Number((k * step).toPrecision(12)));
  return out;
}

// Powers of ten across a range of positive numbers.
export function logTicks(lo, hi) {
  const out = [];
  for (let e = Math.floor(log10(lo)); e <= Math.ceil(log10(hi)); e += 1) out.push(Number(pow(10, e).toPrecision(12)));
  return out.filter((v) => v >= lo / 1.0001 && v <= hi * 1.0001);
}

export const tickText = (v) => (Math.abs(v) >= 1e5 || (Math.abs(v) < 1e-2 && v !== 0) ? v.toExponential(0).replace('e+', 'e') : String(Number(v.toPrecision(4))));

// A scale from a domain to a range (log: by log10).
export function scale(lo, hi, from, to, log = false) {
  const f = (v) => (log ? log10(v) : v);
  const a = f(lo);
  const b = f(hi);
  return (v) => from + ((f(v) - a) / (b - a || 1)) * (to - from);
}

// Axes: the x and y lines, ticks, grid lines across, and the labels. a: { left, right, top,
// bottom } of the plot area; x, y: { lo, hi, log, ticks (a list, or [] for none) }. Returns the
// scales { sx, sy }.
function axes(marks, a, { x, y, xLabel, yLabel, t, grid = true, frame, xTickText = tickText, yTickText = tickText }) {
  const k = t / 7;
  const sx = scale(x.lo, x.hi, a.left, a.right, x.log);
  const sy = scale(y.lo, y.hi, a.bottom, a.top, y.log);
  marks.push({ type: 'line', x1: a.left, y1: a.bottom, x2: a.right, y2: a.bottom, stroke: THEME.axis, width: 0.6 * k });
  marks.push({ type: 'line', x1: a.left, y1: a.top, x2: a.left, y2: a.bottom, stroke: THEME.axis, width: 0.6 * k });
  // Ticks thinned to every other (or fewer) where their labels would crowd.
  const thin = (list, room, need) => {
    if (list.length < 3 || room / (list.length - 1) >= need) return list;
    const step = Math.ceil((need * (list.length - 1)) / room);
    return list.filter((_, i) => i % step === 0);
  };
  const xTicks = x.ticks ?? (x.log ? logTicks(x.lo, x.hi) : ticks(x.lo, x.hi));
  const yTicks = y.ticks ?? (y.log ? logTicks(y.lo, y.hi) : ticks(y.lo, y.hi, 4));
  const widest = Math.max(0, ...xTicks.map((v) => textWidth(xTickText(v), t)));
  for (const v of thin(xTicks, a.right - a.left, widest + 0.8 * t)) {
    marks.push({ type: 'line', x1: sx(v), y1: a.bottom, x2: sx(v), y2: a.bottom + 0.35 * t, stroke: THEME.axis, width: 0.6 * k });
    marks.push({ type: 'text', x: sx(v), y: a.bottom + 1.4 * t, text: xTickText(v), size: t, fill: THEME.muted, anchor: 'middle' });
  }
  for (const v of thin(yTicks, a.bottom - a.top, 1.4 * t)) {
    if (grid) marks.push({ type: 'line', x1: a.left, y1: sy(v), x2: a.right, y2: sy(v), stroke: THEME.grid, width: 0.5 * k, dash: [1.5 * k, 2 * k] });
    marks.push({ type: 'text', x: a.left - 0.4 * t, y: sy(v), text: yTickText(v), size: t, fill: THEME.muted, anchor: 'end', baseline: 'middle' });
  }
  if (xLabel) marks.push({ type: 'text', x: (a.left + a.right) / 2, y: frame.y + frame.height - 0.3 * t, text: ellipsizeText(xLabel, a.right - a.left, t), size: t, fill: THEME.label, anchor: 'middle' });
  if (yLabel) {
    const cy = (a.top + a.bottom) / 2;
    marks.push({ type: 'text', x: frame.x + 0.9 * t, y: cy, text: ellipsizeText(yLabel, a.bottom - a.top, t), size: t, fill: THEME.label, anchor: 'middle', rotate: -90 });
  }
  return { sx, sy };
}

// The plot area left by the axes' labels.
function area(f, { left = 4.6, bottom = 3.2, top = 0.6, right = 1 } = {}) {
  const t = f.text;
  return { left: f.x + left * t, right: f.x + f.width - right * t, top: f.y + top * t, bottom: f.y + f.height - bottom * t };
}

function nothing(f, text = 'Nothing to draw.') {
  return { marks: [{ type: 'text', x: f.x + f.width / 2, y: f.y + f.height / 2, text, size: f.text, fill: THEME.muted, anchor: 'middle', baseline: 'middle' }], description: text, drawn: { empty: true } };
}

// --- A histogram ---------------------------------------------------------------------------------

// Counts in bins (edges: n + 1 values, counts: n), on a linear or log x axis, with vertical lines
// ({ value, label }: a filter's thresholds).
export function histogramChart(spec, f) {
  const { edges, counts, xLabel, yLabel = 'Cells', log = false, lines = [], color = THEME.accent } = spec;
  if (!counts.length || counts.every((c) => c === 0)) return nothing(f);
  const marks = [];
  const top = Math.max(...counts);
  const a = area(f);
  const { sx, sy } = axes(marks, a, { x: { lo: edges[0], hi: edges.at(-1), log }, y: { lo: 0, hi: top * 1.05 }, xLabel, yLabel, t: f.text, frame: f });
  const bars = [];
  counts.forEach((c, i) => {
    if (c <= 0) return;
    const x0 = sx(edges[i]);
    const x1 = sx(edges[i + 1]);
    bars.push({ type: 'rect', x: x0, y: sy(c), w: Math.max(0.1, x1 - x0), h: a.bottom - sy(c), fill: color });
  });
  marks.push({ type: 'group', label: 'bars', marks: bars });
  for (const l of lines) {
    if (!finite(l.value) || (log && l.value <= 0) || l.value < edges[0] || l.value > edges.at(-1)) continue;
    marks.push({ type: 'line', x1: sx(l.value), y1: a.top, x2: sx(l.value), y2: a.bottom, stroke: THEME.found, width: 0.9, dash: [2.5, 1.5] });
  }
  return { marks, description: '', drawn: { bars: bars.length, max: top } };
}

// --- Horizontal bars -----------------------------------------------------------------------------

// A bar per item ({ label, value }), longest first as given, on a linear axis, a vertical line at a
// reference (an expected value).
export function barChart(spec, f) {
  const { xLabel, reference = null, format = (v) => v.toFixed(3) } = spec;
  if (!spec.items.length) return nothing(f);
  const t = f.text;
  const marks = [];
  // As many bars as fit with their names at 6 pt or more; the rest said.
  const fit = Math.max(1, Math.floor((f.height - 3.4 * t) / (MIN_TEXT / 0.85)));
  const items = spec.items.slice(0, fit);
  const labelW = Math.min(f.width * 0.4, Math.max(...items.map((x) => textWidth(x.label, t))) + 0.6 * t);
  const valueW = Math.max(...items.map((x) => textWidth(format(x.value), t))) + 0.6 * t;
  const a = { left: f.x + labelW, right: f.x + f.width - valueW, top: f.y + 0.4 * t, bottom: f.y + f.height - 3 * t };
  const values = items.map((x) => x.value).filter(finite);
  const lo = Math.min(0, ...values, reference ?? 0);
  const hi = Math.max(...values, reference ?? 0) * 1.02 || 1;
  const { sx } = axes(marks, a, { x: { lo, hi }, y: { lo: 0, hi: 1, ticks: [] }, xLabel, t, grid: false, frame: f });
  const row = (a.bottom - a.top) / items.length;
  const size = atLeast(Math.min(t, row * 0.85));
  items.forEach((x, i) => {
    const y = a.top + i * row + row * 0.15;
    const h = row * 0.7;
    marks.push({ type: 'text', x: a.left - 0.4 * t, y: y + h / 2, text: ellipsizeText(x.label, labelW - 0.4 * t, size), size, fill: THEME.label, anchor: 'end', baseline: 'middle' });
    const x0 = sx(Math.min(0, x.value));
    marks.push({ type: 'rect', x: x0, y, w: Math.max(0.2, Math.abs(sx(x.value) - sx(0))), h, fill: x.color ?? THEME.accent });
    marks.push({ type: 'text', x: sx(Math.max(0, x.value)) + 0.3 * t, y: y + h / 2, text: format(x.value), size, fill: THEME.label, baseline: 'middle' });
  });
  if (finite(reference)) marks.push({ type: 'line', x1: sx(reference), y1: a.top, x2: sx(reference), y2: a.bottom, stroke: THEME.muted, width: 0.6, dash: [2, 1.5] });
  return { marks, description: '', drawn: { bars: items.length, left: spec.items.length - items.length } };
}

// --- A scatter of points -------------------------------------------------------------------------

// Points (x, y: arrays; colors: one each, or one for all) as small squares; equal: one unit the
// same on both axes (a map), its ticks hidden.
export function scatterChart(spec, f) {
  const { x, y, colors, xLabel, yLabel, size = 1.2, equal = false, opacity = 0.8 } = spec;
  const keep = [];
  for (let i = 0; i < x.length; i += 1) if (finite(x[i]) && finite(y[i])) keep.push(i);
  if (!keep.length) return nothing(f);
  const marks = [];
  let [x0, x1, y0, y1] = [Infinity, -Infinity, Infinity, -Infinity];
  for (const i of keep) {
    x0 = Math.min(x0, x[i]);
    x1 = Math.max(x1, x[i]);
    y0 = Math.min(y0, y[i]);
    y1 = Math.max(y1, y[i]);
  }
  const mx = (x1 - x0) * 0.03 || 1;
  const my = (y1 - y0) * 0.03 || 1;
  [x0, x1, y0, y1] = [x0 - mx, x1 + mx, y0 - my, y1 + my];
  const a = equal ? area(f, { left: 1.8, bottom: 2.2 }) : area(f);
  if (equal) {
    // One unit the same across and down: the area shrunk on one side, centered.
    const k = Math.min((a.right - a.left) / (x1 - x0), (a.bottom - a.top) / (y1 - y0));
    const w = (x1 - x0) * k;
    const h = (y1 - y0) * k;
    const cx = (a.left + a.right) / 2;
    const cy = (a.top + a.bottom) / 2;
    Object.assign(a, { left: cx - w / 2, right: cx + w / 2, top: cy - h / 2, bottom: cy + h / 2 });
  }
  const { sx, sy } = axes(marks, a, { x: { lo: x0, hi: x1, ticks: equal ? [] : undefined }, y: { lo: y0, hi: y1, ticks: equal ? [] : undefined }, xLabel, yLabel, t: f.text, grid: false, frame: f });
  const dots = [];
  for (const i of keep) dots.push({ type: 'rect', x: Math.round((sx(x[i]) - size / 2) * 100) / 100, y: Math.round((sy(y[i]) - size / 2) * 100) / 100, w: size, h: size, fill: Array.isArray(colors) ? colors[i] : colors ?? THEME.point });
  marks.push({ type: 'group', label: 'points', marks: dots.map((d) => (opacity < 1 ? { ...d, opacity } : d)) });
  // Names at their groups' centers (a map's clusters), each over a pale box so that it reads.
  for (const l of spec.labels ?? []) {
    const size = atLeast(f.text * 0.9);
    const w = textWidth(l.text, size);
    const cx = sx(l.x);
    const cy = sy(l.y);
    marks.push({ type: 'rect', x: cx - w / 2 - 0.6, y: cy - size * 0.55, w: w + 1.2, h: size * 1.1, fill: '#ffffff', opacity: 0.7, radius: 1 });
    marks.push({ type: 'text', x: cx, y: cy, text: l.text, size, fill: THEME.text, anchor: 'middle', baseline: 'middle' });
  }
  return { marks, description: '', drawn: { points: dots.length, labels: (spec.labels ?? []).length } };
}

// --- A heat map ----------------------------------------------------------------------------------

// A square per pair of names (rows × columns), its color from color(i, j) (null: none), a dot where
// marked(i, j); the names along the left and top (turned a quarter), each with its color's tick;
// and a color bar (stops, the words at its ends).
export function heatmapChart(spec, f) {
  const { rows, columns = rows, color, marked = () => false, nameColors = null, bar = null } = spec;
  if (!rows.length) return nothing(f);
  const t = f.text;
  const marks = [];
  const left = Math.min(f.width * 0.35, Math.max(...rows.map((n) => textWidth(n, t))) + 0.8 * t);
  const topLabels = Math.min(f.height * 0.42, Math.max(...columns.map((n) => textWidth(n, t))) + 0.8 * t);
  // The color bar's height with its words below it.
  const barH = bar ? 3.6 * t : 0;
  const side = Math.max(1, Math.min((f.width - left - 0.4 * t) / columns.length, (f.height - topLabels - barH) / rows.length));
  const x0 = f.x + left;
  const y0 = f.y + topLabels;
  const size = atLeast(Math.min(t, side * 0.9));
  rows.forEach((name, i) => {
    marks.push({ type: 'text', x: x0 - 0.5 * t, y: y0 + i * side + side / 2, text: ellipsizeText(name, left - 0.6 * t, size), size, fill: THEME.label, anchor: 'end', baseline: 'middle' });
    if (nameColors?.[i]) marks.push({ type: 'rect', x: x0 - 0.35 * t, y: y0 + i * side + 0.1 * side, w: 0.22 * t, h: side * 0.8, fill: nameColors[i] });
  });
  columns.forEach((name, j) => {
    marks.push({ type: 'text', x: x0 + j * side + side / 2, y: y0 - 0.5 * t, text: ellipsizeText(name, topLabels - 0.6 * t, size), size, fill: THEME.label, anchor: 'start', baseline: 'middle', rotate: -90 });
    if (nameColors?.[j]) marks.push({ type: 'rect', x: x0 + j * side + 0.1 * side, y: y0 - 0.35 * t, w: side * 0.8, h: 0.22 * t, fill: nameColors[j] });
  });
  let squares = 0;
  let dots = 0;
  for (let i = 0; i < rows.length; i += 1) {
    for (let j = 0; j < columns.length; j += 1) {
      const c = color(i, j);
      if (!c) continue;
      marks.push({ type: 'rect', x: x0 + j * side, y: y0 + i * side, w: side - 0.4, h: side - 0.4, fill: c });
      squares += 1;
      if (marked(i, j)) {
        marks.push({ type: 'circle', x: x0 + j * side + side / 2 - 0.2, y: y0 + i * side + side / 2 - 0.2, r: Math.max(0.6, side / 9), fill: THEME.text });
        dots += 1;
      }
    }
  }
  if (bar) {
    const by = y0 + rows.length * side + 1.2 * t;
    // Wide enough for the words at its ends, side by side.
    const bw = Math.min(f.width - left, Math.max(columns.length * side, Math.min(12 * t, textWidth(bar.low, t) + textWidth(bar.high, t) + 1.5 * t)));
    const rgba = new Uint8Array(bar.stops.length * 4);
    bar.stops.forEach((c, i) => {
      const m = /rgb\((\d+), (\d+), (\d+)\)/.exec(c) ?? [];
      rgba.set([Number(m[1]), Number(m[2]), Number(m[3]), 255], 4 * i);
    });
    marks.push({ type: 'image', x: x0, y: by, w: bw, h: 0.8 * t, image: { width: bar.stops.length, height: 1, rgba }, smooth: true });
    marks.push({ type: 'text', x: x0, y: by + 0.8 * t + 1.1 * t, text: bar.low, size: t, fill: THEME.muted });
    marks.push({ type: 'text', x: x0 + bw, y: by + 0.8 * t + 1.1 * t, text: bar.high, size: t, fill: THEME.muted, anchor: 'end' });
  }
  return { marks, description: '', drawn: { squares, dots, side } };
}

// --- A forest of effects -------------------------------------------------------------------------

// Each row an estimate with its interval ({ label, estimate, lo, hi, marked, note }), on a log
// axis (ratios) or a linear one, with a line at the reference (1 or 0). A row without an estimate
// says its note.
export function forestChart(spec, f) {
  const { log = true, reference = log ? 1 : 0, xLabel } = spec;
  if (!spec.rows.length) return nothing(f);
  const t = f.text;
  const fit = Math.max(1, Math.floor((f.height - 3.4 * t) / (MIN_TEXT / 0.85)));
  const rows = spec.rows.slice(0, fit);
  const marks = [];
  const labelW = Math.min(f.width * 0.55, Math.max(...rows.map((r) => textWidth(r.label, t))) + 0.6 * t);
  const a = { left: f.x + labelW, right: f.x + f.width - 0.8 * t, top: f.y + 0.4 * t, bottom: f.y + f.height - 3 * t };
  const values = rows.flatMap((r) => [r.lo, r.hi, r.estimate]).filter((v) => finite(v) && (!log || v > 0));
  values.push(reference);
  let lo = Math.min(...values);
  let hi = Math.max(...values);
  if (log) {
    lo /= 1.25;
    hi *= 1.25;
  } else {
    const m = (hi - lo) * 0.06 || 1;
    lo -= m;
    hi += m;
  }
  const { sx } = axes(marks, a, { x: { lo, hi, log, ticks: log ? forestTicks(lo, hi) : undefined }, y: { lo: 0, hi: 1, ticks: [] }, xLabel, t, grid: false, frame: f });
  marks.push({ type: 'line', x1: sx(reference), y1: a.top, x2: sx(reference), y2: a.bottom, stroke: THEME.muted, width: 0.6, dash: [2, 1.5] });
  const row = (a.bottom - a.top) / rows.length;
  const size = atLeast(Math.min(t, row * 0.85));
  const drawn = [];
  rows.forEach((r, i) => {
    const y = a.top + i * row + row / 2;
    marks.push({ type: 'text', x: a.left - 0.4 * t, y, text: ellipsizeText(r.label, labelW - 0.4 * t, size), size, fill: THEME.label, anchor: 'end', baseline: 'middle' });
    if (!(finite(r.estimate) && (!log || r.estimate > 0))) {
      marks.push({ type: 'text', x: a.left + 0.4 * t, y, text: r.note ?? 'untested', size: size * 0.9, fill: THEME.muted, baseline: 'middle' });
      drawn.push({ label: r.label, x: null });
      return;
    }
    const color = r.marked ? THEME.found : THEME.text;
    const clampX = (v) => sx(Math.min(hi, Math.max(lo, v)));
    marks.push({ type: 'line', x1: clampX(r.lo), y1: y, x2: clampX(r.hi), y2: y, stroke: color, width: 0.9 });
    marks.push({ type: 'rect', x: sx(r.estimate) - 0.32 * t, y: y - 0.32 * t, w: 0.64 * t, h: 0.64 * t, fill: color });
    drawn.push({ label: r.label, x: sx(r.estimate), lo: clampX(r.lo), hi: clampX(r.hi) });
  });
  return { marks, description: '', drawn: { rows: drawn, sx, left: spec.rows.length - rows.length } };
}

// Ticks of a ratio's log axis: 1, 2, 5 and 10 times powers of ten, and their reciprocals.
function forestTicks(lo, hi) {
  const out = [];
  for (let e = Math.floor(log10(lo)) - 1; e <= Math.ceil(log10(hi)); e += 1) {
    for (const m of [1, 2, 5]) {
      const v = Number((m * pow(10, e)).toPrecision(6));
      if (v >= lo && v <= hi) out.push(v);
    }
  }
  return out;
}

// --- A volcano -----------------------------------------------------------------------------------

// Each gene's log2 fold change against −log10 p ({ gene, x, y, found }), those found in the found
// color and on top, the first few found named.
export function volcanoChart(spec, f) {
  const { points, named = 5, cap = 50 } = spec;
  if (!points.length) return nothing(f);
  const t = f.text;
  const marks = [];
  const big = Math.max(1, ...points.map((p) => Math.abs(p.x)).filter(finite));
  const top = Math.max(2, ...points.map((p) => Math.min(p.y, cap)).filter(finite)) * 1.05;
  const a = area(f);
  const { sx, sy } = axes(marks, a, { x: { lo: -big * 1.05, hi: big * 1.05 }, y: { lo: 0, hi: top }, xLabel: 'log2 fold change', yLabel: '−log10 p', t, frame: f });
  marks.push({ type: 'line', x1: sx(0), y1: a.top, x2: sx(0), y2: a.bottom, stroke: THEME.axis, width: 0.6, dash: [2, 1.5] });
  const r = Math.max(0.6, t * 0.14);
  const order = [...points.filter((p) => !p.found), ...points.filter((p) => p.found)];
  const dots = order.filter((p) => finite(p.x) && finite(p.y)).map((p) => ({ type: 'circle', x: Math.round(sx(p.x) * 100) / 100, y: Math.round(sy(Math.min(p.y, cap)) * 100) / 100, r, fill: p.found ? THEME.found : THEME.point, opacity: p.found ? 1 : 0.55 }));
  marks.push({ type: 'group', label: 'genes', marks: dots });
  const found = points.filter((p) => p.found).sort((p, q) => q.y - p.y).slice(0, named);
  for (const p of found) marks.push({ type: 'text', x: sx(p.x) + (p.x > 0 ? -0.5 * t : 0.5 * t), y: sy(Math.min(p.y, cap)) - 0.5 * t, text: p.gene, size: t, fill: THEME.text, anchor: p.x > 0 ? 'end' : 'start' });
  return { marks, description: '', drawn: { points: dots.length, found: points.filter((p) => p.found).length, named: found.map((p) => p.gene) } };
}

// --- Dots by group -------------------------------------------------------------------------------

// Each group's values as dots ({ group, points: [{ value }], mean }) side by side, each group's
// mean a line, in the group's color; format writes the axis's values.
export function groupDotsChart(spec, f) {
  const { groups, yLabel, colors, format = tickText } = spec;
  const values = groups.flatMap((g) => g.points.map((p) => p.value)).filter(finite);
  if (!values.length) return nothing(f);
  const t = f.text;
  const marks = [];
  let lo = Math.min(...values);
  let hi = Math.max(...values);
  const pad = (hi - lo) * 0.08 || Math.abs(hi) * 0.1 || 1;
  lo -= pad;
  hi += pad;
  const a = area(f, { bottom: 2.4 });
  const { sy } = axes(marks, a, { x: { lo: 0, hi: 1, ticks: [] }, y: { lo, hi }, yLabel, t, frame: f, yTickText: format });
  const step = (a.right - a.left) / Math.max(1, groups.length);
  const r = Math.max(0.8, Math.min(t * 0.32, step * 0.08));
  let dots = 0;
  groups.forEach((g, gi) => {
    const x0 = a.left + step * gi + step / 2;
    const color = colors?.[gi] ?? THEME.accent;
    marks.push({ type: 'text', x: x0, y: a.bottom + 1.4 * t, text: ellipsizeText(g.group, step * 0.95, t), size: t, fill: THEME.label, anchor: 'middle' });
    if (g.points.length) marks.push({ type: 'line', x1: x0 - step * 0.28, y1: sy(g.mean), x2: x0 + step * 0.28, y2: sy(g.mean), stroke: THEME.text, width: 0.9 });
    const spread = Math.min(2.4 * r, (step * 0.5) / Math.max(1, g.points.length));
    g.points.forEach((p, pi) => {
      if (!finite(p.value)) return;
      marks.push({ type: 'circle', x: x0 + (pi - (g.points.length - 1) / 2) * spread, y: sy(p.value), r, fill: color });
      dots += 1;
    });
  });
  return { marks, description: '', drawn: { dots, sy } };
}

// --- A legend ------------------------------------------------------------------------------------

// Swatches and names in rows across a width ({ label, color }). Returns { marks, height }.
export function legendMarks(items, f) {
  const t = f.text;
  const marks = [];
  let x = f.x;
  let y = f.y;
  const line = 1.45 * t;
  for (const item of items) {
    const need = 1.3 * t + textWidth(item.label, t) + 1.1 * t;
    if (x > f.x && x + need > f.x + f.width) {
      x = f.x;
      y += line;
    }
    const mid = y + line / 2;
    marks.push({ type: 'rect', x, y: mid - 0.4 * t, w: 0.8 * t, h: 0.8 * t, fill: item.color });
    marks.push({ type: 'text', x: x + 1.2 * t, y: mid, text: item.label, size: t, fill: THEME.label, baseline: 'middle' });
    x += need;
  }
  return { marks, height: items.length ? y - f.y + line : 0 };
}

export const CHARTS = { histogram: histogramChart, bars: barChart, scatter: scatterChart, heatmap: heatmapChart, forest: forestChart, volcano: volcanoChart, dots: groupDotsChart };

// A result panel: its title, its chart and legend in a box. data: { title, chart: { kind, spec },
// legend?: [{ label, color }], description }. Returns { marks, description, notes, facts }.
export function chartPanel(box, data, options = {}) {
  const t = options.text ?? 7;
  const marks = [];
  let y = box.y;
  const title = options.title ?? data.title;
  if (title) {
    marks.push({ type: 'text', x: box.x, y, text: ellipsizeText(title, box.w, t + 0.5, true), size: t + 0.5, bold: true, fill: THEME.text, baseline: 'top' });
    y += t + 4;
  }
  // A legend takes at most a third of the panel: past that, as many entries as fit and how many
  // more there are.
  let items = data.legend ?? [];
  const tall = (list) => legendMarks(list, { x: box.x, y: 0, width: box.w, text: t }).height > (box.h - (y - box.y)) / 3;
  if (items.length && tall(items)) {
    let n = items.length;
    while (n > 1 && tall([...items.slice(0, n - 1), { label: `and ${items.length - n + 1} more`, color: '#ffffff' }])) n -= 1;
    items = [...items.slice(0, n - 1), { label: `and ${items.length - n + 1} more`, color: '#ffffff' }];
  }
  const legend = items.length ? legendMarks(items, { x: box.x, y: 0, width: box.w, text: t }) : null;
  const legendH = legend ? legend.height + 2 : 0;
  const frame = { x: box.x, y, width: box.w, height: box.y + box.h - y - legendH, text: t };
  const draw = CHARTS[data.chart.kind];
  const drawn = draw ? draw(data.chart.spec, frame) : nothing(frame, `No chart "${data.chart.kind}".`);
  marks.push(...drawn.marks);
  if (legend) marks.push(...legendMarks(items, { x: box.x, y: frame.y + frame.height + 2, width: box.w, text: t }).marks);
  const notes = [...(data.notes ?? [])];
  const left = drawn.drawn?.left ?? 0;
  if (left > 0) notes.push(`${left} more ${data.chart.kind === 'bars' ? 'bar' : 'row'}${left === 1 ? ' than the panel holds at 6 pt is' : 's than the panel holds at 6 pt are'} left out: make it taller.`);
  return { marks, description: data.description ?? drawn.description, notes, facts: { chart: data.chart.kind, gathered: data.facts ?? {}, frame, drawn: drawn.drawn } };
}
