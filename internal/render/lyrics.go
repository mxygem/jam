package render

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
)

// Lyrics shows a chart's vocal parts karaoke style across the top of the
// screen. The lead part's current phrase lights up syllable by syllable as it
// is sung, with its next phrase waiting underneath; harmony and backing parts
// get their own rows, each sweeping on its own timing in its own color.
type Lyrics struct {
	Y    float64 // top of the lyrics area
	Opts LyricOpts
	// Beat gives how far song time now is into its beat (0..1), for the pulse
	// effect. Without it, pulse assumes 120 BPM.
	Beat   func(now float64) float64
	tracks []*lyricTrack
}

// LyricOpts are the player's choices for showing vocal parts.
type LyricOpts struct {
	// Backing places harmony/backing parts: "below" the lead (default),
	// "above" it, or "off".
	Backing string
	// BackingScale is the backing parts' text size relative to the lead.
	BackingScale float64
	// Effects is how much of the charts' lyric effects to show: "full"
	// (default), "calm" (no shaking, flying or exploding letters) or "off".
	Effects string
}

// BackingPlacements lists the values LyricOpts.Backing takes, in menu order.
var BackingPlacements = []string{"below", "above", "off"}

// lyricTrack is one vocal part being drawn.
type lyricTrack struct {
	Phrases  []chart.LyricPhrase
	Name     string
	lead     bool
	color    color.RGBA // the sung highlight
	size     float64    // current phrase text size
	nextSize float64    // the waiting next phrase (lead only)
	lines    map[lineKey]*lyricLine
	fx       []phraseFX // per phrase, filtered by LyricOpts.Effects
}

type lineKey struct {
	idx  int
	size float64
}

// lyricLine is one phrase rendered once to an offscreen image, plus where
// each syllable starts and ends so the highlight can sweep across it.
type lyricLine struct {
	img        *ebiten.Image
	w, h       float64
	size       float64     // the text size it was rendered at
	start, end []float64   // per syllable, in pixels from the left edge
	letters    []letterBox // per visible letter, for phrases with effects
	used       int
}

const (
	lyricSize     = 46
	lyricNextSize = 32
	lyricMaxW     = 1500
	lyricPad      = 8 // room around glyphs in the line image
	lyricRowGap   = 10
)

// PartColors are the highlight colors of harmony/backing parts, in part
// order (the lead uses the theme accent).
var PartColors = []color.RGBA{
	{0x5B, 0xC8, 0xFF, 0xFF}, // sky
	{0xFF, 0xB3, 0x47, 0xFF}, // amber
	{0xC5, 0x8B, 0xFF, 0xFF}, // violet
	{0x6E, 0xE7, 0x9C, 0xFF}, // mint
}

// DefaultLyricOpts are the settings used when none are saved.
func DefaultLyricOpts() LyricOpts {
	return LyricOpts{Backing: "below", BackingScale: 0.75, Effects: "full"}
}

// NewLyrics prepares vocal parts for drawing. It returns nil when there are
// no lyrics. The first part is drawn as the lead.
func NewLyrics(parts []chart.VocalPart, y float64, opts LyricOpts) *Lyrics {
	if opts.BackingScale <= 0 {
		opts.BackingScale = DefaultLyricOpts().BackingScale
	}
	if opts.Backing == "" {
		opts.Backing = "below"
	}
	l := &Lyrics{Y: y, Opts: opts}
	for i, p := range parts {
		if len(p.Phrases) == 0 {
			continue
		}
		tr := &lyricTrack{Phrases: p.Phrases, Name: p.Name, lines: map[lineKey]*lyricLine{}}
		for k := range p.Phrases {
			tr.fx = append(tr.fx, buildPhraseFX(&p.Phrases[k], opts.Effects))
		}
		if i == 0 {
			tr.lead, tr.color, tr.size, tr.nextSize = true, DefaultTheme.Accent, lyricSize, lyricNextSize
		} else {
			if opts.Backing == "off" {
				continue
			}
			tr.color = PartColors[(len(l.tracks)-1+len(PartColors))%len(PartColors)]
			tr.size = math.Round(lyricSize * opts.BackingScale)
		}
		l.tracks = append(l.tracks, tr)
	}
	if len(l.tracks) == 0 {
		return nil
	}
	return l
}

