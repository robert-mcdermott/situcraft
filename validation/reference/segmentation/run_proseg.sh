#!/bin/sh
# Proseg 3.2.0's segmentations of the validation data sets, the independent reference SituCraft's
# transcript-informed segmentation is compared with (wave 3, slice 6; research.md §3.2). Run by the
# maintainer; the outputs stay in validation/cache/proseg/ (not committed) and are reduced to
# numbers by
#
#   SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceSegmentation -v -timeout 2h
#
# which writes validation/reference/segmentation/<data set>.json.
#
# Proseg runs with its Xenium preset in two configurations:
#   one-layer  one voxel layer in depth, as SituCraft's default (the like-for-like reference);
#   defaults   Proseg's own defaults (four layers), as a user would run it.
# It has no seed, so each is run twice, to measure how much two of its runs differ. Each run's time
# and peak memory are recorded (proseg.err). Install it with:
#   cargo install proseg --version 3.2.0 --locked     (https://github.com/dcjones/proseg)
#
# Usage: validation/reference/segmentation/run_proseg.sh [data set …]
#   data sets: xenium-lung-2fov xenium-breast-2fov simulated-standard (all by default)
set -eu
here=$(cd "$(dirname "$0")/../../.." && pwd)
data=${SITUCRAFT_DATA:-$here/validation/cache}
proseg=${PROSEG:-proseg}
want="3.2.0"
version=$("$proseg" --version | awk '{print $2}')
if [ "$version" != "$want" ]; then
  echo "Proseg $want is needed; $proseg is $version" >&2
  exit 1
fi
case "$(uname)" in
  Darwin) timer="/usr/bin/time -l" ;;
  *) timer="/usr/bin/time -v" ;;
esac
sets=${*:-xenium-lung-2fov xenium-breast-2fov simulated-standard}
for set in $sets; do
  bundle="$data/$set"
  if [ "$set" = simulated-standard ] && [ ! -d "$bundle" ]; then
    # The standard simulated tissue, the same byte for byte on any computer.
    (cd "$here" && go run . simulate --preset standard --quiet "$bundle")
  fi
  if [ ! -f "$bundle/transcripts.parquet" ]; then
    echo "No $bundle/transcripts.parquet (node validation/fetch.mjs $set)" >&2
    exit 1
  fi
  for config in one-layer defaults; do
    case $config in
      one-layer) flags="--voxel-layers 1" ;;
      defaults) flags="" ;;
    esac
    for run in 1 2; do
      out="$data/proseg/$set/$config-$run"
      if [ -d "$out/proseg-output.zarr" ]; then
        echo "$out: made already"
        continue
      fi
      mkdir -p "$out"
      echo "$set, $config, run $run"
      # shellcheck disable=SC2086 # the flags are words
      (cd "$out" && $timer "$proseg" --xenium "$bundle/transcripts.parquet" $flags --output-spatialdata proseg-output.zarr --overwrite > proseg.log 2> proseg.err)
      printf '{"proseg": "%s", "command": "proseg --xenium transcripts.parquet %s--output-spatialdata proseg-output.zarr", "machine": "%s"}\n' \
        "$version" "${flags:+$flags }" "$(uname -sm)" > "$out/run.json"
    done
  done
done
