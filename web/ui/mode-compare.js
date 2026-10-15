// The comparison workspace: two segmentations of a dataset (a comparison, internal/compare) on the
// same tissue, to see where and how they differ and to decide between them.
//
// - The tissue: A and B side by side with one camera, one over the other (overlay, each in its
//   colour), or in turns (flicker). Cells are filled by the class of their difference (the classes
//   chosen), the chosen difference highlighted; a map of the share of pixels the two give to
//   different cells, region by region.
// - The metrics: the comparison's headline numbers, with what they rest on when it is little.
// - The review queue: the differences ranked (by reassigned transcripts, contamination,
//   disagreement or low counts) among those the reviewer's rules keep; a difference opens where it
//   lies. The cell-matching table: every difference, sortable.
// - The decision: promote one segmentation as the dataset's default, keep one for exploration, or
//   reject one, with a reason, appended to the project's decisions (lib/segmentations.js).

import { h, icon, clear, isTyping } from './dom.js';
import { toast } from './overlays.js';
import { TissueRenderer } from '../render/renderer.js';
import { attachNavigation } from './navigation.js';
import { fitCamera, scaleBar, worldToScreen } from '../lib/viewport.js';
import { defaultLayers } from './viewer-panel.js';
import { categoricalColor, hexToRgb } from '../lib/colormaps.js';
import { comparisonsOf, decideSegmentation, DECISIONS, promotedSegmentation, segmentationDecision, segmentationsOf } from '../lib/segmentations.js';
import { CLASS_INFO, DIFFERING, RANKS, cellNames, prepare, rank, warnings } from '../lib/review.js';
import { cellFlags, flagsOf } from '../lib/refine.js';

const CLASSES = Object.keys(CLASS_INFO);
// A's and B's colours in the overlay (Okabe and Ito's palette, told apart with any colour vision).
const COLOR_A = '#56b4e9';
const COLOR_B = '#e69f00';
const PAGE = 100;

const classColor = (c) => categoricalColor(CLASSES.indexOf(c));
const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const percent = (v, digits = 2) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');
const num = (v, digits = 3) => (Number.isFinite(v) ? v.toLocaleString('en-US', { maximumFractionDigits: digits }) : '—');

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(body.error ?? `HTTP ${response.status}`), { status: response.status });
  return body;
}

// Per comparison, what the workspace shows, for the session.
const sessions = new Map();

