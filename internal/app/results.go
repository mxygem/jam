package app

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/osopen"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
	"github.com/mxygem/jam/internal/rich"
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

// Tab bar placement: under the header normally, along the top of the
// window with large charts.
const (
	tabSize      = 28.0
	tabSizeLarge = 25.0
)

// tabRects are the tab bar's labels (x, y, w, h), for drawing and clicks.
func tabRects(x0, y, size float64) [][4]float64 {
	var out [][4]float64
	x := x0
	gap := size * 30 / 28
	for i, name := range resultTabs {
		for _, g := range resultGroups {
			if g == i {
				x += gap - 4
			}
		}
		w, _ := render.Measure(name, render.CondBold, size)
		out = append(out, [4]float64{x, y, w, size * 40 / 28})
		x += w + gap
	}
	return out
}

// large reports whether the current page draws its charts across the
// whole window.
func (r *Results) large(a *App) bool { return r.tab >= tabTiming && a.Settings.LargeCharts }

// stageSize is the whole window in canvas pixels and where the 1920x1080
// canvas sits in it.
func stageSize(a *App) (w, h, ox, oy float64) {
	if a.viewW == 0 {
		return render.ScreenW, render.ScreenH, 0, 0
	}
	ox, oy = a.canvasOffset()
	return float64(a.viewW), float64(a.viewH), ox, oy
}

// resultButton is a clickable pill at the top right.
type resultButton struct {
	label   string
	x, y, w float64
	act     func(a *App)
}

const resultButtonH = 52.0

// buttons are the top-right buttons, in canvas coordinates normally and
// window coordinates with large charts.
func (r *Results) buttons(a *App) []resultButton {
	var out []resultButton
	if r.view == nil {
		return out
	}
	right, y := 1860.0, 40.0
	if r.large(a) {
		w, _, _, _ := stageSize(a)
		right, y = w-24, 14
	}
	add := func(label string, act func(a *App)) {
		tw, _ := render.Measure(label, render.CondBold, 26)
		bw := tw + 44
		right -= bw
		out = append(out, resultButton{label, right, y, bw, act})
		right -= 14
	}
	add("Open in browser (B)", r.openReport)
	switch {
	case r.large(a):
		add("Normal view (F)", r.toggleLarge)
	case r.tab >= tabTiming:
		add("Large view (F)", r.toggleLarge)
	}
	return out
}

// toggleLarge switches the charts between the panel and the whole window
// (a saved setting). From the overview pages it opens the charts large.
func (r *Results) toggleLarge(a *App) {
	if r.tab < tabTiming {
		r.tab, r.scroll = tabTiming, 0
		a.Settings.LargeCharts = true
	} else {
		a.Settings.LargeCharts = !a.Settings.LargeCharts
	}
	_ = a.Settings.Save()
}

