# Changelog

## 0.2.0 (unreleased)

Wave 2 of the [roadmap](situcraft-spec/roadmap.md): segmentations become artifacts to compare,
review and choose between. Built slice by slice; this section grows with each.

### Added

- **Remote control (wave 2, slice 1).** `situcraft --remote-control` lets programs on this
  computer (scripts, notebooks, the documentation's screenshots) drive the open window, which
  performs each action where you see it; what an action changes is in the history and can be
  undone ([docs/REMOTE-CONTROL.md](docs/REMOTE-CONTROL.md)).
  - **20 actions:**
    - **the project:** describe it; start or open a project; open bundles and archives by path;
      add a dataset; open an example; take a tour;
    - **the tissue:** show a view; focus an item; show the tissue (place, zoom, stains, genes,
      layers, a cell); select a cell;
    - **saved views:** save, open and remove them;
    - **QC:** read the metrics; show a QC map; choose cells; save a filter;
    - **the record:** the methods paragraph; write the project, an archive or the methods to a
      file.
  - **Finding SituCraft:** scripts find it in `remote.json` (its address and a token), and
    `/api/remote/tools` lists the actions with their arguments' schemas.
  - **Safety:** it answers this computer only, and only the window opened from this run.
    Opening paths and writing files also need the token, and a file is never written over an
    existing one unless asked.
- **Segmentations as artifacts (wave 2, slice 2).**
  - **The artifact store** (`<data dir>/artifacts`): what jobs make and register, each with its
    lineage (what it was made from, the job and its spec, the method and its version, the
    software), the SHA-256 of every file, what its check found, and its state.
    - Outputs are written to a staging folder and checked before they become an artifact.
    - An artifact is never changed; one whose check finds a fatal problem is kept as invalid, for
      review, and never used.
  - **The instrument's segmentation** of each dataset is registered as an artifact by a job, when
    the dataset is added or a project reopens (so 0.1.0 projects get theirs). It records:
    - the bundle's files that make it (cells, boundaries, nuclei, transcript assignments, counts,
      masks) by SHA-256, never copied;
    - the cells each method segmented, and the software that did it;
    - the findings of the bundle's validation that bear on it.
  - **Promotion:** each dataset's default segmentation is promoted by a recorded decision with its
    reason. The instrument's is promoted when it is the only one, and decisions are never removed.
  - **The project tree** lists the segmentations, the default marked; the inspector shows a
    segmentation's lineage, files, findings and the decision that made it the default.
- **Jobs that survive the program (wave 2, slice 2).**
  - Jobs are kept in `<data dir>/jobs`. A job SituCraft stopped, or that was running when it
    crashed, is shown as interrupted when it starts again, and can be resumed or discarded.
  - Resuming continues the work from where it stopped: a derived-data build keeps what it
    finished.
  - Closing the window while a job runs asks first; if it is closed anyway, SituCraft keeps
    running until the job ends (Ctrl+C stops it, and the job can be resumed).
  - **The Jobs view:** what runs and waits, what was interrupted, and what finished. Each job has
    its state, progress, times and runs, its whole log and its spec, and Stop, Resume and Discard.
  - The status bar's job opens the Jobs view.
  - Remote control gains `segmentations` and `jobs` (list, read, stop, resume, discard).
- **Nucleus expansion (wave 2, slice 3).** SituCraft's first segmentation of its own.
  - **The method:** each nucleus grows into a cell of its own by up to a chosen distance (5 µm to
    start), every pixel going to the nearest nucleus within it, by exact Euclidean distances. It
    runs on every core in bands of rows, and gives the same cells however the work is split.
    Transcripts belong to the cell holding the pixel they lie in; the counts are of gene
    transcripts of quality 20 or more. Bundles without a nucleus mask have their nucleus polygons
    drawn instead.
  - **An artifact of its own:** its cells, label raster, transcript assignments, counts and
    outlines ([docs/FORMATS.md](docs/FORMATS.md)), checked before it is recorded (geometry,
    assignments, counts, metadata), with every parameter in its lineage. The same dataset and
    distance give the same artifact.
  - **Compared with the instrument's segmentation:** pixels and transcripts in the same cell, in
    another or in only one; cell areas; the assigned fractions; and what differs, described.
  - **The Segmentation view:** the dataset's segmentations side by side by their numbers;
    **Expand the nuclei**, run as a job; the comparison.
  - Remote control gains `expand_nuclei` (23 actions).
