"""Score bar lines against the reference charts (downbeat F-measure, +-70 ms) using held-out detections
(P2_sep.npy from tools/loso2.py)."""
import sys, os, numpy as np, pickle, argparse
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import dataset, tempomap, meter, models, run
ap = argparse.ArgumentParser(); ap.add_argument("--change", type=float, default=2.5); ap.add_argument("--rep", type=float, default=2.0)
ap.add_argument("--prior", type=float, default=1.0); ap.add_argument("--inside", type=float, default=1.0); ap.add_argument("--long", type=float, default=2.0); a = ap.parse_args()
def ref_bars(r):
    tpb = r["tpb"]; sigs = sorted(r["sigs"]) or [(0, 4, 4)]; tm = r["tempo"]
    end = max(t for _, _, t in r["drums"]["expert"])
    bars, tick, i = [], 0, 0
    while tick <= end:
        while i + 1 < len(sigs) and sigs[i + 1][0] <= tick: i += 1
        bars.append((tm.sec(tick), sigs[i][1], sigs[i][2]))
        tick += int(sigs[i][1] * tpb * 4 / sigs[i][2])
    return bars
def fm(est, ref, tol=0.07):
    est, ref = np.asarray(est), np.asarray(ref)
    if len(est) == 0 or len(ref) == 0: return 0.0
    tp = sum(np.min(np.abs(ref - e)) < tol for e in est); return 2 * tp / (len(est) + len(ref))
from autochart import models as M
songs_data = {}
tot = []
for s in sorted(os.listdir(dataset.DATA)):
    pp = os.path.join(dataset.DATA, s, "P2_sep.npy")
    if not os.path.exists(pp): continue
    d = pickle.load(open(os.path.join(dataset.DATA, s, "cands_sep.pkl"), "rb")); P = np.load(pp)
    t = d["times"] - d["offset"]
    hits = []
    for i in range(len(t)):
        for k, cls in ((0, "kick"), (1, "snare"), (3, "tom2")):
            if P[i, k] > 0.4: hits.append(dict(t=t[i], cls=cls, conf=P[i, k]))
        if P[i, 2] > 0.4: hits.append(dict(t=t[i], cls="hihat", conf=P[i, 2]))
    cb = os.path.join(dataset.DATA, s, "beats_cache.npy")
    if os.path.exists(cb): beats = np.load(cb)
    else: beats = run.beat_grid(hits, t.max() + 2, lambda *x: None); np.save(cb, beats)
    pulses, F = meter.pulse_features(beats, hits)
    r = dataset.ref_notes(s); rb = ref_bars(r); rball = np.array([x for x, _, _ in rb])
    y = np.array([np.min(np.abs(rball - p)) < 0.05 for p in pulses], int)
    songs_data[s] = (pulses, F, y, rb)
if os.environ.get("SAVE_DOWNBEAT"):
    X = np.vstack([meter.context_rows(v[1]) for v in songs_data.values()]).astype(np.float32)
    Y = np.concatenate([v[2] for v in songs_data.values()]).astype(np.int8)
    np.savez_compressed(os.environ["SAVE_DOWNBEAT"], X=X, Y=Y); print("saved downbeat rows", X.shape)
for s, (pulses, F, y, rb) in songs_data.items():
    X = np.vstack([meter.context_rows(v[1]) for k, v in songs_data.items() if k != s])
    Y = np.concatenate([v[2] for k, v in songs_data.items() if k != s])
    dm = M.hgb(max_iter=150).fit(X, Y)
    down = meter.downbeat_logodds(dm, F)
    starts, lens = meter.segment(F, down=down, w_rep=a.rep, change=a.change, prior44=a.prior, w_inside=a.inside, long_cost=a.long)
    est = pulses[starts]
    rbt = np.array([x for x, _, _ in rb])
    rbt = rbt[(rbt > est[0] - 1) & (rbt < est[-1] + 1)]
    f = fm(est, rbt); tot.append(f)
    from collections import Counter
    sig_est = Counter(meter.time_signature(L) for L in lens).most_common(4)
    sig_ref = Counter((n, dn) for _, n, dn in rb).most_common(4)
    print(f"{s[:36]:36s} bars F {f:.2f}  est {sig_est}  ref {sig_ref}")
print(f"mean {np.mean(tot):.2f}")
