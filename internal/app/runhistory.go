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

// History pages on the results screen: one player's attempts at the part.

func attemptDate(a replay.Attempt) string { return a.Started.Local().Format("Jan 2 3:04 PM") }

// drawProgressView: score, hit % and best streak per attempt as three
// small charts, then the latest attempts as a table.
func drawProgressView(dst *ebiten.Image, h *replay.History) {
	A := h.Attempts
	metrics := []struct {
		name string
		f    func(replay.Attempt) float64
		fmt  func(float64) string
	}{
		{"Score", func(a replay.Attempt) float64 { return float64(a.Score) }, func(v float64) string { return fmt.Sprintf("%.0f", v) }},
		{"Hit %", func(a replay.Attempt) float64 { return a.HitRate * 100 }, func(v float64) string { return fmt.Sprintf("%.1f%%", v) }},
		{"Best streak", func(a replay.Attempt) float64 { return float64(a.MaxStreak) }, func(v float64) string { return fmt.Sprintf("%.0f", v) }},
	}
	gap := 48.0
	cw := (vizW - 2*gap) / 3
	ch := math.Max(250, vizH-450)
	var b render.Batch
	type label struct {
		s    string
		x, y float64
		al   render.Align
		dim  bool
	}
	var labels []label
	for mi, mt := range metrics {
		x0 := vizX + float64(mi)*(cw+gap)
		y0 := vizY + 40
		vals := make([]float64, len(A))
		lo, hi := math.Inf(1), math.Inf(-1)
		for i, a := range A {
			vals[i] = mt.f(a)
			lo, hi = math.Min(lo, vals[i]), math.Max(hi, vals[i])
		}
		if hi == lo {
			hi, lo = hi+1, lo-1
		}
		pad := (hi - lo) * 0.12
		lo, hi = lo-pad, hi+pad
		x := func(i int) float64 {
			if len(A) == 1 {
				return x0 + cw/2
			}
			return x0 + 8 + float64(i)/float64(len(A)-1)*(cw-16)
		}
		y := func(v float64) float64 { return y0 + (1-(v-lo)/(hi-lo))*ch }
		axis := render.WithAlpha(th.TextDim, 0.4)
		b.Rect(dst, x0, y0+ch+10, cw, 1.5, axis, axis)
		for i := 1; i < len(vals); i++ {
			b.Line(dst, x(i-1), y(vals[i-1]), x(i), y(vals[i]), 3, th.Early, th.Early)
		}
		for i, v := range vals {
			if A[i].Current {
				b.Dot(dst, x(i), y(v), 9, th.Text, th.Text)
				b.Dot(dst, x(i), y(v), 6, th.Panel, th.Panel)
			} else {
				b.Dot(dst, x(i), y(v), 5, th.Early, th.Early)
			}
			if A[i].Best && mi == 0 {
				drawStar(&b, dst, x(i), y(v)-22, 10, th.Lanes[2])
			}
		}
		labels = append(labels, label{mt.name, x0, vizY - 4, render.Left, false},
			label{"latest " + mt.fmt(vals[len(vals)-1]), x0 + cw, vizY - 4, render.Right, true},
			label{"1", x0, y0 + ch + 16, render.Left, true},
			label{fmt.Sprint(len(A)), x0 + cw, y0 + ch + 16, render.Right, true})
	}
	b.Flush(dst)
	for _, l := range labels {
		c := th.Text
		f := render.UIBold
		if l.dim {
			c, f = th.TextDim, render.Cond
		}
		render.Text(dst, l.s, f, 24, l.x, l.y, c, l.al)
	}
	// Table of the latest attempts, newest first.
	ty := vizY + ch + 110
	cols := []float64{0, 70, 420, 620, 800, 1000, 1240}
	heads := []string{"#", "When", "Score", "Hit", "Best streak", "Overstrums", "Timing"}
	if h.Drums {
		heads[5] = "Extra hits"
	}
	for i, hd := range heads {
		al := render.Left
		if i >= 2 {
			al = render.Right
		}
		render.Text(dst, strings.ToUpper(hd), render.CondBold, 20, vizX+cols[i], ty, th.TextDim, al)
	}
	rows := min(len(A), max(3, int((vizY+vizH-40-(ty+36))/40)))
	for k := 0; k < rows; k++ {
		i := len(A) - 1 - k
		a := A[i]
		y := ty + 36 + float64(k)*40
		c := th.Text
		if a.Current {
			render.FillRect(dst, vizX-10, y-4, vizW+20, 38, render.WithAlpha(th.Text, 0.06))
		}
		render.Text(dst, fmt.Sprint(i+1), render.Cond, 24, vizX, y, th.TextDim, render.Left)
		render.Text(dst, attemptDate(a), render.Cond, 24, vizX+cols[1], y, c, render.Left)
		render.Text(dst, fmt.Sprint(a.Score), render.Cond, 24, vizX+cols[2], y, c, render.Right)
		render.Text(dst, fmt.Sprintf("%.1f%%", a.HitRate*100), render.Cond, 24, vizX+cols[3], y, c, render.Right)
		render.Text(dst, fmt.Sprint(a.MaxStreak), render.Cond, 24, vizX+cols[4], y, c, render.Right)
		render.Text(dst, fmt.Sprint(a.Overstrums), render.Cond, 24, vizX+cols[5], y, c, render.Right)
		if a.Hits > 0 {
			render.Text(dst, fmt.Sprintf("%+.1f ±%.0f ms", a.MeanMs, a.StdDevMs), render.Cond, 24, vizX+cols[6], y, c, render.Right)
		}
		var flags []string
		if a.Best {
			flags = append(flags, "best")
		}
		if a.FullCombo {
			flags = append(flags, "full combo")
		}
		if !a.Completed {
			flags = append(flags, "stopped early")
		}
		if a.Current {
			flags = append(flags, "this run")
		}
		if a.Mode != "" {
			flags = append(flags, a.Mode)
		}
		render.Text(dst, strings.Join(flags, " · "), render.Cond, 22, vizX+cols[6]+40, y+1, th.TextDim, render.Left)
	}
	if len(A) > rows {
		render.Text(dst, fmt.Sprintf("latest %d of %d attempts (all of them are in the browser report)", rows, len(A)), render.Cond, 22, vizX, ty+36+float64(rows)*40+4, th.TextDim, render.Left)
	}
}

