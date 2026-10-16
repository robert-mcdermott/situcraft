// The map and the tissue, side by side and linked: a clustering's cells on a map (lib/scatter.js,
// drawn on a 2-D canvas) and on the tissue (the renderer, the segmentation's outlines), colored
// alike (by a clustering's clusters, a label set's labels at a level, their confidence or one
// label's score, a gene's expression, a cell attribute or the map's reliability), with one
// selection: cells lassoed on either, or chosen by cluster or label, are selected on both. Clicking a cell on the map shows it on the tissue, and clicking a cell on the tissue marks
// it on the map.

import { h, icon, clear, formatCount } from './dom.js';
import { toast } from './overlays.js';
import { TissueRenderer } from '../render/renderer.js';
import { attachNavigation } from './navigation.js';
import { fitCamera, screenToWorld, worldToScreen } from '../lib/viewport.js';
import { defaultLayers } from './viewer-panel.js';
import { categoricalColor, colormapCSS, colormapLUT } from '../lib/colormaps.js';
import { readCells } from '../lib/viewer-data.js';
import { readSegCells } from '../lib/segview.js';
import { fitView, insidePolygon, nearestPoint, pointGrid, pointsInPolygon, rasterize, toPixel, toPlane, valueColors, zoomAt } from '../lib/scatter.js';
import { clusterColors, mapWarning, mergedOf, nameOf } from '../lib/clusterings.js';
import { labelColor, labelColors, labelSetName } from '../lib/labels.js';
import { labelSource, labelVersions, perCell, refOf } from './label-sources.js';

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(body.error ?? `HTTP ${response.status}`), { status: response.status });
  return body;
}

// The colors a map can show besides clusters.
export const ATTRIBUTE_COLORS = [
  { id: 'transcripts', label: 'Gene transcripts', column: 'transcripts' },
  { id: 'area', label: 'Cell area (µm²)', column: 'area' },
];

