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
| SpatialData Zarr | the store (`.zarr` folder or its zip), format 2 or 3 | the cells from a shapes element (polygons) or a labels element (a label image); each transcript's cell from a points element (a `cell_id`, `cell` or `assignment` column, or one named); the counts from a table annotating the cells; each element's coordinate transformation to a coordinate system (`global` unless named) |
| Baysor | `segmentation.csv`, and `segmentation_polygons.json` | each transcript's cell (`cell`; `0` and `is_noise` are none), by `transcript_id` when the table has it, else by `x`, `y` and `gene`; the cells' polygons |
| GeoJSON | polygons (`.geojson`, `.json`), and optionally a table (`.csv`, `.csv.gz`, `.tsv`, `.parquet`) | the polygons, each feature naming its cell (`cell_id`, `cell`, `id`…, or the feature's id); each transcript's cell from the table (`transcript_id` and the cell), else from the polygon it lies in |
| Xenium Ranger output | the bundle (folder, zip or `experiment.xenium`) of the same run | its cell mask, cells, boundaries, and each transcript's `cell_id` |

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
| `artifacts` | Results: filters (a name, a version, its parent version, its conditions, and its result: the number of cells, how many it keeps, and a checksum of which), and from 0.2.0 segmentations and comparisons (below) |
| `decisions` | Recorded choices, with their reasons (from 0.2.0) |
| `views` | Saved views (below) |
| `exports` | The exports written from its segmentations (from 0.2.0; absent until the first) |
| `history` | Every change, with its time |

A filter's checksum is the SHA-256 of the cell count (32-bit unsigned, little-endian) followed by
one bit per cell, in the order of `cells.parquet`, the first cell in the lowest bit of the first
byte, set when the cell is kept. Anyone can recompute it from the bundle and the conditions.

A saved view records its dataset, its mode (Viewer or QC), the segmentation shown, the place and
zoom (`camera`: the center in µm and the µm per CSS pixel), each stain by name with its visibility,
color, window and gamma, the genes by name with the transcript settings, the cell layers, the
selected cell (row and ID), and in the QC view the map and filter. Names rather than numbers keep a
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
| `comparison.json` | | `format` (`"situcraft-comparison"`), `version`, `method`; the two segmentations (`a`, `b`: artifact id and name); `params` (the correspondence's and co-expression's thresholds, the regions' size, the seed); each segmentation's measures (`metricsA`, `metricsB`); the differences by class (`classes`, and each side's cells in each: `cellsA`, `cellsB`); the transcripts that changed cell (`changes`, `changesQ20`: gained, lost, moved); `iou` and `displacement` of the cells the same in both; `pixelAgreement`; the co-expression reference (`coexpression`) and the MECR's gene pairs (`mecrPairs`); and the same counts by region (`regions`: squares of `sizeUm`, row-major). A measure not taken is `null` |
| `groups.bin` | `SCCG` | Each difference: its class (by its index in one-to-one, expanded, contracted, split, merged, rearranged, only in A, only in B, assignment-only), its cells in A and in B (indices into each segmentation's cells), the IoU, the transcripts' Jaccard and the boundary displacement (µm) of one cell in each, the gene transcripts whose cell changed, where it lies (µm), its cells' gene transcripts in A and in B, and the most pairs of mutually exclusive lineage markers one of its cells expresses together in A and in B (method version 2) |

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
a spatial index of the transcripts with density grids, and tiled cell outlines. These are internal
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

### Simulated tissues (`situcraft simulate`)

`situcraft simulate [flags] <new folder>` writes a simulated tissue as a Xenium output bundle, in
the layout of Xenium Onboard Analysis 2, so that any tool reading Xenium data reads it. Every cell,
nucleus and molecule is known, and the truth is written beside it. The method and its validation
are in [research.md](../situcraft-spec/research.md) §3.4.

| Flag | Default | Sets |
| --- | --- | --- |
| `--preset` | `standard` | The starting parameters: `standard` (500 × 500 µm, about 3,000 cells and 440,000 transcripts, in a few seconds) or `small` (160 × 120 µm) |
| `--seed` | the preset's | The random seed. The same parameters and seed give the same bundle, byte for byte, on any computer |
| `--width`, `--height` | the preset's | The field's size in µm: at least 20, and at most 50 million pixels in all (about 1.5 × 1.5 mm at 0.2125 µm, which needs about 4 GB of memory; 1 × 1 mm takes about 13 s and 1.7 GB) |
| `--params` | | A JSON file of parameters, each overriding the preset's; `--print-params` prints them all, with their names |
| `--quiet` | | Print only the summary |

The folder must not exist: it is written under a temporary name beside it and renamed into place
when complete, so an interrupted run (Ctrl+C) leaves nothing behind.

| File | Holds |
| --- | --- |
| `situcraft-simulation.json` | `format` (`"situcraft-simulation"`), `version` (1), `simulated` (`true`), `software`, `about`, `truth` (the truth's folder), `parameters` (every parameter, with the seed) and `made` (the cells by type, nuclei in the section, the instrument's cells, the transcripts by origin) |
| `experiment.xenium` | The run: one `experiment_uuid` derived from the parameters, the pixel size, the cell count and median transcripts per cell, the image's path. `analysis_sw_version` names the layout the files follow, then what wrote them (`xenium-2.0.0 layout, situcraft-simulate 0.3.0`): readers choose how to read a bundle by its version |
| `transcripts.parquet` | Every observed molecule: `transcript_id` (its field of view of 250 µm in the top 16 bits), `cell_id` (the simulated instrument's cell, or `UNASSIGNED`), `feature_name`, `x_location`, `y_location`, `z_location` (µm), `qv`, `fov_name`, `overlaps_nucleus` |
| `cells.parquet`, `cell_boundaries.parquet`, `nucleus_boundaries.parquet` | The simulated instrument's cells (Xenium's columns; `segmentation_method` "Segmented by nucleus expansion of 5.0µm") and their outlines along pixel edges |
| `cell_feature_matrix.h5`, `cell_feature_matrix/` | Their counts of Q20 molecules of every feature: as a 10x Genomics feature-barcode matrix in HDF5 (`/matrix`: `barcodes`, `data`, `indices`, `indptr`, `shape`, and `features/` with `id`, `name`, `feature_type`, `genome`), and as Matrix Market |
| `cells.zarr.zip` | Zarr format 2, zipped, as XOA 2 lays it out: `masks/0` (nuclei: label *p* + 1 is nucleus *p*) and `masks/1` (cells: label *k* + 1 is `cells.parquet`'s row *k*) as `uint32` labels on the image's pixels; `masks/homogeneous_transform` (4 × 4, µm to pixels); `cell_id`; `cell_summary`; and `polygon_sets/0` (nuclei) and `polygon_sets/1` (cells), each with its outlines' `vertices` and `num_vertices`, each polygon's `cell_index` and `method`; the root's attributes name the sets and the segmentation methods |
| `morphology_focus/morphology_focus_0000.ome.tif` | The DAPI stain of the nuclei in the section: a tiled, deflate-compressed OME-TIFF with its reduced levels as SubIFDs (Xenium's own are JPEG 2000) |
| `metrics_summary.csv`, `gene_panel.json` | The run's metrics, computed as SituCraft's validation recomputes them, and the panel: genes `SIM001`…, negative control probes `NegControlProbe_SIM01`… and codewords `NegControlCodeword_0500`… |
| `truth/` | A Xenium Ranger-shaped bundle of the same run: `experiment.xenium`; `cells.parquet` (every true cell: `cell_type`, `nucleus_in_section`, areas, centroid, Q20 gene transcripts), both boundary tables and `cells.zarr.zip` of the true cells (the nuclei in the section numbered in their cells' order, `cell_index` naming each one's cell); `transcripts.parquet`, the observed molecules row for row with their true `cell_id`, `true_x_location`, `true_y_location`, `true_z_location`, `origin` (`cell`, `background`, `control probe`, `control codeword`) and `wide_displacement` |

To score a segmentation against the truth, import `truth/` into the simulated dataset as a
Xenium Ranger bundle (Segmentation view, **Import a segmentation**) and compare the two.

Other tools read a simulated bundle as they read Xenium's: spatialdata-io's Xenium reader reads
its table, transcripts, labels, boundaries and image with no warning, and tifffile and pyarrow
its image and tables (`validation/reference/read_simulation.py` checks each against what the
simulation made).
