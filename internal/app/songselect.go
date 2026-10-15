package app

import (
	"fmt"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/render"
)

type songInfo struct {
	err     string
	insts   []chart.Instrument
	diffs   map[chart.Instrument][]chart.Difficulty
	counts  map[chart.TrackKey]int
	length  float64
	drums   chart.DrumMode
	source  string
	tempo   float64
}

// SongSelect lists the library.
type SongSelect struct {
	all      []*chart.Song
	shown    []*chart.Song
	filter   string
	sel, top int
	info     map[string]*songInfo
	scanning bool

	picking bool
	pick    List
	inst    chart.Instrument
	diff    chart.Difficulty
}

// NewSongSelect builds the song list.
func NewSongSelect(a *App) *SongSelect {
	s := &SongSelect{info: map[string]*songInfo{}}
	s.refresh(a)
	if inst, ok := chart.ParseInstrument(a.Settings.LastInstrument); ok {
		s.inst = inst
	}
	s.diff = chart.Expert
	if d, ok := chart.ParseDifficulty(a.Settings.LastDifficulty); ok {
		s.diff = d
	}
	for i, song := range s.shown {
		if song.Dir == a.Settings.LastSong {
			s.sel = i
		}
	}
	return s
}

func (s *SongSelect) refresh(a *App) {
	songs, scanning := a.Library()
	s.all = songs
	s.scanning = scanning
	s.applyFilter()
}

func (s *SongSelect) applyFilter() {
	f := strings.ToLower(strings.TrimSpace(s.filter))
	s.shown = s.shown[:0]
	for _, song := range s.all {
		if f == "" || strings.Contains(strings.ToLower(song.Artist+" "+song.Name+" "+song.Charter+" "+song.Album), f) {
			s.shown = append(s.shown, song)
		}
	}
	if s.sel >= len(s.shown) {
		s.sel = len(s.shown) - 1
	}
	if s.sel < 0 {
		s.sel = 0
	}
}

func (s *SongSelect) current() *chart.Song {
	if s.sel >= 0 && s.sel < len(s.shown) {
		return s.shown[s.sel]
	}
	return nil
}

func (s *SongSelect) infoFor(song *chart.Song) *songInfo {
	if inf, ok := s.info[song.Dir]; ok {
		return inf
	}
	inf := &songInfo{diffs: map[chart.Instrument][]chart.Difficulty{}, counts: map[chart.TrackKey]int{}}
	c, err := song.LoadChart()
	if err != nil {
		inf.err = err.Error()
	} else {
		inf.insts = c.Instruments()
		for _, i := range inf.insts {
			inf.diffs[i] = c.Available(i)
		}
		for k, t := range c.Tracks {
			inf.counts[k] = len(t.Notes)
		}
		inf.length = c.LastTime()
		if t := c.Track(chart.Drums, chart.Expert); t != nil {
			inf.drums = t.DrumMode
		}
		inf.source = c.Source
		inf.tempo = c.Tempo.Tempos[0].BPM
	}
	s.info[song.Dir] = inf
	return inf
}

func (s *SongSelect) Update(a *App) error {
	if s.scanning {
		s.refresh(a)
	}
	if s.picking {
		s.pick.Update(a)
		if a.Pressed(action.MenuBack) {
			s.picking = false
		}
		return nil
	}
	// typing filters the list
	changed := false
	for _, r := range a.Chars {
		if r >= 32 && r != 127 {
			s.filter += string(r)
			changed = true
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(s.filter) > 0 {
		rs := []rune(s.filter)
		s.filter = string(rs[:len(rs)-1])
		changed = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF5) {
		a.RescanLibrary()
		s.scanning = true
	}
	if changed {
		s.sel = 0
		s.applyFilter()
	}
	n := len(s.shown)
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp:
			if n > 0 {
				s.sel = (s.sel - 1 + n) % n
			}
		case action.MenuDown:
			if n > 0 {
				s.sel = (s.sel + 1) % n
			}
		case action.MenuLeft:
			s.sel = max(0, s.sel-12)
		case action.MenuRight:
			s.sel = min(n-1, s.sel+12)
		case action.MenuBack:
			if s.filter != "" {
				s.filter = ""
				s.applyFilter()
			} else {
				a.SetScene(NewMainMenu(a))
				return nil
			}
		case action.MenuConfirm:
			if song := s.current(); song != nil {
				s.openPicker(a, song)
			}
		}
	}
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+12 {
		s.top = s.sel - 11
	}
	if a.Opts.ShotScene == "select" && a.frame > 40 {
		a.TakeScreenshotNext()
	}
	return nil
}

