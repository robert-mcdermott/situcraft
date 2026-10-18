// The cells a spatial analysis's number comes from, shown on the tissue (app.showSpatial): an
// enrichment's pair, its cells with a neighbor of the other label and the edges between them drawn
// in color over the graph; a co-occurrence's, nearest distances' or Ripley's label near another, the
// cells of each that near; a label's cells in a region compared, or in bands across a region's
// edge. Used by the Spatial view and by remote control.

import { formatCount } from './dom.js';
import { readCells } from '../lib/viewer-data.js';
import { readSegCells } from '../lib/segview.js';
import { bandCells, nearCells, pairCells, regionCells, rowNames } from '../lib/spatial.js';

const pairName = (names, a, b) => `${names[a]} – ${names[b]}`;

// Shows them: { dataset (id), item (its bundle), segmentation (the record), analysis (analysis.json),
// cells (its cells), a, b (label indices), k (a co-occurrence's distance); for regions compared, a
// (the label) and row; across an edge, a (the label, −1 any) and bands ({ from, to }: band indices)
// with where they are in words }. Answers what is shown: { title, text, cells }.
export async function showAnalysisCells(app, { dataset, item, segmentation, analysis, cells, a, b, k = 0, row = 0, bands = null, where = '' }) {
  const names = analysis.labels.names;
  const mask = new Uint8Array(analysis.cells.segmentation);
  const fill = `labels:${analysis.labels.id}:${analysis.labels.level}`;
  let focus;
  let graph;
  if (analysis.question === 'regions') {
    const rows = rowNames(analysis.regions, true);
    const rc = regionCells(cells.stratum, cells.label, rows.length, row, a);
    for (let i = 0; i < cells.n; i += 1) if (rc.cells[i]) mask[cells.rows[i]] = 1;
    focus = { title: `${names[a]} in ${rows[row]}`, text: `${rc.count.toLocaleString('en-US')} ${names[a]} cells in ${rows[row]}`, mask };
  } else if (analysis.question === 'boundary') {
    const bc = bandCells(cells.stratum, cells.label, bands.from, bands.to, a);
    for (let i = 0; i < cells.n; i += 1) if (bc.cells[i]) mask[cells.rows[i]] = 1;
    const what = a >= 0 ? names[a] : 'labeled';
    focus = { title: `${what} across the edge of ${analysis.boundary.region.name}`, text: `${bc.count.toLocaleString('en-US')} ${what} cells ${where}`, mask };
  } else if (analysis.question === 'enrichment') {
    [a, b] = [Math.min(a, b), Math.max(a, b)];
    const g = (await app.bundles.graphData(analysis.graph.id)).graph;
    const label = cells.label;
    const pc = pairCells(g, label, a, b);
    for (let i = 0; i < g.n; i += 1) if (pc.cells[i]) mask[g.rows[i]] = 1;
    graph = analysis.graph.id;
    focus = {
      title: pairName(names, a, b),
      text: a === b ? `${formatCount(pc.a)} cells with a neighbor of their own label; ${formatCount(pc.edges)} edges` : `${formatCount(pc.a)} ${names[a]} and ${formatCount(pc.b)} ${names[b]} cells neighboring each other; ${formatCount(pc.edges)} edges`,
      mask, emphasis: { test: (i, j) => (label[i] === a && label[j] === b) || (label[i] === b && label[j] === a), color: '#ff4fd8' },
    };
  } else {
    // A label's cells near another's: within a co-occurrence's distance (or since the one before),
    // within a nearest distance, or within one of Ripley's radii.
    const co = analysis.cooccurrence ?? { distances: analysis.nearest?.distances ?? analysis.ripley.radii, pairs: 'within' };
    const url = segmentation.source === 'instrument' ? `${item.url}/cells` : `${item.url}/segmentations/${encodeURIComponent(segmentation.id)}/cells`;
    const response = await fetch(url);
    if (!response.ok) throw new Error(`its cells: HTTP ${response.status}`);
    const buffer = await response.arrayBuffer();
    const table = segmentation.source === 'instrument' ? readCells(buffer) : readSegCells(buffer);
    const x = Array.from(cells.rows, (r) => table.x[r]);
    const y = Array.from(cells.rows, (r) => table.y[r]);
    const d = co.distances[k];
    const dmin = co.pairs === 'between' && k > 0 ? co.distances[k - 1] : -Infinity;
    const near = nearCells(x, y, cells.label, a, b, d, dmin);
    for (let i = 0; i < cells.n; i += 1) if (near.cells[i]) mask[cells.rows[i]] = 1;
    const within = dmin > 0 ? `${dmin} to ${d}` : `within ${d}`;
    focus = { title: `${names[b]} near ${names[a]}`, text: a === b ? `${formatCount(near.a)} ${names[a]} cells with another ${within} µm` : `${formatCount(near.b)} ${names[b]} cells ${within} µm of ${formatCount(near.a)} ${names[a]} cells`, mask };
  }
  app.showSpatial({ dataset, segmentation: segmentation.id, fill, graph, focus });
  return { title: focus.title, text: focus.text, cells: mask.reduce((n, v) => n + v, 0) };
}
