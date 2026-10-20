package app

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

// Local multiplayer. Players join by pressing confirm (Start or green on a
// guitar, a green pad on a kit, Enter on the keyboard) on a controller no
// one has yet; that controller is theirs until they leave. Each player has
// a profile, which brings their own gameplay settings, calibration,
// bindings and scores.

// MaxPlayers is the most players at once.
const MaxPlayers = 8

// Multiplayer modes.
const (
	modeBand  = "band"  // shared band score; a failed player is out until a bandmate's star power saves them
	modeH2H   = "h2h"   // head to head: everyone for themselves, ranked
	modeParty = "party" // endless songs, no fail, anyone drops in and out at any time
)

var bandModes = []struct{ id, label, hint string }{
	{modeBand, "Band", "One band score. Fail and you're out until a bandmate's star power brings you back; the song fails if everyone is out"},
	{modeH2H, "Head to head", "Everyone for themselves. Fail and you stop; results rank the players"},
	{modeParty, "Party", "Songs play back to back with no failing. Anyone can join, leave or switch parts any time without stopping the music"},
}

func bandModeLabel(id string) string {
	for _, m := range bandModes {
		if m.id == id {
			return m.label
		}
	}
	return bandModes[0].label
}

// Layouts: how highways share the screen.
var bandLayouts = []struct{ id, label, hint string }{
	{"auto", "Auto", "Up to 4 players side by side, 5 to 8 in two rows of four"},
	{"row", "One row", "Every highway side by side"},
	{"rows", "Two rows", "Two rows of highways, top and bottom"},
}

func layoutLabel(id string) string {
	for _, l := range bandLayouts {
		if l.id == id {
			return l.label
		}
	}
	return bandLayouts[0].label
}

// device is one controller.
type device struct {
	src   input.Source
	index int
}

func deviceOf(ev input.RawEvent) device {
	if ev.Src == input.SrcKeyboard {
		return device{src: input.SrcKeyboard}
	}
	return device{src: ev.Src, index: ev.Device}
}

func (d device) name(a *App) string { return a.Input.DeviceName(d.src, d.index) }

// member is a player in the band.
type member struct {
	prof *profile.Profile
	dev  device
	inst chart.Instrument
	diff chart.Difficulty
	menu *input.MenuInput // menu navigation from this player's controller only
}

// family is the binding family the member's instrument uses.
func (m *member) family() input.Family {
	if m.inst.IsDrums() {
		return input.FamilyDrums
	}
	return input.FamilyGuitar
}

func (m *member) partLabel() string { return m.diff.Title() + " " + m.inst.Title() }

// Band is the players playing together, kept between songs.
type Band struct {
	Mode    string
	Members []*member
	Session string // recordings of one sitting share it

	// party mode
	played   []string // recent songs (folder keys), not picked again soon
	queueIdx int      // next playlist entry to play
}

func newBand(mode string) *Band {
	return &Band{Mode: mode, Session: time.Now().Format("20060102-150405")}
}

func (b *Band) owner(d device) *member {
	for _, m := range b.Members {
		if m.dev == d {
			return m
		}
	}
	return nil
}

func (b *Band) has(p *profile.Profile) bool {
	for _, m := range b.Members {
		if m.prof == p {
			return true
		}
	}
	return false
}

func (b *Band) remove(m *member) {
	for i, x := range b.Members {
		if x == m {
			b.Members = append(b.Members[:i], b.Members[i+1:]...)
			return
		}
	}
}

// freeProfile is the first profile no one in the band has, or nil.
func (b *Band) freeProfile(a *App) *profile.Profile {
	if a.Active != nil && !b.has(a.Active) {
		return a.Active
	}
	for _, p := range a.Profiles.All() {
		if !b.has(p) {
			return p
		}
	}
	return nil
}

// join adds a player on device d with the first free profile (making a new
// one if everyone's taken).
func (b *Band) join(a *App, d device) *member {
	p := b.freeProfile(a)
	if o := a.DeviceOwner(d); o != nil && !b.has(o) {
		p = o // the controller remembers who used it last
	}
	if p == nil {
		p = a.NewProfile("")
	}
	m := &member{prof: p, dev: d}
	m.setProfile(a, p)
	m.fitPart()
	b.Members = append(b.Members, m)
	log.Printf("band: %s joined on %s (%s)", p.Name, d.name(a), m.partLabel())
	return m
}

