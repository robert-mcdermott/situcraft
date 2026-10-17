import assert from 'node:assert/strict';
import test from 'node:test';
import { fitView, insidePolygon, nearestPoint, pointGrid, pointsInPolygon, rasterize, toPixel, toPlane, valueColors, zoomAt } from './scatter.js';

test('the view fits the points, and pixels and the plane map both ways', () => {
  const xy = Float32Array.from([0, 0, 10, 5, 4, 2]);
  const view = fitView(xy, 3, 200, 100, 0);
  assert.equal(view.cx, 5);
  assert.equal(view.cy, 2.5);
  assert.equal(view.scale, 0.05);
  assert.deepEqual(toPixel(view, 200, 100, 0, 0), [0, 100]);
  assert.deepEqual(toPixel(view, 200, 100, 10, 5), [200, 0]);
  const [x, y] = toPlane(view, 200, 100, 50, 25);
  assert.deepEqual([x, y], [2.5, 3.75]);
  // Zooming about a pixel keeps the plane's point under it.
  const z = zoomAt(view, 200, 100, 50, 25, 2);
  assert.equal(z.scale, 0.025);
  assert.deepEqual(toPlane(z, 200, 100, 50, 25), [2.5, 3.75]);
});

test('points are squares in their colors; a selection is drawn over the others, dimmed', () => {
  const xy = Float32Array.from([1, 1, 1, 1, 3, 3]);
  const colors = Uint8Array.from([255, 0, 0, 255, 0, 0, 255, 255, 0, 255, 0, 255]);
  const view = { cx: 2, cy: 2, scale: 1 };
  let img = rasterize({ xy, n: 3, colors, view, width: 4, height: 4, size: 1 });
  // Point 1 (blue) drawn after point 0 (red) at the same pixel: (x 1 → px 1, y 1 → py 3).
  const at = (x, y) => [...img.slice(4 * (y * 4 + x), 4 * (y * 4 + x) + 4)];
  assert.deepEqual(at(1, 3), [0, 0, 255, 255]);
  assert.deepEqual(at(3, 1), [0, 255, 0, 255]);
  assert.deepEqual(at(0, 0), [0, 0, 0, 0]);
  // Point 0 selected: drawn last and at full opacity; point 1 dimmed below it.
  img = rasterize({ xy, n: 3, colors, view, width: 4, height: 4, size: 1, selected: Uint8Array.from([1, 0, 0]) });
  assert.deepEqual(at(1, 3), [255, 0, 0, 255]);
  assert.equal(at(3, 1)[3], Math.round(0.18 * 255));
});

test('the point nearest a place, and the points inside a polygon', () => {
  const n = 1000;
  const xy = new Float32Array(2 * n);
  for (let i = 0; i < n; i += 1) {
    xy[2 * i] = (i % 40) * 0.5;
    xy[2 * i + 1] = Math.floor(i / 40) * 0.5;
  }
  const grid = pointGrid(xy, n);
  assert.equal(nearestPoint(grid, 0.1, 0.1, 1), 0);
  assert.equal(nearestPoint(grid, 19.4, 12.1, 1), 24 * 40 + 39);
  assert.equal(nearestPoint(grid, 50, 50, 1), -1);
  const square = [[0.9, 0.9], [2.1, 0.9], [2.1, 2.1], [0.9, 2.1]];
  const inside = pointsInPolygon(xy, n, square);
  let count = 0;
  for (let i = 0; i < n; i += 1) count += inside[i];
  assert.equal(count, 9, 'x and y each 1, 1.5 and 2');
  assert.equal(insidePolygon([[0, 0], [4, 0], [0, 4]], 1, 1), true);
  assert.equal(insidePolygon([[0, 0], [4, 0], [0, 4]], 3, 3), false);
});

test('values on a colormap, missing values faint gray', () => {
  const lut = new Uint8Array(256 * 3);
  for (let k = 0; k < 256; k += 1) lut.set([k, 0, 255 - k], 3 * k);
  const c = valueColors(Float32Array.from([0, 1, NaN, 2]), lut, 0, 1);
  assert.deepEqual([...c.slice(0, 4)], [0, 0, 255, 255]);
  assert.deepEqual([...c.slice(4, 8)], [255, 0, 0, 255]);
  assert.deepEqual([...c.slice(8, 12)], [128, 128, 128, 60]);
  assert.deepEqual([...c.slice(12, 16)], [255, 0, 0, 255], 'clamped');
});
