package app

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/osopen"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// Results shows the score and the post-play analysis of a recording, the
// run visuals, and the player's history on the part.
type Results struct {
	rec    *replay.Recording
	an     *replay.Analysis
	view   *replay.RunView
	path   string
	c      *chart.Chart
	t      *chart.Track
	song   *chart.Song
	tab    int
	scroll int
	back   func(a *App)

	// History loads in the background the first time it's needed.
	histOnce sync.Once
	histMu   sync.Mutex
	hist     *replay.History
	histDone bool
}

// Results pages: the overview (its right panel shows one of the first
// three), the run visuals, and the history.
var resultTabs = []string{"Sections", "Misses", "Breakdown", "Timing", "Chart", "Frets", "Report card", "Progress", "Section history", "Note history", "Timing history"}

const (
	tabTiming = 3 + iota
	tabChart
	tabFrets
	tabCard
	tabProgress
	tabSectionHist
	tabNoteHist
	tabTimingHist
)

// tab groups, for the separators in the tab bar
var resultGroups = []int{3, 7}

// NewResults builds the results screen. c/t/song may be nil when the chart
// a recording was made against is no longer available.
func NewResults(a *App, rec *replay.Recording, path string, c *chart.Chart, t *chart.Track, song *chart.Song) *Results {
	r := &Results{rec: rec, path: path, c: c, t: t, song: song}
	if c != nil && t != nil {
		r.an = replay.Analyze(rec, c, t)
		r.view = replay.BuildRunView(rec, c, t, r.an)
	}
	r.back = func(a *App) { a.SetScene(NewSongSelect(a)) }
	return r
}

// startHistory gathers the player's other runs of this part.
func (r *Results) startHistory(a *App) {
	if r.c == nil || r.rec == nil {
		return
	}
	r.histOnce.Do(func() {
		rec := r.rec
		inst, _ := chart.ParseInstrument(rec.Instrument)
		diff, _ := chart.ParseDifficulty(rec.Difficulty)
		full := r.c.Track(inst, diff)
		if full == nil {
			full = r.t
		}
		owns := func(id string) bool { return id == rec.Profile }
		if a != nil && a.Profiles != nil {
			owns = a.Profiles.OwnerOf(rec.Profile)
		}
		song := r.song
		go func() {
			hash := ""
			if song == nil {
				song, _ = chart.LoadSongFolder(rec.Song.Dir)
			}
			if song != nil {
				hash = song.ChartHash()
			}
			_, h := replay.BuildReport(rec, r.path, r.c, full, hash, config.RunsDir(), owns)
			r.histMu.Lock()
			r.hist, r.histDone = h, true
			r.histMu.Unlock()
		}()
	})
}

func (r *Results) history() (*replay.History, bool) {
	r.histMu.Lock()
	defer r.histMu.Unlock()
	return r.hist, r.histDone
}

// openReport writes the HTML report and opens it in the browser.
func (r *Results) openReport(a *App) {
	if r.view == nil {
		a.Toast("The chart for this run isn't available, so there's no report.")
		return
	}
	h, done := r.history()
	if !done {
		a.Toast("Still gathering your history; try again in a moment.")
		return
	}
	name := replay.ReportName(r.path)
	if r.path == "" {
		name = "run.html"
	}
	path := filepath.Join(config.ReportsDir(), name)
	if err := replay.SaveReport(path, r.view, h); err != nil {
		a.Toast("Couldn't write the report: " + err.Error())
		return
	}
	if err := osopen.Open(path); err != nil {
		a.Toast("Report saved to " + path)
		return
	}
	a.Toast("Opened the report in your browser")
}

// tabRects are the tab bar's labels, for mouse clicks.
func tabRects() [][4]float64 {
	var out [][4]float64
	x := 96.0
	for i, name := range resultTabs {
		for _, g := range resultGroups {
			if g == i {
				x += 26
			}
		}
		w, _ := render.Measure(name, render.CondBold, 28)
		out = append(out, [4]float64{x, 206, w, 40})
		x += w + 30
	}
	return out
}

func (r *Results) Update(a *App) error {
	r.startHistory(a)
	if inpututil.IsKeyJustPressed(ebiten.KeyB) {
		r.openReport(a)
	}
	if m := &a.Mouse; m.Click {
		for i, rc := range tabRects() {
			if m.In(rc[0]-10, rc[1]-6, rc[2]+20, rc[3]+12) {
				r.tab, r.scroll = i, 0
			}
		}
		if m.In(1600, 40, 260, 56) {
			r.openReport(a)
		}
	}
	if w := a.Mouse.Wheel; w != 0 {
		if w > 0 {
			r.scroll = max(0, r.scroll-1)
		} else {
			r.scroll++
		}
	}
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

	r.drawTabBar(dst)
	r.drawReportButton(dst, a)
	if r.tab >= tabTiming {
		r.drawPage(dst, a)
		r.drawFooter(dst, a)
		return
	}

	// --- summary (left) ---
	lx, ly := 96.0, 272.0
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
	hx, hy, hw, hh := 96.0, 600.0, 812.0, 280.0
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
	rx, ry, rw := 1010.0, 272.0, 814.0
	Panel(dst, rx-24, ry-20, rw+48, 728)
	if r.an == nil {
		render.Text(dst, "The chart for this run isn't available, so only the score is shown.", render.UI, 24, rx, ry+10, th.TextDim, render.Left)
	} else {
		r.drawTab(dst, rx, ry, rw)
	}
	r.drawFooter(dst, a)
}

