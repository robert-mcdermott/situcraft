# Reads a simulated bundle (situcraft simulate) with the libraries Xenium data are usually read
# with, and checks that they read what the simulation says it made: spatialdata-io's Xenium reader
# (the table, the transcripts, the cell and nucleus labels and boundaries, the morphology image),
# tifffile (the OME-TIFF and its levels) and pyarrow (the truth's transcripts and cells). It prints
# what it read, with the libraries' versions, and exits 1 on any difference.
#
#   go run . simulate --preset small validation/cache/simulated-small
#   uv run --with 'spatialdata-io==0.7.1' --with 'spatialdata==0.8.0' --with 'tifffile==2026.9.20' \
#       python -I validation/reference/read_simulation.py validation/cache/simulated-small
#
# The weekly workflow (.github/workflows/data.yml) runs it on the small and standard presets.
import importlib.metadata as md, json, os, sys, warnings

import pyarrow.parquet as pq
import spatialdata_io
import tifffile

bundle = sys.argv[1]
with open(os.path.join(bundle, 'situcraft-simulation.json')) as f:
    sim = json.load(f)
made = sim['made']
problems = []


def expect(what, got, want):
    print(f'{what}: {got}' + ('' if got == want else f' (expected {want})'))
    if got != want:
        problems.append(what)


print('versions:', {p: md.version(p) for p in ['spatialdata-io', 'spatialdata', 'tifffile', 'pyarrow']})

# tifffile: the OME-TIFF, its size, pixel size and levels.
with tifffile.TiffFile(os.path.join(bundle, 'morphology_focus', 'morphology_focus_0000.ome.tif')) as tif:
    series = tif.series[0]
    expect('image size', tuple(series.shape[-2:]), (made['image_height_px'], made['image_width_px']))
    # Reduced levels halve the image until it is at most 1,024 pixels a side.
    levels, side = 1, max(made['image_width_px'], made['image_height_px'])
    while side > 1024:
        side, levels = (side + 1) // 2, levels + 1
    expect('image levels', len(series.levels), levels)
    expect('image is OME', tif.is_ome, True)
    pixels = tifffile.xml2dict(tif.ome_metadata)['OME']['Image']['Pixels']
    expect('physical pixel size', float(pixels['PhysicalSizeX']), sim['parameters']['pixel_um'])
    expect('channel', pixels['Channel']['Name'], 'DAPI')
    full = series.levels[0].asarray()
    expect('image type', str(full.dtype), 'uint16')

# spatialdata-io: the bundle as SpatialData.
with warnings.catch_warnings(record=True) as caught:
    warnings.simplefilter('always')
    data = spatialdata_io.xenium(bundle, cells_as_circles=False, morphology_mip=False, aligned_images=False, n_jobs=1)
# Warnings about the bundle itself (its version, files, ids), not the libraries' own deprecations.
about = [w for w in caught if 'Xenium' in str(w.message) or 'version' in str(w.message).lower() or 'cell' in str(w.message).lower()]
for w in about:
    print('warning:', w.category.__name__, str(w.message)[:200])
expect('warnings about the bundle', len(about), 0)
table = data.tables['table']
expect('cells in the table', table.n_obs, made['instrument_cells'])
expect('transcripts', len(data.points['transcripts']), made['transcripts'])
expect('cell boundaries', len(data.shapes['cell_boundaries']), made['instrument_cells'])
expect('nucleus boundaries', len(data.shapes['nucleus_boundaries']), made['instrument_cells'])
labels = data.labels['cell_labels']
height, width = (labels['scale0'].ds['image'] if hasattr(labels, 'children') else labels).shape[-2:]
expect('cell labels size', (height, width), (made['image_height_px'], made['image_width_px']))
image = data.images['morphology_focus']
expect('morphology image size', tuple((image['scale0'].ds['image'] if hasattr(image, 'children') else image).shape[-2:]),
       (made['image_height_px'], made['image_width_px']))

# The counts: the table holds the genes (spatialdata-io keeps gene features only), so its total is
# the cells' gene transcripts (cells.parquet transcript_counts); its cells are cells.parquet's, in
# order, each labelled in the cell mask as its row + 1.
cells_parquet = pq.read_table(os.path.join(bundle, 'cells.parquet'), columns=['cell_id', 'transcript_counts'])
expect('gene counts total', int(table.X.sum()), sum(cells_parquet.column('transcript_counts').to_pylist()))
expect('cells in cells.parquet order', list(table.obs['cell_id'].astype(str)) == cells_parquet.column('cell_id').to_pylist(), True)
attrs = table.uns['spatialdata_attrs']
expect('each cell labelled as its row + 1', list(table.obs[attrs['instance_key']].astype(int)) == list(range(1, table.n_obs + 1)), True)

# pyarrow: the truth.
truth = pq.read_table(os.path.join(bundle, 'truth', 'transcripts.parquet'), columns=['origin', 'cell_id'])
origins = {}
for o in truth.column('origin').to_pylist():
    origins[o] = origins.get(o, 0) + 1
expect('molecules by origin', origins, made['molecules_by_origin'])
cells = pq.read_table(os.path.join(bundle, 'truth', 'cells.parquet'))
expect('true cells', cells.num_rows, made['cells'])
by_type = {}
for t in cells.column('cell_type').to_pylist():
    by_type[t] = by_type.get(t, 0) + 1
expect('true cells by type', by_type, made['cells_by_type'])

if problems:
    print('DIFFERENT:', ', '.join(problems))
    sys.exit(1)
print('all agree')
