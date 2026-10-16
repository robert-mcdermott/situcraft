# Getting started with SituCraft

SituCraft opens the output of a 10x Genomics Xenium run (an *output bundle*), checks it, and shows
the tissue, its transcripts and its cells at any zoom, with quality control linked to the tissue.
Everything runs on your computer; your data are never uploaded.

This guide takes you from installing SituCraft to a saved, reproducible view of your tissue.

## 1. Install and start it

Install SituCraft with the one-line installer ([Installing](INSTALLING.md)), then run:

```sh
situcraft
```

SituCraft opens in its own window: Chrome, Edge, Brave or Chromium in app mode if you have one,
else your default browser. The terminal shows where it keeps your projects, its cache and the
example datasets. Closing the window stops SituCraft (`--keep-running` keeps it serving).

The start page offers your recent projects, the example datasets and **Open a bundle**.

## 2. Try an example

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/start-dark.webp">
  <img alt="SituCraft's start page: recent projects, and the example datasets with their size, source and license and their worked examples, each with a Download or Make it button" src="images/start-light.webp">
</picture>

Under **Examples**, two public Xenium bundles from 10x Genomics (CC BY 4.0) are ready to download,
and a simulated tissue is ready to make:

| Example | Download | On disk | What it shows |
| --- | --- | --- | --- |
| Xenium V1 human Lung, 2 FOVs | 278 MB | 284 MB | 11,898 cells, 825,885 transcripts, 289 genes, a DAPI image, nucleus-expansion segmentation |
| Xenium V1 human Breast, 2 FOVs | 379 MB | 385 MB | 7,275 cells, 1,113,950 transcripts, 280 genes, four stains, multimodal cell segmentation |
| A simulated tissue with known truth | none: made here in seconds | 34 MB | 3,030 cells of four types, 436,996 transcripts, 120 genes, a DAPI image, nucleus-expansion segmentation, and the truth: every cell's and transcript's |

**Download** fetches one from 10x Genomics, checks the archive and every file against recorded
checksums, keeps only the bundle and deletes the archive. Nothing is downloaded until you ask.
**Make it** simulates the third with SituCraft's standard settings (`situcraft simulate` makes
the same tissue, byte for byte); it is labelled simulated wherever it is shown.
When a 10x Genomics example is ready, **Take the tour**: seven steps through the Data view, the Viewer and QC. Use
**Next** and **Back** on the card at the foot of the window; you can explore at any step, and the
tour continues from there.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/tour-dark.webp">
  <img alt="The Lung example's guided tour at its third step, a card over the Viewer saying what the four genes mark" src="images/tour-light.webp">
</picture>

Each example is used under its license; its source and license are linked on its card, and the
methods paragraph (section 7) cites it.

Each example also has a **worked example**: a question answered step by step, saying first what to
expect. Some steps run a job before going on (the card's button says what it does: expand the
nuclei, plant errors, refine, import the truth, compare), and others open the comparison in the
Compare workspace or the Viewer with the view, the ranking and the difference the step talks
about.

| Example | Worked example | The question |
| --- | --- | --- |
| Breast | The instrument's cells against a nucleus expansion | Where does growing each nucleus by 5 µm disagree with the cells the instrument drew from its stains, and which does the evidence favour there? |
| Breast | Redraw the cells from their transcripts | How does SituCraft's transcript-informed segmentation redraw the instrument's cells, and where is it unsure of itself? |
| Lung | Find the planted errors (simulated) | Can you find a segmentation's errors with the comparison workspace, and which ranking finds which kind? |
| Simulated | Score segmentations against the truth | When every cell is known, how close do the instrument's nucleus expansion and SituCraft's transcript-informed segmentation come to the true cells? |

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/worked-dark.webp">
  <img alt="The Lung example's worked example in the Compare workspace: the instrument's segmentation and one with planted errors side by side, a planted merge chosen, and the worked example's card beside the queue" src="images/worked-light.webp">
</picture>

## 3. Open your own bundle

A Xenium output bundle is the folder the instrument or Xenium Ranger wrote, holding
`experiment.xenium`, `transcripts.parquet`, `cells.parquet`, the boundaries, the cell-feature
matrix and the morphology images. SituCraft opens it as:

- its folder, its `experiment.xenium` file, or a `.zip` of it;
- on the command line, `situcraft /path/to/output-XETG00000__0000000__Region_1__20240101__000000`
  (several bundles, or a folder of them, at once);
- or **Open a bundle** in the window (⌘O or Ctrl+O), typing or pasting its path.

SituCraft reads a bundle where it is, and never changes, moves or copies it. Running `situcraft
<paths>` while SituCraft is open hands them to the open window.

