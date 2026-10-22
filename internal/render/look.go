package render

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The look of the game is a Theme (colors) plus a Look (what notes are
// made of, how the highway is drawn). A ThemeSpec is both written down in
// a form people can read and edit: built-in themes are ThemeSpecs, and so
// are theme files in the themes folder and the player's custom look.

// Material is what gems and strike-line buttons look like they're made of.
type Material uint8

const (
	MatClassic Material = iota // lit plastic with a metal rim and dark cap
	MatGlossy                  // candy: bright, shiny, light cap
	MatMatte                   // soft, no shine
	MatMetal                   // brushed metal body, chrome rim and cap
	MatGlass                   // see-through with bright edges
	MatNeon                    // dark body with a glowing outline
	MatFlat                    // flat 2D discs
	numMaterials
)

var materialNames = [numMaterials]struct{ id, label string }{
	{"classic", "Classic"},
	{"glossy", "Glossy"},
	{"matte", "Matte"},
	{"metal", "Metal"},
	{"glass", "Glass"},
	{"neon", "Neon"},
	{"flat", "Flat"},
}

func (m Material) String() string { return materialNames[m%numMaterials].id }
func (m Material) Label() string  { return materialNames[m%numMaterials].label }

// ParseMaterial reads a material id (unknown ids give Classic).
func ParseMaterial(s string) Material {
	for i, n := range materialNames {
		if n.id == strings.ToLower(strings.TrimSpace(s)) {
			return Material(i)
		}
	}
	return MatClassic
}

// Rim is the ring around the top of a gem.
type Rim uint8

const (
	RimSilver Rim = iota
	RimGold
	RimBlack
	RimWhite
	RimLane // the note's own color
	numRims
)

var rimNames = [numRims]struct{ id, label string }{
	{"silver", "Silver"},
	{"gold", "Gold"},
	{"black", "Black"},
	{"white", "White"},
	{"lane", "Note color"},
}

func (r Rim) String() string { return rimNames[r%numRims].id }
func (r Rim) Label() string  { return rimNames[r%numRims].label }

// ParseRim reads a rim id (unknown ids give Silver).
func ParseRim(s string) Rim {
	for i, n := range rimNames {
		if n.id == strings.ToLower(strings.TrimSpace(s)) {
			return Rim(i)
		}
	}
	return RimSilver
}

// rimColors returns the rim's main and shaded colors for a note color.
func (r Rim) colors(c color.RGBA) (main, dark color.RGBA) {
	switch r {
	case RimGold:
		return color.RGBA{0xF2, 0xC9, 0x5C, 0xFF}, color.RGBA{0x8A, 0x60, 0x1C, 0xFF}
	case RimBlack:
		return color.RGBA{0x30, 0x32, 0x3A, 0xFF}, color.RGBA{0x0A, 0x0A, 0x0E, 0xFF}
	case RimWhite:
		return color.RGBA{0xFA, 0xFA, 0xFC, 0xFF}, color.RGBA{0xB8, 0xBC, 0xC8, 0xFF}
	case RimLane:
		return Lighten(c, 0.35), Darken(c, 0.35)
	}
	return silver, silverDark
}

// HighwayStyle is how the highway surface is painted.
type HighwayStyle uint8

const (
	HwSolid    HighwayStyle = iota
	HwGradient              // lighter near the strike line, darker far away
	HwLanes                 // each lane faintly tinted in its note color
	HwGlass                 // see-through with a sheen along the rails
	numHighwayStyles
)

var highwayStyleNames = [numHighwayStyles]struct{ id, label string }{
	{"solid", "Solid"},
	{"gradient", "Gradient"},
	{"lanes", "Lane colors"},
	{"glass", "Glass"},
}

func (h HighwayStyle) String() string { return highwayStyleNames[h%numHighwayStyles].id }
func (h HighwayStyle) Label() string  { return highwayStyleNames[h%numHighwayStyles].label }

// ParseHighwayStyle reads a highway style id (unknown ids give Solid).
func ParseHighwayStyle(s string) HighwayStyle {
	for i, n := range highwayStyleNames {
		if n.id == strings.ToLower(strings.TrimSpace(s)) {
			return HighwayStyle(i)
		}
	}
	return HwSolid
}

// Look is everything about the notes and highway that isn't a color.
type Look struct {
	Material       Material
	Rim            Rim
	Highway        HighwayStyle
	HighwayOpacity float64 // 0..1
	BeatLines      float64 // brightness of the beat lines, 1 is normal
	LaneLines      float64 // brightness of the lines between lanes, 1 is normal
	// Texture is painted on the highway ("" none, a built-in id, or
	// "custom:<image>"); TextureStrength is how much it shows, 0..1.
	Texture         string
	TextureStrength float64
	NoteSize        float64 // gem size, 1 is normal (a preference, not part of themes)
}

// DefaultLook is the out-of-the-box look.
func DefaultLook() Look {
	return Look{Material: MatClassic, Rim: RimSilver, Highway: HwSolid, HighwayOpacity: 0.88, BeatLines: 1, LaneLines: 1, TextureStrength: 0.8, NoteSize: 1}
}

// CurrentLook is the look in use. Change it with SetLook so cached note
// sprites are redrawn.
var CurrentLook = DefaultLook()

// SetLook changes the look.
func SetLook(l Look) {
	if l.NoteSize <= 0 {
		l.NoteSize = 1
	}
	if l != CurrentLook {
		CurrentLook = l
		clearSprites()
		PrewarmTexture(l.Texture)
	}
}

// jamTheme is the built-in palette, kept apart from DefaultTheme (which
// the settings change).
var jamTheme = DefaultTheme

// BaseTheme returns the built-in palette.
func BaseTheme() Theme { return jamTheme }

// ColorSlot is one color a theme sets.
type ColorSlot struct {
	ID, Label, Group string
	// Ptrs are the theme fields the slot sets (drum green is two).
	ptrs func(*Theme) []*color.RGBA
	// follow is a slot this one copies when a theme doesn't set it.
	follow string
}

// Get returns the slot's color in t (straight, not premultiplied).
func (s ColorSlot) Get(t *Theme) color.RGBA { return Straight(*s.ptrs(t)[0]) }

// Set sets the slot's color in t, keeping the field's alpha.
func (s ColorSlot) Set(t *Theme, c color.RGBA) {
	for _, p := range s.ptrs(t) {
		a := Straight(*p).A
		c.A = a
		*p = Premul(c)
	}
}

func one(f func(*Theme) *color.RGBA) func(*Theme) []*color.RGBA {
	return func(t *Theme) []*color.RGBA { return []*color.RGBA{f(t)} }
}

// Color groups, in the order the settings show them.
const (
	GroupGuitar   = "Guitar and bass notes"
	GroupDrums    = "Drum notes"
	GroupHighway  = "Highway"
	GroupFeedback = "Star power and timing"
	GroupMenus    = "Menus and screens"
)

// ColorSlots lists every color a theme sets.
var ColorSlots = []ColorSlot{
	{ID: "green", Label: "Green", Group: GroupGuitar, ptrs: one(func(t *Theme) *color.RGBA { return &t.Lanes[0] })},
	{ID: "red", Label: "Red", Group: GroupGuitar, ptrs: one(func(t *Theme) *color.RGBA { return &t.Lanes[1] })},
	{ID: "yellow", Label: "Yellow", Group: GroupGuitar, ptrs: one(func(t *Theme) *color.RGBA { return &t.Lanes[2] })},
	{ID: "blue", Label: "Blue", Group: GroupGuitar, ptrs: one(func(t *Theme) *color.RGBA { return &t.Lanes[3] })},
	{ID: "orange", Label: "Orange", Group: GroupGuitar, ptrs: one(func(t *Theme) *color.RGBA { return &t.Lanes[4] })},
	{ID: "open", Label: "Open notes", Group: GroupGuitar, ptrs: one(func(t *Theme) *color.RGBA { return &t.Lanes[5] })},

	{ID: "kick", Label: "Kick", Group: GroupDrums, follow: "orange", ptrs: one(func(t *Theme) *color.RGBA { return &t.Drums[0] })},
	{ID: "drum_red", Label: "Red pad", Group: GroupDrums, follow: "red", ptrs: one(func(t *Theme) *color.RGBA { return &t.Drums[1] })},
	{ID: "drum_yellow", Label: "Yellow pad", Group: GroupDrums, follow: "yellow", ptrs: one(func(t *Theme) *color.RGBA { return &t.Drums[2] })},
	{ID: "drum_blue", Label: "Blue pad", Group: GroupDrums, follow: "blue", ptrs: one(func(t *Theme) *color.RGBA { return &t.Drums[3] })},
	{ID: "drum_green", Label: "Green pad", Group: GroupDrums, follow: "green", ptrs: func(t *Theme) []*color.RGBA { return []*color.RGBA{&t.Drums[4], &t.Drums[5]} }},
	{ID: "drum_orange", Label: "Orange pad (5-lane)", Group: GroupDrums, follow: "orange", ptrs: one(func(t *Theme) *color.RGBA { return &t.DrumOrange })},
	{ID: "double_kick", Label: "Expert+ kick", Group: GroupDrums, ptrs: one(func(t *Theme) *color.RGBA { return &t.DoubleKick })},

	{ID: "highway", Label: "Highway", Group: GroupHighway, ptrs: one(func(t *Theme) *color.RGBA { return &t.Highway })},
	{ID: "sp_highway", Label: "Highway in star power", Group: GroupHighway, ptrs: one(func(t *Theme) *color.RGBA { return &t.SPHighway })},
	{ID: "rails", Label: "Rails and strike line", Group: GroupHighway, ptrs: one(func(t *Theme) *color.RGBA { return &t.Rail })},
	{ID: "rails_sp", Label: "Rails in star power", Group: GroupHighway, ptrs: one(func(t *Theme) *color.RGBA { return &t.RailSP })},
	{ID: "beat", Label: "Beat and lane lines", Group: GroupHighway, ptrs: one(func(t *Theme) *color.RGBA { return &t.Beat })},

	{ID: "star_power", Label: "Star power", Group: GroupFeedback, ptrs: one(func(t *Theme) *color.RGBA { return &t.StarPower })},
	{ID: "good", Label: "Good hits", Group: GroupFeedback, ptrs: one(func(t *Theme) *color.RGBA { return &t.Good })},
	{ID: "early", Label: "Early", Group: GroupFeedback, ptrs: one(func(t *Theme) *color.RGBA { return &t.Early })},
	{ID: "late", Label: "Late", Group: GroupFeedback, ptrs: one(func(t *Theme) *color.RGBA { return &t.Late })},
	{ID: "bad", Label: "Misses", Group: GroupFeedback, ptrs: one(func(t *Theme) *color.RGBA { return &t.Bad })},

	{ID: "accent", Label: "Accent", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.Accent })},
	{ID: "text", Label: "Text", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.Text })},
	{ID: "text_dim", Label: "Dim text", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.TextDim })},
	{ID: "panel", Label: "Panels", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.Panel })},
	{ID: "bg_top", Label: "Background top", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.BGTop })},
	{ID: "bg_bottom", Label: "Background bottom", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.BGBottom })},
	{ID: "bg_glow", Label: "Background glow", Group: GroupMenus, ptrs: one(func(t *Theme) *color.RGBA { return &t.BGGlow })},
}

