"""Bar lines and time signatures from the drum pattern.

The beat grid is split into 8th-note pulses. Bars are chosen by dynamic programming over pulse
positions: a bar may be 4 to 18 eighths long (2/4 up to 9/4, odd lengths are x/8). A bar scores well
when it starts with a kick or crash, when its pattern repeats the previous bar's, and when it keeps the
previous bar's length; 4/4 gets a small prior. 29/8 comes out as bars of 7, 7, 7, 8."""
import numpy as np

LENGTHS = list(range(4, 19))


def pulse_features(beats, hits, tol=0.035):
    """8th-note pulse times and per-pulse kick/snare/cymbal/crash/tom strength (max confidence nearby)."""
    beats = np.asarray(beats)
    pulses = np.sort(np.r_[beats, (beats[1:] + beats[:-1]) / 2])
    F = np.zeros((len(pulses), 5))
    col = {"kick": 0, "snare": 1, "hihat": 2, "ride": 2, "crash": 3, "tom1": 4, "tom2": 4, "tom3": 4}
    for h in hits:
        j = np.searchsorted(pulses, h["t"])
        for jj in (j - 1, j):
            if 0 <= jj < len(pulses) and abs(pulses[jj] - h["t"]) < tol:
                c = col.get(h["cls"])
                if c is not None:
                    F[jj, c] = max(F[jj, c], h["conf"])
                    if h["cls"] == "crash":
                        F[jj, 2] = max(F[jj, 2], h["conf"])
    return pulses, F


CTX = 8


def context_rows(F):
    """Per pulse: the drum features of the 8 pulses before and after (17 x 5), for the downbeat model."""
    n = len(F)
    pad = np.vstack([np.zeros((CTX, F.shape[1])), F, np.zeros((CTX, F.shape[1]))])
    return np.stack([pad[j:j + 2 * CTX + 1].ravel() for j in range(n)])


def downbeat_logodds(model, F):
    p = model.predict_proba(context_rows(F))[:, 1]
    p = np.clip(p, 0.02, 0.98)
    return np.log(p / (1 - p))


def main_length(F, lengths=LENGTHS, min_bars=6, margin=0.08):
    """The song's main bar length in 8ths, from how strongly the drum pattern repeats at each lag.
    For each lag L: mean similarity between the pattern at pulse j and at j+L, using windows of L pulses.
    A pattern that repeats every L also repeats every 2L, so a lag only wins over its own half when it
    is clearly better; ties go to the shorter length. Returns (L, scores)."""
    pat = F[:, [0, 1, 2, 3, 4]]
    n = len(pat)
    sc = {}
    for L in range(2, 37):
        if n < (min_bars + 1) * L:
            continue
        a, b = pat[:-L], pat[L:]
        num = (a * b).sum(1); den = np.linalg.norm(a, axis=1) * np.linalg.norm(b, axis=1) + 1e-6
        on = (np.linalg.norm(a, axis=1) > 0) | (np.linalg.norm(b, axis=1) > 0)
        sc[L] = float((num / den)[on].mean()) if on.any() else 0.0
    if not sc:
        return 8, sc
    # strongest repetition period, then the shortest divisor of it that repeats nearly as well
    # (a 4/4 song repeats every bar and every two bars; Glass Bead Game's 5/8 repeats every 5 and 10)
    P = max((k for k in sc if 4 <= k <= 20), key=lambda k: sc[k])
    while P < 6:
        P *= 2
    best = P
    for d in range(5, P + 1):
        # very short bars (5/8) must repeat almost as well as the full period to win
        if P % d == 0 and d in sc and sc[d] >= sc[P] - (margin if d >= 6 else 0.02):
            best = d
            break
    while best > 14 and best % 2 == 0:  # two-bar phrases -> one bar
        best //= 2
    return best, sc


