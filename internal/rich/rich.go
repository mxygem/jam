// Package rich parses the Unity style rich text tags found in song metadata
// (charter names especially): <color=#33ccff>, <b>, <i>, <br> and others,
// as Clone Hero and YARG render them.
package rich

import (
	"image/color"
	"strconv"
	"strings"
)

// Span is a run of text with one style.
type Span struct {
	Text     string
	Color    color.RGBA
	HasColor bool
	Bold     bool
	Italic   bool
}

type richStyle struct {
	color    color.RGBA
	hasColor bool
	bold     int
	italic   int
}

// ParseRich splits s into styled spans. Unknown tags are dropped; <br> becomes
// a space (metadata is drawn on one line). Malformed tags are kept as text.
func Parse(s string) []Span {
	if !strings.Contains(s, "<") {
		return []Span{{Text: s}}
	}
	var out []Span
	st := richStyle{}
	var colors []richStyle // color stack
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		out = append(out, Span{Text: cur.String(), Color: st.color, HasColor: st.hasColor, Bold: st.bold > 0, Italic: st.italic > 0})
		cur.Reset()
	}
	for i := 0; i < len(s); {
		if s[i] != '<' {
			cur.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			cur.WriteString(s[i:])
			break
		}
		tag := s[i+1 : i+end]
		name, val, closing := tagParts(tag)
		if tag == "" || tag[0] == ' ' || !knownTag(name) {
			cur.WriteString(s[i : i+end+1]) // not a tag after all, e.g. "a < b > c"
			i += end + 1
			continue
		}
		flush()
		switch name {
		case "color":
			if closing {
				if n := len(colors); n > 0 {
					st.color, st.hasColor = colors[n-1].color, colors[n-1].hasColor
					colors = colors[:n-1]
				} else {
					st.hasColor = false
				}
			} else if c, ok := ParseColor(val); ok {
				colors = append(colors, richStyle{color: st.color, hasColor: st.hasColor})
				st.color, st.hasColor = c, true
			}
		case "b":
			if closing {
				st.bold = max(0, st.bold-1)
			} else {
				st.bold++
			}
		case "i":
			if closing {
				st.italic = max(0, st.italic-1)
			} else {
				st.italic++
			}
		case "br":
			cur.WriteByte(' ')
		}
		i += end + 1
	}
	flush()
	return out
}

func tagParts(tag string) (name, val string, closing bool) {
	tag = strings.TrimSpace(tag)
	if strings.HasPrefix(tag, "/") {
		closing = true
		tag = tag[1:]
	}
	tag = strings.TrimSuffix(tag, "/")
	if eq := strings.IndexByte(tag, '='); eq >= 0 {
		name, val = tag[:eq], strings.Trim(strings.TrimSpace(tag[eq+1:]), `"'`)
	} else if sp := strings.IndexByte(tag, ' '); sp >= 0 {
		name = tag[:sp]
	} else {
		name = tag
	}
	return strings.ToLower(strings.TrimSpace(name)), val, closing
}

// IsTag reports whether inner (the text between < and >) is a rich text tag.
func IsTag(inner string) bool {
	if inner == "" || inner[0] == ' ' {
		return false
	}
	name, _, _ := tagParts(inner)
	return knownTag(name)
}

// Split cuts s into alternating text and tag pieces; isTag[i] tells which.
func Split(s string) (pieces []string, isTag []bool) {
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		if IsTag(s[i+1 : i+end]) {
			if i > start {
				pieces, isTag = append(pieces, s[start:i]), append(isTag, false)
			}
			pieces, isTag = append(pieces, s[i:i+end+1]), append(isTag, true)
			start = i + end + 1
			i += end
		}
	}
	if start < len(s) {
		pieces, isTag = append(pieces, s[start:]), append(isTag, false)
	}
	return pieces, isTag
}

// knownTag lists the Unity rich text tags; anything else isn't a tag.
func knownTag(name string) bool {
	switch name {
	case "color", "b", "i", "br", "u", "s", "size", "sprite", "cspace", "mark", "alpha",
		"font", "material", "sup", "sub", "voffset", "nobr", "noparse", "lowercase",
		"uppercase", "smallcaps", "style", "align", "indent", "line-height", "pos",
		"space", "width", "mspace", "margin", "link", "rotate", "strikethrough", "underline":
		return true
	}
	return false
}

var namedColors = map[string]color.RGBA{
	"aqua": {0x00, 0xFF, 0xFF, 0xFF}, "black": {0, 0, 0, 0xFF}, "blue": {0x00, 0x00, 0xFF, 0xFF},
	"brown": {0xA5, 0x2A, 0x2A, 0xFF}, "cyan": {0x00, 0xFF, 0xFF, 0xFF}, "darkblue": {0x00, 0x00, 0xA0, 0xFF},
	"fuchsia": {0xFF, 0x00, 0xFF, 0xFF}, "green": {0x00, 0x80, 0x00, 0xFF}, "grey": {0x80, 0x80, 0x80, 0xFF},
	"gray": {0x80, 0x80, 0x80, 0xFF}, "lightblue": {0xAD, 0xD8, 0xE6, 0xFF}, "lime": {0x00, 0xFF, 0x00, 0xFF},
	"magenta": {0xFF, 0x00, 0xFF, 0xFF}, "maroon": {0x80, 0x00, 0x00, 0xFF}, "navy": {0x00, 0x00, 0x80, 0xFF},
	"olive": {0x80, 0x80, 0x00, 0xFF}, "orange": {0xFF, 0xA5, 0x00, 0xFF}, "purple": {0x80, 0x00, 0x80, 0xFF},
	"red": {0xFF, 0x00, 0x00, 0xFF}, "silver": {0xC0, 0xC0, 0xC0, 0xFF}, "teal": {0x00, 0x80, 0x80, 0xFF},
	"white": {0xFF, 0xFF, 0xFF, 0xFF}, "yellow": {0xFF, 0xFF, 0x00, 0xFF},
}

func ParseColor(v string) (color.RGBA, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if c, ok := namedColors[v]; ok {
		return c, true
	}
	if !strings.HasPrefix(v, "#") {
		return color.RGBA{}, false
	}
	h := v[1:]
	if len(h) == 3 || len(h) == 4 { // #rgb(a)
		var b strings.Builder
		for _, ch := range h {
			b.WriteRune(ch)
			b.WriteRune(ch)
		}
		h = b.String()
	}
	if len(h) != 6 && len(h) != 8 {
		return color.RGBA{}, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	if len(h) == 6 {
		return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 0xFF}, true
	}
	return color.RGBA{uint8(n >> 24), uint8(n >> 16), uint8(n >> 8), uint8(n)}, true
}

// PlainText strips rich text tags.
func Plain(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	var b strings.Builder
	for _, sp := range Parse(s) {
		b.WriteString(sp.Text)
	}
	return strings.TrimSpace(b.String())
}
