package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/render"
)

// paletteColors are the quick picks: left/right on a color setting steps
// through them, and the color editor has them too.
var paletteColors = []struct {
	name string
	c    color.RGBA
}{
	{"Red", hexColor("#F23A3A")},
	{"Crimson", hexColor("#C8102E")},
	{"Coral", hexColor("#FF6F61")},
	{"Orange", hexColor("#FF8A1F")},
	{"Amber", hexColor("#FFB000")},
	{"Yellow", hexColor("#FFD01F")},
	{"Lime", hexColor("#A8E61D")},
	{"Green", hexColor("#2BD65A")},
	{"Emerald", hexColor("#00A86B")},
	{"Teal", hexColor("#14B8A6")},
	{"Cyan", hexColor("#22D3EE")},
	{"Sky", hexColor("#56B4E9")},
	{"Blue", hexColor("#2F86FF")},
	{"Navy", hexColor("#1E3A8A")},
	{"Indigo", hexColor("#5B5BD6")},
	{"Purple", hexColor("#B05CFF")},
	{"Magenta", hexColor("#E040FB")},
	{"Pink", hexColor("#FF5CA8")},
	{"Rose", hexColor("#FF9AA2")},
	{"White", hexColor("#F2F4F8")},
	{"Silver", hexColor("#C8D2E6")},
	{"Gray", hexColor("#8C93A8")},
	{"Charcoal", hexColor("#2A2D36")},
	{"Black", hexColor("#0C0F1A")},
}

func hexColor(s string) color.RGBA {
	c, ok := render.ParseHex(s)
	if !ok {
		panic("bad color " + s)
	}
	return c
}

// nearestPalette is the palette color closest to c.
func nearestPalette(c color.RGBA) int {
	best, bi := math.Inf(1), 0
	for i, p := range paletteColors {
		dr, dg, db := float64(c.R)-float64(p.c.R), float64(c.G)-float64(p.c.G), float64(c.B)-float64(p.c.B)
		if d := dr*dr + dg*dg + db*db; d < best {
			best, bi = d, i
		}
	}
	return bi
}

// paletteStep returns the palette color d steps from the one nearest c
// (an exact match counts as the starting point).
func paletteStep(c color.RGBA, d int) int {
	i := nearestPalette(c)
	if paletteColors[i].c.R != c.R || paletteColors[i].c.G != c.G || paletteColors[i].c.B != c.B {
		if d > 0 {
			d--
		} else if d < 0 {
			d++
		}
		if d == 0 {
			return i
		}
	}
	n := len(paletteColors)
	return ((i+d)%n + n) % n
}

// setSlotColor changes one theme color (making the look custom).
func setSlotColor(a *App, slot render.ColorSlot, c color.RGBA) {
	editLook(a, func(t *render.Theme, _ *render.Look) { slot.Set(t, c) })
}

// colorPicker edits one theme color: hue, saturation and brightness,
// quick picks, a typed hex code, or back to the theme's own color.
type colorPicker struct {
	slot    render.ColorSlot
	orig    color.RGBA
	h, s, v float64
	list    List
	typing  *TextInput
}