## 4. Check it: the Data view

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/data-dark.webp">
  <img alt="The Data view of the Lung example: the run's facts, its derived data built, and the check's three notes" src="images/data-light.webp">
</picture>

The Data view says what the bundle is (instrument software and version, panel, tissue, cells,
transcripts, channels) and how far SituCraft supports it: Xenium Onboard Analysis 2.x fully,
other versions best-effort, said so.

It then checks the bundle. Each finding is:

- **fatal:** the bundle cannot be used until it is fixed (a missing or unreadable file);
- **a warning:** usable, but it may bias an analysis;
- **a note:** worth knowing, nothing to fix.

Each finding names its files, how many records it concerns, where in the tissue, and what to do.
SituCraft also recomputes 20 of the run's recorded metrics and 2 values of its manifest from the
data and says whether they agree.

**Add to project** records a bundle with no fatal finding in the current project. SituCraft then
builds its *derived data* once (image tiles at every zoom, a spatial index of the transcripts, tiled
cell outlines): a few seconds for the examples, shown with its progress. They are kept in the cache
and reused whenever the bundle is unchanged; they can be deleted at any time and are rebuilt when
needed. **View the tissue** opens the Viewer.

## 5. See the tissue: the Viewer

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/viewer-genes-dark.webp">
  <img alt="The Viewer: transcripts of EPCAM, COL5A2, PLVAP and CD68 over the gray DAPI stain and the cell outlines, a selected cell's transcripts by gene in the inspector" src="images/viewer-genes-light.webp">
</picture>

- **Move:** drag to pan; scroll, pinch, **+** and **−**, or double-click to zoom; **0** shows the
  whole tissue; the minimap (top right) jumps to a place. The scale bar and position are in µm.
- **Image:** each morphology stain with its color, brightness window (**Low**, **High**, or
  **Auto**) and gamma; the eye shows or hides it.
- **Transcripts:** type a gene's name to show its transcripts (up to 16 genes, each in its color),
  or show all transcripts. Up close each transcript is a dot; farther out, each gene's density.
  **Quality 20 or more only** keeps the transcripts Xenium counts.
- **Cells:** cell and nucleus outlines (drawn once cells are large enough to see), and cells filled
  by their transcripts, area or segmentation method, with a legend.
- **Segmentation:** any of the dataset's segmentations, chosen in the Cells layer (its default
  first); the legend says how it was made. A transcript-informed one also fills its cells by the
  limitations it flagged, its boundary certainty or its assignment certainty, and colors its
  transcripts, close up, by their probability of their cell: doubtful ones bright, certain ones
  dark. **Show on the tissue**, beside a segmentation in the Segmentation view or the inspector,
  opens it here.
- **A cell:** click it to see its ID, position, areas, segmentation method and genes in the
  inspector (right); **Esc** clears it. In another segmentation than the instrument's, the
  inspector also says which instrument's cell it came from and how sure the segmentation is of
  it.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/refined-dark.webp">
  <img alt="The Viewer drawing the Lung example's transcript-informed segmentation: cells filled by their boundary certainty, transcripts colored by their probability of their cell, a refined cell described in the inspector" src="images/refined-light.webp">
</picture>

**Save view** keeps exactly what the Viewer shows: the place and zoom, the stains and their
settings, the genes, the layers, the segmentation and the selected cell. Saved views are listed in the project tree
(left) and the command palette (⌘K or Ctrl+K). Select one to reopen it; in the inspector, **Copy
link** gives a link that reopens it, **Rename** and **Remove** change it.

## 6. Check quality: the QC view

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/qc-dark.webp">
  <img alt="The QC view: the run's metrics with SituCraft's recomputed values, cells colored by their gene transcripts, and a histogram" src="images/qc-light.webp">
</picture>

- **Run metrics:** every metric the run recorded, explained, with SituCraft's own value beside the
  recorded one where it can be recomputed (**Reproduced**), or why not.
- **Maps over the tissue:** cells colored by an attribute (gene transcripts, genes detected,
  area, negative controls, segmentation method and more), or small regions colored by the share of
  transcripts outside cells, the share below quality 20, or negative-control density.
- **Plots:** a histogram, box plots by segmentation method, and a scatter plot. Drag across a plot
  to choose cells; they light up in the tissue (here and in the Viewer) and the inspector compares
  them with all cells.
- **Filters:** conditions on cells (for example, gene transcripts at least 20 and area 15 to 400
  µm²), each condition's effect counted as you edit it. **Save version** keeps the filter as a numbered
  version in the project, with the count and a checksum of the cells it keeps; it is checked to
  keep the same cells whenever the project reopens (**Reproduced**). Filters never change the data.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/qc-filter-dark.webp">
  <img alt="The QC view's Filters tab: a filter's two conditions with their effects, and two saved versions, each reproduced" src="images/qc-filter-light.webp">
