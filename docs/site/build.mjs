// Builds the SituCraft website (published on GitHub Pages from the gh-pages branch) from the page
// fragments in pages/. Usage:
//
//   node docs/site/build.mjs FOLDER        build into a folder, to check and preview the site
//   node docs/site/build.mjs --publish     build into ../situcraft-site, the gh-pages checkout
//
// Committing the gh-pages checkout publishes the site, so the build writes into it (or any
// checkout of gh-pages) only with --publish: when a release is out. Otherwise give a folder.
//
// Each page starts with a front-matter block (title, description, and lede for documentation
// pages) and holds only its own content: the header, documentation menu, pager and footer are
// added here. <shot name="viewer" alt="…">caption</shot> becomes a figure with the screenshot in
// both themes, docs/images/viewer-light.webp and viewer-dark.webp (written by docs/capture, copied
// to assets/img), of which the page shows the one matching its theme. The build then checks every
// local link, anchor and image, and fails if one is broken. Adapted from MaveScape 0.8.0's
// docs/site/build.mjs.

import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '../..');
const args = process.argv.slice(2);
const publish = args.includes('--publish');
const folder = args.find((a) => !a.startsWith('--'));
if (!folder && !publish) {
  console.error('Give a folder to build into (node docs/site/build.mjs FOLDER), or --publish to build into ../situcraft-site, the gh-pages checkout, for a release.');
  process.exit(2);
}
const out = resolve(folder ?? join(repo, '..', 'situcraft-site'));
// A checkout of the gh-pages branch publishes what is committed in it.
const pagesCheckout = (() => {
  try {
    return /ref: refs\/heads\/gh-pages\s*$/.test(readFileSync(join(out, '.git', 'HEAD'), 'utf8'));
  } catch {
    return false;
  }
})();
if (pagesCheckout && !publish) {
  console.error(`${out} is a checkout of gh-pages, which publishes the website: add --publish to build into it (when a release is out), or give another folder.`);
  process.exit(2);
}
const SITE = 'https://robert-mcdermott.github.io/situcraft/';
const GITHUB = 'https://github.com/robert-mcdermott/situcraft';
const VERSION = /var version = "([^"]+)"/.exec(readFileSync(join(repo, 'main.go'), 'utf8'))[1];

// The documentation, in reading order.
const DOCS = [
  { group: 'Start', pages: [['index', 'Overview'], ['getting-started', 'Getting started'], ['opening-data', 'Opening a bundle']] },
  { group: 'The tissue', pages: [['viewer', 'The Viewer'], ['qc', 'Quality control']] },
  { group: 'Segmentations', pages: [['segmentations', 'Segmentations'], ['refinement', 'Refinement'], ['comparing', 'Comparing and deciding'], ['exports', 'Exports']] },
  { group: 'Cell types', pages: [['clusters', 'Clusters and the map'], ['labels', 'Labels']] },
  { group: 'Pathology', pages: [['images', 'Images and alignment'], ['regions', 'Regions and reviews']] },
  { group: 'Spatial', pages: [['spatial', 'Spatial analysis'], ['niches', 'Niches and genes']] },
  { group: 'Cohorts', pages: [['samples', 'Samples']] },
  { group: 'Reference', pages: [['remote-control', 'Remote control'], ['record', 'The record'], ['troubleshooting', 'Troubleshooting']] },
];
const ORDER = DOCS.flatMap((g) => g.pages);

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

function parse(file) {
  const text = readFileSync(file, 'utf8');
  const match = /^---\n([\s\S]*?)\n---\n/.exec(text);
  if (!match) throw new Error(`${file}: no front matter`);
  const meta = {};
  for (const line of match[1].split('\n')) {
    const at = line.indexOf(':');
    if (at > 0) meta[line.slice(0, at).trim()] = line.slice(at + 1).trim();
  }
  for (const key of ['title', 'description']) if (!meta[key]) throw new Error(`${file}: no ${key}`);
  return { meta, body: text.slice(match[0].length) };
}

const ICON = {
  github: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .5a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2c-3.2.7-3.88-1.37-3.88-1.37-.53-1.34-1.29-1.7-1.29-1.7-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.71 1.26 3.37.96.1-.75.4-1.26.73-1.55-2.56-.29-5.25-1.28-5.25-5.69 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.84 1.19 3.1 0 4.42-2.7 5.4-5.26 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .5Z"/></svg>',
  moon: '<svg class="icon-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z"/></svg>',
  sun: '<svg class="icon-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>',
  menu: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16"/></svg>',
};

