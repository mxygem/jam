package app

import (
	"reflect"
	"testing"

	"github.com/mxygem/jam/internal/chart"
)

func TestSnapRoundsToGrid(t *testing.T) {
	e := newChartEdit(192, nil) // default: 1/4 of a beat = 48 ticks
	cases := map[float64]int64{0: 0, 23: 0, 24: 48, 70: 48, 73: 96, -5: 0}
	for in, want := range cases {
		if got := e.snap(in); got != want {
			t.Errorf("snap(%v) = %d, want %d", in, got, want)
		}
	}
	e.finer() // 1/8
	if e.step() != 24 {
		t.Errorf("1/8 step = %d", e.step())
	}
	e.coarser()
	e.coarser()
	e.coarser()
	if e.div() != 1 || e.step() != 192 {
		t.Errorf("coarsest: div %d step %d", e.div(), e.step())
	}
	e.coarser() // stops at a beat
	if e.div() != 1 {
		t.Errorf("coarser past beat: div %d", e.div())
	}
}

func TestMoveCursorSteps(t *testing.T) {
	e := newChartEdit(192, nil)
	e.moveSteps(2) // two quarter-beat steps
	if e.cursor != 96 {
		t.Errorf("cursor %d, want 96", e.cursor)
	}
	e.moveSteps(-10)
	if e.cursor != 0 {
		t.Errorf("cursor should stop at 0, got %d", e.cursor)
	}
}

func TestToggleLaneAddsAndRemovesNotes(t *testing.T) {
	e := newChartEdit(192, nil)
	e.cursor = 192
	e.ToggleLane(chart.LaneYellow)
	e.ToggleLane(chart.LaneGreen)
	if len(e.notes) != 1 || !reflect.DeepEqual(e.notes[0].Lanes, []int{0, 2}) {
		t.Fatalf("chord: %+v", e.notes)
	}
	e.ToggleLane(chart.LaneYellow)
	if !reflect.DeepEqual(e.notes[0].Lanes, []int{0}) {
		t.Errorf("toggle off one lane: %+v", e.notes[0].Lanes)
	}
	e.ToggleLane(chart.LaneGreen)
	if len(e.notes) != 0 {
		t.Errorf("empty note should be removed: %+v", e.notes)
	}
	if !e.dirty {
		t.Errorf("edits must mark the chart dirty")
	}
}

func TestNotesStaySortedByTick(t *testing.T) {
	e := newChartEdit(192, nil)
	for _, c := range []int64{384, 0, 192} {
		e.cursor = c
		e.ToggleLane(chart.LaneRed)
	}
	for i := 1; i < len(e.notes); i++ {
		if e.notes[i-1].Tick > e.notes[i].Tick {
			t.Fatalf("not sorted: %+v", e.notes)
		}
	}
}

func TestCycleSustainWrapsThroughBeats(t *testing.T) {
	e := newChartEdit(192, []chart.ProjNote{{Tick: 0, Lanes: []int{1}}})
	if !e.CycleSustain() {
		t.Fatal("the cursor is on the note at tick 0, so the cycle should work")
	}
	e.notes[0].Length = 0
	var seen []int64
	for i := 0; i < 5; i++ {
		e.CycleSustain()
		seen = append(seen, e.notes[0].Length)
	}
	want := []int64{192, 384, 768, 0, 192}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("sustain cycle = %v, want %v", seen, want)
	}
}

func TestCycleSustainNeedsNoteAtCursor(t *testing.T) {
	e := newChartEdit(192, []chart.ProjNote{{Tick: 0, Lanes: []int{1}}})
	e.cursor = 48
	if e.CycleSustain() {
		t.Error("no note at cursor: nothing should change")
	}
	if e.dirty {
		t.Error("no change, not dirty")
	}
}

func TestDeleteAtCursor(t *testing.T) {
	e := newChartEdit(192, []chart.ProjNote{{Tick: 0, Lanes: []int{1}}, {Tick: 192, Lanes: []int{3}}})
	e.cursor = 192
	if !e.DeleteAtCursor() || len(e.notes) != 1 || e.notes[0].Tick != 0 {
		t.Errorf("delete: %+v", e.notes)
	}
	if e.DeleteAtCursor() {
		t.Error("nothing left at cursor")
	}
}

func TestNewChartEditCleansLanes(t *testing.T) {
	e := newChartEdit(192, []chart.ProjNote{{Tick: 0, Lanes: []int{4, 0, 4, 9, -1}}})
	if !reflect.DeepEqual(e.notes[0].Lanes, []int{0, 4}) {
		t.Errorf("lanes = %v", e.notes[0].Lanes)
	}
}
