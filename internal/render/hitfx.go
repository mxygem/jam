package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
)

// HitFx is one successful hit to animate. Effects are pure functions of the
// hit's time and lane, so they need no per-frame state, stay locked to the
// song clock and look the same in replays.
type HitFx struct {
	At   float64 // song (render) time of the hit
	Lane int     // chart lane
	SP   bool    // star power was active
}

// FxLevel sets how much happens on a hit.
type FxLevel int

const (
	FxFull    FxLevel = iota
	FxReduced         // punch, flash and ring; fewer sparks, no flames
	FxOff             // fret punch only
)

// ParseFxLevel reads a settings value ("full", "reduced", "off").
func ParseFxLevel(s string) FxLevel {
	switch s {
	case "reduced":
		return FxReduced
	case "off":
		return FxOff
	}
	return FxFull
}

func (l FxLevel) String() string {
	switch l {
	case FxReduced:
		return "reduced"
	case FxOff:
		return "off"
	}
	return "full"
}

// FxLife is how long a hit stays relevant to drawing.
const FxLife = 0.6

// Effect timings (seconds).
const (
	punchLife = 0.22
	flashLife = 0.18
	ringLife  = 0.26
	flameLife = 0.30
	sparkLife = 0.50
	pulseLife = 0.16
)

// hash01 is a stable pseudo random number in [0, 1) for a hit and a salt.
func hash01(at float64, lane, salt int) float64 {
	x := math.Float64bits(at) ^ uint64(lane+1)*0x9E3779B97F4A7C15 ^ uint64(salt+1)*0xBF58476D1CE4E5B9
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x>>11) / (1 << 53)
}

// punch is the fret target's springy kick for a hit dt seconds ago: how far
// it is pushed down (px, negative overshoots up) and how bright it is.
func punch(dt float64) (down, lit float64) {
	if dt < 0 || dt > punchLife {
		return 0, 0
	}
	down = 13 * math.Exp(-dt/0.045) * math.Cos(2*math.Pi*dt/0.17)
	lit = math.Exp(-dt / 0.07)
	return
}

// lastHit returns the time since the most recent hit on lane (or a large
// number when there is none).
func lastHit(hits []HitFx, lane int, now float64) float64 {
	best := math.Inf(1)
	for _, h := range hits {
		if h.Lane == lane {
			if dt := now - h.At; dt >= 0 && dt < best {
				best = dt
			}
		}
	}
	return best
}

// thump is how far the whole highway is knocked down by recent kicks (drums)
// or open notes (guitar): a short damped bounce. Hits in quick succession
// bounce less, so a run of open notes doesn't shake the highway.
func thump(hits []HitFx, lane int, now float64) float64 {
	last, prev := math.Inf(1), math.Inf(1)
	for _, h := range hits {
		if h.Lane != lane {
			continue
		}
		if dt := now - h.At; dt >= 0 {
			switch {
			case dt < last:
				last, prev = dt, last
			case dt < prev && dt != last:
				prev = dt
			}
		}
	}
	if last > 0.25 {
		return 0
	}
	amp := 6.0
	if gap := prev - last; gap < 0.4 {
		amp *= gap / 0.4 * gap / 0.4
	}
	return amp * math.Exp(-last/0.05) * math.Cos(2*math.Pi*last/0.2)
}

func easeOut(x float64) float64 { return 1 - (1-x)*(1-x) }

// fxTarget is where a lane's effects come from.
type fxTarget struct {
	cx, y   float64 // center on the strike line
	rx, ry  float64 // target radii
	span    float64 // half width for full-width notes (open, kick); 0 otherwise
	c       color.RGBA
	visible bool
}

