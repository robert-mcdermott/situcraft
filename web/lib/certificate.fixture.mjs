// The certificate the tests read (wave 8, slices 5 to 7): the record's fixture (record.fixture.mjs)
// with this computer's paths and a manifest for each of SituCraft's steps, certified.

import { SUMMARY_MANIFEST, project } from './record.fixture.mjs';
import { resultRecord, scopeRoots } from './record.js';
import { methodsText } from './methods-text.js';
import { buildCertificate } from './certificate.js';
import { sha256 } from './sha256.js';

// The fixture with this computer's paths, a manifest for each of SituCraft's steps (its files by
// SHA-256; the instrument's segmentation reading its bundle's files), and its expression
// comparison certified.
export function certified(edit = (p) => p, { scope = { kind: 'result', id: 'exp-1' }, figures = new Map() } = {}) {
  const p = edit(project());
  p.datasets[0].path = '/Users/someone/data/S01';
  p.datasets[1].path = '/Users/someone/data/S02';
  p.datasets[0].fingerprint = [{ path: 'experiment.xenium', size: 1673, sha256: 'e'.repeat(64) }];
  const manifests = new Map();
  for (const a of p.artifacts.filter((x) => x.registered)) {
    const files = a.kind === 'segmentation'
      ? [{ path: 'cells.zarr.zip', role: 'cells', size: 10, sha256: sha256(new TextEncoder().encode(`cells ${a.id}`)), location: 'bundle' }]
      : [{ path: `${a.kind}.json`, role: a.kind, size: 20, sha256: sha256(new TextEncoder().encode(a.id)), location: '' }];
    const spec = a.id === 'coh-1' ? SUMMARY_MANIFEST.lineage.job.spec : { bundle: p.datasets.find((d) => d.id === a.dataset)?.path ?? null, id: a.id };
    manifests.set(a.id, { format: 'situcraft-artifact', version: 1, id: a.id, kind: a.kind, name: a.name, state: 'complete', created: a.created, lineage: { ...(a.id === 'coh-1' ? SUMMARY_MANIFEST.lineage : { parents: [] }), job: { id: `job-${a.id}`, kind: a.kind, spec }, method: a.method, parameters: a.parameters, software: a.software }, files, summary: a.summaryOf ?? a.summary ?? null });
  }
  const { roots, subject } = scopeRoots(p, scope);
  const record = resultRecord(p, roots, { manifests, program: { version: '0.8.0', commit: 'abcdef1234567' }, subject });
  const methods = methodsText(p, record);
  return { p, manifests, record, methods, built: buildCertificate(p, record, methods, { manifests, figures, software: { version: '0.8.0', commit: 'abcdef1234567' } }) };
}
