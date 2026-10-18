#!/bin/sh
# SituCraft's cell graphs of the reference data sets against SciPy's and Squidpy's (wave 6, slice 1;
# research.md §6.1). Run by the maintainer after
#
#   SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceGraphs -v
#
# wrote them into <data>/graphs/<set>/; writes validation/reference/graphs/<set>.json, whose numbers
# the Go test checks weekly against SituCraft's graphs made again.
#
# Usage: validation/reference/graphs/compare.sh [data set …]
#   data sets: xenium-lung-2fov xenium-breast-2fov simulated-standard (all by default)
set -eu
here=$(cd "$(dirname "$0")/../../.." && pwd)
data=${SITUCRAFT_DATA:-$here/validation/cache}
sets=${*:-xenium-lung-2fov xenium-breast-2fov simulated-standard}
for set in $sets; do
  uv run -q --python 3.12 \
    --with 'squidpy==1.8.3' --with 'anndata==0.13.4' --with 'numpy==2.5.4' --with 'scipy==1.18.1' --with 'scikit-learn==1.9.1' \
    python -I "$here/validation/reference/graphs/compare.py" "$data" "$set" "$here/validation/reference/graphs/$set.json"
done