// drawHitFx draws every live hit effect and the sustain sizzle.
func (h *Highway) drawHitFx(dst *ebiten.Image, f *FrameState, target func(lane int) fxTarget, th *Theme) {
	if f.Fx == FxOff {
		return
	}
	add := &h.fxBatch
	add.Additive = true
	sp := func(c color.RGBA, on bool) color.RGBA {
		if on {
			return Mix(c, th.StarPower, 0.6)
		}
		return c
	}

	for _, hit := range f.Hits {
		dt := f.Now - hit.At
		if dt < 0 || dt > FxLife {
			continue
		}
		tg := target(hit.Lane)
		if !tg.visible {
			continue
		}
		c := sp(tg.c, hit.SP)
		hot := Lighten(c, 0.55)

		// Strike line pulse under the lane.
		if dt < pulseLife {
			k := 1 - dt/pulseLife
			hw := tg.rx * 1.15
			if tg.span > 0 {
				hw = tg.span
			}
			add.Line(dst, tg.cx-hw, tg.y, tg.cx+hw, tg.y, 12*k+2, WithAlpha(hot, 0.75*k), WithAlpha(hot, 0.75*k))
		}

		// Flash: a bright bloom that swells and fades fast.
		if dt < flashLife {
			k := 1 - dt/flashLife
			g := Glow(c)
			op := &ebiten.DrawImageOptions{Blend: ebiten.BlendLighter}
			sx := tg.rx * (2.2 + 1.4*(1-k)) / 128
			sy := sx * 0.75
			if tg.span > 0 {
				sx = tg.span * (1.2 + 0.4*(1-k)) / 128
				sy = tg.rx * 1.6 / 128
			}
			op.GeoM.Scale(sx, sy)
			op.GeoM.Translate(tg.cx-128*sx, tg.y-128*sy)
			op.ColorScale.ScaleAlpha(float32(math.Pow(k, 1.1)))
			dst.DrawImage(g, op)
			dst.DrawImage(g, op) // twice: bloom, not haze
			// white-hot core right on the fret
			if kc := 1 - dt/0.09; tg.span == 0 && kc > 0 {
				cr := tg.rx * (0.9 + 0.5*(1-kc))
				add.Ellipse(dst, tg.cx, tg.y, cr, cr*0.55, WithAlpha(color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, 0.9*kc), WithAlpha(hot, 0))
			}
		}

		// Shockwave ring expanding from the target.
		if dt < ringLife && tg.span == 0 {
			e := easeOut(dt / ringLife)
			rx := tg.rx * (1.05 + 1.1*e)
			ry := tg.ry * (1.05 + 1.1*e)
			ring := WithAlpha(hot, 0.95*(1-e))
			add.Ring(dst, tg.cx, tg.y, rx, ry, 11*(1-e)+2, ring, ring)
		}

		// Flame jet (GH3 style), flickering, up from the fret.
		if f.Fx == FxFull && dt < flameLife && tg.span == 0 {
			env := math.Min(1, dt/0.035) * math.Pow(math.Max(0, 1-(dt-0.035)/(flameLife-0.035)), 1.3)
			fl := 0.85 + 0.15*math.Sin(f.Now*55+float64(hit.Lane)*1.7)
			h.flame(add, dst, tg.cx, tg.y+tg.ry*0.2, tg.rx*0.85*fl, tg.rx*4.6*env*fl, c, env)
		}

		// Sparks: streaks thrown up and out, pulled down by gravity.
		n := 18
		if tg.span > 0 {
			n = 28
		}
		if f.Fx == FxReduced {
			n /= 3
		}
		for i := 0; i < n; i++ {
			life := sparkLife * (0.45 + 0.55*hash01(hit.At, hit.Lane, i*4+3))
			if dt > life {
				continue
			}
			h.spark(add, dst, tg, hit.At, hit.Lane, i, dt, life, hot)
		}
	}

	// Sustain sizzle: sparks and a low flame while a sustain is held.
	if g, ok := f.Eng.(*engine.Guitar); ok && f.Fx == FxFull {
		lanes := g.SustainingLanes()
		for l := 0; l < chart.NumLanes; l++ {
			if lanes&(1<<l) == 0 {
				continue
			}
			tg := target(l)
			if !tg.visible {
				continue
			}
			c := sp(tg.c, f.SPActive)
			hot := Lighten(c, 0.55)
			boost := 1 + float64(f.Whammy)*0.6
			if tg.span == 0 {
				fl := 0.75 + 0.25*math.Sin(f.Now*47+float64(l)*2.1)*math.Sin(f.Now*13+float64(l))
				h.flame(add, dst, tg.cx, tg.y+tg.ry*0.2, tg.rx*0.6, tg.rx*2.2*fl*boost, c, 0.85)
				// steady glow under a held sustain
				add.Ellipse(dst, tg.cx, tg.y, tg.rx*1.1, tg.ry*1.1, WithAlpha(hot, 0.45*fl), WithAlpha(c, 0))
			}
			const slots, period = 9, 0.32
			for k := 0; k < slots; k++ {
				phase := f.Now + float64(k)*period/slots
				cycle := math.Floor(phase / period)
				age := phase - cycle*period
				seed := cycle*period + float64(k)
				life := period * (0.6 + 0.4*hash01(seed, l, 1))
				if age > life {
					continue
				}
				h.spark(add, dst, tg, seed, l, k, age, life, hot)
			}
		}
	}
	add.Flush(dst)
}

