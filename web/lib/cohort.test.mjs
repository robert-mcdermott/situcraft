import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject, parseProject, serializeProject } from './project.js';
import { FAMILIES, addSummary, familyCSV, graphAnalysesOf, graphOfSample, itemIndex, latestByQuestion, readCohortTable, summariesOf, summaryRecord, usedNames, vocabularyOf } from './cohort.js';

const NOW = '2026-10-19T12:00:00.000Z';

test('the cohort\'s names: every label of the level, coarser ones only where cells carry them', () => {
  const sets = [
    { name: 'Immune', level: 0 }, { name: 'Epithelial', level: 0 },
    { name: 'T cell', level: 1, parent: 0 }, { name: 'B cell', level: 1, parent: 0 }, { name: 'AT2', level: 1, parent: 1 },
    { name: 'Cycling', level: 0, state: true },
  ];
  assert.deepEqual(vocabularyOf(sets, 0), ['Immune', 'Epithelial']);
  // At level 1, B cell is counted though no cell carries it; Epithelial only because cells do.
  assert.deepEqual(vocabularyOf(sets, 1, new Set(['T cell', 'Epithelial'])), ['Epithelial', 'T cell', 'B cell', 'AT2']);
  // A name used that the set has not (an older version's) comes last; a state never.
  assert.deepEqual(vocabularyOf(sets, 0, new Set(['Stromal', 'Cycling'])), ['Immune', 'Epithelial', 'Stromal']);
  assert.deepEqual([...usedNames({ names: ['A', 'B', 'C'], label: Int32Array.from([2, -1, 0, 2]) })], ['C', 'A']);
});

test('a bundle\'s index among the open files', () => {
  assert.equal(itemIndex({ url: '/api/local/12' }), '12');
  assert.equal(itemIndex({ url: '/api/other/1' }), null);
  assert.equal(itemIndex(null), null);
});

const graph = (id, created, checksum, kind = 'delaunay', extra = {}) => ({ kind: 'graph', id, dataset: 'd-1', segmentation: 'seg-1', state: 'complete', created, summary: { kind, choice: { checksum } }, ...extra });
const answer = (id, question, created, extra = {}) => ({ kind: 'spatial', id, dataset: 'd-1', segmentation: 'seg-1', graph: 'g-2', question, state: 'complete', created, summary: { labels: { id: 'lab-1', level: 0 } }, ...extra });

test('a sample\'s graph is over exactly its cells; its answers the latest of each question', () => {
  const p = {
    artifacts: [
      graph('g-1', '2026-01-01', 'aaa'), graph('g-2', '2026-01-03', 'aaa'), graph('g-3', '2026-01-04', 'bbb'),
      graph('g-4', '2026-01-05', 'aaa', 'knn'), graph('g-5', '2026-01-06', 'aaa', 'delaunay', { state: 'failed' }),
      answer('e-1', 'enrichment', '2026-01-04'), answer('e-2', 'enrichment', '2026-01-05'),
      answer('e-3', 'enrichment', '2026-01-06', { summary: { labels: { id: 'lab-2' } } }),
      answer('e-4', 'enrichment', '2026-01-07', { summary: { labels: { id: 'lab-1' }, strata: [{ id: 'r-1' }] } }),
      answer('m-1', 'moran', '2026-01-04', { summary: {} }), answer('c-1', 'cooccurrence', '2026-01-08'),
    ],
  };
  assert.equal(graphOfSample(p, 'd-1', 'seg-1', 'aaa', 'delaunay').id, 'g-2');
  assert.equal(graphOfSample(p, 'd-1', 'seg-1', 'aaa').id, 'g-4');
  assert.equal(graphOfSample(p, 'd-1', 'seg-1', 'ccc'), null);
  assert.equal(graphOfSample(p, 'd-1', 'seg-2', 'aaa'), null);
  // Of the enrichments over g-2: those of other labels and those dealt within regions are left out.
  assert.deepEqual(graphAnalysesOf(p, 'd-1', 'g-2', 'lab-1').map((a) => a.id).sort(), ['e-2', 'm-1']);
  assert.deepEqual(graphAnalysesOf(p, 'd-1', 'g-2', null).map((a) => a.id), ['m-1']);
  assert.deepEqual(latestByQuestion([]), []);
});

