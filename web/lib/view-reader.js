// A saved view in a result package (wave 8, slice 8): its tissue drawn in advance as tiles, a
// pyramid of JPEG images 512 pixels on a side (to look at: the figures, in SVG, are what the
// certificate holds), and a page of its own that shows them in any browser,
// without SituCraft: dragged to move, scrolled (or + and −) to zoom, with a scale bar and the view's
// legend. Nothing in it is fetched but its tiles, beside it. The reader's script is written into
// the page as it is here (Function.prototype.toString): it holds everything it uses.

export const TILE = 512;
// The most tiles a view's pyramid holds, and the most levels finer than its own.
export const MAX_TILES = 64;
export const DEEPEST = 2;
// A view's own region: what the Viewer showed of it, 1000 × 625 CSS pixels at its zoom.
export const VIEW_CSS = { width: 1000, height: 625 };

const escapeHTML = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// The region a view's tiles cover (µm): its own, and every figure panel's showing it (panels: [{
// region: { cx, cy, width }, aspect (height / width) }]).
export function viewExtent(view, panels = []) {
  const um = view.camera.umPerCss;
  const own = { cx: view.camera.cx, cy: view.camera.cy, width: VIEW_CSS.width * um, height: VIEW_CSS.height * um };
  const boxes = [own, ...panels.map((p) => ({ cx: p.region.cx, cy: p.region.cy, width: p.region.width, height: p.region.width * (p.aspect ?? 1) }))];
  const r = (x) => Math.round(x * 1000) / 1000;
  return {
    x0: r(Math.min(...boxes.map((b) => b.cx - b.width / 2))),
    y0: r(Math.min(...boxes.map((b) => b.cy - b.height / 2))),
    x1: r(Math.max(...boxes.map((b) => b.cx + b.width / 2))),
    y1: r(Math.max(...boxes.map((b) => b.cy + b.height / 2))),
  };
}

// The pyramid of a region: its levels from the view's own scale (umPerPx: µm a pixel, at most a
// region 2048 pixels across) to the image's full resolution (fullRes: µm a pixel) or DEEPEST
// levels finer, each halving µm a pixel, while the tiles number at most maxTiles. Answers [{ z,
// umPerPx, width, height (pixels), cols, rows }], the coarsest first.
export function tilePlan(extent, { umPerPx, fullRes, tile = TILE, maxTiles = MAX_TILES, deepest = DEEPEST }) {
  const w = extent.x1 - extent.x0;
  const h = extent.y1 - extent.y0;
  const levels = [];
  let u = Math.max(umPerPx, Math.max(w, h) / 2048);
  let total = 0;
  for (let z = 0; z <= deepest; z += 1) {
    const width = Math.ceil(w / u);
    const height = Math.ceil(h / u);
    const cols = Math.ceil(width / tile);
    const rows = Math.ceil(height / tile);
    if (z > 0 && (total + cols * rows > maxTiles || u < fullRes * 0.75)) break;
    levels.push({ z, umPerPx: u, width, height, cols, rows });
    total += cols * rows;
    u /= 2;
  }
  return levels;
}

// A tile's region (µm): { cx, cy, width } (square), and its file, relative to the reader.
export function tileRegion(extent, level, x, y, tile = TILE) {
  const span = level.umPerPx * tile;
  return { cx: extent.x0 + (x + 0.5) * span, cy: extent.y0 + (y + 0.5) * span, width: span };
}
export const tilePath = (dir, level, x, y) => `${dir}/${level.z}/${x}-${y}.jpg`;

// Every tile of a pyramid: [{ level, x, y, region, path }].
export function tilesOf(extent, levels, dir, tile = TILE) {
  const out = [];
  for (const level of levels) {
    for (let y = 0; y < level.rows; y += 1) {
      for (let x = 0; x < level.cols; x += 1) out.push({ level, x, y, region: tileRegion(extent, level, x, y, tile), path: tilePath(dir, level, x, y) });
    }
  }
  return out;
}

