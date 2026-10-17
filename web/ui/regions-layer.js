// Regions on the Viewer's tissue (lib/regions.js): drawn over any layer with a rectangle, a polygon,
// freehand or a brush (painting a region, or erasing from the one chosen), named and classed, their
// cells counted (the segmentation drawn's, by centroid) and chosen, each change a version kept in the
// project; written and read as GeoJSON as QuPath writes and reads its annotations, in µm, in the
// Xenium image's pixels or in a post-Xenium image's (through the version placing it).
//
// The regions are an SVG layer over the canvas, placed from the renderer's camera after each frame.
// While a tool is chosen the layer takes the pointer: a drag draws, the wheel still zooms, and the
// space bar held pans.

import { h, icon, clear, downloadBlob, formatCount } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { prefs } from './storage.js';
import { worldToScreen, screenToWorld } from '../lib/viewport.js';
import { addRegion, area, bounds, brushGeometry, CLASSES, cellsIn, colorCSS, editRegion, fromGeoJSON, nextRegionName, polygonOf, rectangle, regionsOf, toGeoJSON, uniqueRegionName } from '../lib/regions.js';
import { placePoint, transformShape, unplacePoint } from '../lib/transforms.js';

const SVG = 'http://www.w3.org/2000/svg';
const svgEl = (tag, attrs = {}) => {
  const el = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
};

const TOOLS = [
  ['rectangle', 'Rectangle', 'rectangle', 'Drag a rectangle'],
  ['polygon', 'Polygon', 'polygon', 'Click its corners; double-click (or Enter) to close it, Backspace to take a corner back'],
  ['freehand', 'Freehand', 'lasso', 'Drag around it'],
  ['brush', 'Brush', 'edit', 'Paint it; with a region chosen, paint onto it (or erase from it)'],
];

