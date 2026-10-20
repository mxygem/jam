package app

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// Results shows the score and the post-play analysis of a recording.
type Results struct {
	rec    *replay.Recording
	an     *replay.Analysis
	path   string
	c      *chart.Chart
	t      *chart.Track
	song   *chart.Song
	tab    int
	scroll int
	back   func(a *App)
}

var resultTabs = []string{"Sections", "Misses", "Breakdown"}

// NewResults builds the results screen. c/t/song may be nil when the chart
// a recording was made against is no longer available.
func NewResults(a *App, rec *replay.Recording, path string, c *chart.Chart, t *chart.Track, song *chart.Song) *Results {
	r := &Results{rec: rec, path: path, c: c, t: t, song: song}
	if c != nil && t != nil {
		r.an = replay.Analyze(rec, c, t)
	}
	r.back = func(a *App) { a.SetScene(NewSongSelect(a)) }
	return r
}

func (r *Results) Update(a *App) error {
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuLeft:
			r.tab = (r.tab + len(resultTabs) - 1) % len(resultTabs)
			r.scroll = 0
		case action.MenuRight:
			r.tab = (r.tab + 1) % len(resultTabs)
			r.scroll = 0
		case action.MenuUp:
			r.scroll = max(0, r.scroll-1)
		case action.MenuDown:
			r.scroll++
		case action.MenuConfirm:
			if a.Run != nil && r.song != nil {
				a.PlaylistNext()
				return nil
			}
			r.back(a) // green carries on: back to the song list
			return nil
		case action.MenuOptions:
			if r.song != nil && r.t != nil && a.Band == nil { // retry
				a.SetScene(NewLoading(a, r.song, r.t.Key.Inst, r.t.Key.Diff))
				return nil
			}
		case action.MenuBack:
			if a.Run != nil && r.song != nil {
				a.EndPlaylist()
				return nil
			}
			r.back(a)
			return nil
		}
	}
	if a.Opts.ShotScene == "results" && a.frame > 20 {
		a.TakeScreenshotNext()
	}
	return nil
}

func drawStar(b *render.Batch, dst *ebiten.Image, cx, cy, r float64, c color.RGBA) {
	pts := make([][2]float64, 10)
	for i := 0; i < 10; i++ {
		rad := r
		if i%2 == 1 {
			rad = r * 0.45
		}
		ang := -math.Pi/2 + float64(i)*math.Pi/5
		pts[i] = [2]float64{cx + rad*math.Cos(ang), cy + rad*math.Sin(ang)}
	}
	for i := 0; i < 10; i++ {
		p0, p1 := pts[i], pts[(i+1)%10]
		b.Quad(dst, cx, cy, p0[0], p0[1], p1[0], p1[1], cx, cy, c, c, c, c)
	}
}