// Slot finds a color slot by id.
func Slot(id string) (ColorSlot, bool) {
	for _, s := range ColorSlots {
		if s.ID == id {
			return s, true
		}
	}
	return ColorSlot{}, false
}

// Straight undoes premultiplied alpha.
func Straight(c color.RGBA) color.RGBA {
	if c.A == 0xFF || c.A == 0 {
		return c
	}
	f := func(v uint8) uint8 { return uint8(math.Min(255, math.Round(float64(v)*255/float64(c.A)))) }
	return color.RGBA{f(c.R), f(c.G), f(c.B), c.A}
}

// Premul premultiplies a straight color.
func Premul(c color.RGBA) color.RGBA {
	if c.A == 0xFF {
		return c
	}
	f := func(v uint8) uint8 { return uint8(math.Round(float64(v) * float64(c.A) / 255)) }
	return color.RGBA{f(c.R), f(c.G), f(c.B), c.A}
}

// Hex formats a straight color as #RRGGBB (or #RRGGBBAA when see-through).
func Hex(c color.RGBA) string {
	if c.A == 0xFF {
		return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02X%02X%02X%02X", c.R, c.G, c.B, c.A)
}

// ParseHex reads #RGB, #RRGGBB or #RRGGBBAA (the # is optional).
func ParseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 && len(s) != 8 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	if len(s) == 6 {
		return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}, true
	}
	return color.RGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}

