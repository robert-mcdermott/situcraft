import assert from 'node:assert/strict';
import { readFileSync, writeFileSync } from 'node:fs';
import test from 'node:test';
import { certified } from './certificate.fixture.mjs';
import { writeCertificate } from './certificate.js';
import { checkReportData } from './report-check.js';
import { numberText, reportHTML, reportNumbers, summaryTables } from './report.js';
import { readZip } from './zip.js';
import { sha256 } from './sha256.js';

// A report of the fixture's enrichment and expression comparison and a figure, its certificate
// carried.
const SVG = `<?xml version="1.0" encoding="UTF-8"?>\n<svg xmlns="http://www.w3.org/2000/svg" width="493" height="300"><title>Figure 1 – µm</title></svg>\n`;
const svgBytes = new TextEncoder().encode(SVG);

async function report(edit = (p) => p) {
  const figures = new Map([['fig-1', { drawing: 'd'.repeat(64), panels: [{ id: 'panel-1', letter: 'A', drawing: 'e'.repeat(64) }], svg: sha256(svgBytes) }]]);
  const { methods, built } = certified((p) => {
    p.figures = [{ id: 'fig-1', name: 'Figure 1', size: 'double', width: 493, height: 300, text: 7, panels: [], created: '2026-10-19T00:09:00Z' }];
    return edit(p);
  }, { scope: { kind: 'results', ids: ['fig-1', 'spa-1', 'exp-1'], name: 'Results of the fixture' }, figures });
  const zip = await writeCertificate(built);
  const html = reportHTML({ title: 'Results of the fixture', certificate: built.certificate, zip, methods, figures: [{ id: 'fig-1', name: 'Figure 1', svg: SVG, description: 'Panel A: nothing.' }], results: ['spa-1', 'exp-1'] });
  return { html, zip, built, methods };
}

test('a result\'s tables: its numbers, each with its place in the certificate', () => {
  const tables = summaryTables({ labels: { id: '', name: '', level: 0, count: 0 }, cells: 691, labeled: 682, significant: 2, enriched: [{ a: 'T', b: 'B', ratio: 2.0660598, z: 11.04, padj: 0.0016650016 }], sizes: [270, 153], segmentation: { id: 'seg-1' } }, ['steps', 6, 'summary']);
  assert.deepEqual(tables.map((t) => t.caption), ['Its numbers', 'Enriched', 'Cells a cluster']);
  assert.deepEqual(tables[0].rows.map((r) => [r[0].text, r[1].text, r[1].pointer]), [['Cells', '691', '/steps/6/summary/cells'], ['Labeled', '682', '/steps/6/summary/labeled'], ['Found', '2', '/steps/6/summary/significant']]);
  assert.deepEqual(tables[1].columns, ['Label', 'Neighbor', 'Ratio', 'z', 'Adjusted p']);
  assert.deepEqual(tables[1].rows[0].map((c) => c.text), ['T', 'B', '2.066', '11.04', '0.001665']);
  assert.equal(tables[1].rows[0][2].pointer, '/steps/6/summary/enriched/0/ratio');
  assert.equal(tables[1].rows[0][2].value, 2.0660598);
  assert.deepEqual([numberText(1234567), numberText(0.00001234), numberText(-0.5), numberText(0)], ['1,234,567', '1.23e-5', '-0.5', '0']);
});

