# Writes shapes_v2.zarr: the scene of make_fixtures.py (read back from dataset.json, expected.json
# and geojson/cells.geojson, which it writes first) as SpatialData 0.2 wrote it, in Zarr format 2
# (strings in numcodecs' VLenUTF8, a table's attributes in uns).
#
#   uv run --python 3.11 --with 'spatialdata==0.2.6' --with 'dask<2025' --with 'geopandas<1' python -I make_fixtures_v2.py
import json, os, shutil
import numpy as np
import pandas as pd
import geopandas as gpd
import anndata as ad
import dask.dataframe as dd
import scipy.sparse as sp
import spatialdata as sd
from spatialdata.models import PointsModel, ShapesModel, TableModel
from spatialdata.transformations import Identity

here = os.path.dirname(os.path.abspath(__file__))
dataset = json.load(open(os.path.join(here, 'dataset.json')))
expected = json.load(open(os.path.join(here, 'expected.json')))
cells = gpd.read_file(os.path.join(here, 'geojson', 'cells.geojson')).set_index('cell_id')
order = list(cells.index)
tr = pd.DataFrame(dataset['transcripts'])
assign = [expected['assignment'][str(i)] for i in tr['id']]

shapes = ShapesModel.parse(gpd.GeoDataFrame(geometry=list(cells.geometry), index=pd.Index(order, name='cell_id')), transformations={'global': Identity()})
points = PointsModel.parse(dd.from_pandas(pd.DataFrame({
    'x': tr['x'].astype('float32'), 'y': tr['y'].astype('float32'), 'feature_name': pd.Categorical(tr['gene']),
    'transcript_id': tr['id'].astype('uint64'), 'cell_id': [c or 'UNASSIGNED' for c in assign],
}), npartitions=1), coordinates={'x': 'x', 'y': 'y'}, feature_key='feature_name', transformations={'global': Identity()})
genes = sorted(g for g in tr['gene'].unique() if not g.startswith('NegControl'))
m = np.zeros((len(order), len(genes)), dtype=np.float32)
for c, g, q in zip(assign, tr['gene'], tr['qv']):
    if c in order and g in genes and q >= 20:
        m[order.index(c), genes.index(g)] += 1
obs = pd.DataFrame({'cell_id': order, 'region': pd.Categorical(['cell_boundaries'] * len(order))}, index=order)
table = TableModel.parse(ad.AnnData(X=sp.csr_matrix(m), obs=obs, var=pd.DataFrame(index=genes)), region='cell_boundaries', region_key='region', instance_key='cell_id')
out = os.path.join(here, 'shapes_v2.zarr')
shutil.rmtree(out, ignore_errors=True)
sd.SpatialData(shapes={'cell_boundaries': shapes}, points={'transcripts': points}, tables={'table': table}).write(out)
print('spatialdata', sd.__version__)