// HSV converts a straight color to hue (0..360), saturation and value (0..1).
func HSV(c color.RGBA) (h, s, v float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	v = mx
	d := mx - mn
	if mx > 0 {
		s = d / mx
	}
	if d == 0 {
		return 0, s, v
	}
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, v
}

// FromHSV builds an opaque color from hue (degrees), saturation and value.
func FromHSV(h, s, v float64) color.RGBA {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	s, v = math.Max(0, math.Min(1, s)), math.Max(0, math.Min(1, v))
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	f := func(x float64) uint8 { return uint8(math.Round((x + m) * 255)) }
	return color.RGBA{f(r), f(g), f(b), 0xFF}
}

// ThemeSpec is a theme written down: built in, in a theme file, or the
// player's custom look. Missing colors and settings come from the Jam
// theme (drum colors from the matching guitar colors).
type ThemeSpec struct {
	Name            string            `json:"name"`
	Base            string            `json:"base,omitempty"` // custom looks: the theme they started from
	Colors          map[string]string `json:"colors,omitempty"`
	Material        string            `json:"material,omitempty"`
	Rim             string            `json:"rim,omitempty"`
	Highway         string            `json:"highway,omitempty"`
	HighwayOpacity  *float64          `json:"highway_opacity,omitempty"`
	BeatLines       *float64          `json:"beat_lines,omitempty"`
	LaneLines       *float64          `json:"lane_lines,omitempty"`
	Texture         string            `json:"texture,omitempty"`
	TextureStrength *float64          `json:"texture_strength,omitempty"`
}

