// Menus, dialogs, prompts and toasts. Adapted from MaveScape 0.8.0 web/ui/overlays.js.

import { h, icon, clear } from './dom.js';

const root = () => document.getElementById('overlay-root');
let openMenu = null;

export function closeMenu() {
  if (openMenu) {
    openMenu.remove();
    openMenu = null;
    document.removeEventListener('pointerdown', outsideMenu, true);
    document.removeEventListener('keydown', menuKeys, true);
  }
}

function outsideMenu(event) {
  if (openMenu && !openMenu.contains(event.target)) closeMenu();
}

function menuKeys(event) {
  if (!openMenu) return;
  const items = [...openMenu.querySelectorAll('.menu-item:not(:disabled)')];
  const index = items.indexOf(document.activeElement);
  if (event.key === 'Escape') {
    event.preventDefault();
    closeMenu();
  } else if (event.key === 'ArrowDown') {
    event.preventDefault();
    items[(index + 1) % items.length]?.focus();
  } else if (event.key === 'ArrowUp') {
    event.preventDefault();
    items[(index - 1 + items.length) % items.length]?.focus();
  }
}

// items: [{ label, icon, hint, onSelect, danger, disabled, checked } | '-' | { section }]
// anchor: an element or { x, y }. options.search adds a filter box.
export function showMenu(anchor, items, options = {}) {
  closeMenu();
  const menu = h('div.menu', { role: 'menu' });
  const list = h('div');
  const render = (filter = '') => {
    clear(list);
    const lower = filter.toLowerCase();
    for (const item of items) {
      if (item === '-') {
        if (!lower) list.append(h('div.menu-sep'));
        continue;
      }
      if (item.section) {
        if (!lower) list.append(h('div.menu-label', item.section));
        continue;
      }
      if (lower && !`${item.label} ${item.hint ?? ''} ${item.keywords ?? ''}`.toLowerCase().includes(lower)) continue;
      const button = h('button.menu-item', {
        type: 'button',
        role: 'menuitem',
        disabled: item.disabled,
        title: item.title,
        class: [item.danger ? 'danger' : '', item.active ? 'active' : ''].join(' '),
        onclick: (event) => {
          event.stopPropagation();
          closeMenu();
          item.onSelect?.();
        },
      },
      item.checked !== undefined ? icon(item.checked ? 'check' : 'dot', 'icon') : item.icon ? icon(item.icon) : null,
      item.swatch ? h('span.swatch', { style: { background: item.swatch } }) : null,
      h('span', item.label),
      item.hint ? h('span.hint', item.hint) : null);
      if (item.checked === false) button.firstChild.style.opacity = '0';
      list.append(button);
    }
  };
  if (options.search) {
    const input = h('input.input.small.menu-search', { placeholder: options.searchPlaceholder ?? 'Filter…', type: 'search' });
    input.addEventListener('input', () => render(input.value));
    input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') {
        list.querySelector('.menu-item:not(:disabled)')?.click();
      } else if (event.key === 'ArrowDown') {
        event.preventDefault();
        list.querySelector('.menu-item')?.focus();
      }
    });
    menu.append(input);
    setTimeout(() => input.focus(), 0);
  }
  menu.append(list);
  render();
  root().append(menu);
  let x;
  let y;
  if (anchor instanceof Element) {
    const rect = anchor.getBoundingClientRect();
    x = rect.left;
    y = rect.bottom + 4;
    if (options.align === 'right') x = rect.right - menu.offsetWidth;
  } else {
    x = anchor.x;
    y = anchor.y;
  }
  const width = menu.offsetWidth;
  const height = menu.offsetHeight;
  x = Math.max(6, Math.min(x, window.innerWidth - width - 6));
  if (y + height > window.innerHeight - 6) y = Math.max(6, (anchor instanceof Element ? anchor.getBoundingClientRect().top - height - 4 : y - height));
  menu.style.left = `${x}px`;
  menu.style.top = `${y}px`;
  openMenu = menu;
  setTimeout(() => {
    document.addEventListener('pointerdown', outsideMenu, true);
    document.addEventListener('keydown', menuKeys, true);
  }, 0);
  if (!options.search) setTimeout(() => list.querySelector('.menu-item:not(:disabled)')?.focus({ preventScroll: true }), 0);
  return menu;
}

