// The project tree: what the project holds, by kind (datasets, segmentations, label sets, saved
// views), and what was handed to SituCraft on this computer but is not in the project yet.
// Selecting an item focuses it (store.ui.focus): the inspector describes it, and the views that
// use it open on it. Adapted from MaveScape 0.8.0 web/ui/sidebar.js.

import { h, icon, clear, formatBytes, formatCount } from './dom.js';
import { promotedSegmentation } from '../lib/segmentations.js';

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
    kind: 'labels', title: 'Label sets', icon: 'tag',
    items: (project) => project.artifacts.filter((a) => a.kind === 'labels'),
    label: (a) => a.name ?? 'Label set',
    meta: (a) => a.method ?? '',
    empty: 'Cell identities: predicted, curated and consensus, side by side.',
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
