"""Bar-line scores from chart files: autochart output (auto.json: beats + Expert notes) vs the human chart's
bar lines. Downbeat model is leave-one-song-out over these songs.
  metereval2.py <meterdata dir> [--main auto|8] [options]"""
import sys, os, json, argparse, numpy as np
from collections import Counter
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import meter, refmidi, models as M
ap = argparse.ArgumentParser(); ap.add_argument("root"); ap.add_argument("--main", default="auto")
ap.add_argument("--prior", type=float, default=1.0); ap.add_argument("--long", type=float, default=2.0)
ap.add_argument("--change", type=float, default=4.0); ap.add_argument("--rep", type=float, default=1.0)
ap.add_argument("--show-scores", action="store_true"); ap.add_argument("--pmain", type=float, default=1.0)
a = ap.parse_args()
LC = {0: "kick", 1: "snare"}
def load(d):
    doc = json.load(open(os.path.join(d, "auto.json"), encoding="utf-8"))
    tempos, res = doc["grid"]["tempos"], doc["grid"]["resolution"]
    ticks = [e["tick"] for e in tempos]; secs = [0.0]
    for i in range(1, len(tempos)): secs.append(secs[-1] + (ticks[i] - ticks[i - 1]) / res * 60 / tempos[i - 1]["bpm"])
    sec = lambda t: secs[max(0, np.searchsorted(ticks, t, side="right") - 1)] + (t - ticks[max(0, np.searchsorted(ticks, t, side="right") - 1)]) / res * 60 / tempos[max(0, np.searchsorted(ticks, t, side="right") - 1)]["bpm"]
    hits = []
    for n in doc["parts"]["drums"]["difficulties"]["expert"]["notes"]:
        c = LC.get(n["lane"]) or ({2: "hihat", 3: "ride", 4: "crash"}[n["lane"]] if n["cymbal"] else {2: "tom1", 3: "tom2", 4: "tom3"}[n["lane"]])
        hits.append(dict(t=sec(n["t"]), cls=c, conf=n["conf"]))
    beats = np.array(doc["analysis"]["beats"])
    cf = next(f for f in ("notes.mid", "notes.chart") if os.path.exists(os.path.join(d, f)))
    r = refmidi.load(os.path.join(d, cf)) if cf.endswith(".mid") else refmidi.load_chart(os.path.join(d, cf))
    tpb, sigs, tm = r["tpb"], sorted(r["sigs"]) or [(0, 4, 4)], r["tempo"]
    end = max(t for _, _, t in r["drums"]["expert"]) if r.get("drums") and r["drums"].get("expert") else 0
    bars, tick, i = [], 0, 0
    while tick <= end:
        while i + 1 < len(sigs) and sigs[i + 1][0] <= tick: i += 1
        bars.append((tm.sec(tick), sigs[i][1], sigs[i][2])); tick += max(1, int(sigs[i][1] * tpb * 4 / sigs[i][2]))
    return hits, beats, bars
def fm(est, ref, tol=0.07):
    est, ref = np.asarray(est), np.asarray(ref)
    if len(est) == 0 or len(ref) == 0: return 0.0
    tp = sum(np.min(np.abs(ref - e)) < tol for e in est); return 2 * tp / (len(est) + len(ref))
data = {}
for s in sorted(os.listdir(a.root)):
    d = os.path.join(a.root, s)
    if not os.path.exists(os.path.join(d, "auto.json")): continue
    hits, beats, bars = load(d)
    if not bars: continue
    pulses, F = meter.pulse_features(beats, hits)
    rbt = np.array([x for x, _, _ in bars])
    y = np.array([np.min(np.abs(rbt - p)) < 0.05 for p in pulses], int)
    data[s] = (pulses, F, y, bars)
if os.environ.get("SAVE_DOWNBEAT"):
    X = np.vstack([meter.context_rows(v[1]) for v in data.values()]).astype(np.float32)
    Y = np.concatenate([v[2] for v in data.values()]).astype(np.int8)
    np.savez_compressed(os.environ["SAVE_DOWNBEAT"], X=X, Y=Y); print("saved downbeat rows", X.shape, "from", len(data), "songs"); sys.exit(0)
tot = []
for s, (pulses, F, y, bars) in data.items():
    X = np.vstack([meter.context_rows(v[1]) for k, v in data.items() if k != s]); Y = np.concatenate([v[2] for k, v in data.items() if k != s])
    dm = M.hgb(max_iter=150).fit(X, Y)
    down = meter.downbeat_logodds(dm, F)
    main, sc = meter.main_length(F) if a.main == "auto" else (int(a.main), {})
    starts, lens = meter.segment(F, down=down, w_rep=a.rep, change=a.change, prior44=a.prior, long_cost=a.long, main=main, prior_main=a.pmain)
    est = pulses[starts]; rbt = np.array([x for x, _, _ in bars]); rbt = rbt[(rbt > est[0] - 1) & (rbt < est[-1] + 1)]
    f = fm(est, rbt); tot.append(f)
    ac = Counter("%d/%d" % meter.time_signature(L) for L in lens).most_common(3); rc = Counter(f"{n}/{dn}" for _, n, dn in bars).most_common(3)
    print(f"{s[:40]:40s} bars {f:.2f}  main {'%d/%d' % meter.time_signature(main):5s} auto {ac}  human {rc}")
    if a.show_scores: print("    lag scores:", {k: round(v, 2) for k, v in sc.items() if 4 <= k <= 20})
print(f"mean {np.mean(tot):.2f}  (n={len(tot)})")
