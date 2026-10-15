// Package render draws the highway, gems, HUD and UI widgets.
package render

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Logical screen size. Everything is drawn at 1080p and scaled by Ebitengine.
const (
	ScreenW = 1920
	ScreenH = 1080
)

var whiteImg = func() *ebiten.Image {
	img := ebiten.NewImage(3, 3)
	img.Fill(color.White)
	return img
}()

var whiteSub = whiteImg.SubImage(image.Rect(1, 1, 2, 2)).(*ebiten.Image)

// RGBA builds a color from 0-255 components.
func RGBA(r, g, b, a uint8) color.RGBA { return color.RGBA{r, g, b, a} }

// WithAlpha returns c with alpha multiplied by a (0..1), premultiplied.
func WithAlpha(c color.Color, a float64) color.RGBA {
	r, g, b, al := c.RGBA()
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	return color.RGBA{uint8(float64(r>>8) * a), uint8(float64(g>>8) * a), uint8(float64(b>>8) * a), uint8(float64(al>>8) * a)}
}

// Mix blends two colors, t=0 gives a.
func Mix(a, b color.Color, t float64) color.RGBA {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	f := func(x, y uint32) uint8 { return uint8((float64(x>>8)*(1-t) + float64(y>>8)*t)) }
	return color.RGBA{f(ar, br), f(ag, bg), f(ab, bb), f(aa, ba)}
}

// Lighten moves a color toward white.
func Lighten(c color.Color, t float64) color.RGBA {
	_, _, _, a := c.RGBA()
	return Mix(c, color.RGBA{uint8(a >> 8), uint8(a >> 8), uint8(a >> 8), uint8(a >> 8)}, t)
}

// Darken moves a color toward black, keeping alpha.
func Darken(c color.Color, t float64) color.RGBA {
	r, g, b, a := c.RGBA()
	k := 1 - t
	return color.RGBA{uint8(float64(r>>8) * k), uint8(float64(g>>8) * k), uint8(float64(b>>8) * k), uint8(a >> 8)}
}

func vtx(x, y float64, c color.RGBA) ebiten.Vertex {
	// Colors in vertices are premultiplied 0..1.
	return ebiten.Vertex{
		DstX: float32(x), DstY: float32(y), SrcX: 1, SrcY: 1,
		ColorR: float32(c.R) / 255, ColorG: float32(c.G) / 255, ColorB: float32(c.B) / 255, ColorA: float32(c.A) / 255,
	}
}

var (
	triOpts = &ebiten.DrawTrianglesOptions{AntiAlias: true}
	addOpts = &ebiten.DrawTrianglesOptions{AntiAlias: true, Blend: ebiten.BlendLighter}
)

// Batch accumulates triangles and flushes them in one draw call.
type Batch struct {
	v []ebiten.Vertex
	i []uint16

	// Additive batches draw light (glows, sparks, flames) with
	// BlendLighter instead of normal alpha blending.
	Additive bool
}

// Flush draws and clears the batch.
func (b *Batch) Flush(dst *ebiten.Image) {
	if len(b.i) > 0 {
		opts := triOpts
		if b.Additive {
			opts = addOpts
		}
		dst.DrawTriangles(b.v, b.i, whiteSub, opts)
	}
	b.v = b.v[:0]
	b.i = b.i[:0]
}

func (b *Batch) room(n int, dst *ebiten.Image) {
	if len(b.v)+n > 60000 {
		b.Flush(dst)
	}
}

// Quad adds a quadrilateral with per-corner colors (p0..p3 in order).
func (b *Batch) Quad(dst *ebiten.Image, x0, y0, x1, y1, x2, y2, x3, y3 float64, c0, c1, c2, c3 color.RGBA) {
	b.room(4, dst)
	base := uint16(len(b.v))
	b.v = append(b.v, vtx(x0, y0, c0), vtx(x1, y1, c1), vtx(x2, y2, c2), vtx(x3, y3, c3))
	b.i = append(b.i, base, base+1, base+2, base, base+2, base+3)
}

// Rect adds an axis aligned rectangle with a vertical gradient.
func (b *Batch) Rect(dst *ebiten.Image, x, y, w, h float64, top, bottom color.RGBA) {
	b.Quad(dst, x, y, x+w, y, x+w, y+h, x, y+h, top, top, bottom, bottom)
}

// Ellipse adds a filled ellipse with a radial gradient from center to edge.
func (b *Batch) Ellipse(dst *ebiten.Image, cx, cy, rx, ry float64, center, edge color.RGBA) {
	const n = 40
	b.room(n+2, dst)
	base := uint16(len(b.v))
	b.v = append(b.v, vtx(cx, cy, center))
	for k := 0; k <= n; k++ {
		a := float64(k) / n * 2 * math.Pi
		b.v = append(b.v, vtx(cx+rx*math.Cos(a), cy+ry*math.Sin(a), edge))
	}
	for k := 0; k < n; k++ {
		b.i = append(b.i, base, base+1+uint16(k), base+2+uint16(k))
	}
}

