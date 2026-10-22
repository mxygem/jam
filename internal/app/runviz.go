package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// The run visuals on the results screen: the run's timing, the chart with
// every note marked, frets by section, the section report card, and the
// player's history on the part. The same views are in the HTML report
// (replay.WriteReport); these are drawn with the game's own look.

// vizArea is where a full-width visual page draws (inside its panel).
const (
	vizX = 96.0
	vizY = 276.0
	vizW = 1728.0
	vizH = 700.0
)

var (
	catColors = map[string]color.RGBA{
		replay.MissFrets: {0x90, 0x85, 0xE9, 0xFF},
		replay.MissPad:   {0xD5, 0x51, 0x81, 0xFF},
	}
	overColor = color.RGBA{0xEC, 0x83, 0x5A, 0xFF}
)

func catColor(c string) color.RGBA {
	switch c {
	case replay.MissEarly:
		return th.Early
	case replay.MissLate:
		return th.Late
	case replay.MissNone, "":
		return render.WithAlpha(th.TextDim, 0.9)
	}
	return catColors[c]
}

// rowColor is a chart row's lane color from the theme.
func rowColor(r replay.LaneRow) color.RGBA {
	switch r.Color {
	case "green":
		return th.Lanes[0]
	case "red":
		return th.Lanes[1]
	case "yellow":
		return th.Lanes[2]
	case "blue":
		return th.Lanes[3]
	case "orange":
		return th.Lanes[4]
	case "open":
		return th.Lanes[5]
	case "kick":
		return th.Drums[0]
	}
	return th.TextDim
}

// heatColor is the miss-rate ramp: one hue from the panel to full red.
// Nothing missed stays neutral so any miss stands out.
func heatColor(rate float64) color.RGBA {
	base := color.RGBA{0x22, 0x27, 0x3C, 0xFF}
	if rate <= 0 {
		return base
	}
	return render.Mix(base, th.Bad, 0.3+0.7*math.Min(1, rate))
}

func offColor(ms float64) color.RGBA {
	switch {
	case ms < -replay.OnTimeMs:
		return th.Early
	case ms > replay.OnTimeMs:
		return th.Late
	}
	return color.RGBA{0x9A, 0xA1, 0xB4, 0xFF}
}

func fmtMs(ms float64) string { return fmt.Sprintf("%+.1f ms", ms) }

// legend draws colored keys left to right and returns the x after them.
func legend(dst *ebiten.Image, x, y float64, items ...any) float64 {
	for i := 0; i+1 < len(items); i += 2 {
		c := items[i].(color.RGBA)
		s := items[i+1].(string)
		render.RoundRect(dst, x, y+6, 14, 14, 4, c)
		render.Text(dst, s, render.Cond, 22, x+22, y, th.TextDim, render.Left)
		w, _ := render.Measure(s, render.Cond, 22)
		x += w + 52
	}
	return x
}

// ---------------------------------------------------------------- timing

