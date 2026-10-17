// Xenium output bundles on this computer, as the page sees them: the items the program holds
// (local.go), their validation reports (api_datasets.go), the project's datasets reconnected
// to their bundles when a project opens, and each dataset's derived data (api_cache.go), built
// by a job when the dataset is added or found again. Reports are kept for the session, not in the
// project: the project keeps each dataset's summary and findings (lib/datasets.js).

import { openPaths } from './storage.js';
import { addClustering, addEmbedding, clusteringRecord, embeddingRecord } from '../lib/clusterings.js';
import { addDataset, compareFingerprint, datasetRecord, findDataset, relocateDataset, removeDataset } from '../lib/datasets.js';
import { addLabels, labelsRecord, readLabelCells } from '../lib/labels.js';
import { addImage as withImage, imageRecord } from '../lib/images.js';
import { addCheck as withCheck, addTransform as withTransform, checkRecord, SUPPLIED_ALIGNMENT, transformInUse, transformRecord, useTransform } from '../lib/transforms.js';
import { readInverseField } from '../lib/overlays.js';
import { addComparison, addExport, addSegmentation, comparisonRecord, exportRecord, removeComparison as withoutComparison, segmentationRecord, segmentationsOf, withInstrumentSegmentation } from '../lib/segmentations.js';

const BUNDLE_KINDS = new Set(['bundle', 'archive']);

