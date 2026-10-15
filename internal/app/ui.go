package app

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/render"
)

var th = &render.DefaultTheme

// Item is a menu entry. Value, if set, is shown on the right and Left/Right
// call Adjust.
type Item struct {
	Label    string
	Value    func() string
	Adjust   func(dir int)
	Activate func()
	Hint     string
	Disabled bool
}

// List is a vertical, scrollable menu.
type List struct {
	Items   []Item
	Sel     int
	Top     int
	Visible int
}

// Update handles navigation. It returns true if an item was activated.
func (l *List) Update(a *App) bool {
	if len(l.Items) == 0 {
		return false
	}
	if l.Visible == 0 {
		l.Visible = 10
	}
	move := func(d int) {
		for i := 0; i < len(l.Items); i++ {
			l.Sel = (l.Sel + d + len(l.Items)) % len(l.Items)
			if !l.Items[l.Sel].Disabled {
				break
			}
		}
	}
	if l.Items[l.Sel].Disabled {
		move(1)
	}
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp:
			move(-1)
		case action.MenuDown:
			move(1)
		case action.MenuLeft, action.MenuRight:
			if it := l.Items[l.Sel]; it.Adjust != nil {
				d := 1
				if act == action.MenuLeft {
					d = -1
				}
				it.Adjust(d)
			}
		case action.MenuConfirm:
			if it := l.Items[l.Sel]; it.Activate != nil {
				it.Activate()
				return true
			} else if it.Adjust != nil {
				it.Adjust(1)
			}
		}
	}
	if l.Sel < l.Top {
		l.Top = l.Sel
	}
	if l.Sel >= l.Top+l.Visible {
		l.Top = l.Sel - l.Visible + 1
	}
	return false
}

// Draw renders the list with its top-left at x, y.
func (l *List) Draw(dst *ebiten.Image, x, y, w, rowH float64) {
	if l.Visible == 0 {
		l.Visible = 10
	}
	now := clock.Now()
	for i := l.Top; i < len(l.Items) && i < l.Top+l.Visible; i++ {
		it := l.Items[i]
		ry := y + float64(i-l.Top)*rowH
		sel := i == l.Sel
		if sel {
			glow := 0.85 + 0.15*math.Sin(now*5)
			render.RoundRect(dst, x-16, ry-6, w+32, rowH-6, 12, render.WithAlpha(th.Accent, 0.18*glow))
			render.FillRect(dst, x-16, ry-6, 5, rowH-6, th.Accent)
		}
		c := th.Text
		if !sel {
			c = render.Mix(th.Text, th.TextDim, 0.35)
		}
		if it.Disabled {
			c = render.WithAlpha(th.TextDim, 0.6)
		}
		render.Text(dst, it.Label, render.UI, 34, x, ry, c, render.Left)
		if it.Value != nil {
			v := it.Value()
			if it.Adjust != nil && sel {
				v = "<  " + v + "  >"
			}
			render.Text(dst, v, render.UI, 32, x+w, ry+2, c, render.Right)
		}
	}
	var b render.Batch
	cx := x + w/2
	if l.Top > 0 {
		b.Quad(dst, cx-14, y-14, cx+14, y-14, cx+14, y-14, cx, y-30, th.TextDim, th.TextDim, th.TextDim, th.TextDim)
	}
	if l.Top+l.Visible < len(l.Items) {
		yb := y + float64(l.Visible)*rowH
		b.Quad(dst, cx-14, yb, cx+14, yb, cx+14, yb, cx, yb+16, th.TextDim, th.TextDim, th.TextDim, th.TextDim)
	}
	b.Flush(dst)
}

// Hint returns the selected item's hint.
func (l *List) Hint() string {
	if l.Sel >= 0 && l.Sel < len(l.Items) {
		return l.Items[l.Sel].Hint
	}
	return ""
}

// Header draws a screen title and subtitle.
func Header(dst *ebiten.Image, title, sub string) {
	render.Text(dst, title, render.Display, 72, 96, 54, th.Text, render.Left)
	if sub != "" {
		render.Text(dst, sub, render.UI, 28, 100, 140, th.TextDim, render.Left)
	}
	render.FillRect(dst, 96, 186, 140, 5, th.Accent)
}

// Footer draws control hints along the bottom.
func Footer(dst *ebiten.Image, s string) {
	render.FillRect(dst, 0, render.ScreenH-60, render.ScreenW, 60, color.RGBA{0, 0, 0, 0x70})
	render.Text(dst, s, render.Cond, 26, 96, render.ScreenH-48, th.TextDim, render.Left)
}

// Panel draws a rounded translucent panel.
func Panel(dst *ebiten.Image, x, y, w, h float64) {
	render.RoundRect(dst, x, y, w, h, 18, th.Panel)
}

// MenuBackground is the shared animated menu backdrop.
func MenuBackground(dst *ebiten.Image) {
	now := clock.Now()
	render.Background(dst, now, false)
	// slow drifting highway lines for a bit of life
	var b render.Batch
	for i := 0; i < 14; i++ {
		y := math.Mod(float64(i)*90+now*60, 1260) - 90
		a := 0.05 + 0.03*math.Sin(float64(i))
		b.Line(dst, 1100, y, 1920, y+300, 2, render.WithAlpha(th.Accent, a), render.WithAlpha(th.Accent, 0))
	}
	b.Flush(dst)
}
