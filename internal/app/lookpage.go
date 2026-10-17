package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/mxygem/jam/internal/render"
)

// The Appearance settings page: theme, note material, rim and size,
// highway style, and every theme color one by one.

const pageLooks = "looks"

func lookItems(a *App, m *settingsMenu, inSong bool) []Item {
	lk := &a.Settings.Look
	items := []Item{
		{Label: "Theme", Hint: "Colors, note material and highway style together. Changing anything below makes a custom look",
			Value:  func() string { return truncateW(currentTheme(a).label, render.UI, 32, 380) },
			Adjust: func(d int) { cycleTheme(a, sign(d)) }},
	}
	if !inSong {
		items = append(items,
			Item{Label: "Save as a theme file...", Hint: "Keep this look in " + ThemesDir() + " to come back to or share",
				Activate: func() {
					name := "My theme"
					if c := lk.Custom; lk.Theme == themeCustom && c != nil && c.Base != "" {
						name = "My " + c.Base
					}
					m.startTyping("Name the theme", name, func(s string) { saveThemeFile(a, s) })
				}},
			Item{Label: "Open the themes folder", Hint: ThemesDir(), Activate: func() { openFolder(a, ThemesDir()) }},
			Item{Label: "Preview", Hint: "Which part plays in the preview",
				Value:  func() string { return map[bool]string{false: "Guitar", true: "Drums"}[m.previewDrums] },
				Adjust: func(int) { m.previewDrums = !m.previewDrums }},
		)
	}
	choice := func(label, hint string, n int, get func(render.Look) int, set func(*render.Look, int), name func(int) string) Item {
		return Item{Label: label, Hint: hint,
			Value: func() string { return name(get(render.CurrentLook)) },
			Adjust: func(d int) {
				editLook(a, func(_ *render.Theme, l *render.Look) { set(l, ((get(*l)+sign(d))%n+n)%n) })
			}}
	}
	pct := func(v float64) string { return fmt.Sprintf("%.0f%%", math.Round(v*100)) }
	amount := func(label, hint string, lo, hi float64, get func(render.Look) float64, set func(*render.Look, float64)) Item {
		return Item{Label: label, Hint: hint, Range: true,
			Value: func() string { return pct(get(render.CurrentLook)) },
			Adjust: func(d int) {
				editLook(a, func(_ *render.Theme, l *render.Look) {
					set(l, clampf(math.Round(get(*l)*100+float64(d))/100, lo, hi))
				})
			}}
	}
	items = append(items,
		Item{Label: "Notes", Header: true},
		choice("Note material", "What gems and fret buttons look like they're made of", int(render.MatFlat)+1,
			func(l render.Look) int { return int(l.Material) }, func(l *render.Look, v int) { l.Material = render.Material(v) },
			func(v int) string { return render.Material(v).Label() }),
		choice("Note rim", "The ring around the top of each gem", int(render.RimLane)+1,
			func(l render.Look) int { return int(l.Rim) }, func(l *render.Look, v int) { l.Rim = render.Rim(v) },
			func(v int) string { return render.Rim(v).Label() }),
		Item{Label: "Note size", Hint: "Gem size. Not part of the theme", Range: true,
			Value: func() string { return pct(lk.NoteSize) },
			Adjust: func(d int) {
				lk.NoteSize = clampf(math.Round(lk.NoteSize*100+float64(d))/100, 0.6, 1.4)
				l := render.CurrentLook
				l.NoteSize = lk.NoteSize
				render.SetLook(l)
			}},
		Item{Label: "Highway", Header: true},
		choice("Highway style", "How the highway surface is painted", int(render.HwGlass)+1,
			func(l render.Look) int { return int(l.Highway) }, func(l *render.Look, v int) { l.Highway = render.HighwayStyle(v) },
			func(v int) string { return render.HighwayStyle(v).Label() }),
		amount("Highway opacity", "How solid the highway is: lower lets the background show through", 0, 1,
			func(l render.Look) float64 { return l.HighwayOpacity }, func(l *render.Look, v float64) { l.HighwayOpacity = v }),
		amount("Beat lines", "How bright the lines across the highway are (0% hides them)", 0, 3,
			func(l render.Look) float64 { return l.BeatLines }, func(l *render.Look, v float64) { l.BeatLines = v }),
		amount("Lane lines", "How bright the lines between lanes are (0% hides them)", 0, 3,
			func(l render.Look) float64 { return l.LaneLines }, func(l *render.Look, v float64) { l.LaneLines = v }),
	)
	group := ""
	for _, slot := range render.ColorSlots {
		slot := slot
		if slot.Group != group {
			group = slot.Group
			items = append(items, Item{Label: group, Header: true})
		}
		cur := func() color.RGBA { return slot.Get(&render.DefaultTheme) }
		items = append(items, Item{
			Label:  slot.Label,
			Hint:   "Green to edit the color. Left and right step through quick picks",
			Value:  func() string { c := cur(); return render.Hex(color.RGBA{c.R, c.G, c.B, 0xFF}) },
			Swatch: cur,
			Adjust: func(d int) { setSlotColor(a, slot, paletteColors[paletteStep(cur(), sign(d))].c) },
			Activate: func() {
				m.picker = newColorPicker(a, slot, m.items.Visible)
			},
		})
	}
	return items
}
