// The Alignment view (wave 5, slice 2): a post-Xenium image placed on the tissue by landmarks. The
// Xenium image (the dataset's stains, in µm) and the post-Xenium image (in its own pixels) side by
// side, each panned and zoomed on its own or linked through the fit. A landmark is a point placed on
// both: click one image, then the same place on the other; once three are placed the fit predicts
// where the other point falls, and the other image goes there. Landmarks are dragged to move them.
// The fit (lib/transforms.js, the program's own bit for bit) and each landmark's residual and
// leave-one-out error update as they change; the fitted image can be laid over the stains to see
// it. A fit is saved as a version of the image's alignment (api_transforms.go), with who made it,
// a note and the version it was made from, and chosen to place the image; any version is written
// as Xenium Explorer's matrix or keypoints.

import { h, icon, clear } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { prefs } from './storage.js';
import { TissueRenderer } from '../render/renderer.js';
import { fitCamera, screenToWorld, worldToScreen } from '../lib/viewport.js';
import { attachNavigation } from './navigation.js';
import { readCells } from '../lib/viewer-data.js';
import { defaultLayers } from './viewer-panel.js';
import { describeImage, describePlacement, imagesOf } from '../lib/images.js';
import { defaultImageLayer, invert } from '../lib/overlays.js';
import { describeTransform, fitLandmarks, MIN_LANDMARKS, MODELS, placePoint, transformInUse, transformShape, transformsOf, useTransform } from '../lib/transforms.js';

// Per image id, for the session: { landmarks, model, parent, selected, dirty, cameras, overlay }.
const sessions = new Map();

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(body.error ?? `HTTP ${response.status}`), { status: response.status });
  return body;
}

const fmt = (v, digits = 2) => (Number.isFinite(v) ? v.toFixed(digits) : '–');
const IDENTITY = [[1, 0, 0], [0, 1, 0]];
const apply = (t, x, y) => [t[0][0] * x + t[0][1] * y + t[0][2], t[1][0] * x + t[1][1] * y + t[1][2]];

