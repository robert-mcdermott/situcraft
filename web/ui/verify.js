// A certificate verified in SituCraft (wave 8, slice 9; api_verify.go): a certificate, a report or a
// result package sent to the program, which makes each step again in a SituCraft of its own from
// the bundles this library knows, as `situcraft verify` does, and compares every file; then what
// it found shown step by step. From the Reports view (Verify a certificate…, and a record's
// Verify), a worked example and remote control (verify_certificate).

import { h, clear } from './dom.js';
import { showDialog } from './overlays.js';

const VERDICTS = { verified: 'Verified', differs: 'Differs', 'not checkable': 'Not checkable', 'as certified': 'As certified', 'not made again': 'Not made again' };

// What the program found of a file: { file: Uint8Array | Blob, name } sent, or { path } (a file on
// this computer) named. onJob(job) is told the job as it starts. Answers verify's report: {
// verdict, bundles, steps: [{ n, name, kind, by, verdict, detail, differ }], notes, problems,
// firstDiffers, fingerprint, subject, seconds }.
export async function verifyCertificateFile(app, { file = null, name = 'certificate.zip', path = null } = {}, { onJob = () => {} } = {}) {
  const response = path
    ? await fetch('/api/verify', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) })
    : await fetch(`/api/verify?name=${encodeURIComponent(name)}`, { method: 'POST', headers: { 'Content-Type': 'application/octet-stream' }, body: file });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
  onJob(body.job);
  const job = await app.jobs.finished(body.job.id);
  if (job.state !== 'complete') throw new Error(job.error || `the verification was ${job.state}`);
  const found = await fetch(`/api/verify/${encodeURIComponent(body.job.id)}`, { cache: 'no-store' });
  if (!found.ok) throw new Error((await found.json().catch(() => ({}))).error ?? `HTTP ${found.status}`);
  return found.json();
}

// The verdict in a sentence.
export function verdictText(report) {
  const made = report.steps.filter((s) => s.by === 'program' && s.verdict === 'verified').length;
  if (report.verdict === 'verified') return `Verified: ${made} step${made === 1 ? '' : 's'} made again from the bundles, every file the same as certified, and every other step as certified.`;
  if (report.verdict === 'differs') {
    const f = report.firstDiffers;
    return `Differs${f ? `: first at step ${f.n}, ${f.name}${f.detail ? ` (${f.detail})` : ''}` : ''}.`;
  }
  const missing = report.bundles.filter((b) => b.status !== 'found').map((b) => b.name);
  return `Not checkable${missing.length ? `: ${missing.join(', ')} not found among the bundles SituCraft knows (open ${missing.length === 1 ? 'it' : 'them'} in a project, and verify again)` : ''}.`;
}

// What was found, step by step: a block of the page.
export function verificationView(report) {
  const kind = report.verdict === 'verified' ? 'ok' : report.verdict === 'differs' ? 'bad' : 'warn';
  return h('div.verification', { 'data-verdict': report.verdict },
    h(`p.verification-verdict.${kind}`, verdictText(report)),
    h('p.muted', `Of ${report.subject?.name ?? 'a result'}, certified by SituCraft ${report.certifiedBy?.version ?? '?'} and made again by ${report.verifiedBy?.version ?? '?'}; fingerprint ${String(report.fingerprint ?? '').slice(0, 16)}…`),
    ...(report.problems?.length ? [h('ul.verification-problems', ...report.problems.map((p) => h('li', p)))] : []),
    h('ul.verification-bundles', ...report.bundles.map((b) => h('li', `Bundle ${b.name}: ${b.status === 'found' ? `found, ${b.checked} files as certified` : b.status === 'differs' ? `found, but ${(b.differ ?? []).join(', ')} not as certified` : 'not found'}`))),
    h('ol.verification-steps', ...report.steps.map((s) => h('li', { value: s.n, 'data-verdict': s.verdict },
      h('span.badge', VERDICTS[s.verdict] ?? s.verdict), ' ', h('b', s.name), h('span.muted', ` (${s.kind})`),
      s.detail ? h('div.muted', s.detail) : null))),
    ...(report.notes ?? []).map((n) => h('p.muted', n)));
}

// What was found, in a dialog.
export function showVerification(report, name) {
  return showDialog({ title: `Verified: ${name}`, width: 'wide', content: [verificationView(report)], buttons: [{ label: 'Close', primary: true }] });
}

// A block that says the verification is running, then what it found.
export function verificationPending(text = 'Making each step again from the bundles…') {
  const el = h('div.verification', h('p.muted', text));
  return { element: el, show: (report) => clear(el).append(...verificationView(report).childNodes) };
}
