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
- **A release dry run** built the six binaries with `SHA256SUMS`. The macOS one:
  - printed `situcraft 0.1.0`;
  - served the embedded app with its commit in `/api/info`;
  - refused a foreign Host;
  - handed a file to a running SituCraft.
