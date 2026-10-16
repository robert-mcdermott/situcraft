// The comparison workspace's Labels tab: what the choice of segmentation changes in the cells'
// labels (api_impact.go). A label set of each segmentation (any version), read through the
// comparison's differences: each label's cells under each, how many cells matched one to one keep
// their label and which change, what split and merged cells carry, marker mixing by label, and the
// differences whose labels change (each opens where it lies). The tissue can be colored by each
// side's labels. A segmentation without labels can have them made again from the other's
// clustering in one step (ui/remake.js).

import { h, icon } from './dom.js';
import { toast } from './overlays.js';
import { hexToRgb } from '../lib/colormaps.js';
import { CLASS_INFO } from '../lib/review.js';
import { defaultPair, impactLines, int32sOf, sharedColors, sideOf } from '../lib/impact.js';
import { clusteringsOf } from '../lib/clusterings.js';
import { labelSource, labelVersions, refOf } from './label-sources.js';
import { remakeOnSegmentation, remaking } from './remake.js';

const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const pct = (v, digits = 1) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');
const keyOf = (v) => (v ? `${v.id}:${v.level}` : '');
const num = (v) => (Number.isFinite(v) ? v.toFixed(4) : '—');
const UNLABELED = '#9a9a9a';

// Per segmentation cell, its label's color (A's colors for both where the names meet); grey for a
// cell the set covers without a label, clear for one it does not cover.
function fillOf(src, colors) {
  const rgba = new Uint8Array(4 * src.segCells);
  const grey = hexToRgb(UNLABELED);
  const byLabel = src.names.map((n) => hexToRgb(colors.get(n) ?? UNLABELED));
  for (let i = 0; i < src.segCells; i += 1) {
    if (src.label[i] >= 0) rgba.set([...byLabel[src.label[i]], 255], 4 * i);
    else if (src.analyzed[i]) rgba.set([...grey, 150], 4 * i);
  }
  return rgba;
}

// w: the workspace (ui/mode-compare.js): { app, t, cmp, nameA, nameB, groups, session, select,
// renderSide, recolor }. Answers the tab's contents; reading the labels re-renders it.
export function impactPanel(w) {
  const { app, t, cmp, nameA, nameB, session } = w;
  const project = app.store.project;
  const va = labelVersions(project, t.dataset.id, cmp.a);
  const vb = labelVersions(project, t.dataset.id, cmp.b);
  session.impact ??= { a: '', b: '', data: null, loading: '', error: null, fill: false, transition: null };
  const st = session.impact;
  if (!va.some((v) => keyOf(v) === st.a) || !vb.some((v) => keyOf(v) === st.b)) {
    const [pa, pb] = defaultPair(project, va, vb);
    if (!va.some((v) => keyOf(v) === st.a)) st.a = keyOf(pa);
    if (!vb.some((v) => keyOf(v) === st.b)) st.b = keyOf(pb);
  }
  const pick = (id, label, list, value, set) => {
    const el = h('select.input.small', { id }, ...list.map((v) => h('option', { value: keyOf(v) }, `${v.name}${v.kind === 'curated' || v.kind === 'consensus' ? ` (${v.kind})` : ''}`)));
    el.value = value;
    el.addEventListener('change', () => {
      set(el.value);
      w.renderSide();
    });
    return h('label.field', { for: id }, h('span', label), el);
  };
  const out = [h('p.muted.seg-note', `What the choice between ${nameA} (A) and ${nameB} (B) changes in the cells' labels: a label set of each, compared where the comparison matches cells.`)];
  const missing = [[va, cmp.b, cmp.a, nameA, nameB, 'b'], [vb, cmp.a, cmp.b, nameB, nameA, 'a']].filter(([list]) => !list.length);
  if (missing.length) {
    for (const [, from, to, toName, fromName] of missing) out.push(remakeCard(w, from, to, toName, fromName));
    if (missing.length === 2) out.push(h('p.muted.seg-note', 'Neither segmentation has labels yet: label one in the Cell types view.'));
    return out;
  }
  out.push(h('div.cmp-impact-pick', pick('cmp-labels-a', `A's labels`, va, st.a, (v) => { st.a = v; }), pick('cmp-labels-b', `B's labels`, vb, st.b, (v) => { st.b = v; })));
  const want = `${st.a}|${st.b}`;
  if (st.data?.key !== want) {
    if (st.loading !== want) load(w, va.find((v) => keyOf(v) === st.a), vb.find((v) => keyOf(v) === st.b), want);
    out.push(st.error ? h('div.lb-missing', icon('warning'), h('span', st.error)) : h('p.muted.seg-note', 'Reading both label sets through the comparison…'));
    return out;
  }
  out.push(...contents(w, st.data));
  return out;
}

