// The segmentation studio (requirements S3, S4, S5, S7, E5): SituCraft's transcript-informed
// segmentation set up and run in the Segmentation view, in six steps: Input (the transcripts, the
// cells it starts from, what it covers), Compute (threads, memory, disk, time), Model (a preset or
// every setting, each explained), Review (what will run, its problems and risks, its recipe), Run
// (progress, the log, stopping it) and Compare (its convergence, its limitations and what changed
// from the instrument's segmentation). Every change asks the program for the spec's preflight
// (api_refine.go), so that each step shows what the run would do on this bundle and computer.

import { h, icon, formatCount } from './dom.js';
import { toast } from './overlays.js';
import { comparisonsOf } from '../lib/segmentations.js';
import {
  GENERAL_LIMITATION, STUDIO_STEPS, areaText, bytes, convergence, duration, failureOf, isRefinement, limitations, load, memory, parseRegion,
  presetSpec, readRecipe, regionText, settingProblem, settingValue, withSetting,
} from '../lib/refine.js';

const count = (v) => (Number.isFinite(v) ? formatCount(v) : '—');
const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');
const percent = (v, digits = 1) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');

// The step each setting is changed in a number field by.
const STEP = {
  'stages.voxel_um': 0.25, 'stages.layers': 1, 'stages.sweeps': 50, samples: 50, seed: 1, 'sampler.perimeter_factor': 0.1, 'model.nuclear_prior': 0.05,
  'sampler.bubble': 0.01, 'sampler.diffusion_wide_share': 0.05, 'sampler.diffusion_wide_um': 0.5, 'sampler.diffusion_narrow_um': 0.1, 'model.components': 1,
  block_um: 5, gc_percent: 5,
};

// The settings shown in Input (not Model).
const INPUT_SETTINGS = new Set(['min_qv']);

