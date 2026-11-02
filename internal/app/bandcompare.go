package app

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// BandCompare lays every player's play breakdown side by side after a
// multiplayer song: one column per player, one row per stat, the best value
// in each row picked out. It opens from the band results screen.
type BandCompare struct {
	back   func(a *App)
	song   *chart.Song
	cols   []*compareCol
	rows   []compareRow
	scroll int
}

// compareCol is one player's column.
type compareCol struct {
	sr *seatResult
	an *replay.Analysis // nil when the chart for the run isn't available
	v  *replay.RunView
}

// compareRow is one row of the table: a label and one cell per player.
type compareRow struct {
	label string
	cells []string
	best  []bool // the cell holds the row's best value
	dim   bool   // a quiet row (detail rather than headline)
	head  bool   // a heading row with no cells
	// accs, for section rows, is each player's accuracy (-1: not reached)
	// so the cell can be colored.
	accs []float64
}

const (
	compareLabelW = 330.0
	compareRowH   = 44.0
	compareTop    = 330.0
	compareBottom = 975.0
)

func compareVisible() int { return int(math.Floor((compareBottom - compareTop) / compareRowH)) }

// NewBandCompare builds the comparison for a band's results. c is the chart
// the song was played on; back returns to the results screen.
func NewBandCompare(a *App, song *chart.Song, c *chart.Chart, res []*seatResult, back func(a *App)) *BandCompare {
	b := &BandCompare{back: back, song: song}
	for _, sr := range res {
		col := &compareCol{sr: sr}
		if c != nil && sr.t != nil {
			col.an = replay.Analyze(sr.rec, c, sr.t)
			col.v = replay.BuildRunView(sr.rec, c, sr.t, col.an)
		}
		b.cols = append(b.cols, col)
	}
	b.rows = b.buildRows()
	return b
}

