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
	a.Band = nil // back at the title screen: multiplayer is over
	m.list.Items = []Item{
		{Label: "Play", Hint: "Pick a song from your library", Activate: func() { a.playlistFromMenu = false; a.SetScene(NewSongSelect(a)) }},
		{Label: "Multiplayer", Hint: "Up to 8 players: band, head to head, or party mode where anyone drops in and out", Activate: func() { a.SetScene(NewLobby(a)) }},
		{Label: "Playlists", Hint: "Build, save and play lists of songs", Activate: func() { a.playlistFromMenu = true; a.SetScene(NewPlaylistScene(a)) }},
		{Label: "Past runs", Hint: "Review recorded playthroughs: timing, misses, sections", Activate: func() { a.SetScene(NewRuns(a)) }},
		{Label: "Profiles", Hint: "Who's playing: each profile has its own settings, controller setup, scores and runs", Activate: func() { a.SetScene(NewProfileScene(a)) }},
		{Label: "Settings", Hint: "Gameplay, highway, audio and latency, controller and drum setup, song folders", Activate: func() { a.SetScene(NewSettings(a)) }},
		{Label: "Quit", Activate: func() { a.Quit() }},
	}
	m.list.Visible = len(m.list.Items)
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

	m.list.Draw(dst, 120, 370, 560, 70)
	drawWrapped(dst, m.list.Hint(), render.UI, 26, 120, 370+float64(len(m.list.Items))*70+16, 900, th.TextDim)

	// who's playing
	if p := a.Active; p != nil {
		w, _ := render.Measure(p.Name, render.UIBold, 30)
		x := 520.0
		render.RoundRect(dst, x, 292, w+72, 44, 22, render.WithAlpha(th.Panel, 0.85))
		b := render.Batch{}
		b.Ellipse(dst, x+26, 314, 10, 10, profileColor(p), profileColor(p))
		b.Flush(dst)
		render.Text(dst, p.Name, render.UIBold, 30, x+48, 296, th.Text, render.Left)
	}

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