// spark draws spark i of a hit at age dt (of life seconds).
func (h *Highway) spark(b *Batch, dst *ebiten.Image, tg fxTarget, at float64, lane, i int, dt, life float64, c color.RGBA) {
	r1, r2, r3 := hash01(at, lane, i*4), hash01(at, lane, i*4+1), hash01(at, lane, i*4+2)
	ang := -math.Pi/2 + (r1-0.5)*1.9 // mostly up, fanning out
	speed := (420 + 680*r2) * (tg.rx / 50)
	vx, vy := math.Cos(ang)*speed, math.Sin(ang)*speed
	const g = 2100.0
	ox := tg.cx + (r3-0.5)*tg.rx*1.2
	if tg.span > 0 {
		ox = tg.cx + (r3*2-1)*tg.span
		vx *= 0.5
	}
	x := ox + vx*dt
	y := tg.y + vy*dt + 0.5*g*dt*dt
	// streak along the current velocity for a bit of motion blur
	cvx, cvy := vx, vy+g*dt
	tail := 0.016
	k := 1 - dt/life
	b.Line(dst, x, y, x-cvx*tail, y-cvy*tail, 3+3*k, WithAlpha(Lighten(c, 0.5), k), WithAlpha(c, 0))
	hr := 2.2 + 2.2*k
	b.Dot(dst, x, y, hr, WithAlpha(color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, k), WithAlpha(c, 0.6*k))
}

// flame draws a tapered, layered flame of base half-width w and height ht.
func (h *Highway) flame(b *Batch, dst *ebiten.Image, cx, y, w, ht float64, c color.RGBA, alpha float64) {
	if ht < 2 || alpha <= 0 {
		return
	}
	layers := []struct {
		w, h, a float64
		col     color.RGBA
	}{
		{1.0, 1.0, 0.70, c},
		{0.62, 0.80, 0.80, Lighten(c, 0.45)},
		{0.30, 0.55, 0.95, Lighten(c, 0.85)},
	}
	for _, L := range layers {
		bw, top := w*L.w, y-ht*L.h
		mid := y - ht*L.h*0.32
		base := WithAlpha(L.col, L.a*alpha)
		midc := WithAlpha(L.col, L.a*alpha*0.85)
		b.Quad(dst, cx-bw, y, cx+bw, y, cx+bw*1.12, mid, cx-bw*1.12, mid, base, base, midc, midc)
		b.Quad(dst, cx-bw*1.12, mid, cx+bw*1.12, mid, cx+bw*0.08, top, cx-bw*0.08, top, midc, midc, color.RGBA{}, color.RGBA{})
	}
}
