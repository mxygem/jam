import sys, os, time, numpy as np, collections
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import features, refmidi, dataset
d = sys.argv[1]; which = sys.argv[2] if len(sys.argv) > 2 else "drums_ref.wav"
song = os.path.basename(d.rstrip("/"))
ref = refmidi.load(os.path.join(dataset.REF_ROOT, song, "notes.mid"))["drums"]["expert"]
t0 = time.time(); y, times, X = features.analyze(os.path.join(d, which))
rt = np.array(sorted(set(round(t, 4) for t, _, _ in ref)))
# recall of distinct ref hit times by candidates within 40 ms
idx = np.searchsorted(times, rt)
dist = np.minimum(np.abs(times[np.clip(idx, 0, len(times)-1)] - rt), np.abs(times[np.clip(idx-1, 0, len(times)-1)] - rt))
print(f"{song[:40]:40s} cands {len(times)} ref hits {len(rt)} recall@40ms {np.mean(dist<0.04):.3f} median off {np.median((times[np.clip(idx,0,len(times)-1)]-rt)[dist<0.04])*1000:.1f}ms  {time.time()-t0:.0f}s X{X.shape}")
