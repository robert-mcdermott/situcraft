import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addView, captureView, parseViewLink, removeView, renameView, sameView, updateView, viewCamera, viewLayers, viewLink } from './views.js';
import { createProject } from './project.js';

const desc = {
  channels: [{ name: 'DAPI' }, { name: 'ATP1A1/CD45/E-Cadherin' }],
  transcripts: { genes: [{ name: 'ACTA2' }, { name: 'EPCAM' }, { name: 'PTPRC' }] },
};
const defaults = {
  channels: [{ visible: true, color: '#4f7cff', window: [10, 100], gamma: 1 }, { visible: true, color: '#3ddc84', window: [5, 50], gamma: 1 }],
  transcripts: { visible: true, all: false, genes: [], colors: [], minQ: 20, pointSize: 3, gain: 1, allColor: '#ffd84d' },
  cells: { outlines: true, nuclei: false, fill: null, opacity: 0.55 },
};

function shown() {
  return {
    camera: { cx: 412.5, cy: 301.25, scale: 6 },
    ratio: 2,
    layers: {
      channels: [{ visible: true, color: '#ff4d4d', window: [73, 4000], gamma: 0.8 }, { visible: false, color: '#3ddc84', window: [5, 50], gamma: 1 }],
      transcripts: { visible: true, all: false, genes: [1, 2], colors: ['#000', '#111'], minQ: 20, pointSize: 4, gain: 1.5 },
      cells: { outlines: true, nuclei: true, fill: 'area', opacity: 0.4 },
    },
    cell: { row: 42, id: 'aaafiiei-1' },
  };
}

test('a view records what is shown, by name, and the zoom per CSS pixel', () => {
  const view = captureView({ name: 'Tumor edge', dataset: 'd1', state: shown(), desc, id: 'v-1', now: '2026-10-15T00:00:00Z' });
  assert.equal(view.camera.umPerCss, 0.333333333333);
  assert.deepEqual(view.transcripts.genes, ['EPCAM', 'PTPRC']);
  assert.equal(view.channels[0].name, 'DAPI');
  assert.deepEqual(view.channels[0].window, [73, 4000]);
  assert.deepEqual(view.cell, { row: 42, id: 'aaafiiei-1' });
  assert.equal(view.segmentation, 'instrument');
  assert.equal(view.qc, undefined);
  const qc = captureView({ name: 'QC', dataset: 'd1', mode: 'qc', state: { ...shown(), qc: { map: 'bin:outside', filter: 'f-1' } }, desc });
  assert.deepEqual(qc.qc, { map: 'bin:outside', filter: 'f-1' });
});

test('a view reopens exactly, on a screen of another pixel ratio too', () => {
  const view = captureView({ name: 'v', dataset: 'd1', state: shown(), desc, id: 'v-1' });
  // Saved and read back, as the project keeps it.
  const stored = JSON.parse(JSON.stringify(view));
  const { layers, missing } = viewLayers(stored, desc, defaults);
  assert.deepEqual(missing, []);
  assert.deepEqual(layers.transcripts.genes, [1, 2]);
  for (const ratio of [1, 2, 3]) {
    const camera = viewCamera(stored, ratio);
    const again = captureView({ name: 'v', dataset: 'd1', state: { camera, ratio, layers, cell: stored.cell }, desc, id: 'v-2' });
    assert.ok(sameView(view, again), `ratio ${ratio}`);
  }
  // A zoom whose conversion through the pixel ratio is inexact in floating point (3 ÷ (3 ÷ 0.1)).
  const tenth = captureView({ name: 'v', dataset: 'd1', state: { ...shown(), camera: { cx: 5, cy: 5, scale: 10 }, ratio: 1 }, desc });
  assert.equal(tenth.camera.umPerCss, 0.1);
  const back = captureView({ name: 'v', dataset: 'd1', state: { ...shown(), camera: viewCamera(tenth, 3), ratio: 3 }, desc });
  assert.equal(back.camera.umPerCss, 0.1);
  // Something different is not the same view.
  const moved = captureView({ name: 'v', dataset: 'd1', state: { ...shown(), camera: { cx: 412.6, cy: 301.25, scale: 6 } }, desc });
  assert.equal(sameView(view, moved), false);
});

test('what a dataset lacks is named, and the rest applied', () => {
  const view = captureView({ name: 'v', dataset: 'd1', state: shown(), desc });
  const other = { channels: [{ name: 'DAPI' }], transcripts: { genes: [{ name: 'PTPRC' }] } };
  const { layers, missing } = viewLayers(view, other, { ...defaults, channels: [defaults.channels[0]] });
  assert.deepEqual(missing, ['channel ATP1A1/CD45/E-Cadherin', 'gene EPCAM']);
  assert.deepEqual(layers.transcripts.genes, [0]);
  assert.equal(layers.channels[0].color, '#ff4d4d');
});

test('views in a project, and their links', () => {
  let project = createProject('p', { id: 'p-abc' });
  const view = captureView({ name: 'First', dataset: 'd1', state: shown(), desc, id: 'v-1' });
  project = addView(project, view);
  assert.equal(project.views.length, 1);
  project = renameView(project, 'v-1', 'Edge');
  assert.equal(project.views[0].name, 'Edge');
  project = updateView(project, { ...project.views[0], camera: { cx: 1, cy: 2, umPerCss: 0.5 } }, '2026-10-15T01:00:00Z');
  assert.equal(project.views[0].camera.cx, 1);
  assert.equal(project.views[0].updated, '2026-10-15T01:00:00Z');
  assert.equal(removeView(project, 'v-1').views.length, 0);
  assert.equal(project.history.length, 2);
  const link = viewLink('http://127.0.0.1:8840/#old', 'p-abc', 'v-1');
  assert.equal(link, 'http://127.0.0.1:8840/#project=p-abc&view=v-1');
  assert.deepEqual(parseViewLink(new URL(link).hash), { project: 'p-abc', view: 'v-1' });
  assert.equal(parseViewLink('#project=p-abc'), null);
});
