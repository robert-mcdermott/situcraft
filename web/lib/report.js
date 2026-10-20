// The report (wave 8, slice 7): one self-contained HTML file of chosen results. The figures, each an
// SVG carrying its manifest; a table of each result's numbers, every number traced to its place in
// the certificate (a JSON pointer into certificate.json); the methods and their references; and
// the certificate itself, carried in the file and checked by the report as it opens, offline, in
// any browser (lib/report-check.js, written into it). Nothing in it asks the network for anything.
// The same project and choice give the same bytes.

import { checkReportData, numberText, renderReportCheck } from './report-check.js';
import { STEP_KINDS } from './record.js';
import { shortFingerprint } from './certificate.js';

const encoder = new TextEncoder();
const escapeHTML = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const count = (n) => Number(n).toLocaleString('en-US');

function base64(bytes) {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

// --- Tables ---------------------------------------------------------------------------------------

// The words a key of a result's summary is shown by.
const WORDS = {
  cells: 'Cells', labeled: 'Labeled', unlabeled: 'Unlabeled', clusters: 'Clusters', sizes: 'Cells a cluster', modularity: 'Modularity', components: 'Components', varianceShare: 'Variance explained',
  resolution: 'Resolution', edges: 'Edges', meanDegree: 'Mean degree', isolated: 'Isolated cells', medianLength: 'Median edge (µm)', trustworthiness: 'Trustworthiness', precision: 'Precision', recall: 'Recall',
  significant: 'Found', genes: 'Genes', tests: 'Tests', untested: 'Untested', below: 'Below the minimum', niches: 'Niches', labels: 'Labels', states: 'States', margin: 'Margin',
  ratio: 'Ratio', z: 'z', padj: 'Adjusted p', p: 'p', i: 'Moran\'s I', logFC: 'log2 fold change', estimate: 'Estimate', count: 'Count', level: 'Level',
  a: 'Label', b: 'Neighbor', gene: 'Gene', label: 'Label', contrast: 'Contrast', family: 'Family', key: 'Statistic', name: 'Name', sample: 'Sample',
  pixelAgreement: 'Pixels agreeing', changedFraction: 'Changed', medianIoU: 'Median IoU', spuriousCoexpressionA: 'Spurious co-expression, A', spuriousCoexpressionB: 'Spurious co-expression, B', mecrA: 'MECR, A', mecrB: 'MECR, B',
  assignedFraction: 'Transcripts assigned', q20GeneTranscripts: 'Gene transcripts (Q20)', medianTranscriptsPerCell: 'Median transcripts a cell', medianAreaUm2: 'Median area (µm²)', emptyCells: 'Empty cells',
  agreementMin: 'Least agreement', agreementMean: 'Mean agreement',
};
// Summary keys not shown: where a result came from (the steps say it) and colors.
const HIDDEN = new Set(['segmentation', 'graph', 'clustering', 'summary', 'colors', 'id', 'question', 'parameters', 'choice', 'checksum', 'markers', 'methods']);
const words = (key) => WORDS[key] ?? key.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/^./, (c) => c.toUpperCase());
const pointerOf = (path) => `/${path.map((p) => String(p).replace(/~/g, '~0').replace(/\//g, '~1')).join('/')}`;

// A number as a table shows it (and as the report checks it): lib/report-check.js's.
export { numberText };

// A result's tables, from its step's summary in the certificate: its numbers (and words) at the top,
// a table for each list of rows, a list of numbers as a row; each number with its pointer.
// Answers [{ caption, columns, rows: [[{ text, pointer?, value? }]] }].
export function summaryTables(summary, base) {
  const tables = [];
  const top = [];
  const cell = (v, path) => (typeof v === 'number' ? { text: numberText(v), pointer: pointerOf(path), value: v } : { text: String(v ?? '') });
  const walk = (obj, path, prefix) => {
    for (const [k, v] of Object.entries(obj ?? {})) {
      // Nothing, or a reference to nothing (an analysis of genes names no labels: its labels' ID is '').
      if (HIDDEN.has(k) || v === null || v === undefined || v === '' || (typeof v === 'object' && !Array.isArray(v) && v.id === '')) continue;
      const label = prefix ? `${prefix}: ${words(k).toLowerCase()}` : words(k);
      if (Array.isArray(v)) {
        if (!v.length) continue;
        if (v.every((x) => x && typeof x === 'object' && !Array.isArray(x))) {
          const columns = [...new Set(v.flatMap((x) => Object.keys(x).filter((c) => !HIDDEN.has(c) && (x[c] === null || typeof x[c] !== 'object'))))];
          tables.push({ caption: label, columns: columns.map(words), rows: v.map((x, i) => columns.map((c) => cell(x[c], [...path, k, i, c]))) });
        } else if (v.every((x) => typeof x === 'number')) {
          tables.push({ caption: label, columns: v.map((_, i) => String(i + 1)), rows: [v.map((x, i) => cell(x, [...path, k, i]))] });
        } else if (v.every((x) => typeof x !== 'object')) {
          top.push([{ text: label }, { text: v.join(', ') }]);
        }
      } else if (typeof v === 'object') {
        walk(v, [...path, k], label);
      } else {
        top.push([{ text: label }, cell(v, [...path, k])]);
      }
    }
  };
  walk(summary, base, '');
  if (top.length) tables.unshift({ caption: 'Its numbers', columns: ['', ''], rows: top, facts: true });
  return tables;
}

