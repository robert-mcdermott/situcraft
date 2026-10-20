import assert from 'node:assert/strict';
import test from 'node:test';
import { SUMMARY_MANIFEST, at, project } from './record.fixture.mjs';
import { gatherManifests, inputsOf, itemOf, parameterText, recordBibTeX, recordJSON, recordMarkdown, recordableResults, resultRecord } from './record.js';

test('a result\'s steps from its bundle, each after what it was made from, with its method, seed and references', () => {
  const p = project();
  const r = resultRecord(p, 'spa-1', { program: { version: '0.8.0' } });
  assert.equal(r.format, 'situcraft-record');
  assert.deepEqual(r.steps.map((s) => s.id), ['d-1', 'seg-1', 'clu-1', 'ms-1', 'lab-1', 'gra-1', 'spa-1']);
  assert.deepEqual(r.steps.map((s) => s.n), [1, 2, 3, 4, 5, 6, 7]);
  const step = (id) => r.steps.find((s) => s.id === id);
  assert.deepEqual(step('spa-1').inputs, [1, 2, 5, 6]);
  assert.deepEqual(step('lab-1').inputs, [1, 2, 3, 4]);
  assert.deepEqual(step('spa-1').seeds, [7]);
  assert.deepEqual(step('clu-1').method, { name: 'SituCraft clustering', version: '1' });
  assert.deepEqual(step('clu-1').parameters, { minTranscripts: 10, components: 50, resolution: 1, neighbors: 15, seed: 20250 });
  assert.deepEqual(step('seg-1').decisions.map((d) => d.rationale), ['The only one.']);
  assert.equal(step('seg-1').dataset, 'S01');
  // References numbered as first cited: Xenium's with the bundle, then SituCraft with the first
  // step it ran; a marker set's atlases with it; what the registry does not hold, as written.
  const ref = (n) => r.references[n - 1];
  assert.deepEqual(step('d-1').references, [1]);
  assert.equal(ref(1).key, 'janesick2023');
  assert.deepEqual(step('seg-1').references, [1, 2]);
  assert.equal(ref(2).key, 'situcraft');
  assert.match(ref(2).text, /SituCraft, version 0\.8\.0/);
  assert.deepEqual(step('ms-1').references.map((n) => ref(n).key ?? ref(n).doi), ['travaglini2020', '10.9999/atlas']);
  // A label set cites its marker set's atlases.
  assert.deepEqual(step('lab-1').references.map((n) => ref(n).key ?? ref(n).doi), ['situcraft', 'wolf2018', 'travaglini2020', '10.9999/atlas', 'tirosh2016']);
  const unknown = r.references.find((x) => x.text.startsWith('Roe R.'));
  assert.equal(unknown.key, null);
  assert.ok(step('spa-1').references.includes(unknown.n));
  assert.deepEqual(r.software, [{ name: 'Xenium Onboard Analysis', version: 'xenium-2.0.0.10' }, { name: 'situcraft', version: '0.8.0' }]);
  // The same project, the same record; a result it does not hold, refused.
  assert.deepEqual(resultRecord(p, 'spa-1', { program: { version: '0.8.0' } }), r);
  assert.deepEqual(JSON.parse(recordJSON(r)), r);
  assert.throws(() => resultRecord(p, 'spa-none'), /no result spa-none/);
});

