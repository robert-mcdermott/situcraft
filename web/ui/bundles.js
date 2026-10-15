// Xenium output bundles on this computer, as the page sees them: the items the program holds
// (local.go), their validation reports (api_datasets.go), the project's datasets reconnected
// to their bundles when a project opens, and each dataset's derived data (api_cache.go), built
// by a job when the dataset is added or found again. Reports are kept for the session, not in the
// project: the project keeps each dataset's summary and findings (lib/datasets.js).

import { openPaths } from './storage.js';
import { addDataset, compareFingerprint, datasetRecord, findDataset, relocateDataset, removeDataset } from '../lib/datasets.js';
import { addComparison, addExport, addSegmentation, comparisonRecord, exportRecord, segmentationRecord, segmentationsOf, withInstrumentSegmentation } from '../lib/segmentations.js';

const BUNDLE_KINDS = new Set(['bundle', 'archive']);

async function getJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `${url}: HTTP ${response.status}`);
  return body;
}

// The key of the job building a bundle's derived data (api_cache.go).
export function cacheJobKey(path) {
  return `cache\u0000${path}`;
}

export function createBundles(app) {
  // path → { state: 'validating' | 'done' | 'error', report, error }
  const reports = new Map();
  // dataset id → { state: 'checking' | 'ok' | 'missing' | 'changed' | 'different-run', item, files, error }
  const links = new Map();
  // path → { state: 'loading' | 'done' | 'error', status, job, error }: the bundle's derived data
  // and the job building them when it was asked.
  const caches = new Map();
  // What the cache folder holds (api_cache.go's usage), refreshed after each build.
  let usage = null;
  const notify = () => app.store.notify(['bundles']);

  function items() {
    return app.localItems.filter((item) => BUNDLE_KINDS.has(item.kind));
  }

  function itemByPath(path) {
    return app.localItems.find((item) => item.path === path) ?? null;
  }

  // Validates a bundle (once per session unless forced); resolves to its report.
  async function validate(item, { force = false } = {}) {
    const known = reports.get(item.path);
    if (known && !force && known.state !== 'error') return known.promise;
    const entry = { state: 'validating', report: known?.report ?? null, error: null };
    entry.promise = (async () => {
      try {
        const body = await getJSON(`${item.url}/validation`);
        entry.state = 'done';
        entry.report = body.report;
        app.log(`Checked ${body.report.bundle}: ${countText(body.report)}.`);
        return body.report;
      } catch (error) {
        entry.state = 'error';
        entry.error = error.message;
        app.log(`${item.name}: ${error.message}`);
        throw error;
      } finally {
        notify();
      }
    })();
    entry.promise.catch(() => {});
    reports.set(item.path, entry);
    notify();
    return entry.promise;
  }

  async function add(item) {
    const report = await validate(item);
    const record = datasetRecord(item, report);
    app.store.commit(addDataset(app.store.project, record), `Add dataset ${record.name}`);
    links.set(record.id, { state: 'ok', item });
    notify();
    buildCache(item);
    registerSegmentation(item, record.id);
    return record;
  }

  // The instrument's segmentation of a dataset's bundle, registered as an artifact by a job
  // (api_segmentations.go), then recorded in the project (and promoted when the dataset has no
  // promoted segmentation). Resolves with the artifact, or null.
  const registering = new Map(); // dataset id → promise
  function registerSegmentation(item, datasetId) {
    if (registering.has(datasetId)) return registering.get(datasetId);
    const promise = (async () => {
      try {
        let answer = await getJSON(`${item.url}/segmentation`, { method: 'POST' });
        if (!answer.artifact && answer.job) {
          const job = await app.jobs.finished(answer.job.id);
          if (job?.state !== 'complete') {
            app.log(`${item.name}: the instrument's segmentation was not registered (${job?.error || job?.state || 'unknown'}).`);
            return null;
          }
          answer = await getJSON(`${item.url}/segmentation`);
        }
        const manifest = answer.artifact;
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!manifest || !dataset) return manifest ?? null;
        const next = withInstrumentSegmentation(app.store.project, dataset, manifest);
        if (next !== app.store.project) app.store.commit(next, `Register ${dataset.name}'s segmentation`);
        return manifest;
      } catch (error) {
        app.log(`${item.name}: the instrument's segmentation could not be registered: ${error.message}`);
        return null;
      } finally {
        registering.delete(datasetId);
        notify();
      }
    })();
    registering.set(datasetId, promise);
    return promise;
  }

  // SituCraft's nucleus expansion of a dataset's bundle (api_expansion.go): a job grows its nuclei
  // by distance µm and records the segmentation as an artifact, which is then recorded in the
  // project. Resolves with the artifact; rejects with why it was not made.
  const expanding = new Map(); // `${dataset id}|${distance}` → { dataset, distance, job, promise }
  function expandNuclei(item, datasetId, distance = 5) {
    const key = `${datasetId}|${distance}`;
    if (expanding.has(key)) return expanding.get(key).promise;
    const entry = { dataset: datasetId, distance, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/expansion`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ distance_um: distance }) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the expansion ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addSegmentation(app.store.project, segmentationRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Add ${manifest.name.toLowerCase()} of ${dataset.name}`);
        return manifest;
      } finally {
        expanding.delete(key);
        notify();
      }
    })();
    expanding.set(key, entry);
    notify();
    return entry.promise;
  }

  // SituCraft's transcript-informed segmentation of a dataset (api_refine.go): a job refines the
  // instrument's cells from the transcripts and records the segmentation as an artifact, which is
  // recorded in the project and then compared with the instrument's segmentation (the comparison
  // workspace's first comparison of it). Resolves with { manifest, comparison } (the comparison
  // null when it could not be made, with why in the log).
  const refining = new Map(); // dataset id → { dataset, job, promise }
  function refineSegmentation(item, datasetId, options = {}) {
    if (refining.has(datasetId)) return refining.get(datasetId).promise;
    const entry = { dataset: datasetId, job: null, promise: null };
    entry.promise = (async () => {
      let manifest;
      try {
        const answer = await getJSON(`${item.url}/refinement`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(options) });
        manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the refinement ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addSegmentation(app.store.project, segmentationRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Add ${manifest.name.toLowerCase()} of ${dataset.name}`);
      } finally {
        refining.delete(datasetId);
        notify();
      }
      // Into the comparison workspace: compared with the instrument's segmentation.
      const instrument = segmentationsOf(app.store.project, datasetId).find((a) => a.source === 'instrument' && a.state === 'complete');
      let comparison = null;
      if (instrument && manifest.state === 'complete') {
        try {
          comparison = await compareSegmentations(item, datasetId, instrument.id, manifest.id);
        } catch (error) {
          app.log(`${manifest.name} could not be compared with the instrument's segmentation: ${error.message}`);
        }
      }
      return { manifest, comparison };
    })();
    refining.set(datasetId, entry);
    notify();
    return entry.promise;
  }

  // A segmentation made by another tool, imported into a dataset (api_import.go): a job reads the
  // files, checks them against the dataset and records the artifact, which is then recorded in the
  // project, complete or (when its check found a fatal problem) invalid, with what it found.
  // Resolves with the artifact; rejects with why none was made.
  const importing = new Map(); // job id → { dataset, title }
  async function importSegmentation(item, datasetId, options) {
    const answer = await getJSON(`${item.url}/import`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(options) });
    const jobId = answer.job?.id;
    if (!jobId) throw new Error('no job was started');
    importing.set(jobId, { dataset: datasetId, title: answer.job.title });
    notify();
    try {
      const job = await app.jobs.finished(jobId);
      if (job?.state !== 'complete' && job?.state !== 'invalid') throw new Error(`the import ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
      // The job's log names the artifact (an import of files imported before answers that one).
      const id = [...(job.events ?? []).map((e) => e.message), job.error ?? ''].map((m) => /artifact (imp-[0-9a-f]+)/.exec(m)?.[1]).find(Boolean);
      if (!id) throw new Error('the import recorded no artifact');
      const manifest = await getJSON(`/api/artifacts/${encodeURIComponent(id)}`);
      const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
      if (!dataset) throw new Error('the dataset is no longer in the project');
      const next = addSegmentation(app.store.project, segmentationRecord(dataset, manifest));
      if (next !== app.store.project) app.store.commit(next, `Import ${manifest.name.replace(/ \(imported\)$/, '')} into ${dataset.name}`);
      return manifest;
    } finally {
      importing.delete(jobId);
      notify();
    }
  }

  // A comparison of two segmentations of a dataset (api_compare.go), made by a job and recorded as
  // an artifact, then in the project. Resolves with the artifact.
  const comparing = new Map(); // `${dataset}|${a}|${b}` → { dataset, a, b, job, promise }
  function compareSegmentations(item, datasetId, a, b) {
    const key = `${datasetId}|${a}|${b}`;
    if (comparing.has(key)) return comparing.get(key).promise;
    const entry = { dataset: datasetId, a, b, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/compare`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ a, b }) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the comparison ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addComparison(app.store.project, comparisonRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Compare segmentations of ${dataset.name}`);
        return manifest;
      } finally {
        comparing.delete(key);
        notify();
      }
    })();
    comparing.set(key, entry);
    notify();
    return entry.promise;
  }

  // Errors planted in a dataset's instrument segmentation, for practice (api_plant.go): a job
  // writes the planted segmentation with its answer key and records it, simulated; it is then
  // recorded in the project. Resolves with the artifact.
  const planting = new Map(); // dataset id → { dataset, job, promise }
  function plantErrors(item, datasetId, params = {}) {
    if (planting.has(datasetId)) return planting.get(datasetId).promise;
    const entry = { dataset: datasetId, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/plant`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(params) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the planting ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addSegmentation(app.store.project, segmentationRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Plant errors in ${dataset.name}'s segmentation`);
        return manifest;
      } finally {
        planting.delete(datasetId);
        notify();
      }
    })();
    planting.set(datasetId, entry);
    notify();
    return entry.promise;
  }

  // A planted segmentation's answer key (planted.json), read once.
  const keys = new Map();
  function plantedErrors(id) {
    if (!keys.has(id)) {
      const p = getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/planted.json`);
      p.catch(() => keys.delete(id));
      keys.set(id, p);
    }
    return keys.get(id);
  }

  // An export of a segmentation (api_export.go): a job writes the formats into the folder; its
  // manifest is then read and the export recorded in the project. Resolves with the manifest.
  const exporting = new Map(); // job id → { dataset, title, folder }
  async function exportSegmentation(item, datasetId, { segmentation, formats, folder, overwrite = false }) {
    const answer = await getJSON(`${item.url}/export`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ segmentation, formats, folder, overwrite }) });
    const jobId = answer.job?.id;
    if (!jobId) throw new Error('no job was started');
    exporting.set(jobId, { dataset: datasetId, title: answer.job.title, folder: answer.folder });
    notify();
    try {
      const job = await app.jobs.finished(jobId);
      if (job?.state !== 'complete') throw new Error(`the export ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
      const manifest = await getJSON(`/api/exports?folder=${encodeURIComponent(answer.folder)}`);
      const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
      const record = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.id === segmentation);
      if (dataset && record) app.store.commit(addExport(app.store.project, exportRecord(dataset, record, manifest, answer.folder)), `Export ${record.name} of ${dataset.name}`);
      return manifest;
    } finally {
      exporting.delete(jobId);
      notify();
    }
  }

  // A comparison's full description (comparison.json), read once.
  const descriptions = new Map();
  function comparisonDescription(id) {
    if (!descriptions.has(id)) {
      const p = getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/comparison.json`);
      p.catch(() => descriptions.delete(id));
      descriptions.set(id, p);
    }
    return descriptions.get(id);
  }

  // Asks which of the bundle's derived data are built (build: and starts a job building the rest).
  async function cacheStatus(item, { build = false } = {}) {
    const entry = caches.get(item.path) ?? { state: 'loading', status: null, job: null, error: null };
    entry.state = 'loading';
    caches.set(item.path, entry);
    notify();
    try {
      const body = await getJSON(`${item.url}/cache`, build ? { method: 'POST' } : {});
      entry.state = 'done';
      entry.status = body.status;
      entry.job = body.job ?? null;
      entry.error = null;
    } catch (error) {
      entry.state = 'error';
      entry.error = error.message;
    }
    notify();
    return entry;
  }

  function buildCache(item) {
    return cacheStatus(item, { build: true });
  }

  async function refreshUsage() {
    try {
      usage = await getJSON('/api/cache');
    } catch {
      usage = null;
    }
    notify();
    return usage;
  }

  // A finished build changes what is built and what the cache holds.
  app.jobs?.onFinished((job) => {
    if (job.kind !== 'cache') return;
    const item = app.localItems.find((i) => cacheJobKey(i.path) === job.key);
    if (item) cacheStatus(item);
    refreshUsage();
  });

  function remove(id) {
    const dataset = app.store.project.datasets.find((d) => d.id === id);
    if (!dataset) return;
    app.store.commit(removeDataset(app.store.project, id), `Remove dataset ${dataset.name}`);
    links.delete(id);
  }

  // Finds each dataset's bundle where the project recorded it and checks it is the same bundle.
  async function reconnect() {
    const generation = app.store.sameProject();
    const datasets = app.store.project.datasets;
    links.clear();
    for (const d of datasets) links.set(d.id, { state: 'checking' });
    notify();
    for (const d of datasets) {
      if (!d.path) {
        links.set(d.id, { state: 'missing', error: 'The project records no place for this dataset.' });
        notify();
        continue;
      }
      let answer;
      try {
        answer = await openPaths([d.path]);
      } catch (error) {
        links.set(d.id, { state: 'missing', error: error.message });
        continue;
      }
      if (!generation()) return;
      app.setLocalItems(answer.files);
      const item = itemByPath(d.path);
      if (!item) {
        links.set(d.id, { state: 'missing', error: answer.problems?.[0] ?? `${d.path} is not there.` });
        notify();
        continue;
      }
      const link = await check(d, item);
      links.set(d.id, link);
      notify();
      // Derived data are keyed by the files' contents, so a changed bundle gets its own.
      if (link.state === 'ok' || link.state === 'changed') {
        buildCache(item);
        // A dataset added before segmentations were artifacts (SituCraft 0.1.0) gets its
        // instrument segmentation now; one whose bundle changed gets the changed files'.
        if (link.state === 'changed' || !segmentationsOf(app.store.project, d.id).length) registerSegmentation(item, d.id);
      }
    }
  }

  // Compares a dataset with the bundle at an item.
  async function check(dataset, item) {
    try {
      const description = await getJSON(`${item.url}/bundle`);
      const result = compareFingerprint(dataset.fingerprint ?? [], description.fingerprint ?? []);
      return { state: result.status === 'same' ? 'ok' : result.status, item, files: result.files };
    } catch (error) {
      return { state: 'missing', item, error: error.message };
    }
  }

  // Points a dataset at its bundle's new place, if what is there is the same bundle.
  async function locate(id, path) {
    const dataset = app.store.project.datasets.find((d) => d.id === id);
    if (!dataset) return null;
    const answer = await openPaths([path]);
    app.setLocalItems(answer.files);
    if (answer.problems?.length) throw new Error(answer.problems[0]);
    const item = app.localItems.find((i) => BUNDLE_KINDS.has(i.kind) && (i.path === path || i.path.startsWith(`${path}/`))) ?? null;
    if (!item) throw new Error(`${path} is not a Xenium output bundle.`);
    const link = await check(dataset, item);
    if (link.state === 'different-run') throw new Error(`${item.name} is another run (or another analysis of it) than ${dataset.name}; it was not used.`);
    app.store.commit(relocateDataset(app.store.project, id, item.path), `Locate dataset ${dataset.name}`);
    links.set(id, link);
    notify();
    buildCache(item);
    if (!segmentationsOf(app.store.project, id).length) registerSegmentation(item, id);
    return link;
  }

  return {
    items,
    itemByPath,
    validate,
    add,
    remove,
    reconnect,
    locate,
    report: (path) => reports.get(path) ?? null,
    link: (id) => links.get(id) ?? null,
    cache: (path) => caches.get(path) ?? null,
    cacheStatus,
    buildCache,
    registerSegmentation,
    expandNuclei,
    refineSegmentation,
    importSegmentation,
    compareSegmentations,
    comparisonDescription,
    plantErrors,
    plantedErrors,
    exportSegmentation,
    // The plantings in a dataset under way: [{ job }].
    plantings: (id) => [...planting.values()].filter((e) => e.dataset === id).map(({ job }) => ({ job })),
    // The exports of a dataset under way: [{ job, title, folder }].
    exports: (id) => [...exporting.entries()].filter(([, v]) => v.dataset === id).map(([job, v]) => ({ job, title: v.title, folder: v.folder })),
    // The comparisons of a dataset under way: [{ a, b, job }].
    comparisons: (id) => [...comparing.values()].filter((e) => e.dataset === id).map(({ a, b, job }) => ({ a, b, job })),
    // The imports into a dataset under way: [{ job, title }].
    imports: (id) => [...importing.entries()].filter(([, v]) => v.dataset === id).map(([job, v]) => ({ job, title: v.title })),
    // The expansions of a dataset under way: [{ distance, job }].
    expansions: (id) => [...expanding.values()].filter((e) => e.dataset === id).map(({ distance, job }) => ({ distance, job })),
    // The refinement of a dataset under way: [{ job }].
    refinements: (id) => [...refining.values()].filter((e) => e.dataset === id).map(({ job }) => ({ job })),
    // Whether a dataset's instrument segmentation is being registered.
    registeringSegmentation: (id) => registering.has(id),
    usage: () => usage,
    refreshUsage,
    // The job building the bundle's derived data, if one is queued or running: as the job list
    // has it, or as the answer that started it did until the list has it.
    cacheJob(item) {
      const live = app.jobs?.forKey(cacheJobKey(item.path));
      if (live) return live;
      const asked = caches.get(item.path)?.job;
      if (!asked) return null;
      const known = app.jobs?.get(asked.id);
      return known ? null : asked;
    },
    // The project's dataset for a bundle on this computer, if it has one.
    datasetFor: (item) => findDataset(app.store.project, { path: item.path }),
  };
}

export function countText(report) {
  const n = (s) => report.findings.filter((f) => f.severity === s).length;
  const fatal = n('fatal');
  const warnings = n('warning');
  const parts = [fatal ? `${fatal} fatal problem${fatal === 1 ? '' : 's'}` : 'no fatal problems', `${warnings} warning${warnings === 1 ? '' : 's'}`, `${n('info')} note${n('info') === 1 ? '' : 's'}`];
  return parts.join(', ');
}
