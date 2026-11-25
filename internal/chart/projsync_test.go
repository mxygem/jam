package chart

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnchoredMappingIsPiecewiseLinear(t *testing.T) {
	base := NewTempoMap(192, []Tempo{{Tick: 0, BPM: 120}}, nil, 0)
	// 120 BPM in the base, but the sync points say 60 BPM between 1 s and 3 s.
	tm := base.WithAnchors([]Anchor{{Tick: 0, Sec: 1}, {Tick: 384, Sec: 3}})
	cases := []struct {
		tick int64
		sec  float64
	}{
		{0, 1}, {192, 2}, {384, 3}, // inside and at the points
		{768, 4},    // after: the base tempo (0.5 s a beat) shifted to meet the last point
		{-192, 0.5}, // before: one beat of the base tempo (0.5 s), shifted to meet the first point
	}
	for _, c := range cases {
		if got := tm.Seconds(c.tick); math.Abs(got-c.sec) > 1e-9 {
			t.Errorf("Seconds(%d) = %v, want %v", c.tick, got, c.sec)
		}
	}
	for _, sec := range []float64{0.5, 1, 2, 2.75, 3, 4, 6} {
		if got := tm.SecondsF(tm.Ticks(sec)); math.Abs(got-sec) > 1e-9 {
			t.Errorf("round trip at %v gave %v", sec, got)
		}
	}
}

func TestOneAnchorShiftsTheSong(t *testing.T) {
	base := NewTempoMap(192, []Tempo{{Tick: 0, BPM: 120}}, nil, 0)
	tm := base.WithAnchors([]Anchor{{Tick: 192, Sec: 5}})
	if got := tm.Seconds(192); got != 5 {
		t.Errorf("anchor tick at %v, want 5", got)
	}
	if got := tm.Seconds(384); math.Abs(got-5.5) > 1e-9 {
		t.Errorf("one beat later = %v, want 5.5 (120 BPM)", got)
	}
}

func TestWithAnchorsNoPointsIsTheSameMap(t *testing.T) {
	base := NewTempoMap(192, []Tempo{{Tick: 0, BPM: 120}}, nil, 0)
	if base.WithAnchors(nil) != base {
		t.Error("no sync points should give back the same map")
	}
}

func TestValidAnchorsDropsBadPoints(t *testing.T) {
	got := ValidAnchors([]Anchor{
		{Tick: 384, Sec: 3},
		{Tick: 0, Sec: 1},
		{Tick: 192, Sec: 0.5}, // time goes backwards: dropped
		{Tick: 384, Sec: 3.5}, // same tick as 384: the last one wins
	})
	want := []Anchor{{Tick: 0, Sec: 1}, {Tick: 384, Sec: 3.5}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("valid = %+v, want %+v", got, want)
	}
}

const syncChart = "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
	"[Events]\n{\n  0 = E \"lyric hi\"\n  192 = E \"section Chorus\"\n}\n" +
	"[ExpertSingle]\n{\n  0 = N 0 0\n  192 = N 1 0\n  384 = S 2 192\n  384 = N 2 0\n}\n" +
	"[ExpertDrums]\n{\n  0 = N 0 0\n}\n"

func TestSyncPointsRetimeTheWholeChart(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.chart", syncChart)
	// The sync points say the first beat is at 0.5 s and the beat at tick 384 at 1.5 s.
	writeTestFile(t, dir, ProjectFile, `{"format":"jam-project/1","sync":[{"tick":0,"sec":0.5},{"tick":384,"sec":1.5}]}`)
	s, _ := LoadSongFolder(dir)
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	tr := c.Track(Guitar, Expert)
	if tr == nil {
		t.Fatal("no guitar track")
	}
	if got := tr.Notes[1].Time; math.Abs(got-1.0) > 1e-9 { // tick 192 sits halfway
		t.Errorf("note at tick 192 at %v s, want 1.0", got)
	}
	if got := tr.Notes[2].Time; math.Abs(got-1.5) > 1e-9 {
		t.Errorf("note at tick 384 at %v s, want 1.5", got)
	}
	if got := tr.StarPower[0].Start; math.Abs(got-1.5) > 1e-9 {
		t.Errorf("star power starts at %v s, want 1.5", got)
	}
	if len(c.Sections) != 1 || math.Abs(c.Sections[0].Time-1.0) > 1e-9 {
		t.Errorf("section at %+v, want 1.0 s", c.Sections)
	}
	if len(c.Lyrics) != 1 || math.Abs(c.Lyrics[0].Start-0.5) > 1e-9 {
		t.Errorf("chart lyric at %+v, want 0.5 s", c.Lyrics)
	}
	if c.Tempo == nil || len(c.Tempo.Anchors) != 2 {
		t.Errorf("the chart should carry the sync points: %+v", c.Tempo)
	}
}

func TestNoSyncPointsLeavesChartAlone(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.chart", syncChart)
	s, _ := LoadSongFolder(dir)
	c, _ := s.LoadChart()
	if got := c.Track(Guitar, Expert).Notes[1].Time; math.Abs(got-0.5) > 1e-9 {
		t.Errorf("without sync points the note is at %v, want 0.5", got)
	}
}

func TestSyncPointsSaveAndLoadKeepOtherSections(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.chart", syncChart)
	writeProject(t, dir, `{"format":"jam-project/1","vocals":{"source":"aligned"},"tracks":[]}`)
	pts := []Anchor{{Tick: 384, Sec: 2}, {Tick: 0, Sec: 0.25}}
	if err := SaveProjectSync(dir, pts); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProjectSync(dir)
	if err != nil || len(got) != 2 || got[0] != (Anchor{Tick: 0, Sec: 0.25}) || got[1] != (Anchor{Tick: 384, Sec: 2}) {
		t.Fatalf("loaded %+v, err %v", got, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ProjectFile))
	if !strings.Contains(string(b), `"source": "aligned"`) || !strings.Contains(string(b), `"tracks"`) {
		t.Errorf("other sections lost:\n%s", b)
	}
}

// Vocals saved against sync points are timed by those points when loaded.
func TestVocalsFollowSyncPoints(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.chart", syncChart)
	s, _ := LoadSongFolder(dir)
	c, _ := s.LoadChart()
	grid := c.ProjectGridFor(s.Ini)
	parts := []ProjVocalPart{{Name: "Lead", Lead: true, Phrases: []ProjPhrase{{Start: 0, End: 384,
		Syllables: []ProjSyllable{{T: 0, Text: "a"}, {T: 384, Text: "b"}}}}}}
	if err := SaveProjectVocals(dir, parts, &grid); err != nil {
		t.Fatal(err)
	}
	if err := SaveProjectSync(dir, []Anchor{{Tick: 0, Sec: 0.5}, {Tick: 384, Sec: 1.5}}); err != nil {
		t.Fatal(err)
	}
	s2, _ := LoadSongFolder(dir)
	c2, err := s2.LoadChart()
	if err != nil || s2.ProjectErr != nil {
		t.Fatal(err, s2.ProjectErr)
	}
	if len(c2.Lyrics) != 1 || math.Abs(c2.Lyrics[0].Syllables[1].Time-1.5) > 1e-9 {
		t.Errorf("the second word should be at 1.5 s: %+v", c2.Lyrics)
	}
	// The notes and the lyrics agree on the same clock.
	if got := c2.Track(Guitar, Expert).Notes[2].Time; math.Abs(got-c2.Lyrics[0].Syllables[1].Time) > 1e-9 {
		t.Errorf("note at tick 384 (%v) and the word at tick 384 disagree", got)
	}
}

func writeTestFile(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
