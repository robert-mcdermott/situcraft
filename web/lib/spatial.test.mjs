import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addAnalysis, analysesOf, analysisNumbers, analysisRecord, compareNumbers, linkRemake, rankCorrelation, remakeAnalysisPlan, remakesOf, bandCells, bandsOf, BOUNDARY_DEFAULTS, checkAnalysisParams, codesRequest, distancesTo, divergingColor, labelsRequest, nearCells, OUTSIDE_TISSUE, pairCells, localCells, MORAN_DEFAULTS, NICHES_DEFAULTS, nicheCells, nicheNames, readLocal, rankPairs, readAnalyzed, regionCells, REGIONS_DEFAULTS, rowNames, strataRequest, zoneBands } from './spatial.js';
import { sha256 } from './sha256.js';
import { createProject } from './project.js';

const decode = (b64) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));

test('each cell\'s number as the program reads it: count, then 32-bit numbers, and their checksum', () => {
  const r = codesRequest(Int32Array.from([2, -1, 0]));
  const bytes = decode(r.codes);
  assert.deepEqual([...bytes], [3, 0, 0, 0, 2, 0, 0, 0, 255, 255, 255, 255, 0, 0, 0, 0]);
  assert.equal(r.checksum, sha256(bytes));
  const labels = labelsRequest({ id: 'lab-1', kind: 'labels', name: 'Lineage', level: 1, names: ['T', 'B'], colors: [[1, 2, 3], '#fff'], label: Int32Array.from([0, 1]) });
  assert.deepEqual(labels.colors, ['rgb(1, 2, 3)', '#fff']);
  assert.equal(labels.level, 1);
  // Only the labels some cell has, numbered again.
  const some = labelsRequest({ id: 'lab-1', kind: 'labels', name: 'Class', level: 0, names: ['Immune', 'T', 'Epithelial', 'B'], colors: ['#a', '#b', '#c', '#d'], label: Int32Array.from([2, -1, 0, 2]) });
  assert.deepEqual([some.names, some.colors], [['Immune', 'Epithelial'], ['#a', '#c']]);
  const v = new DataView(decode(some.codes).buffer);
  assert.deepEqual([0, 1, 2, 3].map((i) => v.getInt32(4 + 4 * i, true)), [1, -1, 0, 1]);
  // A cell in two regions is in the first.
  const st = strataRequest(4, [{ id: 'r1', name: 'Nest', version: 2, rows: [1, 2] }, { id: 'r2', name: 'Edge', version: 1, rows: [2, 3] }]);
  const sv = new DataView(decode(st.codes).buffer);
  assert.deepEqual([0, 1, 2, 3].map((i) => sv.getInt32(4 + 4 * i, true)), [0, 1, 1, 2]);
  assert.deepEqual(st.regions, [{ id: 'r1', name: 'Nest', version: 2 }, { id: 'r2', name: 'Edge', version: 1 }]);
});

test('parameters checked as the program checks them; distances every step', () => {
  assert.equal(checkAnalysisParams('enrichment', { permutations: 1000, seed: 1, weight: 'edges' }), null);
  assert.match(checkAnalysisParams('enrichment', { permutations: 10, seed: 1, weight: 'edges' }), /100 to 100,000/);
  assert.match(checkAnalysisParams('enrichment', { permutations: 1000, seed: 1, weight: 'area' }), /boundary/);
  assert.equal(checkAnalysisParams('cooccurrence', { distances: [10, 20], pairs: 'between' }), null);
  assert.match(checkAnalysisParams('cooccurrence', { distances: [20, 10], pairs: 'within' }), /increase/);
  assert.match(checkAnalysisParams('hotspots', {}), /No question/);
  assert.equal(checkAnalysisParams('distances', { permutations: 199, reach: 100, step: 2.5 }), null);
  assert.match(checkAnalysisParams('distances', { permutations: 199, reach: 100, step: 0.1 }), /reach over 200/);
  assert.equal(checkAnalysisParams('ripley', { radii: [5, 10], correction: 'isotropic', permutations: 199 }), null);
  assert.match(checkAnalysisParams('ripley', { radii: [5, 10], correction: 'translation', permutations: 199 }), /isotropic or none/);
  assert.deepEqual(distancesTo(50, 10), [10, 20, 30, 40, 50]);
  assert.deepEqual(distancesTo(0.3, 0.1), [0.1, 0.2, 0.3]);
});

