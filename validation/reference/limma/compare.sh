#!/bin/sh
# SituCraft's moderated linear models of pseudobulk counts and of spatial statistics against
# limma's and edgeR's, and the statistics' ordinary least squares against statsmodels' (wave 7,
# slice 7). Writes validation/reference/limma/limma.json and statsmodels.json, which
# internal/cohort's tests compare with SituCraft's on every commit. Run by the maintainer when
# cases.json changes (make_cases.py), with R and the Bioconductor packages limma and edgeR, and
# jsonlite, installed.
#
# Usage: validation/reference/limma/compare.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
Rscript "$here/compare.R" "$here/cases.json" "$here/limma.json"
uv run -q --python 3.12 --with 'statsmodels==0.15.0' --with 'numpy==2.5.4' --with 'pandas==3.0.6' --with 'patsy==1.0.3' \
  python -I "$here/compare_ols.py" "$here/cases.json" "$here/statsmodels.json"
