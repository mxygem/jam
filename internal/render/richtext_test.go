package render

import (
	"image/color"
	"testing"

	"github.com/mxygem/jam/internal/rich"
)

func TestReadable(t *testing.T) {
	// dark author colors get lifted to stay legible, bright ones don't change
	if r := readable(color.RGBA{0x42, 0x0A, 0x55, 0xFF}); r == (color.RGBA{0x42, 0x0A, 0x55, 0xFF}) {
		t.Error("very dark purple should be lifted")
	}
	if r := readable(color.RGBA{0x33, 0xCC, 0xFF, 0xFF}); r != (color.RGBA{0x33, 0xCC, 0xFF, 0xFF}) {
		t.Error("bright colors should be untouched")
	}
}

func TestRichTruncate(t *testing.T) {
	spans := rich.Parse("<b>A very long charter name</b> and more words here")
	out := truncateSpans(spans, UI, 30, 150)
	if out[len(out)-1].Text != "…" {
		t.Fatalf("missing ellipsis: %+v", out)
	}
	w := 0.0
	for _, sp := range out {
		f := UI
		if sp.Bold {
			f = UIBold
		}
		sw, _ := Measure(sp.Text, f, 30)
		w += sw
	}
	if w > 151 {
		t.Errorf("truncated width %.0f > 150", w)
	}
}
