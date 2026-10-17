package app

import (
	"fmt"
	"image"
	"math"
	"os"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
)

// Settings edits preferences, grouped into pages.
type Settings struct {
	menu    *settingsMenu
	preview *render.Highway
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// NewSettings opens the settings screen at its page list.
func NewSettings(a *App) *Settings { return NewSettingsAt(a, "") }

// NewSettingsAt opens the settings screen on a page (see page* constants).
func NewSettingsAt(a *App, page string) *Settings {
	s := &Settings{preview: &render.Highway{}}
	s.menu = newSettingsMenu(a, false)
	s.menu.cats.Visible = 10
	if page != "" {
		s.menu.open(page)
	}
	return s
}

func (s *Settings) save(a *App) {
	if err := a.Settings.Save(); err != nil {
		a.Toast("Couldn't save settings: " + err.Error())
	}
}

// addSongFolder adds a library folder typed by the player.
func addSongFolder(a *App, path string) {
	p := strings.Trim(strings.TrimSpace(path), "\"")
	if p == "" {
		return
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		a.Settings.LibraryPaths = append(a.Settings.LibraryPaths, p)
		if err := a.Settings.Save(); err != nil {
			a.Toast("Couldn't save settings: " + err.Error())
		}
		a.RescanLibrary()
		a.Toast("Added " + p)
	} else {
		a.Toast("Not a folder: " + p)
	}
}

func (s *Settings) Update(a *App) error {
	if s.menu.update(a) {
		s.save(a)
		a.SetScene(NewMainMenu(a))
	}
	return nil
}

func (s *Settings) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	sub := "Changes apply to the next song"
	if s.menu.page >= 0 {
		sub = "Settings"
	}
	Header(dst, s.menu.title(), sub)
	s.menu.draw(dst, 120, 250, 760, 66)
	drawWrapped(dst, s.menu.hint(), render.UI, 26, 120, 250+float64(s.menu.rows())*66+24, 780, th.TextDim)
	switch s.menu.pageID() {
	case pageHighway:
		s.drawPreview(dst, a)
	case pageBackground:
		s.drawBackgroundPreview(dst, a)
	case pageLibrary, "":
		s.drawFolders(dst, a, 1000, 250)
	}
	s.menu.drawTyping(dst)
	Footer(dst, "Left/right change     Enter / green open     Esc / red back")
}

func (s *Settings) drawPreview(dst *ebiten.Image, a *App) {
	// live highway preview
	px, py, pw, ph := 1000.0, 230.0, 840.0, 560.0
	Panel(dst, px-20, py-20, pw+40, ph+70)
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
}

// drawBackgroundPreview shows the chosen background in the shape of the
// screen, darkened as it will be in game.
func (s *Settings) drawBackgroundPreview(dst *ebiten.Image, a *App) {
	px, py, pw := 1000.0, 250.0, 840.0
	vw, vh := float64(max(a.viewW, render.ScreenW)), float64(max(a.viewH, render.ScreenH))
	ph := math.Min(pw*vh/vw, 472)
	Panel(dst, px-20, py-20, pw+40, ph+150)
	render.FillRect(dst, px, py, pw, ph, render.RGBA(0x0a, 0x0a, 0x12, 0xFF))
	img := a.BG.image()
	set := a.Settings
	label := bgLabel(set.Background)
	if img != nil {
		iw, ih := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
		k := math.Max(pw/iw, ph/ih)
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Scale(k, k)
		op.GeoM.Translate(px+(pw-iw*k)/2, py+(ph-ih*k)/2)
		box := dst.SubImage(image.Rect(int(px), int(py), int(px+pw), int(py+ph))).(*ebiten.Image)
		box.DrawImage(img, op)
		render.FillRect(dst, px, py, pw, ph, render.RGBA(0, 0, 0, uint8(255*set.BackgroundDim)))
	} else if set.Background != bgDefault {
		label += "  (loading...)"
	}
	n := len(a.BG.List())
	render.Text(dst, truncateW(label, render.UIBold, 28, pw), render.UIBold, 28, px, py+ph+20, th.Text, render.Left)
	render.Text(dst, fmt.Sprintf("%s in %s", plural(n, "image"), CustomsDir()), render.Cond, 22, px, py+ph+66, th.TextDim, render.Left)
}

func (s *Settings) drawFolders(dst *ebiten.Image, a *App, px, y float64) {
	pw := 840.0
	Panel(dst, px-20, y-20, pw+40, 120+float64(min(5, len(a.Settings.LibraryPaths)))*32)
	render.Text(dst, "Song folders:", render.CondBold, 26, px, y, th.TextDim, render.Left)
	for i, p := range a.Settings.LibraryPaths {
		if i >= 4 {
			render.Text(dst, fmt.Sprintf("...and %d more", len(a.Settings.LibraryPaths)-4), render.Cond, 22, px, y+40+float64(i)*32, th.TextDim, render.Left)
			break
		}
		render.Text(dst, truncateW(p, render.Cond, 22, pw), render.Cond, 22, px, y+40+float64(i)*32, th.Text, render.Left)
	}
	n := float64(min(5, len(a.Settings.LibraryPaths)))
	render.Text(dst, truncateW("Settings file: "+config.SettingsPath(), render.Cond, 20, pw), render.Cond, 20, px, y+50+n*32, th.TextDim, render.Left)
}
