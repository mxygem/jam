package chart

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vocalsChart = "[Song]\n{\n  Resolution = 192\n  Offset = 0.25\n}\n[SyncTrack]\n{\n  0 = B 120000\n  768 = B 180000\n}\n" +
	"[Events]\n{\n  0 = E \"lyric old\"\n}\n[ExpertDrums]\n{\n  0 = N 0 0\n}\n"

func TestProjectVocalsKeepEffectsAndOtherKeys(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, `{"format":"jam-project/1","vocals":{"source":"aligned","parts":[
	  {"name":"Lead","lead":true,"fx":["wave"],"phrases":[{"start":0,"end":192,"syllables":[
	    {"t":0,"text":"hel","join":true,"fx":["shake:2"]},{"t":96,"text":"lo","fx":["color:#ff4d6d"]}]}]}]}}`)
	pv, err := LoadProjectVocals(dir)
	if err != nil || len(pv.Parts) != 1 {
		t.Fatalf("load: %+v %v", pv, err)
	}
	pv.Parts[0].Phrases[0].Syllables[1].Text = "lo!"
	if err := SaveProjectVocals(dir, pv.Parts, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ProjectFile))
	s := string(b)
	for _, want := range []string{`"source": "aligned"`, `"shake:2"`, `"color:#ff4d6d"`, `"wave"`, `"lo!"`} {
		if !strings.Contains(s, want) {
			t.Errorf("saved file lost %s:\n%s", want, s)
		}
	}
}

func TestLegacyPhrasesLoadAsLead(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, `{"format":"jam-project/1","vocals":{"phrases":[{"start":0,"end":96,"syllables":[{"t":0,"text":"la"}]}]}}`)
	pv, err := LoadProjectVocals(dir)
	if err != nil || len(pv.Parts) != 1 || !pv.Parts[0].Lead || pv.Parts[0].Phrases[0].Syllables[0].Text != "la" {
		t.Fatalf("legacy vocals: %+v %v", pv, err)
	}
}

func TestSaveVocalsRefusesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, `{"something":"else"}`)
	if err := SaveProjectVocals(dir, nil, nil); err == nil {
		t.Fatal("a file that isn't a jam project must not be overwritten")
	}
	b, _ := os.ReadFile(filepath.Join(dir, ProjectFile))
	if string(b) != `{"something":"else"}` {
		t.Errorf("file changed: %s", b)
	}
}

// The grid the editor writes must put project times where the chart puts
// them, delay and tempo changes included.
func TestProjectGridMatchesChartTiming(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(vocalsChart), 0o644)
	os.WriteFile(filepath.Join(dir, "song.ini"), []byte("[song]\ndelay = 100\n"), 0o644)
	s, err := LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LoadChart()
	if err != nil {
		t.Fatal(err)
	}
	grid := c.ProjectGridFor(s.Ini)
	if err := SaveProjectVocals(dir, nil, &grid); err != nil {
		t.Fatal(err)
	}
	tm, err := c.ProjectTempo(dir, s.Ini)
	if err != nil {
		t.Fatal(err)
	}
	for _, tick := range []int64{0, 96, 768, 1000, 3000} {
		if got, want := tm.Seconds(tick), c.Tempo.Seconds(tick); math.Abs(got-want) > 1e-9 {
			t.Errorf("tick %d: grid %.6f, chart %.6f", tick, got, want)
		}
	}
}

func TestProjectTempoWithoutGridIsChartTempo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(vocalsChart), 0o644)
	s, _ := LoadSongFolder(dir)
	c, _ := s.LoadChart()
	tm, err := c.ProjectTempo(dir, s.Ini)
	if err != nil || tm != c.Tempo {
		t.Fatalf("no grid should use the chart's tempo: %v", err)
	}
}

// Vocals saved with the editor's grid are what the game loads back.
func TestSavedVocalsLoadBackAtTheSameTimes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(vocalsChart), 0o644)
	s, _ := LoadSongFolder(dir)
	c, _ := s.LoadChart()
	grid := c.ProjectGridFor(s.Ini)
	parts := []ProjVocalPart{{Name: "Lead", Lead: true, Phrases: []ProjPhrase{{Start: 192, End: 384,
		Syllables: []ProjSyllable{{T: 192, Text: "one"}, {T: 768, Text: "two"}}}}}}
	if err := SaveProjectVocals(dir, parts, &grid); err != nil {
		t.Fatal(err)
	}
	c2, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	if len(c2.Vocals) != 1 || c2.Lyrics[0].Line() != "one two" {
		t.Fatalf("vocals: %+v", c2.Vocals)
	}
	tm := c.Tempo
	if got, want := c2.Lyrics[0].Syllables[1].Time, tm.Seconds(768); math.Abs(got-want) > 1e-9 {
		t.Errorf("syllable time %.6f, want %.6f", got, want)
	}
	var check json.RawMessage
	b, _ := os.ReadFile(filepath.Join(dir, ProjectFile))
	if err := json.Unmarshal(b, &check); err != nil {
		t.Fatalf("project file is not JSON: %v", err)
	}
}
