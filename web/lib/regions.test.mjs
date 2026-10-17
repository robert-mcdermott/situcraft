// Regions (lib/regions.js): their shapes, the brush, their versions, and GeoJSON as QuPath writes
// and reads it.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addRegion, area, brushGeometry, cellsIn, classColor, containsPoint, editRegion, fromGeoJSON, nextRegionName, rectangle, regionsOf, toGeoJSON, uniqueRegionName } from './regions.js';

const near = (a, b, tol) => Math.abs(a - b) <= tol;

test('a polygon with a hole: its area and the points in it', () => {
  const g = [[[[0, 0], [10, 0], [10, 10], [0, 10]], [[4, 4], [6, 4], [6, 6], [4, 6]]]];
  assert.equal(area(g), 96);
  assert.ok(containsPoint(g, 1, 1) && !containsPoint(g, 5, 5) && !containsPoint(g, 11, 5));
  const cells = { n: 4, x: Float32Array.from([1, 5, 9.5, 20]), y: Float32Array.from([1, 5, 9.5, 20]) };
  assert.deepEqual([...cellsIn(g, cells)], [0, 2]);
});

test('the brush: a dab is a disc, a stroke a capsule, erasing makes a hole', () => {
  const r = 5;
  const dab = brushGeometry(null, [[20, 20]], r);
  assert.equal(dab.length, 1);
  // The outline found between samples, where the brush's edge crosses them; its chords cut the
  // curve: within 1%.
  assert.ok(near(area(dab), Math.PI * r * r, 0.01 * Math.PI * r * r), `a dab: ${area(dab)}`);
  const stroke = brushGeometry(null, [[0, 0], [30, 0], [30, 40]], r);
  // Two segments: their rectangles and a disc, less the inner corner the rectangles both cover
  // beyond the disc's quarter (a square less a quarter disc).
  const want = 2 * r * 70 + Math.PI * r * r - (r * r - (Math.PI * r * r) / 4);
  assert.ok(near(area(stroke), want, 0.01 * want), `a stroke: ${area(stroke)}`);
  const square = rectangle(0, 0, 40, 40);
  const holed = brushGeometry(square, [[20, 20]], r, true);
  assert.equal(holed.length, 1);
  assert.equal(holed[0].length, 2, 'an outer ring and a hole');
  assert.ok(near(1600 - area(holed), Math.PI * r * r, 0.01 * Math.PI * r * r), `holed: ${area(holed)}`);
  assert.ok(!containsPoint(holed, 20, 20) && containsPoint(holed, 5, 5));
  // A slanted region re-traced keeps its edges where they were (near them, the samples are its
  // distance from them): a hole erased takes the brush's disc from it, within 0.5%.
  const slanted = [[[[250, 180], [300, 190], [290, 240], [240, 230]]]];
  assert.ok(near(2600 - area(brushGeometry(slanted, [[270, 210]], 8, true)), Math.PI * 64, 0.005 * Math.PI * 64));
  // Painting across the square's edge grows it; two dabs apart are two polygons.
  assert.ok(area(brushGeometry(square, [[40, 20]], r)) > 1600 + 0.4 * Math.PI * r * r);
  assert.equal(brushGeometry(null, [[0, 0]], 2).length + brushGeometry(null, [[50, 0]], 2).length, 2);
  assert.equal(brushGeometry(brushGeometry(null, [[0, 0]], 2), [[50, 0]], 2).length, 2);
});

test('the brush changes a region only near its stroke, at its own step however large the region', () => {
  // A 10 × 7 mm outline of 3,000 points, its edge painted with a 6 µm brush.
  const outline = Array.from({ length: 3000 }, (_, k) => [5000 + 5000 * Math.cos((k * Math.PI) / 1500), 3500 + 3500 * Math.sin((k * Math.PI) / 1500)]);
  const big = [[outline]];
  const painted = brushGeometry(big, [[10000, 3500], [10010, 3520], [10020, 3530]], 6);
  assert.equal(painted.length, 1);
  // Away from the stroke, the outline's own points, exactly.
  const kept = new Set(painted[0][0].map(([x, y]) => `${x},${y}`));
  const far = outline.filter(([x, y]) => Math.hypot(x - 10010, y - 3520) > 60);
  assert.ok(far.every(([x, y]) => kept.has(`${x},${y}`)), 'the far points kept');
  // Painted within it, nothing changes; outside it, a disc's area is added (its trace 0.5% small).
  assert.equal(area(brushGeometry(big, [[9900, 3500]], 6)), area(big));
  const added = area(brushGeometry(big, [[11000, 3500]], 6)) - area(big);
  assert.ok(near(added, Math.PI * 36, 0.01 * Math.PI * 36), `added ${added}`);
  // Erased across, a rectangle is two, each with its own corners.
  const cut = brushGeometry(rectangle(0, 0, 100, 40), [[50, -10], [50, 50]], 5, true);
  assert.equal(cut.length, 2);
  assert.ok(near(area(cut), 3600, 1));
  assert.ok(cut.some((p) => p[0].some(([x, y]) => x === 0 && y === 0)) && cut.some((p) => p[0].some(([x, y]) => x === 100 && y === 40)));
});

