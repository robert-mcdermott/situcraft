# SituCraft: methods, formats and data research

*Research date: 2026-10-13. Background for the design of SituCraft. Format facts were checked
directly on the 10x Genomics Xenium 2-FOV example bundles (XOA 2.0).*

**Conventions**
- **(verified)** marks a fact checked on real files.
- **(to verify)** marks a claim not yet confirmed from a primary source or a real file.
- Each method SituCraft implements is cited here and in the header comment of the file that
  implements it. Where a publication leaves a choice open, the section says "SituCraft's choice".

## 0. Summary

1. **Xenium bundles need four codecs and three container formats**, none of them in Go's standard
   library except deflate: JPEG 2000 (images), Zstd and LZ4 inside Blosc (Zarr chunks), and Zstd
   (Parquet pages). They sit inside BigTIFF, Zarr v2 (in zip stores) and Parquet. SituCraft
   implements all of them (§1). JPEG 2000 is the largest piece and is on wave 1's critical path.
2. **No HDF5 is needed.** The cell-by-gene matrix is in `cell_feature_matrix.zarr.zip` and in MEX
   form (`cell_feature_matrix.tar.gz`), as well as `.h5`.
3. **Transcript-informed segmentation** is implemented from the model of Jones et al. (2025)
   (§3). Nucleus expansion is the baseline (§3.1).
4. **Segmentation quality has no single score.** Comparisons report assignment, morphology,
   cell-state and conclusion evidence (§4), including the spurious co-expression measure of Jones
   et al. (2025).
5. **Two small CC BY 4.0 bundles** cover daily development. Simulation covers ground truth and
   scale (§7).

## 1. Xenium output bundles

