"""Leave-one-song-out with the full detector (stage 1 + context stage + cymbal lanes)."""
import sys, os, numpy as np, argparse
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import dataset, train, models
from autochart.dataset import CLASSES
ap = argparse.ArgumentParser(); ap.add_argument("--train", default="sep+ref"); ap.add_argument("--test", default="sep")
ap.add_argument("--thr", type=float, default=0.4); ap.add_argument("--fast", action="store_true")
a = ap.parse_args()
songs = sorted(os.listdir(dataset.DATA))
sets = {}
for s in songs:
    for w in set(a.train.split("+") + [a.test]):
        if os.path.exists(os.path.join(dataset.DATA, s, f"drums_{w}.wav")):
            sets[(s, w)] = dataset.build(s, w)
def match(pred, ref, tol=0.05):
    pred, ref = np.sort(pred), np.sort(ref); used = np.zeros(len(ref), bool); tp = 0
    for p in pred:
        lo, hi = np.searchsorted(ref, [p - tol, p + tol])
        c = [j for j in range(lo, hi) if not used[j]]
        if c: used[min(c, key=lambda j: abs(ref[j] - p))] = True; tp += 1
    return tp, len(pred), len(ref)
G = [g for g, _ in models.GROUPS] + ["hihat", "ride", "crash", "any"]
tot = {g: np.zeros(3) for g in G}
for s in songs:
    test = sets.get((s, a.test))
    if test is None: continue
    tr = [d for (s2, w), d in sets.items() if s2 != s and w in a.train.split("+")]
    m = train.fit(tr, log=lambda *x: None)
    Xn, P = m.predict(test["X"], test["times"])
    t = test["times"] - test["offset"]
    line = f"{s[:34]:34s}"
    for k, (g, mem) in enumerate(models.GROUPS):
        r = match(t[P[:, k] > a.thr], [x for x, c in test["ref"] if c in mem]); tot[g] += r
        line += f" {g} {2*r[0]/max(r[1]+r[2],1):.2f}"
    ci = np.nonzero(P[:, 2] > a.thr)[0]
    lane = m.lane.predict(np.array([models.cym_features(Xn, test["times"], P, i) for i in ci])) if len(ci) else []
    for li, c in enumerate(models.CYM_LANES):
        r = match(t[ci[np.array(lane) == li]] if len(ci) else [], [x for x, cc in test["ref"] if cc == c]); tot[c] += r
        line += f" {c[:4]} {2*r[0]/max(r[1]+r[2],1):.2f}"
    r = match(t[(P > a.thr).any(1)], sorted(set(x for x, _ in test["ref"]))); tot["any"] += r
    line += f" | any {2*r[0]/max(r[1]+r[2],1):.2f}"
    np.save(os.path.join(dataset.DATA, s, f"P2_{a.test}.npy"), P)
    print(line, flush=True)
print("TOTAL " + "  ".join(f"{g} P{v[0]/max(v[1],1):.2f} R{v[0]/max(v[2],1):.2f} F{2*v[0]/max(v[1]+v[2],1):.2f}" for g, v in tot.items()))
