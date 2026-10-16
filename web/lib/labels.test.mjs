import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addLabel, addLabels, changeLabel, checkLabelParameters, checkReferenceParameters, composition, compositionOf, describeLabels, labelColor, labelColors, labelFills, labelSetName, labelSetsFor, labelSetsOf, labelTree, labelsPerCell, labelsRecord, lowConfidenceDefault, markerSetFile, markerSetVersions, markerSetsOf, namesFromLabels, oneLevel, parseGenes, parseLabelFill, readLabelCells, removeLabel, saveMarkerSet, withoutMissing } from './labels.js';
import { categoricalColor } from './colormaps.js';

const NOW = '2026-10-16T12:00:00.000Z';
const set = {
  format: 'situcraft-markers', version: 1, name: 'Two levels', levels: ['Class', 'Type'],
  labels: [
    { name: 'A', positive: ['G0', 'G1'] },
    { name: 'B', positive: ['G2'] },
    { name: 'A1', parent: 'A', positive: ['G3'], negative: ['G4'] },
  ],
  states: [{ name: 'Cycling', positive: ['G5'], parent: 'A' }],
};

test('parameters are checked as the program checks them', () => {
  const good = { margin: 0.1, minimum: 0, state: 0.25, bins: 25, controls: 50, lineage: true, seed: 20250 };
  assert.equal(checkLabelParameters(good), null);
  assert.match(checkLabelParameters({ ...good, margin: 0 }), /Margin is a number from 0.01 to 5/);
  assert.match(checkLabelParameters({ ...good, bins: 2.5 }), /Expression bins is a whole number/);
  assert.match(checkLabelParameters({ ...good, lineage: 'yes' }), /lineage/);
});

