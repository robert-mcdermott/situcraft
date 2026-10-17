"""SituCraft's cell graphs against SciPy's and Squidpy's (wave 6, slice 1; research.md §6.1).

Reads <data>/graphs/<set>/ as SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceGraphs
wrote it (the instrument's cells' centroids, and SituCraft's Delaunay, k-nearest and radius graphs
with their defaults), builds the same graphs with SciPy (the Delaunay triangulation by Qhull; the k
nearest and the cells within the radius by a k-d tree) and with Squidpy's spatial_neighbors, and
writes how they agree to validation/reference/graphs/<set>.json, which the Go test checks weekly.

Where SituCraft's and SciPy's edges differ, a difference is explained when the triangulation is not
unique there (the two diagonals of four cells on one circle, checked in exact rational arithmetic),
or when a k-nearest neighbor ties with the next (equal distances), or a radius neighbor lies at the
radius itself.

Usage: compare.py <data> <set> <out.json>
"""

import csv
import json
import math
import sys
from fractions import Fraction
from importlib.metadata import version
from pathlib import Path

import numpy as np
from scipy.spatial import Delaunay, cKDTree


def read_cells(folder):
    with open(folder / "cells.csv", newline="") as f:
        rows = list(csv.DictReader(f))
    # The centroids as SituCraft holds them: float32, read back exactly.
    x = np.array([float(r["x"]) for r in rows], dtype=np.float32).astype(np.float64)
    y = np.array([float(r["y"]) for r in rows], dtype=np.float32).astype(np.float64)
    return x, y


def read_edges(folder, kind):
    edges = {}
    with open(folder / f"{kind}.csv", newline="") as f:
        for r in csv.DictReader(f):
            a, b = int(r["a"]), int(r["b"])
            edges[(min(a, b), max(a, b))] = float(np.float32(float(r["length"])))
    return edges


def dist(x, y, a, b):
    dx, dy = x[b] - x[a], y[b] - y[a]
    return math.sqrt(dx * dx + dy * dy)


def incircle_exact(x, y, a, b, c, d):
    """The sign of d against the circle through a, b and c (counter-clockwise or not: zero is zero)."""
    F = Fraction
    rows = []
    for p in (a, b, c):
        px, py = F(x[p]) - F(x[d]), F(y[p]) - F(y[d])
        rows.append((px, py, px * px + py * py))
    (ax, ay, al), (bx, by, bl), (cx, cy, cl) = rows
    return al * (bx * cy - cx * by) - bl * (ax * cy - cx * ay) + cl * (ax * by - bx * ay)


def neighbors_of(edges):
    nb = {}
    for a, b in edges:
        nb.setdefault(a, set()).add(b)
        nb.setdefault(b, set()).add(a)
    return nb


def compare(ours, ref, x, y, explain):
    common = ours.keys() & ref.keys()
    only_ours = ours.keys() - ref.keys()
    only_ref = ref.keys() - ours.keys()
    explained = explain(only_ours, only_ref)
    max_length = max((abs(ours[e] - float(np.float32(ref[e]))) for e in common), default=0.0)
    return {"edges": len(ours), "reference": len(ref), "onlySituCraft": len(only_ours), "onlyReference": len(only_ref), "explained": explained, "maxLength": max_length}


def delaunay_edges(x, y, longest):
    tri = Delaunay(np.column_stack([x, y]))
    edges = {}
    for s in tri.simplices:
        for i in range(3):
            a, b = int(s[i]), int(s[(i + 1) % 3])
            e = (min(a, b), max(a, b))
            d = dist(x, y, *e)
            if longest <= 0 or d <= longest:
                edges[e] = d
    return edges


def explain_delaunay(ours, ref, x, y):
    """Each edge only one side has, explained when it and an edge only the other has are the two
    diagonals of four cells on one circle."""
    def count(only, other_only, graph):
        nb = neighbors_of(graph)
        n = 0
        for a, b in only:
            common = sorted(nb.get(a, set()) & nb.get(b, set()))
            if any((min(c, d), max(c, d)) in other_only and incircle_exact(x, y, a, b, c, d) == 0
                   for i, c in enumerate(common) for d in common[i + 1:]):
                n += 1
        return n
    return lambda only_ours, only_ref: count(only_ours, only_ref, ours) + count(only_ref, only_ours, ref)


