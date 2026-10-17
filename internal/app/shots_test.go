package app

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/input"
)

// shot saves the current screen when JAM_SHOTS names a folder (for looking
// at menus during development; does nothing in normal test runs).
func (h *harness) shot(name string) {
	dir := os.Getenv("JAM_SHOTS")
	if dir == "" {
		return
	}
	img := image.NewRGBA(h.screen.Bounds())
	h.screen.ReadPixels(img.Pix)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		h.t.Fatal(err)
	}
}

// TestSongSelectOverlays opens each song list overlay and draws it.
func TestSongSelectOverlays(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.scene = NewSongSelect(a)
	h.idle(0.2)
	h.shot("select")
	h.tap(bitY) // options
	if a.scene.(*SongSelect).ov != ovOptions {
		t.Fatal("options did not open")
	}
	h.idle(0.1)
	h.shot("options")
	h.tap(bitDPadDown)
	h.tap(bitDPadDown)
	h.tap(bitA) // Filters
	if a.scene.(*SongSelect).ov != ovFilter {
		t.Fatalf("filter editor did not open (ov %v)", a.scene.(*SongSelect).ov)
	}
	h.idle(0.1)
	h.shot("filters")
	h.tap(bitA) // first row: on/off
	h.tap(bitDPadDown)
	h.tap(bitA) // instruments
	h.idle(0.1)
	h.shot("filter-parts")
	h.tap(bitB)
	h.tap(bitB)
	h.idle(0.1)
	if a.scene.(*SongSelect).ov != ovNone {
		t.Fatalf("overlay still open: %v", a.scene.(*SongSelect).ov)
	}
	h.tap(bitA) // picker
	h.idle(0.1)
	h.shot("picker")
	h.tap(bitB)
	h.tap(bitB) // back to the menu
	if _, ok := a.scene.(*MainMenu); !ok {
		t.Fatalf("scene %T after back", a.scene)
	}
}

// TestMain runs the tests inside a running game when JAM_SHOTS is set, since
// Ebitengine can only read pixels back once a game loop has started.
func TestMain(m *testing.M) {
	if os.Getenv("JAM_SHOTS") == "" {
		os.Exit(m.Run())
	}
	g := &shotGame{m: m, result: make(chan int, 1)}
	ebiten.SetWindowSize(320, 180)
	if err := ebiten.RunGame(g); err != nil && err != ebiten.Termination {
		panic(err)
	}
	os.Exit(g.code)
}

type shotGame struct {
	m       *testing.M
	started bool
	result  chan int
	code    int
}

func (g *shotGame) Update() error {
	if !g.started {
		g.started = true
		go func() { g.result <- g.m.Run() }()
	}
	select {
	case g.code = <-g.result:
		return ebiten.Termination
	default:
		return nil
	}
}

func (g *shotGame) Draw(*ebiten.Image)         {}
func (g *shotGame) Layout(w, h int) (int, int) { return w, h }

