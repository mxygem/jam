"""Read human charts (.mid) into timed drum notes, tempo map and sections.

Drum classes used across autochart: kick, snare, hihat (yellow cym), ride (blue cym),
crash (green cym), tom1 (yellow tom), tom2 (blue tom), tom3 (green tom).
"""
import mido

DIFF_BASE = {"expert": 96, "hard": 84, "medium": 72, "easy": 60}
PRO_LANE = {0: "kick", 1: "snare", 2: "y", 3: "b", 4: "g"}
TOM_MARK = {110: "y", 111: "b", 112: "g"}
CYM = {"y": "hihat", "b": "ride", "g": "crash"}
TOM = {"y": "tom1", "b": "tom2", "g": "tom3"}
# GH 5-lane: kick, red, yellow cym, blue tom, orange cym, green tom
FIVE = {0: "kick", 1: "snare", 2: "hihat", 3: "tom2", 4: "crash", 5: "tom3"}


class TempoMap:
    def __init__(self, tpb, tempos):
        self.tpb = tpb
        self.pts = []  # (tick, sec, uspb)
        sec, last_t, last_us = 0.0, 0, 500000
        for t, us in sorted(tempos):
            sec += (t - last_t) / tpb * last_us / 1e6
            self.pts.append((t, sec, us))
            last_t, last_us = t, us
        if not self.pts or self.pts[0][0] != 0:
            self.pts.insert(0, (0, 0.0, 500000))

    def sec(self, tick):
        p = self.pts[0]
        for q in self.pts:
            if q[0] > tick:
                break
            p = q
        return p[1] + (tick - p[0]) / self.tpb * p[2] / 1e6


def load(path, five_lane=None):
    mid = mido.MidiFile(path, clip=True)
    tracks = {}
    tempos, sigs = [], []
    for tr in mid.tracks:
        name, t, evs = None, 0, []
        for m in tr:
            t += m.time
            if m.type == "track_name":
                name = m.name
            elif m.type == "set_tempo":
                tempos.append((t, m.tempo))
            elif m.type == "time_signature":
                sigs.append((t, m.numerator, m.denominator))
            evs.append((t, m))
        tracks[name or f"track{len(tracks)}"] = evs
    tm = TempoMap(mid.ticks_per_beat, tempos)
    out = {"tpb": mid.ticks_per_beat, "tempo": tm, "tempos": tempos, "sigs": sigs,
           "tracks": list(tracks)}
    drums = tracks.get("PART DRUMS")
    if drums:
        out["drums"] = parse_drums(drums, tm, five_lane)
    ev = tracks.get("EVENTS", [])
    out["sections"] = [(tm.sec(t), m.text) for t, m in ev
                       if m.type in ("text", "lyrics") and ("section" in m.text or m.text.startswith("[prc_"))]
    out["beats"] = [(tm.sec(t), m.note) for t, m in tracks.get("BEAT", [])
                    if m.type == "note_on" and m.velocity > 0]
    vox = tracks.get("PART VOCALS")
    if vox:
        out["lyrics"] = [(tm.sec(t), m.text) for t, m in vox
                         if m.type in ("lyrics", "text") and not m.text.startswith("[")]
    return out


def parse_drums(evs, tm, five_lane=None):
    ons = [(t, m.note, m.velocity) for t, m in evs if m.type == "note_on" and m.velocity > 0]
    if five_lane is None:
        five_lane = any(n == 101 for _, n, _ in ons)
    # tom marker spans
    spans = {k: [] for k in TOM_MARK}
    open_ = {}
    for t, m in evs:
        if m.type in ("note_on", "note_off") and m.note in TOM_MARK:
            if m.type == "note_on" and m.velocity > 0:
                open_[m.note] = t
            elif m.note in open_:
                spans[m.note].append((open_.pop(m.note), t))

    def in_tom(lane, t):
        note = {"y": 110, "b": 111, "g": 112}[lane]
        return any(a <= t < b for a, b in spans[note])

    res = {}
    for diff, base in DIFF_BASE.items():
        notes = []
        for t, n, v in ons:
            k = n - base
            if diff == "expert" and n == 95:
                notes.append((tm.sec(t), "kick2x", t))
                continue
            if five_lane:
                if 0 <= k <= 5:
                    notes.append((tm.sec(t), FIVE[k], t))
            elif 0 <= k <= 4:
                lane = PRO_LANE[k]
                if lane in ("kick", "snare"):
                    cls = lane
                else:
                    cls = TOM[lane] if in_tom(lane, t) else CYM[lane]
                notes.append((tm.sec(t), cls, t))
        res[diff] = sorted(notes)
    res["five_lane"] = five_lane
    return res


def load_chart(path):
    """Read a .chart (Clone Hero text format) into the same structure as load()."""
    secs, cur = {}, None
    for line in open(path, encoding="utf-8-sig", errors="ignore"):
        line = line.strip()
        if line.startswith("[") and line.endswith("]"):
            cur = line[1:-1]; secs[cur] = []
        elif "=" in line and cur:
            k, _, v = line.partition("=")
            secs[cur].append((k.strip(), v.strip()))
    res = 192
    for k, v in secs.get("Song", []):
        if k == "Resolution":
            res = int(v)
    tempos, sigs = [], []
    for k, v in secs.get("SyncTrack", []):
        p = v.split()
        if p[0] == "B":
            tempos.append((int(k), int(round(60e6 / (int(p[1]) / 1000.0)))))
        elif p[0] == "TS":
            sigs.append((int(k), int(p[1]), 2 ** int(p[2]) if len(p) > 2 else 4))
    tm = TempoMap(res, tempos)
    out = {"tpb": res, "tempo": tm, "tempos": tempos, "sigs": sigs, "tracks": list(secs), "beats": [],
           "sections": [(tm.sec(int(k)), v) for k, v in secs.get("Events", []) if "section" in v]}
    drums, five = {}, False
    for diff in DIFF_BASE:
        name = diff.capitalize() + "Drums"
        notes, cym = {}, set()
        for k, v in secs.get(name, []):
            p = v.split()
            if p[0] != "N":
                continue
            t, n = int(k), int(p[1])
            if n in (66, 67, 68):
                cym.add((t, n - 64))
            elif n == 5:
                five = True; notes.setdefault(t, set()).add(5)
            elif n == 32:
                notes.setdefault(t, set()).add(32)
            elif 0 <= n <= 4:
                notes.setdefault(t, set()).add(n)
        lst = []
        for t, lanes in notes.items():
            for n in lanes:
                if n == 32:
                    cls = "kick2x"
                elif five:
                    cls = FIVE[n]
                elif n in (0, 1):
                    cls = PRO_LANE[n]
                else:
                    lane = PRO_LANE[n]
                    cls = CYM[lane] if (t, n) in cym else TOM[lane]
                lst.append((tm.sec(t), cls, t))
        drums[diff] = sorted(lst)
        drums.setdefault("cym_marks", 0)
        drums["cym_marks"] += len(cym)
    drums["five_lane"] = five
    out["drums"] = drums
    return out


def is_pro(r):
    """True when a parsed chart marks toms vs cymbals (tom markers in .mid, cymbal flags in .chart)."""
    d = r.get("drums") or {}
    if d.get("five_lane"):
        return False
    ex = d.get("expert", [])
    return any(c.startswith("tom") for _, c, _ in ex) and any(c in ("hihat", "ride", "crash") for _, c, _ in ex)
