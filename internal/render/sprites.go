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
	mat   Material
	rim   Rim
}

var sprites = map[spriteKey]*ebiten.Image{}

// ResetSprites drops cached gem and glow sprites (after colors change).
func ResetSprites() { clearSprites() }

// clearSprites drops cached gem and glow sprites after the look changes.
// The images are left to the garbage collector: deallocating them here
// could stall a frame that's still drawing them.
func clearSprites() {
	clear(sprites)
	clear(glowCache)
}

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

// finish is how a material paints the parts of a gem.
type finish struct {
	flat         bool    // no side wall or shadow
	shadow       float64 // shadow strength
	glow         float64 // neon halo strength
	side0, side1 color.RGBA
	face0, face1 color.RGBA // top face: center, edge
	sheen        float64    // soft light patch on the face
	rimW         float64
	rim0, rim1   color.RGBA
	lit, shade   float64    // rim highlight and shadow arcs
	cap0, cap1   color.RGBA // strum gem cap: center, edge
	hopo0, hopo1 color.RGBA
	capShade     float64 // shadow inside the cap's upper edge
	gloss        float64 // specular highlight
	tapFace      color.RGBA
	cymbalFace   color.RGBA
	cymbalLines  float64
	extraRimArcs bool // metal: banded reflections
	brushed      bool // metal: rings on the face
	alpha        float64
	outlineOnly  bool // neon: caps drawn as rings
	cymRim0      color.RGBA
	cymRim1      color.RGBA
}

func finishFor(m Material, rim Rim, c color.RGBA) finish {
	r0, r1 := rim.colors(c)
	f := finish{
		shadow: 0.45, side0: Darken(c, 0.78), side1: Darken(c, 0.15),
		face0: Lighten(c, 0.3), face1: Darken(c, 0.08), sheen: 0.55,
		rimW: 11, rim0: r0, rim1: r1, lit: 0.95, shade: 0.35,
		cap0: capDark, cap1: Darken(c, 0.45), hopo0: color.RGBA{0xFA, 0xFA, 0xFF, 0xFF}, hopo1: Lighten(c, 0.55),
		capShade: 0.55, gloss: 1, tapFace: Lighten(capDark, 0.08), cymbalFace: Lighten(c, 0.4), cymbalLines: 1, alpha: 1,
		cymRim0: Lighten(c, 0.6), cymRim1: Lighten(c, 0.3),
	}
	if rim != RimSilver {
		f.cymRim0, f.cymRim1 = r0, r1
	}
	switch m {
	case MatGlossy:
		f.side0, f.side1 = Darken(c, 0.6), Lighten(c, 0.1)
		f.face0, f.face1 = Lighten(c, 0.5), c
		f.sheen = 0.8
		f.rimW = 8
		f.cap0, f.cap1 = Darken(c, 0.55), Darken(c, 0.3) // HOPOs stay white
		f.capShade = 0.3
		f.gloss = 1.8
		f.tapFace = Darken(c, 0.55)
	case MatMatte:
		f.shadow = 0.35
		f.side0, f.side1 = Darken(c, 0.6), Darken(c, 0.3)
		f.face0, f.face1 = Darken(c, 0.02), Darken(c, 0.15)
		f.sheen = 0
		f.rim0, f.rim1 = Darken(r0, 0.15), r1
		f.lit, f.shade = 0, 0.2
		f.cap0, f.cap1 = Lighten(capDark, 0.06), Darken(c, 0.55)
		f.hopo0, f.hopo1 = color.RGBA{0xE8, 0xE8, 0xEC, 0xFF}, Lighten(c, 0.4)
		f.capShade = 0.25
		f.gloss = 0
		f.cymbalFace = Lighten(c, 0.15)
		f.cymbalLines = 0.6
	case MatMetal:
		f.side0, f.side1 = Darken(c, 0.85), Lighten(c, 0.25)
		f.face0, f.face1 = Mix(Lighten(c, 0.2), silver, 0.2), Darken(c, 0.3)
		f.sheen = 0.35
		f.rimW = 13
		f.cap0, f.cap1 = Mix(capDark, r0, 0.2), Darken(c, 0.55)
		f.hopo0, f.hopo1 = white, Lighten(r0, 0.2)
		f.gloss = 1.3
		f.extraRimArcs = true
		f.brushed = true
		f.cymbalFace = Mix(Lighten(c, 0.3), silver, 0.3)
	case MatGlass:
		f.shadow = 0.18
		f.alpha = 0.55
		f.side0, f.side1 = WithAlpha(Darken(c, 0.4), 0.55), WithAlpha(Lighten(c, 0.3), 0.6)
		f.face0, f.face1 = WithAlpha(Lighten(c, 0.35), 0.45), WithAlpha(c, 0.3)
		f.sheen = 0.5
		f.rimW = 6
		f.rim0, f.rim1 = WithAlpha(Lighten(r0, 0.3), 0.95), WithAlpha(r1, 0.7)
		f.cap0, f.cap1 = WithAlpha(Darken(c, 0.6), 0.7), WithAlpha(Darken(c, 0.3), 0.5)
		f.hopo0, f.hopo1 = WithAlpha(white, 0.9), WithAlpha(Lighten(c, 0.6), 0.6)
		f.capShade = 0.2
		f.gloss = 2
		f.tapFace = WithAlpha(Darken(c, 0.6), 0.4)
		f.cymbalFace = WithAlpha(Lighten(c, 0.4), 0.5)
	case MatNeon:
		dark := color.RGBA{0x0A, 0x0A, 0x12, 0xFF}
		f.shadow = 0.25
		f.glow = 1
		f.side0, f.side1 = Mix(dark, c, 0.15), Mix(dark, c, 0.45)
		f.face0, f.face1 = dark, Mix(dark, c, 0.35)
		f.sheen = 0
		f.rimW = 10
		f.rim0, f.rim1 = Lighten(c, 0.55), c
		f.lit, f.shade = 0.5, 0
		f.cap0, f.cap1 = dark, dark
		f.hopo0, f.hopo1 = Lighten(c, 0.85), Lighten(c, 0.4)
		f.capShade = 0
		f.gloss = 0
		f.tapFace = dark
		f.cymbalFace = Mix(dark, c, 0.25)
		f.outlineOnly = true
	case MatFlat:
		f.flat = true
		f.shadow = 0
		f.face0, f.face1 = c, c
		f.sheen = 0
		f.rimW = 9
		f.lit, f.shade = 0, 0
		f.cap0, f.cap1 = capDark, capDark
		f.hopo0, f.hopo1 = white, white
		f.capShade = 0
		f.gloss = 0
		f.tapFace = capDark
		f.cymbalFace = c
		f.cymbalLines = 0.7
	}
	return f
}

