import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject } from './project.js';
import { addRegion, rectangle } from './regions.js';
import { addSampleField, setRegionSamples, setSampleField } from './samples.js';
import { addDesign, columnRank, designChanges, designFields, designMatrix, designVersions, designsOf, makeDesign, normalizeSpec } from './design.js';

const NOW = '2026-10-19T12:00:00.000Z';

// A project of sections S01…, each with fields given: [{ subject, condition, ...more }], and a
// slide (a bundle field) when given.
function cohort(rows, { numbers = [] } = {}) {
  let p = { ...createProject('Cohort', { now: NOW }), datasets: rows.map((r, k) => ({ id: `d-${k + 1}`, name: `S${String(k + 1).padStart(2, '0')}`, path: `/data/S${k + 1}`, run: r.run ?? 'run 1', slide: r.slide ?? 'slide 1', panel: 'p', panelDesign: r.panel ?? 'panel 1', software: 'sim', cells: 100 })) };
  const extra = [...new Set(rows.flatMap((r) => Object.keys(r)).filter((k) => !['subject', 'condition', 'slide', 'run', 'panel'].includes(k)))];
  for (const f of extra) p = addSampleField(p, f, numbers.includes(f) ? 'number' : 'text', NOW);
  rows.forEach((r, k) => {
    for (const [f, v] of Object.entries(r)) {
      if (['slide', 'run', 'panel'].includes(f) || v === undefined) continue;
      p = setSampleField(p, `s-d-${k + 1}`, f, v, NOW);
    }
  });
  return p;
}

const unpaired = (n = 4) => cohort(Array.from({ length: 2 * n }, (_, k) => ({ subject: `P${k + 1}`, condition: k < n ? 'control' : 'treated', slide: `slide ${(k % 2) + 1}` })));

test('an unpaired design: the subjects in two groups, the treated against the control', () => {
  const d = makeDesign(unpaired(), { unit: 'subject', group: 'condition', reference: 'control' });
  assert.equal(d.ok, true, d.problems.join(' '));
  assert.deepEqual(d.names, ['Intercept', 'condition[T.treated]']);
  assert.equal(d.rows.length, 8);
  assert.deepEqual(d.matrix.map((r) => r.join('')), ['10', '10', '10', '10', '11', '11', '11', '11']);
  assert.equal(d.rank, 2);
  assert.equal(d.df, 6);
  assert.equal(d.paired, false);
  assert.deepEqual(d.contrasts, [{ name: 'treated − control', a: 'treated', b: 'control', vector: [0, 1] }]);
  assert.match(d.words[1], /4 subjects in control, 4 subjects in treated/);
  assert.match(d.words.at(-1), /treated − control: from 4 subjects in treated and 4 subjects in control/);
  assert.deepEqual(d.notes, []);
});

test('a paired design: each subject in both groups, the subjects in the model', () => {
  const p = cohort([1, 2, 3, 4].flatMap((i) => ['before', 'after'].map((c) => ({ subject: `S${i}`, condition: c, slide: `slide ${i}` }))));
  const d = makeDesign(p, { unit: 'subject', group: 'condition', reference: 'before' });
  assert.equal(d.ok, true, d.problems.join(' '));
  assert.equal(d.paired, true);
  assert.deepEqual(d.names, ['Intercept', 'condition[T.after]', 'subject[T.S2]', 'subject[T.S3]', 'subject[T.S4]']);
  assert.equal(d.df, 3);
  assert.match(d.words[1], /4 subjects, each in more than one condition, so the design is paired/);
  assert.match(d.words.at(-1), /4 subjects in both/);
  // Each subject's two samples on one slide: every slide holds both groups.
  assert.equal(d.problems.length, 0);
});

test('covariates: a number as it is, a factor in treatment coding', () => {
  const p = cohort([
    { subject: 'P1', condition: 'control', age: '61', sex: 'F' }, { subject: 'P2', condition: 'control', age: '55', sex: 'M' }, { subject: 'P3', condition: 'control', age: '70', sex: 'F' },
    { subject: 'P4', condition: 'treated', age: '48', sex: 'M' }, { subject: 'P5', condition: 'treated', age: '66', sex: 'F' }, { subject: 'P6', condition: 'treated', age: '59', sex: 'M' },
  ], { numbers: ['age'] });
  const d = makeDesign(p, { unit: 'subject', group: 'condition', covariates: ['age', 'sex'] });
  assert.equal(d.ok, true, d.problems.join(' '));
  assert.deepEqual(d.names, ['Intercept', 'condition[T.treated]', 'sex[T.M]', 'age']);
  assert.deepEqual(d.matrix[1], [1, 0, 1, 55]);
  assert.deepEqual(d.matrix[4], [1, 1, 0, 66]);
  assert.equal(d.df, 2);
  assert.match(d.words[2], /Adjusted for age \(a number\) and sex \(2 levels\)/);
  assert.match(d.notes.join(' '), /Only 2 degrees of freedom/);
});