// Resolve turns a spec into a palette and look. noteSize is kept as is.
func (s *ThemeSpec) Resolve(noteSize float64) (Theme, Look) {
	t := BaseTheme()
	set := map[string]bool{}
	for _, slot := range ColorSlots {
		if v, ok := s.Colors[slot.ID]; ok {
			if c, ok := ParseHex(v); ok {
				if c.A != 0xFF {
					// see-through colors keep their alpha
					for _, p := range slot.ptrs(&t) {
						*p = Premul(c)
					}
				} else {
					slot.Set(&t, c)
				}
				set[slot.ID] = true
			}
		}
	}
	for _, slot := range ColorSlots {
		if !set[slot.ID] && slot.follow != "" && set[slot.follow] {
			f, _ := Slot(slot.follow)
			slot.Set(&t, f.Get(&t))
		}
	}
	l := DefaultLook()
	l.NoteSize = noteSize
	if s.Material != "" {
		l.Material = ParseMaterial(s.Material)
	}
	if s.Rim != "" {
		l.Rim = ParseRim(s.Rim)
	}
	if s.Highway != "" {
		l.Highway = ParseHighwayStyle(s.Highway)
	}
	clamp := func(p *float64, lo, hi float64, dst *float64) {
		if p != nil {
			*dst = math.Max(lo, math.Min(hi, *p))
		}
	}
	clamp(s.HighwayOpacity, 0, 1, &l.HighwayOpacity)
	clamp(s.BeatLines, 0, 3, &l.BeatLines)
	clamp(s.LaneLines, 0, 3, &l.LaneLines)
	if s.Texture != "" {
		l.Texture = ParseTexture(s.Texture)
	}
	clamp(s.TextureStrength, 0, 1, &l.TextureStrength)
	return t, l
}

