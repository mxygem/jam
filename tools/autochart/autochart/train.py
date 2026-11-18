"""Fit a Model from labelled reference song sets (dataset.build outputs)."""
import numpy as np
from . import models
from .dataset import CLASSES


def group_labels(d):
    return np.stack([d["Y"][:, [CLASSES.index(m) for m in mem]].max(1) for _, mem in models.GROUPS], 1)


def song_weights(sets):
    """Per-set sample weight. Reference songs 1.0. Library songs: by Gem's ranking of the charter
    (1st 1.5 down to ~0.6 for 12th), and each charter's songs share at most 20 songs' worth of weight."""
    per = {}
    for d in sets:
        if d.get("charter"):
            per.setdefault(d["charter"], set()).add(d["song"])
    w = []
    for d in sets:
        if not d.get("charter"):
            w.append(1.0); continue
        rank_w = 1.5 - 0.08 * (d["rank"] - 1)
        w.append(rank_w * min(1.0, 20.0 / len(per[d["charter"]])))
    return w


def stack(sets, f):
    w = song_weights(sets)
    return np.concatenate([np.full(len(d["times"]), wi, np.float32) for d, wi in zip(sets, w)])


def fit_stage1(sets):
    X = np.vstack([models.norm(d["X"]) for d in sets]); Y = np.vstack([group_labels(d) for d in sets])
    W = stack(sets, None)
    return [models.hgb().fit(X, Y[:, k], sample_weight=W) for k in range(len(models.GROUPS))]


def oof_stage1(sets):
    """Out-of-fold stage-1 probabilities per song (each song predicted by models trained without it).
    Two sets of the same song (clean and separated stems) are held out together."""
    songs = sorted({d["song"] for d in sets})
    nf = min(len(songs), 6)
    fold = {s: i % nf for i, s in enumerate(songs)}
    cache = {f: fit_stage1([e for e in sets if fold[e["song"]] != f]) for f in range(nf)}
    out = []
    for d in sets:
        Xn = models.norm(d["X"])
        out.append(np.stack([m.predict_proba(Xn)[:, 1] for m in cache[fold[d["song"]]]], 1))
    return out


def lane_rows(d, Xn, P):
    rows, ys = [], []
    for i in np.nonzero(d["Y"][:, [CLASSES.index(c) for c in models.CYM_LANES]].any(1))[0]:
        lab = [c for c in models.CYM_LANES if d["Y"][i, CLASSES.index(c)]]
        rows.append(models.cym_features(Xn, d["times"], P, i)); ys.append(models.CYM_LANES.index(lab[0]))
    return rows, ys


def fit(sets, log=print):
    m = models.Model()
    m.templates = True
    m.det = fit_stage1(sets)
    oof = oof_stage1(sets)
    Xc, Y, LX, LY, LW = [], [], [], [], []
    ws = song_weights(sets)
    for d, P1, wi in zip(sets, oof, ws):
        Xn = models.norm(d["X"])
        Xc.append(models.context(Xn, d["times"], P1)); Y.append(group_labels(d))
        r, y = lane_rows(d, Xn, P1); LX += r; LY += y; LW += [wi] * len(y)
    Xc = np.vstack(Xc); Y = np.vstack(Y)
    W = stack(sets, None)
    m.ctx = [models.hgb().fit(Xc, Y[:, k], sample_weight=W) for k in range(len(models.GROUPS))]
    m.lane = models.hgb(max_iter=150).fit(np.array(LX), np.array(LY), sample_weight=np.array(LW))
    log(f"trained on {len(sets)} sets, {len(Y)} candidates, {len(LY)} cymbal hits")
    return m