// drawTimingView: every hit at its song time and offset, the running
// average, misses and overstrums underneath, and the streak below that.
func drawTimingView(dst *ebiten.Image, v *replay.RunView, m *Mouse) {
	x0, y0, w := vizX+56, vizY+34, vizW-56
	plotH, rowH, streakH := 300.0, 24.0, 120.0
	yMiss := y0 + plotH + 12
	yOver := yMiss + rowH + 4
	yStreak := yOver + rowH + 22
	length := math.Max(v.Length, 1)
	x := func(t float64) float64 { return x0 + t/length*w }
	win := math.Max(v.EarlyMs, v.LateMs)
	var abs []float64
	for _, g := range v.Gems {
		if g.State == replay.GemHit {
			abs = append(abs, math.Abs(g.OffMs))
		}
	}
	lim := win
	if len(abs) > 0 {
		sortFloats(abs)
		lim = math.Max(win, abs[len(abs)*99/100])
	}
	lim = math.Ceil(math.Min(lim, win*1.5)/10) * 10
	y := func(o float64) float64 { return y0 + plotH/2 - math.Max(-1, math.Min(1, o/lim))*plotH/2 }

	var b render.Batch
	// Sections: alternate bands, names above.
	for i, s := range v.Sections {
		a, e := x(s.Start), x(math.Min(s.End, length))
		if i%2 == 1 {
			b.Rect(dst, a, y0, e-a, yStreak+streakH-y0, render.WithAlpha(th.Text, 0.035), render.WithAlpha(th.Text, 0.035))
		}
	}
	for _, sp := range v.SPSpans {
		b.Rect(dst, x(sp.Start), y0+plotH-4, math.Max(2, x(sp.End)-x(sp.Start)), 4, render.WithAlpha(th.StarPower, 0.6), render.WithAlpha(th.StarPower, 0.6))
	}
	grid := render.WithAlpha(th.TextDim, 0.18)
	for _, o := range []float64{-lim, -v.EarlyMs, v.LateMs, lim} {
		b.Rect(dst, x0, y(o), w, 1, grid, grid)
	}
	axis := render.WithAlpha(th.TextDim, 0.45)
	b.Rect(dst, x0, y(0), w, 1.5, axis, axis)

	type hot struct {
		x, y float64
		s    string
	}
	var hots []hot
	var hits []replay.GemMark
	for _, g := range v.Gems {
		switch g.State {
		case replay.GemHit:
			hits = append(hits, g)
			c := offColor(g.OffMs)
			cx, cy := x(g.Time), y(g.OffMs)
			if math.Abs(g.OffMs) > lim {
				b.Ring(dst, cx, cy, 4.5, 4.5, 1.6, c, c)
			} else {
				b.Dot(dst, cx, cy, 3.6, c, c)
			}
			hots = append(hots, hot{cx, cy, fmt.Sprintf("%s  %s  hit %s", fmtClock(g.Time), g.Kind, fmtMs(g.OffMs))})
		case replay.GemMissed:
			cx := x(g.Time)
			b.Rect(dst, cx-1.5, yMiss+3, 3, rowH-6, th.Bad, th.Bad)
			hots = append(hots, hot{cx, yMiss + rowH/2, fmt.Sprintf("%s  %s  missed: %s", fmtClock(g.Time), g.Kind, g.Reason)})
		}
	}
	for _, e := range v.Errors {
		c := overColor
		if !e.Broke {
			c = render.WithAlpha(overColor, 0.55)
		}
		cx := x(e.Time)
		b.Rect(dst, cx-1, yOver+5, 2, rowH-10, c, c)
		what := "overstrum"
		if v.Drums {
			what = "extra hit"
		}
		if e.Held != "" {
			what += ", held " + e.Held
		}
		hots = append(hots, hot{cx, yOver + rowH/2, fmtClock(e.Time) + "  " + what})
	}
	// Running average of the last 25 hits.
	const n = 25
	var px, py float64
	for i := range hits {
		lo := max(0, i-n+1)
		sum := 0.0
		for _, h := range hits[lo : i+1] {
			sum += math.Max(-lim, math.Min(lim, h.OffMs))
		}
		cx, cy := x(hits[i].Time), y(sum/float64(i-lo+1))
		if i >= 4 && i > 0 && px > 0 {
			b.Line(dst, px, py, cx, cy, 2.5, th.Accent, th.Accent)
		}
		if i >= 4 {
			px, py = cx, cy
		}
	}
	// Streak (its own chart, same time axis).
	maxS := 1
	for _, p := range v.Streak {
		maxS = max(maxS, p.Streak)
	}
	ys := func(s int) float64 { return yStreak + streakH - float64(s)/float64(maxS)*streakH }
	b.Rect(dst, x0, yStreak+streakH, w, 1.5, axis, axis)
	// A step line: flat between judgments, jumping at each one.
	fill := render.WithAlpha(th.Good, 0.18)
	base := yStreak + streakH
	prevT, prev := 0.0, 0
	stepTo := func(t1 float64, next int) {
		x0, x1 := x(prevT), x(t1)
		if prev > 0 && x1 > x0 {
			b.Quad(dst, x0, ys(prev), x1, ys(prev), x1, base, x0, base, fill, fill, fill, fill)
			b.Line(dst, x0, ys(prev), x1, ys(prev), 2, th.Good, th.Good)
		}
		if next != prev {
			b.Line(dst, x1, ys(prev), x1, ys(next), 2, th.Good, th.Good)
		}
		prevT, prev = t1, next
	}
	for _, p := range v.Streak {
		stepTo(p.Time, p.Streak)
	}
	end := length
	if !v.Completed {
		end = v.EndTime
	}
	stepTo(end, prev)
	for _, t := range v.SPOn {
		cx, cy := x(t), yStreak+8
		b.Quad(dst, cx, cy-8, cx+8, cy, cx, cy+8, cx-8, cy, th.StarPower, th.StarPower, th.StarPower, th.StarPower)
		hots = append(hots, hot{cx, cy, fmtClock(t) + "  star power"})
	}
	b.Flush(dst)

	for i, s := range v.Sections {
		a, e := x(s.Start), x(math.Min(s.End, length))
		if tw, _ := render.Measure(s.Name, render.Cond, 20); tw < e-a-6 {
			render.Text(dst, s.Name, render.Cond, 20, a+4, y0-28, th.TextDim, render.Left)
		} else if i%2 == 0 && e-a > 30 {
			render.Text(dst, truncateW(s.Name, render.Cond, 20, e-a-6), render.Cond, 20, a+4, y0-28, th.TextDim, render.Left)
		}
	}
	lab := func(yy float64, s string) {
		render.Text(dst, s, render.Cond, 20, x0-8, yy-11, th.TextDim, render.Right)
	}
	lab(y(lim), fmt.Sprintf("+%.0f", lim))
	lab(y(0), "0")
	lab(y(-lim), fmt.Sprintf("-%.0f", lim))
	render.Text(dst, "late", render.Cond, 20, x0+6, y0+2, th.Late, render.Left)
	render.Text(dst, "early", render.Cond, 20, x0+6, y0+plotH-26, th.Early, render.Left)
	lab(yMiss+rowH/2, "miss")
	if v.Drums {
		lab(yOver+rowH/2, "extra")
	} else {
		lab(yOver+rowH/2, "over")
	}
	lab(ys(maxS), fmt.Sprint(maxS))
	lab(yStreak+streakH-8, "streak")
	step := 30.0
	if length > 300 {
		step = 60
	}
	for t := 0.0; t <= length; t += step {
		render.Text(dst, strings.TrimSuffix(fmtClock(t), ".0"), render.Cond, 20, x(t), yStreak+streakH+6, th.TextDim, render.Center)
	}

	// Hover (mouse): the nearest mark's details on the info line.
	info := ""
	if m != nil && m.In(x0-10, y0-10, w+20, yStreak+streakH-y0+20) {
		best, bd := -1, 14.0*14
		for i, h := range hots {
			d := (h.x-m.X)*(h.x-m.X) + (h.y-m.Y)*(h.y-m.Y)
			if d < bd {
				best, bd = i, d
			}
		}
		if best >= 0 {
			info = hots[best].s
		}
	}
	ly := vizY + vizH - 30
	if info != "" {
		render.Text(dst, info, render.UI, 24, vizX, ly, th.Text, render.Left)
		return
	}
	legend(dst, vizX, ly, th.Early, "early", color.RGBA{0x9A, 0xA1, 0xB4, 0xFF}, "on time (±15 ms)", th.Late, "late",
		th.Accent, "average of the last 25 hits", th.Bad, "missed", overColor, map[bool]string{true: "extra hit", false: "overstrum"}[v.Drums], th.StarPower, "star power")
}

