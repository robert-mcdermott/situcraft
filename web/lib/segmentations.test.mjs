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

test("SituCraft's nucleus expansion is recorded as its own, beside the default, with its parameters", () => {
  let p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  const expansion = {
    ...manifest('exp-b'), name: 'Nucleus expansion, 5 µm',
    lineage: {
      parents: [manifest('seg-a').lineage.parents[0], { kind: 'segmentation', id: 'seg-a', name: 'Instrument segmentation' }],
      job: { id: 'j2-1', kind: 'expansion', spec: { bundle: '/data/lung', distance_um: 5 } },
      method: { name: 'SituCraft nucleus expansion', version: '1', citation: 'Okabe A, et al. 2000' },
      parameters: { distance_um: 5, distance_px: 23.53 },
      software: [{ name: 'situcraft', version: '0.2.0' }],
    },
    files: [{ path: 'cells.bin', role: 'cells', size: 1000, sha256: 'cc' }],
    summary: { cells: 11898, agreement: { pixels: { agreement: 0.98 } } },
  };
  p = addSegmentation(p, segmentationRecord({ id: 'd1' }, expansion, NOW), NOW);
  const record = segmentationsOf(p, 'd1').find((a) => a.id === 'exp-b');
  assert.equal(record.source, 'situcraft');
  assert.equal(record.parameters.distance_um, 5);
  assert.deepEqual(record.parents.map((x) => x.kind), ['bundle', 'segmentation']);
  assert.equal(record.files[0].location, '', 'its files are its own, not the bundle\'s');
  assert.equal(record.summary.agreement.pixels.agreement, 0.98);
  assert.equal(promotedSegmentation(p, 'd1').record.id, 'seg-a', 'the instrument\'s stays the default');
});

test('an imported segmentation is recorded as imported, with its files and findings', () => {
  const imported = {
    ...manifest('imp-c'), name: 'Baysor (imported)', state: 'invalid',
    lineage: {
      parents: [manifest('seg-a').lineage.parents[0], { kind: 'import', id: 'ab12', name: 'segmentation.csv', sha256: 'ab12cd' }],
      job: { id: 'j3-1', kind: 'import' },
      method: { name: 'Baysor', version: '0.7.1', detail: { format: 'baysor', sources: [{ path: '/data/baysor/segmentation.csv', files: 1, size: 1200, sha256: 'ee11' }], files: [{ path: '/data/baysor/segmentation.csv', size: 1200, sha256: 'ee10' }] } },
      parameters: { format: 'baysor', units: 'um' },
      software: [{ name: 'Baysor', version: '0.7.1' }, { name: 'situcraft', version: '0.2.0' }],
    },
    findings: [{ severity: 'fatal', code: 'coordinates.extent', message: 'Only 3 of 7 cells lie within the image', count: 0 }],
  };
  let p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  p = addSegmentation(p, segmentationRecord({ id: 'd1' }, imported, NOW), NOW);
  const record = segmentationsOf(p, 'd1').find((a) => a.id === 'imp-c');
  assert.equal(record.source, 'imported');
  assert.equal(record.state, 'invalid');
  assert.equal(record.findings[0].code, 'coordinates.extent');
  assert.deepEqual(record.sources, [{ path: '/data/baysor/segmentation.csv', files: 1, size: 1200, sha256: 'ee11' }]);
  assert.throws(() => promoteSegmentation(p, { dataset: 'd1', segmentation: 'imp-c', rationale: 'x' }), /invalid/);
});