test('a region\'s versions: drawn, renamed, reshaped, removed and returned, each with who and when', () => {
  let p = { datasets: [{ id: 'd1', name: 'Rep1' }], artifacts: [], history: [], decisions: [], views: [] };
  assert.equal(nextRegionName(p, 'd1', 'Tumor'), 'Tumor 1');
  p = addRegion(p, { dataset: 'd1', name: '', cls: 'Tumor', geometry: rectangle(0, 0, 10, 20), tool: 'rectangle', by: 'RM', now: 't1', id: 'r-1' });
  assert.equal(regionsOf(p, 'd1')[0].name, 'Tumor 1');
  assert.deepEqual(regionsOf(p, 'd1')[0].color, [200, 0, 0]);
  p = editRegion(p, 'r-1', { name: 'Invasive front', cls: 'Stroma' }, { by: 'AB', now: 't2', note: 'on review' });
  p = editRegion(p, 'r-1', { geometry: rectangle(0, 0, 10, 10) }, { by: 'AB', now: 't3', tool: 'brush' });
  let r = regionsOf(p, 'd1')[0];
  assert.deepEqual([r.version, r.name, r.cls, area(r.geometry), r.tool, r.by], [3, 'Invasive front', 'Stroma', 100, 'brush', 'AB']);
  assert.deepEqual(r.color, classColor('Stroma'));
  p = editRegion(p, 'r-1', { removed: true }, { now: 't4' });
  assert.equal(regionsOf(p, 'd1').length, 0);
  assert.equal(regionsOf(p, 'd1', { removed: true })[0].version, 4);
  p = editRegion(p, 'r-1', { removed: false }, { now: 't5' });
  r = regionsOf(p, 'd1')[0];
  assert.equal(r.version, 5);
  assert.equal(r.versions[0].name, 'Tumor 1', 'the first version kept');
  assert.match(p.history[1].detail, /Changed Tumor 1 \(named Invasive front, classed Stroma\), version 2: on review/);
  assert.throws(() => addRegion(p, { dataset: 'd1', geometry: [], tool: 'polygon' }), /needs an area/);
  // A region is found by its name: no two of a dataset's have one; a file's repeated names numbered.
  assert.throws(() => addRegion(p, { dataset: 'd1', name: 'Invasive front', geometry: rectangle(0, 0, 1, 1), tool: 'rectangle' }), /Another region of this dataset is named Invasive front/);
  p = addRegion(p, { dataset: 'd1', name: 'Edge', geometry: rectangle(0, 0, 1, 1), tool: 'rectangle', id: 'r-2' });
  assert.throws(() => editRegion(p, 'r-2', { name: 'Invasive front' }), /Another region/);
  assert.equal(uniqueRegionName(p, 'd1', 'Edge'), 'Edge (2)');
  assert.equal(uniqueRegionName(p, 'd1', 'Margin'), 'Margin');
  assert.equal(nextRegionName(p, 'd1', 'Tumor'), 'Tumor 2');
});

const regionsAt = [
  { id: 'r-a', version: 2, name: 'Tumor 1', cls: 'Tumor', color: [200, 0, 0], by: 'RM', geometry: [[[[10, 10], [110, 10], [110, 60], [10, 60]], [[40, 20], [60, 20], [60, 40], [40, 40]]]] },
  { id: 'r-b', version: 1, name: 'Two pieces', cls: 'Lymphoid aggregate', color: classColor('Lymphoid aggregate'), by: 'RM', geometry: [[[[200, 200], [250, 200], [225, 240]]], [[[300, 300], [320, 300], [320, 320], [300, 320]]]] },
];

test('regions written as QuPath writes annotations, and read back the same', () => {
  const doc = toGeoJSON(regionsAt);
  assert.equal(doc.type, 'FeatureCollection');
  const [a, b] = doc.features;
  assert.equal(a.geometry.type, 'Polygon');
  assert.deepEqual(a.geometry.coordinates[0][0], a.geometry.coordinates[0].at(-1), 'rings closed');
  assert.deepEqual(a.properties.classification, { name: 'Tumor', color: [200, 0, 0] });
  assert.equal(a.properties.objectType, 'annotation');
  assert.match(a.id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$/);
  assert.equal(b.geometry.type, 'MultiPolygon');
  const back = fromGeoJSON(JSON.parse(JSON.stringify(doc)));
  assert.deepEqual(back.skipped, {});
  back.regions.forEach((r, k) => {
    assert.deepEqual([r.name, r.cls, r.color, r.source], [regionsAt[k].name, regionsAt[k].cls, regionsAt[k].color, regionsAt[k].id]);
    assert.deepEqual(r.geometry, regionsAt[k].geometry);
  });
});

