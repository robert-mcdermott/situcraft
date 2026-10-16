#!/bin/sh
# SituCraft's clusterings of the reference data sets against scanpy, leidenalg, umap-learn and
# scikit-learn (wave 4, slice 1; research.md §5). Run by the maintainer after
#
#   SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceClustering -v
#
# wrote them into <data>/clustering/<set>/; writes validation/reference/clustering/<set>.json, whose
# numbers the Go test checks weekly against SituCraft's clustering made again.
#
# Usage: validation/reference/clustering/compare.sh [data set …]
#   data sets: xenium-lung-2fov xenium-breast-2fov simulated-standard (all by default)
set -eu
here=$(cd "$(dirname "$0")/../../.." && pwd)
data=${SITUCRAFT_DATA:-$here/validation/cache}
sets=${*:-xenium-lung-2fov xenium-breast-2fov simulated-standard}
for set in $sets; do
  echo "$set"
  uv run -q --python 3.12 \
    --with 'scanpy==1.12.4' --with 'anndata==0.13.4' --with 'leidenalg==0.12.0' --with 'igraph==1.0.0' \
    --with 'umap-learn==0.5.12' --with 'pynndescent==0.6.0' --with 'scikit-learn==1.9.1' \
    --with 'numpy==2.5.4' --with 'scipy==1.18.1' --with 'pandas==3.0.6' --with 'pyarrow==26.0.0' --with 'h5py==3.16.0' \
    python -I "$here/validation/reference/clustering/compare.py" "$data" "$set" "$here/validation/reference/clustering/$set.json"
done