async function getJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(body.error ?? `${url}: HTTP ${response.status}`), { code: body.code, missing: body.missing, status: response.status });
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
          if (job?.state !== 'complete') throw Object.assign(new Error(`the refinement ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`), { job });
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

  // The segmentation studio's presets and settings' help (one request a session), and a spec's
  // preflight on a bundle (api_refine.go).
  let presets = null;
  function refinementPresets() {
    presets ??= getJSON('/api/refinement/presets').catch((error) => {
      presets = null;
      throw error;
    });
    return presets;
  }
  function refinementPreflight(item, request) {
    return getJSON(`${item.url}/refinement/preflight`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
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

  // What an image is, and the alignment files beside it (api_images.go); with alignment, what that
  // file does.
  async function checkImage(item, path, alignment = '') {
    const q = new URLSearchParams({ path });
    if (alignment) q.set('alignment', alignment);
    return getJSON(`${item.url}/images/check?${q}`);
  }

  // A post-Xenium image added to a dataset (api_images.go): a job reads and checksums it and records
  // the artifact, and its alignment when one was supplied as its first transform; both recorded in
  // the project, the alignment placing the image when it is the image's first or new to the
  // project. Resolves with the image's artifact, its transform's (or null) as its transform.
  const addingImages = new Map(); // job id → { dataset, title }
  async function addImage(item, datasetId, request) {
    const answer = await getJSON(`${item.url}/images`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
    const jobId = answer.job?.id;
    if (!jobId) throw new Error('no job was started');
    addingImages.set(jobId, { dataset: datasetId, title: answer.job.title });
    notify();
    try {
      const job = await app.jobs.finished(jobId);
      if (job?.state !== 'complete') throw new Error(`adding the image ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
      const found = (pattern) => (job.events ?? []).map((e) => pattern.exec(e.message)?.[1]).find(Boolean);
      const id = found(/artifact (img-[0-9a-f]+)/);
      if (!id) throw new Error('the job recorded no image');
      const tf = found(/artifact (tf-[0-9a-f]+)/);
      const manifest = await getJSON(`/api/artifacts/${encodeURIComponent(id)}`);
      const transform = tf ? await getJSON(`/api/artifacts/${encodeURIComponent(tf)}`) : null;
      const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
      if (!dataset) throw new Error('the dataset is no longer in the project');
      let next = withImage(app.store.project, imageRecord(dataset, manifest));
      if (transform) {
        const isNew = !next.artifacts.some((a) => a.kind === 'transform' && a.id === transform.id);
        next = withTransform(next, transformRecord(dataset, transform));
        if (isNew || !transformInUse(next, id)) next = useTransform(next, { transform: transform.id, rationale: SUPPLIED_ALIGNMENT, by: 'SituCraft' });
      }
      if (next !== app.store.project) app.store.commit(next, `Add ${manifest.name} to ${dataset.name}`);
      return { ...manifest, transform };
    } finally {
      addingImages.delete(jobId);
      notify();
    }
  }

  // A version of an image's alignment (api_transforms.go): from landmarks ({ landmarks, model,
  // parent, by, note }) or Xenium Explorer's file ({ alignment }), recorded in the project and, with
  // use, chosen to place the image (rationale, by). Resolves with { manifest, transform }.
  async function makeTransform(datasetId, imageId, request, { use = false, rationale = '', by = 'the user' } = {}) {
    const answer = await getJSON(`/api/images/${encodeURIComponent(imageId)}/transforms`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
    const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
    if (!dataset) throw new Error('the dataset is no longer in the project');
    transformDescriptions.set(answer.manifest.id, Promise.resolve(answer.transform));
    let next = withTransform(app.store.project, transformRecord(dataset, answer.manifest));
    if (use && transformInUse(next, imageId)?.record.id !== answer.manifest.id) next = useTransform(next, { transform: answer.manifest.id, rationale, by });
    if (next !== app.store.project) app.store.commit(next, `Align ${next.artifacts.find((a) => a.id === imageId)?.name ?? imageId}: ${answer.manifest.name}`);
    return answer;
  }

  // An image registered automatically to its dataset's DAPI (api_register.go): a job, its version
  // recorded in the project and, with use, chosen to place the image. Resolves with the version's
  // artifact.
  const aligning = new Map(); // job id → { dataset, image, title }
  async function registerImage(item, datasetId, imageId, request, { use = false, rationale = '', by = 'the user' } = {}) {
    const answer = await getJSON(`${item.url}/images/${encodeURIComponent(imageId)}/register`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
    const jobId = answer.job?.id;
    if (!jobId) throw new Error('no job was started');
    aligning.set(jobId, { dataset: datasetId, image: imageId, title: answer.job.title });
    notify();
    try {
      const job = await app.jobs.finished(jobId);
      if (job?.state !== 'complete') throw new Error(`the registration ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
      const id = (job.events ?? []).map((e) => /artifact (tf-[0-9a-f]+)/.exec(e.message)?.[1]).find(Boolean);
      if (!id) throw new Error('the job recorded no alignment');
      const manifest = await getJSON(`/api/artifacts/${encodeURIComponent(id)}`);
      const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
      if (!dataset) throw new Error('the dataset is no longer in the project');
      let next = withTransform(app.store.project, transformRecord(dataset, manifest));
      if (use && transformInUse(next, imageId)?.record.id !== id) next = useTransform(next, { transform: id, rationale, by });
      if (next !== app.store.project) app.store.commit(next, `Align ${next.artifacts.find((a) => a.id === imageId)?.name ?? imageId} automatically`);
      return manifest;
    } finally {
      aligning.delete(jobId);
      notify();
    }
  }

  // A version of an image's alignment checked over the tissue (api_checks.go): a job, the check
  // recorded in the project. Resolves with the check's artifact.
  const checking = new Map(); // job id → { image, transform }
  async function checkAlignment(item, datasetId, imageId, transformId) {
    const answer = await getJSON(`${item.url}/images/${encodeURIComponent(imageId)}/check`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ transform: transformId }) });
    const jobId = answer.job?.id;
    if (!jobId) throw new Error('no job was started');
    checking.set(jobId, { image: imageId, transform: transformId });
    notify();
    try {
      const job = await app.jobs.finished(jobId);
      if (job?.state !== 'complete') throw new Error(`the check ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
      const id = (job.events ?? []).map((e) => /artifact (chk-[0-9a-f]+)/.exec(e.message)?.[1]).find(Boolean);
      if (!id) throw new Error('the job recorded no check');
      const manifest = await getJSON(`/api/artifacts/${encodeURIComponent(id)}`);
      const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
      if (!dataset) throw new Error('the dataset is no longer in the project');
      const next = withCheck(app.store.project, checkRecord(dataset, manifest));
      if (next !== app.store.project) app.store.commit(next, `Check ${next.artifacts.find((a) => a.id === transformId)?.name ?? transformId}`);
      return manifest;
    } finally {
      checking.delete(jobId);
      notify();
    }
  }

  // A check's description (api_checks.go); kept for the session.
  const checkDescriptions = new Map();
  function checkDescription(id) {
    if (!checkDescriptions.has(id)) {
      const p = getJSON(`/api/checks/${encodeURIComponent(id)}`);
      p.catch(() => checkDescriptions.delete(id));
      checkDescriptions.set(id, p);
    }
    return checkDescriptions.get(id);
  }

  // A deformed version's inverse field (api_register.go), read for drawing; kept for the session.
  const inverseFields = new Map();
  function inverseField(id) {
    if (!inverseFields.has(id)) {
      const p = fetch(`/api/transforms/${encodeURIComponent(id)}/inverse`).then(async (r) => {
        if (!r.ok) throw new Error(`its deformation: HTTP ${r.status}`);
        return readInverseField(await r.arrayBuffer());
      });
      p.catch(() => inverseFields.delete(id));
      inverseFields.set(id, p);
    }
    return inverseFields.get(id);
  }

  // A transform's description (api_transforms.go); kept for the session.
  const transformDescriptions = new Map();
  function transformDescription(id) {
    if (!transformDescriptions.has(id)) {
      const p = getJSON(`/api/transforms/${encodeURIComponent(id)}`);
      p.catch(() => transformDescriptions.delete(id));
      transformDescriptions.set(id, p);
    }
    return transformDescriptions.get(id);
  }

  // An image's description (api_images.go), with its id, for drawing it; kept for the session.
  const imageDescriptions = new Map();
  function imageDescription(id) {
    if (!imageDescriptions.has(id)) {
      const p = getJSON(`/api/images/${encodeURIComponent(id)}`).then((d) => ({ ...d, id }));
      p.catch(() => imageDescriptions.delete(id));
      imageDescriptions.set(id, p);
    }
    return imageDescriptions.get(id);
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

  // Removes a comparison from the project (undoable), saves, then asks the program to move its files
  // to the trash, which it does once no saved project names it. Answers the program's answer:
  // { trashed, usedBy: [project names], missing }.
  async function removeComparison(id) {
    const record = app.store.project.artifacts.find((a) => a.kind === 'comparison' && a.id === id);
    const next = withoutComparison(app.store.project, id);
    app.store.commit(next, `Remove the comparison ${record?.name ?? id}`);
    await app.saveNow?.();
    return getJSON(`/api/artifacts/${encodeURIComponent(id)}/trash`, { method: 'POST' });
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
  // Clusters a segmentation's cells (api_clustering.go): request { segmentation, filter,
  // parameters }. A job makes the clustering unless it was made already; it is recorded in the
  // project. Answers its manifest.
  const clustering = new Map(); // `${dataset}|${segmentation}` → { dataset, segmentation, job, promise }
  function clusterCells(item, datasetId, request) {
    const key = `${datasetId}|${request.segmentation}`;
    if (clustering.has(key)) return clustering.get(key).promise;
    const entry = { dataset: datasetId, segmentation: request.segmentation, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/cluster`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the clustering ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addClustering(app.store.project, clusteringRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Cluster the cells of ${dataset.name}`);
        return manifest;
      } finally {
        clustering.delete(key);
        notify();
      }
    })();
    clustering.set(key, entry);
    notify();
    return entry.promise;
  }

  // Lays out a clustering's cells on a map (api_maps.go), unless it was made already; recorded in
  // the project. Answers its manifest.
  const mapping = new Map(); // clustering id → { dataset, clustering, job, promise }
  function makeMap(item, datasetId, clustering, parameters = null) {
    if (mapping.has(clustering)) return mapping.get(clustering).promise;
    const entry = { dataset: datasetId, clustering, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/embed`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ clustering, ...(parameters ? { parameters } : {}) }) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the map ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addEmbedding(app.store.project, embeddingRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Map the cells of ${dataset.name}`);
        return manifest;
      } finally {
        mapping.delete(clustering);
        notify();
      }
    })();
    mapping.set(clustering, entry);
    notify();
    return entry.promise;
  }

  // The instrument's analysis in a bundle: its clusterings, each with its artifact once registered.
  function instrumentAnalysis(item) {
    return getJSON(`${item.url}/analysis`);
  }

  // Registers one of the instrument's clusterings (and its map, the first time), recording both in
  // the project. Answers the clustering's manifest.
  const registeringAnalysis = new Map(); // `${dataset}|${name}` → { job, promise }
  function registerInstrumentClusters(item, datasetId, name) {
    const key = `${datasetId}|${name}`;
    if (registeringAnalysis.has(key)) return registeringAnalysis.get(key).promise;
    const entry = { dataset: datasetId, name, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/analysis`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ clustering: name }) });
        if (!answer.artifact) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`registering the instrument's clusters ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
        }
        const manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        const listed = await instrumentAnalysis(item);
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        let next = addClustering(app.store.project, clusteringRecord(dataset, manifest));
        if (listed.map?.artifact) {
          const mapManifest = await getJSON(`/api/artifacts/${encodeURIComponent(listed.map.artifact)}`);
          next = addEmbedding(next, embeddingRecord(dataset, mapManifest));
        }
        if (next !== app.store.project) app.store.commit(next, `Register the instrument's clusters of ${dataset.name}`);
        return manifest;
      } finally {
        registeringAnalysis.delete(key);
        notify();
      }
    })();
    registeringAnalysis.set(key, entry);
    notify();
    return entry.promise;
  }

  // A map's cells, places and scores, kept for the session.
  const maps = new Map();
  function mapData(id) {
    if (!maps.has(id)) {
      const p = (async () => {
        const response = await fetch(`/api/embeddings/${encodeURIComponent(id)}/map`);
        if (!response.ok) throw new Error(`the map: HTTP ${response.status}`);
        const buffer = await response.arrayBuffer();
        const head = new Uint32Array(buffer, 0, 2);
        const n = head[0];
        return { n, segCells: head[1], cells: new Uint32Array(buffer, 8, n), xy: new Float32Array(buffer, 8 + 4 * n, 2 * n), score: new Float32Array(buffer, 8 + 12 * n, n) };
      })();
      p.catch(() => maps.delete(id));
      maps.set(id, p);
    }
    return maps.get(id);
  }

  // A gene's log-normalized value in each of a segmentation's cells, by a clustering's normalization
  // (NaN for cells it did not analyze); the last few kept.
  const genes = new Map();
  function expression(item, clustering, gene) {
    const key = `${clustering}|${gene}`;
    if (!genes.has(key)) {
      const p = (async () => {
        const response = await fetch(`${item.url}/clusterings/${encodeURIComponent(clustering)}/expression?gene=${encodeURIComponent(gene)}`);
        if (!response.ok) {
          const body = await response.json().catch(() => ({}));
          throw new Error(body.error ?? `HTTP ${response.status}`);
        }
        return { gene: response.headers.get('X-Gene') ?? gene, values: new Float32Array(await response.arrayBuffer()) };
      })();
      p.catch(() => genes.delete(key));
      genes.set(key, p);
      while (genes.size > 12) genes.delete(genes.keys().next().value);
    }
    return genes.get(key);
  }
  function dotPlot(item, clustering, list) {
    return getJSON(`${item.url}/clusterings/${encodeURIComponent(clustering)}/dotplot?genes=${encodeURIComponent(list.join(','))}`);
  }
  // The markers of a selection of cells (a mask over the segmentation's cells) against the rest.
  function contrast(item, clustering, mask) {
    const bytes = new Uint8Array(4 + Math.ceil(mask.length / 8));
    new DataView(bytes.buffer).setUint32(0, mask.length, true);
    for (let i = 0; i < mask.length; i += 1) if (mask[i]) bytes[4 + (i >> 3)] |= 1 << (i & 7);
    let binary = '';
    for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    return getJSON(`${item.url}/clusterings/${encodeURIComponent(clustering)}/contrast`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ cells: btoa(binary) }) });
  }

  // A clustering's description, each cell's cluster (Int32Array over the segmentation's cells, −1
  // for one not analyzed) and its markers, kept for the session.
  const clusterData = new Map();
  function clusteringData(id) {
    if (!clusterData.has(id)) {
      const p = (async () => {
        const [description, cells] = await Promise.all([
          getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/clustering.json`),
          getJSON(`/api/clusterings/${encodeURIComponent(id)}/cells`),
        ]);
        return { description, cluster: Int32Array.from(cells.cluster), sizes: cells.sizes };
      })();
      p.catch(() => clusterData.delete(id));
      clusterData.set(id, p);
    }
    return clusterData.get(id);
  }
  function clusteringMarkers(id, top = 25) {
    return getJSON(`/api/clusterings/${encodeURIComponent(id)}/markers?top=${top}`);
  }

  // --- Labels from markers (api_labels.go) ---

  // The marker sets SituCraft offers for a clustering, with the genes its panel lacks: { genes,
  // starters: [{ id, set, checksum, missing }] }.
  function markerStarters(item, clustering) {
    return getJSON(`${item.url}/clusterings/${encodeURIComponent(clustering)}/markers`);
  }

  // A marker set checked by the program (against a panel's genes when given): { set, checksum,
  // missing: { label: [genes] } }; throws with the program's message when it is not a marker set.
  function checkMarkers(set, genes = null) {
    return getJSON('/api/markers/check', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ set, ...(genes ? { genes } : {}) }) });
  }

  // Labels a clustering's cells from a marker set (request { clustering, markers, parameters }),
  // unless it was done already; recorded in the project with the marker set's version
  // (markerSet). Answers the manifest.
  const labeling = new Map(); // `${clustering}|${markerSet}` → { dataset, clustering, job, promise }
  function labelCells(item, datasetId, request, markerSet = null) {
    const key = `${request.clustering}|${markerSet}|${JSON.stringify(request.parameters ?? null)}`;
    if (labeling.has(key)) return labeling.get(key).promise;
    const entry = { dataset: datasetId, clustering: request.clustering, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/label`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the labels ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addLabels(app.store.project, labelsRecord(dataset, manifest, markerSet));
        if (next !== app.store.project) app.store.commit(next, `Label the cells of ${dataset.name}`);
        return manifest;
      } finally {
        labeling.delete(key);
        notify();
      }
    })();
    labeling.set(key, entry);
    notify();
    return entry.promise;
  }

  // A table of labels read and described by the program, matched to a segmentation's cells
  // (request { path or instrument, segmentation, columns }).
  function readLabelsTable(item, request) {
    return getJSON(`${item.url}/labels/read`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
  }

  // Imports a table's labels for a segmentation's cells, unless done already; recorded in the
  // project. Answers the manifest.
  const importingLabels = new Map(); // request JSON → { dataset, segmentation, job, promise }
  function importLabels(item, datasetId, request) {
    const key = JSON.stringify(request);
    if (importingLabels.has(key)) return importingLabels.get(key).promise;
    const entry = { dataset: datasetId, segmentation: request.segmentation, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/labels/import`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the import ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addLabels(app.store.project, labelsRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Import labels into ${dataset.name}`);
        return manifest;
      } finally {
        importingLabels.delete(key);
        notify();
      }
    })();
    importingLabels.set(key, entry);
    notify();
    return entry.promise;
  }

  // A reference read and described by the program against a clustering's panel (request { path,
  // clustering, matrix, labelColumn, geneColumn, parameters }): { description, preview, error }.
  function readReference(item, request) {
    return getJSON(`${item.url}/references/read`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
  }

  // Labels a clustering's cells from a reference, unless done already; recorded in the project.
  // Answers the manifest.
  const mappingReference = new Map(); // request JSON → { dataset, clustering, job, promise }
  function mapReference(item, datasetId, request) {
    const key = JSON.stringify(request);
    if (mappingReference.has(key)) return mappingReference.get(key).promise;
    const entry = { dataset: datasetId, clustering: request.clustering, job: null, promise: null };
    entry.promise = (async () => {
      try {
        const answer = await getJSON(`${item.url}/references/map`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
        let manifest = answer.artifact;
        if (!manifest) {
          entry.job = answer.job?.id ?? null;
          notify();
          const job = await app.jobs.finished(entry.job);
          if (job?.state !== 'complete') throw new Error(`the mapping ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
          manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
        }
        const dataset = app.store.project.datasets.find((d) => d.id === datasetId);
        if (!dataset) throw new Error('the dataset is no longer in the project');
        const next = addLabels(app.store.project, labelsRecord(dataset, manifest));
        if (next !== app.store.project) app.store.commit(next, `Label the cells of ${dataset.name} from a reference`);
        return manifest;
      } finally {
        mappingReference.delete(key);
        notify();
      }
    })();
    mappingReference.set(key, entry);
    notify();
    return entry.promise;
  }

  // Two label sets read through a comparison (api_impact.go): { a, b, impact, pairs, sensitiveA,
  // sensitiveB }; request { a, b }, each lib/impact.js's sideOf a label source.
  function comparisonImpact(item, comparison, request) {
    return getJSON(`${item.url}/comparisons/${encodeURIComponent(comparison)}/impact`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
  }

  // A label set's description, marker set and cells, kept for the session.
  const labelSets = new Map();
  function labelData(id) {
    if (!labelSets.has(id)) {
      const p = (async () => {
        const [set, response] = await Promise.all([getJSON(`/api/labels/${encodeURIComponent(id)}`), fetch(`/api/labels/${encodeURIComponent(id)}/cells`)]);
        if (!response.ok) throw new Error(`the labels' cells: HTTP ${response.status}`);
        return { description: set.description, markers: set.markers, cells: readLabelCells(await response.arrayBuffer()) };
      })();
      p.catch(() => labelSets.delete(id));
      labelSets.set(id, p);
    }
    return labelSets.get(id);
  }

  // A label's (or state's) score in each of a segmentation's cells (NaN where not labeled).
  async function labelScores(id, set) {
    const response = await fetch(`/api/labels/${encodeURIComponent(id)}/scores?set=${set}`);
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error ?? `HTTP ${response.status}`);
    }
    return { name: response.headers.get('X-Label') ?? '', values: new Float32Array(await response.arrayBuffer()) };
  }

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
    refinementPresets,
    refinementPreflight,
    importSegmentation,
    checkImage,
    addImage,
    addingImages: (datasetId) => [...addingImages.values()].filter((x) => x.dataset === datasetId),
    imageDescription,
    makeTransform,
    transformDescription,
    registerImage,
    aligning: (imageId) => [...aligning.values()].filter((x) => x.image === imageId),
    inverseField,
    checkAlignment,
    checking: (transformId) => [...checking.values()].filter((x) => x.transform === transformId),
    checkDescription,
    compareSegmentations,
    comparisonDescription,
    removeComparison,
    clusterCells,
    clusteringData,
    clusteringMarkers,
    makeMap,
    mapData,
    instrumentAnalysis,
    registerInstrumentClusters,
    expression,
    dotPlot,
    contrast,
    markerStarters,
    checkMarkers,
    labelCells,
    labelData,
    labelScores,
    readLabelsTable,
    importLabels,
    readReference,
    mapReference,
    comparisonImpact,
    // The mappings to a reference of a dataset under way: [{ clustering, job }].
    referenceMappings: (id) => [...mappingReference.values()].filter((e) => e.dataset === id).map(({ clustering, job }) => ({ clustering, job })),
    // The label imports into a dataset under way: [{ segmentation, job }].
    labelImports: (id) => [...importingLabels.values()].filter((e) => e.dataset === id).map(({ segmentation, job }) => ({ segmentation, job })),
    // The labelings of a dataset under way: [{ clustering, job }].
    labelings: (id) => [...labeling.values()].filter((e) => e.dataset === id).map(({ clustering, job }) => ({ clustering, job })),
    // The maps under way of a dataset: [{ clustering, job }].
    mappings: (id) => [...mapping.values()].filter((e) => e.dataset === id).map(({ clustering, job }) => ({ clustering, job })),
    // The instrument's clusterings being registered: [{ name, job }].
    registeringAnalysis: (id) => [...registeringAnalysis.values()].filter((e) => e.dataset === id).map(({ name, job }) => ({ name, job })),
    // The clusterings of a dataset under way: [{ segmentation, job }].
    clusterings: (id) => [...clustering.values()].filter((e) => e.dataset === id).map(({ segmentation, job }) => ({ segmentation, job })),
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
