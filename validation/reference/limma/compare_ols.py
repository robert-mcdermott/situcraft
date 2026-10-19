"""The plain cases of validation/reference/limma/cases.json (each statistic's values by the design's
rows) by ordinary least squares, as statsmodels fits them, for internal/cohort's test to compare
SituCraft's unmoderated fits with (wave 7, slice 7). Run by compare.sh.

Each statistic's model is statsmodels' OLS of its values on the design; each contrast's estimate,
standard error, t (on the residual degrees of freedom), two-sided p and 95% interval come from
t_test.
"""

import json
import sys

import numpy as np
import statsmodels
import statsmodels.api as sm


def fit(case):
    x = np.asarray(case["x"], dtype=float)
    out = []
    for values in case["y"]:
        res = sm.OLS(np.asarray(values, dtype=float), x).fit()
        tests = []
        for c in case["contrasts"]:
            t = res.t_test(np.asarray([c]))
            lo, hi = t.conf_int(alpha=0.05)[0]
            tests.append({"estimate": float(t.effect[0]), "se": float(t.sd[0][0]), "t": float(t.tvalue[0][0]), "p": float(t.pvalue), "lo": float(lo), "hi": float(hi)})
        out.append({"params": res.params.tolist(), "sigma": float(np.sqrt(res.scale)), "df": float(res.df_resid), "tests": tests})
    return {"name": case["name"], "features": out}


def main(cases_path, out_path):
    out = [fit(c) for c in json.load(open(cases_path))["plain"]]
    with open(out_path, "w") as fh:
        json.dump({"statsmodels": statsmodels.__version__, "numpy": np.__version__, "cases": out}, fh, indent=None, separators=(",", ":"))
        fh.write("\n")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
