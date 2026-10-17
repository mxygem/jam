package app

import (
	"strings"

	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
)

// Themes: the built-in ones, theme files in the themes folder and the
// player's custom look. Settings.Look.Theme names the one in use.

const (
	themeCustom   = "custom"
	themeFilePref = "file:"
)

type themeEntry struct {
	id, label string
	spec      render.ThemeSpec
}

// ThemesDir is where theme files live.
func ThemesDir() string { return config.ThemesDir() }

// themeEntries lists the themes to pick from, in order.
func themeEntries(a *App) []themeEntry {
	var out []themeEntry
	for _, s := range render.BuiltinThemes {
		out = append(out, themeEntry{s.Name, s.Name, s})
	}
	files, _ := render.LoadThemeFiles(ThemesDir())
	for _, s := range files {
		out = append(out, themeEntry{themeFilePref + s.Name, s.Name + " (file)", s})
	}
	if c := a.Settings.Look.Custom; c != nil {
		out = append(out, themeEntry{themeCustom, customLabel(c), *c})
	}
	return out
}

func customLabel(c *render.ThemeSpec) string {
	if c.Base != "" {
		return "Custom (from " + c.Base + ")"
	}
	return "Custom"
}

// themeIndex finds the theme in use in the list (-1 if missing).
func themeIndex(list []themeEntry, id string) int {
	if id == "" {
		id = render.BuiltinThemes[0].Name
	}
	for i, e := range list {
		if strings.EqualFold(e.id, id) {
			return i
		}
	}
	return -1
}

// currentTheme is the theme in use (Jam if it's gone missing).
func currentTheme(a *App) themeEntry {
	list := themeEntries(a)
	if i := themeIndex(list, a.Settings.Look.Theme); i >= 0 {
		return list[i]
	}
	return list[0]
}

// applyLook makes the chosen theme and note size the look in use.
func applyLook(a *App) {
	e := currentTheme(a)
	e.spec.Apply(a.Settings.Look.NoteSize)
}

// cycleTheme moves to the next or previous theme.
func cycleTheme(a *App, d int) {
	list := themeEntries(a)
	i := themeIndex(list, a.Settings.Look.Theme)
	if i < 0 {
		i = 0
	}
	n := len(list)
	a.Settings.Look.Theme = list[((i+d)%n+n)%n].id
	applyLook(a)
}

// editLook changes the look. A built-in or file theme becomes the start of
// a custom look first (replacing an earlier custom look).
func editLook(a *App, fn func(t *render.Theme, l *render.Look)) {
	lk := &a.Settings.Look
	if lk.Theme != themeCustom || lk.Custom == nil {
		base := currentTheme(a)
		if lk.Custom != nil {
			a.Toast("Started a new custom look from " + base.spec.Name)
		}
		spec := render.CurrentSpec("Custom")
		spec.Base = base.spec.Name
		lk.Custom = &spec
		lk.Theme = themeCustom
	}
	t, l := render.DefaultTheme, render.CurrentLook
	fn(&t, &l)
	render.DefaultTheme = t
	render.SetLook(l)
	render.ResetSprites()
	spec := render.CaptureSpec(lk.Custom.Name, &t, l)
	spec.Base = lk.Custom.Base
	lk.Custom = &spec
}

// baseColor is a color slot's value in the theme a custom look started
// from (or the theme in use).
func baseColor(a *App, slot render.ColorSlot) (render.ThemeSpec, bool) {
	name := ""
	if c := a.Settings.Look.Custom; a.Settings.Look.Theme == themeCustom && c != nil {
		name = c.Base
	} else {
		name = currentTheme(a).spec.Name
	}
	for _, e := range themeEntries(a) {
		if e.id != themeCustom && e.spec.Name == name {
			return e.spec, true
		}
	}
	return render.ThemeSpec{}, false
}

// saveThemeFile writes the look in use to the themes folder and switches
// to the file.
func saveThemeFile(a *App, name string) {
	spec := render.CurrentSpec(name)
	path, err := render.SaveThemeFile(ThemesDir(), name, spec)
	if err != nil {
		a.Toast("Couldn't save the theme: " + err.Error())
		return
	}
	a.Settings.Look.Theme = themeFilePref + render.SafeFileName(name)
	a.Settings.Look.Custom = nil
	applyLook(a)
	if err := a.Settings.Save(); err != nil {
		a.Toast("Couldn't save settings: " + err.Error())
		return
	}
	a.Toast("Saved " + path)
}
