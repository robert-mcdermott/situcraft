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
│   remote.go, actions.go, connection.go, output.go   remote control (wave 2)                           │
│   mcp.go, run.go, verify.go                         agents and headless runs (wave 9)                 │
│ data plane (new)                                                                                      │
│   internal/codec     zstd (RFC 8878), LZ4 block, Blosc, Snappy; deflate/gzip from the standard library │
│   internal/parquet   Parquet reader (flat schemas) and writer                                         │
│   internal/zarr      Zarr v2 and v3: directory and zip stores; reader and writer                      │
│   internal/tiff      TIFF/BigTIFF, OME-XML, SubIFD pyramids; reader and writer                        │
│   internal/jp2k      JPEG 2000 Part 1 decoder (TIFF compression 34712)                                │
│   internal/xenium    bundle scanner, version detection, validation, typed access                      │
│   internal/cache     derived data: image tile pyramids, transcript spatial index, geometry tiles      │
│   internal/view      the viewer's queries: points and density by gene, cells, one cell                 │
│   api_*.go           viewport queries: tiles, points, polygons, cells (binary, columnar responses)     │
│ compute (new, goroutines)                                                                             │
│   internal/segment   nucleus expansion; transcript-informed segmentation (sampler, outputs)           │
│   internal/compare   cell matching, difference classes, comparison metrics                            │
│   internal/cluster   normalization, principal components, Leiden clusters, markers (wave 4)           │
│   internal/graph     nearest neighbors (exact; a grid in two dimensions), UMAP's graph, Leiden         │
│   internal/embed     UMAP maps and their quality                                                       │
│   internal/label     marker sets, scores against binned control genes, hierarchical labels            │
│   internal/linalg    symmetric eigendecomposition, the same on every processor                         │
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
  cores, compact columnar caches, and a single cross-compiled binary.
- **Go also owns the analyses that make a result** (wave 4): clusters, embeddings, scores and
  labels run as jobs and become artifacts, as segmentations do. A cell table of a million cells
  with a 5,000-gene panel does not fit a page's memory; the same jobs run headless with nothing
  but the binary (wave 9); and a result kept as an artifact can be made again on another
  segmentation and compared.
- **The browser owns** rendering, interaction, and exploring results: coloring, selecting and
  summarizing cells over typed arrays and workers, as in the siblings.
- Unlike the siblings, the web app **needs its host**. There is no static-hosting mode, except the
  redacted static result package (wave 8), which ships precomputed tiles.

### The derived-data cache

Source bundles are read in place and never modified. Everything SituCraft derives lives in its
cache (`<UserCacheDir>/situcraft/`, kept within `--cache-limit`, 10 GB by default, by removing what
was used least recently). Every entry of a dataset shares one grid, the morphology image's pyramid:
level L has pixels 2^L times the full resolution's, and its tile (tx, ty), 512 pixels on a side,
covers the same tissue in every layer, so the renderer asks for the same tiles of each.

- **Image pyramids.** JPEG 2000 tiles are decoded once into SituCraft's own tile store: one file
  per channel and level, each tile a zlib stream of its 16-bit samples with the low and high
  bytes in separate planes (a page can inflate it natively and read the planes as a texture).
  After that, serving a tile is a read. The bundle's own levels are used where they halve the
  level above; levels it lacks are computed.
- **Transcript index.** Each transcript is stored once, in the full-resolution tile holding it.
  A tile's transcripts are sorted by gene (so one gene is one range) and stored as columns: x, y
  and z (float32, µm), quality (float32), cell (uint32 row of cells.parquet), the transcript's
  row in transcripts.parquet (so later segmentations can attach their own assignments), and a
  nucleus flag. Every coarser level has per-gene density grids (128 × 128 bins a tile, all
  transcripts and Q20 ones), so the whole-tissue view never touches points. Groups of
  transcripts have grids too (every transcript, gene transcripts, gene transcripts in a cell,
  negative controls), from which the QC maps compute per-bin shares. Level 1 counts the points; each coarser level adds up the level below, so every level
  sums exactly to each gene's count. The levels nest: a complete quadtree.