const STYLE = `
:root { color-scheme: light dark; --bg: #ffffff; --fg: #171b26; --muted: #5b6478; --line: #d9dde6; --panel: #f5f6f9; --accent: #3d5bd9; }
@media (prefers-color-scheme: dark) { :root { --bg: #11141b; --fg: #e8eaf0; --muted: #a2a9b8; --line: #2c3240; --panel: #1a1e28; --accent: #8ea2ff; } }
* { box-sizing: border-box; }
html, body { height: 100%; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; display: flex; flex-direction: column; }
header { padding: 10px 16px; border-bottom: 1px solid var(--line); }
header a { color: var(--accent); font-size: 13px; }
h1 { font-size: 18px; margin: 2px 0 0; }
.meta { color: var(--muted); font-size: 12.5px; }
.body { flex: 1; display: flex; min-height: 0; }
.frame { position: relative; flex: 1; overflow: hidden; background: #000000; }
#stage { position: absolute; inset: 0; z-index: 0; overflow: hidden; cursor: grab; touch-action: none; outline: none; }
#stage:focus-visible { box-shadow: inset 0 0 0 2px var(--accent); }
#stage.dragging { cursor: grabbing; }
#tiles img { position: absolute; max-width: none; user-select: none; -webkit-user-drag: none; pointer-events: none; }
#scalebar { position: absolute; z-index: 1; right: 14px; bottom: 12px; color: #ffffff; font-size: 12px; text-align: center; text-shadow: 0 0 3px #000000; pointer-events: none; }
#scalebar .bar { height: 3px; background: #ffffff; margin-top: 2px; box-shadow: 0 0 3px #000000; }
.controls { position: absolute; z-index: 1; left: 12px; top: 12px; display: flex; flex-direction: column; gap: 4px; }
.controls button { width: 34px; height: 30px; border-radius: 6px; border: 1px solid #5b6478; background: rgba(17, 20, 27, 0.85); color: #ffffff; font: 15px/1 inherit; cursor: pointer; }
.controls button:focus-visible { outline: 2px solid #8ea2ff; }
aside { width: 240px; padding: 12px 14px; border-left: 1px solid var(--line); overflow: auto; font-size: 13px; background: var(--panel); }
aside h2 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--muted); margin: 12px 0 4px; }
aside h2:first-child { margin-top: 0; }
aside ul { list-style: none; margin: 0; padding: 0; }
aside li { display: flex; align-items: center; gap: 8px; margin: 3px 0; }
.swatch { width: 12px; height: 12px; border-radius: 2px; flex: none; }
.swatch.dot { border-radius: 50%; width: 8px; height: 8px; margin: 0 2px; }
.swatch.ring { background: transparent; border: 2px solid; }
.ramp { height: 10px; border-radius: 2px; margin: 4px 0 2px; }
.ends { display: flex; justify-content: space-between; color: var(--muted); font-size: 12px; }
@media (max-width: 700px) { .body { flex-direction: column; } aside { width: auto; border-left: 0; border-top: 1px solid var(--line); max-height: 35vh; } }
`;

function legendHTML(items, regions) {
  const groups = new Map();
  for (const it of items) {
    if (!groups.has(it.group)) groups.set(it.group, []);
    groups.get(it.group).push(it);
  }
  const parts = [];
  for (const [group, list] of groups) {
    parts.push(`<h2>${escapeHTML(group)}</h2><ul>`);
    for (const it of list) {
      if (it.kind === 'scale') {
        parts.push(`<li style="display:block">${escapeHTML(it.name)}<div class="ramp" style="background: linear-gradient(to right, ${it.colors.map(escapeHTML).join(', ')})"></div><div class="ends"><span>${escapeHTML(it.min)}</span><span>${escapeHTML(it.max)}</span></div></li>`);
      } else parts.push(`<li><span class="swatch${it.kind === 'dot' ? ' dot' : ''}" style="background:${escapeHTML(it.color)}"></span>${escapeHTML(it.name)}</li>`);
    }
    parts.push('</ul>');
  }
  if (regions.length) parts.push(`<h2>Regions</h2><ul>${regions.map((r) => `<li><span class="swatch ring" style="border-color:${escapeHTML(r.color)}"></span>${escapeHTML(r.name)}</li>`).join('')}</ul>`);
  return parts.join('');
}