test('an unbalanced design, and three groups with a contrast between two of them', () => {
  const p = cohort(['a', 'a', 'a', 'b', 'b', 'c', 'c', 'c', 'c'].map((g, k) => ({ subject: `P${k}`, condition: g })));
  const d = makeDesign(p, { unit: 'subject', group: 'condition', reference: 'a', contrasts: [['b', 'a'], ['c', 'b']] });
  assert.equal(d.ok, true, d.problems.join(' '));
  assert.deepEqual(d.names, ['Intercept', 'condition[T.b]', 'condition[T.c]']);
  assert.deepEqual(d.contrasts.map((c) => [c.name, c.vector.join(',')]), [['b − a', '0,1,0'], ['c − b', '0,-1,1']]);
  assert.deepEqual(makeDesign(p, { unit: 'subject', group: 'condition' }).contrasts.map((c) => c.name), ['b − a', 'c − a']);
  assert.match(makeDesign(p, { unit: 'subject', group: 'condition', contrasts: [['b', 'z']] }).problems.join(' '), /not of two of the conditions/);
});

test('technical replicates: a subject\'s samples in one group are one row', () => {
  const p = cohort([{ subject: 'P1', condition: 'control' }, { subject: 'P1', condition: 'control' }, { subject: 'P2', condition: 'control' }, { subject: 'P3', condition: 'treated' }, { subject: 'P4', condition: 'treated' }]);
  const d = makeDesign(p, { unit: 'subject', group: 'condition' });
  assert.equal(d.ok, true, d.problems.join(' '));
  assert.equal(d.rows.length, 4);
  assert.deepEqual(d.rows[0].samples, ['s-d-1', 's-d-2']);
  assert.match(d.notes.join(' '), /Technical replicates combined: P1 in control \(S01, S02\)/);
  assert.equal(d.samples[1].row, 0);
});

test('partly paired: subjects in one group only are said to add nothing', () => {
  const p = cohort([{ subject: 'A', condition: 'pre' }, { subject: 'A', condition: 'post' }, { subject: 'B', condition: 'pre' }, { subject: 'B', condition: 'post' }, { subject: 'C', condition: 'pre' }, { subject: 'D', condition: 'post' }]);
  const d = makeDesign(p, { unit: 'subject', group: 'condition', reference: 'pre' });
  assert.equal(d.paired, true);
  assert.match(d.notes.join(' '), /2 subjects are in one condition only/);
});

test('refused, saying why: a group of one unit, too few degrees of freedom', () => {
  const p = cohort([{ subject: 'P1', condition: 'control' }, { subject: 'P2', condition: 'control' }, { subject: 'P3', condition: 'treated' }]);
  const d = makeDesign(p, { unit: 'subject', group: 'condition' });
  assert.equal(d.ok, false);
  assert.match(d.problems.join(' '), /treated has 1 subject: a group needs at least two/);
  const q = cohort([0, 1, 2, 3].map((k) => ({ subject: `P${k}`, condition: k < 2 ? 'c' : 't', age: String(40 + 7 * k * k), weight: String(60 + k * 3 + (k % 2)) })), { numbers: ['age', 'weight'] });
  const e = makeDesign(q, { unit: 'subject', group: 'condition', covariates: ['age', 'weight'] });
  assert.match(e.problems.join(' '), /No degrees of freedom are left: 4 rows .* and 4 coefficients/);
});

test('refused: a covariate confounded with the groups, a number determined by them', () => {
  const rows = (k) => ({ subject: `P${k}`, condition: k < 3 ? 'control' : 'treated', batch: k < 3 ? 'B1' : 'B2', dose: k < 3 ? '0' : '5' });
  const p = cohort([0, 1, 2, 3, 4, 5].map(rows), { numbers: ['dose'] });
  assert.match(makeDesign(p, { unit: 'subject', group: 'condition', covariates: ['batch'] }).problems.join(' '), /batch is confounded with condition: batch\[T\.B2\] is a combination of the columns before/);
  assert.match(makeDesign(p, { unit: 'subject', group: 'condition', covariates: ['dose'] }).problems.join(' '), /dose is determined by condition/);
});

