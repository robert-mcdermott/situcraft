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
- Opening paths (`open_paths`), reading files (`import_segmentation`) and writing them (`export`,
  `export_segmentation`) also need the header `X-SituCraft-Token` with the token from
  `remote.json`: another user of a shared computer can reach the port, but not your files.
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
| `open_example` | `example` (`lung`, `breast`, or their names), `download` | Opens an example dataset (in a new project when this one has other datasets); with `download`, downloads it first if needed |
| `tour` | `example`, `name` (`tour`, or a worked example: `compare-expansion`, `planted-errors`), `step`, `run`, `end` | Starts an example's guided tour or worked example (opening the example), or goes to a step; `run` does the step's job (expanding, planting, comparing) and goes on; answers the step and the comparison it shows |

### Seeing the tissue

| Action | Arguments | What it does |
| --- | --- | --- |
| `set_mode` | `mode` | Shows a view: `start`, `data`, `viewer`, `qc`, `segmentation`, `compare`, `celltypes`, `spatial`, `cohort`, `reports`, `jobs` |
| `focus` | `kind` (`dataset`, `filter`, `view`, `local`), `name` | Shows an item in the inspector (a saved view opens); no name clears the focus |
| `show_tissue` | `dataset`, `center` ([x, y] µm), `um_per_pixel` or `width_um` or `whole`, `genes`, `all_transcripts`, `transcripts`, `min_quality`, `point_size`, `channels` ({stain: {visible, color, window, gamma}}), `outlines`, `nuclei`, `fill` (`transcripts`, `total`, `area`, `nucleusArea`, `method`, `none`), `opacity`, `cell` (an ID) | Shows a dataset in the Viewer as asked, everything not given as it is; answers what it shows and what the dataset lacks |
| `select_cell` | `cell` (ID, or row as `#123`), `dataset`, `center` | Selects a cell and answers its detail: position, areas, segmentation method, transcripts by gene |

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
| `refine_segmentation` | `dataset`, `sweeps` (600), `samples` (500), `seed` (20250), `layers` (1) | SituCraft's transcript-informed segmentation as a job, in the Segmentation view: starts from the instrument's cells, records the segmentation (its assignments' probabilities and diagnostics with it) and compares it with the instrument's; answers its summary, convergence, and that comparison |
| `import_segmentation` | `dataset`, `format` (`spatialdata`, `baysor`, `geojson`, `xenium`), `paths`, `units` (`um`, `pixels`), `coordinate_system`, `cells`, `points`, `table`, `cell_column`, `cell_property`, `tool`, `tool_version`, `citation` | Imports another tool's segmentation as a job, in the Segmentation view: checked against the dataset, recorded with its files' SHA-256 (invalid when its check finds a fatal problem). Needs the token |
| `compare_segmentations` | `dataset`, `a`, `b` (names or artifact ids; the default segmentation and the latest other if not given) | Compares two segmentations as a job, in the Segmentation view: the differences by class, each segmentation's measures (with spurious and mutually exclusive co-expression), and the pixels and transcripts that changed; recorded as a comparison artifact |
| `show_comparison` | `dataset`, `comparison` (id or "A and B"), `mode` (`side`, `overlay`, `flicker`), `map`, `classes`, `rank` (`reassigned`, `contamination`, `disagreement`, `low-counts`), `min_changed`, `difference` (a place in the queue) or `cell` (an id) | Opens a comparison in the Compare workspace as asked; answers the review queue's first ten, the difference shown, and the cautions about the measures |
| `decide_segmentation` | `dataset`, `segmentation`, `decision` (`promote`, `keep`, `reject`), `rationale`, `comparison` | Records a decision about a segmentation with its reason, appended to the project; the default cannot be rejected |
| `plant_errors` | `dataset`, `merges` (20), `splits` (20), `missed` (10), `grown` (10), `grow_um` (1), `seed`, `reveal` | For practice: a copy of the instrument's segmentation with known errors planted, simulated (never promoted or exported), recorded as a segmentation artifact with its answer key; `reveal` answers the key |
| `export_segmentation` | `dataset`, `segmentation` (the default if not given), `formats` (`spatialdata`, `anndata`, `parquet`, `csv`, `mex`, `geojson`, `xenium-ranger`), `folder` (absolute), `overwrite` | Writes the segmentation in those formats into the folder as a job, in the Segmentation view, with `situcraft-export.json` (every file by SHA-256, totals and content fingerprints); `xenium-ranger` prepares Xenium Ranger's inputs and command, checked. A planted segmentation is refused. Records the export in the project and answers the manifest. Needs the token |
| `jobs` | `job` (id), `do` (`cancel`, `resume`, `discard`), `show` | The jobs, or one job with its whole log and spec; stops, resumes or forgets one |

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
| `export` | `what` (`project`, `archive`, `methods`), `path` (absolute), `overwrite`, `dataset` | Writes the project document (JSON), a project archive (`.scz`) or the methods (Markdown). Needs the token |

## The documentation's screenshots

`node docs/capture/capture.mjs` captures every picture in `docs/images` through remote control,
in the light and dark themes, from the development bundles (`node validation/fetch.mjs`). With
`--audit` it also checks each scene against axe-core's WCAG 2.1 A and AA rules
(`node validation/fetch.mjs axe-core`) and records the result in `docs/capture/audit.json`.
