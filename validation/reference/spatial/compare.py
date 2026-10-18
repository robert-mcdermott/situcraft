"""SituCraft's neighborhood enrichment and co-occurrence against Squidpy's (wave 6, slice 2;
research.md §6.2), and its nearest distances and Ripley's K against SciPy's k-d tree and astropy's
estimator (slice 3; §6.3).

Reads <data>/spatial/<set>/ as SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceSpatial
wrote it: the cells analyzed (their centroids and labels, SituCraft's clusters), the Delaunay and
20 µm graphs' edges, and SituCraft's results. Runs Squidpy's nhood_enrichment on the same graphs
(their adjacency given to it, every cell labeled, 1,000 permutations) and co_occurrence at the same
distances, and writes how they agree to validation/reference/spatial/<set>.json, which the Go test
checks weekly.

The counts are deterministic and must be identical. The z-scores rest on each tool's own random
permutations: each is an estimate, and two independent estimates of z from n permutations differ
by about sqrt(2·(1 + z²/2)/n) (the error of a mean and a standard deviation from n draws); a pair
is beyond the error when they differ by more than five of these. Co-occurrence is deterministic;
Squidpy writes 0 where SituCraft writes null (a label without pairs at that distance), and those
are left out. Squidpy's co_occurrence computes in float32 (its coordinates, distances and
thresholds), SituCraft in float64 from float32 coordinates, so pairs within float32's rounding of a
distance may be counted at one and not the other: the pairs so placed are counted, and Squidpy's
own routine is run again on float64 coordinates, which must agree exactly.

Nearest distances: each label's cells' distance to the nearest cell of each label (another of its
own), at most the reach, by a k-d tree: the medians and means must agree (SituCraft keeps distances
in float32). Ripley's K of each label alone in the rectangle of the middle half of the cells' extent:
astropy's RipleysKEstimator without a correction ("none") must agree exactly; its isotropic
correction ("ripley") weighs each pair by the circle about its first point only, counted both ways,
where SituCraft weighs each direction by its own cell's circle, so the two differ a little (most for
labels of few cells). The same estimator as SituCraft's is computed here too, every pair in both
directions with Ripley's closed form for a rectangle (Ripley 1977), and must agree exactly.

Usage: compare.py <data> <set> <out.json>
"""

import csv
import json
import math
import sys
from importlib.metadata import version
from pathlib import Path

import anndata as ad
import numpy as np
import pandas as pd
import scipy.sparse as sp
import squidpy as sq


