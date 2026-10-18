# Changelog

## Unreleased (0.6.0)

Wave 6 of the [roadmap](situcraft-spec/roadmap.md): niches and spatial statistics. How the tissue is
organized, asked of a segmentation's labeled cells: which cell types are neighbors, how far apart
they lie, which enter a region, what niches the tissue has and which genes follow space, each result
with the cells it comes from.

### Added

- **Cell graphs (wave 6, slice 1).** The **Spatial** view builds a graph of which of a
  segmentation's cells are neighbors, the first step of every spatial question.
  - **Four definitions:** the Delaunay triangulation of the cells' centers, its edges up to a
    longest length (50 µm by default); each cell's k nearest (6), an edge when either cell is among
    the other's k; the cells within a radius (20 µm); and contact, cells whose outlines touch in the
    segmentation (or lie within a gap), with the boundary they share.
  - **The cells:** all of a segmentation's, a QC filter's or those in regions; cells a review
    excluded from analyses are always left out, and the graph records how many.
  - **Exact:** the triangulation by exact geometric predicates, the same for any points, lattices
    whose cells lie four on a circle included; every graph the same on any number of threads and on
    any processor.
  - **An artifact** of kind `graph`, named by what it is made from (asking again answers it): its
    cells and edges with their lengths and shared boundaries, and what it holds: the neighbors a
    cell, cells without one, its pieces, its edges' lengths, with histograms.
  - **On the tissue:** the Viewer's **Graph** draws the edges over the cells, close up; a cell's
    inspector lists its neighbors with their distances and labels. Saved views keep the graph. The
    project tree lists **Graphs**.
  - **Remote control:** `build_graph`; `show_tissue` takes `graph`.
  - **Validated:** lattices worked by hand (each kind's degrees and lengths); the triangulation's
    empty circumcircles and Euler's count of triangles on random, cocircular, collinear and
    repeated points; radius and k-nearest graphs against an exhaustive search; contact on label
    rasters drawn by hand; on both development bundles and the standard simulated tissue, the same
    edges as SciPy 1.18 (Delaunay by Qhull, a k-d tree) and Squidpy 1.8 (`spatial_neighbors`),
    edge for edge, lengths identical; in the page, a radius graph's edges those of an exhaustive
    search, a region's graph over its cells, the edges drawn where they lie.
- **Which cell types are neighbors (wave 6, slice 2).** The Spatial view's **Questions** ask a
  label set's cells (any set, any level) how they are arranged; each answer is an artifact of kind
  `spatial`, named by what it is asked of.
  - **Neighborhood enrichment** over a graph: for each pair of labels, the neighbors between their
    cells against the number expected if the labels were dealt to the same cells at random, the
    expectation exact; z-scores and two-sided p-values from seeded permutations (1,000 by default),
    adjusted by Benjamini and Hochberg's procedure over the pairs that can be tested. Optionally
    dealt within regions, so that each keeps its composition; in a contact graph, counted by the
    boundary shared, with the contact fractions. Shown as a heat map of the ratio (red more, blue
    fewer, the significant marked), tables of the pairs most and least often neighbors, and each
    label's neighbors by label.
  - **Co-occurrence across distances:** from one label's cells, how much likelier each label's are
    within a distance (or since the distance before) than any cell's, as curves.
  - **Every number's cells** on the tissue: a pair's cells with a neighbor of the other label
    highlighted, the edges between them drawn in color over the graph; a label's cells near
    another's.
  - **Remote control:** `spatial_analysis`; `show_tissue` takes `cells_of`. The project tree lists
    **Spatial analyses**.
  - **Validated:** a checkerboard's enrichment, the permutations' mean against the exact
    expectation (with and without regions), uniform p-values on randomly labeled cells, regions
    that fix every label; co-occurrence worked by hand and against an exhaustive count; on both
    development bundles and the standard simulated tissue, labeled by their clusters, every count
    Squidpy 1.8.3's, every z-score within the two tools' permutation error (correlation above
    0.999), and every co-occurrence ratio Squidpy's routine's in float64 (to 10⁻¹⁴; its float32
    places 3 pairs of Lung and 1 of Breast on the other side of a distance); in the page, the
    counts, expectation, a pair's cells and a co-occurrence ratio against the suite's own.

## 0.5.0 (2026-10-17)

Wave 5 of the [roadmap](situcraft-spec/roadmap.md): the pathology bridge. Post-Xenium H&E and
immunofluorescence images on the tissue, aligned and checked in µm; regions exchanged with QuPath;
cells and regions reviewed with their provenance.

### Added

- **Post-Xenium images (wave 5, slice 1).** An H&E or immunofluorescence image of the section,
  stained and scanned after the Xenium run, drawn on the tissue under the transcripts and cells.
  - **The image:** a pyramidal, tiled OME-TIFF, as Xenium Explorer requires: RGB, or channels of
    8 or 16 bits; deflate, LZW, JPEG or JPEG 2000 tiles. Read where it lies, never copied, a tile
    at a time; recorded as an artifact of the dataset with its SHA-256.
  - **Its alignment:** the transformation matrix or keypoints Xenium Explorer saves, found beside
    the image when 10x named it, recorded as the first version of its alignment (slice 2). The
    image is placed by an alignment, never by the pixel size its own metadata gives (on the
    pathology fixture's immunofluorescence image, the Xenium image's, 3.5 times too small).
  - **The Viewer** lists each image with how it is placed (scale, turn, mirroring), shows it at its
    opacity at any rotation or reflection, an immunofluorescence image's channels each with a
    color, window and gamma as the stains have; saved views keep them. The project tree lists
    **Images**. Remote control: `add_image`; `show_tissue` takes `images`.
  - **XOA 1.0 bundles open:** cells numbered rather than named, the count matrix's earlier layout,
    and Parquet files whose footer's row count is wrong (as fastparquet 1.0 wrote them).
  - **JPEG tiles** in TIFFs, their shared tables and colors as libjpeg-turbo decodes them.
  - **Validated** on the pathology fixture (the breast cancer section of Janesick et al. 2023, its
    bundle's files and its H&E and immunofluorescence images, 2.8 GB): every level of both images
    the pixels tifffile reads; the XOA 1.0.1 bundle's checks; its H&E drawn where its matrix puts
    it.
- **Alignment by landmarks, kept as versions (wave 5, slice 2).** An image can be added without an
  alignment and placed by landmarks; every alignment of an image is a version, and one places it.
  - **The Alignment view:** the Xenium image and the post-Xenium image side by side. A landmark is
    a place clicked on both; once three are placed, the fit predicts where the next one falls and
    the other image goes there. Landmarks are dragged to move them. The fit, a similarity (move,
    turn, one scale, a mirror) or an affine transform (also stretched and sheared), updates as they
    change, with each landmark's residual and its leave-one-out error in µm (where it falls when
    fitted from the others: the error to expect where no landmark is), and the image can be laid
    over the stains as fitted.
  - **Versions:** each alignment is an artifact naming its source and destination coordinate
    systems (the image's pixels, the dataset's µm), with its landmarks, fit and errors, who made
    it, a note and the version it was made from. Which one places the image is a decision kept
    with its reason. Any version is written as Xenium Explorer's transformation matrix or
    keypoints, to load with the image there.
  - **Simulated post-Xenium images:** `situcraft simulate --post-xenium` also makes an H&E
    (hematoxylin and eosin by Beer–Lambert's law, their colors varied per section) and an
    immunofluorescence image (DAPI and a membrane stain) of the simulated section, each moved by a
    known transform and a smooth deformation, written as its truth.
  - **Remote control:** `align_image`, `use_transform`, `show_alignment`; `add_image` without an
    alignment; `export` writes an alignment for Xenium Explorer.
  - **Validated:** fits worked by hand (residuals and leave-one-out errors of a square of
    landmarks); known similarities and affine transforms recovered exactly from their landmarks;
    the page's fits the program's bit for bit; 10x's matrices for the fixture's H&E and
    immunofluorescence image are similarities, and keypoints made with them fit back to them; the
    simulated images' nuclei, found in them, lie where their truth puts them (a median 0.03 µm off
    on the H&E), and the leave-one-out error of landmarks on them is near their fit's true error;
    in the page, an image placed by six landmarks clicked on both images is drawn where the fit
    puts it.
- **Automatic registration (wave 5, slice 3).** **Align automatically** in the Alignment view (and
  `register_image`) aligns an image to the Xenium image by the nuclei, as a job: an H&E's
  hematoxylin (color deconvolution) or an immunofluorescence image's nuclear stain against DAPI.
  - **Its scale, turn and mirroring are found,** whatever its metadata says: a sweep of scales, the
    image as it is and mirrored, the turn from the log-polar form of its spectrum and the shift by
    phase correlation, the best by normalized mutual information; then refined by mutual
    information over a pyramid, and fitted (a similarity or an affine transform) to local matches
    of patches over the tissue, those that disagree with the rest dropped.
  - **A smooth deformation,** when asked, for a section that stretched: B-splines fitted to what the
    model leaves of the matches, as smooth as best predicts matches held out, never folding, and
    kept only when it predicts them clearly better than the model alone. The Viewer draws a
    deformed image as it is deformed.
  - **Each result is a version** of the image's alignment, with its matches (shown on both images),
    its fit and what the search found, started from scratch or from another version. The same
    images give the same version on any computer.
  - **Validated:** on the pathology fixture, without their alignments and with the immunofluorescence
    image's metadata 3.5 times wrong, the H&E and the immunofluorescence image placed within 0.61 and
    0.68 µm (root-mean-square) of 10x's alignment over the Xenium image; on simulated images, the
    scale, turn and mirror found and the result within 0.2 µm of the truth where the image was not
    deformed, and the deformation brings a deformed one from 0.9 µm to 0.2 µm and is refused where
    there is none; the page draws a deformed image where the program's deformation puts its pixels.
- **Alignment checked in µm (wave 5, slice 4).** The Alignment view's **See and check** shows any
  version on the Xenium image, and measures where it is off.
  - **Seen:** over the stains at an opacity, as a checkerboard of the image and the stains (squares
    of a size chosen), or flickering between them; with DAPI's edges (the nuclei's boundaries in
    yellow) or the Xenium segmentation's nuclei over the image.
  - **Measured:** **Measure where it is off** checks a version over the whole section, as a job:
    patches of DAPI where there is tissue, between those an automatic alignment matched, each
    against the image as the version draws it; how far it is off in µm (median, 90th percentile,
    most) and a bound of the matches' own error, below which an error may be theirs. **Show where it
    is off** maps each patch's error as a colored square with an arrow. Each check is recorded as
    an artifact of its version. Remote control: `check_alignment`; `show_alignment` shows a
    version as asked.
  - **Validated:** on a simulated H&E placed by its true transform without its deformation, each
    patch's measured error within 0.15 µm of the true one, without bias (0.03 µm); in the page,
    six landmarks' version measured 0.85 µm off (truly 0.89 µm), the automatic one 0.23 µm; the
    checkerboard and DAPI's edges drawn as asked. On the pathology fixture, 10x's alignment of the
    immunofluorescence image is off by 0.85 µm (median), 2.10 µm (90th percentile); the automatic
    one with its deformation, 0.48 and 0.72 µm.
