// The project tree: what the project holds, by kind (datasets, samples, designs, segmentations, comparisons,
// clusterings, label sets, marker sets, graphs, spatial analyses, filters, saved views), and what was handed to SituCraft on
// this computer but is not in the project yet.
// Selecting an item focuses it (store.ui.focus): the inspector describes it, and the views that
// use it open on it. Adapted from MaveScape 0.8.0 web/ui/sidebar.js.

import { h, icon, clear, formatBytes, formatCount } from './dom.js';
import { describeGraph } from '../lib/graphs.js';
import { promotedSegmentation } from '../lib/segmentations.js';
import { transformInUse } from '../lib/transforms.js';
import { labelSetName, markerSetsOf } from '../lib/labels.js';
import { samplesOf } from '../lib/samples.js';
import { designsOf } from '../lib/design.js';
import { summariesOf } from '../lib/cohort.js';
import { comparisonsOf } from '../lib/abundance.js';
import { testsOf } from '../lib/expression.js';

// The icon of an item handed to the program, by its kind (local.go, fileKind and dirKind).
export const KIND_ICONS = {
  bundle: 'bundle', archive: 'bundle', zarr: 'zarr', image: 'image', geometry: 'polygon',
  table: 'table', matrix: 'grid', project: 'library',
};

export const KIND_LABELS = {
  bundle: 'Xenium output bundle', archive: 'Archive', zarr: 'Zarr store', image: 'Image',
  geometry: 'Polygons (GeoJSON)', table: 'Table', matrix: 'Matrix', project: 'Project archive',
};

