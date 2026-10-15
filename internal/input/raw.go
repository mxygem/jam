// Package input turns device events (keyboard, XInput guitars, other
// gamepads, MIDI drum kits) into timestamped game actions.
package input

import (
	"fmt"
	"strings"
)

// Source is the kind of device an event came from.
type Source uint8

const (
	SrcKeyboard Source = iota
	SrcXInput
	SrcGamepad
	SrcMIDI
)

func (s Source) String() string {
	switch s {
	case SrcKeyboard:
		return "keyboard"
	case SrcXInput:
		return "xinput"
	case SrcGamepad:
		return "gamepad"
	case SrcMIDI:
		return "midi"
	}
	return "unknown"
}

// ParseSource is the inverse of String.
func ParseSource(s string) (Source, bool) {
	for _, src := range []Source{SrcKeyboard, SrcXInput, SrcGamepad, SrcMIDI} {
		if src.String() == s {
			return src, true
		}
	}
	return 0, false
}

// Kind is the kind of control.
type Kind uint8

const (
	KindButton Kind = iota
	KindAxis
	KindKey
	KindNote // MIDI note
	KindCC   // MIDI control change
)

func (k Kind) String() string {
	switch k {
	case KindButton:
		return "button"
	case KindAxis:
		return "axis"
	case KindKey:
		return "key"
	case KindNote:
		return "note"
	case KindCC:
		return "cc"
	}
	return "unknown"
}

// ParseKind is the inverse of String.
func ParseKind(s string) (Kind, bool) {
	for _, k := range []Kind{KindButton, KindAxis, KindKey, KindNote, KindCC} {
		if k.String() == s {
			return k, true
		}
	}
	return 0, false
}

// RawEvent is a change on one control of one device.
type RawEvent struct {
	Time   float64 // clock.Now() seconds
	Src    Source
	Device int // XInput slot, Ebiten gamepad id, MIDI port index
	Kind   Kind
	Code   int     // button bit/index, axis index, key code, MIDI note/CC number
	Value  float32 // axis -1..1 (triggers 0..1), 1/0 for buttons, velocity 0..1 for MIDI
	Down   bool
}

// ControlKey identifies a control independent of its state.
type ControlKey struct {
	Src    Source
	Device int
	Kind   Kind
	Code   int
}

// Key returns the event's control identity.
func (e RawEvent) Key() ControlKey { return ControlKey{e.Src, e.Device, e.Kind, e.Code} }

// XInput button bit names.
var xinputButtonNames = [16]string{"DPadUp", "DPadDown", "DPadLeft", "DPadRight", "Start", "Back", "LeftThumb", "RightThumb", "LB", "RB", "Guide", "Bit11", "A", "B", "X", "Y"}

// XInput axis names in the order the poller reports them.
var xinputAxisNames = [6]string{"LeftStickX", "LeftStickY", "RightStickX", "RightStickY", "LeftTrigger", "RightTrigger"}

// ControlName is a human readable name for a control.
func ControlName(src Source, kind Kind, code int, keyName func(int) string) string {
	switch src {
	case SrcXInput:
		switch kind {
		case KindButton:
			if code >= 0 && code < 16 {
				return xinputButtonNames[code]
			}
		case KindAxis:
			if code >= 0 && code < 6 {
				return xinputAxisNames[code]
			}
		}
	case SrcKeyboard:
		if keyName != nil {
			return keyName(code)
		}
	case SrcMIDI:
		switch kind {
		case KindNote:
			return fmt.Sprintf("Note %d (%s)", code, noteName(code))
		case KindCC:
			return fmt.Sprintf("CC %d", code)
		}
	}
	return fmt.Sprintf("%s %d", strings.Title(kind.String()), code)
}

func noteName(n int) string {
	names := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	if n < 0 || n > 127 {
		return "?"
	}
	if gm, ok := gmDrumNames[n]; ok {
		return gm
	}
	return fmt.Sprintf("%s%d", names[n%12], n/12-1)
}

// General MIDI percussion key map names.
var gmDrumNames = map[int]string{
	35: "Acoustic Bass Drum", 36: "Bass Drum", 37: "Side Stick", 38: "Snare", 39: "Hand Clap", 40: "Electric Snare",
	41: "Low Floor Tom", 42: "Closed Hi-Hat", 43: "High Floor Tom", 44: "Pedal Hi-Hat", 45: "Low Tom", 46: "Open Hi-Hat",
	47: "Low-Mid Tom", 48: "Hi-Mid Tom", 49: "Crash 1", 50: "High Tom", 51: "Ride 1", 52: "China", 53: "Ride Bell",
	55: "Splash", 57: "Crash 2", 59: "Ride 2",
}

// DeviceInfo describes a connected device.
type DeviceInfo struct {
	Src    Source
	Index  int
	Name   string
	Detail string // e.g. "guitar (subtype 0x07)"
}

func (d DeviceInfo) String() string {
	if d.Detail != "" {
		return fmt.Sprintf("%s (%s)", d.Name, d.Detail)
	}
	return d.Name
}