- **Regions (wave 5, slice 5).** The Viewer's **Regions** draws areas on the tissue, over any layer
  (the stains, an H&E, an immunofluorescence image).
  - **Tools:** a rectangle (drag), a polygon (click its corners; double-click or Enter closes it,
    Backspace takes a corner back), freehand, and a brush that paints a region, or paints onto the
    region chosen or erases from it (holes, and parts cut apart). The wheel still zooms and the space
    bar pans while drawing.
  - **Named and classed,** with QuPath's classes and colors (Tumor, Stroma, Immune cells,
    Necrosis, Other, Region\*, Ignore\*) or any other; each region's area and cells (those of the
    segmentation drawn whose center lies in it); **Choose its cells** chooses them for the QC view,
    the Viewer and the inspector.
  - **Kept as versions** in the project: renamed, reclassed, reshaped, removed and returned, each
    change with who, when and why; any earlier version restored.
  - **With QuPath:** regions written and read as GeoJSON as QuPath writes and reads annotations
    (from QuPath 0.2's layout to 0.4's), in µm, the Xenium image's pixels, or a post-Xenium image's
    pixels through its alignment (a deformation too): drawn on the H&E in QuPath, they land on the
    tissue here, and back. Detections and cells in a file are left out, and counted.
  - **The brush** paints at an eighth of its radius however large the region, and changes a region
    only near its stroke.
  - **Remote control:** `draw_region`, `edit_region`, `regions`, `import_regions`; `export` writes a
    dataset's regions as GeoJSON.
  - **Validated:** in the page, each region's cells the same as an independent count (a winding
    number over the cells' centers); regions drawn by hand where the pointer went; regions written
    in the simulated H&E's pixels through its deformed alignment and read back with the same areas
    (within 0.001%) and cells; annotations laid out as QuPath 0.4 writes them, drawn on that H&E,
    landing within 0.18 µm of where its truth puts them. The brush's area is within 0.5% of the
    exact shape's. With QuPath 0.7.0 itself: regions written for it open there with their names,
    classes, colors, holes and parts, and come back with the same areas and cells; annotations made
    in QuPath come back to their own pixels.
- **Reviews with provenance (wave 5, slice 6).** Cells and regions are reviewed where you look at
  them, and every review is kept.
  - **Actions:** accept a label (or a region's class), relabel it (saying why), mark it ambiguous
    (with the labels it could be), exclude it from analyses (saying why) or include it again, record a
    segmentation concern (cells merged, a cell split, its boundary off, not a cell, cells missed) or
    resolve it, comment.
  - **Provenance:** each review records who, what, the value before and after, when, the
    segmentation, the label set reviewed, each cell's ID, its label and that label's confidence as
    shown, and the version of each post-Xenium image's alignment in use. Reviews are appended, never
    changed: taking one back is another review.
  - **Where:** a cell's inspector in the Viewer, the cells chosen (from the QC view or a region), a
    region's details (excluding a region can exclude its cells), and the Cell types view's review
    queue, whose decisions join the same record (relabels still go to a curated label set; earlier
    ones are read as reviews).
  - **The Viewer's Reviews** counts the cells drawn by their review, lists every review, and writes
    the record as CSV (a row for each cell or region reviewed) or JSON; **Fill by: Review status**
    colors each cell by its review.
  - **Remote control:** `review`, `reviews`; `export` writes reviews; `show_tissue` fills by review.
  - **Validated:** each review's record against the state it changed (a cell relabeled by hand: its
    ID, label, confidence, label set, segmentation and the H&E's alignment in use recorded, the new
    label on that cell and no other; a region excluded with exactly the cells in it; a region
    reclassed); the record written as CSV and JSON and read back row for row; the Review status fill
    every cell's status.
- **Worked examples of the pathology bridge (wave 5, slice 7),** on the simulated tissue's H&E,
  each asking a question, saying what to expect, and doing its steps' jobs from its card.
  - **Align an H&E three ways:** by Xenium Explorer's matrix, six landmarks and automatically, each
    checked over the tissue in µm (0.63, 0.58 and 0.41 µm off, median) and against the simulation's
    truth, which ranks them alike: the check tells versions apart where there is no truth.
  - **Regions for QuPath:** the epithelial and immune domains drawn on the H&E, their cells and true
    types (domains mix their types), written in the H&E's pixels and read back as they were.
  - **A pathologist's review:** the review queue's first twenty cells decided as a reviewer who knows
    their types would (2 of them mislabeled, six times the rate of all the labels), each decision
    recorded with its provenance; a fold excluded with its cells.
  - **Remote control:** `tour` runs them; `open_example` makes an outdated simulated example again
    with `remake`.

### Changed

- **The simulated example has an H&E and an immunofluorescence image** of its section, with Xenium
  Explorer's matrix and where each image's pixels lie in its truth, and each instrument cell's true
  cell and type (`truth/instrument_cell_types.csv`). One made by an earlier SituCraft is listed as
  outdated, with **Make it again**: the same run, cells and transcripts. A simulation's run (its
  experiment and analysis UUIDs) no longer depends on its post-Xenium images, made after it.
- **Regions' outlines are drawn over a dark casing,** legible on cells filled in a like color,
  and a class named by the user is never given gray, which reads as QuPath's Ignore*.
- **American spelling** in the documentation and the interface (color, gray, center, neighbor,
  hematoxylin, artifact, labeled, modeled); what earlier releases wrote into their artifacts'
  files stays as written, so that those are made again byte for byte.

## 0.4.0 (2026-10-16)

Wave 4 of the [roadmap](situcraft-spec/roadmap.md): cell identities with their evidence. Cells are
clustered, mapped and labeled from markers, imported tables or a reference, each label with its
confidence and evidence, reviewed and kept as versions; every result is an artifact of its
segmentation, so it can be made again on another and the two read against each other.

### Added

- **Clusters (wave 4, slice 1).** The **Cell types** view clusters a segmentation's cells by their
  gene expression, as a job, and records the clustering as an artifact of that segmentation with
  every parameter.
  - **The steps,** as the common single-cell workflow takes them: the cells with at least 10 gene
    transcripts (or those a saved QC filter keeps), each cell's counts scaled to the median total
    and log-transformed, each gene scaled, exact principal components (50), each cell's 15 nearest
    neighbors weighted as UMAP's graph, and Leiden communities at a resolution, numbered by size.
    The same seed gives the same clusters on any computer.
  - **What it shows:** each cluster's size and leading markers (the Wilcoxon rank-sum test against
    the other cells, with log₂ fold changes and the shares expressing; exploratory, since cells
    are not replicates), the components' share of the variance, and **Show on the tissue**: the
    cells filled by cluster in the Viewer, with a legend. The Viewer's **Fill by** lists each
    clustering of the segmentation drawn.
  - **Remote control:** `cluster_cells`; `show_tissue` fills by `clusters`.
  - **Validated** against scanpy, leidenalg, umap-learn and scikit-learn on both development
    bundles and the standard simulated tissue: the same principal components (variances within
    1e-13), the same neighbors, modularity within leidenalg's range over ten seeds, markers' scores
    identical to the rank-sum test with exact ties, and the same leading markers as scanpy. On the
    simulated tissue the clusters agree with the true cell types (adjusted Rand index 0.95).