// TestPauseSettings changes highway settings from the pause menu and checks
// they apply to the song in progress, while engine settings wait.
func TestPauseSettings(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.scene = NewSongSelect(a)
	h.tap(bitA)
	h.tap(bitA)
	h.until("play", 10, sceneIs[*Play](h), nil)
	h.until("3 s in", 10, func() bool { return a.scene.(*Play).songNow(h.now) > 3 }, nil)
	p := a.scene.(*Play)
	h.tap(bitStart)
	h.tap(bitDPadDown)
	h.tap(bitDPadDown) // Settings
	h.tap(bitA)
	if p.pset == nil {
		t.Fatal("settings did not open from pause")
	}
	h.shot("pause-settings")
	h.tap(bitDPadDown) // Highway and visuals
	h.tap(bitA)
	if p.pset.pageID() != pageHighway {
		t.Fatalf("page %q", p.pset.pageID())
	}
	before := p.hw.Cfg.Lookahead
	right := input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(ebiten.KeyRight), Value: 1, Down: true}
	h.frame(right)
	h.frame(input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(ebiten.KeyRight)})
	h.idle(0.1)
	if p.hw.Cfg.Lookahead >= before {
		t.Fatalf("note speed didn't apply live: %.2f -> %.2f", before, p.hw.Cfg.Lookahead)
	}
	h.shot("pause-highway")
	h.tap(bitB) // back to pages
	h.tap(bitDPadUp)
	h.tap(bitA) // Gameplay
	if p.pset.pageID() != pageGameplay {
		t.Fatalf("page %q", p.pset.pageID())
	}
	nf := a.Settings.Engine.NoFail
	for i := 0; i < 5; i++ {
		h.tap(bitDPadDown) // No fail
	}
	h.tap(bitA)
	if a.Settings.Engine.NoFail == nf {
		t.Fatal("no fail didn't toggle")
	}
	h.shot("pause-gameplay")
	h.tap(bitB)
	h.tap(bitB)
	if p.pset != nil || !p.paused {
		t.Fatal("backing out of settings should return to the pause menu")
	}
	h.tap(bitB) // resume
	if p.paused {
		t.Fatal("still paused")
	}
}

// TestSettingsPages walks the settings pages and the setup screens they open.
func TestSettingsPages(t *testing.T) {
	h := newHarness(t)
	a := h.a
	a.scene = NewMainMenu(a)
	for i := 0; i < 3; i++ {
		h.tap(bitDPadDown) // Settings
	}
	h.tap(bitA)
	s, ok := a.scene.(*Settings)
	if !ok {
		t.Fatalf("scene %T", a.scene)
	}
	h.shot("settings")
	for _, id := range []string{pageGameplay, pageHighway, pageBackground, pageAudio, pageControls, pageLibrary} {
		h.tap(bitA)
		if s.menu.pageID() != id {
			t.Fatalf("opened %q, want %q", s.menu.pageID(), id)
		}
		h.shot("settings-" + id)
		h.tap(bitB)
		h.tap(bitDPadDown)
	}
	// Controls -> guitar setup -> Esc comes back to the Controls page.
	s.menu.open(pageControls)
	h.tap(bitA)
	if _, ok := a.scene.(*Wizard); !ok {
		t.Fatalf("scene %T, want the wizard", a.scene)
	}
	h.frame(keyDown(keyEscape))
	h.idle(0.1)
	back, ok := a.scene.(*Settings)
	if !ok || back.menu.pageID() != pageControls {
		t.Fatalf("after Esc: %T", a.scene)
	}
}

func keyUp(k ebiten.Key) input.RawEvent {
	return input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(k)}
}

func (h *harness) key(k ebiten.Key) {
	h.frame(keyDown(k))
	h.idle(0.05)
	h.frame(keyUp(k))
	h.idle(0.05)
}

// TestFilterEditorKeyboard leaves the filters with Esc and with Done.
func TestFilterEditorKeyboard(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	s := NewSongSelect(a)
	a.scene = s
	h.idle(0.1)
	s.openFilter(a)
	h.key(ebiten.KeyEscape)
	if s.ov != ovNone {
		t.Fatal("Esc should close the filters")
	}
	s.openFilter(a)
	for i := 0; i < 5; i++ {
		h.key(ebiten.KeyArrowDown) // to Done
	}
	if got := s.filter.list.Items[s.filter.list.Sel].Label; got != "Done" {
		t.Fatalf("on %q", got)
	}
	h.key(ebiten.KeyEnter)
	if s.ov != ovNone {
		t.Fatal("Enter on Done should close the filters")
	}
	// Done inside the parts list goes back to the main filter page.
	s.openFilter(a)
	h.key(ebiten.KeyArrowDown)
	h.key(ebiten.KeyEnter) // Parts
	if !s.filter.parts {
		t.Fatal("parts didn't open")
	}
	h.key(ebiten.KeyEscape)
	if s.filter.parts || s.ov != ovFilter {
		t.Fatal("Esc in parts should go back to the filter page")
	}
	h.key(ebiten.KeyEscape)
	if s.ov != ovNone {
		t.Fatal("second Esc should close")
	}
}

