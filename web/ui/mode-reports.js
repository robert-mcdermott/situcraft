// The Reports view (wave 8, slice 1): results others can check. Its first part, the figures: the
// project's figures (lib/figures.js), each a page of panels drawn from the project. A figure is
// made from a saved view (a tissue panel: the view's place and look at the figure's size, a scale
// bar and a legend), laid out and lettered, previewed as it will print, and written as SVG, PDF or
// PNG with its manifest inside (ui/figures.js). A figure written earlier is read back and compared
// with the project: what changed since, and whether each panel draws the same now. Below them,
// the methods (slice 4, ui/methods.js) and what made a result (slice 3, ui/record-view.js).

import { h, icon, clear, downloadBlob, debounce } from './dom.js';
import { confirmDialog, promptDialog, showDialog, showMenu, toast } from './overlays.js';
import { COHORT_PLOTS, FIGURE_SIZES, addPanel, createFigure, figureSize, figuresOf, panelLetters, panelSummary, removeFigure, removePanel, saveFigure, toMM, updatePanel } from '../lib/figures.js';
import { QC_METRICS } from './figure-results.js';
import { drawFigure, paintFigure, readBackFigure, writeFigure } from './figures.js';
import { mountRecord } from './record-view.js';
import { mountMethods } from './methods.js';

// A tissue panel's region from a saved view: its center, and the width a Viewer a thousand CSS
// pixels wide shows at its zoom.
export function viewRegion(view) {
  return { cx: view.camera.cx, cy: view.camera.cy, width: Number((view.camera.umPerCss * 1000).toPrecision(6)) };
}

// The project's saved views a tissue panel can show (the Viewer's).
export function tissueViews(project) {
  return (project.views ?? []).filter((v) => (v.mode ?? 'viewer') === 'viewer');
}

// What the project's result panels can draw: its maps, spatial answers drawn in figures, and
// comparisons of groups.
export function resultSources(project) {
  const of = (kinds) => project.artifacts.filter((a) => kinds.includes(a.kind) && (a.state ?? 'complete') === 'complete');
  return {
    maps: of(['embedding']),
    analyses: of(['spatial']).filter((a) => ['enrichment', 'distances', 'moran'].includes(a.question ?? a.summary?.question)),
    comparisons: of(['abundance', 'expression', 'spatial-comparison']),
  };
}

// A new panel of a kind, of the first thing of that kind the project has.
export function newPanel(project, kind) {
  const src = resultSources(project);
  if (kind === 'tissue') {
    const v = tissueViews(project)[0];
    return v ? { kind, view: v.id, region: viewRegion(v) } : null;
  }
  if (kind === 'qc') return project.datasets[0] ? { kind, dataset: project.datasets[0].id, metric: 'transcripts' } : null;
  if (kind === 'map') return src.maps[0] ? { kind, map: src.maps.at(-1).id, color: 'clusters' } : null;
  if (kind === 'spatial') return src.analyses[0] ? { kind, analysis: src.analyses.at(-1).id } : null;
  if (kind === 'cohort') {
    const c = src.comparisons.at(-1);
    return c ? { kind, comparison: c.id, plot: c.kind === 'expression' ? 'volcano' : 'forest' } : null;
  }
  return { kind: 'text', text: 'Text', h: 40 };
}

