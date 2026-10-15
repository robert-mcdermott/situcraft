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

## 2. Coordinates and units

- Transcript and polygon coordinates are in **microns**. Image pixels are related to them by
  `pixel_size` from `experiment.xenium` (0.2125 µm at full resolution) and, for masks, by
  `masks/homogeneous_transform`.
- Coordinate systems are named and explicit in SituCraft (`global` in µm; each image's pixel
  system; each registered image's system). SituCraft never infers pixels versus microns from
  values.
- Post-Xenium images come with a 3×3 alignment matrix (CSV) mapping image pixels to Xenium pixels,
  per 10x's documentation of the H&E alignment file and Xenium Ranger's image inputs (to verify
  exact orientation conventions on a real file in wave 5).

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
background, $\sigma(v) = 0$. Cell $c$'s volume is $V_c = \nu \, |\{v : \sigma(v) = c\}|$. Sampling
starts coarse (4 µm in x and y, one z layer) and doubles the resolution twice, to 1 µm and four
layers; doubling splits voxels without changing which cell owns a region (slice 3).

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

**Expression.** Each cell belongs to one of $K$ components (broad expression states),
$z_c \in \{1, \dots, K\}$, with probabilities $\pi \sim \mathrm{Dirichlet}(\alpha, \dots, \alpha)$.
Given its component, a cell's rates are gamma (shape $r$, rate $\beta$):

$$\lambda_{cg} \mid z_c = k \sim \mathrm{Gamma}(r_{kg}, \beta_{kg}), \quad r_{kg} \sim \mathrm{Gamma}(a_r, b_r), \quad \beta_{kg} \sim \mathrm{Gamma}(a_\beta, \phi_g), \quad \phi_g \sim \mathrm{Gamma}(e_\phi, f_\phi), \quad \lambda_{0g} \sim \mathrm{Gamma}(a_0, b_0).$$

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
2. **Background:** $\lambda_{0g} \sim \mathrm{Gamma}(a_0 + n_{0g},\; b_0 + |R|)$.
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
| $a_0, b_0$ | 1, 1 µm³ | Weak: the region is of the order of $10^5$–$10^7$ µm³ |
| $m_0$ | $\log$ of the median initial cell volume | Centres the volume prior on the cells the sampler starts from |
| $\kappa_0, a_s, b_s$ | 1, 2, 0.25 | A prior mean of 0.25 for $s_k^2$: volumes within a factor of about 1.6 |
| $\rho$ | 0.8 | The publication's |

**Validation** (`internal/segment/refine`; fixed seeds, each test at the 0.001 level):
- **Each conjugate update against its analytic posterior** (Kolmogorov–Smirnov, 6,000 draws):
  the background's, the cells' and the components' rates, the gene scales, the proportions (each
  marginally beta), and the volume prior (its variance inverse gamma, its mean normal given it).
- **The components** against probabilities computed independently from the negative binomial
  and log-normal densities (χ²).
- **The shapes:** 3,000 independent chains of the augmented update, started from the prior, end
  distributed as the posterior computed by numerical integration.
- **The whole sweep** by Geweke's joint distribution test (Geweke J. Getting it right: joint
  distribution tests of posterior simulators. *J Am Stat Assoc* 2004;99:799–804.
  doi:10.1198/016214504000001132). 4,000 chains start from the prior and data drawn from the
  model, then alternate 40 sweeps with fresh data. Eleven quantities (shapes, rate parameters,
  scales, rates, background, proportions, volume parameters, component sizes) stay distributed as
  the prior (two-sample Kolmogorov–Smirnov).
- **Moves:** in a random walk of 2,000 voxel moves, each move's computed change equals the target
  recomputed afresh (to $10^{-9}$ relative); a move that would empty a cell is refused.
- **Attributions:** each in proportion to the rates (z test); the counts follow them.
- **Behaviour:** on counts of 300 cells of three types, the components separate the types
  (purity 0.95 or more).
- **Determinism:** the same parameters on 1 and 8 workers. A pinned checksum of twenty sweeps is
  the same on arm64 and amd64.
- **Power.** Each test was shown to fail on a deliberate error. Nine errors were tried:
  - a conjugate posterior's parameter wrong (background, shapes, rates, rate parameters, gene
    scales, volumes);
  - the volume prior dropped from the components;
  - the CRT off by one;
  - the rates drawn before the shapes, which breaks the partially collapsed order (caught only by
    the joint test, once its chains were long enough for the error to accumulate).

  Four more were tried on the moves and attributions (a sign, the nuclear prior reversed, a
  volume off by one, the attribution's share). Every one was caught.

A sweep of the parameters at the standard simulation's size (3,000 cells, 120 genes, 10
components) takes about 9 ms on ten cores; its cost grows with cells × genes × components.

#### The sampler (wave 3, slice 3; as planned)

- **Morphology.** A hard constraint, not an energy: each cell's perimeter in each z layer is
  bounded relative to the perimeter of a circle of the same area (factor 1.3 by default).
  Proposals that violate it are rejected.
