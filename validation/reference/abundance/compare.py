"""The quasi-likelihood models of validation/reference/abundance/cases.json, as statsmodels fits
them, for internal/cohort's test to compare SituCraft's with (wave 7, slice 6). Run by compare.sh.

A binomial case is a quasi-binomial model (statsmodels' GLM, Binomial family, logit link, each
count's proportion of its total weighted by the total, as R's glm(cbind(y, n - y), family =
quasibinomial) fits it); a Poisson case a quasi-Poisson model (log link, the log of each
area an offset). The dispersion is Pearson's chi-square over the residual degrees of freedom
(scale='X2'); tests are t tests on those degrees of freedom (use_t). Each contrast's estimate,
standard error, t, two-sided p and 95% interval come from t_test.
"""

import json
import sys

import numpy as np
import statsmodels
import statsmodels.api as sm


def fit(case):
    x = np.asarray(case["x"], dtype=float)
    y = np.asarray(case["y"], dtype=float)
    if case["family"] == "binomial":
        n = np.asarray(case["n"], dtype=float)
        # Proportions weighted by their trials: Pearson's chi-square then counts each trial, as
        # R's quasibinomial does (the two-column form leaves the trials out of it).
        model = sm.GLM(y / n, x, family=sm.families.Binomial(), var_weights=n)
    else:
        model = sm.GLM(y, x, family=sm.families.Poisson(), offset=np.log(np.asarray(case["area"], dtype=float)))
    res = model.fit(scale="X2", use_t=True, tol=1e-14, maxiter=1000)
    tests = []
    for c in case["contrasts"]:
        t = res.t_test(np.asarray([c]))
        lo, hi = t.conf_int(alpha=0.05)[0]
        tests.append({"estimate": float(t.effect[0]), "se": float(t.sd[0][0]), "t": float(t.tvalue[0][0]), "p": float(t.pvalue), "lo": float(lo), "hi": float(hi)})
    return {"name": case["name"], "params": res.params.tolist(), "bse": res.bse.tolist(), "scale": float(res.scale), "df": float(res.df_resid), "deviance": float(res.deviance), "tests": tests}


def main(cases_path, out_path):
    out = [fit(c) for c in json.load(open(cases_path))]
    with open(out_path, "w") as fh:
        json.dump({"statsmodels": statsmodels.__version__, "numpy": np.__version__, "cases": out}, fh, indent=1)
        fh.write("\n")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
