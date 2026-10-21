# Using SituCraft with AI agents (MCP)

`situcraft mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server. An AI agent
such as Claude Code can use it to look at your projects and datasets, show the tissue, read a run's
quality control, segmentations, clusters, labels, spatial analyses and cohort comparisons, and say
what made each result. Given leave, it can also change the project, run SituCraft's jobs and read
or write files.

The agent works in a SituCraft window you see. You watch what it does, every change it makes is in
the project's history with its name, and you can undo it or take over at any time.

- [Requirements](#requirements)
- [What an agent may do](#what-an-agent-may-do)
- [Claude Code](#claude-code)
- [Claude Desktop](#claude-desktop)
- [Other clients](#other-clients)
- [Checking the connection](#checking-the-connection)
- [Your window, or one of its own](#your-window-or-one-of-its-own)
- [What to ask](#what-to-ask)
- [A session](#a-session)
- [Tools](#tools)
- [Results as tables](#results-as-tables)
- [Pictures](#pictures)
- [Files](#files)
- [Recipes](#recipes)
- [How well agents do](#how-well-agents-do)
- [Privacy and security](#privacy-and-security)
- [Troubleshooting](#troubleshooting)
- [How it works](#how-it-works)

## Requirements

- **SituCraft 0.9.0 or later.** `situcraft --version` prints yours. To upgrade, install the latest
  release ([Installing SituCraft](INSTALLING.md)).
- **A browser.** SituCraft opens its window in Chrome, Edge or Brave as an app window, or in your
  default browser.
- **The full path to the program.** Agents start SituCraft themselves, often without your shell's
  `PATH`, so give them the full path. The installers put it here:

  | System | Path |
  | --- | --- |
  | macOS and Linux | `~/.local/bin/situcraft`, for example `/Users/you/.local/bin/situcraft` |
  | Windows | `%LOCALAPPDATA%\Programs\SituCraft\situcraft.exe` |

  Print it with `command -v situcraft` (macOS and Linux) or `(Get-Command situcraft).Source`
  (PowerShell).

The examples below use `/Users/you/.local/bin/situcraft`; use your own path.

## What an agent may do

By default an agent may only read and show: describe the project, list what was made, read
results and their records, write the methods, and show things in the window. Nothing it does then
changes your project or starts a job. You give it more when you start the server, with `--allow`:

| Leave | What it adds |
| --- | --- |
| (none) | Reading and showing: the project, datasets, segmentations, labels, analyses, comparisons, records and methods; views, cells and maps already made |
| `--allow edits` | Changing the project: regions, saved views and filters, names of clusters, curated labels and reviews, decisions, samples and designs, figures |
| `--allow compute` | Running jobs, which make artifacts and add them to the project: segmentations, clusterings, labels, graphs, spatial analyses, cohort summaries and comparisons, opening examples; and stopping, resuming or forgetting jobs |
| `--allow files` | This computer's files by path: opening bundles, reading tables, references, images and figures, writing exports; downloading an example |

Give several at once (`--allow edits,compute`) or all (`--allow all`). The agent's client lists
only the tools it may use, and a call that needs more leave than you gave is refused with the
leave it needs: "cluster_cells needs leave to use compute, which this server was not given: the
user starts situcraft mcp with --allow compute to give it." Some arguments ask for more leave than
their tool: `open_example` with `download`, `open_paths` with `add`, `import_labels` with a `path`,
and `jobs` with `do`.

## Claude Code

Add the server for all your projects, read-only:

```sh
claude mcp add situcraft --scope user -- /Users/you/.local/bin/situcraft mcp
```

Everything after `--` is the command Claude Code runs, so SituCraft's flags go there too:

```sh
claude mcp add situcraft --scope user -- /Users/you/.local/bin/situcraft mcp --allow edits,compute
```

Claude Code has three scopes:

| Scope | Where it applies |
| --- | --- |
| `local` (the default) | You, in the current project only |
| `user` | You, in every project |
| `project` | Everyone working in the repository, through a `.mcp.json` file you commit; Claude Code asks each person to approve it |

For the project scope, `.mcp.json` looks like this:

```json
{
  "mcpServers": {
    "situcraft": {
      "command": "/Users/you/.local/bin/situcraft",
      "args": ["mcp"]
    }
  }
}
```

A committed file holds one path for everyone. It works only when everyone has SituCraft at that
path or on their `PATH`, in which case the command can be just `situcraft`.

`claude mcp list` in a terminal shows whether the server connects; inside a session, `/mcp` shows
its status and tools. To remove it:

```sh
claude mcp remove situcraft --scope user
```

A job such as a refinement or a cohort's steps can take several minutes. If a call times out,
start Claude Code with a longer tool timeout, in milliseconds:

```sh
MCP_TOOL_TIMEOUT=900000 claude
```

## Claude Desktop

Open **Settings → Developer → Edit Config**. That opens `claude_desktop_config.json`, which is in
`~/Library/Application Support/Claude/` on macOS and in `%APPDATA%\Claude\` on Windows. Add
SituCraft under `mcpServers`:

```json
{
  "mcpServers": {
    "situcraft": {
      "command": "/Users/you/.local/bin/situcraft",
      "args": ["mcp"]
    }
  }
}
```

Add the leave you give to `args`, for example `["mcp", "--allow", "edits,compute"]`. On Windows,
double the backslashes in the path:
`"C:\\Users\\you\\AppData\\Local\\Programs\\SituCraft\\situcraft.exe"`. Quit and reopen Claude
Desktop; SituCraft's tools then appear in the chat's tools menu.

## Other clients

Most clients take the same two things: the command (the full path to SituCraft) and its arguments
(`["mcp"]`, and any `--allow`). Where the configuration lives depends on the client, so see its
documentation.

Clients that use an `mcpServers` object, such as Cursor (`~/.cursor/mcp.json`) and Gemini CLI
(`~/.gemini/settings.json`), take the same entry as Claude Desktop above.

VS Code (`.vscode/mcp.json` in a workspace) uses `servers`:

```json
{
  "servers": {
    "situcraft": {
      "type": "stdio",
      "command": "/Users/you/.local/bin/situcraft",
      "args": ["mcp"]
    }
  }
}
```

OpenAI Codex CLI (`~/.codex/config.toml`) uses TOML:

```toml
[mcp_servers.situcraft]
command = "/Users/you/.local/bin/situcraft"
args = ["mcp"]
```

## Checking the connection

Without any client, send the server one message and read its answer:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | /Users/you/.local/bin/situcraft mcp --window none
```

