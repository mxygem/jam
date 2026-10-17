package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// GemStyle is the shape of a gem sprite.
type GemStyle uint8

const (
	GemStrum GemStyle = iota
	GemHOPO
	GemTap
	GemCymbal
	GemTom
)

type spriteKey struct {
	style GemStyle
	c     color.RGBA
}

var sprites = map[spriteKey]*ebiten.Image{}

// Sprite size: gems are drawn into a 200x150 image with the top face
// centered at (100, 58); the side wall and shadow hang below.
const (
	spriteW  = 200
	spriteH  = 150
	spriteCX = 100
	spriteCY = 58
	gemRX    = 88.0 // top face radius in the sprite
)

var (
	silver     = color.RGBA{0xE6, 0xEA, 0xF2, 0xFF}
	silverDark = color.RGBA{0x7A, 0x80, 0x90, 0xFF}
	capDark    = color.RGBA{0x16, 0x17, 0x1E, 0xFF}
	white      = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	black      = color.RGBA{0, 0, 0, 0xFF}
)

// Light comes from the upper left: arcs on that side of a rim catch it.
const (
	litFrom, litTo     = -math.Pi * 0.98, -math.Pi * 0.38 // upper-left rim
	shadeFrom, shadeTo = math.Pi * 0.02, math.Pi * 0.62   // lower-right rim
)

// GemSprite returns (and caches) a gem image for a style and color. Gems
// are short cylinders: a lit side wall, a domed top face, a metal rim with
// a highlight and a recessed cap.
func GemSprite(style GemStyle, c color.RGBA) *ebiten.Image {
	k := spriteKey{style, c}
	if img, ok := sprites[k]; ok {
		return img
	}
	img := ebiten.NewImage(spriteW, spriteH)
	var b Batch
	cx, cy := float64(spriteCX), float64(spriteCY)
	rx, ry := gemRX, 44.0
	depth := 24.0
	if style == GemCymbal {
		ry, depth = 34, 12
	}
	// soft shadow on the highway
	b.Ellipse(img, cx+5, cy+depth+10, rx+6, ry+6, WithAlpha(black, 0.45), color.RGBA{})
	// side wall
	b.Ellipse(img, cx, cy+depth, rx, ry, Darken(c, 0.8), Darken(c, 0.8))
	b.CylinderSide(img, cx, cy, rx, ry, depth, Darken(c, 0.78), Darken(c, 0.15))
	switch style {
	case GemCymbal:
		b.Ellipse(img, cx, cy, rx, ry, Lighten(c, 0.4), c)
		b.Ring(img, cx, cy, rx, ry, 6, Lighten(c, 0.6), Lighten(c, 0.3))
		b.Ring(img, cx, cy, rx*0.72, ry*0.72, 3, Darken(c, 0.3), Darken(c, 0.15))
		b.Ring(img, cx, cy, rx*0.46, ry*0.46, 3, Darken(c, 0.3), Darken(c, 0.15))
		b.EllipseArc(img, cx, cy, rx, ry, 5, litFrom, litTo, WithAlpha(white, 0.9))
		b.Ellipse(img, cx, cy, rx*0.2, ry*0.2, Lighten(c, 0.8), Lighten(c, 0.5))
	case GemTap:
		b.Ellipse(img, cx, cy, rx, ry, Lighten(capDark, 0.08), capDark)
		b.Ring(img, cx, cy, rx, ry, 16, Lighten(c, 0.3), Darken(c, 0.1))
		b.EllipseArc(img, cx, cy, rx, ry, 7, litFrom, litTo, WithAlpha(white, 0.85))
		b.EllipseArc(img, cx, cy, rx, ry, 6, shadeFrom, shadeTo, WithAlpha(black, 0.35))
		b.Ring(img, cx, cy, rx-16, ry-8, 3, Lighten(c, 0.7), Lighten(c, 0.7))
		b.Ellipse(img, cx, cy-2, rx*0.28, ry*0.28, Lighten(c, 0.5), WithAlpha(c, 0.6))
	default:
		// Top face, brighter toward the light.
		b.Ellipse(img, cx, cy, rx, ry, Lighten(c, 0.3), Darken(c, 0.08))
		b.Ellipse(img, cx-rx*0.18, cy-ry*0.2, rx*0.62, ry*0.55, WithAlpha(Lighten(c, 0.45), 0.55), color.RGBA{})
		// Metal rim with a bright edge on the lit side.
		b.Ring(img, cx, cy, rx, ry, 11, silver, silverDark)
		b.EllipseArc(img, cx, cy, rx, ry, 5, litFrom, litTo, WithAlpha(white, 0.95))
		b.EllipseArc(img, cx, cy, rx-6, ry-3, 4, shadeFrom, shadeTo, WithAlpha(black, 0.35))
		capC := capDark
		capE := Darken(c, 0.45)
		if style == GemHOPO {
			capC = color.RGBA{0xFA, 0xFA, 0xFF, 0xFF}
			capE = Lighten(c, 0.55)
		}
		if style == GemTom {
			capC, capE = Lighten(c, 0.15), Darken(c, 0.2)
		}
		// Recessed cap: shadow inside its upper edge, light on its lower edge.
		crx, cry := rx*0.5, ry*0.5
		b.Ring(img, cx, cy-1, crx+3, cry+2, 4, Darken(c, 0.35), Darken(c, 0.2))
		b.Ellipse(img, cx, cy-1, crx, cry, capC, capE)
		b.EllipseArc(img, cx, cy-1, crx, cry, 6, -math.Pi*0.95, -math.Pi*0.05, WithAlpha(black, 0.55))
		b.EllipseArc(img, cx, cy-1, crx, cry, 3, math.Pi*0.15, math.Pi*0.85, WithAlpha(Lighten(c, 0.6), 0.8))
	}
	b.Flush(img)
	// Gloss highlight.
	var g Batch
	g.Ellipse(img, cx-rx*0.34, cy-ry*0.52, rx*0.3, ry*0.15, color.RGBA{0xA0, 0xA0, 0xA0, 0xA0}, color.RGBA{0, 0, 0, 0})
	g.Flush(img)
	sprites[k] = img
	return img
}

var glowCache = map[color.RGBA]*ebiten.Image{}

// Glow returns a soft radial glow sprite (256x256) for hit flashes.
func Glow(c color.RGBA) *ebiten.Image {
	if img, ok := glowCache[c]; ok {
		return img
	}
	img := ebiten.NewImage(256, 256)
	var b Batch
	b.Ellipse(img, 128, 128, 128, 128, WithAlpha(Lighten(c, 0.2), 0.55), color.RGBA{})
	b.Ellipse(img, 128, 128, 60, 60, WithAlpha(Lighten(c, 0.7), 0.7), color.RGBA{})
	b.Flush(img)
	glowCache[c] = img
	return img
}
