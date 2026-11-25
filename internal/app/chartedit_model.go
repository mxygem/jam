package app

import (
	"sort"
	"strconv"

	"github.com/mxygem/jam/internal/chart"
)

// snapDivs are the grid divisions per beat the editor can snap to.
var snapDivs = []int{1, 2, 4, 8, 16}

// snapLabel names a snap division for the screen.
func snapLabel(div int) string {
	if div == 1 {
		return "beat"
	}
	return "1/" + strconv.Itoa(div)
}

// sustainBeats are the sustain lengths L cycles through, in beats.
var sustainBeats = []int{0, 1, 2, 4}

// chartEdit is the editing state for one part and difficulty: the notes in
// the chart's own ticks, the cursor and the snap grid. It has no drawing or
// input code, so the rules can be tested on their own.
type chartEdit struct {
	res     int                   // ticks per beat (the chart's resolution)
	notes   []chart.ProjNote      // sorted by tick, lanes sorted
	cursor  int64                 // the snapped edit position in ticks
	snapIdx int                   // index into snapDivs
	dirty   bool                  // unsaved changes
	drums   bool                  // a drum part: lanes are drum lanes, marks apply
	mode    chart.DrumMode        // drums only: the kit layout
	hist    undoStack[notesState] // undo and redo for this part
}

// cols is the number of lane columns the timeline shows.
func (e *chartEdit) cols() int {
	if e.drums && e.mode == chart.DrumsFiveLane {
		return 6
	}
	return 5
}

// Drum modes in the order M cycles through them.
var drumModes = []chart.DrumMode{chart.DrumsFourLane, chart.DrumsPro, chart.DrumsFiveLane}

// NextMode moves the kit to its next layout. A layout that has no room for a
// note already placed (green on a five-lane kit is lane 5) is refused, with the
// reason. It returns the new mode, or an error.
func (e *chartEdit) nextMode() (chart.DrumMode, error) {
	next := drumModes[0]
	for i, m := range drumModes {
		if m == e.mode {
			next = drumModes[(i+1)%len(drumModes)]
			break
		}
	}
	if next != chart.DrumsFiveLane {
		for _, n := range e.notes {
			for _, l := range n.Lanes {
				if l > chart.DrumOrange && l == chart.DrumGreen5 {
					return e.mode, errNoLaneRoom
				}
			}
		}
	}
	e.mode = next
	e.dirty = true
	return next, nil
}

// errNoLaneRoom explains why a kit layout can't be used yet.
var errNoLaneRoom = errorString("Move or remove the green lane 5 notes first: that lane needs a five-lane kit.")

type errorString string

func (e errorString) Error() string { return string(e) }

func hasLane(list []int, l int) bool {
	for _, x := range list {
		if x == l {
			return true
		}
	}
	return false
}

// withoutLane returns list with lane l removed.
func withoutLane(list []int, l int) []int {
	var out []int
	for _, x := range list {
		if x != l {
			out = append(out, x)
		}
	}
	return out
}

// withLane returns list with lane l added, in lane order.
func withLane(list []int, l int) []int {
	return sortedLanes(append(append([]int(nil), list...), l))
}

// cursorNote is the note at the cursor, or nil.
func (e *chartEdit) cursorNote() *chart.ProjNote {
	if i := e.index(e.cursor); i >= 0 {
		return &e.notes[i]
	}
	return nil
}

// ToggleCymbal marks or unmarks a yellow, blue or green lane as a cymbal, on
// the note at the cursor. Only a pro kit has cymbal marks: the five-lane kit
// sets its own. It returns false when nothing could change.
func (e *chartEdit) toggleCymbal(lane int) bool {
	n := e.cursorNote()
	if n == nil || !e.drums || e.mode != chart.DrumsPro || !hasLane(n.Lanes, lane) {
		return false
	}
	if hasLane(n.Cymbals, lane) {
		n.Cymbals = withoutLane(n.Cymbals, lane)
	} else {
		n.Cymbals = withLane(n.Cymbals, lane)
	}
	e.dirty = true
	return true
}