- **Importing segmentations (wave 2, slice 4).** Segmentations made by other tools, imported to
  compare with the others:
  - **SpatialData Zarr** (format 2 or 3, as spatialdata and spatialdata-io write it): cells from
    shapes or a label image, each transcript's cell from points, counts from the table;
  - **Baysor's** `segmentation.csv` with its polygons;
  - **GeoJSON** polygons, with or without a transcript table (CSV or Parquet);
  - **Xenium Ranger** output bundles of the same run.
  - Each is checked against the dataset: its coordinates' units, declared and never guessed (the
    check says which units fit when the cells lie outside the image); the transcripts it names,
    by id and position; an output bundle's run; its ids. It becomes a segmentation artifact of
    SituCraft's own files, kept with the SHA-256 of every file read, and counted as every
    segmentation is; one whose check finds a fatal problem is kept as invalid, for review.
  - **Import a segmentation** in the Segmentation view; the inspector shows the files imported.
  - Remote control gains `import_segmentation` (24 actions).
  - **Readers:** AnnData in Zarr; Zarr's sharding and text of varying length (as anndata 0.11 and
    later write); GeoJSON and WKB polygons; CSV, TSV and Parquet tables.
- **Comparing segmentations (wave 2, slice 5).** Any two segmentations of a dataset compared,
  cell by cell:
  - **Which cells correspond:** a shared nucleus, or at least half of one within the other (or, for
    cells without outlines, of their transcripts). Each set of corresponding cells is a
    difference: one-to-one, expanded, contracted, split, merged, rearranged, only in one, or
    assignment-only.
  - **Measures:** each segmentation's cells, cells with a nucleus or several, assigned fraction,
    transcripts per cell, areas, cells in pieces, job time and size; between the two, the pixels
    that agree, the transcripts that changed cell (gained, lost, moved), the overlap (IoU) and how
    far boundaries moved; and by regions of 50 µm.
  - **Transcripts given to the wrong cell:** spurious co-expression of gene pairs relative to
    nuclei alone, after Jones et al. (2025), and the mutually exclusive co-expression rate of
    Hartman and Satija (2024).
  - **Compare two segmentations** in the Segmentation view; each comparison is an artifact kept
    with the project. Remote control gains `compare_segmentations` (25 actions).
- **The comparison workspace (wave 2, slice 6).** The **Compare** view shows a comparison's two
  segmentations on the tissue:
  - **side by side** with one camera, **overlaid** in two colours, or in turns (**flicker**); cells
    filled by the class of their difference; a **disagreement map** by 50 µm region;
  - **the review queue:** the differences ranked by reassigned transcripts, contamination (markers
    of different lineages in one cell), disagreement or low counts, among the classes and
    reassigned transcripts you keep; each opens where it lies, lit in both and described in the
    inspector; **cell matching** lists them all;
  - **decisions:** promote one segmentation as the dataset's default, keep one for exploration, or
    reject one, each with a reason and the comparison it rests on, appended and never removed;
  - the measures' header warns when a measure rests on little.
  - Comparisons appear in the project tree. Remote control gains `show_comparison` and
    `decide_segmentation` (27 actions).
- **Screenshots** of the documentation: 18 scenes in both themes, captured through remote control
  from the development bundles (`node docs/capture/capture.mjs`), in the README and the
  getting-started guide.
- **A cell by its ID:** the program finds a cell by its `cell_id` as well as by its row.

