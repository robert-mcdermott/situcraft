// Checks the website as built (docs/site/build.mjs) in headless Chrome: every page in both themes,
// at a desktop's width and a phone's, against axe-core's WCAG 2.1 A and AA rules, and at the
// phone's width for anything that makes the page wider than the screen. Writes docs/site/audit.json
// (each page, theme and width with what was found) and lists the problems; exits 1 on any.
//
//   node docs/site/audit.mjs
//
// It needs axe-core (node validation/fetch.mjs axe-core) and Chrome, Chromium, Edge or Brave (set
// CHROME to choose; SITUCRAFT_CHROME_ARGS adds flags). The weekly workflow runs it.

import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, extname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { launch } from '../../validation/cdp.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '../..');
const AXE = join(repo, 'validation/cache/axe-core/axe.min.js');
if (!existsSync(AXE)) {
  console.error('axe-core is missing: run node validation/fetch.mjs axe-core');
  process.exit(2);
}

const site = mkdtempSync(join(tmpdir(), 'situcraft-site-'));
const built = spawnSync(process.execPath, [join(here, 'build.mjs'), site], { stdio: 'inherit' });
if (built.status !== 0) process.exit(built.status ?? 1);

// The built site, served as GitHub Pages serves it: a folder's index.html for the folder.
const TYPES = { '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.jpg': 'image/jpeg', '.xml': 'application/xml', '.txt': 'text/plain' };
const server = createServer((req, res) => {
  let file = join(site, decodeURIComponent(new URL(req.url, 'http://x').pathname));
  if (existsSync(file) && statSync(file).isDirectory()) file = join(file, 'index.html');
  if (!file.startsWith(site) || !existsSync(file)) {
    res.writeHead(404);
    res.end();
    return;
  }
  res.writeHead(200, { 'Content-Type': TYPES[extname(file)] ?? 'application/octet-stream' });
  res.end(readFileSync(file));
});
await new Promise((done) => server.listen(0, '127.0.0.1', done));
const base = `http://127.0.0.1:${server.address().port}/`;

const pages = [];
const walk = (dir) => {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) walk(full);
    else if (entry.name.endsWith('.html') && entry.name !== '404.html') pages.push(relative(site, full).split('\\').join('/'));
  }
};
walk(site);
pages.sort();

const WIDTHS = [{ name: 'desktop', width: 1280, height: 900 }, { name: 'phone', width: 390, height: 844 }];
const results = [];
try {
  for (const w of WIDTHS) {
    // CI's runners: SITUCRAFT_CHROME_ARGS adds Chrome's flags, as the viewer suite takes them.
    const b = await launch({ width: w.width, height: w.height, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(/\s+/).filter(Boolean) });
    try {
      for (const theme of ['light', 'dark']) {
        await b.theme(theme === 'dark');
        for (const page of pages) {
          await b.goto(base + page, 600);
          await b.eval(`if (!window.axe) (0, eval)(${JSON.stringify(readFileSync(AXE, 'utf8'))}); true`);
          const found = await b.eval(`(async () => {
            const result = await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] }, resultTypes: ['violations'] });
            const violations = result.violations.map((v) => ({ id: v.id, impact: v.impact, help: v.help, count: v.nodes.length, nodes: v.nodes.slice(0, 3).map((n) => n.target.join(' ')) }));
            // Wider than the screen: the page's own width, and the widest element that passes it.
            const overflow = document.documentElement.scrollWidth - innerWidth;
            let widest = null;
            if (overflow > 1) {
              for (const el of document.querySelectorAll('body *')) {
                const r = el.getBoundingClientRect();
                if (r.right > innerWidth + 1 && (!widest || r.right > widest.right)) widest = { right: Math.round(r.right), tag: el.tagName.toLowerCase() + (el.className && typeof el.className === 'string' ? '.' + el.className.trim().split(/\\s+/).join('.') : '') };
              }
            }
            return { violations, overflow: Math.max(0, overflow), widest };
          })()`);
          results.push({ page, theme, width: w.name, ...found });
        }
      }
    } finally {
      await b.close();
    }
  }
} finally {
  server.close();
  rmSync(site, { recursive: true, force: true });
}

const problems = [];
for (const r of results) {
  for (const v of r.violations) problems.push(`${r.page} (${r.theme}, ${r.width}): ${v.id} ×${v.count} (${v.impact}) ${v.help}: ${v.nodes.join(', ')}`);
  if (r.width === 'phone' && r.overflow > 1) problems.push(`${r.page} (${r.theme}, phone): ${r.overflow}px wider than the screen${r.widest ? `, ${r.widest.tag} to ${r.widest.right}px` : ''}`);
}
const record = results.map((r) => ({ page: r.page, theme: r.theme, width: r.width, violations: r.violations, overflow: r.overflow }));
writeFileSync(join(here, 'audit.json'), `${JSON.stringify(record, null, 1)}\n`);
console.log(`${pages.length} pages in two themes at two widths: ${results.length} checks, ${problems.length} problem${problems.length === 1 ? '' : 's'} (docs/site/audit.json).`);
if (problems.length) {
  console.error(`  ${problems.join('\n  ')}`);
  process.exitCode = 1;
}
