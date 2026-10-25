package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// seat is one player's part of a multiplayer song: their own engine,
// controller mapping, recording, highway and scoreboard. The song clock and
// audio are shared (see BandPlay).
type seat struct {
	m   *member
	set *config.Settings // the shared settings with the player's own

	t      *chart.Track // what they play (the tail of the part when they joined late)
	first  int          // index in the full part of t's first note
	noPart bool         // the song has no part for their instrument

	eng    engine.Engine
	mapper *input.Mapper
	mapped []input.Mapped
	rec    *replay.Recorder

	hw    *render.Highway
	hud   render.SeatHUD
	frame render.FrameState
	hs    render.HUDState
	drums bool
	view  *ebiten.Image

	audioOff, videoOff float64
	lastEvT            float64
	jIdx               int
	milestone          int
	spReady            bool
	solosDone          map[int]bool
	lastErrSound       float64
	lastNote           float64

	missing bool      // in a miss (their stem is quiet)
	out     bool      // band: failed, waiting for a bandmate's star power
	failed  bool      // head to head: failed and stopped
	revives []float64 // engine times they were saved
	outAt   float64

	bot     []action.Event
	botIdx  int
	botHeld uint8

	// party mode
	menu    *List   // their own menu (change part, leave)
	joining *List   // setting up after joining mid-song
	newProf bool    // joining: the profile row is on "New profile"
	joinedT float64 // song time they joined (party)
	left    bool    // asked to leave: dropped at the end of the frame
}

// newSeat sets a player up for a chart. from is the song time their part
// starts (0 for the whole song); notes before it aren't theirs.
func newSeat(a *App, p *BandPlay, m *member, from float64) *seat {
	s := &seat{m: m, solosDone: map[int]bool{}, lastErrSound: math.Inf(-1), lastEvT: math.Inf(-1)}
	s.set = a.SettingsFor(m.prof)
	if p.band.Mode == modeParty {
		s.set.Engine.NoFail = true
	}
	s.audioOff = s.set.AudioOffsetMs / 1000
	s.videoOff = s.set.VideoOffsetMs / 1000
	s.hw = &render.Highway{Cfg: s.set.Highway}
	full := trackFor(p.c, m.inst, m.diff)
	if full == nil {
		s.noPart = true
		return s
	}
	if from > 0 {
		s.first = full.FirstAfter(from)
	}
	s.t = full.Tail(s.first)
	s.drums = s.t.Key.Inst.IsDrums()
	s.eng = engine.New(p.c, s.t, s.set.Engine)
	if from > 0 {
		s.lastEvT = from - 0.5
	}
	fam := m.family()
	s.mapper = input.NewMapper(a.BindingsFor(m.prof).Profile(fam), a.Input.Devices())
	s.rec = replay.NewRecorder(p.song, s.t.Key.Inst, s.t.Key.Diff, s.set.Engine)
	r := s.rec.Rec
	r.AudioOffset, r.VideoOffset = s.set.AudioOffsetMs, s.set.VideoOffsetMs
	r.Device = m.dev.name(a)
	r.Profile, r.Player = m.prof.ID, m.prof.Name
	r.Mode, r.Session = p.band.Mode, p.band.Session
	r.FirstNote = s.first
	if n := len(s.t.Notes); n > 0 {
		s.lastNote = s.t.Notes[n-1].SustainEnd()
	}
	s.frame = render.FrameState{Chart: p.c, Track: s.t, Eng: s.eng, ExpertPlus: s.set.Engine.ExpertPlus, Fx: render.ParseFxLevel(s.set.HitFx), Bounce: s.set.HighwayBounce}
	for i := range s.frame.Flash {
		s.frame.Flash[i] = -100
		s.frame.PadHit[i] = -100
	}
	s.frame.MissFlash = -100
	s.hs = render.HUDState{BaseMax: 4, ShowTiming: s.set.ShowTiming, LastHitAt: -100, NoFail: s.set.Engine.NoFail}
	if s.t.Key.Inst == chart.Bass && s.set.Engine.BassMaxMult > 0 {
		s.hs.BaseMax = s.set.Engine.BassMaxMult
	}
	if p.bots {
		s.bot = botEvents(s.t, s.set.Engine.ExpertPlus)
		r.Device = "autoplay"
	}
	return s
}

// playing reports whether the seat is in the song (has a part and an
// engine, and isn't setting up).
func (s *seat) playing() bool { return s.eng != nil && s.joining == nil }

// active reports whether the seat still counts: playing and not out of
// the song for good.
func (s *seat) active() bool { return s.playing() && !s.failed }

