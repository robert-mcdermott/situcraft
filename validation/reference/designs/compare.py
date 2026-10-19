"""The design matrices of validation/reference/designs/cases.json, as patsy builds them.

Each case's rows (one sample each) and specification become a formula in patsy's terms (treatment
coding, the reference given; the replicate unit a factor when it is paired; numbers as they are)
and its matrix is written with the column names patsy gives, for web/lib/design-reference.test.mjs
to compare SituCraft's with (wave 7, slice 3). Run by compare.sh.
"""

import json
import sys

import pandas as pd
import patsy


def formula(case):
    spec = case["spec"]
    group, unit = spec["group"], spec["unit"]
    terms = [f"C({group}, Treatment(reference='{spec['reference']}'))"]
    units = {}
    for r in case["rows"]:
        units.setdefault(r[unit], set()).add(r[group])
    if any(len(g) > 1 for g in units.values()):
        terms.append(f"C({unit})")
    numbers = set(case.get("numbers", []))
    for c in spec.get("covariates", []):
        terms.append(c if c in numbers else f"C({c})")
    return "1 + " + " + ".join(terms)


def main(cases_path, out_path):
    out = []
    for case in json.load(open(cases_path)):
        frame = pd.DataFrame(case["rows"])
        f = formula(case)
        m = patsy.dmatrix(f, frame, return_type="dataframe")
        out.append({"name": case["name"], "formula": f, "names": list(m.columns), "matrix": m.values.tolist()})
    with open(out_path, "w") as fh:
        json.dump({"patsy": patsy.__version__, "pandas": pd.__version__, "cases": out}, fh, indent=1)
        fh.write("\n")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