export function mountReports(app, container) {
  const list = h('div.reports-list');
  const editor = h('div.reports-editor');
  const record = mountRecord(app);
  const methods = mountMethods(app);
  const root = h('div.view.reports',
    h('div.workbench-head', h('h1', icon('report'), 'Reports')),
    h('div.workbench-scroll', h('div.reports-body',
      h('p.muted.reports-intro', 'Results others can check. A figure is drawn from the project, a panel at a time: a saved view of the tissue at the figure\'s size, with a scale bar and a legend of what it shows. It is written as SVG, PDF (its text as text) or PNG, each carrying what it was drawn from, so that SituCraft can read it back and say whether the project still draws it the same.'),
      h('div.section-title', 'Figures'),
      h('div.btn-row.reports-actions',
        h('button.btn.primary', { type: 'button', onclick: () => newFigure() }, icon('plus'), 'New figure'),
        h('button.btn', { type: 'button', onclick: () => readBack() }, icon('upload'), 'Read a figure back…')),
      list,
      editor,
      methods.element,
      record.element)));
  container.append(root);
  let selected = app.reportsFocus ?? null;
  app.reportsFocus = null;
  let shape = '';
  let generation = 0;

  const project = () => app.store.project;
  const figure = () => figuresOf(project()).find((f) => f.id === selected) ?? null;
  const commit = (fig, what) => app.store.commit(saveFigure(project(), fig), what);

  async function newFigure() {
    const views = tissueViews(project());
    if (!views.length) {
      toast('Save a view of the tissue first (in the Viewer, Save view): a figure\'s tissue panel shows a saved view.', { kind: 'error', timeout: 8000 });
      return;
    }
    const name = await promptDialog({ title: 'New figure', label: 'Its name', value: `Figure ${figuresOf(project()).length + 1}`, confirm: 'Make it' });
    if (!name) return;
    let fig = createFigure(name, 'double');
    fig = addPanel(fig, { kind: 'tissue', view: views[0].id, region: viewRegion(views[0]) }).figure;
    commit(fig, `New figure ${fig.name}`);
    selected = fig.id;
    render(true);
  }

  async function readBack() {
    const input = h('input', { type: 'file', accept: '.svg,.pdf,.png,.json' });
    input.addEventListener('change', async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        const bytes = new Uint8Array(await file.arrayBuffer());
        const read = await readBackFigure(app, bytes);
        if (!read) {
          toast(`${file.name} is not a figure SituCraft wrote: it carries no manifest.`, { kind: 'error' });
          return;
        }
        showReadBack(file.name, read);
      } catch (error) {
        toast(`${file.name} could not be read: ${error.message}`, { kind: 'error' });
      }
    });
    input.click();
  }

  function showReadBack(name, read) {
    const { manifest, comparison, redrawn } = read;
    const statusText = { unchanged: 'Unchanged', changed: 'Changed', missing: 'Missing' };
    showDialog({
      title: `${name}: ${manifest.figure.name}`,
      width: 'wide',
      content: [
        h('p', comparison.intact ? `Written by SituCraft ${manifest.software.version ?? ''}${manifest.software.commit ? ` (${manifest.software.commit.slice(0, 7)})` : ''} on ${manifest.created ?? 'an unknown date'}, from ${comparison.sameProject ? 'this project' : `the project "${manifest.project.name}"`}.` : `Its manifest is not as it was written: ${comparison.problem}`),
        h('p.muted', manifest.description),
        h('table.table', h('thead', h('tr', h('th', 'Panel'), h('th', 'Shows'), h('th', 'Since'), h('th', 'Drawn again'))),
          h('tbody', ...comparison.panels.map((p) => {
            const again = redrawn.find((r) => r.id === p.id);
            return h('tr', h('td', p.letter ?? p.id), h('td', p.shows), h('td', `${statusText[p.status]}${p.changes.length ? `: ${p.changes.join('; ')}` : ''}`), h('td', again ? (again.same ? 'The same' : 'Differently') : '—'));
          }))),
        redrawn.find((r) => r.error) ? h('p.muted', `It could not be drawn again: ${redrawn.find((r) => r.error).error}`) : null,
      ],
    });
  }

  async function write(fig, format) {
    const busy = toast(`Writing ${fig.name} as ${format.toUpperCase()}…`, { timeout: 0 });
    try {
      const out = await writeFigure(app, fig, format, { dpi: 300 });
      downloadBlob(new Blob([out.bytes], { type: out.mime }), out.name);
      const notes = out.drawn.notes;
      toast(`Wrote ${out.name}${notes.length ? `; ${notes.join(' ')}` : '.'}`, { kind: notes.length ? 'warn' : 'ok', timeout: notes.length ? 10000 : 4000 });
    } catch (error) {
      toast(`${fig.name} could not be written: ${error.message}`, { kind: 'error' });
    } finally {
      busy.remove();
    }
  }

  function card(fig) {
    const letters = panelLetters(fig);
    return h(`article.card.clickable${fig.id === selected ? '.selected' : ''}`, { 'data-figure': fig.id, tabindex: '0', onclick: () => { selected = fig.id; render(true); }, onkeydown: (e) => { if (e.key === 'Enter') { selected = fig.id; render(true); } } },
      h('h4', icon('report'), fig.name),
      h('p', `${toMM(fig.width)} × ${toMM(fig.height)} mm, ${fig.panels.length} panel${fig.panels.length === 1 ? '' : 's'}`),
      h('p.muted', fig.panels.map((p) => `${letters.get(p.id) ?? ''} ${panelSummary(p, project())}`.trim()).join('; ')));
  }

  function numberField(label, value, onchange, attrs = {}) {
    return h('label.field', h('span', label), h('input.input.small', { type: 'number', value: value ?? '', ...attrs, onchange: (e) => onchange(e.target.value === '' ? null : Number(e.target.value)) }));
  }

  // The fields of a result panel: what it draws, chosen among the project's.
  function resultFields(panel, change) {
    const p = project();
    const src = resultSources(p);
    const select = (label, value, options, onchange) => h('label.field', h('span', label), h('select.input.small', { onchange: (e) => onchange(e.target.value) },
      ...options.map(([v, text]) => h('option', { value: v, selected: v === value }, text))));
    const text = (label, value, onchange) => h('label.field', h('span', label), h('input.input.small', { type: 'text', value: value ?? '', onchange: (e) => onchange(e.target.value.trim()) }));
    if (panel.kind === 'qc') {
      return [
        select('Dataset', panel.dataset, p.datasets.map((d) => [d.id, d.name]), (v) => change({ dataset: v, filter: undefined }, 'Choose a panel\'s dataset')),
        select('Metric', panel.metric, QC_METRICS.map((m) => [m.id, m.label]), (v) => change({ metric: v }, 'Choose a panel\'s metric')),
        select('Scale', panel.log ? 'log' : 'linear', [['linear', 'Linear'], ['log', 'Logarithmic']], (v) => change({ log: v === 'log' ? true : undefined }, 'Choose a panel\'s scale')),
        select('Thresholds of a filter', panel.filter ?? '', [['', 'None'], ...p.artifacts.filter((a) => a.kind === 'filter' && a.dataset === panel.dataset).map((a) => [a.id, `${a.name}, version ${a.version}`])], (v) => change({ filter: v || undefined }, 'Choose a panel\'s filter')),
      ];
    }
    if (panel.kind === 'map') {
      const map = src.maps.find((m) => m.id === panel.map);
      const labels = p.artifacts.filter((a) => a.kind === 'labels' && a.segmentation === map?.segmentation);
      return [
        select('Map', panel.map, src.maps.map((m) => [m.id, m.name]), (v) => change({ map: v, color: 'clusters' }, 'Choose a panel\'s map')),
        select('Colored by', panel.color ?? 'clusters', [['clusters', 'Its clusters'], ...labels.flatMap((l) => (l.summary?.levels ?? ['Labels']).map((lv, k) => [`labels:${l.id}:${k}`, `${l.summary?.markers ?? l.name}: ${String(lv).toLowerCase()}`]))], (v) => change({ color: v }, 'Color a panel\'s map')),
      ];
    }
    if (panel.kind === 'spatial') return [select('Answer', panel.analysis, src.analyses.map((a) => [a.id, a.name]), (v) => change({ analysis: v }, 'Choose a panel\'s answer'))];
    if (panel.kind === 'cohort') {
      const c = src.comparisons.find((x) => x.id === panel.comparison);
      const plots = Object.entries(COHORT_PLOTS).filter(([k]) => (c?.kind === 'expression' ? ['volcano', 'gene'].includes(k) : ['forest', 'units'].includes(k)));
      const fields = [
        select('Comparison', panel.comparison, src.comparisons.map((x) => [x.id, x.name]), (v) => {
          const next = src.comparisons.find((x) => x.id === v);
          change({ comparison: v, plot: next?.kind === 'expression' ? 'volcano' : 'forest', family: undefined, label: undefined, gene: undefined, key: undefined }, 'Choose a panel\'s comparison');
        }),
        select('Plot', panel.plot, plots, (v) => change({ plot: v }, 'Choose a panel\'s plot')),
      ];
      if (c?.kind !== 'expression') fields.push(text('Family (share, density, niche; enrichment, nearest, moran)', panel.family, (v) => change({ family: v || undefined }, 'Choose a panel\'s family')));
      if (panel.plot === 'units' && c?.kind === 'spatial-comparison') fields.push(text('Statistic (as a|b)', panel.key, (v) => change({ key: v || undefined }, 'Choose a panel\'s statistic')));
      else if (panel.plot === 'units' || c?.kind === 'expression') fields.push(text('Label', panel.label, (v) => change({ label: v || undefined }, 'Choose a panel\'s label')));
      if (panel.plot === 'gene') fields.push(text('Gene', panel.gene, (v) => v && change({ gene: v }, 'Choose a panel\'s gene')));
      return fields;
    }
    return [];
  }

  function panelEditor(fig, panel, letter) {
    const views = tissueViews(project());
    const change = (patch, what) => commit(updatePanel(fig, panel.id, patch), what);
    const body = [];
    if (panel.kind === 'tissue') {
      const r = panel.region;
      body.push(
        h('label.field', h('span', 'Saved view'), h('select.input.small', { onchange: (e) => {
          const v = views.find((x) => x.id === e.target.value);
          change({ view: v.id, region: viewRegion(v) }, `Show ${v.name} in ${fig.name}`);
        } }, ...views.map((v) => h('option', { value: v.id, selected: v.id === panel.view }, v.name)))),
        h('div.reports-fields',
          numberField('Center x (µm)', r.cx, (v) => Number.isFinite(v) && change({ region: { ...r, cx: v } }, 'Move a panel'), { step: 'any' }),
          numberField('Center y (µm)', r.cy, (v) => Number.isFinite(v) && change({ region: { ...r, cy: v } }, 'Move a panel'), { step: 'any' }),
          numberField('Width (µm)', r.width, (v) => v > 0 && change({ region: { ...r, width: v } }, 'Size a panel\'s region'), { step: 'any', min: '1' }),
          numberField('Scale bar (µm; empty: chosen)', panel.scaleBar ?? null, (v) => change({ scaleBar: v > 0 ? v : undefined }, 'Set a scale bar'), { step: 'any', min: '0' }),
          numberField('Transcripts\' points (pt)', panel.pointSize ?? 1.2, (v) => v > 0 && v <= 12 && change({ pointSize: v }, 'Size the points'), { step: '0.1', min: '0.1', max: '12' })),
        h('label.viewer-toggle', h('input', { type: 'checkbox', checked: panel.regions === true, onchange: (e) => change({ regions: e.target.checked ? true : undefined }, 'Draw a panel\'s regions') }), 'Draw the dataset\'s regions'));
    } else if (panel.kind === 'text') {
      body.push(h('label.field', h('span', 'Text'), h('textarea.input', { rows: 2, onchange: (e) => change({ text: e.target.value }, 'Edit a text panel') }, panel.text)));
    } else {
      body.push(h('div.reports-fields', ...resultFields(panel, change)));
    }
    body.push(h('div.reports-fields',
      numberField('Width (pt)', panel.w, (v) => v >= 20 && change({ w: v }, 'Size a panel'), { step: 'any' }),
      numberField('Height (pt)', panel.h, (v) => v >= 20 && change({ h: v }, 'Size a panel'), { step: 'any' }),
      numberField('Left (pt)', panel.x, (v) => Number.isFinite(v) && change({ x: v }, 'Move a panel'), { step: 'any' }),
      numberField('Top (pt)', panel.y, (v) => Number.isFinite(v) && change({ y: v }, 'Move a panel'), { step: 'any' })));
    return h('div.seg-card.reports-panel', { 'data-panel': panel.id },
      h('div.seg-card-title', h('b', letter ? `Panel ${letter}` : panel.id), h('span.muted', ` · ${panelSummary(panel, project())}`), h('span.spacer'),
        h('button.btn.small.ghost', { type: 'button', title: 'Remove this panel', onclick: () => commit(removePanel(fig, panel.id), `Remove a panel of ${fig.name}`) }, icon('trash'), 'Remove')),
      ...body);
  }

  function figureEditor(fig) {
    const letters = panelLetters(fig);
    const views = tissueViews(project());
    const preview = h('canvas.reports-preview', { role: 'img', 'aria-label': `A preview of ${fig.name}` });
    const description = h('p.muted.reports-description', 'Drawing…');
    const notes = h('ul.reports-notes');
    const change = (patch, what) => commit({ ...fig, ...patch }, what);
    clear(editor).append(h('div.seg-card.reports-figure',
      h('div.seg-card-title', h('b', fig.name), h('span.spacer'),
        h('button.btn.small.ghost', { type: 'button', onclick: async () => {
          const name = await promptDialog({ title: 'Rename the figure', label: 'Its name', value: fig.name, confirm: 'Rename' });
          if (name && name.trim()) change({ name: name.trim() }, `Rename figure ${fig.name}`);
        } }, icon('edit'), 'Rename'),
        h('button.btn.small.ghost', { type: 'button', onclick: async () => {
          if (!(await confirmDialog({ title: 'Remove the figure?', message: `${fig.name} is removed from the project (files written from it are not).`, confirm: 'Remove', danger: true }))) return;
          app.store.commit(removeFigure(project(), fig.id), `Remove figure ${fig.name}`);
          selected = null;
        } }, icon('trash'), 'Remove')),
      h('div.reports-fields',
        h('label.field', h('span', 'Size'), h('select.input.small', { onchange: (e) => {
          const s = figureSize(e.target.value);
          change({ size: s.id, width: Math.round(s.width * 100) / 100, height: Math.round(s.height * 100) / 100 }, `Size figure ${fig.name}`);
        } }, ...FIGURE_SIZES.map((s) => h('option', { value: s.id, selected: s.id === fig.size }, s.label)))),
        numberField('Height (mm)', toMM(fig.height), (v) => v >= 25.4 && v <= 700 && change({ height: Math.round(((v * 72) / 25.4) * 100) / 100 }, `Size figure ${fig.name}`), { step: '1' }),
        numberField('Text (pt)', fig.text, (v) => v >= 6 && v <= 36 && change({ text: v }, `Set the text of ${fig.name}`), { step: '0.5', min: '6', max: '36' })),
      h('div.btn-row',
        h('button.btn.small', { type: 'button', 'aria-haspopup': 'menu', onclick: (e) => {
          const p = project();
          const src = resultSources(p);
          const add = (kind, what) => {
            const panel = newPanel(p, kind);
            if (panel) commit(addPanel(fig, panel, { columns: 2 }).figure, `Add ${what} to ${fig.name}`);
          };
          showMenu(e.currentTarget, [
            { label: 'The tissue (a saved view)', icon: 'eye', disabled: !views.length, hint: views.length ? '' : 'save a view in the Viewer', onSelect: () => add('tissue', 'a tissue panel') },
            { label: 'A QC distribution', icon: 'qc', disabled: !p.datasets.length, onSelect: () => add('qc', 'a QC panel') },
            { label: 'The cells\' map', icon: 'tag', disabled: !src.maps.length, hint: src.maps.length ? '' : 'map a clustering in Cell types', onSelect: () => add('map', 'a map') },
            { label: 'A spatial answer', icon: 'graph', disabled: !src.analyses.length, hint: src.analyses.length ? '' : 'enrichment, distances or Moran\'s I', onSelect: () => add('spatial', 'a spatial answer') },
            { label: 'A cohort\'s comparison', icon: 'samples', disabled: !src.comparisons.length, onSelect: () => add('cohort', 'a comparison') },
            '-',
            { label: 'Text', icon: 'edit', onSelect: () => add('text', 'a text panel') },
          ]);
        } }, icon('plus'), 'Add a panel')),
      h('div.reports-preview-wrap', preview),
      description,
      notes,
      h('div.btn-row',
        h('span.field-label', 'Write it as'),
        ...['svg', 'pdf', 'png'].map((f) => h('button.btn.small', { type: 'button', onclick: () => write(figure() ?? fig, f) }, icon('download'), f === 'png' ? 'PNG (300 dpi)' : f.toUpperCase())),
        h('button.btn.small.ghost', { type: 'button', title: 'What it shows and was drawn from, as JSON', onclick: () => write(figure() ?? fig, 'json') }, icon('download'), 'Its manifest')),
      h('div.section-title', 'Panels'),
      ...fig.panels.map((p) => panelEditor(fig, p, letters.get(p.id)))));
    drawPreview(fig, preview, description, notes);
  }

  async function drawPreview(fig, canvas, description, notes) {
    const mine = (generation += 1);
    try {
      const { drawn } = await drawFigure(app, fig, { dpi: 150 });
      if (mine !== generation) return;
      const wrap = canvas.parentElement;
      const width = Math.min(760, Math.max(200, (wrap?.clientWidth ?? 600) - 26));
      paintFigure(canvas, fig, drawn, (width / fig.width) * (window.devicePixelRatio || 1));
      canvas.style.width = `${width}px`;
      description.textContent = drawn.description;
      clear(notes).append(...drawn.notes.map((n) => h('li', n)));
      root.dataset.drawn = String(mine);
    } catch (error) {
      if (mine === generation) description.textContent = `It could not be drawn: ${error.message}`;
    }
  }

  function render(force = false) {
    const figs = figuresOf(project());
    if (selected && !figs.some((f) => f.id === selected)) selected = null;
    if (!selected && figs.length) selected = figs[0].id;
    const next = JSON.stringify([selected, figs, (project().views ?? []).map((v) => [v.id, v.name])]);
    if (!force && next === shape) return;
    shape = next;
    clear(list).append(...(figs.length ? figs.map(card) : [h('p.muted', 'No figure yet. Save a view of the tissue in the Viewer, then make a figure of it here.')]));
    const fig = figure();
    if (fig) figureEditor(fig);
    else clear(editor);
  }

  const later = debounce(() => {
    render();
    methods.update();
    record.update();
  }, 50);
  render(true);
  if (app.recordFocus) record.show(app.recordFocus, { scroll: true });
  if (app.methodsFocus) methods.show(app.methodsFocus, { scroll: true });
  app.recordFocus = null;
  app.methodsFocus = null;
  app.reportsView = {
    select(id) {
      selected = id;
      render(true);
    },
    drawn: () => Number(root.dataset.drawn ?? 0),
    // What made a result, shown in the section below the figures.
    showRecord(id) {
      return record.show(id, { scroll: true });
    },
    recordShown: () => record.shown(),
    // The methods of the project, a dataset or a result, in the section between them.
    showMethods(scope) {
      return methods.show(scope, { scroll: true });
    },
    methodsShown: () => methods.shown(),
  };
  return {
    update(topics) {
      if (topics.has('project') || topics.has('project-loaded') || topics.has('mode') || topics.has('bundles')) later();
    },
    destroy() {
      app.reportsView = null;
      root.remove();
    },
  };
}
