// A result package (wave 8, slice 8): a folder of static files, or a zip of one, that opens in any
// browser without SituCraft. index.html is the report (lib/report.js: the figures, the numbers of
// each result traced to the certificate, the methods), which carries its certificate and checks
// it; views/ holds each saved view the figures show, its tissue drawn in advance as tiles with a
// reader of its own (lib/view-reader.js); the certificate is beside them too. It leaves out what
// lib/redact.js leaves out, and every text it holds is searched for that before it is written.
//
// manifest.json (format situcraft-package, version 1) lists every other file with its size and
// SHA-256, and says what was left out (how many of each, never what) and what was searched.

import { readZip } from './zip.js';
import { sha256 } from './sha256.js';
import { shortFingerprint } from './certificate.js';

export const PACKAGE_FORMAT = 'situcraft-package';
export const PACKAGE_VERSION = 1;
// A package's files, and its zip's entries, are dated 2026-01-01 (UTC), as a certificate's are:
// the same package is the same bytes.
export const PACKAGE_DATE = new Date(Date.UTC(2026, 0, 1));

const encoder = new TextEncoder();
const decoder = new TextDecoder();
const bytesOf = (data) => (typeof data === 'string' ? encoder.encode(data) : data);
const plural = (n, word, many = `${word}s`) => `${n.toLocaleString('en-US')} ${n === 1 ? word : many}`;

// A file or folder name made of a title: its words, made safe.
export const safeName = (text, fallback = 'package') => String(text ?? '').replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 60) || fallback;

// The package's folder: "<title>-<short fingerprint>".
export const packageFolder = (title, fingerprint) => `${safeName(title)}-${fingerprint.slice(0, 8)}`;

// What a package says of itself, for its README: { title, certificate (its JSON), certificateFile,
// views: [{ name, file }], leftOut: { people, notes, names }, checked: { texts, strings } }.
export function packageReadme({ title, certificate, certificateFile, views = [], leftOut, checked }) {
  const c = certificate;
  const v = c.software.version ?? '?';
  const row = (name, text) => `  ${name.padEnd(30)} ${text}`;
  const lines = [
    'SituCraft result package',
    '========================',
    '',
    title,
    `From the project ${c.project.name}, as of ${c.project.modified}; written by SituCraft ${v}${c.software.commit ? ` (commit ${c.software.commit.slice(0, 12)})` : ''}.`,
    `Its certificate's fingerprint: ${c.fingerprint}`,
    `(short form ${shortFingerprint(c.fingerprint)})`,
    '',
    'Open index.html in a browser. It is the report: the figures, the numbers each result reports',
    '(each traced to its place in the certificate), the methods and their references. As it opens it',
    'checks the certificate it carries, and that every number, figure and paragraph it shows is the',
    'certificate\'s, and says what it found at the top.',
    ...(views.length ? ['', `Each of the ${plural(views.length, 'saved view')} the figures show opens in a page of its own (views/), its tissue`, 'drawn in advance as tiles: drag to move, scroll to zoom.'] : []),
    '',
    'Nothing here asks the network for anything, and nothing needs installing: the folder can be',
    'copied, shared or put on a web server as it is.',
    '',
    'Contents',
    '--------',
    row('index.html', 'the report, carrying its certificate'),
    ...views.map((x) => row(x.file, `the view "${x.name}", its tiles in ${x.file.replace(/\.html$/, '/')}`)),
    row(certificateFile, 'the certificate, as the report carries it'),
    row('manifest.json', 'every other file, with its size and SHA-256'),
    '',
    'Left out',
    '--------',
    'Where its files lay on the computer that wrote it (a bundle\'s folder is written',
    `situcraft:bundle/<its dataset's ID>, any other file situcraft:file/<its name>); ${plural(leftOut.people, 'person\'s name', 'people\'s names')}`,
    `(each written "Person 1", "Person 2", ...); ${plural(leftOut.notes, 'note')} people wrote on reviews and regions${leftOut.names ? ';' : '.'}`,
    ...(leftOut.names ? [`and ${plural(leftOut.names, 'name')} of samples, subjects and sections (written "Sample 1", "Subject 1", "Section 1", ...).`] : ['The samples\' and subjects\' names are kept.']),
    `Before it was written, every text it holds (${plural(checked.texts, 'text')}: its files, the certificate's entries, the`,
    `figures inside the report) was searched for each of them (${plural(checked.strings, 'string')}); none was found.`,
    '',
    'To check the results',
    '--------------------',
    `With SituCraft ${v} or later (https://github.com/robert-mcdermott/situcraft/releases) and the bundles:`,
    '  situcraft verify --data <a folder holding the bundles> <this folder>',
    'makes each step again from the bundles and compares every file bit for bit (see the',
    'certificate\'s README).',
    ...(leftOut.names ? ['A step whose job was given the names left out cannot be made again from this package; the', 'certificate written without leaving them out can be.'] : []),
  ];
  return `${lines.join('\n')}\n`;
}

// The package's manifest.json, of its files (each { name, data }, the manifest not among them).
// info: { title, certificate, certificateFile, leftOut, checked, views: [{ name, file, tiles }] }.
export function packageManifest(files, { title, certificate, certificateFile, leftOut, checked, views = [] }) {
  const record = {
    format: PACKAGE_FORMAT,
    version: PACKAGE_VERSION,
    title,
    software: certificate.software,
    certificate: { fingerprint: certificate.fingerprint, file: certificateFile, subject: certificate.subject },
    views: views.map((x) => ({ name: x.name, file: x.file, tiles: x.tiles, levels: x.levels })),
    leftOut,
    checked,
    files: files.map((f) => {
      const data = bytesOf(f.data);
      return { path: f.name, size: data.length, sha256: sha256(data) };
    }),
  };
  return `${JSON.stringify(record, null, 1)}\n`;
}

const TEXT = /\.(html|json|md|txt|bib|svg|csv)$/i;

// Every text a package holds, to search: its text files'; the entries of a zip it holds (the
// certificate); what its HTML carries (each SVG figure and the certificate, in base64). files: [{
// name, data }]. Answers [{ name, text }].
export async function packageTexts(files) {
  const out = [];
  const fromZip = async (name, bytes) => {
    for (const [entry, data] of await readZip(bytes)) {
      if (TEXT.test(entry)) out.push({ name: `${name}: ${entry}`, text: decoder.decode(data) });
    }
  };
  for (const f of files) {
    const data = bytesOf(f.data);
    if (/\.zip$/i.test(f.name)) await fromZip(f.name, data);
    else if (TEXT.test(f.name)) {
      const text = typeof f.data === 'string' ? f.data : decoder.decode(data);
      out.push({ name: f.name, text });
      if (/\.html$/i.test(f.name)) {
        for (const m of text.matchAll(/data:image\/svg\+xml;base64,([A-Za-z0-9+/=]+)/g)) out.push({ name: `${f.name}: a figure`, text: decoder.decode(Uint8Array.from(atob(m[1]), (c) => c.charCodeAt(0))) });
        for (const m of text.matchAll(/<script type="application\/zip" id="situcraft-certificate">([A-Za-z0-9+/=\s]*)<\/script>/g)) await fromZip(`${f.name}: its certificate`, Uint8Array.from(atob(m[1].replace(/\s+/g, '')), (c) => c.charCodeAt(0)));
      }
    }
  }
  return out;
}
