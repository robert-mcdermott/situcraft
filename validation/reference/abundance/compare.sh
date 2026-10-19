#!/bin/sh
# SituCraft's quasi-likelihood models of counts by sample against statsmodels' (wave 7, slice 6).
# Writes validation/reference/abundance/statsmodels.json, which internal/cohort's test compares
# with SituCraft's on every commit. Run by the maintainer when cases.json changes (make_cases.py).
#
# Usage: validation/reference/abundance/compare.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
uv run -q --python 3.12 --with 'statsmodels==0.15.0' --with 'numpy==2.5.4' --with 'pandas==3.0.6' --with 'patsy==1.0.3' \
  python -I "$here/compare.py" "$here/cases.json" "$here/statsmodels.json"
