package app

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// engineLag keeps the engine slightly behind real time so input events that
// arrive a frame late are still processed in order.
const engineLag = 0.040

// Play is the gameplay screen.
type Play struct {
	song  *chart.Song
	c     *chart.Chart
	t     *chart.Track
	au    *playback.Song
	eng   engine.Engine
	m     *input.Mapper
	rec   *replay.Recorder
	hw    *render.Highway
	hud   *render.HUD
	frame render.FrameState
	hs    render.HUDState
	drums bool

	startClock float64
	started    bool
	biasSet    bool
	biasBuf    []float64
	bias       float64
	audioOff   float64
	videoOff   float64
	lastEvT    float64
	jIdx       int
	mute       []string
	lastNote   float64
	finished   bool
	failed     bool
	section    int
	spReady    bool
	milestone  int
	solosDone  map[int]bool
	prevClock  float64

	paused     bool
	pauseClock float64
	pause      List

	bot     []action.Event
	botIdx  int
	botHeld uint8

	mapped []input.Mapped
}

// NewPlay starts a song.
func NewPlay(a *App, s *chart.Song, c *chart.Chart, t *chart.Track, au *playback.Song) *Play {
	set := a.Settings
	p := &Play{song: s, c: c, t: t, au: au, drums: t.Key.Inst.IsDrums(), solosDone: map[int]bool{}, section: -1}
	p.eng = engine.New(c, t, set.Engine)
	fam := input.FamilyGuitar
	if p.drums {
		fam = input.FamilyDrums
	}
	p.m = input.NewMapper(a.Bindings.Profile(fam), a.Input.Devices())
	p.rec = replay.NewRecorder(s, t.Key.Inst, t.Key.Diff, set.Engine)
	p.rec.Rec.AudioOffset = set.AudioOffsetMs
	p.rec.Rec.VideoOffset = set.VideoOffsetMs
	p.rec.Rec.Device = deviceSummary(a, fam)
	hc := set.Highway
	if hc.NearWidth == 0 {
		hc = render.DefaultHighway()
	}
	p.hw = &render.Highway{Cfg: hc}
	p.hud = &render.HUD{Highway: p.hw}
	p.audioOff = set.AudioOffsetMs / 1000
	p.videoOff = set.VideoOffsetMs / 1000
	p.mute = playback.StemFor(au, t.Key.Inst.String())
	au.SetMaster(set.Volume)
	if n := len(t.Notes); n > 0 {
		p.lastNote = t.Notes[n-1].SustainEnd()
	}
	lead := set.LeadIn
	if lead <= 0 {
		lead = 2.5
	}
	now := clock.Now()
	p.startClock = now + lead
	p.prevClock = now
	p.lastEvT = math.Inf(-1)
	if a.Opts.Autoplay {
		p.bot = botEvents(t, set.Engine.ExpertPlus)
		p.rec.Rec.Device = "autoplay"
	}
	p.frame = render.FrameState{Chart: c, Track: t, Eng: p.eng, ExpertPlus: set.Engine.ExpertPlus, Fx: render.ParseFxLevel(set.HitFx)}
	for i := range p.frame.Flash {
		p.frame.Flash[i] = -100
		p.frame.PadHit[i] = -100
	}
	p.frame.MissFlash = -100
	p.hs = render.HUDState{
		Title: s.Name, Artist: s.Artist, Charter: s.Charter, ShowTiming: set.ShowTiming,
		Instrument: t.Key.Inst.Title(), Difficulty: t.Key.Diff.Title(), BaseMax: 4, NoFail: set.Engine.NoFail,
		LastHitAt: -100, SectionAt: -100,
	}
	if t.Key.Inst == chart.Bass && set.Engine.BassMaxMult > 0 {
		p.hs.BaseMax = set.Engine.BassMaxMult
	}
	return p
}

func deviceSummary(a *App, fam input.Family) string {
	var names []string
	for _, d := range a.Input.Devices() {
		if d.Src == input.SrcKeyboard {
			continue
		}
		if fam == input.FamilyDrums && d.Src != input.SrcMIDI {
			continue
		}
		if fam == input.FamilyGuitar && d.Src == input.SrcMIDI {
			continue
		}
		names = append(names, d.Name)
	}
	if len(names) == 0 {
		return "keyboard"
	}
	return fmt.Sprint(names)
}

