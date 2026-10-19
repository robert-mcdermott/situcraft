// Adapted from MaveScape 0.8.0 (web/lib/draw.test.mjs, Apache-2.0, the same author).
import assert from 'node:assert/strict';
import test from 'node:test';
import { inflateSync } from 'node:zlib';
import { baselineShift, marksToSVG, paintMarks, parsePath, pdfPage, placeMarks, textMarks } from './draw.js';
import { writePDF } from './pdf.js';

test('paths: absolute commands, H and V as lines, relative commands refused', () => {
  assert.deepEqual(parsePath('M1 2H5V7L0 0C1 2 3 4 5 6Z'), [
    { op: 'M', args: [1, 2] }, { op: 'L', args: [5, 2] }, { op: 'L', args: [5, 7] }, { op: 'L', args: [0, 0] }, { op: 'C', args: [1, 2, 3, 4, 5, 6] }, { op: 'Z', args: [] },
  ]);
  assert.deepEqual(parsePath('M0 0 3 4'), [{ op: 'M', args: [0, 0] }, { op: 'L', args: [3, 4] }]);
  assert.throws(() => parsePath('m0 0l1 1'), /absolute/);
  assert.throws(() => parsePath('M0'), /ends early/);
});

test('marks placed: moved and scaled, text and strokes with them, groups and clips too', () => {
  const placed = placeMarks([
    { type: 'rect', x: 10, y: 20, w: 5, h: 4, fill: '#000', width: 2 },
    { type: 'text', x: 1, y: 1, text: 'A', size: 10 },
    { type: 'path', d: 'M0 0H10V10Z', fill: '#f00' },
    { type: 'group', clip: { x: 0, y: 0, w: 10, h: 10 }, marks: [{ type: 'circle', x: 2, y: 2, r: 1 }] },
  ], { dx: 100, dy: 50, k: 0.5 });
  assert.deepEqual(placed[0], { type: 'rect', x: 105, y: 60, w: 2.5, h: 2, fill: '#000', width: 1 });
  assert.deepEqual([placed[1].x, placed[1].y, placed[1].size], [100.5, 50.5, 5]);
  assert.equal(placed[2].d, 'M100 50L105 50L105 55Z');
  assert.deepEqual(placed[3].clip, { x: 100, y: 50, w: 5, h: 5 });
  assert.deepEqual([placed[3].marks[0].x, placed[3].marks[0].r], [101, 0.5]);
  assert.deepEqual(textMarks(placed).map((m) => m.text), ['A']);
});

const marks = [
  { type: 'rect', x: 0, y: 0, w: 100, h: 50, fill: '#ffffff' },
  { type: 'rect', x: 1, y: 2, w: 3, h: 4, fill: '#2166ac', title: 'S2A <x>' },
  { type: 'hatch', x: 10, y: 10, w: 6, h: 6, fill: '#c8c6c2', stroke: '#6e6c68' },
  { type: 'hatch', x: 20, y: 10, w: 6, h: 6, fill: '#c8c6c2', stroke: '#6e6c68' },
  { type: 'line', x1: 0, y1: 0, x2: 10, y2: 10, stroke: '#000', width: 0.5, dash: [2, 1] },
  { type: 'text', x: 50, y: 25, text: 'Score & SE', size: 7, bold: true, anchor: 'middle', baseline: 'middle' },
  { type: 'text', x: 5, y: 45, text: 'Rotated', size: 7, rotate: -90 },
  { type: 'group', clip: { x: 0, y: 0, w: 50, h: 50 }, label: 'Panel A', marks: [{ type: 'circle', x: 5, y: 5, r: 2, fill: '#b2182b' }] },
];

test('SVG: the page in points, one pattern per hatch, clips, titles and text escaped, the same text twice', async () => {
  const svg = await marksToSVG(marks, 100, 50, { title: 'Figure 1', desc: 'A & B', metadata: '{"a":"<1>"}' });
  assert.match(svg, /^<svg xmlns="http:\/\/www.w3.org\/2000\/svg" width="100pt" height="50pt" viewBox="0 0 100 50"/);
  assert.equal(svg.match(/<pattern /g).length, 1, 'two hatches of the same colors share their pattern');
  assert.match(svg, /<rect x="1" y="2" width="3" height="4" fill="#2166ac"><title>S2A &lt;x&gt;<\/title><\/rect>/);
  assert.match(svg, /<desc>A &amp; B<\/desc>/);
  assert.match(svg, /<metadata>\{&quot;a&quot;:&quot;&lt;1&gt;&quot;\}<\/metadata>/);
  assert.match(svg, /<g clip-path="url\(#clip1\)" role="group" aria-label="Panel A">/);
  assert.match(svg, /stroke-dasharray="2 1"/);
  // The baseline moved down by a third of the size, as the PDF places it.
  assert.match(svg, new RegExp(`<text x="50" y="${25 + baselineShift('middle', 7)}" font-size="7" font-weight="bold" text-anchor="middle" fill="#171b26">Score &amp; SE</text>`));
  assert.match(svg, /transform="rotate\(-90 5 45\)"/);
  assert.equal(svg, await marksToSVG(marks, 100, 50, { title: 'Figure 1', desc: 'A & B', metadata: '{"a":"<1>"}' }));
});

