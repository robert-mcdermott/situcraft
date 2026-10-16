import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addClustering, addEmbedding, checkParameters, clusterColors, clusterFills, clusterName, clusterNames, clusteringRecord, clusteringsOf, contingency, describeClustering, embeddingRecord, filterRequest, mapWarning, mapsOf, mergedOf, nameClusters, nameOf } from './clusterings.js';
import { keepChecksum } from './filters.js';
import { categoricalColor } from './colormaps.js';

const NOW = '2026-10-16T12:00:00.000Z';
const manifest = {
  id: 'clu-1', kind: 'clustering', name: 'Clusters of Instrument segmentation, resolution 1', state: 'complete', created: NOW,
  lineage: {
    parents: [{ kind: 'bundle', id: 'b1' }, { kind: 'segmentation', id: 'seg-a', name: 'Instrument segmentation' }],
    job: { id: 'j1-1', kind: 'clustering' },
    method: { name: 'SituCraft clustering', version: '1', citation: 'Wolf FA, et al. 2018' },
    parameters: { minTranscripts: 10, components: 50, neighbors: 15, resolution: 1, seed: 20250 },
    software: [{ name: 'situcraft', version: '0.4.0' }],
  },
  summary: { cells: 11820, clusters: 29, sizes: [1890, 723], modularity: 0.6177, components: 50, resolution: 1 },
  findings: [],
};

function project() {
  const p = createProject('Lung', { now: NOW });
  return { ...p, datasets: [{ id: 'd1', name: 'xenium-lung-2fov' }], artifacts: [{ kind: 'segmentation', id: 'seg-a', name: 'Instrument segmentation', dataset: 'd1' }] };
}

test('a clustering is recorded with its segmentation and parameters, once, its history said', () => {
  const record = clusteringRecord({ id: 'd1' }, manifest, NOW);
  assert.equal(record.segmentation, 'seg-a');
  assert.equal(record.parameters.resolution, 1);
  let p = addClustering(project(), record, NOW);
  assert.deepEqual(clusteringsOf(p, 'd1').map((c) => c.id), ['clu-1']);
  assert.deepEqual(clusteringsOf(p, 'd1', 'seg-b'), []);
  assert.match(p.history.at(-1).detail, /Clustered the cells of instrument segmentation of xenium-lung-2fov: 29 clusters of 11820 cells at resolution 1 \(artifact clu-1\)/);
  assert.equal(addClustering(p, record, NOW), p);
  p = addClustering(p, { ...record, id: 'clu-2', state: 'invalid' }, NOW);
  assert.deepEqual(clusterFills(p, 'd1', 'seg-a').map((f) => f.id), ['clusters:clu-1'], 'only complete clusterings fill');
  assert.match(clusterFills(p, 'd1', 'seg-a')[0].label, /^Clusters, resolution 1 \(29\)$/);
  assert.match(describeClustering(record), /^29 clusters of 11,820 cells with at least 10 gene transcripts, resolution 1, 15 neighbors in 50 components, seed 20250$/);
  assert.equal(clusterName(0), 'Cluster 1');
});

test('each cell is colored by its cluster, cells not analyzed left clear', () => {
  const rgba = clusterColors(Int32Array.from([0, -1, 2, 0]));
  const hex = (i) => [...rgba.slice(4 * i, 4 * i + 3)].map((v) => v.toString(16).padStart(2, '0')).join('');
  assert.equal(`#${hex(0)}`, categoricalColor(0).toLowerCase());
  assert.equal(`#${hex(2)}`, categoricalColor(2).toLowerCase());
  assert.deepEqual([...rgba.slice(4, 8)], [0, 0, 0, 0]);
  assert.equal(rgba[3], 255);
});

test('a QC filter is sent as the bytes its checksum is of', () => {
  const keep = Uint8Array.from({ length: 21 }, (_, i) => (i % 3 === 0 ? 1 : 0));
  const filter = { id: 'f-1', name: 'Enough signal', version: 2, result: { sha256: keepChecksum(keep) } };
  const req = filterRequest(filter, keep);
  assert.equal(req.name, 'Enough signal v2');
  assert.equal(req.checksum, keepChecksum(keep));
  const bytes = Uint8Array.from(atob(req.keep), (c) => c.charCodeAt(0));
  assert.equal(new DataView(bytes.buffer).getUint32(0, true), 21);
  assert.equal(bytes.length, 4 + 3);
  for (let i = 0; i < 21; i += 1) assert.equal((bytes[4 + (i >> 3)] >> (i & 7)) & 1, keep[i]);
});

