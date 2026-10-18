# Remote control

A program on your computer (a script, a notebook, the documentation's screenshot capture) can
drive the open SituCraft window: open a bundle, show the tissue with chosen genes, save views and
filters, read the run's metrics, write the methods. The window performs each action where you see
it, and what an action changes goes into the project's history like any edit, so you can undo it.

## Starting it

```sh
situcraft --remote-control
```

SituCraft prints where scripts send actions, and writes `remote.json` to its data folder
(`~/Library/Application Support/SituCraft` on macOS, `~/.config/SituCraft` on Linux,
`%AppData%\SituCraft` on Windows, or `--data-dir`):

```json
{ "url": "http://127.0.0.1:8840", "token": "…", "version": "0.2.0", "pid": 12345 }
```

The file is readable by you alone and removed when SituCraft stops. A SituCraft started with
`--remote-control` takes its own port rather than handing its paths to one already running, so
that scripts reach a window they can drive.

## Sending an action

POST `{"action": …, "args": {…}, "client": …}` to `/api/remote/action`; the answer is
`{"ok": …, "message": …, "data": …}`. `client` names the sender in the window's log.

```sh
curl -s -X POST http://127.0.0.1:8840/api/remote/action \
  -H 'Content-Type: application/json' \
  -d '{"action": "show_tissue", "args": {"genes": ["EPCAM", "CD68"], "center": [750, 300], "um_per_pixel": 0.4}, "client": "my script"}'
```

In Python, with only the standard library:

```python
import json, pathlib, urllib.request

info = json.loads((pathlib.Path.home() / "Library/Application Support/SituCraft/remote.json").read_text())

def act(action, **args):
    request = urllib.request.Request(
        info["url"] + "/api/remote/action",
        data=json.dumps({"action": action, "args": args, "client": "notebook"}).encode(),
        headers={"Content-Type": "application/json", "X-SituCraft-Token": info["token"]},
    )
    answer = json.load(urllib.request.urlopen(request, timeout=3600))
    if not answer["ok"]:
        raise RuntimeError(answer["message"])
    return answer

act("open_example", example="lung")
act("save_filter", name="Enough signal", conditions=[{"attribute": "transcripts", "min": 20}])
print(act("methods")["message"])
```

Actions run one at a time, in the window most recently opened from this run of SituCraft; one
waits for the one before it. Most answer within seconds; those that check a bundle, build its
derived data or download an example may take minutes.

| Status | Meaning |
| --- | --- |
| 200 | The action ran; `ok` says whether it succeeded, `message` what happened or why not |
| 400 | The request, or a path it names, is not usable |
| 401 | The action opens paths, reads or writes files, and the token is missing or wrong |
| 403 | The request did not come from this computer, or came from a web page of another site |
| 503 | No SituCraft window is open (or it closed before answering) |
| 504 | The window did not answer in time |

`GET /api/remote/tools` lists every action with its arguments as a JSON schema, under an API
version (`api`, also in `/api/info`). The version changes when an action is removed or renamed,
an argument removed or given another meaning, or a field of a result removed; adding an action,
an optional argument or a field does not change it.

## Security

- SituCraft listens on this computer only, and refuses requests that do not name it (DNS
  rebinding) and web pages of other sites.
- Opening paths (`open_paths`), reading files (`import_segmentation`, `add_image`,
  `import_regions`) and writing
  them (`export`, `export_segmentation`) also need the header `X-SituCraft-Token` with the token
  from `remote.json`: another user of a shared computer can reach the port, but not your files.
- A file is written only at an absolute path in a folder that exists, never over an existing file
  unless `overwrite` is true, and through a temporary file renamed into place. An export's folder
  is written under a temporary name beside it and renamed into place; an existing folder is
  replaced only with `overwrite`, and only when it is a SituCraft export.
- Only a window opened from this run of SituCraft takes actions; a tab left open from an earlier
  run is asked to reload.

## Actions

Names (of datasets, filters, views, genes, stains) are found forgivingly: exact, then ignoring
case, then the one name that starts with or contains what was given. An error lists the choices.

### State

| Action | Arguments | What it does |
| --- | --- | --- |
| `get_state` | | The open project (datasets with their bundles, checks and derived data; filters; saved views), the view shown, the item in focus, the cells chosen, what the Viewer or QC view shows, the items on this computer, the examples, the running jobs |

### Projects and data

| Action | Arguments | What it does |
| --- | --- | --- |
| `new_project` | `name` | Starts an empty project (the open one is saved first) |
| `open_project` | `project` (name or id) | Opens a project from the library, finding its bundles |
| `open_paths` | `paths` (absolute), `add` | Opens bundles (folder, `.zip`, `experiment.xenium`), project archives (`.scz`) or folders of them; with `add`, adds each bundle with no fatal problem to the project and waits for its derived data. Needs the token |
| `add_dataset` | `bundle` (name or path), `wait` | Adds a bundle already opened to the project, checking it and building its derived data |
| `open_example` | `example` (`lung`, `breast`, `simulated`, or their names), `download`, `remake` | Opens an example dataset (in a new project when this one has other datasets); with `download`, downloads a 10x Genomics one first if needed. The simulated one is made on this computer, in seconds, with its truth and an H&E and immunofluorescence image of the section; `remake` makes one an earlier SituCraft made (without its images) again, the same run, cells and transcripts |
| `tour` | `example`, `name` (`tour`, or a worked example: `compare-expansion`, `refine-compare` and `labels-two-segmentations` on Breast, `planted-errors` and `cluster-label` on Lung, `score-against-truth`, `labels-against-truth`, `align-he`, `regions-qupath` and `pathologist-review` on the simulated example), `step`, `run`, `end` | Starts an example's guided tour or worked example (opening the example), or goes to a step; `run` does the step's job (expanding, planting, refining, importing the truth, comparing, clustering, labeling, importing the true types, making the labels again on another segmentation, adding the H&E with its matrix, placing landmarks, aligning it automatically, checking its alignments, drawing regions and writing and reading them in its pixels, deciding the review queue's first cells, excluding a fold) and goes on; answers the step, the comparison it shows (in the Labels tab, the two label sets read through it: the matched cells labeled alike, split and merged cells, marker mixing, the cells whose label changes) and, in the Cell types view, the clustering, the map's colors and selection, and the review queue's counts |

### Seeing the tissue

| Action | Arguments | What it does |
| --- | --- | --- |
| `set_mode` | `mode` | Shows a view: `start`, `data`, `viewer`, `qc`, `segmentation`, `compare`, `celltypes`, `spatial`, `cohort`, `reports`, `jobs` |
| `focus` | `kind` (`dataset`, `filter`, `view`, `local`), `name` | Shows an item in the inspector (a saved view opens); no name clears the focus |
| `show_tissue` | `dataset`, `center` ([x, y] µm), `um_per_pixel` or `width_um` or `whole`, `genes`, `all_transcripts`, `transcripts`, `min_quality`, `point_size`, `channels` ({stain: {visible, color, window, gamma}}), `segmentation` (a segmentation's name or artifact ID, `instrument`, `default`), `outlines`, `nuclei`, `fill` (`transcripts`, `total`, `area`, `nucleusArea`, `method` for the instrument's; `limitations`, `certainty`, `probability` for a refinement's; `clusters` (the latest clustering of the segmentation drawn) or `clusters:<id>`; `labels` (the latest label set, its first level), `labels:<level>` or `labels:<id>:<level>`; `review`, each cell by its review; `none`), `opacity`, `color_by` (`gene`, `probability`), `cell` (an ID in the segmentation shown), `images` (post-Xenium images by name or artifact ID: {visible, opacity, channels: {by name: {visible, color, window, gamma}}}), `graph` (a graph of the segmentation drawn, by artifact ID, `latest` or `none`: its edges drawn over the cells when zoomed in), `cells_of` (a spatial analysis's cells: {`analysis` (an ID or `latest`), `a`, `b` (labels by name), `distance` (a co-occurrence's, a nearest distance's or a Ripley radius, µm)}: an enrichment's pair neighboring each other with the edges between them, or label `b`'s cells within the distance of `a`'s; the cells filled by its labels) | Shows a dataset in the Viewer as asked, everything not given as it is; answers what it shows and what the dataset lacks |
| `add_image` | `dataset`, `path`, `alignment` (found beside the image if not given; `null` for none), `name` | Adds a post-Xenium H&E or immunofluorescence image (a pyramidal, tiled OME-TIFF), read where it lies. Its alignment (Xenium Explorer's transformation matrix or keypoints) is recorded as its first version and places it; without one it is not aligned until `align_image` places it. Needs the token (it reads files). Answers what the image is and how it is placed |
| `align_image` | `dataset`, `image`, `landmarks` ([{`fixed`: [x, y] µm, `moving`: [x, y] the image's pixels}]), `model` (`similarity`, `affine`), `use` (default true), `parent`, `by`, `note` | Fits the image's alignment to the landmarks (three or more) and records it as a version, with each landmark's residual and leave-one-out error in µm; with `use`, it places the image. Answers the fit |
| `register_image` | `dataset`, `image`, `model` (`similarity`, `affine`), `nonrigid`, `channel` (an immunofluorescence image's nuclear stain), `start` (a version, or `in use`), `use` (default true), `by`, `note` | Aligns the image automatically by its nuclei against DAPI, as a job: its scale, turn and mirroring found, refined, fitted to local matches over the tissue, and a smooth deformation after it when asked and it helps. Recorded as a version with its matches and fit; with `use`, it places the image. Answers the fit |
| `use_transform` | `dataset`, `image`, `transform` (a version by name or ID), `rationale`, `by` | Chooses the version that places the image, recorded as a decision |
| `show_alignment` | `dataset`, `image`, `transform`, `show` (a version, `in use`, `fit`, `none`), `display` (`overlay`, `checkerboard`, `flicker`), `opacity`, `cell` (µm), `edges`, `outlines`, `map` | Opens the Alignment view on the image (with `transform`, that version's landmarks to edit), the Xenium image showing the version chosen as asked: over the stains, as a checkerboard or flickering, with DAPI's edges, the nuclei's outlines and where it is off (`map`, by its latest check). Answers the landmarks, the fit, what is shown and the image's versions with their latest checks |
| `check_alignment` | `dataset`, `image`, `transform` (the one in use if not given) | Measures where the version places the image off, by local matches over the tissue, as a job, recorded as a check of it. Answers how far off (median, 90th percentile, most), over how many patches, and a bound of the matches' own error |
| `select_cell` | `cell` (ID in the segmentation shown, or row as `#123`), `dataset`, `center` | Selects a cell and answers its detail: position, areas, transcripts by gene, and the segmentation method or, for a segmentation other than the instrument's, the instrument's cell it came from and what its method flags |

### Regions

Regions are areas drawn on a dataset's tissue, named and classed, kept in the project as versions.
Their points are in `coordinates`: `um` (the default; the dataset's coordinates), `xenium` (the
Xenium image's full-resolution pixels) or a post-Xenium image by name or artifact ID (its
full-resolution pixels, as QuPath opens it, placed through the version of its alignment in use).
Their cells are counted by centroid, of the segmentation the Viewer draws.

| Action | Arguments | What it does |
| --- | --- | --- |
| `draw_region` | `dataset`, one of `rectangle` ([x0, y0, x1, y1]), `polygon` ([[x, y], …]) or `brush` ({`points`, `radius` µm}), `coordinates`, `name`, `class`, `by`, `note` | Draws a region and keeps it as its first version. Classes are QuPath's (`Tumor`, `Stroma`, `Immune cells`, `Necrosis`, `Other`, `Region*`, `Ignore*`) or any other. Answers its area and cells |
| `edit_region` | `dataset`, `region` (name or ID), `name`, `class`, `brush` ({`points`, `radius`, `erase`}), `coordinates`, `restore` (a version from 1), `remove` (`true` removes, `false` returns), `by`, `note` | Changes a region, each change a version appended: renamed, reclassed, painted onto or erased from, an earlier version restored, removed or returned |
| `regions` | `dataset`, `choose` (a region), `versions`, `removed` | Lists the regions with their class, version, area and cells; with `choose`, chooses the cells of the instrument's segmentation in a region, as `select_cells` does |
| `import_regions` | `dataset`, `path`, `coordinates`, `by` | Reads regions from GeoJSON as QuPath exports annotations (Polygon and MultiPolygon, each one's name and classification; detections, cells, points and lines left out), each a region. Needs the token. Answers how many and what was left out |

### Reviews

A review of cells or a region is recorded in the project with who made it, the action, the value
before and after, when, the segmentation, the label set reviewed, each cell's label and its
confidence as shown, and the post-Xenium images' alignments in use. Accepting and relabeling cells
append to the curated set begun from the label set reviewed (as `curate_labels` does, which records
a review too).

| Action | Arguments | What it does |
| --- | --- | --- |
| `review` | `dataset`, `action` (`accept`, `relabel`, `ambiguous`, `exclude`, `include`, `concern`, `resolve`, `comment`), one of `cells` (IDs, or rows as `#123`), `chosen` or `region`, `segmentation`, `labels`, `level`, `label` (relabel), `alternatives` (ambiguous), `concern` (`merged`, `split`, `boundary`, `not-a-cell`, `missed`, `other`), `note` (needed to relabel, exclude and comment), `cells_too` (excluding a region: its cells too, the default), `by` | Reviews cells, the cells chosen or a region. Answers the review |
| `reviews` | `dataset`, `reviewer`, `action`, `cell`, `region`, `segmentation`, `limit` | Lists the reviews, the latest first, with how many of the segmentation's cells are accepted, relabeled, ambiguous, excluded, of concern and commented, and each reviewed region's state |

### Saved views

| Action | Arguments | What it does |
| --- | --- | --- |
| `save_view` | `name` | Saves what the Viewer or QC view shows; answers the view and its link |
| `open_view` | `view` (name or id) | Opens a saved view and says whether it shows exactly what was saved |
| `remove_view` | `view` | Removes a saved view (undoable) |

### Segmentations and jobs

| Action | Arguments | What it does |
| --- | --- | --- |
| `segmentations` | `dataset`, `show` | A dataset's segmentations (or every dataset's): each artifact's lineage, files by SHA-256, check findings, state, and whether it is the default (with the decision's reason) |
| `expand_nuclei` | `dataset`, `distance_um` (5 if not given) | Grows the dataset's nuclei by the distance as a job, in the Segmentation view; records the segmentation in the project and answers its summary and its agreement with the instrument's segmentation |
| `refine_segmentation` | `dataset`, `preset` (`balanced`, `conservative`, `flexible`, `low-memory`, `roi-pilot`, `expert`), `sweeps`, `samples`, `seed` (20250), `layers`, `regions` ([x0, y0, x1, y1] in µm each), `sections`, `min_qv` (20), `seed_check`, `threads` | SituCraft's transcript-informed segmentation as a job, in the Segmentation view's studio: starts from the instrument's cells, refines the whole tissue, its sections apart or chosen regions, records the segmentation (its assignments' probabilities and diagnostics with it) and compares it with the instrument's; answers its summary, convergence (with a second seed's agreement), and that comparison. A failure says what to do |
| `refinement_preflight` | as `refine_segmentation` | What that refinement would model, cover and cost (memory, disk and time against the computer's), its problems and risks, and its recipe, without running it |
| `import_segmentation` | `dataset`, `format` (`spatialdata`, `baysor`, `geojson`, `xenium`, `situcraft`), `paths`, `units` (`um`, `pixels`), `coordinate_system`, `cells`, `points`, `table`, `cell_column`, `cell_property`, `tool`, `tool_version`, `citation` | Imports another tool's segmentation as a job, in the Segmentation view: checked against the dataset, recorded with its files' SHA-256 (invalid when its check finds a fatal problem). Needs the token |
| `compare_segmentations` | `dataset`, `a`, `b` (names or artifact ids; the default segmentation and the latest other if not given) | Compares two segmentations as a job, in the Segmentation view: the differences by class, each segmentation's measures (with spurious and mutually exclusive co-expression), and the pixels and transcripts that changed; recorded as a comparison artifact |
| `cluster_cells` | `dataset`, `segmentation` (the default if not given), `filter` (a QC filter, the instrument's segmentation only), `min_transcripts` (10), `components` (50), `neighbors` (15), `resolution` (1), `seed` (20250) | Clusters the segmentation's cells as a job, in the Cell types view: normalized, scaled, exact principal components, UMAP's neighbor graph, Leiden; each cluster's markers, exploratory. Recorded as a clustering artifact; answers the clusters, their sizes and leading markers |
| `show_map` | `dataset`, `clustering` (id or name; SituCraft's latest if not given), `map` (an id, or `instrument`), `color_by` (`clusters`, `clusters:<clustering>`, `labels` (the latest label set, its first level), `labels:<level>`, `labels:<label set>:<level>`, `confidence`, a gene, `transcripts`, `area`, `reliability`), `select` (`{"clusters": [1, 3]}`, `{"polygon": [[x, y], …], "on": "map" or "tissue"}`, or `none`) | Shows the clustering's map beside the tissue in the Cell types view (the map made first if it has none), colored and selected as asked on both; answers the map's trustworthiness and the selection's composition |
| `name_clusters` | `dataset`, `clustering`, `names` (`{"1": "T cells"}`, by number from 1) | A new version of the clustering's names, appended; the same name merges clusters on the map and the tissue |
| `label_cells` | `dataset`, `clustering` (id or name; SituCraft's latest if not given), `markers` (the project's marker set by name or id, or SituCraft's `breast`, `lung` or `simulated`; the first that fits the panel if not given), `set` (a marker set in full, saved in the project), `margin` (0.1), `minimum` (0), `state` (0.25), `bins` (25), `controls` (50), `lineage` (true), `seed` (20250), `name_clusters` (`true`, or the share of a cluster's cells its label must hold) | Labels the clustering's cells from the marker set as a job, in the Cell types view: scores against control genes binned by expression, labels from the top of the hierarchy down by the margin. Recorded as a labels artifact; answers the cells labeled at each level, each label's cells and markers, each cluster's leading labels, and the names given when asked |
| `reference_labels` | `dataset`, `clustering` (id or name; SituCraft's latest if not given), `path` (an AnnData object in Zarr, a folder or a zip, or a table of label profiles: CSV, TSV, Parquet), `matrix` (`X`, `raw/X` or `layers/…`), `label_column`, `gene_column` (found by their usual names if not given), `tune` (0.05), `margin` (0), `markers` (genes per pair; 0 by the labels' number), `name_clusters` | Labels the clustering's cells from the reference as a job, in the Cell types view: each cell's Spearman correlation with each label's profile over the genes that set the labels apart, fine-tuned on the leading labels' genes (Aran et al. 2019). Recorded as a labels artifact; answers the genes shared and compared, the cells labeled and fine-tuned, each label's reference cells and cells, each cluster's leading labels, and the names given when asked. An .h5ad file is read once written as Zarr |
| `import_labels` | `dataset`, `segmentation` (name, id or `instrument`; the default if not given), `path` (a table: CSV, TSV, Parquet, AnnData or SpatialData Zarr, Xenium Explorer's cell groups) or `instrument` (`graph-based`, `k-means N`), `id_column`, `column`, `confidence_column`, `color_column` (found by their usual names if not given) | Imports the labels for the segmentation's cells as a job, matched by id; a table naming cells the segmentation does not have is refused, with how many. Recorded as a labels artifact with its source; answers the labels and their cells |
| `review_labels` | `dataset`, `segmentation`, `labels` (a label set's id, `cur-…` or `con-…`; the latest if not given), `level`, `neighbors` (10), `implausible` (0.2), `low_confidence`, `against` (a label set of another segmentation, its id, `id:level` or name), `limit` (20) | Ranks the set's cells for review (spatially implausible: at most a share of their nearest labeled cells carry their label; segmentation-sensitive: their counterparts under the other segmentation carry another label in the set `against`, as the two segmentations' comparison matches cells; of low confidence), the reviewed left out; answers the counts and the first cells with why |
| `repeat_on_segmentation` | `dataset`, `clustering` (id or name; SituCraft's latest if not given), `segmentation` (the one to make it again on) | Makes the clustering, its map and its label sets from markers or a reference again on the segmentation with the same parameters, as jobs, in one step (a QC filter's cells are the instrument's, so the rule chooses them; imported sets are not made again); answers each made, and why any was not |
| `label_impact` | `dataset`, `comparison` (id or "A and B"; the latest if not given), `labels_a`, `labels_b` (a label set of each segmentation; by default two made the same way), `color` | Opens the comparison's Labels tab: each label's cells under each segmentation, the matched cells labeled alike and the pairs of labels, what split and merged cells carry, cells without a counterpart, the differences whose labels change, the cells whose label changes with the segmentation, and marker mixing by label |
| `label_evidence` | `dataset`, `segmentation`, `cell` (its row, `#123`), `labels`, `level`, `clustering` | A cell's label in each set, its markers and the next label's at each level (from a reference: each round's correlations, and the genes for its label and the next), its nearest cells in expression, and its reviews |
| `curate_labels` | `dataset`, `segmentation`, `labels` (a curated set, or a predicted set to curate), `level`, `cells` (rows) or `clusters` (numbers from 1) with `clustering`, `label` or `accept`, `reason`, `by`, `name` | Appends an edit, with its reason and who, to a curated set (begun from the predicted set the first time); edits are never changed or removed |
| `consensus_labels` | `dataset`, `segmentation`, `members` (ids, `lab-…:1` for a level, or names), `rule` (`unanimous`, `majority`, `priority`), `min`, `mapping` (per set, `{label: common name}`), `name` | Makes a consensus label set by the rule; answers the cells labeled and how far the first two sets agree |
| `instrument_clusters` | `dataset`, `clustering` (`graph-based`, `k-means N`, or the folder's name) | Adds one of the instrument's clusterings (its markers by SituCraft) and, the first time, the instrument's map |
| `show_comparison` | `dataset`, `comparison` (id or "A and B"), `mode` (`side`, `overlay`, `flicker`), `map`, `classes`, `rank` (`reassigned`, `contamination`, `disagreement`, `low-counts`), `min_changed`, `difference` (a place in the queue) or `cell` (an id) | Opens a comparison in the Compare workspace as asked; answers the review queue's first ten, the difference shown, and the cautions about the measures |
| `decide_segmentation` | `dataset`, `segmentation`, `decision` (`promote`, `keep`, `reject`), `rationale`, `comparison` | Records a decision about a segmentation with its reason, appended to the project; the default cannot be rejected |
| `plant_errors` | `dataset`, `merges` (20), `splits` (20), `missed` (10), `grown` (10), `grow_um` (1), `seed`, `reveal` | For practice: a copy of the instrument's segmentation with known errors planted, simulated (never promoted or exported), recorded as a segmentation artifact with its answer key; `reveal` answers the key |
| `export_segmentation` | `dataset`, `segmentation` (the default if not given), `formats` (`spatialdata`, `anndata`, `parquet`, `csv`, `mex`, `geojson`, `xenium-ranger`), `folder` (absolute), `overwrite` | Writes the segmentation in those formats into the folder as a job, in the Segmentation view, with `situcraft-export.json` (every file by SHA-256, totals and content fingerprints); `xenium-ranger` prepares Xenium Ranger's inputs and command, checked. A planted segmentation is refused. Records the export in the project and answers the manifest. Needs the token |
| `jobs` | `job` (id), `do` (`cancel`, `resume`, `discard`), `show` | The jobs, or one job with its whole log and spec; stops, resumes or forgets one |

### Spatial analysis

| Action | Arguments | What it does |
| --- | --- | --- |
| `build_graph` | `dataset`, `segmentation` (the default if not given), `kind` (`delaunay`, `knn`, `radius`, `contact`), `longest` (a Delaunay graph's longest edge, µm; 50, 0 for none), `k` (6), `radius` (µm; 20), `gap` (a contact graph's, µm; 0), `filter` (a QC filter, the instrument's segmentation only), `regions` (names or IDs: only their cells) | Builds a graph of which cells are neighbors as a job, in the Spatial view, over the cells chosen, never cells a review excluded. Recorded as a graph artifact; answers its cells and edges, the neighbors a cell, the cells without one, its pieces and its edges' lengths |
| `spatial_analysis` | `dataset`, `segmentation`, `question` (`enrichment`, `cooccurrence`), `labels` (a label set's id; the latest if not given), `level`; for enrichment `graph` (an id; the latest built if not given), `permutations` (1,000), `seed` (20250), `weight` (`edges`, `boundary`: a contact graph's), `within` (regions to deal the labels within); for co-occurrence, nearest distances and Ripley's K `filter`, `regions` (only their cells); for co-occurrence `up_to` (150 µm), `every` (10 µm), `pairs` (`within`, `between`); for nearest distances `reach` (100 µm), `every` (2.5 µm), `permutations` (199); for Ripley's K `window` (`tissue`, or a region by name or ID), `closing` (10 µm), `up_to` (100 µm), `every` (5 µm), `correction` (`isotropic`, `none`), `permutations` (199) | Asks a question of the label set's cells as a job, in the Spatial view. Enrichment: each pair of labels' neighbors in the graph against the exact expectation if the labels were dealt at random, as a ratio, with z and a two-sided p-value from seeded permutations, Benjamini–Hochberg adjusted. Co-occurrence: each label's likelihood within (or since the last) distance of another's against any cell's. Nearest distances: each label's cells' distance to the nearest of each label against the labels dealt at random, with its distribution's envelope. Ripley's K and L in a window, edge-corrected, against the envelope of the labels dealt at random, with a global test. Recorded as an analysis artifact; answers every pair, the ratios at each distance, or the curves |

### Quality control

| Action | Arguments | What it does |
| --- | --- | --- |
| `qc_metrics` | `dataset` | The run's recorded metrics, each explained, with SituCraft's recomputed value and whether they agree |
| `set_qc_map` | `map`, `dataset`, `filter` (or `none`), `center`, `um_per_pixel` | Shows a QC map: `cell:<attribute>` or `bin:outside`, `bin:quality`, `bin:controls`, `bin:density`, `none` |
| `select_cells` | `attribute`, `min`, `max`, `dataset`, `clear` | Chooses cells by an attribute's range, as dragging across a plot does; answers how many |
| `save_filter` | `name`, `conditions`, `dataset` | Saves a new version of a filter: conditions `{attribute, min, max}` or `{attribute: "method", values: [...]}`; answers the cells it keeps and their SHA-256 |

Cell attributes: `transcripts`, `genes`, `total`, `controls`, `area`, `nucleusArea`,
`nucleusShare`, `density`, `nuclearShare`, `method`.

### The record

| Action | Arguments | What it does |
| --- | --- | --- |
| `methods` | `dataset`, `filters` (names), `show` | The methods paragraph and its references; `show` shows it in the window |
| `export` | `what` (`project`, `archive`, `methods`, `alignment`, `regions`, `reviews`), `path` (absolute), `overwrite`, `dataset`; for an alignment `image`, `transform` (the one in use if not given), `format` (`matrix`, `keypoints`); for regions `coordinates`, `regions` (names or IDs; all if not given); for reviews `format` (`csv`, `json`) | Writes the project document (JSON), a project archive (`.scz`), the methods (Markdown), an image's alignment as Xenium Explorer reads it (CSV), a dataset's regions as QuPath imports annotations (GeoJSON), or a dataset's reviews (CSV, a row a cell, cluster or region reviewed; JSON, the record). Needs the token |

## The documentation's screenshots

`node docs/capture/capture.mjs` captures every picture in `docs/images` through remote control,
in the light and dark themes, from the development bundles (`node validation/fetch.mjs`). With
`--audit` it also checks each scene against axe-core's WCAG 2.1 A and AA rules
(`node validation/fetch.mjs axe-core`) and records the result in `docs/capture/audit.json`.
