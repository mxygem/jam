package chart

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fxNames(fxs []FX) string {
	var n []string
	for _, f := range fxs {
		n = append(n, f.Name)
	}
	return strings.Join(n, ",")
}

func TestParseFX(t *testing.T) {
	if f, ok := ParseFX("Shake:2.5"); !ok || f.Name != "shake" || f.Arg != 2.5 {
		t.Errorf("shake: %+v %v", f, ok)
	}
	if f, ok := ParseFX("color:#ff4d6d"); !ok || f.Color != (color.RGBA{0xff, 0x4d, 0x6d, 0xff}) {
		t.Errorf("color: %+v %v", f, ok)
	}
	if _, ok := ParseFX("color:nope"); ok {
		t.Error("a color effect needs a color")
	}
	if _, ok := ParseFX("sparkle"); ok {
		t.Error("unknown effects are skipped")
	}
	if got := fxNames(ParseFXList([]string{"glow", "sparkle", "zoom:2"})); got != "glow,zoom" {
		t.Errorf("list: %s", got)
	}
}

func TestInlineFXAcrossSyllables(t *testing.T) {
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[Events]\n{\n" +
		"  0 = E \"phrase_start\"\n" +
		"  0 = E \"lyric <fx=shake:2>BURN\"\n" +
		"  96 = E \"lyric IT\"\n" +
		"  192 = E \"lyric DOWN</fx>\"\n" +
		"  288 = E \"lyric <fx=color:#00ff00,glow>now</fx>\"\n" +
		"  384 = E \"lyric plain\"\n" +
		"  480 = E \"phrase_end\"\n}\n"
	c := mustChart(t, chartText, nil)
	if len(c.Lyrics) != 1 {
		t.Fatalf("phrases: %+v", c.Lyrics)
	}
	p := c.Lyrics[0]
	if p.Line() != "BURN IT DOWN now plain" {
		t.Errorf("tags should be gone from the text: %q", p.Line())
	}
	want := []string{"shake", "shake", "shake", "color,glow", ""}
	for i, s := range p.Syllables {
		if got := fxNames(s.FX); got != want[i] {
			t.Errorf("syllable %d %q: fx %q, want %q", i, s.Text, got, want[i])
		}
	}
	if f, _ := FindFX(p.Syllables[3].FX, "color"); f.Color.G != 0xff {
		t.Errorf("inline color lost: %+v", f)
	}
}

const projectFX = `{
 "format": "jam-project/1",
 "grid": {"resolution": 192, "tempos": [{"tick": 0, "bpm": 120.0}], "time_signatures": [{"tick": 0, "num": 4, "den": 4}], "offset": 0.0},
 "vocals": {"parts": [
  {"name": "Lead", "lead": true, "fx": ["wave"], "phrases": [
    {"start": 0, "end": 384, "fx": ["glow"], "syllables": [
      {"t": 0, "text": "Here", "join": false, "fx": []},
      {"t": 192, "text": "GO", "join": false, "fx": ["zoom:2", "wave:0.5"]}]}]}
 ]}
}`

func TestProjectFXMerge(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.chart"), []byte("[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[ExpertDrums]\n{\n  0 = N 0 0\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ProjectFile), []byte(projectFX), 0o644)
	s, err := LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	syl := c.Lyrics[0].Syllables
	if got := fxNames(syl[0].FX); got != "wave,glow" {
		t.Errorf("part and phrase effects on the first syllable: %q", got)
	}
	if got := fxNames(syl[1].FX); got != "wave,glow,zoom" {
		t.Errorf("merged effects: %q", got)
	}
	if w, _ := FindFX(syl[1].FX, "wave"); w.Arg != 0.5 {
		t.Errorf("the syllable's own wave should win: %+v", w)
	}
}
