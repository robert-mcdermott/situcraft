// The cells a spatial analysis's number comes from, shown on the tissue (app.showSpatial): an
// enrichment's pair, its cells with a neighbor of the other label and the edges between them drawn
// in color over the graph; a co-occurrence's label near another, the cells of each that near. Used
// by the Spatial view and by remote control.

import { formatCount } from './dom.js';
import { readCells } from '../lib/viewer-data.js';
import { readSegCells } from '../lib/segview.js';
import { nearCells, pairCells } from '../lib/spatial.js';

const pairName = (names, a, b) => `${names[a]} – ${names[b]}`;

// Shows them: { dataset (id), item (its bundle), segmentation (the record), analysis (analysis.json),
// cells (its cells), a, b (label indices), k (a co-occurrence's distance) }. Answers what is shown:
// { title, text, cells }.
export async function showAnalysisCells(app, { dataset, item, segmentation, analysis, cells, a, b, k = 0 }) {
  const names = analysis.labels.names;
  const mask = new Uint8Array(analysis.cells.segmentation);
  const fill = `labels:${analysis.labels.id}:${analysis.labels.level}`;
  let focus;
  let graph;
  if (analysis.question === 'enrichment') {
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
    const co = analysis.cooccurrence;
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
    focus = { title: `${names[b]} near ${names[a]}`, text: `${formatCount(near.b)} ${names[b]} cells ${dmin > 0 ? `${dmin} to ${d}` : `within ${d}`} µm of ${formatCount(near.a)} ${names[a]} cells`, mask };
  }
  app.showSpatial({ dataset, segmentation: segmentation.id, fill, graph, focus });
  return { title: focus.title, text: focus.text, cells: mask.reduce((n, v) => n + v, 0) };
}
