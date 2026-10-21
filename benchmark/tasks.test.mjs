// The benchmark's graders given right, partly right and wrong answers, against a project made up
// here (what describe_project, get_result and query_cells would answer), and how answers are read
// (wave 9, slice 8). Adapted from CytoWeave 0.8.0's benchmark/tasks.test.mjs (the same author's).
//
//   node --test benchmark/tasks.test.mjs

import assert from 'node:assert/strict';
import test from 'node:test';
import { OPEN_DATA, TASKS, parseAnswer } from './tasks.mjs';

const task = (id) => TASKS.find((t) => t.id === id);
const score = async (id, answer, { call = project(), truth, outputs = '/nonexistent' } = {}) => {
  const graded = await task(id).grade({ answer, text: '', call, truth, outputs, toolCalls: [] });
  return +graded.parts.reduce((a, p) => a + p.weight * p.score, 0).toFixed(4);
};

// A project as SituCraft's tools would answer for it: its results; each label set's table and,
// by label, its cells; each comparison's design terms; each saved filter's cells.
function project({ results = [], labels = {}, designs = {}, filters = {} } = {}) {
  const page = (ids) => ({ data: { kept: ids.length, cells: 100, rows: { columns: ['row', 'id', 'x', 'y'], rows: ids.map((id, i) => [i, id, 0, 0]) } } });
  return async (name, args) => {
    if (name === 'describe_project') return { data: { results } };
    if (name === 'get_result') {
      const table = labels[args.id] && { columns: ['level', 'label', 'cells', 'share'], rows: Object.entries(labels[args.id]).map(([label, ids]) => ['Type', label, ids.length, 0]) };
      return { data: { table, parameters: { design: { names: designs[args.id] ?? [] } } } };
    }
    if (name === 'query_cells') {
      const where = args.where;
      if (where.labels) return page(labels[where.labels]?.[where.in[0]] ?? []);
      if (where.filter) {
        if (!filters[where.filter]) throw new Error(`query_cells: no filter "${where.filter}"`);
        return page(filters[where.filter]);
      }
    }
    throw new Error(`Not answered here: ${name}`);
  };
}

test('answers are read from the last ANSWER line, in or out of a code block', () => {
  assert.deepEqual(parseAnswer('Some text.\nANSWER: {"a": 1}'), { a: 1 });
  assert.deepEqual(parseAnswer('First ANSWER: {"a": 1}\nThen ANSWER: ```json\n{"a": 2, "b": {"c": [1]}}\n```'), { a: 2, b: { c: [1] } });
  assert.equal(parseAnswer('No answer here'), null);
  assert.equal(parseAnswer('ANSWER: {not json}'), null);
});

test('every task is complete, its seed its own, its prompt ending in the answer line', () => {
  const seeds = new Set();
  for (const t of TASKS) {
    for (const key of ['id', 'category', 'title', 'example', 'prompt', 'truth', 'grade', 'expert']) assert.ok(t[key], `${t.id} lacks ${key}`);
    assert.match(t.prompt({ outputs: '/tmp/out' }), /ANSWER: followed by JSON/);
    assert.doesNotMatch(t.prompt({ outputs: '/tmp/out' }), /simulat|example/i, `${t.id}: the prompt names where its data came from`);
    assert.ok(['tissue', 'cohort', 'lung'].includes(t.example.kind), t.id);
    if (t.example.kind === 'lung') continue;
    const seed = t.example.seed;
    assert.ok(Number.isInteger(seed) && !seeds.has(seed), `${t.id}: seed ${seed}`);
    seeds.add(seed);
  }
  assert.equal(new Set(TASKS.map((t) => t.id)).size, TASKS.length);
  assert.match(OPEN_DATA, /"Experiment"/);
});

test('the planted effects are not the examples\' own', () => {
  // The examples' cohort as situcraft simulate --cohort prints it.
  const printed = { seed: 20261019, effects: [{ kind: 'share', group: 'treated', type: 'endothelial-like', fold: 2 }, { kind: 'share', group: 'treated', type: 'immune-like', within: 'epithelial-like', fold: 4 }, { kind: 'level', group: 'treated', type: 'epithelial-like', gene: 'SIM033', fold: 2.5 }] };
  const expression = task('cohort-expression').example.params(printed);
  assert.deepEqual(expression.effects.map((e) => [e.gene, e.type]), [['SIM072', 'fibroblast-like']]);
  const paired = task('cohort-paired').example.params({ ...printed, paired: true });
  assert.deepEqual(paired.effects.map((e) => [e.kind, e.type, e.fold]), [['share', 'immune-like', 0.25]]);
  assert.deepEqual(task('cohort-outlier').example.params(printed).outliers, [{ sample: 3, detection: 0.4 }]);
  assert.equal(task('cohort-null').example.params(printed).effects.length, 3, 'the null preset has none of its own; its parameters are left');
  for (const t of TASKS.filter((x) => x.example.kind === 'cohort')) assert.equal(t.example.params(printed).panel_seed, 20261019, t.id);
  assert.deepEqual(task('tissue-cell-types').example.params({ domains: 6 }).type_odds, [1, 0.6, 0.25, 2.5]);
  const neighbors = task('tissue-neighbors').example.params({ domains: 6 });
  assert.deepEqual(neighbors.domain_odds, [{ leader: 2, type: 1, odds: 6 }]);
});

