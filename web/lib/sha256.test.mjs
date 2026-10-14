import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import test from 'node:test';
import { createRandom } from './random.js';
import { SHA256, sha256 } from './sha256.js';

const text = (s) => new TextEncoder().encode(s);

test('SHA-256 of the FIPS 180-4 examples', () => {
  assert.equal(sha256(text('')), 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
  assert.equal(sha256(text('abc')), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  assert.equal(sha256(text('abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq')), '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1');
  assert.equal(sha256(new Uint8Array(1000000).fill(0x61)), 'cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0');
});

test('SHA-256 in parts of any size equals the hash of the whole', () => {
  const random = createRandom(4);
  for (const n of [0, 1, 55, 56, 63, 64, 65, 119, 120, 128, 1000, 100003]) {
    const bytes = Uint8Array.from({ length: n }, () => Math.floor(random() * 256));
    const expected = createHash('sha256').update(bytes).digest('hex');
    assert.equal(sha256(bytes), expected, `${n} bytes whole`);
    const hash = new SHA256();
    for (let at = 0; at < n;) {
      const size = Math.floor(random() * 150);
      hash.update(bytes.subarray(at, at + size));
      at += size;
    }
    assert.equal(hash.hex(), expected, `${n} bytes in parts`);
  }
});