// drawTabBar draws every page's name with the current one underlined.
func (r *Results) drawTabBar(dst *ebiten.Image) {
	rects := tabRects()
	for i, name := range resultTabs {
		rc := rects[i]
		c := th.TextDim
		if i == r.tab {
			c = th.Text
			render.FillRect(dst, rc[0], rc[1]+38, rc[2], 4, th.Accent)
		}
		for _, g := range resultGroups {
			if g == i {
				render.FillRect(dst, rc[0]-28, rc[1]+6, 2, 28, render.WithAlpha(th.TextDim, 0.4))
			}
		}
		render.Text(dst, name, render.CondBold, 28, rc[0], rc[1], c, render.Left)
	}
}

func (r *Results) drawReportButton(dst *ebiten.Image, a *App) {
	if r.view == nil {
		return
	}
	x, y, w, h := 1600.0, 40.0, 260.0, 56.0
	c := render.WithAlpha(th.Panel, 1)
	if a.Mouse.In(x, y, w, h) {
		c = render.Lighten(c, 0.12)
	}
	render.RoundRect(dst, x, y, w, h, 14, c)
	render.Text(dst, "Open in browser (B)", render.CondBold, 26, x+w/2, y+12, th.Text, render.Center)
}

// drawPage draws one of the full-width visual pages.
func (r *Results) drawPage(dst *ebiten.Image, a *App) {
	Panel(dst, 72, 256, 1776, 744)
	if r.view == nil {
		render.Text(dst, "The chart for this run isn't available, so there's nothing to draw.", render.UI, 26, vizX, vizY+20, th.TextDim, render.Left)
		return
	}
	v := r.view
	m := &a.Mouse
	switch r.tab {
	case tabTiming:
		drawTimingView(dst, v, m)
	case tabChart:
		rows, vis := stripRows(v.Length, len(v.Rows))
		r.scroll = min(r.scroll, max(0, rows-vis))
		drawChartView(dst, v, r.scroll, m)
	case tabFrets:
		r.scroll = drawFretView(dst, v, r.scroll)
	case tabCard:
		r.scroll = min(r.scroll, max(0, len(reportRows(v))-cardVisible()))
		drawReportCard(dst, v, r.scroll)
	default:
		h, done := r.history()
		switch {
		case !done:
			render.Text(dst, "Gathering your earlier runs of this part...", render.UI, 26, vizX, vizY+20, th.TextDim, render.Left)
			return
		case h == nil || len(h.Attempts) == 0:
			render.Text(dst, "No runs of this part to compare yet.", render.UI, 26, vizX, vizY+20, th.TextDim, render.Left)
			return
		case len(h.Attempts) == 1:
			render.Text(dst, "This is your only run of this part so far.", render.UIBold, 30, vizX, vizY+20, th.Text, render.Left)
			render.Text(dst, "Play it again and these pages show how each section, note and your timing change across attempts.", render.UI, 26, vizX, vizY+70, th.TextDim, render.Left)
			return
		}
		switch r.tab {
		case tabProgress:
			drawProgressView(dst, h)
		case tabSectionHist:
			r.scroll = drawSectionHistory(dst, h, r.scroll)
		case tabNoteHist:
			rows, vis := stripRows(h.Length, len(h.Rows))
			r.scroll = min(r.scroll, max(0, rows-vis))
			drawNoteHistory(dst, h, r.scroll, m)
		case tabTimingHist:
			r.scroll = drawTimingHistory(dst, h, r.scroll)
		}
	}
}

func (r *Results) drawFooter(dst *ebiten.Image, a *App) {
	foot := "Enter / green back     Left/right pages     Up/down scroll"
	if r.song != nil && a.Band == nil {
		foot = "Enter / green song list     Tab / yellow retry     Left/right pages     Up/down scroll"
	}
	if a.Run != nil && r.song != nil {
		next := "finish playlist"
		if e := a.Run.Next(); e != nil {
			next = "next: " + truncateW(e.Title, render.Cond, 26, 520)
		}
		foot = fmt.Sprintf("Enter / green %s     Tab / yellow retry     Left/right pages     Esc / red leave playlist (%d of %d)", next, a.Run.idx+1, len(a.Run.list.Entries))
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
