# SituCraft roadmap

The plan for building SituCraft. Each **wave** is one release: wave 1 is 0.1.0, wave 2 is 0.2.0,
and so on. Each wave is split into **slices**, bite-sized units of work. A slice ends with:
- something that runs;
- tests or validation that check it;
- its documentation;
- an entry in `CHANGELOG.md`.

Slices are done in order within a wave unless noted. Each slice names the requirements
(`requirements.md`) it serves. When a wave is reached, it is planned in detail: its slices are
confirmed or re-cut, and the decisions taken are recorded here. Waves 1–3 are planned to the slice
now; later waves are outlined and refined as they come.

The order follows the siblings' principle:
1. **Trust first.** Readers that agree with independent readers on every byte, and a bundle
   validator that says when not to trust the data.
2. **Then the segmentation decision.** Compare segmentations, build segmentation in, choose with
   evidence.
3. **Then tissue biology.** Cell identities, pathology, niches and cohorts on the chosen
   segmentation.
4. **Then reach.** A provable record, agents and scripts, whole slides and other platforms.

## Decisions taken in this plan

| Decision | Choice |
| --- | --- |
| Product and executable | **SituCraft**, `situcraft`; Go module `situcraft`; project archives `.scz` |
| Architecture | One Go binary (standard library) serving an embedded vanilla-JS app in a desktop window, as CytoWeave and MaveScape; Go does I/O, decoding, indexing, segmentation and transcript-scale computation; the browser renders and runs cell-level analytics (`design.md`) |
| Segmentation | Built in: nucleus expansion (wave 2) and transcript-informed segmentation after Jones et al. 2025 (wave 3). Results from other tools imported for comparison, never run by SituCraft |
| Image decoding | SituCraft's own JPEG 2000 decoder, decoded once into a tile cache (wave 1, slice 2) |
| Development data | 10x Xenium V1 human Breast 2-FOV (primary) and Lung 2-FOV (CI and nucleus-expansion validation), both CC BY 4.0, ~700 MB extracted, never in the repository; simulations for ground truth and scale |
| CI | Once per commit; releases check CI's result on the tagged files instead of testing again; large data and reference regeneration weekly or by hand (`design.md`) |
| Default port | 8840 (8840–8859) |

## In every wave, from wave 2

- **Examples.** Each new capability arrives with an example that uses it, on the development
  bundles or a simulation (labeled simulated). Each example states its question, data source and
  license, what to expect, the view it opens and its guided steps, and the validation suite
  checks it.
- **Documentation.**
  - The slice that builds a feature also writes its guide page, its tutorial on an example, and
    the capture scenes for its screenshots.
  - `docs/FORMATS.md` grows with every file format read or written.
  - The README is the front page, laid out as the siblings' are. A slice adds or amends a
    Highlights bullet or a table row there. What each release brought goes in `CHANGELOG.md`.
- **Release.** Screenshots re-captured, the site built and checked, binaries drafted by the
  release workflow.
- **Projects that keep opening.** Every release saves a project (an example with its artifacts,
  decisions and saved views) as a fixture under `validation/projects/`. Every later version must
  open each fixture and reproduce its artifacts' recorded checksums, or say why not.

## 0.1.0: open a bundle and see the tissue (wave 1)

A researcher opens a Xenium bundle and gets:
- a validation report that says what is there and whether to trust it;
- the tissue, its transcripts and its cells at any zoom;
- QC linked to the tissue;
- a project that reopens instantly.

The data plane built here (readers, codecs, the derived-data cache) carries every later wave.

### Wave 1

