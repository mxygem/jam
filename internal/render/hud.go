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
// the end of the bar. It spans the screen, so it only moves up and down.
func (u *HUD) drawProgress(dst *ebiten.Image, s *HUDState) {
	ok, _, dy := u.place(HUDProgress)
	if !ok {
		return
	}
	th := &DefaultTheme
	img, w, _, ox, _ := Backdrop(dst)
	p := math.Max(0, math.Min(1, s.Progress))
	var b Batch
	b.Rect(img, 0, dy, w, 5, color.RGBA{0xFF, 0xFF, 0xFF, 0x18}, color.RGBA{0xFF, 0xFF, 0xFF, 0x18})
	b.Rect(img, 0, dy, w*p, 5, th.Accent, th.Accent)
	b.Flush(img)
	u.Boxes[HUDProgress] = Box{0, dy, ScreenW, 36}
	if s.Length <= 0 {
		return
	}
	total := clockText(s.Length)
	tw, _ := Measure(total, CondBold, 22)
	Text(img, total, CondBold, 22, w-14, dy+10, WithAlpha(th.TextDim, 0.9), Right)
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
		sok, sdx, sdy := u.place(HUDSection)
		sw, _ := Measure(s.Section, CondBold, 34)
		bx := ox + u.Highway.Cfg.CenterX + sdx - sw/2 - 12
		if sok && x+cw > bx && x < bx+sw+24 && math.Abs(sdy-dy) < 40 {
			alpha *= 1 - a
		}
	}
	Text(img, cur, CondBold, 22, x, dy+10, WithAlpha(th.Text, alpha), Left)
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
	// Layout places the items (nil: all at their defaults).
	Layout HUDLayout
	// ShowHidden is the layout editor's view: hidden items are drawn too,
	// and the lyrics, which Play draws itself, get a stand-in.
	ShowHidden bool
	// Boxes is where each item was drawn last frame, for the editor.
	Boxes map[string]Box
	// Inputs, if set, is the input viewer, drawn as of song time InputsAt.
	Inputs   *InputViewer
	InputsAt float64
}

