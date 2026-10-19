import assert from 'node:assert/strict';
import test from 'node:test';
import { createProject, parseProject, serializeProject } from './project.js';
import { addRegion, editRegion, rectangle } from './regions.js';
import { BUNDLE_FIELDS, DEFAULT_FIELDS, addSampleField, fieldOf, fieldValue, parseTable, readSamplesTable, removeSampleField, renameSample, renameSampleField, sampleFields, samplesCSV, samplesOf, setFieldKind, setRegionSamples, setSampleField } from './samples.js';

const NOW = '2026-10-19T12:00:00.000Z';

const dataset = (id, name, extra = {}) => ({ id, name, path: `/data/${name}`, run: 'run', slide: 'slide-1', region: 'region', panel: 'Xenium Human Lung Gene Expression', panelDesign: 'hLung_v1.1', software: 'xenium-2.0.0.10', version: '2.0.0', cells: 100, ...extra });

function project() {
  return { ...createProject('Cohort', { now: NOW }), datasets: [dataset('d-1', 'lung'), dataset('d-2', 'breast', { slide: 'slide-2', panelDesign: 'hBreast_v1' }), dataset('d-3', 'tma')] };
}

test('each dataset is a sample of its own until something is said of it, and the document is unchanged', () => {
  const p = project();
  const s = samplesOf(p);
  assert.deepEqual(s.map((x) => [x.id, x.name, x.region, x.record]), [['s-d-1', 'lung', null, null], ['s-d-2', 'breast', null, null], ['s-d-3', 'tma', null, null]]);
  assert.deepEqual(sampleFields(p), DEFAULT_FIELDS);
  assert.equal(p.samples, undefined);
  assert.equal(fieldValue(s[1], 'slide'), 'slide-2');
  assert.equal(fieldValue(s[1], 'panel'), 'hBreast_v1');
  assert.equal(fieldValue(s[1], 'subject'), '');
  assert.deepEqual(BUNDLE_FIELDS.map((f) => f.key), ['run', 'slide', 'xenium_region', 'panel', 'software']);
});

test('fields are set and cleared, a sample record made only when needed, and the history says so', () => {
  let p = setSampleField(project(), 's-d-1', 'subject', ' P1 ', NOW);
  p = setSampleField(p, 's-d-1', 'condition', 'tumor', NOW);
  assert.deepEqual(p.samples, [{ id: 's-d-1', dataset: 'd-1', region: null, fields: { subject: 'P1', condition: 'tumor' } }]);
  p = setSampleField(p, 's-d-1', 'subject', '', NOW);
  assert.deepEqual(p.samples[0].fields, { condition: 'tumor' });
  assert.match(p.history.at(-1).detail, /lung: subject cleared/);
  assert.throws(() => setSampleField(p, 's-d-1', 'age', 3, NOW), /no field age/);
  assert.throws(() => setSampleField(p, 's-x', 'subject', 'P', NOW), /no such sample/);
});

test('number fields keep finite numbers only; missing is not zero', () => {
  assert.equal(fieldOf('number', ' 42 '), 42);
  assert.equal(fieldOf('number', '-1.5e3'), -1500);
  assert.equal(fieldOf('number', '.5'), 0.5);
  assert.equal(fieldOf('number', ''), null);
  assert.equal(fieldOf('number', 0), 0);
  for (const bad of ['12 years', '0x10', 'NaN', 'Infinity', '1,5']) assert.throws(() => fieldOf('number', bad), /not a number/);
  let p = addSampleField(project(), 'age', 'number', NOW);
  p = setSampleField(p, 's-d-2', 'age', '61', NOW);
  assert.equal(samplesOf(p)[1].fields.age, 61);
  assert.throws(() => setSampleField(p, 's-d-2', 'age', 'sixty', NOW), /not a number/);
});

