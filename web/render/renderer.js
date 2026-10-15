// The tissue renderer: WebGL2 drawing of a dataset's derived data (api_view.go) at any zoom.
//
// - The morphology channels are 16-bit tiles kept as two byte planes in an integer texture and
//   read with texelFetch, so the shader sees each sample exactly; its window, gamma and color
//   are applied there, and channels add up. The level drawn has pixels no larger than a device
//   pixel; until its tiles arrive, coarser tiles stand in (a stencil keeps each pixel drawn once
//   per channel).
// - Transcripts are points (full-resolution tiles, the chosen genes) when few enough tiles are in
//   view, and otherwise density grids at the level whose bins are a few pixels wide.
// - Cell and nucleus outlines come from the geometry level matching the image level (simplified
//   to half a pixel there); cells can be filled by an attribute; the selected cell is outlined
//   from its full-resolution polygons, and cells chosen elsewhere (a brushed plot) highlighted.
// - A QC map colors each density bin by a ratio of two groups of transcripts (the share of gene
//   transcripts outside cells, of quality below 20) or by a count (negative controls), through a
//   colormap.
//
// Coordinates are µm; the camera (lib/viewport.js) maps them to the canvas.
//
// A descriptor may name where its outline tiles are (polygonsBase): the comparison workspace
// draws each segmentation's outlines on the same tissue. A transparent renderer draws only what
// its layers ask on a clear canvas, for an overlay.

import { createContext, createProgram, arrayBuffer, vertexArray, byteTexture, floatTexture, colorTexture, hexColor } from './gl.js';
import { TileCache } from './tiles.js';
import { densityLevel, gridTileRange, imageLevel, tileRange, tileUm, viewMatrix, visibleRect, screenToWorld } from '../lib/viewport.js';
import { readPolygonTile, outlineSegments, fillTriangles, pointInRing } from '../lib/polygons.js';
import { readPoints, readDensity, densityGrid } from '../lib/viewer-data.js';

const QUAD = new Float32Array([0, 0, 1, 0, 0, 1, 1, 1]);

const IMAGE_VS = `#version 300 es
in vec2 a_pos;
uniform mat3 u_view;
uniform vec2 u_origin;
uniform vec2 u_size;
uniform vec2 u_texels;
out vec2 v_tex;
void main() {
  vec2 world = u_origin + a_pos * u_size;
  v_tex = a_pos * u_texels;
  gl_Position = vec4((u_view * vec3(world, 1.0)).xy, 0.0, 1.0);
}`;

const IMAGE_FS = `#version 300 es
precision highp float;
precision highp usampler2D;
uniform usampler2D u_tex;
uniform ivec2 u_dims;
uniform vec2 u_window;
uniform float u_gamma;
uniform vec3 u_color;
uniform int u_raw;
in vec2 v_tex;
out vec4 o;
void main() {
  ivec2 t = clamp(ivec2(floor(v_tex)), ivec2(0), u_dims - 1);
  uint lo = texelFetch(u_tex, t, 0).r;
  uint hi = texelFetch(u_tex, ivec2(t.x, t.y + u_dims.y), 0).r;
  if (u_raw == 1) {
    o = vec4(float(lo) / 255.0, float(hi) / 255.0, 0.0, 1.0);
    return;
  }
  float v = float(lo) + 256.0 * float(hi);
  float n = clamp((v - u_window.x) / max(u_window.y - u_window.x, 1.0), 0.0, 1.0);
  o = vec4(u_color * pow(n, u_gamma), 1.0);
}`;

const DENSITY_FS = `#version 300 es
precision highp float;
uniform sampler2D u_tex;
uniform vec3 u_color;
uniform float u_scale;
uniform float u_gain;
in vec2 v_tex;
out vec4 o;
void main() {
  float c = texture(u_tex, v_tex / 128.0).r;
  if (c <= 0.0) discard;
  float t = clamp(u_gain * log(1.0 + c) / log(1.0 + u_scale), 0.0, 1.0);
  o = vec4(u_color * t, 1.0);
}`;

// A QC map: a bin's value is num / den, or 1 − num / den (ratio maps), or
// log(1 + num) / log(1 + scale) (count maps), shown through a colormap; bins with fewer than minDen transcripts in the denominator
// are left out (a ratio of few transcripts is noise).
const QCMAP_FS = `#version 300 es
precision highp float;
uniform sampler2D u_num;
uniform sampler2D u_den;
uniform sampler2D u_lut;
uniform int u_ratio;
uniform int u_complement;
uniform float u_minDen;
uniform float u_scale;
uniform vec2 u_range;
uniform float u_opacity;
in vec2 v_tex;
out vec4 o;
void main() {
  vec2 uv = v_tex / 128.0;
  float num = texture(u_num, uv).r;
  float t;
  if (u_ratio == 1) {
    float den = texture(u_den, uv).r;
    if (den < u_minDen || den <= 0.0) discard;
    t = num / den;
    if (u_complement == 1) t = 1.0 - t;
  } else {
    if (num <= 0.0) discard;
    t = log(1.0 + num) / log(1.0 + u_scale);
  }
  t = clamp((t - u_range.x) / max(u_range.y - u_range.x, 1e-9), 0.0, 1.0);
  vec3 color = texture(u_lut, vec2(t * 255.0 / 256.0 + 0.5 / 256.0, 0.5)).rgb;
  o = vec4(color, u_opacity);
}`;

