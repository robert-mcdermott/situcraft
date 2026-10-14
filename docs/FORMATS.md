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
| Zarr | Zarr storage specifications v2 and v3 | Directories and zips; Blosc, Zstandard, zlib, gzip and LZ4 chunks; C and F order; both byte orders; fill values; CRC-32C |
| TIFF | TIFF 6.0, BigTIFF, OME-TIFF | Strips and tiles, SubIFD pyramids, deflate, LZW, PackBits, predictors; OME-XML, multi-file OME-TIFF, z-stacks |
| JPEG 2000 | ITU-T T.800 (ISO/IEC 15444-1), Part 1 | JP2 files and codestreams; every progression order and code-block style; tiles, layers and precincts; 5/3 and 9/7 wavelets; decoding at reduced resolution |
| Zstandard | RFC 8878 | Frames with XXH64 checksums |
| LZ4, Snappy, Blosc | Their projects' format descriptions | Blocks; Blosc byte and bit shuffling |
| Matrix Market | NIST Matrix Market exchange format | Coordinate matrices, gzip-compressed |

## Other files

SituCraft recognizes OME-TIFF images, Parquet and CSV tables, GeoJSON polygons and Zarr stores
(on the command line or by **Open a bundle**) and lists them under **On this computer**. Reading them for analysis arrives with later releases: registered H&E and IF
images, imported segmentations, labels and annotations (see the
[roadmap](../situcraft-spec/roadmap.md)).

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