// Apply makes the spec the look in use.
func (s *ThemeSpec) Apply(noteSize float64) {
	t, l := s.Resolve(noteSize)
	DefaultTheme = t
	SetLook(l)
	clearSprites()
}

func fp(v float64) *float64 { return &v }

// CaptureSpec writes down a palette and look as a complete spec.
func CaptureSpec(name string, t *Theme, l Look) ThemeSpec {
	s := ThemeSpec{
		Name:            name,
		Colors:          map[string]string{},
		Material:        l.Material.String(),
		Rim:             l.Rim.String(),
		Highway:         l.Highway.String(),
		HighwayOpacity:  fp(math.Round(l.HighwayOpacity*100) / 100),
		BeatLines:       fp(math.Round(l.BeatLines*100) / 100),
		LaneLines:       fp(math.Round(l.LaneLines*100) / 100),
		Texture:         l.Texture,
		TextureStrength: fp(math.Round(l.TextureStrength*100) / 100),
	}
	for _, slot := range ColorSlots {
		s.Colors[slot.ID] = Hex(Straight(*slot.ptrs(t)[0]))
	}
	return s
}

// CurrentSpec writes down the look in use.
func CurrentSpec(name string) ThemeSpec { return CaptureSpec(name, &DefaultTheme, CurrentLook) }