// sectionsWithNotes are the history's sections that have notes.
func sectionsWithNotes(h *replay.History) []int {
	var out []int
	for i, s := range h.Sections {
		if s.Total > 0 {
			out = append(out, i)
		}
	}
	return out
}

// attemptWindow is which attempts fit, ending back from the newest by
// back attempts.
func attemptWindow(n, fit, back int) (lo, hi, backClamped int) {
	back = max(0, min(back, n-fit))
	hi = n - back
	lo = max(0, hi-fit)
	return lo, hi, back
}

// drawSectionHistory: sections down, attempts across, colored by how much
// of the section was missed.
func drawSectionHistory(dst *ebiten.Image, h *replay.History, back int) int {
	secs := sectionsWithNotes(h)
	labelW := 220.0
	cw := math.Min(64*vizW/vizNormalW, math.Max(26, (vizW-labelW)/float64(len(h.Attempts))))
	fit := int((vizW - labelW) / cw)
	lo, hi, back := attemptWindow(len(h.Attempts), fit, back)
	rowH := math.Min(44*vizGrow(), (vizH-110)/float64(max(1, len(secs))))
	y0 := vizY + 34
	var b render.Batch
	for ri, si := range secs {
		y := y0 + float64(ri)*rowH
		for ai := lo; ai < hi; ai++ {
			a := h.Attempts[ai]
			x := vizX + labelW + float64(ai-lo)*cw
			acc := -1.0
			if si < len(a.SectionAcc) {
				acc = a.SectionAcc[si]
			}
			if acc < 0 {
				dim := render.WithAlpha(th.TextDim, 0.25)
				b.Rect(dst, x+2, y+rowH/2-1, cw-4, 2, dim, dim)
			} else {
				c := heatColor(1 - acc)
				b.Rect(dst, x+2, y+2, cw-4, rowH-4, c, c)
			}
		}
	}
	// Outline this run's column.
	for ai := lo; ai < hi; ai++ {
		if h.Attempts[ai].Current {
			x := vizX + labelW + float64(ai-lo)*cw
			hgt := float64(len(secs)) * rowH
			c := th.Text
			b.Rect(dst, x, y0, cw, 2, c, c)
			b.Rect(dst, x, y0+hgt-2, cw, 2, c, c)
			b.Rect(dst, x, y0, 2, hgt, c, c)
			b.Rect(dst, x+cw-2, y0, 2, hgt, c, c)
		}
	}
	b.Flush(dst)
	size := math.Min(24, rowH*0.6)
	for ri, si := range secs {
		y := y0 + float64(ri)*rowH
		render.Text(dst, truncateW(h.Sections[si].Name, render.UI, size, labelW-16), render.UI, size, vizX, y+rowH/2-size*0.62, th.Text, render.Left)
	}
	every := max(1, int(math.Ceil(40/cw)))
	for ai := lo; ai < hi; ai++ {
		if (ai+1)%every != 0 && ai != hi-1 && ai != lo {
			continue
		}
		c := th.TextDim
		if h.Attempts[ai].Current {
			c = th.Text
		}
		render.Text(dst, fmt.Sprint(ai+1), render.Cond, 20, vizX+labelW+float64(ai-lo)*cw+cw/2, y0-28, c, render.Center)
	}
	ly := vizY + vizH - 30
	render.Text(dst, "0% missed", render.Cond, 22, vizX, ly, th.TextDim, render.Left)
	var rb render.Batch
	for i := 0; i < 40; i++ {
		c := heatColor(float64(i) / 39)
		rb.Rect(dst, vizX+110+float64(i)*5, ly+6, 5, 16, c, c)
	}
	rb.Flush(dst)
	render.Text(dst, "100%   oldest attempt on the left, this run outlined", render.Cond, 22, vizX+320, ly, th.TextDim, render.Left)
	if hi-lo < len(h.Attempts) {
		render.Text(dst, fmt.Sprintf("attempts %d-%d of %d   up/down for older", lo+1, hi, len(h.Attempts)), render.Cond, 22, vizX+vizW, vizY+vizH-30, th.TextDim, render.Right)
	}
	return back
}

