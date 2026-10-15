package render

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
)

// Lyrics shows a chart's lyrics karaoke style across the top of the screen:
// the current phrase lights up syllable by syllable as it is sung, with the
// next phrase waiting underneath.
type Lyrics struct {
	Phrases []chart.LyricPhrase
	Y       float64 // top of the lyrics band

	lines map[lineKey]*lyricLine
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
	start, end []float64 // per syllable, in pixels from the left edge
	used       int
}

const (
	lyricSize     = 46
	lyricNextSize = 32
	lyricMaxW     = 1500
	lyricPad      = 8 // room around glyphs in the line image
)

// NewLyrics prepares lyrics for drawing. It returns nil when there are none.
func NewLyrics(phrases []chart.LyricPhrase, y float64) *Lyrics {
	if len(phrases) == 0 {
		return nil
	}
	return &Lyrics{Phrases: phrases, Y: y, lines: map[lineKey]*lyricLine{}}
}

// lineText joins the syllables of a phrase and records where each one sits
// in the joined text.
func lineText(p *chart.LyricPhrase) (string, []int, []int) {
	var b strings.Builder
	starts := make([]int, len(p.Syllables))
	ends := make([]int, len(p.Syllables))
	for i, s := range p.Syllables {
		starts[i] = b.Len()
		b.WriteString(s.Text)
		ends[i] = b.Len()
		if !s.Join && i < len(p.Syllables)-1 {
			b.WriteByte(' ')
		}
	}
	return b.String(), starts, ends
}

func (l *Lyrics) line(i int, size float64, tick int) *lyricLine {
	k := lineKey{i, size}
	if ln := l.lines[k]; ln != nil {
		ln.used = tick
		return ln
	}
	p := &l.Phrases[i]
	s, starts, ends := lineText(p)
	w, h := MeasureRich(s, UIBold, size)
	if w > lyricMaxW { // long phrases shrink to fit
		size = math.Max(24, size*lyricMaxW/w)
		w, h = MeasureRich(s, UIBold, size)
	}
	if h <= 0 {
		h = size * 1.3
	}
	ln := &lyricLine{w: w, h: h, used: tick}
	for j := range p.Syllables {
		a, _ := MeasureRich(s[:starts[j]], UIBold, size)
		b, _ := MeasureRich(s[:ends[j]], UIBold, size)
		ln.start = append(ln.start, a)
		ln.end = append(ln.end, b)
	}
	iw, ih := int(math.Ceil(w))+2*lyricPad, int(math.Ceil(h))+2*lyricPad
	ln.img = ebiten.NewImage(max(1, iw), max(1, ih))
	RichText(ln.img, s, UIBold, size, lyricPad, lyricPad, color.White, Left, 0)
	l.lines[k] = ln
	// keep only a handful of rendered lines
	if len(l.lines) > 6 {
		for kk, old := range l.lines {
			if old.used < tick-2 {
				old.img.Deallocate()
				delete(l.lines, kk)
			}
		}
	}
	return ln
}

// current returns the index of the first phrase that hasn't finished
// fading out at time now.
func (l *Lyrics) current(now float64) int {
	return sort.Search(len(l.Phrases), func(i int) bool { return l.Phrases[i].End+0.3 >= now })
}

// leadIn is how early a phrase appears: up to 2 s, but never before the
// previous phrase has finished.
func (l *Lyrics) leadIn(i int) float64 {
	lead := 2.0
	if i > 0 {
		lead = math.Min(lead, l.Phrases[i].Start-l.Phrases[i-1].End)
	}
	return math.Max(0.25, lead)
}

// Draw draws the lyrics for song time now, centred on cx. tick is a frame
// counter used to drop rendered lines that are no longer needed.
func (l *Lyrics) Draw(dst *ebiten.Image, now, cx float64, tick int) {
	if l == nil {
		return
	}
	i := l.current(now)
	if i >= len(l.Phrases) {
		return
	}
	p := &l.Phrases[i]
	show := p.Start - l.leadIn(i)
	if now < show {
		// between phrases: only a preview of what's coming, if it's close
		if now < show-1.0 {
			return
		}
		ln := l.line(i, lyricNextSize, tick)
		a := 0.45 * clamp01((now-(show-1.0))/0.3)
		l.drawBand(dst, cx, ln.w, ln.h, 0, a)
		drawLine(dst, ln, cx-ln.w/2, l.Y+lyricNextSize*0.4, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, a, -1)
		return
	}
	fade := clamp01((now-show)/0.25) * clamp01((p.End+0.3-now)/0.3)
	cur := l.line(i, lyricSize, tick)
	var next *lyricLine
	if i+1 < len(l.Phrases) && l.Phrases[i+1].Start-l.leadIn(i+1) < p.End+4 {
		next = l.line(i+1, lyricNextSize, tick)
	}
	bw, nh := cur.w, 0.0
	if next != nil {
		bw, nh = math.Max(bw, next.w), next.h+6
	}
	l.drawBand(dst, cx, bw, cur.h, nh, 0.55*fade)

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
	x := cx - cur.w/2
	drawLine(dst, cur, x, l.Y, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, 0.5*fade, -1)
	if hx > 0 {
		drawLine(dst, cur, x, l.Y, DefaultTheme.Accent, fade, hx)
	}
	if next != nil {
		drawLine(dst, next, cx-next.w/2, l.Y+cur.h+6, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, 0.4*fade, -1)
	}
}

func (l *Lyrics) drawBand(dst *ebiten.Image, cx, w, h, extra, a float64) {
	if a <= 0 {
		return
	}
	bw := w + 80
	RoundRect(dst, cx-bw/2, l.Y-10, bw, h+extra+22, 16, color.RGBA{0, 0, 0, uint8(255 * a)})
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
