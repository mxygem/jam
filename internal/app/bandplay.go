package app

import (
	"fmt"
	"log"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// BandPlay is a song played by several players at once, each on their own
// highway. Band, head to head and party mode all play here.
type BandPlay struct {
	band  *Band
	song  *chart.Song
	c     *chart.Chart
	au    *playback.Song
	seats []*seat
	party bool
	bots  bool // every seat played by the bot (screenshots and tests)

	startClock, prevClock float64
	started               bool
	biasSet               bool
	biasBuf               []float64
	bias                  float64
	audioOff, videoOff    float64 // the shared screen: lyrics, sections, progress

	lyr      *render.Lyrics
	section  int
	secName  string
	secAt    float64
	length   float64
	lastNote float64

	paused     bool
	pauseClock float64
	pause      List

	finished bool
	failed   bool
	joins    joinPresses
	endAt    float64 // party: when the song ended (the scores show briefly)
	next     *partyLoad

	vote         *skipVote // party: a vote to skip the song
	voteFailedAt float64   // song time the last vote failed
	results      []*seatResult
}

// NewBandPlay starts a song for the band.
func NewBandPlay(a *App, b *Band, s *chart.Song, c *chart.Chart, au *playback.Song) *BandPlay {
	set := a.Settings
	p := &BandPlay{band: b, song: s, c: c, au: au, party: b.Mode == modeParty, section: -1, bots: a.Opts.Autoplay}
	p.audioOff = set.AudioOffsetMs / 1000
	p.videoOff = set.VideoOffsetMs / 1000
	for _, m := range b.Members {
		p.seats = append(p.seats, newSeat(a, p, m, 0))
	}
	if set.ShowLyrics {
		opts := lyricOpts(set)
		p.lyr = render.NewLyrics(c.Vocals, 52, opts)
		if p.lyr != nil && c.Tempo != nil {
			tm := c.Tempo
			p.lyr.Beat = func(now float64) float64 {
				b := tm.Ticks(now) / float64(tm.Resolution)
				return b - math.Floor(b)
			}
		}
	}
	p.lastNote = c.LastTime()
	p.length = p.lastNote + 2.5
	if d := au.Duration(); d > 0 && d < p.length {
		p.length = d
	}
	au.SetMaster(set.Volume)
	lead := set.LeadIn
	if lead <= 0 {
		lead = 2.5
	}
	now := clock.Now()
	p.startClock = now + lead
	p.prevClock = now
	if set.Background == bgRandom {
		a.BG.Apply(bgRandom, true)
	}
	log.Printf("band: %s, %s, %d players", s.Title(), bandModeLabel(b.Mode), len(p.seats))
	return p
}

func (p *BandPlay) songNow(clk float64) float64 { return clk - p.startClock }

// renderNow is song time for the shared screen.
func (p *BandPlay) renderNow(now float64) float64 { return p.songNow(now) - p.audioOff + p.videoOff }

// syncToAudio locks the song clock to the audio device (as in Play).
func (p *BandPlay) syncToAudio(now float64) {
	if !p.au.Playing() {
		return
	}
	pos := p.au.Position()
	diff := pos - (now - p.startClock)
	if !p.biasSet {
		if pos > 0.1 {
			p.biasBuf = append(p.biasBuf, diff)
		}
		if len(p.biasBuf) >= 45 {
			s := append([]float64(nil), p.biasBuf...)
			sort.Float64s(s)
			p.bias = s[len(s)/2]
			p.biasSet = true
		}
		return
	}
	e := diff - p.bias
	switch {
	case math.Abs(e) > 0.25:
		p.startClock -= e
	case math.Abs(e) > 0.002:
		p.startClock -= e * 0.02
	}
}

func (p *BandPlay) Update(a *App) error {
	now := clock.Now()
	dt := now - p.prevClock
	p.prevClock = now
	if p.endAt > 0 {
		return p.updateBetween(a, now)
	}
	if p.paused {
		return p.updatePause(a, now)
	}
	if !p.started && p.songNow(now) >= 0 {
		p.au.Play()
		p.started = true
		p.startClock = now
	}
	if p.started {
		p.syncToAudio(now)
	}
	p.au.Update(dt)

	byDev := splitInput(a.Raw)
	pauseReq := false
	for _, ev := range a.Raw {
		if ev.Src == input.SrcKeyboard && ebiten.Key(ev.Code) == ebiten.KeyEscape && ev.Down {
			pauseReq = true
		}
	}
	for _, s := range p.seats {
		// The player's menu input sees all of their controller's events,
		// menu open or not, so it never misses a release.
		acts := memberActs(s.m, byDev)
		inMenu := s.joining != nil || s.menu != nil
		if s.mapper != nil && (inMenu || s.bot != nil) {
			for _, ev := range byDev[s.m.dev] {
				s.mapped = s.mapper.Map(ev, s.mapped[:0]) // keep held buttons in step
			}
		}
		if inMenu {
			p.updateSeatMenu(a, s, acts)
			continue
		}
		if s.bot != nil {
			continue
		}
		if s.mapper == nil { // sitting out: Start / green opens their menu
			if p.party && hasAct(acts, action.MenuConfirm) {
				p.startPressed(a, s)
			}
			continue
		}
		for _, ev := range byDev[s.m.dev] {
			if ev.Src == input.SrcKeyboard && ebiten.Key(ev.Code) == ebiten.KeyEscape {
				continue
			}
			s.mapped = s.mapper.Map(ev, s.mapped[:0])
			for _, mp := range s.mapped {
				if mp.Action == action.Start && mp.Down {
					if p.party {
						p.startPressed(a, s)
					} else {
						pauseReq = true
					}
					continue
				}
				if s.out || s.failed || s.menu != nil {
					continue
				}
				s.feed(mp.Event, p.songNow(mp.Time)-s.audioOff, mp.Src)
			}
		}
	}
	if p.party {
		p.updateJoins(a, byDev)
	}
	// bots
	for _, s := range p.seats {
		if s.bot == nil || !s.playing() {
			continue
		}
		judgeNow := p.songNow(now) - s.audioOff
		for s.botIdx < len(s.bot) && s.bot[s.botIdx].Time <= judgeNow {
			ev := s.bot[s.botIdx]
			s.botIdx++
			if l, ok := ev.Action.FretLane(); ok {
				if ev.Down {
					s.botHeld |= 1 << l
				} else {
					s.botHeld &^= 1 << l
				}
			}
			s.feed(ev, ev.Time, "autoplay")
		}
	}
	// engines
	for _, s := range p.seats {
		if !s.playing() {
			continue
		}
		judgeNow := p.songNow(now) - s.audioOff
		if p.started || judgeNow > -1 {
			s.eng.Advance(judgeNow - engineLag)
		}
	}
	p.judge(a, now)
	p.updateStems()
	p.drop(a)
	if p.party {
		p.updateVote(a, now)
		if p.endAt > 0 {
			return nil // skipped by vote
		}
	}

	if p.failed {
		p.finish(a, false)
		return nil
	}
	judgeNow := p.songNow(now) - p.audioOff
	if !p.finished && p.allDone() && judgeNow > p.lastNote+2.5 {
		p.finish(a, true)
		return nil
	}
	if pauseReq && !p.finished {
		p.openPause(a, now)
	}
	if a.Opts.Screenshot != "" && p.renderNow(now) >= a.Opts.ShotAt-0.3 {
		a.NearShot = true
	}
	if a.Opts.Screenshot != "" && a.Opts.ShotScene != "results" && p.renderNow(now) >= a.Opts.ShotAt && !a.wantShot {
		a.TakeScreenshotNext()
	}
	return nil
}

func hasAct(acts []action.Action, want action.Action) bool {
	for _, a := range acts {
		if a == want {
			return true
		}
	}
	return false
}

func (p *BandPlay) allDone() bool {
	for _, s := range p.seats {
		if s.active() && !s.eng.Done() {
			return false
		}
	}
	return true
}

// judge runs every seat's judgments and the band's fail and save rules.
func (p *BandPlay) judge(a *App, now float64) {
	saves := false
	for _, s := range p.seats {
		if !s.playing() {
			continue
		}
		if s.judge(a) && !s.out {
			saves = true
		}
		st := s.eng.Stats()
		if st.Failed && !s.out && !s.failed {
			switch p.band.Mode {
			case modeBand:
				s.out = true
				s.outAt = p.songNow(now) - s.audioOff
				s.missing = true
				log.Printf("band: %s is out", s.m.prof.Name)
			default:
				s.failed = true
				s.missing = true
				log.Printf("band: %s failed", s.m.prof.Name)
			}
		}
	}
	if saves && p.band.Mode == modeBand {
		for _, s := range p.seats {
			if s.out {
				s.revive(p.songNow(now) - s.audioOff - engineLag)
				log.Printf("band: %s saved", s.m.prof.Name)
			}
		}
	}
	// The song fails when no one is left playing.
	playing, standing := 0, 0
	for _, s := range p.seats {
		if s.playing() {
			playing++
			if !s.out && !s.failed {
				standing++
			}
		}
	}
	if playing > 0 && standing == 0 && !p.party {
		p.failed = true
	}
}

// updateStems mutes each part's audio while everyone playing it is missing.
func (p *BandPlay) updateStems() {
	want := map[string]float64{}
	for _, st := range p.au.Stems {
		want[st.Name] = 1
	}
	quiet := map[string]bool{}
	loud := map[string]bool{}
	for _, s := range p.seats {
		if !s.playing() {
			continue
		}
		for _, name := range playback.StemFor(p.au, s.t.Key.Inst.String()) {
			if (s.missing && s.set.MuteOnMiss) || s.out || s.failed {
				quiet[name] = true
			} else {
				loud[name] = true
			}
		}
	}
	for name := range quiet {
		if !loud[name] {
			want[name] = 0
		}
	}
	for name, v := range want {
		p.au.SetTarget(name, v)
	}
}

// ranks orders seats by score for head to head (1 is the leader; 0 for
// seats not playing).
func (p *BandPlay) ranks() []int {
	out := make([]int, len(p.seats))
	type sc struct {
		i int
		v float64
	}
	var list []sc
	for i, s := range p.seats {
		if s.eng != nil {
			list = append(list, sc{i, s.eng.Stats().Score})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].v > list[j].v })
	for r, x := range list {
		rank := r + 1
		if r > 0 && x.v == list[r-1].v {
			rank = out[list[r-1].i]
		}
		out[x.i] = rank
	}
	return out
}

