package app

import "github.com/mxygem/jam/internal/chart"

// undoLimit is how many steps each part (and the lyrics) remembers.
const undoLimit = 200

// undoStack keeps the states before and after each edit. A run of typed
// characters is one step, so undo takes back a word at a time.
type undoStack[T any] struct {
	undo, redo []T
	lastKind   string
}

// record stores the state before an edit. Any new edit clears the redo steps.
func (u *undoStack[T]) record(kind string, before T) {
	if !(kind == "type" && u.lastKind == "type") {
		u.undo = append(u.undo, before)
		if len(u.undo) > undoLimit {
			u.undo = u.undo[1:]
		}
	}
	u.lastKind = kind
	u.redo = nil
}

// back pops the last state, given the current one for redo. ok is false when
// there is nothing to undo.
func (u *undoStack[T]) back(cur T) (T, bool) {
	var zero T
	n := len(u.undo)
	if n == 0 {
		return zero, false
	}
	prev := u.undo[n-1]
	u.undo = u.undo[:n-1]
	u.redo = append(u.redo, cur)
	u.lastKind = ""
	return prev, true
}

// forward pops the last undone state, given the current one for undo.
func (u *undoStack[T]) forward(cur T) (T, bool) {
	var zero T
	n := len(u.redo)
	if n == 0 {
		return zero, false
	}
	next := u.redo[n-1]
	u.redo = u.redo[:n-1]
	u.undo = append(u.undo, cur)
	u.lastKind = ""
	return next, true
}

// notesState is the state of a part's notes that undo restores.
type notesState struct {
	notes []chart.ProjNote
	mode  chart.DrumMode
}

func copyInts(src []int) []int {
	if src == nil {
		return nil
	}
	return append(make([]int, 0, len(src)), src...)
}

// copyNotes is a deep copy, so that later edits don't change the saved state.
func copyNotes(src []chart.ProjNote) []chart.ProjNote {
	if src == nil {
		return nil
	}
	out := make([]chart.ProjNote, len(src))
	for i, n := range src {
		n.Lanes = copyInts(n.Lanes)
		n.Cymbals = copyInts(n.Cymbals)
		n.Accents = copyInts(n.Accents)
		n.Ghosts = copyInts(n.Ghosts)
		out[i] = n
	}
	return out
}

// snapshot is the part's state now.
func (e *chartEdit) snapshot() notesState {
	return notesState{notes: copyNotes(e.notes), mode: e.mode}
}

// edit runs one change and records the state before it, but only when the
// change really altered the notes (every change sets dirty).
func (e *chartEdit) edit(kind string, fn func()) {
	before := e.snapshot()
	was := e.dirty
	e.dirty = false
	fn()
	if e.dirty {
		e.hist.record(kind, before)
	}
	e.dirty = was || e.dirty
}

// Undo steps the part back one change. It returns false when there is nothing
// to undo.
func (e *chartEdit) Undo() bool {
	prev, ok := e.hist.back(e.snapshot())
	if !ok {
		return false
	}
	e.notes, e.mode = copyNotes(prev.notes), prev.mode
	e.dirty = true
	return true
}

// Redo steps the part forward again.
func (e *chartEdit) Redo() bool {
	next, ok := e.hist.forward(e.snapshot())
	if !ok {
		return false
	}
	e.notes, e.mode = copyNotes(next.notes), next.mode
	e.dirty = true
	return true
}

// lyricState is the lyrics and the selection that undo restores.
type lyricState struct {
	parts []chart.ProjVocalPart
	sel   lyricPos
}

func copyParts(src []chart.ProjVocalPart) []chart.ProjVocalPart {
	out := make([]chart.ProjVocalPart, len(src))
	for i, p := range src {
		p.FX = copyStrings(p.FX)
		p.Phrases = append([]chart.ProjPhrase(nil), p.Phrases...)
		for j := range p.Phrases {
			ph := &p.Phrases[j]
			ph.FX = copyStrings(ph.FX)
			ph.Syllables = append([]chart.ProjSyllable(nil), ph.Syllables...)
			for k := range ph.Syllables {
				ph.Syllables[k].FX = copyStrings(ph.Syllables[k].FX)
			}
		}
		out[i] = p
	}
	return out
}

func copyStrings(src []string) []string {
	if src == nil {
		return nil
	}
	return append(make([]string, 0, len(src)), src...)
}

func (l *lyricEdit) snapshot() lyricState {
	return lyricState{parts: copyParts(l.parts), sel: l.sel}
}

// edit is chartEdit.edit for the lyrics.
func (l *lyricEdit) edit(kind string, fn func()) {
	before := l.snapshot()
	was := l.dirty
	l.dirty = false
	fn()
	if l.dirty {
		l.hist.record(kind, before)
	}
	l.dirty = was || l.dirty
}

// Undo steps the lyrics back one change.
func (l *lyricEdit) Undo() bool {
	prev, ok := l.hist.back(l.snapshot())
	if !ok {
		return false
	}
	l.parts, l.sel = copyParts(prev.parts), prev.sel
	l.dirty = true
	return true
}

// Redo steps the lyrics forward again.
func (l *lyricEdit) Redo() bool {
	next, ok := l.hist.forward(l.snapshot())
	if !ok {
		return false
	}
	l.parts, l.sel = copyParts(next.parts), next.sel
	l.dirty = true
	return true
}
