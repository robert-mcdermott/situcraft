# Writes the AnnData stores the tests read, with the anndata package, in Zarr format 3 (sharded, as
# anndata 0.11 and later write) and format 2: obs with every column encoding SituCraft reads (text,
# categorical, nullable integer, nullable string, boolean, numbers), var, and X as sparse rows,
# sparse columns, and a dense array; and expected.json with the values.
#
#   uv run --with anndata --with scipy python -I make_fixtures.py
import json, os, shutil
import numpy as np
import pandas as pd
import scipy.sparse as sp
import anndata as ad

here = os.path.dirname(os.path.abspath(__file__))
X = np.array([[1, 0, 2], [0, 0, 0], [3, 4, 0], [0, 5, 6]], dtype=np.float32)
obs = pd.DataFrame({
    'cell_id': ['aaaiikim-1', 'bbbbcccc-1', 'µm-cell', ''],
    'region': pd.Categorical(['cell_labels', 'cell_labels', None, 'nuclei']),
    'label': pd.array([1, 2, None, 4], dtype='Int64'),
    'name': pd.array(['a', None, 'c', 'd'], dtype='string'),
    'kept': [True, False, True, True],
    'area': [10.5, 0.0, 33.25, 7.0],
}, index=['c1', 'c2', 'c3', 'c4'])
var = pd.DataFrame(index=['ACTA2', 'EPCAM', 'VIM'])
for fmt in (3, 2):
    ad.settings.zarr_write_format = fmt
    for name, x in (('csr', sp.csr_matrix(X)), ('csc', sp.csc_matrix(X)), ('dense', X)):
        out = os.path.join(here, f'v{fmt}_{name}.zarr')
        shutil.rmtree(out, ignore_errors=True)
        ad.AnnData(X=x, obs=obs, var=var).write_zarr(out)
with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({'anndata': ad.__version__, 'index': list(obs.index), 'cell_id': list(obs['cell_id']),
               'region': [None if pd.isna(v) else v for v in obs['region']], 'label': [None if pd.isna(v) else int(v) for v in obs['label']],
               'name': [None if pd.isna(v) else v for v in obs['name']], 'kept': list(obs['kept']), 'area': list(obs['area']),
               'var': list(var.index), 'row_sums': X.sum(axis=1).tolist()}, f, indent=1)
    f.write('\n')
