// The Cohort view's QC (wave 7, slice 5): before any comparison, the samples side by side. Each
// quality number by sample with its batch, the samples far out flagged with the reason (never passed
// or failed; leaving one out of the design asks for a reason, which the design keeps); the numbers
// by batch; the batches against the design's groups, a group confounded with a slide, run or panel
// said; the pseudobulk's principal components colored by any field; and the samples' panels
// compared, only the genes every panel has compared across samples. Nothing is corrected for a
// batch unless the design adjusts for it.

import { h, icon, clear } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { cohortSession, summaryDocument } from './cohort-summary.js';
import { draftOf, setDraft } from './cohort-design.js';
import { categoricalColor } from '../lib/colormaps.js';
import { designFields, makeDesign } from '../lib/design.js';
import { fieldValue, samplesOf } from '../lib/samples.js';
import { batchStatements, linearScale, qcRows, summariesOf } from '../lib/cohort.js';

// What the section shows, for the session: the principal components on each axis, the field the
// points are colored by.
const view = { x: 0, y: 1, color: '' };

const fmt = (v, share) => {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—';
  if (share) return `${(100 * v).toFixed(1)}%`;
  return v.toLocaleString('en-US', { maximumFractionDigits: Math.abs(v) >= 100 ? 0 : 1 });
};

// The summary the section reads: the one the cohort table shows, else the latest.
function shownSummary(project) {
  const list = summariesOf(project);
  return list.find((r) => r.id === cohortSession.shown) ?? list[0] ?? null;
}

export function qcSection(app, { redraw }) {
  const project = app.store.project;
  const record = shownSummary(project);
  const card = h('div.seg-card.cohort-qc');
  const section = h('section.seg-section', h('div.section-title', 'Cohort QC, batches and panels'), card);
  if (!record) {
    card.append(h('p', 'Summarize the samples first: the quality numbers, the batches, the principal components and the panels are the summary\'s.'));
    return section;
  }
  card.append(h('p.muted', 'Reading the summary…'));
  summaryDocument(record.id).then((doc) => {
    clear(card);
    card.append(...parts(app, doc, record, redraw));
  }, (error) => {
    clear(card);
    card.append(h('p', h('span.badge.warn', 'Not read'), ` ${error.message}`));
  });
  return section;
}

