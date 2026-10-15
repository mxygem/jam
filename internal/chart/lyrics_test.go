package chart

import (
	"strings"
	"testing"
)

func TestDotChartLyrics(t *testing.T) {
	src := `[Song]
{
  Resolution = 192
}
[SyncTrack]
{
  0 = B 120000
}
[Events]
{
  0 = E "section Verse"
  192 = E "phrase_start"
  192 = E "lyric Hel-"
  288 = E "lyric lo#"
  384 = E "lyric <color=#ff0000>world</color>"
  480 = E "lyric +"
  576 = E "lyric it's"
  672 = E "lyric self=" 
  768 = E "lyric aware^"
  864 = E "phrase_end"
  960 = E "phrase_start"
  960 = E "lyric sec_ond"
  1056 = E "lyric line$"
  1152 = E "phrase_end"
  1300 = E "lyric stray"
}
[ExpertSingle]
{
  192 = N 0 0
}
`
	c, err := ParseDotChart(strings.NewReader(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Lyrics) != 2 {
		t.Fatalf("got %d phrases: %+v", len(c.Lyrics), c.Lyrics)
	}
	if got := c.Lyrics[0].Line(); got != "Hello <color=#ff0000>world</color> it's self-aware" {
		t.Errorf("line 1 = %q", got)
	}
	if got := c.Lyrics[1].Line(); got != "sec ond line" {
		t.Errorf("line 2 = %q", got)
	}
	p := c.Lyrics[0]
	if p.Start != 0.5 || p.End != 2.25 || p.Syllables[1].Time != 0.75 {
		t.Errorf("timing: start %v end %v second syllable %v", p.Start, p.End, p.Syllables[1].Time)
	}
	if len(c.Sections) != 1 {
		t.Errorf("lyrics shouldn't disturb sections: %+v", c.Sections)
	}
}

func TestLyricsWithoutPhrases(t *testing.T) {
	src := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[Events]\n{\n" +
		"  0 = E \"lyric one\"\n  96 = E \"lyric two\"\n  1920 = E \"lyric three\"\n}\n"
	c, err := ParseDotChart(strings.NewReader(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Lyrics) != 2 || c.Lyrics[0].Line() != "one two" || c.Lyrics[1].Line() != "three" {
		t.Fatalf("gap split: %+v", c.Lyrics)
	}
}