func sortFloats(xs []float64) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// ---------------------------------------------------------------- chart strip

// stripGem is one gem to draw on a chart strip.
type stripGem struct {
	t, end float64
	rows   []int
	fill   color.RGBA // zero: the row's lane color
	alpha  float64
	ring   bool // missed
	// ringColor, when set, draws the gem as a ring of that color (no
	// missed ring).
	ringColor color.RGBA
	info      string
}

// stripLayout splits a song into rows of rowSecs seconds.
type stripLayout struct {
	rowSecs float64
	rows    int
	laneH   float64
	blockH  float64
	lanes   int
}

func newStripLayout(length float64, lanes int) stripLayout {
	l := stripLayout{rowSecs: 20, lanes: lanes, laneH: 15}
	l.rows = max(1, int(math.Ceil(length/l.rowSecs)))
	l.rowSecs = length / float64(l.rows)
	if l.rowSecs < 1 {
		l.rowSecs = 1
	}
	l.blockH = float64(lanes)*l.laneH + 46
	return l
}

func (l stripLayout) visible() int { return max(1, int((vizH-40)/l.blockH)) }

// drawStrip draws rows first..first+visible of the chart strip. errors are
// overstrum / extra hit times (may be nil).
func drawStrip(dst *ebiten.Image, rows []replay.LaneRow, gems []stripGem, sections []replay.SectionCard, sp []replay.Span, errs []replay.ErrorMark, length float64, first int, m *Mouse) string {
	l := newStripLayout(math.Max(length, 1), len(rows))
	x0, w := vizX+70, vizW-70
	vis := l.visible()
	first = min(first, max(0, l.rows-vis))
	pos := func(t float64) (int, float64) {
		r := min(l.rows-1, int(t/l.rowSecs))
		return r, x0 + (t-float64(r)*l.rowSecs)/l.rowSecs*w
	}
	top := func(r int) float64 { return vizY + 26 + float64(r-first)*l.blockH }
	laneY := func(r, row int) float64 { return top(r) + float64(row)*l.laneH + l.laneH/2 }
	shown := func(r int) bool { return r >= first && r < first+vis }

	var b render.Batch
	for r := first; r < first+vis && r < l.rows; r++ {
		t0 := float64(r) * l.rowSecs
		for i, row := range rows {
			c := render.WithAlpha(rowColor(row), 0.22)
			b.Rect(dst, x0, laneY(r, i), w, 1, c, c)
		}
		for _, s := range sp {
			if s.End < t0 || s.Start > t0+l.rowSecs {
				continue
			}
			a := x0 + math.Max(0, (s.Start-t0)/l.rowSecs)*w
			e := x0 + math.Min(1, (s.End-t0)/l.rowSecs)*w
			c := render.WithAlpha(th.StarPower, 0.1)
			b.Rect(dst, a, top(r)-2, math.Max(2, e-a), float64(len(rows))*l.laneH+4, c, c)
		}
	}
	for _, s := range sections {
		if s.Total == 0 && s.Start > 0 {
			continue
		}
		r, sx := pos(s.Start)
		if !shown(r) {
			continue
		}
		c := render.WithAlpha(th.TextDim, 0.5)
		b.Rect(dst, sx, top(r)-16, 1, float64(len(rows))*l.laneH+16, c, c)
	}
	// Sustains under the gems.
	for _, g := range gems {
		if g.end <= g.t {
			continue
		}
		for _, row := range g.rows {
			t := g.t
			for t < g.end {
				r, sx := pos(t)
				t2 := math.Min(g.end, float64(r+1)*l.rowSecs)
				if shown(r) {
					ex := x0 + (t2-float64(r)*l.rowSecs)/l.rowSecs*w
					c := render.WithAlpha(rowColor(rows[row]), 0.45*g.alpha)
					b.Rect(dst, sx, laneY(r, row)-2, ex-sx, 4, c, c)
				}
				if t2 >= g.end || r == l.rows-1 {
					break
				}
				t = t2 + 1e-6
			}
		}
	}
	type hot struct {
		x, y float64
		s    string
	}
	var hots []hot
	for _, g := range gems {
		r, gx := pos(g.t)
		if !shown(r) {
			continue
		}
		if len(g.rows) > 1 {
			lo, hi := laneY(r, g.rows[0]), laneY(r, g.rows[len(g.rows)-1])
			c := render.WithAlpha(th.TextDim, 0.4)
			b.Rect(dst, gx-0.5, lo, 1, hi-lo, c, c)
		}
		for _, row := range g.rows {
			c := g.fill
			if c.A == 0 {
				c = rowColor(rows[row])
			}
			c = render.WithAlpha(c, g.alpha)
			gy := laneY(r, row)
			switch {
			case g.ringColor.A != 0:
				b.Dot(dst, gx, gy, 5, c, c)
				b.Ring(dst, gx, gy, 6.5, 6.5, 2.4, g.ringColor, g.ringColor)
			case g.ring:
				b.Dot(dst, gx, gy, 5, c, c)
				b.Ring(dst, gx, gy, 6, 6, 2.2, th.Bad, th.Bad)
			default:
				b.Dot(dst, gx, gy, 5, c, c)
			}
			hots = append(hots, hot{gx, gy, g.info})
		}
	}
	for _, e := range errs {
		r, ex := pos(e.Time)
		if !shown(r) {
			continue
		}
		ey := top(r) + float64(len(rows))*l.laneH + 4
		c := overColor
		if !e.Broke {
			c = render.WithAlpha(overColor, 0.55)
		}
		b.Quad(dst, ex, ey, ex+5, ey+8, ex-5, ey+8, ex, ey, c, c, c, c)
	}
	b.Flush(dst)
	for r := first; r < first+vis && r < l.rows; r++ {
		render.Text(dst, strings.TrimSuffix(fmtClock(float64(r)*l.rowSecs), ".0"), render.Cond, 22, vizX, top(r)+float64(len(rows))*l.laneH/2-13, th.TextDim, render.Left)
	}
	for _, s := range sections {
		if s.Total == 0 && s.Start > 0 {
			continue
		}
		r, sx := pos(s.Start)
		if shown(r) {
			render.Text(dst, s.Name, render.Cond, 20, sx+4, top(r)-26, th.Text, render.Left)
		}
	}
	if l.rows > vis {
		render.Text(dst, fmt.Sprintf("%s to %s of %s   up/down to scroll",
			strings.TrimSuffix(fmtClock(float64(first)*l.rowSecs), ".0"),
			strings.TrimSuffix(fmtClock(math.Min(length, float64(first+vis)*l.rowSecs)), ".0"),
			strings.TrimSuffix(fmtClock(length), ".0")), render.Cond, 22, vizX+vizW, vizY+vizH-30, th.TextDim, render.Right)
	}
	if m != nil {
		best, bd := -1, 10.0*10
		for i, h := range hots {
			d := (h.x-m.X)*(h.x-m.X) + (h.y-m.Y)*(h.y-m.Y)
			if d < bd {
				best, bd = i, d
			}
		}
		if best >= 0 {
			return hots[best].s
		}
	}
	return ""
}

