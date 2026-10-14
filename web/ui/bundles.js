// Xenium output bundles on this computer, as the page sees them: the items the program holds
// (local.go), their validation reports (api_datasets.go), the project's datasets reconnected
// to their bundles when a project opens, and each dataset's derived data (api_cache.go), built
// by a job when the dataset is added or found again. Reports are kept for the session, not in the
// project: the project keeps each dataset's summary and findings (lib/datasets.js).

import { openPaths } from './storage.js';
import { addDataset, compareFingerprint, datasetRecord, findDataset, relocateDataset, removeDataset } from '../lib/datasets.js';

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
    return record;
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
      if (link.state === 'ok' || link.state === 'changed') buildCache(item);
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
