package engine

import (
	"math"
	"math/bits"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
)

// legacyChordLenience is how long a strum waited for frets to land under
// rules version 0. Players often strum a little before finishing a chord
// shape; the wait keeps that from reading as an overstrum.
const legacyChordLenience = 0.025

func (g *Guitar) chordLenience() float64 {
	if g.set.ChordLenience > 0 {
		return g.set.ChordLenience
	}
	return legacyChordLenience
}

type sustain struct {
	note    int
	lanes   uint8
	ends    [chart.NumLanes]float64
	sp      bool
	dropped bool
}

// Guitar implements 5-fret guitar, bass, rhythm and keys.
type Guitar struct {
	base
	status []NoteStatus
	next   int
	held   uint8
	whammy float32
	sus    []sustain

	lastFretHit    float64
	fretHitPending bool    // a strum shortly after a fret hit is absorbed
	forgiveUntil   float64 // ...up to this time

	pendingStrum   float64
	strumWaiting   bool
	pendingTargets bool // there was a note in the window when the strum came
}

func newGuitar(c *chart.Chart, t *chart.Track, s Settings) *Guitar {
	g := &Guitar{}
	g.init(c, t, s)
	g.status = make([]NoteStatus, len(t.Notes))
	g.stats.NotesTotal = len(t.Notes)
	g.lastFretHit = math.Inf(-1)
	return g
}

// Held returns the fret mask currently held.
func (g *Guitar) Held() uint8 { return g.held }

// Whammy returns the current whammy position 0..1.
func (g *Guitar) Whammy() float32 { return g.whammy }

// Sustaining returns, for a note index, the lanes currently being sustained.
func (g *Guitar) Sustaining(note int) uint8 {
	for _, s := range g.sus {
		if s.note == note {
			return s.lanes
		}
	}
	return 0
}

// SustainingLanes returns every lane currently sustaining.
func (g *Guitar) SustainingLanes() uint8 {
	var m uint8
	for _, s := range g.sus {
		m |= s.lanes
	}
	return m
}

func (g *Guitar) Status(i int) NoteStatus { return g.status[i] }

func (g *Guitar) LaneHit(i int) uint8 {
	if g.status[i] == NoteHit {
		return g.track.Notes[i].Lanes
	}
	return 0
}

func (g *Guitar) Done() bool {
	return g.next >= len(g.track.Notes) && len(g.sus) == 0 && !g.strumWaiting
}

// Advance resolves timed events up to t in chronological order.
func (g *Guitar) Advance(t float64) {
	notes := g.track.Notes
	for {
		at := t
		kind := 0
		if g.next < len(notes) {
			if m := notes[g.next].Time + g.set.LateWindow; m < at {
				at, kind = m, 1
			}
		}
		si, lane, se := g.nextSustainEnd()
		if si >= 0 && se <= at {
			at, kind = se, 2
		}
		if g.stats.SPActive && g.spEndsAt <= at {
			at, kind = g.spEndsAt, 3
		}
		if g.strumWaiting {
			if e := g.pendingStrum + g.chordLenience(); e < at {
				at, kind = e, 4
			}
		}
		g.integrate(at)
		switch kind {
		case 0:
			return
		case 1:
			g.miss(g.next, notes[g.next].Time+g.set.LateWindow)
			g.advanceNext()
		case 2:
			g.endSustainLane(si, lane, at)
		case 3:
			if g.stats.SPEnergy < 1e-6 {
				g.endSP(at)
			} else {
				g.refreshSPEnd()
				if g.spEndsAt <= g.now+1e-9 {
					g.endSP(at)
				}
			}
		case 4:
			g.strumWaiting = false
			g.overstrum(g.pendingStrum)
		}
	}
}

// integrate accumulates sustain points, whammy energy and star power drain.
func (g *Guitar) integrate(to float64) {
	if to <= g.now {
		return
	}
	charge := 0.0
	if len(g.sus) > 0 {
		mult := float64(g.Multiplier())
		for _, s := range g.sus {
			if s.lanes == 0 {
				continue
			}
			// Sustains score from the note's own time so hitting early or
			// late doesn't change the sustain bonus.
			from := math.Max(g.now, g.track.Notes[s.note].Time)
			if from >= to {
				continue
			}
			beats := g.tm.Beats(from, to)
			g.stats.Score += sustainPerBeat * beats * mult
			if s.sp && g.whammy > whammyChargeLevel {
				charge += beats * spWhammyPerBeat
			}
		}
	}
	g.drainTo(to)
	g.now = to
	if charge > 0 {
		g.addEnergy(charge)
	}
}