export function mountCompare(app, container) {
  const title = h('span.crumbs');
  const headActions = h('div.btn-row.cmp-head-actions');
  const body = h('div.cmp-body');
  const root = h('div.view.compare', h('div.workbench-head', h('h1', icon('compare'), 'Compare', title), h('span.spacer'), headActions), body);
  container.append(root);
  let current = null; // the workspace open: { id, dispose, … }
  let generation = 0;
  let loading = null; // the comparison being opened
  let failed = null; // why the last one could not be

  function target() {
    const { project, ui } = app.store;
    const focusedCmp = ui.focus?.kind === 'comparison' ? project.artifacts.find((a) => a.kind === 'comparison' && a.id === ui.focus.id) : null;
    const seg = ui.focus?.kind === 'segmentation' ? project.artifacts.find((a) => a.kind === 'segmentation' && a.id === ui.focus.id) : null;
    const wanted = app.compareFocus ? project.artifacts.find((a) => a.kind === 'comparison' && a.id === app.compareFocus) : null;
    const id = wanted?.dataset ?? focusedCmp?.dataset ?? (ui.focus?.kind === 'dataset' ? ui.focus.id : seg?.dataset) ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    const list = comparisonsOf(project, dataset.id).filter((c) => c.state === 'complete');
    const comparison = wanted ?? focusedCmp ?? list.find((c) => c.id === current?.id) ?? list.at(-1) ?? null;
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), comparison, list };
  }

  function message(iconName, heading, text, actions = []) {
    teardown();
    clear(body).append(h('div.planned', h('div.empty', icon(iconName), h('h3', heading), h('p', text), actions.length ? h('div.btn-row', { style: { marginTop: '12px' } }, ...actions) : null)));
  }

  function teardown() {
    clear(headActions);
    current?.dispose();
    current = null;
    app.inspector.setSection('compare-selection', null);
  }

  async function show() {
    const t = target();
    app.compareFocus = null;
    if (!t) {
      title.textContent = '';
      message('compare', 'No dataset yet', 'Add a Xenium output bundle in the Data view, then compare its segmentations here.', [h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
      return;
    }
    title.textContent = ` · ${t.dataset.name}`;
    if (!t.comparison) {
      message('compare', 'No comparison yet', `Compare two segmentations of ${t.dataset.name} in the Segmentation view (expand the nuclei or import one to have two); the comparison opens here.`, [h('button.btn.primary', { type: 'button', onclick: () => app.setMode('segmentation') }, icon('segment'), 'Go to Segmentation')]);
      return;
    }
    if (current?.id === t.comparison.id || loading === t.comparison.id) return;
    if (!t.item) {
      message('compare', `${t.dataset.name} is not available`, 'Its bundle was not found where the project records it. Locate it in the Data view.');
      return;
    }
    const mine = (generation += 1);
    loading = t.comparison.id;
    message('compare', 'Opening the comparison…', 'Reading the tissue, both segmentations\' outlines and their differences.');
    let desc, outA, outB, cols, d;
    try {
      [desc, outA, outB, cols, d] = await Promise.all([
        getJSON(`${t.item.url}/view`),
        getJSON(`${t.item.url}/segmentations/${encodeURIComponent(t.comparison.a)}/outlines`),
        getJSON(`${t.item.url}/segmentations/${encodeURIComponent(t.comparison.b)}/outlines`),
        getJSON(`${t.item.url}/comparisons/${encodeURIComponent(t.comparison.id)}/groups`),
        app.bundles.comparisonDescription(t.comparison.id),
      ]);
    } catch (error) {
      if (loading === t.comparison.id) loading = null;
      failed = { id: t.comparison.id, error };
      if (mine !== generation) return;
      if (error.status === 409) {
        message('layers', 'Its derived data are being built', 'The workspace opens when they are ready.', [h('button.btn', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
        return;
      }
      message('warning', 'The comparison cannot be shown', error.message);
      return;
    }
    if (loading === t.comparison.id) loading = null;
    if (mine !== generation) return;
    teardown();
    clear(body);
    current = open(t, desc, outA, outB, prepare(cols), d);
  }

  function open(t, desc, outA, outB, prepared, d) {
    const cmp = t.comparison;
    const segs = segmentationsOf(app.store.project, t.dataset.id);
    const nameA = segs.find((s) => s.id === cmp.a)?.name ?? d.a?.name ?? 'A';
    const nameB = segs.find((s) => s.id === cmp.b)?.name ?? d.b?.name ?? 'B';
    const session = sessions.get(cmp.id) ?? { mode: 'side', map: false, classes: new Set(DIFFERING), by: 'reassigned', minChanged: 0, tab: 'queue', selected: null, page: 0, sort: { key: 'changed', dir: -1 }, camera: null, decision: 'promote', side: 'b' };
    sessions.set(cmp.id, session);
    // The limitations each side's method flags per cell (a transcript-informed segmentation's
    // diagnostics.json), shown with a chosen difference's cells.
    const flags = { a: new Map(), b: new Map() };
    for (const [side, id] of [['a', cmp.a], ['b', cmp.b]]) {
      if (!segs.find((x) => x.id === id)?.files?.some((f) => f.path === 'diagnostics.json')) continue;
      getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/diagnostics.json`).then((diag) => {
        flags[side] = cellFlags(diag);
        if (session.selected !== null && groups[session.selected]) app.inspector.setSection('compare-selection', () => selectionSection(groups[session.selected]));
      }).catch(() => {});
    }
    const { groups, ofA, ofB } = prepared;
    const state = { renderers: [], flicker: null, navs: [], observer: null, queue: [] };

    // --- The head: which comparison, how to show it ---
    const choose = h('select.input.small', { 'aria-label': 'Comparison' }, ...t.list.map((c) => h('option', { value: c.id }, `${segs.find((s) => s.id === c.a)?.name ?? 'A'} and ${segs.find((s) => s.id === c.b)?.name ?? 'B'}`)));
    choose.value = cmp.id;
    choose.addEventListener('change', () => {
      app.compareFocus = choose.value;
      show();
    });
    const modes = h('div.segmented', { role: 'group', 'aria-label': 'How to show A and B' }, ...[['side', 'Side by side'], ['overlay', 'Overlay'], ['flicker', 'Flicker']].map(([m, label]) => h(`button${session.mode === m ? '.active' : ''}`, { type: 'button', 'aria-pressed': String(session.mode === m), 'data-mode': m, onclick: () => setMode(m) }, label)));
    const mapToggle = h('button.btn.small.cmp-toggle', { type: 'button', 'aria-pressed': String(session.map), onclick: () => {
      session.map = !session.map;
      mapToggle.setAttribute('aria-pressed', String(session.map));
      colorCells();
      drawMaps();
    } }, icon('grid'), 'Disagreement map');
    headActions.append(t.list.length > 1 ? choose : null, modes, mapToggle);

    // --- The metrics, and what they rest on ---
    const A = d.metricsA ?? {};
    const B = d.metricsB ?? {};
    const chip = (label, value, help) => h('div.cmp-metric', { title: help }, h('span.muted', label), h('b', value));
    const cautions = warnings(d);
    const metrics = h('section.cmp-metrics', { 'aria-label': 'The comparison\'s measures' },
      chip('Pixels agree', percent(d.pixelAgreement), 'Pixels in the same cell, of those either gives a cell'),
      chip('Transcripts changed cell', percent(d.changesQ20?.fraction), `Gene transcripts (quality 20+): ${exact(d.changesQ20?.gained)} gained, ${exact(d.changesQ20?.lost)} lost, ${exact(d.changesQ20?.moved)} moved`),
      chip('Median IoU', num(d.iou?.median), `Of ${exact(d.iou?.pairs)} cells the same in both`),
      chip('Boundaries moved', Number.isFinite(d.displacement?.medianUm) ? `${d.displacement.medianUm.toFixed(2)} µm` : '—', 'Median, the area between the outlines over their perimeter'),
      chip('Spurious co-expression', `${num(A.spuriousCoexpression)} → ${num(B.spuriousCoexpression)}`, 'A, then B: co-expression of spurious gene pairs relative to nuclei alone (1); Jones et al. 2025'),
      chip('Mutually exclusive co-expression', `${num(A.mecr, 4)} → ${num(B.mecr, 4)}`, 'A, then B: the share of cells expressing either of two lineage markers that express both; Hartman and Satija 2024'),
      cautions.length ? h('details.cmp-cautions', h('summary', icon('warning'), `${cautions.length} caution${cautions.length === 1 ? '' : 's'}`), h('ul', ...cautions.map((w) => h('li', w)))) : null);

    // --- The tissue ---
    const legend = h('div.cmp-legend');
    const stage = h('div.cmp-stage');
    const scale = h('div.viewer-scale', h('div.bar'), h('span'));
    const flickerLabel = h('span.cmp-flicker-label');
    const tissue = h('section.cmp-tissue', stage, h('div.cmp-tissue-foot', scale, flickerLabel, h('span.spacer'), legend));

    // --- The side: queue, table, decision ---
    const tabs = h('div.cmp-tabs', { role: 'tablist' });
    const pane = h('div.cmp-pane');
    const side = h('aside.cmp-side', tabs, pane);
    body.append(metrics, h('div.cmp-main', tissue, side));

    function panes() {
      clear(stage);
      for (const r of state.renderers) r.renderer.destroy();
      for (const n of state.navs) n.dispose();
      state.renderers = [];
      state.navs = [];
      clearInterval(state.flicker);
      state.flicker = null;
      flickerLabel.textContent = '';
      state.observer?.disconnect();
      stage.dataset.mode = session.mode;
      const make = (which, { transparent = false, channels = true, color = '#e8e8e8' } = {}) => {
        const out = which === 'a' ? outA : outB;
        const canvas = h('canvas.viewer-canvas', { tabIndex: 0, 'aria-label': `The tissue with ${which === 'a' ? `A (${nameA})` : `B (${nameB})`}'s cells` });
        const heat = h('canvas.cmp-heat', { 'aria-hidden': 'true' });
        const renderer = new TissueRenderer(canvas, { transparent, onChange: (plan) => synced(renderer, plan) });
        renderer.setDataset({ ...desc, polygonsBase: out.polygonsBase, geometry: out.geometry }, { n: out.cells });
        const layers = defaultLayers(desc);
        if (!channels) for (const c of layers.channels) c.visible = false;
        layers.transcripts.visible = false;
        layers.cells = { outlines: true, nuclei: false, fill: 'class', opacity: 0.5, outlineColor: color };
        renderer.setLayers(layers);
        const entry = { which, canvas, heat, renderer };
        state.renderers.push(entry);
        return entry;
      };
      const label = (text, color) => h('div.cmp-pane-label', h('span.dot', { style: { background: color } }), text);
      if (session.mode === 'side') {
        const a = make('a');
        const b = make('b');
        stage.append(
          h('div.cmp-canvas', label(`A: ${nameA}`, '#e8e8e8'), a.canvas, a.heat),
          h('div.cmp-canvas', label(`B: ${nameB}`, '#e8e8e8'), b.canvas, b.heat));
        state.navs.push(attachNavigation(a.canvas, a.renderer, { onClick: (x, y) => pick(a, x, y) }), attachNavigation(b.canvas, b.renderer, { onClick: (x, y) => pick(b, x, y) }));
      } else if (session.mode === 'overlay') {
        const a = make('a', { color: COLOR_A });
        const b = make('b', { transparent: true, channels: false, color: COLOR_B });
        a.renderer.setLayers({ ...a.renderer.layers, cells: { ...a.renderer.layers.cells, fill: null } });
        stage.append(h('div.cmp-canvas.stacked', label(`A: ${nameA}`, COLOR_A), label(`B: ${nameB}`, COLOR_B), a.canvas, b.canvas, b.heat));
        state.navs.push(attachNavigation(b.canvas, b.renderer, { onClick: (x, y) => pick(b, x, y) }));
      } else {
        const a = make('a');
        const b = make('b');
        stage.append(h('div.cmp-canvas.stacked', a.canvas, b.canvas, b.heat));
        state.navs.push(attachNavigation(b.canvas, b.renderer, { onClick: (x, y) => pick(b, x, y) }));
        let showB = true;
        const flip = () => {
          showB = !showB;
          b.canvas.style.visibility = showB ? 'visible' : 'hidden';
          flickerLabel.textContent = showB ? `B: ${nameB}` : `A: ${nameA}`;
          flickerLabel.style.color = showB ? COLOR_B : COLOR_A;
        };
        flip();
        state.flicker = setInterval(flip, 800);
      }
      colorCells();
      state.observer = new ResizeObserver(() => {
        for (const r of state.renderers) {
          const box = r.canvas.getBoundingClientRect();
          if (box.width < 4) continue;
          r.renderer.resize(box.width, box.height);
          r.heat.width = r.canvas.width;
          r.heat.height = r.canvas.height;
        }
        // The whole tissue, fitted again as the canvases settle, until the camera moves.
        const first = state.renderers[0]?.renderer;
        const untouched = !session.camera || (session.fit && session.camera.cx === session.fit.cx && session.camera.cy === session.fit.cy && session.camera.scale === session.fit.scale);
        if (first && untouched) {
          const g = desc.grid;
          session.camera = session.fit = fitCamera({ x0: 0, y0: 0, x1: g.width * g.pixelSize, y1: g.height * g.pixelSize }, ...first.size(), 0.03);
        }
        if (session.camera) {
          for (const r of state.renderers) {
            r.renderer.setCamera(session.camera);
            r.placed = true;
          }
        }
        drawMaps();
      });
      for (const r of state.renderers) state.observer.observe(r.canvas);
    }

    // One camera for every canvas.
    function synced(from, plan) {
      // A canvas's camera counts once the workspace has placed it (before, it is the default).
      if (!plan || !state.renderers.find((r) => r.renderer === from)?.placed) return;
      const cam = from.camera;
      if (!session.camera || session.camera.cx !== cam.cx || session.camera.cy !== cam.cy || session.camera.scale !== cam.scale) {
        session.camera = { ...cam };
        for (const r of state.renderers) {
          if (r.renderer !== from) r.renderer.setCamera(cam);
        }
        drawMaps();
      }
      const s = scaleBar(plan.umPerCss);
      scale.querySelector('.bar').style.width = `${s.css}px`;
      scale.querySelector('span').textContent = s.label;
    }

    // Cells filled by their difference's class (the classes chosen); the chosen difference lit.
    function colorCells() {
      const fill = (n, of) => {
        const rgba = new Uint8Array(4 * n);
        for (let c = 0; c < n; c += 1) {
          const g = groups[of[c]];
          if (!g || !session.classes.has(g.class)) continue;
          const [r, gg, b] = hexToRgb(classColor(g.class));
          rgba.set([r, gg, b, 255], 4 * c);
        }
        return rgba;
      };
      for (const r of state.renderers) {
        const n = r.which === 'a' ? outA.cells : outB.cells;
        const of = r.which === 'a' ? ofA : ofB;
        if (r.renderer.layers.cells.fill) r.renderer.setCellColors(fill(n, of));
        const g = session.selected === null ? null : groups[session.selected];
        if (g) {
          const mask = new Uint8Array(n);
          for (const c of r.which === 'a' ? g.a : g.b) mask[c] = 1;
          r.renderer.setHighlight(mask, '#ffffff', 0.45);
        } else r.renderer.setHighlight(null);
      }
      clear(legend).append(...CLASSES.filter((c) => session.classes.has(c) && (d.classes?.[c] ?? 0) > 0).map((c) => h('span.cmp-legend-item', h('span.dot', { style: { background: classColor(c) } }), CLASS_INFO[c].label)),
        session.map ? h('span.cmp-legend-item', { title: 'Each 50 µm region\'s share of pixels the two give to different cells' }, h('span.cmp-map-key'), 'Disagreeing pixels, 0–100%') : null);
    }

    // The disagreement map: each region's share of pixels the two give to different cells.
    function drawMaps() {
      const g = d.regions;
      for (const r of state.renderers) {
        const ctx = r.heat.getContext('2d');
        ctx.clearRect(0, 0, r.heat.width, r.heat.height);
        if (!session.map || !g || !session.camera) continue;
        const [w, hgt] = [r.heat.width, r.heat.height];
        for (let row = 0; row < g.rows; row += 1) {
          for (let col = 0; col < g.cols; col += 1) {
            const k = row * g.cols + col;
            if (!(g.pixels[k] > 0) || !(g.disagreeingPixels[k] > 0)) continue;
            const f = g.disagreeingPixels[k] / g.pixels[k];
            const [x0, y0] = worldToScreen(session.camera, w, hgt, col * g.sizeUm, row * g.sizeUm);
            const [x1, y1] = worldToScreen(session.camera, w, hgt, (col + 1) * g.sizeUm, (row + 1) * g.sizeUm);
            if (x1 < 0 || y1 < 0 || x0 > w || y0 > hgt) continue;
            ctx.fillStyle = `rgba(240, 70, 60, ${(0.08 + 0.62 * Math.min(1, f)).toFixed(3)})`;
            ctx.fillRect(x0, y0, x1 - x0, y1 - y0);
          }
        }
      }
    }

    function setMode(m) {
      session.mode = m;
      for (const b of modes.querySelectorAll('button')) {
        b.classList.toggle('active', b.dataset.mode === m);
        b.setAttribute('aria-pressed', String(b.dataset.mode === m));
      }
      panes();
    }

    // A click: the difference of the cell under the pointer.
    function pick(entry, sx, sy) {
      const cell = entry.renderer.pick(sx, sy);
      if (cell < 0) return;
      const g = (entry.which === 'a' ? ofA : ofB)[cell];
      if (g >= 0) select(g, false);
    }

    // Chooses a difference: lit on the tissue, described in the inspector; fly: centred on it.
    function select(index, fly = true) {
      session.selected = index;
      const g = groups[index];
      colorCells();
      if (fly && g && state.renderers[0]) {
        const [w] = state.renderers[0].renderer.size();
        const cam = { cx: g.x, cy: g.y, scale: w / 70 };
        session.camera = cam;
        for (const r of state.renderers) r.renderer.setCamera(cam);
        drawMaps();
      }
      app.inspector.setSection('compare-selection', g ? () => selectionSection(g) : null);
      renderSide();
    }

    function selectionSection(g) {
      const names = cellNames(prepared, g, 8);
      const info = CLASS_INFO[g.class];
      const same = info.same && g.a.length === 1 && g.b.length === 1;
      return h('section.inspector-section',
        h('h3', icon('compare'), `${info.label} difference`),
        h('p.muted', { style: { margin: '0 0 8px', fontSize: '12px' } }, info.about),
        h('dl.kv',
          h('dt', 'Cells of A'), h('dd', names.a),
          h('dt', 'Cells of B'), h('dd', names.b),
          same ? [h('dt', 'IoU'), h('dd', num(g.iou)), h('dt', 'Boundary moved'), h('dd', `${num(g.displacement, 2)} µm`)] : null,
          g.a.length === 1 && g.b.length === 1 ? [h('dt', 'Transcripts, Jaccard'), h('dd', num(g.jaccard))] : null,
          h('dt', 'Reassigned'), h('dd', `${exact(g.changed)} gene transcripts`),
          h('dt', 'Gene transcripts'), h('dd', `A ${exact(g.transcriptsA)}, B ${exact(g.transcriptsB)}`),
          h('dt', 'Mixed markers'), h('dd', `A ${exact(g.mixedA)}, B ${exact(g.mixedB)}`, h('span.muted', ' pairs of exclusive markers in one cell')),
          h('dt', 'Where'), h('dd', `${num(g.x, 1)}, ${num(g.y, 1)} µm`),
          ...[['A', flagsOf(flags.a, g.a.map((i) => prepared.idsA[i]))], ['B', flagsOf(flags.b, g.b.map((i) => prepared.idsB[i]))]]
            .filter(([, text]) => text).flatMap(([side, text]) => [h('dt', `Limitations of ${side}`), h('dd', text)])));
    }

    // --- The side panel ---
    function renderSide() {
      clear(tabs);
      for (const [id, label] of [['queue', 'Review queue'], ['table', 'Cell matching'], ['decide', 'Decide']]) {
        tabs.append(h(`button.cmp-tab${session.tab === id ? '.active' : ''}`, { type: 'button', role: 'tab', 'aria-selected': String(session.tab === id), onclick: () => {
          session.tab = id;
          renderSide();
        } }, label));
      }
      clear(pane);
      if (session.tab === 'decide') {
        pane.append(...decidePanel());
        return;
      }
      pane.append(rules());
      state.queue = rank([...groups], { by: session.by, classes: session.classes, minChanged: session.minChanged });
      if (session.tab === 'queue') pane.append(...queuePanel());
      else pane.append(...tablePanel());
    }

    // The reviewer's rules: which classes, how many reassigned transcripts, in what order.
    function rules() {
      const chips = h('div.cmp-chips', ...CLASSES.filter((c) => (d.classes?.[c] ?? 0) > 0).map((c) => h(`button.cmp-chip${session.classes.has(c) ? '.on' : ''}`, { type: 'button', 'aria-pressed': String(session.classes.has(c)), title: CLASS_INFO[c].about, onclick: () => {
        if (session.classes.has(c)) session.classes.delete(c);
        else session.classes.add(c);
        session.page = 0;
        colorCells();
        renderSide();
      } }, h('span.dot', { style: { background: classColor(c) } }), `${CLASS_INFO[c].label} ${exact(d.classes[c])}`)));
      const by = h('select.input.small', { id: 'cmp-rank', 'aria-label': 'Rank by' }, ...Object.entries(RANKS).map(([k, r]) => h('option', { value: k, title: r.about }, r.label)));
      by.value = session.by;
      by.addEventListener('change', () => {
        session.by = by.value;
        session.page = 0;
        renderSide();
      });
      const min = h('input.input.small.cmp-min', { type: 'number', min: '0', step: '1', value: String(session.minChanged), 'aria-label': 'At least this many reassigned transcripts' });
      min.addEventListener('change', () => {
        session.minChanged = Math.max(0, Number(min.value) || 0);
        session.page = 0;
        renderSide();
      });
      return h('div.cmp-rules', chips, h('div.cmp-rule-row', h('label', { for: 'cmp-rank' }, 'Rank by'), by, h('span.muted', 'at least'), min, h('span.muted', 'reassigned')));
    }

    function queuePanel() {
      const q = state.queue;
      const at = q.findIndex((g) => g.index === session.selected);
      const step = (k) => {
        if (!q.length) return;
        const next = at < 0 ? (k > 0 ? 0 : q.length - 1) : Math.min(q.length - 1, Math.max(0, at + k));
        select(q[next].index);
      };
      const items = q.slice(0, 200).map((g, i) => {
        const names = cellNames(prepared, g);
        return h(`button.cmp-item${g.index === session.selected ? '.selected' : ''}`, { type: 'button', 'data-difference': g.index, onclick: () => select(g.index) },
          h('span.cmp-rank-no.mono', String(i + 1)),
          h('span.cmp-item-main', h('span', h('span.dot', { style: { background: classColor(g.class) } }), ` ${CLASS_INFO[g.class].label}`), h('span.muted', `${names.a} → ${names.b}`)),
          h('span.cmp-item-nums.mono', { title: 'Reassigned gene transcripts; mixed marker pairs in B' }, `${exact(g.changed)}${g.mixedB > g.mixedA ? ` · +${g.mixedB - g.mixedA}` : ''}`));
      });
      return [
        h('div.cmp-queue-head', h('span.muted', `${exact(q.length)} difference${q.length === 1 ? '' : 's'}${at >= 0 ? `; the ${(at + 1).toLocaleString('en-US')}${ordinal(at + 1)}` : ''}`), h('span.spacer'),
          h('button.btn.small', { type: 'button', title: 'The previous ( [ )', disabled: !q.length, onclick: () => step(-1) }, 'Previous'),
          h('button.btn.small.primary', { type: 'button', title: 'The next ( ] )', disabled: !q.length, onclick: () => step(1) }, 'Next')),
        q.length ? h('div.cmp-queue', ...items, q.length > 200 ? h('p.muted.seg-note', `The first 200 of ${exact(q.length)}; Next goes on through them all.`) : null) : h('p.muted.seg-note', 'No difference fits these rules.'),
      ];
    }

    function tablePanel() {
      const keys = [['class', 'Class'], ['a', 'A'], ['b', 'B'], ['iou', 'IoU'], ['jaccard', 'Jaccard'], ['changed', 'Reassigned'], ['displacement', 'Moved (µm)']];
      const value = (g, k) => (k === 'a' ? g.a.length : k === 'b' ? g.b.length : k === 'class' ? CLASSES.indexOf(g.class) : g[k]);
      const rows = [...state.queue].sort((p, q) => session.sort.dir * (value(p, session.sort.key) - value(q, session.sort.key)) || p.index - q.index);
      const pages = Math.max(1, Math.ceil(rows.length / PAGE));
      session.page = Math.min(session.page, pages - 1);
      const shown = rows.slice(session.page * PAGE, (session.page + 1) * PAGE);
      const head = h('tr', ...keys.map(([k, label]) => h(`th${k === 'class' || k === 'a' || k === 'b' ? '' : '.r'}`, { 'aria-sort': session.sort.key === k ? (session.sort.dir > 0 ? 'ascending' : 'descending') : 'none' },
        h('button.link', { type: 'button', onclick: () => {
          session.sort = { key: k, dir: session.sort.key === k ? -session.sort.dir : -1 };
          renderSide();
        } }, label))));
      const body_ = shown.map((g) => {
        const names = cellNames(prepared, g, 2);
        return h(`tr${g.index === session.selected ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, onclick: () => select(g.index), onkeydown: (e) => {
          if (e.key === 'Enter') select(g.index);
        } },
        h('td', h('span.dot', { style: { background: classColor(g.class) } }), ` ${CLASS_INFO[g.class].label}`), h('td', names.a), h('td', names.b),
        h('td.r', g.a.length === 1 && g.b.length === 1 ? num(g.iou) : '—'), h('td.r', num(g.jaccard)), h('td.r', exact(g.changed)), h('td.r', g.a.length === 1 && g.b.length === 1 ? num(g.displacement, 2) : '—'));
      });
      return [
        h('div', { style: { overflowX: 'auto' } }, h('table.data.cmp-table', h('thead', head), h('tbody', ...body_))),
        h('div.btn-row.cmp-pages', h('button.btn.small', { type: 'button', disabled: session.page === 0, onclick: () => {
          session.page -= 1;
          renderSide();
        } }, 'Previous'), h('span.muted', `${exact(rows.length)} differences; page ${session.page + 1} of ${pages}`), h('button.btn.small', { type: 'button', disabled: session.page >= pages - 1, onclick: () => {
          session.page += 1;
          renderSide();
        } }, 'Next')),
      ];
    }

    // The decision: about A or B, promote, keep or reject, with a reason; the dataset's decisions.
    function decidePanel() {
      const project = app.store.project;
      const promoted = promotedSegmentation(project, t.dataset.id)?.record.id;
      const statusOf = (id) => {
        if (id === promoted) return ['accent', 'The default'];
        const latest = segmentationDecision(project, t.dataset.id, id);
        if (latest?.kind === 'keep-segmentation') return ['', 'Kept for exploration'];
        if (latest?.kind === 'reject-segmentation') return ['danger', 'Rejected'];
        return ['', 'Not decided'];
      };
      const sides = [['a', cmp.a, nameA], ['b', cmp.b, nameB]].map(([key, id, name]) => {
        const [badge, label] = statusOf(id);
        return h('label.cmp-decide-side', h('input', { type: 'radio', name: 'cmp-side', value: key, checked: session.side === key, onchange: () => {
          session.side = key;
        } }), h('span', h('b', `${key.toUpperCase()}: ${name}`), ' ', h(`span.badge.${badge}`, label)));
      });
      const kinds = Object.entries(DECISIONS).map(([k, x]) => h('label.cmp-decide-kind', h('input', { type: 'radio', name: 'cmp-decision', value: k, checked: session.decision === k, onchange: () => {
        session.decision = k;
      } }), x.label));
      const reason = h('textarea.input.cmp-reason', { id: 'cmp-reason', rows: 3, placeholder: 'Why: what the comparison showed (recorded with the decision)' });
      reason.value = session.reason ?? '';
      reason.addEventListener('input', () => {
        session.reason = reason.value;
      });
      const record = h('button.btn.primary', { type: 'button', onclick: () => {
        const segmentation = session.side === 'a' ? cmp.a : cmp.b;
        try {
          const next = decideSegmentation(app.store.project, { dataset: t.dataset.id, segmentation, decision: session.decision, rationale: reason.value, comparison: cmp.id });
          app.store.commit(next, `${DECISIONS[session.decision].done} ${session.side === 'a' ? nameA : nameB}`);
          session.reason = '';
          toast(`${DECISIONS[session.decision].done} ${session.side === 'a' ? nameA : nameB}.`, { kind: 'ok' });
          renderSide();
        } catch (error) {
          toast(error.message, { kind: 'error' });
        }
      } }, icon('check'), 'Record the decision');
      const names = new Map(segmentationsOf(project, t.dataset.id).map((s) => [s.id, s.name]));
      const log = project.decisions.filter((x) => x.dataset === t.dataset.id).slice().reverse().map((x) => h('li', h('b', `${Object.values(DECISIONS).find((k) => k.kind === x.kind)?.done ?? x.kind} ${names.get(x.artifact) ?? x.artifact}`), h('span.muted', ` · ${new Date(x.time).toLocaleString()} · ${x.by}`), h('div', x.rationale)));
      return [
        h('div.cmp-decide',
          h('p.muted.seg-note', 'Promoting makes a segmentation the dataset\'s default, which later analyses use; keeping marks one for exploration; rejecting sets one aside. Decisions are appended to the project with their reason and never removed; no segmentation is deleted.'),
          h('fieldset.cmp-fieldset', h('legend', 'Which'), ...sides),
          h('fieldset.cmp-fieldset', h('legend', 'Decision'), ...kinds),
          h('label.field', { for: 'cmp-reason' }, h('span', 'Reason'), reason),
          h('div.btn-row', record)),
        h('div.section-title', 'Decisions about these segmentations'),
        log.length ? h('ol.cmp-log', ...log) : h('p.muted.seg-note', 'None yet.'),
      ];
    }

    const keys = (event) => {
      if (isTyping(event) || app.store.ui.mode !== 'compare') return;
      if (event.key === ']' || event.key === '[') {
        event.preventDefault();
        const q = state.queue;
        if (!q.length) return;
        const at = q.findIndex((g) => g.index === session.selected);
        const k = event.key === ']' ? 1 : -1;
        select(q[at < 0 ? (k > 0 ? 0 : q.length - 1) : Math.min(q.length - 1, Math.max(0, at + k))].index);
      }
    };
    window.addEventListener('keydown', keys);

    panes();
    renderSide();
    if (session.selected !== null) app.inspector.setSection('compare-selection', () => selectionSection(groups[session.selected]));

    const workspace = {
      id: cmp.id,
      dataset: t.dataset,
      state,
      session,
      groups,
      prepared,
      description: d,
      names: [nameA, nameB],
      setMode,
      select,
      setTab(id) {
        session.tab = id;
        renderSide();
      },
      setRules(rules) {
        if (rules.classes) session.classes = new Set(rules.classes);
        if (rules.by) session.by = rules.by;
        if (rules.minChanged !== undefined) session.minChanged = rules.minChanged;
        if (rules.map !== undefined) session.map = rules.map;
        mapToggle.setAttribute('aria-pressed', String(session.map));
        colorCells();
        drawMaps();
        renderSide();
        return state.queue;
      },
      dispose() {
        window.removeEventListener('keydown', keys);
        clearInterval(state.flicker);
        state.observer?.disconnect();
        for (const n of state.navs) n.dispose();
        for (const r of state.renderers) r.renderer.destroy();
      },
    };
    return workspace;
  }

  show();
  // The remote actions' way in (ui/remote.js).
  app.compareView = {
    current: () => current,
    open: async (comparisonId) => {
      failed = null;
      if (current?.id !== comparisonId) {
        app.compareFocus = comparisonId;
        if (loading !== comparisonId) teardown();
        show();
      }
      // Until it opens (or cannot).
      for (let k = 0; k < 1200 && current?.id !== comparisonId && failed?.id !== comparisonId; k += 1) await new Promise((r) => setTimeout(r, 50));
      if (failed?.id === comparisonId) throw failed.error;
      return current?.id === comparisonId ? current : null;
    },
  };
  return {
    update(topics) {
      if (topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || (topics.has('project') && !current)) show();
      else if (topics.has('project') && current?.session.tab === 'decide') current.select(current.session.selected, false);
    },
    destroy() {
      teardown();
      delete app.compareView;
      root.remove();
    },
  };
}

function ordinal(n) {
  const s = n % 100;
  if (s >= 11 && s <= 13) return 'th';
  return { 1: 'st', 2: 'nd', 3: 'rd' }[n % 10] ?? 'th';
}

