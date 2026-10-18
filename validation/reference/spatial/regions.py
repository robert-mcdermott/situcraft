"""SituCraft's regions compared and profile across a region's edge against Shapely's areas and
distances (wave 6, slice 4; research.md §6.4).

Reads <data>/spatial/<set>/ as SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceSpatial
wrote it: the cells analyzed (their centroids and labels) and SituCraft's results, among them two
regions in the rectangle of the middle half of the cells' extent (the tissue window): a heptagon
about its middle and a box over the heptagon's right side reaching past the rectangle. Shapely
intersects them with the rectangle for each region's area and the area in neither, and buffers the
heptagon outward and inward (arcs of 1,024 segments a quarter circle) for the area of the rectangle
between each pair of the profile's edges; each cell's regions by containment, its band by its
distance to the heptagon's outline, negative inside. The areas must agree to within Shapely's arcs
(relative to bands of at least 100 µm²; a band of a few µm² at the heptagon's eroded tip is
compared by its difference), the counts exactly. Adds what it finds to validation/reference/spatial/<set>.json, written by
compare.py first, which the Go test checks weekly.

Usage: regions.py <data> <set> <out.json>
"""

import csv
import json
import sys
from importlib.metadata import version
from pathlib import Path

import numpy as np
import shapely


def main():
    data, set_name, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
    folder = data / "spatial" / set_name
    ours = json.loads((folder / "situcraft.json").read_text())
    with open(folder / "cells.csv", newline="") as f:
        rows = list(csv.DictReader(f))
    # Centroids as SituCraft holds them, in float32.
    xy = np.array([[float(np.float32(float(r["x"]))), float(np.float32(float(r["y"])))] for r in rows])
    label = np.array([int(r["label"]) for r in rows])
    K = ours["labels"]
    result = json.loads(out.read_text())

    rg = ours["regions"]
    rect = shapely.box(*rg["rectangle"])
    polys = [shapely.Polygon(p) for p in rg["polygons"]]
    areas = [rect.intersection(p).area for p in polys] + [rect.difference(shapely.union_all(polys)).area]
    in_rect = shapely.contains_xy(rect, xy[:, 0], xy[:, 1])
    held = [shapely.contains_xy(p, xy[:, 0], xy[:, 1]) for p in polys]
    count = np.zeros((len(polys) + 1, K), dtype=int)
    for i in np.nonzero(in_rect)[0]:
        for r in [r for r in range(len(polys)) if held[r][i]] or [len(polys)]:
            count[r, label[i]] += 1

    edges = rg["edges"]
    hept = polys[0]

    def within(e):
        grown = hept if e == 0 else hept.buffer(e, quad_segs=1024)
        return rect.intersection(grown).area

    cumulative = [within(e) for e in edges]
    bands = [cumulative[0]] + [cumulative[k] - cumulative[k - 1] for k in range(1, len(edges))] + [rect.area - cumulative[-1]]
    dist = shapely.distance(hept.exterior, shapely.points(xy))
    signed = np.where(shapely.contains_xy(hept, xy[:, 0], xy[:, 1]), -dist, dist)
    # The band below the first edge is 0; each edge at or below the distance moves it up one.
    band_of = np.searchsorted(np.array(edges), signed, side="right")
    band_count = np.zeros((len(bands), K), dtype=int)
    for i in np.nonzero(in_rect)[0]:
        band_count[band_of[i], label[i]] += 1

    def rel(a, b):
        return abs(a / b - 1) if b > 0 else abs(a)

    result["regions"] = {
        "rows": len(areas),
        "bands": len(bands),
        "maxRelativeArea": max(rel(a, b) for a, b in zip(rg["area"], areas)),
        "maxRelativeBandArea": max((rel(a, b) for a, b in zip(rg["bandArea"], bands) if b >= 100), default=0.0),
        "maxBandAreaDifference": max(abs(a - b) for a, b in zip(rg["bandArea"], bands)),
        "countsDiffering": int(np.sum(np.array(rg["count"]).reshape(count.shape) != count)),
        "bandCountsDiffering": int(np.sum(np.array(rg["bandCount"]).reshape(band_count.shape) != band_count)),
    }
    result["versions"]["shapely"] = version("shapely")
    r = result["regions"]
    print(f"{set_name} regions: {r['rows']} rows, areas within {r['maxRelativeArea']:.2g} of Shapely's, {r['countsDiffering']} counts differing; {r['bands']} bands, areas within {r['maxRelativeBandArea']:.2g} (at most {r['maxBandAreaDifference']:.2g} µm² apart), {r['bandCountsDiffering']} counts differing")
    out.write_text(json.dumps(result, indent=1) + "\n")


if __name__ == "__main__":
    main()
