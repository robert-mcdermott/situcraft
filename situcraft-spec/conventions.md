# SituCraft engineering conventions

SituCraft is a workbench for imaging-based spatial transcriptomics, starting with 10x Genomics
Xenium. A Go program, built on the standard library, serves an embedded web app and does the heavy
data work: reading bundles, decoding images, indexing, segmentation and comparison. The browser
renders and runs cell-level analytics. License: Apache-2.0. No third-party runtime dependencies,
in Go or JavaScript.

SituCraft is a sibling of Proteoscope, CytoWeave and MaveScape (`~/mycode/…`). It copies their
host, security model, conventions and test approach on purpose, so that one person can maintain
all four. A copied file notes its origin in its header comment ("Adapted from MaveScape 0.8.0
web/lib/zip.js").

## Layout

```
main.go, security.go, local.go, store.go,      Go host (embeds web/), adapted from MaveScape
records.go, window.go
api_*.go                                       HTTP handlers: tiles, points, polygons, cells, jobs
remote.go, actions.go, output.go,              automation: remote-control hub and its actions,
connection.go, mcp.go, run.go, verify.go       remote.json, exports to paths; MCP; headless runs
internal/codec/      zstd, lz4, blosc, snappy
internal/parquet/    Parquet reader and writer
internal/zarr/       Zarr v2/v3 stores (directory, zip), reader and writer
internal/tiff/       TIFF/BigTIFF/OME-TIFF reader and writer
internal/jp2k/       JPEG 2000 Part 1 decoder
internal/xenium/     bundle scanning, version detection, validation, typed access
internal/cache/      derived data: tile pyramids, transcript index, geometry tiles
internal/segment/    nucleus expansion, transcript-informed segmentation
internal/compare/    cell matching, difference classes, metrics
internal/jobs/       durable background jobs
internal/simulate/   seeded simulated tissues
web/index.html, web/styles.css, web/app.js     app shell
web/lib/*.js         pure modules: no DOM, no globals, importable by Node and by workers
web/lib/*.test.mjs   node:test unit tests, one file per module
web/render/*.js      WebGL2 renderer (touches only its canvas)
web/ui/*.js          DOM components (only these touch document/window)
web/workers/*.js     module workers
validation/          checks against independent references and known truth
validation/reference/  committed reference numbers, each with the script that made it
docs/                installation, formats, MCP, network
situcraft-spec/      requirements, design, roadmap, research, these conventions
```

## Names

- Product: **SituCraft**. Executable and Go module: `situcraft`.
- Default port 8840 (tries 8840–8859). Proteoscope 8765–8814, CytoWeave 8770–8789, MaveScape
  8820–8839.
- Data directory `<UserConfigDir>/SituCraft`; derived-data cache `<UserCacheDir>/situcraft`.
- Headers `X-SituCraft-Token`, `X-SituCraft-Cache`.
- Project archive extension `.scz` (a ZIP of the project manifest, events and small artifacts;
  never the source bundle).

## Go

- Standard library only. `gofmt`; `go vet` clean; `go test -race ./...` passes.
- Readers take an `io.ReaderAt` and a size, so the same code reads files, zip members and
  in-memory test data. Readers never load a whole large file to answer a small question.
- Bundle sources are opened read-only. Nothing under a source bundle is ever written, renamed or
  deleted.
- Untrusted input (every file) is bounds-checked. Sizes declared in headers are checked against
  the actual size and against limits before allocation. Every decoder has a fuzz test
  (`go test -fuzz`) with a committed corpus of small cases.
- Long operations take a `context.Context`, check it often, and report progress through a
  callback. Errors a user can fix name the file, member, row, column, tile or cell.
- Parallel code is deterministic: work is split into fixed blocks, each with its own seeded
  random stream (`internal/random`), and results are combined in a fixed order.
- Processes are started only with argument arrays (`exec.Command(name, args...)`), never via a
  shell.

## JavaScript

- Plain ES modules, modern JavaScript, no build step, no TypeScript. Two-space indent, single
  quotes, semicolons, trailing commas in multi-line literals.
- Typed arrays for anything per cell or per transcript. No per-cell object allocation in hot
  loops.
- Code whose numbers reach an analysis result, export or methods text uses MaveScape's
  `web/lib/dmath.js` for transcendental functions, so results are the same bits in every engine.

## Science

- **Cite what you implement.** A file implementing a published method names it in its header
  (authors, year, journal, DOI), and `research.md` lists it. Where the publication leaves a choice
  open, the code comment and `research.md` say "SituCraft's choice" and why.
- **Units are explicit.** Field names and metadata carry units (`x_um`, `area_um2`, `pixel_size_um`).
  Pixels and microns are never inferred from values. Every spatial object names its coordinate
  system.
- **Identifiers are stable.** Never row numbers. Vendor IDs are kept as given. Derived IDs are
  namespaced by dataset and segmentation. A vendor cell ID is never confused with a derived
  segmentation's cell ID.
- **Missing is not zero.** An unobserved value is missing; an explicit 0 is 0.
- **Seeds.** Every stochastic computation takes a seed (default fixed) and records it. Results are
  deterministic for a seed.
- **Defaults are serialized.** A job spec records every effective parameter after defaults are
  resolved, and the method version.
- **Artifacts are immutable.** Outputs are written to a temporary path, validated, then promoted.
  Only `complete` artifacts can be inputs or defaults.
- **Cells are not replicates.** No inferential statistic across a cohort until the design names
  its biological replicate unit. Per-cell tests are labeled exploratory.

## Terminology

SituCraft is for research use. It is not a diagnostic device.

| Use | Never |
| --- | --- |
| "segmentation", "cell boundaries", "transcript assignment" | "true cells", "ground truth" (except for simulations) |
| "promoted segmentation" (the project's chosen default, with its rationale) | "correct segmentation" |
| "label", "cell type (predicted / curated / consensus)", "confidence" | a diagnosis |
| "neighborhood enrichment", "co-occurrence", "proximity" | "interaction", "signaling" (unless qualified as hypothesis) |
| "ligand–receptor co-expression (hypothesis-generating)" | "communication" as a finding |
| "simulated" on anything generated | presenting simulated data as biology |
| "Xenium output bundle", "reads Xenium bundles" | implying endorsement by 10x Genomics |

## Tests and validation

- `go test ./...` and `node --test "web/lib/**/*.test.mjs"` are fast (seconds), deterministic, and
  check numbers against independently known values: hand-computed, published, simulated truth, or
  an independent tool's output committed under `validation/reference/` with the script that
  produced it and the tool's version.
- Readers are checked against independent readers on every development file. Segmentation is
  checked against simulated truth and recorded reference numbers. Statistics are checked against
  reference libraries and analytic cases.
- Anything that needs large data, external tools or more than a few minutes runs outside
  per-commit CI (`design.md`, "Testing and CI").
- A slice is done when it runs, its tests and validation pass, its documentation is written, and
  `CHANGELOG.md` says what it added.
