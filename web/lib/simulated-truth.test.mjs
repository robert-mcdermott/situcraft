// The simulated truth of post-Xenium images (lib/simulated-truth.js): where an image's pixel lies,
// and landmarks placed by it.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { landmarksFromTruth, truePlace } from './simulated-truth.js';

const image = { width: 1000, height: 800, transform: [[0.36, 0.01, -20], [0.01, -0.36, 300]], deformation: [] };

test('without a deformation, a pixel lies where the transform puts it; with one, moved along its axis by the wave', () => {
  assert.deepEqual(truePlace(image, 100, 200), [0.36 * 100 + 0.01 * 200 - 20, 0.01 * 100 - 0.36 * 200 + 300]);
  const waved = { ...image, deformation: [{ axis: 1, amplitude_um: 2, frequency_per_um: [0.25, 0], phase: 0 }] };
  // At x = 1 µm the wave is at a quarter period: sin(π/2) = 1, so y moves by the amplitude.
  const u = (1 + 20 - 0.01 * 100) / 0.36;
  const [x, y] = truePlace(waved, u, 100);
  assert.ok(Math.abs(x - 1) < 1e-12);
  assert.ok(Math.abs(y - (0.01 * u - 0.36 * 100 + 300 + 2)) < 1e-12);
});

test('landmarks: spread over the image, within the rectangle asked, their fixed points the truth\'s', () => {
  const lm = landmarksFromTruth(image, [0, 0, 400, 400], 6);
  assert.equal(lm.length, 6);
  for (const l of lm) {
    assert.deepEqual(l.fixed, truePlace(image, ...l.moving));
    assert.ok(l.fixed.every((v) => v >= 0 && v <= 400));
  }
  assert.equal(new Set(lm.map((l) => l.moving.join())).size, 6);
  // A rectangle few of the image's points fall in: as many as there are.
  assert.ok(landmarksFromTruth(image, [0, 0, 120, 120], 6).length < 6);
});
