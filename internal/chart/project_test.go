package chart

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const projTestChart = "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
	"[ExpertSingle]\n{\n  0 = N 0 0\n  192 = N 1 0\n  384 = S 2 384\n  384 = N 2 0\n  576 = N 3 0\n}\n" +
	"[ExpertDrums]\n{\n  0 = N 0 0\n}\n"

func loadTestSong(t *testing.T, dir string) *Chart {
	t.Helper()
	s, err := LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LoadChart()
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectErr != nil {
		t.Fatalf("project error: %v", s.ProjectErr)
	}
	return c
}

func writeProject(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestSongDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(projTestChart), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProjectTracksReplaceChartNotes(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","tracks":[
	  {"instrument":"guitar","difficulty":"expert","resolution":192,"notes":[
	    {"tick":0,"lanes":[0,2],"length":96},
	    {"tick":384,"lanes":[3]},
	    {"tick":576,"lanes":[4],"kind":"tap"}]}]}`)
	c := loadTestSong(t, dir)
	tr := c.Track(Guitar, Expert)
	if tr == nil || len(tr.Notes) != 3 {
		t.Fatalf("want 3 notes, got %+v", tr)
	}
	n0 := tr.Notes[0]
	if n0.Lanes != 1<<LaneGreen|1<<LaneYellow {
		t.Errorf("chord lanes = %08b", n0.Lanes)
	}
	if n0.EndTick[LaneGreen] != 96 || n0.EndTick[LaneYellow] != 96 {
		t.Errorf("sustain ends = %v", n0.EndTick)
	}
	if !tr.Notes[1].StarPower || !tr.Notes[2].StarPower {
		t.Errorf("star power phrase lost: %+v", tr.Notes)
	}
	if tr.Notes[2].Kind != Tap {
		t.Errorf("kind = %v, want tap", tr.Notes[2].Kind)
	}
	if len(tr.StarPower) != 1 || tr.StarPower[0].StartTick != 384 {
		t.Errorf("phrase not kept: %+v", tr.StarPower)
	}
}

func TestProjectTracksScaleResolution(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","tracks":[
	  {"instrument":"guitar","difficulty":"expert","resolution":96,"notes":[
	    {"tick":48,"lanes":[1],"length":24}]}]}`)
	tr := loadTestSong(t, dir).Track(Guitar, Expert)
	if tr == nil || len(tr.Notes) != 1 {
		t.Fatalf("notes: %+v", tr)
	}
	if tr.Notes[0].Tick != 96 || tr.Notes[0].EndTick[LaneRed] != 144 {
		t.Errorf("scaled tick %d end %d, want 96 and 144", tr.Notes[0].Tick, tr.Notes[0].EndTick[LaneRed])
	}
}

func TestProjectTrackWithoutNotesRemovesPart(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","tracks":[
	  {"instrument":"guitar","difficulty":"expert","resolution":192,"notes":[]}]}`)
	c := loadTestSong(t, dir)
	if c.Track(Guitar, Expert) != nil {
		t.Errorf("empty authored track should remove the part")
	}
	if c.Track(Drums, Expert) == nil {
		t.Errorf("other parts must stay")
	}
}

// The drum part in the project replaces the chart's drums, with its mode,
// cymbals, accents, ghosts and double kick.
func TestProjectDrumsReplaceChartDrums(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","tracks":[
	  {"instrument":"drums","difficulty":"expert","resolution":192,"mode":"pro","notes":[
	    {"tick":0,"lanes":[0,2],"cymbals":[2],"two_x":true},
	    {"tick":192,"lanes":[3,4],"cymbals":[3,4],"accents":[3],"ghosts":[4]}]}]}`)
	c := loadTestSong(t, dir)
	tr := c.Track(Drums, Expert)
	if tr == nil || len(tr.Notes) != 2 {
		t.Fatalf("drum notes: %+v", tr)
	}
	if tr.DrumMode != DrumsPro {
		t.Errorf("mode %v, want pro", tr.DrumMode)
	}
	n0, n1 := tr.Notes[0], tr.Notes[1]
	if !n0.DoubleKick || n0.Cymbals != 1<<DrumYellow || n1.Cymbals != 1<<DrumBlue|1<<DrumGreen4 {
		t.Errorf("notes 0 %+v, 1 %+v", n0, n1)
	}
	if n1.Accents != 1<<DrumBlue || n1.Ghosts != 1<<DrumGreen4 {
		t.Errorf("marks on note 1: accents %08b ghosts %08b", n1.Accents, n1.Ghosts)
	}
	if got := c.ProjectNotes(TrackKey{Drums, Expert}); len(got) != 2 || !got[0].TwoX || len(got[1].Cymbals) != 2 {
		t.Errorf("drum notes out: %+v", got)
	}
}

