// The guided tours and the examples' attribution (lib/tours.js, lib/examples.js).

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { TOURS, stepView, tourGenes } from './tours.js';
import { viewLayers } from './views.js';
import { exampleFor, exampleSource } from './examples.js';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const sources = JSON.parse(readFileSync(join(root, 'validation', 'sources.json'), 'utf8')).datasets;
const dataRoot = process.env.SITUCRAFT_DATA || join(root, 'validation', 'cache');

test('every example has a tour, and every step is complete', () => {
  assert.deepEqual(Object.keys(TOURS).sort(), Object.keys(sources).sort());
  for (const [name, tour] of Object.entries(TOURS)) {
    assert.ok(tour.title && tour.steps.length >= 5, name);
    assert.equal(tour.steps[0].mode, 'data', `${name} starts with its bundle`);
    for (const step of tour.steps) {
      assert.ok(['data', 'viewer', 'qc'].includes(step.mode), step.title);
      assert.ok(step.title && step.text.length > 40, step.title);
      if (step.camera) assert.ok(step.camera.umPerCss > 0 && step.camera.cx >= 0 && step.camera.cy >= 0, step.title);
    }
  }
});

test("a step's view opens through the saved views' path", () => {
  const desc = { channels: [{ name: 'DAPI' }], transcripts: { genes: [{ name: 'EPCAM' }, { name: 'CD68' }, { name: 'PLVAP' }] } };
  const defaults = {
    channels: [{ visible: true, color: '#4f8cff', window: [0, 100], gamma: 1 }],
    transcripts: { visible: true, all: false, genes: [], colors: [], minQ: 20, pointSize: 3, gain: 1 },
    cells: { outlines: true, nuclei: false, fill: null, opacity: 0.55 },
  };
  const tour = TOURS['xenium-lung-2fov'];
  assert.equal(stepView(tour.steps[0], 'd1', 0), null, 'a Data view step opens no view');
  const view = stepView(tour.steps[2], 'd1', 2);
  assert.equal(view.dataset, 'd1');
  assert.equal(view.id, 'tour-2');
  const { layers, missing } = viewLayers(view, desc, defaults);
  // EPCAM, COL5A2, PLVAP and CD68 in the step's order; this panel has no COL5A2.
  assert.deepEqual(layers.transcripts.genes, [0, 2, 1]);
  assert.deepEqual(missing, ['gene COL5A2']);
  assert.deepEqual(layers.channels[0], { visible: true, color: '#8c8c8c', window: [0, 100], gamma: 1 }, 'a step changes only what it names');
  const whole = stepView(tour.steps[1], 'd1', 1);
  assert.equal(whole.camera, null, 'the whole section');
  const qc = stepView(tour.steps.find((s) => s.mode === 'qc'), 'd1', 4);
  assert.deepEqual(qc.qc, { map: 'bin:outside', filter: null });
  const breast = TOURS['xenium-breast-2fov'].steps.find((s) => s.cells?.fill);
  assert.equal(stepView(breast, 'd2', 2).cells.fill, 'method');
});

// The genes each tour names are in its example's panel (when the example is on this computer).
for (const name of Object.keys(TOURS)) {
  const panel = join(dataRoot, name, 'gene_panel.json');
  test(`the ${name} tour's genes are in its panel`, { skip: !existsSync(panel) && 'the example is not on this computer' }, () => {
    const genes = new Set(JSON.parse(readFileSync(panel, 'utf8')).payload.targets.map((t) => t.type.data.name));
    for (const gene of tourGenes(name)) assert.ok(genes.has(gene), gene);
  });
}

test('a dataset is known as its example by its manifest, wherever it is', () => {
  const examples = [
    { name: 'a', title: 'Example A (XOA 2.0)', publisher: '10x Genomics', page: 'https://example.org/a', license: 'CC BY 4.0 (10x Genomics)', manifestSha256: 'aa' },
    { name: 'b', manifestSha256: 'bb' },
  ];
  assert.equal(exampleFor({ fingerprint: [{ path: 'cells.parquet', size: 1 }, { path: 'experiment.xenium', size: 2, sha256: 'bb' }] }, examples).name, 'b');
  assert.equal(exampleFor({ fingerprint: [{ path: 'outs/experiment.xenium', size: 2, sha256: 'aa' }] }, examples).name, 'a');
  assert.equal(exampleFor({ fingerprint: [{ path: 'experiment.xenium', size: 2, sha256: 'cc' }] }, examples), null);
  assert.equal(exampleFor({}, examples), null);
  assert.equal(exampleFor({ fingerprint: [{ path: 'experiment.xenium', sha256: 'aa' }] }, null), null);
  assert.deepEqual(exampleSource(examples[0]), { title: 'Example A (XOA 2.0)', attribution: '10x Genomics', license: 'CC BY 4.0', url: 'https://example.org/a' });
  assert.equal(exampleSource(null), null);
});