- **Geometry tiles.** Cell and nucleus polygons are placed by the centre of their bounding box,
  once per level; level 0 holds the bundle's vertices exactly, each level above them simplified
  to half a pixel of that level.
- **Files.** One layout for every kind: a header, a table of the tiles' blocks with their counts,
  and the blocks, each deflated, so that a damaged file is refused before its claims are believed.
- **Entries.** An entry is named for its kind and a key hashing the cache format, its parameters
  and the SHA-256 of its source files: a bundle that moves or is zipped keeps its entries. Entries
  are built in a folder of their own under a lock, a finished file at a time, and renamed into
  place with a manifest of their sources and every file's size and checksum, so a crash never
  leaves a half-built entry, a build resumes where it stopped, and a resumed build is
  byte-identical to one that was not interrupted. The SHA-256 of each source file is recorded
  under its place, size and time, so an unchanged bundle reopens without being read.

### The tissue renderer

The viewer draws with WebGL2 (`web/render/`), from the derived data's tiles:

- **Image channels** arrive as stored, a zlib stream the browser inflates, and go to the GPU as
  an integer texture of two byte planes. The shader reads each 16-bit sample exactly with
  `texelFetch`, then applies the channel's window, gamma and color, and channels add up. Reading
  the samples back before the window is how the validation harness proves the image is exactly
  the data.
- **The level drawn** is the coarsest whose pixels are no larger than the screen's. Coarser loaded
  tiles stand in until its tiles arrive, with a stencil so each pixel is drawn once per channel.
- **Transcripts** are points while few tiles are in view, and density grids farther out.
  **Cells** are outlines from the matching geometry level, fills colored through a per-cell color
  texture, and the selected cell drawn from its full-resolution polygon.
- **Any segmentation** of the dataset can be drawn (wave 3, slice 8). The instrument's comes from
  the dataset's own entries. Any other's outlines are tiled into an entry of their own, as the
  comparison workspace's are. Its cells table, a cell's detail and its transcript tiles come from
  its artifact's files (`api_segview.go`, kept read for the last few drawn): each transcript
  carries its cell in that segmentation and, for a refinement, its probability. What is per
  transcript is never held: the files are streamed once (`segment.Column`) into a cache entry by
  the dataset's transcript tiles, and each tile's are read when it is drawn. Tile keys name the
  segmentation, so a tile of one is never drawn as another's; while one is read, the cells drawn
  before are hidden rather than shown under its name. The nuclei stay the dataset's.
- **Tiles** are addressed by cache entry, whose name hashes its sources. They never change, so
  the browser caches them for good, and the renderer keeps the GPU's copies within a budget.

### QC

QC is computed where its data are. The run's metrics are recomputed in Go during validation,
against the transcripts, cells, matrix and masks. Per-bin maps come from the density grids'
groups of transcripts, drawn as ratios in the shader. Per-cell attributes, plots, brushing and
filters run in the browser on the cells table. Filters are project artifacts that record which
cells they keep (a SHA-256), so their reproduction can be checked whenever the project opens.

### The record: saved views, methods and archives

- **Saved views** (`lib/views.js`) are project entries, not screenshots: the dataset, the mode,
  the camera in µm (its center and µm per CSS pixel, to 12 significant digits, so a view shows the
  same tissue on any screen and survives a round trip through any pixel ratio), and the layers.
  Stains and genes are kept by name, so a view stays valid when derived data are rebuilt and says
  what a dataset lacks. A view opens through one path (`app.applyView`): the Viewer or QC applies
  it when it has its dataset open. A deep link (`#project=…&view=…`) loads the project from the
  library and applies the view.
