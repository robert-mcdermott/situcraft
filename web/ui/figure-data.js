// What a figure's panels are drawn from (wave 8, slice 1), gathered from the project and the
// program: a tissue panel's saved view read as the Viewer reads it (its channels, genes,
// segmentation and fill; lib/views.js), its stains' samples at the level its output's pixels
// need, its cells' outlines from their full-resolution polygons, their fill's colors (the same
// functions the Viewer colors them by), and its genes' transcripts, all in its region. Then
// lib/figure-tissue.js draws them. The Viewer need not be open: a figure is drawn from the project
// alone, here, by remote control, and (wave 8, slice 6) in a headless page that checks it.

import { viewLayers } from '../lib/views.js';
import { defaultLayers, cellFillColors, FILLS } from './viewer-panel.js';
import { readCells, readPoints, attributeRange } from '../lib/viewer-data.js';
import { readPolygonTile } from '../lib/polygons.js';
import { readSegCells, SEG_FILLS } from '../lib/segview.js';
import { gridTileRange, imageLevel, tileUm } from '../lib/viewport.js';
import { categoricalColor, colormapColor } from '../lib/colormaps.js';
import { clusterColors, clusterNames, mergedOf, nameOf } from '../lib/clusterings.js';
import { labelColor, labelColors, parseLabelFill } from '../lib/labels.js';
import { labelSource, perCell, refOf } from './label-sources.js';
import { panelBox } from '../lib/figures.js';
import { rasterSize, tissueLayout, compositeStains } from '../lib/figure-tissue.js';

const OUTLINE = { color: '#e8e8e8', width: 0.25, opacity: 0.75 };
// More points than this are not drawn: a figure of hundreds of thousands of dots is not read.
export const MAX_POINTS = 250000;

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response.json();
}

async function getBuffer(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return { buffer: await response.arrayBuffer(), headers: response.headers };
}

// The bundle a dataset is read from on this computer, or an error saying it is not found.
function bundleItem(app, dataset) {
  const link = app.bundles.link(dataset.id);
  if (link?.state === 'missing' || link?.state === 'different-run') throw new Error(`${dataset.name}'s bundle is not where the project records it: locate it in the Data view.`);
  const item = link?.item ?? app.bundles.itemByPath(dataset.path);
  if (!item) throw new Error(`${dataset.name}'s bundle is not open on this computer.`);
  return item;
}

// The cells a view draws: the dataset's own (the instrument's) or a segmentation's, with where its
// outline tiles and points are.
async function cellsOf(item, desc, segmentation) {
  if (!segmentation || segmentation === 'instrument') {
    const { buffer } = await getBuffer(`${item.url}/cells`);
    return { table: readCells(buffer), polygonsBase: desc.polygonsBase ?? `/api/cache/${desc.entries.geometry}/polygons`, geometry: desc.geometry, points: `/api/cache/${desc.entries.transcripts}/points` };
  }
  const base = `${item.url}/segmentations/${encodeURIComponent(segmentation)}`;
  const [out, { buffer }] = await Promise.all([getJSON(`${base}/outlines`), getBuffer(`${base}/cells`)]);
  return { table: readSegCells(buffer), polygonsBase: out.polygonsBase, geometry: out.geometry, points: `${base}/points` };
}

