// Adapted from MaveScape 0.8.0 web/lib/zip.test.mjs.
import assert from 'node:assert/strict';
import test from 'node:test';
import { crc32, createZip, isZip, listZip, readZip, readZipEntry } from './zip.js';

const DATE = new Date(2024, 4, 17, 13, 45, 30);

test('CRC-32 matches the standard check value', () => {
  // CRC-32/ISO-HDLC check value for "123456789".
  assert.equal(crc32(new TextEncoder().encode('123456789')), 0xcbf43926);
  assert.equal(crc32(new Uint8Array(0)), 0);
});

test('stored and deflated entries round-trip', async () => {
  const big = new Uint8Array(100000);
  for (let i = 0; i < big.length; i += 1) big[i] = (i * 7) % 13;
  const archive = await createZip([
    { name: 'small.txt', data: 'hello' },
    { name: 'data/big.bin', data: big },
    { name: 'naïve.json', data: '{"a":1}'.repeat(100) },
    { name: 'raw.bin', data: big, compress: false },
  ], { date: DATE });
  assert.ok(isZip(archive));
  const entries = listZip(archive);
  assert.deepEqual(entries.map((e) => e.name), ['small.txt', 'data/big.bin', 'naïve.json', 'raw.bin']);
  assert.equal(entries[0].method, 0);
  assert.equal(entries[1].method, 8);
  assert.ok(entries[1].compressedSize < big.length / 5);
  assert.equal(entries[3].method, 0);
  const files = await readZip(archive);
  assert.equal(new TextDecoder().decode(files.get('small.txt')), 'hello');
  assert.deepEqual(files.get('data/big.bin'), big);
  assert.deepEqual(files.get('raw.bin'), big);
  assert.equal(new TextDecoder().decode(files.get('naïve.json')), '{"a":1}'.repeat(100));
  // The same inputs and date give the same bytes.
  const again = await createZip([{ name: 'small.txt', data: 'hello' }], { date: DATE });
  const twice = await createZip([{ name: 'small.txt', data: 'hello' }], { date: DATE });
  assert.deepEqual(again, twice);
});

test('damaged and foreign inputs fail with clear messages', async () => {
  const archive = await createZip([{ name: 'a.txt', data: 'abcdef' }], { date: DATE });
  const corrupt = archive.slice();
  corrupt[30 + 5] ^= 0xff; // flip a data byte of the stored entry
  const [entry] = listZip(corrupt);
  await assert.rejects(() => readZipEntry(corrupt, entry), /CRC mismatch/);
  assert.throws(() => listZip(new Uint8Array(40)), /Not a ZIP archive/);
  assert.equal(isZip(new TextEncoder().encode('cell_id,label')), false);
  await assert.rejects(() => createZip([{ name: 'x', data: 'a' }, { name: 'x', data: 'b' }]), /twice/);
});