// stripRows is how many rows the strip of a song has and how many show.
func stripRows(length float64, lanes int) (rows, visible int) {
	l := newStripLayout(math.Max(length, 1), lanes)
	return l.rows, l.visible()
}

func runStripGems(v *replay.RunView) []stripGem {
	out := make([]stripGem, 0, len(v.Gems))
	for _, g := range v.Gems {
		sg := stripGem{t: g.Time, end: g.End, rows: g.Rows}
		head := fmtClock(g.Time) + "  " + g.Kind
		switch g.State {
		case replay.GemHit:
			sg.alpha, sg.info = 1, head+"  hit "+fmtMs(g.OffMs)
		case replay.GemMissed:
			sg.alpha, sg.ring, sg.info = 0.35, true, head+"  missed: "+g.Reason
		default:
			sg.alpha, sg.info = 0.18, head+"  not reached"
		}
		out = append(out, sg)
	}
	return out
}

func drawChartView(dst *ebiten.Image, v *replay.RunView, first int, m *Mouse) {
	info := drawStrip(dst, v.Rows, runStripGems(v), v.Sections, v.SPSpans, v.Errors, v.Length, first, m)
	ly := vizY + vizH - 30
	if info != "" {
		render.Text(dst, info, render.UI, 24, vizX, ly, th.Text, render.Left)
		return
	}
	x := legend(dst, vizX, ly, th.Lanes[0], "hit", render.WithAlpha(th.Lanes[0], 0.25), "not reached",
		overColor, map[bool]string{true: "extra hit", false: "overstrum"}[v.Drums], render.WithAlpha(th.StarPower, 0.4), "star power phrase")
	var b render.Batch
	b.Ring(dst, x+7, ly+13, 7, 7, 2.2, th.Bad, th.Bad)
	b.Flush(dst)
	render.Text(dst, "missed", render.Cond, 22, x+22, ly, th.TextDim, render.Left)
}