test('refused: the groups confounded with a slide; a slide not holding every group noted', () => {
  const p = cohort([0, 1, 2, 3, 4, 5].map((k) => ({ subject: `P${k}`, condition: k < 3 ? 'control' : 'treated', slide: k < 3 ? 'slide A' : 'slide B' })));
  const d = makeDesign(p, { unit: 'subject', group: 'condition' });
  assert.equal(d.ok, false);
  assert.match(d.problems.join(' '), /condition is confounded with the slide: slide A holds only control; slide B holds only treated/);
  const q = cohort([0, 1, 2, 3, 4, 5].map((k) => ({ subject: `P${k}`, condition: k < 3 ? 'control' : 'treated', slide: k < 4 ? 'slide A' : 'slide B' })));
  const e = makeDesign(q, { unit: 'subject', group: 'condition' });
  assert.equal(e.ok, true, e.problems.join(' '));
  assert.match(e.notes.join(' '), /Not every slide holds every condition: slide A: 3 control, 1 treated; slide B: 0 control, 2 treated/);
  // Adjusting for it: estimable.
  assert.equal(makeDesign(q, { unit: 'subject', group: 'condition', covariates: ['slide'] }).ok, true);
  assert.deepEqual(makeDesign(q, { unit: 'subject', group: 'condition', covariates: ['slide'] }).names, ['Intercept', 'condition[T.treated]', 'slide[T.slide B]']);
});

test('refused: regions of one section as separate units; with one unit, its samples combined', () => {
  let p = cohort([{ subject: 'P1', condition: 'control' }, { subject: 'P2', condition: 'control' }, { subject: 'P3', condition: 'treated' }, { subject: 'P4', condition: 'treated' }]);
  p = addRegion(p, { dataset: 'd-4', name: 'Core 1', geometry: rectangle(0, 0, 10, 10), tool: 'rectangle', now: NOW, id: 'r-1' });
  p = addRegion(p, { dataset: 'd-4', name: 'Core 2', geometry: rectangle(20, 0, 30, 10), tool: 'rectangle', now: NOW, id: 'r-2' });
  p = setRegionSamples(p, 'd-4', ['r-1', 'r-2'], NOW);
  p = setSampleField(p, 's-r-1', 'subject', 'P4', NOW);
  p = setSampleField(p, 's-r-1', 'condition', 'treated', NOW);
  p = setSampleField(p, 's-r-2', 'subject', 'P5', NOW);
  p = setSampleField(p, 's-r-2', 'condition', 'treated', NOW);
  const d = makeDesign(p, { unit: 'subject', group: 'condition' });
  assert.match(d.problems.join(' '), /Core 1 and Core 2 are regions of one section \(S04\) given different subjects/);
  // A teaching example's pseudo-samples: kept, said first, and in the design's words.
  const t = makeDesign(p, { unit: 'subject', group: 'condition', pseudoSamples: true });
  assert.equal(t.ok, true, t.problems.join(' '));
  assert.equal(t.pseudoSamples, true);
  assert.match(t.notes[0], /^Pseudo-samples, for teaching: Core 1 and Core 2 are regions of one section \(S04\) given different subjects/);
  assert.match(t.words.join(' '), /Its subjects are pseudo-samples: regions of one section, compared for teaching/);
  assert.deepEqual(normalizeSpec({ unit: 'subject' }).pseudoSamples, undefined);
  p = setSampleField(p, 's-r-2', 'subject', 'P4', NOW);
  const e = makeDesign(p, { unit: 'subject', group: 'condition' });
  assert.equal(e.ok, true, e.problems.join(' '));
  assert.match(e.notes.join(' '), /P4 in treated \(Core 1, Core 2\)/);
});

