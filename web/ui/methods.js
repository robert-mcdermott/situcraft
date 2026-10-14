// The methods paragraph of a dataset (lib/methods.js) as the window shows it, and as remote control
// asks for it: the filters described are chosen (by default the latest version of each), the data's
// source cited for an example.

import { h, clear, formatCount } from './dom.js';
import { showDialog, toast } from './overlays.js';
import { methodsParagraph } from '../lib/methods.js';
import { describeCondition } from '../lib/filters.js';
import { exampleSource } from '../lib/examples.js';

// The dataset's filters, and the latest version of each.
export function datasetFilters(project, dataset) {
  const filters = project.artifacts.filter((a) => a.kind === 'filter' && a.dataset === dataset.id);
  const latest = new Map();
  for (const f of filters) if (!latest.has(f.name) || latest.get(f.name).version < f.version) latest.set(f.name, f);
  return { filters, latest: [...latest.values()] };
}

// The paragraph and its references, describing the filters given (artifacts).
export async function writeMethods(app, dataset, report, filters) {
  if (!app.examples.list()) await app.examples.refresh();
  const source = exampleSource(app.examples.forDataset(dataset));
  const methods = report?.summary?.segmentationMethods ?? [];
  const described = filters.map((f) => ({ ...f, conditions: f.conditions.map((c) => ({ ...c, text: describeCondition(c, methods) })) }));
  return methodsParagraph({ dataset, report, filters: described, program: { version: app.version, commit: app.commit }, source });
}

// The paragraph as Markdown, with its references.
export function methodsMarkdown(dataset, written) {
  return `# Methods: ${dataset.name}\n\n${written.text}\n\n## References\n\n${written.references.map((r) => `${r.replace(/^\[(\d+)\]\s*/, '$1. ')}`).join('\n')}\n`;
}

export async function showMethods(app, dataset, report, { chosen: initial = null } = {}) {
  const { filters, latest } = datasetFilters(app.store.project, dataset);
  const chosen = new Set((initial ?? latest).map((f) => f.id));
  const text = h('p.methods-text');
  const refs = h('ol.methods-refs');
  let current = null;
  async function write() {
    current = await writeMethods(app, dataset, report, filters.filter((f) => chosen.has(f.id)));
    text.textContent = current.text;
    clear(refs).append(...current.references.map((r) => h('li', r.replace(/^\[\d+\]\s*/, ''))));
  }
  await write();
  const choices = filters.length ? h('div.methods-filters', h('b', 'Filters to describe'),
    ...filters.map((f) => h('label.viewer-toggle', h('input', { type: 'checkbox', checked: chosen.has(f.id), onchange: (event) => { if (event.target.checked) chosen.add(f.id); else chosen.delete(f.id); write(); } }),
      `${f.name}, version ${f.version} (${formatCount(f.result.kept)} of ${formatCount(f.result.cells)} cells)`))) : null;
  return showDialog({
    title: `Methods: ${dataset.name}`,
    width: 'wide',
    content: [
      h('p.muted', 'Written from what the project records of this dataset and SituCraft\'s version. Check it against what you did, and add what was done elsewhere.'),
      text,
      h('div.section-title', 'References'),
      refs,
      choices,
    ],
    buttons: [
      { label: 'Close', ghost: true },
      { label: 'Copy', primary: true, onClick: async () => {
        try {
          await navigator.clipboard.writeText(`${current.text}\n\n${current.references.join('\n')}\n`);
          toast('Copied the paragraph and its references.', { kind: 'ok' });
        } catch {
          toast('The clipboard is not available: select the text and copy it.', { kind: 'error' });
          return false;
        }
        return undefined;
      } },
    ],
  });
}