// place returns whether to draw an item and its offset from its default
// spot. Items on the score panel move with it.
func (u *HUD) place(id string) (bool, float64, float64) {
	p := u.Layout.Get(id)
	dx, dy := p.DX, p.DY
	switch id {
	case HUDScore, HUDStreak, HUDRock:
		g := u.Layout.Get(HUDPanel)
		dx, dy = dx+g.DX, dy+g.DY
	}
	return !p.Hidden || u.ShowHidden, dx * ScreenW, dy * ScreenH
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
	if u.Boxes == nil {
		u.Boxes = map[string]Box{}
	}
	clear(u.Boxes)

	u.drawProgress(dst, s)

	// --- score panel (left) ---
	b.Flush(dst)
	if ok, dx, dy := u.place(HUDPanel); ok {
		box := Box{leftX - 330 + dx, baseY - 20 + dy, 340, 190}
		RoundRect(dst, box.X, box.Y, box.W, box.H, 18, WithAlpha(th.Panel, 0.8))
		u.Boxes[HUDPanel] = box
	}
	if ok, dx, dy := u.place(HUDScore); ok {
		x, y := leftX-10+dx, baseY-14+dy
		score := fmt.Sprintf("%d", int64(s.Score+0.5))
		TextShadow(dst, score, Display, 76, x, y, th.Text, Right)
		w, _ := Measure(score, Display, 76)
		u.Boxes[HUDScore] = Box{x - w, y, w, 84}
	}
	if ok, dx, dy := u.place(HUDStreak); ok {
		x, y := leftX-10+dx, baseY+dy
		streakTxt := fmt.Sprintf("%d", s.Streak)
		Text(dst, "STREAK", Cond, 22, x, y+78, th.TextDim, Right)
		Text(dst, streakTxt, CondBold, 40, x-90, y+66, th.Text, Right)
		w, _ := Measure(streakTxt, CondBold, 40)
		u.Boxes[HUDStreak] = Box{x - 90 - w, y + 66, 90 + w, 46}
	}
	// rock meter
	if ok, dx, dy := u.place(HUDRock); ok {
		rx, ry, rw := leftX-310+dx, baseY+130+dy, 300.0
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
		u.Boxes[HUDRock] = Box{rx, ry - 5, rw, 24}
	}
	b.Flush(dst)

	// --- multiplier (right) ---
	my0 := baseY + 70
	if ok, dx, dy := u.place(HUDMult); ok {
		mx, my := rightX+80+dx, my0+dy
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
		u.Boxes[HUDMult] = Box{mx - 74, my - 74, 148, 148}
	}

	// --- star power meter (right, under multiplier) ---
	if ok, dx, dy := u.place(HUDStarPower); ok {
		u.Boxes[HUDStarPower] = u.drawStarPower(dst, s, rightX, my0, dx, dy)
	}

	// --- title card ---
	if ok, dx, dy := u.place(HUDTitle); ok && s.TitleAlpha > 0 {
		a := s.TitleAlpha
		x, y := dx, dy
		RichText(dst, s.Title, UIBold, 52, x+82, y+73, color.RGBA{0, 0, 0, uint8(150 * a)}, Left, 900)
		RichText(dst, s.Title, UIBold, 52, x+80, y+70, WithAlpha(th.Text, a), Left, 900)
		RichText(dst, s.Artist, UI, 32, x+82, y+132, WithAlpha(th.TextDim, a), Left, 900)
		if s.Charter != "" {
			RichText(dst, "charted by "+s.Charter, Cond, 24, x+82, y+176, WithAlpha(th.TextDim, a*0.8), Left, 900)
		}
		Text(dst, s.Difficulty+" "+s.Instrument, CondBold, 26, x+82, y+210, WithAlpha(th.Accent, a), Left)
		u.Boxes[HUDTitle] = Box{x + 80, y + 70, 900, 174} // titles are cut off at 900
	}

	// --- section name ---
	if ok, dx, dy := u.place(HUDSection); ok {
		if a := sectionAlpha(s); a > 0 {
			x, y := hc.CenterX+dx, 18+dy
			Text(dst, s.Section, CondBold, 34, x, y, WithAlpha(th.Text, a*0.9), Center)
			w, _ := Measure(s.Section, CondBold, 34)
			u.Boxes[HUDSection] = Box{x - w/2, y, w, 42}
		}
	}

	// --- lyrics stand-in (Play draws the real ones under the highway's HUD) ---
	if u.ShowHidden {
		_, dx, dy := u.place(HUDLyrics)
		x, y := hc.CenterX+dx, SoloLyricsY+dy
		TextShadow(dst, "Lyrics show up here", UIBold, 44, x, y+10, WithAlpha(th.Text, 0.75), Center)
		u.Boxes[HUDLyrics] = Box{x - 500, y, 1000, 72}
	}

	// --- input viewer: beside the highway, above the score panel ---
	if u.Inputs != nil {
		if ok, dx, dy := u.place(HUDInputs); ok {
			iw, ih := u.Inputs.Size(1, false)
			x, y := hc.CenterX-hc.NearWidth/2-400+dx, hc.StrikeY-186-ih+dy
			u.Inputs.Draw(dst, x, y, 1, false, u.InputsAt)
			u.Boxes[HUDInputs] = Box{x, y, iw, ih}
		}
	}

	// --- timing feedback ---
	if ok, dx, dy := u.place(HUDTiming); ok && s.ShowTiming {
		t := s.Now - s.LastHitAt
		if t >= 0 && t < 0.45 {
			a := 1 - t/0.45
			c := th.Good
			txt := fmt.Sprintf("%+.0f", s.LastOffset)
			switch {
			case s.LastOffset < -15:
				c = th.Early
			case s.LastOffset > 15:
				c = th.Late
			}
			x, y := hc.CenterX+dx, hc.StrikeY+40+dy
			Text(dst, txt, CondBold, 30, x, y, WithAlpha(c, a), Center)
			u.Boxes[HUDTiming] = Box{x - 45, y, 90, 38}
		}
	}

	// --- messages ---
	if ok, dx, dy := u.place(HUDMessages); ok {
		x, y := hc.CenterX+dx, 330+dy
		box := Box{x - 300, y, 600, 74}
		for _, m := range s.Messages {
			t := s.Now - m.At
			if t < 0 || t > 1.6 {
				continue
			}
			a := math.Min(1, math.Min(t/0.12, (1.6-t)/0.4))
			sc := 1 + 0.15*math.Max(0, 0.15-t)/0.15
			TextShadow(dst, m.Text, Display, 64*sc, x, y, WithAlpha(m.Color, a), Center)
			w, _ := Measure(m.Text, Display, 64*sc)
			box = box.union(Box{x - w/2, y, w, 74})
			y += 74
		}
		u.Boxes[HUDMessages] = box
	}
}