// CycleMark steps a drum lane on the note at the cursor through normal,
// accent and ghost. It returns false when the lane has no note here.
func (e *chartEdit) cycleMark(lane int) bool {
	n := e.cursorNote()
	if n == nil || !e.drums || !hasLane(n.Lanes, lane) {
		return false
	}
	switch {
	case hasLane(n.Accents, lane):
		n.Accents = withoutLane(n.Accents, lane)
		n.Ghosts = withLane(n.Ghosts, lane)
	case hasLane(n.Ghosts, lane):
		n.Ghosts = withoutLane(n.Ghosts, lane)
	default:
		n.Accents = withLane(n.Accents, lane)
	}
	e.dirty = true
	return true
}

// ToggleTwoX marks the kick on the note at the cursor as a double kick.
func (e *chartEdit) toggleTwoX() bool {
	n := e.cursorNote()
	if n == nil || !e.drums || !hasLane(n.Lanes, chart.DrumKick) {
		return false
	}
	n.TwoX = !n.TwoX
	e.dirty = true
	return true
}

// markLaneGone drops every mark of a lane that a note no longer has.
func markLaneGone(n *chart.ProjNote, lane int) {
	n.Cymbals = withoutLane(n.Cymbals, lane)
	n.Accents = withoutLane(n.Accents, lane)
	n.Ghosts = withoutLane(n.Ghosts, lane)
	if lane == chart.DrumKick {
		n.TwoX = false
	}
}

func newChartEdit(res int, notes []chart.ProjNote) *chartEdit {
	if res <= 0 {
		res = 192
	}
	e := &chartEdit{res: res, snapIdx: 2}
	e.notes = make([]chart.ProjNote, 0, len(notes))
	for _, n := range notes {
		n.Lanes = sortedLanes(n.Lanes)
		e.notes = append(e.notes, n)
	}
	sort.SliceStable(e.notes, func(i, j int) bool { return e.notes[i].Tick < e.notes[j].Tick })
	return e
}

