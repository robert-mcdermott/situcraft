# The development bundles as independent readers see them, for validation/readers_test.go: every
# Parquet column (DuckDB), every Zarr array (zarr-python and numcodecs), and the structure of every
# TIFF with the pixels of each image plane's smallest pyramid level (tifffile, decoding JPEG 2000
# with imagecodecs; those levels are lossless 5/3, so every decoder must agree exactly), reduced to
# sizes and SHA-256 digests in xenium-bundles.json.
#
#   uv run --with duckdb --with zarr --with numcodecs --with tifffile --with imagecodecs --with numpy \
#       python -I validation/reference/generate_bundles.py
#
# Column digests: for each row in file order, 0x00 for a null, else 0x01 then the value as its
# Parquet physical type stores it (little-endian integers and IEEE floats; byte arrays as a 4-byte
# length and the bytes). Array digests: the values in C order, little-endian.
import hashlib, json, os, struct, sys, zipfile
import duckdb
import numpy as np
import tifffile
import zarr
from zarr.storage import ZipStore

here = os.path.dirname(os.path.abspath(__file__))
cache = os.environ.get('SITUCRAFT_DATA') or os.path.join(here, '..', 'cache')
PACK = {'INT32': '<i4', 'INT64': '<i8', 'FLOAT': '<f4', 'DOUBLE': '<f8', 'BOOLEAN': '|b1'}

def column_digest(con, path, name, physical):
    values = con.execute(f'SELECT "{name}" FROM read_parquet(?)', [path]).fetchnumpy()[name]
    mask = np.ma.getmaskarray(values) if np.ma.isMaskedArray(values) else np.zeros(len(values), bool)
    h = hashlib.sha256()
    if physical in PACK and not mask.any():
        width = np.dtype(PACK[physical]).itemsize
        data = np.asarray(values)
        if physical in ('INT32', 'INT64'):
            data = data.astype(np.int64).astype(PACK[physical]) if data.dtype.kind == 'u' else data.astype(PACK[physical])
        rows = np.zeros(len(data), dtype=[('flag', 'u1'), ('v', PACK[physical])])
        rows['flag'] = 1
        rows['v'] = data
        h.update(rows.tobytes())
    else:
        for value, null in zip(np.ma.getdata(values) if np.ma.isMaskedArray(values) else values, mask):
            if null or value is None:
                h.update(b'\x00')
                continue
            h.update(b'\x01')
            if physical == 'BYTE_ARRAY':
                b = value.encode() if isinstance(value, str) else bytes(value)
                h.update(struct.pack('<I', len(b)) + b)
            else:
                h.update(np.array([value]).astype(PACK[physical]).tobytes())
    return h.hexdigest(), int(mask.sum())

def parquet_reference(path):
    con = duckdb.connect()
    schema = con.execute("SELECT name, type FROM parquet_schema(?) WHERE num_children IS NULL OR num_children = 0", [path]).fetchall()
    rows = con.execute('SELECT count(*) FROM read_parquet(?)', [path]).fetchone()[0]
    columns = []
    for name, physical in schema:
        digest, nulls = column_digest(con, path, name, physical)
        columns.append({'name': name, 'physical': physical, 'nulls': nulls, 'sha256': digest})
    return {'rows': rows, 'columns': columns}

def zarr_reference(path):
    arrays = []
    store = ZipStore(path, mode='r')
    root = zarr.open_group(store, mode='r')
    def walk(group, prefix):
        for name, node in sorted(group.members()):
            full = f'{prefix}{name}'
            if isinstance(node, zarr.Array):
                a = np.ascontiguousarray(node[...])
                if a.dtype.byteorder == '>':
                    a = a.astype(a.dtype.newbyteorder('<'))
                arrays.append({'path': full, 'shape': list(a.shape), 'dtype': a.dtype.str, 'sha256': hashlib.sha256(a.tobytes()).hexdigest()})
            else:
                walk(node, full + '/')
    walk(root, '')
    store.close()
    return arrays

def tiff_reference(path):
    with tifffile.TiffFile(path) as t:
        pages = []
        for page in t.pages:
            levels = [page] + list(page.pages or [])
            pages.append([{'width': p.imagewidth, 'height': p.imagelength, 'tiles': len(p.dataoffsets), 'compression': int(p.compression), 'bits': p.bitspersample} for p in levels])
        smallest = []
        for page in t.pages:
            level = ([page] + list(page.pages or []))[-1]
            pixels = np.ascontiguousarray(level.asarray())
            pixels = pixels.astype(pixels.dtype.newbyteorder('<'))
            smallest.append(hashlib.sha256(pixels.tobytes()).hexdigest())
        return {'pages': pages, 'ome': t.is_ome, 'smallest_level_sha256': smallest}

import imagecodecs
out = {'generated_by': f'duckdb {duckdb.__version__}, zarr {zarr.__version__}, tifffile {tifffile.__version__}, imagecodecs {imagecodecs.__version__}', 'bundles': {}}
for bundle in ['xenium-lung-2fov', 'xenium-breast-2fov']:
    root = os.path.join(cache, bundle)
    if not os.path.isdir(root):
        sys.exit(f'{root} is missing: run node validation/fetch.mjs first')
    entry = {'parquet': {}, 'zarr': {}, 'tiff': {}}
    for name in sorted(os.listdir(root)):
        path = os.path.join(root, name)
        if name.endswith('.parquet'):
            entry['parquet'][name] = parquet_reference(path)
        elif name.endswith('.zarr.zip'):
            entry['zarr'][name] = zarr_reference(path)
    for folder in ['', 'morphology_focus']:
        for name in sorted(os.listdir(os.path.join(root, folder))):
            if name.endswith('.ome.tif'):
                entry['tiff'][os.path.join(folder, name)] = tiff_reference(os.path.join(root, folder, name))
    out['bundles'][bundle] = entry
    print(bundle, len(entry['parquet']), 'Parquet files,', sum(len(v) for v in entry['zarr'].values()), 'Zarr arrays,', len(entry['tiff']), 'TIFFs')
with open(os.path.join(here, 'xenium-bundles.json'), 'w') as f:
    json.dump(out, f, indent=1)
    f.write('\n')