test('an enrichment\'s pairs ranked, most enriched first, undefined z last', () => {
  const analysis = {
    labels: { names: ['T', 'B'] },
    enrichment: { count: [4, 10, 10, 0], expected: [5, 5, 5, 2], z: [-1, 3, 3, null], p: [0.3, 0.01, 0.01, 1], padj: [0.3, 0.03, 0.03, 1] },
  };
  const pairs = rankPairs(analysis);
  assert.deepEqual(pairs.map((p) => [p.a, p.b]), [[0, 1], [0, 0], [1, 1]]);
  assert.equal(pairs[0].ratio, 2);
  assert.ok(Number.isNaN(pairs[2].z));
});

test('a pair\'s cells and the edges between them; a label\'s cells near another\'s', () => {
  // A path 0–1–2–3, labels T, B, B, T: T–B edges 0–1 and 2–3; B–B edge 1–2.
  const graph = { n: 4, offsets: Uint32Array.from([0, 1, 3, 5, 6]), neighbors: Uint32Array.from([1, 0, 2, 1, 3, 2]) };
  const label = Int32Array.from([0, 1, 1, 0]);
  const tb = pairCells(graph, label, 0, 1);
  assert.deepEqual([...tb.cells], [1, 1, 1, 1]);
  assert.deepEqual([tb.a, tb.b, tb.edges], [2, 2, 2]);
  const bb = pairCells(graph, label, 1, 1);
  assert.deepEqual([...bb.cells], [0, 1, 1, 0]);
  assert.deepEqual([bb.a, bb.b, bb.edges], [2, 0, 1]);
  // On a line: T at 0 and 12, B at 5 and 30.
  const x = [0, 5, 12, 30];
  const y = [0, 0, 0, 0];
  const lab = [0, 1, 0, 1];
  assert.deepEqual([...nearCells(x, y, lab, 0, 1, 10).cells], [1, 1, 1, 0]);
  assert.deepEqual([...nearCells(x, y, lab, 0, 1, 20, 10).cells], [0, 0, 1, 1]);
  assert.deepEqual(nearCells(x, y, lab, 0, 0, 15).a, 2);
});

test('an analysis recorded once, its cells read, a diverging color', () => {
  let p = createProject('t', { id: 'p-1', now: '2026-10-17T00:00:00Z' });
  p = { ...p, datasets: [{ id: 'd-1', name: 'Lung' }] };
  const manifest = { id: 'spa-1', name: 'Neighborhood enrichment of Lineage', state: 'complete', created: '2026-10-17T00:00:00Z', lineage: { parents: [{ kind: 'segmentation', id: 'seg-1' }, { kind: 'graph', id: 'gra-1' }] }, summary: { question: 'enrichment' } };
  const r = analysisRecord({ id: 'd-1' }, manifest);
  assert.deepEqual([r.segmentation, r.graph, r.question], ['seg-1', 'gra-1', 'enrichment']);
  const next = addAnalysis(p, r);
  assert.equal(analysesOf(next, 'd-1', 'seg-1').length, 1);
  assert.equal(addAnalysis(next, r), next);
  const buf = new ArrayBuffer(4 + 12 * 2);
  const v = new DataView(buf);
  v.setUint32(0, 2, true);
  v.setUint32(4, 3, true);
  v.setUint32(8, 7, true);
  v.setInt32(12, -1, true);
  v.setInt32(16, 1, true);
  const c = readAnalyzed(buf);
  assert.deepEqual([[...c.rows], [...c.label], [...c.stratum]], [[3, 7], [-1, 1], [0, 0]]);
  assert.equal(divergingColor(0), 'rgb(247, 247, 247)');
  assert.equal(divergingColor(5), 'rgb(103, 0, 31)');
  assert.equal(divergingColor(-5), 'rgb(5, 48, 97)');
  assert.equal(divergingColor(-Infinity), 'rgb(5, 48, 97)');
});