test('SVG: an image embedded as a PNG', async () => {
  const rgba = new Uint8Array(2 * 2 * 4).fill(255);
  const svg = await marksToSVG([{ type: 'image', x: 0, y: 0, w: 10, h: 10, image: { width: 2, height: 2, rgba } }], 10, 10);
  const data = svg.match(/href="data:image\/png;base64,([^"]+)"/)[1];
  assert.deepEqual([...Buffer.from(data, 'base64').subarray(1, 4)].map((c) => String.fromCharCode(c)).join(''), 'PNG');
  assert.match(svg, /image-rendering="pixelated"/);
});

test('canvas: every mark painted, in order, clips saved and restored', () => {
  const calls = [];
  const state = {};
  const ctx = new Proxy(state, {
    get: (target, name) => (name in target ? target[name] : (...args) => calls.push([name, ...args])),
    set: (target, name, value) => {
      target[name] = value;
      if (name === 'fillStyle') calls.push(['fillStyle', value]);
      return true;
    },
  });
  paintMarks(ctx, marks);
  const fills = calls.filter((c) => c[0] === 'fillStyle').map((c) => c[1]);
  assert.deepEqual(fills, ['#ffffff', '#2166ac', '#c8c6c2', '#c8c6c2', '#171b26', '#171b26', '#b2182b']);
  assert.equal(calls.filter((c) => c[0] === 'save').length, calls.filter((c) => c[0] === 'restore').length);
  assert.ok(calls.some((c) => c[0] === 'fillText' && c[1] === 'Score & SE'));
  assert.ok(calls.some((c) => c[0] === 'rotate' && Math.abs(c[1] + Math.PI / 2) < 1e-12));
});

test('PDF: the marks on a page of points, text as text, the rotation turned to the PDF\'s sense', async () => {
  const bytes = await writePDF([pdfPage(marks, 100, 50)], { title: 'Figure 1', date: new Date('2026-10-12T00:00:00Z') });
  const text = new TextDecoder('latin1').decode(bytes);
  assert.match(text, /\/MediaBox \[0 0 100 50\]/);
  const at = text.indexOf('/Filter /FlateDecode /Length');
  const length = Number(text.slice(at).match(/\/Length (\d+)/)[1]);
  const start = text.indexOf('stream\n', at) + 'stream\n'.length;
  const ops = inflateSync(Buffer.from(bytes.slice(start, start + length))).toString('latin1');
  assert.match(ops, /\(Score & SE\) Tj/);
  assert.match(ops, /0.129 0.4 0.675 rg/, 'the cell\'s color');
  // Rotated −90° clockwise on the page is 90° counterclockwise in the PDF: cos 0, sin 1.
  assert.match(ops, /0 1 -1 0 [\d.]+ [\d.]+ Tm \/F1 7 Tf 0 Ts \(Rotated\) Tj/);
  assert.match(ops, /re W n/, 'the group clipped');
  assert.deepEqual(bytes, await writePDF([pdfPage(marks, 100, 50)], { title: 'Figure 1', date: new Date('2026-10-12T00:00:00Z') }));
  await assert.rejects(writePDF([pdfPage(marks, 100, 50)], { title: 'x' }), /creation date/);
});

test('a path filled by the even-odd rule: its hole in SVG and in PDF (SituCraft)', async () => {
  const marks = [{ type: 'path', d: 'M0 0H10V10H0ZM3 3H7V7H3Z', fill: '#ff0000', rule: 'evenodd' }];
  const svg = await marksToSVG(marks, 10, 10);
  assert.match(svg, /fill="#ff0000" fill-rule="evenodd"/);
  const pdf = await writePDF([pdfPage(marks, 10, 10)], { date: new Date('2026-10-19T00:00:00Z') });
  const text = new TextDecoder('latin1').decode(pdf);
  const at = text.indexOf('<< /Filter /FlateDecode /Length');
  const length = Number(text.slice(at).match(/\/Length (\d+)/)[1]);
  const start = text.indexOf('stream\n', at) + 'stream\n'.length;
  const ops = inflateSync(Buffer.from(pdf.slice(start, start + length))).toString('latin1');
  assert.match(ops, /\nf\*$/);
});
