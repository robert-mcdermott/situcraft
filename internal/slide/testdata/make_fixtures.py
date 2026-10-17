# Writes the post-Xenium images the tests read, with tifffile and imagecodecs, as Bio-Formats and
# tifffile write them for Xenium Explorer: pyramidal, tiled OME-TIFFs, their levels in SubIFDs. An
# H&E (RGB, 8 bits) deflate and JPEG; an immunofluorescence image of named channels (16 bits,
# deflate with a predictor; 8 bits, JPEG 2000); and two SituCraft refuses (a flat image and a
# pyramid in strips). expected.json holds what each is and the SHA-256 of every level of every
# channel (row-major; an RGB pixel's samples together; 16-bit samples little-endian); a JPEG
# image's levels also as libjpeg-turbo decodes them (<name>-<level>.raw), for a tolerance.
#
#   uv run --with tifffile --with imagecodecs --with numpy python -I make_fixtures.py
import hashlib, json, os
import numpy as np
import tifffile

here = os.path.dirname(os.path.abspath(__file__))
H, W = 70, 90  # partial tiles at every level

def rgb(h, w):
    y, x = np.mgrid[0:h, 0:w]
    return np.stack([(150 + 60 * np.sin(x / 9.0)), (90 + 50 * np.cos(y / 7.0)), (170 + 40 * np.sin((x + y) / 11.0))], axis=-1).astype('u1')

def channels(h, w, dtype, n):
    y, x = np.mgrid[0:h, 0:w]
    top = np.iinfo(dtype).max
    return np.stack([((x * (37 + 11 * c) + y * (91 - 7 * c) + (x * y) % 13) * 97 % top) for c in range(n)]).astype(dtype)

def pyramid(a, levels, axes):
    out = [a]
    for _ in range(levels):
        a = a[..., ::2, ::2, :] if axes.endswith('S') else a[..., ::2, ::2]
        out.append(np.ascontiguousarray(a))
    return out

def digest(a):
    return hashlib.sha256(np.ascontiguousarray(a).astype(a.dtype.newbyteorder('<')).tobytes()).hexdigest()

cases = []
def write(name, levels, axes, metadata, keep_raw=False, **kwargs):
    path = os.path.join(here, name + '.ome.tif')
    with tifffile.TiffWriter(path, ome=True) as tif:
        tif.write(levels[0], subifds=len(levels) - 1, metadata={'axes': axes, **metadata}, **kwargs)
        for level in levels[1:]:
            tif.write(level, subfiletype=1, **kwargs)
    case = {'name': name, 'levels': []}
    with tifffile.TiffFile(path) as t:
        for l, lv in enumerate(t.series[0].levels):
            data = lv.asarray()
            entry = {'shape': list(data.shape)}
            if axes.startswith('C'):
                entry['sha256'] = [digest(data[c]) for c in range(data.shape[0])]
            else:
                entry['sha256'] = [digest(data)]
            if keep_raw:
                with open(os.path.join(here, f'{name}-{l}.raw'), 'wb') as f:
                    f.write(np.ascontiguousarray(data).tobytes())
            case['levels'].append(entry)
    cases.append(case)

he = rgb(H, W)
write('he-deflate', pyramid(he, 2, 'YXS'), 'YXS', {'PhysicalSizeX': 0.3638, 'PhysicalSizeY': 0.3638}, photometric='rgb', compression='zlib', tile=(16, 16))
write('he-jpeg', pyramid(he, 2, 'YXS'), 'YXS', {'PhysicalSizeX': 0.3638, 'PhysicalSizeY': 0.3638}, keep_raw=True, photometric='ycbcr', compression='jpeg', tile=(16, 16))
names = {'Channel': {'Name': ['CD20', 'HER2', 'DAPI']}}
write('if-u16-deflate', pyramid(channels(H, W, 'u2', 3), 2, 'CYX'), 'CYX', {'PhysicalSizeX': 0.2125, 'PhysicalSizeY': 0.2125, **names}, photometric='minisblack', compression='zlib', predictor=2, tile=(16, 16))
write('if-u8-jp2k', pyramid(channels(H, W, 'u1', 3), 2, 'CYX'), 'CYX', names, photometric='minisblack', compression='jpeg2000', compressionargs={'level': 0}, tile=(32, 32))

# Refused: a flat image larger than a screen shows whole, and a pyramid stored in strips.
tifffile.imwrite(os.path.join(here, 'flat.ome.tif'), np.zeros((8, 5000), 'u1'), ome=True, compression='zlib', tile=(16, 16), metadata={'axes': 'YX'})
with tifffile.TiffWriter(os.path.join(here, 'strips.ome.tif'), ome=True) as tif:
    levels = pyramid(he, 1, 'YXS')
    tif.write(levels[0], subifds=1, photometric='rgb', compression='zlib', rowsperstrip=8, metadata={'axes': 'YXS'})
    tif.write(levels[1], subfiletype=1, photometric='rgb', compression='zlib', rowsperstrip=8)

with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({'tifffile': tifffile.__version__, 'cases': cases}, f, indent=1)
    f.write('\n')
print(len(cases), 'images')
