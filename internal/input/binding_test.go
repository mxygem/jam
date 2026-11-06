package input

import (
	"testing"

	"github.com/mxygem/jam/internal/action"
)

func TestAssignSwapsAndShares(t *testing.T) {
	p := DefaultProfile(FamilyGuitar)
	red := p.Get(action.Red)[0]
	shared := func(x, y action.Action) bool { return x == action.StarPower && y == action.Select }
	if got := p.Assign(action.Green, red, false, action.GuitarSet, shared); got != action.Red {
		t.Fatalf("swapped with %s", got)
	}
	if p.Get(action.Green)[1].Code != 13 || p.Get(action.Red)[1].Code != 12 {
		t.Fatalf("green %v red %v", p.Get(action.Green), p.Get(action.Red))
	}
	// add keeps what's there and leaves the other action without it
	dl := Binding{Src: "xinput", Device: -1, Kind: "button", Code: 2}
	if got := p.Assign(action.StarPower, dl, true, action.GuitarSet, shared); got != action.None {
		t.Fatalf("swap %s", got)
	}
	if len(p.Get(action.StarPower)) != 3 {
		t.Fatalf("%v", p.Get(action.StarPower))
	}
	// shared pairs stay shared
	back := p.Get(action.Select)[0]
	if got := p.Assign(action.StarPower, back, false, action.GuitarSet, shared); got != action.None || len(p.Get(action.Select)) != 2 {
		t.Fatalf("shared: %s %v", got, p.Get(action.Select))
	}
	if n := p.Get(action.Green)[0].Short(); n != "Key A" {
		t.Fatalf("short %q", n)
	}
	if n := dl.Short(); n != "D-pad left" {
		t.Fatalf("short %q", n)
	}
}