### Fixed

- **The viewer suite on CI,** which fetches only the Lung bundle: it counted the jobs of two
  bundles.
- **The documentation's screenshots** no longer show a path of the computer that took them in a
  text field.
- **Accessibility, found by the audit of the screenshots** (axe-core, WCAG 2.1 A and AA):
  - the view rail's separators sat inside its tab list;
  - the project tree's sections were marked as lists with no items in them (and its rows,
    buttons, as list items);
  - a hidden stain's name and ranges were dimmed below the contrast text needs.

### Validation

- **The comparison workspace (slice 6):**
  - **Node tests** of the review queue's rankings and rules, the cautions, and the decisions'
    rules; **Go tests** of each segmentation's outline tiles and of a comparison's differences.
  - **In headless Chrome** (70 checks): the queue ranked as asked, both segmentations drawn with
    one camera in every mode, and decisions recorded only as the rules allow.
- **Comparisons (slice 5):**
  - **Hand-drawn segmentations** of every class, each count and measure worked out by hand; a
    planted leak of transcripts raises the spurious co-expression; the MECR by hand.
  - **The instrument's segmentation against the 5 µm expansion:** on Lung every cell one-to-one,
    with the 98.0629% of pixels and 8,724 changed transcripts that the expansion's own comparison
    counted; on Breast every cell of both accounted for by class.
  - **In headless Chrome** (67 checks): `compare_segmentations` by remote control.
- **Imports (slice 4):**
  - **The Lung bundle** imported into itself as an output bundle, and as spatialdata-io writes it
    as SpatialData, gives the instrument's segmentation exactly: the raster identical to the cell
    mask, every transcript's cell, every cell's count. Its polygons and assignments written as
    GeoJSON with a table, and as a Baysor-style table without ids, give every assignment. The
    Breast bundle is refused as another run.
  - **Fixtures** written by each format's own libraries (spatialdata 0.8.0 and 0.2.6, geopandas,
    pyarrow, pandas, anndata, zarr-python) import to the pixels and assignments their scene
    defines, with each planted problem found.
  - **In headless Chrome** (65 checks): `import_segmentation` by remote control, only with the
    token; another run's bundle refused.
- **Nucleus expansion (slice 3):**
  - **The Lung bundle's own 5 µm expansion is reproduced:** 11,898 cells paired one to one with
    the instrument's; 98.06% of pixels and 98.94% of transcripts in the same cells; cell areas'
    median difference +0.25%, 97.0% within 5%; 98.28% of gene transcripts assigned (the
    instrument: 97.97%). Every transcript given differently is explained: 2,534 on the
    expansion's rim, 4.56–4.98 µm from their nucleus, which the instrument's cells do not reach;
    6,182 where neighbouring cells meet, within 0.34 µm of equally near both nuclei; 8 the
    instrument records in a cell other than its own mask's at their pixel.
  - **Breast,** whose cells the instrument drew mostly from stains: 7,312 nuclei grown into cells,
    and the instrument's 7,275 cells accounted for (6,752 holding one nucleus, 255 none, 268
    several).
  - **Go tests:** the distance transform against brute force; the same cells for any split of the
    work; polygons drawn; the files read back as written, and the check finding each kind of
    damage; masks read in bands; on Lung, drawn nucleus polygons agreeing with the mask on 98.56%
    of pixels; the job and its artifact through the program's API.
  - **In headless Chrome** (62 checks): `expand_nuclei` by remote control reproduces the Lung
    expansion in the Segmentation view, and asking again answers the same artifact without a job.
