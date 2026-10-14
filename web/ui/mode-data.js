// The Data view: the Xenium output bundles of the project and of this computer, each with what it
// holds, how it validated (fatal problems, warnings and notes, each with the files, region and
// remedy) and its files by role; a bundle with no fatal problem can be added to the project.

import { h, icon, clear, formatCount, formatBytes, relativeTime } from './dom.js';
import { confirmDialog, promptDialog, toast } from './overlays.js';
import { countText } from './bundles.js';

const SEVERITY = {
  fatal: { label: 'Fatal', badge: 'danger', icon: 'warning', heading: 'Fatal problems', hint: 'The bundle cannot be used until these are fixed.' },
  warning: { label: 'Warning', badge: 'warn', icon: 'warning', heading: 'Warnings', hint: 'The bundle can be used, but these may bias an analysis.' },
  info: { label: 'Note', badge: 'accent', icon: 'info', heading: 'Notes', hint: 'Worth knowing; nothing to fix.' },
};

const LINK_STATES = {
  checking: ['accent', 'Looking for it'],
  ok: ['ok', 'Found'],
  missing: ['danger', 'Not found'],
  changed: ['warn', 'Changed since added'],
  'different-run': ['danger', 'Another run is there'],
};

function fmtUm(v) {
  return `${Number(v).toLocaleString('en-US', { maximumFractionDigits: 1 })} µm`;
}

