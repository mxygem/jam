package app

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/library"
	"github.com/mxygem/jam/internal/playlist"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
	"github.com/mxygem/jam/internal/rich"
)

// Song list layout.
const (
	listX, listY, listW, listBottom = 96.0, 236.0, 1000.0, 990.0
	songRowH, headRowH, subRowH     = 56.0, 46.0, 40.0
	infoX, infoY, infoW             = 1180.0, 236.0, 650.0
)

type overlay int

const (
	ovNone overlay = iota
	ovPicker
	ovOptions
	ovFilter
)

// SongSelect lists the library: sorted, grouped, filtered and searchable.
type SongSelect struct {
	b        *browser
	all      []*chart.Song
	rows     []row
	sel, top int // indexes into rows; sel is always a song row
	search   string
	scanning bool
	gen      int // library index generation the rows were built from
	built    float64

	inst chart.Instrument
	diff chart.Difficulty

	ov       overlay
	menu     List // picker / options
	filter   *filterEditor
	back     backdrop
	hover    int
	lastSel  *chart.Song
	want     string // song to select once the library has loaded
	shotWait int
}

// NewSongSelect builds the song list.
func NewSongSelect(a *App) *SongSelect {
	s := &SongSelect{b: newBrowser(), hover: -1}
	if inst, ok := chart.ParseInstrument(a.Settings.LastInstrument); ok {
		s.inst = inst
	}
	s.diff = chart.Expert
	if d, ok := chart.ParseDifficulty(a.Settings.LastDifficulty); ok {
		s.diff = d
	}
	s.want = a.Settings.LastSong
	s.refresh(a)
	return s
}

func (s *SongSelect) ctx(a *App) *browseCtx {
	return &browseCtx{index: a.Index, played: a.Scores.Played, inst: s.inst, scale: a.Settings.Browse.DiffScale, now: time.Now(), search: s.search}
}

// refresh re-reads the library and rebuilds the rows, keeping the selection.
func (s *SongSelect) refresh(a *App) {
	keep := s.current()
	songs, scanning := a.Library()
	s.all, s.scanning = songs, scanning
	_, _, _, s.gen = a.Index.Progress()
	s.rows = s.b.buildRows(s.all, a.Settings.Browse, s.ctx(a))
	s.built = clock.Now()
	switch {
	case s.want != "" && s.selectDir(s.want):
		s.want = ""
	case keep != nil:
		s.selectDir(keep.Dir)
	}
	s.fixSel(1)
}

func (s *SongSelect) selectDir(dir string) bool {
	for i, r := range s.rows {
		if r.song != nil && r.song.Dir == dir {
			s.sel = i
			s.scrollTo()
			return true
		}
	}
	return false
}

// fixSel moves the selection onto a song row (searching in direction d).
func (s *SongSelect) fixSel(d int) {
	if len(s.rows) == 0 {
		s.sel, s.top = 0, 0
		return
	}
	s.sel = max(0, min(len(s.rows)-1, s.sel))
	for i := 0; i < len(s.rows); i++ {
		if s.rows[s.sel].song != nil {
			break
		}
		s.sel += d
		if s.sel < 0 || s.sel >= len(s.rows) {
			d = -d
			s.sel = max(0, min(len(s.rows)-1, s.sel))
		}
	}
	s.scrollTo()
}

func (s *SongSelect) current() *chart.Song {
	if s.sel >= 0 && s.sel < len(s.rows) {
		return s.rows[s.sel].song
	}
	return nil
}

func rowH(r row) float64 {
	switch {
	case r.header != "" && r.sub:
		return subRowH
	case r.header != "":
		return headRowH
	}
	return songRowH
}

// visibleFrom returns how many rows fit from row top.
func (s *SongSelect) visibleFrom(top int) int {
	y, n := listY, 0
	for i := top; i < len(s.rows); i++ {
		y += rowH(s.rows[i])
		if y > listBottom {
			break
		}
		n++
	}
	return max(1, n)
}

// scrollTo keeps the selection on screen with its group header if it fits.
func (s *SongSelect) scrollTo() {
	if s.sel < s.top {
		s.top = s.sel
		// show the headers right above the selection
		for s.top > 0 && s.rows[s.top-1].header != "" {
			s.top--
		}
	}
	for s.sel >= s.top+s.visibleFrom(s.top) {
		s.top++
	}
}

