package render

import (
	"image/color"

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

// Sprite size: gems are drawn into a 200x130 image centered at (100, 62).
const (
	spriteW  = 200
	spriteH  = 130
	spriteCX = 100
	spriteCY = 62
	gemRX    = 88.0 // top face radius in the sprite
)

var (
	silver     = color.RGBA{0xE6, 0xEA, 0xF2, 0xFF}
	silverDark = color.RGBA{0x7A, 0x80, 0x90, 0xFF}
	capDark    = color.RGBA{0x16, 0x17, 0x1E, 0xFF}
)

// GemSprite returns (and caches) a gem image for a style and color.
func GemSprite(style GemStyle, c color.RGBA) *ebiten.Image {
	k := spriteKey{style, c}
	if img, ok := sprites[k]; ok {
		return img
	}
	img := ebiten.NewImage(spriteW, spriteH)
	var b Batch
	cx, cy := float64(spriteCX), float64(spriteCY)
	rx, ry := gemRX, 44.0
	side := Darken(c, 0.55)
	switch style {
	case GemCymbal:
		ry = 34
		b.Ellipse(img, cx, cy+12, rx+2, ry+4, Darken(c, 0.7), Darken(c, 0.7))
		b.Ellipse(img, cx, cy, rx, ry, Lighten(c, 0.35), c)
		b.Ring(img, cx, cy, rx, ry, 6, Lighten(c, 0.6), Lighten(c, 0.3))
		b.Ring(img, cx, cy, rx*0.72, ry*0.72, 3, Darken(c, 0.25), Darken(c, 0.25))
		b.Ring(img, cx, cy, rx*0.46, ry*0.46, 3, Darken(c, 0.25), Darken(c, 0.25))
		b.Ellipse(img, cx, cy, rx*0.2, ry*0.2, Lighten(c, 0.8), Lighten(c, 0.5))
	case GemTap:
		b.Ellipse(img, cx, cy+12, rx+2, ry+4, Darken(c, 0.75), Darken(c, 0.75))
		b.Ellipse(img, cx, cy, rx, ry, capDark, Darken(c, 0.6))
		b.Ring(img, cx, cy, rx, ry, 16, Lighten(c, 0.25), c)
		b.Ring(img, cx, cy, rx-16, ry-8, 3, Lighten(c, 0.7), Lighten(c, 0.7))
		b.Ellipse(img, cx, cy-2, rx*0.28, ry*0.28, Lighten(c, 0.5), WithAlpha(c, 0.6))
	default:
		// Side wall gives the gem its thickness.
		b.Ellipse(img, cx, cy+13, rx+1, ry+3, side, Darken(c, 0.75))
		b.Rect(img, cx-rx-1, cy, 2*rx+2, 13, side, side)
		// Top face.
		b.Ellipse(img, cx, cy, rx, ry, Lighten(c, 0.3), c)
		// Metal rim.
		b.Ring(img, cx, cy, rx, ry, 10, silver, silverDark)
		capC := capDark
		capE := Darken(c, 0.45)
		if style == GemHOPO {
			capC = color.RGBA{0xFA, 0xFA, 0xFF, 0xFF}
			capE = Lighten(c, 0.55)
		}
		if style == GemTom {
			capC, capE = Lighten(c, 0.15), Darken(c, 0.2)
		}
		b.Ellipse(img, cx, cy-1, rx*0.5, ry*0.5, capC, capE)
		b.Ring(img, cx, cy-1, rx*0.5, ry*0.5, 3, Lighten(c, 0.4), Lighten(c, 0.4))
	}
	b.Flush(img)
	// Gloss highlight.
	var g Batch
	g.Ellipse(img, cx-rx*0.32, cy-ry*0.5, rx*0.28, ry*0.16, color.RGBA{0x90, 0x90, 0x90, 0x90}, color.RGBA{0, 0, 0, 0})
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