func (s *seat) feed(ev action.Event, st float64, src string) {
	if st < s.lastEvT {
		st = s.lastEvT
	}
	s.lastEvT = st
	ev.Time = st
	s.eng.Process(ev)
	s.rec.Input(ev, src)
	if s.drums && ev.Down && ev.Action.IsDrum() {
		if l := drumLaneOf(s.t.DrumMode, ev.Action); l >= 0 {
			s.frame.PadHit[l] = st + s.videoOff
		}
	}
}

func (s *seat) post(text string, col color.RGBA, at float64) {
	s.hs.Messages = append(s.hs.Messages, render.Message{Text: text, Color: col, At: at})
	if len(s.hs.Messages) > 3 {
		s.hs.Messages = s.hs.Messages[len(s.hs.Messages)-3:]
	}
}

// judge applies the engine's new judgments to the seat's effects and
// messages. It returns whether star power was activated (band saves).
func (s *seat) judge(a *App) (activated bool) {
	js := s.eng.Judgments()
	st := s.eng.Stats()
	for ; s.jIdx < len(js); s.jIdx++ {
		j := js[s.jIdx]
		rt := j.Time + s.videoOff
		switch j.Result {
		case engine.Hit:
			s.missing = false
			if s.drums {
				s.frame.Flash[j.Lane] = rt
				s.frame.Hits = append(s.frame.Hits, render.HitFx{At: rt, Lane: j.Lane, SP: st.SPActive})
			} else if j.Note >= 0 {
				n := &s.t.Notes[j.Note]
				for l := 0; l < chart.NumLanes; l++ {
					if n.Lanes&(1<<l) != 0 {
						s.frame.Flash[l] = rt
						s.frame.Hits = append(s.frame.Hits, render.HitFx{At: rt, Lane: l, SP: st.SPActive})
					}
				}
			}
			s.hs.LastOffset = j.Offset * 1000
			s.hs.LastHitAt = rt
			if m := (j.Streak / 50) * 50; m >= 50 && m > s.milestone && (m == 50 || m%100 == 0) {
				s.milestone = m
				s.post(fmt.Sprintf("%d STREAK!", m), th.Text, rt)
			}
		case engine.Miss, engine.Overstrum, engine.Overhit:
			if errorSound(s.set.ErrorSounds, j.Result) && j.Time-s.lastErrSound > 0.06 && !s.out {
				s.lastErrSound = j.Time
				a.SFX.Miss(s.set.ErrorVolume * a.Settings.Volume)
			}
			if j.Result == engine.Overhit && !s.set.Engine.OverhitBreaks {
				continue
			}
			s.frame.MissFlash = rt
			s.missing = true
			if j.Streak == 0 {
				s.milestone = 0
			}
		case engine.SPActivate:
			s.spReady = false
			s.post("STAR POWER!", th.StarPower, rt)
			activated = true
		case engine.SPPhrase:
			if st.SPEnergy >= 0.5 && !st.SPActive && !s.spReady {
				s.spReady = true
				s.post("STAR POWER READY", th.StarPower, rt)
			}
		}
	}
	// solo results
	for i, so := range s.t.Solos {
		if s.solosDone[i] || s.lastEvT < so.End+0.2 {
			continue
		}
		s.solosDone[i] = true
		total, hit := 0, 0
		for k := range s.t.Notes {
			n := &s.t.Notes[k]
			if n.Tick < so.StartTick || n.Tick > so.EndTick {
				continue
			}
			total++
			if s.eng.Status(k) == engine.NoteHit {
				hit++
			}
		}
		if total > 0 {
			pct := hit * 100 / total
			msg := fmt.Sprintf("SOLO %d%%", pct)
			if pct == 100 {
				msg = "PERFECT SOLO!"
			}
			s.post(msg, th.Accent, so.End+s.videoOff)
		}
	}
	return activated
}

// revive brings an out player back (a bandmate's star power).
func (s *seat) revive(at float64) {
	s.out = false
	s.eng.Revive()
	s.revives = append(s.revives, at)
	if at > s.lastEvT {
		s.lastEvT = at // inputs before the save stay before it, as in a replay
	}
	s.missing = false
	s.post("SAVED!", th.Good, at+s.videoOff)
}

