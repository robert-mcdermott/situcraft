import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { base64Of, defaultPair, impactLines, int32sOf, originOf, remakePlan, sharedColors, sideOf } from './impact.js';

const NOW = '2026-10-16T12:00:00.000Z';

// A dataset of two segmentations; a clustering of the first with two label sets from markers (the
// same set, two margins), one from a reference and one imported; the second with one from
// markers at the first margin.
function project() {
  let p = createProject('Lung', { now: NOW });
  const marker = { kind: 'marker-set', id: 'ms-2', family: 'ms-1', version: 2, name: 'Lung', set: { name: 'Lung', labels: [] }, checksum: 'c2' };
  const labels = (id, clustering, segmentation, extra) => ({ kind: 'labels', id, dataset: 'd1', clustering, segmentation, state: 'complete', ...extra });
  p = {
    ...p,
    datasets: [{ id: 'd1', name: 'lung' }],
    artifacts: [
      { kind: 'segmentation', id: 'seg-a', name: 'Instrument segmentation', dataset: 'd1', state: 'complete' },
      { kind: 'segmentation', id: 'seg-b', name: 'Expansion', dataset: 'd1', state: 'complete' },
      { kind: 'clustering', id: 'clu-a', name: 'Clusters of A', dataset: 'd1', segmentation: 'seg-a', state: 'complete', parameters: { resolution: 1, minTranscripts: 10 }, summary: { filter: { name: 'Enough signal v2' } } },
      marker,
      labels('lab-1', 'clu-a', 'seg-a', { markerSet: 'ms-2', markers: { name: 'Lung', checksum: 'c2' }, parameters: { margin: 0.1, markers: { name: 'Lung' } } }),
      labels('lab-2', 'clu-a', 'seg-a', { markerSet: 'ms-2', markers: { name: 'Lung', checksum: 'c2' }, parameters: { margin: 0.2 } }),
      labels('lab-3', 'clu-a', 'seg-a', { reference: { file: 'hlca.zarr', sha256: 'f'.repeat(64), labelColumn: 'cell_type' }, parameters: { tune: 0.05, margin: 0, markers: 0 } }),
      labels('lab-4', null, 'seg-a', { source: { file: 'labels.csv', column: 'type' } }),
      labels('lab-5', 'clu-b', 'seg-b', { markerSet: 'ms-2', markers: { name: 'Lung', checksum: 'c2' }, parameters: { margin: 0.1 } }),
    ],
  };
  return p;
}

test('the label sets compared by default are made the same way on both segmentations', () => {
  const p = project();
  const va = [{ id: 'lab-1', level: 0 }, { id: 'lab-1', level: 1 }, { id: 'lab-2', level: 0 }, { id: 'lab-3', level: 0 }, { id: 'lab-4', level: 0 }];
  // B's set from markers at margin 0.1 pairs with lab-1 (the markers key aside), each at its first
  // level; a set without a first level among the versions, at the level found.
  const vb = [{ id: 'lab-5', level: 0 }, { id: 'lab-5', level: 1 }];
  assert.deepEqual(defaultPair(p, va, vb).map((v) => `${v.id}:${v.level}`), ['lab-1:0', 'lab-5:0']);
  assert.deepEqual(defaultPair(p, va.slice(1), [vb[1]]).map((v) => `${v.id}:${v.level}`), ['lab-1:1', 'lab-5:1']);
  // Without one made the same way, the latest of each; a side without one, null.
  assert.deepEqual(defaultPair(p, va, [{ id: 'cur-1', level: 0 }]).map((v) => v.id), ['lab-4', 'cur-1']);
  assert.deepEqual(defaultPair(p, va, []), [va.at(-1), null]);
  assert.equal(originOf(p.artifacts.find((a) => a.id === 'lab-4')), null);
  assert.match(originOf(p.artifacts.find((a) => a.id === 'lab-3')), /^reference f{64} cell_type /);
});

test('a clustering and its labels made again on another segmentation: what is made, and what is not', () => {
  const p = project();
  const plan = remakePlan(p, 'clu-a', 'seg-b');
  assert.deepEqual(plan.parameters, { resolution: 1, minTranscripts: 10 });
  assert.match(plan.notes[0], /QC filter Enough signal v2 kept of the instrument's segmentation; on expansion they are those with at least 10 gene transcripts/);
  assert.deepEqual(plan.labels.map((l) => [l.kind, l.record.id]), [['markers', 'lab-1'], ['markers', 'lab-2'], ['reference', 'lab-3']]);
  assert.deepEqual(plan.labels[0].parameters, { margin: 0.1 }, 'the record\'s marker set key is not a parameter');
  assert.equal(plan.labels[0].markerSet, 'ms-2');
  assert.deepEqual(plan.skipped, []);
  assert.equal(remakePlan(p, 'clu-a', 'seg-a').problem, 'The clustering is already of instrument segmentation.');
  assert.equal(remakePlan(p, 'nope', 'seg-b').problem, 'No such clustering in the project.');
});

test('a label set as one side of the request; per-cell flags read back', () => {
  const src = { segCells: 4, label: Int32Array.from([1, -1, 0, 2]), analyzed: Uint8Array.from([1, 1, 0, 1]), names: ['T', 'B', 'Mac'] };
  const side = sideOf(src);
  assert.deepEqual([...int32sOf(side.labels)], [1, -1, 0, 2]);
  assert.equal(atob(side.covered), '\x01\x01\x00\x01');
  assert.deepEqual([...int32sOf(base64Of(Int32Array.from([-1, 3])))], [-1, 3]);
  const colors = sharedColors({ names: ['T', 'B'], colors: ['#111111', '#222222'] }, { names: ['B', 'Fib'], colors: ['#333333', '#444444'] });
  assert.deepEqual([...colors], [['T', '#111111'], ['B', '#222222'], ['Fib', '#444444']]);
});

test('the impact in words', () => {
  const im = {
    matched: { cells: 1000, labeled: 900, same: 846, agreement: 0.94, labeledOne: 20 },
    split: { groups: 10, kept: 6, partly: 3, changed: 1, mixed: 2 },
    merged: { groups: 0 },
    mixing: { allA: { share: 0.12, mecr: 0.0455 }, allB: { share: 0.08, mecr: 0.0501 }, a: [{ name: 'T', mecr: 0.02 }, { name: 'Tumor', mecr: 0.05 }], b: [{ name: 'T', mecr: 0.06 }, { name: 'Tumor', mecr: 0.051 }] },
  };
  assert.deepEqual(impactLines(im, 'the instrument', 'the expansion'), [
    'Of the 1,000 cells matched one to one, 900 are labeled under both: 94.0% alike; 20 are labeled under one only.',
    '10 labeled cells of the instrument are split under the expansion: every piece keeps the cell\'s label in 60%, some pieces in 30%, none in 10%; 20% split into cells of two labels or more.',
    'Marker mixing (the mutually exclusive co-expression rate) of the labeled cells: 0.0455 under the instrument, 0.0501 under the expansion; it changes most among T cells (0.0200 → 0.0600).',
  ]);
});