const SECTIONS = [
  {
    kind: 'local', title: 'On this computer', icon: 'folder',
    // Items not in the project yet.
    items: (project, app) => app.localItems.filter((item) => !project.datasets.some((d) => d.path === item.path)),
    label: (item) => item.name,
    itemIcon: (item) => KIND_ICONS[item.kind] ?? 'file',
    meta: (item) => (item.dir ? KIND_LABELS[item.kind] ?? item.kind : formatBytes(item.size)),
    hideWhenEmpty: true,
  },
  {
    kind: 'dataset', title: 'Datasets', icon: 'bundle',
    items: (project) => project.datasets,
    label: (d) => d.name ?? 'Dataset',
    meta: (d, app) => {
      const state = app.bundles.link(d.id)?.state;
      if (state === 'missing') return 'not found';
      if (state === 'changed' || state === 'different-run') return 'changed';
      return Number.isFinite(d.cells) ? `${formatCount(d.cells)} cells` : d.platform ?? '';
    },
    empty: 'Xenium output bundles you open appear here, each checked before it is used.',
    action: { label: 'Open a bundle', run: (app) => app.openPathDialog() },
  },
  {
    kind: 'sample', title: 'Samples', icon: 'samples',
    // A project of several samples (wave 7): each dataset, or its regions, with its condition.
    items: (project) => {
      const samples = samplesOf(project);
      return samples.length > 1 ? samples : [];
    },
    label: (s) => s.name,
    itemIcon: (s) => (s.removed ? 'warning' : 'samples'),
    meta: (s) => (s.removed ? 'region removed' : [s.fields.condition, s.fields.subject].filter((v) => v !== undefined && v !== '').join(' · ') || (s.region ? 'region' : '')),
    hideWhenEmpty: true,
  },
  {
    kind: 'design', title: 'Designs', icon: 'samples',
    // The cohort's designs (wave 7): each name's latest version.
    items: (project) => designsOf(project),
    label: (d) => `${d.name} · v${d.version}`,
    meta: (d) => `${d.result.rows.length} rows, ${d.result.df} df`,
    hideWhenEmpty: true,
  },
  {
    kind: 'cohort-summary', title: 'Summaries', icon: 'table',
    // The cohort's summaries (wave 7): the samples' numbers, the latest first.
    items: (project) => summariesOf(project),
    label: (r) => r.summary?.labels?.name ?? r.name,
    meta: (r) => {
      const n = r.summary?.samples?.length ?? r.samples?.length ?? 0;
      return `${n} sample${n === 1 ? '' : 's'}${r.summary?.niches ? `, ${r.summary.niches} niches` : ''}`;
    },
    hideWhenEmpty: true,
  },
  {
    kind: 'abundance', title: 'Abundance compared', icon: 'compare',
    // The cohort's comparisons of abundance (wave 7), the latest first.
    items: (project) => comparisonsOf(project),
    label: (r) => r.name.replace(/^Abundance compared: /, ''),
    meta: (r) => {
      const n = r.summaryOf?.found?.length ?? 0;
      return `${r.summaryOf?.tests ?? '?'} tests, ${n} found`;
    },
    hideWhenEmpty: true,
  },
  {
    kind: 'expression', title: 'Expression compared', icon: 'compare',
    // The cohort's comparisons of expression (wave 7), the latest first.
    items: (project) => testsOf(project, 'expression'),
    label: (r) => r.name.replace(/^Expression compared: /, ''),
    meta: (r) => `${r.summaryOf?.tests ?? '?'} tests, ${r.summaryOf?.below ?? 0} found`,
    hideWhenEmpty: true,
  },
  {
    kind: 'spatial-comparison', title: 'Spatial statistics compared', icon: 'compare',
    // The cohort's comparisons of spatial statistics (wave 7), the latest first.
    items: (project) => testsOf(project, 'spatial-comparison'),
    label: (r) => r.name.replace(/^Spatial statistics compared: /, ''),
    meta: (r) => `${r.summaryOf?.tests ?? '?'} tests, ${r.summaryOf?.found?.length ?? 0} found`,
    hideWhenEmpty: true,
  },
  {
    kind: 'segmentation', title: 'Segmentations', icon: 'segment',
    items: (project) => project.artifacts.filter((a) => a.kind === 'segmentation'),
    // With more than one dataset, each segmentation names its dataset.
    label: (a, app) => {
      const { datasets } = app.store.project;
      const dataset = datasets.find((d) => d.id === a.dataset);
      return datasets.length > 1 && dataset ? `${a.name} · ${dataset.name}` : a.name ?? 'Segmentation';
    },
    itemIcon: (a) => (a.state === 'complete' ? 'segment' : 'warning'),
    meta: (a, app) => {
      if (a.state !== 'complete') return a.state;
      const promoted = promotedSegmentation(app.store.project, a.dataset)?.record.id === a.id;
      const cells = a.summary?.cells;
      return [promoted ? 'default' : '', Number.isFinite(cells) ? `${formatCount(cells)} cells` : ''].filter(Boolean).join(' · ');
    },
    empty: 'The instrument\'s segmentation, SituCraft\'s own, and ones imported from other tools, each kept with how it was made.',
  },
  {
    kind: 'image', title: 'Images', icon: 'image',
    items: (project) => project.artifacts.filter((a) => a.kind === 'image'),
    label: (a) => a.name ?? 'Image',
    itemIcon: (a) => (a.state === 'complete' ? 'image' : 'warning'),
    meta: (a, app) => {
      if (a.state !== 'complete') return a.state;
      const what = a.summary?.kind === 'rgb' ? 'RGB' : `${(a.summary?.channels ?? []).length} channels`;
      return transformInUse(app.store.project, a.id) ? what : `${what} · not aligned`;
    },
    empty: 'Post-Xenium H&E and immunofluorescence images, read where they lie and placed by an alignment (the Viewer adds them; the Alignment view aligns them by landmarks).',
  },
  {
    kind: 'comparison', title: 'Comparisons', icon: 'compare',
    items: (project) => project.artifacts.filter((a) => a.kind === 'comparison'),
    label: (a, app) => {
      const segs = app.store.project.artifacts;
      const name = (id) => segs.find((x) => x.kind === 'segmentation' && x.id === id)?.name ?? '?';
      return `${name(a.a)} · ${name(a.b)}`;
    },
    meta: (a) => (Number.isFinite(a.summary?.pixelAgreement) ? `${(100 * a.summary.pixelAgreement).toFixed(1)}% agree` : a.state),
    empty: 'Two segmentations compared: which cells correspond, how each differs, and what changed.',
  },
  {
    kind: 'clustering', title: 'Clusterings', icon: 'dots',
    items: (project) => project.artifacts.filter((a) => a.kind === 'clustering'),
    label: (a, app) => {
      const seg = app.store.project.artifacts.find((x) => x.kind === 'segmentation' && x.id === a.segmentation);
      return `${seg?.name ?? 'Cells'} · resolution ${a.parameters?.resolution ?? '?'}`;
    },
    itemIcon: (a) => (a.state === 'complete' ? 'dots' : 'warning'),
    meta: (a) => (a.state === 'complete' ? `${a.summary?.clusters ?? '?'} clusters` : a.state),
    empty: 'Cells grouped by their expression, each clustering kept with every parameter.',
  },
  {
    kind: 'labels', title: 'Label sets', icon: 'tag',
    items: (project) => project.artifacts.filter((a) => a.kind === 'labels' || a.kind === 'curated-labels' || a.kind === 'consensus-labels'),
    label: (a, app) => {
      if (a.kind !== 'labels') return a.name;
      if (a.source || a.reference) return labelSetName(a);
      const c = app.store.project.artifacts.find((x) => x.kind === 'clustering' && x.id === a.clustering);
      return `${a.markers?.name ?? 'Labels'} · ${c?.summary?.source ? 'instrument' : `resolution ${c?.parameters?.resolution ?? '?'}`}`;
    },
    itemIcon: (a) => (a.kind === 'curated-labels' ? 'edit' : a.kind === 'consensus-labels' ? 'layers' : a.state === 'complete' ? 'tag' : 'warning'),
    meta: (a) => (a.kind === 'curated-labels' ? `${a.edits.length} edit${a.edits.length === 1 ? '' : 's'}` : a.kind === 'consensus-labels' ? a.rule.kind : a.state === 'complete' && a.summary?.cells ? `${Math.round((100 * (a.summary.labeled?.[0] ?? 0)) / a.summary.cells)}% labeled` : a.state),
    empty: 'Cells labeled from marker genes or a reference, or imported from a table, each label set kept with what it came from.',
  },
  {
    kind: 'marker-set', title: 'Marker sets', icon: 'dna',
    items: (project) => markerSetsOf(project),
    label: (a) => `${a.name} · v${a.version}`,
    meta: (a) => `${a.set.labels.length} labels`,
    empty: 'Labels with their positive and negative markers, each version kept.',
  },
  {
    kind: 'graph', title: 'Graphs', icon: 'graph',
    items: (project) => project.artifacts.filter((a) => a.kind === 'graph'),
    label: (a, app) => {
      const seg = app.store.project.artifacts.find((x) => x.kind === 'segmentation' && x.id === a.segmentation);
      return `${seg?.name ?? 'Cells'} · ${describeGraph(a.parameters)}`;
    },
    itemIcon: (a) => (a.state === 'complete' ? 'graph' : 'warning'),
    meta: (a) => (a.state === 'complete' ? `${formatCount(a.summary?.edges ?? 0)} edges` : a.state),
    empty: 'Which cells are neighbors: each graph of a segmentation\'s cells kept with its definition and the cells it was built over.',
  },
  {
    kind: 'spatial', title: 'Spatial analyses', icon: 'graph',
    items: (project) => project.artifacts.filter((a) => a.kind === 'spatial'),
    label: (a) => `${{ enrichment: 'Enrichment', cooccurrence: 'Co-occurrence', distances: 'Nearest distances', ripley: 'Ripley\'s K', regions: 'Regions compared', boundary: 'Across an edge', niches: 'Niches', moran: 'Moran\'s I' }[a.question] ?? a.question} · ${a.question === 'moran' ? 'the genes' : a.summary?.labels?.name ?? 'labels'}`,
    itemIcon: (a) => (a.state === 'complete' ? 'graph' : 'warning'),
    meta: (a) => (a.state !== 'complete' ? a.state : a.question === 'cooccurrence' ? `${a.summary?.distances ?? '?'} distances` : a.question === 'regions' ? `${a.summary?.significant ?? 0} differ` : a.question === 'boundary' ? `${a.summary?.significant ?? 0} labels` : a.question === 'niches' ? `${a.summary?.niches?.length ?? 0} niches` : a.question === 'moran' ? `${a.summary?.significant ?? 0} genes` : `${a.summary?.significant ?? 0} pairs`),
    empty: 'Questions asked of a label set\'s cells (which types are neighbors, at what distances, in which regions), each answer kept with its inputs.',
  },
  {
    kind: 'filter', title: 'Filters', icon: 'filter',
    items: (project) => project.artifacts.filter((a) => a.kind === 'filter'),
    label: (a) => `${a.name} · v${a.version}`,
    meta: (a) => `${formatCount(a.result.kept)} cells`,
    empty: 'Cell filters from the QC view, each version kept with the cells it keeps.',
  },
  {
    kind: 'view', title: 'Saved views', icon: 'eye',
    items: (project) => project.views,
    label: (v) => v.name ?? 'View',
    meta: (v) => (v.mode === 'qc' ? 'QC' : 'Viewer'),
    empty: 'A place in the tissue with its layers, genes and colors, reopened exactly.',
  },
];

