package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mxygem/jam/internal/chart"
)

func twoLines() []chart.ProjVocalPart {
	return []chart.ProjVocalPart{
		{Name: "Harmony 1", Lead: false, Phrases: []chart.ProjPhrase{{Start: 0, End: 96, Syllables: []chart.ProjSyllable{{T: 0, Text: "oo"}}}}},
		{Name: "Lead", Lead: true, FX: []string{"wave"}, Phrases: []chart.ProjPhrase{
			{Start: 0, End: 384, FX: []string{"zoom:2"}, Syllables: []chart.ProjSyllable{
				{T: 0, Text: "hel", Join: true, FX: []string{"shake:2"}},
				{T: 96, Text: "lo", FX: []string{"color:#ff4d6d"}},
			}},
			{Start: 384, End: 576, Syllables: []chart.ProjSyllable{{T: 384, Text: "world"}}},
		}},
	}
}

func TestLyricEditFindsLeadPart(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	if l.leadPart().Name != "Lead" {
		t.Fatalf("lead = %q", l.leadPart().Name)
	}
	if got := l.Lines(); !reflect.DeepEqual(got, []string{"hello", "world"}) {
		t.Errorf("lines %q", got)
	}
	if s := l.Selected(); s == nil || s.Text != "hel" {
		t.Errorf("first syllable should be selected, got %+v", s)
	}
}

func TestLyricEditAddsLeadWhenMissing(t *testing.T) {
	l := newLyricEdit([]chart.ProjVocalPart{{Name: "Harmony", Lead: false}}, 192)
	if len(l.Parts()) != 2 || !l.Parts()[l.lead].Lead || l.Parts()[0].Lead {
		t.Errorf("a new lead should be added beside the harmony: %+v", l.Parts())
	}
}

func TestStampMovesToNextSyllable(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.Stamp(200)
	if s := l.leadPart().Phrases[0].Syllables[0]; s.T != 200 {
		t.Errorf("stamped time %d", s.T)
	}
	if l.Selected().Text != "lo" {
		t.Errorf("selection should move on to %q, got %q", "lo", l.Selected().Text)
	}
	if !l.dirty {
		t.Error("stamping is an edit")
	}
}

func TestStampKeepsPhraseBounds(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.sel = lyricPos{1, 0}
	l.Stamp(900) // the "world" syllable moves later
	ph := l.leadPart().Phrases[1]
	if ph.Start != 900 || ph.End < 900 {
		t.Errorf("phrase bounds %d-%d after stamp at 900", ph.Start, ph.End)
	}
}

func TestTypeAndBackspace(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.sel = lyricPos{0, 1} // "lo"
	l.TypeText("!", 0)
	if l.Selected().Text != "lo!" {
		t.Errorf("typed: %q", l.Selected().Text)
	}
	l.Backspace()
	l.Backspace()
	if l.Selected().Text != "l" {
		t.Errorf("backspace: %q", l.Selected().Text)
	}
	l.Backspace() // empties the text
	if l.Selected().Text != "" {
		t.Fatalf("text should be empty: %q", l.Selected().Text)
	}
	l.Backspace() // an empty syllable goes
	if len(l.leadPart().Phrases[0].Syllables) != 1 {
		t.Errorf("empty syllable should be deleted: %+v", l.leadPart().Phrases[0].Syllables)
	}
}

func TestBackspaceRemovesWholeRune(t *testing.T) {
	l := newLyricEdit([]chart.ProjVocalPart{{Lead: true, Phrases: []chart.ProjPhrase{{Syllables: []chart.ProjSyllable{{Text: "café"}}}}}}, 192)
	l.Backspace()
	if l.Selected().Text != "caf" {
		t.Errorf("backspace on a multi-byte letter: %q", l.Selected().Text)
	}
}

