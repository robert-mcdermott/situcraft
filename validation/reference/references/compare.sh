#!/bin/sh
# SituCraft's labels from a reference against SingleR's (wave 4, slice 6; research.md §5.6), run
# by the maintainer in three steps:
#
#   validation/reference/references/compare.sh reference
#       writes the Human Primary Cell Atlas's profiles into <data>/references/ (R, celldex)
#   SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceMapping -v
#       maps each data set's cells, and writes them for SingleR
#   validation/reference/references/compare.sh [data set …]
#       runs SingleR on them and writes validation/reference/references/<set>.json, whose numbers
#       the Go test checks weekly against SituCraft's labels made again
#
# R's packages (SingleR, celldex, from Bioconductor) live in <data>/R-library:
#   R_LIBS_USER=<data>/R-library Rscript -e 'BiocManager::install(c("SingleR", "celldex"), lib = Sys.getenv("R_LIBS_USER"))'
#
#   data sets: xenium-lung-2fov xenium-breast-2fov simulated-standard (all by default)
set -eu
here=$(cd "$(dirname "$0")/../../.." && pwd)
data=${SITUCRAFT_DATA:-$here/validation/cache}
export R_LIBS_USER="$data/R-library"
export GYPSUM_CACHE_DIR="$data/R-gypsum"
if [ "${1:-}" = reference ]; then
  Rscript "$here/validation/reference/references/hpca.R" "$data/references"
  exit 0
fi
sets=${*:-xenium-lung-2fov xenium-breast-2fov simulated-standard}
for set in $sets; do
  echo "$set"
  Rscript "$here/validation/reference/references/singler.R" "$data" "$set"
  uv run -q --python 3.12 \
    --with 'numpy==2.5.4' --with 'pandas==3.0.6' --with 'pyarrow==26.0.0' \
    python -I "$here/validation/reference/references/compare.py" "$data" "$set" "$here/validation/reference/references/$set.json"
done
