package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

// Controls shows a player's bindings and changes them one at a time: green
// on a row waits for the new control, yellow has the rest (add another,
// remove one, back to default). The full setup is one row of many.
type Controls struct {
	prof *profile.Profile
	fam  input.Family
	dev  *device // only this controller counts (a lobby card opened it)
	back func(a *App)

	list List
	acts map[int]action.Action // list row -> action

	card    *List // yellow on a row
	cardAct action.Action

	cap *bindCapture

	menu *input.MenuInput // dev's own menu buttons, with this player's bindings
}

// bindCapture is waiting for the control to bind.
type bindCapture struct {
	act      action.Action
	add      bool
	started  float64
	axes     map[input.ControlKey]*axisTrack
	baseline map[input.ControlKey]float32
	warn     string
}

const captureTime = 8.0 // seconds before giving up

// NewControls opens the Controls screen on the player whose controller
// opened it (or the active profile).
func NewControls(a *App) *Controls {
	prof := a.Active
	if a.confirmSet {
		if p := a.DeviceOwner(a.confirmDev); p != nil {
			prof = p
		}
	}
	return newControls(a, input.FamilyGuitar, prof, nil, leaveControls)
}

// leaveControls goes back to the settings, on the Controls row.
func leaveControls(a *App) {
	s := NewSettings(a)
	s.menu.selectPage(pageControls)
	a.SetScene(s)
}

// NewControlsFor opens the Controls screen for one player and, with dev,
// one controller.
func NewControlsFor(a *App, fam input.Family, p *profile.Profile, dev *device, back func(a *App)) *Controls {
	return newControls(a, fam, p, dev, back)
}

func newControls(a *App, fam input.Family, p *profile.Profile, dev *device, back func(a *App)) *Controls {
	c := &Controls{prof: p, fam: fam, dev: dev, back: back}
	c.build(a)
	c.refreshMenu(a)
	return c
}

// refreshMenu keeps a lobby player's menu buttons in step with their
// bindings: they may not be the same as the active profile's.
func (c *Controls) refreshMenu(a *App) {
	if c.dev != nil {
		c.menu = input.NewMenuInput(c.bindings(a), a.Input.Devices())
	}
}

func (c *Controls) bindings(a *App) *input.Bindings { return a.BindingsFor(c.prof) }

func (c *Controls) profile(a *App) *input.Profile { return c.bindings(a).Profile(c.fam) }

// actions are the rows of this family: what's played, then menus.
func (c *Controls) actions() action.Set {
	if c.fam == input.FamilyDrums {
		return action.DrumSet
	}
	return action.GuitarSet
}

func (c *Controls) famLabel() string {
	if c.fam == input.FamilyDrums {
		return "Drums"
	}
	return "Guitar / bass"
}

// bindsText lists an action's controls, controllers first.
func bindsText(bs []input.Binding) string {
	var pads, keys []string
	for _, b := range bs {
		if b.Src == "keyboard" {
			keys = append(keys, b.Short())
		} else {
			pads = append(pads, b.Short())
		}
	}
	if all := append(pads, keys...); len(all) > 0 {
		return strings.Join(all, ",  ")
	}
	return "Not set"
}

func (c *Controls) build(a *App) {
	sel := c.list.Sel
	c.acts = map[int]action.Action{}
	var items []Item
	if a.Profiles != nil && len(a.Profiles.All()) > 1 && c.dev == nil {
		items = append(items, Item{Label: "Player", Hint: "Each profile keeps its own controls",
			Value:  func() string { return c.prof.Name },
			Swatch: func() color.RGBA { return profileColor(c.prof) },
			Adjust: func(d int) {
				all := a.Profiles.All()
				i := max(0, a.Profiles.Index(c.prof))
				c.prof = all[((i+sign(d))%len(all)+len(all))%len(all)]
				c.build(a)
			}})
	}
	items = append(items, Item{Label: "Instrument", Hint: "Guitar / bass covers rhythm and keys too",
		Value: func() string { return c.famLabel() },
		Adjust: func(int) {
			c.fam = map[input.Family]input.Family{input.FamilyGuitar: input.FamilyDrums, input.FamilyDrums: input.FamilyGuitar}[c.fam]
			c.build(a)
		}})
	row := func(act action.Action) {
		c.acts[len(items)] = act
		items = append(items, Item{Label: act.Title(),
			Hint:     "Green changes it. Yellow adds another control, removes one or goes back to the default",
			Value:    func() string { return truncateW(bindsText(c.profile(a).Get(act)), render.UI, 32, 1000) },
			Activate: func() { c.startCapture(a, act, false) }})
	}
	items = append(items, Item{Label: "Playing", Header: true})
	for _, act := range c.actions() {
		row(act)
	}
	if c.fam == input.FamilyGuitar {
		items = append(items, Item{Label: "Menus (on top of frets and strum)", Header: true})
		for _, act := range action.MenuSet {
			row(act)
		}
	}
	items = append(items,
		Item{Label: "Setup", Header: true},
		Item{Label: "Run the full setup", Hint: "Map and test every control in turn on one controller or the keyboard", Activate: func() {
			prof, fam, dev, back := c.prof, c.fam, c.dev, c.back
			a.SetScene(NewWizardFor(a, fam, prof, dev, func(a *App) { a.SetScene(newControls(a, fam, prof, dev, back)) }))
		}},
		Item{Label: "Reset everything to the defaults", Hint: "All of this instrument's controls for this player", Activate: func() {
			c.bindings(a).Profiles[c.fam] = input.DefaultProfile(c.fam)
			c.save(a)
			a.Toast("Back to the default controls")
		}},
	)
	c.list = List{Items: items, Visible: 11, Sel: min(sel, len(items)-1)}
}

