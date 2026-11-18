"""Train the shipped model on every reference song (clean + separated stems) and save autochart/model.pkl.
  --exclude SONG  leave one song out (for honest end-to-end tests)   --out PATH"""
import sys, os, argparse, numpy as np
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import dataset, train, models, reduce as red
ap = argparse.ArgumentParser(); ap.add_argument("--exclude", default=""); ap.add_argument("--out", default=models.MODEL_PATH)
a = ap.parse_args()
songs = [s for s in sorted(os.listdir(dataset.DATA)) if a.exclude == "" or a.exclude not in s]
sets = [dataset.build(s, w) for s in songs for w in ("sep", "ref") if os.path.exists(os.path.join(dataset.DATA, s, f"drums_{w}.wav"))]
m = train.fit(sets)
Xs, Ys = [], {d: [] for d in ("hard", "medium", "easy")}
for s in songs:
    if "Oort" in s: continue  # its lower difficulties are copies of Expert
    r = dataset.ref_notes(s); dr = r["drums"]
    ex = [(t, c) for _, c, t in dr["expert"]]
    # features use ticks in units of the chart's own resolution; rescale to autochart's 192
    ex192 = [(int(round(t * 192 / r["tpb"])), c) for t, c in ex]
    Xs.append(red.features(ex192, 192))
    for d in Ys: Ys[d].append(red.labels(ex, [(t, c) for _, c, t in dr[d]]))
X = np.vstack(Xs)
for d in Ys:
    m.reduce[d] = models.hgb(max_iter=150).fit(X, np.concatenate(Ys[d]))
m.trained_on = songs
m.save(a.out)
print("saved", a.out, "songs:", len(songs))
