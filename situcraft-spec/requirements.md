# SituCraft requirements

What SituCraft must do, and the wave that delivers each requirement.
- `research.md` explains the methods, formats and data behind them.
- `design.md` explains how they are met.
- `roadmap.md` orders them into waves; wave *N* is released as 0.*N*.0.

Status: **planned (wave N)** until built. Then **done**, **partial** (the gap noted) or
**parked**, with the validation that shows it.

## Users

- **Spatial biologists** who need to know whether a run is usable, see their tissue, choose a
  segmentation, identify cells and find niches, without writing code.
- **Computational biologists** who need deterministic headless runs, open exports, documented
  formats and programmatic access.
- **Pathologists** who review cells, labels and regions against H&E, with their decisions
  recorded.
- **Core-facility analysts** who process many runs consistently and produce standard QC records.
- **Principal investigators** who compare samples and communicate results with their provenance.
- **Method developers** who compare segmentation and annotation methods on identical regions.
- **AI agents** acting for any of them, through a typed and auditable interface.

## Distribution and platform

| # | Requirement | Status |
| --- | --- | --- |
| P1 | One self-contained program for macOS, Linux and Windows (x64 and ARM64); one-line install with checksum verification; no administrator rights, account, Python, R, containers or GPU | done (wave 1, slice 1: six binaries from the release workflow, installers adapted from MaveScape) |
| P2 | Opens as a desktop window (a Chromium-family browser in app mode with its own profile), else the default browser | done (wave 1, slice 1) |
| P3 | Data never leave the computer; loopback only; no telemetry; the only outbound requests are example-data downloads the user starts | done (wave 1, slices 1 and 7: the only outbound request is an example's download, from its publisher, when the user asks) |
| P4 | Apache-2.0; no third-party runtime dependencies | done |
| P5 | A second launch hands its paths to the running SituCraft | done (wave 1, slice 1) |
| P6 | The 2-FOV development bundles open, index and segment on a laptop with 16 GB of memory; memory needs for larger data are estimated before work starts | done (wave 1: both bundles open and index in seconds; wave 3, slices 5 and 9: refined in under 300 MB, every refinement's memory, disk and time estimated before it runs and refused past the computer's; a simulated tissue of 55 million transcripts made, refined, indexed and drawn in at most about 6 GB, measured weekly) |

## Data and import

| # | Requirement | Status |
| --- | --- | --- |
| D1 | Open a Xenium output bundle as a directory or a zip | done (wave 1, slice 3: a folder, its experiment.xenium, or a zip with the bundle at its root or in a folder; links followed) |
| D2 | Detect the XOA version and which files are present; say what is supported | done (wave 1, slice 3: XOA 2.x supported, other versions best-effort and said so; every file by role) |
| D3 | Validate a bundle: required and optional files, schemas, matrix dimensions, identifiers consistent across files, coordinate ranges, geometry validity, image metadata, transforms, free disk. Fatal errors, scientific warnings and informational differences kept apart, each linked to files, regions and a remedy | done (wave 1, slice 3: 13 broken bundles raise exactly their findings; both development bundles reproduce 12 recorded metrics) |
| D4 | Never modify a source bundle | done (wave 1, slice 3: read-only sources; every file's SHA-256 and time checked unchanged by validation) |
| D5 | Derived data (tile pyramids, transcript index, geometry tiles) built as resumable, atomic, checksummed cache entries; an unchanged project reopens without re-indexing | done (wave 1, slice 4) |
| D6 | Readers for Parquet (Zstd, Snappy, gzip, uncompressed), Zarr v2/v3 (Blosc LZ4/Zstd, zip and directory stores), BigTIFF/OME-TIFF (JPEG 2000, JPEG, deflate, LZW), Matrix Market, CSV(.gz), GeoJSON | partial (wave 1, slice 2: Parquet, Zarr 2 and 3, TIFF/BigTIFF/OME-TIFF with JPEG 2000, deflate, LZW and PackBits, all identical to independent readers on both bundles; JPEG for H&E in wave 5; Matrix Market, CSV and GeoJSON with the slices that need them; wave 2, slice 4: CSV and TSV (gzipped too), GeoJSON and WKB polygons, AnnData in Zarr, and Zarr's sharding and text, read as zarr-python, anndata and spatialdata write them) |
| D7 | Projects: a manifest with stable identifiers, versions, datasets and artifacts; portable (relative paths) and linked (institutional storage) modes | partial (wave 1, slices 3 and 7: datasets recorded by place, run identifiers and fingerprint; reconnected on reopening, a moved bundle located again; project archives (`.scz`) record each bundle relative to the archive, so a folder of both can be moved; a project saved by 0.1.0 reopens exactly in the viewer suite. Linked institutional storage is to come) |
| D8 | Artifact lineage: parents, producing job, parameters, method and software versions, checksums, state, role | done (wave 2, slice 2: `internal/artifacts`, each artifact's manifest with its lineage, every file's SHA-256, its check's findings and its state; the role (a dataset's default segmentation) by recorded decisions in the project) |
| D9 | Example data downloaded on request from their source, verified by checksum, attributed | done (wave 1, slice 7: the two development bundles from 10x Genomics, CC BY 4.0, downloaded only when asked, the archive checked by size, MD5 and SHA-256 and every file by size and SHA-256, the archive then deleted; each with its source, license and a guided tour; cited in the methods paragraph) |
| D10 | Several datasets (samples) in one project | planned (wave 7) |
| D11 | CosMx and MERSCOPE outputs | planned (wave 10) |

## Viewer

| # | Requirement | Status |
| --- | --- | --- |
| V1 | Multiscale, multichannel 16-bit images rendered progressively, with per-channel contrast, color and visibility; never a whole slide decoded in the browser | done (wave 1, slice 5) |
| V2 | Layers: morphology channels, transcripts (density and points), cell and nucleus boundaries, cell fills by any attribute, labels, ROIs, registered H&E/IF | partial (wave 1, slice 5: channels, transcripts, cell and nucleus boundaries, fills by the cells table's attributes; wave 3, slice 8: any of a dataset's segmentations drawn, its default first, its cells filled by their counts and areas and a refinement's by its flagged limitations, boundary certainty and assignment certainty, transcripts colored by their assignment's probability; labels in wave 4, ROIs and H&E in wave 5) |
| V3 | Scale-dependent representation: density rasters for whole tissue, simplified polygons at intermediate zoom, exact boundaries and molecules at cellular zoom | done (wave 1, slice 5) |
| V4 | Genes: search; single-gene points; several genes in distinct colors; gene-set scores; per-cell expression fills | partial (wave 1, slice 5: search, points and density of up to 16 genes in distinct colors; gene-set scores and expression fills in wave 4) |
| V5 | Cell inspector: identifiers, geometry, counts and top genes, segmentation, labels, neighbors, provenance, review history | partial (wave 1, slice 5: identifiers, centroid, areas and outline, counts recorded and counted, top genes, segmentation method; wave 3, slice 8: any segmentation's cell, with the instrument's cell it came from, its counts and top genes, its flagged limitations, boundary certainty and its transcripts' mean probability; the rest in waves 4 and 5) |
| V6 | Linked selection across the tissue, plots, tables and inspector | partial (wave 1, slice 6: cells chosen in a QC plot show in the tissue, here and in the Viewer, and in the inspector; tables in wave 4) |
| V7 | Two segmentations compared by overlay, synchronized split view, flicker and disagreement highlighting | done (wave 2, slice 6: the Compare workspace draws A and B side by side with one camera, overlaid in two colours, or in turns; cells filled by the class of their difference; a map of disagreeing pixels by region) |
| V8 | Saved views (dataset, coordinate system, viewport, layers, colors, filters, genes, segmentation, labels, ROIs) that reopen exactly | partial (wave 1, slice 7: dataset, place and zoom in µm, channels, genes, layers, cell fill, selected cell, QC map and filter, by name so they survive rebuilt derived data; a link reopens each; three views of a 0.1.0 project reopen exactly in the viewer suite; wave 3, slice 8: the segmentation drawn, its cell and the transcripts' coloring by probability, a view of a refinement reopening exactly; labels and ROIs as their waves build them) |
| V9 | Scale bar, coordinate readout (µm), minimap, legends; an "explain this color" link from every encoding to its artifact and method | partial (wave 1, slice 5: scale bar, readout, minimap, the fill's legend; the links in wave 4) |
| V10 | Pan and zoom near 30 frames per second; visible-region cell queries within 500 ms once warm | done (wave 1, slice 5: 60 frames per second on an Apple M4, the display's rate; region queries in milliseconds; measured in headless Chrome) |

## Quality control

| # | Requirement | Status |
| --- | --- | --- |
| Q1 | Run metrics from the bundle, explained | done (wave 1, slice 6: every recorded metric explained; 20 recomputed from the data and shown beside it, 4 shown as recorded with why) |
| Q2 | Spatial QC maps: transcripts per cell, negative-control density, unassigned fraction, cell and nuclear area, segmentation method, Q-score | done (wave 1, slice 6) |
| Q3 | Distributions (histograms, box plots, scatter plots) with brushing linked to the tissue | done (wave 1, slice 6) |
| Q4 | Non-destructive cell filters saved as named versions; their effects compared | done (wave 1, slice 6) |
| Q5 | Metrics compared across samples, outliers flagged, no opaque pass/fail thresholds | planned (wave 7) |

## Segmentation

| # | Requirement | Status |
| --- | --- | --- |
| S1 | Nucleus expansion to a chosen distance, neighbor-bounded, with transcript assignment and counts | done (wave 2, slice 3: each nucleus grown by exact Euclidean distance, the nearest within the distance winning; transcripts by pixel, counts of gene transcripts of quality 20 or more, outlines; the Lung bundle's 5 µm expansion reproduced, 98.06% of pixels and 98.94% of transcripts in the same cells, every difference explained) |
| S2 | Transcript-informed segmentation built in (after Jones et al. 2025): voxel lattice, expression mixture, background, morphology and connectivity constraints, repositioning, multi-resolution sampling, nuclei as the prior | done (wave 3, slice 1: simulated tissues with known truth, written as Xenium bundles labelled simulated, their distributions validated; slice 2: the model, its likelihood, priors and exact conditional updates, validated against analytic posteriors and by Geweke's joint test; slice 3: the sampler, exact in detailed balance, recovering simulated cells better than nucleus expansion; slice 4: its outputs as segmentation artifacts, compared with the instrument's at once, with probabilities, diagnostics and limitations; slice 5: the studio, regions and sections, presets measured against simulated truth; slice 6: compared with Proseg 3.2.0 as an independent reference on both 2-FOV bundles and the simulated tissue, every tolerance met or explained, 85.0% of simulated transcripts in their true cell against its 75.6%; slice 7: the background in two parts, its false decodes measured by the negative controls, benchmarked at several levels and kinds against simulated truth, a tenth of Proseg's cells' transcripts left in no cell, the limit for ambient RNA documented; slice 9: large tissues in patches, S12) |
| S3 | Presets (Balanced, Conservative boundary, Flexible morphology, Low memory, ROI pilot, Expert) showing their effective settings; plain-language help on what each setting does and costs; constraint checks (e.g. burn-in voxel size an integer multiple of the final size) | done (wave 3, slice 5: the six presets, each with what it is for, changes and risks and its accuracy measured on a simulated tissue; every setting's help and cost; the settings checked, each stage's voxels whole multiples of the next's, as the studio shows them) |
| S4 | Preflight: transcripts, Q-score policy, prior assignments, regions, memory and disk estimates, threads | done (wave 3, slice 5: the transcripts modelled and left out at the chosen quality, the instrument's cells and nuclei it starts from, the regions or sections, memory, disk and time against the computer's, the threads; estimates within 30% of measured simulated runs and 10% on the Lung bundle) |
| S5 | Whole tissue or chosen regions; unrelated sections segmented separately | done (wave 3, slice 5: whole tissue, chosen regions (the cells about them held, the rest the instrument's), or separate sections, each with a model of its own) |
| S6 | Background jobs with progress, logs and cancellation that survive browser refresh; a warning before closing with a job running | done (wave 1, slice 4; wave 2, slice 2: jobs kept on disk, interrupted ones resumed or discarded, the Jobs view, a warning before closing the window with a job running, and SituCraft kept running until the job ends) |
| S7 | Common failures (memory, malformed input, missing prior, storage) classified, with remedies | done (wave 3, slice 5: memory and disk refused before a run, a missing prior, a malformed bundle, a full disk, a region without cells and unusable settings, each classified with what to do, in the studio, the Jobs view and remote control) |
| S8 | Deterministic for a seed, on any number of threads; the seed recorded | done (wave 3, slice 3: the sampler gives the same bytes on 1, 4 and 16 threads and on arm64 and amd64; the seed is recorded with its options; recorded with each segmentation in slice 4; slice 6: the reference refinements made again byte for byte each week; slice 9: patches chosen by the data and settings alone, never the threads or memory) |
| S9 | Outputs validated (geometry, counts, assignments, metadata) before they can be used | partial (wave 2, slice 2: outputs are checked in staging before they become artifacts, and an invalid one is never used; the instrument's segmentation is checked as the bundle's validation checks its cells, boundaries, masks and assignments; slice 3: SituCraft's nucleus expansion is checked before it becomes an artifact (geometry, assignments, counts, metadata, the files' agreement with one another); imported segmentations from slice 4; wave 3, slice 4: the transcript-informed segmentation's files are checked the same way, with its assignment probabilities, layer outlines and diagnostics when it writes them) |
| S10 | Segmentations from other tools imported: SpatialData Zarr, Baysor-style CSV plus GeoJSON, Xenium Ranger outputs, GeoJSON plus an assignment table | done (wave 2, slice 4: each checked against the dataset (units declared, never guessed; the transcripts it names by id and position; a bundle's run; ids) and recorded with its files' SHA-256; the Lung bundle through a Xenium bundle and through spatialdata-io's SpatialData gives the instrument's segmentation exactly; hand-made fixtures written by each format's own libraries) |
| S11 | Limitations shown in context: no cells beyond the prior's; uncertain boundaries for low-transcript cells and homogeneous regions | done (wave 3, slice 4: each run flags its cells with few transcripts, uncertain boundaries or no nucleus, and those left out; the Segmentation view says so with what every refinement shares, and the comparison workspace lists a chosen difference's flagged cells) |
| S12 | Whole-slide segmentation by overlapping patches, stitched | partial (wave 3, slice 9: a region of more than 4 million modelled transcripts refined in patches of about 2 million, one at a time, each refining the cells within 40 µm of its square, stitched by square and seed; as accurate as one run against simulated truth, seams included; 55 million transcripts refined in 5 to 6 GB where one run needs 10.8; what remains grows by about 100 bytes a transcript, so whole slides beyond about 120 million transcripts on 16 GB wait for the inputs and results streamed by patch, wave 10) |

## Comparison and decisions

| # | Requirement | Status |
| --- | --- | --- |
| C1 | Cells matched across segmentations by shared nucleus, overlap and transcript assignment, by a documented strategy | done (wave 2, slice 5: two cells correspond when they share a nucleus, when at least half of one lies within the other, or, without area, by their transcripts; the Lung bundle's instrument segmentation and 5 µm expansion pair one to one, as the expansion's own comparison counted) |
| C2 | Differences classified: one-to-one, expanded/contracted, split, merge, unmatched, assignment-only | done (wave 2, slice 5: and rearranged, several to several; hand-drawn fixtures of each class) |
| C3 | Metrics, globally and per region: cells, nuclei, assigned fraction, transcripts per cell, areas, geometry validity, changed assignments, IoU and boundary displacement, spurious co-expression, runtime and size | done (wave 2, slice 5: each segmentation's cells, nuclei, assigned fraction, transcripts per cell, areas, cells in pieces, job time and size; between two the pixels that agree, the transcripts that changed cell, IoU and boundary displacement; spurious co-expression after Jones et al. 2025 and the mutually exclusive co-expression rate of Hartman and Satija 2024; by regions of 50 µm too) |
| C4 | A ranked review queue by reassignment, contamination, disagreement, low counts or reviewer rules | done (wave 2, slice 6: ranked by reassigned transcripts, mixed lineage markers, disagreement or low counts, among the classes and reassigned transcripts the reviewer's rules keep; each difference opened where it lies; the cell-matching table) |
| C5 | Promotion of one segmentation as the project default, with a rationale, as an append-only event; alternatives never deleted | done (wave 2, slice 6: promote, keep for exploration or reject, each with a reason and the comparison it rests on, appended; the default never rejected; nothing deleted) |
| C6 | Downstream impact: composition, marker mixing, labels and neighborhoods under each segmentation | planned (waves 4, 6) |
| C7 | Any saved analysis re-run against another segmentation to test its robustness | planned (wave 6) |

## Registration and pathology

| # | Requirement | Status |
| --- | --- | --- |
| R1 | Post-Xenium H&E and IF OME-TIFF images imported | planned (wave 5) |
| R2 | A supplied 3×3 alignment read and applied | planned (wave 5) |
| R3 | Registration by landmarks (affine) and automatically (rigid/affine), optionally refined non-rigidly | planned (wave 5) |
| R4 | Alignment checked by checkerboard, flicker, edge overlay and landmark residuals, in µm | planned (wave 5) |
| R5 | Transforms versioned with source and destination coordinate systems, landmarks, residuals, author and time | planned (wave 5) |
| R6 | ROI tools: rectangle, polygon, freehand, brush; GeoJSON import and export compatible with QuPath | planned (wave 5) |
| R7 | Review actions on cells and ROIs: accept, relabel, ambiguous, exclude, segmentation concern, comment | planned (wave 5) |
| R8 | Review provenance: reviewer, action, previous and new value, time, segmentation, label set, transform, confidence | planned (wave 5) |

## Cell typing

| # | Requirement | Status |
| --- | --- | --- |
| T1 | Labels and confidence imported from CSV, Parquet or AnnData Zarr | planned (wave 4) |
| T2 | Normalization, principal components, a kNN graph and Leiden clustering, with every parameter shown and seeded | done (wave 4, slice 1: scanpy's steps and defaults, exact principal components and neighbors, UMAP's graph, Leiden, markers by the Wilcoxon rank-sum test; every parameter recorded in an artifact; the same on every processor; validated against scanpy, leidenalg, umap-learn and scikit-learn on both development bundles and a simulated tissue) |
| T3 | A UMAP embedding linked to the tissue | planned (wave 4) |
| T4 | Hierarchical labels (class, lineage, subtype, state) from positive and negative marker sets, with inspectable scores | planned (wave 4) |
| T5 | Mapping to a labeled single-cell reference, optional | planned (wave 4) |
| T6 | Evidence card per label: confidence, supporting and contradicting markers, counts, source, neighbors, review status | planned (wave 4) |
| T7 | Predicted, curated and consensus label versions side by side | planned (wave 4) |
| T8 | A review queue of low-confidence, spatially implausible and segmentation-sensitive cells | planned (waves 4, 5) |

## Spatial analysis

| # | Requirement | Status |
| --- | --- | --- |
| N1 | Graphs by radius, kNN, Delaunay, boundary contact, restricted to ROIs; units and parameters kept | planned (wave 6) |
| N2 | Neighborhood enrichment by seeded permutation | planned (wave 6) |
| N3 | Co-occurrence, nearest-neighbor and all-neighbor distances, contact fractions, Ripley's K/L | planned (wave 6) |
| N4 | Infiltration and exclusion relative to region boundaries | planned (wave 6) |
| N5 | Cellular neighborhoods (niches) from neighborhood composition, mapped, with their defining cell types and markers | planned (wave 6) |
| N6 | ROI comparison with area and cell-count denominators | planned (wave 6) |
| N7 | Spatially variable genes (Moran's I) | planned (wave 6) |
| N8 | Every result can show its contributing cells in the tissue | planned (wave 6) |
| N9 | Ligand–receptor analysis, if added: hypothesis-generating, requiring expression and proximity evidence | parked |

## Cohorts

| # | Requirement | Status |
| --- | --- | --- |
| H1 | An experimental design (biological and technical replicates, pairing, covariates, contrasts) before any group inference | planned (wave 7) |
| H2 | Sample summaries: composition, niches, QC, graph metrics, marker expression | planned (wave 7) |
| H3 | Sample- or patient-level inference by default; effect sizes and intervals; raw and adjusted p-values; the correction and universe of tests recorded | planned (wave 7) |
| H4 | Batch and panel differences shown; no silent correction | planned (wave 7) |
| H5 | Panel compatibility checked before cross-sample gene comparisons | planned (wave 7) |

## Output, provenance and reporting

| # | Requirement | Status |
| --- | --- | --- |
| E1 | SpatialData Zarr export (images optional, shapes, points, AnnData table, transformations) | done (wave 2, slice 7: Zarr format 3 as the spatialdata package writes it, without images: the label raster, GeoParquet outlines, transcripts as points with their cells, the AnnData table annotating the labels, each element's transformation to µm; read by spatialdata 0.8.0 exactly, and imported back by SituCraft as the same segmentation) |
| E2 | AnnData Zarr, Parquet, CSV, Matrix Market and GeoJSON exports | done (wave 2, slice 7: each export's files listed by SHA-256 with content fingerprints every format shares; read back by SituCraft's readers, and by anndata, pyarrow, pandas, scipy and geopandas in a maintainer-run check whose result is committed) |
| E3 | A command for a user-installed Xenium Ranger `import-segmentation`, prepared with validated inputs | done (wave 2, slice 7: the cells and the bundle's nuclei as NumPy label masks on the morphology image's pixels, checked for size, cells without pixels, transcripts assigned otherwise than the mask holds, a dataset without nuclei and a zipped bundle; the command written and shown, never run) |
| E4 | Methods text written from the actual metadata, citing every method used | partial (wave 1, slice 7: a paragraph per dataset from its record, validation and filters, citing the platform, the data's source and SituCraft's version; wave 2, slice 7: a default segmentation other than the instrument's, its method, citation and the decision choosing it with the comparison it rests on; later waves add their methods, wave 8 the full record) |
| E5 | Recipes: a job's spec as readable JSON, re-runnable headless | partial (wave 3, slice 5: a refinement's recipe saved and loaded in the studio and run headless by `situcraft refine`, byte for byte as the app, its folder imported back; other jobs' recipes in wave 9) |
| E6 | Figures (SVG, PDF, PNG) with scale bar, legend and provenance | planned (wave 8) |
| E7 | A certificate that `situcraft verify` checks by recomputing | planned (wave 8) |
| E8 | A redacted, self-contained static result package | planned (wave 8) |
| E9 | A list of the software and methods behind a result, with versions and citations | planned (wave 8) |

## Automation

| # | Requirement | Status |
| --- | --- | --- |
| A1 | Remote control from local programs (the hub the documentation's screenshots and later agents use) | done (wave 2, slice 1: `--remote-control`, 20 actions performed in the window, `remote.json`, a token for paths and files; the screenshots and the viewer suite drive the window through it) |
| A2 | An MCP server: read-only tools by default; compute tools only when enabled | planned (wave 9) |
| A3 | Cell and transcript queries compiled from a typed filter tree, never arbitrary SQL or shell | planned (wave 9) |
| A4 | Every agent call logged with parameters, affected artifacts and returned result IDs | planned (wave 9) |
| A5 | Headless `situcraft run` for batch and HPC use | planned (wave 9) |
| A6 | R and Python clients generated from the tool list | planned (wave 9) |

## Quality, security and accessibility

| # | Requirement | Status |
| --- | --- | --- |
| X1 | Loopback only, Host-header check, same-origin API, strict CSP, path allowlist, safe archive extraction | partial (wave 1, slice 1: loopback, Host check, same-origin API, CSP, only named paths served; archive extraction with slice 3) |
| X2 | Keyboard workflows; color-vision-friendly palettes with non-color encodings; text equivalents for status; contrast audited | planned (wave 1, every wave) |
| X3 | Every reader and decoder fuzzed; hostile files refused with a message, never a crash | partial (wave 1, slice 2: every codec and reader fuzzed, crashers fixed and kept; later readers as they arrive) |
| X4 | Performance budgets checked automatically (tile decode, indexing, viewport queries, segmentation) | partial (wave 1, slice 2: a JPEG 2000 tile in about 90 ms on one core, budget 150 ms, benchmarked; slice 4: each development bundle's derived data built within a budget of two minutes (about 2 and 4 s on ten cores) and within 500 MB together, tested; slice 5: frames per second while panning, region queries and the time to draw a view, measured in headless Chrome against budgets (30 fps where there is a GPU; 500 ms)) |
| X5 | Stable, versioned project and export formats; later versions open earlier projects | planned (every wave; promised at 1.0) |
| X6 | Documentation and site: installing, a guide per feature, formats, network, MCP | planned (every wave) |
