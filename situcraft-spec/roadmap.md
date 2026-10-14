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

1. **Remote control and screenshots (A1).** The remote-control hub, actions and `remote.json`
   from MaveScape. The documentation's capture scenes drive the page through it in headless
   Chrome, as in the siblings.
2. **Segmentations as artifacts (D8, S6, S9).**
   - The artifact store: states, lineage, temporary outputs promoted after validation.
   - The instrument's segmentation registered as an artifact.
   - The job system completed: jobs persisted; a warning before closing with a job running;
     interrupted jobs marked orphaned on restart, with resume or discard.
   - The Jobs view.
3. **Nucleus expansion (S1).** `internal/segment/expand`: neighbor-bounded growth of nuclei to a
   chosen distance, transcript assignment and counts; parallel and deterministic.
   - Validation: the Lung bundle's own 5 µm expansion is reproduced. The documented tolerance
     covers cell count (exact), per-cell area, the assigned fraction, and per-transcript
     assignment agreement, with every disagreement explained (e.g. vertex simplification in the
     bundle's polygons).
4. **Importing segmentations (S10, D6).**
   - SpatialData Zarr (shapes, points with assignments, AnnData table).
   - Baysor-style assignment CSV plus GeoJSON polygons.
   - Xenium Ranger output bundles.
   - GeoJSON plus a transcript-to-cell table.
   - Each import is validated against the dataset (IDs, coordinate system, extents).
   - Validation: round trips through SituCraft's own exports (slice 7). Hand-made fixtures for
     each format.
5. **Cell matching and metrics (C1, C2, C3).** `internal/compare`:
   - correspondence by shared nucleus, raster IoU and assignment overlap;
   - difference classes;
   - global and per-region metrics, including the spurious co-expression score of Jones et al.
     (2025) and MECR.
   - Validation: hand-auditable fixtures (two-cell splits, merges, shifted boundaries) with
     expected classes and metrics. Instrument versus expansion on both bundles, with recorded
     reference numbers.
6. **The comparison workspace (V7, C4, C5).**
   - Synchronized A/B canvases, overlay and flicker; a whole-tissue disagreement heatmap;
     difference-class filters.
   - The ranked review queue; the cell-matching table; the metrics header with uncertainty
     warnings.
   - The decision panel: promote, keep for exploration or reject, with a required rationale
     recorded as an append-only event.
7. **Exports and release (E1, E2, E3).**
   - SpatialData Zarr (shapes, points, AnnData table, transformations); AnnData Zarr; Parquet;
     CSV; Matrix Market; GeoJSON.
   - A prepared Xenium Ranger `import-segmentation` command, with validated inputs, for users
     who have it installed.
   - Validation: exports are read back by SituCraft. A maintainer-run check reads them with the
     `spatialdata` and `anndata` Python packages; its result is committed.
   - Examples (compare the instrument's segmentation with an expansion; a planted bad
     segmentation to find), documentation, release 0.2.0.

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