func (p *BandPlay) bandScore() float64 {
	t := 0.0
	for _, s := range p.seats {
		if s.eng != nil {
			t += s.eng.Stats().Score
		}
	}
	for _, r := range p.results {
		if r.left {
			t += float64(r.rec.Result.Score)
		}
	}
	return t
}

// seatResult is one player's finished run.
type seatResult struct {
	m    *member
	rec  *replay.Recording
	path string
	t    *chart.Track
	left bool // left partway (party)
}

// keep saves a seat's run (when it played anything).
func (p *BandPlay) keep(a *App, s *seat, end float64, completed bool, left bool) {
	rec := s.finish(end, completed, len(p.seats))
	if rec == nil {
		return
	}
	r := &seatResult{m: s.m, rec: rec, t: s.t, left: left}
	if rec.Result.Reached() > 0 && !p.bots {
		path, err := rec.Save(config.RunsDir())
		if err != nil {
			a.Toast("Couldn't save a run: " + err.Error())
		}
		r.path = path
		a.ScoresFor(s.m.prof).Add(rec)
		if path != "" {
			runOwners.Store(path, rec.Profile)
		}
	}
	p.results = append(p.results, r)
}

func (p *BandPlay) finish(a *App, completed bool) {
	p.finished = true
	end := p.songNow(clock.Now())
	for _, s := range p.seats {
		p.keep(a, s, end, completed, false)
	}
	log.Printf("band: finished %s (completed=%v), %d runs", p.song.Title(), completed, len(p.results))
	if p.party {
		p.betweenSongs(a)
		return
	}
	p.au.Close()
	a.SetScene(NewBandResults(a, p.band, p.song, p.c, p.results, completed))
}

