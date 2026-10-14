# Writes the Zarr stores the tests read, with zarr-python 3 and numcodecs: format 2 (directory and
# zip) and format 3 arrays covering each choice a reader's code depends on, and expected.json with
# the SHA-256 of every array's values (C order, little-endian).
#
#   uv run --with zarr --with numcodecs --with numpy python -I make_fixtures.py
import hashlib, json, os, shutil, zipfile
import numpy as np
import numcodecs
import zarr
from zarr.codecs import BloscCodec, BytesCodec, Crc32cCodec, GzipCodec, TransposeCodec, ZstdCodec

here = os.path.dirname(os.path.abspath(__file__))
rng = np.random.default_rng(20261014)
expected = []

def digest(a):
    a = np.ascontiguousarray(a)
    if a.dtype.byteorder == '>':
        a = a.astype(a.dtype.newbyteorder('<'))
    return hashlib.sha256(a.tobytes()).hexdigest()

def record(store, path, a, partial=None):
    expected.append({'store': store, 'path': path, 'shape': list(a.shape), 'dtype': a.dtype.str, 'sha256': digest(a if partial is None else partial)})

# Format 2, in a directory.
v2 = os.path.join(here, 'v2.zarr')
shutil.rmtree(v2, ignore_errors=True)
root = zarr.open_group(v2, mode='w', zarr_format=2)
grid = (np.arange(37 * 29).reshape(37, 29) * 1.5).astype('<f4')
def v2array(name, a, chunks, compressor, **kw):
    arr = root.create_array(name, shape=a.shape, chunks=chunks, dtype=a.dtype, compressors=compressor, **kw)
    arr[...] = a
    record('v2.zarr', name, a)
v2array('blosc_lz4_shuffle', grid, (10, 8), numcodecs.Blosc('lz4', 5, numcodecs.Blosc.SHUFFLE))
v2array('blosc_zstd_bitshuffle', grid, (16, 16), numcodecs.Blosc('zstd', 3, numcodecs.Blosc.BITSHUFFLE))
v2array('zstd', grid, (37, 29), numcodecs.Zstd(level=3))
v2array('zlib', grid, (20, 29), numcodecs.Zlib(level=6))
v2array('gzip', grid, (20, 29), numcodecs.GZip(level=6))
v2array('lz4', grid, (20, 29), numcodecs.LZ4())
v2array('none', grid, (20, 29), None)
v2array('order_f', grid, (10, 8), numcodecs.Blosc('lz4', 5, numcodecs.Blosc.SHUFFLE), order='F')
v2array('big_endian', (np.arange(200) * 1000003).astype('>i4'), (64,), numcodecs.Zstd(level=1))
v2array('three_d', rng.integers(0, 60000, size=(5, 7, 9), dtype='<u2'), (2, 3, 4), numcodecs.Blosc('lz4', 5, numcodecs.Blosc.SHUFFLE))
v2array('slash_separator', grid, (10, 10), numcodecs.Zstd(level=1), chunk_key_encoding={'name': 'v2', 'separator': '/'})
v2array('bools', (np.arange(100) % 3 == 0), (32,), numcodecs.Zlib())
v2array('text', np.array(['DAPI', 'Boundary', 'Interior RNA', '18S'], dtype='<U12'), (4,), None)
v2array('bytes', np.array([b'aiaafdpo-1', b'bbbbcccc-1'], dtype='|S10'), (2,), None)
# Partly written: the chunks never written read as the fill value.
sparse = root.create_array('fill_value', shape=(30, 30), chunks=(10, 10), dtype='<f8', fill_value=-1.0, compressors=numcodecs.Zstd(level=1))
sparse[0:10, 0:10] = 7.0
full = np.full((30, 30), -1.0); full[0:10, 0:10] = 7.0
record('v2.zarr', 'fill_value', full)
root.attrs['note'] = 'SituCraft test fixture'

# The same store as a zip with uncompressed members, as Xenium writes its stores.
with zipfile.ZipFile(os.path.join(here, 'v2.zarr.zip'), 'w', zipfile.ZIP_STORED) as z:
    for folder, _, files in os.walk(v2):
        for f in sorted(files):
            full_path = os.path.join(folder, f)
            z.write(full_path, os.path.relpath(full_path, v2))
for e in list(expected):
    expected.append(dict(e, store='v2.zarr.zip'))

# Format 3.
v3 = os.path.join(here, 'v3.zarr')
shutil.rmtree(v3, ignore_errors=True)
root3 = zarr.open_group(v3, mode='w', zarr_format=3)
def v3array(name, a, chunks, **kw):
    arr = root3.create_array(name, shape=a.shape, chunks=chunks, dtype=a.dtype, **kw)
    arr[...] = a
    record('v3.zarr', name, a)
v3array('zstd', grid, (10, 8), compressors=ZstdCodec(level=3))
v3array('blosc', grid, (16, 16), compressors=BloscCodec(cname='lz4', clevel=5, shuffle='shuffle'))
v3array('gzip_crc32c', grid, (16, 16), compressors=[GzipCodec(level=5), Crc32cCodec()])
v3array('big_endian', (np.arange(300) * 7).astype('>i8'), (100,), serializer=BytesCodec(endian='big'), compressors=None)
v3array('transposed', grid, (10, 8), filters=[TransposeCodec(order=(1, 0))], compressors=ZstdCodec(level=1))
sharded = root3.create_array('sharded', shape=grid.shape, chunks=(8, 8), shards=(16, 16), dtype=grid.dtype)
sharded[...] = grid

with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({'zarr': zarr.__version__, 'numcodecs': numcodecs.__version__, 'arrays': expected}, f, indent=1)
    f.write('\n')
print(len(expected), 'arrays')
