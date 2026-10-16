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

1. **Simulated tissues first (S2): done.**
   - **The foundations, the same on every processor:**
     - `internal/random`: seeded PCG streams with normal, log-normal, exponential, gamma, Poisson
       and negative binomial draws, every product rounded on its own;
     - `internal/dmath`: fdlibm's logarithm and exponential in Go, bit for bit as
       `web/lib/dmath.js`;
     - `internal/stats`: the incomplete gamma function, Kolmogorov–Smirnov and χ² tests;
     - a TIFF writer (`internal/tiff`): tiled, deflate-compressed OME-TIFF with SubIFD levels;
     - an HDF5 writer (`internal/hdf5`): groups, fixed-length strings and integers, and
       attributes, enough for the 10x Genomics feature-barcode matrix.
   - **The simulator** (`internal/simulate`; research.md §3.4):
     - domains of different composition; cell types with their own sizes, elongations, outlines,
       densities, markers and dispersions, placed by random sequential adsorption and competing
       for pixels;
     - nuclei, a share outside the section; depth;
     - negative-binomial expression by type, with a size factor;
     - molecules enriched over nuclei, displaced by a wide/narrow normal mixture and in depth;
       Poisson background and negative controls; quality scores;
     - a simulated instrument segmenting by chamfer nucleus expansion; a DAPI image.
   - **The bundle:**
     - every file of the XOA 2 layout, so the validator, viewer, QC, expansion, imports and
       comparison read it as a measured one;
     - the counts also as `cell_feature_matrix.h5`, and `cells.zarr.zip` as XOA 2 lays it out,
       so other tools (spatialdata-io) read it too;
     - `situcraft-simulation.json` with every parameter;
     - `truth/`, a Xenium Ranger-shaped bundle of the same run with each cell's type and each
       molecule's true cell, place, origin and displacement.
   - **`situcraft simulate`:**
     - presets (`standard`, `small`), the seed, the field's size and a parameter file;
     - written beside the folder and renamed into place; interruptible.
   - **Labelled simulated:** the validator's support level `simulated` and an INFO finding instead
     of the version warning; the Data view's badge and notice; the inspector; the dataset list.
     The methods paragraph says that the data were simulated and implies no instrument.
   - Validation:
     - **Goodness of fit** (fixed seeds, the 0.001 level):
       - each gene's count per cell against its negative binomial (probability integral
         transform; a studentized variance test);
       - molecules within their cells and depths, and the nuclear share;
       - the displacement mixture and its share;
       - quality scores; background per tile, by gene and in place;
       - cell areas, types by domain, and isolated cells' pixel areas.

       Each test was shown to fail on a deliberately wrong parameter (research.md §3.4 gives the
       departures each catches).
     - **End to end:**
       - the small preset validates with no warning, every recomputed metric (16 of 16) agreeing;
       - its truth imports through the Xenium import with no finding, and the truth's
         transcripts match the bundle's row for row;
       - the instrument's cells and SituCraft's expansion compare with the truth.
     - **Reproducible:** the small preset's bundle has the same digest with one worker or many,
       on arm64 and amd64 (pinned).
     - **The command:** its summary, refusing an existing folder, unknown presets, parameters out
       of range and misspelled parameter names.
     - **In the app:** a standard bundle opened, checked, added and viewed (the stain, the
       instrument's outlines); the QC view reproduces 14 of 14 run metrics; the methods
       paragraph.
     - **Other readers** (maintainer check, `validation/reference/read_simulation.py`):
       spatialdata-io, tifffile and pyarrow read both presets as the simulation made them, with
       no warning. h5py reads the HDF5 writer's files.
   - **Reference numbers** (standard preset, research.md §3.4): 3,030 cells, 440,796
     transcripts, written in 3.3 s and 29 MB. Against the truth, the instrument's 5 µm expansion
     has a median IoU of 0.62 and puts 20% of Q20 transcripts in another cell than the true one;
     nuclei grown 2 µm have 0.71 and 31%; the nuclei alone have 0.27 and 68%.
   - Found by slice 1:
     - **Nucleus expansion indexed past `cells.parquet`** when a bundle's cell mask had more
       cells. It now refuses with the reason.
     - **A sum of areas fused into a multiply-add on arm64,** changing `metrics_summary.csv`'s
       last digit between processors. Products are rounded on their own.
     - **spatialdata-io needs more than SituCraft:** the HDF5 matrix, `cells.zarr.zip`'s
       polygon sets and cell summary, and a version it can parse. Without them it read the
       bundle as XOA 1. All three are written.
2. **The model (S2): done.**
   - **The equations** are written out in research.md §3.2 (notation, likelihood, priors, every
     conditional, their order, defaults).
   - **The model** (`internal/segment/refine`):
     - a voxel lattice and the labelling's state: cells, transcripts by voxel, prior cells,
       attributions;
     - the Poisson likelihood with per-gene background, the attributions integrated out or drawn;
     - the gamma–negative-binomial mixture of expression states;
     - the log-normal volume prior and the nuclear prior;
     - the change one voxel's move makes to the target, for slice 3's proposals.
   - **The updates:** a partially collapsed Gibbs sweep, with the components and shapes drawn
     with the cells' rates integrated out; the shapes by Chinese-restaurant-table augmentation;
     everything else conjugate. Every cell, gene and component draws from its own stream, in
     parallel, with the same result on any number of threads and on any processor.
   - **SituCraft's choices,** recorded in research.md:
     - a gamma prior on each component's rate parameter, with a gene-level scale, instead of the
       publication's Pólya–Gamma-sampled probability, so that every update is an exact draw;
     - the volume prior's normalizing constant (intractable) treated as fixed when its
       parameters and the components are updated, stated as the sampler's one approximation,
       with an `exact` mode (one fixed volume prior) for the exactness tests.
   - **New draws and tests:** the Chinese-restaurant-table and Dirichlet draws (`internal/random`,
     tested against the exact distributions); the beta distribution function and the two-sample
     Kolmogorov–Smirnov test (`internal/stats`).
   - Validation (research.md §3.2):
     - each conjugate update against its analytic posterior;
     - the components against independently computed probabilities;
     - the shapes' augmented chain against the numerically integrated posterior;
     - Geweke's joint distribution test of the whole sweep;
     - every voxel move's change against the recomputed target;
     - the attributions' shares;
     - type recovery from counts;
     - the same bits on any number of workers and on arm64 and amd64.

     Thirteen deliberate errors were each caught, among them the partially collapsed steps in
     the wrong order.
   - Found by slice 2:
     - **Geweke's test needs long chains for ordering errors:** with 8 sweeps per chain it missed
       the rates drawn before the shapes; with 40 the error accumulates and is caught.
3. **The sampler (S2, S8): done.**
   - **Moves** (`internal/segment/refine`; research.md §3.2, the sampler): Metropolis–Hastings
     with a label copied from a face neighbour or a background "bubble", the proposal ratio local
     and exact.
   - **Constraints:** no cell emptied; connectivity by a local simple-point test applied to both
     cells a move touches, so that a move is allowed exactly when its reverse is; a perimeter
     bound in each layer against the least digital perimeter of the area (factor 2).
   - **Parallel and deterministic:** a checkerboard of 40 µm blocks, shifted each sweep. A cell
     moves in a phase only in the one block near it, so a phase equals its blocks one after
     another. Each block draws from its own stream.
   - **Repositioning:** an independence sampler from the displacement mixture about each
     transcript's observed place.
   - **Multi-resolution:** stages refined by whole factors; the run starts at the coarsest stage
     that keeps every cell.
   - **The start:** the instrument's cells (masks/1) on the first lattice by majority, each seeded
     at its nucleus's centre; each transcript over a nucleus has that nucleus's cell as its prior.
   - **Posterior summaries:** each voxel's most frequent label and each transcript's most frequent
     cell with its probability, over the last sweeps; 2D footprints by each column's dominant
     label.
   - **Defaults from the validation:** one stage of 1 µm in one layer, 600 sweeps, the last 500
     summarized. The publication's schedule is kept as `PublishedStages`, to be revisited on real
     data in slice 6.
   - Validation (research.md §3.2):
     - **exact detailed balance** on every reachable state of two enumerable lattices (2D and
       3D), with no move allowed one way only and every state within the constraints;
     - **sampling:** chains from exact draws of π stay distributed as π, for single steps and for
       the parallel sweep;
     - every proposed move along a walk on a larger lattice balances against the recomputed
       target;
     - **repositioning** against its posterior;
     - **the same digest** on 1, 4 and 16 threads, on arm64 and amd64.

     Eight deliberate errors were each caught.
   - **Recovery:** on the standard simulated tissue, 85.9% of transcripts in their true cells
     against the instrument's 82.5%, and a median IoU of 0.663 against 0.622, in 24 s on ten
     cores. On the small tissue (the unit test) 86.4% and 0.668 against 83.4% and 0.634.
   - Found by slice 3:
     - **Attributions drawn before rates** gave every transcript to the background, for good; the
       start attributes by place.
     - **One sample is not the segmentation:** the posterior summary is.
     - **3D cells' footprints:** any layer's cell spread cells over gaps; the dominant label does
       not.
     - **Coarse starts** reach a higher likelihood on simulations but land further from the truth,
       so the default is one stage.
     - **A one-sided connectivity test** breaks detailed balance only where a voxel sits between
       two pieces of a cell; a test on a larger lattice was needed to catch it.
4. **Outputs (S9, S11): done.**
   - **Segmentation files** (`refine.WriteBundleOutputs`; FORMATS.md):
     - the six of every segmentation: the 2D footprint at the image's resolution, holes filled,
       outlines simplified to half a voxel; assignments; counts of gene transcripts of quality 20
       or more;
     - and three of its own: `probabilities.bin` (each transcript's posterior probability of its
       cell), `layers.bin` (each layer's outlines, with more than one) and `diagnostics.json`
       (every sweep's trace, convergence, the cells' limitations).

     The segmentation check validates the optional files when present.
   - **A job** (`api_refine.go`, `POST /api/local/{index}/refinement`):
     - options: sweeps, samples, seed, layers; the artifact is named by the instrument's
       segmentation and every option, so the same options give the same artifact;
     - resumable after the program stops; recorded with its lineage and the resolved parameters.
   - **Into the comparison workspace at once:** the page compares each refinement with the
     instrument's segmentation as soon as it is recorded.
   - **Convergence diagnostics:**
     - Geweke's diagnostic of the log-likelihood over the summarized sweeps is the verdict;
     - the split-half agreement of the assignments, the mean probability and the share below 0.5,
       and the voxels still moving each sweep are reported alongside.
   - **Limitations, per cell:** few transcripts (under 10), uncertain boundaries (under 75%), no
     nucleus, left out. They are shown in the Segmentation view's new card, with what every
     refinement shares, and listed with a difference's cells in the comparison workspace's
     inspector.
   - **The methods paragraph** describes a refined default segmentation: its start, schedule, seed
     and summary, citing Jones et al.
   - **Remote control:** `refine_segmentation` (28 actions).
   - Validation:
     - **Go tests:**
       - a run's outputs pass the segmentation check, load, and compare with the instrument's;
       - assignments and probabilities are the run's, row for row;
       - the limitations' thresholds, and Geweke's diagnostic on noise, trends and constants;
       - through the API on a simulated bundle: refused before the instrument's segmentation and
         with bad options, recorded with its files and lineage, the same artifact asked again,
         another for another seed.
     - **Node tests:** the convergence and limitation statements, per-cell flags, the methods
       sentence.
     - **In headless Chrome:** `refine_segmentation` on the Lung bundle, recorded with its
       probabilities and diagnostics, one comparison more, the card with its convergence and
       limitations.
     - **In the app:** a simulated tissue refined from the Segmentation view, compared at once, a
       flagged cell's limitation shown in the workspace's inspector.
   - **Reference numbers:** on the standard simulated tissue, 85.9% of transcripts in their true
     cells and a median IoU of 0.678, against the instrument's 82.5% and 0.622 (research.md §3.2).
   - Found by slice 4:
     - **Holes:** in one layer about 8% of a cell's voxels are holes in the posterior's summary.
       Filling enclosed background regions raised the median IoU from 0.663 to 0.678.
     - **A stability figure is not a convergence test.** The split-half agreement plateaus near
       95% because of the posterior's uncertainty. Geweke's diagnostic shows the log-likelihood
       still drifting slowly after 600 sweeps; longer runs barely change the result against the
       truth, and the page says so.
5. **The segmentation studio (S3, S4, S5, S7, E5): done.**
   - **The flow** (`web/ui/refine-studio.js`): Input (the transcripts' quality, the cells it
     starts from, what it covers) → Compute (threads; memory, disk and time against the
     computer's) → Model (a preset, every setting with what it does and costs) → Review (what will
     run, its problems and risks, its recipe) → Run (progress, the log, Stop) → Compare
     (convergence, limitations, the comparison with the instrument's). Every change asks for the
     spec's preflight (`POST /api/local/{index}/refinement/preflight`).
   - **Presets** (`refine.Presets`, version 1): Balanced, Conservative boundary, Flexible
     morphology, Low memory, ROI pilot (a region, run with two seeds) and Expert. Each says what it
     is for, changes and risks, with its measured accuracy; changing a setting but the seed makes
     the settings Expert.
   - **Regions and sections** (`refine.RunArea`): a region's cells refined on its lattice, the
     cells about it held in a 20 µm margin, the rest of the tissue the instrument's; separate
     sections by single linkage, each with a model of its own, one region in memory at a time.
   - **The preflight** (`refine.Check`): the transcripts modelled and those left out, the cells and
     nuclei, the regions, and estimates of memory (by phase: reading, sampling, writing), disk and
     time. It refuses a run past 85% of the computer's memory (`internal/sysinfo`) or the free
     disk, and warns past half of either.
   - **Failures classified** with what to do (`jobs.Classified`): the cells to start from, the
     bundle, memory, storage, the region, the settings; in the studio, the Jobs view and remote
     control.
   - **Recipes** (E5; `recipe.go`): a spec as readable JSON, saved and loaded in the studio, and
     run headless by `situcraft refine`. The terminal's folder imports back (the `situcraft`
     import format). App and terminal run one function, `refine.Refine`.
   - **Remote control:** `refinement_preflight`, and `refine_segmentation` with a preset, regions
     or sections, the quality, a second seed and threads (31 actions); `refine_segmentation` is now
     a long action.
   - **Less memory for every run:**
     - smaller tallies, the split-half agreement on every eighth transcript;
     - only the needed transcript columns decoded (`Transcripts.Only`);
     - the collector run between phases and the sampler's state released before writing;
     - a collection target of 50% (Low memory 20%).

     On the standard tissue the peak fell from 172 to 89 MB, the result unchanged.
   - Validation (research.md §3.2, the segmentation studio):
     - **Go tests:**
       - a region's run changes no pixel outside its lattice and none of a held cell; sections;
       - estimates within 30% of six measured runs;
       - presets well formed, and on the standard tissue each above the instrument, with
         Conservative's outlines more compact than Balanced's and Flexible's less, Low memory
         lighter, and the pilot's seeds agreeing (93.4%);
       - through the API: presets, preflights (before the instrument's segmentation, quality,
         a region outside), a pilot refined twice;
       - a recipe from the app run by `situcraft refine` gives the app's files byte for byte, and
         imports back; another image's folder is refused.
     - **Node tests:** presets' specs, Expert resolution, bounds, regions, recipes, units, failures,
       convergence not judged.
     - **In headless Chrome:** the six steps; a pilot's preflight and run with its seeds'
       agreement; a pilot without a region refused; a region without nuclei failing with what to
       do (86 checks).
     - **In the app:** a pilot on the standard simulated tissue set up step by step, run, and
       compared; a failure shown with its remedy in the studio and the Jobs view.
     - **On the Lung 2-FOV bundle,** a whole run in the terminal: memory 264 MB resident
       (estimated 251), disk 4.45 MB (4.0), 97 s (88).
   - **Reference numbers** (standard simulated tissue): the new default puts 86.3% of transcripts
     in their true cells with a median IoU of 0.712 (the instrument's 82.5% and 0.622).
   - Found by slice 5:
     - **The perimeter bound decides the shapes.** A tighter bound improved every simulated cell
       type, the elongated ones too, up to 1.3–1.4. The default became 1.6, the tightest that
       still admits a cell four times as long as wide at any angle.
     - **Low memory without repositioning** fell below the instrument (83.0% against 83.4%), so
       Low memory keeps repositioning. Coarser voxels alone hardly lowered the peak; the
       collector's target did.
     - **Conservative's cells are not smaller:** a tight bound stops gaps from opening as much as
       tendrils from growing, so its warning names elongated cells, not unassigned transcripts.
     - **The first cost model under-predicted small regions fourfold:** reading and writing the
       bundle cost more than sampling a small region. The model is now by phase.
     - **A short run was shown as settled:** Geweke's z is 0 when there are too few sweeps to
       judge. It is now recorded as not judged.
     - **`refine_segmentation` was not a long action** in slice 4, so a long run could time out
       remotely.
6. **Validation against reference outputs (S2): done.**
   - **The comparison** (`validation/segmentation_test.go`; research.md §3.2, against an
     independent reference): on both 2-FOV bundles and the standard simulated tissue,
     - SituCraft's refinement (Balanced, two seeds, as `situcraft refine` runs it);
     - Proseg 3.2.0 (its Xenium preset; one voxel layer as SituCraft's default, and its own
       defaults; each twice, as it has no seed);
     - the instrument's segmentation, and the truth.

     Each is imported and compared pair by pair with the comparison workspace's measures: cells,
     assigned fraction, spurious and mutually exclusive co-expression, matched cells' count
     correlation, IoU quantiles, transcripts changed, and recovery of the simulated truth.
   - **Reference numbers** in `validation/reference/segmentation/` (one file per data set): made by
     the maintainer from Proseg's outputs (`run_proseg.sh`, outputs kept out of the repository) and
     checked weekly. The weekly run refines each data set again and requires SituCraft's
     segmentation to be, byte for byte, the one the numbers were made from.
   - **Tolerances,** set before the comparison, each with its reason: met on Breast and the
     simulated tissue; three on Lung explained (assigned fraction, spurious co-expression, median
     IoU), with the evidence recorded.
   - **Budgets confirmed:** the Breast 2-FOV in 108 s and 282 MB on a laptop (Proseg: 122 s, 664
     MB).
   - **Reference numbers:**
     - on the simulated tissue, 85.0% of transcripts in their true cell against Proseg's 75.6% and
       the instrument's 82.2%, with a median IoU of 0.714 (Proseg 0.725, the instrument 0.622);
     - two SituCraft seeds differ on 6.9–14.0% of transcripts, two Proseg runs on 23.6–50.6%.
   - **Also:**
     - the comparison counts, of the transcripts A assigns, those B puts elsewhere
       (`changesQ20OfAssignedA`);
     - the SpatialData import matches transcripts at their observed places when a tool kept them;
     - the simulator writes Xenium's transcript columns and types (`nucleus_distance`,
       `codeword_index`, `overlaps_nucleus` as `uint8`).
   - Found by slice 6:
     - **The summary counted the background in cells:** each summarized sweep now draws every
       transcript's attribution, and a transcript counts in its cell only when attributed to it.
       Breast's assigned fraction fell from 98.80% to 97.03%, its co-expression from 1.94 to 1.83.
     - **The background is under-estimated:** on the simulated tissue SituCraft's accounts for
       0.68% of transcripts against the truth's 2.46%, from any start. It is the published model's
       behaviour when cells cover nearly every transcript; slice 7 takes it up (research.md §9).
     - **Spurious co-expression does not rank methods by accuracy:** Proseg's is below the truth's
       own on the simulated tissue, while it puts 9.4 points fewer transcripts in their true cells.
     - **Proseg's four-layer default lost 61% of Lung's transcripts to the background** (4% with one
       layer). SituCraft's one-layer default stands.
     - **The simulated bundles lacked two of Xenium's columns,** so Proseg could not read them.
7. **The background's calibration (S2): done, to the limit found.**
   - **The diagnosis,** on simulated truth (research.md §3.2, the background):
     - cells take the background's molecules where they lie: with every gene's rate held at the
       truth, cells still took 85% of those in the true gaps, and the summary left only 0.65% of
       the transcripts in no cell (the truth: 2.46%);
     - inside cells, ambient RNA looks like expression: one rate per gene gave the rarest genes
       three times their background and the commonest a seventh of theirs; tied to the tissue's
       mean expression, the ambient level collapsed to almost nothing;
     - tighter outlines and more gap proposals changed little.
   - **The background in two parts:** false decodes, shared by every codeword and measured by the
     negative controls, which no cell can take; and each gene's ambient RNA. The updates are
     exact (a binomial split, then conjugate gammas). They are tested against the exact posterior
     (a mixture of gammas over the splits) and by Geweke's joint test, each shown to catch
     deliberate errors, and give the same result on any number of threads and either processor.
   - **The simulator's background is of two kinds:** ambient RNA on the genes in proportion to
     their expression, and false decodes on every codeword. Checked against the real bundles'
     negative controls: false decodes are 0.04% of Lung's transcripts and 0.06% of Breast's. The
     standard tissue has 1.2%, so that both kinds are exercised.
   - **A benchmark** (`TestBackgroundOnSimulatedTruth`): five tissues, backgrounds of 0.5%, 2.5%
     and 9% and each kind alone. The false decodes are measured within a sixth of the truth. On
     the standard tissue the background found doubled (8.2% of it, against slice 6's 4.1%) with
     the same accuracy (86.3% in their true cell, median IoU 0.716).
   - **The diagnostics report the background** (`diagnostics.json`; the studio's Background): the
     shares its rates account for, by kind, the negative controls that measured the false decodes,
     and the share left in no cell.
   - **Reference numbers made again** on the new simulated tissue: SituCraft puts 84.7% of
     transcripts in their true cell (Proseg 75.2%, the instrument 82.2%), median IoU 0.719
     (0.727), and leaves 0.13% of the cells' transcripts in no cell (Proseg 1.42%), a new check.
     The presets were measured again.
   - **"Done when," as measured:**
     - *The background found at least Proseg's 1.53%:* not taken as a goal. On the truth only 18%
       of what Proseg leaves in no cell is background; matching its share would leave out about
       ten times as many of the cells' transcripts. SituCraft leaves 0.34%, 61% of it background.
     - *At least 85.0% in their true cell and a median IoU of 0.714:* the IoU holds (0.719). The
       new tissue puts 84.7% in their true cell, as slice 6's model does on it (85.0% on the old).
     - *Lung's differences from Proseg shrink:* they do not. Its controls put false decodes at
       0.04%, so Lung hardly changed; its two explanations were revised with what the truth shows.
     - *The limit reached is documented* (research.md §9): ambient RNA inside cells cannot be told
       from their expression, and even exact rates find about a sixth of the background.
   - Found by slice 7:
     - **A background share does not rank methods** (above): what is left out wrongly matters as
       much as what is left in.
     - **The old simulator's controls matched its genes' background by chance** (0.05 per
       codeword over 120 genes, against 6 per 100 µm² of ambient RNA), so calibrating by the
       controls alone looked right there. The physically consistent simulator showed otherwise.
     - **A low background is called less surely:** at 0.5%, 36% of what is left in no cell is
       background, 0.04% of all transcripts.
     - **The diagnostics lost the controls' count:** a refinement releases its model before
       writing, so the count read from it was 0. The browser suite caught it on Lung; the count
       now travels with the result, and the summary's test releases its results first.
8. **Segmentations on the tissue (V2, V5, V8): done.** Any segmentation is looked at where it
   lies, its outlines over the stains, as scientists check one.
   - **The Viewer draws any of the dataset's segmentations,** chosen in its Cells layer from the
     complete ones, its default first; a Viewer opened afresh draws the default. The legend names
     the segmentation and says how it was made: the instrument's methods, an expansion's distance,
     a refinement's preset, sweeps and seed, an import's tool. The instrument's comes from the
     dataset's own data; any other's from its artifact (`api_segview.go`): its outlines tiled as the
     comparison workspace's are, its cells table, a cell's detail, and its transcripts, each with
     its cell in that segmentation. The nuclei stay the dataset's.
   - **Fills:** every segmentation's counts and areas; a refinement's flagged limitations
     (unflagged cells left clear, so the flagged stand out), its boundary certainty and its
     assignment certainty (its transcripts' mean probability), on one scale from 0.5, a coin's toss,
     drawn brightest, to 1, every sample agreeing, drawn darkest.
   - **Transcripts by their assignment's probability,** close up, on the same scale, so the
     doubtful ones stand out; those assigned by place in grey.
   - **Each refined cell's boundary certainty is recorded** (`certainty.bin`): the mean share of the
     summarized sweeps its pixels were its, which the uncertain-boundary flag already measured.
   - **The inspector** describes the segmentation's cell: the instrument's cell it came from, its
     counts and top genes, its flagged limitations, its boundary and assignment certainty.
   - **Show on the tissue** on every segmentation in the Segmentation view's table and in the
     inspector, and on an expansion's and a refinement's result.
   - **Saved views and links** record the segmentation drawn, its cell and the transcripts'
     coloring; `show_tissue` takes a segmentation, a refinement's fills and `color_by`;
     `select_cell` selects a cell of the segmentation drawn.
   - **Validation:**
     - Go: `certainty.bin` written, read and checked, and consistent with the uncertain-boundary
       flags; the cells table, a cell's detail and the transcript tiles answer what the artifact's
       files hold.
     - In headless Chrome: each kind (an expansion, an import, a refinement, errors planted for
       practice) drawn as its files hold it, at 24 points well inside its cells: the cell found is
       the one its label raster holds, and the fill drawn is the color of that cell's value. The
       list and its default; a view of a refinement (its fill, its transcripts by probability, a
       cell) reopening exactly; `show_tissue` and `select_cell` in a segmentation, its transcripts
       carrying their probabilities and cells. Shown to catch another segmentation's outlines
       drawn in place of a refinement's, and a fill one cell off.
   - Found by slice 8:
     - **A segmentation showed under another's name while it was read:** the instrument's outlines
       were drawn, named as the instrument's, until the asked-for one arrived. The cells drawn
       before are now hidden while one is read, and the legend says which is being read.
     - **Points at cells' centroids could not tell a segmentation from the instrument's** when it
       numbers its cells as the instrument does. The suite now samples points well inside cells.
       An expansion's and an import's still agree with the instrument's almost everywhere, by
       design, so the refinement and the planted errors are the ones that tell.
9. **Larger tissues and release (S12, P6): done.**
   - **Patches** (research.md §3.2, large tissues): a region of more than twice
     `patch_transcripts` (2 million by default) modelled transcripts is cut into equal rectangles
     of about that many, refined one after another, each refining the cells within 40 µm of its
     rectangle and holding the rest, each sampler released when its run ends. They are stitched:
     each pixel and transcript from the patch whose rectangle holds it, each cell's certainty and
     flags from the patch holding its seed, every cell counted once. The patches depend on the
     data and settings alone, so a seed still gives the same bytes. The preflight (app, studio,
     `situcraft refine`, `refinement_preflight`) says how many; the progress names each patch;
     the diagnostics and the recorded parameters keep each patch.
   - **Memory guards in the Viewer:** the dataset's cells table counts each cell's genes a row of
     tiles at a time. A segmentation's assignments and probabilities are never read whole. They are streamed (`segment.Column`) to give each cell its mean probability and
     to write the transcripts' cells and probabilities by tile into the cache, in batches of about
     4 million, each sorted by row and picked out in one pass; each tile's are then read when
     drawn.
   - **Mosaics in the simulator** (research.md §3.4): `tiles_across` × `tiles_down` fields, each a
     tissue of its own from streams of its own, made one at a time and joined by streaming into
     one bundle (row groups, a mask chunk per tile, the stain by its tiles through a tiled TIFF
     writer). The first tile is the single field, byte for byte. The `large` preset: 7 × 6
     standard fields, 6.1 × 5.2 mm, 54.8 million transcripts.
   - **The scale test** (`validation/scale_test.go`, weekly in `data.yml`): the large tissue made,
     preflighted, refined (40 sweeps), its derived data built, the instrument's segmentation
     registered and the refinement imported, the Viewer opened on the dataset alone and then
     drawing the refinement's cells and every transcript tile; each step a process of its own,
     its time and peak memory against its budget; then all deleted. Measured on a 10-core laptop:
     - made in 621 s, at a peak of 3.1 GB, on 4.0 GB of disk;
     - refined in 25 patches in 338 s at 6.2 GB (5.1 to 6.2 GB over three runs; estimated 6.6
       GB, and 10.8 GB in one run);
     - its derived data built, the instrument's segmentation registered and the refinement
       imported in 128 s at 1.8 GB;
     - the Viewer with the dataset alone at 0.19 GB; drawing the refinement at 0.93 GB, each
       transcript tile in 12 ms at most.
   - **Examples:**
     - a third example, generated: the standard simulated tissue, made on this computer
       (**Make it**; `open_example` with `simulated`);
     - worked examples: refine the Breast example's segmentation and compare it with the
       instrument's (`refine-compare`); score the instrument's and SituCraft's segmentations of
       the simulated tissue against its truth (`score-against-truth`).
   - **Documentation:** research.md (large tissues, mosaics), FORMATS, Getting started, remote
     control, the README; screenshots of the studio and of a refinement on the tissue.
   - **Release 0.3.0:** a project saved by 0.3.0 kept in `validation/projects/` and reopened by
     every later release.
   - **"Done when," as measured:**
     - *Memory bounded for any tissue size:* the sampling's is, at one patch; the rest is not, and
       is measured instead: about 100 bytes a transcript and 2 a pixel, 5.1 to 6.2 GB at 55
       million transcripts against an estimated 10.8 GB in one run. A computer with 16 GB refines about
       120 million transcripts; streaming the rest is wave 10's.
     - *The Viewer reads by tile:* its peak drawing the large refinement fell from 1.8 GB to
       0.93 GB, and with the dataset alone from 1.65 GB to 0.19 GB.
     - *A scale test of ~50 million transcripts, created, measured and deleted weekly:* 54.8
       million.
   - Found by slice 9:
     - **The refinement's peak at scale is not the sampler's:** in patches, holding every
       transcript's inputs and every row's result for writing dominates (the preflight's writing
       phase).
     - **Reading a refinement's files whole held them two or three times over:** each column was
       decompressed into one buffer and converted into another, and the probabilities copied
       again as floats. Streaming holds none of them.
     - **The dataset's own cells table held a pair for every transcript in a cell** while it
       counted each cell's genes: the scale test found the Viewer with the dataset alone at 1.65
       GB, more than drawing the refinement. It now counts a row of tiles at a time, carrying the
       cells that span rows, and is checked cell by cell against transcripts.parquet.
     - **The summary counted patches as regions** ("held about 25 regions"): it now counts the
       regions and the patches apart.

## 0.4.0: cell identities (wave 4)

Cells given identities with their evidence: clusters, a map, labels from markers, imported labels
and an optional reference, each with its confidence, reviewed and kept as versions. Every analysis
is an artifact of a segmentation, so it can be made again on another and the two compared.

**Where the work runs.** The analyses that make a result (normalization, principal components,
neighbors, clusters, the map, scores, labels) run in the program as jobs, as segmentation does, and
their results are artifacts. The page draws and explores them. A cell table of a million cells
with a 5,000-gene panel does not fit a page's memory, and the same jobs then run headless
(`situcraft run`, wave 9) with nothing but the binary. CytoWeave's analyses are the starting point:
its nearest neighbors, Leiden and UMAP are carried to Go, with an origin comment, and made the
same on every processor.

### Wave 4

1. **Clusters (T2): done.**
   - **The method** (`internal/cluster`; research.md §5.1), scanpy's steps and defaults:
     - the cells: a segmentation's counts, those with at least 10 gene transcripts or those a saved
       QC filter keeps (checked by the filter's checksum);
     - each cell's counts scaled to the median total and log-transformed; each gene scaled,
       clipped at ±10;
     - exact principal components (50) of the sparse counts, without the dense matrix
       (`internal/linalg`: Householder reduction and the QL method);
     - each cell's 15 nearest neighbors (the cell included), exactly, weighted as UMAP's fuzzy
       graph; Leiden at a resolution, seeded (`internal/graph`, carried to Go from CytoWeave's
       analyses);
     - each cluster's markers: the Wilcoxon rank-sum test with ties corrected, the log₂ fold
       change and the shares expressing, labeled exploratory;
     - the same files on any number of threads, on arm64 and amd64 (pinned digests);
       `internal/dmath` gained fdlibm's complementary error function for the p-values.
   - **An artifact** of kind `clustering` (`docs/FORMATS.md`): the cells and their clusters, the
     components and scores, the neighbors, the markers; checked (every cell in a cluster numbered
     by size, the graph made again to the recorded modularity). Asking again answers it; an
     interrupted job resumes.
   - **The Cell types view** (`ui/mode-celltypes.js`): the segmentation, the cells and the
     parameters with their help; each clustering's numbers, the components' variances, the
     clusters with their sizes and leading markers, a cluster's markers; **Show on the tissue**,
     and the Viewer's **Fill by** lists each clustering of the segmentation drawn, with a legend.
     The sidebar's **Clusterings** and the inspector. Remote control: `cluster_cells`, and
     `show_tissue` fills by `clusters`.
   - **From testing 0.3.0:**
     - **A comparison can be removed from its project** (the inspector's **Remove**, or the
       Compare view's): the history records it; its files go to the library's trash once no
       saved project names it (`POST /api/artifacts/{id}/trash`); a comparison a decision cites
       is refused, with why. Undoing brings the record back; its files, gone, are made again by
       **Compare again**.
     - **A comparison made by another method version says so** (code
       `comparison.earlier-method`) and offers **Compare again** and **Remove it**; a comparison
       whose files are missing says that (`comparison.missing`) and offers the same.
   - Validation:
     - **Exact cases:** eigenvalues of the second-difference matrix, random, rank-deficient and
       repeated cases; the sparse covariance's components, scores and signs against the dense
       computation; neighbors against brute force with ties; the fuzzy weights by hand;
       modularity of two joined triangles (5/14); Leiden finding a ring of eight cliques and four
       planted groups; normalization, scaling and the rank-sum test by hand (equal ratios tie);
       Benjamini–Hochberg on a known case.
     - **Against scanpy 1.12.4, leidenalg 0.12.0, umap-learn 0.5.12 and scikit-learn 1.9.1**
       (`validation/reference/clustering/`, numbers in research.md §5.1), on both bundles and the
       standard simulated tissue: the same cells; the components' variances within 7 × 10⁻¹⁴
       and the same directions; the same neighbors; modularity on umap-learn's graph within
       3 × 10⁻⁸; Leiden's modularity within leidenalg's range over ten seeds (above it on Lung),
       its agreement with leidenalg as leidenalg's with itself; markers identical to the
       rank-sum test with exact ties, scanpy's fold changes within 10⁻⁶, the same first ten
       genes. Checked weekly against SituCraft's clustering made again, byte for byte.
     - **Known truth:** on the standard simulated tissue the clusters agree with the true cell
       types at an adjusted Rand index of 0.955 (scanpy's own pipeline 0.971).
     - **In the app:** the Lung cells clustered by remote control and drawn on the tissue, every
       cell in its cluster's color, the 78 not analyzed clear (viewer suite); both comparison
       fixes over HTTP on the Lung bundle and in the page; capture scenes `celltypes` and
       `clusters`, with no accessibility violation.
   - **Reference numbers:** Lung 29 clusters of 11,820 cells (modularity 0.6177), Breast 15 of
     7,163 (0.5982), the simulated tissue 5 of 2,925 (0.7166), each in about a second.
   - Found by slice 1:
     - **Ranking rounded values parts cells that express a gene equally:** two cells with the
       same count over total can get log-normalized values a rounding apart (log(1 + x) and
       log1p(x) part different pairs), which moved a marker's score by up to 0.017. The ranks
       now come from count over total, compared exactly; scanpy's scores, which rank the rounded
       values, differ from the exact test's by that much.
     - **An adjusted p-value can be a rounding below its p-value** (p·m/m): the check allows it.
     - **The instrument's graph-based clusters are not scanpy's:** on Lung neither SituCraft's
       clusters nor scanpy's agree with them (adjusted Rand index 0.044 and 0.048), though the
       largest clusters lead with the same genes; on Breast 0.431. Reported, not a tolerance.
2. **The map (T3) and exploring clusters: done.**
   - **The map** (`internal/embed`; research.md §5.2): UMAP of a clustering's graph as umap-learn
     lays it out (the curve fitted, edges pruned, a spectral start by Chebyshev-filtered subspace
     iteration, the stochastic gradient descent with negative sampling), scanpy's defaults, one
     thread and one seeded stream so a seed gives the same map anywhere; a graph in pieces starts
     from the principal components. Made as a job as soon as a clustering is; an artifact of kind
     `embedding`. Exact neighbor search in two dimensions by a grid (`internal/graph`).
   - **Its quality:** trustworthiness over 2,000 evenly spaced cells and each cell's neighborhood
     precision and recall, said in plain words under the map; the map colored by them.
   - **The map and the tissue** (`ui/cell-map.js`, `lib/scatter.js`): side by side, linked; colored
     by any clustering of the segmentation, a gene, the cells' transcripts or area, or the map's
     reliability; one selection (the lasso on either side, a cluster from the legend or the table),
     the others faint on both; a cell clicked on the map found on the tissue and back.
   - **Exploring:** each cluster's names (versions in the project; the same name merges clusters
     on the map, the tissue, the legends and the Viewer's fill); a dot plot of each cluster's two
     leading markers; the markers of any selection against the clustering's other cells; two
     clusterings compared by their cross-table and adjusted Rand index.
   - **The instrument's own analysis** (`internal/xenium` `ReadAnalysis`): its graph-based and
     k-means clusters registered as clusterings of its segmentation (its numbers kept, the markers
     SituCraft's), and its map with the first, judged against its own components.
   - Remote control: `show_map`, `name_clusters`, `instrument_clusters`.
   - Validation:
     - **Exact cases:** the curve's a and b as umap-learn fits them; a ring's spectral layout a
       circle in order; four groups kept apart; trustworthiness against its definition over every
       point, and 1 for a map that is its own space; the grid search against brute force with
       ties; the map's files checked; a cluster's markers equal to its cells' contrast with the
       rest; the expression and the dot plot by hand; names' versions and merges; the contingency
       and adjusted Rand index against scikit-learn's.
     - **Against umap-learn 0.5.12** on the same graph, five seeds (numbers in research.md §5.2):
       the curve within 4 × 10⁻⁷; SituCraft's trustworthiness recomputed from its places,
       identical; within umap-learn's spread (Lung 0.737 against 0.738–0.747, Breast 0.779 against
       0.778–0.782, the simulated tissue 0.840 against 0.838–0.841). Checked weekly, the map byte
       for byte.
     - **The instrument's analysis:** the graph-based clusters as `analysis.zarr.zip` holds them,
       every cell; registered, listed, and the instrument's map registered (Lung, over HTTP).
     - **In the app:** a cluster's cells and a polygon's cells selected exactly, on the map and the
       tissue (viewer suite); capture scene `cellmap`, with no accessibility violation.
   - Found by slice 2:
     - **Maps of 50 components of a 300-gene panel distort much** (trustworthiness 0.74 to 0.84,
       umap-learn's alike): a fixed threshold of unreliable cells (CytoWeave's 0.4, for cytometry)
       would mark most of them. The page says the trustworthiness and recall instead, with what a
       map can and cannot show.
     - **The default seed was Lung's lowest:** SituCraft's five seeds give 0.737 to 0.746, umap-
       learn's 0.738 to 0.747; the tolerance allows a hundredth below umap-learn's lowest.
     - **`analysis.zarr.zip` pads its index list** to every cell's count, so a cell is listed twice
       (the last group ends with a 0 for the cell in no cluster). The clusters are read from the CSV
       files.
     - **A selection on the tissue was hard to see** under a white highlight among colored cells:
       the cells not selected are drawn faint instead, as on the map.
3. **Labels from markers (T4): done.**
   - **Marker sets** (`internal/label`; `docs/FORMATS.md`): a hierarchy of labels on up to six
     levels (class, lineage, subtype) and states (proliferating), each with positive and negative
     markers and the DOIs of their sources; checked (names unique, parents listed first, a gene
     once), matched to the panel (genes it lacks refused, listed), named by a checksum of their
     canonical form.
     - **Starter sets** for the development bundles' panels (Human breast: 22 labels on three
       levels; Human lung: 26), from the breast and lung cell atlases (Wu et al. 2021, Kumar et al.
       2023, Janesick et al. 2023; Travaglini et al. 2020, Sikkema et al. 2023), cited, embedded;
       and the simulated tissue's types from its own markers.
     - **Edited in the app as versions** (the project's `marker-set` records): chosen, edited
       (labels, levels, states, markers; genes the panel lacks shown, and removed on request as a
       new version), read from a file and saved as one.
   - **Scores and labels** (research.md §5.3): each cell's score for each label as scanpy's
     `score_genes` computes it (Tirosh et al. 2016: control genes binned by mean expression), less
     its negative markers' where above 0; a label's lineage's markers left out of its controls.
     Labels from the top of the hierarchy down: the best of a label's children when it leads the
     next by the margin and scores the minimum, else the cell stays at the level above; confidence,
     the lead; states by their own threshold. Each cluster's labels at each level, and names for
     the clusters from them (a new version of the names).
   - **An artifact** of kind `labels`: each cell's scores (and their parts), label and lead at each
     level, confidence and states; each label's markers and control genes; each cluster's
     composition; the marker set by its checksum. Checked: every label the best of its candidates
     by the recorded scores, taken only by the margin and left only below it. Asking again answers
     it.
   - **In the app** (`ui/labels-card.js`): the Cell types view's labels — the marker set, its
     sources and parameters, the label sets of the clustering, each cluster's labels as bars, the
     labels with their markers and controls; the map and the tissue colored by the labels at a
     level, their confidence or a label's score, a label's cells selected from the legend; the
     Viewer's **Fill by** lists each label set's levels. The sidebar's **Label sets** and **Marker
     sets**, and the inspector. Remote control: `label_cells`; `show_map` and `show_tissue` color by
     `labels`.
   - Validation:
     - **Exact cases:** bins and controls as scanpy cuts and draws them (ties, rounding half to
       even), a score by hand, the assignment's ties, margins, single children, minimum and states;
       marker sets checked; a planted tissue's classes and types labeled, the files checked and
       pinned; the expression's means by hand; the project's versions, the editor's operations,
       names from labels and the cells' binary read.
     - **Against scanpy 1.12.4** (`validation/reference/labels/`): every label's control genes
       scanpy's on both bundles and the standard simulated tissue (87 lists), each cell's score
       within 3 × 10⁻⁷. Checked weekly against SituCraft's labels made again, byte for byte.
     - **Known truth:** on the standard simulated tissue 97.2% of the cells take their true type,
       1.5% another.
     - **In the app:** the Lung cells labeled by remote control and drawn on the tissue, each cell
       in its label's color, the unlabeled grey, the 78 not analyzed clear; a cluster selected on
       the labels' colors exactly its cells (viewer suite); capture scene `labels`, with no
       accessibility violation.
   - Found by slice 3:
     - **A tumor's tissue is not the atlas's:** in the Breast region tumor cells express LPL and
       PDGFRA, and FOXA1, GATA3 and ESR1 are nearly absent; in the Lung region the tumor expresses
       COL5A2 and PROX1. The starter sets leave them out of the labels they would mislead, and
       stromal transcripts reaching immune cells are answered by immune markers counted against
       Stromal.
     - **On a targeted panel, controls binned by expression cancel a dominant type's markers:** the
       top bins hold the tumor's own genes, so its epithelial score is near 0, as scanpy's is. On
       Lung about half the tumor cells take no label, on Breast 17% to 28%; reported, the label set
       saying which.
     - **The margin is over the next label, the minimum a separate gate:** a lead over the larger of
       the two left a dominant type unlabeled whenever its score was near 0.
     - **States need their own threshold:** at the margin one transcript of four proliferation
       genes passed (36% of Breast's cells).
     - **Averaging scores over each cell's neighbors in expression** labels more of Breast but
       loses every rare type and labels less of Lung: not taken.
4. **Imported labels (T1): done.**
   - **Read** (`internal/label`'s `table.go`; research.md §5.4): CSV and TSV (gzipped or not),
     Parquet, an AnnData object's `obs` in Zarr (format 2 or 3, a folder or a zip) or a SpatialData
     store's table, Xenium Explorer's cell groups (`cell_id`, `group`, a group's color once), and
     the instrument's graph-based and k-means clusters; each table whole, its columns described,
     the ids', labels', confidence's and colors' columns suggested by their usual names.
   - **Matched** to a segmentation's cells by id (`imported.go`): a table naming cells the
     segmentation does not have refused with their count and the first few (and the dataset's
     segmentation whose cells they are named, when one is); a cell named twice refused; cells the
     table does not name, and empty labels, counted without a label.
   - **Kept** as a label set (kind `labels`) with its source (format, file, SHA-256, columns, rows)
     in place of a marker set: every cell's label and confidence, the labels' colors where given.
     Each cluster's labels are counted in the page for the clustering shown, so one import serves
     every clustering of its segmentation; the map, the tissue, the Viewer and names from labels
     treat it as any label set.
   - **In the app:** the Cell types view's **Labels made elsewhere**: a table read, its columns
     chosen, how its ids match said before importing; or the instrument's clusters. Remote control:
     `import_labels`.
   - Validation:
     - **Files written by pandas, pyarrow and anndata** (`internal/label/testdata`: CSV, gzipped TSV,
       Parquet with dictionary-encoded text and float32 nulls, AnnData Zarr formats 3 and 2 with a
       categorical column and missing values, a SpatialData table, Xenium Explorer's groups) read
       exactly; tables naming another segmentation's cells, or a cell twice, refused.
     - **The development bundles:** each bundle's graph-based clusters, written as a table, imported
       for its instrument segmentation, every cell its cluster; the Lung table refused for the
       Breast cells (checked weekly).
     - **In the app:** a table of three Lung cells imported by remote control, those cells and no
       other labeled; another dataset's table refused; the instrument's clusters imported (viewer
       suite).
5. **Label versions, evidence and review (T6, T7, T8): done** (segmentation-sensitive cells with
   slice 7).
   - **Versions** (`lib/curation.js`, `ui/label-sources.js`; research.md §5.5): predicted (from
     markers, imported), curated (a base set and a reviewer's edits — cells or clusters relabeled,
     labels accepted — appended with reason, who and when) and consensus (chosen sets, their labels
     mapped to common names, by unanimity, majority or order), each on the map, the tissue and the
     Viewer; any two side by side in a cross-table with their agreement.
   - **The evidence card** of a cell: its label in every version; from markers, at each level the
     label taken and the next with their markers' transcripts in the cell against their means and
     controls, and why not the label most of its neighbors carry; its neighbors' labels in the
     tissue and in expression; its transcripts, area and segmentation's flags; its reviews. A cell
     chosen on the map or the tissue, or in the queue (`internal/label/evidence.go`).
   - **The review queue** (`internal/label/review.go`): cells whose label at most a share of their
     nearest labeled cells carry (most carrying another), and of low confidence, ranked, the
     reviewed left out; accepted or relabeled with a reason, the next shown.
   - Remote control: `review_labels`, `label_evidence`, `curate_labels`, `consensus_labels`;
     `show_map` and `show_tissue` color by curated and consensus sets.
   - Validation:
     - **Consensus rules on built sets**, edits applied in order, the cross-table by hand
       (`lib/curation.test.mjs`); the queue and agreement by hand, a cell's evidence equal to its
       recorded scores (`internal/label`).
     - **Planted mislabels** (3% of the cells given another class): the queue finds 75% of them on
       the standard simulated tissue (70% in its first five times as many cells as were planted;
       its domains mix their types), 95% on the Breast tissue (checked weekly).
     - **In the app:** the Lung labels' queue by remote control; a cell relabeled with its reason
       drawn so and no other changed; a consensus (viewer suite).
   - Found by slice 5:
     - **On a tissue whose domains mix their types, place finds few mislabels:** the simulated
       tissue's queue flags a quarter of its cells. Ranking by the share of neighbors agreeing finds
       planted mislabels earlier on Breast than a likelihood against each label's usual
       neighborhood, and is said in plain words.
6. **Mapping to a reference (T5): done.**
   - **Read** (`internal/label`'s `reference.go`; research.md §5.6): an AnnData object in Zarr (a
     folder or a zip; its labels in an `obs` column, its genes' names in `var`, its counts or
     log-normalized values in `layers/counts`, `raw/X` or `X`, each described without reading it
     whole), or a table of profiles (CSV, TSV or Parquet; a row per label or per gene); the columns
     suggested by their usual names. Restricted to the panel's genes, the genes shared and the
     panel's genes it lacks reported; each label's profile its cells' mean log-normalized
     expression.
   - **Mapped:** each cell's Spearman correlation with each label's profile over, for each pair of
     labels, the genes most higher in one than the other (the published number per pair, a
     parameter); fine-tuned on the leading labels' own genes until one is left (Aran et al. 2019);
     the label taken by a margin, its lead the cell's confidence.
   - **Kept** as a label set (kind `labels`) with the reference as compared (`reference.json`), every
     cell's correlations and rounds; the map, the tissue, the Viewer, names from labels, versions and
     the review treat it as any label set. The evidence card shows a cell's rounds and the genes
     setting its label and the next apart.
   - **h5ad is not read:** anndata writes the same object as Zarr in one line (`write_zarr`), and an
     HDF5 reader is left to the parking lot. `situcraft simulate --reference N` writes a single-cell
     reference of a simulated tissue's types beside it.
   - **In the app:** the Cell types view's **Labels from a reference**: a reference read against the
     panel (its labels, the genes shared and compared, what its values are), its columns and the
     parameters chosen. Remote control: `reference_labels`; `label_evidence` answers a reference's
     rounds.
   - Validation:
     - **Exact cases:** Spearman with ties and a cell's rounds by hand; the mapping against a plain
       one over random references; profiles by hand from AnnData counts and from tables in both
       orientations; a typed tissue mapped, the files checked and pinned.
     - **Against SingleR 2.14.2** (`validation/reference/references/`, maintainer-run, committed):
       given the same profiles of the Human Primary Cell Atlas's 36 main labels, every cell's
       first-round correlations within 3 × 10⁻⁸ and the same label on 100% (Lung), 99.97% (Breast)
       and 100% (simulated) of the cells; SingleR at its per-sample defaults agrees on 72.0%,
       62.5% and 99.3%. Checked weekly against SituCraft's labels made again, byte for byte.
     - **Known truth:** on the standard simulated tissue mapped to a reference of its types, 98.1% of
       the cells take their true type (SingleR at its defaults 98.6%).
     - **In the app:** the Lung cells mapped by remote control to a table of four classes' profiles,
       each drawn in its label's color, a cell's evidence its recorded correlations; a reference of
       none of the panel's genes refused (viewer suite); capture scene `reference`, with no
       accessibility violation.
   - Found by slice 6:
     - **On a targeted panel the published number of genes per pair covers the panel**, so
       fine-tuning compares nearly the same genes again; fewer per pair label a few more simulated
       cells right (98.9% at 20 or 5). The published number is kept and is a parameter.
     - **A reference must be of the tissue:** an atlas of primary cells labels tumor cells with the
       nearest profiles it has (Breast's embryonic stem and neuroepithelial cells, Lung's
       hepatocytes), close in correlation, which the evidence and the review show.
7. **Labels across segmentations (C6, T8): done.**
   - **Made again in one step** (`lib/impact.js`, `ui/remake.js`; research.md §5.7): a clustering of
     SituCraft's, its map and its label sets from markers or a reference made again on another
     segmentation with the same parameters (a QC filter's cells by the rule; imported sets not made
     again), from the label sets' card or the comparison's Labels tab.
   - **Read through a comparison** (`internal/compare`'s `impact.go`, `api_impact.go`): each
     label's cells under each segmentation; the matched cells labeled alike and every pair of
     labels; what split cells' pieces and merged cells carry; cells without a counterpart by label;
     marker mixing by label (the share of each label's cells co-expressing exclusive markers, and
     the MECR among them); the differences whose labels change.
   - **The comparison workspace's Labels tab:** a label set of each side (two made the same way by
     default, at their first level), the impact in words and tables, each difference whose labels
     change opening where it lies, and the tissue colored by each side's labels.
   - **Segmentation-sensitive cells** join the review queue: those whose counterparts under another
     segmentation carry another label, with what they carry, against a label set chosen.
   - Remote control: `repeat_on_segmentation`, `label_impact`; `review_labels` takes `against`.
   - Validation:
     - **A built fixture by hand:** every class of difference, labels named in different orders,
       cells not covered; each measure, line and MECR by hand; the queue's third reason.
     - **Known truth** (weekly): on the standard simulated tissue labeled from its types' markers,
       the matched cells take their true type in 100% (the truth), 99.86% (the refinement) and
       98.33% (the instrument's segmentation); the types' shares within 0.5% and 2.2% of the truth's;
       marker mixing (MECR) 0.063, 0.090 and 0.221.
     - **In the app:** the Lung labels made again on the 5 µm expansion by remote control, 99.9% of
       matched cells alike, the tissue colored by them, the queue's sensitive cells the impact's
       (viewer suite); capture scene `impact`, with no accessibility violation.
   - Found by slice 7:
     - **A segmentation's errors reach the labels mostly through cells of two kinds:** the
       refinement's remaining errors on the simulated tissue are true cells merged, a third of them
       of two types.
     - **Finer labels are more sensitive:** on Breast, the instrument's segmentation and a 5 µm
       expansion agree on 96.9% of matched cells' classes and 79.7% of their subtypes; the tab opens
       at the first level.
     - **Marker mixing is read in its rate, not in cells co-expressing any pair,** which saturates
       where markers are not absolutely exclusive (98.9% of the simulated instrument's cells).
8. **Scale: done.**
   - **Nearest neighbors above 50,000 cells by NN-Descent** (Dong, Moses & Li 2011; research.md
     §5.8): the same table for a seed on any number of workers and processors (each round's samples
     from each cell's own stream, its comparisons made a block at a time against the table as it
     stands and applied in order); 3k searched for k; its recall against exact search measured over
     1,000 cells, recorded in the clustering and said in the page. The map's wider neighborhoods the
     same way, started from the clustering's.
   - **The map's optimization over every core above 50,000 cells:** each epoch's edges in batches,
     computed in parallel from the places before the batch and applied in order, the negative
     samples from a hash; the same map for any number of workers.
   - **Principal components above 1,000 genes by the Lanczos method** with full
     reorthogonalization, the covariance formed in place.
   - **Imports a band of rows at a time:** an imported segmentation's raster made and encoded a
     band of rows at a time, and a Xenium bundle's mask read a row of its chunks at a time (the
     instrument's segmentation's files are made so on first use).
   - Below the limits, every method and file is as it was.
   - Validation:
     - **Exact cases:** NN-Descent against exact search, the same for any number of workers; the
       Lanczos method against the full decomposition; the batched map the same for any number of
       workers and as faithful as one thread's; digests pinned on arm64 and amd64.
     - **The bundles' neighbors searched approximately** (weekly): 99.91% (Lung) and 99.95% (Breast)
       of the exact found, the recall recorded within 0.0004; over five seeds, Leiden's clusters on
       the approximate graph agree with the exact graph's (ARI 0.857, 0.704) more than its runs on
       the exact graph with one another (0.791, 0.653).
     - **Large panels** (weekly): 2,000 genes' components against the full decomposition, variances
       within 7 × 10⁻¹⁴, the subspace the same; 0.7 s against 15 s; 5,000 genes in 3.6 s.
     - **The large simulated tissue typed** (the weekly scale test): 383,158 of its cells clustered,
       mapped and labeled in 186 s on ten cores at a peak of 3.9 GB (the instrument's segmentation's
       files made on first use within it): the neighbors by NN-Descent in 34 s, 95.6% of the exact
       found (the simulated types' cells are alike within a type, as noise is, where nearest
       neighbors are least determined; 99.9% on the bundles); 9 clusters; the map laid out in 33 s,
       its trustworthiness 0.861, its wider neighborhoods 97.1% of the exact; 378,743 cells labeled
       from the simulated types' markers, the types' shares within a total variation distance of
       0.015 of the truth's; the map of every cell shown in the page in 20 s and colored again in
       40 ms, SituCraft serving it in 1.9 GB.
   - Found by slice 8:
     - **A wider search, not more rounds, finds the neighbors:** NN-Descent searching k found 91% to
       96% of the exact neighbors on the bundles, searching 3k 99.9%, at about four times the time
       (under a second for Lung's 11,820 cells; 27 s for 400,000 points on ten cores).
     - **The covariance's noise components are not determined singly:** on a simulated panel of
       2,000 genes most of the 50 components' variances lie within a thousandth of the largest of
       one another; the Lanczos method and the full decomposition agree on their subspace exactly,
       and on the directions that stand apart.
     - **The instrument's segmentation held the slide's mask twice when first made:** the scale
       test's typing peaked at 6.4 GB, two copies of a 28,672 × 24,576 mask (2.8 GB each) made
       when the clustering first asked for the instrument's segmentation files. Imports now make
       the raster a band of rows at a time.
9. **Examples, documentation, release 0.4.0: done.**
   - **Worked examples** (`web/lib/tours.js`), each asking its question and saying what to expect,
     the card's button doing each step's job:
     - *Cluster and label the Lung cells* (`cluster-label`): 29 clusters of 11,820 cells, the map
       (trustworthiness 0.74), cluster 13 by its markers (VWF, CD34, KDR), labels from the panel's
       starter set (47% a class, 12% a lineage, 1% a subtype), why the tumor's cells stay
       unlabeled (57% of the largest cluster), the review queue (2,006 of 5,555 labeled cells),
       the labels in the Viewer;
     - *Labels against the truth* (`labels-against-truth`, the simulated example): the truth's
       types imported as labels; the instrument's cells labeled from the types' markers, 98.3% of
       the matched cells labeled taking their true type (marker mixing 0.22 against the true
       cells' 0.06); the refinement's labels, 99.9% (mixing 0.09);
     - *Labels under two segmentations* (`labels-two-segmentations`, Breast): the instrument's
       cells and a 5 µm nucleus expansion labeled the same way and read through their comparison
       (of 6,512 cells matched one to one, 96.9% of those labeled under both keep their class; 223
       split and 106 merged cells; mixing 0.047 and 0.051), the 688 cells whose label changes in
       the review queue.
   - **What a step can do:** cluster a segmentation's cells, label its clustering from a starter
     set, import the truth's types, make a clustering and its labels again on another
     segmentation; show a clustering in the Cell types view (the map's colors, clusters selected,
     a section in view, the review queue made for its labels); open the Compare view's Labels tab;
     fill the Viewer's cells by a clustering or its labels. `tour` answers what a Cell types step
     shows and what the Labels tab reads.
   - **Documentation:** Getting started (the worked examples), the README, remote control,
     FORMATS; the screenshots taken again, one added of the labels against the simulated truth.
   - **Release 0.4.0:** a project saved by 0.4.0 kept in `validation/projects/` and reopened by
     every later release, each clustering, map and label set from markers it records made again
     with the same summary.
   - **Validated** in the viewer suite: each worked example run step by step, its numbers those its
     card states.
   - Found by slice 9:
     - **The review queue chose the segmentation to read against once:** opened before the labels
       were made again on another segmentation, it never took them up, and listed no
       segmentation-sensitive cell until one was chosen by hand. It now follows the latest until
       the user chooses.
     - **A lineage fill leaves most of a tumor section grey:** on Lung, 12% of the cells take a
       lineage; the example fills the cells by class (47%) and names the lineage level.

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
- The refinement's inputs and results streamed by patch, so that its memory no longer grows with
  the tissue: in 0.3.0 the sampling holds one patch, but the run still holds about 100 bytes a
  transcript (wave 3, slice 9).
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
- Reading `.h5ad` (references and imported labels) with an HDF5 reader of SituCraft's own, if
  converting with anndata's `write_zarr` proves a burden.

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
