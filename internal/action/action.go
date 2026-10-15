// Package action defines the logical inputs the game understands. Devices
// are bound to actions; the engine only ever sees actions.
package action

import "strings"

// Action is a logical input.
type Action uint8

const (
	None Action = iota

	// 5-fret guitar / bass
	Green
	Red
	Yellow
	Blue
	Orange
	StrumUp
	StrumDown
	StarPower
	Tilt
	Whammy // analog 0..1

	// drums
	Kick
	DrumRed
	DrumYellow
	DrumBlue
	DrumGreen
	DrumOrange // 5-lane kits
	CymYellow
	CymBlue
	CymGreen

	// shared
	Start
	Select

	// menu navigation (derived from the above or bound separately)
	MenuUp
	MenuDown
	MenuLeft
	MenuRight
	MenuConfirm
	MenuBack
	MenuOptions // song list options (sort, filters, playlists)

	Count
)

var names = [...]string{
	None: "none",

	Green: "green", Red: "red", Yellow: "yellow", Blue: "blue", Orange: "orange",
	StrumUp: "strum_up", StrumDown: "strum_down", StarPower: "star_power", Tilt: "tilt", Whammy: "whammy",

	Kick: "kick", DrumRed: "drum_red", DrumYellow: "drum_yellow", DrumBlue: "drum_blue", DrumGreen: "drum_green",
	DrumOrange: "drum_orange", CymYellow: "cym_yellow", CymBlue: "cym_blue", CymGreen: "cym_green",

	Start: "start", Select: "select",

	MenuUp: "menu_up", MenuDown: "menu_down", MenuLeft: "menu_left", MenuRight: "menu_right",
	MenuConfirm: "menu_confirm", MenuBack: "menu_back", MenuOptions: "menu_options",
}

var titles = [...]string{
	None: "None",

	Green: "Green fret", Red: "Red fret", Yellow: "Yellow fret", Blue: "Blue fret", Orange: "Orange fret",
	StrumUp: "Strum up", StrumDown: "Strum down", StarPower: "Star Power button", Tilt: "Tilt", Whammy: "Whammy",

	Kick: "Kick pedal", DrumRed: "Red pad (snare)", DrumYellow: "Yellow pad (tom)", DrumBlue: "Blue pad (tom)",
	DrumGreen: "Green pad (floor tom)", DrumOrange: "Orange pad", CymYellow: "Yellow cymbal (hi-hat)",
	CymBlue: "Blue cymbal (ride)", CymGreen: "Green cymbal (crash)",

	Start: "Start", Select: "Select",

	MenuUp: "Menu up", MenuDown: "Menu down", MenuLeft: "Menu left", MenuRight: "Menu right",
	MenuConfirm: "Menu confirm", MenuBack: "Menu back", MenuOptions: "Menu options",
}

func (a Action) String() string {
	if int(a) < len(names) && names[a] != "" {
		return names[a]
	}
	return "unknown"
}

// Title is a human readable label.
func (a Action) Title() string {
	if int(a) < len(titles) && titles[a] != "" {
		return titles[a]
	}
	return a.String()
}

// Parse converts a name back into an Action.
func Parse(s string) (Action, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range names {
		if n == s && n != "" {
			return Action(i), true
		}
	}
	return None, false
}

// IsAnalog reports whether the action carries a continuous value.
func (a Action) IsAnalog() bool { return a == Whammy }

// FretLane returns the guitar lane (0-4) for a fret action.
func (a Action) FretLane() (int, bool) {
	if a >= Green && a <= Orange {
		return int(a - Green), true
	}
	return 0, false
}

// IsStrum reports whether the action is a strum.
func (a Action) IsStrum() bool { return a == StrumUp || a == StrumDown }

// IsDrum reports whether the action is a drum pad/cymbal/kick.
func (a Action) IsDrum() bool { return a >= Kick && a <= CymGreen }

// Set is the set of actions an instrument's binding profile covers, in the
// order the setup wizard asks for them.
type Set []Action

// GuitarSet is used for guitar, bass, rhythm and keys.
var GuitarSet = Set{Green, Red, Yellow, Blue, Orange, StrumDown, StrumUp, StarPower, Tilt, Whammy, Start, Select}

// DrumSet is used for drums.
var DrumSet = Set{DrumRed, CymYellow, DrumYellow, CymBlue, DrumBlue, CymGreen, DrumGreen, Kick, DrumOrange, StarPower, Start, Select}

// Optional reports whether a wizard may skip binding the action.
func Optional(a Action) bool {
	switch a {
	case Tilt, Whammy, Select, StarPower, DrumOrange, CymYellow, CymBlue, CymGreen:
		return true
	}
	return false
}

// Event is a timestamped action change. Time is in seconds on the clock the
// consumer expects (song time for the engine).
type Event struct {
	Time   float64
	Action Action
	Down   bool    // pressed (digital) or above threshold (analog)
	Value  float32 // analog value 0..1 for Whammy; velocity 0..1 for drums
}
