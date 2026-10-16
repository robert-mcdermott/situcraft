import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addClustering, checkParameters, clusterColors, clusterFills, clusterName, clusteringRecord, clusteringsOf, describeClustering, filterRequest } from './clusterings.js';
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
