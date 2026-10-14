import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addSegmentation, INSTRUMENT_DEFAULT, promoteSegmentation, promotedSegmentation, segmentationRecord, segmentationsOf, withInstrumentSegmentation } from './segmentations.js';

const NOW = '2026-10-14T12:00:00.000Z';
const manifest = (id, state = 'complete') => ({
  id, kind: 'segmentation', name: 'Instrument segmentation', state, created: NOW,
  lineage: {
    parents: [{ kind: 'bundle', id: '38f7f730', name: 'xenium-lung-2fov', sha256: 'aa' }],
    job: { id: 'j1-1', kind: 'segmentation', spec: { bundle: '/data/lung' } },
    method: { name: 'Xenium cell segmentation (Xenium Onboard Analysis)', version: 'xenium-2.0.0.10', citation: 'Janesick A, et al. 2023', detail: { methods: [{ method: 'Segmented by nucleus expansion of 5.0µm', cells: 11898 }] } },
    software: [{ name: 'Xenium Onboard Analysis', version: 'xenium-2.0.0.10' }, { name: 'situcraft', version: '0.2.0' }],
  },
  files: [{ path: 'cells.parquet', role: 'cells', size: 233148, sha256: 'b24a', location: 'bundle' }],
  findings: [{ severity: 'info', code: 'boundaries.self-intersecting', message: 'Cell boundaries with edges that cross', count: 44 }],
  summary: { cells: 11898 },
});

function project() {
  const p = createProject('Lung', { now: NOW });
  return { ...p, datasets: [{ id: 'd1', name: 'xenium-lung-2fov' }, { id: 'd2', name: 'xenium-breast-2fov' }] };
}

test("a dataset's instrument segmentation is recorded and promoted, once", () => {
  const p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  const [record] = segmentationsOf(p, 'd1');
  assert.equal(record.source, 'instrument');
  assert.equal(record.method.methods[0].cells, 11898);
  assert.equal(record.job.id, 'j1-1');
  assert.equal(record.files[0].location, 'bundle');
  assert.deepEqual(segmentationsOf(p, 'd2'), []);
  const promoted = promotedSegmentation(p, 'd1');
  assert.equal(promoted.record.id, 'seg-a');
  assert.equal(promoted.decision.rationale, INSTRUMENT_DEFAULT);
  assert.equal(promoted.decision.by, 'SituCraft');
  assert.equal(p.history.length, 2);
  // Registering again changes nothing.
  assert.equal(withInstrumentSegmentation(p, { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW), p);
  assert.equal(addSegmentation(p, segmentationRecord({ id: 'd1' }, manifest('seg-a'), NOW), NOW), p);
});

test('promotion needs a complete segmentation and a reason, and the latest decision holds', () => {
  let p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  p = addSegmentation(p, { ...segmentationRecord({ id: 'd1' }, manifest('seg-b'), NOW), name: 'Nucleus expansion 8 µm', source: 'situcraft' }, NOW);
  p = addSegmentation(p, { ...segmentationRecord({ id: 'd1' }, manifest('seg-c', 'invalid'), NOW), name: 'Broken' }, NOW);
  assert.throws(() => promoteSegmentation(p, { dataset: 'd1', segmentation: 'seg-b', rationale: '  ' }), /reason/);
  assert.throws(() => promoteSegmentation(p, { dataset: 'd1', segmentation: 'seg-c', rationale: 'x' }), /invalid/);
  assert.throws(() => promoteSegmentation(p, { dataset: 'd2', segmentation: 'seg-b', rationale: 'x' }), /no such/);
  const q = promoteSegmentation(p, { dataset: 'd1', segmentation: 'seg-b', rationale: 'Fewer transcripts outside cells', now: NOW });
  assert.equal(promotedSegmentation(q, 'd1').record.id, 'seg-b');
  assert.equal(q.decisions.length, 2, 'decisions are appended, never replaced');
  // An invalid instrument segmentation is recorded but never promoted.
  const r = withInstrumentSegmentation(project(), { id: 'd2', name: 'xenium-breast-2fov' }, manifest('seg-d', 'invalid'), NOW);
  assert.equal(segmentationsOf(r, 'd2').length, 1);
  assert.equal(promotedSegmentation(r, 'd2'), null);
});
