import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { appendEdit, applyCurated, checkRule, consensusOf, createCurated, crossTable, curatedSetsOf, nearestCells, saveConsensus, consensusSetsOf, tally } from './curation.js';
import { categoricalColor } from './colormaps.js';

const NOW = '2026-10-16T12:00:00.000Z';

// Three sets over eight cells (−1: no label); their names differ in case and wording.
const A = { names: ['T cell', 'Tumour', 'Fibroblast'], label: Int32Array.from([0, 0, 1, 1, 2, -1, 0, 1]) };
const B = { names: ['Tumour', 'T cell', 'Macrophage'], label: Int32Array.from([1, 1, 0, 2, -1, 0, 0, 0]) };
const C = { names: ['T cells', 'Tumor'], label: Int32Array.from([0, 1, 1, 1, -1, 1, -1, 1]) };
const mapping = [{}, {}, { 'T cells': 'T cell', Tumor: 'Tumour' }];

test('consensus rules on built sets: unanimous, majority, in order, with names mapped', () => {
  const names = (r) => Array.from(r.label, (k) => (k < 0 ? null : r.names[k]));
  // Cell by cell, the three sets (mapped): 0 T/T/T; 1 T/T/Tu; 2 Tu/Tu/Tu; 3 Tu/M/Tu; 4 F/–/–;
  // 5 –/Tu/Tu; 6 T/Tu/–; 7 Tu/Tu/Tu.
  const unanimous = consensusOf({ kind: 'unanimous' }, [A, B, C], mapping);
  assert.deepEqual(names(unanimous), ['T cell', null, 'Tumour', null, null, null, null, 'Tumour']);
  const majority = consensusOf({ kind: 'majority', min: 2 }, [A, B, C], mapping);
  assert.deepEqual(names(majority), ['T cell', 'T cell', 'Tumour', 'Tumour', null, 'Tumour', null, 'Tumour']);
  assert.deepEqual([...majority.votes], [3, 2, 3, 2, 0, 2, 0, 3]);
  assert.deepEqual([...majority.voters], [3, 3, 3, 3, 1, 2, 2, 3]);
  const priority = consensusOf({ kind: 'priority' }, [A, B, C], mapping);
  assert.deepEqual(names(priority), ['T cell', 'T cell', 'Tumour', 'Tumour', 'Fibroblast', 'Tumour', 'T cell', 'Tumour']);
  // Without the mapping, C's names stand apart: cell 1 is T cell, T cell, T cells — still two.
  assert.deepEqual(names(consensusOf({ kind: 'majority', min: 2 }, [A, B, C]))[1], 'T cell');
  assert.deepEqual(names(consensusOf({ kind: 'majority', min: 3 }, [A, B, C])), [null, null, null, null, null, null, null, null], 'C\'s unmapped names never join the others');
  // Equal votes give no label: cell 6 under a majority of 1.
  assert.equal(names(consensusOf({ kind: 'majority', min: 1 }, [A, B, C], mapping))[6], null);
  assert.equal(checkRule({ kind: 'majority', min: 4 }, 3), 'The majority is a number of sets from 1 to 3.');
  assert.equal(checkRule({ kind: 'unanimous' }, 1), 'A consensus is of two sets or more.');
});

test('a consensus is saved with its members, mapping and rule', () => {
  let p = createProject('Lung', { now: NOW });
  p = { ...p, datasets: [{ id: 'd1', name: 'lung' }] };
  p = saveConsensus(p, { dataset: 'd1', segmentation: 'seg-a', name: 'Agreed', members: [{ ref: { kind: 'labels', id: 'lab-1', level: 0 }, name: 'Markers' }, { ref: { kind: 'labels', id: 'lab-2', level: 0 }, name: 'Imported' }], mapping: [{}, { 'T cells': 'T cell' }], rule: { kind: 'majority', min: 2 }, now: NOW, id: 'con-1' });
  assert.deepEqual(consensusSetsOf(p, 'd1', 'seg-a').map((c) => c.id), ['con-1']);
  assert.match(p.history.at(-1).detail, /Made the consensus label set "Agreed" of Markers, Imported by a majority of 2/);
  assert.throws(() => saveConsensus(p, { dataset: 'd1', segmentation: 'seg-a', name: 'x', members: [{ ref: {}, name: 'one' }], rule: { kind: 'priority' } }), /two sets or more/);
});