function parts(app, doc, record, redraw) {
  const project = app.store.project;
  const metrics = doc.qcMetrics ?? [];
  const rows = qcRows(doc);
  const rule = doc.outlierRule ?? {};
  const out = [
    h('p', `Of ${record.name.replace(/^Summary of /, '')}. Each quality number below is compared across the samples: a sample is far out on one when it is ${rule.z} robust standard deviations from the samples' median (the median absolute deviation's) and differs by enough to matter (${Math.round(100 * (rule.fold - 1))}% for a quantity, ${Math.round(100 * rule.points)} points for a share). Being far out is a reason to look at a sample, not a verdict. SituCraft corrects for no batch unless the design adjusts for it.`),
  ];

  // --- Quality by sample.
  const head = h('tr', h('th', 'Sample'), h('th', 'Slide'), h('th', 'Run'), h('th', 'Panel'), ...metrics.map((m) => h('th.r', m.name)));
  const body = rows.map((r) => h('tr', { 'data-sample': r.sample },
    h('td', r.name), h('td', r.slide), h('td', r.run), h('td', r.panel),
    ...metrics.map((m) => {
      const f = r.flags[m.key];
      return h(`td.r${f ? '.cohort-flag' : ''}`, { title: f ? f.reason : '' }, fmt(r.values[m.key], m.share), f ? h('span.sr-only', ` (far out: ${f.reason})`) : null);
    })));
  out.push(h('h3.seg-card-title', 'Quality by sample'),
    h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-qc-table', h('thead', head), h('tbody', ...body))));
  const flagged = [...new Set((doc.outliers ?? []).map((o) => o.sample))];
  if (doc.samples.length < (rule.minSamples ?? 5)) {
    out.push(h('p.muted.seg-note', `${rule.minSamples ?? 5} samples at least are needed to call one far out; these are ${doc.samples.length}.`));
  } else if (!flagged.length) {
    out.push(h('p.seg-note', `No sample is far out on any of the ${metrics.length} quality numbers.`));
  } else {
    const draft = draftOf(project);
    out.push(h('ul.cohort-list.cohort-flags', ...flagged.map((id) => {
      const s = doc.samples.find((x) => x.sample === id);
      const reasons = doc.outliers.filter((o) => o.sample === id).map((o) => o.reason);
      const left = draft.spec.exclude?.[id];
      return h('li', h('b', s?.name ?? id), `: ${reasons.join('; ')}. `,
        left ? h('span.muted', `Left out of the design draft: ${left}`) : h('button.btn.small.ghost', {
          type: 'button',
          'data-action': 'leave-out',
          onclick: async () => {
            const reason = await promptDialog({ title: `Leave ${s?.name ?? id} out of the design?`, label: 'Why (kept with the design)', value: `Far out in cohort QC: ${reasons[0]}`, confirm: 'Leave it out' });
            if (!reason) return;
            const d = draftOf(app.store.project);
            setDraft(app.store.project, { ...d.spec, exclude: { ...(d.spec.exclude ?? {}), [id]: reason } });
            toast(`${s?.name ?? id} is left out of the design draft; save the design to keep it.`, { kind: 'ok' });
            redraw();
          },
        }, 'Leave it out of the design…'));
    })));
  }

  // --- By batch.
  const kinds = ['slide', 'run', 'panel'].map((kind) => ({ kind, batches: (doc.batches ?? []).filter((b) => b.kind === kind) })).filter((k) => k.batches.length);
  out.push(h('h3.seg-card-title', 'By batch'),
    h('p.muted.seg-note', `The samples' median by slide, run and panel: ${kinds.map((k) => `${k.batches.length} ${k.kind}${k.batches.length === 1 ? '' : 's'}`).join(', ')}.`),
    h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-qc-table',
      h('thead', h('tr', h('th', 'Batch'), h('th.r', 'Samples'), ...metrics.map((m) => h('th.r', m.name)))),
      h('tbody', ...kinds.flatMap((k) => k.batches.map((b) => h('tr', h('td', new RegExp(`\\b${k.kind}\\b`, 'i').test(b.name) ? b.name : `${k.kind[0].toUpperCase()}${k.kind.slice(1)} ${b.name}`), h('td.r', String(b.samples.length)), ...metrics.map((m) => h('td.r', fmt(b.medians?.[m.key], m.share))))))))));

  // --- Batches and groups.
  const draft = draftOf(project);
  const design = makeDesign(project, draft.spec);
  const statements = batchStatements(design);
  out.push(h('h3.seg-card-title', 'Batches and groups'));
  if (!design.spec.group || !design.levels.length) {
    out.push(h('p.muted.seg-note', 'Choose the groups compared in the design (above) to see each batch\'s samples by group.'));
  } else {
    for (const b of design.balance.filter((x) => x.table.length)) {
      out.push(h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-balance',
        h('caption.sr-only', `The samples by ${b.label} and ${design.spec.group}`),
        h('thead', h('tr', h('th', `${b.label[0].toUpperCase()}${b.label.slice(1)}`), ...design.levels.map((l) => h('th.r', `${design.spec.group} ${l}`)))),
        h('tbody', ...b.table.map((row) => h('tr', h('td', row.value), ...design.levels.map((l) => h('td.r', String(row.byGroup[l] ?? 0)))))))));
    }
    out.push(statements.length
      ? h('ul.cohort-list', ...statements.map((st) => h('li', h(`span.badge${st.kind === 'confounded' ? '.warn' : ''}`, st.kind === 'confounded' ? 'Confounded' : 'Uneven'), ` ${st.text}`)))
      : h('p.seg-note', `Every slide, run and panel holds every ${design.spec.group}, or there is one of each: no batch separates the groups.`));
  }

  // --- Principal components.
  out.push(h('h3.seg-card-title', 'The pseudobulk\'s principal components'), pcaPart(app, doc, redraw));

  // --- Panels.
  out.push(h('h3.seg-card-title', 'Panels'), panelsPart(doc.panels, doc.samples));
  return out;
}

