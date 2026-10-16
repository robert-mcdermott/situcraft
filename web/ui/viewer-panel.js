// The tissue viewer's layers panel: the morphology channels (shown or not, color, contrast window,
// gamma), transcripts (genes found by name, each in its own color, or all transcripts in one, or
// colored by their assignment's probability; quality floor, point size, density gain) and cells
// (which segmentation, outlines, nuclei, fill by an attribute).

import { h, icon, clear, formatCount } from './dom.js';
import { showMenu } from './overlays.js';
import { categoricalColor, colormapLUT, colormapCSS } from '../lib/colormaps.js';
import { attributeRange } from '../lib/viewer-data.js';
import { CERTAINTY_RANGE, LIMITATIONS, NO_PROBABILITY_COLOR, SEVERAL, certaintyCSS, segFillColors, segFills } from '../lib/segview.js';

// Channel colors by Xenium's channel purpose, then in order.
const PURPOSE_COLORS = [
  [/nuclear/i, '#4f7cff'],
  [/boundary/i, '#3ddc84'],
  [/rna/i, '#ffd23f'],
  [/protein/i, '#ff5ca8'],
];
const CHANNEL_COLORS = ['#4f7cff', '#3ddc84', '#ffd23f', '#ff5ca8', '#3fd6e8', '#ff8a3d', '#c58cff', '#ffffff'];
const SWATCHES = [['Blue', '#4f7cff'], ['Cyan', '#3fd6e8'], ['Green', '#3ddc84'], ['Yellow', '#ffd23f'], ['Orange', '#ff8a3d'], ['Red', '#ff4d4d'], ['Magenta', '#ff5ca8'], ['Violet', '#c58cff'], ['White', '#ffffff']];

const MAX_GENES = 16;

// The cell attributes a fill can show.
export const FILLS = [
  { id: 'transcripts', label: 'Gene transcripts', column: 'transcripts' },
  { id: 'total', label: 'All transcripts', column: 'total' },
  { id: 'area', label: 'Cell area (µm²)', column: 'area' },
  { id: 'nucleusArea', label: 'Nucleus area (µm²)', column: 'nucleusArea' },
  { id: 'method', label: 'Segmentation method', categorical: true },
];

export function defaultLayers(desc) {
  const used = new Set();
  const channels = desc.channels.map((ch) => {
    const purpose = `${ch.purpose ?? ''} ${/dapi/i.test(ch.name) ? 'nuclear' : ''}`;
    let color = PURPOSE_COLORS.find(([re]) => re.test(purpose))?.[1];
    if (!color || used.has(color)) color = CHANNEL_COLORS.find((c) => !used.has(c)) ?? '#ffffff';
    used.add(color);
    return { visible: true, color, window: [...ch.range], gamma: 1, purpose };
  });
  // Four stains added together wash out: the nuclear and boundary stains show at first (or the
  // first two channels when none says which it is), the interior stains a click away.
  const structural = channels.filter((c) => /nuclear|boundary/i.test(c.purpose));
  for (const [i, c] of channels.entries()) {
    c.visible = structural.length ? structural.includes(c) : i < 2;
    delete c.purpose;
  }
  return {
    channels,
    transcripts: { visible: true, all: false, genes: [], colors: [], minQ: 20, pointSize: 3, gain: 1, allColor: '#ffd84d', colorBy: 'gene' },
    // segmentation: null for the dataset's own (the instrument's), an artifact's id, or undefined
    // for the dataset's default (the Viewer resolves it).
    cells: { outlines: true, nuclei: false, fill: null, opacity: 0.55, segmentation: undefined },
  };
}

// Numbers on viridis over their 1st–99th percentiles, unknown values transparent: RGBA per cell.
function numericColors(values) {
  const out = new Uint8Array(4 * values.length);
  const [lo, hi] = attributeRange(values);
  const lut = colormapLUT('viridis');
  for (let i = 0; i < values.length; i += 1) {
    const v = values[i];
    if (!Number.isFinite(v)) continue;
    const k = Math.max(0, Math.min(255, Math.round((255 * (v - lo)) / (hi - lo))));
    out.set([lut[3 * k], lut[3 * k + 1], lut[3 * k + 2], 255], 4 * i);
  }
  return out;
}

// The fills a cells table offers: the dataset's own, or a segmentation's (lib/segview.js).
export function fillsOf(cells, desc) {
  if (cells?.kind === 'segmentation') return segFills(cells);
  return FILLS.filter((f) => !f.categorical || desc.cells.methods?.length);
}

