package app

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

// HUDLayoutEditor arranges the multiplayer scoreboard: a band screen plays
// the preview chart and every item on it (name, score, multiplier, streak,
// star power, rock meter, messages) can be dragged with the mouse, nudged
// with arrows or a guitar, hidden, or (star power) turned on its side.
// Every highway shares the layout. Esc / red saves and goes back.
type HUDLayoutEditor struct {
	layout  render.HUDLayout
	players int
	sel     int // index into render.HUDItems
	pv      *lookPreview
	seats   []*hudSeat
	slots   []slot

	drag      bool
	dragSeat  int
	dragFrom  [2]float64 // seat coordinates where the drag started
	dragPlace render.HUDPlace
	dragBox   render.Box

	buttons []hudButton
	back    func()
}

type hudSeat struct {
	view *ebiten.Image
	hw   render.Highway
	hud  render.SeatHUD
	hc   render.HighwayConfig
	vw   float64
}

type hudButton struct {
	label string
	x, y  float64
	w, h  float64
	do    func()
}

const hudTop = 112.0 // the editor's bar; band play's is shorter, items scale with the view

// NewHUDLayoutEditor opens the editor; back runs when it closes.
func NewHUDLayoutEditor(a *App, back func()) *HUDLayoutEditor {
	e := &HUDLayoutEditor{layout: a.Settings.SeatHUD.Clone(), players: 2, pv: newLookPreview(false), back: back}
	e.sel = e.itemIndex(render.HUDStarPower)
	return e
}

func (e *HUDLayoutEditor) itemIndex(id string) int {
	for i, it := range render.HUDItems {
		if it.ID == id {
			return i
		}
	}
	return 0
}

func (e *HUDLayoutEditor) selID() string { return render.HUDItems[e.sel].ID }

func (e *HUDLayoutEditor) setPlayers(n int) {
	e.players = max(1, min(8, n))
	e.drag = false
}

func (e *HUDLayoutEditor) toggleHidden() {
	id := e.selID()
	p := e.layout.Get(id)
	p.Hidden = !p.Hidden
	e.layout.Set(id, p)
}

func (e *HUDLayoutEditor) turnMeter() {
	p := e.layout.Get(render.HUDStarPower)
	p.Horizontal = !p.Horizontal
	// The two shapes have different default spots: start the new one there.
	p.DX, p.DY = 0, 0
	e.layout.Set(render.HUDStarPower, p)
	e.sel = e.itemIndex(render.HUDStarPower)
}

func (e *HUDLayoutEditor) resetItem() {
	id := e.selID()
	p := e.layout.Get(id)
	e.layout.Set(id, render.HUDPlace{Horizontal: p.Horizontal})
}

func (e *HUDLayoutEditor) resetAll() { e.layout = render.HUDLayout{} }

func (e *HUDLayoutEditor) done(a *App) {
	a.Settings.SeatHUD = e.layout
	if len(e.layout) == 0 {
		a.Settings.SeatHUD = nil
	}
	_ = a.Settings.Save()
	a.Toast("Multiplayer HUD layout saved")
	e.back()
}

// nudge moves the selected item by dx, dy view pixels on the first
// highway's scale.
func (e *HUDLayoutEditor) nudge(dx, dy float64) {
	if len(e.seats) == 0 {
		return
	}
	s := e.seats[0]
	id := e.selID()
	p := e.layout.Get(id)
	if b, ok := s.hud.Boxes[id]; ok {
		dx = math.Max(-b.X, math.Min(s.vw-b.X-b.W, dx))
		dy = math.Max(-b.Y, math.Min(render.SeatH-b.Y-b.H, dy))
	}
	p.DX += dx / s.hc.NearWidth
	p.DY += dy / render.SeatH
	e.layout.Set(id, snapPlace(p, s.hc.NearWidth))
}

// snapPlace pulls an item back onto its default spot when it's close.
func snapPlace(p render.HUDPlace, nw float64) render.HUDPlace {
	if math.Abs(p.DX*nw) < 8 {
		p.DX = 0
	}
	if math.Abs(p.DY*render.SeatH) < 8 {
		p.DY = 0
	}
	return p
}

// toSeat converts canvas coordinates to seat i's view coordinates.
func (e *HUDLayoutEditor) toSeat(i int, x, y float64) (float64, float64) {
	sl, s := e.slots[i], e.seats[i]
	sc := sl.h / render.SeatH
	return (x - (sl.x + (sl.w-s.vw*sc)/2)) / sc, (y - sl.y) / sc
}