1. **Repository foundation (P1, P2, P3, P4, P5, X1): done.** Ported from MaveScape 0.8, starting
   from MaveScape's own slice-1 files plus the host fixes it made later (a window handed to a
   browser already running keeps the server up; an upload already stored is read before the
   answer):
   - **Go host:** `main.go`, `security.go`, `local.go`, `store.go`, `records.go`, `window.go`
     and their Go tests.
     - The library holds projects, and is always on disk: projects refer to bundles by their
       place on this computer, which a browser's storage cannot do, so the page needs its
       program and says so without it.
     - `local.go` recognizes Xenium bundles (a folder holding `experiment.xenium`, or that file),
       Zarr stores, images, tables, polygons and projects, and treats a bundle as one item that
       is never walked into, served or copied.
     - Ports 8840–8859, clear of the siblings' (checked by a test).
   - **Web shell:** `index.html`, `styles.css`, `app.js`, and `ui/` (`dom.js`, `overlays.js`,
     `store.js`, `storage.js`, `palette.js`, `icons.js`, `sidebar.js`, `inspector.js`,
     `drawer.js`, `mode-welcome.js`, `mode-planned.js`, `workers.js`).
     - A view rail (Data, Viewer, QC, Segmentation | Cell types, Spatial, Cohort, Reports, Jobs),
       each view saying what it will do until its slice builds it.
     - The start page with recent projects, the project tree (with **On this computer**), the
       inspector, the bottom drawer, the status bar, light and dark themes, and
       color-vision-friendly colors.
     - **Open a bundle** takes a path and hands it to the program, as a second launch does.
   - **Pure modules:** `web/lib/random.js`, `sha256.js`, `dmath.js`, `memory.js`,
     `colormaps.js`, `colorvision.js`, with their tests; `project.js` (the project envelope,
     `situcraft-project` version 1) with its tests; `determinism.test.mjs`.
   - **Validation:** `validation/run.mjs` with the `accessibility` suite.
   - **CI:** the single-run policy (`design.md`), with jobs Go (gofmt, vet, race tests,
     six-platform cross-compile), browser modules (`node --check`, `node --test`, validation) and
     installers (`sh -n`, PowerShell's parser).
   - **Release workflow:** the tag, `main.go`, `web/app.js`, `index.html`'s cache keys,
     `CITATION.cff` and `CHANGELOG.md` must agree; it checks CI passed on the tagged tree; it
     embeds the commit with `-ldflags -X main.commit`.
   - **Repository files:** `install.sh` and `install.ps1`, `LICENSE`, `NOTICE`, `CITATION.cff`,
     `CHANGELOG.md`, `README.md`, `docs/INSTALLING.md`, `.gitattributes`, `.gitignore`;
     `.claude/launch.json` (port 8840), kept locally as in the siblings.
   - `--version` prints one line, `situcraft X.Y.Z`, which the installers compare.
   - Validation:
     - 17 Go tests: security headers; foreign hosts and cross-origin requests refused; library
       round trips; content addressing; range requests; bundles and files found in folders in
       natural order, a bundle never listed inside, served or copied; every web module
       embedded; the commit reported; ports clear of the siblings'.
     - 23 `node --test` tests.
     - `accessibility` 14/14.
   - The release dry run:
     - It built six binaries (about 7 MB each) with `SHA256SUMS`.
     - The macOS one printed `situcraft 0.1.0`, served the embedded app with its commit in
       `/api/info`, sent the CSP and cross-origin-isolation headers, refused a foreign Host
       (403), and found a bundle in a folder named on its command line.
     - A second launch handed an image to the running program.
   - In the window: opening a bundle by its path lists it under **On this computer**, with its
     kind in the inspector. A project saved in the library reopens from the start page into the
     Data view. No console errors; the page is cross-origin isolated.
2. **Feasibility: every byte of a bundle (D6, X3, X4): done.** The riskiest slice, done before
   any viewer code. It is SituCraft's own code throughout:
   - **Codecs:** `internal/codec` (Zstandard per RFC 8878, the LZ4 block format, the Blosc
     container with byte and bit shuffle).
   - **`internal/parquet`:** flat schemas; PLAIN, dictionary and RLE/bit-packed encodings;
     definition levels; Zstd, Snappy, gzip and uncompressed pages; column projection and
     row-group statistics.
   - **`internal/zarr`:** v2 metadata and chunks, directory and zip stores; v3 metadata read.
   - **`internal/tiff`:** TIFF and BigTIFF IFDs, SubIFD pyramids, tiles, OME-XML.
   - **`internal/jp2k`:** a JPEG 2000 Part 1 decoder (JP2 boxes, codestream, tier-2 packets,
     tier-1 EBCOT with the MQ decoder, 9/7 irreversible and 5/3 reversible wavelets). Every Xenium
     tile is JP2-wrapped, irreversible 9/7 (lossy), RPCL, one layer (found on the files).
   - **OME-XML:** multi-file OME-TIFFs (the four morphology-focus channels name each other's
     files) and z-stacks.
   - **Data:** `validation/sources.json` and `validation/fetch.mjs` download the two 2-FOV
     bundles (browser User-Agent, MD5 check), extract them, record SHA-256 per file and delete
     the zips.
   - **Exit criterion.** Every Parquet column, Zarr array and image tile of both bundles decoded
     by SituCraft equals an independent reader's output:
     - Parquet against DuckDB;
     - Blosc and Zarr against `numcodecs`;
     - every JPEG 2000 tile against OpenJPEG: within ±1 at every pixel, as two decoders of a
       lossy (9/7) stream may differ in the last rounding; exactly for reversible streams.

     The comparison scripts are committed under `validation/reference/`, with their outputs
     reduced to checksums.
   - **Budgets:** decoding a 1024×1024 uint16 tile under 150 ms on one core (a target, confirmed
     by measurement); full-bundle decode time recorded.
   - **Fuzzing:** each decoder fuzzed, with a corpus of small hostile cases.
   - If the JPEG 2000 budget cannot be met, the tile cache (slice 4) still makes viewing fast
     after a one-time decode. The decision is recorded here.
   - Validation:
     - **Readers on both bundles** (`validation/readers_test.go`, against
       `reference/xenium-bundles.json` from DuckDB 1.5, zarr 3.4 and tifffile/imagecodecs):
       - Parquet: 60 columns over 2.9 million rows, identical to DuckDB's reading;
       - Zarr: all 492 arrays, identical to zarr-python's;
       - TIFF: the structure of 135 image levels, identical to tifffile's;
       - the smallest pyramid level of every image plane, pixel for pixel identical to tifffile
         with imagecodecs (those levels are lossless).
     - **JPEG 2000, against OpenJPEG on all 918 tiles:**
       - the 270 reversible 5/3 tiles are identical;
       - the 648 irreversible 9/7 tiles are within ±1 (0.74% of samples, RMSE 0.086);
       - resolution reduction 1, 3 and 5 is also within ±1;
       - the difference has one cause, OpenJPEG's fixed-point approximation of the high-pass
         scaling: with that value substituted (a test-only option) the two agree bit for bit
         (OpenJPEG 2.5.4; CI's 2.5.0 is held to ±1 alone, `research.md` §1.6).
     - **Budget:** the largest real tile decodes in about 90 ms on one core (budget 150 ms; OpenJPEG
       76 ms single-threaded), 15 ms on ten. All 918 tiles of both bundles, through the TIFF
       reader, decode in 8.5 s on ten cores.
     - **Unit tests and fixtures** written by independent tools, committed (about 4.5 MB):
       - zstd: 26 frames, 300 round trips through the zstd tool, 44 hand-built frames, XXH64;
       - Blosc: 61 chunks from c-blosc 1.21;
       - Parquet: 12 files from DuckDB and pyarrow (every codec, both page versions, dictionary,
         delta and byte-stream-split encodings, nulls);
       - Zarr: 35 arrays in formats 2 and 3, directory and zip;
       - TIFF: 11 files from tifffile (LZW, deflate, PackBits, both predictors, both byte orders,
         planar RGB, BigTIFF pyramids, OME);
       - JPEG 2000: 61 codestreams from OpenJPEG (every progression, code-block style, tiles,
         tile-parts, layers, precincts, SOP/EPH, PPM/PPT, ROI, RGB, subsampling) and 4,500 random
         encodings compared on the fly.
     - **Fuzzing:** every decoder, from 15 seconds (Snappy) to 50 minutes (JPEG 2000), about 100
       million executions in all. It found five crashers, all from declared sizes a file did not
       back up; all are fixed, and the four the fuzzer could save are kept as regression cases
       (the fifth, a TIFF tile size that exhausted memory, is now refused before decoding).
     - **CI:** each commit checks the readers and every Lung tile against OpenJPEG; a weekly
       workflow checks both bundles.
   - Found by slice 2:
     - **The images are lossy.** Full-resolution tiles are irreversible 9/7; only the pyramid
       levels are lossless 5/3. Each tile is a JP2 file, not a bare codestream. The criterion
       became "within ±1 of OpenJPEG", as two conformant decoders of a lossy stream may differ.
     - **The morphology-focus images are one multi-file OME-TIFF:** each file's XML names every
       channel's file; channel purposes are MapAnnotations.
     - **Blosc's chunk format leaves out rules a decoder needs** (when blocks are split, the short
       last block, stored streams, bit shuffling of partial groups); they were settled by
       experiment and are written down in `research.md` §1.3.
     - **No HDF5 is needed,** as planned: the same matrix is in Zarr and MEX.

3. **The bundle and its validation (D1, D2, D3, D4, D7): done.** `internal/xenium`:
   - **Opening:** a bundle folder, its `experiment.xenium`, or a zip (with the bundle at its root or
     in one folder). The source is read-only. Links are followed, so bundles behind links on shared
     storage open. In a zip, stored members are read in place; compressed ones are unpacked once
     into the cache, CRC-checked.
   - **Description:** the run manifest, metrics and gene panel; the XOA version, with what SituCraft
     makes of it (2.x supported; others best-effort, said so).
   - **Inventory:** every file by role, required or optional, with the form found. A form SituCraft
     does not read (CSV only, HDF5 only) is named.
   - **Typed access:**
     - cells;
     - transcripts, a row group at a time;
     - cell and nucleus polygons;
     - the matrix, from Zarr or Matrix Market (folder or `.tar.gz`), with feature kinds;
     - the morphology channels through the multi-file OME-XML, with each channel's purpose and
       pyramid.
     - The Xenium cell-ID codec.
   - **Validation** (`validate.go`): findings that are fatal, warnings or notes, each with a stable
     code, the files, a count, the tissue region (µm) and a remedy. It checks:
     - files present and readable;
     - the pixel size;
     - the images' sizes, compression and pixel size against the manifest, and the z-stack;
     - cells: centroids on the images, areas, nuclei larger than cells, totals equal to the sum of
       their parts;
     - every transcript's cell in the cells table;
     - per-cell Q20 counts by kind against cells.parquet;
     - genes against the panel;
     - the matrix's cells and per-cell counts by kind against cells.parquet, and its aggregate
       against its genes;
     - cells.zarr against cells.parquet, and the masks against the images;
     - polygon validity and coverage;
     - twelve of the run's recorded metrics recomputed from the data;
     - free space for the cache.
   - **Program:** `--cache-dir`; `GET /api/local/{i}/bundle` (inventory and fingerprint) and
     `GET /api/local/{i}/validation` (the report). Empty paths are refused.
   - **The Data view** (`ui/mode-data.js`, `ui/bundles.js`): the bundles of the project and of this
     computer; for each, its facts, summary tiles, the findings grouped by severity, and its files
     by role.
     - **Add to project** is refused while a fatal problem stands.
     - **Check again**; **Remove**; **Locate…** for a moved bundle (checked to be the same run
       first).
   - **Datasets in the project** (`lib/datasets.js`): where each bundle is, the run's identifiers,
     its summary, its findings, and a fingerprint (every file's size; the description files'
     SHA-256).
     - A project reopening reconnects each dataset and says whether it is there, the same bundle
       (a copy is), changed, or another run.
     - An untitled project takes its first dataset's name.
   - Portable projects (paths relative to an archive) come with the project archive in slice 7.
   - Validation:
     - **Both development bundles:**
       - no fatal finding and no warning;
       - all 12 recorded metrics reproduced, among them the cells of each segmentation method
         (Breast: 3,300 by boundary stain, 3,913 by interior stain, 62 by nucleus expansion);
       - exactly the expected notes: polygons with crossing edges (44 and 54 in Lung, 10 and 308 in
         Breast, the same counts Shapely 2 (GEOS) finds invalid in the same files); 7 Lung
         transcripts up to 5 µm past the images; 255 Breast cells without a nucleus;
       - checked in 0.2 s each.
     - **The Lung bundle zipped** (in a folder, images stored, the rest deflated) validates as the
       folder does, plus a note that it is zipped.
     - **Thirteen broken bundles** (the Lung bundle with one change each, in a folder of links)
       raise exactly the findings they should:
       - a missing table, missing images, or a matrix only in HDF5;
       - a cut-off Parquet file;
       - transcripts, a matrix, boundaries or a panel from the other run;
       - no pixel size, or a wrong one;
       - a disagreeing metric;
       - an untested version.
     - **No source file changes:** the SHA-256 and time of every file are the same before and
       after.
     - **Unit tests** of the cell-ID codec, feature kinds, versions, polygons (bow tie, pinch,
       degenerate, collinear, non-finite), metrics and Matrix Market parsing.
     - **Fuzzing:** the Matrix Market reader and the polygon check.
     - **Endpoints:** describe and validate through the API.
     - **Datasets:** `lib/datasets.js`'s tests.
     - **In the window:**
       - open a bundle by its path, see its report, add it;
       - reopen the project: the dataset is found and the same;
       - a dataset with no place is "Not found" and **Locate…** reconnects it;
       - no console errors.
   - Found by slice 3:
     - **The Zarr matrix has one feature more than the MEX one:** an aggregate "Total transcripts"
       per cell (`research.md` §1.3a).
     - **The recorded metrics' definitions** hold exactly on both bundles, among them a density per
       cell area rather than per region (§1.3b), so they can be checked, not only shown.
     - **Some boundary polygons cross themselves** in both bundles (up to 4% of nuclei in Breast).
       They are noted, not treated as errors; slice 5's renderer must draw them.
     - **The path dialog's Enter re-pressed the button that opened it** (in the code shared with
       the siblings), and **an empty path meant the current folder**: both fixed.

