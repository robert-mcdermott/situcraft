// Landmark fits (lib/transforms.js), as internal/register fits them, and transforms' records.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addCheck, checkRecord, checksOf, addTransform, deformationAt, describeTransform, fitLandmarks, placePoint, unplacePoint, transformInUse, transformRecord, transformShape, transformsOf, useTransform } from './transforms.js';

// The checksum of 300 fits of each model internal/register/fit_test.go computes (FNV-1a of every
// coefficient's, residual's and leave-one-out error's bits).
function fitFingerprint() {
  let s = 777;
  const next = () => {
    s = (s * 48271) % 2147483647;
    return s / 2147483647;
  };
  let h = 2166136261;
  const view = new DataView(new ArrayBuffer(8));
  const add = (v) => {
    view.setFloat64(0, v, true);
    for (let i = 0; i < 8; i += 1) h = Math.imul(h ^ view.getUint8(i), 16777619) >>> 0;
  };
  for (let k = 0; k < 300; k += 1) {
    const n = 3 + (k % 9);
    const a = 0.3 + next();
    const b = 0.4 * (next() - 0.5);
    const c = 2000 * next();
    const d = 0.4 * (next() - 0.5);
    let e = 0.3 + next();
    if (k % 2 === 0) e = -e;
    const f = 2000 * next();
    const kps = [];
    for (let i = 0; i < n; i += 1) {
      const mx = 30000 * next();
      const my = 24000 * next();
      const nx = (next() - 0.5) * 4;
      const ny = (next() - 0.5) * 4;
      kps.push({ moving: [mx, my], fixed: [a * mx + b * my + c + nx, d * mx + e * my + f + ny] });
    }
    for (const model of ['similarity', 'affine']) {
      let fit;
      try {
        fit = fitLandmarks(model, kps);
      } catch {
        add(-1);
        continue;
      }
      const t = fit.transform;
      for (const v of [t[0][0], t[0][1], t[0][2], t[1][0], t[1][1], t[1][2], fit.rms, fit.max, fit.looRms, fit.looMax]) add(v);
      for (const v of fit.residuals) add(v);
      for (const v of fit.leaveOneOut ?? []) add(v);
    }
  }
  return h.toString(16).padStart(8, '0');
}

test('the fits are the program\'s, bit for bit', () => {
  assert.equal(fitFingerprint(), '5c09decb');
});

test('a square of landmarks, one placed 2 µm off, fitted as by hand', () => {
  const h = 2;
  const kps = [
    { moving: [0, 0], fixed: [0, 0] },
    { moving: [2, 0], fixed: [2, 0] },
    { moving: [0, 2], fixed: [0, 2] },
    { moving: [2, 2], fixed: [2 + h, 2] },
  ];
  const close = (a, b) => a.every((x, i) => Math.abs(x - b[i]) < 1e-12);
  const affine = fitLandmarks('affine', kps);
  assert.ok(close(affine.residuals, [0.5, 0.5, 0.5, 0.5]) && close(affine.leaveOneOut, [2, 2, 2, 2]));
  const sim = fitLandmarks('similarity', kps);
  assert.ok(close(sim.residuals, [0, Math.SQRT2 / 2, Math.SQRT2 / 2, 1]), `${sim.residuals}`);
  assert.ok(close(sim.leaveOneOut, [0, Math.SQRT2, Math.SQRT2, 2]), `${sim.leaveOneOut}`);
  assert.throws(() => fitLandmarks('affine', [{ moving: [0, 0], fixed: [0, 0] }, { moving: [1, 1], fixed: [1, 1] }, { moving: [2, 2], fixed: [2, 2] }]), /one line/);
  assert.throws(() => fitLandmarks('similarity', kps.slice(0, 2)), /three or more/);
  assert.equal(fitLandmarks('affine', kps.slice(0, 3)).leaveOneOut, null);
  // A reflected similarity: scale 0.5, mirrored, turned a quarter.
  const shape = transformShape([[0, 0.5, 3], [0.5, 0, 4]]);
  assert.ok(shape.reflected && Math.abs(shape.scale - 0.5) < 1e-12 && Math.abs(shape.turn[0]) < 1e-12 && Math.abs(shape.turn[1] - 1) < 1e-12 && Math.abs(shape.aspect - 1) < 1e-12);
});

const imageManifest = { id: 'img-1', name: 'H&E', kind: 'image', state: 'complete' };
const tf = (id, method, extra = {}) => ({
  id, name: `Alignment ${id}`, state: 'complete', created: '2026-10-16T00:00:00Z',
  lineage: { parents: [{ kind: 'image', id: 'img-1' }], method: { name: 'Landmarks', version: '1' } },
  files: [{ path: 'transform.json', role: 'transform', size: 1, sha256: 'a' }],
  summary: { image: 'img-1', method, model: 'similarity', landmarks: method === 'matrix' ? 0 : 6, rms: 0.41, looRms: 0.62, ...extra },
});