const POINTS_VS = `#version 300 es
in vec2 a_pos;
in vec2 a_attr;
uniform mat3 u_view;
uniform float u_size;
uniform float u_selected;
uniform vec3 u_colors[16];
out vec3 v_color;
out float v_selected;
void main() {
  gl_Position = vec4((u_view * vec3(a_pos, 1.0)).xy, 0.0, 1.0);
  bool selected = a_attr.y == u_selected;
  gl_PointSize = selected ? u_size * 1.7 : u_size;
  v_color = u_colors[int(a_attr.x)];
  v_selected = selected ? 1.0 : 0.0;
}`;

const POINTS_FS = `#version 300 es
precision highp float;
in vec3 v_color;
in float v_selected;
out vec4 o;
void main() {
  vec2 d = gl_PointCoord - 0.5;
  float r = length(d);
  if (r > 0.5) discard;
  float edge = smoothstep(0.5, 0.36, r);
  vec3 color = mix(vec3(0.0), v_color, smoothstep(0.5, 0.38, r) * 0.6 + 0.4);
  o = vec4(mix(color, vec3(1.0), v_selected * 0.35), edge);
}`;

const SHAPE_VS = `#version 300 es
in vec2 a_pos;
in float a_cell;
uniform mat3 u_view;
uniform sampler2D u_colors;
uniform float u_count;
uniform int u_fill;
out vec4 v_color;
void main() {
  gl_Position = vec4((u_view * vec3(a_pos, 1.0)).xy, 0.0, 1.0);
  v_color = vec4(1.0);
  if (u_fill == 1) {
    if (a_cell >= u_count || a_cell < 0.0) {
      v_color = vec4(0.0);
    } else {
      int i = int(a_cell);
      v_color = texelFetch(u_colors, ivec2(i % 4096, i / 4096), 0);
    }
  }
}`;

const SHAPE_FS = `#version 300 es
precision highp float;
uniform vec4 u_tint;
in vec4 v_color;
out vec4 o;
void main() {
  o = vec4(v_color.rgb * u_tint.rgb, v_color.a * u_tint.a);
  if (o.a <= 0.0) discard;
}`;

// Tiles in view are asked for nearest the centre first.
function priority(rect, side, tx, ty) {
  const cx = (rect.x0 + rect.x1) / 2;
  const cy = (rect.y0 + rect.y1) / 2;
  return Math.hypot((tx + 0.5) * side - cx, (ty + 0.5) * side - cy) / side;
}

// How many full-resolution tiles may be in view for transcripts to be drawn as points.
const POINT_TILES = { genes: 64, all: 24 };

export class TissueRenderer {
  constructor(canvas, { onChange, transparent = false } = {}) {
    this.canvas = canvas;
    this.onChange = onChange;
    this.transparent = transparent;
    this.gl = createContext(canvas, { transparent });
    this.dataset = null;
    this.camera = { cx: 0, cy: 0, scale: 1 };
    this.layers = null;
    this.cells = null; // the cells table (lib/viewer-data.js readCells)
    this.qcMap = null; // { ratio, num, den, q20Num, q20Den, minDen, scale, range, lut, opacity }
    this.highlight = null; // { texture }: cells chosen elsewhere, drawn over the tissue
    this.selected = null; // { row, boundaries: [Float32Array], nuclei: [Float32Array] }
    this.frameRequested = false;
    this.lastFrame = { ms: 0, at: 0 };
    this.forced = null; // { level } while capturing
    this.setup();
    canvas.addEventListener('webglcontextlost', (event) => {
      event.preventDefault();
      this.lost = true;
    });
    canvas.addEventListener('webglcontextrestored', () => {
      this.lost = false;
      this.setup();
      this.request();
    });
  }

  setup() {
    const gl = this.gl;
    this.programs = {
      image: createProgram(gl, IMAGE_VS, IMAGE_FS),
      density: createProgram(gl, IMAGE_VS, DENSITY_FS),
      qcmap: createProgram(gl, IMAGE_VS, QCMAP_FS),
      points: createProgram(gl, POINTS_VS, POINTS_FS),
      shape: createProgram(gl, SHAPE_VS, SHAPE_FS),
    };
    this.quad = arrayBuffer(gl, QUAD);
    this.quadVAO = {
      image: vertexArray(gl, [{ location: this.programs.image.a.a_pos, buffer: this.quad, size: 2 }]),
      density: vertexArray(gl, [{ location: this.programs.density.a.a_pos, buffer: this.quad, size: 2 }]),
      qcmap: vertexArray(gl, [{ location: this.programs.qcmap.a.a_pos, buffer: this.quad, size: 2 }]),
    };
    this.cellColors = null;
    this.selectionBuffer = null;
    // The shape program's color lookup reads texture unit 1. A sampler must always have a texture
    // of its kind bound (WebGL refuses a draw otherwise), so a 1×1 one stands in when cells are
    // not filled.
    this.noColors = colorTexture(gl, 1, 1, new Uint8Array([255, 255, 255, 255]));
    const dispose = (value) => value?.dispose?.();
    const onLoad = () => this.request();
    this.caches = {
      image: new TileCache({ load: (key, signal) => this.loadImage(key, signal), dispose, budget: 768 * 2 ** 20, onLoad }),
      points: new TileCache({ load: (key, signal) => this.loadPoints(key, signal), dispose, budget: 256 * 2 ** 20, onLoad }),
      density: new TileCache({ load: (key, signal) => this.loadDensity(key, signal), dispose, budget: 256 * 2 ** 20, onLoad }),
      shapes: new TileCache({ load: (key, signal) => this.loadShapes(key, signal), dispose, budget: 256 * 2 ** 20, onLoad }),
    };
  }

  // The dataset (its descriptor from the program) and its cells table.
  setDataset(desc, cells) {
    for (const c of Object.values(this.caches)) c.clear();
    this.dataset = desc;
    this.cells = cells;
    this.selected = null;
    this.setCellColors(null);
    this.setHighlight(null);
    this.setQCMap(null);
    this.request();
  }