func (s *SongSelect) move(d int) {
	for i := s.sel + d; i >= 0 && i < len(s.rows); i += d {
		if s.rows[i].song != nil {
			s.sel = i
			s.scrollTo()
			return
		}
	}
}

// jumpGroup moves to the first song of the next/previous group.
func (s *SongSelect) jumpGroup(d int) {
	i := s.sel
	if d < 0 {
		// back to this group's start first, then the previous group's
		for i > 0 && s.rows[i-1].header == "" {
			i--
		}
		if i == s.sel {
			i--
			for i > 0 && s.rows[i].header != "" {
				i--
			}
			for i > 0 && s.rows[i-1].header == "" {
				i--
			}
		}
	} else {
		for i < len(s.rows) && s.rows[i].header == "" {
			i++
		}
		for i < len(s.rows) && s.rows[i].header != "" {
			i++
		}
	}
	if i >= 0 && i < len(s.rows) && s.rows[i].song != nil {
		s.sel = i
		s.top = i
		for s.top > 0 && s.rows[s.top-1].header != "" {
			s.top--
		}
		s.scrollTo()
	}
}

func (s *SongSelect) Update(a *App) error {
	// library and index progress
	if s.scanning {
		if _, scanning := a.Library(); !scanning {
			s.refresh(a)
		}
	}
	if _, _, running, gen := a.Index.Progress(); gen != s.gen && clock.Now()-s.built > 0.6 {
		if s.needsIndex(a) || !running {
			s.refresh(a)
		} else {
			s.gen = gen
		}
	}
	if song := s.current(); song != s.lastSel && (s.want == "" || !s.scanning) {
		s.lastSel = song
		s.want = ""
		if song != nil {
			a.Settings.LastSong = song.Dir
		}
	}

	switch s.ov {
	case ovPicker, ovOptions:
		if s.menu.Update(a) {
			return nil
		}
		if a.Pressed(action.MenuBack) || a.Mouse.Right {
			s.ov = ovNone
		}
		return nil
	case ovFilter:
		if s.filter.update(a) {
			s.ov = ovNone
			_ = a.Settings.Save()
		}
		if s.filter.changed {
			s.filter.changed = false
			s.refresh(a)
		}
		return nil
	}

	// typing searches
	changed := false
	for _, r := range a.Chars {
		if r >= 32 && r != 127 {
			s.search += string(r)
			changed = true
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && s.search != "" {
		rs := []rune(s.search)
		s.search = string(rs[:len(rs)-1])
		changed = true
	}
	if changed {
		s.refresh(a)
		s.sel = 0
		s.top = 0
		s.fixSel(1)
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyF2):
		s.cycleSort(a, 1)
	case inpututil.IsKeyJustPressed(ebiten.KeyF3):
		s.openFilter(a)
		return nil
	case inpututil.IsKeyJustPressed(ebiten.KeyF4):
		s.cycleScale(a, 1)
	case inpututil.IsKeyJustPressed(ebiten.KeyF5):
		a.RescanLibrary()
		s.scanning = true
	case inpututil.IsKeyJustPressed(ebiten.KeyF6):
		a.SetScene(NewPlaylistScene(a))
		return nil
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		s.sel = 0
		s.top = 0
		s.fixSel(1)
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		s.sel = len(s.rows) - 1
		s.fixSel(-1)
	}
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuUp:
			s.move(-1)
		case action.MenuDown:
			s.move(1)
		case action.MenuLeft:
			s.jumpGroup(-1)
		case action.MenuRight:
			s.jumpGroup(1)
		case action.MenuOptions:
			s.openOptions(a)
		case action.MenuBack:
			if s.search != "" {
				s.search = ""
				s.refresh(a)
			} else {
				_ = a.Settings.Save()
				a.SetScene(NewMainMenu(a))
				return nil
			}
		case action.MenuConfirm:
			if song := s.current(); song != nil {
				s.openPicker(a, song)
			}
		}
	}
	s.updateMouse(a)
	s.prefetchArt(a)
	if song := s.current(); song != nil {
		s.back.set(a.Art.get(song.CoverArt))
	} else {
		s.back.set(nil)
	}
	if a.Opts.ShotScene == "select" && a.frame > 40 {
		if song := s.current(); song == nil || !a.Art.pending(song.CoverArt) || a.frame > 600 {
			s.shotWait++
			if s.shotWait > 30 { // let the backdrop fade in
				a.TakeScreenshotNext()
			}
		}
	}
	return nil
}

