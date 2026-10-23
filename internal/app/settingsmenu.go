package app

import (
	"fmt"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/render"
)

// Settings are grouped into pages. The same pages are used by the Settings
// screen and the pause menu; pages and items that can't be used mid-song
// are left out there.

const (
	pageGameplay   = "gameplay"
	pageHighway    = "highway"
	pageBackground = "background"
	pageAudio      = "audio"
	pageControls   = "controls"
	pageLibrary    = "library"
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

	typing      *TextInput
	typingTitle string
	onTyped     func(string)

	picker       *colorPicker // editing a theme color
	previewDrums bool         // the Appearance preview plays drums
}

// startTyping asks for a line of text.
func (m *settingsMenu) startTyping(title, value string, done func(string)) {
	m.typing = &TextInput{Value: value, Max: 400}
	m.typingTitle, m.onTyped = title, done
}

func clampf(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// cycleString steps through opts from cur by d (wrapping); an unknown cur starts at opts[0].
func cycleString(opts []string, cur string, d int) string {
	i := 0
	for j, o := range opts {
		if o == cur {
			i = j
		}
	}
	return opts[((i+sign(d))%len(opts)+len(opts))%len(opts)]
}

func backingLabel(v string) string {
	switch v {
	case "above":
		return "Above lead"
	case "off":
		return "Hidden"
	}
	return "Below lead"
}

func backingSize(v float64) float64 {
	if v <= 0 {
		return render.DefaultLyricOpts().BackingScale
	}
	return v
}

// lyricOpts are the saved lyric display choices.
func lyricOpts(st *config.Settings) render.LyricOpts {
	fx := st.LyricFx
	if fx == "" {
		fx = "full"
	}
	return render.LyricOpts{Backing: st.BackingVocals, BackingScale: backingSize(st.BackingSize), Effects: fx}
}

func lyricFxLabel(v string) string {
	switch v {
	case "calm":
		return "Calm"
	case "off":
		return "Off"
	}
	return "Full"
}

func sign(d int) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}

func absInt(d int) int {
	if d < 0 {
		return -d
	}
	return d
}

// Note speed is shown as a multiple of the default: 1.00x shows 1.05 s of
// notes on the highway, 2.00x half that.
const (
	baseLookahead = 1.05
	minNoteSpeed  = 0.25
	maxNoteSpeed  = 25
)

func noteSpeed(lookahead float64) float64 {
	if lookahead <= 0 {
		return 1
	}
	return baseLookahead / lookahead
}

func lookaheadFor(speed float64) float64 { return baseLookahead / speed }

// speedStep is the note speed change for one step.
const speedStep = 0.01