// GemSprite returns (and caches) a gem image for a style and color in the
// current material and rim. Gems are short cylinders: a side wall, a top
// face, a rim and a recessed cap (flat gems are just the top).
func GemSprite(style GemStyle, c color.RGBA) *ebiten.Image {
	lk := CurrentLook
	k := spriteKey{style, c, lk.Material, lk.Rim}
	if img, ok := sprites[k]; ok {
		return img
	}
	img := ebiten.NewImage(spriteW, spriteH)
	drawGemSprite(img, style, c, finishFor(lk.Material, lk.Rim, c))
	sprites[k] = img
	return img
}

func drawGemSprite(img *ebiten.Image, style GemStyle, c color.RGBA, f finish) {
	var b Batch
	cx, cy := float64(spriteCX), float64(spriteCY)
	rx, ry := gemRX, 44.0
	depth := 24.0
	if style == GemCymbal {
		ry, depth = 34, 12
	}
	if f.flat {
		depth = 0
	}
	if f.glow > 0 {
		b.Ellipse(img, cx, cy+depth/2, rx+10, ry+10, WithAlpha(c, 0.45*f.glow), WithAlpha(c, 0))
	}
	if f.shadow > 0 {
		b.Ellipse(img, cx+5, cy+depth+10, rx+6, ry+6, WithAlpha(black, f.shadow), color.RGBA{})
	}
	if depth > 0 {
		b.Ellipse(img, cx, cy+depth, rx, ry, f.side0, f.side0)
		b.CylinderSide(img, cx, cy, rx, ry, depth, f.side0, f.side1)
	}
	rimArcs := func(rx, ry, w float64) {
		if f.lit > 0 {
			b.EllipseArc(img, cx, cy, rx, ry, w, litFrom, litTo, WithAlpha(white, f.lit))
		}
		if f.shade > 0 {
			b.EllipseArc(img, cx, cy, rx-6, ry-3, w-1, shadeFrom, shadeTo, WithAlpha(black, f.shade))
		}
		if f.extraRimArcs {
			b.EllipseArc(img, cx, cy, rx-w*1.5, ry-w*0.75, 3, -math.Pi*0.9, -math.Pi*0.6, WithAlpha(white, 0.7))
			b.EllipseArc(img, cx, cy, rx, ry, 3, math.Pi*0.1, math.Pi*0.35, WithAlpha(white, 0.6))
		}
	}
	switch style {
	case GemCymbal:
		b.Ellipse(img, cx, cy, rx, ry, f.cymbalFace, Darken(f.cymbalFace, 0.15))
		b.Ring(img, cx, cy, rx, ry, 6, f.cymRim0, f.cymRim1)
		if f.cymbalLines > 0 {
			lc := WithAlpha(Darken(c, 0.3), f.cymbalLines)
			b.Ring(img, cx, cy, rx*0.72, ry*0.72, 3, lc, lc)
			b.Ring(img, cx, cy, rx*0.46, ry*0.46, 3, lc, lc)
		}
		rimArcs(rx, ry, 5)
		b.Ellipse(img, cx, cy, rx*0.2, ry*0.2, Lighten(c, 0.8), Lighten(c, 0.5))
	case GemTap:
		b.Ellipse(img, cx, cy, rx, ry, Lighten(f.tapFace, 0.06), f.tapFace)
		b.Ring(img, cx, cy, rx, ry, 16, Lighten(c, 0.3), Darken(c, 0.1))
		rimArcs(rx, ry, 7)
		b.Ring(img, cx, cy, rx-16, ry-8, 3, Lighten(c, 0.7), Lighten(c, 0.7))
		b.Ellipse(img, cx, cy-2, rx*0.28, ry*0.28, Lighten(c, 0.5), WithAlpha(c, 0.6))
	default:
		b.Ellipse(img, cx, cy, rx, ry, f.face0, f.face1)
		if f.sheen > 0 {
			b.Ellipse(img, cx-rx*0.18, cy-ry*0.2, rx*0.62, ry*0.55, WithAlpha(Lighten(c, 0.45), f.sheen), color.RGBA{})
		}
		if f.brushed {
			for _, k := range []float64{0.92, 0.84, 0.76, 0.68} {
				b.Ring(img, cx, cy, rx*k, ry*k, 1.2, WithAlpha(white, 0.08), WithAlpha(white, 0.08))
			}
		}
		b.Ring(img, cx, cy, rx, ry, f.rimW, f.rim0, f.rim1)
		rimArcs(rx, ry, 5)
		capC, capE := f.cap0, f.cap1
		if style == GemHOPO {
			capC, capE = f.hopo0, f.hopo1
		}
		if style == GemTom {
			capC, capE = Lighten(c, 0.15), Darken(c, 0.2)
			if f.outlineOnly {
				capC, capE = f.cap0, f.cap1
			}
		}
		crx, cry := rx*0.5, ry*0.5
		if f.outlineOnly {
			// neon: the cap is a glowing ring (filled for HOPOs)
			if style == GemHOPO {
				b.Ellipse(img, cx, cy-1, crx, cry, capC, capE)
			}
			b.Ring(img, cx, cy-1, crx, cry, 5, Lighten(c, 0.5), c)
			break
		}
		if !f.flat {
			b.Ring(img, cx, cy-1, crx+3, cry+2, 4, WithAlpha(Darken(c, 0.35), f.alpha), WithAlpha(Darken(c, 0.2), f.alpha))
		}
		b.Ellipse(img, cx, cy-1, crx, cry, capC, capE)
		if f.capShade > 0 {
			b.EllipseArc(img, cx, cy-1, crx, cry, 6, -math.Pi*0.95, -math.Pi*0.05, WithAlpha(black, f.capShade))
			b.EllipseArc(img, cx, cy-1, crx, cry, 3, math.Pi*0.15, math.Pi*0.85, WithAlpha(Lighten(c, 0.6), 0.8*f.alpha))
		}
		if f.flat {
			b.Ring(img, cx, cy-1, crx, cry, 4, f.rim0, f.rim0)
		}
	}
	b.Flush(img)
	if f.gloss > 0 {
		var g Batch
		a := uint8(math.Min(255, 0xA0*f.gloss))
		g.Ellipse(img, cx-rx*0.34, cy-ry*0.52, rx*0.3, ry*0.15, color.RGBA{a, a, a, a}, color.RGBA{})
		if f.gloss > 1.5 {
			g.Dot(img, cx+rx*0.42, cy-ry*0.36, 5, WithAlpha(white, 0.8), WithAlpha(white, 0))
		}
		g.Flush(img)
	}
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