async function load(w, a, b, key) {
  const st = w.session.impact;
  st.loading = key;
  st.error = null;
  try {
    const [srcA, srcB] = await Promise.all([labelSource(w.app, refOf(a.id, a.level)), labelSource(w.app, refOf(b.id, b.level))]);
    const answer = await w.app.bundles.comparisonImpact(w.t.item, w.cmp.id, { a: sideOf(srcA), b: sideOf(srcB) });
    if (st.loading !== key) return;
    const colors = sharedColors(srcA, srcB);
    st.data = { key, srcA, srcB, colors, answer, impact: answer.impact, sensitiveA: int32sOf(answer.sensitiveA), sensitiveB: int32sOf(answer.sensitiveB), fills: { a: fillOf(srcA, colors), b: fillOf(srcB, colors) } };
  } catch (error) {
    if (st.loading !== key) return;
    st.error = `The labels could not be compared: ${error.message}`;
  }
  st.loading = '';
  w.recolor();
  w.renderSide();
}

// The colors the tissue takes when the labels color it: { a, b, legend }, or null.
export function labelFill(session) {
  const st = session.impact;
  if (!st?.fill || !st.data) return null;
  const legend = [...st.data.colors].filter(([n]) => st.data.impact.composition.some((c) => c.name === n));
  return { a: st.data.fills.a, b: st.data.fills.b, legend };
}

