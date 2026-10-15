import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addComparison, addExport, addSegmentation, comparisonRecord, comparisonsOf, decideSegmentation, EXPORT_FORMATS, exportRecord, exportsOf, INSTRUMENT_DEFAULT, PLANTED_METHOD, segmentationDecision, promoteSegmentation, promotedSegmentation, segmentationRecord, segmentationsOf, withInstrumentSegmentation } from './segmentations.js';

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

test('a comparison is recorded with its two segmentations, once', () => {
  let p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  const cmp = {
    id: 'cmp-1', kind: 'comparison', name: 'Instrument segmentation and Nucleus expansion, 5 µm', state: 'complete', created: NOW,
    lineage: {
      parents: [manifest('seg-a').lineage.parents[0], { kind: 'segmentation', id: 'seg-a' }, { kind: 'segmentation', id: 'exp-b' }],
      job: { id: 'j4-1', kind: 'comparison' }, method: { name: 'SituCraft segmentation comparison', version: '1' }, parameters: { within: 0.5 },
    },
    summary: { classes: { 'one-to-one': 11898 }, pixelAgreement: 0.980629 },
    findings: [],
  };
  const record = comparisonRecord({ id: 'd1' }, cmp, NOW);
  assert.equal(record.a, 'seg-a');
  assert.equal(record.b, 'exp-b');
  assert.equal(record.summary.classes['one-to-one'], 11898);
  p = addComparison(p, record, NOW);
  assert.equal(comparisonsOf(p, 'd1').length, 1);
  assert.match(p.history.at(-1).detail, /Compared Instrument segmentation \(A\) with exp-b \(B\)/);
  assert.equal(addComparison(p, record, NOW), p);
  assert.equal(segmentationsOf(p, 'd1').length, 1, 'a comparison is not a segmentation');
});

test('decisions: promote, keep or reject with a reason, appended; the default is never rejected', () => {
  let p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  p = addSegmentation(p, { ...segmentationRecord({ id: 'd1' }, manifest('exp-b'), NOW), name: 'Nucleus expansion, 5 µm', source: 'situcraft' }, NOW);
  p = addSegmentation(p, { ...segmentationRecord({ id: 'd1' }, manifest('imp-c'), NOW), name: 'Baysor (imported)', source: 'imported' }, NOW);
  assert.throws(() => decideSegmentation(p, { dataset: 'd1', segmentation: 'exp-b', decision: 'keep', rationale: ' ' }), /reason/);
  assert.throws(() => decideSegmentation(p, { dataset: 'd1', segmentation: 'exp-b', decision: 'adopt', rationale: 'x' }), /promote, keep, reject/);
  assert.throws(() => decideSegmentation(p, { dataset: 'd1', segmentation: 'seg-a', decision: 'reject', rationale: 'too tight' }), /the default: promote another/);
  p = decideSegmentation(p, { dataset: 'd1', segmentation: 'exp-b', decision: 'keep', rationale: 'A baseline', comparison: 'cmp-1', now: NOW });
  assert.equal(segmentationDecision(p, 'd1', 'exp-b').kind, 'keep-segmentation');
  assert.equal(segmentationDecision(p, 'd1', 'exp-b').comparison, 'cmp-1');
  p = decideSegmentation(p, { dataset: 'd1', segmentation: 'imp-c', decision: 'promote', rationale: 'Fewer misassigned transcripts', comparison: 'cmp-2', now: NOW });
  assert.equal(promotedSegmentation(p, 'd1').record.id, 'imp-c');
  assert.equal(promotedSegmentation(p, 'd1').decision.comparison, 'cmp-2');
  // The instrument's, no longer the default, can be rejected; the default stays.
  p = decideSegmentation(p, { dataset: 'd1', segmentation: 'seg-a', decision: 'reject', rationale: 'Cells cut by the stain', now: NOW });
  assert.equal(segmentationDecision(p, 'd1', 'seg-a').kind, 'reject-segmentation');
  assert.equal(promotedSegmentation(p, 'd1').record.id, 'imp-c');
  assert.equal(p.decisions.length, 4, 'every decision appended');
  assert.match(p.history.at(-1).detail, /Rejected instrument segmentation of xenium-lung-2fov: Cells cut by the stain/);
  // A rejected segmentation promoted again is the default again.
  p = decideSegmentation(p, { dataset: 'd1', segmentation: 'seg-a', decision: 'promote', rationale: 'On second thought', now: NOW });
  assert.equal(promotedSegmentation(p, 'd1').record.id, 'seg-a');
});

