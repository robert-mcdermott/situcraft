"""SituCraft's clustering against scanpy, leidenalg, umap-learn and scikit-learn (wave 4, slice 1;
research.md §5), on the reference data sets: the instrument's segmentation of each, clustered by
SituCraft with its defaults (validation/clustering_test.go writes them into
<data>/clustering/<set>/situcraft/).

The bundle is read here by scanpy from its own cell_feature_matrix.h5, independently of SituCraft,
and taken through the same steps in float64: normalize_total to the median, log1p, scale (max_value
10), PCA (50 components); exact neighbors by scikit-learn on scanpy's components; umap-learn's fuzzy
graph of SituCraft's neighbors, and leidenalg on it with ten seeds; rank_genes_groups (Wilcoxon,
ties corrected) on SituCraft's clusters; and scanpy's own pipeline end to end (its neighbors and
Leiden). Writes validation/reference/clustering/<set>.json, whose numbers the Go test checks.

Usage (by compare.sh, with the pinned versions): python -I compare.py <data folder> <set> <out.json>
"""

import io
import itertools
import json
import os
import struct
import sys
import tarfile
import zlib
from importlib import metadata

import numpy as np
import pandas as pd
import pyarrow.parquet as pq
import scanpy as sc
import scipy.sparse as sp
import igraph as ig
import leidenalg
from scipy.stats import norm, rankdata, tiecorrect
from sklearn.metrics import adjusted_rand_score
from sklearn.neighbors import NearestNeighbors
from umap.umap_ import find_ab_params, fuzzy_simplicial_set, simplicial_set_embedding


# --- SituCraft's files (docs/FORMATS.md): magic, version, length, zlib body ---------------------

class Body:
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

    def str(self):
        shift = n = 0
        while True:
            byte = self.b[self.at]
            self.at += 1
            n |= (byte & 0x7F) << shift
            shift += 7
            if byte < 0x80:
                break
        s = self.b[self.at:self.at + n].decode()
        self.at += n
        return s

    def done(self):
        if self.at != len(self.b):
            raise ValueError(f"{len(self.b) - self.at} bytes left over")


def read_situcraft(folder):
    out = {"description": json.load(open(os.path.join(folder, "clustering.json")))}
    b = Body(os.path.join(folder, "cells.bin"), "SCKC")
    n = b.u32()
    out["cells"] = b.array("<u4", n).astype(np.int64)
    out["cluster"] = b.array("<u4", n).astype(np.int64)
    b.done()
    b = Body(os.path.join(folder, "pca.bin"), "SCPC")
    g = b.u32()
    out["genes"] = [b.str() for _ in range(g)]
    k = b.u32()
    out["mean"], out["sd"] = b.array("<f8", g), b.array("<f8", g)
    out["loadings"] = b.array("<f8", g * k).reshape(g, k)
    n2 = b.u32()
    out["scores"] = b.array("<f4", n2 * k).reshape(n2, k).astype(np.float64)
    b.done()
    b = Body(os.path.join(folder, "neighbors.bin"), "SCKN")
    n3, kk = b.u32(), b.u32()
    out["knn"] = b.array("<u4", n3 * kk).reshape(n3, kk).astype(np.int64)
    out["knn_dist"] = b.array("<f8", n3 * kk).reshape(n3, kk)
    b.done()
    b = Body(os.path.join(folder, "markers.bin"), "SCMR")
    c, g2 = b.u32(), b.u32()
    for name in ["score", "logfc", "p", "padj", "pct_in", "pct_out"]:
        out["m_" + name] = b.array("<f8", c * g2).reshape(c, g2)
    b.done()
    return out


def read_map(folder):
    d = json.load(open(os.path.join(folder, "embedding.json")))
    b = Body(os.path.join(folder, "embedding.bin"), "SCEB")
    n = b.u32()
    cells = b.array("<u4", n).astype(np.int64)
    xy = b.array("<f4", 2 * n).reshape(n, 2).astype(np.float64)
    score = b.array("<f4", n)
    b.done()
    return d, cells, xy, score