// A modal dialog. content: element(s); buttons: [{ label, primary, danger, onClick → false keeps open }].
export function showDialog({ title, content, buttons = [{ label: 'Close' }], width = '', onClose, dismissable = true }) {
  const scrim = h('div.scrim');
  const body = h('div.dialog-body', content);
  const foot = buttons.length ? h('div.dialog-foot') : null;
  const dialog = h(`div.dialog${width ? `.${width}` : ''}`, { role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
    h('div.dialog-head', h('h2', title), dismissable ? h('button.icon-button', { type: 'button', title: 'Close', 'aria-label': 'Close', onclick: () => close() }, icon('close')) : null),
    body, foot);
  let closed = false;
  // Focus goes back where it was when the dialog closes.
  const opener = document.activeElement;
  const close = (result) => {
    if (closed) return;
    closed = true;
    scrim.remove();
    document.removeEventListener('keydown', keys, true);
    if (opener?.isConnected && typeof opener.focus === 'function') opener.focus();
    onClose?.(result);
  };
  const keys = (event) => {
    // Tab stays in the dialog (aria-modal): from the last control to the first and back.
    if (event.key === 'Tab') {
      const focusable = [...dialog.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')].filter((el) => !el.disabled && el.offsetParent !== null);
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!dialog.contains(document.activeElement)) {
        event.preventDefault();
        first.focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
      return;
    }
    if (event.key === 'Escape' && dismissable) {
      event.preventDefault();
      event.stopPropagation();
      close();
    } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      const primary = foot?.querySelector('.btn.primary');
      primary?.click();
    }
  };
  for (const button of buttons) {
    const el = h(`button.btn${button.primary ? '.primary' : ''}${button.danger ? '.danger' : ''}${button.ghost ? '.ghost' : ''}`, {
      type: 'button',
      onclick: async () => {
        const result = button.onClick ? await button.onClick() : undefined;
        if (result !== false) close(button.value ?? result);
      },
    }, button.label);
    foot.append(el);
  }
  scrim.append(dialog);
  scrim.addEventListener('pointerdown', (event) => {
    if (event.target === scrim && dismissable) close();
  });
  document.addEventListener('keydown', keys, true);
  root().append(scrim);
  setTimeout(() => (dialog.querySelector('[autofocus]') ?? dialog.querySelector('input, select, textarea') ?? foot?.querySelector('.btn.primary'))?.focus(), 30);
  return { close, dialog, body };
}

export function confirmDialog({ title, message, confirm = 'OK', danger = false }) {
  return new Promise((resolve) => {
    showDialog({
      title,
      content: h('p', message),
      buttons: [
        { label: 'Cancel', ghost: true, value: false },
        { label: confirm, primary: !danger, danger, value: true, onClick: () => true },
      ],
      onClose: (result) => resolve(Boolean(result)),
    });
  });
}

export function promptDialog({ title, label, value = '', placeholder = '', confirm = 'OK', hint = '' }) {
  return new Promise((resolve) => {
    const input = h('input.input', { value, placeholder, autofocus: true });
    let result = null;
    const { close } = showDialog({
      title,
      content: [h('label.field', h('span', label), input), hint ? h('p.muted', hint) : null],
      buttons: [
        { label: 'Cancel', ghost: true },
        { label: confirm, primary: true, onClick: () => { result = input.value.trim(); return true; } },
      ],
      onClose: () => resolve(result),
    });
    input.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') {
        // Handled here: closing gives focus back to the control that opened the dialog, and an
        // Enter left to the browser would press it, opening the dialog again.
        event.preventDefault();
        result = input.value.trim();
        close();
      }
    });
    setTimeout(() => input.select(), 40);
  });
}

let toastHost = null;

export function toast(message, options = {}) {
  if (!toastHost) {
    toastHost = h('div.toasts');
    document.body.append(toastHost);
  }
  const el = h(`div.toast${options.kind ? `.${options.kind}` : ''}`,
    options.kind === 'error' ? icon('warning') : options.kind === 'ok' ? icon('check') : icon('info'),
    h('span', message),
    options.action ? h('div.actions', h('button.btn.small', { type: 'button', onclick: () => { options.action.onClick(); el.remove(); } }, options.action.label)) : null);
  toastHost.append(el);
  const timeout = options.timeout ?? (options.kind === 'error' ? 8000 : 3800);
  if (timeout) setTimeout(() => el.remove(), timeout);
  return el;
}

// A long-running task shown as a toast with progress and a cancel button.
export function progressToast(message, onCancel) {
  if (!toastHost) {
    toastHost = h('div.toasts');
    document.body.append(toastHost);
  }
  const bar = h('div', { style: { width: '0%' } });
  const text = h('span', message);
  const el = h('div.toast', h('div', { style: { display: 'flex', flexDirection: 'column', gap: '6px', minWidth: '260px' } }, text, h('div.progress', bar)),
    onCancel ? h('div.actions', h('button.btn.small', { type: 'button', onclick: () => { onCancel(); el.remove(); } }, 'Cancel')) : null);
  toastHost.append(el);
  return {
    update(fraction, note) {
      bar.style.width = `${Math.round(Math.max(0, Math.min(1, fraction)) * 100)}%`;
      if (note) text.textContent = note;
    },
    done(note, kind = 'ok') {
      el.remove();
      if (note) toast(note, { kind });
    },
    fail(note) {
      el.remove();
      toast(note, { kind: 'error' });
    },
  };
}
