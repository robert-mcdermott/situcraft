// Records what Crossref says of every publication SituCraft cites by DOI (web/lib/references.js,
// REFERENCES) in crossref.json, beside this file: its title, journal, year (in print and online),
// volume, issue, pages or article number and authors. web/lib/references.test.mjs checks each
// reference against it, offline. Adapted from MaveScape 0.8.0 validation/fixtures/make-crossref.mjs.
//
//   node validation/reference/crossref/make-crossref.mjs
//
// Crossref's REST API (https://api.crossref.org), its metadata CC0. Run again when a reference is
// added; the file is kept in the repository.

import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { REFERENCES } from '../../../web/lib/references.js';

const here = dirname(fileURLToPath(import.meta.url));
const year = (part) => part?.['date-parts']?.[0]?.[0] ?? null;
const sleep = (ms) => new Promise((done) => setTimeout(done, ms));
// One record, waiting and asking again when Crossref says too many requests (429).
async function work(doi) {
  for (let attempt = 0; ; attempt += 1) {
    const response = await fetch(`https://api.crossref.org/works/${encodeURIComponent(doi)}`, { headers: { 'User-Agent': 'SituCraft-validation (https://github.com/robert-mcdermott/situcraft)' } });
    if (response.status !== 429 || attempt === 5) return response;
    await sleep(2000 * (attempt + 1));
  }
}
const records = {};
for (const [key, ref] of Object.entries(REFERENCES)) {
  if (!ref.doi) continue;
  await sleep(250);
  const response = await work(ref.doi);
  if (!response.ok) {
    console.error(`${key}: ${ref.doi}: HTTP ${response.status}`);
    records[ref.doi] = { key, missing: response.status };
    continue;
  }
  const m = (await response.json()).message;
  records[ref.doi] = {
    key,
    doi: m.DOI,
    type: m.type ?? null,
    title: (m.title ?? [])[0] ?? null,
    journal: (m['container-title'] ?? [])[0] ?? null,
    short: (m['short-container-title'] ?? [])[0] ?? null,
    year: year(m.issued),
    print: year(m['published-print']),
    online: year(m['published-online']),
    volume: m.volume ?? null,
    issue: m.issue ?? null,
    page: m.page ?? null,
    article: m['article-number'] ?? null,
    authors: (m.author ?? []).map((a) => a.family ?? a.name ?? null),
  };
  console.log(`${key}: ${records[ref.doi].title}`);
}
writeFileSync(join(here, 'crossref.json'), `${JSON.stringify({ source: 'https://api.crossref.org/works/{doi}', license: 'Crossref metadata, CC0', retrieved: new Date().toISOString().slice(0, 10), records }, null, 2)}\n`);
