package app

import (
	"math"

	"github.com/mxygem/jam/internal/chart"
)

// fretGesture is what the guitar's frets are doing. A fret pressed and let go
// without a strum is a tap; held, it is a sustain. A strum takes the frets held
// at that moment into one note, and a fret held through the strum sustains it.
type fretGesture struct {
	held      [5]bool
	downTick  [5]float64 // where the fret went down, in ticks
	downAt    [5]float64 // when, on the clock
	strummed  [5]bool    // the fret went into a strum
	strumTick [5]int64   // the tick of that strum's note
	strumAt   [5]float64 // when the strum happened
}

// editTick is where the edit position is: the playhead while the song plays,
// otherwise the cursor. It is not snapped.
func (e *ChartEditor) editTick() float64 {
	if e.playing() {
		return e.tm.Ticks(e.songPos())
	}
	return float64(e.ed.cursor)
}

// holdTicks is how many ticks a hold of dur seconds from tick from covers.
func (e *ChartEditor) holdTicks(from, dur float64) int64 {
	sec := e.tm.SecondsF(from)
	return int64(math.Round(e.tm.Ticks(sec+dur) - from))
}

// fretDown records a fret going down.
func (e *ChartEditor) fretDown(lane int, now float64) {
	g := &e.frets
	if g.held[lane] {
		return
	}
	g.held[lane] = true
	g.downTick[lane] = e.editTick()
	g.downAt[lane] = now
	g.strummed[lane] = false
}

// fretUp records a fret coming up. A fret that went into a strum lengthens
// that note's sustain. Otherwise it's a tap, or a sustain when it was held for
// at least one grid step.
func (e *ChartEditor) fretUp(lane int, now float64) {
	g := &e.frets
	if !g.held[lane] {
		return
	}
	g.held[lane] = false
	if g.strummed[lane] {
		g.strummed[lane] = false
		if e.mode == modeNotes && !e.ed.drums {
			e.ed.ExtendSustain(g.strumTick[lane], e.ed.floorStep(e.holdTicks(float64(g.strumTick[lane]), now-g.strumAt[lane])))
		}
		return
	}
	if e.mode != modeNotes || e.ed.drums {
		return
	}
	tick := e.ed.snap(g.downTick[lane])
	length := e.ed.floorStep(e.holdTicks(g.downTick[lane], now-g.downAt[lane]))
	e.ed.AddNote(tick, []int{lane}, "tap", length)
}

// strum makes a note at the playhead (or cursor) from the frets held right
// now, or an open note when none is held.
func (e *ChartEditor) strum(now float64) {
	if e.mode != modeNotes || e.ed.drums {
		return
	}
	tick := e.ed.snap(e.editTick())
	var lanes []int
	for l := 0; l < 5; l++ {
		if e.frets.held[l] {
			lanes = append(lanes, l)
			e.frets.strummed[l] = true
			e.frets.strumTick[l] = tick
			e.frets.strumAt[l] = now
		}
	}
	if len(lanes) == 0 {
		lanes = []int{chart.LaneOpen}
	}
	e.ed.AddNote(tick, lanes, "strum", 0)
}