// prefetchArt starts loading art for the selection and its neighbours so
// scrolling doesn't flash empty panels.
func (s *SongSelect) prefetchArt(a *App) {
	for _, d := range []int{0, 1, -1, 2, -2} {
		i := s.sel + d
		if i >= 0 && i < len(s.rows) && s.rows[i].song != nil {
			a.Art.get(s.rows[i].song.CoverArt)
		}
	}
}

// needsIndex reports whether the current sort/filter depends on index data.
func (s *SongSelect) needsIndex(a *App) bool {
	b := a.Settings.Browse
	if b.Filter.Enabled && len(b.Filter.Instruments) > 0 {
		return true
	}
	switch b.Sort {
	case "difficulty", "length":
		return true
	}
	return false
}

func (s *SongSelect) updateMouse(a *App) {
	m := &a.Mouse
	s.hover = -1
	if !m.In(listX-24, listY-10, listW+48, listBottom-listY+10) {
		return
	}
	if m.Wheel != 0 {
		steps := 3
		if m.Wheel > 0 {
			steps = -3
		}
		s.top = max(0, min(len(s.rows)-1, s.top+steps))
		if steps > 0 && s.top+s.visibleFrom(s.top) > len(s.rows) {
			s.top = max(0, len(s.rows)-s.visibleFrom(max(0, len(s.rows)-20)))
		}
	}
	y := listY
	for i := s.top; i < len(s.rows); i++ {
		h := rowH(s.rows[i])
		if y+h > listBottom {
			break
		}
		if m.Y >= y-4 && m.Y < y+h-4 && s.rows[i].song != nil {
			s.hover = i
			if m.Click {
				if i == s.sel || m.Double {
					s.sel = i
					s.openPicker(a, s.rows[i].song)
				} else {
					s.sel = i
				}
			}
		}
		y += h
	}
}

func (s *SongSelect) cycleSort(a *App, d int) {
	b := &a.Settings.Browse
	idx := 0
	for i, k := range sortKeys {
		if k.id == b.Sort {
			idx = i
		}
	}
	b.Sort = sortKeys[(idx+d+len(sortKeys))%len(sortKeys)].id
	s.refresh(a)
	a.Toast("Sorted by " + strings.ToLower(sortLabel(b.Sort)))
}

func (s *SongSelect) cycleScale(a *App, d int) {
	b := &a.Settings.Browse
	idx := 0
	for i, k := range diffScales {
		if k.id == b.DiffScale {
			idx = i
		}
	}
	b.DiffScale = diffScales[(idx+d+len(diffScales))%len(diffScales)].id
	if b.Sort == "difficulty" {
		s.refresh(a)
	}
	a.Toast("Difficulty: " + scaleLabel(b.DiffScale))
}

func (s *SongSelect) openFilter(a *App) {
	s.filter = newFilterEditor(a)
	s.ov = ovFilter
}

func (s *SongSelect) openOptions(a *App) {
	b := &a.Settings.Browse
	order := func() string {
		if b.Desc {
			return "Reversed"
		}
		return "Normal"
	}
	items := []Item{
		{Label: "Sort by", Value: func() string { return sortLabel(b.Sort) }, Adjust: func(d int) { s.cycleSort(a, d) }},
		{Label: "Order", Value: order, Adjust: func(int) { b.Desc = !b.Desc; s.refresh(a) }},
		{Label: "Filters", Value: func() string { return filterSummary(b.Filter) }, Activate: func() { s.openFilter(a) }},
		{Label: "Difficulty scale", Value: func() string { return scaleLabel(b.DiffScale) }, Adjust: func(d int) { s.cycleScale(a, d) }},
		{Label: "Rating part", Hint: "Which part the list's difficulty badges and difficulty sort use",
			Value:  func() string { return s.inst.Title() },
			Adjust: func(d int) { s.inst = cycleInst(s.inst, d); s.refresh(a) }},
	}
	if song := s.current(); song != nil {
		items = append(items, Item{Label: "Add to playlist", Hint: "Adds the selected song with the part shown in the picker",
			Activate: func() { s.addToQueue(a, song, s.inst, s.diff); s.ov = ovNone }})
	}
	items = append(items,
		Item{Label: "Playlist", Value: func() string { return queueLabel(a) }, Activate: func() { a.SetScene(NewPlaylistScene(a)) }},
		Item{Label: "Rescan library", Activate: func() { a.RescanLibrary(); s.scanning = true; s.ov = ovNone }},
		Item{Label: "Close", Activate: func() { s.ov = ovNone; _ = a.Settings.Save() }},
	)
	s.menu = List{Visible: 9, Items: items}
	s.ov = ovOptions
}