test('fields are added, renamed (in every sample), retyped only when every value fits, and removed', () => {
  let p = addSampleField(project(), 'batch', 'text', NOW);
  assert.deepEqual(sampleFields(p).map((f) => f.name), ['subject', 'condition', 'batch']);
  assert.throws(() => addSampleField(p, 'Batch', 'text', NOW), /already/);
  assert.throws(() => addSampleField(p, 'slide', 'text', NOW), /column of the table/);
  assert.throws(() => addSampleField(p, ' ', 'text', NOW), /needs a name/);
  assert.throws(() => addSampleField(p, 'x', 'date', NOW), /text or a number/);
  p = setSampleField(p, 's-d-1', 'batch', '7', NOW);
  p = setSampleField(p, 's-d-2', 'batch', '8', NOW);
  p = renameSampleField(p, 'batch', 'staining batch', NOW);
  assert.deepEqual(samplesOf(p).map((s) => s.fields['staining batch'] ?? null), ['7', '8', null]);
  p = setFieldKind(p, 'staining batch', 'number', NOW);
  assert.equal(samplesOf(p)[0].fields['staining batch'], 7);
  p = setFieldKind(p, 'staining batch', 'text', NOW);
  assert.equal(samplesOf(p)[0].fields['staining batch'], '7');
  p = setSampleField(p, 's-d-3', 'subject', 'P3', NOW);
  assert.throws(() => setFieldKind(p, 'subject', 'number', NOW), /subject is not a number in tma/);
  p = removeSampleField(p, 'staining batch', NOW);
  assert.ok(samplesOf(p).every((s) => !('staining batch' in s.fields)));
  assert.match(p.history.at(-1).detail, /with 2 values/);
});

test('samples are named uniquely; an empty name gives back the dataset\'s', () => {
  let p = renameSample(project(), 's-d-1', 'Patient 1 tumor', NOW);
  assert.equal(samplesOf(p)[0].name, 'Patient 1 tumor');
  assert.throws(() => renameSample(p, 's-d-2', 'Patient 1 tumor', NOW), /Another sample/);
  p = renameSample(p, 's-d-1', '', NOW);
  assert.equal(samplesOf(p)[0].name, 'lung');
  // Datasets of one name: the later numbered.
  const twin = { ...project(), datasets: [dataset('d-1', 'outs'), dataset('d-2', 'outs')] };
  assert.deepEqual(samplesOf(twin).map((s) => s.name), ['outs', 'outs (2)']);
});

test('a dataset\'s regions become its samples, in the order drawn; removed regions are flagged; the dataset a sample again', () => {
  let p = project();
  p = addRegion(p, { dataset: 'd-3', name: 'Core A1', cls: 'Region*', geometry: rectangle(0, 0, 10, 10), tool: 'rectangle', now: NOW, id: 'r-a1' });
  p = addRegion(p, { dataset: 'd-3', name: 'Core A2', cls: 'Region*', geometry: rectangle(20, 0, 30, 10), tool: 'rectangle', now: NOW, id: 'r-a2' });
  p = addRegion(p, { dataset: 'd-3', name: 'Fold', cls: 'Ignore*', geometry: rectangle(40, 0, 50, 10), tool: 'rectangle', now: NOW, id: 'r-f' });
  p = setSampleField(p, 's-d-3', 'condition', 'slide-level', NOW);
  p = setRegionSamples(p, 'd-3', ['r-a2', 'r-a1'], NOW);
  let s = samplesOf(p);
  assert.deepEqual(s.map((x) => x.name), ['lung', 'breast', 'Core A1', 'Core A2']);
  assert.deepEqual(s.slice(2).map((x) => [x.id, x.region.id, x.removed]), [['s-r-a1', 'r-a1', false], ['s-r-a2', 'r-a2', false]]);
  p = setSampleField(p, 's-r-a1', 'subject', 'P7', NOW);
  // A region renamed: the sample follows it, unless named.
  p = editRegion(p, 'r-a2', { name: 'Core A2 (tumor)' }, { now: NOW });
  assert.equal(samplesOf(p)[3].name, 'Core A2 (tumor)');
  p = editRegion(p, 'r-a2', { removed: true }, { now: NOW });
  assert.equal(samplesOf(p)[3].removed, true);
  // Choosing again keeps a region sample's fields, drops those no longer chosen.
  p = setRegionSamples(p, 'd-3', ['r-a1'], NOW);
  s = samplesOf(p);
  assert.deepEqual(s.map((x) => x.name), ['lung', 'breast', 'Core A1']);
  assert.equal(s[2].fields.subject, 'P7');
  assert.match(p.history.at(-1).detail, /1 region sample and its fields dropped/);
  assert.throws(() => setRegionSamples(p, 'd-3', ['r-zz'], NOW), /no region r-zz/);
  // The dataset a sample again, with the fields it had.
  p = setRegionSamples(p, 'd-3', [], NOW);
  s = samplesOf(p);
  assert.equal(s[2].name, 'tma');
  assert.equal(s[2].fields.condition, 'slide-level');
});

