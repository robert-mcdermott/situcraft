// SituCraft's design matrices against patsy's for the same formulas
// (validation/reference/designs/: compare.sh writes patsy.json from cases.json).
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { createProject } from './project.js';
import { addSampleField, setSampleField } from './samples.js';
import { makeDesign } from './design.js';

const here = new URL('../../validation/reference/designs/', import.meta.url);
const cases = JSON.parse(readFileSync(new URL('cases.json', here), 'utf8'));
const reference = JSON.parse(readFileSync(new URL('patsy.json', here), 'utf8'));

// A project of a case's rows, a section each, its fields as the rows give them.
function projectOf(c) {
  let p = { ...createProject('Reference', { now: 'x' }), datasets: c.rows.map((_, k) => ({ id: `d-${k}`, name: `R${k}`, path: `/r/${k}` })) };
  const fields = [...new Set(c.rows.flatMap((r) => Object.keys(r)))].filter((f) => !['subject', 'condition'].includes(f));
  for (const f of fields) p = addSampleField(p, f, (c.numbers ?? []).includes(f) ? 'number' : 'text', 'x');
  c.rows.forEach((r, k) => {
    for (const [f, v] of Object.entries(r)) p = setSampleField(p, `s-d-${k}`, f, v, 'x');
  });
  return p;
}

// patsy's names in SituCraft's: C(field, Treatment(reference='…'))[T.level] and C(field)[T.level]
// are field[T.level].
const plain = (name) => name.replace(/^C\(([^,)]+)(?:, Treatment\(reference='[^']*'\))?\)/, '$1');

test(`every case's matrix is patsy ${reference.patsy}'s for the same formula`, () => {
  assert.equal(reference.cases.length, cases.length);
  for (const c of cases) {
    const want = reference.cases.find((r) => r.name === c.name);
    const d = makeDesign(projectOf(c), c.spec);
    assert.equal(d.ok, true, `${c.name}: ${d.problems.join(' ')}`);
    assert.deepEqual(d.names, want.names.map(plain), `${c.name}: ${want.formula}`);
    assert.equal(d.matrix.length, want.matrix.length, c.name);
    d.matrix.forEach((row, i) => row.forEach((v, j) => assert.ok(Math.abs(v - want.matrix[i][j]) < 1e-12, `${c.name}: row ${i}, ${d.names[j]}: ${v} against ${want.matrix[i][j]}`)));
  }
});