- **Connectivity.** Proposals that would change local connected components are rejected (an
  articulation check in the Moore neighborhood). Cells are never annihilated. Background
  "bubble" proposals (probability 0.05) let gaps form and close between cells.
- **Sampler.** Metropolis–Hastings. A proposal copies a voxel's label to a neighbor with a
  different label (von Neumann neighborhood), with the proposal-ratio correction. Parallel
  updates use a checkerboard of blocks updated in four phases.
- **Repositioning.** Each transcript's true position has a two-component normal mixture prior
  around its observed position: a wide component (σ = 4 µm, weight 0.2) for leaked or diffused
  molecules and a narrow one (σ = 0.5 µm). It is sampled jointly with the boundaries. A setting
  disables diffusion, lowering memory use.

SituCraft's choices where the publication leaves them open:
- **Determinism.** Each block has its own seeded random stream and blocks are updated in a fixed
  order, so a seed gives the same result on any number of threads.
- **Polygons** come from voxel labels by boundary tracing per z layer. A consensus 2D polygon
  assigns each x/y position to its dominant cell.
- **Convergence.** Diagnostics are reported: assignment changes per sweep, log-likelihood trace,
  and agreement between two seeds on a pilot region.
- **Large tissues** run as overlapping patches that are stitched. Patch-based processing of large
  sections follows the approach of Blampey et al. (2024), *Nat Commun* 15:4981,
  doi:10.1038/s41467-024-48981-z.

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
- **Background.** In 50 µm tiles:
  - each gene's count is Poisson (6 per 100 µm², spread over the genes in proportion to the
    tissue's mean expression);
  - each negative control probe's and codeword's is Poisson (0.05 per 100 µm²);
  - background molecules lie uniformly in the tile and the section's depth.
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
- `transcripts.parquet` with ids, instrument cells, nucleus overlap, fields of view and quality.
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
- **Background.** Per-tile totals are Poisson (χ²; catches a density 3% off), genes are in
  proportion to the tissue's mean expression, and places are uniform.
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
- **Embedding and graph:** principal components (exact, by a Lanczos or randomized SVD; Halko,
  Martinsson & Tropp 2011, *SIAM Rev* 53:217–288); a kNN graph.
- **Clustering:** Leiden (Traag, Waltman & van Eck 2019, *Sci Rep* 9:5233,
  doi:10.1038/s41598-019-41695-z).
- **Display:** UMAP (McInnes, Healy & Melville 2018, arXiv:1802.03426).
- **Marker scores:** gene-set scores against control gene sets binned by expression (Tirosh et al.
  2016, *Science* 352:189–196, doi:10.1126/science.aad0501).
- **Reference mapping:** correlation-based mapping to a labeled single-cell reference with
  iterative fine-tuning (Aran et al. 2019, *Nat Immunol* 20:163–172,
  doi:10.1038/s41590-018-0276-y).
- Benchmarks on Xenium show performance depends on the dataset (Cheng et al. 2025), so no method
  is the only default.
- **Cluster markers:** Wilcoxon rank-sum, labeled exploratory (cells are not replicates).

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
| A bundle with post-Xenium H&E and alignment (≤ 3 GB) | chosen in wave 5 planning (candidate: 10x human lymph node preview data, CC BY 4.0 (to verify size and files)) | | | Registration and pathology |

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
- Umeyama S. Least-squares estimation of transformation parameters between two point patterns.
  *IEEE TPAMI* 1991;13:376–380.
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
  3rd ed., §3.4.1. Addison-Wesley, 1997 (normal, gamma and Poisson draws).
- The HDF Group. HDF5 File Format Specification, version 3.0. 10x Genomics. Feature-barcode
  matrices (HDF5 format), Cell Ranger and Xenium documentation (the simulated bundles'
  `cell_feature_matrix.h5`). Jenkins B. lookup3.c, 2006, public domain (HDF5's metadata
  checksums).
- Sun Microsystems. fdlibm 5.3, 1993: the logarithm and exponential `internal/dmath` and
  `web/lib/dmath.js` compute, the same in Go and JavaScript on every processor.
- Abramowitz M, Stegun IA. *Handbook of Mathematical Functions*. National Bureau of Standards,
  1964, 6.5.29 and 6.5.31 (the incomplete gamma function), 26.5.8 (the incomplete beta function). Stephens MA. Use of the
  Kolmogorov–Smirnov, Cramér–von Mises and related statistics without extensive tables. *J R
  Stat Soc B* 1970;32:115–122 (the goodness-of-fit tests of `internal/stats`).

## 9. To verify before relying on it

- The metrics left empty in the bundles in hand (polygon counts, nuclei without cells), when a
  bundle fills them: whether "non-simple polygon" means what SituCraft's check (and GEOS) means.
- Control-codeword prefixes across XOA versions (1.x, 2.x, 3.x, Prime 5K).
- XOA 3.x and 4.x bundle differences (file names, Zarr versions, new files).
- The H&E alignment matrix orientation on a real file.
- JPEG 2000 options of XOA versions other than 2.0 (the bundles in hand were written by OpenJPEG
  2.5.1; other versions may use other progressions or code-block styles, which the decoder reads
  but which have not been seen on real Xenium files).
