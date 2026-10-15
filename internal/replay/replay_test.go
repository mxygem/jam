package replay

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
)

const testChart = `[Song]
{
  Resolution = 192
}
[SyncTrack]
{
  0 = B 120000
}
[Events]
{
  0 = E "section Intro"
  768 = E "section Verse"
}
[ExpertSingle]
{
  192 = N 0 0
  384 = N 1 0
  576 = N 2 0
  768 = N 3 0
  960 = N 4 0
}
`

func TestRecordAnalyzeRoundTrip(t *testing.T) {
	c, err := chart.ParseDotChart(strings.NewReader(testChart), nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := c.Track(chart.Guitar, chart.Expert)
	set := engine.DefaultSettings()
	e := engine.New(c, tr, set)
	rec := NewRecorder(nil, chart.Guitar, chart.Expert, set)
	evs := []action.Event{
		{Time: 0.45, Action: action.Green, Down: true},
		{Time: 0.49, Action: action.StrumDown, Down: true}, // hit, 10ms early
		{Time: 0.95, Action: action.Green, Down: false},
		{Time: 0.95, Action: action.Yellow, Down: true},
		{Time: 1.00, Action: action.StrumDown, Down: true}, // wrong frets for red
		{Time: 1.40, Action: action.Yellow, Down: false},
		{Time: 1.40, Action: action.Yellow, Down: true},
		{Time: 1.62, Action: action.StrumDown, Down: true}, // 120ms late for yellow at 1.5
		{Time: 1.95, Action: action.Blue, Down: true},
		{Time: 2.02, Action: action.StrumDown, Down: true}, // hit, 20ms late
	}
	for _, ev := range evs {
		e.Process(ev)
		rec.Input(ev, "test")
	}
	e.Advance(3)
	r := rec.Finish(e, 3, true)

	dir := t.TempDir()
	path, err := r.Save(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Inputs) != len(evs) || len(loaded.Judgments) != len(r.Judgments) {
		t.Fatalf("round trip lost data: %d/%d inputs, %d/%d judgments", len(loaded.Inputs), len(evs), len(loaded.Judgments), len(r.Judgments))
	}

	// Re-simulating the recorded inputs reproduces the run.
	e2 := Resimulate(c, tr, loaded.Settings, loaded.Events(), 3)
	if e2.Stats().Score != e.Stats().Score || e2.Stats().NotesHit != e.Stats().NotesHit {
		t.Fatalf("resimulation differs: %v/%d vs %v/%d", e2.Stats().Score, e2.Stats().NotesHit, e.Stats().Score, e.Stats().NotesHit)
	}

	a := Analyze(loaded, c, tr)
	if a.Hits != 2 || len(a.Misses) != 3 {
		t.Fatalf("hits=%d misses=%d: %+v", a.Hits, len(a.Misses), a.Misses)
	}
	reasons := []string{a.Misses[0].Reason, a.Misses[1].Reason, a.Misses[2].Reason}
	if !strings.Contains(reasons[0], "wrong frets: held --Y--, needed -R---") {
		t.Errorf("miss 0 reason: %q", reasons[0])
	}
	if !strings.Contains(reasons[1], "too late") {
		t.Errorf("miss 1 reason: %q", reasons[1])
	}
	if reasons[2] != "no strum" {
		t.Errorf("miss 2 reason: %q", reasons[2])
	}
	if a.Sections[0].Name != "Intro" || a.Sections[0].Total != 3 || a.Sections[1].Total != 2 {
		t.Errorf("sections: %+v", a.Sections)
	}
	if a.MeanMs < 4.9 || a.MeanMs > 5.1 { // (-10 + 20) / 2
		t.Errorf("mean %v", a.MeanMs)
	}
	txt := a.Text()
	if !strings.Contains(txt, "Sections:") || !strings.Contains(txt, "Misses:") {
		t.Errorf("report missing parts:\n%s", txt)
	}
	t.Log("\n" + txt)
}

func TestSaveNameStripsTags(t *testing.T) {
	rec := &Recording{Song: SongRef{Artist: "<color=#33ccff><b>gem</b></color>", Name: "김진산 <i>Crow</i>: a/b?"}, Instrument: "guitar", Difficulty: "expert"}
	path, err := rec.Save(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(path)
	if strings.ContainsAny(base, "<>:/?\\\"|*") || !strings.Contains(base, "gem - 김진산 Crow") {
		t.Errorf("file name %q", base)
	}
}
