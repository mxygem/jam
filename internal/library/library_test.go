package library

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	// Reopening uses the cache: nothing to analyze. The cache is saved just
	// after the index goes idle, so wait for it.
	waitSaved(t, cache, s)
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

// waitSaved waits until the cache file at path has an entry for s.
func waitSaved(t *testing.T, path string, s *chart.Song) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if Open(path).Get(s) != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cache never saved")
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

// fakeSongs makes n songs with chart files on disk. Their cached Info is
// fresh for i%4==0, out of date for i%4==1, missing for i%4==2, and fresh
// again but with the chart file deleted for i%4==3 (a stat error keeps the
// entry).
func fakeSongs(t *testing.T, n int) ([]*chart.Song, []*Info) {
	t.Helper()
	dir := t.TempDir()
	songs := make([]*chart.Song, n)
	infos := make([]*Info, n)
	for i := range songs {
		d := filepath.Join(dir, fmt.Sprintf("s%04d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		cp := filepath.Join(d, "notes.chart")
		if err := os.WriteFile(cp, []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(cp)
		songs[i] = &chart.Song{Dir: d, ChartPath: cp}
		switch i % 4 {
		case 0:
			infos[i] = &Info{ChartSize: st.Size(), ChartMod: st.ModTime().UnixNano()}
		case 1:
			infos[i] = &Info{ChartSize: st.Size() + 1, ChartMod: st.ModTime().UnixNano()}
		case 3:
			infos[i] = &Info{ChartSize: st.Size() + 1}
			os.Remove(cp)
		}
	}
	return songs, infos
}

func TestStaleSongsSameForAnyWorkerCount(t *testing.T) {
	songs, infos := fakeSongs(t, 203)
	want := make([]bool, len(songs))
	for i := range songs {
		want[i] = !fresh(infos[i], songs[i])
	}
	nStale := 0
	for _, w := range want {
		if w {
			nStale++
		}
	}
	if nStale != 102 { // i%4 == 1 and i%4 == 2
		t.Fatalf("setup: %d stale", nStale)
	}
	for _, workers := range []int{1, 2, 4, 7, 500} {
		if got := staleSongs(songs, infos, workers); !reflect.DeepEqual(got, want) {
			t.Fatalf("workers=%d: result differs", workers)
		}
	}
	if got := staleSongs(nil, nil, 4); len(got) != 0 {
		t.Fatal("no songs")
	}
}

// While Update checks the charts it holds no lock, so the song list can keep
// reading the index, and an entry added meanwhile is not thrown away.
func TestUpdateDoesNotBlockReadersAndKeepsNewerEntries(t *testing.T) {
	songs, infos := fakeSongs(t, 40)
	x := Open(filepath.Join(t.TempDir(), "library.json.gz"))
	for i, in := range infos {
		if in != nil {
			x.Put(songs[i], in)
		}
	}
	fresh0, fresh4 := songs[0], songs[4]
	newer := &Info{ChartSize: infos[0].ChartSize, ChartMod: infos[0].ChartMod, Lyrics: true}
	testHookChecking = func() {
		done := make(chan struct{})
		go func() {
			x.Progress()
			x.Get(fresh0)
			x.Put(fresh0, newer) // loaded to play while the check runs
			x.Update(songs)      // a second Update while one is checking does nothing
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("index calls blocked while Update checks charts")
		}
	}
	defer func() { testHookChecking = nil }()
	x.Update(songs)
	testHookChecking = nil

	if got := x.Get(fresh0); got != newer {
		t.Errorf("entry added during the check was replaced: %+v", got)
	}
	if x.Get(fresh4) != infos[4] {
		t.Error("fresh entry lost")
	}
	// i%4==3 has no chart file: kept. i%4==1 is out of date, i%4==2 is new.
	if x.Get(songs[3]) != infos[3] {
		t.Error("entry with unreadable chart should be kept")
	}
	if x.Get(songs[1]) != nil || x.Get(songs[2]) != nil {
		t.Error("stale or new songs should wait for analysis")
	}
	if _, total, running, _ := x.Progress(); total != 20 || !running {
		t.Errorf("total=%d running=%v, want 20 songs to analyze", total, running)
	}
	waitIdle(t, x)
	// The cache is saved just after the index goes idle; don't end the test
	// (and remove its temp dir) before that.
	for i := 0; i < 500; i++ {
		if _, err := os.Stat(x.path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("cache never saved")
}

// BenchmarkStaleSongs checks JAM_BENCH_SONGS charts (default 5000) with 1 and
// 4 workers.
func BenchmarkStaleSongs(b *testing.B) {
	n := 5000
	if v := os.Getenv("JAM_BENCH_SONGS"); v != "" {
		fmt.Sscan(v, &n)
	}
	dir := b.TempDir()
	songs := make([]*chart.Song, n)
	infos := make([]*Info, n)
	for i := range songs {
		d := filepath.Join(dir, fmt.Sprintf("p%03d", i/200), fmt.Sprintf("s%05d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			b.Fatal(err)
		}
		cp := filepath.Join(d, "notes.chart")
		if err := os.WriteFile(cp, []byte("x"), 0o644); err != nil {
			b.Fatal(err)
		}
		st, _ := os.Stat(cp)
		songs[i] = &chart.Song{Dir: d, ChartPath: cp}
		infos[i] = &Info{ChartSize: st.Size(), ChartMod: st.ModTime().UnixNano()}
	}
	for _, w := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				staleSongs(songs, infos, w)
			}
		})
	}
}
