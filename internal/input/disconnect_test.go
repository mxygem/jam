package input

import (
	"reflect"
	"testing"

	"github.com/mxygem/jam/internal/action"
)

func TestXInputButtonEvents(t *testing.T) {
	const a, lb, left = 1 << 12, 1 << 8, 1 << 2
	// Dropping off lets go of everything that was held, frets first.
	evs := xinputButtonEvents(0, a|lb|left, 0, 5)
	var codes []int
	for _, e := range evs {
		if e.Down || e.Src != SrcXInput || e.Time != 5 {
			t.Fatalf("release event wrong: %+v", e)
		}
		codes = append(codes, e.Code)
	}
	if !reflect.DeepEqual(codes, []int{12, 8, 2}) {
		t.Fatalf("released %v", codes)
	}
	// Coming back with a button down presses it.
	evs = xinputButtonEvents(1, 0, lb, 6)
	if len(evs) != 1 || !evs[0].Down || evs[0].Code != 8 || evs[0].Device != 1 {
		t.Fatalf("press on connect: %+v", evs)
	}
	if xinputButtonEvents(0, a, a, 7) != nil {
		t.Fatal("no change should give no events")
	}
	if got := heldNote(a | lb); got != ", holding A, LB" {
		t.Fatalf("heldNote = %q", got)
	}
	if heldNote(0) != "" {
		t.Fatal("heldNote of nothing should be empty")
	}
}

func TestPadReleases(t *testing.T) {
	p := &padState{buttons: []bool{false, true, false, true}}
	evs := padReleases(3, p, 2)
	if len(evs) != 2 || evs[0].Code != 1 || evs[1].Code != 3 || evs[0].Down || evs[0].Device != 3 {
		t.Fatalf("pad releases: %+v", evs)
	}
}

// A guitar that dropped while yellow was held must not leave yellow held in
// menus (the song list reads a held yellow as "show the shortcuts").
func TestMenuReleasedWhenControllerDrops(t *testing.T) {
	b := &Bindings{Version: 1, Profiles: map[Family]*Profile{}}
	g := DefaultProfile(FamilyGuitar)
	g.Bindings[action.Yellow.String()] = []Binding{xbtn(14)}
	b.Profiles[FamilyGuitar] = g
	mi := NewMenuInput(b, nil)
	mi.Feed(xinputButtonEvents(0, 0, 1<<14, 1), 1)
	if !mi.Held(action.MenuOptions) {
		t.Fatal("yellow should hold options")
	}
	mi.Feed(xinputButtonEvents(0, 1<<14, 0, 2), 2)
	if mi.Held(action.MenuOptions) {
		t.Fatal("options still held after the controller dropped")
	}
}

// D-pad left bound to Star Power, Select and menu left moves left in menus
// and doesn't also go back. Select on its own button still goes back.
func TestOwnMenuBindingWins(t *testing.T) {
	b := &Bindings{Version: 1, Profiles: map[Family]*Profile{}}
	g := DefaultProfile(FamilyGuitar)
	g.Bindings[action.StarPower.String()] = []Binding{xbtn(2)}
	g.Bindings[action.Select.String()] = []Binding{xbtn(2), xbtn(5)}
	b.Profiles[FamilyGuitar] = g
	mi := NewMenuInput(b, nil)
	press := func(bit int, t0 float64) []action.Action {
		acts := mi.Feed(xinputButtonEvents(0, 0, 1<<bit, t0), t0)
		mi.Feed(xinputButtonEvents(0, 1<<bit, 0, t0+0.01), t0+0.01)
		return acts
	}
	if got := press(2, 1); !reflect.DeepEqual(got, []action.Action{action.MenuLeft}) {
		t.Fatalf("d-pad left gave %v, want only menu left", got)
	}
	if got := press(5, 2); !reflect.DeepEqual(got, []action.Action{action.MenuBack}) {
		t.Fatalf("Back gave %v, want menu back", got)
	}
	// Strum down shares the d-pad with menu down: one menu down, not two.
	if got := press(1, 3); !reflect.DeepEqual(got, []action.Action{action.MenuDown}) {
		t.Fatalf("d-pad down gave %v", got)
	}
}
