"""SituCraft's labels from a reference against SingleR's (wave 4, slice 6; research.md §5.6), on the
reference data sets: the instrument's segmentation of each, clustered and mapped by SituCraft with
its defaults (validation/references_test.go writes them into <data>/clustering/<set>/reference/),
and the same cells labeled by SingleR (singler.R): given the same profiles, each cell's first-round
correlation with every label is compared with SituCraft's, and its label; at SingleR's defaults on
the reference's own samples or cells, its label. On the simulated tissue, each one's labels are
compared with the cells' true types (that of the true cell holding most of each cell's
transcripts). Writes validation/reference/references/<set>.json, whose numbers the Go test checks.

Usage (by compare.sh, with the pinned versions): python -I compare.py <data folder> <set> <out.json>
"""

import json
import os
import struct
import sys
import zlib
from importlib import metadata

import numpy as np
import pandas as pd
import pyarrow.parquet as pq


class Body:
    """A SituCraft binary file (docs/FORMATS.md): magic, version, length, zlib body."""

    def __init__(self, path, magic):
        raw = open(path, "rb").read()
        if raw[:4] != magic.encode():
            raise ValueError(f"{path} is not a SituCraft {magic} file")
        _, length = struct.unpack("<IQ", raw[4:16])
        self.b = zlib.decompress(raw[16:])
        if len(self.b) != length:
            raise ValueError(f"{path}: {len(self.b)} bytes, its header says {length}")
        self.at = 0

    def u32(self):
        v = struct.unpack_from("<I", self.b, self.at)[0]
        self.at += 4
        return v

    def array(self, dtype, n):
        a = np.frombuffer(self.b, dtype=dtype, count=n, offset=self.at)
        self.at += a.nbytes
        return a


def read_reference_labels(folder):
    """labels.json and labels.bin of a label set from a reference (docs/FORMATS.md, Labels)."""
    d = json.load(open(os.path.join(folder, "labels.json")))
    b = Body(os.path.join(folder, "labels.bin"), "SCLB")
    n = b.u32()
    cells = b.array("<u4", n)
    b.array("<u4", n)  # clusters
    S, levels, states = b.u32(), b.u32(), b.u32()
    if levels != 1 or states != 0:
        raise ValueError(f"{folder}: {levels} levels and {states} states; a reference's labels have one and none")
    first = np.stack([b.array("<f4", n) for _ in range(S)], axis=1)
    last = np.stack([b.array("<f4", n) for _ in range(S)], axis=1)
    label = b.array("<u4", n).astype(np.int64)
    label[label == 0xFFFFFFFF] = -1
    return {"description": d, "cells": cells, "first": first, "last": last, "label": label}


def true_types(bundle):
    """Each instrument cell's true type in a simulated bundle: that of the true cell holding most
    of its transcripts."""
    truth = os.path.join(bundle, "truth")
    if not os.path.isdir(truth):
        return None
    seen = pq.read_table(os.path.join(bundle, "transcripts.parquet"), columns=["transcript_id", "cell_id"]).to_pandas()
    true = pq.read_table(os.path.join(truth, "transcripts.parquet"), columns=["transcript_id", "cell_id"]).to_pandas()
    both = seen.merge(true, on="transcript_id", suffixes=("", "_true"))
    both = both[(both.cell_id != "UNASSIGNED") & (both.cell_id_true != "UNASSIGNED")]
    major = both.groupby(["cell_id", "cell_id_true"]).size().reset_index(name="n").sort_values(["cell_id", "n", "cell_id_true"], ascending=[True, False, True]).drop_duplicates("cell_id")
    types = pq.read_table(os.path.join(truth, "cells.parquet"), columns=["cell_id", "cell_type"]).to_pandas().set_index("cell_id")["cell_type"]
    return major.set_index("cell_id")["cell_id_true"].map(types)


