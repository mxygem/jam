package render

import (
	"image/color"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
)

const zMin = -0.14 // how far past the strike line notes keep scrolling

// Highway draws the note highway in perspective.
type Highway struct {
	Cfg     HighwayConfig
	Theme   *Theme
	batch   Batch
	fxBatch Batch
	beats   []chart.BeatLine
}

// FrameState is everything the highway needs for one frame.
type FrameState struct {
	Now        float64 // song time to display
	Chart      *chart.Chart
	Track      *chart.Track
	Eng        engine.Engine
	Held       uint8      // guitar: frets held
	PadHit     [7]float64 // drums: last pad hit time per chart lane
	Flash      [7]float64 // last successful hit per chart lane (song time)
	Whammy     float32    //
	SPActive   bool       //
	MissFlash  float64    // last miss/overstrum (song time)
	ExpertPlus bool       //
	Hits       []HitFx    // recent successful hits, for hit effects
	Fx         FxLevel    // how much the hit effects do
	Bounce     float64    // 0..1: how much open notes and kicks bounce the highway
}

func (h *Highway) far() float64 { return h.Cfg.FarRatio / (1 - h.Cfg.FarRatio) }

// Scale returns the perspective scale at depth z (1 at the strike line).
func (h *Highway) Scale(z float64) float64 {
	d := h.far()
	if z < -d*0.9 {
		z = -d * 0.9
	}
	return d / (d + z)
}

func (h *Highway) horizon() float64 {
	r := h.Cfg.FarRatio
	return (h.Cfg.TopY - h.Cfg.StrikeY*r) / (1 - r)
}

// Y returns the screen y of depth z.
func (h *Highway) Y(z float64) float64 {
	hz := h.horizon()
	return hz + (h.Cfg.StrikeY-hz)*h.Scale(z)
}

// X returns the screen x of a lane position (0 = left edge, lanes = right edge).
func (h *Highway) X(pos float64, lanes int, z float64) float64 {
	lw := h.Cfg.NearWidth / float64(lanes)
	return h.Cfg.CenterX + (pos-float64(lanes)/2)*lw*h.Scale(z)
}

// Z converts a time offset to depth.
func (h *Highway) Z(dt float64) float64 { return dt / h.Cfg.Lookahead }

func fadeFar(z float64) float64 {
	if z > 0.86 {
		return math.Max(0, (1-z)/0.14)
	}
	return 1
}

func fadePast(z float64) float64 {
	if z < 0 {
		return math.Max(0, 1-z/zMin)
	}
	return 1
}

// lanesFor returns the number of visual lanes and the chart-lane -> visual
// position mapping (-1 for lanes drawn as bars: open notes, kicks).
func lanesFor(t *chart.Track) (int, [7]int, [7]color.RGBA, *Theme) {
	th := &DefaultTheme
	var pos [7]int
	var col [7]color.RGBA
	for i := range pos {
		pos[i] = -1
	}
	if t.Key.Inst.IsDrums() {
		col[chart.DrumKick] = th.Drums[0]
		if t.DrumMode == chart.DrumsFiveLane {
			for i, l := range []int{1, 2, 3, 4, 5} {
				pos[l] = i
			}
			col[1], col[2], col[3], col[4], col[5] = th.Drums[1], th.Drums[2], th.Drums[3], th.DrumOrange, th.Drums[5]
			return 5, pos, col, th
		}
		for i, l := range []int{1, 2, 3, 4} {
			pos[l] = i
		}
		col[1], col[2], col[3], col[4] = th.Drums[1], th.Drums[2], th.Drums[3], th.Drums[4]
		return 4, pos, col, th
	}
	for l := 0; l < 5; l++ {
		pos[l] = l
		col[l] = th.Lanes[l]
	}
	col[chart.LaneOpen] = th.Lanes[5]
	return 5, pos, col, th
}

