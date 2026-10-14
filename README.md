# SituCraft

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

**Status:** 0.1.0, the first release. SituCraft opens a Xenium output bundle (folder or zip),
reads every byte of it with its own readers, validates it (fatal problems, warnings and notes, and
22 of the run's recorded values recomputed from its data) and records it in a project. It builds
the dataset's derived data once, and its tissue viewer shows the morphology stains, the
transcripts of chosen genes and the cells at any zoom, every image sample exactly as recorded. Its
QC view explains the run's metrics, maps quality over the tissue, links plots to the tissue, and
keeps cell filters as reproducible versions. Saved views reopen exactly, from the project or a
link; a methods paragraph is written from what the project records; projects travel as archives
with their bundles. Two public example datasets download in a click, each with a guided tour.
Segmentation comes next (0.2.0 compares segmentations, 0.3.0 builds one in): see the
[roadmap](situcraft-spec/roadmap.md) and the [changelog](CHANGELOG.md).

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
| `--host` | 127.0.0.1 | Interface to bind |
| `--dev` | off | Serve `web/` from the working directory (development) |
| `--version` | | Print `situcraft X.Y.Z` and exit |

## Documentation

- [Getting started](docs/GETTING-STARTED.md): from the examples to a saved, reproducible view.
- [Formats](docs/FORMATS.md): what SituCraft reads and writes.
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
go run . --dev --window none             # serve web/ from disk at http://127.0.0.1:8840
```

The web app is plain ES modules with no build step and no dependencies. CI tests each commit
once. A release is a tag (`v0.1.0`) whose files CI has passed on; the release workflow builds the
six binaries and drafts the release.

## Example data

The example datasets are 10x Genomics' Xenium human Lung and Breast 2-FOV output bundles, used under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) from
[10x Genomics' example data](https://www.10xgenomics.com/support/software/xenium-onboard-analysis/latest/resources/xenium-example-data).
SituCraft downloads them from 10x Genomics only when asked; they are not part of SituCraft or this
repository.

## Research use

SituCraft is for research use only. It is not a medical device and makes no diagnostic claims.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE).