test('a curated set: edits appended with their reasons, applied in order', () => {
  let p = createProject('Lung', { now: NOW });
  p = createCurated(p, { dataset: 'd1', segmentation: 'seg-a', base: { labels: 'lab-1', level: 0, name: 'Labels by Lung' }, name: 'Reviewed', now: NOW, id: 'cur-1' });
  assert.deepEqual(curatedSetsOf(p, 'd1').map((c) => c.id), ['cur-1']);
  p = appendEdit(p, 'cur-1', { kind: 'cells', cells: [2, 2, 5], label: 'Macrophage', reason: 'CD68 and C1QA, no EPCAM', by: 'R.' }, NOW);
  assert.match(p.history.at(-1).detail, /^Reviewed: 2 cells relabeled "Macrophage" \(CD68 and C1QA, no EPCAM; R\.\)$/);
  p = appendEdit(p, 'cur-1', { kind: 'cluster', clustering: 'clu-1', clusters: [1], label: 'T cell', reason: 'CD3E leads' }, NOW);
  p = appendEdit(p, 'cur-1', { kind: 'accept', cells: [0], label: 'T cell', reason: 'looked right' }, NOW);
  assert.throws(() => appendEdit(p, 'cur-1', { kind: 'cells', cells: [1], label: 'X', reason: ' ' }), /Say why/);
  const record = p.artifacts.find((a) => a.id === 'cur-1');
  assert.deepEqual(record.edits[0].cells, [2, 5]);
  const cluster = Int32Array.from([0, 0, 1, 1, 0, 1, 0, 0]);
  const out = applyCurated(record, A, (id) => (id === 'clu-1' ? cluster : null));
  // Cell 2 relabeled Macrophage, then cluster 1 (cells 2, 3, 5) T cell; cell 0 accepted.
  const names = Array.from(out.label, (k) => (k < 0 ? null : out.names[k]));
  assert.deepEqual(names, ['T cell', 'T cell', 'T cell', 'T cell', 'Fibroblast', 'T cell', 'T cell', 'Tumour']);
  assert.deepEqual(out.names, ['T cell', 'Tumour', 'Fibroblast', 'Macrophage']);
  assert.equal(out.colors[3], categoricalColor(3));
  assert.deepEqual([...out.edited], [2, -1, 1, 1, -1, 1, -1, -1]);
  assert.equal(out.reviewed, 4);
  assert.deepEqual([...A.label], [0, 0, 1, 1, 2, -1, 0, 1], 'the base is left as it was');
});

test('two label sets side by side, and the labels of a cell\'s neighbors', () => {
  const ct = crossTable(A, B);
  // Cells both label: 0, 1, 2, 3, 6, 7; the same name in 0, 1 (T cell), 2 and 7 (Tumour).
  assert.equal(ct.cells, 6);
  assert.ok(Math.abs(ct.agreement - 4 / 6) < 1e-15);
  assert.deepEqual(ct.table, [[1, 2, 0], [2, 0, 1], [0, 0, 0]]);
  assert.deepEqual(tally(A.label, [0, 1, 2, 4, 5]), [{ label: 0, count: 2 }, { label: 1, count: 1 }, { label: 2, count: 1 }]);
  const x = Float32Array.from([0, 1, 2, 3, 4, 5, 6, 7]);
  const y = new Float32Array(8);
  assert.deepEqual(nearestCells(x, y, A.label, 4, 3), [3, 6, 2].sort((p, q) => Math.abs(p - 4) - Math.abs(q - 4) || p - q), 'cell 5 has no label');
});
