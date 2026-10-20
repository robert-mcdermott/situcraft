import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { REFERENCES, bibtexEntry, citationKeys, referenceByDoi, referenceText, situcraftReference } from './references.js';

const root = fileURLToPath(new URL('../../', import.meta.url));
const fold = (text) => String(text ?? '').normalize('NFKD').replace(/<[^>]+>/g, ' ').replace(/[̀-ͯ]/g, '').toLowerCase().replace(/^the\s+/, '').replace(/[^a-z0-9]+/g, ' ').trim();
const firstPage = (pages) => String(pages ?? '').split(/--|-|–/)[0].trim();

// Crossref's records that differ from the publication as cited, for reasons of their own: each
// field not compared, and why.
const CROSSREF_EXCEPTIONS = {
  '10.2202/1544-6115.1027': { fields: ['pages'], why: 'the journal numbers its articles (Article 3); Crossref records the article\'s pages, 1–25' },
  '10.4086/toc.2012.v008a019': { fields: ['title'], why: 'Crossref records the article without its title' },
  '10.1093/biomet/28.3-4.437': { fields: ['title'], why: 'Crossref numbers the note as the issue did: "(i) Fiducial Limits for the Poisson Distribution"' },
  '10.1007/3-540-44668-0_68': { fields: ['journal'], why: 'Crossref gives the series (Lecture Notes in Computer Science), not the proceedings (ICANN 2001)' },
};

// Whether a journal's abbreviation (NLM's, as the references cite it) is of its name as Crossref
// records it: each word the start of the name's next word, its small words aside ("J R Stat Soc B"
// of "Journal of the Royal Statistical Society Series B: Statistical Methodology").
function abbreviates(abbreviation, name) {
  const small = new Set(['of', 'the', 'and', 'amp', 'on', 'in', 'for', 'series']);
  const a = fold(abbreviation).split(' ');
  const n = fold(name).split(' ').filter((w) => !small.has(w));
  return a.length <= n.length && a.every((w, i) => n[i].startsWith(w));
}

// Each reference with a DOI against Crossref's record of it: [{ key, doi, problems: [text] }].
function checkReferences(fixture) {
  const out = [];
  for (const [key, ref] of Object.entries(REFERENCES)) {
    if (!ref.doi) continue;
    const r = fixture.records[ref.doi];
    const problems = [];
    const skip = CROSSREF_EXCEPTIONS[ref.doi]?.fields ?? [];
    if (!r || r.missing) {
      out.push({ key, doi: ref.doi, problems: ['not in the recorded copy: run validation/reference/crossref/make-crossref.mjs'] });
      continue;
    }
    if (r.doi.toLowerCase() !== ref.doi.toLowerCase()) problems.push(`DOI ${r.doi}`);
    // A title as Crossref records it, or its part before a subtitle Crossref keeps apart.
    if (!skip.includes('title') && fold(r.title) !== fold(ref.title) && fold(r.title) !== fold(ref.title.split(':')[0])) problems.push(`title "${r.title}"`);
    // The journal (a preprint's server and a book's publisher Crossref may not record).
    const journals = [r.journal, r.short].filter(Boolean);
    if (ref.journal && journals.length && !skip.includes('journal') && !journals.some((j) => fold(j) === fold(ref.journal) || fold(ref.journal).startsWith(fold(j)) || fold(j).startsWith(fold(ref.journal)) || abbreviates(ref.journal, j))) problems.push(`journal "${r.journal}"`);
    if (![r.year, r.print, r.online].includes(ref.year)) problems.push(`year ${r.print ?? r.year} (online ${r.online})`);
    if (!skip.includes('volume') && String(r.volume ?? '') !== String(ref.volume ?? '')) problems.push(`volume ${r.volume}`);
    // Pages, where Crossref records them (a first page or an article number).
    if (!skip.includes('pages') && (r.page || r.article) && ![firstPage(r.page), r.article].filter(Boolean).map(String).includes(firstPage(ref.pages))) problems.push(`pages ${r.page ?? r.article}`);
    // The first author's family name.
    // (or its end, where Crossref records a given name with it: "Richard Shewchuk").
    const family = fold(ref.authors[0].replace(/\s+\S+$/, ''));
    const theirs = fold(r.authors?.[0]);
    if (!skip.includes('author') && r.authors?.length && theirs !== family && !theirs.endsWith(` ${family}`)) problems.push(`first author ${r.authors[0]}`);
    out.push({ key, doi: ref.doi, problems });
  }
  return out;
}

