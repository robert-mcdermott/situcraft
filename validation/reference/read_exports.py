# Reads SituCraft's exports with the libraries their users read them with, and checks that each
# format holds exactly the segmentation its manifest (situcraft-export.json) describes: spatialdata
# (the SpatialData store), anndata (the AnnData store), pyarrow and pandas (the Parquet and CSV
# tables), scipy (the Matrix Market files), geopandas (the GeoJSON polygons) and numpy (Xenium
# Ranger's masks). Each export's totals and content fingerprints are recomputed from what the
# libraries read; the result, with the libraries' versions, goes to exports.json, which the
# real-data tests compare with what SituCraft writes now (internal/export, TestRealExports).
#
#   SITUCRAFT_EXPORT_DIR=validation/cache/exports SITUCRAFT_REQUIRE_DATA=xenium-lung-2fov \
#       go test -run TestRealExports ./internal/export/
#   uv run --with 'spatialdata==0.8.0' --with 'anndata==0.13.4' --with 'pyarrow==26.0.0' \
#       --with 'geopandas==1.2.0' --with 'scipy==1.18.1' python -I validation/reference/read_exports.py [--check]
#
# With --check, the result is compared with exports.json instead of written (the weekly workflow).
import gzip, hashlib, importlib.metadata as md, json, os, sys, warnings

import anndata as ad
import geopandas as gpd
import numpy as np
import pandas as pd
import pyarrow.parquet as pq
import scipy.io
import spatialdata as sd
from spatialdata.models import get_table_keys
from spatialdata.transformations import get_transformation

here = os.path.dirname(os.path.abspath(__file__))
root = os.environ.get('SITUCRAFT_EXPORT_DIR') or os.path.join(here, '..', 'cache', 'exports')
out_path = os.path.join(here, 'exports.json')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def labels_digest(a):
    return sha(np.ascontiguousarray(np.asarray(a), dtype='<u4').tobytes())


def counts_digest(cells, genes, X):
    """X: cells × genes sparse (or the long table's triples)."""
    X = X.tocsr()
    X.sort_indices()
    order = np.argsort(np.array([g.encode() for g in genes], dtype=object))
    rank = np.empty(len(order), dtype=np.int64)
    rank[order] = np.arange(len(order))
    lines = []
    for i, cell in enumerate(cells):
        lo, hi = X.indptr[i], X.indptr[i + 1]
        idx, val = X.indices[lo:hi], X.data[lo:hi]
        for k in np.argsort(rank[idx], kind='stable'):
            lines.append(f'{cell}\t{genes[idx[k]]}\t{int(val[k])}\n')
    return sha(''.join(lines).encode())


def long_counts_digest(cells, table):
    """The counts as a long table (cell_id, feature_name, count)."""
    pos = {c: i for i, c in enumerate(cells)}
    t = table.assign(_cell=table['cell_id'].map(pos), _gene=table['feature_name'].map(lambda g: g.encode()))
    t = t.sort_values(['_cell', '_gene'], kind='stable')
    return sha(''.join(f'{c}\t{g}\t{int(n)}\n' for c, g, n in zip(t['cell_id'], t['feature_name'], t['count'])).encode())


def assignments_digest(cell_ids):
    return sha(''.join(f'{c}\n' for c in cell_ids).encode())