// Parts reports how many vocal parts are being shown.
func (l *Lyrics) Parts() int {
	if l == nil {
		return 0
	}
	return len(l.tracks)
}

// lineText joins the syllables of a phrase and records where each one sits
// in the joined text.
func lineText(p *chart.LyricPhrase) (string, []int, []int) { return lineTextFX(p, nil) }

func (t *lyricTrack) line(i int, size float64, tick int) *lyricLine {
	k := lineKey{i, size}
	if ln := t.lines[k]; ln != nil {
		ln.used = tick
		return ln
	}
	p := &t.Phrases[i]
	var pf *phraseFX
	if i < len(t.fx) && t.fx[i].any {
		pf = &t.fx[i]
	}
	s, starts, ends := lineTextFX(p, pf)
	w, h := MeasureRich(s, UIBold, size)
	if w > lyricMaxW { // long phrases shrink to fit
		size = math.Max(20, size*lyricMaxW/w)
		w, h = MeasureRich(s, UIBold, size)
	}
	if h <= 0 {
		h = size * 1.3
	}
	ln := &lyricLine{w: w, h: h, size: size, used: tick}
	for j := range p.Syllables {
		a, _ := MeasureRich(s[:starts[j]], UIBold, size)
		b, _ := MeasureRich(s[:ends[j]], UIBold, size)
		ln.start = append(ln.start, a)
		ln.end = append(ln.end, b)
	}
	if pf != nil {
		ln.letters = measureLetters(s, starts, ends, size)
	}
	iw, ih := int(math.Ceil(w))+2*lyricPad, int(math.Ceil(h))+2*lyricPad
	ln.img = ebiten.NewImage(max(1, iw), max(1, ih))
	RichText(ln.img, s, UIBold, size, lyricPad, lyricPad, color.White, Left, 0)
	t.lines[k] = ln
	// keep only a handful of rendered lines
	if len(t.lines) > 6 {
		for kk, old := range t.lines {
			if old.used < tick-2 {
				old.img.Deallocate()
				delete(t.lines, kk)
			}
		}
	}
	return ln
}

// current returns the index of the first phrase that hasn't finished
// fading out at time now.
func (t *lyricTrack) current(now float64) int {
	return sort.Search(len(t.Phrases), func(i int) bool { return t.Phrases[i].End+t.tail(i) >= now })
}

// tail is how long phrase i stays up after it ends (longer when it explodes).
func (t *lyricTrack) tail(i int) float64 {
	if i < len(t.fx) {
		return t.fx[i].tail
	}
	return 0.3
}

// leadIn is how early a phrase appears: up to 2 s, but never before the
// previous phrase has finished.
func (t *lyricTrack) leadIn(i int) float64 {
	lead := 2.0
	if i > 0 {
		lead = math.Min(lead, t.Phrases[i].Start-t.Phrases[i-1].End)
	}
	return math.Max(0.25, lead)
}

// rowHeight is the fixed height a track's row takes, so parts don't jump
// around as phrases come and go.
func (t *lyricTrack) rowHeight() float64 {
	h := t.size*1.3 + 22
	if t.lead {
		h += t.nextSize*1.3 + 6
	}
	return h
}

// Draw draws the lyrics for song time now, centred on cx. tick is a frame
// counter used to drop rendered lines that are no longer needed.
func (l *Lyrics) Draw(dst *ebiten.Image, now, cx float64, tick int) {
	if l == nil || len(l.tracks) == 0 {
		return
	}
	lead, backing := l.tracks[0], l.tracks[1:]
	y := l.Y
	if l.Opts.Backing == "above" {
		for _, t := range backing {
			t.draw(dst, now, cx, y, tick, l.Beat)
			y += t.rowHeight() + lyricRowGap
		}
		lead.draw(dst, now, cx, y, tick, l.Beat)
		return
	}
	lead.draw(dst, now, cx, y, tick, l.Beat)
	y += lead.rowHeight() + lyricRowGap
	for _, t := range backing {
		t.draw(dst, now, cx, y, tick, l.Beat)
		y += t.rowHeight() + lyricRowGap
	}
}