test('regions compared and a profile across an edge: parameters, rows, bands, zones and their cells', () => {
  assert.equal(checkAnalysisParams('regions', REGIONS_DEFAULTS), null);
  assert.match(checkAnalysisParams('regions', { level: 1 }), /0.5 to 0.999/);
  assert.equal(checkAnalysisParams('boundary', BOUNDARY_DEFAULTS), null);
  assert.match(checkAnalysisParams('boundary', { ...BOUNDARY_DEFAULTS, step: 7 }), /divides the reach/);
  assert.match(checkAnalysisParams('boundary', { ...BOUNDARY_DEFAULTS, margin: 100 }), /less than the reach/);
  assert.deepEqual(rowNames({ regions: [{ name: 'Tumor' }] }), ['Tumor', 'The rest of the tissue']);
  assert.deepEqual(rowNames({ regions: [{ name: 'A' }, { name: 'B' }] }), ['A', 'B', 'The tissue in none of them']);
  // Cells in A (bit 0), in A and B, in neither, outside the tissue; labels 0 and 1.
  const stratum = Int32Array.from([1, 3, 0, OUTSIDE_TISSUE, 2, 0]);
  const label = Int32Array.from([0, 0, 0, 0, 1, -1]);
  assert.deepEqual([...regionCells(stratum, label, 3, 0, 0).cells], [1, 1, 0, 0, 0, 0]);
  assert.equal(regionCells(stratum, label, 3, 1, 1).count, 1);
  assert.deepEqual([...regionCells(stratum, label, 3, 2, 0).cells], [0, 0, 1, 0, 0, 0]);
  const edges = [-30, -20, -10, 0, 10, 20, 30];
  assert.deepEqual(bandsOf([-10, 0, 10]).map((b) => [b.from, b.to]), [[-Infinity, -10], [-10, 0], [0, 10], [10, Infinity]]);
  assert.deepEqual(zoneBands(edges, 20, 'core'), { from: 0, to: 1 });
  assert.deepEqual(zoneBands(edges, 20, 'margin'), { from: 2, to: 5 });
  assert.deepEqual(zoneBands(edges, 20, 'around'), { from: 6, to: 6 });
  assert.equal(zoneBands(edges, 15, 'core'), null);
  // Cells by band + 1: outside the tissue, the deepest band, the margin's.
  const bands = Int32Array.from([0, 1, 3, 4, 6, 8]);
  const lab = Int32Array.from([0, 0, 0, 1, 0, -1]);
  assert.deepEqual([...bandCells(bands, lab, 2, 5, 0).cells], [0, 0, 1, 0, 1, 0]);
  assert.equal(bandCells(bands, lab, 0, 7, -1).count, 4);
});

test('niches: parameters, names and cells', () => {
  assert.equal(checkAnalysisParams('niches', NICHES_DEFAULTS), null);
  assert.equal(checkAnalysisParams('niches', { ...NICHES_DEFAULTS, neighbors: 0, radius: 30 }), null);
  assert.match(checkAnalysisParams('niches', { ...NICHES_DEFAULTS, radius: 30 }), /one of them/);
  assert.match(checkAnalysisParams('niches', { ...NICHES_DEFAULTS, niches: 1 }), /2 to 30/);
  assert.deepEqual(nicheNames({ defining: [[2, 0], [1]], composition: [0.3, 0.1, 0.6, 0.1, 0.85, 0.05] }, ['Stromal', 'Immune', 'Tumor']), ['1 · Tumor 60% + Stromal 30%', '2 · Immune 85%']);
  const nc = nicheCells(Int32Array.from([0, 1, 2, 1]), 0);
  assert.deepEqual([[...nc.cells], nc.count], [[0, 1, 0, 1], 2]);
});

test('Moran\'s I: parameters, and its local values read', () => {
  assert.equal(checkAnalysisParams('moran', MORAN_DEFAULTS), null);
  assert.equal(checkAnalysisParams('moran', { ...MORAN_DEFAULTS, permutations: 0, local: 0 }), null);
  assert.match(checkAnalysisParams('moran', { ...MORAN_DEFAULTS, weights: 'distance' }), /row/);
  assert.match(checkAnalysisParams('moran', { ...MORAN_DEFAULTS, permutations: 5 }), /0 \(the analytic test\)/);
  // Two cells, one gene (index 7): I 0.5 and −1, p 0.01 and 0.5, classes high among high and none.
  const buf = new ArrayBuffer(8 + 4 + 9 * 2);
  const v = new DataView(buf);
  v.setUint32(0, 2, true);
  v.setUint32(4, 1, true);
  v.setUint32(8, 7, true);
  v.setFloat32(12, 0.5, true);
  v.setFloat32(16, -1, true);
  v.setFloat32(20, 0.01, true);
  v.setFloat32(24, 0.5, true);
  v.setUint8(28, 1);
  v.setUint8(29, 0);
  const local = readLocal(buf);
  assert.deepEqual([local.n, local.genes, [...local.I[0]], [...local.classes[0]]], [2, [7], [0.5, -1], [1, 0]]);
  assert.equal(localCells(local, 0, 1).count, 1);
  assert.equal(localCells(local, 0, 2).count, 0);
});