export function mountData(app, container) {
  const body = h('div.view-body');
  const root = h('div.view',
    h('div.workbench-head', h('h1', icon('database'), 'Data'), h('span.spacer'),
      h('button.btn.small', { type: 'button', onclick: () => app.openPathDialog() }, icon('bundle'), 'Open a bundle')),
    body);
  container.append(root);

  // The bundle the view shows: the one in focus, else the first dataset, else the first bundle.
  function current() {
    const { project, ui } = app.store;
    const focus = ui.focus;
    if (focus?.kind === 'dataset') {
      const dataset = project.datasets.find((d) => d.id === focus.id);
      if (dataset) return { dataset, item: app.bundles.link(dataset.id)?.item ?? app.bundles.itemByPath(dataset.path) };
    }
    if (focus?.kind === 'local') {
      const item = app.localItems.find((i) => i.url === focus.id);
      if (item && (item.kind === 'bundle' || item.kind === 'archive')) return { item, dataset: app.bundles.datasetFor(item) };
    }
    if (project.datasets.length) {
      const dataset = project.datasets[0];
      return { dataset, item: app.bundles.link(dataset.id)?.item ?? app.bundles.itemByPath(dataset.path) };
    }
    const item = app.bundles.items()[0];
    return item ? { item, dataset: app.bundles.datasetFor(item) } : null;
  }

  function render() {
    clear(body);
    const target = current();
    const others = listOthers(target);
    if (!target) {
      body.append(h('div.planned', h('div.empty', icon('database'), h('h3', 'No bundle yet'),
        h('p', 'Open a Xenium output bundle (its folder, its .zip, or its experiment.xenium) to see what it holds and whether to trust it. SituCraft reads it where it is and never changes it.'),
        h('div.btn-row', { style: { marginTop: '12px' } }, h('button.btn.primary', { type: 'button', onclick: () => app.openPathDialog() }, icon('bundle'), 'Open a bundle')))));
      return;
    }
    if (others) body.append(others);
    try {
      body.append(renderBundle(target));
    } catch (error) {
      // A record this version does not understand should say so, not leave the view empty.
      body.append(h('div.callout.danger', `This dataset could not be shown: ${error.message}`));
    }
  }

  // The other bundles, as chips to switch to.
  function listOthers(target) {
    const { project } = app.store;
    const chips = [];
    for (const d of project.datasets) {
      const active = target?.dataset?.id === d.id;
      chips.push(h(`button.chip${active ? '.active' : ''}`, { type: 'button', onclick: () => app.focusItem({ kind: 'dataset', id: d.id }) }, icon('bundle'), d.name, h('span.count', 'in project')));
    }
    for (const item of app.bundles.items()) {
      if (app.bundles.datasetFor(item)) continue;
      const active = !target?.dataset && target?.item?.url === item.url;
      chips.push(h(`button.chip${active ? '.active' : ''}`, { type: 'button', onclick: () => app.focusItem({ kind: 'local', id: item.url }) }, icon('folder'), item.name, h('span.count', 'not added')));
    }
    return chips.length > 1 ? h('div.bundle-chips', ...chips) : null;
  }

  function renderBundle({ item, dataset }) {
    const entry = item ? app.bundles.report(item.path) : null;
    if (item && !entry) app.bundles.validate(item).catch(() => {});
    const report = entry?.report ?? null;
    const link = dataset ? app.bundles.link(dataset.id) : null;
    const name = report?.bundle ?? dataset?.name ?? item?.name;
    const fatal = report ? report.findings.filter((f) => f.severity === 'fatal').length : 0;
    const section = h('div.bundle');

    // Title, place and state.
    const badges = [];
    if (dataset) badges.push(h('span.badge.ok', 'In the project'));
    else badges.push(h('span.badge', 'Not in the project'));
    if (link && LINK_STATES[link.state]) badges.push(h(`span.badge.${LINK_STATES[link.state][0]}`, LINK_STATES[link.state][1]));
    if (report) badges.push(h(`span.badge.${report.support === 'supported' ? 'ok' : 'warn'}`, report.support === 'supported' ? `XOA ${report.version}` : `XOA ${report.version}: ${report.support}`));
    const actions = [];
    if (item && !dataset) {
      actions.push(h('button.btn.primary', {
        type: 'button',
        disabled: !report || fatal > 0,
        title: fatal > 0 ? 'Fix the fatal problems first' : report ? 'Record this bundle in the project' : 'Checking…',
        onclick: async () => {
          try {
            const record = await app.bundles.add(item);
            app.focusItem({ kind: 'dataset', id: record.id });
            toast(`${record.name} added to the project.`, { kind: 'ok' });
          } catch (error) {
            toast(error.message, { kind: 'error' });
          }
        },
      }, icon('plus'), 'Add to project'));
    }
    if (item) actions.push(h('button.btn', { type: 'button', disabled: entry?.state === 'validating', onclick: () => app.bundles.validate(item, { force: true }).catch(() => {}) }, icon('qc'), 'Check again'));
    if (dataset && (!link || link.state === 'missing' || link.state === 'different-run' || link.state === 'changed')) {
      actions.push(h('button.btn', { type: 'button', onclick: () => locate(dataset) }, icon('folder'), 'Locate…'));
    }
    if (dataset) {
      actions.push(h('button.btn.ghost.danger', {
        type: 'button',
        onclick: async () => {
          if (await confirmDialog({ title: 'Remove from the project?', message: `${dataset.name} leaves this project. The bundle itself is not touched.`, confirm: 'Remove', danger: true })) app.bundles.remove(dataset.id);
        },
      }, icon('trash'), 'Remove'));
    }
    section.append(h('div.bundle-head',
      h('div', { style: { minWidth: 0 } },
        h('h2', name, ...badges),
        h('p.mono.muted.bundle-path', dataset?.path ?? item?.path ?? ''),
        facts(report, dataset)),
      h('div.btn-row', ...actions)));

    if (link?.state === 'missing') section.append(callout('danger', `The bundle is not where the project recorded it. ${link.error ?? ''} Locate it to reconnect the dataset.`));
    if (link?.state === 'changed') section.append(callout('warn', `Files changed since the dataset was added: ${link.files.slice(0, 6).join(', ')}${link.files.length > 6 ? '…' : ''}. Check it again before relying on earlier results.`));
    if (link?.state === 'different-run') section.append(callout('danger', 'What is there now is another run (or another analysis of it) than the one the dataset recorded.'));
    if (report && report.support !== 'supported') section.append(callout('warn', report.supportMessage));

    if (entry?.state === 'validating' && !report) section.append(h('div.callout.accent', h('span.spinner'), ` Checking ${name}: reading its tables, matrix, boundaries and images…`));
    if (entry?.state === 'error') section.append(callout('danger', `It could not be checked: ${entry.error}`));
    if (!item && dataset) section.append(recordedSummary(dataset));
    if (report) {
      section.append(statTiles(report));
      section.append(findings(report));
      section.append(inventory(report));
    }
    return section;
  }

  async function locate(dataset) {
    const path = await promptDialog({ title: `Locate ${dataset.name}`, label: 'Path on this computer', value: dataset.path ?? '', hint: 'Where the bundle is now: its folder or .zip. SituCraft checks it is the same run before using it.', confirm: 'Locate' });
    if (!path) return;
    try {
      await app.bundles.locate(dataset.id, path);
      toast(`${dataset.name} found at ${path}.`, { kind: 'ok' });
    } catch (error) {
      toast(error.message, { kind: 'error' });
    }
  }

  render();
  return {
    update(topics) {
      if (topics.has('project') || topics.has('focus') || topics.has('bundles') || topics.has('local') || topics.has('project-loaded')) render();
    },
    destroy() {
      root.remove();
    },
  };
}

function callout(kind, text) {
  return h(`div.callout.${kind}`, text);
}

function facts(report, dataset) {
  const s = report?.summary ?? dataset ?? {};
  const parts = [s.panel, [s.organism, s.tissue].filter(Boolean).join(' '), s.preservation?.toUpperCase(), s.runStart ? `run ${new Date(s.runStart).toLocaleDateString()}` : '', s.region && s.region !== 'region' ? `region ${s.region}` : ''].filter(Boolean);
  return h('p.bundle-facts', parts.join(' · '));
}

