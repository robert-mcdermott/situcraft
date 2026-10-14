# Writes the Blosc chunks the tests decode: each combination of codec, shuffle, type size, block
# size and length that a decoder's choices depend on (splits, the short last block, stored streams,
# stored chunks), compressed by c-blosc through numcodecs, with the SHA-256 and size of the data.
#
#   uv run --with numcodecs --with numpy python -I make_fixtures.py
#
# The data are synthetic (seeded); expected.json records what each chunk must decode to.
import hashlib, json, os
import numpy as np
from numcodecs import Blosc, __version__ as numcodecs_version

here = os.path.dirname(os.path.abspath(__file__))
rng = np.random.default_rng(20261014)

def data(dtype, n):
    if dtype.startswith('V'):
        return rng.integers(0, 4, size=n * int(dtype[1:]), dtype=np.uint8).view(dtype)
    if dtype.startswith('f'):
        return (np.cumsum(rng.normal(size=n)) * 3.5).astype(dtype)
    info = np.iinfo(dtype)
    return (np.arange(n) * 7 % min(int(info.max), 5003)).astype(dtype)

cases = []
def add(name, array, **blosc):
    raw = array.tobytes()
    chunk = Blosc(**blosc).encode(array)
    with open(os.path.join(here, name + '.blosc'), 'wb') as f:
        f.write(chunk)
    cases.append({'name': name, 'nbytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(), 'blosc': blosc})

for cname in ['lz4', 'lz4hc', 'zstd', 'zlib']:
    for shuffle, label in [(0, 'noshuffle'), (1, 'shuffle'), (2, 'bitshuffle')]:
        for dtype in ['u1', 'u2', 'f4', 'u8']:
            add(f'{cname}-{label}-{dtype}', data(dtype, 6000), cname=cname, clevel=5, shuffle=shuffle)
# Several blocks with a short last one (a split decision for each), and odd lengths.
for shuffle, label in [(1, 'shuffle'), (2, 'bitshuffle')]:
    add(f'lz4-{label}-f4-blocks', data("f4", 20003), cname='lz4', clevel=5, shuffle=shuffle, blocksize=16384)
    add(f'zstd-{label}-u2-blocks', data("u2", 20001), cname='zstd', clevel=3, shuffle=shuffle, blocksize=8192)
    add(f'lz4-{label}-f8-small', data('f8', 13), cname='lz4', clevel=5, shuffle=shuffle)
# Types larger than the split limit (16 bytes), and a 20-byte record type.
add('lz4-shuffle-V20', data('V20', 3001), cname='lz4', clevel=5, shuffle=1)
add('lz4-shuffle-V32', data('V32', 2000), cname='lz4', clevel=5, shuffle=1)
add('lz4-bitshuffle-V20', data('V20', 3001), cname='lz4', clevel=5, shuffle=2)
# Incompressible data: streams stored as they are; and clevel 0: the whole chunk stored.
noise = rng.integers(0, 2**32, size=3000, dtype=np.uint32)
add('lz4-shuffle-noise', noise, cname='lz4', clevel=5, shuffle=1)
add('zstd-noshuffle-noise', noise, cname='zstd', clevel=5, shuffle=0)
add('lz4-clevel0', data('f4', 5000), cname='lz4', clevel=0, shuffle=1)
add('lz4-empty', np.zeros(0, dtype='f4'), cname='lz4', clevel=5, shuffle=1)

with open(os.path.join(here, 'expected.json'), 'w') as f:
    json.dump({'numcodecs': numcodecs_version, 'cases': cases}, f, indent=1)
    f.write('\n')
print(len(cases), 'chunks written')