  setCamera(cam) {
    this.camera = { ...cam };
    this.request();
  }

  // layers: { channels: [{ visible, color, window, gamma }], transcripts: { visible, all, genes,
  // colors, minQ, pointSize, gain }, cells: { outlines, nuclei, fill, opacity } }
  setLayers(layers) {
    this.layers = layers;
    this.request();
  }

  // RGBA bytes per cell (4 × cells), or null for no fill.
  setCellColors(rgba) {
    const gl = this.gl;
    if (this.cellColors) gl.deleteTexture(this.cellColors.texture);
    this.cellColors = null;
    if (rgba && this.cells) this.cellColors = { texture: this.cellTexture(rgba) };
    this.request();
  }

  // A per-cell RGBA texture (4096 cells a row).
  cellTexture(rgba) {
    const width = 4096;
    const height = Math.max(1, Math.ceil(this.cells.n / width));
    const data = new Uint8Array(width * height * 4);
    data.set(rgba.subarray(0, Math.min(rgba.length, data.length)));
    return colorTexture(this.gl, width, height, data);
  }

  // Cells to highlight (a Uint8Array mask, 1 for highlighted) in a color, or null for none.
  setHighlight(mask, color = '#ffffff', alpha = 0.6) {
    if (this.highlight) this.gl.deleteTexture(this.highlight.texture);
    this.highlight = null;
    if (mask && this.cells) {
      const [r, g, b] = hexColor(color).map((v) => Math.round(255 * v));
      const rgba = new Uint8Array(4 * this.cells.n);
      for (let i = 0; i < this.cells.n; i += 1) if (mask[i]) rgba.set([r, g, b, Math.round(255 * alpha)], 4 * i);
      this.highlight = { texture: this.cellTexture(rgba) };
    }
    this.request();
  }

  // A QC map ({ ratio: true, num, den, q20Num, q20Den, minDen, range, lut, opacity } with gene
  // indexes of density grids, or { ratio: false, num, q20Num, scale, range, lut, opacity }; lut
  // 256 × 3 bytes), or null.
  setQCMap(map) {
    const gl = this.gl;
    if (this.qcMap?.lutTexture) gl.deleteTexture(this.qcMap.lutTexture);
    this.qcMap = null;
    if (map) {
      const rgba = new Uint8Array(256 * 4);
      for (let i = 0; i < 256; i += 1) rgba.set([map.lut[3 * i], map.lut[3 * i + 1], map.lut[3 * i + 2], 255], 4 * i);
      const lutTexture = colorTexture(gl, 256, 1, rgba);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      this.qcMap = { ...map, lutTexture };
    }
    this.request();
  }

  // The selected cell (from the program's cell detail), or null.
  setSelected(detail) {
    this.selected = detail ? { row: detail.row, boundaries: detail.boundaries.map((r) => Float32Array.from(r)), nuclei: detail.nuclei.map((r) => Float32Array.from(r)) } : null;
    this.request();
  }

  // Device pixels.
  size() {
    return [this.canvas.width, this.canvas.height];
  }

  resize(cssWidth, cssHeight, ratio = window.devicePixelRatio || 1) {
    const w = Math.max(1, Math.round(cssWidth * ratio));
    const h = Math.max(1, Math.round(cssHeight * ratio));
    if (this.canvas.width !== w || this.canvas.height !== h) {
      this.canvas.width = w;
      this.canvas.height = h;
    }
    this.ratio = ratio;
    this.request();
  }

  request() {
    if (this.frameRequested) return;
    this.frameRequested = true;
    requestAnimationFrame(() => {
      this.frameRequested = false;
      this.draw();
    });
  }

  // --- Loading ---------------------------------------------------------------------------------

  async fetch(url, signal) {
    const response = await fetch(url, { signal });
    if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
    return { buffer: await response.arrayBuffer(), headers: response.headers };
  }

  async loadImage(key, signal) {
    const [, c, l, tx, ty] = key.split(':').map(Number);
    const { buffer, headers } = await this.fetch(`/api/cache/${this.dataset.entries.image}/image/${c}/${l}/${tx}/${ty}`, signal);
    const width = Number(headers.get('X-Tile-Width'));
    const height = Number(headers.get('X-Tile-Height'));
    if (buffer.byteLength !== 2 * width * height) throw new Error(`image tile ${key}: ${buffer.byteLength} bytes for ${width}×${height}`);
    const gl = this.gl;
    const texture = byteTexture(gl, width, 2 * height, new Uint8Array(buffer));
    return { value: { texture, width, height, dispose: () => gl.deleteTexture(texture) }, bytes: buffer.byteLength };
  }

  async loadPoints(key, signal) {
    // key: pts:<genes>:<minQ>:<tx>:<ty>
    const [, genes, minQ, tx, ty] = key.split(':');
    const query = `${genes === 'all' ? '' : `genes=${genes}&`}minq=${minQ}`;
    const { buffer } = await this.fetch(`/api/cache/${this.dataset.entries.transcripts}/points/${tx}/${ty}?${query}`, signal);
    const p = readPoints(buffer);
    const slots = genes === 'all' ? null : new Map(genes.split(',').map((g, i) => [Number(g), i]));
    const positions = new Float32Array(2 * p.n);
    const attrs = new Float32Array(2 * p.n);
    for (let k = 0; k < p.genes.length; k += 1) {
      const slot = slots ? slots.get(p.genes[k]) ?? 0 : 0;
      for (let i = p.starts[k]; i < p.starts[k + 1]; i += 1) {
        positions[2 * i] = p.x[i];
        positions[2 * i + 1] = p.y[i];
        attrs[2 * i] = slot;
        attrs[2 * i + 1] = p.cell[i] === 0xffffffff ? -1 : p.cell[i];
      }
    }
    const gl = this.gl;
    const pos = arrayBuffer(gl, positions);
    const attr = arrayBuffer(gl, attrs);
    const prog = this.programs.points;
    const vao = vertexArray(gl, [{ location: prog.a.a_pos, buffer: pos, size: 2 }, { location: prog.a.a_attr, buffer: attr, size: 2 }]);
    return {
      value: { vao, count: p.n, points: p, dispose: () => { gl.deleteVertexArray(vao); gl.deleteBuffer(pos); gl.deleteBuffer(attr); } },
      bytes: 16 * p.n + 64,
    };
  }