function statTiles(report) {
  const s = report.summary;
  const tile = (k, v, title) => h('div.stat-tile', { title: title ?? '' }, h('div.k', k), h('div.v', v));
  return h('div.stat-grid.bundle-stats',
    tile('Cells', formatCount(s.cells)),
    tile('Transcripts', formatCount(s.transcripts), `${s.transcripts.toLocaleString('en-US')} decoded, of every quality`),
    tile('Q20 gene transcripts', formatCount(s.q20GeneTranscripts), 'Gene transcripts with a quality score of at least 20'),
    tile('In cells', `${(s.assignedFraction * 100).toFixed(1)}%`, 'Share of Q20 gene transcripts assigned to a cell'),
    tile('Median per cell', String(s.medianTranscriptsPerCell)),
    tile('Genes', s.panelGenes ? `${s.observedGenes} of ${s.panelGenes}` : String(s.observedGenes), 'Panel genes detected'),
    tile('Channels', String(s.channels?.length ?? 0), (s.channels ?? []).join('\n')),
    tile('Z planes', String(s.zPlanes || '—')),
    tile('Area', s.extentUm ? `${fmtUm(s.extentUm[0])} × ${fmtUm(s.extentUm[1])}` : '—'),
    tile('Pixel', s.pixelSizeUm ? `${s.pixelSizeUm} µm` : '—'),
    tile('Size', formatBytes(s.bundleBytes)),
    tile('Checked in', `${s.checkDurationSeconds} s`));
}

function recordedSummary(dataset) {
  const v = dataset.validation ?? {};
  return h('div.pane', h('h3', icon('info'), 'As recorded when added'),
    h('dl.kv',
      h('dt', 'Cells'), h('dd', formatCount(dataset.cells)),
      h('dt', 'Transcripts'), h('dd', formatCount(dataset.transcripts)),
      h('dt', 'Checked'), h('dd', v.checked ? `${relativeTime(v.checked)}: ${v.fatal} fatal, ${v.warnings} warnings, ${v.info} notes` : 'not recorded'),
      h('dt', 'Metrics reproduced'), h('dd', v.metricsCompared ? `${v.metricsAgreeing} of ${v.metricsCompared}` : '—')));
}

function findings(report) {
  const pane = h('div.pane');
  const s = report.summary;
  pane.append(h('h3', icon('qc'), 'Validation', h('span.muted', { style: { fontWeight: 500 } }, countText(report)), h('span.spacer'),
    h('span.muted', { style: { fontWeight: 500 } }, `${s.metricsAgreeing} of the run's ${s.metricsCompared} recorded metrics reproduced from its data`)));
  if (!report.findings.length) pane.append(h('p.muted', 'Nothing to report: every check passed.'));
  for (const severity of ['fatal', 'warning', 'info']) {
    const list = report.findings.filter((f) => f.severity === severity);
    if (!list.length) continue;
    const meta = SEVERITY[severity];
    pane.append(h('div.finding-group', h('div.finding-group-head', h(`span.badge.${meta.badge}`, `${list.length} ${meta.heading.toLowerCase()}`), h('span.muted', meta.hint)),
      ...list.map((f) => h(`div.finding.${severity}`,
        h('div.finding-title', icon(meta.icon), h('b', f.title), f.count ? h('span.badge', f.count.toLocaleString('en-US')) : null, h('code.finding-code', f.code)),
        f.detail ? h('p', f.detail) : null,
        f.files?.length || f.region ? h('p.finding-where.muted',
          f.files?.length ? h('span', icon('file'), f.files.join(', ')) : null,
          f.region ? h('span', icon('target'), `x ${fmtUm(f.region.x0)}–${fmtUm(f.region.x1)}, y ${fmtUm(f.region.y0)}–${fmtUm(f.region.y1)}`) : null) : null,
        f.remedy ? h('p.finding-remedy', icon('lightbulb'), f.remedy) : null))));
  }
  if (s.segmentationMethods?.length) {
    const rows = s.segmentationMethods.map((m, i) => [m, s.segmentationCounts?.[i]]).filter(([, n]) => n);
    if (rows.length) pane.append(h('p.muted', { style: { marginTop: '10px' } }, `Segmentation: ${rows.map(([m, n]) => `${n.toLocaleString('en-US')} cells ${m.replace(/^Segmented /, '')}`).join('; ')}.`));
  }
  return pane;
}

function inventory(report) {
  const rows = report.inventory.map((item) => {
    const present = item.files?.length > 0;
    let status;
    if (item.note) status = h('span.badge.danger', 'Unread form');
    else if (present) status = h('span.badge.ok', 'Present');
    else if (item.required) status = h('span.badge.danger', 'Missing');
    else status = h('span.muted', 'Absent');
    return h('tr',
      h('td', h('b', item.description), item.note ? h('div.muted', item.note) : null),
      h('td.mono', (item.files ?? []).slice(0, 4).join(', ') + ((item.files?.length ?? 0) > 4 ? ` and ${item.files.length - 4} more` : '')),
      h('td', item.format ?? ''),
      h('td.r', present ? formatBytes(item.size) : ''),
      h('td', status));
  });
  return h('div.pane', h('h3', icon('folder'), 'Files by role'),
    h('div', { style: { overflowX: 'auto' } }, h('table.data', h('thead', h('tr', h('th', 'Role'), h('th', 'Files'), h('th', 'Form'), h('th.r', 'Size'), h('th', 'Status'))), h('tbody', ...rows))));
}