const TYPES = ['epithelial-like', 'fibroblast-like', 'immune-like', 'endothelial-like'];
// Forty cells, ten of each type in turn.
const cellTypes = () => {
  const lines = ['cell_id,true_cell_id,cell_type'];
  for (let i = 0; i < 40; i += 1) lines.push(`c${i},t${i},${TYPES[i % 4]}`);
  return task('tissue-cell-types').truth({ truth: { cellTypes: `${lines.join('\n')}\n`, simulation: { parameters: { types: TYPES.map((name) => ({ name })) } } } });
};

test('partial credit: cell types', async () => {
  const truth = cellTypes();
  assert.equal(truth.typeOf.size, 40);
  assert.deepEqual(Object.values(truth.percent), [25, 25, 25, 25]);
  const byType = (wrong = 0) => Object.fromEntries(TYPES.map((t, k) => [t, Array.from({ length: 10 }, (_, j) => `c${4 * j + ((k + (j < wrong ? 1 : 0)) % 4)}`)]));
  const results = [{ id: 'lab-1', kind: 'labels', name: 'Labels' }];
  const shares = { 'Epithelial cells': 25, fibroblast: 25, 'immune-like': 26, 'Endothelial-like': 24 };
  assert.equal(await score('tissue-cell-types', { percentages: shares }, { truth, call: project({ results, labels: { 'lab-1': byType() } }) }), 1);
  // A fifth of the cells wrong: the label set's credit falls to two thirds.
  assert.equal(await score('tissue-cell-types', { percentages: shares }, { truth, call: project({ results, labels: { 'lab-1': byType(2) } }) }), +(0.6 * (2 / 3) + 0.4).toFixed(4));
  assert.equal(await score('tissue-cell-types', null, { truth, call: project({ results, labels: { 'lab-1': byType() } }) }), 0.6, 'labels without the percentages');
  assert.equal(await score('tissue-cell-types', { percentages: shares }, { truth }), 0.4, 'percentages without labels');
  // Labels named otherwise than the types are not taken for them.
  assert.equal(await score('tissue-cell-types', null, { truth, call: project({ results, labels: { 'lab-1': { 1: byType()['epithelial-like'] } } }) }), 0);
});