func (e *HUDLayoutEditor) Update(a *App) error {
	e.pv.update(a, clock.Now())
	e.layoutButtons(a)
	m := &a.Mouse

	// mouse: buttons, then picking up an item
	if m.Click {
		for _, b := range e.buttons {
			if m.In(b.x, b.y, b.w, b.h) {
				b.do()
				if a.scene != e {
					return nil
				}
				m.Click = false
				break
			}
		}
	}
	if m.Click && len(e.seats) == len(e.slots) {
		e.pick(m.X, m.Y)
	}
	if e.drag {
		if !m.Down {
			e.drag = false
		} else {
			e.dragTo(m.X, m.Y)
		}
	}

	// keys
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)
	step := 4.0
	if shift {
		step = 24
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyR):
		e.turnMeter()
	case inpututil.IsKeyJustPressed(ebiten.KeyH):
		e.toggleHidden()
	case inpututil.IsKeyJustPressed(ebiten.KeyDelete), inpututil.IsKeyJustPressed(ebiten.KeyBackspace):
		e.resetItem()
	}
	for k := ebiten.Key1; k <= ebiten.Key8; k++ {
		if inpututil.IsKeyJustPressed(k) {
			e.setPlayers(int(k-ebiten.Key1) + 1)
		}
	}
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp:
			e.nudge(0, -step)
		case action.MenuDown:
			e.nudge(0, step)
		case action.MenuLeft:
			e.nudge(-step, 0)
		case action.MenuRight:
			e.nudge(step, 0)
		case action.MenuOptions:
			n := len(render.HUDItems)
			if shift {
				e.sel = (e.sel + n - 1) % n
			} else {
				e.sel = (e.sel + 1) % n
			}
		case action.MenuConfirm:
			e.toggleHidden()
		case action.MenuBack:
			e.done(a)
			return nil
		}
	}
	return nil
}

// pick starts dragging the item under canvas point x, y. Smaller items
// win over the big ones they sit on.
func (e *HUDLayoutEditor) pick(x, y float64) {
	for i := range e.seats {
		sx, sy := e.toSeat(i, x, y)
		best, area := "", math.Inf(1)
		for _, it := range render.HUDItems {
			b, ok := e.seats[i].hud.Boxes[it.ID]
			if !ok || !b.Contains(sx, sy, 6) {
				continue
			}
			if b.W*b.H < area {
				best, area = it.ID, b.W*b.H
			}
		}
		if best != "" {
			e.sel = e.itemIndex(best)
			e.drag, e.dragSeat = true, i
			e.dragFrom = [2]float64{sx, sy}
			e.dragPlace = e.layout.Get(best)
			e.dragBox = e.seats[i].hud.Boxes[best]
			return
		}
	}
}

func (e *HUDLayoutEditor) dragTo(x, y float64) {
	if e.dragSeat >= len(e.seats) {
		e.drag = false
		return
	}
	s := e.seats[e.dragSeat]
	sx, sy := e.toSeat(e.dragSeat, x, y)
	dx, dy := sx-e.dragFrom[0], sy-e.dragFrom[1]
	b := e.dragBox
	dx = math.Max(-b.X, math.Min(s.vw-b.X-b.W, dx))
	dy = math.Max(-b.Y, math.Min(render.SeatH-b.Y-b.H, dy))
	p := e.dragPlace
	p.DX += dx / s.hc.NearWidth
	p.DY += dy / render.SeatH
	e.layout.Set(e.selID(), snapPlace(p, s.hc.NearWidth))
}

func (e *HUDLayoutEditor) layoutButtons(a *App) {
	meter := "Turn meter sideways (R)"
	if e.layout.Get(render.HUDStarPower).Horizontal {
		meter = "Stand meter up (R)"
	}
	hide := "Hide (H)"
	if e.layout.Get(e.selID()).Hidden {
		hide = "Show (H)"
	}
	labels := []struct {
		l  string
		do func()
	}{
		{"-", func() { e.setPlayers(e.players - 1) }},
		{fmt.Sprintf("%d players", e.players), func() { e.setPlayers(e.players%8 + 1) }},
		{"+", func() { e.setPlayers(e.players + 1) }},
		{hide, e.toggleHidden},
		{meter, e.turnMeter},
		{"Reset item (Del)", e.resetItem},
		{"Reset all", e.resetAll},
		{"Done (Esc)", func() { e.done(a) }},
	}
	e.buttons = e.buttons[:0]
	x := float64(render.ScreenW) - 24
	size := 24.0
	for i := len(labels) - 1; i >= 0; i-- {
		w, _ := render.Measure(labels[i].l, render.UIBold, size)
		w += 32
		x -= w
		e.buttons = append(e.buttons, hudButton{label: labels[i].l, x: x, y: 14, w: w, h: 44, do: labels[i].do})
		x -= 10
	}
	// reverse into reading order
	for i, j := 0, len(e.buttons)-1; i < j; i, j = i+1, j-1 {
		e.buttons[i], e.buttons[j] = e.buttons[j], e.buttons[i]
	}
}

