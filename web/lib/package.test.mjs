import assert from 'node:assert/strict';
import test from 'node:test';
import { certified } from './certificate.fixture.mjs';
import { buildCertificate, readme, writeCertificate } from './certificate.js';
import { reportHTML } from './report.js';
import { findLeftOut, leftOutStrings, redactionOf } from './redact.js';
import { packageFolder, packageManifest, packageReadme, packageTexts } from './package.js';
import { sha256 } from './sha256.js';

const encoder = new TextEncoder();

test('a certificate made for others: what it leaves out said, a step whose job was given names left out marked, and not made again from it', async () => {
  const { p, manifests, record, methods } = certified();
  const { certificate } = buildCertificate(p, record, methods, { manifests, leftOut: { people: 2, notes: 1, names: 4 }, redacted: new Set(['coh-1']) });
  assert.deepEqual(certificate.leftOut, { people: 2, notes: 1, names: 4 });
  assert.equal(certificate.steps.find((s) => s.id === 'coh-1').redacted, true);
  assert.equal(certificate.steps.filter((s) => s.redacted).length, 1);
  assert.match(certificate.notMadeAgain.find((x) => x.what === 'Summary of 2 samples' && /names/.test(x.why)).why, /cannot be made again from this certificate/);
  const text = readme(certificate);
  assert.match(text, /Left out\n--------\nMade for others to read, it leaves out 2 people's names/);
  assert.match(text, /and 4 names\nof samples, subjects and sections/);
  // Without them, a certificate is as before.
  assert.equal(buildCertificate(p, record, methods, { manifests }).certificate.leftOut, undefined);
});

test('a package\'s texts: its files\', its certificate\'s entries, and what its report carries (each figure, its certificate); what was left out found in any', async () => {
  const { built, methods } = certified();
  const zip = await writeCertificate(built);
  const svg = '<svg xmlns="http://www.w3.org/2000/svg"><text>Reviewed by Ada Lovelace</text></svg>';
  const html = reportHTML({ title: 'Results', certificate: built.certificate, zip, methods, figures: [{ id: 'fig-x', name: 'Figure', svg, description: 'A figure.' }], results: ['spa-1'], views: [{ name: 'Lung classes', href: 'views/1-lung.html', thumb: 'views/1-lung/0/0-0.jpg', description: 'Sample 1, 400 µm across.' }] });
  assert.match(html, /img-src data: blob: 'self' file:;/);
  assert.match(html, /<section id="views"><h2>Views<\/h2>/);
  assert.match(html, /<a class="view" href="views\/1-lung\.html"><img src="views\/1-lung\/0\/0-0\.jpg" alt="">/);
  const files = [
    { name: 'index.html', data: html },
    { name: 'certificate-x.zip', data: zip },
    { name: 'views/1-lung.html', data: '<p>A view</p>' },
    { name: 'views/1-lung/0/0-0.jpg', data: new Uint8Array([255, 216, 255, 224]) },
  ];
  const texts = await packageTexts(files);
  const names = texts.map((t) => t.name);
  assert.ok(names.includes('index.html: a figure'));
  assert.ok(names.includes('index.html: its certificate: certificate.json'));
  assert.ok(names.includes('certificate-x.zip: project.json'));
  assert.ok(!names.some((n) => n.endsWith('.jpg')));
  const strings = leftOutStrings(redactionOf({ datasets: [], reviews: [{ by: 'Ada Lovelace' }] }), { paths: ['/Users/someone/data/S01'] });
  assert.deepEqual(findLeftOut(texts, strings), [{ file: 'index.html: a figure', what: 'a person\'s name' }]);
});

test('a package\'s README and list of files: what it is, what it holds by SHA-256, what was left out and searched for, how to check it', () => {
  const { built } = certified();
  const c = built.certificate;
  const info = { title: 'Results of the fixture', certificate: c, certificateFile: 'certificate-x.zip', leftOut: { people: 2, notes: 1, names: 0 }, checked: { texts: 30, strings: 7 }, views: [{ name: 'Lung classes', file: 'views/1-lung.html', tiles: 16, levels: 2 }] };
  const readmeText = packageReadme(info);
  assert.match(readmeText, /^SituCraft result package\n/);
  assert.match(readmeText, /Open index\.html in a browser/);
  assert.match(readmeText, /views\/1-lung\.html\s+the view "Lung classes", its tiles in views\/1-lung\//);
  assert.match(readmeText, /2 people's names\n\(each written "Person 1", "Person 2", \.\.\.\); 1 note people wrote on reviews and regions\./);
  assert.match(readmeText, /The samples' and subjects' names are kept\./);
  assert.match(readmeText, /\(30 texts: its files, the certificate's entries, the\nfigures inside the report\) was searched for each of them \(7 strings\); none was found\./);
  assert.match(readmeText, /situcraft verify --data <a folder holding the bundles> <this folder>/);
  const files = [{ name: 'index.html', data: '<p>x</p>' }, { name: 'README.txt', data: readmeText }];
  const manifest = JSON.parse(packageManifest(files, info));
  assert.equal(manifest.format, 'situcraft-package');
  assert.deepEqual(manifest.certificate, { fingerprint: c.fingerprint, file: 'certificate-x.zip', subject: c.subject });
  assert.deepEqual(manifest.files[0], { path: 'index.html', size: 8, sha256: sha256(encoder.encode('<p>x</p>')) });
  assert.deepEqual(manifest.leftOut, info.leftOut);
  assert.equal(packageFolder('Results of the fixture', c.fingerprint), `Results_of_the_fixture-${c.fingerprint.slice(0, 8)}`);
});
