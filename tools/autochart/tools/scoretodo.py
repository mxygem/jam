"""Autochart each reference song from its own audio and score it: notes per class (+-50 ms) and bar lines.
  scoretodo.py <reference root> [--out DIR] [--model PATH]
Reference folders hold a human chart (notes.mid or notes.chart) and its audio (stems or song.*)."""
import sys, os, subprocess, argparse, json, re, numpy as np, soundfile as sf
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import refmidi, models

ap = argparse.ArgumentParser(); ap.add_argument("root"); ap.add_argument("--out", default=os.path.expanduser("~/autochart-scratch"))
ap.add_argument("--model", default=models.MODEL_PATH); ap.add_argument("--demucs-repo", default=os.path.expanduser("~/autochart-models"))
a = ap.parse_args()
SR = 44100
GROUP = {"kick": "kick", "kick2x": "kick", "snare": "snare", "hihat": "cym", "ride": "cym", "crash": "cym", "tom1": "tom", "tom2": "tom", "tom3": "tom"}


def decode(p):
    raw = subprocess.run(["ffmpeg", "-v", "error", "-i", p, "-f", "f32le", "-ac", "2", "-ar", str(SR), "-"], capture_output=True, check=True).stdout
    return np.frombuffer(raw, np.float32).reshape(-1, 2)


def match(p, q, tol=0.05):
    p, q = np.sort(p), np.sort(q); used = np.zeros(len(q), bool); tp = 0
    for x in p:
        lo, hi = np.searchsorted(q, [x - tol, x + tol]); c = [j for j in range(lo, hi) if not used[j]]
        if c: used[min(c, key=lambda j: abs(q[j] - x))] = True; tp += 1
    return tp, len(p), len(q)


def f1(r): return 2 * r[0] / max(r[1] + r[2], 1)


def ref_bars(r, end_tick):
    tpb, sigs, tm = r["tpb"], sorted(r["sigs"]) or [(0, 4, 4)], r["tempo"]
    out, tick, i = [], 0, 0
    while tick <= end_tick:
        while i + 1 < len(sigs) and sigs[i + 1][0] <= tick: i += 1
        out.append((tm.sec(tick), sigs[i][1], sigs[i][2])); tick += max(1, int(sigs[i][1] * tpb * 4 / sigs[i][2]))
    return out


def auto_secs(doc):
    tempos, res = doc["grid"]["tempos"], doc["grid"]["resolution"]
    ticks = [e["tick"] for e in tempos]; secs = [0.0]
    for i in range(1, len(tempos)):
        secs.append(secs[-1] + (ticks[i] - ticks[i - 1]) / res * 60 / tempos[i - 1]["bpm"])
    def sec(t):
        i = max(0, np.searchsorted(ticks, t, side="right") - 1)
        return secs[i] + (t - ticks[i]) / res * 60 / tempos[i]["bpm"]
    return sec


rows = []
for name in sorted(os.listdir(a.root)):
    d = os.path.join(a.root, name)
    chart_f = next((f for f in ("notes.mid", "notes.chart") if os.path.exists(os.path.join(d, f))), None)
    if not chart_f: continue
    ini = open(os.path.join(d, "song.ini"), encoding="utf-8", errors="ignore").read().lower() if os.path.exists(os.path.join(d, "song.ini")) else ""
    five = "five_lane_drums = 1" in ini or "five_lane_drums = true" in ini
    r = refmidi.load(os.path.join(d, chart_f), five_lane=five or None) if chart_f.endswith(".mid") else refmidi.load_chart(os.path.join(d, chart_f))
    if not r.get("drums") or not r["drums"].get("expert"):
        print(f"{name}: no Expert drums in the chart, skipped"); continue
    work = os.path.join(a.out, "_mix", re.sub(r"[^\w\- ]", "", name)); os.makedirs(work, exist_ok=True)
    mixp = os.path.join(work, "mix.wav")
    if not os.path.exists(mixp):
        mix = None
        for f in sorted(os.listdir(d)):
            st, ext = os.path.splitext(f)
            if ext.lower() in (".ogg", ".opus", ".mp3", ".flac", ".wav") and st.lower() not in ("preview", "crowd"):
                x = decode(os.path.join(d, f))
                if mix is None: mix = x
                else:
                    n = max(len(mix), len(x)); y = np.zeros((n, 2), np.float32); y[:len(mix)] += mix; y[:len(x)] += x; mix = y
        sf.write(mixp, np.clip(mix, -1, 1), SR, subtype="PCM_16")
    title = re.sub(r"[^\w\- ]", "", name)
    folder = os.path.join(a.out, f"ref - {title} (autochart)")
    if not os.path.exists(os.path.join(folder, "chart.jam.json")):
        subprocess.run([sys.executable, "-m", "autochart.run", mixp, "--artist", "ref", "--title", title, "--out-root", a.out,
                        "--demucs-repo", a.demucs_repo, "--model", a.model], check=True, capture_output=True)
    doc = json.load(open(os.path.join(folder, "chart.jam.json"), encoding="utf-8")); sec = auto_secs(doc)
    LC = {0: "kick", 1: "snare"}
    auto = [(sec(n["t"]), LC.get(n["lane"]) or ({2: "hihat", 3: "ride", 4: "crash"}[n["lane"]] if n["cymbal"] else {2: "tom1", 3: "tom2", 4: "tom3"}[n["lane"]]))
            for n in doc["parts"]["drums"]["difficulties"]["expert"]["notes"]]
    ref = [(t, c) for t, c, _ in r["drums"]["expert"]]
    line = f"{name[:44]:44s}"
    for g in ("kick", "snare", "cym", "tom"):
        line += f" {g} {f1(match([t for t, c in auto if GROUP[c] == g], [t for t, c in ref if GROUP[c] == g])):.2f}"
    exact = sum(match([t for t, c in auto if c == k], [t for t, c in ref if (c if c != "kick2x" else "kick") == k])[0]
                for k in ("kick", "snare", "hihat", "ride", "crash", "tom1", "tom2", "tom3"))
    line += f" | all {2 * exact / (len(auto) + len(ref)):.2f}"
    # bar lines
    end_tick = max(t for _, _, t in r["drums"]["expert"])
    rb = ref_bars(r, end_tick)
    ab = [sec(s["tick"]) for s in doc["grid"]["time_signatures"]]
    # expand auto signatures into bar starts
    sigs = doc["grid"]["time_signatures"]; res = doc["grid"]["resolution"]; last = max(n["t"] for n in doc["parts"]["drums"]["difficulties"]["expert"]["notes"])
    abars = []
    for i, s in enumerate(sigs):
        stop = sigs[i + 1]["tick"] if i + 1 < len(sigs) else last
        tk = s["tick"]; blen = s["num"] * res * 4 // s["den"]
        while tk < stop or (i + 1 == len(sigs) and tk <= last):
            abars.append((sec(tk), s["num"], s["den"])); tk += blen
    rbt = np.array([x for x, _, _ in rb]); abt = np.array([x for x, _, _ in abars])
    rbt = rbt[(rbt > abt[0] - 1) & (rbt < abt[-1] + 1)] if len(abt) else rbt
    bf = f1(match(abt, rbt, 0.07))
    from collections import Counter
    rc = Counter(f"{n}/{dn}" for _, n, dn in rb).most_common(4); ac = Counter(f"{n}/{dn}" for _, n, dn in abars).most_common(4)
    print(line + f" | bars {bf:.2f}\n{'':44s}  human bars {rc}\n{'':44s}  auto bars  {ac}", flush=True)
