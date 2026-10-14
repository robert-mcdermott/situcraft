// The inspector: what is in focus (a dataset, a segmentation, a cell), described in full. Views add
// their own sections through app.inspector.setSection(id, builder) as they are built; until
// something is in focus it describes the project and says how to begin. Adapted from MaveScape
// 0.8.0 web/ui/inspector.js.

import { h, icon, relativeTime, formatBytes } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { removeView, renameView, viewLink } from '../lib/views.js';
import { KIND_ICONS, KIND_LABELS } from './sidebar.js';

export function mountInspector(app) {
  const root = document.getElementById('inspector');
  // Sections contributed by views: id → (app) => Node | null.
  const contributed = new Map();

  function projectSection() {
    const { project } = app.store;
    const segmentations = project.artifacts.filter((a) => a.kind === 'segmentation').length;
    const kv = h('dl.kv',
      h('dt', 'Name'), h('dd', project.name),
      h('dt', 'Datasets'), h('dd', String(project.datasets.length)),
      h('dt', 'Segmentations'), h('dd', String(segmentations)),
      h('dt', 'Saved views'), h('dd', String(project.views.length)),
      h('dt', 'Modified'), h('dd', relativeTime(project.modified)));
    return h('section.inspector-section', h('h3', icon('library'), 'Project'), kv);
  }

  // An item handed to the program on this computer: what SituCraft takes it to be.
  function localSection(item) {
    return h('section.inspector-section',
      h('h3', icon(KIND_ICONS[item.kind] ?? 'file'), item.name),
      h('dl.kv',
        h('dt', 'Kind'), h('dd', KIND_LABELS[item.kind] ?? item.kind),
        item.folder ? [h('dt', 'Found in'), h('dd', item.folder)] : null,
        item.dir ? null : [h('dt', 'Size'), h('dd', formatBytes(item.size))]),
      h('p.muted', { style: { margin: '8px 0 0', fontSize: '12px' } }, item.dir
        ? 'Read where it is: SituCraft never copies, changes or moves a bundle.'
        : 'Read on this computer; nothing is uploaded.'));
  }

  // A dataset of the project: what it is, where, and how it validated.
  function datasetSection(d) {
    const v = d.validation ?? {};
    const state = app.bundles.link(d.id)?.state;
    const where = { ok: 'found where recorded', checking: 'looking for it', missing: 'not found where recorded', changed: 'changed since added', 'different-run': 'another run is there' }[state] ?? '';
    const item = app.bundles.link(d.id)?.item;
    const job = item ? app.bundles.cacheJob(item) : null;
    const cached = item ? app.bundles.cache(item.path)?.status : null;
    let derived = '—';
    if (job) derived = job.state === 'queued' ? 'waiting to build' : `building, ${Math.round(job.progress * 100)}%`;
    else if (cached?.ready) derived = `ready (${formatBytes(cached.entries.reduce((n, e) => n + (e.size ?? 0), 0))})`;
    else if (cached) {
      const buildable = cached.entries.filter((e) => e.state !== 'unavailable');
      const built = buildable.filter((e) => e.state === 'ready').length;
      derived = built ? `${built} of ${buildable.length} built` : 'not built';
    }
    return h('section.inspector-section',
      h('h3', icon('bundle'), d.name),
      h('dl.kv',
        h('dt', 'Platform'), h('dd', `${d.platform} (XOA ${d.version})`),
        h('dt', 'Panel'), h('dd', d.panel || '—'),
        h('dt', 'Tissue'), h('dd', [d.organism, d.tissue].filter(Boolean).join(' ') || '—'),
        h('dt', 'Cells'), h('dd', Number(d.cells).toLocaleString('en-US')),
        h('dt', 'Transcripts'), h('dd', Number(d.transcripts).toLocaleString('en-US')),
        h('dt', 'Channels'), h('dd', String(d.channels?.length ?? 0)),
        h('dt', 'Checked'), h('dd', `${v.fatal ?? 0} fatal, ${v.warnings ?? 0} warnings, ${v.info ?? 0} notes`),
        h('dt', 'Bundle'), h('dd', where),
        h('dt', 'Derived data'), h('dd', derived)),
      h('p.muted.mono', { style: { margin: '8px 0 0', fontSize: '11px', wordBreak: 'break-all' } }, d.path));
  }

  // A filter version: its conditions and the cells it keeps.
  function filterSection(f) {
    const dataset = app.store.project.datasets.find((d) => d.id === f.dataset);
    return h('section.inspector-section',
      h('h3', icon('filter'), `${f.name} · version ${f.version}`),
      h('dl.kv',
        h('dt', 'Dataset'), h('dd', dataset?.name ?? '—'),
        h('dt', 'Keeps'), h('dd', `${f.result.kept.toLocaleString('en-US')} of ${f.result.cells.toLocaleString('en-US')} cells`),
        h('dt', 'Conditions'), h('dd', String(f.conditions.length)),
        h('dt', 'Saved'), h('dd', relativeTime(f.created))),
      h('p.muted.mono', { style: { margin: '8px 0 0', fontSize: '10.5px', wordBreak: 'break-all' } }, `SHA-256 of the cells kept: ${f.result.sha256}`));
  }

  // A saved view: what it shows, its link, and what can be done with it.
  function viewSection(v) {
    const dataset = app.store.project.datasets.find((d) => d.id === v.dataset);
    const link = viewLink(location.href, app.store.project.id, v.id);
    return h('section.inspector-section',
      h('h3', icon('eye'), v.name),
      h('dl.kv',
        h('dt', 'Dataset'), h('dd', dataset?.name ?? '—'),
        h('dt', 'In'), h('dd', v.mode === 'qc' ? 'QC' : 'Viewer'),
        h('dt', 'Zoom'), h('dd', `${v.camera.umPerCss.toPrecision(3)} µm per pixel`),
        h('dt', 'Genes'), h('dd', v.transcripts?.all ? 'all transcripts' : v.transcripts?.genes?.join(', ') || '—'),
        v.qc ? [h('dt', 'Map'), h('dd', v.qc.map)] : null,
        h('dt', 'Saved'), h('dd', relativeTime(v.updated ?? v.created))),
      h('div.btn-row', { style: { marginTop: '10px' } },
        h('button.btn.small.primary', { type: 'button', onclick: () => app.applyView(v) }, icon('eye'), 'Open'),
        h('button.btn.small', { type: 'button', title: link, onclick: async () => {
          try {
            await navigator.clipboard.writeText(link);
            toast('Link copied: it opens this view while SituCraft runs on this computer.', { kind: 'ok' });
          } catch {
            toast(link);
          }
        } }, icon('link'), 'Copy link'),
        h('button.btn.small.ghost', { type: 'button', onclick: async () => {
          const name = await promptDialog({ title: 'Rename view', label: 'Name', value: v.name });
          if (name) app.store.commit(renameView(app.store.project, v.id, name), 'Rename view');
        } }, icon('edit'), 'Rename'),
        h('button.btn.small.ghost.danger', { type: 'button', onclick: () => {
          app.store.commit(removeView(app.store.project, v.id), `Remove view ${v.name}`);
          app.focusItem(null);
        } }, icon('trash'), 'Remove')));
  }

  function focusSection() {
    const focus = app.store.ui.focus;
    if (focus?.kind === 'view') {
      const view = app.store.project.views.find((v) => v.id === focus.id);
      if (view) return viewSection(view);
    }
    if (focus?.kind === 'filter') {
      const filter = app.store.project.artifacts.find((a) => a.id === focus.id);
      if (filter) return filterSection(filter);
    }
    if (focus?.kind === 'dataset') {
      const dataset = app.store.project.datasets.find((d) => d.id === focus.id);
      if (dataset) return datasetSection(dataset);
    }
    if (focus?.kind === 'local') {
      const item = app.localItems.find((f) => f.url === focus.id);
      if (item) return localSection(item);
    }
    if (!focus) {
      return h('section.inspector-section',
        h('h3', icon('info'), 'Nothing selected'),
        h('p.muted', { style: { margin: 0, fontSize: '12.5px' } }, 'Select an item in the project tree, or a cell in the tissue, to see everything known about it here: identifiers, counts, geometry, labels, and where each number came from.'));
    }
    return null;
  }

  function render() {
    root.replaceChildren();
    root.append(focusSection());
    for (const build of contributed.values()) {
      try {
        root.append(build(app));
      } catch (error) {
        root.append(h('section.inspector-section', h('p.muted', `This section could not be shown: ${error.message}`)));
      }
    }
    root.append(projectSection());
    root.append(h('section.inspector-section', h('p.muted', { style: { margin: 0, fontSize: '11.5px' } },
      'SituCraft is for research use only. It is not a diagnostic device.')));
  }

  return {
    render,
    setSection(id, build) {
      if (build) contributed.set(id, build);
      else contributed.delete(id);
      render();
    },
    update(topics) {
      if (topics.has('project') || topics.has('focus') || topics.has('selection') || topics.has('project-loaded') || topics.has('local') || topics.has('bundles') || topics.has('jobs')) render();
    },
  };
}
