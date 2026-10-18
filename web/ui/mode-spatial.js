// The Spatial view: questions about how the tissue is organized, asked of a segmentation's cells.
// Every question starts from a graph of which cells are neighbors (internal/spatial,
// api_graphs.go): by the Delaunay triangulation of their centroids, their k nearest, a radius, or
// contact in the segmentation. A graph is built over the cells chosen (all, a QC filter's, those in
// regions, never those a review excluded), as a job, and kept as an artifact with its definition;
// the view shows what it holds (degrees, edge lengths, cells without a neighbor) and draws it on
// the tissue. Questions are asked of a label set's cells (internal/spatial, api_spatial.go):
// neighborhood enrichment over a graph, co-occurrence across distances, nearest distances, Ripley's
// K, regions compared, a profile across a region's edge and cellular neighborhoods (niches); and of
// genes, Moran's I over a graph (spatially variable genes); each answer is kept as an artifact, and
// every number opens the cells it comes from on the tissue.

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
import { BOUNDARY_DEFAULTS, COOCCURRENCE_DEFAULTS, DISTANCES_DEFAULTS, ENRICHMENT_DEFAULTS, LOCAL_CLASSES, MAX_REGIONS, MORAN_DEFAULTS, NICHES_DEFAULTS, QUESTIONS, REGIONS_DEFAULTS, RIPLEY_DEFAULTS, analysesOf, bandsOf, checkAnalysisParams, distancesTo, labelsRequest, nicheNames, rankPairs, rowNames, strataRequest, zoneBands } from '../lib/spatial.js';
import { cooccurrenceChart, distancesChart, distancesHeatmap, distancesTable, enrichmentHeatmap, moranTable, nicheHeatmap, nichesTable, pairName, pairsTable, profileChart, pvalue, ratioForest, ratioText, readingsTable, regionsTable, ripleyChart, ripleyTable, sharesTable, times } from './spatial-results.js';
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
        q: {
          question: 'enrichment', labels: '', graph: '', ...ENRICHMENT_DEFAULTS, within: new Set(), cells: 'all', regions: new Set(), farthest: 150, step: 10, pairs: COOCCURRENCE_DEFAULTS.pairs,
          // Nearest distances and Ripley's K.
          dPermutations: DISTANCES_DEFAULTS.permutations, reach: DISTANCES_DEFAULTS.reach, dStep: DISTANCES_DEFAULTS.step,
          upTo: 100, every: 5, correction: RIPLEY_DEFAULTS.correction, window: 'tissue', closing: 10,
          // Regions compared, and a profile across a region's edge.
          compare: new Set(), edge: '', bReach: BOUNDARY_DEFAULTS.reach, bStep: BOUNDARY_DEFAULTS.step, margin: BOUNDARY_DEFAULTS.margin,
          // Cellular neighborhoods: the window (nearest or a radius), the niches, starts and seeds.
          windowBy: 'neighbors', neighbors: NICHES_DEFAULTS.neighbors, nRadius: 30, niches: NICHES_DEFAULTS.niches, starts: NICHES_DEFAULTS.starts, seeds: NICHES_DEFAULTS.seeds,
          // Spatially variable genes: the weights, permutations and local genes.
          mWeights: MORAN_DEFAULTS.weights, mPermutations: MORAN_DEFAULTS.permutations, local: MORAN_DEFAULTS.local, localPermutations: MORAN_DEFAULTS.localPermutations,
        },
        analysis: null, pair: null, from: 0, to: null, at: null,
        // Regions compared's two rows, a label chosen, densities or shares, and a profile's zone.
        rows: null, label: null, measure: 'density', zone: 'margin', niche: null, gene: null, localClass: 1,
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
    if (q.question === 'moran') {
      // Of genes: a graph, no labels.
      const parameters = { weights: q.mWeights, permutations: q.mPermutations, seed: q.seed, local: q.local, localPermutations: q.localPermutations };
      const problem = checkAnalysisParams('moran', parameters);
      if (problem) throw new Error(problem);
      const graph = graphsOf(project, t.dataset.id, seg.id).find((g) => g.id === q.graph && g.state === 'complete');
      if (!graph) throw new Error('choose a graph of the segmentation (build one above)');
      const manifest = await app.bundles.runAnalysis(t.item, t.dataset.id, { question: 'moran', segmentation: seg.id, graph: graph.id, parameters });
      session.analysis = manifest.id;
      session.gene = null;
      render(true);
      return manifest;
    }
    const v = labelVersions(project, t.dataset.id, seg.id, { niches: true }).find((x) => labelKey(x) === q.labels);
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
      const measured = q.question === 'regions' || q.question === 'boundary';
      const parameters = q.question === 'cooccurrence' ? { distances: distancesTo(q.farthest, q.step), pairs: q.pairs }
        : q.question === 'distances' ? { permutations: q.dPermutations, seed: q.seed, reach: q.reach, step: q.dStep }
          : q.question === 'regions' ? { ...REGIONS_DEFAULTS }
            : q.question === 'boundary' ? { reach: q.bReach, step: q.bStep, margin: q.margin, level: BOUNDARY_DEFAULTS.level }
              : q.question === 'niches' ? { neighbors: q.windowBy === 'radius' ? 0 : q.neighbors, radius: q.windowBy === 'radius' ? q.nRadius : 0, niches: q.niches, starts: q.starts, seeds: q.seeds, seed: q.seed }
                : { radii: distancesTo(q.upTo, q.every), correction: q.correction, permutations: q.dPermutations, seed: q.seed };
      const problem = checkAnalysisParams(q.question, parameters);
      if (problem) throw new Error(problem);
      // Regions compared and a profile across an edge are of all the cells chosen, in and out of
      // the regions.
      const chosen = await chosenCells(t, seg, { cells: q.cells, regions: measured ? new Set() : q.regions });
      if (chosen.count < 2) throw new Error('fewer than two cells are chosen');
      request = { question: q.question, segmentation: seg.id, cells: chosen.request, labels, parameters };
      if (measured) {
        const all = regionsOf(project, t.dataset.id);
        const picked = q.question === 'regions' ? all.filter((r) => q.compare.has(r.id)) : all.filter((r) => r.id === q.edge);
        if (!picked.length) throw new Error(q.question === 'regions' ? 'choose the regions to compare' : 'choose the region whose edge to cross');
        if (picked.length > MAX_REGIONS) throw new Error(`compare at most ${MAX_REGIONS} regions at once`);
        request.window = { kind: 'tissue', closing: q.closing };
        request.regions = picked.map((r) => ({ region: { id: r.id, name: r.name, version: r.version }, geometry: r.geometry }));
      }
      if (q.question === 'ripley') {
        if (q.window === 'tissue') request.window = { kind: 'tissue', closing: q.closing };
        else {
          const r = regionsOf(project, t.dataset.id).find((x) => x.id === q.window);
          if (!r) throw new Error('the region chosen as the window is no longer in the project');
          request.window = { kind: 'region', region: { id: r.id, name: r.name, version: r.version }, geometry: r.geometry };
        }
      }
    }
    const manifest = await app.bundles.runAnalysis(t.item, t.dataset.id, request);
    session.analysis = manifest.id;
    session.pair = null;
    session.to = null;
    session.at = null;
    session.rows = null;
    session.label = null;
    session.niche = null;
    render(true);
    return manifest;
  }

  function questionCard(t, seg, session) {
    const q = session.q;
    const project = app.store.project;
    const versions = labelVersions(project, t.dataset.id, seg.id, { niches: true });
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
        ...(question.id === 'cooccurrence' ? [
          field('sp-q-farthest', 'Up to (µm)', number('sp-q-farthest', 'farthest', { min: 1, max: 1000, step: 10 })),
          field('sp-q-step', 'Every (µm)', number('sp-q-step', 'step', { min: 1, max: 500, step: 1 })),
          field('sp-q-pairs', 'Pairs at each distance', select('sp-q-pairs', [['within', 'within it (as Squidpy)'], ['between', 'since the distance before']], 'pairs')),
        ] : question.id === 'distances' ? [
          field('sp-q-reach', 'Search up to (µm)', number('sp-q-reach', 'reach', { min: 1, max: 1000, step: 10 }), 'A cell with no cell of a label this near counts at this distance.'),
          field('sp-q-dstep', 'Distribution every (µm)', number('sp-q-dstep', 'dStep', { min: 0.5, max: 100, step: 0.5 })),
          field('sp-q-dperm', 'Permutations', number('sp-q-dperm', 'dPermutations', { min: 19, max: 9999, step: 10 }), 'How many times the labels are dealt again: the envelope is their 2.5% to 97.5%.'),
          field('sp-q-seed', 'Seed', number('sp-q-seed', 'seed', { min: 0, step: 1 })),
        ] : [
          field('sp-q-window', 'Window', select('sp-q-window', [['tissue', 'The tissue, from its cells'], ...regions.map((r) => [r.id, `The region ${r.name}`])], 'window', () => render(true)), 'Where the cells could have been: its area sets their density and its edge is corrected for. The tissue is its cells\' footprint with gaps narrower than twice the closing radius filled, holes and the space beyond the section left out.'),
          q.window === 'tissue' ? field('sp-q-closing', 'Closing (µm)', number('sp-q-closing', 'closing', { min: 0, max: 100, step: 1 }), 'Gaps between cells narrower than twice this are tissue.') : null,
          field('sp-q-upto', 'Up to (µm)', number('sp-q-upto', 'upTo', { min: 1, max: 500, step: 5 }), 'Keep it below about a quarter of the window\'s width: farther, the edge correction rests on little.'),
          field('sp-q-every', 'Every (µm)', number('sp-q-every', 'every', { min: 1, max: 100, step: 1 })),
          field('sp-q-correction', 'Edge correction', select('sp-q-correction', [['isotropic', 'Ripley\'s isotropic'], ['none', 'none']], 'correction')),
          field('sp-q-dperm', 'Permutations', number('sp-q-dperm', 'dPermutations', { min: 19, max: 9999, step: 10 }), 'How many times the labels are dealt again: the envelope is their 2.5% to 97.5%.'),
          field('sp-q-seed', 'Seed', number('sp-q-seed', 'seed', { min: 0, step: 1 })),
        ])),
      regionBoxes(q.regions, 'Only the cells in')];
    const closing = field('sp-q-closing', 'Tissue closing (µm)', number('sp-q-closing', 'closing', { min: 0, max: 100, step: 1 }), 'The tissue is its cells\' footprint with gaps narrower than twice this filled; its area within each region, or band, is the density\'s denominator, so a region\'s empty glass and holes are not counted.');
    const cellsField = field('sp-q-cells', 'Cells', select('sp-q-cells', [['all', 'All cells'], ...(seg.source === 'instrument' ? filtersOf(t).map((f) => [f.id, `QC filter ${f.name} v${f.version}`]) : [])], 'cells'));
    if (question.id === 'regions') {
      for (const id of [...q.compare]) if (!regions.some((r) => r.id === id)) q.compare.delete(id);
      // None chosen yet: the dataset's regions, as many as can be compared at once.
      if (!q.compare.size) for (const r of regions.slice(0, MAX_REGIONS)) q.compare.add(r.id);
      inputs.splice(0, inputs.length, h('div.seg-import-grid', cellsField, closing), regionBoxes(q.compare, 'Compare these regions (and the tissue in none of them)'));
    } else if (question.id === 'moran') {
      inputs.splice(0, inputs.length, h('div.seg-import-grid',
        field('sp-q-graph', 'Graph', select('sp-q-graph', graphs.map((g) => [g.id, `${describeGraph(g.parameters)}${g.summary?.choice?.regions?.length ? `, in ${g.summary.choice.regions.map((r) => r.name).join(', ')}` : ''}`]), 'graph', () => render(true)), 'Each gene\'s expression is compared across the graph\'s edges: neighbors alike make a pattern.'),
        field('sp-q-mweights', 'Weights', select('sp-q-mweights', [['row', 'each cell\'s neighbors share 1 (row-standardized)'], ['binary', 'each edge 1 (binary)']], 'mWeights'), 'Row-standardized weights count every cell alike, however many neighbors it has.'),
        field('sp-q-mperm', 'Permutations', number('sp-q-mperm', 'mPermutations', { min: 0, max: 9999, step: 10 }), 'How many times the cells\' values are dealt again for the test (0: the analytic test alone, under randomization).'),
        field('sp-q-local', 'Local Moran\'s I of', number('sp-q-local', 'local', { min: 0, max: 50, step: 1 }), 'The leading genes whose local Moran\'s I is found: which cells make the pattern.'),
        field('sp-q-lperm', 'Local permutations', number('sp-q-lperm', 'localPermutations', { min: 19, max: 9999, step: 10 })),
        field('sp-q-seed', 'Seed', number('sp-q-seed', 'seed', { min: 0, step: 1 }))));
    } else if (question.id === 'niches') {
      inputs.splice(0, inputs.length, h('div.seg-import-grid',
        cellsField,
        field('sp-q-windowby', 'Each cell\'s neighborhood', select('sp-q-windowby', [['neighbors', 'its nearest labeled cells'], ['radius', 'the labeled cells within a radius']], 'windowBy', () => render(true)), 'The cells whose labels describe a cell\'s place: itself among them when it is labeled.'),
        q.windowBy === 'radius' ? field('sp-q-nradius', 'Radius (µm)', number('sp-q-nradius', 'nRadius', { min: 5, max: 500, step: 5 })) : field('sp-q-neighbors', 'Nearest', number('sp-q-neighbors', 'neighbors', { min: 2, max: 200, step: 1 }), 'How many labeled cells make a neighborhood (10, as Schürch et al. used).'),
        field('sp-q-niches', 'Niches', number('sp-q-niches', 'niches', { min: 2, max: 30, step: 1 }), 'How many kinds of place to find. Too few merges different places; too many splits one; the agreement between seeds is a guide.'),
        field('sp-q-starts', 'k-means starts', number('sp-q-starts', 'starts', { min: 1, max: 100, step: 1 }), 'k-means finds a local best from where it starts: the best of these many starts is kept.'),
        field('sp-q-seeds', 'Seeds compared', number('sp-q-seeds', 'seeds', { min: 0, max: 20, step: 1 }), 'The niches are found again from these many other seeds, to say how stable they are.'),
        field('sp-q-seed', 'Seed', number('sp-q-seed', 'seed', { min: 0, step: 1 }))),
      regionBoxes(q.regions, 'Only the cells in'));
    } else if (question.id === 'boundary') {
      if (!regions.some((r) => r.id === q.edge)) q.edge = regions[0]?.id ?? '';
      inputs.splice(0, inputs.length, h('div.seg-import-grid',
        cellsField,
        field('sp-q-edge', 'Across the edge of', select('sp-q-edge', regions.map((r) => [r.id, r.name]), 'edge')),
        field('sp-q-breach', 'Bands to (µm)', number('sp-q-breach', 'bReach', { min: 10, max: 1000, step: 10 }), 'How far inside and outside the edge the bands reach; cells deeper or farther are counted in a band of their own.'),
        field('sp-q-bstep', 'Every (µm)', number('sp-q-bstep', 'bStep', { min: 1, max: 100, step: 1 }), 'Each band\'s width: it divides the reach.'),
        field('sp-q-margin', 'Margin (µm)', number('sp-q-margin', 'margin', { min: 1, max: 500, step: 5 }), 'The margin is within this of the edge on either side; the core is inside beyond it, around is outside beyond it to the reach.'),
        closing));
    }
    const ofGraph = question.id === 'enrichment' || question.id === 'moran';
    const go = h('button.btn.primary', { type: 'button', disabled: !t.item || (!versions.length && question.labels !== false) || (ofGraph && !graphs.length) || ((question.id === 'regions' || question.id === 'boundary') && !regions.length), 'data-action': 'ask', onclick: async () => {
      try {
        const m = await ask(t, seg, session);
        toast(`${m.name}: done.`, { kind: 'ok' });
      } catch (error) {
        toast(`The question: ${error.message}`, { kind: 'error', timeout: 12000 });
      }
    } }, icon('play'), 'Ask');
    const needsRegions = (question.id === 'regions' || question.id === 'boundary') && !regions.length;
    const missing = !versions.length && question.labels !== false ? 'Label the cells first: the Cell types view clusters and labels them, or imports labels.' : ofGraph && !graphs.length ? `Build a graph above first: ${question.id === 'moran' ? 'Moran\'s I compares neighbors along it' : 'enrichment counts neighbors in it'}.` : needsRegions ? 'Draw a region on the tissue first (the Viewer\'s region tools), or read regions from a file.' : null;
    return h('section.seg-section',
      h('div.section-title', 'Questions'),
      h('div.seg-card',
        h('div.field', h('span', 'Ask'), picker),
        h('p.muted.seg-note.sp-help', question.help),
        question.labels === false ? null : h('div.seg-import-grid', field('sp-q-labels', 'Labels', select('sp-q-labels', versions.map((v) => [labelKey(v), v.name]), 'labels'))),
        ...inputs,
        h('div.seg-form', go, missing ? h('span.muted', missing) : null),
        ...progress(app.bundles.analyzing(t.dataset.id), (e) => QUESTIONS.find((x) => x.id === e.question)?.label ?? 'The question'),
        h('p.muted.seg-cite', 'Methods: neighborhood enrichment by permutation of the labels (Schapiro et al. 2017; Palla et al. 2022), its expectation exact, its p-values adjusted by Benjamini and Hochberg (1995); co-occurrence as Palla et al. (2022); nearest distances and Ripley\'s K and L (Ripley 1977; Besag 1977) with the isotropic edge correction, against the labels dealt at random, with envelopes and a global test (Diggle 2003; Baddeley et al. 2014); densities and shares with Garwood\'s (1936) and Wilson\'s (1927) intervals, their ratios with exact conditional (Ederer and Mantel 1974) and Koopman\'s (1984) intervals; a tumor\'s core and margin as Galon et al. (2006) measure them; cellular neighborhoods as Goltsev et al. (2018) and Schürch et al. (2020) define them, by k-means (Lloyd 1982) with k-means++ seeding (Arthur and Vassilvitskii 2007); Moran\'s I (Moran 1950) with its variances under normality and randomization (Cliff and Ord 1981), and local Moran\'s I (Anselin 1995). A section\'s cells are not replicates: these describe this section, and the sample is the unit of any comparison between samples.')));
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
      session.rows = null;
      session.label = null;
      session.niche = null;
      render(true);
    };
    const rows = list.map((a) => {
      const s = a.summary ?? {};
      const over = a.question === 'enrichment' ? describeGraph(s.graph?.parameters) : a.question === 'cooccurrence' ? `${s.distances ?? '?'} distances` : a.question === 'ripley' ? `${s.window?.kind === 'region' ? `the region ${s.window.region?.name}` : 'the tissue'}`
        : a.question === 'regions' ? (s.rows ?? []).map((r) => r.name).join(', ') : a.question === 'boundary' ? `the edge of ${s.edge?.name ?? '?'}` : a.question === 'niches' ? `${s.niches?.length ?? '?'} niches` : a.question === 'moran' ? describeGraph(s.graph?.parameters) : 'the cells';
      const what = a.question === 'cooccurrence' ? '' : a.question === 'regions' ? `${s.significant ?? 0} densities differ` : a.question === 'boundary' ? `${s.significant ?? 0} labels differ` : a.question === 'niches' ? (Number.isFinite(s.agreementMin) ? `seeds agree at ${s.agreementMin.toFixed(2)} or more` : '') : a.question === 'moran' ? `${s.significant ?? 0} of ${s.genes ?? '?'} genes differ` : `${s.significant ?? 0} pairs differ`;
      return h(`tr${session.analysis === a.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-analysis': a.id, onclick: () => choose(a), onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          choose(a);
        }
      } },
      h('td', { enrichment: 'Enrichment', cooccurrence: 'Co-occurrence', distances: 'Nearest distances', ripley: 'Ripley\'s K', regions: 'Regions compared', boundary: 'Across an edge', niches: 'Niches', moran: 'Moran\'s I' }[a.question] ?? a.question),
      h('td', a.question === 'moran' ? 'the genes' : s.labels?.name ?? '—'),
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
      const answer = { enrichment: enrichmentAnswer, cooccurrence: cooccurrenceAnswer, distances: distancesAnswer, ripley: ripleyAnswer, regions: regionsAnswer, boundary: boundaryAnswer, niches: nichesAnswer, moran: moranAnswer }[data.analysis.question];
      clear(host).append(h('div.section-title', 'The answer'), ...answer(t, seg, session, record, data));
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

  // An ordered pair of labels chosen by two selects (from, to), for nearest distances and Ripley's K.
  function pairPicks(names, session, labels) {
    if (!session.pair || session.pair[0] >= names.length || session.pair[1] >= names.length) session.pair = [0, 0];
    const pick = (id, i) => {
      const el = h('select.input', { id }, ...names.map((n, k) => h('option', { value: String(k) }, n)));
      el.value = String(session.pair[i]);
      el.addEventListener('change', () => {
        session.pair = i === 0 ? [Number(el.value), session.pair[1]] : [session.pair[0], Number(el.value)];
        render(true);
      });
      return el;
    };
    return h('div.seg-import-grid', h('label.field', { for: 'sp-from' }, h('span', labels[0]), pick('sp-from', 0)), h('label.field', { for: 'sp-to' }, h('span', labels[1]), pick('sp-to', 1)));
  }

  // A distance to show cells within, of a list: a select.
  function distancePick(list, session, text) {
    if (session.at === null || session.at >= list.length) session.at = Math.min(list.length - 1, Math.max(0, Math.round(list.length / 4) - 1));
    const el = h('select.input', { id: 'sp-at', 'aria-label': 'The distance to show the cells within' }, ...list.map((d, k) => h('option', { value: String(k) }, `${text} ${d} µm`)));
    el.value = String(session.at);
    el.addEventListener('change', () => {
      session.at = Number(el.value);
    });
    return el;
  }

  function distancesAnswer(t, seg, session, record, { analysis, cells }) {
    const r = analysis.nearest;
    const names = analysis.labels.names;
    const K = names.length;
    const p = analysis.parameters;
    const pick = (a, b) => {
      session.pair = [a, b];
      render(true);
    };
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${K})`)),
      h('dt', 'Cells'), h('dd', `${exact(analysis.cells.labeled)} labeled of ${exact(analysis.cells.analyzed)}`, analysis.cells.regions?.length ? h('span.muted', ` in ${analysis.cells.regions.map((x) => x.name).join(', ')}`) : null),
      h('dt', 'Searched'), h('dd', `to ${r.reach} µm (a cell with none nearer counts at it)`),
      h('dt', 'Chance'), h('dd', `the labels dealt again ${exact(p.permutations)} times (seed ${p.seed})`),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const parts = [kv, distancesHeatmap(analysis, { onPick: pick, chosen: session.pair }), distancesTable(analysis, { onPick: pick })];
    parts.push(pairPicks(names, session, ['From the cells of', 'To the nearest']));
    const [a, b] = session.pair;
    const q = a * K + b;
    parts.push(distancesChart(analysis, a, b));
    parts.push(h('div.seg-card.sp-pair',
      h('b', `${names[a]} → the nearest ${names[b]}`),
      h('p', `A median of ${fixed(r.median[q])} µm (mean ${fixed(r.mean[q])} µm), against ${fixed(r.permMean[q])} µm when the labels are dealt at random: ${times(r.median[q] / r.permMean[q])}; z ${Number.isFinite(r.z[q]) ? r.z[q].toFixed(1) : '—'}, adjusted p ${pvalue(r.padj[q] ?? NaN)}.${r.beyond[q] ? ` ${exact(r.beyond[q])} cells have none within ${r.reach} µm.` : ''}`),
      h('div.btn-row', distancePick(r.distances, session, 'within'), h('button.btn.small.primary', { type: 'button', 'data-action': 'show-near', onclick: () => showNear(t, seg, analysis, cells, a, b, session.at) }, icon('tissue'), 'Show these cells on the tissue'))));
    parts.push(h('p.muted.seg-note', 'Red in the heat map: nearer than chance; blue: farther. Each row\'s cells to each column\'s nearest cell, so the map is not symmetric: a rare label lies near a common one more surely than the other way.'));
    return parts;
  }

  function ripleyAnswer(t, seg, session, record, { analysis, cells }) {
    const r = analysis.ripley;
    const names = analysis.labels.names;
    const K = names.length;
    const p = analysis.parameters;
    const w = analysis.window;
    const pick = (a, b) => {
      session.pair = [a, b];
      render(true);
    };
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${K})`)),
      h('dt', 'Window'), h('dd', w.kind === 'region' ? `the region ${w.region?.name} (v${w.region?.version})` : `the tissue, gaps narrower than ${2 * w.closing} µm filled`, h('span.muted', `; ${exact(Math.round(w.area))} µm²`)),
      h('dt', 'Cells'), h('dd', `${exact(r.cells.reduce((x, y) => x + y, 0))} labeled in the window`, r.outside ? h('span.muted', `; ${exact(r.outside)} outside it left out`) : null),
      h('dt', 'Corrected'), h('dd', r.correction === 'isotropic' ? 'for the edge, by Ripley\'s isotropic correction' : 'not for the edge'),
      h('dt', 'Chance'), h('dd', `the labels dealt again ${exact(p.permutations)} times (seed ${p.seed})`),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const parts = [kv, ripleyTable(analysis, { onPick: pick }), pairPicks(names, session, ['The cells of', 'Around them, the cells of'])];
    const [a, b] = session.pair;
    const q = a * K + b;
    parts.push(ripleyChart(analysis, a, b));
    parts.push(h('div.seg-card.sp-pair',
      h('b', a === b ? names[a] : `${names[b]} around ${names[a]}`),
      h('p', `Global test against the labels dealt at random: adjusted p ${pvalue(r.padj[q] ?? NaN)}. Above the envelope, ${a === b ? 'the label\'s cells gather' : 'the two labels draw together'} at that scale; below it, ${a === b ? 'they spread out' : 'they keep apart'}. The dashed line is points scattered at random (L = r), a reference only: cells cannot overlap, and tissue is not uniform.`),
      h('div.btn-row', distancePick(r.radii, session, 'within'), h('button.btn.small.primary', { type: 'button', 'data-action': 'show-near', onclick: () => showNear(t, seg, analysis, cells, a, b, session.at) }, icon('tissue'), 'Show these cells on the tissue'))));
    return parts;
  }

  // Densities (per mm² of tissue) or shares (of the labeled cells): a radio pair.
  function measurePick(session) {
    return h('div.sp-kinds', { role: 'radiogroup', 'aria-label': 'Measure' }, ...[['density', 'Cells per mm² of tissue'], ['share', 'Share of the labeled cells']].map(([id, text]) => {
      const radio = h('input', { type: 'radio', name: 'sp-measure', value: id, checked: session.measure === id });
      radio.addEventListener('change', () => {
        session.measure = id;
        render(true);
      });
      return h('label.sp-kind', radio, text);
    }));
  }

  const windowText = (w) => `the tissue, gaps narrower than ${2 * w.closing} µm filled; ${exact(Math.round(w.area))} µm²`;
  const levelText = (p) => `${Math.round(100 * (p.level ?? 0.95))}%, as if the cells were placed independently (clustered cells make the true uncertainty wider)`;

  function regionsAnswer(t, seg, session, record, { analysis, cells }) {
    const r = analysis.regions;
    const names = analysis.labels.names;
    const K = names.length;
    const rows = rowNames(r);
    const said = rowNames(r, true);
    const R = rows.length;
    if (!session.rows || session.rows[0] >= R || session.rows[1] >= R || session.rows[0] === session.rows[1]) session.rows = [0, R - 1];
    if (session.label === null || session.label >= K) session.label = 0;
    const pickLabel = (k) => {
      session.label = k;
      render(true);
    };
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${K})`)),
      h('dt', 'Regions'), h('dd', r.regions.map((x) => `${x.name} (v${x.version})`).join(', ')),
      h('dt', 'Window'), h('dd', windowText(analysis.window)),
      h('dt', 'Cells'), h('dd', `${exact(analysis.cells.labeled)} labeled of ${exact(analysis.cells.analyzed)}`, r.outside ? h('span.muted', `; ${exact(r.outside)} outside the tissue window left out`) : null),
      h('dt', 'Intervals'), h('dd', levelText(analysis.parameters)),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const rowPick = (id, i, label) => {
      const el = h('select.input', { id }, ...rows.map((n, k) => h('option', { value: String(k) }, n)));
      el.value = String(session.rows[i]);
      el.addEventListener('change', () => {
        const next = [...session.rows];
        next[i] = Number(el.value);
        if (next[0] === next[1]) next[1 - i] = session.rows[i];
        session.rows = next;
        render(true);
      });
      return h('label.field', { for: id }, h('span', label), el);
    };
    const [a, b] = session.rows;
    const k = session.label;
    const q = (a * R + b) * K + k;
    const share = session.measure === 'share';
    const parts = [kv, measurePick(session), regionsTable(analysis, { measure: session.measure, onPick: pickLabel, chosen: k }),
      h('div.section-title', 'Two compared'),
      h('div.seg-import-grid', rowPick('sp-row-a', 0, 'The cells in'), rowPick('sp-row-b', 1, 'Over those in')),
      ratioForest(analysis, { r: a, s: b, measure: session.measure, onPick: pickLabel, chosen: k })];
    const value = (row) => {
      const at = row * K + k;
      return share ? `${(100 * (r.share[at] ?? NaN)).toFixed(1)}% of its labeled cells` : `${exact(Math.round(r.density[at] ?? NaN))} a mm²`;
    };
    const v = share ? [r.shareRatio[q], r.shareRatioLo[q], r.shareRatioHi[q]] : [r.densityRatio[q], r.densityRatioLo[q], r.densityRatioHi[q]];
    const lo = v[1] ?? NaN;
    const hi = v[2] ?? (Number.isFinite(lo) ? Infinity : NaN);
    const ratio = v[0] ?? (lo > 0 ? Infinity : NaN);
    const rowSelect = h('select.input', { id: 'sp-show-row', 'aria-label': 'The cells to show: those in this row' }, ...said.map((n, i) => h('option', { value: String(i) }, `in ${n}`)));
    rowSelect.value = String(a);
    parts.push(h('div.seg-card.sp-pair',
      h('b', `${names[k]}: ${said[a]} over ${said[b]}`),
      h('p', `${exact(r.count[a * K + k])} ${names[k]} cells in ${said[a]}, ${value(a)}; ${exact(r.count[b * K + k])} in ${said[b]}, ${value(b)}. ${share ? 'The share' : 'The density'}, ${said[a]} over ${said[b]}: ${ratioText(ratio, lo, hi)}, ${lo > 1 ? 'higher' : hi < 1 ? 'lower' : 'not shown to differ'}.`),
      h('div.btn-row', rowSelect, h('button.btn.small.primary', { type: 'button', 'data-action': 'show-region', onclick: () => showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, a: k, row: Number(rowSelect.value) }).catch((error) => toast(`The cells: ${error.message}`, { kind: 'error' })) }, icon('tissue'), 'Show these cells on the tissue'))));
    parts.push(h('p.muted.seg-note', `Densities are cells per mm² of the tissue within each region (its empty glass and holes not counted); shares, of the region's labeled cells. Filled points in the comparison: intervals clear of 1. ${r.regions.length > 1 ? 'Regions that overlap share cells, so their rows are not independent. ' : ''}A section's regions are not samples: these describe this section.`));
    return parts;
  }

  function boundaryAnswer(t, seg, session, record, { analysis, cells }) {
    const b = analysis.boundary;
    const names = analysis.labels.names;
    const K = names.length;
    const p = analysis.parameters;
    if (session.label === null || session.label >= K) session.label = 0;
    const k = session.label;
    const pickLabel = (x) => {
      session.label = x;
      render(true);
    };
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${K})`)),
      h('dt', 'Edge'), h('dd', `of ${b.region.name} (v${b.region.version})`),
      h('dt', 'Bands'), h('dd', `every ${p.step} µm to ${p.reach} µm inside and outside, the deeper and the farther a band each; the margin ${p.margin} µm either side`),
      h('dt', 'Window'), h('dd', windowText(analysis.window)),
      h('dt', 'Cells'), h('dd', `${exact(analysis.cells.labeled)} labeled of ${exact(analysis.cells.analyzed)}`, b.outside ? h('span.muted', `; ${exact(b.outside)} outside the tissue window left out`) : null),
      h('dt', 'Intervals'), h('dd', levelText(p)),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const bands = bandsOf(b.edges);
    const zones = ['core', 'margin', 'around'].map((z) => [z, zoneBands(b.edges, p.margin, z)]).filter(([, r]) => r);
    if (!zones.some(([z]) => z === session.zone)) session.zone = zones[0]?.[0] ?? 'all';
    const zonePick = h('select.input', { id: 'sp-zone', 'aria-label': 'Where to show the cells' },
      ...zones.map(([z]) => h('option', { value: z }, { core: 'in the core', margin: 'at the margin', around: 'around' }[z])), h('option', { value: 'all' }, 'in every band'));
    zonePick.value = session.zone;
    zonePick.addEventListener('change', () => {
      session.zone = zonePick.value;
    });
    const zoneText = { core: `the core (inside, beyond ${p.margin} µm)`, margin: `the margin (within ${p.margin} µm of the edge)`, around: `around (outside, ${p.margin} to ${p.reach} µm)` };
    const C = 3;
    const ri = (c) => {
      const lo = b.ratioLo[k * C + c] ?? NaN;
      return ratioText(b.ratio[k * C + c] ?? (lo > 0 ? Infinity : NaN), lo, b.ratioHi[k * C + c] ?? (Number.isFinite(lo) ? Infinity : NaN));
    };
    const reading = [b.inside[k] > 0 ? 'Denser in the core than around: it fills the region' : b.inside[k] < 0 ? 'Sparser in the core than around: it stays out' : 'Not shown denser or sparser in the core than around', b.atMargin[k] ? 'denser at the margin than in both: it gathers there' : null].filter(Boolean).join('; ');
    const parts = [kv, readingsTable(analysis, { onPick: pickLabel, chosen: k }),
      h('div.seg-import-grid', h('label.field', { for: 'sp-profile-label' }, h('span', 'Profile of'), (() => {
        const el = h('select.input', { id: 'sp-profile-label' }, ...names.map((n, x) => h('option', { value: String(x) }, n)));
        el.value = String(k);
        el.addEventListener('change', () => pickLabel(Number(el.value)));
        return el;
      })())),
      measurePick(session),
      profileChart(analysis, k, { measure: session.measure }),
      h('div.seg-card.sp-pair',
        h('b', `${names[k]} across the edge of ${b.region.name}`),
        h('p', `The core over around ${ri(0)}; the margin over the core ${ri(1)} and over around ${ri(2)}. ${reading}.`),
        h('div.btn-row', zonePick, h('button.btn.small.primary', { type: 'button', 'data-action': 'show-band', onclick: () => {
          const range = session.zone === 'all' ? { from: 0, to: bands.length - 1 } : zoneBands(b.edges, p.margin, session.zone);
          showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, a: k, bands: range, where: session.zone === 'all' ? 'in every band' : zoneText[session.zone] }).catch((error) => toast(`The cells: ${error.message}`, { kind: 'error' }));
        } }, icon('tissue'), 'Show these cells on the tissue'))),
      h('p.muted.seg-note', 'Each band\'s density is its cells over its own area of tissue, so a band that runs off the tissue, or into a hole, is not diluted. The core, margin and around are compared by their densities\' ratios: a label fills the region when the core\'s interval lies above around\'s, stays out when below, and gathers at the margin when the margin is denser than both.')];
    return parts;
  }

  function nichesAnswer(t, seg, session, record, { analysis, cells }) {
    const r = analysis.niches;
    const names = analysis.labels.names;
    const K = names.length;
    const p = analysis.parameters;
    const niches = nicheNames(r, names);
    if (session.niche !== null && session.niche >= r.niches) session.niche = null;
    const pick = (v) => {
      session.niche = v;
      render(true);
    };
    const agreement = r.agreement?.length ? `the niches found from ${r.agreement.length} other seeds agree with these at an adjusted Rand index of ${Math.min(...r.agreement).toFixed(2)} to ${Math.max(...r.agreement).toFixed(2)}` : 'not measured (no other seed)';
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Labels'), h('dd', analysis.labels.name, h('span.muted', ` (${K})`)),
      h('dt', 'Cells'), h('dd', `${exact(r.windows)} with a neighborhood, of ${exact(analysis.cells.analyzed)}; ${exact(analysis.cells.labeled)} labeled`, analysis.cells.regions?.length ? h('span.muted', ` in ${analysis.cells.regions.map((x) => x.name).join(', ')}`) : null),
      h('dt', 'Neighborhood'), h('dd', p.radius > 0 ? `the labeled cells within ${p.radius} µm of each cell` : `each cell's ${p.neighbors} nearest labeled cells, itself among them when labeled`),
      h('dt', 'k-means'), h('dd', `${r.niches} niches, the best of ${p.starts} starts (seed ${p.seed}); within-niche sum of squares ${fixed(r.inertia, 1)}`),
      h('dt', 'Stability'), h('dd', agreement),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const parts = [kv, nicheHeatmap(analysis, { onPick: pick, chosen: session.niche }), nichesTable(analysis, { onPick: pick, chosen: session.niche }),
      h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'show-niches', onclick: () => showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, id: record.id }).catch((error) => toast(`The niches: ${error.message}`, { kind: 'error' })) }, icon('tissue'), 'Show the niches on the tissue'))];
    if (session.niche !== null) {
      const v = session.niche;
      const genes = (r.markers[v] ?? []).slice(0, 8).map((m) => `${m.gene} (${m.logfc > 0 ? '+' : ''}${fixed(m.logfc, 1)}, ${Math.round(100 * m.pctIn)}% against ${Math.round(100 * m.pctOut)}%)`);
      parts.push(h('div.seg-card.sp-pair',
        h('b', `Niche ${niches[v]}`),
        h('p', `${exact(r.cells[v])} cells, ${(100 * r.cells[v] / r.windows).toFixed(1)}% of those with a neighborhood. Its neighborhoods: ${r.defining[v].map((k) => `${names[k]} ${(100 * r.composition[v * K + k]).toFixed(0)}% (the tissue ${(100 * r.tissue[k]).toFixed(0)}%)`).join(', ')}.`),
        genes.length ? h('p', `Its cells' leading genes against the other niches' (log₂ fold change; cells expressing it here against elsewhere): ${genes.join('; ')}.`) : null,
        h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'show-niche', onclick: () => showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, id: record.id, niche: v }).catch((error) => toast(`The niche: ${error.message}`, { kind: 'error' })) }, icon('tissue'), 'Show this niche\'s cells on the tissue'))));
    }
    parts.push(h('p.muted.seg-note', 'Red in the heat map: a label more common in a niche\'s neighborhoods than in the tissue; blue, less. A niche is a kind of place: its genes are much those of the cells that define it, and they are exploratory (its cells are not replicates). The niches are a label set too: fill the Viewer by them, or ask the other questions of them.'));
    return parts;
  }

  function moranAnswer(t, seg, session, record, { analysis, cells, local }) {
    const r = analysis.moran;
    const p = analysis.parameters;
    const pick = (g) => {
      session.gene = g;
      render(true);
    };
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Graph'), h('dd', analysis.graph.name),
      h('dt', 'Cells'), h('dd', `${exact(r.cells)} in the graph`, h('span.muted', `${r.islands ? `; ${exact(r.islands)} without a neighbor, weighing nothing` : ''}${r.noTranscript ? `; ${exact(r.noTranscript)} without a gene transcript, zeros` : ''}`)),
      h('dt', 'Genes'), h('dd', `${exact(r.genes.length)}, log-normalized as SituCraft clusters them (each cell scaled to ${exact(Math.round(r.target))} transcripts, log1p)`),
      h('dt', 'Weights'), h('dd', r.weights === 'row' ? 'each cell\'s neighbors share 1 (row-standardized)' : 'each edge 1 (binary)'),
      h('dt', 'Test'), h('dd', `against the values dealt to the cells at random: ${p.permutations ? `${exact(p.permutations)} permutations (seed ${p.seed})` : 'analytically, under randomization'}; Benjamini–Hochberg over the genes; the expectation ${fixed(r.expected, 5)}`),
      h('dt', 'Artifact'), h('dd.mono', record.id));
    const parts = [kv, moranTable(analysis, { onPick: pick, chosen: session.gene })];
    if (session.gene !== null && session.gene < r.genes.length) {
      const g = session.gene;
      const k = r.local.indexOf(g);
      const lines = [`Moran's I ${fixed(r.i[g], 3)} (the expectation ${fixed(r.expected, 4)}): z ${fixed(r.z[g], 1)} under randomization (two-sided p ${pvalue(r.p[g])})${Number.isFinite(r.permP?.[g]) ? `, permutation p ${pvalue(r.permP[g])}` : ''}, adjusted p ${pvalue(r.padj[g])}; expressed in ${(100 * r.detected[g] / r.cells).toFixed(0)}% of the cells.`];
      const panel = [h('b', r.genes[g]), h('p', lines[0])];
      if (k >= 0 && local) {
        const counts = [1, 2, 3, 4].map((c) => local.classes[k].reduce((n, v) => n + (v === c ? 1 : 0), 0));
        panel.push(h('p', `Its local Moran's I (p below 0.05, unadjusted: where the pattern lies, not a test of it): ${counts.map((n, c) => `${exact(n)} cells ${LOCAL_CLASSES[c + 1]}`).join(', ')}.`));
        const classPick = h('select.input', { id: 'sp-local-class', 'aria-label': 'The cells to show: those of this class' }, ...[1, 2, 3, 4, 0].map((c) => h('option', { value: String(c) }, c === 0 ? 'every class' : LOCAL_CLASSES[c])));
        classPick.value = String(session.localClass);
        classPick.addEventListener('change', () => {
          session.localClass = Number(classPick.value);
        });
        panel.push(h('div.btn-row', classPick, h('button.btn.small.primary', { type: 'button', 'data-action': 'show-local', onclick: () => showAnalysisCells(app, { dataset: t.dataset.id, item: t.item, segmentation: seg, analysis, cells, local, gene: g, localClass: session.localClass }).catch((error) => toast(`The cells: ${error.message}`, { kind: 'error' })) }, icon('tissue'), 'Show these cells on the tissue')));
      } else {
        panel.push(h('p.muted', `Local Moran's I is kept for the ${r.local.length} leading genes; ask again with more to see where ${r.genes[g]}'s pattern lies.`));
      }
      parts.push(h('div.seg-card.sp-pair', ...panel));
    }
    parts.push(h('p.muted.seg-note', 'Moran\'s I measures how alike neighbors\' expression is: 1 alike everywhere, about 0 for no pattern, below 0 where neighbors differ. Genes of cell types that gather (a tissue\'s domains) are in a pattern because their cells are; a section\'s cells are not replicates, so the p-values rank genes in this section.'));
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
    // step, pairs, dPermutations, reach, dStep, upTo, every, correction, window, closing, compare:
    // region ids, edge: a region's id, bReach, bStep, margin, windowBy, neighbors, nRadius, niches,
    // starts, seeds, mWeights, mPermutations, local, localPermutations }); answers its manifest.
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
      Object.assign(q, {
        ...ENRICHMENT_DEFAULTS, farthest: 150, step: 10, pairs: COOCCURRENCE_DEFAULTS.pairs, cells: 'all',
        dPermutations: DISTANCES_DEFAULTS.permutations, reach: DISTANCES_DEFAULTS.reach, dStep: DISTANCES_DEFAULTS.step, upTo: 100, every: 5, correction: RIPLEY_DEFAULTS.correction, window: 'tissue', closing: 10,
        edge: '', bReach: BOUNDARY_DEFAULTS.reach, bStep: BOUNDARY_DEFAULTS.step, margin: BOUNDARY_DEFAULTS.margin,
        windowBy: 'neighbors', neighbors: NICHES_DEFAULTS.neighbors, nRadius: 30, niches: NICHES_DEFAULTS.niches, starts: NICHES_DEFAULTS.starts, seeds: NICHES_DEFAULTS.seeds,
        mWeights: MORAN_DEFAULTS.weights, mPermutations: MORAN_DEFAULTS.permutations, local: MORAN_DEFAULTS.local, localPermutations: MORAN_DEFAULTS.localPermutations,
      });
      for (const k of ['question', 'labels', 'graph', 'permutations', 'seed', 'weight', 'cells', 'farthest', 'step', 'pairs', 'dPermutations', 'reach', 'dStep', 'upTo', 'every', 'correction', 'window', 'closing', 'edge', 'bReach', 'bStep', 'margin', 'windowBy', 'neighbors', 'nRadius', 'niches', 'starts', 'seeds', 'mWeights', 'mPermutations', 'local', 'localPermutations']) if (args[k] !== undefined) q[k] = args[k];
      q.within = new Set(args.within ?? []);
      q.regions = new Set(args.regions ?? []);
      q.compare = new Set(args.compare ?? []);
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