// ---------------------------------------------------------------- frets

// drawFretView: miss rate per fret (or pad) per section.
func drawFretView(dst *ebiten.Image, v *replay.RunView, firstCol int) int {
	var cols []int
	for i, s := range v.Sections {
		if s.Total > 0 || v.Over[i] > 0 {
			cols = append(cols, i)
		}
	}
	labelW := 170.0
	cw := math.Max(96, math.Min(200, (vizW-labelW)/float64(max(1, len(cols)))))
	fit := int((vizW - labelW) / cw)
	firstCol = min(firstCol, max(0, len(cols)-fit))
	cols = cols[firstCol:min(len(cols), firstCol+fit)]
	rowH := math.Min(64, (vizH-140)/float64(len(v.Rows)+1))
	y0 := vizY + 34
	for ci, si := range cols {
		x := vizX + labelW + float64(ci)*cw
		render.Text(dst, truncateW(v.Sections[si].Name, render.Cond, 22, cw-8), render.Cond, 22, x+4, y0-30, th.TextDim, render.Left)
	}
	var b render.Batch
	for ri, row := range v.Rows {
		y := y0 + float64(ri)*rowH
		b.Dot(dst, vizX+9, y+rowH/2, 8, rowColor(row), rowColor(row))
		for ci, si := range cols {
			c := v.Grid[ri][si]
			x := vizX + labelW + float64(ci)*cw
			if c.Total == 0 {
				continue
			}
			if rate := c.MissRate(); rate < 0 {
				dim := render.WithAlpha(th.TextDim, 0.25)
				b.Rect(dst, x+2, y+2, cw-4, 1.5, dim, dim)
				b.Rect(dst, x+2, y+rowH-3.5, cw-4, 1.5, dim, dim)
			} else {
				hc := heatColor(rate)
				b.Rect(dst, x+2, y+2, cw-4, rowH-4, hc, hc)
			}
		}
	}
	b.Flush(dst)
	for ri, row := range v.Rows {
		y := y0 + float64(ri)*rowH
		render.Text(dst, strings.Title(row.Name), render.UI, 24, vizX+28, y+rowH/2-15, th.Text, render.Left)
		for ci, si := range cols {
			c := v.Grid[ri][si]
			x := vizX + labelW + float64(ci)*cw
			switch {
			case c.Total == 0:
			case c.MissRate() < 0:
				render.Text(dst, "not reached", render.Cond, 20, x+cw/2, y+rowH/2-12, th.TextDim, render.Center)
			case c.Misses > 0:
				ink := th.Text
				render.Text(dst, fmt.Sprintf("%d/%d", c.Misses, c.Hits+c.Misses), render.CondBold, 24, x+cw/2, y+rowH/2-15, ink, render.Center)
			}
		}
	}
	oy := y0 + float64(len(v.Rows))*rowH + 8
	over := "Overstrums"
	if v.Drums {
		over = "Extra hits"
	}
	render.Text(dst, over, render.UI, 24, vizX+28, oy, overColor, render.Left)
	for ci, si := range cols {
		if n := v.Over[si]; n > 0 {
			render.Text(dst, fmt.Sprint(n), render.CondBold, 26, vizX+labelW+float64(ci)*cw+cw/2, oy, overColor, render.Center)
		}
	}
	ly := vizY + vizH - 30
	render.Text(dst, "0% missed", render.Cond, 22, vizX, ly, th.TextDim, render.Left)
	var rb render.Batch
	for i := 0; i < 40; i++ {
		c := heatColor(float64(i) / 39)
		rb.Rect(dst, vizX+110+float64(i)*5, ly+6, 5, 16, c, c)
	}
	rb.Flush(dst)
	render.Text(dst, "100%   numbers are misses over notes reached; plain cells had no misses", render.Cond, 22, vizX+320, ly, th.TextDim, render.Left)
	if total := len(colsAll(v)); total > fit {
		render.Text(dst, fmt.Sprintf("sections %d-%d of %d   up/down to scroll", firstCol+1, firstCol+len(cols), total), render.Cond, 22, vizX+vizW, vizY+vizH-30, th.TextDim, render.Right)
	}
	return firstCol
}

