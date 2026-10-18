# The SituCraft website

The website at <https://robert-mcdermott.github.io/situcraft/> is built from this folder. GitHub
Pages serves it from the `gh-pages` branch of this repository. That branch holds only the built
site, with no history from `main`.

```text
docs/site/
  build.mjs          the generator: layout, menus, screenshots, link check
  audit.mjs          the pages in headless Chrome: axe-core in both themes, a phone's width
  audit.json         the audit's latest result, page by page
  assets/            site.css, site.js and the link-preview image (og-image.jpg)
  pages/             index (home), install, science and 404
  pages/docs/        the user guide, one file per page
docs/images/         the screenshots, <scene>-light.webp and <scene>-dark.webp
docs/capture/        the script that takes the screenshots
```

The build and its assets are adapted from MaveScape 0.8.0's website.

## Updating the site

1. Edit the pages in `pages/` (see [Writing pages](#writing-pages)).
2. While you work, build into any folder to check the pages:

   ```sh
   node docs/site/build.mjs /tmp/situcraft-site-check
   ```

   The build checks every local link, anchor and screenshot, and every link into this
   repository's files. It exits with an error and lists the problems if any are broken. CI runs
   it on every commit.
3. Check the pages in both themes and at a phone's width:

   ```sh
   node docs/site/audit.mjs
   ```

   It builds the site, serves it, and runs axe-core's WCAG 2.1 A and AA rules on every page in
   both themes at a desktop's width and a phone's, and checks that nothing makes a page wider
   than a phone's screen. It writes `audit.json` and exits with an error on any problem. It needs
   axe-core (`node validation/fetch.mjs axe-core`) and Chrome, Chromium, Edge or Brave (`CHROME`
   chooses).
4. When a release is out, build into your checkout of the `gh-pages` branch, `../situcraft-site`
   beside this repository. Committing that checkout publishes the site, so the build writes into
   it only with `--publish`:

   ```sh
   node docs/site/build.mjs --publish
   ```

5. Preview the site:

   ```sh
   python3 -m http.server 8854 --directory ../situcraft-site
   ```

   Then open <http://localhost:8854>.
6. Publish it:

   ```sh
   cd ../situcraft-site
   git add -A
   git commit -m "Update the website"
   git push
   ```

   GitHub Pages republishes within a minute or two.

Commit the page sources in this repository as usual; the built site is committed only on
`gh-pages`.

### Creating the `gh-pages` branch (once)

The branch holds only the built site, with no history from `main`. Make it in a new folder beside
this repository, then turn on GitHub Pages for the `gh-pages` branch (Settings, Pages, "Deploy
from a branch", `gh-pages`, `/ (root)`):

```sh
node docs/site/build.mjs ../situcraft-site
cd ../situcraft-site
git init -b gh-pages
git remote add origin git@github.com:robert-mcdermott/situcraft.git
git add -A
git commit -m "The website"
git push -u origin gh-pages
```

After that, build into the checkout with `--publish`.

### First time on a new computer

Clone just the `gh-pages` branch, beside this repository:

```sh
git clone --branch gh-pages --single-branch git@github.com:robert-mcdermott/situcraft.git ../situcraft-site
```

The build only adds and replaces files. To remove a page, delete its built file from the checkout
as well.

## Writing pages

Each page starts with a front-matter block. The rest of the page is the content only: the build
adds the header, the documentation menu, the previous/next links, the "On this page" list and the
footer.

```html
---
title: Spatial analysis
description: One sentence for search engines and link previews.
lede: The paragraph under the title (documentation pages).
---
      <h2 id="graphs">Which cells are neighbors</h2>
      <p>…</p>
```

- Every `h2` needs an `id`. The "On this page" list is made from them, and other pages link to
  them (`spatial.html#enrichment`).
- `layout: page` (used by Install and Science) gives a top-level page the documentation column and
  its "On this page" list.
- Screenshots are written as:

  ```html
  <shot name="enrichment" alt="What the screenshot shows">The caption, which may contain HTML.</shot>
  ```

  This becomes a figure holding `docs/images/enrichment-dark.webp` and `enrichment-light.webp`,
  and the page shows the one matching its theme. Add `window` to draw it in a window frame (as on
  the home page), and `eager` for the first image on a page.
- `{{version}}` becomes the version in `main.go`, and `{{github}}` becomes the repository URL. Link
  to the references kept in the repository as `{{github}}/blob/main/docs/FORMATS.md`.
- To add a documentation page, create `pages/docs/<name>.html` and add it to `DOCS` at the top of
  `build.mjs`. That list sets the menu and the previous/next order.

## Screenshots

The screenshots are captured from the examples, in both themes, by driving SituCraft through its
remote control (`--remote-control`) in headless Chrome. The script builds SituCraft from source and
runs each scene on an empty library, so every run gives the same pictures:

```sh
node docs/capture/capture.mjs                 # every scene, both themes
node docs/capture/capture.mjs remake moran    # some scenes
node docs/capture/capture.mjs remake --theme dark
```

Each scene is a function in `docs/capture/capture.mjs`. To add one, write its function there, run
it, and refer to it with `<shot name="…">`. The README uses the same images. `--audit` also checks
every captured scene with axe-core and records the result in `docs/capture/audit.json`.

## For each release

1. Re-capture the screenshots if the interface has changed, and commit them.
2. Check the pages against what changed, especially the guide pages for new or changed features,
   and on the home page the release's line under the title and the releases table.
3. Run `node docs/site/audit.mjs`.
4. Build, preview and publish after the release is out. The site advertises the new version, and
   its install commands fetch the latest release.