// The samples on two principal components, colored by a field.
function pcaPart(app, doc, redraw) {
  const pca = doc.pca;
  if (!pca || !pca.variance?.length) return h('p.muted.seg-note', 'No principal components: they need two samples whose expression differs.');
  const project = app.store.project;
  const K = pca.variance.length;
  if (view.x >= K) view.x = 0;
  if (K > 1 && (view.y >= K || view.y === view.x)) view.y = view.x === 0 ? 1 : 0;
  const fields = designFields(project);
  if (!view.color) view.color = draftOf(project).spec.group || fields[0]?.name || '';
  const samples = samplesOf(project);
  const valueOf = (id) => {
    const s = samples.find((x) => x.id === id);
    const v = s ? fieldValue(s, view.color) : '';
    return v === '' ? 'not recorded' : String(v);
  };
  const values = [...new Set(doc.samples.map((s) => valueOf(s.sample)))];
  const color = (v) => categoricalColor(values.indexOf(v));
  const pick = (label, value, onchange, options) => h('select.input.small', { 'aria-label': label, onchange: (e) => onchange(e.target.value) }, ...options.map((o) => h('option', { value: o.value, selected: o.value === value }, o.text)));
  const pcs = pca.variance.map((v, k) => ({ value: String(k), text: `PC${k + 1} (${(100 * v).toFixed(1)}%)` }));
  const controls = h('div.btn-row',
    h('span.muted', 'Across'), pick('The component across', String(view.x), (v) => { view.x = Number(v); redraw(); }, pcs),
    h('span.muted', 'up'), K > 1 ? pick('The component up', String(view.y), (v) => { view.y = Number(v); redraw(); }, pcs) : null,
    h('span.muted', 'Colored by'), pick('The field the samples are colored by', view.color, (v) => { view.color = v; redraw(); }, fields.map((f) => ({ value: f.name, text: f.bundle ? f.label : f.name }))));
  const W = 560;
  const H = 360;
  const P = 44;
  const xs = doc.samples.map((_, s) => pca.scores[s][view.x]);
  const ys = doc.samples.map((_, s) => (K > 1 ? pca.scores[s][view.y] : 0));
  const sx = linearScale(Math.min(...xs), Math.max(...xs), P, W - 16);
  const sy = linearScale(Math.min(...ys), Math.max(...ys), H - P, 16);
  const ns = 'http://www.w3.org/2000/svg';
  const el = (tag, attrs, ...kids) => {
    const n = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
    for (const kid of kids) n.append(kid);
    return n;
  };
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}`, class: 'cohort-pca', role: 'img', 'aria-label': `The samples on PC${view.x + 1} and PC${view.y + 1}, colored by ${view.color}` });
  svg.append(el('line', { x1: P, x2: W - 16, y1: H - P, y2: H - P, class: 'cohort-axis' }), el('line', { x1: P, x2: P, y1: 16, y2: H - P, class: 'cohort-axis' }));
  if (Math.min(...xs) < 0 && Math.max(...xs) > 0) svg.append(el('line', { x1: sx(0), x2: sx(0), y1: 16, y2: H - P, class: 'cohort-zero' }));
  if (K > 1 && Math.min(...ys) < 0 && Math.max(...ys) > 0) svg.append(el('line', { x1: P, x2: W - 16, y1: sy(0), y2: sy(0), class: 'cohort-zero' }));
  svg.append(el('text', { x: (P + W - 16) / 2, y: H - 10, 'text-anchor': 'middle', class: 'cohort-axis-label' }, `PC${view.x + 1} (${(100 * pca.variance[view.x]).toFixed(1)}% of the variance)`));
  if (K > 1) svg.append(el('text', { x: 14, y: (16 + H - P) / 2, 'text-anchor': 'middle', transform: `rotate(-90 14 ${(16 + H - P) / 2})`, class: 'cohort-axis-label' }, `PC${view.y + 1} (${(100 * pca.variance[view.y]).toFixed(1)}%)`));
  doc.samples.forEach((s, k) => {
    const v = valueOf(s.sample);
    const g = el('g', {}, el('title', {}, `${s.name}: ${view.color} ${v}; PC${view.x + 1} ${xs[k].toFixed(2)}${K > 1 ? `, PC${view.y + 1} ${ys[k].toFixed(2)}` : ''}`),
      el('circle', { cx: sx(xs[k]), cy: sy(ys[k]), r: 6, fill: color(v), class: 'cohort-point' }));
    if (doc.samples.length <= 30) g.append(el('text', { x: sx(xs[k]) + 9, y: sy(ys[k]) + 4, class: 'cohort-point-label' }, s.name));
    svg.append(g);
  });
  const legend = h('div.cohort-legend', ...values.map((v) => h('span', h('span.cohort-swatch', { style: { background: color(v) } }), `${view.color} ${v}`)));
  return h('div',
    h('p.muted.seg-note', `Each sample's transcripts of the ${pca.genes.length < pca.top ? pca.genes.length : pca.top} most variable genes every panel has, summed over its labels, as log2(1 + counts per million), centered; nothing corrected. Samples that gather by a batch rather than by group are a batch effect to account for; a sample apart from all is one to look at.`),
    controls, svg, legend);
}