export function mountSidebar(app) {
  const root = document.getElementById('sidebar');

  function render() {
    const { project, ui } = app.store;
    clear(root);
    const body = h('div.panel-body', { style: { paddingTop: '4px' } });
    for (const section of SECTIONS) {
      const items = section.items(project, app);
      if (!items.length && section.hideWhenEmpty) continue;
      // A list of its items (each a button in a list item), or what the section will hold.
      const list = items.length ? h('div', { role: 'list' }) : h('div');
      for (const item of items) {
        const id = item.id ?? item.url;
        const selected = ui.focus?.kind === section.kind && ui.focus?.id === id;
        list.append(h('div', { role: 'listitem' }, h(`button.dataset-row${selected ? '.selected' : ''}`, {
          type: 'button',
          'aria-current': selected ? 'true' : null,
          title: section.label(item, app),
          onclick: () => {
            app.focusItem({ kind: section.kind, id });
            // Bundles and datasets are shown by the Data view; from the start page, go there.
            const bundle = section.kind === 'dataset' || item.kind === 'bundle' || item.kind === 'archive';
            if (bundle && app.store.ui.mode === 'welcome') app.setMode('data');
            // A filter is edited and compared in the QC view; a saved view opens.
            if (section.kind === 'filter') app.setMode('qc');
            if (section.kind === 'clustering') app.setMode('celltypes');
            if (section.kind === 'graph' || section.kind === 'spatial') app.setMode('spatial');
            if (['sample', 'design', 'cohort-summary', 'abundance', 'expression', 'spatial-comparison'].includes(section.kind)) app.setMode('cohort');
            // An image is shown on its dataset's tissue; one not aligned yet, in the Alignment view.
            if (section.kind === 'image') {
              if (transformInUse(app.store.project, item.id)) {
                app.focusItem({ kind: 'dataset', id: item.dataset });
                app.setMode('viewer');
              } else app.showAlignment(item.dataset, item.id);
            }
            if (section.kind === 'view') app.applyView(item);
          },
        }, icon(section.itemIcon?.(item) ?? section.icon), h('span.label', section.label(item, app)), h('span.meta', section.meta(item, app)))));
      }
      if (!items.length) {
        list.append(h('p.tree-empty', section.empty));
        if (section.action) list.append(h('button.drop-hint', { type: 'button', style: { width: 'calc(100% - 12px)' }, onclick: () => section.action.run(app) }, section.action.label));
      }
      body.append(h('section.tree-section', { 'aria-label': section.title },
        h('div.panel-head', h('h2', section.title, items.length ? h('span.muted', `· ${items.length}`) : null)),
        list));
    }
    root.append(h('div.panel', { style: { flex: '1' } }, body));
  }

  return {
    render,
    update(topics) {
      if (topics.has('project') || topics.has('focus') || topics.has('project-loaded') || topics.has('local') || topics.has('bundles')) render();
    },
  };
}
