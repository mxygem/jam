"""Difficulty reduction learned from human charts: for each Expert note, predict whether it stays in
Hard / Medium / Easy. Features describe the note's place in the rhythm and how busy its surroundings are."""
import numpy as np

KINDS = ["kick", "snare", "cym", "tom"]
def kind(c):
    if c in ("kick", "kick2x"): return "kick"
    if c == "snare": return "snare"
    if c in ("hihat", "ride", "crash"): return "cym"
    return "tom"


def features(notes, tpb):
    """notes: list of (tick, cls). Returns one row per note."""
    ticks = np.array([t for t, _ in notes]); ks = [kind(c) for _, c in notes]
    rows = []
    for i, (tk, c) in enumerate(notes):
        k = ks[i]
        pos = (tk % tpb) / tpb
        same = ticks[[j for j in range(len(notes)) if ks[j] == k]]
        j = np.searchsorted(same, tk)
        prev_gap = (tk - same[j - 1]) / tpb if j > 0 else 8.0
        nxt = same[j + 1] if j + 1 < len(same) else None
        next_gap = (nxt - tk) / tpb if nxt is not None else 8.0
        lo, hi = np.searchsorted(ticks, [tk - 2 * tpb, tk + 2 * tpb])
        dens = (hi - lo) / 4.0
        chord = int((ticks == tk).sum())
        on16 = float(abs(pos * 4 - round(pos * 4)) < 1e-6); on8 = float(abs(pos * 2 - round(pos * 2)) < 1e-6)
        beat_in_bar = (tk // tpb) % 4
        others_here = [ks[q] for q in range(len(notes)) if ticks[q] == tk and q != i]
        rows.append([KINDS.index(k), pos, on8, on16, float(pos == 0), prev_gap, next_gap, min(prev_gap, next_gap), dens, chord,
                     beat_in_bar, float("kick" in others_here), float("snare" in others_here), float("cym" in others_here),
                     float(c == "crash"), float(c == "ride"), float(c == "hihat"), float(c == "kick2x")])
    return np.array(rows, np.float32)


def labels(expert, lower):
    """Is this expert note (tick, class) also in the lower difficulty at the same tick with the same kind."""
    have = {(t, kind(c)) for t, c in lower}
    return np.array([(t, kind(c)) in have for t, c in expert], np.int8)
