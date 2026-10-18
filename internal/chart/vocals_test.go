package chart

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lyricEv(tick int64, text string) midiEv {
	return midiEv{tick, append(append([]byte{0xFF, 0x05}, vlq(int64(len(text)))...), text...)}
}

func vocalTrack(name string, withPhrases bool, words ...string) []byte {
	var evs []midiEv
	if withPhrases {
		evs = append(evs, note(0, 1900, 105)...)
	}
	for i, w := range words {
		evs = append(evs, lyricEv(int64(i)*480, w))
	}
	return buildTrack(name, sortEvs(evs))
}

func midiFile(tracks ...[]byte) []byte {
	tempo := buildTrack("tempo", []midiEv{{0, []byte{0xFF, 0x51, 0x03, 0x07, 0xA1, 0x20}}})
	var f bytes.Buffer
	f.WriteString("MThd")
	binary.Write(&f, binary.BigEndian, uint32(6))
	binary.Write(&f, binary.BigEndian, uint16(1))
	binary.Write(&f, binary.BigEndian, uint16(len(tracks)+1))
	binary.Write(&f, binary.BigEndian, uint16(480))
	f.Write(tempo)
	for _, t := range tracks {
		f.Write(t)
	}
	return f.Bytes()
}

func TestMidiHarmonies(t *testing.T) {
	b := midiFile(
		vocalTrack("PART VOCALS", true, "I'm", "in", "a", "band"),
		vocalTrack("HARM1", true, "I'm", "in", "a", "band"),      // same as lead: dropped
		vocalTrack("HARM2", true, "$I'm", "she's", "in", "band"), // "$" lyric hidden
		vocalTrack("HARM3", false, "you're", "in", "a", "band"),  // no phrase markers: uses HARM2's
	)
	c, err := ParseMidiChart(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Vocals) != 3 {
		t.Fatalf("got %d parts: %+v", len(c.Vocals), c.Vocals)
	}
	if !c.Vocals[0].Lead || c.Vocals[0].Name != "Lead" || c.Lyrics[0].Line() != "I'm in a band" {
		t.Errorf("lead part: %+v", c.Vocals[0])
	}
	if c.Vocals[1].Name != "Harmony 2" || c.Vocals[1].Phrases[0].Line() != "she's in band" {
		t.Errorf("harmony 2: %q", c.Vocals[1].Phrases[0].Line())
	}
	if c.Vocals[2].Name != "Harmony 3" || len(c.Vocals[2].Phrases) != 1 || c.Vocals[2].Phrases[0].Line() != "you're in a band" {
		t.Errorf("harmony 3 should borrow HARM2's phrase: %+v", c.Vocals[2])
	}
}

func TestMidiHarm1WithoutLead(t *testing.T) {
	c, err := ParseMidiChart(midiFile(vocalTrack("HARM1", true, "hel-", "lo"), vocalTrack("HARM2", true, "oh")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Vocals) != 2 || !c.Vocals[0].Lead || c.Lyrics[0].Line() != "hello" {
		t.Fatalf("HARM1 should become the lead: %+v", c.Vocals)
	}
}

const projectVocals = `{
 "format": "jam-project/1",
 "grid": {"resolution": 192, "tempos": [{"tick": 0, "bpm": 120.0}], "time_signatures": [{"tick": 0, "num": 4, "den": 4}], "offset": 0.0},
 "parts": {"drums": {"instrument": "drums"}},
 "vocals": {"source": "aligned", "parts": [
  {"name": "Backing", "lead": false, "phrases": [{"start": 192, "end": 576, "syllables": [{"t": 192, "text": "ooh", "join": false}]}]},
  {"name": "Lead", "lead": true, "phrases": [{"start": 0, "end": 384, "syllables": [
     {"t": 0, "text": "but", "join": true}, {"t": 96, "text": "ter", "join": true}, {"t": 192, "text": "fly", "join": false}]}]}
 ]}
}`

func TestProjectVocalsReplaceChartLyrics(t *testing.T) {
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[Events]\n{\n  0 = E \"lyric old\"\n}\n" +
		"[ExpertDrums]\n{\n  0 = N 0 0\n}\n"
	os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(chartText), 0o644)
	os.WriteFile(filepath.Join(dir, ProjectFile), []byte(projectVocals), 0o644)
	s, err := LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	if len(c.Vocals) != 2 || c.Vocals[0].Name != "Lead" || c.Lyrics[0].Line() != "butterfly" {
		t.Fatalf("parts: %+v", c.Vocals)
	}
	if got := c.Vocals[1].Phrases[0]; got.Line() != "ooh" || !near(got.Start, 0.5) || !near(got.Syllables[0].Time, 0.5) {
		t.Errorf("backing timing: %+v", got)
	}
}

func TestBrokenProjectKeepsChartLyrics(t *testing.T) {
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[Events]\n{\n  0 = E \"lyric kept\"\n}\n" +
		"[ExpertDrums]\n{\n  0 = N 0 0\n}\n"
	os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(chartText), 0o644)
	os.WriteFile(filepath.Join(dir, ProjectFile), []byte("{not json"), 0o644)
	s, _ := LoadSongFolder(dir)
	c, err := s.LoadChart()
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectErr == nil || !strings.Contains(s.ProjectErr.Error(), ProjectFile) {
		t.Errorf("expected a project error, got %v", s.ProjectErr)
	}
	if len(c.Lyrics) != 1 || c.Lyrics[0].Line() != "kept" {
		t.Errorf("chart lyrics should stay: %+v", c.Lyrics)
	}
}
