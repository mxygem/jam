package render

import (
	"image/color"
	"testing"
)

func TestHexRoundTrip(t *testing.T) {
	for _, s := range []string{"#2BD65A", "#120A1EE8", "#000000", "#FFFFFF"} {
		c, ok := ParseHex(s)
		if !ok || Hex(c) != s {
			t.Errorf("%s -> %v -> %s", s, c, Hex(c))
		}
	}
	if c, ok := ParseHex("f0a"); !ok || c != (color.RGBA{0xFF, 0x00, 0xAA, 0xFF}) {
		t.Errorf("short form: %v %v", c, ok)
	}
	for _, bad := range []string{"", "#12345", "zzzzzz", "#1234567"} {
		if _, ok := ParseHex(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestHSVRoundTrip(t *testing.T) {
	for _, s := range []string{"#2BD65A", "#F23A3A", "#2F86FF", "#808080", "#000000", "#FFD01F"} {
		c, _ := ParseHex(s)
		h, sat, v := HSV(c)
		got := FromHSV(h, sat, v)
		d := func(a, b uint8) int { x := int(a) - int(b); return x * x }
		if d(got.R, c.R)+d(got.G, c.G)+d(got.B, c.B) > 3 {
			t.Errorf("%s -> %.1f %.2f %.2f -> %s", s, h, sat, v, Hex(got))
		}
	}
}

func TestPremulRoundTrip(t *testing.T) {
	c := color.RGBA{0x12, 0x15, 0x22, 0xE8}
	if got := Straight(Premul(c)); got != c {
		t.Errorf("%v -> %v", c, got)
	}
}

// A theme that only sets guitar colors gets matching drum colors; the
// rest comes from Jam.
func TestThemeResolve(t *testing.T) {
	s := ThemeSpec{Name: "x", Material: "neon", Colors: map[string]string{"red": "#FF0000", "accent": "#00FF00"}}
	th, l := s.Resolve(1.2)
	red, _ := Slot("drum_red")
	if got := red.Get(&th); got != (color.RGBA{0xFF, 0, 0, 0xFF}) {
		t.Errorf("drum red follows red: %v", got)
	}
	if th.Lanes[0] != BaseTheme().Lanes[0] {
		t.Error("unset colors should be Jam's")
	}
	if l.Material != MatNeon || l.NoteSize != 1.2 || l.HighwayOpacity != DefaultLook().HighwayOpacity {
		t.Errorf("look %+v", l)
	}
	// Panels keep their see-through alpha when a theme sets them opaque.
	s.Colors["panel"] = "#202020"
	th, _ = s.Resolve(1)
	if th.Panel.A != BaseTheme().Panel.A {
		t.Errorf("panel alpha %x", th.Panel.A)
	}
	// Capturing and resolving gives the same palette back.
	back, bl := ptr(CaptureSpec("y", &th, l)).Resolve(1)
	if back != th || bl.Material != l.Material {
		t.Error("capture/resolve round trip changed the theme")
	}
}

func ptr(s ThemeSpec) *ThemeSpec { return &s }

func TestBuiltinThemesParse(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range BuiltinThemes {
		if seen[s.Name] {
			t.Errorf("two themes named %q", s.Name)
		}
		seen[s.Name] = true
		for id, v := range s.Colors {
			if _, ok := Slot(id); !ok {
				t.Errorf("%s: unknown color %q", s.Name, id)
			}
			if _, ok := ParseHex(v); !ok {
				t.Errorf("%s: bad color %s=%q", s.Name, id, v)
			}
		}
	}
}

func TestThemeFiles(t *testing.T) {
	dir := t.TempDir()
	spec := CaptureSpec("whatever", &jamTheme, DefaultLook())
	p, err := SaveThemeFile(dir, `Mine: "best"?`, spec)
	if err != nil {
		t.Fatal(err)
	}
	got, errs := LoadThemeFiles(dir)
	if len(errs) > 0 || len(got) != 1 || got[0].Name != "Mine best" {
		t.Fatalf("%s: %+v %v", p, got, errs)
	}
}
