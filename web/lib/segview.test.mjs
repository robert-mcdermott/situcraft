import { test } from 'node:test';
import assert from 'node:assert/strict';
import { CERTAINTY_RANGE, LIMITATIONS, SEVERAL, certaintyColor, defaultChoice, howMade, probabilityColors, probabilitySlot, readSegCells, segFillColors, segFills, segmentationChoices } from './segview.js';
import { hexToRgb } from './colormaps.js';

const rec = (id, source, extra = {}) => ({ kind: 'segmentation', id, name: id, dataset: 'd1', source, state: 'complete', method: { name: '', version: '', methods: [] }, parameters: null, software: [{ name: 'situcraft', version: '0.3.0' }], ...extra });

test('the segmentations a dataset can show: the complete ones, the default first', () => {
  const project = {
    artifacts: [rec('inst', 'instrument'), rec('exp', 'situcraft', { parameters: { distance_um: 5 } }), rec('bad', 'situcraft', { state: 'failed' }), rec('other', 'instrument', { dataset: 'd2' })],
    decisions: [{ kind: 'promote-segmentation', dataset: 'd1', artifact: 'inst' }, { kind: 'promote-segmentation', dataset: 'd1', artifact: 'exp' }],
  };
  const choices = segmentationChoices(project, 'd1');
  assert.deepEqual(choices.map((c) => [c.id, c.isDefault, c.instrument]), [['exp', true, false], ['inst', false, true]]);
  assert.equal(defaultChoice(project, 'd1').id, 'exp');
  assert.equal(defaultChoice({ ...project, decisions: [] }, 'd1').id, 'inst');
  assert.equal(defaultChoice(project, 'none'), null);
});

test('how each kind of segmentation was made, in plain words', () => {
  assert.equal(howMade(rec('i', 'instrument', { method: { methods: [{ method: 'Segmented by nucleus expansion of 5.0µm', cells: 10 }, { method: 'Segmented by boundary stain', cells: 0 }] } })), 'the instrument\'s own: nucleus expansion of 5.0 µm');
  assert.equal(howMade(rec('e', 'situcraft', { parameters: { distance_um: 2.5 } })), 'nucleus expansion by 2.5 µm, by SituCraft 0.3.0');
  assert.equal(howMade(rec('r', 'situcraft', { method: { name: 'SituCraft transcript-informed segmentation' }, parameters: { preset: 'balanced', stages: [{ sweeps: 100 }, { sweeps: 500 }], seed: 20250 } })), 'transcript-informed, Balanced, 600 sweeps, seed 20250, by SituCraft 0.3.0');
  assert.equal(howMade(rec('p', 'imported', { method: { name: 'Proseg', version: '3.2.0' } })), 'Proseg 3.2.0, imported');
  assert.match(howMade(rec('x', 'planted')), /simulated/);
});

// A table as api_segview.go encodes it.
function table(n, holds, columns) {
  let length = 16;
  for (const c of columns) length += c.byteLength;
  length = (length + 3) & ~3;
  const buffer = new ArrayBuffer(length);
  new Uint8Array(buffer, 0, 4).set([...'SCVS'].map((c) => c.charCodeAt(0)));
  new Uint32Array(buffer, 4, 3).set([n, holds, 11]);
  let at = 16;
  for (const c of columns) {
    new Uint8Array(buffer, at, c.byteLength).set(new Uint8Array(c.buffer, c.byteOffset, c.byteLength));
    at += c.byteLength;
  }
  return buffer;
}

test('a segmentation\'s cells table, its fills and what it holds', () => {
  const f = (...v) => new Float32Array(v);
  const u = (...v) => new Uint32Array(v);
  const cells = readSegCells(table(3, 7, [f(1, 2, 3), f(4, 5, 6), f(50, 60, 70), f(20, NaN, 25), u(10, 0, 30), u(12, 1, 33), u(5, 0, 9), u(0, 0xffffffff, 2), f(0.95, 0.6, NaN), f(0.9, NaN, 0.5), new Uint8Array([0, 3, 4])]));
  assert.equal(cells.n, 3);
  assert.deepEqual(cells.holds, { limitations: true, certainty: true, probability: true });
  assert.deepEqual([...cells.total], [12, 1, 33]);
  assert.equal(cells.instrument[1], 0xffffffff);
  assert.deepEqual(segFills(cells).map((x) => x.id), ['transcripts', 'total', 'area', 'nucleusArea', 'limitations', 'certainty', 'probability']);
  const bare = readSegCells(table(1, 0, [f(1), f(1), f(1), f(1), u(1), u(1), u(1), u(0), f(NaN), f(NaN), new Uint8Array([0])]));
  assert.deepEqual(segFills(bare).map((x) => x.id), ['transcripts', 'total', 'area', 'nucleusArea']);
  assert.equal(segFillColors('certainty', bare, () => null), null);
  // Limitations: unflagged cells clear, one in its color, several in red.
  const lim = segFillColors('limitations', cells, () => null);
  assert.deepEqual([...lim.slice(0, 4)], [0, 0, 0, 0]);
  assert.deepEqual([...lim.slice(4, 8)], [...hexToRgb(SEVERAL.color), 255]);
  assert.deepEqual([...lim.slice(8, 12)], [...hexToRgb(LIMITATIONS.find((l) => l.bit === 4).color), 255]);
  // Certainty: unknown clear, doubtful brighter than certain.
  const cert = segFillColors('certainty', cells, () => null);
  assert.equal(cert[11], 0);
  const brightness = (i) => cert[4 * i] + cert[4 * i + 1] + cert[4 * i + 2];
  assert.ok(brightness(1) > brightness(0));
  // Counts and areas go to the dataset's own numeric coloring.
  let asked = null;
  segFillColors('total', cells, (values) => {
    asked = values;
    return new Uint8Array(12);
  });
  assert.equal(asked, cells.total);
  assert.throws(() => readSegCells(new ArrayBuffer(8)));
});

test('the certainty scale and transcripts by their probability', () => {
  const lum = (hex) => hexToRgb(hex).reduce((a, b) => a + b, 0);
  assert.ok(lum(certaintyColor(0.2)) === lum(certaintyColor(CERTAINTY_RANGE[0])));
  assert.ok(lum(certaintyColor(0.6)) > lum(certaintyColor(0.9)));
  assert.equal(probabilitySlot(NaN), 15);
  assert.equal(probabilitySlot(0.1), 0);
  assert.equal(probabilitySlot(0.5), 0);
  assert.equal(probabilitySlot(1), 14);
  assert.ok(probabilitySlot(0.7) < probabilitySlot(0.8));
  const colors = probabilityColors();
  assert.equal(colors.length, 16);
  assert.ok(lum(colors[0]) > lum(colors[14]));
});
