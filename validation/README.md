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
| `node validation/run.mjs viewer` (the tissue viewer and QC) | In headless Chrome: every channel drawn at levels 0, 1 and the coarsest, read back before any window or color, equals the derived data's tiles sample for sample (across tile edges and at the image's edges); the transcripts loaded for a region equal the program's query of the index; frames per second while panning and the time to draw a view, against budgets (30 fps where there is a GPU; 500 ms); in the QC view, every recomputable metric agrees, brushing chooses exactly the cells in range, the choice shows in the Viewer, a saved filter keeps the cells (and SHA-256) an independent evaluation gives, and every QC map draws; the examples are listed and none is downloaded unasked; every step of both tours opens with its genes; the project saved by 0.1.0 (`validation/projects/`) opens, finds its moved bundle again, its filters keep exactly the cells they recorded, and its saved views reopen exactly, from the project and from a link; each bundle's instrument segmentation is registered as an artifact, every file's SHA-256 as Node computes it, promoted as the default, and the 0.1.0 project gets the same artifact; through remote control (the program started with `--remote-control`), a program's actions show the tissue, save and reopen a view, save a filter keeping the cells the 0.1.0 project's identical filter recorded, write the methods, and write a file only with the token. Set `CHROME` to a browser, and `SITUCRAFT_CHROME_ARGS` to more of its flags (CI: software WebGL) | the 2-FOV bundles present, Chrome |
| `node docs/capture/capture.mjs --audit` (the documentation's screenshots) | Every scene, driven through remote control in headless Chrome in both themes, against axe-core's WCAG 2.1 A and AA rules (`docs/capture/audit.json`) | the 2-FOV bundles, axe-core (`node validation/fetch.mjs axe-core`), Chrome |
| `go test ./internal/view/` (the viewer's and QC's queries) | A region's transcripts against a scan of transcripts.parquet; density by gene against each gene's count; every cell's gene transcripts counted from the index against cells.parquet; a cell's boundary against cell_boundaries.parquet; the QC maps' groups of transcripts summed over the tissue against the run's assigned and Q20 shares; the cells table's genes per cell against the recorded median; the descriptor | the two 2-FOV bundles |
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

Regenerate a reference only when its data set or the tool changes, and say why in the commit.

## Projects

`projects/` holds a project saved by each release, as that release wrote it, so that every later
release is checked to reopen it exactly (`node validation/run.mjs viewer`, and
`web/lib/project.test.mjs` for the document alone):
- `0.1.0-lung.situcraft.json`: the Lung 2-FOV dataset (recorded at `/data/examples/…`, a place
  the harness then locates it from), the filter "Enough signal" in two versions, and three saved
  views (two of the Viewer, one of QC with the filter in use).

Make each with its release and add one per release; only the bundle's place is replaced (by one
that does not exist, so that locating a moved bundle is checked too).
