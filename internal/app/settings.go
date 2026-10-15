package app

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
)

// Settings edits preferences.
type Settings struct {
	list    List
	typing  bool
	path    string
	preview *render.Highway
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// NewSettings opens the settings screen.
func NewSettings(a *App) *Settings {
	s := &Settings{}
	st := a.Settings
	hw := &st.Highway
	clampf := func(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
	s.list.Items = []Item{
		{Label: "Note speed", Hint: "How many seconds of notes are visible (lower = faster)",
			Value:  func() string { return fmt.Sprintf("%.2f s", hw.Lookahead) },
			Adjust: func(d int) { hw.Lookahead = clampf(hw.Lookahead-float64(d)*0.05, 0.4, 2.5) }},
		{Label: "Highway width", Value: func() string { return fmt.Sprintf("%.0f px", hw.NearWidth) },
			Adjust: func(d int) { hw.NearWidth = clampf(hw.NearWidth+float64(d)*20, 400, 1100) }},
		{Label: "Highway angle", Hint: "Lower is steeper (more perspective)", Value: func() string { return fmt.Sprintf("%.2f", hw.FarRatio) },
			Adjust: func(d int) { hw.FarRatio = clampf(hw.FarRatio+float64(d)*0.02, 0.12, 0.8) }},
		{Label: "Highway length", Hint: "Where the far end of the highway sits", Value: func() string { return fmt.Sprintf("%.0f", hw.TopY) },
			Adjust: func(d int) { hw.TopY = clampf(hw.TopY-float64(d)*20, 0, 600) }},
		{Label: "Hit window", Hint: "Early and late window around each note", Value: func() string {
			return fmt.Sprintf("±%.0f ms", st.Engine.EarlyWindow*1000)
		}, Adjust: func(d int) {
			v := clampf(st.Engine.EarlyWindow+float64(d)*0.005, 0.03, 0.15)
			st.Engine.EarlyWindow, st.Engine.LateWindow = v, v
		}},
		{Label: "Drum hit window", Hint: "Early and late window for drum notes", Value: func() string {
			e, _ := st.Engine.Windows(true)
			return fmt.Sprintf("±%.0f ms", e*1000)
		}, Adjust: func(d int) {
			e, _ := st.Engine.Windows(true)
			st.Engine.DrumWindow = clampf(e+float64(d)*0.005, 0.03, 0.15)
		}},
		{Label: "Infinite front end", Hint: "Strum the next note as early as you like once the previous one has passed",
			Value: func() string { return onOff(st.Engine.InfiniteFrontEnd) }, Adjust: func(int) { st.Engine.InfiniteFrontEnd = !st.Engine.InfiniteFrontEnd }},
		{Label: "HOPO strum leniency", Hint: "A strum this soon after hammering a HOPO is ignored (it also lasts to the end of the note's window)",
			Value:  func() string { return fmt.Sprintf("%.0f ms", st.Engine.StrumLenience*1000) },
			Adjust: func(d int) { st.Engine.StrumLenience = clampf(st.Engine.StrumLenience+float64(d)*0.005, 0, 0.15) }},
		{Label: "Chord leniency", Hint: "How long a strum waits for the frets to finish landing",
			Value:  func() string { return fmt.Sprintf("%.0f ms", st.Engine.ChordLenience*1000) },
			Adjust: func(d int) { st.Engine.ChordLenience = clampf(st.Engine.ChordLenience+float64(d)*0.005, 0.005, 0.12) }},
		{Label: "No fail", Value: func() string { return onOff(st.Engine.NoFail) }, Adjust: func(int) { st.Engine.NoFail = !st.Engine.NoFail }},
		{Label: "Show hit timing", Hint: "Show +/- ms under the strike line on every hit", Value: func() string { return onOff(st.ShowTiming) }, Adjust: func(int) { st.ShowTiming = !st.ShowTiming }},
		{Label: "Mute stem on miss", Value: func() string { return onOff(st.MuteOnMiss) }, Adjust: func(int) { st.MuteOnMiss = !st.MuteOnMiss }},
		{Label: "Pro drums (cymbals)", Value: func() string { return onOff(st.Engine.ProDrums) }, Adjust: func(int) { st.Engine.ProDrums = !st.Engine.ProDrums }},
		{Label: "Expert+ double kick", Value: func() string { return onOff(st.Engine.ExpertPlus) }, Adjust: func(int) { st.Engine.ExpertPlus = !st.Engine.ExpertPlus }},
		{Label: "Volume", Value: func() string { return fmt.Sprintf("%.0f%%", st.Volume*100) }, Adjust: func(d int) { st.Volume = clampf(st.Volume+float64(d)*0.05, 0, 1) }},
		{Label: "Audio offset", Hint: "Use Calibrate latency to measure this", Value: func() string { return fmt.Sprintf("%.0f ms", st.AudioOffsetMs) },
			Adjust: func(d int) { st.AudioOffsetMs += float64(d) }},
		{Label: "Video offset", Value: func() string { return fmt.Sprintf("%.0f ms", st.VideoOffsetMs) }, Adjust: func(d int) { st.VideoOffsetMs += float64(d) }},
		{Label: "VSync", Value: func() string { return onOff(st.VSync) }, Adjust: func(int) { st.VSync = !st.VSync; ebiten.SetVsyncEnabled(st.VSync) }},
		{Label: "Reset highway shape", Activate: func() { st.Highway = render.DefaultHighway() }},
		{Label: "Add a song folder...", Hint: "Type or paste a folder path, then Enter", Activate: func() { s.typing = true; s.path = "" }},
		{Label: "Rescan library", Activate: func() { a.RescanLibrary(); a.Toast("Rescanning library...") }},
		{Label: "Back", Activate: func() { s.save(a); a.SetScene(NewMainMenu(a)) }},
	}
	s.list.Visible = 11
	s.preview = &render.Highway{}
	return s
}

func (s *Settings) save(a *App) {
	if err := a.Settings.Save(); err != nil {
		a.Toast("Couldn't save settings: " + err.Error())
	}
}

func (s *Settings) Update(a *App) error {
	if s.typing {
		for _, r := range a.Chars {
			s.path += string(r)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(s.path) > 0 {
			rs := []rune(s.path)
			s.path = string(rs[:len(rs)-1])
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			s.typing = false
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			p := strings.Trim(strings.TrimSpace(s.path), "\"")
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				a.Settings.LibraryPaths = append(a.Settings.LibraryPaths, p)
				s.save(a)
				a.RescanLibrary()
				a.Toast("Added " + p)
			} else {
				a.Toast("Not a folder: " + p)
			}
			s.typing = false
		}
		return nil
	}
	s.list.Update(a)
	if a.Pressed(action.MenuBack) {
		s.save(a)
		a.SetScene(NewMainMenu(a))
	}
	return nil
}

func (s *Settings) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	Header(dst, "Settings", "Changes apply to the next song")
	s.list.Draw(dst, 120, 250, 760, 66)
	render.Text(dst, s.list.Hint(), render.UI, 26, 120, 250+11*66+16, th.TextDim, render.Left)

	// live highway preview
	px, py, pw, ph := 1000.0, 230.0, 840.0, 560.0
	Panel(dst, px-20, py-20, pw+40, ph+260)
	cfg := a.Settings.Highway
	k := pw / render.ScreenW
	s.preview.Cfg = render.HighwayConfig{
		CenterX: px + pw/2, StrikeY: py + cfg.StrikeY*k*1.0, TopY: py + cfg.TopY*k,
		NearWidth: cfg.NearWidth * k, FarRatio: cfg.FarRatio, Lookahead: cfg.Lookahead,
	}
	var b render.Batch
	h := s.preview
	for z := 0.0; z < 1; z += 0.1 {
		y0, y1 := h.Y(z), h.Y(z+0.1)
		l0, r0 := h.X(0, 5, z), h.X(5, 5, z)
		l1, r1 := h.X(0, 5, z+0.1), h.X(5, 5, z+0.1)
		c := render.WithAlpha(th.Highway, 1-z*0.6)
		b.Quad(dst, l0, y0, r0, y0, r1, y1, l1, y1, c, c, c, c)
	}
	for l := 0; l < 5; l++ {
		for _, z := range []float64{0.2, 0.55} {
			x := h.X(float64(l)+0.5, 5, z)
			sc := h.Scale(z)
			b.Ellipse(dst, x, h.Y(z), 30*sc*k*2.4, 16*sc*k*2.4, render.Lighten(th.Lanes[l], 0.3), th.Lanes[l])
		}
	}
	b.Flush(dst)
	render.Text(dst, "Highway preview", render.Cond, 24, px, py+ph+10, th.TextDim, render.Left)
	y := py + ph + 60
	render.Text(dst, "Song folders:", render.CondBold, 26, px, y, th.TextDim, render.Left)
	for i, p := range a.Settings.LibraryPaths {
		if i >= 4 {
			render.Text(dst, fmt.Sprintf("...and %d more", len(a.Settings.LibraryPaths)-4), render.Cond, 22, px, y+40+float64(i)*32, th.TextDim, render.Left)
			break
		}
		render.Text(dst, truncateW(p, render.Cond, 22, pw), render.Cond, 22, px, y+40+float64(i)*32, th.Text, render.Left)
	}
	if s.typing {
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xA0))
		Panel(dst, 260, 420, 1400, 200)
		render.Text(dst, "Folder path:", render.UIBold, 32, 300, 450, th.Text, render.Left)
		render.Text(dst, s.path+"_", render.Mono, 26, 300, 510, th.Text, render.Left)
		render.Text(dst, "Enter to add, Esc to cancel. Clone Hero song folders were imported on first run. Settings file: "+config.SettingsPath(), render.Cond, 22, 300, 570, th.TextDim, render.Left)
	}
	Footer(dst, "Left/right change     Esc save and back")
}
