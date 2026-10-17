// The Spatial view: questions about how the tissue is organized, asked of a segmentation's cells.
// Every question starts from a graph of which cells are neighbors (internal/spatial,
// api_graphs.go): by the Delaunay triangulation of their centroids, their k nearest, a radius, or
// contact in the segmentation. A graph is built over the cells chosen (all, a QC filter's, those in
// regions, never those a review excluded), as a job, and kept as an artifact with its definition;
// the view shows what it holds (degrees, edge lengths, cells without a neighbor) and draws it on
// the tissue.

import { h, icon, clear, formatCount, relativeTime } from './dom.js';
import { toast } from './overlays.js';
import { promotedSegmentation, segmentationsOf } from '../lib/segmentations.js';
import { evaluate, keepChecksum } from '../lib/filters.js';
import { readCells } from '../lib/viewer-data.js';
import { readSegCells } from '../lib/segview.js';
import { cellsIn, regionsOf } from '../lib/regions.js';
import { cellReviewState, reviewsOf } from '../lib/review-record.js';
import { DEFAULTS, KINDS, cellsRequest, checkGraphParams, chooseCells, describeGraph, graphParams, graphsOf } from '../lib/graphs.js';
import { histogramPlot } from './qc-plots.js';

const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const fixed = (v, digits = 1) => (Number.isFinite(v) ? v.toFixed(digits) : '—');

// Per dataset id, what the view shows and the graph being defined, for the session.
const sessions = new Map();