func (s *SongSelect) openPicker(a *App, song *chart.Song) {
	inf := s.infoFor(song)
	if inf.err != "" || len(inf.insts) == 0 {
		a.Toast("This chart has no playable parts")
		return
	}
	has := func(i chart.Instrument) bool {
		for _, x := range inf.insts {
			if x == i {
				return true
			}
		}
		return false
	}
	if !has(s.inst) {
		s.inst = inf.insts[0]
	}
	fixDiff := func() {
		ds := inf.diffs[s.inst]
		for _, d := range ds {
			if d == s.diff {
				return
			}
		}
		s.diff = ds[0]
	}
	fixDiff()
	s.pick = List{Visible: 4, Items: []Item{
		{Label: "Instrument", Value: func() string { return s.inst.Title() }, Adjust: func(d int) {
			idx := 0
			for i, x := range inf.insts {
				if x == s.inst {
					idx = i
				}
			}
			s.inst = inf.insts[(idx+d+len(inf.insts))%len(inf.insts)]
			fixDiff()
		}},
		{Label: "Difficulty", Value: func() string { return s.diff.Title() }, Adjust: func(d int) {
			ds := inf.diffs[s.inst]
			idx := 0
			for i, x := range ds {
				if x == s.diff {
					idx = i
				}
			}
			s.diff = ds[(idx-d+len(ds))%len(ds)] // list is hardest first
		}},
		{Label: "Play", Activate: func() {
			a.Settings.LastSong = song.Dir
			a.Settings.LastInstrument = s.inst.String()
			a.Settings.LastDifficulty = s.diff.String()
			_ = a.Settings.Save()
			a.SetScene(NewLoading(a, song, s.inst, s.diff))
		}},
		{Label: "Back", Activate: func() { s.picking = false }},
	}}
	s.pick.Sel = 2
	s.picking = true
}

func fmtDur(sec float64) string {
	if sec <= 0 {
		return "?"
	}
	m := int(sec) / 60
	return fmt.Sprintf("%d:%02d", m, int(sec)%60)
}

