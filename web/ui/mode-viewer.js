// The tissue viewer: a dataset's morphology channels, transcripts and cells at any zoom
// (render/renderer.js), with its layers panel (ui/viewer-panel.js), scale bar, coordinate readout,
// minimap, and the cell under the pointer selected into the inspector. The cells are any of the
// dataset's segmentations (wave 3, slice 8): its default when nothing says which, the instrument's
// from the dataset's own data, any other from its artifact (api_segview.go).
//
// What is shown of each dataset (camera, channels, genes, layers) is kept for the session;
// saved views keep it in the project (wave 1, slice 7).

import { h, icon, clear, formatCount } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { addView, captureView, viewCamera, viewLayers } from '../lib/views.js';
import { TissueRenderer } from '../render/renderer.js';
import { fitCamera, scaleBar, screenToWorld, visibleRect, worldToScreen } from '../lib/viewport.js';
import { attachNavigation } from './navigation.js';
import { readCells, joinSamples, nearestCell } from '../lib/viewer-data.js';
import { mountViewerPanel, defaultLayers, cellFillColors } from './viewer-panel.js';
import { defaultChoice, readSegCells, segmentationChoices } from '../lib/segview.js';
import { hexColor } from '../render/gl.js';
import { categoricalColor } from '../lib/colormaps.js';
import { clusterColors, clusterFills, clusterNames, mergedOf, nameOf } from '../lib/clusterings.js';
import { labelColor, labelColors, labelFills, parseLabelFill } from '../lib/labels.js';
import { labelSource, labelVersions, perCell, refOf } from './label-sources.js';

// Per dataset id: { camera, layers, selected } for the session.
const sessions = new Map();

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(body.error ?? `HTTP ${response.status}`), { status: response.status });
  return body;
}