// The citations SituCraft's Go code records with its results: each constant or field named
// …Citation, each `Citation:` and the alignment methods' (umeyama, stone, explorer, cite), its
// string literals joined.
function goCitations() {
  const files = [];
  const walk = (dir) => {
    for (const f of readdirSync(dir)) {
      const p = join(dir, f);
      if (statSync(p).isDirectory()) {
        if (!['.git', 'web', 'docs', 'validation', 'product_research', 'testdata', 'dist', 'node_modules'].includes(f)) walk(p);
      } else if (p.endsWith('.go') && !p.endsWith('_test.go')) files.push(p);
    }
  };
  walk(root);
  const re = /(?:\b\w*Citation\s*=|\bCitation:\s*|\b(?:umeyama|stone|explorer|cite)\s*(?::=|\+=))[ \t]*((?:[^\n]*\+[ \t]*\n)*[^\n]*)/g;
  const out = [];
  for (const f of files) {
    const src = readFileSync(f, 'utf8');
    for (const m of src.matchAll(re)) {
      const text = [...m[1].matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((x) => x[1]).join('');
      if (text.length > 30) out.push({ file: f.slice(root.length), text });
    }
  }
  return out;
}

test('a citation names its references by first author and year; the rest kept as written', () => {
  assert.deepEqual(citationKeys('Wolf FA, Angerer P, Theis FJ. SCANPY. Genome Biol 2018;19:15; Traag VA, et al. From Louvain to Leiden. Sci Rep 2019;9:5233'), { keys: ['wolf2018', 'traag2019'], unknown: [] });
  // A note before a citation; two cited in one sentence; one the registry does not hold.
  const c = citationKeys('10x Genomics. Xenium Explorer: image alignment. Umeyama S. Least squares. 1991; Okabe A, Boots B. Spatial Tessellations. Wiley, 2000; distances: Felzenszwalb PF, Huttenlocher DP. Theory Comput. 2012;8:415; Doe J. Another method. J Things 2024;1:1');
  assert.deepEqual(c.keys, ['explorer', 'umeyama1991', 'okabe2000', 'felzenszwalb2012']);
  assert.deepEqual(c.unknown, ['Doe J. Another method. J Things 2024;1:1']);
  // The year matters: the same author of another year is not the reference.
  assert.deepEqual(citationKeys('Smyth GK. Something else. 2010').keys, []);
  assert.deepEqual(citationKeys('Schürch CM, et al. Cell 2020;182:1341').keys, ['schurch2020']);
  assert.deepEqual(citationKeys(''), { keys: [], unknown: [] });
  assert.equal(referenceByDoi('10.1186/S13059-017-1382-0'), 'wolf2018');
  assert.equal(referenceByDoi('10.1/none'), null);
});

test('every citation SituCraft records with a result is a reference of the registry', () => {
  const cites = goCitations();
  assert.ok(cites.length >= 20, `found ${cites.length} citations`);
  for (const c of cites) {
    const { keys, unknown } = citationKeys(c.text);
    assert.deepEqual(unknown, [], `${c.file}: ${c.text}`);
    assert.ok(keys.length, c.file);
  }
});

test('every publication the science page cites is a reference of the registry', () => {
  const html = readFileSync(join(root, 'docs/site/pages/science.html'), 'utf8');
  const list = html.slice(html.indexOf('<ul class="references">'));
  const items = [...list.matchAll(/<li>(.*?)<\/li>/gs)].map((m) => m[1].replace(/<[^>]+>/g, ''));
  assert.ok(items.length >= 55, `found ${items.length}`);
  for (const text of items) {
    const { keys } = citationKeys(text);
    assert.equal(keys.length, 1, text);
  }
});

test('the starter marker sets\' references are the registry\'s, by DOI', () => {
  const dir = join(root, 'internal/label/starters');
  for (const f of readdirSync(dir).filter((x) => x.endsWith('.json'))) {
    const set = JSON.parse(readFileSync(join(dir, f), 'utf8'));
    for (const r of set.references) {
      const key = referenceByDoi(r.doi);
      assert.ok(key, `${f}: ${r.doi}`);
      assert.deepEqual(citationKeys(r.citation).keys, [key], `${f}: ${r.citation}`);
    }
  }
});

test('each DOI is the publication cited, as Crossref records it', () => {
  const fixture = JSON.parse(readFileSync(join(root, 'validation/reference/crossref/crossref.json'), 'utf8'));
  const checked = checkReferences(fixture);
  assert.equal(checked.length, Object.values(REFERENCES).filter((r) => r.doi).length);
  const wrong = checked.filter((c) => c.problems.length).map((c) => `${c.key} (${c.doi}): ${c.problems.join('; ')}`);
  assert.deepEqual(wrong, []);
  // A record unlike the reference is found.
  const doi = REFERENCES.wolf2018.doi;
  const changed = { records: { ...fixture.records, [doi]: { ...fixture.records[doi], volume: '20', title: 'Another title' } } };
  assert.deepEqual(checkReferences(changed).find((c) => c.key === 'wolf2018').problems, ['title "Another title"', 'volume 20']);
});

test('a reference in Vancouver style and as BibTeX', () => {
  assert.equal(referenceText(REFERENCES.wolf2018), 'Wolf FA, Angerer P, Theis FJ. SCANPY: large-scale single-cell gene expression data analysis. Genome Biol 2018;19:15. doi:10.1186/s13059-017-1382-0');
  assert.equal(referenceText(REFERENCES.tirosh2016).startsWith('Tirosh I, Izar B, Prakadan SM, et al. Dissecting'), true);
  assert.equal(referenceText(REFERENCES.cliff1981), 'Cliff AD, Ord JK. Spatial Processes: Models and Applications. Pion, 1981.');
  assert.match(referenceText(situcraftReference('0.8.0')), /^McDermott R\. SituCraft, version 0\.8\.0\. https:/);
  const bib = bibtexEntry('wolf2018', REFERENCES.wolf2018);
  assert.match(bib, /^@article\{wolf2018,\n/);
  assert.match(bib, /author = \{Wolf, F\. A\. and Angerer, P\. and Theis, F\. J\.\}/);
  assert.match(bib, /pages = \{15\}/);
  assert.match(bib, /doi = \{10\.1186\/s13059-017-1382-0\}/);
  assert.match(bibtexEntry('mcinnes2018', REFERENCES.mcinnes2018), /^@misc\{mcinnes2018,[\s\S]*eprint = \{1802\.03426\}/);
  assert.match(bibtexEntry('tirosh2016', REFERENCES.tirosh2016), /and others\}/);
  assert.match(bibtexEntry('anselin1995', REFERENCES.anselin1995), /pages = \{93--115\}/);
  // Every entry has its braces balanced.
  for (const [key, r] of Object.entries(REFERENCES)) {
    const b = bibtexEntry(key, r);
    assert.equal((b.match(/\{/g) ?? []).length, (b.match(/\}/g) ?? []).length, key);
  }
});