func (p *Play) songNow(clk float64) float64 { return clk - p.startClock }

func (p *Play) Update(a *App) error {
	now := clock.Now()
	dt := now - p.prevClock
	p.prevClock = now
	if p.paused {
		return p.updatePause(a, now)
	}

	// Start audio when the lead-in ends.
	if !p.started && p.songNow(now) >= 0 {
		log.Printf("play: %s, %s %s, %d notes, stems %d, mute on miss %v", p.song.Title(), p.t.Key.Diff.Title(), p.t.Key.Inst.Title(), len(p.t.Notes), len(p.au.Stems), p.mute)
		p.au.Play()
		p.started = true
		p.startClock = now
	}
	if p.started {
		p.syncToAudio(now)
	}
	p.au.Update(dt)

	// Input.
	pauseReq := false
	for _, ev := range a.Raw {
		if ev.Src == input.SrcKeyboard && ebiten.Key(ev.Code) == ebiten.KeyEscape && ev.Down {
			pauseReq = true
		}
		if p.bot != nil {
			continue
		}
		p.mapped = p.m.Map(ev, p.mapped[:0])
		for _, mp := range p.mapped {
			if mp.Action == action.Start && mp.Down {
				pauseReq = true
				continue
			}
			st := p.songNow(mp.Time) - p.audioOff
			p.feed(mp.Event, st, mp.Src)
		}
	}
	if p.bot != nil {
		judgeNow := p.songNow(now) - p.audioOff
		for p.botIdx < len(p.bot) && p.bot[p.botIdx].Time <= judgeNow {
			ev := p.bot[p.botIdx]
			p.botIdx++
			if l, ok := ev.Action.FretLane(); ok {
				if ev.Down {
					p.botHeld |= 1 << l
				} else {
					p.botHeld &^= 1 << l
				}
			}
			p.feed(ev, ev.Time, "autoplay")
		}
	}
	judgeNow := p.songNow(now) - p.audioOff
	if p.started || judgeNow > -1 {
		p.eng.Advance(judgeNow - engineLag)
	}
	p.processJudgments(a)

	st := p.eng.Stats()
	if st.Failed && !p.failed {
		p.failed = true
		p.finish(a, false)
		return nil
	}
	// End a few seconds after the last note (or when audio runs out).
	if !p.finished && p.eng.Done() && judgeNow > p.lastNote+2.5 {
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
		log.Printf("screenshot at song time %.3f", p.renderNow(now))
		a.TakeScreenshotNext()
	}
	return nil
}

func (p *Play) feed(ev action.Event, st float64, src string) {
	if st < p.lastEvT {
		st = p.lastEvT
	}
	p.lastEvT = st
	ev.Time = st
	p.eng.Process(ev)
	p.rec.Input(ev, src)
	if p.drums && ev.Down && ev.Action.IsDrum() {
		if l := drumLaneOf(p.t.DrumMode, ev.Action); l >= 0 {
			p.frame.PadHit[l] = st + p.videoOff
		}
	}
}

// syncToAudio keeps the song clock locked to the audio device. The offset
// between the player's reported position and our clock is measured during
// the first second; afterwards only drift from that baseline is corrected.
func (p *Play) syncToAudio(now float64) {
	if !p.au.Playing() {
		return
	}
	pos := p.au.Position()
	st := now - p.startClock
	diff := pos - st
	if !p.biasSet {
		if pos > 0.1 {
			p.biasBuf = append(p.biasBuf, diff)
		}
		if len(p.biasBuf) >= 45 {
			s := append([]float64(nil), p.biasBuf...)
			sort.Float64s(s)
			p.bias = s[len(s)/2]
			p.biasSet = true
			log.Printf("audio sync: player position leads the song clock by %.1f ms (spread %.1f ms)", p.bias*1000, (s[len(s)*9/10]-s[len(s)/10])*1000)
		}
		return
	}
	e := diff - p.bias
	switch {
	case math.Abs(e) > 0.25:
		p.startClock -= e // big jump (audio hiccup): resync immediately
	case math.Abs(e) > 0.002:
		p.startClock -= e * 0.02
	}
}

