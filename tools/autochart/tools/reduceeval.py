import sys, os, numpy as np
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import dataset, reduce, models
songs = [s for s in sorted(os.listdir(dataset.DATA)) if "Oort" not in s]
data = {}
for s in songs:
    r = dataset.ref_notes(s); dr = r["drums"]
    ex = [(t, c) for _, c, t in dr["expert"]]
    X = reduce.features(ex, r["tpb"])
    data[s] = (ex, X, {d: reduce.labels(ex, [(t, c) for _, c, t in dr[d]]) for d in ("hard", "medium", "easy")}, r["tpb"])
for d in ("hard", "medium", "easy"):
    line = f"{d:6s}"; tot = np.zeros(3); base = np.zeros(3)
    for s in songs:
        X = np.vstack([data[o][1] for o in songs if o != s]); Y = np.concatenate([data[o][2][d] for o in songs if o != s])
        m = models.hgb(max_iter=150).fit(X, Y)
        p = m.predict_proba(data[s][1])[:, 1] > 0.5; y = data[s][2][d].astype(bool)
        tp = (p & y).sum(); tot += [tp, p.sum(), y.sum()]
        base += [y.sum(), len(y), y.sum()]  # keep-everything baseline
        line += f" {s[:12]} {2*tp/(p.sum()+y.sum()):.2f}"
    print(line, f"| learned F1 {2*tot[0]/(tot[1]+tot[2]):.2f}  keep-all F1 {2*base[0]/(base[1]+base[2]):.2f}")