test('an image\'s alignments are versions; the latest decision places it', () => {
  const dataset = { id: 'd1', name: 'Rep1' };
  let project = { datasets: [dataset], artifacts: [{ kind: 'image', ...imageManifest, dataset: 'd1' }], history: [], decisions: [], views: [] };
  assert.equal(transformInUse(project, 'img-1'), null, 'not aligned');
  const supplied = transformRecord(dataset, tf('tf-a', 'matrix'));
  project = addTransform(project, supplied);
  assert.equal(addTransform(project, supplied), project, 'recorded once');
  project = useTransform(project, { transform: 'tf-a', rationale: 'Supplied with the image.' });
  const mine = transformRecord(dataset, tf('tf-b', 'landmarks', { parent: 'tf-a' }));
  assert.equal(mine.parent, 'tf-a');
  project = addTransform(project, mine);
  assert.equal(transformInUse(project, 'img-1').record.id, 'tf-a');
  project = useTransform(project, { transform: 'tf-b', by: 'RM' });
  const inUse = transformInUse(project, 'img-1');
  assert.equal(inUse.record.id, 'tf-b');
  assert.equal(inUse.decision.by, 'RM');
  assert.match(inUse.decision.rationale, /6 landmarks, similarity: residuals 0\.41 µm root-mean-square, 0\.62 µm left out/);
  assert.equal(transformsOf(project, 'img-1').length, 2);
  assert.equal(describeTransform(supplied), 'Xenium Explorer\'s transformation matrix');
  assert.throws(() => useTransform(project, { transform: 'tf-z' }), /no such alignment/);
});

test('a deformation evaluated as the program evaluates it', () => {
  // A grid of 5 × 4 control points 10 µm apart, from (−10, −10); one moved by (2, −1).
  const b = { origin: [-10, -10], spacing: 10, nx: 5, ny: 4, dx: new Array(20).fill(0), dy: new Array(20).fill(0) };
  b.dx[1 * 5 + 2] = 2;
  b.dy[1 * 5 + 2] = -1;
  // At the control point itself the cubic B-spline weighs it 4/6 · 4/6.
  const [dx, dy] = deformationAt(b, 10, 0);
  assert.ok(Math.abs(dx - 2 * 16 / 36) < 1e-12 && Math.abs(dy + 16 / 36) < 1e-12, `${dx}, ${dy}`);
  assert.deepEqual(deformationAt(b, 100, 100), [0, 0]);
  assert.deepEqual(placePoint({ transform: [[2, 0, 1], [0, 2, 0]] }, 3, 4), [7, 8]);
  const [px, py] = placePoint({ transform: [[1, 0, 0], [0, 1, 0]], deformation: b }, 10, 0);
  assert.ok(Math.abs(px - 10 - 2 * 16 / 36) < 1e-12 && Math.abs(py + 16 / 36) < 1e-12);
  // And back: the image's point a version places at a point of the tissue.
  const desc = { transform: [[0.5, 0.1, 3], [-0.1, 0.5, 7]], deformation: b };
  for (const [u, v] of [[10, 0], [-4, 12], [30, 25]]) {
    const [x, y] = placePoint(desc, u, v);
    const [bu, bv] = unplacePoint(desc, x, y);
    assert.ok(Math.abs(bu - u) < 1e-6 && Math.abs(bv - v) < 1e-6, `${u}, ${v}: back at ${bu}, ${bv}`);
  }
});

test('a version\'s checks recorded and described', () => {
  const dataset = { id: 'd1', name: 'Rep1' };
  let project = { datasets: [dataset], artifacts: [{ kind: 'transform', id: 'tf-a', name: 'Automatic', dataset: 'd1', state: 'complete' }], history: [], decisions: [], views: [] };
  const rec = checkRecord(dataset, { id: 'chk-1', name: 'Check of Automatic', state: 'complete', summary: { image: 'img-1', transform: 'tf-a', checked: 40, matched: 38, median: 0.42, p90: 0.9, max: 1.31, rms: 0.55, noise: 0.3 } });
  project = addCheck(project, rec);
  assert.equal(addCheck(project, rec), project, 'recorded once');
  assert.equal(checksOf(project, 'tf-a').length, 1);
  assert.match(project.history.at(-1).detail, /Checked Automatic: off by 0\.42 µm \(median\), 0\.90 µm \(90th percentile\), at most 1\.31 µm, over 38 of 40 patches; the matches' own error at most about 0\.30 µm/);
});