// setProfile switches the member to profile p, picking up its preferred
// part and controller bindings.
func (m *member) setProfile(a *App, p *profile.Profile) {
	m.prof = p
	set := a.SettingsFor(p)
	m.inst = chart.Guitar
	if inst, ok := chart.ParseInstrument(set.LastInstrument); ok {
		m.inst = inst
	}
	m.fitPart()
	m.diff = chart.Expert
	if d, ok := chart.ParseDifficulty(set.LastDifficulty); ok {
		m.diff = d
	}
	m.menu = input.NewMenuInput(a.BindingsFor(p), a.Input.Devices())
	a.LinkDevice(p, m.dev)
}

// remember saves the member's part as their profile's preference.
func (m *member) remember(a *App) {
	if m.prof == a.Active {
		a.Settings.LastInstrument, a.Settings.LastDifficulty = m.inst.String(), m.diff.String()
		_ = a.Settings.Save()
		return
	}
	m.prof.Player.LastInstrument, m.prof.Player.LastDifficulty = m.inst.String(), m.diff.String()
	a.SaveProfile(m.prof)
}

// splitInput sorts this frame's raw events by controller.
func splitInput(evs []input.RawEvent) map[device][]input.RawEvent {
	out := map[device][]input.RawEvent{}
	for _, ev := range evs {
		d := deviceOf(ev)
		out[d] = append(out[d], ev)
	}
	return out
}

// joinPresses tracks menu input on controllers no one owns, to notice
// someone pressing confirm to join.
type joinPresses struct {
	menus map[device]*input.MenuInput
}

// pressed returns the unowned controllers that pressed confirm (or back)
// this frame.
func (j *joinPresses) pressed(a *App, b *Band, byDev map[device][]input.RawEvent) (join, back []device) {
	if j.menus == nil {
		j.menus = map[device]*input.MenuInput{}
	}
	now := clock.Now()
	for d, evs := range byDev {
		if b.owner(d) != nil {
			delete(j.menus, d)
			continue
		}
		mi := j.menus[d]
		if mi == nil {
			mi = input.NewMenuInput(a.Bindings, a.Input.Devices())
			j.menus[d] = mi
		}
		for _, act := range mi.Feed(evs, now) {
			switch act {
			case action.MenuConfirm:
				join = append(join, d)
			case action.MenuBack:
				back = append(back, d)
			}
		}
	}
	return join, back
}

// memberActs runs a member's controller through their menu navigation.
func memberActs(m *member, byDev map[device][]input.RawEvent) []action.Action {
	return m.menu.Feed(byDev[m.dev], clock.Now())
}

// asMember runs fn with the menu input set to one member's, so lists
// they're using only react to their controller.
func asMember(a *App, acts []action.Action, fn func()) {
	saved, savedMouse, savedMenu := a.MenuActs, a.Mouse, a.Menu
	a.MenuActs, a.Mouse = acts, Mouse{}
	defer func() { a.MenuActs, a.Mouse, a.Menu = saved, savedMouse, savedMenu }()
	fn()
}

// slot is where one player's highway goes on screen.
type slot struct{ x, y, w, h float64 }

// bandSlots lays out n highways in the area below top.
func bandSlots(n int, layout string, top float64) []slot {
	if n <= 0 {
		return nil
	}
	rows := 1
	switch layout {
	case "rows":
		rows = min(2, n)
	case "row":
		rows = 1
	default:
		if n > 4 {
			rows = 2
		}
	}
	cols := int(math.Ceil(float64(n) / float64(rows)))
	h := (render.ScreenH - top) / float64(rows)
	w := render.ScreenW / float64(cols)
	var out []slot
	for r := 0; r < rows; r++ {
		inRow := cols
		if r == rows-1 {
			inRow = n - cols*(rows-1)
		}
		x0 := (render.ScreenW - w*float64(inRow)) / 2
		for c := 0; c < inRow; c++ {
			out = append(out, slot{x0 + float64(c)*w, top + float64(r)*h, w, h})
		}
	}
	return out
}

// ordinal is 1st, 2nd...
func ordinal(n int) string {
	suf := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suf = "st"
	case n%10 == 2:
		suf = "nd"
	case n%10 == 3:
		suf = "rd"
	}
	return fmt.Sprintf("%d%s", n, suf)
}

