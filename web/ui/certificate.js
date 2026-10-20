// Certificates (wave 8, slice 5) as the page writes them: the record of the project, a dataset or a
// result (its artifacts' manifests read from the program), its methods, and each figure's drawing
// (drawn again, its manifest's digests), certified by lib/certificate.js as a zip.

import { buildCertificate, writeCertificate } from '../lib/certificate.js';
import { recordAndMethods } from './methods.js';
import { writeFigure } from './figures.js';

// A figure's manifest has a time; its drawing's digests do not. Drawn for a certificate at this one.
const DRAWN_AT = '2026-01-01T00:00:00.000Z';

// The certificate's file name: "certificate-<subject>-<short fingerprint>.zip".
export function certificateFileName(certificate) {
  const base = certificate.subject.name.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 60) || certificate.subject.id;
  return `certificate-${base}-${certificate.fingerprint.slice(0, 8)}.zip`;
}

// The certificate of a scope ({ kind: 'project' }, { kind: 'dataset', id } or { kind: 'result',
// id }): { bytes (the zip), certificate, name }.
export async function certify(app, scope) {
  const { project, manifests, record, methods } = await recordAndMethods(app, scope);
  const figures = new Map();
  for (const s of record.steps.filter((x) => x.kind === 'figure')) {
    const fig = (project.figures ?? []).find((f) => f.id === s.id);
    const out = await writeFigure(app, fig, 'json', { dpi: 300, time: DRAWN_AT });
    figures.set(fig.id, { drawing: out.manifest.drawing, panels: out.manifest.panels.map((p) => ({ id: p.id, letter: p.letter, drawing: p.drawing })) });
  }
  // The content of each step whose files also record how long its run took.
  const contents = new Map();
  for (const s of record.steps.filter((x) => ['clustering', 'embedding', 'labels'].includes(x.kind) && manifests.get(x.id))) {
    const response = await fetch(`/api/artifacts/${encodeURIComponent(s.id)}/digest`);
    const content = response.ok ? (await response.json()).content : null;
    if (content) contents.set(s.id, content);
  }
  const built = buildCertificate(project, record, methods, { manifests, contents, figures, software: { version: app.version ?? null, commit: app.commit ?? null } });
  return { bytes: await writeCertificate(built), certificate: built.certificate, name: certificateFileName(built.certificate) };
}