test('samples without a value are left out and said; fields missing or misused refused', () => {
  const p = cohort([{ subject: 'P1', condition: 'control' }, { subject: 'P2', condition: 'control' }, { subject: 'P3', condition: 'treated' }, { subject: 'P4', condition: 'treated' }, { subject: 'P5' }]);
  const d = makeDesign(p, { unit: 'subject', group: 'condition', exclude: { 's-d-1': 'a fold across the section' } });
  assert.deepEqual(d.excluded, [{ id: 's-d-1', name: 'S01', reason: 'a fold across the section' }, { id: 's-d-5', name: 'S05', reason: 'no condition' }]);
  assert.match(d.notes.join(' '), /1 sample without a value the design reads is left out: S05 \(no condition\)/);
  assert.match(d.problems.join(' '), /control has 1 subject/);
  assert.match(makeDesign(p, {}).problems.join(' '), /Choose the replicate unit/);
  assert.match(makeDesign(p, { unit: 'subject', group: 'subject' }).problems.join(' '), /different fields/);
  assert.match(makeDesign(p, { unit: 'subject', group: 'condition', covariates: ['condition'] }).problems.join(' '), /already in the design/);
  assert.match(makeDesign(p, { unit: 'subject', group: 'condition', covariates: ['age'] }).problems.join(' '), /no field age/);
  assert.match(makeDesign(p, { unit: 'subject', group: 'condition', reference: 'sham' }).problems.join(' '), /No sample is in sham/);
  const q = cohort([{ subject: 'P1', condition: 'c', age: '1' }, { subject: 'P1', condition: 'c', age: '2' }, { subject: 'P2', condition: 'c', age: '3' }, { subject: 'P3', condition: 't', age: '4' }, { subject: 'P4', condition: 't', age: '5' }], { numbers: ['age'] });
  assert.match(makeDesign(q, { unit: 'subject', group: 'condition', covariates: ['age'] }).problems.join(' '), /age differs between P1's samples in c \(1 and 2\)/);
});

test('the fields a design reads: the user\'s, then run, slide and panel', () => {
  assert.deepEqual(designFields(unpaired()).map((f) => `${f.name}:${f.kind}:${f.bundle}`), ['subject:text:false', 'condition:text:false', 'run:text:true', 'slide:text:true', 'panel:text:true']);
});

test('designs are saved as versions, and say when the samples have changed', () => {
  let p = unpaired();
  const d = makeDesign(p, { unit: 'subject', group: 'condition' });
  let r1;
  ({ project: p, record: r1 } = addDesign(p, d, { name: 'Main', now: NOW, id: 'des-1' }));
  assert.equal(r1.version, 1);
  assert.equal(r1.samples.length, 8);
  assert.deepEqual(r1.result.names, ['Intercept', 'condition[T.treated]']);
  assert.deepEqual(designChanges(p, r1), []);
  p = setSampleField(p, 's-d-8', 'condition', 'control', NOW);
  assert.deepEqual(designChanges(p, r1), ["S08's values changed"]);
  let r2;
  ({ project: p, record: r2 } = addDesign(p, makeDesign(p, { unit: 'subject', group: 'condition' }), { name: 'Main', now: NOW, id: 'des-2' }));
  assert.equal(r2.version, 2);
  assert.equal(r2.parent, 'des-1');
  assert.deepEqual(designVersions(p, 'Main').map((x) => x.id), ['des-1', 'des-2']);
  assert.deepEqual(designsOf(p).map((x) => x.id), ['des-2']);
  assert.throws(() => addDesign(p, makeDesign(p, {}), { name: 'Bad' }), /cannot be saved: Choose the replicate unit/);
});

test('the rank of columns, and a matrix\'s columns by term', () => {
  assert.deepEqual(columnRank([[1, 0, 1], [1, 1, 2], [1, 0, 1]]), { rank: 2, independent: [true, true, false] });
  assert.deepEqual(columnRank([[1, 2], [2, 4.001]]).rank, 2);
  assert.deepEqual(columnRank([[1, 2], [2, 4.000000001]]).rank, 1);
  assert.deepEqual(columnRank([[0, 1], [0, 1]]), { rank: 1, independent: [false, true] });
  const m = designMatrix([{ g: 'b', x: 2 }, { g: 'a', x: 3 }, { g: 'c', x: 5 }], [{ name: 'Intercept', kind: 'intercept' }, { name: 'g', kind: 'factor', reference: 'b' }, { name: 'x', kind: 'number' }]);
  assert.deepEqual(m.names, ['Intercept', 'g[T.a]', 'g[T.c]', 'x']);
  assert.deepEqual(m.columns, [[0], [1, 2], [3]]);
  assert.deepEqual(m.matrix, [[1, 0, 0, 2], [1, 1, 0, 3], [1, 0, 1, 5]]);
});
