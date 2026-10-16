# Formats SituCraft reads and writes

What SituCraft 0.2.0 reads, what it writes, and how. Every format is read by SituCraft's own code;
the program needs no other software. The research notes
([research.md](../situcraft-spec/research.md), section 1) give the details and their sources.

## Xenium output bundles

SituCraft reads bundles written by Xenium Onboard Analysis (XOA) and Xenium Ranger. XOA 2.x is
supported and checked against real bundles; other versions are read best-effort, and the Data view
says so. A bundle opens as its folder, its `experiment.xenium`, or a `.zip` holding the bundle at
its root or in one folder. SituCraft never writes to a bundle; a zipped bundle's compressed members
are unpacked once into the cache.

| Role | File | Format | Read for |
| --- | --- | --- | --- |
| Manifest (required) | `experiment.xenium` | JSON | Software and version, run and region, panel, pixel size, image files, counts |
| Run metrics | `metrics_summary.csv` | CSV | The QC view's metrics; 20 of them recomputed from the data |
| Gene panel | `gene_panel.json` | JSON | The panel's genes, checked against the transcripts and matrix |
| Transcripts (required) | `transcripts.parquet` | Parquet | Every decoded transcript: position (µm), gene, quality (qv), cell, nucleus overlap |
| Cells (required) | `cells.parquet` | Parquet | Each cell's centroid, counts and areas |
| Cell and nucleus boundaries (required) | `cell_boundaries.parquet`, `nucleus_boundaries.parquet` | Parquet | The outlines drawn in the Viewer |
| Cell-by-feature counts (required) | `cell_feature_matrix.zarr.zip`, or the Matrix Market folder or `.tar.gz` | Zarr, Matrix Market | Checked against the cells (counts, order) and the panel; the run's median genes per cell |
| Morphology images (required) | `morphology_focus/morphology_focus_000N.ome.tif` (XOA 2), `morphology_focus.ome.tif` (XOA 1) | OME-TIFF, BigTIFF, JPEG 2000 tiles | The stains, every sample decoded exactly |
| Morphology z-stack | `morphology.ome.tif` | OME-TIFF, JPEG 2000 tiles | Checked against the in-focus images |
| Cells for Xenium Explorer | `cells.zarr.zip` | Zarr | Each cell's segmentation method; the segmentation masks (for nuclear transcript density); checked against `cells.parquet` |

`transcripts.zarr.zip`, the CSV copies of the tables, `cell_feature_matrix.h5`,
`analysis.zarr.zip`, `analysis.tar.gz`, `aux_outputs.tar.gz` and `analysis_summary.html` are listed
with their sizes but not read; when only a CSV copy of a required table is present, the Data view
says so.

Coordinates are in microns throughout; images are related to them by the manifest's pixel size
(0.2125 µm at full resolution). Transcripts of quality 20 or more (Q20) are the ones counted, as
by XOA.

A bundle with `situcraft-simulation.json` at its root was made by `situcraft simulate` (below),
not by an instrument. SituCraft reads it as any other bundle, labels it simulated wherever it is
shown, and lists its truth (`truth/`) among its files.

### The underlying formats

| Format | Specification | What SituCraft supports |
| --- | --- | --- |
| Parquet | Apache Parquet format; Thrift compact protocol | Flat schemas, every physical type; plain, dictionary, RLE, delta and byte-stream-split encodings; data pages v1 and v2; Zstandard, Snappy, gzip, LZ4 or no compression |
| Zarr | Zarr storage specifications v2 and v3 | Directories and zips; Blosc, Zstandard, zlib, gzip and LZ4 chunks; C and F order; both byte orders; fill values; CRC-32C; sharding; text of fixed and varying length (VLenUTF8) |
| AnnData | anndata's on-disk format, in Zarr | Data frames (text, categorical, nullable and numeric columns); matrices as sparse rows, sparse columns or dense |
| GeoJSON, WKB | RFC 7946; OGC Simple Features | Polygons and MultiPolygons with holes |
| TIFF | TIFF 6.0, BigTIFF, OME-TIFF | Strips and tiles, SubIFD pyramids, deflate, LZW, PackBits, predictors; OME-XML, multi-file OME-TIFF, z-stacks |
| JPEG 2000 | ITU-T T.800 (ISO/IEC 15444-1), Part 1 | JP2 files and codestreams; every progression order and code-block style; tiles, layers and precincts; 5/3 and 9/7 wavelets; decoding at reduced resolution |
| Zstandard | RFC 8878 | Frames with XXH64 checksums |
| LZ4, Snappy, Blosc | Their projects' format descriptions | Blocks; Blosc byte and bit shuffling |
| Matrix Market | NIST Matrix Market exchange format | Coordinate matrices, gzip-compressed |

## Segmentations SituCraft imports

The Segmentation view's **Import a segmentation** (and the `import_segmentation` remote action)
reads a segmentation made by another tool into a dataset:

