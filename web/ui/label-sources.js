// Any label set's labels per segmentation cell, for the map, the tissue, the Viewer and the review:
// a set from markers or imported (its artifact, at a level), a curated set (its base with its edits)
// or a consensus set (its members by its rule). Answers { kind, id, name, names, colors, label
// (Int32Array over the segmentation's cells, −1 for none), analyzed (Uint8Array: the cells the set
// covers), confidence (Float32Array, NaN for none, or null), segCells, record }.

import { categoricalColor } from '../lib/colormaps.js';
import { applyCurated, consensusOf } from '../lib/curation.js';
import { labelSetName, oneLevel } from '../lib/labels.js';

// A set's reference from its id: 'lab-…' a program's label set (at a level), 'cur-…' curated,
// 'con-…' consensus.
export function refOf(id, level = 0) {
  if (String(id).startsWith('cur-')) return { kind: 'curated', id };
  if (String(id).startsWith('con-')) return { kind: 'consensus', id };
  return { kind: 'labels', id, level };
}

export async function labelSource(app, ref, seen = new Set()) {
  if (seen.has(ref.id)) throw new Error('a label set made of itself');
  seen.add(ref.id);
  const project = app.store.project;
  if (ref.kind === 'labels') {
    const data = await app.bundles.labelData(ref.id);
    const cells = data.cells;
    const d = data.description;
    const level = Math.min(ref.level ?? 0, cells.levels - 1);
    const label = new Int32Array(cells.segCells).fill(-1);
    const analyzed = new Uint8Array(cells.segCells);
    const confidence = new Float32Array(cells.segCells).fill(NaN);
    for (let i = 0; i < cells.n; i += 1) {
      const c = cells.cells[i];
      label[c] = cells.label[level][i];
      analyzed[c] = 1;
      confidence[c] = cells.confidence[i];
    }
    const record = project.artifacts.find((a) => a.kind === 'labels' && a.id === ref.id) ?? null;
    const name = record ? (oneLevel(record) ? labelSetName(record) : `${labelSetName(record)}: ${String(d.levels[level]).toLowerCase()}`) : ref.id;
    return { kind: 'labels', id: ref.id, level, name, names: d.sets.map((s) => s.name), colors: d.sets.map((s, k) => s.color || categoricalColor(k)), label, analyzed, confidence, segCells: cells.segCells, record, description: d };
  }
  if (ref.kind === 'curated') {
    const record = project.artifacts.find((a) => a.kind === 'curated-labels' && a.id === ref.id);
    if (!record) throw new Error(`no curated label set ${ref.id}`);
    const base = await labelSource(app, refOf(record.base.labels, record.base.level), seen);
    const clusters = new Map();
    for (const e of record.edits) {
      if (e.kind === 'cluster' && !clusters.has(e.clustering)) clusters.set(e.clustering, (await app.bundles.clusteringData(e.clustering)).cluster);
    }
    const out = applyCurated(record, base, (id) => clusters.get(id) ?? null);
    const analyzed = Uint8Array.from(base.analyzed);
    for (let i = 0; i < out.label.length; i += 1) if (out.label[i] >= 0) analyzed[i] = 1;
    return { kind: 'curated', id: ref.id, name: record.name, names: out.names, colors: out.colors, label: out.label, analyzed, confidence: base.confidence, edited: out.edited, segCells: base.segCells, record, base };
  }
  const record = project.artifacts.find((a) => a.kind === 'consensus-labels' && a.id === ref.id);
  if (!record) throw new Error(`no consensus label set ${ref.id}`);
  const members = [];
  for (const m of record.members) members.push(await labelSource(app, m.ref, new Set(seen)));
  const segCells = members[0].segCells;
  if (members.some((m) => m.segCells !== segCells)) throw new Error('the consensus\'s sets are of different segmentations');
  const out = consensusOf(record.rule, members, record.mapping);
  const colors = out.names.map((name, k) => {
    for (const m of members) {
      const j = m.names.indexOf(name);
      if (j >= 0) return m.colors[j];
    }
    return categoricalColor(k);
  });
  const analyzed = new Uint8Array(segCells);
  const confidence = new Float32Array(segCells).fill(NaN);
  for (let i = 0; i < segCells; i += 1) {
    if (out.voters[i]) analyzed[i] = 1;
    if (out.label[i] >= 0) confidence[i] = out.votes[i] / members.length;
  }
  return { kind: 'consensus', id: ref.id, name: record.name, names: out.names, colors, label: out.label, analyzed, confidence, votes: out.votes, voters: out.voters, segCells, record, members };
}

// Per segmentation cell, the label a set gives (−1 for a cell it does not cover, unlabeled for one
// it covers without a label), for coloring.
export function perCell(src, unlabeled) {
  const out = new Int32Array(src.segCells).fill(-1);
  for (let i = 0; i < src.segCells; i += 1) {
    if (src.label[i] >= 0) out[i] = src.label[i];
    else if (src.analyzed[i]) out[i] = unlabeled;
  }
  return out;
}

// The label sets of a segmentation the page can draw, each { id, name, kind }: the program's
// (every level of a set from markers), the curated and the consensus.
export function labelVersions(project, datasetId, segmentationId) {
  const out = [];
  for (const r of project.artifacts.filter((a) => a.kind === 'labels' && a.dataset === datasetId && a.segmentation === segmentationId && a.state === 'complete')) {
    (r.summary?.levels ?? ['']).forEach((level, l) => out.push({ id: r.id, level: l, kind: r.source ? 'imported' : r.reference ? 'reference' : 'markers', name: oneLevel(r) ? labelSetName(r) : `${labelSetName(r)}: ${String(level).toLowerCase()}` }));
  }
  for (const r of project.artifacts.filter((a) => a.kind === 'curated-labels' && a.dataset === datasetId && a.segmentation === segmentationId)) out.push({ id: r.id, level: 0, kind: 'curated', name: r.name });
  for (const r of project.artifacts.filter((a) => a.kind === 'consensus-labels' && a.dataset === datasetId && a.segmentation === segmentationId)) out.push({ id: r.id, level: 0, kind: 'consensus', name: r.name });
  return out;
}