- **Segmentations and jobs (slice 2):**
  - **Go tests:**
    - the artifact store records checked outputs with their SHA-256, keeps an invalid one apart,
      refuses unlisted outputs and paths leaving its folder, references bundle files without
      copying them, verifies them again, and finds a changed file;
    - jobs are kept on disk, read back after the program stops or crashes (interrupted ones
      orphaned), resumed under the same id with their log, and discarded with what they left;
    - on the Lung bundle, a derived-data build interrupted by the program stopping resumes, after
      a restart, to entries that pass their check;
    - the instrument segmentation's six files are recorded with the SHA-256 a direct reading
      gives, and registering again gives the same artifact without a job.
  - **In headless Chrome:**
    - both bundles' instrument segmentations are registered, every file's SHA-256 as Node computes
      it, promoted as the default;
    - their methods account for every cell (Breast: 3,300 by boundary stain, 3,913 by interior
      stain, 62 by nucleus expansion);
    - the 0.1.0 project gets its own, the same artifact, and still reopens its views exactly;
    - every job is complete.
  - **By hand:**
    - SituCraft killed in the middle of the Breast bundle's build: on restart the build is
      interrupted, and resuming it continues the image pyramid where it stopped;
    - every scene, the two new ones included, passes axe-core in both themes.
- **Go tests of the remote-control hub:** actions reach the window and its answer returns, one at
  a time; no window, a silent one, one that leaves and one of another run are each refused
  plainly; only this computer, and no other site's page, may send actions; opening paths and
  writing files need the token; exports never replace a file unasked; the connection file is
  private and removed on stop; the window's action table, the program's list and
  `docs/REMOTE-CONTROL.md` name the same actions.
- **In headless Chrome** (`node validation/run.mjs viewer`, 52 checks):
  - a program drives the window: `get_state` describes the open project;
  - `show_tissue` shows exactly the genes, place, zoom, cell and fill asked for;
  - a view it saves reopens exactly;
  - a filter it saves keeps the cells, under the same SHA-256, that the 0.1.0 project's identical
    filter recorded;
  - the methods cite the data and SituCraft;
  - `export` writes the project only with the token.
- **Every screenshot scene in both themes** passes axe-core's WCAG 2.1 A and AA rules
  (`docs/capture/audit.json`, 36 captures, no violation).

## 0.1.0 (2026-10-14)

SituCraft's first release, wave 1 of the [roadmap](situcraft-spec/roadmap.md): open a Xenium
output bundle, check it, and see the tissue, its transcripts and its cells at any zoom, with QC
linked to the tissue and a project that reopens exactly. Start with
[Getting started](docs/GETTING-STARTED.md): its example datasets download in a click, each with a
guided tour.

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
- **Every byte of a Xenium bundle (wave 1, slice 2).** SituCraft's own readers, in Go's standard
  library alone, for everything a Xenium output bundle stores:
  - **Codecs:** Zstandard (RFC 8878, with XXH64 checksums), LZ4 blocks, Snappy and Blosc (byte and
    bit shuffling).
  - **Parquet:** flat schemas, every physical type, plain, dictionary, RLE, delta and
    byte-stream-split encodings, data page versions 1 and 2, and Zstd, Snappy, gzip, LZ4 or no
    compression.
  - **Zarr:** formats 2 and 3, in directories and zips, with Blosc, Zstd, zlib, gzip and LZ4
    chunks, C and F order, both byte orders, fill values for chunks never written, CRC-32C and
    transposition.
  - **TIFF and BigTIFF:** strips and tiles, SubIFD pyramids, deflate, LZW, PackBits and both
    predictors, and OME-XML, including multi-file OME-TIFFs (Xenium's four morphology-focus
    channels) and z-stacks.
  - **JPEG 2000 (ITU-T T.800, Part 1):** JP2 files and raw codestreams; every progression order,
    code-block style, tiles and tile-parts, layers, precincts, packed headers and ROI; 5/3 and 9/7
    wavelets with both component transforms; decoding at reduced resolution; code blocks decoded
    in parallel with the same result on any number of cores. Wired into the TIFF reader for
    Xenium's tiles (34712) and whole-slide images' (33003, 33005).