func (r *Results) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	rec := r.rec
	res := rec.Result
	title := rec.Song.Name
	if !rec.Completed {
		title += "  (incomplete)"
	}
	Header(dst, title, fmt.Sprintf("%s   %s %s   %s", rec.Song.Artist, chartTitle(rec.Difficulty), chartTitle(rec.Instrument), rec.Started.Format("Jan 2 15:04")))

	// --- summary (left) ---
	lx, ly := 96.0, 230.0
	Panel(dst, lx-24, ly-20, 860, 300)
	render.TextShadow(dst, fmt.Sprintf("%d", res.Score), render.Display, 96, lx, ly-10, th.Text, render.Left)
	var b render.Batch
	for i := 0; i < 5; i++ {
		c := render.WithAlpha(th.TextDim, 0.3)
		if i < res.Stars {
			c = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
		}
		drawStar(&b, dst, lx+520+float64(i)*58, ly+50, 24, c)
	}
	b.Flush(dst)
	stat := func(x, y float64, k, v string) {
		render.Text(dst, k, render.Cond, 24, x, y, th.TextDim, render.Left)
		render.Text(dst, v, render.UIBold, 40, x, y+26, th.Text, render.Left)
	}
	stat(lx, ly+130, "ACCURACY", fmt.Sprintf("%.1f%%", res.HitRate()*100))
	notes := fmt.Sprintf("%d/%d", res.NotesHit, res.NotesTotal)
	if res.Reached() < res.NotesTotal {
		notes = fmt.Sprintf("%d/%d", res.NotesHit, res.Reached())
		render.Text(dst, fmt.Sprintf("of %d in the song", res.NotesTotal), render.Cond, 22, lx+200, ly+202, th.TextDim, render.Left)
	}
	stat(lx+200, ly+130, "NOTES", notes)
	stat(lx+420, ly+130, "BEST STREAK", fmt.Sprintf("%d", res.MaxStreak))
	over := "OVERSTRUMS"
	if rec.Instrument == "drums" {
		over = "OVERHITS"
	}
	stat(lx+640, ly+130, over, fmt.Sprintf("%d", res.Overstrums))
	render.Text(dst, fmt.Sprintf("Star power phrases %d/%d, activated %d", res.SPHit, res.SPPhrases, res.SPActivated), render.Cond, 24, lx, ly+222, th.TextDim, render.Left)

	// --- timing histogram ---
	hx, hy, hw, hh := 96.0, 560.0, 812.0, 300.0
	Panel(dst, hx-24, hy-20, hw+48, hh+120)
	render.Text(dst, "TIMING", render.CondBold, 26, hx, hy, th.TextDim, render.Left)
	if r.an != nil && r.an.Hits > 0 {
		an := r.an
		render.Text(dst, fmt.Sprintf("mean %+.1f ms   median %+.1f   spread ±%.1f", an.MeanMs, an.MedianMs, an.StdDevMs), render.Cond, 24, hx+hw, hy, th.Text, render.Right)
		maxC := 1
		for _, c := range an.Hist {
			maxC = max(maxC, c)
		}
		n := len(an.Hist)
		bw := hw / float64(n)
		base := hy + hh
		for i, c := range an.Hist {
			lo := an.HistStartMs + float64(i)*an.BucketMs
			col := th.Good
			switch {
			case lo+an.BucketMs <= -replay.OnTimeMs:
				col = th.Early
			case lo >= replay.OnTimeMs:
				col = th.Late
			}
			h := (hh - 60) * float64(c) / float64(maxC)
			b.Rect(dst, hx+float64(i)*bw+1, base-h, bw-2, h, render.Lighten(col, 0.2), col)
		}
		zero := hx + (-an.HistStartMs/an.BucketMs)*bw
		b.Rect(dst, zero-1, hy+44, 2, hh-44, render.WithAlpha(th.Text, 0.5), render.WithAlpha(th.Text, 0.5))
		mean := hx + ((an.MeanMs-an.HistStartMs)/an.BucketMs)*bw
		b.Rect(dst, mean-2, hy+44, 4, hh-44, th.Accent, th.Accent)
		b.Flush(dst)
		render.Text(dst, "early", render.Cond, 22, hx, base+8, th.Early, render.Left)
		render.Text(dst, "late", render.Cond, 22, hx+hw, base+8, th.Late, render.Right)
		render.Text(dst, fmt.Sprintf("%d early   %d on time (±%.0f ms)   %d late", an.Early, an.OnTime, replay.OnTimeMs, an.Late), render.Cond, 24, hx+hw/2, base+8, th.Text, render.Center)
	} else {
		render.Text(dst, "No hits to analyze.", render.UI, 26, hx, hy+60, th.TextDim, render.Left)
	}

	// --- tabs (right) ---
	rx, ry, rw := 1010.0, 230.0, 814.0
	Panel(dst, rx-24, ry-20, rw+48, 770)
	x := rx
	for i, name := range resultTabs {
		c := th.TextDim
		if i == r.tab {
			c = th.Text
			w, _ := render.Measure(name, render.CondBold, 30)
			render.FillRect(dst, x, ry+40, w, 4, th.Accent)
		}
		render.Text(dst, name, render.CondBold, 30, x, ry, c, render.Left)
		w, _ := render.Measure(name, render.CondBold, 30)
		x += w + 40
	}
	if r.an == nil {
		render.Text(dst, "The chart for this run isn't available, so only the score is shown.", render.UI, 24, rx, ry+80, th.TextDim, render.Left)
	} else {
		r.drawTab(dst, rx, ry+70, rw)
	}
	foot := "Enter / green back     Left/right tabs     Up/down scroll"
	if r.song != nil && a.Band == nil {
		foot = "Enter / green song list     Tab / yellow retry     Left/right tabs     Up/down scroll"
	}
	if a.Run != nil && r.song != nil {
		next := "finish playlist"
		if e := a.Run.Next(); e != nil {
			next = "next: " + truncateW(e.Title, render.Cond, 26, 520)
		}
		foot = fmt.Sprintf("Enter / green %s     Tab / yellow retry     Left/right tabs     Esc / red leave playlist (%d of %d)", next, a.Run.idx+1, len(a.Run.list.Entries))
	}
	if r.path != "" {
		foot += "     saved: " + filepath.Base(r.path)
	}
	Footer(dst, foot)
}

func chartTitle(s string) string {
	if d, ok := chart.ParseDifficulty(s); ok {
		return d.Title()
	}
	if i, ok := chart.ParseInstrument(s); ok {
		return i.Title()
	}
	return s
}