func queueLabel(a *App) string {
	n := len(a.Queue.Entries)
	name := a.Queue.Name
	if name == "" {
		name = "unsaved"
	}
	return fmt.Sprintf("%s, %s", name, plural(n, "song"))
}

func cycleInst(cur chart.Instrument, d int) chart.Instrument {
	all := chart.Instruments()
	idx := 0
	for i, x := range all {
		if x == cur {
			idx = i
		}
	}
	return all[(idx+d+len(all))%len(all)]
}

func filterSummary(f config.Filter) string {
	if !f.Enabled {
		return "Off"
	}
	n := len(f.Instruments)
	for _, ff := range f.Fields {
		if ff.Text != "" {
			n++
		}
	}
	if n == 0 {
		return "On (no rules)"
	}
	return fmt.Sprintf("On, %s", plural(n, "rule"))
}

func (s *SongSelect) addToQueue(a *App, song *chart.Song, inst chart.Instrument, diff chart.Difficulty) {
	a.Queue.Entries = append(a.Queue.Entries, playlist.Entry{
		Dir: song.Dir, Title: plainTitle(song), Instrument: inst.String(), Difficulty: diff.String(),
	})
	a.Toast(fmt.Sprintf("Added to playlist (%s)", plural(len(a.Queue.Entries), "song")))
}

func plainTitle(song *chart.Song) string {
	t := rich.Plain(song.Name)
	if ar := rich.Plain(song.Artist); ar != "" {
		t = ar + " - " + t
	}
	return t
}

func (s *SongSelect) openPicker(a *App, song *chart.Song) {
	in := a.SongInfo(song)
	if in == nil || in.Err != "" || len(in.Parts) == 0 {
		a.Toast("This chart has no playable parts")
		return
	}
	if in.Part(s.inst) == nil {
		s.inst = in.Parts[0].Inst
	}
	fixDiff := func() {
		ds := in.Part(s.inst).Diffs
		for _, d := range ds {
			if d == s.diff {
				return
			}
		}
		s.diff = ds[0]
	}
	fixDiff()
	s.menu = List{Visible: 5, Items: []Item{
		{Label: "Instrument", Value: func() string { return s.inst.Title() }, Adjust: func(d int) {
			idx := 0
			for i, p := range in.Parts {
				if p.Inst == s.inst {
					idx = i
				}
			}
			s.inst = in.Parts[(idx+d+len(in.Parts))%len(in.Parts)].Inst
			fixDiff()
		}},
		{Label: "Difficulty", Value: func() string { return s.diff.Title() }, Adjust: func(d int) {
			ds := in.Part(s.inst).Diffs
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
			a.Run = nil
			a.SetScene(NewLoading(a, song, s.inst, s.diff))
		}},
		{Label: "Add to playlist", Activate: func() { s.addToQueue(a, song, s.inst, s.diff); s.ov = ovNone }},
		{Label: "Back", Activate: func() { s.ov = ovNone }},
	}}
	s.menu.Sel = 2
	s.ov = ovPicker
}

// --- drawing ---

func (s *SongSelect) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	song := s.current()
	var art *artEntry
	if song != nil {
		art = a.Art.get(song.CoverArt)
	}
	s.back.draw(dst)

	Header(dst, "Song select", s.subtitle(a))
	s.drawList(dst, a)
	s.drawInfo(dst, a, song, art)

	switch s.ov {
	case ovPicker:
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xB8))
		Panel(dst, 560, 300, 800, 500)
		render.RichText(dst, song.Name, render.Display, 52, 600, 320, th.Text, render.Left, 720)
		s.menu.Draw(dst, 620, 420, 680, 70)
		if b := a.Scores.Best(song.Dir, s.inst.String(), s.diff.String()); b != nil {
			render.Text(dst, bestLine(b), render.Cond, 26, 600, 770, th.TextDim, render.Left)
		}
	case ovOptions:
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xB8))
		Panel(dst, 470, 180, 980, 760)
		render.Text(dst, "Song list options", render.Display, 52, 510, 200, th.Text, render.Left)
		s.menu.Draw(dst, 530, 300, 860, 68)
		render.Text(dst, s.menu.Hint(), render.UI, 24, 510, 900, th.TextDim, render.Left)
	case ovFilter:
		s.filter.draw(dst, a)
	}
	Footer(dst, "Type to search   Enter / green choose   Tab / yellow options   F2 sort   F3 filter   F4 difficulty scale   F6 playlist   Esc / red back")
}