- **Opening and checking a bundle (wave 1, slice 3).**
  - **Opening:** a Xenium output bundle opens by its folder, its `experiment.xenium` or its zip, on
    the command line or by **Open a bundle**. SituCraft never writes to it. A zipped bundle's
    compressed members are unpacked into the cache once.
  - **What it describes:** which software and version wrote the bundle and how far SituCraft reads
    it (XOA 2.x supported, others best-effort, said so), and every file by role, required or
    optional.
  - **Checks, each finding fatal, a warning or a note, with its files, count, tissue region and
    remedy:**
    - files present and readable;
    - images against the manifest;
    - cells, transcripts, the matrix, cells.zarr and boundaries against one another;
    - genes against the panel;
    - polygon validity;
    - free space for the cache.
    - It also recomputes twelve of the run's own recorded metrics from the data.
  - **The Data view** shows each bundle's facts, summary, findings and files.
    - **Add to project** records a bundle with no fatal problem: its place, run identifiers,
      summary, findings and a fingerprint of its files.
    - Reopening a project finds each dataset's bundle again and says whether it is the same,
      changed, another run, or not found; **Locate…** reconnects a bundle that moved, once it is
      shown to be the same run.
  - `--cache-dir` sets where derived data go.
- **Derived data, built once (wave 1, slice 4).**
  - **What is built** for each dataset of a project, on one grid of tiles shared with the
    morphology image's pyramid:
    - **image tiles** of every channel at every zoom level, decoded once from the bundle's JPEG
      2000;
    - **a spatial index of the transcripts:** each one in the tile holding it, grouped by gene,
      with per-gene density grids for every coarser level;
    - **cell and nucleus outlines** in tiles, exact at full resolution and simplified for each
      coarser level.
  - **When:** a dataset's derived data are built when it is added to a project, or when a project
    reopens and finds it again. Both development bundles take 2 to 4 seconds and 222 MB together.
  - **Built to be trusted and reused:**
    - entries are named for the contents of the files they come from, so a bundle that moves or
      is zipped keeps them and a changed one gets new ones;
    - an unchanged bundle reopens without being read;
    - an interrupted build resumes where it stopped, with the same result to the byte;
    - a crash never leaves a half-built entry.
  - **The cache is kept within a size,** `--cache-limit` (10 GB by default), by removing what was
    used least recently; it can be deleted at any time.
  - **Jobs:** long work runs in the program as a job with progress, a log and **Stop**. The page
    follows it live and finds it again after a reload.
  - **The Data view** shows each dataset's derived data (what is built, the size, a running
    build's progress, **Build now**, why the last build failed if it did) and what the cache
    holds; the status bar and inspector show a running build.
  - Sizes are now shown in decimal units (1 GB is 10^9 bytes), as `--cache-limit` reads them.
- **The tissue viewer (wave 1, slice 5).**
  - **The tissue at any zoom**, drawn with WebGL2 from the derived data:
    - the morphology channels, each with its own color, contrast window and gamma, every 16-bit
      sample exactly as recorded;
    - transcripts of chosen genes, up to 16 in distinct colors, or all transcripts: as points up
      close, and as each gene's density farther out;
    - cell and nucleus outlines, and cells filled by transcripts, area or segmentation method,
      with a legend.
  - **Finding your way:** pan and zoom by dragging, scrolling, pinching or the keyboard; a scale
    bar, the position in µm, and a minimap of the whole tissue.
  - **Finding genes** by typing their names, with each one's count.
  - **Clicking a cell** shows it in the inspector: its ID, position, areas, segmentation method,
    transcripts and top genes. Clicking one of its genes shows that gene.
  - **View the tissue** in the Data view opens a dataset once its derived data are built.
  - Tiles are served from the cache and kept by the browser, so a view you return to is drawn at
    once.
