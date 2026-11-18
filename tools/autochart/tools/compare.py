"""Score an autochart against a human chart (.mid) per class, +-50 ms, after a global offset search.
  compare.py <human notes.mid> <autochart folder> [--diff expert]"""
import sys, os, numpy as np, argparse, json
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import refmidi, chart
ap = argparse.ArgumentParser(); ap.add_argument("ref"); ap.add_argument("auto"); ap.add_argument("--diff", default="expert")
ap.add_argument("--five", action="store_true")
a = ap.parse_args()
r = refmidi.load(a.ref, five_lane=a.five or None)
ref = [(t, c) for t, c, _ in r["drums"][a.diff]]
doc = json.load(open(os.path.join(a.auto, "chart.jam.json")))
tempos = doc["grid"]["tempos"]; res = doc["grid"]["resolution"]
def sec(tick):
    s, last_t, last_b = 0.0, 0, tempos[0]["bpm"]
    for e in tempos[1:]:
        if e["tick"] > tick: break
        s += (e["tick"] - last_t) / res * 60 / last_b; last_t, last_b = e["tick"], e["bpm"]
    return s + (tick - last_t) / res * 60 / last_b
LANECLS = {0: "kick", 1: "snare"}
auto = []
for n in doc["parts"]["drums"]["difficulties"][a.diff]["notes"]:
    c = LANECLS.get(n["lane"]) or ({2: "hihat", 3: "ride", 4: "crash"}[n["lane"]] if n["cymbal"] else {2: "tom1", 3: "tom2", 4: "tom3"}[n["lane"]])
    auto.append((sec(n["t"]), c))
GROUP = {"kick": "kick", "kick2x": "kick", "snare": "snare", "hihat": "cym", "ride": "cym", "crash": "cym", "tom1": "tom", "tom2": "tom", "tom3": "tom"}
def match(p, q, tol=0.05):
    p, q = np.sort(p), np.sort(q); used = np.zeros(len(q), bool); tp = 0
    for x in p:
        lo, hi = np.searchsorted(q, [x - tol, x + tol]); c = [j for j in range(lo, hi) if not used[j]]
        if c: used[min(c, key=lambda j: abs(q[j] - x))] = True; tp += 1
    return tp, len(p), len(q)
at = np.array([t for t, _ in auto]); rt = np.array([t for t, _ in ref])
# global offset = median distance from each auto note to the nearest human note (within 60 ms)
srt = np.sort(rt); j = np.clip(np.searchsorted(srt, at), 1, len(srt) - 1)
near = np.where(np.abs(srt[j - 1] - at) < np.abs(srt[j] - at), srt[j - 1], srt[j]); dd = near - at
off = float(np.median(dd[np.abs(dd) < 0.06]))
print(f"offset {off*1000:+.0f} ms (auto notes shifted to match)")
out = {}
for g in ["kick", "snare", "cym", "tom"]:
    tp, n_a, n_r = match([t + off for t, c in auto if GROUP[c] == g], [t for t, c in ref if GROUP[c] == g])
    out[g] = (tp, n_a, n_r)
for c in ["hihat", "ride", "crash"]:
    out[c] = match([t + off for t, cc in auto if cc == c], [t for t, cc in ref if cc == c])
out["all (lane-exact)"] = tuple(sum(out[g][i] for g in ["kick", "snare", "hihat", "ride", "crash"]) + match([t + off for t, c in auto if GROUP[c] == "tom"], [t for t, c in ref if GROUP[c] == "tom"])[i] for i in range(3))
for k, (tp, n_a, n_r) in out.items():
    print(f"  {k:18s} P {tp/max(n_a,1):.2f} R {tp/max(n_r,1):.2f} F1 {2*tp/max(n_a+n_r,1):.2f}   ({n_a} auto, {n_r} human)")