// Draw renders the highway, notes and strike line.
func (h *Highway) Draw(dst *ebiten.Image, f *FrameState) {
	if h.Theme == nil {
		h.Theme = &DefaultTheme
	}
	t := f.Track
	lanes, pos, col, th := lanesFor(t)
	drums := t.Key.Inst.IsDrums()
	b := &h.batch

	// Kicks (drums) and open notes (guitar) knock the whole highway down a
	// touch and let it bounce back.
	thumpLane := chart.LaneOpen
	if drums {
		thumpLane = chart.DrumKick
	}
	if d := thump(f.Hits, thumpLane, f.Now) * f.Bounce; d != 0 && f.Fx != FxOff {
		saved := h.Cfg
		h.Cfg.StrikeY += d
		h.Cfg.TopY += d * 0.35
		defer func() { h.Cfg = saved }()
	}

	// --- surface ---
	left := func(z float64) float64 { return h.X(0, lanes, z) }
	right := func(z float64) float64 { return h.X(float64(lanes), lanes, z) }
	lk := CurrentLook
	surf := th.Highway
	if f.SPActive {
		surf = Mix(surf, th.SPHighway, 0.6)
	}
	opacity := lk.HighwayOpacity
	near, far := surf, surf
	switch lk.Highway {
	case HwGradient:
		near, far = Lighten(surf, 0.16), Darken(surf, 0.45)
	case HwGlass:
		near, far = Lighten(surf, 0.08), surf
		opacity *= 0.85
	}
	steps := []float64{zMin, 0, 0.5, 0.86, 1}
	for i := 0; i < len(steps)-1; i++ {
		z0, z1 := steps[i], steps[i+1]
		s0, s1 := Mix(near, far, math.Max(0, z0)), Mix(near, far, z1)
		c0, c1 := WithAlpha(s0, fadeFar(z0)*fadePast(z0)*opacity), WithAlpha(s1, fadeFar(z1)*fadePast(z1)*opacity)
		b.Quad(dst, left(z0), h.Y(z0), right(z0), h.Y(z0), right(z1), h.Y(z1), left(z1), h.Y(z1), c0, c0, c1, c1)
	}
	if lk.Texture != TextureNone && lk.TextureStrength > 0 {
		if tex := loadTexture(lk.Texture); tex != nil {
			b.Flush(dst)
			h.drawTexture(dst, tex, lanes, f.Now, surf, th.Beat, th.SPHighway, f.SPActive, lk.TextureStrength*opacity)
		}
	}
	switch lk.Highway {
	case HwLanes:
		// each lane tinted in its note color, strongest near the strike line
		lc := make([]color.RGBA, lanes)
		for l, p := range pos {
			if p >= 0 && p < lanes {
				lc[p] = col[l]
			}
		}
		for l := 0; l < lanes; l++ {
			n0, n1 := WithAlpha(lc[l], 0.22*opacity), WithAlpha(lc[l], 0.06*opacity)
			fl := float64(l)
			b.Quad(dst, h.X(fl, lanes, zMin), h.Y(zMin), h.X(fl+1, lanes, zMin), h.Y(zMin),
				h.X(fl+1, lanes, 0.86), h.Y(0.86), h.X(fl, lanes, 0.86), h.Y(0.86), n0, n0, n1, n1)
		}
	case HwGlass:
		// sheen along both rails and down the middle
		for _, e := range [][2]float64{{0, 0.45}, {float64(lanes), float64(lanes) - 0.45}} {
			sh := WithAlpha(white, 0.10)
			b.Quad(dst, h.X(e[0], lanes, 0), h.Y(0), h.X(e[1], lanes, 0), h.Y(0),
				h.X(e[1], lanes, 0.86), h.Y(0.86), h.X(e[0], lanes, 0.86), h.Y(0.86), sh, color.RGBA{}, color.RGBA{}, WithAlpha(white, 0.02))
		}
		mid := float64(lanes) / 2
		gl := WithAlpha(white, 0.05)
		b.Quad(dst, h.X(mid-0.6, lanes, 0), h.Y(0), h.X(mid+0.6, lanes, 0), h.Y(0),
			h.X(mid+0.6, lanes, 0.86), h.Y(0.86), h.X(mid-0.6, lanes, 0.86), h.Y(0.86), color.RGBA{}, gl, color.RGBA{}, color.RGBA{})
	default:
		// Alternate lane tint for depth.
		for l := 1; l < lanes; l += 2 {
			z0, z1 := 0.0, 1.0
			tint := color.RGBA{0xFF, 0xFF, 0xFF, 0x06}
			b.Quad(dst, h.X(float64(l), lanes, z0), h.Y(z0), h.X(float64(l+1), lanes, z0), h.Y(z0),
				h.X(float64(l+1), lanes, z1), h.Y(z1), h.X(float64(l), lanes, z1), h.Y(z1), tint, tint, color.RGBA{}, color.RGBA{})
		}
	}

	// --- beat lines ---
	h.beats = f.Chart.Tempo.BeatLines(h.beats[:0], f.Now+zMin*h.Cfg.Lookahead, f.Now+h.Cfg.Lookahead)
	for _, bl := range h.beats {
		z := h.Z(bl.Time - f.Now)
		a := fadeFar(z) * fadePast(z)
		w := 1.2
		alpha := 0.12
		switch bl.Kind {
		case chart.BeatMeasure:
			w, alpha = 4.5, 0.55
		case chart.BeatStrong:
			w, alpha = 2.2, 0.30
		}
		s := h.Scale(z)
		y := h.Y(z)
		c := WithAlpha(th.Beat, math.Min(1, alpha*lk.BeatLines)*a)
		if lk.BeatLines > 0 {
			b.Line(dst, left(z), y, right(z), y, w*s, c, c)
		}
	}

	// --- lane dividers & rails ---
	for l := 1; l < lanes; l++ {
		x0, y0 := h.X(float64(l), lanes, zMin), h.Y(zMin)
		x1, y1 := h.X(float64(l), lanes, 1), h.Y(1)
		if lk.LaneLines > 0 {
			b.Line(dst, x0, y0, x1, y1, 1.6, WithAlpha(th.Beat, math.Min(1, 0.10*lk.LaneLines)), color.RGBA{})
		}
	}
	rail := th.Rail
	if f.SPActive {
		rail = th.RailSP
	}
	if f.Now-f.MissFlash < 0.15 {
		rail = Mix(rail, th.Bad, 1-(f.Now-f.MissFlash)/0.15)
	}
	for _, side := range []float64{0, float64(lanes)} {
		x0, y0 := h.X(side, lanes, zMin), h.Y(zMin)
		x1, y1 := h.X(side, lanes, 1), h.Y(1)
		off := -1.0
		if side > 0 {
			off = 1
		}
		b.Line(dst, x0+off*5, y0, x1+off*2, y1, 18, WithAlpha(rail, 0.10), color.RGBA{})
		b.Line(dst, x0+off*2, y0, x1+off*1, y1, 5, WithAlpha(rail, 0.95), WithAlpha(rail, 0.0))
	}

	// --- strike line and targets (under the gems) ---
	ys := h.Y(0)
	lw := h.Cfg.NearWidth / float64(lanes)
	b.Line(dst, left(0)-6, ys, right(0)+6, ys, 6, WithAlpha(th.Rail, 0.55), WithAlpha(th.Rail, 0.55))
	for l := 0; l < 7; l++ {
		p := pos[l]
		if p < 0 {
			continue
		}
		cx := h.X(float64(p)+0.5, lanes, 0)
		c := col[l]
		rx := lw * 0.40
		ry := rx * 0.50
		depth := rx * 0.24 // how far the button stands up from its socket
		pressed := false
		if drums {
			pressed = f.Now-f.PadHit[l] < 0.09 && f.Now >= f.PadHit[l]
		} else {
			pressed = f.Held&(1<<l) != 0
		}
		dy := 0.0
		if pressed {
			dy = depth * 0.45
		}
		// A hit kicks the target down and lets it spring back.
		down, lit := punch(lastHit(f.Hits, l, f.Now))
		dy += down * 0.6
		grow := 1 + 0.12*lit
		rx, ry = rx*grow, ry*grow
		top := ys + dy
		rimC, _ := lk.Rim.colors(c)
		switch lk.Material {
		case MatFlat:
			// flat: a ring on the highway, filled while pressed
			if pressed || lit > 0.05 {
				b.Ellipse(dst, cx, ys, rx, ry, Lighten(c, 0.35+0.45*lit), Lighten(c, 0.25*lit))
			} else {
				b.Ellipse(dst, cx, ys, rx, ry, WithAlpha(Darken(c, 0.6), 0.85), WithAlpha(Darken(c, 0.6), 0.85))
			}
			b.Ring(dst, cx, ys, rx, ry, 7, rimC, rimC)
			b.Ring(dst, cx, ys, rx*0.55, ry*0.55, 4, WithAlpha(c, 0.9), WithAlpha(c, 0.9))
			continue
		case MatNeon:
			dark := color.RGBA{0x0A, 0x0A, 0x12, 0xFF}
			b.Ellipse(dst, cx, ys, rx+12, ry+10, WithAlpha(c, 0.25+0.35*lit), WithAlpha(c, 0))
			fill := dark
			if pressed || lit > 0.05 {
				fill = Mix(dark, c, 0.5+0.4*lit)
			}
			b.Ellipse(dst, cx, top, rx, ry, fill, Mix(fill, c, 0.3))
			b.Ring(dst, cx, top, rx, ry, 7, Lighten(c, 0.5+0.4*lit), c)
			b.Ring(dst, cx, top, rx*0.55, ry*0.55, 4, Lighten(c, 0.3), c)
			continue
		}
		// socket the button sits in
		b.Ellipse(dst, cx+4, ys+depth+7, rx+8, ry+7, WithAlpha(black, 0.55), color.RGBA{})
		b.Ellipse(dst, cx, ys+depth, rx+3, ry+2, Darken(c, 0.85), Darken(c, 0.9))
		// side wall, shorter while pushed in
		b.CylinderSide(dst, cx, top, rx, ry, math.Max(1, depth-dy), Darken(c, 0.78), Darken(c, 0.2))
		if pressed || lit > 0.05 {
			b.Ellipse(dst, cx, top, rx, ry, Lighten(c, 0.45+0.45*lit), Lighten(c, 0.35*lit))
			b.Ring(dst, cx, top, rx, ry, 6, Lighten(rimC, 0.5*lit), Lighten(c, 0.6))
			b.EllipseArc(dst, cx, top, rx, ry, 4, litFrom, litTo, WithAlpha(white, 0.8))
		} else {
			// colored bezel around a recessed dark center
			bezel0, bezel1 := Lighten(c, 0.2), Darken(c, 0.15)
			if lk.Rim != RimSilver {
				bezel0, bezel1 = lk.Rim.colors(c)
			}
			b.Ellipse(dst, cx, top, rx, ry, Darken(c, 0.3), Darken(c, 0.5))
			b.Ring(dst, cx, top, rx, ry, 8, bezel0, bezel1)
			if lk.Material != MatMatte {
				b.EllipseArc(dst, cx, top, rx, ry, 4, litFrom, litTo, WithAlpha(white, 0.65))
			}
			b.EllipseArc(dst, cx, top, rx-4, ry-2, 3, shadeFrom, shadeTo, WithAlpha(black, 0.4))
			irx, iry := rx*0.55, ry*0.55
			b.Ellipse(dst, cx, top, irx, iry, capDark, Darken(c, 0.62))
			b.EllipseArc(dst, cx, top, irx, iry, irx*0.22, -math.Pi*0.95, -math.Pi*0.05, WithAlpha(black, 0.6))
			b.EllipseArc(dst, cx, top, irx, iry, 2.5, math.Pi*0.15, math.Pi*0.85, WithAlpha(Lighten(c, 0.4), 0.6))
		}
	}
	if drums {
		// Kick target: a bar along the strike line.
		kc := col[chart.DrumKick]
		a := 0.55
		if f.Now-f.PadHit[chart.DrumKick] < 0.09 && f.Now >= f.PadHit[chart.DrumKick] {
			a = 1
		}
		if _, lit := punch(lastHit(f.Hits, chart.DrumKick, f.Now)); lit > 0 {
			kc = Lighten(kc, 0.5*lit)
			a = 1
		}
		b.Line(dst, left(0)+4, ys+lw*0.26, right(0)-4, ys+lw*0.26, 8, WithAlpha(kc, a), WithAlpha(kc, a))
	}
	b.Flush(dst)

	// --- notes ---
	notes := t.Notes
	tFrom := f.Now + zMin*h.Cfg.Lookahead
	tTo := f.Now + h.Cfg.Lookahead
	first := sort.Search(len(notes), func(i int) bool { return notes[i].SustainEnd() >= tFrom })
	last := sort.Search(len(notes), func(i int) bool { return notes[i].Time > tTo })

	var gsus *engine.Guitar
	if g, ok := f.Eng.(*engine.Guitar); ok {
		gsus = g
	}
	var dEng *engine.Drums
	if d, ok := f.Eng.(*engine.Drums); ok {
		dEng = d
	}

	// Sustains first so gems sit on top of their tails.
	if !drums {
		for i := first; i < last; i++ {
			n := &notes[i]
			if !n.HasSustain() {
				continue
			}
			st := f.Eng.Status(i)
			held := uint8(0)
			if gsus != nil {
				held = gsus.Sustaining(i)
			}
			for l := 0; l < chart.NumLanes; l++ {
				if n.End[l] <= 0 {
					continue
				}
				on := held&(1<<l) != 0
				startT := n.Time
				if on {
					startT = math.Max(n.Time, f.Now)
				} else if st == engine.NoteHit {
					continue // dropped or finished: tail disappears
				}
				z0 := math.Max(h.Z(startT-f.Now), zMin)
				z1 := math.Min(h.Z(n.End[l]-f.Now), 1)
				if z1 <= z0 {
					continue
				}
				c := col[l]
				if f.Eng.SPAlive(i) || (on && f.SPActive) {
					c = th.StarPower
				}
				alpha := 0.9
				if st == engine.NoteMissed {
					c = Mix(c, color.RGBA{0x50, 0x50, 0x58, 0xFF}, 0.7)
					alpha = 0.5
				}
				if on {
					c = Lighten(c, 0.35)
				}
				if l == chart.LaneOpen {
					h.drawSustain(dst, 0, float64(lanes), lanes, z0, z1, c, alpha*0.6, 0.30, on, f)
				} else {
					p := float64(pos[l])
					h.drawSustain(dst, p+0.5, p+0.5, lanes, z0, z1, c, alpha, 0.17, on, f)
				}
			}
		}
		b.Flush(dst)
	}

	// Gems, far to near.
	for i := last - 1; i >= first; i-- {
		n := &notes[i]
		z := h.Z(n.Time - f.Now)
		if z < zMin || z > 1 {
			continue
		}
		st := f.Eng.Status(i)
		hitLanes := f.Eng.LaneHit(i)
		if !drums && st == engine.NoteHit {
			continue
		}
		a := fadeFar(z) * fadePast(z)
		missed := st == engine.NoteMissed || (drums && z < 0 && st != engine.Pending)
		if missed {
			a *= 0.45
		}
		sp := f.Eng.SPAlive(i)
		s := h.Scale(z)
		y := h.Y(z)
		if drums {
			lanesMask := n.Lanes
			if dEng != nil {
				lanesMask = dEng.Playable(i)
			}
			if lanesMask&1 != 0 && hitLanes&1 == 0 {
				kc := col[chart.DrumKick]
				if n.DoubleKick {
					kc = th.DoubleKick
				}
				if sp {
					kc = th.StarPower
				}
				h.drawBar(dst, lanes, z, kc, a, 0.11)
			}
			for l := 1; l < chart.NumLanes; l++ {
				bit := uint8(1) << l
				if lanesMask&bit == 0 || hitLanes&bit != 0 || pos[l] < 0 {
					continue
				}
				c := col[l]
				if sp {
					c = th.StarPower
				}
				style := GemTom
				if n.Cymbals&bit != 0 {
					style = GemCymbal
				}
				scale := 0.92
				if n.Ghosts&bit != 0 {
					scale = 0.72
				}
				h.drawGem(dst, style, c, float64(pos[l])+0.5, lanes, z, s, y, a, scale, n.Accents&bit != 0, n.Activation && f.Eng.Stats().SPEnergy >= 0.5)
			}
			continue
		}
		if n.IsOpen() {
			c := col[chart.LaneOpen]
			if sp {
				c = th.StarPower
			}
			thick := 0.10
			if n.Kind == chart.HOPO {
				thick = 0.075
			}
			h.drawBar(dst, lanes, z, c, a, thick)
			continue
		}
		style := GemStrum
		scale := 1.0
		switch n.Kind {
		case chart.HOPO:
			style, scale = GemHOPO, 0.86
		case chart.Tap:
			style, scale = GemTap, 0.9
		}
		for l := 0; l < 5; l++ {
			if n.Lanes&(1<<l) == 0 {
				continue
			}
			c := col[l]
			if sp {
				c = th.StarPower
			}
			h.drawGem(dst, style, c, float64(pos[l])+0.5, lanes, z, s, y, a, scale, false, false)
		}
		if n.IsChord() && z >= 0 {
			// thin connector so chords read as one shape
			lo, hi := -1, -1
			for l := 0; l < 5; l++ {
				if n.Lanes&(1<<l) != 0 {
					if lo < 0 {
						lo = l
					}
					hi = l
				}
			}
			x0, x1 := h.X(float64(lo)+0.5, lanes, z), h.X(float64(hi)+0.5, lanes, z)
			b.Line(dst, x0, y+4*s, x1, y+4*s, 4*s, WithAlpha(th.Beat, 0.25*a), WithAlpha(th.Beat, 0.25*a))
			b.Flush(dst)
		}
	}

	b.Flush(dst)

	// --- hit effects ---
	h.drawHitFx(dst, f, func(l int) fxTarget {
		rx := lw * 0.40
		tg := fxTarget{y: ys, rx: rx, ry: rx * 0.5, c: col[l], visible: true}
		switch {
		case !drums && l == chart.LaneOpen:
			tg.cx, tg.span = h.Cfg.CenterX, h.Cfg.NearWidth*0.47
		case drums && l == chart.DrumKick:
			tg.cx, tg.span, tg.y = h.Cfg.CenterX, h.Cfg.NearWidth*0.47, ys+lw*0.26
		case pos[l] >= 0:
			tg.cx = h.X(float64(pos[l])+0.5, lanes, 0)
		default:
			tg.visible = false
		}
		return tg
	}, th)
}