- **QC (wave 1, slice 6).** A QC view of whether the run supports analysis, and where it does
  not.
  - **Run metrics explained:** every metric the run recorded, with what it measures. For 20 of
    them, SituCraft's own value from the bundle's data is shown beside it; the rest are shown as
    recorded, with why.
  - **QC maps over the tissue:**
    - per cell: transcripts, genes, area, nucleus area, transcript density, transcripts in the
      nucleus, segmentation method and more;
    - per small region: the share of gene transcripts outside cells, the share below quality 20,
      and negative-control density.
  - **Plots:** a histogram, box plots by segmentation method and a scatter plot. Dragging across
    one chooses those cells, which the tissue shows (in the QC view and in the Viewer) and the
    inspector summarizes.
  - **Filters:** conditions on the cells, their effects counted as you edit them. They are saved
    as named versions in the project and compared, and never change the data. Each version
    records which cells it keeps and is checked to keep them again when the project reopens.
  - The derived data's format changed (to count the groups of transcripts the maps need), so
    datasets build their derived data once more.
- **Saved views, the record and examples (wave 1, slice 7).**
  - **Saved views:** **Save view** in the Viewer or the QC view keeps what it shows: the place and
    zoom (in µm, so the same tissue on any screen), the channels with their colors and windows,
    the genes, the layers and cell fill, the selected cell, and the QC map and filter in use.
    Channels and genes are kept by name, so a view survives its derived data being rebuilt. Views
    are listed in the project tree and the palette; each can be renamed, removed, or copied as a
    link that reopens it.
  - **Project archives (`.scz`):** **Project archive (.scz)…** in the project menu writes the project (datasets,
    filters, saved views, history; not the bundles) where you say, recording each bundle relative
    to the archive too, so that a folder holding both can be moved or shared. Opening an archive
    (by its path, or on the command line) finds each bundle beside it first; an archive whose
    project is already in the library opens as a copy.
  - **A methods paragraph** for each dataset (**Methods** in the Data view): the platform, panel
    and software, the segmentation, the transcripts counted, how SituCraft checked the bundle
    (with its version and commit), the filters chosen, and the data's source for public data,
    with references, ready to copy.
  - **Example datasets:** the start page offers 10x Genomics' Xenium human Lung and Breast 2-FOV
    bundles (CC BY 4.0), with their source, license and size. Nothing is downloaded until asked;
    a download runs as a job with progress and **Stop**, its archive checked by size, MD5 and
    SHA-256 and every file by size and SHA-256, then deleted. `--examples-dir` sets where they go
    (by default the `examples` folder of the data directory).
  - **Guided tours** of both examples: seven steps each through the Data view, the Viewer (the
    section, marker genes where they meet, segmentation methods) and QC (maps and filters).
  - **Documentation:** [Getting started](docs/GETTING-STARTED.md) and
    [the formats SituCraft reads and writes](docs/FORMATS.md).
- **The development data.** `node validation/fetch.mjs` downloads 10x Genomics' Xenium human Lung
  and Breast 2-FOV bundles (CC BY 4.0). It checks each archive's MD5, extracts it (each member's
  CRC-32 checked), checks every file's SHA-256, and deletes the archive. Both take about 640 MB;
  nothing is stored in the repository.
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
- **The readers on both development bundles** (`validation/readers_test.go`), against
  independent readers:
  - 60 Parquet columns over 2.9 million rows, identical to DuckDB 1.5's reading;
  - all 492 Zarr arrays, identical to zarr-python 3.4 with numcodecs;
  - the structure of 135 image levels, identical to tifffile's;
  - the smallest pyramid level of every image plane, pixel for pixel identical to tifffile with
    imagecodecs.