func (s *SongSelect) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	sub := plural(len(s.all), "song")
	if s.scanning {
		sub = fmt.Sprintf("Scanning... %d songs so far", len(s.all))
	}
	if s.filter != "" {
		sub = fmt.Sprintf("Filter: \"%s\"  (%d of %d)", s.filter, len(s.shown), len(s.all))
	}
	Header(dst, "Song select", sub)

	lx, ly, lw, rowH := 96.0, 230.0, 1000.0, 58.0
	Panel(dst, lx-24, ly-20, lw+48, rowH*12+40)
	if len(s.shown) == 0 {
		msg := "No songs found. Add your chart folders in Settings."
		if s.scanning {
			msg = "Scanning your library..."
		}
		render.Text(dst, msg, render.UI, 30, lx, ly+20, th.TextDim, render.Left)
	}
	for i := s.top; i < len(s.shown) && i < s.top+12; i++ {
		song := s.shown[i]
		y := ly + float64(i-s.top)*rowH
		sel := i == s.sel
		if sel {
			render.RoundRect(dst, lx-12, y-4, lw+24, rowH-6, 10, render.WithAlpha(th.Accent, 0.2))
			render.FillRect(dst, lx-12, y-4, 5, rowH-6, th.Accent)
		}
		c := render.Mix(th.Text, th.TextDim, 0.3)
		if sel {
			c = th.Text
		}
		render.Text(dst, truncateW(song.Name, render.UI, 30, 560), render.UI, 30, lx, y+4, c, render.Left)
		render.Text(dst, truncateW(song.Artist, render.Cond, 28, 380), render.Cond, 28, lx+lw, y+6, th.TextDim, render.Right)
	}

	// details
	song := s.current()
	dx, dy, dw := 1180.0, 230.0, 640.0
	Panel(dst, dx-24, dy-20, dw+48, 760)
	if song != nil {
		inf := s.infoFor(song)
		render.Text(dst, truncateW(song.Name, render.UIBold, 44, dw), render.UIBold, 44, dx, dy, th.Text, render.Left)
		render.Text(dst, truncateW(song.Artist, render.UI, 32, dw), render.UI, 32, dx, dy+60, th.TextDim, render.Left)
		y := dy + 120
		row := func(k, v string) {
			if v == "" {
				return
			}
			render.Text(dst, k, render.Cond, 26, dx, y, th.TextDim, render.Left)
			render.Text(dst, truncateW(v, render.UI, 26, dw-170), render.UI, 26, dx+170, y-2, th.Text, render.Left)
			y += 40
		}
		row("Album", song.Album)
		row("Year", song.Year)
		row("Charter", song.Charter)
		row("Length", fmtDur(inf.length))
		row("Format", inf.source)
		if inf.err != "" {
			render.Text(dst, "Chart error: "+truncateW(inf.err, render.Cond, 24, dw-40), render.Cond, 24, dx, y+10, th.Bad, render.Left)
		}
		y += 20
		for _, inst := range inf.insts {
			name := inst.Title()
			if inst == chart.Drums {
				name += " (" + inf.drums.String() + ")"
			}
			render.Text(dst, name, render.CondBold, 28, dx, y, th.Text, render.Left)
			x := dx + 250
			for d := chart.Easy; d <= chart.Expert; d++ {
				has := false
				for _, ad := range inf.diffs[inst] {
					if ad == d {
						has = true
					}
				}
				c := render.WithAlpha(th.TextDim, 0.35)
				if has {
					c = th.Accent
				}
				render.Text(dst, string("EMHX"[d]), render.CondBold, 28, x, y, c, render.Left)
				x += 36
			}
			if n := inf.counts[chart.TrackKey{Inst: inst, Diff: chart.Expert}]; n > 0 {
				render.Text(dst, fmt.Sprintf("%d notes", n), render.Cond, 24, dx+dw, y+2, th.TextDim, render.Right)
			}
			y += 42
		}
		stems := make([]string, 0, len(song.Stems))
		for k := range song.Stems {
			stems = append(stems, k)
		}
		if len(stems) > 0 {
			render.Text(dst, "Audio: "+strings.Join(sortStrings(stems), ", "), render.Cond, 22, dx, dy+700, th.TextDim, render.Left)
		}
	}
	if s.picking {
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0x90))
		Panel(dst, 560, 330, 800, 420)
		render.Text(dst, "Ready?", render.Display, 56, 600, 350, th.Text, render.Left)
		s.pick.Draw(dst, 620, 450, 680, 70)
	}
	Footer(dst, "Type to filter     Enter / green to choose     Esc / red back     F5 rescan")
}

func sortStrings(xs []string) []string {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
	return xs
}

// truncateW shortens s with an ellipsis to fit width w.
func truncateW(s string, f render.Font, size, w float64) string {
	if tw, _ := render.Measure(s, f, size); tw <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		if tw, _ := render.Measure(string(r)+"...", f, size); tw <= w {
			return string(r) + "..."
		}
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
