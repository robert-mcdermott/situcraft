// The bottom drawer, under the view: the history of the project, the session's log, the library's
// log of programs' calls, and (as views are built) job logs, tables and evidence too long for the
// inspector. ⌘J or the status bar opens it. Adapted from MaveScape 0.8.0 web/ui/drawer.js.

import { h, clear, iconButton } from './dom.js';
import { prefs } from './storage.js';
import { callsTab } from './calls-tab.js';

export function mountDrawer(app) {
  const root = document.getElementById('drawer');
  // Tabs views add: id → { label, render(app) → Node }.
  const calls = callsTab(app);
  const tabs = new Map([
    ['history', { label: 'History', render: renderHistory }],
    ['log', { label: 'Log', render: renderLog }],
    ['calls', calls],
  ]);
  const log = [];
  let height = prefs.get('drawerHeight', 240);

  function renderHistory() {
    const { state } = app.store;
    const done = state.labels.past.map((label) => ({ label, undone: false }));
    const undone = [...state.labels.future].reverse().map((label) => ({ label, undone: true }));
    const rows = [...done, ...undone];
    if (!rows.length) return h('p.muted', 'Changes to the project are listed here as you make them; undo (⌘Z) steps back through them.');
    return h('ol.history-list', ...rows.map((row, i) => h(`li${row.undone ? '.undone' : ''}`, h('time', `#${i + 1}`), h('span', row.label, row.undone ? ' (undone)' : ''))));
  }

  function renderLog() {
    if (!log.length) return h('p.muted', 'Messages of this session (items opened, warnings, errors) are kept here.');
    return h('ol.history-list', ...log.slice().reverse().map((entry) => h('li', h('time', entry.time.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })), h('span', entry.message))));
  }

  function render() {
    const { drawer } = app.store.ui;
    root.hidden = !drawer.open;
    document.documentElement.style.setProperty('--drawer', `${height}px`);
    clear(root);
    if (!drawer.open) return;
    const active = tabs.has(drawer.tab) ? drawer.tab : 'history';
    const resize = h('div.drawer-resize', { role: 'separator', 'aria-orientation': 'horizontal', 'aria-label': 'Resize the drawer' });
    resize.addEventListener('pointerdown', (event) => {
      const startY = event.clientY;
      const startHeight = height;
      resize.setPointerCapture(event.pointerId);
      const move = (e) => {
        height = Math.max(120, Math.min(window.innerHeight * 0.7, startHeight + startY - e.clientY));
        document.documentElement.style.setProperty('--drawer', `${height}px`);
      };
      const up = () => {
        resize.removeEventListener('pointermove', move);
        prefs.set('drawerHeight', Math.round(height));
      };
      resize.addEventListener('pointermove', move);
      resize.addEventListener('pointerup', up, { once: true });
    });
    // The tabs alone in the tab list; the close button beside it.
    const head = h('div.drawer-head',
      h('div.drawer-tabs', { role: 'tablist', 'aria-label': 'Drawer' },
        ...[...tabs].map(([id, tab]) => h(`button.drawer-tab${id === active ? '.active' : ''}`, {
          type: 'button', role: 'tab', 'aria-selected': id === active ? 'true' : 'false',
          onclick: () => app.store.setUI({ drawer: { open: true, tab: id } }, ['drawer']),
        }, tab.label))),
      h('span.spacer'),
      iconButton('close', 'Close the drawer (⌘J)', () => app.toggleDrawer(false), { class: 'small' }));
    root.append(resize, head, h('div.drawer-body', { role: 'tabpanel' }, tabs.get(active).render(app)));
  }

  return {
    render,
    // Adds a message to the session log.
    log(message) {
      log.push({ time: new Date(), message });
      if (log.length > 500) log.shift();
      if (app.store.ui.drawer.open && app.store.ui.drawer.tab === 'log') render();
    },
    addTab(id, tab) {
      tabs.set(id, tab);
      render();
    },
    // A program's call just logged (remote.js, the hub's notice).
    callLogged(entry) {
      calls.logged(entry);
    },
    update(topics) {
      // The Calls tab reads the library's log, not the project: a change of the project leaves it.
      const project = topics.has('history') || topics.has('project-loaded');
      if (topics.has('drawer') || (project && app.store.ui.drawer.tab !== 'calls')) render();
    },
  };
}
