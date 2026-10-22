package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
)

// vizChart is 120 BPM with four 8-bar sections of 8th notes: single notes,
// a chord every fourth note, some sustains and a star power phrase.
func vizChart() string {
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[Events]\n{\n")
	for i, name := range []string{"Intro", "Verse", "Chorus", "Outro"} {
		fmt.Fprintf(&b, "  %d = E \"section %s\"\n", 768+i*8*768, name)
	}
	b.WriteString("}\n[ExpertSingle]\n{\n")
	r := rand.New(rand.NewSource(7))
	for tick := 768; tick < 768+32*768; tick += 96 {
		lane := r.Intn(5)
		fmt.Fprintf(&b, "  %d = N %d 0\n", tick, lane)
		if (tick/96)%4 == 0 {
			fmt.Fprintf(&b, "  %d = N %d 0\n", tick, (lane+2)%5)
		}
		if (tick/96)%16 == 3 {
			fmt.Fprintf(&b, "  %d = N %d 150\n", tick, lane)
		}
	}
	fmt.Fprintf(&b, "  %d = S 2 %d\n", 768+8*768, 4*768)
	b.WriteString("}\n")
	return b.String()
}

// fakePlayer plays a guitar track: frets land early, the strum is off by
// a random amount around bias, and in the chorus a note is skipped with
// probability choke (an extra strum follows it, as players do).
func fakePlayer(tr *chart.Track, c *chart.Chart, seed int64, bias, spread, choke float64) []action.Event {
	r := rand.New(rand.NewSource(seed))
	frets := [5]action.Action{action.Green, action.Red, action.Yellow, action.Blue, action.Orange}
	var evs []action.Event
	held := uint8(0)
	for i := range tr.Notes {
		n := &tr.Notes[i]
		want := n.Lanes & chart.FretMask
		at := n.Time - 0.05
		for l := 0; l < 5; l++ {
			bit := uint8(1) << l
			if held&bit != 0 && want&bit == 0 {
				evs = append(evs, action.Event{Time: at, Action: frets[l], Down: false})
			}
			if want&bit != 0 && held&bit == 0 {
				evs = append(evs, action.Event{Time: at, Action: frets[l], Down: true})
			}
		}
		held = want
		if c.SectionAt(n.Time) == "Chorus" && r.Float64() < choke {
			// Fumbled: strum way late, an overstrum and a miss.
			evs = append(evs, action.Event{Time: n.Time + 0.11, Action: action.StrumDown, Down: true})
			evs = append(evs, action.Event{Time: n.Time + 0.12, Action: action.StrumDown, Down: false})
			continue
		}
		off := bias + r.NormFloat64()*spread
		off = math.Max(-0.06, math.Min(0.06, off))
		evs = append(evs, action.Event{Time: n.Time + off, Action: action.StrumDown, Down: true})
		evs = append(evs, action.Event{Time: n.Time + off + 0.02, Action: action.StrumDown, Down: false})
	}
	// Events must arrive in time order.
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].Time < evs[j-1].Time; j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
	return evs
}

