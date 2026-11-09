package app

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/render"
)

// The single player HUD in the layout editor: every item is on screen and
// can be picked, dragging the panel brings what's on it along, the star
// power meter stands up, and Esc saves a layout the song screen's HUD uses.
// With JAM_SHOTS it saves the editor and a screen with the layout.
func TestSoloHUDLayoutEditor(t *testing.T) {
	h := newHarness(t)
	a := h.a
	backed := false
	e := NewSoloHUDLayoutEditor(a, func() { backed = true })
	a.scene = e
	h.idle(0.2)
	h.frame()
	if !h.shots {
		e.Draw(h.screen, a)
	}
	boxes := e.seats[0].solo.Boxes
	for _, it := range render.SoloHUDItems {
		if _, ok := boxes[it.ID]; !ok {
			t.Fatalf("%s isn't in the editor", it.Label)
		}
	}
	h.shot("hudlayout-solo-editor")

	// drag the panel by a clear spot on it
	panel := boxes[render.HUDPanel]
	score := boxes[render.HUDScore]
	sl := e.slots[0]
	sc := sl.h / render.SeatH
	ox := sl.x + (sl.w-render.ScreenW*sc)/2
	cx, cy := ox+(panel.X+12)*sc, sl.y+(panel.Y+panel.H-6)*sc
	a.Mouse.X, a.Mouse.Y, a.Mouse.Click, a.Mouse.Down = cx, cy, true, true
	h.frame()
	a.Mouse.Click = false
	a.Mouse.X, a.Mouse.Y = cx-60, cy-200
	h.frame()
	a.Mouse.Down = false
	h.frame()
	if e.selID() != render.HUDPanel {
		t.Fatalf("selected %s", e.selID())
	}
	if !h.shots {
		e.Draw(h.screen, a)
	}
	got := e.seats[0].solo.Boxes
	if d := (got[render.HUDPanel].Y - panel.Y) * sc; math.Abs(d+200) > 2 {
		t.Fatalf("panel moved %.1f, want -200", d)
	}
	if d := got[render.HUDScore].Y - score.Y; math.Abs(d-(got[render.HUDPanel].Y-panel.Y)) > 0.01 {
		t.Fatalf("score moved %.1f with the panel", d)
	}

	// the panel can't take the score off the left edge
	e.layout.Set(render.HUDPanel, render.HUDPlace{})
	e.Draw(h.screen, a)
	e.nudge(-5000, 0)
	e.Draw(h.screen, a)
	if x := e.seats[0].solo.Boxes[render.HUDPanel].X; math.Abs(x) > 0.5 {
		t.Fatalf("panel at %.1f, want the left edge", x)
	}

	// the progress bar spans the screen: it only goes up and down
	e.sel = e.itemIndex(render.HUDProgress)
	e.nudge(100, 40)
	if p := e.layout.Get(render.HUDProgress); p.DX != 0 || math.Abs(p.DY*render.ScreenH-40) > 0.01 {
		t.Fatalf("progress place %+v", p)
	}

	// stand the meter up, and turning it back returns to the row
	before := boxes[render.HUDStarPower]
	e.turnMeter()
	h.frame()
	if !h.shots {
		e.Draw(h.screen, a)
	}
	box := e.seats[0].solo.Boxes[render.HUDStarPower]
	if box.H < box.W*3 {
		t.Fatalf("meter should stand up: %+v (was %+v)", box, before)
	}
	if box.X < 0 || box.Y < 0 || box.X+box.W > render.ScreenW {
		t.Fatalf("meter off screen: %+v", box)
	}
	h.shot("hudlayout-solo-moved")

	// Tab to the streak and hide it; reset the progress bar
	e.sel = e.itemIndex(render.HUDProgress)
	e.resetItem()
	for e.selID() != render.HUDStreak {
		h.key(ebiten.KeyTab)
	}
	h.key(keyEnter)
	if !e.layout.Get(render.HUDStreak).Hidden {
		t.Fatal("streak not hidden")
	}
	h.key(keyEscape)
	if !backed {
		t.Fatal("Esc didn't go back")
	}
	if a.Settings.SoloHUD == nil || !a.Settings.SoloHUD.Get(render.HUDStreak).Hidden {
		t.Fatalf("saved layout %+v", a.Settings.SoloHUD)
	}
	if len(a.Settings.SeatHUD) != 0 {
		t.Fatalf("multiplayer layout changed: %+v", a.Settings.SeatHUD)
	}

	// The song screen's HUD draws with it: no streak, panel moved.
	hc := render.DefaultHighway()
	hw := render.Highway{Cfg: hc}
	u := render.HUD{Highway: &hw, Layout: a.Settings.SoloHUD}
	d := render.HUD{Highway: &hw}
	hs := render.HUDState{SPEnergy: 0.5, BaseMax: 4, Mult: 1}
	u.Draw(h.screen, &hs)
	d.Draw(h.screen, &hs)
	if _, ok := u.Boxes[render.HUDStreak]; ok {
		t.Fatal("hidden streak was drawn")
	}
	if _, ok := d.Boxes[render.HUDStreak]; !ok {
		t.Fatal("default HUD has no streak")
	}
	if u.Boxes[render.HUDPanel].X >= d.Boxes[render.HUDPanel].X {
		t.Fatal("panel didn't move")
	}
}
