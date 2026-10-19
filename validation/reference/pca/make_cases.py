"""Writes validation/reference/pca/cases.json: the pseudobulk matrices (a row a sample, a column a
gene, transcripts) whose principal components compare.py finds with numpy and
internal/cohort's test finds with SituCraft (wave 7, slice 5). Run by the maintainer, once:

  python3 -I make_cases.py <the simulated cohort's pseudobulk.csv> cases.json

The cases: the standard simulated cohort's eight samples (each one's labeled cells' transcripts
summed over its labels, from a summary's pseudobulk.csv); twelve samples of 800 genes drawn here
(Poisson counts around gene means that two batches shift, numpy's PCG64 from seed 7), more genes
than the 500 most variable kept; and three samples of five genes, small enough to check by hand.
"""

import csv
import json
import sys

import numpy as np


def cohort(path):
    rows = list(csv.reader(open(path)))
    genes = rows[0][4:]
    by = {}
    order = []
    for r in rows[1:]:
        if r[1] not in by:
            by[r[1]] = [0] * len(genes)
            order.append(r[1])
        by[r[1]] = [a + int(b) for a, b in zip(by[r[1]], r[4:])]
    return {"name": "simulated cohort", "samples": order, "genes": genes, "counts": [by[s] for s in order]}


def drawn():
    rng = np.random.Generator(np.random.PCG64(7))
    genes = 800
    means = rng.gamma(0.6, 40.0, size=genes) + 0.5
    shift = np.where(rng.random(genes) < 0.1, rng.normal(0, 1.0, genes), 0.0)
    counts = []
    for s in range(12):
        depth = rng.uniform(0.6, 1.6)
        batch = shift if s % 2 else 0.0
        counts.append(rng.poisson(means * depth * np.exp(batch)).tolist())
    # Two genes no sample expresses, and one every sample expresses alike.
    for c in counts:
        c[10] = 0
        c[11] = 0
        c[12] = 25
    return {"name": "drawn, two batches", "samples": [f"D{s + 1:02d}" for s in range(12)], "genes": [f"G{g + 1:03d}" for g in range(genes)], "counts": counts}


def small():
    return {"name": "three samples", "samples": ["A", "B", "C"], "genes": ["g1", "g2", "g3", "g4", "g5"],
            "counts": [[10, 0, 5, 100, 3], [20, 1, 5, 80, 0], [5, 4, 10, 90, 9]]}


if __name__ == "__main__":
    cases = [cohort(sys.argv[1]), drawn(), small()]
    with open(sys.argv[2], "w") as fh:
        json.dump(cases, fh, separators=(",", ":"))
        fh.write("\n")
