package render

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

// The game is laid out on a 1920x1080 canvas. When the window isn't 16:9
// (an ultrawide monitor, say) the canvas sits in the middle of a larger
// screen, and backgrounds are drawn across the whole screen instead of
// stopping at the canvas edges.

type stage struct {
	full   *ebiten.Image
	ox, oy float64 // canvas position on the full screen
	w, h   float64 // full screen size
}

var cur stage

// SetStage says where the canvas sits on the real screen for this frame.
// full nil means the canvas is the screen.
func SetStage(full *ebiten.Image, ox, oy float64) {
	cur = stage{full: full, ox: ox, oy: oy}
	if full != nil {
		b := full.Bounds()
		cur.w, cur.h = float64(b.Dx()), float64(b.Dy())
	}
}

// Backdrop returns where to draw a full-screen background: the image, the
// rectangle covering the whole screen, and the canvas origin within it (add
// it to canvas coordinates to line things up with the game).
func Backdrop(dst *ebiten.Image) (img *ebiten.Image, w, h, ox, oy float64) {
	if cur.full == nil {
		return dst, ScreenW, ScreenH, 0, 0
	}
	return cur.full, cur.w, cur.h, cur.ox, cur.oy
}

// Dim darkens the whole screen (dialogs, pause): the canvas and, on a wider
// screen, the background either side of it.
func Dim(dst *ebiten.Image, c color.Color) {
	FillRect(dst, 0, 0, ScreenW, ScreenH, c)
	if cur.full == nil {
		return
	}
	fill := func(x, y, w, h float64) {
		if w > 0 && h > 0 {
			FillRect(cur.full, x, y, w, h, c)
		}
	}
	fill(0, 0, cur.ox, cur.h)
	fill(cur.ox+ScreenW, 0, cur.w-cur.ox-ScreenW, cur.h)
	fill(cur.ox, 0, ScreenW, cur.oy)
	fill(cur.ox, cur.oy+ScreenH, ScreenW, cur.h-cur.oy-ScreenH)
}

// ViewSize is the logical screen size for a window of w x h: the canvas
// widened (or heightened) to the window's shape.
func ViewSize(w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return ScreenW, ScreenH
	}
	if w*ScreenH >= h*ScreenW { // wider than 16:9
		return max(ScreenW, (ScreenH*w+h/2)/h), ScreenH
	}
	return ScreenW, max(ScreenH, (ScreenW*h+w/2)/w)
}
