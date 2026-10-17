package render

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Message is a centered pop-up (e.g. "STAR POWER READY").
type Message struct {
	Text  string
	Color color.RGBA
	At    float64 // song time it was posted
}

// HUDState is the scoreboard for one frame.
type HUDState struct {
	Now         float64
	Score       float64
	Streak      int
	Mult        int // including star power
	BaseMax     int // max multiplier without star power
	SPEnergy    float64
	SPActive    bool
	Rock        float64
	NoFail      bool
	Progress    float64 // 0..1 through the song
	SongTime    float64 // seconds into the song
	Length      float64 // song length in seconds (0 hides the times)
	Title       string
	Artist      string
	Charter     string
	TitleAlpha  float64
	Section     string
	SectionAt   float64
	Messages    []Message
	LastOffset  float64 // ms of the last hit
	LastHitAt   float64
	ShowTiming  bool
	Instrument  string
	Difficulty  string
	Hits, Total int
}

// drawProgress draws the song progress bar across the top of the whole
// screen, with the song length at the right and the current time riding
// the end of the bar.
func (u *HUD) drawProgress(dst *ebiten.Image, s *HUDState) {
	th := &DefaultTheme
	img, w, _, ox, _ := Backdrop(dst)
	p := math.Max(0, math.Min(1, s.Progress))
	var b Batch
	b.Rect(img, 0, 0, w, 5, color.RGBA{0xFF, 0xFF, 0xFF, 0x18}, color.RGBA{0xFF, 0xFF, 0xFF, 0x18})
	b.Rect(img, 0, 0, w*p, 5, th.Accent, th.Accent)
	b.Flush(img)
	if s.Length <= 0 {
		return
	}
	total := clockText(s.Length)
	tw, _ := Measure(total, CondBold, 22)
	Text(img, total, CondBold, 22, w-14, 10, WithAlpha(th.TextDim, 0.9), Right)
	cur := clockText(math.Max(0, math.Min(s.Length, s.SongTime)))
	cw, _ := Measure(cur, CondBold, 22)
	x := w*p + 8
	if limit := w - 14 - tw - 18 - cw; x > limit {
		x = limit
	}
	x = math.Max(6, x)
	// Step aside for the section name while it's showing.
	alpha := 0.85
	if a := sectionAlpha(s); a > 0 {
		sw, _ := Measure(s.Section, CondBold, 34)
		bx := ox + u.Highway.Cfg.CenterX - sw/2 - 12
		if x+cw > bx && x < bx+sw+24 {
			alpha *= 1 - a
		}
	}
	Text(img, cur, CondBold, 22, x, 10, WithAlpha(th.Text, alpha), Left)
}

// sectionAlpha is how visible the section name banner is (0 when hidden).
func sectionAlpha(s *HUDState) float64 {
	if s.Section == "" {
		return 0
	}
	dt := s.Now - s.SectionAt
	if dt < 0 || dt >= 2.5 {
		return 0
	}
	return math.Max(0, math.Min(1, math.Min(dt/0.2, (2.5-dt)/0.5)))
}

