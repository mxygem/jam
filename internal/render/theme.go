package render

import "image/color"

// Theme holds the palette. Colors are premultiplied, as everything in
// render draws them. The Appearance settings change it in place (see
// ThemeSpec), so everything picks up a new look on the next frame.
type Theme struct {
	Lanes      [6]color.RGBA // guitar: green, red, yellow, blue, orange, open
	Drums      [6]color.RGBA // kick, red, yellow, blue, green(4-lane), green(5-lane)
	DrumOrange color.RGBA    // the 5-lane orange pad
	DoubleKick color.RGBA    // Expert+ kicks
	StarPower  color.RGBA
	Highway    color.RGBA // surface; how see-through it is is Look.HighwayOpacity
	SPHighway  color.RGBA // surface tint while star power is active
	BGGlow     color.RGBA // the glow behind the highway on the built-in background
	Rail       color.RGBA
	RailSP     color.RGBA
	Beat       color.RGBA
	BGTop      color.RGBA
	BGBottom   color.RGBA
	Accent     color.RGBA
	Text       color.RGBA
	TextDim    color.RGBA
	Panel      color.RGBA
	Early      color.RGBA
	Late       color.RGBA
	Good       color.RGBA
	Bad        color.RGBA
}

// DefaultTheme is a sleek take on the classic five-color highway.
var DefaultTheme = Theme{
	Lanes: [6]color.RGBA{
		{0x2B, 0xD6, 0x5A, 0xFF}, // green
		{0xF2, 0x3A, 0x3A, 0xFF}, // red
		{0xFF, 0xD0, 0x1F, 0xFF}, // yellow
		{0x2F, 0x86, 0xFF, 0xFF}, // blue
		{0xFF, 0x8A, 0x1F, 0xFF}, // orange
		{0xB0, 0x5C, 0xFF, 0xFF}, // open
	},
	Drums: [6]color.RGBA{
		{0xFF, 0x8A, 0x1F, 0xFF}, // kick
		{0xF2, 0x3A, 0x3A, 0xFF}, // red
		{0xFF, 0xD0, 0x1F, 0xFF}, // yellow
		{0x2F, 0x86, 0xFF, 0xFF}, // blue
		{0x2B, 0xD6, 0x5A, 0xFF}, // green (4-lane)
		{0x2B, 0xD6, 0x5A, 0xFF}, // green (5-lane)
	},
	DrumOrange: color.RGBA{0xFF, 0x8A, 0x1F, 0xFF},
	DoubleKick: color.RGBA{0xFF, 0x61, 0xA4, 0xFF},
	StarPower:  color.RGBA{0x7E, 0xE8, 0xFF, 0xFF},
	Highway:    color.RGBA{0x0C, 0x0F, 0x1A, 0xFF},
	SPHighway:  color.RGBA{0x10, 0x3A, 0x52, 0xFF},
	BGGlow:     color.RGBA{0xB8, 0x62, 0xFF, 0xFF},
	Rail:       color.RGBA{0xC8, 0xD2, 0xE6, 0xFF},
	RailSP:     color.RGBA{0x6A, 0xE0, 0xFF, 0xFF},
	Beat:       color.RGBA{0xFF, 0xFF, 0xFF, 0xFF},
	BGTop:      color.RGBA{0x06, 0x07, 0x10, 0xFF},
	BGBottom:   color.RGBA{0x14, 0x0C, 0x26, 0xFF},
	Accent:     color.RGBA{0xFF, 0x6A, 0x2B, 0xFF},
	Text:       color.RGBA{0xF2, 0xF4, 0xF8, 0xFF},
	TextDim:    color.RGBA{0x8C, 0x93, 0xA8, 0xFF},
	Panel:      color.RGBA{0x12, 0x15, 0x22, 0xE8},
	Early:      color.RGBA{0x5C, 0xB8, 0xFF, 0xFF},
	Late:       color.RGBA{0xFF, 0xA4, 0x4C, 0xFF},
	Good:       color.RGBA{0x4A, 0xE0, 0x8A, 0xFF},
	Bad:        color.RGBA{0xFF, 0x4D, 0x5E, 0xFF},
}

// HighwayConfig controls the highway's shape. All values are tunable from
// settings; this is the hook for the "interface tuning" phase.
type HighwayConfig struct {
	CenterX   float64 `json:"center_x"`
	StrikeY   float64 `json:"strike_y"`   // y of the strike line
	TopY      float64 `json:"top_y"`      // y of the far end
	NearWidth float64 `json:"near_width"` // width at the strike line
	FarRatio  float64 `json:"far_ratio"`  // far width / near width (lower = steeper angle)
	Lookahead float64 `json:"lookahead"`  // seconds of notes visible
}

// DefaultHighway is the out-of-the-box shape.
func DefaultHighway() HighwayConfig {
	return HighwayConfig{CenterX: ScreenW / 2, StrikeY: 945, TopY: 150, NearWidth: 640, FarRatio: 0.30, Lookahead: 1.05}
}
