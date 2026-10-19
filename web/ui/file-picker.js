// Choosing a place on this computer (browse.go): a page cannot learn where a file is (the browser's
// own file picker hands it a file's bytes, never its place), and SituCraft reads bundles where
// they are, so the program lists this computer's folders and this dialog walks them: the places to
// start from (the home folder, its Desktop, Documents and Downloads, the examples, the disks) and
// the folders lately used; the folder's crumbs; its contents, folders first, each bundle with its
// run and region; a path typed or pasted. What can be chosen depends on what is asked: what
// SituCraft opens (bundles, stores, archives, projects, images, tables, polygons; a folder opens
// what it holds), a file or a bundle, or a folder.

import { h, icon, clear } from './dom.js';
import { showDialog } from './overlays.js';
import { prefs } from './storage.js';

const KINDS = {
  folder: { label: 'Folder', icon: 'folder' },
  bundle: { label: 'Xenium bundle', icon: 'bundle' },
  zarr: { label: 'Zarr store', icon: 'zarr' },
  archive: { label: 'Zip archive', icon: 'file' },
  project: { label: 'SituCraft project', icon: 'save' },
  image: { label: 'Image', icon: 'image' },
  geometry: { label: 'Polygons', icon: 'polygon' },
  table: { label: 'Table', icon: 'table' },
  matrix: { label: 'Matrix', icon: 'grid' },
  file: { label: 'File', icon: 'file' },
};
const PLACE_ICONS = { home: 'folder', examples: 'sparkles', disk: 'database', folder: 'folder', recent: 'history' };
const RECENT = 'recentFolders';
const LAST = 'openFolder';

