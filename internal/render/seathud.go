package render

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// SeatH is the height of one player's view in multiplayer. Each view is
// drawn at this height and scaled into its place on the screen.
const SeatH = 1080.0

// SeatInfo is what a multiplayer view shows besides the scoreboard.
type SeatInfo struct {
	Name     string
	Color    color.RGBA
	Part     string // "Expert Guitar"
	Rank     int    // head to head: 1 for the leader (0 hides it)
	ShowRock bool   // the player can fail
	// Overlay, if set, covers the highway: "OUT", "FAILED", "No drums part"...
	Overlay, OverlaySub string
	Dim                 float64 // 0..1 darkening of the highway under the overlay
	// Banner, if set, is a notice in a pill near the top of the highway
	// (a party skip vote).
	Banner string
}

// SeatHighway fits a player's highway shape into a view vw wide.
func SeatHighway(base HighwayConfig, vw float64) HighwayConfig {
	hc := base
	if hc.NearWidth == 0 {
		hc = DefaultHighway()
	}
	hc.CenterX = vw / 2
	hc.NearWidth = math.Min(hc.NearWidth, vw*0.86)
	hc.StrikeY = math.Min(hc.StrikeY, SeatH-135)
	hc.TopY = math.Max(hc.TopY, 118)
	if hc.TopY > hc.StrikeY-150 {
		hc.TopY = hc.StrikeY - 150
	}
	return hc
}

// SeatHUD draws one player's scoreboard around their highway: name and score
// above, multiplier, streak, star power and rock meter below the strike line.
type SeatHUD struct {
	batch Batch
}

