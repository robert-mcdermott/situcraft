import assert from 'node:assert/strict';
import test from 'node:test';
import { addPanel, checkFigure, createFigure, figureMarks, figurePDF, figureSVG, mm, panelLetters, pngSize, saveFigure, removeFigure, figuresOf } from './figures.js';
import { channelTable, compositeStains, lengthText, legendLayout, niceLength, rasterSize, tissueLayout, tissuePanel } from './figure-tissue.js';
import { buildManifest, checkManifest, compareManifest, embedPNG, embedSVG, pdfAttachment, readFigureManifest, viewArtifacts, withResolution } from './figure-manifest.js';
import { encodePNG } from './png.js';

const near = (a, b, tol = 1e-12) => Math.abs(a - b) <= tol;

test('the scale bar: 1, 2 or 5 times a power of ten, the longest of a fifth of the width or less', () => {
  assert.deepEqual([250, 1000, 33, 7, 100000, 0.004, 400].map(niceLength), [50, 200, 5, 1, 20000, 0.001, 50]);
  assert.deepEqual([50, 2000, 0.5, 1500].map(lengthText), ['50 µm', '2 mm', '0.5 µm', '1.5 mm']);
});

test('a channel\'s table: windowed, clamped, raised to its gamma', () => {
  const t = channelTable([100, 1100], 1);
  assert.deepEqual([t[0], t[100], t[600], t[1100], t[60000]], [0, 0, 0.5, 1, 1]);
  const g = channelTable([0, 100], 2);
  assert.ok(near(g[50], 0.25) && near(g[10], 0.01));
  // A window of no width divides by 1, as the shader does.
  assert.deepEqual([...channelTable([5, 5], 1).subarray(4, 7)], [0, 0, 1]);
});

test('stains composited: each pixel the sample under its center, the channels added and clamped', () => {
  // A 4 × 2 grid of 1 µm samples from (10, 20).
  const a = { window: [0, 100], gamma: 1, color: '#ff0000', grid: { x0: 10, y0: 20, um: 1, cols: 4, rows: 2, data: new Uint16Array([0, 50, 100, 200, 25, 75, 100, 0]) } };
  const b = { window: [0, 100], gamma: 1, color: [1, 1, 0], grid: { x0: 10, y0: 20, um: 1, cols: 4, rows: 2, data: new Uint16Array([100, 100, 0, 0, 0, 0, 0, 0]) } };
  const one = compositeStains({ width: 4, height: 2, region: { x0: 10, y0: 20, x1: 14, y1: 22 }, channels: [a] });
  assert.deepEqual([...one].filter((_, i) => i % 4 === 0), [0, 128, 255, 255, 64, 191, 255, 0]);
  assert.ok([...one].filter((_, i) => i % 4 === 3).every((v) => v === 255));
  const both = compositeStains({ width: 4, height: 2, region: { x0: 10, y0: 20, x1: 14, y1: 22 }, channels: [a, b] });
  // Red 0.5 + yellow 1 clamps red to 1; green 1.
  assert.deepEqual([...both.subarray(4, 8)], [255, 255, 0, 255]);
  // Finer than the samples: each sample two pixels a side; beyond the grid, black.
  const fine = compositeStains({ width: 10, height: 4, region: { x0: 10, y0: 20, x1: 15, y1: 22 }, channels: [a] });
  assert.deepEqual([...fine].filter((_, i) => i % 4 === 0).slice(0, 10), [0, 0, 128, 128, 255, 255, 255, 255, 0, 0]);
});

test('layout: the image fills the width above the legend, the region follows its shape', () => {
  const box = { x: 10, y: 20, w: 200, h: 160 };
  const items = [{ group: 'Stains', name: 'DAPI', kind: 'square', color: '#8c8c8c' }, { group: 'Genes', name: 'EPCAM', kind: 'dot', color: '#ff0000' }];
  const l = tissueLayout(box, { region: { cx: 500, cy: 300, width: 400 } }, items, 7);
  assert.equal(l.image.w, 200);
  assert.equal(l.umPerPt, 2);
  assert.ok(near(l.region.y1 - l.region.y0, l.image.h * 2));
  assert.ok(near((l.region.y0 + l.region.y1) / 2, 300));
  assert.equal(l.legend.layout.rows.length, 1);
  assert.ok(near(l.image.h, 160 - l.legend.layout.height - 7 * 0.4));
  // A narrow legend wraps.
  assert.equal(legendLayout(items, 40, 7).rows.length > 1, true);
  assert.deepEqual(rasterSize(l.image, 300), { width: Math.round(200 * 300 / 72), height: Math.round(l.image.h * 300 / 72) });
});

