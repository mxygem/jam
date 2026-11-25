package app

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/mxygem/jam/internal/chart"
)

// lyricPos points at one syllable: phrase and syllable indices in the lead part.
type lyricPos struct{ phrase, syl int }

var noPos = lyricPos{-1, -1}

// lyricEdit is the editing state for the lead vocal part. It works on the
// project's own vocal data (ticks and effect strings), so anything the editor
// doesn't touch is saved as it was. The other parts of the file are kept.
type lyricEdit struct {
	parts []chart.ProjVocalPart
	lead  int // index of the lead part in parts
	res   int // ticks per beat
	sel   lyricPos
	dirty bool
	hist  undoStack[lyricState] // undo and redo for the lyrics
}

// newLyricEdit starts editing parts. A song with no lead part gets an empty one.
func newLyricEdit(parts []chart.ProjVocalPart, res int) *lyricEdit {
	if res <= 0 {
		res = 192
	}
	l := &lyricEdit{parts: parts, res: res, lead: -1}
	for i := range parts {
		if parts[i].Lead {
			l.lead = i
			break
		}
	}
	if l.lead < 0 {
		l.parts = append(l.parts, chart.ProjVocalPart{Name: "Lead", Lead: true})
		l.lead = len(l.parts) - 1
	}
	l.sel = noPos
	if order := l.order(); len(order) > 0 {
		l.sel = order[0]
	}
	return l
}

// Parts returns every vocal part, ready to save.
func (l *lyricEdit) Parts() []chart.ProjVocalPart { return l.parts }

func (l *lyricEdit) leadPart() *chart.ProjVocalPart { return &l.parts[l.lead] }

// order lists every syllable of the lead part in song order (phrase by phrase).
func (l *lyricEdit) order() []lyricPos {
	var out []lyricPos
	for p, ph := range l.leadPart().Phrases {
		for s := range ph.Syllables {
			out = append(out, lyricPos{p, s})
		}
	}
	return out
}

func (l *lyricEdit) valid(pos lyricPos) bool {
	ph := l.leadPart().Phrases
	return pos.phrase >= 0 && pos.phrase < len(ph) && pos.syl >= 0 && pos.syl < len(ph[pos.phrase].Syllables)
}

// Selected returns the selected syllable, or nil when there is none.
func (l *lyricEdit) Selected() *chart.ProjSyllable {
	if !l.valid(l.sel) {
		return nil
	}
	return &l.leadPart().Phrases[l.sel.phrase].Syllables[l.sel.syl]
}

// move selects the syllable n places along (negative goes back).
func (l *lyricEdit) move(n int) {
	order := l.order()
	if len(order) == 0 {
		l.sel = noPos
		return
	}
	i := 0
	for j, pos := range order {
		if pos == l.sel {
			i = j
			break
		}
	}
	i = min(max(i+n, 0), len(order)-1)
	l.sel = order[i]
}

// recalc keeps a phrase's bounds around its syllables.
func (l *lyricEdit) recalc(p int) {
	ph := &l.leadPart().Phrases[p]
	if len(ph.Syllables) == 0 {
		return
	}
	lo, hi := ph.Syllables[0].T, ph.Syllables[0].T
	for _, s := range ph.Syllables {
		lo = min(lo, s.T)
		hi = max(hi, s.T)
	}
	ph.Start = lo
	if ph.End <= hi {
		ph.End = hi + int64(l.res)
	}
}

// Stamp sets the selected syllable's time to tick and moves on to the next one.
func (l *lyricEdit) stamp(tick int64) bool {
	s := l.Selected()
	if s == nil {
		return false
	}
	s.T = max(tick, 0)
	l.recalc(l.sel.phrase)
	l.dirty = true
	l.move(1)
	return true
}

// TypeText adds text to the selected syllable. With nothing selected, it
// starts a new syllable at tick.
func (l *lyricEdit) typeText(text string, tick int64) {
	if text == "" {
		return
	}
	if l.Selected() == nil {
		l.insertAfter(tick)
	}
	l.Selected().Text += text
	l.dirty = true
}

// Backspace removes the last character of the selected syllable's text. A
// syllable with no text left is deleted.
func (l *lyricEdit) backspace() {
	s := l.Selected()
	if s == nil {
		return
	}
	if s.Text == "" {
		l.deleteSelected()
		return
	}
	_, size := utf8.DecodeLastRuneInString(s.Text)
	s.Text = s.Text[:len(s.Text)-size]
	l.dirty = true
}

// insertAfter adds an empty syllable after the selection, in the same
// phrase, at tick. With no selection it starts a new phrase at the end.
func (l *lyricEdit) insertAfter(tick int64) {
	lp := l.leadPart()
	tick = max(tick, 0)
	if !l.valid(l.sel) {
		if n := len(lp.Phrases); n > 0 && len(lp.Phrases[n-1].Syllables) > 0 {
			l.sel = lyricPos{n - 1, len(lp.Phrases[n-1].Syllables) - 1}
		} else {
			lp.Phrases = append(lp.Phrases, chart.ProjPhrase{Start: tick, End: tick + int64(l.res)})
			l.sel = lyricPos{len(lp.Phrases) - 1, -1}
		}
	}
	ph := &lp.Phrases[l.sel.phrase]
	at := l.sel.syl + 1
	ph.Syllables = append(ph.Syllables, chart.ProjSyllable{})
	copy(ph.Syllables[at+1:], ph.Syllables[at:])
	ph.Syllables[at] = chart.ProjSyllable{T: tick}
	l.sel = lyricPos{l.sel.phrase, at}
	l.recalc(l.sel.phrase)
	l.dirty = true
}

