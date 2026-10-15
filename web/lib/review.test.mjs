import assert from 'node:assert/strict';
import test from 'node:test';
import { CLASS_INFO, DIFFERING, cellNames, prepare, rank, warnings } from './review.js';

// Five differences: a one-to-one pair, an expansion, a split, a cell only in A, a cell only in B.
const cols = {
  classes: Object.keys(CLASS_INFO),
  class: [0, 1, 3, 6, 7],
  a: [[0], [1], [2], [3], []],
  b: [[0], [1], [2, 3], [], [4]],
  iou: [0.95, 0.6, 0, 0, 0],
  jaccard: [0.9, 0.5, 0, 0, 0],
  displacement: [0.1, 1.2, 0, 0, 0],
  changed: [2, 9, 30, 12, 4],
  x: [10, 20, 30, 40, 50],
  y: [5, 5, 5, 5, 5],
  transcriptsA: [80, 40, 120, 6, 0],
  transcriptsB: [82, 55, 118, 0, 3],
  mixedA: [0, 0, 1, 0, 0],
  mixedB: [0, 2, 1, 0, 0],
  idsA: ['a0', 'a1', 'a2', 'a3'],
  idsB: ['b0', 'b1', 'b2', 'b3', 'b4'],
};

test('differences are prepared with each cell\'s difference', () => {
  const p = prepare(cols);
  assert.equal(p.groups.length, 5);
  assert.equal(p.groups[2].class, 'split');
  assert.deepEqual([...p.ofA], [0, 1, 2, 3]);
  assert.deepEqual([...p.ofB], [0, 1, 2, 2, 4]);
  assert.deepEqual(cellNames(p, p.groups[2]), { a: 'a2', b: 'b2, b3' });
  assert.deepEqual(cellNames(p, p.groups[4]), { a: '—', b: 'b4' });
});

test('the review queue ranks by the reviewer\'s order and keeps what the rules keep', () => {
  const { groups } = prepare(cols);
  const order = (by, rules = {}) => rank([...groups], { by, ...rules }).map((g) => g.index);
  // One-to-one cells are left out at first.
  assert.ok(!DIFFERING.includes('one-to-one'));
  assert.deepEqual(order('reassigned'), [2, 3, 1, 4]);
  assert.deepEqual(order('contamination'), [1, 2, 3, 4]);
  // Splits and cells in one only (weight 1) before the expansion, in the comparison's order.
  assert.deepEqual(order('disagreement'), [2, 3, 4, 1]);
  assert.deepEqual(order('low-counts'), [4, 3, 1, 2]);
  assert.deepEqual(order('reassigned', { minChanged: 10 }), [2, 3]);
  assert.deepEqual(order('reassigned', { classes: new Set(['one-to-one', 'split']) }), [2, 0]);
});

test('warnings name the measures that rest on little', () => {
  const w = warnings({ coexpression: { pairs: 12, nuclearCells: 600 }, metricsA: { coexpressionCells: 900, emptyCells: 0 }, metricsB: { coexpressionCells: 300, emptyCells: 4 }, mecrPairs: [['EPCAM', 'PTPRC']], iou: { pairs: 30 } });
  assert.equal(w.length, 6);
  assert.match(w.join(' '), /600 nuclei/);
  assert.match(w.join(' '), /12 gene pairs/);
  assert.match(w.join(' '), /B's spurious co-expression is measured on 300 cells/);
  assert.match(w.join(' '), /4 cells of B have no outline/);
  assert.match(w.join(' '), /1 pair of lineage markers/);
  assert.equal(warnings({ coexpression: { pairs: 2344, nuclearCells: 1376 }, metricsA: { coexpressionCells: 7025 }, metricsB: { coexpressionCells: 7051 }, mecrPairs: new Array(251), iou: { pairs: 11898 } }).length, 0);
});