- **The map and exploring clusters (wave 4, slice 2).** Each clustering comes with a map of its
  cells (UMAP, made at once as a job and kept as an artifact), beside the tissue in the Cell types
  view.
  - **Linked:** the map and the tissue colored alike (by any clustering's clusters, a gene, the
    cells' transcripts or area, or how faithfully the map keeps their neighbors), and one selection
    on both: cells lassoed on either side, or chosen by cluster. **Markers of the selection**
    compares any selection with the other cells.
  - **Honest about distortion:** every map's trustworthiness and each cell's neighborhood
    precision and recall are measured, and the page says in plain words what the map can show.
  - **Clusters named and merged:** names are versions in the project (the clusters keep their
    numbers); the same name merges clusters on the map, the tissue and the legends. A **dot plot**
    of each cluster's leading markers.
  - **The instrument's own clusters** (graph-based, and k-means of 2 to 10) and map, read from the
    bundle's `analysis.tar.gz` and added as a clustering whose markers SituCraft computes;
    **two clusterings compared** by their cross-table and adjusted Rand index.
  - **Remote control:** `show_map`, `name_clusters`, `instrument_clusters`.
  - **Validated:** the same map as umap-learn's of the same graph, by every measure within its
    spread over five seeds (trustworthiness 0.737, 0.779 and 0.840 on Lung, Breast and the
    simulated tissue); the trustworthiness recomputed independently from the places; the
    instrument's clusters as the bundle's other copy of them holds them; the map's selection and
    linking in the viewer suite.

- **Labels from markers (wave 4, slice 3).** Under a clustering, the Cell types view labels each
  cell from a marker set, as a job, and records the labels as an artifact with the marker set they
  came from.
  - **Marker sets:** a hierarchy of labels (class, lineage, subtype) and states, each with positive
    and negative markers and the sources they come from. Starter sets for the Breast and Lung
    panels, from published cell atlases; edited in the app as versions kept in the project; read
    and saved as files. Genes the panel lacks are shown, and removed on request.
  - **Scores and labels:** each cell scored for each label against control genes of similar
    expression, as scanpy's `score_genes` scores a gene list (Tirosh et al. 2016), its negative
    markers counting against it; labels taken from the top of the hierarchy down when the best
    leads the next by a margin, else the cell stays at the level above; confidence from the lead.
  - **What it shows:** each cluster's labels at each level as bars, names for the clusters from
    them, each label's markers and control genes; the map, the tissue and the Viewer colored by the
    labels, their confidence or a label's score.
  - **Remote control:** `label_cells`; `show_map` and `show_tissue` color by `labels`.
  - **Validated** against scanpy on both development bundles and the standard simulated tissue:
    every label's control genes scanpy's, every cell's score within 3e-7. On the simulated tissue
    97% of the cells take their true type.

- **Imported labels (wave 4, slice 4).** Labels made elsewhere are imported for a segmentation's
  cells and kept as a label set with their source.
  - **Read:** CSV and TSV (gzipped or not), Parquet, an AnnData object's obs in Zarr (or a
    SpatialData store's table), Xenium Explorer's cell groups with their colors, and the
    instrument's own clusters. The columns of the cells' ids, their labels, a confidence and colors
    are suggested by their usual names, and chosen before importing.
  - **Matched by the cells' ids:** a table naming cells the segmentation does not have is refused,
    with how many (and the segmentation whose cells they are, when the dataset has it); cells the
    table does not name are counted.
  - **Shown as any label set:** each cluster's labels for the clustering shown, names for the
    clusters from them, the map, the tissue and the Viewer colored by them, a label's cells
    selected.
  - **Remote control:** `import_labels`.
  - **Validated** on files written by pandas, pyarrow and anndata, read exactly, and on both
    development bundles: their graph-based clusters, imported as a table, every cell its cluster.

- **Label versions, evidence and review (wave 4, slice 5).**
  - **Versions side by side:** curated labels (a predicted set and a reviewer's changes, each with
    its reason, who and when, appended) and consensus labels (a rule — unanimous, majority, in order
    — over chosen sets, their labels mapped to common names), shown on the map, the tissue and the
    Viewer like any label set; any two compared in a cross-table.
  - **A cell's evidence:** its label in every version; from markers, why it took its label and not
    the next, or not the one its neighbors carry, gene by gene in the cell; its neighbors' labels in
    the tissue and in expression; its counts and its segmentation's flags; its reviews.
  - **The review queue:** cells whose label their nearest cells rarely share, and cells of low
    confidence, the most doubtful first; accept or relabel each, saying why.
  - **Remote control:** `review_labels`, `label_evidence`, `curate_labels`, `consensus_labels`.
  - **Validated** on built sets (the consensus rules), and on planted mislabels: the queue finds 95%
    of them on the Breast tissue and 75% on the simulated one, whose domains mix their types.

- **Labels from a reference (wave 4, slice 6).** Under a clustering, the Cell types view labels each
  cell from a labeled single-cell reference, as a job, and records the labels as an artifact with
  the reference as it was compared.
  - **References:** an AnnData object in Zarr (its labels in `obs`, its counts or log-normalized
    values in `X`, `raw/X` or a layer), or a table of each label's mean expression (CSV, TSV or
    Parquet, a row per label or per gene), its columns suggested and described before use: its
    labels, how many of the panel's genes it has and which it lacks. An `.h5ad` file is read once
    written as Zarr (anndata's `write_zarr`).
  - **The method:** each cell's Spearman correlation with each label's profile over the genes that
    set the labels apart, then again over the leading labels' own genes until one is left (Aran et
    al. 2019, as SingleR maps cells); the label taken by a margin, its lead the cell's confidence.
  - **Its evidence:** each round's correlations, and the genes higher in the cell's label's profile
    and in the next's that the cell expresses; the review queue and versions treat these labels as
    any others.
  - **`situcraft simulate --reference N`** writes a reference of the simulated tissue's types beside
    it.
  - **Remote control:** `reference_labels`; `label_evidence` answers a reference's rounds.
  - **Validated** against SingleR on both development bundles (the Human Primary Cell Atlas's main
    labels) and the simulated tissue: given the same profiles, every cell's correlations within
    3 × 10⁻⁸ and the same label on 99.97% to 100% of the cells. On the simulated tissue 98.1% of the
    cells take their true type.

- **Labels across segmentations (wave 4, slice 7).** What the choice of segmentation changes in
  the cells' labels.
  - **Made again in one step:** a clustering and its label sets from markers or a reference made
    again on another segmentation with the same parameters, from the label sets' card.
  - **The comparison workspace's Labels tab:** a label set of each segmentation read through the
    comparison: how many matched cells keep their label and which change, what split and merged
    cells carry, each label's cells under each, marker mixing by label, and the differences whose
    labels change; the tissue colored by each side's labels.
  - **Segmentation-sensitive cells** join the review queue, with what their counterparts carry
    under the other segmentation.
  - **Remote control:** `repeat_on_segmentation`, `label_impact`; `review_labels` takes `against`.
  - **Validated** on a built fixture by hand and on the simulated tissue's truth: labeled from its
    types' markers, 99.86% of the cells SituCraft's refinement matches to their true cell take its
    type, 98.33% under the instrument's segmentation, with marker mixing nearer the truth's.

