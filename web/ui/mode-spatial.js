// The Spatial view: questions about how the tissue is organized, asked of a segmentation's cells.
// Every question starts from a graph of which cells are neighbors (internal/spatial,
// api_graphs.go): by the Delaunay triangulation of their centroids, their k nearest, a radius, or
// contact in the segmentation. A graph is built over the cells chosen (all, a QC filter's, those in
// regions, never those a review excluded), as a job, and kept as an artifact with its definition;
// the view shows what it holds (degrees, edge lengths, cells without a neighbor) and draws it on
// the tissue. Questions are asked of a label set's cells (internal/spatial, api_spatial.go):
// neighborhood enrichment over a graph, and co-occurrence across distances; each answer is kept as
// an artifact, and every number opens the cells it comes from on the tissue.

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
import { labelSource, labelVersions, refOf } from './label-sources.js';
import { COOCCURRENCE_DEFAULTS, ENRICHMENT_DEFAULTS, QUESTIONS, analysesOf, checkAnalysisParams, distancesTo, labelsRequest, rankPairs, strataRequest } from '../lib/spatial.js';
import { cooccurrenceChart, enrichmentHeatmap, pairName, pairsTable, pvalue, sharesTable, times } from './spatial-results.js';
import { showAnalysisCells } from './spatial-cells.js';

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
    const focusedAnalysis = ui.focus?.kind === 'spatial' ? project.artifacts.find((a) => a.kind === 'spatial' && a.id === ui.focus.id) : null;
    const seg = ui.focus?.kind === 'segmentation' ? project.artifacts.find((a) => a.kind === 'segmentation' && a.id === ui.focus.id) : null;
    const id = focusedGraph?.dataset ?? focusedAnalysis?.dataset ?? (ui.focus?.kind === 'dataset' ? ui.focus.id : seg?.dataset) ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), focusedGraph: focusedGraph?.dataset === dataset.id ? focusedGraph : null, focusedAnalysis: focusedAnalysis?.dataset === dataset.id ? focusedAnalysis : null, focusedSegmentation: seg?.dataset === dataset.id ? seg.id : null };
  }

  function sessionOf(dataset) {
    if (!sessions.has(dataset.id)) {
      sessions.set(dataset.id, {
        segmentation: '', cells: 'all', regions: new Set(), kind: 'delaunay', params: structuredClone(DEFAULTS), shown: null, chosen: null,
        // The question being asked, the answer shown, and its pair, or its label and distance.
        q: { question: 'enrichment', labels: '', graph: '', ...ENRICHMENT_DEFAULTS, within: new Set(), cells: 'all', regions: new Set(), farthest: 150, step: 10, pairs: COOCCURRENCE_DEFAULTS.pairs },
        analysis: null, pair: null, from: 0, to: null, at: null,
      });
    }
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

  // --- Questions: asked of a label set's cells ---
  const labelKey = (v) => `${v.id}|${v.level}`;

  async function ask(t, seg, session) {
    const q = session.q;
    const project = app.store.project;
    const v = labelVersions(project, t.dataset.id, seg.id).find((x) => labelKey(x) === q.labels);
    if (!v) throw new Error('choose a label set (the Cell types view labels the cells)');
    const src = await labelSource(app, refOf(v.id, v.level));
    const labels = labelsRequest(src);
    let request;
    if (q.question === 'enrichment') {
      const parameters = { permutations: q.permutations, seed: q.seed, weight: q.weight };
      const problem = checkAnalysisParams('enrichment', parameters);
      if (problem) throw new Error(problem);
      const graph = graphsOf(project, t.dataset.id, seg.id).find((g) => g.id === q.graph && g.state === 'complete');
      if (!graph) throw new Error('choose a graph of the segmentation (build one above)');
      request = { question: 'enrichment', segmentation: seg.id, graph: graph.id, labels, parameters };
      const within = regionsOf(project, t.dataset.id).filter((r) => q.within.has(r.id));
      if (within.length) {
        const cells = await cellsOf(t, seg);
        request.strata = strataRequest(cells.n, within.map((r) => ({ id: r.id, name: r.name, version: r.version, rows: cellsIn(r.geometry, cells) })));
      }
    } else {
      const parameters = { distances: distancesTo(q.farthest, q.step), pairs: q.pairs };
      const problem = checkAnalysisParams('cooccurrence', parameters);
      if (problem) throw new Error(problem);
      const chosen = await chosenCells(t, seg, { cells: q.cells, regions: q.regions });
      if (chosen.count < 2) throw new Error('fewer than two cells are chosen');
      request = { question: 'cooccurrence', segmentation: seg.id, cells: chosen.request, labels, parameters };
    }
    const manifest = await app.bundles.runAnalysis(t.item, t.dataset.id, request);
    session.analysis = manifest.id;
    session.pair = null;
    session.to = null;
    session.at = null;
    render(true);
    return manifest;
  }

  function questionCard(t, seg, session) {
    const q = session.q;
    const project = app.store.project;
    const versions = labelVersions(project, t.dataset.id, seg.id);
    if (!versions.some((v) => labelKey(v) === q.labels)) q.labels = versions[0] ? labelKey(versions[0]) : '';
    const graphs = graphsOf(project, t.dataset.id, seg.id).filter((g) => g.state === 'complete');
    if (!graphs.some((g) => g.id === q.graph)) q.graph = graphs.at(-1)?.id ?? '';
    const graph = graphs.find((g) => g.id === q.graph);
    if (graph?.parameters?.kind !== 'contact') q.weight = 'edges';
    const regions = regionsOf(project, t.dataset.id);
    const question = QUESTIONS.find((x) => x.id === q.question) ?? QUESTIONS[0];
    const field = (id, label, input, hint = null) => h('label.field', { for: id, title: hint ?? '' }, h('span', label), input);
    const number = (id, key, attrs) => {
      const input = h('input.input', { type: 'number', id, value: String(q[key]), ...attrs });
      input.addEventListener('change', () => {
        q[key] = Number(input.value);
      });
      return input;
    };
    const select = (id, options, key, after = () => {}) => {
      const el = h('select.input', { id }, ...options.map(([value, text]) => h('option', { value }, text)));
      el.value = q[key];
      el.addEventListener('change', () => {
        q[key] = el.value;
        after();
      });
      return el;
    };
    const regionBoxes = (set, legend) => (regions.length ? h('fieldset.sp-regions', h('legend', legend),
      ...regions.map((r) => {
        const box = h('input', { type: 'checkbox', checked: set.has(r.id) });
        box.addEventListener('change', () => {
          if (box.checked) set.add(r.id);
          else set.delete(r.id);
        });
        return h('label.viewer-toggle', box, h('span.sp-swatch', { style: { background: `rgb(${r.color.join(', ')})` } }), `${r.name} `, h('span.muted', r.cls));
      })) : null);
    const picker = h('div.sp-kinds', { role: 'radiogroup', 'aria-label': 'Question' }, ...QUESTIONS.map((x) => {
      const radio = h('input', { type: 'radio', name: 'sp-question', value: x.id, checked: x.id === question.id });
      radio.addEventListener('change', () => {
        q.question = x.id;
        render(true);
      });
      return h('label.sp-kind', radio, x.label);
    }));
    const inputs = question.id === 'enrichment'
      ? [h('div.seg-import-grid',
        field('sp-q-graph', 'Graph', select('sp-q-graph', graphs.map((g) => [g.id, `${describeGraph(g.parameters)}${g.summary?.choice?.regions?.length ? `, in ${g.summary.choice.regions.map((r) => r.name).join(', ')}` : ''}`]), 'graph', () => render(true))),
        field('sp-q-permutations', 'Permutations', number('sp-q-permutations', 'permutations', { min: 100, max: 100000, step: 100 }), 'How many times the labels are dealt again at random: more give steadier z-scores and smaller p-values (the smallest is 1 / (permutations + 1)).'),
        field('sp-q-seed', 'Seed', number('sp-q-seed', 'seed', { min: 0, step: 1 }), 'The same seed deals the labels the same way, on any computer.'),
        graph?.parameters?.kind === 'contact' ? field('sp-q-weight', 'Count neighbors by', select('sp-q-weight', [['edges', 'cells in contact'], ['boundary', 'boundary shared (µm)']], 'weight')) : null),
      regionBoxes(q.within, 'Deal the labels within each of these regions (the cells in none, among themselves)')]
      : [h('div.seg-import-grid',
        field('sp-q-cells', 'Cells', select('sp-q-cells', [['all', 'All cells'], ...(seg.source === 'instrument' ? filtersOf(t).map((f) => [f.id, `QC filter ${f.name} v${f.version}`]) : [])], 'cells')),
        field('sp-q-farthest', 'Up to (µm)', number('sp-q-farthest', 'farthest', { min: 1, max: 1000, step: 10 })),
        field('sp-q-step', 'Every (µm)', number('sp-q-step', 'step', { min: 1, max: 500, step: 1 })),
        field('sp-q-pairs', 'Pairs at each distance', select('sp-q-pairs', [['within', 'within it (as Squidpy)'], ['between', 'since the distance before']], 'pairs'))),
      regionBoxes(q.regions, 'Only the cells in')];
    const go = h('button.btn.primary', { type: 'button', disabled: !t.item || !versions.length || (question.id === 'enrichment' && !graphs.length), 'data-action': 'ask', onclick: async () => {
      try {
        const m = await ask(t, seg, session);
        toast(`${m.name}: done.`, { kind: 'ok' });
      } catch (error) {
        toast(`The question: ${error.message}`, { kind: 'error', timeout: 12000 });
      }
    } }, icon('play'), 'Ask');
    const missing = !versions.length ? 'Label the cells first: the Cell types view clusters and labels them, or imports labels.' : question.id === 'enrichment' && !graphs.length ? 'Build a graph above first: enrichment counts neighbors in it.' : null;
    return h('section.seg-section',
      h('div.section-title', 'Questions'),
      h('div.seg-card',
        h('div.field', h('span', 'Ask'), picker),
        h('p.muted.seg-note.sp-help', question.help),
        h('div.seg-import-grid', field('sp-q-labels', 'Labels', select('sp-q-labels', versions.map((v) => [labelKey(v), v.name]), 'labels'))),
        ...inputs,
        h('div.seg-form', go, missing ? h('span.muted', missing) : null),
        ...progress(app.bundles.analyzing(t.dataset.id), (e) => (e.question === 'enrichment' ? 'Neighborhood enrichment' : 'Co-occurrence')),
        h('p.muted.seg-cite', 'Methods: neighborhood enrichment by permutation of the labels (Schapiro et al. 2017; Palla et al. 2022), its expectation exact, its p-values adjusted by Benjamini and Hochberg (1995); co-occurrence as Palla et al. (2022). A section\'s cells are not replicates: these describe this section, and the sample is the unit of any comparison between samples.')));
  }

  function analysisListCard(t, seg, session) {
    const list = analysesOf(app.store.project, t.dataset.id, seg.id).filter((a) => a.state === 'complete');
    if (!list.some((a) => a.id === session.analysis)) session.analysis = list.at(-1)?.id ?? null;
    if (!list.length) return null;
    const choose = (a) => {
      session.analysis = a.id;
      session.pair = null;
      session.to = null;
      session.at = null;
      render(true);
    };
    const rows = list.map((a) => {
      const s = a.summary ?? {};
      const over = a.question === 'enrichment' ? describeGraph(s.graph?.parameters) : `${s.distances ?? '?'} distances`;
      const what = a.question === 'enrichment' ? `${s.significant ?? 0} pairs differ` : '';
      return h(`tr${session.analysis === a.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-analysis': a.id, onclick: () => choose(a), onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          choose(a);
        }
      } },
      h('td', a.question === 'enrichment' ? 'Enrichment' : 'Co-occurrence'),
      h('td', s.labels?.name ?? '—'),
      h('td', over, s.strata?.length ? h('span.muted', `, within ${s.strata.map((r) => r.name).join(', ')}`) : null),
      h('td.r', exact(s.labeled)),
      h('td', what),
      h('td.muted', relativeTime(a.created)));
    });
    return h('section.seg-section',
      h('div.section-title', `Answers about ${seg.name}`),
      h('div.table-wrap', h('table.data-table.seg-table',
        h('thead', h('tr', h('th', 'Question'), h('th', 'Labels'), h('th', 'Over'), h('th.r', 'Labeled cells'), h('th', ''), h('th', 'Made'))),
        h('tbody', ...rows))));
  }

  function analysisCard(t, seg, session) {
    const record = app.store.project.artifacts.find((a) => a.kind === 'spatial' && a.id === session.analysis);
    if (!record) return null;
    const host = h('section.seg-section.sp-answer', h('div.section-title', 'The answer'), h('p.muted.seg-note', 'Reading the answer…'));
    const mine = generation;
    app.bundles.analysisData(record.id).then((data) => {
      if (mine !== generation) return;
      clear(host).append(h('div.section-title', 'The answer'), ...(data.analysis.question === 'enrichment' ? enrichmentAnswer(t, seg, session, record, data) : cooccurrenceAnswer(t, seg, session, record, data)));
    }).catch((error) => {
      if (mine !== generation) return;
      clear(host).append(h('div.section-title', 'The answer'), h('p.muted.seg-note', `The answer could not be read: ${error.message}`));
    });
    return host;
  }

  const fillOf = (a) => `labels:${a.labels.id}:${a.labels.level}`;

  function enrichmentAnswer(t, seg, session, record, { analysis, cells }) {
    const e = analysis.enrichment;
    const names = analysis.labels.names;
    const p = analysis.parameters;
    const pick = (a, b) => {
      session.pair = [a, b];
      render(true);
    };
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${names.length})`)),
      h('dt', 'Graph'), h('dd', analysis.graph.name),
      h('dt', 'Cells'), h('dd', `${exact(analysis.cells.labeled)} labeled of ${exact(analysis.cells.analyzed)} in the graph`, h('span.muted', `; ${exact(e.edges)} edges between labeled cells`)),
      h('dt', 'Null'), h('dd', `the labels dealt again ${exact(p.permutations)} times (seed ${p.seed})${analysis.strata ? `, within ${analysis.strata.regions.map((r) => r.name).join(', ')} and the rest` : ''}`),
      p.weight === 'boundary' ? [h('dt', 'Counted by'), h('dd', 'the boundary each pair of cells shares (µm)')] : null,
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const parts = [kv, enrichmentHeatmap(analysis, { onPick: pick, chosen: session.pair }), pairsTable(analysis, { onPick: pick }), sharesTable(analysis)];
    if (session.pair) {
      const [a, b] = session.pair;
      const pair = rankPairs(analysis).find((x) => x.a === a && x.b === b);
      const edges = a === b ? pair.count / 2 : pair.count;
      const panel = h('div.seg-card.sp-pair',
        h('b', pairName(names, a, b)),
        h('p', `${exact(Math.round(edges * 100) / 100)} ${p.weight === 'boundary' ? 'µm of boundary shared' : a === b ? 'edges between cells of this label' : 'edges between their cells'}, against ${exact(Math.round(a === b ? pair.expected / 2 : pair.expected))} expected if the labels were dealt at random: ${times(pair.ratio)} as many; z ${Number.isFinite(pair.z) ? pair.z.toFixed(1) : '—'}, adjusted p ${pvalue(pair.padj)}.`),
        h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'show-pair', onclick: () => showPair(t, seg, record, analysis, cells, a, b) }, icon('tissue'), 'Show these cells on the tissue')));
      parts.push(panel);
    }
    parts.push(h('p.muted.seg-note', 'Found: each cell\'s neighbors of the other label, summed (an edge within one label counts from both ends). Expected: exactly, if the labels were dealt to the same cells at random. The ratio is the effect\'s size; z and the p-value say how unusual it is in this section, whose cells are not replicates.'));
    return parts;
  }

  function showPair(t, seg, record, analysis, cells, a, b) {
    return showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, a, b }).catch((error) => toast(`The cells: ${error.message}`, { kind: 'error' }));
  }

  function cooccurrenceAnswer(t, seg, session, record, { analysis, cells }) {
    const co = analysis.cooccurrence;
    const names = analysis.labels.names;
    const K = names.length;
    if (session.from >= K) session.from = 0;
    if (session.at === null || session.at >= co.distances.length) session.at = 0;
    const fromPick = h('select.input', { id: 'sp-from' }, ...names.map((n, k) => h('option', { value: String(k) }, n)));
    fromPick.value = String(session.from);
    fromPick.addEventListener('change', () => {
      session.from = Number(fromPick.value);
      render(true);
    });
    const atPick = h('select.input', { id: 'sp-at' }, ...co.distances.map((d, k) => h('option', { value: String(k) }, co.pairs === 'between' ? `${k ? co.distances[k - 1] : 0} to ${d} µm` : `within ${d} µm`)));
    atPick.value = String(session.at);
    atPick.addEventListener('change', () => {
      session.at = Number(atPick.value);
      render(true);
    });
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${K})`)),
      h('dt', 'Cells'), h('dd', `${exact(analysis.cells.labeled)} labeled of ${exact(analysis.cells.analyzed)}`, analysis.cells.regions?.length ? h('span.muted', ` in ${analysis.cells.regions.map((r) => r.name).join(', ')}`) : null),
      h('dt', 'Distances'), h('dd', `${co.distances[0]} to ${co.distances.at(-1)} µm, the pairs ${co.pairs === 'between' ? 'since the distance before' : 'within each'}`),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const chart = cooccurrenceChart(analysis, { from: session.from, chosen: session.to, onPick: (b) => {
      session.to = b;
      render(true);
    } });
    const parts = [kv, h('div.seg-import-grid', h('label.field', { for: 'sp-from' }, h('span', 'From the cells of'), fromPick), h('label.field', { for: 'sp-at' }, h('span', 'At'), atPick)), chart];
    if (session.to !== null && session.to < K) {
      const a = session.from;
      const b = session.to;
      const k = session.at;
      const ratio = co.ratio[k][a * K + b];
      const panel = h('div.seg-card.sp-pair',
        h('b', `${names[b]} near ${names[a]}`),
        h('p', `${co.pairs === 'between' ? `Between ${k ? co.distances[k - 1] : 0} and ${co.distances[k]} µm` : `Within ${co.distances[k]} µm`} of a ${names[a]} cell, a cell is ${names[b]} ${times(ratio)} as often as cells are ${names[b]} at that distance from any cell (${formatCount(co.count[k][a * K + b])} pairs).`),
        h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'show-near', onclick: () => showNear(t, seg, analysis, cells, a, b, k) }, icon('tissue'), 'Show these cells on the tissue')));
      parts.push(panel);
    }
    parts.push(h('p.muted.seg-note', 'Click a curve or a label to choose it. Descriptive: there is no test, and the comparison is between labels within this section.'));
    return parts;
  }

  function showNear(t, seg, analysis, cells, a, b, k) {
    return showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, a, b, k }).catch((error) => toast(`The cells: ${error.message}`, { kind: 'error' }));
  }

  // --- Render ---
  let shape = '';
  function render(force = false) {
    const t = target();
    const key = JSON.stringify([t?.dataset.id, app.store.project.artifacts.filter((a) => ['graph', 'segmentation', 'filter', 'spatial', 'labels', 'curated-labels', 'consensus-labels'].includes(a.kind)).map((a) => `${a.id}${a.state}${a.edits?.length ?? ''}`), (app.store.project.regions ?? []).map((r) => `${r.id}:${r.versions.length}`), (app.store.project.reviews ?? []).length, app.bundles.graphing(t?.dataset.id ?? '').map((e) => e.job), app.bundles.analyzing(t?.dataset.id ?? '').map((e) => e.job)]);
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
    } else if (t.focusedAnalysis) {
      session.segmentation = t.focusedAnalysis.segmentation;
      if (session.analysis !== t.focusedAnalysis.id) {
        session.analysis = t.focusedAnalysis.id;
        session.pair = null;
        session.to = null;
      }
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
    body.append(questionCard(t, seg, session));
    const answers = analysisListCard(t, seg, session);
    if (answers) body.append(answers, analysisCard(t, seg, session));
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
    // Asks a question as the form would ({ segmentation, question, labels: a label set's id and level
    // as "id|level", graph, permutations, seed, weight, within: region ids, cells, regions, farthest,
    // step, pairs }); answers its manifest.
    ask: async (dataset, args) => {
      const t0 = target();
      if (!t0 || t0.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const t = target();
      if (!t.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const seg = segmentationList(t).find((x) => x.id === args.segmentation);
      if (!seg) throw new Error(`No complete segmentation ${args.segmentation} of ${dataset.name}.`);
      const session = sessionOf(dataset);
      session.segmentation = seg.id;
      const q = session.q;
      Object.assign(q, { ...ENRICHMENT_DEFAULTS, farthest: 150, step: 10, pairs: COOCCURRENCE_DEFAULTS.pairs, cells: 'all' });
      for (const k of ['question', 'labels', 'graph', 'permutations', 'seed', 'weight', 'cells', 'farthest', 'step', 'pairs']) if (args[k] !== undefined) q[k] = args[k];
      q.within = new Set(args.within ?? []);
      q.regions = new Set(args.regions ?? []);
      return ask(t, seg, session);
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
