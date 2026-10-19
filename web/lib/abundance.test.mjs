import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject, parseProject, serializeProject } from './project.js';
import { ABUNDANCE_FAMILIES, abundanceRecord, abundanceRequest, addAbundance, comparisonsOf, effectWords, rowsByGroup } from './abundance.js';

const NOW = '2026-10-19T12:00:00.000Z';

// A design as lib/design.js makes it, by hand: two subjects in each group, one subject's two
// sections combined (technical replicates), a sample left out.
const design = {
  ok: true,
  problems: [],
  spec: { unit: 'subject', group: 'condition', reference: 'control', contrasts: [], covariates: [], exclude: { 's-5': 'a fold' } },
  names: ['Intercept', 'condition[T.treated]'],
  samples: [{ id: 's-1' }, { id: 's-2' }, { id: 's-3' }, { id: 's-4' }, { id: 's-6' }],
  rows: [{ unit: 'P1', group: 'control', samples: ['s-1'] }, { unit: 'P2', group: 'control', samples: ['s-2'] }, { unit: 'P3', group: 'treated', samples: ['s-3', 's-6'] }, { unit: 'P4', group: 'treated', samples: ['s-4'] }],
  matrix: [[1, 0], [1, 0], [1, 1], [1, 1]],
  contrasts: [{ name: 'treated − control', a: 'treated', b: 'control', vector: [0, 1] }],
  excluded: [{ id: 's-5', name: 'S5', reason: 'a fold' }],
};

test('a comparison\'s request: the design\'s rows, matrix and contrasts, the samples left out', () => {
  const r = abundanceRequest(design, 'coh-1', { families: ['share'], level: 0.9, name: 'Main', version: 2 });
  assert.equal(r.summary, 'coh-1');
  assert.deepEqual(r.families, ['share']);
  assert.equal(r.level, 0.9);
  assert.deepEqual(r.design.rows[2], { unit: 'P3', group: 'treated', samples: ['s-3', 's-6'], x: [1, 1] });
  assert.deepEqual(r.design.contrasts, [{ name: 'treated − control', vector: [0, 1] }]);
  assert.deepEqual(r.design.excluded, [{ sample: 's-5', name: 'S5', reason: 'a fold' }]);
  assert.equal(r.design.name, 'Main');
  assert.equal(r.design.version, 2);
  assert.throws(() => abundanceRequest({ ...design, ok: false, problems: ['treated has 1 subject.'] }, 'coh-1'), /cannot be used: treated has 1 subject/);
  assert.throws(() => abundanceRequest(design, ''), /Summarize/);
  assert.deepEqual(abundanceRequest(design, 'coh-1').families, ['share', 'density']);
  assert.equal(ABUNDANCE_FAMILIES.length, 3);
});

test('a result\'s rows by group; an effect in words', () => {
  const comparison = {
    design: { rows: abundanceRequest(design, 'coh-1').design.rows },
    families: [{ family: 'share', labels: ['T cell', 'B cell'], values: [[0.1, 0.2, 0.3, 0.5], [0.4, 0.4, 0.4, 0.4]], counts: [[10, 20, 60, 50], [40, 40, 80, 40]], totals: [100, 100, 200, 100] }],
  };
  const g = rowsByGroup(comparison, 'share', 'T cell');
  assert.deepEqual(g.map((x) => [x.group, x.points.map((p) => p.unit), x.mean]), [['control', ['P1', 'P2'], 0.15000000000000002], ['treated', ['P3', 'P4'], 0.4]]);
  assert.deepEqual(g[1].points[0].samples, ['s-3', 's-6']);
  assert.equal(g[1].points[0].total, 200);
  assert.deepEqual(rowsByGroup(comparison, 'share', 'Neuron'), []);
  assert.deepEqual(rowsByGroup(comparison, 'density', 'T cell'), []);
  assert.equal(effectWords({ tested: true, ratio: 1.8412, lo: 1.2093, hi: 2.8031 }), '1.84× (1.21 to 2.80)');
  assert.equal(effectWords({ tested: false, note: 'no sample has a cell of it' }), 'no sample has a cell of it');
});

test('a comparison recorded once, with its summary and design; the project round-trips it', () => {
  const manifest = {
    id: 'abu-1', name: 'Abundance compared: treated − control', state: 'complete', created: NOW,
    lineage: { method: { name: 'SituCraft differential abundance', version: '1', citation: 'Wedderburn' }, parameters: { families: ['share'] }, job: { id: 'j-1', kind: 'abundance' } },
    summary: { summary: 'coh-1', tests: 8, untested: 0, found: [{ family: 'share', label: 'endothelial-like', contrast: 'treated − control', ratio: 1.74, padj: 0.01 }] },
  };
  const record = abundanceRecord(manifest, { summary: 'coh-1', design: { name: 'Main', version: 2 } }, NOW);
  assert.equal(record.kind, 'abundance');
  assert.equal(record.summaryOf.tests, 8);
  const p1 = addAbundance(createProject('Cohort', { now: NOW }), record, NOW);
  assert.equal(addAbundance(p1, record, NOW), p1);
  assert.match(p1.history.at(-1).detail, /treated − control\): 8 tests, 1 below an adjusted p of 0.05 \(endothelial-like 1.74×\) \(artifact abu-1\)/);
  assert.deepEqual(comparisonsOf(parseProject(serializeProject(p1)))[0], record);
});
