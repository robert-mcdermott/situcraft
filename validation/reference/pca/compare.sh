#!/bin/sh
# SituCraft's principal components of a cohort's pseudobulk against numpy's (wave 7, slice 5).
# Writes validation/reference/pca/numpy.json, which internal/cohort's test compares with
# SituCraft's on every commit. Run by the maintainer when cases.json changes (make_cases.py).
#
# Usage: validation/reference/pca/compare.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
uv run -q --python 3.12 --with 'numpy==2.5.4' python -I "$here/compare.py" "$here/cases.json" "$here/numpy.json"