func (h *Highway) drawGem(dst *ebiten.Image, style GemStyle, c color.RGBA, p float64, lanes int, z, s, y, alpha, scale float64, accent, activation bool) {
	lw := h.Cfg.NearWidth / float64(lanes)
	img := GemSprite(style, c)
	w := lw * 0.88 * s * scale * CurrentLook.NoteSize
	k := w / (2 * gemRX)
	x := h.X(p, lanes, z)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Translate(-spriteCX, -spriteCY)
	op.GeoM.Scale(k, k)
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleAlpha(float32(alpha))
	dst.DrawImage(img, op)
	if accent || activation {
		var b Batch
		rc := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
		if activation {
			rc = DefaultTheme.StarPower
		}
		b.Ring(dst, x, y, gemRX*k*1.12, 48*k*1.12, 4*s, WithAlpha(rc, alpha*0.9), WithAlpha(rc, alpha*0.2))
		b.Flush(dst)
	}
}

// drawBar draws a full-width bar (open notes, kicks) at depth z.
func (h *Highway) drawBar(dst *ebiten.Image, lanes int, z float64, c color.RGBA, alpha, thick float64) {
	s := h.Scale(z)
	y := h.Y(z)
	lw := h.Cfg.NearWidth / float64(lanes)
	x0 := h.X(0.15, lanes, z)
	x1 := h.X(float64(lanes)-0.15, lanes, z)
	t := lw * thick * s * CurrentLook.NoteSize
	var b Batch
	b.Quad(dst, x0, y-t/2+t*0.35, x1, y-t/2+t*0.35, x1, y+t/2+t*0.35, x0, y+t/2+t*0.35,
		WithAlpha(Darken(c, 0.5), alpha), WithAlpha(Darken(c, 0.5), alpha), WithAlpha(Darken(c, 0.7), alpha), WithAlpha(Darken(c, 0.7), alpha))
	b.Quad(dst, x0, y-t/2, x1, y-t/2, x1, y+t/2, x0, y+t/2,
		WithAlpha(Lighten(c, 0.35), alpha), WithAlpha(Lighten(c, 0.35), alpha), WithAlpha(c, alpha), WithAlpha(c, alpha))
	b.Line(dst, x0, y-t/2, x1, y-t/2, 2.5*s, WithAlpha(silver, alpha), WithAlpha(silver, alpha))
	b.Flush(dst)
}