// Dot adds a small filled circle with few segments (sparks, particles).
func (b *Batch) Dot(dst *ebiten.Image, cx, cy, r float64, center, edge color.RGBA) {
	const n = 8
	b.room(n+2, dst)
	base := uint16(len(b.v))
	b.v = append(b.v, vtx(cx, cy, center))
	for k := 0; k <= n; k++ {
		a := float64(k) / n * 2 * math.Pi
		b.v = append(b.v, vtx(cx+r*math.Cos(a), cy+r*math.Sin(a), edge))
	}
	for k := 0; k < n; k++ {
		b.i = append(b.i, base, base+1+uint16(k), base+2+uint16(k))
	}
}

// Ring adds an elliptical ring between radii scaled by inner (0..1).
func (b *Batch) Ring(dst *ebiten.Image, cx, cy, rx, ry, thickness float64, outer, inner color.RGBA) {
	const n = 40
	b.room(2*(n+1), dst)
	base := uint16(len(b.v))
	irx, iry := math.Max(0, rx-thickness), math.Max(0, ry-thickness*ry/math.Max(rx, 1e-6))
	for k := 0; k <= n; k++ {
		a := float64(k) / n * 2 * math.Pi
		c, s := math.Cos(a), math.Sin(a)
		b.v = append(b.v, vtx(cx+rx*c, cy+ry*s, outer), vtx(cx+irx*c, cy+iry*s, inner))
	}
	for k := 0; k < n; k++ {
		o := base + uint16(2*k)
		b.i = append(b.i, o, o+1, o+2, o+1, o+3, o+2)
	}
}

// Arc adds a thick circular arc from a0 to a1 (radians, 0 = right, clockwise
// on screen because y grows down).
func (b *Batch) Arc(dst *ebiten.Image, cx, cy, r, thickness, a0, a1 float64, c color.RGBA) {
	n := int(math.Max(4, math.Abs(a1-a0)/(2*math.Pi)*64))
	b.room(2*(n+1), dst)
	base := uint16(len(b.v))
	for k := 0; k <= n; k++ {
		a := a0 + (a1-a0)*float64(k)/float64(n)
		cs, sn := math.Cos(a), math.Sin(a)
		b.v = append(b.v, vtx(cx+r*cs, cy+r*sn, c), vtx(cx+(r-thickness)*cs, cy+(r-thickness)*sn, c))
	}
	for k := 0; k < n; k++ {
		o := base + uint16(2*k)
		b.i = append(b.i, o, o+1, o+2, o+1, o+3, o+2)
	}
}

// Line adds a thick line segment.
func (b *Batch) Line(dst *ebiten.Image, x0, y0, x1, y1, w float64, c0, c1 color.RGBA) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*w/2, dx/l*w/2
	b.Quad(dst, x0+nx, y0+ny, x1+nx, y1+ny, x1-nx, y1-ny, x0-nx, y0-ny, c0, c1, c1, c0)
}

// FillRect draws a solid rectangle immediately.
func FillRect(dst *ebiten.Image, x, y, w, h float64, c color.Color) {
	vector.FillRect(dst, float32(x), float32(y), float32(w), float32(h), c, false)
}

// StrokeRect draws a rectangle outline immediately.
func StrokeRect(dst *ebiten.Image, x, y, w, h, t float64, c color.Color) {
	vector.StrokeRect(dst, float32(x), float32(y), float32(w), float32(h), float32(t), c, true)
}

// RoundRect fills a rounded rectangle.
func RoundRect(dst *ebiten.Image, x, y, w, h, r float64, c color.Color) {
	var p vector.Path
	r = math.Min(r, math.Min(w, h)/2)
	p.MoveTo(float32(x+r), float32(y))
	p.LineTo(float32(x+w-r), float32(y))
	p.ArcTo(float32(x+w), float32(y), float32(x+w), float32(y+r), float32(r))
	p.LineTo(float32(x+w), float32(y+h-r))
	p.ArcTo(float32(x+w), float32(y+h), float32(x+w-r), float32(y+h), float32(r))
	p.LineTo(float32(x+r), float32(y+h))
	p.ArcTo(float32(x), float32(y+h), float32(x), float32(y+h-r), float32(r))
	p.LineTo(float32(x), float32(y+r))
	p.ArcTo(float32(x), float32(y), float32(x+r), float32(y), float32(r))
	p.Close()
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(c)
	vector.FillPath(dst, &p, nil, op)
}