const brand = (root) => `<a class="brand" href="${home(root)}"><img src="${root}assets/img/logo.svg" alt="" width="30" height="30" /><span>Situ<b>Craft</b></span></a>`;

function head(meta, root, path) {
  const title = meta.title === 'SituCraft' ? meta.title : `${meta.title} · SituCraft`;
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>${esc(meta.head ?? title)}</title>
  <meta name="description" content="${esc(meta.description)}" />
  <link rel="canonical" href="${SITE}${path}" />
  <meta property="og:type" content="website" />
  <meta property="og:title" content="${esc(meta.title)}" />
  <meta property="og:description" content="${esc(meta.description)}" />
  <meta property="og:url" content="${SITE}${path}" />
  <meta property="og:image" content="${SITE}assets/img/og-image.jpg" />
  <meta name="twitter:card" content="summary_large_image" />
  <meta name="theme-color" content="#07090e" />
  <link rel="icon" href="${root}assets/img/logo.svg" type="image/svg+xml" />
  <link rel="stylesheet" href="${root}assets/css/site.css" />
  <script>try { const t = localStorage.getItem('situcraft-theme'); if (t) document.documentElement.dataset.theme = t; } catch (e) {}</script>
  <script src="${root}assets/js/site.js" defer></script>
</head>
<body>
  <a class="skip-link" href="#main">Skip to content</a>
`;
}

// root is '' for top-level pages, so links to the home page need './'.
const home = (root) => root || './';

function header(root, current) {
  const link = (href, label, key) => `<a href="${href.startsWith('#') ? home(root) : root}${href}"${current === key ? ' aria-current="page"' : ''}>${label}</a>`;
  return `
  <header class="site-header">
    <div class="container">
      ${brand(root)}
      <nav class="site-nav" id="site-nav" aria-label="Main">
        ${link('#features', 'Features', 'features')}
        ${link('docs/', 'Documentation', 'docs')}
        ${link('install.html', 'Install', 'install')}
        ${link('science.html', 'Science', 'science')}
      </nav>
      <div class="header-actions">
        <a class="icon-button" href="${GITHUB}" aria-label="SituCraft on GitHub">
          ${ICON.github}
        </a>
        <button class="icon-button theme-toggle" type="button" aria-label="Switch between light and dark">
          ${ICON.moon}
          ${ICON.sun}
        </button>
        <button class="icon-button nav-toggle" type="button" aria-label="Menu" aria-controls="site-nav" aria-expanded="false">
          ${ICON.menu}
        </button>
      </div>
    </div>
  </header>
`;
}

function footer(root) {
  return `
  <footer class="site-footer">
    <div class="container">
      <div class="footer-grid">
        <div class="footer-about">
          ${brand(root)}
          <p>A local-first workbench for imaging-based spatial transcriptomics: Xenium bundles checked, segmented, typed, reviewed and analyzed on your own computer, every decision on record. Free and open source under the Apache License 2.0.</p>
        </div>
        <div>
          <h4>Product</h4>
          <ul>
            <li><a href="${home(root)}#features">Features</a></li>
            <li><a href="${root}install.html">Install</a></li>
            <li><a href="${root}science.html">Science</a></li>
            <li><a href="${GITHUB}/blob/main/CHANGELOG.md">Changelog</a></li>
          </ul>
        </div>
        <div>
          <h4>Documentation</h4>
          <ul>
            <li><a href="${root}docs/getting-started.html">Getting started</a></li>
            <li><a href="${root}docs/segmentations.html">Segmentations</a></li>
            <li><a href="${root}docs/clusters.html">Cell types</a></li>
            <li><a href="${root}docs/spatial.html">Spatial analysis</a></li>
            <li><a href="${root}docs/troubleshooting.html">Troubleshooting</a></li>
          </ul>
        </div>
        <div>
          <h4>Project</h4>
          <ul>
            <li><a href="${GITHUB}">GitHub</a></li>
            <li><a href="${GITHUB}/releases">Releases</a></li>
            <li><a href="${GITHUB}/issues">Issues</a></li>
            <li><a href="${root}science.html#cite">Cite SituCraft</a></li>
          </ul>
        </div>
      </div>
      <div class="footer-bottom">SituCraft is licensed under the Apache License 2.0. It is for research use only: it is not a medical device and makes no diagnostic claims. See <a href="${root}science.html#credits">credits</a>.</div>
    </div>
  </footer>
</body>
</html>
`;
}

function sidebar(current) {
  const groups = DOCS.map((g) => `        <h4>${g.group}</h4>
        <ul>
${g.pages.map(([slug, label]) => `          <li><a href="${slug === 'index' ? './' : `${slug}.html`}"${slug === current ? ' aria-current="page"' : ''}>${label}</a></li>`).join('\n')}
        </ul>`).join('\n');
  return `
    <aside class="docs-sidebar" aria-label="Documentation">
      <button class="btn docs-menu-toggle" type="button" aria-expanded="false">Documentation menu <span aria-hidden="true">▾</span></button>
      <nav>
${groups}
      </nav>
    </aside>
`;
}

function pager(current) {
  const i = ORDER.findIndex(([slug]) => slug === current);
  const href = (slug) => (slug === 'index' ? './' : `${slug}.html`);
  const prev = ORDER[i - 1];
  const next = ORDER[i + 1];
  return `
      <nav class="doc-pager" aria-label="Pages">
        ${prev ? `<a class="prev" href="${href(prev[0])}"><span>Previous</span>${prev[1]}</a>` : '<span></span>'}
        ${next ? `<a class="next" href="${href(next[0])}"><span>Next</span>${next[1]}</a>` : ''}
      </nav>`;
}

// <shot name="x" alt="…" [window] [eager]>caption</shot> → a figure with both themes' screenshot.
const shotsUsed = new Set();
function shots(html, root) {
  return html.replace(/<shot\s+([^>]*)>([\s\S]*?)<\/shot>/g, (_, attrs, caption) => {
    const attr = (name) => new RegExp(`${name}="([^"]*)"`).exec(attrs)?.[1];
    const name = attr('name');
    const alt = attr('alt');
    if (!name || !alt) throw new Error(`<shot> needs name and alt: ${attrs}`);
    shotsUsed.add(name);
    const window = /\bwindow\b/.test(attrs);
    const loading = /\beager\b/.test(attrs) ? 'fetchpriority="high"' : 'loading="lazy"';
    const img = (theme) => `<img class="shot-${theme}" src="${root}assets/img/${name}-${theme}.webp" width="2000" height="1250" ${loading} alt="${alt}" />`;
    return `<figure${window ? ' class="hero-shot"' : ''}>
        <div class="shot${window ? ' shot-window" data-url="127.0.0.1:8840' : ''}">${img('dark')}${img('light')}</div>${caption.trim() ? `
        <figcaption>${caption.trim()}</figcaption>` : ''}
      </figure>`;
  });
}