  async loadDensity(key, signal) {
    // key: den:<genes>:<q20: 1 or 0>:<level>:<tx>:<ty>
    const [, genes, q20, level, tx, ty] = key.split(':');
    const { buffer } = await this.fetch(`/api/cache/${this.dataset.entries.transcripts}/density/${level}/${tx}/${ty}?genes=${genes}&q20=${q20}`, signal);
    const d = readDensity(buffer);
    const gl = this.gl;
    const textures = new Map();
    for (const gene of d.genes) textures.set(gene, floatTexture(gl, d.bins, d.bins, densityGrid(d, gene)));
    return {
      value: { textures, dispose: () => { for (const t of textures.values()) gl.deleteTexture(t); } },
      bytes: textures.size * d.bins * d.bins * 4 + 64,
    };
  }

  async loadShapes(key, signal) {
    // key: geo:<set>:<level>:<tx>:<ty>
    const [, set, level, tx, ty] = key.split(':');
    const base = this.dataset.polygonsBase ?? `/api/cache/${this.dataset.entries.geometry}/polygons`;
    const { buffer, headers } = await this.fetch(`${base}/${set}/${level}/${tx}/${ty}`, signal);
    const tile = readPolygonTile(buffer, Number(headers.get('X-Polygons')), Number(headers.get('X-Vertices')));
    const gl = this.gl;
    const lines = outlineSegments(tile);
    const prog = this.programs.shape;
    const made = [];
    const upload = (geometry) => {
      const pos = arrayBuffer(gl, geometry.positions);
      const cells = new Float32Array(geometry.polygon.length);
      geometry.polygon.forEach((p, i) => {
        const row = tile.cells[p];
        cells[i] = row === 0xffffffff ? -1 : row;
      });
      const cell = arrayBuffer(gl, cells);
      const vao = vertexArray(gl, [{ location: prog.a.a_pos, buffer: pos, size: 2 }, { location: prog.a.a_cell, buffer: cell, size: 1 }]);
      made.push(() => { gl.deleteVertexArray(vao); gl.deleteBuffer(pos); gl.deleteBuffer(cell); });
      return { vao, count: geometry.count };
    };
    const value = {
      tile,
      lines: upload(lines),
      fills: null,
      // Fills are triangulated the first time they are drawn.
      fill() {
        if (!this.fills) this.fills = upload(fillTriangles(tile));
        return this.fills;
      },
      dispose: () => { for (const f of made) f(); },
    };
    return { value, bytes: buffer.byteLength * 3 + 64 };
  }

  // --- Drawing ---------------------------------------------------------------------------------

  // What the current view draws: the levels and modes, for the page and the tests.
  plan() {
    const d = this.dataset;
    if (!d) return null;
    const [w, h] = this.size();
    const cam = this.camera;
    const rect = visibleRect(cam, w, h);
    const grid = d.grid;
    const ratio = this.ratio || 1;
    const umPerCss = ratio / cam.scale;
    const level = this.forced?.level ?? imageLevel(grid, cam);
    const t = this.layers?.transcripts;
    let transcripts = null;
    if (t?.visible && d.transcripts && (t.all || t.genes.length)) {
      const genes = t.all ? 'all' : t.genes.join(',');
      const r0 = gridTileRange(grid, 0, rect);
      const tiles = (r0.tx1 - r0.tx0 + 1) * (r0.ty1 - r0.ty0 + 1);
      if (tiles <= (t.all ? POINT_TILES.all : POINT_TILES.genes)) transcripts = { mode: 'points', genes, range: r0 };
      else {
        const dl = densityLevel(grid, d.transcripts.densityBins, d.transcripts.densityLevels, umPerCss);
        const densityGenes = t.all ? String(d.transcripts.genes.length) : genes;
        if (dl !== null) transcripts = { mode: 'density', genes: densityGenes, level: dl, range: gridTileRange(grid, dl, rect) };
      }
    }
    // Outlines are drawn while a cell spans ten pixels or so (denser, they only grey the image);
    // fills at any zoom.
    const shapesVisible = (this.layers?.cells?.outlines || this.layers?.cells?.nuclei) && umPerCss <= 1.2;
    const fillVisible = Boolean(this.layers?.cells?.fill && this.cellColors);
    // QC maps use bins about six pixels wide: a ratio over fewer transcripts is noise.
    let qc = null;
    if (this.qcMap && d.transcripts?.densityLevels.length) {
      const ql = densityLevel(grid, d.transcripts.densityBins, d.transcripts.densityLevels, umPerCss, 6);
      qc = { level: ql, range: gridTileRange(grid, ql, rect) };
    }
    return { rect, level, umPerCss, transcripts, shapesVisible, fillVisible, qc };
  }

