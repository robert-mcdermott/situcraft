// Small DOM helpers: element creation, icons, and listeners. Adapted from MaveScape 0.8.0
// web/ui/dom.js.

import { ICONS } from './icons.js';

// The views build content with conditionals (`cond ? node : null`); the DOM would insert those as
// the text "null". append, prepend and replaceChildren skip null, undefined and false instead.
if (typeof Element !== 'undefined' && !Element.prototype.append.situcraft) {
  for (const name of ['append', 'prepend', 'replaceChildren']) {
    const original = Element.prototype[name];
    const patched = function patchedChildren(...nodes) {
      return original.apply(this, nodes.filter((node) => node !== null && node !== undefined && node !== false));
    };
    patched.situcraft = true;
    Element.prototype[name] = patched;
    if (typeof DocumentFragment !== 'undefined') DocumentFragment.prototype[name] = patched;
  }
}

// h('div.card#id', { onclick, title, dataset: {...}, style: {...} }, children...)
export function h(selector, props, ...children) {
  let tag = 'div';
  const classes = [];
  let id = null;
  selector.replace(/([.#]?)([\w-]+)/g, (_, kind, name) => {
    if (kind === '.') classes.push(name);
    else if (kind === '#') id = name;
    else tag = name;
    return '';
  });
  const el = document.createElement(tag);
  if (classes.length) el.className = classes.join(' ');
  if (id) el.id = id;
  // Anything but an object of properties is a child: 0 too, which a test of truth would drop.
  if (props !== null && props !== undefined && props !== false && (typeof props !== 'object' || props instanceof Node || Array.isArray(props))) {
    children.unshift(props);
    props = null;
  }
  if (props) {
    for (const [key, value] of Object.entries(props)) {
      if (value === undefined || value === null || value === false) continue;
      if (key === 'class' || key === 'className') el.className = [el.className, value].filter(Boolean).join(' ');
      else if (key === 'style' && typeof value === 'object') {
        Object.assign(el.style, value);
        // A region that scrolls must be reachable by keyboard to be scrolled (WCAG 2.1.1).
        if ([value.overflow, value.overflowY, value.overflowX].includes('auto') && !('tabIndex' in props) && !('tabindex' in props)) el.tabIndex = 0;
      }
      else if (key === 'dataset') Object.assign(el.dataset, value);
      else if (key.startsWith('on') && typeof value === 'function') el.addEventListener(key.slice(2).toLowerCase(), value);
      else if (key === 'html') el.innerHTML = value;
      else if (key in el && typeof value !== 'string') el[key] = value;
      else if (value === true) el.setAttribute(key, '');
      else el.setAttribute(key, value);
    }
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export function clear(el) {
  while (el.firstChild) el.firstChild.remove();
  return el;
}

export function replace(el, ...children) {
  clear(el);
  append(el, children);
  return el;
}

const parser = typeof DOMParser !== 'undefined' ? new DOMParser() : null;

// An inline SVG icon element.
export function icon(name, className = 'icon') {
  const markup = ICONS[name] ?? ICONS.dot;
  const doc = parser.parseFromString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="${className}" aria-hidden="true">${markup}</svg>`, 'image/svg+xml');
  return document.importNode(doc.documentElement, true);
}

export function iconButton(name, title, onClick, extra = {}) {
  return h('button.icon-button', { type: 'button', title, 'aria-label': title, onclick: onClick, ...extra }, icon(name));
}

export function debounce(fn, wait = 200) {
  let timer = null;
  const wrapped = (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), wait);
  };
  wrapped.flush = (...args) => {
    clearTimeout(timer);
    fn(...args);
  };
  wrapped.cancel = () => clearTimeout(timer);
  return wrapped;
}

export function formatCount(n) {
  if (!Number.isFinite(n)) return '—';
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 1 : 2)}M`;
  if (n >= 1e4) return `${(n / 1e3).toFixed(n >= 1e5 ? 0 : 1)}K`;
  return Math.round(n).toLocaleString('en-US');
}

// Sizes in decimal units (1 GB is 10^9 bytes), as the program's --cache-limit reads them and as
// macOS shows files.
export function formatBytes(bytes) {
  if (!Number.isFinite(bytes)) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = bytes;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i += 1;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function relativeTime(iso) {
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return '';
  const seconds = (Date.now() - then) / 1000;
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} h ago`;
  if (seconds < 86400 * 30) return `${Math.floor(seconds / 86400)} d ago`;
  return new Date(iso).toLocaleDateString();
}

export function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = h('a', { href: url, download: filename });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 4000);
}

export function isTyping(event) {
  const target = event.target;
  return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName));
}

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
export const modKey = isMac ? '⌘' : 'Ctrl+';
