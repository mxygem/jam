package render

import "math"

// The multiplayer scoreboard is made of items a player can move, hide and
// (for the star power meter) turn on its side in the HUD layout editor.
const (
	HUDName      = "name"       // color dot, name and part (top left)
	HUDScore     = "score"      // score and head to head rank (top right)
	HUDMult      = "multiplier" // multiplier ring
	HUDStreak    = "streak"     // streak count
	HUDStarPower = "star_power" // star power meter
	HUDRock      = "rock"       // rock meter
	HUDMessages  = "messages"   // pop-ups like SOLO! and STAR POWER READY
)

// HUDItems lists the movable items in the order the editor steps through
// them, with the names it shows.
var HUDItems = []struct{ ID, Label string }{
	{HUDName, "Name"},
	{HUDScore, "Score"},
	{HUDMult, "Multiplier"},
	{HUDStreak, "Streak"},
	{HUDStarPower, "Star power"},
	{HUDRock, "Rock meter"},
	{HUDMessages, "Messages"},
}

// HUDPlace is where one item sits relative to its default spot. DX is a
// fraction of the highway's near width and DY a fraction of the view's
// height, so a layout fits any number of players.
type HUDPlace struct {
	DX         float64 `json:"dx,omitempty"`
	DY         float64 `json:"dy,omitempty"`
	Hidden     bool    `json:"hidden,omitempty"`
	Horizontal bool    `json:"horizontal,omitempty"` // star power: a row of segments instead of a column
}

// IsDefault reports whether the place is the item's default.
func (p HUDPlace) IsDefault() bool { return p == HUDPlace{} }

// HUDLayout is the multiplayer scoreboard's layout by item id. Missing
// items sit at their defaults.
type HUDLayout map[string]HUDPlace

// Get returns an item's place.
func (l HUDLayout) Get(id string) HUDPlace { return l[id] }

// Set stores an item's place, dropping it when it is the default.
func (l HUDLayout) Set(id string, p HUDPlace) {
	if p.IsDefault() {
		delete(l, id)
		return
	}
	l[id] = p
}

// Clone copies the layout.
func (l HUDLayout) Clone() HUDLayout {
	out := HUDLayout{}
	for k, v := range l {
		out[k] = v
	}
	return out
}

// Box is an item's rectangle in view coordinates.
type Box struct{ X, Y, W, H float64 }

// Contains reports whether x, y is inside the box, grown by pad.
func (b Box) Contains(x, y, pad float64) bool {
	return x >= b.X-pad && x < b.X+b.W+pad && y >= b.Y-pad && y < b.Y+b.H+pad
}

// union grows b to cover o.
func (b Box) union(o Box) Box {
	if b.W == 0 && b.H == 0 {
		return o
	}
	x0, y0 := math.Min(b.X, o.X), math.Min(b.Y, o.Y)
	x1, y1 := math.Max(b.X+b.W, o.X+o.W), math.Max(b.Y+b.H, o.Y+o.H)
	return Box{x0, y0, x1 - x0, y1 - y0}
}