func (p *Play) renderNow(now float64) float64 {
	return p.songNow(now) - p.audioOff + p.videoOff
}

func (p *Play) processJudgments(a *App) {
	js := p.eng.Judgments()
	st := p.eng.Stats()
	for ; p.jIdx < len(js); p.jIdx++ {
		j := js[p.jIdx]
		rt := j.Time + p.videoOff
		switch j.Result {
		case engine.Hit:
			if p.drums {
				p.frame.Flash[j.Lane] = rt
				p.frame.Hits = append(p.frame.Hits, render.HitFx{At: rt, Lane: j.Lane, SP: st.SPActive})
			} else if j.Note >= 0 {
				n := &p.t.Notes[j.Note]
				for l := 0; l < chart.NumLanes; l++ {
					if n.Lanes&(1<<l) != 0 {
						p.frame.Flash[l] = rt
						p.frame.Hits = append(p.frame.Hits, render.HitFx{At: rt, Lane: l, SP: st.SPActive})
					}
				}
			}
			p.hs.LastOffset = j.Offset * 1000
			p.hs.LastHitAt = rt
			for _, s := range p.mute {
				p.au.SetTarget(s, 1)
			}
			if m := (j.Streak / 50) * 50; m >= 50 && m > p.milestone && (m == 50 || m%100 == 0) {
				p.milestone = m
				p.post(fmt.Sprintf("%d NOTE STREAK!", m), th.Text, rt)
			}
		case engine.Miss, engine.Overstrum, engine.Overhit:
			if j.Result == engine.Overhit && !a.Settings.Engine.OverhitBreaks {
				continue
			}
			p.frame.MissFlash = rt
			if a.Settings.MuteOnMiss {
				for _, s := range p.mute {
					p.au.SetTarget(s, 0)
				}
			}
			if j.Result == engine.Overstrum {
				a.SFX.Miss(0.45 * a.Settings.Volume)
			}
			if j.Streak == 0 {
				p.milestone = 0
			}
		case engine.SPActivate:
			p.spReady = false
			p.post("STAR POWER!", th.StarPower, rt)
		case engine.SPPhrase:
			if st.SPEnergy >= 0.5 && !st.SPActive && !p.spReady {
				p.spReady = true
				p.post("STAR POWER READY", th.StarPower, rt)
			}
		}
	}
	// Solo results.
	judgeNow := p.lastEvT
	for i, s := range p.t.Solos {
		if p.solosDone[i] || judgeNow < s.End+0.2 {
			continue
		}
		p.solosDone[i] = true
		total, hit := 0, 0
		for k := range p.t.Notes {
			n := &p.t.Notes[k]
			if n.Tick < s.StartTick || n.Tick > s.EndTick {
				continue
			}
			total++
			if p.eng.Status(k) == engine.NoteHit {
				hit++
			}
		}
		if total > 0 {
			pct := hit * 100 / total
			msg := fmt.Sprintf("SOLO  %d%%", pct)
			if pct == 100 {
				msg = "PERFECT SOLO!"
			}
			p.post(msg, th.Accent, s.End+p.videoOff)
		}
	}
}

func (p *Play) post(text string, col color.RGBA, at float64) {
	p.hs.Messages = append(p.hs.Messages, render.Message{Text: text, Color: col, At: at})
	if len(p.hs.Messages) > 3 {
		p.hs.Messages = p.hs.Messages[len(p.hs.Messages)-3:]
	}
}

func (p *Play) finish(a *App, completed bool) {
	p.finished = true
	end := p.songNow(clock.Now()) - p.audioOff
	p.eng.Advance(end)
	rec := p.rec.Finish(p.eng, end, completed)
	log.Printf("run finished (completed=%v): score %d, notes %d/%d, inputs %d", completed, rec.Result.Score, rec.Result.NotesHit, rec.Result.NotesHit+rec.Result.NotesMissed, len(rec.Inputs))
	p.au.Close()
	path := ""
	if rec.Result.NotesHit+rec.Result.NotesMissed > 0 {
		var err error
		path, err = rec.Save(config.RunsDir())
		if err != nil {
			a.Toast("Couldn't save the run: " + err.Error())
		}
	}
	a.SetScene(NewResults(a, rec, path, p.c, p.t, p.song))
}

