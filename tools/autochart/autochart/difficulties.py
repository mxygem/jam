"""Add Hard / Medium / Easy drums to an autochart folder from its Expert chart.

  python -m autochart.difficulties <song folder> [--model PATH]

Reads Expert from chart.jam.json, predicts which notes stay in each lower difficulty (classifiers
learned from official charts' reductions, see tools/train_final.py), and rewrites notes.chart and
chart.jam.json with all four difficulties. Run it again to redo them; Expert is never changed."""
import argparse, json, os, re
from . import models, reduce as red, chart

CLS = {(0, False): "kick", (1, False): "snare", (2, True): "hihat", (3, True): "ride", (4, True): "crash",
       (2, False): "tom1", (3, False): "tom2", (4, False): "tom3"}


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("folder"); ap.add_argument("--model", default=models.MODEL_PATH)
    a = ap.parse_args(argv)
    pj = os.path.join(a.folder, "chart.jam.json")
    doc = json.load(open(pj, encoding="utf-8"))
    res = doc["grid"]["resolution"]
    diffs = doc["parts"]["drums"]["difficulties"]
    expert = diffs["expert"]["notes"]
    m = models.Model.load(a.model)
    notes = [(n["t"] * chart.RES // res, CLS.get((n["lane"], bool(n["cymbal"])), "snare")) for n in expert]
    X = red.features(notes, chart.RES)
    out = {}
    for d in ("hard", "medium", "easy"):
        keep = m.reduce[d].predict_proba(X)[:, 1] > 0.5
        out[d] = [dict(n, conf=round(n["conf"] * 0.9, 3), src=n["src"] + f" kept:{d}") for n, k in zip(expert, keep) if k]
        diffs[d] = {"notes": out[d], "star_power": [], "solos": [], "fills": []}
    json.dump(doc, open(pj, "w", encoding="utf-8"), indent=0)
    # notes.chart: drop any old lower-difficulty drum sections, append the new ones
    pc = os.path.join(a.folder, "notes.chart")
    txt = open(pc, encoding="utf-8").read()
    txt = re.sub(r"\[(Hard|Medium|Easy)Drums\]\s*\{[^}]*\}\s*", "", txt)
    lines = []
    for d in ("hard", "medium", "easy"):
        lines += [f"[{d.capitalize()}Drums]", "{"]
        for n in out[d]:
            lines.append(f"  {n['t']} = N {n['lane']} 0")
            if n["cymbal"] and n["lane"] in (2, 3, 4):
                lines.append(f"  {n['t']} = N {64 + n['lane']} 0")
        lines.append("}")
    open(pc, "w", encoding="utf-8").write(txt.rstrip("\n") + "\n" + "\n".join(lines) + "\n")
    print(f"expert {len(expert)}, " + ", ".join(f"{d} {len(v)}" for d, v in out.items()))


if __name__ == "__main__":
    main()
