package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

// Lobby is where players join before multiplayer: each one presses a button
// on their controller and sets up their own card with it.
type Lobby struct {
	band  *Band
	cards map[*member]*lobbyCard
	joins joinPresses
	start float64 // when everyone became ready (0: not all ready)
}

type lobbyCard struct {
	list    List
	ready   bool
	newProf bool // the profile row is on "New profile"
	joined  float64
}

// NewLobby opens the lobby with a fresh band, or the band from the last
// song (coming back from the song list or results).
func NewLobby(a *App) *Lobby {
	b := a.Band
	if b == nil {
		b = newBand(a.Settings.Multi.Mode)
		a.Band = b
	}
	l := &Lobby{band: b, cards: map[*member]*lobbyCard{}}
	for _, m := range b.Members {
		l.card(a, m)
	}
	return l
}

func (l *Lobby) host() *member {
	if len(l.band.Members) == 0 {
		return nil
	}
	return l.band.Members[0]
}

// card builds (or rebuilds) a member's card.
func (l *Lobby) card(a *App, m *member) *lobbyCard {
	c := l.cards[m]
	if c == nil {
		c = &lobbyCard{joined: clock.Now()}
		l.cards[m] = c
	}
	sel := c.list.Sel
	var items []Item
	if m == l.host() {
		items = append(items,
			Item{Label: "Mode", Hint: bandModes[0].hint,
				Value: func() string { return bandModeLabel(l.band.Mode) },
				Adjust: func(d int) {
					l.band.Mode = cycleMode(l.band.Mode, d)
					a.Settings.Multi.Mode = l.band.Mode
					_ = a.Settings.Save()
				}},
			Item{Label: "Layout",
				Value: func() string { return layoutLabel(a.Settings.Multi.Layout) },
				Adjust: func(d int) {
					a.Settings.Multi.Layout = cycleLayout(a.Settings.Multi.Layout, d)
					_ = a.Settings.Save()
				}},
		)
	}
	items = append(items,
		Item{Label: "Profile",
			Value: func() string {
				if c.newProf {
					return "+ New profile"
				}
				return m.prof.Name
			},
			Swatch: func() color.RGBA {
				if c.newProf {
					return th.TextDim
				}
				return profileColor(m.prof)
			},
			Adjust: func(d int) { l.cycleProfile(a, m, c, d) },
			Activate: func() {
				if c.newProf {
					p := a.NewProfile("")
					m.setProfile(a, p)
					c.newProf = false
					a.Toast("Made " + p.Name + ". Rename it in Profiles from the main menu")
				}
			}},
		Item{Label: "Instrument", Value: func() string { return m.inst.Title() },
			Adjust: func(d int) { m.inst = cyclePart(m.inst, d, m.parts(nil)) }},
		Item{Label: "Difficulty", Value: func() string { return m.diff.Title() },
			Adjust: func(d int) { m.diff = cycleDiff(m.diff, d) }},
		Item{Label: "Ready", Activate: func() {
			if c.newProf {
				c.newProf = false
			}
			c.ready = true
			m.remember(a)
		}},
		Item{Label: "Controls", Hint: "Map this controller for your profile (only you are changed)", Activate: func() {
			if c.newProf {
				return
			}
			b := l.band
			a.SetScene(NewWizardFor(a, m.family(), m.prof, &m.dev, func(a *App) {
				m.menu = input.NewMenuInput(a.BindingsFor(m.prof), a.Input.Devices())
				a.Band = b
				a.SetScene(NewLobby(a))
			}))
		}},
	)
	c.list = List{Items: items, Visible: len(items)}
	c.list.Sel = min(sel, len(items)-1)
	return c
}

func cycleMode(cur string, d int) string {
	i := 0
	for j, m := range bandModes {
		if m.id == cur {
			i = j
		}
	}
	n := len(bandModes)
	return bandModes[((i+sign(d))%n+n)%n].id
}

func cycleLayout(cur string, d int) string {
	i := 0
	for j, l := range bandLayouts {
		if l.id == cur {
			i = j
		}
	}
	n := len(bandLayouts)
	return bandLayouts[((i+sign(d))%n+n)%n].id
}

// cycleProfile steps a member through the profiles no one else has, plus
// "New profile".
func (l *Lobby) cycleProfile(a *App, m *member, c *lobbyCard, d int) {
	var opts []*profile.Profile
	for _, p := range a.Profiles.All() {
		if p == m.prof || !l.band.has(p) {
			opts = append(opts, p)
		}
	}
	opts = append(opts, nil) // new profile
	i := 0
	for j, p := range opts {
		if (c.newProf && p == nil) || (!c.newProf && p == m.prof) {
			i = j
		}
	}
	n := len(opts)
	next := opts[((i+sign(d))%n+n)%n]
	if next == nil {
		c.newProf = true
		return
	}
	c.newProf = false
	m.setProfile(a, next)
}

func (l *Lobby) allReady() bool {
	if len(l.band.Members) == 0 {
		return false
	}
	for _, m := range l.band.Members {
		if c := l.cards[m]; c == nil || !c.ready {
			return false
		}
	}
	return true
}

func (l *Lobby) leave(a *App, m *member) {
	wasHost := m == l.host()
	l.band.remove(m)
	delete(l.cards, m)
	if wasHost && l.host() != nil {
		l.card(a, l.host()) // the next player gets the band options
	}
}

