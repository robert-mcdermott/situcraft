import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addGraph, cellsRequest, checkGraphParams, chooseCells, describeGraph, edgesIn, graphParams, graphRecord, graphsOf, gridOf, neighborsOf, readGraph } from './graphs.js';
import { keepChecksum } from './filters.js';
import { createProject } from './project.js';

// A graph answer, as GET /api/graphs/{id}/edges writes it.
function answer({ rows, x, y, edges, shared = null }) {
  const n = rows.length;
  const lists = rows.map(() => []);
  for (const [i, j, s] of edges) {
    lists[i].push([j, s]);
    lists[j].push([i, s]);
  }
  for (const l of lists) l.sort((a, b) => a[0] - b[0]);
  const m = 2 * edges.length;
  const buf = new ArrayBuffer(12 + 12 * n + 4 * (n + 1) + 8 * m + (shared ? 4 * m : 0));
  const v = new DataView(buf);
  v.setUint32(0, n, true);
  v.setUint32(4, m, true);
  v.setUint8(8, shared ? 1 : 0);
  let at = 12;
  const u32 = (val) => { v.setUint32(at, val, true); at += 4; };
  const f32 = (val) => { v.setFloat32(at, val, true); at += 4; };
  rows.forEach(u32);
  x.forEach(f32);
  y.forEach(f32);
  let off = 0;
  u32(0);
  for (const l of lists) u32((off += l.length));
  for (const l of lists) for (const [j] of l) u32(j);
  lists.forEach((l, i) => l.forEach(([j]) => f32(Math.hypot(x[j] - x[i], y[j] - y[i]))));
  if (shared) for (const l of lists) for (const [, s] of l) f32(s);
  return buf;
}

test('parameters are checked as the program checks them', () => {
  assert.equal(checkGraphParams({ kind: 'delaunay', longest: 0 }), null);
  assert.equal(checkGraphParams({ kind: 'knn', k: 6 }), null);
  assert.match(checkGraphParams({ kind: 'knn', k: 0 }), /whole number from 1 to 100/);
  assert.match(checkGraphParams({ kind: 'knn', k: 2.5 }), /whole number/);
  assert.match(checkGraphParams({ kind: 'radius', radius: 0 }), /more than 0 and at most 500/);
  assert.equal(checkGraphParams({ kind: 'contact', gap: 20 }), null);
  assert.match(checkGraphParams({ kind: 'contact', gap: -1 }), /from 0 to 20/);
  assert.match(checkGraphParams({ kind: 'voronoi' }), /No graph of kind voronoi/);
  assert.deepEqual(graphParams({ kind: 'knn', k: 6, radius: 30 }), { kind: 'knn', k: 6 });
  assert.equal(describeGraph({ kind: 'delaunay', longest: 50 }), 'Delaunay, edges to 50 µm');
  assert.equal(describeGraph({ kind: 'contact', gap: 0 }), 'Contact');
});

test('the cells chosen: a filter\'s, in regions, never those a review excluded', () => {
  const keep = Uint8Array.from([1, 1, 0, 1, 1, 1]);
  const excluded = Uint8Array.from([0, 0, 0, 1, 0, 1]);
  const c = chooseCells(6, { keep, regionRows: [Uint32Array.from([1, 2, 3]), Uint32Array.from([5])], excluded });
  assert.deepEqual([...c.keep], [0, 1, 0, 0, 0, 0]);
  assert.equal(c.count, 1);
  assert.equal(c.excluded, 2);
  const all = chooseCells(3);
  assert.deepEqual([...all.keep], [1, 1, 1]);
  const r = cellsRequest(c.keep, { regions: [{ id: 'reg-1', name: 'Nest', version: 1 }], excluded: 2 });
  assert.equal(r.checksum, keepChecksum(c.keep));
  const bytes = Uint8Array.from(atob(r.keep), (ch) => ch.charCodeAt(0));
  assert.deepEqual([...bytes], [6, 0, 0, 0, 0b10]);
  assert.equal(r.excluded, 2);
  assert.equal(r.filter, undefined);
});

test('a graph read, its cells\' neighbors, and the edges in a rectangle', () => {
  // Five cells on a line and one apart: rows 2, 4, 7, 8, 9, 12 of a segmentation.
  const rows = [2, 4, 7, 8, 9, 12];
  const x = [0, 10, 20, 30, 40, 200];
  const y = [0, 0, 0, 0, 0, 200];
  const g = readGraph(answer({ rows, x, y, edges: [[0, 1], [1, 2], [2, 3], [3, 4], [1, 3]] }));
  assert.equal(g.n, 6);
  assert.equal(g.shared, null);
  assert.deepEqual(neighborsOf(g, 4).map((e) => [e.row, e.length]), [[2, 10], [7, 10], [8, 20]]);
  assert.deepEqual(neighborsOf(g, 12), []);
  assert.equal(neighborsOf(g, 3), null);
  assert.equal(neighborsOf(g, 99), null);
  const grid = gridOf(g, 15);
  const all = edgesIn(g, grid, { x0: -1, y0: -1, x1: 300, y1: 300 });
  assert.equal(all.length, 10);
  // Cells 0 and 1 inside: their edges, including those reaching out.
  const part = edgesIn(g, grid, { x0: -1, y0: -1, x1: 12, y1: 1 });
  const pairs = [];
  for (let k = 0; k < part.length; k += 2) pairs.push([part[k], part[k + 1]].sort().join('-'));
  assert.deepEqual(pairs.sort(), ['0-1', '1-2', '1-3']);
  assert.equal(edgesIn(g, grid, { x0: -1, y0: -1, x1: 300, y1: 300 }, 3), null);
  assert.throws(() => readGraph(new ArrayBuffer(13)), /bytes, not/);
  const c = readGraph(answer({ rows: [0, 1], x: [0, 3], y: [0, 4], edges: [[0, 1, 2.5]], shared: true }));
  assert.deepEqual(neighborsOf(c, 0), [{ row: 1, length: 5, shared: 2.5 }]);
});

test('a graph recorded in the project once', () => {
  let p = createProject('t', { id: 'p-1', now: '2026-10-17T00:00:00Z' });
  p = { ...p, datasets: [{ id: 'd-1', name: 'Lung' }] };
  const manifest = { id: 'gra-1', name: 'Delaunay graph of X', state: 'complete', created: '2026-10-17T00:00:00Z', lineage: { parents: [{ kind: 'bundle', id: 'b' }, { kind: 'segmentation', id: 'seg-1' }], parameters: { kind: 'delaunay', longest: 50 } }, summary: { cells: 10, edges: 20 } };
  const record = graphRecord({ id: 'd-1' }, manifest, '2026-10-17T00:00:01Z');
  assert.equal(record.segmentation, 'seg-1');
  const next = addGraph(p, record, '2026-10-17T00:00:02Z');
  assert.equal(graphsOf(next, 'd-1', 'seg-1').length, 1);
  assert.match(next.history.at(-1).detail, /10 cells, 20 edges \(artifact gra-1\)/);
  assert.equal(addGraph(next, record), next);
});