export function mountSpatial(app, container) {
  const title = h('span.crumbs');
  const body = h('div.seg-body.sp-body');
  const root = h('div.view.spatial', h('div.workbench-head', h('h1', icon('graph'), 'Spatial', title)), h('div.workbench-scroll', body));
  container.append(root);
  const live = new Map(); // job id → { bar, message }
  let generation = 0;
  let plots = [];
  // Each segmentation's cells table, read once: segmentation id → Promise<cells>.
  const tables = new Map();

  function target() {
    const { project, ui } = app.store;
    const focusedGraph = ui.focus?.kind === 'graph' ? project.artifacts.find((a) => a.kind === 'graph' && a.id === ui.focus.id) : null;
    const seg = ui.focus?.kind === 'segmentation' ? project.artifacts.find((a) => a.kind === 'segmentation' && a.id === ui.focus.id) : null;
    const id = focusedGraph?.dataset ?? (ui.focus?.kind === 'dataset' ? ui.focus.id : seg?.dataset) ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), focusedGraph: focusedGraph?.dataset === dataset.id ? focusedGraph : null, focusedSegmentation: seg?.dataset === dataset.id ? seg.id : null };
  }

  function sessionOf(dataset) {
    if (!sessions.has(dataset.id)) sessions.set(dataset.id, { segmentation: '', cells: 'all', regions: new Set(), kind: 'delaunay', params: structuredClone(DEFAULTS), shown: null, chosen: null });
    return sessions.get(dataset.id);
  }

  // The segmentations a graph is built on: complete ones, the default first.
  function segmentationList(t) {
    const promoted = promotedSegmentation(app.store.project, t.dataset.id)?.record.id;
    const list = segmentationsOf(app.store.project, t.dataset.id).filter((a) => a.state === 'complete' && a.source !== 'planted');
    return [...list.filter((a) => a.id === promoted), ...list.filter((a) => a.id !== promoted)].map((a) => ({ ...a, isDefault: a.id === promoted }));
  }

  // A QC filter chooses cells of the instrument's segmentation (its conditions are on them).
  function filtersOf(t) {
    return app.store.project.artifacts.filter((a) => a.kind === 'filter' && a.dataset === t.dataset.id).sort((a, b) => b.version - a.version || (a.created < b.created ? 1 : -1));
  }

  // A segmentation's cells table (centroids and the attributes filters read).
  function cellsOf(t, seg) {
    if (!tables.has(seg.id)) {
      const url = seg.source === 'instrument' ? `${t.item.url}/cells` : `${t.item.url}/segmentations/${encodeURIComponent(seg.id)}/cells`;
      const p = fetch(url).then(async (response) => {
        if (!response.ok) throw new Error(`its cells: HTTP ${response.status}`);
        const buffer = await response.arrayBuffer();
        return seg.source === 'instrument' ? readCells(buffer) : readSegCells(buffer);
      });
      p.catch(() => tables.delete(seg.id));
      tables.set(seg.id, p);
    }
    return tables.get(seg.id);
  }

  // The cells the form chooses: { keep, count, excluded, n, request } (lib/graphs.js).
  async function chosenCells(t, seg, session) {
    const cells = await cellsOf(t, seg);
    const project = app.store.project;
    let keep = null;
    let filter = null;
    if (session.cells !== 'all') {
      const f = filtersOf(t).find((x) => x.id === session.cells);
      if (!f) throw new Error('the filter chosen is no longer in the project');
      ({ keep } = evaluate(f.conditions, cells));
      if (keepChecksum(keep) !== f.result.sha256) throw new Error(`the filter ${f.name} v${f.version} no longer keeps the cells it recorded`);
      filter = { id: f.id, name: `${f.name} v${f.version}`, checksum: f.result.sha256 };
    }
    const regions = regionsOf(project, t.dataset.id).filter((r) => session.regions.has(r.id));
    const regionRows = regions.length ? regions.map((r) => cellsIn(r.geometry, cells)) : null;
    const state = cellReviewState(reviewsOf(project, t.dataset.id).filter((r) => r.target === 'cells'), seg.id, cells.n);
    const chosen = chooseCells(cells.n, { keep, regionRows, excluded: state.excluded });
    const request = cellsRequest(chosen.keep, { filter, regions: regions.map((r) => ({ id: r.id, name: r.name, version: r.version })), excluded: chosen.excluded });
    return { ...chosen, n: cells.n, request };
  }

  async function build(t, seg, session) {
    const params = graphParams({ kind: session.kind, ...session.params[session.kind] });
    const problem = checkGraphParams(params);
    if (problem) {
      toast(problem, { kind: 'error' });
      return;
    }
    try {
      const chosen = await chosenCells(t, seg, session);
      if (chosen.count < 2) throw new Error('fewer than two cells are chosen');
      const manifest = await app.bundles.buildGraph(t.item, t.dataset.id, { segmentation: seg.id, parameters: params, cells: chosen.request });
      session.shown = manifest.id;
      render(true);
      const s = manifest.summary ?? {};
      toast(`Built: ${formatCount(s.edges ?? 0)} edges among ${formatCount(s.cells ?? 0)} cells.`, { kind: 'ok' });
    } catch (error) {
      toast(`The graph: ${error.message}`, { kind: 'error', timeout: 12000 });
    }
  }

  function progress(entries, label) {
    return entries.map((e) => {
      const job = e.job ? app.jobs.get(e.job) : null;
      const bar = h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } });
      const message = h('span.muted', job?.message ?? 'Starting…');
      if (e.job) live.set(e.job, { bar, message });
      return h('div.seg-running', h('div', h('b', label(e)), ' ', message), h('div.progress', bar));
    });
  }

  // --- The form: the segmentation, the cells and the graph's definition ---
  function formCard(t, segs, session) {
    const seg = segs.find((s) => s.id === session.segmentation);
    const instrument = seg?.source === 'instrument';
    const filters = instrument ? filtersOf(t) : [];
    if (session.cells !== 'all' && !filters.some((f) => f.id === session.cells)) session.cells = 'all';
    const regions = regionsOf(app.store.project, t.dataset.id);
    for (const id of [...session.regions]) if (!regions.some((r) => r.id === id)) session.regions.delete(id);

    const segPick = h('select.input', { id: 'sp-segmentation' }, ...segs.map((s) => h('option', { value: s.id }, `${s.name}${s.isDefault ? ' (default)' : ''}`)));
    segPick.value = session.segmentation;
    segPick.addEventListener('change', () => {
      session.segmentation = segPick.value;
      session.shown = null;
      render(true);
    });
    const cellsPick = h('select.input', { id: 'sp-cells' }, h('option', { value: 'all' }, 'All cells'),
      ...filters.map((f) => h('option', { value: f.id }, `QC filter ${f.name} v${f.version} (${formatCount(f.result.kept)} cells)`)));
    cellsPick.value = session.cells;
    cellsPick.addEventListener('change', () => {
      session.cells = cellsPick.value;
      count();
    });
    const regionBoxes = regions.length ? h('fieldset.sp-regions', h('legend', 'Only the cells in'),
      ...regions.map((r) => {
        const box = h('input', { type: 'checkbox', checked: session.regions.has(r.id), 'data-region': r.id });
        box.addEventListener('change', () => {
          if (box.checked) session.regions.add(r.id);
          else session.regions.delete(r.id);
          count();
        });
        return h('label.viewer-toggle', box, h('span.sp-swatch', { style: { background: `rgb(${r.color.join(', ')})` } }), `${r.name} `, h('span.muted', r.cls));
      })) : null;
    const chosenNote = h('p.muted.seg-note.sp-chosen', 'Counting the cells…');
    const mine = generation;
    function count() {
      if (!seg || !t.item) {
        chosenNote.textContent = '';
        return;
      }
      chosenCells(t, seg, session).then((c) => {
        if (mine !== generation) return;
        chosenNote.textContent = `${exact(c.count)} of ${exact(c.n)} cells${c.excluded ? `; ${exact(c.excluded)} a review excluded ${c.excluded === 1 ? 'is' : 'are'} left out` : ''}.`;
      }).catch((error) => {
        if (mine !== generation) return;
        chosenNote.textContent = `The cells could not be chosen: ${error.message}.`;
      });
    }
    count();

    const kind = KINDS.find((k) => k.id === session.kind) ?? KINDS[0];
    const kindPick = h('div.sp-kinds', { role: 'radiogroup', 'aria-label': 'Neighbors by' }, ...KINDS.map((k) => {
      const radio = h('input', { type: 'radio', name: 'sp-kind', value: k.id, checked: k.id === kind.id });
      radio.addEventListener('change', () => {
        session.kind = k.id;
        render(true);
      });
      return h('label.sp-kind', radio, k.label);
    }));
    const spec = kind.param;
    const input = h('input.input', { type: 'number', id: 'sp-param', min: String(spec.min), max: String(spec.max), step: String(spec.step), value: String(session.params[kind.id][spec.id]) });
    input.addEventListener('change', () => {
      session.params[kind.id][spec.id] = Number(input.value);
    });
    const go = h('button.btn.primary', { type: 'button', disabled: !seg || !t.item, 'data-action': 'build-graph', onclick: () => build(t, seg, session) }, icon('play'), 'Build the graph');
    const segName = (id) => segs.find((s) => s.id === id)?.name ?? 'the cells';
    return h('section.seg-section',
      h('div.section-title', 'Which cells are neighbors'),
      h('div.seg-card',
        h('p', 'Every spatial question starts from which cells count as neighbors. A graph joins neighboring cells, by one of four definitions, over the cells chosen; it is kept with its definition, and the same definition gives the same graph on any computer.'),
        h('div.seg-import-grid',
          h('label.field', { for: 'sp-segmentation' }, h('span', 'Segmentation'), segPick),
          h('label.field', { for: 'sp-cells' }, h('span', 'Cells'), cellsPick)),
        regionBoxes,
        chosenNote,
        h('div.field', h('span', 'Neighbors by'), kindPick),
        h('p.muted.seg-note.sp-help', kind.help),
        h('div.seg-import-grid', h('label.field', { for: 'sp-param' }, h('span', spec.label), input)),
        h('div.seg-form', go, !t.item ? h('span.muted', `${t.dataset.name}'s bundle was not found where the project records it: locate it in the Data view.`) : null),
        ...progress(app.bundles.graphing(t.dataset.id), (e) => `Building a graph of ${segName(e.segmentation)}`),
        h('p.muted.seg-cite', 'Methods: Delaunay triangulation (Lee & Schachter 1980; Bowyer 1981; Watson 1981) with exact geometric predicates (Shewchuk 1997); nearest neighbors and radii by exact search; distances between centroids, in µm.')));
  }

  // --- The graphs built of the segmentation ---
  function listCard(t, seg, session) {
    const list = graphsOf(app.store.project, t.dataset.id, seg?.id).filter((g) => g.state === 'complete');
    if (!list.some((g) => g.id === session.shown)) session.shown = list.at(-1)?.id ?? null;
    if (!list.length) return null;
    const choose = (g) => {
      session.shown = g.id;
      render(true);
    };
    const rows = list.map((g) => {
      const s = g.summary ?? {};
      const choice = s.choice ?? {};
      const over = choice.regions?.length ? `in ${choice.regions.map((r) => r.name).join(', ')}` : choice.filter ? `filter ${choice.filter.name}` : 'all';
      return h(`tr${session.shown === g.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-graph': g.id, onclick: () => choose(g), onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          choose(g);
        }
      } },
      h('td', describeGraph(g.parameters)),
      h('td', over),
      h('td.r', exact(s.cells)),
      h('td.r', exact(s.edges)),
      h('td.r', fixed(s.meanDegree, 2)),
      h('td.r', exact(s.isolated)),
      h('td.r', Number.isFinite(s.medianLength) ? `${fixed(s.medianLength)} µm` : '—'),
      h('td.muted', relativeTime(g.created)),
      h('td', h('button.icon-button', { type: 'button', title: 'Show the graph on the tissue in the Viewer', 'aria-label': 'Show the graph on the tissue in the Viewer', onclick: (event) => {
        event.stopPropagation();
        app.showGraph(g);
      } }, icon('tissue'))));
    });
    return h('section.seg-section',
      h('div.section-title', `Graphs of ${seg.name}`),
      h('div.table-wrap', h('table.data-table.seg-table',
        h('thead', h('tr', h('th', 'Neighbors'), h('th', 'Built over'), h('th.r', 'Cells'), h('th.r', 'Edges'), h('th.r', 'Mean degree'), h('th.r', 'Without one'), h('th.r', 'Median edge'), h('th', 'Made'), h('th', ''))),
        h('tbody', ...rows))));
  }

  // --- One graph: what it holds ---
  function detailCard(t, session) {
    const g = app.store.project.artifacts.find((a) => a.kind === 'graph' && a.id === session.shown);
    if (!g) return null;
    const host = h('section.seg-section.sp-detail', h('div.section-title', 'The graph'), h('p.muted.seg-note', 'Reading the graph…'));
    const mine = generation;
    app.bundles.graphData(g.id).then((data) => {
      if (mine !== generation) return;
      clear(host).append(h('div.section-title', 'The graph'), ...detail(g, data));
    }).catch((error) => {
      if (mine !== generation) return;
      clear(host).append(h('div.section-title', 'The graph'), h('p.muted.seg-note', `The graph could not be read: ${error.message}`));
    });
    return host;
  }

  function detail(g, { description: d, graph }) {
    const choice = d.cells;
    const over = [
      choice.filter ? `the QC filter ${choice.filter.name}'s` : '',
      choice.regions?.length ? `in ${choice.regions.map((r) => `${r.name} (v${r.version})`).join(', ')}` : '',
    ].filter(Boolean).join(', ');
    const q = (v) => `${fixed(v)} µm`;
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Neighbors'), h('dd', describeGraph(d.parameters)),
      h('dt', 'Cells'), h('dd', `${exact(choice.inGraph)} of ${exact(choice.segmentation)}`, over ? h('span.muted', ` (${over})`) : null, choice.excluded ? h('span.muted', `; ${exact(choice.excluded)} a review excluded left out`) : null),
      h('dt', 'Edges'), h('dd', exact(d.edges)),
      h('dt', 'Neighbors a cell'), h('dd', `${fixed(d.degree.mean, 2)} on average, median ${fixed(d.degree.median, 0)}, at most ${d.degree.max}`),
      h('dt', 'Without a neighbor'), h('dd', `${exact(d.isolated)} cells`),
      h('dt', 'Pieces'), h('dd', d.components.count === 1 ? 'one: every cell reaches every other' : `${exact(d.components.count)}, the largest of ${exact(d.components.largest)} cells`),
      h('dt', 'Edge lengths'), h('dd', `median ${q(d.lengths.median)}; 5% to 95%: ${q(d.lengths.q05)} to ${q(d.lengths.q95)}; longest ${q(d.lengths.max)}`),
      d.shared ? [h('dt', 'Shared boundary'), h('dd', `median ${q(d.shared.median)} an edge (pixel edges, as the segmentation drew them)`)] : null,
      h('dt', 'Artifact'), h('dd.mono', g.id));
    const notes = [];
    if (d.isolated && d.parameters.kind !== 'knn') notes.push(`${exact(d.isolated)} cells have no neighbor in this graph: alone in the tissue, or beyond its ${d.parameters.kind === 'radius' ? 'radius' : d.parameters.kind === 'contact' ? 'gap' : 'longest edge'}. Spatial statistics leave them out or count them as they are; a wider definition joins them.`);
    if (d.duplicates) notes.push(`${exact(d.duplicates)} cells lie at the same place as another: each is joined to it and to its neighbors.`);
    if (d.collinear) notes.push('Every cell lies on one line: they are joined along it.');
    const degrees = new Float64Array(graph.n);
    for (let i = 0; i < graph.n; i += 1) degrees[i] = graph.offsets[i + 1] - graph.offsets[i];
    const lengths = new Float64Array(graph.length.length / 2);
    for (let i = 0, k = 0; i < graph.n; i += 1) {
      for (let e = graph.offsets[i]; e < graph.offsets[i + 1]; e += 1) if (graph.neighbors[e] > i) lengths[k++] = graph.length[e];
    }
    for (const p of plots) p.dispose?.();
    plots = [
      histogramPlot(degrees, { label: 'neighbors of a cell', integer: true }),
      histogramPlot(lengths, { label: 'edge length (µm)', unit: 'edges' }),
    ];
    return [kv,
      ...notes.map((n) => h('p.callout.warn', n)),
      h('div.sp-plots', h('div', h('div.muted.sp-plot-title', 'Neighbors of each cell'), plots[0].el), h('div', h('div.muted.sp-plot-title', 'Edge lengths'), plots[1].el)),
      h('div.btn-row', h('button.btn.small.primary', { type: 'button', onclick: () => app.showGraph(g) }, icon('tissue'), 'Show on the tissue'))];
  }

  // --- Render ---
  let shape = '';
  function render(force = false) {
    const t = target();
    const key = JSON.stringify([t?.dataset.id, app.store.project.artifacts.filter((a) => ['graph', 'segmentation', 'filter'].includes(a.kind)).map((a) => `${a.id}${a.state}`), (app.store.project.regions ?? []).map((r) => `${r.id}:${r.versions.length}`), (app.store.project.reviews ?? []).length, app.bundles.graphing(t?.dataset.id ?? '').map((e) => e.job)]);
    if (!force && key === shape) return;
    shape = key;
    generation += 1;
    live.clear();
    for (const p of plots) p.dispose?.();
    plots = [];
    clear(body);
    if (!t) {
      title.textContent = '';
      body.append(h('div.planned', h('div.empty', icon('graph'), h('h3', 'No dataset yet'), h('p', 'Add a Xenium output bundle to the project in the Data view, or open an example from the start page; then ask how its cells are arranged here.'))));
      return;
    }
    title.textContent = ` · ${t.dataset.name}`;
    const segs = segmentationList(t);
    const session = sessionOf(t.dataset);
    if (t.focusedGraph) {
      session.segmentation = t.focusedGraph.segmentation;
      session.shown = t.focusedGraph.id;
    } else if (t.focusedSegmentation && segs.some((s) => s.id === t.focusedSegmentation)) session.segmentation = t.focusedSegmentation;
    if (!segs.some((s) => s.id === session.segmentation)) session.segmentation = segs[0]?.id ?? '';
    if (!segs.length) {
      body.append(h('div.planned', h('div.empty', icon('graph'), h('h3', 'No segmentation yet'), h('p', `${t.dataset.name}'s cells are registered when its bundle is opened in the Viewer or the Segmentation view; then build a graph of them here.`))));
      return;
    }
    const seg = segs.find((s) => s.id === session.segmentation);
    body.append(formCard(t, segs, session));
    const list = listCard(t, seg, session);
    if (list) body.append(list, detailCard(t, session));
  }

  function updateLive() {
    for (const [id, parts] of live) {
      const job = app.jobs.get(id);
      if (!job) continue;
      parts.bar.style.width = `${Math.round((job.progress ?? 0) * 100)}%`;
      parts.message.textContent = job.message ?? '';
    }
  }

  render(true);
  // For remote control (ui/remote.js build_graph) and the validation suite.
  app.spatialView = {
    // Builds a graph as the form would, its cells chosen as the form chooses them ({ segmentation,
    // kind, params, cells: 'all' or a filter's id, regions: [ids] }); answers its manifest.
    build: async (dataset, { segmentation, kind, params = {}, cells = 'all', regions = [] }) => {
      const t0 = target();
      if (!t0 || t0.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const t = target();
      if (!t.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const segs = segmentationList(t);
      const seg = segmentation ? segs.find((s) => s.id === segmentation) : segs[0];
      if (!seg) throw new Error(segmentation ? `No complete segmentation ${segmentation} of ${dataset.name}.` : `${dataset.name} has no segmentation yet.`);
      const session = sessionOf(dataset);
      session.segmentation = seg.id;
      session.kind = kind;
      session.params[kind] = { ...session.params[kind], ...params, kind };
      session.cells = cells;
      session.regions = new Set(regions);
      const parameters = graphParams({ kind, ...session.params[kind] });
      const problem = checkGraphParams(parameters);
      if (problem) throw new Error(problem);
      const chosen = await chosenCells(t, seg, session);
      if (chosen.count < 2) throw new Error('fewer than two cells are chosen');
      const manifest = await app.bundles.buildGraph(t.item, dataset.id, { segmentation: seg.id, parameters, cells: chosen.request });
      session.shown = manifest.id;
      render(true);
      return { manifest, chosen: { count: chosen.count, excluded: chosen.excluded, checksum: chosen.request.checksum } };
    },
  };
  return {
    update(topics) {
      if (topics.has('project') || topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('mode')) render();
      else if (topics.has('jobs')) updateLive();
    },
    destroy() {
      if (app.spatialView) delete app.spatialView;
      for (const p of plots) p.dispose?.();
      root.remove();
    },
  };
}
