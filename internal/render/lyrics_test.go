package render

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
)

func TestLyricsTiming(t *testing.T) {
	ph := []chart.LyricPhrase{
		{Start: 10, End: 12, Syllables: []chart.Syllable{{Time: 10, Text: "Hel", Join: true}, {Time: 10.4, Text: "lo"}, {Time: 11, Text: "<i>world</i>"}}},
		{Start: 12.5, End: 14, Syllables: []chart.Syllable{{Time: 12.5, Text: "again"}}},
		{Start: 30, End: 31, Syllables: []chart.Syllable{{Time: 30, Text: "later"}}},
	}
	if NewLyrics(nil, 0, DefaultLyricOpts()) != nil {
		t.Error("no phrases should mean no lyrics view")
	}
	lv := NewLyrics([]chart.VocalPart{{Name: "Lead", Lead: true, Phrases: ph}}, 64, DefaultLyricOpts())
	l := lv.tracks[0]
	for _, c := range []struct {
		now  float64
		want int
	}{{0, 0}, {11, 0}, {12.2, 0}, {12.4, 1}, {14.2, 1}, {20, 2}, {40, 3}} {
		if got := l.current(c.now); got != c.want {
			t.Errorf("current(%v) = %d, want %d", c.now, got, c.want)
		}
	}
	// A phrase right after another appears as soon as the previous ends;
	// one after a long gap gets the full 2 s lead.
	if got := l.leadIn(1); got != 0.5 {
		t.Errorf("leadIn(1) = %v", got)
	}
	if got := l.leadIn(2); got != 2 {
		t.Errorf("leadIn(2) = %v", got)
	}
	s, starts, ends := lineText(&ph[0])
	if s != "Hello <i>world</i>" || starts[1] != 3 || ends[1] != 5 || starts[2] != 6 {
		t.Errorf("lineText = %q %v %v", s, starts, ends)
	}
	// Syllable positions sweep left to right and drawing doesn't panic.
	ln := l.line(0, lyricSize, 1)
	for i := 1; i < len(ln.start); i++ {
		if ln.start[i] < ln.end[i-1]-0.01 || ln.end[i] <= ln.start[i] {
			t.Errorf("syllable %d at %.1f-%.1f after %.1f", i, ln.start[i], ln.end[i], ln.end[i-1])
		}
	}
	dst := ebiten.NewImage(ScreenW, ScreenH)
	for tick, now := range []float64{0, 8.5, 9, 10.2, 10.45, 11.5, 12.3, 13, 28.5, 30.5, 50} {
		lv.Draw(dst, now, ScreenW/2, tick)
	}
}

func TestLyricsParts(t *testing.T) {
	lead := []chart.LyricPhrase{{Start: 1, End: 3, Syllables: []chart.Syllable{{Time: 1, Text: "ah"}, {Time: 2, Text: "ah"}}}}
	back := []chart.LyricPhrase{{Start: 1.5, End: 3.5, Syllables: []chart.Syllable{{Time: 1.5, Text: "I'm"}, {Time: 2, Text: "in"}, {Time: 2.2, Text: "a"}, {Time: 2.4, Text: "band"}}}}
	parts := []chart.VocalPart{{Name: "Lead", Lead: true, Phrases: lead}, {Name: "Backing", Phrases: back}, {Name: "Empty"}}
	l := NewLyrics(parts, 64, DefaultLyricOpts())
	if l.Parts() != 2 {
		t.Fatalf("parts shown = %d, want 2 (empty part skipped)", l.Parts())
	}
	if l.tracks[1].color != PartColors[0] || l.tracks[1].size >= l.tracks[0].size {
		t.Errorf("backing part should be smaller and in the first part color: %+v", l.tracks[1])
	}
	if NewLyrics(parts, 64, LyricOpts{Backing: "off"}).Parts() != 1 {
		t.Error("backing off should show the lead only")
	}
	// Only backing lyrics (no lead phrases) still draws.
	if NewLyrics(parts[1:2], 64, DefaultLyricOpts()).Parts() != 1 {
		t.Error("a lone part is shown as the lead")
	}
	dst := ebiten.NewImage(ScreenW, ScreenH)
	for _, opts := range []LyricOpts{DefaultLyricOpts(), {Backing: "above", BackingScale: 1}} {
		l := NewLyrics(parts, 64, opts)
		for tick, now := range []float64{0, 1.2, 1.7, 2.3, 3.2, 5} {
			l.Draw(dst, now, ScreenW/2, tick)
		}
	}
}

// TestLyricFX draws a phrase with each effect through its whole life, in every
// effects setting, and checks what each setting keeps.
func TestLyricFX(t *testing.T) {
	var ph []chart.LyricPhrase
	at := 1.0
	for _, name := range chart.FXNames {
		arg := name
		if name == "color" {
			arg = "color:#00ff00"
		}
		fx, ok := chart.ParseFX(arg)
		if !ok {
			t.Fatalf("parse %s", arg)
		}
		f := []chart.FX{fx}
		ph = append(ph, chart.LyricPhrase{Start: at, End: at + 2, Syllables: []chart.Syllable{
			{Time: at, Text: "Hel", Join: true, FX: f}, {Time: at + 0.4, Text: "lo", FX: f}, {Time: at + 1, Text: "<b>world</b>", FX: f}}})
		at += 4
	}
	parts := []chart.VocalPart{{Name: "Lead", Lead: true, Phrases: ph},
		{Name: "Backing", Phrases: []chart.LyricPhrase{{Start: 1.5, End: 3, Syllables: []chart.Syllable{
			{Time: 1.5, Text: "oh", FX: []chart.FX{{Name: "wave"}}}}}}}}
	dst := ebiten.NewImage(ScreenW, ScreenH)
	for _, mode := range LyricFxModes {
		opts := DefaultLyricOpts()
		opts.Effects = mode
		l := NewLyrics(parts, 64, opts)
		l.Beat = func(now float64) float64 { return now - float64(int(now)) }
		lead := l.tracks[0]
		for i, name := range chart.FXNames {
			want := mode == "full" || (mode == "calm" && !calmDrops[name])
			if got := lead.fx[i].any; got != want {
				t.Errorf("%s: %s kept = %v, want %v", mode, name, got, want)
			}
		}
		for tick, now := 0, 0.0; now < at+2; tick, now = tick+1, now+0.05 {
			l.Draw(dst, now, ScreenW/2, tick)
		}
	}
	// An exploding phrase stays up after its end so the sparks can finish.
	l := NewLyrics(parts, 64, DefaultLyricOpts())
	ex := 0
	for i, n := range chart.FXNames {
		if n == "explode" {
			ex = i
		}
	}
	if got := l.tracks[0].current(ph[ex].End + 1); got != ex {
		t.Errorf("explode phrase should still show 1 s after it ends, current = %d", got)
	}
}
