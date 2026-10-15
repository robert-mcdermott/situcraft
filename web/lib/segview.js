// Any segmentation on the tissue (wave 3, slice 8): what the Viewer needs to draw one other than
// the instrument's, without the DOM (tested in Node).
//
// - The segmentations a dataset can show, its default first, and a plain account of how each was
//   made, for the Viewer's list and legend.
// - A segmentation's cells table (api_segview.go, "SCVS"), read in place.
// - Its fills: the counts and areas every segmentation has, and what a transcript-informed one
//   adds: its flagged limitations, each cell's boundary certainty, and its transcripts' mean
//   probability. Certainties go from 0.5 (a coin's toss) to 1 (every sample agreed) through a
//   reversed inferno, so the doubtful cells are the bright ones.
// - Transcripts colored by their assignment's probability on the same scale, in the points
//   shader's 16 color slots: 15 bins, and one for a transcript without a probability.

import { isRefinement } from './refine.js';
import { promotedSegmentation, segmentationsOf } from './segmentations.js';
import { colormapColor, hexToRgb } from './colormaps.js';

// The instrument's segmentation is drawn from the dataset's own data, named so in views.
export const INSTRUMENT = 'instrument';

const PRESET_NAMES = { balanced: 'Balanced', conservative: 'Conservative boundary', flexible: 'Flexible morphology', 'low-memory': 'Low memory', 'roi-pilot': 'ROI pilot', expert: 'Expert' };

// The segmentations of a dataset the Viewer can draw: the complete ones, the default first, then
// as they were made. Each: { id, name, source, isDefault, instrument, about }.
export function segmentationChoices(project, datasetId) {
  const promoted = promotedSegmentation(project, datasetId)?.record.id ?? null;
  const list = segmentationsOf(project, datasetId).filter((a) => a.state === 'complete');
  const choices = list.map((a) => ({ id: a.id, name: a.name, source: a.source, isDefault: a.id === promoted, instrument: a.source === 'instrument', about: howMade(a) }));
  return [...choices.filter((c) => c.isDefault), ...choices.filter((c) => !c.isDefault)];
}

// The segmentation a dataset shows when nothing says which: its default, else the instrument's.
export function defaultChoice(project, datasetId) {
  const choices = segmentationChoices(project, datasetId);
  return choices.find((c) => c.isDefault) ?? choices.find((c) => c.instrument) ?? null;
}

// How a segmentation was made, in a few plain words.
export function howMade(record) {
  const version = (name) => record.software?.find((s) => s.name === name)?.version ?? '';
  const by = version('situcraft') ? `SituCraft ${version('situcraft')}` : 'SituCraft';
  const p = record.parameters ?? {};
  switch (record.source) {
    case 'instrument': {
      const methods = (record.method?.methods ?? []).filter((m) => m.cells > 0).map((m) => m.method.replace(/^Segmented by /, '').replace(/(\d)µm/, '$1 µm'));
      return `the instrument's own${methods.length ? `: ${methods.join('; ')}` : ''}`;
    }
    case 'imported':
      return `${record.method?.name || 'another tool'}${record.method?.version ? ` ${record.method.version}` : ''}, imported`;
    case 'planted':
      return 'errors planted in the instrument\'s segmentation for practice (simulated)';
    default:
      if (p.distance_um !== undefined) return `nucleus expansion by ${p.distance_um} µm, by ${by}`;
      if (isRefinement(record)) {
        const sweeps = (p.stages ?? []).reduce((n, s) => n + (s.sweeps ?? 0), 0);
        const preset = PRESET_NAMES[p.preset] ?? (p.preset ? p.preset : 'its own settings');
        return `transcript-informed, ${preset}${sweeps ? `, ${sweeps.toLocaleString('en-US')} sweeps` : ''}${p.seed !== undefined ? `, seed ${p.seed}` : ''}, by ${by}`;
      }
      return `${record.method?.name || 'SituCraft'}, by ${by}`;
  }
}

const pad4 = (n) => (n + 3) & ~3;

// A segmentation's cells table: { n, kind: 'segmentation', holds: { limitations, certainty,
// probability }, x, y, area, nucleusArea, transcripts, total, genes, instrument, certainty,
// probability, limitations }. total is every transcript assigned to the cell, as the dataset's
// table names it.
export function readSegCells(buffer) {
  if (buffer.byteLength < 16) throw new Error(`a cells answer of ${buffer.byteLength} bytes`);
  const magic = String.fromCharCode(...new Uint8Array(buffer, 0, 4));
  if (magic !== 'SCVS') throw new Error(`expected a segmentation's cells table, got ${JSON.stringify(magic)}`);
  const [n, holds, columns] = new Uint32Array(buffer, 4, 3);
  if (columns !== 11) throw new Error(`a cells table of ${columns} columns, not 11 (an older program?)`);
  let at = 16;
  const take = (Type) => {
    const a = new Type(buffer, at, n);
    at += Type.BYTES_PER_ELEMENT * n;
    return a;
  };
  const out = {
    n, kind: 'segmentation',
    holds: { limitations: Boolean(holds & 1), certainty: Boolean(holds & 2), probability: Boolean(holds & 4) },
    x: take(Float32Array), y: take(Float32Array), area: take(Float32Array), nucleusArea: take(Float32Array),
    transcripts: take(Uint32Array), total: take(Uint32Array), genes: take(Uint32Array), instrument: take(Uint32Array),
    certainty: take(Float32Array), probability: take(Float32Array), limitations: take(Uint8Array),
  };
  if (pad4(at) !== buffer.byteLength) throw new Error('a cells answer of the wrong length');
  return out;
}

