// The Cell types view: clusters of a segmentation's cells by their gene expression (internal/
// cluster, api_clustering.go), each clustering an artifact with every parameter recorded; the map
// of its cells (internal/embed, api_maps.go) beside the tissue, linked (ui/cell-map.js): colored by
// cluster, gene or attribute, one selection on both; its clusters with their sizes, names and
// markers, as a table and a dot plot; the markers of any selection; labels from markers (ui/
// labels-card.js); the instrument's own clusters and map, registered from the bundle; and two
// clusterings compared. Imported labels and a reference come in later slices of wave 4
// (situcraft-spec/roadmap.md).

import { h, icon, clear, formatCount, relativeTime } from './dom.js';
import { toast } from './overlays.js';
import { categoricalColor, colormapLUT } from '../lib/colormaps.js';
import { promotedSegmentation, segmentationsOf } from '../lib/segmentations.js';
import { evaluate, keepChecksum } from '../lib/filters.js';
import { readCells } from '../lib/viewer-data.js';
import { PARAMETERS, checkParameters, clusterName, clusterNames, clusteringsOf, contingency, describeClustering, filterRequest, mapsOf, mergedOf, nameClusters, nameOf } from '../lib/clusterings.js';
import { mountCellMap } from './cell-map.js';
import { labelClustering, labelsSections } from './labels-card.js';
import { labelSetsOf } from '../lib/labels.js';

const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const percent = (v, digits = 1) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');
const fixed = (v, digits = 2) => (Number.isFinite(v) ? v.toFixed(digits) : '—');
const pvalue = (p) => (!Number.isFinite(p) ? '—' : p === 0 ? '< 1e-300' : p < 1e-3 ? p.toExponential(1) : p.toFixed(3));

// Per dataset id, what the view shows and the parameters being edited, for the session.
const sessions = new Map();

let defaults = null;
async function clusteringDefaults() {
  if (!defaults) {
    const response = await fetch('/api/clustering/defaults');
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    defaults = await response.json();
  }
  return defaults;
}