</picture>

**Save view** in the QC view keeps the map, the filter in use and the place.

## 7. Keep and share the record

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/saved-views-dark.webp">
  <img alt="Three saved views in the project tree, one open in the Viewer, its details and Copy link in the inspector" src="images/saved-views-light.webp">
</picture>

- **Projects save themselves** to the library as you work; the start page and the project menu
  (the project's name, top left) reopen them. **Undo** and **Redo** (⌘Z, ⇧⌘Z) cover every change,
  and the drawer (⌘J) shows the project's history and the session's log.
- **A bundle that moved:** reopening a project finds each bundle where it was recorded. If it is
  gone, **Locate…** in the Data view points to its new place, once SituCraft has checked it is the
  same run.
- **Methods:** **Methods** in the Data view writes a paragraph for the dataset from what the project
  records: the platform, panel and software, the segmentation, the transcripts counted, how
  SituCraft checked the bundle (with its version and commit), the filters you choose, and the data's
  source, with references. **Copy** puts it on the clipboard; read it against what you did and add
  what was done elsewhere.
- **Project archives:** **Project archive (.scz)…** in the project menu writes the project
  (datasets, filters, saved views, history) to a file you name. It does not contain the bundles:
  it records each one relative to the archive, so a folder holding the archive and its bundles can
  be moved, copied or shared, and opened with `situcraft project.scz` or **Open a bundle**.

## Segmentations and jobs

When a dataset is added, SituCraft registers the instrument's segmentation as an artifact: the
cells, their boundaries, the nuclei, the transcripts' assignments and the counts, each file recorded
by its SHA-256 (it stays in the bundle). Under **Segmentations** in the project tree, the default
one is marked; the inspector shows how it was made (the methods and how many cells each found), its
files, what its check found, and why it is the default.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/expansion-dark.webp">
  <img alt="The Segmentation view of the Lung example: its nuclei expanded by 5 µm, compared with the instrument's segmentation, 98.06% of pixels and 98.94% of transcripts in the same cells, with what differs described" src="images/expansion-light.webp">
</picture>

The **Segmentation** view lists the dataset's segmentations side by side, with their cells, the
share of gene transcripts each assigns, transcripts per cell and cell area. **Expand the nuclei**
grows each nucleus by the distance you choose (5 µm, Xenium's default, to start) into a cell of its
own: every pixel goes to the nearest nucleus within the distance, and transcripts to the cell
holding the pixel they lie in. It runs as a job (about a second for the Lung example) and becomes a
segmentation of its own, kept with its parameters. Below, it is compared with the instrument's
segmentation pixel by pixel and transcript by transcript, and what differs is described: on the
Lung example, segmented by the instrument the same way, 98.06% of pixels and 98.94% of transcripts
are in the same cells.

**Transcript-informed segmentation** redraws each cell from the transcripts (SituCraft's
implementation, after Jones et al. 2025). It starts from the instrument's cells and their nuclei.
Each boundary moves to where the cell's own transcripts are, and each transcript may have drifted
from the cell it came from. Its studio takes you through six steps, each showing what the run
would do on this bundle and this computer:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/studio-dark.webp">
  <img alt="The segmentation studio on the Breast example at its Compute step: the threads, and the memory, disk and time the Balanced refinement needs against what the computer has" src="images/studio-light.webp">
</picture>

1. **Input:** the transcripts' quality (Q20 to start), the cells it starts from, and what it
   covers: the whole tissue, its separate sections apart, or chosen regions (typed in µm, or the
   region last framed in the Viewer). Outside a region the instrument's cells stay as they are.
2. **Compute:** the threads, and the memory, disk and time it needs against what the computer
   has.
3. **Model:** a preset (Balanced, Conservative boundary, Flexible morphology, Low memory, ROI pilot,
   Expert), what it changes and what it risks, and every setting with what it does and costs.
4. **Review:** what will run, anything that stops it (with what to do) and its risks; **Refine**
   starts it, and **Save the recipe** keeps its settings as a file, to run again here or with
   `situcraft refine` in a terminal.
5. **Run:** its progress and log, and **Stop**. A failure says what went wrong and what to do.
6. **Compare:** whether it settled, which cells its result is least sure of (few transcripts,
   uncertain boundaries, no nucleus), and, for an ROI pilot, how often a second seed put the
   transcripts in the same cells. **Review the differences** opens the comparison workspace on it
   against the instrument's segmentation, made as soon as the refinement is.

The ROI pilot is the place to start on a new tissue: a region of a few hundred µm takes seconds,
and its two seeds show how much of the result is chance. The result itself is the summary of many
samples from the model: each transcript in its most probable cell, with that probability kept.

A large tissue (more than about 4 million transcripts modelled) is refined in patches of about 2
million transcripts each, one after another, each also refining the cells within 40 µm of its
square, and stitched into one segmentation; the Compute step says how many. The result is the same
for the same seed whatever the computer. The memory still grows with the tissue, by about 100
bytes a transcript: a tissue of 55 million transcripts needed 5 to 6 GB.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/import-dark.webp">
  <img alt="The Segmentation view of the Lung example: the instrument's segmentation and one imported from a SpatialData store written by spatialdata-io, side by side, and the imported one's lineage in the inspector" src="images/import-light.webp">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/comparison-dark.webp">
  <img alt="The Segmentation view of the Breast example comparing the instrument's segmentation with a 5 µm nucleus expansion: the differences by class, each segmentation's measures, and the pixels and transcripts that changed" src="images/comparison-light.webp">
</picture>

**Compare two segmentations** chooses an A and a B and shows which cells correspond and how each
differs: one-to-one, expanded or contracted (by more than 25% of its area), split, merged,
rearranged, or only in one. Two cells correspond when they share a nucleus, or when at least half
of one lies within the other. Beside each segmentation's own numbers (cells, cells with a nucleus,
assigned transcripts, transcripts per cell, areas), two measure transcripts given to the wrong
cell: the spurious co-expression of gene pairs relative to nuclei alone (Jones et al. 2025; 1 is
as in nuclei), and how often markers of different lineages are found in the same cell (the
mutually exclusive co-expression rate of Hartman and Satija). Between the two, the pixels that
agree, the transcripts that changed cell, and how far the boundaries moved. The comparison runs
as a job and is kept with the project. One you no longer need can be removed from the project
(**Remove** in the inspector): the two segmentations stay, and it can be made again at any time.
A comparison a decision rests on stays, since the methods paragraph tells how the default was
chosen from it. A comparison made by an earlier SituCraft whose list of differences this version
does not read says so, and offers to compare the two segmentations again.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/workspace-dark.webp">
  <img alt="The Compare workspace: the Breast example's instrument segmentation and a 5 µm nucleus expansion side by side on the tissue, cells filled by the class of their difference, the review queue's first split chosen and described in the inspector" src="images/workspace-light.webp">
</picture>

**Open** (beside a comparison, or **Compare** in the views) shows the two on the tissue:

- **Side by side** with one camera, **Overlay** (B's outlines over A's, each in its colour), or
  **Flicker** (A and B in turns). Cells are filled by the class of their difference; the
  **Disagreement map** shades each 50 µm region by the share of its pixels the two give to
  different cells.
- **The review queue** ranks the differences by reassigned transcripts, contamination (pairs of
  markers of different lineages found in one cell of B and not of A), disagreement or low counts,
  among the classes you keep and with at least the reassigned transcripts you set. **Next** and
  **Previous** (or `]` and `[`) go through it; each difference opens where it lies, its cells lit
  in both, and is described in the inspector. **Cell matching** lists every difference, sortable.
- **Labels** reads a label set of each segmentation through the comparison: what the choice
  changes in the cells' labels (below, Labels under two segmentations).
- **Decide** promotes one segmentation as the dataset's default, keeps one for exploration, or
  rejects one, with a reason. Decisions are appended to the project with the comparison they rest
  on and never removed; the default cannot be rejected until another is promoted, and no
  segmentation is ever deleted.

The measures' header says when one rests on little (few nuclei for the spurious co-expression,
few marker pairs in the panel, cells without outlines).