const size = (n) => {
  if (!Number.isFinite(n) || n <= 0) return '';
  const units = ['bytes', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let u = 0;
  while (v >= 1000 && u < units.length - 1) {
    v /= 1000;
    u += 1;
  }
  return u === 0 ? `${v} bytes` : `${v.toFixed(v < 10 ? 1 : 0)} ${units[u]}`;
};
const when = (iso) => (iso ? new Date(iso).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' }) : '');

// A bundle as its manifest names it: "Lung run · Region 1 · 11,898 cells".
function bundleWords(b) {
  if (!b) return '';
  return [b.run, b.region, Number.isFinite(b.cells) ? `${b.cells.toLocaleString('en-US')} cells` : ''].filter(Boolean).join(' · ');
}

// Whether an entry can be chosen: what SituCraft opens (open), a file or a bundle or store (file),
// or a plain folder (folder: never a bundle or a store, which SituCraft does not write into).
function choosable(entry, choose) {
  if (choose === 'folder') return entry.kind === 'folder';
  if (choose === 'file') return entry.kind !== 'folder';
  return entry.kind !== 'file';
}

// Whether double-clicking an entry walks into it rather than choosing it (→ walks into any folder).
const walksInto = (entry, choose) => entry.kind === 'folder' || (choose === 'folder' && entry.dir);

async function listing(path, hidden) {
  const params = new URLSearchParams();
  if (path) params.set('path', path);
  if (hidden) params.set('hidden', '1');
  const response = await fetch(`api/browse?${params}`, { cache: 'no-store' });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
  return body;
}

function remember(path) {
  prefs.set(LAST, path);
  const recent = prefs.get(RECENT, []).filter((p) => p !== path);
  prefs.set(RECENT, [path, ...recent].slice(0, 6));
}

// Shows the dialog; answers the paths chosen, or null when it is cancelled. options: title,
// confirm (the button's word), choose ('open', 'file' or 'folder'), multiple, start (a path: its
// folder is shown, the item chosen), hint; save (a file's name: the dialog chooses the folder it
// is written in, and answers the folder joined with the name as typed).
export function pickPaths({ title = 'Open', confirm = 'Open', choose = 'open', multiple = false, start = null, hint = '', save = null } = {}) {
  if (save !== null) {
    choose = 'folder';
    multiple = false;
  }
  return new Promise((resolve) => {
    const state = { answer: null, selected: new Set(), active: -1, anchor: -1, hidden: false, error: '', typed: '', seek: '', seekAt: 0 };
    let result = null;
    const places = h('nav.picker-places', { 'aria-label': 'Places' });
    const crumbs = h('nav.picker-crumbs', { 'aria-label': 'The folder shown' });
    const up = h('button.icon-button', { type: 'button', title: 'The folder above (Backspace)', 'aria-label': 'The folder above', onclick: () => state.answer?.parent && go(state.answer.parent) }, icon('chevronUp'));
    // The list has the focus first (the dialog focuses what is marked autofocus).
    const list = h('div.picker-list', { role: 'listbox', tabindex: '0', autofocus: true, 'aria-label': 'The folder\'s contents', 'aria-multiselectable': multiple ? 'true' : 'false' });
    const note = h('p.picker-note', { 'aria-live': 'polite' });
    // Beside the list (a listbox holds options only): a bundle's own folder said, and what the
    // list cannot show (reading, an error, nothing there, more than it lists).
    const banner = h('div.picker-inside', { hidden: true });
    const message = h('p.picker-empty', { role: 'status', hidden: true });
    const pathInput = h('input.input.picker-path', { type: 'text', spellcheck: false, autocomplete: 'off', 'aria-label': 'A folder or file by its path', placeholder: 'Type or paste a path, then Enter' });
    const hiddenBox = h('input', { type: 'checkbox', onchange: () => { state.hidden = hiddenBox.checked; go(state.answer?.path ?? '', { keepSelection: true }); } });
    const nameInput = save !== null ? h('input.input', { type: 'text', value: save, spellcheck: false, autocomplete: 'off', 'aria-label': 'The file\'s name', oninput: () => describe() }) : null;
    const { close, dialog } = showDialog({
      title,
      width: 'wide',
      content: [
        h('div.picker',
          nameInput ? h('label.field.picker-name-field', h('span', 'Name'), nameInput) : null,
          h('div.picker-bar', up, crumbs),
          h('div.picker-main', places, h('div.picker-pane', banner, list, message)),
          h('div.picker-type', pathInput, h('button.btn.small', { type: 'button', onclick: () => go(pathInput.value) }, 'Go'), h('label.check', hiddenBox, 'Show hidden files')),
          note,
          hint ? h('p.muted.picker-hint', hint) : null),
      ],
      buttons: [
        { label: 'Cancel', ghost: true },
        { label: confirm, primary: true, onClick: () => finish() },
      ],
      onClose: () => resolve(result),
    });
    const button = dialog.querySelector('.dialog-foot .btn.primary');
    const entries = () => state.answer?.entries ?? [];

    // What the button chooses: the entries chosen, else (where it can be) the folder shown.
    function chosen() {
      const picked = entries().filter((e) => state.selected.has(e.path) && choosable(e, choose));
      if (picked.length) return { paths: picked.map((e) => e.path), entries: picked };
      const a = state.answer;
      if (!a) return { paths: [], entries: [] };
      const here = { name: a.name, path: a.path, kind: a.kind || 'folder', bundle: a.bundle };
      // The folder shown opens what it holds when it holds something SituCraft opens itself (not
      // only folders: opening the home folder would look through all of it).
      const holds = a.kind || entries().some((e) => e.kind !== 'folder' && e.kind !== 'file');
      if ((choose === 'open' && holds) || (choose === 'folder' && !a.kind) || (choose === 'file' && (a.kind === 'bundle' || a.kind === 'zarr'))) return { paths: [a.path], entries: [here], folder: true };
      return { paths: [], entries: [] };
    }

    function finish() {
      const c = chosen();
      if (!c.paths.length) return false;
      if (state.answer) remember(state.answer.path);
      if (nameInput) {
        const name = nameInput.value.trim();
        if (!name) return false;
        const sep = state.answer?.separator ?? '/';
        result = [`${c.paths[0].replace(/[\\/]+$/, '')}${sep}${name}`];
        return true;
      }
      result = c.paths;
      return true;
    }

    function describe() {
      const c = chosen();
      const a = state.answer;
      if (nameInput) {
        const name = nameInput.value.trim();
        button.disabled = !c.paths.length || !name;
        button.textContent = confirm;
        note.textContent = c.paths.length && name ? `Written as ${name} in ${c.entries[0].name}.` : a?.kind ? 'SituCraft does not write into a bundle or a store: choose another folder.' : 'Name the file, and choose the folder it is written in.';
        return;
      }
      button.disabled = !c.paths.length;
      if (!c.paths.length) {
        button.textContent = confirm;
        note.textContent = choose === 'file' ? 'Choose a file.' : choose === 'folder' ? 'SituCraft does not write into a bundle or a store: choose another folder.' : 'Choose a bundle or a file, or go into the folder that holds them.';
        return;
      }
      if (c.folder) {
        const what = a.kind === 'bundle' ? 'this bundle' : a.kind === 'zarr' ? 'this store' : 'this folder';
        button.textContent = `${confirm} ${what}`;
        note.textContent = a.kind === 'bundle' ? `${a.name}: a Xenium bundle${a.bundle ? `, ${bundleWords(a.bundle)}` : ''}.`
          : choose === 'open' ? `${a.name}: SituCraft opens the bundles and files it holds.` : `${a.name}.`;
        return;
      }
      button.textContent = c.entries.length > 1 ? `${confirm} ${c.entries.length}` : confirm;
      if (c.entries.length > 1) note.textContent = `${c.entries.length} chosen: ${c.entries.slice(0, 4).map((e) => e.name).join(', ')}${c.entries.length > 4 ? ', …' : ''}.`;
      else {
        const e = c.entries[0];
        const kind = e.kind === 'bundle' ? 'a Xenium bundle' : e.kind === 'folder' ? 'a folder; SituCraft opens the bundles and files it holds' : (KINDS[e.kind]?.label ?? 'a file').toLowerCase();
        note.textContent = `${e.name}: ${kind}${e.bundle ? `, ${bundleWords(e.bundle)}` : ''}${e.bundle?.software ? ` (${e.bundle.software})` : ''}.${e.dir && e.kind !== 'folder' ? ' → looks inside.' : ''}`;
      }
    }

    function drawPlaces() {
      clear(places);
      const known = state.answer?.places ?? [];
      const row = (p, kind) => h(`button.picker-place${state.answer?.path === p.path ? '.current' : ''}`, { type: 'button', title: p.path, onclick: () => go(p.path) }, icon(PLACE_ICONS[kind ?? p.kind] ?? 'folder'), h('span', p.name));
      places.append(h('div.picker-heading', 'Places'), ...known.map((p) => row(p)));
      const recent = prefs.get(RECENT, []).filter((p) => !known.some((k) => k.path === p));
      if (recent.length) {
        const sep = state.answer?.separator ?? '/';
        places.append(h('div.picker-heading', 'Lately'), ...recent.map((p) => row({ name: p.split(sep).filter(Boolean).at(-1) || p, path: p }, 'recent')));
      }
    }

    // The crumbs: from the home folder (Home) when the folder is in it, else from the disk.
    function drawCrumbs() {
      clear(crumbs);
      const a = state.answer;
      if (!a) return;
      const sep = a.separator;
      let base = '';
      let parts;
      const inHome = a.home && (a.path === a.home || a.path.startsWith(a.home.endsWith(sep) ? a.home : a.home + sep));
      if (inHome) {
        base = a.home;
        parts = a.path.slice(a.home.length).split(sep).filter(Boolean);
        crumbs.append(h('button.picker-crumb', { type: 'button', title: a.home, onclick: () => go(a.home) }, 'Home'));
      } else {
        const root = sep === '\\' ? a.path.slice(0, a.path.indexOf('\\') + 1) : '/';
        base = root;
        parts = a.path.slice(root.length).split(sep).filter(Boolean);
        crumbs.append(h('button.picker-crumb', { type: 'button', title: root, onclick: () => go(root) }, sep === '\\' ? root.replace(/\\$/, '') : 'This computer'));
      }
      let at = base;
      parts.forEach((part, k) => {
        at = at.endsWith(sep) ? at + part : at + sep + part;
        const target = at;
        crumbs.append(h('span.picker-sep', { 'aria-hidden': 'true' }, '›'), h(`button.picker-crumb${k === parts.length - 1 ? '.current' : ''}`, { type: 'button', title: target, 'aria-current': k === parts.length - 1 ? 'location' : null, onclick: () => go(target) }, part));
      });
      up.disabled = !a.parent;
    }

    function say(text) {
      message.textContent = text;
      message.hidden = !text;
    }

    function drawList() {
      clear(list);
      list.removeAttribute('aria-activedescendant');
      const a = state.answer;
      banner.hidden = !(a && !state.error && a.kind === 'bundle');
      if (!banner.hidden) banner.replaceChildren(icon('bundle'), h('span', `${a.name} is a Xenium bundle${a.bundle ? `: ${bundleWords(a.bundle)}` : ''}.${choose === 'folder' ? '' : ` ${confirm} it, or choose one of its files.`}`));
      const shown = Boolean(a && !state.error && entries().length);
      list.hidden = !shown;
      if (state.error) say(state.error);
      else if (!a) say('Reading…');
      else if (!entries().length) say(state.hidden ? 'The folder is empty.' : 'Nothing here but hidden files, if any.');
      else say(a.more ? `${a.more.toLocaleString('en-US')} more not listed: type a path to go further.` : '');
      if (!shown) return;
      entries().forEach((e, k) => {
        const can = choosable(e, choose);
        const k2 = KINDS[e.kind] ?? KINDS.file;
        const row = h(`div.picker-row${can ? '' : '.dim'}${state.selected.has(e.path) ? '.selected' : ''}${k === state.active ? '.active' : ''}`, {
          role: 'option', id: `picker-row-${k}`, 'aria-selected': state.selected.has(e.path) ? 'true' : 'false', 'aria-disabled': can || e.kind === 'folder' ? null : 'true', title: e.path,
          onclick: (event) => click(k, event),
          ondblclick: () => open(k),
        },
        h('span.picker-icon', icon(k2.icon)),
        h('span.picker-name', e.name),
        h('span.picker-kind', e.bundle ? bundleWords(e.bundle) || k2.label : k2.label),
        h('span.picker-size', size(e.size)),
        h('span.picker-date', when(e.modified)));
        list.append(row);
      });
      if (state.active >= 0) {
        list.setAttribute('aria-activedescendant', `picker-row-${state.active}`);
        list.querySelector(`#picker-row-${state.active}`)?.scrollIntoView({ block: 'nearest' });
      }
    }

    function draw() {
      drawPlaces();
      drawCrumbs();
      drawList();
      describe();
    }

    async function go(path, { keepSelection = false } = {}) {
      const typed = String(path ?? '').trim();
      state.error = '';
      try {
        const answer = await listing(typed, state.hidden);
        const was = keepSelection ? state.selected : new Set();
        state.answer = answer;
        state.selected = new Set([...was].filter((p) => answer.entries.some((e) => e.path === p)));
        state.active = -1;
        if (answer.select) {
          const k = answer.entries.findIndex((e) => e.name === answer.select);
          if (k >= 0) {
            state.selected = new Set([answer.entries[k].path]);
            state.active = k;
          }
        }
        state.anchor = state.active;
        pathInput.value = answer.select ? `${answer.path}${answer.path.endsWith(answer.separator) ? '' : answer.separator}${answer.select}` : answer.path;
      } catch (error) {
        state.error = `${error.message.replace(/\.?$/, '.')}`;
      }
      draw();
      if (!dialog.contains(document.activeElement) || document.activeElement === document.body || (list.hidden && document.activeElement === list)) (list.hidden ? pathInput : list).focus();
      return !state.error;
    }

    function click(k, event) {
      const e = entries()[k];
      if (multiple && (event.metaKey || event.ctrlKey)) {
        if (state.selected.has(e.path)) state.selected.delete(e.path);
        else state.selected.add(e.path);
        state.anchor = k;
      } else if (multiple && event.shiftKey && state.anchor >= 0) {
        const [from, to] = [Math.min(state.anchor, k), Math.max(state.anchor, k)];
        state.selected = new Set(entries().slice(from, to + 1).map((x) => x.path));
      } else {
        state.selected = new Set([e.path]);
        state.anchor = k;
      }
      state.active = k;
      drawList();
      describe();
      list.focus();
    }

    // Opens an entry: walks into a folder, or chooses what can be chosen.
    function open(k) {
      const e = entries()[k];
      if (!e) return;
      if (walksInto(e, choose)) {
        go(e.path);
        return;
      }
      if (!choosable(e, choose)) return;
      state.selected = new Set([e.path]);
      if (finish()) close();
    }

    function move(k, extend) {
      const n = entries().length;
      if (!n) return;
      const next = Math.max(0, Math.min(n - 1, k));
      state.active = next;
      if (extend && multiple && state.anchor >= 0) {
        const [from, to] = [Math.min(state.anchor, next), Math.max(state.anchor, next)];
        state.selected = new Set(entries().slice(from, to + 1).map((x) => x.path));
      } else {
        state.selected = new Set([entries()[next].path]);
        state.anchor = next;
      }
      drawList();
      describe();
    }

    list.addEventListener('keydown', (event) => {
      const n = entries().length;
      if (event.key === 'ArrowDown') move(state.active + 1, event.shiftKey);
      else if (event.key === 'ArrowUp' && event.altKey) state.answer?.parent && go(state.answer.parent);
      else if (event.key === 'ArrowUp') move(state.active < 0 ? n - 1 : state.active - 1, event.shiftKey);
      else if (event.key === 'Home') move(0, event.shiftKey);
      else if (event.key === 'End') move(n - 1, event.shiftKey);
      else if (event.key === 'Enter' && !(event.metaKey || event.ctrlKey)) {
        if (state.active >= 0 && state.selected.size <= 1) open(state.active);
        else if (finish()) close();
      } else if (event.key === 'Backspace' || event.key === 'ArrowLeft') {
        if (state.answer?.parent) go(state.answer.parent);
      } else if (event.key === 'ArrowRight') {
        const e = entries()[state.active];
        if (e?.dir) go(e.path);
      } else if ((event.key === 'a' || event.key === 'A') && (event.metaKey || event.ctrlKey) && multiple) {
        state.selected = new Set(entries().filter((e) => choosable(e, choose)).map((e) => e.path));
        drawList();
        describe();
      } else if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
        // Typing a name's first letters goes to it.
        const now = Date.now();
        state.seek = now - state.seekAt < 800 ? state.seek + event.key.toLowerCase() : event.key.toLowerCase();
        state.seekAt = now;
        const k = entries().findIndex((e) => e.name.toLowerCase().startsWith(state.seek));
        if (k >= 0) move(k, false);
      } else return;
      event.preventDefault();
    });
    pathInput.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') {
        // Handled here: Enter left to the dialog would press its button.
        event.preventDefault();
        event.stopPropagation();
        go(pathInput.value);
      }
    });

    draw();
    // At the place asked for, else where the dialog was last used, else the home folder.
    const first = start || prefs.get(LAST, '');
    if (nameInput) setTimeout(() => nameInput.select(), 40);
    (async () => {
      // A place that is gone (a bundle that moved): the nearest folder above it that is there.
      let at = first;
      for (let k = 0; k < 12 && !(await go(at)) && at; k += 1) {
        const above = at.replace(/[\\/]+$/, '').replace(/[\\/][^\\/]*$/, '');
        at = above && above !== at ? above : '';
      }
    })();
  });
}

// A Browse… button for a path field: the dialog opens at the field's path, and what is chosen is
// put in the field (its input and change events fired, as typing would). options as pickPaths's;
// save may be a function of the field's value (the name it suggests).
export function browseButton(input, options = {}) {
  return h('button.btn.small.picker-browse', {
    type: 'button',
    title: 'Choose it on this computer',
    onclick: async () => {
      const value = input.value.trim();
      const save = typeof options.save === 'function' ? options.save(value) : options.save;
      const paths = await pickPaths({ confirm: 'Choose', choose: 'file', ...options, save: save ?? null, start: value || options.start || null });
      if (!paths?.length) return;
      input.value = paths[0];
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
      input.focus();
    },
  }, icon('folder'), 'Browse…');
}

// A path's input with a Browse… button beside it, for where the input was. The input keeps its
// own name (name, when it has none), so that a label around both names the input alone.
export function pathField(input, { name = null, ...options } = {}) {
  if (name && !input.getAttribute('aria-label')) input.setAttribute('aria-label', name);
  return h('div.picker-field', input, browseButton(input, options));
}
