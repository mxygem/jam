package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
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
