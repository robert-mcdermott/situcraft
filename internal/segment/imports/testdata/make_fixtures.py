# Writes the segmentation imports the tests read, from one hand-made scene, with the libraries
# that write each format for real: SpatialData Zarr (spatialdata: shapes, points and a table; and
# a label image with its table, in the image's pixels, as spatialdata-io writes Xenium data),
# Baysor-style tables and polygons, GeoJSON with Parquet and CSV tables (geopandas, pyarrow,
# pandas), and a small Xenium Ranger-style bundle (pyarrow, zarr). Also writes the dataset they
# segment (dataset.json) and what each import must give (expected.json).
#
#   uv run --with spatialdata --with pyarrow python -I make_fixtures.py
import json, os, shutil, zipfile
import numpy as np
import pandas as pd
import geopandas as gpd
import shapely
from shapely.geometry import Point, Polygon, MultiPolygon, box
import pyarrow as pa
import pyarrow.parquet as pq

here = os.path.dirname(os.path.abspath(__file__))
rng = np.random.default_rng(20261015)

# --- The scene: a 40 × 30 µm field, pixels of 0.5 µm --------------------------------------------
PIXEL = 0.5
W, H = 80, 60
cells = {
    'A': box(2, 2, 10, 10),
    'B': box(12, 2, 20, 8),
    'C': Polygon([(2, 12), (10, 12), (10, 16), (6, 16), (6, 22), (2, 22)]),
    'D': MultiPolygon([box(22, 2, 26, 6), box(28, 2, 32, 6)]),
    'E': Polygon([(12, 12), (22, 12), (22, 22), (12, 22)], [[(15, 15), (19, 15), (19, 19), (15, 19)]]),
    'F': box(34, 20, 44, 28),  # reaches beyond the image's right edge (40 µm)
}
order = list(cells)            # the polygons' cells, in order; G has no polygon
genes = ['ACTA2', 'EPCAM', 'VIM', 'PTPRC', 'NegControlProbe_00001']
N = 240
ids = np.arange(1001, 1001 + N, dtype=np.uint64)
xs = np.round(rng.uniform(0, 40, N), 3).astype(np.float32)
ys = np.round(rng.uniform(0, 30, N), 3).astype(np.float32)
gene = [genes[i] for i in rng.integers(0, len(genes), N)]
qv = rng.choice([10.0, 25.0, 35.0], N).astype(np.float32)

# The tool's assignment: the polygon containing the transcript; G holds those in its box that no
# polygon does; three transcripts outside every polygon are given to B (the tool's model decides,
# not its outline); five inside A are left as noise.
G_BOX = box(28, 22, 34, 28.5)
assign = []
for i in range(N):
    p = Point(float(xs[i]), float(ys[i]))
    c = next((k for k in order if cells[k].contains(p)), '')
    if not c and G_BOX.contains(p):
        c = 'G'
    assign.append(c)
outside = [i for i in range(N) if assign[i] == ''][:3]
for i in outside:
    assign[i] = 'B'
noise = [i for i in range(N) if assign[i] == 'A'][:5]
for i in noise:
    assign[i] = ''
UNKNOWN_ID = 999999  # a transcript the dataset does not have, assigned to A by the tool

# The pixels each cell draws: those whose centre the polygon contains, within the image.
centres = [(x, y) for y in range(H) for x in range(W)]
pixels = {k: 0 for k in order}
label = np.zeros((H, W), dtype=np.uint32)
for k_i, k in enumerate(order):
    g = cells[k]
    for (x, y) in centres:
        if g.contains(Point((x + 0.5) * PIXEL, (y + 0.5) * PIXEL)):
            label[y, x] = k_i + 1
            pixels[k] += 1

# --- The dataset and what imports must give ----------------------------------------------------
with open(os.path.join(here, 'dataset.json'), 'w') as f:
    json.dump({'width': W, 'height': H, 'pixel_size': PIXEL, 'run': 'fixture run', 'region': 'region 1',
               'transcripts': [{'id': int(ids[i]), 'x': float(xs[i]), 'y': float(ys[i]), 'gene': gene[i], 'qv': float(qv[i])} for i in range(N)]}, f)
    f.write('\n')
