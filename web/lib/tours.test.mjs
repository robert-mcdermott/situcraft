// The guided tours and the examples' attribution (lib/tours.js, lib/examples.js).

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
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
  // The downloaded examples', and the simulated ones made on this computer (api_examples.go).
  assert.deepEqual(Object.keys(WORKED).sort(), [...Object.keys(sources), 'simulated-standard', 'simulated-cohort', 'simulated-null-cohort', 'simulated-paired-cohort'].sort());
  const roles = new Set(['instrument', 'expansion-5', 'expansion-3', 'planted', 'refined', 'truth']);
  // A spatial question as a step asks it: its question, segmentation, labels and level, region,
  // niches' window and count.
  const answer = (q) => `answer:${[q.question, q.segmentation ?? 'instrument', q.labels ?? 'markers', q.level ?? 0, q.region ?? '', q.niches ?? '', q.neighbors ?? ''].join('|')}`;
  // The starter marker sets (internal/label/starters), and the simulated tissue's own.
  const starters = new Set(['simulated', ...readdirSync(join(root, 'internal', 'label', 'starters')).map((f) => f.replace(/\.json$/, ''))]);
  for (const [name, list] of Object.entries(WORKED)) {
    for (const w of list) {
      assert.equal(guideFor(name, w.id), w);
      assert.ok(w.question.endsWith('?') && w.expect.length > 80 && w.steps.length >= 5, w.id);
      // What the steps have made: segmentations by role, comparisons (a|b), clusterings and labels
      // of a segmentation (clustering:role, labels:role).
      const made = new Set(['instrument']);
      // A cohort's: the design drafted (its name), the steps run, the summary made, comparisons
      // made of a design.
      let drafted = null;
      for (const step of w.steps) {
        assert.ok(['segmentation', 'compare', 'viewer', 'qc', 'data', 'celltypes', 'alignment', 'spatial', 'cohort'].includes(step.mode), step.title);
        assert.ok(step.mode !== 'alignment' || step.alignment, `${step.title}: an Alignment view step says what it shows`);
        assert.ok(step.title && step.text.length > 40, step.title);
        if (step.do) {
          const d = step.do;
          assert.ok(step.label, `${step.title} says what it does`);
          if (d.action === 'expand') made.add(`expansion-${d.distance}`);
          if (d.action === 'plant') made.add('planted');
          // A refinement is compared with the instrument's at once (api_refine.go).
          if (d.action === 'refine') made.add('refined').add('instrument|refined');
          if (d.action === 'import-truth') made.add('truth');
          if (d.action === 'compare') {
            assert.ok(made.has(d.a) && made.has(d.b), `${step.title}: its segmentations are made before`);
            made.add(`${d.a}|${d.b}`);
          }
          // Cell types: in the Cell types view, of segmentations made before.
          if (['cluster', 'label', 'import-truth-labels', 'remake'].includes(d.action)) assert.equal(step.mode, 'celltypes', step.title);
          if (d.action === 'cluster') {
            assert.ok(made.has(d.segmentation ?? 'instrument'), `${step.title}: its segmentation is made before`);
            made.add(`clustering:${d.segmentation ?? 'instrument'}`);
          }
          if (d.action === 'label') {
            assert.ok(made.has(`clustering:${d.segmentation ?? 'instrument'}`) && starters.has(d.markers), `${step.title}: its clustering is made before, its markers a starter set`);
            made.add(`labels:${d.segmentation ?? 'instrument'}`);
          }
          if (d.action === 'import-truth-labels') {
            assert.ok(made.has('truth'), `${step.title}: the truth is imported before`);
            made.add('labels:truth');
          }
          if (d.action === 'remake') {
            assert.ok(roles.has(d.to) && made.has(d.to) && made.has(`labels:${d.from}`), `${step.title}: its labels and the other segmentation are made before`);
            made.add(`clustering:${d.to}`).add(`labels:${d.to}`);
          }
          // Post-Xenium images (the simulated example's): added, then aligned, checked, drawn on.
          if (d.action === 'add-image') {
            assert.ok(w.images && ['he', 'if'].includes(d.image), `${step.title}: an image of the example's`);
            made.add(`image:${d.image}`);
            if (d.alignment === 'matrix') made.add(`version:${d.image}:matrix`);
          }
          if (['landmarks', 'register', 'check', 'write-regions', 'read-regions'].includes(d.action)) assert.ok(made.has(`image:${d.image}`), `${step.title}: its image is added before`);
          if (d.action === 'landmarks') {
            assert.ok(d.count >= 3 && d.within.length === 4, step.title);
            made.add(`version:${d.image}:landmarks`);
          }
          if (d.action === 'register') made.add(`version:${d.image}:automatic`);
          if (d.action === 'check') assert.ok(d.versions.every((m) => made.has(`version:${d.image}:${m}`)), `${step.title}: its versions are made before`);
          if (d.action === 'draw-regions') d.regions.forEach((r) => made.add(`region:${r.name}`));
          if (d.action === 'write-regions' || d.action === 'read-regions') assert.ok([...made].some((x) => x.startsWith('region:')), `${step.title}: its regions are drawn before`);
          if (d.action === 'import-instrument-types') made.add('labels:true-types');
          if (d.action === 'review-queue') assert.ok(step.mode === 'celltypes' && made.has('labels:instrument') && d.first > 0, `${step.title}: the labels are made before`);
          if (d.action === 'exclude-fold') assert.ok(step.mode === 'viewer' && d.region.name && d.note, step.title);
          // Spatial: a graph of a segmentation made before; a question of its labels (or the truth's
          // types), over a graph built before, across a region drawn before; an answer made again
          // on a segmentation made before; the truth's domains imported.
          if (['graph', 'ask', 'remake-analysis', 'import-truth-domains'].includes(d.action)) assert.equal(step.mode, 'spatial', step.title);
          if (d.action === 'graph') {
            assert.ok(made.has(d.segmentation ?? 'instrument') && ['delaunay', 'knn', 'radius', 'contact'].includes(d.kind), `${step.title}: its segmentation is made before`);
            made.add(`graph:${d.segmentation ?? 'instrument'}:${d.kind}`);
          }
          if (d.action === 'ask') {
            const r = d.segmentation ?? 'instrument';
            if (d.question !== 'moran') assert.ok(made.has(d.labels === 'true-types' ? 'labels:truth' : `labels:${r}`), `${step.title}: its labels are made before`);
            if (d.question === 'enrichment' || d.question === 'moran') assert.ok(made.has(`graph:${r}:delaunay`), `${step.title}: its graph is built before`);
            if (d.region !== undefined) assert.ok(made.has(`region:${d.region}`), `${step.title}: its region is drawn before`);
            made.add(answer(d));
          }
          if (d.action === 'remake-analysis') {
            assert.ok(made.has(answer(d)) && roles.has(d.to) && made.has(d.to), `${step.title}: its answer and the other segmentation are made before`);
            made.add(`${answer(d)}>${d.to}`);
          }
          if (d.action === 'import-truth-domains') {
            assert.ok(made.has('truth'), `${step.title}: the truth is imported before`);
            made.add('domains:truth');
          }
          // Cohorts: in the Cohort view; regions made samples once drawn; the design before the
          // comparisons (each found by the design's name), the steps before the summary, the
          // summary before the comparisons.
          if (['sample-fields', 'region-samples', 'cohort-design', 'cohort-steps', 'summarize', 'compare-abundance', 'compare-expression', 'compare-spatial'].includes(d.action)) assert.equal(step.mode, 'cohort', step.title);
          if (d.action === 'region-samples') assert.ok(d.regions.every((r) => made.has(`region:${r}`)), `${step.title}: its regions are drawn before`);
          if (d.action === 'cohort-design') {
            assert.ok(d.unit && d.group && d.reference && (d.refused || d.save), step.title);
            drafted = d.refused ? null : d.save;
          }
          if (d.action === 'cohort-steps') made.add('cohort:steps');
          if (d.action === 'summarize') {
            assert.ok(made.has('cohort:steps'), `${step.title}: the steps are run before`);
            made.add('cohort:summary');
          }
          if (d.action.startsWith('compare-')) {
            assert.ok(made.has('cohort:summary') && drafted, `${step.title}: the summary and a design are made before`);
            made.add(`${d.action.slice('compare-'.length)}:${drafted}`);
          }
        }
        if (step.cohort) {
          const c = step.cohort;
          assert.ok(step.mode === 'cohort' && ['samples', 'design', 'steps', 'summary', 'qc', 'abundance', 'expression', 'spatial'].includes(c.show), step.title);
          // A comparison shown is made before (by its step, which shows its section while it runs).
          if (['abundance', 'expression', 'spatial'].includes(c.show) && !step.do?.action?.startsWith('compare-')) assert.ok(made.has(`${c.show}:${c.design}`), `${step.title}: its comparison is made before`);
        }
        if (step.spatial) {
          assert.equal(step.mode, 'spatial', step.title);
          assert.ok(made.has(answer(step.spatial)), `${step.title}: its answer is made before`);
          if (step.spatial.to) assert.ok(made.has(`${answer(step.spatial)}>${step.spatial.to}`), `${step.title}: its answer is made again before`);
        }
        if (step.cells_of) assert.ok(step.mode === 'viewer' && made.has(answer(step.cells_of)), `${step.title}: its answer is made before`);
        if (step.images) assert.ok(Object.keys(step.images).every((k) => made.has(`image:${k}`)), `${step.title}: its images are added before`);
        if (step.alignment) {
          const a = step.alignment;
          assert.ok(step.mode === 'alignment' && made.has(`image:${a.image}`), `${step.title}: its image is added before`);
          assert.ok(a.show === 'in use' || made.has(`version:${a.image}:${a.show}`), `${step.title}: the version it shows is made before`);
        }
        if (step.segmentation) assert.ok(step.mode === 'viewer' && roles.has(step.segmentation) && made.has(step.segmentation), `${step.title}: its segmentation is made before`);
        if (step.compare) {
          assert.ok(roles.has(step.compare.a) && roles.has(step.compare.b), step.title);
          assert.ok(made.has(`${step.compare.a}|${step.compare.b}`), `${step.title}: its comparison is made before`);
          if (step.compare.tab === 'labels') assert.ok(made.has(`labels:${step.compare.a}`) && made.has(`labels:${step.compare.b}`), `${step.title}: both sides' labels are made before`);
        }
        if (step.celltypes) {
          const c = step.celltypes;
          const r = c.segmentation ?? 'instrument';
          assert.equal(step.mode, 'celltypes', step.title);
          assert.ok(made.has(`clustering:${r}`), `${step.title}: its clustering is made before`);
          if (/^(labels|confidence)/.test(c.color ?? '') || c.review || c.section === 'labels') assert.ok(made.has(`labels:${r}`), `${step.title}: its labels are made before`);
          if (c.section) assert.ok(['map', 'selection', 'clusters', 'labels', 'review'].includes(c.section), step.title);
        }
        const fill = step.cells?.fill ?? '';
        if (fill === 'clusters' || /^labels/.test(fill)) assert.ok(made.has(`${fill === 'clusters' ? 'clustering' : 'labels'}:${step.segmentation ?? 'instrument'}`), `${step.title}: what fills the cells is made before`);
        if (fill === 'true-types') assert.ok(made.has('labels:true-types'), `${step.title}: the true types are imported before`);
        if (fill === 'niches') assert.ok(step.niches && made.has(answer({ ...step.niches, question: 'niches' })), `${step.title}: its niches are found before`);
        if (fill === 'true-domains') assert.ok(made.has('domains:truth'), `${step.title}: the truth's domains are imported before`);
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