test('an analysis made again on another segmentation: its plan, its numbers read with the other\'s', () => {
  const project = {
    artifacts: [
      { kind: 'segmentation', id: 's1', dataset: 'd', name: 'Instrument', state: 'complete' },
      { kind: 'segmentation', id: 's2', dataset: 'd', name: 'Refinement', state: 'complete' },
      { kind: 'segmentation', id: 's3', dataset: 'd', name: 'Running', state: 'running' },
      { kind: 'labels', id: 'lab-1', dataset: 'd', segmentation: 's1', clustering: 'clu-1', markerSet: 'mk-1', state: 'complete' },
      { kind: 'labels', id: 'lab-2', dataset: 'd', segmentation: 's1', source: { file: 'cells.csv' }, state: 'complete' },
      { kind: 'spatial', id: 'spa-1', dataset: 'd', segmentation: 's1', state: 'complete' },
      { kind: 'spatial', id: 'spa-2', dataset: 'd', segmentation: 's2', state: 'complete' },
    ],
  };
  const record = project.artifacts.find((a) => a.id === 'spa-1');
  const analysis = { question: 'enrichment', labels: { id: 'lab-1', kind: 'labels', level: 1, names: ['A', 'B'] }, graph: { parameters: { kind: 'delaunay', longest: 50 } }, cells: { regions: [{ id: 'r-1' }], filter: { name: 'QC' } } };
  const plan = remakeAnalysisPlan(project, record, analysis, 's2');
  assert.deepEqual([plan.labels.clustering, plan.labels.level, plan.graph.parameters.kind, plan.regions], ['clu-1', 1, 'delaunay', ['r-1']]);
  assert.match(plan.notes[0], /QC filter QC/);
  assert.match(remakeAnalysisPlan(project, record, analysis, 's1').problem, /already of/);
  assert.match(remakeAnalysisPlan(project, record, analysis, 's3').problem, /not complete/);
  assert.match(remakeAnalysisPlan(project, record, { ...analysis, labels: { id: 'lab-2', kind: 'labels' } }, 's2').problem, /imported/);
  assert.match(remakeAnalysisPlan(project, record, { ...analysis, labels: { id: 'cur-1', kind: 'curated' } }, 's2').problem, /curated/);
  assert.equal(remakeAnalysisPlan(project, record, { question: 'moran', graph: { parameters: { kind: 'knn' } }, cells: {} }, 's2').labels, null);
  // Numbers by name: an enrichment's pairs (A ≤ B) and a Moran's I's genes.
  const e1 = { labels: { names: ['A', 'B'] }, enrichment: { count: [4, 2, 2, 8], expected: [2, 4, 4, 4], z: [1, -1, -1, 2], p: [0.1, 0.1, 0.1, 0.1], padj: [0.1, 0.1, 0.1, 0.1] } };
  const e2 = { labels: { names: ['B', 'A'] }, enrichment: { count: [8, 2, 2, 2], expected: [4, 4, 4, 2], z: [2, -1, -1, 0], p: [0.1, 0.1, 0.1, 0.1], padj: [0.1, 0.1, 0.1, 0.1] } };
  const n1 = analysisNumbers(e1);
  assert.deepEqual(n1.map((x) => [x.key, x.value]).sort(), [['pair:A|A', 2], ['pair:A|B', 0.5], ['pair:B|B', 2]]);
  // The second's labels in another order: A – A is 2/2 = 1 there, B – B 8/4 = 2, and B – A its A – B.
  const cmp = compareNumbers(n1, analysisNumbers(e2));
  const aa = cmp.rows.find((r) => r.key === 'pair:A|A');
  assert.deepEqual([aa.a, aa.b, aa.change], [2, 1, -1]);
  assert.deepEqual([cmp.rows.length, cmp.onlyA, cmp.onlyB], [3, 0, 0]);
  const m = compareNumbers(analysisNumbers({ moran: { genes: ['G1', 'G2'], i: [0.3, null] } }), analysisNumbers({ moran: { genes: ['G1'], i: [0.1] } }));
  assert.ok(Math.abs(m.rows[0].change + 0.2) < 1e-12);
  assert.equal(rankCorrelation([{ a: 1, b: 10 }, { a: 2, b: 20 }, { a: 3, b: 30 }]), 1);
  assert.equal(rankCorrelation([{ a: 1, b: 3 }, { a: 2, b: 2 }, { a: 3, b: 1 }]), -1);
  assert.ok(Number.isNaN(rankCorrelation([{ a: 1, b: 2 }])));
  // Linked: each finds the other.
  const linked = linkRemake(project, 'spa-2', 'spa-1', 'cmp-1');
  assert.deepEqual(remakesOf(linked, 'spa-1').map((a) => a.id), ['spa-2']);
  assert.deepEqual(remakesOf(linked, 'spa-2').map((a) => a.id), ['spa-1']);
});