func sortedLanes(lanes []int) []int {
	out := make([]int, 0, len(lanes))
	seen := map[int]bool{}
	for _, l := range lanes {
		if l < 0 || l > chart.LaneOpen || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Ints(out)
	return out
}

// div is the grid division per beat currently in use.
func (e *chartEdit) div() int { return snapDivs[e.snapIdx] }

// step is the grid spacing in ticks (never less than one tick).
func (e *chartEdit) step() int64 {
	s := int64(e.res) / int64(e.div())
	if s < 1 {
		s = 1
	}
	return s
}

// snap rounds a tick to the nearest grid line, never below zero.
func (e *chartEdit) snap(t float64) int64 {
	s := e.step()
	if t < 0 {
		t = 0
	}
	n := int64(t + float64(s)/2)
	return n / s * s
}

// finer and coarser change the snap grid one division.
func (e *chartEdit) finer() {
	if e.snapIdx < len(snapDivs)-1 {
		e.snapIdx++
	}
	e.cursor = e.snap(float64(e.cursor))
}

func (e *chartEdit) coarser() {
	if e.snapIdx > 0 {
		e.snapIdx--
	}
	e.cursor = e.snap(float64(e.cursor))
}

// moveSteps moves the cursor by n grid steps (positive is later in the song).
func (e *chartEdit) moveSteps(n int) {
	c := e.cursor + int64(n)*e.step()
	if c < 0 {
		c = 0
	}
	e.cursor = c
}

// index returns the position of the note at tick, or -1.
func (e *chartEdit) index(tick int64) int {
	i := sort.Search(len(e.notes), func(i int) bool { return e.notes[i].Tick >= tick })
	if i < len(e.notes) && e.notes[i].Tick == tick {
		return i
	}
	return -1
}

// NoteAtCursor returns the note at the cursor, or nil.
func (e *chartEdit) NoteAtCursor() *chart.ProjNote {
	if i := e.index(e.cursor); i >= 0 {
		return &e.notes[i]
	}
	return nil
}

// ToggleLane adds lane to the note at the cursor, or removes it when it's
// already there. A note with no lanes left is removed. A new note has no
// sustain and no kind, so the builder decides its kind from the spacing.
func (e *chartEdit) toggleLane(lane int) {
	if lane < 0 || lane > chart.LaneOpen {
		return
	}
	i := e.index(e.cursor)
	if i < 0 {
		e.notes = append(e.notes, chart.ProjNote{Tick: e.cursor, Lanes: []int{lane}})
		e.sortNotes()
		e.dirty = true
		return
	}
	n := &e.notes[i]
	has := false
	kept := n.Lanes[:0]
	for _, l := range n.Lanes {
		if l == lane {
			has = true
			continue
		}
		kept = append(kept, l)
	}
	if !has {
		kept = append(kept, lane)
	}
	n.Lanes = sortedLanes(kept)
	if has {
		markLaneGone(n, lane)
	}
	if len(n.Lanes) == 0 {
		e.notes = append(e.notes[:i], e.notes[i+1:]...)
	}
	e.dirty = true
}

// CycleSustain moves the sustain of the note at the cursor to the next length
// in sustainBeats (wrapping to none). It does nothing with no note at the cursor.
func (e *chartEdit) cycleSustain() bool {
	i := e.index(e.cursor)
	if i < 0 || e.drums {
		return false
	}
	cur := e.notes[i].Length
	next := int64(0)
	for _, b := range sustainBeats {
		if l := int64(b) * int64(e.res); l > cur {
			next = l
			break
		}
	}
	e.notes[i].Length = next
	e.dirty = true
	return true
}

// DeleteAtCursor removes the note at the cursor.
func (e *chartEdit) deleteAtCursor() bool {
	i := e.index(e.cursor)
	if i < 0 {
		return false
	}
	e.notes = append(e.notes[:i], e.notes[i+1:]...)
	e.dirty = true
	return true
}

func (e *chartEdit) sortNotes() {
	sort.SliceStable(e.notes, func(i, j int) bool { return e.notes[i].Tick < e.notes[j].Tick })
}

// ForceCymbal marks lane as a cymbal on the note at the cursor, when the note
// has that lane and the kit is a pro kit. A cymbal pad hit uses it.
func (e *chartEdit) forceCymbal(lane int) {
	n := e.cursorNote()
	if n == nil || e.mode != chart.DrumsPro || !hasLane(n.Lanes, lane) || hasLane(n.Cymbals, lane) {
		return
	}
	n.Cymbals = withLane(n.Cymbals, lane)
	e.dirty = true
}

// AddNote puts lanes on the note at tick, creating the note when there is
// none. A new note gets kind; an existing note keeps its own kind unless it
// has none. length lengthens a sustain but never shortens one. An open note
// can't share a tick with frets, so the two replace each other.
func (e *chartEdit) addNote(tick int64, lanes []int, kind string, length int64) {
	if len(lanes) == 0 {
		return
	}
	i := e.index(tick)
	if i < 0 {
		e.notes = append(e.notes, chart.ProjNote{Tick: tick, Kind: kind, Length: length})
		i = len(e.notes) - 1
		e.sortNotes()
		i = e.index(tick)
	}
	n := &e.notes[i]
	for _, l := range lanes {
		if l == chart.LaneOpen && hasFret(n.Lanes) {
			continue
		}
		if l != chart.LaneOpen && hasLane(n.Lanes, chart.LaneOpen) {
			n.Lanes = withoutLane(n.Lanes, chart.LaneOpen)
		}
		n.Lanes = withLane(n.Lanes, l)
	}
	if n.Kind == "" {
		n.Kind = kind
	}
	if length > n.Length {
		n.Length = length
	}
	e.dirty = true
}

// hasFret reports whether a guitar lane list has a fret (not the open lane).
func hasFret(lanes []int) bool {
	for _, l := range lanes {
		if l < chart.LaneOpen {
			return true
		}
	}
	return false
}

// ExtendSustain lengthens the note at tick to length ticks, if it is longer.
func (e *chartEdit) extendSustain(tick, length int64) {
	i := e.index(tick)
	if i < 0 || length <= e.notes[i].Length {
		return
	}
	e.notes[i].Length = length
	e.dirty = true
}

// noteKinds is the order T (and the controller's Yellow) steps through: tap
// and hopo first, strum last.
var noteKinds = []string{"tap", "hopo", "strum"}

// CycleKind changes the kind of the note at the cursor: tap, hopo, strum, and
// back. It returns false when there is no note at the cursor.
func (e *chartEdit) cycleKind() bool {
	n := e.cursorNote()
	if n == nil || e.drums {
		return false
	}
	next := noteKinds[0]
	for i, k := range noteKinds {
		if k == n.Kind {
			next = noteKinds[(i+1)%len(noteKinds)]
			break
		}
	}
	n.Kind = next
	e.dirty = true
	return true
}

// floorStep rounds a length down to whole grid steps.
func (e *chartEdit) floorStep(ticks int64) int64 {
	s := e.step()
	if ticks <= 0 {
		return 0
	}
	return ticks / s * s
}

// Paste adds lines of words after the selected line (or at the end). Each
// line is a phrase; each word a syllable, step ticks after the last. The first
// pasted word is selected, ready to be timed. It returns how many words were added.
func (l *lyricEdit) paste(lines [][]string, start, step int64) int {
	lp := l.leadPart()
	var phrases []chart.ProjPhrase
	t := start
	count := 0
	for _, words := range lines {
		if len(words) == 0 {
			continue
		}
		ph := chart.ProjPhrase{Start: t}
		for _, w := range words {
			ph.Syllables = append(ph.Syllables, chart.ProjSyllable{T: t, Text: w})
			t += step
			count++
		}
		ph.End = t
		phrases = append(phrases, ph)
	}
	if count == 0 {
		return 0
	}
	at := len(lp.Phrases)
	if l.valid(l.sel) {
		at = l.sel.phrase + 1
	}
	out := append([]chart.ProjPhrase(nil), lp.Phrases[:at]...)
	out = append(out, phrases...)
	out = append(out, lp.Phrases[at:]...)
	lp.Phrases = out
	for i := at; i < at+len(phrases); i++ {
		l.recalc(i)
	}
	l.sel = lyricPos{at, 0}
	l.dirty = true
	return count
}

// ToggleCymbal is toggleCymbal as one undoable change.
func (e *chartEdit) ToggleCymbal(lane int) bool {
	var out bool
	e.edit("cymbal", func() { out = e.toggleCymbal(lane) })
	return out
}

// CycleMark is cycleMark as one undoable change.
func (e *chartEdit) CycleMark(lane int) bool {
	var out bool
	e.edit("mark", func() { out = e.cycleMark(lane) })
	return out
}

// ToggleTwoX is toggleTwoX as one undoable change.
func (e *chartEdit) ToggleTwoX() bool {
	var out bool
	e.edit("twox", func() { out = e.toggleTwoX() })
	return out
}

// ToggleLane is toggleLane as one undoable change.
func (e *chartEdit) ToggleLane(lane int) {
	e.edit("lane", func() { e.toggleLane(lane) })
}

// CycleSustain is cycleSustain as one undoable change.
func (e *chartEdit) CycleSustain() bool {
	var out bool
	e.edit("sustain", func() { out = e.cycleSustain() })
	return out
}

// DeleteAtCursor is deleteAtCursor as one undoable change.
func (e *chartEdit) DeleteAtCursor() bool {
	var out bool
	e.edit("delete", func() { out = e.deleteAtCursor() })
	return out
}

// ForceCymbal is forceCymbal as one undoable change.
func (e *chartEdit) ForceCymbal(lane int) {
	e.edit("cymbal", func() { e.forceCymbal(lane) })
}

// AddNote is addNote as one undoable change.
func (e *chartEdit) AddNote(tick int64, lanes []int, kind string, length int64) {
	e.edit("note", func() { e.addNote(tick, lanes, kind, length) })
}

// ExtendSustain is extendSustain as one undoable change.
func (e *chartEdit) ExtendSustain(tick, length int64) {
	e.edit("sustain", func() { e.extendSustain(tick, length) })
}

// CycleKind is cycleKind as one undoable change.
func (e *chartEdit) CycleKind() bool {
	var out bool
	e.edit("kind", func() { out = e.cycleKind() })
	return out
}

// Paste is paste as one undoable change.
func (l *lyricEdit) Paste(lines [][]string, start, step int64) int {
	var out int
	l.edit("paste", func() { out = l.paste(lines, start, step) })
	return out
}

// NextMode moves the kit to its next layout, as one undoable change.
func (e *chartEdit) NextMode() (chart.DrumMode, error) {
	var m chart.DrumMode
	var err error
	e.edit("kit", func() { m, err = e.nextMode() })
	return m, err
}