- **Scale (wave 4, slice 8).** Clusters, maps and labels of whole sections and large panels.
  - **Above 50,000 cells,** each cell's nearest neighbors are found by NN-Descent, the same for a
    seed on any computer, its recall against exact search measured and shown (99.9% on the
    examples' components), and the map is laid out over every core, the same for any number of
    cores.
  - **Above 1,000 genes** (Xenium Prime), the principal components are found by the Lanczos method:
    5,000 genes in seconds, the full decomposition's to rounding.
  - **Imported segmentations are drawn a band of rows at a time,** and a Xenium bundle's cell mask
    read a row of its chunks at a time (as when the instrument's segmentation is first clustered):
    the memory no longer grows with the slide's area, where a whole slide's mask was held twice.
  - **Validated** on the examples (their neighbors searched approximately: the clusters agree with
    the exact graph's as much as Leiden's own runs do), on simulated panels of 2,000 and 5,000
    genes, and weekly on a simulated tissue of about 400,000 cells: 383,158 typed in about three
    minutes on ten cores at a peak of 3.9 GB, the types' shares within 1.5% of the truth's, and
    the map of every cell drawn in the page in 20 seconds.

- **Worked examples of cell identities (wave 4, slice 9).** Each asks a question, says what to
  expect, and does its steps' jobs from its card.
  - **Cluster and label the Lung cells:** 29 clusters, the map, a cluster by its markers, labels
    from the panel's starter set, why a tumor's cells stay unlabeled by a normal lung's markers,
    the review queue, and the labels in the Viewer.
  - **Labels against the truth** (the simulated tissue): its true types imported as labels; the
    instrument's cells labeled from the types' markers take their true type in 98.3% of matched
    cells, the refinement's in 99.9%, with marker mixing nearer the true cells'.
  - **Labels under two segmentations** (the Breast data): the instrument's cells and a 5 µm
    nucleus expansion labeled the same way and read through their comparison (96.9% keep their
    class), split and merged cells, and the cells whose label changes in the review queue.
  - **Remote control:** `tour` answers what a Cell types step shows and what the Labels tab reads.

### Changed

- **The review queue reads against another segmentation's labels by default** until another
  choice is made, including labels made there after the queue was first opened.
- **A simulated tissue's truth imports as "Simulated truth (imported)"**; it was named after the
  instrument's software ("Xenium Onboard Analysis (imported)").
- **A comparison can be removed from its project** (the inspector's **Remove**). The project's
  history records it, and its files go to SituCraft's trash once no saved project names them. A
  comparison a decision rests on cannot be removed, and the app says why.
- **A comparison made by an earlier SituCraft says so** when its list of differences is in a
  layout this version does not read, and offers to compare the two segmentations again (it said
  "groups.bin: the file ends early"). A comparison whose files are missing offers the same.

## 0.3.0 (2026-10-15)

Wave 3 of the [roadmap](situcraft-spec/roadmap.md): SituCraft's own transcript-informed
segmentation, validated against simulated truth and independent reference outputs.

### Added

- **Simulated tissues (wave 3, slice 1).** `situcraft simulate` makes a tissue whose every cell,
  nucleus and molecule is known, and writes it as a Xenium output bundle labeled simulated, with
  the truth beside it ([docs/FORMATS.md](docs/FORMATS.md)).
  - **The tissue:**
    - cell types with their own sizes, shapes, densities and expression, in domains of different
      composition;
    - nuclei, some outside the section;
    - negative-binomial expression; molecules diffused (a few far), background and negative
      controls, quality scores and depth.
  - **The simulated instrument** segments it by nucleus expansion. Its DAPI image is a tiled
    OME-TIFF.
  - **The truth** (`truth/`) imports into the simulated dataset as a Xenium Ranger bundle of the
    same run, so any segmentation is scored against it with the comparison workspace.
  - **Labeled simulated:** SituCraft marks the bundle simulated in the Data view, the inspector
    and the dataset list. Its methods paragraph says that the data were simulated and implies no
    instrument.
  - **Read by other tools as Xenium data:** the counts are also written as the 10x Genomics
    HDF5 matrix (`cell_feature_matrix.h5`, by SituCraft's own HDF5 writer). `cells.zarr.zip`
    follows XOA 2's layout. spatialdata-io reads a simulated bundle with no warning.
  - **Reproducible:** the same parameters and seed give the same bundle, byte for byte, on any
    computer.
  - **Validated:** goodness-of-fit tests show that the counts, places, displacements, quality
    scores, background and cells follow the distributions they are drawn from.

- **The model of SituCraft's transcript-informed segmentation (wave 3, slice 2),** not yet in
  the app: a voxel lattice; a Poisson likelihood with per-gene background; a mixture of
  expression states with negative-binomial counts; log-normal cell volumes; and a prior from the
  nuclei. Its parameters are drawn by exact conditional updates. Each update is checked against
  its analytic posterior, and the whole sweep by Geweke's joint distribution test, with the same
  result on any number of threads and any processor. The equations are in
  [research.md §3.2](situcraft-spec/research.md).

- **The sampler of SituCraft's transcript-informed segmentation (wave 3, slice 3),** not yet in
  the app. It moves cells' boundaries voxel by voxel, keeping each cell whole and within a
  perimeter bound, and repositions each transcript within its likely displacement. It runs in
  parallel with the same result on any number of threads and any processor. It reports each
  transcript's most probable cell with its probability. On a simulated tissue it puts 85.9% of
  transcripts in their true cells, against 82.5% for the instrument's nucleus expansion, and its
  cells overlap the true ones more (median IoU 0.663 against 0.622). Its moves are checked to keep
  the posterior exactly (detailed balance on every state of small lattices).

- **Transcript-informed segmentation in the app (wave 3, slice 4).** **Refine from the
  transcripts** in the Segmentation view redraws the cells from the transcripts as a job and
  records the segmentation as an artifact. That artifact keeps each transcript's probability of
  its cell, the run's diagnostics, and each layer's outlines when there are several. It is
  compared with the instrument's segmentation at once, ready for review in the comparison
  workspace.
  - **The view shows** whether the run settled (Geweke's diagnostic), how stable its assignments
    are, and which cells it is least sure of: few transcripts, uncertain boundaries, no nucleus.
  - **The workspace** lists a chosen difference's flagged cells.
  - **The methods paragraph** describes a refined segmentation.
  - **Remote control** adds `refine_segmentation`.
  - On a simulated tissue it puts 85.9% of transcripts in their true cells (the instrument's
    expansion: 82.5%), with a median IoU of 0.678 (0.622).

- **The segmentation studio (wave 3, slice 5).** The Segmentation view sets up and runs a
  refinement in six steps: Input, Compute, Model, Review, Run, Compare. Each step shows what the
  run would do on this bundle and this computer.
  - **What it covers:** the whole tissue, its separate sections apart (each with a model of its
    own), or chosen regions, typed in µm or the region last framed in the Viewer. Cells about a
    region are held as the instrument drew them, so no cell is cut; the rest of the tissue keeps
    the instrument's segmentation. The transcripts' quality is a setting (Q20 to start).
  - **Presets:** Balanced, Conservative boundary, Flexible morphology, Low memory, ROI pilot and
    Expert. Each says what it is for, what it changes and what it risks, with its measured
    accuracy on a simulated tissue; every setting says what it does and what it costs. Changing
    one but the seed makes the settings Expert.
  - **The ROI pilot** refines a region twice with different seeds and reports how often the two
    put transcripts in the same cell. On a simulated tissue it matched the whole tissue's run in
    its region.
  - **A preflight** counts the transcripts modeled and those left out, the cells it starts from,
    and what it covers. It estimates memory, disk and time against what the computer has, and
    refuses a run that would not fit. On the Lung example its estimates were within 5% of the
    memory a whole run took, 10% of the disk and 10% of the time.
  - **Progress, a log and Stop** while it runs. A failure (missing cells to start from, a
    malformed bundle, memory, disk space, a region without cells) says what went wrong and what
    to do, in the studio, the Jobs view and remote control.
  - **Recipes:** **Save the recipe** keeps a refinement's settings as readable JSON. **Load a
    recipe** applies one.
  - **Remote control** adds `refinement_preflight`; `refine_segmentation` takes a preset, regions
    or sections, the quality, a second seed and the threads.

- **`situcraft refine`** runs a refinement in the terminal from a preset or a recipe, writing the
  segmentation's files and its recipe to a new folder; `--preflight` says what it would need. The
  same recipe gives the same files, byte for byte, as the app. The folder imports into the app
  (**Import a segmentation**, SituCraft segmentation).

- **Validation against an independent reference (wave 3, slice 6).** SituCraft's refinement is
  compared with Proseg 3.2.0 (the publication's software) on both 2-FOV bundles and a simulated
  tissue, with the comparison workspace's measures; the numbers are kept in
  `validation/reference/segmentation/` and checked weekly.
  - On the simulated tissue SituCraft puts 84.7% of transcripts in their true cell, against
    Proseg's 75.2% and the instrument's 82.2%, with outlines as close to the true ones (median IoU
    0.719 against 0.727), and leaves a tenth as many of the cells' transcripts in no cell (0.13%
    against 1.42%).
  - Every tolerance is met on the Breast bundle and the simulated tissue. Three differences on the
    Lung bundle are explained in research.md.
  - The Breast bundle refines in 83 s and 279 MB on a laptop.

- **The background's calibration (wave 3, slice 7).** The refinement's background has two parts:
  false decodes, which fall on every codeword alike and which the panel's negative controls
  measure, and each gene's ambient RNA. The false decodes are now measured, not guessed.
  - **A benchmark on simulated truth** at three background levels and each kind alone: the false
    decodes are measured within a sixth of the truth; on the standard tissue twice as much of the
    background is found as before, with the same accuracy. Of what SituCraft leaves in no cell,
    55–72% truly is background (Proseg: 18%).
  - **The diagnostics report the background:** the shares the model's rates account for, false
    decodes and ambient RNA, the negative controls that measured them, and the share left in no
    cell (`diagnostics.json`; the studio's Background).
  - **The limit, documented:** inside cells, ambient RNA cannot be told from the cells' own
    expression, so most of it stays counted in cells (research.md §3.2 and §9). On the real
    bundles false decodes are under 0.1% of the transcripts, and results there hardly change.

- **Segmentations on the tissue (wave 3, slice 8).** The Viewer draws any of a dataset's
  segmentations over its stains, chosen in its Cells layer: the instrument's, nucleus expansions,
  refinements, imports. A Viewer opened afresh draws the dataset's default. The legend names the
  segmentation drawn and says how it was made.
  - **A refinement's uncertainty, shown:** cells filled by their flagged limitations (few
    transcripts, uncertain boundary, no nucleus), by their boundary certainty or by their
    transcripts' mean probability; transcripts colored by their assignment's probability, close
    up. Doubtful cells and transcripts are drawn bright, certain ones dark.
  - **The inspector** describes the segmentation's cell: the instrument's cell it came from, its
    counts and top genes, and what its method flags.
  - **Show on the tissue** on each segmentation in the Segmentation view and the inspector, and on
    an expansion's and a refinement's result.
  - **Saved views and links** keep the segmentation drawn, its cell and how transcripts are
    colored. Remote control: `show_tissue` takes `segmentation`, the new fills and `color_by`;
    `select_cell` selects a cell of the segmentation drawn.
  - **A refinement records each cell's boundary certainty** (`certainty.bin`,
    [docs/FORMATS.md](docs/FORMATS.md)).

- **Larger tissues (wave 3, slice 9).** A region of more than about 4 million modeled transcripts
  is refined in patches of about 2 million, one after another, each also refining the cells within
  40 µm of its square, and stitched into one segmentation: each pixel and transcript from the
  patch whose square holds it, each cell counted once. The result is still the same for the same
  seed on any computer. The preflight, the studio and `situcraft refine` say how many patches, and
  the diagnostics keep each patch's summary.
  - **As accurate as one run:** on the standard simulated tissue in four patches, 86.1% of
    transcripts in their true cell (86.3% in one run), 87.4% within 20 µm of a seam.
  - **Measured at scale:** a simulated tissue of 6.1 × 5.2 mm and 55 million transcripts was
    refined in 25 patches in 5 to 6 GB of memory (an estimated 10.8 GB in one run). What still
    grows with the tissue is about 100 bytes a transcript: a computer with 16 GB refines about 120
    million.
  - **The Viewer streams a segmentation's transcripts:** their cells and probabilities are written
    once into the cache by tile and read by tile, never held whole. The dataset's own cells table
    counts each cell's genes a row of tiles at a time, no longer holding a pair for every
    transcript. At 55 million transcripts the
    Viewer drawing a refinement peaked at 0.9 GB (1.8 GB before), and with the dataset alone at 0.2 GB (1.7 GB before).
  - **`situcraft simulate --preset large`** makes that tissue: a mosaic of 7 × 6 standard fields,
    made a field at a time and joined into one bundle (`tiles_across`, `tiles_down`;
    [docs/FORMATS.md](docs/FORMATS.md)), in about 10 minutes and 3 GB of memory.
  - **A weekly scale test** makes it, refines it, opens and draws it, each step's time and memory
    within its budget, then deletes it (`validation/scale_test.go`).

- **A third example: a simulated tissue with known truth,** made on this computer in seconds
  (**Make it** on the start page; `open_example` with `simulated`). Its worked example scores the
  instrument's nucleus expansion and SituCraft's refinement against the truth.
- **A worked example on the Breast data refines the instrument's cells** from their transcripts,
  compares the two, and shows where the refinement is unsure of itself.

### Changed

- **Simulated tissues' background is of two kinds:** ambient RNA on the genes in proportion to
  their expression, and false decodes on every codeword, genes and negative controls alike
  (`ambient_per_100um2` and `false_decodes_per_100um2`; a parameters file giving the old
  `background_per_100um2` or `control_per_100um2` is refused). The standard tissue's background is still 2.46% of its transcripts; the same seed
  now makes a different tissue.
- **A transcript counts in a cell only when the model attributes it to the cell,** not merely when
  it lies in the cell's voxels: the refinement's assignments and counts leave out what the model
  takes for background. On the Breast example the assigned fraction fell from 98.8% to 97.0% and
  spurious co-expression from 1.94 to 1.83.
- **Simulated tissues have every transcript column of Xenium's,** with its types:
  `nucleus_distance` and `codeword_index` are new, and `overlaps_nucleus` is 8 bits. Tools that
  need them now read simulated bundles.
- `comparison.json` counts, of the transcripts A assigns, those B puts elsewhere
  (`changesQ20OfAssignedA`): against the truth, the share in their true cell.
- **The default perimeter bound is 1.6** (it was 2), the tightest that still admits a cell four
  times as long as wide at any angle. On the standard simulated tissue the median IoU with the
  true cells rose from 0.678 to 0.712 and the transcripts in their true cells from 85.9% to
  86.3%, every cell type gaining. Refinements made with 0.3.0's earlier builds keep their own
  settings.
- **A refinement takes about half the memory:** the posterior's tallies are smaller (the
  split-half agreement is estimated on every eighth transcript), only the columns it needs are
  read, and the garbage collector keeps the heap closer to what it holds. On the standard
  simulated tissue its peak fell from 172 to 89 MB; its result is unchanged.
- **Convergence is said to be judged only when it can be:** with fewer than 100 sweeps
  summarized, Geweke's diagnostic cannot judge it, and the page now says so instead of
  "settled".
- `diagnostics.json` holds each region's summary and trace (`regions`; a patch's with its square,
  `patch`, and the summary's `patches`); the recorded parameters hold the area, the regions
  refined, the patches, the quality, the preset and a second seed
  ([docs/FORMATS.md](docs/FORMATS.md)).
- `situcraft refine`'s summary says what it refined in: "in one region", "in 3 regions" or "in 25
  patches of one region".

### Fixed

- **A SpatialData import matched only some transcripts** when its points kept both where a tool
  moved each transcript and where it was observed: transcripts are matched at their observed
  places.
- **Discarding or resuming a job could read its state while it was changing** (a data race the
  race detector found now and then). The state is read before the lock is let go.
- **`refine_segmentation` could time out** under remote control on a long run: it is now a long
  action.
- **Nucleus expansion stopped with an internal error** when a bundle's cell mask
  (`cells.zarr.zip`) had more cells than its `cells.parquet`. It now refuses with the reason.

## 0.2.0 (2026-10-14)

Wave 2 of the [roadmap](situcraft-spec/roadmap.md): segmentations become artifacts to compare,
review and choose between, and to export. The instrument's segmentation is the first; SituCraft's
nucleus expansion the second; other tools' are imported. Any two are compared cell by cell, reviewed
on the tissue, and one promoted with its reason; any is exported in open formats, every file listed
by its SHA-256.

### Added

- **Remote control (wave 2, slice 1).** `situcraft --remote-control` lets programs on this
  computer (scripts, notebooks, the documentation's screenshots) drive the open window, which
  performs each action where you see it; what an action changes is in the history and can be
  undone ([docs/REMOTE-CONTROL.md](docs/REMOTE-CONTROL.md)).
  - **20 actions:**
    - **the project:** describe it; start or open a project; open bundles and archives by path;
      add a dataset; open an example; take a tour;
    - **the tissue:** show a view; focus an item; show the tissue (place, zoom, stains, genes,
      layers, a cell); select a cell;
    - **saved views:** save, open and remove them;
    - **QC:** read the metrics; show a QC map; choose cells; save a filter;
    - **the record:** the methods paragraph; write the project, an archive or the methods to a
      file.
  - **Finding SituCraft:** scripts find it in `remote.json` (its address and a token), and
    `/api/remote/tools` lists the actions with their arguments' schemas.
  - **Safety:** it answers this computer only, and only the window opened from this run.
    Opening paths and writing files also need the token, and a file is never written over an
    existing one unless asked.
- **Segmentations as artifacts (wave 2, slice 2).**
  - **The artifact store** (`<data dir>/artifacts`): what jobs make and register, each with its
    lineage (what it was made from, the job and its spec, the method and its version, the
    software), the SHA-256 of every file, what its check found, and its state.
    - Outputs are written to a staging folder and checked before they become an artifact.
    - An artifact is never changed; one whose check finds a fatal problem is kept as invalid, for
      review, and never used.
  - **The instrument's segmentation** of each dataset is registered as an artifact by a job, when
    the dataset is added or a project reopens (so 0.1.0 projects get theirs). It records:
    - the bundle's files that make it (cells, boundaries, nuclei, transcript assignments, counts,
      masks) by SHA-256, never copied;
    - the cells each method segmented, and the software that did it;
    - the findings of the bundle's validation that bear on it.
  - **Promotion:** each dataset's default segmentation is promoted by a recorded decision with its
    reason. The instrument's is promoted when it is the only one, and decisions are never removed.
  - **The project tree** lists the segmentations, the default marked; the inspector shows a
    segmentation's lineage, files, findings and the decision that made it the default.
- **Jobs that survive the program (wave 2, slice 2).**
  - Jobs are kept in `<data dir>/jobs`. A job SituCraft stopped, or that was running when it
    crashed, is shown as interrupted when it starts again, and can be resumed or discarded.
  - Resuming continues the work from where it stopped: a derived-data build keeps what it
    finished.
  - Closing the window while a job runs asks first; if it is closed anyway, SituCraft keeps
    running until the job ends (Ctrl+C stops it, and the job can be resumed).
  - **The Jobs view:** what runs and waits, what was interrupted, and what finished. Each job has
    its state, progress, times and runs, its whole log and its spec, and Stop, Resume and Discard.
  - The status bar's job opens the Jobs view.
  - Remote control gains `segmentations` and `jobs` (list, read, stop, resume, discard).
- **Nucleus expansion (wave 2, slice 3).** SituCraft's first segmentation of its own.
  - **The method:** each nucleus grows into a cell of its own by up to a chosen distance (5 µm to
    start), every pixel going to the nearest nucleus within it, by exact Euclidean distances. It
    runs on every core in bands of rows, and gives the same cells however the work is split.
    Transcripts belong to the cell holding the pixel they lie in; the counts are of gene
    transcripts of quality 20 or more. Bundles without a nucleus mask have their nucleus polygons
    drawn instead.
  - **An artifact of its own:** its cells, label raster, transcript assignments, counts and
    outlines ([docs/FORMATS.md](docs/FORMATS.md)), checked before it is recorded (geometry,
    assignments, counts, metadata), with every parameter in its lineage. The same dataset and
    distance give the same artifact.
  - **Compared with the instrument's segmentation:** pixels and transcripts in the same cell, in
    another or in only one; cell areas; the assigned fractions; and what differs, described.
  - **The Segmentation view:** the dataset's segmentations side by side by their numbers;
    **Expand the nuclei**, run as a job; the comparison.
  - Remote control gains `expand_nuclei` (23 actions).
- **Importing segmentations (wave 2, slice 4).** Segmentations made by other tools, imported to
  compare with the others:
  - **SpatialData Zarr** (format 2 or 3, as spatialdata and spatialdata-io write it): cells from
    shapes or a label image, each transcript's cell from points, counts from the table;
  - **Baysor's** `segmentation.csv` with its polygons;
  - **GeoJSON** polygons, with or without a transcript table (CSV or Parquet);
  - **Xenium Ranger** output bundles of the same run.
  - Each is checked against the dataset: its coordinates' units, declared and never guessed (the
    check says which units fit when the cells lie outside the image); the transcripts it names,
    by id and position; an output bundle's run; its ids. It becomes a segmentation artifact of
    SituCraft's own files, kept with the SHA-256 of every file read, and counted as every
    segmentation is; one whose check finds a fatal problem is kept as invalid, for review.
  - **Import a segmentation** in the Segmentation view; the inspector shows the files imported.
  - Remote control gains `import_segmentation` (24 actions).
  - **Readers:** AnnData in Zarr; Zarr's sharding and text of varying length (as anndata 0.11 and
    later write); GeoJSON and WKB polygons; CSV, TSV and Parquet tables.
- **Comparing segmentations (wave 2, slice 5).** Any two segmentations of a dataset compared,
  cell by cell:
  - **Which cells correspond:** a shared nucleus, or at least half of one within the other (or, for
    cells without outlines, of their transcripts). Each set of corresponding cells is a
    difference: one-to-one, expanded, contracted, split, merged, rearranged, only in one, or
    assignment-only.
  - **Measures:** each segmentation's cells, cells with a nucleus or several, assigned fraction,
    transcripts per cell, areas, cells in pieces, job time and size; between the two, the pixels
    that agree, the transcripts that changed cell (gained, lost, moved), the overlap (IoU) and how
    far boundaries moved; and by regions of 50 µm.
  - **Transcripts given to the wrong cell:** spurious co-expression of gene pairs relative to
    nuclei alone, after Jones et al. (2025), and the mutually exclusive co-expression rate of
    Hartman and Satija (2024).
  - **Compare two segmentations** in the Segmentation view; each comparison is an artifact kept
    with the project. Remote control gains `compare_segmentations` (25 actions).
- **The comparison workspace (wave 2, slice 6).** The **Compare** view shows a comparison's two
  segmentations on the tissue:
  - **side by side** with one camera, **overlaid** in two colors, or in turns (**flicker**); cells
    filled by the class of their difference; a **disagreement map** by 50 µm region;
  - **the review queue:** the differences ranked by reassigned transcripts, contamination (markers
    of different lineages in one cell), disagreement or low counts, among the classes and
    reassigned transcripts you keep; each opens where it lies, lit in both and described in the
    inspector; **cell matching** lists them all;
  - **decisions:** promote one segmentation as the dataset's default, keep one for exploration, or
    reject one, each with a reason and the comparison it rests on, appended and never removed;
  - the measures' header warns when a measure rests on little.
  - Comparisons appear in the project tree. Remote control gains `show_comparison` and
    `decide_segmentation` (27 actions).
- **Exports (wave 2, slice 7).** **Export a segmentation**, in the Segmentation view, writes any
  of a dataset's segmentations into one folder, in the formats chosen:
  - **SpatialData Zarr:** the cells as a label image and as polygons, the transcripts as points
    with their cells, and the counts as an AnnData table annotating the labels, laid out as the
    spatialdata package writes a store, in µm;
  - **AnnData Zarr:** the counts with each cell's measures and centroid;
  - **Parquet and CSV:** cells, transcripts with their cells, outlines and counts, in Xenium's
    column names;
  - **Matrix Market:** the counts as Xenium and Cell Ranger write them;
  - **GeoJSON:** each cell's outline as a polygon naming it;
  - **Xenium Ranger's inputs:** the cells and the bundle's nuclei as label masks on the morphology
    image's pixels, checked (their size, cells without pixels, transcripts assigned otherwise than
    the mask holds, a dataset without nuclei, a zipped bundle), and the `import-segmentation`
    command, shown with a button to copy it. SituCraft does not run it.
  - Beside the files, `situcraft-export.json` says what was exported, from what and by which
    SituCraft, lists every file by its SHA-256, and fingerprints the label image, the counts and
    the transcripts' cells, which every format holds the same.
  - The export runs as a job, written under a temporary name and renamed into place; a folder
    that exists is replaced only when asked, and only when it is a SituCraft export. The project
    records each export.
  - SituCraft's own writers: Parquet, Zarr (formats 2 and 3), AnnData, NumPy arrays, Matrix
    Market and GeoJSON, each read back by its readers in the tests.
