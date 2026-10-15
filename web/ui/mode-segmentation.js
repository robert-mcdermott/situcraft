// The Segmentation view: a dataset's segmentations side by side, by their numbers; SituCraft's
// nucleus expansion run from here (api_expansion.go); and other tools' segmentations imported
// (api_import.go). An expansion is compared with the instrument's segmentation it grew from, cell
// by cell, pixel by pixel and transcript by transcript, and what differs is described
// (internal/segment/expand). Comparing any two segmentations, reviewing their differences and
// promoting one come later in wave 2 (situcraft-spec/roadmap.md).

import { h, icon, clear, formatCount } from './dom.js';
import { toast } from './overlays.js';
import { comparisonsOf, promotedSegmentation, segmentationsOf } from '../lib/segmentations.js';

const DEFAULT_DISTANCE = 5;
const MAX_DISTANCE = 100;

const percent = (v, digits = 1) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');
const signedPercent = (v, digits = 2) => (Number.isFinite(v) ? `${v >= 0 ? '+' : '−'}${(100 * Math.abs(v)).toFixed(digits)}%` : '—');
const count = (v) => (Number.isFinite(v) ? formatCount(v) : '—');
const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const um = (v) => (Number.isFinite(v) ? `${v.toFixed(2)} µm` : '—');

// Per dataset id, the segmentation whose agreement is shown, the distance asked and the import
// being filled in, for the session.
const sessions = new Map();

// The formats imported: the files each needs, what it is, and its options worth changing.
export const IMPORT_FORMATS = {
  spatialdata: {
    label: 'SpatialData Zarr',
    paths: [{ label: 'Store', placeholder: '/data/sample.zarr', hint: 'its .zarr folder, or a zip of it' }],
    about: 'Cells from a shapes or labels element, each transcript\'s cell from a points element, and a count table, as the spatialdata package writes them (format 2 or 3). spatialdata-io\'s Xenium stores are in the morphology image\'s pixels.',
    options: ['cells', 'points', 'table', 'coordinate_system', 'cell_column'],
  },
  baysor: {
    label: 'Baysor',
    paths: [{ label: 'Assignments', placeholder: '/data/baysor/segmentation.csv', hint: 'segmentation.csv' }, { label: 'Polygons', placeholder: '/data/baysor/segmentation_polygons.json', hint: 'segmentation_polygons.json (optional)', optional: true }],
    about: 'Each transcript\'s cell from Baysor\'s segmentation.csv (by transcript_id when it kept them, else by position and gene) and the cells\' polygons.',
    options: ['cell_column', 'cell_property'],
  },
  geojson: {
    label: 'GeoJSON polygons',
    paths: [{ label: 'Polygons', placeholder: '/data/cells.geojson', hint: 'a FeatureCollection of polygons, each naming its cell' }, { label: 'Transcript table', placeholder: '/data/assignments.parquet', hint: 'CSV or Parquet: transcript_id and the cell (optional; else by the polygons)', optional: true }],
    about: 'Cell polygons, and each transcript\'s cell from a table when there is one, else from the polygon it lies in.',
    options: ['cell_property', 'cell_column'],
  },
  xenium: {
    label: 'Xenium Ranger output',
    paths: [{ label: 'Output bundle', placeholder: '/data/ranger/outs', hint: 'its folder, zip or experiment.xenium' }],
    about: 'A bundle of the same run segmented anew (Xenium Ranger import-segmentation or resegment): its cell mask, cells, boundaries and each transcript\'s cell.',
    options: [],
  },
};

// The classes of differences (internal/compare), with what each means.
const CLASSES = [
  ['one-to-one', 'The same cell in both, its area within 25%'],
  ['expanded', 'The same cell, more than 25% larger in B'],
  ['contracted', 'The same cell, more than 25% smaller in B'],
  ['split', 'One cell of A is several of B'],
  ['merged', 'Several cells of A are one of B'],
  ['rearranged', 'Several cells of each overlap one another'],
  ['only in A', 'A cell of A with no counterpart in B'],
  ['only in B', 'A cell of B with no counterpart in A'],
  ['assignment-only', 'Matched by their transcripts; one has no outline'],
];

const OPTION_LABELS = {
  cells: ['Cells element', 'shapes or labels; the table\'s, else one named for cells'],
  points: ['Points element', 'transcripts, or the only one'],
  table: ['Table', 'the one annotating the cells'],
  coordinate_system: ['Coordinate system', 'global'],
  cell_column: ['Cell column', 'cell_id, cell, assignment…'],
  cell_property: ['Cell property', 'cell_id, cell, id…'],
};

