// Package chart loads Clone Hero / YARG style song folders (.chart, notes.mid,
// song.ini, audio stems) into a format-independent model used by the engine
// and renderer.
package chart

import (
	"fmt"
	"math/bits"
	"strings"
)

// Instrument identifies a playable part.
type Instrument uint8

const (
	Guitar Instrument = iota
	Bass
	Rhythm
	GuitarCoop
	Keys
	Drums
	numInstruments
)

var instrumentNames = [...]string{"guitar", "bass", "rhythm", "guitar_coop", "keys", "drums"}

func (i Instrument) String() string {
	if int(i) < len(instrumentNames) {
		return instrumentNames[i]
	}
	return fmt.Sprintf("instrument(%d)", i)
}

// Title is a human readable name.
func (i Instrument) Title() string {
	switch i {
	case Guitar:
		return "Guitar"
	case Bass:
		return "Bass"
	case Rhythm:
		return "Rhythm"
	case GuitarCoop:
		return "Guitar Co-op"
	case Keys:
		return "Keys"
	case Drums:
		return "Drums"
	}
	return i.String()
}

// IsDrums reports whether the instrument uses the drum ruleset.
func (i Instrument) IsDrums() bool { return i == Drums }

// ParseInstrument converts a name produced by String back to an Instrument.
func ParseInstrument(s string) (Instrument, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range instrumentNames {
		if n == s {
			return Instrument(i), true
		}
	}
	return 0, false
}

// Instruments lists all instruments in display order.
func Instruments() []Instrument {
	return []Instrument{Guitar, Bass, Rhythm, GuitarCoop, Keys, Drums}
}

// Difficulty is a chart difficulty.
type Difficulty uint8

const (
	Easy Difficulty = iota
	Medium
	Hard
	Expert
)

var difficultyNames = [...]string{"easy", "medium", "hard", "expert"}

func (d Difficulty) String() string {
	if int(d) < len(difficultyNames) {
		return difficultyNames[d]
	}
	return fmt.Sprintf("difficulty(%d)", d)
}

// Title is a human readable name.
func (d Difficulty) Title() string {
	s := d.String()
	return strings.ToUpper(s[:1]) + s[1:]
}

// ParseDifficulty converts a name produced by String back to a Difficulty.
func ParseDifficulty(s string) (Difficulty, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range difficultyNames {
		if n == s {
			return Difficulty(i), true
		}
	}
	return 0, false
}

// TrackKey identifies one difficulty of one instrument.
type TrackKey struct {
	Inst Instrument
	Diff Difficulty
}

func (k TrackKey) String() string { return k.Diff.String() + " " + k.Inst.String() }

// Guitar lanes. Open notes use their own bit so that a Note's lane mask fully
// describes what must be held.
const (
	LaneGreen  = 0
	LaneRed    = 1
	LaneYellow = 2
	LaneBlue   = 3
	LaneOrange = 4
	LaneOpen   = 5

	OpenBit  = 1 << LaneOpen
	FretMask = 0x1F
)

// Drum lanes use the raw .chart numbering. Lane 4 is green on 4-lane tracks
// and orange on 5-lane tracks; lane 5 only exists on 5-lane tracks.
const (
	DrumKick   = 0
	DrumRed    = 1
	DrumYellow = 2
	DrumBlue   = 3
	DrumGreen4 = 4
	DrumOrange = 4
	DrumGreen5 = 5
	NumLanes   = 6
)

// DrumMode is the layout of a drums track.
type DrumMode uint8

const (
	DrumsFourLane DrumMode = iota
	DrumsPro
	DrumsFiveLane
)

func (m DrumMode) String() string {
	switch m {
	case DrumsPro:
		return "pro"
	case DrumsFiveLane:
		return "5-lane"
	}
	return "4-lane"
}

// NoteKind is how a guitar note may be played.
type NoteKind uint8

const (
	Strum NoteKind = iota
	HOPO
	Tap
)

func (k NoteKind) String() string {
	switch k {
	case HOPO:
		return "hopo"
	case Tap:
		return "tap"
	}
	return "strum"
}

// Note is everything that happens on one tick of one track: a single gem, a
// chord, or a set of drum hits.
type Note struct {
	Tick int64
	Time float64 // seconds on the song clock (audio aligned)

	Lanes uint8 // bit per lane (see Lane* / Drum* constants)
	Kind  NoteKind

	// Per-lane sustain end. Zero means no sustain on that lane.
	EndTick [NumLanes]int64
	End     [NumLanes]float64

	StarPower bool // inside a star power phrase
	SPEnd     bool // last note of its star power phrase
	Solo      bool

	// Drums only.
	Cymbals    uint8 // lanes that are cymbals
	Accents    uint8
	Ghosts     uint8
	DoubleKick bool // kick on this note is an Expert+ (2x) kick
	Activation bool // star power activation note at the end of a fill

	Index int // position in Track.Notes
}

// Count is the number of gems in the note.
func (n *Note) Count() int { return bits.OnesCount8(n.Lanes) }