- **Worked examples and practice (wave 2, slice 7).**
  - Each example has a **worked example**, a question answered step by step, saying first what to
    expect. Some steps run a job before going on (the card's button says what it does), others
    open the comparison in the workspace as they describe it.
    - **Breast:** the instrument's cells, drawn from stains, against a 5 µm nucleus expansion.
    - **Lung:** find the planted errors.
  - **Practice: find planted errors** copies the instrument's segmentation with known errors in
    it (neighboring cells of different lineages merged, large cells split, cells missed, cells
    drawn too wide), recorded as a segmentation, simulated, with its answer key. The answer key
    lists every error and takes you to it in the workspace. It is offered on the example datasets;
    on your own, only once you turn on **Practice tools for every dataset** (the theme button's
    menu). A planted segmentation is never promoted or exported.
- **The methods paragraph** describes a dataset's default segmentation when it is not the
  instrument's: its method and citation, the cells it gives, and the decision choosing it with the
  comparison it rests on.
- **Remote control** gains `plant_errors` and `export_segmentation` (29 actions; exporting needs
  the token, as it writes files), and `tour` takes a worked example and does its steps' jobs.
- **Screenshots** of the documentation: 20 scenes in both themes, captured through remote control
  from the development bundles (`node docs/capture/capture.mjs`), in the README and the
  getting-started guide.
- **A cell by its ID:** the program finds a cell by its `cell_id` as well as by its row.

### Fixed

- **The same segmentation on every processor:** the outlines of an expansion (and of planted
  errors) could differ in their last bits between Apple silicon and Intel or AMD processors, where
  the compiler may fuse a multiply and an add; the outlines are now simplified with exact
  arithmetic, and every product that reaches an artifact is rounded on its own. Found by the
  continuous integration (Linux, amd64) reopening the 0.2.0 project made on a Mac (arm64).
- **Job ids** could repeat when SituCraft was started again within the same millisecond, and a
  new job then took an earlier one's record: an id already used is skipped.
- **A dataset opened by several requests at once** (the Compare view's, when no other view had
  opened the dataset first) could be closed by one while another read it, and answered "damaged
  cache file": one request now opens it while the others wait for it.
- **A SpatialData table's region** written as a text scalar in Zarr format 3 was read from its
  attributes only: zero-dimensional text arrays are now read.
- **The viewer suite on CI,** which fetches only the Lung bundle: it counted the jobs of two
  bundles.
- **The documentation's screenshots** no longer show a path of the computer that took them in a
  text field.
- **Accessibility, found by the audit of the screenshots** (axe-core, WCAG 2.1 A and AA):
  - the view rail's separators sat inside its tab list;
  - the project tree's sections were marked as lists with no items in them (and its rows,
    buttons, as list items);
  - a hidden stain's name and ranges were dimmed below the contrast text needs.