const fill = (html) => html.replaceAll('{{version}}', VERSION).replaceAll('{{github}}', GITHUB);

function write(path, html) {
  const file = join(out, path);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, html);
}

const written = [];

// Top-level pages.
for (const [slug, key] of [['index', 'home'], ['install', 'install'], ['science', 'science'], ['404', '404']]) {
  const { meta, body } = parse(join(here, 'pages', `${slug}.html`));
  const root = slug === '404' ? '/situcraft/' : '';
  const path = slug === 'index' ? '' : `${slug}.html`;
  // layout: page is a single documentation-style column with its own contents list.
  const main = meta.layout === 'page' ? `
  <div class="page-layout">
    <main id="main" class="doc">
      <h1>${meta.heading ?? meta.title}</h1>
      <p class="doc-lede">${meta.lede}</p>

${fill(shots(body, root)).trimEnd()}
    </main>

    <aside class="toc" aria-label="On this page">
      <h4>On this page</h4>
      <ul></ul>
    </aside>
  </div>
` : `
  <main id="main">
${fill(shots(body, root)).trimEnd()}
  </main>
`;
  write(`${slug}.html`, head(meta, root, path) + header(root, key) + main + footer(root));
  if (slug !== '404') written.push(path);
}

// Documentation.
for (const [slug] of ORDER) {
  const { meta, body } = parse(join(here, 'pages', 'docs', `${slug}.html`));
  if (!meta.lede) throw new Error(`docs/${slug}.html: no lede`);
  const root = '../';
  const path = `docs/${slug === 'index' ? '' : `${slug}.html`}`;
  write(`docs/${slug}.html`, head(meta, root, path) + header(root, 'docs') + `
  <div class="docs-layout">${sidebar(slug)}
    <main id="main" class="doc">
      <h1>${meta.title}</h1>
      <p class="doc-lede">${meta.lede}</p>

${fill(shots(body, root)).trimEnd()}
${pager(slug)}
    </main>

    <aside class="toc" aria-label="On this page">
      <h4>On this page</h4>
      <ul></ul>
    </aside>
  </div>
` + footer(root));
  written.push(path);
}