test('a cohort\'s result: its samples\' steps, found in the manifests the program keeps', async () => {
  const p = project();
  // From the project alone: the summary's samples (their bundles) and marker set, the design.
  const bare = resultRecord(p, 'exp-1');
  assert.deepEqual(bare.steps.map((s) => s.id), ['d-1', 'd-2', 'ms-1', 'des-1', 'coh-1', 'exp-1']);
  // With the manifests: each sample's segmentation, clustering, labels, graph and analysis.
  const asked = [];
  const manifests = await gatherManifests(p, 'exp-1', async (id) => {
    asked.push(id);
    return id === 'coh-1' ? SUMMARY_MANIFEST : { id, lineage: { parents: [] } };
  });
  assert.ok(asked.includes('coh-1') && asked.includes('lab-2') && asked.includes('spa-1'), asked.join(' '));
  assert.equal(new Set(asked).size, asked.length, 'each manifest asked for once');
  assert.ok(!asked.includes('d-1') && !asked.includes('ms-1') && !asked.includes('des-1'), 'the page\'s records have no manifest');
  const r = resultRecord(p, 'exp-1', { manifests });
  assert.deepEqual(r.steps.map((s) => s.id), ['d-1', 'd-2', 'seg-1', 'seg-2', 'clu-1', 'clu-2', 'ms-1', 'lab-1', 'lab-2', 'gra-1', 'gra-2', 'spa-1', 'des-1', 'coh-1', 'exp-1']);
  assert.deepEqual(r.steps.find((s) => s.id === 'coh-1').inputs, [1, 2, 3, 4, 7, 8, 9, 10, 11, 12]);
  assert.deepEqual(r.steps.find((s) => s.id === 'coh-1').seeds, [20250]);
  // A step's inputs are what the project holds: a parent it no longer has is left out.
  assert.deepEqual(inputsOf(p, itemOf(p, 'gra-1'), { lineage: { parents: [{ kind: 'segmentation', id: 'seg-gone' }] } }), ['d-1', 'seg-1']);
  // The same steps of each sample are written once, naming each sample.
  const md = recordMarkdown(r);
  assert.match(md, /^# What made Expression compared: treated − control\n/);
  assert.match(md, /\n1–2\. \*\*Xenium output bundles, one for each of 2:\*\* S01 \(`d-1`\); S02 \(`d-2`\)\. Method: Xenium Onboard Analysis, version 2\.0\.0\./);
  assert.match(md, /\n3–4\. \*\*Segmentations, one for each of 2:\*\* Instrument segmentation, of S01 \(`seg-1`\), S02 \(`seg-2`\)\./);
  assert.match(md, /Each: chosen as the dataset's segmentation: "The only one\." \(SituCraft, 2026-10-19\)\./);
  assert.match(md, /\n5–6\. \*\*Clusterings, one for each of 2:\*\*.*Parameters: minTranscripts = 10; components = 50; resolution = 1; neighbors = 15\. Seed: 20250\./);
  assert.match(md, /\n15\. \*\*Expression compared: treated − control\*\* \(expression comparison `exp-1`, 2026-10-19\)\. Method: SituCraft differential expression, version 1\. From steps 13, 14\. Parameters: level = 0\.95; minCells = 10; design\.name = By condition; design\.version = 1; design\.contrasts = treated − control\./);
  assert.match(md, /\n## References\n\n1\. Janesick A, Shelansky R, Gottscho AD, et al\. High resolution/);
  // BibTeX: an entry for each reference, under the registry's key.
  const bib = recordBibTeX(r);
  const keys = [...bib.matchAll(/^@(\w+)\{([^,]+),$/gm)].map((m) => m[2]);
  assert.equal(keys.length, r.references.length);
  assert.deepEqual(keys.slice(0, 3), ['janesick2023', 'situcraft', 'wolf2018']);
  assert.ok(keys.includes('ref' + r.references.find((x) => x.doi === '10.9999/atlas').n));
  assert.equal((bib.match(/\{/g) ?? []).length, (bib.match(/\}/g) ?? []).length);
});

test('the results a record is made of; parameters in words', () => {
  const p = project();
  p.figures = [{ id: 'fig-1', name: 'Figure 1', size: 'double', width: 493, height: 300, panels: [], created: at(9) }];
  const results = recordableResults(p);
  assert.deepEqual(results.slice(0, 3).map((x) => x.id), ['fig-1', 'exp-1', 'coh-1']);
  assert.ok(!results.some((x) => ['d-1', 'ms-1', 'des-1'].includes(x.id)));
  const fig = resultRecord(p, 'fig-1', { program: { version: '0.8.0' } });
  assert.deepEqual(fig.steps.map((s) => s.kind), ['figure']);
  assert.equal(fig.references[0].key, 'situcraft');
  assert.equal(parameterText({ a: 1, b: { c: 'x', colors: ['#fff'] }, list: [1, 2], seed: 3, sum: 'f'.repeat(64), long: Array.from({ length: 20 }, (_, i) => i), items: [{ x: 1 }], named: [{ name: 'A' }, { name: 'B' }], none: null, w: 0.123456789 }),
    'a = 1; b.c = x; list = 1, 2; sum = ffffffffffff…; long = 20 values; items = 1 items; named = A, B; w = 0.123457');
});
