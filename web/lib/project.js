// The project document: one immutable JSON value per project. Edits return a new value
// (store.commit records it for undo); nothing here mutates its argument. Adapted from MaveScape
// 0.8.0 web/lib/workspace.js.
//
// Wave 1, slice 1 defines the envelope; later slices fill it: datasets (slice 3), QC filter
// versions (slice 6), saved views (slice 7), segmentation artifacts, decisions and exports
// (wave 2).
// Source bundles are never inside a project: a dataset refers to its bundle by path and checksums.

export const PROJECT_FORMAT = 'situcraft-project';
export const PROJECT_VERSION = 1;

const ID_PATTERN = /^[A-Za-z0-9_-]{1,80}$/;

function newId() {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  return `p-${random.replace(/-/g, '').slice(0, 20)}`;
}

export function createProject(name = 'Untitled project', options = {}) {
  const now = options.now ?? new Date().toISOString();
  return {
    format: PROJECT_FORMAT,
    version: PROJECT_VERSION,
    id: options.id ?? newId(),
    name,
    created: now,
    modified: now,
    // Xenium output bundles (and later other platforms' outputs), each by its path on this
    // computer, its version and the checksums of its key files.
    datasets: [],
    // Derived results with their lineage: segmentations, label sets, filters, graphs, analyses.
    artifacts: [],
    // Append-only decisions with their rationale (a promoted segmentation, a review).
    decisions: [],
    // Saved views: viewport, layers, colors, genes and filters, reopened exactly.
    views: [],
    // (exports: the exports written from its segmentations, added with the first; lib/segmentations.js.)
    // (regions: the regions drawn on its datasets, added with the first; lib/regions.js.)
    // (reviews: the reviews of its cells and regions, added with the first; lib/review-record.js.)
    // (samples and sampleFields: its samples' records and fields, added with the first; lib/samples.js.)
    // (figures: its figures, added with the first; lib/figures.js.)
    // The log of material changes: [{ time, action, detail }].
    history: [],
  };
}

// A project from its saved text: checked for the format, completed with any field a later version
// added, and refused (with a plain message) when it is not a SituCraft project or is newer than
// this program.
export function parseProject(text) {
  let doc;
  try {
    doc = JSON.parse(text);
  } catch {
    throw new Error('This is not a SituCraft project: it is not valid JSON.');
  }
  if (!doc || typeof doc !== 'object' || doc.format !== PROJECT_FORMAT) {
    throw new Error(`This is not a SituCraft project (its format is not "${PROJECT_FORMAT}").`);
  }
  if (!Number.isInteger(doc.version) || doc.version < 1) throw new Error('The project has no valid version number.');
  if (doc.version > PROJECT_VERSION) {
    throw new Error(`The project was saved by a newer SituCraft (project version ${doc.version}; this program reads up to ${PROJECT_VERSION}). Update SituCraft to open it.`);
  }
  if (typeof doc.id !== 'string' || !ID_PATTERN.test(doc.id)) throw new Error('The project has no valid id.');
  for (const field of ['datasets', 'artifacts', 'decisions', 'views', 'exports', 'regions', 'reviews', 'samples', 'sampleFields', 'figures', 'history']) {
    if (field in doc && !Array.isArray(doc[field])) throw new Error(`The project's ${field} is not a list.`);
  }
  const base = createProject(typeof doc.name === 'string' ? doc.name : 'Untitled project', { id: doc.id, now: doc.created ?? doc.modified });
  return { ...base, ...doc };
}

// The saved text. Keys keep their order, so saving an unchanged project writes the same bytes.
export function serializeProject(project) {
  return JSON.stringify(project);
}

export function touch(project, now = new Date().toISOString()) {
  return { ...project, modified: now };
}

export function rename(project, name, now) {
  const trimmed = String(name).trim();
  if (!trimmed || trimmed === project.name) return project;
  return touch({ ...project, name: trimmed }, now);
}

// Whether a project holds anything worth saving (an empty, untitled one is not saved).
export function isEmptyProject(project) {
  return !project.datasets.length && !project.artifacts.length && !project.decisions.length && !project.views.length;
}
