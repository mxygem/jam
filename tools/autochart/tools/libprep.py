"""Prepare library charts as training songs (runs on the PC, GPU for Demucs).
  libprep.py training_candidates.csv [--limit N]
For each chart: parse it, keep pro-drum Expert parts with 150+ notes, mix all audio stems, run Demucs,
and write $AUTOCHART_DATA/<key>/{ref.pkl, drums_sep.wav}. Skips songs already done and the reference songs."""
import sys, os, csv, pickle, subprocess, hashlib, argparse, re, numpy as np, soundfile as sf
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import refmidi, dataset

SR = 44100
ap = argparse.ArgumentParser(); ap.add_argument("csv"); ap.add_argument("--limit", type=int, default=0)
ap.add_argument("--demucs-repo", default=os.path.expanduser("~/autochart-models"))
a = ap.parse_args()
REF_SONGS = {"attack", "rehab", "one armed scissor", "beast and the harlot", "25 or 6 to 4", "oort cloud"}


def winpath(p):
    m = re.match(r"^([A-Za-z]):\\(.*)$", p)
    return f"/mnt/{m.group(1).lower()}/" + m.group(2).replace("\\", "/") if (m and os.name != "nt") else p


def decode(p):
    raw = subprocess.run(["ffmpeg", "-v", "error", "-i", p, "-f", "f32le", "-ac", "2", "-ar", str(SR), "-"],
                         capture_output=True, check=True).stdout
    return np.frombuffer(raw, np.float32).reshape(-1, 2)


rows = list(csv.DictReader(open(a.csv, encoding="utf-8-sig")))
done = skipped = 0
for row in rows:
    if a.limit and done >= a.limit:
        break
    name = row["Name"].strip().lower()
    if name in REF_SONGS:
        print("skip (reference song):", row["Name"]); skipped += 1; continue
    d = winpath(row["Dir"])
    key = re.sub(r"[^\w\- ]", "", f"{row['Artist']} - {row['Name']}")[:80] + "_" + hashlib.md5(row["Dir"].encode()).hexdigest()[:6]
    out = os.path.join(dataset.DATA, key)
    if os.path.exists(os.path.join(out, "drums_sep.wav")):
        done += 1; continue
    try:
        cp = os.path.join(d, row["Chart"])
        r = refmidi.load(cp) if cp.endswith(".mid") else refmidi.load_chart(cp)
        if not refmidi.is_pro(r) or len(r["drums"]["expert"]) < 150:
            print("skip (not pro drums or too few notes):", key); skipped += 1; continue
        mix = None
        for f in sorted(os.listdir(d)):
            st, ext = os.path.splitext(f)
            if ext.lower() in (".ogg", ".opus", ".mp3", ".flac", ".wav") and st.lower() not in ("preview", "crowd"):
                x = decode(os.path.join(d, f))
                if mix is None: mix = x
                else:
                    n = max(len(mix), len(x)); y = np.zeros((n, 2), np.float32); y[:len(mix)] += mix; y[:len(x)] += x; mix = y
        if len(mix) / SR > 900:
            # full-album charts (40+ minutes) run the WSL VM out of memory in Demucs and crash it
            print(f"skip (longer than 15 minutes, {len(mix) / SR / 60:.0f} min):", key, flush=True); skipped += 1; continue
        os.makedirs(out, exist_ok=True)
        sf.write(os.path.join(out, "mix.wav"), np.clip(mix, -1, 1), SR, subtype="PCM_16")
        subprocess.run([sys.executable, "-m", "demucs", "--repo", a.demucs_repo, "-n", "htdemucs", "--two-stems", "drums",
                        "-o", os.path.join(out, "demucs"), os.path.join(out, "mix.wav")], check=True, capture_output=True)
        os.replace(os.path.join(out, "demucs", "htdemucs", "mix", "drums.wav"), os.path.join(out, "drums_sep.wav"))
        subprocess.run(["rm", "-rf", os.path.join(out, "demucs"), os.path.join(out, "mix.wav")])
        ini = open(os.path.join(d, "song.ini"), encoding="utf-8", errors="ignore").read()
        delay = re.search(r"(?im)^\s*delay\s*=\s*(-?\d+)", ini)
        pickle.dump(dict(song=key, charter=row["Charter"], rank=int(row["Rank"]), delay_ms=int(delay.group(1)) if delay else 0,
                         tpb=r["tpb"], drums={k: v for k, v in r["drums"].items() if k in ("expert", "hard", "medium", "easy")},
                         beats=[], src=row["Dir"]), open(os.path.join(out, "ref.pkl"), "wb"))
        done += 1
        print(f"ok {done}: {key} ({len(r['drums']['expert'])} expert notes)", flush=True)
    except Exception as e:
        print("fail:", key, repr(e)[:200], flush=True); skipped += 1
print(f"DONE prepared {done}, skipped {skipped}")