test('errors planted for practice are recorded as simulated, and never promoted', () => {
  let p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  const planted = {
    ...manifest('pla-c'), name: 'Planted errors (simulated)',
    lineage: {
      parents: [manifest('seg-a').lineage.parents[0], { kind: 'segmentation', id: 'seg-a', name: 'Instrument segmentation' }],
      job: { id: 'j3-1', kind: 'plant', spec: {} },
      method: { name: PLANTED_METHOD, version: '1' },
      parameters: { merges: 20, splits: 20, missed: 10, grown: 10, grow_um: 1, seed: 20261014 },
      software: [{ name: 'situcraft', version: '0.2.0' }],
    },
  };
  p = addSegmentation(p, segmentationRecord({ id: 'd1' }, planted, NOW), NOW);
  assert.equal(segmentationsOf(p, 'd1').find((a) => a.id === 'pla-c').source, 'planted');
  assert.throws(() => decideSegmentation(p, { dataset: 'd1', segmentation: 'pla-c', decision: 'promote', rationale: 'Testing', now: NOW }), /simulated/);
  const rejected = decideSegmentation(p, { dataset: 'd1', segmentation: 'pla-c', decision: 'reject', rationale: 'Its errors were found', now: NOW });
  assert.equal(segmentationDecision(rejected, 'd1', 'pla-c').kind, 'reject-segmentation');
});

test('an export is recorded with its formats, folder, files and fingerprints', () => {
  const p = withInstrumentSegmentation(project(), { id: 'd1', name: 'xenium-lung-2fov' }, manifest('seg-a'), NOW);
  assert.deepEqual(exportsOf(p, 'd1'), [], 'a project without exports has none');
  const record = segmentationsOf(p, 'd1')[0];
  const m = {
    created: NOW, software: 'situcraft 0.2.0', formats: { spatialdata: '…', parquet: '…' },
    files: [{ path: 'spatialdata.zarr/zarr.json', size: 120 }, { path: 'parquet/cells.parquet', size: 880 }],
    counts: { cells: 11898 }, content: { labelsSha256: 'ab' },
    findings: [{ format: 'xenium-ranger', severity: 'warning', code: 'ranger.assigned-otherwise', message: '9 transcripts…', count: 9 }],
    xeniumRanger: { command: 'xeniumranger import-segmentation --id=x' },
  };
  const next = addExport(p, exportRecord({ id: 'd1' }, record, m, '/data/out', NOW), NOW);
  const [x] = exportsOf(next, 'd1');
  assert.deepEqual(x.formats, ['spatialdata', 'parquet']);
  assert.equal(x.files, 2);
  assert.equal(x.size, 1000);
  assert.equal(x.content.labelsSha256, 'ab');
  assert.equal(x.xeniumRanger, 'xeniumranger import-segmentation --id=x');
  assert.match(x.id, /^out-[0-9a-z]{6,}/);
  assert.match(next.history.at(-1).detail, /Exported instrument segmentation of xenium-lung-2fov as spatialdata, parquet into \/data\/out \(2 files by SHA-256\)/);
  assert.deepEqual(exportsOf(next, 'd2'), []);
  for (const f of ['spatialdata', 'anndata', 'parquet', 'csv', 'mex', 'geojson', 'xenium-ranger']) assert.ok(EXPORT_FORMATS[f]?.label, f);
});