// The reader's page. data: { title (the view's name), dataset (its name), project, software,
// extent, levels (tilePlan's), dir (its tiles' folder, relative to the page), tile, legend (items,
// as a figure's tissue panel lists them), regions: [{ name, color }], back (the report, relative) }.
export function readerHTML(data) {
  const across = data.extent.x1 - data.extent.x0;
  const shown = { extent: data.extent, levels: data.levels, dir: data.dir, tile: data.tile ?? TILE };
  return [
    '<!doctype html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n<meta name="viewport" content="width=device-width, initial-scale=1">',
    `<meta name="generator" content="SituCraft ${escapeHTML(data.software ?? '')}">`,
    '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; img-src \'self\' file: data:; style-src \'unsafe-inline\'; script-src \'unsafe-inline\'">',
    `<title>${escapeHTML(data.title)}</title>\n<style>${STYLE}</style>\n</head>\n<body>`,
    `<header>${data.back ? `<a href="${escapeHTML(data.back)}">← The report</a>` : ''}<h1>${escapeHTML(data.title)}</h1><div class="meta">A saved view of ${escapeHTML(data.dataset)}, ${escapeHTML(across >= 1000 ? `${(across / 1000).toFixed(2)} mm` : `${Math.round(across)} µm`)} across, from the project ${escapeHTML(data.project)}; drawn by SituCraft ${escapeHTML(data.software ?? '')} in ${data.levels.length} level${data.levels.length === 1 ? '' : 's'}, to ${escapeHTML(data.levels.at(-1).umPerPx.toFixed(3))} µm a pixel. Drag to move; scroll, or + and −, to zoom.</div></header>`,
    `<div class="body"><div class="frame"><div id="stage" tabindex="0" role="img" aria-label="${escapeHTML(`The tissue of the view ${data.title}: drag or use the arrow keys to move, scroll or + and − to zoom, 0 to fit`)}"><div id="tiles"></div></div><div id="scalebar"><span></span><div class="bar"></div></div><div class="controls"><button type="button" id="zoom-in" aria-label="Zoom in">+</button><button type="button" id="zoom-out" aria-label="Zoom out">−</button><button type="button" id="fit" aria-label="Fit the view">⤢</button></div></div>`,
    `<aside aria-label="Legend">${legendHTML(data.legend ?? [], data.regions ?? [])}</aside></div>`,
    `<script>\n(${runReader.toString()})(document, ${JSON.stringify(shown).replace(/</g, '\\u003c')});\n</script>`,
    '</body>\n</html>\n',
  ].join('\n');
}