// The fill's colors per cell and what the legend says of them: { rgba, name, category (each
// cell's, or null), categories: [{ name, color }] or null, scale: { name, colors, min, max } or
// null }, or { note } for a fill figures do not draw yet.
async function fillOf(app, project, dataset, cells, desc, fill) {
  if (!fill) return null;
  const labels = parseLabelFill(fill);
  if (labels) {
    const src = await labelSource(app, refOf(labels.id, labels.level));
    if (src.segCells !== cells.n) return { note: `The labels are of another segmentation's ${src.segCells} cells; ${cells.n} are drawn.` };
    const sets = src.names.map((name, k) => ({ name, color: src.colors[k] }));
    const per = perCell(src, sets.length);
    const categories = [...sets.map((s, k) => ({ name: s.name, color: labelColor(k, sets.length, sets) })), { name: 'Unlabeled', color: labelColor(sets.length, sets.length, sets) }];
    return { rgba: labelColors(per, sets.length, sets), name: src.name ? `the label set "${src.name}"` : 'labels', category: per, categories };
  }
  if (fill.startsWith('clusters:')) {
    const id = fill.slice('clusters:'.length);
    const data = await app.bundles.clusteringData(id);
    if (data.cluster.length !== cells.n) return { note: `The clustering is of another segmentation's ${data.cluster.length} cells; ${cells.n} are drawn.` };
    const names = clusterNames(project, id);
    const merged = mergedOf(names, data.sizes.length);
    const categories = data.sizes.map((_, k) => ({ name: nameOf(names, k), color: categoricalColor(merged[k]) }));
    return { rgba: clusterColors(data.cluster, (k) => merged[k]), name: 'clusters', category: Int32Array.from(data.cluster, (k) => (merged[k] ?? k)), categories };
  }
  const spec = (cells.kind === 'segmentation' ? SEG_FILLS : FILLS).find((f) => f.id === fill);
  if (!spec || spec.scale || spec.id === 'limitations' || fill === 'review') return { note: `Cells filled by ${spec?.label?.toLowerCase() ?? fill} are not drawn in figures yet: they are drawn unfilled.` };
  const rgba = cellFillColors(fill, cells, desc);
  if (!rgba) return { note: `The fill ${fill} is not the cells'.` };
  if (spec.categorical) {
    const methods = desc.cells.methods ?? [];
    return { rgba, name: spec.label.toLowerCase(), category: Int32Array.from(cells.method, (m) => (methods[m] ? m : -1)), categories: methods.map((m, i) => ({ name: m.replace(/^Segmented /, ''), color: categoricalColor(i) })) };
  }
  const [lo, hi] = attributeRange(cells[spec.column]);
  const r3 = (v) => Number(v.toPrecision(3));
  return { rgba, name: spec.label.replace(/^./, (c) => c.toLowerCase()), category: null, categories: null, scale: { name: spec.label, colors: Array.from({ length: 16 }, (_, i) => colormapColor('viridis', i / 15)), min: r3(lo), max: r3(hi) } };
}

// The cells' full-resolution rings around a region: [{ cell (its row, or -1), xy }] in µm, each
// polygon once (a level's tiles hold each once).
async function ringsAround(cells, set, grid, region) {
  const info = cells.geometry?.find((s) => s.name === set);
  if (!info) return [];
  const reach = info.maxExtent ?? 0;
  const range = gridTileRange(grid, 0, { x0: region.x0 - reach, y0: region.y0 - reach, x1: region.x1 + reach, y1: region.y1 + reach });
  const tiles = [];
  for (let ty = range.ty0; ty <= range.ty1; ty += 1) for (let tx = range.tx0; tx <= range.tx1; tx += 1) tiles.push([tx, ty]);
  const read = await Promise.all(tiles.map(async ([tx, ty]) => {
    const { buffer, headers } = await getBuffer(`${cells.polygonsBase}/${set}/0/${tx}/${ty}`);
    return readPolygonTile(buffer, Number(headers.get('X-Polygons')), Number(headers.get('X-Vertices')));
  }));
  const rings = [];
  for (const tile of read) {
    for (let p = 0; p < tile.cells.length; p += 1) {
      const start = tile.starts[p];
      const end = tile.starts[p + 1];
      const xy = new Float64Array(2 * (end - start));
      for (let i = start; i < end; i += 1) {
        xy[2 * (i - start)] = tile.xy[2 * i];
        xy[2 * (i - start) + 1] = tile.xy[2 * i + 1];
      }
      rings.push({ cell: tile.cells[p] === 0xffffffff ? -1 : tile.cells[p], xy });
    }
  }
  return rings;
}

// A channel's samples covering a region at a level, as one grid: { x0, y0, um, cols, rows, data }.
async function stainGrid(desc, channel, level, region) {
  const grid = desc.grid;
  const um = grid.pixelSize * 2 ** level;
  const range = gridTileRange(grid, level, region);
  const side = grid.tile;
  const cols = (range.tx1 - range.tx0 + 1) * side;
  const rows = (range.ty1 - range.ty0 + 1) * side;
  const data = new Uint16Array(cols * rows);
  const jobs = [];
  for (let ty = range.ty0; ty <= range.ty1; ty += 1) {
    for (let tx = range.tx0; tx <= range.tx1; tx += 1) {
      jobs.push(getBuffer(`/api/cache/${desc.entries.image}/image/${channel}/${level}/${tx}/${ty}`).then(({ buffer, headers }) => {
        const w = Number(headers.get('X-Tile-Width'));
        const h = Number(headers.get('X-Tile-Height'));
        const bytes = new Uint8Array(buffer);
        if (bytes.length !== 2 * w * h) throw new Error(`a stain tile of ${bytes.length} bytes for ${w} × ${h}`);
        const ox = (tx - range.tx0) * side;
        const oy = (ty - range.ty0) * side;
        for (let y = 0; y < h; y += 1) {
          for (let x = 0; x < w; x += 1) data[(oy + y) * cols + ox + x] = bytes[y * w + x] + 256 * bytes[w * h + y * w + x];
        }
      }));
    }
  }
  await Promise.all(jobs);
  return { x0: range.tx0 * tileUm(grid, level), y0: range.ty0 * tileUm(grid, level), um, cols, rows, data };
}

