import assert from 'node:assert/strict';
import test from 'node:test';
import { chartPanel, forestChart, groupDotsChart, heatmapChart, histogramChart, logTicks, scatterChart, tickText, ticks, volcanoChart, barChart } from './charts.js';
import { textMarks } from './draw.js';
import { addPanel, checkFigure, createFigure, figureMarks } from './figures.js';

const near = (a, b, tol = 1e-9) => Math.abs(a - b) <= tol;
const frame = { x: 10, y: 20, width: 200, height: 140, text: 7 };
const flat = (marks) => marks.flatMap((m) => (m.type === 'group' ? flat(m.marks) : [m]));

test('ticks: nice numbers, whole multiples of the step; powers of ten; their text', () => {
  assert.deepEqual(ticks(0, 100), [0, 20, 40, 60, 80, 100]);
  assert.deepEqual(ticks(0.3, 2.7), [0.5, 1, 1.5, 2, 2.5]);
  assert.deepEqual(ticks(-1, 1, 4), [-1, -0.5, 0, 0.5, 1]);
  assert.deepEqual(logTicks(0.5, 200), [1, 10, 100]);
  assert.deepEqual([0, 0.5, 1234, 250000, 0.001].map(tickText), ['0', '0.5', '1234', '3e5', '1e-3']);
});

test('a histogram: a bar for each bin counted, its height the count on the axis, the thresholds drawn', () => {
  const edges = [0, 10, 20, 30, 40];
  const out = histogramChart({ edges, counts: [5, 0, 20, 10], xLabel: 'Transcripts', lines: [{ value: 25 }, { value: 99 }] }, frame);
  const bars = out.marks.find((m) => m.type === 'group').marks;
  assert.equal(bars.length, 3);
  assert.equal(out.drawn.bars, 3);
  // Heights in proportion: 20 is twice 10, and four times 5.
  const [b5, b20, b10] = bars;
  assert.ok(near(b20.h, 2 * b10.h, 1e-9) && near(b20.h, 4 * b5.h, 1e-9));
  assert.ok(near(b5.w, b20.w) && b5.x < b20.x);
  // One threshold within the axis; the other beyond it, left out.
  assert.equal(out.marks.filter((m) => m.type === 'line' && m.stroke === '#d63b4a').length, 1);
  assert.ok(histogramChart({ edges, counts: [0, 0, 0, 0] }, frame).drawn.empty);
});

test('a forest: each estimate on a log axis with its interval, the reference line at 1, an untested row its note', () => {
  const out = forestChart({ rows: [{ label: 'endothelial-like', estimate: 2, lo: 1, hi: 4, marked: true }, { label: 'immune-like', estimate: 0.5, lo: 0.25, hi: 1 }, { label: 'none', estimate: NaN, note: 'untested' }], log: true, xLabel: 'Odds ratio' }, frame);
  const rows = out.drawn.rows;
  const sx = out.drawn.sx;
  // On a log axis 2 and 0.5 lie as far from 1 on each side; 4 twice as far as 2.
  assert.ok(near(sx(2) - sx(1), sx(1) - sx(0.5), 1e-9) && near(sx(4) - sx(1), 2 * (sx(2) - sx(1)), 1e-9));
  assert.ok(near(rows[0].x, sx(2)) && near(rows[0].lo, sx(1)) && near(rows[0].hi, sx(4)));
  assert.equal(rows[2].x, null);
  assert.ok(flat(out.marks).some((m) => m.type === 'text' && m.text === 'untested'));
  assert.ok(flat(out.marks).some((m) => m.type === 'rect' && m.fill === '#d63b4a'), 'a row found in red');
});

test('a heat map: a square for each pair with a color, a dot where marked, names on two sides, a color bar', () => {
  const names = ['A', 'B', 'C'];
  const out = heatmapChart({ rows: names, color: (i, j) => (i === 2 && j === 2 ? null : `rgb(${i}, ${j}, 0)`), marked: (i, j) => i === j, bar: { stops: ['rgb(0, 0, 255)', 'rgb(255, 255, 255)', 'rgb(255, 0, 0)'], low: 'fewer', high: 'more' } }, frame);
  assert.deepEqual([out.drawn.squares, out.drawn.dots], [8, 2]);
  const texts = textMarks(out.marks).map((m) => m.text);
  assert.deepEqual(texts.filter((x) => names.includes(x)).length, 6);
  assert.ok(texts.includes('fewer') && texts.includes('more'));
  assert.equal(out.marks.filter((m) => m.type === 'image').length, 1);
});

