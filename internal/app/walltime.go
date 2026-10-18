package app

import (
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/render"
)

// wallClock is the local time of day shown in the bottom right corner of
// every screen. It is drawn after the scene, straight onto the window, so on
// a wider or taller window it sits in the window's corner rather than the
// 1920x1080 canvas's.
type wallClock struct {
	sec  int64  // the Unix second text was made for
	text string // "2:57 PM"
}

// wallClockText formats a time as the clock shows it.
func wallClockText(t time.Time) string { return t.Format("3:04 PM") }

func (c *wallClock) Draw(screen *ebiten.Image) {
	now := time.Now()
	if s := now.Unix(); s != c.sec || c.text == "" {
		c.sec, c.text = s, wallClockText(now)
	}
	b := screen.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	const (
		size   = 26.0
		padX   = 16.0
		pillH  = 40.0
		margin = 24.0 // from the right edge
		bottom = 10.0 // from the bottom edge: the pill sits inside the footer band
	)
	tw, _ := render.Measure(c.text, render.CondBold, size)
	pw := tw + padX*2
	x, y := w-margin-pw, h-bottom-pillH
	// Same colors as the rest of the UI, so it follows the selected theme.
	render.RoundRect(screen, x, y, pw, pillH, pillH/2, render.WithAlpha(th.Panel, 0.8))
	render.FillRect(screen, x+pillH/2, y+pillH-3, pw-pillH, 3, render.WithAlpha(th.Accent, 0.9))
	render.Text(screen, c.text, render.CondBold, size, x+pw/2, y+(pillH-size)/2-2, th.Text, render.Center)
}