// drawSustain draws a tail between lane positions p0..p1 (equal for a single
// lane) from depth z0 to z1, wobbling with the whammy while held.
func (h *Highway) drawSustain(dst *ebiten.Image, p0, p1 float64, lanes int, z0, z1 float64, c color.RGBA, alpha, width float64, held bool, f *FrameState) {
	lw := h.Cfg.NearWidth / float64(lanes)
	const segs = 24
	b := &h.batch
	var px0, px1, py float64
	for k := 0; k <= segs; k++ {
		z := z0 + (z1-z0)*float64(k)/segs
		s := h.Scale(z)
		y := h.Y(z)
		a := alpha * fadeFar(z) * fadePast(z)
		var xa, xb float64
		if p0 == p1 {
			w := lw * width * s
			wob := 0.0
			if held {
				wob = math.Sin(z*28-f.Now*30) * float64(f.Whammy) * lw * 0.07 * s
			}
			x := h.X(p0, lanes, z) + wob
			xa, xb = x-w/2, x+w/2
		} else {
			xa = h.X(p0+0.2, lanes, z)
			xb = h.X(p1-0.2, lanes, z)
			y += lw * 0.06 * s
		}
		if k > 0 {
			ca := WithAlpha(c, a)
			b.Quad(dst, px0, py, px1, py, xb, y, xa, y, ca, ca, ca, ca)
		}
		px0, px1, py = xa, xb, y
	}
}
