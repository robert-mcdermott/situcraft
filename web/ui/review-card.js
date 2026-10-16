// Label versions and their review in the Cell types view (wave 4, slice 5): the segmentation's
// predicted label sets (from markers, imported), curated sets (a reviewer's edits, each with its
// reason) and consensus sets (a rule over chosen sets), side by side; the review queue (cells whose
// label their tissue neighbors rarely share, or of low confidence, api_labels.go); and a cell's
// evidence card: its label in each version, why (its markers and the next label's, in the cell),
// its neighbors' labels in the tissue and in expression, its counts and its segmentation's flags,
// and whether it was reviewed, by whom and why. Decisions are appended to a curated set.

import { h, icon, clear, formatCount, relativeTime } from './dom.js';
import { toast } from './overlays.js';
import { RULES, appendEdit, checkRule, createCurated, crossTable, curatedSetsOf, nearestCells, saveConsensus, tally } from '../lib/curation.js';
import { labelSource, labelVersions, refOf } from './label-sources.js';

const percent = (v, digits = 0) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');
const fixed = (v, digits = 2) => (Number.isFinite(v) ? v.toFixed(digits) : '—');

function b64(typed) {
  const bytes = new Uint8Array(typed.buffer, typed.byteOffset, typed.byteLength);
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

async function postJSON(url, body) {
  const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const out = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(out.error ?? `HTTP ${response.status}`);
  return out;
}

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  const out = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(out.error ?? `HTTP ${response.status}`);
  return out;
}

const versionKey = (v) => `${v.id}:${v.level}`;

