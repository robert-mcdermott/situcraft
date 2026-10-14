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

**Status:** in development toward 0.1.0. SituCraft opens a Xenium output bundle (folder or zip),
reads every byte of it with its own readers, validates it (fatal problems, warnings and notes, and
the run's own metrics recomputed from its data) and records it in a project; the derived-data cache
and the tissue viewer come next. See the
[roadmap](situcraft-spec/roadmap.md) and the [changelog](CHANGELOG.md).

## Install

Releases will be one file per platform, installed by a one-line installer that checks its
checksum ([docs/INSTALLING.md](docs/INSTALLING.md)). Until the first release, build from source
with Go (the version in `go.mod` or newer):

```sh
git clone https://github.com/robert-mcdermott/situcraft.git
cd situcraft
go build -o situcraft .
./situcraft --version
```

## Getting started

```sh
situcraft /path/to/Xenium_output_bundle
```

SituCraft opens in a desktop window: a Chromium-family browser (Chrome, Edge, Brave or Chromium)
in app mode with its own profile, or else your default browser. Name bundles (their folder, their
`.zip` or their `experiment.xenium`) or folders of them on the command line, or use **Open a
bundle** in the window. A bundle is read where it is: SituCraft never copies, changes or moves it.
Running `situcraft <paths>` again hands them to the SituCraft already running.

## Options

| Flag | Default | What it does |
| --- | --- | --- |
| `--port` | 8840 | Preferred port; the next free one up to 8859 is used if it is taken |
| `--window` | `app` | `app` (a desktop window), `browser` (the default browser) or `none` |
| `--keep-running` | off | Keep serving after the window is closed |
| `--data-dir` | `<user config dir>/SituCraft` | Where the project library is kept |
| `--cache-dir` | `<user cache dir>/situcraft` | Where derived data go (a zipped bundle's unpacked members; later, tile pyramids and indexes); can be deleted at any time |
| `--host` | 127.0.0.1 | Interface to bind |
| `--dev` | off | Serve `web/` from the working directory (development) |
| `--version` | | Print `situcraft X.Y.Z` and exit |

## Documentation

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
go run . --dev --window none             # serve web/ from disk at http://127.0.0.1:8840
```

The web app is plain ES modules with no build step and no dependencies. CI tests each commit
once. A release is a tag (`v0.1.0`) whose files CI has passed on; the release workflow builds the
six binaries and drafts the release.

## Research use

SituCraft is for research use only. It is not a medical device and makes no diagnostic claims.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE).
