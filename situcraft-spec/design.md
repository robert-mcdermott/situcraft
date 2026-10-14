# SituCraft design

This document explains how SituCraft is built and why. For code conventions and the data model,
see `conventions.md`. For the methods, formats and datasets behind these choices, see
`research.md`. For what SituCraft must do, see `requirements.md`. For the order of the work, see
`roadmap.md`.

## Goals

1. **One workflow from bundle to biology.** Open a Xenium output bundle, check it, see the tissue,
   segment cells, compare segmentations, register pathology images, type cells, analyze niches
   and compare samples, all in one project.
2. **Segmentation as a decision.** Every segmentation (the instrument's, SituCraft's own, or one
   imported from another tool) is an artifact that can be compared, reviewed and promoted with a
   recorded rationale. Alternatives are never deleted.
3. **Defensible numbers.** Every method is implemented in the open, cites its publication, and is
   checked automatically against independent references or known truth (`validation/`).
4. **Local and private.** Data stay on the computer. SituCraft serves only the loopback interface
   and sends nothing anywhere, except downloads of public example data the user asks for.
5. **Self-contained.** One program per platform. No Python, R, containers, GPU or other software
   is needed, segmentation included.
6. **Open.** Apache-2.0. No third-party runtime dependencies. Standard formats out: SpatialData
   Zarr, AnnData Zarr, Parquet, OME-TIFF, GeoJSON, CSV, Matrix Market.
7. **Research use.** Not a diagnostic device. Wording is fixed in `conventions.md`.

## Architecture