// ctx: { item, dataset, segmentation (record), instrument (whether it is the instrument's),
// clusterings (records of the segmentation), maps (records), names (clustering id → names record),
// labelSets (label set records of its clusterings), onSelect(mask, count), onFocus(cell) }.
export function mountCellMap(app, host, ctx) {
  const state = {
    ctx, cells: null, desc: null, outlines: null, renderer: null, nav: null, observer: null,
    map: null, mapId: null, grid: null, view: null, color: { kind: 'clusters', clustering: null }, colors: null, legend: null,
    selected: null, count: 0, lasso: false, focus: -1, generation: 0, values: null,
  };
  const mapCanvas = h('canvas.cm-map', { tabIndex: 0, 'aria-label': 'The map of the cells: drag to move, scroll to zoom' });
  const mapOverlay = h('canvas.cm-overlay', { 'aria-hidden': 'true' });
  const tissueCanvas = h('canvas.viewer-canvas.cm-tissue', { tabIndex: 0, 'aria-label': 'The tissue with the cells colored as on the map' });
  const tissueOverlay = h('canvas.cm-overlay', { 'aria-hidden': 'true' });
  const mapStatus = h('div.cm-status.muted');
  const tissueStatus = h('div.cm-status.muted');
  const mapPick = h('select.input.small', { 'aria-label': 'Map' });
  const colorPick = h('select.input.small', { 'aria-label': 'Color the cells by' });
  const geneInput = h('input.input.small.cm-gene', { type: 'search', placeholder: 'A gene…', 'aria-label': 'Color by a gene', list: 'cm-genes', hidden: true });
  const genes = h('datalist', { id: 'cm-genes' });
  const lassoButton = h('button.btn.small', { type: 'button', 'aria-pressed': 'false', title: 'Draw around cells on the map or the tissue to select them (shift adds to the selection)', onclick: () => setLasso(!state.lasso) }, icon('lasso'), 'Lasso');
  const clearButton = h('button.btn.small.ghost', { type: 'button', disabled: true, onclick: () => select(null) }, icon('close'), 'Clear selection');
  const selectionInfo = h('span.muted.cm-selection');
  const legend = h('div.cm-legend');
  const warning = h('p.seg-note.cm-warning', { hidden: true });
  const root = h('div.cell-map',
    h('div.cm-toolbar', h('label.cm-field', h('span', 'Map'), mapPick), h('label.cm-field', h('span', 'Color by'), colorPick), geneInput, genes, h('span.spacer'), selectionInfo, lassoButton, clearButton),
    h('div.cm-panes',
      h('div.cm-pane', h('div.cm-pane-label', 'The map'), mapCanvas, mapOverlay, mapStatus),
      h('div.cm-pane', h('div.cm-pane-label', 'The tissue'), tissueCanvas, tissueOverlay, tissueStatus)),
    legend, warning);
  host.append(root);

  // --- Loading ---
  async function load() {
    const mine = (state.generation += 1);
    const { item, segmentation } = state.ctx;
    tissueStatus.textContent = 'Reading the tissue…';
    try {
      const base = state.ctx.instrument ? null : `${item.url}/segmentations/${encodeURIComponent(segmentation.id)}`;
      const [desc, outlines, cellsResponse] = await Promise.all([
        getJSON(`${item.url}/view`),
        getJSON(`${item.url}/segmentations/${encodeURIComponent(segmentation.id)}/outlines`),
        fetch(base ? `${base}/cells` : `${item.url}/cells`),
      ]);
      if (!cellsResponse.ok) throw new Error(`its cells: HTTP ${cellsResponse.status}`);
      const cells = base ? readSegCells(await cellsResponse.arrayBuffer()) : readCells(await cellsResponse.arrayBuffer());
      if (mine !== state.generation) return;
      state.desc = desc;
      state.outlines = outlines;
      state.cells = cells;
      state.selected = new Uint8Array(cells.n);
      for (const g of desc.transcripts?.genes ?? []) genes.append(h('option', { value: g.name ?? g }));
      makeTissue();
      tissueStatus.textContent = '';
      fillPickers();
      await showMap();
    } catch (error) {
      if (mine !== state.generation) return;
      tissueStatus.textContent = `The tissue could not be drawn: ${error.message}`;
    }
  }

  function makeTissue() {
    state.renderer?.destroy();
    state.nav?.dispose();
    const renderer = new TissueRenderer(tissueCanvas, { onChange: () => drawFocusOnTissue() });
    renderer.setDataset({ ...state.desc, polygonsBase: state.outlines.polygonsBase, geometry: state.outlines.geometry }, { n: state.outlines.cells });
    const layers = defaultLayers(state.desc);
    layers.transcripts.visible = false;
    layers.cells = { outlines: true, nuclei: false, fill: 'cluster', opacity: 0.75 };
    for (const c of layers.channels) c.color = '#8c8c8c';
    renderer.setLayers(layers);
    state.renderer = renderer;
    state.nav = attachNavigation(tissueCanvas, renderer, { onClick: (x, y) => {
      const cell = renderer.pick(x, y);
      if (cell >= 0) focus(cell, { tissue: false });
    } });
    state.observer?.disconnect();
    state.observer = new ResizeObserver(() => resize());
    state.observer.observe(tissueCanvas);
    state.observer.observe(mapCanvas);
  }

  function resize() {
    const box = tissueCanvas.getBoundingClientRect();
    if (box.width > 4 && state.renderer) {
      state.renderer.resize(box.width, box.height);
      tissueOverlay.width = tissueCanvas.width;
      tissueOverlay.height = tissueCanvas.height;
      // The whole tissue, fitted again as the pane settles, until the camera moves.
      const cam = state.renderer.camera;
      const untouched = !state.fitted || (cam.cx === state.fitted.cx && cam.cy === state.fitted.cy && cam.scale === state.fitted.scale);
      if (untouched) {
        const g = state.desc.grid;
        state.fitted = fitCamera({ x0: 0, y0: 0, x1: g.width * g.pixelSize, y1: g.height * g.pixelSize }, ...state.renderer.size(), 0.03);
        state.renderer.setCamera(state.fitted);
      }
    }
    const ratio = window.devicePixelRatio || 1;
    const mb = mapCanvas.getBoundingClientRect();
    if (mb.width > 4) {
      mapCanvas.width = Math.round(mb.width * ratio);
      mapCanvas.height = Math.round(mb.height * ratio);
      mapOverlay.width = mapCanvas.width;
      mapOverlay.height = mapCanvas.height;
      // The whole map, fitted again as the pane settles, until it is moved.
      if (state.map && (!state.view || !state.mapMoved)) state.view = fitView(state.map.xy, state.map.n, mapCanvas.width, mapCanvas.height);
      drawMap();
    }
  }

  function fillPickers() {
    const { maps, clusterings } = state.ctx;
    clear(mapPick).append(...maps.map((m) => h('option', { value: m.id }, m.name)));
    if (!maps.some((m) => m.id === state.mapId)) state.mapId = maps.find((m) => m.clustering === state.ctx.shown)?.id ?? maps[0]?.id ?? null;
    if (state.mapId) mapPick.value = state.mapId;
    mapPick.disabled = maps.length < 2;
    const labelSets = (state.ctx.labelSets ?? []).filter((r) => r.state === 'complete');
    const c = state.color;
    const options = [
      ...clusterings.map((c) => h('option', { value: `clusters:${c.id}` }, `Clusters: ${c.summary?.source ? c.name : `resolution ${c.parameters?.resolution}`} (${c.summary?.clusters})`)),
      ...labelSets.flatMap((r) => [
        ...(r.summary?.levels ?? []).map((level, l) => h('option', { value: `labels:${r.id}:${l}` }, r.source ? `Labels: ${labelSetName(r)}` : `${labelSetName(r)}: ${level.toLowerCase()}`)),
        ...(!r.source || r.source.confidenceColumn ? [h('option', { value: `confidence:${r.id}` }, `${labelSetName(r)}: confidence`)] : []),
      ]),
      ...labelVersions(app.store.project, state.ctx.dataset.id, state.ctx.segmentation.id).filter((v) => v.kind === 'curated' || v.kind === 'consensus').map((v) => h('option', { value: `labels:${v.id}:0` }, `${v.kind === 'curated' ? 'Curated' : 'Consensus'}: ${v.name}`)),
      ...(c.kind === 'score' ? [h('option', { value: `score:${c.labels}:${c.set}` }, `Score: ${c.name ?? 'a label'}`)] : []),
      h('option', { value: 'gene' }, 'A gene…'),
      ...ATTRIBUTE_COLORS.map((a) => h('option', { value: `attribute:${a.id}` }, a.label)),
      h('option', { value: 'reliability' }, 'How faithfully the map keeps neighbors'),
    ];
    clear(colorPick).append(...options);
    if (!state.color.clustering && state.color.kind === 'clusters') state.color.clustering = state.ctx.shown ?? clusterings[0]?.id ?? null;
    colorPick.value = colorValue(state.color);
    geneInput.hidden = state.color.kind !== 'gene';
  }

  // What the shown map's quality says.
  function showWarning() {
    const record = state.ctx.maps.find((m) => m.id === state.mapId);
    const w = mapWarning(record?.summary);
    clear(warning);
    warning.hidden = !w;
    if (!w) return;
    warning.classList.toggle('ct-warning', w.level === 'warning');
    warning.classList.toggle('muted', w.level !== 'warning');
    warning.append(w.level === 'warning' ? icon('warning') : '', ' ', w.text);
  }

  async function showMap() {
    const id = state.mapId;
    showWarning();
    if (!id) {
      state.map = null;
      mapStatus.textContent = 'No map yet: it is made when a clustering is.';
      drawMap();
      await recolor();
      return;
    }
    mapStatus.textContent = 'Reading the map…';
    try {
      const data = await app.bundles.mapData(id);
      if (id !== state.mapId) return;
      if (data.segCells && data.segCells !== state.cells.n) throw new Error(`it is of another segmentation's ${formatCount(data.segCells)} cells`);
      state.map = data;
      state.grid = pointGrid(data.xy, data.n);
      state.mapMoved = false;
      state.view = mapCanvas.width ? fitView(data.xy, data.n, mapCanvas.width, mapCanvas.height) : null;
      mapStatus.textContent = '';
      await recolor();
    } catch (error) {
      mapStatus.textContent = `The map could not be read: ${error.message}`;
    }
  }

  // --- Colors ---
  function colorValue(c) {
    switch (c.kind) {
      case 'clusters': return `clusters:${c.clustering}`;
      case 'attribute': return `attribute:${c.attribute}`;
      case 'labels': return `labels:${c.labels}:${c.level}`;
      case 'confidence': return `confidence:${c.labels}`;
      case 'score': return `score:${c.labels}:${c.set}`;
      default: return c.kind;
    }
  }

  // Values per segmentation cell (NaN for the others) between their 1st and 99th percentiles.
  function percentiles(values) {
    const finite = Float64Array.from([...values].filter(Number.isFinite)).sort();
    if (!finite.length) return [0, 1];
    return [finite[Math.floor(0.01 * (finite.length - 1))], finite[Math.floor(0.99 * (finite.length - 1))]];
  }

  async function recolor() {
    const n = state.cells.n;
    const c = state.color;
    try {
      // The shown clustering's clusters, for selecting by cluster whatever colors the cells.
      const shown = state.ctx.shown;
      if (shown && state.clustersOf !== shown) {
        const data = await app.bundles.clusteringData(shown);
        if (data.cluster.length === n) {
          state.clusters = data.cluster;
          state.clustersOf = shown;
        }
      }
      if (c.kind === 'labels' || c.kind === 'confidence') {
        const src = await labelSource(app, refOf(c.labels, c.level ?? 0));
        if (src.segCells !== n) throw new Error('the labels are of another segmentation');
        const sets = src.names.map((name, k) => ({ name, color: src.colors[k] }));
        if (c.kind === 'labels') {
          const unlabeled = sets.length;
          const per = perCell(src, unlabeled);
          state.colors = labelColors(per, unlabeled, sets);
          state.groups = per;
          const counts = new Map();
          for (const k of per) if (k >= 0) counts.set(k, (counts.get(k) ?? 0) + 1);
          const entries = [...counts.entries()].sort((a, b) => (a[0] === unlabeled) - (b[0] === unlabeled) || b[1] - a[1])
            .map(([k, size]) => ({ name: k === unlabeled ? 'No label' : sets[k].name, color: labelColor(k, unlabeled, sets), cells: size, clusters: [k] }));
          state.legend = { kind: 'clusters', entries, labels: c.labels };
        } else {
          const values = src.confidence ? Float32Array.from(src.confidence) : new Float32Array(n).fill(NaN);
          const [, hi] = percentiles(values);
          state.colors = valueColors(values, colormapLUT('viridis'), 0, hi || 1);
          state.legend = { kind: 'scale', label: src.kind === 'consensus' ? 'The share of the sets agreeing on each cell\'s label' : 'Confidence: how far each cell\'s label leads the next', lo: 0, hi: hi || 1, note: 'up to the 99th percentile; cells below 0 at the lowest' };
        }
      } else if (c.kind === 'score') {
        const { name, values } = await app.bundles.labelScores(c.labels, c.set);
        if (values.length !== n) throw new Error('the labels are of another segmentation');
        const [lo, hi] = percentiles(values);
        state.colors = valueColors(values, colormapLUT('viridis'), lo, hi);
        state.legend = { kind: 'scale', label: `${name}: its markers' score against their controls`, lo, hi, note: '1st to 99th percentile' };
      } else if (c.kind === 'clusters' && c.clustering) {
        const data = await app.bundles.clusteringData(c.clustering);
        if (data.cluster.length !== n) throw new Error('the clustering is of another segmentation');
        const names = state.ctx.names?.[c.clustering] ?? null;
        const merged = mergedOf(names, data.sizes.length);
        state.colors = clusterColors(data.cluster, (k) => merged[k]);
        state.groups = data.cluster;
        const shown = new Map();
        data.sizes.forEach((size, k) => {
          const rep = merged[k];
          const entry = shown.get(rep) ?? { name: nameOf(names, rep), color: categoricalColor(rep), cells: 0, clusters: [] };
          entry.cells += size;
          entry.clusters.push(k);
          shown.set(rep, entry);
        });
        state.legend = { kind: 'clusters', entries: [...shown.values()], clustering: c.clustering };
      } else if (c.kind === 'gene' && c.gene) {
        const clustering = state.ctx.shown ?? state.ctx.clusterings[0]?.id;
        const { gene, values } = await app.bundles.expression(state.ctx.item, clustering, c.gene);
        let hi = 0;
        for (const v of values) if (v > hi) hi = v;
        state.colors = valueColors(values, colormapLUT('viridis'), 0, hi || 1);
        state.legend = { kind: 'scale', label: `${gene}: log-normalized expression`, lo: 0, hi };
      } else if (c.kind === 'attribute') {
        const spec = ATTRIBUTE_COLORS.find((a) => a.id === c.attribute);
        const raw = state.cells[spec.column];
        const sorted = Float64Array.from(raw).sort();
        const lo = sorted[Math.floor(0.01 * (sorted.length - 1))];
        const hi = sorted[Math.floor(0.99 * (sorted.length - 1))];
        state.colors = valueColors(raw, colormapLUT('viridis'), lo, hi);
        state.legend = { kind: 'scale', label: spec.label, lo, hi, note: '1st to 99th percentile' };
      } else if (c.kind === 'reliability' && state.map) {
        const values = new Float32Array(n).fill(NaN);
        for (let i = 0; i < state.map.n; i += 1) values[state.map.cells[i]] = state.map.score[i];
        state.colors = valueColors(values, colormapLUT('viridis'), 0, 1);
        state.legend = { kind: 'scale', label: 'How faithfully the map keeps each region\'s neighbors (precision and recall, 0 to 1)', lo: 0, hi: 1 };
      } else {
        state.colors = new Uint8Array(4 * n);
        state.legend = null;
      }
    } catch (error) {
      toast(`The colors: ${error.message}`, { kind: 'error' });
      state.colors = new Uint8Array(4 * n);
      state.legend = null;
    }
    tissueColors();
    drawMap();
    drawLegend();
  }

  // The tissue's fills: with a selection, the other cells faint, as on the map.
  function tissueColors() {
    if (!state.renderer || !state.colors) return;
    if (!state.count) {
      state.renderer.setCellColors(state.colors);
      state.renderer.setHighlight(null);
      return;
    }
    const rgba = state.colors.slice();
    for (let i = 0; i < state.selected.length; i += 1) {
      if (!state.selected[i]) rgba[4 * i + 3] = Math.round(rgba[4 * i + 3] * 0.15);
      else if (rgba[4 * i + 3] === 0) rgba.set([255, 255, 255, 200], 4 * i);
    }
    state.renderer.setCellColors(rgba);
    state.renderer.setHighlight(state.selected, '#ffffff', 0.18);
  }

  function drawLegend() {
    clear(legend);
    const l = state.legend;
    if (!l) return;
    if (l.kind === 'scale') {
      const fmt = (v) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(2));
      legend.append(h('div.cm-scale', h('b', l.label), h('div.viewer-legend-bar', { style: { background: colormapCSS('viridis') } }), h('div.viewer-legend-range', h('span', fmt(l.lo)), h('span', fmt(l.hi))), l.note ? h('span.muted', l.note) : null));
      return;
    }
    const inSelection = new Map();
    const groups = state.groups;
    if (state.count && groups) {
      for (let i = 0; i < groups.length; i += 1) if (state.selected[i] && groups[i] >= 0) inSelection.set(groups[i], (inSelection.get(groups[i]) ?? 0) + 1);
    }
    legend.append(...l.entries.map((e) => {
      const picked = e.clusters.reduce((s, k) => s + (inSelection.get(k) ?? 0), 0);
      return h('button.cm-legend-item', { type: 'button', title: `Select the cells of ${e.name}`, 'data-clusters': e.clusters.join(','), onclick: (event) => selectGroups(groups, e.clusters, event.shiftKey) },
        h('span.dot', { style: { background: e.color } }), e.name, h('span.muted', state.count ? `${formatCount(picked)} of ${formatCount(e.cells)}` : formatCount(e.cells)));
    }));
  }

  // --- The map ---
  function drawMap() {
    const ctx2 = mapCanvas.getContext('2d');
    ctx2.clearRect(0, 0, mapCanvas.width, mapCanvas.height);
    if (!state.map || !state.view || !state.colors) return;
    const { n, xy, cells } = state.map;
    const colors = new Uint8Array(4 * n);
    const selected = state.count ? new Uint8Array(n) : null;
    for (let i = 0; i < n; i += 1) {
      const c = cells[i];
      colors.set(state.colors.subarray(4 * c, 4 * c + 4), 4 * i);
      if (colors[4 * i + 3] === 0) colors.set([128, 128, 128, 60], 4 * i);
      if (selected) selected[i] = state.selected[c];
    }
    const ratio = window.devicePixelRatio || 1;
    const size = Math.max(2, Math.round((n > 50000 ? 2 : 3) * ratio));
    const img = rasterize({ xy, n, colors, view: state.view, width: mapCanvas.width, height: mapCanvas.height, size, selected });
    ctx2.putImageData(new ImageData(img, mapCanvas.width, mapCanvas.height), 0, 0);
    if (state.focus >= 0) {
      const at = state.mapIndexOf?.get(state.focus);
      if (at !== undefined) {
        const [px, py] = toPixel(state.view, mapCanvas.width, mapCanvas.height, xy[2 * at], xy[2 * at + 1]);
        ctx2.strokeStyle = '#ffffff';
        ctx2.lineWidth = 2 * ratio;
        ctx2.beginPath();
        ctx2.arc(px, py, 7 * ratio, 0, 2 * Math.PI);
        ctx2.stroke();
        ctx2.strokeStyle = '#000000';
        ctx2.lineWidth = ratio;
        ctx2.beginPath();
        ctx2.arc(px, py, 9 * ratio, 0, 2 * Math.PI);
        ctx2.stroke();
      }
    }
  }

  const devicePoint = (canvas, event) => {
    const r = canvas.getBoundingClientRect();
    const ratio = window.devicePixelRatio || 1;
    return [(event.clientX - r.left) * ratio, (event.clientY - r.top) * ratio];
  };
  let drag = null;
  mapCanvas.addEventListener('pointerdown', (event) => {
    if (event.button !== 0 || !state.view) return;
    mapCanvas.setPointerCapture(event.pointerId);
    const [x, y] = devicePoint(mapCanvas, event);
    drag = { x, y, view: state.view, moved: false };
  });
  mapCanvas.addEventListener('pointermove', (event) => {
    if (!drag) return;
    const [x, y] = devicePoint(mapCanvas, event);
    if (Math.hypot(x - drag.x, y - drag.y) > 3) drag.moved = true;
    if (drag.moved) {
      state.view = { ...drag.view, cx: drag.view.cx - (x - drag.x) * drag.view.scale, cy: drag.view.cy + (y - drag.y) * drag.view.scale };
      state.mapMoved = true;
      drawMap();
    }
  });
  mapCanvas.addEventListener('pointerup', (event) => {
    if (!drag) return;
    const moved = drag.moved;
    drag = null;
    if (moved || !state.map) return;
    const [px, py] = devicePoint(mapCanvas, event);
    const [x, y] = toPlane(state.view, mapCanvas.width, mapCanvas.height, px, py);
    const at = nearestPoint(state.grid, x, y, 8 * (window.devicePixelRatio || 1) * state.view.scale);
    if (at >= 0) focus(state.map.cells[at], { map: false });
  });
  mapCanvas.addEventListener('wheel', (event) => {
    if (!state.view) return;
    event.preventDefault();
    const [x, y] = devicePoint(mapCanvas, event);
    const delta = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaY;
    state.view = zoomAt(state.view, mapCanvas.width, mapCanvas.height, x, y, Math.exp(-delta * (event.ctrlKey ? 0.01 : 0.0018)));
    state.mapMoved = true;
    drawMap();
  }, { passive: false });
  mapCanvas.addEventListener('dblclick', () => {
    if (!state.map) return;
    state.view = fitView(state.map.xy, state.map.n, mapCanvas.width, mapCanvas.height);
    state.mapMoved = false;
    drawMap();
  });

  // --- Lasso, on either pane ---
  function setLasso(on) {
    state.lasso = on;
    lassoButton.setAttribute('aria-pressed', String(on));
    root.classList.toggle('lassoing', on);
  }
  function lassoOn(overlay, done) {
    let points = null;
    overlay.addEventListener('pointerdown', (event) => {
      if (!state.lasso || event.button !== 0) return;
      overlay.setPointerCapture(event.pointerId);
      points = [devicePoint(overlay, event)];
    });
    overlay.addEventListener('pointermove', (event) => {
      if (!points) return;
      points.push(devicePoint(overlay, event));
      const g = overlay.getContext('2d');
      g.clearRect(0, 0, overlay.width, overlay.height);
      g.strokeStyle = '#ffffff';
      g.setLineDash([6, 4]);
      g.lineWidth = 1.5 * (window.devicePixelRatio || 1);
      g.beginPath();
      points.forEach(([x, y], i) => (i ? g.lineTo(x, y) : g.moveTo(x, y)));
      g.closePath();
      g.stroke();
    });
    overlay.addEventListener('pointerup', (event) => {
      if (!points) return;
      const drawn = points;
      points = null;
      overlay.getContext('2d').clearRect(0, 0, overlay.width, overlay.height);
      if (drawn.length >= 3) done(drawn, event.shiftKey);
    });
  }
  lassoOn(mapOverlay, (pixels, add) => {
    if (!state.map) return;
    const polygon = pixels.map(([x, y]) => toPlane(state.view, mapCanvas.width, mapCanvas.height, x, y));
    selectOnMap(polygon, add);
  });
  lassoOn(tissueOverlay, (pixels, add) => {
    if (!state.renderer) return;
    const [w, hgt] = state.renderer.size();
    const polygon = pixels.map(([x, y]) => screenToWorld(state.renderer.camera, w, hgt, x, y));
    selectOnTissue(polygon, add);
  });

  // The map's cells inside a polygon (map coordinates).
  function selectOnMap(polygon, add = false) {
    const inside = pointsInPolygon(state.map.xy, state.map.n, polygon);
    const mask = add && state.count ? state.selected.slice() : new Uint8Array(state.cells.n);
    for (let i = 0; i < state.map.n; i += 1) if (inside[i]) mask[state.map.cells[i]] = 1;
    select(mask);
  }
  // The cells whose centroid lies inside a polygon (µm).
  function selectOnTissue(polygon, add = false) {
    const mask = add && state.count ? state.selected.slice() : new Uint8Array(state.cells.n);
    let x0 = Infinity; let y0 = Infinity; let x1 = -Infinity; let y1 = -Infinity;
    for (const [x, y] of polygon) {
      x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y);
    }
    const { x, y, n } = state.cells;
    for (let i = 0; i < n; i += 1) {
      if (x[i] < x0 || x[i] > x1 || y[i] < y0 || y[i] > y1) continue;
      if (insidePolygon(polygon, x[i], y[i])) mask[i] = 1;
    }
    select(mask);
  }
  // Selects the cells of some groups (each cell's group: its cluster, or its label).
  function selectGroups(groups, list, add = false) {
    if (!groups) return;
    const set = new Set(list);
    const mask = add && state.count ? state.selected.slice() : new Uint8Array(state.cells.n);
    for (let i = 0; i < groups.length; i += 1) if (set.has(groups[i])) mask[i] = 1;
    select(mask);
  }

  // Selects the cells of some of the shown clustering's clusters, whatever colors them.
  function selectClusters(list, add = false) {
    selectGroups(state.clusters, list, add);
  }

  function select(mask) {
    state.selected = mask ?? new Uint8Array(state.cells?.n ?? 0);
    let count = 0;
    for (const v of state.selected) count += v;
    state.count = count;
    tissueColors();
    clearButton.disabled = !count;
    selectionInfo.textContent = count ? `${formatCount(count)} cells selected` : '';
    drawMap();
    drawLegend();
    state.ctx.onSelect?.(state.selected, count);
  }

  // --- Focus: a cell shown on both ---
  function focus(cell, { map = true, tissue = true } = {}) {
    state.focus = cell;
    if (state.map && !state.mapIndexOf) {
      state.mapIndexOf = new Map();
      for (let i = 0; i < state.map.n; i += 1) state.mapIndexOf.set(state.map.cells[i], i);
    }
    if (tissue && state.renderer) {
      const cam = state.renderer.camera;
      state.renderer.setCamera({ ...cam, cx: state.cells.x[cell], cy: state.cells.y[cell], scale: Math.max(cam.scale, 3 * (window.devicePixelRatio || 1)) });
    }
    drawMap();
    drawFocusOnTissue();
    state.ctx.onFocus?.(cell);
    void map;
  }
  function drawFocusOnTissue() {
    const g = tissueOverlay.getContext('2d');
    g.clearRect(0, 0, tissueOverlay.width, tissueOverlay.height);
    if (state.focus < 0 || !state.renderer || !state.cells) return;
    const [w, hgt] = state.renderer.size();
    const [px, py] = worldToScreen(state.renderer.camera, w, hgt, state.cells.x[state.focus], state.cells.y[state.focus]);
    const ratio = window.devicePixelRatio || 1;
    g.strokeStyle = '#ffffff';
    g.lineWidth = 2 * ratio;
    g.beginPath();
    g.arc(px, py, 12 * ratio, 0, 2 * Math.PI);
    g.stroke();
  }

  // --- Pickers ---
  mapPick.addEventListener('change', () => {
    state.mapId = mapPick.value;
    state.mapIndexOf = null;
    showMap();
  });
  colorPick.addEventListener('change', () => {
    const v = colorPick.value;
    const [kind, id, k] = v.split(':');
    if (v.startsWith('clusters:')) state.color = { kind: 'clusters', clustering: v.slice(9) };
    else if (v.startsWith('attribute:')) state.color = { kind: 'attribute', attribute: v.slice(10) };
    else if (kind === 'labels') state.color = { kind, labels: id, level: Number(k) };
    else if (kind === 'confidence') state.color = { kind, labels: id };
    else if (kind === 'score') state.color = { ...state.color, kind, labels: id, set: Number(k) };
    else if (v === 'gene') state.color = { kind: 'gene', gene: geneInput.value.trim() || null };
    else state.color = { kind: v };
    geneInput.hidden = state.color.kind !== 'gene';
    if (state.color.kind === 'gene' && !state.color.gene) geneInput.focus();
    recolor();
  });
  geneInput.addEventListener('change', () => {
    state.color = { kind: 'gene', gene: geneInput.value.trim() };
    if (state.color.gene) recolor();
  });

  load();
  return {
    state,
    // A new context (the clusterings, maps or names changed): the pickers again, the colors again.
    update(next) {
      const segmentationChanged = next.segmentation.id !== state.ctx.segmentation.id;
      const shownChanged = next.shown !== state.ctx.shown;
      state.ctx = next;
      if (segmentationChanged) {
        state.fitted = null;
        state.view = null;
        state.mapId = null;
        state.color = { kind: 'clusters', clustering: next.shown };
        load();
        return;
      }
      if (shownChanged && state.color.kind === 'clusters') state.color = { kind: 'clusters', clustering: next.shown };
      if (shownChanged) state.mapId = next.maps.find((m) => m.clustering === next.shown)?.id ?? state.mapId;
      const mapBefore = state.mapId;
      if (!state.cells) return;
      fillPickers();
      if (mapBefore !== state.mapId || !state.map) showMap();
      else recolor();
    },
    selectClusters,
    // Selects the cells of some of the legend's groups (labels, when colored by labels).
    selectGroups: (list, add = false) => selectGroups(state.groups, list, add),
    selectOnMap: (polygon, add) => selectOnMap(polygon, add),
    selectOnTissue: (polygon, add) => selectOnTissue(polygon, add),
    setColor(color) {
      state.color = color;
      if (color.kind === 'gene') geneInput.value = color.gene ?? '';
      fillPickers();
      return recolor();
    },
    setMap(id) {
      state.mapId = id;
      state.mapIndexOf = null;
      mapPick.value = id;
      return showMap();
    },
    focus,
    select,
    ready: () => Boolean(state.cells && (state.map || !state.mapId) && state.colors),
    settled: () => state.renderer?.settled(),
    destroy() {
      state.generation += 1;
      state.observer?.disconnect();
      state.nav?.dispose();
      state.renderer?.destroy();
      root.remove();
    },
  };
}
