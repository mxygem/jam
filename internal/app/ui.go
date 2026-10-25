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
//
// A Range item is a number: green (or Enter) starts editing it, strum up and
// down (or the arrows) change it, faster the longer they're held, and green
// or red finishes. Its Adjust gets steps larger than one while held.
type Item struct {
	Label    string
	Value    func() string
	Adjust   func(dir int)
	Activate func()
	Hint     string
	Disabled bool
	Range    bool
	// Header items are section titles: drawn small and skipped over.
	Header bool
	// Swatch, if set, is a color shown beside the value.
	Swatch func() color.RGBA
	// Extra, if set, draws something in the row between label and value
	// (a color bar, say).
	Extra func(dst *ebiten.Image, x, y, w, h float64)
	// Gap is extra space above the row; Tall, if set, is the row's own
	// height. Button rows are drawn as a centered button filling the row.
	Gap    float64
	Tall   float64
	Button bool
}

func (it *Item) skip() bool { return it.Disabled || it.Header }

// List is a vertical, scrollable menu.
type List struct {
	Items   []Item
	Sel     int
	Top     int
	Visible int

	editing bool // changing a Range item's value

	// where it was last drawn, for the mouse
	gx, gy, gw, gh float64
	drawn          bool
}

// Editing reports whether a Range item is being changed.
func (l *List) Editing() bool { return l.editing }

// rangeStep is how big a step held input makes: one at first, then
// bigger the longer the strum (or arrow) is held.
func rangeStep(a *App, act action.Action) int {
	held := 0.0
	if a.Menu != nil {
		held = a.Menu.HeldFor(act, clock.Now())
	}
	switch {
	case held > 4:
		return 50
	case held > 2.5:
		return 20
	case held > 1.2:
		return 5
	}
	return 1
}

// updateEditing changes the value of the item being edited.
func (l *List) updateEditing(a *App) {
	it := l.Items[l.Sel]
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp, action.MenuRight:
			it.Adjust(rangeStep(a, act))
		case action.MenuDown, action.MenuLeft:
			it.Adjust(-rangeStep(a, act))
		case action.MenuConfirm, action.MenuBack:
			l.editing = false
			return
		}
	}
	m := &a.Mouse
	switch {
	case m.Wheel > 0:
		it.Adjust(1)
	case m.Wheel < 0:
		it.Adjust(-1)
	}
	if m.Click || m.Right {
		l.editing = false
	}
}