// The chosen genes' transcripts in a region: { x, y, gene (each one's slot among the genes) }.
async function pointsIn(cells, grid, region, genes, minQ) {
  const range = gridTileRange(grid, 0, region);
  const slot = new Map(genes.map((g, i) => [g, i]));
  const query = `genes=${genes.join(',')}&minq=${minQ}`;
  const tiles = [];
  for (let ty = range.ty0; ty <= range.ty1; ty += 1) for (let tx = range.tx0; tx <= range.tx1; tx += 1) tiles.push([tx, ty]);
  const read = await Promise.all(tiles.map(([tx, ty]) => getBuffer(`${cells.points}/${tx}/${ty}?${query}`).then(({ buffer }) => readPoints(buffer))));
  const x = [];
  const y = [];
  const gene = [];
  for (const p of read) {
    for (let k = 0; k < p.genes.length; k += 1) {
      const s = slot.get(p.genes[k]);
      if (s === undefined) continue;
      for (let i = p.starts[k]; i < p.starts[k + 1]; i += 1) {
        if (p.x[i] < region.x0 || p.x[i] > region.x1 || p.y[i] < region.y0 || p.y[i] > region.y1) continue;
        x.push(p.x[i]);
        y.push(p.y[i]);
        gene.push(s);
      }
    }
  }
  return { x: Float32Array.from(x), y: Float32Array.from(y), gene: Uint16Array.from(gene) };
}

function ringMeets(xy, region) {
  for (let i = 0; i < xy.length; i += 2) if (xy[i] >= region.x0 && xy[i] <= region.x1 && xy[i + 1] >= region.y0 && xy[i + 1] <= region.y1) return true;
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (let i = 0; i < xy.length; i += 2) {
    x0 = Math.min(x0, xy[i]);
    x1 = Math.max(x1, xy[i]);
    y0 = Math.min(y0, xy[i + 1]);
    y1 = Math.max(y1, xy[i + 1]);
  }
  return x1 >= region.x0 && x0 <= region.x1 && y1 >= region.y0 && y0 <= region.y1;
}