// RGBA bytes per cell for a fill (null for none): numbers on viridis over their 1st–99th
// percentiles, methods in categorical colors; unknown values transparent. A segmentation's own
// fills are colored as lib/segview.js says.
export function cellFillColors(fill, cells, desc) {
  if (!fill || !cells) return null;
  if (cells.kind === 'segmentation') return segFillColors(fill, cells, numericColors);
  const spec = FILLS.find((f) => f.id === fill);
  if (!spec) return null;
  const out = new Uint8Array(4 * cells.n);
  if (spec.categorical) {
    const colors = desc.cells.methods?.map((_, i) => categoricalColor(i)) ?? [];
    for (let i = 0; i < cells.n; i += 1) {
      const c = colors[cells.method[i]];
      if (!c) continue;
      const v = parseInt(c.slice(1), 16);
      out.set([(v >> 16) & 255, (v >> 8) & 255, v & 255, 255], 4 * i);
    }
    return out;
  }
  return numericColors(cells[spec.column]);
}

// segmentations: the dataset's, to choose which is drawn ({ id, name, isDefault, instrument,
// about }; lib/segview.js segmentationChoices); drawn: the one drawn (its choice, or null for the
// dataset's own before any is registered).
// The certainty scale's legend: doubtful (bright) to certain (dark).
function certaintyLegend(label, help) {
  return h('div', h('b', label), h('div.viewer-legend-bar', { style: { background: certaintyCSS() } }),
    h('div.viewer-legend-range', h('span', `${CERTAINTY_RANGE[0]} or less`), h('span', String(CERTAINTY_RANGE[1]))), help ? h('div.muted', help) : null);
}

