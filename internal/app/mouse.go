package app

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Mouse is this frame's mouse state, in screen (1920x1080) coordinates.
type Mouse struct {
	X, Y     float64
	Moved    bool    // moved since last frame
	Wheel    float64 // wheel notches, positive = up
	Click    bool    // left button pressed this frame
	Right    bool    // right button pressed this frame
	Double   bool    // second click within 0.35 s near the first
	lastX    float64
	lastY    float64
	lastTick int
	tick     int
}

// update reads the mouse; ox, oy is where the canvas sits on the screen.
func (m *Mouse) update(ox, oy float64) {
	m.tick++
	x, y := ebiten.CursorPosition()
	m.X, m.Y = float64(x)-ox, float64(y)-oy
	m.Moved = m.X != m.lastX || m.Y != m.lastY
	_, wy := ebiten.Wheel()
	m.Wheel = wy
	m.Click = inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
	m.Right = inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight)
	m.Double = false
	if m.Click {
		tps := ebiten.ActualTPS()
		if tps < 1 {
			tps = 60
		}
		near := abs(m.X-m.lastX) < 8 && abs(m.Y-m.lastY) < 8
		if m.lastTick > 0 && float64(m.tick-m.lastTick)/tps < 0.35 && near {
			m.Double = true
		}
		m.lastTick = m.tick
	}
	m.lastX, m.lastY = m.X, m.Y
}

// In reports whether the pointer is inside the rectangle.
func (m *Mouse) In(x, y, w, h float64) bool {
	return m.X >= x && m.X < x+w && m.Y >= y && m.Y < y+h
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