// A tissue panel's data (lib/figure-tissue.js tissuePanel) for a figure, its stains at dpi dots
// an inch. Throws when its view or dataset cannot be read.
export async function tissuePanelData(app, fig, panel, { dpi = 300 } = {}) {
  const project = app.store.project;
  const view = (project.views ?? []).find((v) => v.id === panel.view);
  if (!view) throw new Error(`The saved view ${panel.view} is not in the project.`);
  const dataset = project.datasets.find((d) => d.id === view.dataset);
  if (!dataset) throw new Error(`The saved view "${view.name}" is of a dataset no longer in the project.`);
  const item = bundleItem(app, dataset);
  const desc = await getJSON(`${item.url}/view`);
  const { layers, missing } = viewLayers(view, desc, defaultLayers(desc));
  const notes = missing.length ? [`${dataset.name} has no ${missing.join(', ')}.`] : [];
  const genesAll = desc.transcripts?.genes ?? [];
  const channels = layers.channels.map((c, i) => ({ ...c, name: desc.channels[i]?.name ?? `channel ${i}`, index: i })).filter((c) => c.visible);
  const t = layers.transcripts;
  const genes = t.visible && !t.all ? t.genes : [];
  if (t.visible && t.all) notes.push('All transcripts are not drawn in figures: choose genes in the view.');
  if (t.visible && t.colorBy === 'probability') notes.push('The transcripts are drawn by gene, not by their assignment\'s probability.');
  const c = layers.cells;
  const drawCells = c.outlines || c.nuclei || c.fill;
  const cells = drawCells ? await cellsOf(item, desc, view.segmentation) : null;
  let fill = null;
  if (cells && c.fill) {
    fill = await fillOf(app, project, dataset, cells.table, desc, c.fill);
    if (fill?.note) {
      notes.push(fill.note);
      fill = null;
    }
  }
  const box = panelBox(fig, panel);
  // The legend: the stains, the genes, and the fill's categories among the cells drawn (found
  // over the region the legend leaves, which the legend's own size sets: settled in a few rounds).
  const baseItems = [
    ...channels.map((ch) => ({ group: 'Stains', name: ch.name, kind: 'square', color: ch.color })),
    ...genes.map((g, i) => ({ group: 'Transcripts', name: genesAll[g]?.name ?? `gene ${g}`, kind: 'dot', color: categoricalColor(i) })),
  ];
  let rings = [];
  let items = baseItems;
  let layout = tissueLayout(box, panel, items, fig.text);
  if (cells) {
    // Every ring the largest region could show, then those the settled region shows.
    rings = await ringsAround(cells, 'cells', desc.grid, layout.region);
    for (let round = 0; round < 3; round += 1) {
      const fillItems = [];
      if (fill?.categories) {
        const present = new Set();
        for (const r of rings) if (r.cell >= 0 && ringMeets(r.xy, layout.region) && fill.category[r.cell] >= 0) present.add(fill.category[r.cell]);
        // Labels' unlabeled cells are their set's last category.
        fill.categories.forEach((cat, k) => {
          if (present.has(k)) fillItems.push({ group: 'Cells', name: cat.name, kind: 'square', color: cat.color });
        });
      } else if (fill?.scale) fillItems.push({ group: 'Cells', name: fill.scale.name, kind: 'scale', colors: fill.scale.colors, min: fill.scale.min, max: fill.scale.max });
      const next = [...baseItems, ...fillItems];
      const nextLayout = tissueLayout(box, panel, next, fig.text);
      const same = JSON.stringify(next) === JSON.stringify(items);
      items = next;
      layout = nextLayout;
      if (same) break;
    }
  }
  const region = layout.region;
  const raster = rasterSize(layout.image, dpi);
  let stains = null;
  if (channels.length) {
    const umPerPixel = (region.x1 - region.x0) / raster.width;
    const level = imageLevel(desc.grid, { scale: 1 / umPerPixel });
    const grids = await Promise.all(channels.map((ch) => stainGrid(desc, ch.index, level, region)));
    const rgba = compositeStains({ width: raster.width, height: raster.height, region, channels: channels.map((ch, i) => ({ window: ch.window, gamma: ch.gamma, color: ch.color, grid: grids[i] })) });
    stains = { width: raster.width, height: raster.height, rgba, level };
  }
  let transcripts = null;
  if (genes.length) {
    const p = await pointsIn(cells ?? { points: `/api/cache/${desc.entries.transcripts}/points` }, desc.grid, region, genes, t.minQ ?? 20);
    if (p.x.length > MAX_POINTS) notes.push(`The region holds ${p.x.length.toLocaleString('en-US')} transcripts of the genes chosen, more than a figure draws (${MAX_POINTS.toLocaleString('en-US')}): show a smaller region or fewer genes.`);
    else transcripts = { ...p, genes: genes.map((g, i) => ({ name: genesAll[g]?.name ?? `gene ${g}`, color: categoricalColor(i) })) };
  }
  const nuclei = cells && c.nuclei ? (await ringsAround({ ...cells, polygonsBase: desc.polygonsBase ?? `/api/cache/${desc.entries.geometry}/polygons`, geometry: desc.geometry }, 'nuclei', desc.grid, region)) : null;
  return {
    dataset: dataset.name,
    view: view.name,
    background: '#000000',
    legend: items,
    channels: channels.map((ch) => ch.name),
    stains,
    cells: cells && (c.outlines || fill) ? { rings, fill: fill?.rgba ?? null, fillName: fill?.name ?? null, opacity: c.opacity ?? 0.55, outline: c.outlines ? OUTLINE : null } : null,
    nuclei: nuclei ? { rings: nuclei, outline: { color: '#6fa8ff', width: 0.25, opacity: 0.75 } } : null,
    transcripts,
    notes,
    source: { dataset: dataset.id, level: stains?.level ?? null, raster },
  };
}

// Every panel's data of a figure: Map(panel id → data, or { missing: why }).
export async function resolveFigure(app, fig, options = {}) {
  const out = new Map();
  for (const panel of fig.panels) {
    if (panel.kind !== 'tissue') continue;
    try {
      out.set(panel.id, await tissuePanelData(app, fig, panel, options));
    } catch (error) {
      out.set(panel.id, { missing: error.message });
    }
  }
  return out;
}
