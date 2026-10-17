package app

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/engine"
)

func iniSong(t *testing.T, dir, ini string) *chart.Song {
	t.Helper()
	parsed, err := chart.ParseSongIni(strings.NewReader("[song]\n" + ini))
	if err != nil {
		t.Fatal(err)
	}
	s := &chart.Song{Dir: dir, Ini: parsed}
	if v, ok := parsed.Int("preview_start_time"); ok {
		s.PreviewMs = v
	}
	return s
}

func TestIniPreview(t *testing.T) {
	cases := []struct {
		ini       string
		length    float64
		ok        bool
		start, du float64
	}{
		{"preview_start_time = 45000", 200, true, 45, 30},
		{"preview_start_time = 45000\npreview_end_time = 65000", 200, true, 45, 20},
		{"preview_start_time = 45000\npreview_end_time = 245000", 300, true, 45, 60}, // capped
		{"preview_start_time = 45000\npreview_end_time = -1", 200, true, 45, 30},
		{"preview_start_time = 45000", 0, true, 45, 30},   // length unknown
		{"preview_start_time = 0", 200, false, 0, 0},      // unset
		{"preview_start_time = -1", 200, false, 0, 0},     // unset
		{"preview_start_time = 199000", 200, false, 0, 0}, // past the end
		{"name = x", 200, false, 0, 0},
	}
	for _, c := range cases {
		start, dur, ok := iniPreview(iniSong(t, "x", c.ini), c.length)
		if ok != c.ok || start != c.start || dur != c.du {
			t.Errorf("%q: got %v %v %v, want %v %v %v", c.ini, start, dur, ok, c.start, c.du, c.ok)
		}
	}
}

func TestFallbackSpot(t *testing.T) {
	song := &chart.Song{Dir: `E:\charts\complete\Some Song`}
	secs := []chart.Section{{Time: 0, Name: "Intro"}, {Time: 20, Name: "Verse 1"}, {Time: 41, Name: "Pre-Chorus"}, {Time: 52, Name: "Chorus 1"}, {Time: 80, Name: "Verse 2"}}
	if got, why := fallbackSpot(song, secs, 200); math.Abs(got-(52-previewPreRoll)) > 1e-9 || why != "first chorus" {
		t.Fatalf("got %v (%s), want just before the first chorus", got, why)
	}
	// No chorus: a spot 30-55% in, the same every time for this song.
	plain := []chart.Section{{Time: 0, Name: "Intro"}, {Time: 30, Name: "Verse"}}
	a, _ := fallbackSpot(song, plain, 200)
	b, _ := fallbackSpot(song, plain, 200)
	if a != b || a < 60 || a > 110 {
		t.Fatalf("random spot %v then %v, want the same value 60-110 s in", a, b)
	}
	other, _ := fallbackSpot(&chart.Song{Dir: `E:\charts\complete\Another Song`}, nil, 200)
	if other == a {
		t.Fatalf("two songs picked the same spot %v", a)
	}
	// A section start shortly before the spot wins.
	near := []chart.Section{{Time: 0, Name: "Intro"}, {Time: a - 5, Name: "Solo"}}
	if got, why := fallbackSpot(song, near, 200); math.Abs(got-(a-5-previewPreRoll)) > 1e-9 || why != "section Solo" {
		t.Fatalf("got %v (%s), want the Solo section", got, why)
	}
	// A chorus in the last seconds isn't worth previewing.
	late := []chart.Section{{Time: 195, Name: "Chorus"}}
	if _, why := fallbackSpot(song, late, 200); why == "first chorus" {
		t.Fatal("picked a chorus 5 s from the end")
	}
}