// draw draws one part's current phrase (and for the lead, the next one) with
// its band at y.
func (t *lyricTrack) draw(dst *ebiten.Image, now, cx, y float64, tick int, beat func(float64) float64) {
	i := t.current(now)
	if i >= len(t.Phrases) {
		return
	}
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	unsung := white
	if !t.lead {
		// a hint of the part's color, so parts are easy to tell apart before they're sung
		unsung = color.RGBA{uint8((int(t.color.R) + 3*255) / 4), uint8((int(t.color.G) + 3*255) / 4), uint8((int(t.color.B) + 3*255) / 4), 0xFF}
	}
	p := &t.Phrases[i]
	show := p.Start - t.leadIn(i)
	if now < show {
		// between phrases: only a preview of what's coming, if it's close
		if now < show-1.0 {
			return
		}
		size := t.size
		if t.lead {
			size = t.nextSize
		}
		if i < len(t.fx) && (t.fx[i].hasAny("flyin") || t.fx[i].hasAny("type")) {
			return // these phrases make their own entrance
		}
		ln := t.line(i, size, tick)
		a := 0.45 * clamp01((now-(show-1.0))/0.3)
		drawBand(dst, cx, y, ln.w, ln.h, 0, a)
		drawLine(dst, ln, cx-ln.w/2, y+size*0.4*b2f(t.lead), unsung, a, -1)
		return
	}
	fade := clamp01((now-show)/0.25) * clamp01((p.End+t.tail(i)-now)/0.3)
	cur := t.line(i, t.size, tick)
	var next *lyricLine
	if t.lead && i+1 < len(t.Phrases) && t.Phrases[i+1].Start-t.leadIn(i+1) < p.End+4 {
		next = t.line(i+1, t.nextSize, tick)
	}
	// how far the highlight has swept
	hx := 0.0
	for j, s := range p.Syllables {
		if s.Time > now {
			break
		}
		d := 0.12
		if j+1 < len(p.Syllables) {
			d = math.Min(d, math.Max(0.02, p.Syllables[j+1].Time-s.Time))
		}
		hx = cur.start[j] + (cur.end[j]-cur.start[j])*clamp01((now-s.Time)/d)
	}
	if i < len(t.fx) && t.fx[i].any {
		t.drawFX(dst, i, cur, now, cx, y, fade, hx, unsung, beat, next)
		return
	}
	bw, nh := cur.w, 0.0
	if next != nil {
		bw, nh = math.Max(bw, next.w), next.h+6
	}
	drawBand(dst, cx, y, bw, cur.h, nh, 0.55*fade)
	x := cx - cur.w/2
	drawLine(dst, cur, x, y, unsung, 0.5*fade, -1)
	if hx > 0 {
		drawLine(dst, cur, x, y, t.color, fade, hx)
	}
	if next != nil {
		drawLine(dst, next, cx-next.w/2, y+cur.h+6, white, 0.4*fade, -1)
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func drawBand(dst *ebiten.Image, cx, y, w, h, extra, a float64) {
	if a <= 0 {
		return
	}
	bw := w + 80
	RoundRect(dst, cx-bw/2, y-10, bw, h+extra+22, 16, color.RGBA{0, 0, 0, uint8(255 * a)})
}

// drawLine draws a rendered line tinted c at alpha a. clip >= 0 draws only
// the first clip pixels (the sung part).
func drawLine(dst *ebiten.Image, ln *lyricLine, x, y float64, c color.RGBA, a, clip float64) {
	if a <= 0 {
		return
	}
	src := ln.img
	if clip >= 0 {
		cw := int(math.Ceil(clip)) + lyricPad
		if cw <= 0 {
			return
		}
		b := src.Bounds()
		src = src.SubImage(image.Rect(b.Min.X, b.Min.Y, min(b.Max.X, b.Min.X+cw), b.Max.Y)).(*ebiten.Image)
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(x-lyricPad, y-lyricPad)
	op.ColorScale.Scale(float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, 1)
	op.ColorScale.ScaleAlpha(float32(a))
	dst.DrawImage(src, op)
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// Height is how much space the lyrics take below Y (0 with no lyrics).
func (l *Lyrics) Height() float64 {
	if l == nil || len(l.tracks) == 0 {
		return 0
	}
	h := 0.0
	for i, t := range l.tracks {
		if i > 0 && l.Opts.Backing == "off" {
			break
		}
		h += t.rowHeight() + lyricRowGap
	}
	return h
}