**Import a segmentation** brings in one made by another tool: a SpatialData Zarr store, Baysor's
`segmentation.csv` with its polygons, GeoJSON polygons with an optional transcript table (CSV or
Parquet), or a Xenium Ranger output bundle of the same run. Say whether its coordinates are in µm
or in pixels of the morphology image (stores written by spatialdata-io from Xenium data are in
pixels): SituCraft does not guess, and the import's check says when the cells do not lie within
the dataset's image. It also checks that the transcripts it names are the dataset's. The import
runs as a job and becomes a segmentation of its own, kept with the SHA-256 of every file it read,
its transcripts counted as every segmentation's are; one whose check finds a fatal problem is
kept as invalid, for review.

**Practice: find planted errors**, on the example datasets, makes a copy of the instrument's
segmentation with known errors in it: neighbouring cells of different lineages merged, large cells
split in two, cells missed, and cells drawn too wide. Compare it with the instrument's, review the
differences in the workspace, then open **The answer key** to see what was planted where (**Show**
takes you to each). The copy is simulated, named so, and is never promoted or exported. On the Lung
example, the comparison lists exactly the 60 planted errors, each in its class, and ranked by
contamination the merges come first: their cells express markers of two lineages together. To
practise on a dataset of your own, turn on **Practice tools for every dataset** in the menu of the
theme button (top right); it is off unless you do.