| Format | Files | What is read |
| --- | --- | --- |
| SpatialData Zarr | the store (`.zarr` folder or its zip), format 2 or 3 | the cells from a shapes element (polygons) or a labels element (a label image); each transcript's cell from a points element (a `cell_id`, `cell` or `assignment` column, or one named; matched by `transcript_id` at its observed place, `observed_x` and `observed_y` when a tool kept them beside where it moved the transcript); the counts from a table annotating the cells; each element's coordinate transformation to a coordinate system (`global` unless named) |
| Baysor | `segmentation.csv`, and `segmentation_polygons.json` | each transcript's cell (`cell`; `0` and `is_noise` are none), by `transcript_id` when the table has it, else by `x`, `y` and `gene`; the cells' polygons |
| GeoJSON | polygons (`.geojson`, `.json`), and optionally a table (`.csv`, `.csv.gz`, `.tsv`, `.parquet`) | the polygons, each feature naming its cell (`cell_id`, `cell`, `id`…, or the feature's id); each transcript's cell from the table (`transcript_id` and the cell), else from the polygon it lies in |
| Xenium Ranger output | the bundle (folder, zip or `experiment.xenium`) of the same run | its cell mask, cells, boundaries, and each transcript's `cell_id` |
| SituCraft segmentation | the folder `situcraft refine` wrote (below) | its files as they are, once their image's grid and their transcripts' count match the dataset's; a refinement is recorded as its method, with its parameters |

Coordinates are read in µm unless the import says they are pixels of the morphology image: they
are never guessed. A cell of `""`, `UNASSIGNED`, `NA`, `nan`, `None` or `-1` is none. The import is
checked against the dataset (the transcripts it names, by id and, where it gives them, position;
where its cells lie; its ids), and becomes a segmentation of SituCraft's own files (below), with
the SHA-256 of every file it read.

## Other files

SituCraft recognizes OME-TIFF images, Parquet and CSV tables, GeoJSON polygons and Zarr stores
(on the command line or by **Open a bundle**) and lists them under **On this computer**. Reading
them for analysis arrives with later releases: registered H&E and IF images, labels and
annotations (see the [roadmap](../situcraft-spec/roadmap.md)).

## What SituCraft writes

### Project documents

A project is one JSON document, saved automatically in the library (`<data dir>/projects/`):

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-project"`, `1` |
| `id`, `name`, `created`, `modified` | Its identity |
| `datasets` | Each bundle: its place, run and analysis identifiers, panel, tissue, counts, pixel size, channels, a fingerprint of its files (each file's size; the SHA-256 of its manifest, metrics and panel), and its validation summary and findings |
| `artifacts` | Results: filters (a name, a version, its parent version, its conditions, and its result: the number of cells, how many it keeps, and a checksum of which), from 0.2.0 segmentations and comparisons, and from 0.4.0 clusterings, their names and maps (below) |
| `decisions` | Recorded choices, with their reasons (from 0.2.0) |
| `views` | Saved views (below) |
| `exports` | The exports written from its segmentations (from 0.2.0; absent until the first) |
| `history` | Every change, with its time |

A filter's checksum is the SHA-256 of the cell count (32-bit unsigned, little-endian) followed by
one bit per cell, in the order of `cells.parquet`, the first cell in the lowest bit of the first
byte, set when the cell is kept. Anyone can recompute it from the bundle and the conditions.

A saved view records its dataset, its mode (Viewer or QC), the segmentation shown (`segmentation`:
`instrument` for the dataset's own, else the segmentation artifact's id), the place and zoom
(`camera`: the center in µm and the µm per CSS pixel), each stain by name with its visibility,
color, window and gamma, the genes by name with the transcript settings (`colorBy: "probability"`
when transcripts are colored by their assignment's probability), the cell layers, the selected
cell (its row and ID in the segmentation shown), and in the QC view the map and filter. Names rather than numbers keep a
view valid when the derived data are rebuilt. A link to a view is the page's address with
`#project=<project id>&view=<view id>`; it opens in SituCraft on the computer whose library holds
the project.

SituCraft reads projects saved by any earlier version; a project saved by a newer version is
refused with a message to update. A project saved by each release (0.1.0, 0.2.0) is kept in
`validation/projects/`, and every later release is checked to reopen it exactly and, for 0.2.0's,
to make its artifacts again with the same checksums.

A segmentation is recorded in `artifacts` (kind `segmentation`) as the artifact the program keeps:
its id, name, dataset, state, method (name, version, citation, and the cells each of the
instrument's methods segmented), software, the job that made or registered it, its parents, its
files (path, role, size, SHA-256, and `bundle` for a file of the dataset's bundle), its summary and
its check's findings; an import's also records its sources (each path imported, the number and
size of the files read under it, and their digest). A comparison is recorded as kind `comparison`:
its id, dataset, its two segmentations (`a`, `b`), state, method, parameters, the job that made it,
its summary and findings.

`exports` records each export: its id, dataset, segmentation (id and name), formats, folder, when,
the SituCraft that wrote it, the number and size of its files, its totals and content fingerprints
(below), what it found, and the Xenium Ranger command when it prepared one. The export's own
`situcraft-export.json` lists every file by its SHA-256.

`decisions` records each decision about a dataset's segmentations, appended and never removed:
`{kind, dataset, artifact, rationale, by, time, comparison}`, the kind `promote-segmentation` (its
default), `keep-segmentation` (kept for exploration) or `reject-segmentation`, and `comparison`
the comparison it rests on, when there is one. A segmentation's status is its latest decision; the
default is the segmentation of the latest promotion not rejected since.

### Artifacts

What jobs make and register is kept in `<data dir>/artifacts/<id>/`, beside a `manifest.json`:

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-artifact"`, `1` |
| `id`, `kind`, `name`, `state`, `created` | Its identity; `state` is `complete` or `invalid` |
| `lineage` | `parents` (a bundle by its analysis UUID and manifest's SHA-256, or other artifacts), `job` (its id, kind and spec), `method` (name, version, citation, detail), `parameters`, `software` (each program's name, version and commit) |
| `files` | Each file's `path`, `role`, `size` and `sha256`; `location: "bundle"` for a file of the dataset's bundle, named by its path there and never copied |
| `findings` | What the kind's check found: `severity`, `code`, `message`, `count` |
| `summary` | What the kind says of it (a segmentation's cells, gene transcripts, medians and methods; for one SituCraft made, its agreement with the instrument's segmentation) |

### Segmentations SituCraft makes

A segmentation SituCraft makes (nucleus expansion, from 0.2.0) or imports is an artifact of six
files of its own; one with errors planted for practice has a seventh, `planted.json` (below). Each binary file starts with four magic bytes, a format version (`uint32`) and its body's
length (`uint64`), little-endian, followed by the body compressed with zlib (RFC 1950/1951).
Readers check every size before reading, and the zlib checksum.

| File | Magic | Holds |
| --- | --- | --- |
| `segmentation.json` | | `format` (`"situcraft-segmentation"`), `version`, `method`, `parameters` (every one), the raster's `width`, `height` and `pixelSizeUm`, the numbers of `cells`, `transcripts` and `genes`, the `nuclei` it grew from (`source`, `count`), the gene transcripts of quality 20 or more and those assigned, and the `agreement` with the instrument's segmentation |
| `cells.bin` | `SCSC` | Each cell's id (`c1`, `c2`, … for an expansion; the tool's own for an import) and the bundle's id of the cell its nucleus lies in; centroid (µm), area and nucleus area (µm²); its gene transcripts of quality 20 or more, all its transcripts, and its genes detected |
| `mask.bin` | `SCMK` | The label raster at the morphology image's resolution (label *k* is cell *k*; 0 none), each row as (label, length) runs in unsigned varints |
| `assignments.bin` | `SCAS` | Each transcript's cell, in the row order of the bundle's `transcripts.parquet` (0: none) |
| `counts.bin` | `SCCT` | The cell-by-gene counts of gene transcripts of quality 20 or more: the genes' names, then compressed sparse rows (row offsets, gene indices, counts) |
| `outlines.bin` | `SCOL` | Each cell's outline in µm (its largest piece, simplified to half a pixel), with its pixels, pieces and holes counted |

These files are SituCraft's own; for other tools, export a segmentation (below).

SituCraft's transcript-informed segmentation (`SituCraft transcript-informed segmentation`, from
0.3.0) writes the six with what its model adds:
- **Its cells** are named `c1`, `c2`, …: cell *k* is the instrument's cell *k* (`cells.parquet`'s
  row *k*), whose id it records, whether it was refined or not.
- **Its raster** is the instrument's, with each refined region's lattice drawn again: there, each
  refined cell's 2D footprint (its voxels' most frequent labels, at the image's resolution), and
  the cells held about the region as the instrument drew them. Its outlines are simplified to half
  a voxel.
- **A region of more than 4 million modelled transcripts** (twice `area.patch_transcripts`, 2
  million by default) is refined in patches, one after another: equal rectangles of about 2
  million transcripts each, each run refining the cells within 40 µm of its rectangle and holding
  the rest. The patches are stitched: each pixel and transcript is taken from the patch whose
  rectangle holds it, each cell's certainty and flags from the patch holding its nucleus's seed.
- **Its assignments** are, for the gene transcripts it models (of the quality it was given, in a
  region's lattice), each one's most frequent place over the summarized sweeps: a cell when the
  transcript lay in its voxels and the model attributed it to the cell, else the background. Every other
  transcript (controls, those below the quality or outside the regions) goes to the cell holding
  the pixel it was observed in.
- **Its parameters** (`segmentation.json`) are every setting after defaults: `model`, `sampler`,
  `stages`, `block_um`, `seed`, `samples`; `area` as asked (`mode`: `whole`, `sections` or
  `regions`, with `regions` and `section_gap_um`; `patch_transcripts`, when set, the transcripts a
  patch aims at) and `regions`, the rectangles refined (µm: `x0_um`, `y0_um`, `x1_um`, `y1_um`;
  for a region refined in patches, each patch's rectangle of refined cells); `patches`, each
  patch's own rectangle, when there are patches; `min_qv`, the quality modelled, and `counts_min_quality`
  (20); `start` and `start_stages`, each region's first stage; `preset` and `presets_version`; and
  `second_seed`, when a second run checked the first.

| File | Magic | Holds |
| --- | --- | --- |
| `probabilities.bin` | `SCPR` | Each transcript's posterior probability of its cell (`float32`, in the row order of `transcripts.parquet`): the share of the summarized sweeps it lay in that cell; NaN for a transcript assigned by place |
| `certainty.bin` | `SCBC` | Each cell's boundary certainty (`float32`, in `cells.bin`'s order): the mean, over its pixels, of the share of the summarized sweeps each was its; NaN for a cell held or outside the regions refined. A cell is flagged uncertain below 0.75 |
| `diagnostics.json` | | `summary` (the sweeps and samples; the split-half agreement, on every eighth transcript; the mean assignment probability and the share below 0.5; the log-likelihood's drift, Geweke's z and `gewekeJudged`, false when fewer than 100 sweeps were summarized; the voxels moved per sweep at the end; the cells refined, held and flagged; the regions, and `patches`, the patches they were refined in; the transcripts modelled; the background: `falseDecodeShare` and `ambientShare`, the shares of the modelled transcripts its rates account for (their mean over the summarized sweeps), `negativeControls`, the negative controls' transcripts in the regions that measure the false decodes, and `inBackgroundShare`, the share the summary leaves in no cell; `seedAgreement`, with a second seed: the share of transcripts either run put in a refined cell that both put in the same cell), `limitations` (the cells, by id, with fewer than 10 gene transcripts, uncertain boundaries, or no nucleus, and the instrument's cells left out; the thresholds) and `regions` (each region or patch refined: its rectangle, a patch's own rectangle as `patch`, its own `summary`, and its `trace`, each sweep's stage, voxels moved, transcripts repositioned and log-likelihood). Over several regions, the summary's Geweke's z is the one farthest from 0 |
| `layers.bin` | `SCLY` | With more than one layer: each layer's depths (µm) and each cell's outline in it, as `outlines.bin` holds them |

A segmentation with errors planted for practice (`SituCraft planted errors`, simulated) is the
instrument's segmentation with known errors in it, and keeps its answer key in `planted.json`:
`format` (`"situcraft-planted-errors"`), `source` (the segmentation planted in), `params` (how many
merges, splits, missed and grown cells, how far a grown cell reaches, the seed) and `errors`, each
with its `kind` (`merge`, `split`, `missed`, `grown`), the source's `cells` it changed, the planted
segmentation's cells they became (`into`), where it lies (`x`, `y`, µm) and a `note` saying what
makes it an error. A planted segmentation is never promoted or exported.

### Comparisons

A comparison of two segmentations is an artifact of two files:

| File | Magic | Holds |
| --- | --- | --- |
| `comparison.json` | | `format` (`"situcraft-comparison"`), `version`, `method`; the two segmentations (`a`, `b`: artifact id and name); `params` (the correspondence's and co-expression's thresholds, the regions' size, the seed); each segmentation's measures (`metricsA`, `metricsB`); the differences by class (`classes`, and each side's cells in each: `cellsA`, `cellsB`); the transcripts that changed cell (`changes`, `changesQ20`: gained, lost, moved; `changesQ20OfAssignedA`, of those A assigns to a cell: with the truth as A, the rest are in their true cell); `iou` and `displacement` of the cells the same in both; `pixelAgreement`; the co-expression reference (`coexpression`) and the MECR's gene pairs (`mecrPairs`); and the same counts by region (`regions`: squares of `sizeUm`, row-major). A measure not taken is `null` |
| `groups.bin` | `SCCG` | Each difference: its class (by its index in one-to-one, expanded, contracted, split, merged, rearranged, only in A, only in B, assignment-only), its cells in A and in B (indices into each segmentation's cells), the IoU, the transcripts' Jaccard and the boundary displacement (µm) of one cell in each, the gene transcripts whose cell changed, where it lies (µm), its cells' gene transcripts in A and in B, and the most pairs of mutually exclusive lineage markers one of its cells expresses together in A and in B (method version 2) |

A comparison can be removed from its project; its folder then moves to the library's
`trash/artifacts/<id>-<time>/` once no saved project names it.

### Clusterings

A clustering of a segmentation's cells (from 0.4.0) is an artifact of five files. The binary ones
have the envelope above (`segmentation`'s: magic, version, length, zlib); arrays are
little-endian, a string is its length (unsigned varint) then its UTF-8 bytes.

| File | Magic | Holds |
| --- | --- | --- |
| `clustering.json` | | `format` (`"situcraft-clustering"`), `version`, `method`; the `segmentation` (artifact id and name); `parameters` (`minTranscripts`, `components`, `neighbors`, `resolution`, `seed`); `fixed` (the normalization and its `targetSum`, the median total; `clip`; the metric, graph, quality and Leiden's `randomness`; the markers' test); `cells` (the segmentation's, those `analyzed`, `excluded` by the rule or the `filter` with its id, name and checksum, `withoutTranscripts`); `genes` (`count`, `notDetected`, `medianCellsExpressing`); `pca` (`components`, each one's `variance` and `varianceRatio`, `totalVariance`); `graph` (`neighbors`, `edges`); `clusters` (`count`, `sizes` from the largest, `modularity`, `iterations`); `top`, each cluster's first ten markers (`gene`, `score`, `logfc`, `padj`, `pctIn`, `pctOut`); `seconds`, the run's timings |
| `cells.bin` | `SCKC` | The cells analyzed (u32 count; u32 indices into the segmentation's cells, increasing) and each one's cluster (u32, 0 the largest) |
| `pca.bin` | `SCPC` | The genes (u32 count; strings), the components k (u32), each gene's mean and standard deviation of the log-normalized values (f64), the loadings (f64, genes × k, row-major), and each analyzed cell's scores (u32 count; f32, cells × k) |
| `neighbors.bin` | `SCKN` | The cells n and the neighbors k of each (u32, u32: the parameter less one, the cell itself not listed), each one's neighbors nearest first (u32 indices among the cells analyzed, n × k) and their Euclidean distances in the components (f64) |
| `markers.bin` | `SCMR` | The clusters c and genes g (u32, u32), then six arrays of f64, each c × g, cluster-major: the rank-sum test's z-score, the log₂ fold change, the p-value, the Benjamini–Hochberg adjusted p-value, and the shares of cells expressing the gene in the cluster and out of it |

A clustering of the instrument's (registered from the bundle's `analysis.tar.gz`) has a `source`
in `clustering.json` (`tool`, `version`, `file`, `name`, `title`), keeps the instrument's cluster
numbers (from 0 here: the instrument's Cluster 1 is 0), has no `neighbors.bin`, and its `pca.bin`
holds the genes and their scaling with no components (k 0, the cells' count, no scores).

The project records each clustering it uses (an `artifacts` entry of kind `clustering`, with its
dataset, segmentation, parameters and summary), and each version of a clustering's names (kind
`cluster-names`: `clustering`, `version`, `parent`, `names`, a name or `null` per cluster, `by`,
`created`). The Viewer's fill `clusters:<artifact id>` colors cells by a clustering of the
segmentation drawn; saved views keep it.

### Maps

A map of a clustering's cells (from 0.4.0) is an artifact of two files:

| File | Magic | Holds |
| --- | --- | --- |
| `embedding.json` | | `format` (`"situcraft-embedding"`), `version`, `method`; the `segmentation` and the `clustering` (artifact id and name); `parameters` (`minDist`, `spread`, `epochs`, 0 for the default, `negativeRate`, `seed`); `fitted` (the curve's `a` and `b`, the `epochs` run, the `edges` optimized, the `init`ial layout); `cells`; `quality` (`k`, `kWide`, `trustworthiness` over `queries` cells, the mean `precision` and `recall`); `seconds`. A map of the instrument's has a `source` (`tool`, `version`, `file`, the `space` its quality is judged in) instead of `parameters` and `fitted` |
| `embedding.bin` | `SCEB` | The cells (u32 count; u32 indices into the segmentation's cells, increasing), each one's place (f32 × 2, interleaved) and its reliability score (f32, 0 to 1: its neighborhood precision and recall, averaged over it and its 15 nearest on the map) |

The project records each map it uses (kind `embedding`, with its segmentation and clustering).

### Jobs

Each job is kept in `<data dir>/jobs/<id>.json`: its kind, title, spec (every effective
parameter), state, times, number of runs, error and log. A job interrupted by SituCraft stopping is
`orphaned` when it starts again, to be resumed or discarded.

### Project archives (`.scz`)

A zip holding `project.json` (the project document) and a `README.txt`. Each dataset also records
its bundle's place relative to the archive (`archivePath`); opening the archive takes a bundle from
beside it first, from its recorded place otherwise. The bundles themselves are never inside.

### Derived data

The cache (`--cache-dir`) holds what SituCraft builds from each bundle: image tiles at every zoom,
a spatial index of the transcripts with density grids, tiled cell outlines, and, for each
segmentation drawn in the Viewer, its transcripts' cells and probabilities by the index's tiles. These are internal
files, versioned with SituCraft and rebuilt when their format changes; they can be deleted at any
time. They are not for exchange: export a segmentation instead (below).

### Exports

An export writes one segmentation of a dataset into a folder the user names, in the formats asked
for, beside `situcraft-export.json`. The folder is written under a temporary name beside it and
renamed into place when complete; an existing folder is replaced only when asked and only when it
is a SituCraft export. Coordinates are in µm, as the bundle's are; the counts are of gene
transcripts of quality 20 or more, as every segmentation's in SituCraft. The same segmentation
gives the same files.

`situcraft-export.json`:

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-export"`, `1` |
| `software`, `commit`, `created` | The SituCraft that wrote it, and when |
| `dataset` | Its name, the run's experiment and analysis UUIDs, name and region, and the bundle's place |
| `segmentation` | The segmentation's artifact id, name, method, version and citation |
| `units` | `coordinates` (`µm`), `pixelSizeUm`, `rasterSizePx` (the label raster's width and height) |
| `counts` | `cells`, `cellsWithOutline`, `cellsWithPixels`, `genes`, `transcripts`, `assignedTranscripts`, `countEntries`, `countSum` |
| `content` | Fingerprints of what every format holds (below) |
| `formats` | Each format written, with what its files hold |
| `files` | Every file: `path`, `format`, `size`, `sha256` |
| `findings` | What the export found (Xenium Ranger's inputs' checks): `format`, `severity`, `code`, `message`, `count` |
| `xeniumRanger` | The prepared command: its `id`, `args` and `command` |

The content fingerprints let a reader check, with any library, that it read the segmentation
exactly, whatever the format; each is a SHA-256 in hex:

- `labelsSha256`: the label raster as little-endian `uint32`, row by row (height × width);
- `countsSha256`: a line `cell_id<TAB>gene<TAB>count<LF>` for each nonzero count, the cells in the
  export's order and each cell's genes in the order of their names' bytes;
- `assignmentsSha256`: a line `cell_id<LF>` for each transcript, in the order of the bundle's
  `transcripts.parquet`, `UNASSIGNED` for a transcript in no cell.

| Format | Files | Holds |
| --- | --- | --- |
| SpatialData Zarr (`spatialdata`) | `spatialdata.zarr/` | A SpatialData store (Marconato et al. 2025) in Zarr format 3, laid out as the spatialdata package writes one: `labels/cell_labels` (the label raster, single-scale, scaled by the pixel size into the `global` coordinate system in µm; label *k* is cell *k*), `shapes/cell_boundaries` (GeoParquet: one WKB polygon per cell with an outline, indexed by `cell_id`), `points/transcripts` (one Parquet part: `x`, `y`, `z`, `feature_name`, `cell_id`, `qv`, `transcript_id`), and `tables/table` (AnnData annotating `cell_labels`, region key `region`, instance key `cell_labels`; obs as below, with `cell_id`, `region` and `cell_labels`) |
| AnnData Zarr (`anndata`) | `anndata.zarr/` | The counts as AnnData in Zarr format 2 (anndata's default write format until 0.13): `X` cells × genes in compressed sparse rows (`float32`), `obs` indexed by cell id with `x_centroid`, `y_centroid`, `transcript_counts`, `assigned_transcripts`, `detected_genes`, `cell_area`, `nucleus_area`, `nucleus_id`; `var` indexed by gene with `gene_ids` and `feature_types`; `obsm['spatial']` the centroids (µm); `uns['situcraft']` where it came from |
| Parquet (`parquet`) | `parquet/*.parquet` | Four tables in Xenium's column names: `cells` (`cell_id`, `x_centroid`, `y_centroid`, `transcript_counts`, `assigned_transcripts`, `detected_genes`, `cell_area`, `nucleus_area`, `nucleus_id`), `transcripts` (`transcript_id`, `cell_id` or `UNASSIGNED`, `feature_name`, `x_location`, `y_location`, `z_location`, `qv`), `cell_boundaries` (`cell_id`, `vertex_x`, `vertex_y`: each outline's vertices, the first repeated last) and `counts` (`cell_id`, `feature_name`, `count`: the nonzero counts). GZIP-compressed pages, unsigned integers annotated as such |
| CSV (`csv`) | `csv/*.csv.gz` | The same four tables, gzipped, numbers in their shortest exact form |
| Matrix Market (`mex`) | `cell_feature_matrix/` | `matrix.mtx.gz` (genes × cells, 1-based, `integer general`, with a `%metadata_json` line), `features.tsv.gz` (gene id, name, `Gene Expression`) and `barcodes.tsv.gz` (cell ids), as Xenium and Cell Ranger write them |
| GeoJSON (`geojson`) | `cell_boundaries.geojson` | A FeatureCollection: each cell with an outline as a Polygon feature (µm) whose `id` and `cell_id` name it, with its area, gene transcripts and nucleus id; a `situcraft` member says the units and the segmentation |
| Xenium Ranger inputs (`xenium-ranger`) | `xenium-ranger/` | `cells.npy` (the label raster as a NumPy `uint32` array on the morphology image's pixels: label *k* is row *k* of `cell_labels.csv`, 0 none), `nuclei.npy` (the bundle's nucleus mask), `cell_labels.csv` (`label`, `cell_id`) and `command.sh`, Xenium Ranger's `import-segmentation` with those files and the bundle, which SituCraft does not run |

Before Xenium Ranger's command is prepared, its inputs are checked: the masks' size against the
morphology image's (a fatal finding when they differ), cells without a pixel (Xenium Ranger
cannot keep them), transcripts the segmentation assigns otherwise than its mask holds at their
pixel (Xenium Ranger assigns by the mask), a dataset without a nucleus mask, and a zipped bundle
(Xenium Ranger reads a folder).

### Refinement recipes

A recipe is a refinement's spec as readable JSON, to keep with its result, share, and run again
(the Segmentation view's **Save the recipe**, `refinement_preflight`, and `situcraft refine`
beside its output):

| Field | Holds |
| --- | --- |
| `format`, `version`, `kind` | `"situcraft-recipe"`, 1, `"refinement"`. A newer version, another kind or a field this SituCraft does not know is refused |
| `dataset` | The bundle it was made for: `name`, `path`, `run_name` and `region_name` from `experiment.xenium`, and `simulated` |
| `refinement` | `preset`; `options` (`model`, `sampler`, `stages`, `block_um`, `samples`, `seed`, as `segmentation.json` records them); `area`; `min_qv`; `seed_check`; `gc_percent`, the memory target (it never changes the result) |
| `presets_version`, `method`, `citation`, `software` | What made it |
| `run` | How to run it again |

The same recipe on the same bundle gives the same segmentation, byte for byte, in the app or the
terminal, on any computer and any number of threads.

### Refinements in the terminal (`situcraft refine`)

`situcraft refine [flags] <bundle> <new folder>` refines a bundle's segmentation as the app does,
without the app, and writes the segmentation's files (above) to the new folder with its
`recipe.json`. The folder imports into the app as a SituCraft segmentation of the same dataset.

| Flag | Sets |
| --- | --- |
| `--recipe` | A recipe to run (the bundle defaults to its `dataset.path`); the flags below change it |
| `--preset` | `balanced` (the default), `conservative`, `flexible`, `low-memory`, `roi-pilot` or `expert` |
| `--seed`, `--sweeps`, `--samples` | The seed, the last stage's sweeps and the sweeps summarized |
| `--min-qv` | The quality the modelled transcripts reach (20) |
| `--region x0,y0,x1,y1` | A region to refine, in µm (repeatable); `--sections` refines the tissue's separate sections apart |
| `--seed-check` | Run again with the next seed and report the two runs' agreement |
| `--threads` | Threads to run on (every core); the result does not depend on them |
| `--force` | Run even when the estimate exceeds the computer's memory |
| `--preflight`, `--print-recipe` | Print what it would model, cover and cost, or its recipe, and exit |

The folder must not exist: it is written under a hidden name beside it and renamed into place when
the segmentation check passes. A failure is printed with what to do; the exit code is 2 for a
mistaken command, 1 for a failed run.

### Simulated tissues (`situcraft simulate`)

`situcraft simulate [flags] <new folder>` writes a simulated tissue as a Xenium output bundle, in
the layout of Xenium Onboard Analysis 2, so that any tool reading Xenium data reads it. Every cell,
nucleus and molecule is known, and the truth is written beside it. The method and its validation
are in [research.md](../situcraft-spec/research.md) §3.4.

| Flag | Default | Sets |
| --- | --- | --- |
| `--preset` | `standard` | The starting parameters: `standard` (500 × 500 µm, about 3,000 cells and 440,000 transcripts, in a few seconds), `small` (160 × 120 µm) or `large` (a mosaic of 7 × 6 fields of 870.4 µm, about 6.1 × 5.2 mm, 400,000 cells and 55 million transcripts: about 10 minutes, 3.2 GB of memory and 4 GB of disk) |
| `--seed` | the preset's | The random seed. The same parameters and seed give the same bundle, byte for byte, on any computer |
| `--width`, `--height` | the preset's | The field's size in µm: at least 20, and at most 50 million pixels in all (about 1.5 × 1.5 mm at 0.2125 µm, which needs about 4 GB of memory; 1 × 1 mm takes about 13 s and 1.7 GB). A larger tissue is a mosaic of fields (below) |
| `--params` | | A JSON file of parameters, each overriding the preset's; `--print-params` prints them all, with their names |
| `--quiet` | | Print only the summary |

The folder must not exist: it is written under a temporary name beside it and renamed into place
when complete, so an interrupted run (Ctrl+C) leaves nothing behind.

**A mosaic** (`tiles_across` and `tiles_down` in the parameters, 0 or 1 for one field) is a tissue
too large to make whole: tiles of the field's size, each a whole number of 512-pixel blocks each
way, side by side. Each tile is a tissue of its own (its layout, cells, molecules and stain, from
streams of its own), sharing the panel and every other parameter; the first is the single field
the same parameters make. The tiles are made one at a time and joined into one bundle, so memory
is a tile's whatever the mosaic's size: cell and nucleus ids, transcript ids and fields of view
are numbered across the mosaic, and coordinates placed in it.

| File | Holds |
| --- | --- |
| `situcraft-simulation.json` | `format` (`"situcraft-simulation"`), `version` (1), `simulated` (`true`), `software`, `about`, `truth` (the truth's folder), `parameters` (every parameter, with the seed) and `made` (the cells by type, nuclei in the section, the instrument's cells, the transcripts by origin) |
| `experiment.xenium` | The run: one `experiment_uuid` derived from the parameters, the pixel size, the cell count and median transcripts per cell, the image's path. `analysis_sw_version` names the layout the files follow, then what wrote them (`xenium-2.0.0 layout, situcraft-simulate 0.3.0`): readers choose how to read a bundle by its version |
| `transcripts.parquet` | Every observed molecule, in Xenium's columns and types: `transcript_id` (its field of view of 250 µm in the top 16 bits), `cell_id` (the simulated instrument's cell, or `UNASSIGNED`), `overlaps_nucleus` (`uint8`), `feature_name`, `x_location`, `y_location`, `z_location` (µm), `qv`, `fov_name`, `nucleus_distance` (µm to the nearest nucleus in the section, from the molecule's pixel; 0 over one), `codeword_index` (its feature's codeword in `gene_panel.json`) |
| `cells.parquet`, `cell_boundaries.parquet`, `nucleus_boundaries.parquet` | The simulated instrument's cells (Xenium's columns; `segmentation_method` "Segmented by nucleus expansion of 5.0µm") and their outlines along pixel edges |
| `cell_feature_matrix.h5`, `cell_feature_matrix/` | Their counts of Q20 molecules of every feature: as a 10x Genomics feature-barcode matrix in HDF5 (`/matrix`: `barcodes`, `data`, `indices`, `indptr`, `shape`, and `features/` with `id`, `name`, `feature_type`, `genome`), and as Matrix Market |
| `cells.zarr.zip` | Zarr format 2, zipped, as XOA 2 lays it out: `masks/0` (nuclei: label *p* + 1 is nucleus *p*) and `masks/1` (cells: label *k* + 1 is `cells.parquet`'s row *k*) as `uint32` labels on the image's pixels; `masks/homogeneous_transform` (4 × 4, µm to pixels); `cell_id`; `cell_summary`; and `polygon_sets/0` (nuclei) and `polygon_sets/1` (cells), each with its outlines' `vertices` and `num_vertices`, each polygon's `cell_index` and `method`; the root's attributes name the sets and the segmentation methods |
| `morphology_focus/morphology_focus_0000.ome.tif` | The DAPI stain of the nuclei in the section: a tiled, deflate-compressed OME-TIFF with its reduced levels as SubIFDs (Xenium's own are JPEG 2000) |
| `metrics_summary.csv`, `gene_panel.json` | The run's metrics, computed as SituCraft's validation recomputes them, and the panel: genes `SIM001`…, negative control probes `NegControlProbe_SIM01`… and codewords `NegControlCodeword_0500`… |
| `truth/` | A Xenium Ranger-shaped bundle of the same run: `experiment.xenium`; `cells.parquet` (every true cell: `cell_type`, `nucleus_in_section`, areas, centroid, Q20 gene transcripts), both boundary tables and `cells.zarr.zip` of the true cells (the nuclei in the section numbered in their cells' order, `cell_index` naming each one's cell); `transcripts.parquet`, the observed molecules row for row in the bundle's columns, with their true `cell_id`, and `true_x_location`, `true_y_location`, `true_z_location`, `origin` (`cell`, `background`, `control probe`, `control codeword`) and `wide_displacement` |

To score a segmentation against the truth, import `truth/` into the simulated dataset as a
Xenium Ranger bundle (Segmentation view, **Import a segmentation**) and compare the two.

Other tools read a simulated bundle as they read Xenium's: spatialdata-io's Xenium reader reads
its table, transcripts, labels, boundaries and image with no warning, and tifffile and pyarrow
its image and tables (`validation/reference/read_simulation.py` checks each against what the
simulation made).
