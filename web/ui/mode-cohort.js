// The Cohort view (wave 7): a study's sections compared as samples. Slice 1: the samples, each
// dataset a sample or its regions instead (a tissue microarray's cores, sections on one slide),
// with the user's fields and those read from each bundle, written and read as CSV (lib/samples.js).
// Slice 3: the design (ui/cohort-design.js). Slice 4: the same steps on every sample and the cohort
// table (ui/cohort-summary.js). Slice 5: cohort QC, batches and panels (ui/cohort-qc.js). Slice 6:
// differential abundance (ui/cohort-abundance.js). Slice 7: differential expression and spatial
// statistics between groups (ui/cohort-expression.js).

import { h, icon, clear, downloadBlob, formatCount } from './dom.js';
import { confirmDialog, toast } from './overlays.js';
import { promotedSegmentation } from '../lib/segmentations.js';
import { area, cellsIn, regionsOf } from '../lib/regions.js';
import { readCells } from '../lib/viewer-data.js';
import { readSegCells } from '../lib/segview.js';
import { designSection, draftOf, setDraft } from './cohort-design.js';
import { makeDesign } from '../lib/design.js';
import { batchStatements, itemIndex } from '../lib/cohort.js';
import { cohortSession, runSteps, stepsSection, summarize, summaryDocument, summarySection, summaryTable, updateSummaryProgress } from './cohort-summary.js';
import { qcSection } from './cohort-qc.js';
import { abundanceSection, abundanceSession, compareAbundance, comparisonDocument, showSampleCells } from './cohort-abundance.js';
import { compareExpression, compareSpatial, expressionDocument, expressionSection, expressionSession, geneValues, spatialDocument, spatialSession, spatialTestsSection } from './cohort-expression.js';
import { BUNDLE_FIELDS, FIELD_KINDS, addSampleField, fieldValue, readSamplesTable, removeSampleField, renameSample, renameSampleField, sampleFields, samplesCSV, samplesOf, setFieldKind, setRegionSamples, setSampleField } from '../lib/samples.js';

const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const mm2 = (um2) => `${(um2 / 1e6).toLocaleString('en-US', { minimumFractionDigits: 3, maximumFractionDigits: 3 })} mm²`;
const safeName = (s) => s.replace(/[^A-Za-z0-9._-]+/g, '_');

// The regions chosen as samples, being chosen, per dataset id, for the session.
const choosing = new Map();

