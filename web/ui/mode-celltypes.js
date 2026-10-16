// The Cell types view: clusters of a segmentation's cells by their gene expression (internal/
// cluster, api_clustering.go), each clustering an artifact with every parameter recorded; its
// clusters with their sizes and leading markers, the components' variances, and the clusters drawn
// on the tissue in the Viewer. Labels from markers, imported labels and a reference come in later
// slices of wave 4 (situcraft-spec/roadmap.md).

import { h, icon, clear, formatCount, relativeTime } from './dom.js';
import { toast } from './overlays.js';
import { categoricalColor } from '../lib/colormaps.js';
import { promotedSegmentation, segmentationsOf } from '../lib/segmentations.js';
import { evaluate, keepChecksum } from '../lib/filters.js';
import { readCells } from '../lib/viewer-data.js';
import { PARAMETERS, checkParameters, clusterName, clusteringsOf, describeClustering, filterRequest } from '../lib/clusterings.js';

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

  function target() {
    const { project, ui } = app.store;
    const focusedClu = ui.focus?.kind === 'clustering' ? project.artifacts.find((a) => a.kind === 'clustering' && a.id === ui.focus.id) : null;
    const seg = ui.focus?.kind === 'segmentation' ? project.artifacts.find((a) => a.kind === 'segmentation' && a.id === ui.focus.id) : null;
    const id = focusedClu?.dataset ?? (ui.focus?.kind === 'dataset' ? ui.focus.id : seg?.dataset) ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), focusedClustering: focusedClu?.dataset === dataset.id ? focusedClu : null, focusedSegmentation: seg?.dataset === dataset.id ? seg.id : null };
  }

  function sessionOf(dataset) {
    if (!sessions.has(dataset.id)) sessions.set(dataset.id, { segmentation: '', cells: 'rule', params: null, shown: null, cluster: 0 });
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

  async function run(t, session, request) {
    const problem = checkParameters(request.parameters);
    if (problem) throw new Error(problem);
    const manifest = await app.bundles.clusterCells(t.item, t.dataset.id, request);
    session.shown = manifest.id;
    session.cluster = 0;
    render(true);
    return manifest;
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
    const running = app.bundles.clusterings(t.dataset.id).map((e) => {
      const job = e.job ? app.jobs.get(e.job) : null;
      const bar = h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } });
      const message = h('span.muted', job?.message ?? 'Starting…');
      if (e.job) live.set(e.job, { bar, message });
      const name = segs.find((s) => s.id === e.segmentation)?.name ?? 'the cells';
      return h('div.seg-running', h('div', h('b', `Clustering ${name}`), ' ', message), h('div.progress', bar));
    });
    return h('section.seg-section',
      h('div.section-title', 'Clusters'),
      h('div.seg-card',
        h('p', 'Cells grouped by their gene expression, as the common single-cell workflow does it: each cell\'s counts scaled to the median total and log-transformed, each gene scaled, principal components, each cell\'s nearest neighbors, then Leiden communities of the neighbor graph. Every parameter is recorded, and the same seed gives the same clusters on any computer.'),
        h('div.seg-import-grid',
          h('label.field', { for: 'ct-segmentation' }, h('span', 'Segmentation'), segPick),
          h('label.field', { for: 'ct-cells' }, h('span', 'Cells analyzed'), cellsPick)),
        h('details.seg-advanced', { open: session.open ?? false, ontoggle: (event) => { session.open = event.currentTarget.open; } },
          h('summary', 'Parameters'),
          h('div.seg-import-grid', ...inputs),
          h('p.muted.seg-note', 'Hover a parameter for what it does. The defaults are scanpy\'s: 50 components, 15 neighbors, resolution 1.')),
        h('div.seg-form', go, !t.item ? h('span.muted', `${t.dataset.name}'s bundle was not found where the project records it: locate it in the Data view.`) : null),
        ...running,
        h('p.muted.seg-cite', 'Methods: normalization and scaling as scanpy (Wolf et al. 2018); exact principal components; UMAP\'s neighbor graph (McInnes et al. 2018); Leiden (Traag et al. 2019); markers by the Wilcoxon rank-sum test (Mann & Whitney 1947), exploratory: cells are not replicates.')));
  }

  // --- The clusterings made ---
  function listCard(t, seg, session) {
    const list = clusteringsOf(app.store.project, t.dataset.id, seg?.id).filter((c) => c.state === 'complete');
    if (!list.some((c) => c.id === session.shown)) session.shown = list.at(-1)?.id ?? null;
    if (!list.length) return null;
    const rows = list.map((c) => {
      const s = c.summary ?? {};
      return h(`tr${session.shown === c.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-clustering': c.id, onclick: () => {
        session.shown = c.id;
        session.cluster = 0;
        render(true);
      }, onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          session.shown = c.id;
          session.cluster = 0;
          render(true);
        }
      } },
      h('td', String(c.parameters?.resolution ?? '—')),
      h('td.r', exact(s.clusters)),
      h('td.r', exact(s.cells)),
      h('td', s.filter ? `filter ${s.filter.name}` : `≥ ${c.parameters?.minTranscripts ?? '?'} transcripts`),
      h('td.r', fixed(s.modularity, 3)),
      h('td.r', String(c.parameters?.seed ?? '—')),
      h('td.muted', relativeTime(c.created)),
      h('td', h('button.icon-button', { type: 'button', title: 'Show the clusters on the tissue', 'aria-label': 'Show the clusters on the tissue', onclick: (event) => {
        event.stopPropagation();
        app.showClusters(c);
      } }, icon('tissue'))));
    });
    return h('section.seg-section',
      h('div.section-title', `Clusterings of ${seg.name}`),
      h('div.table-wrap', h('table.data-table.seg-table',
        h('thead', h('tr', h('th', 'Resolution'), h('th.r', 'Clusters'), h('th.r', 'Cells'), h('th', 'Cells analyzed'), h('th.r', 'Modularity'), h('th.r', 'Seed'), h('th', 'Made'), h('th', ''))),
        h('tbody', ...rows))));
  }

  // --- One clustering: its numbers, clusters and markers ---
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
    const explained = d.pca.varianceRatio.reduce((a, b) => a + b, 0);
    const kv = h('dl.kv.seg-kv',
      h('dt', 'Cells analyzed'), h('dd', `${exact(n)} of ${exact(d.cells.segmentation)}`, d.cells.excluded ? h('span.muted', ` (${exact(d.cells.excluded)} left out${d.cells.filter ? ' by the filter' : ` with fewer than ${d.parameters.minTranscripts} gene transcripts`})`) : null, d.cells.withoutTranscripts ? h('span.muted', `; ${exact(d.cells.withoutTranscripts)} without a gene transcript`) : null),
      h('dt', 'Genes'), h('dd', exact(d.genes.count), d.genes.notDetected ? h('span.muted', ` (${d.genes.notDetected} not detected in these cells)`) : null),
      h('dt', 'Normalization'), h('dd', `to the median total, ${fixed(d.fixed.targetSum, 1)} gene transcripts; log1p; each gene scaled, clipped at ±${d.fixed.clip}`),
      h('dt', 'Components'), h('dd', `${d.pca.components}, holding ${percent(explained)} of the variance`),
      h('dt', 'Graph'), h('dd', `${d.graph.neighbors} neighbors (the cell included), ${exact(d.graph.edges)} edges`),
      h('dt', 'Leiden'), h('dd', `resolution ${d.parameters.resolution}, seed ${d.parameters.seed}: modularity ${fixed(d.clusters.modularity, 4)} after ${d.clusters.iterations} iteration${d.clusters.iterations === 1 ? '' : 's'}`),
      h('dt', 'Artifact'), h('dd.mono', c.id));
    const scree = screePlot(d.pca.varianceRatio);
    const selected = Math.min(session.cluster, sizes.length - 1);
    const rows = sizes.map((size, k) => {
      const top = (d.top?.[k] ?? []).slice(0, 5).map((m) => m.gene);
      return h(`tr${k === selected ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-cluster': k, onclick: () => {
        session.cluster = k;
        render(true);
      }, onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          session.cluster = k;
          render(true);
        }
      } },
      h('td', h('span.ct-swatch', { style: { background: categoricalColor(k) }, 'aria-hidden': 'true' }), clusterName(k)),
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
    return [
      h('div.ct-summary', kv, scree),
      h('div.btn-row', h('button.btn.small.primary', { type: 'button', onclick: () => app.showClusters(c) }, icon('tissue'), 'Show on the tissue')),
      h('div.table-wrap', h('table.data-table.seg-table.ct-clusters',
        h('thead', h('tr', h('th', 'Cluster'), h('th.r', 'Cells'), h('th.r', 'Share'), h('th', 'Leading markers'))),
        h('tbody', ...rows))),
      h('div.section-title', { style: { marginTop: '14px' } }, `${clusterName(selected)}: markers against the other cells`),
      h('div.table-wrap', h('table.data-table.seg-table.ct-markers',
        h('thead', h('tr', h('th', 'Gene'), h('th.r', { title: 'log₂ of the ratio of the mean expression in the cluster to the rest' }, 'log₂ fold change'), h('th.r', 'Cells expressing in'), h('th.r', 'out'), h('th.r', { title: 'The Wilcoxon rank-sum test\'s z-score' }, 'Score'), h('th.r', { title: 'Benjamini–Hochberg adjusted over the genes' }, 'Adjusted p'))),
        h('tbody', ...markerRows))),
      h('p.muted.seg-note', 'Exploratory: each cell counts as an observation, but cells of one tissue are not independent replicates, so the p-values rank genes rather than test them. Markers are the genes most enriched in the cluster against all other cells analyzed.'),
      h('p.muted.seg-cite', describeClustering(c)),
    ];
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
    } else if (t.focusedSegmentation && segs.some((s) => s.id === t.focusedSegmentation)) session.segmentation = t.focusedSegmentation;
    if (!segs.some((s) => s.id === session.segmentation)) session.segmentation = segs[0]?.id ?? '';
    const seg = segs.find((s) => s.id === session.segmentation);
    const list = clusteringsOf(app.store.project, t.dataset.id);
    const next = JSON.stringify([t.dataset.id, Boolean(t.item), segs.map((s) => [s.id, s.isDefault]), list.map((c) => [c.id, c.state]), session.segmentation, session.cells, session.shown, session.cluster, app.bundles.clusterings(t.dataset.id), filtersOf(t).map((f) => f.id)]);
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
    clear(body).append(
      h('p.muted.seg-intro', 'Cell identities start from the cells\' expression. Each clustering is kept as an artifact of its segmentation with every parameter, so it can be made again, on another segmentation too, and compared. Labels from markers, imported labels and a reference come next.'),
      formCard(t, segs, session),
      ...[listCard(t, seg, session), detailCard(t, session)].filter(Boolean));
    updateLive();
  }

  render();
  // The remote action's way in (ui/remote.js).
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
  };
  return {
    update(topics) {
      if (topics.has('project') || topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('mode')) render();
      else if (topics.has('jobs')) updateLive();
    },
    destroy() {
      if (app.cellTypesView) delete app.cellTypesView;
      root.remove();
    },
  };
}
