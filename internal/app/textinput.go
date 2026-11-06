package app

import (
	"math"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/render"
)

// TextInput is a one-line text field. It's typed on the keyboard, or on an
// on-screen keyboard played with the guitar: that opens by itself when a
// controller was the last thing used, and goes away when a key is typed.
type TextInput struct {
	Value       string
	Placeholder string
	Max         int // runes; 0 = 120

	bsHeld float64
	bsLast float64

	started bool
	osk     bool // the on-screen keyboard is up
	row     int  // its selected key
	col     int
	shift   bool
	drawn   bool // where it was last drawn, for the mouse
}

// Keyboard reports whether the on-screen keyboard is showing.
func (t *TextInput) Keyboard() bool { return t.osk }

func (t *TextInput) limit() int {
	if t.Max == 0 {
		return 120
	}
	return t.Max
}

func (t *TextInput) add(r rune) {
	if r >= 32 && r != 127 && len([]rune(t.Value)) < t.limit() {
		t.Value += string(r)
	}
}

func (t *TextInput) backspace() {
	if rs := []rune(t.Value); len(rs) > 0 {
		t.Value = string(rs[:len(rs)-1])
	}
}

// Update reads typing. It returns done on Enter and cancel on Escape.
func (t *TextInput) Update(a *App) (done, cancel bool) {
	if !t.started {
		t.started = true
		t.osk = a.padNav
		t.row, t.col = 1, 0
	}
	for _, e := range a.Raw {
		if e.Down && e.Kind != input.KindAxis {
			t.osk = e.Src != input.SrcKeyboard
		}
	}
	if t.osk {
		return t.updateKeyboard(a)
	}
	for _, r := range a.Chars {
		t.add(r)
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
		} else {
			t.backspace()
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

// Draw draws the field with a blinking caret, and the on-screen keyboard
// when it's up.
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
	if t.osk {
		t.drawKeyboard(dst)
	}
}

// --- on-screen keyboard ---

// oskKey is a key: its text, or a command.
type oskKey struct {
	label string
	cmd   oskCmd
	span  int // columns wide
}

type oskCmd int

const (
	oskChar oskCmd = iota
	oskShift
	oskSpace
	oskDelete
	oskClear
	oskCancel
	oskDone
)

const oskCols = 11

func charRow(s string) []oskKey {
	var out []oskKey
	for _, r := range s {
		out = append(out, oskKey{label: string(r), span: 1})
	}
	return out
}

// Paths and names need the symbols on the right.
var oskRows = [][]oskKey{
	charRow(`1234567890-`),
	charRow(`qwertyuiop\`),
	charRow(`asdfghjkl':`),
	charRow(`zxcvbnm,./_`),
	{{"Shift", oskShift, 2}, {"Space", oskSpace, 2}, {"Delete", oskDelete, 2}, {"Clear", oskClear, 1}, {"Cancel", oskCancel, 2}, {"Done", oskDone, 2}},
}

// oskAt finds the key covering a column of a row.
func oskAt(row, col int) int {
	c := 0
	for i, k := range oskRows[row] {
		c += k.span
		if col < c {
			return i
		}
	}
	return len(oskRows[row]) - 1
}

func oskStart(row, key int) int {
	c := 0
	for _, k := range oskRows[row][:key] {
		c += k.span
	}
	return c
}

func (t *TextInput) moveKey(dr, dc int) {
	if dr != 0 {
		t.row = (t.row + dr + len(oskRows)) % len(oskRows)
		return
	}
	row := oskRows[t.row]
	i := (oskAt(t.row, t.col) + dc + len(row)) % len(row)
	t.col = oskStart(t.row, i)
}

// keyLabel is what a key shows right now.
func (t *TextInput) keyLabel(k oskKey) string {
	if k.cmd == oskChar && t.shift {
		return strings.ToUpper(k.label)
	}
	return k.label
}

func (t *TextInput) press(k oskKey) (done, cancel bool) {
	switch k.cmd {
	case oskChar:
		for _, r := range t.keyLabel(k) {
			t.add(r)
		}
		if unicode.IsLetter([]rune(k.label)[0]) {
			t.shift = false // Shift is for one letter
		}
	case oskShift:
		t.shift = !t.shift
	case oskSpace:
		t.add(' ')
	case oskDelete:
		t.backspace()
	case oskClear:
		t.Value = ""
	case oskCancel:
		return false, true
	case oskDone:
		return true, false
	}
	return false, false
}

// updateKeyboard plays the on-screen keyboard: strum and blue/orange (or the
// d-pad) move, green presses, red deletes, yellow is a space, Start is done.
func (t *TextInput) updateKeyboard(a *App) (done, cancel bool) {
	if a.Pressed(action.MenuStart) {
		return true, false
	}
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp:
			t.moveKey(-1, 0)
		case action.MenuDown:
			t.moveKey(1, 0)
		case action.MenuLeft:
			t.moveKey(0, -1)
		case action.MenuRight:
			t.moveKey(0, 1)
		case action.MenuOptions:
			t.add(' ')
		case action.MenuBack:
			if t.Value == "" {
				return false, true
			}
			t.backspace()
		case action.MenuConfirm:
			if done, cancel := t.press(oskRows[t.row][oskAt(t.row, t.col)]); done || cancel {
				return done, cancel
			}
		}
	}
	if !t.drawn {
		return false, false
	}
	m := &a.Mouse
	if m.Click || m.Moved {
		for r, row := range oskRows {
			for i, k := range row {
				x, y, w, h := oskRect(r, i)
				if !m.In(x, y, w, h) {
					continue
				}
				t.row, t.col = r, oskStart(r, i)
				if m.Click {
					return t.press(k)
				}
			}
		}
	}
	return false, false
}

// Keyboard layout on the 1920x1080 canvas. Every text box sits above it.
const (
	oskX, oskY   = 354.0, 660.0
	oskKeyW      = 110.0
	oskKeyH      = 62.0
	oskPanelW    = oskCols*oskKeyW + 40
	oskPanelH    = 5*oskKeyH + 70
	oskLegendTop = oskY + 5*oskKeyH + 6
)

func oskRect(row, key int) (x, y, w, h float64) {
	k := oskRows[row][key]
	return oskX + float64(oskStart(row, key))*oskKeyW, oskY + float64(row)*oskKeyH, float64(k.span)*oskKeyW - 6, oskKeyH - 6
}

func (t *TextInput) drawKeyboard(dst *ebiten.Image) {
	t.drawn = true
	Panel(dst, oskX-20, oskY-20, oskPanelW, oskPanelH)
	sel := oskAt(t.row, t.col)
	for r, row := range oskRows {
		for i, k := range row {
			x, y, w, h := oskRect(r, i)
			c := render.WithAlpha(th.TextDim, 0.16)
			tc := th.Text
			switch {
			case r == t.row && i == sel:
				c = th.Accent
			case k.cmd == oskDone:
				c = render.WithAlpha(th.Good, 0.45)
			case k.cmd == oskShift && t.shift:
				c = render.WithAlpha(th.Accent, 0.5)
			}
			render.RoundRect(dst, x, y, w, h, 10, c)
			f, size := render.UIBold, 32.0
			if k.span > 1 || k.cmd != oskChar {
				f, size = render.CondBold, 26
			}
			render.Text(dst, t.keyLabel(k), f, size, x+w/2, y+h/2-size*0.68, tc, render.Center)
		}
	}
	render.Text(dst, "Strum up/down, blue/orange or d-pad move   Green press   Red delete   Yellow space   Start done",
		render.Cond, 24, oskX, oskLegendTop, th.TextDim, render.Left)
}