test('QuPath\'s layouts read: 0.2\'s array and packed colours, 0.3\'s objectType, 0.4\'s colours; detections and points left out', () => {
  const v02 = [{ type: 'Feature', id: 'PathAnnotationObject', geometry: { type: 'Polygon', coordinates: [[[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]]] }, properties: { classification: { name: 'Stroma', colorRGB: -6895466 }, isLocked: false, measurements: [] } },
    { type: 'Feature', id: 'PathDetectionObject', geometry: { type: 'Polygon', coordinates: [[[1, 1], [2, 1], [2, 2], [1, 1]]] }, properties: { isLocked: false } }];
  const r02 = fromGeoJSON(v02);
  assert.equal(r02.regions.length, 1);
  assert.deepEqual(r02.regions[0].color, [150, 200, 150]);
  assert.deepEqual(r02.skipped, { detection: 1 });
  const v04 = { type: 'FeatureCollection', features: [
    { type: 'Feature', id: '1b6f4c0e-3b1a-4c5e-9a55-0d2c3e4f5a6b', geometry: { type: 'Polygon', coordinates: [[[0, 0], [8, 0], [8, 8], [0, 8], [0, 0]]] }, properties: { objectType: 'annotation', name: 'Hand-drawn', classification: { name: 'Immune cells', color: [160, 90, 160] } } },
    { type: 'Feature', geometry: { type: 'Point', coordinates: [3, 3] }, properties: { objectType: 'annotation' } },
    { type: 'Feature', geometry: { type: 'LineString', coordinates: [[0, 0], [5, 5]] }, properties: { objectType: 'annotation' } },
    { type: 'Feature', geometry: { type: 'Polygon', coordinates: [[[0, 0], [1, 0], [1, 1], [0, 0]]] }, properties: { objectType: 'cell' } },
  ] };
  const r04 = fromGeoJSON(v04);
  assert.deepEqual(r04.regions.map((r) => [r.name, r.cls, area(r.geometry)]), [['Hand-drawn', 'Immune cells', 64]]);
  assert.deepEqual(r04.skipped, { Point: 1, LineString: 1, cell: 1 });
  // The type as some writers spell it.
  assert.deepEqual(fromGeoJSON([{ type: 'Feature', geometry: v04.features[0].geometry, properties: { object_type: 'detection' } }]).skipped, { detection: 1 });
  // An unclassified annotation is a region of no particular class.
  assert.equal(fromGeoJSON({ type: 'Feature', geometry: v04.features[0].geometry, properties: {} }).regions[0].cls, 'Region*');
  assert.throws(() => fromGeoJSON({ type: 'Polygon' }), /not GeoJSON of objects/);
});

test('regions written in an image\'s pixels through its transform, and read back in µm', () => {
  // µm = T(pixels): 0.36 µm a pixel, mirrored, turned, shifted.
  const T = [[0.36 * Math.cos(0.3), 0.36 * Math.sin(0.3), 12], [0.36 * Math.sin(0.3), -0.36 * Math.cos(0.3), 900]];
  const det = T[0][0] * T[1][1] - T[0][1] * T[1][0];
  const inv = (x, y) => {
    const dx = x - T[0][2];
    const dy = y - T[1][2];
    return [(T[1][1] * dx - T[0][1] * dy) / det, (T[0][0] * dy - T[1][0] * dx) / det];
  };
  const fwd = (u, v) => [T[0][0] * u + T[0][1] * v + T[0][2], T[1][0] * u + T[1][1] * v + T[1][2]];
  const doc = toGeoJSON(regionsAt, { toCoords: inv });
  const px = doc.features[0].geometry.coordinates[0][0];
  const [ex, ey] = inv(10, 10);
  assert.ok(near(px[0], ex, 1e-3) && near(px[1], ey, 1e-3), 'in the image\'s pixels');
  const back = fromGeoJSON(doc, { fromCoords: fwd });
  back.regions.forEach((r, k) => r.geometry.forEach((poly, i) => poly.forEach((ring, j) => ring.forEach((p, m) => {
    const want = regionsAt[k].geometry[i][j][m];
    assert.ok(near(p[0], want[0], 1e-3) && near(p[1], want[1], 1e-3), `${p} against ${want}`);
  }))));
  // Densified, a straight edge gains points (for a deformation to bend).
  const dense = toGeoJSON([regionsAt[0]], { toCoords: inv, step: 5 });
  assert.ok(dense.features[0].geometry.coordinates[0].length > 50);
});