func (e *HUDLayoutEditor) Draw(dst *ebiten.Image, a *App) {
	render.Background(dst, e.pv.frame.Now, false)
	e.slots = bandSlots(e.players, a.Settings.Multi.Layout, hudTop)
	for len(e.seats) < len(e.slots) {
		e.seats = append(e.seats, &hudSeat{})
	}
	e.seats = e.seats[:len(e.slots)]
	for i, sl := range e.slots {
		e.drawSeat(dst, a, i, sl)
	}

	// top bar
	render.FillRect(dst, 0, 0, render.ScreenW, hudTop, render.WithAlpha(th.Panel, 0.94))
	render.Text(dst, "Multiplayer HUD layout", render.Display, 40, 32, 12, th.Text, render.Left)
	help := fmt.Sprintf("Editing: %s.  Drag with the mouse, or Tab / yellow to pick and arrows / strum + blue/orange to nudge (Shift: faster).",
		render.HUDItems[e.sel].Label)
	render.Text(dst, help, render.UI, 22, 34, 74, th.TextDim, render.Left)
	for _, b := range e.buttons {
		hot := a.Mouse.In(b.x, b.y, b.w, b.h)
		c := render.WithAlpha(th.Highway, 0.9)
		if hot {
			c = render.WithAlpha(th.Accent, 0.9)
		}
		render.RoundRect(dst, b.x, b.y, b.w, b.h, 10, c)
		render.Text(dst, b.label, render.UIBold, 24, b.x+b.w/2, b.y+7, th.Text, render.Center)
	}
}

var hudSampleNames = []string{"Gem", "Player 2", "Player 3", "Player 4", "Player 5", "Player 6", "Player 7", "Player 8"}

func (e *HUDLayoutEditor) drawSeat(dst *ebiten.Image, a *App, i int, sl slot) {
	s := e.seats[i]
	vw := math.Max(200, math.Round(sl.w*render.SeatH/sl.h))
	if s.view == nil || s.view.Bounds().Dx() != int(vw) {
		if s.view != nil {
			s.view.Deallocate()
		}
		s.view = ebiten.NewImage(int(vw), int(render.SeatH))
	}
	s.vw = vw
	v := s.view
	v.Clear()
	s.hc = render.SeatHighway(a.Settings.Highway, vw)
	s.hw.Cfg = s.hc
	f := e.pv.frame
	s.hw.Draw(v, &f)

	hs := e.pv.hs
	hs.Score = hs.Score*float64(10+i)/10 + float64(i*1234)
	hs.Messages = []render.Message{{Text: "SOLO!", Color: th.Accent, At: hs.Now - 0.5}}
	hs.SPEnergy = 0.62
	hs.Rock = 0.7
	name := hudSampleNames[i%len(hudSampleNames)]
	if i == 0 && a.Active != nil {
		name = a.Active.Name
	}
	col, _ := render.ParseHex(profile.Palette[i%len(profile.Palette)])
	info := render.SeatInfo{Name: name, Color: col, Part: "Expert Guitar", ShowRock: true}
	s.hud.Layout = e.layout
	s.hud.ShowHidden = true
	s.hud.Draw(v, s.hc, &hs, &info)

	// outlines: every item faint, the one being edited bright, hidden ones crossed out
	k := math.Min(1.25, math.Max(0.45, s.hc.NearWidth/440))
	for idx, it := range render.HUDItems {
		b, ok := s.hud.Boxes[it.ID]
		if !ok {
			continue
		}
		pad := 6 * k
		x, y, w, h := b.X-pad, b.Y-pad, b.W+2*pad, b.H+2*pad
		if e.layout.Get(it.ID).Hidden {
			render.FillRect(v, x, y, w, h, render.RGBA(0, 0, 0, 0xB0))
			render.Text(v, "hidden", render.CondBold, 20*k, x+w/2, y+h/2-12*k, th.TextDim, render.Center)
		}
		if idx == e.sel {
			render.StrokeRect(v, x, y, w, h, 3*k, th.Accent)
			if i == 0 {
				render.Text(v, it.Label, render.CondBold, 20*k, x, y-26*k, th.Accent, render.Left)
			}
		} else {
			render.StrokeRect(v, x, y, w, h, 1.5*k, render.WithAlpha(th.Text, 0.35))
		}
	}

	sc := sl.h / render.SeatH
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(sc, sc)
	op.GeoM.Translate(sl.x+(sl.w-vw*sc)/2, sl.y)
	dst.DrawImage(v, op)
}
