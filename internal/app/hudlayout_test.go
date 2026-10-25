package app

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/render"
)

// The HUD layout editor: drag the star power meter with the mouse, nudge
// it with arrows, turn it, hide the streak, then Esc saves the layout and
// band play's highways use it. With JAM_SHOTS it saves the editor and a
// band screen with the layout.
func TestHUDLayoutEditor(t *testing.T) {
	h := newHarness(t)
	a := h.a
	backed := false
	e := NewHUDLayoutEditor(a, func() { backed = true })
	a.scene = e
	h.idle(0.2)
	h.frame() // draw once so the boxes exist
	if !h.shots {
		e.Draw(h.screen, a)
	}
	box, ok := e.seats[0].hud.Boxes[render.HUDStarPower]
	if !ok {
		t.Fatal("no star power box")
	}
	if box.H < box.W*3 {
		t.Fatalf("star power meter should stand up by default: %+v", box)
	}
	h.shot("hudlayout-editor")

	// drag it 100 px (canvas) left and 60 px down
	sl := e.slots[0]
	sc := sl.h / render.SeatH
	ox := sl.x + (sl.w-e.seats[0].vw*sc)/2
	cx, cy := ox+(box.X+box.W/2)*sc, sl.y+(box.Y+box.H/2)*sc
	a.Mouse.X, a.Mouse.Y, a.Mouse.Click, a.Mouse.Down = cx, cy, true, true
	h.frame()
	a.Mouse.Click = false
	for i := 1; i <= 10; i++ {
		a.Mouse.X, a.Mouse.Y = cx-10*float64(i), cy+6*float64(i)
		h.frame()
	}
	a.Mouse.Down = false
	h.frame()
	if e.selID() != render.HUDStarPower {
		t.Fatalf("selected %s", e.selID())
	}
	p := e.layout.Get(render.HUDStarPower)
	nw := e.seats[0].hc.NearWidth
	if got := p.DX * nw * sc; math.Abs(got+100) > 2 {
		t.Fatalf("dragged %.1f px across, want -100", got)
	}
	if got := p.DY * render.SeatH * sc; math.Abs(got-60) > 2 {
		t.Fatalf("dragged %.1f px down, want 60", got)
	}

	// arrows nudge 4 view px
	before := e.layout.Get(render.HUDStarPower).DY
	h.key(keyArrowDown)
	if d := (e.layout.Get(render.HUDStarPower).DY - before) * render.SeatH; math.Abs(d-4) > 0.01 {
		t.Fatalf("nudged %.2f px, want 4", d)
	}

	// Tab to the streak and hide it with Enter
	for e.selID() != render.HUDStreak {
		h.key(ebiten.KeyTab)
	}
	h.key(keyEnter)
	if !e.layout.Get(render.HUDStreak).Hidden {
		t.Fatal("streak not hidden")
	}
	e.turnMeter()
	if p := e.layout.Get(render.HUDStarPower); !p.Horizontal || p.DX != 0 || p.DY != 0 {
		t.Fatalf("turned meter %+v", p)
	}
	e.turnMeter()
	e.sel = e.itemIndex(render.HUDStarPower)
	e.layout.Set(render.HUDStarPower, render.HUDPlace{DX: -0.2})

	h.key(keyEscape)
	if !backed {
		t.Fatal("Esc didn't go back")
	}
	got := a.Settings.SeatHUD
	if !got.Get(render.HUDStreak).Hidden || got.Get(render.HUDStarPower).DX != -0.2 {
		t.Fatalf("saved layout %+v", got)
	}

	// A seat draws with the saved layout: no streak box, meter moved.
	var u render.SeatHUD
	u.Layout = got
	hc := render.SeatHighway(render.DefaultHighway(), 960)
	u.Draw(h.screen, hc, &render.HUDState{SPEnergy: 0.5}, &render.SeatInfo{Name: "Gem"})
	if _, ok := u.Boxes[render.HUDStreak]; ok {
		t.Fatal("hidden streak was drawn")
	}
	var d render.SeatHUD
	d.Draw(h.screen, hc, &render.HUDState{SPEnergy: 0.5}, &render.SeatInfo{Name: "Gem"})
	if dx := u.Boxes[render.HUDStarPower].X - d.Boxes[render.HUDStarPower].X; math.Abs(dx+0.2*hc.NearWidth) > 0.5 {
		t.Fatalf("meter moved %.1f, want %.1f", dx, -0.2*hc.NearWidth)
	}
}