// noteHistoryGems colors the chart by each note's record: solid red for
// notes missed in the latest attempt that reached them (darker: missed more
// often), a red ring for notes missed before but hit since, faded lane
// colors for notes never missed.
func noteHistoryGems(h *replay.History) []stripGem {
	out := make([]stripGem, 0, len(h.Gems))
	for _, g := range h.Gems {
		sg := stripGem{t: g.Time, rows: g.Rows}
		head := fmtClock(g.Time) + "  " + g.Kind
		switch {
		case g.Reached == 0:
			sg.alpha, sg.info = 0.12, head+"  never reached"
		case g.Missed == 0:
			sg.alpha, sg.info = 0.3, fmt.Sprintf("%s  never missed (%d times)", head, g.Reached)
		default:
			rate := float64(g.Missed) / float64(g.Reached)
			sg.info = fmt.Sprintf("%s  missed %d of %d", head, g.Missed, g.Reached)
			if g.HitStreak > 0 {
				sg.info += fmt.Sprintf(", hit the last %d in a row", g.HitStreak)
				sg.alpha, sg.ringColor = 0.3, heatColor(math.Max(0.35, rate))
			} else {
				sg.alpha, sg.fill = 1, heatColor(rate)
			}
		}
		out = append(out, sg)
	}
	return out
}

func drawNoteHistory(dst *ebiten.Image, h *replay.History, first int, m *Mouse) {
	info := drawStrip(dst, h.Rows, noteHistoryGems(h), h.Sections, nil, nil, h.Length, first, m)
	ly := vizY + vizH - 30
	if info != "" {
		render.Text(dst, info, render.UI, 24, vizX, ly, th.Text, render.Left)
		return
	}
	render.Text(dst, "Missed last time:  rarely", render.Cond, 22, vizX, ly, th.TextDim, render.Left)
	var rb render.Batch
	for i := 0; i < 30; i++ {
		c := heatColor(0.05 + 0.95*float64(i)/29)
		rb.Rect(dst, vizX+232+float64(i)*5, ly+6, 5, 16, c, c)
	}
	ring := heatColor(0.6)
	rb.Dot(dst, vizX+530, ly+14, 6, render.WithAlpha(th.Lanes[0], 0.3), render.WithAlpha(th.Lanes[0], 0.3))
	rb.Ring(dst, vizX+530, ly+14, 7, 7, 2.4, ring, ring)
	rb.Dot(dst, vizX+845, ly+14, 6, render.WithAlpha(th.Lanes[0], 0.3), render.WithAlpha(th.Lanes[0], 0.3))
	rb.Flush(dst)
	render.Text(dst, "every time", render.Cond, 22, vizX+392, ly, th.TextDim, render.Left)
	render.Text(dst, "missed before, hit since", render.Cond, 22, vizX+546, ly, th.TextDim, render.Left)
	render.Text(dst, "never missed (lane color)", render.Cond, 22, vizX+861, ly, th.TextDim, render.Left)
}

