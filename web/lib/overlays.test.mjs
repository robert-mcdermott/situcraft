// Post-Xenium images' geometry (lib/overlays.js) and records (lib/images.js).

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { apply, defaultImageLayer, invert, modelMatrix, overlayBounds, overlayLevel, overlayTileRange, readInverseField, tileGeometry, umPerPixel } from './overlays.js';
import { addImage, describeImage, describePlacement, imageRecord, imagesOf, placedImages } from './images.js';
import { captureView, viewLayers } from './views.js';

// The pathology fixture's immunofluorescence image: its pixels to µm, turned half a turn (its
// transformation matrix times 0.2125 µm).
const IF = [[-0.738696690, 0.005370856, 9027.406152], [-0.005370856, -0.738696690, 6886.863877]];
const LEVELS = [
  { width: 14239, height: 9777, tileWidth: 1024, tileHeight: 1024, across: 14, down: 10 },
  { width: 7119, height: 4888, tileWidth: 1024, tileHeight: 1024, across: 7, down: 5 },
  { width: 3559, height: 2444, tileWidth: 1024, tileHeight: 1024, across: 4, down: 3 },
];

test('a transform, its inverse and its scale', () => {
  const inv = invert(IF);
  for (const [x, y] of [[0, 0], [14239, 9777], [5000.5, 123]]) {
    const [u, v] = apply(IF, x, y);
    const [bx, by] = apply(inv, u, v);
    assert.ok(Math.abs(bx - x) < 1e-6 && Math.abs(by - y) < 1e-6, `${x}, ${y}`);
  }
  assert.ok(Math.abs(umPerPixel(IF) - 0.73871621) < 1e-6);
  assert.equal(invert([[1, 2, 0], [2, 4, 0]]), null);
  // Column-major, as WebGL takes it: the third column is the translation.
  const m = modelMatrix(IF);
  assert.deepEqual([m[6], m[7], m[8]], [Math.fround(9027.406152), Math.fround(6886.863877), 1]);
});

test('the level for a zoom: the coarsest whose pixels are no larger than a device pixel', () => {
  // 0.74 µm a pixel at full resolution, about 1.48 at level 1, 2.95 at level 2.
  assert.equal(overlayLevel(LEVELS, IF, 2), 0, 'half a µm a device pixel: full resolution');
  assert.equal(overlayLevel(LEVELS, IF, 1 / 1.6), 1);
  assert.equal(overlayLevel(LEVELS, IF, 1 / 3.5), 2);
  assert.equal(overlayLevel(LEVELS, IF, 1 / 100), 2, 'the coarsest when even it is finer');
});

test('the tiles in view, through the transform', () => {
  const inv = invert(IF);
  // A rectangle around the image's full-resolution pixel (2000, 3000), turned half a turn.
  const [cx, cy] = apply(IF, 2000, 3000);
  const r = overlayTileRange(LEVELS, 0, inv, { x0: cx - 10, y0: cy - 10, x1: cx + 10, y1: cy + 10 });
  assert.deepEqual(r, { tx0: 1, ty0: 2, tx1: 1, ty1: 2 });
  const whole = overlayBounds({ image: { levels: LEVELS }, transform: IF });
  assert.deepEqual(overlayTileRange(LEVELS, 2, inv, whole), { tx0: 0, ty0: 0, tx1: 3, ty1: 2 });
  assert.equal(overlayTileRange(LEVELS, 0, inv, { x0: 1e6, y0: 1e6, x1: 1e6 + 5, y1: 1e6 + 5 }), null);
  // An edge tile is drawn as far as the image goes, in full-resolution pixels.
  const g = tileGeometry(LEVELS, 1, 6, 4);
  assert.deepEqual(g.texels, [7119 - 6 * 1024, 4888 - 4 * 1024]);
  assert.ok(Math.abs(g.origin[0] - 6 * 1024 * (14239 / 7119)) < 1e-9 && Math.abs(g.size[1] - (4888 - 4096) * (9777 / 4888)) < 1e-9);
});

test('an image starts opaque if RGB; an immunofluorescence image\'s channels in their ranges, its nuclear stain off', () => {
  const he = defaultImageLayer({ id: 'img-a', image: { kind: 'rgb', bits: 8, channels: [{ name: 'RGB' }] } });
  assert.deepEqual(he, { id: 'img-a', visible: true, opacity: 1, channels: [] });
  const fl = defaultImageLayer({ id: 'img-b', image: { kind: 'channels', bits: 16, channels: [{ name: 'CD20' }, { name: 'DAPI' }] }, ranges: [[10, 900], [5, 4000]] });
  assert.deepEqual(fl.channels.map((c) => [c.visible, c.window]), [[true, [10, 900]], [false, [5, 4000]]]);
});

