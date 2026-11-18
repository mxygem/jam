import sys, os, collections
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from autochart import refmidi
root = sys.argv[1]
for d in sorted(os.listdir(root)):
    r = refmidi.load(os.path.join(root, d, "notes.mid"))
    dr = r.get("drums", {})
    bpms = [60e6 / us for _, us in r["tempos"]]
    print(f"== {d}\n tracks: {r['tracks']}\n tempos: {len(bpms)} ({min(bpms):.1f}-{max(bpms):.1f}) sigs: {sorted(set((n,dn) for _,n,dn in r['sigs']))} five_lane={dr.get('five_lane')}")
    for diff in ("expert", "hard", "medium", "easy"):
        c = collections.Counter(c for _, c, _ in dr.get(diff, []))
        print(f"  {diff:6s} {sum(c.values()):5d} {dict(c)}")
    print("  sections:", len(r["sections"]), [s for _, s in r["sections"][:6]])
    print("  lyrics events:", len(r.get("lyrics", [])))