// pickBest marks the best of vals (among valid cells); nothing is marked
// when every valid value is the same or fewer than two players have one.
func pickBest(vals []float64, valid []bool, lower bool) []bool {
	best := make([]bool, len(vals))
	n := 0
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, v := range vals {
		if !valid[i] {
			continue
		}
		n++
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if n < 2 || lo == hi {
		return best
	}
	target := hi
	if lower {
		target = lo
	}
	for i, v := range vals {
		best[i] = valid[i] && v == target
	}
	return best
}

// numRow makes a row from numbers. A column without an analysis shows "-"
// when need is set (the stat comes from the analysis).
func (b *BandCompare) numRow(label string, dim, lower, need bool, get func(c *compareCol) float64, format func(float64) string) compareRow {
	r := compareRow{label: label, dim: dim}
	vals := make([]float64, len(b.cols))
	valid := make([]bool, len(b.cols))
	for i, c := range b.cols {
		if need && c.an == nil {
			r.cells = append(r.cells, "-")
			continue
		}
		vals[i] = get(c)
		valid[i] = true
		r.cells = append(r.cells, format(vals[i]))
	}
	r.best = pickBest(vals, valid, lower)
	return r
}

func intf(v float64) string { return fmt.Sprintf("%d", int(math.Round(v))) }

func (b *BandCompare) buildRows() []compareRow {
	var rows []compareRow
	res := func(c *compareCol) replay.Summary { return c.sr.rec.Result }

	rows = append(rows,
		b.numRow("Score", false, false, false, func(c *compareCol) float64 { return float64(res(c).Score) }, intf),
		b.numRow("Stars", false, false, false, func(c *compareCol) float64 { return float64(res(c).Stars) }, intf),
		b.numRow("Hit rate", false, false, false, func(c *compareCol) float64 { return res(c).HitRate() * 100 },
			func(v float64) string { return fmt.Sprintf("%.1f%%", v) }),
	)

	// Notes hit as "n / N": the best is the most hit, so rank on the count.
	notes := b.numRow("Notes hit", false, false, false, func(c *compareCol) float64 { return float64(res(c).NotesHit) }, intf)
	for i, c := range b.cols {
		notes.cells[i] = fmt.Sprintf("%d / %d", res(c).NotesHit, res(c).NotesTotal)
	}
	rows = append(rows, notes)

	rows = append(rows,
		b.numRow("Misses", false, true, false, func(c *compareCol) float64 { return float64(res(c).NotesMissed) }, intf),
	)

	drums := 0
	for _, c := range b.cols {
		if c.v != nil && c.v.Drums {
			drums++
		}
	}
	over := "Overstrums"
	switch {
	case drums == len(b.cols):
		over = "Extra hits"
	case drums > 0:
		over = "Overstrums / extra hits"
	}
	rows = append(rows,
		b.numRow(over, false, true, false, func(c *compareCol) float64 { return float64(res(c).Overstrums) }, intf),
		b.numRow("Best streak", false, false, false, func(c *compareCol) float64 { return float64(res(c).MaxStreak) }, intf),
		b.numRow("Streak breaks", true, true, true, func(c *compareCol) float64 { return float64(len(c.an.Breaks)) }, intf),
	)

	// Timing: the spread decides the best (steadier is better); the mean
	// shows which way the player leans.
	timing := b.numRow("Timing (avg ± spread)", false, true, true, func(c *compareCol) float64 { return c.an.StdDevMs },
		func(v float64) string { return "" })
	for i, c := range b.cols {
		if c.an != nil && c.an.Hits > 0 {
			timing.cells[i] = fmt.Sprintf("%+.1f ± %.0f ms", c.an.MeanMs, c.an.StdDevMs)
		} else {
			timing.cells[i] = "-"
			timing.best[i] = false
		}
	}
	rows = append(rows, timing)
	rows = append(rows,
		b.numRow("Hits more than 15 ms early", true, true, true, func(c *compareCol) float64 { return float64(c.an.Early) }, intf),
		b.numRow("Hits more than 15 ms late", true, true, true, func(c *compareCol) float64 { return float64(c.an.Late) }, intf),
	)

	// Misses by cause, only the causes someone had.
	causes := map[string][]int{}
	for i, c := range b.cols {
		if c.v == nil {
			continue
		}
		for _, s := range c.v.Sections {
			for cat, n := range s.MissBy {
				if causes[cat] == nil {
					causes[cat] = make([]int, len(b.cols))
				}
				causes[cat][i] += n
			}
		}
	}
	for _, cat := range replay.MissCategories {
		counts, ok := causes[cat]
		if !ok {
			continue
		}
		total := 0
		for _, n := range counts {
			total += n
		}
		if total == 0 {
			continue
		}
		cat := cat
		counts = causes[cat]
		rows = append(rows, b.numRow("Missed: "+strings.ToLower(replay.MissCategoryTitle(cat)), true, true, true,
			func(c *compareCol) float64 {
				for i, x := range b.cols {
					if x == c {
						return float64(counts[i])
					}
				}
				return 0
			}, intf))
	}

	// Star power and sustains.
	sp := b.numRow("Star power phrases", true, false, false, func(c *compareCol) float64 { return float64(res(c).SPHit) }, intf)
	anySP := false
	for i, c := range b.cols {
		if res(c).SPPhrases > 0 {
			anySP = true
		}
		sp.cells[i] = fmt.Sprintf("%d / %d", res(c).SPHit, res(c).SPPhrases)
	}
	if anySP {
		rows = append(rows, sp,
			b.numRow("Star power used", true, false, false, func(c *compareCol) float64 { return float64(res(c).SPActivated) }, intf))
	}
	sus := b.numRow("Sustains held to the end", true, false, true, func(c *compareCol) float64 { return float64(c.an.SustainDone) }, intf)
	anySus := false
	for i, c := range b.cols {
		if c.an == nil {
			continue
		}
		if c.an.SustainTotal > 0 {
			anySus = true
			sus.cells[i] = fmt.Sprintf("%d / %d", c.an.SustainDone, c.an.SustainTotal)
		} else {
			sus.cells[i] = "-"
			sus.best[i] = false
		}
	}
	if anySus {
		rows = append(rows, sus)
	}
	saves := b.numRow("Times saved by the band", true, true, false, func(c *compareCol) float64 { return float64(len(c.sr.rec.Revives)) }, intf)
	anySaved := false
	for _, c := range b.cols {
		if len(c.sr.rec.Revives) > 0 {
			anySaved = true
		}
	}
	if anySaved {
		saves.best = make([]bool, len(b.cols)) // not a contest
		rows = append(rows, saves)
	}

	rows = append(rows, b.sectionRows()...)
	return rows
}

// sectionRows is the accuracy of each practice section per player, so you can
// see who struggled where.
func (b *BandCompare) sectionRows() []compareRow {
	// Sections come from the chart, so every column has the same list; use the
	// first analysed column's names and look the others up by position (or by
	// name when the lists differ).
	var names []string
	for _, c := range b.cols {
		if c.v == nil {
			continue
		}
		for _, s := range reportRows(c.v) {
			names = append(names, s.Name)
		}
		break
	}
	if len(names) == 0 {
		return nil
	}
	rows := []compareRow{{label: "Accuracy by section", head: true}}
	seen := map[string]int{}
	for _, name := range names {
		seen[name]++
		nth := seen[name]
		r := compareRow{label: name}
		vals := make([]float64, len(b.cols))
		valid := make([]bool, len(b.cols))
		r.accs = make([]float64, len(b.cols))
		for i, c := range b.cols {
			r.accs[i] = -1
			r.cells = append(r.cells, "-")
			if c.v == nil {
				continue
			}
			k := 0
			for _, s := range reportRows(c.v) {
				if s.Name != name {
					continue
				}
				if k++; k != nth {
					continue
				}
				if s.Judged() > 0 {
					acc := s.Accuracy()
					vals[i], valid[i], r.accs[i] = acc, true, acc
					r.cells[i] = fmt.Sprintf("%.0f%%", acc*100)
				}
				break
			}
		}
		r.best = pickBest(vals, valid, false)
		rows = append(rows, r)
	}
	return rows
}

func (b *BandCompare) Update(a *App) error {
	maxScroll := max(0, len(b.rows)-compareVisible())
	if w := a.Mouse.Wheel; w != 0 {
		if w > 0 {
			b.scroll = max(0, b.scroll-1)
		} else {
			b.scroll = min(maxScroll, b.scroll+1)
		}
	}
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp:
			b.scroll = max(0, b.scroll-1)
		case action.MenuDown:
			b.scroll = min(maxScroll, b.scroll+1)
		case action.MenuLeft:
			b.scroll = max(0, b.scroll-compareVisible())
		case action.MenuRight:
			b.scroll = min(maxScroll, b.scroll+compareVisible())
		case action.MenuConfirm, action.MenuBack:
			b.back(a)
			return nil
		}
	}
	if a.Opts.ShotScene == "compare" && a.frame > 20 {
		a.TakeScreenshotNext()
	}
	return nil
}

