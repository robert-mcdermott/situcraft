"""The principal components of validation/reference/pca/cases.json's pseudobulk, as numpy finds them,
for internal/cohort's test to compare SituCraft's with (wave 7, slice 5). Run by compare.sh.

Each sample's counts become log2(1 + counts per million of its total); the 500 genes of greatest
variance across the samples are kept (as DESeq2's plotPCA keeps its ntop; ties to the first gene),
centered, and decomposed by numpy's SVD. A component's sign makes the sample farthest along it
positive (scikit-learn's svd_flip on U); scores are U times the singular values, and each
component's share of the variance is its squared singular value over their sum.
"""

import json
import sys

import numpy as np

TOP = 500
COMPONENTS = 10


def pca(counts):
    x = np.asarray(counts, dtype=float)
    y = np.log2(1.0 + x / x.sum(axis=1, keepdims=True) * 1e6)
    var = y.var(axis=0)
    keep = sorted(sorted(range(y.shape[1]), key=lambda g: (-var[g], g))[:TOP])
    yc = y[:, keep] - y[:, keep].mean(axis=0)
    u, s, _ = np.linalg.svd(yc, full_matrices=False)
    k = min(y.shape[0] - 1, COMPONENTS)
    u, s = u[:, :k], s[:k]
    for c in range(k):
        if u[np.argmax(np.abs(u[:, c])), c] < 0:
            u[:, c] = -u[:, c]
    total = float((np.linalg.svd(yc, compute_uv=False) ** 2).sum())
    return {"genes": keep, "scores": (u * s).tolist(), "variance": (s ** 2 / total).tolist()}


def main(cases_path, out_path):
    out = [{"name": c["name"], **pca(c["counts"])} for c in json.load(open(cases_path))]
    with open(out_path, "w") as fh:
        json.dump({"numpy": np.__version__, "cases": out}, fh, indent=1)
        fh.write("\n")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
