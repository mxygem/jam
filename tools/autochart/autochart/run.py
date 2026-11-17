"""autochart: audio file -> jam song folder (drums for now).

  python -m autochart.run SONG.mp3 --artist A --title T [--drums STEM.wav] [--out-root DIR]

Steps: Demucs drum stem (unless --drums is given) -> onset candidates -> trained hit detectors ->
cymbal lanes, tom pitches -> beat grid -> snapped Expert notes -> learned Hard/Medium/Easy ->
notes.chart + chart.jam.json + song.ini + audio.
"""
import argparse, os, subprocess, sys, json, shutil, time, numpy as np
from . import features, models, tempomap, chart, reduce as red

THR = 0.4


def default_out_root():
    if os.name == "nt":
        return r"E:\charts\autocharts"
    return os.environ.get("AUTOCHART_OUT", os.path.expanduser("~/autocharts"))


def separate(audio, work, repo=None):
    out = os.path.join(work, "drums_sep.wav")
    if os.path.exists(out) and os.path.exists(os.path.join(work, "no_drums.wav")):
        return out
    cmd = [sys.executable, "-m", "demucs", "-n", "htdemucs", "--two-stems", "drums", "-o", os.path.join(work, "demucs"), audio]
    if repo:
        cmd[3:3] = ["--repo", repo]
    subprocess.run(cmd, check=True)
    stem = os.path.splitext(os.path.basename(audio))[0]
    shutil.copy(os.path.join(work, "demucs", "htdemucs", stem, "drums.wav"), out)
    shutil.copy(os.path.join(work, "demucs", "htdemucs", stem, "no_drums.wav"), os.path.join(work, "no_drums.wav"))
    return out


def tom_lanes(Xn, idx):
    """Split tom hits into high/mid/floor by the centroid of their attack spectrum (first 16 features)."""
    if len(idx) == 0:
        return {}
    shape = Xn[idx, :16]
    cen = (shape * np.arange(16)).sum(1) / (shape.sum(1) + 1e-6)
    if len(idx) < 6:
        return {i: "tom2" for i in idx}
    q1, q2 = np.percentile(cen, [33, 67])
    return {i: ("tom1" if c > q2 else "tom3" if c < q1 else "tom2") for i, c in zip(idx, cen)}


def detect(model, drums_path, log):
    y, times, X = features.analyze(drums_path)
    Xn, P = model.predict(X, times)
    hits = []
    # official charts sit on the detection peaks (within ~5 ms); waveform attack starts ran ~18 ms early
    times_fixed = times.copy()
    ci = np.nonzero(P[:, 2] > THR)[0]
    lanes = model.lane.predict_proba(np.array([models.cym_features(Xn, times, P, i) for i in ci])) if len(ci) else []
    ti = np.nonzero(P[:, 3] > THR)[0]
    toms = tom_lanes(Xn, ti)
    for i in range(len(times)):
        t = float(times_fixed[i])
        if P[i, 0] > THR: hits.append(dict(t=t, cls="kick", conf=P[i, 0], src="model:kick"))
        if P[i, 1] > THR: hits.append(dict(t=t, cls="snare", conf=P[i, 1], src="model:snare"))
        if P[i, 3] > THR and P[i, 3] >= P[i, 2]: hits.append(dict(t=t, cls=toms[i], conf=P[i, 3], src="model:tom"))
    for k, i in enumerate(ci):
        if P[i, 3] > THR and P[i, 3] >= P[i, 2]:
            continue
        lane = models.CYM_LANES[int(np.argmax(lanes[k]))]
        hits.append(dict(t=float(times_fixed[i]), cls=lane, conf=P[i, 2] * lanes[k].max(), src=f"model:cym->{lane}"))
    log(f"candidates {len(times)}, hits {len(hits)}")
    return hits, times_fixed, P


def beat_grid(hits, end, log):
    w = {"kick": 0.6, "snare": 1.0}
    hs = sorted((h["t"], h["conf"] * w.get(h["cls"], 0.45)) for h in hits)
    beats, (c, bpm, conf) = tempomap.track(hs, end)
    pick = lambda names: sorted((h["t"], h["conf"]) for h in hits if h["cls"] in names)
    beats, nflip = tempomap.fix_phase(beats, pick({"kick"}), pick({"snare"}), pick({"hihat", "ride", "crash"}))
    beats = tempomap.refine(beats, hs)
    log(f"beats {len(beats)}, tempo {np.median(bpm):.1f} BPM (range {np.nanmin(bpm):.1f}-{np.nanmax(bpm):.1f}), phase fixes {nflip}")
    return beats


