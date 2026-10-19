"""Writes validation/reference/limma/cases.json: pseudobulk count tables, their designs' matrices and
contrasts, which compare.R tests with limma and edgeR and internal/cohort's test with SituCraft (wave
7, slice 7). Run by the maintainer, once:

  python3 -I make_cases.py <the simulated cohort's summary folder> cases.json

The cases: the standard simulated cohort's eight samples, each label's pseudobulk compared by
condition, and the first label's by condition adjusted for the slide; four subjects each sampled before and after
(paired); three groups of four, each against the first and the third against the second; libraries
that differ tenfold, with a few genes that make up much of some (where normalization matters); and
40 genes (a trend as wide as the genes). Drawn counts are negative binomial, from numpy's PCG64,
seed 7. Plain values (no counts): the cohort's samples' spatial statistics by family (log2 neighbor
enrichments and nearest distances, Moran's I, niches' shares), by condition and by condition and
slide; one statistic alone, and three. And curves for lowess alone: sorted points with ties, spans
and skips.
"""

import csv
import json
import math
import os
import sys

import numpy as np


def cohort(path):
    rows = list(csv.DictReader(open(path)))
    genes = [k for k in rows[0] if k not in ("sample", "name", "label", "cells")]
    labels = list(dict.fromkeys(r["label"] for r in rows))
    out = []
    for label in labels:
        rs = sorted((r for r in rows if r["label"] == label), key=lambda r: r["name"])
        treated = [1.0 if k >= 4 else 0.0 for k in range(len(rs))]
        slide2 = [1.0 if k % 2 else 0.0 for k in range(len(rs))]
        counts = [[float(r[g]) for r in rs] for g in genes]
        designs = [("by condition", [[1.0, t] for t in treated])]
        if label == labels[0]:
            designs.append(("by condition and slide", [[1.0, t, s] for t, s in zip(treated, slide2)]))
        for name, x in designs:
            out.append({"name": f"simulated cohort {name}: {label}", "genes": genes, "counts": counts, "x": x, "contrasts": [[0.0, 1.0] + [0.0] * (len(x[0]) - 2)]})
    return out


def plain(path):
    d = json.load(open(path))
    samples = d["samples"]
    treated = [1.0 if k >= 4 else 0.0 for k in range(len(samples))]
    slide2 = [1.0 if k % 2 else 0.0 for k in range(len(samples))]
    out = []
    for family, log in [("enrichment", True), ("nearest", True), ("moran", False), ("niche", False)]:
        rows = []
        for s in samples:
            if family == "niche":
                rows.append({f"niche {v + 1}": x for v, x in enumerate(s.get("nicheShares", []))})
            else:
                rows.append({st["key"]: (math.log2(st["value"]) if log else st["value"]) for st in s["statistics"] if st["family"] == family and (not log or st["value"] > 0)})
        keys = sorted(set.intersection(*(set(r) for r in rows)))
        y = [[r[k] for r in rows] for k in keys]
        for name, x in [("by condition", [[1.0, t] for t in treated]),
                        ("by condition and slide", [[1.0, t, s] for t, s in zip(treated, slide2)])]:
            out.append({"name": f"simulated cohort's {family} {name}", "keys": keys, "y": y, "x": x, "contrasts": [[0.0, 1.0] + [0.0] * (len(x[0]) - 2)]})
    rng = np.random.Generator(np.random.PCG64(7))
    x = [[1.0, float(g == 1), float(g == 2)] for g in (0, 1, 2) for _ in range(3)]
    y = rng.normal(0, 1, size=(3, 9)) + np.array([[0, 1.5, 0]])[:, [g for g in (0, 1, 2) for _ in range(3)]]
    out.append({"name": "three statistics, three groups", "keys": ["a", "b", "c"], "y": y.tolist(), "x": x, "contrasts": [[0.0, 1.0, 0.0], [0.0, -1.0, 1.0]]})
    out.append({"name": "one statistic", "keys": ["a"], "y": y[:1].tolist(), "x": x, "contrasts": [[0.0, 1.0, 0.0]]})
    return out


def nb(rng, mean, size):
    return rng.negative_binomial(size, size / (size + mean)).astype(float)