func (r *Results) drawTab(dst *ebiten.Image, x, y, w float64) {
	an := r.an
	const rows = 15
	rowH := 44.0
	clampScroll := func(n int) {
		if r.scroll > max(0, n-rows) {
			r.scroll = max(0, n-rows)
		}
	}
	switch r.tab {
	case 0:
		var secs []replay.SectionStat
		for _, s := range an.Sections {
			if s.Total > 0 || s.Overstrums > 0 {
				secs = append(secs, s)
			}
		}
		clampScroll(len(secs))
		for i := r.scroll; i < len(secs) && i < r.scroll+rows; i++ {
			s := secs[i]
			yy := y + float64(i-r.scroll)*rowH
			acc := s.Accuracy()
			col := th.Good
			switch {
			case acc < 0.75:
				col = th.Bad
			case acc < 0.95:
				col = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
			}
			render.Text(dst, truncateW(s.Name, render.UI, 26, 300), render.UI, 26, x, yy, th.Text, render.Left)
			bx := x + 320
			render.FillRect(dst, bx, yy+10, 220, 14, render.WithAlpha(th.TextDim, 0.2))
			if s.Judged() == 0 {
				render.Text(dst, "not reached", render.Cond, 24, bx+240, yy+2, th.TextDim, render.Left)
				continue
			}
			render.FillRect(dst, bx, yy+10, 220*acc, 14, col)
			frac := fmt.Sprintf("%d/%d", s.Hits, s.Total)
			if s.Judged() < s.Total {
				frac = fmt.Sprintf("%d/%d of %d", s.Hits, s.Judged(), s.Total)
			}
			render.Text(dst, frac, render.Cond, 24, bx+240, yy+2, th.Text, render.Left)
			extra := fmt.Sprintf("%+.0f ms", s.MeanMs)
			if s.Overstrums > 0 {
				extra = fmt.Sprintf("%d over  ", s.Overstrums) + extra
			}
			render.Text(dst, extra, render.Cond, 24, x+w, yy+2, th.TextDim, render.Right)
		}
	case 1:
		if len(an.Misses) == 0 {
			render.Text(dst, "No misses. Nice.", render.UI, 28, x, y, th.Good, render.Left)
			return
		}
		clampScroll(len(an.Misses))
		for i := r.scroll; i < len(an.Misses) && i < r.scroll+rows; i++ {
			m := an.Misses[i]
			yy := y + float64(i-r.scroll)*rowH
			render.Text(dst, fmtClock(m.Time), render.Mono, 22, x, yy+4, th.TextDim, render.Left)
			render.Text(dst, truncateW(m.Section, render.Cond, 24, 170), render.Cond, 24, x+110, yy+2, th.TextDim, render.Left)
			r.drawLanes(dst, m, x+290, yy+16)
			render.Text(dst, truncateW(m.Reason, render.UI, 24, w-430), render.UI, 24, x+w, yy+2, th.Text, render.Right)
		}
	case 2:
		yy := y
		line := func(s string, c color.RGBA) {
			render.Text(dst, truncateW(s, render.UI, 25, w), render.UI, 25, x, yy, c, render.Left)
			yy += 40
		}
		for _, t := range an.Tips {
			line(t, th.Text)
		}
		if len(an.Tips) > 0 {
			yy += 16
		}
		for _, k := range an.Kinds {
			pct := 0.0
			if k.Total > 0 {
				pct = float64(k.Hits) / float64(k.Total) * 100
			}
			line(fmt.Sprintf("%-14s %d/%d (%.0f%%)   mean %+.1f ms", k.Kind, k.Hits, k.Total, pct, k.MeanMs), th.TextDim)
		}
		if an.SustainTotal > 0 {
			line(fmt.Sprintf("Sustains held to the end: %d/%d (avg %.0f%% held)", an.SustainDone, an.SustainTotal, an.SustainAvg*100), th.TextDim)
		}
		if an.FretLeadMs > 0 {
			line(fmt.Sprintf("Frets landed a median %.0f ms before each strum", an.FretLeadMs), th.TextDim)
		}
		if len(an.Breaks) > 0 {
			yy += 10
			line("Streak breaks:", th.Text)
			for i, br := range an.Breaks {
				if i >= 5 {
					break
				}
				line(fmt.Sprintf("  %s  lost %d in %s (%s)", fmtClock(br.Time), br.Lost, br.Section, br.Cause), th.TextDim)
			}
		}
	}
}

func (r *Results) drawLanes(dst *ebiten.Image, m replay.MissDetail, x, y float64) {
	if r.t == nil || m.Note < 0 || m.Note >= len(r.t.Notes) {
		return
	}
	if r.t.Key.Inst.IsDrums() {
		render.Text(dst, m.Lanes, render.Cond, 24, x, y-14, th.Text, render.Left)
		return
	}
	n := &r.t.Notes[m.Note]
	var b render.Batch
	if n.IsOpen() {
		b.Rect(dst, x, y-5, 120, 10, th.Lanes[5], th.Lanes[5])
	}
	for l := 0; l < 5; l++ {
		c := render.WithAlpha(th.TextDim, 0.25)
		if n.Lanes&(1<<l) != 0 {
			c = th.Lanes[l]
		}
		b.Ellipse(dst, x+10+float64(l)*25, y, 10, 10, c, c)
	}
	b.Flush(dst)
}

func fmtClock(t float64) string {
	if t < 0 {
		t = 0
	}
	m := int(t) / 60
	return fmt.Sprintf("%d:%04.1f", m, t-float64(m*60))
}