function tableHTML(t) {
  const td = (c) => (c.pointer ? `<td class="num" data-pointer="${escapeHTML(c.pointer)}" data-value="${escapeHTML(JSON.stringify(c.value))}" title="certificate.json ${escapeHTML(c.pointer)}">${escapeHTML(c.text)}</td>` : `<td>${escapeHTML(c.text)}</td>`);
  const head = t.facts ? '' : `<thead><tr>${t.columns.map((c) => `<th>${escapeHTML(c)}</th>`).join('')}</tr></thead>`;
  return `<table class="${t.facts ? 'facts' : 'rows'}"><caption>${escapeHTML(t.caption)}</caption>${head}<tbody>${t.rows.map((r) => `<tr>${r.map(td).join('')}</tr>`).join('')}</tbody></table>`;
}

// --- The report -----------------------------------------------------------------------------------

const STYLE = `
:root { color-scheme: light dark; --bg: #ffffff; --fg: #171b26; --muted: #5b6478; --line: #d9dde6; --panel: #f5f6f9; --accent: #3d5bd9; --ok: #1f7a4d; --bad: #b42335; }
@media (prefers-color-scheme: dark) { :root { --bg: #11141b; --fg: #e8eaf0; --muted: #a2a9b8; --line: #2c3240; --panel: #1a1e28; --accent: #8ea2ff; --ok: #6fd3a0; --bad: #ff8b97; } }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 15px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
main { max-width: 960px; margin: 0 auto; padding: 32px 16px 64px; }
h1 { font-size: 28px; margin: 0 0 6px; } h2 { font-size: 20px; margin: 36px 0 10px; padding-top: 8px; border-top: 1px solid var(--line); } h3 { font-size: 16px; margin: 22px 0 6px; }
.meta, .muted { color: var(--muted); font-size: 13.5px; }
.check { margin: 16px 0; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--line); background: var(--panel); font-size: 14px; }
.check.ok { border-color: var(--ok); } .check.bad { border-color: var(--bad); color: var(--bad); }
nav a { color: var(--accent); margin-right: 14px; font-size: 14px; }
figure { margin: 18px 0; } figure img { width: 100%; height: auto; background: #ffffff; border: 1px solid var(--line); border-radius: 4px; }
figcaption { font-size: 14px; margin-top: 6px; }
.table-wrap { overflow-x: auto; }
table { border-collapse: collapse; margin: 10px 0 14px; font-size: 13.5px; }
caption { text-align: left; font-weight: 600; padding: 4px 0; }
th, td { border-bottom: 1px solid var(--line); padding: 4px 10px 4px 0; text-align: left; vertical-align: top; }
td.num { font-variant-numeric: tabular-nums; text-align: right; cursor: help; }
td.differs { color: var(--bad); text-decoration: underline wavy; }
.methods-paragraph { margin: 0 0 12px; }
ol.refs { font-size: 13.5px; padding-left: 26px; } code { font-size: 13px; }
a { color: var(--accent); }
`;

