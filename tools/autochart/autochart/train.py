"""Fit a Model from labelled reference song sets (dataset.build outputs)."""
import numpy as np
from . import models
from .dataset import CLASSES


def group_labels(d):
    return np.stack([d["Y"][:, [CLASSES.index(m) for m in mem]].max(1) for _, mem in models.GROUPS], 1)


def fit_stage1(sets):
    X = np.vstack([models.norm(d["X"]) for d in sets]); Y = np.vstack([group_labels(d) for d in sets])
    return [models.hgb().fit(X, Y[:, k]) for k in range(len(models.GROUPS))]


def oof_stage1(sets):
    """Out-of-fold stage-1 probabilities per song (each song predicted by models trained without it).
    Two sets of the same song (clean and separated stems) are held out together."""
    out, cache = [], {}
    for d in sets:
        if d["song"] not in cache:
            cache[d["song"]] = fit_stage1([e for e in sets if e["song"] != d["song"]])
        Xn = models.norm(d["X"])
        out.append(np.stack([m.predict_proba(Xn)[:, 1] for m in cache[d["song"]]], 1))
    return out


def lane_rows(d, Xn, P):
    rows, ys = [], []
    for i in np.nonzero(d["Y"][:, [CLASSES.index(c) for c in models.CYM_LANES]].any(1))[0]:
        lab = [c for c in models.CYM_LANES if d["Y"][i, CLASSES.index(c)]]
        rows.append(models.cym_features(Xn, d["times"], P, i)); ys.append(models.CYM_LANES.index(lab[0]))
    return rows, ys


def fit(sets, log=print):
    m = models.Model()
    m.det = fit_stage1(sets)
    oof = oof_stage1(sets)
    Xc, Y, LX, LY = [], [], [], []
    for d, P1 in zip(sets, oof):
        Xn = models.norm(d["X"])
        Xc.append(models.context(Xn, d["times"], P1)); Y.append(group_labels(d))
        r, y = lane_rows(d, Xn, P1); LX += r; LY += y
    Xc = np.vstack(Xc); Y = np.vstack(Y)
    m.ctx = [models.hgb().fit(Xc, Y[:, k]) for k in range(len(models.GROUPS))]
    m.lane = models.hgb(max_iter=150).fit(np.array(LX), np.array(LY))
    log(f"trained on {len(sets)} sets, {len(Y)} candidates, {len(LY)} cymbal hits")
    return m
