import assert from 'node:assert/strict';
import test from 'node:test';
import { project } from './record.fixture.mjs';
import { absolutePaths } from './certificate.js';
import { findLeftOut, leftOutStrings, redact, redactionOf } from './redact.js';

const NOTE = 'The slide was cracked near its edge';
const REGION_NOTE = 'Drawn after the meeting with Dr Wu';
const OPAQUE = `${'A'.repeat(30)}+S01+${'B'.repeat(30)}`;

// The record's fixture with people, their notes, the samples' subjects and this computer's paths.
function planted() {
  const p = project();
  p.datasets[0].path = '/Users/someone/data/S01';
  p.datasets[1].path = '/Users/someone/data/S02';
  p.samples = [
    { id: 's-d-1', dataset: 'd-1', region: null, fields: { subject: 'P-17', condition: 'control' } },
    { id: 's-d-2', dataset: 'd-2', region: null, fields: { subject: 'P-18', condition: 'treated' } },
  ];
  p.reviews = [
    { id: 'rev-1', dataset: 'd-1', action: 'comment', by: 'Ada Lovelace', note: NOTE, time: '2026-10-19T00:10:00Z' },
    { id: 'rev-2', dataset: 'd-1', action: 'accept', by: 'Grace Hopper', note: 'Accepted on review', time: '2026-10-19T00:11:00Z' },
  ];
  p.regions = [{ id: 'reg-1', dataset: 'd-1', versions: [{ name: 'Tumor nest', cls: 'Tumor', by: 'Ada Lovelace', note: REGION_NOTE, time: '2026-10-19T00:12:00Z' }] }];
  p.history.push(
    { time: '2026-10-19T00:12:00Z', action: 'region', detail: `Drew Tumor nest (Tumor) on S01: ${REGION_NOTE}` },
    { time: '2026-10-19T00:13:00Z', action: 'review', detail: 'Reviewed by Ada Lovelace: S01 seen in S01.zip, and xS01.' },
    { time: '2026-10-19T00:14:00Z', action: 'import', detail: 'Read P-17_labels.csv from /Users/someone/data/S01/outs' },
  );
  p.artifacts.find((a) => a.id === 'gra-1').keep = OPAQUE;
  return p;
}

test('people and their notes left out: each person a stand-in, wherever named; each note, wherever written; SituCraft\'s own kept', () => {
  const p = planted();
  const r = redactionOf(p, { people: ['Ada Lovelace', 'Alan Turing'] });
  assert.deepEqual([...r.people], [['Ada Lovelace', 'Person 1'], ['Grace Hopper', 'Person 2'], ['Alan Turing', 'Person 3']]);
  assert.deepEqual(r.notes.sort(), [REGION_NOTE, NOTE].sort());
  assert.deepEqual(r.counts, { people: 3, notes: 2, names: 0 });
  const out = redact(p, r);
  assert.equal(out.reviews[0].by, 'Person 1');
  assert.equal(out.reviews[0].note, '');
  assert.equal(out.reviews[1].note, 'Accepted on review');
  assert.equal(out.regions[0].versions[0].by, 'Person 1');
  assert.equal(out.regions[0].versions[0].note, '');
  assert.equal(out.history.at(-3).detail, 'Drew Tumor nest (Tumor) on S01: (a note, left out)');
  assert.match(out.history.at(-2).detail, /^Reviewed by Person 1:/);
  // This computer's paths stood in for, as the certificate stands them in.
  assert.equal(out.datasets[0].path, 'situcraft:bundle/d-1');
  assert.equal(out.history.at(-1).detail, 'Read P-17_labels.csv from situcraft:bundle/d-1/outs');
  assert.equal(out.decisions[0].by, 'SituCraft');
  const text = JSON.stringify(out);
  for (const gone of ['Ada', 'Lovelace', 'Grace Hopper', 'cracked', 'Dr Wu']) assert.ok(!text.includes(gone), gone);
  // The names are kept unless asked; the project given is left alone.
  assert.equal(out.datasets[0].name, 'S01');
  assert.equal(p.reviews[0].by, 'Ada Lovelace');
});

test('the samples\' and subjects\' names left out when asked: each sample, subject and its dataset a stand-in wherever named as a word; base64 untouched', () => {
  const p = planted();
  const r = redactionOf(p, { names: true });
  assert.deepEqual([...r.names], [['S01', 'Sample 1'], ['S02', 'Sample 2'], ['P-17', 'Subject 1'], ['P-18', 'Subject 2']]);
  const out = redact(p, r);
  assert.deepEqual(out.datasets.map((d) => d.name), ['Sample 1', 'Sample 2']);
  assert.deepEqual(out.samples.map((s) => s.fields), [{ subject: 'Subject 1', condition: 'control' }, { subject: 'Subject 2', condition: 'treated' }]);
  // As a word only: not within another word, an identifier or a file's extension; but between a
  // file name's underscores. A bundle's folder named as its dataset is stood in for, not renamed.
  assert.equal(out.history.at(-2).detail, 'Reviewed by Person 1: Sample 1 seen in S01.zip, and xS01.');
  assert.equal(out.history.at(-1).detail, 'Read Subject 1_labels.csv from situcraft:bundle/d-1/outs');
  assert.equal(out.datasets[0].path, 'situcraft:bundle/d-1');
  assert.equal(out.artifacts.find((a) => a.id === 'gra-1').keep, OPAQUE);
  assert.ok(!JSON.stringify(out.design ?? out.artifacts.find((a) => a.kind === 'design')).includes('"S01"'));
});

test('what is left out, searched for in every text: paths and their home folder, people, notes, names; found as written in HTML and JSON', () => {
  const p = planted();
  const r = redactionOf(p, { names: true });
  const strings = leftOutStrings(r, { paths: absolutePaths(p) });
  const of = (what) => strings.filter((s) => s.what === what).map((s) => s.text);
  assert.deepEqual(of('a path'), ['/Users/someone/data/S01', '/Users/someone/data/S02', '/Users/someone/data/S01/outs']);
  assert.deepEqual(of('a home folder'), ['/Users/someone']);
  assert.deepEqual(of('a person\'s name'), ['Ada Lovelace', 'Grace Hopper']);
  assert.equal(of('a note').length, 2);
  assert.deepEqual(of('a sample\'s or subject\'s name'), ['S01', 'S02', 'P-17', 'P-18']);
  const found = findLeftOut([
    { name: 'index.html', text: '<p>Reviewed by Ada Lovelace &amp; others</p>' },
    { name: 'project.json', text: '{"path": "/Users/someone/data/S02/outs"}' },
    { name: 'methods.md', text: 'Sample 1 and Sample 2, of Subject 1; S01.zip and P-170.' },
    { name: 'figure.svg', text: '<text>P-18</text>' },
  ], strings);
  assert.deepEqual(found, [
    { file: 'index.html', what: 'a person\'s name' },
    { file: 'project.json', what: 'a home folder' },
    { file: 'project.json', what: 'a path' },
    { file: 'project.json', what: 'a sample\'s or subject\'s name' },
    { file: 'figure.svg', what: 'a sample\'s or subject\'s name' },
  ]);
  // Everything redacted holds none of it (S01.zip and xS01 are not the name).
  assert.deepEqual(findLeftOut([{ name: 'project.json', text: JSON.stringify(redact(p, r)) }], strings), []);
});