// IsChord reports whether more than one fret is part of the note.
func (n *Note) IsChord() bool { return bits.OnesCount8(n.Lanes&FretMask) > 1 }

// IsOpen reports whether the note is an open (no fret) note.
func (n *Note) IsOpen() bool { return n.Lanes&OpenBit != 0 }

// HasSustain reports whether any lane sustains.
func (n *Note) HasSustain() bool {
	for _, e := range n.End {
		if e > 0 {
			return true
		}
	}
	return false
}

// SustainEnd returns the latest sustain end time, or Time when none.
func (n *Note) SustainEnd() float64 {
	end := n.Time
	for _, e := range n.End {
		if e > end {
			end = e
		}
	}
	return end
}

// LaneString renders the lane mask as fret letters, e.g. "G-Y--".
func (n *Note) LaneString() string { return FretString(n.Lanes) }

// FretString renders a guitar lane mask as letters.
func FretString(mask uint8) string {
	if mask&OpenBit != 0 && mask&FretMask == 0 {
		return "open"
	}
	const letters = "GRYBO"
	b := []byte("-----")
	for i := 0; i < 5; i++ {
		if mask&(1<<i) != 0 {
			b[i] = letters[i]
		}
	}
	return string(b)
}

// Phrase is a span of the chart (star power, solo, fill, roll).
type Phrase struct {
	StartTick, EndTick int64
	Start, End         float64
}

// Contains reports whether tick falls in the phrase [start, end).
func (p Phrase) Contains(tick int64) bool {
	end := p.EndTick
	if end <= p.StartTick {
		end = p.StartTick + 1
	}
	return tick >= p.StartTick && tick < end
}

// Track is one playable difficulty of one instrument.
type Track struct {
	Key       TrackKey
	Notes     []Note
	StarPower []Phrase
	Solos     []Phrase
	Fills     []Phrase // drum star power activation phrases
	Rolls     []Phrase
	DrumMode  DrumMode
}

// Tail returns the track from note first on, for a player who joins
// partway through a song. Phrases stay as they are (they're matched to
// notes by tick).
func (t *Track) Tail(first int) *Track {
	c := *t
	if first < 0 {
		first = 0
	}
	if first > len(t.Notes) {
		first = len(t.Notes)
	}
	c.Notes = t.Notes[first:]
	return &c
}

// FirstAfter is the index of the first note at or after song time tm.
func (t *Track) FirstAfter(tm float64) int {
	for i := range t.Notes {
		if t.Notes[i].Time >= tm {
			return i
		}
	}
	return len(t.Notes)
}

// GemCount is the total number of gems in the track.
func (t *Track) GemCount() int {
	n := 0
	for i := range t.Notes {
		n += t.Notes[i].Count()
	}
	return n
}

// Section is a named practice section.
type Section struct {
	Tick int64
	Time float64
	Name string
}

// TextEvent is a raw global text event.
type TextEvent struct {
	Tick int64
	Time float64
	Text string
}

// Chart is a fully parsed chart file.
type Chart struct {
	Source     string // "chart" or "mid"
	Resolution int
	Tempo      *TempoMap
	Sections   []Section
	Events     []TextEvent
	Tracks     map[TrackKey]*Track
	Meta       map[string]string // [Song] section of a .chart, lower-cased keys
	// Vocals is every vocal part that has lyrics: the lead first (when there
	// is one), then harmony and backing parts in chart order.
	Vocals []VocalPart
	// Lyrics is the lead part's phrases (Vocals[0]), for callers that only
	// ever show one line.
	Lyrics []LyricPhrase
}

// Track returns the track for key or nil.
func (c *Chart) Track(inst Instrument, diff Difficulty) *Track {
	if c == nil || c.Tracks == nil {
		return nil
	}
	return c.Tracks[TrackKey{inst, diff}]
}

// Available lists the difficulties present for an instrument, hardest first.
func (c *Chart) Available(inst Instrument) []Difficulty {
	var out []Difficulty
	for d := Expert; ; d-- {
		if t := c.Track(inst, d); t != nil && len(t.Notes) > 0 {
			out = append(out, d)
		}
		if d == Easy {
			break
		}
	}
	return out
}

// Instruments lists instruments with at least one non-empty difficulty.
func (c *Chart) Instruments() []Instrument {
	var out []Instrument
	for _, i := range Instruments() {
		if len(c.Available(i)) > 0 {
			out = append(out, i)
		}
	}
	return out
}

// LastTime returns the time of the end of the last note in any track.
func (c *Chart) LastTime() float64 {
	last := 0.0
	for _, t := range c.Tracks {
		if n := len(t.Notes); n > 0 {
			if e := t.Notes[n-1].SustainEnd(); e > last {
				last = e
			}
		}
	}
	return last
}

// SectionAt returns the name of the section containing time t.
func (c *Chart) SectionAt(t float64) string {
	name := ""
	for _, s := range c.Sections {
		if s.Time > t {
			break
		}
		name = s.Name
	}
	return name
}