func (p *Play) openPause(a *App, now float64) {
	p.paused = true
	p.pauseClock = now
	p.au.Pause()
	p.pause = List{Visible: 4, Items: []Item{
		{Label: "Resume", Activate: func() { p.resume(now) }},
		{Label: "Restart", Activate: func() {
			p.au.Pause()
			p.au.Seek(0)
			for _, s := range p.au.Stems {
				p.au.SetTarget(s.Name, 1)
			}
			a.SetScene(NewPlay(a, p.song, p.c, p.t, p.au))
		}},
		{Label: "End run and review it", Activate: func() { p.finish(a, false) }},
		{Label: "Quit to song list", Activate: func() {
			p.au.Close()
			a.SetScene(NewSongSelect(a))
		}},
	}}
}

func (p *Play) resume(float64) {
	now := clock.Now()
	p.startClock += now - p.pauseClock
	p.paused = false
	p.prevClock = now
	if p.started {
		p.au.Play()
	}
	p.m.Reset()
}

func (p *Play) updatePause(a *App, now float64) error {
	if p.pause.Update(a) {
		return nil
	}
	if a.Pressed(action.MenuBack) {
		p.resume(now)
	}
	return nil
}

func (p *Play) Draw(dst *ebiten.Image, a *App) {
	now := clock.Now()
	if p.paused {
		now = p.pauseClock
	}
	rn := p.renderNow(now)
	st := p.eng.Stats()
	render.Background(dst, rn, st.SPActive)
	f := &p.frame
	f.Now = rn
	f.SPActive = st.SPActive
	if p.bot != nil {
		f.Held = p.botHeld
	} else if !p.drums {
		f.Held = 0
		for l, act := range fretActions {
			if p.m.IsDown(act) {
				f.Held |= 1 << l
			}
		}
		f.Whammy = p.m.Analog(action.Whammy)
	}
	// Drop hit effects that have finished.
	keep := f.Hits[:0]
	for _, h := range f.Hits {
		if rn-h.At < render.FxLife {
			keep = append(keep, h)
		}
	}
	f.Hits = keep
	p.hw.Draw(dst, f)

	h := &p.hs
	h.Now = rn
	h.Score = st.Score
	h.Streak = st.Streak
	h.Mult = p.eng.Multiplier()
	h.SPEnergy = st.SPEnergy
	h.SPActive = st.SPActive
	h.Rock = st.Rock
	if p.lastNote > 0 {
		h.Progress = rn / p.lastNote
	}
	h.TitleAlpha = 0
	if rn < 2 {
		h.TitleAlpha = math.Min(1, (2-rn)/0.8)
	}
	// section banner
	idx := -1
	for i, s := range p.c.Sections {
		if s.Time <= rn {
			idx = i
		}
	}
	if idx != p.section && idx >= 0 {
		p.section = idx
		h.Section = p.c.Sections[idx].Name
		h.SectionAt = p.c.Sections[idx].Time
	}
	p.hud.Draw(dst, h)

	if p.bot != nil {
		render.Text(dst, "AUTOPLAY", render.CondBold, 26, render.ScreenW-40, 20, th.TextDim, render.Right)
	}
	if !p.started && p.songNow(now) < 0 {
		left := -p.songNow(now)
		if left < 1.5 {
			render.TextShadow(dst, fmt.Sprintf("%d", int(math.Ceil(left))), render.Display, 120, render.ScreenW/2, 420, render.WithAlpha(th.Text, math.Min(1, left)), render.Center)
		}
	}
	if p.paused {
		render.FillRect(dst, 0, 0, render.ScreenW, render.ScreenH, render.RGBA(0, 0, 0, 0xA8))
		Panel(dst, 600, 300, 720, 460)
		render.Text(dst, "Paused", render.Display, 64, 640, 320, th.Text, render.Left)
		p.pause.Draw(dst, 660, 430, 600, 72)
	}
}
