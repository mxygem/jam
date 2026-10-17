package library

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/demo"
)

// Reference values from Fretwork's own Python code (commit 4a73fea) on two
// of Gem's charts. The port must match it.
func TestFretworkMatchesReference(t *testing.T) {
	charts := os.Getenv("FRETWORK_CHARTS") // folder with onlyhuman/ and neon/
	if charts == "" {
		t.Skip("set FRETWORK_CHARTS to run against the reference charts")
	}
	want := map[string]map[chart.Instrument]Fretwork{
		"onlyhuman": {chart.Guitar: {12.995351504323212, 4, 4}, chart.Rhythm: {12.9446799687381, 4, 4}},
		"neon":      {chart.Bass: {9.506666280350963, 3, 2}, chart.Drums: {12.88541472251876, 2, 2}},
	}
	for dir, parts := range want {
		s, err := chart.LoadSongFolder(filepath.Join(charts, dir))
		if err != nil {
			t.Fatal(err)
		}
		in := Analyze(s)
		for inst, w := range parts {
			p := in.Part(inst)
			if p == nil || p.Fw == nil {
				t.Errorf("%s %s: no rating", dir, inst)
				continue
			}
			if math.Abs(p.Fw.D-w.D) > 1e-6 || p.Fw.Remap != w.Remap || p.Fw.Tier != w.Tier {
				t.Errorf("%s %s: got D %.6f remap %d tier %d, want D %.6f remap %d tier %d", dir, inst, p.Fw.D, p.Fw.Remap, p.Fw.Tier, w.D, w.Remap, w.Tier)
			}
		}
	}
}

func TestIndexCacheAndUpdate(t *testing.T) {
	dir := t.TempDir()
	songDir, err := demo.Write(filepath.Join(dir, "songs"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := chart.LoadSongFolder(songDir)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "library.json.gz")
	x := Open(cache)
	x.Update([]*chart.Song{s})
	waitIdle(t, x)
	in := x.Get(s)
	if in == nil || in.Err != "" || len(in.Parts) < 3 || in.LengthMs < 10000 {
		t.Fatalf("demo info: %+v", in)
	}
	if p := in.Part(chart.Guitar); p == nil || p.Fw == nil || p.Fw.D <= 0 || p.AvgNPS <= 0 || p.PeakNPS < p.AvgNPS {
		t.Fatalf("guitar part: %+v", p)
	}
	// Reopening uses the cache: nothing to analyze.
	y := Open(cache)
	if y.Get(s) == nil {
		t.Fatal("cache not loaded")
	}
	y.Update([]*chart.Song{s})
	if _, total, _, _ := y.Progress(); total != 0 {
		t.Fatalf("unchanged chart re-analyzed (%d)", total)
	}
	// Touching the chart makes it stale.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(s.ChartPath, later, later); err != nil {
		t.Fatal(err)
	}
	y.Update([]*chart.Song{s})
	if _, total, _, _ := y.Progress(); total != 1 {
		t.Fatalf("changed chart not re-analyzed (%d)", total)
	}
	waitIdle(t, y)
}

func waitIdle(t *testing.T, x *Index) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if _, _, running, _ := x.Progress(); !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("index never finished")
}

