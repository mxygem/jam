"""Decode a reference song folder into drums.wav (official drum stems summed) and mix.wav."""
import sys, os, subprocess, numpy as np, soundfile as sf
SR = 44100
def dec(p):
    raw = subprocess.run(["ffmpeg", "-v", "error", "-i", p, "-f", "f32le", "-ac", "2", "-ar", str(SR), "-"],
                         capture_output=True, check=True).stdout
    return np.frombuffer(raw, np.float32).reshape(-1, 2)
src, dst = sys.argv[1], sys.argv[2]
os.makedirs(dst, exist_ok=True)
drums, mix = None, None
def add(a, b):
    if a is None: return b.copy()
    n = max(len(a), len(b)); out = np.zeros((n, 2), np.float32); out[:len(a)] += a; out[:len(b)] += b; return out
for f in sorted(os.listdir(src)):
    stem, ext = os.path.splitext(f)
    if ext not in (".ogg", ".opus", ".mp3", ".flac", ".wav") or stem in ("crowd", "preview"): continue
    a = dec(os.path.join(src, f))
    mix = add(mix, a)
    if stem.startswith("drums"): drums = add(drums, a)
sf.write(os.path.join(dst, "mix.wav"), np.clip(mix, -1, 1), SR, subtype="PCM_16")
if drums is not None: sf.write(os.path.join(dst, "drums_ref.wav"), np.clip(drums, -1, 1), SR, subtype="PCM_16")
print(dst, len(mix) / SR, drums is not None)