export function mountSegmentation(app, container) {
  const title = h('span.crumbs');
  const body = h('div.seg-body');
  const root = h('div.view.segmentation', h('div.workbench-head', h('h1', icon('segment'), 'Segmentation', title)), h('div.workbench-scroll', body));
  container.append(root);
  const live = new Map(); // job id → { bar, message }
  let shape = '';

  function target() {
    const { project, ui } = app.store;
    // A dataset in focus, or a segmentation's dataset, else the first.
    const seg = ui.focus?.kind === 'segmentation' ? project.artifacts.find((a) => a.kind === 'segmentation' && a.id === ui.focus.id) : null;
    const id = ui.focus?.kind === 'dataset' ? ui.focus.id : seg?.dataset ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id) ?? project.datasets[0];
    if (!dataset) return null;
    const link = app.bundles.link(dataset.id);
    return { dataset, item: link?.item ?? app.bundles.itemByPath(dataset.path), focused: seg?.dataset === dataset.id ? seg.id : null };
  }

  function sessionOf(dataset) {
    if (!sessions.has(dataset.id)) sessions.set(dataset.id, { shown: null, distance: DEFAULT_DISTANCE, compare: { a: '', b: '', shown: null }, import: { format: 'spatialdata', paths: ['', ''], units: 'um', fields: {}, tool: '', tool_version: '', citation: '' } });
    return sessions.get(dataset.id);
  }

  // The import's options, from the form's state.
  function importOptions(state) {
    const spec = IMPORT_FORMATS[state.format];
    const options = { format: state.format, paths: spec.paths.map((_, i) => (state.paths[i] ?? '').trim()).filter(Boolean), units: state.units };
    for (const key of spec.options) if ((state.fields[key] ?? '').trim()) options[key] = state.fields[key].trim();
    if (state.format !== 'xenium') {
      for (const key of ['tool', 'tool_version', 'citation']) if (state[key].trim()) options[key] = state[key].trim();
    }
    return options;
  }

  async function runImport(t, options) {
    const spec = IMPORT_FORMATS[options.format];
    if (!spec) throw new Error(`The format is one of ${Object.keys(IMPORT_FORMATS).join(', ')}.`);
    const needed = spec.paths.filter((p) => !p.optional).length;
    if ((options.paths ?? []).length < needed) throw new Error(`Name the ${spec.paths.filter((p) => !p.optional).map((p) => p.label.toLowerCase()).join(' and ')}.`);
    const manifest = await app.bundles.importSegmentation(t.item, t.dataset.id, options);
    render(true);
    return manifest;
  }

  async function importFromForm(t, session) {
    try {
      const manifest = await runImport(t, importOptions(session.import));
      if (manifest.state === 'complete') {
        toast(`${manifest.name} into ${t.dataset.name}: ${exact(manifest.summary?.cells)} cells.`, { kind: 'ok' });
      } else {
        const fatal = manifest.findings.find((f) => f.severity === 'fatal');
        toast(`${manifest.name} is ${manifest.state}: ${fatal?.message ?? 'its check found fatal problems'}`, { kind: 'error', timeout: 12000 });
      }
      app.focusItem({ kind: 'segmentation', id: manifest.id });
    } catch (error) {
      toast(`The import into ${t.dataset.name}: ${error.message}`, { kind: 'error', timeout: 12000 });
    }
  }

  async function expand(t, session, distance) {
    if (!(distance >= 0 && distance <= MAX_DISTANCE)) {
      toast(`The distance is from 0 to ${MAX_DISTANCE} µm.`, { kind: 'error' });
      return null;
    }
    session.distance = distance;
    try {
      const manifest = await app.bundles.expandNuclei(t.item, t.dataset.id, distance);
      session.shown = manifest.id;
      toast(`${manifest.name} of ${t.dataset.name}: ${exact(manifest.summary?.cells)} cells.`, { kind: 'ok' });
      render(true);
      return manifest;
    } catch (error) {
      toast(`Nucleus expansion of ${t.dataset.name}: ${error.message}`, { kind: 'error' });
      return null;
    }
  }

  // --- The segmentations, side by side ---
  function table(t, list, session) {
    const promoted = promotedSegmentation(app.store.project, t.dataset.id);
    const rows = list.map((a) => {
      const s = a.summary ?? {};
      const own = a.source === 'situcraft';
      return h(`tr${session.shown === a.id ? '.selected' : ''}`, {
        tabIndex: 0, 'data-segmentation': a.id, style: { cursor: 'pointer' },
        title: own ? 'Show its agreement with the instrument\'s segmentation, and it in the inspector' : 'Show it in the inspector',
        onclick: () => choose(a), onkeydown: (event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            choose(a);
          }
        },
      },
      h('td', h('b', a.name), promoted?.record.id === a.id ? h('span.badge.accent', { style: { marginLeft: '6px' } }, 'default') : null, a.state !== 'complete' ? h('span.badge.danger', { style: { marginLeft: '6px' } }, a.state) : null),
      h('td', own ? `SituCraft ${a.software.find((x) => x.name === 'situcraft')?.version ?? ''}` : a.source === 'imported' ? `${a.method.name}${a.method.version ? ` ${a.method.version}` : ''}, imported` : a.method.name.replace(/^Xenium cell segmentation \((.*)\)$/, '$1')),
      h('td.r', exact(s.cells)),
      h('td.r', percent(s.assignedFraction)),
      h('td.r', Number.isFinite(s.medianTranscriptsPerCell) ? String(s.medianTranscriptsPerCell) : '—'),
      h('td.r', Number.isFinite(s.medianAreaUm2) ? s.medianAreaUm2.toFixed(1) : '—'));
    });
    return h('section.seg-section',
      h('div.section-title', `Segmentations of ${t.dataset.name}`),
      list.length
        ? h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table',
          h('thead', h('tr', h('th', 'Segmentation'), h('th', 'Made by'), h('th.r', 'Cells'), h('th.r', { title: 'Gene transcripts of quality 20 or more assigned to a cell' }, 'Assigned'), h('th.r', { title: 'Gene transcripts of quality 20 or more per cell, median' }, 'Transcripts per cell'), h('th.r', { title: 'Median cell area, µm²' }, 'Area (µm²)'))),
          h('tbody', ...rows)))
        : h('p.muted.seg-note', app.bundles.registeringSegmentation(t.dataset.id) ? 'The instrument\'s segmentation is being registered…' : 'None yet: the instrument\'s segmentation is registered when the dataset\'s bundle is found.'),
      h('p.muted.seg-note', 'The numbers are each segmentation\'s own: gene transcripts of quality 20 or more, as the QC view counts them. The default segmentation is the one later analyses use; choosing another comes with the comparison workspace.'));
  }

  function choose(a) {
    const session = sessionOf(app.store.project.datasets.find((d) => d.id === a.dataset));
    if (a.source === 'situcraft') session.shown = a.id;
    app.focusItem({ kind: 'segmentation', id: a.id });
    render(true);
  }

  // What the instrument's segmentation did, by its recorded methods.
  function instrumentMethods(t, instrument) {
    const methods = (instrument?.method.methods ?? []).filter((m) => m.cells > 0);
    if (!methods.length) return '';
    const name = (m) => m.method.replace(/^Segmented by /, '').replace(/(\d)µm/, '$1 µm');
    const expansion = (m) => /nucleus expansion/i.test(m.method);
    if (methods.every(expansion)) return `The instrument's segmentation of ${t.dataset.name} was made the same way (${methods.map(name).join('; ')}); SituCraft's is compared with it below.`;
    const expanded = methods.filter(expansion).reduce((sum, m) => sum + m.cells, 0) / methods.reduce((sum, m) => sum + m.cells, 0);
    return `The instrument segmented ${t.dataset.name}'s cells ${expanded > 0.5 ? 'partly' : 'mostly'} from cell stains (${methods.map((m) => `${name(m)}: ${formatCount(m.cells)}`).join('; ')}), so an expansion differs from it most where those stains drew the cells.`;
  }

  // --- Nucleus expansion ---
  function expansionCard(t, list, session) {
    const instrument = list.find((a) => a.source === 'instrument' && a.state === 'complete');
    const input = h('input.input.seg-distance', { type: 'number', min: '0', max: String(MAX_DISTANCE), step: '0.5', value: String(session.distance), 'aria-label': 'Expansion distance in µm', id: 'seg-distance' });
    const run = h('button.btn.primary', { type: 'button', disabled: !instrument || !t.item, onclick: () => expand(t, session, Number(input.value)) }, icon('play'), 'Expand the nuclei');
    input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' && !run.disabled) run.click();
    });
    input.addEventListener('change', () => {
      session.distance = Number(input.value);
    });
    const running = app.bundles.expansions(t.dataset.id).map((e) => {
      const job = e.job ? app.jobs.get(e.job) : null;
      const bar = h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } });
      const message = h('span.muted', job?.message ?? 'Starting…');
      if (e.job) live.set(e.job, { bar, message });
      return h('div.seg-running', h('div', h('b', `Expanding by ${e.distance} µm`), ' ', message), h('div.progress', bar));
    });
    const existing = list.filter((a) => a.source === 'situcraft' && a.parameters?.distance_um !== undefined).map((a) => a.parameters.distance_um);
    return h('section.seg-section',
      h('div.section-title', 'Nucleus expansion'),
      h('div.seg-card',
        h('p', 'Each nucleus grows by up to a distance, and every pixel goes to the nearest nucleus within it: the cells meet where they are equally far from two nuclei. Transcripts belong to the cell holding the pixel they lie in, as the instrument assigns them. ', instrumentMethods(t, instrument)),
        h('div.seg-form',
          h('label.field', { for: 'seg-distance' }, h('span', 'Distance (µm)'), input),
          run,
          existing.length ? h('span.muted', `Made already: ${existing.sort((x, y) => x - y).map((d) => `${d} µm`).join(', ')}.`) : null),
        !instrument ? h('p.muted.seg-note', 'It starts from the nuclei of the instrument\'s segmentation, which is not registered yet.') : null,
        !t.item ? h('p.muted.seg-note', `${t.dataset.name}'s bundle was not found where the project records it: locate it in the Data view.`) : null,
        ...running,
        h('p.muted.seg-cite', 'Methods: a Voronoi partition of the tissue by the nuclei, clipped at the distance (Okabe et al. 2000), with exact Euclidean distances (Felzenszwalb & Huttenlocher 2012).')));
  }

  // --- Comparing two segmentations ---
  async function runComparison(t, a, b) {
    const manifest = await app.bundles.compareSegmentations(t.item, t.dataset.id, a, b);
    sessionOf(t.dataset).compare.shown = manifest.id;
    render(true);
    return manifest;
  }

  function comparisonCard(t, list, session) {
    const state = session.compare;
    const complete = list.filter((a) => a.state === 'complete');
    const promoted = promotedSegmentation(app.store.project, t.dataset.id)?.record.id;
    if (!complete.some((x) => x.id === state.a)) state.a = promoted ?? complete[0]?.id ?? '';
    if (!complete.some((x) => x.id === state.b) || state.b === state.a) state.b = [...complete].reverse().find((x) => x.id !== state.a)?.id ?? '';
    const pick = (id, label, key) => {
      const el = h('select.input', { id }, ...complete.map((x) => h('option', { value: x.id }, x.name)));
      el.value = state[key];
      el.addEventListener('change', () => {
        state[key] = el.value;
        render(true);
      });
      return h('label.field', { for: id }, h('span', label), el);
    };
    const go = h('button.btn.primary', { type: 'button', disabled: complete.length < 2 || !state.a || !state.b || state.a === state.b || !t.item, onclick: async () => {
      try {
        const m = await runComparison(t, state.a, state.b);
        toast(`Compared: ${exact(m.summary?.classes?.['one-to-one'] ?? 0)} cells one-to-one, ${percent(m.summary?.changedFraction, 2)} of gene transcripts changed cell.`, { kind: 'ok' });
      } catch (error) {
        toast(`The comparison: ${error.message}`, { kind: 'error', timeout: 12000 });
      }
    } }, icon('compare'), 'Compare');
    const running = app.bundles.comparisons(t.dataset.id).map((e) => {
      const job = e.job ? app.jobs.get(e.job) : null;
      const bar = h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } });
      const message = h('span.muted', job?.message ?? 'Starting…');
      if (e.job) live.set(e.job, { bar, message });
      return h('div.seg-running', h('div', h('b', 'Comparing'), ' ', message), h('div.progress', bar));
    });
    const done = comparisonsOf(app.store.project, t.dataset.id);
    const nameOf = (id) => list.find((x) => x.id === id)?.name ?? id;
    if (!done.some((c) => c.id === state.shown)) state.shown = done.at(-1)?.id ?? null;
    const rows = done.map((c) => {
      const s = c.summary ?? {};
      return h(`tr${state.shown === c.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-comparison': c.id, onclick: () => {
        state.shown = c.id;
        render(true);
      }, onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          state.shown = c.id;
          render(true);
        }
      } },
      h('td', nameOf(c.a)), h('td', nameOf(c.b)),
      h('td.r', exact(s.classes?.['one-to-one'] ?? 0)),
      h('td.r', percent(s.pixelAgreement, 2)),
      h('td.r', percent(s.changedFraction, 2)),
      h('td.r', Number.isFinite(s.medianIoU) ? s.medianIoU.toFixed(3) : '—'),
      h('td', h('button.btn.small', { type: 'button', title: 'Both segmentations on the tissue, the differences to review, and the decision', onclick: (event) => {
        event.stopPropagation();
        app.compareFocus = c.id;
        app.setMode('compare');
      } }, icon('eye'), 'Open')));
    });
    const detail = h('div.seg-compare-detail');
    const shown = done.find((c) => c.id === state.shown);
    if (shown) {
      detail.append(h('p.muted.seg-note', 'Reading the comparison…'));
      app.bundles.comparisonDescription(shown.id).then((d) => {
        clear(detail).append(...comparisonDetail(d, nameOf(shown.a), nameOf(shown.b)));
      }).catch((error) => {
        clear(detail).append(h('p.muted.seg-note', `The comparison could not be read: ${error.message}`));
      });
    }
    return h('section.seg-section',
      h('div.section-title', 'Compare two segmentations'),
      h('div.seg-card',
        h('p', 'Which cells correspond, how each differs, and what changes: a cell corresponds to another when they share a nucleus, or when at least half of one lies within the other. Each segmentation\'s transcripts are counted the same way, and the share of gene pairs co-expressed more than in nuclei alone, and of mutually exclusive markers co-expressed, show transcripts given to the wrong cell.'),
        h('div.seg-import-grid', pick('seg-compare-a', 'A', 'a'), pick('seg-compare-b', 'B', 'b')),
        h('div.btn-row', go, complete.length < 2 ? h('span.muted.seg-note', 'Expand the nuclei, or import a segmentation, to have two to compare.') : null),
        ...running,
        done.length ? h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.seg-comparisons',
          h('thead', h('tr', h('th', 'A'), h('th', 'B'), h('th.r', 'One-to-one'), h('th.r', { title: 'Pixels in the same cell, of those either gives a cell' }, 'Pixels agree'), h('th.r', { title: 'Gene transcripts (quality 20+) whose cell changed' }, 'Changed'), h('th.r', { title: 'Median intersection over union of the one-to-one cells' }, 'IoU'), h('th', h('span.sr-only', 'Workspace')))),
          h('tbody', ...rows))) : null,
        detail));
  }

  // A comparison's measures, from its description.
  function comparisonDetail(d, nameA, nameB) {
    const A = d.metricsA ?? {};
    const B = d.metricsB ?? {};
    const num = (v, digits = 1) => (Number.isFinite(v) ? v.toLocaleString('en-US', { maximumFractionDigits: digits }) : '—');
    const row = (label, f, help) => h('tr', h('th', { title: help ?? '' }, label), h('td.r', f(A)), h('td.r', f(B)));
    const classes = h('table.data.seg-tally',
      h('thead', h('tr', h('th', 'Difference'), h('th', ''), h('th.r', 'Count'), h('th.r', 'Cells of A'), h('th.r', 'Cells of B'))),
      h('tbody', ...CLASSES.filter(([c]) => d.classes?.[c] || ['one-to-one', 'split', 'merged'].includes(c)).map(([c, what]) => h('tr', h('th', c), h('td.muted', what), h('td.r', exact(d.classes?.[c] ?? 0)), h('td.r', exact(d.cellsA?.[c] ?? 0)), h('td.r', exact(d.cellsB?.[c] ?? 0))))));
    const each = h('table.data.seg-tally',
      h('thead', h('tr', h('th', ''), h('th.r', `A: ${nameA}`), h('th.r', `B: ${nameB}`))),
      h('tbody',
        row('Cells', (m) => exact(m.cells)),
        row('With a nucleus', (m) => (m.cellsWithNucleus >= 0 ? exact(m.cellsWithNucleus) : '—'), 'Cells holding most of a nucleus of the dataset'),
        row('With several nuclei', (m) => (m.cellsWithSeveralNuclei >= 0 ? exact(m.cellsWithSeveralNuclei) : '—')),
        row('Gene transcripts assigned', (m) => percent(m.assignedFraction, 2), 'Of gene transcripts of quality 20 or more'),
        row('Transcripts per cell', (m) => num(m.medianTranscriptsPerCell), 'Median, gene transcripts of quality 20 or more'),
        row('Area (µm²)', (m) => num(m.medianAreaUm2), 'Median'),
        row('Cells in several pieces', (m) => exact(m.multipieceCells)),
        row('Spurious co-expression', (m) => num(m.spuriousCoexpression, 3), 'Co-expression of the dataset\'s spurious gene pairs relative to nuclei alone (1); median over the pairs (Jones et al. 2025)'),
        row('Mutually exclusive co-expression', (m) => num(m.mecr, 4), 'Share of cells expressing either of two markers of different lineages that express both; mean over the pairs (Hartman and Satija 2024)'),
        row('Job time', (m) => (m.runtimeSeconds ? `${num(m.runtimeSeconds, 1)} s` : '—'), 'The time SituCraft\'s job took to make or import it (the instrument\'s own segmentation was made on the instrument)'),
        row('Files', (m) => (m.sizeBytes ? `${num(m.sizeBytes / 1e6, 1)} MB` : 'in the bundle'))));
    const c = d.changesQ20 ?? {};
    const between = h('dl.kv.seg-kv',
      h('dt', 'Pixels'), h('dd', percent(d.pixelAgreement, 2), h('span.muted', ' in the same cell, of those either gives a cell')),
      h('dt', 'Transcripts'), h('dd', percent(c.fraction, 2), h('span.muted', ` of gene transcripts changed cell: ${exact(c.gained)} gained, ${exact(c.lost)} lost, ${exact(c.moved)} moved`)),
      h('dt', 'Overlap'), h('dd', Number.isFinite(d.iou?.median) ? d.iou.median.toFixed(3) : '—', h('span.muted', ` median IoU of ${exact(d.iou?.pairs ?? 0)} cells the same in both`)),
      h('dt', 'Boundaries'), h('dd', Number.isFinite(d.displacement?.medianUm) ? `${d.displacement.medianUm.toFixed(2)} µm` : '—', h('span.muted', ' moved, median (the area between the outlines over their perimeter)')));
    return [
      h('h3.seg-card-title', `${nameA} (A) and ${nameB} (B)`),
      classes,
      each,
      between,
      h('p.muted.seg-cite', `The spurious co-expression rests on ${exact(d.coexpression?.pairs ?? 0)} gene pairs co-expressed at least 1.5 times as often once nuclei grow by 10 µm, measured on ${exact(d.coexpression?.nuclearCells ?? 0)} nuclei of 50 gene transcripts or more, every cell down-sampled to 50 (Jones et al. 2025). The mutually exclusive co-expression uses ${exact(d.mecrPairs?.length ?? 0)} pairs of lineage markers in the panel (Hartman and Satija 2024).`),
    ];
  }

  // --- Importing another tool's segmentation ---
  function importCard(t, list, session) {
    const state = session.import;
    const spec = IMPORT_FORMATS[state.format];
    const instrument = list.find((a) => a.source === 'instrument' && a.state === 'complete');
    const field = (id, label, input, hint) => h('label.field', { for: id }, h('span', label), input, hint ? h('span.muted.seg-hint', hint) : null);
    const text = (id, value, placeholder, set) => {
      const el = h('input.input', { id, type: 'text', value, placeholder, spellcheck: false, autocomplete: 'off' });
      el.addEventListener('input', () => set(el.value));
      return el;
    };
    const format = h('select.input', { id: 'seg-import-format' }, ...Object.entries(IMPORT_FORMATS).map(([k, f]) => h('option', { value: k }, f.label)));
    format.value = state.format;
    format.addEventListener('change', () => {
      state.format = format.value;
      render(true);
    });
    const units = h('select.input', { id: 'seg-import-units' }, h('option', { value: 'um' }, 'µm'), h('option', { value: 'pixels' }, 'pixels of the morphology image'));
    units.value = state.units;
    units.addEventListener('change', () => {
      state.units = units.value;
    });
    const paths = spec.paths.map((p, i) => field(`seg-import-path-${i}`, `${p.label}${p.optional ? ' (optional)' : ''}`, text(`seg-import-path-${i}`, state.paths[i] ?? '', p.placeholder, (v) => {
      state.paths[i] = v;
    }), p.hint));
    const advanced = spec.options.map((key) => field(`seg-import-${key}`, OPTION_LABELS[key][0], text(`seg-import-${key}`, state.fields[key] ?? '', OPTION_LABELS[key][1], (v) => {
      state.fields[key] = v;
    })));
    const tool = state.format === 'xenium' ? [] : [
      field('seg-import-tool', 'Made by', text('seg-import-tool', state.tool, state.format === 'baysor' ? 'Baysor' : 'the tool\'s name', (v) => {
        state.tool = v;
      })),
      field('seg-import-tool-version', 'Its version', text('seg-import-tool-version', state.tool_version, '0.7.1', (v) => {
        state.tool_version = v;
      })),
      field('seg-import-citation', 'Its citation', text('seg-import-citation', state.citation, 'the paper to cite (optional)', (v) => {
        state.citation = v;
      })),
    ];
    const go = h('button.btn.primary', { type: 'button', disabled: !instrument || !t.item, onclick: () => importFromForm(t, session) }, icon('upload'), 'Import');
    const running = app.bundles.imports(t.dataset.id).map((e) => {
      const job = app.jobs.get(e.job);
      const bar = h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } });
      const message = h('span.muted', job?.message ?? 'Starting…');
      live.set(e.job, { bar, message });
      return h('div.seg-running', h('div', h('b', e.title), ' ', message), h('div.progress', bar));
    });
    return h('section.seg-section',
      h('div.section-title', 'Import a segmentation'),
      h('div.seg-card',
        h('p', 'A segmentation made by another tool, imported to compare with the others: it is checked against the dataset (the transcripts it names, where its cells lie, its ids) and kept with the files it came from, by their SHA-256. Its transcripts are counted as every segmentation\'s are.'),
        h('div.seg-import-grid',
          field('seg-import-format', 'Format', format),
          field('seg-import-units', 'Coordinates in', units, 'SituCraft does not guess them; the check says when they do not fit'),
          ...paths),
        h('p.muted.seg-note', spec.about),
        advanced.length || tool.length ? h('details.seg-advanced', state.open ? { open: true } : {},
          h('summary', 'How to read it, and what made it'),
          h('div.seg-import-grid', ...advanced, ...tool)) : null,
        h('div.btn-row', go, !instrument ? h('span.muted.seg-note', 'An import is checked against the dataset once its instrument segmentation is registered.') : null),
        ...running));
  }

  // --- The agreement with the instrument's segmentation ---
  function agreementCard(t, list, session) {
    const own = list.filter((a) => a.source === 'situcraft');
    const a = own.find((x) => x.id === session.shown) ?? own[own.length - 1];
    const head = h('div.section-title', 'Agreement with the instrument\'s segmentation');
    if (!a) return h('section.seg-section', head, h('p.muted.seg-note', 'Expand the nuclei to compare the result with the instrument\'s segmentation: its cells, pixels and transcripts.'));
    const g = a.summary?.agreement;
    if (!g) return h('section.seg-section', head, h('p.muted.seg-note', `${a.name} has no comparison with the instrument's segmentation.`));
    const p = g.pixels ?? {};
    const tr = g.transcripts ?? {};
    const d = g.differences;
    const kv = (label, value, note) => [h('dt', label), h('dd', value, note ? h('span.muted', ` ${note}`) : null)];
    const tally = (label, x, neither) => h('tr', h('th', label), h('td.r', count(x.same)), h('td.r', count(x.otherCell)), h('td.r', count(x.situcraftOnly)), h('td.r', count(x.instrumentOnly)), h('td.r', neither ? count(x.neither ?? 0) : '—'));
    const differs = [];
    const distance = a.parameters?.distance_um;
    if (d && p.situcraftOnly > 0) {
      const rim = Number.isFinite(distance) && d.situcraftOnlyDistanceUm[0] >= 0.8 * distance;
      differs.push(`${count(p.situcraftOnly)} pixels are in a SituCraft cell and in none of the instrument's: each lies ${um(d.situcraftOnlyDistanceUm[0])} to ${um(d.situcraftOnlyDistanceUm[1])} from its nucleus.${rim ? ' They are the rim of the expansion, which the instrument\'s cells do not reach there.' : ''}`);
    }
    const instrumentOnly = (p.instrumentOnly ?? 0) - (d?.instrumentOnlyNoNucleus ?? 0);
    if (d && instrumentOnly > 0) differs.push(`${count(instrumentOnly)} pixels are in an instrument's cell and in none of SituCraft's: each lies ${um(d.instrumentOnlyDistanceUm[0])} to ${um(d.instrumentOnlyDistanceUm[1])} from that cell's nearest nucleus.`);
    if (d?.instrumentOnlyNoNucleus > 0) differs.push(`${count(d.instrumentOnlyNoNucleus)} pixels are in an instrument's cell that holds no nucleus, which no expansion of nuclei makes.`);
    const otherCell = (p.otherCell ?? 0) - (d?.otherCellNoNucleus ?? 0);
    if (d && otherCell > 0) {
      const near = d.otherCellGapUm <= 0.5;
      differs.push(`${count(otherCell)} pixels are in different cells. SituCraft gives each to the nearest nucleus; none is more than ${um(d.otherCellGapUm)} nearer to it than to the instrument's cell's nuclei${near ? ', so they lie where neighbouring cells meet, almost equally far from both' : ''}.`);
    }
    if (d?.otherCellNoNucleus > 0) differs.push(`${count(d.otherCellNoNucleus)} pixels SituCraft gives a cell are in an instrument's cell that holds no nucleus.`);
    if (d?.transcriptsOffMask > 0) differs.push(`${exact(d.transcriptsOffMask)} transcripts given differently are ones the instrument records in a cell other than its own mask holds at their pixel (on a pixel's edge, or outside the mask).`);
    const c = g.cells ?? {};
    const oneToOne = c.paired === c.instrument && c.situcraft === c.instrument;
    const pairing = oneToOne
      ? 'each of the instrument\'s cells holds one nucleus'
      : `of the instrument's cells, ${exact(c.paired)} hold one nucleus, ${exact(c.instrumentWithoutNucleus)} none and ${exact(c.instrumentWithSeveralNuclei)} several${c.nucleiOutsideCells ? `; ${exact(c.nucleiOutsideCells)} nuclei lie in none` : ''}`;
    const params = a.parameters ?? {};
    return h('section.seg-section',
      head,
      h('div.seg-card',
        h('h3.seg-card-title', a.name, h('span.muted', ` · compared with ${g.reference.replace(/^the instrument's segmentation: /, '')}`)),
        h('dl.kv.seg-kv',
          ...kv('Cells', `SituCraft ${exact(c.situcraft)}, one for each nucleus; the instrument ${exact(c.instrument)}`, `(${pairing})`),
          ...kv('Pixels', percent(p.agreement, 2), 'in the same cell, of those either gives a cell'),
          ...kv('Transcripts', percent(tr.agreement, 2), 'in the same cell, or in none in both'),
          ...kv('Cell area', signedPercent(g.area?.medianRelativeDifference), `median difference${oneToOne ? '' : ' of the cells paired one to one'}; ${percent(g.area?.within5Percent)} within 5%, ${percent(g.area?.within10Percent)} within 10%`),
          ...kv('Assigned', `SituCraft ${percent(g.assignedFraction?.situcraft, 2)}; the instrument ${percent(g.assignedFraction?.instrument, 2)}`, 'of gene transcripts of quality 20 or more')),
        h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-tally',
          h('thead', h('tr', h('th', ''), h('th.r', 'Same cell'), h('th.r', 'Other cells'), h('th.r', 'SituCraft\'s only'), h('th.r', 'Instrument\'s only'), h('th.r', 'In neither'))),
          h('tbody', tally('Pixels', p, false), tally('Transcripts', tr, true)))),
        h('p.muted.seg-note', 'A pixel or transcript is in the same cell when SituCraft\'s cell grew from a nucleus the instrument\'s cell holds.'),
        differs.length ? h('div.seg-differs', h('div.muted', 'What differs'), h('ul', ...differs.map((x) => h('li', x)))) : null,
        h('p.muted.seg-cite', `Parameters: ${params.distance_um ?? '—'} µm (${Number.isFinite(params.distance_px) ? params.distance_px.toFixed(2) : '—'} pixels); nuclei from ${params.nuclei ?? '—'}; ${params.nucleus_cells ? `${params.nucleus_cells}; ` : ''}${params.ties ?? ''}.`)));
  }

  function updateLive() {
    for (const [id, parts] of live) {
      const job = app.jobs.get(id);
      if (!job) continue;
      parts.bar.style.width = `${Math.round(job.progress * 100)}%`;
      parts.message.textContent = job.message ?? '';
    }
  }

  function render(force = false) {
    const t = target();
    if (!t) {
      title.textContent = '';
      shape = '';
      clear(body).append(h('div.planned', h('div.empty', icon('segment'), h('h3', 'No dataset yet'),
        h('p', 'Add a Xenium output bundle to the project in the Data view: its segmentation is registered, and nucleus expansion can be run here.'),
        h('div.btn-row', { style: { marginTop: '12px' } }, h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')))));
      return;
    }
    const session = sessionOf(t.dataset);
    if (t.focused && segmentationsOf(app.store.project, t.dataset.id).some((a) => a.id === t.focused && a.source === 'situcraft')) session.shown = t.focused;
    const list = segmentationsOf(app.store.project, t.dataset.id);
    const next = JSON.stringify([t.dataset.id, Boolean(t.item), list.map((a) => [a.id, a.state]), session.shown, comparisonsOf(app.store.project, t.dataset.id).map((c) => c.id), session.compare, app.bundles.comparisons(t.dataset.id), app.bundles.expansions(t.dataset.id), app.bundles.imports(t.dataset.id), app.bundles.registeringSegmentation(t.dataset.id), promotedSegmentation(app.store.project, t.dataset.id)?.record.id]);
    if (!force && next === shape) {
      updateLive();
      return;
    }
    shape = next;
    title.textContent = ` · ${t.dataset.name}`;
    live.clear();
    // Keep what is being typed when the view redraws (the import's fields keep their values in the
    // session; the distance its own).
    const focused = document.activeElement && body.contains(document.activeElement) ? document.activeElement.id : '';
    const typing = focused === 'seg-distance' ? document.activeElement.value : null;
    const advancedOpen = body.querySelector('details.seg-advanced')?.open;
    if (advancedOpen !== undefined) session.import.open = advancedOpen;
    clear(body).append(
      h('p.muted.seg-intro', 'A segmentation decides which transcripts belong to which cell, and so every count, cell type and neighbourhood after it. Each is kept as an artifact with how it was made; none is ever changed or deleted.'),
      table(t, list, session),
      expansionCard(t, list, session),
      agreementCard(t, list, session),
      comparisonCard(t, list, session),
      importCard(t, list, session));
    if (typing !== null) body.querySelector('#seg-distance').value = typing;
    if (focused) body.querySelector(`#${CSS.escape(focused)}`)?.focus();
    updateLive();
  }

  render();
  // The remote action's way in (ui/remote.js).
  app.segmentationView = {
    compare: async (dataset, a, b) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      if (!now.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      Object.assign(sessionOf(dataset).compare, { a, b });
      render(true);
      const manifest = await runComparison(now, a, b);
      const d = await app.bundles.comparisonDescription(manifest.id);
      document.querySelector(`[data-comparison="${CSS.escape(manifest.id)}"]`)?.scrollIntoView({ block: 'center' });
      return { manifest, description: d };
    },
    importSegmentation: async (dataset, options) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      if (!now.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const state = sessionOf(dataset).import;
      state.format = options.format;
      state.paths = [...(options.paths ?? []), '', ''].slice(0, 2);
      state.units = options.units ?? 'um';
      render(true);
      const manifest = await runImport(now, options);
      app.focusItem({ kind: 'segmentation', id: manifest.id });
      return manifest;
    },
    expand: async (dataset, distance) => {
      const t = target();
      if (!t || t.dataset.id !== dataset.id) app.focusItem({ kind: 'dataset', id: dataset.id });
      const now = target();
      if (!now.item) throw new Error(`${dataset.name}'s bundle was not found where the project records it.`);
      const manifest = await app.bundles.expandNuclei(now.item, dataset.id, distance);
      sessionOf(dataset).shown = manifest.id;
      sessionOf(dataset).distance = distance;
      render(true);
      return manifest;
    },
  };
  return {
    update(topics) {
      if (topics.has('project') || topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('mode')) render();
      else if (topics.has('jobs')) updateLive();
    },
    destroy() {
      if (app.segmentationView) delete app.segmentationView;
      root.remove();
    },
  };
}