// The built-in demo song rated by Fretwork's own Python code (commit
// 4a73fea), so the port is checked on every test run.
func TestFretworkMatchesReferenceOnDemo(t *testing.T) {
	songDir, err := demo.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := chart.LoadSongFolder(songDir)
	if err != nil {
		t.Fatal(err)
	}
	in := Analyze(s)
	want := map[chart.Instrument]Fretwork{
		chart.Guitar: {10.22548091496365, 2, 3},
		chart.Bass:   {5.684074668133737, 1, 0},
		chart.Drums:  {9.212754149101206, 0, 0},
	}
	for inst, w := range want {
		p := in.Part(inst)
		if p == nil || p.Fw == nil {
			t.Fatalf("%s: no rating", inst)
		}
		if math.Abs(p.Fw.D-w.D) > 1e-6 || p.Fw.Remap != w.Remap || p.Fw.Tier != w.Tier {
			t.Errorf("%s: got D %.6f remap %d tier %d, want D %.6f remap %d tier %d", inst, p.Fw.D, p.Fw.Remap, p.Fw.Tier, w.D, w.Remap, w.Tier)
		}
	}
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// waitCache waits until the saved cache file holds an entry for s that ok
// accepts (the save runs just after the index goes idle).
func waitCache(t *testing.T, cache string, s *chart.Song, ok func(*Info) bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if in := Open(cache).Get(s); in != nil && ok(in) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cache was not saved")
}

// settle waits for the index to go idle and for its cache save to land, so a
// test never ends while the indexer is still writing into its temp dir.
func settle(t *testing.T, x *Index, songs ...*chart.Song) {
	t.Helper()
	waitIdle(t, x)
	for _, s := range songs {
		want := x.Get(s).SongSHA256
		waitCache(t, x.path, s, func(c *Info) bool { return c.SongSHA256 == want })
	}
}

// changeFile makes a file different in content, size and modification time.
func changeFile(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func demoSong(t *testing.T, parent string) *chart.Song {
	t.Helper()
	songDir, err := demo.Write(parent)
	if err != nil {
		t.Fatal(err)
	}
	s, err := chart.LoadSongFolder(songDir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIndexHashes(t *testing.T) {
	dir := t.TempDir()
	s := demoSong(t, filepath.Join(dir, "songs"))
	cache := filepath.Join(dir, "library.json.gz")
	x := Open(cache)
	x.Update([]*chart.Song{s})
	waitIdle(t, x)
	in := x.Get(s)
	if in.ChartSHA256 != fileSHA(t, s.ChartPath) {
		t.Fatalf("chart hash %q", in.ChartSHA256)
	}
	if len(in.Audio) != len(s.Stems) || len(s.Stems) == 0 {
		t.Fatalf("audio hashes for %d of %d stems", len(in.Audio), len(s.Stems))
	}
	for name, path := range s.Stems {
		f := in.Audio[name]
		if f == nil || f.SHA256 != fileSHA(t, path) || f.File != filepath.Base(path) {
			t.Fatalf("stem %s: %+v", name, f)
		}
	}
	if in.SongSHA256 == "" {
		t.Fatal("no combined hash")
	}

	// Hashes survive the cache, and an unchanged library needs no work.
	waitCache(t, cache, s, func(c *Info) bool { return c.SongSHA256 == in.SongSHA256 })
	y := Open(cache)
	y.Update([]*chart.Song{s})
	if _, total, _, _ := y.Progress(); total != 0 {
		t.Fatalf("unchanged song re-indexed (%d)", total)
	}

	// Changing one stem re-hashes only that stem and does not re-parse the chart.
	before := x.Get(s)
	oldSong := before.SongSHA256
	changeFile(t, s.Stems["guitar"])
	x.Update([]*chart.Song{s})
	if _, total, _, _ := x.Progress(); total != 1 {
		t.Fatalf("changed stem not picked up (%d)", total)
	}
	waitIdle(t, x)
	after := x.Get(s)
	if after.Audio["guitar"].SHA256 != fileSHA(t, s.Stems["guitar"]) || after.Audio["guitar"].SHA256 == before.Audio["guitar"].SHA256 {
		t.Fatal("changed stem not re-hashed")
	}
	if after.Audio["bass"] != before.Audio["bass"] {
		t.Fatal("unchanged stem was hashed again")
	}
	if after.ChartSHA256 != before.ChartSHA256 || after.SongSHA256 == oldSong {
		t.Fatal("combined hash did not follow the stem")
	}
	if &after.Parts[0] != &before.Parts[0] {
		t.Fatal("chart was parsed again for a stem change")
	}
	if before.SongSHA256 != oldSong {
		t.Fatal("old entry was modified in place")
	}
	settle(t, x, s)
}

// A cache written before hashes existed gets them without re-parsing charts.
func TestHashBackfill(t *testing.T) {
	dir := t.TempDir()
	s := demoSong(t, filepath.Join(dir, "songs"))
	cache := filepath.Join(dir, "library.json.gz")
	x := Open(cache)
	x.Update([]*chart.Song{s})
	waitIdle(t, x)
	want := x.Get(s).SongSHA256
	waitCache(t, cache, s, func(c *Info) bool { return c.SongSHA256 == want })

	old := Open(cache)
	for _, in := range old.infos {
		in.ChartSHA256, in.Audio, in.SongSHA256 = "", nil, ""
	}
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}
	y := Open(cache)
	parts := &y.Get(s).Parts[0]
	y.Update([]*chart.Song{s})
	if _, total, _, _ := y.Progress(); total != 1 {
		t.Fatalf("old cache entry not queued for hashing (%d)", total)
	}
	waitIdle(t, y)
	in := y.Get(s)
	if in.SongSHA256 != want || len(in.Audio) != len(s.Stems) {
		t.Fatalf("backfilled hashes: %+v", in)
	}
	if &in.Parts[0] != parts {
		t.Fatal("chart was parsed again during backfill")
	}
	settle(t, y, s)
}

func TestHashSkipsUnreadableStem(t *testing.T) {
	s := demoSong(t, t.TempDir())
	s.Stems["ghost"] = filepath.Join(s.Dir, "ghost.ogg")
	in := &Info{}
	in.addSums(s, nil)
	if in.ChartSHA256 == "" || in.Audio["ghost"] != nil || len(in.Audio) != len(s.Stems)-1 {
		t.Fatalf("sums: %+v", in)
	}
	if in.SongSHA256 != "" {
		t.Fatal("combined hash set from partial data")
	}
}

func TestDuplicates(t *testing.T) {
	dir := t.TempDir()
	a := demoSong(t, filepath.Join(dir, "a"))
	bDir := filepath.Join(dir, "b")
	if err := os.CopyFS(bDir, os.DirFS(a.Dir)); err != nil {
		t.Fatal(err)
	}
	b, err := chart.LoadSongFolder(bDir)
	if err != nil {
		t.Fatal(err)
	}
	songs := []*chart.Song{b, a}
	x := Open(filepath.Join(dir, "library.json.gz"))
	x.Update(songs)
	settle(t, x, a, b)

	// Identical folders are one "song" group, and nothing else is reported.
	groups := x.Duplicates(songs)
	if len(groups) != 1 || groups[0].Kind != "song" || groups[0].SHA256 != x.Get(a).SongSHA256 ||
		len(groups[0].Dirs) != 2 || groups[0].Dirs[0] != a.Dir || groups[0].Dirs[1] != b.Dir {
		t.Fatalf("identical copies: %+v", groups)
	}

	// With one stem different, the chart and the other stems still match.
	changeFile(t, b.Stems["guitar"])
	x.Update(songs)
	settle(t, x, a, b)
	kinds := map[string]int{}
	for _, g := range x.Duplicates(songs) {
		kinds[g.Kind]++
	}
	if kinds["song"] != 0 || kinds["chart"] != 1 || kinds["audio"] != len(a.Stems)-1 {
		t.Fatalf("one stem differs: %v", kinds)
	}

	// A song that is not hashed yet is skipped, not guessed at.
	if g := Open(filepath.Join(dir, "none.json.gz")).Duplicates(songs); len(g) != 0 {
		t.Fatalf("unindexed: %+v", g)
	}
}
