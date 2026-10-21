# Formats SituCraft reads and writes

What SituCraft 0.2.0 reads, what it writes, and how. Every format is read by SituCraft's own code;
the program needs no other software. The research notes
([research.md](../situcraft-spec/research.md), section 1) give the details and their sources.

## Xenium output bundles

SituCraft reads bundles written by Xenium Onboard Analysis (XOA) and Xenium Ranger. XOA 2.x and
1.0 are supported and checked against real bundles (from 0.5.0, XOA 1.0's: its cells numbered
rather than named, its count matrix in its earlier Zarr layout, without the cells' IDs, and its
Parquet files as fastparquet 1.0 wrote them, the footer's row count wrong); other versions are read
best-effort, and the Data view says so. A bundle opens as its folder, its `experiment.xenium`, or a `.zip` holding the bundle at
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
| TIFF | TIFF 6.0, BigTIFF, OME-TIFF | Strips and tiles, SubIFD pyramids, deflate, LZW, PackBits, predictors; JPEG (TIFF Technical Note 2: shared tables, YCbCr or RGB, chroma interpolated as libjpeg-turbo does); OME-XML, multi-file OME-TIFF, z-stacks |
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

## Labels SituCraft imports

The Cell types view's **Labels made elsewhere** (and the `import_labels` remote action) reads cells'
labels made by another tool or an earlier analysis, for a segmentation's cells:

| Format | Files | What is read |
| --- | --- | --- |
| CSV or TSV | `.csv`, `.tsv`, `.txt`, gzipped or not; a header row, comma or tab separated | every column; a column of numbers wherever every value is one (or missing) |
| Parquet | `.parquet` (text, numbers and booleans; dictionary-encoded columns) | every column |
| AnnData Zarr | an AnnData object's store (`.zarr` folder or its zip, format 2 or 3), or a SpatialData store with a table | its `obs`: the index and every column (text, categorical, nullable, numbers) |
| Xenium Explorer cell groups | a CSV of `cell_id`, `group` and, optionally, `color` (`#rrggbb`, a group's given once) | the groups and their colors |
| The instrument's clusters | the bundle's `analysis.tar.gz` (or `analysis/`) | a clustering's barcodes and clusters, labeled `Instrument cluster N` by the instrument's numbers |

Four columns are chosen: the cells' **ids** (by default `cell_id`, `Cell ID`, `cell`, `barcode`,
`id`, an AnnData index, else the first column of distinct text), their **labels** (`group`,
`cell_type`, `celltype`, `label`, `annotation`, `cluster`, `leiden` …, else the first column of 2 to
1,000 repeated values), and optionally a **confidence** (numbers) and **colors**. Each row's id is
matched exactly to a cell's id in the segmentation (the bundle's own for the instrument's
segmentation; SituCraft's for its own, as its exports write them). A table naming cells the
segmentation does not have is refused, with how many and the first few; so is one naming a cell
twice, or holding more than 1,000 labels. Cells the table does not name, and rows whose label is
`""`, `NA`, `nan`, `None` or `null`, have no label. The import records the SHA-256 of the file
read (of an AnnData store, its `obs`'s files).

## References SituCraft reads

The Cell types view's **Labels from a reference** (and the `reference_labels` remote action) reads a
labeled single-cell reference, restricted to the clustering's panel:

| Format | Files | What is read |
| --- | --- | --- |
| AnnData Zarr | an AnnData object's store (`.zarr` folder or its zip, format 2 or 3) | the cells' labels from an `obs` column (2 to 500 labels); the genes' names from `var` (its index or a column); their expression from `layers/counts`, `raw/X` (named by `raw/var`) or `X` (sparse or dense): counts (whole numbers) or log-normalized values; scaled values (below 0) are refused |
| A table of profiles | `.csv`, `.tsv`, `.txt` (gzipped or not) or `.parquet` | a row per label (a column of labels, each gene a numeric column named for it) or a row per gene (a column of genes, each label a numeric column named for it): each label's mean expression, as given |

The usual names of label columns are suggested (`cell_type`, `celltype`, `cell_type_fine`,
`cell_ontology_class`, `ann_finest_level`, `label.main` …), and the gene column naming most of the
panel's genes. Genes are matched to the panel's by name, exactly, else ignoring case where that
names one panel gene. Counts are scaled to the reference's median total and log-transformed (log1p)
before each label's cells are averaged. The SHA-256 of the file read (of a store, of its files) is
recorded. An `.h5ad` file is read once anndata has written it as Zarr:

```python
import anndata
anndata.read_h5ad("reference.h5ad").write_zarr("reference.zarr")
```

`situcraft simulate --reference N <folder>` writes a reference of the simulated tissue's types,
N cells of each, as `<folder>-reference.zarr` (its types in `obs`'s `cell_type`, counts in `X`).

## Post-Xenium images SituCraft reads

An H&E or immunofluorescence image of a dataset's section, stained and scanned after the Xenium run
(from 0.5.0), is added in the Viewer (**Post-Xenium images**) or by the `add_image` remote action,
with its alignment or without one. It is read where it lies, never copied, a tile at a time.

| What | Form | What is read |
| --- | --- | --- |
| The image | A pyramidal, tiled OME-TIFF, as Xenium Explorer requires (10x Genomics' image conversion guide): its full-resolution image directory and its reduced levels in SubIFDs | RGB (an H&E: three samples a pixel, 8 bits, stored together), or one or more channels (an immunofluorescence image: a directory each, 8 or 16 bits, named by the OME-XML); tiles deflate (with or without a predictor), LZW, JPEG or JPEG 2000; the first z-plane and time point. A flat image larger than 4,096 pixels a side, or one in strips, is refused, with how to make a pyramid |
| Its alignment: the transformation matrix | `<name>_imagealignment.csv`, as Xenium Explorer saves it | A 3×3 affine matrix, row-major, its last row `0,0,1`: the image's pixels to the Xenium image's pixels (its scale is the image's pixel size over the Xenium image's) |
| Its alignment: the keypoints | `<name>_keypoints.csv`, as Xenium Explorer saves it | Columns `fixedX`, `fixedY` (the Xenium image's pixels) and `alignmentX`, `alignmentY` (the image's), a row each landmark; fitted by least squares as a similarity (translation, one scale, rotation and, where it fits better, a reflection), each keypoint's residual kept |

The alignment found beside the image (named after it, as 10x names them) is offered, and recorded
as the first version of the image's alignment; versions are also made from landmarks (below). An
image is placed by an alignment, its pixels to the Xenium image's, then in µm by the dataset's
pixel size; the pixel size its own metadata gives is recorded but never used to place it
(post-Xenium images are often written with the Xenium image's).

SituCraft writes any version of an alignment back in Xenium Explorer's convention, to load with
the image there: the transformation matrix (`<name>_imagealignment.csv`, from the version's
transform), and for a version made from landmarks or keypoints its keypoints (`<name>_keypoints.csv`,
their fixed positions in the Xenium image's pixels). Each number is written as short as reads back
the same; written and read again, a file gives the same transform.

## Regions SituCraft reads and writes (GeoJSON)

Regions drawn on a dataset's tissue (from 0.5.0) are written, and regions are read, as GeoJSON
(RFC 7946's objects) laid out as QuPath writes and reads its annotations (Bankhead et al. 2017), so
that a region drawn in one opens in the other: the Viewer's **Regions** (GeoJSON, as QuPath reads
and writes it), or the `export` (`what: "regions"`) and `import_regions` remote actions.

| What | Written | Read |
| --- | --- | --- |
| The document | A `FeatureCollection` | A `FeatureCollection`, an array of Features (as QuPath 0.2 writes them) or one `Feature` |
| Each region | A `Feature`: its `id` (a UUID-like id, the same for the same version of the same region), a `Polygon` (one part) or `MultiPolygon` (several), each ring closed, holes after their outer ring | `Polygon`, `MultiPolygon`, and the polygons of a `GeometryCollection`; points and lines are left out (they have no area), as are objects whose `objectType` (or `object_type`, or a QuPath 0.2 `id` of `PathDetectionObject` or `PathCellObject`) is not `annotation`. What is left out is counted and reported |
| Its properties | `objectType: "annotation"`, `name`, `classification` (`name` and `color`, `[r, g, b]`), `isLocked: false`, and `metadata` (`SituCraft region`: its id; `SituCraft version`: its version; `SituCraft by`: who made it) | `name`; `classification.name` (or QuPath's `names`, joined), its `color` or a packed `colorRGB` (QuPath 0.2 and 0.3); an unclassified annotation becomes a `Region*` |
| Its coordinates | Chosen when written | Chosen when read |

The coordinates are one of: **µm** (the dataset's coordinates); **the Xenium image's pixels**
(its full resolution, µm over the dataset's pixel size), as QuPath opens `morphology_focus`; or
**a post-Xenium image's pixels** (its full resolution, (0, 0) the corner of its first pixel), as
QuPath opens that image, through the version of its alignment in use: written, each point is taken
back through any deformation and the version's transform; read, each point is placed by them. When
the version deforms the image, each edge is first divided every 10 µm (or its pixels' worth), so
that its points follow the deformation. Coordinates are written to 0.001.

QuPath's own classes keep QuPath's colors: Tumor (200, 0, 0), Stroma (150, 200, 150), Immune
cells (160, 90, 160), Necrosis (50, 50, 50), Other (255, 200, 0), Region\* (0, 0, 180) and
Ignore\* (180, 180, 180); another class is given a color from its name, and a region read keeps
the color its file gives.

## Other files

SituCraft recognizes OME-TIFF images, Parquet and CSV tables, GeoJSON polygons and Zarr stores
(on the command line or by **Open a bundle**) and lists them under **On this computer**. A GeoJSON
file of regions is read into a dataset from the Viewer's **Regions** (above).

## What SituCraft writes

### Project documents

A project is one JSON document, saved automatically in the library (`<data dir>/projects/`):

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-project"`, `1` |
| `id`, `name`, `created`, `modified` | Its identity |
| `datasets` | Each bundle: its place, run and analysis identifiers, run, slide and Xenium region names, panel (its name and design), tissue, counts, pixel size, channels, a fingerprint of its files (each file's size; the SHA-256 of its manifest, metrics and panel), and its validation summary and findings |
| `artifacts` | Results: filters (a name, a version, its parent version, its conditions, and its result: the number of cells, how many it keeps, and a checksum of which), from 0.2.0 segmentations and comparisons, and from 0.4.0 clusterings, their names and maps, marker sets, label sets, and curated and consensus labels (below) |
| `decisions` | Recorded choices, with their reasons (from 0.2.0) |
| `views` | Saved views (below) |
| `exports` | The exports written from its segmentations (from 0.2.0; absent until the first) |
| `regions` | The regions drawn on its datasets, each with its versions (from 0.5.0; absent until the first; below) |
| `reviews` | The reviews of its cells and regions, appended and never changed (from 0.5.0; absent until the first; below) |
| `samples`, `sampleFields` | What is known of its samples, and the fields that say it (from 0.7.0; absent until the first; below) |
| `figures` | Its figures (from 0.8.0; absent until the first; below) |
| `history` | Every change, with its time |

A filter's checksum is the SHA-256 of the cell count (32-bit unsigned, little-endian) followed by
one bit per cell, in the order of `cells.parquet`, the first cell in the lowest bit of the first
byte, set when the cell is kept. Anyone can recompute it from the bundle and the conditions.

From 0.9.0 a filter may instead hold a `tree` of conditions (`all`, `any` and `not` of conditions on
a cell's attributes, gene counts, labels, clusters, niches, regions, distances from them, a box,
and other saved filters, as [MCP](MCP.md#asking-which-cells) lists them) and `text`, the tree in
words; SituCraft applies it to the instrument's segmentation's cells, the result recorded the same
way. A filter of `conditions` reads as the tree `{"all": [...]}` of them.

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
refused with a message to update. A project saved by each release (0.1.0, 0.2.0, 0.3.0, 0.4.0) is
kept in `validation/projects/`, and every later release is checked to reopen it exactly and, from
0.2.0's, to make its artifacts again with the same checksums (a clustering, a map and a label set
from markers, from 0.4.0's, with the same summary).

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

`regions` records each region drawn on a dataset (from 0.5.0): `{id, dataset, versions}`, its
versions appended and never removed, the latest the region. A version is `{name, cls, color,
geometry, tool, layer, by, time, note}`, and `removed: true` when it removed the region (a later
version may return it). `geometry` is a multipolygon in µm (the dataset's coordinates): a list of
polygons, each a list of rings (its outer ring, then its holes), each ring a list of `[x, y]` that
does not repeat its first point. `tool` is how it was made (`rectangle`, `polygon`, `freehand`,
`brush` or `imported`), `layer` what it was drawn over. No two of a dataset's regions (those not
removed) have the same name.

`samples` records what is known of the project's samples (from 0.7.0). Each dataset is a sample
until regions of it are made its samples instead; a sample nothing was said of has no record.
A record is `{id, dataset, region, name, fields}`: `id` is `s-` and the dataset's id (or the
region's, for a region sample), `region` the region's id or `null` for the whole dataset, `name`
present only when one was given (else the dataset's or region's), and `fields` the sample's values
by field name (text, or a number for a number field; a missing value is absent, never zero). A
dataset with region samples is not a sample itself; its own record is kept for when it is again.
`sampleFields` lists the fields in order, `[{name, kind}]` with `kind` `text` or `number`, and is
absent while they are the first two, `subject` and `condition`. The run, slide, Xenium region,
panel and software of a sample are read from its dataset and not stored again.

A design (from 0.7.0) is recorded in `artifacts` as kind `design`: `{id, name, version, parent,
created, spec, samples, excluded, result}`, each saved design a new version of its name. `spec` is
what was asked: `unit` (the replicate unit's field), `group` (the field compared), `reference`,
`contrasts` (`[a, b]` pairs, each a − b; empty for each level against the reference),
`covariates` (fields, or `run`, `slide`, `panel`) and `exclude` (samples left out: `{sample id:
reason}`). `samples` holds the samples as they were: each one's `id`, `name`, `dataset`, `region`,
`unit`, `group`, covariate `values` and `row`; `excluded` each sample left out, `{id, name,
reason}`. `result` is the design: `levels` (the reference first), `reference`, `paired`, `rows`
(each a unit in a group, `{unit, group, samples}`, a unit's samples in one group combined),
`names` (the columns: `Intercept`, `field[T.level]` for each factor level but the reference, a
number field by its name; factors before numbers), `matrix` (a row for each of `rows`), `rank`,
`df` (the residual degrees of freedom), `contrasts` (`{name, a, b, vector}`, the vector over the
columns), `words` and `notes`. Only a design that can be estimated is saved.

The samples' table, written and read as CSV, has a row for each sample: `sample` (its name),
`dataset`, `region` (its name, or empty), the fields in order, then the bundle's `run`, `slide`,
`xenium_region`, `panel` and `software` (written for reference, never read). Text is quoted when
it holds a comma, a quote, a line break or space at either end; numbers are written so that they
read back exactly.

`reviews` records each review of a dataset's cells, clusters or regions (from 0.5.0), appended and
never changed or removed (taking one back is another review):

| Field | Holds |
| --- | --- |
| `id`, `time`, `by` | The review's id (`rev-…`), when, and who (the reviewer's name or initials) |
| `action` | `accept`, `relabel`, `ambiguous`, `exclude`, `include` (an exclusion taken back), `concern` (its segmentation looks wrong), `resolve` (the concern answered) or `comment` |
| `dataset`, `target` | The dataset, and what was reviewed: `cells`, `clusters` or `region` |
| `segmentation` | `{id, name}`: the segmentation the cells are of (for a region, the one drawn) |
| `cells` | Each cell reviewed: `{row, id, label, confidence, previous}`: its row and ID in the segmentation, its label in the set reviewed and that label's confidence (as shown; `null` for none), and its value before (its label, `included` or `excluded`, its concern or `none`) |
| `clusters` | `{clustering, clusters}`: a clustering's clusters reviewed (numbered from 0) |
| `region` | `{id, name, version, cls}`: the region and its version and class after the review |
| `labels` | `{id, level, name, kind}`: the label set reviewed (`markers`, `reference`, `imported`, `curated`) |
| `previous`, `next` | The value before (`mixed` when the cells' differ) and after: a label or class, `ambiguous`, `excluded`, `included`, a concern or `none` |
| `alternatives`, `concern`, `note` | The labels an ambiguous one could be; the concern (`merged`, `split`, `boundary`, `not-a-cell`, `missed`, `other`); why, or the comment |
| `images` | Each post-Xenium image of the dataset: `{image, name, transform, version, shown}`, the version of its alignment placing it and whether it was shown (`null` when not known) |
| `edits` | Accepting or relabeling cells: the edits appended to the curated set (`{set, index}`), each naming the review back (`review`) |

Relabeling a region appends a version of it with the new class. Curated edits made before 0.5.0
(or without a review) are read as reviews, their label before, cells' IDs and alignments unknown.

`decisions` records each decision about a dataset's segmentations, appended and never removed:
`{kind, dataset, artifact, rationale, by, time, comparison}`, the kind `promote-segmentation` (its
default), `keep-segmentation` (kept for exploration) or `reject-segmentation`, and `comparison`
the comparison it rests on, when there is one. A segmentation's status is its latest decision; the
default is the segmentation of the latest promotion not rejected since.

`figures` lists the project's figures (from 0.8.0), each `{id, name, size, width, height, text,
letters, panels, created}`: `size` one of `single`, `onehalf`, `double` or `slide`; `width` and
`height` the page in points (1/72 inch); `text` its text size in points (6 to 36); `letters` whether
its panels are lettered. Each panel is `{id, kind, x, y, w, h}`, its place on the page in points,
and what it shows: a `tissue` panel's `view` (a saved view's id), `region` (`{cx, cy, width}`, its
center and width in µm), and optionally `scaleBar` (µm), `pointSize` (pt) and `regions` (`true`
for all the dataset's regions, or a list of their ids); a `qc` panel's `dataset`, `metric`
(`transcripts`, `total`, `area` or `nucleusArea`), and optionally `log`, `filter` (a filter's id,
whose thresholds are drawn) and `segmentation`; a `map` panel's `map` (a map's id) and `color`
(`clusters`, or `labels:<label set id>:<level>`); a `spatial` panel's `analysis` (an enrichment's,
nearest distances' or Moran's I's id); a `cohort` panel's `comparison` (a comparison of abundance,
expression or spatial statistics), `plot` (`forest`, `units`, `volcano` or `gene`) and optionally
`family`, `label`, `gene`, `contrast` and `key` (a spatial statistic, as `a|b`); a `text` panel's
`text`, `size`, `bold` and `align`. Any panel may have a `title`. A panel with `letter: false` is
not lettered. A figure holds no
data: it is drawn from the project whenever it is shown or written.

### Figures

A figure is written as SVG, PDF (1.4; its text in Helvetica, the stains one image) or PNG (8-bit
RGBA, its resolution in a `pHYs` chunk, 300 dpi by default), each carrying the figure's manifest,
or as the manifest alone (JSON):

- in an SVG, the element `<metadata id="situcraft-figure">`, the JSON in CDATA, first in the
  document;
- in a PDF, an attached file `situcraft-figure.json` (`application/json`, stored uncompressed);
- in a PNG, an `iTXt` chunk, keyword `situcraft-figure`, uncompressed UTF-8, before `IEND`.

| Manifest field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-figure-manifest"`, `1` |
| `created` | When it was written (ISO 8601) |
| `software` | `{name, version, commit}`: the SituCraft that wrote it |
| `project` | `{id, name}` |
| `figure` | The figure as the project holds it (above) |
| `description` | The figure in words, panel by panel |
| `panels` | Each `{id, letter, kind, shows, drawing, inputs, facts}`: `shows` in a few words; `drawing` the SHA-256 of the panel's marks written as SVG, each image in it stood in for by its size and the SHA-256 of its RGBA pixels (so that it does not depend on how a PNG is compressed); `inputs` what it was drawn from (a tissue panel's `dataset`: `{id, name, run, experimentUuid, fingerprint}`, the fingerprint the SHA-256 of the dataset's file fingerprint as canonical JSON; `view`: `{id, name, made}`, `made` the SHA-256 of the saved view as canonical JSON; `region`; `artifacts`: the segmentation, label set or clustering drawn, each post-Xenium image shown and the alignment in use, `{id, kind, name, files?}`; a result panel's `dataset` and `artifacts`, the analysis, map or comparison it draws and what colors it); `facts` what it drew (a tissue panel's `region` `{x0, y0, x1, y1}` in µm, `umPerPt`, `scaleBar` `{um, pt}`, `cells`, `transcripts` and `regions` drawn, `raster` `{width, height, level, images?}` of its stains or null, each image `{id, name, transform, level, covered}`; a result panel's `chart` and what it counted, such as a distribution's `counts`) |
| `drawing` | The SHA-256 of the panels' drawings' list as canonical JSON |
| `fingerprint` | The SHA-256 of everything else as canonical JSON (every object's keys sorted): a manifest edited after it was written does not match it |

Canonical JSON is JSON with every object's keys sorted and nothing undefined; the SHA-256 is of its
UTF-8 bytes. The same figure written at the same time (`write_figure`'s `time`) is the same bytes in
SVG and PDF.

### What made a result

The record of a result (any analysis, comparison, summary, label set, clustering, map, graph,
segmentation, alignment, filter or figure), as the Reports view shows it and remote control's
`record` answers it, read from the project and the artifacts' manifests (their parents and their
jobs' inputs). Written as JSON:

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-record"`, `1` |
| `result` | `{id, kind, name}` |
| `program` | `{name, version, commit?}`: the SituCraft that wrote the record |
| `recorded` | When (ISO 8601) |
| `steps` | Each step from the bundles to the result, each after the steps it was made from: `{n, id, kind, name, made, method: {name, version}, parameters, seeds, software: [{name, version, commit?}], references (numbers), inputs (the numbers of the steps it was made from), decisions: [{kind, rationale, by, time}], dataset? (its dataset's name), note?, by?}` |
| `software` | Every program the steps ran, each `{name, version}` once |
| `references` | `{n, key, text, doi?, url?}`, numbered as first cited: `key` the reference's in SituCraft's list (`web/lib/references.js`; `situcraft` for SituCraft itself), or null for a citation SituCraft does not list (a marker set's own, an import's), kept as written |

A step's `kind` is `dataset` (a bundle), `image`, `transform`, `alignment-check`, `region`, `filter`,
`segmentation`, `comparison`, `clustering`, `cluster-names`, `embedding`, `marker-set`, `labels`,
`curated-labels`, `consensus-labels`, `graph`, `spatial`, `view`, `design`, `cohort-summary`,
`abundance`, `expression`, `spatial-comparison` or `figure`. As Markdown, the steps of the same
kind, method, parameters, software and references (each sample's segmentation, say) are written
once, naming each item, followed by the software and the references in Vancouver style. As BibTeX,
each reference is an entry under its key (`@article`, `@book`, `@inproceedings`, `@misc` for a
preprint, `@manual`, `@software` for SituCraft), and one SituCraft does not list a `@misc` under
`ref<n>` with the citation as its note. Each DOI in SituCraft's list is checked against a recorded
copy of Crossref's record of it (`validation/reference/crossref/crossref.json`).

### Certificates

A certificate (Reports, **Certificate**; remote control's `export` with `what: certificate`) is a
zip of what a result, a dataset's results or the whole project rests on and what it reports. It has
no time of its own and keeps no path of the computer that made it: the same project and SituCraft
give the same bytes. A bundle's folder is written `situcraft:bundle/<dataset ID>` (a file in it
with its path in the bundle after it), any other absolute path `situcraft:file/<its name>`.

| Entry | Holds |
| --- | --- |
| `README.txt` | What it is of, its fingerprint, what it proves, how to check it (`situcraft verify`), what is not made again |
| `certificate.json` | What was certified (below) |
| `project.json` | The project as it was |
| `record.json`, `record.md` | Each step from the bundles to the result ([What made a result](#what-made-a-result)) |
| `methods.md`, `references.bib` | The methods, written from the record, and their references |
| `artifacts/<ID>/manifest.json` | Each step SituCraft's jobs made: its manifest ([Artifacts](#artifacts)) |
| `manifest.json` | Every other entry with its size and SHA-256 (`situcraft-certificate-files` 1) |

| `certificate.json` field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-certificate"`, `1` |
| `software` | `{name, version, commit}`: the SituCraft that certified it |
| `subject` | What it is of: `{id, kind, name}` (a result's kind, or `project`, `dataset`, `report`: the results a report shows) |
| `project` | `{id, name, modified, path, sha256}`: the project as it was, `project.json` |
| `record`, `methods` | `{path, sha256}` of `record.json` and `methods.md` |
| `bundles` | Each `{dataset, name, run, region, analysisUuid, experimentUuid, software, panel, cells, transcripts, files}`: `files` each `{path, size, sha256}`, those the dataset's fingerprint records and those the instrument's segmentation read |
| `steps` | Each step of the record, in its order: `{n, id, kind, name, inputs, method, parameters, seeds, software, references, decisions, by}`; `by` is `instrument` (a bundle, with `bundle`, its dataset), `program` (with `job`: `{kind, spec}`, the spec's digest; `manifest`: `{path, sha256}`; `files`: each `{path, role, size, sha256, location}`; `content`, for a clustering, a map or labels, the SHA-256 of its content without how long its run took, which its description also records; `summary`: the numbers it reports) or `page` (with `record`, its record's digest; `summary`; a figure's `drawing`: `{drawing, panels: [{id, letter, drawing}]}`, its manifest's digests, and in a report's, `svg`: the SHA-256 of the SVG the report shows) |
| `notMadeAgain` | Each step not made again in checking it, and why: `{step, what, why}` (the bundles and the instrument's segmentation, checked by their files' SHA-256; an import, by the files read; a person's work, by its digest) |
| `leftOut` | A report's or a package's only: `{people, notes, names}`, how many of each it leaves out ([Reports](#reports)); a step of SituCraft's whose job was given names left out has `redacted: true` and is not made again from it |
| `fingerprint` | The SHA-256 of everything else as canonical JSON: cite it with the result |

A digest is the SHA-256 of a value's canonical JSON (every object's keys sorted), with this
computer's paths stood in for as above.

### Reports

A report (Reports, **A report**; remote control's `export` with `what: report`) is one HTML file of
results chosen, read in any browser, offline: it loads nothing from the network (its
Content-Security-Policy is `default-src 'none'`), and the same project and choice give the same
bytes.

| Part | Holds |
| --- | --- |
| Figures | Each figure chosen, as an SVG with its manifest inside ([Figures](#figures)), in an `<img data-figure="<ID>">` |
| Results | Each other result's numbers, from its step's `summary` in the certificate: each in a `<td data-pointer data-value>`, `data-pointer` its place in `certificate.json` (a JSON pointer, RFC 6901) and `data-value` its value as JSON |
| Methods, References | The methods, written from the record, each paragraph a `<p class="methods-paragraph">`, and their references |
| The certificate | The certificate of the results ([Certificates](#certificates); its subject's kind `report`), base64 in `<script type="application/zip" id="situcraft-certificate">` |

As it opens, the report checks itself: the certificate's fingerprint and the SHA-256 of each file
it names; each number, the value at its place in `certificate.json`, its text that value as the
report writes it (whole numbers with commas; others to 4 significant digits, below 0.001 and from
a million up to 3 and a power of ten); each figure's SVG, the SHA-256 its step's `drawing.svg`
records; the methods' paragraphs, those of `methods.md`. It says what it found at the top, marks a
number that differs, and sets `data-checked` on `<html>` to `intact`, `differs` or `error`.
`situcraft verify` takes a report as it takes a certificate, and checks it the same way.

A report, and a package, leave out what this computer and the people using it say of themselves,
before anything is drawn or certified, so that its figures, record, methods and certificate all
say the same:

| Left out | As |
| --- | --- |
| Paths of this computer | `situcraft:bundle/<dataset ID>[/<path in the bundle>]`, `situcraft:file/<its name>`, as in a certificate |
| People's names (who reviewed, drew, decided, curated) | `Person 1`, `Person 2`, … in the order the project first names them, wherever named |
| Notes people wrote on reviews and regions | Emptied, and `(a note, left out)` wherever their text is repeated |
| The samples', subjects' and sections' names, when asked | `Sample 1`, `Subject 1`, `Section 1`, … wherever named as a word |

Every text it holds (its files', a zip's entries, the SVG figures and the certificate inside its
HTML) is then searched for each path, its home folder, each person's name, each note and each
name left out; it is not written if any is found.

### Result packages

A result package (Reports, **Write a package**; remote control's `export` with `what: package`)
is a folder of static files, or a zip of one, that opens in any browser without SituCraft and
asks the network for nothing. It leaves out what a report does.

| File | Holds |
| --- | --- |
| `index.html` | The report ([Reports](#reports)), with a Views section linking each view |
| `views/<n>-<name>.html` | Each saved view its figures' tissue panels show, in a reader of its own: drag to move, scroll or + and − to zoom, its scale bar and legend |
| `views/<n>-<name>/<level>/<x>-<y>.jpg` | The view's tissue drawn in advance, as its figure's tissue panel draws it, as JPEG tiles 512 pixels a side: level 0 at the view's own scale, each next level at half its µm a pixel, to the image's full resolution or two levels finer, 64 tiles at most |
| `certificate-<fingerprint>.zip` | The certificate, as the report carries it |
| `README.txt` | What it is, what it leaves out and what was searched, how to check it |
| `manifest.json` | `format` `"situcraft-package"`, `version` 1, `title`, `software`, `certificate` (`{fingerprint, file, subject}`), `views` (each `{name, file, tiles, levels}`), `leftOut` (`{people, notes, names}`), `checked` (`{texts, strings}`), and `files`: every other file `{path, size, sha256}` |

The views are drawn from the project, as figures are; the certificate does not hold them.
`situcraft verify` takes a package's folder, or its zip, as it takes its report.

### Artifacts

What jobs make and register is kept in `<data dir>/artifacts/<id>/`, beside a `manifest.json`:

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-artifact"`, `1` |
| `id`, `kind`, `name`, `state`, `created` | Its identity; `state` is `complete` or `invalid` |
| `lineage` | `parents` (a bundle by its analysis UUID and manifest's SHA-256, or other artifacts), `job` (its id, kind and spec), `method` (name, version, citation, detail), `parameters`, `software` (each program's name, version and commit) |
| `files` | Each file's `path`, `role`, `size` and `sha256`; `location: "bundle"` for a file of the dataset's bundle, named by its path there and never copied; `location: "place"` for a file where it lies on the computer (a post-Xenium image and its alignment), named by its absolute path and never copied |
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
- **A region of more than 4 million modeled transcripts** (twice `area.patch_transcripts`, 2
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
  patch's own rectangle, when there are patches; `min_qv`, the quality modeled, and `counts_min_quality`
  (20); `start` and `start_stages`, each region's first stage; `preset` and `presets_version`; and
  `second_seed`, when a second run checked the first.

| File | Magic | Holds |
| --- | --- | --- |
| `probabilities.bin` | `SCPR` | Each transcript's posterior probability of its cell (`float32`, in the row order of `transcripts.parquet`): the share of the summarized sweeps it lay in that cell; NaN for a transcript assigned by place |
| `certainty.bin` | `SCBC` | Each cell's boundary certainty (`float32`, in `cells.bin`'s order): the mean, over its pixels, of the share of the summarized sweeps each was its; NaN for a cell held or outside the regions refined. A cell is flagged uncertain below 0.75 |
| `diagnostics.json` | | `summary` (the sweeps and samples; the split-half agreement, on every eighth transcript; the mean assignment probability and the share below 0.5; the log-likelihood's drift, Geweke's z and `gewekeJudged`, false when fewer than 100 sweeps were summarized; the voxels moved per sweep at the end; the cells refined, held and flagged; the regions, and `patches`, the patches they were refined in; the transcripts modeled; the background: `falseDecodeShare` and `ambientShare`, the shares of the modeled transcripts its rates account for (their mean over the summarized sweeps), `negativeControls`, the negative controls' transcripts in the regions that measure the false decodes, and `inBackgroundShare`, the share the summary leaves in no cell; `seedAgreement`, with a second seed: the share of transcripts either run put in a refined cell that both put in the same cell), `limitations` (the cells, by id, with fewer than 10 gene transcripts, uncertain boundaries, or no nucleus, and the instrument's cells left out; the thresholds) and `regions` (each region or patch refined: its rectangle, a patch's own rectangle as `patch`, its own `summary`, and its `trace`, each sweep's stage, voxels moved, transcripts repositioned and log-likelihood). Over several regions, the summary's Geweke's z is the one farthest from 0 |
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
| `clustering.json` | | `format` (`"situcraft-clustering"`), `version`, `method`; the `segmentation` (artifact id and name); `parameters` (`minTranscripts`, `components`, `neighbors`, `resolution`, `seed`); `fixed` (the normalization and its `targetSum`, the median total; `clip`; the metric, graph, quality and Leiden's `randomness`; the markers' test); `cells` (the segmentation's, those `analyzed`, `excluded` by the rule or the `filter` with its id, name and checksum, `withoutTranscripts`); `genes` (`count`, `notDetected`, `medianCellsExpressing`); `pca` (`components`, each one's `variance` and `varianceRatio`, `totalVariance`; for panels of more than 1,000 genes, the `solver`, the Lanczos method); `graph` (`neighbors`, `edges`; for more than 50,000 cells, the `search`: its `method`, NN-Descent, the `width` searched, its sample rate `rho`, stop `delta` and `rounds`, and its `recall` against exact search over `queries` cells); `clusters` (`count`, `sizes` from the largest, `modularity`, `iterations`); `top`, each cluster's first ten markers (`gene`, `score`, `logfc`, `padj`, `pctIn`, `pctOut`); `seconds`, the run's timings |
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
| `embedding.json` | | `format` (`"situcraft-embedding"`), `version`, `method`; the `segmentation` and the `clustering` (artifact id and name); `parameters` (`minDist`, `spread`, `epochs`, 0 for the default, `negativeRate`, `seed`); `fitted` (the curve's `a` and `b`, the `epochs` run, the `edges` optimized, the `init`ial layout; for more than 50,000 cells, the `optimizer`, in batches over every core); `cells`; `quality` (`k`, `kWide`, `trustworthiness` over `queries` cells, the mean `precision` and `recall`; for more than 50,000 cells, the `search` the wider neighborhoods were found by, as `graph.search` above); `seconds`. A map of the instrument's has a `source` (`tool`, `version`, `file`, the `space` its quality is judged in) instead of `parameters` and `fitted` |
| `embedding.bin` | `SCEB` | The cells (u32 count; u32 indices into the segmentation's cells, increasing), each one's place (f32 × 2, interleaved) and its reliability score (f32, 0 to 1: its neighborhood precision and recall, averaged over it and its 15 nearest on the map) |

The project records each map it uses (kind `embedding`, with its segmentation and clustering).

### Graphs

A graph of a segmentation's cells (from 0.6.0) is an artifact of three files. The binary ones have
the envelope above; arrays are little-endian.

| File | Magic | Holds |
| --- | --- | --- |
| `graph.json` | | `format` (`"situcraft-graph"`), `version`, `method`, `methodVersion`; the `segmentation` (artifact id and name); `parameters`: the `kind` (`delaunay`, `knn`, `radius`, `contact`) and its parameter (`longest`, a Delaunay graph's longest edge, µm, 0 for every edge; `k`; `radius`, µm; `gap`, a contact graph's, µm); `cells`: the segmentation's (`segmentation`), those in the graph (`inGraph`), and how they were chosen: the `checksum` of the cells (the SHA-256 of their count, u32, then a bit a cell, as a QC filter's), the QC `filter` (id, name, checksum), the `regions` (id, name, version), and the cells a review `excluded`, left out; `edges`, each counted once; `degree` (`mean`, `median`, `max`, the `histogram` of cells by degree from 0); `isolated`, cells without a neighbor; `components` (`count`, the `largest`'s cells); `lengths`, the edges' lengths in µm (`min`, `q05`, `q25`, `median`, `q75`, `q95`, `max`, `mean`, and a `histogram` of bins of `step` from 0 to the 99th percentile, larger lengths in the last); in a contact graph, `shared`, the boundaries shared, alike; `duplicates`, cells at the same place as another (Delaunay); `collinear`, every cell on one line (Delaunay) |
| `cells.bin` | `SCGC` | The cells in the graph (u32 count; u32 indices into the segmentation's cells, increasing), then their centroids (f32 x, then f32 y, µm) |
| `edges.bin` | `SCGE` | The cells n and the edge ends m (u32, u32: each edge listed from both of its cells), whether it holds shared boundaries (a byte, 1 in a contact graph); each cell's neighbors in compressed rows: the offsets (u32, n + 1), the neighbors (u32 indices among the graph's cells, increasing within a cell, m) and each edge end's length (f32, µm, the distance between the two centroids) and, in a contact graph, the boundary shared (f32, µm: the pixel edges between the two cells in the label raster times the pixel's side) |

A graph is named by its segmentation, the cells' checksum, the method's version and its parameters,
so the same graph is never built twice. The project records each graph it uses (kind `graph`, with
its dataset, segmentation, parameters and summary). The Viewer's cells layer names the graph drawn
(`graph`, an artifact id); saved views keep it.

### Spatial analyses

A spatial analysis of a segmentation's labeled cells (from 0.6.0) is an artifact of two files, and
Moran's I of three:

| File | Magic | Holds |
| --- | --- | --- |
| `analysis.json` | | `format` (`"situcraft-spatial-analysis"`), `version`, `method`, `methodVersion`; the `question` (`enrichment`, `cooccurrence`, `distances`, `ripley`, `regions`, `boundary`, `niches`, `moran`); the `segmentation` (artifact id and name); for an enrichment and Moran's I, the `graph` (id, name, parameters); `cells`: the segmentation's (`segmentation`), those `analyzed` (the graph's, or those chosen, with how, as a graph records them) and of them `labeled`; `labels` (empty for Moran's I, which is of genes): the set's `id`, `kind` (`labels`, `curated`, `consensus`), `name` and `level`, the `names` and `colors` of the labels some cell has (in the set's order), and the `checksum` of each cell's label as sent; for an enrichment within regions, the `strata` (the `regions`, each id, name and version, and the checksum of each cell's region); the `parameters` (an enrichment's `permutations`, `seed` and `weight`, `edges` or `boundary`; a co-occurrence's `distances` in µm and `pairs`, `within` or `between`; nearest distances' `permutations`, `seed`, `reach` and `step`, µm; Ripley's K's `radii`, `correction`, `isotropic` or `none`, `permutations` and `seed`; regions compared's `level` of the intervals; a profile across an edge's `reach`, `step` and `margin`, µm, and `level`; niches' window, `neighbors` (the nearest labeled cells) or `radius` (µm), one of them 0, the `niches`, k-means's `starts` and `seed`, and the further `seeds`; Moran's I's `weights`, `row` or `binary`, `permutations` (0 for the analytic test), `seed`, and the `local` genes and their `localPermutations`); for Ripley's K, regions compared and a profile across an edge, the `window` (`kind`, `tissue` or, for Ripley's K, `region`; a region's `region`, its id, name and version; the tissue's `closing` radius and `grid`, µm; its `area`, µm², and boundary `segments`); and the results. An enrichment's (`enrichment`), K × K matrices, row-major, `[a·K + b]`: the `cells` of each label, the `edges` between labeled cells, the `strata`, the directed `count` (each label's cells' neighbors of the other label, an edge within a label counting from both ends; or the boundary shared, µm), the exact `expected` count, the permutations' `mean` and `sd`, `z`, the two-sided `p` and the Benjamini–Hochberg adjusted `padj` (`null` for a pair with a label no cell has). A co-occurrence's (`cooccurrence`): its `distances` and `pairs`, the `cells` of each label, and at each distance the ordered pair `count` and the `ratio`, K × K (`null` where a label has no pair). Nearest distances' (`nearest`): the `reach`, the `distances` the distribution is at, the `cells` of each label; by ordered pair (from the row's label to the column's nearest), K × K: the cells with none within the reach (`beyond`), the `median` and `mean` distance (at most the reach), the permutations' mean and standard deviation of the median (`permMean`, `permSd`), `z`, `p` and `padj`; and the distribution, (K × K) × D: the share of the row's cells whose nearest lies within each distance (`cdf`), the permutations' mean (`meanCdf`) and their 2.5% and 97.5% (`lo`, `hi`). Ripley's K's (`ripley`): the `radii`, `correction`, window `area`, `cells` of each label inside it, the `pairs` within the farthest radius and the cells `outside` it; by ordered pair, (K × K) × R: `k`, `l` (√(K/π)), the permutations' `mean` L and its 2.5% and 97.5% (`lo`, `hi`); and K × K, the global test's `p` and `padj`. Regions compared (`regions`): the `regions` (each id, name and version), then by row (each region, then the tissue in none of them: R rows), the tissue's `area` within it (µm²), its `labeled` and `all` cells; by row and label, R × K: the `count`, the `density` (cells per mm² of tissue) with Garwood's interval (`densityLo`, `densityHi`) and the `share` of the row's labeled cells with Wilson's (`shareLo`, `shareHi`); by ordered pair of rows and label, R × R × K, the first row's over the second's: `densityRatio` with its exact conditional interval and `shareRatio` with Koopman's (each `Lo`, `Hi`); and the labeled cells `outside` the tissue window. A profile across an edge (`boundary`): the `region`, the bands' `edges` (signed distance, µm, inside negative; bands below the first, between each pair and from the last on: B = edges + 1); by band, the tissue's `area`, `labeled` and `all` cells; by band and label, B × K: `count`, `density`, `densityLo`, `densityHi`, `share`, `shareLo`, `shareHi`; by zone (the core, the margin, around), `zoneArea` and `zoneLabeled`, and by zone and label, 3 × K, `zoneCount`, `zoneDensity`, `zoneLo`, `zoneHi`; by label, K × 3, the core over around, the margin over the core and the margin over around (`ratio`, `ratioLo`, `ratioHi`); each label's reading, `inside` (1 denser in the core than around, −1 sparser, 0 neither shown) and `atMargin`; and the cells `outside` the tissue window. Niches (`niches`), numbered by size, the largest first: their number (`niches`), the cells with a neighborhood (`windows`); by niche, its `cells`, and N × K the mean composition of its cells' neighborhoods (`composition`, its k-means center) and its labeled cells by label (`counts`); each label's share of the labeled cells (`tissue`); each niche's `defining` labels (indices: those at least 1.5 times their share in the tissue and 5% of its neighborhoods, by that ratio, up to three; else the commonest); k-means's `inertia` and each start's (`inertias`); the adjusted Rand index with each further seed's niches (`agreement`); each niche's leading genes against the other niches' cells (`markers`: gene, score, logfc, padj, pctIn, pctOut, as a clustering's) from the cells with a gene transcript (`markerCells`). Moran's I (`moran`): the `genes` (the segmentation's), the `weights`, the `cells`, graph `edges`, cells without a neighbor (`islands`, weighing nothing) and without a transcript (`noTranscript`, zeros), the normalization's `target` total (each cell's counts scaled to it, log1p), the weights' sums `s0`, `s1`, `s2` and the `expected` I (−1 / (n − 1)); by gene, `i`, its variance under normality (`varNorm`) and randomization (`varRand`), randomization's `z` and two-sided `p`, the permutations' mean and deviation of I (`permMean`, `permSd`) and their p-value (`permP`; `null` without permutations), the adjusted p (`padj`, Benjamini–Hochberg of the permutations' p when run, else of `p`), the `mean` value and the cells expressing it (`detected`); and the genes (indices) whose local Moran's I is in `local.bin` (`local`). An upper limit beyond any finite one, and a ratio with no cell in its denominator, are `null` |
| `cells.bin` | `SCSA` | The cells analyzed (u32 count; u32 indices into the segmentation's cells, increasing), each one's label (i32 into `labels.names`, −1 for none) and a number by question (i32): for an enrichment its region (0 in none of the strata's regions, k in the kth); for regions compared the regions holding it (bit k − 1 for the kth; 2³⁰ outside the tissue window); across an edge its band + 1 (0 outside the tissue window); for niches its niche + 1 (0 without a labeled cell in its neighborhood); for the others 0 |
| `local.bin` | `SCSL` | Moran's I only: the cells' (u32) and the genes' number (u32), the genes (u32 indices into `moran.genes`); then each gene's local Moran's I for every cell (f32), their conditional permutation p-values (f32), and their classes (a byte each: 0 none, 1 high among high, 2 low among low, 3 high among low, 4 low among high, where p is below 0.05) |

An analysis is named by its question, segmentation, graph (or the cells' checksum), the labels'
checksum and names, the regions' checksum, its window, the regions compared or crossed (their
versions and polygons), the method's version and its parameters, so asking again answers it. The project records each analysis it uses (kind `spatial`, with its dataset,
segmentation, graph, question, parameters and summary: the pairs most enriched and most depleted); one made again on another segmentation (from 0.6.0) records the analysis it was made from (`remakeOf`, its id) and the comparison of the two segmentations (`comparison`, its id), through which its numbers are read with that analysis's. Niches are a label set too: a fill `labels:<their analysis's id>:0` colors each cell by its niche.

### Cohort summaries

A cohort summary (from 0.7.0) is an artifact of kind `cohort-summary`: every sample summarized the
same way, each number traced to the artifact it came from. Its files (the binary ones in the
envelope above; arrays little-endian):

| File | Magic | Holds |
| --- | --- | --- |
| `summary.json` | | `format` (`"situcraft-cohort-summary"`), `version`, `method`, `methodVersion`; the cohort's `labels` (the marker set's `name` and `level`, the `names` every sample is counted in, in order, and their `colors`): every label of the level, and those of coarser levels some sample's cells carry there; the `genes`, every sample's panel's, in the order first met; `samples`, each one's `sample` (its id), `name`, `dataset`, `segmentation` (id and name), `region` (id, name and version, for a region sample), the `labels` its cells carry (the set's `id`, `kind`, `name`, `level` and `checksum`), the cells chosen (`choice`: `checksum`, `regions`, `excluded`, as a graph records them), its tissue `window` (`kind` `tissue` or `region`, the tissue's `closing` radius and `grid`, the `area` in µm² and boundary `segments`), its `panelGenes` (the genes counted in it), its `bundle` (the `run`, `slide`, Xenium `region` and analysis `software` its bundle names, and its `panel`'s `name`, `design`, `version` and `genes`), its `cells` chosen and `labeled`, `tissueAreaUm2`, and by label `counts`, `shares` (of its labeled cells) and `densityPerMm2`; its quality (`qc`: `cells`, `cellsPerMm2`, `medianTranscripts`, `medianGenes`, `medianAreaUm2`, `medianNucleusAreaUm2`, `nucleusShare`, `transcriptsPer100um2`, `transcripts`, `noTranscriptShare`; the nuclei's numbers `null` where the segmentation records no nucleus, the instrument's segmentation's read from the bundle's cells table); with niches, its cells in each niche (`niches`), their `nicheShares` and the cells with a window (`nicheCells`); and its `statistics`, each `{family, key, value, source}`: its graph's (`graph`: `mean neighbors`, `median edge µm`, `pieces`, `without a neighbor`), an enrichment's ratio of neighbors found over expected by pair (`enrichment`, `A\|B`, the names in order), nearest distances' median from each label to each (`nearest`, `A→B`, µm) and Moran's I by gene (`moran`), the source its artifact's id; and, with niches, the `niches` found across the samples together: their `parameters` (as a niches analysis's), number (`niches`), each one's `composition` (N × K, its windows' mean, the k-means center), `cells` in all the samples, `tissue` (each label's share of all the samples' labeled cells), `windows`, k-means's `inertia`, the further seeds' `agreement` (adjusted Rand index) and each niche's `defining` labels (indices, as a niches analysis's); the samples' quality compared (from 0.7.0's method 2): `qcMetrics` (each `{key, name, share}`: the numbers compared), `outlierRule` (`z`, `fold`, `points`, `minSamples`), `outliers` (each sample far out on a number: `sample`, `name`, `metric`, `value`, the samples' `median`, the `spread` and the robust `z` on the scale compared, log(1 + value) for a quantity, and the `reason` in words; strongest first) and `batches` (each slide's, run's and panel's `kind`, `name`, `samples` and the `medians` of the numbers over them); `panels`, the samples' panels compared (`panels`, each one's `name`, `design`, `version`, `genes`, a `checksum` of its genes, their IDs and codewords, and the `samples` read with it; `shared`, the genes every panel has, in the first panel's order; `partial`, each gene some panel lacks, its `id` and the `panels` (indices) that have it; `codewords`, the shared genes read by other codewords, by panel; `ids`, genes named alike with other Ensembl `ids`, or an `id` under other `names`); and `pca`, the pseudobulk's principal components (`normalization`, `top`, the `genes` used, each component's share of the `variance`, each sample's `scores`), absent for one sample |
| `pseudobulk.bin` | `SCCP` | The samples', labels' and genes' counts (u32 each: S, K, G, as `summary.json`'s), then for each sample, each label, each gene the transcripts of the sample's cells of that label (u32; `0xFFFFFFFF` for a gene the sample's panel has not) |
| `niches.bin` | `SCCN` | With niches: the cells' count (u32), each cell's row of its segmentation (u32), then each one's niche (i32, −1 without a window); the samples' cells chosen, in the samples' order |
| `table.csv` | | The cohort table, a row each number of each sample: `sample` (its id), `name`, `family` (`cells`, `share`, `density`, `niche`, `qc`, `graph`, `enrichment`, `nearest`, `moran`, `pca`), `key` (a label, a niche as `niche 1`, a quality number's name, a pair, a gene, a component as `PC1`), `value` (the shortest decimal that reads back as it) and `source` (the artifact it came from: the sample's labels for cells, shares and densities; its segmentation for quality; its graph or analysis for theirs; empty for a niche's share or a component's score, the summary's own; a number the segmentation does not record is left out) |
| `pseudobulk.csv` | | The pseudobulk as a table: a row each sample's label (`sample`, `name`, `label`, `cells`), a column each gene (empty for a gene its panel has not, never zero) |

The samples' quality is compared on the numbers that do not follow a section's size: a sample is far
out on one when its robust z-score (its distance from the samples' median over 1.4826 times their
median absolute deviation, or where that is 0, over 1.2533 times their mean absolute deviation; on a
log scale for a quantity) is at least 3.5 and it differs by at least 20% of the median (a quantity)
or 5 points (a share); with fewer than five samples none is. The principal components are of each
sample's transcripts of the genes every panel has, summed over its labels, as log2(1 + counts per
million of their total), the 500 most variable genes kept (ties to the first), centered, by the
eigenvectors of their Gram matrix (Jacobi's rotations); a component's sign makes the sample farthest
along it positive.

A sample's cells are those chosen (its dataset's, or its region's; never those a review excluded),
each labeled in the cohort's names (a sample's label not among them is refused). Its tissue is the
tissue window's area (the cells' footprints closed by the radius, 10 µm unless given), within its
region for a region sample. A summary is named by its samples (each one's dataset, segmentation,
cells' and labels' checksums, region, graph and analyses), the cohort's labels, the niches'
parameters, the closing radius and the method's version, so summarizing again answers it. The
project records each summary (kind `cohort-summary`: `{id, name, labelsFrom, markerSet, level,
samples, state, created, method, parameters, summary}`, `samples` the samples' ids, `labelsFrom` what
every sample is labeled from: a marker set's record id, which `markerSet` repeats, or a reference as
`ref:` its file's SHA-256 and its column of labels).

### Comparisons of abundance

A comparison of abundance (from 0.7.0) is an artifact of kind `abundance`, made from a cohort
summary: each label's cells compared between a design's groups, the replicate unit the unit.

| File | Holds |
| --- | --- |
| `abundance.json` | `format` (`"situcraft-abundance"`), `version`, `method`, `methodVersion`; the `summary` (`id`, `name`); the summary's `labels`; the `design`: its `name` and `version` when saved, its `spec`, the columns' `names`, its `rows` (each a replicate unit in a group: `unit`, `group`, the `samples` combined by id, the columns' values `x`), its `contrasts` (each `name` and `vector` over the columns), the samples it leaves out with their reasons (`excluded`), and `pseudoSamples` (from 0.7.0's slice 8) when its units are regions of one section compared as samples for teaching, not independent replicates; the intervals' `level`; and the `families` compared (`share`, `density`, `niche`), each its `name`, `model`, `effect` (`odds ratio`, `density ratio`), `labels`, each row's `counts` of each label (labels × rows), its `totals` (labeled cells; tissue in mm²; cells with a window) and `values` (count over total), its `tests` and their `universe` in words, and the tests `untested`. A test is a label's of a contrast: `tested`, or a `note` saying why not (no sample has a cell of it; every cell is of it; a group with none of it, or nothing else); the `estimate` on the link's scale (log odds, log density), its `se`, `t`, residual degrees of freedom `df`, two-sided `p`, the Benjamini–Hochberg `padj` over the family's tests, the effect as a `ratio` with its interval (`lo`, `hi`) and the model's `dispersion` |
| `results.csv` | A row each test: `family`, `label`, `contrast`, `tested`, `estimate`, `se`, `t`, `df`, `p`, `padj`, `ratio`, `lo`, `hi`, `dispersion`, `note` |

A share is a quasi-binomial generalized linear model of the label's cells out of the row's labeled
cells (logit link), a density a quasi-Poisson model of its cells with the log of the row's tissue
in mm² an offset (log link), each fitted by iteratively reweighted least squares as R's `glm` fits
it; the dispersion is Pearson's χ² over the residual degrees of freedom, and a contrast's test a t
test on those degrees of freedom. A comparison is named by its summary, design (rows, matrix and
contrasts), families, level and method version, so comparing again answers it. Every comparison
(of abundance, expression or spatial statistics) refuses a design whose tests would be of cells
rather than of replicates: a group of one unit, a unit in two rows of one group, and regions of
one section given different units unless the design marks them `pseudoSamples`. The project records
each comparison (kind `abundance`: `{id, name, summary, design, state, created, method,
parameters, summaryOf}`, `summaryOf` the manifest's summary: the tests, those untested, and those
`found`, each `family`, `label`, `contrast`, `ratio` and `padj`, below an adjusted p of 0.05).

### Comparisons of expression

A comparison of expression (from 0.7.0) is an artifact of kind `expression`, made from a cohort
summary: each label's pseudobulk compared between a design's groups, the replicate unit the unit.

| File | Holds |
| --- | --- |
| `expression.json` | `format` (`"situcraft-expression"`), `version`, `method`, `methodVersion`; the `summary` (`id`, `name`); the summary's `labels`; the `design` (as `abundance.json` has it); the intervals' `level`; `minCells`, the fewest cells of a label a row is tested with; the `genes` compared (those every panel has); the `results`, one per label; the `tests` and their `universe` in words. A label's result: `label`; `tested`, or a `note` saying why not; each design row's `cells` of the label and the `rows` used (those with `minCells` or more); then, tested, the used rows' `library` sizes (the tested genes' transcripts) and TMM `factors`; the `genes` tested (indices into `genes`), each one's `aveLogCPM` (mean log2 count per million), the genes `filtered` as too little expressed; voom's lowess `span`; the variances' prior (`s2Prior`, and `dfPrior`, null when infinite: the variances agree beyond chance), the tests' degrees of freedom `dfTotal`; and its `contrasts`, each `contrast` (its name) with arrays one per tested gene: `logFC`, its interval `lo` and `hi`, the moderated `t`, two-sided `p` and Benjamini–Hochberg `padj` over every label, gene and contrast tested; and the genes `up` and `down` below an adjusted p of 0.05 |
| `results.csv` | A row each test: `label`, `contrast`, `gene`, `tested`, `aveLogCPM`, `logFC`, `lo`, `hi`, `t`, `df`, `p`, `padj`, `note`; and a row each label untested, with its note |

A label's pseudobulk in a row is its cells' transcripts of each gene summed over the row's samples.
Genes are kept with a count per million of at least 10 over the median library size in as many
rows as the smallest group has (1 over the design's largest leverage) and a total count of 15;
library sizes are normalized by TMM (log-ratios trimmed 30% at each end and mean log-expressions 5%,
weighted by their binomial variances, against the row whose upper quartile is nearest the rows');
voom's values are log2((count + 0.5) / (library × factor + 1) × 10⁶), its weights the inverse of a
lowess trend of the genes' square-root residual deviations (its span 0.3 + 0.7 (50 / genes)^⅓, or 1
for 50 genes or fewer) at each value's fitted log count, raised to the fourth power; each gene's
model is weighted least squares, and its variance is moderated by the prior fitted to the genes'
log variances by their moments. A comparison is named by its summary, design, level, `minCells` and
method version, so comparing again answers it. The project records each comparison (kind
`expression`: `{id, name, summary, design, state, created, method, parameters, summaryOf}`,
`summaryOf` the manifest's summary: the `tests`, the labels `untested`, the tests `below` an
adjusted p of 0.05, and the 10 lowest `found`, each `label`, `gene`, `contrast`, `logFC` and
`padj`).

### Comparisons of spatial statistics

A comparison of spatial statistics (from 0.7.0) is an artifact of kind `spatial-comparison`, made
from a cohort summary: each sample's spatial statistics compared between a design's groups.

| File | Holds |
| --- | --- |
| `spatial-tests.json` | `format` (`"situcraft-spatial-tests"`), `version`, `method`, `methodVersion`; the `summary`; the summary's `labels`; the `design` (as `abundance.json` has it); the intervals' `level`; and the `families` compared (`enrichment`, `nearest`, `moran`), each its `name`, `effect`, whether it is compared as log2 ratios (`log`), its statistics' `keys` (a pair `A\|B`, a distance `A→B`, a gene), each row's `values` of each (keys × rows; log2 for a log family; null where none of the row's samples has it), the variances' prior (`s2Prior`; `dfPrior`, null when infinite and 0 for a family of one statistic) and `dfTotal`, its `tests`, their `universe` in words, and the tests `untested`. A test is a statistic's of a contrast: `key`, `contrast`, `tested` or a `note` saying why not (a row has none of it; no variation left to test); the `estimate` (a log2 ratio or a difference) with its interval (`lo`, `hi`), the moderated `t`, two-sided `p`, the Benjamini–Hochberg `padj` over the family's tests; for a log family, the effect as a `ratio` with its interval (`ratioLo`, `ratioHi`) |
| `results.csv` | A row each test: `family`, `key`, `contrast`, `tested`, `estimate`, `lo`, `hi`, `t`, `df`, `p`, `padj`, `ratio`, `ratio_lo`, `ratio_hi`, `note` |

A row's value is the mean of its samples' (a ratio of 0 counted as none). Each statistic's model is
ordinary least squares, and each family's variances are moderated by the prior fitted to their log
variances by their moments. A comparison is named by its summary, design, families, level and
method version. The project records each comparison (kind `spatial-comparison`, as an expression
comparison is recorded; `summaryOf` the `tests`, those `untested`, and those `found` below an
adjusted p of 0.05, each `family`, `key`, `contrast`, `estimate`, `ratio` and `padj`).

### Post-Xenium images

A post-Xenium image (from 0.5.0) is an artifact of one file in its folder, `image.json`, and one in
place: the image (role `image`), by its absolute path and SHA-256. Its id is made of the dataset
(its analysis UUID and manifest's SHA-256) and the image's SHA-256: the same image is the same
artifact.

| Field of `image.json` | Holds |
| --- | --- |
| `format`, `version`, `name` | `"situcraft-image"`, `1`, what it is called |
| `source` | The image's `path`, `size` and `sha256` |
| `image` | What the image is: `kind` (`rgb` or `channels`), `width`, `height`, `bits`, `compression`, `pixelSize` (µm, as its metadata says; absent when it says none), `channels` (each one's `name`), `levels` (each one's `width`, `height`, `tileWidth`, `tileHeight`, `across`, `down`), `notes` |
| `xeniumPixelSize` | The dataset's pixel size (µm) |
| `ranges` | An immunofluorescence image's channels: each one's samples from the 0.1th to the 99.9th percentile over its smallest level, the window it is first shown with |

Its summary says what it is. The project records each image (kind `image`, with its dataset and the
image's path), and a saved view of the Viewer records each
image's layer when there are some (`images`: each one's artifact `id`, `visible`, `opacity`, and an
immunofluorescence image's `channels` as the stains': `visible`, `color`, `window`, `gamma`).

### Alignments of post-Xenium images

Each version of an image's alignment (from 0.5.0) is an artifact of kind `transform`: one file in
its folder, `transform.json`, and, when it was read from Xenium Explorer's file, that file in place
(role `alignment`, by its absolute path and SHA-256). Its parents are the image and, when it was
made from another version, that version. Its id is the SHA-256 of `transform.json`: the same
version made again is the same artifact; the time it was made is its manifest's `created`.

| Field of `transform.json` | Holds |
| --- | --- |
| `format`, `version`, `name` | `"situcraft-transform"`, `1`, what it is called |
| `source`, `destination` | The coordinate systems it maps between: `name` (`image:<artifact id>`, `global`), `units` (`pixels`, `µm`), and the `image` artifact or the `dataset` (its analysis UUID). An image's pixels are its full-resolution pixels, (0, 0) the corner of its first |
| `method`, `model` | How it was made (`matrix` or `keypoints`, Xenium Explorer's file; `landmarks`, placed in SituCraft; `automatic`, registered by the nuclei) and what its fit may change (`similarity`: move, turn, one scale and a mirror; `affine`: also a scale along each axis and a shear; a matrix is `affine`) |
| `transform` | The source to the destination, two rows of three: (x, y) → (a·x + b·y + c, d·x + e·y + f) |
| `landmarks` | Each landmark's `fixed` position (µm) and `moving` position (the image's pixels); Xenium Explorer's keypoints, their fixed positions converted to µm |
| `fit` | Each landmark's `residuals` (µm from where the fit puts it) and `leaveOneOut` errors (µm from where the fit of the others puts it; absent when too few remain to fit), and their `rms`, `max`, `looRms`, `looMax` |
| `file` | Xenium Explorer's file it was read from: `path`, `size`, `sha256` |
| `matches` | An automatic version's local matches: each patch's center on the Xenium image (`fixed`, µm), where it matched on the image (`moving`, its pixels), the correlation's `quality` (its peak's height over the surface's standard deviation) and whether the fit `kept` it; `fit` is of those kept |
| `deformation` | A smooth deformation after the transform (an automatic version's, when one was kept): cubic B-splines on a grid of control points, its `origin` (µm), `spacing`, `nx` × `ny` points and each one's displacement `dx`, `dy` (µm, row by row); a point x of the transform is moved to x + the B-splines' sum at x |
| `registration` | What an automatic registration found: the image's `channel` registered and the Xenium image's (`fixed`), the search's `best` candidate and best few (`candidates`: mirrored or not, pixel size, turn, normalized mutual information, overlap, transform), the mutual information at the finest level refined (`nmi`, `resolution`), the patches' side (`patchUm`) and pixel (`fine`), a deformation's errors (`deformedRms`; each fifth of the matches predicted from the rest, `heldOut`, and by the model alone, `modelHeldOut`), its `smoothness` and `maxDisplacement`, and `warnings` |
| `xeniumPixelSize`, `parent`, `by`, `note` | The dataset's pixel size; the version it was made from; who made it; a note |

Its summary gives the image, method, model, number of landmarks, the fit's errors, `umPerPixel`,
`rotation` (degrees) and `reflected`, `by` and `parent`. The project records each version (kind
`transform`, with its image and parent), and which version places each image as a decision (kind
`use-transform`: the image, the version, the reason, who and when); the latest names it. An image
with no decision is not aligned, and not drawn on the tissue.

### Checks of alignments

A check of a version of an image's alignment (from 0.5.0) is an artifact of kind `alignment-check`:
one file in its folder, `check.json`; its parents are the image and the version. Its id is the
SHA-256 of `check.json`: the same images and version give the same check.

| Field of `check.json` | Holds |
| --- | --- |
| `format`, `version`, `name` | `"situcraft-alignment-check"`, `1`, what it is called |
| `image`, `transform` | The image's and the version's artifacts |
| `channel`, `fixed` | What of the image was compared (its hematoxylin, or a nuclear channel), and the Xenium image's channel |
| `patches` | Each patch: its `centre` on the tissue (µm), its side (`size`, µm), how far the version draws the image there from where it belongs (`error`, µm, drawn less true), the correlation's `quality`, and whether it `matched` clearly enough to tell |
| `patchUm`, `fine` | The patches' side and their µm per pixel |
| `checked`, `matched`, `median`, `p90`, `max`, `rms`, `worst` | How many patches were checked and matched; the matched patches' errors' median, 90th percentile, most and root-mean-square (µm); where the most is |
| `noise` | A bound of the matches' own error (root-mean-square, µm), from neighboring patches' differences: an error well above it is the alignment's |

The project records each check (kind `alignment-check`, with its image and version).

### Marker sets

A marker set (from 0.4.0) is a JSON file SituCraft reads, writes and ships (one for each
development bundle's panel); the project keeps each version of one, and each label set keeps the
one it was made from.

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-markers"`, 1 |
| `name`, `about`, `panel` | Its name; what it is for; the panel it was made for (a design id, informative) |
| `levels` | The levels' names, the top first (1 to 6): `["Class", "Lineage", "Subtype"]` |
| `labels` | Each label (1 to 500): `name`; `parent`, a label listed before it, at the level above (none at the top); `positive` markers (1 to 200 genes) and `negative` (up to 200); `references`, DOIs among the set's |
| `states` | Each state (up to 50), carried besides a label (proliferating, say): `name`, `positive`, `negative`, `references`, and `parent`, the label whose cells alone can carry it |
| `references` | The published sources of its markers: `doi`, `citation` |

Names are unique regardless of case; a gene is listed once in a label, not both positive and
negative. Genes are matched to the panel by name (regardless of case where one gene matches); a set
naming genes the panel lacks is refused, with them listed. Its checksum is the SHA-256 of its
canonical form: the JSON as SituCraft writes it compactly, its fields in the order above.

The project records each version of a marker set as kind `marker-set`: `family` (the first
version's id), `version`, `parent`, `name`, `set` (the file), `checksum`, `source` (`{starter}`,
`{file}` or `{edited: true}`), `by`, `created`.

### Labels

A label set (from 0.4.0) is an artifact of three files:

| File | Magic | Holds |
| --- | --- | --- |
| `labels.json` | | `format` (`"situcraft-labels"`), `version`, `method`; the `segmentation` and the `clustering` (artifact id and name); `markers` (the set's `name` and `checksum`); `parameters` (`margin`, `minimum`, `state`, `bins`, `controls`, `lineage`, `seed`); `fixed` (the expression, its `targetSum`, the score, the negative markers' part, the assignment); `levels`; `sets`, each label then each state: `name`, `level` (−1 for a state), `parent` (an index, −1 for none), `state`, `positive` and `negative` markers and their control genes (`positiveControls`, `negativeControls`), and `cells` carrying it; `cells` (`analyzed`, those `labeled` at each level, `unlabeled`); `clusters`; `composition`, for each level and cluster the cells by the label they carry at the level (indices among the sets; the last count, cells without a label); `states`, for each state and cluster the cells carrying it; `seconds` |
| `markers.json` | | The marker set, as above |
| `labels.bin` | `SCLB` | The cells n (u32) and their indices into the segmentation's cells (u32 × n, increasing) and clusters (u32 × n); the sets S, levels L and states (u32 each); each set's score for each cell (f32, S × n, set-major) and its positive markers' part (f32, S × n); for each set with negative markers, their part (f32 × n); at each level each cell's label (u32 × n, an index among the sets, 0xFFFFFFFF where it took none), and its lead over the next label (f32 × n, NaN where the level was not reached); each cell's confidence (f32 × n); and for each state whether each cell carries it (u8 × n) |

Labels imported from a table (above) are an artifact of two files: `labels.json` has a `source`
(`format`, `file`, `sha256`, `path` in a store, `idColumn`, `column`, `confidenceColumn`,
`colorColumn`, `rows`; for the instrument's clusters `tool`, `version` and `title`) in place of
`markers`, `parameters` and `clustering`, one level named by the column, each label's `color` where
the table gave one, and no composition; `labels.bin` holds every cell of the segmentation in order,
in no cluster (0xFFFFFFFF), with no scores: after the sets, levels (1) and states (0), each cell's
label, its lead (NaN) and its confidence (NaN where none was given). There is no `markers.json`.

Labels from a reference (above) are an artifact of three files: `labels.json` has a `reference`
(`format`, `file`, `sha256`, `path`, `matrix`, `labelColumn`, `geneColumn`, `values`, `refCells`,
`refGenes`, the `labels` and each one's reference `cells`, the panel's genes it lacks (`missing`),
the `parameters` (`tune`, `margin`, `markers`), the genes per pair (`perPair`), the genes `shared`
and those `compared` in the first round, and the cells `fineTuned` and `changed` by fine-tuning) in
place of `markers` and `parameters`, one level named by the label column, and the composition;
`reference.json` (`format` `"situcraft-reference-profiles"`, `version`, `labels`, `cells`, `genes`
(the shared genes), `profiles`: each label's mean log-normalized expression over them) in place of
`markers.json`; `labels.bin` is as above with each label's first-round correlation as its score and
its last round's correlation as its positive part (NaN for labels not in that round, and for every
label when there was one round), no negative parts, one level, the lead over the next label in the
deciding round, the same lead as the confidence, and no states.

The project records each label set it uses (kind `labels`, with its dataset, segmentation,
clustering, `markerSet`, the version's record id, `markers` and summary; an imported one, its
`source`; one from a reference, its `reference` and `parameters`).

Label versions are the project's own records, made in the page:
- **Curated labels** (kind `curated-labels`): `dataset`, `segmentation`, `base` (`labels`, a label
  set's id, a curated or consensus set's; `level`; `name`), `name`, `by`, `created`, and `edits`,
  appended, never changed: each `{kind, cells | clustering and clusters, label, reason, by, time}`,
  `kind` being `cells` (cells relabeled; their indices among the segmentation's cells), `cluster` (a
  clustering's clusters relabeled, from 0) or `accept` (cells' labels confirmed). Its labels are
  the base's with the edits applied in order; a new label's name is added after the base's.
- **Consensus labels** (kind `consensus-labels`): `dataset`, `segmentation`, `name`, `members` (each
  `{ref: {kind, id, level}, name}`), `mapping` (for each member, `{its label: a common name}`),
  `rule` (`{kind: "unanimous"}`, `{kind: "majority", min}` or `{kind: "priority"}`), `by`,
  `created`.

The Viewer's fill `labels:<id>:0` colors cells by a curated or consensus set too. The Viewer's fill
`labels:<artifact id>:<level>` colors cells by the label they carry at a level, from 0; saved views
keep it.

### Jobs

Each job is kept in `<data dir>/jobs/<id>.json`: its kind, title, spec (every effective
parameter), state, times, number of runs, error and log. A job interrupted by SituCraft stopping is
`orphaned` when it starts again, to be resumed or discarded.

### The log of calls

Every call an agent or a script makes of SituCraft is a line of `<data dir>/calls/YYYY-MM.jsonl`
(the month in UTC), a JSON object a line, appended:

| Field | Value |
| --- | --- |
| `time` | When it was asked, UTC, RFC 3339 with milliseconds |
| `by`, `via` | The caller's name (an agent's client, a script's `client`), and `agent` or `script` |
| `leave` | An agent's leave (`edits`, `compute`, `files`; none: it may only read) |
| `tool`, `args` | The action and its arguments, texts cut at 500 characters and lists at 50 items |
| `outcome` | `done`, `refused` (not performed) or `failed` (performed, or tried, and not done) |
| `message` | The sentence it answered, or why not (cut at 1,000 characters) |
| `changed` | The project's records it added, changed or removed: `{id, kind, name, change}` |
| `ids` | The ids of records its answer named, each once |
| `ms` | How long it took |
| `session` | The run of SituCraft that took it |

### Reviews

A dataset's reviews (from 0.5.0) are written from the Viewer's **Reviews** or by `export` (`what:
"reviews"`):

- **CSV** (`<dataset>-reviews.csv`): a row for each cell, cluster or region a review is of; its
  columns `review`, `time`, `reviewer`, `action`, `dataset`, `dataset_name`, `segmentation`,
  `segmentation_name`, `target`, `cell_id`, `cell_row` (from 0), `clustering`, `cluster` (from 1),
  `region`, `region_name`, `region_version`, `label_set`, `label_set_name`, `label_set_level`,
  `label` (the cell's, as shown), `confidence`, `previous` (the cell's value before), `new`,
  `concern`, `alternatives` (separated by `; `), `note`, `images` (each image's alignment version
  in use, with its artifact and whether it was shown) and `derived` (`yes` for a label edit read as
  a review). RFC 4180: a field with a comma, a quote or a line break is quoted.
- **JSON** (`<dataset>-reviews.json`): `{format: "situcraft-reviews", version: 1, written,
  situcraft, project, datasets, actions, concerns, reviews}`, the reviews as the project keeps them.

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

### Recipes of results

A result's recipe (the Reports view's **Recipe**, `recipe`, `GET /api/artifacts/<id>/recipe`) is
every step from its bundles to it, made again by `situcraft run`:

| Field | Holds |
| --- | --- |
| `format`, `version`, `kind` | `"situcraft-recipe"`, 1, `"result"`. A newer version, another kind or a field this SituCraft does not know is refused |
| `name`, `software` | The result's name, and the SituCraft that wrote the recipe |
| `result` | The step that makes the result (the last) |
| `bundles` | Each bundle by its `role` (its folder's name), `name` and `analysis` (its run's analysis UUID), and the `files` its steps read, by `path` and `sha256`, so that the same bundle is known again |
| `files` | Other files its steps read (a table of labels, a reference), by `name` and `sha256` |
| `steps` | Each step: `n`, the artifact's `kind` and `name`, the id it `made` where the recipe was written, the `job` that makes it, its `method`, the steps it is made `inputs` from, whether it `carries` cells the window chose (made again only on the same bundle), and the job's `spec` |
| `run` | How to make it again |

A spec is the job's, as its artifact's manifest records it, with `situcraft:bundle/<role>[/<path
in it>]` for a bundle's folder, `situcraft:file/<name>` for another file, and
`situcraft:step/<n>` for what an earlier step made.

### Runs (`situcraft run`)

`situcraft run --recipe <recipe> --bundle [<role>=]<folder> --out <new folder>` writes, in its
output folder, `library/` (its SituCraft's library, the artifacts it made in `library/artifacts`),
`certificate.zip` (a certificate of the result, as the window writes one, which `situcraft
verify` checks) and `run.json`:

| Field | Holds |
| --- | --- |
| `format`, `version` | `"situcraft-run"`, 1 |
| `software`, `recipe` | The SituCraft that ran it; the recipe's `name`, `sha256` and the SituCraft that wrote it (`madeBy`) |
| `clock`, `threads` | The time it recorded everything as made (`--clock`), and the threads it was given (0, every core) |
| `bundles` | Each bundle by its `role` and `analysis`, whether it is the one the recipe was made from (`sameAsRecipe`, else the files that `differ`), and its `files` by SHA-256 |
| `files` | The other files it read, by name and SHA-256 |
| `steps` | Each step: `n`, `kind`, `name`, `job`, its `state` (`made`, `failed`, `not made`), the `id` it made and the one the recipe's step made (`recipeMade`, `sameAsRecipe`), its `files` by SHA-256, a clustering's, map's or labels' `content` digest, its `error`, and `seconds` |
| `result`, `certificate` | The result's step, and the certificate's `path` and `fingerprint` |
| `exit` | Its exit status (`code`: 0 made, 1 a step failed, 2 not run) and the `reason` |
| `seconds` | How long it took |

### Refinements in the terminal (`situcraft refine`)

`situcraft refine [flags] <bundle> <new folder>` refines a bundle's segmentation as the app does,
without the app, and writes the segmentation's files (above) to the new folder with its
`recipe.json`. The folder imports into the app as a SituCraft segmentation of the same dataset.

| Flag | Sets |
| --- | --- |
| `--recipe` | A recipe to run (the bundle defaults to its `dataset.path`); the flags below change it |
| `--preset` | `balanced` (the default), `conservative`, `flexible`, `low-memory`, `roi-pilot` or `expert` |
| `--seed`, `--sweeps`, `--samples` | The seed, the last stage's sweeps and the sweeps summarized |
| `--min-qv` | The quality the modeled transcripts reach (20) |
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
| `--reference` | 0 | Also write a single-cell reference of the tissue's types, this many cells of each, as `<folder>-reference.zarr` (below) |
| `--post-xenium` | | Also make post-Xenium images of the section, an H&E and an immunofluorescence image, each placed by a known transform and deformation (below); not for a mosaic |
| `--cohort` | | A simulated cohort instead of one tissue: `standard`, `null` or `paired` (below); `--params` then reads the cohort's parameters, `--seed` its seed and `--width`, `--height` its sections' size |

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
| `experiment.xenium` | The run: one `experiment_uuid` derived from the parameters (not those of its post-Xenium images, made after the run), the pixel size, the cell count and median transcripts per cell, the image's path. `analysis_sw_version` names the layout the files follow, then what wrote them (`xenium-2.0.0 layout, situcraft-simulate 0.3.0`): readers choose how to read a bundle by its version |
| `transcripts.parquet` | Every observed molecule, in Xenium's columns and types: `transcript_id` (its field of view of 250 µm in the top 16 bits), `cell_id` (the simulated instrument's cell, or `UNASSIGNED`), `overlaps_nucleus` (`uint8`), `feature_name`, `x_location`, `y_location`, `z_location` (µm), `qv`, `fov_name`, `nucleus_distance` (µm to the nearest nucleus in the section, from the molecule's pixel; 0 over one), `codeword_index` (its feature's codeword in `gene_panel.json`) |
| `cells.parquet`, `cell_boundaries.parquet`, `nucleus_boundaries.parquet` | The simulated instrument's cells (Xenium's columns; `segmentation_method` "Segmented by nucleus expansion of 5.0µm") and their outlines along pixel edges |
| `cell_feature_matrix.h5`, `cell_feature_matrix/` | Their counts of Q20 molecules of every feature: as a 10x Genomics feature-barcode matrix in HDF5 (`/matrix`: `barcodes`, `data`, `indices`, `indptr`, `shape`, and `features/` with `id`, `name`, `feature_type`, `genome`), and as Matrix Market |
| `cells.zarr.zip` | Zarr format 2, zipped, as XOA 2 lays it out: `masks/0` (nuclei: label *p* + 1 is nucleus *p*) and `masks/1` (cells: label *k* + 1 is `cells.parquet`'s row *k*) as `uint32` labels on the image's pixels; `masks/homogeneous_transform` (4 × 4, µm to pixels); `cell_id`; `cell_summary`; and `polygon_sets/0` (nuclei) and `polygon_sets/1` (cells), each with its outlines' `vertices` and `num_vertices`, each polygon's `cell_index` and `method`; the root's attributes name the sets and the segmentation methods |
| `morphology_focus/morphology_focus_0000.ome.tif` | The DAPI stain of the nuclei in the section: a tiled, deflate-compressed OME-TIFF with its reduced levels as SubIFDs (Xenium's own are JPEG 2000) |
| `metrics_summary.csv`, `gene_panel.json` | The run's metrics, computed as SituCraft's validation recomputes them, and the panel: genes `SIM001`…, negative control probes `NegControlProbe_SIM01`… and codewords `NegControlCodeword_0500`… |
| `truth/` | A Xenium Ranger-shaped bundle of the same run: `experiment.xenium`; `cells.parquet` (every true cell: `cell_type`, `nucleus_in_section`, its `domain` (from 0.6.0: the domain, numbered from 0, whose mix chose its type; a mosaic's numbered across its tiles), areas, centroid, Q20 gene transcripts), both boundary tables and `cells.zarr.zip` of the true cells (the nuclei in the section numbered in their cells' order, `cell_index` naming each one's cell); `transcripts.parquet`, the observed molecules row for row in the bundle's columns, with their true `cell_id`, and `true_x_location`, `true_y_location`, `true_z_location`, `origin` (`cell`, `background`, `control probe`, `control codeword`) and `wide_displacement`; and `instrument_cell_types.csv`, each of the instrument's cells (`cell_id`) with its true cell (`true_cell_id`, the one whose nucleus it grew from) and that cell's `cell_type`: imported as labels of the instrument's own cells, it scores any labels of them by cell ID |
| `post_xenium/` | With `--post-xenium`: `simulated_he_image.ome.tif` (an H&E, 8-bit RGB) and `simulated_if_image.ome.tif` (an immunofluorescence image, 16-bit channels `DAPI` and `Membrane`), pyramidal, tiled OME-TIFFs (deflate) of the same cells and nuclei, as Xenium Explorer reads them, with no alignment beside them |
| `truth/post_xenium/` | Where each image lies: `registration.json` (each image's `transform`, its pixels to µm, two rows of three; its `deformation`, waves each displacing points along an `axis` by `amplitude_um` × sin(2π `frequency_per_um` · x + `phase`), x a point in µm after the transform; its `parameters`; the H&E's `stains`, the hematoxylin and eosin colors of this section), and `<name>_imagealignment.csv`, each transform without its deformation as Xenium Explorer's matrix |

To score a segmentation against the truth, import `truth/` into the simulated dataset as a
Xenium Ranger bundle (Segmentation view, **Import a segmentation**) and compare the two.

**A reference of its types** (`--reference N`) is an AnnData object in Zarr (format 2) beside the
bundle: N cells of each type (`obs`'s `cell_type`; the index `ref-000001`…), each drawn from its
type's expression over the panel's genes (`var`, `SIM001`…) as the tissue's cells are, but clean of
background and displaced molecules, at 1,000 expected transcripts (spread log-normally, σ 0.4), the
counts in `X` (CSR, float32); its seed is the tissue's. Label the tissue's cells from it in the Cell
types view (**Labels from a reference**) and compare them with the truth's types.

Other tools read a simulated bundle as they read Xenium's: spatialdata-io's Xenium reader reads
its table, transcripts, labels, boundaries and image with no warning, and tifffile and pyarrow
its image and tables (`validation/reference/read_simulation.py` checks each against what the
simulation made).

### Simulated cohorts (`situcraft simulate --cohort`)

`situcraft simulate --cohort <name> <new folder>` writes a simulated cohort: sections of subjects
in groups, each a simulated bundle of its own layout, cells and molecules, with one panel; the
sections differ from one another by stated amounts and between groups by known effects. The model
and its validation are in [research.md](../situcraft-spec/research.md) §3.4b.

| Cohort | Holds |
| --- | --- |
| `standard` | Four subjects in each of two groups, `control` and `treated`, on two slides; sections of 250 × 250 µm with twelve domains (about 750 cells each), with effects in the treated group: the endothelial-like type's odds against each other type ×2 in every domain, the immune-like type's ×4 in the domains the epithelial-like type leads, and `SIM033` ×2.5 in epithelial-like cells. About 3 s and 64 MB |
| `null` | The same design and panel, its own seed, without effects: the groups differ only by chance (the start page's **A simulated null cohort**). About 3 s and 68 MB |
| `paired` | Six subjects, each a section `before` and `after` (twelve sections), the same panel, the effects after, the endothelial-like type's odds ×4; a subject's sections share 60% of their variation, and lie on one slide (the start page's **A simulated cohort, before and after**). About 4 s and 95 MB |
| `outlier` | The standard cohort with its sixth sample (`S06`) planted to detect 40% of the transcripts it would, as a failed run would: for checking that cohort QC finds it |

Its parameters (`--print-params` prints them; `--params` reads them back):

| Parameter | Sets |
| --- | --- |
| `seed`, `panel_seed` | The cohort's seed: each sample's seed and draws come from it; and the panel's stream (the seed if not given). The presets share the standard cohort's panel |
| `groups`, `subjects`, `paired` | The groups (2 to 10; the first the reference) and the subjects in each, or, paired, the subjects each sampled in every group |
| `slides`, `slide_sd` | Slides as batches (0 for none): an unpaired group's samples dealt to them in turn, a paired subject's together; each slide's detection efficiency log-normal with this σ, multiplying its cells' densities and the ambient background |
| `share_sd`, `level_sd`, `subject_share` | Sample-to-sample variation on the log scale: of each type's weight in the domains' mixes, and of each gene's expected count in each type; in a paired design, the share of each draw's variance a subject's samples have in common |
| `outliers` | Samples made far out on purpose: each `{sample, detection}`, the sample by its number (`S01` is 1) and the factor its detection efficiency is multiplied by (its cells' densities and the ambient background, as a slide's efficiency multiplies them), above 0 and at most 10 |
| `effects` | Each `{kind, group, type, within, gene, fold}`: `share` multiplies a type's weight in every domain's mix (its odds against each other type, in every domain), or with `within` in the domains another type leads; `level` multiplies a gene's expected count in a type's cells; in `group`, against the first group |
| `section` | Every section's parameters, as `--print-params` lists a tissue's (but its seed, odds, levels, names and densities, which each sample sets). Each section has its own domains' places and its own cells, and the same domains' mixes as the others (the domain numbered *d* is the same kind of place in every section) |

| File | Holds |
| --- | --- |
| `situcraft-cohort.json` | `format` (`"situcraft-cohort"`), `version` (1), `simulated`, `software`, `about`, `truth`, `samples` (each one's `name`, `subject`, `group` and `slide`) and `parameters` |
| `samples.csv` | Each sample's `sample`, `subject` and `condition` (its group), as SituCraft reads a table of samples |
| `reference.zarr` | With `--reference N` (and in the start page's example, 300): a single-cell reference of the cohort's types over the panel every sample shares, N cells of each type, as the tissue's reference is (an AnnData object in Zarr: `cell_type` in obs), each sample's own levels aside |
| `truth/cohort.json` | `design` (the groups, subjects, pairing, slides and variation); `effects`, each as asked with what the true cells show of it (each group's mean of its samples' values, their difference `measured_log_fold` against `log_fold`, and its `std_error` from the samples' spread; paired: of the subjects' differences), the value being a type's log odds against the cells of the types no share effect changes, in each domain the effect holds in (averaged over them), or a gene's log odds among its type's transcripts, both of which the fold multiplies; and `samples`, each one's seed, `detection` (its slide's efficiency times, for a planted outlier, its factor, which `planted` gives), `type_odds`, true `cells`, `cells_by_type`, `domains` (each domain's `leader`, the type that leads it, and its `cells_by_type`) and, for each level effect, its `fold` and its type's `cells`, `transcripts` of the gene and `type_transcripts` |
| `<sample>/` | A simulated bundle (above): its `experiment.xenium` names the sample's region, its run (`simulated run 1` for `simulated slide 1`) and its `slide_id`; its `situcraft-simulation.json` records its parameters, among them the cohort's `panel_seed` and `mix_seed`, its `placement_retries`, `type_odds`, `domain_odds` and `level_folds` |

Opened in SituCraft (the start page's **A simulated cohort**, or the folder's bundles added
together and `samples.csv` read in the Cohort view), each section is a sample with its subject,
condition and slide.