// --- pause ---

func (p *BandPlay) openPause(a *App, now float64) {
	p.paused = true
	p.pauseClock = now
	p.au.Pause()
	items := []Item{{Label: "Resume", Activate: func() { p.resume() }}}
	if p.party {
		items = append(items,
			Item{Label: "Skip this song", Activate: func() { p.resume(); p.finish(a, false) }},
			Item{Label: "Back to the lobby", Activate: func() { p.quit(a, true) }},
		)
	} else {
		items = append(items,
			Item{Label: "Restart", Activate: func() {
				p.au.Pause()
				p.au.Seek(0)
				for _, s := range p.au.Stems {
					p.au.SetTarget(s.Name, 1)
				}
				a.SetScene(NewBandPlay(a, p.band, p.song, p.c, p.au))
			}},
			Item{Label: "End the song and see results", Activate: func() { p.paused = false; p.finish(a, false) }},
			Item{Label: "Quit to song list", Activate: func() { p.quit(a, false) }},
			Item{Label: "Back to the lobby", Activate: func() { p.quit(a, true) }},
		)
	}
	p.pause = List{Visible: len(items), Items: items}
}

func (p *BandPlay) quit(a *App, lobby bool) {
	p.au.Close()
	if lobby {
		a.SetScene(NewLobby(a))
		return
	}
	a.SetScene(NewSongSelect(a))
}

