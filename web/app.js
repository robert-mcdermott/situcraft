// SituCraft: application bootstrap, views, shortcuts, opening data and autosave. Adapted from
// MaveScape 0.8.0 web/app.js (situcraft-spec/design.md, "The host, from the siblings").

import { h, icon, clear, debounce, isTyping, downloadBlob, modKey } from './ui/dom.js';
import { showMenu, showDialog, promptDialog, confirmDialog, toast, closeMenu } from './ui/overlays.js';
import { createStore } from './ui/store.js';
import { createLibrary, detectBackend, openPaths, prefs } from './ui/storage.js';
import { mountSidebar } from './ui/sidebar.js';
import { mountInspector } from './ui/inspector.js';
import { mountDrawer } from './ui/drawer.js';
import { plannedMode } from './ui/mode-planned.js';
import { openPalette } from './ui/palette.js';
import { WorkerClient } from './ui/workers.js';
import { createBundles } from './ui/bundles.js';
import { createJobs } from './ui/jobs.js';
import { createExamples } from './ui/examples.js';
import { colorVisionFriendly, setColorVisionFriendly } from './lib/colormaps.js';
import { createProject, isEmptyProject, parseProject, rename, serializeProject } from './lib/project.js';
import { parseViewLink } from './lib/views.js';

const VERSION = '0.1.0';

const toOpen = { label: 'Open a bundle', icon: 'bundle', run: (app) => app.openPathDialog() };

// The views, in the order of the work: data in, see it, check it, segment, then biology. Each
// loads its module when first shown; views still to be built say what they are for
// (mode-planned.js) until the slice that builds them (situcraft-spec/roadmap.md).
const MODES = [
  { id: 'welcome', label: 'Start', icon: 'grid', hidden: true, load: () => import('./ui/mode-welcome.js').then((m) => m.mountWelcome) },
  { id: 'data', label: 'Data', icon: 'database', load: () => import('./ui/mode-data.js').then((m) => m.mountData) },
  { id: 'viewer', label: 'Viewer', icon: 'tissue', load: () => import('./ui/mode-viewer.js').then((m) => m.mountViewer) },
  { id: 'qc', label: 'QC', icon: 'qc', load: () => import('./ui/mode-qc.js').then((m) => m.mountQC) },
  { id: 'segmentation', label: 'Segmentation', icon: 'segment', load: () => import('./ui/mode-segmentation.js').then((m) => m.mountSegmentation) },
  { id: 'compare', label: 'Compare', icon: 'compare', load: () => import('./ui/mode-compare.js').then((m) => m.mountCompare) },
  'sep',
  {
    id: 'celltypes', label: 'Cell types', icon: 'tag',
    load: async () => plannedMode({
      title: 'Cell types', icon: 'tag',
      purpose: 'Cell identities with their evidence: clusters, marker-based labels at several levels (class, lineage, subtype, state), imported labels and mapping to a reference, each with its confidence and the markers for and against it. Predicted, curated and consensus labels stay side by side.',
      steps: ['Promote a segmentation.', 'Cluster, score markers or import labels.', 'Review the cells with low confidence.'],
    }),
  },
  {
    id: 'spatial', label: 'Spatial', icon: 'graph',
    load: async () => plannedMode({
      title: 'Spatial analysis', icon: 'graph',
      purpose: 'Questions about tissue organization: which cell types are neighbors more often than chance, which enter or stay outside a region, what recurring neighborhoods (niches) the tissue has, and which genes follow space. Every result shows the cells it comes from, and can be re-run on another segmentation to test it.',
      steps: ['Promote a segmentation and a label set.', 'Choose a question and a neighborhood definition.', 'Run, and open the contributing cells.'],
    }),
  },
  {
    id: 'cohort', label: 'Cohort', icon: 'samples',
    load: async () => plannedMode({
      title: 'Cohort', icon: 'samples',
      purpose: 'Samples compared with the sample or patient, not the cell, as the unit: composition, niches and expression across conditions, with effect sizes and intervals, once the experimental design names its replicates.',
      steps: ['Add the samples\' datasets to the project.', 'Describe the design: samples, patients, conditions, batches.', 'Compare, with corrections recorded.'],
    }),
  },
  {
    id: 'reports', label: 'Reports', icon: 'report',
    load: async () => plannedMode({
      title: 'Reports', icon: 'report',
      purpose: 'Results others can check: figures with scale bars and legends, methods written from what was actually done, citations of every method used, and exports in open formats (SpatialData Zarr, AnnData Zarr, Parquet, GeoJSON) that carry their provenance.',
      steps: ['Analyze.', 'Choose the views, figures and results to report.', 'Export.'],
    }),
  },
  { id: 'jobs', label: 'Jobs', icon: 'jobs', load: () => import('./ui/mode-jobs.js').then((m) => m.mountJobs) },
];