q20_genes = [i for i in range(N) if qv[i] >= 20 and not gene[i].startswith('NegControl')]
with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({
        'cells': order + ['G'],
        'pixels': pixels,
        'assignment': {str(int(ids[i])): assign[i] for i in range(N)},
        'q20_gene_assigned': {k: sum(1 for i in q20_genes if assign[i] == k) for k in order + ['G']},
    }, f, indent=1)
    f.write('\n')

def reset(p):
    if os.path.isdir(p):
        shutil.rmtree(p)
    elif os.path.exists(p):
        os.remove(p)

# --- GeoJSON polygons with a table --------------------------------------------------------------
geo = os.path.join(here, 'geojson')
reset(geo)
os.makedirs(geo)
frame = gpd.GeoDataFrame({'cell_id': order}, geometry=[cells[k] for k in order])
frame.to_file(os.path.join(geo, 'cells.geojson'), driver='GeoJSON')
scaled = frame.copy()
scaled['geometry'] = scaled.geometry.apply(lambda g: shapely.affinity.scale(g, 1 / PIXEL, 1 / PIXEL, origin=(0, 0)))
scaled.to_file(os.path.join(geo, 'cells_pixels.geojson'), driver='GeoJSON')
table = pd.DataFrame({'transcript_id': np.append(ids, np.uint64(UNKNOWN_ID)), 'cell_id': [c or 'UNASSIGNED' for c in assign] + ['A']})
pq.write_table(pa.Table.from_pandas(table, preserve_index=False), os.path.join(geo, 'assignments.parquet'))
table.to_csv(os.path.join(geo, 'assignments.csv'), index=False)

# --- Baysor-style: segmentation.csv and segmentation_polygons.json ------------------------------
bay = os.path.join(here, 'baysor')
reset(bay)
os.makedirs(bay)
number = {k: i + 1 for i, k in enumerate(order + ['G'])}  # Baysor numbers its cells from 1; 0 is noise
pd.DataFrame({
    'x': xs, 'y': ys, 'z': np.zeros(N, dtype=np.float32), 'gene': gene,
    'cell': [number.get(c, 0) for c in assign], 'is_noise': [c == '' for c in assign],
    'assignment_confidence': np.round(rng.uniform(0.5, 1, N), 3),
}).to_csv(os.path.join(bay, 'segmentation.csv'), index=False)
with open(os.path.join(bay, 'segmentation_polygons.json'), 'w') as f:
    geoms = []
    for k in order:
        g = shapely.geometry.mapping(cells[k])
        g['cell'] = number[k]
        geoms.append(g)
    json.dump({'type': 'GeometryCollection', 'geometries': json.loads(json.dumps(geoms))}, f)

# --- SpatialData --------------------------------------------------------------------------------
import anndata as ad
import dask.dataframe as dd
import scipy.sparse as sp
import spatialdata as sd
from spatialdata.models import Labels2DModel, PointsModel, ShapesModel, TableModel
from spatialdata.transformations import Identity, Scale

def counts_table(index, region, instance_key, instance_values):
    m = np.zeros((len(index), len(genes) - 1), dtype=np.float32)
    for i in q20_genes:
        if assign[i] in index:
            m[index.index(assign[i]), genes.index(gene[i])] += 1
    obs = pd.DataFrame({instance_key: instance_values, 'region': pd.Categorical([region] * len(index))}, index=index)
    return TableModel.parse(ad.AnnData(X=sp.csr_matrix(m), obs=obs, var=pd.DataFrame(index=genes[:-1])), region=region, region_key='region', instance_key=instance_key)

def points(scale):
    df = pd.DataFrame({'x': xs * scale, 'y': ys * scale, 'feature_name': pd.Categorical(gene), 'transcript_id': ids, 'cell_id': [c or 'UNASSIGNED' for c in assign]})
    return PointsModel.parse(dd.from_pandas(df, npartitions=2), coordinates={'x': 'x', 'y': 'y'}, feature_key='feature_name', transformations={'global': Identity()})

# Shapes (µm), points with their cells, a table annotating the shapes.
shapes = ShapesModel.parse(gpd.GeoDataFrame(geometry=[cells[k] for k in order], index=pd.Index(order, name='cell_id')), transformations={'global': Identity()})
s = sd.SpatialData(shapes={'cell_boundaries': shapes}, points={'transcripts': points(1)}, tables={'table': counts_table(order, 'cell_boundaries', 'cell_id', order)})
reset(os.path.join(here, 'shapes.zarr'))
s.write(os.path.join(here, 'shapes.zarr'))

