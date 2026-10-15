package demo

import (
	"testing"

	"github.com/mxygem/jam/internal/chart"
)

func TestDemoParses(t *testing.T) {
	dir := t.TempDir()
	folder, err := Write(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := chart.LoadSongFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LoadChart()
	if err != nil {
		t.Fatal(err)
	}
	for _, inst := range []chart.Instrument{chart.Guitar, chart.Bass, chart.Drums} {
		if len(c.Available(inst)) != 4 {
			t.Errorf("%s difficulties: %v", inst, c.Available(inst))
		}
	}
	g := c.Track(chart.Guitar, chart.Expert)
	kinds := map[chart.NoteKind]int{}
	opens, chords, sus, sp := 0, 0, 0, 0
	for _, n := range g.Notes {
		kinds[n.Kind]++
		if n.IsOpen() {
			opens++
		}
		if n.IsChord() {
			chords++
		}
		if n.HasSustain() {
			sus++
		}
		if n.SPEnd {
			sp++
		}
	}
	t.Logf("guitar: %d notes, kinds %v, opens %d chords %d sustains %d sp phrases %d; stems %v; drums mode %s",
		len(g.Notes), kinds, opens, chords, sus, sp, s.Stems, c.Track(chart.Drums, chart.Expert).DrumMode)
	if kinds[chart.HOPO] == 0 || kinds[chart.Tap] == 0 || opens == 0 || chords == 0 || sus == 0 || sp != 3 {
		t.Errorf("demo is missing note types")
	}
	if len(s.Stems) != 4 {
		t.Errorf("stems: %v", s.Stems)
	}
}