### Export a segmentation

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/export-dark.webp">
  <img alt="The Segmentation view's Export a segmentation: the formats chosen, the folder, and an export of the Lung example's instrument segmentation listed with its files and the prepared Xenium Ranger command" src="images/export-light.webp">
</picture>

**Export a segmentation** writes any of a dataset's segmentations for other tools, into one folder
you name:

| Format | For | Holds |
| --- | --- | --- |
| SpatialData Zarr | spatialdata, napari-spatialdata | The cells as a label image and as polygons, the transcripts as points with their cells, the counts as an AnnData table |
| AnnData Zarr | scanpy, anndata | The counts, with each cell's measures and centroid |
| Parquet; CSV | DuckDB, pandas, R, polars | Cells, transcripts with their cells, outlines and counts, in Xenium's column names |
| Matrix Market | Seurat (`Read10X`), scanpy | The counts as Xenium and Cell Ranger write them |
| GeoJSON | QuPath, GIS tools | Each cell's outline as a polygon naming it |
| Xenium Ranger inputs | Xenium Ranger's `import-segmentation` | The cells and nuclei as label masks on the morphology image's pixels, and the command |

Coordinates are in µm. Beside the files, `situcraft-export.json` says what was exported, from what
and by which SituCraft, lists every file by its SHA-256, and gives fingerprints of the label image,
the counts and the transcripts' cells that every format holds the same (`docs/FORMATS.md`), so that
you can check any reader got it exactly. The export runs as a job, is written under a temporary
name and renamed into place when complete, and is recorded in the project. A folder that exists is
replaced only when you ask, and only when it is an earlier SituCraft export.

For **Xenium Ranger**, SituCraft prepares the inputs and the command, and does not run it. It
checks first that the masks are the morphology image's size, which cells have no pixel (Xenium
Ranger cannot keep them), and how many transcripts this segmentation assigns otherwise than its
mask holds at their pixel (Xenium Ranger assigns by the mask). **Copy the command** puts it on the
clipboard to run where Xenium Ranger is installed.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/jobs-dark.webp">
  <img alt="The Jobs view: four finished jobs, building derived data and registering segmentations of both examples, one with its log open" src="images/jobs-light.webp">
</picture>

Long work runs as a job: building derived data, downloading an example, registering a
segmentation, expanding nuclei, importing, comparing or exporting segmentations, planting errors,
clustering cells, mapping them, adding the instrument's clusters.
The **Jobs** view shows what runs and waits, what finished, and each job's log and spec. Jobs keep
running when the window reloads, and closing the window while one runs asks first. If SituCraft
stops (or the computer does) in the middle of a job, the job is shown as interrupted
when SituCraft starts again: **Resume** continues it where it stopped, **Discard** forgets it.

## Cell types: clusters

The **Cell types** view groups a segmentation's cells by their gene expression, the first step
toward naming them. Choose the segmentation (the default to start) and the cells to analyze: those
with at least 10 gene transcripts, or, for the instrument's segmentation, the cells one of your QC
filters keeps. **Cluster the cells** runs the common single-cell workflow as a job (about a second
for the examples):

1. each cell's counts scaled to the median total and log-transformed;
2. each gene scaled, so that every gene counts alike;
3. principal components (50), computed exactly;
4. each cell's 15 nearest neighbors in them, as a graph;
5. Leiden communities of the graph: the clusters, numbered from the largest.

The same steps serve a whole section: above 50,000 cells the nearest neighbors are found by
NN-Descent rather than by comparing every pair (the clustering says how many of the exact
neighbors it found, measured on 1,000 of its cells: 99.9% on the examples), the map is laid out
over every core, and for panels of more than 1,000 genes (Xenium Prime) the components are found by
the Lanczos method, the full decomposition's to rounding.

**Parameters** shows each setting with what it does. They are scanpy's defaults, so the numbers
match what the scanpy tutorials give. **Resolution** is the one you will change most: higher gives
more and smaller clusters. There is no right value; make a few and compare their markers. Each
clustering is kept as an artifact of its segmentation with every parameter and its seed, and the
same seed gives the same clusters on any computer. Asking for one already made answers it at once.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/celltypes-dark.webp">
  <img alt="The Cell types view of the Breast example: 15 clusters of 7,163 cells, each with its size and leading markers (TOP2A and MKI67 for dividing cells, CD68 for macrophages, LUM for fibroblasts, CD3E for T cells), the components' share of the variance, and the first cluster's markers against the other cells" src="images/celltypes-light.webp">
</picture>