test('a report: its figures, tables, methods and references, and its certificate carried; nothing asked of the network', async () => {
  const { html, built, methods } = await report();
  assert.match(html, /^<!doctype html>\n<html lang="en">/);
  assert.match(html, /<title>Results of the fixture<\/title>/);
  assert.match(html, /<meta http-equiv="Content-Security-Policy" content="default-src 'none';/);
  // Nothing fetched: no source or link to another place but the file's own.
  assert.doesNotMatch(html, /\s(?:src|href)="(?:https?:)?\/\//);
  assert.match(html, /<img data-figure="fig-1" alt="Panel A: nothing\." src="data:image\/svg\+xml;base64,/);
  assert.ok(html.includes(Buffer.from(SVG).toString('base64')), 'the SVG as written');
  assert.match(html, /<h3 id="result-spa-1">Neighborhood enrichment<\/h3>/);
  assert.match(html, /<td class="num" data-pointer="\/steps\/\d+\/summary\/significant" data-value="2"/);
  assert.equal((html.match(/class="methods-paragraph"/g) ?? []).length, methods.paragraphs.length);
  assert.match(html, new RegExp(`<code>${built.certificate.fingerprint}</code>`));
  assert.match(html, /<script type="application\/zip" id="situcraft-certificate">[A-Za-z0-9+/=]+<\/script>/);
  assert.match(html, /const checkReportData = async function checkReportData\(input\)/);
  // The same project and choice, the same bytes.
  assert.equal((await report()).html, html);
});

test('the report checks itself: its certificate intact, every number, figure and paragraph the certificate\'s; an edit found', async () => {
  const { html, zip, methods } = await report();
  const numbers = reportNumbers(html);
  assert.ok(numbers.length >= 5, `${numbers.length} numbers`);
  assert.ok(numbers.every((n) => n.text === numberText(n.value)));
  const input = { zip, numbers, figures: [{ id: 'fig-1', svg: svgBytes }], methods: methods.paragraphs.map((p) => p.text) };
  // The check as the report holds it (its own text, nothing imported), not this module's.
  const script = /<script>\n([\s\S]*?)<\/script>/.exec(html)[1];
  assert.doesNotMatch(script, /<\/script|<!--/i);
  const inline = new Function(`${script.split('\n(')[0]}\nreturn checkReportData;`)();
  assert.match(script, /^const numberText = function numberText\(v\)/);
  const ok = await inline(input);
  assert.deepEqual(ok.problems, []);
  assert.equal(ok.intact, true);
  assert.deepEqual([ok.numbers.checked, ok.numbers.same], [numbers.length, numbers.length]);
  assert.deepEqual([ok.figures.checked, ok.figures.same, ok.methods.same], [1, 1, true]);
  // A number edited (its value, or only its text as shown), a figure drawn otherwise, a paragraph
  // changed: each found.
  const edited = await checkReportData({ ...input, numbers: numbers.map((n, i) => (i === 0 ? { ...n, value: n.value + 1 } : i === 1 ? { ...n, text: `${n.text}1` } : n)), figures: [{ id: 'fig-1', svg: new TextEncoder().encode(SVG.replace('µm', 'mm')) }], methods: ['Something else.', ...input.methods.slice(1)] });
  assert.equal(edited.intact, false);
  assert.deepEqual(edited.numbers.differ, [numbers[0].pointer, numbers[1].pointer]);
  assert.deepEqual(edited.figures.differ, ['fig-1']);
  assert.equal(edited.methods.same, false);
  // The certificate itself edited: its fingerprint no longer matches.
  const { createZip } = await import('./zip.js');
  const files = await readZip(zip);
  const cert = JSON.parse(new TextDecoder().decode(files.get('certificate.json')));
  cert.steps[0].name = 'Another';
  files.set('certificate.json', new TextEncoder().encode(JSON.stringify(cert)));
  const tampered = await checkReportData({ ...input, zip: await createZip([...files].map(([name, data]) => ({ name, data }))) });
  assert.match(tampered.problems[0], /fingerprint does not match/);
});

// The report the program's verify reads in its own tests (testdata/report.html, verify_test.go):
// what this page writes for the fixture, its certificate entry by entry (deflate's bytes are the
// platform's). Run with SITUCRAFT_WRITE_FIXTURES=1 to write it again.
test('the report the program reads in its tests is the one this page writes', async () => {
  const { html } = await report();
  const path = new URL('../../testdata/report.html', import.meta.url);
  if (process.env.SITUCRAFT_WRITE_FIXTURES) writeFileSync(path, html);
  const kept = readFileSync(path, 'utf8');
  const carried = /(<script type="application\/zip" id="situcraft-certificate">)([^<]*)/;
  const zipOf = (text) => readZip(new Uint8Array(Buffer.from(carried.exec(text)[2], 'base64')));
  assert.equal(kept.replace(carried, '$1'), html.replace(carried, '$1'));
  const [a, b] = [await zipOf(kept), await zipOf(html)];
  assert.deepEqual([...a.keys()], [...b.keys()]);
  for (const [name, bytes] of b) assert.deepEqual(a.get(name), bytes, name);
});