func (s *SongSelect) subtitle(a *App) string {
	b := a.Settings.Browse
	parts := []string{}
	shown := 0
	for _, r := range s.rows {
		if r.song != nil {
			shown++
		}
	}
	if s.scanning {
		parts = append(parts, fmt.Sprintf("Scanning... %d songs so far", len(s.all)))
	} else if shown != len(s.all) {
		parts = append(parts, fmt.Sprintf("%d of %s", shown, plural(len(s.all), "song")))
	} else {
		parts = append(parts, plural(len(s.all), "song"))
	}
	dir := ""
	if b.Desc {
		dir = ", reversed"
	}
	parts = append(parts, "sorted by "+strings.ToLower(sortLabel(b.Sort))+dir)
	if b.Filter.Active() {
		parts = append(parts, "filter "+strings.ToLower(filterSummary(b.Filter)))
	}
	if s.search != "" {
		parts = append(parts, fmt.Sprintf("search \"%s\"", s.search))
	}
	if done, total, running, _ := a.Index.Progress(); running {
		parts = append(parts, fmt.Sprintf("indexing %d/%d", done, total))
	}
	return strings.Join(parts, "  ·  ")
}

func (s *SongSelect) drawList(dst *ebiten.Image, a *App) {
	GlassPanel(dst, listX-24, listY-20, listW+48, listBottom-listY+30)
	if len(s.rows) == 0 {
		msg := "No songs match. Press F3 for filters or Esc to clear the search."
		if s.scanning {
			msg = "Scanning your library..."
		} else if len(s.all) == 0 {
			msg = "No songs found. Add your chart folders in Settings."
		}
		render.Text(dst, msg, render.UI, 28, listX, listY+20, th.TextDim, render.Left)
		return
	}
	scale := a.Settings.Browse.DiffScale
	y := listY
	for i := s.top; i < len(s.rows); i++ {
		r := s.rows[i]
		h := rowH(r)
		if y+h > listBottom {
			break
		}
		switch {
		case r.header != "" && r.sub:
			render.RichText(dst, r.header, render.CondBold, 24, listX+18, y+6, render.Mix(th.Text, th.TextDim, 0.4), render.Left, listW-40)
		case r.header != "":
			render.RichText(dst, strings.ToUpper(r.header), render.CondBold, 26, listX, y+8, th.Accent, render.Left, listW-20)
			w, _ := render.MeasureRich(strings.ToUpper(r.header), render.CondBold, 26)
			if w < listW-40 {
				render.FillRect(dst, listX+min(w, listW-60)+16, y+24, listW-min(w, listW-60)-16, 2, render.WithAlpha(th.Accent, 0.25))
			}
		default:
			sel := i == s.sel
			if sel {
				render.RoundRect(dst, listX-12, y-4, listW+24, h-6, 10, render.WithAlpha(th.Accent, 0.22))
				render.FillRect(dst, listX-12, y-4, 5, h-6, th.Accent)
			} else if i == s.hover {
				render.RoundRect(dst, listX-12, y-4, listW+24, h-6, 10, render.WithAlpha(th.Text, 0.06))
			}
			c := render.Mix(th.Text, th.TextDim, 0.3)
			if sel {
				c = th.Text
			}
			in := a.Index.Get(r.song)
			badge := listBadge(in, r.song, s.inst, scale)
			bw := 0.0
			if badge != "" {
				bw = 70
				render.Text(dst, badge, render.CondBold, 24, listX+listW, y+8, render.WithAlpha(th.Accent, 0.9), render.Right)
			}
			render.RichText(dst, r.song.Name, render.UI, 29, listX, y+4, c, render.Left, 560)
			render.RichText(dst, rowDetail(r.song, a.Settings.Browse.Sort), render.Cond, 26, listX+listW-bw, y+7, th.TextDim, render.Right, 360-bw)
		}
		y += h
	}
	// scroll position
	if n := len(s.rows); n > s.visibleFrom(0) {
		frac := float64(s.top) / float64(n)
		vis := float64(s.visibleFrom(s.top)) / float64(n)
		total := listBottom - listY
		render.FillRect(dst, listX+listW+16, listY+total*frac, 4, math.Max(24, total*vis), render.WithAlpha(th.TextDim, 0.5))
	}
}