func (g *Guitar) nextSustainEnd() (int, int, float64) {
	best, bestLane, bestT := -1, -1, math.Inf(1)
	for i, s := range g.sus {
		for l := 0; l < chart.NumLanes; l++ {
			if s.lanes&(1<<l) != 0 && s.ends[l] < bestT {
				best, bestLane, bestT = i, l, s.ends[l]
			}
		}
	}
	return best, bestLane, bestT
}

func (g *Guitar) endSustainLane(si, lane int, t float64) {
	s := &g.sus[si]
	s.lanes &^= 1 << lane
	if s.lanes == 0 {
		if !s.dropped {
			g.record(Judgment{Time: t, Note: s.note, Lane: -1, Result: SustainDone, Value: 1, Held: g.held})
		}
		g.sus = append(g.sus[:si], g.sus[si+1:]...)
	}
}

// Process applies one input event.
func (g *Guitar) Process(ev action.Event) {
	g.Advance(ev.Time)
	t := ev.Time
	if lane, ok := ev.Action.FretLane(); ok {
		if ev.Down {
			g.held |= 1 << lane
		} else {
			g.held &^= 1 << lane
		}
		g.checkHolds(t)
		if g.strumWaiting {
			if i := g.findTarget(g.pendingStrum); i >= 0 {
				g.strumWaiting = false
				g.hit(i, g.pendingStrum, ViaStrum)
				return
			}
		}
		g.tryFretHit(t)
		return
	}
	switch ev.Action {
	case action.StrumUp, action.StrumDown:
		if ev.Down {
			g.strum(t)
		}
	case action.StarPower, action.Tilt:
		if ev.Down {
			g.tryActivate(t)
		}
	case action.Whammy:
		g.whammy = ev.Value
	}
}

func (g *Guitar) strum(t float64) {
	if g.strumWaiting {
		g.strumWaiting = false
		g.overstrum(g.pendingStrum)
	}
	if g.fretHitPending && t <= g.forgiveUntil {
		g.fretHitPending = false
		return
	}
	g.fretHitPending = false
	if i := g.findTarget(t); i >= 0 {
		g.hit(i, t, ViaStrum)
		return
	}
	if g.candidate(t) >= 0 {
		g.pendingStrum = t
		g.strumWaiting = true
		return
	}
	g.overstrum(t)
}

// candidate returns the first pending note whose window contains t.
func (g *Guitar) candidate(t float64) int {
	notes := g.track.Notes
	for i := g.next; i < len(notes); i++ {
		if g.status[i] != Pending {
			continue
		}
		if notes[i].Time-g.set.EarlyWindow > t {
			break
		}
		if notes[i].Time+g.set.LateWindow >= t {
			return i
		}
	}
	return -1
}

// findTarget returns the first pending note in the window at t whose frets
// match what is held.
func (g *Guitar) findTarget(t float64) int {
	notes := g.track.Notes
	for i := g.next; i < len(notes); i++ {
		if g.status[i] != Pending {
			continue
		}
		if notes[i].Time-g.set.EarlyWindow > t {
			break
		}
		if notes[i].Time+g.set.LateWindow < t {
			continue
		}
		if g.matches(i) {
			return i
		}
	}
	return -1
}

func (g *Guitar) matches(i int) bool {
	n := &g.track.Notes[i]
	held := g.held
	// Frets held only to keep an extended sustain going don't count.
	held &^= g.SustainingLanes() &^ n.Lanes & chart.FretMask
	if n.IsOpen() {
		return held&chart.FretMask == 0
	}
	want := n.Lanes & chart.FretMask
	if n.IsChord() {
		return held == want
	}
	lane := bits.TrailingZeros8(want)
	return held>>lane == 1 // the note's fret is the highest held (anchoring)
}

func (g *Guitar) tryFretHit(t float64) {
	i := g.candidate(t)
	if i < 0 {
		return
	}
	n := &g.track.Notes[i]
	switch n.Kind {
	case chart.Tap:
	case chart.HOPO:
		if g.stats.Streak == 0 {
			return
		}
	default:
		return
	}
	if g.matches(i) {
		g.hit(i, t, ViaFret)
	}
}