func colsAll(v *replay.RunView) []int {
	var cols []int
	for i, s := range v.Sections {
		if s.Total > 0 || v.Over[i] > 0 {
			cols = append(cols, i)
		}
	}
	return cols
}

// ---------------------------------------------------------------- report card

func reportRows(v *replay.RunView) []replay.SectionCard {
	var out []replay.SectionCard
	for _, s := range v.Sections {
		if s.Total > 0 || s.Overstrums > 0 {
			out = append(out, s)
		}
	}
	return out
}

const cardRowH = 50.0

func cardVisible() int { return int(math.Floor((vizH - 110) / cardRowH)) }

func drawReportCard(dst *ebiten.Image, v *replay.RunView, first int) {
	rows := reportRows(v)
	vis := cardVisible()
	first = min(first, max(0, len(rows)-vis))
	cx := []float64{0, 290, 400, 700, 830, 1180, 1330, 1500, 1728}
	over := "Over"
	if v.Drums {
		over = "Extra"
	}
	heads := []string{"Section", "Starts", "Accuracy", "Notes", "Misses by cause", over, "Timing", "Early / late", "Best streak"}
	right := map[int]bool{1: true, 3: true, 5: true, 6: true, 7: true, 8: true}
	for i, hd := range heads {
		x, al := vizX+cx[i], render.Left
		if right[i] {
			al = render.Right
			if i == 1 || i == 3 {
				x = vizX + cx[i] + 60
			}
		}
		render.Text(dst, strings.ToUpper(hd), render.CondBold, 20, x, vizY, th.TextDim, al)
	}
	maxMiss := 1
	for _, s := range rows {
		maxMiss = max(maxMiss, s.Misses)
	}
	unit := math.Min(10, 220/float64(maxMiss))
	var b render.Batch
	type txt struct {
		s     string
		f     render.Font
		x, y  float64
		c     color.RGBA
		align render.Align
	}
	var texts []txt
	for i := first; i < len(rows) && i < first+vis; i++ {
		s := rows[i]
		y := vizY + 40 + float64(i-first)*cardRowH
		line := render.WithAlpha(th.TextDim, 0.15)
		b.Rect(dst, vizX, y+cardRowH-6, vizW, 1, line, line)
		texts = append(texts, txt{truncateW(s.Name, render.UI, 26, 270), render.UI, vizX, y, th.Text, render.Left},
			txt{strings.TrimSuffix(fmtClock(s.Start), ".0"), render.Cond, vizX + cx[1] + 60, y + 2, th.TextDim, render.Right})
		judged := s.Judged()
		if judged == 0 {
			texts = append(texts, txt{"not reached", render.Cond, vizX + cx[2], y + 2, th.TextDim, render.Left})
			continue
		}
		acc := s.Accuracy()
		col := th.Good
		switch {
		case acc < 0.75:
			col = th.Bad
		case acc < 0.95:
			col = th.Lanes[2]
		}
		bg := render.WithAlpha(th.TextDim, 0.2)
		b.Rect(dst, vizX+cx[2], y+12, 180, 10, bg, bg)
		b.Rect(dst, vizX+cx[2], y+12, 180*acc, 10, col, col)
		texts = append(texts, txt{fmt.Sprintf("%.1f%%", acc*100), render.Cond, vizX + cx[2] + 196, y + 2, th.Text, render.Left})
		notes := fmt.Sprintf("%d/%d", s.Hits, judged)
		if judged < s.Total {
			notes += fmt.Sprintf(" of %d", s.Total)
		}
		texts = append(texts, txt{notes, render.Cond, vizX + cx[3] + 60, y + 2, th.Text, render.Right})
		if s.Misses > 0 {
			texts = append(texts, txt{fmt.Sprint(s.Misses), render.Cond, vizX + cx[4], y + 2, th.TextDim, render.Left})
			bx := vizX + cx[4] + 40
			for _, c := range replay.MissCategories {
				if n := s.MissBy[c]; n > 0 {
					wd := math.Max(4, float64(n)*unit)
					cc := catColor(c)
					b.Rect(dst, bx, y+11, wd, 12, cc, cc)
					bx += wd + 2
				}
			}
		} else {
			texts = append(texts, txt{"none", render.Cond, vizX + cx[4], y + 2, th.TextDim, render.Left})
		}
		if s.Overstrums > 0 {
			texts = append(texts, txt{fmt.Sprint(s.Overstrums), render.Cond, vizX + cx[5], y + 2, overColor, render.Right})
		}
		if s.Hits > 0 {
			texts = append(texts, txt{fmt.Sprintf("%+.1f ±%.0f", s.MeanMs, s.SpreadMs), render.Cond, vizX + cx[6], y + 2, th.Text, render.Right})
		}
		texts = append(texts, txt{fmt.Sprintf("%d / %d", s.Early, s.Late), render.Cond, vizX + cx[7], y + 2, th.Text, render.Right},
			txt{fmt.Sprint(s.LongestStreak), render.Cond, vizX + cx[8], y + 2, th.Text, render.Right})
	}
	b.Flush(dst)
	for _, t := range texts {
		render.Text(dst, t.s, t.f, 26, t.x, t.y, t.c, t.align)
	}
	ly := vizY + vizH - 30
	var items []any
	for _, c := range replay.MissCategories {
		for _, s := range rows {
			if s.MissBy[c] > 0 {
				items = append(items, catColor(c), replay.MissCategoryTitle(c))
				break
			}
		}
	}
	x := legend(dst, vizX, ly, items...)
	render.Text(dst, "Timing: average ms ± spread. Early / late: hits more than 15 ms off.", render.Cond, 22, x, ly, th.TextDim, render.Left)
	if len(rows) > vis {
		render.Text(dst, fmt.Sprintf("%d-%d of %d sections   up/down to scroll", first+1, min(len(rows), first+vis), len(rows)), render.Cond, 22, vizX+vizW, vizY+vizH-30, th.TextDim, render.Right)
	}
}
