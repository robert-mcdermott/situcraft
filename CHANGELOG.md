# Changelog

## 0.1.0 (unreleased)

SituCraft's first release, wave 1 of the [roadmap](situcraft-spec/roadmap.md): open a Xenium
output bundle, check it, and see the tissue, its transcripts and its cells at any zoom, with QC
linked to the tissue and a project that reopens instantly. Built slice by slice; this section grows
with each.

### Added

- **The program and its window (wave 1, slice 1).**
  - One self-contained program (`situcraft`) for macOS, Linux and Windows, x64 and ARM64.
  - It serves its embedded web app on 127.0.0.1, port 8840 or the next free one up to 8859, and
    opens it in a desktop window.
  - Its ports are clear of the sibling programs' (Proteoscope 8765–8814, CytoWeave 8770–8789,
    MaveScape 8820–8839), and a test keeps them so.
  - A second launch hands its paths to the running SituCraft.
  - The host and its security are adapted from MaveScape 0.8.0: a Host-header check against DNS
    rebinding, a same-origin API, a Content-Security-Policy and cross-origin isolation.
  - The content-addressed library is adapted from MaveScape 0.8.0 too, and holds projects.
- **Items named on this computer.**
  - On the command line or through **Open a bundle** (a path), SituCraft recognizes:
    - Xenium output bundles, by their folder, their `.zip` or their `experiment.xenium`;
    - Zarr stores, OME-TIFF images, Parquet and CSV tables, GeoJSON polygons and project archives;
    - folders of these, in natural order.
  - A bundle counts as one item: SituCraft never walks into it, serves it whole or copies it into
    the library.
  - The items are listed under **On this computer** in the project tree. Reading bundles comes in
    a later slice of this wave.
- **The workbench shell.**
  - A view rail in the order of the work: Data, Viewer, QC, Segmentation, then Cell types,
    Spatial, Cohort, Reports and Jobs. Each view says what it will do until it is built.
  - A start page with recent projects.
  - The project tree, an inspector, and a bottom drawer with the project's history and the
    session's log.
  - A status bar, the command palette (⌘K), undo and redo.
  - Light and dark themes, and color-vision-friendly colors.
  - Without its program, the page says how to start SituCraft: a web page alone cannot read data
    where it lies on the computer.
- **The project document** (`situcraft-project` version 1): datasets, artifacts, decisions, saved
  views and history. It is saved automatically to the library, and refused with a plain message
  when it is not a SituCraft project or was saved by a newer SituCraft.
- **The build commit** in the status bar and `/api/info`. Release builds embed it; other builds
  take Go's version-control stamp.
- **Installers** (`install.sh`, `install.ps1`) that check the download's SHA-256 and version.
- **CI and releases.**
  - CI tests each commit once (go vet, race tests, the six-platform cross-compile, the browser
    modules, validation, installer syntax). A pull request shows its branch's checks, and an
    identical merge is not tested again.
  - A release workflow builds the six binaries with their checksums once CI has passed on exactly
    the tagged files.

### Validation

- **Go tests of the host:**
  - security headers; foreign Host headers and cross-origin requests refused;
  - the project library's round trips; files stored under the hash the program computes; range
    requests;
  - bundles, Zarr stores and files found in folders in natural order, with a bundle's insides
    never listed, served or copied;
  - every web module embedded; the commit reported;
  - the port range clear of the siblings'.
- **`node --test` of the shared modules:** seeded random numbers, SHA-256, engine-independent
  logarithms and exponentials, color-vision simulation, the project document, and no
  engine-dependent math in modules that compute results.
- **`validation/run.mjs accessibility`:**
  - every text-on-surface pair of both themes reaches WCAG AA contrast, with color-vision-friendly
    colors off and on;
  - the palettes and status colors stay apart in protanopia, deuteranopia and tritanopia;
  - the diverging maps' two ends stay apart in every vision.
- **A release dry run** built the six binaries with `SHA256SUMS`. The macOS one:
  - printed `situcraft 0.1.0`;
  - served the embedded app with its commit in `/api/info`;
  - refused a foreign Host;
  - handed a file to a running SituCraft.