def read_export(folder):
    manifest = json.load(open(os.path.join(folder, 'situcraft-export.json')))
    want, content = manifest['counts'], manifest['content']
    formats = {}

    def agree(name, got):
        checks = {}
        for k, v in got.items():
            if k in want:
                checks[k] = v == want[k]
            elif k in content:
                checks[k] = v == content[k]
        got['agrees'] = all(checks.values())
        if not got['agrees']:
            got['differs'] = sorted(k for k, ok in checks.items() if not ok)
        formats[name] = got

    if 'spatialdata' in manifest['formats']:
        s = sd.read_zarr(os.path.join(folder, 'spatialdata.zarr'))
        labels = s['cell_labels']
        shapes = s['cell_boundaries']
        points = s['transcripts'].compute()
        table = s['table']
        region, region_key, instance_key = get_table_keys(table)
        scale = get_transformation(labels, 'global')
        agree('spatialdata', {
            'cells': table.n_obs, 'genes': table.n_vars, 'cellsWithOutline': len(shapes),
            'cellsWithPixels': int(len(np.unique(np.asarray(labels))) - (0 in np.asarray(labels))),
            'transcripts': len(points), 'assignedTranscripts': int((points['cell_id'] != 'UNASSIGNED').sum()),
            'countEntries': int(table.X.nnz), 'countSum': int(table.X.sum()),
            'labelsSha256': labels_digest(labels), 'countsSha256': counts_digest(list(table.obs_names), list(table.var_names), table.X),
            'assignmentsSha256': assignments_digest(points['cell_id']),
            'table': {'region': region, 'region_key': region_key, 'instance_key': instance_key},
            'labelsToGlobal': [float(v) for v in scale.scale] if hasattr(scale, 'scale') else str(scale),
            'pointsToGlobal': type(get_transformation(s['transcripts'], 'global')).__name__,
            'shapesToGlobal': type(get_transformation(shapes, 'global')).__name__,
        })

    if 'anndata' in manifest['formats']:
        a = ad.read_zarr(os.path.join(folder, 'anndata.zarr'))
        agree('anndata', {
            'cells': a.n_obs, 'genes': a.n_vars, 'countEntries': int(a.X.nnz), 'countSum': int(a.X.sum()),
            'countsSha256': counts_digest(list(a.obs_names), list(a.var_names), a.X),
            'spatial': list(a.obsm['spatial'].shape),
        })

    for name, ext, read in [('parquet', '.parquet', pd.read_parquet), ('csv', '.csv.gz', lambda p: pd.read_csv(p, keep_default_na=False, dtype={'cell_id': str, 'nucleus_id': str}))]:
        if name not in manifest['formats']:
            continue
        t = {k: read(os.path.join(folder, name, k + ext)) for k in ['cells', 'transcripts', 'cell_boundaries', 'counts']}
        cells = list(t['cells']['cell_id'])
        agree(name, {
            'cells': len(cells), 'transcripts': len(t['transcripts']),
            'assignedTranscripts': int((t['transcripts']['cell_id'] != 'UNASSIGNED').sum()),
            'cellsWithOutline': int(t['cell_boundaries']['cell_id'].nunique()),
            'countEntries': len(t['counts']), 'countSum': int(t['counts']['count'].sum()),
            'countsSha256': long_counts_digest(cells, t['counts']),
            'assignmentsSha256': assignments_digest(t['transcripts']['cell_id']),
        })

    if 'mex' in manifest['formats']:
        d = os.path.join(folder, 'cell_feature_matrix')
        m = scipy.io.mmread(os.path.join(d, 'matrix.mtx.gz'), spmatrix=False).tocsr()
        features = pd.read_csv(os.path.join(d, 'features.tsv.gz'), sep='\t', header=None)
        barcodes = pd.read_csv(os.path.join(d, 'barcodes.tsv.gz'), sep='\t', header=None, dtype=str, keep_default_na=False)[0]
        agree('mex', {
            'genes': m.shape[0], 'cells': m.shape[1], 'countEntries': int(m.nnz), 'countSum': int(m.sum()),
            'countsSha256': counts_digest(list(barcodes), list(features[1]), m.T),
        })

    if 'geojson' in manifest['formats']:
        g = gpd.read_file(os.path.join(folder, 'cell_boundaries.geojson'))
        agree('geojson', {'cellsWithOutline': len(g), 'polygons': int((g.geom_type == 'Polygon').sum()), 'invalid': int((~g.is_valid).sum())})

    if 'xenium-ranger' in manifest['formats']:
        d = os.path.join(folder, 'xenium-ranger')
        cells = np.load(os.path.join(d, 'cells.npy'))
        got = {'labelsSha256': labels_digest(cells), 'dtype': str(cells.dtype), 'shape': list(cells.shape),
               'cellsWithPixels': int(len(np.unique(cells)) - (0 in cells))}
        if os.path.exists(os.path.join(d, 'nuclei.npy')):
            nuclei = np.load(os.path.join(d, 'nuclei.npy'), mmap_mode='r')
            got['nuclei'] = {'dtype': str(nuclei.dtype), 'shape': list(nuclei.shape)}
        agree('xenium-ranger', got)

    return {'segmentation': manifest['segmentation']['name'], 'software': manifest['software'],
            'counts': want, 'content': content, 'formats': formats}


def main():
    warnings.simplefilter('ignore', FutureWarning)
    names = sorted(n for n in os.listdir(root) if os.path.exists(os.path.join(root, n, 'situcraft-export.json')))
    if not names:
        sys.exit(f'No exports in {root}: write them first (see the comment at the top).')
    versions = ', '.join(f'{p} {md.version(p)}' for p in ['spatialdata', 'anndata', 'zarr', 'pyarrow', 'pandas', 'geopandas', 'scipy', 'numpy'])
    result = {'generated_by': versions, 'exports': {n: read_export(os.path.join(root, n)) for n in names}}
    bad = [f'{n}: {f}' for n, e in result['exports'].items() for f, r in e['formats'].items() if not r['agrees']]
    for line in bad:
        print('disagrees:', line, file=sys.stderr)
    if '--check' in sys.argv:
        recorded = json.load(open(out_path))
        for n, e in result['exports'].items():
            r = recorded['exports'].get(n)
            if r is None or r['content'] != e['content'] or r['counts'] != e['counts']:
                bad.append(f'{n}: differs from exports.json (recorded with {recorded["generated_by"]})')
                print('differs from exports.json:', n, file=sys.stderr)
        print(f'{len(names)} exports read with {versions}')
    else:
        with open(out_path, 'w') as f:
            json.dump(result, f, indent=1, sort_keys=True)
            f.write('\n')
        print(out_path)
    sys.exit(1 if bad else 0)


main()