// The samples' panels compared.
function panelsPart(panels, samples) {
  if (!panels) return h('p.muted.seg-note', 'The summary does not compare the panels: summarize again.');
  const name = (id) => samples.find((s) => s.sample === id)?.name ?? id;
  const P = panels.panels.length;
  const out = [h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table',
    h('thead', h('tr', h('th', 'Panel'), h('th', 'Design'), h('th', 'Version'), h('th.r', 'Genes'), h('th', 'Samples'))),
    h('tbody', ...panels.panels.map((p) => h('tr', h('td', p.name || '—'), h('td', p.design || '—'), h('td', p.version || '—'), h('td.r', p.genes.toLocaleString('en-US')), h('td', p.samples.map(name).join(', '))))))),
  ];
  if (P <= 1) {
    out.push(h('p.seg-note', `One panel: all ${panels.shared.length.toLocaleString('en-US')} of its genes are compared across the samples.`));
    return h('div', ...out);
  }
  out.push(h('p.seg-note', `${panels.shared.length.toLocaleString('en-US')} genes are in every panel: only they are compared across the samples (the principal components above, and the comparisons to come). ${panels.partial.length.toLocaleString('en-US')} are in some panels only and are not compared.`));
  const panelName = (k) => panels.panels[k]?.design || panels.panels[k]?.name || `panel ${k + 1}`;
  if (panels.partial.length) {
    out.push(h('details.cohort-details', h('summary', `The ${panels.partial.length.toLocaleString('en-US')} genes not in every panel`),
      h('ul.cohort-list.cohort-genes', ...panels.partial.map((g) => h('li', h('b', g.gene), ` in ${g.panels.map(panelName).join(', ')} only`)))));
  }
  if (panels.codewords.length) {
    out.push(h('details.cohort-details', h('summary', `${panels.codewords.length.toLocaleString('en-US')} shared genes read by other codewords`),
      h('p.muted', 'Each panel designs its own probes: a gene read by other codewords may be detected a little differently in each.'),
      h('ul.cohort-list.cohort-genes', ...panels.codewords.map((g) => h('li', h('b', g.gene), `: ${g.codewords.map((c, k) => `${panelName(k)} ${c.join(', ')}`).join('; ')}`)))));
  }
  if (panels.ids.length) {
    out.push(h('details.cohort-details', h('summary', `${panels.ids.length.toLocaleString('en-US')} genes named or identified differently`),
      h('ul.cohort-list.cohort-genes', ...panels.ids.map((g) => h('li', g.gene ? [h('b', g.gene), `: Ensembl ${g.ids.map((id, k) => `${panelName(k)} ${id}`).join('; ')}`] : [h('b', g.id), `: named ${g.names.map((n, k) => `${n} in ${panelName(k)}`).join(', ')}`])))));
  }
  return h('div', ...out);
}
