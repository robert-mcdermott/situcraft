// The inspector: what is in focus (a dataset, a segmentation, a cell), described in full. Views add
// their own sections through app.inspector.setSection(id, builder) as they are built; until
// something is in focus it describes the project and says how to begin. Adapted from MaveScape
// 0.8.0 web/ui/inspector.js.

import { h, icon, relativeTime, formatBytes } from './dom.js';
import { describeGraph } from '../lib/graphs.js';
import { promptDialog, toast } from './overlays.js';
import { removeView, renameView, viewLink } from '../lib/views.js';
import { promotedSegmentation } from '../lib/segmentations.js';
import { KIND_ICONS, KIND_LABELS } from './sidebar.js';
import { askRemoveComparison } from './comparisons.js';
import { describeLabels, markerSetFile, markerSetVersions } from '../lib/labels.js';
import { signed } from '../lib/expression.js';
import { RESULT_KINDS, itemOf } from '../lib/record.js';

export function mountInspector(app) {
  const root = document.getElementById('inspector');
  // Sections contributed by views: id → (app) => Node | null.
  const contributed = new Map();

  function projectSection() {
    const { project } = app.store;
    const segmentations = project.artifacts.filter((a) => a.kind === 'segmentation').length;
    const kv = h('dl.kv',
      h('dt', 'Name'), h('dd', project.name),
      h('dt', 'Datasets'), h('dd', String(project.datasets.length)),
      h('dt', 'Segmentations'), h('dd', String(segmentations)),
      h('dt', 'Saved views'), h('dd', String(project.views.length)),
      h('dt', 'Modified'), h('dd', relativeTime(project.modified)));
    return h('section.inspector-section', h('h3', icon('library'), 'Project'), kv);
  }

  // An item handed to the program on this computer: what SituCraft takes it to be.
  function localSection(item) {
    return h('section.inspector-section',
      h('h3', icon(KIND_ICONS[item.kind] ?? 'file'), item.name),
      h('dl.kv',
        h('dt', 'Kind'), h('dd', KIND_LABELS[item.kind] ?? item.kind),
        item.folder ? [h('dt', 'Found in'), h('dd', item.folder)] : null,
        item.dir ? null : [h('dt', 'Size'), h('dd', formatBytes(item.size))]),
      h('p.muted', { style: { margin: '8px 0 0', fontSize: '12px' } }, item.dir
        ? 'Read where it is: SituCraft never copies, changes or moves a bundle.'
        : 'Read on this computer; nothing is uploaded.'));
  }

  // A dataset of the project: what it is, where, and how it validated.
  function datasetSection(d) {
    const v = d.validation ?? {};
    const state = app.bundles.link(d.id)?.state;
    const where = { ok: 'found where recorded', checking: 'looking for it', missing: 'not found where recorded', changed: 'changed since added', 'different-run': 'another run is there' }[state] ?? '';
    const item = app.bundles.link(d.id)?.item;
    const job = item ? app.bundles.cacheJob(item) : null;
    const cached = item ? app.bundles.cache(item.path)?.status : null;
    let derived = '—';
    if (job) derived = job.state === 'queued' ? 'waiting to build' : `building, ${Math.round(job.progress * 100)}%`;
    else if (cached?.ready) derived = `ready (${formatBytes(cached.entries.reduce((n, e) => n + (e.size ?? 0), 0))})`;
    else if (cached) {
      const buildable = cached.entries.filter((e) => e.state !== 'unavailable');
      const built = buildable.filter((e) => e.state === 'ready').length;
      derived = built ? `${built} of ${buildable.length} built` : 'not built';
    }
    return h('section.inspector-section',
      h('h3', icon('bundle'), d.name),
      h('dl.kv',
        h('dt', 'Platform'), h('dd', v.support === 'simulated' ? `Simulated, in ${d.platform}'s format` : `${d.platform} (XOA ${d.version})`),
        h('dt', 'Panel'), h('dd', d.panel || '—'),
        h('dt', 'Tissue'), h('dd', [d.organism, d.tissue].filter(Boolean).join(' ') || '—'),
        h('dt', 'Cells'), h('dd', Number(d.cells).toLocaleString('en-US')),
        h('dt', 'Transcripts'), h('dd', Number(d.transcripts).toLocaleString('en-US')),
        h('dt', 'Channels'), h('dd', String(d.channels?.length ?? 0)),
        h('dt', 'Checked'), h('dd', `${v.fatal ?? 0} fatal, ${v.warnings ?? 0} warnings, ${v.info ?? 0} notes`),
        h('dt', 'Bundle'), h('dd', where),
        h('dt', 'Derived data'), h('dd', derived)),
      h('p.muted.mono', { style: { margin: '8px 0 0', fontSize: '11px', wordBreak: 'break-all' } }, d.path));
  }

  // A filter version: its conditions and the cells it keeps.
  function filterSection(f) {
    const dataset = app.store.project.datasets.find((d) => d.id === f.dataset);
    return h('section.inspector-section',
      h('h3', icon('filter'), `${f.name} · version ${f.version}`),
      h('dl.kv',
        h('dt', 'Dataset'), h('dd', dataset?.name ?? '—'),
        h('dt', 'Keeps'), h('dd', `${f.result.kept.toLocaleString('en-US')} of ${f.result.cells.toLocaleString('en-US')} cells`),
        h('dt', 'Conditions'), h('dd', String(f.conditions.length)),
        h('dt', 'Saved'), h('dd', relativeTime(f.created))),
      h('p.muted.mono', { style: { margin: '8px 0 0', fontSize: '10.5px', wordBreak: 'break-all' } }, `SHA-256 of the cells kept: ${f.result.sha256}`));
  }

  // A saved view: what it shows, its link, and what can be done with it.
  function viewSection(v) {
    const dataset = app.store.project.datasets.find((d) => d.id === v.dataset);
    const link = viewLink(location.href, app.store.project.id, v.id);
    return h('section.inspector-section',
      h('h3', icon('eye'), v.name),
      h('dl.kv',
        h('dt', 'Dataset'), h('dd', dataset?.name ?? '—'),
        h('dt', 'In'), h('dd', v.mode === 'qc' ? 'QC' : 'Viewer'),
        h('dt', 'Zoom'), h('dd', `${v.camera.umPerCss.toPrecision(3)} µm per pixel`),
        h('dt', 'Genes'), h('dd', v.transcripts?.all ? 'all transcripts' : v.transcripts?.genes?.join(', ') || '—'),
        v.qc ? [h('dt', 'Map'), h('dd', v.qc.map)] : null,
        h('dt', 'Saved'), h('dd', relativeTime(v.updated ?? v.created))),
      h('div.btn-row', { style: { marginTop: '10px' } },
        h('button.btn.small.primary', { type: 'button', onclick: () => app.applyView(v) }, icon('eye'), 'Open'),
        h('button.btn.small', { type: 'button', title: link, onclick: async () => {
          try {
            await navigator.clipboard.writeText(link);
            toast('Link copied: it opens this view while SituCraft runs on this computer.', { kind: 'ok' });
          } catch {
            toast(link);
          }
        } }, icon('link'), 'Copy link'),
        h('button.btn.small.ghost', { type: 'button', onclick: async () => {
          const name = await promptDialog({ title: 'Rename view', label: 'Name', value: v.name });
          if (name) app.store.commit(renameView(app.store.project, v.id, name), 'Rename view');
        } }, icon('edit'), 'Rename'),
        h('button.btn.small.ghost.danger', { type: 'button', onclick: () => {
          app.store.commit(removeView(app.store.project, v.id), `Remove view ${v.name}`);
          app.focusItem(null);
        } }, icon('trash'), 'Remove')));
  }

  // A segmentation: what made it (method, software, the job), what it was made from, its files by
  // SHA-256, what its check found, and whether it is its dataset's default (and why).
  function segmentationSection(a) {
    const { project } = app.store;
    const dataset = project.datasets.find((d) => d.id === a.dataset);
    const promoted = promotedSegmentation(project, a.dataset);
    const isDefault = promoted?.record.id === a.id;
    const short = (sum) => (sum ? `${sum.slice(0, 12)}…` : '—');
    const severity = { fatal: 'danger', warning: 'warn', info: 'accent' };
    return h('section.inspector-section.segmentation',
      h('h3', icon('segment'), a.name),
      h('dl.kv',
        h('dt', 'Dataset'), h('dd', dataset?.name ?? a.dataset),
        h('dt', 'State'), h('dd', h(`span.badge.${a.state === 'complete' ? 'ok' : 'danger'}`, a.state === 'complete' ? 'Complete' : a.state)),
        h('dt', 'Default'), h('dd', isDefault ? 'yes' : 'no'),
        h('dt', 'Method'), h('dd', a.method.name || '—'),
        a.method.version ? [h('dt', 'Version'), h('dd', a.method.version)] : null,
        Number.isFinite(a.summary?.cells) ? [h('dt', 'Cells'), h('dd', a.summary.cells.toLocaleString('en-US'))] : null,
        Number.isFinite(a.summary?.assignedFraction) ? [h('dt', 'Assigned'), h('dd', `${(100 * a.summary.assignedFraction).toFixed(1)}% of gene transcripts (Q20)`)] : null,
        h('dt', 'Artifact'), h('dd.mono', a.id),
        a.job ? [h('dt', 'Made by'), h('dd', h('button.link', { type: 'button', title: 'Show the job in the Jobs view', onclick: () => { app.jobsFocus = a.job.id; app.setMode('jobs'); } }, `job ${a.job.id}`))] : null,
        h('dt', 'Software'), h('dd', a.software.map((x) => `${x.name} ${x.version}`).join('; '))),
      isDefault && promoted.decision ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '12px' } }, `Default since ${new Date(promoted.decision.time).toLocaleString()}, by ${promoted.decision.by}: ${promoted.decision.rationale}`) : null,
      a.method.methods?.length ? h('div.seg-methods', h('div.muted', 'Cells by method'), ...a.method.methods.map((m) => h('div.seg-method', h('span', m.method.replace(/^Segmented /, '')), h('span.mono', m.cells.toLocaleString('en-US'))))) : null,
      a.sources?.length ? h('div.seg-files', h('div.muted', 'Imported from (by SHA-256 of the files read)'),
        ...a.sources.map((f) => h('div.seg-file', { title: `${f.path}: ${f.files} files read, SHA-256 of their checksums ${f.sha256}` }, h('div', f.path.split(/[\\/]/).pop()), h('div.muted', `${f.files === 1 ? '1 file' : `${f.files.toLocaleString('en-US')} files`}, ${formatBytes(f.size)} · `, h('span.mono', short(f.sha256)))))) : null,
      h('div.seg-files', h('div.muted', a.files.some((f) => f.location === 'bundle') ? 'Files (in the bundle, by SHA-256)' : 'Files (by SHA-256)'),
        ...a.files.map((f) => h('div.seg-file', { title: `${f.path}: SHA-256 ${f.sha256}` }, h('div', f.path), h('div.muted', f.role, ' · ', h('span.mono', short(f.sha256)))))),
      a.findings.length ? h('div.seg-findings', h('div.muted', 'Its check found'),
        ...a.findings.map((f) => h('div.seg-finding', h(`span.badge.${severity[f.severity] ?? ''}`, f.severity), h('span', `${f.message}${f.count ? ` (${f.count.toLocaleString('en-US')})` : ''}`)))) : h('p.muted', { style: { fontSize: '12px' } }, 'Its check found nothing to report.'),
      a.method.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, a.method.citation) : null,
      a.state === 'complete' ? h('div.btn-row', { style: { marginTop: '10px' } }, h('button.btn.small', { type: 'button', onclick: () => app.showOnTissue(a.dataset, a.id) }, icon('tissue'), 'Show on the tissue')) : null);
  }

  // A comparison: its two segmentations, its headline numbers, and the workspace.
  function comparisonSection(c) {
    const { project } = app.store;
    const name = (id) => project.artifacts.find((a) => a.kind === 'segmentation' && a.id === id)?.name ?? id;
    const s = c.summary ?? {};
    const pct = (v) => (Number.isFinite(v) ? `${(100 * v).toFixed(2)}%` : '—');
    return h('section.inspector-section',
      h('h3', icon('compare'), 'Comparison'),
      h('dl.kv',
        h('dt', 'A'), h('dd', name(c.a)),
        h('dt', 'B'), h('dd', name(c.b)),
        h('dt', 'One-to-one'), h('dd', (s.classes?.['one-to-one'] ?? 0).toLocaleString('en-US')),
        h('dt', 'Pixels agree'), h('dd', pct(s.pixelAgreement)),
        h('dt', 'Changed cell'), h('dd', pct(s.changedFraction)),
        h('dt', 'Artifact'), h('dd.mono', c.id)),
      h('div.btn-row', { style: { marginTop: '8px' } }, h('button.btn.small.primary', { type: 'button', onclick: () => {
        app.compareFocus = c.id;
        app.setMode('compare');
      } }, icon('eye'), 'Open in the workspace'),
      h('button.btn.small.ghost.danger', { type: 'button', title: 'Remove it from this project (the segmentations stay)', onclick: () => askRemoveComparison(app, c) }, icon('trash'), 'Remove')),
      c.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, c.method.citation) : null);
  }

  // A clustering: its segmentation, its numbers, how it was made, and the tissue.
  function clusteringSection(c) {
    const { project } = app.store;
    const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === c.segmentation);
    const s = c.summary ?? {};
    const p = c.parameters ?? {};
    return h('section.inspector-section',
      h('h3', icon('dots'), 'Clustering'),
      h('dl.kv',
        h('dt', 'Segmentation'), h('dd', seg?.name ?? c.segmentation),
        h('dt', 'Cells'), h('dd', (s.cells ?? 0).toLocaleString('en-US')),
        h('dt', 'Analyzed'), h('dd', s.filter ? `filter ${s.filter.name}` : `≥ ${p.minTranscripts} gene transcripts`),
        h('dt', 'Clusters'), h('dd', String(s.clusters ?? '—')),
        h('dt', 'Resolution'), h('dd', String(p.resolution ?? '—')),
        h('dt', 'Neighbors'), h('dd', `${p.neighbors} in ${s.components ?? p.components} components`),
        h('dt', 'Modularity'), h('dd', Number.isFinite(s.modularity) ? s.modularity.toFixed(4) : '—'),
        h('dt', 'Seed'), h('dd', String(p.seed ?? '—')),
        h('dt', 'Method'), h('dd', `${c.method?.name ?? ''} ${c.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', c.id)),
      c.state === 'complete' ? h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small.primary', { type: 'button', onclick: () => app.showClusters(c) }, icon('tissue'), 'Show on the tissue'),
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'clustering', id: c.id });
          app.setMode('celltypes');
        } }, icon('dots'), 'Its clusters')) : null,
      c.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, c.method.citation) : null);
  }

  // A graph: its segmentation, definition, the cells it was built over, and what it holds.
  function graphSection(g) {
    const { project } = app.store;
    const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === g.segmentation);
    const s = g.summary ?? {};
    const choice = s.choice ?? {};
    const over = [choice.filter ? `filter ${choice.filter.name}` : '', choice.regions?.length ? `in ${choice.regions.map((r) => r.name).join(', ')}` : '', choice.excluded ? `${choice.excluded.toLocaleString('en-US')} excluded by review left out` : ''].filter(Boolean).join('; ');
    return h('section.inspector-section',
      h('h3', icon('graph'), 'Graph'),
      h('dl.kv',
        h('dt', 'Segmentation'), h('dd', seg?.name ?? g.segmentation),
        h('dt', 'Neighbors'), h('dd', describeGraph(g.parameters)),
        h('dt', 'Cells'), h('dd', (s.cells ?? 0).toLocaleString('en-US'), over ? h('span.muted', ` (${over})`) : null),
        h('dt', 'Edges'), h('dd', (s.edges ?? 0).toLocaleString('en-US')),
        h('dt', 'Mean degree'), h('dd', Number.isFinite(s.meanDegree) ? s.meanDegree.toFixed(2) : '—'),
        h('dt', 'Without one'), h('dd', `${(s.isolated ?? 0).toLocaleString('en-US')} cells`),
        h('dt', 'Median edge'), h('dd', Number.isFinite(s.medianLength) ? `${s.medianLength.toFixed(1)} µm` : '—'),
        h('dt', 'Method'), h('dd', `${g.method?.name ?? ''} ${g.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', g.id)),
      g.state === 'complete' ? h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small.primary', { type: 'button', onclick: () => app.showGraph(g) }, icon('tissue'), 'Show on the tissue'),
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'graph', id: g.id });
          app.setMode('spatial');
        } }, icon('graph'), 'In the Spatial view')) : null);
  }

  // A cohort summary: what it is of (its samples, labels and genes, the niches across them) and how
  // it was made.
  function summarySection(r) {
    const s = r.summary ?? {};
    const samples = s.samples ?? [];
    const set = app.store.project.artifacts.find((a) => a.kind === 'marker-set' && a.id === r.markerSet);
    return h('section.inspector-section',
      h('h3', icon('table'), 'Cohort summary'),
      h('dl.kv',
        h('dt', 'Samples'), h('dd', `${samples.length}: ${samples.map((x) => x.name).join(', ')}`),
        h('dt', 'Cells'), h('dd', `${samples.reduce((n, x) => n + (x.labeled ?? 0), 0).toLocaleString('en-US')} labeled of ${samples.reduce((n, x) => n + (x.cells ?? 0), 0).toLocaleString('en-US')}`),
        h('dt', 'Labels'), h('dd', `${s.labels?.name ?? '—'} (${s.labels?.names?.length ?? '?'})`, set ? h('span.muted', ` · marker set v${set.version}`) : null),
        h('dt', 'Genes'), h('dd', (s.genes ?? 0).toLocaleString('en-US')),
        h('dt', 'Niches'), h('dd', s.niches ? `${s.niches}, found across the samples together` : 'none'),
        h('dt', 'Method'), h('dd', `${r.method?.name ?? ''} ${r.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', r.id)),
      h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'cohort-summary', id: r.id });
          app.setMode('cohort');
        } }, icon('table'), 'In the Cohort view')),
      r.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, r.method.citation) : null);
  }

  // A comparison of abundance: what it compared and what it found.
  function abundanceSection(r) {
    const s = r.summaryOf ?? {};
    const summary = app.store.project.artifacts.find((a) => a.kind === 'cohort-summary' && a.id === r.summary);
    return h('section.inspector-section',
      h('h3', icon('compare'), 'Differential abundance'),
      h('dl.kv',
        h('dt', 'Contrasts'), h('dd', r.name.replace(/^Abundance compared: /, '')),
        h('dt', 'Of'), h('dd', summary?.name ?? r.summary ?? '—'),
        h('dt', 'Design'), h('dd', r.design ? `${r.design.name}, version ${r.design.version}` : 'the draft, unsaved'),
        h('dt', 'Tests'), h('dd', `${s.tests ?? '?'}${s.untested ? `, ${s.untested} untested` : ''}`),
        h('dt', 'Found'), h('dd', (s.found ?? []).length ? s.found.map((f) => `${f.label} ${f.ratio.toFixed(2)}× (${f.family}, adjusted p ${f.padj < 1e-4 ? f.padj.toExponential(1) : f.padj.toFixed(4)})`).join('; ') : 'none below an adjusted p of 0.05'),
        h('dt', 'Method'), h('dd', `${r.method?.name ?? ''} ${r.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', r.id)),
      h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'abundance', id: r.id });
          app.setMode('cohort');
        } }, icon('compare'), 'In the Cohort view')),
      r.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, r.method.citation) : null);
  }

  // A comparison of expression or of spatial statistics: what it compared and what it found.
  function testSection(r) {
    const s = r.summaryOf ?? {};
    const summary = app.store.project.artifacts.find((a) => a.kind === 'cohort-summary' && a.id === r.summary);
    const expression = r.kind === 'expression';
    const q = (v) => (v < 1e-4 ? v.toExponential(1) : v.toFixed(4));
    const found = expression
      ? (s.found ?? []).map((f) => `${f.gene} in ${f.label} ${signed(f.logFC)} log2 (adjusted p ${q(f.padj)})`)
      : (s.found ?? []).map((f) => `${f.key} ${f.ratio ? `${f.ratio.toFixed(2)}×` : signed(f.estimate, 3)} (${f.family}, adjusted p ${q(f.padj)})`);
    const below = expression ? s.below ?? 0 : found.length;
    return h('section.inspector-section',
      h('h3', icon('compare'), expression ? 'Differential expression' : 'Spatial statistics compared'),
      h('dl.kv',
        h('dt', 'Contrasts'), h('dd', r.name.replace(/^(Expression|Spatial statistics) compared: /, '')),
        h('dt', 'Of'), h('dd', summary?.name ?? r.summary ?? '—'),
        h('dt', 'Design'), h('dd', r.design ? `${r.design.name}, version ${r.design.version}` : 'the draft, unsaved'),
        h('dt', 'Tests'), h('dd', `${s.tests ?? '?'}${expression && s.untested?.length ? `; ${s.untested.join(', ')} untested` : !expression && s.untested ? `, ${s.untested} untested` : ''}`),
        h('dt', 'Found'), h('dd', found.length ? `${found.join('; ')}${below > found.length ? `; ${below - found.length} more` : ''}` : 'none below an adjusted p of 0.05'),
        h('dt', 'Method'), h('dd', `${r.method?.name ?? ''} ${r.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', r.id)),
      h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: r.kind, id: r.id });
          app.setMode('cohort');
        } }, icon('compare'), 'In the Cohort view')),
      r.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, r.method.citation) : null);
  }

  // A spatial analysis: its question, labels, what it is of, and what it found.
  function analysisSection(r) {
    const { project } = app.store;
    const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === r.segmentation);
    const s = r.summary ?? {};
    const pair = (p) => `${p.a} – ${p.b} ${p.ratio.toFixed(2)}×`;
    return h('section.inspector-section',
      h('h3', icon('graph'), { enrichment: 'Neighborhood enrichment', cooccurrence: 'Co-occurrence', distances: 'Nearest distances', ripley: 'Ripley\'s K and L', regions: 'Regions compared', boundary: 'Across a region\'s edge', niches: 'Cellular neighborhoods', moran: 'Spatially variable genes' }[r.question] ?? r.question),
      h('dl.kv',
        h('dt', 'Segmentation'), h('dd', seg?.name ?? r.segmentation),
        r.question === 'moran' ? [h('dt', 'Genes'), h('dd', `${(s.genes ?? 0).toLocaleString('en-US')}, over ${describeGraph(s.graph?.parameters)}`)] : [h('dt', 'Labels'), h('dd', `${s.labels?.name ?? '—'} (${s.labels?.count ?? '?'})`)],
        r.question === 'enrichment' ? [h('dt', 'Graph'), h('dd', describeGraph(s.graph?.parameters))] : r.question === 'cooccurrence' ? [h('dt', 'Distances'), h('dd', String(s.distances ?? '—'))] : null,
        s.rows?.length ? [h('dt', 'Regions'), h('dd', s.rows.map((x) => x.name).join(', '))] : null,
        s.edge ? [h('dt', 'Edge'), h('dd', `of ${s.edge.name}`)] : null,
        s.window ? [h('dt', 'Window'), h('dd', s.window.kind === 'region' ? `the region ${s.window.region?.name ?? ''}` : `the tissue, ${Math.round(s.window.area).toLocaleString('en-US')} µm²`)] : null,
        h('dt', 'Cells'), h('dd', `${(s.labeled ?? 0).toLocaleString('en-US')} labeled of ${(s.cells ?? 0).toLocaleString('en-US')}`),
        s.strata?.length ? [h('dt', 'Dealt within'), h('dd', s.strata.map((x) => x.name).join(', '))] : null,
        r.question === 'regions' ? [h('dt', 'Densities that differ'), h('dd', String(s.significant ?? 0))] : r.question === 'boundary' ? [h('dt', 'Labels that differ'), h('dd', String(s.significant ?? 0))] : r.question === 'moran' ? [h('dt', 'Genes that differ'), h('dd', String(s.significant ?? 0))] : r.question !== 'cooccurrence' && r.question !== 'niches' ? [h('dt', 'Pairs that differ'), h('dd', String(s.significant ?? 0))] : null,
        s.denser?.length ? [h('dt', 'Denser'), h('dd', s.denser.map((x) => `${x.label} in ${x.region} ${x.ratio === null ? '∞' : x.ratio.toFixed(2)}×`).join('; '))] : null,
        s.sparser?.length ? [h('dt', 'Sparser'), h('dd', s.sparser.map((x) => `${x.label} in ${x.region} ${x.ratio.toFixed(2)}×`).join('; '))] : null,
        s.fill?.length ? [h('dt', 'Fill it'), h('dd', s.fill.join(', '))] : null,
        s.avoid?.length ? [h('dt', 'Stay out'), h('dd', s.avoid.join(', '))] : null,
        s.gather?.length ? [h('dt', 'At its margin'), h('dd', s.gather.join(', '))] : null,
        s.niches?.length ? [h('dt', 'Niches'), h('dd', s.niches.map((x, v) => `${v + 1} · ${x.defining.join(' + ')} (${x.cells.toLocaleString('en-US')})`).join('; '))] : null,
        Number.isFinite(s.agreementMin) ? [h('dt', 'Stability'), h('dd', `other seeds agree at ${s.agreementMin.toFixed(2)} or more (adjusted Rand index)`)] : null,
        s.patterned?.length ? [h('dt', 'In a pattern'), h('dd', s.patterned.map((x) => `${x.gene} ${x.i.toFixed(2)}`).join('; '))] : null,
        s.dispersed?.length ? [h('dt', 'Dispersed'), h('dd', s.dispersed.map((x) => `${x.gene} ${x.i.toFixed(2)}`).join('; '))] : null,
        s.enriched?.length ? [h('dt', 'Most'), h('dd', s.enriched.map(pair).join('; '))] : null,
        s.depleted?.length ? [h('dt', 'Least'), h('dd', s.depleted.map(pair).join('; '))] : null,
        s.nearer?.length ? [h('dt', 'Nearer'), h('dd', s.nearer.map((p) => `${p.a} → ${p.b} ${p.ratio.toFixed(2)}×`).join('; '))] : null,
        s.farther?.length ? [h('dt', 'Farther'), h('dd', s.farther.map((p) => `${p.a} → ${p.b} ${p.ratio.toFixed(2)}×`).join('; '))] : null,
        s.above?.length ? [h('dt', 'Gathered'), h('dd', s.above.map((c) => `${c.a === c.b ? c.a : `${c.a} → ${c.b}`} at ${c.radius} µm`).join('; '))] : null,
        s.below?.length ? [h('dt', 'Apart'), h('dd', s.below.map((c) => `${c.a === c.b ? c.a : `${c.a} → ${c.b}`} at ${c.radius} µm`).join('; '))] : null,
        h('dt', 'Method'), h('dd', `${r.method?.name ?? ''} ${r.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', r.id)),
      h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'spatial', id: r.id });
          app.setMode('spatial');
        } }, icon('graph'), 'In the Spatial view')),
      r.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, r.method.citation) : null);
  }

  // A label set: its clustering, marker set, cells labeled at each level, and parameters.
  function labelsSection(r) {
    const { project } = app.store;
    const c = project.artifacts.find((a) => a.kind === 'clustering' && a.id === r.clustering);
    const ms = project.artifacts.find((a) => a.kind === 'marker-set' && a.id === r.markerSet);
    const s = r.summary ?? {};
    const p = r.parameters ?? {};
    const pct = (v) => (s.cells ? `${Math.round((100 * v) / s.cells)}%` : '—');
    const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === r.segmentation);
    const src = r.source;
    return h('section.inspector-section',
      h('h3', icon('tag'), 'Label set'),
      h('dl.kv',
        ...(src ? [
          h('dt', 'Segmentation'), h('dd', seg?.name ?? r.segmentation),
          h('dt', 'Imported from'), h('dd', src.format === 'instrument' ? `${src.tool} ${src.version}` : src.file),
          h('dt', 'Column'), h('dd', src.format === 'instrument' ? src.title : src.column),
          h('dt', 'Matched by'), h('dd', src.idColumn),
          h('dt', 'SHA-256'), h('dd.mono', String(src.sha256 ?? '').slice(0, 16)),
        ] : r.reference ? [
          h('dt', 'Clustering'), h('dd', c?.name ?? r.clustering),
          h('dt', 'Reference'), h('dd', `${r.reference.file}${r.reference.labelColumn ? ` (${r.reference.labelColumn})` : ''}`),
          h('dt', 'Genes shared'), h('dd', `${s.reference?.shared ?? '?'} (${s.reference?.missing ?? '?'} of the panel's not in it)`),
          h('dt', 'SHA-256'), h('dd.mono', String(r.reference.sha256 ?? '').slice(0, 16)),
        ] : [
          h('dt', 'Clustering'), h('dd', c?.name ?? r.clustering),
          h('dt', 'Marker set'), h('dd', `${r.markers?.name ?? '?'}${ms ? ` (version ${ms.version})` : ''}`),
          h('dt', 'Checksum'), h('dd.mono', (r.markers?.checksum ?? '').slice(0, 16)),
        ]),
        h('dt', 'Cells'), h('dd', (s.cells ?? 0).toLocaleString('en-US')),
        ...(s.levels ?? []).flatMap((level, l) => [h('dt', level), h('dd', `${(s.labeled?.[l] ?? 0).toLocaleString('en-US')} (${pct(s.labeled?.[l] ?? 0)})`)]),
        src ? null : h('dt', 'Margin'), src ? null : h('dd', r.reference ? `${p.margin ?? '—'} (fine-tuning ${p.tune ?? '—'})` : `${p.margin ?? '—'} (minimum ${p.minimum ?? '—'})`),
        h('dt', 'Method'), h('dd', `${r.method?.name ?? ''} ${r.method?.version ?? ''}`.trim()),
        h('dt', 'Artifact'), h('dd.mono', r.id)),
      r.state === 'complete' ? h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small.primary', { type: 'button', onclick: () => app.showLabels(r, 0) }, icon('tissue'), 'Show on the tissue'),
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'labels', id: r.id });
          app.setMode('celltypes');
        } }, icon('tag'), 'Its labels')) : null,
      h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, describeLabels(r)),
      r.method?.citation ? h('p.muted', { style: { margin: '8px 0 0', fontSize: '11.5px' } }, r.method.citation) : null);
  }

  // A curated or consensus label set: what it is made of, and its edits or rule.
  function versionSection(r) {
    const seg = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.id === r.segmentation);
    const curated = r.kind === 'curated-labels';
    return h('section.inspector-section',
      h('h3', icon(curated ? 'edit' : 'layers'), curated ? 'Curated labels' : 'Consensus labels'),
      h('dl.kv',
        h('dt', 'Name'), h('dd', r.name),
        h('dt', 'Segmentation'), h('dd', seg?.name ?? r.segmentation),
        ...(curated ? [h('dt', 'From'), h('dd', r.base.name), h('dt', 'Edits'), h('dd', String(r.edits.length))]
          : [h('dt', 'Of'), h('dd', r.members.map((m) => m.name).join('; ')), h('dt', 'Rule'), h('dd', r.rule.kind === 'majority' ? `majority of ${r.rule.min}` : r.rule.kind)]),
        h('dt', 'Made'), h('dd', `${relativeTime(r.created)} by ${r.by}`)),
      curated && r.edits.length ? h('ul.inspector-list', ...r.edits.slice(-8).reverse().map((e) => h('li', `${e.kind === 'accept' ? 'accepted' : 'relabeled'} "${e.label}" (${e.kind === 'cluster' ? `clusters ${e.clusters.map((k) => k + 1).join(', ')}` : `${e.cells.length} cell${e.cells.length === 1 ? '' : 's'}`}): ${e.reason}, ${e.by}, ${relativeTime(e.time)}`))) : null,
      h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small.primary', { type: 'button', onclick: () => app.showOnTissue(r.dataset, r.segmentation, `labels:${r.id}:0`) }, icon('tissue'), 'Show on the tissue'),
        h('button.btn.small', { type: 'button', onclick: () => {
          app.focusItem({ kind: 'labels', id: r.id });
          app.setMode('celltypes');
        } }, icon('tag'), 'Review')));
  }

  // A marker set: its versions, where it came from, and its file.
  function markerSetSection(m) {
    const versions = markerSetVersions(app.store.project, m.family);
    const from = (v) => (v.source?.starter ? `SituCraft's starter set (${v.source.starter})` : v.source?.file ? `the file ${v.source.file}` : 'edited');
    return h('section.inspector-section',
      h('h3', icon('dna'), 'Marker set'),
      h('dl.kv',
        h('dt', 'Name'), h('dd', m.name),
        h('dt', 'Levels'), h('dd', m.set.levels.join(', ')),
        h('dt', 'Labels'), h('dd', String(m.set.labels.length)),
        h('dt', 'States'), h('dd', String(m.set.states?.length ?? 0)),
        h('dt', 'References'), h('dd', String(m.set.references?.length ?? 0)),
        h('dt', 'Checksum'), h('dd.mono', m.checksum.slice(0, 16))),
      h('div.section-title', { style: { marginTop: '8px' } }, 'Versions'),
      h('ul.inspector-list', ...versions.map((v) => h('li', `Version ${v.version}: ${from(v)}, ${relativeTime(v.created)}`))),
      h('div.btn-row', { style: { marginTop: '8px' } },
        h('button.btn.small', { type: 'button', onclick: () => {
          const blob = new Blob([markerSetFile(m.set)], { type: 'application/json' });
          const a = h('a', { href: URL.createObjectURL(blob), download: `${m.name.replace(/[^\w.-]+/g, '_')}.markers.json` });
          document.body.append(a);
          a.click();
          a.remove();
          setTimeout(() => URL.revokeObjectURL(a.href), 1000);
        } }, icon('download'), 'Save as a file')));
  }

  // The focused item's section, with a link to what made it when it is a result.
  function focusSection() {
    const section = itemSection();
    const id = app.store.ui.focus?.id;
    const item = id ? itemOf(app.store.project, id) : null;
    if (section && item && RESULT_KINDS.includes(item.kind)) {
      section.append(h('div.btn-row.inspector-record', { style: { marginTop: '8px' } },
        h('button.btn.small', { type: 'button', title: 'Each step from the bundles to it, with its method, parameters, seed, software and references (in Reports)', onclick: () => app.showRecord(id) }, icon('report'), 'What made it'),
        h('button.btn.small', { type: 'button', title: 'A methods section for it, written from its record (in Reports)', onclick: () => app.showMethods({ kind: 'result', id }) }, icon('report'), 'Its methods')));
    }
    return section;
  }

  function itemSection() {
    const focus = app.store.ui.focus;
    if (focus?.kind === 'view') {
      const view = app.store.project.views.find((v) => v.id === focus.id);
      if (view) return viewSection(view);
    }
    if (focus?.kind === 'segmentation') {
      const a = app.store.project.artifacts.find((x) => x.kind === 'segmentation' && x.id === focus.id);
      if (a) return segmentationSection(a);
    }
    if (focus?.kind === 'comparison') {
      const c = app.store.project.artifacts.find((a) => a.kind === 'comparison' && a.id === focus.id);
      if (c) return comparisonSection(c);
    }
    if (focus?.kind === 'clustering') {
      const c = app.store.project.artifacts.find((a) => a.kind === 'clustering' && a.id === focus.id);
      if (c) return clusteringSection(c);
    }
    if (focus?.kind === 'spatial') {
      const r = app.store.project.artifacts.find((a) => a.kind === 'spatial' && a.id === focus.id);
      if (r) return analysisSection(r);
    }
    if (focus?.kind === 'abundance') {
      const r = app.store.project.artifacts.find((a) => a.kind === 'abundance' && a.id === focus.id);
      if (r) return abundanceSection(r);
    }
    if (focus?.kind === 'expression' || focus?.kind === 'spatial-comparison') {
      const r = app.store.project.artifacts.find((a) => a.kind === focus.kind && a.id === focus.id);
      if (r) return testSection(r);
    }
    if (focus?.kind === 'cohort-summary') {
      const r = app.store.project.artifacts.find((a) => a.kind === 'cohort-summary' && a.id === focus.id);
      if (r) return summarySection(r);
    }
    if (focus?.kind === 'graph') {
      const g = app.store.project.artifacts.find((a) => a.kind === 'graph' && a.id === focus.id);
      if (g) return graphSection(g);
    }
    if (focus?.kind === 'labels') {
      const r = app.store.project.artifacts.find((a) => a.id === focus.id && ['labels', 'curated-labels', 'consensus-labels'].includes(a.kind));
      if (r?.kind === 'labels') return labelsSection(r);
      if (r) return versionSection(r);
    }
    if (focus?.kind === 'marker-set') {
      const m = app.store.project.artifacts.find((a) => a.kind === 'marker-set' && a.id === focus.id);
      if (m) return markerSetSection(m);
    }
    if (focus?.kind === 'filter') {
      const filter = app.store.project.artifacts.find((a) => a.id === focus.id);
      if (filter) return filterSection(filter);
    }
    if (focus?.kind === 'dataset') {
      const dataset = app.store.project.datasets.find((d) => d.id === focus.id);
      if (dataset) return datasetSection(dataset);
    }
    if (focus?.kind === 'local') {
      const item = app.localItems.find((f) => f.url === focus.id);
      if (item) return localSection(item);
    }
    if (!focus) {
      return h('section.inspector-section',
        h('h3', icon('info'), 'Nothing selected'),
        h('p.muted', { style: { margin: 0, fontSize: '12.5px' } }, 'Select an item in the project tree, or a cell in the tissue, to see everything known about it here: identifiers, counts, geometry, labels, and where each number came from.'));
    }
    return null;
  }

  function render() {
    root.replaceChildren();
    root.append(focusSection());
    for (const build of contributed.values()) {
      try {
        root.append(build(app));
      } catch (error) {
        root.append(h('section.inspector-section', h('p.muted', `This section could not be shown: ${error.message}`)));
      }
    }
    root.append(projectSection());
    root.append(h('section.inspector-section', h('p.muted', { style: { margin: 0, fontSize: '11.5px' } },
      'SituCraft is for research use only. It is not a diagnostic device.')));
  }

  return {
    render,
    setSection(id, build) {
      if (build) contributed.set(id, build);
      else contributed.delete(id);
      render();
    },
    update(topics) {
      if (topics.has('project') || topics.has('focus') || topics.has('selection') || topics.has('project-loaded') || topics.has('local') || topics.has('bundles') || topics.has('jobs')) render();
    },
  };
}