export function mountViewer(app, container) {
  const body = h('div.viewer-body');
  const title = h('span.crumbs');
  const headActions = h('div.btn-row');
  const root = h('div.view.viewer',
    h('div.workbench-head', h('h1', icon('tissue'), 'Viewer', title), h('span.spacer'), headActions),
    body);
  container.append(root);

  let current = null; // { dataset, item, desc, cells, renderer, panel, ... }
  let generation = 0;
  let loading = null; // the dataset and place being opened
  let waiting = null; // { text, item }: the message while its derived data are built

  // The dataset the view shows: the one in focus, else the first of the project.
  function target() {
    const { project, ui } = app.store;
    // A dataset in focus, or a saved view's, else the first.
    const view = ui.focus?.kind === 'view' ? project.views.find((v) => v.id === ui.focus.id) : null;
    const id = ui.focus?.kind === 'dataset' ? ui.focus.id : view?.dataset ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), link };
  }

  function message(iconName, heading, text, actions = []) {
    waiting = null;
    teardown();
    clear(body);
    body.append(h('div.planned', h('div.empty', icon(iconName), h('h3', heading), h('p', text), actions.length ? h('div.btn-row', { style: { marginTop: '12px' } }, ...actions) : null)));
  }

  function teardown() {
    clear(headActions);
    if (!current) return;
    sessions.set(current.dataset.id, { camera: current.renderer.camera, layers: current.layers, selected: current.selectedRow });
    // The region last in view (µm), within the image, which the segmentation studio offers to
    // refine.
    const [w, h2] = current.renderer.size();
    const grid = current.desc?.grid;
    if (w > 0 && h2 > 0 && grid) {
      const r = visibleRect(current.renderer.camera, w, h2);
      const W = grid.width * grid.pixelSize;
      const H = grid.height * grid.pixelSize;
      const x0 = Math.max(0, r.x0);
      const y0 = Math.max(0, r.y0);
      const x1 = Math.min(W, r.x1);
      const y1 = Math.min(H, r.y1);
      app.viewedRegions ??= new Map();
      if (x1 > x0 && y1 > y0) app.viewedRegions.set(current.dataset.id, { x0_um: x0, y0_um: y0, x1_um: x1, y1_um: y1 });
    }
    current.dispose();
    current = null;
    app.inspector.setSection('viewer-cell', null);
  }

  async function show() {
    const t = target();
    if (!t) {
      generation += 1;
      title.textContent = '';
      message('tissue', 'No dataset yet', 'Add a Xenium output bundle to the project in the Data view; its tissue shows here once its derived data are built.', [h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
      return;
    }
    title.textContent = ` · ${t.dataset.name}`;
    if (current?.dataset.id === t.dataset.id && current.item?.path === t.item?.path) return;
    const key = `${t.dataset.id}|${t.item?.path}`;
    if (loading === key) return;
    // A later call supersedes this one (another dataset, or the same one found elsewhere).
    const mine = (generation += 1);
    if (!t.item) {
      message('tissue', `${t.dataset.name} is not available`, t.link?.state === 'checking' ? 'Looking for its bundle…' : 'Its bundle was not found where the project records it. Locate it in the Data view.', [h('button.btn', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
      return;
    }
    if (!waiting || waiting.item !== t.item) message('tissue', `Opening ${t.dataset.name}…`, 'Reading its description and cells.');
    let desc;
    let cells;
    loading = key;
    try {
      desc = await getJSON(`${t.item.url}/view`);
      const response = await fetch(`${t.item.url}/cells`);
      if (!response.ok) throw new Error(`cells: HTTP ${response.status}`);
      cells = readCells(await response.arrayBuffer());
    } catch (error) {
      if (mine !== generation) return;
      if (error.status === 409) {
        if (!app.bundles.cacheJob(t.item)) app.bundles.buildCache(t.item);
        if (waiting?.item !== t.item) {
          message('layers', 'Its derived data are being built', '', [h('button.btn', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
          waiting = { item: t.item, text: body.querySelector('.empty p') };
        }
        updateWaiting();
        return;
      }
      waiting = null;
      message('warning', `${t.dataset.name} cannot be shown`, error.message);
      return;
    } finally {
      if (loading === key) loading = null;
    }
    if (mine !== generation) return;
    waiting = null;
    teardown();
    clear(body);
    current = open(t, desc, cells);
  }

  function open(t, desc, cells) {
    const canvas = h('canvas.viewer-canvas', { tabIndex: 0, 'aria-label': `Tissue of ${t.dataset.name}: drag to pan, scroll to zoom, click a cell to select it` });
    const scale = h('div.viewer-scale', h('div.bar'), h('span'));
    const readout = h('div.viewer-readout.mono');
    const status = h('div.viewer-status');
    const minimap = h('canvas.viewer-minimap', { width: 200, height: 130, title: 'The whole tissue: click to go there' });
    const legend = h('div.viewer-legend');
    const zoomIn = h('button.icon-button', { type: 'button', title: 'Zoom in (+)', 'aria-label': 'Zoom in', onclick: () => zoomBy(2) }, icon('plus'));
    const zoomOut = h('button.icon-button', { type: 'button', title: 'Zoom out (−)', 'aria-label': 'Zoom out', onclick: () => zoomBy(0.5) }, icon('minus'));
    const fit = h('button.icon-button', { type: 'button', title: 'The whole tissue (0)', 'aria-label': 'Show the whole tissue', onclick: () => fitAll() }, icon('expand'));
    const stage = h('div.viewer-stage', canvas, h('div.viewer-tools', zoomIn, zoomOut, fit), minimap, legend, h('div.viewer-foot', scale, readout, status));
    const panelHost = h('aside.viewer-panel', { 'aria-label': 'Layers' });
    body.append(panelHost, stage);

    const session = sessions.get(t.dataset.id);
    const layers = session?.layers ?? defaultLayers(desc);
    const renderer = new TissueRenderer(canvas, { onChange: (plan) => afterFrame(plan) });
    renderer.setDataset(desc, cells);
    renderer.setLayers(layers);
    const state = {
      dataset: t.dataset, item: t.item, desc, cells, renderer, layers, selectedRow: -1, detail: null,
      // The segmentation drawn other than the dataset's own: { id, choice, cells }, or null.
      seg: null, segGeneration: 0, segReady: Promise.resolve(),
      dispose() {
        observer.disconnect();
        navigation.dispose();
        renderer.destroy();
        window.removeEventListener('keydown', onKey);
        if (window.situcraftViewer?.renderer === renderer) delete window.situcraftViewer;
      },
    };

    // The layers panel, mounted again when a saved view replaces every setting.
    let panel = null;
    // The dataset's segmentations, its default first; the one drawn (the instrument's when its
    // own cells are).
    const choices = () => segmentationChoices(app.store.project, t.dataset.id);
    const drawnChoice = () => state.seg?.choice ?? choices().find((c) => c.instrument) ?? null;
    // What the panel lists: the segmentations, and the drawn one's clusterings.
    // Curated and consensus label sets, drawn as labels at one level.
    const versionFills = () => labelVersions(app.store.project, t.dataset.id, drawnChoice()?.id).filter((v) => v.kind === 'curated' || v.kind === 'consensus').map((v) => ({ id: `labels:${v.id}:0`, labels: v.id, level: 0, label: `${v.kind === 'curated' ? 'Curated' : 'Consensus'} labels: ${v.name}` }));
    const analysisFills = () => [...clusterFills(app.store.project, t.dataset.id, drawnChoice()?.id), ...labelFills(app.store.project, t.dataset.id, drawnChoice()?.id), ...versionFills()];
    const choiceKey = () => `${choices().map((c) => `${c.id}${c.isDefault ? '*' : ''}`).join()}|${analysisFills().map((f) => f.id).join()}`;

    function mountPanel(start) {
      clear(panelHost);
      state.choiceKey = choiceKey();
      panel = mountViewerPanel(panelHost, { desc, layers: start, cells: renderer.cells, app, segmentations: choices(), drawn: drawnChoice(), clusterings: analysisFills(), onChange: (next, what) => {
        state.layers = next;
        renderer.setLayers(next);
        if (what === 'segmentation') {
          drawSegmentation(next.cells.segmentation);
          return;
        }
        if (what === 'fill') applyFill();
        if (what === 'channels') drawMinimap();
        updateLegend();
      } });
      state.panel = panel;
    }
    mountPanel(layers);

    // The fill's colors per cell; a clustering's are read first (each cell's cluster, from the
    // program), and must be of the cells drawn.
    function applyFill() {
      const fill = state.layers.cells.fill;
      state.clusterFill = null;
      // Every change of fill: a clustering's or labels' colors read for an earlier one are dropped.
      const mine = (state.fillGeneration = (state.fillGeneration ?? 0) + 1);
      const asLabels = parseLabelFill(fill);
      if (asLabels) {
        renderer.setCellColors(null);
        labelSource(app, refOf(asLabels.id, asLabels.level)).then((src) => {
          if (mine !== state.fillGeneration || current !== state) return;
          if (src.segCells !== renderer.cells?.n) {
            toast(`The labels are of another segmentation's ${formatCount(src.segCells)} cells; ${formatCount(renderer.cells?.n ?? 0)} are drawn.`, { kind: 'error' });
            return;
          }
          const sets = src.names.map((name, k) => ({ name, color: src.colors[k] }));
          const per = perCell(src, sets.length);
          const counts = new Map();
          let analyzed = 0;
          for (const k of per) {
            if (k >= 0) {
              counts.set(k, (counts.get(k) ?? 0) + 1);
              analyzed += 1;
            }
          }
          state.clusterFill = { labels: true, unlabeled: sets.length, sets, counts, analyzed, cells: per.length, name: [...labelFills(app.store.project, t.dataset.id, drawnChoice()?.id), ...versionFills()].find((f) => f.id === fill)?.label ?? src.name, rgba: labelColors(per, sets.length, sets) };
          renderer.setCellColors(state.clusterFill.rgba);
          updateLegend();
        }).catch((error) => toast(`The labels could not be drawn: ${error.message}`, { kind: 'error' }));
        return;
      }
      if (!fill?.startsWith('clusters:')) {
        renderer.setCellColors(cellFillColors(fill, renderer.cells, desc));
        return;
      }
      const id = fill.slice('clusters:'.length);
      renderer.setCellColors(null);
      app.bundles.clusteringData(id).then((data) => {
        if (mine !== state.fillGeneration || current !== state) return;
        if (data.cluster.length !== renderer.cells?.n) {
          toast(`The clustering is of another segmentation's ${formatCount(data.cluster.length)} cells; ${formatCount(renderer.cells?.n ?? 0)} are drawn.`, { kind: 'error' });
          return;
        }
        state.clusterFill = { id, sizes: data.sizes, analyzed: data.description.cells.analyzed, cells: data.cluster.length, name: clusterFills(app.store.project, t.dataset.id, drawnChoice()?.id).find((f) => f.clustering === id)?.label ?? 'Clusters' };
        const names = clusterNames(app.store.project, id);
        state.clusterFill.names = names;
        state.clusterFill.merged = mergedOf(names, data.sizes.length);
        state.clusterFill.rgba = clusterColors(data.cluster, (k) => state.clusterFill.merged[k]);
        renderer.setCellColors(state.clusterFill.rgba);
        updateLegend();
      }).catch((error) => toast(`The clusters could not be drawn: ${error.message}`, { kind: 'error' }));
    }

    // A clustering's legend: each cluster's color and cells (the first 24), and the cells not
    // analyzed, which are not filled.
    function clusterLegend() {
      const f = state.clusterFill;
      if (!f) return null;
      if (f.labels) {
        const entries = [...f.counts.entries()].sort((a, b) => (a[0] === f.unlabeled) - (b[0] === f.unlabeled) || b[1] - a[1]);
        const shown = entries.slice(0, 24);
        return h('div', h('b', f.name),
          ...shown.map(([k, size]) => h('div.viewer-legend-item', h('span.dot', { style: { background: labelColor(k, f.unlabeled, f.sets) } }), k === f.unlabeled ? 'No label' : f.sets[k].name, h('span.muted', formatCount(size)))),
          entries.length > shown.length ? h('div.muted', `and ${entries.length - shown.length} more labels`) : null,
          f.analyzed < f.cells ? h('div.muted', `${formatCount(f.cells - f.analyzed)} cells not analyzed are not filled.`) : null);
      }
      // Clusters sharing a name are one entry, in the first one's color.
      const groups = new Map();
      f.sizes.forEach((size, k) => {
        const rep = f.merged?.[k] ?? k;
        const g = groups.get(rep) ?? { name: nameOf(f.names, rep), size: 0 };
        g.size += size;
        groups.set(rep, g);
      });
      const entries = [...groups.entries()];
      const shown = entries.slice(0, 24);
      return h('div', h('b', f.name),
        ...shown.map(([rep, g]) => h('div.viewer-legend-item', h('span.dot', { style: { background: categoricalColor(rep) } }), g.name, h('span.muted', formatCount(g.size)))),
        entries.length > shown.length ? h('div.muted', `and ${entries.length - shown.length} smaller clusters`) : null,
        f.analyzed < f.cells ? h('div.muted', `${formatCount(f.cells - f.analyzed)} cells not analyzed are not filled.`) : null);
    }

    // The legend: which segmentation is drawn and how it was made, the fill's scale, and the
    // transcripts' probability scale when they are colored by it.
    function updateLegend() {
      clear(legend);
      const c = drawnChoice();
      const cellsShown = state.layers.cells.outlines || state.layers.cells.fill;
      if (state.loadingSeg) legend.append(h('div.viewer-legend-seg', h('b', state.loadingSeg.name), h('div.muted', 'Reading its cells…')));
      else if (c && cellsShown && choices().length > 1) legend.append(h('div.viewer-legend-seg', h('b', c.name), h('div.muted', `${c.about[0].toUpperCase()}${c.about.slice(1)}`)));
      const part = state.layers.cells.fill ? (state.layers.cells.fill.startsWith('clusters:') || parseLabelFill(state.layers.cells.fill) ? clusterLegend() : panel.legend?.()) : null;
      if (part) legend.append(part);
      const probability = panel.probabilityLegend?.();
      if (probability) legend.append(probability);
      legend.hidden = !legend.childNodes.length;
    }

    // Draws a segmentation: undefined for the dataset's default, null for its own (the
    // instrument's), else an artifact's id. Its outlines, cells table and transcripts come from
    // its artifact; the selection, fills' colors and highlights go with the cells they were of.
    function drawSegmentation(requested) {
      const list = choices();
      let c;
      if (requested === undefined) c = defaultChoice(app.store.project, t.dataset.id);
      else if (requested !== null) {
        c = list.find((x) => x.id === requested);
        if (!c) toast(`The segmentation ${requested} is not among ${t.dataset.name}'s (it may have been removed): the instrument's is drawn.`, { kind: 'error' });
      }
      if (c?.instrument) c = null;
      const mine = (state.segGeneration += 1);
      const settle = () => {
        state.loadingSeg = null;
        state.layers = { ...state.layers, cells: { ...state.layers.cells, segmentation: state.seg?.id ?? null } };
        renderer.setLayers(state.layers);
        state.detail = null;
        state.selectedRow = -1;
        app.inspector.setSection('viewer-cell', null);
        mountPanel(state.layers);
        applyFill();
        state.showChosen?.();
        updateLegend();
      };
      if ((c?.id ?? null) === (state.seg?.id ?? null)) {
        if (c) state.seg.choice = c;
        settle();
        state.segReady = Promise.resolve();
        return state.segReady;
      }
      if (!c) {
        state.seg = null;
        renderer.setSegmentation(null);
        settle();
        state.segReady = Promise.resolve();
        return state.segReady;
      }
      const base = `${t.item.url}/segmentations/${encodeURIComponent(c.id)}`;
      // While it is read, the cells drawn before are not shown as though they were its.
      state.loadingSeg = c;
      renderer.setLayers({ ...state.layers, cells: { ...state.layers.cells, outlines: false, fill: null } });
      updateLegend();
      state.segReady = (async () => {
        try {
          const [out, response] = await Promise.all([getJSON(`${base}/outlines`), fetch(`${base}/cells`)]);
          if (!response.ok) throw new Error(`its cells: HTTP ${response.status}`);
          const segCells = readSegCells(await response.arrayBuffer());
          if (mine !== state.segGeneration || current !== state) return;
          state.seg = { id: c.id, choice: c, cells: segCells };
          renderer.setSegmentation({ id: c.id, polygonsBase: out.polygonsBase, geometry: out.geometry, cells: segCells, points: `${base}/points`, probability: segCells.holds.probability });
          settle();
        } catch (error) {
          if (mine !== state.segGeneration || current !== state) return;
          state.loadingSeg = null;
          toast(`${c.name} could not be drawn: ${error.message}`, { kind: 'error' });
          await drawSegmentation(null);
        }
      })();
      return state.segReady;
    }

    // --- Size and camera ---
    const observer = new ResizeObserver(() => {
      const r = stage.getBoundingClientRect();
      renderer.resize(r.width, r.height);
      if (!state.placed) {
        state.placed = true;
        if (session?.camera && !state.wholeSection) renderer.setCamera(session.camera);
        else fitAll();
      }
      drawMinimap();
    });
    observer.observe(stage);

    function tissueRect() {
      const b = desc.transcripts?.bounds;
      if (b && b[2] > b[0]) return { x0: Math.min(0, b[0]), y0: Math.min(0, b[1]), x1: Math.max(desc.grid.width * desc.grid.pixelSize, b[2]), y1: Math.max(desc.grid.height * desc.grid.pixelSize, b[3]) };
      return { x0: 0, y0: 0, x1: desc.grid.width * desc.grid.pixelSize, y1: desc.grid.height * desc.grid.pixelSize };
    }

    const limits = () => ({ minScale: (renderer.ratio || 1) / 400, maxScale: (renderer.ratio || 1) * 60 });

    function fitAll() {
      const [w, h2] = renderer.size();
      renderer.setCamera(fitCamera(tissueRect(), w, h2));
    }

    // --- Pointer ---
    const navigation = attachNavigation(canvas, renderer, {
      limits,
      onClick: (sx, sy) => select(sx, sy),
      onMove: (x, y) => {
        readout.textContent = x === null ? '' : `x ${x.toFixed(1)} µm  y ${y.toFixed(1)} µm`;
      },
    });
    const zoomBy = (factor, sx, sy) => navigation.zoomBy(factor, sx, sy);

    function onKey(event) {
      if (document.activeElement !== canvas) return;
      const [w, h2] = renderer.size();
      const step = Math.min(w, h2) * 0.15 / renderer.camera.scale;
      const cam = renderer.camera;
      const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
      if (moves[event.key]) renderer.setCamera({ ...cam, cx: cam.cx + moves[event.key][0], cy: cam.cy + moves[event.key][1] });
      else if (event.key === '+' || event.key === '=') zoomBy(1.5);
      else if (event.key === '-' || event.key === '_') zoomBy(1 / 1.5);
      else if (event.key === '0') fitAll();
      else if (event.key === 'Escape') select(null);
      else return;
      event.preventDefault();
    }
    window.addEventListener('keydown', onKey);

    // --- Selection ---
    async function select(sx, sy) {
      let row = -1;
      if (sx !== null && sx !== undefined) {
        row = renderer.pick(sx, sy);
        if (row < 0) {
          // No outline loaded there (zoomed out): the nearest centroid within a cell's width.
          const [w, h2] = renderer.size();
          const [wx, wy] = screenToWorld(renderer.camera, w, h2, sx, sy);
          row = nearestCell(renderer.cells, wx, wy, Math.max(8, 6 * (renderer.ratio || 1) / renderer.camera.scale));
        }
      }
      await selectRow(row);
    }

    async function selectRow(row) {
      state.selectedRow = row;
      if (row < 0) {
        state.detail = null;
        renderer.setSelected(null);
        app.inspector.setSection('viewer-cell', null);
        return;
      }
      const seg = state.seg;
      try {
        const detail = await getJSON(seg ? `${t.item.url}/segmentations/${encodeURIComponent(seg.id)}/cells/${row}?top=12` : `${t.item.url}/cells/${row}?top=12`);
        if (state.selectedRow !== row || current !== state || state.seg !== seg) return;
        state.detail = detail;
        renderer.setSelected(detail);
        app.inspector.setSection('viewer-cell', () => (seg ? segCellSection(detail) : cellSection(detail)));
      } catch (error) {
        toast(`That cell could not be read: ${error.message}`, { kind: 'error' });
      }
    }

    function cellSection(d) {
      const fmtArea = (v) => (v === undefined || v === null ? '—' : `${v.toFixed(1)} µm²`);
      const recorded = d.recorded.find((c) => c.label === 'Gene transcripts')?.value;
      const agrees = recorded === undefined || recorded === d.counted.q20Genes;
      const maxGene = d.genes[0]?.count || 1;
      return h('section.inspector-section.viewer-cell',
        h('h3', icon('cell'), `Cell ${d.id}`),
        h('dl.kv',
          h('dt', 'Row'), h('dd', String(d.row)),
          h('dt', 'Centroid'), h('dd', `${d.x.toFixed(1)}, ${d.y.toFixed(1)} µm`),
          h('dt', 'Area'), h('dd', fmtArea(d.area)),
          h('dt', 'Nucleus'), h('dd', fmtArea(d.nucleusArea)),
          d.method ? [h('dt', 'Segmented'), h('dd', d.method.replace(/^Segmented by /, 'by '))] : null,
          h('dt', 'Transcripts'), h('dd', `${formatCount(d.counted.q20Genes)} of genes (Q20)`, h('span.muted', `; ${formatCount(d.counted.all)} of any kind or quality`)),
          h('dt', 'Genes'), h('dd', String(d.genesDetected))),
        agrees ? null : h('p.callout.warn', `cells.parquet records ${recorded} gene transcripts; the transcripts assigned to the cell are ${d.counted.q20Genes}.`),
        d.genes.length ? h('div.viewer-genes',
          h('div.muted', { style: { fontSize: '11.5px', margin: '8px 0 4px' } }, 'Top genes (Q20 transcripts); click one to show it'),
          ...d.genes.map((g) => h('button.viewer-gene-bar', { type: 'button', title: `Show ${g.name}'s transcripts`, onclick: () => panel.addGene(g.gene) },
            h('span.name', g.name), h('span.bar', h('span', { style: { width: `${(100 * g.count) / maxGene}%` } })), h('span.n', String(g.count))))) : null,
        h('div.btn-row', { style: { marginTop: '10px' } },
          h('button.btn.small', { type: 'button', onclick: () => centerOn(d) }, icon('target'), 'Center'),
          h('button.btn.small', { type: 'button', onclick: () => selectRow(-1) }, icon('close'), 'Clear')));
    }

    // A cell of a segmentation other than the instrument's: where it came from, its counts, and
    // what its method says of it (its limitations, its boundary's certainty, its transcripts').
    function segCellSection(d) {
      const fmtArea = (v) => (Number.isFinite(v) && v > 0 ? `${v.toFixed(1)} µm²` : '—');
      const maxGene = d.genes[0]?.count || 1;
      const certain = (v) => (v === null || v === undefined ? null : v.toFixed(2));
      return h('section.inspector-section.viewer-cell',
        h('h3', icon('cell'), `Cell ${d.id}`),
        h('dl.kv',
          h('dt', 'Segmentation'), h('dd', d.segmentation.name),
          h('dt', 'From'), h('dd', d.instrument ? `the instrument's cell ${d.instrument.id}` : h('span.muted', 'no cell of the instrument\'s (its nucleus lies in none)')),
          h('dt', 'Centroid'), h('dd', `${d.x.toFixed(1)}, ${d.y.toFixed(1)} µm`),
          h('dt', 'Area'), h('dd', fmtArea(d.area)),
          h('dt', 'Nucleus'), h('dd', fmtArea(d.nucleusArea)),
          h('dt', 'Transcripts'), h('dd', `${formatCount(d.transcripts)} of genes (Q20)`, h('span.muted', `; ${formatCount(d.assigned)} of any kind or quality`)),
          h('dt', 'Genes'), h('dd', String(d.genesDetected)),
          certain(d.boundaryCertainty) ? [h('dt', 'Boundary'), h('dd', `certainty ${certain(d.boundaryCertainty)}`, h('span.muted', ': the mean share of the samples its pixels were its'))] : null,
          certain(d.meanProbability) ? [h('dt', 'Assignments'), h('dd', `certainty ${certain(d.meanProbability)}`, h('span.muted', ': its transcripts\' mean probability of being its'))] : null),
        d.limitations.length ? h('p.callout.warn', `Flagged: ${d.limitations.join(', ')}. ${d.limitations.includes('uncertain boundary') ? 'Its pixels were its in fewer than three quarters of the samples, on average. ' : ''}${d.limitations.includes('few transcripts') ? 'With so few transcripts its boundary comes more from the prior than from them. ' : ''}${d.limitations.includes('no nucleus') ? 'No nuclear prior held it.' : ''}`.trim()) : null,
        d.genes.length ? h('div.viewer-genes',
          h('div.muted', { style: { fontSize: '11.5px', margin: '8px 0 4px' } }, 'Top genes (Q20 transcripts); click one to show it'),
          ...d.genes.map((g) => h('button.viewer-gene-bar', { type: 'button', disabled: g.gene < 0, title: g.gene < 0 ? `${g.name} is not among the dataset's features` : `Show ${g.name}'s transcripts`, onclick: () => panel.addGene(g.gene) },
            h('span.name', g.name), h('span.bar', h('span', { style: { width: `${(100 * g.count) / maxGene}%` } })), h('span.n', String(g.count))))) : null,
        h('div.btn-row', { style: { marginTop: '10px' } },
          h('button.btn.small', { type: 'button', onclick: () => centerOn(d) }, icon('target'), 'Center'),
          h('button.btn.small', { type: 'button', onclick: () => selectRow(-1) }, icon('close'), 'Clear')));
    }

    function centerOn(d) {
      const cam = renderer.camera;
      // Close enough to see the cell and its neighbors.
      const scale = Math.max(cam.scale, (renderer.ratio || 1) * 4);
      renderer.setCamera({ cx: d.x, cy: d.y, scale });
    }

    // --- Overlays ---
    function afterFrame(plan) {
      if (!plan) return;
      const s = scaleBar(plan.umPerCss);
      scale.querySelector('.bar').style.width = `${s.css}px`;
      scale.querySelector('span').textContent = s.label;
      const pending = renderer.pending();
      const mode = plan.transcripts ? (plan.transcripts.mode === 'points' ? 'transcripts as points' : `transcript density (level ${plan.transcripts.level})`) : '';
      status.textContent = [`level ${plan.level}`, mode, pending ? `loading ${pending}` : ''].filter(Boolean).join(' · ');
      drawMinimapView();
    }

    // The minimap: the coarsest level of the visible channels, and the view's rectangle.
    let minimapImage = null;
    async function drawMinimap() {
      const ctx = minimap.getContext('2d');
      const grid = desc.grid;
      const ratio = window.devicePixelRatio || 1;
      const cssW = 200;
      const cssH = Math.round(cssW * Math.min(1, grid.height / grid.width));
      minimap.style.width = `${cssW}px`;
      minimap.style.height = `${cssH}px`;
      minimap.width = Math.round(cssW * ratio);
      minimap.height = Math.round(cssH * ratio);
      const visible = state.layers.channels.map((c, i) => [c, i]).filter(([c]) => c.visible);
      if (!desc.channels.length) {
        minimapImage = null;
        drawMinimapView();
        return;
      }
      const top = desc.channels[0].levels.length - 1;
      const lv = desc.channels[0].levels[top];
      const rgb = new Float32Array(lv.width * lv.height * 3);
      try {
        for (const [c, i] of visible) {
          const color = hexColor(c.color);
          const ctop = desc.channels[i].levels.length - 1;
          const clv = desc.channels[i].levels[ctop];
          for (let ty = 0; ty < clv.down; ty += 1) {
            for (let tx = 0; tx < clv.across; tx += 1) {
              const response = await fetch(`/api/cache/${desc.entries.image}/image/${i}/${ctop}/${tx}/${ty}`);
              const w = Number(response.headers.get('X-Tile-Width'));
              const hgt = Number(response.headers.get('X-Tile-Height'));
              const samples = joinSamples(new Uint8Array(await response.arrayBuffer()), w, hgt);
              const span = Math.max(1, c.window[1] - c.window[0]);
              for (let y = 0; y < hgt; y += 1) {
                for (let x = 0; x < w; x += 1) {
                  const px = tx * desc.grid.tile + x;
                  const py = ty * desc.grid.tile + y;
                  if (px >= lv.width || py >= lv.height) continue;
                  const n = Math.min(1, Math.max(0, (samples[y * w + x] - c.window[0]) / span)) ** c.gamma;
                  const k = 3 * (py * lv.width + px);
                  rgb[k] += color[0] * n;
                  rgb[k + 1] += color[1] * n;
                  rgb[k + 2] += color[2] * n;
                }
              }
            }
          }
        }
      } catch {
        return;
      }
      const img = new ImageData(lv.width, lv.height);
      for (let i = 0; i < lv.width * lv.height; i += 1) {
        img.data[4 * i] = Math.min(255, 255 * rgb[3 * i]);
        img.data[4 * i + 1] = Math.min(255, 255 * rgb[3 * i + 1]);
        img.data[4 * i + 2] = Math.min(255, 255 * rgb[3 * i + 2]);
        img.data[4 * i + 3] = 255;
      }
      minimapImage = await createImageBitmap(img);
      if (ctx) drawMinimapView();
    }

    function drawMinimapView() {
      const ctx = minimap.getContext('2d');
      if (!ctx) return;
      const grid = desc.grid;
      const W = grid.width * grid.pixelSize;
      const sx = minimap.width / W;
      ctx.fillStyle = '#000';
      ctx.fillRect(0, 0, minimap.width, minimap.height);
      if (minimapImage) ctx.drawImage(minimapImage, 0, 0, minimap.width, minimap.height);
      const [w, h2] = renderer.size();
      const r = visibleRect(renderer.camera, w, h2);
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = Math.max(1, (window.devicePixelRatio || 1) * 1.5);
      ctx.strokeRect(r.x0 * sx, r.y0 * sx, (r.x1 - r.x0) * sx, (r.y1 - r.y0) * sx);
    }

    minimap.addEventListener('pointerdown', (event) => {
      const r = minimap.getBoundingClientRect();
      const W = desc.grid.width * desc.grid.pixelSize;
      const x = ((event.clientX - r.left) / r.width) * W;
      const y = ((event.clientY - r.top) / r.height) * (desc.grid.height * desc.grid.pixelSize);
      renderer.setCamera({ ...renderer.camera, cx: x, cy: y });
    });

    // Cells chosen in the QC view are the instrument's: they show while its cells are drawn.
    state.showChosen = () => {
      const chosen = app.selection?.dataset === t.dataset.id && !state.seg ? app.selection : null;
      renderer.setHighlight(chosen?.mask ?? null, '#ff4fd8', 0.6);
    };
    applyFill();
    updateLegend();
    state.showChosen();
    // The segmentation to draw: one asked for (Show on the tissue), else the session's, else the
    // default; then the session's cell.
    const asked = app.pendingSegmentation?.dataset === t.dataset.id ? app.pendingSegmentation : null;
    if (asked) app.pendingSegmentation = null;
    drawSegmentation(asked ? asked.segmentation : layers.cells.segmentation).then(() => {
      if (asked?.fill !== undefined) setFill(asked.fill);
      if (!asked && session?.selected >= 0) selectRow(session.selected);
    });

    // Fills the cells by a fill asked for (a clustering's, from Show on the tissue), outlines on.
    function setFill(fill) {
      if (current !== state) return;
      state.layers = { ...state.layers, cells: { ...state.layers.cells, fill, outlines: true } };
      renderer.setLayers(state.layers);
      mountPanel(state.layers);
      applyFill();
      updateLegend();
    }

    // --- Saved views ---
    function captureNow(name = '') {
      const detail = state.detail;
      return captureView({ name, dataset: t.dataset.id, mode: 'viewer', desc,
        state: { camera: renderer.camera, ratio: window.devicePixelRatio || 1, layers: state.layers, cell: detail ? { row: detail.row, id: detail.id } : null } });
    }

    function applyView(view) {
      const { layers: next, missing } = viewLayers(view, desc, defaultLayers(desc));
      // Each gene in its color, as the panel colors them.
      next.transcripts.colors = next.transcripts.genes.map((_, i) => categoricalColor(i));
      state.layers = next;
      renderer.setLayers(next);
      const drawn = drawSegmentation(next.cells.segmentation);
      drawMinimap();
      // A view without a place (a tour's step) shows the whole section.
      if (view.camera) {
        state.placed = true;
        renderer.setCamera(viewCamera(view, window.devicePixelRatio || 1));
      } else if (state.placed) fitAll();
      else state.wholeSection = true;
      drawn.then(() => {
        if (view.cell) {
          selectRow(view.cell.row).then(() => {
            if (state.detail && state.detail.id !== view.cell.id) toast(`The view's cell ${view.cell.id} is now ${state.detail.id}: the cells changed since it was saved.`, { kind: 'error' });
          });
        } else selectRow(-1);
      });
      if (missing.length) toast(`Not in this dataset, so not shown: ${missing.join(', ')}.`, { kind: 'error' });
    }

    // The segmentations listed again when the project's change (one made, promoted or removed).
    state.refreshChoices = () => {
      const key = choiceKey();
      if (key === state.choiceKey) return;
      if (state.seg && !choices().some((c) => c.id === state.seg.id)) drawSegmentation(null);
      else {
        if (state.seg) state.seg.choice = choices().find((c) => c.id === state.seg.id) ?? state.seg.choice;
        mountPanel(state.layers);
        updateLegend();
      }
    };

    state.takePending = () => {
      const asked = app.pendingSegmentation;
      if (asked?.dataset === t.dataset.id) {
        app.pendingSegmentation = null;
        drawSegmentation(asked.segmentation).then(() => {
          if (asked.fill !== undefined) setFill(asked.fill);
        });
      }
      const view = app.pendingView;
      if (view?.mode !== 'viewer' || view.dataset !== t.dataset.id) return;
      app.pendingView = null;
      applyView(view);
    };

    headActions.append(h('button.btn.small', { type: 'button', title: 'Keep what the viewer shows, to reopen it exactly', onclick: async () => {
      const name = await promptDialog({ title: 'Save view', label: 'Name', value: `${t.dataset.name}, view ${app.store.project.views.length + 1}`, confirm: 'Save' });
      if (!name) return;
      const view = captureNow(name);
      app.store.commit(addView(app.store.project, view), `Save view ${name}`);
      toast(`Saved ${name}. It is listed under Saved views; its link reopens it.`, { kind: 'ok' });
    } }, icon('save'), 'Save view'));

    // For the validation harness (validation/viewer.mjs) and the console.
    window.situcraftViewer = {
      renderer,
      desc,
      cells,
      dataset: t.dataset.id,
      // The selected cell's detail (internal/view), or null; the stage's size in CSS pixels.
      detail: () => state.detail,
      // The segmentation drawn (an artifact's id, null for the dataset's own), drawing another,
      // the cells table drawn, the choices, and the fill's colors per cell.
      segmentation: () => state.seg?.id ?? null,
      setSegmentation: (id) => drawSegmentation(id),
      segmentationReady: () => state.segReady,
      cellsDrawn: () => renderer.cells,
      choices: () => choices(),
      fillColors: () => (state.layers.cells.fill?.startsWith('clusters:') || parseLabelFill(state.layers.cells.fill) ? state.clusterFill?.rgba ?? null : cellFillColors(state.layers.cells.fill, renderer.cells, desc)),
      clusterFill: () => state.clusterFill,
      setCells: (patch) => {
        state.layers = { ...state.layers, cells: { ...state.layers.cells, ...patch } };
        renderer.setLayers(state.layers);
        mountPanel(state.layers);
        applyFill();
        updateLegend();
      },
      setChannelsVisible: (on) => {
        state.layers = { ...state.layers, channels: state.layers.channels.map((c) => ({ ...c, visible: on })) };
        renderer.setLayers(state.layers);
        mountPanel(state.layers);
      },
      pick: (x, y) => renderer.pick(x, y),
      readColor: (x, y) => renderer.readColor(x, y),
      stageSize: () => [stage.clientWidth, stage.clientHeight],
      layers: () => state.layers,
      plan: () => renderer.plan(),
      settled: () => renderer.settled(),
      setCamera: (cam) => renderer.setCamera(cam),
      cameraFor: (rect) => fitCamera(rect, ...renderer.size(), 0),
      setGenes: (genes) => panel.setGenes(genes),
      setTranscripts: (patch) => panel.setTranscripts(patch),
      rawCapture: (spec) => renderer.rawCapture(spec),
      captureView: (name) => captureNow(name),
      saveView: (name) => {
        const view = captureNow(name);
        app.store.commit(addView(app.store.project, view), `Save view ${name}`);
        return view;
      },
      loadedPoints: (rect) => renderer.loadedPoints(rect),
      select: (row) => selectRow(row),
      worldToScreen: (x, y) => worldToScreen(renderer.camera, ...renderer.size(), x, y),
      // Pans from one camera to another over `ms`, drawing every frame; reports the frame rate.
      async pan(from, to, ms = 3000) {
        const intervals = [];
        const draws = [];
        let last = null;
        const started = performance.now();
        await new Promise((resolve) => {
          const step = (now) => {
            const f = Math.min(1, (now - started) / ms);
            renderer.camera = { cx: from.cx + (to.cx - from.cx) * f, cy: from.cy + (to.cy - from.cy) * f, scale: from.scale * (to.scale / from.scale) ** f };
            const t0 = performance.now();
            renderer.draw();
            draws.push(performance.now() - t0);
            if (last !== null) intervals.push(now - last);
            last = now;
            if (f < 1) requestAnimationFrame(step);
            else resolve();
          };
          requestAnimationFrame(step);
        });
        intervals.sort((a, b) => a - b);
        const mean = intervals.reduce((a, b) => a + b, 0) / Math.max(1, intervals.length);
        return { frames: intervals.length + 1, fps: 1000 / mean, p95ms: intervals[Math.floor(0.95 * (intervals.length - 1))] ?? 0, drawMs: draws.reduce((a, b) => a + b, 0) / Math.max(1, draws.length) };
      },
      // The time from a new view to every tile it needs being drawn.
      async timeTo(cam) {
        const t0 = performance.now();
        renderer.setCamera(cam);
        await renderer.settled();
        return performance.now() - t0;
      },
    };
    // A saved view (or a tour's step) waiting for this dataset.
    state.takePending();
    return state;
  }

  // While the derived data are built, the message follows the job.
  function updateWaiting() {
    if (!waiting?.text) return;
    const job = app.bundles.cacheJob(waiting.item);
    waiting.text.textContent = job
      ? `${job.state === 'queued' ? 'Waiting to start' : `${Math.round(job.progress * 100)}%: ${job.message ?? ''}`}. The tissue shows here when they are ready.`
      : 'The tissue shows here when they are ready.';
  }

  show();
  return {
    update(topics) {
      if (topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('project')) show();
      if (topics.has('jobs') && waiting) updateWaiting();
      if (topics.has('colors') && current) {
        current.panel.recolor?.();
      }
      // Cells chosen in the QC view (a brushed plot) show here too.
      if (topics.has('selection') && current) current.showChosen();
      if (topics.has('project') && current) current.refreshChoices();
      // A saved view of the dataset shown.
      if (current && (topics.has('view-pending') || topics.has('focus'))) current.takePending();
    },
    destroy() {
      teardown();
      root.remove();
    },
  };
}