test('the table is written as CSV and read back exactly', () => {
  let p = addSampleField(project(), 'age', 'number', NOW);
  p = addSampleField(p, 'notes', 'text', NOW);
  p = setSampleField(p, 's-d-1', 'subject', 'P1', NOW);
  p = setSampleField(p, 's-d-1', 'age', 0.1 + 0.2, NOW);
  p = setSampleField(p, 's-d-2', 'notes', 'said "fine", then\nleft', NOW);
  p = setSampleField(p, 's-d-3', 'age', 0, NOW);
  const csv = samplesCSV(p);
  assert.equal(csv.split('\n')[0], 'sample,dataset,region,subject,condition,age,notes,run,slide,xenium_region,panel,software');
  assert.match(csv, /"said ""fine"", then\nleft"/);
  // Read into a project whose samples know nothing yet.
  let q = addSampleField(project(), 'age', 'number', NOW);
  q = addSampleField(q, 'notes', 'text', NOW);
  const read = readSamplesTable(q, csv, NOW);
  assert.deepEqual(read.matched, ['lung', 'breast', 'tma']);
  assert.deepEqual(read.ignored, ['dataset', 'region', 'run', 'slide', 'xenium_region', 'panel', 'software']);
  assert.deepEqual(samplesOf(read.project).map((s) => s.fields), samplesOf(p).map((s) => s.fields));
  assert.equal(samplesOf(read.project)[0].fields.age, 0.30000000000000004);
  assert.equal(samplesCSV(read.project), csv);
});

test('reading a table matches by name, adds new fields, and says what did not match', () => {
  const text = '﻿Sample\tSubject\tTreatment\tDose\n LUNG \tP1\tdrug\t2.5\nbreast\tP2\tplacebo\t\nspleen\tP9\tdrug\t1\n';
  const read = readSamplesTable(project(), text, NOW);
  assert.deepEqual(read.matched, ['lung', 'breast']);
  assert.deepEqual(read.unmatched, ['spleen']);
  assert.deepEqual(read.untouched, ['tma']);
  assert.deepEqual(read.added, [{ name: 'Treatment', kind: 'text' }, { name: 'Dose', kind: 'number' }]);
  const s = samplesOf(read.project);
  assert.deepEqual(s[0].fields, { subject: 'P1', Treatment: 'drug', Dose: 2.5 });
  assert.deepEqual(s[1].fields, { subject: 'P2', Treatment: 'placebo' });
  // An empty cell clears a value.
  const cleared = readSamplesTable(read.project, 'sample,subject\nlung,\n', NOW);
  assert.equal(samplesOf(cleared.project)[0].fields.subject, undefined);
});

test('a table is refused, saying why, and nothing is set', () => {
  const p = project();
  assert.throws(() => readSamplesTable(p, 'subject\nP1\n', NOW), /No column names the samples/);
  assert.throws(() => readSamplesTable(p, 'sample,subject,Subject\nlung,P1,P2\n', NOW), /in the table twice/);
  assert.throws(() => readSamplesTable(p, 'sample,subject\nlung,P1\nLung,P2\n', NOW), /Two rows name the sample lung/);
  let q = addSampleField(p, 'age', 'number', NOW);
  q = setSampleField(q, 's-d-1', 'age', 50, NOW);
  assert.throws(() => readSamplesTable(q, 'sample,age\nbreast,60\nlung,old\n', NOW), /lung, age: "old" is not a number/);
  assert.equal(samplesOf(q)[1].fields.age, undefined);
  assert.throws(() => readSamplesTable(p, 'sample,,x\nlung,1,2\n', NOW), /Column 2 has values but no name/);
  assert.throws(() => readSamplesTable(p, 'sample,subject\n"lung,P1\n', NOW), /inside a quoted field/);
  assert.throws(() => readSamplesTable(p, '', NOW), /empty/);
});

test('parseTable reads quotes, line breaks within them, CRLF and tabs', () => {
  assert.deepEqual(parseTable('a,b\r\n"x, ""y""","1\n2"\r\n'), [['a', 'b'], ['x, "y"', '1\n2']]);
  assert.deepEqual(parseTable('a\tb\n1\t2'), [['a', 'b'], ['1', '2']]);
  assert.deepEqual(parseTable('a,b\n\n1,2\n'), [['a', 'b'], ['1', '2']]);
});

test('samples and their fields are saved with the project and read back', () => {
  let p = addSampleField(project(), 'age', 'number', NOW);
  p = setSampleField(p, 's-d-2', 'age', 44, NOW);
  const back = parseProject(serializeProject(p));
  assert.deepEqual(back.samples, p.samples);
  assert.deepEqual(back.sampleFields, p.sampleFields);
  assert.throws(() => parseProject(JSON.stringify({ ...p, samples: {} })), /samples is not a list/);
});