function contents(w, data) {
  const { session, nameA, nameB, groups } = w;
  const st = session.impact;
  const im = data.impact;
  const color = (n) => (n ? data.colors.get(n) ?? UNLABELED : UNLABELED);
  const dot = (n) => h('span.dot', { style: { background: color(n) } });
  const label = (n) => (n || 'no label');
  const parts = [];
  parts.push(h('div.cmp-impact-lines', ...impactLines(im, `A (${nameA})`, `B (${nameB})`).map((l) => h('p', l))));
  const fillButton = h('button.btn.small', { type: 'button', 'aria-pressed': String(st.fill), 'data-action': 'color-by-labels', onclick: () => {
    st.fill = !st.fill;
    w.recolor();
    w.renderSide();
  } }, icon('tag'), st.fill ? 'Color by difference' : 'Color the cells by their labels');
  parts.push(h('div.btn-row', fillButton, h('span.muted', `${exact(im.sensitive[0])} of A's and ${exact(im.sensitive[1])} of B's labeled cells change label with the segmentation: the review queue can list them (Cell types).`)));

  // Each label's cells under each.
  const covA = im.cellsA.covered || 1;
  const covB = im.cellsB.covered || 1;
  const maxShare = Math.max(...im.composition.map((c) => Math.max(c.a / covA, c.b / covB)), 0.01);
  const bar = (v, c, title) => h('span.cmp-bar', { title }, h('span', { style: { width: `${(100 * v) / maxShare}%`, background: c } }));
  parts.push(h('div.section-title', 'Each label\'s cells'),
    h('div', { style: { overflowX: 'auto' } }, h('table.data.cmp-table.cmp-impact-table',
      h('thead', h('tr', h('th', 'Label'), h('th.r', 'A'), h('th.r', 'B'), h('th', { title: 'Each one\'s share of the cells its label set covers' }, 'A, B'), h('th.r', { title: 'B\'s share less A\'s, in points' }, 'Change'))),
      h('tbody', ...im.composition.map((c) => {
        const sa = c.a / covA;
        const sb = c.b / covB;
        return h('tr', h('td', dot(c.name), ` ${label(c.name)}`), h('td.r', exact(c.a)), h('td.r', exact(c.b)),
          h('td', h('div.cmp-bars', bar(sa, color(c.name), `A: ${pct(sa)}`), bar(sb, color(c.name), `B: ${pct(sb)}`))),
          h('td.r', `${sb >= sa ? '+' : '−'}${Math.abs(100 * (sb - sa)).toFixed(1)}`));
      })))),
    h('p.muted.seg-note', `Of the cells each label set covers: A ${exact(im.cellsA.covered)} of ${exact(im.cellsA.cells)}, B ${exact(im.cellsB.covered)} of ${exact(im.cellsB.cells)}.`));

  // The matched cells whose label changes, by the pair of labels; chosen, its differences.
  const moves = im.matched.transitions.filter((x) => x.a !== x.b).slice(0, 12);
  if (moves.length) {
    parts.push(h('div.section-title', 'Matched cells whose label changes'),
      h('div.cmp-chips', h(`button.cmp-chip${st.transition ? '' : '.on'}`, { type: 'button', 'aria-pressed': String(!st.transition), onclick: () => {
        st.transition = null;
        w.renderSide();
      } }, 'Every change'), ...moves.map((x) => {
        const on = st.transition && st.transition[0] === x.a && st.transition[1] === x.b;
        return h(`button.cmp-chip${on ? '.on' : ''}`, { type: 'button', 'aria-pressed': String(Boolean(on)), onclick: () => {
          st.transition = on ? null : [x.a, x.b];
          w.renderSide();
        } }, dot(x.a), ` ${label(x.a)} → `, dot(x.b), ` ${label(x.b)} ${exact(x.cells)}`);
      })));
  }

  // The differences whose labels change, the most transcripts reassigned first.
  const nameIn = (src, c) => (src.label[c] >= 0 ? src.names[src.label[c]] : '');
  let changed = im.changed.map((i) => groups[i]);
  if (st.transition) changed = changed.filter((g) => g.a.length === 1 && g.b.length === 1 && nameIn(data.srcA, g.a[0]) === st.transition[0] && nameIn(data.srcB, g.b[0]) === st.transition[1]);
  const words = (src, cells) => [...new Set(cells.filter((c) => src.analyzed[c]).map((c) => label(nameIn(src, c))))].join(', ');
  parts.push(h('div.section-title', `Differences whose labels change (${exact(changed.length)})`),
    changed.length ? h('div.cmp-queue', ...changed.slice(0, 100).map((g) => h(`button.cmp-item${g.index === session.selected ? '.selected' : ''}`, { type: 'button', 'data-difference': g.index, onclick: () => w.select(g.index) },
      h('span.cmp-rank-no.mono', CLASS_INFO[g.class].label.slice(0, 5)),
      h('span.cmp-item-main', h('span', `${words(data.srcA, g.a) || '—'} → ${words(data.srcB, g.b) || '—'}`), h('span.muted', `${CLASS_INFO[g.class].label}: ${g.a.length} cell${g.a.length === 1 ? '' : 's'} of A, ${g.b.length} of B`)),
      h('span.cmp-item-nums.mono', { title: 'Reassigned gene transcripts' }, exact(g.changed)))),
    changed.length > 100 ? h('p.muted.seg-note', `The first 100 of ${exact(changed.length)}.`) : null) : h('p.muted.seg-note', 'None.'));

  // Marker mixing by label.
  const x = im.mixing;
  if (x) {
    const byName = new Map();
    for (const m of x.a) byName.set(m.name, { a: m });
    for (const m of x.b) byName.set(m.name, { ...(byName.get(m.name) ?? {}), b: m });
    const cell = (m) => (m ? `${pct(m.share)} of ${exact(m.cells)}` : '—');
    parts.push(h('div.section-title', 'Marker mixing by label'),
      h('div', { style: { overflowX: 'auto' } }, h('table.data.cmp-table',
        h('thead', h('tr', h('th', 'Label'), h('th.r', { title: 'A\'s cells of the label expressing two exclusive markers together' }, 'A'), h('th.r', 'B'), h('th.r', { title: 'The mutually exclusive co-expression rate among the label\'s cells, A then B' }, 'MECR'))),
        h('tbody',
          h('tr', h('td', h('b', 'All labeled cells')), h('td.r', cell(x.allA)), h('td.r', cell(x.allB)), h('td.r', `${num(x.allA.mecr)} → ${num(x.allB.mecr)}`)),
          ...[...byName].map(([n, v]) => h('tr', h('td', dot(n), ` ${n}`), h('td.r', cell(v.a)), h('td.r', cell(v.b)), h('td.r', `${num(v.a?.mecr)} → ${num(v.b?.mecr)}`)))))),
      h('p.muted.seg-note', `The share of each label's cells expressing a pair of mutually exclusive markers together (at least one transcript of each), and the mutually exclusive co-expression rate among them (Hartman and Satija 2024), over ${exact(x.pairs)} pairs of ${data.answer.pairs}. Fewer is better: transcripts of a neighbor counted in a cell mix its markers.`));
  }

  // Cells without a counterpart.
  const list = (rows) => rows.slice(0, 6).map((r) => `${label(r.name)} ${exact(r.a)}`).join(', ') + (rows.length > 6 ? ', …' : '');
  if (im.onlyA.length || im.onlyB.length) parts.push(h('p.muted.seg-note', `Cells without a counterpart: only in A, ${list(im.onlyA) || 'none'}; only in B, ${list(im.onlyB) || 'none'}.`));
  parts.push(h('p.muted.seg-note', 'Labels are compared by name. Matched cells are those the comparison pairs one to one (the same, expanded or contracted); a cell a label set does not cover (not analyzed) is left out.'));
  return parts;
}

