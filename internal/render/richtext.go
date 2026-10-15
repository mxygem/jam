package render

import (
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/mxygem/jam/internal/rich"
)

// Rich text drawing for song metadata; parsing lives in package rich.

// PlainText strips rich text tags.
func PlainText(s string) string { return rich.Plain(s) }

func boldOf(f Font) Font {
	switch f {
	case UI:
		return UIBold
	case Cond:
		return CondBold
	case CondBold:
		return Display
	}
	return f
}

// readable keeps author colors legible on the dark UI: very dark colors are
// lifted toward white just enough to read.
func readable(c color.RGBA) color.RGBA {
	lum := 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
	if lum < 90 {
		return Mix(c, color.RGBA{0xFF, 0xFF, 0xFF, c.A}, (90-lum)/255*1.6)
	}
	return c
}

// MeasureRich returns the drawn width and line height of rich text.
func MeasureRich(s string, f Font, size float64) (float64, float64) {
	w, h := 0.0, 0.0
	for _, sp := range rich.Parse(s) {
		ff := f
		if sp.Bold {
			ff = boldOf(f)
		}
		sw, sh := Measure(sp.Text, ff, size)
		w += sw
		h = max(h, sh)
	}
	return w, h
}

// RichText draws s with its styling. def is the color for unstyled text;
// its alpha also fades styled spans. maxW > 0 truncates with an ellipsis.
func RichText(dst *ebiten.Image, s string, f Font, size, x, y float64, def color.Color, al Align, maxW float64) {
	spans := rich.Parse(s)
	if maxW > 0 {
		spans = truncateSpans(spans, f, size, maxW)
	}
	w := 0.0
	for _, sp := range spans {
		ff := f
		if sp.Bold {
			ff = boldOf(f)
		}
		sw, _ := Measure(sp.Text, ff, size)
		w += sw
	}
	switch al {
	case Center:
		x -= w / 2
	case Right:
		x -= w
	}
	dr, dg, db, da := def.RGBA()
	alpha := float64(da) / 0xFFFF
	defc := color.RGBA{uint8(dr >> 8), uint8(dg >> 8), uint8(db >> 8), uint8(da >> 8)}
	for _, sp := range spans {
		ff := f
		if sp.Bold {
			ff = boldOf(f)
		}
		c := defc
		if sp.HasColor {
			c = WithAlpha(readable(sp.Color), alpha*float64(sp.Color.A)/255)
		}
		op := &text.DrawOptions{}
		if sp.Italic {
			// slant around the baseline
			op.GeoM.Translate(0, -size*0.8)
			op.GeoM.Skew(-0.2, 0)
			op.GeoM.Translate(0, size*0.8)
		}
		op.GeoM.Translate(x, y)
		op.ColorScale.ScaleWithColor(c)
		text.Draw(dst, sp.Text, faceFor(sp.Text, ff, size), op)
		sw, _ := Measure(sp.Text, ff, size)
		x += sw
	}
}

// truncateSpans cuts spans to fit maxW, ending with an ellipsis.
func truncateSpans(spans []rich.Span, f Font, size, maxW float64) []rich.Span {
	total := 0.0
	widths := make([]float64, len(spans))
	for i, sp := range spans {
		ff := f
		if sp.Bold {
			ff = boldOf(f)
		}
		widths[i], _ = Measure(sp.Text, ff, size)
		total += widths[i]
	}
	if total <= maxW {
		return spans
	}
	ell, _ := Measure("…", f, size)
	budget := maxW - ell
	var out []rich.Span
	used := 0.0
	for i, sp := range spans {
		if used+widths[i] <= budget {
			out = append(out, sp)
			used += widths[i]
			continue
		}
		ff := f
		if sp.Bold {
			ff = boldOf(f)
		}
		rs := []rune(sp.Text)
		for n := len(rs); n > 0; n-- {
			if w, _ := Measure(string(rs[:n]), ff, size); used+w <= budget {
				sp.Text = strings.TrimRight(string(rs[:n]), " ")
				out = append(out, sp)
				break
			}
		}
		break
	}
	last := rich.Span{Text: "…"}
	if n := len(out); n > 0 {
		last.Color, last.HasColor, last.Bold, last.Italic = out[n-1].Color, out[n-1].HasColor, out[n-1].Bold, out[n-1].Italic
	}
	return append(out, last)
}