10x Genomics documents the bundle in its Xenium Onboard Analysis (XOA) output pages ("Output
overview", "Outputs at a glance", "Understanding outputs").

### 1.1 Files (verified, XOA 2.0, Lung 2-FOV)

| File | Content | Format |
| --- | --- | --- |
| `experiment.xenium` | Run manifest: versions, run/region names, panel, pixel size (0.2125 µm), z step, file paths, counts | JSON |
| `metrics_summary.csv` | Run metrics (decoding, Q20 fraction, negative-control rates, cells, assigned fraction, segmentation method fractions, polygon checks) | CSV, one row |
| `gene_panel.json` | Panel targets and codewords | JSON |
| `transcripts.parquet` (`.csv.gz`) | One row per decoded molecule | Parquet, Zstd, PLAIN encoding |
| `transcripts.zarr.zip` | Spatially chunked transcripts | Zarr v2, stored zip |
| `cells.parquet` (`.csv.gz`) | Per-cell summary | Parquet |
| `cell_boundaries.parquet`, `nucleus_boundaries.parquet` | Polygon vertices (cell_id, vertex x, y) | Parquet |
| `cells.zarr.zip` | Polygon sets (nucleus, cell), label masks, cell summary, segmentation method per cell, homogeneous transform | Zarr v2, stored zip |
| `cell_feature_matrix.zarr.zip`, `.tar.gz` (MEX), `.h5` | Cell-by-gene counts | Zarr v2 / Matrix Market / HDF5 |
| `analysis.zarr.zip`, `analysis.tar.gz` | Secondary analysis (clusters, projections, differential expression) | Zarr v2 / CSV |
| `morphology_focus/morphology_focus_000N.ome.tif` | 2D focus image(s): one file (DAPI) for nucleus expansion; four (DAPI, boundary, interior RNA, interior protein) for multimodal segmentation | OME-TIFF (BigTIFF) |
| `morphology.ome.tif` | Z-stack | OME-TIFF (BigTIFF) |
| `aux_outputs.tar.gz`, `analysis_summary.html` | Auxiliary outputs; HTML summary | |

### 1.2 Transcripts (verified)

Columns: `transcript_id` (uint64), `cell_id` (string; `UNASSIGNED` when unassigned),
`overlaps_nucleus` (uint8), `feature_name` (string), `x_location`, `y_location`, `z_location`
(float32, µm), `qv` (float32, Phred-scaled), `fov_name` (string), `nucleus_distance` (float32,
µm), `codeword_index` (int32).

The Lung 2-FOV has 825,885 rows and 537 distinct feature names for 289 panel targets. The rest
are control and unassigned codewords, recognized by their name prefixes
(`NegControlProbe_`, `NegControlCodeword_`, `UnassignedCodeword_`; others in older and newer
panels (to verify)). QC and segmentation default to `qv ≥ 20` (Q20), as XOA's metrics and
Xenium Ranger do.

### 1.3 Zarr stores (verified)

- Zarr format 2, in zip files stored without zip compression.
- Chunks are compressed by **Blosc** with **LZ4** (clevel 5, byte shuffle) for most arrays and
  **Zstd** (clevel 1, byte shuffle, blocks not split) for label masks.
- Blosc details settled by experiment (chunks written by c-blosc 1.21 through numcodecs, and every
  chunk of both bundles), where the format description is silent:
  - block offsets count from the start of the chunk, and blocks may be stored out of order;
  - a block is split into one stream per byte of its type only when the "don't split" flag is
    clear, the type is at most 16 bytes, each stream holds at least 128 elements, and the block is
    not the chunk's short last one;
  - a stream whose compressed size equals its decoded size is stored as it is;
  - format version 2 (Blosc 1) bit-shuffles only blocks of whole groups of eight elements; other
    blocks are stored unshuffled.
- Dtypes: `<u4`, `<i4`, `<f4`, `<f8`; C order.
- `cells.zarr.zip` (format "CellSegmentationDataset" 6.2) holds:
  - `cell_id` (uint32 × 2: a numeric prefix and dataset suffix, which together encode the
    string cell ID);
  - `polygon_sets/{0,1}` (nucleus, cell): `vertices` (float32, up to 25 vertex pairs per cell),
    `num_vertices`, `cell_index` and `method`;
  - `masks/{0,1}` (uint32 label images at image resolution) and `masks/homogeneous_transform`
    (4×4);
  - `cell_summary` (centroids, areas, z level, nucleus count).
- **Cell IDs (verified):** the string form (`aaafiiei-1`) is the uint32 prefix written as eight
  hexadecimal digits shifted to the letters a–p (0 → a, …, f → p), a hyphen, and the suffix: the
  pair [362568, 1] is `aaafiiei-1`. Every copy of the cell list in both bundles (cells.parquet,
  cells.zarr.zip, the Zarr matrix's cell_id, the MEX barcodes) agrees under this encoding.

### 1.3a The cell-by-feature matrix (verified)

- `cell_feature_matrix.zarr.zip`: group `cell_features` with `indptr`, `indices`, `data` (uint32;
  compressed by **feature**: `indptr` has one entry per feature plus one) and `cell_id` (the cells'
  uint32 pairs); the group's attributes list `feature_ids`, `feature_keys` (names) and
  `feature_types` (`gene`, `negative_control_probe`, `negative_control_codeword`,
  `unassigned_codeword`, `deprecated_codeword`), and **one more feature, `Total transcripts` of type
  `aggregate_gene`**, each cell's gene total. That is why the Zarr form has one feature more than
  the MEX form (542 and 541 in the Lung bundle) and why its counts sum to about twice the cells'.
- `cell_feature_matrix.tar.gz` (MEX): `matrix.mtx.gz` (features × cells, 1-based), `features.tsv.gz`
  (ID, name, type in words: "Gene Expression", "Negative Control Probe", …) and `barcodes.tsv.gz`
  (cell IDs).
- Each cell's matrix counts, summed by feature type, equal cells.parquet's `transcript_counts`,
  `control_probe_counts`, `control_codeword_counts`, `unassigned_codeword_counts` and
  `deprecated_codeword_counts`, and `total_counts` is their sum; all are counts of transcripts with
  a quality score of at least 20 assigned to the cell.

### 1.3b The run's metrics (verified)

On both bundles, these metrics of `metrics_summary.csv` (and two values of `experiment.xenium`)
are reproduced exactly (counts) or to 1e-6 (ratios) from the data. Validation recomputes them all,
and the QC view shows each beside its recorded value:

| Metric | Definition |
| --- | --- |
| `num_cells_detected`, `experiment.num_cells` | rows of cells.parquet |
| `total_high_quality_decoded_transcripts` | gene transcripts with qv ≥ 20 |
| `fraction_transcripts_decoded_q20` | gene transcripts with qv ≥ 20 ÷ all gene transcripts |
| `fraction_transcripts_assigned` | of the Q20 gene transcripts, the share assigned to a cell |
| `total_cell_area` | the sum of cells.parquet's `cell_area` (µm²); the cell mask's (`masks/1`) pixels give the same |
| `decoded_transcripts_per_100um2` | Q20 gene transcripts per 100 µm² of **cell area** (also `experiment.transcripts_per_100um`) |
| `nuclear_transcripts_per_100um2` | Q20 gene transcripts overlapping a nucleus (`overlaps_nucleus`) per 100 µm² of the **nucleus mask** (cells.zarr.zip `masks/0`, its non-zero pixels × 0.2125² µm²; cells.parquet's `nucleus_area` agrees only in Lung, and the nuclei's polygons give 5–8% more) |
| `median_transcripts_per_cell`, `experiment.transcripts_per_cell` | the median of `transcript_counts` |
| `median_genes_per_cell` | the median over cells of their genes with a Q20 transcript (the matrix's non-zero genes) |
| `fraction_empty_cells` | cells whose `transcript_counts` is 0 ÷ cells |
| `negative_control_probe_counts_per_control_per_cell` | the sum of `control_probe_counts` ÷ the panel's negative control probes ÷ cells |
| `estimated_number_of_false_positive_transcripts_per_cell` | the previous × the panel's genes |
| `cells_per_100um2` | cells per 100 µm² of `region_area` |
| `segmented_cell_boundary_count`, `…_interior_count`, `…_nuc_expansion_count` | cells by segmentation method (cells.zarr.zip's `polygon_sets/1/method`, indexing its `segmentation_methods`) |
| `segmented_cell_boundary_frac`, `…_interior_frac`, `…_nuc_expansion_frac` | those counts ÷ cells |
| `segmented_cell_stain_frac` | (boundary-stain + interior-stain cells) ÷ cells |

Four recorded metrics are not reproduced, and the QC view shows them as recorded, saying why:

- `region_area`: the area of the region chosen on the slide, which the bundle does not describe.
- `adjusted_negative_control_probe_rate` and `adjusted_negative_control_codeword_rate`: the
  plain per-feature rate of control detections against gene detections (Q20) is about 7% below
  the recorded probe rate and about half the recorded codeword rate, and no simple variant tried
  (all qualities, counts in cells) gives the recorded values. The adjustment is not described in
  the bundle.
- `thickness_transcripts_high_quality`: the 5th-to-95th percentile range of the Q20 gene
  transcripts' z is 4.84 µm in Breast (recorded 4.836) but 8.26 µm in Lung (recorded 7.61), per
  field of view or not. Its definition is not in the bundle.

Some metrics are empty in both bundles (the polygon counts, `fraction_of_nuclei_without_cell`,
the predesigned and custom panel splits); when a bundle fills them, they are shown as recorded.

### 1.3c The derived data of the development bundles (verified)

What the cache (`internal/cache`, roadmap wave 1 slice 4) builds from each bundle, first build on
an Apple M4 with 10 cores, reading the source files' checksums included:

| | Lung 2-FOV | Breast 2-FOV |
| --- | --- | --- |
| Grid | 5,791 × 3,553 px at 0.2125 µm; 5 levels; 12 × 7 tiles at level 0 | 5,792 × 3,529 px; 5 levels; 12 × 7 tiles |
| Image pyramid | 1 channel, all 5 levels decoded from the bundle: 29.4 MB | 4 channels, all levels decoded: 132.5 MB |
| Transcript index | 825,885 transcripts of 537 features (746,475 of quality ≥ 20; 809,249 in a cell): 21.8 MB | 1,113,950 of 529 features (1,036,083; 964,035): 29.7 MB |
| Cell outlines | 11,898 cells and 11,898 nuclei: 7.4 MB | 7,275 cells and 7,020 nuclei: 5.4 MB |
| In all | 58.6 MB in 2.0 s | 167.5 MB in 4.1 s |

(Sizes since wave 1, slice 6, whose QC maps added three groups of transcripts to the density grids:
2 MB more each. Slice 4 measured 56.6 and 165.0 MB.)

- Every transcript's cell and every polygon's cell is a row of cells.parquet.
- **Simplification** (Douglas & Peucker 1973) to half a pixel of each level keeps, of the cell
  outlines' vertices, 48%, 33%, 26% and 21% at levels 1–4 in Lung (297,356 at level 0) and 68%,
  45%, 32% and 24% in Breast (181,804): five to six vertices a cell at the coarsest level, the
  closing one included.
- **Density grids are as large as the points** before compression: at the finer levels nearly
  every transcript is alone in its bin (a level-1 bin is 1.7 µm). Deflating every block made the
  Lung index 42.4 MB → 19.7 MB; the density blocks deflate to 28%, the points to 65%.
- **Image tiles** deflate to 59% of their 16-bit samples as they are, and to 48% with the low and
  high bytes in separate planes, at half the time (Breast, deflate at its fastest level).
- Reopened unchanged, a dataset's status takes under a millisecond: its files are looked at, not
  read.

### 1.3d The development bundles in the viewer (verified)

- **Cell counts from the transcripts:** for every one of the 19,173 cells of both bundles, the
  Q20 gene transcripts assigned to the cell in transcripts.parquet are exactly cells.parquet's
  `transcript_counts`. A cell's top genes can therefore be counted from the transcript index
  without the matrix.
- **Segmentation method per cell:** XOA 2.0's cells.parquet has no `segmentation_method` column.
  cells.zarr.zip's `polygon_sets/1/method` indexes its `segmentation_methods` for each cell of its
  `cell_id` array, which lists the cells in cells.parquet's order.
- **Default display windows** (the 1st and 99.5th percentiles of the coarsest level's non-zero
  samples), Breast: DAPI 73–4,337 (largest 7,698), ATP1A1/CD45/E-Cadherin 109–2,272, 18S
  91–2,763, alphaSMA/Vimentin 33–2,782. The four stains added together are nearly white, so the
  viewer shows the nuclear and boundary stains at first.
- **Density scale:** at the coarsest density level (bins of 13.6 µm), the 99th percentile of
  every gene's bins together is 377 Q20 transcripts in Breast.

### 1.4 Images (verified)

- BigTIFF; uint16, one sample per pixel; tiles of 1024×1024.
- Pyramid levels are stored in **SubIFDs** (four per image in the 2-FOV files).
- OME-XML is in the first IFD's ImageDescription. The morphology-focus images are a **multi-file
  OME-TIFF**: each file's XML describes every channel (DAPI, boundary, interior RNA, interior
  protein in the Breast bundle) and names the file holding each by its UUID; each channel's
  "Purpose" is a MapAnnotation. `morphology.ome.tif` is a z-stack (11 planes, 3 µm apart, in
  consecutive IFDs).
- Compression code **34712, JPEG 2000**. Each tile is a **JP2 file** (signature, `ftyp`, `jp2h`,
  then the codestream box), not a bare codestream, written by OpenJPEG 2.5.1 (its COM marker says
  so). Every tile is 1024×1024, one 16-bit unsigned component, one image tile, 64×64 code blocks
  with no style flags, one quality layer. Two profiles, by level:
  - **full resolution:** **irreversible 9/7 wavelet with scalar expounded quantization** (lossy), 5
    decomposition levels, 256×256 precincts, RPCL progression;
  - **every pyramid (SubIFD) level:** **reversible 5/3** (lossless), LRCP, default precincts, no
    quantization. (Of the 918 tiles of the two bundles, 648 are 9/7 and 270 are 5/3.)
- 10x's image-conversion guide recommends ZLIB or lossless JPEG 2000 compression, 1024-pixel tiles
  and a downsampling factor of 2 for images that Xenium Explorer opens. Post-Xenium H&E images
  supplied by users therefore arrive with other compressions, which SituCraft also needs: JPEG
  (Go's `image/jpeg`), deflate/ZLIB and LZW.

### 1.5 Specifications SituCraft implements

| Format | Specification |
| --- | --- |
| Zstandard | RFC 8878, *Zstandard Compression and the application/zstd Media Type* (Collet & Kucherawy 2021); frame checksums with XXH64 (Yann Collet, xxHash specification) |
| LZ4 block | LZ4 Block Format Description (lz4 project) |
| Blosc | Blosc format description (c-blosc project) |
| Snappy | Snappy framing and format description |
| Parquet | Apache Parquet format specification; Apache Thrift compact protocol |
| Zarr | Zarr storage specification v2 and v3 |
| OME-NGFF | OME-NGFF (OME-Zarr) specification, for SpatialData export |
| SpatialData | SpatialData on-disk format (Marconato et al. 2025, doi:10.1038/s41592-024-02212-x) |
| AnnData | AnnData on-disk format specification (Zarr) (Virshup et al. 2024, doi:10.21105/joss.04371) |
| TIFF | TIFF 6.0; BigTIFF; OME-TIFF specification |
| JPEG 2000 | ITU-T Rec. T.800, ISO/IEC 15444-1 (JPEG 2000 Part 1 core coding system) |
| GeoJSON | RFC 7946 |
| Matrix Market | NIST Matrix Market exchange format |

### 1.6 JPEG 2000 decoding plan

The decoder follows T.800 in this order: the JP2 boxes (Annex I) around the codestream,
codestream markers (SIZ, COD, COC, QCD, QCC, SOT/SOD), tag trees and packet headers (tier 2), EBCOT
block decoding with the MQ arithmetic decoder (tier 1, including the bypass and termination
modes), dequantization, the inverse 9/7 irreversible and 5/3 reversible wavelets, and the inverse
component transform when present. Xenium's profile (above) is made fast first. SituCraft's
choices:
- Decode whole 1024×1024 tiles once into the tile cache rather than decoding on demand.
- Run code blocks in parallel.
- Implement only the Part 1 features that real files use, refusing anything else with a clear
  message.

Correctness is checked against an independent decoder (OpenJPEG) on every tile of both
development bundles. Reversible streams must match exactly. Irreversible streams, which Xenium's
are, must match within ±1 at every pixel: the 9/7 transform runs in floating point, and two
conformant decoders legitimately differ in the last rounding (ISO/IEC 15444-4 states conformance
for lossy decoding as error bounds, not equality). The maximum difference, the share of differing
pixels and the RMSE are recorded. Measured on all 918 tiles: the 270 reversible tiles are
identical; the 648 irreversible tiles are within ±1 (0.74% of samples differ by 1, RMSE 0.086).
The difference has one cause: OpenJPEG 2.5.4's output corresponds to a 13-bit fixed-point
approximation of the 9/7 high-pass scaling (13318/8192 for 2/K), where SituCraft uses the
standard's 1/K; with that value substituted (a test-only option), the two agree bit for bit on
every tile. OpenJPEG 2.5.0 (Ubuntu 24.04's, which CI installs) is within ±1 too but follows
neither constant exactly (on the Lung bundle's tiles, 1.03% of samples differ by 1 with the
standard's constant, about 1.3% with 2.5.4's), so the tests require the bit-for-bit match only
from OpenJPEG 2.5.4.

### 1.7 Xenium Onboard Analysis 1.0 bundles (verified, wave 5, slice 1)

The pathology fixture's bundle (§7) was written by XOA 1.0.1 (`analysis_sw_version`
"Xenium-1.0.1"). What differs from XOA 2.0, each now read:
- **Cells are numbered,** not named: `cell_id` is INT32 in `cells.parquet` and both boundary
  tables (from 1), and in `transcripts.parquet` (−1 for a transcript in no cell). A number is still
  an identity: SituCraft reads it as its decimal text. The check that IDs have XOA 1.3's form says
  so (a note).
- **The count matrix** (`cell_feature_matrix.zarr.zip`) keeps `cell_features/data`, `indices` and
  `indptr` by feature, as later versions do, but no `cell_id` array: `number_cells` in the group's
  attributes, its cells in `cells.parquet`'s order, numbered from 1. The checks of the matrix
  against the cells (counts, order) hold.
- **Parquet footers that lie:** the files were written by fastparquet 1.0.0, whose footer gives
  1,000,000 rows (the first row group's) for files of 42,638,083 (`transcripts.parquet`, 43 row
  groups) and 2,181,086 (`cell_boundaries.parquet`). pyarrow 26 reads the same row groups.
  SituCraft trusts the row groups and keeps the footer's figure.
- **`transcripts_per_cell` is a decimal** (166.0) in `experiment.xenium`.
- **Its metrics are not all its data's:** 4 of the 9 recomputable metrics agree; the median
  transcripts per cell is recorded as 166, the transcripts (quality 20 or more) give 164; 17,358
  cells' counts differ from `cells.parquet`'s, and 9,148 cells' totals are not the sum of their
  parts. The checks say so as warnings, as for any bundle; nothing is fatal.
- Not taken for the fixture: `morphology.ome.tif` (the z-stack, 5.2 GB), `morphology_mip.ome.tif`,
  the CSV copies, `cells.zarr.zip` and `transcripts.zarr.zip` (Xenium Explorer's). Without the
  cell masks, the instrument's segmentation is drawn from its polygons.

### 1.8 Post-Xenium images (verified, wave 5, slice 1)

Xenium Explorer opens a post-Xenium image as a pyramidal, tiled OME-TIFF (10x Genomics, Xenium
Explorer: "Image alignment" and "Image file conversion"), and so does SituCraft (`internal/slide`):
its full-resolution image directory and its reduced levels in SubIFDs, read a tile at a time where
it lies.

| Image (the fixture) | Written by | Pixels | Levels | Tiles |
| --- | --- | --- | --- | --- |
| H&E, `…_he_image.ome.tif` (1.43 GB) | Bio-Formats 6.7.0 (QuPath's export), big-endian classic TIFF | 30,786 × 24,241 RGB, 8 bits, stored together; 0.363788 µm in its OME-XML | 7 (to 481 × 378) | 1024², deflate, no predictor |
| IF, `…_if_image.ome.tif` (64 MB) | tifffile 2023.7.4 | 14,239 × 9,777, three channels (CD20, HER2, DAPI) of 8 bits, a directory each; 0.2125 µm in its OME-XML | 8 (to 111 × 76) | 1024², JPEG 2000 |

Every level of both, read through SituCraft's tiles, is the pixels tifffile 2026.10.15 with
imagecodecs 2026.10.10 reads (SHA-256 of each level of each channel;
`validation/reference/images/`). A deflate tile of the H&E (3 MB) decodes in about 25 ms; the
page draws tiles as they arrive.

**JPEG tiles** (TIFF Technical Note 2) are decoded by Go's `image/jpeg`, with the tables an image
keeps in its JPEGTables tag put before an abbreviated stream. Go's decoder repeats each chroma
sample over the pixels it covers; libjpeg-turbo, which tifffile, OpenSlide and most slide readers
decode with, interpolates them by default (its "fancy upsampling": weights 3 and 1 between
neighbouring samples, in each direction subsampled, rounding with alternating offsets). On a 4:2:0
H&E-like image the two differed by up to 74 levels where hues change. SituCraft interpolates
likewise: within 3 levels of libjpeg-turbo's pixels on tifffile's JPEG files, 99% within 1 (the
transform's and the colour conversion's rounding).

**Flat images are refused** (no reduced levels and larger than 4,096 pixels a side), as are images
stored in strips: Xenium Explorer requires the same, and 10x's conversion guide makes one.

## 2. Coordinates and units

- Transcript and polygon coordinates are in **microns**. Image pixels are related to them by
  `pixel_size` from `experiment.xenium` (0.2125 µm at full resolution) and, for masks, by
  `masks/homogeneous_transform`.
- Coordinate systems are named and explicit in SituCraft (`global` in µm; each image's pixel
  system; each registered image's system). SituCraft never infers pixels versus microns from
  values.
- Post-Xenium images come with a 3×3 alignment matrix (CSV) mapping image pixels to Xenium pixels
  (§2.1).

### 2.1 Post-Xenium alignment (verified, wave 5, slice 1)

Xenium Explorer saves an image's alignment as a transformation matrix (`<image>_imagealignment.csv`:
3×3, row-major, its last row 0, 0, 1; "the transformation value units are in pixels") or as
keypoints (`<image>_keypoints.csv`: `fixedX`, `fixedY` on the DAPI image, `alignmentX`,
`alignmentY` on the image), and fits keypoints by least squares as a similarity (translation, one
scale, rotation, reflection; after Horn 1987, in two dimensions). SituCraft reads both
(`internal/register`); keypoints are fitted in closed form (Umeyama 1991), with and without a
reflection, the better kept, and each keypoint's residual recorded.

**The matrix maps the image's pixels to the Xenium image's**, verified on the fixture: its scale is
the image's pixel size over the Xenium image's (H&E: 1.71252 against 0.363788 / 0.2125 = 1.71194;
a step of one H&E pixel is 0.3639 µm), and the inverse puts the centre of the Xenium image on the
H&E at (16,342, 14,283) of 30,786 × 24,241. Drawn so, the H&E's structures lie on the DAPI image's.
Both of the fixture's alignments change orientation: the H&E's mirrors top to bottom (turned 0.3°),
the IF's turns −179.6°.

**An image's own pixel size does not place it:** the fixture's IF image says 0.2125 µm, the Xenium
image's; its alignment makes its pixels 0.7387 µm (scale 3.476). SituCraft places an image by its
alignment alone, then in µm by the dataset's pixel size; the metadata's figure is kept, and the
artifact notes when the two differ by more than 5%.

### 2.2 Landmark fits (verified, wave 5, slice 2)

An image is aligned by landmarks, each a place marked on both images: its fixed position (µm, the
dataset's coordinates) and its moving position (the image's full-resolution pixels, (0, 0) the
corner of its first). Two models, fitted by least squares (`internal/register/fit.go`):

- **A similarity:** translation, one scale and a rotation, and a reflection when that fits better
  (Umeyama 1991, in closed form, with and without a reflection; Xenium Explorer fits its keypoints
  so). Three landmarks at least: two fix a similarity up to its reflection, the third tells it.
- **An affine transform:** a scale along each axis and a shear as well (scanners' pixels need not
  be square, and a section may stretch one way). From the normal equations of the centred
  positions; three landmarks not on a line at least.

**How well a fit fits** is said two ways, each landmark's:
- **residual:** its distance from where the fit puts it, the landmark among those fitted;
- **leave-one-out error:** its distance from where the fit of the others puts it (cross-validation,
  Stone 1974), when the others still fit the model (two for a similarity, which keeps the full
  fit's reflection; three not on a line for an affine transform).

Residuals understate the error where no landmark is: a fit is drawn toward its own landmarks. A
square of landmarks, one placed 2 µm off, worked by hand: an affine fit leaves every residual at
0.5 µm, and each landmark left out is predicted 2 µm off (the three others fit exactly); a
similarity's residuals are 0, 0.71, 0.71 and 1 µm, its leave-one-out errors 0, 1.41, 1.41 and 2 µm.
On the simulated images (§3.4a), twelve landmarks at nuclei fitted as an affine transform leave
residuals of 0.16 µm root-mean-square; left out, 0.21 µm; the fit's true error over the section is
0.31 µm (a smooth deformation no affine transform follows).

**The page fits as the program does,** bit for bit: the same operations in the same order, every
product rounded on its own and square roots IEEE 754's (`web/lib/transforms.js`; a checksum of 600
fits agrees). What the page shows while landmarks are placed is what is recorded.

**10x's alignments are similarities.** The fixture's H&E matrix is exactly a reflected similarity
(its A is −E, its B is D, to the last bit) and its immunofluorescence matrix exactly a similarity
turned −179.6°: as fitted from keypoints. Keypoints made with either, written as Xenium Explorer
writes them and read back, fit to it within 1e-15.

**Versions.** Each alignment is an artifact of kind `transform`, named by its description's
checksum: its source and destination systems, method, model, transform, landmarks, fit, who made
it, a note and its parent version. Which one places an image is a decision with its reason; the
latest names it. An image with none is not drawn on the tissue: a place guessed from its metadata
would look aligned and not be.

### 2.3 Automatic registration (verified, wave 5, slice 3)

A post-Xenium image is registered to the Xenium image by the nuclei, which both show: DAPI against
an H&E's haematoxylin (colour deconvolution, Ruifrok & Johnston 2001, with their stain colours and
the glass's light each channel's 99.5th percentile over the image's smallest level) or an
immunofluorescence image's nuclear stain (`internal/nuclei`, `internal/register/auto.go`).

1. **The search,** on both images about 256 pixels across. An image's metadata cannot be trusted
   for its pixel size (the fixture's immunofluorescence image gives the Xenium image's), so scales a
   quarter octave apart are swept, two octaves each way of what it says (0.5 µm when it says
   nothing, three octaves each way), the image as it is and mirrored. For each, the log-polar forms
   of both magnitude spectra, emphasized at high frequencies (Reddy & Chatterji 1996), correlate at
   the turn and the scale left; the two turns half a turn apart (spectra cannot tell them) each
   with its shift by phase correlation (Kuglin & Hines 1975) and the shifts a correlation's
   wrapping allows. The candidate whose images' normalized mutual information (Studholme, Hill &
   Hawkes 1999) is highest, over at least 30% of the Xenium image, is kept. An image a scale makes
   over four times the Xenium image's size is not tried.
2. **Refinement** over a pyramid, each level twice as fine to about 4 µm a pixel: the model's
   parameters (a shift, a turn, a scale, and for an affine transform an aspect and a shear, about
   the Xenium image's centre) moved by a compass search (Hooke & Jeeves 1961) maximizing the mutual
   information, which needs no gradient.
3. **Local matches:** patches of 128 pixels, a sixth of the tissue's smaller side (32 to 160 µm),
   where the Xenium image has tissue (at most 200), the image drawn under the transform in each, the
   shift found by phase correlation with Foroosh, Zerubia and Berthod's (2002) sub-pixel estimate
   (a parabola's is biased toward whole pixels by up to a fifth of a pixel). A patch is kept when its
   peak stands six standard deviations above the surface and the shift is within a quarter of the
   patch. The model is fitted to the matches (as landmarks, §2.2), those over three robust standard
   deviations (1.4826 × the median residual) dropped until none is; twice.
4. **A deformation,** when asked (`bspline.go`): cubic B-splines (Rueckert et al. 1999) on a grid of
   about eight cells across the tissue (no finer than two patches, at most 24 across), fitted to
   what the model leaves of the kept matches by least squares with a penalty on the control
   points' second differences. The smoothness is the one of 0.01, 0.1, 1 and 10 that best predicts
   each fifth of the matches from the rest, raised fourfold until the deformation's Jacobian
   determinant stays above 0.5. It is kept only when it predicts the matches clearly better
   (by 10%) than the model alone: fitted to matches' noise, it would make an alignment worse (on an
   undeformed simulated image, from 0.19 to 0.40 µm from the truth). It needs 25 kept matches:
   fewer cross-validate too coarsely, and leave it to extrapolate (on a 160 × 120 µm simulated
   tissue's 13, a deformation was kept that put the section's edge 2.8 µm off).

**The same transform on any computer:** every product rounded on its own (`mul`), the FFT's
twiddles and every sine, exponential and logarithm `dmath`'s, histograms counted in integers, cores
sharing only work whose result does not depend on how; a fingerprint of a registration's transform
and deformation is pinned in a test that continuous integration checks on another kind of
processor. Under Go's race detector a registration is about fifteen times slower; a small one runs
there.

**On simulated images** (§3.4a; 360 × 280 µm, the H&E deformed by up to 2 µm, the
immunofluorescence image not deformed; the metadata's pixel size right or 0.2125 µm):

| Image | Found by the search | The model's fit from the truth (RMS) | With a deformation |
|---|---|---|---|
| H&E (0.3638 µm, mirrored, turned 2.5°) | 0.362 µm, mirrored, 2.4° | 0.87 µm | 0.20 µm (kept: 0.30 µm held out against 0.88) |
| Immunofluorescence (0.65 µm, turned −177°) | 0.652 µm, −177.4° | 0.13–0.16 µm | not kept (held out 0.69–0.73 µm against 0.69–0.73) |

**On the pathology fixture,** without the images' alignments, against 10x's alignment over the
Xenium image (7.5 × 5.5 mm) every 100 µm:

| Image | Found by the search | Patches kept | From 10x's alignment | Time |
|---|---|---|---|---|
| H&E (0.3639 µm, mirrored, turned 0.28°) | 0.3640 µm, mirrored, 0.3° | 140 of 143 | 0.61 µm RMS, at most 0.91 | 10 s |
| Immunofluorescence (0.7387 µm, turned −179.6°; its metadata 0.2125) | 0.7412 µm, −179.6° | 139 of 140 | 0.68 µm RMS, at most 1.19 | 3 s |

The kept matches lie 0.91 µm (H&E) and 1.04 µm (immunofluorescence) from the similarity fitted to
them: what a similarity cannot follow plus the matches' error. For the immunofluorescence image a
deformation (941 µm cells) predicts each fifth of the matches 0.49 µm off against 1.05 µm by the
similarity: it is deformed against DAPI, by up to 2.6 µm, which no similarity (10x's included)
follows. The H&E's deformation predicts 0.87 against 0.93 µm and is not kept.

## 3. Segmentation

### 3.1 Nucleus expansion (verified, wave 2, slice 3)

**The method.** Each nucleus grows into a cell of its own by up to *d* µm (Xenium's default
5 µm), and no pixel belongs to two cells: a pixel belongs to the nearest nucleus within *d*,
measured between pixel centres to the nucleus's nearest pixel. That is a Voronoi partition of
the plane by the nuclei, clipped at distance *d* (Okabe et al., *Spatial Tessellations*, 2nd ed.,
Wiley 2000). Distances are exact: the distance transform of Felzenszwalb and Huttenlocher (2012),
which also gives each pixel its nearest nucleus pixel. A pixel equally near two nuclei goes to the
upper one in its column, else the left one in its row. The image is processed in bands of rows
with a margin of *d*, on every core; the result is the same for any band height and number of
workers (`internal/segment/expand`, tested against brute force). A transcript belongs to the cell
holding the pixel it lies in. Each cell's outline follows its largest 4-connected piece's pixel
edges, simplified to 0.5 pixel (Douglas and Peucker 1973).

**What the bundles hold** (measured on the development bundles):

- `cells.zarr.zip` holds `masks/0` (nuclei) and `masks/1` (cells), 32-bit labels at the
  morphology image's resolution (Lung: 5,791 × 3,553 pixels in chunks of 445 × 724), with
  `masks/homogeneous_transform` scaling µm to pixels by 4.705882 (1/0.2125). Label *k* of
  `masks/1` is `cells.parquet`'s row *k*−1.
- The instrument assigns a transcript to the cell holding pixel (⌊*x*/0.2125⌋, ⌊*y*/0.2125⌋) of
  `masks/1`: on Lung that gives 825,869 of the 825,878 transcripts inside the mask their
  `cell_id`; the other 9 lie on a pixel's exact edge, and 7 more lie outside the mask. `overlaps_nucleus` follows `masks/0` the same way
  (17 such edge cases).
- In Lung, segmented wholly by nucleus expansion, `masks/0` label *k* is cell *k*'s nucleus. In
  Breast, whose cells were mostly drawn from cell stains, the nucleus mask has 7,312 labels for
  7,275 cells and its labels are not the cells': 255 cells hold no nucleus and 268 several. So
  SituCraft pairs each nucleus with the instrument's cell holding most of its pixels, and grows
  every nucleus into a cell of its own.
- The nucleus polygons (`nucleus_boundaries.parquet`) outline the mask, simplified; their
  vertices lie on their edge pixels' own coordinates (pixel (*i*, *j*) at (*i*, *j*) × 0.2125 µm).
  A bundle without `cells.zarr.zip` has its polygons drawn instead, a pixel belonging to a nucleus
  when that point is inside the polygon or on its edge: on Lung the drawn nuclei agree with the
  mask on 98.56% of their pixels (the rest where a simplified edge cuts a corner), and the cells
  grown from them with the instrument's on 97.88%.

**The Lung bundle's 5 µm expansion, reproduced** (5 µm = 23.53 pixels; reference numbers of
`TestRealExpansionReproducesTheInstruments`, whose tolerances are in brackets):

| | SituCraft | The instrument | Agreement [tolerance] |
| --- | --- | --- | --- |
| Cells | 11,898, one for each nucleus | 11,898 | paired one to one [exact] |
| Pixels in the same cell | | | 98.06% of the 14,367,793 either gives a cell [≥ 98%] |
| Transcripts in the same cell, or in none in both | | | 98.94% of 825,885 [≥ 98.5%] |
| Cell area | median 48.7 µm² | | median difference +0.25%; 97.0% within 5% [≥ 96%], 99.95% within 10% [≥ 99.5%] |
| Gene transcripts (Q20) assigned | 98.28% | 97.97% | SituCraft 0.31 points more [0 to 0.5] |

Every difference is one of three kinds, and the test requires each to be:

1. **The rim** (156,308 pixels, 2,534 transcripts): pixels within 5 µm of their nucleus that the
   instrument's cells do not reach, each 4.56 to 4.98 µm (21.5 to 23.4 pixels) from it [4.5 to
   5 µm]. The instrument's cells reach 23 pixels from their nucleus along the image's axes and
   (16, 16) pixels along its diagonals, less far than 23.53 pixels in the directions between; no
   pixel of an instrument cell lies more than 23 pixels from its nucleus. SituCraft keeps the
   method's definition, an exact Euclidean distance, and records the difference.
2. **Boundaries between neighbours** (122,016 pixels, 6,182 transcripts): pixels the two give
   neighbouring cells, each at most 0.34 µm (1.6 pixels) nearer to SituCraft's cell's nucleus than
   to the instrument's [0.4 µm]: almost equally far from both, where ties and near-ties are broken
   differently.
3. **The instrument's records** (8 transcripts): transcripts whose `cell_id` is not the cell the
   instrument's own mask holds at their pixel.

No pixel or transcript is in an instrument's cell and in none of SituCraft's. 68 cells are in more
than one piece (the largest is outlined). The whole expansion of Lung (growing, assigning 825,885
transcripts, counting, outlining 11,898 cells, comparing) takes about a second on an Apple M4.

**Breast** (5 µm): 7,312 cells; of the instrument's 7,275, 6,752 hold one nucleus, 255 none and
268 several. 72.1% of pixels and 80.7% of transcripts are in the same cells, and 94.7% of gene
transcripts are assigned (the instrument: 86.6%): there the instrument drew most cells from their
stains (3,300 by boundary stain, 3,913 by interior stain, 62 by nucleus expansion), so an expansion
is another segmentation, not a reproduction.

### 3.2 Transcript-informed segmentation (after Jones et al. 2025)

Jones DC, Elz AE, Hadadianpour A, Ryu H, Glass DR, Newell EW. Cell simulation as cell
segmentation. *Nat Methods* 2025;22(6):1331–1342. doi:10.1038/s41592-025-02697-0.

A cell is a region of space whose molecules are a Poisson process with its own rate for each
gene, over a background with a rate for each gene everywhere; a segmentation is the labelling of
space that makes the observed molecules most probable, given what cells' expression and sizes are
like. SituCraft samples labellings from their posterior, as the publication does. The model and
its updates are written out below (notation is SituCraft's), then the sampler.

#### The model (verified, wave 3, slice 2)

**Space.** The region $R$ (a patch of the section, of volume $|R|$ µm³) is a lattice of voxels,
each of volume $\nu$ (its side in x and y times the section's depth divided by the number of z
layers). A labelling $\sigma$ gives each voxel $v$ a cell $\sigma(v) \in \{1, \dots, C\}$ or the
background, $\sigma(v) = 0$. Cell $c$'s volume is $V_c = \nu \, |\{v : \sigma(v) = c\}|$. A run
can sample in stages of finer voxels, each splitting the last's without changing which cell owns
a region (the sampler, below).

**Molecules.** Transcript $i$ ($i = 1, \dots, N$; gene transcripts of quality 20 or more) has
gene $g_i$ and lies in voxel $v(i)$; write $\sigma(i) = \sigma(v(i))$. The molecules of gene $g$
are a Poisson process of intensity $\lambda_{0g} + \lambda_{\sigma(x) g}$ at $x$ (per µm³; the
second term is absent over the background): the background's rate $\lambda_{0g}$ everywhere and
cell $c$'s rate $\lambda_{cg}$ within it. The log-likelihood of the labelling and the rates is

$$\log p(\text{molecules} \mid \sigma, \lambda) = \sum_i \log\bigl(\lambda_{0 g_i} + \lambda_{\sigma(i) g_i}\bigr) - |R| \sum_g \lambda_{0g} - \sum_c V_c \Lambda_c, \qquad \Lambda_c = \sum_g \lambda_{cg},$$

with $\lambda_{0\cdot}$ for $\sigma(i) = 0$ read as zero. Each molecule in a cell came from the cell
or from the background: the attribution $a_i$, with
$P(a_i = \text{cell}) = \lambda_{\sigma(i) g_i} / (\lambda_{0 g_i} + \lambda_{\sigma(i) g_i})$
(the superposition of two Poisson processes). Given the attributions, cell $c$'s count of gene
$g$, $n_{cg}$, is Poisson with mean $V_c \lambda_{cg}$, and the background's, $n_{0g}$ (its
molecules in cells and outside them), Poisson with mean $|R| \lambda_{0g}$.

**The background, of two kinds (wave 3, slice 7).** Molecules from no cell arise two ways: decoding
errors and probes bound off target, which fall on every codeword alike (false decodes), and RNA
loose in the tissue, which falls on each gene by its own rate (ambient RNA). So
$\lambda_{0g} = f + a_g$: a false-decode rate $f$ shared by every codeword and an ambient rate
$a_g$ for each gene. The panel's negative-control codewords (probes and codewords that match no
gene, $C$ of them, seen at any quality) are false decodes only: their $n_c$ transcripts of quality
20 or more in the region are Poisson with mean $|R|\,C f$, evidence of the background that no cell
can take. *SituCraft's extension:* the publication has one background rate per gene and does not
model the negative controls.

**Expression.** Each cell belongs to one of $K$ components (broad expression states),
$z_c \in \{1, \dots, K\}$, with probabilities $\pi \sim \mathrm{Dirichlet}(\alpha, \dots, \alpha)$.
Given its component, a cell's rates are gamma (shape $r$, rate $\beta$):

$$\lambda_{cg} \mid z_c = k \sim \mathrm{Gamma}(r_{kg}, \beta_{kg}), \quad r_{kg} \sim \mathrm{Gamma}(a_r, b_r), \quad \beta_{kg} \sim \mathrm{Gamma}(a_\beta, \phi_g), \quad \phi_g \sim \mathrm{Gamma}(e_\phi, f_\phi), \quad f, a_g \sim \mathrm{Gamma}(a_0, b_0).$$

A cell's count, its rate integrated out, is negative binomial:
$n_{cg} \sim \mathrm{NB}(r_{kg}, p_{ckg})$, $p_{ckg} = V_c / (V_c + \beta_{kg})$, of mean
$V_c r_{kg} / \beta_{kg}$. The gene-level $\phi_g$ shares each gene's scale across components, so a
component with few cells is drawn toward the gene's typical rate.

*SituCraft's choice:* the publication parameterizes the negative binomial's probability
logistically and samples it with Pólya–Gamma variables (Polson, Scott & Windle 2013, *JASA*
108:1339–1349, doi:10.1080/01621459.2013.829001; as in Zhou M, Li L, Dunson D, Carin L.
Lognormal and gamma mixed negative binomial regression. *ICML* 2012). SituCraft places a gamma
prior on the rate $\beta_{kg}$ instead, conjugate given the cells' rates. Every expression update
is then an exact draw, with no Pólya–Gamma variates, whose exact sampling for the large,
non-integer shapes that arise here ($n_{cg} + r_{kg}$) is costly and whose fast samplers
approximate. The sampler's target is then exactly the model's posterior, which slice 3 tests by
enumeration.

**Volume.** A cell's volume is log-normal by component,
$\log V_c \mid z_c = k \sim \mathcal{N}(m_k, s_k^2)$, with the conjugate normal–inverse-gamma
prior $s_k^2 \sim \mathrm{InvGamma}(a_s, b_s)$, $m_k \mid s_k^2 \sim \mathcal{N}(m_0, s_k^2/\kappa_0)$.

**The nuclear prior.** A transcript over a nucleus in the section has that nucleus's cell as its
prior cell $q_i$ (otherwise $q_i = 0$). The labelling's prior weighs each such transcript by
$\rho$ if it lies in $q_i$ and $1 - \rho$ if it does not ($\rho = 0.8$, the publication's).

**The labelling's prior** is
$p(\sigma \mid z, m, s) \propto I(\sigma) \prod_c \mathrm{LogNormal}(V_c; m_{z_c}, s^2_{z_c}) \prod_{i : q_i \ne 0} \rho^{[\sigma(i) = q_i]} (1 - \rho)^{[\sigma(i) \ne q_i]}$,
where $I(\sigma)$ is 1 when every cell is connected, within its perimeter bound and non-empty, and
0 otherwise (slice 3). *The one approximation.* This prior's normalizing constant, a sum over
every labelling, depends on the volume parameters and the components, and cannot be computed
(the "doubly intractable" problem: Murray I, Ghahramani Z, MacKay DJC. MCMC for
doubly-intractable distributions. *UAI* 2006). SituCraft updates the components and the volume
parameters as though the cells' volumes were independent log-normal draws, which treats that
constant as fixed. With one volume prior for every component, held fixed (`exact` mode), the
constant is a constant and the sampler is exact; the tests that check exactness use that mode.

**The updates.** One sweep of the parameters, given the labelling, draws in this order:

1. **Attributions:** each molecule in a cell from the cell with probability
   $\lambda_{\sigma(i) g_i} / (\lambda_{0 g_i} + \lambda_{\sigma(i) g_i})$; the counts $n_{cg}$
   and $n_{0g}$ follow.
2. **Background:** each gene's $n_{0g}$ splits into false decodes and ambient RNA,
   $k_g \sim \mathrm{Binomial}(n_{0g}, f / (f + a_g))$ (each a false decode with that
   probability); then $a_g \sim \mathrm{Gamma}(a_0 + n_{0g} - k_g,\; b_0 + |R|)$ and
   $f \sim \mathrm{Gamma}(a_0 + n_c + \sum_g k_g,\; b_0 + |R|(C + G))$, every codeword exposed
   to false decodes over the region; $\lambda_{0g} = f + a_g$. The binomial is drawn exactly, by
   splitting large counts with beta order statistics (Devroye L. *Non-Uniform Random Variate
   Generation.* Springer, 1986, ch. X.4).
3. **Components**, each cell's rates integrated out:
   $P(z_c = k) \propto \pi_k \, \mathrm{LogNormal}(V_c; m_k, s_k^2) \prod_g \mathrm{NB}(n_{cg}; r_{kg}, p_{ckg})$,
   where $\log \mathrm{NB}(n; r, p) = \sum_{j=0}^{n-1} \log(r + j) + r \log(1 - p) + n \log p - \log n!$
   (the last term is the same for every $k$ and is dropped).
4. **Shapes**, the rates integrated out, by Chinese-restaurant-table augmentation (Zhou M, Carin
   L. Negative binomial process count and mixture modeling. *IEEE TPAMI* 2015;37:307–320.
   doi:10.1109/TPAMI.2013.211): for each cell of component $k$,
   $\ell_{cg} \sim \mathrm{CRT}(n_{cg}, r_{kg})$, the sum of $n_{cg}$ Bernoulli draws of
   probabilities $r/(r + j - 1)$, $j = 1, \dots, n_{cg}$; then
   $r_{kg} \sim \mathrm{Gamma}\bigl(a_r + \sum_c \ell_{cg},\; b_r + \sum_{c : z_c = k} \log(1 + V_c/\beta_{kg})\bigr)$.
5. **Rates:** $\lambda_{cg} \sim \mathrm{Gamma}(r_{z_c g} + n_{cg},\; \beta_{z_c g} + V_c)$.
6. **Rate parameters:** $\beta_{kg} \sim \mathrm{Gamma}\bigl(a_\beta + N_k r_{kg},\; \phi_g + \sum_{c : z_c = k} \lambda_{cg}\bigr)$, $N_k$ the component's cells.
7. **Gene scales:** $\phi_g \sim \mathrm{Gamma}(e_\phi + K a_\beta,\; f_\phi + \sum_k \beta_{kg})$.
8. **Proportions:** $\pi \sim \mathrm{Dirichlet}(\alpha + N_1, \dots, \alpha + N_K)$.
9. **Volumes:** for each component, with $y_c = \log V_c$ over its cells, mean $\bar y$:
   $\kappa_n = \kappa_0 + N_k$, $m_n = (\kappa_0 m_0 + N_k \bar y)/\kappa_n$, $a_n = a_s + N_k/2$,
   $b_n = b_s + \tfrac12 \sum_c (y_c - \bar y)^2 + \kappa_0 N_k (\bar y - m_0)^2 / (2\kappa_n)$;
   $s_k^2 \sim \mathrm{InvGamma}(a_n, b_n)$, $m_k \sim \mathcal{N}(m_n, s_k^2/\kappa_n)$
   (skipped in `exact` mode).

Steps 3 and 4 integrate the cells' rates out, and step 5 draws them anew before anything
conditions on them (step 6, and step 1 of the next sweep): a partially collapsed Gibbs sampler,
valid in this order (van Dyk DA, Park T. Partially collapsed Gibbs samplers: theory and methods.
*JASA* 2008;103:790–796. doi:10.1198/016214508000000409). Collapsing the rates lets a cell change
component without its rates holding it in place. Every cell's draws in steps 1, 3, 4 and 5 come
from its own random stream, named by the sweep, the step and the cell, so the cells are updated in
parallel with the same result on any number of threads.

**A voxel's move** from cell $a$ to cell $b$ (either may be the background, $0$; slice 3 proposes
them) changes the log-posterior, the attributions integrated out, by

$$\Delta = \sum_{i \in v} \log\frac{\lambda_{0 g_i} + \lambda_{b g_i}}{\lambda_{0 g_i} + \lambda_{a g_i}} - \nu(\Lambda_b - \Lambda_a) + \Delta_{\text{volume}} + \sum_{i \in v,\, q_i \ne 0} \Delta_{\text{nuclear}}(i),$$

with the background's $\lambda_{0\cdot}$ and $\Lambda_0$ read as zero in the cell terms;
$\Delta_{\text{volume}}$ the change in $\log \mathrm{LogNormal}$ of the cells' volumes ($V_a - \nu$,
$V_b + \nu$); and $\Delta_{\text{nuclear}}(i)$ $\log\rho - \log(1-\rho)$ when the molecule moves
into its prior cell, the opposite when it moves out of it, and 0 otherwise. A move that empties a
cell is refused.

**Defaults** (SituCraft's choices; every one recorded with a segmentation):

| Parameter | Default | Why |
| --- | --- | --- |
| Components $K$ | 10 | Enough for a panel's broad cell types; empty components fall back to the prior |
| $\alpha$ | 1 | A uniform prior over the proportions |
| $a_r, b_r$ | 2, 0.5 | Shapes of mean 4 (sd 2.8): overdispersed, as single-cell counts are, without ruling out near-Poisson genes |
| $a_\beta$ | 1 | An exponential prior on each component's rate parameter about the gene's scale |
| $e_\phi, f_\phi$ | 1, 1 µm³ | Weak: rate parameters are hundreds to thousands of µm³ |
| $a_0, b_0$ | 1, 1 µm³ | Weak (on $f$ and each $a_g$): the region is of the order of $10^5$–$10^7$ µm³ |
| $m_0$ | $\log$ of the median initial cell volume | Centres the volume prior on the cells the sampler starts from |
| $\kappa_0, a_s, b_s$ | 1, 2, 0.25 | A prior mean of 0.25 for $s_k^2$: volumes within a factor of about 1.6 |
| $\rho$ | 0.8 | The publication's |

**Validation** (`internal/segment/refine`; fixed seeds, each test at the 0.001 level):
- **Each conjugate update against its analytic posterior** (Kolmogorov–Smirnov, 6,000 draws):
  the cells' and the components' rates, the gene scales, the proportions (each marginally beta),
  and the volume prior (its variance inverse gamma, its mean normal given it). The background's
  update is a data augmentation (the split), so its chain, thinned to every tenth draw, is tested
  against the exact posterior of $f$ and each $a_g$: a mixture of gammas over the splits, each
  weighted by its marginal likelihood, enumerated.
- **The components** against probabilities computed independently from the negative binomial
  and log-normal densities (χ²).
- **The shapes:** 3,000 independent chains of the augmented update, started from the prior, end
  distributed as the posterior computed by numerical integration.
- **The whole sweep** by Geweke's joint distribution test (Geweke J. Getting it right: joint
  distribution tests of posterior simulators. *J Am Stat Assoc* 2004;99:799–804.
  doi:10.1198/016214504000001132). 4,000 chains start from the prior and data drawn from the
  model (the negative controls among the data), then alternate 40 sweeps with fresh data. Thirteen
  quantities (shapes, rate parameters, scales, rates, the background's rate and its two parts,
  proportions, volume parameters, component sizes) stay distributed as the prior (two-sample
  Kolmogorov–Smirnov).
- **Moves:** in a random walk of 2,000 voxel moves, each move's computed change equals the target
  recomputed afresh (to $10^{-9}$ relative); a move that would empty a cell is refused.
- **Attributions:** each in proportion to the rates (z test); the counts follow them.
- **Behaviour:** on counts of 300 cells of three types, the components separate the types
  (purity 0.95 or more).
- **Determinism:** the same parameters on 1 and 8 workers. A pinned checksum of twenty sweeps is
  the same on arm64 and amd64.
- **Power.** Each test was shown to fail on a deliberate error. Nine errors were tried:
  - a conjugate posterior's parameter wrong (background, shapes, rates, rate parameters, gene
    scales, volumes); for the two-part background, the controls' exposure without the genes, the
    split inverted, the controls left out of $f$ (each caught by its posterior test), and the
    exposure one codeword short (caught by the joint test);
  - the volume prior dropped from the components;
  - the CRT off by one;
  - the rates drawn before the shapes, which breaks the partially collapsed order (caught only by
    the joint test, once its chains were long enough for the error to accumulate).

  Four more were tried on the moves and attributions (a sign, the nuclear prior reversed, a
  volume off by one, the attribution's share). Every one was caught.

A sweep of the parameters at the standard simulation's size (3,000 cells, 120 genes, 10
components) takes about 9 ms on ten cores; its cost grows with cells × genes × components.

#### The sampler (verified, wave 3, slice 3)

Each sweep draws the attributions and the parameters (above), then the labelling, then the
transcripts' places.

**The labelling's moves** (Metropolis–Hastings). A voxel $v$ is chosen. With probability
$p_b = 0.05$ the background is proposed for it (a "bubble", which lets gaps open between cells);
otherwise the label of one of its face neighbours, chosen uniformly. A voxel's neighbours do not
change when it moves, so the proposal's probability both ways is local,
$q(a \to b) \propto p_b [b = 0] + (1 - p_b)\, n_b(v)/n(v)$ ($n_b(v)$ of its $n(v)$ face
neighbours labelled $b$). The move is accepted with probability
$\min\{1, e^{\Delta}\, q(b \to a)/q(a \to b)\}$.

**The constraints,** each holding in both directions, so that a move is allowed exactly when its
reverse is (which detailed balance needs):
- **No cell is emptied.**
- **Connectivity.** Among the 26 voxels about $v$, those of the cell losing $v$ and those of the
  cell gaining it must each form exactly one face-connected piece touching $v$'s faces. Removing
  $v$ then cannot disconnect the first, and adding it cannot join pieces of the second into a
  loop. This is a local test of simple points (Kong TY, Rosenfeld A. Digital topology:
  introduction and survey. *Comput Vis Graph Image Process* 1989;48:357–393).
  - *SituCraft's choice:* the test applies to both cells. A test of the losing cell alone allows
    moves whose reverse it forbids. The tests below catch that error.
- **Perimeter.** In each layer a cell's perimeter, counted in voxel edges, is at most $f$ times the
  least any shape of its area can have: $2\lceil 2\sqrt{n}\rceil$ for $n$ voxels (Harary F,
  Harborth H. Extremal animals. *J Combin Inform System Sci* 1976;1:1–8), $f = 1.6$ (2 until
  slice 5).
  - *SituCraft's choice:* the publication bounds the perimeter against a circle's (a factor of
    1.3). In voxel edges, a digital disc's perimeter already exceeds the circle's by about 4/π.
    SituCraft bounds against the least digital perimeter instead. An elongated cell (an ellipse of
    aspect 4 at 45°) reaches about 1.6 times it, a thin tendril far more. $f = 1.6$ is the tightest
    bound that still admits that ellipse at any angle; it was chosen by measurement (the
    segmentation studio, below).
  - A cell that starts beyond the bound keeps its own starting ratio as its bound.

**Parallel and deterministic.** The region is cut into square blocks (40 µm, all layers), coloured
in a 2 × 2 checkerboard and shifted each sweep by an offset drawn from the sweep's stream. In each
of four phases the blocks of one colour are updated in parallel. Each block proposes as many moves
as it has voxels, at voxels chosen uniformly within it, from its own random stream.

Blocks of one colour are a block apart, so a move in one never reads a voxel another changes. A
cell may move in a phase only if its bounding box, grown by one voxel, meets exactly one of the
phase's blocks. Blocks therefore change disjoint cells, and a phase is the same as its blocks
updated one after another. A block's moves never change which cells it may move, so each phase
keeps the target. A seed gives the same result, byte for byte, on any number of threads and on
any processor.

**Repositioning.** Each transcript's true place in x and y is its observed place displaced by a
normal of σ 4 µm with probability 0.2, else of σ 0.5 µm (the publication's mixture). Its depth is
kept: a section's layer is thicker than the displacement. Each sweep proposes a place for every
transcript from that prior, about where it was observed, and accepts it with probability
$\min\{1, \Lambda(\text{new})/\Lambda(\text{old})\}$: an independence sampler, whose proposal is the
prior. $\Lambda$ is the gene's intensity at the place's voxel times the transcript's nuclear prior
there. A place outside the region is refused.

**The start.** The run starts from the instrument's cells (`cells.zarr.zip` masks/1):
- **On the first lattice:** each column takes the label most of its pixels have, and each cell
  keeps its piece holding its seed, the centre of its largest nucleus (masks/0).
- **Prior cells:** each nucleus belongs to the cell holding most of its pixels, and each
  transcript over a nucleus has that cell as its prior.
- **Small cells:** a cell left without a column takes its seed's column. If a cell still has none
  (two seeds in one voxel), the run starts at the next finer stage, and cells lost even at the
  finest are left out and counted.
- **Each finer stage** splits every voxel by whole factors, so no region changes owner.

**What is reported** is the posterior's summary over the last sweeps, not one sample: each voxel's
most frequent label, and each transcript's most frequent cell with its posterior probability (the
share of samples). A cell's 2D footprint takes, in each column, the label holding most of its
layers (the background counted like a cell; a tie between a cell and the background goes to the
cell).

At 1 µm a voxel at a cell's edge holds a transcript or two at most (in four layers, about 0.4),
so whether it belongs to the cell is decided by a few nats at most. Any one sample's boundary is
therefore as rough as the posterior is broad, and the summary is what estimates it.

**Defaults** (SituCraft's choices, from the validation below): one stage of 1 µm voxels in one
layer, 600 sweeps, the last 500 summarized; perimeters within 1.6 times the least (slice 5).
- **The schedule** was the best of those tried on simulated truth. The publication's schedule (4 µm
  and one layer, then 2 µm and two, then 1 µm and four) is available. Coarse starts reach a higher
  likelihood on the simulations, but land further from the truth there. These simulated cells do
  not overlap in depth, which real tissue can.
- **The samples:** summarizing more of them helped steadily. On the standard tissue, a summary of
  100, 300 and 500 sweeps gave median IoUs of 0.610, 0.640 and 0.663.
- Slice 6's comparison on real data revisits both.

**Validation** (`internal/segment/refine`; fixed seeds):
- **Detailed balance, exactly.** On lattices small enough to list every labelling (3 × 3 in one
  layer, 2,136 states reachable; 2 × 2 × 2, 2,314), with the parameters held fixed:
  - every move from every reachable state satisfies $\pi(\sigma)K(\sigma\to\sigma') = \pi(\sigma')K(\sigma'\to\sigma)$
    to $10^{-9}$ (13,712 and 22,592 moves);
  - no move is allowed one way only;
  - every reachable state keeps the constraints, checked independently.
- **The random steps sample it.** 20,000 chains started from exact draws of π, each run 200 steps,
  end distributed as π (χ²). The same holds for the parallel sweep on a 4 × 2 lattice in blocks of
  two voxels.
- **Along a random walk** on a 9 × 8 × 2 lattice with six cells, where a voxel can sit between two
  pieces of a cell, every proposed move balances against the target computed afresh.
- **Repositioning:** 20,000 transcripts observed at one place, near cells of different rates and a
  nuclear prior, end in each voxel as often as the displacement prior's mass, the intensity and
  the prior say (χ²).
- **Determinism:** a run gives the same digest on 1, 4 and 16 threads, on arm64 and amd64
  (pinned).
- **Power.** Each test was shown to fail on a deliberate error. Eight errors were tried:
  - connectivity tested for the losing cell only;
  - the proposal ratio left out, or the bubble left out of it;
  - a step that always copies the first neighbour;
  - the perimeter's change computed wrongly;
  - repositioning without the nuclear prior, with one width for both components, or about the
    current place instead of the observed one.

  Every one was caught; the one-sided connectivity test only by the walk on the larger lattice.

**Reference numbers.** On the standard simulated tissue (500 × 500 µm, seed 20261014: 2,925
cells started from the instrument's, 394,490 gene transcripts of quality 20 or more; ten cores),
scored on gene transcripts of quality 20 or more and on the true cells with a nucleus in the
section:

| Segmentation | Transcripts in their true cell | Median IoU | Time |
| --- | --- | --- | --- |
| The true cells, each transcript by where it was observed | 85.0% (the small tissue) | 1 | |
| The instrument's 5 µm expansion (the start) | 82.5% | 0.622 | |
| Default: 1 µm, one layer, 600 sweeps (500 summarized), perimeters within 1.6 times the least (slice 5) | **86.3%** | **0.712** | 22 s |
| The same with perimeters within twice the least, holes filled (slice 4; the rows below too) | 85.9% | 0.678 | 20 s |
| The same, holes left (slice 3) | 85.9% | 0.663 | 24 s |
| 1 µm, one layer, 1,500 sweeps (the last 1,000 summarized) | 85.9% | 0.677 | 56 s |
| 1 µm, four layers, 600 sweeps (500 summarized) | 85.1% | 0.630 | 51 s |
| The publication's schedule, 4 → 2 → 1 µm (250 summarized) | 83.0% | 0.591 | 39 s |

The refined segmentation assigns more transcripts to their true cells than the true cells
themselves do by place. Repositioning estimates where each molecule came from, which no
segmentation by place can. Its cells cover 175,252 µm² against the true 185,852 µm² (the
expansion's 245,477): the sparse edges of cells are left to the background. On the small tissue
(the unit test: 300 sweeps, 250 summarized), 86.4% of transcripts are in their true cell (the
expansion 83.4%), with a median IoU of 0.712 (0.634).

**Found by slice 3:**
- **Attributions before rates.** The first attribution, drawn before any cell had rates, gave every
  transcript to the background, and the chain never recovered. The start now attributes by place.
- **One sample is not the segmentation.** On the small tissue at 1 µm in four layers, the last of
  80 sweeps put 80.8% of transcripts in their true cells (median IoU 0.571), below the expansion.
  The posterior summary is above it.
- **Projecting 3D cells:** taking any layer's cell for a column spread cells over the gaps; the
  column's dominant label does not.

#### The outputs (verified, wave 3, slice 4)

A run is written as SituCraft's segmentation files (docs/FORMATS.md), checked, and compared with
the instrument's segmentation as soon as it is made.

- **The 2D footprint.** Each column's dominant label. *SituCraft's choice:* with every hole
  filled, so that a region of the background enclosed by one cell (not reaching the region's
  edge) is the cell's. In one layer an empty voxel inside a cell gains about 2.5 nats by going to
  the background, so about 8% of a cell's voxels are holes in the summary where no transcript
  happened to lie; cells have none. Filling them raised the standard tissue's median IoU from
  0.663 to 0.678, and the small tissue's from 0.668 to 0.678.
- **The raster and outlines.** The footprint at the image's resolution; outlines traced from it
  and simplified to half a voxel, so that the voxels' steps are not drawn. With several layers,
  each layer's outlines too (`layers.bin`).
- **Assignments and probabilities.** Each modelled transcript's most frequent cell and its share
  of the summarized sweeps; the others by the pixel they were observed in.
- **Counts:** the gene transcripts of quality 20 or more, by assigned cell.
- **Limitations, per cell** (shown in the Segmentation view and with a difference's cells in the
  comparison workspace). *SituCraft's thresholds:*
  - fewer than 10 gene transcripts: the boundary comes more from the prior than from the
    transcripts;
  - uncertain boundaries: the cell's voxels are its own in fewer than 75% of the summarized
    sweeps, on average (that average, each cell's boundary certainty, is recorded since slice 8,
    `certainty.bin`, and fills the cells in the Viewer);
  - no nucleus: the cell started without one, so no nuclear prior holds it;
  - left out at the start;
  - for every cell: no cell absent from the start can be created, and boundaries between cells
    of like expression are uncertain.
- **Convergence.** The verdict is Geweke's diagnostic of the log-likelihood over the summarized
  sweeps: the means of their first tenth and last half compared, the standard errors from batch
  means (Geweke J. Evaluating the accuracy of sampling-based approaches to the calculation of
  posterior moments. In: *Bayesian Statistics 4*, Oxford University Press 1992:169–193). Within
  ±2 the run has settled. The split-half agreement (each transcript's most frequent cell over
  each half) describes stability. It is bounded below 1 by the posterior's own uncertainty: on
  the small tissue it was 94.3% after 300 and 600 sweeps, 95.4% after 1,200 and 95.8% after 3,000,
  about a third of transcripts having probabilities below 0.9.
  - *What the diagnostic found:* the log-likelihood climbs for about a thousand sweeps on the
    small tissue, while cells leave the gaps (their volume falling from 128,000 to 124,000 µm³)
    and the expression states consolidate (10 to 7 used). It then drifts slowly. With the
    defaults, Geweke's z is about 9.5 on the small tissue and 15 on the standard one, and still
    4.6 after 1,500 sweeps on the standard tissue.
  - Against the truth the result barely changes with longer runs (the table above). So the
    defaults stand, and the page says plainly that the run is still drifting and that more
    sweeps would make sure.
  - Agreement between two seeds on a pilot region came with the segmentation studio (below).

#### The segmentation studio (verified, wave 3, slice 5)

**Regions and sections.** A refinement covers the whole tissue, its sections apart, or chosen
regions (rectangles, µm).
- **A region's cells** are those whose seed (their nucleus's centre) lies in it. They are refined
  on a lattice covering the region and a margin of 20 µm about it. The cells about it are held
  where the instrument drew them: no move takes a voxel from a held cell or gives it one. So the
  region's edge never cuts a cell, and a refined cell near it keeps its whole extent. The rest of
  the tissue keeps the instrument's segmentation, and its transcripts their pixel's cell.
- **Regions** are clipped to the tissue. Regions whose margins would overlap are merged, so that no
  cell is both refined and held.
- **Sections** are the tissue's separate pieces: cells grouped where their boxes come within a gap
  (100 µm unless set; single linkage, through a grid of the gap's size), each group's box grown by
  the margin. Each is refined apart, with a model of its own: unrelated tissues on one slide do not
  share expression states. Only one region's run is held in memory at a time.
- *Validation:* a region's run changes no pixel outside its lattice and none of a held cell; its
  outputs pass the segmentation check. Sections are found as the tissue's pieces and joined by a
  wide enough gap.

**Presets** (`refine.Presets`, version 1), each measured on the standard simulated tissue
(`TestPresetsOnSimulatedTissue`, REFINE_PRESETS=standard; measured again after slice 6's change to
the summary, and on slice 7's tissue, whose background is of two kinds). The scores are as above; "outlines" is the median ratio of each cell's outline (pixel
edges) to the least its area can have.

| Preset | What it changes from Balanced | In their true cell | Median IoU | Outlines | Memory (estimated) |
| --- | --- | --- | --- | --- | --- |
| The instrument's expansion | | 82.5% | 0.622 | 1.22 | |
| Balanced (the defaults) | | 86.3% | 0.716 | 1.32 | 89 MB |
| Conservative boundary | perimeters within 1.3 times the least; gaps proposed at 0.1; nuclear prior 0.9 | 86.2% | 0.731 | 1.21 | 89 MB |
| Flexible morphology | perimeters within 2.5 times the least; nuclear prior 0.7 | 84.7% | 0.638 | 1.62 | 89 MB |
| Low memory | 2 µm voxels; the collector's target 20% | 84.8% | 0.648 | 1.33 | 72 MB |
| ROI pilot (a 200 µm square in the middle) | a region, run with two seeds | 86.5% in the region (Balanced on the whole tissue: 87.1%) | 0.724 (0.718) | | |

- **Every preset puts more transcripts in their true cells than the instrument's segmentation.**
- **The pilot reproduces the whole run in its region:** 86.5% and 0.724 against the whole run's
  87.1% and 0.718 there. Its two seeds put 93.2% of the transcripts in refined cells in the same
  cell; the split-half agreement is about 95%, both bounded by the posterior's own uncertainty.
- **What did not work:**
  - *Low memory without repositioning:* keeping transcripts where they were observed put only
    83.0% of them in their true cells on the small tissue, below the instrument's 83.4%; with 2 µm
    voxels as well, 78.2%. Low memory keeps repositioning.
  - *Coarser voxels alone* hardly lowered the peak, since most memory goes with the transcripts.
    The collector's target does lower it (below).
- **The perimeter bound decides the shapes.** Conservative's three changes were tried apart on the
  standard tissue. The bubble (0.1: IoU 0.688) and the nuclear prior (0.9: 0.681) moved little.
  The perimeter bound moved most, and every simulated cell type gained from a tighter bound,
  including the elongated ones:

  | Perimeter bound | Median IoU | Endothelial-like (aspect up to 4) | Fibroblast-like (up to 3.2) | Epithelial-like | Immune-like |
  | --- | --- | --- | --- | --- | --- |
  | 2.5 | 0.642 | | | | |
  | 2 (slice 4) | 0.678 | 0.641 | 0.694 | 0.738 | 0.663 |
  | 1.8 | 0.697 | | | | |
  | 1.6 (the default) | 0.712 | 0.665 | 0.716 | 0.775 | 0.701 |
  | 1.4 | 0.724 | 0.673 | 0.718 | 0.780 | 0.720 |
  | 1.3 | 0.724 | | | | |
  | 1.2 | 0.716 | | | | |

  - The bound acts mostly against ragged outlines rather than long ones: cells start from the
    instrument's expansion, and a cell starting beyond the bound keeps its own ratio.
  - *SituCraft's choice:* the default is 1.6, the tightest bound that still admits an ellipse
    four times as long as wide at any angle, rather than the simulation's best (1.3–1.4).
    Simulated outlines are smoother than many real cells', and a bound below 1.6 would hold real
    elongated cells to shorter shapes than they have.
  - Conservative takes the simulation's best, 1.3, for tissues of rounded cells. Its cells cover a
    little more area than Balanced's, not less: a tight bound stops the background carving ragged
    gaps as much as it stops cells growing tendrils. So its warning is about elongated cells, not
    unassigned transcripts (0.02% unassigned, against Balanced's 0.01%).
  - Slice 6's comparison on real data revisits the default.

**Memory.** Measured with the heap in use sampled every 10 ms. The run now holds less:
- **Tallies:** each transcript's tally keeps up to four labels with their counts (24 bytes; a slot
  is free while its count is 0). The split-half agreement is estimated on every eighth transcript.
  It is a share, and on 50,000 transcripts its standard error is about 0.1%. Before, two more
  full tallies were kept for it. The live heap on the standard tissue fell from 75 to 53 MB; the
  assignments are unchanged (the pinned digest held).
- **Reading:** only the columns used are decoded from `transcripts.parquet` (gene, quality and
  place). A row group then costs about 70 bytes a row instead of 135.
- **Between phases** (after reading, after sampling) the collector runs, and the sampler's state is
  let go before the segmentation is written (`Result.Release`).
- **The collector's target:** refinements run with Go's collector keeping the heap within 50% of
  its live size (Go's default is 100%). On the standard tissue the peak fell from 190 to 141 MB,
  in 7.1 s against 7.0. Low memory sets 20%. The target never changes the result; a lower setting
  of the user's own (GOGC) is kept.
- With all of these, the standard tissue's peak fell from 172 MB (slice 4) to 89 MB.

**Preflight estimates** (`refine.EstimateCost`). The peak memory is the largest of three phases:
- reading: a block of each mask (its chunks' rows across), else a row group of the transcripts,
  with the inputs;
- sampling: the inputs, and the largest region's run (per modelled transcript, voxel, and cell ×
  gene);
- writing: a row group again, every row's assignment, and the regions' results.

The phases' terms are scaled by the collector's target. Disk is counted per row (assignment), per
modelled transcript (probability), per non-zero count, per cell (record, outline, label runs) and
per sweep (the trace). Time is counted per modelled transcript and per cell × gene × component in
each sweep.

| Run (100 sweeps; ten cores) | Memory measured (estimated) | Disk | Time |
| --- | --- | --- | --- |
| Small simulated tissue, 120 genes | 8 MB (8) | 115 kB (140) | 0.4 s (0.3) |
| 300 µm, 1,000 genes | 48 MB (47) | 648 kB (602) | 3.8 s (4.0) |
| 300 µm, a region | 28 MB (28) | 401 kB (464) | 0.7 s (0.7) |
| 500 µm, a small region | 75 MB (71) | 1,001 kB (1,051) | 0.7 s (0.9) |
| 500 µm, 120 genes | 89 MB (89) | 1,352 kB (1,631) | 4.0 s (4.1) |
| 500 µm, 1,000 genes, Low memory | 97 MB (97) | 1,713 kB (1,658) | 10.5 s (11.4) |
| The Lung 2-FOV bundle, Balanced (600 sweeps), whole | 264 MB resident (251) | 4.45 MB (4.0) | 97 s (88) |
| The large simulated tissue (§3.4), 25 patches | 5.1 GB resident (6.6) | 166 MB (224) | 626 s (about 600) |

- A region's run is dominated by reading the bundle; a small panel's by the transcripts modelled; a
  large panel's by the cells × genes. In patches, the memory is the largest patch's run with
  what is held throughout (large tissues, below); the large tissue's estimate is a fifth over
  what it took, on the safe side.
- The preflight refuses a run whose estimate passes 85% of the computer's memory (unless forced)
  or the free space where it is written, and warns past half of either.
- *Found by measuring:* the first, linear model under-predicted small regions by a factor of four.
  Reading and writing the bundle cost more than sampling a small region.

**Failures, classified** (requirements S7): the cells to start from missing (`cells.zarr.zip`), a
malformed bundle, memory, storage (also a full disk met while writing), a region holding no cell's
nucleus or no transcript, and settings a run cannot use. Each failure carries what to do, shown in
the studio, the Jobs view and the remote action's answer.

**Convergence judged only when it can be.** Geweke's diagnostic needs a first tenth of five
batches of two: 100 summarized sweeps. Below that the diagnostics say it was not judged
(`gewekeJudged`), the page says so, and the preflight warns. *Found by slice 5:* a short run's z
of 0, which means too short to judge, was shown as settled.

**Recipes and the terminal** (requirements E5). A spec is kept as a recipe (docs/FORMATS.md) and
run by the app's job or by `situcraft refine`, through one function (`refine.Refine`).
- On a simulated bundle, a recipe from the app's preflight, run in the terminal on three threads,
  gave every file the app's job made, byte for byte.
- The terminal's folder imports back into the app as a refinement of the same dataset, with the
  same files' SHA-256.
- A folder of another image's size is refused.

#### Against an independent reference (verified, wave 3, slice 6)

SituCraft's refinement was compared with Proseg 3.2.0 (Jones et al. 2025;
https://github.com/dcjones/proseg), the publication's own software, on both 2-FOV bundles and the
standard simulated tissue (`validation/reference/segmentation/`).
- **SituCraft:** Balanced, as `situcraft refine` runs it, with two seeds.
- **Proseg:** its Xenium preset, as a user runs it, in two configurations, each run twice (it has
  no seed):
  - one voxel layer (`--voxel-layers 1`), as SituCraft's default: the like-for-like reference;
  - its own defaults (four layers).
- **Read and measured as any segmentation:** each is imported (Proseg's SpatialData store) and
  compared with every other by the comparison workspace's measures, against the instrument's
  segmentation and, on the simulated tissue, the truth:
  - each segmentation's cells, assigned fraction (gene transcripts of quality 20 or more), spurious
    co-expression (relative to the nuclei) and mutually exclusive co-expression;
  - between two, the cells matched (one to one, expanded or contracted), the quantiles of their
    IoU, the correlation of their counts (Pearson's on log(1 + n)), and the transcripts whose cell
    changed;
  - against the truth, the share of the transcripts in a true cell that are in it
    (`changesQ20OfAssignedA`). It pairs cells by the comparison's matching, so it differs a little
    from the nucleus-based scoring of the tests above (85.0% against 86.3% for the same run).

**Tolerances,** set before the comparison was made (`validation/segmentation_test.go`), each with
its reason. Differences beyond one are explained below or fixed.

| Measure | Tolerance |
| --- | --- |
| Cells | SituCraft's within 5% of Proseg's (SituCraft keeps every cell of the instrument's; Proseg makes one per nucleus) |
| Assigned fraction | within 3 points (both assign by the same model) |
| Spurious co-expression | SituCraft's at most 10% above Proseg's |
| Matched cells' counts | correlated within 0.05 of the lower of each method's own two runs |
| Median IoU | within 0.10 of the lower of each method's own two runs |
| On the truth | in their true cell no more than 1 point below Proseg; median IoU no more than 0.02 below; cells' transcripts left in no cell no more than Proseg's (slice 7) |
| Budget (Breast) | under 2 minutes and 4 GB on a laptop |

**Results** (one run of each; ten cores of an Apple M4 laptop; made again in slice 7, when the
simulated tissue's background became two kinds and SituCraft's model began measuring the false
decodes):

| | Lung: SituCraft | Lung: Proseg | Breast: SituCraft | Breast: Proseg | Simulated: SituCraft | Simulated: Proseg |
| --- | --- | --- | --- | --- | --- | --- |
| Cells | 11,898 | 11,871 | 7,275 | 7,008 | 2,925 | 2,925 |
| Assigned | 99.63% | 95.68% | 97.04% | 94.39% | 99.66% (truth 97.54%) | 98.27% |
| Spurious co-expression (instrument) | 1.68 (1.87) | 1.37 | 1.83 (1.77) | 1.69 | 2.02 (2.53; truth 1.89) | 1.87 |
| Median IoU, SituCraft with Proseg (each's own runs) | 0.558 (0.688) | (0.701) | 0.676 (0.775) | (0.815) | 0.713 (0.771) | (0.784) |
| Counts correlated with the other | 0.839 | | 0.919 | | 0.961 | |
| Transcripts changing cell between own runs | 14.0% | 50.6% | 11.1% | 37.3% | 6.8% | 23.6% |
| In their true cell (instrument 82.2%) | | | | | **84.7%** | 75.2% |
| Median IoU with the truth (instrument 0.622) | | | | | 0.719 | 0.727 |
| Cells' transcripts left in no cell (instrument 1.17%) | | | | | **0.13%** | 1.42% |
| Time, peak memory | 93 s, 263 MB | 156 s, 610 MB | 83 s, 279 MB | 122 s, 663 MB | 23 s, 106 MB | 22 s, 305 MB |

- **Met everywhere** on Breast and the simulated tissue, the budget included: Breast in 83 s and
  279 MB.
- **On the truth** SituCraft puts 9.5 points more transcripts in their true cells than Proseg, with
  outlines as close to the true ones (median IoU 0.719 against 0.727), and leaves a tenth as many
  of the cells' transcripts in no cell. On slice 6's tissue it put 85.0% in their true cell and
  Proseg 75.6%: slice 7's tissue differs, and on it slice 6's model scores as slice 7's.
- **Stability:** Proseg's per-transcript assignments differ between two of its runs three to four
  times as much as SituCraft's between two seeds, while their outlines differ about as much.
- **On Lung, three are explained:**
  - *Assigned fraction (99.63% against 95.68%):* a larger background share is not a more accurate
    one. On the simulated tissue only 18% of what Proseg leaves in no cell is background, and the
    cells' transcripts it leaves out are ten times SituCraft's (the background, below).
  - *Spurious co-expression (1.68 against 1.37):* the measure does not rank methods by accuracy.
    On the simulated tissue Proseg's is below the truth's own (1.87 against 1.89), while it puts
    9.5 points fewer transcripts in their true cells than SituCraft (2.02). On Lung SituCraft's is
    10% below the instrument's; part of the rest is ambient RNA counted in cells.
  - *Median IoU (0.558, the tolerance 0.588):* Lung's cells are the smallest (median 37 µm²), and
    Proseg's outlines are more compact there (1.16 times the least perimeter against SituCraft's
    1.32). With Conservative's bound (1.3) SituCraft's agreement rises to 0.576; started from the
    nuclei, as Proseg starts, it stays at 0.557.
- **The background** was found under-estimated here (a quarter of the simulated tissue's) and
  taken up by slice 7 (the background, below).

**Found by slice 6:**
- **The summary counted the background in cells.** A transcript in a cell's voxel was reported in
  the cell even in the sweeps where the sampler attributed it to the background. Each summarized
  sweep now draws every transcript's attribution, from a random stream of its own, so the chain is
  unchanged; the reported cell is where it most often lay and was attributed. Breast's assigned
  fraction fell from 98.80% to 97.03% and its co-expression from 1.94 to 1.83. Elsewhere the
  posterior's mode moved little: a background probability of a few percent rarely wins the mode.
  One posterior sample assigns about 1% fewer (Lung: 98.96% against 99.63%).
- **Proseg's four-layer default lost Lung's transcripts:** 61% of them went to the background, and
  5,035 of its 11,872 cells were left empty. With one layer, 4% went to the background. Lung's
  transcripts span more depth than Breast's (§1.3b). The like-for-like reference is therefore one
  layer, as SituCraft's default; SituCraft's default stays one layer.
- **The simulated bundles lacked two of Xenium's transcript columns,** `nucleus_distance` and
  `codeword_index`, and stored `overlaps_nucleus` as 32 bits rather than 8. Proseg could not read
  them. The simulator now writes Xenium's columns and types (§3.4).
- **A tool may keep where it moved each transcript beside where it was observed.** Proseg's points
  hold both; the SpatialData import matches transcripts at their observed places
  (`observed_x`, `observed_y`).
- **The perimeter bound, revisited:** tighter bounds agree a little more with Proseg on Lung (0.557
  at 1.6, 0.576 at 1.3, 0.499 at 2.5), as with the simulated truth (slice 5). The default stays at
  1.6, the tightest that still lets a round start grow into a cell four times as long as wide.

The maintainer makes Proseg's segmentations (`validation/reference/segmentation/run_proseg.sh`) and
reduces them to the committed numbers. The weekly real-data workflow refines each data set again
and checks that SituCraft's segmentation is still, byte for byte, the one the numbers were made
from.

#### The background (verified, wave 3, slice 7)

Slice 6 found SituCraft's background rate accounting for a quarter of the simulated tissue's
background, whatever the chain's start. Slice 7 diagnosed it on simulated truth, made the
background two-part (the model, above), and measured what any per-transcript call can find.

**The diagnosis** (the standard tissue; temporary test hooks, not kept):
- **Cells take the background's molecules where they lie.** The refined cells cover as much of the
  field as the true ones (73% and 74%), but their gaps fall where no transcripts lie. With every
  gene's background rate held at the truth, cells still took 85% of the background molecules in
  the true gaps, and the summary left 0.65% of the transcripts in no cell (the truth: 2.46%).
  Rates alone cannot recover the background transcript by transcript.
- **Inside cells, ambient RNA looks like expression.** On a tissue of ambient RNA alone, one rate
  per gene gave the rarest fifth of the genes three times their background and the commonest fifth
  a seventh of theirs. A cell's rates absorb the ambient RNA of the genes it expresses; only the
  rare transcripts of genes a cell barely expresses stand out. Tying the ambient RNA to the
  tissue's mean expression, one level for every gene, let the level collapse to almost nothing
  (0.01% of the transcripts): the posterior prefers cells explaining a floor of every gene.
- **The outlines' settings change little:** a perimeter bound of 1.3 or 1.2, or gap proposals
  four times as often (0.2), left 0.35–0.37% of the transcripts in no cell, against 0.34%.

**The change:** the negative controls measure the false decodes, which no cell can take (the
model, above). The ambient RNA keeps one rate per gene.

**The benchmark** (`TestBackgroundOnSimulatedTruth`, run by hand: `REFINE_BACKGROUND=all`): the
standard tissue at three levels of both kinds, and each kind alone, refined with Balanced.
Precision is the share of the transcripts left in no cell that truly are background, recall the
share of the background left in no cell.

| Tissue: ambient RNA, false decodes (per 100 µm²) | Background (truth) | Rates: false decodes (expected) | Rates: ambient RNA (expected) | In no cell | Precision | Cells' transcripts in no cell | Recall | In their true cell | Median IoU |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Low: 0.6, 0.005 | 0.51% | 0.21% (0.25%) | 0.47% (0.25%) | 0.06% | 35.9% | 0.04% | 4.3% | 86.4% | 0.717 |
| Standard: 3, 0.025 | 2.46% | 1.10% (1.24%) | 0.61% (1.24%) | 0.34% | 60.2% | 0.13% | 8.2% | 86.3% | 0.716 |
| High: 12, 0.1 | 9.13% | 4.32% (4.61%) | 1.21% (4.61%) | 2.10% | 71.5% | 0.60% | 16.4% | 85.8% | 0.715 |
| Ambient RNA only: 6, 0 | 2.46% | 0.01% (0) | 0.71% (2.47%) | 0.06% | 55.5% | 0.02% | 1.2% | 86.3% | 0.713 |
| False decodes only: 0, 0.05 | 2.44% | 2.21% (2.47%) | 0.55% (0) | 0.74% | 62.1% | 0.28% | 18.9% | 86.2% | 0.719 |
| Standard, slice 6's background (one rate per gene, no controls) | 2.46% | | 0.78% in all | 0.14% | 70.5% | 0.04% | 4.1% | 86.3% | 0.716 |
| Standard, every gene's rate held at the truth | 2.46% | | | 0.65% | 56.1% | 0.29% | 14.8% | 86.2% | 0.718 |
| Ambient RNA only, the rates held at the truth | 2.46% | | | 0.79% | 54.1% | 0.36% | 17.4% | 86.1% | 0.715 |

(The rates are their mean over the summarized sweeps times the region's volume, as shares of the
modelled transcripts; the expected shares are the simulator's, of quality 20 or more. The last
three rows are diagnostics, not settings.)

- **The false decodes are measured** within a sixth of the truth at every level, a little low: the
  controls are unbiased, but the genes' false decodes that cells take pull the estimate down.
- **The background found doubles** on the standard tissue against slice 6's model (8.2% of it
  against 4.1%, its rates 1.71% of the transcripts against 0.78%), with the same accuracy.
- **The ambient RNA is estimated less surely:** from a quarter of the truth (high, ambient only)
  to about twice it (low, where the rare transcripts of genes a cell barely expresses are taken
  for background).
- **What is left in no cell is mostly background** (55–72% from the standard level up), and the
  cells' transcripts left there are a small share of all (0.03–0.6%). The rest of the background
  is counted in cells, as every per-transcript call must count most of it.
- **Accuracy is unchanged:** 85.8–86.4% of transcripts in their true cell, median IoU
  0.713–0.719.
- **Where the limit lies:** even with the rates at the truth, about a sixth of the background
  could be told from the cells' transcripts (recall 15–17%). The rest lies in or beside cells that
  express the same genes, and only a better prior on cells' shapes could leave more of it out.

**Against Proseg** (§3.2, against an independent reference; `lostQ20` in the comparison's numbers):
on the simulated tissue Proseg leaves more in no cell (1.59–1.73% of the transcripts in its two
runs), but only 18–19% of it is background, so 1.29–1.42% of all transcripts are cells' it leaves
out: ten times SituCraft's 0.12–0.13% (0.32–0.34% in no cell, 61–63% of it background). Proseg
recovers more of the background (12% against 8%; 21% with its four-layer defaults, which leave out
2.0% of the transcripts wrongly). A background share alone does not rank methods: what is left out
wrongly matters as much as what is left in. The slice's first aim, a share at least Proseg's 1.53%,
would therefore have been the wrong goal, and is not taken; the reference comparison checks instead
that SituCraft leaves out no more of the cells' transcripts than Proseg.

On the real bundles the controls put false decodes at 0.04% (Lung) and 0.06% (Breast) of the
transcripts, so SituCraft's results there hardly changed (Lung's assigned fraction 99.63%, as in
slice 6; Breast's 97.04%). Their background beyond that is ambient RNA, the part no method here
can tell from the cells' own expression transcript by transcript.

The refinement's diagnostics report the background (`diagnostics.json`; the studio's Background):
the shares its rates account for, by kind, the negative controls that measured the false decodes,
and the share left in no cell.

#### Large tissues (verified, wave 3, slice 9)

A run's memory grows with what it holds of its region: about 130 bytes a modelled transcript, 50
a voxel and 17 a cell × gene (the cost model above). A tissue of tens of millions of transcripts
cannot be held as one run on a laptop, so a large region is refined in patches, an approach
after Blampey et al. (2024), *Nat Commun* 15:4981, doi:10.1038/s41467-024-48981-z.

**Patches.**
- **When:** a region of more than twice `patch_transcripts` modelled transcripts (2 million by
  default, so above 4 million) is cut into equal rectangles: $k = \lceil n / \text{target} \rceil$
  of them, $n_x = \operatorname{round}(\sqrt{k\,w/h})$ across and $\lceil k / n_x \rceil$ down, the
  last ending exactly at the region's edges. They depend on the data and the settings only, never
  on the threads or the memory, so a refinement is still the same bytes for the same seed.
- **Each patch is a run** of the region's kind: it refines the cells whose seeds lie within 40 µm
  of its rectangle (clipped to the region) and holds the cells about them as the instrument drew
  them. A cell near a seam is refined with its neighbours on both sides, in every patch whose
  margin reaches it.
- **One at a time:** each patch's sampler is released when its run ends, so the sampling holds
  one patch's state.
- **Stitching:** each pixel and transcript is taken from the patch whose rectangle holds it, each
  cell's boundary certainty and flags from the patch whose rectangle holds its seed, and each cell
  is counted once. The diagnostics keep each patch's summary and trace, with its rectangle.

**Validation.**
- **Stitched as specified** (`TestPatchesAreStitched`, a 200 × 160 µm tissue in four patches):
  the segmentation passes the check; every pixel is a cell its rectangle's patch refines where
  its footprint has one; every cell is decided once and none is held. The patches and a run of
  the whole agree on 89.7% of transcripts, two seeds of the whole on 90.7%: the patches differ
  from the whole as chance does.
- **As accurate as the whole** (`REFINE_PATCHES=standard`, the standard tissue in four patches,
  Balanced): 86.1% of transcripts in their true cell and a median IoU of 0.719, against the
  whole's 86.3% and 0.716; within 20 µm of a seam, 87.4%. The seams lose nothing.

**At scale** (`validation/scale_test.go`, weekly; measured here on a 10-core laptop with 32 GB):
the `large` simulated tissue (§3.4), 6.1 × 5.2 mm, 399,084 cells and 54,782,352
transcripts, made, refined, opened and drawn, each step in a process of its own, then deleted.

| Step | Time | Peak memory |
|---|---|---|
| Making the tissue, a field at a time | 621 s | 3.1 GB |
| Refining it in 25 patches (40 sweeps) | 338 s | 6.2 GB |
| Its derived data, the instrument's segmentation registered, the refinement imported | 128 s | 1.8 GB |
| The Viewer with the dataset alone: its descriptor, cells and a region's transcripts | 20 s | 0.19 GB |
| The Viewer drawing the refinement: its cells and all 2,688 transcript tiles | 38 s | 0.93 GB |

- **The preflight** foresaw 25 patches of 1,219 µm, 49.4 million transcripts modelled (58.3
  million with those the patches share) and 6.6 GB; without patches the run would need 10.8 GB.
- **The refinement** (40 sweeps, the last 20 summarized: the test checks the scale, not the
  chain) refined all 383,159 of the instrument's cells in 25 patches. With 100 sweeps, 50 summarized, it took
  626 s at 5.1 GB, Geweke's z 0.0, and 93.5% of transcripts were in the same cell over both
  halves of the summarized sweeps; Balanced's 600 sweeps are estimated at 53 minutes. Its peak
  moves by a sixth from run to run with the collector's timing (5.1 to 6.2 GB over three runs),
  always under the estimate.
- **The Viewer** answers each transcript tile of the refinement, with every transcript's cell
  and probability, in 12 ms at worst (2,688 tiles).

**What still grows with the tissue.** Patches bound the sampling; the run still holds every
modelled transcript's place and prior, each patch's results until the segmentation is written,
and every row's assignment and probability as they are written: about 100 bytes a transcript and
2 a pixel of the image. A computer with 16 GB thus refines about 120 million transcripts at this
density, and the preflight says so before a run starts. Whole slides beyond that need the inputs
and results streamed by patch (wave 10).

**The Viewer's memory.** The dataset's cells table counts each cell's genes detected a row of
tiles at a time: a first pass finds the last row each cell has a transcript in, and the second
makes each row's (cell, gene) pairs unique with those carried from earlier rows, counting a cell
at its last row (checked cell by cell against transcripts.parquet on both bundles). It held a pair
for every transcript in a cell before: with the dataset alone at 55 million transcripts, the
Viewer now peaks at 0.19 GB, against 1.65 GB. A segmentation's per-transcript files (its assignments and
probabilities, 8 bytes a transcript) are never read whole: they are streamed once, to give each
cell its transcripts' mean probability and to write the transcripts' cells and probabilities by
the dataset's tiles into the cache, in batches of about 4 million transcripts, each batch sorted by
row and picked out in one pass over the files. Each tile's are then read when it is drawn. At 55
million transcripts the Viewer drawing a refinement peaked at 0.93 GB, against 1.8 GB reading
the files whole.

SituCraft's other choices where the publication leaves them open:
- **Polygons** come from the summarized voxel labels by boundary tracing per z layer, and from
  the 2D footprint.
- **Convergence.** Each sweep records the voxels moved, the transcripts repositioned and the
  log-likelihood (`diagnostics.json`).
- **Large tissues** run as overlapping patches that are stitched (above).

Limitations to show in the interface: refinement cannot create cells absent from the prior
segmentation; boundaries of cells with few transcripts, or in regions of homogeneous expression,
are uncertain.

### 3.3 Importing other segmentations (verified, wave 2, slice 4)

| Source | Read |
| --- | --- |
| SpatialData Zarr (Marconato et al. 2025), format 2 or 3, a folder or a zip | the cells from a shapes element (GeoParquet: WKB polygons, ids in the index) or a labels element (a multiscale label image, its finest level); each transcript's cell from a points element (Parquet parts); the counts from an AnnData table (anndata's on-disk format: X as sparse rows, sparse columns or dense; obs and var data frames) annotating the cells |
| Baysor (Petukhov et al. 2022): `segmentation.csv` with `segmentation_polygons.json` | each transcript's cell (`cell`; 0 and `is_noise` none), by `transcript_id` when the input kept it, else by position and gene; polygons as a GeoJSON GeometryCollection whose geometries carry their `cell` |
| GeoJSON polygons (RFC 7946), with or without a transcript table (CSV, gzipped CSV, TSV or Parquet) | polygons and MultiPolygons, each feature naming its cell; each transcript's cell from the table, else from the polygon it lies in |
| Xenium Ranger outputs (`import-segmentation`, `resegment`), and Xenium Onboard Analysis bundles of the same run | the cell mask (`cells.zarr.zip` masks/1), cells, boundaries, and `transcripts.parquet`'s `cell_id` by `transcript_id` |

**What every import becomes.** The same files as SituCraft's own segmentations (§3.1): a label
raster at the morphology image's resolution (a label image resampled, nearest pixel; else the
polygons drawn, a pixel belonging to a polygon whose interior holds its centre), each of the
dataset's transcripts' cell, counts of gene transcripts of quality 20 or more, and outlines (the
tool's polygons, else traced). Counting the same way for every segmentation keeps comparisons
fair; a count table the tool wrote is compared with the counts, cell by cell.

**What is checked against the dataset.**

- **Units are declared, never guessed.** The coordinates are read as µm, or as pixels of the
  morphology image when the import says so. spatialdata-io's Xenium reader writes its global
  coordinate system in the morphology image's pixels (its points and shapes carry a scale of
  1/0.2125 to it, its labels the identity); other tools write µm. When the cells do not lie
  within the dataset's image, the import is invalid, and the finding says how many would were the
  coordinates read in the other unit.
- **The transcripts it names are the dataset's.** By `transcript_id`, and where the import gives
  positions too (SpatialData points, Xenium bundles) within 0.05 µm of the dataset's transcript of
  that id: the two development bundles share transcript ids (each run numbers its own), so ids
  alone would let another run's transcripts pass. Without ids, by position and gene within
  0.01 µm.
- **An output bundle is of the same run:** by its experiment UUID; the development bundles both
  name their run "run" and their region "region" (anonymized for sharing).
- **Ids:** cells named twice, cells the table names without a polygon, polygons whose cells no
  transcript names; labels the table has no row for.

**Measured on the Lung bundle** (`internal/segment/imports`, reference numbers of its tests):

- The Lung bundle imported into itself as an output bundle gives the instrument's segmentation
  exactly: 11,898 cells, the raster identical to `masks/1`, every transcript's cell its `cell_id`,
  and `cells.parquet`'s counts equal to the assignment's for every cell.
- Written by spatialdata-io 0.7.1 (spatialdata 0.8.0, Zarr format 3, AnnData's arrays sharded),
  read in pixels: the cells from `labels/cell_labels`, each label named through the table's
  `cell_labels` column, the outlines from `shapes/cell_boundaries`. The raster is identical to
  `masks/1`, every assignment the instrument's, the table's counts equal for every cell. Read as
  µm, 3 of the 11,898 cells lie within the image, and the import says that 11,898 would in
  pixels.
- The instrument's own polygons and assignments, written as GeoJSON with a table (by
  `transcript_id`), or as a Baysor-style table without ids (by position and gene): every
  assignment the instrument's. The polygons drawn agree with `masks/1` on 94.33% of pixels: the
  Xenium polygons pass through their edge pixels' own coordinates (§3.1), half a pixel inside the
  pixels' areas, so a pixel whose centre lies on the cell's edge is drawn in or out by a hair.
- The Breast bundle is refused as an import into the Lung run (another experiment).

**Fixtures** (`internal/segment/imports/testdata`), one hand-made scene written by the libraries
that write each format: spatialdata 0.8.0 (shapes, points, table; a label image with its table
and shapes in pixels), spatialdata 0.2.6 (Zarr format 2), geopandas and pyarrow (GeoJSON, Parquet
and CSV tables), pandas (Baysor's tables), pyarrow and zarr-python (a Xenium Ranger-style
bundle). The scene has a cell with a hole, one of two polygons, one reaching beyond the image, one
without a polygon, transcripts the tool assigns outside every polygon, noise, and a transcript the
dataset does not have.

### 3.4 Simulated tissues (verified, wave 3, slice 1)

A segmentation can be scored only against a truth, and no measured tissue has one. `situcraft
simulate` (`internal/simulate`) makes tissues whose every cell, nucleus and molecule is known and
writes each as a Xenium output bundle with its truth. The simulator shares no code with
SituCraft's segmentation; only the file writers and the outline tracer are common. Its generative
model differs from the one the transcript-informed segmentation fits (§3.2): fixed expression
profiles by cell type rather than a gamma mixture, a different wide displacement (3 µm, not 4), and
cell shapes that are not grown by the sampler's moves. A method scored only on data drawn from its
own model is flattered (the "inverse crime": Wirgin A. The inverse crime. arXiv:math-ph/0401050,
2004).

**The tissue.**
- **Domains.** The Voronoi regions of seeded centres. Each has a mix of the cell types, one
  dominating it (a share of 0.6, the rest by gamma weights), and a direction its elongated cells
  tend to follow.
- **Cells** are placed by random sequential adsorption (Feder J. Random sequential adsorption.
  *J Theor Biol* 1980;87:237–254. doi:10.1016/0022-5193(80)90358-6):
  - a candidate's domain chooses its type, and the type its area (log-normal about the type's
    median), elongation (uniform from 1 to the type's) and outline;
  - a candidate closer to a placed cell than three quarters of their summed radii is refused;
  - placement stops when the cells' nominal areas cover the fill asked for, or after 2,000
    refusals in a row.
- **Shapes.** A cell is an ellipse of its area and elongation. Its radius varies with direction
  by second and third harmonics (σ 0.06 and 0.05). A pixel belongs to the cell whose scaled
  distance to it is least, below 1. Neighbours meet where their outlines compete, and gaps remain
  where none reaches. Each cell keeps its largest 4-connected piece; one under 8 µm² is dropped.
- **Nuclei.** A smaller, rounder ellipse (the square root of the cell's elongation) near the
  cell's centre, of the type's share of its radius. A share of cells (4%) have their nucleus
  outside the section; so do nuclei of under 4 µm² within the cell.
- **Depth.** Each cell spans its type's height, centred anywhere in the section and cut by its
  faces; its nucleus spans the middle 60%.

**The molecules.**
- **Expression.** Each gene has a base level (gamma, shape 0.6). A type's profile is:
  - the base levels;
  - its markers' (base + 0.5) times its fold;
  - the other types' markers at a fifth;
  - all normalized to sum to one.

  A cell's expected total is its type's density × its area × a size factor (log-normal, σ 0.25).
  Each gene's count is negative binomial: mean = that total × the gene's share of the profile,
  dispersion = the type's.
- **Places.** A molecule lies at a uniform point of a pixel of its cell. A nucleus pixel weighs
  the nuclear enrichment (1.5); any other pixel weighs one. It lies at a uniform depth of the cell
  (of the nucleus, for a nuclear molecule).
- **Displacement.** A molecule is observed displaced by a normal in x and y: σ 3 µm for a fifth
  of molecules, 0.5 µm for the rest. In z it is displaced with σ 0.5 µm and held within the
  section. One displaced out of the field is lost.
- **Background,** of two kinds (wave 3, slice 7), in 50 µm tiles:
  - ambient RNA on the genes: Poisson, 3 per 100 µm² in all, spread over the genes in proportion
    to the tissue's mean expression;
  - false decodes on every codeword alike, genes and negative controls: Poisson, 0.025 per
    100 µm² each (3 per 100 µm² over the 120 genes);
  - background molecules lie uniformly in the tile and the section's depth.

  Before slice 7 the controls' molecules had no counterpart on the genes, and the genes'
  background was all in proportion to expression (6 per 100 µm²).

  *Against the real bundles:* the negative-control codewords' rate is 0.04% of a gene's mean on
  Lung and 0.06% on Breast (262 and 540 controls of quality 20 or more on 248 and 249 codewords),
  so false decodes are under 0.1% of their gene transcripts. On the standard tissue they are
  1.2%, twenty to thirty times as many, so that both kinds of background are exercised; its
  benchmark (§3.2, the background) also has tissues of ambient RNA alone.
- **Quality.** A cell's molecule is below 20 with probability 0.08 (uniform in 4–20); otherwise
  its quality is 40 less an exponential of mean 3, no lower than 20.1. Background and control
  molecules are below 20 with probability 0.35.

**The instrument and the image.**
- **The simulated instrument** sees the nuclei in the section and grows each by 5 µm, every pixel
  going to the nearest nucleus. Its distances are the 5-7-11 chamfer transform (Borgefors G.
  Distance transformations in digital images. *Comput Vis Graph Image Process* 1986;34:344–371),
  within 2% of Euclidean. This is an instrument's own approximation, deliberately not SituCraft's
  exact expansion (§3.1). A transcript belongs to the cell holding the pixel it is observed in.
- **The DAPI image:**
  - each visible nucleus has a brightness (log-normal about 1,800, σ 0.25), falling to about two
    thirds at its edge, with grain (each pixel times 1 + a normal of σ 0.15);
  - the stain is blurred by two passes of a 3 × 3 mean, laid on a background of 80, and given
    camera noise (a normal of σ 6);
  - it is written as a tiled, deflate-compressed OME-TIFF with its reduced levels (lossless,
    readable by every TIFF reader; Xenium's own images are JPEG 2000).

**The bundle.** It holds every file the Xenium Onboard Analysis 2 layout requires (§1.1):
- `experiment.xenium`: one experiment UUID for the run, derived from the parameters.
  `analysis_sw_version` names the layout first (`xenium-2.0.0 layout, situcraft-simulate …`),
  because readers choose how to read a bundle by it. spatialdata-io reads a bundle whose version it
  cannot parse as XOA 1: it looks for the image elsewhere and reads cell ids another way.
- `transcripts.parquet` in Xenium's columns and types (§1.2): ids, instrument cells, nucleus
  overlap (`uint8`), fields of view, quality, each molecule's distance to the nearest nucleus (µm,
  from its pixel; 0 over one) and its codeword (the panel's). The last two came in slice 6, when
  Proseg would not read a bundle without them.
- `cells.parquet` and both boundary tables.
- The counts in both forms:
  - `cell_feature_matrix.h5`: the 10x Genomics feature-barcode matrix in HDF5, which
    spatialdata-io and scanpy read. It is written by SituCraft's own HDF5 writer
    (`internal/hdf5`, from the HDF5 File Format Specification 3.0: superblock version 2, compact
    groups, contiguous datasets, Jenkins' lookup3 checksums; h5py reads it).
  - `cell_feature_matrix/`: Matrix Market.
- `cells.zarr.zip` as XOA 2 lays it out: both masks, the 4 × 4 transform, the cell ids,
  `cell_summary`, and both polygon sets with each polygon's cell (`cell_index`) and method.
- The morphology image.
- `metrics_summary.csv`, computed as SituCraft's validation recomputes it.
- `gene_panel.json`.

`situcraft-simulation.json` at the root says that the bundle is simulated and gives every
parameter. SituCraft labels such a bundle simulated wherever it is shown, and its methods
paragraph says that the data were simulated. `truth/` is a Xenium Ranger-shaped bundle of the same
run, which SituCraft's Xenium import reads as a segmentation:
- every true cell, with its type and whether its nucleus is in the section;
- the true outlines, nuclei and masks;
- every molecule's true cell, true place, origin (cell, background, control probe or codeword)
  and kind of displacement.

**Random numbers.** Each part has its own stream:
- the layout and the panel each have one;
- each cell has one, as does each background tile and each image row.

So molecules are drawn in parallel and merged in order. Every product is rounded on its own, and
logarithms and exponentials come from `internal/dmath`. The same parameters give the same bundle
byte for byte on any number of threads, on arm64 and amd64 alike (the small preset's digest is
pinned in the tests).

**Mosaics** (wave 3, slice 9). A field holds its image whole, about 75 bytes a pixel, so one
field stops at 50 million pixels (about 1.5 mm a side). A larger tissue is a mosaic of fields
(`tiles_across` × `tiles_down`), each a whole number of the stain's 512-pixel tiles each way:
- **each tile is a tissue of its own:** its layout, cells, molecules and stain come from streams
  of its own, the seed mixed with SplitMix64's finalizer of the tile's number (Steele GL, Lea D,
  Flood CH. Fast splittable pseudorandom number generators. *OOPSLA* 2014:453–472.
  doi:10.1145/2660193.2660195); the panel is shared, and the first tile is the single field the
  same parameters make, byte for byte;
- **made one at a time and joined by streaming** into one bundle: the tables by row groups, the
  masks a chunk per tile, the stain tile by tile at every level whose tiles hold whole tiles of
  the field (the coarser levels assembled whole, being small), ids, fields of view and places
  numbered across the mosaic. Memory is a tile's whatever the mosaic's size;
- **its seams show:** no cell crosses one, a molecule displaced across one is lost as at the
  field's edge, and each tile has its own domains. A mosaic is for measuring SituCraft at scale,
  not a model of a larger tissue.

The `large` preset is 7 × 6 standard fields of 4,096 pixels (870.4 µm), 12 domains each: 6.1 ×
5.2 mm, 399,084 cells and 54,782,352 transcripts, made here in about 620 s with a peak of 3.1 GB and 4.0
GB of disk. A mosaic of 2 × 2 small tiles validates as one bundle, with every metric the validator
recomputes agreeing; its first tile is the single field, its transcript ids unique, its truth
matching it row for row, and its masks whole (`TestAMosaicIsOneBundle`).

**Validation** (`internal/simulate/gof_test.go`; fixed seeds, each test at the 0.001 level):
- **Counts.** The randomized probability integral transform of every interior cell's count of
  every gene (87,480 counts) is uniform (Kolmogorov–Smirnov). A studentized test of the
  variance against the negative binomial's catches a dispersion 30% off.
- **Places.** Every molecule's true place is in its cell, at a depth within its cell's (or
  nucleus's). The share in nuclei matches the enrichment (z test).
- **Displacements.** The wide and narrow displacements, divided by their σ, are standard normal
  (Kolmogorov–Smirnov; catches a σ 5% off), as are the displacements in depth. The wide share is
  binomial.
- **Quality.** Both quality distributions and both low-quality shares (one catches an exponential
  mean 3% off).
- **Background.** Per-tile totals are Poisson (χ²; catches a density 3% off); the genes' shares are
  their ambient RNA's (in proportion to the tissue's mean expression) plus the false decodes', the
  controls' total is Poisson at the false-decode rate, and places are uniform.
- **Cells.** On a sparse tissue (1,012 cells), the standardized log areas are normal (catches a
  median 5% off); the types are in their domains' proportions (χ²); and isolated cells' pixels
  cover their nominal areas (median ratio within 0.97–1.04).
- **End to end.** The small preset's bundle:
  - passes SituCraft's validation with no warning;
  - has every recomputed metric (16 of 16) agreeing with what it records;
  - imports its truth through the Xenium import with no finding;
  - is segmented and compared like a measured bundle.
- **Other readers** (`validation/reference/read_simulation.py`; spatialdata-io 0.7.1, spatialdata
  0.8.0, tifffile 2026.9.20, pyarrow 26.0.0). Both presets are read with no warning about the
  bundle:
  - spatialdata-io's Xenium reader gives the instrument's cells in `cells.parquet`'s order, each
    labelled as its row + 1, and their gene counts;
  - it also gives every transcript, both boundary sets, the labels and the image, each of the
    simulation's size;
  - tifffile reads the OME-TIFF's pixel size, channel and levels;
  - pyarrow reads the truth's molecules by origin and its cells by type.

**Reference numbers** (the standard preset, 500 × 500 µm, seed 20261014; 3.3 s and 29 MB):
- **The tissue:** 3,030 cells (611 epithelial-like, 660 fibroblast-like, 1,113 immune-like, 646
  endothelial-like), 2,925 nuclei in the section, 440,796 transcripts.
- **Against the truth,** for gene transcripts of quality 20 or more:

  | Segmentation | Median IoU | Assigned to a cell | Assigned to another cell than the true one |
  |---|---|---|---|
  | The nuclei alone | 0.27 | 32% | 68% |
  | Nuclei grown 2 µm | 0.71 | 78% | 31% |
  | The instrument's 5 µm expansion | 0.62 | 98.8% | 20% |

  Most of the instrument's cells are larger than the true cells (1,915 of 2,925), as nucleus
  expansion's are where it fills the gaps between cells. These are the baselines the
  transcript-informed segmentation must beat (wave 3, slice 3).

### 3.4a Simulated post-Xenium images (verified, wave 5, slice 2)

Registration, like segmentation, is scored only against a truth. `situcraft simulate --post-xenium`
makes an H&E and an immunofluorescence image of the simulated section (`internal/simulate/
postxenium.go`), each placed by a known transform, written with the bundle (FORMATS.md).

**Where each lies.** A pixel q of an image (full resolution, (0, 0) the corner of its first) is at
x = A·q on the section, then displaced by a smooth deformation d(x): along each axis, three sine
waves of random direction, a length about the wavelength asked for (0.7 to 1.3 times it), and
amplitudes summing to at most the deformation asked for. A is the image's pixel size and aspect,
a shear, a reflection and a turn, its translation such that the image covers the section and a
margin of glass. The defaults: an H&E of 0.3638 µm pixels (a 40× scanner's), aspect 1.004, shear
0.003, mirrored, turned 2.5°, deformed by up to 2 µm over 300 µm; an immunofluorescence image of
0.65 µm pixels turned −177°, deformed by up to 1.5 µm over 400 µm. The deformation is of another
family than the B-splines registration may fit (wave 5, slice 3): a method is not scored on its own
model (the "inverse crime", §3.4).

**What each shows,** each pixel the mean of 2 × 2 points of the section:
- **The H&E,** by Beer–Lambert's law: each point's optical densities of haematoxylin and eosin
  (haematoxylin log-normal about 0.85 in a nucleus, 0.05 in cytoplasm, 0.02 between cells; eosin
  by the cell's type, 0.18 to 0.40, varied by cell, 0.09 between cells, 0.12 in nuclei), with
  grain, blurred by a 3 × 3 mean; the light through them over the glass's (241, 238, 243), each
  channel exp(−ln 10 · (h·H + e·E)), with the camera's noise (σ 1.2). The stains' colours are
  Ruifrok and Johnston's (2001), haematoxylin (0.65, 0.70, 0.29) and eosin (0.07, 0.99, 0.11),
  each component varied by 4% per section and recorded in the truth, so that colour deconvolution
  (slice 3) is not handed its vectors.
- **The immunofluorescence image,** two 16-bit channels: DAPI, each nucleus of its own brightness
  (log-normal about 2,400); and a membrane stain, bright (log-normal about 1,600 per cell) where a
  point's neighbours 0.6 µm away are another cell's or none's, faint (120) inside cells; blurred,
  on a background of 110, with noise (σ 9).

Every product is rounded on its own and every sine is `internal/dmath`'s (Taylor's series on a
reduced turn, within a few units in the last place of Go's): the images are the same on every
processor and however many cores render them.

**Checked against their truth** (the small preset, 160 × 120 µm, 208 nuclei in the section), by
finding the nuclei in each image (haematoxylin's optical density in red above 0.2; DAPI above
1,000; 4-connected pieces of 6 µm² or more) and mapping each through the truth onto the true
nuclei:

| Image | Size | Nuclei found | From a true nucleus (median) | Twelve landmarks, affine: residuals / left out / true error over the section |
|---|---|---|---|---|
| H&E, 0.3638 µm | 699 × 593 | 206 | 0.032 µm | 0.16 / 0.21 / 0.31 µm |
| Immunofluorescence, 0.65 µm | 360 × 302 | 202 | 0.067 µm | 0.16 / 0.24 / 0.17 µm |

A half-pixel error in the truth's convention would put the nuclei 0.26 µm (H&E) and 0.46 µm off.
On the standard preset the images are 1,675 × 1,672 (6.4 MB) and 913 × 914 (2.8 MB), and the
simulation takes 4.5 s with them.

## 4. Comparing segmentations (verified, wave 2, slice 5)

Every segmentation is compared as SituCraft's own files (§3.1, §3.3): the instrument's is written
from its bundle (the cell mask, and each transcript's `cell_id` row by row), identical to the
bundle's; the two rasters share the morphology image's pixels.

**Correspondence** (SituCraft's documented strategy, `internal/compare`). Two cells, one of each,
correspond when they share a nucleus of the dataset (a nucleus whose pixels lie mostly in each),
or when at least half of one lies within the other; cells without area (an import without
outlines) correspond the same way by their transcripts. A fragment split off a cell lies within
it, a neighbour whose boundary moved does not: with a lower share (a fifth of either, first
tried), two neighbours whose shared boundary moved one column became one "rearranged" difference.
Each connected set of corresponding cells is one difference: one cell in each is one-to-one, or
expanded (B's area over 1.25 times A's) or contracted (under 1/1.25); assignment-only when one has
no area; one of A to several of B a split, several to one merged, several to several
rearranged; a cell with none only in A or only in B. Hand-drawn fixtures check each class, and
the numbers below by hand (`compare_test.go`).

**Measures.** For each segmentation: cells, cells with a nucleus and with several, the assigned
fraction and transcripts per cell (gene transcripts of quality 20 or more), median area, cells in
several pieces, the job's time and the files' size. Between them: the pixels that agree (in the
same cell, of those either gives a cell); the transcripts whose cell changed (gained, lost, moved
between cells that are not the same cell); for cells the same in both, the intersection over
union and the boundary displacement, the area between the two outlines over their mean perimeter
(the mean distance the boundary moved: a cell shifted 1 µm sideways moves its two sides 1 µm and
its top and bottom 0, 0.5 µm on average). The same counts by region (squares of 50 µm).

**Spurious co-expression** (Jones DC, Elz AE, Hadadianpour A, et al. Cell simulation as cell
segmentation. *Nat Methods* 2025;22:1331–1342, Methods, "Testing for spurious co-expression"):

- co-expression of gene *i* given gene *j*: the share of cells expressing *j* (one transcript or
  more) that also express *i*;
- spurious pairs: those whose co-expression rises at least 1.5-fold (log₂ ratio ≥ log₂ 1.5) from
  nuclear segmentation to an intentionally coarse expansion of the nuclei;
- a segmentation's score for a pair: its co-expression divided by the nuclei's;
- every cell down-sampled to 50 transcripts (cells with fewer left out), a multinomial draw in its
  own proportions, so that assigning fewer transcripts, or splitting cells, is not rewarded.

Where the paper leaves a choice open, SituCraft's: the nuclei are the dataset's nucleus mask (each
transcript by its pixel), the coarse expansion grows them by 10 µm; pairs need 20 cells expressing
the conditioning gene in both; the pairs are found on the down-sampled cells too; a
segmentation's score is the median over its pairs (the paper shows the pairs' distribution; 1 is
as in nuclei alone); each cell draws from its own seeded stream, so scores are reproducible.

**Mutually exclusive co-expression rate** (Hartman A, Satija R. Comparative analysis of
multiplexed in situ gene expression profiling technologies. bioRxiv 2024,
doi:10.1101/2024.01.11.575135; reviewed in eLife as 96949): for two genes that mark different
cell types, MECR = P(both) / P(either) over cells (expressed: one transcript or more), averaged
over the pairs. The paper takes its pairs from single-cell references; without one, SituCraft
pairs canonical markers of different lineages (its own table: epithelial, T cell, B cell,
myeloid, endothelial, fibroblast, smooth muscle, plasma cell, mast cell), as far as the panel has
them; a reference's pairs can be given instead.

**Measured: the instrument's segmentation (A) and SituCraft's 5 µm expansion (B).**

| | Lung | Breast |
| --- | --- | --- |
| Differences | 11,898 one-to-one | 3,599 one-to-one, 2,829 expanded, 194 contracted, 263 split (into 550 cells), 128 merged (of 259), 6 rearranged, 119 only in A |
| Pixels in the same cell | 98.0629% (as §3.1 counted, independently) | 66.50% |
| Gene transcripts (Q20) that changed cell | 1.063% (all transcripts: 8,724, as §3.1: 2,534 gained, 6,190 moved) | 26.12% (94,904 gained, 10,540 lost, 165,082 moved) |
| Median IoU, cells the same in both | 0.979 (11,898) | 0.719 (6,622) |
| Boundary displacement, median | 0.027 µm | 0.60 µm |
| Spurious co-expression, A and B | 1.870 and 1.886 (2,344 pairs, 1,376 nuclei of 50 transcripts or more) | 1.772 and 2.040 (10,235 pairs, 3,950 nuclei) |
| MECR, A and B | 0.0091 and 0.0091 (251 pairs) | 0.0455 and 0.0501 (337 pairs) |

On Breast, whose cells the instrument drew mostly from stains, the expansion assigns more
transcripts (94.7% against 86.6%) at the cost of more spurious co-expression, as Jones et al.
found of expansions; on Lung, segmented by the instrument the same way, the two are alike.

### 4.1 Errors planted for practice (verified, wave 2, slice 7)

The Lung worked example plants errors in the instrument's segmentation (`internal/segment/plant`,
seed 20261014): 20 merges of neighbouring cells sharing at least 8 pixel edges, each with 30 gene
transcripts or more, chosen first among the pairs whose merged cell would express two lineage
markers neither does alone (the comparison's own marker pairs), then among the quarter least alike
by the cosine of their counts; 20 splits of cells larger than the median with 40 gene transcripts
or more, cut across their long axis (the pixels' second moments) through the centroid; 10 cells
with 20 gene transcripts or more dropped; and 10 cells with three neighbours or more grown by
1 µm into their neighbours' pixels and the background. Each error's cells and their neighbours are
kept from every other error's. The transcripts of changed cells go to the cell holding their pixel
afterwards; every other transcript keeps its cell.

**Measured (Lung, the comparison of the instrument's segmentation with the planted one):** 11,811
cells one-to-one; the 20 merges each a merged difference, every merged cell mixing more pairs of
mutually exclusive markers than its two cells (so ranked by contamination they come first); the
20 splits each a split; the 10 missed cells each only in the instrument's; the 10 grown cells each
expanded (7 of their neighbours contracted); no other difference, and no other cell's transcripts
changed. A first reach of 2 µm made four grown cells take more than half of a neighbour, and so
"rearranged" differences: Lung's cells are small (median 48.5 µm²).

### 4.2 Exports (verified, wave 2, slice 7)

**Measured: the Lung instrument segmentation exported in every format** (11,898 cells, 289 genes,
825,885 transcripts of which 809,249 assigned, 294,927 nonzero counts summing to 731,083). Read
back by SituCraft, the SpatialData store imports as the same segmentation: every pixel, every
transcript's cell and every count (the content fingerprints equal). Read by spatialdata 0.8.0,
anndata 0.13.4 (zarr 3.4.1), pyarrow 26.0.0 and pandas 3.0.6, scipy 1.18.1, geopandas 1.2.0 and
numpy 2.5.4 (`validation/reference/read_exports.py`, `exports.json`), every format gives the
manifest's totals and the fingerprints it holds; spatialdata finds the table annotating the labels
(region `cell_labels`, instance key `cell_labels`), the labels scaled by 0.2125 into µm and the
points and shapes in µm. geopandas finds 44 of the 11,898 outlines invalid: the instrument's own
polygons whose edges cross (§1, wave 1 found the same 44). Xenium Ranger's masks are the
morphology image's size (5,791 × 3,553), with the bundle's nuclei; 9 transcripts (8 gene
transcripts of quality 20 or more) are assigned by the instrument otherwise than its own mask holds
at their pixel (§3.1: the 8 it records off its mask).

## 5. Cell typing (wave 4)

- **Normalization:** library-size normalization and log1p.
- **Embedding and graph:** principal components; a kNN graph.
- **Clustering:** Leiden (Traag, Waltman & van Eck 2019, *Sci Rep* 9:5233,
  doi:10.1038/s41598-019-41695-z).
- **Display:** UMAP (McInnes, Healy & Melville 2018, arXiv:1802.03426).
- **Marker scores:** gene-set scores against control gene sets binned by expression (Tirosh et al.
  2016, *Science* 352:189–196, doi:10.1126/science.aad0501); §5.3.
- **Reference mapping:** correlation-based mapping to a labeled single-cell reference with
  iterative fine-tuning (Aran et al. 2019, *Nat Immunol* 20:163–172,
  doi:10.1038/s41590-018-0276-y).
- Benchmarks on Xenium show performance depends on the dataset (Cheng et al. 2025), so no method
  is the only default.
- **Cluster markers:** Wilcoxon rank-sum, labeled exploratory (cells are not replicates).

### 5.1 Clusters (verified, wave 4, slice 1)

`internal/cluster` takes the steps of the common single-cell workflow (Wolf, Angerer & Theis 2018,
SCANPY, *Genome Biol* 19:15, doi:10.1186/s13059-017-1382-0), with scanpy's defaults, so that a
user who knows that workflow finds the same numbers:

1. **The cells:** a segmentation's counts (gene transcripts of quality 20 or more), the cells with
   at least 10 of them, or those a saved QC filter keeps (sent with the filter's checksum, which
   the program checks). Cells without a gene transcript are left out and counted.
2. **Normalization:** each cell's count of gene g, c, becomes log(1 + c/(t/T)), t its total and T
   the median total of the cells analyzed (scanpy's `normalize_total` with `target_sum=None`, then
   `log1p`).
3. **Scaling:** each gene centered and divided by its standard deviation (n − 1 in the
   denominator; 1 for a gene without variance), clipped at ±10 (scanpy's `scale(max_value=10)`).
4. **Principal components, exact:** the eigenvectors of the genes' covariance, by Householder
   reduction and the QL method with implicit shifts (Wilkinson & Reinsch 1971, procedures tred2
   and tql2; Golub & Van Loan 2013, §8.3). SituCraft's choice: the covariance is computed from the
   sparse counts without forming the dense matrix. A cell's scaled values are each gene's value at
   zero, z₀ = clip(−μ/σ), plus a sparse difference d over the genes it expresses; with a = z₀ minus
   the scaled values' mean, the covariance is (Σᵢ dᵢdᵢᵀ − n·aaᵀ)/(n − 1), exact for any clipping.
   Its cost grows with the square of the genes each cell expresses, not of the panel. Each
   component is signed so that its largest loading in magnitude is positive (scikit-learn's
   convention, which scanpy follows). 50 components, or one fewer than the genes or cells.
5. **Neighbors:** each cell's 14 nearest other cells in the components (15 neighbors counting the
   cell, as scanpy and umap-learn count them), by Euclidean distance, exactly. Equal distances are
   ordered by the cells' order, so the table does not depend on the number of threads.
6. **The graph:** UMAP's fuzzy simplicial set of the neighbors (McInnes et al. 2018; umap-learn's
   `fuzzy_simplicial_set` with its defaults, the graph scanpy clusters on): for cell i, ρᵢ is the
   distance to its nearest neighbor and σᵢ is found by bisection so that
   Σⱼ exp(−max(0, dᵢⱼ − ρᵢ)/σᵢ) = log₂ 15; the weight of i → j is exp(−(dᵢⱼ − ρᵢ)/σᵢ); the
   undirected weight a + b − ab of the two directions'.
7. **Leiden** (Traag et al. 2019) on the graph, optimizing modularity at a resolution γ (1 by
   default; Reichardt & Bornholdt 2006): fast local moving, the refinement that merges only
   well-connected subsets (randomness θ = 0.01, leidenalg's default), aggregation on the refined
   partition; iterations repeat until one moves no cell (at most 50). Clusters are numbered by
   decreasing size. Carried to Go from CytoWeave's `graph-cluster.js`, which follows the
   arrangement of the authors' networkanalysis package.
8. **Markers,** exploratory: each cluster against the other cells analyzed, gene by gene, by the
   Wilcoxon rank-sum test (Mann & Whitney 1947) with ties corrected: z = (R − n₁(n + 1)/2)/σ,
   σ² = n₁n₂(n + 1)/12 · (1 − Σ(t³ − t)/(n³ − n)); two-sided p-values from the normal
   approximation, adjusted over the genes by Benjamini and Hochberg's procedure; the log₂ fold
   change log₂((expm1(m₁) + 10⁻⁹)/(expm1(m₂) + 10⁻⁹)) of the clusters' mean log-normalized values
   m₁ and m₂, and the shares of cells expressing the gene in and out, as scanpy's
   `rank_genes_groups` (method `wilcoxon`, `tie_correct`) computes them. SituCraft's choice: cells
   are ranked by their count over their total, compared exactly in integers, so that cells
   expressing a gene equally tie. Ranking the rounded log-normalized values, as scanpy does, can
   part two of them by a rounding (log(1 + x) and log1p(x) part different pairs), which moves a
   score by up to a few hundredths.

Every product is rounded on its own, sums are taken in a fixed order, logarithms, exponentials and
the error function come from `internal/dmath` (fdlibm's), and Leiden draws from one seeded stream:
the files are the same on any number of threads, on arm64 and amd64 (pinned digests).

**A clustering is an artifact** of kind `clustering` (`docs/FORMATS.md`), named by the
segmentation, the cells analyzed, the method's version and the parameters. Its check reads every
file, requires every analyzed cell to be in a cluster numbered by size, makes the graph again from
the neighbors and requires the recorded modularity, and requires every number finite and every
p-value a probability.

**Against scanpy 1.12.4, leidenalg 0.12.0, umap-learn 0.5.12 and scikit-learn 1.9.1**
(`validation/reference/clustering/`, maintainer-run; the bundles read by scanpy from their own
`cell_feature_matrix.h5`, in float64; checked weekly against SituCraft's clustering made again, byte
for byte):

| | Lung 2-FOV | Breast 2-FOV | Simulated (standard) |
| --- | --- | --- | --- |
| Cells analyzed (the same as scanpy's ≥ 10 counts) | 11,820 | 7,163 | 2,925 |
| Components' variances, largest relative difference | 6.8 × 10⁻¹⁴ | 2.7 × 10⁻¹⁴ | 1.7 × 10⁻¹⁴ |
| Components' directions (\|cos\|, all 50) | 1.000 | 1.000 | 1.000 |
| Cells with the same neighbors as exact search | 100% | 100% | 100% |
| Modularity on umap-learn's graph of the same neighbors, difference | 3 × 10⁻⁸ | 1 × 10⁻⁸ | 2 × 10⁻⁸ |
| SituCraft's clusters, modularity | 29, 0.6177 | 15, 0.5982 | 5, 0.7166 |
| leidenalg, ten seeds: clusters, modularity | 26–29, 0.6138–0.6173 | 12–16, 0.5927–0.6001 | 4–6, 0.7161–0.7167 |
| Agreement (ARI) with leidenalg, median; leidenalg with itself | 0.711; 0.703 | 0.620; 0.662 | 0.983; 0.978 |
| Markers: scores against the rank-sum test with exact ties (scipy) | identical | identical | identical |
| Markers against scanpy: log₂ fold change; scores | 1 × 10⁻⁶; 0.017 | 1 × 10⁻⁶; 0.002 | 7 × 10⁻⁷; 0.002 |
| Each cluster's first ten markers, the same as scanpy's | 100% | 100% | 100% |
| scanpy's own pipeline (its neighbors and Leiden): clusters, ARI | 28, 0.736 | 13, 0.579 | 4, 0.983 |
| The instrument's graph-based clusters: clusters, ARI | 17, 0.044 | 9, 0.431 | — |
| Against the true cell types (4): ARI, SituCraft; scanpy | — | — | 0.955; 0.971 |

The tolerances (`validation/clustering_test.go`), set before the comparison: variances within
10⁻⁶, the first ten components within |cos| 0.9999 and the fifty's subspace within 0.999; 99.9% of
cells with the same neighbors; modularity on umap-learn's graph within 10⁻⁵; Leiden's modularity
no lower than leidenalg's lowest over ten seeds less 0.003, and its agreement with leidenalg no
lower than leidenalg's with itself less 0.10; markers within 10⁻⁹ of the exact rank-sum test, 10⁻⁴
of scanpy's fold changes, 0.05 of scanpy's scores, and the same first ten genes; on the simulated
tissue, agreement with the truth no lower than scanpy's less 0.02 (and at least 0.85). All are met.

What the numbers say: everything up to the graph is the same computation as scanpy's, to rounding.
Leiden is random, and two of its runs with different seeds agree about as much as SituCraft's agrees
with leidenalg's (on Lung, about 0.7: its clusters at resolution 1 are many and small); SituCraft's
modularity is within leidenalg's range, above it on Lung. scanpy's own pipeline differs further on
the real bundles because, above 4,096 cells, its neighbors are approximate (pynndescent). The
instrument's graph-based clustering is not scanpy's: on Lung neither SituCraft's clusters nor
scanpy's agree with it (ARI 0.044 and 0.048), though the instrument's largest cluster leads with
the genes SituCraft's largest does (CD24, CXCL13, TM4SF4). On the simulated tissue, a type is split
in two at resolution 1 (5 clusters for 4 types), as it is in two of leidenalg's ten runs.

### 5.2 The map, exploring clusters, and the instrument's analysis (verified, wave 4, slice 2)

**The map** (`internal/embed`) lays a clustering's cells out by UMAP (McInnes, Healy & Melville
2018) on the clustering's own graph (§5.1, step 6), as umap-learn's `simplicial_set_embedding` lays
out a fuzzy graph and scanpy's `tl.umap` calls it, with scanpy's defaults (minimum distance 0.5,
spread 1, 500 epochs up to 10,000 cells and 200 above, 5 negative samples per pull):

1. the curve 1/(1 + a·d^{2b}) fitted to the membership it stands for on 300 points of [0, 3] by
   least squares (umap-learn's `find_ab_params`; here Levenberg–Marquardt with the analytic
   Jacobian): a = 0.58303, b = 1.33417 for the defaults, umap-learn's to 4 × 10⁻⁷;
2. edges below the heaviest's 1/epochs pruned (never sampled);
3. the start: the graph's normalized Laplacian's two eigenvectors with the smallest nonzero
   eigenvalues (Belkin & Niyogi 2003), as umap-learn's `spectral_layout`, by Chebyshev-filtered
   subspace iteration (Zhou & Saad 2007) with Rayleigh–Ritz steps, scaled to a largest coordinate
   of 10 with noise of standard deviation 10⁻⁴, then each axis to [0, 10]. SituCraft's choice: a
   graph in pieces starts from the first two principal components (umap-learn lays the pieces out
   apart);
4. umap-learn's `optimize_layout_euclidean`: each epoch, every edge due (every w_max/w epochs)
   pulls its two cells together by the gradient of log(1/(1 + a·d^{2b})) and pushes the first away
   from random cells, gradients clipped at ±4, the learning rate falling linearly to 0. SituCraft's
   choice: one thread and one seeded stream, in float64, so that a seed gives the same map on any
   processor (umap-learn computes in float32 and, in parallel, not reproducibly).

**Its quality,** measured on every map: trustworthiness T(15) (Venna & Kaski 2001), exact over
2,000 query cells evenly spaced through the cells (every n/2,000-th) against all cells, in the
clustering's principal components, ranks by distance then by the cells' order (over every cell it
is `sklearn.manifold.trustworthiness`); and each cell's neighborhood precision and recall
(Venna et al. 2010): the share of its 15 nearest on the map among its 75 nearest in the components,
and of its 15 nearest in the components among its 75 nearest on the map. A cell's score, the mean
of the two averaged over it and its 15 map neighbors, colors the map. The page says how far the map
can be trusted in plain words: maps of 50 components of 300-gene panels keep neighborhoods only in
part (trustworthiness 0.74 to 0.84 below), so a fixed threshold of "unreliable" cells (CytoWeave's
0.4, made for cytometry) would mark most cells; the trustworthiness and recall are said instead,
with what the map can and cannot show.

**Against umap-learn 0.5.12** on the same graph (`validation/reference/clustering/`; five seeds of
umap-learn's, the same measures computed in Python from each map's places):

| | Lung 2-FOV | Breast 2-FOV | Simulated (standard) |
| --- | --- | --- | --- |
| SituCraft's map: trustworthiness (recomputed: identical) | 0.737 | 0.779 | 0.840 |
| umap-learn's, five seeds | 0.738–0.747 | 0.778–0.782 | 0.838–0.841 |
| Recall: SituCraft; umap-learn | 0.220; 0.217–0.223 | 0.235; 0.234–0.238 | 0.281; 0.281–0.286 |
| Time (layout; quality) | 5.4 s; 1.0 s | 9.3 s; 0.6 s | 3.8 s; 0.1 s |

The tolerances: the curve within 10⁻⁵ of umap-learn's; the trustworthiness recomputed from the
places within 10⁻⁴ of the recorded; no less than umap-learn's lowest of five seeds less 0.01. All
are met. On Lung SituCraft's default seed happens to give the lowest of its own five seeds
(0.737–0.746, mean 0.742, against umap-learn's mean 0.743): the two spread alike.

**Exploring clusters:**
- **Expression** for colors, the dot plot and a selection's markers: each analyzed cell's
  log-normalized value of a gene, as the clustering normalized it (its target total); the dot plot,
  each cluster's mean of it and share of cells expressing it, for each cluster's two leading
  markers.
- **A selection's markers:** the cells selected (lassoed on the map or the tissue, or chosen by
  cluster) against the clustering's other cells, by the rank-sum test of §5.1 with two groups;
  exploratory. A cluster's markers are exactly its cells' contrast with the rest (tested).
- **Names:** each change of a clustering's names is a new version, appended (the project's
  `cluster-names` records); the clusters keep their numbers. Clusters given the same name are one on
  the map, the tissue and the legends, in the first one's color.
- **Two clusterings compared:** their contingency over the cells both cluster and the adjusted Rand
  index (Hubert & Arabie 1985).

**The instrument's analysis** (`internal/xenium/analysis.go`): Xenium Onboard Analysis writes its
graph-based and k-means (2 to 10) clusters, a 10-component PCA and a UMAP as CSV in
`analysis.tar.gz` (or `analysis/`). A clustering of the instrument's is registered as a clustering
of the instrument's segmentation (`source` naming the tool, version, file and clustering), its
cells matched by id, its numbers kept, its markers computed by SituCraft (§5.1, step 8) from the
cells' counts normalized to their median total. The instrument's map is registered with the first,
its quality judged against the instrument's own 10 components (Breast 0.903: a 10-component space
is smoother than 50, so the two maps' numbers do not compare). Read against the bundle's other copy
of the graph-based clusters (`analysis.zarr.zip`'s `cell_groups`): every cell the same. That copy
pads its index list to every cell's count: a cell without transcripts, in no cluster, leaves a
trailing 0 in the last group, so cell 0 is listed twice.

### 5.3 Labels from markers (verified, wave 4, slice 3)

**A marker set** (`internal/label`, `docs/FORMATS.md`) is a hierarchy of labels (a class, a
lineage, a subtype; up to six levels) and states (proliferating, say), each with positive and
negative marker genes and the published sources they come from. SituCraft ships one for each
development bundle's panel, every marker in the panel; the editor makes versions, kept in the
project, and a set is saved and read as a file.

**Each cell's score** for each label is computed as scanpy's `score_genes` computes it (Tirosh
et al. 2016, *Science* 352:189–196, doi:10.1126/science.aad0501; Wolf et al. 2018), on the
expression as the clustering normalized it (§5.1, step 2):

1. each gene's mean over the cells; ranks by mean, equal means given the lowest rank (pandas'
   `rank(method="min")`); the genes cut into 25 bins of round(G/24) ranks (numpy's rounding, half to
   even);
2. a list's control genes: the genes of each bin holding one of its markers, or 50 of them drawn at
   random from a bin holding more, less the markers themselves. On panels of up to 1,200 genes no
   bin holds more than 50 and nothing is drawn; above, SituCraft draws from its own seeded stream,
   not numpy's;
3. the score: the markers' mean value less the controls' mean value, in each cell.

SituCraft's choices:
- **Negative markers:** a label's score is its positive markers' score less its negative markers'
  (scored alike) where that is above 0: a negative marker counts against a label where the cell
  expresses it above its controls, and its absence earns nothing.
- **The lineage left out of the controls** (scanpy's `gene_pool`; on by default): a label's control
  genes leave out its ancestors' and descendants' positive markers. On a targeted panel, genes of
  matched expression are often other labels' markers, and a type's class's markers its own cells
  express: as controls they cancel its score. On the planted tissue of `internal/label`'s tests (two
  classes of two types, 200 genes) the types are labeled 91.2% right with them left out and 89.0%
  with every gene as controls; on Breast the lineage level's leads (10th and 25th percentiles) rise
  from 0.002 and 0.021 to 0.022 and 0.061.

**Labels from the top down.** At each level a cell's candidates are the children of the label it
took above (the top level's labels at first). The best is taken when it leads the next by the
margin (0.1) and scores at least the minimum (0); a label without siblings must lead the minimum.
Otherwise the cell stays at the level above: it is not forced into a subtype its markers do not
distinguish. Equal scores lead by 0 and are not taken. A cell's confidence is the lead of the label
it carries (for a cell without a label, the top level's). A state is carried where its score
reaches the state threshold (0.25), by cells of its label (or below it) when it names one. Scores
are in log-normalized expression: in a cell of the median total, one transcript of a marker adds
log 2 ≈ 0.69 over the markers' number, so a margin of 0.1 is about one transcript more of a 6-gene
label's markers than of the next label's, and a threshold of 0.25 about two transcripts of a 5-gene
state's.

**Each cluster's labels:** for each level, a cluster's cells by the label they carry there (a cell
that stopped above counted under the label it stopped at). Names for the clusters come from them on
request: each cluster the most specific label at least a share (one half by default) of its cells
carry, a new version of the clustering's names.

**The artifact** (kind `labels`) keeps each cell's score for every label and state with its
positive and negative parts, its label and lead at each level, its confidence and states, each
label's markers and control genes, each cluster's composition, and the marker set (`markers.json`)
by its checksum. Its check requires every cell's label to be the best of its candidates by the
recorded scores, taken only by the margin and left only below it; the leads, confidences, states
and composition to follow from them.

**Against scanpy 1.12.4** (`validation/reference/labels/`): each data set's cells, read and
normalized by scanpy from the bundle's own matrix, scored by `score_genes` with SituCraft's gene
pool for every label's positive and negative markers:

| | Lung 2-FOV | Breast 2-FOV | Simulated (standard) |
| --- | --- | --- | --- |
| Lists scored (positive and negative) | 43 | 40 | 4 |
| Control genes the same as scanpy's | 43 | 40 | 4 |
| Largest difference of a cell's score | 2.7 × 10⁻⁷ | 2.8 × 10⁻⁷ | 1.7 × 10⁻⁷ |
| Cells labeled: top level; second; third | 47%; 12%; 1% | 81%; 49%; 6% | 98.7% |

The tolerances: every list's controls scanpy's; each score within 10⁻⁵ (scanpy scores the float32
matrix it reads, SituCraft in float64); a label's score from its parts within 10⁻⁶. All are met.
The same files on arm64 and amd64.

**Known truth:** on the standard simulated tissue, labeled from each type's own markers as the
simulation made them, 97.2% of the cells take their true type (that of the true cell holding most
of their transcripts), 1.5% another, 1.3% none (tolerances: at least 96%, at most 2%).

**The development bundles.** Both 2-FOV regions are dominated by tumor, and their cells are sparse
(median totals 111 transcripts on Breast, 57 on Lung). On Breast the tumor clusters are
Epithelial (with Luminal or Basal where their markers lead), the macrophages Macrophage or
Monocyte, the fibroblasts Fibroblast (71%), the vessels Endothelial (72%), the T cells CD8 and CD4
T cells. On Lung about half the tumor cells take no label: see the second finding below.

Found by slice 3:
- **Markers that are textbook elsewhere are not in a tumor's tissue.** In the Breast region the
  tumor cells express LPL (69% of a tumor cluster's cells) and PDGFRA (52%), and FOXA1, GATA3 and
  ESR1 are nearly absent; in the Lung region the tumor expresses COL5A2 and PROX1. The starter sets
  leave these out of the labels they would mislead (Adipocyte, Stromal, Lymphatic endothelial), and
  say so. Abundant transcripts reach neighboring cells: in Breast's T cells LUM is at 1.67
  (log-normalized; 2.54 in the fibroblasts), so Stromal counts immune markers (PTPRC, FCER1G, CD3E)
  against it. Cells of the T cluster that carry fibroblasts' LUM and SFRP4 at fibroblasts' levels
  (100% and 92% expressing) are labeled Fibroblast: outlines holding parts of two cells, which the
  per-cluster shares show.
- **Control genes binned by expression cancel a dominant type's markers on a targeted panel.** A
  bin's genes are, on a panel built of cell-type markers, other types' markers; in a tissue a type
  dominates, the top bins are its own genes (on Lung CXCL13, CD24, TM4SF4, ATP1B1, SEC11C, AGR3,
  EHF …), so EPCAM's and CDH1's controls rise and fall with them and the epithelial score of the
  tumor's cells is near 0 — as scanpy's is. On Lung about half of them take no label; on Breast,
  17% to 28% of each tumor cluster. The scores are scanpy's by design; the label set says which
  cells stayed without a label, and the clusters' markers name them.
- **The margin is over the next label, not over 0.** Measuring a label's lead over the larger of
  the next and the minimum left a dominant type's cells unlabeled whenever its depressed score was
  near 0 even with every other label far below it (Breast's tumor clusters: 24% to 35% without a
  label); the minimum is a separate gate.
- **A state needs its own threshold.** At the margin, one transcript of a 4-gene proliferation set
  passes in a cell of the median total: 36% of Breast's cells were proliferating. The threshold of
  0.25 (about two transcripts) keeps 1,886 (26%), most in the cluster led by TOP2A and MKI67.
- **Averaging each cell's scores over its 15 nearest cells in expression** (the clustering's
  neighbors) labels more of Breast (81% to 97% at the top level) but loses every rare type (mast
  cells 28 to 0, plasma cells 11 to 0, B, NK and dendritic cells), and labels fewer of Lung (47% to
  28%): not taken.

### 5.4 Imported labels (verified, wave 4, slice 4)

Labels made elsewhere — another tool's predictions, an earlier analysis, a colleague's
annotation, the instrument's own clusters — are imported for a segmentation's cells, matched by
the cells' ids, and kept as an artifact of kind `labels` with their source (`internal/label`'s
`table.go` and `imported.go`; `docs/FORMATS.md`, Labels SituCraft imports).

**What is read.** A CSV or TSV table (gzipped or not; a column of numbers wherever every value is
one); a Parquet file (text, numbers, booleans, dictionary-encoded columns); an AnnData object's
`obs` in Zarr (format 2 or 3, a folder or a zip; its index and every column, with anndata's
categorical and nullable encodings), or a SpatialData store's table; Xenium Explorer's cell groups
(a CSV of `cell_id`, `group` and optionally `color`, a group's HEX color given once, as Xenium
Explorer documents its import, 10x Genomics, *Xenium Explorer: Cells*); and the instrument's
graph-based and k-means clusters from `analysis.tar.gz` (§5.2). h5ad files are not read: anndata
writes Zarr with `write_zarr`, and whether to read HDF5 AnnData is decided with references (slice
6).

**Matching.** The cells' ids are matched exactly to the segmentation's (the bundle's own for the
instrument's segmentation, SituCraft's for its own, as its exports write them). SituCraft's
choices:
- **A table naming a cell the segmentation does not have is refused**, with how many and the first
  few: labels made for another segmentation, or another sample, would otherwise be attached to the
  cells they happen to share ids with. The page names the dataset's other segmentations whose
  cells the ids are, when one is.
- **A cell named twice is refused**; so is a label column of more than 1,000 values (an id or a
  measurement, not labels).
- **Cells the table does not name have no label** and are counted; so are rows whose label is
  empty or `NA`, `nan`, `None`, `null`.
- **The columns are suggested by their usual names** (the ids `cell_id`, `Cell ID`, `cell`,
  `barcode`, an AnnData index; the labels `group`, `cell_type`, `label`, `annotation`, `cluster`,
  `leiden` …; a confidence column of numbers named for a score or probability; colors), and chosen
  by the user.

**The artifact** keeps each of the segmentation's cells' label and confidence (NaN where none was
given), the labels by decreasing cells with their colors where the table gave them, and the
source: its format, file and SHA-256 (of an AnnData store, of its `obs`'s files), the columns and
rows. Each cluster's labels are counted in the page for the clustering shown, so one imported
label set serves every clustering of its segmentation. The check requires every cell, in order,
one level, each label an index among the labels and counted as described.

**Validation:**
- **Files written by pandas 3.0.6, pyarrow 26.0.0 and anndata 0.13.4** (`internal/label/testdata`:
  CSV and gzipped TSV; Parquet with a dictionary-encoded column and float32 nulls; AnnData in Zarr
  formats 3 and 2 with a categorical column and a missing value, the ids as the index; a
  SpatialData-like store's table; Xenium Explorer's groups): every cell's label and confidence as
  written, the columns' roles found.
- **Refused:** a table naming a cell the segmentation does not have (with the count), one naming a
  cell twice, a confidence column of text, a missing column.
- **The development bundles:** each bundle's graph-based clusters written as a table and imported
  for the instrument's segmentation, every cell its cluster (Lung 11,897 of 11,898 cells, Breast
  7,273 of 7,275; the others not clustered by the instrument, without a label); the Lung table
  refused for the Breast cells, all 11,897 rows counted.
- **In the app:** a table of three Lung cells (their ids as `select_cell` answers them) imported by
  remote control, those cells and no other labeled; a table naming another dataset's cell refused;
  the instrument's graph-based clusters imported, 11,897 cells in 17 clusters (viewer suite).

### 5.5 Label versions, evidence and review (verified, wave 4, slice 5)

**Versions.** A segmentation's cells carry predicted labels (from markers, §5.3; imported, §5.4),
curated labels and consensus labels, each a label set the map, the tissue, the Viewer and the
review treat alike (`ui/label-sources.js` resolves any of them to each cell's label):
- **Curated:** a base set (any version, at a level) and a reviewer's edits appended to it — cells
  relabeled, a clustering's clusters relabeled, or cells' labels accepted — each with its reason,
  who and when (`lib/curation.js`). Edits are never changed or removed: undoing one is another edit.
  SituCraft's choice: a decision is an edit of a curated set begun from the set reviewed, so the
  predicted set stays as the method made it and the curated one says what changed and why.
- **Consensus:** chosen sets, each one's labels mapped to common names where they name them
  differently, and a rule: **unanimous** (every set labels the cell, all alike), **majority** (at
  least a number of sets give the label, more than give any other; equal votes give none), or **in
  order** (the first set's label, else the next's). A consensus's confidence is the share of its sets
  agreeing.
- **Side by side:** two versions' cross-table over the cells both label, with the share whose labels
  have the same name.

**The evidence of a cell's label** (`internal/label/evidence.go`, the page's evidence card): its
label in every version; for labels from markers, at each level the label it took (or the best,
where it stopped above) and the next, with each one's score and markers in the cell — each gene's
transcripts and log-normalized value against its mean, and the controls' mean in the cell (as the
recorded scores, to 10⁻⁶) — and, when most of its nearest cells carry another label, that label's
evidence ("why not"); its neighbors' labels in the tissue (its 10 nearest labeled cells by
centroid) and in expression (the clustering's 14 neighbors); its transcripts, genes, area and its
segmentation's flags (few transcripts, uncertain boundary, no nucleus; boundary certainty); and its
reviews.

**The review queue** (`internal/label/review.go`): each labeled cell's agreement — the share of its
k nearest labeled cells (by centroid, exactly, `internal/graph` KNN2D; 10 by default) carrying its
label — and the label most of them carry. A cell is **spatially implausible** when at most a share
(0.2) of them carry its label and most carry another; of **low confidence** when its confidence is
below a threshold (labels from markers: the lead, by default under twice the margin; imported: the
table's). The queue ranks cells flagged by both first, then the implausible by agreement, then the
least confident; cells reviewed already are left out. Segmentation-sensitive cells join it with slice
7. SituCraft's choice: the share agreeing, not a likelihood of the cell's label given its
neighbors' labels against each label's usual neighborhood: on the Breast tissue's nests and stroma
the share finds planted mislabels earlier (1% or 3% planted, 10 or 15 neighbors: 53% to 85% of them
in the first twice as many cells as were planted, against 38% to 60%; 85% to 96% in the first five
times as many, against 75% to 92%; on the simulated tissue the two alike), and it is said in plain
words ("2 of 10 nearest share it; most are Epithelial").

**Validation:**
- **Consensus rules on built sets** (`lib/curation.test.mjs`): three sets naming labels differently,
  mapped; unanimous, majority (with equal votes) and in order cell by cell; edits appended and applied
  in order (a cluster edit over a cell edit, an acceptance); the cross-table and agreement by hand.
- **The queue by hand** (`internal/label`): agreement on a grid (equal distances by index), the
  queue's order and counts, the reviewed left out; a cell's evidence equal to its recorded scores.
- **Planted mislabels** (`validation/labels_test.go`): 3% of a tissue's labeled cells given another
  class at random (seeded), the queue at the defaults:

| | Simulated (standard) | Breast 2-FOV |
| --- | --- | --- |
| Cells labeled; planted | 2,887; 97 | 5,821; 170 |
| The queue (cells flagged) | 711 (24.6%) | 616 (10.6%) |
| Planted found: in the queue; in its first five times as many | 75.3%; 70.1% | 94.7%; 94.7% |

  The simulated tissue's domains mix their types, so a mislabel its neighbors share cannot be found
  by place: the queue flags a quarter of its cells, and the evidence card is the review's.
- **In the app:** the Lung labels' queue by remote control (the implausible in rising agreement, at
  most 0.2); a cell relabeled with its reason, drawn in its new label's color and no other cell
  changed; a consensus of the set and its curated version agreeing everywhere but on that cell
  (viewer suite).

### 5.6 Labels from a reference (verified, wave 4, slice 6)

A clustering's cells are labeled from a labeled single-cell reference by correlating each cell with
each label's expression profile, then again on the leading labels' own genes (Aran et al. 2019,
*Nat Immunol* 20:163–172, doi:10.1038/s41590-018-0276-y), and kept as an artifact of kind `labels`
with the reference in place of a marker set (`internal/label`'s `reference.go` and
`reference_files.go`; `docs/FORMATS.md`, Labels from a reference).

**What is read.** An AnnData object in Zarr (format 2 or 3, a folder or a zip): its cells' labels
in an `obs` column, its genes' names in `var` (the index or a column), and its expression in
`layers/counts`, `raw/X` or `X` (counts preferred; the first of these holding counts or
log-normalized values is suggested, and each is described from its first chunk without reading it
whole). Or a table of profiles (CSV, TSV or Parquet): a row per label (a column of labels, the
genes the numeric columns) or a row per gene (a column of genes, the labels the numeric columns).
The usual names of label columns (`cell_type`, `celltype`, `cell_ontology_class`, `ann_finest_level`,
`label.main` …) are suggested. A matrix in sparse rows is read a chunk at a time, twice (each
cell's total first, then each label's sums), so an atlas of hundreds of thousands of cells needs no
more memory than a chunk (a simulated reference of 200,000 cells is read in 0.9 s). **h5ad files
are not read:** SituCraft's own HDF5 reader would serve
one format, and anndata writes the same object as Zarr in one line (`adata.write_zarr`); whether to
read HDF5 is left until users ask for it (roadmap, the parking lot).

**The reference restricted to the panel.** Gene names are matched exactly, else ignoring case where
that names one panel gene; the genes shared are reported, and the panel's genes the reference lacks
listed (the page warns below half: symbols against ids, or another species). SituCraft's choices:
- **One profile per label:** its reference cells' mean log-normalized expression — counts scaled to
  the reference's median total, then log1p, as SituCraft's clustering normalizes cells;
  log-normalized values averaged as given; scaled values (below 0) refused, since their ranks are
  not expression's. A table's profiles are taken as given. SingleR by default correlates a cell
  with every reference cell or sample and takes a quantile of each label's; one profile per label
  is what the evidence card can show, costs the same for any size of reference, and is measured
  against the per-sample default below.
- **At most 500 labels**, each pair of which is compared.

**The genes compared.** For each ordered pair of labels (a, b), the shared genes most higher in a's
profile than b's — at most 500 × (2/3)^log₂(labels), the published number (60 for 36 labels), only
those higher at all, ties in the panel's order; for a set of labels, the union over its pairs. The
number per pair is a parameter (**Genes per pair**; 0 for the published one).

**The score** of a cell for a label is the Spearman correlation of its log-normalized values and the
label's profile over the genes compared (equal values their mean rank). A cell's genes without
transcripts tie at the lowest ranks, so the sums run over the genes it expresses; a cell expressing
too few of them to rank (all equal) takes no label.

**Fine-tuning.** The labels within **Fine-tuning** (0.05, the published threshold) of the best
correlation are scored again over their own pairs' genes, until one is left or none drops; the cell
takes the last round's best when it leads the next by the **margin** (SituCraft's; 0 by default:
the best unless two tie). A cell's confidence is that lead, in correlation.

**The artifact** keeps the reference as compared in `reference.json` (each label's reference cells
and profile over the shared genes), every cell's first-round correlation with every label as its
score, its last round's correlations (none where there was one round), its label and lead. The
check requires `reference.json` to give the description's labels and shared genes, its pairs' genes
the first round's genes, each cell's label the best of its deciding round by the margin and within
the fine-tuning threshold of its first round's best, its lead its confidence, and the counts.

**The evidence card** of a cell labeled from a reference maps it again from `reference.json`: each
round's labels and correlations; the deciding round's best label and the next, with the genes most
higher in each one's profile than the other's and the cell's transcripts of them (green: expressed
for its label; red: expressed for the next; dashed: not expressed); and, when most of its nearest
cells carry another label, where that label stood in the first round. The review queue's low
confidence for these labels is, by default, a lead below the fine-tuning threshold or twice the
margin, whichever is more.

**A reference for the simulated tissue.** `situcraft simulate --reference N` writes, beside the
bundle, N cells of each of its types drawn from the simulation's own expression profiles (clean of
the tissue's background and displaced transcripts, 1,000 expected transcripts over the panel, spread
log-normally), as an AnnData object in Zarr (`internal/simulate`'s `reference.go`).

**Validation:**
- **Exact cases** (`internal/label`): ranks with ties; a Spearman correlation by hand (√0.9); the
  pairs' genes by hand; a cell's rounds by hand (0.4, 0.8 and −1.5/√22.5; within 0.5, a second round
  over the two labels' genes, −1 and 1); the mapping against a plain one sorting every pair afresh
  and ranking every value (20 random references with tied values, 1,000 cells, each round's labels,
  genes and correlations within 10⁻¹²; 163 cells changing label in fine-tuning); a reference's
  profiles by hand from AnnData counts (median scaling, a label missing); tables in both
  orientations; scaled values and a label given twice refused; a tissue mapped to another draw of
  its types (86% right; the same labels for any number of workers; the files checked and pinned on
  arm64 and amd64); a cell's evidence equal to its recorded scores.
- **Against SingleR 2.14.2** (R 4.6.1; `validation/reference/references/`), run as a separate program
  on the same cells: the bundles mapped to the Human Primary Cell Atlas's 36 main labels (Mabbott et
  al. 2013, *BMC Genomics* 14:632, doi:10.1186/1471-2164-14-632; celldex 1.22.0's log-expression,
  each label's mean as its profile), the simulated tissue to its reference of 300 cells per type:

| | Lung 2-FOV | Breast 2-FOV | Simulated (standard) |
| --- | --- | --- | --- |
| Genes shared; compared in the first round | 280; 280 | 270; 270 | 120; 120 |
| Cells; fine-tuned; changed by fine-tuning | 11,820; 10,139; 1,472 | 7,163; 7,116; 696 | 2,925; 96; 0 |
| SingleR given the same profiles: first-round correlations, max \|difference\| | 1.5 × 10⁻⁸ | 3.0 × 10⁻⁸ | 3.0 × 10⁻⁸ |
| … cells labeled alike | 100% | 99.97% | 100% |
| SingleR at its defaults (every sample or cell, 0.8 quantile): cells labeled alike | 72.0% | 62.5% | 99.3% |
| Cells taking their true type: SituCraft; SingleR, same profiles; SingleR, defaults | | | 98.1%; 98.1%; 98.6% |

  The two Breast cells labeled otherwise are ties SituCraft leaves without a label at a margin of
  0. Checked weekly against SituCraft's labels made again, byte for byte (the atlas's sets where its
  profiles are present).
- **In the app:** the Lung cells mapped by remote control to a table of four classes' profiles over
  15 of the panel's genes, each labeled cell drawn in its label's color and a cell's evidence its
  recorded correlations; a reference sharing no gene with the panel refused (viewer suite); capture
  scene `reference`, with no accessibility violation.

**Found by slice 6:**
- **On a targeted panel the published number of genes per pair covers the panel:** 60 genes for
  each of 36 labels' pairs make every one of 270 to 280 genes compared, and fine-tuning compares
  nearly the same genes again. On the simulated tissue fewer genes per pair label a few more cells
  right (with fine-tuning: 20 per pair 98.9%, 5 per pair 98.9%, 2 per pair 94.9%; the published
  98.1%). The published number is kept, comparable with SingleR's results, and is a parameter.
- **A reference must be of the tissue:** the atlas of primary cells labels the Breast tumor's cells
  embryonic stem cells, neuroepithelial cells and iPS cells, and the Lung tumor's hepatocytes and
  epithelial cells — the nearest profiles it has. The correlations among those labels lie within a
  few hundredths, so the evidence card and the review queue show the doubt; the labels follow the
  reference chosen.
- **One profile per label against every sample:** SingleR's per-sample default parts mostly where
  the reference itself hardly tells labels apart (on Breast, embryonic stem cells, iPS cells,
  neuroepithelial cells and neurons); on the simulated tissue, whose reference cells are of one type
  each, the two agree on 99.3% of the cells and take the true type as often.

### 5.7 Labels across segmentations (verified, wave 4, slice 7)

Cell types are read from cells, so a segmentation's errors can reach them: a cell drawn too wide
takes its neighbor's transcripts, two cells merged become one of two kinds. SituCraft says how much
by reading two label sets, one of each segmentation, through the two segmentations' comparison
(§4; `internal/compare`'s `impact.go`, `api_impact.go`).

**Made again in one step.** A clustering of SituCraft's and its label sets from markers or a
reference are made again on another segmentation with the same parameters (`lib/impact.js`'s
`remakePlan`, `ui/remake.js`): the clustering (its cells by the rule of gene transcripts: a QC
filter's cells are the instrument segmentation's), its map, and each label set (the same marker set
version and parameters, or the same reference file, columns and parameters; a reference that
changed since is said to have). Imported label sets belong to their segmentation's cells and are
not made again. Two label sets made the same way are paired by default, at their first level.

**Read through the comparison.** Labels are compared by name; a cell a label set does not cover
(not analyzed) is left out. SituCraft's choices:
- **Composition:** each label's cells under each segmentation, and its share of the cells covered.
- **Matched cells:** those the comparison pairs one to one (one-to-one, expanded, contracted,
  assignment-only), covered by both: the share labeled alike of those labeled under both, the cells
  labeled under one only, by class, and every pair of labels.
- **Split and merged cells:** for a labeled cell split, whether every piece covered carries its
  label, some do, or none, and whether the pieces carry two labels or more; for a labeled cell into
  which cells were merged, the same of the cells merged; rearrangements by whether the two sides'
  labels are alike, overlap or are apart.
- **Marker mixing by label:** of each label's cells, the share expressing a pair of mutually
  exclusive markers together, and the mutually exclusive co-expression rate among them (Hartman and
  Satija 2024, §4), over the comparison's lineage-marker pairs (on a simulated tissue, each pair of
  two types' markers).
- **Segmentation-sensitive cells:** a covered cell whose covered counterparts carry another label
  (no label counting as one), with what they carry ("Macrophage"; "split into 2 cells: T cell,
  Tumour"; "one of 3 cells merged into a cell labeled Fibroblast"). The review queue (§5.5) takes
  them as a third reason, after the spatially implausible and before the least confident, flagged
  against a label set of another segmentation the dataset's comparisons pair with the reviewed one.
- **The differences whose labels change,** the most transcripts reassigned first, in the comparison
  workspace's Labels tab, which can color each side's cells by its labels.

**Validation:**
- **A built fixture by hand** (`internal/compare/impact_test.go`): ten cells of each, labels named in
  different orders, through each class of difference (cells not covered, a split into two labels,
  a merge of one kind, cells only in either): the composition, the agreement, the transitions, the
  fates, the cells without a counterpart, the differences whose labels change in order, every
  segmentation-sensitive cell's line, and marker mixing by label (each MECR by hand); the queue's
  third reason by hand (`internal/label`).
- **Known truth** (`validation/impact_test.go`, weekly): the standard simulated tissue under its
  truth, the instrument's nucleus expansion and SituCraft's refinement (Balanced), each clustered and
  labeled from its types' markers, read through its comparison with the truth against the truth's
  cells' true types:

| | Truth | Refinement | Instrument |
| --- | --- | --- | --- |
| Cells matched to their true cell (labeled under both) | 3,030 | 2,859 | 2,879 |
| … labeled with its true type | 100% | 99.86% | 98.33% |
| The types' shares against the truth's (total variation distance) | 0 | 0.5% | 2.2% |
| True cells merged into one (of them, joining two types) | | 62 (21) | 8 (2) |
| Marker mixing, MECR of the labeled cells | 0.063 | 0.090 | 0.221 |

  The instrument's and the refinement's labels read against each other: 98.0% of 2,883 matched
  cells alike; 57 and 95 labeled cells change label with the segmentation.
- **In the app** (viewer suite): the Lung clustering and its two label sets made again on the 5 µm
  expansion by remote control; the labels from a table of profiles read through the comparison,
  every matched cell one to one, 99.9% of 10,814 labeled alike, marker mixing unchanged (MECR
  0.0090); the tissue colored by them; the review queue's 40 segmentation-sensitive cells those the
  impact counts. Capture scene `impact`, with no accessibility violation.

**Found by slice 7:**
- **A segmentation's errors reach the labels mostly through cells of two kinds:** on the simulated
  tissue the refinement's labels are truer than the instrument's and its marker mixing lower, near
  the truth's; its remaining errors are true cells merged (62), a third of them of two types.
- **Finer labels are more sensitive:** on the Breast tissue, the instrument's segmentation and a 5
  µm expansion label 96.9% of 4,873 matched cells alike at the class level, 79.7% at the subtype;
  223 cells split by the expansion keep their class in every piece in 66%, and 106 cells into which
  others were merged had carried its class in 63%; marker mixing rises from 0.0466 to 0.0513, most
  among adipocytes (0.0511 to 0.0828). The labels tab opens at the first level.
- **The share of cells co-expressing any exclusive pair saturates** where markers are not absolutely
  exclusive: the simulator expresses each type's markers at a fifth of their base in the other
  types, so 98.9% of the instrument's cells co-express one of 336 pairs. The MECR, a mean over
  pairs, separates the segmentations; the summary leads with it.

### 5.8 Scale (verified, wave 4, slice 8)

A whole Xenium section holds hundreds of thousands of cells, and Xenium Prime's panel 5,000 genes.
Three steps of the clustering and the map cost too much there: exact nearest neighbors cost the
square of the cells (6 s for 50,000 on ten cores, about 6.5 minutes for 400,000), the full
eigendecomposition the cube of the genes (15 s for 2,000, about 4 minutes for 5,000), and the map's
optimization ran on one thread. Below the limits below the methods and their results are as before,
byte for byte (pinned digests; the comparisons with scanpy, leidenalg and umap-learn of §5.1–5.2
hold); above them:

**Nearest neighbors above 50,000 cells: NN-Descent** (Dong W, Moses C, Li K. Efficient k-nearest
neighbor graph construction for generic similarity measures. WWW 2011:577–586,
doi:10.1145/1963405.1963487; `internal/graph`'s `descent.go`): from random neighbors, each round
compares each cell's sampled new and old neighbors and reverse neighbors with one another, and keeps
for every cell the nearest it has met, until a round changes fewer than 0.1% of the table (ρ 1,
δ 0.001, the paper's; at most 30 rounds). SituCraft's choices:
- **The same table for any number of workers and processors:** each round's samples drawn from each
  cell's own stream; the comparisons made a block of 2,048 cells at a time against the table as it
  stands, their improvements then applied by target cell in the order found (a cell keeps the k
  nearest by distance, then index, of all it has met, whatever the order); distances summed in
  order, every product rounded (pinned digest on arm64 and amd64).
- **A wider search:** 3k neighbors (at most 45) searched for k, the k nearest kept. On the
  development bundles' 50 components, searching 14 finds 91.3% (Lung), 92.8% (Breast) and 96.1%
  (simulated) of the exact 14; 28, 99.2%, 99.5% and 99.9%; 42, 99.91%, 99.95% and 99.99%.
- **Its recall measured each time,** against exact search over 1,000 evenly spaced cells,
  recorded in the clustering (`graph.search`) and said in the page; on the bundles it is within
  0.0004 of the recall over every cell.
- **The map's wider neighborhoods** (its precision and recall, 75 nearest) by NN-Descent at a sample
  rate of 0.5, started from the clustering's neighbors: 99.90% (Lung), 99.95% (Breast) and 99.99%
  (simulated) of the exact 75. The trustworthiness stays exact over its 2,000 query cells.

**The map's optimization over every core above 50,000 cells** (`internal/embed`'s
`optimizeBatched`): each epoch's edges due, in order, in batches of 16,384; a batch's pulls and
pushes computed in parallel from the cells' places before it, then applied in the edges' order; the
negative samples drawn from a hash of the seed, the epoch, the edge and the sample (SplitMix64's
finalizer), so that no stream is shared. The same map for any number of workers and processors
(pinned digest). On a graph of 3,000 cells in four groups, its trustworthiness 0.7916 against
0.7937 on one thread (recall 0.346 and 0.349).

**Principal components above 1,000 genes: the Lanczos method** (Lanczos 1950) with full
reorthogonalization (Paige 1972; Golub & Van Loan 2013, §10.1; `internal/linalg`'s `lanczos.go`): the
Krylov space of a seeded start grows, each vector made orthogonal to all before it twice over,
until the 50 largest Ritz pairs' residuals are below 10⁻¹¹ of the largest; the covariance's product
dealt by rows to the workers, each summed in order; the covariance formed in place of its sums (one
matrix of the genes' size, not two). On the standard simulated tissue made with 2,000 genes,
against the full decomposition of the covariance computed independently from the dense scaled
values: the variances within 7.2 × 10⁻¹⁴ of the largest, every direction whose variance stands
apart (15) parallel to twelve digits, the 50 components' subspace the same (overlap 1.000000000);
0.7 s against 15.1 s. With 5,000 genes, as Xenium Prime's: 3.6 s.

**Validation:**
- **Exact cases** (`internal/graph`, `internal/linalg`, `internal/cluster`, `internal/embed`):
  NN-Descent against exact search (recall at least 0.99, every distance true, rows in order), the
  same for one worker and four, started from the exact table and kept; the sampled recall by hand;
  the Lanczos method against the full decomposition on covariances of falling spectra and on a
  matrix of rank 2 (its Krylov space closing and going on); a panel of 1,100 genes' components
  against the full decomposition's, the same for any number of workers; the batched map the same for
  any number of workers and as faithful as one thread's.
- **The development bundles' neighbors searched approximately** (`validation/neighbors_test.go`,
  weekly): NN-Descent finds 99.91% (Lung) and 99.95% (Breast) of the exact neighbors, the recall
  recorded within 0.0004 of it; over five seeds, the clusters Leiden finds on its graph agree with
  the exact graph's (median ARI 0.857 and 0.704) more than Leiden's runs on the exact graph agree
  with one another (0.791 and 0.653), at the same modularity (within 0.002).
- **Large panels** (`validation/panels_test.go`, weekly): the numbers above.
- **The large simulated tissue typed** (`validation/scale_test.go`, weekly): 383,158 of the large
  tissue's cells (`situcraft simulate --preset large`) clustered, mapped and labeled in 186 s on ten
  cores at a peak of 3.9 GB, the instrument's segmentation's files made on first use within it (the
  raster a band of rows at a time, the mask a row of its chunks at a time). The neighbors by
  NN-Descent in 34 s, 95.6% of the exact over 1,000 cells in nine rounds: lower than on the bundles'
  components (99.9%), the simulated types' cells being alike within a type, as noise is, where which
  cells are nearest is least determined; 9 clusters. The map laid out in 33 s, its trustworthiness
  0.861 and its wider neighborhoods 97.1% of the exact. Labeled from the simulated types' markers:
  378,743 cells, the types' shares within a total variation distance of 0.015 of the true cells'.
  The map of every cell shown in headless Chrome in 20 s and colored again in 40 ms, SituCraft
  serving it in 1.9 GB.

## 6. Spatial analysis (wave 6) and cohorts (wave 7)

- **Graphs:**
  - radius and kNN;
  - Delaunay triangulation (Lee & Schachter 1980, *Int J Comput Inf Sci* 9:219–242; Bowyer
    1981; Watson 1981, *Comput J* 24);
  - contact graphs from shared polygon boundaries.
- **Neighborhood enrichment** by label permutation z-scores (Schapiro et al. 2017, histoCAT,
  *Nat Methods* 14:873–876, doi:10.1038/nmeth.4391; Palla et al. 2022, Squidpy, *Nat Methods*
  19:171–178, doi:10.1038/s41592-021-01358-2).
- **Co-occurrence ratios** across distances (Palla et al. 2022). **Ripley's K/L** (Ripley 1977,
  *J R Stat Soc B* 39:172–212).
- **Spatial autocorrelation** of genes by **Moran's I** (Moran 1950, *Biometrika* 37:17–23).
- **Cellular neighborhoods** by clustering neighborhood composition (Goltsev et al. 2018, *Cell*
  174:968–981; Schürch et al. 2020, *Cell* 182:1341–1359), using k-means with k-means++ seeding
  (Arthur & Vassilvitskii 2007).
- **Infiltration and exclusion:** distances to region boundaries and compartment composition.
- **Cohort inference** at the sample level: pseudobulk aggregation (Crowell et al. 2020, *Nat
  Commun* 11:6077), motivated by the pseudoreplication problem (Squair et al. 2021, *Nat Commun*
  12:5692).
- **Multiple testing:** Benjamini–Hochberg (1995, *J R Stat Soc B* 57:289–300).
- **Moderated statistics:** where used, from MaveScape's `limma.js` (Smyth 2004, *Stat Appl Genet
  Mol Biol* 3:3).

## 7. Datasets

| Dataset | Source | Size | License | Use |
| --- | --- | --- | --- | --- |
| Xenium V1 human Breast 2-FOV (XOA 2.0, multimodal segmentation) | 10x Genomics example data: `cf.10xgenomics.com/samples/xenium/2.0.0/Xenium_V1_human_Breast_2fov/Xenium_V1_human_Breast_2fov_outs.zip` (md5 4632914eca973a1d532231ea646e10cc) | 379 MB | CC BY 4.0 | Primary development data |
| Xenium V1 human Lung 2-FOV (XOA 2.0, nucleus expansion 5 µm) | `cf.10xgenomics.com/samples/xenium/2.0.0/Xenium_V1_human_Lung_2fov/Xenium_V1_human_Lung_2fov_outs.zip` (md5 bf9c5e6762681b81eab0a19d3d590381) | 278 MB | CC BY 4.0 | CI; nucleus-expansion validation |
| Simulated tissues | `situcraft simulate` (seeded) | generated | — | Ground truth; scale |
| Xenium FFPE Human Breast Cancer, replicate 1 (XOA 1.0.1; Janesick et al. 2023), with its post-Xenium H&E and IF images | 10x Genomics: ten members of `cf.10xgenomics.com/samples/xenium/1.0.1/Xenium_FFPE_Human_Breast_Cancer_Rep1/Xenium_FFPE_Human_Breast_Cancer_Rep1_outs.zip` (9.9 GB) taken by range, and `…_he_image.ome.tif`, `…_if_image.ome.tif` and their `…_imagealignment.csv` | 2.85 GB | CC BY 4.0 | The pathology fixture (wave 5): XOA 1.0, post-Xenium images, alignment, registration |

Notes:
- The two 2-FOV bundles are also SituCraft's example datasets (wave 1, slice 7), downloaded from
  the same URLs when a user asks.
- The 10x CDN once answered HTTP 403 to requests without a browser-like User-Agent; in October
  2026 it accepts any (verified with none, Go's, curl's and SituCraft's). The fetcher and SituCraft
  name themselves in theirs.
- Attribution, as CC BY 4.0 requires, appears in the README, the example's card, the methods
  paragraph's references, and any figure made from the data.
- **The tours' genes** (`web/lib/tours.js`), among the panel genes with the most Q20 transcripts
  in each section: Lung: EPCAM (23,071), COL5A2 (6,371), PLVAP (3,472), CD68 (790), VWF (1,326),
  RGS5 (1,888), PDGFRB (1,240). Breast: KRT7 (14,545), ACTA2 (13,072), LUM (20,388), KRT14 (654),
  PTPRC (1,729), CD68 (5,253), PECAM1 (1,583), MKI67 (7,823). The Lung panel's alveolar and B-cell
  markers are scarce in this section (AGER 102, SFTPD 142, MS4A1 29), and PTPRC, ACTA2, PECAM1
  and COL1A1 are not in it, so the tour does not use them.

## 8. Other references

- 10x Genomics. Xenium Onboard Analysis documentation: output overview; understanding outputs;
  example data. Xenium Ranger: import segmentation. Xenium Explorer: image file conversion.
- Marco Salas S, et al. Optimizing Xenium In Situ data utility by quality assessment and
  best-practice analysis workflows. *Nat Methods* 2025. doi:10.1038/s41592-025-02617-2.
- Petukhov V, et al. Cell segmentation in imaging-based spatial transcriptomics. *Nat Biotechnol*
  2022;40:345–354. doi:10.1038/s41587-021-01044-w.
- Wolf FA, Angerer P, Theis FJ. SCANPY. *Genome Biol* 2018;19:15.
- Kuglin CD, Hines DC. The phase correlation image alignment method. *Proc IEEE Conf Cybernetics
  and Society* 1975 (registration, wave 5).
- Viola P, Wells WM. Alignment by maximization of mutual information. *Int J Comput Vis*
  1997;24:137–154.
- Bookstein FL. Principal warps: thin-plate splines and the decomposition of deformations.
  *IEEE TPAMI* 1989;11:567–585.
- Horn BKP. Closed-form solution of absolute orientation using unit quaternions. *J Opt Soc Am A*
  1987;4:629–642 (Xenium Explorer's keypoint fit).
- Janesick A, Shelansky R, Gottscho AD, et al. High resolution mapping of the tumor
  microenvironment using integrated single-cell, spatial and in situ analysis. *Nat Commun*
  2023;14:8353. doi:10.1038/s41467-023-43458-x (the pathology fixture).
- Umeyama S. Least-squares estimation of transformation parameters between two point patterns.
  *IEEE TPAMI* 1991;13:376–380.
- Reddy BS, Chatterji BN. An FFT-based technique for translation, rotation, and scale-invariant
  image registration. *IEEE Trans Image Process* 1996;5(8):1266–1271.
- Studholme C, Hill DLG, Hawkes DJ. An overlap invariant entropy measure of 3D medical image
  alignment. *Pattern Recognit* 1999;32(1):71–86.
- Foroosh H, Zerubia J, Berthod M. Extension of phase correlation to subpixel registration. *IEEE
  Trans Image Process* 2002;11(3):188–200.
- Rueckert D, Sonoda LI, Hayes C, Hill DLG, Leach MO, Hawkes DJ. Nonrigid registration using
  free-form deformations: application to breast MR images. *IEEE Trans Med Imaging*
  1999;18(8):712–721.
- Hooke R, Jeeves TA. "Direct search" solution of numerical and statistical problems. *J ACM*
  1961;8(2):212–229.
- Stone M. Cross-validatory choice and assessment of statistical predictions. *J R Stat Soc B*
  1974;36(2):111–147 (leave-one-out errors of landmark fits).
- Ruifrok AC, Johnston DA. Quantification of histochemical staining by color deconvolution.
  *Anal Quant Cytol Histol* 2001;23(4):291–299 (the stains' colours of the simulated H&E).
- Douglas DH, Peucker TK. Algorithms for the reduction of the number of points required to
  represent a digitized line or its caricature. *Cartographica* 1973;10(2):112–122.
  doi:10.3138/FM57-6770-U75U-7727 (polygon simplification in the geometry tiles and segmentation
  outlines).
- Felzenszwalb PF, Huttenlocher DP. Distance transforms of sampled functions. *Theory Comput*
  2012;8:415–428. doi:10.4086/toc.2012.v008a019 (nucleus expansion's exact distances).
- Okabe A, Boots B, Sugihara K, Chiu SN. *Spatial Tessellations: Concepts and Applications of
  Voronoi Diagrams*, 2nd ed. Wiley, 2000 (nucleus expansion).
- Marconato L, Palla G, Yamauchi KA, et al. SpatialData: an open and universal data framework
  for spatial omics. *Nat Methods* 2025;22:58–62. doi:10.1038/s41592-024-02212-x (imports and
  exports).
- Virshup I, Rybakov S, Theis FJ, Angerer P, Wolf FA. anndata: Access and store annotated data
  matrices. *J Open Source Softw* 2024;9(101):4371. doi:10.21105/joss.04371 (tables; the AnnData
  export).
- Okabe M, Ito K. Color Universal Design (CUD): how to make figures and presentations that are
  friendly to colorblind people. J*FLY, 2008 (the comparison workspace's two overlay colours).
- Hartman A, Satija R. Comparative analysis of multiplexed in situ gene expression profiling
  technologies. bioRxiv 2024. doi:10.1101/2024.01.11.575135 (the mutually exclusive
  co-expression rate).
- Butler H, Daly M, Doyle A, Gillies S, Hagen S, Schaub T. The GeoJSON Format. RFC 7946, 2016;
  OGC Simple Features (well-known binary); GeoParquet 1.0 (imports and exports).
- Boisvert RF, Pozo R, Remington KA. The Matrix Market exchange formats: initial design. NIST
  Interagency Report 5935, 1996 (the Matrix Market export).
- Kern R. A simple file format for NumPy arrays. NumPy Enhancement Proposal 1, 2007 (the `.npy`
  masks for Xenium Ranger).
- Apache Parquet format specification (parquet-format: file layout, `parquet.thrift`,
  Encodings.md) and the Apache Thrift compact protocol specification (the Parquet reader and
  writer); the Zarr storage specifications, formats 2 and 3 (the Zarr reader and writer).
- Hyndman RJ, Fan Y. Sample quantiles in statistical packages. *Am Stat* 1996;50:361–365
  (the QC plots' quantiles, type 7).
- Tukey JW. *Exploratory Data Analysis*. Addison-Wesley, 1977 (box plots, whiskers at 1.5
  interquartile ranges).
- Deutsch P, Gailly J-L. ZLIB compressed data format specification version 3.3. RFC 1950, 1996;
  Deutsch P. DEFLATE compressed data format specification version 1.3. RFC 1951, 1996 (the
  cache's blocks, through Go's standard library; the simulated morphology images).
- Zhou M, Carin L. Negative binomial process count and mixture modeling. *IEEE TPAMI*
  2015;37:307–320. doi:10.1109/TPAMI.2013.211. van Dyk DA, Park T. Partially collapsed Gibbs
  samplers: theory and methods. *JASA* 2008;103:790–796. doi:10.1198/016214508000000409. Geweke J.
  Getting it right: joint distribution tests of posterior simulators. *JASA* 2004;99:799–804.
  doi:10.1198/016214504000001132. Murray I, Ghahramani Z, MacKay DJC. MCMC for
  doubly-intractable distributions. *UAI* 2006 (the transcript-informed segmentation's model,
  §3.2).
- Geweke J. Evaluating the accuracy of sampling-based approaches to the calculation of posterior
  moments. In: Bernardo JM, Berger JO, Dawid AP, Smith AFM, eds. *Bayesian Statistics 4*. Oxford
  University Press, 1992:169–193 (the transcript-informed segmentation's convergence diagnostic).
- Kong TY, Rosenfeld A. Digital topology: introduction and survey. *Comput Vis Graph Image
  Process* 1989;48:357–393 (the sampler's connectivity test). Harary F, Harborth H. Extremal
  animals. *J Combin Inform System Sci* 1976;1:1–8 (the least perimeter of n voxels, the
  sampler's perimeter bound).
- Feder J. Random sequential adsorption. *J Theor Biol* 1980;87:237–254.
  doi:10.1016/0022-5193(80)90358-6 (placing simulated cells).
- Borgefors G. Distance transformations in digital images. *Comput Vis Graph Image Process*
  1986;34:344–371. doi:10.1016/S0734-189X(86)80047-0 (the simulated instrument's expansion).
- Wirgin A. The inverse crime. arXiv:math-ph/0401050, 2004 (why the simulator's model differs
  from the segmentation's).
- O'Neill ME. PCG: a family of simple fast space-efficient statistically good algorithms for
  random number generation. Harvey Mudd College technical report HMC-CS-2014-0905, 2014 (the
  generator under `internal/random`, through Go's math/rand/v2).
- Marsaglia G, Bray TA. A convenient method for generating normal variables. *SIAM Rev*
  1964;6:260–264. Marsaglia G, Tsang WW. A simple method for generating gamma variables.
  *ACM Trans Math Softw* 2000;26:363–372. Knuth DE. *The Art of Computer Programming*, vol. 2,
  3rd ed., §3.4.1. Addison-Wesley, 1997 (normal, gamma and Poisson draws). Devroye L.
  *Non-Uniform Random Variate Generation*. Springer, 1986, ch. X.4 (binomial draws, by splitting
  a large count at a beta-distributed order statistic).
- The HDF Group. HDF5 File Format Specification, version 3.0. 10x Genomics. Feature-barcode
  matrices (HDF5 format), Cell Ranger and Xenium documentation (the simulated bundles'
  `cell_feature_matrix.h5`). Jenkins B. lookup3.c, 2006, public domain (HDF5's metadata
  checksums).
- Sun Microsystems. fdlibm 5.3, 1993: the logarithm and exponential `internal/dmath` and
  `web/lib/dmath.js` compute, the same in Go and JavaScript on every processor, and the
  complementary error function `internal/dmath` computes (the markers' p-values).
- Wilkinson JH, Reinsch C. *Handbook for Automatic Computation*, vol. II: *Linear Algebra*.
  Springer, 1971 (procedures tred2 and tql2). Golub GH, Van Loan CF. *Matrix Computations*, 4th
  ed. Johns Hopkins University Press, 2013, §8.3 (the principal components' eigenvectors,
  `internal/linalg`).
- Reichardt J, Bornholdt S. Statistical mechanics of community detection. *Phys Rev E*
  2006;74:016110 (modularity with a resolution).
- Mann HB, Whitney DR. On a test of whether one of two random variables is stochastically larger
  than the other. *Ann Math Stat* 1947;18:50–60 (the clusters' markers).
- Hubert L, Arabie P. Comparing partitions. *J Classif* 1985;2:193–218 (the adjusted Rand index).
- Belkin M, Niyogi P. Laplacian eigenmaps for dimensionality reduction and data representation.
  *Neural Comput* 2003;15:1373–1396. Zhou Y, Saad Y. A Chebyshev–Davidson algorithm for large
  symmetric eigenproblems. *SIAM J Matrix Anal Appl* 2007;29:954–971 (the map's spectral start).
- Venna J, Kaski S. Neighborhood preservation in nonlinear projection methods: an experimental
  study. *ICANN* 2001:485–491. Venna J, Peltonen J, Nybo K, Aidos H, Kaski S. Information retrieval
  perspective to nonlinear dimensionality reduction for data visualization. *J Mach Learn Res*
  2010;11:451–490 (the map's quality).
- Abramowitz M, Stegun IA. *Handbook of Mathematical Functions*. National Bureau of Standards,
  1964, 6.5.29 and 6.5.31 (the incomplete gamma function), 26.5.8 (the incomplete beta function). Stephens MA. Use of the
  Kolmogorov–Smirnov, Cramér–von Mises and related statistics without extensive tables. *J R
  Stat Soc B* 1970;32:115–122 (the goodness-of-fit tests of `internal/stats`).

## 9. To verify before relying on it

- **The transcript-informed segmentation's ambient RNA** (§3.2, the background): inside cells it
  cannot be told from the cells' own expression, so the model estimates it at a quarter to twice
  the truth on simulated tissues and leaves most of it counted in cells; even exact rates would
  tell about a sixth of the background apart. What might tell more: a prior on cells' shapes that
  leaves the gaps between cells to the background, or expression states in which a gene can be
  absent altogether (the simulator's cell types express every gene, so it would need tissues that
  do not). Each would be judged on the background benchmark, and the real bundles' background
  measured where cells are far apart.

- The metrics left empty in the bundles in hand (polygon counts, nuclei without cells), when a
  bundle fills them: whether "non-simple polygon" means what SituCraft's check (and GEOS) means.
- Control-codeword prefixes across XOA versions (1.x, 2.x, 3.x, Prime 5K).
- XOA 3.x and 4.x bundle differences (file names, Zarr versions, new files).
- Xenium Explorer's keypoints file on a real file (the fixture's keypoints are not published): its
  coordinates' units (read as pixels, as the matrix's), and that its fit gives the matrix saved
  beside it.
- JPEG 2000 options of XOA versions other than 2.0 (the bundles in hand were written by OpenJPEG
  2.5.1; other versions may use other progressions or code-block styles, which the decoder reads
  but which have not been seen on real Xenium files).