- **The methods paragraph** (`lib/methods.js`) is a pure function of the dataset's record, its
  validation report, the chosen filters and the program's version and commit: it says only what
  was done, numbers its references in order of first citation, and gives the same text for the
  same inputs.
- **Project archives** (`api_project.go`) are zips of the project document and a README, written
  where the user says (only from this computer). Each dataset gains its place relative to the
  archive; on opening, a bundle beside the archive wins over the recorded place. An archive whose
  project the library already holds opens as a copy.
- **A project saved by each release** is kept in `validation/projects/`; the viewer suite opens it,
  locates its moved bundle, re-evaluates its filters and reopens its views exactly.

### Example datasets and tours

The examples are the development bundles, listed in `validation/sources.json` (embedded in the
program: the list, not the data), and the standard simulated tissue, made on this computer when
asked (`examples.Generated`, by `internal/simulate`; byte for byte what `situcraft simulate`
makes). `internal/examples` downloads one when asked, as a job: it checks
free space first; the archive by its size, the publisher's MD5 and its SHA-256; each extracted file
by its size and SHA-256 (a member claiming more is refused, as is a name leaving the folder); then
deletes the archive and moves the checked folder into place, so a failed or stopped download
leaves nothing. A downloaded bundle is opened like any other. A dataset is recognized as an example
by its manifest's SHA-256, wherever its bundle is, so the methods paragraph can cite the data's
source.

A tour (`lib/tours.js`) is data: steps that each open a view (a saved view's shape, applied but not
kept) and say what it shows. The genes are among those the section's transcripts show most, and
each zoomed step looks where its genes meet (the 300 µm square where their shares of the section's
transcripts are highest together).

A worked example is a tour that answers a question, saying first what to expect: its steps may run
a job before going on (expanding the nuclei, planting errors, refining, importing the simulated
tissue's truth, comparing), each named by the segmentations it needs ('instrument',
'expansion-5', 'planted', 'refined', 'truth') so that a step already done is not done again, and may show a comparison in the workspace as the step describes it (the view, the
ranking, the classes kept, the difference shown, the side panel). The Lung example's practises on
errors planted in its instrument segmentation (`internal/segment/plant`): neighbouring cells whose
merged cell would mix markers of two lineages, large cells split across their long axis, cells
dropped, and cells grown into their neighbours; each error kept apart from the others by its
neighbours, so that each is one difference; seeded, so the same errors each time; recorded as a
segmentation artifact with its answer key, simulated, never promoted and never exported. The
Segmentation view offers it on the example datasets, and on others only once the user turns
practice tools on (a preference of the browser, off by default), so that simulated cells stay out
of real work unless asked for.

### Segmentation built in

Segmentation is SituCraft's own code (`internal/segment`), not an external program:

- **Nucleus expansion** (`internal/segment/expand`). Each nucleus grows into a cell of its own,
  up to a distance and bounded by its neighbors (a distance-limited Voronoi partition), on a label
  raster at the morphology image's resolution: an exact Euclidean distance transform gives each
  pixel its nearest nucleus, in bands of rows on every core, with the same result for any split.
  Transcripts are assigned by the pixel they lie in, as the instrument assigns them. It is the
  baseline; the Lung example reproduces the instrument's own 5 µm expansion (`research.md` §3.1).
- **Segmentations as files** (`internal/segment`): a raster of cell labels in runs (lookups by
  pixel or µm, per-label statistics, outlines traced and simplified), each transcript's cell, the
  counts in sparse rows, and the cells, written zlib-compressed with checked sizes and checked
  together (geometry, assignments, counts, metadata) before they become an artifact.
