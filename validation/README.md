# Validation

SituCraft's results are checked against independent references and known truth. Unit tests
(`go test ./...`, `node --test "web/lib/**/*.test.mjs"`) check functions in isolation; the checks
here run on real data or whole interfaces.

| Check | What it compares | Data |
| --- | --- | --- |
| `node validation/run.mjs accessibility` | WCAG AA contrast of every text-on-surface pair in both themes, with color-vision-friendly colors off and on; palettes, status colors and diverging maps apart in protanopia, deuteranopia and tritanopia (Machado et al. 2009, CIEDE2000) | `web/styles.css` |
| `go test ./validation/` (readers) | Every Parquet column of the development bundles as DuckDB reads it; every Zarr array as zarr-python and numcodecs read it; every TIFF's structure (images, pyramid levels, tiles, compression) and the pixels of each plane's smallest (lossless) level as tifffile and imagecodecs read them; and every image tile decoded, with the time taken | the two 2-FOV bundles |
| `go test ./internal/xenium/` (bundles) | Both development bundles validate with exactly the expected findings and reproduce 12 of the run's recorded metrics; the Lung bundle zipped validates as the folder does; 13 broken bundles (the Lung bundle with one change each) raise exactly their findings; nothing in a bundle changes | the two 2-FOV bundles |
| `go test ./internal/cache/` (derived data) | Every tile of every image level equals the bundle's own level decoded directly; every transcript is in one tile with its values; every gene's density grid sums to its count at every level and level 1's bins equal a recount; level 0's polygons equal the boundary files' and every level holds each once, within its simplification tolerance; builds stopped in each entry resume to byte-identical entries; an unchanged bundle reopens without reading or building; the zipped Lung bundle reuses the folder's entries; sizes within 500 MB for both, and build times within budget | the two 2-FOV bundles |
| `node validation/run.mjs viewer` (the tissue viewer) | In headless Chrome: every channel drawn at levels 0, 1 and the coarsest, read back before any window or color, equals the derived data's tiles sample for sample (across tile edges and at the image's edges); the transcripts loaded for a region equal the program's query of the index; frames per second while panning and the time to draw a view, against budgets (30 fps where there is a GPU; 500 ms). Set `CHROME` to a browser, and `SITUCRAFT_CHROME_ARGS` to more of its flags (CI: software WebGL) | the 2-FOV bundles present, Chrome |
| `go test ./internal/view/` (the viewer's queries) | A region's transcripts against a scan of transcripts.parquet; density by gene against each gene's count; every cell's gene transcripts counted from the index against cells.parquet; a cell's boundary against cell_boundaries.parquet; the descriptor | the two 2-FOV bundles |
| `go test ./internal/jp2k/` (real tiles) | The JPEG 2000 tiles of the development bundles against OpenJPEG (`opj_decompress`): identical for lossless 5/3 tiles, within ±1 at every pixel for lossy 9/7 ones (every tile of the morphology-focus images and every 25th of the z-stacks; `JP2K_ZSTACK_ALL=1` for all) | the two 2-FOV bundles, OpenJPEG |

## Data

`node validation/fetch.mjs [data set …]` downloads the public data sets in `sources.json` into
`validation/cache/` (or `$SITUCRAFT_DATA`):
- a bundle's zip is checked by its publisher's MD5;
- it is extracted, each member's CRC-32 checked, and every extracted file checked against the
  SHA-256 recorded in `sources.json`;
- then the zip is deleted.

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
