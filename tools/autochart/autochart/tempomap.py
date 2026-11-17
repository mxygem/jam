"""Beat grid from classified drum hits.

Steps: hit envelope -> tempo per window (comb score over 8th-note grids) -> dynamic-programming beat
tracking on the envelope with that local period -> each beat nudged to a strong hit within 25 ms ->
downbeats from kick/snare/crash placement. Tempo octave is folded into [fold_lo, fold_lo*2).
"""
import numpy as np

FPS = 200


def envelope(hits, end, sigma=0.008):
    n = int(end * FPS) + 1
    env = np.zeros(n)
    for t, w in hits:
        i = int(round(t * FPS))
        if 0 <= i < n:
            env[i] += w
    k = np.exp(-0.5 * (np.arange(-4, 5) / (sigma * FPS)) ** 2)
    return np.convolve(env, k, "same")


def comb_score(ts, ws, bpm):
    """How well weighted hits sit on an 8th grid at this tempo, beyond chance (Rayleigh resultant)."""
    ph = 2 * np.pi * ts / (30.0 / bpm)
    return np.abs((ws * np.exp(1j * ph)).sum()) / (ws.sum() + 1e-9)


def local_tempo(hits, end, win=12.0, hop=3.0, lo=60, hi=240, fold_lo=90):
    ts = np.array([t for t, _ in hits]); ws = np.array([w for _, w in hits])
    bpms = np.arange(lo, hi, 0.25)
    centers, best, conf = [], [], []
    for c in np.arange(win / 2, max(end - win / 2, win / 2) + 1e-9, hop):
        m = (ts > c - win / 2) & (ts < c + win / 2)
        if m.sum() < 8:
            centers.append(c); best.append(np.nan); conf.append(0); continue
        sc = np.array([comb_score(ts[m], ws[m], b) for b in bpms])
        # every tempo's score includes its own octave multiples; prefer the slowest tempo whose
        # score is within 8% of the best (finer grids always fit at least as well)
        top = sc.max()
        ok = np.nonzero(sc >= 0.92 * top)[0]
        b = bpms[ok[0]]
        # refine around b
        fine = np.arange(b - 0.5, b + 0.5, 0.02)
        b = fine[np.argmax([comb_score(ts[m], ws[m], x) for x in fine])]
        centers.append(c); best.append(b); conf.append(top)
    best = np.array(best)
    # fold octave
    with np.errstate(invalid="ignore"):
        while np.any(best < fold_lo): best = np.where(best < fold_lo, best * 2, best)
        while np.any(best >= fold_lo * 2): best = np.where(best >= fold_lo * 2, best / 2, best)
    # fill gaps with neighbours, then median-smooth
    idx = np.arange(len(best)); good = ~np.isnan(best)
    if good.sum() == 0:
        best[:] = 120
    else:
        best = np.interp(idx, idx[good], best[good])
    sm = np.array([np.median(best[max(0, i - 2):i + 3]) for i in range(len(best))])
    return np.array(centers), sm, np.array(conf)


def track(hits, end, fold_lo=90, tightness=100.0):
    env = envelope(hits, end)
    centers, bpm, conf = local_tempo(hits, end, fold_lo=fold_lo)
    n = len(env)
    period = np.interp(np.arange(n) / FPS, centers, 60.0 / bpm) * FPS  # frames per beat
    # DP (Ellis 2007) with a time-varying period
    score = env.copy(); back = -np.ones(n, int)
    for i in range(n):
        P = period[i]
        lo, hi = int(i - 1.5 * P), int(i - 0.66 * P)
        if hi <= 0:
            continue
        lo = max(lo, 0)
        prev = np.arange(lo, hi)
        pen = -tightness * np.log((i - prev) / P) ** 2
        j = np.argmax(score[prev] + pen)
        if score[prev[j]] + pen[j] > 0:
            score[i] = env[i] + score[prev[j]] + pen[j]; back[i] = prev[j]
    # best end point in the last beat
    last = n - 1 - int(period[-1])
    i = last + np.argmax(score[last:])
    beats = []
    while i >= 0:
        beats.append(i); i = back[i]
    beats = np.array(beats[::-1]) / FPS
    return beats, (centers, bpm, conf)


def refine(beats, hits, tol=0.025):
    """Move each beat to the strongest hit within tol, then smooth jitter with neighbours."""
    ts = np.array([t for t, _ in hits]); ws = np.array([w for _, w in hits])
    out = beats.copy()
    for i, b in enumerate(beats):
        m = np.abs(ts - b) < tol
        if m.any() and ws[m].max() >= 0.5:
            j = np.nonzero(m)[0][np.argmax(ws[m])]
            out[i] = ts[j]
    # light smoothing: average with the straight line through neighbours (keeps live drift, removes flams)
    sm = out.copy()
    for i in range(1, len(out) - 1):
        sm[i] = 0.5 * out[i] + 0.25 * (out[i - 1] + out[i + 1])
    return sm


