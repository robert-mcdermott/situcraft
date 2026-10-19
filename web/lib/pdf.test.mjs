// Adapted from MaveScape 0.8.0 (web/lib/pdf.test.mjs, Apache-2.0, the same author).
import assert from 'node:assert/strict';
import test from 'node:test';
import { inflateSync } from 'node:zlib';
import { rgbaToRgb, writePDF } from './pdf.js';

test('a one-page PDF has a valid structure and the image data inflates', async () => {
  const width = 4;
  const height = 3;
  const rgba = new Uint8ClampedArray(width * height * 4);
  for (let i = 0; i < width * height; i += 1) {
    rgba[i * 4] = 255;
    rgba[i * 4 + 3] = i % 2 ? 255 : 0;
  }
  const rgb = rgbaToRgb(rgba);
  assert.deepEqual(Array.from(rgb.slice(0, 6)), [255, 255, 255, 255, 0, 0]);
  const bytes = await writePDF([{ width: 72, height: 54, image: { width, height, rgb } }], { title: 'Test (1)', date: new Date('2026-10-04T00:00:00Z') });
  const text = new TextDecoder('latin1').decode(bytes);
  assert.ok(text.startsWith('%PDF-1.4'));
  assert.ok(text.trimEnd().endsWith('%%EOF'));
  assert.match(text, /\/Title \(Test \\\(1\\\)\)/);
  // The xref offset points at the xref table.
  const startxref = Number(text.match(/startxref\n(\d+)/)[1]);
  assert.equal(text.slice(startxref, startxref + 4), 'xref');
  // Every object offset in the table points at "n 0 obj".
  const entries = [...text.slice(startxref).matchAll(/^(\d{10}) 00000 n $/gm)].map((m) => Number(m[1]));
  entries.forEach((offset, i) => assert.equal(text.slice(offset, offset + `${i + 1} 0 obj`.length), `${i + 1} 0 obj`));
  // The image stream decompresses to the RGB bytes.
  const start = text.indexOf('stream\n') + 'stream\n'.length;
  const length = Number(text.match(/\/Length (\d+) >>\nstream/)[1]);
  const inflated = inflateSync(Buffer.from(bytes.slice(start, start + length)));
  assert.deepEqual(Array.from(inflated), Array.from(rgb));
});

test('vector pages: WinAnsi text with superscripts, paths, translucent fills and fonts', async () => {
  const { PDFPage, textWidth, winAnsi } = await import('./pdf.js');
  assert.equal(textWidth('Hello', 10), (722 + 556 + 222 + 222 + 556) / 100);
  assert.equal(textWidth('10⁴', 10), (556 + 556 + 0.7 * 556) / 100);
  assert.deepEqual(winAnsi('µ × − ≥ χ'), [0xb5, 0x20, 0xd7, 0x20, 0x2d, 0x20, 0x3e, 0x3d, 0x20, 0x63, 0x68, 0x69]);
  const page = new PDFPage(400, 300);
  page.fillRect(0, 0, 400, 300, '#ffffff');
  page.text('CD4 (µL) 10⁴', 10, 20, { size: 12, bold: true, align: 'left', baseline: 'top' });
  page.moveTo(0, 0);
  page.lineTo(100, 100);
  page.draw({ stroke: '#ff0000', fill: 'rgba(0, 0, 255, 0.5)', width: 2 });
  const bytes = await writePDF([page], { title: 'Vector', date: new Date('2026-10-04T00:00:00Z') });
  const text = new TextDecoder('latin1').decode(bytes);
  assert.match(text, /\/BaseFont \/Helvetica-Bold \/Encoding \/WinAnsiEncoding/);
  assert.match(text, /\/MediaBox \[0 0 300 225\]/);
  assert.match(text, /\/CreationDate \(D:20261004000000Z\)/);
  assert.match(text, /\/ExtGState << \/GS0 << \/Type \/ExtGState \/ca 0.5 \/CA 1 >> >>/);
  const at = text.indexOf('/Filter /FlateDecode /Length');
  const length = Number(text.slice(at).match(/\/Length (\d+)/)[1]);
  const start = text.indexOf('stream\n', at) + 'stream\n'.length;
  const ops = inflateSync(Buffer.from(bytes.slice(start, start + length))).toString('latin1');
  // The parentheses escaped, µ as octal 265, the exponent smaller and raised.
  assert.match(ops, /\/F2 9 Tf 0 Ts \(CD4 \\\(\\265L\\\) 10\) Tj \/F2 6.3 Tf 3.42 Ts \(4\) Tj/);
  assert.match(ops, /\/GS0 gs/);
});
