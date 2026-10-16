#!/bin/sh
# SituCraft's labels from markers of the reference data sets against scanpy's score_genes (wave 4,
# slice 3; research.md §5.3). Run by the maintainer after
#
#   SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceLabels -v
#
# wrote them into <data>/clustering/<set>/labels/; writes validation/reference/labels/<set>.json,
# whose numbers the Go test checks weekly against SituCraft's labels made again.
#
# Usage: validation/reference/labels/compare.sh [data set …]
#   data sets: xenium-lung-2fov xenium-breast-2fov simulated-standard (all by default)
set -eu
here=$(cd "$(dirname "$0")/../../.." && pwd)
data=${SITUCRAFT_DATA:-$here/validation/cache}
sets=${*:-xenium-lung-2fov xenium-breast-2fov simulated-standard}
for set in $sets; do
  echo "$set"
  uv run -q --python 3.12 \
    --with 'scanpy==1.12.4' --with 'anndata==0.13.4' \
    --with 'numpy==2.5.4' --with 'scipy==1.18.1' --with 'pandas==3.0.6' --with 'pyarrow==26.0.0' --with 'h5py==3.16.0' \
    python -I "$here/validation/reference/labels/compare.py" "$data" "$set" "$here/validation/reference/labels/$set.json"
done