func (p *BandPlay) resume() {
	now := clock.Now()
	p.startClock += now - p.pauseClock
	p.paused = false
	p.prevClock = now
	if p.started {
		p.au.Play()
	}
	for _, s := range p.seats {
		if s.mapper != nil {
			s.mapper.Reset()
		}
	}
}

func (p *BandPlay) updatePause(a *App, now float64) error {
	if p.pause.Update(a) {
		return nil
	}
	if a.Pressed(action.MenuBack) {
		p.resume()
	}
	return nil
}

// --- drawing ---

// top is where the highways start: under the top bar and lyrics.
func (p *BandPlay) top() float64 {
	t := 52.0
	if h := p.lyr.Height(); h > 0 {
		t += h
	}
	return t + 6
}

func (p *BandPlay) Draw(dst *ebiten.Image, a *App) {
	now := clock.Now()
	if p.paused {
		now = p.pauseClock
	}
	rn := p.renderNow(now)
	sp := false
	for _, s := range p.seats {
		if s.eng != nil && s.eng.Stats().SPActive {
			sp = true
		}
	}
	render.Background(dst, rn, sp)
	slots := bandSlots(len(p.seats), a.Settings.Multi.Layout, p.top())
	var ranks []int
	if p.band.Mode == modeH2H && len(p.seats) > 1 {
		ranks = p.ranks()
	}
	for i, s := range p.seats {
		r := 0
		if ranks != nil {
			r = ranks[i]
		}
		srn := p.songNow(now) - s.audioOff + s.videoOff
		s.draw(dst, a, p, slots[i], srn, r)
	}
	if len(p.seats) == 0 {
		render.TextShadow(dst, "Press START or green on any controller to join", render.UIBold, 48, render.ScreenW/2, 520, th.Text, render.Center)
	}
	p.lyr.Draw(dst, rn, render.ScreenW/2, a.frame)
	p.drawTop(dst, a, rn)

	if !p.started && p.songNow(now) < 0 {
		left := -p.songNow(now)
		if left < 1.5 {
			render.TextShadow(dst, fmt.Sprintf("%d", int(math.Ceil(left))), render.Display, 120, render.ScreenW/2, 420, render.WithAlpha(th.Text, math.Min(1, left)), render.Center)
		}
	}
	if p.bots {
		render.Text(dst, "AUTOPLAY", render.CondBold, 22, render.ScreenW-110, 12, th.TextDim, render.Right)
	}
	if p.endAt > 0 {
		p.drawBetween(dst, a)
	}
	if p.paused {
		render.Dim(dst, render.RGBA(0, 0, 0, 0xA8))
		h := 200 + float64(len(p.pause.Items))*72
		Panel(dst, 600, 540-h/2, 720, h)
		render.Text(dst, "Paused", render.Display, 64, 640, 560-h/2, th.Text, render.Left)
		p.pause.Draw(dst, 660, 540-h/2+130, 600, 72)
	}
}