func (l *Lobby) Update(a *App) error {
	byDev := splitInput(a.Raw)
	join, back := l.joins.pressed(a, l.band, byDev)
	fresh := map[*member]bool{} // the join press isn't also a press on the new card
	for _, d := range join {
		if len(l.band.Members) >= MaxPlayers {
			a.Toast(fmt.Sprintf("The band is full (%d players)", MaxPlayers))
			break
		}
		m := l.band.join(a, d)
		fresh[m] = true
		l.card(a, m)
		l.start = 0
	}
	// Back on a controller no one has leaves the lobby.
	if len(back) > 0 && len(join) == 0 {
		a.Band = nil
		a.SetScene(NewMainMenu(a))
		return nil
	}
	for _, m := range append([]*member(nil), l.band.Members...) {
		c := l.cards[m]
		acts := memberActs(m, byDev)
		if len(acts) == 0 || fresh[m] {
			continue
		}
		if c.ready {
			for _, act := range acts {
				if act == action.MenuBack {
					c.ready = false
					l.start = 0
				}
			}
			continue
		}
		used := false
		asMember(a, acts, func() { used = c.list.Update(a) })
		if used {
			continue
		}
		for _, act := range acts {
			if act == action.MenuBack {
				l.leave(a, m)
				break
			}
		}
	}
	// Everyone ready: go after a short beat (so a late joiner can still
	// get in).
	if l.allReady() {
		if l.start == 0 {
			l.start = clock.Now()
		}
		if clock.Now()-l.start > 0.8 {
			l.begin(a)
		}
	} else {
		l.start = 0
	}
	if a.Opts.Screenshot != "" && a.Opts.ShotScene == "lobby" && a.frame > 30 {
		a.TakeScreenshotNext()
	}
	return nil
}

// begin moves on from the lobby: to the song list, or straight into party
// mode.
func (l *Lobby) begin(a *App) {
	a.Band = l.band
	l.band.Mode = a.Settings.Multi.Mode
	if l.band.Mode == modeParty {
		a.SetScene(NewPartyNext(a, nil))
		return
	}
	a.SetScene(NewSongSelect(a))
}

func (l *Lobby) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	n := len(l.band.Members)
	sub := "Press START or green on a guitar, green on a drum kit, or Enter on the keyboard to join"
	if n > 0 {
		sub = fmt.Sprintf("%s · %s · %s", plural(n, "player"), bandModeLabel(a.Settings.Multi.Mode), layoutLabel(a.Settings.Multi.Layout))
	}
	Header(dst, "Multiplayer", sub)

	cols, rows := 4, 2
	cw, ch := 420.0, 396.0
	gap := 18.0
	x0 := (render.ScreenW - float64(cols)*cw - float64(cols-1)*gap) / 2
	y0 := 204.0
	for i := 0; i < MaxPlayers; i++ {
		x := x0 + float64(i%cols)*(cw+gap)
		y := y0 + float64(i/cols)*(ch+gap)
		if i < n {
			m := l.band.Members[i]
			l.drawCard(dst, a, m, l.cards[m], x, y, cw, ch, i == 0)
		} else if i == n {
			drawJoinSlot(dst, x, y, cw, ch, true)
		} else if i < rows*cols {
			drawJoinSlot(dst, x, y, cw, ch, false)
		}
	}
	hint := "Each player uses their own controller on their card. Red or Esc leaves; Ready when you're set"
	if h := l.host(); h != nil {
		if c := l.cards[h]; c != nil && !c.ready && c.list.Sel < 2 {
			hint = modeHint(a.Settings.Multi.Mode)
			if c.list.Sel == 1 {
				hint = bandLayouts[layoutIndex(a.Settings.Multi.Layout)].hint
			}
		}
	}
	if l.start > 0 {
		hint = "Everyone's ready!"
	}
	Footer(dst, hint)
}

func modeHint(id string) string {
	for _, m := range bandModes {
		if m.id == id {
			return m.hint
		}
	}
	return ""
}

func layoutIndex(id string) int {
	for i, l := range bandLayouts {
		if l.id == id {
			return i
		}
	}
	return 0
}

func drawJoinSlot(dst *ebiten.Image, x, y, w, h float64, next bool) {
	a := 0.35
	if next {
		a = 0.55 + 0.2*math.Sin(clock.Now()*3)
	}
	render.RoundRect(dst, x, y, w, h, 18, render.WithAlpha(th.Panel, 0.35))
	render.StrokeRect(dst, x+2, y+2, w-4, h-4, 2, render.WithAlpha(th.TextDim, a*0.6))
	if next {
		render.Text(dst, "Press START", render.UIBold, 34, x+w/2, y+h/2-46, render.WithAlpha(th.Text, a), render.Center)
		render.Text(dst, "or green / Enter to join", render.UI, 26, x+w/2, y+h/2+4, render.WithAlpha(th.TextDim, a), render.Center)
	}
}

func (l *Lobby) drawCard(dst *ebiten.Image, a *App, m *member, c *lobbyCard, x, y, w, h float64, host bool) {
	col := profileColor(m.prof)
	Panel(dst, x, y, w, h)
	render.FillRect(dst, x, y, w, 8, col)
	name := truncateW(m.prof.Name, render.UIBold, 38, w-40)
	if c.newProf {
		name = "New profile"
	}
	render.Text(dst, name, render.UIBold, 38, x+22, y+22, th.Text, render.Left)
	dev := truncateW(m.dev.name(a), render.Cond, 22, w-44)
	render.Text(dst, dev, render.Cond, 22, x+22, y+72, th.TextDim, render.Left)
	if host {
		render.Text(dst, "HOST", render.CondBold, 20, x+w-22, y+76, render.WithAlpha(col, 0.9), render.Right)
	}
	rowH := 40.0
	c.list.Draw(dst, x+10, y+106, w-20, rowH)
	if c.ready {
		render.FillRect(dst, x, y+h-62, w, 62, render.WithAlpha(col, 0.85))
		render.Text(dst, "READY", render.Display, 40, x+w/2, y+h-60, th.Text, render.Center)
	}
}
