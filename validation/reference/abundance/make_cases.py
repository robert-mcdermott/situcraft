"""Writes validation/reference/abundance/cases.json: count tables, their designs' matrices and
contrasts, whose quasi-likelihood models compare.py fits with statsmodels and internal/cohort's test
fits with SituCraft (wave 7, slice 6). Run by the maintainer, once:

  python3 -I make_cases.py <the simulated cohort's summary.json> cases.json

The cases: the standard simulated cohort's eight samples (each label's cells of the labeled cells,
and per mm² of tissue), compared by condition, and by condition adjusted for the slide; four
subjects each sampled before and after (paired: the subjects in the model); three groups of four,
the third against the second; and densities over areas that vary threefold. The drawn counts are
beta-binomial or negative binomial (overdispersed, as samples are), from numpy's PCG64, seed 11.
"""

import json
import sys

import numpy as np


def cohort(path):
    d = json.load(open(path))
    labels = d["labels"]["names"]
    samples = d["samples"]
    treated = [1.0 if k >= 4 else 0.0 for k in range(len(samples))]
    slide2 = [1.0 if k % 2 else 0.0 for k in range(len(samples))]
    out = []
    for name, x in [("simulated cohort by condition", [[1.0, t] for t in treated]),
                    ("simulated cohort by condition and slide", [[1.0, t, s] for t, s in zip(treated, slide2)])]:
        for k, label in enumerate(labels):
            y = [s["counts"][k] for s in samples]
            out.append({"name": f"{name}: share of {label}", "family": "binomial", "x": x, "y": y, "n": [s["labeled"] for s in samples], "contrasts": [[0.0, 1.0] + [0.0] * (len(x[0]) - 2)]})
            out.append({"name": f"{name}: density of {label}", "family": "poisson", "x": x, "y": y, "area": [s["tissueAreaUm2"] / 1e6 for s in samples], "contrasts": [[0.0, 1.0] + [0.0] * (len(x[0]) - 2)]})
    return out


def drawn():
    rng = np.random.Generator(np.random.PCG64(11))
    out = []
    # Paired: four subjects before and after; the subjects in the model.
    x = []
    for s in range(4):
        for after in (0, 1):
            x.append([1.0, float(after)] + [1.0 if s == j else 0.0 for j in range(1, 4)])
    n = rng.integers(400, 900, size=8)
    p = rng.beta(30 * np.where(np.array([r[1] for r in x]) > 0, 0.3, 0.2), 30 * (1 - np.where(np.array([r[1] for r in x]) > 0, 0.3, 0.2)))
    out.append({"name": "paired subjects", "family": "binomial", "x": x, "y": rng.binomial(n, p).tolist(), "n": n.tolist(), "contrasts": [[0.0, 1.0, 0.0, 0.0, 0.0]]})
    # Three groups of four: B and C against A, and C against B.
    x = [[1.0, float(g == 1), float(g == 2)] for g in (0, 1, 2) for _ in range(4)]
    n = rng.integers(500, 1500, size=12)
    share = np.array([0.1, 0.13, 0.08])[[g for g in (0, 1, 2) for _ in range(4)]]
    y = rng.binomial(n, rng.beta(50 * share, 50 * (1 - share)))
    out.append({"name": "three groups", "family": "binomial", "x": x, "y": y.tolist(), "n": n.tolist(), "contrasts": [[0.0, 1.0, 0.0], [0.0, 0.0, 1.0], [0.0, -1.0, 1.0]]})
    # Densities over areas that vary threefold; negative binomial counts.
    x = [[1.0, float(k >= 5)] for k in range(10)]
    area = rng.uniform(0.05, 0.15, size=10)
    mean = area * np.where(np.arange(10) >= 5, 900.0, 600.0)
    y = rng.negative_binomial(8, 8 / (8 + mean))
    out.append({"name": "densities over areas", "family": "poisson", "x": x, "y": y.tolist(), "area": area.tolist(), "contrasts": [[0.0, 1.0]]})
    return out


if __name__ == "__main__":
    cases = cohort(sys.argv[1]) + drawn()
    with open(sys.argv[2], "w") as fh:
        json.dump(cases, fh, indent=None, separators=(",", ":"))
        fh.write("\n")
