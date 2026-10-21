// The drawer's Calls tab: the library's log of the calls agents (situcraft mcp) and scripts (remote
// control) made of SituCraft (calls.go), the newest first, searched by words and chosen by caller,
// tool and outcome. A call opens to its arguments, what it changed in the project and the ids it
// answered with; those of the open project open their records. Exported as CSV or JSON lines.

import { h, clear } from './dom.js';
import { callDuration, callIDs, callTime, callsQuery } from '../lib/calls.js';
import { openItem } from './sidebar.js';

const OUTCOMES = { done: 'ok', refused: 'warn', failed: 'danger' };
const SHOWN = 200;

export function callsTab(app) {
  const state = { q: '', tool: '', by: '', outcome: '', opened: new Set(), read: null, error: null };
  let list = null;
  let summary = null;
  let selects = null;
  let exports = [];
  let asked = 0;
  let timer = null;

  async function load() {
    const n = ++asked;
    try {
      const response = await fetch(`api/calls?${callsQuery(state, { limit: SHOWN })}`, { cache: 'no-store' });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
      if (n !== asked) return;
      state.read = body;
      state.error = null;
    } catch (error) {
      if (n !== asked) return;
      state.error = error.message;
    }
    show();
  }

  const later = (ms = 250) => {
    clearTimeout(timer);
    timer = setTimeout(load, ms);
  };

  // A record of the open project by its id: [kind, record], or null.
  function recordOf(id) {
    const p = app.store.project;
    const d = p.datasets.find((x) => x.id === id);
    if (d) return ['dataset', d];
    const a = p.artifacts.find((x) => x.id === id);
    if (a) return [a.kind, a];
    for (const [key, kind] of [['views', 'view'], ['figures', 'figure'], ['samples', 'sample'], ['regions', 'region']]) {
      const x = (p[key] ?? []).find((y) => y.id === id);
      if (x) return [kind, x];
    }
    return null;
  }

  function idLink(id) {
    const found = recordOf(id);
    if (!found) return h('span.mono.muted', { title: 'Not in the open project' }, id);
    const [kind, record] = found;
    return h('button.link.mono', {
      type: 'button',
      title: `Show this ${kind} of the open project`,
      onclick: () => {
        // A region is shown on its dataset's tissue.
        if (kind === 'region') {
          app.focusItem({ kind: 'dataset', id: record.dataset });
          app.setMode('viewer');
        } else openItem(app, kind, record);
      },
    }, id);
  }

  function detail(call) {
    const changed = call.changed ?? [];
    const answered = (call.ids ?? []).filter((id) => !changed.some((c) => c.id === id));
    const leave = call.via === 'agent' ? (call.leave?.length ? `read, ${call.leave.join(', ')}` : 'read only') : null;
    return h('div.call-detail',
      h('dl.kv',
        h('dt', 'Caller'), h('dd', `${call.by} (${call.via === 'agent' ? 'an agent, situcraft mcp' : 'a script, remote control'})`),
        leave ? [h('dt', 'Leave'), h('dd', leave)] : null,
        h('dt', 'Asked'), h('dd', new Date(call.time).toLocaleString('en-US')),
        h('dt', 'Answer'), h('dd', call.message || '—'),
        changed.length ? [h('dt', 'Changed'), h('dd', h('ul.call-ids', ...changed.map((c) => h('li', `${c.change} ${c.kind}${c.name ? ` ${c.name}` : ''} `, c.id ? idLink(c.id) : null))))] : null,
        answered.length ? [h('dt', 'Answered with'), h('dd', h('span.call-ids', ...answered.flatMap((id, i) => [i ? ' ' : null, idLink(id)])))] : null),
      h('div.muted', 'Arguments'),
      h('pre.mono.call-args', call.args ? JSON.stringify(call.args, null, 2) : 'none'));
  }

  function row(call) {
    const key = `${call.time}|${call.tool}|${call.by}`;
    const open = state.opened.has(key);
    const ids = callIDs(call);
    return h('li',
      h('button.call-row', {
        type: 'button',
        'aria-expanded': String(open),
        onclick: () => {
          if (open) state.opened.delete(key);
          else state.opened.add(key);
          show();
        },
      },
      h('time', callTime(call.time)),
      h(`span.badge.${OUTCOMES[call.outcome] ?? 'accent'}`, call.outcome),
      h('span.mono.call-tool', call.tool),
      h('span.call-by', { title: call.via === 'agent' ? 'An agent (situcraft mcp)' : 'A script (remote control)' }, call.by),
      h('span.call-message', { title: call.message }, call.message || (ids.length ? ids.join(' ') : '')),
      h('span.call-ms', callDuration(call.ms))),
      open ? detail(call) : null);
  }

  function options(select, values, all) {
    const current = select.value;
    clear(select);
    select.append(h('option', { value: '' }, all), ...values.map((v) => h('option', { value: v, selected: v === current }, v)));
  }

  function show() {
    if (!list?.isConnected) return;
    // The exports take the calls chosen.
    for (const [a, format] of exports) a.href = `api/calls/export?${callsQuery(state, { format })}`;
    const read = state.read;
    if (read && selects) {
      options(selects.by, read.callers, 'Every caller');
      options(selects.tool, read.tools, 'Every tool');
    }
    clear(list);
    clear(summary);
    if (state.error) {
      list.append(h('p.muted', `The log could not be read: ${state.error}`));
      return;
    }
    if (!read) {
      list.append(h('p.muted', 'Reading the log…'));
      return;
    }
    const chosen = state.q || state.tool || state.by || state.outcome;
    summary.append(`${read.total.toLocaleString('en-US')} call${read.total === 1 ? '' : 's'}${chosen ? ' chosen' : ''}${read.total > read.calls.length ? `, the latest ${read.calls.length} shown` : ''}`);
    if (!read.calls.length) {
      list.append(h('p.muted', chosen
        ? 'No call matches.'
        : 'The calls agents (situcraft mcp) and scripts (remote control) make of SituCraft are logged here, refusals and failures too, with their arguments, what they changed and the ids they answered with. The log stays on this computer, in the library.'));
      return;
    }
    list.append(h('ol.calls-list', ...read.calls.map(row)));
  }

  return {
    label: 'Calls',
    render() {
      const search = h('input.input.small', { type: 'search', placeholder: 'Search the calls', 'aria-label': 'Search the calls', value: state.q });
      search.addEventListener('input', () => {
        state.q = search.value;
        for (const [a, format] of exports) a.href = `api/calls/export?${callsQuery(state, { format })}`;
        later();
      });
      const choose = (key, label) => {
        const select = h('select.input.small', { 'aria-label': label });
        select.addEventListener('change', () => {
          state[key] = select.value;
          load();
        });
        return select;
      };
      selects = { outcome: choose('outcome', 'Outcome'), by: choose('by', 'Caller'), tool: choose('tool', 'Tool') };
      selects.outcome.append(...[['', 'Every outcome'], ['done', 'Done'], ['refused', 'Refused'], ['failed', 'Failed']].map(([v, text]) => h('option', { value: v, selected: v === state.outcome }, text)));
      if (state.read) {
        options(selects.by, state.read.callers, 'Every caller');
        options(selects.tool, state.read.tools, 'Every tool');
        selects.by.value = state.by;
        selects.tool.value = state.tool;
      }
      const exportLink = (format, text) => {
        const a = h('a.btn.small.ghost', { href: `api/calls/export?${callsQuery(state, { format })}`, download: '', title: `The calls chosen, the oldest first, as ${text}` }, text);
        exports.push([a, format]);
        return a;
      };
      exports = [];
      summary = h('span.muted.calls-summary');
      list = h('div.calls-body');
      const root = h('div.calls-tab',
        h('div.calls-bar',
          h('div.calls-choose', search, selects.outcome, selects.by, selects.tool),
          h('div.calls-choose', summary, h('span.spacer'), h('span.muted', 'Export'), exportLink('csv', 'CSV'), exportLink('jsonl', 'JSON lines'))),
        list);
      show();
      load();
      return root;
    },
    // A call just logged (the hub's notice): the tab, shown, reads the log again.
    logged() {
      if (list?.isConnected) later(300);
    },
  };
}