test('a volcano: genes found drawn on top, the strongest named; dots by group with their means', () => {
  const points = [{ gene: 'A', x: -1, y: 1, found: false }, { gene: 'SIM033', x: 1.2, y: 6, found: true }, { gene: 'B', x: 2, y: 3, found: true }];
  const v = volcanoChart({ points, named: 1 }, frame);
  const dots = v.marks.find((m) => m.type === 'group').marks;
  assert.deepEqual(dots.map((d) => d.fill), ['#5b6478', '#d63b4a', '#d63b4a']);
  assert.deepEqual(v.drawn.named, ['SIM033']);
  const g = groupDotsChart({ groups: [{ group: 'control', points: [{ value: 1 }, { value: 3 }], mean: 2 }, { group: 'treated', points: [{ value: 4 }], mean: 4 }], yLabel: 'log2 CPM' }, frame);
  assert.equal(g.drawn.dots, 3);
  assert.equal(g.marks.filter((m) => m.type === 'line' && m.stroke === '#171b26').length, 2);
  // The means where their values are.
  const means = g.marks.filter((m) => m.type === 'line' && m.stroke === '#171b26');
  assert.ok(near(means[0].y1, g.drawn.sy(2)) && near(means[1].y1, g.drawn.sy(4)));
});

test('a map: one unit the same across and down; bars of genes; a panel with its title and legend', () => {
  const s = scatterChart({ x: [0, 10, 5], y: [0, 2, 1], colors: ['#ff0000', '#00ff00', '#0000ff'], xLabel: 'UMAP 1', yLabel: 'UMAP 2', equal: true }, frame);
  const pts = s.marks.find((m) => m.type === 'group').marks;
  assert.equal(pts.length, 3);
  // The points' spread across is five times their spread down, as in the data.
  const dx = pts[1].x - pts[0].x;
  const dy = pts[0].y - pts[1].y;
  assert.ok(near(dx / dy, 5, 0.05));
  const b = barChart({ items: [{ label: 'GJA5', value: 0.436 }, { label: 'IGFBP3', value: 0.411 }], xLabel: 'Moran\'s I', reference: 0 }, frame);
  assert.equal(b.drawn.bars, 2);
  const p = chartPanel({ x: 0, y: 0, w: 200, h: 150 }, { title: 'A map', chart: { kind: 'scatter', spec: { x: [0, 1], y: [0, 1], colors: '#ff0000', equal: true } }, legend: [{ label: 'T cells', color: '#ff0000' }], facts: { points: 2 } }, { text: 7 });
  assert.deepEqual(textMarks(p.marks).map((m) => m.text).filter((x) => x === 'A map' || x === 'T cells'), ['A map', 'T cells']);
  assert.deepEqual(p.facts.gathered, { points: 2 });
});

test('a figure of result panels: each drawn in its box, its words, nothing below 6 pt', () => {
  let fig = createFigure('Results', 'double', { id: 'fig-r' });
  fig = addPanel(fig, { kind: 'qc', dataset: 'd-1', metric: 'transcripts' }, { columns: 2 }).figure;
  fig = addPanel(fig, { kind: 'cohort', comparison: 'abu-1', plot: 'forest' }, { columns: 2 }).figure;
  checkFigure(fig);
  assert.throws(() => checkFigure({ ...fig, panels: [{ ...fig.panels[1], plot: 'pie' }] }), /forest, units, volcano, gene/);
  const resolved = new Map([
    ['panel-1', { title: 'Gene transcripts', chart: { kind: 'histogram', spec: { edges: [0, 1, 2], counts: [3, 4] } }, description: 'A distribution.' }],
    ['panel-2', { title: 'Shares', chart: { kind: 'forest', spec: { rows: [{ label: 'a', estimate: 2, lo: 1, hi: 3 }] } }, description: 'Effects.' }],
  ]);
  const drawn = figureMarks(fig, resolved);
  assert.match(drawn.description, /Panel A: A distribution\. Panel B: Effects\./);
  assert.ok(drawn.smallest >= 6);
  assert.equal(drawn.panels[0].facts.chart, 'histogram');
});
