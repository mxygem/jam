package app

import (
	"testing"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/input"
)

// XInput bits as a CRKD guitar reports them (probed on real hardware).
const (
	bitDPadUp   = 0
	bitDPadDown = 1
	bitStart    = 4
	bitBack     = 5
	bitLB       = 8
	bitA        = 12
	bitB        = 13
	bitX        = 14
	bitY        = 15
	axisRX      = 2 // whammy, rests at -1
	axisRY      = 3 // tilt
)

func TestGuitarWizardWithCRKDEvents(t *testing.T) {
	h := newHarness(t)
	// Whammy rests fully negative on these guitars; the poller knows that
	// before the wizard starts.
	h.a.Input.Inject([]input.RawEvent{xaxis(axisRX, -1), xaxis(axisRY, 0)})
	w := NewWizard(h.a, input.FamilyGuitar)
	h.a.scene = w
	h.idle(0.2)

	h.tap(bitA) // pick the device
	if w.stage != stageBind {
		t.Fatalf("stage %v after picking the device", w.stage)
	}
	for _, bit := range []int{bitA, bitB, bitY, bitX, bitLB, bitDPadDown, bitDPadUp, bitBack} {
		h.tap(bit)
		h.idle(0.6)
	}
	h.sweep(axisRY, 0, 1, 1.7) // tilt and hold
	h.idle(0.6)
	h.sweep(axisRX, -1, 0.85, 0.2) // whammy
	h.idle(0.6)
	h.tap(bitStart)
	h.idle(0.6)
	h.tap(bitBack) // select shares the button with star power
	h.idle(0.6)
	if w.stage != stageTest {
		t.Fatalf("wizard stuck at step %d (%s), caps %+v", w.step, w.current().Title(), w.caps)
	}
	// Play on the test screen, then save with Start.
	h.tap(bitB)
	h.frame(xbtn(bitA, true), xbtn(bitDPadDown, true))
	h.idle(0.1)
	h.frame(xbtn(bitA, false), xbtn(bitDPadDown, false))
	h.tap(bitStart)

	p := h.a.Bindings.Profile(input.FamilyGuitar)
	want := map[action.Action]string{
		action.Green: "XInput A", action.Red: "XInput B", action.Yellow: "XInput Y", action.Blue: "XInput X",
		action.Orange: "XInput LB", action.StrumDown: "XInput DPadDown", action.StrumUp: "XInput DPadUp",
		action.StarPower: "XInput Back", action.Start: "XInput Start", action.Select: "XInput Back",
		action.Tilt: "XInput RightStickY +", action.Whammy: "XInput RightStickX +",
	}
	for act, desc := range want {
		var got []string
		for _, b := range p.Get(act) {
			if b.Src == "xinput" {
				got = append(got, b.Describe())
			}
		}
		if len(got) != 1 || got[0] != desc {
			t.Errorf("%s bound to %v, want [%s]", act.Title(), got, desc)
		}
	}
	for _, b := range p.Get(action.Whammy) {
		if b.Src == "xinput" && (b.Rest > -0.9 || b.Full < 0.8) {
			t.Errorf("whammy range %v..%v, want about -1..0.85", b.Rest, b.Full)
		}
	}
	// Keyboard bindings survive a controller mapping.
	if len(p.Get(action.Green)) != 2 {
		t.Errorf("green has %d bindings, want xinput + keyboard", len(p.Get(action.Green)))
	}
}

func TestGuitarWizardSkipsAndGoesBack(t *testing.T) {
	h := newHarness(t)
	w := NewWizard(h.a, input.FamilyGuitar)
	h.a.scene = w
	h.tap(bitA)
	h.tap(bitA) // green
	h.idle(0.6)
	esc := input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(keyEscape), Value: 1, Down: true}
	h.frame(esc) // back to green
	if w.step != 0 {
		t.Fatalf("step %d after Esc, want 0", w.step)
	}
	bs := input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(keyBackspace), Value: 1, Down: true}
	for i := 0; i < len(action.GuitarSet); i++ {
		h.frame(bs)
		h.idle(0.05)
	}
	if w.stage != stageTest {
		t.Fatalf("skipping every step should reach the test screen, at step %d", w.step)
	}
	// Optional steps time out on their own.
	h2 := newHarness(t)
	w2 := NewWizard(h2.a, input.FamilyGuitar)
	h2.a.scene = w2
	h2.tap(bitA)
	for _, bit := range []int{bitA, bitB, bitY, bitX, bitLB, bitDPadDown, bitDPadUp} {
		h2.tap(bit)
		h2.idle(0.6)
	}
	h2.idle(13) // star power: optional, times out
	if w2.current() != action.Tilt {
		t.Fatalf("at %s after the star power timeout, want Tilt", w2.current().Title())
	}
}