// draw renders the seat's view (highway, scoreboard, menus) into its own
// image and puts it in its slot.
func (s *seat) draw(dst *ebiten.Image, a *App, p *BandPlay, sl slot, rn float64, rank int) {
	vw := math.Max(200, math.Round(sl.w*render.SeatH/sl.h))
	if s.view == nil || s.view.Bounds().Dx() != int(vw) {
		if s.view != nil {
			s.view.Deallocate()
		}
		s.view = ebiten.NewImage(int(vw), int(render.SeatH))
	}
	v := s.view
	v.Clear()
	hc := render.SeatHighway(s.set.Highway, vw)
	if s.hw == nil {
		s.hw = &render.Highway{}
	}
	s.hw.Cfg = hc
	info := render.SeatInfo{Name: s.m.prof.Name, Color: profileColor(s.m.prof), Part: s.m.partLabel(), Rank: rank,
		ShowRock: s.playing() && !s.set.Engine.NoFail}
	if p.vote != nil && s.joining == nil {
		info.Banner = "Skip this song? START to vote yes"
		if p.vote.yes[s.m] {
			info.Banner = "You voted to skip"
		}
	}
	if s.playing() {
		f := &s.frame
		st := s.eng.Stats()
		f.Now = rn
		f.SPActive = st.SPActive
		if s.bot != nil {
			f.Held = s.botHeld
		} else if !s.drums {
			f.Held = 0
			for l, act := range fretActions {
				if s.mapper.IsDown(act) {
					f.Held |= 1 << l
				}
			}
			f.Whammy = s.mapper.Analog(action.Whammy)
		}
		keep := f.Hits[:0]
		for _, h := range f.Hits {
			if rn-h.At < render.FxLife {
				keep = append(keep, h)
			}
		}
		f.Hits = keep
		s.hw.Draw(v, f)
		h := &s.hs
		h.Now = rn
		h.Score = st.Score
		h.Streak = st.Streak
		h.Mult = s.eng.Multiplier()
		h.SPEnergy = st.SPEnergy
		h.SPActive = st.SPActive
		h.Rock = st.Rock
		info.Part = s.t.Key.Diff.Title() + " " + s.t.Key.Inst.Title()
	}
	switch {
	case s.noPart:
		info.Overlay = "SITTING OUT"
		info.OverlaySub = "No " + s.m.inst.Title() + " part in this song"
		if p.party {
			info.OverlaySub += ". START to switch parts"
		}
	case s.out:
		info.Overlay, info.Dim = "OUT", 0.55
		info.OverlaySub = "A bandmate's star power brings you back"
	case s.failed:
		info.Overlay, info.Dim = "FAILED", 0.6
	case s.joinedLate() && len(s.t.Notes) > 0 && s.t.Notes[0].Time-rn > 0 && s.t.Notes[0].Time-rn < 4:
		info.Overlay = fmt.Sprintf("%d", int(math.Ceil(s.t.Notes[0].Time-rn)))
		info.OverlaySub = "Get ready!"
	}
	if s.joining == nil {
		s.hud.Layout = a.Settings.SeatHUD
		s.hud.Draw(v, hc, &s.hs, &info)
	}
	if s.joining != nil {
		s.drawCard(v, a, vw, "Join the party", s.joining)
	} else if s.menu != nil {
		s.drawCard(v, a, vw, s.m.prof.Name, s.menu)
	}

	sc := sl.h / render.SeatH
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(sc, sc)
	op.GeoM.Translate(sl.x+(sl.w-vw*sc)/2, sl.y)
	dst.DrawImage(v, op)
}

// joinedLate reports whether the player came in partway through the song.
func (s *seat) joinedLate() bool { return s.playing() && s.joinedT > 0 }

// drawCard draws a menu panel in the middle of the seat's view.
func (s *seat) drawCard(v *ebiten.Image, a *App, vw float64, title string, l *List) {
	w := math.Min(vw*0.9, 640)
	k := w / 640
	rows := float64(len(l.Items))
	rowH := 64 * k
	h := 150*k + rows*rowH + 70*k
	x, y := (vw-w)/2, (render.SeatH-h)/2
	Panel(v, x, y, w, h)
	render.FillRect(v, x, y, w, 10*k, profileColor(s.m.prof))
	render.Text(v, title, render.UIBold, 44*k, x+24*k, y+30*k, th.Text, render.Left)
	render.Text(v, truncateW(s.m.dev.name(a), render.Cond, 24*k, w-48*k), render.Cond, 24*k, x+24*k, y+88*k, th.TextDim, render.Left)
	l.Draw(v, x+12*k, y+140*k, w-24*k, rowH)
	render.Text(v, "Red / back to close", render.Cond, 22*k, x+24*k, y+h-50*k, th.TextDim, render.Left)
}

// finish turns the seat's run into a recording.
func (s *seat) finish(end float64, completed bool, players int) *replay.Recording {
	if s.eng == nil {
		return nil
	}
	s.eng.Advance(end - s.audioOff)
	rec := s.rec.Finish(s.eng, end-s.audioOff, completed && !s.failed && s.first == 0)
	rec.Players = players
	rec.Revives = append([]float64(nil), s.revives...)
	return rec
}
