# Validation

SituCraft's results are checked against independent references and known truth. Unit tests
(`go test ./...`, `node --test "web/lib/**/*.test.mjs"`) check functions in isolation; the checks
here run on real data or whole interfaces.

| Check | What it compares | Data |
| --- | --- | --- |
| `node validation/run.mjs accessibility` | WCAG AA contrast of every text-on-surface pair in both themes, with color-vision-friendly colors off and on; palettes, status colors and diverging maps apart in protanopia, deuteranopia and tritanopia (Machado et al. 2009, CIEDE2000) | `web/styles.css` |
| `go test ./validation/` (readers) | Every Parquet column of the development bundles as DuckDB reads it; every Zarr array as zarr-python and numcodecs read it; every TIFF's structure (images, pyramid levels, tiles, compression) and the pixels of each plane's smallest (lossless) level as tifffile and imagecodecs read them; and every image tile decoded, with the time taken | the two 2-FOV bundles |
| `go test ./internal/xenium/` (bundles) | Both development bundles validate with exactly the expected findings and reproduce 22 of the run's recorded values (research.md §1.3b); the Lung bundle zipped validates as the folder does; 13 broken bundles (the Lung bundle with one change each) raise exactly their findings; nothing in a bundle changes | the two 2-FOV bundles |
| `go test ./internal/cache/` (derived data) | Every tile of every image level equals the bundle's own level decoded directly; every transcript is in one tile with its values; every gene's density grid sums to its count at every level and level 1's bins equal a recount; level 0's polygons equal the boundary files' and every level holds each once, within its simplification tolerance; builds stopped in each entry resume to byte-identical entries; an unchanged bundle reopens without reading or building; the zipped Lung bundle reuses the folder's entries; sizes within 500 MB for both, and build times within budget | the two 2-FOV bundles |
| `node validation/run.mjs viewer` (the tissue viewer and QC) | In headless Chrome: every channel drawn at levels 0, 1 and the coarsest, read back before any window or color, equals the derived data's tiles sample for sample (across tile edges and at the image's edges); the transcripts loaded for a region equal the program's query of the index; frames per second while panning and the time to draw a view, against budgets (30 fps where there is a GPU; 500 ms); in the QC view, every recomputable metric agrees, brushing chooses exactly the cells in range, the choice shows in the Viewer, a saved filter keeps the cells (and SHA-256) an independent evaluation gives, and every QC map draws; the examples are listed and none is downloaded unasked; every step of both tours opens with its genes; the project saved by 0.1.0 (`validation/projects/`) opens, finds its moved bundle again, its filters keep exactly the cells they recorded, and its saved views reopen exactly, from the project and from a link; each bundle's instrument segmentation is registered as an artifact, every file's SHA-256 as Node computes it, promoted as the default, and the 0.1.0 project gets the same artifact; through remote control (the program started with `--remote-control`), a program's actions show the tissue, save and reopen a view, save a filter keeping the cells the 0.1.0 project's identical filter recorded, write the methods, and write a file only with the token; `expand_nuclei` grows the Lung bundle's nuclei by 5 µm nearly as the instrument's segmentation did, the expansion is recorded and shown with what differs, and asking again answers the same artifact without a job; `import_segmentation` imports the Lung bundle into itself only with the token, recorded as imported, and refuses the Breast bundle as another run; `compare_segmentations` pairs every Lung cell one to one with its 5 µm expansion, with the expansion's own counts, recorded and shown; `show_comparison` ranks the review queue as asked and draws both segmentations with one camera side by side, overlaid and in turns; `decide_segmentation` records decisions only as their rules allow; the Lung worked example plants errors, compares, and every planted error is the difference it should be (by contamination, a merge first), a planted segmentation is never promoted or exported; `export_segmentation` writes only with the token, recorded in the project, and its SpatialData store imports back as the instrument's segmentation; the project saved by 0.2.0 reopens and its artifacts are made again with the same checksums; with the Breast bundle, its worked example gives 263 splits, 128 merges and 119 cells only in the instrument's. Set `CHROME` to a browser, and `SITUCRAFT_CHROME_ARGS` to more of its flags (CI: software WebGL) | the 2-FOV bundles present, Chrome |
| `node docs/capture/capture.mjs --audit` (the documentation's screenshots) | Every scene, driven through remote control in headless Chrome in both themes, against axe-core's WCAG 2.1 A and AA rules (`docs/capture/audit.json`) | the 2-FOV bundles, axe-core (`node validation/fetch.mjs axe-core`), Chrome |
| `go test ./internal/view/` (the viewer's and QC's queries) | A region's transcripts against a scan of transcripts.parquet; density by gene against each gene's count; every cell's gene transcripts counted from the index against cells.parquet; a cell's boundary against cell_boundaries.parquet; the QC maps' groups of transcripts summed over the tissue against the run's assigned and Q20 shares; the cells table's genes per cell against the recorded median; the descriptor | the two 2-FOV bundles |
| `go test ./internal/segment/...` (nucleus expansion) | The Lung bundle's own 5 µm nucleus expansion reproduced: the same 11,898 cells, paired one to one; at least 98% of pixels and 98.5% of transcripts in the same cells; cell areas' median difference under 0.5%, at least 96% within 5% and 99.5% within 10%; the assigned fraction at most 0.5 points above the instrument's; every transcript given differently explained, one by one, as the expansion's rim (4.5–5 µm from its nucleus), a boundary between neighbours (within 0.4 µm of equidistant) or the instrument's record off its own mask (research.md §3.1). The Lung nucleus polygons drawn agree with the mask on at least 98% of pixels; Breast's nuclei, which do not pair one to one with its cells, all grow into cells, and its cells are all accounted for | the 2-FOV bundles |
| `go test ./internal/simulate/` (simulated tissues) | Goodness of fit, at the 0.001 level with fixed seeds, of what the simulator draws against what it specifies: every count against its negative binomial, molecules within their cells and depths and the nuclear share, the displacement mixture, quality scores, background, cell areas and types (research.md §3.4); the small preset's bundle validates with no warning and 16 of 16 metrics agreeing, its truth imports as a Xenium Ranger bundle and the instrument's cells and SituCraft's expansion compare with it; the same bundle, byte for byte, on any number of workers and on arm64 and amd64 (a pinned digest) | none (made by the tests) |
| `go test ./internal/segment/refine/` (the transcript-informed segmentation) | The model: every conditional update against its analytic posterior (Kolmogorov–Smirnov, χ²) or the numerically integrated one (the shapes); the whole parameter sweep by Geweke's joint distribution test (4,000 chains of 40 sweeps); every voxel move's change against the recomputed target; the attributions' shares; type recovery from counts. The sampler: exact detailed balance on every reachable state of two enumerable lattices (2D and 3D), and chains from exact draws of π staying distributed as π (single steps and the parallel sweep); every proposed move along a walk on a larger lattice balanced against the target; repositioning against its posterior; a run's digest the same on 1, 4 and 16 threads and on arm64 and amd64. Recovery: on a simulated tissue, more transcripts in their true cells and a higher median IoU than the instrument's nucleus expansion (research.md §3.2; `REFINE_PRESET=standard` for the standard tissue's reference numbers) | none (made by the tests) |
| `go test ./internal/segment/imports/` (imports) | The Lung bundle imported into itself as an output bundle: the instrument's segmentation exactly (raster, every assignment, every cell's count); its own polygons and assignments written as GeoJSON with a table and as a Baysor-style table without ids: every assignment the instrument's, the polygons drawn agreeing with the mask on at least 85% of pixels (94.33%); the Breast bundle refused as another run. The Lung bundle as spatialdata-io writes it (`uv run --with 'spatialdata-io==0.7.1' --with 'spatialdata==0.8.0' python -I validation/spatialdata.py`; weekly in CI): the instrument's segmentation exactly, read in pixels, and refused in µm with the units that fit named. Hand-made fixtures written by spatialdata (format 3 and, 0.2.6, format 2), geopandas, pyarrow, pandas and zarr-python: every cell's pixels and every transcript's cell as the scene defines them, and each finding (a transcript the dataset lacks, transcripts elsewhere, cells without polygons, outlines cut at the image's edge) | the 2-FOV bundles; the fixtures |
| `go test ./internal/export/` (exports) | A fixture segmentation exported in every format and read back by SituCraft's readers: the SpatialData store imports as the same segmentation (every pixel, transcript and count), the GeoJSON polygons with the Parquet or CSV transcripts give every transcript's cell, the tables, AnnData, Matrix Market and masks hold what it does; the same segmentation gives the same files; folders replaced only as the rules allow; Xenium Ranger's checks. The Lung bundle's own segmentation in every format, imported back from SpatialData exactly, its masks the image's size, and what `validation/reference/exports.json` recorded the Python libraries reading (`SITUCRAFT_EXPORT_DIR` keeps the export for `read_exports.py`) | the Lung bundle; `exports.json` |
| `go test ./internal/segment/plant/` (planted errors) | On hand-made tissue and on the Lung bundle's segmentation, every planted error is found by the comparison as what it is (merged, split, only in A, expanded), every merged cell mixing lineage markers, and nothing else differs; the same seed plants the same errors | the Lung bundle |
| `go test ./internal/compare/` (comparisons) | Hand-drawn segmentations with every class (one-to-one, a cell shifted a pixel: IoU 0.5 and a boundary moved 0.5 µm; a boundary between neighbours moved: contracted and expanded; split, merged, rearranged; only in one; assignment-only; a shared nucleus), each count and measure worked out by hand; the spurious co-expression of a planted leak (the nuclei 1, more leak more), down-sampling reproducible; the MECR by hand; the comparison's files read back and their check finding damage. On the bundles, the instrument's segmentation against the 5 µm expansion: Lung pairs all 11,898 cells one to one, with the pixels (98.0629%) and the 8,724 transcripts the expansion's own comparison counted; Breast accounts for every cell of both in its classes | the 2-FOV bundles; none for the fixtures |
| `go test ./internal/anndata/ ./internal/zarr/` (AnnData, sharding, text) | AnnData stores written by anndata in Zarr formats 3 (sharded) and 2, X as sparse rows, sparse columns and dense: every column and row sum as written; Zarr text (VLenUTF8 in format 2, the string type in format 3, plain and sharded) and shards (index at the end and at the start) as zarr-python wrote them; damaged text and shards refused | the fixtures |
| `go test ./internal/jp2k/` (real tiles) | The JPEG 2000 tiles of the development bundles against OpenJPEG (`opj_decompress`): identical for lossless 5/3 tiles, within ±1 at every pixel for lossy 9/7 ones (every tile of the morphology-focus images and every 25th of the z-stacks; `JP2K_ZSTACK_ALL=1` for all) | the two 2-FOV bundles, OpenJPEG |

## Data

`node validation/fetch.mjs [data set …]` downloads the public data sets in `sources.json` into
`validation/cache/` (or `$SITUCRAFT_DATA`):
- a bundle's zip is checked by its publisher's MD5;
- it is extracted, each member's CRC-32 checked, and every extracted file checked against the
  SHA-256 recorded in `sources.json`;
- then the zip is deleted.

The same two bundles are SituCraft's example datasets: the program embeds `sources.json` (the list,
not the data) and downloads one, with the same checks, when a user asks.