- **Transcript-informed segmentation** (`internal/segment/refine`) follows the generative model
  and sampler of Jones et al. (2025): a voxel lattice, Poisson transcript likelihoods over a
  background of false decodes (measured by the negative controls) and each gene's ambient RNA, a
  gamma–negative-binomial mixture of expression states, hard morphology
  and connectivity constraints, transcript repositioning, multi-resolution sampling, and
  initialization from nuclei (`research.md` §3). Its parameters are drawn by a partially
  collapsed Gibbs sweep in which every update is an exact draw (a gamma prior on the components'
  rate parameters, where the publication uses Pólya–Gamma variables). The labelling's moves keep
  detailed balance exactly (constraints that hold both ways); the reported segmentation is the
  posterior's summary over the last sweeps; the default schedule is the one that recovers
  simulated cells best (research.md §3.2). Unlike the published implementation, it is **deterministic for a
  seed regardless of thread count**: each parallel block draws from its own seeded random stream,
  in a fixed update order.
- **Outputs** are converted to polygons (per z layer and consensus), transcript assignments with
  probabilities, and counts. They enter the artifact system like any other segmentation, with
  diagnostics: convergence (Geweke's diagnostic of the log-likelihood) and each cell's flagged
  limitations, shown in the Segmentation view and the comparison workspace. Each refinement is
  compared with the instrument's segmentation as soon as it is made.
- **The studio** (`web/ui/refine-studio.js`, `api_refine.go`) sets a refinement up in six steps
  (Input, Compute, Model, Review, Run, Compare). Every change asks the program for the spec's
  **preflight** (`refine.Check`): what it models and covers, its problems and risks, and its cost
  against the computer's memory (`internal/sysinfo`) and the free disk. The cost model is fitted
  to measured runs by phase: reading, sampling one region at a time, writing. The bundle's inputs
  are kept between preflights.
  - **Presets** (`refine.Presets`, versioned) and every setting's help come from the program, so
    the page, the terminal and remote control say the same.
  - **Regions** hold the cells about them, so a region never cuts a cell. **Sections** are found
    by single linkage, and each gets a model of its own.
  - **Patches** (`refine.Units`): a region of more than about 4 million modelled transcripts is
    cut into rectangles of about 2 million, run one at a time as regions are (each also refining
    the cells within 40 µm of its rectangle), and stitched by rectangle and by seed, so the
    sampling holds one patch. They depend on the data and settings alone.
- **One way to run** (`refine.Refine`): the app's job and `situcraft refine` read, check, sample,
  release the sampler's state and write the same way, under a tighter target for Go's collector
  (it never changes the result). A run's spec is kept as a **recipe** (`recipe.go`) that either
  runs again, byte for byte. Failures carry a class and a remedy (`jobs.Classified`), which the
  Jobs view and the studio show.
- **Validated against an independent reference:** on the 2-FOV bundles and a simulated tissue,
  SituCraft's refinement is compared with Proseg 3.2.0's by the comparison workspace's own measures.
  The maintainer reduces Proseg's outputs to committed numbers (`validation/reference/segmentation/`),
  and a weekly check requires SituCraft's segmentation to be, byte for byte, the one compared.
- **Imported segmentations** (`internal/segment/imports`) come from SpatialData Zarr,
  Baysor-style CSV plus GeoJSON, Xenium Ranger outputs, or GeoJSON polygons plus an assignment
  table; SituCraft's own files (`situcraft refine`'s) are checked against the dataset and copied. This is how results from external tools are compared, without SituCraft running those
  tools. Each becomes SituCraft's own segmentation files: a label image resampled or polygons
  drawn at the morphology image's resolution, the tool's assignment matched to the dataset's
  transcripts (by id and position, or by position and gene), counts made as for every
  segmentation. It is checked against the dataset: coordinates in units the import declares
  (never guessed from the values), the transcripts it names, an output bundle's run, its ids.
  The readers it needs are SituCraft's own: AnnData (`internal/anndata`), Zarr's sharding and
  text, GeoJSON and WKB, CSV and Parquet tables.

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
- Jobs run one at a time, one per key (a dataset's build). Each is kept in `<data dir>/jobs` as
  a file, written at each change of state and each line of its log (not its progress), so that it
  survives the program (wave 2, slice 2).
- A job the program stopped, or one that was running when it crashed, is `orphaned` when the
  program starts again. Its kind's handler makes its work again from its spec, so it resumes under
  the same id and log; work that keeps what it finished (building derived data) continues from
  there. Discarding one clears away what its kind leaves (a partial download).
- Closing the window while a job runs asks first (`beforeunload`); a window closed anyway leaves
  SituCraft running until the job ends.

### Artifacts

What jobs make, and what they register from a bundle, are artifacts (`internal/artifacts`, in
`<data dir>/artifacts`), shared by every project of the library.
- **The manifest** (`manifest.json`, format `situcraft-artifact` 1) records:
  - the lineage: parents, the job and its spec, the method with its version and citation, the
    parameters, and the software;
  - every file with its role, size and SHA-256;
  - the findings of the kind's check, and the state.
- **Promotion:** a job writes its outputs into `staging/<id>`; `Promote` checksums them (refusing
  a file the manifest does not list, or a path leaving the folder), runs the kind's check, and
  renames the folder into place: `complete`, or `invalid` (kept for review) when the check finds a
  fatal problem. Only complete artifacts are inputs or defaults; an artifact is never changed.
- **Files by reference:** a file of the dataset's bundle (location `bundle`) is recorded by its
  path and SHA-256 and never copied. The instrument's segmentation is such an artifact: its id
  hashes its files' checksums, so the same bundle files give the same artifact in any project.
  An artifact SituCraft computes is named by its inputs and parameters (a nucleus expansion: the
  instrument's segmentation, the method's version and the distance), so asking again answers it
  without a job. An import is named by the dataset, the SHA-256 of the files it read (not their
  places) and its options, so importing the same files again answers the same artifact.
- **In the project:** the project records each artifact it uses (an `artifacts` entry of kind
  `segmentation` mirroring the manifest). A dataset's default segmentation is set by decisions
  (`decisions`, kind `promote-segmentation`, with a reason, by whom, when), never removed; the
  latest decision naming a complete segmentation holds.

### Comparing segmentations

`internal/compare` compares two segmentations as SituCraft's segmentation files (the instrument's
written once from its bundle into the cache, beside the dataset's nuclei and a coarse expansion of
them for the co-expression reference). Rasters are compared run by run, without decoding pixels;
correspondences are the connected sets of a graph whose edges are shared nuclei and overlaps
holding half of a cell; each set is one difference with a class. A comparison is an artifact of
kind `comparison` (its measures, its regions, and every difference with its cells), named by the
two segmentations, the method's version and its parameters.