// clockText formats seconds as m:ss.
func clockText(sec float64) string {
	t := int(sec)
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// HUD draws the scoreboard around the highway.
type HUD struct {
	Highway *Highway
	batch   Batch
}

func multColor(m int, sp bool) color.RGBA {
	if sp {
		return DefaultTheme.StarPower
	}
	switch {
	case m >= 4:
		return color.RGBA{0xC0, 0x6C, 0xFF, 0xFF}
	case m == 3:
		return color.RGBA{0x4A, 0xE0, 0x6A, 0xFF}
	case m == 2:
		return color.RGBA{0xFF, 0x9A, 0x2E, 0xFF}
	}
	return color.RGBA{0xD8, 0xDC, 0xE6, 0xFF}
}

// Draw renders the HUD.
func (u *HUD) Draw(dst *ebiten.Image, s *HUDState) {
	th := &DefaultTheme
	hc := u.Highway.Cfg
	b := &u.batch
	leftX := hc.CenterX - hc.NearWidth/2 - 70
	rightX := hc.CenterX + hc.NearWidth/2 + 70
	baseY := hc.StrikeY - 150

	u.drawProgress(dst, s)

	// --- score panel (left) ---
	b.Flush(dst)
	RoundRect(dst, leftX-330, baseY-20, 340, 190, 18, WithAlpha(th.Panel, 0.8))
	TextShadow(dst, fmt.Sprintf("%d", int64(s.Score+0.5)), Display, 76, leftX-10, baseY-14, th.Text, Right)
	streakTxt := fmt.Sprintf("%d", s.Streak)
	Text(dst, "STREAK", Cond, 22, leftX-10, baseY+78, th.TextDim, Right)
	Text(dst, streakTxt, CondBold, 40, leftX-100, baseY+66, th.Text, Right)
	// rock meter
	rx, ry, rw := leftX-310, baseY+130, 300.0
	for i := 0; i < 30; i++ {
		t := float64(i) / 29
		var c color.RGBA
		switch {
		case t < 0.33:
			c = th.Bad
		case t < 0.66:
			c = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
		default:
			c = th.Good
		}
		a := 0.25
		if t <= s.Rock {
			a = 0.95
		}
		b.Rect(dst, rx+float64(i)*rw/30, ry, rw/30-2, 14, WithAlpha(c, a), WithAlpha(c, a*0.8))
	}
	needle := rx + rw*math.Max(0, math.Min(1, s.Rock))
	b.Rect(dst, needle-2, ry-5, 4, 24, th.Text, th.Text)

	// --- multiplier (right) ---
	mx, my := rightX+80, baseY+70
	mc := multColor(s.Mult/boolInt(s.SPActive, 2, 1), s.SPActive)
	b.Ellipse(dst, mx, my, 74, 74, WithAlpha(th.Panel, 0.95), WithAlpha(th.Panel, 0.95))
	segs := 10
	filled := s.Streak % 10
	base := s.Mult
	if s.SPActive {
		base /= 2
	}
	if base >= s.BaseMax {
		filled = segs
	}
	for i := 0; i < segs; i++ {
		a0 := -math.Pi/2 + float64(i)*2*math.Pi/float64(segs) + 0.05
		a1 := a0 + 2*math.Pi/float64(segs) - 0.1
		c := WithAlpha(th.TextDim, 0.25)
		if i < filled {
			c = mc
		}
		b.Arc(dst, mx, my, 70, 11, a0, a1, c)
	}
	b.Flush(dst)
	Text(dst, fmt.Sprintf("x%d", s.Mult), Display, 60, mx, my-38, mc, Center)

	// --- star power meter (right, under multiplier) ---
	sx, sy, sw := rightX, my+100, 160.0
	ready := s.SPEnergy >= 0.5 && !s.SPActive
	pulse := 0.0
	if ready {
		pulse = 0.5 + 0.5*math.Sin(s.Now*8)
	}
	for q := 0; q < 4; q++ {
		x := sx + float64(q)*(sw/4)
		fill := math.Max(0, math.Min(1, s.SPEnergy*4-float64(q)))
		b.Rect(dst, x, sy, sw/4-6, 16, WithAlpha(th.StarPower, 0.28), WithAlpha(th.StarPower, 0.18))
		if fill > 0 {
			c := Lighten(th.StarPower, pulse*0.5)
			b.Rect(dst, x, sy, (sw/4-6)*fill, 16, c, Darken(c, 0.2))
		}
	}
	b.Flush(dst)
	label := "STAR POWER"
	if s.SPActive {
		label = "STAR POWER ACTIVE"
	} else if ready {
		label = "STAR POWER READY"
	}
	Text(dst, label, Cond, 20, sx, sy+20, WithAlpha(th.StarPower, 0.6+0.4*pulse), Left)

	// --- title card ---
	if s.TitleAlpha > 0 {
		a := s.TitleAlpha
		RichText(dst, s.Title, UIBold, 52, 82, 73, color.RGBA{0, 0, 0, uint8(150 * a)}, Left, 900)
		RichText(dst, s.Title, UIBold, 52, 80, 70, WithAlpha(th.Text, a), Left, 900)
		RichText(dst, s.Artist, UI, 32, 82, 132, WithAlpha(th.TextDim, a), Left, 900)
		if s.Charter != "" {
			RichText(dst, "charted by "+s.Charter, Cond, 24, 82, 176, WithAlpha(th.TextDim, a*0.8), Left, 900)
		}
		Text(dst, s.Difficulty+" "+s.Instrument, CondBold, 26, 82, 210, WithAlpha(th.Accent, a), Left)
	}

	// --- section name ---
	if a := sectionAlpha(s); a > 0 {
		Text(dst, s.Section, CondBold, 34, hc.CenterX, 18, WithAlpha(th.Text, a*0.9), Center)
	}

	// --- timing feedback ---
	if s.ShowTiming {
		dt := s.Now - s.LastHitAt
		if dt >= 0 && dt < 0.45 {
			a := 1 - dt/0.45
			c := th.Good
			txt := fmt.Sprintf("%+.0f", s.LastOffset)
			switch {
			case s.LastOffset < -15:
				c = th.Early
			case s.LastOffset > 15:
				c = th.Late
			}
			Text(dst, txt, CondBold, 30, hc.CenterX, hc.StrikeY+40, WithAlpha(c, a), Center)
		}
	}

	// --- messages ---
	y := 330.0
	for _, m := range s.Messages {
		dt := s.Now - m.At
		if dt < 0 || dt > 1.6 {
			continue
		}
		a := math.Min(1, math.Min(dt/0.12, (1.6-dt)/0.4))
		sc := 1 + 0.15*math.Max(0, 0.15-dt)/0.15
		TextShadow(dst, m.Text, Display, 64*sc, hc.CenterX, y, WithAlpha(m.Color, a), Center)
		y += 74
	}
}

func boolInt(b bool, t, f int) int {
	if b {
		return t
	}
	return f
}

// Background draws the gameplay backdrop across the whole screen: the
// chosen background image, or a gradient with a soft glow behind the
// highway.
func Background(dst *ebiten.Image, now float64, sp bool) {
	drawBackground(dst, now, sp, true)
}

// MenuBackdrop draws the menu backdrop: the background image too when it's
// set to show in menus.
func MenuBackdrop(dst *ebiten.Image, now float64) {
	drawBackground(dst, now, false, bgMenus)
}

var (
	bgImage *ebiten.Image
	bgDim   float64
	bgMenus bool
)

// SetBackgroundImage sets the image drawn behind everything (nil for the
// default gradient), how much it's darkened (0..1) and whether menus use it.
func SetBackgroundImage(img *ebiten.Image, dim float64, menus bool) {
	bgImage, bgDim, bgMenus = img, dim, menus
}

func drawBackground(dst *ebiten.Image, now float64, sp, useImage bool) {
	th := &DefaultTheme
	img, w, h, ox, oy := Backdrop(dst)
	if useImage && bgImage != nil {
		iw, ih := float64(bgImage.Bounds().Dx()), float64(bgImage.Bounds().Dy())
		s := math.Max(w/iw, h/ih)
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Scale(s, s)
		op.GeoM.Translate((w-iw*s)/2, (h-ih*s)/2)
		img.DrawImage(bgImage, op)
		FillRect(img, 0, 0, w, h, color.RGBA{0, 0, 0, uint8(255 * math.Max(0, math.Min(1, bgDim)))})
		if sp {
			FillRect(img, 0, 0, w, h, color.RGBA{0x04, 0x18, 0x22, 0x30})
		}
		return
	}
	var b Batch
	b.Rect(img, 0, 0, w, h, th.BGTop, th.BGBottom)
	glow := WithAlpha(th.BGGlow, 0.31)
	if sp {
		glow = WithAlpha(Darken(th.StarPower, 0.45), 0.38)
	}
	pulse := 0.9 + 0.1*math.Sin(now*1.3)
	b.Ellipse(img, ox+ScreenW/2, oy+ScreenH*0.55, 900*pulse, 520*pulse, glow, color.RGBA{})
	b.Ellipse(img, ox+ScreenW/2, oy+ScreenH*1.05, 1200, 260, color.RGBA{0x00, 0x00, 0x00, 0x60}, color.RGBA{})
	b.Flush(img)
}
