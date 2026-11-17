"""Beat F-measure vs the reference chart's tempo map (+-70 ms), octave tolerant (x2, /2)."""
import sys, os, numpy as np, pickle
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import tempomap, dataset
which = sys.argv[1] if len(sys.argv) > 1 else "ref"
def ref_beats(song):
    r = dataset.ref_notes(song); tm = r["tempo"]; tpb = r["tpb"]
    if r["beats"]:
        return np.array([t for t, _ in r["beats"]])
    end = max(t for _, _, t in r["drums"]["expert"])
    return np.array([tm.sec(k) for k in range(0, end, tpb)])
def fm(est, ref, tol=0.07):
    if len(est) == 0: return 0
    tp = sum(np.min(np.abs(ref - e)) < tol for e in est)
    return 2 * tp / (len(est) + len(ref))
for s in sorted(os.listdir(dataset.DATA)):
    pk = os.path.join(dataset.DATA, s, f"cands_{which}.pkl")
    pp = os.path.join(dataset.DATA, s, f"P_{which}_{which}.npy")
    if not (os.path.exists(pk) and os.path.exists(pp)): continue
    d = pickle.load(open(pk, "rb")); P = np.load(pp)
    t = d["times"] - d["offset"]
    w = np.maximum.reduce([P[:, 0] * 0.6, P[:, 1] * 1.0, P[:, 2] * 0.45, P[:, 3] * 0.45])
    hits = [(x, y) for x, y in zip(t, w) if y > 0.3]
    beats, (c, bpm, conf) = tempomap.track(hits, t.max() + 1)
    hk = lambda k: [(x, y) for x, y in zip(t, P[:, k]) if y > 0.3]
    beats, nflip = tempomap.fix_phase(beats, hk(0), hk(1), hk(2))
    beats = tempomap.refine(beats, hits)
    rb = ref_beats(s); rb = rb[(rb > beats[0] - 1) & (rb < beats[-1] + 1)]
    rbpm = 60 / np.median(np.diff(rb))
    f1 = fm(beats, rb); f2 = fm(beats, np.sort(np.r_[rb, (rb[1:] + rb[:-1]) / 2])); f3 = fm(beats, rb[::2])
    print(f"{s[:36]:36s} ref ~{rbpm:6.1f} est median {np.median(bpm):6.1f}  F {f1:.2f} (x2 {f2:.2f}, /2 {f3:.2f})  beats {len(beats)} vs {len(rb)} flipped {nflip}")