func newSettingsMenu(a *App, inSong bool) *settingsMenu {
	m := &settingsMenu{inSong: inSong, page: -1}
	st := a.Settings
	hw := &st.Highway
	nextRun := ""
	if inSong {
		nextRun = " (applies when you restart)"
	}

	ms := func(v float64) string { return fmt.Sprintf("%.0f ms", v*1000) }
	gameplay := []Item{
		{Label: "Hit window", Hint: "Early and late window around each note" + nextRun, Range: true,
			Value: func() string { return "±" + ms(st.Engine.EarlyWindow) },
			Adjust: func(d int) {
				v := clampf(st.Engine.EarlyWindow+float64(d)*0.001, 0.02, 0.2)
				st.Engine.EarlyWindow, st.Engine.LateWindow = v, v
			}},
		{Label: "Drum hit window", Hint: "Early and late window for drum notes" + nextRun, Range: true,
			Value: func() string { e, _ := st.Engine.Windows(true); return "±" + ms(e) },
			Adjust: func(d int) {
				e, _ := st.Engine.Windows(true)
				st.Engine.DrumWindow = clampf(e+float64(d)*0.001, 0.02, 0.2)
			}},
		{Label: "Infinite front end", Hint: "Strum the next note as early as you like once the previous one has passed" + nextRun,
			Value: func() string { return onOff(st.Engine.InfiniteFrontEnd) }, Adjust: func(int) { st.Engine.InfiniteFrontEnd = !st.Engine.InfiniteFrontEnd }},
		{Label: "HOPO strum leniency", Hint: "A strum this soon after hammering a HOPO is ignored" + nextRun, Range: true,
			Value:  func() string { return ms(st.Engine.StrumLenience) },
			Adjust: func(d int) { st.Engine.StrumLenience = clampf(st.Engine.StrumLenience+float64(d)*0.001, 0, 0.2) }},
		{Label: "Chord leniency", Hint: "How long a strum waits for the frets to finish landing" + nextRun, Range: true,
			Value:  func() string { return ms(st.Engine.ChordLenience) },
			Adjust: func(d int) { st.Engine.ChordLenience = clampf(st.Engine.ChordLenience+float64(d)*0.001, 0.001, 0.15) }},
		{Label: "No fail", Hint: "Keep playing when the rock meter runs out" + nextRun,
			Value: func() string { return onOff(st.Engine.NoFail) }, Adjust: func(int) { st.Engine.NoFail = !st.Engine.NoFail }},
		{Label: "Pro drums (cymbals)", Hint: "Tell cymbals and toms apart on drum charts that mark them" + nextRun,
			Value: func() string { return onOff(st.Engine.ProDrums) }, Adjust: func(int) { st.Engine.ProDrums = !st.Engine.ProDrums }},
		{Label: "Expert+ double kick", Hint: "Include the extra kick notes on Expert drums" + nextRun,
			Value: func() string { return onOff(st.Engine.ExpertPlus) }, Adjust: func(int) { st.Engine.ExpertPlus = !st.Engine.ExpertPlus }},
		{Label: "Count-in", Hint: "Time before the song starts" + nextRun, Range: true,
			Value:  func() string { return fmt.Sprintf("%.1f s", st.LeadIn) },
			Adjust: func(d int) { st.LeadIn = clampf(st.LeadIn+float64(d)*0.1, 0.5, 10) }},
	}

	highway := []Item{
		{Label: "Note speed", Hint: "How fast notes come down the highway (higher is faster)", Range: true,
			Value: func() string { return fmt.Sprintf("%.2fx", noteSpeed(hw.Lookahead)) },
			Adjust: func(d int) {
				v := math.Round(noteSpeed(hw.Lookahead)*100)/100 + float64(d)*speedStep
				hw.Lookahead = lookaheadFor(clampf(v, minNoteSpeed, maxNoteSpeed))
			}},
		{Label: "Highway width", Range: true, Value: func() string { return fmt.Sprintf("%.0f px", hw.NearWidth) },
			Adjust: func(d int) { hw.NearWidth = clampf(hw.NearWidth+float64(d)*2, 300, 1400) }},
		{Label: "Highway angle", Hint: "Lower is steeper (more perspective)", Range: true,
			Value:  func() string { return fmt.Sprintf("%.3f", hw.FarRatio) },
			Adjust: func(d int) { hw.FarRatio = clampf(hw.FarRatio+float64(d)*0.005, 0.05, 1) }},
		{Label: "Highway length", Hint: "How far up the screen the highway reaches", Range: true,
			Value:  func() string { return fmt.Sprintf("%.0f", hw.StrikeY-hw.TopY) },
			Adjust: func(d int) { hw.TopY = clampf(hw.TopY-float64(d)*2, 0, hw.StrikeY-150) }},
		{Label: "Reset highway shape", Hint: "Width, angle and length back to the defaults (note speed stays)", Activate: func() {
			look := hw.Lookahead
			st.Highway = render.DefaultHighway()
			st.Highway.Lookahead = look
		}},
		{Label: "Highway bounce", Hint: "How much open notes and kicks bounce the highway", Range: true,
			Value:  func() string { return fmt.Sprintf("%.0f%%", st.HighwayBounce*100) },
			Adjust: func(d int) { st.HighwayBounce = clampf(st.HighwayBounce+float64(d)*0.01, 0, 1) }},
		{Label: "Hit effects", Hint: "Flash, flames and sparks when you hit a note (the fret punch always shows)",
			Value: func() string {
				l := render.ParseFxLevel(st.HitFx).String()
				return strings.ToUpper(l[:1]) + l[1:]
			},
			Adjust: func(d int) {
				l := (int(render.ParseFxLevel(st.HitFx)) + sign(d) + 3) % 3
				st.HitFx = render.FxLevel(l).String()
			}},
		{Label: "Show hit timing", Hint: "Show +/- ms under the strike line on every hit",
			Value: func() string { return onOff(st.ShowTiming) }, Adjust: func(int) { st.ShowTiming = !st.ShowTiming }},
		{Label: "Show lyrics", Hint: "Karaoke-style lyrics across the top when a chart has them",
			Value: func() string { return onOff(st.ShowLyrics) }, Adjust: func(int) { st.ShowLyrics = !st.ShowLyrics }},
		{Label: "Backing vocals", Hint: "Harmony and backing parts, each in its own color, when a chart has them",
			Value:  func() string { return backingLabel(st.BackingVocals) },
			Adjust: func(d int) { st.BackingVocals = cycleString(render.BackingPlacements, st.BackingVocals, d) }},
		{Label: "Backing vocal size", Hint: "Text size of backing parts compared with the lead", Range: true,
			Value:  func() string { return fmt.Sprintf("%.0f%%", backingSize(st.BackingSize)*100) },
			Adjust: func(d int) { st.BackingSize = clampf(backingSize(st.BackingSize)+float64(d)*0.05, 0.5, 1) }},
		{Label: "Lyric effects", Hint: "Animated lyrics when a chart asks for them. Calm drops shaking, glitching, flying and exploding text",
			Value:  func() string { return lyricFxLabel(st.LyricFx) },
			Adjust: func(d int) { st.LyricFx = cycleString(render.LyricFxModes, lyricOpts(st).Effects, d) }},
	}

	background := []Item{
		{Label: "Background", Hint: "Images from " + CustomsDir(), Range: true,
			Value: func() string { return truncateW(bgLabel(st.Background), render.UIBold, 32, 330) },
			Adjust: func(d int) {
				st.Background = cycleBackground(st.Background, a.BG.List(), d)
				a.BG.Apply(st.Background, true)
			}},
		{Label: "Darken background", Hint: "Dims the image so the highway and notes stand out", Range: true,
			Value:  func() string { return fmt.Sprintf("%.0f%%", st.BackgroundDim*100) },
			Adjust: func(d int) { st.BackgroundDim = clampf(st.BackgroundDim+float64(d)*0.01, 0, 0.95) }},
		{Label: "Behind menus too", Hint: "Show the background image behind menus as well as songs",
			Value: func() string { return onOff(st.BackgroundMenus) }, Adjust: func(int) { st.BackgroundMenus = !st.BackgroundMenus }},
	}
	if !inSong {
		background = append(background,
			Item{Label: "Open the image folder", Hint: CustomsDir(), Activate: func() { openFolder(a, CustomsDir()) }},
			Item{Label: "Look for new images", Activate: func() {
				a.Toast(fmt.Sprintf("%s found", plural(len(a.BG.Rescan()), "background image")))
			}},
		)
	}

	audioItems := []Item{
		{Label: "Volume", Range: true, Value: func() string { return fmt.Sprintf("%.0f%%", st.Volume*100) },
			Adjust: func(d int) { st.Volume = clampf(st.Volume+float64(d)*0.01, 0, 1) }},
		{Label: "Mute stem on miss", Hint: "Silence your part's audio while you're missing notes",
			Value: func() string { return onOff(st.MuteOnMiss) }, Adjust: func(int) { st.MuteOnMiss = !st.MuteOnMiss }},
		{Label: "Error sounds", Hint: "Which mistakes make a noise",
			Value:  func() string { return errorSoundsLabel(st.ErrorSounds) },
			Adjust: func(d int) { st.ErrorSounds = cycleErrorSounds(st.ErrorSounds, d) }},
		{Label: "Error sound volume", Hint: "Relative to Volume", Range: true,
			Value:  func() string { return fmt.Sprintf("%.0f%%", st.ErrorVolume*100) },
			Adjust: func(d int) { st.ErrorVolume = clampf(st.ErrorVolume+float64(d)*0.01, 0, 1) }},
		{Label: "Audio offset", Hint: "How late you hear the music. Calibrate latency measures it", Range: true,
			Value: func() string { return fmt.Sprintf("%.0f ms", st.AudioOffsetMs) }, Adjust: func(d int) { st.AudioOffsetMs += float64(d) }},
		{Label: "Video offset", Hint: "How late the screen shows the notes. Calibrate latency measures it", Range: true,
			Value: func() string { return fmt.Sprintf("%.0f ms", st.VideoOffsetMs) }, Adjust: func(d int) { st.VideoOffsetMs += float64(d) }},
	}
	if !inSong {
		audioItems = append(audioItems[:1], append([]Item{{Label: "Song previews", Hint: "Volume of the music that plays in the song list, relative to Volume", Range: true,
			Value: func() string {
				if st.PreviewVolume <= 0 {
					return "Off"
				}
				return fmt.Sprintf("%.0f%%", st.PreviewVolume*100)
			},
			Adjust: func(d int) { st.PreviewVolume = clampf(math.Round((st.PreviewVolume+float64(d)*0.01)*100)/100, 0, 1) }}}, audioItems[1:]...)...)
		audioItems = append(audioItems, Item{Label: "Calibrate latency", Hint: "Measure audio and video delay so hits line up",
			Activate: func() { a.SetScene(NewCalibration(a)) }})
	}

	controls := []Item{
		{Label: "Guitar / bass setup", Hint: "Map and test a guitar controller (or keyboard). Saved to the profile you pick, starting with whoever's controller pressed green", Activate: func() { a.SetScene(NewWizard(a, input.FamilyGuitar)) }},
		{Label: "Drum setup", Hint: "Map a MIDI drum kit or keyboard keys pad by pad. Saved to the profile you pick", Activate: func() { a.SetScene(NewWizard(a, input.FamilyDrums)) }},
	}

	system := []Item{
		{Label: "Fullscreen", Hint: "F11 or Alt+Enter also switch",
			Value: func() string { return onOff(ebiten.IsFullscreen()) },
			Adjust: func(int) {
				st.Fullscreen = !ebiten.IsFullscreen()
				ebiten.SetFullscreen(st.Fullscreen)
			}},
		{Label: "VSync", Value: func() string { return onOff(st.VSync) }, Adjust: func(int) { st.VSync = !st.VSync; ebiten.SetVsyncEnabled(st.VSync) }},
		{Label: "Results charts", Hint: "Large draws the run and history charts across the whole window. F on the results screen also switches.",
			Value: func() string {
				if st.LargeCharts {
					return "Large"
				}
				return "Normal"
			},
			Adjust: func(int) { st.LargeCharts = !st.LargeCharts }},
	}
	if !inSong {
		system = append(system,
			Item{Label: "Add a song folder...", Hint: "Type or paste a folder path, then Enter", Activate: func() {
				m.startTyping("Add a song folder", "", func(s string) { addSongFolder(a, s) })
				m.typing.Placeholder = `C:\Users\you\Songs`
			}},
			Item{Label: "Rescan library", Activate: func() { a.RescanLibrary(); a.Toast("Rescanning library...") }},
		)
	}

	m.pages = []settingsPage{
		{pageGameplay, "Gameplay", "Hit windows, leniency, no fail, drums", gameplay},
		{pageHighway, "Highway and visuals", "Note speed, highway shape, bounce, effects, lyrics", highway},
		{pageLooks, "Appearance", "Themes, note materials, highway style and every color", lookItems(a, m, inSong)},
		{pageBackground, "Background", "Pick a background image and how dark it is", background},
		{pageAudio, "Audio and timing", "Volume, previews, error sounds, offsets, latency calibration", audioItems},
	}
	if !inSong {
		m.pages = append(m.pages, settingsPage{pageControls, "Controls", "Set up a guitar or drum kit", controls})
	}
	title := "Display and library"
	hint := "Fullscreen, VSync, results charts, song folders"
	if inSong {
		title, hint = "Display", "Fullscreen, VSync, results charts"
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
	if m.picker != nil {
		return m.picker.slot.Label
	}
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
			v := m.typing.Value
			m.typing = nil
			if m.onTyped != nil {
				m.onTyped(v)
			}
		}
		return false
	}
	if m.picker != nil {
		closed := m.picker.update(a)
		m.changed()
		if closed {
			m.picker = nil
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
	switch {
	case m.picker != nil:
		l = &m.picker.list
	case m.page >= 0:
		l = &m.items
	}
	return min(len(l.Items), max(1, l.Visible))
}

// hint is the help line for the selected entry.
func (m *settingsMenu) hint() string {
	if m.picker != nil {
		return m.picker.list.Hint()
	}
	if m.page < 0 {
		return m.cats.Hint()
	}
	return m.items.Hint()
}

// draw draws the current list at x, y.
func (m *settingsMenu) draw(dst *ebiten.Image, x, y, w, rowH float64) {
	if m.picker != nil {
		m.picker.list.Draw(dst, x, y, w, rowH)
		return
	}
	if m.page < 0 {
		m.cats.Draw(dst, x, y, w, rowH)
	} else {
		m.items.Draw(dst, x, y, w, rowH)
	}
}

func (m *settingsMenu) drawTyping(dst *ebiten.Image) {
	if m.picker != nil {
		m.picker.drawTyping(dst)
	}
	if m.typing == nil {
		return
	}
	render.Dim(dst, render.RGBA(0, 0, 0, 0xB8))
	Panel(dst, 260, 400, 1400, 240)
	render.Text(dst, m.typingTitle, render.UIBold, 36, 300, 430, th.Text, render.Left)
	m.typing.Draw(dst, 300, 500, 1320)
	render.Text(dst, "Enter to add, Esc to cancel", render.Cond, 24, 300, 580, th.TextDim, render.Left)
}

// Error sound modes: which judgments make the miss noise.
var errorSoundModes = []struct{ id, label string }{
	{"off", "Off"},
	{"overstrum", "Overstrums"},
	{"extra", "Overstrums and extra hits"},
	{"all", "Every miss"},
}

func errorSoundsIndex(mode string) int {
	for i, m := range errorSoundModes {
		if m.id == mode {
			return i
		}
	}
	return 1 // unknown: the default
}

func errorSoundsLabel(mode string) string { return errorSoundModes[errorSoundsIndex(mode)].label }

func cycleErrorSounds(mode string, d int) string {
	n := len(errorSoundModes)
	return errorSoundModes[(errorSoundsIndex(mode)+d%n+n)%n].id
}
