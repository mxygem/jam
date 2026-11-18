"""Trained models: hit detectors per group, cymbal lane chooser, difficulty reducer.
All are gradient-boosted trees trained on reference charts (see tools/train.py)."""
import os, pickle, numpy as np
from sklearn.ensemble import HistGradientBoostingClassifier

GROUPS = [("kick", ["kick"]), ("snare", ["snare"]), ("cym", ["hihat", "ride", "crash"]), ("tom", ["tom"])]
CYM_LANES = ["hihat", "ride", "crash"]
MODEL_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "model.pkl")


def hgb(**kw):
    p = dict(max_iter=200, learning_rate=0.08, max_leaf_nodes=31, l2_regularization=1.0, random_state=0)
    p.update(kw)
    return HistGradientBoostingClassifier(**p)


def norm(X):
    """Raw features plus the same features standardized within the song (mixes differ)."""
    mu = X.mean(0); sd = X.std(0) + 1e-6
    return np.hstack([X, (X - mu) / sd])


def context(Xn, times, P, templates=True):
    """Second-stage features: own probabilities plus the strongest same-group probabilities about
    one and two typical gaps before/after (captures steady patterns like 8th-note hats)."""
    cols = [P]
    n = len(times)
    for dt in (0.1, 0.2, 0.4):
        for sgn in (-1, 1):
            tgt = times + sgn * dt
            lo = np.searchsorted(times, tgt - 0.04); hi = np.searchsorted(times, tgt + 0.04)
            M = np.zeros_like(P)
            for k in range(P.shape[1]):
                # max over [lo, hi) via a sparse table would be overkill: windows hold 0-3 candidates
                col = P[:, k]
                for off in range(4):
                    j = lo + off
                    ok = j < hi
                    M[ok, k] = np.maximum(M[ok, k], col[np.minimum(j[ok], n - 1)])
            cols.append(M)
    if templates:
        cols.append(song_templates(Xn, P))
    return np.hstack([Xn] + cols).astype(np.float32)


TIMBRE = slice(17, 49)   # rise over the pre-onset level, first two spans (0-30 ms), 16 bands each


def song_templates(Xn, P, conf=0.7):
    """How much each candidate sounds like this song's own confident hits of each group.
    Mixes and dynamics differ: a quiet verse snare under a hi-hat can look like a hat next to other
    songs' snares, but still matches this song's snare in its low-mid body. Per group: cosine
    similarity of the candidate's attack rise to the group's mean, and its size relative to it."""
    V = Xn[:, TIMBRE]
    out = []
    for k in range(P.shape[1]):
        m = P[:, k] > conf
        if m.sum() < 8:
            out += [np.zeros(len(V)), np.zeros(len(V))]; continue
        tpl = V[m].mean(0)
        nt = np.linalg.norm(tpl) + 1e-6
        nv = np.linalg.norm(V, axis=1) + 1e-6
        out.append(V @ tpl / (nv * nt))
        out.append((V @ tpl) / (nt * nt))
    return np.stack(out, 1)


def cym_features(Xn, times, P, i):
    """Features for picking a cymbal lane: the hit's own sound, how long it rings, how regular the
    cymbal pattern around it is, and whether a kick lands with it. `times` must be sorted."""
    t = times[i]
    lo, hi = np.searchsorted(times, [t - 1.0, t + 1.0])
    seg = times[lo:hi][P[lo:hi, 2] > 0.4]
    n_near = len(seg)
    gaps = np.diff(seg) if n_near > 2 else np.array([1.0])
    return np.r_[Xn[i], P[i], n_near, np.median(gaps), gaps.std()]


class Model:
    def __init__(self):
        self.det = []      # stage-1 detectors per group
        self.ctx = []      # stage-2 detectors using neighbours
        self.lane = None   # cymbal lane classifier
        self.reduce = {}   # diff -> keep classifier

    def predict(self, X, times):
        Xn = norm(X)
        P1 = np.stack([m.predict_proba(Xn)[:, 1] for m in self.det], 1)
        Xc = context(Xn, times, P1, getattr(self, "templates", False))
        P = np.stack([m.predict_proba(Xc)[:, 1] for m in self.ctx], 1)
        return Xn, P

    def save(self, path=MODEL_PATH):
        pickle.dump(self, open(path, "wb"))

    @staticmethod
    def load(path=MODEL_PATH):
        return pickle.load(open(path, "rb"))
