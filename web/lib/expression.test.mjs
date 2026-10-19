import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject, parseProject, serializeProject } from './project.js';
import { SPATIAL_FAMILIES, addTest, expressionRequest, geneRows, labelWords, spatialEffectWords, spatialFamiliesOf, spatialRequest, statisticByGroup, testRecord, testsOf, volcanoPoints } from './expression.js';

const NOW = '2026-10-19T12:00:00.000Z';

// A design as lib/design.js makes it, by hand (as abundance.test.mjs has it).
const design = {
  ok: true,
  problems: [],
  spec: { unit: 'subject', group: 'condition', reference: 'control', contrasts: [], covariates: [], exclude: {} },
  names: ['Intercept', 'condition[T.treated]'],
  samples: [{ id: 's-1' }, { id: 's-2' }, { id: 's-3' }, { id: 's-4' }],
  rows: [{ unit: 'P1', group: 'control', samples: ['s-1'] }, { unit: 'P2', group: 'control', samples: ['s-2'] }, { unit: 'P3', group: 'treated', samples: ['s-3'] }, { unit: 'P4', group: 'treated', samples: ['s-4'] }],
  matrix: [[1, 0], [1, 0], [1, 1], [1, 1]],
  contrasts: [{ name: 'treated − control', a: 'treated', b: 'control', vector: [0, 1] }],
  excluded: [],
};

test('the requests: the design as the abundance request has it, the level, the fewest cells, the families', () => {
  const e = expressionRequest(design, 'coh-1', { minCells: 20 });
  assert.equal(e.summary, 'coh-1');
  assert.equal(e.minCells, 20);
  assert.equal(e.level, 0.95);
  assert.deepEqual(e.design.rows[2], { unit: 'P3', group: 'treated', samples: ['s-3'], x: [1, 1] });
  assert.deepEqual(e.design.contrasts, [{ name: 'treated − control', vector: [0, 1] }]);
  assert.equal(e.design.excluded, undefined);
  const s = spatialRequest(design, 'coh-1', { families: ['enrichment'], name: 'Main', version: 3 });
  assert.deepEqual(s.families, ['enrichment']);
  assert.equal(s.design.name, 'Main');
  assert.deepEqual(spatialRequest(design, 'coh-1').families, []);
  assert.throws(() => expressionRequest({ ...design, ok: false, problems: ['treated has 1 subject.'] }, 'coh-1'), /cannot be used: treated has 1 subject/);
  assert.throws(() => spatialRequest(design, ''), /Summarize/);
  assert.equal(SPATIAL_FAMILIES.length, 4);
  const doc = { samples: [{ statistics: [{ family: 'moran' }, { family: 'graph' }], nicheShares: [0.5, 0.5] }, { statistics: [{ family: 'enrichment' }] }] };
  assert.deepEqual(spatialFamiliesOf(doc), ['enrichment', 'niche', 'moran']);
});

// An expression comparison as expression.json has it: two labels, the second untested.
const expression = {
  genes: ['CD3E', 'MS4A1', 'EPCAM', 'KRT8'],
  minCells: 10,
  design: { rows: design.rows.map((r, k) => ({ ...r, x: design.matrix[k] })) },
  results: [
    { label: 'T cell', tested: true, cells: [40, 50, 30, 45], rows: [0, 1, 2, 3], genes: [0, 1, 3], aveLogCPM: [12, 9, 8], filtered: 1, span: 1, s2Prior: 0.4, dfPrior: 6.2, dfTotal: 8.2,
      contrasts: [{ contrast: 'treated − control', logFC: [1.5, -0.2, -2.1], lo: [0.9, -0.8, -3], hi: [2.1, 0.4, -1.2], t: [5, -0.7, -4], p: [0.001, 0.5, 0.004], padj: [0.003, 0.5, 0.006], up: 1, down: 1 }] },
    { label: 'Neuron', tested: false, note: '1 row has 10 of its cells or more: too few for the design\'s 2 columns', cells: [0, 3, 12, 1], rows: [2], filtered: 0, dfPrior: null },
  ],
};