```
┌──────────────────────────── situcraft (one Go binary, standard library only) ─────────────────────────┐
│ host (adapted from MaveScape 0.8)                                                                     │
│   main.go        flags, startup, single instance, /api/info                                           │
│   security.go    loopback only, Host-header check, same-origin API, CSP                               │
│   window.go      desktop window (Chrome/Edge/Brave --app with its own profile) or default browser     │
│   store.go       library: projects/*.json, content-addressed files, trash                             │
│   remote.go, actions.go, connection.go, mcp.go, run.go, verify.go   automation (later waves)          │
│ data plane (new)                                                                                      │
│   internal/codec     zstd (RFC 8878), LZ4 block, Blosc, Snappy; deflate/gzip from the standard library │
│   internal/parquet   Parquet reader (flat schemas) and writer                                         │
│   internal/zarr      Zarr v2 and v3: directory and zip stores; reader and writer                      │
│   internal/tiff      TIFF/BigTIFF, OME-XML, SubIFD pyramids; reader and writer                        │
│   internal/jp2k      JPEG 2000 Part 1 decoder (TIFF compression 34712)                                │
│   internal/xenium    bundle scanner, version detection, validation, typed access                      │
│   internal/cache     derived data: image tile pyramids, transcript spatial index, geometry tiles      │
│   api_*.go           viewport queries: tiles, points, polygons, cells (binary, columnar responses)     │
│ compute (new, goroutines)                                                                             │
│   internal/segment   nucleus expansion; transcript-informed segmentation (sampler, outputs)           │
│   internal/compare   cell matching, difference classes, comparison metrics                            │
│   internal/register  image registration (later)                                                       │
│   internal/spatial   graphs and spatial statistics on large tables (later)                            │
│   internal/jobs      durable background jobs: spec, state, progress, cancellation, logs               │
│ embed web/ ──────────────────────────────────────────────────────────────────────────────────────┐   │
└──────────────────────────────────────────────────────────────────────────────────────────────────│───┘
                                                                                                   ▼
┌──────────────────────────────────────────── browser ──────────────────────────────────────────────────┐
│ web/app.js           shell, views, commands, shortcuts                                                │
│ web/ui/*.js          views and components (the only code that touches the DOM)                        │
│ web/render/*.js      WebGL2 tile renderer: 16-bit image channels, points, polygons, fills             │
│ web/workers/*.js     module workers: cell-level analytics, decoding binary responses                  │
│ web/lib/*.js         pure modules (no DOM), shared by page, workers and Node tests                    │
└───────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### Why the work is split between Go and the browser

CytoWeave and MaveScape compute everything in the browser because their data fit there. Xenium
data do not. A bundle holds millions to billions of transcripts and gigabytes of JPEG 2000 images,
and a segmentation run touches every transcript many times. So:

- **Go owns** file I/O, decoding, indexing, viewport queries, segmentation, comparison over
  transcripts, and any statistic over transcript-scale tables. Go gives us goroutines across all
  cores, memory-mapped columnar caches, and a single cross-compiled binary.
- **The browser owns** rendering, interaction, and cell-level analytics. Cell tables (10⁴–10⁶
  rows) fit typed arrays and workers, as in the siblings.
- Unlike the siblings, the web app **needs its host**. There is no static-hosting mode, except the
  redacted static result package (wave 8), which ships precomputed tiles.

### The derived-data cache

Source bundles are read in place and never modified. Everything SituCraft derives lives in its
cache (`<UserCacheDir>/situcraft/`, with a size cap and an eviction policy):

- **Image pyramids.** JPEG 2000 tiles are decoded once into SituCraft's own tile store
  (uint16, deflate-compressed, 512×512, one file per level per channel, with an index). After
  that, serving a tile is a read and an inflate. Pyramid levels missing from the source are
  computed.
- **Transcript index.** Transcripts are partitioned into a quadtree of spatial tiles. Each tile is
  stored as columnar arrays: x and y (float32, µm), gene (uint16), qv (uint8), z (float32), and
  cell (uint32 index into the segmentation's cell table). Each gene has per-level density grids,
  so the whole-tissue view never touches points.
- **Geometry tiles.** Cell and nucleus polygons are bucketed by tile and simplified per level.
- Each cache entry records the source files' sizes, modification times and checksums, plus
  SituCraft's cache-format version. Entries are built in a temporary directory and renamed into
  place, so a crash never leaves a half-built entry. They resume where they stopped and are
  reused on reopen.

### Segmentation built in

Segmentation is SituCraft's own code (`internal/segment`), not an external program:

- **Nucleus expansion.** Nuclei are grown up to a distance, bounded by their neighbors (a
  distance-limited Voronoi partition), and transcripts are assigned by point in polygon. It is the
  baseline, and the Lung example bundle lets us check it against the instrument's own 5 µm
  expansion.
- **Transcript-informed segmentation** follows the generative model and sampler of Jones et al.
  (2025): a voxel lattice, Poisson transcript likelihoods with per-gene background, a
  gamma–negative-binomial mixture of expression states, hard morphology and connectivity
  constraints, transcript repositioning, multi-resolution sampling, and initialization from
  nuclei (`research.md` §3). Unlike the published implementation, it is **deterministic for a
  seed regardless of thread count**: each parallel block draws from its own seeded random stream,
  in a fixed update order.
- **Outputs** are converted to polygons (per z layer and consensus), transcript assignments with
  probabilities, and counts. They enter the artifact system like any other segmentation.
- **Imported segmentations** come from SpatialData Zarr, Baysor-style CSV plus GeoJSON, Xenium
  Ranger outputs, or GeoJSON polygons plus an assignment table. This is how results from external
  tools are compared, without SituCraft running those tools.

### Jobs

Long computations (indexing, segmentation, comparison, registration, permutation tests) are jobs:

- Each job has an immutable JSON spec and a state (`declared → queued → running → validating →
  complete`, or `cancelled | failed | invalid | orphaned`).
- A job keeps an append-only event log and its outputs in a temporary directory until it is
  validated and promoted.
- Jobs run in the host and survive browser refresh. Closing the window while a job runs asks
  whether to keep SituCraft running. On restart, an interrupted job is `orphaned` and can be
  resumed or discarded.
- Progress and logs stream to the page over server-sent events. Cancellation is cooperative and
  checked often.

### The host, from the siblings

| From | What | How much changes |
| --- | --- | --- |
| MaveScape 0.8 | `security.go`, `window.go`, `connection.go`, `records.go` | renames only |
| MaveScape 0.8 | `main.go`, `local.go`, `store.go` | renames; file kinds (bundle directories, zips, OME-TIFF, GeoJSON, Parquet, CSV) |
| MaveScape 0.8 | `remote.go`, `output.go`, `mcp.go` framework, `run.go`/`verify.go` pattern | transport verbatim; actions and tools new |
| MaveScape 0.8 | `web/ui/dom.js`, `overlays.js`, `store.js`, `palette.js`, `icons.js`, drawer, inspector, status bar | renames |
| MaveScape 0.8 | `web/lib/random.js`, `sha256.js`, `zip.js`, `png.js`, `pdf.js`, `colormaps.js`, `colorvision.js`, `stats.js`, `limma.js`, `figure-svg.js`, `methods.js`, `certificate.js` pattern | copied with tests when a slice needs them |
| MaveScape 0.8 | CI and release workflows, installers, `validation/fetch.mjs` + `sources.json`, `docs/capture`, `clients/generate.mjs` | renames; CI as below |

Each copied file keeps an origin comment ("Adapted from MaveScape 0.8.0 security.go"), so fixes
can be carried across.

### Network and security

As in the siblings:
- SituCraft listens on 127.0.0.1 only and requires a loopback Host header.
- API calls must be same-origin, under a strict CSP.
- Paths can be opened only through the file chooser, the command line or a project.
- Processes, if any are ever started (only the optional Xenium Ranger handoff), get argument
  arrays, never shell strings.
- Archive extraction is guarded against path traversal and decompression bombs.

The only outbound requests are example-data downloads the user starts, from the URLs in the
example manifest, checked against their checksums.

Default port 8840 (8840–8859). The siblings use 8765–8839.

## Development data

The data are far larger than the siblings', and they never go in the repository or the binary.

- **Primary: 10x Xenium V1 human Breast 2-FOV** (XOA 2.0; multimodal cell segmentation with four
  morphology-focus channels; 379 MB zip; CC BY 4.0).
- **Second: 10x Xenium V1 human Lung 2-FOV** (XOA 2.0; nucleus expansion 5 µm; 278 MB zip; 11,898
  cells; 825,885 transcripts; CC BY 4.0). It is the smaller of the two, so CI uses it.
- `validation/sources.json` lists each download (URL, size, MD5, license, attribution).
  `validation/fetch.mjs` downloads it into `validation/cache/` (git-ignored; override with
  `SITUCRAFT_DATA`), verifies the checksum, extracts it, records SHA-256 for every extracted file,
  and **deletes the zip**. Both bundles take about 700 MB on disk.
- **Simulated tissues** (`situcraft simulate`, seeded) give segmentation a ground truth and give
  scale tests billions of transcripts. A scale test generates its data, measures, and deletes
  them, so it costs no disk between runs.
- A pathology bundle (post-Xenium H&E, ≤ 3 GB) is chosen when wave 5 is planned. Large bundles for
  stress tests are used only on machines with room, outside CI.
- The derived-data cache for both development bundles stays under ~500 MB.

## Testing and CI

Tests and CI follow MaveScape: **each commit is tested once**.
- A push to any branch runs CI.
- A pull request from a branch of this repository is not tested again: it shows the branch's
  checks. Only pull requests from forks run CI themselves.
- A merge whose files equal its branch's (same tree) skips testing.
- A newer push to the same branch cancels an unfinished run.
- The release workflow builds and drafts the release only after checking that a CI run passed on
  exactly the tagged files. It never re-runs the tests.
- Documentation-only changes (Markdown, the spec, license, citation) do not run CI.

CI jobs (all on fixed data):
- **Go:** gofmt, vet, `go test -race ./...`, and the six-platform cross-compile.
- **Web modules:** `node --check`, `node --test`.
- **Validation:** on the Lung 2-FOV bundle, cached by its checksum, with recorded reference
  numbers.
- **Installers:** syntax checks.

Anything slow, large or external is not in per-commit CI. That covers the Breast bundle's full
validation, scale tests, and regenerating reference numbers from external tools. These run in a
weekly and on-demand workflow, or on the maintainer's machine, and their outputs are committed as
reference JSON.

## Decisions

| Decision | Choice |
| --- | --- |
| Product and executable | **SituCraft**, `situcraft`; Go module `situcraft` |
| Go version | The siblings' minimum (`go 1.24` in `go.mod`); releases built with the current stable Go |
| Image codec | Our own JPEG 2000 Part 1 decoder in Go (lossless 5/3 path first, irreversible 9/7 next), validated tile by tile against an independent decoder; decoded once into the tile cache |
| HDF5 | Not read or written: Xenium's matrix is also in Zarr and MEX; AnnData export uses its Zarr layout |
| Internal store | Our own cache and project manifests; SpatialData Zarr and AnnData Zarr are exports |
| Renderer | Our own WebGL2 renderer (images, points, polygons); Canvas 2D for figures and fallbacks |
| Segmentation | Built in (nucleus expansion; transcript-informed after Jones et al. 2025); external results imported, never run |
| Remote control and screenshots | From wave 2, as in the siblings: the documentation's capture scenes and headless runs drive the page through it |
| Default port | 8840 (8840–8859) |
