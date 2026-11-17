"""Onset candidates and per-candidate features for drum transcription."""
import numpy as np, librosa, soundfile as sf
from scipy.signal import find_peaks

SR = 44100
HOP = 220            # ~5 ms frames
N_FFT = 2048
N_MELS = 64


def load_mono(path):
    y, sr = sf.read(path, dtype="float32", always_2d=True)
    y = y.mean(1)
    if sr != SR:
        y = librosa.resample(y, orig_sr=sr, target_sr=SR)
    return y


def logmel(y):
    S = librosa.feature.melspectrogram(y=y, sr=SR, n_fft=N_FFT, hop_length=HOP, n_mels=N_MELS,
                                       fmin=30, fmax=18000, power=2.0)
    return np.log1p(S * 100.0).astype(np.float32)  # (mels, frames)


def candidates(M, min_gap_s=0.028, delta=0.04):
    """Very sensitive onset peaks: positive flux summed over three band groups, any group may fire."""
    D = np.maximum(0, np.diff(M, axis=1, prepend=M[:, :1]))
    # max-filter over freq (superflux style) reduces vibrato/ringing false peaks
    groups = [D[:12].sum(0), D[12:36].sum(0), D[36:].sum(0)]
    peaks = set()
    for g in groups:
        g = g / (np.percentile(g, 99.5) + 1e-9)
        p, _ = find_peaks(g, height=delta, distance=max(1, int(min_gap_s * SR / HOP)))
        peaks.update(p.tolist())
    total = D.sum(0)
    peaks = sorted(peaks)
    # merge peaks closer than min gap, keep the one with the larger total flux
    out = []
    gap = int(min_gap_s * SR / HOP)
    for p in peaks:
        if out and p - out[-1] < gap:
            if total[p] > total[out[-1]]:
                out[-1] = p
            continue
        out.append(p)
    return np.array(out, int), D


SPANS = [(0, 2), (2, 6), (6, 16), (16, 40), (40, 80)]  # frames after onset: 0-10ms .. 200-400ms


def feats(M, D, peaks):
    """Feature rows per candidate: flux at the attack, level and decay over several spans, context."""
    n = M.shape[1]
    # 16 coarse bands for compactness
    edges = np.linspace(0, N_MELS, 17).astype(int)
    Mb = np.stack([M[a:b].mean(0) for a, b in zip(edges[:-1], edges[1:])])
    Db = np.stack([D[a:b].sum(0) for a, b in zip(edges[:-1], edges[1:])])
    loud = M.mean(0)
    # running reference level: 90th pct over +-5 s
    win = int(5 * SR / HOP)
    ref = np.array([np.percentile(loud[max(0, i - win):i + win:4], 90) for i in range(0, n, 50)])
    ref = np.interp(np.arange(n), np.arange(0, n, 50), ref)
    # 8 bands of flux, each divided by the 95th percentile of that band's flux at candidates within +-4 s
    e8 = np.linspace(0, N_MELS, 9).astype(int)
    D8 = np.stack([D[a:b].sum(0) for a, b in zip(e8[:-1], e8[1:])])
    D8 = np.stack([D8[:, p:p + 3].sum(1) for p in peaks], 1) if len(peaks) else np.zeros((8, 0))
    locD = np.zeros((8, n), np.float32)
    w4 = int(4 * SR / HOP)
    for i, p in enumerate(peaks):
        lo, hi = np.searchsorted(peaks, [p - w4, p + w4])
        ref95 = np.percentile(D8[:, lo:hi], 95, axis=1) + 1e-6
        locD[:, p] = np.log1p(D8[:, i] / ref95)
    totD = D.sum(0)
    tref = np.percentile(totD[peaks], 90) if len(peaks) else 1.0
    rows = []
    for i, p in enumerate(peaks):
        r = []
        flux = Db[:, p:p + 3].sum(1)
        r += list(flux / (flux.sum() + 1e-6))            # spectral shape of the attack
        r += [np.log1p(totD[p:p + 3].sum() / tref)]      # attack strength vs song
        pre = Mb[:, max(0, p - 4):max(1, p - 1)].mean(1)
        for a, b in SPANS:
            seg = Mb[:, min(n - 1, p + a):min(n, p + b)]
            seg = seg.mean(1) if seg.shape[1] else Mb[:, -1]
            r += list(seg - pre)                          # rise over pre-onset level, per band
        r += list(Mb[:, p:p + 6].mean(1) - ref[p])         # absolute level vs song level
        r += list(locD[:, p])                              # band flux vs the band's own recent hits
        r += list(locD[:, p] - (locD[:, peaks[i - 1]] if i else 0))
        prev_gap = (p - peaks[i - 1]) * HOP / SR if i else 1.0
        next_gap = (peaks[i + 1] - p) * HOP / SR if i + 1 < len(peaks) else 1.0
        r += [min(prev_gap, 1.0), min(next_gap, 1.0)]
        rows.append(r)
    return np.array(rows, np.float32)


def attack_time(y, t, search=0.03):
    """Refine an onset to the waveform attack start: first sample reaching 25% of the local peak envelope."""
    a = int(max(0, (t - search) * SR)); b = int(min(len(y), (t + search) * SR))
    seg = np.abs(y[a:b])
    if len(seg) < 10:
        return t
    env = np.convolve(seg, np.ones(32) / 32, mode="same")
    pk = env.argmax()
    base = np.median(env[:max(1, pk // 3)])
    thr = base + 0.25 * (env[pk] - base)
    idx = np.nonzero(env[:pk + 1] >= thr)[0]
    return (a + (idx[0] if len(idx) else pk)) / SR


def analyze(path):
    y = load_mono(path)
    M = logmel(y)
    peaks, D = candidates(M)
    X = feats(M, D, peaks)
    times = peaks * HOP / SR
    return y, times, X