// rowDetail is the dim text at the right of a list row: whatever the group
// header doesn't already say.
func rowDetail(s *chart.Song, sort string) string {
	switch sort {
	case "artist":
		if s.Album != "" {
			return s.Album
		}
		return s.Year
	case "artist_album", "album":
		if sort == "album" {
			return s.Artist
		}
		return s.Year
	}
	return s.Artist
}

// listBadge is the short difficulty shown on each list row.
func listBadge(in *library.Info, s *chart.Song, inst chart.Instrument, scale string) string {
	v, ok := ratingOf(in, s, inst, scale)
	if !ok {
		return ""
	}
	switch scale {
	case "charter":
		return fmt.Sprintf("T%d", int(v))
	case "fretwork":
		return fmt.Sprintf("FW %d", library.RemapFor(v, inst))
	case "fwtier":
		return fmt.Sprintf("FT %d", library.TierFor(v, inst))
	case "nps":
		return fmt.Sprintf("%.1f/s", v)
	}
	return ""
}

func bestLine(b *replay.Best) string {
	line := fmt.Sprintf("Best: %s  %s  %.1f%%  (%s)", strings.Repeat("★", b.Stars)+strings.Repeat("☆", max(0, 5-b.Stars)),
		commas(b.Score), b.HitRate*100, plural(b.Plays, "play"))
	if b.FullCombo {
		line += "  FC"
	}
	return line
}

