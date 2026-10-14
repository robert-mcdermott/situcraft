// The example datasets (api_examples.go): listed with their size, source and license, downloaded
// as a job when asked (its progress in the job list), opened like any bundle, toured, removed.

import { openPaths } from './storage.js';
import { confirmDialog, toast } from './overlays.js';
import { exampleFor } from '../lib/examples.js';
import { tourFor } from '../lib/tours.js';
import { startTour } from './tour.js';

export function exampleJobKey(name) {
  return `example\u0000${name}`;
}

async function getJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `${url}: HTTP ${response.status}`);
  return body;
}

export function createExamples(app) {
  let list = null;
  let dir = '';
  let error = null;
  const notify = () => app.store.notify(['examples']);

  async function refresh() {
    try {
      const body = await getJSON('/api/examples');
      list = body.examples ?? [];
      dir = body.dir ?? '';
      error = null;
    } catch (e) {
      error = e.message;
    }
    notify();
    return list;
  }

  const byName = (name) => (list ?? []).find((e) => e.name === name) ?? null;

  async function download(name) {
    const example = byName(name);
    try {
      await getJSON(`/api/examples/${encodeURIComponent(name)}`, { method: 'POST' });
      app.log(`Downloading ${example?.title ?? name} into ${dir}.`);
    } catch (e) {
      toast(e.message, { kind: 'error' });
    }
    notify();
  }

  async function remove(name) {
    const example = byName(name);
    if (!example) return;
    if (!(await confirmDialog({ title: 'Remove the example?', message: `Delete ${example.title} from ${example.path}? It can be downloaded again. Projects using it will find it missing.`, confirm: 'Remove', danger: true }))) return;
    try {
      await getJSON(`/api/examples/${encodeURIComponent(name)}`, { method: 'DELETE' });
      app.log(`Removed ${example.title}.`);
    } catch (e) {
      toast(e.message, { kind: 'error' });
    }
    await refresh();
  }

  // The example's bundle as a local item (local.go), registered if it is not yet.
  async function item(name) {
    const example = byName(name);
    if (example?.state !== 'ready') throw new Error('It is not downloaded.');
    const known = app.localItems.find((i) => i.path === example.path);
    if (known) return known;
    const answer = await openPaths([example.path]);
    app.setLocalItems(answer.files);
    const found = app.localItems.find((i) => i.path === example.path);
    if (!found) throw new Error(answer.problems?.[0] ?? `${example.path} could not be opened.`);
    return found;
  }

  async function open(name) {
    try {
      const it = await item(name);
      app.focusItem({ kind: 'local', id: it.url });
      app.setMode('data');
    } catch (e) {
      toast(e.message, { kind: 'error' });
    }
  }

  // Tours an example: in this project if it has the example's dataset (or is empty), else in a
  // new project named after it.
  async function tour(name) {
    const example = byName(name);
    const steps = tourFor(name);
    if (!example || !steps) return;
    try {
      const it = await item(name);
      let dataset = app.bundles.datasetFor(it);
      if (!dataset) {
        if (app.store.project.datasets.length) {
          await app.newProject();
          app.store.commit({ ...app.store.project, name: `${example.title.replace(/\s*\(.*\)$/, '')} example` }, 'Name project');
        }
        dataset = await app.bundles.add(it);
      }
      startTour(app, { tour: steps, dataset: dataset.id });
    } catch (e) {
      toast(`The tour could not start: ${e.message}`, { kind: 'error' });
    }
  }

  app.jobs.onFinished((job) => {
    if (job.kind !== 'example') return;
    refresh().then(() => {
      const example = byName(job.spec?.example);
      if (job.state === 'complete' && example) {
        toast(`${example.title} is ready.`, { kind: 'ok', action: tourFor(example.name) ? { label: 'Take the tour', onClick: () => tour(example.name) } : { label: 'Open', onClick: () => open(example.name) } });
      } else if (job.state === 'failed') {
        toast(`${example?.title ?? 'The example'} was not downloaded: ${job.error}`, { kind: 'error' });
      }
    });
  });

  return {
    list: () => list,
    dir: () => dir,
    error: () => error,
    refresh,
    download,
    remove,
    open,
    tour,
    job: (name) => app.jobs.forKey(exampleJobKey(name)),
    // The example a project's dataset is, if it is one (once the list is known).
    forDataset: (dataset) => exampleFor(dataset, list),
  };
}