// Update handles navigation. It returns true if an item was activated, or
// if input went to editing a value (callers then ignore it, so red
// finishing an edit doesn't also leave the menu).
func (l *List) Update(a *App) bool {
	if len(l.Items) == 0 {
		return false
	}
	if l.Visible == 0 {
		l.Visible = 10
	}
	if l.editing {
		if l.Sel < len(l.Items) && l.Items[l.Sel].Adjust != nil {
			l.updateEditing(a)
			return true
		}
		l.editing = false
	}
	move := func(d int) {
		for i := 0; i < len(l.Items); i++ {
			l.Sel = (l.Sel + d + len(l.Items)) % len(l.Items)
			if !l.Items[l.Sel].skip() {
				break
			}
		}
	}
	if l.Items[l.Sel].skip() {
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
			if it := l.Items[l.Sel]; it.Range && it.Adjust != nil {
				l.editing = true
				return true
			} else if it.Activate != nil {
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
	if l.Sel == l.Top && l.Sel > 0 && l.Items[l.Sel-1].Header {
		l.Top-- // show the section title above its first item
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
	if m.Wheel != 0 && m.In(l.gx-20, l.gy-20, l.gw+40, l.height()+40) {
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
	i := l.rowAt(m.Y)
	if m.X < l.gx-16 || m.X > l.gx+l.gw+16 || i < 0 {
		return false
	}
	if l.Items[i].skip() {
		return false
	}
	l.Sel = i
	if !m.Click {
		return false
	}
	it := l.Items[i]
	switch {
	case it.Swatch != nil && it.Activate != nil:
		it.Activate() // a color: open its editor
		return true
	case it.Range && it.Adjust != nil:
		l.editing = true
		return true
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
		ry, rowH := l.row(i)
		// Text sits centered in the selection bar (ry-6, rowH-6): the
		// label's caps are centered 23 px below where it's drawn.
		ty := ry + rowH/2 - 32
		if it.Header {
			render.Text(dst, strings.ToUpper(it.Label), render.CondBold, 24, x, ry+rowH*0.34, th.Accent, render.Left)
			tw, _ := render.Measure(strings.ToUpper(it.Label), render.CondBold, 24)
			render.FillRect(dst, x+tw+16, ry+rowH*0.34+14, w-tw-16, 2, render.WithAlpha(th.TextDim, 0.35))
			continue
		}
		sel := i == l.Sel
		if it.Button {
			drawButtonRow(dst, it.Label, x-16, ry-6, w+32, rowH-6, ty, sel, it.Disabled, now)
			continue
		}
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
		render.Text(dst, it.Label, render.UI, 34, x, ty, c, render.Left)
		if it.Extra != nil {
			it.Extra(dst, x, ry, w, rowH)
		}
		if it.Value != nil {
			v := it.Value()
			if it.Swatch != nil {
				vw, _ := render.Measure(v, render.UI, 32)
				if it.Adjust != nil && sel && !l.editing {
					vw, _ = render.Measure("<  "+v+"  >", render.UI, 32)
				}
				drawSwatch(dst, x+w-vw-62, ry+2, 46, rowH-20, it.Swatch())
			}
			switch {
			case sel && l.editing:
				// value being changed: accent box with up/down marks
				vw, _ := render.Measure(v, render.UIBold, 32)
				bx := x + w - vw - 44
				render.RoundRect(dst, bx-14, ry-4, vw+72, rowH-10, 10, render.WithAlpha(th.Accent, 0.9))
				render.Text(dst, v, render.UIBold, 32, bx, ty+1, th.Text, render.Left)
				var b render.Batch
				ax, ay := x+w+6, ry+rowH*0.38
				b.Quad(dst, ax-9, ay-3, ax+9, ay-3, ax+9, ay-3, ax, ay-14, th.Text, th.Text, th.Text, th.Text)
				b.Quad(dst, ax-9, ay+5, ax+9, ay+5, ax+9, ay+5, ax, ay+16, th.Text, th.Text, th.Text, th.Text)
				b.Flush(dst)
			case it.Adjust != nil && sel:
				render.Text(dst, "<  "+v+"  >", render.UI, 32, x+w, ty+2, c, render.Right)
			default:
				render.Text(dst, v, render.UI, 32, x+w, ty+2, c, render.Right)
			}
		}
	}
	var b render.Batch
	cx := x + w/2
	if l.Top > 0 {
		b.Quad(dst, cx-14, y-14, cx+14, y-14, cx+14, y-14, cx, y-30, th.TextDim, th.TextDim, th.TextDim, th.TextDim)
	}
	if l.Top+l.Visible < len(l.Items) {
		yb := y + l.height()
		b.Quad(dst, cx-14, yb, cx+14, yb, cx+14, yb, cx, yb+16, th.TextDim, th.TextDim, th.TextDim, th.TextDim)
	}
	b.Flush(dst)
}

// row returns where item i's row starts and how tall it is, for the
// current scroll position (it must be visible).
func (l *List) row(i int) (float64, float64) {
	y := l.gy
	for j := l.Top; j <= i && j < len(l.Items); j++ {
		y += l.Items[j].Gap
		if j == i {
			break
		}
		y += l.rowH(j)
	}
	return y, l.rowH(i)
}

func (l *List) rowH(i int) float64 {
	if i >= 0 && i < len(l.Items) && l.Items[i].Tall > 0 {
		return l.Items[i].Tall
	}
	return l.gh
}

// height is how tall the visible rows are.
func (l *List) height() float64 {
	last := min(len(l.Items), l.Top+l.Visible) - 1
	if last < l.Top {
		return 0
	}
	ry, h := l.row(last)
	return ry + h - l.gy
}

// rowAt returns the visible item whose row (with its selection bar, which
// starts 6 px above it) covers screen y, or -1.
func (l *List) rowAt(y float64) int {
	for i := l.Top; i < len(l.Items) && i < l.Top+l.Visible; i++ {
		ry, h := l.row(i)
		if y >= ry-6 && y < ry-6+h {
			return i
		}
	}
	return -1
}

// drawButtonRow draws a list row as a button: an outlined box with the
// label centered, filled in the accent color when selected.
func drawButtonRow(dst *ebiten.Image, label string, x, y, w, h, ty float64, sel, disabled bool, now float64) {
	c := th.Text
	if sel {
		glow := 0.85 + 0.15*math.Sin(now*5)
		render.RoundRect(dst, x, y, w, h, 12, render.WithAlpha(th.Accent, 0.9*glow))
	} else {
		render.RoundRect(dst, x, y, w, h, 12, render.WithAlpha(th.TextDim, 0.55))
		bg := th.Panel
		bg.A = 0xFF
		render.RoundRect(dst, x+2, y+2, w-4, h-4, 10, bg)
		render.RoundRect(dst, x+2, y+2, w-4, h-4, 10, render.WithAlpha(th.Text, 0.05))
		c = render.Mix(th.Text, th.TextDim, 0.35)
	}
	if disabled {
		c = render.WithAlpha(th.TextDim, 0.6)
	}
	render.Text(dst, label, render.UIBold, 34, x+w/2, ty, c, render.Center)
}

// drawSwatch draws a color sample with a thin border.
func drawSwatch(dst *ebiten.Image, x, y, w, h float64, c color.RGBA) {
	render.RoundRect(dst, x-2, y-2, w+4, h+4, 9, render.WithAlpha(th.TextDim, 0.8))
	render.RoundRect(dst, x, y, w, h, 7, render.RGBA(0x10, 0x10, 0x14, 0xFF))
	render.RoundRect(dst, x, y, w, h, 7, render.Premul(c))
}

// Hint returns the selected item's hint.
func (l *List) Hint() string {
	if l.editing {
		return "Strum up or down to change it (hold to go faster). Green or red when done."
	}
	if l.Sel >= 0 && l.Sel < len(l.Items) {
		h := l.Items[l.Sel].Hint
		if l.Items[l.Sel].Range {
			if h != "" {
				h += ". "
			}
			h += "Green to change"
		}
		return h
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
	render.MenuBackdrop(dst, now)
	// slow drifting highway lines for a bit of life, at the right edge
	img, w, h, _, _ := render.Backdrop(dst)
	var b render.Batch
	for i := 0; i < 14; i++ {
		y := math.Mod(float64(i)*90+now*60, h+180) - 90
		a := 0.05 + 0.03*math.Sin(float64(i))
		b.Line(img, w-820, y, w, y+300, 2, render.WithAlpha(th.Accent, a), render.WithAlpha(th.Accent, 0))
	}
	b.Flush(img)
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
	for i, line := range wrapText(s, f, size, w) {
		if i == 3 {
			break
		}
		render.Text(dst, line, f, size, x, y+float64(i)*size*1.3, c, render.Left)
	}
}

// wrapText splits s into lines no wider than w, breaking between words (a
// single word wider than w gets a line of its own).
func wrapText(s string, f render.Font, size, w float64) []string {
	var out []string
	line := ""
	for _, wd := range strings.Fields(s) {
		try := wd
		if line != "" {
			try = line + " " + wd
		}
		if tw, _ := render.Measure(try, f, size); tw > w && line != "" {
			out = append(out, line)
			line = wd
		} else {
			line = try
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