func commas(n int64) string {
	s := fmt.Sprint(n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func (s *SongSelect) drawInfo(dst *ebiten.Image, a *App, song *chart.Song, art *artEntry) {
	GlassPanel(dst, infoX-24, infoY-20, infoW+48, listBottom-infoY+30)
	if song == nil {
		return
	}
	in := a.Index.Get(song)
	artSize := 0.0
	if art != nil {
		artSize = 220
		render.FillRect(dst, infoX-2, infoY-2, artSize+4, artSize+4, render.RGBA(0, 0, 0, 0x80))
		drawFit(dst, art.sharp, infoX, infoY, artSize, artSize)
		artSize += 24
	}
	tx, tw := infoX+artSize, infoW-artSize
	render.RichText(dst, song.Name, render.UIBold, 40, tx, infoY-4, th.Text, render.Left, tw)
	render.RichText(dst, song.Artist, render.UI, 30, tx, infoY+52, th.TextDim, render.Left, tw)
	y := infoY + 104
	line := func(k, v string) {
		if strings.TrimSpace(rich.Plain(v)) == "" || y > infoY+220 && art != nil && tx != infoX {
			return
		}
		render.Text(dst, k, render.Cond, 24, tx, y, th.TextDim, render.Left)
		render.RichText(dst, v, render.UI, 24, tx+110, y-1, th.Text, render.Left, tw-110)
		y += 34
	}
	album := song.Album
	if song.Year != "" {
		album = strings.TrimSpace(album + "  (" + song.Year + ")")
	}
	line("Album", album)
	line("Charter", song.Charter)
	line("Genre", song.Genre)
	y = max(y, infoY+artSize+6)
	if art != nil {
		tx, tw = infoX, infoW
	}
	length := ""
	if ms := lengthOf(in, song); ms > 0 {
		length = fmtDur(float64(ms) / 1000)
	}
	facts := []string{}
	if length != "" {
		facts = append(facts, length)
	}
	if song.Icon != "" {
		facts = append(facts, "source "+rich.Plain(song.Icon))
	}
	if in != nil && in.Format != "" {
		facts = append(facts, "format "+in.Format)
	}
	if in != nil && in.Lyrics {
		facts = append(facts, "lyrics")
	}
	render.Text(dst, truncateW(strings.Join(facts, "   ·   "), render.Cond, 24, infoW), render.Cond, 24, infoX, y, th.TextDim, render.Left)
	y += 46

	// parts with difficulty and best scores
	scale := a.Settings.Browse.DiffScale
	render.Text(dst, "PARTS  ·  "+strings.ToUpper(scaleLabel(scale))+"  (F4 to change)", render.CondBold, 20, infoX, y, render.WithAlpha(th.TextDim, 0.8), render.Left)
	y += 32
	if in == nil {
		render.Text(dst, "Reading chart...", render.UI, 26, infoX, y, th.TextDim, render.Left)
		return
	}
	if in.Err != "" {
		render.Text(dst, "Chart error: "+truncateW(in.Err, render.Cond, 24, infoW-160), render.Cond, 24, infoX, y, th.Bad, render.Left)
		return
	}
	for _, p := range in.Parts {
		if y > listBottom-60 {
			break
		}
		name := p.Inst.Title()
		if p.Mode != "" {
			name += " (" + p.Mode + ")"
		}
		c := th.Text
		if p.Inst == s.inst {
			c = th.Accent
		}
		render.Text(dst, name, render.CondBold, 27, infoX, y, c, render.Left)
		x := infoX + 230
		for d := chart.Easy; d <= chart.Expert; d++ {
			has := false
			for _, ad := range p.Diffs {
				has = has || ad == d
			}
			dc := render.WithAlpha(th.TextDim, 0.3)
			if has {
				dc = th.Text
			}
			render.Text(dst, string("EMHX"[d]), render.CondBold, 26, x, y, dc, render.Left)
			x += 28
		}
		drawRating(dst, infoX+360, y+2, in, song, p, scale)
		y += 36
		// best score on the difficulty the picker would use
		diff := s.diff
		if !hasDiff(p.Diffs, diff) {
			diff = p.Diffs[0]
		}
		if b := a.Scores.Best(song.Dir, p.Inst.String(), diff.String()); b != nil {
			render.Text(dst, diff.Title()+"  "+bestLine(b), render.Cond, 22, infoX+18, y-4, th.TextDim, render.Left)
			y += 30
		}
		y += 6
	}
	if in.Lyrics && y < listBottom-50 {
		render.Text(dst, "Vocals", render.CondBold, 27, infoX, y, th.Text, render.Left)
		render.Text(dst, "lyrics shown while playing", render.Cond, 24, infoX+230, y+2, th.TextDim, render.Left)
	}
	render.Text(dst, "Folder: "+truncateW(folderOf(song), render.Cond, 22, infoW-80), render.Cond, 22, infoX, listBottom-30, render.WithAlpha(th.TextDim, 0.8), render.Left)
}

func hasDiff(ds []chart.Difficulty, d chart.Difficulty) bool {
	for _, x := range ds {
		if x == d {
			return true
		}
	}
	return false
}

// drawRating shows one part's difficulty on the chosen scale: tier dots for
// 0-6 scales, numbers otherwise.
func drawRating(dst *ebiten.Image, x, y float64, in *library.Info, s *chart.Song, p library.Part, scale string) {
	v, ok := ratingOf(in, s, p.Inst, scale)
	if !ok {
		render.Text(dst, "not rated", render.Cond, 22, x, y, render.WithAlpha(th.TextDim, 0.6), render.Left)
		return
	}
	dots := func(n int, label string) {
		var b render.Batch
		for i := 0; i < 6; i++ {
			c := render.RGBA(0x44, 0x49, 0x60, 0xFF)
			if i < n {
				c = th.Accent
			}
			if n > 6 { // devil tier
				c = th.Bad
			}
			b.Ellipse(dst, x+10+float64(i)*24, y+13, 8, 8, c, c)
		}
		b.Flush(dst)
		render.Text(dst, label, render.Cond, 22, x+160, y, th.TextDim, render.Left)
	}
	switch scale {
	case "charter":
		dots(int(v), fmt.Sprintf("tier %d", int(v)))
	case "fretwork":
		dots(library.RemapFor(v, p.Inst), fmt.Sprintf("FW %d  (D %.1f)", library.RemapFor(v, p.Inst), v))
	case "fwtier":
		render.Text(dst, fmt.Sprintf("tier %d", library.TierFor(v, p.Inst)), render.CondBold, 24, x, y, th.Accent, render.Left)
		render.Text(dst, fmt.Sprintf("D %.1f", v), render.Cond, 22, x+90, y+1, th.TextDim, render.Left)
	case "nps":
		render.Text(dst, fmt.Sprintf("%.1f notes/s", p.AvgNPS), render.CondBold, 24, x, y, th.Accent, render.Left)
		render.Text(dst, fmt.Sprintf("peak %.0f   %d notes", p.PeakNPS, p.Notes), render.Cond, 22, x+150, y+1, th.TextDim, render.Left)
	}
}