// BuiltinThemes are the themes that come with the game, Jam first.
var BuiltinThemes = []ThemeSpec{
	{Name: "Jam", Texture: "rosewood"},
	{Name: "Neon", Material: "neon", Rim: "lane", Highway: "lanes", Texture: "hex", TextureStrength: fp(0.6), HighwayOpacity: fp(0.9), BeatLines: fp(1.2), LaneLines: fp(1.6), Colors: map[string]string{
		"green": "#39FF14", "red": "#FF2E63", "yellow": "#FFF01F", "blue": "#2D7BFF", "orange": "#FF7A00", "open": "#D04BFF",
		"highway": "#05050A", "sp_highway": "#1A0533", "rails": "#FF2EC4", "rails_sp": "#00F0FF", "beat": "#C8B6FF",
		"star_power": "#00F0FF", "accent": "#FF2EC4", "text": "#F8F0FF", "text_dim": "#9C8CB8", "panel": "#120A1EE8",
		"bg_top": "#05000A", "bg_bottom": "#1A0026", "bg_glow": "#FF2EC4",
	}},
	{Name: "Arcade", Material: "flat", Rim: "white", Highway: "solid", HighwayOpacity: fp(0.92), Colors: map[string]string{
		"green": "#00C850", "red": "#F02828", "yellow": "#FFD200", "blue": "#1E78FF", "orange": "#FF8200", "open": "#A050FF",
		"highway": "#101830", "rails": "#FFFFFF", "accent": "#FFD200", "panel": "#141C38E8",
		"bg_top": "#0A1028", "bg_bottom": "#18204A", "bg_glow": "#3050C0",
	}},
	{Name: "Chrome", Material: "metal", Rim: "silver", Highway: "gradient", Texture: "brushed", Colors: map[string]string{
		"green": "#1FB84A", "red": "#D92B2B", "yellow": "#F2C014", "blue": "#2470E6", "orange": "#F2761A", "open": "#9A52E6",
		"highway": "#15171C", "rails": "#D8DEE8", "beat": "#E0E6F0", "accent": "#9FB4D0", "text": "#EEF2F7", "panel": "#181A20E8",
		"bg_top": "#0A0B0E", "bg_bottom": "#22252C", "bg_glow": "#607080",
	}},
	{Name: "Pastel", Material: "glossy", Rim: "white", Highway: "gradient", Colors: map[string]string{
		"green": "#8EE6A8", "red": "#FF9AA2", "yellow": "#FFE29A", "blue": "#9AC8FF", "orange": "#FFC29A", "open": "#D3B0FF",
		"highway": "#2A2438", "rails": "#F4E8FF", "star_power": "#B8F4FF", "accent": "#FF9AC8", "text": "#FFF7FB", "text_dim": "#B6A8C6",
		"panel": "#2A2238E8", "bg_top": "#1C1726", "bg_bottom": "#3A2E4F", "bg_glow": "#FFB6E1",
	}},
	{Name: "Midnight", Material: "matte", Rim: "black", Highway: "gradient", Texture: "stars", BeatLines: fp(0.8), Colors: map[string]string{
		"green": "#3FA66B", "red": "#C2484E", "yellow": "#D9B44A", "blue": "#4C7FC2", "orange": "#D9823F", "open": "#8E6BC2",
		"highway": "#0A0E18", "rails": "#6E7FA0", "beat": "#8090B0", "accent": "#5C8FD6", "text": "#DCE3F0", "text_dim": "#7A859C",
		"panel": "#0E1424E8", "bg_top": "#03050A", "bg_bottom": "#0B1222", "bg_glow": "#1E3A6E",
	}},
	{Name: "Gold", Material: "metal", Rim: "gold", Highway: "gradient", Texture: "diamond", TextureStrength: fp(0.6), Colors: map[string]string{
		"highway": "#140E08", "rails": "#E8C36A", "beat": "#F0D9A0", "accent": "#E8B33A", "text": "#FFF6E6", "text_dim": "#B8A486",
		"panel": "#1E150BE8", "bg_top": "#0A0703", "bg_bottom": "#2A1A08", "bg_glow": "#B07A20",
	}},
	{Name: "Glass", Material: "glass", Rim: "white", Highway: "glass", HighwayOpacity: fp(0.6), Colors: map[string]string{
		"green": "#5CFF8A", "red": "#FF5C6C", "yellow": "#FFE05C", "blue": "#5CA8FF", "orange": "#FFA65C", "open": "#C68CFF",
		"highway": "#0A1420", "rails": "#BFE6FF", "accent": "#5CC8FF", "panel": "#0E1A28E8",
		"bg_top": "#040A12", "bg_bottom": "#0E2236", "bg_glow": "#2A7AB0",
	}},
	{Name: "Color-blind friendly", Rim: "white", Colors: map[string]string{
		// Okabe and Ito's palette: tells apart under the common kinds of
		// color blindness.
		"green": "#009E73", "red": "#D55E00", "yellow": "#F0E442", "blue": "#0072B2", "orange": "#E69F00", "open": "#CC79A7",
		"star_power": "#56B4E9", "early": "#56B4E9", "late": "#E69F00", "good": "#F0E442", "bad": "#D55E00",
	}},
}

// LoadThemeFiles reads the theme files in dir: one ThemeSpec as JSON per
// .json file, named after the file.
func LoadThemeFiles(dir string) ([]ThemeSpec, []error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var out []ThemeSpec
	var errs []error
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var s ThemeSpec
		if err := json.Unmarshal(data, &s); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		s.Name = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, errs
}

// SaveThemeFile writes a spec to dir/<name>.json and returns the path.
func SaveThemeFile(dir, name string, s ThemeSpec) (string, error) {
	name = SafeFileName(name)
	if name == "" {
		return "", fmt.Errorf("the theme needs a name")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	s.Name = name
	s.Base = ""
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name+".json")
	return p, os.WriteFile(p, append(data, '\n'), 0o644)
}

// SafeFileName drops characters Windows won't take in a file name.
func SafeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, s)
	return strings.Trim(strings.TrimSpace(s), ".")
}
