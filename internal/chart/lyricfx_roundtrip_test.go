package chart

import (
	"image/color"
	"reflect"
	"testing"
)

// Effects written to a project file read back to the same effect, so a
// chart's lyrics can be saved without losing them.
func TestFXStringsRoundTrip(t *testing.T) {
	in := []string{"color:#ff4d6d", "rainbow", "glow:3", "shake:2.5", "wave", "zoom:1.5", "hold", "takeover"}
	fxs := ParseFXList(in)
	if len(fxs) != len(in) {
		t.Fatalf("parsed %d of %d", len(fxs), len(in))
	}
	back := ParseFXList(FXStrings(fxs))
	if !reflect.DeepEqual(back, fxs) {
		t.Errorf("round trip:\n got %+v\nwant %+v", back, fxs)
	}
	if got := (FX{Name: "color", Color: color.RGBA{0x0a, 0x0b, 0x0c, 0xff}}).String(); got != "color:#0a0b0c" {
		t.Errorf("color string %q", got)
	}
}