  draw() {
    if (this.lost || !this.dataset || !this.layers) return;
    const started = performance.now();
    const gl = this.gl;
    const [w, h] = this.size();
    gl.viewport(0, 0, w, h);
    gl.clearColor(0, 0, 0, this.transparent ? 0 : 1);
    gl.clearStencil(0);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.STENCIL_BUFFER_BIT);
    const plan = this.plan();
    const view = viewMatrix(this.camera, w, h);
    for (const c of Object.values(this.caches)) c.begin();
    this.drawImage(plan, view);
    if (plan.qc) this.drawQCMap(plan, view);
    if (plan.fillVisible) this.drawShapes(plan, view, 'fill');
    if (this.highlight) this.drawShapes(plan, view, 'highlight');
    if (plan.transcripts?.mode === 'density') this.drawDensity(plan, view);
    if (plan.shapesVisible) this.drawShapes(plan, view, 'lines');
    if (plan.transcripts?.mode === 'points') this.drawPoints(plan, view);
    this.drawSelection(view);
    for (const c of Object.values(this.caches)) c.end();
    const now = performance.now();
    this.lastFrame = { ms: now - started, at: now };
    this.onChange?.(plan);
  }

  // Draws tiles of levels finest first, each pixel once: a stencil marks what is drawn. tiles is
  // [{ level, list: [{ value, ... }] }] in that order; drawTile(item) draws one.
  stencilled(levels, drawTile) {
    const gl = this.gl;
    gl.enable(gl.STENCIL_TEST);
    gl.clear(gl.STENCIL_BUFFER_BIT);
    gl.stencilFunc(gl.EQUAL, 0, 0xff);
    gl.stencilOp(gl.KEEP, gl.KEEP, gl.INCR);
    for (const { list } of levels) for (const item of list) drawTile(item);
    gl.disable(gl.STENCIL_TEST);
  }

  drawImage(plan, view) {
    const d = this.dataset;
    const gl = this.gl;
    const prog = this.programs.image;
    gl.useProgram(prog.program);
    gl.uniformMatrix3fv(prog.u.u_view, false, view);
    gl.bindVertexArray(this.quadVAO.image);
    gl.uniform1i(prog.u.u_tex, 0);
    gl.activeTexture(gl.TEXTURE0);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE);
    const raw = this.forced?.raw ? 1 : 0;
    gl.uniform1i(prog.u.u_raw, raw);
    if (raw) gl.disable(gl.BLEND);
    this.layers.channels.forEach((ch, c) => {
      if (!ch.visible || this.forced && this.forced.channel !== c) return;
      const info = d.channels[c];
      const top = info.levels.length - 1;
      const level = Math.min(plan.level, top);
      const levels = [];
      for (let l = level; l <= top; l += 1) {
        const lv = info.levels[l];
        const range = tileRange(d.grid, l, plan.rect, lv.across, lv.down);
        const list = [];
        if (range) {
          const side = tileUm(d.grid, l);
          for (let ty = range.ty0; ty <= range.ty1; ty += 1) {
            for (let tx = range.tx0; tx <= range.tx1; tx += 1) {
              const key = `img:${c}:${l}:${tx}:${ty}`;
              // The level drawn is asked for; coarser ones stand in where they are loaded (the
              // coarsest is always asked for, so something shows at once).
              const tile = l === level || l === top ? this.caches.image.want(key, priority(plan.rect, side, tx, ty) + (l === level ? 0 : -1000)) : this.caches.image.peek(key);
              if (tile) list.push({ tile, tx, ty, l });
            }
          }
        }
        levels.push({ level: l, list });
      }
      gl.uniform3fv(prog.u.u_color, hexColor(ch.color));
      gl.uniform2f(prog.u.u_window, ch.window[0], ch.window[1]);
      gl.uniform1f(prog.u.u_gamma, ch.gamma);
      const pixel = d.grid.pixelSize;
      this.stencilled(levels, ({ tile, tx, ty, l }) => {
        const scale = pixel * 2 ** l;
        const side = d.grid.tile * scale;
        gl.uniform2f(prog.u.u_origin, tx * side, ty * side);
        gl.uniform2f(prog.u.u_size, tile.width * scale, tile.height * scale);
        gl.uniform2f(prog.u.u_texels, tile.width, tile.height);
        gl.uniform2i(prog.u.u_dims, tile.width, tile.height);
        gl.bindTexture(gl.TEXTURE_2D, tile.texture);
        gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      });
    });
    gl.disable(gl.BLEND);
    gl.bindVertexArray(null);
  }

  drawDensity(plan, view) {
    const d = this.dataset;
    const t = this.layers.transcripts;
    const gl = this.gl;
    const prog = this.programs.density;
    const { genes, level, range } = plan.transcripts;
    const levels = d.transcripts.densityLevels;
    const top = levels[levels.length - 1];
    gl.useProgram(prog.program);
    gl.uniformMatrix3fv(prog.u.u_view, false, view);
    gl.bindVertexArray(this.quadVAO.density);
    gl.uniform1i(prog.u.u_tex, 0);
    gl.activeTexture(gl.TEXTURE0);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE);
    gl.uniform1f(prog.u.u_gain, t.gain ?? 1);
    // The tiles of the level drawn, and of coarser density levels standing in.
    const tilesOf = (l) => {
      const r = l === level ? range : gridTileRange(d.grid, l, plan.rect);
      const side = tileUm(d.grid, l);
      const list = [];
      for (let ty = r.ty0; ty <= r.ty1; ty += 1) {
        for (let tx = r.tx0; tx <= r.tx1; tx += 1) {
          const key = `den:${genes}:1:${l}:${tx}:${ty}`;
          const tile = l === level ? this.caches.density.want(key, priority(plan.rect, side, tx, ty)) : this.caches.density.peek(key);
          if (tile) list.push({ tile, tx, ty, l });
        }
      }
      return { level: l, list };
    };
    const tiers = levels.filter((l) => l >= level).map(tilesOf);
    const geneList = genes.split(',').map(Number);
    geneList.forEach((gene, slot) => {
      const color = t.all ? (t.allColor ?? '#ffd84d') : t.colors[slot];
      gl.uniform3fv(prog.u.u_color, hexColor(color));
      this.stencilled(tiers, ({ tile, tx, ty, l }) => {
        const texture = tile.textures.get(gene);
        if (!texture) return;
        const side = tileUm(d.grid, l);
        // The color scale: the coarsest level's 99th percentile, a quarter of it per level finer.
        const scale = Math.max(1, d.transcripts.scale[gene] / 4 ** (top - l));
        gl.uniform1f(prog.u.u_scale, scale);
        gl.uniform2f(prog.u.u_origin, tx * side, ty * side);
        gl.uniform2f(prog.u.u_size, side, side);
        gl.uniform2f(prog.u.u_texels, 128, 128);
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      });
    });
    gl.disable(gl.BLEND);
    gl.bindVertexArray(null);
  }

  // The QC map: its bins at the plan's level (coarser loaded levels standing in), through the
  // map's colormap.
  drawQCMap(plan, view) {
    const d = this.dataset;
    const m = this.qcMap;
    const gl = this.gl;
    const prog = this.programs.qcmap;
    const levels = d.transcripts.densityLevels;
    const level = plan.qc.level;
    const genes = m.ratio ? `${m.num},${m.den}` : String(m.num);
    gl.useProgram(prog.program);
    gl.uniformMatrix3fv(prog.u.u_view, false, view);
    gl.bindVertexArray(this.quadVAO.qcmap);
    gl.uniform1i(prog.u.u_num, 0);
    gl.uniform1i(prog.u.u_den, 1);
    gl.uniform1i(prog.u.u_lut, 2);
    gl.activeTexture(gl.TEXTURE2);
    gl.bindTexture(gl.TEXTURE_2D, m.lutTexture);
    gl.uniform1i(prog.u.u_ratio, m.ratio ? 1 : 0);
    gl.uniform1i(prog.u.u_complement, m.complement ? 1 : 0);
    gl.uniform1f(prog.u.u_minDen, m.minDen ?? 1);
    gl.uniform2f(prog.u.u_range, m.range[0], m.range[1]);
    gl.uniform1f(prog.u.u_opacity, m.opacity ?? 0.85);
    const top = levels[levels.length - 1];
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    // A ratio's two grids may count all transcripts or Q20 ones: one tile request for each.
    const keys = (l, tx, ty) => {
      const num = `den:${genes}:${m.q20Num ? 1 : 0}:${l}:${tx}:${ty}`;
      const den = m.ratio ? `den:${genes}:${m.q20Den ? 1 : 0}:${l}:${tx}:${ty}` : num;
      return [num, den];
    };
    const tiers = levels.filter((l) => l >= level).map((l) => {
      const r = l === level ? plan.qc.range : gridTileRange(d.grid, l, plan.rect);
      const side = tileUm(d.grid, l);
      const list = [];
      for (let ty = r.ty0; ty <= r.ty1; ty += 1) {
        for (let tx = r.tx0; tx <= r.tx1; tx += 1) {
          const [kn, kd] = keys(l, tx, ty);
          const get = (k) => (l === level ? this.caches.density.want(k, priority(plan.rect, side, tx, ty)) : this.caches.density.peek(k));
          const num = get(kn);
          const den = kd === kn ? num : get(kd);
          if (num && den) list.push({ num, den, tx, ty, l });
        }
      }
      return { level: l, list };
    });
    this.stencilled(tiers, ({ num, den, tx, ty, l }) => {
      const tn = num.textures.get(m.num);
      const td = m.ratio ? den.textures.get(m.den) : tn;
      if (!tn || !td) return;
      const side = tileUm(d.grid, l);
      gl.uniform1f(prog.u.u_scale, Math.max(1, (m.scale ?? 1) / 4 ** (top - l)));
      gl.uniform2f(prog.u.u_origin, tx * side, ty * side);
      gl.uniform2f(prog.u.u_size, side, side);
      gl.uniform2f(prog.u.u_texels, 128, 128);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, tn);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, td);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    });
    gl.activeTexture(gl.TEXTURE0);
    gl.disable(gl.BLEND);
    gl.bindVertexArray(null);
  }

  drawPoints(plan, view) {
    const t = this.layers.transcripts;
    const gl = this.gl;
    const prog = this.programs.points;
    gl.useProgram(prog.program);
    gl.uniformMatrix3fv(prog.u.u_view, false, view);
    const ratio = this.ratio || 1;
    // Points grow with zoom from their set size, up to three times it.
    const grow = Math.min(3, Math.max(1, 0.6 / plan.umPerCss));
    gl.uniform1f(prog.u.u_size, (t.pointSize ?? 3) * ratio * grow);
    gl.uniform1f(prog.u.u_selected, this.selected ? this.selected.row : -2);
    const colors = new Float32Array(48);
    const list = t.all ? [t.allColor ?? '#ffd84d'] : t.colors;
    list.slice(0, 16).forEach((hex, i) => colors.set(hexColor(hex), 3 * i));
    gl.uniform3fv(prog.u.u_colors, colors);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    const { range, genes } = plan.transcripts;
    const side = tileUm(this.dataset.grid, 0);
    const minQ = t.minQ ?? 20;
    for (let ty = range.ty0; ty <= range.ty1; ty += 1) {
      for (let tx = range.tx0; tx <= range.tx1; tx += 1) {
        const tile = this.caches.points.want(`pts:${genes}:${minQ}:${tx}:${ty}`, priority(plan.rect, side, tx, ty));
        if (!tile || !tile.count) continue;
        gl.bindVertexArray(tile.vao);
        gl.drawArrays(gl.POINTS, 0, tile.count);
      }
    }
    gl.bindVertexArray(null);
    gl.disable(gl.BLEND);
  }

  // The geometry tiles in view at the image level, widened by the largest polygon.
  shapeTiles(plan, set) {
    const d = this.dataset;
    const info = d.geometry.find((s) => s.name === set);
    if (!info) return [];
    const level = Math.min(plan.level, d.grid.levels - 1);
    const reach = info.maxExtent;
    const r = { x0: plan.rect.x0 - reach, y0: plan.rect.y0 - reach, x1: plan.rect.x1 + reach, y1: plan.rect.y1 + reach };
    const range = gridTileRange(d.grid, level, r);
    const side = tileUm(d.grid, level);
    const out = [];
    for (let ty = range.ty0; ty <= range.ty1; ty += 1) {
      for (let tx = range.tx0; tx <= range.tx1; tx += 1) {
        const tile = this.caches.shapes.want(`geo:${set}:${level}:${tx}:${ty}`, priority(plan.rect, side, tx, ty));
        if (tile) out.push(tile);
      }
    }
    return out;
  }

  drawShapes(plan, view, kind) {
    const c = this.layers.cells;
    const gl = this.gl;
    const prog = this.programs.shape;
    gl.useProgram(prog.program);
    gl.uniformMatrix3fv(prog.u.u_view, false, view);
    gl.enable(gl.BLEND);
    // On a transparent canvas the drawn alpha must add up as colour does.
    gl.blendFuncSeparate(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA, gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
    gl.uniform1i(prog.u.u_colors, 1);
    gl.activeTexture(gl.TEXTURE1);
    const texture = { fill: this.cellColors?.texture, highlight: this.highlight?.texture }[kind] ?? this.noColors;
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.activeTexture(gl.TEXTURE0);
    if (kind === 'fill' || kind === 'highlight') {
      gl.uniform1i(prog.u.u_fill, 1);
      gl.uniform1f(prog.u.u_count, this.cells.n);
      gl.uniform4f(prog.u.u_tint, 1, 1, 1, kind === 'fill' ? (c.opacity ?? 0.6) : 1);
      for (const tile of this.shapeTiles(plan, 'cells')) {
        const f = tile.fill();
        gl.bindVertexArray(f.vao);
        gl.drawArrays(gl.TRIANGLES, 0, f.count);
      }
    } else {
      gl.uniform1i(prog.u.u_fill, 0);
      for (const [set, on, color] of [['nuclei', c.nuclei, c.nucleusColor ?? '#6fa8ff'], ['cells', c.outlines, c.outlineColor ?? '#e8e8e8']]) {
        if (!on) continue;
        gl.uniform4f(prog.u.u_tint, ...hexColor(color), 0.75);
        for (const tile of this.shapeTiles(plan, set)) {
          gl.bindVertexArray(tile.lines.vao);
          gl.drawArrays(gl.LINES, 0, tile.lines.count);
        }
      }
    }
    gl.bindVertexArray(null);
    gl.disable(gl.BLEND);
  }

  // The selected cell: its full-resolution outline, two device pixels wide, and its nucleus's.
  drawSelection(view) {
    if (!this.selected) return;
    const gl = this.gl;
    const half = (1.25 * (this.ratio || 1)) / this.camera.scale;
    const triangles = [];
    const ring = (xy) => {
      const n = xy.length / 2 - (xy[0] === xy[xy.length - 2] && xy[1] === xy[xy.length - 1] ? 1 : 0);
      for (let i = 0; i < n; i += 1) {
        const ax = xy[2 * i], ay = xy[2 * i + 1];
        const bx = xy[2 * ((i + 1) % n)], by = xy[2 * ((i + 1) % n) + 1];
        const len = Math.hypot(bx - ax, by - ay) || 1;
        const nx = (-(by - ay) / len) * half, ny = ((bx - ax) / len) * half;
        const ex = ((bx - ax) / len) * half, ey = ((by - ay) / len) * half;
        triangles.push(ax - ex + nx, ay - ey + ny, ax - ex - nx, ay - ey - ny, bx + ex + nx, by + ey + ny, bx + ex + nx, by + ey + ny, ax - ex - nx, ay - ey - ny, bx + ex - nx, by + ey - ny);
      }
    };
    const draws = [];
    for (const [rings, color] of [[this.selected.boundaries, '#ffffff'], [this.selected.nuclei, '#7fb2ff']]) {
      const start = triangles.length / 2;
      for (const r of rings) ring(r);
      draws.push({ start, count: triangles.length / 2 - start, color });
    }
    if (!triangles.length) return;
    const prog = this.programs.shape;
    if (this.selectionBuffer) gl.deleteBuffer(this.selectionBuffer.buffer);
    const buffer = arrayBuffer(gl, new Float32Array(triangles), gl.STREAM_DRAW);
    const vao = vertexArray(gl, [{ location: prog.a.a_pos, buffer, size: 2 }]);
    this.selectionBuffer = { buffer };
    gl.useProgram(prog.program);
    gl.uniformMatrix3fv(prog.u.u_view, false, view);
    gl.uniform1i(prog.u.u_fill, 0);
    gl.uniform1i(prog.u.u_colors, 1);
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D, this.noColors);
    gl.activeTexture(gl.TEXTURE0);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.bindVertexArray(vao);
    gl.vertexAttrib1f(prog.a.a_cell, 0);
    for (const d of draws) {
      if (!d.count) continue;
      gl.uniform4f(prog.u.u_tint, ...hexColor(d.color), 1);
      gl.drawArrays(gl.TRIANGLES, d.start, d.count);
    }
    gl.bindVertexArray(null);
    gl.deleteVertexArray(vao);
    gl.disable(gl.BLEND);
  }

  // --- Questions --------------------------------------------------------------------------------

  // The cell under a canvas position (device pixels): the row of the loaded cell (or nucleus)
  // polygon containing it, else -1.
  pick(sx, sy) {
    if (!this.dataset) return -1;
    const [w, h] = this.size();
    const [x, y] = screenToWorld(this.camera, w, h, sx, sy);
    const plan = this.plan();
    const level = Math.min(plan.level, this.dataset.grid.levels - 1);
    for (const set of ['cells', 'nuclei']) {
      const info = this.dataset.geometry.find((s) => s.name === set);
      if (!info) continue;
      const r = { x0: x - info.maxExtent, y0: y - info.maxExtent, x1: x + info.maxExtent, y1: y + info.maxExtent };
      const range = gridTileRange(this.dataset.grid, level, r);
      for (let ty = range.ty0; ty <= range.ty1; ty += 1) {
        for (let tx = range.tx0; tx <= range.tx1; tx += 1) {
          const tile = this.caches.shapes.peek(`geo:${set}:${level}:${tx}:${ty}`)?.tile;
          if (!tile) continue;
          for (let i = 0; i < tile.cells.length; i += 1) {
            if (tile.cells[i] !== 0xffffffff && pointInRing(tile.xy, tile.starts[i], tile.starts[i + 1], x, y)) return tile.cells[i];
          }
        }
      }
    }
    return -1;
  }

  // Resolves once every tile the view needs is loaded and drawn.
  async settled() {
    for (;;) {
      this.draw();
      await Promise.all(Object.values(this.caches).map((c) => c.whenIdle()));
      this.draw();
      if (Object.values(this.caches).every((c) => c.idle())) return;
    }
  }

  pending() {
    return Object.values(this.caches).reduce((n, c) => n + c.pending(), 0);
  }

  // The points loaded for the current view inside a rectangle: [{ gene, x, y, cell }], for the
  // validation harness.
  loadedPoints(rect) {
    const plan = this.plan();
    if (plan?.transcripts?.mode !== 'points') return [];
    const out = [];
    const { range, genes } = plan.transcripts;
    const minQ = this.layers.transcripts.minQ ?? 20;
    for (let ty = range.ty0; ty <= range.ty1; ty += 1) {
      for (let tx = range.tx0; tx <= range.tx1; tx += 1) {
        const p = this.caches.points.peek(`pts:${genes}:${minQ}:${tx}:${ty}`)?.points;
        if (!p) continue;
        for (let k = 0; k < p.genes.length; k += 1) {
          for (let i = p.starts[k]; i < p.starts[k + 1]; i += 1) {
            if (p.x[i] >= rect.x0 && p.x[i] < rect.x1 && p.y[i] >= rect.y0 && p.y[i] < rect.y1) out.push({ gene: p.genes[k], x: p.x[i], y: p.y[i], cell: p.cell[i] });
          }
        }
      }
    }
    return out;
  }

  // Draws one channel's raw samples at a level, one device pixel per sample, the sample at
  // (x0, y0) of the level in the canvas's top-left pixel, and reads back a w × h block: the low
  // byte of each sample in red and the high byte in green, before any window or color. For the
  // validation harness (validation/viewer.mjs).
  async rawCapture({ channel, level, x0, y0, w, h }) {
    const d = this.dataset;
    const texel = d.grid.pixelSize * 2 ** level;
    const [cw, ch] = this.size();
    if (w > cw || h > ch) throw new Error(`a ${w}×${h} capture in a ${cw}×${ch} canvas`);
    const saved = { camera: this.camera, forced: this.forced, layers: this.layers };
    this.camera = { cx: x0 * texel + (cw / 2) * texel, cy: y0 * texel + (ch / 2) * texel, scale: 1 / texel };
    this.forced = { level, channel, raw: true };
    this.layers = { ...saved.layers, transcripts: { ...saved.layers.transcripts, visible: false }, cells: { ...saved.layers.cells, outlines: false, nuclei: false, fill: null }, channels: saved.layers.channels.map((c, i) => ({ ...c, visible: i === channel })) };
    const selected = this.selected;
    this.selected = null;
    try {
      await this.settled();
      this.draw();
      const gl = this.gl;
      const pixels = new Uint8Array(w * h * 4);
      gl.readPixels(0, ch - h, w, h, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
      // Rows come bottom up; the samples top down.
      const out = new Uint16Array(w * h);
      for (let row = 0; row < h; row += 1) {
        const src = (h - 1 - row) * w * 4;
        for (let x = 0; x < w; x += 1) out[row * w + x] = pixels[src + 4 * x] | (pixels[src + 4 * x + 1] << 8);
      }
      return out;
    } finally {
      this.camera = saved.camera;
      this.forced = saved.forced;
      this.layers = saved.layers;
      this.selected = selected;
      this.request();
    }
  }

  destroy() {
    for (const c of Object.values(this.caches)) c.clear();
    if (this.cellColors) this.gl.deleteTexture(this.cellColors.texture);
    if (this.highlight) this.gl.deleteTexture(this.highlight.texture);
    if (this.qcMap?.lutTexture) this.gl.deleteTexture(this.qcMap.lutTexture);
    this.gl.deleteTexture(this.noColors);
    this.gl.getExtension('WEBGL_lose_context')?.loseContext();
  }
}