test('partial credit: neighbors, the outlier and the comparisons', async () => {
  const neighborsTruth = { pair: ['immune-like', 'fibroblast-like'] };
  const enriched = project({ results: [{ id: 'spa-1', kind: 'spatial', name: 'Neighborhood enrichment of Labels' }] });
  assert.equal(await score('tissue-neighbors', { pair: ['Fibroblast', 'Immune'] }, { truth: neighborsTruth, call: enriched }), 1);
  assert.equal(await score('tissue-neighbors', { pair: ['fibroblast-like', 'immune-like'] }, { truth: neighborsTruth }), 0.7, 'no analysis in the project');
  assert.equal(await score('tissue-neighbors', { pair: ['epithelial-like', 'immune-like'] }, { truth: neighborsTruth, call: enriched }), 0.3);
  assert.equal(await score('tissue-neighbors', { pair: ['immune-like', 'immune-like'] }, { truth: neighborsTruth, call: enriched }), 0.3, 'one type twice');

  const outlierTruth = { outliers: ['S03'] };
  const summarized = project({ results: [{ id: 'coh-1', kind: 'cohort-summary' }] });
  assert.equal(await score('cohort-outlier', { outliers: ['S03'], problem: 'far fewer transcripts per cell' }, { truth: outlierTruth, call: summarized }), 1);
  assert.equal(await score('cohort-outlier', { outliers: ['S03', 'S06'], problem: 'low median transcripts' }, { truth: outlierTruth, call: summarized }), 0.7);
  assert.equal(await score('cohort-outlier', { outliers: ['S03'], problem: 'it looks odd' }, { truth: outlierTruth, call: summarized }), 0.8);
  assert.equal(await score('cohort-outlier', { outliers: ['S06'], problem: 'low transcripts' }, { truth: outlierTruth }), 0);

  const level = { kind: 'level', type: 'fibroblast-like', gene: 'SIM072', fold: 3 };
  const compared = project({ results: [{ id: 'exp-1', kind: 'expression' }] });
  assert.equal(await score('cohort-expression', { changes: [{ gene: 'SIM072', cellType: 'Fibroblasts', direction: 'up' }] }, { truth: level, call: compared }), 1);
  assert.equal(await score('cohort-expression', { changes: [{ gene: 'sim072', cellType: 'fibroblast-like', direction: 'increased' }, { gene: 'SIM033', cellType: 'epithelial-like', direction: 'up' }] }, { truth: level, call: compared }), 0.9, 'another change claimed');
  assert.equal(await score('cohort-expression', { changes: [{ gene: 'SIM072', cellType: 'epithelial-like', direction: 'down' }] }, { truth: level, call: compared }), 0.65);
  assert.equal(await score('cohort-expression', { changes: [{ gene: 'SIM033', cellType: 'epithelial-like', direction: 'up' }] }, { truth: level, call: compared }), 0.2, 'the examples\' gene');

  const both = project({ results: [{ id: 'abu-1', kind: 'abundance' }, { id: 'exp-1', kind: 'expression' }] });
  assert.equal(await score('cohort-null', { abundance: [], expression: [] }, { truth: { effects: [] }, call: both }), 1);
  assert.equal(await score('cohort-null', { abundance: [], expression: [] }, { truth: { effects: [] } }), 0, 'nothing compared');
  assert.equal(await score('cohort-null', { abundance: ['immune-like'], expression: [] }, { truth: { effects: [] }, call: both }), 0.5);
  assert.equal(await score('cohort-null', null, { truth: { effects: [] }, call: both }), 0);

  const share = { kind: 'share', type: 'immune-like', fold: 0.25 };
  const paired = project({ results: [{ id: 'abu-1', kind: 'abundance' }], designs: { 'abu-1': ['Intercept', 'condition[T.after]', 'subject[T.S02]'] } });
  const unpaired = project({ results: [{ id: 'abu-1', kind: 'abundance' }], designs: { 'abu-1': ['Intercept', 'condition[T.after]'] } });
  assert.equal(await score('cohort-paired', { changes: [{ cellType: 'Immune cells', direction: 'down' }] }, { truth: share, call: paired }), 1);
  assert.equal(await score('cohort-paired', { changes: [{ cellType: 'immune-like', direction: 'decrease' }] }, { truth: share, call: unpaired }), 0.7, 'not paired');
  assert.equal(await score('cohort-paired', { changes: [{ cellType: 'immune-like', direction: 'up' }] }, { truth: share, call: paired }), 0.8);
  assert.equal(await score('cohort-paired', { changes: [{ cellType: 'endothelial-like', direction: 'up' }] }, { truth: share, call: paired }), 0.3, 'the examples\' type');
});

test('partial credit: the Lung\'s endothelial cells and quality', async () => {
  const endothelial = new Set(Array.from({ length: 200 }, (_, i) => `c${i}`));
  const truth = { endothelial, withACE: 100 };
  const exact = project({ filters: { 'Endothelial cells': [...endothelial] } });
  assert.equal(await score('lung-endothelial', { endothelial: 200, withACE: 101 }, { truth, call: exact }), 1);
  assert.equal(await score('lung-endothelial', { endothelial: 200, withACE: 100 }, { truth }), 0.5, 'no filter');
  // A filter of 180 of them and 20 others (a Jaccard index of 0.818), 10% too many, ACE 5 off.
  const off = project({ filters: { 'Endothelial cells': [...[...endothelial].slice(0, 180), ...Array.from({ length: 20 }, (_, i) => `x${i}`)] } });
  const graded = await score('lung-endothelial', { endothelial: 220, withACE: 105 }, { truth, call: off });
  assert.ok(graded > 0.1 && graded < 0.2, String(graded));
  assert.equal(await score('lung-endothelial', { endothelial: 0, withACE: 0 }, { truth }), 0, 'nothing found');

  const qc = { cells: 11898, medianTranscripts: 56, under30Percent: 14.11 };
  assert.equal(await score('lung-qc', { cells: 11898, medianTranscripts: 56, under30Percent: 14.1 }, { truth: qc }), 1);
  assert.equal(await score('lung-qc', { cells: 11898, medianTranscripts: 56, under30Percent: 0.66 }, { truth: qc }), 0.6, 'the share under 10 transcripts');
  assert.equal(await score('lung-qc', { cells: 12000, medianTranscripts: 120, under30Percent: 30 }, { truth: qc }), 0);
});

test('partial credit: files a task asks for, absent', async () => {
  assert.equal(await score('tissue-report', { figure: '/nonexistent/cell-types.pdf' }, { truth: task('tissue-report').truth() }), 0);
  assert.equal(await score('tissue-certificate', { certificate: '/nonexistent/neighbors-certificate.zip' }, { truth: task('tissue-certificate').truth() }), 0);
});