test('parameters are checked as the program checks them', () => {
  const good = { minTranscripts: 10, components: 50, neighbors: 15, resolution: 1, seed: 20250 };
  assert.equal(checkParameters(good), null);
  assert.match(checkParameters({ ...good, components: 1 }), /Principal components is a whole number from 2 to 200/);
  assert.match(checkParameters({ ...good, neighbors: 15.5 }), /Neighbors/);
  assert.match(checkParameters({ ...good, resolution: 0 }), /Resolution/);
  assert.match(checkParameters({ ...good, minTranscripts: NaN }), /Gene transcripts at least/);
});

test('names are versions, appended; the same name merges clusters', () => {
  let p = addClustering(project(), clusteringRecord({ id: 'd1' }, manifest, NOW), NOW);
  assert.equal(clusterNames(p, 'clu-1'), null);
  assert.equal(nameOf(null, 2), 'Cluster 3');
  p = nameClusters(p, { clustering: 'clu-1', names: ['T cells', '', 'T cells'], now: NOW });
  const v1 = clusterNames(p, 'clu-1');
  assert.equal(v1.version, 1);
  assert.equal(v1.names.length, 29, 'a name or null for every cluster');
  assert.deepEqual(v1.names.slice(0, 3), ['T cells', null, 'T cells']);
  assert.match(p.history.at(-1).detail, /version 1\): Cluster 1 "T cells", Cluster 3 "T cells"/);
  assert.deepEqual(mergedOf(v1, 4), [0, 1, 0, 3]);
  assert.equal(nameClusters(p, { clustering: 'clu-1', names: v1.names, now: NOW }), p, 'no change, no version');
  const names = [...v1.names];
  names[2] = 'Dividing T cells';
  p = nameClusters(p, { clustering: 'clu-1', names, now: NOW });
  const v2 = clusterNames(p, 'clu-1');
  assert.equal(v2.version, 2);
  assert.equal(v2.parent, v1.id);
  assert.equal(p.artifacts.filter((a) => a.kind === 'cluster-names').length, 2, 'the first version kept');
  assert.throws(() => nameClusters(p, { clustering: 'clu-9', names: [] }), /no such clustering/);
});

test('a map is recorded with its segmentation and clustering', () => {
  const m = { id: 'emb-1', kind: 'embedding', name: 'Map of Clusters', state: 'complete', created: NOW, lineage: { parents: [{ kind: 'bundle', id: 'b' }, { kind: 'segmentation', id: 'seg-a' }, { kind: 'clustering', id: 'clu-1' }], method: { name: 'SituCraft UMAP', version: '1' } }, summary: { trustworthiness: 0.7769, precision: 0.15, recall: 0.24 } };
  let p = addEmbedding(project(), embeddingRecord({ id: 'd1' }, m, NOW), NOW);
  assert.deepEqual(mapsOf(p, 'd1', 'seg-a').map((x) => x.clustering), ['clu-1']);
  assert.match(p.history.at(-1).detail, /trustworthiness 0.777 \(artifact emb-1\)/);
  assert.equal(addEmbedding(p, embeddingRecord({ id: 'd1' }, m, NOW), NOW), p);
  assert.equal(mapWarning(m.summary).level, 'warning');
  assert.match(mapWarning(m.summary).text, /distorts much \(trustworthiness 0.777\): a cell's 15 nearest cells in expression are, on average, 24% among its 75 nearest/);
  assert.equal(mapWarning({ trustworthiness: 0.95 }).level, 'info');
  assert.equal(mapWarning(null), null);
});

test('two clusterings compared: the contingency and the adjusted Rand index', () => {
  // scikit-learn's adjusted_rand_score([0,0,1,1],[0,0,1,2]) is 4/7; cells either leaves out are not counted.
  const ct = contingency(Int32Array.from([0, 0, 1, 1, -1, 0]), Int32Array.from([0, 0, 1, 2, 1, -1]), 2, 3);
  assert.equal(ct.cells, 4);
  assert.deepEqual(ct.table, [[2, 0, 0], [0, 1, 1]]);
  assert.ok(Math.abs(ct.ari - 4 / 7) < 1e-15);
  assert.equal(contingency(Int32Array.from([0, 1, 1]), Int32Array.from([5 - 5, 1, 1]), 2, 2).ari, 1);
});