### Validation

- **Exports and worked examples (slice 7):**
  - **Go tests:** the Parquet, Zarr and AnnData writers' files read back as written by SituCraft's
    readers (and by pyarrow, zarr-python and anndata when they were written); every format of a
    fixture segmentation read back, the SpatialData store imported as the same segmentation and the
    GeoJSON polygons with the Parquet or CSV transcripts giving every transcript's cell; the same
    segmentation giving the same files; an existing folder replaced only as the rules allow.
  - **The Lung bundle's own segmentation exported in every format:** imported back from
    SpatialData exactly (the same fingerprints); Xenium Ranger's masks of the morphology image's
    size with the bundle's nuclei. **Read by spatialdata 0.8.0, anndata 0.13.4, pyarrow, pandas,
    scipy, geopandas and numpy** (`validation/reference/read_exports.py`, run by the maintainer
    and weekly), every format gives the manifest's totals and fingerprints; the result is
    committed (`validation/reference/exports.json`) and the real-data tests check SituCraft still
    writes it.
  - **Planted errors:** on hand-made tissue and on the Lung bundle, the comparison finds each
    planted error as what it is (every merge merged, mixing lineage markers; every split split;
    every missed cell only in the instrument's; every grown cell expanded) and nothing else; the
    same seed plants the same errors.
  - **Node tests:** planted segmentations recorded as such and never promoted; exports recorded;
    every worked example's steps doable in order; the methods' sentence for a default segmentation.
  - **The same artifacts on every processor:** the Lung expansion's and planted errors' files have
    the same SHA-256 built for arm64 and for amd64, and the browser suite passes with an amd64
    build against the project saved on arm64.
  - **In headless Chrome** (80 checks): the Lung worked example plants, compares and opens the
    queue on a planted error, and by contamination a merge comes first; every planted error is
    the difference it should be, and a planted segmentation is never exported;
    `export_segmentation` writes only with the token and the
    SpatialData export imports back as the instrument's segmentation; the Breast worked example
    gives 263 splits, 128 merges and 119 cells only in the instrument's; the project saved by 0.2.0
    (`validation/projects/`) opens, and every artifact it records is made again with the same
    checksums.
  - **Every screenshot scene in both themes** passes axe-core (40 captures, no violation).
- **The comparison workspace (slice 6):**
  - **Node tests** of the review queue's rankings and rules, the cautions, and the decisions'
    rules; **Go tests** of each segmentation's outline tiles and of a comparison's differences.
  - **In headless Chrome** (70 checks): the queue ranked as asked, both segmentations drawn with
    one camera in every mode, and decisions recorded only as the rules allow.
- **Comparisons (slice 5):**
  - **Hand-drawn segmentations** of every class, each count and measure worked out by hand; a
    planted leak of transcripts raises the spurious co-expression; the MECR by hand.
  - **The instrument's segmentation against the 5 µm expansion:** on Lung every cell one-to-one,
    with the 98.0629% of pixels and 8,724 changed transcripts that the expansion's own comparison
    counted; on Breast every cell of both accounted for by class.
  - **In headless Chrome** (67 checks): `compare_segmentations` by remote control.
- **Imports (slice 4):**
  - **The Lung bundle** imported into itself as an output bundle, and as spatialdata-io writes it
    as SpatialData, gives the instrument's segmentation exactly: the raster identical to the cell
    mask, every transcript's cell, every cell's count. Its polygons and assignments written as
    GeoJSON with a table, and as a Baysor-style table without ids, give every assignment. The
    Breast bundle is refused as another run.
  - **Fixtures** written by each format's own libraries (spatialdata 0.8.0 and 0.2.6, geopandas,
    pyarrow, pandas, anndata, zarr-python) import to the pixels and assignments their scene
    defines, with each planted problem found.
  - **In headless Chrome** (65 checks): `import_segmentation` by remote control, only with the
    token; another run's bundle refused.
- **Nucleus expansion (slice 3):**
  - **The Lung bundle's own 5 µm expansion is reproduced:** 11,898 cells paired one to one with
    the instrument's; 98.06% of pixels and 98.94% of transcripts in the same cells; cell areas'
    median difference +0.25%, 97.0% within 5%; 98.28% of gene transcripts assigned (the
    instrument: 97.97%). Every transcript given differently is explained: 2,534 on the
    expansion's rim, 4.56–4.98 µm from their nucleus, which the instrument's cells do not reach;
    6,182 where neighboring cells meet, within 0.34 µm of equally near both nuclei; 8 the
    instrument records in a cell other than its own mask's at their pixel.
  - **Breast,** whose cells the instrument drew mostly from stains: 7,312 nuclei grown into cells,
    and the instrument's 7,275 cells accounted for (6,752 holding one nucleus, 255 none, 268
    several).
  - **Go tests:** the distance transform against brute force; the same cells for any split of the
    work; polygons drawn; the files read back as written, and the check finding each kind of
    damage; masks read in bands; on Lung, drawn nucleus polygons agreeing with the mask on 98.56%
    of pixels; the job and its artifact through the program's API.
  - **In headless Chrome** (62 checks): `expand_nuclei` by remote control reproduces the Lung
    expansion in the Segmentation view, and asking again answers the same artifact without a job.