// The reader: the tiles of the coarsest level always, and of the level whose pixels are no larger
// than the screen's over it, of the part shown. Sets data-ready on the page when the first tiles
// shown have loaded, and data-level to the finest level shown.
export function runReader(doc, data) {
  const win = doc.defaultView;
  const stage = doc.getElementById('stage');
  const layer = doc.getElementById('tiles');
  const bar = doc.querySelector('#scalebar .bar');
  const label = doc.querySelector('#scalebar span');
  const ex = data.extent;
  const W = ex.x1 - ex.x0;
  const H = ex.y1 - ex.y0;
  const levels = data.levels;
  const finest = levels[levels.length - 1].umPerPx;
  let cx = (ex.x0 + ex.x1) / 2;
  let cy = (ex.y0 + ex.y1) / 2;
  let um = 1;
  const size = () => ({ w: stage.clientWidth || 1, h: stage.clientHeight || 1 });
  const fitUm = () => Math.max(W / size().w, H / size().h) * 1.02;
  const clamp = () => {
    um = Math.min(fitUm() * 2, Math.max(finest / 4, um));
    cx = Math.min(ex.x1, Math.max(ex.x0, cx));
    cy = Math.min(ex.y1, Math.max(ex.y0, cy));
  };
  const images = new Map();
  let pending = 0;
  let ready = false;
  const settle = () => {
    pending -= 1;
    if (!ready && pending === 0) {
      ready = true;
      doc.documentElement.dataset.ready = 'true';
    }
  };
  const tile = (lv, x, y) => {
    const key = `${lv.z}/${x}-${y}`;
    let img = images.get(key);
    if (!img) {
      img = doc.createElement('img');
      img.alt = '';
      img.draggable = false;
      pending += 1;
      img.addEventListener('load', settle, { once: true });
      img.addEventListener('error', () => {
        doc.documentElement.dataset.missing = String(Number(doc.documentElement.dataset.missing ?? 0) + 1);
        settle();
      }, { once: true });
      img.src = `${data.dir}/${key}.jpg`;
      layer.append(img);
      images.set(key, img);
    }
    return img;
  };
  // The scale bar's length: 1, 2 or 5 times a power of ten, at most target µm.
  const nice = (target) => {
    let p = 1;
    while (p * 10 <= target) p *= 10;
    while (p > target && p > 1e-3) p /= 10;
    return [5, 2, 1].map((m) => m * p).find((v) => v <= target) ?? p;
  };
  function draw() {
    clamp();
    const s = size();
    const dpr = win.devicePixelRatio || 1;
    const level = levels.find((lv) => lv.umPerPx <= (um / dpr) * 1.01) ?? levels[levels.length - 1];
    const shown = new Set();
    const left = cx - (s.w / 2) * um;
    const top = cy - (s.h / 2) * um;
    for (const lv of level === levels[0] ? [level] : [levels[0], level]) {
      const span = lv.umPerPx * data.tile;
      const x0 = Math.max(0, Math.floor((left - ex.x0) / span));
      const x1 = Math.min(lv.cols - 1, Math.floor((left + s.w * um - ex.x0) / span));
      const y0 = Math.max(0, Math.floor((top - ex.y0) / span));
      const y1 = Math.min(lv.rows - 1, Math.floor((top + s.h * um - ex.y0) / span));
      const at = (v, origin) => Math.round((v - origin) / um);
      for (let y = y0; y <= y1; y += 1) {
        for (let x = x0; x <= x1; x += 1) {
          const img = tile(lv, x, y);
          const l = at(ex.x0 + x * span, left);
          const t = at(ex.y0 + y * span, top);
          img.style.left = `${l}px`;
          img.style.top = `${t}px`;
          img.style.width = `${at(ex.x0 + (x + 1) * span, left) - l}px`;
          img.style.height = `${at(ex.y0 + (y + 1) * span, top) - t}px`;
          img.style.zIndex = String(lv.z);
          shown.add(img);
        }
      }
    }
    for (const img of images.values()) img.style.display = shown.has(img) ? '' : 'none';
    const length = nice(120 * um);
    bar.style.width = `${Math.round(length / um)}px`;
    label.textContent = length >= 1000 ? `${length / 1000} mm` : `${length} µm`;
    doc.documentElement.dataset.level = String(level.z);
    if (pending === 0 && !ready) {
      ready = true;
      doc.documentElement.dataset.ready = 'true';
    }
  }
  const zoom = (factor, mx = 0, my = 0) => {
    const ax = cx + mx * um;
    const ay = cy + my * um;
    um *= factor;
    clamp();
    cx = ax - mx * um;
    cy = ay - my * um;
    draw();
  };
  const fromCenter = (e) => {
    const r = stage.getBoundingClientRect();
    return [e.clientX - r.left - r.width / 2, e.clientY - r.top - r.height / 2];
  };
  let drag = null;
  stage.addEventListener('pointerdown', (e) => {
    drag = { x: e.clientX, y: e.clientY };
    stage.setPointerCapture(e.pointerId);
    stage.classList.add('dragging');
  });
  stage.addEventListener('pointermove', (e) => {
    if (!drag) return;
    cx -= (e.clientX - drag.x) * um;
    cy -= (e.clientY - drag.y) * um;
    drag = { x: e.clientX, y: e.clientY };
    draw();
  });
  const end = () => {
    drag = null;
    stage.classList.remove('dragging');
  };
  stage.addEventListener('pointerup', end);
  stage.addEventListener('pointercancel', end);
  stage.addEventListener('wheel', (e) => {
    e.preventDefault();
    const k = 1 + Math.min(1, Math.abs(e.deltaY) * 0.002);
    zoom(e.deltaY > 0 ? k : 1 / k, ...fromCenter(e));
  }, { passive: false });
  stage.addEventListener('keydown', (e) => {
    const step = 0.15 * Math.min(size().w, size().h) * um;
    const keys = { '+': () => zoom(1 / 1.5), '=': () => zoom(1 / 1.5), '-': () => zoom(1.5), 0: () => { um = fitUm(); cx = (ex.x0 + ex.x1) / 2; cy = (ex.y0 + ex.y1) / 2; draw(); }, ArrowLeft: () => { cx -= step; draw(); }, ArrowRight: () => { cx += step; draw(); }, ArrowUp: () => { cy -= step; draw(); }, ArrowDown: () => { cy += step; draw(); } };
    if (keys[e.key]) {
      e.preventDefault();
      keys[e.key]();
    }
  });
  doc.getElementById('zoom-in').addEventListener('click', () => zoom(1 / 1.5));
  doc.getElementById('zoom-out').addEventListener('click', () => zoom(1.5));
  doc.getElementById('fit').addEventListener('click', () => {
    um = fitUm();
    cx = (ex.x0 + ex.x1) / 2;
    cy = (ex.y0 + ex.y1) / 2;
    draw();
  });
  win.addEventListener('resize', draw);
  um = fitUm();
  draw();
  return { draw, zoom, state: () => ({ cx, cy, um }) };
}
