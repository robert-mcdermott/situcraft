# Writes the TIFFs the tests read, with tifffile and imagecodecs: each container and codec choice a
# reader's code depends on (byte order, BigTIFF, strips and tiles with partial edges, LZW, deflate,
# PackBits, JPEG (YCbCr and grey), both predictors, planar RGB, SubIFD pyramids, OME-XML), and
# expected.json with the SHA-256 of every image directory's pixels (row-major, samples
# little-endian).
#
#   uv run --with tifffile --with imagecodecs --with numpy python -I make_fixtures.py
#
# The images are synthetic (seeded).
import hashlib, json, os
import numpy as np
import tifffile

here = os.path.dirname(os.path.abspath(__file__))
rng = np.random.default_rng(20261014)
H, W = 29, 37  # odd sizes: partial strips and tiles

def image(dtype, shape):
    y, x = np.mgrid[0:shape[0], 0:shape[1]]
    base = (x * 37 + y * 91 + (x * y) % 13).astype('f8')
    if len(shape) == 3:
        base = np.stack([base + 50 * k for k in range(shape[2])], axis=-1)
    if np.dtype(dtype).kind == 'f':
        return (np.sin(base / 40.0) * 1000 + rng.normal(size=base.shape)).astype(dtype)
    info = np.iinfo(dtype)
    return (base * 97 % info.max).astype(dtype)

cases = []
def digest(a):
    return hashlib.sha256(np.ascontiguousarray(a).astype(a.dtype.newbyteorder('<')).tobytes()).hexdigest()

def add(name, levels, expected=None, **kwargs):
    path = os.path.join(here, name + '.tif')
    subifds = len(levels) - 1
    with tifffile.TiffWriter(path, bigtiff=kwargs.pop('bigtiff', False), byteorder=kwargs.pop('byteorder', '<'), ome=kwargs.pop('ome', False)) as tif:
        tif.write(levels[0], subifds=subifds or None, **kwargs)
        for level in levels[1:]:
            tif.write(level, subfiletype=1, **{k: v for k, v in kwargs.items() if k not in ('metadata', 'description')})
    # What a reader returns: samples of a pixel together (expected overrides for planar files).
    cases.append({'name': name, 'levels': [{'shape': list(l.shape), 'dtype': str(l.dtype), 'sha256': digest(l)} for l in (expected or levels)]})

add('u8-lzw-strips', [image('u1', (H, W))], compression='lzw', rowsperstrip=8)
add('u16-lzw-predictor', [image('u2', (H, W))], compression='lzw', predictor=2)
add('u16-deflate-predictor-be-tiles', [image('u2', (H, W))], compression='zlib', predictor=2, byteorder='>', tile=(16, 16))
add('rgb-lzw-predictor', [image('u1', (H, W, 3))], compression='lzw', predictor=2, photometric='rgb')
rgb = image('u1', (H, W, 3))
add('rgb-deflate-planar', [np.ascontiguousarray(rgb.transpose(2, 0, 1))], expected=[rgb], compression='zlib', photometric='rgb', planarconfig='separate', rowsperstrip=10)
add('f32-deflate-floatpredictor', [image('f4', (H, W))], compression='zlib', predictor=3)
add('f32-deflate-floatpredictor-be', [image('f4', (H, W))], compression='zlib', predictor=3, byteorder='>')
add('u8-packbits', [image('u1', (H, W))], compression='packbits')
add('u16-none-be', [image('u2', (H, W))], byteorder='>')
full = image('u2', (70, 90))
add('bigtiff-pyramid', [full, full[::2, ::2].copy(), full[::4, ::4].copy()], bigtiff=True, compression='zlib', tile=(32, 32))
add('ome-3channels', [image('u2', (3, H, W))], ome=True, compression='zlib', photometric='minisblack',
    metadata={'axes': 'CYX', 'PhysicalSizeX': 0.5, 'PhysicalSizeY': 0.5, 'Channel': {'Name': ['DAPI', 'CD45', 'PanCK']}})

# JPEG is lossy and decoders round differently: a JPEG case's expected pixels are libjpeg-turbo's
# (imagecodecs), kept beside it (<name>.raw), and a reader must come within a tolerance of them.
def add_jpeg(name, data, tolerance, **kwargs):
    path = os.path.join(here, name + '.tif')
    tifffile.imwrite(path, data, compression='jpeg', **kwargs)
    decoded = tifffile.imread(path)
    with open(os.path.join(here, name + '.raw'), 'wb') as f:
        f.write(np.ascontiguousarray(decoded).tobytes())
    cases.append({'name': name, 'tolerance': tolerance, 'levels': [{'shape': list(decoded.shape), 'dtype': str(decoded.dtype), 'sha256': digest(decoded)}]})

y, x = np.mgrid[0:48, 0:56]
photo = np.stack([(120 + 80 * np.sin(x / 7.0 + y / 11.0)), (90 + 60 * np.cos(y / 5.0)), (160 + 50 * np.sin((x - y) / 9.0))], axis=-1).astype('u1')
add_jpeg('rgb-jpeg-ycbcr-tiles', photo, 3, photometric='ycbcr', tile=(16, 16))
add_jpeg('u8-jpeg-strips', photo[..., 1].copy(), 3, photometric='minisblack', rowsperstrip=16)

with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({'tifffile': tifffile.__version__, 'cases': cases}, f, indent=1)
    f.write('\n')
print(len(cases), 'files')