// drawStarPower draws the star power meter: by default a row of four
// segments under the multiplier; stood up, a column above it.
func (u *HUD) drawStarPower(dst *ebiten.Image, s *HUDState, rightX, my, dx, dy float64) Box {
	th := &DefaultTheme
	b := &u.batch
	ready := s.SPEnergy >= 0.5 && !s.SPActive
	pulse := 0.0
	if ready {
		pulse = 0.5 + 0.5*math.Sin(s.Now*8)
	}
	label := "STAR POWER"
	if s.SPActive {
		label = "STAR POWER ACTIVE"
	} else if ready {
		label = "STAR POWER READY"
	}
	labelC := WithAlpha(th.StarPower, 0.6+0.4*pulse)
	fillC := Lighten(th.StarPower, pulse*0.5)
	seg := func(q int) float64 { return math.Max(0, math.Min(1, s.SPEnergy*4-float64(q))) }

	if u.Layout.Get(HUDStarPower).Vertical {
		sw, sh := 24.0, 230.0
		sx, sy := rightX+68+dx, my-74-34-sh+dy
		gap := 6.0
		qh := (sh - 3*gap) / 4
		for q := 0; q < 4; q++ {
			y := sy + sh - float64(q+1)*qh - float64(q)*gap
			b.Rect(dst, sx, y, sw, qh, WithAlpha(th.StarPower, 0.28), WithAlpha(th.StarPower, 0.18))
			if f := seg(q); f > 0 {
				b.Rect(dst, sx, y+qh*(1-f), sw, qh*f, fillC, Darken(fillC, 0.2))
			}
		}
		b.Flush(dst)
		short := "SP"
		if s.SPActive {
			short = "ACTIVE"
		} else if ready {
			short = "READY"
		}
		Text(dst, short, CondBold, 20, sx+sw/2, sy+sh+6, labelC, Center)
		lw, _ := Measure(short, CondBold, 20)
		w := math.Max(sw, lw)
		return Box{sx + sw/2 - w/2, sy, w, sh + 32}
	}

	sx, sy, sw := rightX+dx, my+100+dy, 160.0
	for q := 0; q < 4; q++ {
		x := sx + float64(q)*(sw/4)
		b.Rect(dst, x, sy, sw/4-6, 16, WithAlpha(th.StarPower, 0.28), WithAlpha(th.StarPower, 0.18))
		if f := seg(q); f > 0 {
			b.Rect(dst, x, sy, (sw/4-6)*f, 16, fillC, Darken(fillC, 0.2))
		}
	}
	b.Flush(dst)
	Text(dst, label, Cond, 20, sx, sy+20, labelC, Left)
	lw, _ := Measure(label, Cond, 20)
	return Box{sx, sy, math.Max(sw, lw), 46}
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
