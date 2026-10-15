package engine

import (
	"math/bits"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
)

// Drums implements 4-lane, pro and 5-lane drums. Streak and scoring count
// individual gems.
type Drums struct {
	base
	lanes  []uint8 // playable lanes per note (2x kicks removed unless Expert+)
	hitM   []uint8
	missM  []uint8
	status []NoteStatus
	next   int
	mode   chart.DrumMode
}

func newDrums(c *chart.Chart, t *chart.Track, s Settings) *Drums {
	d := &Drums{mode: t.DrumMode}
	d.init(c, t, s)
	n := len(t.Notes)
	d.lanes = make([]uint8, n)
	d.hitM = make([]uint8, n)
	d.missM = make([]uint8, n)
	d.status = make([]NoteStatus, n)
	for i := range t.Notes {
		l := t.Notes[i].Lanes
		if t.Notes[i].DoubleKick && !s.ExpertPlus {
			l &^= 1 << chart.DrumKick
		}
		d.lanes[i] = l
		d.stats.NotesTotal += bits.OnesCount8(l)
		if l == 0 {
			d.status[i] = NoteHit // nothing to play
		}
	}
	d.advanceNext()
	return d
}

func (d *Drums) Status(i int) NoteStatus { return d.status[i] }
func (d *Drums) LaneHit(i int) uint8     { return d.hitM[i] }

// Playable returns the lanes of a note the player is expected to hit.
func (d *Drums) Playable(i int) uint8 { return d.lanes[i] }

func (d *Drums) Done() bool { return d.next >= len(d.track.Notes) }

func (d *Drums) Advance(t float64) {
	notes := d.track.Notes
	for {
		at := t
		kind := 0
		if d.next < len(notes) {
			if m := notes[d.next].Time + d.set.LateWindow; m < at {
				at, kind = m, 1
			}
		}
		if d.stats.SPActive && d.spEndsAt <= at {
			at, kind = d.spEndsAt, 2
		}
		if at > d.now {
			d.drainTo(at)
			d.now = at
		}
		switch kind {
		case 0:
			return
		case 1:
			d.resolveMisses(d.next, at)
			d.advanceNext()
		case 2:
			d.endSP(at)
		}
	}
}

func (d *Drums) resolveMisses(i int, t float64) {
	left := d.lanes[i] &^ d.hitM[i]
	for l := 0; l < chart.NumLanes; l++ {
		if left&(1<<l) == 0 {
			continue
		}
		d.missM[i] |= 1 << l
		d.stats.NotesMissed++
		d.breakStreak()
		d.rock(-rockMiss)
		d.record(Judgment{Time: t, Note: i, Lane: l, Result: Miss})
	}
	if d.hitM[i] == 0 {
		d.status[i] = NoteMissed
	} else {
		d.status[i] = NoteHit
	}
	d.notePhraseResolved(i, left == 0, t)
}

func (d *Drums) advanceNext() {
	for d.next < len(d.status) && (d.status[d.next] != Pending) {
		d.next++
	}
}

// padLane maps an action to a chart lane and whether it is a cymbal.
func (d *Drums) padLane(a action.Action) (lane int, cym bool, ok bool) {
	five := d.mode == chart.DrumsFiveLane
	switch a {
	case action.Kick:
		return chart.DrumKick, false, true
	case action.DrumRed:
		return chart.DrumRed, false, true
	case action.DrumYellow:
		return chart.DrumYellow, false, true
	case action.CymYellow:
		return chart.DrumYellow, true, true
	case action.DrumBlue:
		return chart.DrumBlue, false, true
	case action.CymBlue:
		if five {
			return chart.DrumOrange, true, true
		}
		return chart.DrumBlue, true, true
	case action.DrumGreen:
		if five {
			return chart.DrumGreen5, false, true
		}
		return chart.DrumGreen4, false, true
	case action.CymGreen:
		return chart.DrumGreen4, true, true // 4-lane green cymbal / 5-lane orange
	case action.DrumOrange:
		return chart.DrumOrange, true, true
	}
	return 0, false, false
}

func (d *Drums) Process(ev action.Event) {
	d.Advance(ev.Time)
	t := ev.Time
	switch ev.Action {
	case action.StarPower:
		if ev.Down {
			d.tryActivate(t)
		}
		return
	}
	lane, cym, ok := d.padLane(ev.Action)
	if !ok || !ev.Down {
		return
	}
	d.pad(t, lane, cym, float64(ev.Value))
}

func (d *Drums) pad(t float64, lane int, cym bool, vel float64) {
	notes := d.track.Notes
	bit := uint8(1) << lane
	needType := d.mode == chart.DrumsPro && d.set.ProDrums && lane >= chart.DrumYellow && lane <= chart.DrumGreen4
	for i := d.next; i < len(notes); i++ {
		n := &notes[i]
		if n.Time-d.set.EarlyWindow > t {
			break
		}
		if d.status[i] != Pending || n.Time+d.set.LateWindow < t {
			continue
		}
		if d.lanes[i]&bit == 0 || d.hitM[i]&bit != 0 {
			continue
		}
		if needType && (n.Cymbals&bit != 0) != cym {
			continue
		}
		d.stats.Score += float64(pointsPerGem * d.Multiplier())
		d.hitM[i] |= bit
		d.stats.Streak++
		if d.stats.Streak > d.stats.MaxStreak {
			d.stats.MaxStreak = d.stats.Streak
		}
		d.stats.NotesHit++
		if d.stats.SPActive {
			d.rock(rockHitSP)
		} else {
			d.rock(rockHit)
		}
		d.record(Judgment{Time: t, Note: i, Lane: lane, Result: Hit, Offset: t - n.Time, Via: ViaPad, Value: vel})
		if d.hitM[i] == d.lanes[i] {
			d.status[i] = NoteHit
			d.notePhraseResolved(i, true, t)
			if n.Activation {
				d.tryActivate(t)
			}
			d.advanceNext()
		}
		return
	}
	d.stats.Overstrums++
	if d.set.OverhitBreaks {
		d.breakStreak()
	}
	d.record(Judgment{Time: t, Note: -1, Lane: lane, Result: Overhit, Value: vel})
}