// Drum notes written by the editor read back the same, mode included.
func TestDrumNotesRoundTripThroughSave(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","tracks":[
	  {"instrument":"drums","difficulty":"expert","resolution":192,"mode":"5-lane","notes":[
	    {"tick":0,"lanes":[0,4,5],"accents":[4],"two_x":true},{"tick":96,"lanes":[2],"ghosts":[2]}]}]}`)
	c := loadTestSong(t, dir)
	want := c.ProjectNotes(TrackKey{Drums, Expert})
	tracks, err := LoadProjectTracks(dir)
	if err != nil {
		t.Fatal(err)
	}
	pt := ProjTrack{Instrument: "drums", Difficulty: "expert", Resolution: 192, Mode: "5-lane", Notes: want}
	if err := SaveProjectTracks(dir, SetProjectTrack(tracks, TrackKey{Drums, Expert}, pt)); err != nil {
		t.Fatal(err)
	}
	c2 := loadTestSong(t, dir)
	if got := c2.ProjectNotes(TrackKey{Drums, Expert}); !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
	}
	if c2.Track(Drums, Expert).DrumMode != DrumsFiveLane {
		t.Errorf("five-lane mode lost")
	}
}

// A drum part with no notes removes the part, like any other.
func TestEmptyProjectDrumsRemovePart(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","tracks":[
	  {"instrument":"drums","difficulty":"expert","resolution":192,"notes":[]}]}`)
	if loadTestSong(t, dir).Track(Drums, Expert) != nil {
		t.Errorf("an empty project drum part should remove the chart's drums")
	}
}

func TestProjectNotesRoundTripThroughSave(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, `{"format":"jam-project/1","grid":{"resolution":192,"tempos":[{"tick":0,"bpm":120}]},
	  "vocals":{"parts":[{"name":"Lead","lead":true,"phrases":[{"start":0,"end":192,"syllables":[{"t":0,"text":"hi"}]}]}]},
	  "tracks":[]}`)
	c := loadTestSong(t, dir)
	want := c.ProjectNotes(TrackKey{Guitar, Expert})
	if len(want) != 4 {
		t.Fatalf("chart notes: %+v", want)
	}
	tracks, err := LoadProjectTracks(dir)
	if err != nil {
		t.Fatal(err)
	}
	pt := ProjTrack{Instrument: "guitar", Difficulty: "expert", Resolution: c.Resolution, Notes: want}
	if err := SaveProjectTracks(dir, SetProjectTrack(tracks, TrackKey{Guitar, Expert}, pt)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ProjectFile))
	if !strings.Contains(string(b), `"vocals"`) || !strings.Contains(string(b), `"grid"`) {
		t.Fatalf("save dropped other sections:\n%s", b)
	}
	c2 := loadTestSong(t, dir)
	got := c2.ProjectNotes(TrackKey{Guitar, Expert})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed notes:\n got %+v\nwant %+v", got, want)
	}
	if len(c2.Vocals) == 0 || c2.Vocals[0].Phrases[0].Line() != "hi" {
		t.Errorf("vocals lost after save: %+v", c2.Vocals)
	}
}

func TestSaveProjectRefusesBrokenFile(t *testing.T) {
	dir := newTestSongDir(t)
	writeProject(t, dir, "{not json")
	err := SaveProjectTracks(dir, nil)
	if err == nil {
		t.Fatal("saving over a broken project file must fail")
	}
	b, _ := os.ReadFile(filepath.Join(dir, ProjectFile))
	if string(b) != "{not json" {
		t.Errorf("broken file was overwritten: %q", b)
	}
}

func TestSaveProjectCreatesFile(t *testing.T) {
	dir := newTestSongDir(t)
	pt := ProjTrack{Instrument: "bass", Difficulty: "hard", Resolution: 192, Notes: []ProjNote{{Tick: 0, Lanes: []int{0}}}}
	if err := SaveProjectTracks(dir, SetProjectTrack(nil, TrackKey{Bass, Hard}, pt)); err != nil {
		t.Fatal(err)
	}
	tracks, err := LoadProjectTracks(dir)
	if err != nil || len(tracks) != 1 || tracks[0].Instrument != "bass" {
		t.Fatalf("tracks %+v, err %v", tracks, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ProjectFile+".tmp")); err == nil {
		t.Errorf("temporary file left behind")
	}
}