const square = (cell, x, y, s) => ({ cell, xy: new Float64Array([x, y, x + s, y, x + s, y + s, x, y + s]) });

function tissueData() {
  const fill = new Uint8Array([0, 128, 255, 255, 255, 0, 0, 255, 0, 0, 0, 0]);
  return {
    dataset: 'Lung', view: 'Epithelium', background: '#000000',
    legend: [{ group: 'Cells', name: 'T cells', kind: 'square', color: '#0080ff' }],
    stains: null, channels: ['DAPI'],
    cells: { rings: [square(0, 100, 100, 10), square(1, 900, 900, 10), square(2, 120, 100, 10)], fill, fillName: 'labels', opacity: 0.5, outline: { color: '#ffffff', width: 0.25 } },
    transcripts: { x: new Float32Array([105, 130, 5000]), y: new Float32Array([105, 105, 5000]), gene: new Uint16Array([0, 1, 0]), genes: [{ name: 'EPCAM', color: '#ff0000' }, { name: 'CD68', color: '#00ff00' }] },
  };
}

test('a tissue panel: the cells and transcripts in its region, a scale bar of round length, said in words', () => {
  const box = { x: 0, y: 0, w: 200, h: 150 };
  const drawn = tissuePanel(box, tissueData(), { region: { cx: 150, cy: 150, width: 200 }, text: 7 });
  const f = drawn.facts;
  assert.equal(f.umPerPt, 1);
  assert.equal(f.cells, 2, 'the cell far away is not drawn');
  assert.equal(f.transcripts, 2);
  assert.deepEqual(f.scaleBar, { um: 20, pt: 20 });
  const group = drawn.marks[0];
  assert.equal(group.type, 'group');
  assert.deepEqual(group.clip, f.image);
  const paths = group.marks.filter((m) => m.type === 'path');
  // One fill path per color (blue and red), the third cell unfilled; one outline path of both.
  assert.deepEqual(paths.map((p) => [p.fill, (p.d.match(/M/g) ?? []).length]), [['#0080ff', 1], ['none', 2]]);
  const y = (v) => String(Math.round((v - f.region.y0) * 100) / 100);
  assert.equal(paths[0].d, `M50 ${y(100)}L60 ${y(100)}L60 ${y(110)}L50 ${y(110)}Z`);
  assert.equal(group.marks.filter((m) => m.type === 'circle').length, 2);
  assert.match(drawn.description, /^The tissue of Lung \(the saved view "Epithelium"\), 200 µm across: the DAPI stain; 2 cells outlined and filled by labels; 2 transcripts of EPCAM and CD68; the scale bar 20 µm\.$/);
  // The same data, the same marks.
  assert.deepEqual(tissuePanel(box, tissueData(), { region: { cx: 150, cy: 150, width: 200 }, text: 7 }).marks, drawn.marks);
});

const project = () => ({
  id: 'p-1', name: 'Test',
  datasets: [{ id: 'd-1', name: 'Lung', run: 'run', fingerprint: [{ path: 'experiment.xenium', size: 10, sha256: 'ab' }] }],
  artifacts: [{ id: 'lab-1', kind: 'labels', name: 'Lung labels' }],
  views: [{ id: 'v-1', name: 'Epithelium', dataset: 'd-1', segmentation: 'instrument', cells: { fill: 'labels:lab-1:0', outlines: true } }],
});

function figureWithPanels() {
  let fig = createFigure('Figure 1', 'double', { id: 'fig-1', now: '2026-10-19T00:00:00Z' });
  fig = addPanel(fig, { kind: 'tissue', view: 'v-1', region: { cx: 150, cy: 150, width: 200 } }, { columns: 2 }).figure;
  fig = addPanel(fig, { kind: 'text', text: 'A note' }, { columns: 2 }).figure;
  return checkFigure(fig);
}

