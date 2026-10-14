# Formats SituCraft reads and writes

What SituCraft 0.1.0 reads, what it writes, and how. Every format is read by SituCraft's own code;
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
| `artifacts` | Results. In 0.1.0, filters: a name, a version, its parent version, its conditions, and its result (the number of cells, how many it keeps, and a checksum of which) |
| `decisions` | Recorded choices, with their reasons (from 0.2.0) |
| `views` | Saved views (below) |
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
refused with a message to update. A project saved by 0.1.0 is kept in
`validation/projects/`, and every later release is checked to reopen it exactly.

A segmentation is recorded in `artifacts` (kind `segmentation`) as the artifact the program keeps:
its id, name, dataset, state, method (name, version, citation, and the cells each of the
instrument's methods segmented), software, the job that made or registered it, its parents, its
files (path, role, size, SHA-256, and `bundle` for a file of the dataset's bundle), its summary and
its check's findings. `decisions` records each promotion of a dataset's default segmentation:
`{kind: "promote-segmentation", dataset, artifact, rationale, by, time}`; the latest holds.

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
files of its own. Each binary file starts with four magic bytes, a format version (`uint32`) and its body's
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

These files are SituCraft's own; the open formats for exchange come later in wave 2.

### Comparisons

A comparison of two segmentations is an artifact of two files:

| File | Magic | Holds |
| --- | --- | --- |
| `comparison.json` | | `format` (`"situcraft-comparison"`), `version`, `method`; the two segmentations (`a`, `b`: artifact id and name); `params` (the correspondence's and co-expression's thresholds, the regions' size, the seed); each segmentation's measures (`metricsA`, `metricsB`); the differences by class (`classes`, and each side's cells in each: `cellsA`, `cellsB`); the transcripts that changed cell (`changes`, `changesQ20`: gained, lost, moved); `iou` and `displacement` of the cells the same in both; `pixelAgreement`; the co-expression reference (`coexpression`) and the MECR's gene pairs (`mecrPairs`); and the same counts by region (`regions`: squares of `sizeUm`, row-major). A measure not taken is `null` |
| `groups.bin` | `SCCG` | Each difference: its class (by its index in one-to-one, expanded, contracted, split, merged, rearranged, only in A, only in B, assignment-only), its cells in A and in B (indices into each segmentation's cells), the IoU, the transcripts' Jaccard and the boundary displacement (µm) of one cell in each, the gene transcripts whose cell changed, and where it lies (µm) |

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
time. They are not for exchange: open formats for exchange (SpatialData, AnnData, Parquet,
GeoJSON) arrive with 0.2.0.