Under each clustering are its numbers (the cells analyzed and left out, the components' share of
the variance, the graph and the modularity Leiden reached) and its clusters, each with its size
and leading markers. Choose a cluster to see its markers against all the other cells: how much
higher its expression is (log₂ fold change), the share of cells expressing the gene in and out of
it, and the rank-sum test's score and adjusted p-value. These are exploratory: the cells of one
tissue are not independent replicates, so the p-values rank genes rather than test them.

**Show in the Viewer** opens the Viewer with the cells filled by cluster and a legend. The Viewer's
**Fill by** lists every clustering of the segmentation drawn, and a saved view keeps the one shown.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/clusters-dark.webp">
  <img alt="The Breast example in the Viewer, its cells filled by their cluster: tumour nests and stroma in different clusters, with the clusters' legend" src="images/clusters-light.webp">
</picture>

### The map and the tissue

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/cellmap-dark.webp">
  <img alt="The Cell types view of the Breast example: the map of the cells (UMAP) beside the tissue, colored by cluster with some clusters named, the T cells selected on both, the selection's composition below" src="images/cellmap-light.webp">
</picture>

Each clustering comes with a **map** (UMAP, made at once, a few seconds for the examples): cells
with similar expression lie near each other. It sits beside the tissue, and the two are linked:
- **Color by** a clustering's clusters (any clustering of the segmentation), a gene (type its
  name), the cells' gene transcripts or area, or how faithfully the map keeps each region's
  neighbors.
- **Select cells** on either side with the **Lasso** (shift adds to the selection), or by clicking
  a cluster in the legend or the table: the cells are selected on both, the others faint. Click a
  cell on the map to find it on the tissue, or on the tissue to mark it on the map.
- **Markers of the selection** compares the selected cells with the clustering's other cells, as the
  clusters' markers are computed: what distinguishes a region you drew.

A map shows which cells are alike, not how alike: distances between groups, and their sizes, mean
little. Under the map, SituCraft says how far this one can be trusted, from its trustworthiness and
how many of each cell's nearest neighbors in expression stay near it on the map.

**Name** clusters in the table (a T-cell cluster, a tumour cluster): names show on the map, the
tissue and the legends, and giving several clusters the same name merges them. Each change is a
new version of the names, kept in the project's history; the clusters keep their numbers. The
**dot plot** shows each cluster's two leading markers across all clusters: dot size, the share of
cells expressing the gene; color, its mean expression.

**The instrument's clusters:** the bundle holds the instrument's own analysis (graph-based clusters,
and k-means for 2 to 10 clusters, with its own map). **Add** one to explore it here like any
clustering (SituCraft computes its markers), and the instrument's map comes with the first.
**Two clusterings compared** cross-tabulates any two clusterings of the same cells, with the
adjusted Rand index (1: the same partition; about 0: as alike as chance).

## Cell types: labels from markers

Under a clustering, **Labels from markers** labels each cell from the genes known to mark cell
types. A **marker set** is a hierarchy of labels (a class such as Immune, a lineage such as T cell,
a subtype such as CD8 T cell) and states (Proliferating), each with **positive** markers, the genes
its cells express, and **negative** ones, genes they do not. SituCraft offers a starter set for
each example's panel, from published breast and lung cell atlases (the **Sources**), every marker
in the panel.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/labels-dark.webp">
  <img alt="The Cell types view of the Breast example labeled from the breast starter set: each cluster's labels at the lineage level as colored bars (the tumor clusters Epithelial and Luminal, with some cells left without a label; the macrophages Myeloid, the fibroblasts Fibroblast, the T cells T cell, the vessels Endothelial), names for the clusters from them, and the labels with their markers" src="images/labels-light.webp">
</picture>

**Label the cells** scores each cell for each label: its markers' expression less that of control
genes of similar average expression (as scanpy's `score_genes` scores a gene list), its negative
markers counting against it where the cell expresses them. Labels are then taken from the top
down: a cell takes the best class when it leads the next by the **margin**, then the best of that
class's lineages, and so on; where no label leads, the cell stays at the level above, so a T cell
whose CD4 and CD8 markers are both faint stays a T cell. Each cell's **confidence** is how far its
label led. A label set is kept as an artifact with the marker set it came from and every
parameter; asking again answers it.

Under the labels:
- **Each cluster's labels** at the level chosen (Class, Lineage, Subtype), as bars. A cluster split
  between labels may hold two kinds of cells, or cells whose outlines hold parts of two.
- **Name the clusters** by the most specific label at least half (or a share you choose) of each
  cluster's cells carry: a new version of the names, which you can edit. Clusters given the same
  name merge on the map and the tissue.
- **Color the map and the tissue** by the labels, their confidence, or one label's score (choose it
  in the labels' table, which lists each label's markers and control genes). The Viewer's **Fill
  by** lists each label set's levels.

