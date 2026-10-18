package chart

import (
	"image/color"
	"regexp"
	"strconv"
	"strings"

	"github.com/mxygem/jam/internal/rich"
)

// FX is one lyric effect on a syllable, with its optional parameter.
//
//	color:#hex    sung color for the syllable
//	rainbow       hue drifts across the letters
//	glow[:size]   halo around sung letters
//	whisper       smaller, italic, faded
//	pulse         the line breathes on the beat
//	bounce        each syllable hops as it's sung
//	wave[:height] letters ride a slow wave
//	shake[:n]     letters jitter while sung
//	zoom[:scale]  the syllable pops big on its hit
//	hold          a held syllable's letters spread over the note
//	glitch        color channels split while sung
//	type          letters appear only as they're sung
//	flyin         letters drop into place before the phrase
//	explode       the syllable bursts into sparks after it's sung
//	takeover      the phrase fills the middle of the screen
type FX struct {
	Name  string
	Arg   float64 // the number after the colon, 0 when there is none
	Color color.RGBA
}

// FXNames lists every lyric effect jam draws, in the order the docs give them.
var FXNames = []string{"color", "rainbow", "glow", "whisper", "pulse", "bounce", "wave", "shake",
	"zoom", "hold", "glitch", "type", "flyin", "explode", "takeover"}

func knownFX(name string) bool {
	for _, n := range FXNames {
		if n == name {
			return true
		}
	}
	return false
}

// ParseFX reads one effect like "shake:2", "color:#ff4d6d" or "glow".
func ParseFX(s string) (FX, bool) {
	s = strings.TrimSpace(s)
	name, arg, _ := strings.Cut(s, ":")
	name = strings.ToLower(strings.TrimSpace(name))
	if !knownFX(name) {
		return FX{}, false
	}
	fx := FX{Name: name}
	arg = strings.TrimSpace(arg)
	if name == "color" {
		c, ok := rich.ParseColor(arg)
		if !ok {
			return FX{}, false
		}
		fx.Color = c
		return fx, true
	}
	if arg != "" {
		if v, err := strconv.ParseFloat(arg, 64); err == nil && v > 0 {
			fx.Arg = v
		}
	}
	return fx, true
}

// ParseFXList reads a list of effects, skipping ones jam doesn't know.
func ParseFXList(list []string) []FX {
	var out []FX
	for _, s := range list {
		if fx, ok := ParseFX(s); ok {
			out = append(out, fx)
		}
	}
	return out
}

// Has reports whether name is among fxs.
func HasFX(fxs []FX, name string) bool {
	_, ok := FindFX(fxs, name)
	return ok
}

// FindFX returns the effect called name, if present.
func FindFX(fxs []FX, name string) (FX, bool) {
	for _, f := range fxs {
		if f.Name == name {
			return f, true
		}
	}
	return FX{}, false
}

// mergeFX adds add to base, a later effect of the same name replacing an earlier one.
func mergeFX(base []FX, add ...FX) []FX {
	out := append([]FX(nil), base...)
	for _, a := range add {
		replaced := false
		for i := range out {
			if out[i].Name == a.Name {
				out[i], replaced = a, true
			}
		}
		if !replaced {
			out = append(out, a)
		}
	}
	return out
}

var fxTag = regexp.MustCompile(`(?i)<fx=([^>]*)>|</fx>`)

// fxScanner moves inline effect tags out of lyric text, phrase by phrase. A tag
// can open in one syllable and close in a later one: "<fx=shake:2>BURN" "IT"
// "DOWN</fx>". Several effects go in one tag, comma separated: <fx=zoom:2,glow>.
// It runs on the raw lyric text, before the lyric symbols are read (those would
// turn the "=" into a hyphen). Other games don't know these tags, so they're for
// charts meant for jam.
type fxScanner struct{ stack [][]FX }

// take strips the tags from one syllable's raw text and returns the effects
// that cover its visible text.
func (sc *fxScanner) take(raw string) (string, []FX) {
	var cover []FX
	addCover := func() {
		for _, open := range sc.stack {
			cover = mergeFX(cover, open...)
		}
	}
	if !strings.Contains(strings.ToLower(raw), "fx") {
		addCover()
		return raw, cover
	}
	var b strings.Builder
	last := 0
	for _, m := range fxTag.FindAllStringSubmatchIndex(raw, -1) {
		if strings.TrimSpace(raw[last:m[0]]) != "" {
			addCover()
		}
		b.WriteString(raw[last:m[0]])
		if m[2] >= 0 {
			sc.stack = append(sc.stack, ParseFXList(strings.Split(raw[m[2]:m[3]], ",")))
		} else if len(sc.stack) > 0 {
			sc.stack = sc.stack[:len(sc.stack)-1]
		}
		last = m[1]
	}
	if strings.TrimSpace(raw[last:]) != "" {
		addCover()
	}
	b.WriteString(raw[last:])
	return b.String(), cover
}

// withFX gives every syllable of the phrases the extra effects (a part's or
// phrase's list), keeping the syllable's own on top.
func withFX(phrases []LyricPhrase, fxs []FX) {
	if len(fxs) == 0 {
		return
	}
	for i := range phrases {
		for j := range phrases[i].Syllables {
			s := &phrases[i].Syllables[j]
			s.FX = mergeFX(fxs, s.FX...)
		}
	}
}