def main(data, name, out_path):
    folder = os.path.join(data, "clustering", name)
    info = json.load(open(os.path.join(folder, "reference.json")))
    s = read_reference_labels(os.path.join(folder, "reference"))
    d = s["description"]
    ref = d["reference"]
    names = [x["name"] for x in d["sets"]]
    n = len(s["cells"])
    situ = np.array([names[k] if k >= 0 else None for k in s["label"]], dtype=object)
    situ_first = np.array([names[k] for k in np.nanargmax(np.where(np.isnan(s["first"]), -np.inf, s["first"]), axis=1)], dtype=object)

    query = os.path.join(data, "references", name)
    cells = [int(x) for x in open(os.path.join(query, "cells.txt")).read().split()]
    if cells != [int(c) for c in s["cells"]]:
        raise ValueError(f"{name}: SingleR's cells are not SituCraft's")
    agg = pd.read_csv(os.path.join(query, "singler-aggregated.csv"))
    default = pd.read_csv(os.path.join(query, "singler-default.csv"))
    tools = json.load(open(os.path.join(query, "singler.json")))
    tools.update({k: metadata.version(k) for k in ["numpy", "pandas", "pyarrow"]})

    # The first round's correlations: SingleR's columns are the labels, by name.
    sr_scores = agg[names].to_numpy(dtype=np.float64)
    diff = np.abs(sr_scores - s["first"].astype(np.float64))
    both = ~np.isnan(diff)
    sr_label = agg["label"].astype(object).to_numpy()
    sr_first = np.array([names[k] for k in np.argmax(sr_scores, axis=1)], dtype=object)
    agree = situ == sr_label
    result = {
        "dataSet": name,
        "tools": tools,
        "situcraft": {"version": d["method"], "digest": info["digest"], "reference": ref["file"], "sha256": ref["sha256"], "cells": n, "labeled": int(np.sum(s["label"] >= 0)), "shared": len(ref["shared"]), "compared": len(ref["compared"]),
                      "fineTuned": ref["fineTuned"], "changed": ref["changed"], "parameters": ref["parameters"]},
        "singler": {
            "aggregated": {
                "scoreMaxAbsDiff": float(np.max(diff[both])) if both.any() else None,
                "scoresWithout": int(np.sum(~both)),
                "agreement": float(np.mean(agree)),
                "firstAgreement": float(np.mean(situ_first == sr_first)),
                "disagreements": [{"situcraft": a, "singler": b, "cells": int(c)} for (a, b), c in pd.Series(list(zip(situ[~agree], sr_label[~agree]))).value_counts().head(8).items()],
            },
            "default": {
                "agreement": float(np.mean(situ == default["label"].astype(object).to_numpy())),
                "pruned": int(default["pruned"].isna().sum()),
                "disagreements": [{"situcraft": a, "singler": b, "cells": int(c)} for (a, b), c in pd.Series(list(zip(situ[situ != default["label"].to_numpy()], default["label"].to_numpy()[situ != default["label"].to_numpy()]))).value_counts().head(8).items()],
            },
        },
        "labels": [{"label": x, "situcraft": int(np.sum(situ == x)), "singlerAggregated": int(np.sum(sr_label == x)), "singlerDefault": int(np.sum(default["label"].to_numpy() == x))} for x in names],
        "truth": None,
    }
    t = true_types(info["bundle"])
    if t is not None:
        ids = pq.read_table(os.path.join(info["bundle"], "cells.parquet"), columns=["cell_id"]).column(0).to_pylist()
        truth = pd.Series([ids[c] for c in s["cells"]]).map(t).to_numpy()
        known = pd.notna(truth)
        right = lambda lab: float(np.mean(lab[known] == truth[known]))
        result["truth"] = {"cells": int(known.sum()), "situcraft": right(situ), "singlerAggregated": right(sr_label), "singlerDefault": right(default["label"].astype(object).to_numpy())}
    json.dump(result, open(out_path, "w"), indent=1)
    open(out_path, "a").write("\n")
    print(json.dumps({k: result[k] for k in ["situcraft", "singler", "truth"]}, indent=1))


if __name__ == "__main__":
    main(*sys.argv[1:4])
