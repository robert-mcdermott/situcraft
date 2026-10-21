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
`{"ok": …, "message": …, "data": …}`, and `changed` when the action added, changed or removed
records of the project (each `{id, kind, name, change}`). `client` names the sender in the
project's history and the library's log of calls.

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
  `import_regions`, `verify_certificate`, `read_figure`; `import_labels` and `reference_labels`
  given a `path`, and `cohort_steps` and `summarize_samples` given a `reference` by its path) and
  writing them (`export`, `write_figure`, `export_segmentation`) also need the header
  `X-SituCraft-Token` with the token from `remote.json`: another user of a shared computer can
  reach the port, but not your files.
- A file is written only at an absolute path in a folder that exists, never over an existing file
  unless `overwrite` is true, and through a temporary file renamed into place. An export's folder
  is written under a temporary name beside it and renamed into place; an existing folder is
  replaced only with `overwrite`, and only when it is a SituCraft export.
- Only a window opened from this run of SituCraft takes actions; a tab left open from an earlier
  run is asked to reload.

## The log of calls

Every call a script or an agent makes is a line of the library's log, refused and failed ones too:
when, the caller's `client` name and whether it came as a script or an agent (with the leave it
was given), the action and its arguments (long texts and lists cut short), whether it was `done`,
`refused` (SituCraft would not perform it: no token, a request it could not read, an action it
does not know, or an agent's leave or required argument missing) or `failed` (performed, or tried,
and answered why not, or no window answered), the sentence it answered, what it changed in the
project, the ids its answer named, and how long it took. The log is
`calls/YYYY-MM.jsonl` in the data folder, a month a file ([formats](FORMATS.md#the-log-of-calls));
it never leaves this computer.

The drawer's **Calls** tab (`⌘J`) shows it, the newest first, as calls arrive: searched by words,
chosen by caller, action and outcome, each call opening to its arguments, what it changed and the
ids it answered with (those of the open project open their records), and exported as CSV or JSON
lines. Scripts read it too: `GET /api/calls?q=&tool=&by=&outcome=&offset=&limit=` (100 a page)
and `GET /api/calls/export?format=csv` (or `jsonl`).

## Actions

Names (of datasets, filters, views, genes, stains) are found forgivingly: exact, then ignoring
case, then the one name that starts with or contains what was given. An error lists the choices.

### State

| Action | Arguments | What it does |
| --- | --- | --- |
| `get_state` | | The open project (datasets with their bundles, checks and derived data; filters; saved views), the view shown, the item in focus, the cells chosen, what the Viewer or QC view shows, the items on this computer, the examples, the running jobs |
| `list_projects` | | The library's projects, the most recently changed first: each one's id, name, when it was made and changed, its datasets, and how many results, figures and saved views it has |
| `describe_project` | `project` (name, part of it, or id; the most recently changed if not given, and of several with one name, the most recently changed) | What a project holds without opening it: its datasets, its results (each one's id, kind, name, dataset, method and the ids of what it was made from), the page's records (filters, designs, marker sets, curated and consensus labels), regions, saved views, figures and decisions |
| `get_result` | `id` (a result's or record's), `table`, `offset` (0), `limit` (100; 0 for all, up to 10,000) | A result's record (kind, name, dataset, state, method, parameters, what it was made from, software, job, findings, the projects holding it), its summary, and its numbers as tables, a page of one table's rows: a spatial analysis's by its question, a clustering's clusters, a label set's labels, a cohort comparison's tests and a cohort summary's table as their CSV files hold them, and its summary as the report shows it (`summary`, `summary.<key>`). Numbers are numbers, null where there is none |
| `query_cells` | `where` (a tree of conditions), `project`, `dataset` (its only one if not given), `segmentation` (by id or name, or `instrument`; the one promoted, else the instrument's, if not given), `by` (a label set's, clustering's or niches analysis's id), `level` (0), `columns` (attributes or genes), `offset` (0), `limit` (20) | The cells the tree keeps: how many of all, the SHA-256 of which (as a saved filter records it), a page of them (row, id, centroid and the columns asked for), and with `by` how they divide among its labels, clusters or niches against all the cells |
| `query_transcripts` | `where` (a tree of conditions), `project`, `dataset`, `min_quality` (20), `limit` (20 genes; 0 for all) | The transcripts the tree keeps, of that quality or more, counted by gene |
| `summarize_region` | `region` (name or id) or `box` ([x0, y0, x1, y1] in µm), `project`, `dataset`, `segmentation`, `labels` (a label set's id; the latest of the segmentation in the project if not given), `level` (0), `genes` (10 a list) | A region's area, its cells by centroid and their density, how they divide among the labels against the whole tissue (with each label's density), and its genes: the most transcripts, and those most above the rest of the tissue |
| `recipe` | `id` (a result's) | Its recipe: every step from its bundles to it, each the job that made it and its spec as readable JSON, the bundles by their roles and each step's inputs by the steps that make them; `situcraft run --recipe` makes it again without a window ([formats](FORMATS.md#recipes-of-results)) |

`list_projects`, `describe_project`, `get_result`, `query_cells`, `query_transcripts`,
`summarize_region` and `recipe` are answered by the program itself, from the library and the
results' folders, whether or not a window is open; every other action is performed by the window.

A tree of conditions (`where`) is JSON, never a query string: `{"all": [trees]}`, `{"any":
[trees]}`, `{"not": tree}`, or one condition. On cells: `{"attribute": …, "min", "max"}` (a QC
attribute, bounds included), `{"attribute": "method", "in": [...]}`, `{"gene": …, "min", "max"}`
(at least 1 when no bound), `{"labels": id, "level", "in": [names or numbers from 0]}`,
`{"clusters": id, "in": [names or numbers from 1]}`, `{"niches": id, "in": [numbers from 1]}`,
`{"region": name}`, `{"distance": region, "min", "max"}` (µm from its edge, negative inside),
`{"box": [x0, y0, x1, y1]}` and `{"filter": name}`. On transcripts: `{"gene": name or [names]}`,
`region`, `distance`, `box`, `{"assigned": true or false}` and `{"cells": a tree of cells}`. A tree
SituCraft cannot read is refused with the reason; [MCP](MCP.md#asking-which-cells) has examples.

### Projects and data

| Action | Arguments | What it does |
| --- | --- | --- |
| `new_project` | `name` | Starts an empty project (the open one is saved first) |
| `open_project` | `project` (name or id) | Opens a project from the library, finding its bundles |
| `open_paths` | `paths` (absolute), `add` | Opens bundles (folder, `.zip`, `experiment.xenium`), project archives (`.scz`) or folders of them; with `add`, adds each bundle with no fatal problem to the project and waits for its derived data. Needs the token |
| `add_dataset` | `bundle` (name or path), `wait` | Adds a bundle already opened to the project, checking it and building its derived data |
| `add_results` | `ids` | Adds results of the library (segmentations, clusterings and their maps, label sets, graphs, spatial analyses) to the open project, each to the dataset of its bundle, as the view that made it adds it (`situcraft run --report` adds those it made); answers those added and those passed over, and why |
| `open_example` | `example` (`lung`, `breast`, `simulated`, `cohort`, `null-cohort`, `paired-cohort`, or their names), `download`, `remake` | Opens an example dataset (in a new project when this one has other datasets); with `download`, downloads a 10x Genomics one first if needed. The simulated tissue is made on this computer, in seconds, with its truth and an H&E and immunofluorescence image of the section; `remake` makes one an earlier SituCraft made (without its images) again, the same run, cells and transcripts. A simulated cohort (`simulated-cohort`, `simulated-null-cohort`, `simulated-paired-cohort`) is made in seconds too, and its samples are added together, each with its subject and condition |
| `tour` | `example`, `name` (`tour`, or a worked example: `compare-expansion`, `refine-compare`, `labels-two-segmentations`, `neighbors-breast` and `tumor-edge` on Breast, `planted-errors`, `cluster-label`, `moran-genes` and `remake-enrichment` on Lung, `score-against-truth`, `labels-against-truth`, `align-he`, `regions-qupath`, `pathologist-review` and `niches-domains` on the simulated example, `pseudo-samples` on Breast (for teaching), `cohort-gene` on the simulated cohort, `cohort-null` on the null cohort, `cohort-shift` and `cohort-paired` on the cohort before and after), `step`, `run`, `end` | Starts an example's guided tour or worked example (opening the example), or goes to a step; `run` does the step's job (expanding, planting, refining, importing the truth, comparing, clustering, labeling, importing the true types, making the labels again on another segmentation, adding the H&E with its matrix, placing landmarks, aligning it automatically, checking its alignments, drawing regions and writing and reading them in its pixels, deciding the review queue's first cells, excluding a fold, importing the truth's domains, building a graph, asking a spatial question, making an answer again on another segmentation, setting the samples, describing the design, running the same steps on every sample, summarizing them, comparing the groups) and goes on; answers the step, the comparison it shows (in the Labels tab, the two label sets read through it: the matched cells labeled alike, split and merged cells, marker mixing, the cells whose label changes) and, in the Cell types view, the clustering, the map's colors and selection, and the review queue's counts; a spatial step answers the analysis it shows (`spatial`: `analysis`, and `again`, the one made again on another segmentation) and the cells it shows on the tissue (`focus`: their title and count); a cohort step answers the summary and the comparison it shows (`cohort`: `summary`, `comparison`, their IDs) |

### Seeing the tissue

| Action | Arguments | What it does |
| --- | --- | --- |
| `set_mode` | `mode` | Shows a view: `start`, `data`, `viewer`, `qc`, `segmentation`, `compare`, `celltypes`, `spatial`, `cohort`, `reports`, `jobs` |
| `focus` | `kind` (`dataset`, `filter`, `view`, `local`), `name` | Shows an item in the inspector (a saved view opens); no name clears the focus |
| `show_tissue` | `dataset`, `center` ([x, y] µm), `um_per_pixel` or `width_um` or `whole`, `genes`, `all_transcripts`, `transcripts`, `min_quality`, `point_size`, `channels` ({stain: {visible, color, window, gamma}}), `segmentation` (a segmentation's name or artifact ID, `instrument`, `default`), `outlines`, `nuclei`, `fill` (`transcripts`, `total`, `area`, `nucleusArea`, `method` for the instrument's; `limitations`, `certainty`, `probability` for a refinement's; `clusters` (the latest clustering of the segmentation drawn) or `clusters:<id>`; `labels` (the latest label set, its first level), `labels:<level>` or `labels:<id>:<level>`; `review`, each cell by its review; `none`), `opacity`, `color_by` (`gene`, `probability`), `cell` (an ID in the segmentation shown), `images` (post-Xenium images by name or artifact ID: {visible, opacity, channels: {by name: {visible, color, window, gamma}}}), `graph` (a graph of the segmentation drawn, by artifact ID, `latest` or `none`: its edges drawn over the cells when zoomed in), `cells_of` (a spatial analysis's cells: {`analysis` (an ID or `latest`), `a`, `b` (labels by name), `distance` (a co-occurrence's, a nearest distance's or a Ripley radius, µm)}: an enrichment's pair neighboring each other with the edges between them, or label `b`'s cells within the distance of `a`'s; for regions compared {`analysis`, `a`, `region` (a region's name, or the row of the tissue in none)}, label `a`'s cells in it; across an edge {`analysis`, `a` (any label if not given), `zone` (`core`, `margin`, `around`, `all`)}, label `a`'s cells there; for niches {`analysis`, `niche` (its number from 1)}, the niche's cells; for Moran's I {`analysis`, `gene` (one whose local Moran's I was kept), `class` (`high`, `low`, `high-low`, `low-high`, `all`)}, the gene's cells of that local class; the cells filled by its labels); `fill` also takes `niches` (the latest niches of the segmentation drawn) or `labels:<their analysis's id>:0`, `picture`, `picture_width` (1000) | Shows a dataset in the Viewer as asked, everything not given as it is; answers what it shows and what the dataset lacks; with `picture`, a picture of what the Viewer shows too, drawn as a figure's tissue panel draws it with its scale bar and legend (`picture`: its caption, a JPEG as a data URL, its size and notes) |
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
| `show_map` | `dataset`, `clustering` (id or name; SituCraft's latest if not given), `map` (an id, or `instrument`), `color_by` (`clusters`, `clusters:<clustering>`, `labels` (the latest label set, its first level), `labels:<level>`, `labels:<label set>:<level>`, `confidence`, a gene, `transcripts`, `area`, `reliability`), `select` (`{"clusters": [1, 3]}`, `{"polygon": [[x, y], …], "on": "map" or "tissue"}`, or `none`), `picture`, `picture_width` (1000) | Shows the clustering's map beside the tissue in the Cell types view (the map made first if it has none), colored and selected as asked on both; answers the map's trustworthiness and the selection's composition; with `picture`, a picture of the map too, by its clusters or a label set (a PNG as a data URL). For an agent not given compute, a segmentation without a map is refused rather than mapped |
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
| `spatial_analysis` | `dataset`, `segmentation`, `question` (`enrichment`, `cooccurrence`, `distances`, `ripley`, `regions`, `boundary`, `niches`, `moran`), `labels` (all but `moran`) (a label set's id, or niches' analysis id; the latest label set if not given), `level`; for enrichment `graph` (an id; the latest built if not given), `permutations` (1,000), `seed` (20250), `weight` (`edges`, `boundary`: a contact graph's), `within` (regions to deal the labels within); for the others `filter`; for co-occurrence, nearest distances and Ripley's K `regions` (only their cells); for co-occurrence `up_to` (150 µm), `every` (10 µm), `pairs` (`within`, `between`); for nearest distances `reach` (100 µm), `every` (2.5 µm), `permutations` (199); for Ripley's K `window` (`tissue`, or a region by name or ID), `closing` (10 µm), `up_to` (100 µm), `every` (5 µm), `correction` (`isotropic`, `none`), `permutations` (199); for regions compared `regions` (the regions to compare, up to 20), `closing` (10 µm); across an edge `region` (by name or ID), `reach` (100 µm), `every` (10 µm), `margin` (20 µm), `closing` (10 µm); for niches `neighbors` (10) or `radius` (µm), `niches` (6), `starts` (10), `seeds` (4), `seed` (20250); for Moran's I `graph`, `weights` (`row`, `binary`), `permutations` (199; 0 for the analytic test), `local` (10 genes), `local_permutations` (199), `seed` | Asks a question of the label set's cells as a job, in the Spatial view. Enrichment: each pair of labels' neighbors in the graph against the exact expectation if the labels were dealt at random, as a ratio, with z and a two-sided p-value from seeded permutations, Benjamini–Hochberg adjusted. Co-occurrence: each label's likelihood within (or since the last) distance of another's against any cell's. Nearest distances: each label's cells' distance to the nearest of each label against the labels dealt at random, with its distribution's envelope. Ripley's K and L in a window, edge-corrected, against the envelope of the labels dealt at random, with a global test. Regions compared: each region's labels per mm² of the tissue within it and per labeled cell, with intervals, the tissue in none of them a row of its own, each region over it as ratios with intervals. Across an edge: each label's density in bands of distance from a region's edge, each band's tissue its denominator, and the core, margin and around compared: which labels fill the region, stay out or gather at its margin. Niches: each cell's neighborhood by its labels' shares, grouped by k-means (k-means++ seeding) into niches, each with its make-up, defining labels and leading genes, and their agreement with further seeds' niches. Moran's I: each gene's spatial autocorrelation over a graph, tested analytically and by permutation, Benjamini–Hochberg over the genes, and the leading genes' local Moran's I. Recorded as an analysis artifact; answers every pair, the ratios at each distance, the curves, the regions' table, the bands and readings, the niches, or the genes |
| `remake_analysis` | `dataset`, `analysis` (an id; the latest if not given), `segmentation` (the other, by name or ID) | Makes a spatial analysis again on another segmentation in one step: the two compared (unless they are), its labels made again from the same clustering and marker set or reference, its graph with the same definition, the same parameters, regions and window; recorded as made from the first. Answers each number under both (by label and gene names), the change, the numbers' rank correlation and the largest changes. Imported, curated and consensus labels, and niches, are not made again |

### Samples

A project's samples: each dataset, or its regions instead (a tissue microarray's cores, sections on
one slide), with the user's fields and those read from each bundle.

| Action | Arguments | What it does |
| --- | --- | --- |
| `samples` | none | Lists the samples in the Cohort view: each one's name, dataset, region (a region removed flagged), cells under its dataset's default segmentation (counted in its region), its region's area in µm², its fields (missing values `null`, never zero) and its bundle's run, slide, Xenium region, panel and software; and the fields with their kinds |
| `set_samples` | `regions` (`{dataset, regions}`: a dataset's regions by name or ID as its samples; none for the whole section), `fields` (`[{name, kind}]`, `text` or `number`: added, or retyped), `remove_fields` (names), `names` (`{sample: name}`; empty for the dataset's or region's), `table` (CSV or tab-separated text with a `sample` column), `values` (`{sample: {field: value}}`; `null` or empty clears) | Sets them in that order, in one step: if any part is refused, nothing changes. A table's rows are matched to samples by name (ignoring case), its fields' columns set (an empty cell clears), its other columns added as fields (numbers when every value is one), the bundle's columns left alone; rows naming no sample are said. Samples are named as they are, or as they were before the call. Answers the samples, as `samples` does |

### Designs

| Action | Arguments | What it does |
| --- | --- | --- |
| `design` | `unit` (the replicate unit's field, such as `subject`), `group` (the field compared, such as `condition`), `reference` (the first in code-point order if not given), `contrasts` (`[[a, b]]`, each a − b; each level against the reference if not given), `covariates` (fields, or `run`, `slide`, `panel`), `exclude` (`{sample: reason}`), `save` (a name), `pseudo_samples` (for teaching only: regions of one section as separate units kept, marked pseudo-samples, instead of refused) | Makes the experimental design in the Cohort view: the units in each group (a unit's samples in one group combined; a unit in more than one group pairing them), the matrix in treatment coding (as patsy's), its rank and residual degrees of freedom, the contrasts, the samples left out, notes, and why it cannot be used if it cannot (a group of fewer than two units, regions of one section as separate units unless marked pseudo-samples, a term confounded with another, no degrees of freedom left, the groups confounded with a slide, run or panel). With `save`, keeps it as that name's next version. Without `unit` or `group`, answers the latest saved design |

### Summaries

Every sample summarized the same way, and the cohort table: each sample's numbers, each traced to the
artifact it came from.

| Action | Arguments | What it does |
| --- | --- | --- |
| `cohort_steps` | `markers` (a project set's name or ID, or a starter's ID; the starter that fits the first sample's panel if not given), or `reference` (a labeled single-cell reference's path: an AnnData object saved with `write_zarr`), `level` (1, the first, if not given), `graph` (`delaunay`, `knn`, `radius`, `contact`; `delaunay` if not given), `questions` (`enrichment`, `distances`, `moran`; `enrichment` if not given) | Runs the same steps on every sample in the Cohort view: each dataset's cells clustered with SituCraft's defaults and labeled from the one marker set (a starter resolved on the first dataset and used for the rest) or the one reference, then a graph of each sample's cells (a region sample's, its region's; never those a review excluded) and the questions asked of it with their defaults. A step made already is answered at once. Answers the labels' source (the marker set or the reference) and each sample's clustering, labels, graph and answers |
| `summarize_samples` | `markers` (by name or ID; the one the steps used, or the first that labels every sample, if not given), or `reference` (by its path or SHA-256), `level`, `niches` (6 if not given; 0 for none), `neighbors` (10 if not given), `closing` (µm; 10 if not given), `samples` (names or IDs; every sample if not given) | Summarizes the samples as an artifact of kind `cohort-summary`: each sample's cells by label (counts, shares, densities per mm² of its tissue), its niches' shares (niches found across the samples together), its quality, its graph's and answers' numbers and its pseudobulk; the samples far out, the batches, the panels compared and the pseudobulk's principal components (`cohort_qc` reads them). Answers the summary's ID, the cohort's labels, each sample's cells, labeled cells, counts, tissue, labels, graph, answers and niches' shares, the niches (each one's cells and defining labels) and the table's families |
| `cohort_table` | `summary` (an ID; the latest if not given), `family` (`cells`, `share`, `density`, `niche`, `qc`, `graph`, `enrichment`, `nearest`, `moran`; `cells` if not given), `keys` (only these) | Reads one family of a summary's table and shows it: each sample's value by key and the artifact each came from |

### Cohort QC

Before any comparison, the samples side by side: their quality, their batches and their panels.

| Action | Arguments | What it does |
| --- | --- | --- |
| `cohort_qc` | `summary` (an ID; the latest if not given) | Reads a summary's QC in the Cohort view: each sample's quality numbers (null where its segmentation records no nucleus) with its bundle's run, slide and panel; the samples far out on one (a robust z-score of 3.5 or more and a difference that matters: 20% for a quantity, 5 points for a share), each with the reason, never passed or failed; the numbers' medians by slide, run and panel; the design draft's samples by batch and group, and what it says of a group confounded with a batch; the pseudobulk's principal components (each sample's scores, each component's share of the variance); and the panels compared |
| `compare_panels` | none | Compares the project's samples' gene panels, before any summary: the panels (each one's name, design, version, genes and samples), the genes every panel has (the only ones compared across samples), those in some only, shared genes read by other codewords, and genes whose Ensembl IDs or names differ |

### Differential abundance

Each label's cells compared between the design's groups, with the replicate unit as the unit.

| Action | Arguments | What it does |
| --- | --- | --- |
| `compare_abundance` | `families` (`share`, `density`, `niche`; `share` and `density` if not given), `design` (a saved design by name; the Cohort view's draft if not given), `summary` (an ID; the one shown or the latest if not given), `level` (0.95 if not given) | Compares the summary's samples as an artifact of kind `abundance`: each label's share of the labeled cells by a quasi-binomial model of the design, its density per mm² by a quasi-Poisson model with the tissue's area as offset, the niches' shares as shares; each contrast's effect (odds or density ratio) with its interval, t test, dispersion and Benjamini–Hochberg-adjusted p over each family's tests. Answers every test, and those untested with why |
| `show_sample_cells` | `sample` (name or ID), `label`, `summary` (the latest if not given) | Shows a sample's cells of a label on its tissue in the Viewer: of its cells chosen, those the summary's label set gives the label. Answers the cells shown |

### Differential expression and spatial statistics

Each label's genes, and the samples' spatial statistics, compared between the design's groups, with
the replicate unit as the unit.

| Action | Arguments | What it does |
| --- | --- | --- |
| `compare_expression` | `design` (a saved design by name; the Cohort view's draft if not given), `summary` (an ID; the one shown or the latest if not given), `level` (0.95 if not given), `min_cells` (the fewest cells of a label a unit is tested with; 10 if not given) | Compares each label's pseudobulk (its cells' transcripts summed over each unit's samples, over the genes every panel has) as an artifact of kind `expression`: genes filtered by expression, TMM normalization, voom's weights, each gene's weighted model of the design and limma's moderated t; each contrast's log2 fold change with its interval, and p-values adjusted by Benjamini and Hochberg over every label, gene and contrast tested. Answers each label's units, genes tested and prior, the genes below an adjusted p of 0.05 and the 10 lowest p-values of each, and the labels untested with why |
| `show_gene` | `gene`, `label` (by name), `comparison` (an expression comparison's ID; the latest if not given) | Shows a gene's values in a label by group in the Cohort view: each unit's log2 counts per million of the label's pseudobulk, its count and library size, with the gene's test of each contrast |
| `compare_spatial` | `families` (`enrichment`, `nearest`, `moran`; every family the summary has if not given), `design`, `summary`, `level` (as `compare_expression`'s) | Compares the samples' spatial statistics as an artifact of kind `spatial-comparison`: neighbor enrichments and nearest distances as log2 ratios, and Moran's I (niches' shares are compared by `compare_abundance`); each statistic a linear model of the design, each family's variances moderated across its statistics; each contrast's effect (a ratio, or a difference) with its interval and moderated t, and p-values adjusted over each family's tests. Answers every test, and those untested with why |

### Quality control

| Action | Arguments | What it does |
| --- | --- | --- |
| `qc_metrics` | `dataset` | The run's recorded metrics, each explained, with SituCraft's recomputed value and whether they agree |
| `set_qc_map` | `map`, `dataset`, `filter` (or `none`), `center`, `um_per_pixel` | Shows a QC map: `cell:<attribute>` or `bin:outside`, `bin:quality`, `bin:controls`, `bin:density`, `none` |
| `select_cells` | `attribute`, `min`, `max`, `dataset`, `clear` | Chooses cells by an attribute's range, as dragging across a plot does; answers how many |
| `save_filter` | `name`, `conditions` or `where`, `dataset` | Saves a new version of a filter of the instrument's segmentation's cells: conditions `{attribute, min, max}` or `{attribute: "method", values: [...]}`, or `where`, a tree of conditions as `query_cells` takes it, which SituCraft applies; answers the cells it keeps and their SHA-256 |

Cell attributes: `transcripts`, `genes`, `total`, `controls`, `area`, `nucleusArea`,
`nucleusShare`, `density`, `nuclearShare`, `method`.

### The record

| Action | Arguments | What it does |
| --- | --- | --- |
| `methods` | `dataset` (the default), `result` (by ID or name), `project` (true), `filters` (for a dataset: names; the latest version of each if not given), `show` | A methods section written from the record of a dataset and everything made of it, of a result, or of the whole project: every step said as what was done, in paragraphs by topic, with its parameters, seeds and what it found, and the software; its references numbered as first cited. Answers `title`, `text`, `paragraphs` (`{topic, heading, text}`) and `references`; `show` shows it (a dataset's in the Data view, any other in the Reports view) |
| `record` | `result` (by ID or name), or `dataset` (its results) or `project` (true), `format` (`json`, `markdown`, `bibtex`; `json` if not given), `time` (ISO 8601; now if not given) | What made a result, shown in the Reports view: each step from the bundles to it (bundles, segmentations, clusterings, marker sets, labels, graphs, analyses, designs, summaries, comparisons, figures…), each after the steps it was made from, with its method and version, parameters, seeds, software, decisions and references (numbered as first cited, each with its DOI). The result is any analysis, comparison, summary, label set, clustering, map, graph, segmentation, alignment, filter or figure. Answers the record (`situcraft-record`, [Formats](FORMATS.md#what-made-a-result)) and, for `markdown` or `bibtex`, its text |
| `verify_records` | `steps` (each `{id, kind, drawing}`) | Used by `situcraft verify`: the page's records named (a design, a filter, a figure) computed again in the project open; answers each one's `id`, `checked`, `same`, `detail` and `differ` |
| `export` | `what` (`project`, `archive`, `methods`, `alignment`, `regions`, `reviews`, `samples`, `cohort-table`, `pseudobulk`, `record`, `certificate`, `report`, `package`), `path` (absolute), `overwrite`, `dataset`; for an alignment `image`, `transform` (the one in use if not given), `format` (`matrix`, `keypoints`); for regions `coordinates`, `regions` (names or IDs; all if not given); for reviews `format` (`csv`, `json`); for a cohort table or pseudobulk `summary` (the latest if not given); for a record `result` (or `dataset`, `project`), `format` (`markdown`, `json`, `bibtex`; from the path's extension, `.md`, `.json` or `.bib`, if not given); for a certificate `result`, `dataset` or `project`; for a report or a package `results` (IDs or names, figures among them), `title`, `names` (leave out the samples' and subjects' names); for a package, `path` a folder that does not exist yet (or a `.zip`) | Writes the project document (JSON), a project archive (`.scz`), the methods of a dataset, a result (`result`) or the project (`project: true`) as Markdown, an image's alignment as Xenium Explorer reads it (CSV), a dataset's regions as QuPath imports annotations (GeoJSON), a dataset's reviews (CSV, a row a cell, cluster or region reviewed; JSON, the record), the project's samples (CSV, a row a sample), a cohort summary's table (CSV, a row each number of each sample with the artifact it came from) or pseudobulk (CSV, a row each sample's label, a column each gene), what made a result (Markdown, JSON or BibTeX), or a certificate (`certificate`: a zip of what a result, a dataset's results or the project rests on and reports, with `result`, `dataset` or `project`; answers its fingerprint, its steps and what is not made again; [Formats](FORMATS.md#certificates)), or a report (`report`: one HTML file of the results chosen, their figures, a table of each other result's numbers traced to the certificate, the methods and the certificate itself, which it checks as it opens; answers how many numbers it traces and the fingerprint; [Formats](FORMATS.md#reports)), or a result package (`package`: the report, each saved view its figures show drawn in advance as tiles with a reader of its own, and the certificate, as a folder or a zip that opens in any browser without SituCraft; [Formats](FORMATS.md#result-packages)). A report and a package leave out this computer's paths, people's names and their notes (and with `names`, the samples' and subjects'), and are written only when none is found in them; each answers what it left out and how many texts were searched. Needs the token |

### Figures

| Action | Arguments | What it does |
| --- | --- | --- |
| `figure` | `name` (by name or ID), `views` (saved views of the tissue, by name or ID, each a panel side by side), `panels` (any panels in order, each an object: `{kind: tissue, view, width_um?, center?, regions?}`, `{kind: qc, dataset, metric, log?, filter?, segmentation?}`, `{kind: map, map, color?}`, `{kind: spatial, analysis}`, `{kind: cohort, comparison, plot, family?, label?, gene?, contrast?, key?}`, `{kind: text, text}`, each naming what it draws by name or ID, any with a `title`), `columns`, `size` (`single`, `onehalf`, `double`, `slide`), `height_mm`, `text` (pt), `width_um` (each tissue panel's width of tissue), `center` (`[x, y]` µm), `scale_bar` (µm), `point_size` (pt), `remove`, `picture`, `picture_width` (1000) | Makes or changes a figure in the Reports view: with `views` or `panels`, a new figure (or the named one made again) of those panels in rows of `columns`, the page as tall as its panels, the tissue panels of a row as tall as each other; without, the named figure's size, text and panels changed. Answers what it shows, each panel's letter, place on the page (pt), region (µm), µm a point, scale bar, the cells and transcripts drawn, and notes; with `picture`, the figure as it draws now (a PNG as a data URL) |
| `write_figure` | `name` (the only one if not given), `path` (absolute), `format` (`svg`, `pdf`, `png`, `json`; from the path's extension if not given), `dpi` (300 if not given), `time` (ISO 8601: the same time, the same bytes), `overwrite` | Writes the figure with its manifest inside (an SVG's metadata, a PDF's attached file, a PNG's text chunk), or the manifest alone as JSON; answers its fingerprint, the digest of each panel's drawing, its inputs and facts. Needs the token |
| `read_figure` | `path` | Reads a figure SituCraft wrote and compares it with the project: whether its manifest is as written, what changed since in each panel's view, dataset and artifacts, and whether each panel draws the same now. Needs the token |
| `verify_certificate` | `path`, or `result`, `dataset` or `project` | Verifies a certificate, a report or a result package (`path`), or what made a result (certified first), as `situcraft verify` does, in SituCraft itself: each step made again from the bundles this library knows (each project's datasets and the examples, found by their files' SHA-256) and every file compared; the page's records checked and its designs, filters and figures computed again in a headless browser. Answers the verdict (verified, differs or not checkable), each bundle and each step's verdict, as verify's `--report` writes them. Needs the token with `path` |

## The documentation's screenshots

`node docs/capture/capture.mjs` captures every picture in `docs/images` through remote control,
in the light and dark themes, from the development bundles (`node validation/fetch.mjs`). With
`--audit` it also checks each scene against axe-core's WCAG 2.1 A and AA rules
(`node validation/fetch.mjs axe-core`) and records the result in `docs/capture/audit.json`.