// Limitations by bit, with their colors (a cell flagged with several is drawn in red).
export const LIMITATIONS = [
  { bit: 1, label: 'Few transcripts', color: '#ff9f43' },
  { bit: 2, label: 'Uncertain boundary', color: '#ff5ca8' },
  { bit: 4, label: 'No nucleus', color: '#3fd6e8' },
];
export const SEVERAL = { label: 'Several', color: '#ff4040' };

// The fills a segmentation's cells can show: every segmentation's, then a transcript-informed
// one's (needs: what its table must hold).
export const SEG_FILLS = [
  { id: 'transcripts', label: 'Gene transcripts', column: 'transcripts' },
  { id: 'total', label: 'All transcripts', column: 'total' },
  { id: 'area', label: 'Cell area (µm²)', column: 'area' },
  { id: 'nucleusArea', label: 'Nucleus area (µm²)', column: 'nucleusArea' },
  { id: 'limitations', label: 'Limitations flagged', needs: 'limitations', categorical: true },
  { id: 'certainty', label: 'Boundary certainty', needs: 'certainty', column: 'certainty', scale: 'certainty', help: 'The mean share of the samples in which its pixels were its: 1, every sample agreed; 0.5, a coin\'s toss.' },
  { id: 'probability', label: 'Assignment certainty', needs: 'probability', column: 'probability', scale: 'certainty', help: 'Its transcripts\' mean probability of being its.' },
];

// The fills a table offers.
export function segFills(cells) {
  return SEG_FILLS.filter((f) => !f.needs || cells?.holds?.[f.needs]);
}

// The certainty scale: 0.5 or less is the brightest, 1 the darkest.
export const CERTAINTY_RANGE = [0.5, 1];
export function certaintyColor(v) {
  const t = Math.min(1, Math.max(0, (v - CERTAINTY_RANGE[0]) / (CERTAINTY_RANGE[1] - CERTAINTY_RANGE[0])));
  // inferno from its brightest (doubtful) to a deep violet, still visible over the tissue.
  return colormapColor('inferno', 1 - 0.8 * t);
}

// CSS for the certainty scale's bar, doubtful (left) to certain (right).
export function certaintyCSS(steps = 12) {
  const stops = [];
  for (let i = 0; i <= steps; i += 1) {
    const v = CERTAINTY_RANGE[0] + ((CERTAINTY_RANGE[1] - CERTAINTY_RANGE[0]) * i) / steps;
    stops.push(`${certaintyColor(v)} ${((100 * i) / steps).toFixed(1)}%`);
  }
  return `linear-gradient(90deg, ${stops.join(', ')})`;
}

// RGBA bytes per cell for a segmentation's fill, or null for none (or one it does not offer):
// limitations in their colors (unflagged cells clear), certainties on the certainty scale, the
// rest through `numeric` (the dataset's fills' own coloring, viridis over their percentiles).
export function segFillColors(fill, cells, numeric) {
  const spec = segFills(cells).find((f) => f.id === fill);
  if (!spec) return null;
  const out = new Uint8Array(4 * cells.n);
  const put = (i, hex) => {
    const [r, g, b] = hexToRgb(hex);
    out.set([r, g, b, 255], 4 * i);
  };
  if (spec.id === 'limitations') {
    for (let i = 0; i < cells.n; i += 1) {
      const f = cells.limitations[i];
      if (!f) continue;
      const one = LIMITATIONS.filter((l) => f & l.bit);
      put(i, one.length === 1 ? one[0].color : SEVERAL.color);
    }
    return out;
  }
  if (spec.scale === 'certainty') {
    const values = cells[spec.column];
    for (let i = 0; i < cells.n; i += 1) if (Number.isFinite(values[i])) put(i, certaintyColor(values[i]));
    return out;
  }
  return numeric(cells[spec.column]);
}

// Transcripts by their probability: the slot (0–14 by probability, 15 for none) and the 16 slot
// colors.
export function probabilitySlot(p) {
  if (!Number.isFinite(p)) return 15;
  const t = Math.min(1, Math.max(0, (p - CERTAINTY_RANGE[0]) / (CERTAINTY_RANGE[1] - CERTAINTY_RANGE[0])));
  return Math.min(14, Math.floor(t * 15));
}
export const NO_PROBABILITY_COLOR = '#8a8a8a';
export function probabilityColors() {
  const out = [];
  for (let k = 0; k < 15; k += 1) out.push(certaintyColor(CERTAINTY_RANGE[0] + ((k + 0.5) / 15) * (CERTAINTY_RANGE[1] - CERTAINTY_RANGE[0])));
  out.push(NO_PROBABILITY_COLOR);
  return out;
}