test('a summary recorded once, with what it is of; the project round-trips it', () => {
  const manifest = {
    id: 'coh-1', name: 'Summary of 2 samples by Types', state: 'complete', created: NOW,
    lineage: { method: { name: 'SituCraft cohort summary', version: '1', citation: 'Crowell' }, parameters: { closing: 10 }, job: { id: 'j-1', kind: 'cohort' } },
    summary: { samples: [{ sample: 's-1' }, { sample: 's-2' }], labels: { name: 'Types', names: ['A', 'B'] }, genes: 300, niches: 4 },
  };
  const record = summaryRecord(manifest, { markerSet: 'ms-1', level: 1, samples: ['s-1', 's-2'] }, NOW);
  assert.equal(record.kind, 'cohort-summary');
  assert.deepEqual([record.markerSet, record.level, record.samples], ['ms-1', 1, ['s-1', 's-2']]);
  const p0 = createProject('Cohort', { now: NOW });
  const p1 = addSummary(p0, record, NOW);
  assert.equal(addSummary(p1, record, NOW), p1);
  assert.equal(summariesOf(p1).length, 1);
  assert.match(p1.history.at(-1).detail, /Summarized 2 samples by Types: 2 labels, 300 genes, 4 niches across them \(artifact coh-1\)/);
  const back = parseProject(serializeProject(p1));
  assert.deepEqual(summariesOf(back)[0], record);
});

test('the cohort table read into families, and a family as a wide CSV', () => {
  const text = [
    'sample,name,family,key,value,source',
    's-1,First,cells,T cell,12,lab-1',
    's-1,First,cells,"B, plasma",3,lab-1',
    's-1,First,niche,niche 1,0.25,',
    's-1,First,qc,cells,15,seg-1',
    's-1,First,enrichment,"B, plasma|T cell",1.5,spa-1',
    's-2,"Second ""core""",cells,T cell,0,lab-2',
    's-2,"Second ""core""",cells,"B, plasma",7,lab-2',
    's-2,"Second ""core""",moran,CD3E,0.30000000000000004,spa-9',
    '',
  ].join('\n');
  const t = readCohortTable(text);
  assert.deepEqual(t.samples, [{ sample: 's-1', name: 'First' }, { sample: 's-2', name: 'Second "core"' }]);
  assert.deepEqual(t.families.map((f) => f.id), ['cells', 'niche', 'qc', 'enrichment', 'moran']);
  const cells = t.families[0];
  assert.equal(cells.label, FAMILIES[0].label);
  assert.deepEqual(cells.keys, ['T cell', 'B, plasma']);
  assert.deepEqual(cells.values.get('s-2').get('B, plasma'), { value: 7, source: 'lab-2' });
  assert.equal(t.families[4].values.get('s-2').get('CD3E').value, 0.1 + 0.2);
  assert.equal(t.families[1].values.get('s-1').get('niche 1').source, '');
  assert.equal(familyCSV(t, 'cells'), 'sample,name,T cell,"B, plasma"\ns-1,First,12,3\ns-2,"Second ""core""",0,7\n');
  // A sample without a family's number has an empty cell, not a zero.
  assert.equal(familyCSV(t, 'enrichment'), 'sample,name,"B, plasma|T cell"\ns-1,First,1.5\ns-2,"Second ""core""",\n');
  assert.equal(familyCSV(t, 'graph'), '');
  assert.throws(() => readCohortTable('a,b\n1,2\n'), /not a cohort table/);
  assert.throws(() => readCohortTable('sample,name,family,key,value,source\ns,n,cells,A,1\n'), /5 fields/);
});
