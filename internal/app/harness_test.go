package app

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/playback"
)

// harness drives scenes frame by frame with a fake clock and scripted raw
// input, drawing every frame so Draw panics surface too.
type harness struct {
	t      *testing.T
	a      *App
	now    float64
	screen *ebiten.Image
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("JAM_HOME", t.TempDir())
	h := &harness{t: t, now: 100}
	t.Cleanup(clock.Override(func() float64 { return h.now }))
	settings := config.Default()
	b, err := input.LoadBindings(config.BindingsPath())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Settings: settings, Bindings: b, NoAudio: true, SFX: playback.NewSFX(nil)}
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
		if err := a.scene.Update(a); err != nil {
			h.t.Fatal(err)
		}
	}()
	func() {
		defer func() {
			if r := recover(); r != nil {
				h.t.Fatalf("panic in %T.Draw: %v", a.scene, r)
			}
		}()
		a.scene.Draw(h.screen, a)
	}()
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
)
