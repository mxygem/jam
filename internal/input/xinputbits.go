package input

import (
	"fmt"
	"strings"
)

// xinputBitOrder is the order button changes are reported in when several
// change in one poll: frets first, then the rest.
var xinputBitOrder = []int{12, 13, 14, 15, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 10, 11}

// xinputButtonEvents turns a change in an XInput button mask into events.
// From 0 it presses everything held; to 0 it lets everything go.
func xinputButtonEvents(slot int, prev, cur uint16, t float64) []RawEvent {
	diff := prev ^ cur
	if diff == 0 {
		return nil
	}
	var out []RawEvent
	for _, bit := range xinputBitOrder {
		if diff&(1<<bit) == 0 {
			continue
		}
		down := cur&(1<<bit) != 0
		v := float32(0)
		if down {
			v = 1
		}
		out = append(out, RawEvent{Time: t, Src: SrcXInput, Device: slot, Kind: KindButton, Code: bit, Value: v, Down: down})
	}
	return out
}

// heldNote lists the buttons in a mask for the log (" holding A, LB"), or
// nothing when none are down.
func heldNote(mask uint16) string {
	var names []string
	for _, bit := range xinputBitOrder {
		if mask&(1<<bit) != 0 {
			names = append(names, xinputButtonNames[bit])
		}
	}
	if len(names) == 0 {
		return ""
	}
	return ", holding " + strings.Join(names, ", ")
}

func subtypeName(st uint8) string {
	switch st {
	case 0x01:
		return "gamepad"
	case 0x02:
		return "wheel"
	case 0x03:
		return "arcade stick"
	case 0x04:
		return "flight stick"
	case 0x05:
		return "dance pad"
	case 0x06, 0x07:
		return "guitar"
	case 0x08:
		return "drum kit"
	case 0x0B:
		return "bass guitar"
	case 0x13:
		return "arcade pad"
	}
	return fmt.Sprintf("subtype 0x%02X", st)
}