// drawTop draws the bar across the top: song, progress, section and the
// band's score (or who's leading).
func (p *BandPlay) drawTop(dst *ebiten.Image, a *App, rn float64) {
	render.FillRect(dst, 0, 0, render.ScreenW, 48, render.RGBA(0, 0, 0, 0x78))
	if p.length > 0 {
		render.FillRect(dst, 0, 44, render.ScreenW*math.Max(0, math.Min(1, rn/p.length)), 4, render.WithAlpha(th.Accent, 0.9))
	}
	title := truncateW(render.PlainText(p.song.Name)+"  ·  "+render.PlainText(p.song.Artist), render.Cond, 26, 640)
	render.Text(dst, title, render.Cond, 26, 20, 8, th.Text, render.Left)
	idx := -1
	for i, s := range p.c.Sections {
		if s.Time <= rn {
			idx = i
		}
	}
	if idx != p.section && idx >= 0 {
		p.section = idx
		p.secName, p.secAt = p.c.Sections[idx].Name, p.c.Sections[idx].Time
	}
	label := ""
	switch p.band.Mode {
	case modeBand:
		label = fmt.Sprintf("BAND  %d", int64(p.bandScore()+0.5))
	case modeH2H:
		label = p.leaderText()
	case modeParty:
		label = "PARTY"
		if len(p.seats) < MaxPlayers {
			label += "  ·  press START to join"
		}
		if p.vote != nil {
			now := clock.Now()
			if p.paused {
				now = p.pauseClock
			}
			label = p.voteLabel(now)
		}
	}
	render.Text(dst, label, render.CondBold, 30, render.ScreenW/2, 6, th.Text, render.Center)
	if p.secName != "" && rn-p.secAt < 4 {
		a := math.Min(1, (4-(rn-p.secAt))/0.6)
		render.Text(dst, p.secName, render.Cond, 26, render.ScreenW-150, 8, render.WithAlpha(th.TextDim, a), render.Right)
	}
	render.Text(dst, clockText(math.Max(0, p.length-rn)), render.Cond, 26, render.ScreenW-20, 8, th.TextDim, render.Right)
}

func clockText(sec float64) string {
	s := int(sec + 0.5)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (p *BandPlay) leaderText() string {
	ranks := p.ranks()
	var lead *seat
	second := 0.0
	for i, s := range p.seats {
		if ranks[i] == 1 && lead == nil {
			lead = s
		}
	}
	if lead == nil {
		return "HEAD TO HEAD"
	}
	for _, s := range p.seats {
		if s != lead && s.eng != nil && s.eng.Stats().Score > second {
			second = s.eng.Stats().Score
		}
	}
	gap := int64(lead.eng.Stats().Score - second + 0.5)
	if gap == 0 {
		return "HEAD TO HEAD  ·  tied"
	}
	return fmt.Sprintf("%s LEADS BY %d", lead.m.prof.Name, gap)
}

// --- loading ---

// BandLoading reads a chart and its audio for the band.
type BandLoading struct {
	*Loading
	band *Band
}

// NewBandLoading starts loading a song for the band.
func NewBandLoading(a *App, b *Band, s *chart.Song) *BandLoading {
	inst, diff := chart.Guitar, chart.Expert
	if len(b.Members) > 0 {
		inst, diff = b.Members[0].inst, b.Members[0].diff
	}
	return &BandLoading{Loading: NewLoading(a, s, inst, diff), band: b}
}

func (l *BandLoading) Update(a *App) error {
	if l.err != "" {
		if a.Pressed(action.MenuBack) || a.Pressed(action.MenuConfirm) {
			a.SetScene(NewSongSelect(a))
		}
		return nil
	}
	select {
	case r := <-l.done:
		if r.err != nil {
			l.err = r.err.Error()
			log.Printf("load %s: %v", l.song.Dir, r.err)
			return nil
		}
		a.SetScene(NewBandPlay(a, l.band, l.song, r.c, r.audio))
	default:
	}
	return nil
}

func (l *BandLoading) Draw(dst *ebiten.Image, a *App) {
	l.Loading.Draw(dst, a)
	if l.err != "" {
		return
	}
	y := 720.0
	for _, m := range l.band.Members {
		render.Text(dst, m.prof.Name+"  ·  "+m.partLabel(), render.Cond, 28, render.ScreenW/2, y, profileColor(m.prof), render.Center)
		y += 38
	}
}