func newColorPicker(a *App, slot render.ColorSlot, visible int) *colorPicker {
	p := &colorPicker{slot: slot}
	p.orig = slot.Get(&render.DefaultTheme)
	p.h, p.s, p.v = render.HSV(p.orig)
	p.list.Visible = visible
	cur := func() color.RGBA { return slot.Get(&render.DefaultTheme) }
	pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
	bar := func(left, right func() color.RGBA, hue bool, at func() float64) func(dst *ebiten.Image, x, y, w, h float64) {
		return func(dst *ebiten.Image, x, y, w, h float64) {
			bx, bw := x+220, w-220-150
			if bw < 60 {
				return
			}
			by, bh := y+h*0.22, h*0.32
			var b render.Batch
			if hue {
				for i := 0; i < 12; i++ {
					c0 := render.FromHSV(float64(i)*30, p.s*0.85+0.15, math.Max(p.v, 0.35))
					c1 := render.FromHSV(float64(i+1)*30, p.s*0.85+0.15, math.Max(p.v, 0.35))
					x0 := bx + bw*float64(i)/12
					x1 := bx + bw*float64(i+1)/12
					b.Quad(dst, x0, by, x1, by, x1, by+bh, x0, by+bh, c0, c1, c1, c0)
				}
			} else {
				l, r := left(), right()
				b.Quad(dst, bx, by, bx+bw, by, bx+bw, by+bh, bx, by+bh, l, r, r, l)
			}
			mx := bx + bw*at()
			b.Rect(dst, mx-3, by-6, 6, bh+12, th.Text, th.Text)
			b.Flush(dst)
		}
	}
	p.list.Items = []Item{
		{Label: "Hue", Hint: "Around the color wheel", Range: true,
			Value: func() string { return fmt.Sprintf("%.0f°", p.h) },
			Adjust: func(d int) {
				p.h = math.Mod(p.h+float64(d)+360, 360)
				p.apply(a)
			},
			Extra: bar(nil, nil, true, func() float64 { return p.h / 360 })},
		{Label: "Saturation", Hint: "From gray to full color", Range: true,
			Value: func() string { return pct(p.s) },
			Adjust: func(d int) {
				p.s = clampf(math.Round(p.s*100+float64(d))/100, 0, 1)
				p.apply(a)
			},
			Extra: bar(func() color.RGBA { return render.FromHSV(p.h, 0, p.v) }, func() color.RGBA { return render.FromHSV(p.h, 1, p.v) }, false, func() float64 { return p.s })},
		{Label: "Brightness", Hint: "From black to full brightness", Range: true,
			Value: func() string { return pct(p.v) },
			Adjust: func(d int) {
				p.v = clampf(math.Round(p.v*100+float64(d))/100, 0, 1)
				p.apply(a)
			},
			Extra: bar(func() color.RGBA { return render.FromHSV(p.h, p.s, 0) }, func() color.RGBA { return render.FromHSV(p.h, p.s, 1) }, false, func() float64 { return p.v })},
		{Label: "Quick picks", Hint: "Left and right step through a set of colors",
			Value:  func() string { return paletteColors[nearestPalette(cur())].name },
			Swatch: cur,
			Adjust: func(d int) { p.set(a, paletteColors[paletteStep(cur(), d)].c) }},
		{Label: "Type a color code", Hint: "A hex code like #2BD65A (keyboard)",
			Value:    func() string { return render.Hex(color.RGBA{cur().R, cur().G, cur().B, 0xFF}) },
			Activate: func() { p.typing = &TextInput{Placeholder: "#2BD65A", Max: 9} }},
		{Label: "Use the theme's color", Hint: "Back to this color in the theme the look started from",
			Swatch: func() color.RGBA {
				if c, ok := p.themeColor(a); ok {
					return c
				}
				return cur()
			},
			Value: func() string { return "" },
			Activate: func() {
				if c, ok := p.themeColor(a); ok {
					p.set(a, c)
				}
			}},
		{Label: "Undo changes", Hint: "Back to the color this had when you opened it",
			Swatch: func() color.RGBA { return p.orig }, Value: func() string { return "" },
			Activate: func() { p.set(a, p.orig) }},
		{Label: "Done", Activate: func() {}},
	}
	return p
}

// themeColor is the slot's color in the theme the look is based on.
func (p *colorPicker) themeColor(a *App) (color.RGBA, bool) {
	spec, ok := baseColor(a, p.slot)
	if !ok {
		return color.RGBA{}, false
	}
	t, _ := spec.Resolve(1)
	return p.slot.Get(&t), true
}

func (p *colorPicker) apply(a *App) {
	setSlotColor(a, p.slot, render.FromHSV(p.h, p.s, p.v))
}

// set changes the color outright (keeping the hue when it's a gray).
func (p *colorPicker) set(a *App, c color.RGBA) {
	h, s, v := render.HSV(c)
	if s > 0 {
		p.h = h
	}
	p.s, p.v = s, v
	setSlotColor(a, p.slot, color.RGBA{c.R, c.G, c.B, 0xFF})
}

// update handles input; it returns true when the editor closes.
func (p *colorPicker) update(a *App) bool {
	if p.typing != nil {
		done, cancel := p.typing.Update(a)
		switch {
		case cancel:
			p.typing = nil
		case done:
			if c, ok := render.ParseHex(p.typing.Value); ok {
				p.set(a, c)
			} else {
				a.Toast("Not a color code: " + p.typing.Value)
			}
			p.typing = nil
		}
		return false
	}
	if p.list.Update(a) {
		return p.list.Items[p.list.Sel].Label == "Done"
	}
	return a.Pressed(action.MenuBack) || a.Mouse.Right
}

func (p *colorPicker) drawTyping(dst *ebiten.Image) {
	if p.typing == nil {
		return
	}
	render.Dim(dst, render.RGBA(0, 0, 0, 0xB8))
	Panel(dst, 560, 400, 800, 240)
	render.Text(dst, p.slot.Label+": color code", render.UIBold, 36, 600, 430, th.Text, render.Left)
	p.typing.Draw(dst, 600, 500, 720)
	render.Text(dst, "Enter to use it, Esc to cancel", render.Cond, 24, 600, 580, th.TextDim, render.Left)
}