- **Segmentations and jobs (slice 2):**
  - **Go tests:**
    - the artifact store records checked outputs with their SHA-256, keeps an invalid one apart,
      refuses unlisted outputs and paths leaving its folder, references bundle files without
      copying them, verifies them again, and finds a changed file;
    - jobs are kept on disk, read back after the program stops or crashes (interrupted ones
      orphaned), resumed under the same id with their log, and discarded with what they left;
    - on the Lung bundle, a derived-data build interrupted by the program stopping resumes, after
      a restart, to entries that pass their check;
    - the instrument segmentation's six files are recorded with the SHA-256 a direct reading
      gives, and registering again gives the same artifact without a job.
  - **In headless Chrome:**
    - both bundles' instrument segmentations are registered, every file's SHA-256 as Node computes
      it, promoted as the default;
    - their methods account for every cell (Breast: 3,300 by boundary stain, 3,913 by interior
      stain, 62 by nucleus expansion);
    - the 0.1.0 project gets its own, the same artifact, and still reopens its views exactly;
    - every job is complete.
  - **By hand:**
    - SituCraft killed in the middle of the Breast bundle's build: on restart the build is
      interrupted, and resuming it continues the image pyramid where it stopped;
    - every scene, the two new ones included, passes axe-core in both themes.
- **Go tests of the remote-control hub:** actions reach the window and its answer returns, one at
  a time; no window, a silent one, one that leaves and one of another run are each refused
  plainly; only this computer, and no other site's page, may send actions; opening paths and
  writing files need the token; exports never replace a file unasked; the connection file is
  private and removed on stop; the window's action table, the program's list and
  `docs/REMOTE-CONTROL.md` name the same actions.
- **In headless Chrome** (`node validation/run.mjs viewer`, 52 checks):
  - a program drives the window: `get_state` describes the open project;
  - `show_tissue` shows exactly the genes, place, zoom, cell and fill asked for;
  - a view it saves reopens exactly;
  - a filter it saves keeps the cells, under the same SHA-256, that the 0.1.0 project's identical
    filter recorded;
  - the methods cite the data and SituCraft;
  - `export` writes the project only with the token.
- **Every screenshot scene in both themes** passes axe-core's WCAG 2.1 A and AA rules
  (`docs/capture/audit.json`, no violation).

## 0.1.0 (2026-10-14)

SituCraft's first release, wave 1 of the [roadmap](situcraft-spec/roadmap.md): open a Xenium
output bundle, check it, and see the tissue, its transcripts and its cells at any zoom, with QC
linked to the tissue and a project that reopens exactly. Start with
[Getting started](docs/GETTING-STARTED.md): its example datasets download in a click, each with a
guided tour.

### Added

- **The program and its window (wave 1, slice 1).**
  - One self-contained program (`situcraft`) for macOS, Linux and Windows, x64 and ARM64.
  - It serves its embedded web app on 127.0.0.1, port 8840 or the next free one up to 8859, and
    opens it in a desktop window.
  - Its ports are clear of the sibling programs' (Proteoscope 8765–8814, CytoWeave 8770–8789,
    MaveScape 8820–8839), and a test keeps them so.
  - A second launch hands its paths to the running SituCraft.
  - The host and its security are adapted from MaveScape 0.8.0: a Host-header check against DNS
    rebinding, a same-origin API, a Content-Security-Policy and cross-origin isolation.
  - The content-addressed library is adapted from MaveScape 0.8.0 too, and holds projects.
- **Items named on this computer.**
  - On the command line or through **Open a bundle** (a path), SituCraft recognizes:
    - Xenium output bundles, by their folder, their `.zip` or their `experiment.xenium`;
    - Zarr stores, OME-TIFF images, Parquet and CSV tables, GeoJSON polygons and project archives;
    - folders of these, in natural order.
  - A bundle counts as one item: SituCraft never walks into it, serves it whole or copies it into
    the library.
  - The items are listed under **On this computer** in the project tree. Reading bundles comes in
    a later slice of this wave.
- **The workbench shell.**
  - A view rail in the order of the work: Data, Viewer, QC, Segmentation, then Cell types,
    Spatial, Cohort, Reports and Jobs. Each view says what it will do until it is built.
  - A start page with recent projects.
  - The project tree, an inspector, and a bottom drawer with the project's history and the
    session's log.
  - A status bar, the command palette (⌘K), undo and redo.
  - Light and dark themes, and color-vision-friendly colors.
  - Without its program, the page says how to start SituCraft: a web page alone cannot read data
    where it lies on the computer.
- **Every byte of a Xenium bundle (wave 1, slice 2).** SituCraft's own readers, in Go's standard
  library alone, for everything a Xenium output bundle stores:
  - **Codecs:** Zstandard (RFC 8878, with XXH64 checksums), LZ4 blocks, Snappy and Blosc (byte and
    bit shuffling).
  - **Parquet:** flat schemas, every physical type, plain, dictionary, RLE, delta and
    byte-stream-split encodings, data page versions 1 and 2, and Zstd, Snappy, gzip, LZ4 or no
    compression.
  - **Zarr:** formats 2 and 3, in directories and zips, with Blosc, Zstd, zlib, gzip and LZ4
    chunks, C and F order, both byte orders, fill values for chunks never written, CRC-32C and
    transposition.
  - **TIFF and BigTIFF:** strips and tiles, SubIFD pyramids, deflate, LZW, PackBits and both
    predictors, and OME-XML, including multi-file OME-TIFFs (Xenium's four morphology-focus
    channels) and z-stacks.
  - **JPEG 2000 (ITU-T T.800, Part 1):** JP2 files and raw codestreams; every progression order,
    code-block style, tiles and tile-parts, layers, precincts, packed headers and ROI; 5/3 and 9/7
    wavelets with both component transforms; decoding at reduced resolution; code blocks decoded
    in parallel with the same result on any number of cores. Wired into the TIFF reader for
    Xenium's tiles (34712) and whole-slide images' (33003, 33005).
- **Opening and checking a bundle (wave 1, slice 3).**
  - **Opening:** a Xenium output bundle opens by its folder, its `experiment.xenium` or its zip, on
    the command line or by **Open a bundle**. SituCraft never writes to it. A zipped bundle's
    compressed members are unpacked into the cache once.
  - **What it describes:** which software and version wrote the bundle and how far SituCraft reads
    it (XOA 2.x supported, others best-effort, said so), and every file by role, required or
    optional.
  - **Checks, each finding fatal, a warning or a note, with its files, count, tissue region and
    remedy:**
    - files present and readable;
    - images against the manifest;
    - cells, transcripts, the matrix, cells.zarr and boundaries against one another;
    - genes against the panel;
    - polygon validity;
    - free space for the cache.
    - It also recomputes twelve of the run's own recorded metrics from the data.
  - **The Data view** shows each bundle's facts, summary, findings and files.
    - **Add to project** records a bundle with no fatal problem: its place, run identifiers,
      summary, findings and a fingerprint of its files.
    - Reopening a project finds each dataset's bundle again and says whether it is the same,
      changed, another run, or not found; **Locate…** reconnects a bundle that moved, once it is
      shown to be the same run.
  - `--cache-dir` sets where derived data go.
- **Derived data, built once (wave 1, slice 4).**
  - **What is built** for each dataset of a project, on one grid of tiles shared with the
    morphology image's pyramid:
    - **image tiles** of every channel at every zoom level, decoded once from the bundle's JPEG
      2000;
    - **a spatial index of the transcripts:** each one in the tile holding it, grouped by gene,
      with per-gene density grids for every coarser level;
    - **cell and nucleus outlines** in tiles, exact at full resolution and simplified for each
      coarser level.
  - **When:** a dataset's derived data are built when it is added to a project, or when a project
    reopens and finds it again. Both development bundles take 2 to 4 seconds and 222 MB together.
  - **Built to be trusted and reused:**
    - entries are named for the contents of the files they come from, so a bundle that moves or
      is zipped keeps them and a changed one gets new ones;
    - an unchanged bundle reopens without being read;
    - an interrupted build resumes where it stopped, with the same result to the byte;
    - a crash never leaves a half-built entry.
  - **The cache is kept within a size,** `--cache-limit` (10 GB by default), by removing what was
    used least recently; it can be deleted at any time.
  - **Jobs:** long work runs in the program as a job with progress, a log and **Stop**. The page
    follows it live and finds it again after a reload.
  - **The Data view** shows each dataset's derived data (what is built, the size, a running
    build's progress, **Build now**, why the last build failed if it did) and what the cache
    holds; the status bar and inspector show a running build.
  - Sizes are now shown in decimal units (1 GB is 10^9 bytes), as `--cache-limit` reads them.
- **The tissue viewer (wave 1, slice 5).**
  - **The tissue at any zoom**, drawn with WebGL2 from the derived data:
    - the morphology channels, each with its own color, contrast window and gamma, every 16-bit
      sample exactly as recorded;
    - transcripts of chosen genes, up to 16 in distinct colors, or all transcripts: as points up
      close, and as each gene's density farther out;
    - cell and nucleus outlines, and cells filled by transcripts, area or segmentation method,
      with a legend.
  - **Finding your way:** pan and zoom by dragging, scrolling, pinching or the keyboard; a scale
    bar, the position in µm, and a minimap of the whole tissue.
  - **Finding genes** by typing their names, with each one's count.
  - **Clicking a cell** shows it in the inspector: its ID, position, areas, segmentation method,
    transcripts and top genes. Clicking one of its genes shows that gene.
  - **View the tissue** in the Data view opens a dataset once its derived data are built.
  - Tiles are served from the cache and kept by the browser, so a view you return to is drawn at
    once.
