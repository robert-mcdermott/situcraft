// The Reports view (wave 8, slice 1): results others can check. Its first part, the figures: the
// project's figures (lib/figures.js), each a page of panels drawn from the project. A figure is
// made from a saved view (a tissue panel: the view's place and look at the figure's size, a scale
// bar and a legend), laid out and lettered, previewed as it will print, and written as SVG, PDF or
// PNG with its manifest inside (ui/figures.js). A figure written earlier is read back and compared
// with the project: what changed since, and whether each panel draws the same now.

import { h, icon, clear, downloadBlob, debounce } from './dom.js';
import { confirmDialog, promptDialog, showDialog, toast } from './overlays.js';
import { FIGURE_SIZES, addPanel, createFigure, figureSize, figuresOf, panelLetters, panelSummary, removeFigure, removePanel, saveFigure, toMM, updatePanel } from '../lib/figures.js';
import { drawFigure, paintFigure, readBackFigure, writeFigure } from './figures.js';

// A tissue panel's region from a saved view: its center, and the width a Viewer a thousand CSS
// pixels wide shows at its zoom.
export function viewRegion(view) {
  return { cx: view.camera.cx, cy: view.camera.cy, width: Number((view.camera.umPerCss * 1000).toPrecision(6)) };
}

// The project's saved views a tissue panel can show (the Viewer's).
export function tissueViews(project) {
  return (project.views ?? []).filter((v) => (v.mode ?? 'viewer') === 'viewer');
}

export function mountReports(app, container) {
  const list = h('div.reports-list');
  const editor = h('div.reports-editor');
  const root = h('div.view.reports',
    h('div.workbench-head', h('h1', icon('report'), 'Reports')),
    h('div.workbench-scroll', h('div.reports-body',
      h('p.muted.reports-intro', 'Results others can check. A figure is drawn from the project, a panel at a time: a saved view of the tissue at the figure\'s size, with a scale bar and a legend of what it shows. It is written as SVG, PDF (its text as text) or PNG, each carrying what it was drawn from, so that SituCraft can read it back and say whether the project still draws it the same.'),
      h('div.section-title', 'Figures'),
      h('div.btn-row.reports-actions',
        h('button.btn.primary', { type: 'button', onclick: () => newFigure() }, icon('plus'), 'New figure'),
        h('button.btn', { type: 'button', onclick: () => readBack() }, icon('upload'), 'Read a figure back…')),
      list,
      editor)));
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
          numberField('Transcripts\' points (pt)', panel.pointSize ?? 1.2, (v) => v > 0 && v <= 12 && change({ pointSize: v }, 'Size the points'), { step: '0.1', min: '0.1', max: '12' })));
    } else if (panel.kind === 'text') {
      body.push(h('label.field', h('span', 'Text'), h('textarea.input', { rows: 2, onchange: (e) => change({ text: e.target.value }, 'Edit a text panel') }, panel.text)));
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
        h('button.btn.small', { type: 'button', disabled: !views.length, onclick: () => {
          const v = views[0];
          commit(addPanel(fig, { kind: 'tissue', view: v.id, region: viewRegion(v) }, { columns: 2 }).figure, `Add a panel to ${fig.name}`);
        } }, icon('plus'), 'Add a tissue panel'),
        h('button.btn.small', { type: 'button', onclick: () => commit(addPanel(fig, { kind: 'text', text: 'Text' }, { columns: 2, h: 40 }).figure, `Add a text panel to ${fig.name}`) }, icon('plus'), 'Add text')),
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

  const later = debounce(() => render(), 50);
  render(true);
  app.reportsView = {
    select(id) {
      selected = id;
      render(true);
    },
    drawn: () => Number(root.dataset.drawn ?? 0),
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
