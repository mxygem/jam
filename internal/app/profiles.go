package app

import (
	"image/color"
	"log"
	"os"
	"path/filepath"

	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// Profiles: every player has one. The active profile is the one using the
// menus. Its gameplay settings and calibration live in a.Settings (and so
// in settings.json) while it's active, its bindings are a.Bindings and its
// scores a.Scores. Settings saves copy those back into the profile file.

// initProfiles loads the profiles, making the first one out of the
// existing settings, and makes the saved active profile current.
func (a *App) initProfiles() {
	a.scoresBy = map[string]*replay.Scores{}
	a.bindsBy = map[string]*input.Bindings{}
	st, errs := profile.Load(config.ProfilesDir())
	for _, err := range errs {
		log.Printf("profiles: %v", err)
	}
	a.Profiles = st
	if len(st.All()) == 0 {
		p, err := st.Migrate(a.Settings)
		if err != nil {
			log.Printf("profiles: couldn't save the first profile: %v", err)
		}
		log.Printf("profiles: made %q from the existing settings", p.Name)
		a.Settings.ActiveProfile = p.ID
	}
	p := st.Get(a.Settings.ActiveProfile)
	if p == nil {
		p = st.All()[0]
	}
	a.Settings.OnSave = func(s *config.Settings) {
		if a.Active == nil {
			return
		}
		a.Active.Player = s.Player()
		if err := a.Profiles.Save(a.Active); err != nil {
			log.Printf("profiles: save %s: %v", a.Active.Name, err)
		}
	}
	a.UseProfile(p)
}

// UseProfile makes p the active profile.
func (a *App) UseProfile(p *profile.Profile) {
	if a.Active == p {
		return
	}
	if a.Active != nil {
		a.Active.Player = a.Settings.Player()
		_ = a.Profiles.Save(a.Active)
	}
	first := a.Active == nil
	if a.Active != nil && a.Bindings != nil {
		a.bindsBy[a.Active.ID] = a.Bindings
	}
	a.Bindings = nil
	a.Active = p
	if !(first && p.Legacy) {
		// settings.json already holds the legacy profile's settings at
		// startup (and may be newer than its profile file)
		a.Settings.SetPlayer(p.Player)
	}
	a.Settings.ActiveProfile = p.ID
	a.Bindings = a.BindingsFor(p)
	a.Scores = a.ScoresFor(p)
	if a.Input != nil {
		a.Menu = input.NewMenuInput(a.Bindings, a.Input.Devices())
	}
	if !first {
		if err := a.Settings.Save(); err != nil {
			a.Toast("Couldn't save settings: " + err.Error())
		}
		log.Printf("profiles: playing as %s", p.Name)
	}
}

// ScoresFor returns a profile's best scores.
func (a *App) ScoresFor(p *profile.Profile) *replay.Scores {
	if p == nil {
		return replay.LoadScores(filepath.Join(config.Dir(), "scores.json"), config.RunsDir(), nil)
	}
	if s := a.scoresBy[p.ID]; s != nil {
		return s
	}
	s := replay.LoadScores(p.ScoresPath(), config.RunsDir(), p.Owns)
	a.scoresBy[p.ID] = s
	return s
}

// BindingsFor returns a profile's controller bindings.
func (a *App) BindingsFor(p *profile.Profile) *input.Bindings {
	if p == a.Active && a.Bindings != nil {
		return a.Bindings
	}
	if b := a.bindsBy[p.ID]; b != nil {
		return b
	}
	b, err := input.LoadBindings(p.BindingsPath())
	if err != nil {
		log.Printf("bindings for %s: %v", p.Name, err)
	}
	a.bindsBy[p.ID] = b
	return b
}

// SettingsFor returns the settings a profile plays with: the shared
// settings with the profile's own gameplay settings and calibration.
func (a *App) SettingsFor(p *profile.Profile) *config.Settings {
	if p == nil || p == a.Active {
		return a.Settings.ForPlayer(a.Settings.Player())
	}
	return a.Settings.ForPlayer(p.Player)
}

// NewProfile makes a profile that starts from the active profile's
// settings and controller bindings.
func (a *App) NewProfile(name string) *profile.Profile {
	p, err := a.Profiles.Create(name, a.Settings.Player())
	if err != nil {
		a.Toast("Couldn't save the profile: " + err.Error())
	}
	if a.Bindings != nil {
		if data, err := os.ReadFile(a.activeBindingsPath()); err == nil {
			_ = os.WriteFile(p.BindingsPath(), data, 0o644)
		} else if err := a.Bindings.Save(p.BindingsPath()); err != nil {
			log.Printf("bindings for %s: %v", p.Name, err)
		}
	}
	log.Printf("profiles: new profile %s", p.Name)
	return p
}

func (a *App) activeBindingsPath() string {
	if a.Active != nil {
		return a.Active.BindingsPath()
	}
	return config.BindingsPath()
}

// SaveProfile writes a profile (after a rename or color change).
func (a *App) SaveProfile(p *profile.Profile) {
	if p == a.Active {
		p.Player = a.Settings.Player()
	}
	if err := a.Profiles.Save(p); err != nil {
		a.Toast("Couldn't save the profile: " + err.Error())
	}
}

// DeleteProfile removes a profile (not the active one).
func (a *App) DeleteProfile(p *profile.Profile) bool {
	if p == a.Active {
		a.Toast("Switch to another profile before deleting this one")
		return false
	}
	if err := a.Profiles.Delete(p); err != nil {
		a.Toast(err.Error())
		return false
	}
	delete(a.scoresBy, p.ID)
	delete(a.bindsBy, p.ID)
	a.Toast("Deleted " + p.Name + " (their recordings stay in Past runs)")
	return true
}

// profileColor is a profile's color for drawing.
func profileColor(p *profile.Profile) color.RGBA {
	if p != nil {
		if c, ok := render.ParseHex(p.Color); ok {
			return c
		}
	}
	return th.Accent
}