The answer is one line of JSON naming the server (`"name":"situcraft"`) and its version; SituCraft's
banner goes to stderr. No window opens, since no tool was called.

## Your window, or one of its own

When SituCraft is already running with your library and remote control on (`situcraft
--remote-control`), the agent works in its window: you see each step where you are working. When
it is not, `situcraft mcp` starts a SituCraft of its own, with your library, and opens its window
when a tool first needs it; that SituCraft stops when the agent's session ends. Jobs it started and
did not finish are kept, and can be resumed in the Jobs view.

When SituCraft is running with your library without remote control, the agent's SituCraft is a
second program on the same library, and says so on stderr. Start yours with `--remote-control` to
have the agent work in it.

`--window app` (the default) opens an app window from Chrome, Edge or Brave, `--window browser` a
tab of your default browser, and `--window none` nothing: then open the address in SituCraft's
banner (in the client's logs) yourself.

## What to ask

Start with what is in your library, or what is open, and say what you want to know:

- "Which projects do I have, and what was found in the Lung one?" (`list_projects`,
  `describe_project`, `get_result`, without opening anything)
- "How many T cells are within 50 µm of the tumor nest, and which genes stand out in it?"
  (`query_cells`, `summarize_region`, without opening anything)
- "Write the recipe of the Lung's enrichment, so that I can make it again on our cluster."
  (`recipe`; `situcraft run` makes it)
- "What is in SituCraft now? Which datasets, and is each bundle healthy?" (`get_state`,
  `qc_metrics`)
- "List the Lung dataset's segmentations and say how each was made." (`segmentations`, `record`)
- "Show me the tissue around the tumor nest region with CD3E and EPCAM." (`regions`,
  `show_tissue` with `picture`: the agent sees it too)
- "Which labels are enriched near each other in the Lung, and is the answer the same on the
  refined segmentation?" (needs `--allow compute`: `spatial_analysis`, `remake_analysis`)
- "Write the methods for the whole project, with their references." (`methods`)

SituCraft's results are research findings about a tissue, never a diagnosis, and its instructions
to the agent say so.

## A session

The guide's [Agents and scripts](https://robert-mcdermott.github.io/situcraft/docs/agents.html)
page follows an agent through two sessions: on the simulated tissue (its cells labeled by type,
which types neighbor each other, a question of which cells, kept as a filter, and the enrichment's
recipe made again by `situcraft run`) and on the simulated cohort (the one gene the treatment
changes, found with the subject as the replicate). Every call in them is real:
`validation/agent-session.mjs` makes them through `situcraft mcp` in CI, and checks every number
their answers quote.

## Tools

The tools are SituCraft's remote-control actions, under the same names and with the same arguments
(`GET /api/remote/tools` lists them, and the agent's client shows each one's description). Each
carries MCP's hints: whether it only reads (it may still show something in the window), whether it
may replace or remove something, and whether it may reach the network (downloading an example).

**Reading and showing**, always offered:

| Area | Tools |
| --- | --- |
| The library | `list_projects`, `describe_project`, `get_result`, `query_cells`, `query_transcripts`, `summarize_region`, `recipe` (answered without the window) |
| The project | `get_state` (start here), `open_project`, `set_mode`, `focus`, `jobs` (with `do`, needs compute) |
| The tissue | `show_tissue` (with `picture`), `select_cell`, `open_view`, `regions`, `show_alignment` |
| Quality control | `qc_metrics`, `set_qc_map`, `select_cells` |
| Segmentations | `segmentations`, `refinement_preflight`, `show_comparison`, `label_impact` |
| Cell types | `show_map` (with `picture`; making a map where there is none needs compute), `label_evidence`, `review_labels`, `reviews` |
| Cohorts | `samples`, `compare_panels`, `cohort_table`, `cohort_qc`, `show_gene`, `show_sample_cells` |
| The record | `record`, `methods`, `verify_records` |

**Changing the project**, with `--allow edits`: `new_project`, `add_results`, `draw_region`, `edit_region`,
`save_view`, `remove_view`, `save_filter`, `name_clusters`, `curate_labels`, `review`,
`decide_segmentation`, `use_transform`, `set_samples`, `design`, `figure`.

**Running jobs**, with `--allow compute`:

| Area | Tools |
| --- | --- |
| Data | `add_dataset`, `open_example`, `tour` |
| Images | `align_image`, `register_image`, `check_alignment` |
| Segmentations | `expand_nuclei`, `refine_segmentation`, `compare_segmentations`, `plant_errors` |
| Cell types | `cluster_cells`, `instrument_clusters`, `label_cells`, `import_labels` (an instrument's clusters; a table needs files), `repeat_on_segmentation`, `consensus_labels` |
| Spatial | `build_graph`, `spatial_analysis`, `remake_analysis` |
| Cohorts | `cohort_steps`, `summarize_samples`, `compare_abundance`, `compare_expression`, `compare_spatial` |

**This computer's files**, with `--allow files` (and compute where the tool runs a job):
`open_paths`, `add_image`, `import_regions` (and edits), `import_segmentation`,
`reference_labels`, `export_segmentation`, `export`, `write_figure`, `read_figure`,
`verify_certificate`.

A result is the action's sentence and its data: as text (the data's JSON after the sentence) and
as structured content. A result also says what the action changed in the project (`changed`: each
record added, changed or removed, with its id, kind and name), as a script's answer does. An action
SituCraft refuses is a tool error carrying its reason (`No dataset "liver" in the project.`), which
the agent can act on.

The tools SituCraft's product requirements name, and the tool that does each:

| Named in the requirements | SituCraft's tool |
| --- | --- |
| `list_projects`, `describe_project` | the same |
| `list_datasets`, `list_segmentations`, `list_label_sets`, `list_saved_analyses` | `describe_project`; in the open project, `get_state` and `segmentations` |
| `get_dataset_qc` | `qc_metrics` |
| `get_analysis_result` | `get_result` |
| `compare_segmentations` | `compare_segmentations` (with compute), `show_comparison`, `get_result` |
| `query_cells` | the same, with `query_transcripts` |
| `summarize_roi` | `summarize_region` |
| `create_saved_view` | `save_view` (with edits) |
| `get_job_status`, `cancel_job` | `jobs`, and `jobs` with `do` (with compute) |
| `submit_segmentation_job` | `refine_segmentation` (with compute) |

## Results as tables

`describe_project` lists a project's results by id, with what each was made from, without opening
it. `get_result` gives a result's record and its numbers as tables, one table's rows at a time:

| A result | Its tables |
| --- | --- |
| Neighborhood enrichment | `pairs`: each pair of labels' count, expected, permutations' mean and SD, z, p and adjusted p |
| Co-occurrence | `ratios`: each distance and pair's count and ratio |
| Nearest distances | `pairs`: each pair's cells beyond reach, median and mean distance, the permutations', z, p and adjusted p |
| Ripley's K | `pairs` (p and adjusted p) and `curves` (each pair and radius: K, L, the permutations' mean and band) |
| Regions compared | `rows` (each region and the tissue in none, by label: area, count, density and share with intervals) and `ratios` (one row over another) |
| Across an edge | `bands`, `zones` (core, margin, around) and `ratios` (the zones compared, by label) |
| Niches | `niches` (cells, defining labels, leading genes) and `composition` |
| Moran's I | `genes`: I, z, p, adjusted p, the permutations', mean expression, cells detected |
| A clustering | `clusters`: cells, share and leading markers |
| A label set | `labels`: each level's labels, cells and share |
| A cohort's comparison | `results`: every test, as its `results.csv` holds it |
| A cohort summary | `table` (every sample's numbers) and `pseudobulk` |
| Every result | `summary` (its numbers and words) and `summary.<key>` (its lists), as the report shows them |

`table` chooses a table (the first if not given), `offset` and `limit` a page of its rows (100 at a
time; `limit: 0` for up to 10,000). The answer lists every table with its columns and rows. Numbers
are numbers, `null` where there is none.

## Asking which cells

`query_cells` and `query_transcripts` take `where`, a tree of conditions in JSON, never a query
string: SituCraft checks it against its grammar, refuses one it cannot read with the reason, and
evaluates it over the dataset's columns; nothing in it is run. A tree is `{"all": [trees]}`,
`{"any": [trees]}`, `{"not": tree}` or one condition:

| A condition on cells | Keeps a cell |
| --- | --- |
| `{"attribute": "transcripts", "min": 20, "max": 500}` | whose QC attribute is within the bounds (both included, either optional): `transcripts`, `genes`, `total`, `controls`, `area`, `nucleusArea`, `nucleusShare`, `density` or `nuclearShare`; a cell without a value fails |
| `{"attribute": "method", "in": [0]}` | the instrument found by one of these methods (by name or number) |
| `{"gene": "EPCAM", "min": 3}` | with that many of the gene's transcripts (at least 1 when no bound is given) |
| `{"labels": "lab-…", "level": 0, "in": ["T cells"]}` | labeled so by the label set (by name, or number from 0) |
| `{"clusters": "clu-…", "in": ["Cluster 3", 5]}` | in one of the clusters (by name, or number from 1) |
| `{"niches": "spa-…", "in": [1]}` | in one of the niches (numbers from 1) |
| `{"region": "Tumor nest"}` | whose centroid lies in the region (by name or id) |
| `{"distance": "Tumor nest", "min": 0, "max": 50}` | whose centroid is so far from the region's edge, in µm (negative inside) |
| `{"box": [x0, y0, x1, y1]}` | whose centroid lies in the box, in µm |
| `{"filter": "Enough signal"}` | kept by the saved filter (its latest version, or one by id) |

A transcript's conditions are `{"gene": "EPCAM"}` or `{"gene": ["EPCAM", "KRT8"]}`, `region`,
`distance` and `box` as above, `{"assigned": true}` (to a cell of the instrument's segmentation),
and `{"cells": tree}` (assigned to a cell the tree keeps); `min_quality` sets the least quality
(20). A tree on a large dataset's transcripts is bounded by a region, a distance's `max` or a box.

`query_cells` answers how many cells are kept and the SHA-256 of which (as a saved filter records
it), a page of them with the `columns` asked for, and with `by` (a label set, clustering or niches
analysis) how they divide against all the cells. `summarize_region` gives a region's (or box's)
area, cells, density, labels against the whole tissue, and its genes: the most transcripts and
those most above the rest. Both name the segmentation they read (`segmentation`; by default the
one promoted, else the instrument's). With `--allow edits`, `save_filter` with the same `where`
saves the cells as a filter of the instrument's segmentation's cells, which clusterings, graphs and
spatial analyses can then use.

## Pictures

`show_tissue`, `show_map` and `figure` with `picture: true` give a picture too, drawn as a figure
draws it: the tissue as the Viewer shows it, with its scale bar and legend (a JPEG); a clustering's
map, by its clusters or a label set (a PNG); a figure as it draws now (a PNG). `picture_width` sets
its width in pixels (1,000; 300 to 2,000). The agent receives each as an image after the text,
captioned, and the data names it by its number; a script gets it as a data URL. The pictures of
one result are limited to 8 MB in all.

## Files

With `--allow files`, the agent opens and reads files where they lie and writes files at absolute
paths in folders that exist, never over an existing file unless `overwrite` is given, as a script
with remote control's token does ([Remote control](REMOTE-CONTROL.md)).

## Recipes

`recipe` gives a result's recipe: every step from its bundles to it, each the job that made it and
its parameters, as readable JSON, with nothing of this computer's paths in it. Saved to a file (the
agent's own tools write it, or a script's), `situcraft run --recipe … --bundle … --out …` makes it
again without a window, on this computer, a server or a cluster's nodes: on the same bundle, the
same result with the same id, and a certificate `situcraft verify` checks
([Running at scale](https://robert-mcdermott.github.io/situcraft/docs/running.html)).

## How well agents do

The [agent benchmark](../benchmark/README.md) gives agents tasks whose answers are known, on
simulated tissues and cohorts and on the Lung bundle, and grades by code what they answer and what
they leave in the project; the website's
[Agent benchmark](https://robert-mcdermott.github.io/situcraft/benchmark.html) page shows the
results.

## Privacy and security

- Everything runs on this computer: SituCraft answers only requests from this computer, and your
  data never leave it. The only outbound requests are example downloads, which need
  `--allow files`.
- The agent acts in a window you can see, under its name; nothing happens out of sight, and every
  change to the project can be undone.
- An agent may only read until you give it more leave, and its client offers it only the tools it
  may use. The leave is checked by SituCraft, not left to the agent.
- While an agent drives a SituCraft that `situcraft mcp` started, that SituCraft takes no actions
  from scripts, and writes no `remote.json`.
- Every call the agent makes is logged in your library, refusals and failures too: the tool and
  its arguments, whether it was done, refused or failed, what it changed in the project, the ids
  it answered with and how long it took. The drawer's **Calls** tab (`⌘J`) shows and searches the
  log and exports it ([remote control](REMOTE-CONTROL.md#the-log-of-calls)); it never leaves
  this computer.

## Troubleshooting

- **The client shows no SituCraft tools**: it could not start the program. Give the full path (the
  client may not use your shell's `PATH`), check that `situcraft --version` says 0.9.0 or later,
  and try the [check above](#checking-the-connection). Claude Code's `claude mcp list` and Claude
  Desktop's logs say why a server did not start.
- **A tool is missing**: it needs leave you did not give; see
  [What an agent may do](#what-an-agent-may-do).
- **No window opens**: SituCraft opens one when a tool first needs it. With `--window browser` it
  opens in your default browser; with `--window none`, open the address on SituCraft's stderr (in
  the client's logs). A window you close is opened again by the next tool that needs one.
- **A call times out**: a job can take minutes; give your client a longer tool timeout (Claude
  Code: `MCP_TOOL_TIMEOUT`).
- **A file was not written**: give an absolute path in a folder that exists; an existing file is
  replaced only with `overwrite: true`.

## How it works

`situcraft mcp` speaks JSON-RPC on its stdin and stdout, one message per line (protocol versions
2024-11-05 to 2025-11-25); its banner and logs go to stderr. A tool call becomes a remote-control
action: SituCraft forwards it to the open window over Server-Sent Events, the window performs it
with the same code as its own controls, and posts the result back. Actions reach the window one at
a time; a long call reports progress while it runs, and the agent may cancel it. When the agent
closes stdin, a SituCraft that `situcraft mcp` started stops.

The tools, their arguments and their leave are `actions.go`'s, the same list scripts use; a test
keeps this page naming every one. In CI, `validation/agent-session.mjs` drives `situcraft mcp` over
stdio as an agent's client does, with no model: every tool called in a headless window, those that
only read leaving the project as they found it, every change under the agent's name, every call in
the log, and the guide's sessions with their numbers.
