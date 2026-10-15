package app

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/render"
)

// TextInput is a one-line text field typed on the keyboard.
type TextInput struct {
	Value       string
	Placeholder string
	Max         int // runes; 0 = 120

	bsHeld float64
	bsLast float64
}

// Update reads typing. It returns done on Enter and cancel on Escape.
func (t *TextInput) Update(a *App) (done, cancel bool) {
	limit := t.Max
	if limit == 0 {
		limit = 120
	}
	for _, r := range a.Chars {
		if r >= 32 && r != 127 && len([]rune(t.Value)) < limit {
			t.Value += string(r)
		}
	}
	now := clock.Now()
	del := false
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyBackspace):
		del, t.bsHeld, t.bsLast = true, now, now
	case ebiten.IsKeyPressed(ebiten.KeyBackspace) && now-t.bsHeld > 0.4 && now-t.bsLast > 0.035:
		del, t.bsLast = true, now
	}
	if del {
		if ebiten.IsKeyPressed(ebiten.KeyControl) {
			t.Value = "" // ctrl+backspace clears
		} else if rs := []rune(t.Value); len(rs) > 0 {
			t.Value = string(rs[:len(rs)-1])
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		return true, false
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return false, true
	}
	return false, false
}

// Draw draws the field with a blinking caret.
func (t *TextInput) Draw(dst *ebiten.Image, x, y, w float64) {
	render.RoundRect(dst, x, y, w, 64, 12, render.RGBA(0x0C, 0x0E, 0x18, 0xF0))
	render.StrokeRect(dst, x, y, w, 64, 2, render.WithAlpha(th.Accent, 0.8))
	if t.Value == "" && t.Placeholder != "" {
		render.Text(dst, t.Placeholder, render.UI, 30, x+20, y+14, render.WithAlpha(th.TextDim, 0.7), render.Left)
	}
	tw, _ := render.Measure(t.Value, render.UI, 30)
	shown := t.Value
	if tw > w-50 {
		// keep the end (where the caret is) visible
		rs := []rune(t.Value)
		for len(rs) > 0 {
			rs = rs[1:]
			if ww, _ := render.Measure(string(rs), render.UI, 30); ww <= w-50 {
				break
			}
		}
		shown = string(rs)
		tw, _ = render.Measure(shown, render.UI, 30)
	}
	render.Text(dst, shown, render.UI, 30, x+20, y+14, th.Text, render.Left)
	if math.Mod(clock.Now(), 1) < 0.55 {
		render.FillRect(dst, x+22+tw, y+14, 3, 36, th.Accent)
	}
}