func (c *Controls) save(a *App) {
	var err error
	if c.prof == a.Active {
		err = a.SaveBindings() // also refreshes menu navigation
	} else {
		err = c.bindings(a).Save(c.prof.BindingsPath())
	}
	if err != nil {
		a.Toast("Couldn't save the controls: " + err.Error())
	}
	c.refreshMenu(a)
}

func (c *Controls) startCapture(a *App, act action.Action, add bool) {
	c.cap = &bindCapture{act: act, add: add, started: clock.Now(), axes: map[input.ControlKey]*axisTrack{}, baseline: a.Input.AxisSnapshot()}
	c.card = nil
}

func (c *Controls) openCard(a *App, act action.Action) {
	p := c.profile(a)
	items := []Item{
		{Label: "Change it", Activate: func() { c.startCapture(a, act, false) }},
		{Label: "Add another control", Hint: "Keeps what's there", Activate: func() { c.startCapture(a, act, true) }},
	}
	for _, b := range p.Get(act) {
		b := b
		items = append(items, Item{Label: "Remove " + b.Short(), Activate: func() {
			p.Remove(act, b)
			c.save(a)
			c.openCard(a, act)
		}})
	}
	items = append(items,
		Item{Label: "Back to the default", Value: func() string {
			return truncateW(bindsText(input.DefaultProfile(c.fam).Get(act)), render.UI, 32, 360)
		}, Activate: func() {
			c.toDefault(a, act)
			c.save(a)
			c.card = nil
		}},
		Item{Label: "Close", Activate: func() { c.card = nil }},
	)
	c.card, c.cardAct = &List{Items: items, Visible: min(len(items), 9)}, act
}

// setFor is what an action's controls can clash with.
func (c *Controls) setFor(act action.Action) action.Set {
	if act.IsMenu() {
		return action.MenuSet
	}
	return c.actions()
}

// toDefault puts an action's default controls back, swapping with whatever
// has taken them.
func (c *Controls) toDefault(a *App, act action.Action) {
	p := c.profile(a)
	seen := map[string]bool{}
	for _, b := range input.DefaultProfile(c.fam).Get(act) {
		p.Assign(act, b, seen[b.Src], c.setFor(act), sharedOK)
		seen[b.Src] = true
	}
	for _, b := range p.Get(act) { // sources the default doesn't use
		if !seen[b.Src] {
			p.Remove(act, b)
		}
	}
}

func (c *Controls) Update(a *App) error {
	if c.menu != nil {
		var own []input.RawEvent
		for _, e := range a.Raw {
			if deviceOf(e) == *c.dev {
				own = append(own, e)
			}
		}
		a.MenuActs = c.menu.Feed(own, clock.Now())
	}
	if c.cap != nil {
		c.updateCapture(a)
		return nil
	}
	if c.card != nil {
		if !c.card.Update(a) && (a.Pressed(action.MenuBack) || a.Mouse.Right) {
			c.card = nil
		}
		return nil
	}
	if act, ok := c.acts[c.list.Sel]; ok && a.Pressed(action.MenuOptions) {
		c.openCard(a, act)
		return nil
	}
	if c.list.Update(a) {
		return nil
	}
	if a.Pressed(action.MenuBack) || a.Mouse.Right {
		c.back(a)
	}
	return nil
}

