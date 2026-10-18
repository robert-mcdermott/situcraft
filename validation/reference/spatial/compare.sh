#!/bin/sh
# SituCraft's neighborhood enrichment, co-occurrence, nearest distances and Ripley's K of the
# reference data sets against Squidpy's, SciPy's and astropy's (wave 6, slices 2 and 3; research.md
# §6.2, §6.3). Run by the maintainer after
#
#   SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceSpatial -v
#
# wrote them into <data>/spatial/<set>/; writes validation/reference/spatial/<set>.json, whose
# numbers the Go test checks weekly against SituCraft's results made again.
#
# Usage: validation/reference/spatial/compare.sh [data set …]
#   data sets: xenium-lung-2fov xenium-breast-2fov simulated-standard (all by default)
set -eu
here=$(cd "$(dirname "$0")/../../.." && pwd)
data=${SITUCRAFT_DATA:-$here/validation/cache}
sets=${*:-xenium-lung-2fov xenium-breast-2fov simulated-standard}
for set in $sets; do
  uv run -q --python 3.12 \
    --with 'squidpy==1.8.3' --with 'anndata==0.13.4' --with 'numpy==2.5.4' --with 'scipy==1.18.1' --with 'scikit-learn==1.9.1' --with 'pandas==3.0.6' --with 'astropy==8.0.2' \
    python -I "$here/validation/reference/spatial/compare.py" "$data" "$set" "$here/validation/reference/spatial/$set.json"
done
