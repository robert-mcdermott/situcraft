// The tissue viewer's answers from the program (internal/view, api_view.go), read in place as
// typed arrays (no copying, no parsing), and what is computed from them. Little-endian, as every
// machine SituCraft runs on is. Without the DOM (tested in Node).

function header(buffer, magic) {
  if (buffer.byteLength < 16) throw new Error(`a ${magic} answer of ${buffer.byteLength} bytes`);
  const bytes = new Uint8Array(buffer, 0, 4);
  const got = String.fromCharCode(...bytes);
  if (got !== magic) throw new Error(`expected a ${magic} answer, got ${JSON.stringify(got)}`);
  const words = new Uint32Array(buffer, 4, 3);
  return [words[0], words[1], words[2]];
}

const pad4 = (n) => (n + 3) & ~3;

// Points (SCVP): { n, genes: Uint16Array, starts: Uint32Array, x, y: Float32Array, cell: Uint32Array }.
export function readPoints(buffer) {
  const [n, g] = header(buffer, 'SCVP');
  let at = 16;
  const starts = new Uint32Array(buffer, at, g + 1);
  at += 4 * (g + 1);
  const x = new Float32Array(buffer, at, n);
  at += 4 * n;
  const y = new Float32Array(buffer, at, n);
  at += 4 * n;
  const cell = new Uint32Array(buffer, at, n);
  at += 4 * n;
  const genes = new Uint16Array(buffer, at, g);
  at = pad4(at + 2 * g);
  if (at !== buffer.byteLength) throw new Error('a points answer of the wrong length');
  return { n, genes, starts, x, y, cell };
}

// Density grids (SCVD): { bins, genes, starts, count: Uint32Array, bin: Uint16Array }.
export function readDensity(buffer) {
  const [bins, g, e] = header(buffer, 'SCVD');
  let at = 16;
  const starts = new Uint32Array(buffer, at, g + 1);
  at += 4 * (g + 1);
  const count = new Uint32Array(buffer, at, e);
  at += 4 * e;
  const genes = new Uint16Array(buffer, at, g);
  at = pad4(at + 2 * g);
  const bin = new Uint16Array(buffer, at, e);
  at = pad4(at + 2 * e);
  if (at !== buffer.byteLength) throw new Error('a density answer of the wrong length');
  return { bins, genes, starts, count, bin };
}

// A gene's grid of a density answer as a bins × bins Float32Array (row-major, y down), or null.
export function densityGrid(density, gene) {
  const k = density.genes.indexOf(gene);
  if (k < 0) return null;
  const grid = new Float32Array(density.bins * density.bins);
  for (let i = density.starts[k]; i < density.starts[k + 1]; i += 1) grid[density.bin[i]] = density.count[i];
  return grid;
}

// The cells table (SCVC; internal/view CellTable): { n, methods, x, y, area, nucleusArea,
// transcripts, total, controlProbes, controlCodewords, genes, nuclear, method }.
export function readCells(buffer) {
  const [n, methods, columns] = header(buffer, 'SCVC');
  if (columns !== 8) throw new Error(`a cells table of ${columns} columns, not 8 (an older program?)`);
  let at = 16;
  const take = (Type) => {
    const a = new Type(buffer, at, n);
    at += Type.BYTES_PER_ELEMENT * n;
    return a;
  };
  const out = {
    n, methods,
    x: take(Float32Array), y: take(Float32Array), area: take(Float32Array), nucleusArea: take(Float32Array),
    transcripts: take(Uint32Array), total: take(Uint32Array), controlProbes: take(Uint32Array), controlCodewords: take(Uint32Array),
    genes: take(Uint32Array), nuclear: take(Uint32Array), method: take(Uint8Array),
  };
  if (pad4(at) !== buffer.byteLength) throw new Error('a cells answer of the wrong length');
  return out;
}

// An image tile's samples (the bytes split in a low and a high plane) as 16-bit values.
export function joinSamples(split, width, height) {
  const n = width * height;
  if (split.length !== 2 * n) throw new Error(`a ${width}×${height} tile of ${split.length} bytes`);
  const out = new Uint16Array(n);
  for (let i = 0; i < n; i += 1) out[i] = split[i] | (split[n + i] << 8);
  return out;
}

// The cell nearest (x, y) within maxDistance (µm), by centroid: its row, or -1.
export function nearestCell(cells, x, y, maxDistance) {
  let best = -1;
  let bestD = maxDistance * maxDistance;
  for (let i = 0; i < cells.n; i += 1) {
    const dx = cells.x[i] - x;
    const dy = cells.y[i] - y;
    const d = dx * dx + dy * dy;
    if (d <= bestD) {
      best = i;
      bestD = d;
    }
  }
  return best;
}

// Robust bounds of a cell attribute for a color scale: the 1st and 99th percentiles of its finite
// values ([0, 1] when it has none).
export function attributeRange(values) {
  const finite = [];
  for (const v of values) if (Number.isFinite(v)) finite.push(v);
  if (!finite.length) return [0, 1];
  finite.sort((a, b) => a - b);
  const at = (q) => finite[Math.min(finite.length - 1, Math.max(0, Math.floor(q * (finite.length - 1))))];
  const lo = at(0.01);
  const hi = at(0.99);
  return hi > lo ? [lo, hi] : [lo, lo + 1];
}