4. **The derived-data cache (D5, X4): done.** `internal/cache` builds three kinds of entry on one
   grid, the morphology image's pyramid: level L has pixels 2^L times the full resolution's, and
   its tile (tx, ty), 512 pixels on a side, covers the same tissue in every layer.
   - **Image pyramids:** each morphology-focus channel decoded once into one file per level. Each
     tile is a zlib stream of its 16-bit samples with their low and high bytes in two planes: about
     a fifth smaller than deflating the samples as they are, and faster. The bundle's own levels
     are used where they halve the level above; a level it lacks is averaged from the one above.
   - **The transcript index:** every transcript stored once, in its full-resolution tile, sorted by
     gene, with its position, z, quality, cell (a row of cells.parquet), nucleus flag and its row
     in transcripts.parquet. Every coarser level has per-gene density grids: 128 × 128 bins a
     tile, counting all transcripts and those of quality 20 or more, and every gene together.
     Level 1's grids count the points; each coarser level's add up the level below's. The levels
     nest, so the index is a complete quadtree aligned with the image's tiles.
   - **Geometry tiles:** cell and nucleus polygons, per level, each in the tile holding the centre
     of its bounding box. Level 0 holds the bundle's vertices exactly; each level above holds them
     simplified (Douglas–Peucker) to within half a pixel of that level, keeping at least three.
   - **One binary layout** for all three (`internal/cache/binfile.go`): a header, a table of the
     tiles' blocks with their counts, and each block a zlib stream whose size follows from its
     counts, so a damaged file is refused before anything is allocated for it.
   - **Entries** are named for their kind and a key hashing the cache format, their parameters and
     the SHA-256 of their source files. A bundle that moves or is zipped keeps its entries; one
     whose files change gets new ones.
     - Each is built in its own folder under a lock (taken over when its program dies), a finished
       file at a time, then renamed into place with a manifest of its sources and every file's size
       and SHA-256.
     - Build times go in a separate file, so two builds of an entry are byte-identical.
     - Each source file's SHA-256 is recorded under its place, size and time: an unchanged bundle
       reopens by looking at its files, not reading them.
   - **Size:** `--cache-limit` (10 GB by default) keeps the cache within a size by removing what
     was used least recently, never what was used since the program started; zipped bundles'
     unpacked files count. A build first checks the disk has room.
   - **Jobs** (`internal/jobs`, first version): an immutable spec; a state (declared, queued,
     running, validating, then complete, cancelled, failed or invalid); progress; an append-only
     log; cooperative cancellation; one job at a time and one per key.
     - Jobs live in memory; persisting them comes with the artifact store (wave 2, slice 2).
     - A build job ends by checking what it built: every tile present, the transcripts and polygons
       counted, the coarsest density grid summing to the transcripts.
   - **Program:**
     - `--cache-limit`;
     - `GET` and `POST /api/local/{i}/cache`: what is built, and a job building the rest;
     - `GET /api/cache`: what the cache holds;
     - `GET /api/jobs`, `GET /api/jobs/{id}`, `POST /api/jobs/{id}/cancel`, and
       `GET /api/jobs/events`, server-sent events with the list of jobs whenever one changes.
     - Stopping the program stops its job where it is; the next build resumes it.
   - **Page:** a dataset's derived data are built when it is added to the project or found again
     on reopening. The Data view shows each entry's state and size, the build's progress with
     **Stop**, **Build now** for what is missing, why the last build failed if it did, and what
     the cache holds. The status bar and the inspector show a running build.
   - Validation:
     - **Both development bundles** (the Lung bundle in CI, both weekly):
       - every pixel of every tile of every level equals the bundle's own level, decoded directly;
       - every transcript is in exactly one tile, the one holding it, with its gene, position, z,
         quality, cell and nucleus flag equal to transcripts.parquet's;
       - each gene's density grid sums to its count (and its Q20 count) at every level, and every
         gene together to the transcripts; every bin of level 1 equals a recount of its points;
       - level 0's polygons equal the boundary files' exactly, and every level holds each polygon
         once, simplified within its tolerance;
       - builds stopped in the image, in the transcript index and in the outlines resume to
         byte-identical entries;
       - reopened unchanged, nothing is read or built (the status takes under a millisecond);
       - the zipped Lung bundle finds the folder's entries and only reads its files' checksums;
       - no bundle file changes.
     - **Sizes and times** (Apple M4, 10 cores), within the 500 MB budget, which a test keeps:
       - Lung: 57 MB in 1.9 s (image 29 MB, transcripts 20 MB, outlines 7 MB);
       - Breast: 165 MB in 3.8 s (133, 27 and 5 MB);
       - 222 MB together.
     - **Unit tests on synthetic data:**
       - pyramids with levels decoded and computed;
       - an index with transcripts past the edges, unassigned and unknown cells, six groups of
         rows, and a stop in each phase;
       - polygons with and without closing vertices;
       - the grid, entry names, the checksum record, locks, eviction and sizes.
     - **Fuzzing:** every reader on damaged files; the polygon simplification.
     - **Jobs:** states, cancelling queued and running jobs, one job per key, the log, a panic
       turned into a failure; over HTTP, the event stream and cancelling.
     - **In the window:**
       - add the Breast bundle and watch it build;
       - **Stop** partway: the image is kept and the index "partly built";
       - **Build now** resumes, and the result equals an uninterrupted build;
       - a cache folder that cannot be written fails the build with that reason shown, and
         **Build now** succeeds once it can;
       - no console errors.
   - Found by slice 4:
     - **Density grids are as large as the points**, because at fine levels nearly every
       transcript is alone in its bin. Deflating every block halved the transcript index (Lung:
       42 MB to 20 MB).
     - **A whole view redrawn five times a second loses clicks:** the Data view and the status bar
       now update a running job's progress in place.
     - **A damaged block could claim a gigabyte:** readers refuse a block claiming more than
       deflate's largest expansion of its stored bytes.
