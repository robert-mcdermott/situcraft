// The QC view: whether the run supports analysis, and where it does not.
//
// - Metrics: the run's recorded metrics, each explained, beside SituCraft's recomputation from the
//   bundle's data (or why there is none).
// - The tissue with a QC map: per cell (transcripts, genes, areas, segmentation method…) or per
//   density bin (gene transcripts outside cells, of quality below 20, negative controls).
// - Plots (a histogram, box plots by segmentation method, a scatter plot): dragging across one
//   chooses cells, which the tissue (here and in the Viewer) and the inspector then show.
// - Filters: conditions on the cells' attributes, their effects counted as they are edited, saved
//   as named versions in the project and compared; applying a saved one again is checked to keep
//   the same cells. A filter never changes the data.

import { h, icon, clear, formatCount } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { addView, captureView, viewCamera } from '../lib/views.js';
import { TissueRenderer } from '../render/renderer.js';
import { fitCamera, scaleBar } from '../lib/viewport.js';
import { readCells } from '../lib/viewer-data.js';
import { ATTRIBUTES, attribute, attributeValues } from '../lib/cell-attributes.js';
import { addFilter, compareKeeps, createFilter, describeCondition, effect, evaluate, removeFilter, verifyFilter, versions } from '../lib/filters.js';
import { countMarked, selectRange, selectRect } from '../lib/plots.js';
import { METRIC_GROUPS, formatMetric, metricRows } from '../lib/metrics.js';
import { categoricalColor, colormapCSS, colormapLUT } from '../lib/colormaps.js';
import { attributeRange } from '../lib/viewer-data.js';
import { defaultLayers } from './viewer-panel.js';
import { attachNavigation } from './navigation.js';
import { boxPlot, histogramPlot, scatterPlot } from './qc-plots.js';

// Per dataset id, what the view shows, for the session.
const sessions = new Map();

// The QC maps. Cell maps color cells by an attribute; bin maps color density bins.
function qcMaps(desc) {
  const n = desc.transcripts?.genes.length ?? 0;
  const pseudo = (name) => n + (desc.transcripts?.pseudo ?? []).findIndex((p) => p.name === name);
  const maps = [{ id: 'none', label: 'No map' }];
  for (const a of ATTRIBUTES) maps.push({ id: `cell:${a.id}`, label: `Cells: ${a.label.toLowerCase()}`, cell: a.id });
  if (desc.transcripts?.pseudo?.length) {
    maps.push(
      { id: 'bin:outside', label: 'Gene transcripts outside cells', bin: { ratio: true, complement: true, num: pseudo('Gene transcripts in a cell'), den: pseudo('Gene transcripts'), q20Num: true, q20Den: true, minDen: 4, range: [0, 0.5] }, legend: ['0%', '50% or more'], help: 'The share of each bin\'s gene transcripts (quality 20 or more) not assigned to a cell. High where cells are missed or boundaries are too tight.' },
      { id: 'bin:quality', label: 'Gene transcripts below quality 20', bin: { ratio: true, complement: true, num: pseudo('Gene transcripts'), den: pseudo('Gene transcripts'), q20Num: true, q20Den: false, minDen: 4, range: [0, 0.3] }, legend: ['0%', '30% or more'], help: 'The share of each bin\'s gene transcripts with quality below 20. High where imaging or chemistry was poor.' },
      { id: 'bin:controls', label: 'Negative-control density', bin: { ratio: false, num: pseudo('Negative controls'), q20Num: false, range: [0, 1] }, scale: pseudo('Negative controls'), legend: ['few', 'many'], help: 'Where negative control probes and codewords are detected (of any quality): they should be rare and scattered.' },
      { id: 'bin:density', label: 'Gene transcript density', bin: { ratio: false, num: pseudo('Gene transcripts'), q20Num: true, range: [0, 1] }, scale: pseudo('Gene transcripts'), legend: ['few', 'many'], help: 'Gene transcripts of quality 20 or more per bin, on a log scale.' },
    );
  }
  return maps;
}

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(body.error ?? `HTTP ${response.status}`), { status: response.status });
  return body;
}