// Draw draws the HUD for a highway shaped hc.
func (u *SeatHUD) Draw(dst *ebiten.Image, hc HighwayConfig, s *HUDState, in *SeatInfo) {
	th := &DefaultTheme
	b := &u.batch
	cx, nw := hc.CenterX, hc.NearWidth
	k := math.Min(1.25, math.Max(0.45, nw/440))
	left, right := cx-nw/2, cx+nw/2

	// --- overlay (out, failed, sitting out) under the HUD ---
	if in.Overlay != "" {
		if in.Dim > 0 {
			FillRect(dst, left-nw*0.1, hc.TopY-20, nw*1.2, hc.StrikeY-hc.TopY+60, color.RGBA{0, 0, 0, uint8(200 * in.Dim)})
		}
		TextShadow(dst, in.Overlay, Display, 72*k, cx, hc.TopY+(hc.StrikeY-hc.TopY)*0.32, th.Text, Center)
		if in.OverlaySub != "" {
			drawWrappedCenter(dst, in.OverlaySub, UI, 30*k, cx, hc.TopY+(hc.StrikeY-hc.TopY)*0.32+90*k, nw*1.05, th.TextDim)
		}
	}

	// --- name tag and score (top) ---
	ty := 18.0
	b.Ellipse(dst, left+14*k, ty+24*k, 11*k, 11*k, in.Color, in.Color)
	b.Flush(dst)
	name := []rune(in.Name)
	nameW := nw * 0.55
	for len(name) > 1 {
		if w, _ := Measure(string(name), CondBold, 38*k); w <= nameW {
			break
		}
		name = name[:len(name)-1]
	}
	TextShadow(dst, string(name), CondBold, 38*k, left+32*k, ty, th.Text, Left)
	Text(dst, in.Part, Cond, 24*k, left+32*k, ty+46*k, th.TextDim, Left)
	TextShadow(dst, fmt.Sprintf("%d", int64(s.Score+0.5)), Display, 50*k, right, ty-4, th.Text, Right)
	if in.Rank > 0 {
		c := th.TextDim
		if in.Rank == 1 {
			c = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
		}
		Text(dst, ordinalText(in.Rank), CondBold, 28*k, right, ty+52*k, c, Right)
	}
	FillRect(dst, left, ty+92*k, nw, 4*k, WithAlpha(in.Color, 0.85))

	// --- under the strike line ---
	y0 := hc.StrikeY + 30
	// multiplier ring (left)
	mr := math.Min(42*k, (SeatH-y0)/2-8)
	mx, my := left+50*k, y0+mr+4
	base := s.Mult
	if s.SPActive {
		base /= 2
	}
	mc := multColor(base, s.SPActive)
	b.Ellipse(dst, mx, my, mr+4*k, mr+4*k, WithAlpha(th.Panel, 0.95), WithAlpha(th.Panel, 0.95))
	segs := 10
	filled := s.Streak % 10
	if base >= s.BaseMax {
		filled = segs
	}
	for i := 0; i < segs; i++ {
		a0 := -math.Pi/2 + float64(i)*2*math.Pi/float64(segs) + 0.06
		a1 := a0 + 2*math.Pi/float64(segs) - 0.12
		c := WithAlpha(th.TextDim, 0.25)
		if i < filled {
			c = mc
		}
		b.Arc(dst, mx, my, mr, 8*k, a0, a1, c)
	}
	b.Flush(dst)
	Text(dst, fmt.Sprintf("x%d", s.Mult), Display, 40*k*mr/(42*k), mx, my-25*k*mr/(42*k), mc, Center)

	// streak and timing (middle)
	Text(dst, fmt.Sprintf("%d", s.Streak), CondBold, 40*k, cx, y0+2*k, th.Text, Center)
	Text(dst, "STREAK", Cond, 18*k, cx, y0+50*k, th.TextDim, Center)
	if s.ShowTiming {
		dt := s.Now - s.LastHitAt
		if dt >= 0 && dt < 0.45 {
			a := 1 - dt/0.45
			c := th.Good
			switch {
			case s.LastOffset < -15:
				c = th.Early
			case s.LastOffset > 15:
				c = th.Late
			}
			Text(dst, fmt.Sprintf("%+.0f", s.LastOffset), CondBold, 28*k, cx, hc.StrikeY-58*k, WithAlpha(c, a), Center)
		}
	}

	// star power (right)
	sw := 132 * k
	sx, sy := right-sw, y0+24*k
	ready := s.SPEnergy >= 0.5 && !s.SPActive
	pulse := 0.0
	if ready || s.SPActive {
		pulse = 0.5 + 0.5*math.Sin(s.Now*8)
	}
	for q := 0; q < 4; q++ {
		x := sx + float64(q)*(sw/4)
		fill := math.Max(0, math.Min(1, s.SPEnergy*4-float64(q)))
		b.Rect(dst, x, sy, sw/4-5*k, 14*k, WithAlpha(th.StarPower, 0.28), WithAlpha(th.StarPower, 0.18))
		if fill > 0 {
			c := Lighten(th.StarPower, pulse*0.5)
			b.Rect(dst, x, sy, (sw/4-5*k)*fill, 14*k, c, Darken(c, 0.2))
		}
	}
	b.Flush(dst)
	label := "STAR POWER"
	switch {
	case s.SPActive:
		label = "ACTIVE"
	case ready:
		label = "READY"
	}
	Text(dst, label, Cond, 18*k, right, sy+20*k, WithAlpha(th.StarPower, 0.6+0.4*pulse), Right)

	// rock meter (bottom edge)
	if in.ShowRock {
		ry := SeatH - 16*k
		c := th.Good
		switch {
		case s.Rock < 0.33:
			c = th.Bad
		case s.Rock < 0.66:
			c = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
		}
		FillRect(dst, left, ry, nw, 8*k, WithAlpha(th.TextDim, 0.25))
		FillRect(dst, left, ry, nw*math.Max(0, math.Min(1, s.Rock)), 8*k, c)
	}

	// --- banner ---
	if in.Banner != "" {
		size := 28 * k
		w, _ := Measure(in.Banner, CondBold, size)
		if w > nw*1.05 {
			size *= nw * 1.05 / w
			w = nw * 1.05
		}
		by := ty + 112*k
		RoundRect(dst, cx-w/2-18*k, by, w+36*k, size*1.5, size*0.75, WithAlpha(th.Accent, 0.92))
		Text(dst, in.Banner, CondBold, size, cx, by+size*0.22, th.Text, Center)
	}

	// --- messages over the highway ---
	y := hc.TopY + (hc.StrikeY-hc.TopY)*0.22
	for _, m := range s.Messages {
		dt := s.Now - m.At
		if dt < 0 || dt > 1.6 {
			continue
		}
		a := math.Min(1, math.Min(dt/0.12, (1.6-dt)/0.4))
		sc := 1 + 0.15*math.Max(0, 0.15-dt)/0.15
		size := 50 * k * sc
		if w, _ := Measure(m.Text, Display, size); w > nw*1.1 {
			size *= nw * 1.1 / w
		}
		TextShadow(dst, m.Text, Display, size, cx, y, WithAlpha(m.Color, a), Center)
		y += size * 1.15
	}
}

func ordinalText(n int) string {
	suf := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suf = "st"
	case n%10 == 2:
		suf = "nd"
	case n%10 == 3:
		suf = "rd"
	}
	return fmt.Sprintf("%d%s", n, suf)
}

// drawWrappedCenter draws centered text wrapped to width w.
func drawWrappedCenter(dst *ebiten.Image, s string, f Font, size, cx, y, w float64, c color.Color) {
	line := ""
	flush := func() {
		if line != "" {
			Text(dst, line, f, size, cx, y, c, Center)
			y += size * 1.25
			line = ""
		}
	}
	word := ""
	for _, r := range s + " " {
		if r != ' ' {
			word += string(r)
			continue
		}
		if word == "" {
			continue
		}
		try := word
		if line != "" {
			try = line + " " + word
		}
		if tw, _ := Measure(try, f, size); tw > w && line != "" {
			flush()
			line = word
		} else {
			line = try
		}
		word = ""
	}
	flush()
}
