#!/bin/sh
# SituCraft's design matrices against patsy's for the same formulas (wave 7, slice 3). Writes
# validation/reference/designs/patsy.json, which web/lib/design-reference.test.mjs compares with
# SituCraft's on every commit. Run by the maintainer when cases.json changes.
#
# Usage: validation/reference/designs/compare.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
uv run -q --python 3.12 --with 'patsy==1.0.3' --with 'pandas==3.0.6' --with 'numpy==2.5.4' \
  python -I "$here/compare.py" "$here/cases.json" "$here/patsy.json"
