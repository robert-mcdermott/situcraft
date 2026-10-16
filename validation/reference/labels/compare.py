"""SituCraft's labels from markers against scanpy's score_genes (wave 4, slice 3; research.md §5.3),
on the reference data sets: the instrument's segmentation of each, clustered and labeled by
SituCraft with its defaults (validation/labels_test.go writes them into <data>/clustering/<set>/
labels/).

The bundle is read here by scanpy from its own cell_feature_matrix.h5, independently of SituCraft:
the cells SituCraft labeled, normalize_total to their median and log1p. For each label's (and
state's) positive and negative markers, scanpy's score_genes scores the cells with the same gene
pool (every gene, less the label's lineage's markers when SituCraft left them out), its 25 bins
and 50 control genes per bin; its control genes (scanpy's _score_genes_bins) are compared with
SituCraft's, and each cell's score with SituCraft's. On the simulated tissue, each cell's label is
compared with its true type (that of the true cell holding most of its transcripts). Writes
validation/reference/labels/<set>.json, whose numbers the Go test checks.

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
import scanpy as sc
import scipy.sparse as sp
from scanpy.tools._score_genes import _check_score_genes_args, _score_genes_bins


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


def read_labels(folder):
    """labels.json and labels.bin (docs/FORMATS.md, Labels)."""
    d = json.load(open(os.path.join(folder, "labels.json")))
    b = Body(os.path.join(folder, "labels.bin"), "SCLB")
    n = b.u32()
    cells = b.array("<u4", n)
    cluster = b.array("<u4", n)
    S, levels, states = b.u32(), b.u32(), b.u32()
    score = [b.array("<f4", n) for _ in range(S)]
    positive = [b.array("<f4", n) for _ in range(S)]
    negative = [b.array("<f4", n) if d["sets"][k].get("negative") else None for k in range(S)]
    label = [b.array("<u4", n).astype(np.int64) for _ in range(levels)]
    for lab in label:
        lab[lab == 0xFFFFFFFF] = -1
    return {"description": d, "cells": cells, "cluster": cluster, "score": score, "positive": positive, "negative": negative, "label": label}


def lineage_markers(sets, k):
    """The positive markers of label k's ancestors and descendants."""
    def ancestors(j):
        out = []
        while sets[j]["parent"] >= 0:
            j = sets[j]["parent"]
            out.append(j)
        return out
    related = set(ancestors(k)) | {j for j in range(len(sets)) if not sets[j].get("state") and k in ancestors(j)}
    return {g for j in related for g in sets[j]["positive"]}


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


def versions():
    return {name: metadata.version(name) for name in ["scanpy", "anndata", "numpy", "scipy", "pandas", "pyarrow"]}


def main(data, name, out_path):
    folder = os.path.join(data, "clustering", name)
    info = json.load(open(os.path.join(folder, "labels.json")))
    bundle = info["bundle"]
    s = read_labels(os.path.join(folder, "labels"))
    d = s["description"]
    p = d["parameters"]
    sets = d["sets"]
    ids = pq.read_table(os.path.join(bundle, "cells.parquet"), columns=["cell_id"]).column(0).to_pylist()
    analyzed = [ids[i] for i in s["cells"]]
    result = {"dataSet": name, "tools": versions()}
    result["situcraft"] = {"version": d["method"], "digest": info["digest"], "markers": d["markers"]["name"], "checksum": d["markers"]["checksum"], "cells": len(analyzed), "labeled": d["cells"]["labeled"]}

    adata = sc.read_10x_h5(os.path.join(bundle, "cell_feature_matrix.h5"))
    adata.var_names_make_unique()
    adata = adata[analyzed].copy()
    adata.X = sp.csr_matrix(adata.X, dtype=np.float32)
    sc.pp.normalize_total(adata, target_sum=None)
    sc.pp.log1p(adata)
    genes = adata.var_names

    lists, same, max_abs, max_diff, combined = 0, 0, 0.0, 0.0, 0.0
    detail = []
    for k, st in enumerate(sets):
        for part in ("positive", "negative"):
            gene_list = st.get(part) or []
            if not gene_list:
                continue
            pool = set(genes)
            if p.get("lineage") and not st.get("state"):
                pool -= lineage_markers(sets, k) - set(gene_list)
            pool = [g for g in genes if g in pool]
            gl, gp, get_subset = _check_score_genes_args(adata, gene_list, pool, use_raw=False, layer=None)
            controls = pd.Index([], dtype="string")
            for r in _score_genes_bins(gl, gp, ctrl_as_ref=True, ctrl_size=p["controls"], n_bins=p["bins"], get_subset=get_subset):
                controls = controls.union(r)
            ours = st["positiveControls" if part == "positive" else "negativeControls"]
            lists += 1
            same += set(controls) == set(ours)
            sc.tl.score_genes(adata, gene_list, ctrl_size=p["controls"], n_bins=p["bins"], gene_pool=pool, random_state=0, score_name="s", use_raw=False)
            theirs = adata.obs["s"].to_numpy()
            mine = (s["positive"] if part == "positive" else s["negative"])[k].astype(np.float64)
            diff = float(np.max(np.abs(mine - theirs)))
            max_diff = max(max_diff, diff)
            max_abs = max(max_abs, float(np.max(np.abs(theirs))))
            detail.append({"label": st["name"], "part": part, "genes": len(gene_list), "controls": len(ours), "controlsSame": set(controls) == set(ours), "maxAbsDiff": diff})
        # The label's score from its parts.
        pos = s["positive"][k].astype(np.float64)
        neg = s["negative"][k]
        want = pos if neg is None else pos - np.maximum(neg.astype(np.float64), 0)
        combined = max(combined, float(np.max(np.abs(s["score"][k].astype(np.float64) - want))))
    result["scores"] = {"lists": lists, "controlsSame": same, "maxAbs": max_abs, "scanpyMaxAbsDiff": max_diff, "combinedMaxAbs": combined, "byList": detail}

    # Each level's labels, by cell.
    deepest = np.full(len(analyzed), -1)
    for lab in s["label"]:
        deepest = np.where(lab >= 0, lab, deepest)
    names = np.array([st["name"] for st in sets] + ["(none)"])
    labels = names[np.where(deepest >= 0, deepest, len(sets))]
    result["labels"] = {str(k): int(v) for k, v in zip(*np.unique(labels, return_counts=True))}

    t = true_types(bundle)
    result["truth"] = None
    if t is not None:
        truth = pd.Series(analyzed).map(t).to_numpy()
        known = pd.notna(truth)
        right = int(np.sum(labels[known] == truth[known]))
        unlabeled = int(np.sum(labels[known] == "(none)"))
        n = int(known.sum())
        result["truth"] = {"cells": n, "right": right / n, "wrong": (n - right - unlabeled) / n, "unlabeled": unlabeled / n, "types": sorted(set(truth[known]))}

    json.dump(result, open(out_path, "w"), indent=1)
    open(out_path, "a").write("\n")
    print(json.dumps({k: result[k] for k in ["situcraft", "labels", "truth"]}, indent=1))
    print(json.dumps({k: v for k, v in result["scores"].items() if k != "byList"}, indent=1))


if __name__ == "__main__":
    main(*sys.argv[1:4])