export function mountCohort(app, container) {
  const crumbs = h('span.crumbs');
  const body = h('div.seg-body.cohort-body');
  const root = h('div.view.cohort', h('div.workbench-head', h('h1', icon('samples'), 'Cohort', crumbs)), h('div.workbench-scroll', body));
  container.append(root);
  // Each segmentation's cells (segmentation id → Promise<cells>), and each region sample's count of
  // them (key → number, or an Error).
  const tables = new Map();
  const counts = new Map();

  function commit(change, label) {
    try {
      app.store.commit(change(app.store.project), label);
      return true;
    } catch (error) {
      toast(error.message, { kind: 'error' });
      return false;
    }
  }

  // --- A sample's cells ----------------------------------------------------------------------------

  function cellsOf(item, seg) {
    if (!tables.has(seg.id)) {
      const url = seg.source === 'instrument' ? `${item.url}/cells` : `${item.url}/segmentations/${encodeURIComponent(seg.id)}/cells`;
      const p = fetch(url).then(async (response) => {
        if (!response.ok) throw new Error(`its cells: HTTP ${response.status}`);
        const buffer = await response.arrayBuffer();
        return seg.source === 'instrument' ? readCells(buffer) : readSegCells(buffer);
      });
      p.catch(() => tables.delete(seg.id));
      tables.set(seg.id, p);
    }
    return tables.get(seg.id);
  }

  // A sample's cells under its dataset's default segmentation: the dataset's all, or those in its
  // region, counted once they are read. Answers a number, '…' while counting, or null.
  function sampleCells(sample) {
    const seg = promotedSegmentation(app.store.project, sample.dataset.id)?.record;
    if (!seg) return null;
    if (!sample.region) return seg.summary?.cells ?? null;
    if (sample.removed) return null;
    const key = `${seg.id}|${sample.region.id}|${sample.region.version}`;
    if (counts.has(key)) {
      const v = counts.get(key);
      return v instanceof Error ? null : v;
    }
    const item = app.bundles.link(sample.dataset.id)?.item ?? app.bundles.itemByPath(sample.dataset.path);
    if (!item) return null;
    counts.set(key, '…');
    cellsOf(item, seg).then((cells) => {
      counts.set(key, cellsIn(sample.region.geometry, cells).length);
      render();
    }, (error) => counts.set(key, error));
    return '…';
  }

  // --- The samples table ---------------------------------------------------------------------------

  function cellInput(sample, field) {
    const value = fieldValue(sample, field.name);
    const input = h('input.input.small', {
      value: value === '' ? '' : String(value),
      inputmode: field.kind === 'number' ? 'decimal' : null,
      'aria-label': `${field.name} of ${sample.name}`,
      placeholder: '—',
      onchange: () => {
        if (!commit((p) => setSampleField(p, sample.id, field.name, input.value), `Set ${field.name} of ${sample.name}`)) input.value = value === '' ? '' : String(value);
      },
    });
    if (field.kind === 'number') input.classList.add('r');
    return input;
  }

  function nameInput(sample) {
    const input = h('input.input.small.cohort-name', {
      value: sample.name,
      'aria-label': `The name of sample ${sample.name}`,
      onchange: () => {
        if (!commit((p) => renameSample(p, sample.id, input.value), `Name sample ${sample.name}`)) input.value = sample.name;
      },
    });
    return input;
  }

  function samplesSection(project, samples) {
    const fields = sampleFields(project);
    const focus = app.store.ui.focus?.kind === 'sample' ? app.store.ui.focus.id : null;
    const head = h('tr',
      h('th', 'Sample'), h('th', 'Dataset'), h('th', 'Region'), h('th.r', 'Cells'), h('th.r', 'Area'),
      ...fields.map((f) => h(`th${f.kind === 'number' ? '.r' : ''}`, f.name, h('span.muted', f.kind === 'number' ? ' (number)' : ''))),
      h('th.cohort-bundle', { title: 'Read from each bundle' }, 'Run · slide · panel'));
    const rows = samples.map((s) => {
      const cells = sampleCells(s);
      const region = s.region ? (s.removed ? h('span.badge.warn', { title: 'Its region was removed: it is left out of the cohort' }, `${s.region?.name ?? 'region'} (removed)`) : s.region.name) : h('span.muted', 'whole section');
      return h(`tr${focus === s.id ? '.selected' : ''}`, { 'data-sample': s.id },
        h('td', nameInput(s)),
        h('td', h('button.link', { type: 'button', title: `Show ${s.dataset.name} in the Data view`, onclick: () => { app.focusItem({ kind: 'dataset', id: s.dataset.id }); app.setMode('data'); } }, s.dataset.name)),
        h('td', region),
        h('td.r', cells === '…' ? h('span.muted', '…') : exact(cells)),
        h('td.r', s.region ? mm2(area(s.region.geometry)) : h('span.muted', '—')),
        ...fields.map((f) => h('td', cellInput(s, f))),
        h('td.cohort-bundle', { title: BUNDLE_FIELDS.map((f) => `${f.label}: ${f.of(s.dataset) || 'not recorded'}`).join('\n') }, ['run', 'slide', 'panel'].map((k) => BUNDLE_FIELDS.find((f) => f.key === k).of(s.dataset) || '—').join(' · ')));
    });
    const tableInput = h('input', {
      type: 'file', accept: '.csv,.tsv,.txt,text/csv,text/tab-separated-values', hidden: true,
      onchange: async (event) => {
        const file = event.target.files?.[0];
        event.target.value = '';
        if (file) readTable(await file.text(), file.name);
      },
    });
    return h('section.seg-section',
      h('div.section-title', 'Samples'),
      h('div.seg-card',
        h('p', 'Each section is a sample, or its regions are instead (a tissue microarray\'s cores, sections on one slide). Describe each sample with its fields: the subject it came from, its condition, and any you add. Its run, slide and panel are read from its bundle (with its Xenium region and software, in the cell\'s tip).'),
        h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-table', h('thead', head), h('tbody', ...rows))),
        h('div.btn-row',
          h('button.btn.small', { type: 'button', title: 'The samples and their fields as CSV, to fill in elsewhere and read back', onclick: () => downloadBlob(new Blob([samplesCSV(app.store.project)], { type: 'text/csv' }), `${safeName(app.store.project.name)}-samples.csv`) }, icon('download'), 'Write CSV'),
          h('button.btn.small', { type: 'button', title: 'Read a table of samples\' fields (CSV or tab-separated), matched by its sample column', onclick: () => tableInput.click() }, icon('upload'), 'Read a table…'),
          tableInput),
        h('p.muted.seg-note', 'A table read sets the fields of the samples its sample column names (an empty cell clears a value) and adds its other columns as fields. Missing values are left empty, never zero.')));
  }

  function readTable(text, name) {
    try {
      const result = readSamplesTable(app.store.project, text);
      app.store.commit(result.project, `Read the samples' table ${name}`);
      const parts = [`${result.matched.length} sample${result.matched.length === 1 ? '' : 's'} matched`];
      if (result.added.length) parts.push(`fields added: ${result.added.map((f) => f.name).join(', ')}`);
      if (result.untouched.length) parts.push(`not in the table: ${result.untouched.join(', ')}`);
      toast(`${name}: ${parts.join('; ')}.`, { kind: 'ok' });
      if (result.unmatched.length) toast(`No sample is named ${result.unmatched.slice(0, 5).join(', ')}${result.unmatched.length > 5 ? ` or ${result.unmatched.length - 5} more` : ''}: those rows were not read.`, { kind: 'error' });
    } catch (error) {
      toast(`${name}: ${error.message}`, { kind: 'error' });
    }
  }

  // --- The fields ----------------------------------------------------------------------------------

  function fieldsSection(project) {
    const fields = sampleFields(project);
    const rows = fields.map((f) => {
      const rename = h('input.input.small', {
        value: f.name, 'aria-label': `Rename the field ${f.name}`,
        onchange: () => {
          if (!commit((p) => renameSampleField(p, f.name, rename.value), `Rename the field ${f.name}`)) rename.value = f.name;
        },
      });
      const kind = h('select.input.small', {
        'aria-label': `What the field ${f.name} holds`,
        onchange: () => {
          if (!commit((p) => setFieldKind(p, f.name, kind.value), `Make ${f.name} ${kind.value}`)) kind.value = f.kind;
        },
      }, ...FIELD_KINDS.map((k) => h('option', { value: k, selected: k === f.kind }, k === 'number' ? 'a number' : 'text')));
      const values = samplesOf(project).filter((s) => fieldValue(s, f.name) !== '').length;
      return h('tr',
        h('td', rename), h('td', kind), h('td.r', exact(values)),
        h('td', h('button.btn.small.ghost.danger', {
          type: 'button',
          onclick: async () => {
            if (values && !(await confirmDialog({ title: `Remove ${f.name}?`, message: `Its ${values} value${values === 1 ? '' : 's'} go with it.`, confirm: 'Remove', danger: true }))) return;
            commit((p) => removeSampleField(p, f.name), `Remove the field ${f.name}`);
          },
        }, icon('trash'), 'Remove')));
    });
    const name = h('input.input.small', { placeholder: 'age, treatment, batch…', 'aria-label': 'The new field\'s name' });
    const kind = h('select.input.small', { 'aria-label': 'What the new field holds' }, ...FIELD_KINDS.map((k) => h('option', { value: k }, k === 'number' ? 'a number' : 'text')));
    const add = () => {
      if (commit((p) => addSampleField(p, name.value, kind.value), `Add the field ${name.value.trim()}`)) name.value = '';
    };
    name.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') add();
    });
    return h('section.seg-section',
      h('div.section-title', 'Fields'),
      h('div.seg-card',
        h('table.data.seg-table', h('thead', h('tr', h('th', 'Field'), h('th', 'Holds'), h('th.r', 'Samples with a value'), h('th', h('span.sr-only', 'Remove')))), h('tbody', ...rows)),
        h('div.btn-row.cohort-add', name, kind, h('button.btn.small', { type: 'button', onclick: add }, icon('plus'), 'Add a field'))));
  }

  // --- Regions as samples --------------------------------------------------------------------------

  function regionsSection(project, samples) {
    const withRegions = project.datasets.filter((d) => regionsOf(project, d.id).length);
    if (!withRegions.length) return null;
    const cards = withRegions.map((d) => {
      const regions = regionsOf(project, d.id);
      const current = new Set(samples.filter((s) => s.dataset.id === d.id && s.region).map((s) => s.region.id));
      if (!choosing.has(d.id)) choosing.set(d.id, new Set(current));
      const chosen = choosing.get(d.id);
      const boxes = regions.map((r) => h('label.check', h('input', {
        type: 'checkbox', checked: chosen.has(r.id),
        onchange: (e) => {
          if (e.target.checked) chosen.add(r.id);
          else chosen.delete(r.id);
          render();
        },
      }), r.name, h('span.muted', ` · ${r.cls} · ${mm2(area(r.geometry))}`)));
      const same = chosen.size === current.size && [...chosen].every((id) => current.has(id));
      const lost = samples.filter((s) => s.dataset.id === d.id && s.region && !chosen.has(s.region.id) && Object.keys(s.fields).length).length;
      const apply = async () => {
        if (lost && !(await confirmDialog({ title: 'Drop region samples?', message: `${lost} region sample${lost === 1 ? '' : 's'} not chosen ${lost === 1 ? 'has' : 'have'} fields, which go with ${lost === 1 ? 'it' : 'them'}.`, confirm: 'Drop', danger: true }))) return;
        commit((p) => setRegionSamples(p, d.id, regions.filter((r) => chosen.has(r.id)).map((r) => r.id)), chosen.size ? `${d.name}'s regions as samples` : `${d.name} a sample again`);
      };
      return h('div.cohort-regions',
        h('h3.seg-card-title', d.name, h('span.muted', current.size ? ` · ${current.size} region${current.size === 1 ? '' : 's'} as samples` : ' · the whole section is one sample')),
        h('div.cohort-boxes', ...boxes),
        h('div.btn-row',
          h('button.btn.small', { type: 'button', disabled: same || !chosen.size, onclick: apply }, icon('samples'), same && chosen.size ? 'These regions are the samples' : `Make the ${chosen.size || ''} chosen region${chosen.size === 1 ? '' : 's'} samples`),
          current.size ? h('button.btn.small.ghost', { type: 'button', onclick: () => { chosen.clear(); apply(); } }, 'The whole section as one sample') : null));
    });
    return h('section.seg-section',
      h('div.section-title', 'Regions as samples'),
      h('div.seg-card',
        h('p', 'A section can hold several samples: a tissue microarray\'s cores, or sections placed on one slide. Draw a region around each (or read them from QuPath), then make them samples instead of the section. Regions of one section of one tissue are not independent samples: the design will say so.'),
        ...cards));
  }

  // --- The view ------------------------------------------------------------------------------------

  function render() {
    const project = app.store.project;
    const samples = samplesOf(project);
    clear(body);
    crumbs.textContent = project.datasets.length ? ` · ${samples.length} sample${samples.length === 1 ? '' : 's'} from ${project.datasets.length} dataset${project.datasets.length === 1 ? '' : 's'}` : '';
    const waiting = app.bundles.notAdded();
    if (waiting.length) {
      body.append(h('div.callout.accent', { style: { marginBottom: '14px' } },
        h('p', { style: { margin: '0 0 8px' } }, `${waiting.length} bundle${waiting.length === 1 ? ' is' : 's are'} open on this computer and not in the project: ${waiting.map((i) => i.name).join(', ')}.`),
        h('button.btn.small', {
          type: 'button',
          onclick: async () => {
            const { added, refused } = await app.bundles.addOpen();
            if (added.length) toast(`${added.length} bundle${added.length === 1 ? '' : 's'} added to the project.`, { kind: 'ok' });
            for (const r of refused) toast(`${r.name}: ${r.reason}`, { kind: 'error' });
          },
        }, icon('plus'), waiting.length === 1 ? 'Add it to the project' : `Add all ${waiting.length} to the project`)));
    }
    if (!project.datasets.length) {
      body.append(h('div.planned', h('div.empty', icon('samples'), h('h3', 'No samples yet'),
        h('p', 'A cohort compares sections as samples, with the sample or the subject, not the cell, as the unit. Open the sections\' bundles (several at once, or a folder of them) and add them to the project.'),
        h('div.btn-row', { style: { marginTop: '12px' } }, h('button.btn.primary', { type: 'button', onclick: () => app.openPathDialog() }, icon('bundle'), 'Open bundles')))));
      return;
    }
    if (samples.length === 1) body.append(h('p.muted.seg-note', { style: { marginBottom: '12px' } }, 'One sample so far: a cohort compares several. Add the other sections, or make regions of this one samples.'));
    body.append(samplesSection(project, samples), fieldsSection(project));
    const regions = regionsSection(project, samples);
    if (regions) body.append(regions);
    body.append(designSection(app, { commit, redraw: () => schedule() }));
    body.append(stepsSection(app, { cellsOf, redraw: () => schedule() }), summarySection(app, { cellsOf, redraw: () => schedule() }), qcSection(app, { redraw: () => schedule() }), abundanceSection(app, { cellsOf, redraw: () => schedule() }),
      expressionSection(app, { redraw: () => schedule() }), spatialTestsSection(app, { redraw: () => schedule() }));
    const focus = app.store.ui.focus?.kind === 'sample' ? app.store.ui.focus.id : null;
    if (focus) body.querySelector(`tr[data-sample="${CSS.escape(focus)}"]`)?.scrollIntoView({ block: 'nearest' });
  }

  // The design last shown from the project tree.
  let shownDesign = null;

  // Renders, the focus kept on the control of the same name.
  function renderKeepingFocus() {
    const active = document.activeElement;
    const inside = root.contains(active) && active !== body;
    const label = inside ? active.getAttribute('aria-label') : null;
    const id = inside ? active.id : '';
    const range = (label || id) && active.matches('input') ? [active.selectionStart, active.selectionEnd] : null;
    render();
    const next = label ? body.querySelector(`[aria-label="${CSS.escape(label)}"]`) : id ? body.querySelector(`#${CSS.escape(id)}`) : null;
    next?.focus();
    // The text as the keyboard left it selected (Tab selects a cell's whole value).
    if (next && range && next.matches('input')) next.setSelectionRange(range[0], range[1]);
  }

  // Renders once the focus has settled (an edit is saved as its cell is left, before the keyboard's
  // next cell has the focus); while a cell holds typing not yet saved, when it is left.
  let pending = false;
  function schedule() {
    if (pending) return;
    pending = true;
    setTimeout(() => {
      pending = false;
      const active = document.activeElement;
      if (root.contains(active) && active.matches('input.input') && active.value !== active.defaultValue) {
        active.addEventListener('blur', schedule, { once: true });
        return;
      }
      renderKeepingFocus();
    }, 0);
  }

  app.cohortView = {
    // Reads a samples' table (as text), as Read a table does.
    readTable,
    // Shows a design's specification (and its name) in the Design section.
    showDesign(spec, name) {
      setDraft(app.store.project, spec, name);
      schedule();
    },
    // Runs the same steps on every sample (cohort-summary.js runSteps): { markers, level, kind,
    // questions }; answers what each sample has.
    steps: (options = {}) => runSteps(app, cellsOf, options, (text, done, total) => {
      cohortSession.running = done < total ? { text, done, total } : null;
      schedule();
    }).finally(() => {
      cohortSession.running = null;
      schedule();
    }),
    // Summarizes the samples (cohort-summary.js summarize): { markerSet, level, niches }; answers the
    // summary's manifest, shown in the view.
    summarize: (options = {}) => summarize(app, cellsOf, options, () => schedule()),
    // A summary's table (lib/cohort.js readCohortTable), shown in the view.
    table: async (id, family = null) => {
      cohortSession.shown = id;
      if (family) cohortSession.family = family;
      schedule();
      return summaryTable(id);
    },
    // A summary's cohort QC: its document, and the design draft's sentences about batches.
    qc: async (id) => {
      cohortSession.shown = id;
      schedule();
      const doc = await summaryDocument(id);
      const design = makeDesign(app.store.project, draftOf(app.store.project).spec);
      return { doc, design: { group: design.spec.group, levels: design.levels, balance: design.balance, statements: batchStatements(design) } };
    },
    // Compares the abundances (cohort-abundance.js compareAbundance): { families, level, summary,
    // spec }; answers the comparison's manifest, shown in the view.
    abundance: (options = {}) => compareAbundance(app, options, () => schedule()),
    // A comparison's document, shown in the view (a family and a result chosen when given).
    comparison: async (id, { family = null, pick = null } = {}) => {
      abundanceSession.shown = id;
      if (family) abundanceSession.family = family;
      if (pick) abundanceSession.pick = pick;
      schedule();
      return comparisonDocument(id);
    },
    // Compares the labels' expression (cohort-expression.js compareExpression): { level, minCells,
    // summary, spec }; answers the comparison's manifest, shown in the view.
    expression: (options = {}) => compareExpression(app, options, () => schedule()),
    // An expression comparison's document, shown in the view (a label, contrast and gene chosen
    // when given).
    expressionResult: async (id, { label = null, contrast = null, gene = null } = {}) => {
      expressionSession.shown = id;
      if (label) expressionSession.label = label;
      if (contrast) expressionSession.contrast = contrast;
      if (gene) {
        expressionSession.gene = gene;
        expressionSession.filter = '';
      }
      schedule();
      return expressionDocument(id);
    },
    // One gene's values in a label's rows (api_expression.go expressionValues).
    geneValues: (id, label, gene) => geneValues(id, label, gene),
    // Compares the samples' spatial statistics (cohort-expression.js compareSpatial): { families,
    // level, summary, spec }; answers the comparison's manifest, shown in the view.
    spatial: (options = {}) => compareSpatial(app, options, () => schedule()),
    // A spatial comparison's document, shown in the view (a family and a test chosen when given).
    spatialResult: async (id, { family = null, pick = null } = {}) => {
      spatialSession.shown = id;
      if (family) spatialSession.family = family;
      if (pick) spatialSession.pick = pick;
      schedule();
      return spatialDocument(id);
    },
    // A sample's cells of a label shown on its tissue (cohort-abundance.js showSampleCells).
    sampleCells: (summaryId, sampleId, label) => showSampleCells(app, cellsOf, summaryId, sampleId, label),
    // The samples' panels compared now, before any summary (api_cohort.go comparePanels).
    panels: async () => {
      const samples = samplesOf(app.store.project).filter((s) => !s.removed);
      const body = { samples: [] };
      for (const s of samples) {
        const item = app.bundles.link(s.dataset.id)?.item ?? app.bundles.itemByPath(s.dataset.path);
        if (!item) throw new Error(`${s.name}: its bundle is not open on this computer`);
        body.samples.push({ sample: s.id, name: s.name, item: itemIndex(item) });
      }
      const response = await fetch('/api/cohort/panels', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      const answer = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(answer.error ?? `HTTP ${response.status}`);
      return answer;
    },
    // Each sample's cells as the table shows them, counted where they need counting.
    async cells() {
      for (let k = 0; k < 600; k += 1) {
        const all = samplesOf(app.store.project).map((s) => sampleCells(s));
        if (!all.includes('…')) return all;
        await new Promise((r) => setTimeout(r, 50));
      }
      return samplesOf(app.store.project).map((s) => sampleCells(s));
    },
  };

  return {
    update(topics) {
      // A design chosen in the project tree is shown in the Design section.
      const focus = app.store.ui.focus;
      if (topics.has('focus') && focus?.kind === 'cohort-summary') cohortSession.shown = focus.id;
      if (topics.has('focus') && focus?.kind === 'abundance') abundanceSession.shown = focus.id;
      if (topics.has('focus') && focus?.kind === 'expression') expressionSession.shown = focus.id;
      if (topics.has('focus') && focus?.kind === 'spatial-comparison') spatialSession.shown = focus.id;
      if (topics.has('focus') && focus?.kind === 'design' && focus.id !== shownDesign) {
        const d = app.store.project.artifacts.find((a) => a.kind === 'design' && a.id === focus.id);
        if (d) setDraft(app.store.project, d.spec, d.name);
        shownDesign = focus.id;
      }
      if (topics.has('project') || topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('local') || topics.has('mode')) schedule();
      else if (topics.has('jobs')) updateSummaryProgress(root, app);
    },
    destroy() {
      if (app.cohortView) delete app.cohortView;
      root.remove();
    },
  };
}