The comparison workspace (`ui/mode-compare.js`, `lib/review.js`) draws on it. Each segmentation's
outlines are geometry tiles on the dataset's grid: the instrument's are the dataset's own; any
other's are tiled once into a cache entry of their own. Two renderers share one camera (side by
side, or stacked: B transparent over A for the overlay, in turns for the flicker); cells are
filled by their difference's class through the renderer's per-cell colours; the regions' counts
give the disagreement map, drawn on a 2-D canvas over the tissue. The differences come to the page
column by column; ranking and the reviewer's rules are pure functions, tested in Node. Decisions
(promote, keep, reject) are appended to the project with their reason and comparison.

### Cell identities

Wave 4 types cells from their expression (`research.md` §5). Every analysis is an artifact of a
segmentation, made by a job, named by its inputs and parameters, and checked before it is recorded.

- **Clusters** (`internal/cluster`, `api_clustering.go`, slice 1): the cells analyzed (a rule, or a
  QC filter's cells checked by its checksum), normalized and scaled; exact principal components of
  the sparse counts (`internal/linalg`); each cell's nearest neighbors, exactly, weighted as UMAP's
  graph, and Leiden (`internal/graph`, carried to Go from CytoWeave's analyses); each cluster's
  markers. scanpy's defaults, so that its users find the same numbers; the same files on every
  processor. The artifact (`docs/FORMATS.md`) keeps the cells and their clusters, the components
  and scores, the neighbors and the markers; its check makes the graph again and requires the
  recorded modularity.
- **Maps** (`internal/embed`, `api_maps.go`, slice 2): UMAP of a clustering's graph, made by a job
  as soon as the clustering is, one thread and one seeded stream so a seed gives the same map
  anywhere; each map judged (trustworthiness, each cell's neighborhood precision and recall) and
  kept as an artifact of kind `embedding`. The instrument's own analysis (`internal/xenium`'s
  `ReadAnalysis`) registers as a clustering of the instrument's segmentation (its markers by
  SituCraft) and a map.
- **Labels from markers** (`internal/label`, `api_labels.go`, slice 3): a marker set (a hierarchy
  of labels and states with positive and negative markers, `docs/FORMATS.md`) checked against the
  clustering's panel; each cell scored for each label as scanpy's `score_genes` scores a gene list
  against control genes binned by expression, then labeled from the top of the hierarchy down by a
  margin; kept as an artifact of kind `labels` with the marker set by its checksum. SituCraft ships
  a starter set for each development bundle's panel (`internal/label/starters/`, embedded); the
  project keeps each marker set's versions (records of kind `marker-set`, `lib/labels.js`), and
  each label set the version it used. The labels' card (`ui/labels-card.js`) chooses, edits, reads
  and saves marker sets, labels the shown clustering, and shows each cluster's labels, names from
  them, and each label's markers and controls; the map, the tissue and the Viewer color the cells
  by label (`labels:<id>:<level>` among the Viewer's fills), confidence or a label's score.
- **Imported labels** (`internal/label`'s `table.go` and `imported.go`, slice 4): a CSV, TSV,
  Parquet or AnnData Zarr table (or Xenium Explorer's cell groups, or the instrument's clusters)
  read whole, its columns described and their roles suggested, its ids matched to a
  segmentation's cells; a table naming cells the segmentation lacks refused. Kept as a label set
  of kind `labels` with its source in place of a marker set (one level, no scores, every cell of
  the segmentation), so that the map, the tissue, the Viewer and each cluster's labels treat it as
  any label set; its composition is counted in the page for the clustering shown.
- **Label versions and review** (slice 5): curated sets (a base and a reviewer's edits, appended
  with reasons) and consensus sets (a rule over chosen sets) are the project's records
  (`lib/curation.js`), resolved with the predicted sets to each cell's label in the page
  (`ui/label-sources.js`) for the map, the tissue, the Viewer and side-by-side tables. The program
  ranks the review queue for any labels the page sends (`internal/label/review.go`: each cell's
  agreement with its nearest labeled cells, `internal/graph`'s exact two-dimensional neighbors) and
  answers a cell's evidence from markers (`evidence.go`); the evidence card and the decisions are
  the page's (`ui/review-card.js`).
- **The Cell types view** (`ui/mode-celltypes.js`, `lib/clusterings.js`) runs a clustering and
  shows its clusters, names, markers (a table and a dot plot) and the components' variances, and
  compares two clusterings. **The map and the tissue** (`ui/cell-map.js`, `lib/scatter.js`): the map
  rasterized onto a 2-D canvas (a million points redraw in tens of milliseconds, and the same pure
  functions run in Node's tests) beside a tissue renderer of the segmentation's outlines, one
  coloring (a clustering, a gene, an attribute, the map's reliability) and one selection on both:
  lassoed on either (on the tissue, by the cells' centroids), or chosen by cluster. A gene's values,
  the dot plot and a selection's markers come from the program, which keeps the last few
  clusterings' normalized expression in memory. Names are versions in the project. The Viewer fills
  the cells by cluster (`clusters:<id>` among its fills), with a legend, merged names one color.

### Exports

`internal/export` writes a segmentation from SituCraft's segmentation files and the dataset's
transcripts, with writers of its own: Parquet (`internal/parquet`: flat schemas of required
columns, PLAIN-encoded GZIP pages, the footer in Thrift's compact protocol), Zarr formats 2 and 3
(`internal/zarr`: gzip, text of varying length), AnnData's on-disk encoding (`internal/anndata`),
NumPy arrays, Matrix Market and GeoJSON, each read back by SituCraft's own readers in the tests.
The SpatialData store follows the layout spatialdata writes, element by element, so that its own
reader and the import read it; exports are written in streams (the label image by chunks, the
transcripts by the bundle's row groups). The same segmentation gives the same files, and
`situcraft-export.json` fingerprints the content (labels, counts, assignments) independently of
any format's encoding, so that any reader can check it read the segmentation exactly. An export
leaves SituCraft's keeping: it is not an artifact, and the project records it. Xenium Ranger's
inputs are prepared and checked, and its command shown; SituCraft never runs another program.

### Remote control

With `--remote-control`, programs on this computer drive the open window (`remote.go`, adapted from
MaveScape 0.8). An action POSTed to `/api/remote/action` goes to the page over server-sent events;
the page performs it (`web/ui/remote.js`) and posts its answer back, which the program returns to
the caller. The window is where the work happens, as when the user does it: an action shows what
it does, its changes go into the project's history and can be undone, and the page's own code
(saved views' path, the QC view's filters, the methods) is what runs, so a script and a person get
the same result.

- **One at a time,** to the window opened last from this run of the program; a window of an
  earlier run is refused and asked to reload.
- **The actions** are listed once in `actions.go` with JSON schemas for their arguments (served at
  `/api/remote/tools`, under an API version), and performed by the page's table of the same names;
  a test keeps the two, and `docs/REMOTE-CONTROL.md`, in step. Wave 9's MCP server offers the same
  list to agents.
- **Paths and files:** `open_paths` and `export` need the token in `remote.json` (mode 0600, in
  the data folder, removed on stop). The program checks a file's path before asking the page, and
  the page uploads the bytes to a one-use slot that writes them through a temporary file.
- **Tissue views** are applied as saved views are (a view made from the arguments, not kept), so
  `show_tissue` and `open_view` share the Viewer's one path; the answer is the view as the page
  then captures it.
- **The documentation's screenshots** (`docs/capture/capture.mjs`) are scenes of actions on the
  development bundles, captured in headless Chrome in both themes and audited by axe-core.

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
example manifest (`validation/sources.json`), checked against their checksums. Downloading and
removing examples, like writing a project archive, are accepted only from this computer.

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
validation, scale tests, the refinement's memory estimates against measured runs (which the race
detector would distort), and regenerating reference numbers from external tools. These run in a
weekly and on-demand workflow, or on the maintainer's machine, and their outputs are committed as
reference JSON.

## Decisions

| Decision | Choice |
| --- | --- |
| Product and executable | **SituCraft**, `situcraft`; Go module `situcraft` |
| Go version | The siblings' minimum (`go 1.24` in `go.mod`); releases built with the current stable Go |
| Image codec | Our own JPEG 2000 Part 1 decoder in Go, Xenium's profile first (JP2-wrapped, irreversible 9/7), validated tile by tile against an independent decoder (within ±1, as lossy decoding allows); decoded once into the tile cache |
| HDF5 | Not read or written: Xenium's matrix is also in Zarr and MEX; AnnData export uses its Zarr layout |
| Internal store | Our own cache and project manifests; SpatialData Zarr and AnnData Zarr are exports |
| Renderer | Our own WebGL2 renderer (images, points, polygons); Canvas 2D for figures and fallbacks |
| Segmentation | Built in (nucleus expansion; transcript-informed after Jones et al. 2025); external results imported, never run |
| Remote control and screenshots | From wave 2, as in the siblings: the documentation's capture scenes and headless runs drive the page through it |
| Default port | 8840 (8840–8859) |