// --- Theme ---------------------------------------------------------------------------------------

function applyTheme(preference) {
  const dark = preference === 'dark' || (preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
}

// Color-vision-friendly colors (a setting): the palettes (colormaps.js) and status colors.
function applyColorVision(on) {
  setColorVisionFriendly(on);
  if (on) document.documentElement.dataset.cvd = '';
  else delete document.documentElement.dataset.cvd;
}

// The kind of a file by its name: the same table as the program's (local.go, fileKind).
export function fileKind(name) {
  const lower = name.toLowerCase();
  const has = (...suffixes) => suffixes.some((s) => lower.endsWith(s));
  if (lower === 'experiment.xenium') return 'bundle';
  if (has('.scz')) return 'project';
  if (has('.zarr.zip')) return 'zarr';
  if (has('.zip')) return 'archive';
  if (has('.ome.tif', '.ome.tiff', '.tif', '.tiff', '.ome.btf')) return 'image';
  if (has('.geojson', '.geojson.gz')) return 'geometry';
  if (has('.parquet', '.csv', '.csv.gz', '.tsv', '.tsv.gz')) return 'table';
  if (has('.mtx', '.mtx.gz')) return 'matrix';
  return '';
}

const KIND_NAMES = {
  bundle: 'Xenium output bundles', project: 'project archives (.scz)', zarr: 'Zarr stores', archive: 'archives',
  image: 'images', geometry: 'polygons (GeoJSON)', table: 'tables', matrix: 'matrices',
};

// Without its program (the page opened from a plain web server or a file), SituCraft cannot reach
// the data on this computer: say so and how to start it.
function showNeedsProgram() {
  document.getElementById('workbench').append(h('div.workbench-scroll', h('div.empty',
    icon('terminal'),
    h('h3', 'Start SituCraft with its program'),
    h('p', 'SituCraft reads bundles where they are on this computer, which a web page alone cannot do. Run the situcraft program (it opens this window itself), for example:'),
    h('pre.mono', 'situcraft /path/to/Xenium_output_bundle'))));
}

async function start() {
  const themePref = prefs.get('theme', 'system');
  applyTheme(themePref);
  applyColorVision(prefs.get('colorVision', false));
  const info = await detectBackend();
  if (!info) {
    showNeedsProgram();
    return;
  }
  const library = createLibrary(info);

  // SituCraft starts on the start page with a new, empty project: the last one is a click away in
  // its recent projects.
  const store = createStore(createProject());
  store.state.ui.theme = themePref;
  store.state.ui.drawer = { open: prefs.get('drawerOpen', false), tab: prefs.get('drawerTab', 'history') };
  const app = { store, library, info, version: VERSION, commit: info.commit ?? '' };
  // What was handed to the program (command line, a later launch, Open a bundle): local.go's list.
  app.localItems = info.files ?? [];
  app.workers = {};
  app.worker = (name) => {
    app.workers[name] ??= new WorkerClient(`../workers/${name}-worker.js`, { max: 1 });
    return app.workers[name];
  };
  // Readers of files opened in the browser by kind, registered by the slices that build them:
  // kind → async (item).
  app.importers = new Map();
  // A project archive picked in the browser would lose where it is, and so its bundles beside it.
  app.importers.set('project', async () => {
    throw new Error('a project archive opens by its path (Open a bundle), so that the bundles beside it are found');
  });

  app.jobs = createJobs(app);
  app.bundles = createBundles(app);
  app.examples = createExamples(app);
  // Remote control (situcraft --remote-control): programs on this computer drive this window.
  if (info.remoteControl) import('./ui/remote.js').then((m) => { app.remote = m.installRemote(app); });
  app.sidebar = mountSidebar(app);
  app.inspector = mountInspector(app);
  app.drawer = mountDrawer(app);
  app.log = (message) => app.drawer.log(message);

  // --- Views -------------------------------------------------------------------------------------

  const workbench = document.getElementById('workbench');
  const rail = document.getElementById('view-rail');
  let current = null;
  let currentId = null;
  let mounting = 0;

  function renderRail() {
    clear(rail);
    for (const mode of MODES) {
      if (mode === 'sep') {
        // Drawn only: a tab list holds tabs alone.
        rail.append(h('span.rail-sep', { 'aria-hidden': 'true' }));
        continue;
      }
      if (mode.hidden) continue;
      const active = store.ui.mode === mode.id;
      rail.append(h(`button.rail-tab${active ? '.active' : ''}`, { type: 'button', role: 'tab', 'aria-selected': active ? 'true' : 'false', title: mode.label, onclick: () => app.setMode(mode.id) }, icon(mode.icon), h('span', mode.label)));
    }
  }

  app.setMode = async (id) => {
    if (id === currentId && current) return;
    const mode = MODES.find((m) => m !== 'sep' && m.id === id) ?? MODES[0];
    const token = ++mounting;
    store.setUI({ mode: mode.id }, ['mode']);
    document.getElementById('app').dataset.mode = mode.id;
    renderRail();
    current?.destroy();
    current = null;
    currentId = mode.id;
    clear(workbench);
    let mount;
    try {
      mount = await mode.load();
    } catch (error) {
      if (token !== mounting) return;
      workbench.append(h('div.workbench-scroll', h('div.empty', icon(mode.icon), h('h3', `${mode.label} is not available in this build`), h('p', error.message))));
      return;
    }
    if (token !== mounting) return;
    current = mount(app, workbench);
    current.update?.(new Set(['project', 'focus', 'selection', 'mode']));
  };

  app.focusItem = (focus) => store.setUI({ focus }, ['focus']);

  app.toggleDrawer = (open = !store.ui.drawer.open, tab = store.ui.drawer.tab) => {
    store.setUI({ drawer: { open, tab } }, ['drawer']);
    prefs.set('drawerOpen', open);
    prefs.set('drawerTab', tab);
  };

  // --- Data on this computer ---------------------------------------------------------------------

  // The program's list of what it was handed, after a later launch or Open a bundle added to it.
  function setLocalItems(files) {
    const before = new Set(app.localItems.map((f) => f.url));
    app.localItems = files ?? [];
    const added = app.localItems.filter((f) => !before.has(f.url));
    for (const item of added) app.log(`Opened ${item.name} (${KIND_NAMES[item.kind] ?? item.kind}).`);
    if (added.length) store.notify(['local']);
    return added;
  }
  app.setLocalItems = setLocalItems;

  // Shows a bundle handed to the program in the Data view, which checks it.
  function showBundle(item) {
    app.focusItem({ kind: 'local', id: item.url });
    app.setMode('data');
  }

  // A browser does not tell a page where a folder is, so a bundle is named by its path (or on the
  // situcraft command line); the program reads it where it is.
  app.openPathDialog = async () => {
    const path = await promptDialog({
      title: 'Open a bundle',
      label: 'Path on this computer',
      placeholder: '/data/Xenium_V1_human_Lung_2fov_outs',
      hint: 'A Xenium output bundle (its folder, its .zip or its experiment.xenium), an image, a table, polygons, or a folder of them. SituCraft reads it where it is and never changes it.',
      confirm: 'Open',
    });
    if (!path) return;
    try {
      const answer = await openPaths([path]);
      const added = setLocalItems(answer.files);
      for (const problem of answer.problems ?? []) {
        toast(problem, { kind: 'error' });
        app.log(problem);
      }
      const archive = added.find((item) => item.kind === 'project');
      const bundle = added.find((item) => item.kind === 'bundle' || item.kind === 'archive');
      if (archive) {
        await openArchive(archive);
      } else if (bundle) {
        showBundle(bundle);
      } else if (added.length) {
        toast(`Opened ${added.length === 1 ? added[0].name : `${added.length} items`}; they are listed under On this computer.`, { timeout: 4000 });
        app.focusItem({ kind: 'local', id: added[0].url });
      } else {
        // Already open: show it.
        const known = app.localItems.find((item) => item.path === path || item.path === path.replace(/\/+$/, ''));
        if (known?.kind === 'project') await openArchive(known);
        else if (known && (known.kind === 'bundle' || known.kind === 'archive')) showBundle(known);
        else if (!(answer.problems ?? []).length) toast('Already open.', { timeout: 1800 });
      }
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  };

  // --- Files opened in the browser ---------------------------------------------------------------

  const fileInput = document.getElementById('file-input');
  app.pickFiles = (accept) => {
    fileInput.accept = accept ?? '';
    fileInput.value = '';
    fileInput.click();
  };
  fileInput.addEventListener('change', () => app.importFiles([...fileInput.files]));

  // Opens dropped or picked files by kind. A bundle cannot be read through the browser (its
  // files are many and large, and the page never learns where they are): it is opened by path.
  app.importFiles = async (files) => {
    if (files.some((file) => file.name.toLowerCase() === 'experiment.xenium')) {
      const message = 'That looks like a Xenium output bundle. Bundles are read where they are, by their path: use Open a bundle, or name it when starting situcraft.';
      toast(message, { kind: 'error', timeout: 6000 });
      app.log(message);
      return;
    }
    const unread = new Map();
    const unknown = [];
    for (const file of files) {
      const kind = fileKind(file.name);
      const importer = app.importers.get(kind);
      if (!importer) {
        if (kind) unread.set(kind, (unread.get(kind) ?? 0) + 1);
        else unknown.push(file.name);
        continue;
      }
      try {
        await importer({ file, name: file.name, folder: file.folder ?? null });
      } catch (error) {
        toast(`${file.name}: ${error.message}`, { kind: 'error' });
        app.log(`${file.name}: ${error.message}`);
      }
    }
    const messages = [];
    if (unread.size) messages.push(`This build of SituCraft cannot read ${[...unread.keys()].map((k) => KIND_NAMES[k] ?? k).join(', ')} yet.`);
    if (unknown.length) messages.push(`${unknown.length === 1 ? unknown[0] : `${unknown.length} files`}: SituCraft opens Xenium output bundles, images (.ome.tif), tables (.parquet, .csv), polygons (.geojson), Zarr stores and projects (.scz).`);
    for (const message of messages) {
      toast(message, { kind: 'error' });
      app.log(message);
    }
  };

  // --- Projects ----------------------------------------------------------------------------------

  async function loadProject(doc) {
    store.reset(doc);
    prefs.set('lastProject', doc.id);
    store.setUI({ focus: null, selection: null }, ['focus', 'selection']);
    app.setMode(isEmptyProject(doc) ? 'welcome' : 'data');
    updateTitle();
    // Each dataset's bundle, where the project recorded it.
    app.bundles.reconnect();
  }
  app.loadProject = loadProject;

  app.openProject = async (id) => {
    try {
      await saveNow();
      await loadProject(parseProject(await library.loadProject(id)));
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  };

  const saveState = document.getElementById('save-state');
  // The save under way, if any. A save asked for meanwhile (opening another project) waits for
  // it, then saves what changed since: returning at once would lose those edits.
  let saving = null;
  async function writeProject(project) {
    saveState.className = 'save-state saving';
    saveState.setAttribute('aria-label', 'Saving');
    try {
      await library.saveProject(project.id, serializeProject(project));
      prefs.set('lastProject', project.id);
      // Another project may have been opened while this one was written.
      if (store.project.id === project.id) store.markSaved(project);
      saveState.className = 'save-state';
      saveState.title = `Saved to ${library.location}`;
      saveState.setAttribute('aria-label', 'Saved');
    } catch (error) {
      saveState.className = 'save-state error';
      saveState.title = `Not saved: ${error.message}`;
      saveState.setAttribute('aria-label', 'Not saved');
    }
  }
  async function saveNow() {
    while (saving) await saving;
    const { project } = store;
    if (!store.isDirty() || isEmptyProject(project)) return;
    saving = writeProject(project);
    try {
      await saving;
    } finally {
      saving = null;
    }
    if (store.isDirty()) autosave();
  }
  app.saveNow = saveNow;
  const autosave = debounce(saveNow, 1200);

  // Opens a saved view: its dataset in its view (the Viewer or QC), which applies it when it has
  // the dataset open (app.pendingView). A view not kept in the project (a tour's step) focuses its
  // dataset.
  app.applyView = (view) => {
    if (!view) return;
    if (!store.project.datasets.some((d) => d.id === view.dataset)) {
      toast(`The view ${view.name} is of a dataset this project no longer has.`, { kind: 'error' });
      return;
    }
    app.pendingView = view;
    const kept = store.project.views.some((v) => v.id === view.id);
    app.focusItem(kept ? { kind: 'view', id: view.id } : { kind: 'dataset', id: view.dataset });
    if (store.ui.mode === view.mode) store.notify(['view-pending']);
    else app.setMode(view.mode);
  };

  // A deep link (#project=…&view=…) opens its project from the library and shows the view.
  async function openLink(hash) {
    const link = parseViewLink(hash);
    if (!link) return false;
    try {
      if (store.project.id !== link.project) {
        await saveNow();
        await loadProject(parseProject(await library.loadProject(link.project)));
      }
      const view = store.project.views.find((v) => v.id === link.view);
      if (!view) throw new Error('The project has no such view (it may have been removed).');
      app.applyView(view);
      return true;
    } catch (error) {
      toast(`The link could not be opened: ${error.message}`, { kind: 'error' });
      return false;
    }
  }
  window.addEventListener('hashchange', () => openLink(location.hash));

  app.newProject = async () => {
    await saveNow();
    await loadProject(createProject());
  };

  async function openLibraryDialog() {
    let list = [];
    try {
      list = await library.listProjects();
    } catch (error) {
      toast(error.message, { kind: 'error' });
      return;
    }
    const body = h('tbody');
    const dialog = showDialog({
      title: 'Open a project',
      width: 'wide',
      content: [h('p', `Projects in ${library.location}.`), h('div', { style: { maxHeight: '60vh', overflow: 'auto' } }, h('table.data', h('thead', h('tr', h('th', 'Name'), h('th.r', 'Datasets'), h('th', 'Modified'), h('th', h('span.sr-only', 'Delete')))), body))],
    });
    for (const item of list) {
      body.append(h(`tr${item.id === store.project.id ? '.selected' : ''}`, { style: { cursor: 'pointer' }, onclick: () => { dialog.close(); app.openProject(item.id); } },
        h('td', item.name || 'Untitled'), h('td.r', String(item.datasets ?? '')), h('td', new Date(item.modified).toLocaleString()),
        h('td.r', h('button.icon-button.small', {
          type: 'button',
          title: 'Delete',
          'aria-label': `Delete ${item.name || 'Untitled'}`,
          onclick: async (event) => {
            event.stopPropagation();
            if (!(await confirmDialog({ title: 'Delete project?', message: `Delete "${item.name}"? It is moved to the library's trash folder. Its bundles are not touched.`, confirm: 'Delete', danger: true }))) return;
            try {
              await library.deleteProject(item.id);
              event.target.closest('tr').remove();
              store.notify(['library']);
            } catch (error) {
              toast(error.message, { kind: 'error' });
            }
          },
        }, icon('trash')))));
    }
    if (!list.length) body.append(h('tr', h('td', { colSpan: 4 }, h('span.muted', 'No saved projects yet.'))));
  }
  app.openLibrary = openLibraryDialog;

  // A project archive (.scz) handed to the program: its project opens, its datasets found beside
  // it first (api_project.go). An archive whose project is already in the library opens as a copy,
  // so that neither replaces the other.
  async function openArchive(item) {
    try {
      const response = await fetch(`${item.url}/project`, { cache: 'no-store' });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
      let doc = parseProject(JSON.stringify(body.project));
      const known = (await library.listProjects().catch(() => [])).some((p) => p.id === doc.id);
      if (known) doc = { ...doc, id: createProject().id, name: `${doc.name} (from ${item.name})` };
      await saveNow();
      await loadProject(doc);
      store.commit({ ...store.project, history: [...store.project.history, { time: new Date().toISOString(), action: 'archive', detail: `Opened from ${item.path}` }] }, 'Open project archive');
      toast(`Opened ${item.name}${known ? ' as a copy (its project is already in the library)' : ''}.`, { kind: 'ok' });
    } catch (error) {
      toast(`${item.name}: ${error.message}`, { kind: 'error' });
    }
  }
  app.openArchive = openArchive;

  // Writes the project as an archive where the user says (by default beside its first bundle).
  async function exportArchive() {
    const { project } = store;
    const first = project.datasets[0]?.path;
    const dir = first ? first.replace(/[\\/]+$/, '').replace(/[\\/][^\\/]*$/, '') : library.location;
    const name = project.name.replace(/[^\w.-]+/g, '_') || 'project';
    const path = await promptDialog({
      title: 'Save a project archive',
      label: 'Path of the archive (.scz)',
      value: `${dir}/${name}.scz`,
      hint: 'Written on this computer: the project (datasets, filters, saved views, history), not the bundles. Each bundle is also recorded by its place relative to the archive, so a folder holding both can be moved or shared.',
      confirm: 'Save',
    });
    if (!path) return;
    const post = (overwrite) => fetch('/api/project-archive', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path, project, overwrite }) });
    try {
      let response = await post(false);
      if (response.status === 409) {
        if (!(await confirmDialog({ title: 'Replace the archive?', message: `${path} already exists.`, confirm: 'Replace', danger: true }))) return;
        response = await post(true);
      }
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
      toast(`Saved ${body.path}.`, { kind: 'ok' });
      app.log(`Project archive written: ${body.path} (${body.bytes} bytes).`);
    } catch (error) {
      toast(`The archive was not written: ${error.message}`, { kind: 'error' });
    }
  }

  // The project document as JSON.
  function exportProjectJSON() {
    downloadBlob(new Blob([serializeProject(store.project)], { type: 'application/json' }), `${store.project.name.replace(/[^\w.-]+/g, '_') || 'project'}.situcraft.json`);
  }

  document.getElementById('project-menu').addEventListener('click', (event) => {
    showMenu(event.currentTarget, [
      { label: 'New project', icon: 'plus', onSelect: () => app.newProject() },
      { label: 'Open…', icon: 'library', hint: `${modKey}⇧O`, onSelect: openLibraryDialog },
      { label: 'Rename…', icon: 'edit', onSelect: async () => {
        const name = await promptDialog({ title: 'Rename project', label: 'Name', value: store.project.name });
        if (name) store.commit(rename(store.project, name), 'Rename project');
      } },
      { label: 'Save now', icon: 'save', hint: `${modKey}S`, onSelect: async () => { await saveNow(); toast('Saved.', { kind: 'ok' }); } },
      '-',
      { section: 'Open' },
      { label: 'A bundle…', icon: 'bundle', hint: `${modKey}O`, onSelect: () => app.openPathDialog() },
      { label: 'Files…', icon: 'file', onSelect: () => app.pickFiles() },
      '-',
      { section: 'Export' },
      { label: 'Project archive (.scz)…', icon: 'save', onSelect: exportArchive },
      { label: 'Project document (JSON)', icon: 'download', onSelect: exportProjectJSON },
      '-',
      { label: 'Start page', icon: 'grid', onSelect: () => app.setMode('welcome') },
    ]);
  });

  // --- Commands (palette) ------------------------------------------------------------------------

  app.commands = () => [
    ...MODES.filter((m) => m !== 'sep').map((m) => ({ label: `Go to ${m.label}`, icon: m.icon, run: () => app.setMode(m.id), keywords: m.id })),
    { label: 'Open a bundle', icon: 'bundle', hint: `${modKey}O`, run: () => app.openPathDialog(), keywords: 'xenium output folder path import' },
    { label: 'Open files', icon: 'file', run: () => app.pickFiles() },
    { label: 'Example datasets (download, open, take a tour)', icon: 'download', run: () => app.setMode('welcome'), keywords: 'example demo tour lung breast tutorial' },
    { label: 'New project', icon: 'plus', run: () => app.newProject() },
    { label: 'Open a saved project', icon: 'library', hint: `${modKey}⇧O`, run: openLibraryDialog },
    { label: 'Save project now', icon: 'save', hint: `${modKey}S`, run: saveNow },
    { label: 'Export the project document', icon: 'download', run: exportProjectJSON },
    { label: 'Show or hide the drawer (history, log)', icon: 'drawer', hint: `${modKey}J`, run: () => app.toggleDrawer(), keywords: 'history log undo' },
    { label: 'Toggle dark theme', icon: 'moon', run: () => toggleTheme() },
    { label: 'Color-vision-friendly colors (on or off)', icon: 'eye', run: () => toggleColorVision(), keywords: 'color blind accessibility deuteranopia protanopia palette' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: showHelp },
  ];

  // --- Top bar -----------------------------------------------------------------------------------

  const undoButton = document.getElementById('undo-button');
  const redoButton = document.getElementById('redo-button');
  const themeButton = document.getElementById('theme-button');
  undoButton.append(icon('undo'));
  redoButton.append(icon('redo'));
  document.getElementById('help-button').append(icon('help'));
  document.getElementById('help-button').addEventListener('click', showHelp);
  document.getElementById('command-button').addEventListener('click', () => openPalette(app));
  undoButton.addEventListener('click', () => doUndo());
  redoButton.addEventListener('click', () => doRedo());
  themeButton.addEventListener('click', () => showMenu(themeButton, [
    { section: 'Appearance' },
    ...[['light', 'Light', 'sun'], ['dark', 'Dark', 'moon'], ['system', 'Match the system', 'settings']].map(([id, label, glyph]) => ({ label, icon: glyph, checked: prefs.get('theme', 'system') === id, onSelect: () => setTheme(id) })),
    '-',
    { label: 'Color-vision-friendly colors', icon: 'eye', checked: colorVisionFriendly(), onSelect: () => toggleColorVision() },
  ]));

  function doUndo() {
    const label = store.undo();
    if (label) toast(`Undid: ${label}`, { timeout: 1800 });
  }
  function doRedo() {
    const label = store.redo();
    if (label) toast(`Redid: ${label}`, { timeout: 1800 });
  }

  function toggleTheme() {
    setTheme(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark');
  }
  function toggleColorVision() {
    const next = !colorVisionFriendly();
    prefs.set('colorVision', next);
    applyColorVision(next);
    store.notify(['theme', 'colors']);
    toast(next ? 'Color-vision-friendly colors: palettes and status colors stay distinguishable with red–green and blue–yellow color blindness.' : 'Default colors.', { timeout: 3500 });
  }
  function setTheme(next) {
    prefs.set('theme', next);
    store.state.ui.theme = next;
    applyTheme(next);
    renderTheme();
    store.notify(['theme']);
  }
  function renderTheme() {
    clear(themeButton).append(icon(document.documentElement.dataset.theme === 'dark' ? 'sun' : 'moon'));
  }
  renderTheme();
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (prefs.get('theme', 'system') === 'system') {
      applyTheme('system');
      renderTheme();
      store.notify(['theme']);
    }
  });

  function updateTitle() {
    const { project } = store;
    document.getElementById('project-name').textContent = project.name;
    document.title = `${project.name} · SituCraft`;
    undoButton.disabled = !store.canUndo();
    redoButton.disabled = !store.canRedo();
    undoButton.title = store.canUndo() ? `Undo ${store.undoLabel()} (${modKey}Z)` : 'Nothing to undo';
    redoButton.title = store.canRedo() ? `Redo ${store.redoLabel()} (⇧${modKey}Z)` : 'Nothing to redo';
    if (!saveState.classList.contains('error') && !saveState.classList.contains('saving')) {
      const dirty = store.isDirty() && !isEmptyProject(project);
      saveState.className = `save-state${dirty ? ' dirty' : ''}`;
      saveState.setAttribute('aria-label', dirty ? 'Unsaved changes' : 'Saved');
    }
  }

  // --- Status bar --------------------------------------------------------------------------------

  const statusbar = document.getElementById('statusbar');
  // The status bar is redrawn when what it shows changes; a running job's progress is updated in
  // place, so that clicking it is not lost to a redraw.
  let statusShape = '';
  let jobLabel = null;
  function renderStatus() {
    const busy = [...store.state.busy.values()];
    const job = busy.length ? null : app.jobs.running()[0];
    const jobText = job ? `${job.title}: ${job.state === 'queued' ? 'waiting' : `${Math.round(job.progress * 100)}%`}` : '';
    const { project } = store;
    const shape = JSON.stringify([busy[0] ?? '', job?.id ?? '', library.location, project.datasets.length, store.ui.drawer.open, app.commit]);
    if (shape === statusShape) {
      if (jobLabel) {
        jobLabel.textContent = jobText;
        jobLabel.parentElement.title = job?.message ?? '';
      }
      return;
    }
    statusShape = shape;
    jobLabel = null;
    clear(statusbar);
    const version = `SituCraft ${VERSION}${app.commit ? ` (${app.commit.slice(0, 7)})` : ''}`;
    let state = h('span.item', h(`span.dot${busy.length ? '.busy' : ''}`), busy.length ? busy[0] : 'Ready');
    if (job) {
      // A running job: what it is doing, and how far it has got.
      jobLabel = h('span', jobText);
      state = h('button.item.statusbar-link', { type: 'button', title: job.message ?? '', onclick: () => app.setMode('jobs') }, h('span.dot.busy'), jobLabel);
    }
    statusbar.append(
      state,
      h('span.item', icon('library'), `Library: ${library.location}`),
      h('span.item', `${project.datasets.length} dataset${project.datasets.length === 1 ? '' : 's'}`),
      h('button.item.statusbar-link', { type: 'button', title: `History and log (${modKey}J)`, onclick: () => app.toggleDrawer() }, icon('drawer'), store.ui.drawer.open ? 'Hide the drawer' : 'History and log'),
      h('span.spacer'),
      h('span.item', version),
      h('span.item.muted', 'Research use only'));
  }

  function showHelp() {
    const rows = [
      ['Search and commands', `${modKey}K`],
      ['Undo / redo', `${modKey}Z / ⇧${modKey}Z`],
      ['Open a bundle', `${modKey}O`],
      ['Open a saved project', `⇧${modKey}O`],
      ['Save now', `${modKey}S`],
      ['Go to a view', `${modKey}1 … ${modKey}9`],
      ['Toggle project tree / inspector', `${modKey}\\ / ${modKey}⇧\\`],
      ['Drawer (history, log)', `${modKey}J`],
      ['Viewer: pan', 'Drag, or the arrow keys'],
      ['Viewer: zoom', 'Scroll or pinch; + and −; double-click'],
      ['Viewer: the whole tissue', '0'],
      ['Viewer: select a cell; clear', 'Click; Esc'],
      ['Close a menu or dialog', 'Esc'],
    ];
    showDialog({
      title: 'Keyboard shortcuts',
      content: h('table.data', h('tbody', ...rows.map(([action, keys]) => h('tr', h('td', action), h('td.r', h('kbd', keys)))))),
    });
  }

  // --- Keyboard ----------------------------------------------------------------------------------

  document.addEventListener('keydown', (event) => {
    const mod = event.metaKey || event.ctrlKey;
    const key = event.key.toLowerCase();
    if (mod && key === 'k') {
      event.preventDefault();
      openPalette(app);
      return;
    }
    if (isTyping(event)) return;
    if (mod && key === 'z') {
      event.preventDefault();
      if (event.shiftKey) doRedo();
      else doUndo();
      return;
    }
    if (mod && key === 'y') {
      event.preventDefault();
      doRedo();
      return;
    }
    if (mod && key === 's') {
      event.preventDefault();
      saveNow().then(() => toast('Saved.', { kind: 'ok', timeout: 1500 }));
      return;
    }
    if (mod && key === 'o') {
      event.preventDefault();
      if (event.shiftKey) openLibraryDialog();
      else app.openPathDialog();
      return;
    }
    if (mod && key === 'j') {
      event.preventDefault();
      app.toggleDrawer();
      return;
    }
    if (mod && event.key === '\\') {
      event.preventDefault();
      document.getElementById('app').classList.toggle(event.shiftKey ? 'inspector-closed' : 'sidebar-closed');
      store.notify(['layout']);
      return;
    }
    if (mod && /^[1-9]$/.test(event.key)) {
      const modes = MODES.filter((m) => m !== 'sep' && !m.hidden);
      const mode = modes[Number(event.key) - 1];
      if (mode) {
        event.preventDefault();
        app.setMode(mode.id);
      }
      return;
    }
    if (mod || event.altKey) return;
    if (event.key === '?') {
      showHelp();
      return;
    }
    if (event.key === 'Escape') closeMenu();
  });

  // --- Drag and drop -----------------------------------------------------------------------------

  const dropOverlay = document.getElementById('drop-overlay');
  let dragDepth = 0;
  const hasFiles = (event) => [...(event.dataTransfer?.types ?? [])].includes('Files');
  window.addEventListener('dragenter', (event) => {
    if (!hasFiles(event)) return;
    dragDepth += 1;
    dropOverlay.hidden = false;
  });
  window.addEventListener('dragleave', (event) => {
    if (!hasFiles(event)) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) dropOverlay.hidden = true;
  });
  window.addEventListener('dragover', (event) => {
    if (hasFiles(event)) event.preventDefault();
  });
  window.addEventListener('drop', async (event) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    dragDepth = 0;
    dropOverlay.hidden = true;
    app.importFiles(await collectDropped(event.dataTransfer));
  });

  // Walks dropped folders one level deep (Chromium and Safari support webkitGetAsEntry): enough to
  // recognize a bundle by its experiment.xenium without reading a large folder.
  async function collectDropped(transfer) {
    const entries = [...transfer.items].map((item) => item.webkitGetAsEntry?.()).filter(Boolean);
    if (!entries.length) return [...transfer.files];
    const out = [];
    for (const entry of entries) {
      if (entry.isFile) {
        out.push(await new Promise((resolve, reject) => entry.file(resolve, reject)));
      } else if (entry.isDirectory) {
        const batch = await new Promise((resolve, reject) => entry.createReader().readEntries(resolve, reject));
        for (const child of batch) {
          if (!child.isFile || child.name.startsWith('.')) continue;
          const file = await new Promise((resolve, reject) => child.file(resolve, reject));
          Object.defineProperty(file, 'folder', { value: entry.name });
          out.push(file);
        }
      }
    }
    return out;
  }

  // --- Store subscription ------------------------------------------------------------------------

  store.subscribe((topics) => {
    if (topics.has('project') || topics.has('history') || topics.has('saved')) updateTitle();
    if (topics.has('project')) autosave();
    app.sidebar.update(topics);
    app.inspector.update(topics);
    app.drawer.update(topics);
    current?.update?.(topics);
    renderStatus();
  });

  window.addEventListener('beforeunload', (event) => {
    if (store.isDirty() && !isEmptyProject(store.project)) {
      saveNow();
      event.preventDefault();
    }
    // A job under way: closing the window asks first (SituCraft keeps running until the job ends,
    // and an interrupted job can be resumed, but the user should know).
    if (app.jobs.running().length) event.preventDefault();
  });
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') saveNow();
  });

  // --- Start -------------------------------------------------------------------------------------

  renderRail();
  updateTitle();
  renderStatus();
  app.sidebar.render();
  app.inspector.render();
  app.drawer.render();
  await app.setMode('welcome');
  if (location.hash && (await openLink(location.hash))) {
    // Opened at a saved view.
  } else if (app.localItems.length) {
    app.log(`${app.localItems.length} item${app.localItems.length === 1 ? '' : 's'} named on the command line.`);
    const archive = app.localItems.find((item) => item.kind === 'project');
    const bundle = app.bundles.items()[0];
    if (archive) await openArchive(archive);
    else if (bundle) showBundle(bundle);
  }

  // A second "situcraft <paths>" launch hands its paths to the program and brings this window
  // forward, so checking on focus (and now and then) is enough.
  let checking = false;
  const check = async () => {
    if (checking || document.visibilityState !== 'visible') return;
    checking = true;
    try {
      const fresh = await detectBackend();
      if (fresh) {
        const added = setLocalItems(fresh.files);
        const archive = added.find((item) => item.kind === 'project');
        const bundle = added.find((item) => item.kind === 'bundle' || item.kind === 'archive');
        if (archive) await openArchive(archive);
        else if (bundle) showBundle(bundle);
      }
    } finally {
      checking = false;
    }
  };
  window.addEventListener('focus', check);
  setInterval(check, 30000);
  window.situcraft = app;
}

start().catch((error) => {
  console.error(error);
  document.getElementById('workbench').append(h('div.empty', h('h3', 'SituCraft could not start'), h('p', error.message)));
});
