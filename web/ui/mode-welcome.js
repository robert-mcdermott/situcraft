// The start page: open a bundle, or a saved project. Adapted from MaveScape 0.8.0
// web/ui/mode-welcome.js.

import { h, icon, clear, relativeTime, formatBytes } from './dom.js';
import { prefs } from './storage.js';
import { tourFor, workedExamples } from '../lib/tours.js';

// What a researcher brings (situcraft-spec/requirements.md, D1, D6, S10, R1).
const INPUTS = [
  ['bundle', 'A Xenium output bundle', 'The folder (or zip) the instrument or Xenium Ranger wrote: transcripts, cells, polygons, the cell-by-gene matrix and morphology images. SituCraft reads it where it is and never changes it.'],
  ['image', 'Post-Xenium H&E or IF', 'OME-TIFF images of the same section, registered to the transcripts so that pathology and molecules share one view.'],
  ['segment', 'Segmentations from other tools', 'Cell boundaries and transcript assignments from other methods (SpatialData Zarr, GeoJSON with an assignment table), compared with the instrument\'s and SituCraft\'s own.'],
  ['tag', 'Labels and annotations', 'Cell types, regions of interest and pathology annotations as tables or GeoJSON.'],
];

export function mountWelcome(app, container) {
  const { library } = app;
  const recent = h('div.welcome-grid');
  const examples = h('div.welcome-grid.examples');
  const root = h('div.workbench-scroll', h('div.welcome',
    h('div.welcome-hero',
      h('div', { style: { flex: 1 } },
        h('h1', 'Spatial transcriptomics, from bundle ', h('span.brand-text', 'to tissue biology.')),
        h('p', 'SituCraft opens a Xenium output bundle, checks it, and shows the tissue, its transcripts and its cells at any zoom. Segment cells and choose between segmentations with evidence, then type cells, find niches and compare samples, on your own computer, with every step recorded.'),
        h('div.btn-row',
          h('button.btn.primary', { type: 'button', onclick: () => app.openPathDialog() }, icon('bundle'), 'Open a bundle'),
          h('button.btn', { type: 'button', onclick: () => app.openLibrary() }, icon('library'), 'Open a saved project'),
          h('button.btn', { type: 'button', onclick: () => app.pickFiles() }, icon('file'), 'Open files'))),
      h('img', { src: 'favicon.svg', width: 132, height: 132, alt: '', style: { filter: 'drop-shadow(0 18px 40px rgba(31, 138, 158, 0.35))', borderRadius: '28px' } })),
    h('div.section-title', 'Recent projects'),
    recent,
    h('div.section-title', { style: { marginTop: '22px' } }, 'Examples'),
    examples,
    h('div.section-title', { style: { marginTop: '22px' } }, 'What to bring'),
    h('div.feature-list', ...INPUTS.map(([glyph, title, text]) => h('div.feature', h('span.glyph', icon(glyph)), h('div', h('b', title), h('span', text))))),
    h('p.muted', { style: { marginTop: '22px', fontSize: '12px' } },
      'Data are read on this computer and never uploaded. SituCraft is for research use only; it is not a diagnostic device.')));
  container.append(root);

  async function loadRecent() {
    clear(recent);
    let list = [];
    try {
      list = await library.listProjects();
    } catch { /* none */ }
    if (!list.length) {
      recent.append(h('div.card', h('p.muted', `No saved projects yet. Projects save automatically to the library folder (${library.location}).`)));
      return;
    }
    const last = prefs.get('lastProject', null);
    for (const item of list.slice(0, 8)) {
      recent.append(h('div.card.clickable', { role: 'button', tabIndex: 0, onclick: () => app.openProject(item.id), onkeydown: (event) => { if (event.key === 'Enter') app.openProject(item.id); } },
        h('h4', icon('library'), item.name || 'Untitled project', item.id === last ? h('span.badge.accent', { style: { marginLeft: '6px' } }, 'Last opened') : null),
        h('p', `${item.datasets ?? 0} dataset${item.datasets === 1 ? '' : 's'} · ${relativeTime(item.modified)}${item.size ? ` · ${formatBytes(item.size)}` : ''}`)));
    }
  }

  // The example datasets: downloaded only when asked, each with its source and license. A card's
  // progress is updated in place, so that its Stop button is not lost to a redraw.
  const progress = new Map(); // name → { bar, label }
  let shape = '';
  function renderExamples() {
    const list = app.examples.list();
    const error = app.examples.error();
    const next = JSON.stringify([list, error, (list ?? []).map((e) => app.examples.job(e.name)?.id ?? '')]);
    if (next === shape) {
      updateProgress();
      return;
    }
    shape = next;
    clear(examples);
    progress.clear();
    if (error) {
      examples.append(h('div.card', h('p.muted', `The examples could not be listed: ${error}`)));
      return;
    }
    if (!list) {
      examples.append(h('div.card', h('p.muted', 'Listing the examples…')));
      return;
    }
    for (const example of list) {
      const job = app.examples.job(example.name);
      const actions = h('div.btn-row');
      const card = h('div.card.example', { 'data-example': example.name },
        h('h4', icon('bundle'), example.title.replace(/\s*\(.*\)$/, '')),
        h('p', example.summary),
        example.generated
          ? h('p.muted.example-source', `Data: simulated by SituCraft on this computer, nothing downloaded · about ${formatBytes(example.size)} on disk`)
          : h('p.muted.example-source', 'Data: ', example.page ? h('a', { href: example.page, target: '_blank', rel: 'noopener noreferrer' }, example.publisher) : example.publisher, ', ',
            example.licenseUrl ? h('a', { href: example.licenseUrl, target: '_blank', rel: 'noopener noreferrer' }, example.license.replace(/\s*\(.*\)$/, '')) : example.license,
            ` · ${formatBytes(example.download)} to download, ${formatBytes(example.size)} on disk`),
        example.outdated ? h('p.callout.warn', example.outdated) : null,
        actions);
      const worked = workedExamples(example.name);
      if (worked.length) {
        card.insertBefore(h('div.example-worked', h('div.example-worked-title', 'Worked example'), ...worked.map((w) => h('div.example-worked-item',
          h('div', h('b', w.title), w.simulated ? h('span.badge.warn', { style: { marginLeft: '6px' } }, example.generated ? 'simulated tissue' : 'simulated errors') : null),
          h('p.muted', w.question),
          example.state === 'ready' && !job ? h('button.btn.small', { type: 'button', 'data-worked': w.id, disabled: Boolean(w.images && example.outdated), title: w.images && example.outdated ? 'It needs the H&E: make the example again first' : '', onclick: () => app.examples.tour(example.name, w.id) }, icon('flask'), 'Start') : null))), actions);
      }
      if (job) {
        const bar = h('div');
        const label = h('span.muted');
        progress.set(example.name, { bar, label });
        card.insertBefore(h('div.example-progress', h('div.progress', bar), label), actions);
        actions.append(h('button.btn.small', { type: 'button', onclick: () => app.jobs.cancel(job.id).catch(() => {}) }, icon('close'), 'Stop'));
      } else if (example.state === 'ready') {
        if (tourFor(example.name)) actions.append(h('button.btn.small.primary', { type: 'button', onclick: () => app.examples.tour(example.name) }, icon('play'), 'Take the tour'));
        actions.append(
          example.outdated ? h('button.btn.small', { type: 'button', title: `Simulate it again into ${example.path}: the same run, cells and transcripts, with what this SituCraft adds`, onclick: () => app.examples.download(example.name) }, icon('sparkles'), 'Make it again') : null,
          h('button.btn.small', { type: 'button', onclick: () => app.examples.open(example.name) }, icon('bundle'), 'Open'),
          h('button.btn.small', { type: 'button', title: `Delete it from ${example.path}`, onclick: () => app.examples.remove(example.name) }, icon('trash'), 'Remove'));
      } else if (example.generated) {
        actions.append(h('button.btn.small.primary', { type: 'button', title: `Simulate it into ${app.examples.dir()}`, onclick: () => app.examples.download(example.name) }, icon('sparkles'), 'Make it'));
      } else {
        actions.append(h('button.btn.small.primary', { type: 'button', title: `Download into ${app.examples.dir()}, checking every file`, onclick: () => app.examples.download(example.name) }, icon('download'), example.state === 'incomplete' ? 'Download again' : 'Download'));
      }
      examples.append(card);
    }
    updateProgress();
  }
  function updateProgress() {
    for (const [name, { bar, label }] of progress) {
      const job = app.examples.job(name);
      if (!job) continue;
      bar.style.width = `${Math.round(job.progress * 100)}%`;
      label.textContent = job.state === 'queued' ? 'Waiting' : `${job.message ?? 'Downloading'}: ${Math.round(job.progress * 100)}%`;
    }
  }

  loadRecent();
  renderExamples();
  app.examples.refresh();
  return {
    update(topics) {
      if (topics.has('library')) loadRecent();
      if (topics.has('examples') || topics.has('jobs')) renderExamples();
    },
    destroy() {
      root.remove();
    },
  };
}
