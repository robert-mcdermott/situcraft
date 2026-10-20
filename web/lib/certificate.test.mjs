import assert from 'node:assert/strict';
import test from 'node:test';
import { SUMMARY_MANIFEST, at, project } from './record.fixture.mjs';
import { resultRecord, scopeRoots } from './record.js';
import { methodsText } from './methods-text.js';
import { sha256 } from './sha256.js';
import { readZip } from './zip.js';
import { readFileSync, writeFileSync } from 'node:fs';
import { CERTIFICATE_NAME, FILES_NAME, README_NAME, buildCertificate, certificateFingerprint, readCertificate, redactPaths, shortFingerprint, writeCertificate } from './certificate.js';

const decode = (bytes) => new TextDecoder().decode(bytes);

// The fixture with this computer's paths, a manifest for each of SituCraft's steps (its files by
// SHA-256; the instrument's segmentation reading its bundle's files), and its expression
// comparison certified.
function certified(edit = (p) => p) {
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
    manifests.set(a.id, { format: 'situcraft-artifact', version: 1, id: a.id, kind: a.kind, name: a.name, state: 'complete', created: a.created, lineage: { ...(a.id === 'coh-1' ? SUMMARY_MANIFEST.lineage : { parents: [] }), job: { id: `job-${a.id}`, kind: a.kind, spec }, method: a.method, parameters: a.parameters, software: a.software }, files, summary: a.summary ?? null });
  }
  const { roots, subject } = scopeRoots(p, { kind: 'result', id: 'exp-1' });
  const record = resultRecord(p, roots, { manifests, program: { version: '0.8.0', commit: 'abcdef1234567' }, subject });
  const methods = methodsText(p, record);
  return { p, manifests, record, methods, built: buildCertificate(p, record, methods, { manifests, software: { version: '0.8.0', commit: 'abcdef1234567' } }) };
}

test('a certificate names the bundles by their files\' SHA-256 and each step by its job, files or digest, with no path of this computer', async () => {
  const { record, built } = certified();
  const c = built.certificate;
  assert.equal(c.format, 'situcraft-certificate');
  assert.deepEqual(c.subject, { id: 'exp-1', kind: 'expression', name: 'Expression compared: treated − control' });
  assert.deepEqual(c.software, { name: 'SituCraft', version: '0.8.0', commit: 'abcdef1234567' });
  assert.deepEqual(c.bundles.map((b) => b.dataset), ['d-1', 'd-2']);
  // A bundle's files: its fingerprint's and those its instrument segmentation read.
  assert.deepEqual(c.bundles[0].files.map((f) => f.path), ['cells.zarr.zip', 'experiment.xenium']);
  assert.equal(c.steps.length, record.steps.length);
  const step = (id) => c.steps.find((s) => s.id === id);
  assert.equal(step('d-1').by, 'instrument');
  assert.equal(step('clu-1').by, 'program');
  assert.equal(step('clu-1').manifest.path, 'artifacts/clu-1/manifest.json');
  assert.deepEqual(step('clu-1').files.map((f) => f.path), ['clustering.json']);
  assert.match(step('clu-1').job.spec, /^[0-9a-f]{64}$/);
  assert.equal(step('ms-1').by, 'page');
  assert.match(step('ms-1').record, /^[0-9a-f]{64}$/);
  // The numbers each reports.
  assert.deepEqual(step('clu-1').summary, { cells: 91, clusters: 5 });
  // What is not made again, and why.
  assert.deepEqual(c.notMadeAgain.map((x) => x.what).slice(0, 3), ['S01', 'S02', 'Instrument segmentation']);
  // No path of this computer, anywhere: a bundle's folder stood in for by its dataset.
  const zip = await writeCertificate(built);
  const files = await readZip(zip);
  for (const [name, bytes] of files) assert.doesNotMatch(decode(bytes), /\/Users\/someone/, name);
  assert.match(decode(files.get('artifacts/clu-1/manifest.json')), /"bundle": "situcraft:bundle\/d-1"/);
  assert.match(decode(files.get('project.json')), /"path": "situcraft:bundle\/d-1"/);
  assert.deepEqual(redactPaths({ a: '/Users/someone/data/S01/outs/x.tif', b: 'C:\\images\\he.tif', c: 'relative/name.csv', d: 'Added S01 (/Users/someone/data/S01), then /home/someone/he.tif', e: 'segments 3/4', keep: '/////w==', codes: 'AAAA/Users/AAAA+/w==' }, { datasets: [{ id: 'd-1', path: '/Users/someone/data/S01' }] }),
    { a: 'situcraft:bundle/d-1/outs/x.tif', b: 'situcraft:file/he.tif', c: 'relative/name.csv', d: 'Added S01 (situcraft:bundle/d-1), then situcraft:file/he.tif', e: 'segments 3/4', keep: '/////w==', codes: 'AAAA/Users/AAAA+/w==' }, 'base64 text is not a path, even starting with a slash');
});

