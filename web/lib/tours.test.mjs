// The guided tours and the examples' attribution (lib/tours.js, lib/examples.js).

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { TOURS, WORKED, guideFor, segmentationByRole, stepView, tourGenes, workedExamples } from './tours.js';
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

test('every worked example asks its question, says what to expect, and its steps can be done', () => {
  // The downloaded examples', and the simulated one made on this computer (api_examples.go).
  assert.deepEqual(Object.keys(WORKED).sort(), [...Object.keys(sources), 'simulated-standard'].sort());
  const roles = new Set(['instrument', 'expansion-5', 'planted', 'refined', 'truth']);
  for (const [name, list] of Object.entries(WORKED)) {
    for (const w of list) {
      assert.equal(guideFor(name, w.id), w);
      assert.ok(w.question.endsWith('?') && w.expect.length > 80 && w.steps.length >= 5, w.id);
      const made = new Set(['instrument']);
      for (const step of w.steps) {
        assert.ok(['segmentation', 'compare', 'viewer', 'qc', 'data'].includes(step.mode), step.title);
        assert.ok(step.title && step.text.length > 40, step.title);
        if (step.do) {
          assert.ok(step.label, `${step.title} says what it does`);
          if (step.do.action === 'expand') made.add(`expansion-${step.do.distance}`);
          if (step.do.action === 'plant') made.add('planted');
          // A refinement is compared with the instrument's at once (api_refine.go).
          if (step.do.action === 'refine') made.add('refined').add('instrument|refined');
          if (step.do.action === 'import-truth') made.add('truth');
          if (step.do.action === 'compare') {
            assert.ok(made.has(step.do.a) && made.has(step.do.b), `${step.title}: its segmentations are made before`);
            made.add(`${step.do.a}|${step.do.b}`);
          }
        }
        if (step.segmentation) assert.ok(step.mode === 'viewer' && roles.has(step.segmentation) && made.has(step.segmentation), `${step.title}: its segmentation is made before`);
        if (step.compare) {
          assert.ok(roles.has(step.compare.a) && roles.has(step.compare.b), step.title);
          assert.ok(made.has(`${step.compare.a}|${step.compare.b}`), `${step.title}: its comparison is made before`);
        }
      }
    }
  }
  assert.equal(guideFor('xenium-lung-2fov'), TOURS['xenium-lung-2fov']);
  assert.equal(guideFor('xenium-lung-2fov', 'nothing'), null);
  assert.equal(workedExamples('nothing').length, 0);
});

test('segmentations are found by what they are', () => {
  const segs = [
    { id: 'a', source: 'instrument', state: 'complete' },
    { id: 'b', source: 'situcraft', state: 'complete', parameters: { distance_um: 5 } },
    { id: 'c', source: 'situcraft', state: 'complete', parameters: { distance_um: 10 } },
    { id: 'd', source: 'planted', state: 'invalid' },
    { id: 'e', source: 'planted', state: 'complete' },
  ];
  assert.equal(segmentationByRole(segs, 'instrument').id, 'a');
  assert.equal(segmentationByRole(segs, 'expansion-5').id, 'b');
  assert.equal(segmentationByRole(segs, 'expansion-10').id, 'c');
  assert.equal(segmentationByRole(segs, 'planted').id, 'e', 'a complete one');
  assert.equal(segmentationByRole(segs, 'expansion-2'), null);
});

test('segmentations by role: the latest refinement, and the truth imported from its folder', () => {
  const seg = (id, extra) => ({ id, state: 'complete', source: 'situcraft', method: { name: '' }, ...extra });
  const list = [
    seg('inst', { source: 'instrument' }),
    seg('r1', { method: { name: 'SituCraft transcript-informed segmentation' } }),
    seg('r2', { method: { name: 'SituCraft transcript-informed segmentation' } }),
    seg('other', { source: 'imported', sources: [{ path: '/data/other' }] }),
    seg('truth', { source: 'imported', sources: [{ path: '/data/simulated-standard/truth' }] }),
  ];
  assert.equal(segmentationByRole(list, 'refined').id, 'r2');
  assert.equal(segmentationByRole(list, 'truth').id, 'truth');
  assert.equal(segmentationByRole(list.slice(0, 1), 'refined'), null);
  // A Viewer step draws its segmentation and colors its transcripts as it says.
  const view = stepView({ mode: 'viewer', title: 't', transcripts: { all: true, colorBy: 'probability' }, cells: { fill: 'certainty' } }, 'd1', 0);
  assert.equal(view.transcripts.all, true);
  assert.equal(view.transcripts.colorBy, 'probability');
  assert.equal(view.cells.fill, 'certainty');
});
