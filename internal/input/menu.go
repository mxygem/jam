package input

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
)

// MenuInput turns any device into menu navigation: arrows/Enter/Escape on
// the keyboard, strum/green/red on a guitar, and yellow/blue/green/red pads on
// a drum kit (Rock Band style).
type MenuInput struct {
	guitar *Mapper
	drums  *Mapper

	held      [action.Count]bool
	heldSince [action.Count]float64
	lastRep   [action.Count]float64
}

// NewMenuInput builds menu navigation from the current profiles. Keyboard
// bindings in the profiles are ignored here so typing doesn't navigate.
func NewMenuInput(b *Bindings, devices []DeviceInfo) *MenuInput {
	strip := func(p *Profile) *Profile {
		q := &Profile{Family: p.Family, Bindings: map[string][]Binding{}}
		for k, bs := range p.Bindings {
			for _, bd := range bs {
				if bd.Src != "keyboard" {
					q.Bindings[k] = append(q.Bindings[k], bd)
				}
			}
		}
		return q
	}
	return &MenuInput{
		guitar: NewMapper(strip(b.Profile(FamilyGuitar)), devices),
		drums:  NewMapper(strip(b.Profile(FamilyDrums)), devices),
	}
}

var guitarMenu = map[action.Action]action.Action{
	action.StrumUp: action.MenuUp, action.StrumDown: action.MenuDown,
	action.Green: action.MenuConfirm, action.Start: action.MenuConfirm,
	action.Red: action.MenuBack, action.Select: action.MenuBack,
	action.Blue: action.MenuLeft, action.Orange: action.MenuRight,
	action.Yellow: action.MenuOptions,
}

var drumMenu = map[action.Action]action.Action{
	action.DrumYellow: action.MenuUp, action.CymYellow: action.MenuUp,
	action.DrumBlue: action.MenuDown, action.CymBlue: action.MenuDown,
	action.DrumGreen: action.MenuConfirm, action.CymGreen: action.MenuConfirm,
	action.DrumRed: action.MenuBack,
}

var keyMenu = map[ebiten.Key]action.Action{
	ebiten.KeyArrowUp: action.MenuUp, ebiten.KeyArrowDown: action.MenuDown,
	ebiten.KeyArrowLeft: action.MenuLeft, ebiten.KeyArrowRight: action.MenuRight,
	ebiten.KeyEnter: action.MenuConfirm, ebiten.KeyNumpadEnter: action.MenuConfirm,
	ebiten.KeyEscape: action.MenuBack,
	ebiten.KeyPageUp: action.MenuLeft, ebiten.KeyPageDown: action.MenuRight,
	ebiten.KeyTab: action.MenuOptions,
}

// Feed converts raw events into menu actions (presses only).
func (mi *MenuInput) Feed(evs []RawEvent, now float64) []action.Action {
	var out []action.Action
	press := func(a action.Action, down bool, t float64) {
		if down {
			if !mi.held[a] {
				out = append(out, a)
			}
			mi.held[a] = true
			mi.heldSince[a] = t
			mi.lastRep[a] = t
		} else {
			mi.held[a] = false
		}
	}
	var buf []Mapped
	for _, e := range evs {
		if e.Src == SrcKeyboard {
			if a, ok := keyMenu[ebiten.Key(e.Code)]; ok {
				press(a, e.Down, e.Time)
			}
			continue
		}
		buf = mi.guitar.Map(e, buf[:0])
		for _, m := range buf {
			if a, ok := guitarMenu[m.Action]; ok {
				press(a, m.Down, m.Time)
			}
		}
		buf = mi.drums.Map(e, buf[:0])
		for _, m := range buf {
			if a, ok := drumMenu[m.Action]; ok {
				// Pads are hits, not holds.
				out = append(out, a)
			}
		}
	}
	// Auto-repeat for held directions.
	for _, a := range []action.Action{action.MenuUp, action.MenuDown, action.MenuLeft, action.MenuRight} {
		if mi.held[a] && now-mi.heldSince[a] > 0.35 && now-mi.lastRep[a] > 0.055 {
			mi.lastRep[a] = now
			out = append(out, a)
		}
	}
	return out
}
