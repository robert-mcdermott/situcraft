import assert from 'node:assert/strict';
import test from 'node:test';
import { inflateSync } from 'node:zlib';
import { crc32, encodePNG, pngChunk } from './png.js';

test('CRC-32: the check value of "123456789"', () => {
  assert.equal(crc32(new TextEncoder().encode('123456789')), 0xcbf43926);
  assert.equal(crc32(new Uint8Array(0)), 0);
});

test('a PNG: its signature, header, a chunk\'s CRC, and its rows inflating to the pixels', async () => {
  const rgba = new Uint8Array([255, 0, 0, 255, 0, 255, 0, 128, 0, 0, 255, 0, 1, 2, 3, 4]);
  const png = await encodePNG(rgba, 2, 2);
  assert.deepEqual([...png.subarray(0, 8)], [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  const view = new DataView(png.buffer, png.byteOffset);
  assert.equal(String.fromCharCode(...png.subarray(12, 16)), 'IHDR');
  assert.deepEqual([view.getUint32(16), view.getUint32(20)], [2, 2]);
  assert.equal(view.getUint32(8 + 4 + 4 + 13), crc32(png.subarray(12, 12 + 4 + 13)));
  const idat = 8 + 25;
  const length = view.getUint32(idat);
  assert.equal(String.fromCharCode(...png.subarray(idat + 4, idat + 8)), 'IDAT');
  const raw = inflateSync(png.subarray(idat + 8, idat + 8 + length));
  assert.deepEqual([...raw], [0, ...rgba.subarray(0, 8), 0, ...rgba.subarray(8, 16)]);
  assert.deepEqual([...pngChunk('IEND', new Uint8Array(0))], [...png.subarray(png.length - 12)]);
});