// A segmentation without labels: another's clustering and its label sets made again on it.
function remakeCard(w, from, to, toName, fromName) {
  const { app, t } = w;
  const project = app.store.project;
  const sources = clusteringsOf(project, t.dataset.id, from).filter((c) => c.state === 'complete' && !c.summary?.source && project.artifacts.some((a) => a.kind === 'labels' && a.state === 'complete' && a.clustering === c.id));
  if (!sources.length) return h('div.lb-missing', icon('warning'), h('span', `${toName} has no labels, and ${fromName} no clustering of SituCraft's with labels to make again on it: label the cells in the Cell types view.`));
  const id = `cmp-remake-${to}`;
  const pick = h('select.input.small', { id }, ...sources.map((c) => h('option', { value: c.id }, c.name)));
  pick.value = sources.at(-1).id;
  const going = sources.flatMap((c) => remaking(c.id).filter((g) => g.segmentation === to));
  const run = async () => {
    try {
      const done = await remakeOnSegmentation(app, t, pick.value, to, () => w.renderSide());
      const problems = done.failed.map((f) => f.error);
      toast(`Made again on ${toName}: ${done.labels.length} label set${done.labels.length === 1 ? '' : 's'}.${problems.length ? ` ${problems.join('; ')}.` : ''}`, { kind: problems.length ? 'info' : 'ok', timeout: 10000 });
    } catch (error) {
      toast(`Made again: ${error.message}`, { kind: 'error', timeout: 12000 });
    }
    w.renderSide();
  };
  return h('div.seg-card.cmp-remake',
    h('p', `${toName} has no labels yet. Make a clustering of ${fromName} and its label sets again on it, with the same parameters, to compare them here.`),
    h('div.lb-import-row', h('label.field', { for: id }, h('span', 'Clustering'), pick), h('button.btn.primary', { type: 'button', 'data-action': 'remake', disabled: going.length > 0, onclick: run }, icon('copy'), 'Make again')),
    ...going.map((g) => h('div.seg-running', h('div', h('b', `Making again on ${toName}`), ' ', h('span.muted', g.step)))));
}
