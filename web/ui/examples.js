// The example datasets (api_examples.go): listed with their size, source and license, downloaded
// as a job when asked (its progress in the job list), opened like any bundle, toured, removed.

import { openPaths } from './storage.js';
import { confirmDialog, toast } from './overlays.js';
import { exampleFor } from '../lib/examples.js';
import { guideFor, tourFor } from '../lib/tours.js';
import { startTour } from './tour.js';
import { samplesOf, setSampleField } from '../lib/samples.js';

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
      app.log(`${example?.generated ? 'Making' : 'Downloading'} ${example?.title ?? name} into ${dir}.`);
    } catch (e) {
      toast(e.message, { kind: 'error' });
    }
    notify();
  }

  async function remove(name) {
    const example = byName(name);
    if (!example) return;
    if (!(await confirmDialog({ title: 'Remove the example?', message: `Delete ${example.title} from ${example.path}? It can be ${example.generated ? 'made' : 'downloaded'} again. Projects using it will find it missing.`, confirm: 'Remove', danger: true }))) return;
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
    if (example?.state !== 'ready') throw new Error(example?.generated ? 'It is not made yet.' : 'It is not downloaded.');
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
      if (byName(name)?.kind === 'cohort') {
        await inProject(name);
        app.setMode('cohort');
        return;
      }
      const it = await item(name);
      app.focusItem({ kind: 'local', id: it.url });
      app.setMode('data');
    } catch (e) {
      toast(e.message, { kind: 'error' });
    }
  }

  // A simulated cohort's samples (situcraft-cohort.json) and their bundles as local items, in the
  // cohort's order: [{ sample, item }].
  async function cohortItems(example) {
    const sep = example.path.includes('\\') && !example.path.includes('/') ? '\\' : '/';
    const doc = await getJSON(`/api/read-json?path=${encodeURIComponent(`${example.path}${sep}situcraft-cohort.json`)}`);
    const paths = doc.samples.map((s) => `${example.path}${sep}${s.name}`);
    // The samples' folders, not the cohort's (which would list its table too).
    if (!paths.every((p) => app.localItems.some((i) => i.path === p))) {
      const answer = await openPaths(paths);
      app.setLocalItems(answer.files);
    }
    return doc.samples.map((sample, k) => {
      const found = app.localItems.find((i) => i.path === paths[k]);
      if (!found) throw new Error(`${sample.name}'s bundle is not in ${example.path}.`);
      return { sample, item: found };
    });
  }

  // A simulated cohort in this project if it has its samples (or is empty), else in a new project
  // named after it: each sample's bundle added, and its subject and condition (its group) set.
  async function cohortInProject(example) {
    const entries = await cohortItems(example);
    const missing = entries.filter((e) => !app.bundles.datasetFor(e.item));
    if (missing.length && app.store.project.datasets.length && missing.length === entries.length) await app.newProject();
    // Named after the cohort, not its first sample.
    if (!app.store.project.datasets.length && app.store.project.name === 'Untitled project') app.store.commit({ ...app.store.project, name: `${example.title} example` }, 'Name project');
    const datasets = [];
    for (const e of entries) datasets.push(app.bundles.datasetFor(e.item) ?? (await app.bundles.add(e.item)));
    // Each sample's subject and condition, unless already said.
    let project = app.store.project;
    let changed = false;
    for (const [k, e] of entries.entries()) {
      const sample = samplesOf(project).find((s) => s.dataset.id === datasets[k].id && !s.region);
      if (!sample) continue;
      for (const [field, value] of [['subject', e.sample.subject], ['condition', e.sample.group]]) {
        if (sample.fields[field] === undefined && value) {
          project = setSampleField(project, sample.id, field, value);
          changed = true;
        }
      }
    }
    if (changed) app.store.commit(project, `Read ${example.title}'s subjects and conditions`);
    return { example, items: entries.map((e) => e.item), datasets, item: entries[0].item, dataset: datasets[0] };
  }

  // The example's dataset in this project if it has it (or is empty), else in a new project named
  // after it: { example, item, dataset } (a cohort's: its items and datasets too).
  async function inProject(name) {
    const example = byName(name);
    if (!example) throw new Error(`No example "${name}".`);
    if (example.kind === 'cohort') {
      if (example.state !== 'ready') throw new Error('It is not made yet.');
      return cohortInProject(example);
    }
    const it = await item(name);
    let dataset = app.bundles.datasetFor(it);
    if (!dataset) {
      if (app.store.project.datasets.length) {
        await app.newProject();
        app.store.commit({ ...app.store.project, name: `${example.title.replace(/\s*\(.*\)$/, '')} example` }, 'Name project');
      }
      dataset = await app.bundles.add(it);
    }
    return { example, item: it, dataset };
  }

  // Tours an example (lib/tours.js), or takes one of its worked examples (by id), in the project
  // inProject chooses; answers the tour's card.
  async function tour(name, which = 'tour') {
    const steps = guideFor(name, which);
    if (!byName(name) || !steps) return null;
    try {
      const { dataset } = await inProject(name);
      return startTour(app, { tour: steps, dataset: dataset.id });
    } catch (error) {
      toast(`The tour could not start: ${error.message}`, { kind: 'error' });
      return null;
    }
  }

  app.jobs.onFinished((job) => {
    if (job.kind !== 'example') return;
    refresh().then(() => {
      const example = byName(job.spec?.example);
      if (job.state === 'complete' && example) {
        toast(`${example.title} is ready.`, { kind: 'ok', action: tourFor(example.name) ? { label: 'Take the tour', onClick: () => tour(example.name) } : { label: 'Open', onClick: () => open(example.name) } });
      } else if (job.state === 'failed') {
        toast(`${example?.title ?? 'The example'} was not ${example?.generated ? 'made' : 'downloaded'}: ${job.error}`, { kind: 'error' });
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
    inProject,
    job: (name) => app.jobs.forKey(exampleJobKey(name)),
    // The example a project's dataset is, if it is one (once the list is known).
    forDataset: (dataset) => exampleFor(dataset, list),
  };
}
