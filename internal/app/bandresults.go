package app

import (
	"fmt"
	"image/color"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/render"
)

// BandResults shows every player's score after a multiplayer song: the
// band's total, or (head to head) who won.
type BandResults struct {
	band      *Band
	song      *chart.Song
	c         *chart.Chart
	res       []*seatResult
	completed bool
	sel       int // a player card, then the buttons
	buttons   []string
}

// NewBandResults builds the results screen.
func NewBandResults(a *App, b *Band, song *chart.Song, c *chart.Chart, res []*seatResult, completed bool) *BandResults {
	r := &BandResults{band: b, song: song, c: c, res: append([]*seatResult(nil), res...), completed: completed}
	if b.Mode == modeH2H {
		sort.SliceStable(r.res, func(i, j int) bool { return r.res[i].rec.Result.Score > r.res[j].rec.Result.Score })
	}
	r.buttons = []string{"Pick another song", "Play it again", "Back to the lobby"}
	r.sel = len(r.res) // first button
	return r
}

func (r *BandResults) Update(a *App) error {
	n := len(r.res) + len(r.buttons)
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuLeft, action.MenuUp:
			r.sel = (r.sel - 1 + n) % n
		case action.MenuRight, action.MenuDown:
			r.sel = (r.sel + 1) % n
		case action.MenuOptions:
			a.SetScene(NewBandLoading(a, r.band, r.song))
			return nil
		case action.MenuBack:
			a.SetScene(NewSongSelect(a))
			return nil
		case action.MenuConfirm:
			if r.sel < len(r.res) {
				r.details(a, r.res[r.sel])
				return nil
			}
			switch r.sel - len(r.res) {
			case 0:
				a.SetScene(NewSongSelect(a))
			case 1:
				a.SetScene(NewBandLoading(a, r.band, r.song))
			case 2:
				a.SetScene(NewLobby(a))
			}
			return nil
		}
	}
	if a.Opts.ShotScene == "results" && a.frame > 20 {
		a.TakeScreenshotNext()
	}
	return nil
}

// details opens one player's full results (sections, misses, timing).
func (r *BandResults) details(a *App, sr *seatResult) {
	d := NewResults(a, sr.rec, sr.path, r.c, sr.t, nil)
	d.back = func(a *App) { a.SetScene(r) }
	a.SetScene(d)
}

func (r *BandResults) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	title := render.PlainText(r.song.Name)
	if !r.completed {
		title += "  (failed)"
		if r.band.Mode == modeH2H {
			title = render.PlainText(r.song.Name) + "  (everyone failed)"
		}
	}
	Header(dst, title, render.PlainText(r.song.Artist)+"  ·  "+bandModeLabel(r.band.Mode))

	// headline: band score or the winner
	total := int64(0)
	for _, sr := range r.res {
		total += sr.rec.Result.Score
	}
	head := ""
	switch {
	case r.band.Mode == modeH2H && len(r.res) > 1:
		head = r.res[0].m.prof.Name + " wins!"
		if r.res[0].rec.Result.Score == r.res[1].rec.Result.Score {
			head = "A tie!"
		}
	case len(r.res) > 1:
		head = fmt.Sprintf("Band score  %d", total)
	}
	if head != "" {
		render.TextShadow(dst, head, render.Display, 64, render.ScreenW-96, 60, th.Text, render.Right)
	}

	n := len(r.res)
	cols := min(4, max(1, n))
	cw, ch := 420.0, 300.0
	gap := 24.0
	x0 := (render.ScreenW - float64(cols)*cw - float64(cols-1)*gap) / 2
	y0 := 210.0
	if n <= 4 {
		y0 = 260
	}
	for i, sr := range r.res {
		row, col := i/cols, i%cols
		inRow := min(cols, n-row*cols)
		x := (render.ScreenW-float64(inRow)*cw-float64(inRow-1)*gap)/2 + float64(col)*(cw+gap)
		if inRow == cols {
			x = x0 + float64(col)*(cw+gap)
		}
		y := y0 + float64(row)*(ch+gap)
		r.drawCard(dst, sr, x, y, cw, ch, i, i == r.sel)
	}

	// buttons
	by := 880.0
	bx := float64(render.ScreenW) / 2
	widths := make([]float64, len(r.buttons))
	totalW := 0.0
	for i, b := range r.buttons {
		w, _ := render.Measure(b, render.UIBold, 30)
		widths[i] = w + 60
		totalW += widths[i] + 24
	}
	x := bx - (totalW-24)/2
	for i, b := range r.buttons {
		on := r.sel == len(r.res)+i
		c := render.WithAlpha(th.Panel, 0.9)
		if on {
			c = th.Accent
		}
		render.RoundRect(dst, x, by, widths[i], 64, 32, c)
		render.Text(dst, b, render.UIBold, 30, x+widths[i]/2, by+12, th.Text, render.Center)
		x += widths[i] + 24
	}
	Footer(dst, "Arrows / strum to choose     Enter / green open a player's details     Tab / yellow play again     Esc / red song list")
}

func (r *BandResults) drawCard(dst *ebiten.Image, sr *seatResult, x, y, w, h float64, i int, on bool) {
	col := profileColor(sr.m.prof)
	Panel(dst, x, y, w, h)
	if on {
		render.StrokeRect(dst, x-3, y-3, w+6, h+6, 4, th.Accent)
	}
	render.FillRect(dst, x, y, w, 8, col)
	res := sr.rec.Result
	nameX := x + 24
	if r.band.Mode == modeH2H && len(r.res) > 1 {
		c := th.TextDim
		if i == 0 {
			c = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
		}
		render.Text(dst, ordinal(i+1), render.Display, 44, x+24, y+20, c, render.Left)
		nameX = x + 110
	}
	render.Text(dst, truncateW(sr.m.prof.Name, render.UIBold, 34, x+w-nameX-20), render.UIBold, 34, nameX, y+26, th.Text, render.Left)
	part := chartTitle(sr.rec.Difficulty) + " " + chartTitle(sr.rec.Instrument)
	render.Text(dst, part, render.Cond, 24, x+24, y+80, th.TextDim, render.Left)
	render.Text(dst, fmt.Sprintf("%d", res.Score), render.Display, 56, x+24, y+112, th.Text, render.Left)
	var b render.Batch
	for s := 0; s < 5; s++ {
		c := render.WithAlpha(th.TextDim, 0.3)
		if s < res.Stars {
			c = color.RGBA{0xFF, 0xD0, 0x1F, 0xFF}
		}
		drawStar(&b, dst, x+w-150+float64(s)*28, y+140, 12, c)
	}
	b.Flush(dst)
	render.Text(dst, fmt.Sprintf("%.1f%% hit  ·  %d streak", res.HitRate()*100, res.MaxStreak), render.Cond, 26, x+24, y+196, th.Text, render.Left)
	note := ""
	switch {
	case sr.rec.Completed && res.NotesMissed == 0 && res.Overstrums == 0 && res.NotesHit == res.NotesTotal && res.NotesTotal > 0:
		note = "FULL COMBO"
	case len(sr.rec.Revives) > 0:
		note = fmt.Sprintf("Saved %s", plural(len(sr.rec.Revives), "time"))
	case sr.left:
		note = "Left partway"
	}
	if note != "" {
		render.Text(dst, note, render.CondBold, 24, x+24, y+240, col, render.Left)
	}
}
