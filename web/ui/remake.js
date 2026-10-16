// A clustering and its label sets made again on another segmentation of the dataset, in one step
// (lib/impact.js's remakePlan): the clustering with the same parameters, its map, and each label
// set from markers (the same marker set version and parameters) or from a reference (the same
// file, columns and parameters), each a job as when made by hand. While it runs, remaking(id)
// says how far it has come.

import { remakePlan } from '../lib/impact.js';

const under = new Map(); // `${clustering}|${segmentation}` → { step, promise }

// The remakes under way of a clustering: [{ segmentation, step }].
export function remaking(clusteringId) {
  return [...under.entries()].filter(([k]) => k.startsWith(`${clusteringId}|`)).map(([k, v]) => ({ segmentation: k.split('|')[1], step: v.step }));
}

// Answers { clustering (the manifest), labels: [manifests], failed: [{ id, error }], skipped,
// notes }; an error when nothing can be made.
export function remakeOnSegmentation(app, t, clusteringId, segmentationId, notify = () => {}) {
  const key = `${clusteringId}|${segmentationId}`;
  if (under.has(key)) return under.get(key).promise;
  const plan = remakePlan(app.store.project, clusteringId, segmentationId);
  if (plan.problem) return Promise.reject(new Error(plan.problem));
  const entry = { step: 'Clustering the cells', promise: null };
  const step = (text) => {
    entry.step = text;
    notify();
  };
  entry.promise = (async () => {
    try {
      const clustering = await app.bundles.clusterCells(t.item, t.dataset.id, { segmentation: segmentationId, parameters: plan.parameters });
      step('Making the map');
      const map = app.bundles.makeMap(t.item, t.dataset.id, clustering.id).catch(() => null);
      const labels = [];
      const failed = [];
      for (const [k, l] of plan.labels.entries()) {
        step(`Labeling the cells (${k + 1} of ${plan.labels.length})`);
        try {
          if (l.kind === 'markers') {
            labels.push(await app.bundles.labelCells(t.item, t.dataset.id, { clustering: clustering.id, markers: l.set, parameters: l.parameters }, l.markerSet));
          } else {
            const ref = (await app.bundles.labelData(l.record.id)).description.reference;
            const m = await app.bundles.mapReference(t.item, t.dataset.id, { path: ref.path, matrix: ref.matrix ?? '', labelColumn: ref.labelColumn ?? '', geneColumn: ref.geneColumn ?? '', clustering: clustering.id, parameters: l.parameters });
            const sha = m.lineage?.parameters?.reference?.sha256;
            if (sha && sha !== ref.sha256) failed.push({ id: l.record.id, error: `${ref.file} changed since it was first read (SHA-256 ${String(ref.sha256).slice(0, 12)}, now ${sha.slice(0, 12)}): the labels were made from it as it is now` });
            labels.push(m);
          }
        } catch (error) {
          failed.push({ id: l.record.id, error: error.message });
        }
      }
      await map;
      return { clustering, labels, failed, skipped: plan.skipped, notes: plan.notes };
    } finally {
      under.delete(key);
      notify();
    }
  })();
  under.set(key, entry);
  notify();
  return entry.promise;
}