func TestIsChorus(t *testing.T) {
	for name, want := range map[string]bool{
		"Chorus": true, "chorus_1a": true, "CHORUS 2": true, "Final Chorus": true,
		"Pre-Chorus": false, "prechorus 1": false, "Post Chorus": false, "Verse": false, "Choir": false,
	} {
		if got := isChorus(name); got != want {
			t.Errorf("isChorus(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestErrorSoundModes(t *testing.T) {
	type want struct{ overstrum, overhit, miss bool }
	for mode, w := range map[string]want{
		"off":       {false, false, false},
		"overstrum": {true, false, false},
		"extra":     {true, true, false},
		"all":       {true, true, true},
		"":          {true, false, false}, // unknown: the default
	} {
		got := want{errorSound(mode, engine.Overstrum), errorSound(mode, engine.Overhit), errorSound(mode, engine.Miss)}
		if got != w {
			t.Errorf("%q: got %+v, want %+v", mode, got, w)
		}
		if errorSound(mode, engine.Hit) {
			t.Errorf("%q: a hit made the error sound", mode)
		}
	}
	m := "overstrum"
	seen := map[string]bool{}
	for i := 0; i < len(errorSoundModes); i++ {
		seen[m] = true
		m = cycleErrorSounds(m, 1)
	}
	if m != "overstrum" || len(seen) != len(errorSoundModes) {
		t.Fatalf("cycling visited %v and ended on %q", seen, m)
	}
	if cycleErrorSounds("off", -1) != "all" {
		t.Fatal("cycling back from off should wrap to all")
	}
}

// withPreviewSong adds a copy of the demo song whose song.ini sets a
// preview start.
func withPreviewSong(h *harness) *chart.Song {
	h.t.Helper()
	src := h.a.library[0].Dir
	dst := filepath.Join(config.SongsDir(), "Preview Test")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		h.t.Fatal(err)
	}
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			h.t.Fatal(err)
		}
		if e.Name() == "song.ini" {
			b = []byte(strings.Replace(string(b), "name = Highway Test", "name = Preview Test\npreview_start_time = 5000", 1))
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
	withDemoLibrary(h)
	for _, s := range h.a.library {
		if s.Dir == dst {
			return s
		}
	}
	h.t.Fatal("preview test song not scanned")
	return nil
}

// waitPreview runs frames until cond holds, giving the decoder goroutine
// real time to work.
func (h *harness) waitPreview(what string, cond func() bool) {
	h.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
		h.frame()
	}
}

func TestSongListPreview(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	withPreview := withPreviewSong(h)
	a := h.a
	a.Preview = newPreviewer()
	a.Settings.LastSong = withPreview.Dir
	a.scene = NewSongSelect(a)
	sel := a.scene.(*SongSelect)
	if sel.current() != withPreview {
		t.Fatalf("selected %v", sel.current())
	}

	// Nothing loads until the selection has settled.
	h.idle(previewSettle / 2)
	if a.Preview.started {
		t.Fatal("preview started before the selection settled")
	}
	h.waitPreview("song.ini preview", func() bool { s, _ := a.Preview.Playing(); return s == withPreview })
	if _, start := a.Preview.Playing(); start != 5 {
		t.Fatalf("preview starts at %v, want song.ini's 5 s", start)
	}
	h.idle(0.5)
	if lv := a.Preview.meter(withPreview, 4); lv == nil {
		t.Fatal("no meter for the playing preview")
	}
	h.shot("select-preview")

	// Moving on fades it out, and the next song (no preview time in its
	// song.ini) previews from its first chorus.
	h.tap(bitDPadDown)
	if sel.current() == withPreview {
		h.tap(bitDPadUp)
	}
	demoSong := sel.current()
	if demoSong == withPreview {
		t.Fatal("selection didn't move")
	}
	if s, _ := a.Preview.Playing(); s == withPreview {
		t.Fatal("old preview still playing at full volume after moving on")
	}
	h.waitPreview("chorus preview", func() bool { s, _ := a.Preview.Playing(); return s == demoSong })
	chorus := 12 * 4 * 60 / 132.0 // demo: chorus at bar 12, 132 bpm
	if _, start := a.Preview.Playing(); math.Abs(start-(chorus-previewPreRoll)) > 0.01 {
		t.Fatalf("preview starts at %.3f, want the chorus at %.3f", start, chorus-previewPreRoll)
	}
	h.idle(1)
	if len(a.Preview.old) != 0 {
		t.Fatalf("%d faded previews not closed", len(a.Preview.old))
	}

	// Leaving the song list stops it.
	h.tap(bitB)
	if _, ok := a.scene.(*MainMenu); !ok {
		t.Fatalf("scene %T after back", a.scene)
	}
	h.idle(1)
	if a.Preview.cur != nil {
		t.Fatal("preview still playing on the main menu")
	}
}

func TestSongListPreviewOff(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.Settings.PreviewVolume = 0
	a.Preview = newPreviewer()
	a.scene = NewSongSelect(a)
	h.idle(1)
	if a.Preview.started || a.Preview.cur != nil {
		t.Fatal("preview loaded with previews off")
	}
}
