# SituCraft

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/viewer-genes-dark.webp">
  <img alt="SituCraft's Viewer: transcripts of four genes over the DAPI stain and cell outlines of 10x Genomics' Xenium human Lung example, one cell's transcripts by gene in the inspector" src="docs/images/viewer-genes-light.webp">
</picture>

SituCraft is an open-source, local-first workbench for imaging-based spatial transcriptomics,
starting with 10x Genomics Xenium output bundles. It makes cell segmentation, pathology review and
tissue-level spatial analysis one reproducible workflow:

- open and validate a bundle;
- see the tissue, its transcripts and its cells at any zoom;
- segment cells with built-in transcript-informed segmentation, and compare segmentations and
  choose one with evidence;
- register H&E;
- type cells, find niches and compare samples;
- export open formats with complete provenance.

SituCraft runs as one self-contained program on macOS, Linux and Windows. It opens as a desktop
window, needs no other software, and keeps your data on your computer.

**Status:** 0.2.0. SituCraft opens a Xenium output bundle (folder or zip), reads every byte of it
with its own readers, validates it (fatal problems, warnings and notes, and 22 of the run's
recorded values recomputed from its data) and records it in a project. Its tissue viewer shows the
morphology stains, the transcripts of chosen genes and the cells at any zoom, every image sample
exactly as recorded; its QC view explains the run's metrics, maps quality over the tissue and keeps
cell filters as reproducible versions. Since 0.2.0, segmentations are artifacts with their lineage:
the instrument's, SituCraft's own nucleus expansion, and other tools' imported from SpatialData,
Baysor, GeoJSON or Xenium Ranger. Any two are compared cell by cell, with measures of transcripts
given to the wrong cell; a workspace shows both on the tissue, ranks their differences for review,
and records the decision that promotes one, with its reason. Any segmentation exports to
SpatialData, AnnData, Parquet, CSV, Matrix Market and GeoJSON, or is prepared for Xenium Ranger,
every file listed by its SHA-256. Programs drive the window by remote control. Two public example
datasets download in a click, each with a guided tour and a worked example. Transcript-informed
segmentation built in comes next (0.3.0): see the [roadmap](situcraft-spec/roadmap.md) and the
[changelog](CHANGELOG.md).

## A look around

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/qc-dark.webp">
  <img alt="SituCraft's QC view of the Lung example: the run's metrics, 20 recomputed from the data and reproduced, cells colored by their gene transcripts, and their distribution" src="docs/images/qc-light.webp">
</picture>

The QC view: every metric the run recorded, explained, with SituCraft's own value beside it where
it can be recomputed from the data; quality mapped over the tissue; plots linked to the tissue.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/viewer-methods-dark.webp">
  <img alt="SituCraft's Viewer: cells of the Breast example colored by the method that found them, from the boundary or interior stains or by expanding a nucleus, with a legend counting each" src="docs/images/viewer-methods-light.webp">
</picture>

The Breast example's cells, colored by how the instrument found each one.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/workspace-dark.webp">
  <img alt="The Compare workspace: the Breast example's instrument segmentation and a 5 µm nucleus expansion side by side on the tissue, cells filled by the class of their difference, the review queue's first split chosen and described in the inspector" src="docs/images/workspace-light.webp">
</picture>

The comparison workspace: two segmentations on the tissue with one camera, their differences
ranked for review, and the decision recorded with its reason.