def map_quality(high, low, k=15, wide=75):
    """SituCraft's measures of a map, by their definitions (internal/embed): trustworthiness T(k)
    exact over every (n/Q)-th query cell, Q = min(n, 2000), ranks in the space by distance then by
    index; and the mean precision and recall of each cell's k nearest within the other side's
    wide nearest."""
    n = len(high)
    queries = min(n, 2000)
    lo = NearestNeighbors(n_neighbors=wide + 1, algorithm="brute").fit(low).kneighbors(return_distance=False)[:, :wide]
    hi = NearestNeighbors(n_neighbors=wide + 1, algorithm="brute").fit(high).kneighbors(return_distance=False)[:, :wide]
    total = 0
    for q in range(queries):
        i = q * n // queries
        d = ((high - high[i]) ** 2).sum(axis=1)
        d[i] = np.inf
        order = np.lexsort((np.arange(n), d))
        rank = np.empty(n, dtype=np.int64)
        rank[order] = np.arange(1, n + 1)
        for j in lo[i, :k]:
            if rank[j] > k:
                total += int(rank[j]) - k
    t = 1 - 2 / (queries * k * (2 * n - 3 * k - 1)) * total
    precision = np.mean([len(set(lo[i, :k]) & set(hi[i])) / k for i in range(n)])
    recall = np.mean([len(set(hi[i, :k]) & set(lo[i])) / k for i in range(n)])
    return float(t), float(precision), float(recall)


# --- Helpers ----------------------------------------------------------------------------------

def modularity(graph, labels, resolution=1.0):
    """Σ_c in_c/2m − γ(tot_c/2m)² of a symmetric sparse graph."""
    graph = sp.csr_matrix(graph)
    strength = np.asarray(graph.sum(axis=1)).ravel()
    two_m = strength.sum()
    coo = graph.tocoo()
    same = labels[coo.row] == labels[coo.col]
    k = labels.max() + 1
    inside = np.bincount(labels[coo.row[same]], weights=coo.data[same], minlength=k)
    tot = np.bincount(labels, weights=strength, minlength=k)
    return float(np.sum(inside / two_m - resolution * (tot / two_m) ** 2))


def bh(p):
    """Benjamini–Hochberg adjusted p-values (as statsmodels' fdr_bh)."""
    order = np.argsort(p, kind="stable")
    m = len(p)
    adj = np.minimum.accumulate((p[order] * m / np.arange(1, m + 1))[::-1])[::-1]
    out = np.empty(m)
    out[order] = np.minimum(adj, 1)
    return out


def subspace_cos(a, b):
    """The cosine of the largest principal angle between two column spaces."""
    qa, _ = np.linalg.qr(a)
    qb, _ = np.linalg.qr(b)
    return float(np.linalg.svd(qa.T @ qb, compute_uv=False).min())


def versions():
    out = {"python": sys.version.split()[0]}
    for p in ["scanpy", "anndata", "leidenalg", "igraph", "umap-learn", "scikit-learn", "numpy", "scipy", "pandas", "pyarrow", "h5py", "pynndescent"]:
        out[p] = metadata.version(p)
    return out


def instrument_graphclust(bundle):
    """The instrument's graph-based clusters (analysis.tar.gz), by cell id."""
    path = os.path.join(bundle, "analysis.tar.gz")
    if not os.path.exists(path):
        return None
    with tarfile.open(path) as tar:
        for member in tar.getmembers():
            if member.name.endswith("gene_expression_graphclust/clusters.csv"):
                return pd.read_csv(io.BytesIO(tar.extractfile(member).read())).set_index("Barcode")["Cluster"]
    return None


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


# --- The comparison ---------------------------------------------------------------------------

