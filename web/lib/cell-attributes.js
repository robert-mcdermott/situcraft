// The cells' QC attributes, computed from the cells table (lib/viewer-data.js readCells): what
// the QC maps color, the plots show and the filters test. Each is a value per cell, NaN where a
// cell has none (an area the bundle does not record, a share of no transcripts).

export const ATTRIBUTES = [
  { id: 'transcripts', label: 'Gene transcripts', integer: true, help: 'Transcripts of genes with quality 20 or more assigned to the cell (cells.parquet transcript_counts).' },
  { id: 'genes', label: 'Genes detected', integer: true, help: 'Genes with at least one such transcript in the cell.' },
  { id: 'total', label: 'All transcripts', integer: true, help: 'Every transcript with quality 20 or more assigned to the cell, controls and codewords included (total_counts).' },
  { id: 'controls', label: 'Negative controls', integer: true, help: 'Negative control probes and codewords assigned to the cell: they should be rare.' },
  { id: 'area', label: 'Cell area', unit: 'µm²', help: 'The area of the cell\'s segmentation (cells.parquet cell_area).' },
  { id: 'nucleusArea', label: 'Nucleus area', unit: 'µm²', help: 'The area of its nucleus (nucleus_area).' },
  { id: 'nucleusShare', label: 'Nucleus share of the cell', help: 'Nucleus area divided by cell area.' },
  { id: 'density', label: 'Transcript density', unit: 'per 100 µm²', help: 'Gene transcripts per 100 µm² of the cell.' },
  { id: 'nuclearShare', label: 'Transcripts in the nucleus', help: 'The share of its gene transcripts that overlap the nucleus.' },
  { id: 'method', label: 'Segmentation method', categorical: true, help: 'How the instrument found the cell (cells.zarr.zip).' },
];

export function attribute(id) {
  return ATTRIBUTES.find((a) => a.id === id) ?? null;
}

const cache = new WeakMap();

// The values of an attribute, one per cell (a Float64Array; for the segmentation method, the
// method's index, NaN when unknown).
export function attributeValues(cells, id) {
  let mine = cache.get(cells);
  if (!mine) {
    mine = new Map();
    cache.set(cells, mine);
  }
  if (mine.has(id)) return mine.get(id);
  const n = cells.n;
  const out = new Float64Array(n);
  const ratio = (a, b) => (b > 0 && Number.isFinite(a) ? a / b : NaN);
  for (let i = 0; i < n; i += 1) {
    switch (id) {
      case 'transcripts': out[i] = cells.transcripts[i]; break;
      case 'genes': out[i] = cells.genes[i]; break;
      case 'total': out[i] = cells.total[i]; break;
      case 'controls': out[i] = cells.controlProbes[i] + cells.controlCodewords[i]; break;
      case 'area': out[i] = cells.area[i]; break;
      case 'nucleusArea': out[i] = cells.nucleusArea[i]; break;
      case 'nucleusShare': out[i] = ratio(cells.nucleusArea[i], cells.area[i]); break;
      case 'density': out[i] = ratio(100 * cells.transcripts[i], cells.area[i]); break;
      case 'nuclearShare': out[i] = ratio(cells.nuclear[i], cells.transcripts[i]); break;
      case 'method': out[i] = cells.method[i] === 255 ? NaN : cells.method[i]; break;
      default: throw new Error(`no cell attribute ${id}`);
    }
  }
  mine.set(id, out);
  return out;
}
