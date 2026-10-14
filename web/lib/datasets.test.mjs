import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addDataset, compareFingerprint, datasetRecord, findDataset, relocateDataset, removeDataset } from './datasets.js';

const NOW = '2026-10-14T12:00:00.000Z';

function report(overrides = {}) {
  return {
    bundle: 'Xenium_V1_human_Lung_2fov_outs',
    version: '2.0.0',
    support: 'supported',
    checked: NOW,
    experimentUuid: 'b3d47512',
    analysisUuid: '38f7f730',
    fingerprint: [
      { path: 'experiment.xenium', size: 1673, modTime: 1, sha256: 'aa' },
      { path: 'transcripts.parquet', size: 15254903, modTime: 1 },
    ],
    findings: [{ severity: 'info', code: 'transcripts.at-edge', title: 'Transcripts just past the images\' edge', count: 7 }],
    summary: { cells: 11898, transcripts: 825885, panel: 'Xenium Human Lung Gene Expression', organism: 'Human', tissue: 'Lung', pixelSizeUm: 0.2125, extentUm: [1230.587, 755.012], channels: ['DAPI (Nuclear)'], zPlanes: 11, metricsCompared: 12, metricsAgreeing: 12 },
    ...overrides,
  };
}

test('a validated bundle becomes a dataset record with its identity, summary and validation', () => {
  const record = datasetRecord({ path: '/data/lung' }, report(), { id: 'd-1', now: NOW });
  assert.equal(record.path, '/data/lung');
  assert.equal(record.cells, 11898);
  assert.equal(record.validation.info, 1);
  assert.equal(record.validation.fatal, 0);
  assert.equal(record.validation.metricsAgreeing, 12);
  assert.deepEqual(record.fingerprint[0], { path: 'experiment.xenium', size: 1673, sha256: 'aa' });
  assert.deepEqual(record.fingerprint[1], { path: 'transcripts.parquet', size: 15254903 });
});

test('adding a dataset names an untitled project, records it, and refuses duplicates and fatal bundles', () => {
  const project = createProject('Untitled project', { now: NOW });
  const record = datasetRecord({ path: '/data/lung' }, report(), { id: 'd-1', now: NOW });
  const next = addDataset(project, record, NOW);
  assert.equal(next.name, 'Xenium_V1_human_Lung_2fov_outs');
  assert.equal(next.datasets.length, 1);
  assert.equal(next.history.at(-1).action, 'add-dataset');
  assert.equal(project.datasets.length, 0);
  assert.throws(() => addDataset(next, { ...record, id: 'd-2' }, NOW), /already in this project/);
  // The same run elsewhere (a copy) is the same dataset.
  assert.throws(() => addDataset(next, { ...record, id: 'd-3', path: '/copy/lung' }, NOW), /already in this project/);
  const fatal = datasetRecord({ path: '/data/broken' }, report({ experimentUuid: 'x', findings: [{ severity: 'fatal', code: 'files.missing', title: 'Cells missing' }] }), { now: NOW });
  assert.throws(() => addDataset(next, fatal, NOW), /fatal problem/);
  // A named project keeps its name.
  const named = addDataset(createProject('PTLD cohort', { now: NOW }), record, NOW);
  assert.equal(named.name, 'PTLD cohort');
  assert.ok(findDataset(named, { path: '/data/lung' }));
});

test('removing and relocating', () => {
  const record = datasetRecord({ path: '/data/lung' }, report(), { id: 'd-1', now: NOW });
  const project = addDataset(createProject('P', { now: NOW }), record, NOW);
  const moved = relocateDataset(project, 'd-1', '/archive/lung', NOW);
  assert.equal(moved.datasets[0].path, '/archive/lung');
  assert.match(moved.history.at(-1).detail, /\/data\/lung → \/archive\/lung/);
  assert.equal(relocateDataset(moved, 'd-1', '/archive/lung', NOW), moved);
  const removed = removeDataset(moved, 'd-1', NOW);
  assert.equal(removed.datasets.length, 0);
  assert.equal(removeDataset(removed, 'd-1', NOW), removed);
});

test('a fingerprint tells the same bundle (even copied) from a changed one or another run', () => {
  const recorded = [{ path: 'experiment.xenium', size: 1673, sha256: 'aa' }, { path: 'transcripts.parquet', size: 100 }];
  const copied = [{ path: 'experiment.xenium', size: 1673, modTime: 999, sha256: 'aa' }, { path: 'transcripts.parquet', size: 100, modTime: 999 }];
  assert.equal(compareFingerprint(recorded, copied).status, 'same');
  const grown = [copied[0], { path: 'transcripts.parquet', size: 101 }];
  assert.deepEqual(compareFingerprint(recorded, grown), { status: 'changed', files: ['transcripts.parquet'] });
  const extra = [...copied, { path: 'notes.txt', size: 5 }];
  assert.deepEqual(compareFingerprint(recorded, extra), { status: 'changed', files: ['notes.txt'] });
  const other = [{ path: 'experiment.xenium', size: 1673, sha256: 'bb' }, copied[1]];
  assert.equal(compareFingerprint(recorded, other).status, 'different-run');
});