// The report's HTML. input: { title, certificate (its JSON), zip (Uint8Array: the certificate's
// zip), methods (lib/methods-text.js's), figures: [{ id, name, svg (text, its manifest inside; the
// SHA-256 of its UTF-8 the certificate's), description }], results: [ID] (the results whose tables
// it shows, in order) }.
export function reportHTML({ title, certificate, zip, methods, figures = [], results = [] }) {
  const c = certificate;
  const stepIndex = (id) => c.steps.findIndex((s) => s.id === id);
  const parts = [];
  parts.push(`<!doctype html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n<meta name="viewport" content="width=device-width, initial-scale=1">\n<meta name="generator" content="SituCraft ${escapeHTML(c.software.version ?? '')}">\n<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src data: blob:">\n<title>${escapeHTML(title)}</title>\n<style>${STYLE}</style>\n</head>\n<body>\n<main>`);
  parts.push(`<h1>${escapeHTML(title)}</h1>`);
  parts.push(`<p class="meta">From the project ${escapeHTML(c.project.name)}, as of ${escapeHTML(c.project.modified)}. Written by SituCraft ${escapeHTML(c.software.version ?? '')}${c.software.commit ? ` (commit ${escapeHTML(c.software.commit.slice(0, 7))})` : ''}. Its certificate's fingerprint: <code>${escapeHTML(c.fingerprint)}</code> (${escapeHTML(shortFingerprint(c.fingerprint))}).</p>`);
  parts.push('<div id="report-check" class="check">This report checks the certificate it carries as it opens; with scripts off, it is not checked.</div>');
  const sections = [figures.length ? ['figures', 'Figures'] : null, results.length ? ['results', 'Results'] : null, methods?.paragraphs?.length ? ['methods', 'Methods'] : null, methods?.references?.length ? ['references', 'References'] : null, ['certificate', 'The certificate']].filter(Boolean);
  parts.push(`<nav>${sections.map(([id, name]) => `<a href="#${id}">${name}</a>`).join('')}</nav>`);

  if (figures.length) {
    parts.push('<section id="figures"><h2>Figures</h2>');
    figures.forEach((f, i) => {
      const n = stepIndex(f.id);
      parts.push(`<figure id="${escapeHTML(f.id)}"><img data-figure="${escapeHTML(f.id)}" alt="${escapeHTML(f.description)}" src="data:image/svg+xml;base64,${base64(encoder.encode(f.svg))}"><figcaption><b>Figure ${i + 1}. ${escapeHTML(f.name)}.</b> ${escapeHTML(f.description)}${n >= 0 ? ` <span class="muted">(Step ${c.steps[n].n} of the certificate, which holds the SHA-256 of this SVG.)</span>` : ''}</figcaption></figure>`);
    });
    parts.push('</section>');
  }

  if (results.length) {
    parts.push('<section id="results"><h2>Results</h2><p class="muted">Each number is read from the certificate; rest the pointer on one to see its place in certificate.json.</p>');
    for (const id of results) {
      const n = stepIndex(id);
      if (n < 0) continue;
      const s = c.steps[n];
      parts.push(`<h3 id="result-${escapeHTML(id)}">${escapeHTML(s.name)}</h3><p class="muted">${escapeHTML(STEP_KINDS[s.kind] ?? s.kind)} · step ${s.n} of the certificate · <code>${escapeHTML(id)}</code>${s.method?.name ? ` · ${escapeHTML(s.method.name)}${s.method.version ? `, version ${escapeHTML(s.method.version)}` : ''}` : ''}</p>`);
      const tables = summaryTables(s.summary, ['steps', n, 'summary']);
      if (!tables.length) parts.push('<p class="muted">It reports no numbers of its own.</p>');
      for (const t of tables) parts.push(`<div class="table-wrap">${tableHTML(t)}</div>`);
    }
    parts.push('</section>');
  }

  if (methods?.paragraphs?.length) {
    parts.push('<section id="methods"><h2>Methods</h2>');
    for (const p of methods.paragraphs) parts.push(`<h3>${escapeHTML(p.heading)}</h3><p class="methods-paragraph">${escapeHTML(p.text)}</p>`);
    parts.push('</section>');
  }
  if (methods?.references?.length) {
    parts.push(`<section id="references"><h2>References</h2><ol class="refs">${methods.references.map((r) => `<li value="${r.n}">${escapeHTML(r.text)}${r.doi && !r.text.includes(r.doi) ? ` doi:${escapeHTML(r.doi)}` : ''}</li>`).join('')}</ol></section>`);
  }

  const name = `certificate-${c.fingerprint.slice(0, 8)}.zip`;
  parts.push(`<section id="certificate"><h2>The certificate</h2><p>This report carries its certificate: the bundles the results rest on, by their files' SHA-256, and each step from them to the results, ${count(c.steps.length)} in all (the record of each and the methods are in it too). It proves that these bundles, analyzed as recorded, give these files and numbers; not that the results are right. As the report opens it checks, offline, that the certificate is as certified and that each number, figure and paragraph here is the certificate's.</p><p>To check the results themselves, make them again: with SituCraft ${escapeHTML(c.software.version ?? '')} or later, run <code>situcraft verify --data &lt;a folder holding the bundles&gt; &lt;this file&gt;</code>, which also checks this report against its certificate (or save the certificate, <a id="certificate-link" download="${escapeHTML(name)}">${escapeHTML(name)}</a>, and give it instead).</p></section>`);
  parts.push('</main>');
  parts.push(`<script type="application/zip" id="situcraft-certificate">${base64(zip)}</script>`);
  parts.push(`<script>\nconst numberText = ${numberText.toString()};\nconst checkReportData = ${checkReportData.toString()};\n(${renderReportCheck.toString()})(document, checkReportData);\n</script>`);
  parts.push('</body>\n</html>\n');
  return parts.join('\n');
}

// The numbers a report traces, from its HTML: [{ pointer, value, text }].
export function reportNumbers(html) {
  const unescape = (t) => t.replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
  return [...html.matchAll(/data-pointer="([^"]*)" data-value="([^"]*)"[^>]*>([^<]*)</g)].map((m) => ({ pointer: unescape(m[1]), value: JSON.parse(unescape(m[2])), text: unescape(m[3]) }));
}