def main():
    data, set_name, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
    folder = data / "spatial" / set_name
    ours = json.loads((folder / "situcraft.json").read_text())
    with open(folder / "cells.csv", newline="") as f:
        rows = list(csv.DictReader(f))
    n, K = len(rows), ours["labels"]
    xy = np.array([[float(np.float32(float(r["x"]))), float(np.float32(float(r["y"])))] for r in rows])
    label = np.array([int(r["label"]) for r in rows])
    adata = ad.AnnData(np.zeros((n, 1), dtype=np.float32))
    adata.obsm["spatial"] = xy
    # Categories in SituCraft's order of labels, so matrices line up.
    adata.obs["label"] = pd.Categorical([str(v) for v in label], categories=[str(k) for k in range(K)])
    perms = ours["permutations"]
    result = {"situcraft": {"cells": n, "labels": K, "digest": ours["digest"]},
              "versions": {name: version(name) for name in ("squidpy", "anndata", "numpy", "scipy")},
              "enrichment": {}}

    for kind in ("delaunay", "radius"):
        with open(folder / f"{kind}.csv", newline="") as f:
            edges = [(int(r["a"]), int(r["b"])) for r in csv.DictReader(f)]
        a = np.array([e[0] for e in edges] + [e[1] for e in edges])
        b = np.array([e[1] for e in edges] + [e[0] for e in edges])
        adata.obsp["spatial_connectivities"] = sp.csr_matrix((np.ones(len(a)), (a, b)), shape=(n, n))
        adata.obsp["spatial_distances"] = adata.obsp["spatial_connectivities"].copy()
        z, count = sq.gr.nhood_enrichment(adata, cluster_key="label", connectivity_key="spatial", n_perms=perms, seed=0, copy=True, show_progress_bar=False)
        mine = ours["enrichment"][kind]
        c_ours = np.array(mine["count"], dtype=float).reshape(K, K)
        z_ours = np.array([np.nan if v is None else v for v in mine["z"]], dtype=float).reshape(K, K)
        differing = int(np.sum(c_ours != np.asarray(count, dtype=float)))
        z_sq = np.asarray(z, dtype=float)
        both = np.isfinite(z_ours) & np.isfinite(z_sq)
        beyond, largest = 0, 0.0
        for i, j in zip(*np.nonzero(both)):
            zo, zs = z_ours[i, j], z_sq[i, j]
            err = math.sqrt(2 * (1 + ((abs(zo) + abs(zs)) / 2) ** 2 / 2) / perms)
            largest = max(largest, abs(zo - zs))
            if abs(zo - zs) > 5 * err:
                beyond += 1
        corr = float(np.corrcoef(z_ours[both], z_sq[both])[0, 1])
        result["enrichment"][kind] = {"edges": len(edges), "pairs": int(K * K), "countsDiffering": differing, "zCompared": int(both.sum()), "zBeyond": beyond, "zCorrelation": corr, "zMaxDifference": largest}
        print(f"{set_name} {kind}: {len(edges)} edges; counts differing {differing} of {K * K}; z compared {int(both.sum())}, beyond the permutations' error {beyond}, correlation {corr:.5f}, largest difference {largest:.3f}")

    co = ours["cooccurrence"]
    distances = co["distances"]
    interval = np.array([0.0] + distances)
    ratios = [np.array([np.nan if v is None else v for v in r], dtype=float).reshape(K, K) for r in co["ratio"]]

    def difference(occ):
        compared, largest = 0, 0.0
        for d, r in enumerate(ratios):
            # Squidpy's occ[i, c] is P(i | c) / P(i): SituCraft's ratio[c][i].
            for c in range(K):
                for i in range(K):
                    if np.isfinite(r[c, i]):
                        compared += 1
                        largest = max(largest, abs(r[c, i] - occ[i, c, d]))
        return compared, largest

    occ32, _ = sq.gr.co_occurrence(adata, cluster_key="label", interval=interval, copy=True, show_progress_bar=False)
    compared, largest32 = difference(occ32)
    # Squidpy's routine on float64 coordinates and thresholds.
    from squidpy.gr._ppatterns import _co_occurrence_helper
    occ64 = _co_occurrence_helper(xy[:, 0].astype(np.float64), xy[:, 1].astype(np.float64), interval.astype(np.float64), label.astype(np.int32))
    _, largest64 = difference(occ64)
    # The pairs float32's rounding places on the other side of a distance.
    from scipy.spatial import cKDTree
    pairs = cKDTree(xy).query_pairs(distances[-1] * 1.001, output_type="ndarray")
    dx64, dy64 = xy[pairs[:, 0], 0] - xy[pairs[:, 1], 0], xy[pairs[:, 0], 1] - xy[pairs[:, 1], 1]
    d64 = np.sqrt(dx64 * dx64 + dy64 * dy64)
    x32, y32 = xy.astype(np.float32)[:, 0], xy.astype(np.float32)[:, 1]
    dx32, dy32 = x32[pairs[:, 0]] - x32[pairs[:, 1]], y32[pairs[:, 0]] - y32[pairs[:, 1]]
    d32sq = dx32 * dx32 + dy32 * dy32
    flipped = sum(int(np.sum((d64 <= t) != (d32sq <= np.float32(t) ** 2))) for t in distances)
    result["cooccurrence"] = {"distances": len(distances), "compared": compared, "maxDifference": largest64, "maxDifferenceFloat32": largest32, "pairsPlacedOtherwiseInFloat32": flipped}
    print(f"{set_name} co-occurrence: {len(distances)} distances, {compared} ratios compared; Squidpy's routine in float64: largest difference {largest64:.3g}; its float32 API: {largest32:.3g}, {flipped} pairs placed otherwise by float32's rounding")
    # Nearest distances by a k-d tree.
    from scipy.spatial import cKDTree
    nearest = ours["nearest"]
    reach = nearest["reach"]
    worst = 0.0
    for b in range(K):
        idx = np.nonzero(label == b)[0]
        if len(idx) == 0:
            continue
        tree = cKDTree(xy[idx])
        d, j = tree.query(xy, k=min(2, len(idx)))
        if d.ndim == 1:
            d, j = d[:, None], j[:, None]
        for a in range(K):
            cells = np.nonzero(label == a)[0]
            vals = []
            for c in cells:
                # The nearest that is not the cell itself.
                k = 1 if idx[j[c, 0]] == c and d.shape[1] > 1 else 0
                v = d[c, k] if not (idx[j[c, 0]] == c and d.shape[1] == 1) else np.inf
                vals.append(min(float(np.float32(v)), reach))
            q = a * K + b
            worst = max(worst, abs(np.median(vals) - nearest["median"][q]), abs(np.mean(vals) - nearest["mean"][q]))
    result["nearest"] = {"pairs": int(K * K), "maxDifference": worst}
    print(f"{set_name} nearest distances: {K * K} pairs, largest difference from SciPy's {worst:.2g} µm")

    # Ripley's K of each label alone in the rectangle, against astropy.
    from astropy.stats import RipleysKEstimator
    rp = ours["ripley"]
    x0, y0, x1, y1 = rp["rectangle"]
    inside = (xy[:, 0] > x0) & (xy[:, 0] < x1) & (xy[:, 1] > y0) & (xy[:, 1] < y1)
    est = RipleysKEstimator(area=(x1 - x0) * (y1 - y0), x_min=x0, y_min=y0, x_max=x1, y_max=y1)
    radii = np.array(rp["radii"])
    def isotropic(pts):
        """K of points in the rectangle, each ordered pair weighted by its first point's circle."""
        n = len(pts)
        h = np.minimum(pts[:, 0] - x0, x1 - pts[:, 0])
        v = np.minimum(pts[:, 1] - y0, y1 - pts[:, 1])
        d = np.hypot(pts[:, None, 0] - pts[None, :, 0], pts[:, None, 1] - pts[None, :, 1])
        np.fill_diagonal(d, np.inf)
        hh, vv = h[:, None] * np.ones((1, n)), v[:, None] * np.ones((1, n))
        with np.errstate(invalid="ignore", divide="ignore"):
            near = d <= np.hypot(hh, vv)
            w1 = 1 - (np.arccos(np.minimum(hh, d) / d) + np.arccos(np.minimum(vv, d) / d)) / np.pi
            w2 = 0.75 - (np.arccos(np.minimum(hh / d, 1)) + np.arccos(np.minimum(vv / d, 1))) / (2 * np.pi)
            share = np.where(near, w1, w2)
        area = (x1 - x0) * (y1 - y0)
        return np.array([area * np.sum((d <= r) / share) / (n * (n - 1)) for r in radii])

    rel_none, rel_iso, rel_exact = [], [], []
    for a in range(K):
        pts = xy[inside & (label == a)]
        if len(pts) < 10:
            continue
        none = est(pts, radii, mode="none")
        iso = est(pts, radii, mode="ripley")
        exact = isotropic(pts)
        for k in range(len(radii)):
            if none[k] > 0:
                rel_none.append(abs(rp["none"][a][k] / none[k] - 1))
            if iso[k] > 0:
                rel_iso.append(abs(rp["isotropic"][a][k] / iso[k] - 1))
            if exact[k] > 0:
                rel_exact.append(abs(rp["isotropic"][a][k] / exact[k] - 1))
    result["ripley"] = {"labels": int(sum(1 for a in range(K) if np.sum(inside & (label == a)) >= 10)), "maxRelativeNone": max(rel_none, default=0.0), "maxRelativeExact": max(rel_exact, default=0.0), "maxRelativeIsotropic": max(rel_iso, default=0.0), "medianRelativeIsotropic": float(np.median(rel_iso)) if rel_iso else 0.0}
    result["versions"]["astropy"] = version("astropy")
    print(f"{set_name} Ripley's K: {result['ripley']['labels']} labels; none within {result['ripley']['maxRelativeNone']:.2g} of astropy's; isotropic within {result['ripley']['maxRelativeExact']:.2g} of the same estimator by Ripley's formula, and within {result['ripley']['maxRelativeIsotropic']:.3f} of astropy's (median {result['ripley']['medianRelativeIsotropic']:.4f})")
    out.write_text(json.dumps(result, indent=1) + "\n")


if __name__ == "__main__":
    main()
