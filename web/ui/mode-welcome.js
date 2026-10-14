// The start page: open a bundle, or a saved project. Adapted from MaveScape 0.8.0
// web/ui/mode-welcome.js.

import { h, icon, clear, relativeTime, formatBytes } from './dom.js';
import { prefs } from './storage.js';

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

  loadRecent();
  return {
    update(topics) {
      if (topics.has('library')) loadRecent();
    },
    destroy() {
      root.remove();
    },
  };
}
