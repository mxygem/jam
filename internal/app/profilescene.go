package app

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

// ProfileScene lists the player profiles: switch who's playing, make new
// ones, rename, recolor, delete.
type ProfileScene struct {
	list    List
	sel     *profile.Profile // the profile whose options are open
	opts    List
	typing  *TextInput
	onTyped func(string)
	confirm bool // asking before a delete
	back    func(a *App)
}

// NewProfileScene opens the profiles screen.
func NewProfileScene(a *App) *ProfileScene {
	s := &ProfileScene{back: func(a *App) { a.SetScene(NewMainMenu(a)) }}
	s.build(a)
	return s
}

func (s *ProfileScene) build(a *App) {
	keep := s.list.Sel
	s.list = List{Visible: 9}
	for _, p := range a.Profiles.All() {
		p := p
		s.list.Items = append(s.list.Items, Item{
			Label: p.Name,
			Value: func() string {
				if p == a.Active {
					return "Playing now"
				}
				return ""
			},
			Swatch:   func() color.RGBA { return profileColor(p) },
			Hint:     profileHint(a, p),
			Activate: func() { s.open(a, p) },
		})
	}
	s.list.Items = append(s.list.Items, Item{Label: "New profile...", Hint: "Starts with your current settings and controller setup. Type a name, or Enter for \"Player N\"",
		Activate: func() {
			s.startTyping("", func(name string) {
				p := a.NewProfile(name)
				s.build(a)
				s.list.Sel = a.Profiles.Index(p)
				s.open(a, p)
			})
		}})
	s.list.Sel = min(keep, len(s.list.Items)-1)
}

func profileHint(a *App, p *profile.Profile) string {
	sc := a.ScoresFor(p)
	n := 0
	if sc != nil {
		n = sc.Count()
	}
	hint := fmt.Sprintf("%s with a best score", plural(n, "part"))
	if p.Legacy {
		hint += ". The first profile: it has every run from before profiles"
	}
	return hint
}

func (s *ProfileScene) startTyping(value string, done func(string)) {
	s.typing = &TextInput{Value: value, Max: profile.MaxName, Placeholder: "Name"}
	s.onTyped = done
}

func (s *ProfileScene) open(a *App, p *profile.Profile) {
	s.sel = p
	s.confirm = false
	s.opts = List{Visible: 6, Items: []Item{
		{Label: "Play as " + p.Name, Hint: "Menus, settings, scores and runs become " + p.Name + "'s", Disabled: p == a.Active,
			Activate: func() {
				a.UseProfile(p)
				a.Toast("Playing as " + p.Name)
				s.sel = nil
				s.build(a)
			}},
		{Label: "Rename", Hint: "Up to 20 letters", Activate: func() {
			s.startTyping(p.Name, func(name string) {
				p.Name = a.Profiles.UniqueName(profile.CleanName(name), p)
				a.SaveProfile(p)
				s.build(a)
				s.open(a, p)
			})
		}},
		{Label: "Color", Hint: "Shown on your highway, in the lobby and in results",
			Value:  func() string { return "" },
			Swatch: func() color.RGBA { return profileColor(p) },
			Adjust: func(d int) {
				p.Color = cycleColor(p.Color, d)
				a.SaveProfile(p)
			}},
		{Label: "Delete", Hint: "Removes the profile, its settings, bindings and best scores. Its recordings stay in Past runs",
			Disabled: p.Legacy || p == a.Active,
			Activate: func() { s.confirm = true }},
		{Label: "Back", Activate: func() { s.sel = nil }},
	}}
	if p == a.Active {
		s.opts.Sel = 1
	}
}

func cycleColor(cur string, d int) string {
	i := 0
	for j, c := range profile.Palette {
		if strings.EqualFold(c, cur) {
			i = j
		}
	}
	n := len(profile.Palette)
	return profile.Palette[((i+sign(d))%n+n)%n]
}

func (s *ProfileScene) Update(a *App) error {
	if s.typing != nil {
		done, cancel := s.typing.Update(a)
		switch {
		case cancel:
			s.typing = nil
		case done:
			v := s.typing.Value
			s.typing = nil
			s.onTyped(v)
		}
		return nil
	}
	if s.confirm {
		switch {
		case a.Pressed(action.MenuConfirm):
			if a.DeleteProfile(s.sel) {
				s.sel = nil
				s.build(a)
			}
			s.confirm = false
		case a.Pressed(action.MenuBack):
			s.confirm = false
		}
		return nil
	}
	if s.sel != nil {
		if s.opts.Update(a) {
			return nil
		}
		if a.Pressed(action.MenuBack) || a.Mouse.Right {
			s.sel = nil
		}
		return nil
	}
	if s.list.Update(a) {
		return nil
	}
	if a.Pressed(action.MenuBack) || a.Mouse.Right {
		s.back(a)
	}
	return nil
}

func (s *ProfileScene) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	sub := "Playing as " + a.Active.Name + ". Each profile has its own settings, calibration, controller setup, scores and runs"
	Header(dst, "Profiles", sub)
	s.list.Draw(dst, 120, 250, 900, 76)
	drawWrapped(dst, s.list.Hint(), render.UI, 26, 120, 250+float64(min(len(s.list.Items), 9))*76+24, 900, th.TextDim)

	if s.sel != nil {
		p := s.sel
		x, y, w := 1100.0, 250.0, 700.0
		Panel(dst, x-30, y-30, w+60, 620)
		render.FillRect(dst, x-30, y-30, 12, 620, profileColor(p))
		render.Text(dst, p.Name, render.Display, 56, x, y-10, th.Text, render.Left)
		s.opts.Draw(dst, x, y+90, w, 70)
		drawWrapped(dst, s.opts.Hint(), render.UI, 24, x, y+90+5*70+20, w, th.TextDim)
	}
	if s.confirm {
		render.Dim(dst, render.RGBA(0, 0, 0, 0xB8))
		Panel(dst, 460, 400, 1000, 220)
		render.Text(dst, "Delete "+s.sel.Name+"?", render.UIBold, 40, 500, 430, th.Text, render.Left)
		render.Text(dst, "Enter / green to delete, Esc / red to keep", render.UI, 28, 500, 510, th.TextDim, render.Left)
	}
	if s.typing != nil {
		render.Dim(dst, render.RGBA(0, 0, 0, 0xB8))
		Panel(dst, 460, 400, 1000, 240)
		render.Text(dst, "Profile name", render.UIBold, 36, 500, 430, th.Text, render.Left)
		s.typing.Draw(dst, 500, 500, 920)
		render.Text(dst, "Enter to save, Esc to cancel", render.Cond, 24, 500, 580, th.TextDim, render.Left)
	}
	Footer(dst, "Enter / green open     Left/right change color     Esc / red back")
}