func (g *Guitar) checkHolds(t float64) {
	for si := 0; si < len(g.sus); si++ {
		s := &g.sus[si]
		for l := 0; l < 5; l++ {
			bit := uint8(1) << l
			if s.lanes&bit == 0 || g.held&bit != 0 {
				continue
			}
			s.lanes &^= bit
			if !s.dropped {
				s.dropped = true
				n := &g.track.Notes[s.note]
				frac := 0.0
				if d := n.SustainEnd() - n.Time; d > 0 {
					frac = math.Max(0, math.Min(1, (t-n.Time)/d))
				}
				g.record(Judgment{Time: t, Note: s.note, Lane: -1, Result: SustainDrop, Value: frac, Held: g.held})
			}
		}
		if s.lanes == 0 {
			g.sus = append(g.sus[:si], g.sus[si+1:]...)
			si--
		}
	}
}

func (g *Guitar) hit(i int, t float64, via Via) {
	notes := g.track.Notes
	for j := g.next; j < i; j++ {
		if g.status[j] == Pending {
			g.miss(j, t)
		}
	}
	n := &notes[i]
	// A new note ends sustains on the lanes it uses.
	for si := 0; si < len(g.sus); si++ {
		s := &g.sus[si]
		if s.lanes&n.Lanes != 0 {
			s.lanes &^= n.Lanes
			if s.lanes == 0 {
				if !s.dropped {
					g.record(Judgment{Time: t, Note: s.note, Lane: -1, Result: SustainDone, Value: 1, Held: g.held})
				}
				g.sus = append(g.sus[:si], g.sus[si+1:]...)
				si--
			}
		}
	}
	g.stats.Score += float64(pointsPerGem * n.Count() * g.Multiplier())
	g.stats.Streak++
	if g.stats.Streak > g.stats.MaxStreak {
		g.stats.MaxStreak = g.stats.Streak
	}
	g.stats.NotesHit++
	if g.stats.SPActive {
		g.rock(rockHitSP)
	} else {
		g.rock(rockHit)
	}
	g.status[i] = NoteHit
	g.record(Judgment{Time: t, Note: i, Lane: -1, Result: Hit, Offset: t - n.Time, Via: via, Held: g.held})
	g.notePhraseResolved(i, true, t)
	if n.HasSustain() {
		s := sustain{note: i, sp: n.StarPower}
		for l := 0; l < chart.NumLanes; l++ {
			if n.End[l] > t {
				s.lanes |= 1 << l
				s.ends[l] = n.End[l]
			}
		}
		if s.lanes != 0 {
			g.sus = append(g.sus, s)
			// Credit the part of the sustain before a late hit so timing
			// within the window doesn't change the sustain bonus.
			if t > n.Time {
				g.stats.Score += sustainPerBeat * g.tm.Beats(n.Time, math.Min(t, n.SustainEnd())) * float64(g.Multiplier())
			}
		}
	}
	if via == ViaFret {
		g.lastFretHit = t
		g.fretHitPending = true
		g.forgiveUntil = t + g.set.StrumLenience
		if g.set.Rules >= 1 {
			g.forgiveUntil = g.strumForgiveness(i, t)
		}
	}
	g.advanceNext()
}

// strumForgiveness returns until when a strum after hammering or tapping
// note i (at time t) is absorbed instead of counting as an overstrum. Players
// often strum HOPOs out of habit; strumming one on time is never punished.
// The window runs to the end of the note's own hit window (at least the strum
// lenience after the fret hit), but stops halfway to the next note so it
// can't swallow a strum meant for that one.
func (g *Guitar) strumForgiveness(i int, t float64) float64 {
	n := &g.track.Notes[i]
	until := n.Time + g.set.LateWindow
	if i+1 < len(g.track.Notes) {
		if mid := (n.Time + g.track.Notes[i+1].Time) / 2; mid < until {
			until = mid
		}
	}
	return math.Max(until, t+g.set.StrumLenience)
}

func (g *Guitar) miss(i int, t float64) {
	g.status[i] = NoteMissed
	g.stats.NotesMissed++
	g.breakStreak()
	g.rock(-rockMiss)
	g.record(Judgment{Time: t, Note: i, Lane: -1, Result: Miss, Held: g.held})
	g.notePhraseResolved(i, false, t)
}

func (g *Guitar) overstrum(t float64) {
	g.stats.Overstrums++
	g.breakStreak()
	g.rock(-rockOverstrum)
	g.record(Judgment{Time: t, Note: g.candidate(t), Lane: -1, Result: Overstrum, Held: g.held})
}

func (g *Guitar) advanceNext() {
	for g.next < len(g.status) && g.status[g.next] != Pending {
		g.next++
	}
}
