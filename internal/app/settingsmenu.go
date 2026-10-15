package app

import (
	"fmt"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/render"
)

// Settings are grouped into pages. The same pages are used by the Settings
// screen and the pause menu; pages and items that can't be used mid-song
// are left out there.

const (
	pageGameplay = "gameplay"
	pageHighway  = "highway"
	pageAudio    = "audio"
	pageControls = "controls"
	pageLibrary  = "library"
)

type settingsPage struct {
	id, title, hint string
	items           []Item
}

// settingsMenu is a two-level menu: pages, then the settings on a page.
type settingsMenu struct {
	inSong   bool
	pages    []settingsPage
	cats     List
	page     int // -1 while choosing a page
	items    List
	onChange func() // called after any setting changes
	typing   *TextInput
}

func clampf(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func newSettingsMenu(a *App, inSong bool) *settingsMenu {
	m := &settingsMenu{inSong: inSong, page: -1}
	st := a.Settings
	hw := &st.Highway
	nextRun := ""
	if inSong {
		nextRun = " (applies when you restart)"
	}

	gameplay := []Item{
		{Label: "Hit window", Hint: "Early and late window around each note" + nextRun, Value: func() string {
			return fmt.Sprintf("±%.0f ms", st.Engine.EarlyWindow*1000)
		}, Adjust: func(d int) {
			v := clampf(st.Engine.EarlyWindow+float64(d)*0.005, 0.03, 0.15)
			st.Engine.EarlyWindow, st.Engine.LateWindow = v, v
		}},
		{Label: "Drum hit window", Hint: "Early and late window for drum notes" + nextRun, Value: func() string {
			e, _ := st.Engine.Windows(true)
			return fmt.Sprintf("±%.0f ms", e*1000)
		}, Adjust: func(d int) {
			e, _ := st.Engine.Windows(true)
			st.Engine.DrumWindow = clampf(e+float64(d)*0.005, 0.03, 0.15)
		}},
		{Label: "Infinite front end", Hint: "Strum the next note as early as you like once the previous one has passed" + nextRun,
			Value: func() string { return onOff(st.Engine.InfiniteFrontEnd) }, Adjust: func(int) { st.Engine.InfiniteFrontEnd = !st.Engine.InfiniteFrontEnd }},
		{Label: "HOPO strum leniency", Hint: "A strum this soon after hammering a HOPO is ignored" + nextRun,
			Value:  func() string { return fmt.Sprintf("%.0f ms", st.Engine.StrumLenience*1000) },
			Adjust: func(d int) { st.Engine.StrumLenience = clampf(st.Engine.StrumLenience+float64(d)*0.005, 0, 0.15) }},
		{Label: "Chord leniency", Hint: "How long a strum waits for the frets to finish landing" + nextRun,
			Value:  func() string { return fmt.Sprintf("%.0f ms", st.Engine.ChordLenience*1000) },
			Adjust: func(d int) { st.Engine.ChordLenience = clampf(st.Engine.ChordLenience+float64(d)*0.005, 0.005, 0.12) }},
		{Label: "No fail", Hint: "Keep playing when the rock meter runs out" + nextRun,
			Value: func() string { return onOff(st.Engine.NoFail) }, Adjust: func(int) { st.Engine.NoFail = !st.Engine.NoFail }},
		{Label: "Pro drums (cymbals)", Hint: "Tell cymbals and toms apart on drum charts that mark them" + nextRun,
			Value: func() string { return onOff(st.Engine.ProDrums) }, Adjust: func(int) { st.Engine.ProDrums = !st.Engine.ProDrums }},
		{Label: "Expert+ double kick", Hint: "Include the extra kick notes on Expert drums" + nextRun,
			Value: func() string { return onOff(st.Engine.ExpertPlus) }, Adjust: func(int) { st.Engine.ExpertPlus = !st.Engine.ExpertPlus }},
		{Label: "Count-in", Hint: "Time before the song starts" + nextRun,
			Value:  func() string { return fmt.Sprintf("%.1f s", st.LeadIn) },
			Adjust: func(d int) { st.LeadIn = clampf(st.LeadIn+float64(d)*0.5, 1, 6) }},
	}

	highway := []Item{
		{Label: "Note speed", Hint: "How many seconds of notes are visible (lower = faster)",
			Value:  func() string { return fmt.Sprintf("%.2f s", hw.Lookahead) },
			Adjust: func(d int) { hw.Lookahead = clampf(hw.Lookahead-float64(d)*0.05, 0.4, 2.5) }},
		{Label: "Highway width", Value: func() string { return fmt.Sprintf("%.0f px", hw.NearWidth) },
			Adjust: func(d int) { hw.NearWidth = clampf(hw.NearWidth+float64(d)*20, 400, 1100) }},
		{Label: "Highway angle", Hint: "Lower is steeper (more perspective)", Value: func() string { return fmt.Sprintf("%.2f", hw.FarRatio) },
			Adjust: func(d int) { hw.FarRatio = clampf(hw.FarRatio+float64(d)*0.02, 0.12, 0.8) }},
		{Label: "Highway length", Hint: "Where the far end of the highway sits", Value: func() string { return fmt.Sprintf("%.0f", hw.TopY) },
			Adjust: func(d int) { hw.TopY = clampf(hw.TopY-float64(d)*20, 0, 600) }},
		{Label: "Reset highway shape", Activate: func() { st.Highway = render.DefaultHighway() }},
		{Label: "Hit effects", Hint: "Flash, flames and sparks when you hit a note (the fret punch always shows)",
			Value: func() string {
				l := render.ParseFxLevel(st.HitFx).String()
				return strings.ToUpper(l[:1]) + l[1:]
			},
			Adjust: func(d int) {
				l := (int(render.ParseFxLevel(st.HitFx)) + d + 3) % 3
				st.HitFx = render.FxLevel(l).String()
			}},
		{Label: "Show hit timing", Hint: "Show +/- ms under the strike line on every hit",
			Value: func() string { return onOff(st.ShowTiming) }, Adjust: func(int) { st.ShowTiming = !st.ShowTiming }},
		{Label: "Show lyrics", Hint: "Karaoke-style lyrics across the top when a chart has them",
			Value: func() string { return onOff(st.ShowLyrics) }, Adjust: func(int) { st.ShowLyrics = !st.ShowLyrics }},
	}

	audioItems := []Item{
		{Label: "Volume", Value: func() string { return fmt.Sprintf("%.0f%%", st.Volume*100) },
			Adjust: func(d int) { st.Volume = clampf(st.Volume+float64(d)*0.05, 0, 1) }},
		{Label: "Mute stem on miss", Hint: "Silence your part's audio while you're missing notes",
			Value: func() string { return onOff(st.MuteOnMiss) }, Adjust: func(int) { st.MuteOnMiss = !st.MuteOnMiss }},
		{Label: "Audio offset", Hint: "How late you hear the music. Calibrate latency measures it",
			Value: func() string { return fmt.Sprintf("%.0f ms", st.AudioOffsetMs) }, Adjust: func(d int) { st.AudioOffsetMs += float64(d) }},
		{Label: "Video offset", Hint: "How late the screen shows the notes. Calibrate latency measures it",
			Value: func() string { return fmt.Sprintf("%.0f ms", st.VideoOffsetMs) }, Adjust: func(d int) { st.VideoOffsetMs += float64(d) }},
	}
	if !inSong {
		audioItems = append(audioItems, Item{Label: "Calibrate latency", Hint: "Measure audio and video delay so hits line up",
			Activate: func() { a.SetScene(NewCalibration(a)) }})
	}

	controls := []Item{
		{Label: "Guitar / bass setup", Hint: "Map and test a guitar controller (or keyboard)", Activate: func() { a.SetScene(NewWizard(a, input.FamilyGuitar)) }},
		{Label: "Drum setup", Hint: "Map a MIDI drum kit pad by pad (or keyboard)", Activate: func() { a.SetScene(NewWizard(a, input.FamilyDrums)) }},
	}

	system := []Item{
		{Label: "Fullscreen", Hint: "F11 or Alt+Enter also switch",
			Value: func() string { return onOff(ebiten.IsFullscreen()) },
			Adjust: func(int) {
				st.Fullscreen = !ebiten.IsFullscreen()
				ebiten.SetFullscreen(st.Fullscreen)
			}},
		{Label: "VSync", Value: func() string { return onOff(st.VSync) }, Adjust: func(int) { st.VSync = !st.VSync; ebiten.SetVsyncEnabled(st.VSync) }},
	}
	if !inSong {
		system = append(system,
			Item{Label: "Add a song folder...", Hint: "Type or paste a folder path, then Enter", Activate: func() {
				m.typing = &TextInput{Placeholder: `C:\Users\you\Songs`, Max: 400}
			}},
			Item{Label: "Rescan library", Activate: func() { a.RescanLibrary(); a.Toast("Rescanning library...") }},
		)
	}

	m.pages = []settingsPage{
		{pageGameplay, "Gameplay", "Hit windows, leniency, no fail, drums", gameplay},
		{pageHighway, "Highway and visuals", "Note speed, highway shape, effects, lyrics", highway},
		{pageAudio, "Audio and timing", "Volume, offsets, latency calibration", audioItems},
	}
	if !inSong {
		m.pages = append(m.pages, settingsPage{pageControls, "Controls", "Set up a guitar or drum kit", controls})
	}
	title := "Display and library"
	hint := "Fullscreen, VSync, song folders"
	if inSong {
		title, hint = "Display", "Fullscreen, VSync"
	}
	m.pages = append(m.pages, settingsPage{pageLibrary, title, hint, system})

	// Every change goes through onChange so the caller can apply it live.
	for pi := range m.pages {
		for ii := range m.pages[pi].items {
			it := &m.pages[pi].items[ii]
			if adj := it.Adjust; adj != nil {
				it.Adjust = func(d int) { adj(d); m.changed() }
			}
			if act := it.Activate; act != nil {
				it.Activate = func() { act(); m.changed() }
			}
		}
	}
	for _, p := range m.pages {
		p := p
		m.cats.Items = append(m.cats.Items, Item{Label: p.title, Hint: p.hint, Activate: func() { m.open(p.id) }})
	}
	return m
}

func (m *settingsMenu) changed() {
	if m.onChange != nil {
		m.onChange()
	}
}

// open shows a page by id.
func (m *settingsMenu) open(id string) {
	for i, p := range m.pages {
		if p.id == id {
			m.page = i
			m.cats.Sel = i
			m.items = List{Items: p.items, Visible: m.cats.Visible}
			return
		}
	}
}

// pageID is the open page's id ("" while choosing a page).
func (m *settingsMenu) pageID() string {
	if m.page < 0 {
		return ""
	}
	return m.pages[m.page].id
}

// title is the heading for what's on screen.
func (m *settingsMenu) title() string {
	if m.page < 0 {
		return "Settings"
	}
	return m.pages[m.page].title
}

// update handles input. It returns true when the player backs out of the
// page list (the caller closes the menu).
func (m *settingsMenu) update(a *App) bool {
	if m.typing != nil {
		done, cancel := m.typing.Update(a)
		switch {
		case cancel:
			m.typing = nil
		case done:
			addSongFolder(a, m.typing.Value)
			m.typing = nil
		}
		return false
	}
	if m.page < 0 {
		if m.cats.Update(a) {
			return false
		}
		return a.Pressed(action.MenuBack) || a.Mouse.Right
	}
	if m.items.Update(a) {
		return false
	}
	if a.Pressed(action.MenuBack) || a.Mouse.Right {
		m.page = -1
	}
	return false
}

// rows is how many rows the current list shows.
func (m *settingsMenu) rows() int {
	l := &m.cats
	if m.page >= 0 {
		l = &m.items
	}
	return min(len(l.Items), max(1, l.Visible))
}

// hint is the help line for the selected entry.
func (m *settingsMenu) hint() string {
	if m.page < 0 {
		return m.cats.Hint()
	}
	return m.items.Hint()
}

// draw draws the current list at x, y.
func (m *settingsMenu) draw(dst *ebiten.Image, x, y, w, rowH float64) {
	if m.page < 0 {
		m.cats.Draw(dst, x, y, w, rowH)
	} else {
		m.items.Draw(dst, x, y, w, rowH)
	}
}

func (m *settingsMenu) drawTyping(dst *ebiten.Image) {
	if m.typing == nil {
		return
	}
	render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xB8))
	Panel(dst, 260, 400, 1400, 240)
	render.Text(dst, "Add a song folder", render.UIBold, 36, 300, 430, th.Text, render.Left)
	m.typing.Draw(dst, 300, 500, 1320)
	render.Text(dst, "Enter to add, Esc to cancel", render.Cond, 24, 300, 580, th.TextDim, render.Left)
}