func (c *Controls) updateCapture(a *App) {
	k := c.cap
	now := clock.Now()
	if now-k.started > captureTime {
		c.cap = nil
		return
	}
	for _, e := range a.Raw {
		if e.Src == input.SrcKeyboard && e.Down && ebiten.Key(e.Code) == ebiten.KeyEscape {
			c.cap = nil
			return
		}
		if c.dev != nil && deviceOf(e) != *c.dev && e.Src != input.SrcKeyboard {
			continue
		}
		if c.fam == input.FamilyDrums && e.Src != input.SrcMIDI && e.Src != input.SrcKeyboard {
			continue
		}
		switch e.Kind {
		case input.KindButton, input.KindKey, input.KindNote:
			if !e.Down {
				continue
			}
			if k.act.IsAnalog() {
				k.warn = "The whammy needs something that moves, not a button"
				continue
			}
			if k.act.IsMenu() && e.Src == input.SrcKeyboard {
				k.warn = "Menus always use the arrow keys, so pick a controller button"
				continue
			}
			c.finish(a, bindingFromEvent(a, e))
			return
		case input.KindAxis, input.KindCC:
			if k.act.IsMenu() {
				continue
			}
			b, ok := k.step(e, now)
			if !ok {
				continue
			}
			if e.Src == input.SrcMIDI {
				b.DeviceName = a.Input.DeviceName(e.Src, e.Device)
			}
			c.finish(a, b)
			return
		}
	}
}

// step follows an axis until it's been moved and let go.
func (k *bindCapture) step(e input.RawEvent, now float64) (input.Binding, bool) {
	b, ok := trackAxis(k.axes, k.baseline, e, now)
	if ok && !k.act.IsAnalog() {
		b.Threshold = 0.55
	}
	return b, ok
}

// finish binds the control. A control in use by another action swaps with
// it; playing and menus don't clash, because they're never live together.
func (c *Controls) finish(a *App, b input.Binding) {
	act, add := c.cap.act, c.cap.add
	c.cap = nil
	p := c.profile(a)
	if other := p.Assign(act, b, add, c.setFor(act), sharedOK); other != action.None {
		a.Toast(fmt.Sprintf("%s is now %s, swapped with %s", b.Short(), act.Title(), other.Title()))
	} else {
		a.Toast(fmt.Sprintf("%s is now %s", b.Short(), act.Title()))
	}
	c.save(a)
}

func (c *Controls) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	Header(dst, "Controls", c.prof.Name+"  ·  "+c.famLabel())
	x, y, w := 120.0, 230.0, 1680.0
	GlassPanel(dst, x-40, y-24, w+80, 11*62+40)
	c.list.Draw(dst, x, y, w, 62)
	drawWrapped(dst, c.list.Hint(), render.UI, 26, x, y+11*62+36, w, th.TextDim)
	switch {
	case c.cap != nil:
		render.Dim(dst, render.RGBA(0, 0, 0, 0xC0))
		Panel(dst, 460, 340, 1000, 340)
		k := c.cap
		verb := "Press the control for"
		if k.add {
			verb = "Press another control for"
		}
		render.Text(dst, verb, render.UI, 34, 960, 380, th.TextDim, render.Center)
		render.Text(dst, k.act.Title(), render.Display, 64, 960, 436, th.Text, render.Center)
		render.Text(dst, "Now: "+truncateW(bindsText(c.profile(a).Get(k.act)), render.UI, 28, 900), render.UI, 28, 960, 540, th.TextDim, render.Center)
		left := math.Max(0, captureTime-(clock.Now()-k.started))
		note := fmt.Sprintf("Cancels by itself in %.0f s, or press Esc", math.Ceil(left))
		col := th.TextDim
		if k.warn != "" {
			note, col = k.warn, th.Late
		}
		render.Text(dst, note, render.Cond, 26, 960, 600, col, render.Center)
	case c.card != nil:
		render.Dim(dst, render.RGBA(0, 0, 0, 0xB8))
		h := 170 + float64(min(len(c.card.Items), 9))*66
		Panel(dst, 460, 540-h/2, 1000, h)
		render.Text(dst, c.cardAct.Title(), render.Display, 52, 500, 560-h/2, th.Text, render.Left)
		c.card.Draw(dst, 520, 540-h/2+110, 880, 66)
	}
	foot := "Green change     Yellow more     Left/right change the top rows     Red back"
	if c.cap != nil {
		foot = "Press any control on your controller or keyboard"
	}
	Footer(dst, foot)
}