**Edit** the marker set to add labels, levels or states, or change markers (the panel's genes are
offered as you type, and those it lacks are marked). Saving makes a new version, kept with the
earlier ones and the labels made from them. **Save as a file** writes the set as JSON, and **Read
a file…** reads one (`docs/FORMATS.md`); a set naming genes the panel lacks offers to remove them.

Starter sets are a starting point. Markers that hold in an atlas may not in your tissue (in the
Breast example the tumor cells express LPL, an adipocyte marker), and in a tissue one type
dominates, its own markers score low against control genes that are mostly its other genes, so
many of its cells may stay without a label. Check each cluster's labels against its markers before
naming it.

### Labels from a reference

When a labeled single-cell dataset of the same tissue exists (an atlas, or your own single-cell
experiment), **Labels from a reference** labels each cell by the reference label it most resembles.
Name the reference (its path on this computer) and **Read** it: an AnnData object saved with
`write_zarr` (a `.zarr` folder, or a zip of one), whose cells carry their labels in an `obs` column,
or a table of each label's mean expression (CSV, TSV or Parquet, a row per label or per gene). An
`.h5ad` file is written as Zarr in one line of Python:
`anndata.read_h5ad("reference.h5ad").write_zarr("reference.zarr")`.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/reference-dark.webp">
  <img alt="The simulated example's reference read against its panel: an AnnData object of 1,200 cells, its counts in X, its labels in obs cell_type (four types of 300 cells each), all 120 of the panel's genes in the reference and compared; the button to label the cells from it" src="images/reference-light.webp">
</picture>

SituCraft suggests the reference's expression (counts preferred), its column of labels and its
column of gene names, and says what it found: its labels and their cells, how many of the panel's
genes it has (and which it lacks: if it has few, check that its genes are named by symbol, and the
species), and how many will be compared. **Label the cells from the reference** then correlates
each cell with each label's profile (its reference cells' mean expression) over the genes that set
the labels apart, and scores the leading labels again on their own genes until one is left (as
SingleR does, Aran et al. 2019). Each cell's **confidence** is how far its label led the next, in
correlation. The label set is kept with the reference as it was compared and its SHA-256.

The labels follow the reference: a reference of other cells (an atlas of healthy tissue for a
tumor, say) gives each cell the nearest label it has, which may be wrong. The evidence card (below)
shows, for each cell, how close the leading labels were and which genes set its label apart from
the next. To try it, `situcraft simulate --reference 300 <folder>` writes a simulated tissue and a
reference of its types beside it.

### Labels made elsewhere