export function createStudio(app, hooks) {
  let catalog = null; // { version, presets, settings, defaults, citation }
  let asking = false;
  const timers = new Map();

  function ensureCatalog() {
    if (catalog || asking) return;
    asking = true;
    app.bundles.refinementPresets().then((c) => {
      catalog = c;
      hooks.render(true);
    }, (error) => {
      asking = false;
      app.log?.(`The refinement presets could not be read: ${error.message}`);
    });
  }

  function stateOf(session, running = false) {
    session.studio ??= { step: running ? 'run' : 'input', spec: null, threads: 0, force: false, answer: null, asked: '', error: '', pending: false, showAll: false, problems: {}, draft: '', failure: null, events: [], eventsOf: '', note: '' };
    const st = session.studio;
    if (!st.spec && catalog) st.spec = presetSpec(catalog.presets, 'balanced');
    return st;
  }

  const requestOf = (st) => ({ spec: st.spec, ...(st.threads ? { threads: st.threads } : {}), ...(st.force ? { force: true } : {}) });

  // Asks for the spec's preflight, a quarter of a second after the last change.
  function preflight(t, st, now = false) {
    if (!st.spec || !t.item) return;
    const key = JSON.stringify(requestOf(st));
    if (key === st.asked && (st.answer || st.error || st.pending)) return;
    clearTimeout(timers.get(t.dataset.id));
    timers.set(t.dataset.id, setTimeout(async () => {
      st.asked = key;
      st.pending = true;
      try {
        const answer = await app.bundles.refinementPreflight(t.item, requestOf(st));
        if (JSON.stringify(requestOf(st)) !== key) return;
        st.answer = answer;
        st.error = '';
        st.spec.preset = answer.preflight.spec.preset;
      } catch (error) {
        if (JSON.stringify(requestOf(st)) !== key) return;
        st.answer = null;
        st.error = error.message;
      } finally {
        if (JSON.stringify(requestOf(st)) === key) st.pending = false;
      }
      hooks.render(true);
    }, now ? 0 : 250));
  }

  function change(t, st, next) {
    st.spec = next;
    hooks.render(true);
    preflight(t, st);
  }

  // --- The run ---
  async function start(t, st, request = requestOf(st)) {
    st.failure = null;
    st.note = '';
    st.step = 'run';
    hooks.render(true);
    try {
      const answer = await hooks.refineRun(t, request);
      st.step = 'compare';
      st.asked = '';
      preflight(t, st, true);
      const s = answer.manifest.summary ?? {};
      toast(`${answer.manifest.name} of ${t.dataset.name}: ${exact(s.cells)} cells, ${percent(s.assignedFraction)} of gene transcripts assigned${answer.comparison ? `; ${percent(answer.comparison.summary?.changedFraction, 2)} changed cell from the instrument's segmentation` : ''}.`, { kind: 'ok', timeout: 9000 });
      hooks.render(true);
      return answer;
    } catch (error) {
      if (error.job?.state === 'cancelled') st.note = 'The refinement was stopped; nothing was recorded.';
      else st.failure = failureOf(error.job) ?? { title: 'The refinement', message: error.message, remedy: '' };
      hooks.render(true);
      throw error;
    }
  }

  // The job's log, read again while it runs (at most every two seconds).
  async function readEvents(st, id) {
    if (!id || Date.now() - (st.eventsAt ?? 0) < 2000) return;
    st.eventsAt = Date.now();
    try {
      const response = await fetch(`/api/jobs/${encodeURIComponent(id)}`, { cache: 'no-store' });
      const job = await response.json();
      const events = (job.events ?? []).slice(-12);
      if (JSON.stringify(events) !== JSON.stringify(st.events)) {
        st.events = events;
        st.eventsOf = id;
        hooks.render(true);
      }
    } catch {
      // The next update reads it again.
    }
  }

  // --- The steps ---
  function stepper(st) {
    return h('nav.studio-steps', { 'aria-label': 'The refinement\'s steps' }, ...STUDIO_STEPS.map((s, i) => h(`button.studio-step${st.step === s.id ? '.active' : ''}`, {
      type: 'button', 'aria-current': st.step === s.id ? 'step' : null, title: s.about, dataset: { step: s.id },
      onclick: () => {
        st.step = s.id;
        hooks.render(true);
      },
    }, h('span.studio-step-n', String(i + 1)), h('span', s.label))));
  }

  function nav(st) {
    const i = STUDIO_STEPS.findIndex((s) => s.id === st.step);
    const go = (j) => () => {
      st.step = STUDIO_STEPS[j].id;
      hooks.render(true);
    };
    return h('div.btn-row.studio-nav',
      i > 0 ? h('button.btn', { type: 'button', onclick: go(i - 1) }, `Back: ${STUDIO_STEPS[i - 1].label}`) : null,
      i < 3 ? h('button.btn', { type: 'button', onclick: go(i + 1) }, `Next: ${STUDIO_STEPS[i + 1].label}`) : null);
  }

  function status(st) {
    if (st.error) return h('p.studio-problem', { role: 'alert' }, icon('warning'), st.error);
    if (st.pending && !st.answer) return h('p.muted.seg-note', 'Reading the bundle for the preflight…');
    return null;
  }

  function findings(pre, classes = null) {
    const keep = (f) => !classes || classes.includes(f.class);
    const problems = (pre?.problems ?? []).filter(keep);
    const risks = (pre?.risks ?? []).filter(keep);
    if (!problems.length && !risks.length) return null;
    return h('ul.studio-findings',
      ...problems.map((f) => h('li.studio-problem', icon('warning'), h('span', h('b', f.message), f.remedy ? ` ${f.remedy}` : ''))),
      ...risks.map((f) => h('li.studio-risk', icon('info'), h('span', f.message))));
  }

  function number(id, value, { min, max, step, label, onchange, width = '110px' }) {
    const input = h('input.input', { id, type: 'number', value: String(value), min: min !== undefined ? String(min) : null, max: max !== undefined ? String(max) : null, step: String(step ?? 'any'), 'aria-label': label, style: { width } });
    input.addEventListener('change', () => onchange(input.value === '' ? Number.NaN : Number(input.value)));
    input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') input.blur();
    });
    return input;
  }

  function inputStep(t, st, pre) {
    const help = catalog.settings.find((s) => s.key === 'min_qv');
    const q = st.spec.min_qv;
    const area = st.spec.area;
    const setArea = (next) => change(t, st, { ...structuredClone(st.spec), area: next });
    const mode = (id, label) => h(`button${area.mode === id ? '.active' : ''}`, { type: 'button', 'aria-pressed': area.mode === id ? 'true' : 'false', onclick: () => {
      if (area.mode === id) return;
      setArea(id === 'regions' ? { mode: 'regions', regions: area.regions ?? [] } : id === 'sections' ? { mode: 'sections' } : { mode: 'whole' });
    } }, label);
    const regions = area.regions ?? [];
    const viewed = app.viewedRegions?.get(t.dataset.id);
    const draft = h('input.input', { id: 'studio-region', type: 'text', placeholder: 'x0, y0, x1, y1 (µm)', value: st.draft, 'aria-label': 'A region\'s corners in µm', style: { width: '220px' } });
    const add = (r) => setArea({ mode: 'regions', regions: [...regions, r] });
    const addTyped = () => {
      try {
        add(parseRegion(draft.value));
        st.draft = '';
      } catch (error) {
        st.draft = draft.value;
        toast(error.message, { kind: 'error' });
      }
    };
    draft.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') addTyped();
    });
    draft.addEventListener('change', () => {
      st.draft = draft.value;
    });
    return h('div.studio-pane',
      h('h4', 'Transcripts'),
      h('div.seg-form',
        h('label.field', { for: 'studio-min-qv' }, h('span', 'Quality (Q) at least'),
          number('studio-min-qv', q, { min: 0, max: 40, step: 1, label: 'The quality the modelled transcripts reach', onchange: (v) => {
            const problem = settingProblem(help, v);
            if (problem) return toast(problem, { kind: 'error' });
            change(t, st, { ...structuredClone(st.spec), min_qv: v });
          } })),
        h('span.muted', help.help)),
      pre?.rows ? h('p', `${exact(pre.transcripts)} gene transcripts reach quality ${q}, ${percent(pre.transcripts ? pre.overNuclei / pre.transcripts : Number.NaN)} of them over a nucleus${area.mode === 'regions' && pre.estimate?.regions ? `; the ${exact(pre.estimate.transcripts)} in and about the region${pre.estimate.regions === 1 ? '' : 's'} are modelled` : ': all are modelled'}. Of the bundle's ${exact(pre.rows)} rows, ${exact(pre.belowQuality)} gene transcripts below Q${q}, ${exact(pre.controls)} negative controls and other features${pre.outside ? ` and ${exact(pre.outside)} beyond the image` : ''} are not: they keep the cell of the pixel they lie in.`) : null,
      h('h4', 'The cells it starts from'),
      pre?.cells ? h('p', `The instrument's segmentation: ${exact(pre.cells)} cells over ${exact(pre.genes)} genes, ${exact(pre.cellsWithNucleus)} with a nucleus in the section. Each cell starts from its outline, seeded at its nucleus; a transcript over a nucleus is held to that nucleus's cell by the nuclear prior. No cell is created or removed.`) : null,
      findings(pre, ['prior', 'input', 'quality']),
      h('h4', 'What it covers'),
      h('div.segmented', { role: 'group', 'aria-label': 'What the refinement covers' }, mode('whole', 'The whole tissue'), mode('sections', 'Its sections apart'), mode('regions', 'Chosen regions')),
      area.mode === 'sections' ? h('div.seg-form',
        h('label.field', { for: 'studio-gap' }, h('span', 'Sections are apart by (µm)'),
          number('studio-gap', area.section_gap_um || 100, { min: 1, max: 100000, step: 10, label: 'The gap between sections in µm', onchange: (v) => {
            if (!(v > 0)) return toast('The gap is a positive number of µm.', { kind: 'error' });
            setArea({ mode: 'sections', section_gap_um: v });
          } })),
        h('span.muted', 'Cells farther apart than this belong to separate sections; each is refined with a model of its own, since unrelated tissues on one slide do not share expression states.')) : null,
      area.mode === 'regions' ? h('div.studio-regions',
        regions.length ? h('ul', ...regions.map((r, i) => h('li', regionText(r), ' ', h('button.btn.small.ghost', { type: 'button', title: 'Remove this region', 'aria-label': `Remove the region ${regionText(r)}`, onclick: () => setArea({ mode: 'regions', regions: regions.filter((_, j) => j !== i) }) }, icon('close'))))) : h('p.muted.seg-note', 'No region yet: type its corners, or add the one last in view in the Viewer.'),
        h('div.seg-form', draft, h('button.btn', { type: 'button', onclick: addTyped }, icon('plus'), 'Add'),
          h('button.btn', { type: 'button', disabled: !viewed, title: viewed ? regionText(viewed) : 'Open the dataset in the Viewer and frame a region first', onclick: () => add(viewed) }, icon('rectangle'), 'Add the region in view')),
        h('p.muted.seg-note', 'The cells whose nuclei lie in a region are refined; those about it are held where the instrument drew them, so that no cell is cut at the region\'s edge. The rest of the tissue keeps the instrument\'s segmentation.')) : null,
      pre?.regions?.length ? h('p.seg-note', area.mode === 'whole' ? `The whole tissue: ${regionText(pre.regions[0])}.` : `${area.mode === 'sections' ? `${pre.regions.length === 1 ? 'One section' : `${pre.regions.length} sections`} found` : 'Refined'}: ${pre.regions.map(regionText).join('; ')}.`) : null,
      findings(pre, ['region']));
  }

  function meter(label, need, have, haveText, unit = bytes) {
    const l = load(need, have);
    return h(`div.studio-meter.${l.level}`,
      h('div.studio-meter-head', h('b', label), h('span', `about ${unit(need)}`, h('span.muted', ` of ${haveText}`))),
      h('div.progress', h('div', { style: { width: `${Math.min(100, Math.round(100 * (l.share ?? 0)))}%` } })));
  }

  function computeStep(t, st, pre, answer) {
    const e = pre?.estimate;
    const cores = answer?.cores ?? 1;
    return h('div.studio-pane',
      h('h4', 'How it runs'),
      h('div.seg-form',
        h('label.field', { for: 'studio-threads' }, h('span', 'Threads'),
          number('studio-threads', st.threads || cores, { min: 1, max: cores, step: 1, label: 'Threads to run on', onchange: (v) => {
            if (!(Number.isInteger(v) && v >= 1 && v <= cores)) return toast(`Threads are from 1 to ${cores}.`, { kind: 'error' });
            st.threads = v === cores ? 0 : v;
            hooks.render(true);
            preflight(t, st);
          } })),
        h('span.muted', `Of this computer's ${cores} cores. The result is the same on any number of threads, byte for byte; fewer leave the computer freer and take longer.`)),
      h('h4', 'What it needs'),
      e && e.regions ? h('div.studio-meters',
        meter('Memory', e.peakMemoryBytes, pre.memoryBytes, pre.memoryBytes > 0 ? `the computer's ${memory(pre.memoryBytes)}` : 'the computer\'s (unknown)', memory),
        meter('Disk', e.diskBytes, pre.freeDiskBytes, pre.freeDiskBytes >= 0 ? `${bytes(pre.freeDiskBytes)} free` : 'the free space (unknown)'),
        h('div.studio-meter', h('div.studio-meter-head', h('b', 'Time'), h('span', `about ${duration(e.seconds)}`, h('span.muted', ` on ${e.cores} thread${e.cores === 1 ? '' : 's'}${st.spec.seed_check ? ', both seeds' : ''}`))))) : h('p.muted.seg-note', 'The estimates follow the preflight.'),
      e && e.regions ? h('p.seg-note', `It refines ${exact(e.cells)} cells${e.cellsHeld ? ` and holds ${exact(e.cellsHeld)} about them` : ''}: ${exact(e.transcripts)} transcripts in ${exact(e.voxels)} voxels${e.regions > 1 ? `, in ${e.regions} regions one after another (memory is the largest region's)` : ''}.`) : null,
      findings(pre, ['memory', 'storage', 'time', 'transcripts']),
      (pre?.problems ?? []).some((f) => f.class === 'memory') ? h('label.studio-check', h('input', { type: 'checkbox', checked: st.force, onchange: (event) => {
        st.force = event.target.checked;
        hooks.render(true);
      } }), 'Run even so: it may slow the computer, or stop for want of memory.') : null,
      h('p.muted.seg-note', 'The estimates come from measured runs of simulated tissues: memory within about 15%, disk within 30%; the time depends on the computer. Memory is the most the program holds at once, while it reads the bundle, samples a region or writes the segmentation.'));
  }

  function settingRow(t, st, s) {
    const id = `studio-set-${s.key.replace(/[._]/g, '-')}`;
    const value = settingValue(st.spec, s.key);
    const problem = st.problems[s.key];
    const apply = (v) => {
      const p = settingProblem(s, v);
      if (p) {
        st.problems[s.key] = p;
        hooks.render(true);
        return;
      }
      delete st.problems[s.key];
      change(t, st, withSetting(st.spec, s.key, v, catalog.presets));
    };
    const control = typeof value === 'boolean'
      ? h('input', { id, type: 'checkbox', checked: value, 'aria-label': s.name, onchange: (event) => apply(event.target.checked) })
      : number(id, value, { min: s.min, max: s.max, step: STEP[s.key], label: s.name, onchange: apply, width: '100px' });
    return h('tr', { dataset: { setting: s.key } },
      h('th', { scope: 'row' }, h('label', { for: id }, s.name), s.unit && s.unit !== '%' ? h('span.muted', ` (${s.unit})`) : null),
      h('td', control, problem ? h('div.studio-problem', problem) : null),
      h('td.muted', s.help, s.cost && s.cost !== 'None.' ? h('span', ` Cost: ${s.cost.charAt(0).toLowerCase()}${s.cost.slice(1)}`) : null));
  }

  function modelStep(t, st) {
    const spec = st.spec;
    const chosen = catalog.presets.find((p) => p.id === spec.preset) ?? catalog.presets.find((p) => p.id === 'expert');
    const expert = spec.preset === 'expert';
    const settings = catalog.settings.filter((s) => !INPUT_SETTINGS.has(s.key) && (expert || st.showAll || !s.expert));
    return h('div.studio-pane',
      h('h4', 'Preset'),
      h('div.studio-presets', { role: 'group', 'aria-label': 'Presets' }, ...catalog.presets.map((p) => h(`button.studio-preset${p.id === spec.preset ? '.active' : ''}`, {
        type: 'button', 'aria-pressed': p.id === spec.preset ? 'true' : 'false', dataset: { preset: p.id },
        onclick: () => {
          if (p.id === spec.preset) return;
          st.problems = {};
          const next = p.id === 'expert' ? { ...structuredClone(spec), preset: 'expert' } : presetSpec(catalog.presets, p.id, spec);
          change(t, st, next);
        },
      }, h('b', p.name), h('span', p.about)))),
      h('div.studio-preset-detail',
        h('p', h('b', 'What it changes: '), chosen.changes),
        h('p.studio-warning', icon('warning'), h('span', h('b', 'Mind: '), chosen.warning)),
        chosen.measured ? h('p.muted', chosen.measured) : null),
      h('h4', 'Settings', h('span.muted', expert ? ' · every one open; any change keeps the settings Expert' : ' · changing one but the seed makes the settings Expert')),
      h('div', { style: { overflowX: 'auto' } }, h('table.data.studio-settings', h('tbody', ...settings.map((s) => settingRow(t, st, s))))),
      expert ? null : h('button.btn.small.ghost', { type: 'button', onclick: () => {
        st.showAll = !st.showAll;
        hooks.render(true);
      } }, icon(st.showAll ? 'minus' : 'plus'), st.showAll ? 'Fewer settings' : 'Every setting'));
  }

  function downloadRecipe(answer, dataset) {
    const blob = new Blob([`${JSON.stringify(answer.recipe, null, 2)}\n`], { type: 'application/json' });
    const a = h('a', { href: URL.createObjectURL(blob), download: `${dataset.name.replace(/[^\w.-]+/g, '_')}-refinement-recipe.json` });
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  function loadRecipe(t, st) {
    const input = h('input', { type: 'file', accept: '.json,application/json', style: { display: 'none' } });
    input.addEventListener('change', async () => {
      const file = input.files?.[0];
      input.remove();
      if (!file) return;
      try {
        const { spec, dataset } = readRecipe(await file.text());
        st.problems = {};
        st.note = dataset && dataset.name !== t.dataset.name ? `The recipe was made for ${dataset.name}; its settings are applied to ${t.dataset.name}.` : `The recipe ${file.name} is loaded.`;
        change(t, st, spec);
        toast(st.note, { kind: 'ok' });
      } catch (error) {
        toast(`${file.name}: ${error.message}`, { kind: 'error', timeout: 9000 });
      }
    });
    document.body.append(input);
    input.click();
  }

  function reviewStep(t, st, pre, answer, running) {
    const spec = st.spec;
    const preset = catalog.presets.find((p) => p.id === spec.preset);
    const o = spec.options;
    const last = o.stages.at(-1);
    const e = pre?.estimate;
    const blocked = (pre?.problems ?? []).filter((f) => !(f.class === 'memory' && st.force));
    const made = answer?.artifact;
    const ready = answer && !st.error && !st.pending && !blocked.length && !running && t.item;
    return h('div.studio-pane',
      h('h4', answer?.name ?? 'Transcript-informed segmentation', made ? h('span.badge.ok', { style: { marginLeft: '8px' } }, 'made already') : null),
      h('dl.kv.seg-kv',
        h('dt', 'Preset'), h('dd', preset?.name ?? spec.preset),
        h('dt', 'Covers'), h('dd', areaText(spec.area)),
        h('dt', 'Transcripts'), h('dd', pre?.rows ? `${exact(e?.transcripts ?? pre.transcripts)} modelled, of quality ${spec.min_qv} or more` : '—'),
        h('dt', 'Cells'), h('dd', e?.regions ? `${exact(e.cells)} refined${e.cellsHeld ? `, ${exact(e.cellsHeld)} held about them` : ''}, of the instrument's ${exact(pre.cells)}` : '—'),
        h('dt', 'Model'), h('dd', `${o.stages.length > 1 ? `${o.stages.map((s) => `${s.voxel_um} µm`).join(' → ')}, ` : `${last.voxel_um} µm voxels, `}${last.layers} layer${last.layers === 1 ? '' : 's'}; outlines within ${o.sampler.perimeter_factor} times the most compact shape's; nuclear prior ${o.model.nuclear_prior}; ${o.sampler.reposition ? 'molecules may have drifted' : 'no drift'}; ${o.model.components} expression states`),
        h('dt', 'Sampling'), h('dd', `${exact(o.stages.reduce((n, s) => n + s.sweeps, 0))} sweeps, the last ${exact(o.samples)} summarized; seed ${o.seed}${spec.seed_check ? `, then seed ${o.seed + 1} to measure their agreement` : ''}`),
        h('dt', 'Needs'), h('dd', e?.regions ? `about ${memory(e.peakMemoryBytes)} of memory, ${bytes(e.diskBytes)} of disk and ${duration(e.seconds)} on ${e.cores} threads` : '—')),
      status(st),
      findings(pre),
      preset?.warning ? h('p.studio-warning', icon('warning'), h('span', h('b', 'Mind: '), preset.warning)) : null,
      h('p.seg-note', 'It makes a segmentation artifact: the cells, their outlines and label image; each transcript\'s cell with its posterior probability; the counts of gene transcripts of quality 20 or more; and its diagnostics (convergence, flagged cells). It is checked before it is recorded, and compared with the instrument\'s segmentation at once. Nothing else changes.'),
      h('p.muted.seg-cite', `Method: SituCraft's implementation (open source, Apache-2.0) of transcript-informed segmentation after ${catalog.citation}. Presets version ${catalog.version}.`),
      h('details.studio-recipe',
        h('summary', 'Recipe'),
        answer?.recipe ? h('pre.mono', JSON.stringify(answer.recipe, null, 2)) : h('p.muted', 'It follows the preflight.'),
        h('p.muted.seg-note', 'Keep it with the result, or run it again in a terminal, on this computer or another: ', h('code', 'situcraft refine --recipe recipe.json <bundle> <new folder>'), '. The same recipe on the same bundle gives the same segmentation, byte for byte; the folder imports here as a SituCraft segmentation.')),
      st.note ? h('p.muted.seg-note', st.note) : null,
      h('div.btn-row',
        made
          ? h('button.btn.primary', { type: 'button', onclick: () => {
            hooks.adopt(t, made);
            st.step = 'compare';
            hooks.render(true);
          } }, icon('eye'), 'Show it')
          : h('button.btn.primary', { type: 'button', disabled: !ready, dataset: { action: 'refine' }, onclick: () => start(t, st).catch(() => {}) }, icon('play'), 'Refine'),
        h('button.btn', { type: 'button', disabled: !answer?.recipe, onclick: () => downloadRecipe(answer, t.dataset) }, icon('download'), 'Save the recipe'),
        h('button.btn', { type: 'button', onclick: () => loadRecipe(t, st) }, icon('upload'), 'Load a recipe…'),
        running ? h('span.muted', 'A refinement of this dataset is running.') : null));
  }

  function runStep(t, st, running) {
    const entry = running[0];
    if (entry?.job) readEvents(st, entry.job);
    const job = entry?.job ? app.jobs.get(entry.job) : null;
    if (entry) {
      const bar = h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } });
      const message = h('span.muted', job?.message ?? 'Starting…');
      if (entry.job) hooks.live.set(entry.job, { bar, message });
      return h('div.studio-pane',
        h('div.seg-running', h('div', h('b', 'Refining'), ' ', message), h('div.progress', bar)),
        st.events.length && st.eventsOf === entry.job ? h('div.job-log.studio-log', ...st.events.map((e) => h(`div.job-event.${e.level}`, h('span.mono.muted', new Date(e.time).toLocaleTimeString()), h('span', e.message)))) : null,
        h('div.btn-row', h('button.btn', { type: 'button', disabled: !entry.job, onclick: async () => {
          try {
            await app.jobs.cancel(entry.job);
          } catch (error) {
            toast(`Stopping the refinement: ${error.message}`, { kind: 'error' });
          }
        } }, icon('stop'), 'Stop'), h('span.muted', 'It runs on if the window closes; the Jobs view follows it too.')));
    }
    if (st.failure) {
      return h('div.studio-pane',
        h('div.studio-failure', { role: 'alert' }, h('h4', icon('warning'), `${st.failure.title}: the refinement failed`), h('p', st.failure.message), st.failure.remedy ? h('p', h('b', 'What to do: '), st.failure.remedy) : null),
        h('button.btn', { type: 'button', onclick: () => {
          st.step = 'review';
          hooks.render(true);
        } }, 'Back to Review'));
    }
    return h('div.studio-pane', h('p.muted', st.note || 'Nothing is running: start a refinement from Review.'));
  }

  function compareStep(t, list, session, st) {
    const instrument = list.find((a) => a.source === 'instrument' && a.state === 'complete');
    const done = list.filter((a) => isRefinement(a));
    const a = done.find((x) => x.id === session.refined) ?? done.at(-1);
    if (!a) return h('div.studio-pane', h('p.muted', 'No refinement of this dataset yet.'));
    const s = a.summary ?? {};
    const d = s.diagnostics;
    const cmp = instrument ? comparisonsOf(app.store.project, t.dataset.id).find((c) => c.a === instrument.id && c.b === a.id) : null;
    const comparing = app.bundles.comparisons(t.dataset.id).some((e) => e.b === a.id);
    return h('div.studio-pane.seg-refined',
      h('h3.seg-card-title', a.name, a.state !== 'complete' ? h('span.badge.warn', ` ${a.state}`) : null),
      h('dl.kv.seg-kv',
        h('dt', 'Cells'), h('dd', exact(s.cells), h('span.muted', ' started from the instrument\'s')),
        h('dt', 'Assigned'), h('dd', percent(s.assignedFraction, 2), h('span.muted', ' of gene transcripts of quality 20 or more')),
        h('dt', 'Median cell'), h('dd', `${count(s.medianTranscriptsPerCell)} transcripts, ${count(s.medianAreaUm2)} µm²`)),
      d ? h('div.seg-differs', h('div.muted', 'Convergence'), h('ul', ...convergence(d).map((c) => h(`li${c.ok ? '' : '.warn'}`, c.text)))) : null,
      h('div.seg-differs', h('div.muted', 'Limitations'), h('ul', ...limitations(d).map((l) => h('li', l.text)), h('li', GENERAL_LIMITATION))),
      cmp
        ? h('p.seg-note', `Compared with the instrument's segmentation: ${exact(cmp.summary?.classes?.['one-to-one'] ?? 0)} cells one to one, ${percent(cmp.summary?.changedFraction, 2)} of gene transcripts in another cell. `,
          h('button.btn.small', { type: 'button', onclick: () => hooks.showInWorkspace(cmp.id) }, icon('eye'), 'Review the differences'))
        : comparing ? h('p.muted.seg-note', 'Comparing it with the instrument\'s segmentation…') : null,
      done.length > 1 ? h('p.muted.seg-note', 'Others: ', ...done.filter((x) => x.id !== a.id).flatMap((x, i) => [i ? ', ' : '', h('a', { href: '#', onclick: (event) => {
        event.preventDefault();
        session.refined = x.id;
        hooks.render(true);
      } }, x.name)])) : null);
  }

  // The studio's section of the Segmentation view.
  function card(t, list, session) {
    ensureCatalog();
    const instrument = list.find((a) => a.source === 'instrument' && a.state === 'complete');
    const running = app.bundles.refinements(t.dataset.id);
    const head = h('p', 'Each cell is redrawn from the transcripts: starting from the instrument\'s cells and their nuclei, its boundary moves to where its own transcripts are, and each transcript may have drifted from the cell it came from. The result is the summary of many samples from the model, with each transcript\'s probability of its cell.');
    if (!catalog) return h('section.seg-section', h('div.section-title', 'Transcript-informed segmentation'), h('div.seg-card', head, h('p.muted', 'Reading the presets…')));
    const st = stateOf(session, running.length > 0);
    if (t.item && instrument) preflight(t, st);
    const answer = st.answer;
    const pre = answer?.preflight;
    const panes = {
      input: () => inputStep(t, st, pre),
      compute: () => computeStep(t, st, pre, answer),
      model: () => modelStep(t, st),
      review: () => reviewStep(t, st, pre, answer, running.length > 0),
      run: () => runStep(t, st, running),
      compare: () => compareStep(t, list, session, st),
    };
    return h('section.seg-section.studio',
      h('div.section-title', 'Transcript-informed segmentation'),
      h('div.seg-card',
        head,
        !instrument ? h('p.muted.seg-note', 'It starts from the instrument\'s segmentation, which is not registered yet.') : null,
        !t.item ? h('p.muted.seg-note', `${t.dataset.name}'s bundle was not found where the project records it: locate it in the Data view.`) : null,
        stepper(st),
        ['input', 'compute', 'model'].includes(st.step) ? status(st) : null,
        panes[st.step](),
        nav(st),
        h('p.muted.seg-cite', 'Methods: after Jones et al. 2025 (Nat Methods 22:1331–1342): a Poisson model of each cell\'s transcripts over a background, a mixture of expression states, cells kept whole and compact, sampled by Metropolis–Hastings. SituCraft\'s model, sampler, presets and their validation: research.md §3.2.')));
  }

  // A request from elsewhere (the remote action) shown in the studio and run.
  async function run(t, session, request) {
    const st = stateOf(session);
    return start(t, st, request);
  }

  async function preflightNow(t, session, request) {
    stateOf(session);
    return app.bundles.refinementPreflight(t.item, request);
  }

  return { card, run, preflight: preflightNow, stateOf };
}