test('a label\'s genes by p or fold, filtered; its volcano; its words', () => {
  assert.deepEqual(geneRows(expression, 'T cell', 'treated − control').map((r) => r.gene), ['CD3E', 'KRT8', 'MS4A1']);
  assert.deepEqual(geneRows(expression, 'T cell', 'treated − control', { sort: 'fold', limit: 2 }).map((r) => r.gene), ['KRT8', 'CD3E']);
  assert.deepEqual(geneRows(expression, 'T cell', 'treated − control', { filter: 'ms4' }).map((r) => r.gene), ['MS4A1']);
  assert.deepEqual(geneRows(expression, 'Neuron', 'treated − control'), []);
  assert.deepEqual(geneRows(expression, 'T cell', 'other'), []);
  const v = volcanoPoints(expression, 'T cell', 'treated − control');
  assert.deepEqual(v.map((p) => [p.gene, p.x, p.found]), [['CD3E', 1.5, true], ['KRT8', -2.1, true], ['MS4A1', -0.2, false]]);
  assert.ok(Math.abs(v[0].y - 3) < 1e-12);
  assert.equal(labelWords(expression, 'T cell'), '4 of 4 rows (each with 10 of its cells or more); 3 genes tested, 1 too little expressed; the genes\' variances moderated toward a prior of 6.2 degrees of freedom; tests on 8.2 degrees of freedom.');
  assert.match(labelWords(expression, 'Neuron'), /^Untested: 1 row has 10/);
});

test('a statistic\'s rows by group; effects in words', () => {
  const comparison = {
    design: { rows: expression.design.rows },
    families: [{ family: 'enrichment', log: true, keys: ['A|B'], values: [[0.1, 0.3, null, 1.1]] }],
  };
  const g = statisticByGroup(comparison, 'enrichment', 'A|B');
  assert.deepEqual(g.map((x) => [x.group, x.points.map((p) => p.unit), x.mean]), [['control', ['P1', 'P2'], 0.2], ['treated', ['P4'], 1.1]]);
  assert.deepEqual(statisticByGroup(comparison, 'enrichment', 'B|C'), []);
  assert.equal(spatialEffectWords({ family: 'enrichment', log: true }, { tested: true, ratio: 1.8412, ratioLo: 1.2093, ratioHi: 2.8031 }), '1.84× (1.21 to 2.80)');
  assert.equal(spatialEffectWords({ family: 'niche', log: false }, { tested: true, estimate: 0.042, lo: -0.01, hi: 0.094 }), '+4.2 points (−1.0 to +9.4)');
  assert.equal(spatialEffectWords({ family: 'moran', log: false }, { tested: true, estimate: -0.05, lo: -0.1, hi: 0 }), '−0.050 (−0.100 to 0.000)');
  assert.equal(spatialEffectWords({ family: 'moran' }, { tested: false, note: 'not in every row' }), 'not in every row');
});

test('the comparisons recorded once, each kind apart; the project round-trips them', () => {
  const em = {
    id: 'exp-1', kind: 'expression', name: 'Expression compared: treated − control', state: 'complete', created: NOW,
    lineage: { method: { name: 'SituCraft differential expression', version: '1', citation: 'Law' }, parameters: { minCells: 10 }, job: { id: 'j-1', kind: 'expression' } },
    summary: { summary: 'coh-1', tests: 300, untested: ['Neuron'], below: 2, found: [{ label: 'epithelial-like', gene: 'SIM033', contrast: 'treated − control', logFC: 1.31, padj: 0.001 }, { label: 'T cell', gene: 'CD3E', contrast: 'treated − control', logFC: -0.8, padj: 0.04 }] },
  };
  const sm = {
    id: 'spt-1', kind: 'spatial-comparison', name: 'Spatial statistics compared: treated − control', state: 'complete', created: NOW,
    lineage: { method: { name: 'SituCraft spatial statistics compared', version: '1' }, job: { id: 'j-2', kind: 'spatial-tests' } },
    summary: { summary: 'coh-1', tests: 12, untested: 0, found: [{ family: 'enrichment', key: 'epithelial-like|immune-like', contrast: 'treated − control', estimate: 0.9, ratio: 1.87, padj: 0.02 }] },
  };
  const er = testRecord(em, { summary: 'coh-1' }, NOW);
  const sr = testRecord(sm, { summary: 'coh-1', design: { name: 'Main', version: 2 } }, NOW);
  assert.equal(er.kind, 'expression');
  assert.equal(sr.job.kind, 'spatial-tests');
  let p = addTest(createProject('Cohort', { now: NOW }), er, NOW);
  p = addTest(p, sr, NOW);
  assert.equal(addTest(p, er, NOW), p);
  assert.match(p.history.at(-2).detail, /treated − control\): 300 tests, 2 below an adjusted p of 0.05 \(SIM033 in epithelial-like \+1.31 log2, CD3E in T cell −0.80 log2\); Neuron untested \(artifact exp-1\)/);
  assert.match(p.history.at(-1).detail, /12 tests, 1 below an adjusted p of 0.05 \(epithelial-like\|immune-like \(enrichment\) 1.87×\) \(artifact spt-1\)/);
  const back = parseProject(serializeProject(p));
  assert.deepEqual(testsOf(back, 'expression'), [er]);
  assert.deepEqual(testsOf(back, 'spatial-comparison'), [sr]);
});
