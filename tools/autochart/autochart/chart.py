"""Turn detected hits + beats into a tick grid, notes per difficulty, and write .chart / chart.jam.json."""
import json, os, numpy as np

RES = 192
LANE = {"kick": 0, "snare": 1, "hihat": 2, "tom1": 2, "ride": 3, "tom2": 3, "crash": 4, "tom3": 4}
CYM = {"hihat", "ride", "crash"}


class Grid:
    """Beats (seconds) -> ticks. Tick 0 is time 0; the first beat interval is stretched from 0."""
    def __init__(self, beats):
        b = np.asarray(beats, float)
        P = b[1] - b[0]
        while b[0] - P > 0:
            b = np.r_[b[0] - P, b]
        self.beats = np.r_[0.0, b] if b[0] > 1e-3 else b
        self.ticks = np.arange(len(self.beats)) * RES
        self.pickup = 0  # beats before the first downbeat (written as a short first bar)
        self.bars = None  # [(tick, num, den)] from meter detection; None = 4/4 from the pickup

    def set_downbeat(self, idx):
        self.pickup = int(idx) % 4

    def set_bars(self, bars):
        """bars: [(tick, num, den)] in order. A gap before the first bar becomes a pickup bar."""
        out = []
        if bars and bars[0][0] > 0:
            # intro before the first detected bar: a short pickup bar, then 4/4 bars
            eighths = int(round(bars[0][0] / (RES / 2)))
            rem, tick = eighths % 8, 0
            if rem:
                out.append((0, rem // 2, 4) if rem % 2 == 0 else (0, rem, 8)); tick = rem * RES // 2
            while tick < bars[0][0]:
                out.append((tick, 4, 4)); tick += 4 * RES
        self.bars = out + list(bars)

    def time_sigs(self, ts=4):
        """TS events (tick, num, den), only where the signature changes."""
        if self.bars is None:
            return ([(0, self.pickup, 4)] if self.pickup else []) + [(self.pickup * RES, ts, 4)]
        out = []
        for tick, num, den in self.bars:
            if not out or (out[-1][1], out[-1][2]) != (num, den):
                out.append((int(tick), num, den))
        return out

    def tick(self, t):
        return float(np.interp(t, self.beats, self.ticks, right=self.ticks[-1] + (t - self.beats[-1]) / (self.beats[-1] - self.beats[-2]) * RES))

    def sec(self, tick):
        return float(np.interp(tick, self.ticks, self.beats))

    def tempos(self):
        out = []
        for i in range(len(self.beats) - 1):
            bpm = 60.0 / (self.beats[i + 1] - self.beats[i])
            if not out or abs(out[-1][1] - bpm) > 0.005:
                out.append((int(self.ticks[i]), round(bpm, 3)))
        return out


def snap(grid, t, tol=0.035):
    """Nearest 16th or 8th-triplet within tol; else keep the raw tick. Returns tick, kind, error s."""
    raw = grid.tick(t)
    best = None
    for div, kind in ((4, "q16"), (3, "q12"), (6, "q24")):
        step = RES / div
        k = round(raw / step) * step
        err = abs(grid.sec(k) - t)
        pref = {"q16": 0, "q12": 0.004, "q24": 0.008}[kind]  # prefer straight 16ths on near ties
        if best is None or err + pref < best[2] + best[3]:
            best = (int(round(k)), kind, err, pref)
    if best[2] <= tol:
        return best[0], best[1], best[2]
    return int(round(raw)), "raw", 0.0


def build_expert(grid, hits):
    """hits: list of dicts {t, cls, conf, src}. Snaps, merges duplicates on the same tick and lane."""
    notes = {}
    for h in hits:
        tk, kind, err = snap(grid, h["t"])
        lane = LANE[h["cls"]]
        key = (tk, lane)
        n = dict(t=tk, lane=lane, len=0, cymbal=h["cls"] in CYM, accent=False, ghost=False, hopo=None, tap=False,
                 conf=round(float(h["conf"]), 3), src=f"{h['src']} {kind} err={err*1000:.0f}ms", vel=1.0, alt=[], cls=h["cls"])
        if key not in notes or notes[key]["conf"] < n["conf"]:
            notes[key] = n
    # a lane can't be both cymbal and tom on one tick: keep the more confident
    return sorted(notes.values(), key=lambda n: (n["t"], n["lane"]))


def write_chart(path, meta, grid, diffs, sections, ts=4):
    L = ["[Song]", "{", f'  Name = "{meta["title"]}"', f'  Artist = "{meta["artist"]}"', '  Charter = "jam autochart"',
         f"  Resolution = {RES}", "  Offset = 0", f'  MusicStream = "{meta["audio"].get("song", "song.ogg")}"', f'  DrumStream = "{meta["audio"].get("drums", "drums.ogg")}"', "}", "[SyncTrack]", "{"]
    ev = [(tk, f"TS {n}" if d == 4 else f"TS {n} {d.bit_length() - 1}") for tk, n, d in grid.time_sigs(ts)] + [(tk, f"B {int(round(bpm * 1000))}") for tk, bpm in grid.tempos()]
    L += [f"  {tk} = {e}" for tk, e in sorted(ev, key=lambda x: (x[0], x[1][0] != "T"))]
    L += ["}", "[Events]", "{"] + [f'  {tk} = E "section {name}"' for tk, name in sections] + ["}"]
    for diff, notes in diffs.items():
        L += [f"[{diff.capitalize()}Drums]", "{"]
        for n in notes:
            L.append(f"  {n['t']} = N {n['lane']} 0")
            if n["cymbal"] and n["lane"] in (2, 3, 4):
                L.append(f"  {n['t']} = N {64 + n['lane']} 0")
        L += ["}"]
    open(path, "w", encoding="utf-8").write("\n".join(L) + "\n")


def write_project(path, meta, grid, diffs, sections, analysis, log, ts=4):
    clean = lambda ns: [{k: v for k, v in n.items() if k != "cls"} for n in ns]
    doc = {
        "format": "jam-project/1",
        "song": meta,
        "grid": {"resolution": RES, "tempos": [{"tick": t, "bpm": b} for t, b in grid.tempos()],
                 "time_signatures": [{"tick": tk, "num": n, "den": d} for tk, n, d in grid.time_sigs(ts)], "offset": 0.0},
        "sections": [{"tick": t, "name": n, "conf": 0.5} for t, n in sections],
        "parts": {"drums": {"instrument": "drums", "mode": "pro", "difficulties": {
            d: {"notes": clean(ns), "star_power": [], "solos": [], "fills": []} for d, ns in diffs.items()}}},
        "vocals": {"parts": []},
        "analysis": analysis,
        "log": log,
    }
    json.dump(doc, open(path, "w", encoding="utf-8"), indent=0)
