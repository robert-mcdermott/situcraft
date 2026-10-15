import { test } from 'node:test';
import assert from 'node:assert/strict';
import { camera, densityLevel, fitCamera, gridTileRange, imageLevel, scaleBar, screenToWorld, tileRange, tileUm, viewMatrix, visibleRect, worldToScreen, zoomAt } from './viewport.js';
import { fillTriangles, outlineSegments, pointInRing, readPolygonTile, triangulate } from './polygons.js';
import { attributeRange, densityGrid, joinSamples, nearestCell, readCells, readDensity, readPoints } from './viewer-data.js';

// The Lung bundle's grid (research.md §1.3c).
const grid = { pixelSize: 0.2125, width: 5791, height: 3553, tile: 512, levels: 5 };

test('screen and tissue positions convert both ways, and zooming keeps the point under the cursor', () => {
  const cam = camera(600, 400, 2);
  const [x, y] = screenToWorld(cam, 800, 600, 100, 50);
  assert.deepEqual(worldToScreen(cam, 800, 600, x, y), [100, 50]);
  const zoomed = zoomAt(cam, 800, 600, 100, 50, 3);
  assert.equal(zoomed.scale, 6);
  const [x2, y2] = screenToWorld(zoomed, 800, 600, 100, 50);
  assert.ok(Math.abs(x2 - x) < 1e-9 && Math.abs(y2 - y) < 1e-9);
  assert.equal(zoomAt(cam, 800, 600, 0, 0, 1e9, { maxScale: 50 }).scale, 50);
  const rect = visibleRect(cam, 800, 600);
  assert.deepEqual(rect, { x0: 400, y0: 250, x1: 800, y1: 550 });
  // The matrix takes the visible rectangle's corners to clip space's (y up).
  const m = viewMatrix(cam, 800, 600);
  const clip = (wx, wy) => [m[0] * wx + m[3] * wy + m[6], m[1] * wx + m[4] * wy + m[7]];
  assert.deepEqual(clip(400, 250).map((v) => Math.round(v * 1e6) / 1e6), [-1, 1]);
  assert.deepEqual(clip(800, 550).map((v) => Math.round(v * 1e6) / 1e6), [1, -1]);
});

test('fitting shows the whole rectangle', () => {
  const cam = fitCamera({ x0: 0, y0: 0, x1: 1230, y1: 755 }, 1000, 1000, 0);
  assert.equal(cam.cx, 615);
  assert.ok(Math.abs(cam.scale - 1000 / 1230) < 1e-12);
  const r = visibleRect(cam, 1000, 1000);
  assert.ok(r.x0 <= 1e-9 && r.x1 >= 1230 - 1e-9 && r.y0 <= 0 && r.y1 >= 755);
});

test('the image level keeps pixels no larger than a device pixel', () => {
  // One device pixel per full-resolution pixel: level 0; per level-2 pixel: level 2.
  assert.equal(imageLevel(grid, camera(0, 0, 1 / 0.2125)), 0);
  assert.equal(imageLevel(grid, camera(0, 0, 1 / (0.2125 * 4))), 2);
  assert.equal(imageLevel(grid, camera(0, 0, 1 / (0.2125 * 3.9))), 1);
  assert.equal(imageLevel(grid, camera(0, 0, 100)), 0);
  assert.equal(imageLevel(grid, camera(0, 0, 1e-4)), 4);
});

test('tile ranges', () => {
  assert.equal(tileUm(grid, 0), 108.8);
  assert.deepEqual(tileRange(grid, 0, { x0: 100, y0: 0, x1: 300, y1: 108.8 }, 12, 7), { tx0: 0, ty0: 0, tx1: 2, ty1: 1 });
  assert.equal(tileRange(grid, 0, { x0: -50, y0: -50, x1: -1, y1: 10 }, 12, 7), null);
  // Points past the edges are in the edge tiles.
  assert.deepEqual(gridTileRange(grid, 0, { x0: -50, y0: -50, x1: 5000, y1: 10 }), { tx0: 0, ty0: 0, tx1: 11, ty1: 0 });
  assert.deepEqual(gridTileRange(grid, 4, { x0: 0, y0: 0, x1: 1e6, y1: 1e6 }), { tx0: 0, ty0: 0, tx1: 0, ty1: 0 });
});