def main(data, name, out_path):
    folder = os.path.join(data, "clustering", name)
    info = json.load(open(os.path.join(folder, "situcraft.json")))
    bundle = info["bundle"]
    s = read_situcraft(os.path.join(folder, "situcraft"))
    d = s["description"]
    ids = pq.read_table(os.path.join(bundle, "cells.parquet"), columns=["cell_id"]).column(0).to_pylist()
    analyzed = [ids[i] for i in s["cells"]]
    result = {"dataSet": name, "tools": versions()}
    result["situcraft"] = {"version": d["method"], "digest": info["digest"], "cells": len(analyzed), "clusters": d["clusters"]["count"], "modularity": d["clusters"]["modularity"]}

    # scanpy reads the bundle's own matrix; the cells with at least 10 gene transcripts.
    adata = sc.read_10x_h5(os.path.join(bundle, "cell_feature_matrix.h5"))
    adata.var_names_make_unique()
    adata.X = sp.csr_matrix(adata.X, dtype=np.float64)
    totals = np.asarray(adata.X.sum(axis=1)).ravel()
    kept = set(adata.obs_names[totals >= d["parameters"]["minTranscripts"]])
    result["cells"] = {"same": kept == set(analyzed), "analyzed": len(kept)}
    if set(adata.var_names) != set(s["genes"]):
        raise SystemExit(f"{name}: the genes differ: {sorted(set(adata.var_names) ^ set(s['genes']))[:10]}")
    adata = adata[analyzed, s["genes"]].copy()

    counts = adata.X.tocsc(copy=True)
    totals = np.asarray(adata.X.sum(axis=1)).ravel()
    sc.pp.normalize_total(adata, target_sum=None)
    sc.pp.log1p(adata)
    adata.raw = adata
    sc.pp.scale(adata, max_value=10)
    k = d["pca"]["components"]
    sc.tl.pca(adata, n_comps=k, svd_solver="covariance_eigh", zero_center=True)
    variance = adata.uns["pca"]["variance"]
    ratio = adata.uns["pca"]["variance_ratio"]
    loadings = adata.varm["PCs"]
    ours_var = np.array(d["pca"]["variance"])
    cos = np.abs(np.sum(loadings * s["loadings"], axis=0)) / (np.linalg.norm(loadings, axis=0) * np.linalg.norm(s["loadings"], axis=0))
    signs = np.sign(np.sum(loadings * s["loadings"], axis=0))
    result["pca"] = {
        "varianceRelDiff": float(np.max(np.abs(ours_var - variance) / variance)),
        "ratioRelDiff": float(np.max(np.abs(np.array(d["pca"]["varianceRatio"]) - ratio) / ratio)),
        "cosMin10": float(cos[:10].min()),
        "cosByComponent": [round(float(c), 8) for c in cos],
        "subspaceCos50": subspace_cos(loadings, s["loadings"]),
        "scoreMaxDiff": float(np.max(np.abs(adata.obsm["X_pca"][:, :10] * signs[:10] - s["scores"][:, :10]))),
    }

    # Exact neighbors on scanpy's components, against SituCraft's.
    kk = d["parameters"]["neighbors"] - 1
    nn = NearestNeighbors(n_neighbors=kk + 1, algorithm="brute").fit(adata.obsm["X_pca"])
    _, idx = nn.kneighbors()
    idx = idx[:, :kk]
    same = [set(idx[i]) == set(s["knn"][i]) for i in range(len(idx))]
    jac = [len(set(idx[i]) & set(s["knn"][i])) / len(set(idx[i]) | set(s["knn"][i])) for i in range(len(idx))]
    result["neighbors"] = {"sameSets": float(np.mean(same)), "meanJaccard": float(np.mean(jac))}

    # umap-learn's fuzzy graph of SituCraft's neighbors (the cell first, as umap-learn counts it),
    # and of scikit-learn's exact neighbors as scanpy makes it.
    n = len(analyzed)
    knn_i = np.hstack([np.arange(n)[:, None], s["knn"]])
    knn_d = np.hstack([np.zeros((n, 1)), s["knn_dist"]])
    g, _, _ = fuzzy_simplicial_set(np.zeros((n, 1)), kk + 1, 0, "euclidean", knn_indices=knn_i, knn_dists=knn_d)
    g = sp.csr_matrix(g)
    labels = s["cluster"]
    result["graph"] = {"modularityOnUmapGraph": modularity(g, labels)}
    sc.pp.neighbors(adata, n_neighbors=kk + 1, use_rep="X_pca", transformer="sklearn")
    result["graph"]["weightMaxDiff"] = float(np.max(np.abs((adata.obsp["connectivities"] - g).toarray()))) if n <= 20000 else None

    # leidenalg on that graph, ten seeds.
    coo = sp.triu(g, k=1).tocoo()
    graph = ig.Graph(n=n, edges=list(zip(coo.row.tolist(), coo.col.tolist())), edge_attrs={"weight": coo.data.tolist()})
    theirs = []
    for seed in range(10):
        part = leidenalg.find_partition(graph, leidenalg.RBConfigurationVertexPartition, weights="weight", resolution_parameter=1.0, n_iterations=-1, seed=seed)
        theirs.append(np.array(part.membership))
    result["leiden"] = {
        "leidenalg": [modularity(g, m) for m in theirs],
        "clusters": [int(m.max() + 1) for m in theirs],
        "ariWithLeidenalg": [float(adjusted_rand_score(labels, m)) for m in theirs],
        "ariAmongLeidenalg": [float(adjusted_rand_score(a, b)) for a, b in itertools.combinations(theirs, 2)],
    }

    # The map: its curve against umap-learn's; its trustworthiness recomputed from its places; and
    # umap-learn's layouts of the same graph (five seeds) by the same measures.
    md, mcells, mxy, _ = read_map(os.path.join(folder, "map"))
    if not np.array_equal(mcells, s["cells"]):
        raise SystemExit(f"{name}: the map's cells are not the clustering's")
    ua, ub = find_ab_params(1.0, md["parameters"]["minDist"])
    t_ours, p_ours, r_ours = map_quality(s["scores"], mxy)
    umap_t, umap_r = [], []
    for seed in range(5):
        emb, _ = simplicial_set_embedding(s["scores"], sp.coo_matrix(g), 2, 1.0, ua, ub, 1.0, 5, None, "spectral", np.random.RandomState(seed), "euclidean", {}, False, {}, False)
        t_u, _, r_u = map_quality(s["scores"], np.asarray(emb, dtype=np.float64))
        umap_t.append(t_u)
        umap_r.append(r_u)
    result["map"] = {
        "a": md["fitted"]["a"], "b": md["fitted"]["b"], "umapA": float(ua), "umapB": float(ub),
        "trustworthiness": md["quality"]["trustworthiness"], "trustworthinessRecomputed": t_ours,
        "precision": md["quality"]["precision"], "precisionRecomputed": p_ours,
        "recall": md["quality"]["recall"], "recallRecomputed": r_ours,
        "umapTrustworthiness": umap_t, "umapRecall": umap_r,
    }
    result["situcraft"]["mapDigest"] = info["mapDigest"]

    # rank_genes_groups on SituCraft's clusters, from the log-normalized values.
    raw = adata.raw.to_adata()
    raw.obs["situcraft"] = pd.Categorical([str(c) for c in labels], categories=[str(c) for c in range(labels.max() + 1)])
    sc.tl.rank_genes_groups(raw, "situcraft", method="wilcoxon", tie_correct=True, pts=True, n_genes=raw.n_vars, use_raw=False)
    rg = raw.uns["rank_genes_groups"]
    gene_at = {g: i for i, g in enumerate(s["genes"])}
    score_diff = logfc_diff = pct_diff = lp_diff = 0.0
    top = []
    for c in range(labels.max() + 1):
        names = rg["names"][str(c)]
        at = np.array([gene_at[x] for x in names])
        score_diff = max(score_diff, float(np.max(np.abs(rg["scores"][str(c)] - s["m_score"][c, at]))))
        logfc_diff = max(logfc_diff, float(np.max(np.abs(rg["logfoldchanges"][str(c)] - s["m_logfc"][c, at]))))
        lp = lambda p: np.log10(np.maximum(p, 1e-300))
        lp_diff = max(lp_diff, float(np.max(np.abs(lp(rg["pvals_adj"][str(c)]) - lp(s["m_padj"][c, at])))))
        pts = raw.uns["rank_genes_groups"]["pts"][str(c)].reindex(s["genes"]).to_numpy()
        pts_rest = raw.uns["rank_genes_groups"]["pts_rest"][str(c)].reindex(s["genes"]).to_numpy()
        pct_diff = max(pct_diff, float(np.max(np.abs(pts - s["m_pct_in"][c]))), float(np.max(np.abs(pts_rest - s["m_pct_out"][c]))))
        ours_top = [s["genes"][i] for i in np.lexsort((np.arange(len(s["genes"])), -s["m_score"][c]))[:10]]
        top.append(len(set(ours_top) & set(names[:10])) / 10)
    result["markers"] = {"scoreMaxDiff": score_diff, "logfcMaxDiff": logfc_diff, "pctMaxDiff": pct_diff, "log10PadjMaxDiff": lp_diff, "top10Agreement": float(np.mean(top))}

    # The rank-sum test by scipy with exact ties: cells ranked by count / total, which float64
    # division keeps equal for equal ratios and apart for unequal ones (scanpy ranks the rounded
    # log-normalized values, whose rounding can part two cells expressing a gene equally).
    k = labels.max() + 1
    n1 = np.bincount(labels, minlength=k).astype(np.float64)
    n2 = n - n1
    exact_score = np.zeros((k, len(s["genes"])))
    for g in range(len(s["genes"])):
        key = counts[:, g].toarray().ravel() / totals
        r = rankdata(key)
        t = tiecorrect(r)
        sums = np.bincount(labels, weights=r, minlength=k)
        sd = np.sqrt(t * n1 * n2 * (n + 1) / 12)
        with np.errstate(invalid="ignore", divide="ignore"):
            exact_score[:, g] = np.where(sd > 0, (sums - n1 * (n + 1) / 2) / sd, 0)
    exact_p = 2 * norm.sf(np.abs(exact_score))
    exact_adj = np.vstack([bh(row) for row in exact_p])
    lp = lambda p: np.log10(np.maximum(p, 1e-300))
    result["markers"]["exactScoreMaxDiff"] = float(np.max(np.abs(exact_score - s["m_score"])))
    result["markers"]["exactLog10PadjMaxDiff"] = float(np.max(np.abs(lp(exact_adj) - lp(s["m_padj"]))))

    # scanpy's own pipeline end to end: its neighbors (umap-learn's, approximate above 4,096
    # cells) and Leiden.
    sc.pp.neighbors(adata, n_neighbors=kk + 1, use_rep="X_pca", random_state=0)
    sc.tl.leiden(adata, resolution=1.0, random_state=0, flavor="leidenalg", n_iterations=-1)
    scanpy_labels = adata.obs["leiden"].astype(int).to_numpy()
    result["endToEnd"] = {"ari": float(adjusted_rand_score(labels, scanpy_labels)), "clustersScanpy": int(scanpy_labels.max() + 1)}

    # The instrument's own clusters, and on a simulation the true types.
    graphclust = instrument_graphclust(bundle)
    result["instrument"] = None
    if graphclust is not None:
        common = [c for c in analyzed if c in graphclust.index]
        at = {c: i for i, c in enumerate(analyzed)}
        result["instrument"] = {"ariGraphclust": float(adjusted_rand_score(labels[[at[c] for c in common]], graphclust.loc[common])), "clustersGraphclust": int(graphclust.nunique()), "cells": len(common)}
    types = true_types(bundle)
    result["truth"] = None
    if types is not None:
        known = [i for i, c in enumerate(analyzed) if c in types.index and isinstance(types.loc[c], str)]
        t = types.loc[[analyzed[i] for i in known]].to_numpy()
        result["truth"] = {"types": int(len(set(t))), "cells": len(known), "ariSituCraft": float(adjusted_rand_score(t, labels[known])), "ariScanpy": float(adjusted_rand_score(t, scanpy_labels[known])), "clustersScanpy": int(scanpy_labels.max() + 1)}

    with open(out_path, "w") as f:
        json.dump(result, f, indent=1)
        f.write("\n")
    print(json.dumps({k: result[k] for k in ["cells", "neighbors", "graph", "map", "markers", "endToEnd", "instrument", "truth"]}, indent=1)[:4000])


if __name__ == "__main__":
    main(*sys.argv[1:4])