// drawTimingHistory: a hit-offset histogram per attempt, stacked like a
// ridgeline, newest at the bottom.
func drawTimingHistory(dst *ebiten.Image, h *replay.History, back int) int {
	// Rows grow to fill the page when there are only a few attempts.
	n := len(h.Attempts)
	rowH := math.Max(30, math.Min(90, (vizH-110)/(float64(n)+1.9)))
	peak := rowH * 1.9
	fit := int((vizH - peak - 80) / rowH)
	lo, hi, back := attemptWindow(n, fit, back)
	labelW, rightW := 230.0, 110.0
	x0, w := vizX+labelW, vizW-labelW-rightW
	lim := -h.HistStartMs
	x := func(o float64) float64 { return x0 + (o+lim)/(2*lim)*w }
	top := vizY + 10
	smooth := func(hs []int) []float64 {
		out := make([]float64, len(hs))
		for i := range hs {
			s := 2 * float64(hs[i])
			if i > 0 {
				s += float64(hs[i-1])
			}
			if i+1 < len(hs) {
				s += float64(hs[i+1])
			}
			out[i] = s / 4
		}
		return out
	}
	maxC := 0.0
	sm := make([][]float64, hi-lo)
	for i := lo; i < hi; i++ {
		a := h.Attempts[i]
		sm[i-lo] = smooth(a.Hist)
		if a.Hits > 0 {
			for _, c := range sm[i-lo] {
				maxC = math.Max(maxC, c/float64(a.Hits))
			}
		}
	}
	if maxC == 0 {
		maxC = 1
	}
	var b render.Batch
	bottom := top + peak + float64(hi-lo)*rowH
	grid := render.WithAlpha(th.TextDim, 0.18)
	for _, o := range []float64{-lim, -lim / 2, lim / 2, lim} {
		b.Rect(dst, x(o), top, 1, bottom-top, grid, grid)
	}
	axis := render.WithAlpha(th.TextDim, 0.5)
	b.Rect(dst, x(0), top, 1.5, bottom-top, axis, axis)
	onTime := render.RGBA(0x9A, 0xA1, 0xB4, 0xFF)
	colorAt := func(o float64) color.RGBA {
		switch {
		case o < -replay.OnTimeMs:
			return th.Early
		case o > replay.OnTimeMs:
			return th.Late
		}
		return onTime
	}
	for i := lo; i < hi; i++ {
		a := h.Attempts[i]
		base := top + peak + float64(i-lo+1)*rowH
		alpha := 0.6
		if a.Current {
			alpha = 0.95
		}
		if a.Hits > 0 {
			hs := sm[i-lo]
			for k := range hs {
				o0 := h.HistStartMs + float64(k)*h.BucketMs
				o1 := o0 + h.BucketMs
				v0 := hs[k] / float64(a.Hits) / maxC * peak
				v1 := v0
				if k+1 < len(hs) {
					v1 = (hs[k] + hs[k+1]) / 2 / float64(a.Hits) / maxC * peak
				}
				c := render.WithAlpha(colorAt(o0+h.BucketMs/2), alpha)
				b.Quad(dst, x(o0), base-v0, x(o1), base-v1, x(o1), base, x(o0), base, c, c, c, c)
			}
			b.Rect(dst, x(a.MeanMs)-1.5, base-12, 3, 12, th.Text, th.Text)
		}
		edge := render.WithAlpha(th.Panel, 1)
		b.Rect(dst, x0, base, w, 1.5, edge, edge)
	}
	b.Flush(dst)
	for i := lo; i < hi; i++ {
		a := h.Attempts[i]
		base := top + peak + float64(i-lo+1)*rowH
		c := th.TextDim
		if a.Current {
			c = th.Text
		}
		render.Text(dst, fmt.Sprintf("#%d  %s", i+1, a.Started.Local().Format("Jan 2 3:04 PM")), render.Cond, 22, x0-16, base-26, c, render.Right)
		if a.Hits > 0 {
			render.Text(dst, fmt.Sprintf("±%.0f ms", a.StdDevMs), render.Cond, 22, x0+w+16, base-26, c, render.Left)
		}
	}
	render.Text(dst, fmt.Sprintf("-%.0f early", lim), render.Cond, 22, x(-lim), bottom+6, th.Early, render.Center)
	render.Text(dst, "0", render.Cond, 22, x(0), bottom+6, th.TextDim, render.Center)
	render.Text(dst, fmt.Sprintf("+%.0f late", lim), render.Cond, 22, x(lim), bottom+6, th.Late, render.Center)
	ly := vizY + vizH - 30
	render.Text(dst, "Where hits landed in each attempt. A tall narrow peak on 0 is tight, on-time playing; white ticks are the averages.", render.Cond, 22, vizX, ly, th.TextDim, render.Left)
	if hi-lo < len(h.Attempts) {
		render.Text(dst, fmt.Sprintf("attempts %d-%d of %d   up/down for older", lo+1, hi, len(h.Attempts)), render.Cond, 22, vizX+vizW, vizY+vizH-30, th.TextDim, render.Right)
	}
	return back
}