export function mountAlignment(app, container) {
  const body = h('div.align-body');
  const title = h('span.crumbs');
  const headActions = h('div.btn-row');
  const root = h('div.view.viewer.alignment',
    h('div.workbench-head', h('h1', icon('target'), 'Alignment', title), h('span.spacer'), headActions),
    body);
  container.append(root);

  let current = null; // { key, dispose, ... }
  let generation = 0;

  // The dataset and image shown: as asked (app.showAlignment), else the dataset in focus and its
  // first image (or the one shown last).
  function target() {
    const { project, ui } = app.store;
    const pending = app.pendingAlignment;
    const id = pending?.dataset ?? (ui.focus?.kind === 'dataset' ? ui.focus.id : null) ?? project.datasets.find((d) => imagesOf(project, d.id).length)?.id ?? project.datasets[0]?.id;
    const dataset = project.datasets.find((d) => d.id === id);
    if (!dataset) return null;
    const images = imagesOf(project, dataset.id);
    const image = images.find((r) => r.id === pending?.image) ?? images.find((r) => r.id === current?.image?.id) ?? images[0] ?? null;
    const link = app.bundles.link(dataset.id);
    return { dataset, images, image, item: link?.item ?? app.bundles.itemByPath(dataset.path), link };
  }

  function message(iconName, heading, text, actions = []) {
    teardown();
    clear(body);
    body.append(h('div.planned', h('div.empty', icon(iconName), h('h3', heading), h('p', text), actions.length ? h('div.btn-row', { style: { marginTop: '12px' } }, ...actions) : null)));
  }

  function teardown() {
    clear(headActions);
    if (!current) return;
    current.dispose();
    current = null;
  }

  async function show() {
    const t = target();
    const mine = (generation += 1);
    if (!t) {
      title.textContent = '';
      message('target', 'No dataset yet', 'Add a Xenium output bundle in the Data view, then a post-Xenium image of its section in the Viewer.', [h('button.btn.primary', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
      return;
    }
    title.textContent = ` · ${t.dataset.name}${t.image ? ` · ${t.image.name}` : ''}`;
    if (!t.image) {
      message('target', `${t.dataset.name} has no post-Xenium image`, 'Add an H&E or immunofluorescence image of this section in the Viewer (its layers panel, Post-Xenium images); then align it here.', [h('button.btn.primary', { type: 'button', onclick: () => app.setMode('viewer') }, icon('tissue'), 'Go to the Viewer')]);
      return;
    }
    const key = `${t.dataset.id}|${t.image.id}|${t.item?.path}`;
    const pending = app.pendingAlignment;
    if (current?.key === key) {
      if (pending) {
        app.pendingAlignment = null;
        if (pending.transform) current.loadVersion(pending.transform);
      }
      current.refresh();
      return;
    }
    if (!t.item) {
      message('target', `${t.dataset.name} is not available`, 'Its bundle was not found where the project records it. Locate it in the Data view.', [h('button.btn', { type: 'button', onclick: () => app.setMode('data') }, icon('database'), 'Go to Data')]);
      return;
    }
    message('target', `Opening ${t.image.name}…`, 'Reading the dataset and the image.');
    let desc;
    let cells;
    let imageDesc;
    try {
      [desc, imageDesc] = await Promise.all([getJSON(`${t.item.url}/view`), app.bundles.imageDescription(t.image.id)]);
      const response = await fetch(`${t.item.url}/cells`);
      if (!response.ok) throw Object.assign(new Error(`cells: HTTP ${response.status}`), { status: response.status });
      cells = readCells(await response.arrayBuffer());
    } catch (error) {
      if (mine !== generation) return;
      if (error.status === 409) {
        message('layers', 'Its derived data are being built', 'Open it in the Viewer, which builds them; then align its image here.', [h('button.btn', { type: 'button', onclick: () => app.setMode('viewer') }, icon('tissue'), 'Go to the Viewer')]);
        return;
      }
      message('warning', `${t.image.name} cannot be shown`, error.message);
      return;
    }
    if (mine !== generation) return;
    teardown();
    clear(body);
    app.pendingAlignment = null;
    current = open(t, desc, cells, { ...imageDesc, id: t.image.id }, key);
    if (pending?.transform) current.loadVersion(pending.transform);
  }

  function open(t, desc, cells, imageDesc, key) {
    const record = t.image;
    const lv0 = imageDesc.image.levels[0];
    const session = sessions.get(record.id) ?? { landmarks: [], model: 'similarity', parent: null, selected: -1, dirty: false, overlay: { visible: false, opacity: 0.5 }, link: true };
    sessions.set(record.id, session);
    const st = session;

    // --- The stages ---
    const stage = (side, label) => {
      const canvas = h('canvas.viewer-canvas', { tabIndex: 0, 'aria-label': side === 'left' ? `The Xenium image of ${t.dataset.name}: click to place a landmark, drag to pan, scroll to zoom` : `${record.name}: click to place a landmark, drag to pan, scroll to zoom` });
      const marks = h('div.align-marks');
      const dots = h('div.align-marks');
      const zoomIn = h('button.icon-button', { type: 'button', title: 'Zoom in', 'aria-label': 'Zoom in' }, icon('plus'));
      const zoomOut = h('button.icon-button', { type: 'button', title: 'Zoom out', 'aria-label': 'Zoom out' }, icon('minus'));
      const fit = h('button.icon-button', { type: 'button', title: 'The whole image', 'aria-label': 'Show the whole image' }, icon('expand'));
      const el = h('div.viewer-stage.align-stage', canvas, dots, marks, h('div.viewer-tools', zoomIn, zoomOut, fit), h('div.align-stage-label', label));
      return { side, canvas, marks, dots, el, zoomIn, zoomOut, fit };
    };
    const L = stage('left', `Xenium: ${desc.channels.map((c) => c.name).join(', ')} (µm)`);
    const R = stage('right', `${record.name} (its pixels)`);
    const panelHost = h('aside.viewer-panel.align-panel', { 'aria-label': 'Alignment' });
    body.append(panelHost, h('div.align-stages', L.el, R.el));

    // The Xenium image: its stains only; the post-Xenium image, under the fit, laid over when asked.
    const base = defaultLayers(desc);
    const quiet = { ...base, transcripts: { ...base.transcripts, visible: false }, cells: { ...base.cells, outlines: false, nuclei: false, fill: null } };
    L.renderer = new TissueRenderer(L.canvas, { onChange: () => afterFrame(L) });
    L.renderer.setDataset(desc, cells);
    L.renderer.setLayers(quiet);
    // The post-Xenium image alone, in its pixels: every channel shown, the stains not.
    R.renderer = new TissueRenderer(R.canvas, { onChange: () => afterFrame(R) });
    R.renderer.setDataset(desc, null);
    R.renderer.setLayers({ ...quiet, channels: quiet.channels.map((c) => ({ ...c, visible: false })) });
    const own = defaultImageLayer(imageDesc);
    own.opacity = 1;
    own.channels = own.channels.map((c) => ({ ...c, visible: true }));
    R.renderer.setOverlays([{ desc: { ...imageDesc, transform: IDENTITY }, layer: own }]);
    const sides = [L, R];

    // --- Landmarks and the fit ---
    const complete = () => st.landmarks.filter((l) => l.fixed && l.moving);
    let fit = null;
    let fitError = '';
    const inUse = () => transformInUse(app.store.project, record.id);
    // The transform that predicts: the fit, else the version in use (its description, once read).
    let inUseDesc = null;
    const predictor = () => fit?.transform ?? inUseDesc?.transform ?? null;
    function refit() {
      const list = complete();
      fit = null;
      fitError = '';
      if (list.length >= MIN_LANDMARKS) {
        try {
          fit = fitLandmarks(st.model, list);
        } catch (error) {
          fitError = error.message;
        }
      }
      applyOverlay();
      renderPanel();
      placeAll();
    }
    function applyOverlay() {
      const tf = predictor();
      const layer = { ...defaultImageLayer(imageDesc), visible: st.overlay.visible, opacity: st.overlay.opacity };
      L.renderer.setOverlays(tf && st.overlay.visible ? [{ desc: { ...imageDesc, transform: tf }, layer }] : []);
    }
    function changed() {
      st.dirty = true;
      refit();
    }

    // Placing: a click on either image places that side of the landmark being made (a new one when
    // the last is complete); with a fit, the other side's place is predicted and shown there.
    function place(side, sx, sy) {
      const s = side === 'left' ? L : R;
      const [w, hh] = s.renderer.size();
      const p = screenToWorld(s.renderer.camera, w, hh, sx, sy);
      let last = st.landmarks.at(-1);
      if (!last || (last.fixed && last.moving)) {
        last = { fixed: null, moving: null };
        st.landmarks.push(last);
      }
      last[side === 'left' ? 'fixed' : 'moving'] = p;
      st.selected = st.landmarks.length - 1;
      if (!(last.fixed && last.moving)) goToPredicted(last);
      changed();
    }
    // The other image centred where the fit puts a half-placed landmark.
    function goToPredicted(l) {
      const tf = predictor();
      if (!tf) return;
      if (l.fixed && !l.moving) {
        const inv = invert(tf);
        if (!inv) return;
        const [u, v] = apply(inv, l.fixed[0], l.fixed[1]);
        R.renderer.setCamera({ ...R.renderer.camera, cx: u, cy: v });
      } else if (l.moving && !l.fixed) {
        const [x, y] = apply(tf, l.moving[0], l.moving[1]);
        L.renderer.setCamera({ ...L.renderer.camera, cx: x, cy: y });
      }
    }
    // Where a half-placed landmark's other side is predicted.
    function predicted(l) {
      const tf = predictor();
      if (!tf || (l.fixed && l.moving)) return null;
      if (l.fixed) {
        const inv = invert(tf);
        return inv ? { side: 'right', at: apply(inv, l.fixed[0], l.fixed[1]) } : null;
      }
      if (l.moving) return { side: 'left', at: apply(tf, l.moving[0], l.moving[1]) };
      return null;
    }

    // --- Markers: each landmark on both images, numbered; dragged to move ---
    // A marker per landmark and image, kept while the landmark is (a marker being dragged must stay
    // in the page to keep the pointer), moved to its place on each frame.
    function placeMarks(s) {
      s.markEls ??= [];
      const ratio = s.renderer.ratio || 1;
      const [w, hh] = s.renderer.size();
      st.landmarks.forEach((l, i) => {
        const p = s.side === 'left' ? l.fixed : l.moving;
        const pred = predicted(l);
        const at = p ?? (pred?.side === s.side ? pred.at : null);
        let el = s.markEls[i];
        if (!el) {
          el = h('div.align-mark', h('span'));
          el.addEventListener('pointerdown', (event) => drag(event, s, Number(el.dataset.index)));
          s.markEls[i] = el;
          s.marks.append(el);
        }
        const [x, y] = at ? worldToScreen(s.renderer.camera, w, hh, at[0], at[1]) : [0, 0];
        el.hidden = !at || x < -20 || y < -20 || x > w + 20 || y > hh + 20;
        el.dataset.index = String(i);
        el.className = `align-mark${i === st.selected ? ' selected' : ''}${p ? '' : ' predicted'}`;
        el.style.left = `${x / ratio}px`;
        el.style.top = `${y / ratio}px`;
        el.title = p ? `Landmark ${i + 1}: drag to move it` : `Landmark ${i + 1}, predicted: click the image to place it`;
        el.firstChild.textContent = String(i + 1);
      });
      for (const el of s.markEls.splice(st.landmarks.length)) el.remove();
    }
    function placeAll() {
      for (const s of sides) {
        placeMarks(s);
        placeDots(s);
      }
    }

    // An automatic version's local matches, shown as dots on both images: kept, or dropped as
    // disagreeing with the others.
    function placeDots(s) {
      s.dotEls ??= [];
      const list = st.matches?.list ?? [];
      const ratio = s.renderer.ratio || 1;
      const [w, hh] = s.renderer.size();
      list.forEach((m, i) => {
        let el = s.dotEls[i];
        if (!el) {
          el = h('div.align-dot');
          s.dotEls[i] = el;
          s.dots.append(el);
        }
        const at = s.side === 'left' ? m.fixed : m.moving;
        const [x, y] = worldToScreen(s.renderer.camera, w, hh, at[0], at[1]);
        el.hidden = x < -5 || y < -5 || x > w + 5 || y > hh + 5;
        el.className = `align-dot${m.kept ? '' : ' dropped'}`;
        el.style.left = `${x / ratio}px`;
        el.style.top = `${y / ratio}px`;
        el.title = `A local match (quality ${m.quality.toFixed(1)})${m.kept ? '' : ', dropped: it disagrees with the others'}`;
      });
      for (const el of s.dotEls.splice(list.length)) el.remove();
    }
    function drag(event, s, i) {
      const key = s.side === 'left' ? 'fixed' : 'moving';
      if (!st.landmarks[i]?.[key]) return;
      event.stopPropagation();
      event.preventDefault();
      st.selected = i;
      const mark = event.currentTarget;
      mark.setPointerCapture(event.pointerId);
      let frame = 0;
      const move = (e) => {
        const r = s.canvas.getBoundingClientRect();
        const ratio = s.renderer.ratio || 1;
        const [w, hh] = s.renderer.size();
        st.landmarks[i][key] = screenToWorld(s.renderer.camera, w, hh, (e.clientX - r.left) * ratio, (e.clientY - r.top) * ratio);
        st.dirty = true;
        placeMarks(s);
        if (!frame) frame = requestAnimationFrame(() => {
          frame = 0;
          refit();
        });
      };
      const up = () => {
        mark.removeEventListener('pointermove', move);
        mark.removeEventListener('pointerup', up);
        mark.removeEventListener('pointercancel', up);
        refit();
      };
      mark.addEventListener('pointermove', move);
      mark.addEventListener('pointerup', up);
      mark.addEventListener('pointercancel', up);
      renderPanel();
      placeAll();
    }

    // Linked views: a move of one image moves the other to the same place, at the same scale.
    let following = null;
    const camKey = (c) => `${c.cx.toFixed(3)}:${c.cy.toFixed(3)}:${c.scale.toPrecision(6)}`;
    const lastCam = new Map();
    function afterFrame(s) {
      placeMarks(s);
      placeDots(s);
      const cam = s.renderer.camera;
      const k = camKey(cam);
      if (lastCam.get(s) === k) return;
      lastCam.set(s, k);
      if (following === s) {
        following = null;
        return;
      }
      const tf = predictor();
      if (!st.link || !tf) return;
      const other = s === L ? R : L;
      const um = Math.sqrt(Math.abs(tf[0][0] * tf[1][1] - tf[0][1] * tf[1][0]));
      let next;
      if (s === L) {
        const inv = invert(tf);
        if (!inv) return;
        const [u, v] = apply(inv, cam.cx, cam.cy);
        next = { cx: u, cy: v, scale: cam.scale * um };
      } else {
        const [x, y] = apply(tf, cam.cx, cam.cy);
        next = { cx: x, cy: y, scale: cam.scale / um };
      }
      following = other;
      other.renderer.setCamera(next);
    }

    // --- Size and cameras ---
    const fitSide = (s) => {
      const [w, hh] = s.renderer.size();
      if (!w || !hh) return;
      const rect = s === L
        ? { x0: 0, y0: 0, x1: desc.grid.width * desc.grid.pixelSize, y1: desc.grid.height * desc.grid.pixelSize }
        : { x0: 0, y0: 0, x1: lv0.width, y1: lv0.height };
      following = s === L ? null : following;
      s.renderer.setCamera(fitCamera(rect, w, hh));
    };
    const observer = new ResizeObserver(() => {
      for (const s of sides) {
        const r = s.el.getBoundingClientRect();
        s.renderer.resize(r.width, r.height);
        if (!s.placed && r.width > 0) {
          s.placed = true;
          const saved = st.cameras?.[s.side];
          if (saved) s.renderer.setCamera(saved);
          else fitSide(s);
        }
      }
    });
    for (const s of sides) observer.observe(s.el);
    const navigations = sides.map((s) => attachNavigation(s.canvas, s.renderer, { onClick: (sx, sy) => place(s.side, sx, sy) }));
    sides.forEach((s, k) => {
      s.zoomIn.onclick = () => navigations[k].zoomBy(2);
      s.zoomOut.onclick = () => navigations[k].zoomBy(0.5);
      s.fit.onclick = () => fitSide(s);
    });
    const onKey = (event) => {
      if (event.target.closest?.('input, textarea, select')) return;
      if ((event.key === 'Delete' || event.key === 'Backspace') && st.selected >= 0) {
        removeLandmark(st.selected);
        event.preventDefault();
      } else if (event.key === 'Escape') {
        const last = st.landmarks.at(-1);
        if (last && !(last.fixed && last.moving)) {
          st.landmarks.pop();
          st.selected = -1;
          refit();
        }
      }
    };
    window.addEventListener('keydown', onKey);
    function removeLandmark(i) {
      st.landmarks.splice(i, 1);
      st.selected = Math.min(st.selected, st.landmarks.length - 1);
      changed();
    }

    // --- The panel ---
    async function readInUse() {
      const u = inUse();
      inUseDesc = u ? await app.bundles.transformDescription(u.record.id).catch(() => null) : null;
      applyOverlay();
      renderPanel();
    }
    // Loads a version's landmarks to edit (its file's keypoints, in µm), as made from it.
    async function loadVersion(id) {
      const tfRecord = transformsOf(app.store.project, record.id).find((r) => r.id === id);
      if (!tfRecord) {
        toast(`No alignment ${id} of ${record.name}.`, { kind: 'error' });
        return;
      }
      const d = await app.bundles.transformDescription(id);
      if (!d.landmarks?.length) {
        toast(`${tfRecord.name} has no landmarks to edit (it is a transformation matrix): place landmarks to make another version.`, { kind: 'error' });
        return;
      }
      st.landmarks = d.landmarks.map((l) => ({ fixed: [...l.fixed], moving: [...l.moving] }));
      st.model = d.model;
      st.parent = id;
      st.selected = -1;
      st.dirty = false;
      refit();
    }
    async function save(use) {
      const list = complete();
      if (!fit) {
        toast(fitError || `Place at least ${MIN_LANDMARKS} landmarks on both images first.`, { kind: 'error' });
        return null;
      }
      const by = byInput.value.trim() || 'the user';
      prefs.set('reviewer', byInput.value.trim());
      try {
        const answer = await app.bundles.makeTransform(t.dataset.id, record.id, { landmarks: list, model: st.model, parent: st.parent ?? '', by, note: noteInput.value.trim() }, { use, rationale: noteInput.value.trim(), by });
        st.parent = answer.manifest.id;
        st.dirty = false;
        noteInput.value = '';
        toast(`Saved ${answer.manifest.name}${use ? ': it places the image' : ''}.`, { kind: 'ok' });
        await readInUse();
        return answer;
      } catch (error) {
        toast(`The alignment was not saved: ${error.message}`, { kind: 'error' });
        return null;
      }
    }
    async function choose(tfRecord) {
      const reason = await promptDialog({ title: `Place ${record.name} by ${tfRecord.name}`, label: 'Why (kept with the decision; optional)', value: '', confirm: 'Use it' });
      if (reason === null) return;
      const by = prefs.get('reviewer', '') || 'the user';
      app.store.commit(useTransform(app.store.project, { transform: tfRecord.id, rationale: reason, by }), `Place ${record.name} by ${tfRecord.name}`);
      await readInUse();
    }

    // Shows (or hides) an automatic version's matches.
    async function showMatches(id) {
      if (st.matches?.id === id) st.matches = null;
      else {
        const d = await app.bundles.transformDescription(id);
        st.matches = { id, list: d.matches ?? [] };
      }
      renderPanel();
      placeAll();
    }

    // Aligning automatically: the image's nuclei registered to DAPI by a job, saved as a version.
    const nuclear = imageDesc.image.kind === 'rgb' ? -1 : Math.max(0, imageDesc.image.channels.findIndex((c) => /dapi|hoechst|nucle/i.test(c.name)));
    st.auto ??= { model: 'similarity', nonrigid: false, channel: nuclear, start: false, use: !inUse() };
    function automatic(u) {
      const a = st.auto;
      const running = app.bundles.aligning(record.id).length > 0;
      const model = h('select.input', { 'aria-label': 'What the alignment may change', onchange: (e) => { a.model = e.target.value; } }, ...MODELS.map((m) => h('option', { value: m, selected: m === a.model }, m === 'similarity' ? 'Similarity (move, turn, scale, mirror)' : 'Affine (also stretch and shear)')));
      const channel = imageDesc.image.kind === 'rgb' ? null : h('select.input', { 'aria-label': 'The image\'s nuclear stain', onchange: (e) => { a.channel = Number(e.target.value); } }, ...imageDesc.image.channels.map((c, i) => h('option', { value: i, selected: i === a.channel }, c.name)));
      const box = (key, label, disabled = false) => h('label.viewer-toggle', h('input', { type: 'checkbox', checked: Boolean(a[key]) && !disabled, disabled, onchange: (e) => { a[key] = e.target.checked; } }), label);
      const go = h('button.btn.small.primary', { type: 'button', disabled: running, onclick: async () => {
        const by = byInput.value.trim() || 'the user';
        prefs.set('reviewer', byInput.value.trim());
        const request = { model: a.model, nonrigid: a.nonrigid, by, note: noteInput.value.trim(), ...(a.start && u ? { start: u.record.id } : {}), ...(imageDesc.image.kind === 'rgb' ? {} : { channel: a.channel }) };
        renderPanel();
        try {
          const m = await app.bundles.registerImage(t.item, t.dataset.id, record.id, request, { use: a.use, rationale: noteInput.value.trim() || 'Aligned automatically by the nuclei.', by });
          toast(`Aligned automatically: ${m.name}${a.use ? '; it places the image' : ''}.`, { kind: 'ok' });
          await readInUse();
        } catch (error) {
          toast(`The image was not aligned automatically: ${error.message}`, { kind: 'error' });
          renderPanel();
        }
      } }, icon('sparkles'), running ? 'Aligning…' : 'Align automatically');
      return h('section.align-section',
        h('h3', 'Align automatically'),
        h('p.muted', `By the nuclei: the ${imageDesc.image.kind === 'rgb' ? 'H&E\'s haematoxylin' : 'image\'s nuclear stain'} against the Xenium image's DAPI. Its scale, turn and mirroring are found whatever its metadata says, then refined over patches of the tissue. Seconds for a small section, a minute or so for a large one.`),
        h('label.viewer-slider', 'Fit', model),
        channel ? h('label.viewer-slider', 'Nuclear stain', channel) : null,
        box('nonrigid', 'Allow a smooth deformation (for a section that stretched); kept only if it predicts the tissue better'),
        box('start', 'Start from the version in use (no search)', !u),
        box('use', 'Place the image by it when done'),
        h('div.btn-row', go),
        running ? h('p.muted.viewer-hint', 'Aligning: its progress is under Jobs. The result is saved as a version below.') : null);
    }

    const byInput = h('input.input', { type: 'text', value: prefs.get('reviewer', ''), placeholder: 'Your name or initials', 'aria-label': 'Who places the landmarks' });
    const noteInput = h('input.input', { type: 'text', placeholder: 'A note (kept with the version)', 'aria-label': 'A note' });
    function renderPanel() {
      clear(panelHost);
      const project = app.store.project;
      const u = inUse();
      const images = imagesOf(project, t.dataset.id);
      const imageSelect = images.length > 1 ? h('select.input', { 'aria-label': 'The image', onchange: (e) => app.showAlignment(t.dataset.id, e.target.value) }, ...images.map((r) => h('option', { value: r.id, selected: r.id === record.id }, r.name))) : null;
      const head = h('section.align-section',
        h('h3', 'Image'),
        imageSelect,
        h('p', h('b', record.name)),
        h('p.muted', `${describeImage(record)}; its metadata says ${record.summary?.pixelSize ? `${record.summary.pixelSize} µm per pixel` : 'nothing of its pixels\' size'}.`),
        h('p', u ? `Placed by ${u.record.name}: ${describePlacement(u.record.summary)}.` : 'Not aligned yet: it is not drawn on the tissue.'));

      // The landmarks and the fit.
      const model = h('select.input', { 'aria-label': 'What the fit may change', onchange: (e) => { st.model = e.target.value; changed(); } }, ...MODELS.map((m) => h('option', { value: m, selected: m === st.model }, m === 'similarity' ? 'Similarity (move, turn, scale, mirror)' : 'Affine (also stretch and shear)')));
      // A landmark stands out when its left-out error is over three times the median (and over
      // 1 µm): one misplaced landmark cannot raise the median as it raises the root-mean-square.
      const sortedLoo = fit?.leaveOneOut ? [...fit.leaveOneOut].sort((a, b) => a - b) : [];
      const medianLoo = sortedLoo.length ? sortedLoo[Math.floor(sortedLoo.length / 2)] : 0;
      const rows = st.landmarks.map((l, i) => {
        const k = complete().indexOf(l);
        const res = fit && k >= 0 ? fit.residuals[k] : null;
        const loo = fit?.leaveOneOut && k >= 0 ? fit.leaveOneOut[k] : null;
        const worst = fit?.leaveOneOut && loo !== null && loo > Math.max(1, 3 * medianLoo);
        return h(`tr${i === st.selected ? '.selected' : ''}${worst ? '.warn' : ''}`, { onclick: () => { st.selected = i; renderPanel(); placeAll(); } },
          h('td', String(i + 1)),
          h('td.mono', l.fixed ? `${fmt(l.fixed[0], 1)}, ${fmt(l.fixed[1], 1)}` : '…'),
          h('td.mono', l.moving ? `${fmt(l.moving[0], 1)}, ${fmt(l.moving[1], 1)}` : '…'),
          h('td.mono', res === null ? '' : fmt(res)),
          h('td.mono', { title: worst ? 'Far more than the others: is it misplaced?' : '' }, loo === null ? '' : fmt(loo)),
          h('td', h('button.icon-button', { type: 'button', title: `Remove landmark ${i + 1}`, 'aria-label': `Remove landmark ${i + 1}`, onclick: (e) => { e.stopPropagation(); removeLandmark(i); } }, icon('close'))));
      });
      const table = st.landmarks.length ? h('table.align-landmarks', h('thead', h('tr', h('th', '#'), h('th', 'Xenium (µm)'), h('th', 'Image (px)'), h('th', { title: 'Its distance from where the fit puts it (µm)' }, 'Residual'), h('th', { title: 'Its distance from where the fit of the others puts it (µm): the error to expect where no landmark is' }, 'Left out'), h('th', ''))), h('tbody', ...rows)) : null;
      let summary;
      if (fit) {
        const shape = transformShape(fit.transform);
        const turn = Math.atan2(shape.turn[1], shape.turn[0]) * 180 / Math.PI;
        const under = u && inUseDesc && u.record.id !== st.parent ? complete().map((l) => {
          const [x, y] = placePoint(inUseDesc, l.moving[0], l.moving[1]);
          return Math.hypot(x - l.fixed[0], y - l.fixed[1]);
        }) : null;
        const underRms = under ? Math.sqrt(under.reduce((a, r) => a + r * r, 0) / under.length) : null;
        summary = h('div.align-fit',
          h('p', h('b', `${complete().length} landmarks, ${st.model}:`), ` residuals ${fmt(fit.rms)} µm root-mean-square (at most ${fmt(fit.max)})`, fit.leaveOneOut ? `; left out, ${fmt(fit.looRms)} µm (at most ${fmt(fit.looMax)}).` : '; too few to leave one out.'),
          h('p.muted', `${fmt(shape.scale, 4)} µm per pixel, turned ${fmt(turn, 1)}°${shape.reflected ? ', mirrored' : ''}${st.model === 'affine' ? `; its scales ${fmt(shape.aspect, 4)} to 1, sheared ${fmt(shape.shear, 4)}` : ''}.`),
          underRms !== null ? h('p.muted', `By the version in use (${u.record.name}), these landmarks are ${fmt(underRms)} µm off, root-mean-square (at most ${fmt(Math.max(...under))}).`) : null);
      } else {
        summary = h('p.muted', fitError || `Place at least ${MIN_LANDMARKS} landmarks: click a place on one image, then the same place on the other. Nuclei and the tissue's edges make good landmarks; spread them over the section.`);
      }
      const overlayBox = h('input', { type: 'checkbox', checked: st.overlay.visible, disabled: !predictor(), onchange: (e) => { st.overlay.visible = e.target.checked; applyOverlay(); } });
      const overlayOpacity = h('input', { type: 'range', min: 0, max: 1, step: 0.05, value: st.overlay.opacity, 'aria-label': 'Its opacity', oninput: (e) => { st.overlay.opacity = Number(e.target.value); applyOverlay(); } });
      const linkBox = h('input', { type: 'checkbox', checked: st.link, onchange: (e) => { st.link = e.target.checked; } });
      const landmarks = h('section.align-section',
        h('h3', 'Landmarks'),
        h('label.viewer-slider', 'Fit', model),
        table, summary,
        h('label.viewer-toggle', overlayBox, 'Lay the image over the stains, as fitted'),
        h('label.viewer-slider', 'Opacity', overlayOpacity),
        h('label.viewer-toggle', linkBox, 'Move the images together'),
        h('label.viewer-slider', 'By', byInput),
        h('label.viewer-slider', 'Note', noteInput),
        h('div.btn-row',
          h('button.btn.small.primary', { type: 'button', disabled: !fit, onclick: () => save(true) }, icon('check'), 'Save and use'),
          h('button.btn.small', { type: 'button', disabled: !fit, onclick: () => save(false) }, icon('save'), 'Save as a version'),
          h('button.btn.small.ghost', { type: 'button', disabled: !st.landmarks.length, onclick: () => { st.landmarks = []; st.parent = null; st.selected = -1; changed(); } }, 'Clear')),
        st.parent ? h('p.muted', `Made from ${transformsOf(project, record.id).find((r) => r.id === st.parent)?.name ?? st.parent}${st.dirty ? ', changed' : ''}.`) : null,
        h('p.muted.viewer-hint', 'Delete removes the landmark chosen; Escape, a landmark placed on one image only.'));

      // The versions.
      const versions = transformsOf(project, record.id).slice().sort((a, b) => String(a.created).localeCompare(String(b.created)));
      const list = versions.map((v) => {
        const using = u?.record.id === v.id;
        const parentName = v.parent ? versions.find((x) => x.id === v.parent)?.name ?? v.parent : null;
        const editable = ['landmarks', 'keypoints'].includes(v.summary?.method);
        return h(`div.align-version${using ? '.in-use' : ''}`,
          h('div.row', h('b', v.name), using ? h('span.badge.ok', 'In use') : null),
          h('p.muted', `${describeTransform(v)}. ${describePlacement(v.summary)}.`),
          h('p.muted', `By ${v.summary?.by ?? 'the user'}, ${v.created ? new Date(v.created).toLocaleString() : ''}${parentName ? `; made from ${parentName}` : ''}.`),
          using && u.decision ? h('p.muted', `In use since ${new Date(u.decision.time).toLocaleString()}, chosen by ${u.decision.by}: ${u.decision.rationale}`) : null,
          h('div.btn-row',
            using ? null : h('button.btn.small', { type: 'button', onclick: () => choose(v) }, icon('check'), 'Use'),
            editable ? h('button.btn.small', { type: 'button', onclick: () => loadVersion(v.id) }, icon('edit'), 'Edit from it') : null,
            v.summary?.method === 'automatic' ? h('button.btn.small', { type: 'button', 'aria-pressed': String(st.matches?.id === v.id), onclick: () => showMatches(v.id) }, icon('dots'), st.matches?.id === v.id ? 'Hide its matches' : 'Show its matches') : null,
            h('a.btn.small.ghost', { href: `/api/transforms/${encodeURIComponent(v.id)}/xenium-explorer/matrix`, download: '', title: 'Its transformation matrix for Xenium Explorer (the image\'s pixels to the Xenium image\'s)' }, icon('download'), 'Matrix'),
            editable ? h('a.btn.small.ghost', { href: `/api/transforms/${encodeURIComponent(v.id)}/xenium-explorer/keypoints`, download: '', title: 'Its landmarks as Xenium Explorer\'s keypoints' }, icon('download'), 'Keypoints') : null));
      });
      const versionSection = h('section.align-section', h('h3', 'Versions'), versions.length ? h('div', ...list) : h('p.muted', 'None yet: a version is saved from landmarks here, or supplied with the image (Xenium Explorer\'s matrix or keypoints).'));
      panelHost.append(head, automatic(u), landmarks, versionSection);
    }

    readInUse();
    refit();

    // For the validation harness (validation/viewer.mjs) and remote control.
    const hook = {
      dataset: t.dataset.id,
      image: record.id,
      renderers: { left: L.renderer, right: R.renderer },
      settled: () => Promise.all([L.renderer.settled(), R.renderer.settled()]),
      state: () => ({ image: record.id, model: st.model, parent: st.parent, landmarks: st.landmarks.map((l) => ({ fixed: l.fixed, moving: l.moving })), fit, error: fitError, inUse: inUse()?.record.id ?? null, dirty: st.dirty }),
      setLandmarks: (landmarks, model = st.model) => {
        st.landmarks = landmarks.map((l) => ({ fixed: l.fixed ? [...l.fixed] : null, moving: l.moving ? [...l.moving] : null }));
        st.model = model;
        changed();
      },
      setOverlay: (visible, opacity = st.overlay.opacity) => {
        st.overlay = { visible, opacity };
        applyOverlay();
        renderPanel();
      },
      place: (side, x, y) => {
        const s = side === 'left' ? L : R;
        const [w, hh] = s.renderer.size();
        place(side, ...worldToScreen(s.renderer.camera, w, hh, x, y));
      },
      save: (use) => save(use),
      loadVersion: (id) => loadVersion(id),
      readColor: (side, x, y) => (side === 'left' ? L : R).renderer.readColor(x, y),
      // Where a point (µm on the left, the image's pixels on the right) is in the window (CSS pixels).
      clientOf: (side, x, y) => {
        const s = side === 'left' ? L : R;
        const [w, hh] = s.renderer.size();
        const [sx, sy] = worldToScreen(s.renderer.camera, w, hh, x, y);
        const r = s.canvas.getBoundingClientRect();
        const ratio = s.renderer.ratio || 1;
        return [r.left + sx / ratio, r.top + sy / ratio];
      },
      fitAll: () => {
        for (const s of sides) fitSide(s);
      },
      setCamera: (side, cam) => (side === 'left' ? L : R).renderer.setCamera(cam),
    };
    window.situcraftAlignment = hook;

    return {
      key,
      image: record,
      loadVersion,
      refresh: () => {
        readInUse();
        renderPanel();
      },
      dispose() {
        st.cameras = { left: L.renderer.camera, right: R.renderer.camera };
        observer.disconnect();
        for (const n of navigations) n.dispose();
        window.removeEventListener('keydown', onKey);
        L.renderer.destroy();
        R.renderer.destroy();
        if (window.situcraftAlignment === hook) delete window.situcraftAlignment;
      },
    };
  }

  show();
  return {
    update(topics) {
      if (topics.has('project-loaded') || topics.has('focus') || topics.has('bundles') || topics.has('project') || topics.has('alignment')) show();
    },
    destroy() {
      teardown();
      root.remove();
    },
  };
}
