// A view whose workbench is not built yet: it says what the view is for and what comes before it,
// so the shell can be navigated end to end while the views are built one by one
// (situcraft-spec/roadmap.md). Each is replaced by its own mode-<name>.js when it is built.

import { h, icon } from './dom.js';

// spec: { title, icon, purpose, steps: [text], actions: [{ label, icon, run(app) }] }
export function plannedMode(spec) {
  return function mountPlanned(app, container) {
    const root = h('div.view',
      h('div.workbench-head', h('h1', icon(spec.icon), spec.title)),
      h('div.view-body', h('div.planned',
        h('div.empty',
          icon(spec.icon),
          h('h3', spec.title),
          h('p', spec.purpose),
          spec.steps?.length ? h('ol', ...spec.steps.map((step) => h('li', step))) : null,
          spec.actions?.length ? h('div.btn-row', { style: { marginTop: '12px' } }, ...spec.actions.map((action, i) => h(`button.btn${i === 0 ? '.primary' : ''}`, { type: 'button', onclick: () => action.run(app) }, icon(action.icon ?? 'dot'), action.label))) : null))));
    container.append(root);
    return {
      update() {},
      destroy() {
        root.remove();
      },
    };
  };
}
