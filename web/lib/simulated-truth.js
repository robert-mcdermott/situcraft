// A simulated tissue's truth for its post-Xenium images (truth/post_xenium/registration.json,
// internal/simulate/postxenium.go): where a pixel of an image truly lies on the section (its
// transform, then its deformation, a sum of plane waves along an axis), and landmarks placed by it,
// as a careful hand clicking the same point on both images would place them. Deterministic (the
// sine is lib/dmath.js's), so a version fitted to them is the same on any computer.

import { sincos } from './dmath.js';

const TWO_PI = 2 * Math.PI;

// Where the truth places a point of the image (its full-resolution pixels, (0, 0) its first
// pixel's corner) on the section, in µm.
export function truePlace(image, u, v) {
  const t = image.transform;
  const x = t[0][0] * u + t[0][1] * v + t[0][2];
  const y = t[1][0] * u + t[1][1] * v + t[1][2];
  const d = [0, 0];
  for (const w of image.deformation ?? []) {
    const [s] = sincos(TWO_PI * (w.frequency_per_um[0] * x + w.frequency_per_um[1] * y) + w.phase);
    d[w.axis] += w.amplitude_um * s;
  }
  return [x + d[0], y + d[1]];
}

// The image's points, as fractions of its width and height, tried in order for landmarks: spread
// over it, the middle among them.
const SPREAD = [[0.25, 0.25], [0.75, 0.25], [0.5, 0.5], [0.25, 0.75], [0.75, 0.75], [0.5, 0.2], [0.2, 0.5], [0.8, 0.5], [0.5, 0.8], [0.35, 0.4], [0.65, 0.6]];

// count landmarks ({ fixed: [x, y] µm, moving: [u, v] the image's pixels }) at the image's pixels
// (their centers) the truth places within a rectangle of the section ([x0, y0, x1, y1] µm).
export function landmarksFromTruth(image, within, count = 6) {
  const [x0, y0, x1, y1] = within;
  const out = [];
  for (const [a, b] of SPREAD) {
    const u = Math.floor(a * image.width) + 0.5;
    const v = Math.floor(b * image.height) + 0.5;
    const [x, y] = truePlace(image, u, v);
    if (x < x0 || y < y0 || x > x1 || y > y1) continue;
    out.push({ fixed: [x, y], moving: [u, v] });
    if (out.length === count) break;
  }
  return out;
}
