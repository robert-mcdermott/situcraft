# Writes the tables of labels the import's tests read, as other tools write them: CSV and gzipped
# TSV by pandas, Parquet by pyarrow (a dictionary-encoded text column, a float32 column with nulls),
# AnnData in Zarr formats 3 and 2 by anndata (a categorical column with a missing value, the cells'
# ids as the index), a SpatialData-like store holding the AnnData as a table, Xenium Explorer's cell
# groups (cell_id, group, a color given once per group), and two tables to refuse (a cell another
# segmentation has; a cell named twice); and expected.json with each cell's label and confidence.
#
#   uv run --with 'pandas==3.0.6' --with 'pyarrow==26.0.0' --with 'anndata==0.13.4' --with 'numpy==2.5.4' python -I make_fixtures.py
import csv, json, os, shutil
import numpy as np
import pandas as pd
import pyarrow as pa
import pyarrow.parquet as pq
import anndata as ad

here = os.path.dirname(os.path.abspath(__file__))

# The segmentation's cells, in order; the tables label eight of them, in another order.
cells = ['aaaabbbb-1', 'aaaacccc-1', 'aaaadddd-1', 'aaaaeeee-1', 'aaaaffff-1', 'aaaagggg-1', 'aaaahhhh-1', 'aaaaiiii-1', 'aaaajjjj-1', 'µm-cell-2']
rows = [3, 0, 7, 9, 1, 5, 2, 8]
types = ['T cell', 'Tumour', 'T cell', 'Macrophage (M2)', None, 'Tumour', 'Tumour', 'Fibroblast, myo']
confidence = [0.91, 0.5, None, 0.25, 0.75, 1.0, 0.125, 0.0625]
ids = [cells[r] for r in rows]

df = pd.DataFrame({'cell_id': ids, 'cell_type': types, 'confidence': confidence, 'sample': ['s1'] * len(ids)})
df.to_csv(os.path.join(here, 'labels.csv'), index=False)
df.to_csv(os.path.join(here, 'labels.tsv.gz'), index=False, sep='\t', compression={'method': 'gzip', 'mtime': 0})

table = pa.table({
    'cell_id': pa.array(ids, pa.string()),
    'cell_type': pa.array(types, pa.string()).dictionary_encode(),
    'confidence': pa.array(confidence, pa.float32()),
    'n': pa.array(range(len(ids)), pa.int64()),
})
pq.write_table(table, os.path.join(here, 'labels.parquet'))

obs = pd.DataFrame({'cell_type': pd.Categorical(types), 'confidence': [np.nan if v is None else v for v in confidence]}, index=pd.Index(ids, name='cell'))
for fmt in (3, 2):
    ad.settings.zarr_write_format = fmt
    out = os.path.join(here, f'labels_v{fmt}.zarr')
    shutil.rmtree(out, ignore_errors=True)
    ad.AnnData(obs=obs).write_zarr(out)
ad.settings.zarr_write_format = 3
sd = os.path.join(here, 'spatialdata.zarr')
shutil.rmtree(sd, ignore_errors=True)
ad.AnnData(obs=obs.reset_index().rename(columns={'cell': 'cell_id'}).set_index(pd.Index([str(i) for i in range(len(ids))]))).write_zarr(os.path.join(sd, 'tables', 'table'))

# Xenium Explorer's cell groups: cell_id, group, and a group's color once.
with open(os.path.join(here, 'explorer.csv'), 'w', newline='') as f:
    w = csv.writer(f)
    w.writerow(['cell_id', 'group', 'color'])
    given = set()
    for i, t in zip(ids, types):
        if t is None:
            continue
        w.writerow([i, t, '' if t in given else {'T cell': '#FF0055', 'Tumour': '#1f77b4', 'Macrophage (M2)': '#2ca02c', 'Fibroblast, myo': '#9467bd'}[t]])
        given.add(t)

pd.DataFrame({'cell_id': ids[:4] + ['zzzzzzzz-1'], 'cell_type': types[:4] + ['T cell']}).to_csv(os.path.join(here, 'other.csv'), index=False)
pd.DataFrame({'cell_id': ids[:4] + [ids[0]], 'cell_type': types[:4] + ['T cell']}).to_csv(os.path.join(here, 'twice.csv'), index=False)

label = [None] * len(cells)
conf = [None] * len(cells)
for r, t, c in zip(rows, types, confidence):
    label[r] = t
    conf[r] = c
with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({'pandas': pd.__version__, 'pyarrow': pa.__version__, 'anndata': ad.__version__, 'cells': cells, 'label': label, 'confidence': conf}, f, indent=1, ensure_ascii=False)
    f.write('\n')
