package library

import (
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
