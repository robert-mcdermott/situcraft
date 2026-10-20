import assert from 'node:assert/strict';
import test from 'node:test';
import { TILE, readerHTML, tilePlan, tilesOf, viewExtent } from './view-reader.js';

const VIEW = { id: 'v-1', name: 'Lung classes', camera: { cx: 500, cy: 300, umPerCss: 0.4 } };

test('a view\'s region: what the Viewer showed of it, and every figure panel\'s showing it', () => {
  assert.deepEqual(viewExtent(VIEW), { x0: 300, y0: 175, x1: 700, y1: 425 });
  assert.deepEqual(viewExtent(VIEW, [{ region: { cx: 800, cy: 300, width: 300 }, aspect: 0.5 }]), { x0: 300, y0: 175, x1: 950, y1: 425 });
});

test('its pyramid: from the view\'s own scale, halving µm a pixel to the image\'s full resolution, within the tiles allowed', () => {
  const extent = viewExtent(VIEW);
  const plan = tilePlan(extent, { umPerPx: 0.4, fullRes: 0.2125 });
  assert.deepEqual(plan.map((l) => [l.z, l.umPerPx, l.width, l.height, l.cols, l.rows]), [[0, 0.4, 1000, 625, 2, 2], [1, 0.2, 2000, 1250, 4, 3]]);
  // A finer image: as deep as allowed; fewer tiles allowed: fewer levels.
  assert.equal(tilePlan(extent, { umPerPx: 0.4, fullRes: 0.05 }).length, 3);
  assert.equal(tilePlan(extent, { umPerPx: 0.4, fullRes: 0.05, maxTiles: 10 }).length, 1);
  // A whole section: its coarsest level at most 2048 pixels across.
  const wide = tilePlan({ x0: 0, y0: 0, x1: 7000, y1: 4000 }, { umPerPx: 7, fullRes: 0.2125 });
  assert.ok(wide[0].width <= 2048 && wide.reduce((n, l) => n + l.cols * l.rows, 0) <= 64);
  // Each tile's region and file.
  const tiles = tilesOf(extent, plan, 'views/1-lung');
  assert.equal(tiles.length, 16);
  assert.deepEqual(tiles[0].region, { cx: 300 + 0.5 * TILE * 0.4, cy: 175 + 0.5 * TILE * 0.4, width: TILE * 0.4 });
  assert.equal(tiles.at(-1).path, 'views/1-lung/1/3-2.jpg');
});

test('its reader: one page, nothing fetched but its tiles beside it, its legend and scale bar its own', () => {
  const extent = viewExtent(VIEW);
  const html = readerHTML({ title: 'Lung classes <1>', dataset: 'Sample 1', project: 'Lung', software: '0.8.0', extent, levels: tilePlan(extent, { umPerPx: 0.4, fullRes: 0.2125 }), dir: '1-lung', legend: [{ group: 'Stains', name: 'DAPI', kind: 'square', color: '#4f7cff' }, { group: 'Transcripts', name: 'EPCAM', kind: 'dot', color: '#e6194b' }, { group: 'Cells', name: 'Cell area', kind: 'scale', colors: ['#000000', '#ffffff'], min: 10, max: 150 }], regions: [{ name: 'Tumor nest', color: 'rgb(230, 25, 75)' }], back: '../index.html' });
  assert.match(html, /<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self' file: data:;/);
  assert.match(html, /<title>Lung classes &lt;1&gt;<\/title>/);
  assert.doesNotMatch(html, /\s(?:src|href)="(?:https?:)?\/\//);
  assert.match(html, /<a href="\.\.\/index\.html">← The report<\/a>/);
  assert.match(html, /A saved view of Sample 1, 400 µm across, from the project Lung; drawn by SituCraft 0\.8\.0 in 2 levels, to 0\.200 µm a pixel/);
  for (const name of ['DAPI', 'EPCAM', 'Cell area', 'Tumor nest']) assert.ok(html.includes(name), name);
  assert.match(html, /\(function runReader\(doc, data\)/);
  assert.match(html, /"dir":"1-lung"/);
  assert.doesNotMatch(html.split('<script>')[1], /<\/script[\s\S]*<\/script/);
});