export function mountQC(app, container) {
  const body = h('div.qc-body');
  const title = h('span.crumbs');
  const headActions = h('div.btn-row');
  const root = h('div.view.qc', h('div.workbench-head', h('h1', icon('qc'), 'QC', title), h('span.spacer'), headActions), body);
  container.append(root);
  let current = null;
  let generation = 0;
  let loading = null;

  function target() {
    const { project, ui } = app.store;
    // A dataset in focus, or a filter's or a saved view's dataset, else the first.
    const filter = ui.focus?.kind === 'filter' ? project.artifacts.find((a) => a.id === ui.focus.id) : null;
    const view = ui.focus?.kind === 'view' ? project.views.find((v) => v.id === ui.focus.id) : null;
    const id = ui.focus?.kind === 'dataset' ? ui.focus.id : filter?.dataset ?? view?.dataset ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path) };
  }

  function message(iconName, heading, text, actions = []) {
    teardown();
    clear(body);
    body.append(h('div.planned', h('div.empty', icon(iconName), h('h3', heading), h('p', text), actions.length ? h('div.btn-row', { style: { marginTop: '12px' } }, ...actions) : null)));
  }

  function teardown() {
    clear(headActions);
    if (!current) return;
    current.dispose();
    current = null;
    app.inspector.setSection('qc-selection', null);
  }

  async function show() {
    const t = target();
    if (!t) {
      generation += 1;
      title.textContent = '';
      message('qc', 'No dataset yet', 'Add a Xenium output bundle to the project in the Data view; its quality shows here once its derived data are built.', [h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
      return;
    }
    title.textContent = ` · ${t.dataset.name}`;
    if (current?.dataset.id === t.dataset.id && current.item?.path === t.item?.path) return;
    const key = `${t.dataset.id}|${t.item?.path}`;
    if (loading === key) return;
    const mine = (generation += 1);
    if (!t.item) {
      message('qc', `${t.dataset.name} is not available`, 'Its bundle was not found where the project records it. Locate it in the Data view.');
      return;
    }
    message('qc', `Opening ${t.dataset.name}…`, 'Reading its metrics, cells and description.');
    loading = key;
    let desc;
    let cells;
    let report;
    try {
      desc = await getJSON(`${t.item.url}/view`);
      const response = await fetch(`${t.item.url}/cells`);
      if (!response.ok) throw new Error(`cells: HTTP ${response.status}`);
      cells = readCells(await response.arrayBuffer());
      report = await app.bundles.validate(t.item);
    } catch (error) {
      if (mine !== generation) return;
      if (error.status === 409) {
        if (!app.bundles.cacheJob(t.item)) app.bundles.buildCache(t.item);
        message('layers', 'Its derived data are being built', 'The QC maps and plots show here when they are ready.', [h('button.btn', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
        return;
      }
      message('warning', `${t.dataset.name} cannot be shown`, error.message);
      return;
    } finally {
      if (loading === key) loading = null;
    }
    if (mine !== generation) return;
    teardown();
    clear(body);
    current = open(t, desc, cells, report);
  }

  function open(t, desc, cells, report) {
    const session = sessions.get(t.dataset.id) ?? {
      tab: 'metrics', map: 'cell:transcripts', hist: 'transcripts', histLog: false, box: 'transcripts', scatterX: 'area', scatterY: 'transcripts',
      draft: [{ attribute: 'transcripts', min: 10, max: null }], name: 'QC filter', active: null, showRemoved: false, compare: [null, null],
    };
    sessions.set(t.dataset.id, session);
    if (app.store.ui.focus?.kind === 'filter') session.tab = 'filters';
    const methods = desc.cells.methods ?? [];
    const state = { dataset: t.dataset, item: t.item, desc, cells, report, plots: [], renderer: null };

    // --- The tissue ---
    const canvas = h('canvas.viewer-canvas', { tabIndex: 0, 'aria-label': `Tissue of ${t.dataset.name} with the chosen QC map` });
    const legend = h('div.qc-legend');
    const scale = h('div.viewer-scale', h('div.bar'), h('span'));
    const chosen = h('div.qc-chosen');
    const mapSelect = h('select', { 'aria-label': 'QC map' }, ...qcMaps(desc).map((m) => h('option', { value: m.id }, m.label)));
    mapSelect.value = session.map;
    const stage = h('div.viewer-stage.qc-stage', canvas, h('div.viewer-foot', scale));
    const tissue = h('section.qc-tissue', h('div.qc-toolbar', h('label', 'Map', mapSelect), h('span.spacer'), chosen), stage, legend);
    const renderer = new TissueRenderer(canvas, { onChange: (plan) => {
      if (!plan) return;
      const s = scaleBar(plan.umPerCss);
      scale.querySelector('.bar').style.width = `${s.css}px`;
      scale.querySelector('span').textContent = s.label;
    } });
    renderer.setDataset(desc, cells);
    state.renderer = renderer;
    const layers = defaultLayers(desc);
    layers.transcripts.visible = false;
    layers.cells = { outlines: true, nuclei: false, fill: null, opacity: 0.75 };
    renderer.setLayers(layers);
    const navigation = attachNavigation(canvas, renderer, {});
    const observer = new ResizeObserver(() => {
      const r = stage.getBoundingClientRect();
      renderer.resize(r.width, r.height);
      if (!state.placed && r.width > 10) {
        state.placed = true;
        const g = desc.grid;
        renderer.setCamera(session.camera ?? fitCamera({ x0: 0, y0: 0, x1: g.width * g.pixelSize, y1: g.height * g.pixelSize }, ...renderer.size(), 0.03));
      }
    });
    observer.observe(stage);

    // The cells the view counts: all, or those a filter in use keeps.
    const keepOf = () => {
      if (!session.active) return null;
      const f = app.store.project.artifacts.find((a) => a.id === session.active);
      return f ? verifyFilter(f, cells).keep : null;
    };
    let keep = keepOf();

    function applyMap() {
      const map = qcMaps(desc).find((m) => m.id === session.map) ?? qcMaps(desc)[0];
      clear(legend);
      renderer.setQCMap(null);
      renderer.setCellColors(null);
      layers.cells.fill = null;
      if (map.cell) {
        const a = attribute(map.cell);
        const values = attributeValues(cells, a.id);
        const rgba = new Uint8Array(4 * cells.n);
        if (a.categorical) {
          for (let i = 0; i < cells.n; i += 1) {
            if (!Number.isFinite(values[i]) || (keep && !keep[i])) continue;
            const v = parseInt(categoricalColor(values[i]).slice(1), 16);
            rgba.set([(v >> 16) & 255, (v >> 8) & 255, v & 255, 255], 4 * i);
          }
          legend.append(h('div', h('b', a.label), ...methods.map((m, i) => h('div.viewer-legend-item', h('span.dot', { style: { background: categoricalColor(i) } }), m.replace(/^Segmented by /, '')))));
        } else {
          const [lo, hi] = attributeRange(keep ? Float64Array.from(values, (v, i) => (keep[i] ? v : NaN)) : values);
          const lut = colormapLUT('viridis');
          for (let i = 0; i < cells.n; i += 1) {
            if (!Number.isFinite(values[i]) || (keep && !keep[i])) continue;
            const k = Math.max(0, Math.min(255, Math.round((255 * (values[i] - lo)) / (hi - lo))));
            rgba.set([lut[3 * k], lut[3 * k + 1], lut[3 * k + 2], 255], 4 * i);
          }
          const f = (v) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(2).replace(/\.?0+$/, ''));
          legend.append(h('div', h('b', `${a.label}${a.unit ? ` (${a.unit})` : ''}`), h('div.viewer-legend-bar', { style: { background: colormapCSS('viridis') } }), h('div.viewer-legend-range', h('span', f(lo)), h('span', f(hi))), h('div.muted', a.help)));
        }
        layers.cells.fill = a.id;
        renderer.setCellColors(rgba);
      } else if (map.bin) {
        renderer.setQCMap({ ...map.bin, scale: map.scale !== undefined ? desc.transcripts.scale[map.scale] : 1, lut: colormapLUT(map.id === 'bin:controls' ? 'magma' : 'viridis'), opacity: 0.85 });
        legend.append(h('div', h('b', map.label), h('div.viewer-legend-bar', { style: { background: colormapCSS(map.id === 'bin:controls' ? 'magma' : 'viridis') } }), h('div.viewer-legend-range', h('span', map.legend[0]), h('span', map.legend[1])), h('div.muted', map.help)));
      }
      renderer.setLayers({ ...layers, cells: { ...layers.cells } });
    }
    mapSelect.addEventListener('change', () => {
      session.map = mapSelect.value;
      applyMap();
    });

    // --- The chosen cells (brushing) ---
    function choose(mask, label) {
      app.selection = mask ? { dataset: t.dataset.id, mask, label, count: countMarked(mask) } : null;
      app.store.notify(['selection']);
    }

    function showChosen() {
      const sel = app.selection?.dataset === t.dataset.id ? app.selection : null;
      // The cells the filter being edited removes, when asked.
      const removed = session.showRemoved && state.draftKeep ? Uint8Array.from(state.draftKeep, (k) => (k ? 0 : 1)) : null;
      if (sel) renderer.setHighlight(sel.mask, '#ff4fd8', 0.85);
      else if (removed) renderer.setHighlight(removed, '#ff4d4d', 0.7);
      else renderer.setHighlight(null);
      clear(chosen);
      if (sel) {
        chosen.append(h('span.badge.accent', `${formatCount(sel.count)} cells chosen (${((100 * sel.count) / cells.n).toFixed(1)}%)`), h('span.muted', sel.label),
          sel.condition ? h('button.btn.small', { type: 'button', title: 'Add the brushed range to the filter being edited', onclick: () => {
            session.draft.push(sel.condition);
            session.tab = 'filters';
            renderSide();
          } }, icon('filter'), 'Add as a condition') : null,
          h('button.btn.small.ghost', { type: 'button', onclick: () => choose(null) }, icon('close'), 'Clear'));
      }
      for (const p of state.plots) p.setSelection(sel?.mask ?? null);
      app.inspector.setSection('qc-selection', sel ? () => selectionSection(sel) : null);
    }

    function selectionSection(sel) {
      const rows = ['transcripts', 'genes', 'area', 'nuclearShare'].map((id) => {
        const a = attribute(id);
        const e = effect(sel.mask, attributeValues(cells, id));
        const f = (v) => (Number.isFinite(v) ? (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(2)) : '—');
        return [h('dt', `${a.label} (median)`), h('dd', `${f(e.keptMedian)} chosen, ${f(e.allMedian)} all`)];
      });
      return h('section.inspector-section', h('h3', icon('qc'), 'Chosen cells'),
        h('dl.kv', h('dt', 'Cells'), h('dd', `${formatCount(sel.count)} of ${formatCount(cells.n)}`), h('dt', 'Chosen by'), h('dd', sel.label), ...rows));
    }

    // --- Plots ---
    const plotsHost = h('section.qc-plots');
    const attrOptions = (selected, filter = () => true) => ATTRIBUTES.filter(filter).map((a) => h('option', { value: a.id, selected: a.id === selected }, a.label));
    function renderPlots() {
      for (const p of state.plots) p.dispose();
      state.plots = [];
      clear(plotsHost);
      const numeric = (a) => !a.categorical;
      const sel = app.selection?.dataset === t.dataset.id ? app.selection.mask : null;
      // Histogram.
      const histAttr = attribute(session.hist);
      const histSelect = h('select', { 'aria-label': 'Histogram of' }, ...attrOptions(session.hist, numeric));
      const logBox = h('input', { type: 'checkbox', checked: session.histLog });
      histSelect.addEventListener('change', () => {
        session.hist = histSelect.value;
        renderPlots();
      });
      logBox.addEventListener('change', () => {
        session.histLog = logBox.checked;
        renderPlots();
      });
      const hist = histogramPlot(attributeValues(cells, histAttr.id), {
        label: histAttr.label, integer: histAttr.integer, log: session.histLog, keep, selection: sel,
        onBrush: (lo, hi) => {
          if (lo > hi) return choose(null);
          const a = histAttr.integer ? Math.ceil(lo) : lo;
          const b = histAttr.integer ? Math.floor(hi) : hi;
          const label = `${histAttr.label} from ${formatNum(a)} to ${formatNum(b)}`;
          const mask = selectRange(attributeValues(cells, histAttr.id), a, b, keep);
          app.selection = { dataset: t.dataset.id, mask, label, count: countMarked(mask), condition: { attribute: histAttr.id, min: round(a), max: round(b) } };
          app.store.notify(['selection']);
        },
      });
      // Box plots by segmentation method.
      const boxAttr = attribute(session.box);
      const boxSelect = h('select', { 'aria-label': 'Box plots of' }, ...attrOptions(session.box, numeric));
      boxSelect.addEventListener('change', () => {
        session.box = boxSelect.value;
        renderPlots();
      });
      const groups = methods.map((m, i) => ({ label: m.replace(/^Segmented by /, ''), index: i, color: categoricalColor(i) }));
      const box = groups.length ? boxPlot(attributeValues(cells, boxAttr.id), attributeValues(cells, 'method'), groups, {
        label: boxAttr.label, groupLabel: 'segmentation method', keep, selection: sel,
        onBrush: (lo, hi) => {
          if (lo > hi) return choose(null);
          const mask = selectRange(attributeValues(cells, boxAttr.id), lo, hi, keep);
          app.selection = { dataset: t.dataset.id, mask, label: `${boxAttr.label} from ${formatNum(lo)} to ${formatNum(hi)}`, count: countMarked(mask), condition: { attribute: boxAttr.id, min: round(lo), max: round(hi) } };
          app.store.notify(['selection']);
        },
      }) : null;
      // Scatter plot.
      const xSelect = h('select', { 'aria-label': 'Scatter plot: horizontal' }, ...attrOptions(session.scatterX, numeric));
      const ySelect = h('select', { 'aria-label': 'Scatter plot: vertical' }, ...attrOptions(session.scatterY, numeric));
      xSelect.addEventListener('change', () => {
        session.scatterX = xSelect.value;
        renderPlots();
      });
      ySelect.addEventListener('change', () => {
        session.scatterY = ySelect.value;
        renderPlots();
      });
      const xa = attribute(session.scatterX);
      const ya = attribute(session.scatterY);
      const scatter = scatterPlot(attributeValues(cells, xa.id), attributeValues(cells, ya.id), {
        xLabel: xa.label, yLabel: ya.label, keep, selection: sel,
        onBrush: (r) => {
          if (!r) return choose(null);
          const mask = selectRect(attributeValues(cells, xa.id), attributeValues(cells, ya.id), r.x0, r.x1, r.y0, r.y1, keep);
          choose(mask, `${xa.label} ${formatNum(r.x0)}–${formatNum(r.x1)}, ${ya.label.toLowerCase()} ${formatNum(r.y0)}–${formatNum(r.y1)}`);
        },
      });
      state.plots = [hist, box, scatter].filter(Boolean);
      const card = (heading, controls, plot, hint) => h('div.pane.qc-card', h('h3', heading, h('span.spacer'), ...controls), plot.el, h('p.muted.qc-hint', hint));
      plotsHost.append(
        card('Distribution', [histSelect, h('label.viewer-toggle', logBox, 'Log')], hist, 'Drag across bars to choose those cells; click to clear.'),
        box ? card('By segmentation method', [boxSelect], box, 'Drag up or down to choose cells in a range.') : null,
        card('Two attributes', [xSelect, ySelect], scatter, 'Drag a rectangle to choose cells.'));
    }

    // --- Side: metrics and filters ---
    const side = h('aside.qc-side');
    function renderSide() {
      clear(side);
      const tabs = h('div.qc-tabs', { role: 'tablist' },
        ...[['metrics', 'Run metrics'], ['filters', 'Filters']].map(([id, label]) => h(`button.qc-tab${session.tab === id ? '.active' : ''}`, { type: 'button', role: 'tab', 'aria-selected': String(session.tab === id), onclick: () => {
          session.tab = id;
          renderSide();
        } }, label)));
      side.append(tabs, session.tab === 'metrics' ? metricsPanel() : filtersPanel());
      if (session.showRemoved && state.renderer) showChosen();
    }

    function metricsPanel() {
      const rows = metricRows(report.summary.runMetrics, report.summary.metrics);
      const checked = rows.filter((r) => r.agrees !== null);
      const agree = checked.filter((r) => r.agrees).length;
      const panel = h('div.qc-metrics',
        h('p.qc-intro', `${agree} of the ${checked.length} metrics SituCraft can recompute from the bundle agree with what the run recorded. Each explains what it measures and how it is computed.`));
      for (const group of [...METRIC_GROUPS, { id: 'other', label: 'Other' }]) {
        const mine = rows.filter((r) => r.group === group.id);
        if (!mine.length) continue;
        panel.append(h('h4', group.label));
        for (const r of mine) {
          const badge = r.agrees === null ? h('span.badge', { title: r.why }, 'As recorded') : r.agrees ? h('span.badge.ok', { title: 'SituCraft\'s recomputation agrees' }, 'Reproduced') : h('span.badge.danger', 'Differs');
          panel.append(h('details.qc-metric',
            h('summary', h('span.name', r.label), h('span.value.mono', `${formatMetric(r.recorded, r.format)}${r.unit ? ` ${r.unit}` : ''}`), badge),
            h('p', r.help),
            r.how ? h('p.muted', h('b', 'Recomputed: '), r.how, r.computed !== null ? ` SituCraft gets ${formatMetric(r.computed, r.format)}.` : '') : h('p.muted', r.why),
            h('p.mono.muted.qc-metric-name', r.name)));
        }
      }
      return panel;
    }

    function filtersPanel() {
      const panel = h('div.qc-filters');
      const draft = session.draft;
      const result = evaluate(draft, cells);
      const transcripts = effect(result.keep, attributeValues(cells, 'transcripts'));
      // The draft's conditions.
      const list = h('div.qc-conditions');
      draft.forEach((c, k) => {
        const a = attribute(c.attribute);
        const attrSelect = h('select', { 'aria-label': 'Attribute' }, ...ATTRIBUTES.map((x) => h('option', { value: x.id, selected: x.id === c.attribute }, x.label)));
        attrSelect.addEventListener('change', () => {
          const next = attribute(attrSelect.value);
          draft[k] = next.categorical ? { attribute: next.id, values: methods.map((_, i) => i) } : { attribute: next.id, min: null, max: null };
          renderSide();
        });
        let bounds;
        if (a.categorical) {
          bounds = h('div.qc-values', ...methods.map((m, i) => {
            const box = h('input', { type: 'checkbox', checked: c.values.includes(i) });
            box.addEventListener('change', () => {
              c.values = box.checked ? [...new Set([...c.values, i])].sort() : c.values.filter((v) => v !== i);
              renderSide();
            });
            return h('label.viewer-toggle', box, m.replace(/^Segmented by /, ''));
          }));
        } else {
          const input = (key, label) => {
            const el = h('input.qc-number', { type: 'number', step: 'any', placeholder: label, value: c[key] ?? '', 'aria-label': `${a.label}: ${label}` });
            el.addEventListener('change', () => {
              c[key] = el.value === '' ? null : Number(el.value);
              renderSide();
            });
            return el;
          };
          bounds = h('div.qc-bounds', input('min', 'at least'), h('span.muted', 'to'), input('max', 'at most'), a.unit ? h('span.muted', a.unit) : null);
        }
        list.append(h('div.qc-condition', h('div.row', attrSelect,
          h('button.icon-button', { type: 'button', title: 'Remove this condition', 'aria-label': 'Remove this condition', onclick: () => {
            draft.splice(k, 1);
            renderSide();
          } }, icon('close'))), bounds,
          h('p.muted.qc-hint', `${describeCondition(c, methods)}: ${formatCount(result.failing[k])} cells fail it, ${formatCount(result.only[k])} only this one.`)));
      });
      const showRemoved = h('input', { type: 'checkbox', checked: session.showRemoved });
      showRemoved.addEventListener('change', () => {
        session.showRemoved = showRemoved.checked;
        state.draftKeep = result.keep;
        showChosen();
      });
      const name = h('input.qc-name', { type: 'text', value: session.name, 'aria-label': 'Filter name' });
      name.addEventListener('change', () => {
        session.name = name.value.trim() || 'QC filter';
      });
      panel.append(
        h('p.qc-intro', 'Conditions every kept cell meets. A filter never changes the data: saved, it records which cells it keeps, so that it can be checked to keep the same cells again.'),
        list,
        h('button.btn.small', { type: 'button', onclick: () => {
          draft.push({ attribute: 'genes', min: 5, max: null });
          renderSide();
        } }, icon('plus'), 'Add a condition'),
        h('div.qc-effect',
          h('div', h('b', `Keeps ${formatCount(result.kept)} of ${formatCount(cells.n)} cells`), ` (${((100 * result.kept) / Math.max(1, cells.n)).toFixed(1)}%)`),
          h('div.muted', `Their gene transcripts: ${((100 * transcripts.keptSum) / Math.max(1, transcripts.allSum)).toFixed(1)}% of all in cells; median per cell ${formatNum(transcripts.keptMedian)} (all cells: ${formatNum(transcripts.allMedian)}).`),
          h('label.viewer-toggle', showRemoved, 'Show the cells it removes in the tissue (red)')),
        h('div.qc-save', name, h('button.btn.small.primary', { type: 'button', onclick: () => saveDraft() }, icon('save'), 'Save version')));
      // Saved filters, by name, with their versions.
      const saved = app.store.project.artifacts.filter((a) => a.kind === 'filter' && a.dataset === t.dataset.id);
      const names = [...new Set(saved.map((f) => f.name))];
      if (names.length) panel.append(h('h4', 'Saved filters'));
      for (const n of names) {
        for (const f of versions(app.store.project, t.dataset.id, n)) {
          const check = verifyFilter(f, cells);
          const active = session.active === f.id;
          panel.append(h(`div.qc-saved${active ? '.active' : ''}`,
            h('div.row', h('b', `${f.name} · version ${f.version}`), h('span.spacer'),
              check.same ? h('span.badge.ok', { title: 'Applied again, it keeps exactly the cells it recorded (SHA-256 of which cells)' }, 'Reproduced') : h('span.badge.danger', { title: 'The cells it keeps now differ from those it recorded' }, 'Differs')),
            h('div.muted', f.conditions.map((c) => describeCondition(c, methods)).join('; ') || 'No conditions'),
            h('div.muted', `Keeps ${formatCount(f.result.kept)} of ${formatCount(f.result.cells)} cells · ${new Date(f.created).toLocaleString()}`),
            h('div.btn-row',
              h('button.btn.small', { type: 'button', 'aria-pressed': String(active), onclick: () => {
                session.active = active ? null : f.id;
                refreshKeep();
              } }, icon('filter'), active ? 'Stop using' : 'Use in plots and maps'),
              h('button.btn.small.ghost', { type: 'button', onclick: () => {
                session.draft = f.conditions.map((c) => ({ ...c, values: c.values ? [...c.values] : undefined }));
                session.name = f.name;
                renderSide();
              } }, icon('edit'), 'Edit as a new version'),
              h('button.btn.small.ghost.danger', { type: 'button', onclick: () => {
                if (session.active === f.id) session.active = null;
                app.store.commit(removeFilter(app.store.project, f.id), `Remove filter ${f.name} version ${f.version}`);
                refreshKeep();
              } }, icon('trash'), 'Remove'))));
        }
      }
      // Comparing two versions.
      if (saved.length >= 2) {
        const option = (f, selected) => h('option', { value: f.id, selected }, `${f.name} · v${f.version}`);
        const [a0, b0] = session.compare;
        const aSel = h('select', { 'aria-label': 'First filter' }, ...saved.map((f, i) => option(f, a0 ? f.id === a0 : i === saved.length - 2)));
        const bSel = h('select', { 'aria-label': 'Second filter' }, ...saved.map((f, i) => option(f, b0 ? f.id === b0 : i === saved.length - 1)));
        const out = h('div.qc-compare-result');
        const compare = () => {
          session.compare = [aSel.value, bSel.value];
          const fa = saved.find((f) => f.id === aSel.value);
          const fb = saved.find((f) => f.id === bSel.value);
          const ka = verifyFilter(fa, cells).keep;
          const kb = verifyFilter(fb, cells).keep;
          const c = compareKeeps(ka, kb);
          clear(out);
          out.append(h('table.data', h('tbody',
            h('tr', h('td', 'Kept by both'), h('td.r', formatCount(c.both))),
            h('tr', h('td', `Only by ${fa.name} v${fa.version}`), h('td.r', formatCount(c.onlyA))),
            h('tr', h('td', `Only by ${fb.name} v${fb.version}`), h('td.r', formatCount(c.onlyB))),
            h('tr', h('td', 'By neither'), h('td.r', formatCount(c.neither))))),
          h('button.btn.small', { type: 'button', onclick: () => {
            const diff = Uint8Array.from(ka, (k, i) => (k !== kb[i] ? 1 : 0));
            choose(diff, `Kept by one of ${fa.name} v${fa.version} and ${fb.name} v${fb.version}, not both`);
          } }, icon('target'), 'Show the difference in the tissue'));
        };
        aSel.addEventListener('change', compare);
        bSel.addEventListener('change', compare);
        panel.append(h('h4', 'Compare two versions'), h('div.qc-compare', aSel, bSel), out);
        compare();
      }
      state.draftKeep = result.keep;
      return panel;
    }

    function saveDraft() {
      const project = app.store.project;
      const previous = versions(project, t.dataset.id, session.name).at(-1) ?? null;
      const filter = createFilter({ dataset: t.dataset.id, name: session.name, conditions: session.draft, cells, previous });
      app.store.commit(addFilter(project, filter), `Save filter ${filter.name} version ${filter.version}`);
      toast(`Saved ${filter.name}, version ${filter.version}: it keeps ${formatCount(filter.result.kept)} of ${formatCount(filter.result.cells)} cells.`, { kind: 'ok' });
      renderSide();
    }

    // A filter in use (or none) changes what the plots and maps count.
    function refreshKeep() {
      keep = keepOf();
      renderSide();
      renderPlots();
      applyMap();
      showChosen();
    }

    body.append(side, h('div.qc-main', tissue, plotsHost));
    renderSide();
    renderPlots();
    applyMap();
    showChosen();

    // --- Saved views: the map, the filter in use and the place ---
    const captureNow = (name = '') => captureView({ name, dataset: t.dataset.id, mode: 'qc', desc,
      state: { camera: renderer.camera, ratio: window.devicePixelRatio || 1, layers, cell: null, qc: { map: session.map, filter: session.active } } });
    const takePending = () => {
      const view = app.pendingView;
      if (view?.mode !== 'qc' || view.dataset !== t.dataset.id) return;
      app.pendingView = null;
      session.map = view.qc?.map ?? session.map;
      mapSelect.value = session.map;
      session.active = view.qc?.filter && app.store.project.artifacts.some((a) => a.id === view.qc.filter) ? view.qc.filter : null;
      if (view.camera) {
        state.placed = true;
        renderer.setCamera(viewCamera(view, window.devicePixelRatio || 1));
      }
      refreshKeep();
    };
    headActions.append(h('button.btn.small', { type: 'button', title: 'Keep this map, filter and place, to reopen them exactly', onclick: async () => {
      const name = await promptDialog({ title: 'Save view', label: 'Name', value: `${t.dataset.name}, QC view ${app.store.project.views.length + 1}`, confirm: 'Save' });
      if (!name) return;
      app.store.commit(addView(app.store.project, captureNow(name)), `Save view ${name}`);
      toast(`Saved ${name}.`, { kind: 'ok' });
    } }, icon('save'), 'Save view'));

    // For the validation harness (validation/viewer.mjs) and the console.
    window.situcraftQC = {
      renderer, desc, cells, report,
      dataset: t.dataset.id,
      map: () => session.map,
      maps: () => qcMaps(desc).map((m) => ({ id: m.id, label: m.label })),
      // The filter in use (an artifact's id), or null for none.
      filter: () => session.active,
      setFilter: (id) => {
        session.active = id;
        refreshKeep();
      },
      metricRows: () => metricRows(report.summary.runMetrics, report.summary.metrics),
      setMap: (id) => {
        session.map = id;
        mapSelect.value = id;
        applyMap();
      },
      // Brushes the histogram of an attribute over [lo, hi], as dragging across it does.
      brush: (id, lo, hi) => {
        const mask = selectRange(attributeValues(cells, id), lo, hi, keep);
        app.selection = { dataset: t.dataset.id, mask, label: `${id} from ${lo} to ${hi}`, count: countMarked(mask) };
        app.store.notify(['selection']);
        return app.selection.count;
      },
      chosen: () => (app.selection?.dataset === t.dataset.id ? Array.from(app.selection.mask) : null),
      saveFilter: (name, conditions) => {
        session.name = name;
        session.draft = conditions;
        saveDraft();
        return app.store.project.artifacts.filter((a) => a.kind === 'filter' && a.name === name).at(-1);
      },
      settled: () => renderer.settled(),
      captureView: (name) => captureNow(name),
      saveView: (name) => {
        const view = captureNow(name);
        app.store.commit(addView(app.store.project, view), `Save view ${name}`);
        return view;
      },
    };
    takePending();

    return {
      ...state,
      showChosen,
      takePending,
      projectChanged() {
        keep = keepOf();
        renderSide();
      },
      dispose() {
        session.camera = renderer.camera;
        for (const p of state.plots) p.dispose();
        observer.disconnect();
        navigation.dispose();
        renderer.destroy();
        if (window.situcraftQC?.renderer === renderer) delete window.situcraftQC;
      },
    };
  }

  show();
  return {
    update(topics) {
      if (topics.has('project-loaded') || topics.has('focus') || topics.has('bundles')) show();
      else if (topics.has('project') && current) current.projectChanged();
      else if (topics.has('project')) show();
      if (topics.has('selection') && current) current.showChosen();
      if (current && (topics.has('view-pending') || topics.has('focus'))) current.takePending();
    },
    destroy() {
      teardown();
      root.remove();
    },
  };
}

const round = (v) => Number(v.toPrecision(6));

function formatNum(v) {
  if (!Number.isFinite(v)) return '—';
  const a = Math.abs(v);
  if (a >= 100 || Number.isInteger(v)) return String(Math.round(v));
  return a >= 1 ? v.toFixed(1) : v.toPrecision(2);
}