Nothing here is stored in the repository. The two Xenium 2-FOV bundles take about 640 MB
extracted. They are 10x Genomics example data under CC BY 4.0.

A check whose data are missing is skipped, unless `SITUCRAFT_REQUIRE_DATA` names the data set (a
comma-separated list, or `1` for all).
- Each commit's CI requires `xenium-lung-2fov`.
- The weekly run (`.github/workflows/data.yml`) requires both bundles.

## References

`reference/` holds what independent tools report, with the script that produced it:
- `generate_bundles.py` → `xenium-bundles.json`: DuckDB 1.5, zarr 3.4 and tifffile with
  imagecodecs on both bundles, as sizes and SHA-256 digests.
- `read_exports.py` → `exports.json`: the Lung bundle's own segmentation exported in every format
  (`SITUCRAFT_EXPORT_DIR=validation/cache/exports go test -run TestRealExports
  ./internal/export/`), read by spatialdata 0.8.0, anndata 0.13.4, pyarrow, pandas, scipy,
  geopandas and numpy: what each read (totals, and the content fingerprints of
  `situcraft-export.json` recomputed from its arrays and tables) and whether it agrees with the
  manifest. `TestRealExports` checks SituCraft still writes what was read; the weekly workflow
  reads a fresh export with `--check`.