// fakeHistory records attempts that get better: less chorus choking and a
// tighter, more centered timing each time.
func fakeHistory(t *testing.T, dir string, attempts int) (*chart.Chart, *chart.Track, []string) {
	t.Helper()
	c, err := chart.ParseDotChart(strings.NewReader(vizChart()), nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := c.Track(chart.Guitar, chart.Expert)
	song := &chart.Song{Name: "Viz Test", Artist: "jam", Dir: "/songs/viz"}
	var paths []string
	start := time.Date(2026, 10, 20, 20, 0, 0, 0, time.UTC)
	for k := 0; k < attempts; k++ {
		f := float64(k) / float64(max(1, attempts-1))
		set := engine.DefaultSettings()
		e := engine.New(c, tr, set)
		rec := NewRecorder(song, chart.Guitar, chart.Expert, set)
		rec.Rec.Started = start.Add(time.Duration(k) * 7 * time.Minute)
		rec.Rec.Profile, rec.Rec.Player = "p1", "Gem"
		end := tr.Notes[len(tr.Notes)-1].SustainEnd() + 1
		for _, ev := range fakePlayer(tr, c, int64(k+1), -0.022+0.02*f, 0.026-0.014*f, 0.45-0.4*f) {
			e.Process(ev)
			rec.Input(ev, "test")
		}
		e.Advance(end)
		r := rec.Finish(e, end, true)
		p, err := r.Save(dir)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return c, tr, paths
}

func TestRunView(t *testing.T) {
	dir := t.TempDir()
	c, tr, paths := fakeHistory(t, dir, 1)
	rec, err := Load(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	v := BuildRunView(rec, c, tr, nil)
	if len(v.Rows) != 5 {
		t.Fatalf("rows = %d, want 5 frets", len(v.Rows))
	}
	if len(v.Gems) != len(tr.Notes) {
		t.Fatalf("gems = %d, want one per note (%d)", len(v.Gems), len(tr.Notes))
	}
	hits, misses := 0, 0
	for _, g := range v.Gems {
		switch g.State {
		case GemHit:
			hits++
		case GemMissed:
			misses++
			if g.Cat == "" || g.Reason == "" {
				t.Errorf("miss at %.2f has no reason", g.Time)
			}
		}
	}
	if hits != rec.Result.NotesHit || misses != rec.Result.NotesMissed {
		t.Fatalf("gems hit/missed %d/%d, recording says %d/%d", hits, misses, rec.Result.NotesHit, rec.Result.NotesMissed)
	}
	if misses == 0 || len(v.Errors) == 0 {
		t.Fatalf("the fake player should miss and overstrum in the chorus (misses %d, overstrums %d)", misses, len(v.Errors))
	}
	// The grid adds up to the sections: every fret of every note.
	for si, s := range v.Sections {
		frets, chordNotes := 0, 0
		for i := range tr.Notes {
			n := &tr.Notes[i]
			if n.Time >= s.Start && n.Time < s.End || (si == len(v.Sections)-1 && n.Time >= s.Start) {
				frets += n.Count()
				if n.IsChord() {
					chordNotes++
				}
			}
		}
		got := 0
		for r := range v.Rows {
			got += v.Grid[r][si].Total
		}
		if got != frets {
			t.Errorf("section %s: grid has %d gems, chart %d", s.Name, got, frets)
		}
		if s.Name == "Chorus" && (s.Misses == 0 || s.MissBy[MissLate] == 0) {
			t.Errorf("chorus misses %d by category %v, want late misses", s.Misses, s.MissBy)
		}
		if s.Judged() > 0 && s.LongestStreak == 0 {
			t.Errorf("section %s: no streak", s.Name)
		}
	}
	if len(v.SPSpans) != 1 || len(v.Streak) == 0 {
		t.Errorf("star power spans %d, streak points %d", len(v.SPSpans), len(v.Streak))
	}
	if _, err := json.Marshal(v); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryAndReport(t *testing.T) {
	dir := t.TempDir()
	c, tr, paths := fakeHistory(t, dir, 6)
	// Someone else's run of the same part doesn't count.
	other, _ := Load(paths[0])
	other.Profile, other.Player = "p2", "Etheno"
	other.Started = other.Started.Add(time.Hour)
	if _, err := other.Save(dir); err != nil {
		t.Fatal(err)
	}
	own := func(id string) bool { return id == "p1" }
	found := FindRuns(dir, "/songs/viz", "guitar", "expert", own)
	if len(found) != 6 {
		t.Fatalf("found %d runs, want 6", len(found))
	}
	if all := FindRuns(dir, "/songs/viz", "guitar", "expert", nil); len(all) != 7 {
		t.Fatalf("found %d runs for everyone, want 7", len(all))
	}
	last := paths[len(paths)-1]
	rec, _ := Load(last)
	v, h := BuildReport(rec, last, c, tr, "", dir, own)
	if len(h.Attempts) != 6 {
		t.Fatalf("attempts = %d", len(h.Attempts))
	}
	if !h.Attempts[5].Current {
		t.Fatal("the run the report is for should be the current attempt")
	}
	chorus := -1
	for i, s := range h.Sections {
		if s.Name == "Chorus" {
			chorus = i
		}
	}
	first, latest := h.Attempts[0].SectionAcc[chorus], h.Attempts[5].SectionAcc[chorus]
	if latest <= first {
		t.Errorf("chorus accuracy %.2f -> %.2f, the fake player improves", first, latest)
	}
	reached, missed, fixed := 0, 0, 0
	for _, g := range h.Gems {
		reached += g.Reached
		missed += g.Missed
		if g.Missed > 0 && g.HitStreak > 0 {
			fixed++
		}
	}
	if reached != 6*len(tr.Notes) {
		t.Errorf("gems reached %d times, want %d", reached, 6*len(tr.Notes))
	}
	if missed == 0 || fixed == 0 {
		t.Errorf("missed %d, fixed since %d", missed, fixed)
	}
	for _, a := range h.Attempts {
		n := 0
		for _, c := range a.Hist {
			n += c
		}
		if n != a.Hits || a.Hits == 0 {
			t.Errorf("histogram holds %d of %d hits", n, a.Hits)
		}
	}
	var buf bytes.Buffer
	if err := WriteReport(&buf, v, h); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	if strings.Contains(page, "{{DATA}}") || !strings.Contains(page, `"attempts"`) || !strings.Contains(page, "Viz Test · run report") {
		t.Fatal("report page is missing its data")
	}
	if out := os.Getenv("JAM_REPORT_OUT"); out != "" {
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