// ctx: { t, seg, session, clustering, cellMap, render }. Answers the sections to show.
export function reviewSections(app, ctx) {
  const { t, seg, session, clustering } = ctx;
  if (!seg || !clustering) return [];
  const st = session.labels;
  st.versions ??= { a: '', b: '', building: null };
  st.review ??= { set: '', neighbors: 10, implausible: 0.2, low: null, queue: null, page: 0, cell: null, busy: false };
  const project = app.store.project;
  const versions = labelVersions(project, t.dataset.id, seg.id);
  if (!versions.length) return [];
  const rv = st.review;
  if (!versions.some((v) => versionKey(v) === rv.set)) {
    rv.set = versionKey(versions.find((v) => v.id === st.shown) ?? versions[0]);
    rv.queue = null;
  }
  // Low confidence, by default: from markers, a lead under twice the margin.
  const reviewed = versions.find((v) => versionKey(v) === rv.set);
  if (rv.lowFor !== rv.set) {
    rv.lowFor = rv.set;
    const rec = reviewed?.kind === 'markers' ? project.artifacts.find((a) => a.id === reviewed.id) : null;
    rv.low = rec ? 2 * (rec.parameters?.margin ?? 0.1) : null;
  }
  const evidenceHost = (st.evidenceHost ??= h('div.rv-evidence'));
  // A cell chosen on the map or the tissue shows its evidence.
  app.labelReview = { focusCell: (cell) => showEvidence(cell) };
  return [versionsCard(), reviewCard()];

  // --- The versions, side by side ---
  function versionsCard() {
    const vs = st.versions;
    const rows = versions.map((v) => {
      const curated = v.kind === 'curated' ? project.artifacts.find((a) => a.id === v.id) : null;
      return h('tr',
        h('td', h('span.badge', v.kind)),
        h('td', v.name),
        h('td.muted', curated ? `${curated.edits.length} edit${curated.edits.length === 1 ? '' : 's'}` : ''),
        h('td.lb-actions',
          h('button.btn.small', { type: 'button', title: 'Color the map and the tissue by it', onclick: () => ctx.cellMap?.setColor({ kind: 'labels', labels: v.id, level: v.level }) }, icon('tag'), 'Color'),
          v.kind === 'markers' || v.kind === 'imported' ? h('button.btn.small', { type: 'button', title: 'Begin a curated set from it: its labels, with your edits appended', onclick: () => beginCurated(v) }, icon('edit'), 'Curate') : null));
    });
    const pick = (key, label) => {
      const el = h('select.input', { id: `rv-cmp-${key}` }, ...versions.map((v) => h('option', { value: versionKey(v) }, v.name)));
      if (!versions.some((v) => versionKey(v) === vs[key])) vs[key] = versionKey(versions[key === 'a' ? 0 : Math.min(1, versions.length - 1)]);
      el.value = vs[key];
      el.addEventListener('change', () => {
        vs[key] = el.value;
        ctx.render(true);
      });
      return h('label.field', { for: `rv-cmp-${key}` }, h('span', label), el);
    };
    const result = h('div.rv-compare', h('p.muted.seg-note', 'Comparing…'));
    const va = versions.find((v) => versionKey(v) === vs.a);
    const vb = versions.find((v) => versionKey(v) === vs.b);
    if (va && vb) {
      Promise.all([labelSource(app, refOf(va.id, va.level)), labelSource(app, refOf(vb.id, vb.level))]).then(([a, b]) => {
        const ct = crossTable(a, b);
        const usedA = a.names.map((_, i) => i).filter((i) => ct.rowSums[i] > 0);
        const usedB = b.names.map((_, j) => j).filter((j) => ct.colSums[j] > 0);
        clear(result).append(
          h('p', h('b', `${percent(ct.agreement, 1)} of the ${formatCount(ct.cells)} cells both label carry the same name`), h('span.muted', ` in ${va.name} (rows) and ${vb.name} (columns).`)),
          h('div.table-wrap', h('table.data-table.ct-contingency',
            h('thead', h('tr', h('th', ''), ...usedB.map((j) => h('th', { title: b.names[j] }, b.names[j])))),
            h('tbody', ...usedA.map((i) => h('tr', h('th', a.names[i]), ...usedB.map((j) => {
              const v = ct.table[i][j];
              const share = ct.rowSums[i] ? v / ct.rowSums[i] : 0;
              return h('td', { style: { background: share > 0.02 ? `color-mix(in srgb, var(--accent) ${Math.round(80 * share)}%, transparent)` : '' }, title: `${formatCount(v)} cells (${percent(share)} of the row)` }, v ? formatCount(v) : '');
            })))))),
          h('p.muted.seg-note', 'Each cell counts the cells both sets give these labels, shaded by its share of the row.'));
      }).catch((error) => clear(result).append(h('p.muted.seg-note', `The comparison: ${error.message}`)));
    }
    return h('section.seg-section',
      h('div.section-title', 'Label versions'),
      h('div.seg-card',
        h('p', 'Predicted labels (from markers, imported), curated labels (a reviewer\'s changes to a predicted set, each with its reason, appended) and consensus labels (a rule over chosen sets), side by side. Each is kept in the project; the Viewer\'s Fill by and the map list them.'),
        h('div.table-wrap', h('table.data-table.seg-table', h('tbody', ...rows))),
        vs.building ? consensusBuilder() : h('div.btn-row', h('button.btn.small', { type: 'button', disabled: versions.length < 2, 'data-action': 'consensus', onclick: () => {
          // Two different sets by default (not two levels of one).
          const distinct = versions.filter((v, i) => v.kind !== 'consensus' && versions.findIndex((w) => w.id === v.id) === i);
          vs.building = { members: (distinct.length >= 2 ? distinct : versions).slice(0, 2).map(versionKey), rule: 'majority', min: 2, name: 'Consensus', mapping: {} };
          ctx.render(true);
        } }, icon('layers'), 'Make a consensus…')),
        versions.length > 1 ? h('div.seg-import-grid', pick('a', 'Side by side: A'), pick('b', 'B')) : null,
        versions.length > 1 ? result : null));
  }

  function beginCurated(v) {
    const name = `${v.name}, curated`;
    const next = createCurated(app.store.project, { dataset: t.dataset.id, segmentation: seg.id, base: { labels: v.id, level: v.level, name: v.name }, name });
    app.store.commit(next, `Begin the curated label set ${name}`);
    const made = curatedSetsOf(app.store.project, t.dataset.id, seg.id).at(-1);
    rv.set = `${made.id}:0`;
    rv.queue = null;
    ctx.render(true);
    toast(`${name}: review its cells below; each decision is kept with its reason.`, { kind: 'ok' });
  }

  function consensusBuilder() {
    const b = st.versions.building;
    const chosen = b.members.map((k) => versions.find((v) => versionKey(v) === k)).filter(Boolean);
    const boxes = versions.filter((v) => v.kind !== 'consensus').map((v) => {
      const key = versionKey(v);
      const box = h('input', { type: 'checkbox', checked: b.members.includes(key) });
      box.addEventListener('change', () => {
        b.members = box.checked ? [...b.members, key] : b.members.filter((x) => x !== key);
        ctx.render(true);
      });
      return h('label.lb-check', box, ` ${v.name}`);
    });
    const rule = h('select.input', { id: 'rv-rule' }, ...RULES.map((r) => h('option', { value: r.id, title: r.help }, r.label)));
    rule.value = b.rule;
    rule.addEventListener('change', () => {
      b.rule = rule.value;
      ctx.render(true);
    });
    const min = h('input.input', { type: 'number', id: 'rv-min', min: '1', max: String(Math.max(1, chosen.length)), step: '1', value: String(b.min) });
    min.addEventListener('change', () => {
      b.min = Number(min.value);
    });
    const name = h('input.input', { type: 'text', id: 'rv-name', value: b.name });
    name.addEventListener('change', () => {
      b.name = name.value.trim() || 'Consensus';
    });
    const mappingHost = h('div.rv-mapping', h('p.muted.seg-note', 'Reading the sets\' labels…'));
    Promise.all(chosen.map((v) => labelSource(app, refOf(v.id, v.level)))).then((srcs) => {
      // The labels each set gives some cell; those another set does not give may be mapped.
      const used = srcs.map((s) => {
        const seen = new Set();
        for (const k of s.label) if (k >= 0) seen.add(s.names[k]);
        return [...seen];
      });
      const all = new Set(used.flat());
      const rows = srcs.flatMap((s, j) => used[j].filter((n) => used.some((o, k) => k !== j && !o.includes(n))).map((n) => {
        const key = versionKey(chosen[j]);
        const input = h('input.input.small', { type: 'text', value: b.mapping[key]?.[n] ?? '', placeholder: n, list: 'rv-names', 'aria-label': `${chosen[j].name}: ${n} as` });
        input.addEventListener('change', () => {
          b.mapping[key] = { ...(b.mapping[key] ?? {}), [n]: input.value.trim() };
        });
        return h('tr', h('td.muted', chosen[j].name), h('td', n), h('td', input));
      }));
      clear(mappingHost).append(h('datalist', { id: 'rv-names' }, ...[...all].map((n) => h('option', { value: n }))),
        rows.length ? h('details.seg-advanced', h('summary', `Labels the sets name differently (${rows.length}): map them to common names`), h('div.table-wrap', h('table.data-table.seg-table', h('thead', h('tr', h('th', 'Set'), h('th', 'Label'), h('th', 'Counts as'))), h('tbody', ...rows)))) : h('p.muted.seg-note', 'The sets name their labels alike.'));
    }).catch((error) => clear(mappingHost).append(h('p.muted.seg-note', error.message)));
    const save = () => {
      const members = chosen.map((v) => ({ ref: refOf(v.id, v.level), name: v.name }));
      const r = { kind: b.rule, ...(b.rule === 'majority' ? { min: b.min } : {}) };
      const problem = checkRule(r, members.length);
      if (problem) {
        toast(problem, { kind: 'error' });
        return;
      }
      const mapping = chosen.map((v) => Object.fromEntries(Object.entries(b.mapping[versionKey(v)] ?? {}).filter(([, to]) => to)));
      app.store.commit(saveConsensus(app.store.project, { dataset: t.dataset.id, segmentation: seg.id, name: b.name, members, mapping, rule: r }), `Make the consensus label set ${b.name}`);
      st.versions.building = null;
      ctx.render(true);
    };
    return h('div.rv-builder',
      h('div.section-title', 'A consensus'),
      h('div.rv-boxes', ...boxes),
      h('div.seg-import-grid',
        h('label.field', { for: 'rv-name' }, h('span', 'Name'), name),
        h('label.field', { for: 'rv-rule', title: RULES.find((r) => r.id === b.rule)?.help }, h('span', 'Rule'), rule),
        b.rule === 'majority' ? h('label.field', { for: 'rv-min' }, h('span', 'Sets agreeing, at least'), min) : null),
      h('p.muted.seg-note', RULES.find((r) => r.id === b.rule)?.help ?? ''),
      mappingHost,
      h('div.btn-row', h('button.btn.primary', { type: 'button', disabled: chosen.length < 2, onclick: save }, icon('save'), 'Save the consensus'), h('button.btn', { type: 'button', onclick: () => {
        st.versions.building = null;
        ctx.render(true);
      } }, 'Cancel')));
  }

  // --- The review ---
  function reviewCard() {
    const v = versions.find((x) => versionKey(x) === rv.set);
    const pick = h('select.input', { id: 'rv-set' }, ...versions.map((x) => h('option', { value: versionKey(x) }, `${x.name} (${x.kind})`)));
    pick.value = rv.set;
    pick.addEventListener('change', () => {
      rv.set = pick.value;
      rv.queue = null;
      rv.cell = null;
      ctx.render(true);
    });
    const num = (id, label, key, step, help) => {
      const input = h('input.input', { type: 'number', id, step: String(step), value: rv[key] === null ? '' : String(rv[key]), title: help, placeholder: key === 'low' ? 'none' : '' });
      input.addEventListener('change', () => {
        rv[key] = input.value === '' ? null : Number(input.value);
      });
      return h('label.field', { for: id, title: help }, h('span', label), input);
    };
    const queueHost = h('div.rv-queue');
    if (rv.queue) drawQueue(queueHost, v);
    return h('section.seg-section.rv-section',
      h('div.section-title', 'Review'),
      h('div.seg-card',
        h('p', 'Which cells to look at first: those whose label their nearest labeled cells in the tissue rarely share (most share another: a lone label among others is often a mislabel, a doublet, or an outline holding parts of two cells), and those of low confidence. Choose a cell, here or on the map and the tissue, to see its evidence; accept its label or relabel it, saying why. Decisions are appended to a curated set.'),
        h('div.seg-import-grid',
          h('label.field', { for: 'rv-set' }, h('span', 'Labels'), pick),
          num('rv-neighbors', 'Nearest cells', 'neighbors', 1, 'The labeled cells nearest each cell (by centroid) whose labels are counted.'),
          num('rv-implausible', 'Implausible at most', 'implausible', 0.05, 'A cell is flagged when at most this share of its nearest cells carry its label, and most carry another.'),
          num('rv-low', 'Confidence below', 'low', 0.05, 'A cell is flagged when its confidence is below this (empty: not by confidence). From markers, the confidence is how far the label led the next; imported, the table\'s.')),
        h('div.btn-row', h('button.btn.primary', { type: 'button', disabled: rv.busy, 'data-action': 'review-queue', onclick: () => makeQueue(v) }, icon('stethoscope'), rv.busy ? 'Ranking…' : 'Make the queue')),
        queueHost,
        evidenceHost));
  }

  async function makeQueue(v) {
    rv.busy = true;
    ctx.render(true);
    try {
      const src = await labelSource(app, refOf(v.id, v.level));
      const target = curatedFor(v, false);
      const reviewed = [];
      if (target) {
        const cur = await labelSource(app, refOf(target.id));
        cur.edited.forEach((e, i) => {
          if (e >= 0) reviewed.push(i);
        });
      }
      const body = { labels: b64(src.label), neighbors: rv.neighbors, implausible: rv.implausible, lowConfidence: rv.low, limit: 500, reviewed };
      if (src.confidence) body.confidence = b64(src.confidence);
      const answer = await postJSON(`${t.item.url}/segmentations/${encodeURIComponent(seg.id)}/review`, body);
      rv.queue = { ...answer, names: src.names, labels: src.label, set: versionKey(v) };
      rv.page = 0;
    } catch (error) {
      toast(`The review queue: ${error.message}`, { kind: 'error', timeout: 10000 });
    }
    rv.busy = false;
    ctx.render(true);
  }

  // The curated set a review of v appends to: v itself when curated, else the curated set begun from
  // v (made when make is true and there is none).
  function curatedFor(v, make) {
    if (v.kind === 'curated') return app.store.project.artifacts.find((a) => a.id === v.id);
    const found = curatedSetsOf(app.store.project, t.dataset.id, seg.id).find((c) => c.base.labels === v.id && c.base.level === v.level);
    if (found || !make) return found ?? null;
    const name = `${v.name}, reviewed`;
    app.store.commit(createCurated(app.store.project, { dataset: t.dataset.id, segmentation: seg.id, base: { labels: v.id, level: v.level, name: v.name }, name }), `Begin the curated label set ${name}`);
    return curatedSetsOf(app.store.project, t.dataset.id, seg.id).at(-1);
  }

  function drawQueue(host, v) {
    const q = rv.queue;
    const c = q.counts;
    const per = 12;
    const items = q.queue.slice(rv.page * per, (rv.page + 1) * per);
    const reason = (it) => [
      it.reasons.includes('implausible') ? `${Math.round(it.share * q.neighbors)} of ${q.neighbors} nearest share it; most are ${q.names[it.dominant] ?? '?'}` : null,
      it.reasons.includes('low-confidence') ? `confidence ${fixed(it.confidence)}` : null,
    ].filter(Boolean).join('; ');
    clear(host).append(
      h('p', h('b', `${formatCount(c.implausible + c.lowConfidence - c.both)} cells to review`), h('span.muted', ` of ${formatCount(c.labeled)} labeled: ${formatCount(c.implausible)} implausible, ${formatCount(c.lowConfidence)} of low confidence, ${formatCount(c.both)} both; ${formatCount(c.reviewed)} reviewed already. The first ${formatCount(q.queue.length)}, most doubtful first:`)),
      h('div.table-wrap', h('table.data-table.seg-table.rv-table',
        h('thead', h('tr', h('th.r', 'Rank'), h('th', 'Cell'), h('th', 'Label'), h('th', 'Why'))),
        h('tbody', ...items.map((it, r) => h(`tr${rv.cell === it.cell ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-cell': it.cell, onclick: () => showEvidence(it.cell, true), onkeydown: (event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            showEvidence(it.cell, true);
          }
        } }, h('td.r', String(rv.page * per + r + 1)), h('td.mono', `#${it.cell}`), h('td', q.names[q.labels[it.cell]] ?? ''), h('td.muted', reason(it))))))),
      h('div.btn-row',
        h('button.btn.small', { type: 'button', disabled: rv.page === 0, onclick: () => {
          rv.page -= 1;
          drawQueue(host, v);
        } }, 'Previous'),
        h('span.muted', `${rv.page * per + 1}–${Math.min(q.queue.length, (rv.page + 1) * per)} of ${q.queue.length}`),
        h('button.btn.small', { type: 'button', disabled: (rv.page + 1) * per >= q.queue.length, onclick: () => {
          rv.page += 1;
          drawQueue(host, v);
        } }, 'Next')));
  }

  // --- A cell's evidence card ---
  async function showEvidence(cell, fromQueue = false) {
    rv.cell = cell;
    const v = versions.find((x) => versionKey(x) === rv.set) ?? versions[0];
    if (fromQueue) ctx.cellMap?.focus(cell);
    clear(evidenceHost).append(h('p.muted.seg-note', `Reading cell #${cell}…`));
    try {
      const shown = await labelSource(app, refOf(v.id, v.level));
      const all = [];
      for (const x of versions.slice(0, 10)) all.push([x, await labelSource(app, refOf(x.id, x.level))]);
      const markerSet = shown.kind === 'labels' && shown.record && !shown.record.source && shown.record.clustering === clustering.id ? shown.id
        : shown.kind === 'curated' && shown.base?.kind === 'labels' && shown.base.record && !shown.base.record.source && shown.base.record.clustering === clustering.id ? shown.base.id : null;
      // The label most of its nearest cells carry, when it is not the cell's: why not that one.
      const cells = ctx.cellMap?.state?.cells;
      const nearest = cells ? nearestCells(cells.x, cells.y, shown.label, cell, 10) : [];
      const most = tally(shown.label, nearest)[0]?.label ?? -1;
      const also = markerSet && most >= 0 && most !== shown.label[cell] && most < (shown.base?.names ?? shown.names).length ? `&also=${most}` : '';
      const [ev, detail] = await Promise.all([
        getJSON(`${t.item.url}/clusterings/${encodeURIComponent(clustering.id)}/cells/${cell}/evidence${markerSet ? `?labels=${encodeURIComponent(markerSet)}${also}` : ''}`),
        getJSON(seg.source === 'instrument' ? `${t.item.url}/cells/${cell}` : `${t.item.url}/segmentations/${encodeURIComponent(seg.id)}/cells/${cell}`).catch(() => null),
      ]);
      if (rv.cell !== cell) return;
      clear(evidenceHost).append(...evidenceCard(cell, v, shown, all, ev, detail));
      evidenceHost.scrollIntoView({ block: 'nearest' });
    } catch (error) {
      clear(evidenceHost).append(h('p.muted.seg-note', `The evidence of cell #${cell}: ${error.message}`));
    }
  }

  function evidenceCard(cell, v, shown, all, ev, detail) {
    const name = (src, k) => (k >= 0 ? src.names[k] : 'no label');
    const own = shown.label[cell];
    const parts = [];
    const cells = ctx.cellMap?.state?.cells;
    parts.push(h('div.rv-head', h('b', `Cell ${detail?.id ?? `#${cell}`}`), h('span.muted', ` of ${seg.name.toLowerCase()}${ev.index >= 0 ? `, cluster ${ev.cluster + 1} of ${clustering.name.toLowerCase()}` : ', not analyzed by the clustering'}`)));
    // Its labels in each version.
    parts.push(h('div.section-title', 'Its labels'), h('dl.kv.seg-kv', ...all.flatMap(([x, src]) => [h('dt', x.name), h('dd', name(src, src.label[cell]), Number.isFinite(src.confidence?.[cell]) ? h('span.muted', ` (${x.kind === 'consensus' ? `${percent(src.confidence[cell])} of the sets` : `confidence ${fixed(src.confidence[cell])}`})`) : null)])));
    // Why, from markers.
    if (ev.labels) {
      const e = ev.labels;
      const gene = (g, sign) => {
        const on = g.count > 0;
        const cls = sign === 'positive' ? (on ? '.rv-for' : '.rv-missing') : (on ? '.rv-against' : '.rv-missing');
        return h(`span.rv-gene${cls}`, { title: `${g.gene}: ${g.count} transcript${g.count === 1 ? '' : 's'}, ${fixed(g.value)} log-normalized (the cells' mean ${fixed(g.mean)})` }, `${g.gene} ${g.count}`);
      };
      const list = (le) => h('div.rv-label',
        h('div', h('b', le.name), h('span.muted', ` score ${fixed(le.score)}: positive markers ${fixed(le.positive.mean)} against their controls' ${fixed(le.positive.controls)}${le.negative ? `; negative markers ${fixed(le.negative.mean)} against ${fixed(le.negative.controls)}` : ''}`)),
        h('div.rv-genes', ...le.positive.genes.map((g) => gene(g, 'positive')), ...(le.negative?.genes ?? []).map((g) => gene(g, 'negative'))));
      for (const lv of e.levels) {
        const best = lv.evidence[0];
        const next = lv.evidence[1];
        const took = lv.label >= 0;
        const lead = lv.lead === null ? '' : ` by ${fixed(lv.lead)}`;
        parts.push(h('div.section-title', `${lv.level}: ${took ? best.name : 'no label'}`),
          h('p.muted.seg-note', took ? `${best.name} led ${next ? next.name : 'the minimum'}${lead} (the margin ${e.margin}).` : `${best.name} led ${next ? next.name : 'the minimum'}${lead}, less than the margin ${e.margin}${best.score < e.minimum ? `, and scored below the minimum ${e.minimum}` : ''}: the cell stays at the level above.`),
          list(best), next ? list(next) : null);
      }
      if (e.also) parts.push(h('div.section-title', `Why not ${e.also.name}, as most of its neighbors?`), list(e.also));
      parts.push(h('p.muted.seg-note', 'Green: a positive marker the cell expresses; dashed: one it does not; red: a negative marker it expresses. Each with its transcripts in the cell; hover for its value against the cells\' mean.'));
    }
    // Its neighbors' labels.
    const near = [];
    if (cells) {
      const nearest = nearestCells(cells.x, cells.y, shown.label, cell, 10);
      near.push(h('dt', 'In the tissue'), h('dd', summary(tally(shown.label, nearest), shown, nearest.length)));
    }
    if (ev.neighbors?.length) near.push(h('dt', 'In expression'), h('dd', summary(tally(shown.label, ev.neighbors), shown, ev.neighbors.length)));
    if (near.length) parts.push(h('div.section-title', `Its neighbors' labels (${shown.name})`), h('dl.kv.seg-kv', ...near));
    // The cell itself.
    if (detail) {
      const method = detail.method ? `${detail.method.charAt(0).toLowerCase()}${detail.method.slice(1)}` : null;
      const flags = [...(detail.limitations ?? []), method].filter(Boolean);
      parts.push(h('div.section-title', 'The cell'), h('dl.kv.seg-kv',
        h('dt', 'Transcripts'), h('dd', `${formatCount(detail.transcripts ?? detail.counted?.q20 ?? 0)} (${formatCount(detail.genesDetected ?? 0)} genes)`, detail.genes?.length ? h('span.muted', `; most: ${detail.genes.slice(0, 5).map((g) => `${g.name} ${g.count}`).join(', ')}`) : null),
        h('dt', 'Area'), h('dd', `${fixed(detail.area, 1)} µm²${detail.nucleusArea ? `, nucleus ${fixed(detail.nucleusArea, 1)} µm²` : ''}`),
        h('dt', 'Its segmentation'), h('dd', flags.length ? flags.join('; ') : 'no flag', Number.isFinite(detail.boundaryCertainty) ? h('span.muted', `; boundary certainty ${fixed(detail.boundaryCertainty)}`) : null)));
    }
    // Its review.
    const target = curatedFor(v, false);
    const edits = target ? target.edits.map((e, n) => [e, n]).filter(([e]) => e.cells?.includes(cell) || (e.kind === 'cluster' && ev.index >= 0 && e.clustering === clustering.id && e.clusters.includes(ev.cluster))) : [];
    parts.push(h('div.section-title', 'Its review'),
      edits.length ? h('ul.inspector-list', ...edits.map(([e]) => h('li', `${e.kind === 'accept' ? `accepted "${e.label}"` : `relabeled "${e.label}"${e.kind === 'cluster' ? ' with its cluster' : ''}`}: ${e.reason} (${e.by}, ${relativeTime(e.time)})`))) : h('p.muted.seg-note', 'Not reviewed yet.'),
      decide(cell, v, shown, own));
    return parts;
  }

  function summary(counts, src, of) {
    if (!counts.length) return 'none labeled';
    return counts.slice(0, 3).map((c) => `${src.names[c.label]} ${c.count}`).join(', ') + ` of ${of}`;
  }

  // Accept or relabel, with a reason; then the next cell in the queue.
  function decide(cell, v, shown, own) {
    const names = shown.names;
    const choose = h('select.input', { id: 'rv-relabel', 'aria-label': 'The cell\'s label' }, ...names.map((n, k) => h('option', { value: n, selected: k === own }, n)), h('option', { value: '' }, 'A new label…'));
    const fresh = h('input.input', { type: 'text', id: 'rv-new', placeholder: 'The new label', hidden: true, 'aria-label': 'A new label' });
    choose.addEventListener('change', () => {
      fresh.hidden = choose.value !== '';
    });
    const why = h('input.input', { type: 'text', id: 'rv-why', placeholder: 'Why (kept with the decision)', 'aria-label': 'Why' });
    const by = h('input.input', { type: 'text', id: 'rv-by', value: st.reviewer ?? '', placeholder: 'Your name or initials', 'aria-label': 'Reviewer' });
    by.addEventListener('change', () => {
      st.reviewer = by.value.trim();
    });
    const commit = (kind) => {
      const reviewer = by.value.trim() || 'the user';
      st.reviewer = by.value.trim();
      const label = kind === 'accept' ? names[own] : (choose.value || fresh.value.trim());
      const reason = why.value.trim() || (kind === 'accept' ? 'Accepted on review' : '');
      try {
        if (kind === 'accept' && own < 0) throw new Error('The cell has no label to accept: relabel it.');
        const target = curatedFor(v, true);
        const next = appendEdit(app.store.project, target.id, { kind: kind === 'accept' ? 'accept' : 'cells', cells: [cell], label, reason, by: reviewer });
        app.store.commit(next, `${kind === 'accept' ? 'Accept' : 'Relabel'} cell #${cell} in ${target.name}`);
        if (v.kind !== 'curated') {
          rv.set = `${target.id}:0`;
        }
        // The next in the queue.
        const q = rv.queue;
        if (q) {
          const at = q.queue.findIndex((it) => it.cell === cell);
          if (at >= 0) q.queue.splice(at, 1);
          q.counts.reviewed += 1;
          const following = q.queue[Math.max(0, at)];
          ctx.render(true);
          if (following) setTimeout(() => app.labelReview?.focusCell(following.cell), 0);
          return;
        }
        ctx.render(true);
      } catch (error) {
        toast(error.message, { kind: 'error' });
      }
    };
    return h('div.rv-decide',
      h('div.seg-import-grid', h('label.field', { for: 'rv-relabel' }, h('span', 'Label'), choose), fresh, h('label.field', { for: 'rv-why' }, h('span', 'Why'), why), h('label.field', { for: 'rv-by' }, h('span', 'Reviewer'), by)),
      h('div.btn-row',
        h('button.btn.small.primary', { type: 'button', 'data-action': 'accept-label', disabled: own < 0, onclick: () => commit('accept') }, icon('check'), own >= 0 ? `Accept "${names[own]}"` : 'Accept'),
        h('button.btn.small', { type: 'button', 'data-action': 'relabel', onclick: () => {
          if (!why.value.trim()) {
            toast('Say why the cell is relabeled: the reason is kept with the decision.', { kind: 'error' });
            why.focus();
            return;
          }
          commit('relabel');
        } }, icon('edit'), 'Relabel')));
  }
}
