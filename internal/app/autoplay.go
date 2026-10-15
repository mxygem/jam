package app

import (
	"sort"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
)

var fretActions = [5]action.Action{action.Green, action.Red, action.Yellow, action.Blue, action.Orange}

// botEvents generates a perfect performance of a track.
func botEvents(t *chart.Track, expertPlus bool) []action.Event {
	var evs []action.Event
	if t.Key.Inst.IsDrums() {
		for i := range t.Notes {
			n := &t.Notes[i]
			for l := 0; l < chart.NumLanes; l++ {
				if n.Lanes&(1<<l) == 0 {
					continue
				}
				if l == chart.DrumKick && n.DoubleKick && !expertPlus {
					continue
				}
				evs = append(evs, action.Event{Time: n.Time, Action: drumAction(t.DrumMode, l, n.Cymbals&(1<<l) != 0), Down: true, Value: 0.8})
			}
		}
		return evs
	}
	held := uint8(0)
	for i := range t.Notes {
		n := &t.Notes[i]
		want := n.Lanes & chart.FretMask
		press := n.Time - 0.012
		if i > 0 {
			prev := &t.Notes[i-1]
			// keep sustained frets down until the sustain ends
			release := press
			if e := prev.SustainEnd(); e > release && e < n.Time {
				release = e
			}
			if release > n.Time-0.002 {
				release = n.Time - 0.002
			}
			for l := 0; l < 5; l++ {
				bit := uint8(1) << l
				if held&bit != 0 && want&bit == 0 {
					evs = append(evs, action.Event{Time: release, Action: fretActions[l], Down: false})
					held &^= bit
				}
			}
		}
		for l := 0; l < 5; l++ {
			bit := uint8(1) << l
			if want&bit != 0 && held&bit == 0 {
				evs = append(evs, action.Event{Time: press, Action: fretActions[l], Down: true})
				held |= bit
			}
		}
		// Strum everything except HOPOs/taps that follow a hit; the strum
		// lenience would absorb it anyway, but fretting looks nicer.
		if n.Kind == chart.Strum || i == 0 {
			evs = append(evs, action.Event{Time: n.Time, Action: action.StrumDown, Down: true})
			evs = append(evs, action.Event{Time: n.Time + 0.04, Action: action.StrumDown, Down: false})
		}
		if n.StarPower && n.SPEnd {
			evs = append(evs, action.Event{Time: n.Time + 0.05, Action: action.StarPower, Down: true})
			evs = append(evs, action.Event{Time: n.Time + 0.1, Action: action.StarPower, Down: false})
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Time < evs[j].Time })
	return evs
}

func drumAction(mode chart.DrumMode, lane int, cym bool) action.Action {
	switch lane {
	case chart.DrumKick:
		return action.Kick
	case chart.DrumRed:
		return action.DrumRed
	case chart.DrumYellow:
		if cym {
			return action.CymYellow
		}
		return action.DrumYellow
	case chart.DrumBlue:
		if cym {
			return action.CymBlue
		}
		return action.DrumBlue
	case 4:
		if mode == chart.DrumsFiveLane {
			return action.DrumOrange
		}
		if cym {
			return action.CymGreen
		}
		return action.DrumGreen
	case chart.DrumGreen5:
		return action.DrumGreen
	}
	return action.None
}

// drumLaneOf maps a drum action back to a chart lane for pad animations.
func drumLaneOf(mode chart.DrumMode, a action.Action) int {
	switch a {
	case action.Kick:
		return chart.DrumKick
	case action.DrumRed:
		return chart.DrumRed
	case action.DrumYellow, action.CymYellow:
		return chart.DrumYellow
	case action.DrumBlue:
		return chart.DrumBlue
	case action.CymBlue:
		if mode == chart.DrumsFiveLane {
			return chart.DrumOrange
		}
		return chart.DrumBlue
	case action.DrumGreen:
		if mode == chart.DrumsFiveLane {
			return chart.DrumGreen5
		}
		return chart.DrumGreen4
	case action.CymGreen, action.DrumOrange:
		return 4
	}
	return -1
}
