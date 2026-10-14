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
  **Zstd** (clevel 1, byte shuffle) for label masks.
- Dtypes: `<u4`, `<i4`, `<f4`, `<f8`; C order.
- `cells.zarr.zip` (format "CellSegmentationDataset" 6.2) holds:
  - `cell_id` (uint32 × 2: a numeric prefix and dataset suffix, which together encode the
    string cell ID);
  - `polygon_sets/{0,1}` (nucleus, cell): `vertices` (float32, up to 25 vertex pairs per cell),
    `num_vertices`, `cell_index` and `method`;
  - `masks/{0,1}` (uint32 label images at image resolution) and `masks/homogeneous_transform`
    (4×4);
  - `cell_summary` (centroids, areas, z level, nucleus count).
- The string form of cell IDs (e.g. `aiaafdpo-1`) and its relation to the uint32 pair follow 10x's
  documentation of cell IDs (to verify against the documentation before relying on it).

### 1.4 Images (verified)

- BigTIFF; uint16, one sample per pixel; tiles of 1024×1024.
- Pyramid levels are stored in **SubIFDs** (four per image in the 2-FOV files).
- OME-XML is in the first IFD's ImageDescription.
- Compression code **34712, JPEG 2000** (codestreams per tile).
- 10x's image-conversion guide recommends ZLIB or lossless JPEG 2000 compression, 1024-pixel tiles
  and a downsampling factor of 2 for images that Xenium Explorer opens. Post-Xenium H&E images
  supplied by users therefore arrive with other compressions, which SituCraft also needs: JPEG
  (Go's `image/jpeg`), deflate/ZLIB and LZW.

### 1.5 Specifications SituCraft implements

| Format | Specification |
| --- | --- |
| Zstandard | RFC 8878, *Zstandard Compression and the application/zstd Media Type* (Collet & Kucherawy 2021) |
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

The decoder follows T.800 in this order: codestream markers (SIZ, COD, COC, QCD, QCC, SOT/SOD),
tag trees and packet headers (tier 2), EBCOT block decoding with the MQ arithmetic decoder (tier
1, including the bypass and termination modes), dequantization, the inverse 5/3 reversible
wavelet (lossless) and 9/7 irreversible wavelet, and the inverse component transform when
present. SituCraft's choices:
- Decode whole 1024×1024 tiles once into the tile cache rather than decoding on demand.
- Run code blocks in parallel.
- Implement only the Part 1 features that real files use, refusing anything else with a clear
  message.

Correctness is checked pixel for pixel against an independent decoder on every tile of both
development bundles.

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

### 3.1 Nucleus expansion

Each nucleus polygon is grown by up to *d* µm (Xenium's default 5 µm), and no point is assigned to
more than one cell: a point belongs to the nearest nucleus within *d*. That is a Voronoi
partition of the plane by nuclei, clipped at distance *d* (Okabe et al., *Spatial Tessellations*,
2nd ed., Wiley 2000). Transcripts are assigned by point in polygon (or by label lookup in a
raster at a resolution chosen to bound the error). Validation: the Lung 2-FOV was segmented this
way by XOA 2.0 ("Segmented by nucleus expansion of 5.0 µm"), so SituCraft's expansion is checked
against its cells, areas and assignments.

### 3.2 Transcript-informed segmentation (after Jones et al. 2025)

Jones DC, Elz AE, Hadadianpour A, Ryu H, Glass DR, Newell EW. Cell simulation as cell
segmentation. *Nat Methods* 2025;22(6):1331–1342. doi:10.1038/s41592-025-02697-0.

The model, as SituCraft implements it (notation is SituCraft's; equations are written out in full
when wave 3 is planned):

- **Space.** A 3D lattice of voxels. Each voxel is either unassigned (background) or assigned to
  exactly one cell. Sampling starts coarse (4 µm in x/y, one z layer) and doubles resolution twice
  (to 1 µm, 4 z layers). Doubling splits voxels without changing which cell owns each region.
- **Transcripts.** Observed molecules are a Poisson point process. Each gene has a constant
  background rate over the region. Each cell has a constant per-gene rate over its volume.
- **Expression.** Cell rates follow a hierarchical gamma mixture with *k* components (broad
  expression states). With per-gene, per-component gamma parameters, counts are marginally
  negative binomial. Conditional updates use conjugacy and data augmentation:
  - Chinese-restaurant-table augmentation for the dispersion (Zhou & Carin 2015, *IEEE TPAMI*
    37:307–320, doi:10.1109/TPAMI.2013.211);
  - Pólya–Gamma augmentation for the logistic parameters (Polson, Scott & Windle 2013, *JASA*
    108:1339–1349, doi:10.1080/01621459.2013.829001).
- **Volume.** Log-normal cell volume per component.
- **Prior segmentation.** Sampling is initialized from nuclei. A nuclear prior penalizes moving a
  transcript away from its initial nucleus (probability 0.8 that the initial assignment is
  correct, in the publication).
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

### 3.3 Importing other segmentations

| Source | Read |
| --- | --- |
| SpatialData Zarr (written by several segmentation tools) | shapes (cell boundaries), points (transcripts with assignment), table (AnnData counts) |
| Baysor-style transcript assignment CSV + cell polygon GeoJSON | assignment, polygons |
| Xenium Ranger outputs (`import-segmentation`, `resegment`) | a new bundle, read as a bundle |
| GeoJSON polygons + a transcript-to-cell table | polygons, assignment |

## 4. Comparing segmentations

- **Cell correspondence:** by shared nucleus where both derive from the same nuclei; otherwise by
  polygon overlap (intersection over union on a raster at 0.25 µm, SituCraft's choice, which keeps
  non-convex polygons exact enough and fast) and by transcript-assignment overlap (Jaccard). Pairs
  are classified as one-to-one, expanded/contracted, split, merge, unmatched, or assignment-only.
- **Spurious co-expression** (Jones et al. 2025): gene pairs whose conditional co-expression rises
  at least 1.5-fold under a broader segmentation than under nuclei alone are flagged. The score is
  normalized to nuclear segmentation. Cells are down-sampled to a fixed transcript count (50,
  multinomial) so that discarding transcripts is not rewarded.
- **Mutually exclusive co-expression rate (MECR)** for gene pairs known not to co-occur (Hartman &
  Satija 2024, bioRxiv, doi:10.1101/2024.04.03.586404 (to verify the identifier)).
- **Also reported:**
  - cell and nucleus counts; assigned/unassigned fractions; transcripts per cell; areas;
  - geometry validity;
  - changed-assignment fraction; IoU and boundary displacement on matched cells;
  - cell-type composition changes (after wave 4);
  - runtime, peak memory and output size.

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
- The 10x CDN answers HTTP 403 to requests without a browser-like User-Agent (verified); the
  fetcher sends one.
- Attribution, as CC BY 4.0 requires, appears in the README, the example's card, and any figure
  made from the data.

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

## 9. To verify before relying on it

- The cell-ID string encoding against 10x's documentation.
- Control-codeword prefixes across XOA versions (1.x, 2.x, 3.x, Prime 5K).
- XOA 3.x and 4.x bundle differences (file names, Zarr versions, new files).
- The H&E alignment matrix orientation on a real file.
- The MECR reference's identifier.
- That every JPEG 2000 feature used by real Xenium images is covered (the codestream options of
  the files in hand: progression order, code-block size, modes, number of levels).