def knn_edges(x, y, k):
    xy = np.column_stack([x, y])
    tree = cKDTree(xy)
    d, idx = tree.query(xy, k=k + 1)
    edges = {}
    kth = {}
    for i in range(len(x)):
        chosen = [int(j) for j in idx[i] if int(j) != i][:k]
        kth[i] = max(dist(x, y, i, j) for j in chosen)
        for j in chosen:
            e = (min(i, j), max(i, j))
            edges[e] = dist(x, y, *e)
    return edges, kth


def explain_knn(kth, x, y):
    """A difference is a tie: the edge as long as one of its cells' k-th nearest."""
    def explain(only_ours, only_ref):
        return sum(1 for a, b in only_ours | only_ref if dist(x, y, a, b) in (kth[a], kth[b]))
    return explain


def radius_edges(x, y, r):
    tree = cKDTree(np.column_stack([x, y]))
    return {(int(a), int(b)): dist(x, y, int(a), int(b)) for a, b in tree.query_pairs(r)}


def squidpy_edges(x, y, **kwargs):
    import anndata as ad
    import squidpy as sq

    adata = ad.AnnData(np.zeros((len(x), 1), dtype=np.float32))
    adata.obsm["spatial"] = np.column_stack([x, y])
    sq.gr.spatial_neighbors(adata, coord_type="generic", **kwargs)
    m = adata.obsp["spatial_connectivities"].tocoo()
    return {(min(int(a), int(b)), max(int(a), int(b))) for a, b in zip(m.row, m.col) if a != b}


def main():
    data, set_name, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
    folder = data / "graphs" / set_name
    info = json.loads((folder / "situcraft.json").read_text())
    x, y = read_cells(folder)
    p = info["params"]
    kinds = {}

    ours = read_edges(folder, "delaunay")
    ref = delaunay_edges(x, y, p["delaunay"].get("longest", 0))
    kinds["delaunay"] = compare(ours, ref, x, y, explain_delaunay(ours, ref, x, y))
    full = set(delaunay_edges(x, y, 0))
    sq = squidpy_edges(x, y, delaunay=True)
    kinds["delaunay"].update(squidpy=len(sq), squidpyDiffering=len(sq ^ full))

    k = p["knn"]["k"]
    ours = read_edges(folder, "knn")
    ref, kth = knn_edges(x, y, k)
    kinds["knn"] = compare(ours, ref, x, y, explain_knn(kth, x, y))
    sq = squidpy_edges(x, y, n_neighs=k)
    kinds["knn"].update(squidpy=len(sq), squidpyDiffering=len(sq ^ set(ref)))

    r = p["radius"]["radius"]
    ours = read_edges(folder, "radius")
    ref = radius_edges(x, y, r)
    kinds["radius"] = compare(ours, ref, x, y, lambda a, b: sum(1 for e in a | b if dist(x, y, *e) == r))
    sq = squidpy_edges(x, y, radius=r)
    kinds["radius"].update(squidpy=len(sq), squidpyDiffering=len(sq ^ set(ref)))

    for kind, v in kinds.items():
        v["params"] = p[kind]
        print(f"{set_name} {kind}: {v['edges']} edges, SciPy {v['reference']}; only SituCraft's {v['onlySituCraft']}, only SciPy's {v['onlyReference']} ({v['explained']} explained); lengths within {v['maxLength']:.2g} µm; Squidpy {v['squidpy']}, {v['squidpyDiffering']} differing")
    result = {
        "situcraft": {"cells": info["cells"], "digest": info["digest"]},
        "versions": {name: version(name) for name in ("scipy", "numpy", "squidpy", "anndata")},
        "kinds": kinds,
    }
    out.write_text(json.dumps(result, indent=1) + "\n")


if __name__ == "__main__":
    main()
