// Post-Xenium images in the project (api_images.go): an H&E or immunofluorescence image of a
// dataset's section, read where it lies and placed by its alignment, recorded as an artifact of
// kind "image" with the image and its alignment file by their SHA-256.

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
    alignment: file('alignment')?.path ?? null,
    summary,
    findings: manifest.findings ?? [],
  };
}

export function imagesOf(project, datasetId) {
  return project.artifacts.filter((a) => a.kind === 'image' && a.dataset === datasetId && a.state === 'complete');
}

// The project with an image recorded (unchanged when it already is).
export function addImage(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'image' && a.id === record.id && a.dataset === record.dataset)) return project;
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  const s = record.summary ?? {};
  const how = s.alignment?.kind === 'keypoints' ? `its ${s.alignment.keypoints} keypoints` : 'its transformation matrix';
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'image', detail: `Added ${record.name} to ${dataset?.name ?? record.dataset}, aligned by ${how} (${Number.isFinite(s.umPerPixel) ? `${s.umPerPixel.toFixed(4)} µm per pixel` : 'its scale unknown'}; artifact ${record.id})` }],
  };
}

// What an image is, in a line: "RGB, 30,786 × 24,241, 7 levels, deflate; 0.3639 µm per pixel".
export function describeImage(record) {
  const s = record.summary ?? {};
  const what = s.kind === 'rgb' ? 'RGB' : `${(s.channels ?? []).length} channels (${(s.channels ?? []).map((c) => c.name).join(', ')}), ${s.bits} bits`;
  const size = Number.isFinite(s.width) ? `${s.width.toLocaleString('en-US')} × ${s.height.toLocaleString('en-US')}` : '';
  return [what, size, s.levels ? `${s.levels} levels` : '', s.compression ?? ''].filter(Boolean).join(', ') + (Number.isFinite(s.umPerPixel) ? `; ${s.umPerPixel.toFixed(4)} µm per pixel` : '');
}

// How an image is aligned, in plain words.
export function describeAlignment(summary) {
  const a = summary?.alignment;
  if (!a) return '';
  const turn = Math.abs(a.rotation) < 0.05 ? '' : `, turned ${a.rotation.toFixed(1)}°`;
  const by = a.kind === 'keypoints' ? `${a.keypoints} keypoints (residuals ${a.rmsResidual.toFixed(1)} pixels root-mean-square, at most ${a.maxResidual.toFixed(1)})` : 'its transformation matrix';
  return `Aligned by ${by}: scaled ${a.scale.toFixed(4)}${turn}${a.reflected ? ', mirrored' : ''}.`;
}