def drawn():
    rng = np.random.Generator(np.random.PCG64(7))
    out = []
    # Paired: four subjects before and after; 300 genes, the first 20 doubled after.
    x = []
    for s in range(4):
        for after in (0, 1):
            x.append([1.0, float(after)] + [1.0 if s == j else 0.0 for j in range(1, 4)])
    base = rng.gamma(0.8, 60, size=300)
    subject = rng.lognormal(0, 0.3, size=(300, 4))
    fold = np.where(np.arange(300) < 20, 2.0, 1.0)
    lib = rng.uniform(0.6, 1.6, size=8)
    mean = np.array([[base[g] * subject[g, k // 2] * (fold[g] if k % 2 else 1) * lib[k] for k in range(8)] for g in range(300)])
    out.append({"name": "paired subjects", "counts": nb(rng, mean, 12).tolist(), "x": x, "contrasts": [[0.0, 1.0, 0.0, 0.0, 0.0]]})
    # Three groups of four: B and C against A, and C against B; 400 genes.
    g3 = [g for g in (0, 1, 2) for _ in range(4)]
    x = [[1.0, float(g == 1), float(g == 2)] for g in g3]
    base = rng.gamma(0.6, 80, size=400)
    folds = np.ones((400, 3))
    folds[:25, 1] = 3.0
    folds[25:50, 2] = 0.4
    lib = rng.uniform(0.5, 2.0, size=12)
    mean = np.array([[base[g] * folds[g, g3[k]] * lib[k] for k in range(12)] for g in range(400)])
    out.append({"name": "three groups", "counts": nb(rng, mean, 6).tolist(), "x": x, "contrasts": [[0.0, 1.0, 0.0], [0.0, 0.0, 1.0], [0.0, -1.0, 1.0]]})
    # Libraries that differ tenfold, five genes that make up much of the treated libraries.
    x = [[1.0, float(k >= 5)] for k in range(10)]
    base = rng.gamma(0.7, 40, size=400)
    lib = np.exp(rng.uniform(np.log(0.3), np.log(3.0), size=10))
    mean = np.array([[base[g] * lib[k] * (40.0 if g < 5 and k >= 5 else 1.0) for k in range(10)] for g in range(400)])
    out.append({"name": "composition", "counts": nb(rng, mean, 10).tolist(), "x": x, "contrasts": [[0.0, 1.0]]})
    # 40 genes: a span of 1.
    x = [[1.0, float(k >= 3)] for k in range(6)]
    base = rng.gamma(2, 50, size=40)
    mean = np.array([[base[g] * (1.8 if g < 4 and k >= 3 else 1.0) for k in range(6)] for g in range(40)])
    out.append({"name": "forty genes", "counts": nb(rng, mean, 20).tolist(), "x": x, "contrasts": [[0.0, 1.0]]})
    return out


def curves():
    rng = np.random.Generator(np.random.PCG64(7))
    out = []
    x = np.sort(np.round(rng.uniform(0, 10, size=200), 1))
    y = np.sin(x) + rng.normal(0, 0.3, size=200)
    y[::37] += 4
    for f, delta in [(2 / 3, 0.0), (0.3, 0.01 * 10), (0.1, 0.0), (1.0, 0.5)]:
        out.append({"name": f"lowess f {f:.3g} delta {delta:.3g}", "x": x.tolist(), "y": y.tolist(), "f": f, "iter": 3, "delta": delta})
    x = np.sort(rng.uniform(-2, 2, size=1000))
    y = np.abs(x) + rng.standard_t(3, size=1000) * 0.2
    out.append({"name": "lowess of 1000 points", "x": x.tolist(), "y": y.tolist(), "f": 0.4, "iter": 3, "delta": 0.01 * (x[-1] - x[0])})
    out.append({"name": "lowess without robustness", "x": x.tolist(), "y": y.tolist(), "f": 0.4, "iter": 0, "delta": 0.0})
    return out


if __name__ == "__main__":
    folder = sys.argv[1]
    cases = {"tests": cohort(os.path.join(folder, "pseudobulk.csv")) + drawn(), "plain": plain(os.path.join(folder, "summary.json")), "lowess": curves()}
    with open(sys.argv[2], "w") as fh:
        json.dump(cases, fh, indent=None, separators=(",", ":"))
        fh.write("\n")
