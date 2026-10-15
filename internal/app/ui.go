package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"

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

	// where it was last drawn, for the mouse
	gx, gy, gw, gh float64
	drawn          bool
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
	if l.mouse(a) {
		return true
	}
	if l.Sel < l.Top {
		l.Top = l.Sel
	}
	if l.Sel >= l.Top+l.Visible {
		l.Top = l.Sel - l.Visible + 1
	}
	return false
}

// mouse handles hover, click and wheel. It returns true if an item was
// activated.
func (l *List) mouse(a *App) bool {
	m := &a.Mouse
	if !l.drawn {
		return false
	}
	if m.Wheel != 0 && m.In(l.gx-20, l.gy-20, l.gw+40, l.gh*float64(l.Visible)+40) {
		steps := int(-m.Wheel)
		if steps == 0 {
			steps = -1
			if m.Wheel < 0 {
				steps = 1
			}
		}
		l.Top = max(0, min(len(l.Items)-l.Visible, l.Top+steps))
		l.Sel = max(l.Top, min(l.Top+l.Visible-1, l.Sel))
	}
	if !m.Moved && !m.Click {
		return false
	}
	i := l.Top + int((m.Y-l.gy+6)/l.gh)
	if m.X < l.gx-16 || m.X > l.gx+l.gw+16 || m.Y < l.gy-6 || i < l.Top || i >= len(l.Items) || i >= l.Top+l.Visible {
		return false
	}
	if l.Items[i].Disabled {
		return false
	}
	l.Sel = i
	if !m.Click {
		return false
	}
	it := l.Items[i]
	switch {
	case it.Adjust != nil && it.Value != nil:
		// clicking the left part of the value steps back, elsewhere forward
		if m.X > l.gx+l.gw*0.55 && m.X < l.gx+l.gw*0.78 {
			it.Adjust(-1)
		} else {
			it.Adjust(1)
		}
	case it.Activate != nil:
		it.Activate()
		return true
	case it.Adjust != nil:
		it.Adjust(1)
	}
	return false
}

// Draw renders the list with its top-left at x, y.
func (l *List) Draw(dst *ebiten.Image, x, y, w, rowH float64) {
	if l.Visible == 0 {
		l.Visible = 10
	}
	l.gx, l.gy, l.gw, l.gh, l.drawn = x, y, w, rowH, true
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
	render.RichText(dst, title, render.Display, 72, 96, 54, th.Text, render.Left, 1700)
	if sub != "" {
		render.RichText(dst, sub, render.UI, 28, 100, 140, th.TextDim, render.Left, 1700)
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
	c := th.Panel
	c.A = 0xFF // opaque so dialogs don't show what's behind them
	render.RoundRect(dst, x, y, w, h, 18, c)
}

// GlassPanel is a panel that lets a backdrop (album art) show through.
func GlassPanel(dst *ebiten.Image, x, y, w, h float64) {
	render.RoundRect(dst, x, y, w, h, 18, color.RGBA{0x0c, 0x0e, 0x18, 0xB8})
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

func fmtDur(sec float64) string {
	if sec <= 0 {
		return "?"
	}
	m := int(sec) / 60
	return fmt.Sprintf("%d:%02d", m, int(sec)%60)
}

func sortStrings(xs []string) []string {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
	return xs
}

// truncateW shortens s with an ellipsis to fit width w.
func truncateW(s string, f render.Font, size, w float64) string {
	if tw, _ := render.Measure(s, f, size); tw <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		if tw, _ := render.Measure(string(r)+"...", f, size); tw <= w {
			return string(r) + "..."
		}
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// drawWrapped draws text word-wrapped to width w (at most 3 lines).
func drawWrapped(dst *ebiten.Image, s string, f render.Font, size, x, y, w float64, c color.Color) {
	words := strings.Fields(s)
	line := ""
	lines := 0
	flush := func() {
		if line != "" && lines < 3 {
			render.Text(dst, line, f, size, x, y+float64(lines)*size*1.3, c, render.Left)
			lines++
		}
		line = ""
	}
	for _, wd := range words {
		try := wd
		if line != "" {
			try = line + " " + wd
		}
		if tw, _ := render.Measure(try, f, size); tw > w && line != "" {
			flush()
			line = wd
		} else {
			line = try
		}
	}
	flush()
}