// partChoices are the instruments a player can pick.
var partChoices = []chart.Instrument{chart.Guitar, chart.Bass, chart.Rhythm, chart.Keys, chart.Drums, chart.GuitarCoop}

// cyclePart steps through the allowed instruments. With none allowed the
// instrument stays; one that isn't allowed moves to the first (or last).
func cyclePart(cur chart.Instrument, d int, allowed []chart.Instrument) chart.Instrument {
	n := len(allowed)
	if n == 0 {
		return cur
	}
	for j, x := range allowed {
		if x == cur {
			return allowed[((j+sign(d))%n+n)%n]
		}
	}
	if d < 0 {
		return allowed[n-1]
	}
	return allowed[0]
}

// fretParts are the parts played on a 5-fret guitar controller.
var fretParts = []chart.Instrument{chart.Guitar, chart.Bass, chart.Rhythm, chart.Keys, chart.GuitarCoop}

// partsFor are the parts a controller can play: a drum kit plays drums, a
// guitar any 5-fret part (guitar, bass, rhythm, keys, co-op), and the
// keyboard anything.
func partsFor(d device) []chart.Instrument {
	switch d.src {
	case input.SrcMIDI:
		return []chart.Instrument{chart.Drums}
	case input.SrcKeyboard:
		return partChoices
	}
	return fretParts
}

// parts are the member's playable parts, narrowed to what a song has when
// songParts isn't nil.
func (m *member) parts(songParts []chart.Instrument) []chart.Instrument {
	can := partsFor(m.dev)
	if songParts == nil {
		return can
	}
	var out []chart.Instrument
	for _, i := range can {
		for _, j := range songParts {
			if i == j {
				out = append(out, i)
			}
		}
	}
	return out
}

// fitPart moves the member to a part their controller can play.
func (m *member) fitPart() {
	for _, i := range partsFor(m.dev) {
		if i == m.inst {
			return
		}
	}
	m.inst = partsFor(m.dev)[0]
}

func cycleDiff(cur chart.Difficulty, d int) chart.Difficulty {
	// up is harder
	v := (int(cur)+sign(d))%4 + 4
	return chart.Difficulty(v % 4)
}

// trackFor picks a member's track in a chart: their difficulty, or the
// closest one the part has. nil when the chart has no such part.
func trackFor(c *chart.Chart, inst chart.Instrument, diff chart.Difficulty) *chart.Track {
	if t := c.Track(inst, diff); t != nil && len(t.Notes) > 0 {
		return t
	}
	ds := c.Available(inst) // hardest first
	if len(ds) == 0 {
		return nil
	}
	best := ds[0]
	for _, d := range ds {
		if absInt(int(d)-int(diff)) < absInt(int(best)-int(diff)) {
			best = d
		}
	}
	return c.Track(inst, best)
}

// testBand starts a multiplayer song from the command line (--players):
// the first player is the active profile, the rest stand-in profiles that
// aren't saved. Parts go round the instruments the chart has.
func (a *App) testBand(s *chart.Song, inst chart.Instrument, diff chart.Difficulty) Scene {
	mode := a.Opts.Mode
	if mode == "" {
		mode = modeBand
	}
	if a.Opts.Layout != "" {
		a.Settings.Multi.Layout = a.Opts.Layout
	}
	b := newBand(mode)
	var parts []chart.Instrument
	if c, err := s.LoadChart(); err == nil {
		for _, i := range partChoices {
			if len(c.Available(i)) > 0 {
				parts = append(parts, i)
			}
		}
	}
	if len(parts) == 0 {
		parts = []chart.Instrument{inst}
	}
	// the requested instrument first
	for i, p := range parts {
		if p == inst {
			parts[0], parts[i] = parts[i], parts[0]
		}
	}
	n := min(MaxPlayers, a.Opts.Players)
	for i := 0; i < n; i++ {
		p := a.Active
		if i > 0 {
			p = profile.Temp(fmt.Sprintf("Player %d", i+1), profile.Palette[i%len(profile.Palette)], a.Settings.Player())
		}
		m := &member{prof: p, dev: device{src: input.SrcGamepad, index: 1000 + i}, inst: parts[i%len(parts)], diff: diff}
		m.menu = input.NewMenuInput(a.BindingsFor(p), a.Input.Devices())
		b.Members = append(b.Members, m)
	}
	a.Band = b
	return NewBandLoading(a, b, s)
}