func (r *Results) Update(a *App) error {
	r.startHistory(a)
	if inpututil.IsKeyJustPressed(ebiten.KeyB) {
		r.openReport(a)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF) && r.view != nil {
		r.toggleLarge(a)
	}
	if m := a.Mouse; m.Click {
		rects := tabRects(96, 206, tabSize)
		if r.large(a) {
			_, _, ox, oy := stageSize(a)
			m.X, m.Y = m.X+ox, m.Y+oy
			rects = tabRects(40, 24, tabSizeLarge)
		}
		clicked := false
		for _, b := range r.buttons(a) {
			if m.In(b.x, b.y, b.w, resultButtonH) {
				b.act(a)
				clicked = true
			}
		}
		for i, rc := range rects {
			if !clicked && m.In(rc[0]-10, rc[1]-6, rc[2]+20, rc[3]+12) {
				r.tab, r.scroll = i, 0
			}
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
	if r.large(a) {
		r.drawLarge(dst, a)
		return
	}
	Header(dst, title, fmt.Sprintf("%s   %s %s   %s", rec.Song.Artist, chartTitle(rec.Difficulty), chartTitle(rec.Instrument), rec.Started.Format("Jan 2 15:04")))

	r.drawTabBar(dst, 96, 206, tabSize)
	r.drawButtons(dst, a, a.Mouse.X, a.Mouse.Y)
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
func (r *Results) drawTabBar(dst *ebiten.Image, x0, y, size float64) {
	rects := tabRects(x0, y, size)
	k := size / 28
	for i, name := range resultTabs {
		rc := rects[i]
		c := th.TextDim
		if i == r.tab {
			c = th.Text
			render.FillRect(dst, rc[0], rc[1]+38*k, rc[2], 4, th.Accent)
		}
		for _, g := range resultGroups {
			if g == i {
				render.FillRect(dst, rc[0]-28*k, rc[1]+6*k, 2, 28*k, render.WithAlpha(th.TextDim, 0.4))
			}
		}
		render.Text(dst, name, render.CondBold, size, rc[0], rc[1], c, render.Left)
	}
}

// drawButtons draws the top-right buttons; mx, my is the pointer in the
// same coordinates.
func (r *Results) drawButtons(dst *ebiten.Image, a *App, mx, my float64) {
	for _, b := range r.buttons(a) {
		c := render.WithAlpha(th.Panel, 1)
		if mx >= b.x && mx < b.x+b.w && my >= b.y && my < b.y+resultButtonH {
			c = render.Lighten(c, 0.12)
		}
		render.RoundRect(dst, b.x, b.y, b.w, resultButtonH, 14, c)
		render.Text(dst, b.label, render.CondBold, 26, b.x+b.w/2, b.y+10, th.Text, render.Center)
	}
}

// drawPage draws one of the visual pages in the panel under the tabs.
func (r *Results) drawPage(dst *ebiten.Image, a *App) {
	Panel(dst, 72, 256, 1776, 744)
	setVizArea(vizNormalX, vizNormalY, vizNormalW, vizNormalH)
	r.drawPageContent(dst, &a.Mouse)
}

// drawLarge draws a visual page across the whole window (which can be
// wider than the canvas): tabs along the top, the chart filling the rest.
func (r *Results) drawLarge(dst *ebiten.Image, a *App) {
	img, w, h, ox, oy := render.Backdrop(dst)
	m := a.Mouse
	m.X, m.Y = m.X+ox, m.Y+oy
	r.drawTabBar(img, 40, 24, tabSizeLarge)
	r.drawButtons(img, a, m.X, m.Y)
	Panel(img, 24, 80, w-48, h-80-64)
	setVizArea(56, 112, w-112, h-112-92)
	defer setVizArea(vizNormalX, vizNormalY, vizNormalW, vizNormalH)
	r.drawPageContent(img, &m)

	// Bottom line: what this is on the left, controls on the right.
	rec := r.rec
	what := fmt.Sprintf("%s · %s   %s %s   %s", rich.Plain(rec.Song.Name), rich.Plain(rec.Song.Artist), chartTitle(rec.Difficulty), chartTitle(rec.Instrument), rec.Started.Format("Jan 2 15:04"))
	keys := "Enter / green back     Left/right pages     Up/down scroll     F normal view"
	if r.song != nil && a.Band == nil {
		keys = "Enter / green song list     Tab / yellow retry     Left/right pages     Up/down scroll     F normal view"
	}
	if a.Run != nil && r.song != nil {
		keys = "Enter / green next     Esc / red leave playlist     Left/right pages     Up/down scroll     F normal view"
	}
	kw, _ := render.Measure(keys, render.Cond, 24)
	render.Text(img, keys, render.Cond, 24, w-40, h-50, th.TextDim, render.Right)
	render.Text(img, truncateW(what, render.Cond, 24, math.Max(200, w-80-kw-40)), render.Cond, 24, 40, h-50, th.TextDim, render.Left)
}

// drawPageContent draws the current visual page in the viz area; m is the
// pointer in dst's coordinates.
func (r *Results) drawPageContent(dst *ebiten.Image, m *Mouse) {
	if r.view == nil {
		render.Text(dst, "The chart for this run isn't available, so there's nothing to draw.", render.UI, 26, vizX, vizY+20, th.TextDim, render.Left)
		return
	}
	v := r.view
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
		r.drawBreakdown(dst, x, y, w)
	}
}

// breakdownLine is one line of the Breakdown tab ("" is a gap).
type breakdownLine struct {
	s string
	c color.RGBA
}

const (
	breakdownSize  = 25.0
	breakdownLineH = 38.0
)

// breakdownLines is the Breakdown tab's text wrapped to width w.
func (r *Results) breakdownLines(w float64) []breakdownLine {
	an := r.an
	var out []breakdownLine
	add := func(s string, c color.RGBA, indent string) {
		for i, l := range wrapText(s, render.UI, breakdownSize, w) {
			if i > 0 {
				l = indent + l
			}
			out = append(out, breakdownLine{l, c})
		}
	}
	gap := func() {
		if len(out) > 0 && out[len(out)-1].s != "" {
			out = append(out, breakdownLine{})
		}
	}
	for _, t := range an.Tips {
		add(t, th.Text, "")
	}
	gap()
	for _, k := range an.Kinds {
		pct := 0.0
		if k.Total > 0 {
			pct = float64(k.Hits) / float64(k.Total) * 100
		}
		add(fmt.Sprintf("%s: %d/%d (%.0f%%), mean %+.1f ms", strings.Title(k.Kind), k.Hits, k.Total, pct, k.MeanMs), th.TextDim, "    ")
	}
	if an.SustainTotal > 0 {
		add(fmt.Sprintf("Sustains held to the end: %d/%d (avg %.0f%% held)", an.SustainDone, an.SustainTotal, an.SustainAvg*100), th.TextDim, "    ")
	}
	if an.FretLeadMs > 0 {
		add(fmt.Sprintf("Frets landed a median %.0f ms before each strum", an.FretLeadMs), th.TextDim, "    ")
	}
	if len(an.Breaks) > 0 {
		gap()
		add(fmt.Sprintf("Streak breaks (%d):", len(an.Breaks)), th.Text, "")
		for _, br := range an.Breaks {
			add(fmt.Sprintf("%s  lost %d in %s (%s)", fmtClock(br.Time), br.Lost, br.Section, br.Cause), th.TextDim, "        ")
		}
	}
	for len(out) > 0 && out[len(out)-1].s == "" {
		out = out[:len(out)-1]
	}
	return out
}

// breakdownVisible is how many lines fit in the right panel.
const breakdownVisible = 17

// drawBreakdown draws the wrapped Breakdown text from line r.scroll, with a
// scroll bar when it doesn't all fit.
func (r *Results) drawBreakdown(dst *ebiten.Image, x, y, w float64) {
	lines := r.breakdownLines(w - 20)
	if len(lines) == 0 {
		render.Text(dst, "Nothing to add for this run.", render.UI, breakdownSize, x, y, th.TextDim, render.Left)
		return
	}
	vis := breakdownVisible
	r.scroll = min(r.scroll, max(0, len(lines)-vis))
	for i := r.scroll; i < len(lines) && i < r.scroll+vis; i++ {
		if l := lines[i]; l.s != "" {
			render.Text(dst, l.s, render.UI, breakdownSize, x, y+float64(i-r.scroll)*breakdownLineH, l.c, render.Left)
		}
	}
	if len(lines) <= vis {
		return
	}
	// Scroll bar along the panel's right edge, and where you are.
	trackH := float64(vis)*breakdownLineH - 8
	bx := x + w + 8
	render.FillRect(dst, bx, y, 4, trackH, render.WithAlpha(th.TextDim, 0.2))
	thumb := math.Max(30, trackH*float64(vis)/float64(len(lines)))
	ty := y + (trackH-thumb)*float64(r.scroll)/float64(len(lines)-vis)
	render.RoundRect(dst, bx-1, ty, 6, thumb, 3, th.TextDim)
	render.Text(dst, fmt.Sprintf("lines %d-%d of %d   up/down to scroll", r.scroll+1, min(len(lines), r.scroll+vis), len(lines)),
		render.Cond, 22, x+w, y+float64(vis)*breakdownLineH+6, th.TextDim, render.Right)
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