# A label image and shapes in the image's pixels (as spatialdata-io writes Xenium data), the table
# annotating the labels: each label's row names its cell.
labels = Labels2DModel.parse(label, dims=('y', 'x'), transformations={'global': Identity()})
shapes_px = ShapesModel.parse(gpd.GeoDataFrame(geometry=[shapely.affinity.scale(cells[k], 1 / PIXEL, 1 / PIXEL, origin=(0, 0)) for k in order], index=pd.Index(order, name='cell_id')), transformations={'global': Identity()})
s = sd.SpatialData(labels={'cell_labels': labels}, shapes={'cell_boundaries': shapes_px}, points={'transcripts': points(1 / PIXEL)},
                   tables={'table': counts_table(order, 'cell_labels', 'cell_labels', list(range(1, len(order) + 1)))})
reset(os.path.join(here, 'labels.zarr'))
s.write(os.path.join(here, 'labels.zarr'))

# --- A Xenium Ranger-style bundle --------------------------------------------------------------
import zarr
xen = os.path.join(here, 'ranger')
reset(xen)
os.makedirs(xen)
with open(os.path.join(xen, 'experiment.xenium'), 'w') as f:
    json.dump({'major_version': 5, 'minor_version': 0, 'run_name': 'fixture run', 'region_name': 'region 1', 'pixel_size': PIXEL,
               'analysis_sw_version': 'xenium-ranger-3.1.0.4', 'analysis_uuid': 'fixture-ranger', 'panel_name': 'Fixture panel'}, f)
ranger_cells = order  # G has no outline in a bundle; its transcripts are unassigned there
pq.write_table(pa.table({
    'cell_id': ranger_cells,
    'x_centroid': [cells[k].centroid.x for k in ranger_cells], 'y_centroid': [cells[k].centroid.y for k in ranger_cells],
    'transcript_counts': [sum(1 for i in q20_genes if assign[i] == k) for k in ranger_cells],
}), os.path.join(xen, 'cells.parquet'))
pq.write_table(pa.table({
    'transcript_id': pa.array(ids, pa.uint64()), 'cell_id': [c if c in ranger_cells else 'UNASSIGNED' for c in assign],
    'overlaps_nucleus': pa.array(np.zeros(N, dtype=np.uint8)), 'feature_name': gene,
    'x_location': xs, 'y_location': ys, 'z_location': np.zeros(N, dtype=np.float32), 'qv': qv,
}), os.path.join(xen, 'transcripts.parquet'))
vid, vx, vy = [], [], []
for k in ranger_cells:
    g = cells[k] if cells[k].geom_type == 'Polygon' else max(cells[k].geoms, key=lambda p: p.area)
    for (x, y) in g.exterior.coords:
        vid.append(k); vx.append(x); vy.append(y)
pq.write_table(pa.table({'cell_id': vid, 'vertex_x': pa.array(vx, pa.float32()), 'vertex_y': pa.array(vy, pa.float32())}), os.path.join(xen, 'cell_boundaries.parquet'))
store = os.path.join(here, 'ranger-cells.zarr')
reset(store)
root = zarr.open_group(store, mode='w', zarr_format=2)
masks = root.create_group('masks')
masks.create_array('0', shape=label.shape, chunks=(32, 40), dtype='<u4', compressors=None)[...] = np.zeros_like(label)
masks.create_array('1', shape=label.shape, chunks=(32, 40), dtype='<u4', compressors=None)[...] = label
t = masks.create_array('homogeneous_transform', shape=(3, 3), chunks=(3, 3), dtype='<f4', compressors=None)
t[...] = np.array([[1 / PIXEL, 0, 0], [0, 1 / PIXEL, 0], [0, 0, 1]], dtype='<f4')
with zipfile.ZipFile(os.path.join(xen, 'cells.zarr.zip'), 'w', zipfile.ZIP_STORED) as z:
    for folder, _, files in os.walk(store):
        for name in sorted(files):
            full = os.path.join(folder, name)
            z.write(full, os.path.relpath(full, store))
shutil.rmtree(store)

print('cells', {k: pixels[k] for k in order}, 'assigned', sum(1 for c in assign if c))