export function mountCellTypes(app, container) {
  const title = h('span.crumbs');
  const body = h('div.seg-body.ct-body');
  const root = h('div.view.celltypes', h('div.workbench-head', h('h1', icon('tag'), 'Cell types', title)), h('div.workbench-scroll', body));
  container.append(root);
  const live = new Map(); // job id → { bar, message }
  let shape = '';
  let generation = 0;
  // The map and the tissue: one component, moved into each render.
  const mapHost = h('div.ct-map-host');
  let cellMap = null;
  let selection = { mask: null, count: 0, markers: null, clustering: null };
  const selectionHost = h('div.ct-selection');
  const analyses = new Map(); // item path → the instrument's analysis listed

  function target() {
    const { project, ui } = app.store;
    const focusedLabels = ui.focus?.kind === 'labels' ? project.artifacts.find((a) => a.kind === 'labels' && a.id === ui.focus.id) : null;
    // An imported label set is of a segmentation: shown under its latest clustering.
    const cluOf = (r) => r.clustering ?? project.artifacts.filter((a) => a.kind === 'clustering' && a.segmentation === r.segmentation && a.state === 'complete').at(-1)?.id;
    const focusedClu = ui.focus?.kind === 'clustering' || focusedLabels ? project.artifacts.find((a) => a.kind === 'clustering' && a.id === (focusedLabels ? cluOf(focusedLabels) : ui.focus.id)) : null;
    const seg = ui.focus?.kind === 'segmentation' ? project.artifacts.find((a) => a.kind === 'segmentation' && a.id === ui.focus.id) : null;
    const id = focusedClu?.dataset ?? (ui.focus?.kind === 'dataset' ? ui.focus.id : seg?.dataset) ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), focusedClustering: focusedClu?.dataset === dataset.id ? focusedClu : null, focusedLabels: focusedLabels?.dataset === dataset.id ? focusedLabels : null, focusedSegmentation: seg?.dataset === dataset.id ? seg.id : null };
  }

  function sessionOf(dataset) {
    if (!sessions.has(dataset.id)) sessions.set(dataset.id, { segmentation: '', cells: 'rule', params: null, shown: null, cluster: 0, compare: { a: '', b: '' } });
    return sessions.get(dataset.id);
  }

  // The segmentations clustered: complete ones, the default first.
  function segmentationList(t) {
    const promoted = promotedSegmentation(app.store.project, t.dataset.id)?.record.id;
    const list = segmentationsOf(app.store.project, t.dataset.id).filter((a) => a.state === 'complete' && a.source !== 'planted');
    return [...list.filter((a) => a.id === promoted), ...list.filter((a) => a.id !== promoted)].map((a) => ({ ...a, isDefault: a.id === promoted }));
  }

  // The QC filters of a dataset that choose cells of the instrument's segmentation (their
  // conditions are on its cells), latest versions first.
  function filtersOf(t) {
    return app.store.project.artifacts.filter((a) => a.kind === 'filter' && a.dataset === t.dataset.id).sort((a, b) => b.version - a.version || (a.created < b.created ? 1 : -1));
  }

  // A filter's cells, applied again to the dataset's cells and checked against its recorded checksum.
  async function filterCells(t, filter) {
    const response = await fetch(`${t.item.url}/cells`);
    if (!response.ok) throw new Error(`the dataset's cells: HTTP ${response.status}`);
    const cells = readCells(await response.arrayBuffer());
    const { keep } = evaluate(filter.conditions, cells);
    if (keepChecksum(keep) !== filter.result.sha256) throw new Error(`the filter ${filter.name} v${filter.version} no longer keeps the cells it recorded`);
    return filterRequest(filter, keep);
  }

  // A clustering made: shown, and its map made at once.
  async function run(t, session, request) {
    const problem = checkParameters(request.parameters);
    if (problem) throw new Error(problem);
    const manifest = await app.bundles.clusterCells(t.item, t.dataset.id, request);
    session.shown = manifest.id;
    session.cluster = 0;
    render(true);
    ensureMap(t, manifest.id);
    return manifest;
  }

  // Makes a SituCraft clustering's map when it has none (another tool's clustering uses its own).
  function ensureMap(t, clusteringId) {
    const c = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === clusteringId);
    if (!c || c.state !== 'complete' || c.summary?.source || !t.item) return null;
    if (app.store.project.artifacts.some((a) => a.kind === 'embedding' && a.clustering === clusteringId)) return null;
    // A map under way answers its own promise (no second job).
    if (app.bundles.mappings(t.dataset.id).some((e) => e.clustering === clusteringId)) return app.bundles.makeMap(t.item, t.dataset.id, clusteringId);
    const p = app.bundles.makeMap(t.item, t.dataset.id, clusteringId);
    p.then(() => render(true)).catch((error) => toast(`The map: ${error.message}`, { kind: 'error', timeout: 10000 }));
    return p;
  }

  async function runFromForm(t, session) {
    try {
      const request = { segmentation: session.segmentation, parameters: { ...session.params } };
      if (session.cells !== 'rule') {
        const filter = app.store.project.artifacts.find((a) => a.kind === 'filter' && a.id === session.cells);
        if (!filter) throw new Error('the filter chosen is no longer in the project');
        request.filter = await filterCells(t, filter);
      }
      const m = await run(t, session, request);
      toast(`Clustered: ${m.summary?.clusters ?? '?'} clusters of ${exact(m.summary?.cells)} cells.`, { kind: 'ok' });
    } catch (error) {
      toast(`The clustering: ${error.message}`, { kind: 'error', timeout: 12000 });
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

  // --- Clustering: the form ---
  function formCard(t, segs, session) {
    const seg = segs.find((s) => s.id === session.segmentation);
    const instrument = seg?.source === 'instrument';
    const filters = instrument ? filtersOf(t) : [];
    if (session.cells !== 'rule' && !filters.some((f) => f.id === session.cells)) session.cells = 'rule';
    const segPick = h('select.input', { id: 'ct-segmentation' }, ...segs.map((s) => h('option', { value: s.id }, `${s.name}${s.isDefault ? ' (default)' : ''}`)));
    segPick.value = session.segmentation;
    segPick.addEventListener('change', () => {
      session.segmentation = segPick.value;
      session.shown = null;
      render(true);
    });
    const cellsPick = h('select.input', { id: 'ct-cells' },
      h('option', { value: 'rule' }, `At least ${session.params.minTranscripts} gene transcripts`),
      ...filters.map((f) => h('option', { value: f.id }, `QC filter ${f.name} v${f.version} (${formatCount(f.result.kept)} cells)`)));
    cellsPick.value = session.cells;
    cellsPick.addEventListener('change', () => {
      session.cells = cellsPick.value;
      render(true);
    });
    const inputs = PARAMETERS.map((spec) => {
      const input = h('input.input', { type: 'number', id: `ct-${spec.id}`, min: String(spec.min), max: String(spec.max), step: String(spec.step), value: String(session.params[spec.id]), title: spec.help, disabled: spec.id === 'minTranscripts' && session.cells !== 'rule' });
      input.addEventListener('change', () => {
        session.params[spec.id] = Number(input.value);
        if (spec.id === 'minTranscripts') cellsPick.options[0].textContent = `At least ${session.params.minTranscripts} gene transcripts`;
      });
      return h('label.field', { for: `ct-${spec.id}`, title: spec.help }, h('span', spec.label), input);
    });
    const go = h('button.btn.primary', { type: 'button', disabled: !seg || !t.item, 'data-action': 'cluster', onclick: () => runFromForm(t, session) }, icon('play'), 'Cluster the cells');
    const segName = (id) => segs.find((s) => s.id === id)?.name ?? 'the cells';
    return h('section.seg-section',
      h('div.section-title', 'Clusters'),
      h('div.seg-card',
        h('p', 'Cells grouped by their gene expression, as the common single-cell workflow does it: each cell\'s counts scaled to the median total and log-transformed, each gene scaled, principal components, each cell\'s nearest neighbors, then Leiden communities of the neighbor graph. Every parameter is recorded, and the same seed gives the same clusters on any computer. A map of the cells is made with each clustering.'),
        h('div.seg-import-grid',
          h('label.field', { for: 'ct-segmentation' }, h('span', 'Segmentation'), segPick),
          h('label.field', { for: 'ct-cells' }, h('span', 'Cells analyzed'), cellsPick)),
        h('details.seg-advanced', { open: session.open ?? false, ontoggle: (event) => { session.open = event.currentTarget.open; } },
          h('summary', 'Parameters'),
          h('div.seg-import-grid', ...inputs),
          h('p.muted.seg-note', 'Hover a parameter for what it does. The defaults are scanpy\'s: 50 components, 15 neighbors, resolution 1.')),
        h('div.seg-form', go, !t.item ? h('span.muted', `${t.dataset.name}'s bundle was not found where the project records it: locate it in the Data view.`) : null),
        ...progress(app.bundles.clusterings(t.dataset.id), (e) => `Clustering ${segName(e.segmentation)}`),
        ...progress(app.bundles.mappings(t.dataset.id), () => 'Mapping the cells'),
        h('p.muted.seg-cite', 'Methods: normalization and scaling as scanpy (Wolf et al. 2018); exact principal components; UMAP\'s neighbor graph and map (McInnes et al. 2018); Leiden (Traag et al. 2019); markers by the Wilcoxon rank-sum test (Mann & Whitney 1947), exploratory: cells are not replicates.')));
  }

  // --- The clusterings made ---
  function listCard(t, seg, session) {
    const list = clusteringsOf(app.store.project, t.dataset.id, seg?.id).filter((c) => c.state === 'complete');
    if (!list.some((c) => c.id === session.shown)) session.shown = list.filter((c) => !c.summary?.source).at(-1)?.id ?? list.at(-1)?.id ?? null;
    if (!list.length) return null;
    const choose = (c) => {
      session.shown = c.id;
      session.cluster = 0;
      render(true);
      ensureMap(t, c.id);
    };
    const rows = list.map((c) => {
      const s = c.summary ?? {};
      return h(`tr${session.shown === c.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-clustering': c.id, onclick: () => choose(c), onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          choose(c);
        }
      } },
      h('td', s.source ? h('span', c.name.replace(/^The instrument's /, 'Instrument: ')) : `Resolution ${c.parameters?.resolution ?? '—'}`),
      h('td.r', exact(s.clusters)),
      h('td.r', exact(s.cells)),
      h('td', s.source ? `${s.source.tool} ${s.source.version}` : s.filter ? `filter ${s.filter.name}` : `≥ ${c.parameters?.minTranscripts ?? '?'} transcripts`),
      h('td.r', s.source ? '—' : fixed(s.modularity, 3)),
      h('td.r', s.source ? '—' : String(c.parameters?.seed ?? '—')),
      h('td.muted', relativeTime(c.created)),
      h('td', h('button.icon-button', { type: 'button', title: 'Show the clusters on the tissue in the Viewer', 'aria-label': 'Show the clusters on the tissue in the Viewer', onclick: (event) => {
        event.stopPropagation();
        app.showClusters(c);
      } }, icon('tissue'))));
    });
    return h('section.seg-section',
      h('div.section-title', `Clusterings of ${seg.name}`),
      h('div.table-wrap', h('table.data-table.seg-table',
        h('thead', h('tr', h('th', 'Clustering'), h('th.r', 'Clusters'), h('th.r', 'Cells'), h('th', 'Cells analyzed'), h('th.r', 'Modularity'), h('th.r', 'Seed'), h('th', 'Made'), h('th', ''))),
        h('tbody', ...rows))));
  }

  // --- The map and the tissue ---
  function mapCard(t, seg, session) {
    const project = app.store.project;
    const clusterings = clusteringsOf(project, t.dataset.id, seg.id).filter((c) => c.state === 'complete');
    if (!clusterings.length) return null;
    const maps = mapsOf(project, t.dataset.id, seg.id);
    const names = Object.fromEntries(clusterings.map((c) => [c.id, clusterNames(project, c.id)]));
    const ctx = {
      item: t.item, dataset: t.dataset, segmentation: seg, instrument: seg.source === 'instrument', clusterings, maps, names, shown: session.shown,
      labelSets: labelSetsOf(project, t.dataset.id).filter((r) => r.segmentation === seg.id),
      onSelect: (mask, count) => {
        selection = { mask, count, markers: null, clustering: session.shown };
        drawSelection(t, session);
      },
    };
    if (!cellMap) cellMap = mountCellMap(app, mapHost, ctx);
    else cellMap.update(ctx);
    return h('section.seg-section',
      h('div.section-title', 'The map and the tissue'),
      h('p.muted.seg-note', 'The map (UMAP) puts cells with similar expression near each other. Draw around cells with the lasso on either side, or click a cluster below, to select them on both; click a cell on the map to find it on the tissue.'),
      mapHost,
      selectionHost);
  }

  function drawSelection(t, session) {
    clear(selectionHost);
    if (!selection.count) return;
    const clusteringId = session.shown;
    const c = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === clusteringId);
    const parts = [];
    app.bundles.clusteringData(clusteringId).then((data) => {
      const counts = new Array(data.sizes.length).fill(0);
      let analyzed = 0;
      for (let i = 0; i < data.cluster.length; i += 1) {
        if (selection.mask[i] && data.cluster[i] >= 0) {
          counts[data.cluster[i]] += 1;
          analyzed += 1;
        }
      }
      const names = clusterNames(app.store.project, clusteringId);
      const bar = h('div.ct-composition', { role: 'img', 'aria-label': 'The selection by cluster' }, ...counts.map((n, k) => (n ? h('span', { style: { width: `${(100 * n) / Math.max(analyzed, 1)}%`, background: categoricalColor(mergedOf(names, data.sizes.length)[k]) }, title: `${nameOf(names, k)}: ${formatCount(n)}` }) : null)));
      const top = counts.map((n, k) => [n, k]).filter(([n]) => n).sort((a, b) => b[0] - a[0]).slice(0, 4).map(([n, k]) => `${nameOf(names, k)} ${percent(n / analyzed, 0)}`).join(', ');
      parts.push(h('div', h('b', `${formatCount(selection.count)} cells selected`), h('span.muted', ` (${formatCount(analyzed)} in ${c?.name.toLowerCase() ?? 'the clustering'}): ${top}`)), bar);
      const markersButton = h('button.btn.small', { type: 'button', disabled: !analyzed || analyzed === data.cluster.filter((v) => v >= 0).length, onclick: async () => {
        markersButton.disabled = true;
        try {
          selection.markers = await app.bundles.contrast(t.item, clusteringId, selection.mask);
          drawSelection(t, session);
        } catch (error) {
          toast(`The selection's markers: ${error.message}`, { kind: 'error' });
          markersButton.disabled = false;
        }
      } }, icon('sparkles'), 'Markers of the selection');
      parts.push(h('div.btn-row', markersButton));
      if (selection.markers) parts.push(contrastTables(selection.markers));
      clear(selectionHost).append(...parts);
    }).catch(() => {});
  }

  function contrastTables(m) {
    const table = (list, caption) => h('div', h('div.section-title', caption), h('div.table-wrap', h('table.data-table.seg-table.ct-markers',
      h('thead', h('tr', h('th', 'Gene'), h('th.r', 'log₂ fold change'), h('th.r', 'Cells expressing in'), h('th.r', 'out'), h('th.r', 'Score'), h('th.r', 'Adjusted p'))),
      h('tbody', ...list.slice(0, 12).map((g) => h('tr', h('td', h('b', g.gene)), h('td.r', fixed(g.logfc)), h('td.r', percent(g.pctIn, 0)), h('td.r', percent(g.pctOut, 0)), h('td.r', fixed(g.score, 1)), h('td.r', pvalue(g.padj))))))));
    return h('div.ct-contrast', table(m.up, `Higher in the ${formatCount(m.selected)} selected cells`), table(m.down, 'Lower in them'),
      h('p.muted.seg-note', 'Exploratory, as the clusters\' markers: the selection against the clustering\'s other cells.'));
  }

  // --- One clustering: its numbers, clusters, names and markers ---
  function detailCard(t, session) {
    const c = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === session.shown);
    if (!c) return null;
    const host = h('section.seg-section.ct-detail', h('div.section-title', 'The clusters'), h('p.muted.seg-note', 'Reading the clustering…'));
    const mine = generation;
    Promise.all([app.bundles.clusteringData(c.id), app.bundles.clusteringMarkers(c.id, 25)]).then(([data, markers]) => {
      if (mine !== generation) return;
      clear(host).append(h('div.section-title', 'The clusters'), ...detail(t, c, data.description, markers, session));
    }).catch((error) => {
      if (mine !== generation) return;
      clear(host).append(h('div.section-title', 'The clusters'), h('p.muted.seg-note', `The clustering could not be read: ${error.message}`));
    });
    return host;
  }

  function detail(t, c, d, markers, session) {
    const sizes = d.clusters.sizes;
    const n = d.cells.analyzed;
    const source = d.source;
    const names = clusterNames(app.store.project, c.id);
    const merged = mergedOf(names, sizes.length);
    const kv = source
      ? h('dl.kv.seg-kv',
        h('dt', 'Made by'), h('dd', `${source.tool} ${source.version}: its ${source.title.toLowerCase()} clustering`),
        h('dt', 'File'), h('dd.mono', source.file),
        h('dt', 'Cells'), h('dd', `${exact(n)} of ${exact(d.cells.segmentation)}`, d.cells.excluded ? h('span.muted', ` (${exact(d.cells.excluded)} not in its clusters)`) : null),
        h('dt', 'Markers'), h('dd', `by SituCraft, from the cells' counts normalized to the median total (${fixed(d.fixed.targetSum, 1)})`),
        h('dt', 'Artifact'), h('dd.mono', c.id))
      : h('dl.kv.seg-kv',
        h('dt', 'Cells analyzed'), h('dd', `${exact(n)} of ${exact(d.cells.segmentation)}`, d.cells.excluded ? h('span.muted', ` (${exact(d.cells.excluded)} left out${d.cells.filter ? ' by the filter' : ` with fewer than ${d.parameters.minTranscripts} gene transcripts`})`) : null, d.cells.withoutTranscripts ? h('span.muted', `; ${exact(d.cells.withoutTranscripts)} without a gene transcript`) : null),
        h('dt', 'Genes'), h('dd', exact(d.genes.count), d.genes.notDetected ? h('span.muted', ` (${d.genes.notDetected} not detected in these cells)`) : null),
        h('dt', 'Normalization'), h('dd', `to the median total, ${fixed(d.fixed.targetSum, 1)} gene transcripts; log1p; each gene scaled, clipped at ±${d.fixed.clip}`),
        h('dt', 'Components'), h('dd', `${d.pca.components}, holding ${percent(d.pca.varianceRatio.reduce((a, b) => a + b, 0))} of the variance`),
        h('dt', 'Graph'), h('dd', `${d.graph.neighbors} neighbors (the cell included), ${exact(d.graph.edges)} edges`),
        h('dt', 'Leiden'), h('dd', `resolution ${d.parameters.resolution}, seed ${d.parameters.seed}: modularity ${fixed(d.clusters.modularity, 4)} after ${d.clusters.iterations} iteration${d.clusters.iterations === 1 ? '' : 's'}`),
        h('dt', 'Artifact'), h('dd.mono', c.id));
    const scree = source ? null : screePlot(d.pca.varianceRatio);
    const selected = Math.min(session.cluster, sizes.length - 1);
    // Names: each change a new version, kept; the same name on several clusters merges them.
    const pending = names?.names ? [...names.names] : new Array(sizes.length).fill(null);
    const saveNames = () => {
      const next = nameClusters(app.store.project, { clustering: c.id, names: pending });
      if (next !== app.store.project) app.store.commit(next, `Name the clusters of ${c.name}`);
    };
    const rows = sizes.map((size, k) => {
      const top = (d.top?.[k] ?? []).slice(0, 5).map((m) => m.gene);
      const input = h('input.input.small.ct-name', { type: 'text', value: names?.names?.[k] ?? '', placeholder: clusterName(k), 'aria-label': `The name of ${clusterName(k)}`, 'data-name': k, onclick: (event) => event.stopPropagation(), onkeydown: (event) => {
        event.stopPropagation();
        if (event.key === 'Enter') event.currentTarget.blur();
      } });
      input.addEventListener('change', () => {
        pending[k] = input.value.trim() || null;
        saveNames();
      });
      return h(`tr${k === selected ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-cluster': k, onclick: () => {
        session.cluster = k;
        cellMap?.selectClusters(merged.map((r, j) => (r === merged[k] ? j : -1)).filter((j) => j >= 0));
        render(true);
      } },
      h('td', h('span.ct-swatch', { style: { background: categoricalColor(merged[k]) }, 'aria-hidden': 'true' }), clusterName(k)),
      h('td', input),
      h('td.r', exact(size)),
      h('td.r', percent(size / n)),
      h('td', h('span.ct-genes', top.join(', '))));
    });
    const group = markers.clusters[selected];
    const markerRows = (group?.markers ?? []).map((m) => h('tr',
      h('td', h('b', m.gene)),
      h('td.r', fixed(m.logfc, 2)),
      h('td.r', percent(m.pctIn, 0)),
      h('td.r', percent(m.pctOut, 0)),
      h('td.r', fixed(m.score, 1)),
      h('td.r', pvalue(m.padj))));
    // Focusable: it scrolls sideways when the clusters' markers are many.
    const dot = h('div.ct-dotplot', { tabIndex: 0, role: 'region', 'aria-label': 'Dot plot of each cluster\'s leading markers' }, h('p.muted.seg-note', 'Drawing the dot plot…'));
    dotPlot(t, c, d, names, dot);
    return [
      h('div.ct-summary', kv, scree),
      h('div.btn-row', h('button.btn.small.primary', { type: 'button', onclick: () => app.showClusters(c) }, icon('tissue'), 'Show in the Viewer')),
      h('div.table-wrap', h('table.data-table.seg-table.ct-clusters',
        h('thead', h('tr', h('th', 'Cluster'), h('th', 'Name'), h('th.r', 'Cells'), h('th.r', 'Share'), h('th', 'Leading markers'))),
        h('tbody', ...rows))),
      h('p.muted.seg-note', names ? `Names, version ${names.version} (${relativeTime(names.created)}): each change is a new version in the project's history, and the clusters keep their numbers. ` : 'Name a cluster to label it on the map and the tissue. ', 'Give several clusters the same name to merge them; choose a cluster to select its cells on both.'),
      h('div.section-title', { style: { marginTop: '14px' } }, 'Markers by cluster'),
      dot,
      h('div.section-title', { style: { marginTop: '14px' } }, `${nameOf(names, selected)}: markers against the other cells`),
      h('div.table-wrap', h('table.data-table.seg-table.ct-markers',
        h('thead', h('tr', h('th', 'Gene'), h('th.r', { title: 'log₂ of the ratio of the mean expression in the cluster to the rest' }, 'log₂ fold change'), h('th.r', 'Cells expressing in'), h('th.r', 'out'), h('th.r', { title: 'The Wilcoxon rank-sum test\'s z-score' }, 'Score'), h('th.r', { title: 'Benjamini–Hochberg adjusted over the genes' }, 'Adjusted p'))),
        h('tbody', ...markerRows))),
      h('p.muted.seg-note', 'Exploratory: each cell counts as an observation, but cells of one tissue are not independent replicates, so the p-values rank genes rather than test them. Markers are the genes most enriched in the cluster against all other cells analyzed.'),
      h('p.muted.seg-cite', source ? `${c.name}, read from ${source.file}; markers by SituCraft.` : describeClustering(c)),
    ];
  }

  // The dot plot: each cluster's leading markers (two each, the first 40 genes), dot size the share
  // of its cells expressing the gene, color the mean expression scaled per gene.
  async function dotPlot(t, c, d, names, host) {
    const genes = [];
    for (const list of d.top ?? []) {
      for (const m of list.slice(0, 2)) if (!genes.includes(m.gene) && genes.length < 40) genes.push(m.gene);
    }
    if (!genes.length || !t.item) {
      clear(host);
      return;
    }
    try {
      const dp = await app.bundles.dotPlot(t.item, c.id, genes);
      const k = dp.mean.length;
      const cell = 18;
      const left = 150;
      const top = 70;
      const width = left + cell * dp.genes.length + 10;
      const height = top + cell * k + 40;
      const ns = 'http://www.w3.org/2000/svg';
      const el = (tag, attrs, text) => {
        const node = document.createElementNS(ns, tag);
        for (const [key, v] of Object.entries(attrs)) node.setAttribute(key, String(v));
        if (text !== undefined) node.textContent = text;
        return node;
      };
      const svg = el('svg', { width, height, viewBox: `0 0 ${width} ${height}`, role: 'img', 'aria-label': `Dot plot of ${dp.genes.length} marker genes over ${k} clusters` });
      const lut = colormapLUT('viridis');
      dp.genes.forEach((g, j) => {
        const label = el('text', { x: 0, y: 0, transform: `translate(${left + cell * j + cell / 2 + 3},${top - 6}) rotate(-60)` }, g);
        svg.append(label);
        let hi = 0;
        for (let r = 0; r < k; r += 1) hi = Math.max(hi, dp.mean[r][j]);
        for (let r = 0; r < k; r += 1) {
          const share = dp.share[r][j];
          const v = hi > 0 ? dp.mean[r][j] / hi : 0;
          const idx = Math.round(255 * v);
          const dotEl = el('circle', { cx: left + cell * j + cell / 2, cy: top + cell * r + cell / 2, r: Math.max(0.5, (cell / 2 - 1) * Math.sqrt(share)), fill: `rgb(${lut[3 * idx]},${lut[3 * idx + 1]},${lut[3 * idx + 2]})` });
          dotEl.append(el('title', {}, `${nameOf(names, r)}, ${g}: ${percent(share, 0)} expressing, mean ${dp.mean[r][j].toFixed(2)}`));
          svg.append(dotEl);
        }
      });
      for (let r = 0; r < k; r += 1) svg.append(el('text', { x: left - 8, y: top + cell * r + cell / 2 + 4, 'text-anchor': 'end', class: 'ct-row-label' }, nameOf(names, r)));
      svg.append(el('text', { x: left, y: height - 14 }, 'Dot size: the share of the cluster\'s cells expressing the gene; color: its mean expression, scaled per gene (dark low, yellow high).'));
      clear(host).append(svg);
    } catch (error) {
      clear(host).append(h('p.muted.seg-note', `The dot plot could not be drawn: ${error.message}`));
    }
  }

  // The components' share of the variance, as bars.
  function screePlot(ratios) {
    const w = 260;
    const hgt = 90;
    const n = ratios.length;
    const top = Math.max(...ratios, 1e-9);
    const bw = w / n;
    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('viewBox', `0 0 ${w} ${hgt + 16}`);
    svg.setAttribute('class', 'ct-scree');
    svg.setAttribute('role', 'img');
    svg.setAttribute('aria-label', `Variance by component: the first holds ${percent(ratios[0])}, the first ${n} together ${percent(ratios.reduce((a, b) => a + b, 0))}`);
    ratios.forEach((r, i) => {
      const bar = document.createElementNS(ns, 'rect');
      const bh = (hgt * r) / top;
      bar.setAttribute('x', String(i * bw + 0.5));
      bar.setAttribute('y', String(hgt - bh));
      bar.setAttribute('width', String(Math.max(1, bw - 1)));
      bar.setAttribute('height', String(bh));
      const tip = document.createElementNS(ns, 'title');
      tip.textContent = `Component ${i + 1}: ${percent(r, 2)} of the variance`;
      bar.append(tip);
      svg.append(bar);
    });
    const label = document.createElementNS(ns, 'text');
    label.setAttribute('x', '0');
    label.setAttribute('y', String(hgt + 13));
    label.textContent = `Variance by component (1–${n})`;
    svg.append(label);
    return h('figure.ct-scree-figure', svg);
  }

  // --- The instrument's own clusters ---
  function instrumentCard(t, seg) {
    if (seg.source !== 'instrument' || !t.item) return null;
    const known = analyses.get(t.item.path);
    if (!known) {
      analyses.set(t.item.path, { loading: true });
      app.bundles.instrumentAnalysis(t.item).then((a) => {
        analyses.set(t.item.path, a);
        render(true);
      }).catch((error) => analyses.set(t.item.path, { error: error.message }));
      return null;
    }
    if (known.loading || !known.clusterings?.length) return null;
    const registering = app.bundles.registeringAnalysis(t.dataset.id);
    const rows = known.clusterings.map((c) => {
      const recorded = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === c.artifact);
      const busy = registering.some((e) => e.name === c.name);
      return h('tr', h('td', c.title), h('td.r', exact(c.clusters)), h('td.r', exact(c.cells)),
        h('td', recorded ? h('span.badge', 'in the project') : h('button.btn.small', { type: 'button', disabled: busy, 'data-instrument': c.name, onclick: async () => {
          try {
            const m = await app.bundles.registerInstrumentClusters(t.item, t.dataset.id, c.name);
            analyses.delete(t.item.path);
            const session = sessionOf(t.dataset);
            session.shown = m.id;
            render(true);
            toast(`${m.name}: ${m.summary?.clusters} clusters of ${exact(m.summary?.cells)} cells.`, { kind: 'ok' });
          } catch (error) {
            toast(`The instrument's clusters: ${error.message}`, { kind: 'error', timeout: 10000 });
          }
        } }, busy ? 'Adding…' : 'Add')));
    });
    return h('section.seg-section',
      h('div.section-title', 'The instrument\'s clusters'),
      h('div.seg-card',
        h('p', `The instrument clustered the cells itself (${known.source}): graph-based, and by k-means for 2 to 10 clusters, with its own components and map. Add one to explore it here as any clustering (SituCraft computes its markers), compare it with SituCraft's, and see the instrument's map${known.map?.artifact ? '' : ' (added with the first)'}.`),
        h('div.table-wrap', h('table.data-table.seg-table', h('thead', h('tr', h('th', 'Clustering'), h('th.r', 'Clusters'), h('th.r', 'Cells'), h('th', ''))), h('tbody', ...rows))),
        ...progress(registering, (e) => `Adding the instrument's ${known.clusterings.find((c) => c.name === e.name)?.title.toLowerCase() ?? ''} clusters`)));
  }

  // --- Two clusterings compared ---
  function compareCard(t, seg, session) {
    const list = clusteringsOf(app.store.project, t.dataset.id, seg.id).filter((c) => c.state === 'complete');
    if (list.length < 2) return null;
    const state = session.compare;
    if (!list.some((c) => c.id === state.a)) state.a = session.shown ?? list[0].id;
    if (!list.some((c) => c.id === state.b) || state.b === state.a) state.b = list.find((c) => c.id !== state.a)?.id ?? '';
    const label = (c) => (c.summary?.source ? c.name : `Resolution ${c.parameters?.resolution} (${c.summary?.clusters} clusters)`);
    const pick = (key, text) => {
      const el = h('select.input', { id: `ct-compare-${key}` }, ...list.map((c) => h('option', { value: c.id }, label(c))));
      el.value = state[key];
      el.addEventListener('change', () => {
        state[key] = el.value;
        render(true);
      });
      return h('label.field', { for: `ct-compare-${key}` }, h('span', text), el);
    };
    const result = h('div.ct-compare-result', h('p.muted.seg-note', 'Comparing…'));
    const mine = generation;
    const a = list.find((c) => c.id === state.a);
    const b = list.find((c) => c.id === state.b);
    if (a && b) {
      Promise.all([app.bundles.clusteringData(a.id), app.bundles.clusteringData(b.id)]).then(([da, db]) => {
        if (mine !== generation) return;
        const ct = contingency(da.cluster, db.cluster, da.sizes.length, db.sizes.length);
        const na = clusterNames(app.store.project, a.id);
        const nb = clusterNames(app.store.project, b.id);
        const head = h('tr', h('th', ''), ...db.sizes.map((_, j) => h('th', { title: nameOf(nb, j) }, String(j + 1))));
        const rows = ct.table.map((row, i) => h('tr', h('th', { title: nameOf(na, i) }, nameOf(na, i)), ...row.map((v) => {
          const share = ct.rowSums[i] ? v / ct.rowSums[i] : 0;
          return h('td', { style: { background: share > 0.02 ? `color-mix(in srgb, var(--accent) ${Math.round(80 * share)}%, transparent)` : '' }, title: `${formatCount(v)} cells (${percent(share, 0)} of the row)` }, v ? formatCount(v) : '');
        })));
        clear(result).append(
          h('p', h('b', `Adjusted Rand index ${ct.ari.toFixed(3)}`), h('span.muted', ` over the ${formatCount(ct.cells)} cells both cluster (1: the same partition; about 0: as alike as chance).`)),
          h('div.table-wrap', h('table.data-table.ct-contingency', h('thead', head), h('tbody', ...rows))),
          h('p.muted.seg-note', 'Rows: A\'s clusters; columns: B\'s, by number. Each cell counts the cells in both, shaded by its share of the row.'));
      }).catch((error) => clear(result).append(h('p.muted.seg-note', `The comparison: ${error.message}`)));
    }
    return h('section.seg-section',
      h('div.section-title', 'Two clusterings compared'),
      h('div.seg-card', h('div.seg-import-grid', pick('a', 'A'), pick('b', 'B')), result));
  }

  function updateLive() {
    for (const [id, parts] of live) {
      const job = app.jobs.get(id);
      if (!job) continue;
      parts.bar.style.width = `${Math.round(job.progress * 100)}%`;
      parts.message.textContent = job.message ?? '';
    }
  }

  function render(force = false) {
    const t = target();
    if (!t) {
      title.textContent = '';
      shape = '';
      clear(body).append(h('div.planned', h('div.empty', icon('tag'), h('h3', 'No dataset yet'),
        h('p', 'Add a Xenium output bundle to the project in the Data view; its cells can then be clustered here.'),
        h('div.btn-row', { style: { marginTop: '12px' } }, h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')))));
      return;
    }
    const session = sessionOf(t.dataset);
    if (!session.params) {
      clusteringDefaults().then((d) => {
        session.params ??= { ...d.parameters };
        render(true);
      }).catch((error) => toast(`The clustering's defaults: ${error.message}`, { kind: 'error' }));
      return;
    }
    const segs = segmentationList(t);
    if (t.focusedClustering) {
      session.segmentation = t.focusedClustering.segmentation;
      session.shown = t.focusedClustering.id;
      if (t.focusedLabels) {
        session.labels ??= { choice: '', params: null, editing: null, shown: null, level: 0, share: 0.5, open: false };
        session.labels.shown = t.focusedLabels.id;
      }
    } else if (t.focusedSegmentation && segs.some((s) => s.id === t.focusedSegmentation)) session.segmentation = t.focusedSegmentation;
    if (!segs.some((s) => s.id === session.segmentation)) session.segmentation = segs[0]?.id ?? '';
    const seg = segs.find((s) => s.id === session.segmentation);
    const project = app.store.project;
    const kinds = new Set(['clustering', 'embedding', 'cluster-names', 'filter', 'labels']);
    const next = JSON.stringify([t.dataset.id, Boolean(t.item), segs.map((s) => [s.id, s.isDefault]), project.artifacts.filter((a) => (kinds.has(a.kind) && a.dataset === t.dataset.id) || a.kind === 'marker-set').map((a) => [a.id, a.state]), session.segmentation, session.cells, session.shown, session.cluster, session.compare, app.bundles.clusterings(t.dataset.id), app.bundles.mappings(t.dataset.id), app.bundles.registeringAnalysis(t.dataset.id), app.bundles.labelings(t.dataset.id), app.bundles.labelImports(t.dataset.id), Boolean(analyses.get(t.item?.path)?.clusterings)]);
    if (!force && next === shape) {
      updateLive();
      return;
    }
    shape = next;
    generation += 1;
    title.textContent = ` · ${t.dataset.name}`;
    live.clear();
    if (!segs.length) {
      clear(body).append(h('div.planned', h('div.empty', icon('tag'), h('h3', 'No segmentation yet'),
        h('p', `${t.dataset.name}'s segmentation is registered when its derived data are built; its cells can then be clustered here.`),
        h('div.btn-row', { style: { marginTop: '12px' } }, h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')))));
      return;
    }
    // Keep what is being typed (a cluster's name) when the view redraws.
    const focused = document.activeElement && body.contains(document.activeElement) ? document.activeElement : null;
    const typingName = focused?.dataset?.name !== undefined ? { k: focused.dataset.name, value: focused.value } : null;
    if (selection.clustering && selection.clustering !== session.shown) selection = { mask: null, count: 0, markers: null, clustering: null };
    clear(body).append(
      h('p.muted.seg-intro', 'Cell identities start from the cells\' expression. Each clustering is kept as an artifact of its segmentation with every parameter, so it can be made again, on another segmentation too, and compared; labels from marker genes are kept the same way, with the marker set they came from. Imported labels and a reference come next.'),
      formCard(t, segs, session),
      ...[listCard(t, seg, session), mapCard(t, seg, session), detailCard(t, session)].filter(Boolean),
      ...labelsSections(app, { t, seg, session, clustering: app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === session.shown && a.state === 'complete'), cellMap, render }),
      ...[instrumentCard(t, seg), compareCard(t, seg, session)].filter(Boolean));
    if (typingName) {
      const input = body.querySelector(`[data-name="${typingName.k}"]`);
      if (input) {
        input.value = typingName.value;
        input.focus();
      }
    }
    if (session.shown) ensureMap(t, session.shown);
    updateLive();
  }

  render();
  // The remote actions' way in (ui/remote.js).
  app.cellTypesView = {
    cluster: async (dataset, request) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      if (!now.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const session = sessionOf(dataset);
      const d = await clusteringDefaults();
      session.params = { ...d.parameters, ...(request.parameters ?? {}) };
      session.segmentation = request.segmentation;
      session.cells = request.filterId ?? 'rule';
      render(true);
      const full = { segmentation: request.segmentation, parameters: session.params };
      if (request.filterId) {
        const filter = app.store.project.artifacts.find((a) => a.kind === 'filter' && a.id === request.filterId);
        if (!filter) throw new Error(`No filter ${request.filterId} in the project.`);
        full.filter = await filterCells(now, filter);
      }
      const manifest = await run(now, session, full);
      document.querySelector(`[data-clustering="${CSS.escape(manifest.id)}"]`)?.scrollIntoView({ block: 'center' });
      return manifest;
    },
    // Shows a clustering, with its map made if it has none, and answers when both are drawn.
    show: async (dataset, clusteringId) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      const c = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === clusteringId);
      if (!c) throw new Error(`No clustering ${clusteringId}.`);
      const session = sessionOf(dataset);
      session.segmentation = c.segmentation;
      session.shown = c.id;
      render(true);
      await ensureMap(now, c.id);
      render(true);
      for (let k = 0; k < 600 && !cellMap?.ready(); k += 1) await new Promise((r) => setTimeout(r, 50));
      await cellMap?.settled();
      return cellMap;
    },
    // Labels a clustering's cells from a marker set (labels-card.js labelClustering) and shows them.
    label: async (dataset, clusteringId, request) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      if (!now.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const c = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === clusteringId);
      if (!c) throw new Error(`No clustering ${clusteringId}.`);
      const session = sessionOf(dataset);
      session.segmentation = c.segmentation;
      session.shown = c.id;
      render(true);
      const manifest = await labelClustering(app, { t: now, session, clustering: c }, request);
      render(true);
      document.querySelector(`[data-labels="${CSS.escape(manifest.id)}"]`)?.scrollIntoView({ block: 'center' });
      return manifest;
    },
    map: () => cellMap,
    selection: () => selection,
    registerInstrument: async (dataset, name) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      if (!now.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const m = await app.bundles.registerInstrumentClusters(now.item, dataset.id, name);
      analyses.delete(now.item.path);
      render(true);
      return m;
    },
  };
  return {
    update(topics) {
      if (topics.has('project') || topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('mode')) render();
      else if (topics.has('jobs')) updateLive();
    },
    destroy() {
      if (app.cellTypesView) delete app.cellTypesView;
      cellMap?.destroy();
      cellMap = null;
      root.remove();
    },
  };
}