export function mountViewerPanel(host, { desc, layers, cells, onChange, segmentations = [], drawn = null, clusterings = [] }) {
  let state = structuredClone(layers);
  const emit = (what) => onChange(structuredClone(state), what);
  const genes = desc.transcripts?.genes ?? [];

  // --- Channels ---
  const channelList = h('div.viewer-channels');
  function renderChannels() {
    clear(channelList);
    desc.channels.forEach((info, i) => {
      const ch = state.channels[i];
      const top = Math.max(info.max, info.range[1]) * 1.25;
      const ceiling = Math.min(2 ** info.bits - 1, Math.ceil(top / 10) * 10 || 1);
      const lo = h('input', { type: 'range', min: 0, max: ceiling, step: 1, value: ch.window[0], 'aria-label': `${info.name}: lowest value shown` });
      const hi = h('input', { type: 'range', min: 0, max: ceiling, step: 1, value: ch.window[1], 'aria-label': `${info.name}: value shown brightest` });
      const gamma = h('input', { type: 'range', min: -1.2, max: 1.2, step: 0.05, value: Math.log2(ch.gamma), 'aria-label': `${info.name}: gamma` });
      const values = h('span.mono.muted', `${ch.window[0]}–${ch.window[1]}`);
      const sync = () => {
        let a = Number(lo.value);
        let b = Number(hi.value);
        if (b <= a) b = a + 1;
        ch.window = [a, b];
        ch.gamma = 2 ** Number(gamma.value);
        values.textContent = `${a}–${b}${Math.abs(ch.gamma - 1) > 0.01 ? `, γ ${ch.gamma.toFixed(2)}` : ''}`;
        emit('channels');
      };
      for (const input of [lo, hi, gamma]) input.addEventListener('input', sync);
      const swatch = h('button.viewer-swatch', {
        type: 'button', title: `Color of ${info.name}`, 'aria-label': `Color of ${info.name}`, style: { background: ch.color },
        onclick: (event) => showMenu(event.currentTarget, SWATCHES.map(([label, color]) => ({ label, swatch: color, onSelect: () => {
          ch.color = color;
          renderChannels();
          emit('channels');
        } }))),
      });
      const eye = h('button.icon-button', {
        type: 'button', title: ch.visible ? `Hide ${info.name}` : `Show ${info.name}`, 'aria-pressed': String(ch.visible),
        onclick: () => {
          ch.visible = !ch.visible;
          renderChannels();
          emit('channels');
        },
      }, icon(ch.visible ? 'eye' : 'dot'));
      channelList.append(h(`div.viewer-channel${ch.visible ? '' : '.off'}`,
        h('div.row', eye, swatch, h('div.name', h('b', info.name), info.purpose ? h('span.muted', info.purpose) : null),
          h('button.btn.small.ghost', { type: 'button', title: 'Reset the window to the image\'s own range', onclick: () => {
            ch.window = [...info.range];
            ch.gamma = 1;
            renderChannels();
            emit('channels');
          } }, 'Auto')),
        h('div.ranges', h('label', 'Low', lo), h('label', 'High', hi), h('label', 'Gamma', gamma), values)));
    });
  }

  // --- Transcripts ---
  const chips = h('div.viewer-gene-chips');
  const search = h('input.viewer-gene-search', { type: 'search', placeholder: genes.length ? `Find a gene (${genes.length} features)` : 'No transcripts', disabled: !genes.length, 'aria-label': 'Find a gene', autocomplete: 'off' });
  const suggestions = h('div.viewer-suggestions', { role: 'listbox', hidden: true });
  const allBox = h('input', { type: 'checkbox', checked: state.transcripts.all });
  const q20Box = h('input', { type: 'checkbox', checked: state.transcripts.minQ >= 20 });
  const visibleBox = h('input', { type: 'checkbox', checked: state.transcripts.visible });
  const size = h('input', { type: 'range', min: 1, max: 8, step: 0.5, value: state.transcripts.pointSize, 'aria-label': 'Point size' });
  const gain = h('input', { type: 'range', min: -2, max: 3, step: 0.1, value: Math.log2(state.transcripts.gain), 'aria-label': 'Density brightness' });
  // Coloring by the assignment's probability needs a segmentation that gives one.
  const probable = Boolean(cells?.holds?.probability);
  const colorBy = h('select', { 'aria-label': 'Color transcripts by' },
    h('option', { value: 'gene' }, 'Gene'),
    h('option', { value: 'probability', disabled: !probable }, probable ? 'Assignment probability' : 'Assignment probability (a refinement\'s)'));
  colorBy.value = probable && state.transcripts.colorBy === 'probability' ? 'probability' : 'gene';
  let active = -1;

  function recolor() {
    state.transcripts.colors = state.transcripts.genes.map((_, i) => categoricalColor(i));
  }

  function renderChips() {
    clear(chips);
    recolor();
    state.transcripts.genes.forEach((g, i) => {
      chips.append(h('span.viewer-gene-chip', h('span.dot', { style: { background: state.transcripts.colors[i] } }), genes[g].name,
        h('span.muted', formatCount(genes[g].q20)),
        h('button.icon-button', { type: 'button', title: `Remove ${genes[g].name}`, 'aria-label': `Remove ${genes[g].name}`, onclick: () => removeGene(g) }, icon('close'))));
    });
    if (!state.transcripts.genes.length && !state.transcripts.all) chips.append(h('p.muted.viewer-hint', 'Find genes to show their transcripts, or show all transcripts in one color.'));
  }

  function addGene(g) {
    if (g < 0 || g >= genes.length) return;
    if (!state.transcripts.genes.includes(g)) {
      if (state.transcripts.genes.length >= MAX_GENES) state.transcripts.genes.shift();
      state.transcripts.genes.push(g);
    }
    state.transcripts.all = false;
    allBox.checked = false;
    state.transcripts.visible = true;
    visibleBox.checked = true;
    renderChips();
    emit('genes');
  }

  function removeGene(g) {
    state.transcripts.genes = state.transcripts.genes.filter((x) => x !== g);
    renderChips();
    emit('genes');
  }

  function matches(text) {
    const q = text.trim().toLowerCase();
    if (!q) return [];
    const scored = [];
    genes.forEach((g, i) => {
      const name = g.name.toLowerCase();
      const at = name.indexOf(q);
      if (at < 0) return;
      scored.push({ i, rank: name === q ? 0 : at === 0 ? 1 : 2, count: g.q20 });
    });
    scored.sort((a, b) => a.rank - b.rank || b.count - a.count);
    return scored.slice(0, 30).map((s) => s.i);
  }

  function renderSuggestions() {
    const found = matches(search.value);
    clear(suggestions);
    suggestions.hidden = !found.length;
    active = Math.min(active, found.length - 1);
    found.forEach((g, k) => {
      const kind = genes[g].kind === 'gene' ? '' : genes[g].kind.replace(/_/g, ' ');
      suggestions.append(h(`button.viewer-suggestion${k === active ? '.active' : ''}`, { type: 'button', role: 'option', 'aria-selected': String(k === active), onmousedown: (event) => {
        event.preventDefault();
        choose(g);
      } }, h('b', genes[g].name), kind ? h('span.muted', kind) : null, h('span.spacer'), h('span.muted', formatCount(genes[g].q20))));
    });
    return found;
  }

  function choose(g) {
    addGene(g);
    search.value = '';
    active = -1;
    renderSuggestions();
  }

  search.addEventListener('input', () => {
    active = 0;
    renderSuggestions();
  });
  search.addEventListener('keydown', (event) => {
    const found = matches(search.value);
    if (event.key === 'ArrowDown') active = Math.min(found.length - 1, active + 1);
    else if (event.key === 'ArrowUp') active = Math.max(0, active - 1);
    else if (event.key === 'Enter' && found.length) {
      event.preventDefault();
      choose(found[Math.max(0, active)]);
      return;
    } else if (event.key === 'Escape') {
      search.value = '';
    } else return;
    event.preventDefault();
    renderSuggestions();
  });
  search.addEventListener('blur', () => {
    suggestions.hidden = true;
  });
  search.addEventListener('focus', () => renderSuggestions());
  allBox.addEventListener('change', () => {
    state.transcripts.all = allBox.checked;
    if (allBox.checked) state.transcripts.visible = visibleBox.checked = true;
    renderChips();
    emit('genes');
  });
  q20Box.addEventListener('change', () => {
    state.transcripts.minQ = q20Box.checked ? 20 : 0;
    emit('transcripts');
  });
  visibleBox.addEventListener('change', () => {
    state.transcripts.visible = visibleBox.checked;
    emit('transcripts');
  });
  size.addEventListener('input', () => {
    state.transcripts.pointSize = Number(size.value);
    emit('transcripts');
  });
  gain.addEventListener('input', () => {
    state.transcripts.gain = 2 ** Number(gain.value);
    emit('transcripts');
  });
  colorBy.addEventListener('change', () => {
    state.transcripts.colorBy = colorBy.value;
    emit('transcripts');
  });

  // --- Cells ---
  const outlines = h('input', { type: 'checkbox', checked: state.cells.outlines });
  const nuclei = h('input', { type: 'checkbox', checked: state.cells.nuclei });
  // The cells' own fills, then the clusterings and label sets of the segmentation drawn
  // (lib/clusterings.js, lib/labels.js).
  const fills = [...fillsOf(cells, desc), ...clusterings];
  const fill = h('select', { 'aria-label': 'Fill cells by' }, h('option', { value: '' }, 'No fill'), ...fills.map((f) => h('option', { value: f.id, title: f.help ?? '' }, f.label)));
  fill.value = fills.some((f) => f.id === state.cells.fill) ? state.cells.fill : '';
  // Which segmentation is drawn: the dataset's, its default first.
  const segPicker = h('select', { 'aria-label': 'Segmentation drawn' }, ...segmentations.map((c) => h('option', { value: c.id }, `${c.name}${c.isDefault ? ' (default)' : ''}`)));
  if (drawn) segPicker.value = drawn.id;
  segPicker.disabled = segmentations.length < 2;
  segPicker.addEventListener('change', () => {
    const c = segmentations.find((x) => x.id === segPicker.value);
    state.cells.segmentation = !c || c.instrument ? null : c.id;
    emit('segmentation');
  });
  const opacity = h('input', { type: 'range', min: 0.1, max: 1, step: 0.05, value: state.cells.opacity, 'aria-label': 'Fill opacity' });
  const sets = new Set((desc.geometry ?? []).map((s) => s.name));
  outlines.disabled = !sets.has('cells');
  nuclei.disabled = !sets.has('nuclei');
  outlines.addEventListener('change', () => {
    state.cells.outlines = outlines.checked;
    emit('cells');
  });
  nuclei.addEventListener('change', () => {
    state.cells.nuclei = nuclei.checked;
    emit('cells');
  });
  fill.addEventListener('change', () => {
    state.cells.fill = fill.value || null;
    emit('fill');
  });
  opacity.addEventListener('input', () => {
    state.cells.opacity = Number(opacity.value);
    emit('cells');
  });

  const section = (title, iconName, ...content) => h('section.viewer-section', h('h3', icon(iconName), title), ...content);
  const toggle = (box, label) => h('label.viewer-toggle', box, label);
  host.append(
    desc.channels.length ? section('Image', 'image', channelList) : null,
    desc.transcripts ? section('Transcripts', 'dots',
      toggle(visibleBox, 'Show transcripts'),
      h('div.viewer-gene-find', search, suggestions),
      chips,
      toggle(allBox, `All transcripts (${formatCount(desc.transcripts.count)})`),
      toggle(q20Box, 'Quality 20 or more only'),
      h('label.viewer-slider', 'Color by', colorBy),
      h('label.viewer-slider', 'Point size', size),
      h('label.viewer-slider', 'Density brightness', gain),
      h('p.muted.viewer-hint', 'Close up, each transcript is a point; farther out, each gene\'s density, counted in bins a few pixels wide. A transcript\'s probability shows on its point, close up.')) : null,
    desc.geometry?.length ? section('Cells', 'cell',
      segmentations.length ? h('label.viewer-slider', 'Segmentation', segPicker) : null,
      drawn ? h('p.muted.viewer-hint.viewer-seg-about', `${drawn.about[0].toUpperCase()}${drawn.about.slice(1)}.`) : null,
      toggle(outlines, `Cell outlines (${formatCount(cells?.n ?? desc.cells.count)})`),
      toggle(nuclei, 'Nucleus outlines'),
      h('label.viewer-slider', 'Fill by', fill),
      h('label.viewer-slider', 'Fill opacity', opacity)) : null);
  renderChannels();
  renderChips();

  // The transcripts' probability legend, when they are colored by it.
  function probabilityLegend() {
    if (!(probable && state.transcripts.colorBy === 'probability' && state.transcripts.visible)) return null;
    return h('div', certaintyLegend('Transcripts by their assignment\'s probability', 'Close up, each transcript is drawn by its probability of the cell (or the background) it is in.'),
      h('div.viewer-legend-item', h('span.dot', { style: { background: NO_PROBABILITY_COLOR } }), 'Assigned by place (not modelled)'));
  }

  return {
    probabilityLegend,
    addGene,
    setGenes(list) {
      state.transcripts.genes = list.filter((g) => g >= 0 && g < genes.length).slice(0, MAX_GENES);
      state.transcripts.all = false;
      allBox.checked = false;
      renderChips();
      emit('genes');
    },
    setTranscripts(patch) {
      Object.assign(state.transcripts, patch);
      allBox.checked = state.transcripts.all;
      q20Box.checked = state.transcripts.minQ >= 20;
      visibleBox.checked = state.transcripts.visible;
      renderChips();
      emit('genes');
    },
    recolor() {
      renderChips();
      emit('genes');
    },
    // The fill's legend: a color bar with its range, or the methods' colors; a segmentation's
    // limitations' colors, or the certainty scale.
    legend() {
      const id = state.cells.fill;
      // A clustering's or label set's legend is the Viewer's, once its cells are read.
      if (id?.startsWith('clusters:') || id?.startsWith('labels:')) return null;
      if (cells?.kind === 'segmentation') {
        const spec = fills.find((f) => f.id === id);
        if (!spec) return null;
        if (spec.id === 'limitations') {
          const count = (test) => {
            let n = 0;
            for (let i = 0; i < cells.n; i += 1) if (test(cells.limitations[i])) n += 1;
            return n;
          };
          const single = (bit) => (f) => f === bit;
          return h('div', h('b', spec.label),
            ...LIMITATIONS.map((l) => h('div.viewer-legend-item', h('span.dot', { style: { background: l.color } }), l.label, h('span.muted', formatCount(count(single(l.bit)))))),
            h('div.viewer-legend-item', h('span.dot', { style: { background: SEVERAL.color } }), SEVERAL.label, h('span.muted', formatCount(count((f) => f && (f & (f - 1)) !== 0)))),
            h('div.muted', 'Cells without one are not filled.'));
        }
        if (spec.scale === 'certainty') return certaintyLegend(spec.label, spec.help);
        const [lo, hi] = attributeRange(cells[spec.column]);
        const fmt = (v) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));
        return h('div', h('b', spec.label), h('div.viewer-legend-bar', { style: { background: colormapCSS('viridis') } }), h('div.viewer-legend-range', h('span', fmt(lo)), h('span', fmt(hi))), h('div.muted', '1st to 99th percentile'));
      }
      const spec = FILLS.find((f) => f.id === id);
      if (!spec) return null;
      if (spec.categorical) {
        return h('div', h('b', spec.label), ...(desc.cells.methods ?? []).map((m, i) => h('div.viewer-legend-item', h('span.dot', { style: { background: categoricalColor(i) } }), m.replace(/^Segmented by /, ''), h('span.muted', formatCount(desc.cells.methodCounts?.[i] ?? 0)))));
      }
      const [lo, hi] = attributeRange(cells[spec.column]);
      const fmt = (v) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));
      return h('div', h('b', spec.label), h('div.viewer-legend-bar', { style: { background: colormapCSS('viridis') } }), h('div.viewer-legend-range', h('span', fmt(lo)), h('span', fmt(hi))), h('div.muted', '1st to 99th percentile'));
    },
  };
}