func (b *BandCompare) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	Header(dst, "Compare players", render.PlainText(b.song.Name)+"  ·  "+render.PlainText(b.song.Artist))

	n := max(1, len(b.cols))
	colW := math.Min(300, (render.ScreenW-2*96-compareLabelW)/float64(n))
	// Center the player columns in the room beside the labels.
	x0 := 96 + compareLabelW + ((render.ScreenW-2*96-compareLabelW)-colW*float64(n))/2
	gold := color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}

	// Player headers.
	for i, c := range b.cols {
		x := x0 + float64(i)*colW
		col := profileColor(c.sr.m.prof)
		render.FillRect(dst, x+8, 196, colW-16, 8, col)
		render.Text(dst, truncateW(c.sr.m.prof.Name, render.UIBold, 30, colW-24), render.UIBold, 30, x+colW/2, 214, th.Text, render.Center)
		part := chartTitle(c.sr.rec.Difficulty) + " " + chartTitle(c.sr.rec.Instrument)
		render.Text(dst, truncateW(part, render.Cond, 22, colW-24), render.Cond, 22, x+colW/2, 254, th.TextDim, render.Center)
		note := ""
		switch {
		case c.sr.left:
			note = "left partway"
		case !c.sr.rec.Completed:
			note = "failed"
		}
		if note != "" {
			render.Text(dst, note, render.Cond, 20, x+colW/2, 280, col, render.Center)
		}
	}

	vis := compareVisible()
	b.scroll = min(b.scroll, max(0, len(b.rows)-vis))
	panelH := float64(vis)*compareRowH + 16
	Panel(dst, 80, compareTop-12, render.ScreenW-160, panelH)
	for i := b.scroll; i < len(b.rows) && i < b.scroll+vis; i++ {
		r := b.rows[i]
		y := compareTop + float64(i-b.scroll)*compareRowH
		if i > b.scroll {
			line := render.WithAlpha(th.TextDim, 0.12)
			render.FillRect(dst, 100, y-6, render.ScreenW-200, 1, line)
		}
		if r.head {
			render.Text(dst, strings.ToUpper(r.label), render.CondBold, 22, 108, y+6, th.TextDim, render.Left)
			continue
		}
		lc := th.Text
		if r.dim {
			lc = th.TextDim
		}
		render.Text(dst, truncateW(r.label, render.UI, 26, compareLabelW-24), render.UI, 26, 108, y, lc, render.Left)
		for ci, cell := range r.cells {
			x := x0 + float64(ci)*colW + colW/2
			c, f := th.Text, render.Cond
			if r.dim {
				c = th.TextDim
			}
			if r.accs != nil && r.accs[ci] >= 0 {
				switch acc := r.accs[ci]; {
				case acc < 0.75:
					c = th.Bad
				case acc < 0.95:
					c = th.Lanes[2]
				default:
					c = th.Good
				}
			}
			if ci < len(r.best) && r.best[ci] {
				c, f = gold, render.CondBold
			}
			render.Text(dst, cell, f, 28, x, y+2, c, render.Center)
		}
	}
	if len(b.rows) > vis {
		render.Text(dst, fmt.Sprintf("rows %d-%d of %d   up/down to scroll", b.scroll+1, min(len(b.rows), b.scroll+vis), len(b.rows)),
			render.Cond, 22, render.ScreenW-100, compareBottom+22, th.TextDim, render.Right)
	}
	render.Text(dst, "Gold marks the best in each row. Timing is ranked by how steady the hits were.", render.Cond, 22, 100, compareBottom+22, th.TextDim, render.Left)
	Footer(dst, "Up / down to scroll     Enter / green or Esc / red back to the results")
}
