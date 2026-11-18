"""Train on library songs only (no reference songs at all), then score each reference song from its
separated mix. Compare with tools/loso2.py, which trains on the other reference songs.
  evallib.py [--with-refs]   (--with-refs also adds the other reference songs, leave-one-out)"""
import sys, os, argparse, numpy as np, time
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import dataset, train, models
ap = argparse.ArgumentParser(); ap.add_argument("--with-refs", action="store_true"); ap.add_argument("--thr", type=float, default=0.4)
a = ap.parse_args()
t0 = time.time()
all_songs = sorted(os.listdir(dataset.DATA))
lib = [s for s in all_songs if os.path.exists(os.path.join(dataset.DATA, s, "ref.pkl"))]
refs = [s for s in all_songs if s not in lib and os.path.exists(os.path.join(dataset.DATA, s, "drums_sep.wav"))]
print(f"library songs {len(lib)}, reference songs {len(refs)}", flush=True)
lib_sets = []
for s in lib:
    try:
        lib_sets.append(dataset.build(s, "sep"))
    except Exception as e:
        print("skip", s, repr(e)[:120])
ref_sets = {s: dataset.build(s, "sep") for s in refs}
print(f"features ready in {time.time()-t0:.0f}s", flush=True)
def match(pred, ref, tol=0.05):
    pred, ref = np.sort(pred), np.sort(ref); used = np.zeros(len(ref), bool); tp = 0
    for p in pred:
        lo, hi = np.searchsorted(ref, [p - tol, p + tol]); c = [j for j in range(lo, hi) if not used[j]]
        if c: used[min(c, key=lambda j: abs(ref[j] - p))] = True; tp += 1
    return tp, len(pred), len(ref)
G = [g for g, _ in models.GROUPS] + list(models.CYM_LANES) + ["any"]
tot = {g: np.zeros(3) for g in G}
shared = None if a.with_refs else train.fit(lib_sets)
for s in refs:
    m = shared or train.fit(lib_sets + [d for r, d in ref_sets.items() if r != s])
    test = ref_sets[s]
    Xn, P = m.predict(test["X"], test["times"])
    t = test["times"] - test["offset"]
    line = f"{s[:34]:34s}"
    for k, (g, mem) in enumerate(models.GROUPS):
        r = match(t[P[:, k] > a.thr], [x for x, c in test["ref"] if c in mem]); tot[g] += r
        line += f" {g} {2*r[0]/max(r[1]+r[2],1):.2f}"
    ci = np.nonzero(P[:, 2] > a.thr)[0]
    lane = m.lane.predict(np.array([models.cym_features(Xn, test["times"], P, i) for i in ci])) if len(ci) else np.array([])
    for li, c in enumerate(models.CYM_LANES):
        r = match(t[ci[lane == li]] if len(ci) else [], [x for x, cc in test["ref"] if cc == c]); tot[c] += r
        line += f" {c[:4]} {2*r[0]/max(r[1]+r[2],1):.2f}"
    r = match(t[(P > a.thr).any(1)], sorted(set(x for x, _ in test["ref"]))); tot["any"] += r
    print(line + f" | any {2*r[0]/max(r[1]+r[2],1):.2f}", flush=True)
print("TOTAL " + "  ".join(f"{g} P{v[0]/max(v[1],1):.2f} R{v[0]/max(v[2],1):.2f} F{2*v[0]/max(v[1]+v[2],1):.2f}" for g, v in tot.items()))
print(f"took {time.time()-t0:.0f}s")
