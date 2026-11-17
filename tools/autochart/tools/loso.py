import os
"""Leave-one-song-out: train the per-class hit classifier on all other songs, score the held-out song.
Scoring: per class, a predicted hit matches a reference note of that class within +-50 ms (after the
song's chart offset). Reports precision, recall, F1."""
import sys, os, numpy as np, argparse
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import dataset
from autochart.dataset import CLASSES
from sklearn.ensemble import HistGradientBoostingClassifier

ap = argparse.ArgumentParser()
ap.add_argument("--train", default="ref", help="ref, sep or ref+sep")
ap.add_argument("--test", default="ref")
ap.add_argument("--songs", nargs="*")
ap.add_argument("--thr", type=float, default=0.4)
ap.add_argument("--group", default="kick,snare,cym:hihat+ride+crash,tom")
ap.add_argument("--norm", type=int, default=1)
a = ap.parse_args()
songs = a.songs or sorted(os.listdir(dataset.DATA))
sets = {}
for s in songs:
    for w in set(a.train.split("+") + [a.test]):
        p = os.path.join(dataset.DATA, s, f"drums_{w}.wav")
        if os.path.exists(p):
            sets[(s, w)] = dataset.build(s, w)


def match(pred, ref, tol=0.05):
    pred, ref = sorted(pred), sorted(ref)
    used = np.zeros(len(ref), bool); tp = 0
    j0 = 0
    for p in pred:
        while j0 < len(ref) and ref[j0] < p - tol: j0 += 1
        best = None
        for j in range(j0, len(ref)):
            if ref[j] > p + tol: break
            if not used[j] and (best is None or abs(ref[j] - p) < abs(ref[best] - p)): best = j
        if best is not None:
            used[best] = True; tp += 1
    return tp, len(pred), len(ref)


GROUPS = []
for g in a.group.split(","):
    name, _, members = g.partition(":")
    GROUPS.append((name, (members or name).split("+")))
def gY(d):
    return np.stack([d["Y"][:, [CLASSES.index(m) for m in mem]].max(1) for _, mem in GROUPS], 1)
def gref(d, mem):
    return [t for t, c in d["ref"] if c in mem]
def norm(X):
    mu = X.mean(0); sd = X.std(0) + 1e-6
    return np.hstack([X, (X - mu) / sd]) if a.norm else X
for d in sets.values():
    d["Xn"] = norm(d["X"]); d["G"] = gY(d)
tot = {g: [0, 0, 0] for g, _ in GROUPS}; tot["any"] = [0, 0, 0]
for s in songs:
    test = sets.get((s, a.test))
    if test is None: continue
    tr = [d for (s2, w), d in sets.items() if s2 != s and w in a.train.split("+")]
    X = np.vstack([d["Xn"] for d in tr]); Y = np.vstack([d["G"] for d in tr])
    line = f"{s[:34]:34s}"
    P = np.zeros((len(test["times"]), len(GROUPS)))
    for k in range(len(GROUPS)):
        m = HistGradientBoostingClassifier(max_iter=200, learning_rate=0.08, max_leaf_nodes=31,
                                           l2_regularization=1.0, random_state=0)
        m.fit(X, Y[:, k]); P[:, k] = m.predict_proba(test["Xn"])[:, 1]
    sh = test["times"] - test["offset"]
    for k, (g, mem) in enumerate(GROUPS):
        tp, np_, nr = match(sh[P[:, k] > a.thr], gref(test, mem))
        for i, v in enumerate((tp, np_, nr)): tot[g][i] += v
        line += f" {g[:5]} {2*tp/max(np_+nr,1):.2f}"
    tp, np_, nr = match(sh[(P > a.thr).any(1)], sorted(set(t for t, _ in test["ref"])))
    for i, v in enumerate((tp, np_, nr)): tot["any"][i] += v
    line += f" | any {2*tp/max(np_+nr,1):.2f}"
    np.save(os.path.join(dataset.DATA, s, f"P_{a.train}_{a.test}.npy"), P)
    print(line, flush=True)
allf = []
print("TOTAL", end="")
TP = NP = NR = 0
for c in [g for g, _ in GROUPS]:
    tp, np_, nr = tot[c]; TP += tp; NP += np_; NR += nr
    print(f"  {c} P{tp/max(np_,1):.2f} R{tp/max(nr,1):.2f} F{2*tp/max(np_+nr,1):.2f}", end="")
print(f"  any F{2*tot['any'][0]/max(tot['any'][1]+tot['any'][2],1):.2f}")
print(f"ALL  P{TP/NP:.3f} R{TP/NR:.3f} F1 {2*TP/(NP+NR):.3f}")