test('the density level has bins of a few pixels', () => {
  // Level L's bins are 0.85 × 2^L µm.
  assert.equal(densityLevel(grid, 128, [1, 2, 3, 4], 1), 2);
  assert.equal(densityLevel(grid, 128, [1, 2, 3, 4], 0.1), 1);
  assert.equal(densityLevel(grid, 128, [1, 2, 3, 4], 10), 4);
  assert.equal(densityLevel(grid, 128, [], 1), null);
});

test('the scale bar is a round length', () => {
  assert.deepEqual(scaleBar(1, 120), { um: 100, css: 100, label: '100 µm' });
  assert.equal(scaleBar(10, 120).label, '1 mm');
  assert.equal(scaleBar(0.04, 120).label, '2 µm');
  assert.equal(scaleBar(0.001, 120).label, '100 nm');
});

test('triangulation fills rings exactly, concave ones too', () => {
  const area = (xy, idx) => {
    let a = 0;
    for (let i = 0; i < idx.length; i += 3) {
      const [p, q, r] = [idx[i], idx[i + 1], idx[i + 2]];
      a += Math.abs((xy[2 * q] - xy[2 * p]) * (xy[2 * r + 1] - xy[2 * p + 1]) - (xy[2 * r] - xy[2 * p]) * (xy[2 * q + 1] - xy[2 * p + 1])) / 2;
    }
    return a;
  };
  // An L shape (area 3), closed, in both orientations.
  const L = [0, 0, 2, 0, 2, 1, 1, 1, 1, 2, 0, 2, 0, 0];
  assert.equal(area(L, triangulate(L, 0, 7)), 3);
  const reversed = [];
  for (let i = L.length - 2; i >= 0; i -= 2) reversed.push(L[i], L[i + 1]);
  assert.equal(area(reversed, triangulate(reversed, 0, 7)), 3);
  assert.equal(triangulate(L, 0, 7).length, 12);
  // A star of 10 points: 8 triangles of the right area.
  const star = [];
  for (let i = 0; i < 10; i += 1) {
    const r = i % 2 ? 1 : 2.5;
    star.push(r * Math.cos((i * Math.PI) / 5), r * Math.sin((i * Math.PI) / 5));
  }
  let shoelace = 0;
  for (let i = 0, j = 9; i < 10; j = i, i += 1) shoelace += star[2 * j] * star[2 * i + 1] - star[2 * i] * star[2 * j + 1];
  const tri = triangulate(star, 0, 10);
  assert.equal(tri.length, 24);
  assert.ok(Math.abs(area(star, tri) - Math.abs(shoelace) / 2) < 1e-9);
  // A bow tie (crossing itself) is still covered, and degenerate rings give nothing.
  assert.ok(triangulate([0, 0, 2, 2, 2, 0, 0, 2], 0, 4).length >= 6);
  assert.deepEqual(triangulate([0, 0, 1, 1, 0, 0], 0, 3), []);
});

test('points in rings', () => {
  const L = [0, 0, 2, 0, 2, 1, 1, 1, 1, 2, 0, 2, 0, 0];
  assert.equal(pointInRing(L, 0, 7, 0.5, 1.5), true);
  assert.equal(pointInRing(L, 0, 7, 1.5, 1.5), false);
  assert.equal(pointInRing(L, 0, 7, 1.5, 0.5), true);
  assert.equal(pointInRing(L, 0, 7, 3, 0.5), false);
});

test('geometry tiles become outlines and fills', () => {
  // Two polygons: a closed square (5 vertices) and an open triangle (3).
  const p = 2;
  const v = 8;
  const buffer = new ArrayBuffer(4 * p + 4 * (p + 1) + 8 * v);
  new Uint32Array(buffer, 0, p).set([17, 4]);
  new Uint32Array(buffer, 8, p + 1).set([0, 5, 8]);
  new Float32Array(buffer, 20, 2 * v).set([0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 5, 5, 6, 5, 5, 6]);
  const tile = readPolygonTile(buffer, p, v);
  assert.deepEqual([...tile.cells], [17, 4]);
  const lines = outlineSegments(tile);
  assert.equal(lines.count, 2 * (4 + 3));
  assert.deepEqual([...lines.polygon.slice(0, 2)], [0, 0]);
  assert.deepEqual([...lines.positions.slice(12, 16)], [0, 1, 0, 0]);
  const fills = fillTriangles(tile);
  assert.equal(fills.count, 9);
  assert.deepEqual([...fills.polygon], [0, 0, 0, 0, 0, 0, 1, 1, 1]);
  assert.throws(() => readPolygonTile(buffer, 2, 9));
  assert.equal(readPolygonTile(new ArrayBuffer(0), 0, 0).cells.length, 0);
});