def downbeat_phase(beats, hits):
    pick = lambda n: np.array(sorted(h["t"] for h in hits if h["cls"] == n))
    return tempomap.downbeats(beats, pick("kick"), pick("snare"), pick("crash"))


def reduce_diffs(model, expert):
    notes = [(n["t"], n["cls"]) for n in expert]
    out = {"expert": expert}
    if not notes:
        return {d: [] for d in ("expert", "hard", "medium", "easy")}
    X = red.features(notes, chart.RES)
    for d in ("hard", "medium", "easy"):
        keep = model.reduce[d].predict_proba(X)[:, 1] > 0.5
        out[d] = [dict(n, conf=round(n["conf"] * 0.9, 3), src=n["src"] + f" kept:{d}") for n, k in zip(expert, keep) if k]
    return out


def sections_every(grid, bars=8, meter=4):
    """Practice sections every 8 bars from the first full bar (placeholder until structure analysis)."""
    first = grid.pickup * chart.RES
    last = grid.ticks[-1]
    return [(int(t), f"Part {i + 1}") for i, t in enumerate(range(first, int(last), bars * meter * chart.RES))]


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("audio"); ap.add_argument("--artist", default="Unknown"); ap.add_argument("--title")
    ap.add_argument("--drums", help="use this drum stem instead of running Demucs")
    ap.add_argument("--out-root", default=default_out_root()); ap.add_argument("--demucs-repo")
    ap.add_argument("--model", default=models.MODEL_PATH)
    a = ap.parse_args(argv)
    title = a.title or os.path.splitext(os.path.basename(a.audio))[0]
    folder = os.path.join(a.out_root, f"{a.artist} - {title} (autochart)".replace(":", "").replace("/", "-"))
    work = os.path.join(os.path.expanduser("~/.cache/autochart/work"), os.path.basename(folder))
    os.makedirs(folder, exist_ok=True); os.makedirs(work, exist_ok=True)
    logs = []
    def log(msg):
        logs.append(msg); print(msg, flush=True)
    t0 = time.time()
    model = models.Model.load(a.model)
    drums = a.drums or separate(a.audio, work, a.demucs_repo)
    hits, _, _ = detect(model, drums, log)
    end = max(h["t"] for h in hits) + 2
    beats = beat_grid(hits, end, log)
    grid = chart.Grid(beats)
    expert = chart.build_expert(grid, hits)
    diffs = reduce_diffs(model, expert)
    phase = downbeat_phase(beats, hits)
    grid.set_downbeat(len(grid.beats) - len(beats) + phase)
    sections = sections_every(grid)
    log(" ".join(f"{d} {len(n)}" for d, n in diffs.items()))
    meta = dict(title=title, artist=a.artist, album="", year="", genre="", charter="jam autochart",
                length_ms=int(end * 1000), audio={"drums": "drums.ogg", "song": "song.ogg"}, preview_start_ms=-1)
    chart.write_chart(os.path.join(folder, "notes.chart"), meta, grid, diffs, sections)
    chart.write_project(os.path.join(folder, "chart.jam.json"), meta, grid, diffs, sections,
                        {"beats": [round(b, 4) for b in beats], "drum_hits": len(hits)}, logs)
    with open(os.path.join(folder, "song.ini"), "w", encoding="utf-8") as f:
        f.write(f"[song]\nname = {title}\nartist = {a.artist}\ncharter = jam autochart\npro_drums = True\n"
                f"diff_drums = -1\ndiff_drums_real = -1\nsong_length = {int(end * 1000)}\npreview_start_time = -1\n")
    # audio: the mix without drums as song.ogg (full mix if no separation ran), drum stem as drums.ogg
    rest = os.path.join(work, "no_drums.wav")
    subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", rest if (not a.drums and os.path.exists(rest)) else a.audio, "-c:a", "libvorbis", "-q:a", "5", os.path.join(folder, "song.ogg")], check=True)
    if not a.drums:
        subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", drums, "-c:a", "libvorbis", "-q:a", "5", os.path.join(folder, "drums.ogg")], check=True)
    else:
        meta["audio"].pop("drums", None)
    log(f"wrote {folder} in {time.time() - t0:.0f}s")
    return folder


if __name__ == "__main__":
    main()