5. **The tissue viewer (V1, V2, V3, V4, V5, V9, V10): done.**
   - **Renderer** (`web/render/`, WebGL2):
     - **Channels:** 16-bit tiles kept as two byte planes in an integer texture and read with
       `texelFetch`, so the shader sees every sample exactly. Each channel's window, gamma and
       color are applied there, and channels add up.
     - **Levels:** the level drawn has pixels no larger than a device pixel. Until its tiles
       arrive, coarser loaded tiles stand in, and a stencil keeps each pixel drawn once per channel.
     - **Transcripts** are points from the full-resolution tiles, of the chosen genes or of all,
       while few enough tiles are in view (64 for chosen genes, 24 for all). Farther out they are
       density grids at the level whose bins are a few pixels wide, on a logarithmic scale set by
       the coarsest level's 99th percentile.
     - **Outlines:** cell and nucleus outlines from the geometry level matching the image level,
       once a cell spans about ten pixels.
     - **Fills:** cells by an attribute, through a per-cell color texture; rings triangulated by
       ear clipping, with a fan for rings that cross themselves.
     - **The selected cell** is drawn from its full-resolution polygons.
     - **Tiles** are fetched nearest the centre of the view first, a few at a time; those no
       longer wanted are not fetched, and the GPU's copies are kept within a budget.
   - **Program** (`internal/view`, `api_view.go`):
     - `GET /api/local/{i}/view`: what can be shown, with each channel's default window (the 1st
       and 99.5th percentiles of its coarsest level) and the density scales.
     - **Tiles by entry:** `GET /api/cache/{entry}/image|points|density|polygons/…`. Image and
       polygon tiles are sent as stored (`Content-Encoding: deflate`); points and density are cut
       to the chosen genes and quality. An entry's name hashes its sources, so tiles are marked
       immutable and the browser keeps them.
     - `GET /api/local/{i}/cells`: the cells table, binary.
     - `GET /api/local/{i}/cells/{row}`: one cell, with its transcripts counted from the index.
     - `GET /api/local/{i}/points?x0&y0&x1&y1`: a region's transcripts.
     - Each segmentation method comes from cells.parquet or, in XOA 2.0, from cells.zarr.zip.
   - **The Viewer** (`ui/mode-viewer.js`, `ui/viewer-panel.js`):
     - **Navigation:** drag, scroll, pinch, double-click and the keyboard to pan and zoom.
     - **The layers panel:**
       - channels: shown or not, color, window, gamma;
       - transcripts: gene search, up to 16 genes in distinct colors, or all transcripts; the
         quality floor, point size and density brightness;
       - cells: outlines, nucleus outlines, a fill by an attribute with its legend.
     - **Around the tissue:** a scale bar, the µm readout and a minimap.
     - **Clicking a cell** puts it in the inspector: its ID, centroid, areas, segmentation method,
       Q20 gene transcripts counted from the index, and top genes (a click shows one).
     - **Defaults:** at first only the nuclear and boundary stains show, since four stains added
       together wash out.
     - **From the Data view:** **View the tissue** opens a dataset whose derived data are ready.
   - Validation:
     - **In headless Chrome** (`validation/viewer.mjs`, `node validation/run.mjs viewer`; the
       Lung bundle in CI with software WebGL, both weekly):
       - **Exact samples:** every channel drawn at levels 0, 1 and the coarsest is read back
         before any window or color, across tile edges and at the image's edges. All samples
         equal the derived data's tiles: 271,214 in Lung and 1,082,848 in Breast, with a GPU and
         with software WebGL.
       - **Points:** the transcripts loaded for a region equal the program's query of the index,
         for three genes and for all transcripts (4,208 to 46,153 points).
       - **Speed on an Apple M4:**
         - panning at cellular zoom and zooming over the whole tissue, while data arrive and once
           loaded: 60 frames per second (the display's rate), a frame drawn in about 0.4 ms;
         - region queries in 3–23 ms;
         - a new view drawn whole in 14–30 ms, a revisited one at once.
       - **Software WebGL** (as on CI): the same exactness, and 23–31 frames per second, reported
         but not held to the budget.
     - **In Go** (`internal/view`):
       - region queries equal a scan of transcripts.parquet, for genes, quality floors, regions
         across tile edges and past the image;
       - density by gene sums to each gene's Q20 count;
       - every cell's Q20 gene transcripts counted from the index equal cells.parquet's
         `transcript_counts`, for all 19,173 cells of both bundles;
       - a cell's detail has its boundary exactly;
       - the methods' counts equal validation's.
     - **Endpoints:** tiles as stored, points and density as the index has them, cells,
       refusals.
     - **Node tests:** the camera, levels and tile ranges, the scale bar, triangulation (concave,
       crossing and degenerate rings), point in ring, and the binary answers.
     - **In the window:**
       - open a dataset from the Data view;
       - find genes by typing;
       - points up close, density farther out;
       - click a cell to inspect it;
       - fill by segmentation method;
       - no WebGL or console errors.
   - Found by slice 5:
     - **Four stains added together wash out the image:** the nuclear and boundary stains show at
       first.
     - **A sampler must always have a texture of its kind bound:** the outline shader's
       per-cell color lookup, unused for plain outlines, made WebGL refuse the draw while an
       integer image tile was bound. It now has its own texture unit with a placeholder.
     - **Outlines drawn at whole-tissue zoom only grey the image:** they are drawn once a cell
       spans about ten pixels.
     - **XOA 2.0 records each cell's segmentation method only in cells.zarr.zip.**
6. **QC (Q1, Q2, Q3, Q4, V6): done.** The QC view (`ui/mode-qc.js`) has four parts.
   - **Run metrics:** every recorded metric of `metrics_summary.csv`, explained
     (`lib/metrics.js`).
     - **Recomputed:** validation (`internal/xenium`) now recomputes 20 of them from the data (10
       more than slice 3), plus two values of the manifest. The view shows SituCraft's value
       beside the recorded one.
     - **New definitions found:**
       - nuclear transcripts are counted per area of the nucleus *mask*, not of the polygons;
       - the false-positive estimate is controls per control and cell × genes;
       - the Q20 fraction is over gene transcripts;
       - empty cells, median genes per cell and the segmentation methods' shares.
     - **Not reproduced:** four metrics (the region's area, the two "adjusted" control rates, the
       transcript layer's thickness) are shown as recorded, with why (`research.md` §1.3b).
   - **The tissue with a QC map:**
     - **Per cell:** gene transcripts, genes detected, all transcripts, negative controls, cell
       and nucleus area, the nucleus's share of the cell, transcript density, the share of
       transcripts in the nucleus, segmentation method.
     - **Per density bin:** the share of gene transcripts outside cells (unassigned), the share
       below quality 20 (Q-score), negative-control density and gene transcript density. These
       read three new groups of transcripts the density grids now count, so the cache format
       went to version 2: gene transcripts, gene transcripts in a cell, and negative controls.
       Ratio maps leave out bins of fewer than four transcripts.
     - **The cells table** gains control counts, genes detected and nuclear transcripts, the last
       two counted from the transcript index.
   - **Plots** (Canvas 2D): a histogram (linear or log), box plots by segmentation method, and a
     scatter plot of any two attributes.
     - **Brushing:** dragging chooses cells, shown in the plots, in the tissue (magenta, here and
       in the Viewer) and in the inspector (counts and medians against all cells).
     - **To a filter:** a brushed range becomes a filter condition in one click.
   - **Filters** (`lib/filters.js`):
     - **Conditions** on the attributes (bounds included; a cell without a value fails). Each
       condition's effect is counted as it is edited: cells failing it, and failing only it.
     - **Effects:** the cells and the share of transcripts kept, the median per cell, and the
       removed cells shown in the tissue.
     - **Saved as versions:** project artifacts of kind `filter`, never replaced. Each records the
       count and the SHA-256 of which cells it keeps, and on reopening is checked to keep them
       again ("Reproduced").
     - **Compared:** kept by both, by one or by neither, with the difference shown in the
       tissue.
     - **Used in plots and maps,** and listed in the project tree and the palette.
   - **Viewer navigation** moved to `ui/navigation.js`, shared by both views.
   - Validation:
     - **Run metrics** (`internal/xenium`, both bundles): 22 of 22 recorded values reproduced
       (20 of `metrics_summary.csv`, 2 of the manifest); the recorded metrics are carried whole
       in the report.
     - **The QC maps' data** (`internal/view`): the whole tissue's bins give exactly the run's
       share of gene transcripts in cells and its Q20 share. The cells table's genes per cell give
       the recorded median (25 and 46).
     - **Cache:** each group's density grid sums to its count at every level, and the groups'
       counts follow from the genes'.
     - **In headless Chrome** (`node validation/run.mjs viewer`, both bundles):
       - all recomputable metrics agree in the view (20 of 20);
       - brushing gene transcripts 20–100 chooses exactly the cells a count in Node finds (9,926
         and 2,995);
       - the choice shows in the Viewer;
       - a saved filter keeps the cells an independent evaluation in Node keeps, under the same
         SHA-256 (from Node's own hash);
       - all six kinds of map draw without a WebGL error.
     - **Node tests:**
       - the attributes and NaN handling;
       - filter evaluation (bounds, categories, missing values);
       - the checksum's layout, versions, removal and comparison;
       - histograms, quantiles, box statistics and brushing;
       - the metrics' explanations (every one says how it is computed or why not).
     - **In the window:**
       - metrics explained, 20 of 20 reproduced;
       - a cell map and a bin map;
       - brushing the histogram and the scatter plot;
       - two filter versions saved, compared, and both reproduced after a reload.
   - Found by slice 6:
     - **Nuclear transcript density divides by the nucleus mask's area**, not the polygons'.
       `masks/1`'s area is exactly the recorded total cell area.
     - **Four recorded metrics cannot be reproduced** from the bundle (above).
     - **The chosen cells' yellow highlight vanished into viridis's yellow end:** it is magenta.
7. **Saved views, the record and the release (V8, E4, D9, D7): done.**
   - **Saved views** (`lib/views.js`):
     - **What a view keeps:** the dataset and mode (Viewer or QC), the camera in µm (center, and µm
       per CSS pixel to 12 significant digits), each stain by name with its visibility, color,
       window and gamma, the genes by name, the transcript and cell layers, the selected cell, and
       in QC the map and filter.
     - **Where they are:** **Save view** in both views; listed in the project tree and the palette;
       in the inspector, **Open**, **Copy link**, **Rename**, **Remove**.
     - **Deep links:** `#project=…&view=…` loads the project from the library and opens the view.
   - **Project archives (`.scz`, D7):** written where the user says, from this computer only
     (`api_project.go`). Each dataset gains its place relative to the archive, and a bundle beside
     the archive wins when it is opened. An archive whose project the library holds opens as a
     copy. Opened from the command line or **Open a bundle**.
   - **The methods paragraph** (`lib/methods.js`, **Methods** in the Data view):
     - **What it says:** the platform, panel and software, the segmentation, the Q20 transcripts
       counted, how SituCraft read and checked the bundle (version, commit, findings, metrics
       recomputed), the chosen filters (with their checksums), and the data's source for public
       data.
     - **References:** Janesick et al. 2023 for the platform, the data's publisher, and SituCraft's
       version.
   - **Examples** (`internal/examples`, `api_examples.go`, the start page):
     - **What:** the two development bundles, listed from the embedded `validation/sources.json`
       with their summary, source, license (linked) and sizes.
     - **Downloaded only when asked,** as a job with progress and **Stop**: free space checked
       first; the archive by size, MD5 and SHA-256; every extracted file by size and SHA-256; then
       the archive deleted. A failure or a stop leaves nothing. The bundle then opens like any
       other.
     - `--examples-dir` (default `<data dir>/examples`). Downloading and removing are refused to
       other computers.
     - A dataset is known as an example by its manifest's SHA-256, so its methods cite the data.
   - **Guided tours** (`lib/tours.js`, `ui/tour.js`): seven steps per example through the Data
     view, the Viewer and QC, each opening a view without keeping it. The genes are among the
     section's most detected, and each zoomed step looks where its genes meet (`research.md` §7).
   - **Documentation:** `docs/GETTING-STARTED.md`, `docs/FORMATS.md`, the README for the release,
     and the changelog.
   - Validation:
     - **In headless Chrome** (`node validation/run.mjs viewer`, 46 checks):
       - the examples are listed and none is downloaded unasked;
       - every step of both tours opens with its genes;
       - the project saved by 0.1.0 (`validation/projects/0.1.0-lung.situcraft.json`: the Lung
         dataset, a filter in two versions, three views) opens, finds its moved bundle again
         (**Locate**), its filters keep exactly the cells they recorded (an evaluation in Node, and
         Node's own SHA-256), and its three views reopen exactly, from the project and from a link.
     - **Go tests:**
       - an example downloads, is checked, extracted and opened, and is removed;
       - a wrong MD5, a wrong size, a file's wrong SHA-256, a missing file, a server's error, a
         member leaving the folder, too little free space and a stop each leave nothing behind;
       - downloading and removing are refused to other computers;
       - the program's examples are the validation suite's sources;
       - an archive keeps its bundles when the folder holding them moves; existing archives are
         not overwritten unasked; relative paths are refused.
     - **Node tests:**
       - views capture by name, reopen exactly through any pixel ratio, and report what a dataset
         lacks; links round trip;
       - the methods paragraph says what was done and nothing else, the same each time;
       - every example has a tour whose steps are complete, and each gene a tour names is in its
         panel;
       - the 0.1.0 project parses with every field kept, and its views and filters refer to what
         it holds.
     - **In the window:**
       - both examples downloaded from 10x Genomics (the Breast bundle in seconds), checked and
         toured;
       - the methods paragraph of the Lung example cites the data;
       - saved views reopened, and their links copied.
   - Found by slice 7:
     - **The first gene's color was nearly the nuclear stain's blue** (`#4c78e0` against
       `#4f7cff`): tours show nuclei in gray behind colored transcripts.
     - **The Lung section has few alveolar or B-cell transcripts**, and its panel lacks PTPRC, so
       its tour shows epithelium, fibroblasts, vessels and macrophages (`research.md` §7).
     - **The 10x CDN now accepts any User-Agent** (it once refused requests without a
       browser-like one).
   - Release 0.1.0: the version agrees across `main.go`, `web/app.js`, `index.html`'s cache keys,
     `CITATION.cff` (dated) and the changelog.

## 0.2.0: compare segmentations (wave 2)

Segmentations become artifacts that can be compared, reviewed and promoted. The instrument's
segmentation is the first. SituCraft's nucleus expansion is the second. Segmentations made by
other tools can be imported.

### Wave 2

1. **Remote control and screenshots (A1): done.** Programs on this computer drive the open
   window, which performs each action where the user sees it.
   - **The hub** (`remote.go`, `connection.go`, `output.go`, adapted from MaveScape 0.8), with
     `--remote-control`:
     - actions POSTed to `/api/remote/action` reach the page over server-sent events, one at a
       time, and the page's answer returns;
     - only this computer, no other site's page, and only the window opened from this run;
     - opening paths and writing files need the token printed at startup and kept in
       `remote.json` (mode 0600, removed on stop); a file is written through a one-use upload
       slot, never over an existing one unless asked;
     - `GET /api/remote/tools` lists the actions with their arguments' JSON schemas, under an
       API version (also in `/api/info`).
   - **20 actions** (`actions.go`; performed by `ui/remote.js`, whose table a test keeps equal to
     the program's list and to `docs/REMOTE-CONTROL.md`):
     - **state:** `get_state`;
     - **projects and data:** `new_project`, `open_project`, `open_paths`, `add_dataset`,
       `open_example`, `tour`;
     - **seeing:** `set_mode`, `focus`, `show_tissue`, `select_cell`;
     - **saved views:** `save_view`, `open_view`, `remove_view`;
     - **QC:** `qc_metrics`, `set_qc_map`, `select_cells`, `save_filter`;
     - **the record:** `methods`, `export` (the project document, an archive, the methods).
   - **How the actions run:**
     - `show_tissue` builds a view from its arguments and applies it as a saved view is applied;
       its answer is the view as the page then captures it.
     - An action closes a dialog left open, as Escape does.
     - A cell is found by its ID (the host's cell endpoint now takes `id:<cell_id>`).
     - The methods dialog moved to `ui/methods.js`, shared by the Data view and the `methods`
       action.
   - **Screenshots** (`docs/capture/capture.mjs`): 11 scenes (start, data, viewer, viewer-genes,
     viewer-methods, qc, qc-outside, qc-filter, saved-views, methods, tour) on the development
     bundles, in both themes. They are driven through remote control in headless Chrome, as
     `docs/images/*.webp` (5.3 MB), and shown in the README and the getting-started guide.
     - With `--audit`, axe-core 4.13.0 (`node validation/fetch.mjs axe-core`, a new `tools` entry
       of `validation/sources.json`) checks each scene against WCAG 2.1 A and AA.
     - The places shown are the default ones, not the run's temporary folders.
   - **Documentation:** `docs/REMOTE-CONTROL.md` (starting, sending actions in curl and Python,
     status codes, security, every action).
   - Validation:
     - **Go tests** (16 of the hub, most adapted from MaveScape's):
       - an action reaches the page and its answer returns;
       - one action runs at a time;
       - no page, a silent page, a page that leaves and a page of another run are refused
         plainly;
       - only this computer may send actions, and no other site's page;
       - opening paths and writing files need the token;
       - exports never replace a file unasked, and an upload slot takes one upload;
       - the connection file is private and removed on stop, and another server's is kept;
       - without the flag there is no hub;
       - the tools list is complete;
       - the page's action table, the program's list and the documentation name the same actions;
       - a cell is found by its ID.
     - **In headless Chrome** (`node validation/run.mjs viewer`, 52 checks; the program now runs
       with `--remote-control`):
       - `get_state` describes the open project;
       - `show_tissue` shows exactly the genes, place, zoom, cell and fill asked for;
       - a view saved by a program reopens exactly;
       - a program's filter keeps the cells, under the same SHA-256, that the 0.1.0 project's
         identical filter recorded;
       - the methods cite the data and SituCraft;
       - `export` writes the project only with the token.
     - **axe-core:** all 22 captures (11 scenes, 2 themes) have no violation.
     - **By hand:** every action run from a script against the window, with its errors (unknown
       names listing the choices, bad colors, unknown stains, a missing cell, a file that exists,
       a missing token, another site's page).
   - Found by slice 1:
     - **The audit found three accessibility faults in 0.1.0,** now fixed:
       - the view rail's separators inside its tab list;
       - the project tree's empty sections marked as lists, and its rows (buttons) as list items;
       - a hidden stain's name dimmed below WCAG contrast.
     - **A dialog left open** (the methods, say) covered what later actions showed: actions now
       close it first.
2. **Segmentations as artifacts (D8, S6, S9): done.**
   - **The artifact store** (`internal/artifacts`, `<data dir>/artifacts`):
     - an artifact is a folder with `manifest.json`: its lineage (parents, the job and its spec,
       the method with its version and citation, the parameters, the software), every file with
       its role, size and SHA-256, its check's findings, and its state;
     - a job writes outputs into `staging/<id>`; `Promote` checksums them, refuses unlisted outputs
       and paths leaving the folder, runs the kind's check, and renames the folder into place,
       complete or invalid (kept for review, never used); an artifact never changes;
     - a file of the dataset's bundle is recorded by its path and SHA-256 and never copied;
     - `Verify` checksums an artifact's files again.
   - **The instrument's segmentation as an artifact** (`api_segmentations.go`):
     - a job registers it, from the bundle's cells, cell and nucleus boundaries, transcripts (their
       assignments), cell-by-gene counts and segmentation masks: each file's SHA-256 (from the
       cache's record of checksums when it has them);
     - its check is the bundle's validation, the findings that bear on the segmentation recorded
       with it; a fatal one makes it invalid;
     - its lineage names the bundle (analysis UUID, the manifest's SHA-256), the software, and the
       cells each method segmented;
     - its id hashes its files' checksums, so the same bundle gives the same artifact in any
       project.
   - **In the project** (`lib/segmentations.js`):
     - a segmentation record mirrors the artifact;
     - a dataset's default is set by `promote-segmentation` decisions, with a reason, by whom and
       when, never removed: the latest one naming a complete segmentation holds;
     - the instrument's is registered and promoted (as the dataset's only segmentation) when a
       dataset is added, a project reopens (0.1.0 projects included) or a moved bundle is located.
     - The project tree lists segmentations, the default marked; the inspector shows the lineage,
       the cells by method, the files by SHA-256, the findings, and the decision that made it the
       default.
   - **Jobs completed** (`internal/jobs`):
     - jobs are kept in `<data dir>/jobs`, one file each, written at each change of state and each
       line of the log, with ids unique across runs;
     - a job interrupted by the program stopping, or found running after a crash, is orphaned;
       resumed by its kind's handler from its spec (same id, its log kept, its runs counted), or
       discarded with what it left (a partial download);
     - handlers for derived data, segmentation registration and example downloads.
   - **Closing:** the window asks before closing with a job running; closed anyway, SituCraft
     keeps running until the job ends (Ctrl+C stops it).
   - **The Jobs view** (`ui/mode-jobs.js`):
     - running and waiting, interrupted, and finished jobs;
     - each with its state, progress, times and runs, its whole log and its spec;
     - Stop, Resume and Discard; the status bar's job opens it.
   - **Remote control:** `segmentations` and `jobs`; `open_example` and `add_dataset` wait for the
     segmentation's registration; an action's wait for frames is bounded, so a hidden window still
     answers.
   - **Screenshots:** two more scenes (a segmentation in the inspector, the Jobs view); every scene
     passes axe-core.
   - Validation:
     - **Go tests:** the artifact store (checked outputs recorded with their SHA-256, an invalid one
       kept apart, unlisted outputs and escaping paths refused, bundle files referenced and
       verified, a changed file found); jobs kept, orphaned, resumed and discarded across managers;
       on the Lung bundle, a build interrupted by stopping resumes after a restart to entries that
       pass their check, and the segmentation's six files carry the SHA-256 a direct reading gives.
     - **In headless Chrome** (`node validation/run.mjs viewer`, 59 checks):
       - both bundles' instrument segmentations registered, every file's SHA-256 as Node computes
         it, promoted as the default, their methods accounting for every cell;
       - the 0.1.0 project given its own, the same artifact;
       - every job complete; `segmentations` and `jobs` by remote control.
     - **Node tests:** records, promotion (a reason required, only a complete segmentation, the
       latest decision holding), registering twice changing nothing.
     - **By hand:** SituCraft killed in the middle of the Breast bundle's build; on restart the
       build is interrupted, and resuming it continues the image pyramid where it stopped.
   - Found by slice 2:
     - **A hidden window draws no frames,** so a remote action waiting for two frames waited until
       the window was shown: the wait is now bounded.
     - **The documentation's capture shares one cache across scenes,** so a scene showing jobs
       needs its own (in the second theme the builds were already done).
3. **Nucleus expansion (S1, S9): done.**
   - **The method** (`internal/segment/expand`; research.md §3.1):
     - each nucleus grows into a cell of its own by up to the distance, every pixel going to the
       nearest nucleus within it (a Voronoi partition clipped at the distance, Okabe et al. 2000),
       by exact Euclidean distances between pixel centres (Felzenszwalb and Huttenlocher 2012);
       ties broken by position;
     - in bands of rows with a margin of the distance, on every core; the same result for any band
       height and number of workers;
     - the nuclei are the bundle's nucleus mask, or its nucleus polygons drawn when it has none;
     - a transcript belongs to the cell holding its pixel, as the instrument assigns them; counts
       of gene transcripts of quality 20 or more; outlines traced and simplified to half a pixel.
   - **Segmentations as files** (`internal/segment`; FORMATS.md): a description, the cells, the
     label raster in runs, each transcript's cell, the counts in sparse rows, and the outlines;
     zlib-compressed with sizes checked on reading.
   - **The check (S9)** before an expansion becomes an artifact: its files read and agree (the
     raster's labels are cells, areas and centroids are the pixels', outlines lie in the raster,
     every cell's assigned transcripts and counts add up); a fatal finding makes it invalid.
   - **The comparison with the instrument's segmentation:**
     - each nucleus is paired with the instrument's cell holding most of its pixels (a cell drawn
       from a stain may hold none, or several);
     - pixels and transcripts in the same cell, in another, or in only one segmentation; areas of
       the cells paired one to one; the assigned fractions;
     - what differs, described: how far from their nucleus the pixels only one gives a cell lie,
       how near to equidistant the pixels given to different cells are, the instrument's cells
       holding no nucleus, and transcripts the instrument records off its own mask.
   - **A job** (`api_expansion.go`): staged, checked and promoted; the artifact's lineage names
     the bundle and the instrument's segmentation it grew from, the method and its version, every
     parameter and the software. Its id hashes the instrument's segmentation, the method's version
     and the distance, so the same request answers the same artifact without a job.
   - **The Segmentation view** (`ui/mode-segmentation.js`): the dataset's segmentations side by
     side (cells, assigned fraction, transcripts per cell, area); **Expand the nuclei** with a
     distance, its job's progress shown; the comparison with the instrument's segmentation.
   - **Remote control:** `expand_nuclei` (23 actions).
   - **Screenshots:** the Segmentation view after expanding the Lung example's nuclei.
   - Validation:
     - **The Lung bundle's 5 µm expansion reproduced** (`TestRealExpansionReproducesTheInstruments`):
       11,898 cells paired one to one (exact); 98.06% of pixels and 98.94% of transcripts in the
       same cells (at least 98% and 98.5%); cell areas' median difference +0.25% (under 0.5%),
       97.0% within 5% and 99.95% within 10% (at least 96% and 99.5%); 98.28% of gene transcripts
       assigned, the instrument 97.97% (at most 0.5 points more). Every transcript given
       differently is explained, one by one: 2,534 on the rim (4.56–4.98 µm from their nucleus,
       which the instrument's cells do not reach), 6,182 on boundaries between neighbours (within
       0.34 µm of equidistant), 8 the instrument records off its own mask.
     - **Breast:** 7,312 nuclei grown into cells; the instrument's 7,275 cells accounted for (6,752
       with one nucleus, 255 with none, 268 with several).
     - **Go tests:** the distance transform against brute force; expansion geometry; any band and
       worker count the same; polygons drawn; counts; the files read back as written; the check
       finding each kind of damage (truncation, sizes, areas, centroids, assignments, counts,
       outlines); masks read in bands in either order; on Lung, the drawn polygons agree with the
       mask on 98.56% of pixels; the job, its artifact and its lineage through the program's API.
     - **In headless Chrome** (62 checks): `expand_nuclei` by remote control reproduces the Lung
       expansion, recorded in the project and shown with what differs; the same distance again
       answers the same artifact with no job.
   - Found by slice 3:
     - **Breast's nucleus labels are not its cells'.** Cells drawn from stains hold no nucleus or
       several, and the nucleus mask numbers its nuclei on its own; the first expansion of Breast
       failed. Each nucleus now grows into a cell of its own, paired with the instrument's cell it
       lies in.
     - **A truncated file read without error,** its last bytes (the zlib checksum) never read: each
       file is now read to its end.
     - **Nucleus polygons drawn by pixel centres missed their edge pixels** (92.0% of the mask's
       pixels): the polygons' vertices lie on the edge pixels' own coordinates (98.56%).
     - **The viewer suite counted jobs for two bundles,** failing on CI, which fetches one.
4. **Importing segmentations (S10, D6): done.**
   - **Formats** (`internal/segment/imports`; FORMATS.md):
     - SpatialData Zarr, format 2 or 3, a folder or a zip: the cells from shapes (GeoParquet
       polygons) or labels (a label image, named through the table annotating it), each
       transcript's cell from points, the counts from the table; each element's coordinate
       transformation;
     - Baysor's `segmentation.csv` with its polygons (a GeoJSON GeometryCollection);
     - GeoJSON polygons, with or without a transcript table (CSV, TSV, gzipped, or Parquet);
     - Xenium Ranger output bundles (and XOA bundles) of the same run: the cell mask, cells,
       boundaries and assignments.
   - **Readers of SituCraft's own:** AnnData in Zarr (`internal/anndata`: data frames with text,
     categorical, nullable and numeric columns; matrices as sparse rows, columns or dense); Zarr's
     sharding and text of varying length; GeoJSON and WKB polygons; CSV and Parquet tables.
   - **Each import becomes SituCraft's segmentation files:** a label image resampled, or polygons
     drawn, at the morphology image's resolution; the tool's assignment matched to the dataset's
     transcripts; counts of gene transcripts of quality 20 or more, as for every segmentation (a
     count table the tool wrote is compared with them); the tool's outlines.
   - **Checked against the dataset:**
     - units declared (µm, or pixels of the morphology image), never guessed: cells outside the
       image make the import invalid, and the finding says how many would fit in the other unit;
     - the transcripts it names, by id and, where it gives them, position (within 0.05 µm), else
       by position and gene (within 0.01 µm);
     - an output bundle's run, by its experiment;
     - ids: cells without polygons, labels without a table row, transcripts named twice.
   - **A job** (`api_import.go`): only this computer may ask; the import is staged, named by the
     dataset, the SHA-256 of the files read (not their places) and its options, checked and
     promoted, invalid when a check is fatal. Its lineage names the bundle, the imported files
     and the tool, with its version and citation.
   - **In the project:** imported segmentations recorded as such, with their source files; the
     Segmentation view's **Import a segmentation**; the inspector shows the files imported.
   - **Remote control:** `import_segmentation` (24 actions; it reads files, so it needs the token).
   - **Screenshots:** a segmentation imported from SpatialData beside the instrument's.
   - Validation:
     - **The Lung bundle:** imported into itself as an output bundle, and as spatialdata-io 0.7.1
       writes it (`validation/spatialdata.py`; weekly in CI), it gives the instrument's
       segmentation exactly: the raster identical to the cell mask, every transcript's cell, every
       cell's count. Its polygons and assignments written as GeoJSON with a table and as a
       Baysor-style table without ids give every assignment; drawn, the polygons agree with the
       mask on 94.33% of pixels (Xenium's polygons pass through their edge pixels' coordinates).
       The Breast bundle is refused as another run.
     - **Fixtures:** one hand-made scene written by spatialdata 0.8.0 and 0.2.6 (Zarr formats 3
       and 2), geopandas, pyarrow, pandas and zarr-python, imported to its pixels and
       assignments exactly, with each finding planted. AnnData and Zarr text and shards as anndata
       and zarr-python write them.
     - **In headless Chrome** (65 checks): `import_segmentation` imports the Lung bundle into
       itself only with the token; the Breast bundle is refused.
     - Round trips through SituCraft's own exports come with them (slice 7).
   - Found by slice 4:
     - **The development bundles share their run's name, region and transcript ids** (anonymized
       for sharing): the Breast bundle imported into the Lung dataset as if it were its own. An
       output bundle's run is now its experiment's UUID, and transcripts named by id must lie
       where the dataset's do.
     - **spatialdata-io's global coordinate system is in pixels,** not µm: the units are declared
       with each import, and the extent check names the unit that fits.
     - **The documentation's capture showed a path of this computer** in a text field: text fields
       are now shown with the places a user would see.
5. **Cell matching and metrics (C1, C2, C3): done.**
   - **Correspondence** (`internal/compare`; research.md §4): two cells correspond when they share
     a nucleus of the dataset, or when at least half of one lies within the other; cells without
     area by their transcripts. Each connected set is one difference: one-to-one, expanded or
     contracted (area beyond 1.25 times), split, merged, rearranged, only in A or B,
     assignment-only.
   - **Measures:** each segmentation's cells, nuclei, assigned fraction, transcripts per cell,
     areas, cells in pieces, job time and size; between two, the pixels that agree, the
     transcripts that changed cell (gained, lost, moved), the IoU and boundary displacement of the
     cells the same in both; by regions of 50 µm.
   - **Spurious co-expression** after Jones et al. (2025): the gene pairs co-expressed at least 1.5
     times as often once nuclei grow by 10 µm, each segmentation's co-expression of them relative
     to the nuclei's, cells down-sampled to 50 transcripts; **the mutually exclusive co-expression
     rate** of Hartman and Satija (2024), over pairs of lineage markers.
   - **Every segmentation as SituCraft's files:** the instrument's written from its bundle (row by
     row, so bundles without transcript ids too), with the nuclei and the coarse expansion, as
     derived data in the cache (listed, counted and evicted as the cache's others).
   - **A job** (`api_compare.go`) and an artifact of kind `comparison`: its measures, regions and
     differences (`groups.bin`), checked (every cell in one difference whose class fits it, the
     counts consistent); named by the two segmentations and the parameters, so asking again
     answers it. An artifact's own files are served for the page.
   - **The Segmentation view's Compare two segmentations:** A and B, the comparisons made, and a
     comparison's differences by class, each segmentation's measures, and what changed.
   - **Remote control:** `compare_segmentations` (25 actions).
   - **Screenshots:** the Breast example's instrument segmentation compared with its expansion.
   - Validation:
     - **Hand-drawn fixtures** of each class, every count and measure worked out by hand: a cell
       shifted one pixel (IoU 0.5, the boundary moved 0.5 µm on average), a boundary between
       neighbours moved (contracted and expanded), a split, a merge, columns become rows, a cell in
       one only, a cell without outline, two cells joined by a nucleus. A planted leak of
       transcripts raises the spurious co-expression (the nuclei score 1); the MECR by hand.
     - **The instrument's segmentation and the 5 µm expansion:** on Lung, 11,898 cells one to one,
       98.0629% of pixels and 8,724 transcripts changed, as the expansion's own comparison counted
       independently (slice 3); on Breast, every cell of both in its classes (3,599 one-to-one, 263
       splits into 550 cells, 128 merges of 259, 119 only the instrument's), the expansion's
       spurious co-expression 2.04 against the instrument's 1.77. Reference numbers in
       research.md §4.
     - **In headless Chrome** (67 checks): `compare_segmentations` by remote control, as the
       expansion counted, recorded and shown.
   - Found by slice 5:
     - **A fifth of either cell was too little to correspond:** neighbours whose shared boundary
       moved one column joined into one "rearranged" difference. Cells now correspond when half of
       one lies within the other: fragments of a split still do, neighbours do not.
     - **The MECR's reference** had a wrong identifier in research.md: it is
       doi:10.1101/2024.01.11.575135.
     - **A segmentation's cost:** the instrument's showed its registration's time; it shows none.
6. **The comparison workspace (V7, C4, C5): done.**
   - **The Compare view** (`ui/mode-compare.js`): a comparison's two segmentations on the tissue,
     side by side with one camera, overlaid (B's outlines over A's, each in its colour) or in
     turns (flicker); cells filled by the class of their difference, the classes chosen; the
     chosen difference lit in both and described in the inspector; a disagreement map (each 50 µm
     region shaded by its share of disagreeing pixels).
   - **Each segmentation's outlines as tiles:** the instrument's are the dataset's geometry; any
     other's are tiled once into a cache entry (`outlines`), served from `/api/outlines`; the
     renderer draws a descriptor's tiles wherever they are, and on a transparent canvas.
   - **The metrics header** with cautions (`lib/review.js`): few nuclei or gene pairs under the
     spurious co-expression, few marker pairs in the panel, cells without outlines, few cells the
     same in both.
   - **The review queue:** the differences ranked by reassigned transcripts, contamination (pairs
     of mutually exclusive markers in one cell of B more than of A; the comparison now records,
     per difference, its cells' gene transcripts and most mixed pairs: method version 2), by
     disagreement (class, then 1 − IoU) or by low counts, among the classes and the reassigned
     transcripts the reviewer's rules keep; Next and Previous (`]`, `[`). **The cell-matching
     table:** every difference, sortable, by pages.
   - **The decision panel:** promote, keep for exploration or reject either segmentation, with a
     reason (required) and the comparison it rests on, appended to the project's decisions
     (`lib/segmentations.js`); the default cannot be rejected until another is promoted; the
     dataset's decisions listed. Comparisons in the project tree and the inspector.
   - **Remote control:** `show_comparison` and `decide_segmentation` (27 actions); `set_mode`
     shows `compare`.
   - **Screenshots:** the workspace with a split chosen; the overlay with the disagreement map.
   - Validation:
     - **Node tests:** the queue's rankings and the reviewer's rules on hand-made differences; the
       cautions; decisions (a reason required, the default never rejected, appended with their
       comparison, a rejected segmentation promoted again).
     - **Go tests:** a segmentation's outline tiles built, served and opened; the comparison's
       differences with every cell's id; the instrument's outlines from the dataset's geometry;
       per-difference transcripts and mixed marker pairs.
     - **In headless Chrome** (70 checks): `show_comparison` ranks the queue as asked (the Lung
       cells, all one-to-one, least overlap first) and shows the first; both segmentations drawn
       with one camera in each mode (B transparent over A in the overlay); `decide_segmentation`
       refuses to reject the default and a decision without a reason, and records one with its
       comparison.
   - Found by slice 6:
     - **A new canvas's default camera** (0, 0 at scale 1) overwrote the shared one on its first
       frame: a canvas's camera now counts once the workspace has placed it.
     - **Two requests for the same outlines** built them at once, and the second failed: builds
       are serialized, and an entry that does not open is built again.
     - **The tissue was fitted to the canvases' first size,** before the layout settled: it is
       fitted again until the camera moves.
7. **Exports and release (E1, E2, E3): done.**
   - **Exports** (`internal/export`; FORMATS.md): any segmentation of a dataset, into one folder,
     in the formats chosen:
     - SpatialData Zarr (format 3, as the spatialdata package lays out a store): the label raster
       scaled into µm, GeoParquet outlines, the transcripts as three-dimensional points with their
       cells, the AnnData table annotating the labels; no images;
     - AnnData Zarr (format 2, anndata's default write format); Parquet and gzipped CSV (cells,
       transcripts, outlines and counts, in Xenium's column names); Matrix Market (as Xenium and
       Cell Ranger write it); GeoJSON outlines;
     - Xenium Ranger's inputs: the cells and the bundle's nuclei as NumPy label masks on the
       morphology image's pixels, `cell_labels.csv`, and `command.sh`, after checks (the masks'
       size, cells without pixels, transcripts assigned otherwise than the mask holds, no nuclei,
       a zipped bundle). SituCraft does not run it.
     - `situcraft-export.json`: what was exported, from what, by which SituCraft; every file by
       SHA-256; totals; fingerprints of the labels, counts and assignments that every format holds
       the same, so any reader can check it read the segmentation exactly.
     - Written under a temporary name beside the folder and renamed into place; a folder replaced
       only when asked and only when it is an export; the same segmentation gives the same files.
       Recorded in the project (`exports`, absent until the first, so earlier projects open
       unchanged).
   - **Writers of SituCraft's own:** Parquet (`internal/parquet`: required flat columns, PLAIN,
     GZIP, Thrift compact), Zarr formats 2 and 3 (`internal/zarr`: gzip, text of varying length),
     AnnData (`internal/anndata`).
   - **A job** (`api_export.go`, only from this computer), and the Segmentation view's **Export a
     segmentation**: the segmentation, the formats, the folder; the exports listed with their
     findings and Xenium Ranger's command (copied with a button).
   - **Errors planted for practice** (`internal/segment/plant`, `api_plant.go`): a copy of the
     instrument's segmentation with merges of neighbouring cells whose merged cell mixes lineage
     markers, splits across the long axis, missed cells and cells grown 1 µm into their
     neighbours, each kept apart from the others, seeded; a segmentation artifact with its answer
     key (`planted.json`), simulated, never promoted or exported. The Segmentation view's
     **Practice: find planted errors** plants, compares, and opens the answer key, each error shown
     in the workspace; it is offered on the example datasets, and on others only when the user
     turns on practice tools (off by default).
   - **Worked examples** (`lib/tours.js`, `ui/tour.js`): a question and what to expect, steps that
     run a job before going on or show a comparison as they describe it. The Breast example's: the
     instrument's cells against a 5 µm expansion. The Lung example's: find the planted errors.
     Listed on the example cards.
   - **The methods paragraph** describes a default segmentation other than the instrument's: its
     method and citation, and the decision choosing it with its comparison.
   - **Remote control:** `plant_errors`, `export_segmentation` (29 actions; exports need the
     token); `tour` takes a worked example and runs its steps.
   - **Screenshots:** a worked example in the workspace; an export with Xenium Ranger's command
     (20 scenes, 40 captures).
   - **Release 0.2.0:** the version, the changelog, the documentation, every screenshot captured
     again and audited, and the project the release saves (`validation/projects/0.2.0-lung`).
   - Validation:
     - **Go tests:** the writers' files read back as written; every format of a fixture
       segmentation read back by SituCraft's readers (the SpatialData store imported as the same
       segmentation; the GeoJSON polygons with the Parquet or CSV transcripts give every
       assignment); reproducible files; the folder's rules; Xenium Ranger's checks; planted errors
       on hand-made tissue each found as planted by the comparison; the program's plant and export
       jobs; a dataset opened by several requests at once.
     - **The Lung bundle** (`TestRealExports`, `TestRealPlantedErrors`): its own segmentation in
       every format, imported back from SpatialData exactly; Xenium Ranger's masks of the image's
       size, 9 transcripts assigned off the instrument's own mask; 60 errors planted, each found as
       what it is, nothing else differing.
     - **The maintainer's check** (`validation/reference/read_exports.py`; weekly with
       `--check`): spatialdata 0.8.0, anndata 0.13.4, pyarrow, pandas, scipy, geopandas and numpy
       read every format, giving the manifest's totals and fingerprints; its result
       (`exports.json`) is committed and the real-data tests check SituCraft still writes it.
     - **In headless Chrome** (80 checks): both worked examples run through by remote control;
       every planted error found as planted; a planted segmentation never promoted or exported;
       exports only
       with the token, imported back; the 0.2.0 project reopens and its artifacts are made again
       with the same checksums.
   - Found by slice 7:
     - **A dataset opened by several requests at once** could be closed by one while another read
       it ("damaged cache file"): the Compare view opened first, before any Viewer, asks four
       things at once. One request now opens it while the others wait.
     - **Grown by 2 µm, Lung's small cells** took more than half of a neighbour and became
       rearranged differences: they grow by 1 µm.
     - **Merges chosen by unlike counts alone** mixed lineage markers in 5 of 20: they are chosen
       first among pairs whose merged cell would.
     - **spatialdata writes points with z in three dimensions:** the export does the same.
     - **A project reopened must stay as it was saved:** exports are added to a project with its
       first, not to every project opened.
     - **Zarr format 3's text scalars** (a SpatialData table's region) were not read.
     - **anndata 0.13 writes Zarr format 3 by default;** the AnnData export keeps format 2, which
       every version reads.
     - **Artifacts differed between processors:** CI (Linux, amd64) reopened the 0.2.0 project
       written on a Mac (arm64), and the expansion's and planted errors' outlines had other
       checksums. The compiler fuses multiply-adds on arm64 and not on amd64; the outline
       simplification now uses exact arithmetic, and products reaching an artifact are rounded on
       their own (conventions.md). The planted errors' sums over a map, whose order Go randomizes,
       are taken in order. Both builds now give the same files.
     - **Job ids repeated** when the program started again within the same millisecond (a test
       restarting the job manager found it): an id in use is skipped.
     - **Practice belongs with the tutorials:** offered on every dataset, planting errors put
       simulated cells one click from a real project, and from an export. It is offered on the
       example datasets only, unless the user turns practice tools on, and planted segmentations
       are never exported.

## 0.3.0: transcript-informed segmentation, built in (wave 3)

SituCraft's own transcript-informed segmentation, after Jones et al. (2025). It needs nothing but
the binary, gives the same result for the same seed, and is validated against simulated truth and
independent reference outputs.

### Wave 3

1. **Simulated tissues first (S2).** `internal/simulate` and `situcraft simulate` build seeded
   tissues with known cells. They have:
   - shapes and densities by cell type;
   - expression from a negative-binomial mixture;
   - leakage and diffusion of molecules, background noise, and z structure;
   - nuclei as the prior.

   They are written as Xenium-shaped bundles (labeled simulated), so any segmentation, SituCraft's
   or another tool's, can be scored against the truth. The simulator is independent of the
   segmentation code.
   - Validation: simulated counts and positions follow their specified distributions
     (goodness-of-fit tests).
2. **The model (S2).**
   - Voxel lattice and cell state.
   - Poisson likelihood with per-gene background.
   - The gamma–negative-binomial expression mixture with its conjugate and augmented updates
     (Chinese-restaurant-table and Pólya–Gamma), the log-normal volume prior, and the nuclear
     prior.

   The equations are written out in `research.md` §3 before the code.
   - Validation: each conditional update agrees with its analytic posterior on small cases, or
     with a brute-force numerical posterior.
3. **The sampler (S2, S8).**
   - Proposals: voxel-label copy, Metropolis–Hastings with proposal correction, bubble proposals.
   - Constraints: connectivity (articulation check) and the perimeter bound.
   - Multi-resolution: 4 → 2 → 1 µm, with z layers.
   - Repositioning: the transcript-repositioning mixture.
   - Parallelism: checkerboard blocks, each with its own random stream.
   - Validation:
     - On lattices small enough to enumerate every state, sampled state frequencies match the
       exact target distribution (detailed balance).
     - A seed gives byte-identical output on 1, 4 and 16 threads.
     - Cells are recovered on simulations (IoU and assignment accuracy against truth, better
       than nucleus expansion where leakage is simulated).
4. **Outputs (S9, S11).**
   - Polygons per z layer and in consensus; transcript assignments with probabilities; counts.
   - Convergence diagnostics.
   - The artifact enters the comparison workspace automatically.
   - Limitations flagged per cell (low transcripts; no prior nucleus).
5. **The segmentation studio (S3, S4, S5, S7).**
   - The Input → Compute → Model → Review → Run → Compare flow, with presets and expert settings
     in plain language.
   - Preflight memory and disk estimates (validated against measured runs); regions and separate
     sections.
   - Progress, cancellation and a failure classifier with remedies.
   - Job specs saved as recipes (E5).
6. **Validation against reference outputs.** On both 2-FOV bundles and on simulations, SituCraft's
   segmentation is compared with Proseg 3.2.0 as an independent reference. The reference
   outputs are generated by the maintainer and reduced to numbers in
   `validation/reference/segmentation/`. The comparison covers:
   - cell counts;
   - the assigned fraction;
   - spurious co-expression;
   - matched-cell count correlation;
   - IoU distributions;
   - recovery of simulated truth.

   Each tolerance is documented, and differences beyond it are explained or fixed.
   - Budgets: the Breast 2-FOV under 2 minutes and 4 GB on a laptop (targets, confirmed by
     measurement).
7. **Larger tissues and release (S12, P6).**
   - Overlapping patches with stitching, so that memory is bounded for any tissue size.
   - Memory guards.
   - A scale test on a generated tissue of ~50 million transcripts, created, measured and deleted
     (weekly workflow).
   - Examples (refine the Breast bundle's segmentation and compare; a simulation with known
     truth), documentation, release 0.3.0.

## 0.4.0: cell identities (wave 4)

Outline; planned in detail when reached.
- Normalization, principal components, kNN graph and Leiden clustering, seeded and transparent
  (T2). UMAP linked to the tissue (T3).
- Marker-based hierarchical labels with inspectable scores (T4). Imported labels (T1). Optional
  reference mapping (T5).
- Evidence cards, label versions and the low-confidence queue (T6, T7, T8). Cluster markers,
  labeled exploratory.
- Labels per segmentation, and composition changes in the comparison's downstream-impact tab (C6).
- Validation: clustering and embeddings against reference libraries on the development bundles
  (partition agreement, trustworthiness), marker scores against hand computation.

## 0.5.0: the pathology bridge (wave 5)

Outline.
- Post-Xenium H&E and IF OME-TIFF import: JPEG, deflate, LZW and JPEG 2000; RGB pyramids (R1).
- Supplied alignment matrices (R2); landmark and automatic registration with optional non-rigid
  refinement (R3); alignment QC in µm (R4); versioned transforms (R5).
- ROI tools and GeoJSON interchange (R6); review actions, queues and provenance (R7, R8, T8).
- The pathology fixture (≤ 3 GB, post-Xenium H&E with alignment) chosen and added to
  `validation/sources.json`.
- Validation: identity and known-affine recovery, the supplied 10x transform reproduced, landmark
  residuals reported.

## 0.6.0: niches and spatial statistics (wave 6)

Outline.
- The graph builder (N1); neighborhood enrichment, co-occurrence, distances and Ripley's K/L (N2,
  N3); infiltration and exclusion (N4); cellular neighborhoods (N5); ROI comparison (N6);
  spatially variable genes (N7).
- The structured analysis builder; every result showing its contributing cells (N8).
- Segmentation sensitivity: any saved analysis re-run against another segmentation (C7).
- Validation: analytic geometries; reference-library results on the development bundles; seeded
  permutations reproducible.

## 0.7.0: samples and cohorts (wave 7)

Outline.
- Several datasets per project (D10); the experimental design (H1); sample summaries (H2); cohort
  QC (Q5).
- Sample-level inference with effect sizes, intervals and recorded corrections (H3); batch and
  panel checks (H4, H5).
- Data plan: simulated cohorts with known effects (labeled simulated), plus regions of the
  development bundles treated as pseudo-samples only in clearly labeled teaching examples.
- Validation: inference on simulated designs reaches its nominal error rates; cell-level
  pseudoreplication is refused.

## 0.8.0: figures and a provable record (wave 8)

Outline.
- Figures (SVG, PDF, PNG) with scale bars, legends and provenance (E6), adapted from MaveScape.
- The full methods text and citation helper (E4, E9).
- A certificate and `situcraft verify` (E7).
- The redacted static result package (E8); the report.

## 0.9.0: agents and scripts (wave 9)

Outline.
- `situcraft mcp` (A2), with the typed filter tree (A3) and audit log (A4).
- Headless `situcraft run` for batch and HPC (A5); recipes re-run headless (E5).
- R and Python clients (A6); the agent benchmark, as in the siblings.

## 0.10.0: whole slides and other platforms (wave 10)

Outline.
- Whole-slide performance on a large public bundle (on a machine with room) and on generated
  tissues of billions of transcripts.
- CosMx and MERSCOPE readers (D11).
- XOA 3.x/4.x and Xenium Prime 5K coverage completed.

## Toward 1.0

- Stable, versioned project and export formats, with migrations and the promise that later
  versions open them (X5).
- A security review; signed releases; accessibility and documentation complete.
- External use and review, arranged by the maintainer outside this plan. No wave waits for it;
  what it reports is taken in when it comes.

## Parking lot

- Ligand–receptor analysis (N9), only with its restraints.
- Deep-learning nucleus segmentation from stains (nuclei come from the bundle until then).
- A remote/institutional multi-user mode (authentication, CSRF, audit).
- HDF5 (`.h5ad`) export, if users need it beyond AnnData Zarr.

## Risks

| Risk | Why it matters | What SituCraft does |
| --- | --- | --- |
| JPEG 2000 decoding is slow or incomplete | No images | Feasibility slice before any viewer; tile-by-tile equality with an independent decoder; decode once into the tile cache; a recorded budget |
| The data plane is a large amount of bespoke code | Schedule | Formats limited to what real bundles use; each reader checked against an independent reader on every file; fuzzing |
| Built-in segmentation behaves differently from the published method | Trust | Simulated ground truth; exact small-lattice tests; comparison with reference outputs under documented tolerances; method version in every artifact |
| Segmentation runs exhaust memory | Failed runs | Preflight estimates; ROI pilot; low-memory preset; patches |
| Any segmentation is treated as ground truth | Overclaiming | Multi-evidence comparison; limitations shown; rationale required to promote |
| Xenium formats change | Import breakage | Version detection; capability-based readers; fixtures per version |
| Little disk for development | Slow progress | Two small bundles; simulations generated and deleted; a capped cache |
| Pseudoreplication in cohort analysis | Inflated significance | A design before inference; sample-level defaults |
| One developer | Continuity | Validation and documentation that let others check and continue |