// TestRangeEditing changes a number the guitar way: green to start, strum
// to change (faster when held), red to finish without leaving the page.
func TestRangeEditing(t *testing.T) {
	h := newHarness(t)
	a := h.a
	s := NewSettingsAt(a, pageHighway)
	a.scene = s
	h.idle(0.1)
	for i := 0; i < 3; i++ {
		h.tap(bitDPadDown) // Highway length
	}
	if got := s.menu.items.Items[s.menu.items.Sel].Label; got != "Highway length" {
		t.Fatalf("on %q", got)
	}
	top := a.Settings.Highway.TopY
	h.tap(bitA)
	if !s.menu.items.Editing() {
		t.Fatal("green should start editing a range")
	}
	h.shot("range-editing")
	for i := 0; i < 3; i++ {
		h.tap(bitDPadUp)
	}
	if got := top - a.Settings.Highway.TopY; got != 6 {
		t.Fatalf("three strums up moved the length by %v, want 6", got)
	}
	h.tap(bitDPadDown)
	if got := top - a.Settings.Highway.TopY; got != 4 {
		t.Fatalf("strum down: %v, want 4", got)
	}
	// Holding the strum speeds up.
	before := a.Settings.Highway.TopY
	h.frame(xbtn(bitDPadUp, true))
	h.idle(3)
	h.frame(xbtn(bitDPadUp, false))
	if moved := before - a.Settings.Highway.TopY; moved < 100 {
		t.Errorf("holding for 3 s moved only %v", moved)
	}
	h.tap(bitB) // red finishes editing...
	if s.menu.items.Editing() || s.menu.pageID() != pageHighway {
		t.Fatalf("red should finish editing and stay on the page (editing %v, page %q)", s.menu.items.Editing(), s.menu.pageID())
	}
	h.tap(bitB) // ...and red again leaves the page
	if s.menu.pageID() != "" {
		t.Fatalf("still on %q", s.menu.pageID())
	}
	// Note speed goes well past the old limit.
	a.Settings.Highway.Lookahead = lookaheadFor(9.9)
	s.menu.open(pageHighway)
	it := s.menu.items.Items[0]
	for i := 0; i < 100; i++ {
		it.Adjust(1)
	}
	if v := noteSpeed(a.Settings.Highway.Lookahead); math.Abs(v-maxNoteSpeed) > 1e-6 {
		t.Errorf("note speed tops out at %.2f, want %v", v, float64(maxNoteSpeed))
	}
}

// TestFilterEditorCloses leaves the filter editor with Done and with Esc,
// using the keyboard.
func TestFilterEditorCloses(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	s := NewSongSelect(a)
	a.scene = s
	h.idle(0.1)
	enter := func() {
		h.frame(keyDown(keyEnter))
		h.frame(input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(keyEnter)})
	}
	esc := func() {
		h.frame(keyDown(keyEscape))
		h.frame(input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(keyEscape)})
	}
	s.openFilter(a)
	s.filter.list.Sel = len(s.filter.list.Items) - 1 // Done
	enter()
	if s.ov != ovNone {
		t.Fatal("Enter on Done didn't close the filters")
	}
	s.openFilter(a)
	esc()
	if s.ov != ovNone {
		t.Fatal("Esc didn't close the filters")
	}
	// From the parts list: Esc goes back a level, then closes.
	s.openFilter(a)
	s.filter.list.Sel = 1
	enter()
	if !s.filter.parts {
		t.Fatal("Parts didn't open")
	}
	esc()
	esc()
	if s.ov != ovNone {
		t.Fatal("Esc from the parts list didn't get out")
	}
	if _, ok := a.scene.(*SongSelect); !ok {
		t.Fatalf("Esc went past the song list to %T", a.scene)
	}
}