func TestInsertAndDelete(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.sel = lyricPos{0, 0}
	l.Insert(48)
	// "hel" joins the empty syllable, which doesn't join the next, so a space follows it.
	if got := l.Lines(); !reflect.DeepEqual(got, []string{"hel lo", "world"}) {
		t.Errorf("lines after insert %q", got)
	}
	if l.Selected().T != 48 || l.Selected().Text != "" {
		t.Errorf("new syllable: %+v", l.Selected())
	}
	l.DeleteSelected()
	if len(l.leadPart().Phrases[0].Syllables) != 2 {
		t.Errorf("delete: %+v", l.leadPart().Phrases[0].Syllables)
	}
}

func TestDeletingLastSyllableRemovesPhrase(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.sel = lyricPos{1, 0}
	l.DeleteSelected()
	if n := len(l.leadPart().Phrases); n != 1 {
		t.Errorf("phrases after deleting a line's only word: %d", n)
	}
}

func TestSplitAndJoinLines(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.sel = lyricPos{0, 0} // "hel" stays; "lo" goes to a new line
	if !l.SplitLine() {
		t.Fatal("split should work")
	}
	if got := l.Lines(); !reflect.DeepEqual(got, []string{"hel", "lo", "world"}) {
		t.Errorf("after split: %q", got)
	}
	if l.leadPart().Phrases[1].Start != 96 {
		t.Errorf("new line should start on its first syllable, got %d", l.leadPart().Phrases[1].Start)
	}
	if !l.JoinLine() {
		t.Fatal("join should work")
	}
	if got := l.Lines(); !reflect.DeepEqual(got, []string{"hello", "world"}) {
		t.Errorf("after join: %q", got)
	}
}

func TestSplitAtEndOfLineDoesNothing(t *testing.T) {
	l := newLyricEdit(twoLines(), 192)
	l.sel = lyricPos{0, 1}
	if l.SplitLine() {
		t.Error("nothing follows the last syllable of a line")
	}
}

// Effects on syllables the editor doesn't touch come through a save unchanged.
func TestEffectsSurviveEditing(t *testing.T) {
	parts := twoLines()
	l := newLyricEdit(parts, 192)
	l.sel = lyricPos{1, 0}
	l.Stamp(400)
	l.TypeText("!", 0)
	s := l.leadPart().Phrases[0].Syllables[0]
	if !reflect.DeepEqual(s.FX, []string{"shake:2"}) {
		t.Errorf("effect on untouched syllable: %v", s.FX)
	}
	if !reflect.DeepEqual(l.leadPart().FX, []string{"wave"}) || !reflect.DeepEqual(l.leadPart().Phrases[0].FX, []string{"zoom:2"}) {
		t.Errorf("part or phrase effects changed: %+v", l.leadPart())
	}
	if !reflect.DeepEqual(l.Parts()[0].Phrases[0].Syllables[0].Text, "oo") {
		t.Errorf("other parts must stay as they were")
	}
}

func TestLyricPartsFromChartKeepsTimingAndEffects(t *testing.T) {
	dir := t.TempDir()
	c := lyricChart(t, dir)
	tm := c.Tempo
	parts := lyricPartsFromChart(c, tm)
	if len(parts) != 1 || !parts[0].Lead {
		t.Fatalf("parts: %+v", parts)
	}
	ph := parts[0].Phrases[0]
	if ph.Syllables[0].T != 0 || ph.Syllables[1].T != 192 {
		t.Errorf("tick times: %+v", ph.Syllables)
	}
	if !reflect.DeepEqual(ph.Syllables[0].FX, []string{"shake:2"}) {
		t.Errorf("effects from the chart: %v", ph.Syllables[0].FX)
	}
}

func lyricChart(t *testing.T, dir string) *chart.Chart {
	t.Helper()
	text := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
		"[Events]\n{\n  0 = E \"lyric <fx=shake:2>hel-\"\n  192 = E \"lyric lo\"\n}\n" +
		"[ExpertDrums]\n{\n  0 = N 0 0\n}\n"
	writeTestFile(t, dir, "notes.chart", text)
	s, err := chart.LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LoadChart()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeTestFile(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