// Insert adds an empty syllable after the selection at tick.
func (l *lyricEdit) insert(tick int64) {
	l.insertAfter(tick)
}

// DeleteSelected removes the selected syllable. A phrase left empty goes too,
// and the selection moves to the syllable that takes its place.
func (l *lyricEdit) deleteSelected() bool {
	if !l.valid(l.sel) {
		return false
	}
	before := l.order()
	idx := 0
	for i, pos := range before {
		if pos == l.sel {
			idx = i
			break
		}
	}
	lp := l.leadPart()
	ph := &lp.Phrases[l.sel.phrase]
	ph.Syllables = append(ph.Syllables[:l.sel.syl], ph.Syllables[l.sel.syl+1:]...)
	if len(ph.Syllables) == 0 {
		lp.Phrases = append(lp.Phrases[:l.sel.phrase], lp.Phrases[l.sel.phrase+1:]...)
	} else {
		l.recalc(l.sel.phrase)
	}
	l.dirty = true
	after := l.order()
	l.sel = noPos
	if len(after) > 0 {
		l.sel = after[min(idx, len(after)-1)]
	}
	return true
}

// SplitLine starts a new line after the selected syllable.
func (l *lyricEdit) splitLine() bool {
	if !l.valid(l.sel) {
		return false
	}
	lp := l.leadPart()
	p := l.sel.phrase
	ph := &lp.Phrases[p]
	if l.sel.syl >= len(ph.Syllables)-1 {
		return false // already the end of the line
	}
	rest := append([]chart.ProjSyllable(nil), ph.Syllables[l.sel.syl+1:]...)
	ph.Syllables = ph.Syllables[:l.sel.syl+1]
	next := chart.ProjPhrase{Start: rest[0].T, End: ph.End, FX: append([]string(nil), ph.FX...), Syllables: rest}
	lp.Phrases = append(lp.Phrases[:p+1], append([]chart.ProjPhrase{next}, lp.Phrases[p+1:]...)...)
	l.recalc(p)
	l.recalc(p + 1)
	l.dirty = true
	return true
}

// JoinLine joins the selected line with the one after it.
func (l *lyricEdit) joinLine() bool {
	if !l.valid(l.sel) {
		return false
	}
	lp := l.leadPart()
	p := l.sel.phrase
	if p+1 >= len(lp.Phrases) {
		return false
	}
	lp.Phrases[p].Syllables = append(lp.Phrases[p].Syllables, lp.Phrases[p+1].Syllables...)
	lp.Phrases = append(lp.Phrases[:p+1], lp.Phrases[p+2:]...)
	l.recalc(p)
	l.dirty = true
	return true
}

// Lines returns the lead part's lines as plain text, for the sidebar.
func (l *lyricEdit) Lines() []string {
	var out []string
	for _, ph := range l.leadPart().Phrases {
		line := ""
		for i, s := range ph.Syllables {
			line += s.Text
			if !s.Join && i < len(ph.Syllables)-1 {
				line += " "
			}
		}
		out = append(out, line)
	}
	return out
}

// lyricPartsFromChart converts the chart's lyrics (in seconds) into the
// project's form, on the ticks of tm. Effects are written back as strings.
func lyricPartsFromChart(c *chart.Chart, tm *chart.TempoMap) []chart.ProjVocalPart {
	tick := func(sec float64) int64 { return int64(math.Round(tm.Ticks(sec))) }
	var parts []chart.ProjVocalPart
	for _, vp := range c.Vocals {
		out := chart.ProjVocalPart{Name: vp.Name, Lead: vp.Lead}
		for _, lp := range vp.Phrases {
			ph := chart.ProjPhrase{Start: tick(lp.Start), End: tick(lp.End)}
			for _, s := range lp.Syllables {
				ph.Syllables = append(ph.Syllables, chart.ProjSyllable{
					T:    tick(s.Time),
					Text: s.Text,
					Join: s.Join,
					FX:   chart.FXStrings(s.FX),
				})
			}
			out.Phrases = append(out.Phrases, ph)
		}
		parts = append(parts, out)
	}
	return parts
}

// lyricLines splits pasted text into lines of words. Blank lines are skipped.
func lyricLines(text string) [][]string {
	var out [][]string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if words := strings.Fields(line); len(words) > 0 {
			out = append(out, words)
		}
	}
	return out
}

// Stamp is stamp as one undoable change.
func (l *lyricEdit) Stamp(tick int64) bool {
	var out bool
	l.edit("stamp", func() { out = l.stamp(tick) })
	return out
}

// TypeText is typeText as one undoable change.
func (l *lyricEdit) TypeText(text string, tick int64) {
	l.edit("type", func() { l.typeText(text, tick) })
}

// Backspace is backspace as one undoable change.
func (l *lyricEdit) Backspace() {
	l.edit("backspace", func() { l.backspace() })
}

// Insert is insert as one undoable change.
func (l *lyricEdit) Insert(tick int64) {
	l.edit("insert", func() { l.insert(tick) })
}

// DeleteSelected is deleteSelected as one undoable change.
func (l *lyricEdit) DeleteSelected() bool {
	var out bool
	l.edit("delete", func() { out = l.deleteSelected() })
	return out
}

// SplitLine is splitLine as one undoable change.
func (l *lyricEdit) SplitLine() bool {
	var out bool
	l.edit("split", func() { out = l.splitLine() })
	return out
}

// JoinLine is joinLine as one undoable change.
func (l *lyricEdit) JoinLine() bool {
	var out bool
	l.edit("join", func() { out = l.joinLine() })
	return out
}
