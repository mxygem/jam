"""Summarize autochart folders: tempo range, time signatures, notes off the grid.
  python tools/report.py <folder> [<folder> ...]"""
import sys, json, os
from collections import Counter

for d in sys.argv[1:]:
    p = os.path.join(d, "chart.jam.json")
    if not os.path.exists(p):
        continue
    j = json.load(open(p, encoding="utf-8"))
    g = j["grid"]; res = g["resolution"]
    bpms = [t["bpm"] for t in g["tempos"][1:]] or [g["tempos"][0]["bpm"]]
    sigs = g["time_signatures"]
    # bars per signature: walk the signature list
    end = max((n["t"] for n in j["parts"]["drums"]["difficulties"]["expert"]["notes"]), default=0)
    cnt = Counter()
    for i, s in enumerate(sigs):
        stop = sigs[i + 1]["tick"] if i + 1 < len(sigs) else end
        blen = s["num"] * res * 4 // s["den"]
        cnt[f'{s["num"]}/{s["den"]}'] += max(1, (stop - s["tick"]) // blen) if stop > s["tick"] else 0
    notes = j["parts"]["drums"]["difficulties"]["expert"]["notes"]
    raw = sum(" raw " in n["src"] or n["src"].endswith(" raw") for n in notes)
    q12 = sum(" q12 " in n["src"] or " q24 " in n["src"] for n in notes)
    bpms.sort()
    lo, hi = bpms[len(bpms) // 20], bpms[-1 - len(bpms) // 20]
    print(f"{os.path.basename(d)}\n  tempo {lo:.0f}-{hi:.0f} BPM (middle 90%), bars: "
          + ", ".join(f"{k} x{v}" for k, v in cnt.most_common(6))
          + f"\n  notes {len(notes)}, off the grid {raw} ({100 * raw / max(len(notes), 1):.0f}%), triplet {q12}")