// options: { app, dataset, canvas, renderer, zoomBy(factor, sx, sy) (device pixels), drawnCells() (the
// cells table drawn), ownCells (the dataset's), images() (the Viewer's image entries), pixelSize (the
// Xenium image's, µm) }.
export function mountRegions({ app, dataset, canvas, renderer, zoomBy, drawnCells, ownCells, images, pixelSize }) {
  const svg = svgEl('svg', { class: 'viewer-regions' });
  // The regions' shapes in µm, moved by one transform each frame (their strokes not scaled); their
  // labels, placed each frame; what is being drawn.
  const shapes = svgEl('g');
  const labels = svgEl('g');
  const sketch = svgEl('g', { class: 'sketch' });
  const catcher = svgEl('rect', { x: 0, y: 0, width: '100%', height: '100%', fill: 'transparent', class: 'catcher' });
  svg.append(shapes, labels, sketch, catcher);
  canvas.after(svg);
  const st = { tool: null, cls: prefs.get('regionClass', 'Tumor'), brush: 24, erase: false, into: true, selected: null, drawing: null, counts: new Map(), panning: null, space: false };
  const regions = () => regionsOf(app.store.project, dataset.id);
  const by = () => prefs.get('reviewer', '') || 'the user';

  // --- Placing ---
  const toCss = (x, y) => {
    const [w, hh] = renderer.size();
    const ratio = renderer.ratio || 1;
    const [sx, sy] = worldToScreen(renderer.camera, w, hh, x, y);
    return [sx / ratio, sy / ratio];
  };
  const toWorld = (event) => {
    const r = canvas.getBoundingClientRect();
    const ratio = renderer.ratio || 1;
    const [w, hh] = renderer.size();
    return screenToWorld(renderer.camera, w, hh, (event.clientX - r.left) * ratio, (event.clientY - r.top) * ratio);
  };
  const umPerCss = () => (renderer.ratio || 1) / renderer.camera.scale;
  const pathOf = (geometry) => geometry.map((poly) => poly.map((ring) => `M${ring.map(([x, y]) => toCss(x, y).map((v) => v.toFixed(1)).join(',')).join('L')}Z`).join('')).join('');

  // The shapes, made again only when the regions (or the one chosen) change.
  let drawnKey = null;
  let corners = [];
  const worldPath = (geometry) => geometry.map((poly) => poly.map((ring) => `M${ring.map(([x, y]) => `${x.toFixed(3)},${y.toFixed(3)}`).join('L')}Z`).join('')).join('');
  function place() {
    const list = regions();
    const key = `${st.selected}|${list.map((r) => `${r.id}:${r.version}`).join()}`;
    if (key !== drawnKey) {
      drawnKey = key;
      clear(shapes);
      clear(labels);
      corners = [];
      for (const r of list) {
        const chosen = r.id === st.selected;
        const color = colorCSS(r.color);
        shapes.append(svgEl('path', { d: worldPath(r.geometry), fill: color, 'fill-opacity': chosen ? 0.22 : 0.12, stroke: color, 'stroke-width': chosen ? 3 : 1.5, 'stroke-dasharray': chosen ? '6 3' : '', 'fill-rule': 'evenodd', 'vector-effect': 'non-scaling-stroke', 'data-region': r.id }));
        const label = svgEl('text', { fill: color, class: 'region-label' });
        label.textContent = r.name;
        labels.append(label);
        const b = bounds(r.geometry);
        corners.push([label, b.x0, b.y0]);
      }
    }
    // µm to CSS pixels: a scale and a shift.
    const [tx, ty] = toCss(0, 0);
    const [ux] = toCss(1, 0);
    shapes.setAttribute('transform', `matrix(${ux - tx} 0 0 ${ux - tx} ${tx} ${ty})`);
    for (const [label, x, y] of corners) {
      const [lx, ly] = toCss(x, y);
      label.setAttribute('x', (lx + 3).toFixed(1));
      label.setAttribute('y', (ly - 4).toFixed(1));
    }
    placeSketch();
  }

  function placeSketch() {
    clear(sketch);
    const d = st.drawing;
    if (!d) return;
    if (d.tool === 'rectangle' && d.end) {
      const path = pathOf(rectangle(d.start[0], d.start[1], d.end[0], d.end[1]));
      sketch.append(svgEl('path', { d: path, class: 'sketch-halo' }), svgEl('path', { d: path, class: 'sketch-line' }));
    } else if ((d.tool === 'polygon' || d.tool === 'freehand') && d.points.length) {
      const pts = [...d.points, ...(d.hover ? [d.hover] : [])].map(([x, y]) => toCss(x, y).map((v) => v.toFixed(1)).join(',')).join(' ');
      sketch.append(svgEl('polyline', { points: pts, class: 'sketch-halo' }), svgEl('polyline', { points: pts, class: 'sketch-line' }));
      if (d.tool === 'polygon') for (const p of d.points) {
        const [cx, cy] = toCss(...p);
        sketch.append(svgEl('circle', { cx, cy, r: 3, class: 'sketch-point' }));
      }
    } else if (d.tool === 'brush' && d.points.length) {
      const r = d.radius / umPerCss();
      const pts = d.points.map(([x, y]) => toCss(x, y).map((v) => v.toFixed(1)).join(','));
      sketch.append(svgEl('polyline', { points: pts.join(' '), class: `sketch-brush${d.erase ? ' erase' : ''}`, 'stroke-width': 2 * r }));
    }
  }

  // --- Drawing ---
  function setTool(tool) {
    st.tool = st.tool === tool ? null : tool;
    st.drawing = null;
    svg.classList.toggle('drawing', Boolean(st.tool));
    placeSketch();
    renderSection();
  }

  function finish(geometry, tool) {
    st.drawing = null;
    placeSketch();
    if (!geometry?.length || !(area(geometry) > 0)) return;
    try {
      const name = nextRegionName(app.store.project, dataset.id, st.cls);
      const next = addRegion(app.store.project, { dataset: dataset.id, name, cls: st.cls, geometry, tool, layer: layerName(), by: by() });
      st.selected = next.regions.at(-1).id;
      app.store.commit(next, `Draw ${name}`);
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  }

  // What the region was drawn on: the layers shown.
  function layerName() {
    const shown = (images() ?? []).filter((e) => e.transform && renderer.overlays.some((o) => o.desc.id === e.record.id && o.layer.visible)).map((e) => e.record.name);
    return ['the stains', ...shown].join(', ');
  }

  const onDown = (event) => {
    if (!st.tool || event.button !== 0) return;
    event.preventDefault();
    // The keys (Enter, Backspace, Escape, the space bar) to the tissue, not a button last pressed.
    canvas.focus({ preventScroll: true });
    svg.setPointerCapture(event.pointerId);
    const p = toWorld(event);
    if (st.space) {
      st.panning = { x: event.clientX, y: event.clientY, camera: renderer.camera };
      return;
    }
    if (st.tool === 'polygon') {
      st.drawing ??= { tool: 'polygon', points: [] };
      const first = st.drawing.points[0];
      if (first && st.drawing.points.length >= 3) {
        const [fx, fy] = toCss(...first);
        const [px, py] = toCss(...p);
        if (Math.hypot(fx - px, fy - py) < 8) {
          finish(polygonOf(st.drawing.points), 'polygon');
          return;
        }
      }
      st.drawing.points.push(p);
    } else if (st.tool === 'rectangle') st.drawing = { tool: 'rectangle', start: p, end: null };
    else if (st.tool === 'freehand') st.drawing = { tool: 'freehand', points: [p] };
    else if (st.tool === 'brush') st.drawing = { tool: 'brush', points: [p], radius: st.brush * umPerCss(), erase: st.erase };
    placeSketch();
  };
  const onMove = (event) => {
    if (st.panning) {
      const ratio = renderer.ratio || 1;
      const c = st.panning.camera;
      renderer.setCamera({ ...c, cx: c.cx - ((event.clientX - st.panning.x) * ratio) / c.scale, cy: c.cy - ((event.clientY - st.panning.y) * ratio) / c.scale });
      return;
    }
    const d = st.drawing;
    if (!d) return;
    const p = toWorld(event);
    if (d.tool === 'rectangle' && event.buttons) d.end = p;
    else if (d.tool === 'polygon') d.hover = p;
    else if ((d.tool === 'freehand' || d.tool === 'brush') && event.buttons) {
      const last = d.points.at(-1);
      const [lx, ly] = toCss(...last);
      const [px, py] = toCss(...p);
      if (Math.hypot(px - lx, py - ly) >= 2) d.points.push(p);
    }
    placeSketch();
  };
  const onUp = () => {
    if (st.panning) {
      st.panning = null;
      return;
    }
    const d = st.drawing;
    if (!d) return;
    if (d.tool === 'rectangle') {
      if (d.end && Math.abs(d.end[0] - d.start[0]) > 0 && Math.abs(d.end[1] - d.start[1]) > 0) finish(rectangle(d.start[0], d.start[1], d.end[0], d.end[1]), 'rectangle');
      else st.drawing = null;
    } else if (d.tool === 'freehand') {
      if (d.points.length >= 3) finish(polygonOf(d.points), 'freehand');
      else st.drawing = null;
    } else if (d.tool === 'brush') brushed(d);
    placeSketch();
  };
  // A brush stroke: onto the region chosen (or erased from it), else a new region.
  function brushed(d) {
    const target = st.into && st.selected ? regions().find((r) => r.id === st.selected) : null;
    if (d.erase && !target) {
      st.drawing = null;
      toast('Choose a region to erase from.', { kind: 'error' });
      return;
    }
    const geometry = brushGeometry(target?.geometry ?? null, d.points, d.radius, d.erase);
    if (!target) {
      finish(geometry, 'brush');
      return;
    }
    st.drawing = null;
    try {
      app.store.commit(editRegion(app.store.project, target.id, { geometry }, { by: by(), tool: 'brush', note: d.erase ? 'Erased with the brush' : 'Painted with the brush' }), `${d.erase ? 'Erase from' : 'Paint onto'} ${target.name}`);
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  }
  const onDouble = (event) => {
    if (st.tool !== 'polygon' || !st.drawing) return;
    event.preventDefault();
    const pts = st.drawing.points;
    if (pts.length >= 3) finish(polygonOf(pts), 'polygon');
  };
  const onWheel = (event) => {
    if (!st.tool) return;
    event.preventDefault();
    const r = canvas.getBoundingClientRect();
    const ratio = renderer.ratio || 1;
    const delta = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaY;
    zoomBy(Math.exp(-delta * (event.ctrlKey ? 0.01 : 0.0018)), (event.clientX - r.left) * ratio, (event.clientY - r.top) * ratio);
  };
  const onKey = (event) => {
    if (event.key === ' ' && st.tool && !event.target.closest?.('input, textarea, select, button')) {
      st.space = event.type === 'keydown';
      svg.classList.toggle('panning', st.space);
      event.preventDefault();
      return;
    }
    if (event.type !== 'keydown' || !st.tool || event.target.closest?.('input, textarea, select')) return;
    const d = st.drawing;
    if (event.key === 'Escape') {
      if (d) st.drawing = null;
      else setTool(null);
    } else if (event.key === 'Enter' && d?.tool === 'polygon' && d.points.length >= 3) finish(polygonOf(d.points), 'polygon');
    else if (event.key === 'Backspace' && d?.tool === 'polygon') d.points.pop();
    else return;
    event.preventDefault();
    placeSketch();
  };
  svg.addEventListener('pointerdown', onDown);
  svg.addEventListener('pointermove', onMove);
  svg.addEventListener('pointerup', onUp);
  svg.addEventListener('dblclick', onDouble);
  svg.addEventListener('wheel', onWheel, { passive: false });
  window.addEventListener('keydown', onKey);
  window.addEventListener('keyup', onKey);

  // --- Cells ---
  // The cells of the segmentation drawn in each region, counted when first asked (by region version).
  function countOf(r) {
    const cells = drawnCells();
    if (!cells) return null;
    const key = `${r.id}:${r.version}:${cells.n}`;
    if (!st.counts.has(key)) st.counts.set(key, cellsIn(r.geometry, cells).length);
    return st.counts.get(key);
  }
  // The dataset's own cells in a region chosen, as the QC view chooses cells: shown in the Viewer,
  // the QC view and the inspector.
  function chooseCells(r) {
    const rows = cellsIn(r.geometry, ownCells);
    const mask = new Uint8Array(ownCells.n);
    for (const i of rows) mask[i] = 1;
    app.selection = { dataset: dataset.id, mask, label: `Cells in ${r.name}`, count: rows.length };
    app.store.notify(['selection']);
    return rows.length;
  }

  function choose(id) {
    st.selected = st.selected === id ? null : id;
    place();
    renderSection();
  }

  function zoomTo(r) {
    const b = bounds(r.geometry);
    const [w, hh] = renderer.size();
    const scale = Math.min(w / ((b.x1 - b.x0) * 1.3), hh / ((b.y1 - b.y0) * 1.3));
    renderer.setCamera({ cx: (b.x0 + b.x1) / 2, cy: (b.y0 + b.y1) / 2, scale });
  }

  // --- Coordinates for GeoJSON ---
  // The coordinate systems regions are written in or read from: µm, the Xenium image's pixels, and
  // each post-Xenium image placed, its pixels (through the version placing it and any deformation).
  function systems() {
    const out = [{ id: 'um', label: 'µm (the dataset\'s coordinates)' }, { id: 'xenium', label: 'The Xenium image\'s pixels' }];
    for (const e of images() ?? []) if (e.transform) out.push({ id: e.record.id, label: `${e.record.name}'s pixels (as QuPath opens it)`, entry: e });
    return out;
  }
  async function mapping(systemId) {
    if (systemId === 'um') return { to: (x, y) => [x, y], from: (x, y) => [x, y], step: 0, name: 'µm' };
    if (systemId === 'xenium') return { to: (x, y) => [x / pixelSize, y / pixelSize], from: (u, v) => [u * pixelSize, v * pixelSize], step: 0, name: 'the Xenium image\'s pixels' };
    const e = (images() ?? []).find((x) => x.record.id === systemId);
    if (!e?.transform) throw new Error('That image is not aligned.');
    const tf = await app.bundles.transformDescription(e.transform.id);
    const desc = { transform: tf.transform, deformation: tf.deformation ?? null };
    // A deformation bends straight edges: densified every 10 µm (or its pixels' worth) first.
    const um = transformShape(tf.transform).scale;
    return { to: (x, y) => unplacePoint(desc, x, y), from: (u, v) => placePoint(desc, u, v), step: desc.deformation ? 10 : 0, fromStep: desc.deformation ? 10 / um : 0, name: `${e.record.name}'s pixels`, image: e.record };
  }

  async function exportRegions(systemId, which) {
    const m = await mapping(systemId);
    const list = which === 'chosen' && st.selected ? regions().filter((r) => r.id === st.selected) : regions();
    const doc = toGeoJSON(list, { toCoords: m.to, step: m.step });
    const blob = new Blob([`${JSON.stringify(doc, null, 1)}\n`], { type: 'application/geo+json' });
    downloadBlob(blob, `${dataset.name}-regions${m.image ? `-${m.image.name.replace(/[^A-Za-z0-9._-]+/g, '_')}` : ''}.geojson`);
    toast(`Wrote ${list.length} region${list.length === 1 ? '' : 's'} in ${m.name}, as QuPath reads them.`, { kind: 'ok' });
  }

  // Regions from GeoJSON (as QuPath exports annotations), their coordinates in a system: added to the
  // project, each a region of its own. Answers how many and what was left out.
  async function importRegions(doc, systemId, source, who = by()) {
    const m = await mapping(systemId);
    const { regions: found, skipped } = fromGeoJSON(doc, { fromCoords: m.from, step: m.fromStep ?? 0 });
    let next = app.store.project;
    for (const r of found) {
      next = addRegion(next, { dataset: dataset.id, name: r.name ? uniqueRegionName(next, dataset.id, r.name) : nextRegionName(next, dataset.id, r.cls), cls: r.cls, color: r.color, geometry: r.geometry, tool: 'imported', layer: m.name, by: who, note: `Imported from ${source}, in ${m.name}` });
    }
    if (next !== app.store.project) app.store.commit(next, `Import ${found.length} regions from ${source}`);
    return { added: found.length, skipped, ids: next.regions.slice(-found.length).map((r) => r.id) };
  }

  // --- The panel section ---
  const body = h('div');
  const section = h('section.viewer-section.viewer-regions-section', h('h3', icon('polygon'), 'Regions'), body);
  function renderSection() {
    clear(body);
    const list = regions();
    const classes = [...new Set([...CLASSES.map((c) => c.name), ...list.map((r) => r.cls)])];
    const cls = h('select.input', { 'aria-label': 'The class of regions drawn', onchange: (e) => {
      if (e.target.value === '\u0000new') {
        promptDialog({ title: 'A new class', label: 'Its name', confirm: 'Use it' }).then((name) => {
          if (name) {
            st.cls = name;
            prefs.set('regionClass', name);
          }
          renderSection();
        });
        return;
      }
      st.cls = e.target.value;
      prefs.set('regionClass', st.cls);
    } }, ...classes.map((c) => h('option', { value: c, selected: c === st.cls }, c)), h('option', { value: '\u0000new' }, 'A new class…'));
    if (!classes.includes(st.cls)) cls.prepend(h('option', { value: st.cls, selected: true }, st.cls));
    const tools = h('div.btn-row.viewer-region-tools', ...TOOLS.map(([id, label, glyph, hint]) => h(`button.btn.small${st.tool === id ? '.primary' : ''}`, { type: 'button', title: hint, 'aria-pressed': String(st.tool === id), onclick: () => setTool(id) }, icon(glyph), label)));
    const brush = st.tool === 'brush' ? h('div',
      h('label.viewer-slider', 'Brush', h('input', { type: 'range', min: 4, max: 120, step: 1, value: st.brush, 'aria-label': 'The brush\'s radius (pixels)', oninput: (e) => { st.brush = Number(e.target.value); } })),
      h('label.viewer-toggle', h('input', { type: 'checkbox', checked: st.erase, onchange: (e) => { st.erase = e.target.checked; } }), 'Erase'),
      h('label.viewer-toggle', h('input', { type: 'checkbox', checked: st.into, onchange: (e) => { st.into = e.target.checked; } }), 'Onto (or from) the region chosen')) : null;
    const hint = st.tool ? h('p.muted.viewer-hint', `${TOOLS.find((x) => x[0] === st.tool)[3]}. The wheel zooms; hold the space bar to pan; Escape stops drawing.`) : null;
    const rows = list.map((r) => {
      const n = countOf(r);
      const chosen = r.id === st.selected;
      return h(`div.viewer-region${chosen ? '.chosen' : ''}`,
        h('div.row', { onclick: () => choose(r.id), ondblclick: () => { st.selected = r.id; place(); renderSection(); zoomTo(r); } },
          h('span.dot', { style: { background: colorCSS(r.color) } }),
          h('div.name', h('b', r.name), h('span.muted', `${r.cls} · ${formatArea(area(r.geometry))}${n === null ? '' : ` · ${formatCount(n)} cells`}`))),
        chosen ? h('div.viewer-region-detail',
          h('p.muted', `Version ${r.version}, ${r.tool}, by ${r.by}, ${new Date(r.time).toLocaleString()}${r.note ? `: ${r.note}` : ''}. Drawn on ${r.versions[0].layer || 'the tissue'}.`),
          h('div.btn-row',
            h('button.btn.small', { type: 'button', onclick: () => rename(r) }, icon('edit'), 'Rename'),
            h('select.input.small', { 'aria-label': 'Its class', onchange: (e) => reclass(r, e.target.value) }, ...classes.map((c) => h('option', { value: c, selected: c === r.cls }, c))),
            h('button.btn.small', { type: 'button', onclick: () => { const k = chooseCells(r); toast(`Chose the ${formatCount(k)} cells in ${r.name}: the Viewer, the QC view and the inspector show them.`, { kind: 'ok' }); } }, icon('check'), 'Choose its cells'),
            h('button.btn.small', { type: 'button', onclick: () => zoomTo(r) }, icon('expand'), 'Go to it'),
            h('button.btn.small.ghost', { type: 'button', onclick: () => remove(r) }, icon('trash'), 'Remove')),
          r.versions.length > 1 ? h('details', h('summary', `Its ${r.versions.length} versions`), h('ol.viewer-region-versions', ...r.versions.map((v, k) => h('li', `${v.removed ? 'Removed' : `${v.name} (${v.cls}, ${formatArea(area(v.geometry))})`}, by ${v.by}, ${new Date(v.time).toLocaleString()}${v.note ? `: ${v.note}` : ''} `,
            k < r.versions.length - 1 && !v.removed ? h('button.btn.small.ghost', { type: 'button', onclick: () => restore(r, v, k) }, 'Restore') : null)))) : null) : null);
    });
    const systemSel = h('select.input', { 'aria-label': 'Coordinates' }, ...systems().map((x) => h('option', { value: x.id }, x.label)));
    const file = h('input', { type: 'file', accept: '.geojson,.json,application/geo+json,application/json', hidden: true, onchange: async (e) => {
      const f = e.target.files?.[0];
      e.target.value = '';
      if (!f) return;
      try {
        const res = await importRegions(JSON.parse(await f.text()), systemSel.value, f.name);
        const left = Object.entries(res.skipped).map(([k, n]) => `${n} ${k}`).join(', ');
        toast(`Imported ${res.added} region${res.added === 1 ? '' : 's'} from ${f.name}${left ? `; left out: ${left}` : ''}.`, { kind: 'ok' });
      } catch (error) {
        toast(`${f.name} was not imported: ${error.message}`, { kind: 'error' });
      }
    } });
    body.append(
      h('label.viewer-slider', 'Class', cls),
      tools, brush, hint,
      list.length ? h('div.viewer-region-list', ...rows) : h('p.muted.viewer-hint', 'No region yet: choose a tool and draw on the tissue, over any layer.'),
      h('details.viewer-region-files', h('summary', 'GeoJSON, as QuPath reads and writes it'),
        h('label.viewer-slider', 'In', systemSel),
        h('div.btn-row',
          h('button.btn.small', { type: 'button', disabled: !list.length, onclick: () => exportRegions(systemSel.value, 'all').catch((error) => toast(error.message, { kind: 'error' })) }, icon('download'), 'Write them all'),
          st.selected ? h('button.btn.small', { type: 'button', onclick: () => exportRegions(systemSel.value, 'chosen').catch((error) => toast(error.message, { kind: 'error' })) }, icon('download'), 'Write the one chosen') : null,
          h('button.btn.small', { type: 'button', onclick: () => file.click() }, icon('upload'), 'Read a file…'), file),
        h('p.muted.viewer-hint', 'An image\'s pixels are as QuPath opens that image: draw on the H&E in QuPath, export its annotations as GeoJSON, and read them here in its pixels; written in them, regions open on the H&E in QuPath.')));
  }
  const formatArea = (a) => (a >= 1e6 ? `${(a / 1e6).toFixed(2)} mm²` : `${Math.round(a).toLocaleString('en-US')} µm²`);

  async function rename(r) {
    const name = await promptDialog({ title: `Rename ${r.name}`, label: 'Its name', value: r.name, confirm: 'Rename' });
    if (!name || name === r.name) return;
    try {
      app.store.commit(editRegion(app.store.project, r.id, { name }, { by: by() }), `Rename ${r.name}`);
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  }
  function reclass(r, cls) {
    if (cls === r.cls) return;
    app.store.commit(editRegion(app.store.project, r.id, { cls }, { by: by() }), `Class ${r.name} as ${cls}`);
  }
  async function remove(r) {
    const why = await promptDialog({ title: `Remove ${r.name}`, label: 'Why (kept with its versions; optional)', confirm: 'Remove' });
    if (why === null) return;
    st.selected = null;
    app.store.commit(editRegion(app.store.project, r.id, { removed: true }, { by: by(), note: why }), `Remove ${r.name}`);
  }
  function restore(r, v, k) {
    try {
      app.store.commit(editRegion(app.store.project, r.id, { name: v.name, cls: v.cls, color: v.color, geometry: v.geometry }, { by: by(), tool: v.tool, note: `Restored version ${k + 1}` }), `Restore ${r.name}`);
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  }

  renderSection();
  place();

  return {
    section,
    afterFrame: place,
    refresh() {
      if (st.selected && !regions().some((r) => r.id === st.selected)) st.selected = null;
      place();
      renderSection();
    },
    // For the validation harness (validation/viewer.mjs) and remote control.
    hook: {
      setTool,
      tool: () => st.tool,
      setClass: (cls) => { st.cls = cls; renderSection(); },
      setBrush: (radius, erase = false, into = true) => { Object.assign(st, { brush: radius, erase, into }); renderSection(); },
      choose: (id) => { st.selected = id; place(); renderSection(); },
      chosen: () => st.selected,
      list: () => regions().map((r) => ({ id: r.id, name: r.name, cls: r.cls, version: r.version, area: area(r.geometry), cells: countOf(r) })),
      cellsOf: (id) => {
        const r = regions().find((x) => x.id === id);
        return r ? countOf(r) : null;
      },
      chooseCells: (id) => chooseCells(regions().find((r) => r.id === id)),
      // A coordinate system's maps to and from µm ('um', 'xenium' or an image's artifact ID).
      mapping,
      clientOf: (x, y) => {
        const [cx, cy] = toCss(x, y);
        const b = canvas.getBoundingClientRect();
        return [b.left + cx, b.top + cy];
      },
      importRegions,
      exportDocument: async (systemId, ids = null) => {
        const m = await mapping(systemId);
        return toGeoJSON(regions().filter((r) => !ids || ids.includes(r.id)), { toCoords: m.to, step: m.step });
      },
      systems: () => systems().map(({ id, label }) => ({ id, label })),
    },
    dispose() {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('keyup', onKey);
      svg.remove();
    },
  };
}
