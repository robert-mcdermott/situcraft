// Post-Xenium images in the project (api_images.go): an H&E or immunofluorescence image of a
// dataset's section, read where it lies, recorded as an artifact of kind "image" with the image by
// its SHA-256. An image is placed by a transform (lib/transforms.js): the one the project's latest
// decision chooses among its versions, else none (it is not aligned, and not drawn on the tissue).

import { transformInUse } from './transforms.js';

export function imageRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const summary = manifest.summary ?? {};
  const file = (role) => (manifest.files ?? []).find((f) => f.role === role) ?? null;
  return {
    kind: 'image',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: lineage.method?.name ?? '', version: lineage.method?.version ?? '', citation: lineage.method?.citation ?? '' },
    software: lineage.software ?? [],
    job: lineage.job ? { id: lineage.job.id, kind: lineage.job.kind } : null,
    files: (manifest.files ?? []).map(({ path, role, size, sha256, location }) => ({ path, role, size, sha256, location: location ?? '' })),
    image: file('image')?.path ?? null,
    summary,
    findings: manifest.findings ?? [],
  };
}

export function imagesOf(project, datasetId) {
  return project.artifacts.filter((a) => a.kind === 'image' && a.dataset === datasetId && a.state === 'complete');
}

// The dataset's images, each with the transform placing it (null when it is not aligned).
export function placedImages(project, datasetId) {
  return imagesOf(project, datasetId).map((record) => ({ record, transform: transformInUse(project, record.id)?.record ?? null }));
}

// The project with an image recorded (unchanged when it already is).
export function addImage(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'image' && a.id === record.id && a.dataset === record.dataset)) return project;
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'image', detail: `Added ${record.name} to ${dataset?.name ?? record.dataset}, read where it lies (artifact ${record.id}, by SHA-256)` }],
  };
}

// What an image is, in a line: "RGB, 30,786 × 24,241, 7 levels, deflate".
export function describeImage(record) {
  const s = record.summary ?? {};
  const what = s.kind === 'rgb' ? 'RGB' : `${(s.channels ?? []).length} channels (${(s.channels ?? []).map((c) => c.name).join(', ')}), ${s.bits} bits`;
  const size = Number.isFinite(s.width) ? `${s.width.toLocaleString('en-US')} × ${s.height.toLocaleString('en-US')}` : '';
  return [what, size, s.levels ? `${s.levels} levels` : '', s.compression ?? ''].filter(Boolean).join(', ');
}

// How a transform places its image, in plain words: "0.3639 µm per pixel, turned 0.3°, mirrored".
export function describePlacement(summary) {
  if (!summary || !Number.isFinite(summary.umPerPixel)) return '';
  const turn = Math.abs(summary.rotation) < 0.05 ? '' : `, turned ${summary.rotation.toFixed(1)}°`;
  return `${summary.umPerPixel.toFixed(4)} µm per pixel${turn}${summary.reflected ? ', mirrored' : ''}`;
}