- **Bundle validation** (`internal/xenium`):
  - both development bundles validate with no fatal finding and no warning, and reproduce all 12 of
    their recorded metrics;
  - their notes are exactly the expected ones (polygons with crossing edges, the same counts GEOS
    finds; transcripts just past the images' edge; Breast cells without a nucleus);
  - the Lung bundle zipped validates as its folder does;
  - 13 broken bundles raise exactly the findings they should;
  - no file of a bundle changes when it is checked.
- **The derived data of both development bundles** (`internal/cache`):
  - every pixel of every image tile equals the bundle's own level, decoded directly;
  - every transcript is in exactly one tile, with its values; every gene's density grid sums to
    its count at every level;
  - the outlines at full resolution equal the bundle's, and every level holds each outline once,
    within its simplification tolerance;
  - builds stopped partway resume to byte-identical entries; an unchanged bundle reopens with
    nothing read or built; the zipped Lung bundle reuses the folder's entries;
  - both stay under the 500 MB budget, and every reader refuses damaged files cleanly (fuzzed).
- **QC** (`internal/xenium`, `internal/view`, `node validation/run.mjs viewer`):
  - 22 of the runs' recorded values reproduced from the data on both bundles (20 metrics of
    metrics_summary.csv, 2 of the manifest);
  - the QC maps' data, summed over the tissue, give the runs' share of gene transcripts in cells
    and of quality 20 or more exactly;
  - brushing chooses exactly the cells in range;
  - a saved filter keeps the same cells, under the same SHA-256, as an independent evaluation.
- **The tissue viewer in headless Chrome** (`node validation/run.mjs viewer`):
  - every channel of both bundles, drawn at full resolution, at half and at the coarsest level
    and read back before any window or color, equals the derived data sample for sample (1.35
    million samples);
  - the transcripts it loads for a region equal the program's query of the index;
  - it pans and zooms at 60 frames per second on an Apple M4, and a view it returns to is drawn
    at once.
- **The viewer's queries** (`internal/view`): a region's transcripts equal a scan of
  transcripts.parquet; every cell's gene transcripts counted from the index equal cells.parquet's
  count, for all 19,173 cells of both bundles.
- **JPEG 2000 against OpenJPEG on all 918 tiles of both bundles:**
  - the 270 lossless (5/3) tiles are identical;
  - the 648 lossy (9/7) tiles are within ±1 at every pixel (0.74% of samples differ by 1, RMSE
    0.086), which is how far two conformant decoders of a lossy stream may differ;
  - resolution reduction is within ±1 too.
  - A tile decodes in about 90 ms on one core (budget 150 ms), and every tile of both bundles in
    8.5 s on ten cores.
- **Fixtures written by independent tools** (the zstd and OpenJPEG tools, c-blosc, DuckDB, pyarrow,
  zarr-python, tifffile), committed with the scripts that made them, test each format's every
  variant without the data. Every decoder was fuzzed (about 100 million executions); the five
  crashers found, all from declared sizes a file did not back up, are fixed.
- **CI** checks the readers and the Lung bundle's tiles for each commit (`node
  validation/fetch.mjs xenium-lung-2fov`, OpenJPEG installed as the reference); a weekly workflow
  checks both bundles.
- **The record (wave 1, slice 7):**
  - a project saved by this release (`validation/projects/0.1.0-lung.situcraft.json`: the Lung
    dataset, two versions of a filter and three saved views) opens with every field kept; in
    headless Chrome its moved bundle is located again, its filters keep exactly the cells they
    recorded (by an evaluation in Node and Node's own SHA-256), and its three views reopen exactly,
    from the project and from a link;
  - every step of both tours opens with its genes, and each gene a tour names is in its panel;
  - an example downloads as a job, is checked and opened as a bundle, and is removed; a damaged
    archive, a wrong checksum, a member leaving its folder, too little free space and a stopped
    download each leave nothing behind (Go tests); downloading and removing are refused to other
    computers;
  - project archives keep their bundles when their folder moves (Go test);
  - the methods paragraph says only what was done, and the same inputs give the same text.
- **A release dry run** built the six binaries with `SHA256SUMS`. The macOS one:
  - printed `situcraft 0.1.0`;
  - served the embedded app with its commit in `/api/info`;
  - refused a foreign Host;
  - handed a file to a running SituCraft.
