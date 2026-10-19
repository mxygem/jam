package app

import (
	"image/color"
	"strings"

	"github.com/mxygem/jam/internal/chart"
)

// openBandPicker is the song picker for a band: each player's part for
// this song (left/right changes the instrument, green the difficulty),
// then Play.
func (s *SongSelect) openBandPicker(a *App, song *chart.Song) {
	in := a.SongInfo(song)
	if in == nil || in.Err != "" || len(in.Parts) == 0 {
		a.Toast("This chart has no playable parts")
		return
	}
	var parts []chart.Instrument
	for _, p := range in.Parts {
		parts = append(parts, p.Inst)
	}
	var items []Item
	for _, m := range a.Band.Members {
		m := m
		items = append(items, Item{
			Label:  m.prof.Name,
			Swatch: func() color.RGBA { return profileColor(m.prof) },
			Hint:   "Left/right: instrument     Green / Enter: difficulty",
			Value: func() string {
				p := in.Part(m.inst)
				if p == nil {
					return m.inst.Title() + " (no part: sits out)"
				}
				d := m.diff
				has := false
				for _, x := range p.Diffs {
					has = has || x == d
				}
				if !has {
					return m.partLabel() + " (" + closestDiff(p.Diffs, d).Title() + ")"
				}
				return m.partLabel()
			},
			Adjust:   func(d int) { m.inst = cyclePart(m.inst, d, parts) },
			Activate: func() { m.diff = cycleDiff(m.diff, -1) },
		})
	}
	items = append(items,
		Item{Label: "Play", Activate: func() {
			for _, m := range a.Band.Members {
				m.remember(a)
			}
			a.Settings.LastSong = song.Dir
			_ = a.Settings.Save()
			a.Run = nil
			a.SetScene(NewBandLoading(a, a.Band, song))
		}},
		Item{Label: "Back", Activate: func() { s.ov = ovNone }},
	)
	s.menu = List{Visible: min(len(items), 10), Items: items}
	s.menu.Sel = len(a.Band.Members)
	s.ov = ovPicker
}

func closestDiff(ds []chart.Difficulty, want chart.Difficulty) chart.Difficulty {
	if len(ds) == 0 {
		return want
	}
	best := ds[0]
	for _, d := range ds {
		if absInt(int(d)-int(want)) < absInt(int(best)-int(want)) {
			best = d
		}
	}
	return best
}

// bandLine is the song list's reminder of who's playing.
func bandLine(a *App) string {
	if a.Band == nil || len(a.Band.Members) == 0 {
		return ""
	}
	var names []string
	for _, m := range a.Band.Members {
		names = append(names, m.prof.Name+" ("+m.inst.Title()+")")
	}
	return bandModeLabel(a.Band.Mode) + ": " + strings.Join(names, ", ")
}