Labels from another tool or an earlier analysis (a colleague's annotation, a reference mapping,
the instrument's clusters) are imported under **Labels made elsewhere**, for the segmentation's
cells. Name the table (its path on this computer) and **Read** it: a CSV or TSV file, a Parquet
file, an AnnData object saved with `write_zarr` (or a SpatialData store), or Xenium Explorer's cell
groups. SituCraft lists the columns and suggests which holds the cells' ids, which their labels,
and, optionally, a confidence and colors; change them if they are wrong. Before you import, it
says how the ids match: a table naming cells this segmentation does not have is refused (its labels
were made for another segmentation, or another sample), and the cells it does not name will have
no label. **Or the instrument's clusters** imports one of the instrument's clusterings as labels.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/imported-dark.webp">
  <img alt="The Lung example's graph-based clusters from the instrument imported as labels: each of SituCraft's clusters by the instrument's clusters, the tumor clusters spread across many of them, the vessel and stroma clusters mostly one (80% to 85%)" src="images/imported-light.webp">
</picture>

An imported label set is shown like one from markers: each cluster's labels (here, how SituCraft's
clusters of the Lung example split among the instrument's), names for the clusters from them, the
map, the tissue and the Viewer colored by them, and a label's cells selected when you choose it.
Its file is recorded by its SHA-256, so the project says exactly what was imported.

### Reviewing labels

Labels are predictions. Under **Label versions** each label set of the segmentation is listed:
from markers or a reference, imported, curated and consensus. **Curate** begins a curated set from a predicted one:
its labels, with your changes appended, each with its reason, who made it and when (the predicted
set stays as the method made it). **Make a consensus…** combines sets by a rule: unanimous (every
set gives the label), majority (at least so many sets, more than any other label) or in order (the
first set's label, else the next's); labels the sets name differently can be mapped to a common
name. Any two versions are compared **side by side** in a table of their labels.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/review-dark.webp">
  <img alt="The review of the Breast example's labels: a cell in a tumor cluster labeled Stromal though all ten of its nearest cells are Epithelial, with its evidence: Stromal led Immune by 0.12, its markers in the cell (PDGFRB 2, SFRP4 2, and EPCAM 9 counted against it), why not Epithelial (its markers 0.50 against their controls' 0.75), its neighbors' labels, and the buttons to accept or relabel it" src="images/review-light.webp">
</picture>

**Review** ranks the cells to look at first: those whose label few of their nearest cells in the
tissue share while most share another (a lone label among others is often a mislabel, two cells in
one outline, or a cell at a boundary), those whose label changes under another segmentation (choose
the label set of it in **Label changes against**; below), and those of low confidence. Choose a
cell, in the list or on the map or the tissue, to see its **evidence**: its label in every version;
for labels from markers, why it took its label and not the next, gene by gene in the cell (green, a
marker it expresses; red, a negative marker it expresses), and why not the label its neighbors
carry; for labels from a reference, each round's correlations and the genes higher in its label's
profile and in the next's that the cell expresses; what its counterparts carry under the other
segmentation; its neighbors' labels in the tissue and in expression; its transcripts and its
segmentation's flags. **Accept** its label or **Relabel** it, saying why; the decision is added to
a curated set and the next cell is shown.

### Labels under two segmentations

Cell types are read from cells, so the segmentation chosen can change them: a cell drawn too wide
takes its neighbor's transcripts and may take its neighbor's label; a cell split in two may become
two kinds. To see how much, compare two segmentations (Segmentation view), then, under a
clustering's label sets, **Make this clustering and its label sets again on** the other
segmentation: the clustering with the same parameters, its map, and each label set from markers or
a reference, in one step (imported labels belong to their segmentation's cells and are not made
again; a QC filter's cells are the instrument segmentation's, so the cells are then those with
enough transcripts).

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/impact-dark.webp">
  <img alt="The Compare workspace's Labels tab for the Breast example: the instrument's segmentation and a 5 µm nucleus expansion side by side, each cell colored by its label (Epithelial, Immune, Endothelial, Stromal, Adipocyte); of the 6,512 cells matched one to one, 96.9% labeled alike; what split and merged cells carry; marker mixing slightly higher under the expansion" src="images/impact-light.webp">
</picture>

The comparison's **Labels** tab (Compare workspace) reads a label set of each segmentation through
the comparison (by default two made the same way, at their first level):
- **How many cells keep their label:** of the cells matched one to one, the share labeled alike,
  and every change (T cell → Macrophage, and so on); what split cells' pieces carry (the cell's
  label, some of them, or none) and what merged cells carried.
- **Each label's cells** under each segmentation, and the change in its share.
- **Marker mixing by label:** of each label's cells, the share expressing two mutually exclusive
  markers together, and the mutually exclusive co-expression rate among them. Under a segmentation
  that gives cells their neighbors' transcripts, it rises.
- **The differences whose labels change,** the most transcripts reassigned first; each opens where
  it lies. **Color the cells by their labels** fills each side's cells with its labels' colors.

The cells whose label changes are segmentation-sensitive: the review queue lists them when you
choose the other segmentation's label set in **Label changes against**, with what their
counterparts carry.

## Keyboard

| Action | Keys |
| --- | --- |
| Search and commands | ⌘K |
| Open a bundle; a saved project | ⌘O; ⇧⌘O |
| Save now | ⌘S |
| Undo; redo | ⌘Z; ⇧⌘Z |
| Go to a view | ⌘1 … ⌘9 |
| Hide the project tree; the inspector | ⌘\\; ⇧⌘\\ |
| History and log | ⌘J |
| Viewer: pan; zoom; whole tissue; clear the cell | arrows; + and −; 0; Esc |
| All shortcuts | ? |

On Windows and Linux, use Ctrl for ⌘.

## Where things are kept

| What | Where (macOS; Linux; Windows) | Flag |
| --- | --- | --- |
| Projects | `~/Library/Application Support/SituCraft`; `~/.config/SituCraft`; `%AppData%\SituCraft` | `--data-dir` |
| Example datasets | the `examples` folder of the above | `--examples-dir` |
| Derived data (can be deleted) | `~/Library/Caches/situcraft`; `~/.cache/situcraft`; `%LocalAppData%\situcraft` | `--cache-dir`, `--cache-limit` |

SituCraft listens only on this computer (127.0.0.1) and sends nothing anywhere. Its only request
to the internet is an example's download, when you ask for it.

## If something goes wrong

- **The window does not open:** `situcraft --window browser` uses your default browser instead;
  `--window none` prints the address to open yourself.
- **"Start SituCraft with its program":** the page was opened without SituCraft running; run
  `situcraft`.
- **The Viewer needs WebGL2,** which current Chrome, Edge, Firefox and Safari have.
- **A bundle will not open:** the Data view lists each fatal finding with its remedy.

SituCraft is for research use only. It is not a medical device and makes no diagnostic claims.