- **QC (wave 1, slice 6).** A QC view of whether the run supports analysis, and where it does
  not.
  - **Run metrics explained:** every metric the run recorded, with what it measures. For 20 of
    them, SituCraft's own value from the bundle's data is shown beside it; the rest are shown as
    recorded, with why.
  - **QC maps over the tissue:**
    - per cell: transcripts, genes, area, nucleus area, transcript density, transcripts in the
      nucleus, segmentation method and more;
    - per small region: the share of gene transcripts outside cells, the share below quality 20,
      and negative-control density.
  - **Plots:** a histogram, box plots by segmentation method and a scatter plot. Dragging across
    one chooses those cells, which the tissue shows (in the QC view and in the Viewer) and the
    inspector summarizes.
  - **Filters:** conditions on the cells, their effects counted as you edit them. They are saved
    as named versions in the project and compared, and never change the data. Each version
    records which cells it keeps and is checked to keep them again when the project reopens.
  - The derived data's format changed (to count the groups of transcripts the maps need), so
    datasets build their derived data once more.
- **Saved views, the record and examples (wave 1, slice 7).**
  - **Saved views:** **Save view** in the Viewer or the QC view keeps what it shows: the place and
    zoom (in µm, so the same tissue on any screen), the channels with their colors and windows,
    the genes, the layers and cell fill, the selected cell, and the QC map and filter in use.
    Channels and genes are kept by name, so a view survives its derived data being rebuilt. Views
    are listed in the project tree and the palette; each can be renamed, removed, or copied as a
    link that reopens it.
  - **Project archives (`.scz`):** **Project archive (.scz)…** in the project menu writes the project (datasets,
    filters, saved views, history; not the bundles) where you say, recording each bundle relative
    to the archive too, so that a folder holding both can be moved or shared. Opening an archive
    (by its path, or on the command line) finds each bundle beside it first; an archive whose
    project is already in the library opens as a copy.
  - **A methods paragraph** for each dataset (**Methods** in the Data view): the platform, panel
    and software, the segmentation, the transcripts counted, how SituCraft checked the bundle
    (with its version and commit), the filters chosen, and the data's source for public data,
    with references, ready to copy.
  - **Example datasets:** the start page offers 10x Genomics' Xenium human Lung and Breast 2-FOV
    bundles (CC BY 4.0), with their source, license and size. Nothing is downloaded until asked;
    a download runs as a job with progress and **Stop**, its archive checked by size, MD5 and
    SHA-256 and every file by size and SHA-256, then deleted. `--examples-dir` sets where they go
    (by default the `examples` folder of the data directory).
  - **Guided tours** of both examples: seven steps each through the Data view, the Viewer (the
    section, marker genes where they meet, segmentation methods) and QC (maps and filters).
  - **Documentation:** [Getting started](docs/GETTING-STARTED.md) and
    [the formats SituCraft reads and writes](docs/FORMATS.md).
- **The development data.** `node validation/fetch.mjs` downloads 10x Genomics' Xenium human Lung
  and Breast 2-FOV bundles (CC BY 4.0). It checks each archive's MD5, extracts it (each member's
  CRC-32 checked), checks every file's SHA-256, and deletes the archive. Both take about 640 MB;
  nothing is stored in the repository.
- **The project document** (`situcraft-project` version 1): datasets, artifacts, decisions, saved
  views and history. It is saved automatically to the library, and refused with a plain message
  when it is not a SituCraft project or was saved by a newer SituCraft.
- **The build commit** in the status bar and `/api/info`. Release builds embed it; other builds
  take Go's version-control stamp.
- **Installers** (`install.sh`, `install.ps1`) that check the download's SHA-256 and version.
- **CI and releases.**
  - CI tests each commit once (go vet, race tests, the six-platform cross-compile, the browser
    modules, validation, installer syntax). A pull request shows its branch's checks, and an
    identical merge is not tested again.
  - A release workflow builds the six binaries with their checksums once CI has passed on exactly
    the tagged files.

### Validation

- **Go tests of the host:**
  - security headers; foreign Host headers and cross-origin requests refused;
  - the project library's round trips; files stored under the hash the program computes; range
    requests;
  - bundles, Zarr stores and files found in folders in natural order, with a bundle's insides
    never listed, served or copied;
  - every web module embedded; the commit reported;
  - the port range clear of the siblings'.
- **`node --test` of the shared modules:** seeded random numbers, SHA-256, engine-independent
  logarithms and exponentials, color-vision simulation, the project document, and no
  engine-dependent math in modules that compute results.
- **`validation/run.mjs accessibility`:**
  - every text-on-surface pair of both themes reaches WCAG AA contrast, with color-vision-friendly
    colors off and on;
  - the palettes and status colors stay apart in protanopia, deuteranopia and tritanopia;
  - the diverging maps' two ends stay apart in every vision.
- **The readers on both development bundles** (`validation/readers_test.go`), against
  independent readers:
  - 60 Parquet columns over 2.9 million rows, identical to DuckDB 1.5's reading;
  - all 492 Zarr arrays, identical to zarr-python 3.4 with numcodecs;
  - the structure of 135 image levels, identical to tifffile's;
  - the smallest pyramid level of every image plane, pixel for pixel identical to tifffile with
    imagecodecs.
- **Bundle validation** (`internal/xenium`):
  - both development bundles validate with no fatal finding and no warning, and reproduce all 12 of
    their recorded metrics;
  - their notes are exactly the expected ones (polygons with crossing edges, the same counts GEOS
    finds; transcripts just past the images' edge; Breast cells without a nucleus);
  - the Lung bundle zipped validates as its folder does;
  - 13 broken bundles raise exactly the findings they should;
  - no file of a bundle changes when it is checked.
- **The derived data of both development bundles** (`internal/cache`):
  - every pixel of every image tile equals the bundle's own level, decoded directly;
  - every transcript is in exactly one tile, with its values; every gene's density grid sums to
    its count at every level;
  - the outlines at full resolution equal the bundle's, and every level holds each outline once,
    within its simplification tolerance;
  - builds stopped partway resume to byte-identical entries; an unchanged bundle reopens with
    nothing read or built; the zipped Lung bundle reuses the folder's entries;
  - both stay under the 500 MB budget, and every reader refuses damaged files cleanly (fuzzed).
- **QC** (`internal/xenium`, `internal/view`, `node validation/run.mjs viewer`):
  - 22 of the runs' recorded values reproduced from the data on both bundles (20 metrics of
    metrics_summary.csv, 2 of the manifest);
  - the QC maps' data, summed over the tissue, give the runs' share of gene transcripts in cells
    and of quality 20 or more exactly;
  - brushing chooses exactly the cells in range;
  - a saved filter keeps the same cells, under the same SHA-256, as an independent evaluation.
- **The tissue viewer in headless Chrome** (`node validation/run.mjs viewer`):
  - every channel of both bundles, drawn at full resolution, at half and at the coarsest level
    and read back before any window or color, equals the derived data sample for sample (1.35
    million samples);
  - the transcripts it loads for a region equal the program's query of the index;
  - it pans and zooms at 60 frames per second on an Apple M4, and a view it returns to is drawn
    at once.
- **The viewer's queries** (`internal/view`): a region's transcripts equal a scan of
  transcripts.parquet; every cell's gene transcripts counted from the index equal cells.parquet's
  count, for all 19,173 cells of both bundles.
- **JPEG 2000 against OpenJPEG on all 918 tiles of both bundles:**
  - the 270 lossless (5/3) tiles are identical;
  - the 648 lossy (9/7) tiles are within ±1 at every pixel (0.74% of samples differ by 1, RMSE
    0.086), which is how far two conformant decoders of a lossy stream may differ;
  - resolution reduction is within ±1 too.
  - A tile decodes in about 90 ms on one core (budget 150 ms), and every tile of both bundles in
    8.5 s on ten cores.
- **Fixtures written by independent tools** (the zstd and OpenJPEG tools, c-blosc, DuckDB, pyarrow,
  zarr-python, tifffile), committed with the scripts that made them, test each format's every
  variant without the data. Every decoder was fuzzed (about 100 million executions); the five
  crashers found, all from declared sizes a file did not back up, are fixed.
- **CI** checks the readers and the Lung bundle's tiles for each commit (`node
  validation/fetch.mjs xenium-lung-2fov`, OpenJPEG installed as the reference); a weekly workflow
  checks both bundles.
- **The record (wave 1, slice 7):**
  - a project saved by this release (`validation/projects/0.1.0-lung.situcraft.json`: the Lung
    dataset, two versions of a filter and three saved views) opens with every field kept; in
    headless Chrome its moved bundle is located again, its filters keep exactly the cells they
    recorded (by an evaluation in Node and Node's own SHA-256), and its three views reopen exactly,
    from the project and from a link;
  - every step of both tours opens with its genes, and each gene a tour names is in its panel;
  - an example downloads as a job, is checked and opened as a bundle, and is removed; a damaged
    archive, a wrong checksum, a member leaving its folder, too little free space and a stopped
    download each leave nothing behind (Go tests); downloading and removing are refused to other
    computers;
  - project archives keep their bundles when their folder moves (Go test);
  - the methods paragraph says only what was done, and the same inputs give the same text.
- **A release dry run** built the six binaries with `SHA256SUMS`. The macOS one:
  - printed `situcraft 0.1.0`;
  - served the embedded app with its commit in `/api/info`;
  - refused a foreign Host;
  - handed a file to a running SituCraft.