test('the zip: its README, the certificate and every file it names, the same bytes again; read back, an edit found', async () => {
  const { built } = certified();
  const zip = await writeCertificate(built);
  assert.deepEqual(await writeCertificate(certified().built), zip, 'the same project, the same bytes');
  const files = await readZip(zip);
  assert.deepEqual([...files.keys()], [README_NAME, CERTIFICATE_NAME, ...[...files.keys()].slice(2, -1), FILES_NAME]);
  for (const name of ['project.json', 'record.json', 'record.md', 'methods.md', 'references.bib', 'artifacts/coh-1/manifest.json']) assert.ok(files.has(name), name);
  const c = JSON.parse(decode(files.get(CERTIFICATE_NAME)));
  assert.equal(certificateFingerprint(c), c.fingerprint);
  assert.equal(sha256(files.get('methods.md')), c.methods.sha256);
  const listed = JSON.parse(decode(files.get(FILES_NAME))).files;
  assert.equal(listed.length, files.size - 1);
  for (const f of listed) assert.equal(sha256(files.get(f.path)), f.sha256, f.path);
  const readmeText = decode(files.get(README_NAME));
  assert.match(readmeText, /^SituCraft certificate\n=+\n\nOf: +Expression compared: treated − control \(expression, exp-1\)\n/);
  assert.match(readmeText, new RegExp(`Fingerprint:  ${c.fingerprint}\\n +\\(short form ${shortFingerprint(c.fingerprint)};`));
  assert.match(readmeText, /situcraft verify <this file> --data <a folder holding the bundles>/);
  assert.match(readmeText, /Exit status: 0 verified, 1 something differs \(listed\), 2 it could not be checked/);
  // Read back: intact; then a number edited, found.
  const read = await readCertificate(zip);
  assert.deepEqual(read.problems, []);
  const edited = JSON.parse(decode(files.get(CERTIFICATE_NAME)));
  edited.steps.find((s) => s.id === 'clu-1').files[0].sha256 = '0'.repeat(64);
  const { createZip } = await import('./zip.js');
  const tampered = await createZip([...files].map(([name, data]) => ({ name, data: name === CERTIFICATE_NAME ? `${JSON.stringify(edited)}\n` : data })));
  assert.deepEqual((await readCertificate(tampered)).problems, ['certificate.json is not as certified: its fingerprint does not match its content.']);
  await assert.rejects(() => readCertificate(new TextEncoder().encode('cell_id,label')), /not a SituCraft certificate/);
});

test('a different result, step or SituCraft, a different fingerprint; one not complete, or not in the store, refused', () => {
  const base = certified().built.certificate.fingerprint;
  assert.notEqual(certified((p) => { p.artifacts.find((a) => a.id === 'clu-1').parameters.resolution = 2; return p; }).built.certificate.fingerprint, base);
  const { p, manifests, record, methods } = certified();
  assert.notEqual(buildCertificate(p, record, methods, { manifests, software: { version: '0.8.1' } }).certificate.fingerprint, base);
  const running = new Map(manifests);
  running.set('gra-1', { ...manifests.get('gra-1'), state: 'running' });
  assert.throws(() => buildCertificate(p, record, methods, { manifests: running }), /is running: only complete results are certified/);
  const gone = new Map(manifests);
  gone.delete('lab-2');
  assert.throws(() => buildCertificate(p, record, methods, { manifests: gone }), /is not in SituCraft's store of results/);
  // The project's certificate, its subject the project.
  const all = scopeRoots(p, { kind: 'project' });
  const projectRecord = resultRecord(p, all.roots, { manifests, program: { version: '0.8.0' }, subject: all.subject });
  const c = buildCertificate(p, projectRecord, methodsText(p, projectRecord, { scope: 'project' }), { manifests }).certificate;
  assert.deepEqual(c.subject, { id: p.id, kind: 'project', name: 'Record' });
  assert.ok(c.steps.some((s) => s.id === 'spa-1') && c.steps.some((s) => s.id === 'exp-1'));
  assert.ok(at(0));
});

// The certificate the program's verify reads in its own tests (testdata/certificate.zip, verify_test.go):
// what this page writes for the fixture, entry by entry (deflate's bytes are the platform's). Run
// with SITUCRAFT_WRITE_FIXTURES=1 to write it again.
test('the certificate the program reads in its tests is the one this page writes', async () => {
  const zip = await writeCertificate(certified().built);
  const path = new URL('../../testdata/certificate.zip', import.meta.url);
  if (process.env.SITUCRAFT_WRITE_FIXTURES) writeFileSync(path, zip);
  const fixture = await readZip(new Uint8Array(readFileSync(path)));
  const made = await readZip(zip);
  assert.deepEqual([...fixture.keys()], [...made.keys()]);
  for (const [name, bytes] of made) assert.deepEqual(fixture.get(name), bytes, name);
});