More: [checking a bundle](docs/images/data-light.webp), [the tissue's stains](docs/images/viewer-light.webp),
[transcripts outside cells](docs/images/qc-outside-light.webp), [filters as versions](docs/images/qc-filter-light.webp),
[saved views](docs/images/saved-views-light.webp), [the methods paragraph](docs/images/methods-light.webp),
[a guided tour](docs/images/tour-light.webp), [a segmentation's lineage](docs/images/segmentation-light.webp),
[nucleus expansion compared with the instrument's segmentation](docs/images/expansion-light.webp),
[a segmentation imported from SpatialData](docs/images/import-light.webp),
[two segmentations compared](docs/images/comparison-light.webp),
[the comparison workspace's disagreement map](docs/images/workspace-map-light.webp),
[a worked example finding planted errors](docs/images/worked-light.webp),
[exporting a segmentation](docs/images/export-light.webp),
[the Jobs view](docs/images/jobs-light.webp).

## Install

One file per platform, installed by a one-line installer that checks its checksum
([docs/INSTALLING.md](docs/INSTALLING.md)). On macOS and Linux:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.ps1 | iex
```

Or build from source with Go (the version in `go.mod` or newer): `go build -o situcraft .`

## Getting started

```sh
situcraft                                # then try an example from the start page
situcraft /path/to/Xenium_output_bundle  # or open your own
```

SituCraft opens in a desktop window: a Chromium-family browser (Chrome, Edge, Brave or Chromium)
in app mode with its own profile, or else your default browser. Name bundles (their folder, their
`.zip` or their `experiment.xenium`) or folders of them on the command line, or use **Open a
bundle** in the window. A bundle is read where it is: SituCraft never copies, changes or moves it.
Running `situcraft <paths>` again hands them to the SituCraft already running.

[Getting started](docs/GETTING-STARTED.md) walks through the examples, the Data view, the Viewer,
QC, saved views and the methods paragraph.

## Options

| Flag | Default | What it does |
| --- | --- | --- |
| `--port` | 8840 | Preferred port; the next free one up to 8859 is used if it is taken |
| `--window` | `app` | `app` (a desktop window), `browser` (the default browser) or `none` |
| `--keep-running` | off | Keep serving after the window is closed |
| `--data-dir` | `<user config dir>/SituCraft` | Where the project library is kept |
| `--cache-dir` | `<user cache dir>/situcraft` | Where derived data go (image tiles, transcript indexes, tiled outlines, a zipped bundle's unpacked members); can be deleted at any time |
| `--cache-limit` | `10GB` | The size the cache is kept within, removing what was used least recently (`500MB`, `20GB`; `0` for no limit) |
| `--examples-dir` | `<data dir>/examples` | Where example datasets are downloaded, when you ask |
| `--remote-control` | off | Let programs on this computer drive the window ([remote control](docs/REMOTE-CONTROL.md)) |
| `--host` | 127.0.0.1 | Interface to bind |
| `--dev` | off | Serve `web/` from the working directory (development) |
| `--version` | | Print `situcraft X.Y.Z` and exit |

## Documentation

- [Getting started](docs/GETTING-STARTED.md): from the examples to a saved, reproducible view.
- [Formats](docs/FORMATS.md): what SituCraft reads and writes.
- [Remote control](docs/REMOTE-CONTROL.md): driving the window from scripts and notebooks.
- [Installing](docs/INSTALLING.md): release files, installers, upgrading, uninstalling.
- [Requirements](situcraft-spec/requirements.md): what SituCraft must do.
- [Design](situcraft-spec/design.md): how it is built and why.
- [Roadmap](situcraft-spec/roadmap.md): the order of the work, wave by wave (wave *N* is release
  0.*N*.0).
- [Research](situcraft-spec/research.md): the methods, formats and datasets behind it, with
  citations.
- [Conventions](situcraft-spec/conventions.md): code, science and terminology rules.

## Development

```sh
go test -race ./...                      # the Go host
node --test "web/lib/**/*.test.mjs"      # the browser modules
node validation/run.mjs                  # checks against references (validation/README.md)
node validation/run.mjs viewer           # the tissue viewer in headless Chrome against its data
node docs/capture/capture.mjs --audit    # the screenshots, through remote control, checked by axe-core
go run . --dev --window none             # serve web/ from disk at http://127.0.0.1:8840
```

The web app is plain ES modules with no build step and no dependencies. CI tests each commit
once. A release is a tag (`v0.2.0`) whose files CI has passed on; the release workflow builds the
six binaries and drafts the release.

## Example data

The example datasets are 10x Genomics' Xenium human Lung and Breast 2-FOV output bundles, used under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) from
[10x Genomics' example data](https://www.10xgenomics.com/support/software/xenium-onboard-analysis/latest/resources/xenium-example-data).
SituCraft downloads them from 10x Genomics only when asked; they are not part of SituCraft or this
repository. The screenshots in this documentation show these data.

## Research use

SituCraft is for research use only. It is not a medical device and makes no diagnostic claims.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE).
