# The pathology fixture's post-Xenium images as tifffile and imagecodecs read them: each image's
# levels, and the SHA-256 of every level of every channel (row-major; an RGB pixel's samples
# together; samples wider than a byte little-endian), for validation/images_test.go. Maintainer-run:
#
#   uv run --with tifffile --with imagecodecs --with numpy python -I levels.py <validation/cache>
import hashlib, json, os, sys
import numpy as np
import tifffile, imagecodecs

root = os.path.join(sys.argv[1], 'xenium-breast-cancer-rep1', 'images')
out = {'tifffile': tifffile.__version__, 'imagecodecs': imagecodecs.__version__, 'images': {}}
for name in sorted(os.listdir(root)):
    if not name.endswith('.ome.tif'):
        continue
    with tifffile.TiffFile(os.path.join(root, name)) as t:
        series = t.series[0]
        levels = []
        for lv in series.levels:
            data = lv.asarray()
            axes = lv.axes
            planes = [data[c] for c in range(data.shape[0])] if axes.startswith('C') else [data]
            levels.append({'shape': list(data.shape), 'axes': axes,
                           'sha256': [hashlib.sha256(np.ascontiguousarray(p).astype(p.dtype.newbyteorder('<')).tobytes()).hexdigest() for p in planes]})
            del data, planes
        out['images'][name] = {'levels': levels}
    print(name, len(levels), 'levels', flush=True)
with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'xenium-breast-cancer-rep1.json'), 'w') as f:
    json.dump(out, f, indent=1)
    f.write('\n')