- `read_simulation.py`: a simulated bundle (`situcraft simulate`) read by spatialdata-io 0.7.1's
  Xenium reader, tifffile and pyarrow, each against what `situcraft-simulation.json` says the
  simulation made (cells, transcripts, labels, boundaries, image, counts, the truth's molecules
  and cells), with no warning about the bundle; it exits 1 on any difference. The weekly workflow
  runs it on the small and standard presets.

Regenerate a reference only when its data set or the tool changes, and say why in the commit.

## Projects

`projects/` holds a project saved by each release, as that release wrote it, so that every later
release is checked to reopen it exactly (`node validation/run.mjs viewer`, and
`web/lib/project.test.mjs` for the document alone):
- `0.1.0-lung.situcraft.json`: the Lung 2-FOV dataset (recorded at `/data/examples/…`, a place
  the harness then locates it from), the filter "Enough signal" in two versions, and three saved
  views (two of the Viewer, one of QC with the filter in use).
- `0.2.0-lung.situcraft.json`: the 0.1.0 project as the viewer suite leaves it in 0.2.0, written
  by it with `SITUCRAFT_WRITE_FIXTURE=1`: the instrument's segmentation, the 5 µm expansion, an
  import of the bundle into itself, the planted errors and an import of a SpatialData export;
  comparisons of the instrument's with the expansion and with the planted errors; decisions
  (the instrument's the default, the expansion kept, the planted errors rejected); an export;
  four saved views. Reopened, every artifact SituCraft made that it records is made again with the
  same files' checksums (a comparison by its differences, its `comparison.json` recording the
  jobs' times; an import of an export left out, an export's files naming the SituCraft that wrote
  them).

Make each with its release and add one per release; only the places of the run's folders are
replaced (by ones that do not exist, so that locating a moved bundle is checked too), and the
0.2.0 project, grown from the 0.1.0 one, is given its own id.
