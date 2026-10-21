# R and Python clients

SituCraft started with `situcraft --remote-control` accepts actions from programs on this computer:
every action an AI agent has (open bundles, segment, cluster and label cells, ask which cells meet
conditions, run spatial analyses and cohort comparisons, read results as tables, show the tissue,
write files) is performed by the open SituCraft window, so you watch the script work, and every
change goes into the project's history ([remote control](../docs/REMOTE-CONTROL.md)).

These packages make each action a function, with its arguments and help:

| | R | Python |
| --- | --- | --- |
| Package | [`r/`](r/) (`situcraft`, needs curl and jsonlite) | [`python/`](python/) (`situcraft`, the standard library only; pandas for data frames) |
| Install | `remotes::install_github("robert-mcdermott/situcraft", subdir = "clients/r")` | `pip install "git+https://github.com/robert-mcdermott/situcraft#subdirectory=clients/python"` |
| Connect | `sc_connect()` | `sc = situcraft.connect()` |
| An action | `sc_cluster_cells()` | `sc.cluster_cells()` |
| A table | `as.data.frame(sc_get_result(id, limit = 0))` | `sc.get_result(id, limit=0).frame()` (or `.records()`) |
| A picture | `sc_show_tissue(file = "tissue.jpg")` | `sc.show_tissue(file="tissue.jpg")` |
| Any action by name | `sc_call("qc_metrics")` | `sc.call("qc_metrics")` |
| The actions | `sc_tools()` | `sc.tools()` |

They are not on CRAN or PyPI. Their version is SituCraft's; use the clients of the SituCraft
release you run.

```r
library(situcraft)
sc_connect()
sc_open_example("simulated")
sc_cluster_cells()
labels <- sc_label_cells(markers = "simulated")
cells <- as.data.frame(sc_query_cells(list(labels = labels$data$id, `in` = list("immune-like")), limit = 0), key = "rows")
sc_show_tissue(fill = "labels", file = "tissue.jpg")
```

```python
import situcraft

sc = situcraft.connect()
sc.open_example("simulated")
sc.cluster_cells()
labels = sc.label_cells(markers="simulated")
cells = sc.query_cells({"labels": labels["id"], "in": ["immune-like"]}, limit=0).frame("rows")
sc.show_tissue(fill="labels", file="tissue.jpg")
```

## Connecting

With `--remote-control`, SituCraft writes `remote.json` to its data folder: its address and the
token that opening and writing files need, readable by you only, removed when SituCraft stops.
`sc_connect()` and `situcraft.connect()` read it. Otherwise they use, in order: the `url` and
`token` you give; `SITUCRAFT_URL` and `SITUCRAFT_TOKEN`; `remote.json` in `SITUCRAFT_DATA_DIR` (for
SituCraft started with `--data-dir`) or the default data folder; `http://127.0.0.1:8840` without a
token. `client` names your script in the project's history and the library's log of calls.

## Arguments and results

Arguments are those of the actions ([remote control](../docs/REMOTE-CONTROL.md#actions),
`sc_tools()`); the required ones come first and can be given by position. In Python, an argument
named like a keyword ends with `_` (`draw_region`'s `class` is `class_`); in R, `in` is written
`` `in` `` inside a list. Every result has the action's `message` (a sentence), its `data`, and what
it `changed` in the project (each `{id, kind, name, change}`):

- **Tables.** SituCraft gives a table as its `columns` and `rows` (a result's numbers from
  `get_result`, the cells `query_cells` keeps, a composition), or as a list of records (the
  results `describe_project` lists). `as.data.frame(result)` and `result.frame()` take the first
  table in the data, a table of columns and rows before a list of records; name one with `key`
  (`key = "rows"`, or `"genes.enriched"` for one inside another). Each column keeps its type:
  numbers as numbers, `NA` (R) or `None`/`NaN` (Python) where SituCraft gave null.
- **Pictures.** `show_tissue`, `show_map` and `figure` give a picture with `picture`: `file` asks
  for it and saves it (the tissue's a JPEG, a map's and a figure's a PNG); `sc_save_image()` and
  `result.save_image()` write one, `sc_images()` and `result.images()` give them all.
- **Errors.** An action SituCraft refuses is an error carrying its reason, the HTTP status and the
  action (`situcraft_error` in R, `SituCraftError` in Python).

## Examples

Two examples in [`examples/`](examples/), each run in CI against a build of SituCraft, its checks
(`assert`, `stopifnot()`) part of the tests:

- [`simulated-tissue.ipynb`](examples/simulated-tissue.ipynb), a Python notebook (pandas and
  matplotlib): the simulated tissue opened and its run's metrics checked; its cells clustered and
  labeled from its marker set, and the labels measured against the true types the bundle's truth
  folder holds (a confusion matrix, made with `query_cells`); the labels' neighborhood
  enrichment as a heatmap; the tissue filled by label as a picture; and the enrichment's recipe
  saved, which `situcraft run` makes again.
- [`simulated-cohort.R`](examples/simulated-cohort.R), an R script: the simulated cohort's design
  (the subject the replicate), the same steps on every sample, the samples summarized and each
  label's expression compared between the conditions; the 424 tests as a data frame, the planted
  change (SIM033 in the epithelial-like cells) found and nothing else, a volcano, the tests as
  CSV and SituCraft's methods.

Start SituCraft with `--remote-control`, then run them where they may write their results: open the
notebook in Jupyter, and `Rscript simulated-cohort.R`.

## For developers

The functions are generated from [`tools.json`](tools.json), the program's actions, which a Go
test keeps equal to `actions.go`. After changing an action, or SituCraft's version:

```bash
go test -run TestTheClientsToolListIsCurrent -update
node clients/generate.mjs
```

`generate.mjs` also sets both packages' versions to `main.go`'s; `--check` fails when anything is
out of date (CI runs it, and the release). The tests run each client against SituCraft in headless
Chrome: the simulated tissue opened, clustered and asked about through the client, its tables
as data frames, the tissue and a map written as files, errors carrying SituCraft's reason,
results equal to the HTTP API's. Then the examples run, the notebook executed with nbclient:

```bash
node clients/test-clients.mjs            # both; --python or --r for one
node clients/test-clients.mjs --update   # and write the notebook's new outputs into it
```

They need Go, Chrome (`CHROME=path`), Python 3 (`PYTHON=path`; pandas for the data frames, else
that test is skipped; nbclient, ipykernel and matplotlib for the notebook) and R with curl, jsonlite
and testthat. An example that cannot run is skipped, saying why; `--require` makes it a failure, as
CI runs it. The R package also passes `R CMD check`.