// Encodes answers as internal/view does.
function encode(magic, words, columns) {
  const parts = [];
  let length = 16;
  for (const c of columns) {
    length += c.byteLength;
    if (c.pad) length = (length + 3) & ~3;
  }
  const buffer = new ArrayBuffer(length);
  new Uint8Array(buffer, 0, 4).set([...magic].map((ch) => ch.charCodeAt(0)));
  new Uint32Array(buffer, 4, 3).set(words);
  let at = 16;
  for (const c of columns) {
    new Uint8Array(buffer, at, c.byteLength).set(new Uint8Array(c.buffer, c.byteOffset, c.byteLength));
    at += c.byteLength;
    if (c.pad) at = (at + 3) & ~3;
    parts.push(at);
  }
  return buffer;
}

test('points, density and cells answers are read in place', () => {
  const genes = new Uint16Array([2, 9]);
  genes.pad = true;
  const points = readPoints(encode('SCVP', [3, 2, 0], [new Uint32Array([0, 1, 3]), new Float32Array([1, 2, 3]), new Float32Array([4, 5, 6]), new Uint32Array([7, 8, 9]), genes]));
  assert.deepEqual([...points.genes], [2, 9]);
  assert.deepEqual([...points.y], [4, 5, 6]);
  assert.deepEqual([...points.cell], [7, 8, 9]);
  const dGenes = new Uint16Array([4]);
  dGenes.pad = true;
  const bins = new Uint16Array([1, 2, 130]);
  bins.pad = true;
  const density = readDensity(encode('SCVD', [128, 1, 3], [new Uint32Array([0, 3]), new Uint32Array([5, 6, 7]), dGenes, bins]));
  const g = densityGrid(density, 4);
  assert.equal(g.length, 128 * 128);
  assert.deepEqual([g[1], g[2], g[130], g[3]], [5, 6, 7, 0]);
  assert.equal(densityGrid(density, 5), null);
  const method = new Uint8Array([0, 255]);
  method.pad = true;
  const u = (a, b) => new Uint32Array([a, b]);
  const cells = readCells(encode('SCVC', [2, 1, 8], [new Float32Array([1, 2]), new Float32Array([3, 4]), new Float32Array([50, NaN]), new Float32Array([20, 30]), u(10, 11), u(12, 13), u(1, 0), u(0, 2), u(7, 8), u(5, 6), method]));
  assert.equal(cells.n, 2);
  assert.deepEqual([...cells.method], [0, 255]);
  assert.deepEqual([...cells.genes], [7, 8]);
  assert.deepEqual([...cells.nuclear], [5, 6]);
  assert.deepEqual([...cells.controlCodewords], [0, 2]);
  assert.ok(Number.isNaN(cells.area[1]));
  assert.equal(nearestCell(cells, 1.2, 3.1, 1), 0);
  assert.equal(nearestCell(cells, 10, 10, 1), -1);
  assert.throws(() => readPoints(encode('SCVD', [0, 0, 0], [new Uint32Array([0])])));
  assert.equal(points.probability, null);
  // A segmentation's points carry each one's probability after the genes.
  const withP = readPoints(encode('SCVP', [3, 2, 1], [new Uint32Array([0, 1, 3]), new Float32Array([1, 2, 3]), new Float32Array([4, 5, 6]), new Uint32Array([7, 8, 9]), genes, new Float32Array([0.5, NaN, 1])]));
  assert.equal(withP.probability[0], 0.5);
  assert.ok(Number.isNaN(withP.probability[1]));
  assert.deepEqual([...withP.cell], [7, 8, 9]);
});

test('16-bit samples are joined from their byte planes', () => {
  const split = new Uint8Array([0x34, 0xff, 0x12, 0x00]);
  assert.deepEqual([...joinSamples(split, 2, 1)], [0x1234, 0x00ff]);
  assert.throws(() => joinSamples(split, 2, 2));
});

test('attribute ranges ignore missing values and outliers', () => {
  const values = Float32Array.from({ length: 1001 }, (_, i) => i);
  values[3] = NaN;
  const [lo, hi] = attributeRange(values);
  assert.ok(lo >= 9 && lo <= 11 && hi >= 989 && hi <= 991);
  assert.deepEqual(attributeRange([NaN]), [0, 1]);
  assert.deepEqual(attributeRange([5, 5]), [5, 6]);
});