def downbeats(beats, kick, snare, crash, meter=4):
    """Pick the bar phase: kicks and crashes on beat 1, snares on 2 and 4."""
    def near(ts, b):
        if len(ts) == 0: return 0.0
        j = np.searchsorted(ts, b); d = min(abs(ts[min(j, len(ts) - 1)] - b), abs(ts[max(j - 1, 0)] - b))
        return 1.0 if d < 0.035 else 0.0
    feat = np.array([[near(kick, b), near(snare, b), near(crash, b)] for b in beats])
    best, bp = -1e9, 0
    for ph in range(meter):
        pos = (np.arange(len(beats)) - ph) % meter
        s = (feat[pos == 0, 0].sum() + 2 * feat[pos == 0, 2].sum() - feat[pos == 0, 1].sum()
             + feat[(pos == 1) | (pos == 3), 1].sum() - 0.5 * feat[(pos == 1) | (pos == 3), 0].sum())
        if s > best: best, bp = s, ph
    return bp


def fix_half_beat(beats, snare_hits, win=16, tol=0.035):
    """Where snares sit between beats instead of on them, move that stretch by half a beat.
    snare_hits: (time, prob) pairs. Returns new beats and the number of beats moved."""
    if len(beats) < 4:
        return beats, 0
    st = np.array([t for t, _ in snare_hits]); sw = np.array([w for _, w in snare_hits])
    mids = (beats[1:] + beats[:-1]) / 2

    def mass(points):
        out = np.zeros(len(points))
        if len(st) == 0: return out
        for i, p in enumerate(points):
            m = np.abs(st - p) < tol
            out[i] = sw[m].max() if m.any() else 0
        return out
    on, off = mass(beats[:-1]), mass(mids)
    n = len(mids)
    flip = np.zeros(n, bool)
    for i in range(n):
        a, b = max(0, i - win // 2), min(n, i + win // 2)
        flip[i] = off[a:b].sum() > 1.6 * on[a:b].sum() + 1.0
    if not flip.any():
        return beats, 0
    pts = [mids[i] if flip[i] else beats[i] for i in range(n)] + [beats[-1]]
    pts = np.array(sorted(pts))
    # repair the seams: drop beats that came too close, fill gaps that opened up
    P = np.median(np.diff(beats))
    out = [pts[0]]
    for p in pts[1:]:
        g = p - out[-1]
        if g < 0.7 * P:
            continue
        if g > 1.3 * P:
            out.append(out[-1] + g / 2)
        out.append(p)
    return np.array(out), int(flip.sum())


def fix_phase(beats, kick, snare, cym, win=8, switch_cost=6.0, tol=0.035):
    """Choose per stretch of `win` beats between the tracked beats and the half-beat-shifted grid.
    Evidence for a grid = kicks + snares + 0.5*cymbals landing on it (probability-weighted). A two-state
    Viterbi with a switching cost keeps the phase steady through ambiguous passages.
    kick/snare/cym: (time, prob) lists. Returns beats, number of beats moved."""
    if len(beats) < 2 * win:
        return beats, 0
    mids = (beats[1:] + beats[:-1]) / 2
    on_pts, off_pts = beats[:-1], mids

    def mass(points, hits, w):
        if not hits: return np.zeros(len(points))
        ht = np.array([t for t, _ in hits]); hw = np.array([p for _, p in hits])
        out = np.zeros(len(points))
        j = np.searchsorted(ht, points)
        for k in (-1, 0):
            jj = np.clip(j + k, 0, len(ht) - 1)
            close = np.abs(ht[jj] - points) < tol
            out = np.maximum(out, np.where(close, hw[jj], 0))
        return w * out
    ev_on = mass(on_pts, kick, 1.0) + mass(on_pts, snare, 1.0) + mass(on_pts, cym, 0.5)
    ev_off = mass(off_pts, kick, 1.0) + mass(off_pts, snare, 1.0) + mass(off_pts, cym, 0.5)
    nwin = len(on_pts) // win
    E = np.array([[ev_on[i * win:(i + 1) * win].sum(), ev_off[i * win:(i + 1) * win].sum()] for i in range(nwin)])
    # Viterbi, prefer 'on' at the start
    cost = np.array([0.0, switch_cost]); back = np.zeros((nwin, 2), int)
    for i in range(nwin):
        new = np.zeros(2)
        for s in range(2):
            stay, sw = cost[s], cost[1 - s] + switch_cost
            back[i, s] = s if stay <= sw else 1 - s
            new[s] = min(stay, sw) - E[i, s]
        cost = new
    st = np.zeros(nwin, int); st[-1] = int(np.argmin(cost))
    for i in range(nwin - 1, 0, -1):
        st[i - 1] = back[i, st[i]]
    flip = np.zeros(len(on_pts), bool)
    for i in range(nwin):
        flip[i * win:(i + 1) * win] = st[i] == 1
    if nwin * win < len(on_pts):
        flip[nwin * win:] = st[-1] == 1
    if not flip.any():
        return beats, 0
    pts = np.array(sorted([mids[i] if flip[i] else beats[i] for i in range(len(mids))] + [beats[-1]]))
    P = np.median(np.diff(beats))
    out = [pts[0]]
    for p in pts[1:]:
        g = p - out[-1]
        if g < 0.7 * P:
            continue
        if g > 1.3 * P:
            out.append(out[-1] + g / 2)
        out.append(p)
    return np.array(out), int(flip.sum())