test('a figure: panels side by side, lettered in reading order, drawn the same twice as SVG and PDF', async () => {
  const fig = figureWithPanels();
  assert.equal(fig.panels[1].y, fig.panels[0].y);
  assert.ok(fig.panels[1].x > fig.panels[0].x);
  assert.deepEqual([...panelLetters(fig).values()], ['A', 'B']);
  const resolved = new Map([['panel-1', tissueData()]]);
  const drawn = figureMarks(fig, resolved);
  assert.equal(drawn.smallest, 7);
  assert.match(drawn.description, /^Figure 1, 174 × 120 mm, 2 panels\. Panel A: The tissue of Lung/);
  const svg = await figureSVG(fig, resolved);
  assert.equal(svg, await figureSVG(fig, resolved));
  const date = new Date('2026-10-19T00:00:00Z');
  const pdf = await figurePDF(fig, resolved, { date });
  assert.deepEqual(pdf, await figurePDF(fig, resolved, { date }));
  assert.deepEqual(pngSize(fig, 300), { width: Math.round(mm(174) / 72 * 300), height: Math.round(mm(120) / 72 * 300), scale: 300 / 72 });
  // Kept in the project, checked.
  const p = saveFigure(project(), fig);
  assert.equal(figuresOf(p).length, 1);
  assert.equal(figuresOf(removeFigure(p, fig.id)).length, 0);
  assert.throws(() => saveFigure(project(), { ...fig, panels: [{ ...fig.panels[0], region: { cx: 0, cy: 0, width: 0 } }] }), /region/);
  assert.throws(() => checkFigure({ ...fig, text: 4 }), /6 to 36 pt/);
});

test('the manifest: built, embedded in SVG, PNG and PDF, read back; edits and changes found', async () => {
  const fig = figureWithPanels();
  const resolved = new Map([['panel-1', tissueData()]]);
  const drawn = figureMarks(fig, resolved);
  const record = buildManifest(project(), fig, drawn, { software: { version: '0.8.0', commit: 'abc' }, created: '2026-10-19T00:00:00Z' });
  assert.equal(checkManifest(record).ok, true);
  assert.deepEqual(record.panels[0].inputs.artifacts, [{ id: 'lab-1', kind: 'labels', name: 'Lung labels' }]);
  assert.equal(record.panels[0].facts.cells, 2);
  assert.deepEqual(viewArtifacts({ segmentation: 'seg-9', cells: { fill: 'clusters:clu-1' } }), [{ id: 'seg-9', kind: 'segmentation' }, { id: 'clu-1', kind: 'clustering' }]);
  const svg = embedSVG(await figureSVG(fig, resolved), record);
  assert.deepEqual(readFigureManifest(svg), record);
  const png = withResolution(embedPNG(await encodePNG(new Uint8Array(16).fill(255), 2, 2), record), 300);
  assert.deepEqual(readFigureManifest(png), record);
  assert.match(new TextDecoder('latin1').decode(png), /pHYs/);
  const pdf = await figurePDF(fig, resolved, { date: new Date('2026-10-19T00:00:00Z'), attachments: [pdfAttachment(record)] });
  assert.deepEqual(readFigureManifest(pdf), record);
  // Edited after it was written.
  assert.equal(checkManifest({ ...record, description: 'Other' }).ok, false);
  // The project now: unchanged; the view saved again; the labels removed.
  assert.deepEqual(compareManifest(record, project()).panels.map((p) => p.status), ['unchanged', 'unchanged']);
  const resaved = project();
  resaved.views[0] = { ...resaved.views[0], name: 'Epithelium again' };
  assert.match(compareManifest(record, resaved).panels[0].changes.join(), /saved again/);
  const gone = { ...project(), artifacts: [] };
  assert.equal(compareManifest(record, gone).panels[0].status, 'missing');
});

test('a drawing\'s digest: an image by its pixels, whatever compressed its PNG', async () => {
  const { panelDigests } = await import('./figure-manifest.js');
  const fig = createFigure('F', 'single', { id: 'fig-2' });
  const one = addPanel(fig, { kind: 'text', text: 'x' }).figure;
  const rgba = new Uint8Array(4 * 4).fill(200);
  const drawn = (pixels, png) => ({ marks: [{ type: 'rect' }, { type: 'group', marks: [{ type: 'image', x: 0, y: 0, w: 10, h: 10, image: { width: 2, height: 2, rgba: pixels, ...(png ? { png } : {}) } }] }] });
  const a = panelDigests(one, drawn(rgba)).get('panel-1');
  assert.equal(a, panelDigests(one, drawn(rgba, new Uint8Array([1, 2, 3]))).get('panel-1'));
  const other = rgba.slice();
  other[0] = 201;
  assert.notEqual(a, panelDigests(one, drawn(other)).get('panel-1'));
});