// Assets, and the files GitHub Pages and crawlers look for.
for (const dir of ['assets/css', 'assets/js', 'assets/img']) mkdirSync(join(out, dir), { recursive: true });
copyFileSync(join(here, 'assets/site.css'), join(out, 'assets/css/site.css'));
copyFileSync(join(here, 'assets/site.js'), join(out, 'assets/js/site.js'));
copyFileSync(join(repo, 'web/favicon.svg'), join(out, 'assets/img/logo.svg'));
for (const name of shotsUsed) for (const theme of ['light', 'dark']) {
  const image = join(repo, 'docs/images', `${name}-${theme}.webp`);
  if (existsSync(image)) copyFileSync(image, join(out, 'assets/img', `${name}-${theme}.webp`));
}
// Link previews: social sites want PNG or JPEG, so the preview image is kept as a JPEG.
if (existsSync(join(here, 'assets/og-image.jpg'))) copyFileSync(join(here, 'assets/og-image.jpg'), join(out, 'assets/img/og-image.jpg'));
write('.nojekyll', '');
write('robots.txt', `User-agent: *\nAllow: /\n\nSitemap: ${SITE}sitemap.xml\n`);
write('sitemap.xml', `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${written.map((p) => `  <url><loc>${SITE}${p}</loc></url>`).join('\n')}
</urlset>
`);

// Check every local link, anchor and image.
const problems = [];
const ids = new Map();
const idsOf = (file) => {
  if (!ids.has(file)) ids.set(file, new Set([...readFileSync(file, 'utf8').matchAll(/\sid="([^"]+)"/g)].map((m) => m[1])));
  return ids.get(file);
};
const pages = [];
const walk = (dir) => {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.startsWith('.') && entry.name !== '.nojekyll') continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) walk(full);
    else if (entry.name.endsWith('.html')) pages.push(full);
  }
};
walk(out);
for (const file of pages) {
  if (file.endsWith('404.html')) continue;
  const html = readFileSync(file, 'utf8');
  for (const [, url] of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
    if (/^(https?:|mailto:|data:)/.test(url)) continue;
    const [path, anchor] = url.split('#');
    let target = path ? resolve(dirname(file), path) : file;
    if (path.endsWith('/') || (existsSync(target) && !target.endsWith('.html') && !/\.\w+$/.test(target))) target = join(target, 'index.html');
    const where = relative(out, file);
    if (!existsSync(target)) problems.push(`${where}: ${url} does not exist`);
    else if (anchor && target.endsWith('.html') && !idsOf(target).has(anchor)) problems.push(`${where}: ${url} has no #${anchor}`);
  }
  // Links into the repository's own files (FORMATS.md and the like) must name files it has.
  for (const [, file2] of html.matchAll(/href="https:\/\/github\.com\/robert-mcdermott\/situcraft\/blob\/main\/([^"#]+)/g)) {
    if (!existsSync(join(repo, file2))) problems.push(`${relative(out, file)}: the repository has no ${file2}`);
  }
}
for (const name of shotsUsed) for (const theme of ['light', 'dark']) {
  if (!existsSync(join(repo, 'docs/images', `${name}-${theme}.webp`))) problems.push(`screenshot docs/images/${name}-${theme}.webp is missing (node docs/capture/capture.mjs ${name})`);
}
console.log(`Built ${pages.length} pages into ${out} (SituCraft ${VERSION}, ${shotsUsed.size} screenshots).`);
if (problems.length) {
  console.error(`${problems.length} problem${problems.length === 1 ? '' : 's'}:\n  ${[...new Set(problems)].join('\n  ')}`);
  process.exitCode = 1;
}