test('marker sets are versions in the project; the same content is not saved twice', () => {
  let p = createProject('Breast', { now: NOW });
  p = saveMarkerSet(p, { set, checksum: 'a'.repeat(64), source: { starter: 'breast' }, now: NOW });
  const first = markerSetsOf(p)[0];
  assert.equal(first.version, 1);
  assert.equal(first.family, first.id);
  assert.match(p.history.at(-1).detail, /Added the marker set "Two levels" from SituCraft's starter set "breast" \(version 1: 3 labels, 1 state; checksum aaaaaaaaaaaa\)/);
  assert.equal(saveMarkerSet(p, { set, checksum: 'a'.repeat(64), family: first.family, now: NOW }), p, 'no change, no version');
  p = saveMarkerSet(p, { set: { ...set, name: 'Two levels, edited' }, checksum: 'b'.repeat(64), family: first.family, now: NOW });
  assert.equal(markerSetsOf(p).length, 1, 'one set, its latest version');
  assert.equal(markerSetsOf(p)[0].version, 2);
  assert.equal(markerSetsOf(p)[0].parent, first.id);
  assert.deepEqual(markerSetVersions(p, first.family).map((v) => v.version), [1, 2]);
  assert.match(markerSetFile(set), /^\{\n "format": "situcraft-markers"/);
});

test('editing a marker set: genes typed, labels added, renamed and removed with what hangs on them', () => {
  assert.deepEqual(parseGenes('CD3E, cd3e;CD8A  CD4,,'), ['CD3E', 'CD8A', 'CD4']);
  assert.deepEqual(labelTree(set).map((e) => [e.label.name, e.depth]), [['A', 0], ['A1', 1], ['B', 0]]);
  const added = addLabel(set, 'A');
  assert.deepEqual(labelTree(added).map((e) => e.label.name), ['A', 'A1', 'New label', 'B']);
  assert.equal(added.labels.find((l) => l.name === 'New label').parent, 'A');
  assert.equal(addLabel(added, 'A').labels.filter((l) => l.name.startsWith('New label')).length, 2);
  const renamed = changeLabel(set, 'A', { name: 'Alpha' });
  assert.equal(renamed.labels.find((l) => l.name === 'A1').parent, 'Alpha');
  assert.equal(renamed.states[0].parent, 'Alpha');
  assert.equal(changeLabel(set, 'A1', { negative: [] }).labels.find((l) => l.name === 'A1').negative, undefined, 'an empty list is left out');
  const removed = removeLabel(set, 'A');
  assert.deepEqual(removed.labels.map((l) => l.name), ['B']);
  assert.deepEqual(removed.states, [], 'a state of a removed label goes with it');
  const { set: fixed, removed: genes, dropped } = withoutMissing(set, { A1: ['G3', 'G4'], Cycling: ['G5'] });
  assert.deepEqual(genes.map((r) => r.label), ['A1', 'Cycling']);
  assert.deepEqual(dropped, ['A1', 'Cycling'], 'labels left without a positive marker go');
  assert.deepEqual(fixed.labels.map((l) => l.name), ['A', 'B']);
  assert.equal(fixed.states, undefined);
});

const description = {
  levels: ['Class', 'Type'],
  clusters: 2,
  sets: [{ name: 'A', level: 0, parent: -1 }, { name: 'B', level: 0, parent: -1 }, { name: 'A1', level: 1, parent: 0 }, { name: 'Cycling', level: -1, parent: 0, state: true }],
  // Cluster 0: 6 A1, 2 A (stopped), 2 none; cluster 1: 3 B, 1 A, 4 none.
  composition: [
    [[8, 0, 0, 0, 2], [1, 3, 0, 0, 4]],
    [[2, 0, 6, 0, 2], [1, 3, 0, 0, 4]],
  ],
};

test('each cluster\'s labels, and names from the most specific label a share of its cells carry', () => {
  const rows = composition(description, 1);
  assert.deepEqual(rows[0].labels.map((e) => [e.name, e.count]), [['A1', 6], ['A', 2], ['No label', 2]]);
  assert.equal(rows[0].cells, 10);
  assert.equal(rows[1].labels[0].name, 'No label');
  assert.deepEqual(namesFromLabels(description, 0.5), ['A1', null]);
  assert.deepEqual(namesFromLabels(description, 0.7), ['A', null], 'A1 holds 60%: the class, 80%');
  assert.deepEqual(namesFromLabels(description, 0.35), ['A1', 'B']);
});

function cellsBuffer({ segCells, cells, cluster, label, lead, confidence, state }) {
  const n = cells.length;
  const buffer = new ArrayBuffer(16 + n * (8 + 8 * label.length + 4) + n * state.length);
  new Uint32Array(buffer, 0, 4).set([segCells, n, label.length, state.length]);
  let at = 16;
  for (const arr of [cells, cluster]) {
    new Uint32Array(buffer, at, n).set(arr);
    at += 4 * n;
  }
  for (const arr of label) {
    new Int32Array(buffer, at, n).set(arr);
    at += 4 * n;
  }
  for (const arr of [...lead, confidence]) {
    new Float32Array(buffer, at, n).set(arr);
    at += 4 * n;
  }
  for (const arr of state) {
    new Uint8Array(buffer, at, n).set(arr);
    at += n;
  }
  return buffer;
}

test('a label set\'s cells read, labels per segmentation cell and their colors', () => {
  const buffer = cellsBuffer({ segCells: 5, cells: [0, 2, 3], cluster: [0, 1, 0], label: [[0, -1, 1], [2, -1, 1]], lead: [[0.5, 0.02, 0.3], [0.2, NaN, NaN]], confidence: [0.2, 0.02, 0.3], state: [[1, 0, 0]] });
  const data = readLabelCells(buffer);
  assert.equal(data.n, 3);
  assert.deepEqual([...data.label[1]], [2, -1, 1]);
  assert.ok(Math.abs(data.confidence[0] - 0.2) < 1e-7);
  assert.deepEqual([...data.state[0]], [1, 0, 0]);
  const per = labelsPerCell(data, 1, 4);
  assert.deepEqual([...per], [2, -1, 4, 1, -1], 'not analyzed −1, analyzed without a label the unlabeled index');
  const rgba = labelColors(per, 4);
  const hex = (i) => `#${[...rgba.slice(4 * i, 4 * i + 3)].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
  assert.equal(hex(0), categoricalColor(2).toLowerCase());
  assert.equal(rgba[4 * 1 + 3], 0);
  assert.equal(hex(2), '#9a9a9a');
});

test('a label set is recorded with its marker set version, and offered to the Viewer by level', () => {
  let p = createProject('Breast', { now: NOW });
  p = { ...p, datasets: [{ id: 'd1', name: 'breast' }], artifacts: [{ kind: 'clustering', id: 'clu-1', name: 'Clusters of Instrument segmentation, resolution 1', dataset: 'd1', segmentation: 'seg-a' }] };
  p = saveMarkerSet(p, { set, checksum: 'c'.repeat(64), now: NOW, id: 'ms-1' });
  const manifest = {
    id: 'lab-1', kind: 'labels', name: 'Labels of Clusters by Two levels', state: 'complete', created: NOW,
    lineage: { parents: [{ kind: 'bundle', id: 'b' }, { kind: 'segmentation', id: 'seg-a' }, { kind: 'clustering', id: 'clu-1' }], method: { name: 'SituCraft labels from markers', version: '1' }, parameters: { margin: 0.1, minimum: 0, markers: { name: 'Two levels', checksum: 'c'.repeat(64) } } },
    summary: { cells: 100, levels: ['Class', 'Type'], labeled: [80, 40], unlabeled: 20, labels: 3, states: 1 },
  };
  const record = labelsRecord({ id: 'd1' }, manifest, 'ms-1', NOW);
  assert.equal(record.clustering, 'clu-1');
  assert.equal(record.segmentation, 'seg-a');
  p = addLabels(p, record, NOW);
  assert.equal(addLabels(p, record, NOW), p);
  assert.deepEqual(labelSetsOf(p, 'd1', 'clu-1').map((r) => r.id), ['lab-1']);
  assert.match(p.history.at(-1).detail, /Labeled the cells of clusters of instrument segmentation, resolution 1 by the marker set "Two levels" \(version 1\): 80 of 100 cells at the class level, margin 0.1 \(artifact lab-1\)/);
  const fills = labelFills(p, 'd1', 'seg-a');
  assert.deepEqual(fills.map((f) => f.id), ['labels:lab-1:0', 'labels:lab-1:1']);
  assert.equal(fills[1].label, 'Labels by Two levels: type');
  assert.deepEqual(parseLabelFill('labels:lab-1:1'), { id: 'lab-1', level: 1 });
  assert.equal(parseLabelFill('clusters:clu-1'), null);
});

test('labels imported for a segmentation: recorded with their source, carried by its clusterings, counted by cluster, in their own colors', () => {
  let p = createProject('Lung', { now: NOW });
  p = { ...p, datasets: [{ id: 'd1', name: 'lung' }], artifacts: [{ kind: 'segmentation', id: 'seg-a', name: 'Instrument segmentation', dataset: 'd1' }, { kind: 'clustering', id: 'clu-1', name: 'Clusters', dataset: 'd1', segmentation: 'seg-a', state: 'complete' }, { kind: 'clustering', id: 'clu-2', name: 'Clusters of another', dataset: 'd1', segmentation: 'seg-b', state: 'complete' }] };
  const source = { format: 'xenium-explorer', file: 'groups.csv', sha256: 'f'.repeat(64), idColumn: 'cell_id', column: 'group', colorColumn: 'color', rows: 4 };
  const manifest = {
    id: 'lab-9', kind: 'labels', name: 'Labels: group from groups.csv', state: 'complete', created: NOW,
    lineage: { parents: [{ kind: 'bundle', id: 'b' }, { kind: 'segmentation', id: 'seg-a' }], method: { name: 'Imported labels', version: '1' }, parameters: source },
    summary: { cells: 6, levels: ['group'], labeled: [4], unlabeled: 2, labels: 2 },
  };
  const record = labelsRecord({ id: 'd1' }, manifest, null, NOW);
  assert.equal(record.clustering, null);
  assert.deepEqual(record.source, source);
  assert.equal(record.markers, null);
  p = addLabels(p, record, NOW);
  assert.match(p.history.at(-1).detail, /^Imported group from groups.csv for the cells of instrument segmentation: 2 labels, 4 of 6 cells labeled \(SHA-256 ffffffffffff; artifact lab-9\)$/);
  assert.equal(labelSetName(record), 'group from groups.csv');
  assert.equal(labelSetName({ source: { format: 'instrument', title: 'Graph-based' } }), 'The instrument\'s graph-based clusters');
  assert.match(describeLabels(record), /^2 labels imported from groups.csv, column group, matched to the cells by cell_id: 4 of 6 cells labeled$/);
  const clu1 = p.artifacts.find((a) => a.id === 'clu-1');
  const clu2 = p.artifacts.find((a) => a.id === 'clu-2');
  assert.deepEqual(labelSetsFor(p, clu1).map((r) => r.id), ['lab-9'], 'its segmentation\'s clusterings carry it');
  assert.deepEqual(labelSetsFor(p, clu2), []);
  assert.equal(labelFills(p, 'd1', 'seg-a')[0].label, 'Labels: group from groups.csv');
  // Six cells: labels 0, 1, none, 0, none, 1; clusters 0, 0, 1, 1, −1 (not analyzed), 1.
  const data = { description: { sets: [{ name: 'Tumour', color: '#1f77b4' }, { name: 'T cell' }] }, cells: { n: 6, levels: 1, cells: Uint32Array.from([0, 1, 2, 3, 4, 5]), label: [Int32Array.from([0, 1, -1, 0, -1, 1])] } };
  assert.deepEqual(compositionOf(data, Int32Array.from([0, 0, 1, 1, -1, 1]), 2), [[[1, 1, 0], [1, 1, 1]]]);
  assert.equal(labelColor(0, 2, data.description.sets), '#1f77b4', 'the table\'s color');
  assert.equal(labelColor(1, 2, data.description.sets), categoricalColor(1));
});

test('labels from a reference: recorded with the reference and the mapping\'s parameters, named by it, one level, flagged for review below their threshold', () => {
  let p = createProject('Lung', { now: NOW });
  p = { ...p, datasets: [{ id: 'd1', name: 'lung' }], artifacts: [{ kind: 'clustering', id: 'clu-1', name: 'Clusters', dataset: 'd1', segmentation: 'seg-a', state: 'complete' }] };
  const reference = { file: 'lung_atlas.zarr', sha256: 'a'.repeat(64), format: 'anndata', matrix: 'X', labelColumn: 'cell_type', geneColumn: '' };
  const manifest = {
    id: 'lab-7', kind: 'labels', name: 'Labels of Clusters from lung_atlas.zarr', state: 'complete', created: NOW,
    lineage: { parents: [{ kind: 'bundle', id: 'b' }, { kind: 'segmentation', id: 'seg-a' }, { kind: 'clustering', id: 'clu-1' }], method: { name: 'SituCraft labels from a reference', version: '1' }, parameters: { parameters: { tune: 0.05, margin: 0, markers: 0 }, reference } },
    summary: { cells: 10, levels: ['cell_type'], labeled: [9], unlabeled: 1, labels: 3, reference: { format: 'anndata', file: 'lung_atlas.zarr', labels: 3, shared: 280, missing: 20 } },
  };
  const record = labelsRecord({ id: 'd1' }, manifest, null, NOW);
  assert.equal(record.clustering, 'clu-1');
  assert.equal(record.markers, null);
  assert.equal(record.source, null);
  assert.deepEqual(record.reference, reference);
  assert.deepEqual(record.parameters, { tune: 0.05, margin: 0, markers: 0 });
  assert.ok(oneLevel(record) && oneLevel({ source: {} }) && !oneLevel({ markers: {} }));
  assert.equal(labelSetName(record), 'Labels from lung_atlas.zarr (cell_type)');
  p = addLabels(p, record, NOW);
  assert.match(p.history.at(-1).detail, /^Labeled the cells of clusters from the reference lung_atlas.zarr \(cell_type\): 3 labels, 280 of the panel's genes shared; 9 of 10 cells labeled, fine-tuning 0.05, margin 0 \(SHA-256 aaaaaaaaaaaa; artifact lab-7\)$/);
  assert.match(describeLabels(record), /^3 labels from the reference lung_atlas.zarr \(cell_type\), 280 of the panel's genes shared: 9 of 10 cells labeled by their correlation with each label's profile, fine-tuned within 0.05, margin 0/);
  assert.equal(labelFills(p, 'd1', 'seg-a')[0].label, 'Labels: Labels from lung_atlas.zarr (cell_type)');
  // The review's default threshold: the fine-tuning threshold, or twice the margin when more.
  assert.equal(lowConfidenceDefault(record), 0.05);
  assert.equal(lowConfidenceDefault({ ...record, parameters: { tune: 0.05, margin: 0.04 } }), 0.08);
  assert.equal(lowConfidenceDefault({ markers: {}, parameters: { margin: 0.1 } }), 0.2);
  assert.equal(lowConfidenceDefault({ source: {} }), null);
  assert.equal(checkReferenceParameters({ tune: 0.05, margin: 0, markers: 0 }), null);
  assert.equal(checkReferenceParameters({ tune: 0.05, margin: 0, markers: 2.5 }), 'Genes per pair is a whole number from 0 to 10,000.');
  assert.equal(checkReferenceParameters({ tune: -1, margin: 0, markers: 0 }), 'Fine-tuning is a number from 0 to 1.');
});
