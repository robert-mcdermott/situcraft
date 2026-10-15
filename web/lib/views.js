// Saved views: what the Viewer or the QC view shows of a dataset (where, at what zoom, which
// channels in which colors and windows, which genes, which layers, which cell, which QC map and
// filter), kept in the project and reopened exactly. Channels and genes are named, not numbered,
// so a view survives its derived data being rebuilt. The zoom is in µm per CSS pixel, so a view
// shows the same tissue on any screen. A deep link names the project and the view.

function newId() {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  return `v-${random.replace(/-/g, '').slice(0, 16)}`;
}

// A view from what a view shows. state: { camera: { cx, cy, scale }, ratio (device pixels per
// CSS pixel), layers (the renderer's: channels, transcripts, cells), cell: { row, id } or null,
// qc: { map, filter } (the QC view only) }; desc: the dataset's descriptor.
export function captureView({ name, dataset, mode = 'viewer', state, desc, id = newId(), now = new Date().toISOString() }) {
  const { camera, ratio = 1, layers } = state;
  const genes = desc.transcripts?.genes ?? [];
  const view = {
    id,
    name,
    dataset,
    mode,
    created: now,
    // The segmentation the view draws: the instrument's (the Compare view draws any two).
    segmentation: 'instrument',
    // To 12 significant digits: converting back through another pixel ratio (viewCamera) and
    // capturing again would otherwise differ in the last bit.
    camera: { cx: camera.cx, cy: camera.cy, umPerCss: Number((ratio / camera.scale).toPrecision(12)) },
    channels: layers.channels.map((c, i) => ({ name: desc.channels[i]?.name ?? `channel ${i}`, visible: c.visible, color: c.color, window: [c.window[0], c.window[1]], gamma: c.gamma })),
    transcripts: {
      visible: layers.transcripts.visible,
      all: layers.transcripts.all,
      genes: layers.transcripts.genes.map((g) => genes[g]?.name).filter(Boolean),
      minQ: layers.transcripts.minQ,
      pointSize: layers.transcripts.pointSize,
      gain: layers.transcripts.gain,
    },
    cells: { outlines: layers.cells.outlines, nuclei: layers.cells.nuclei, fill: layers.cells.fill ?? null, opacity: layers.cells.opacity },
    cell: state.cell ?? null,
  };
  if (mode === 'qc') view.qc = { map: state.qc?.map ?? 'none', filter: state.qc?.filter ?? null };
  return view;
}

// The renderer's layers for a view, on top of a dataset's defaults: channels and genes found by
// name. missing lists what the dataset does not have (a gene of another panel, a channel).
export function viewLayers(view, desc, defaults) {
  const missing = [];
  const channels = defaults.channels.map((c) => ({ ...c, window: [...c.window] }));
  for (const saved of view.channels ?? []) {
    const i = desc.channels.findIndex((c) => c.name === saved.name);
    if (i < 0) {
      missing.push(`channel ${saved.name}`);
      continue;
    }
    // A saved view records every setting; a tour's step may name only what it changes.
    const c = channels[i];
    channels[i] = { visible: saved.visible ?? c.visible, color: saved.color ?? c.color, window: saved.window ? [saved.window[0], saved.window[1]] : c.window, gamma: saved.gamma ?? c.gamma };
  }
  const index = new Map((desc.transcripts?.genes ?? []).map((g, i) => [g.name, i]));
  const genes = [];
  for (const name of view.transcripts?.genes ?? []) {
    if (index.has(name)) genes.push(index.get(name));
    else missing.push(`gene ${name}`);
  }
  const t = view.transcripts ?? {};
  return {
    layers: {
      channels,
      transcripts: { ...defaults.transcripts, visible: t.visible ?? defaults.transcripts.visible, all: t.all ?? false, genes, colors: [], minQ: t.minQ ?? defaults.transcripts.minQ, pointSize: t.pointSize ?? defaults.transcripts.pointSize, gain: t.gain ?? defaults.transcripts.gain },
      cells: { ...defaults.cells, ...(view.cells ?? {}) },
    },
    missing,
  };
}

// The camera for a view on a screen of `ratio` device pixels per CSS pixel.
export function viewCamera(view, ratio = 1) {
  return { cx: view.camera.cx, cy: view.camera.cy, scale: ratio / view.camera.umPerCss };
}

export function addView(project, view, now = new Date().toISOString()) {
  return { ...project, modified: now, views: [...project.views, view], history: [...project.history, { time: now, action: 'view', detail: `Saved view ${view.name}` }] };
}

// The project with a view replaced by an updated one (same id), kept in its place.
export function updateView(project, view, now = new Date().toISOString()) {
  if (!project.views.some((v) => v.id === view.id)) return project;
  return { ...project, modified: now, views: project.views.map((v) => (v.id === view.id ? { ...view, updated: now } : v)), history: [...project.history, { time: now, action: 'view', detail: `Updated view ${view.name}` }] };
}

export function renameView(project, id, name, now = new Date().toISOString()) {
  const trimmed = String(name).trim();
  const view = project.views.find((v) => v.id === id);
  if (!view || !trimmed || trimmed === view.name) return project;
  return { ...project, modified: now, views: project.views.map((v) => (v.id === id ? { ...v, name: trimmed } : v)) };
}

export function removeView(project, id, now = new Date().toISOString()) {
  const view = project.views.find((v) => v.id === id);
  if (!view) return project;
  return { ...project, modified: now, views: project.views.filter((v) => v.id !== id), history: [...project.history, { time: now, action: 'view', detail: `Removed view ${view.name}` }] };
}

// A deep link to a view: the page's address with the project and the view in its fragment.
export function viewLink(base, projectId, viewId) {
  return `${base.replace(/#.*$/, '')}#project=${encodeURIComponent(projectId)}&view=${encodeURIComponent(viewId)}`;
}

// The project and view a link's fragment names, or null.
export function parseViewLink(hash) {
  const params = new URLSearchParams(String(hash).replace(/^#/, ''));
  const project = params.get('project');
  const view = params.get('view');
  return project && view ? { project, view } : null;
}

// Whether two views show the same thing (ids, names and times aside), for checking that a view
// reopens exactly.
export function sameView(a, b) {
  const strip = (v) => JSON.stringify({ ...v, id: undefined, name: undefined, created: undefined, updated: undefined });
  return strip(a) === strip(b);
}
