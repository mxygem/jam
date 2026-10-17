package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/library"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/playlist"
	"github.com/mxygem/jam/internal/replay"
)

// harness drives scenes frame by frame with a fake clock and scripted raw
// input, drawing every frame so Draw panics surface too.
type harness struct {
	t      *testing.T
	a      *App
	now    float64
	screen *ebiten.Image
	shots  bool // JAM_SHOTS: draw every frame
}

const drawEvery = 6

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("JAM_HOME", t.TempDir())
	h := &harness{t: t, now: 100, shots: os.Getenv("JAM_SHOTS") != ""}
	t.Cleanup(clock.Override(func() float64 { return h.now }))
	settings := config.Default()
	b, err := input.LoadBindings(config.BindingsPath())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: settings, Bindings: b, NoAudio: true, SFX: playback.NewSFX(nil)}
	applyLook(a) // tests share the theme: start each from the default
	a.Index = library.Open(filepath.Join(config.Dir(), "library.json.gz"))
	a.Scores = replay.LoadScores(filepath.Join(config.Dir(), "scores.json"), config.RunsDir())
	a.Art = newArtCache()
	a.Queue = &playlist.Playlist{}
	a.BG = newBackgrounds(CustomsDir())
	a.Input = input.NewBackground()
	t.Cleanup(a.Input.Close)
	a.Menu = input.NewMenuInput(a.Bindings, a.Input.Devices())
	h.a = a
	h.screen = ebiten.NewImage(1920, 1080)
	return h
}

// frame runs one update+draw with the given raw events, then advances time.
func (h *harness) frame(evs ...input.RawEvent) {
	h.t.Helper()
	for i := range evs {
		if evs[i].Time == 0 {
			evs[i].Time = h.now
		}
	}
	a := h.a
	a.frame++
	a.Raw = append(a.Raw[:0], evs...)
	a.Input.Inject(a.Raw)
	a.MenuActs = a.Menu.Feed(a.Raw, h.now)
	func() {
		defer func() {
			if r := recover(); r != nil {
				h.t.Fatalf("panic in %T.Update: %v", a.scene, r)
			}
		}()
		if err := a.step(); err != nil {
			h.t.Fatal(err)
		}
	}()
	// Without a running game loop Ebitengine keeps every draw command until
	// the process ends, so draw every few frames (every frame when taking
	// screenshots, where the commands are carried out).
	if h.shots || a.frame%drawEvery == 1 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.t.Fatalf("panic in %T.Draw: %v", a.scene, r)
				}
			}()
			a.scene.Draw(h.screen, a)
		}()
	}
	h.now += 1.0 / 60
}

// idle runs frames with no input for d seconds.
func (h *harness) idle(d float64) {
	for end := h.now + d; h.now < end; {
		h.frame()
	}
}

func xbtn(bit int, down bool) input.RawEvent {
	v := float32(0)
	if down {
		v = 1
	}
	return input.RawEvent{Src: input.SrcXInput, Device: 0, Kind: input.KindButton, Code: bit, Value: v, Down: down}
}

func xaxis(axis int, v float32) input.RawEvent {
	return input.RawEvent{Src: input.SrcXInput, Device: 0, Kind: input.KindAxis, Code: axis, Value: v}
}

// tap presses and releases an XInput button over a few frames, the way a
// person does (release arrives on a later frame than the press).
func (h *harness) tap(bit int) {
	h.frame(xbtn(bit, true))
	h.idle(0.08)
	h.frame(xbtn(bit, false))
	h.idle(0.05)
}

// sweep moves an axis from one value to another and back over time.
func (h *harness) sweep(axis int, from, to float32, hold float64) {
	steps := 8
	for i := 1; i <= steps; i++ {
		h.frame(xaxis(axis, from+(to-from)*float32(i)/float32(steps)))
	}
	h.idle(hold)
	for i := steps - 1; i >= 0; i-- {
		h.frame(xaxis(axis, from+(to-from)*float32(i)/float32(steps)))
	}
	h.idle(0.1)
}

const (
	keyEscape    = ebiten.KeyEscape
	keyBackspace = ebiten.KeyBackspace
	keyEnter     = ebiten.KeyEnter
	keyLeft      = ebiten.KeyArrowLeft
	keyRight     = ebiten.KeyArrowRight
	keyArrowUp   = ebiten.KeyArrowUp
	keyArrowDown = ebiten.KeyArrowDown
)

func keyDown(k ebiten.Key) input.RawEvent {
	return input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(k), Value: 1, Down: true}
}
