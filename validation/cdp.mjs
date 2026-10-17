// A minimal Chrome DevTools Protocol driver for the validation harness (validation/viewer.mjs) and
// the documentation's screenshots (docs/capture/capture.mjs): starts headless Chrome (or Chromium,
// Edge, Brave; set CHROME to choose), evaluates scripts in a page, emulates the color scheme and
// captures the page. No dependencies beyond Node 22. Adapted from MaveScape 0.8.0
// docs/capture/cdp.mjs.

import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const CANDIDATES = [
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
  '/Applications/Brave Browser.app/Contents/MacOS/Brave Browser',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
];

export function findChrome() {
  return process.env.CHROME ?? CANDIDATES.find((path) => existsSync(path)) ?? null;
}

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// width × height CSS pixels at `scale` device pixels per CSS pixel. args: more of Chrome's flags.
export async function launch({ width = 1600, height = 1000, scale = 1, args = [] } = {}) {
  const chromePath = findChrome();
  if (!chromePath) throw new Error('No Chrome, Chromium, Edge or Brave found; set CHROME to its executable.');
  const profile = mkdtempSync(join(tmpdir(), 'situcraft-harness-'));
  const chrome = spawn(chromePath, ['--headless=new', '--remote-debugging-port=0', `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check', '--hide-scrollbars', '--force-color-profile=srgb', `--window-size=${width},${height}`, ...args, 'about:blank'], { stdio: 'ignore' });
  let page = null;
  // A cold start on a CI runner can take tens of seconds.
  const deadline = Date.now() + 60000;
  while (!page && Date.now() < deadline) {
    try {
      const active = Number(readFileSync(join(profile, 'DevToolsActivePort'), 'utf8').split('\n')[0]);
      if (active) page = (await (await fetch(`http://127.0.0.1:${active}/json`, { signal: AbortSignal.timeout(5000) })).json()).find((t) => t.type === 'page');
    } catch { /* not up yet */ }
    if (!page) await sleep(200);
  }
  if (!page) {
    chrome.kill();
    throw new Error('Chrome did not start.');
  }
  const socket = new WebSocket(page.webSocketDebuggerUrl);
  const opened = await new Promise((resolve) => {
    const timer = setTimeout(() => resolve(false), 15000);
    socket.addEventListener('open', () => { clearTimeout(timer); resolve(true); }, { once: true });
    socket.addEventListener('error', () => { clearTimeout(timer); resolve(false); }, { once: true });
  });
  if (!opened) {
    socket.close();
    chrome.kill('SIGKILL');
    throw new Error('Chrome did not open its debugging connection.');
  }
  let next = 1;
  const pending = new Map();
  const listeners = new Map();
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(event.data);
    if (message.method) for (const listener of listeners.get(message.method) ?? []) listener(message.params);
    if (!message.id || !pending.has(message.id)) return;
    const { resolve, reject } = pending.get(message.id);
    pending.delete(message.id);
    if (message.error) reject(new Error(message.error.message));
    else resolve(message.result);
  });
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const id = next++;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
  await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: scale, mobile: false });
  await send('Page.enable');
  await send('Runtime.enable');
  return {
    send,
    on(method, listener) {
      if (!listeners.has(method)) listeners.set(method, []);
      listeners.get(method).push(listener);
    },
    async goto(url, wait = 2500) {
      await send('Page.navigate', { url });
      await sleep(wait);
    },
    async eval(expression) {
      const result = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
      if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description ?? result.exceptionDetails.text);
      return result.result.value;
    },
    // A click of the left button at (x, y) in CSS pixels, as a mouse makes it (pointer events and
    // all).
    async click(x, y) {
      for (const type of ['mouseMoved', 'mousePressed', 'mouseReleased']) {
        await send('Input.dispatchMouseEvent', { type, x, y, button: type === 'mouseMoved' ? 'none' : 'left', buttons: type === 'mousePressed' ? 1 : 0, clickCount: type === 'mouseMoved' ? 0 : 1 });
      }
    },
    async theme(dark) {
      await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: dark ? 'dark' : 'light' }] });
    },
    // format: 'jpeg', 'webp' or 'png'; quality for the first two.
    async capture(file, { format = 'jpeg', quality = 86 } = {}) {
      const { data } = await send('Page.captureScreenshot', { format, ...(format === 'png' ? {} : { quality }), captureBeyondViewport: false });
      writeFileSync(file, Buffer.from(data, 'base64'));
    },
    async close() {
      const exited = new Promise((resolve) => chrome.once('exit', resolve));
      send('Browser.close').catch(() => { /* closing */ });
      await Promise.race([exited, sleep(5000)]);
      if (chrome.exitCode === null && chrome.signalCode === null) chrome.kill('SIGKILL');
      try { rmSync(profile, { recursive: true, force: true, maxRetries: 5 }); } catch { /* a temporary folder */ }
    },
  };
}