const manifest = {
  id: 'img-1', name: 'H&E (S1_he_image.ome.tif)', state: 'complete', created: '2026-10-16T00:00:00Z',
  lineage: { method: { name: 'Post-Xenium image, read where it lies', version: '1' }, job: { id: 'j1', kind: 'image' } },
  files: [{ path: 'image.json', role: 'description', size: 10, sha256: 'a' }, { path: '/data/S1_he_image.ome.tif', role: 'image', size: 9, sha256: 'b', location: 'place' }],
  summary: { kind: 'rgb', width: 30786, height: 24241, levels: 7, compression: 'deflate', channels: [{ name: 'RGB' }], pixelSize: 0.3638 },
};

test('an image recorded in the project, described in words, not placed until aligned', () => {
  const project = { datasets: [{ id: 'd1', name: 'Rep1' }], artifacts: [], history: [], decisions: [], views: [] };
  const record = imageRecord(project.datasets[0], manifest, 'now');
  assert.equal(record.image, '/data/S1_he_image.ome.tif');
  const next = addImage(project, record, 'now');
  assert.equal(imagesOf(next, 'd1').length, 1);
  assert.equal(addImage(next, record, 'now'), next, 'recorded once');
  assert.match(next.history[0].detail, /Added H&E .* to Rep1, read where it lies \(artifact img-1, by SHA-256\)/);
  assert.equal(describeImage(record), 'RGB, 30,786 × 24,241, 7 levels, deflate');
  assert.deepEqual(placedImages(next, 'd1').map((p) => p.transform), [null], 'not aligned');
  assert.equal(describePlacement({ umPerPixel: 0.363902, rotation: 0.28, reflected: true }), '0.3639 µm per pixel, turned 0.3°, mirrored');
});

test('a saved view keeps its images\' layers, and a view without images is as before', () => {
  const desc = { channels: [{ name: 'DAPI' }], transcripts: { genes: [] } };
  const layers = {
    channels: [{ visible: true, color: '#4f8cff', window: [0, 100], gamma: 1 }],
    transcripts: { visible: true, all: false, genes: [], colors: [], minQ: 20, pointSize: 3, gain: 1 },
    cells: { outlines: true, nuclei: false, fill: null, opacity: 0.55, segmentation: null },
  };
  const state = { camera: { cx: 1, cy: 2, scale: 1 }, layers };
  assert.equal(captureView({ name: 'v', dataset: 'd1', state, desc }).images, undefined);
  const withImages = { ...layers, images: [{ id: 'img-1', visible: true, opacity: 0.4, channels: [{ visible: false, color: '#ff5ca8', window: [3, 200], gamma: 1.2 }] }] };
  const view = captureView({ name: 'v', dataset: 'd1', state: { ...state, layers: withImages }, desc });
  assert.deepEqual(view.images, withImages.images);
  const back = viewLayers(view, desc, layers);
  assert.deepEqual(back.layers.images, withImages.images);
});

test('a deformed image\'s inverse field read as the program writes it', () => {
  const nx = 3;
  const ny = 2;
  const buffer = new ArrayBuffer(24 + 8 * nx * ny);
  const view = new DataView(buffer);
  [83, 67, 73, 86].forEach((c, i) => view.setUint8(i, c));
  view.setUint32(4, nx, true);
  view.setUint32(8, ny, true);
  view.setFloat32(12, -100, true);
  view.setFloat32(16, -50, true);
  view.setFloat32(20, 25, true);
  for (let k = 0; k < nx * ny; k += 1) {
    view.setFloat32(24 + 8 * k, k === 4 ? 3 : 0.5, true);
    view.setFloat32(28 + 8 * k, k === 4 ? -4 : 0, true);
  }
  const f = readInverseField(buffer);
  assert.deepEqual([f.x0, f.y0, f.step, f.nx, f.ny, f.field.length], [-100, -50, 25, 3, 2, 12]);
  assert.equal(f.max, 5);
  assert.throws(() => readInverseField(new ArrayBuffer(24)), /not an inverse field/);
});
