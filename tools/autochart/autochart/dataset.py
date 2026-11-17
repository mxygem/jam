"""Build labelled candidate sets from reference songs (for training and leave-one-song-out tests)."""
import os, numpy as np, pickle
from . import features, refmidi

CLASSES = ["kick", "snare", "hihat", "ride", "crash", "tom"]
REF_ROOT = os.environ.get("AUTOCHART_REF", r"C:\Users\gemini\Games\jam\autochart-reference" if os.name == "nt" else "/mnt/user-data/uploads/autochart-reference")
DATA = os.environ.get("AUTOCHART_DATA", os.path.expanduser("~/.cache/autochart/refdata"))


def ref_notes(song):
    """Reference chart for a song: library songs carry ref.pkl (from tools/libprep.py), the reference set
    is read from REF_ROOT."""
    lib = os.path.join(DATA, song, "ref.pkl")
    if os.path.exists(lib):
        return pickle.load(open(lib, "rb"))
    ini = open(os.path.join(REF_ROOT, song, "song.ini"), encoding="utf-8", errors="ignore").read().lower()
    five = "five_lane_drums = 1" in ini
    r = refmidi.load(os.path.join(REF_ROOT, song, "notes.mid"), five_lane=five or None)
    return r


def canon(c):
    if c == "kick2x": return "kick"
    if c.startswith("tom"): return "tom"
    return c


def estimate_offset(times, ref_times):
    """Median signed distance (cand - ref) of each ref hit to its nearest candidate within 60 ms."""
    idx = np.clip(np.searchsorted(times, ref_times), 1, len(times) - 1)
    a, b = times[idx - 1], times[idx]
    near = np.where(np.abs(a - ref_times) < np.abs(b - ref_times), a, b)
    d = near - ref_times
    return float(np.median(d[np.abs(d) < 0.06]))


def build(song, which):
    cache = os.path.join(DATA, song, f"cands_{which}.pkl")
    if os.path.exists(cache):
        return pickle.load(open(cache, "rb"))
    y, times, X = features.analyze(os.path.join(DATA, song, f"drums_{which}.wav"))
    r = ref_notes(song)
    ex = [(t, canon(c)) for t, c, _ in r["drums"]["expert"]]
    rt = np.array(sorted(set(t for t, _ in ex)))
    off = estimate_offset(times, rt)
    Y = np.zeros((len(times), len(CLASSES)), np.int8)
    shifted = times - off
    for t, c in ex:
        j = np.argmin(np.abs(shifted - t))
        if abs(shifted[j] - t) < 0.04:
            Y[j, CLASSES.index(c)] = 1
    d = dict(song=song, which=which, times=times, X=X, Y=Y, offset=off, ref=ex,
             end=max(t for t, _, _ in r["drums"]["expert"]) + 1, rank=r.get("rank"), charter=r.get("charter"))
    pickle.dump(d, open(cache, "wb"))
    return d