def segment(F, down=None, w_down=1.0, w_rep=1.0, change=4.0, prior44=1.0, w_inside=1.0, long_cost=2.0, main=8, prior_main=1.0):
    """Returns bar start pulse indices and bar lengths (in 8ths). `down`: per-pulse log-odds that the
    pulse is beat 1 (from downbeat_logodds); without it a kick/crash heuristic is used."""
    n = len(F)
    if down is None:
        down = F[:, 0] + 1.5 * F[:, 3] - 0.3 * F[:, 1]
        down = down - down.mean()
    # pattern vector per pulse for repetition: kick, snare, cymbal, tom
    pat = F[:, [0, 1, 2, 4]]

    def rep(j, L):
        if j - L < 0 or j + L > n:
            return 0.0
        a, b = pat[j:j + L].ravel(), pat[j - L:j].ravel()
        na, nb = np.linalg.norm(a), np.linalg.norm(b)
        if na < 1e-6 or nb < 1e-6:
            return 0.0
        return float(a @ b / (na * nb))

    NEG = -1e18
    nl = len(LENGTHS)
    best = np.full((n + 1, nl), NEG)   # best[j, l]: a bar of length LENGTHS[l] ends at pulse j
    back = np.full((n + 1, nl), -1, int)
    start_best = np.full(n + 1, NEG)   # best score of any segmentation ending exactly at j
    # the first bar may start at any of the first 18 pulses (pickup)
    for j0 in range(min(18, n)):
        start_best[j0] = 0.0
    for j in range(n):
        if start_best[j] == NEG and best[j].max() == NEG:
            continue
        for li, L in enumerate(LENGTHS):
            e = j + L
            if e > n:
                break
            # repetition counts per 8th note, measured against a typical cross-bar similarity
            # a bar that swallows a likely beat 1 in its middle pays for it (stops 8/4 instead of 2 x 4/4)
            inside = np.clip(down[j + 1:e], 0, None).sum()
            gain = w_down * down[j] - w_inside * inside + w_rep * rep(j, L) * L / 8 + (prior44 if L == 8 else 0.0) + ((prior_main if prior_main is not None else 0.0) if (L == main and main != 8) else 0.0) - (long_cost if L > max(12, main) else 0.0)
            # from a fresh start
            if start_best[j] > NEG:
                v = start_best[j] + gain
                if v > best[e, li]:
                    best[e, li], back[e, li] = v, -2 - j  # encode "started at j"
            # from a previous bar of length LENGTHS[pl] ending at j
            for pl in range(nl):
                if best[j, pl] == NEG:
                    continue
                v = best[j, pl] + gain - (0.0 if pl == li else change)
                if v > best[e, li]:
                    best[e, li], back[e, li] = v, pl
    # end anywhere in the last 18 pulses
    ends = [(best[e, li], e, li) for e in range(max(0, n - 18), n + 1) for li in range(nl) if best[e, li] > NEG]
    if not ends:
        return [], []
    _, e, li = max(ends)
    starts, lens = [], []
    while True:
        L = LENGTHS[li]; j = e - L
        starts.append(j); lens.append(L)
        b = back[e, li]
        if b <= -2:
            break
        e, li = j, b
    return starts[::-1], lens[::-1]


def time_signature(L):
    """Bar length in 8ths -> (num, den): even lengths as x/4, odd as x/8."""
    return (L // 2, 4) if L % 2 == 0 else (L, 8)


DOWNBEAT_ROWS = __import__("os").path.join(__import__("os").path.dirname(__file__), "downbeat_rows.npz")


def downbeat_model(path=DOWNBEAT_ROWS):
    """Fit the beat-1 classifier from saved rows (reference charts' bar lines vs held-out detections)."""
    from .models import hgb
    d = np.load(path)
    return hgb(max_iter=150).fit(d["X"], d["Y"])


def bars(beats, hits, model):
    """Bar list [(pulse_index, length_in_8ths)] and the pulse times."""
    pulses, F = pulse_features(beats, hits)
    main, _ = main_length(F)
    starts, lens = segment(F, down=downbeat_logodds(model, F), main=main)
    return pulses, list(zip(starts, lens)), main
