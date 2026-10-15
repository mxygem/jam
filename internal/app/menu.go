package app

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/render"
)

// MainMenu is the title screen.
type MainMenu struct {
	list List
}

// NewMainMenu builds the title screen.
func NewMainMenu(a *App) *MainMenu {
	m := &MainMenu{}
	m.list.Items = []Item{
		{Label: "Play", Hint: "Pick a song from your library", Activate: func() { a.playlistFromMenu = false; a.SetScene(NewSongSelect(a)) }},
		{Label: "Playlists", Hint: "Build, save and play lists of songs", Activate: func() { a.playlistFromMenu = true; a.SetScene(NewPlaylistScene(a)) }},
		{Label: "Past runs", Hint: "Review recorded playthroughs: timing, misses, sections", Activate: func() { a.SetScene(NewRuns(a)) }},
		{Label: "Settings", Hint: "Gameplay, highway, audio and latency, controller and drum setup, song folders", Activate: func() { a.SetScene(NewSettings(a)) }},
		{Label: "Quit", Activate: func() { a.Quit() }},
	}
	m.list.Visible = 5
	return m
}

func (m *MainMenu) Update(a *App) error {
	m.list.Update(a)
	if a.Opts.Screenshot != "" && a.Opts.ShotScene == "menu" && a.frame > 30 {
		a.TakeScreenshotNext()
	}
	return nil
}

func (m *MainMenu) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	now := clock.Now()
	// logo
	pulse := 0.5 + 0.5*math.Sin(now*2)
	render.TextShadow(dst, "jam", render.Display, 190, 110, 70, th.Text, render.Left)
	render.FillRect(dst, 168, 318, 210, 8, render.Mix(th.Accent, th.StarPower, pulse*0.3))
	render.Text(dst, "v"+Version, render.Cond, 24, 390, 300, th.TextDim, render.Left)

	m.list.Draw(dst, 120, 380, 560, 76)
	render.Text(dst, m.list.Hint(), render.UI, 26, 120, 380+5*76+20, th.TextDim, render.Left)

	// devices panel
	px, py, pw := 1180.0, 380.0, 640.0
	devs := a.Input.Devices()
	Panel(dst, px, py, pw, 90+float64(len(devs))*52)
	render.Text(dst, "CONNECTED", render.CondBold, 26, px+32, py+24, th.TextDim, render.Left)
	for i, d := range devs {
		y := py + 70 + float64(i)*52
		render.Text(dst, d.Name, render.UI, 28, px+32, y, th.Text, render.Left)
		if d.Detail != "" {
			render.Text(dst, d.Detail, render.Cond, 22, px+pw-32, y+4, th.TextDim, render.Right)
		}
	}
	songs, scanning := a.Library()
	status := plural(len(songs), "song") + " in library"
	if scanning {
		status = "Scanning library..."
	}
	render.Text(dst, status, render.Cond, 26, px+32, py+90+float64(len(devs))*52+20, th.TextDim, render.Left)
	if a.FFmpeg == "" {
		render.Text(dst, "ffmpeg not found: .opus/.flac songs won't load", render.Cond, 24, px+32, py+90+float64(len(devs))*52+56, th.Late, render.Left)
	}
	Footer(dst, "Arrows / strum to move     Enter / green to select     F11 fullscreen")
}
