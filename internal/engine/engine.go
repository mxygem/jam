// Package engine implements the gameplay rules for guitar/bass and drums.
//
// Engines are deterministic discrete-event simulations: feeding the same
// chart, settings and input events always yields the same judgments and
// score, no matter how often Advance is called in between. That property is
// what lets recorded playthroughs be re-simulated and analysed.
package engine

import (
	"math"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
)

// Settings tune the ruleset.
type Settings struct {
	EarlyWindow   float64 `json:"early_window"`   // seconds before a note it may be hit
	LateWindow    float64 `json:"late_window"`    // seconds after a note it may be hit
	StrumLenience float64 `json:"strum_lenience"` // ignore a strum this soon after a fret-hit HOPO/tap
	NoFail        bool    `json:"no_fail"`
	ExpertPlus    bool    `json:"expert_plus"`     // drums: play 2x kick notes
	ProDrums      bool    `json:"pro_drums"`       // drums: cymbals and toms must match
	OverhitBreaks bool    `json:"overhit_breaks"`  // drums: extra hits break the streak
	BassMaxMult   int     `json:"bass_max_mult"`   // max streak multiplier for bass (4 GH style, 6 RB style)
}

// DefaultSettings are tuned to feel like Clone Hero's defaults.
func DefaultSettings() Settings {
	return Settings{
		EarlyWindow:   0.070,
		LateWindow:    0.070,
		StrumLenience: 0.060,
		NoFail:        true,
		ExpertPlus:    false,
		ProDrums:      true,
		OverhitBreaks: false,
		BassMaxMult:   4,
	}
}

// Result is the outcome recorded in a Judgment.
type Result uint8

const (
	Hit Result = iota
	Miss
	Overstrum   // guitar: strum with no matching note
	Overhit     // drums: pad hit with no matching gem
	SustainDrop // sustain released early
	SustainDone // sustain held to the end
	SPActivate  // star power activated
	SPPhrase    // star power phrase completed
)

func (r Result) String() string {
	switch r {
	case Hit:
		return "hit"
	case Miss:
		return "miss"
	case Overstrum:
		return "overstrum"
	case Overhit:
		return "overhit"
	case SustainDrop:
		return "sustain_drop"
	case SustainDone:
		return "sustain_done"
	case SPActivate:
		return "sp_activate"
	case SPPhrase:
		return "sp_phrase"
	}
	return "unknown"
}

// Via says how a hit was produced.
type Via uint8

const (
	ViaNone Via = iota
	ViaStrum
	ViaFret // HOPO or tap played without strumming
	ViaPad
)

func (v Via) String() string {
	switch v {
	case ViaStrum:
		return "strum"
	case ViaFret:
		return "fret"
	case ViaPad:
		return "pad"
	}
	return ""
}

// Judgment is one gameplay outcome. Everything needed for post-play analysis
// is captured here.
type Judgment struct {
	Time   float64 `json:"t"`              // song time the judgment happened
	Note   int     `json:"note"`           // note index, -1 if none
	Lane   int     `json:"lane"`           // drums lane, -1 for guitar
	Result Result  `json:"r"`              //
	Offset float64 `json:"off,omitempty"`  // input time - note time (hits)
	Via    Via     `json:"via,omitempty"`  //
	Held   uint8   `json:"held,omitempty"` // guitar frets held
	Streak int     `json:"streak"`         // streak after the judgment
	Value  float64 `json:"v,omitempty"`    // sustain: fraction held; drums: velocity
}

// NoteStatus is the render-facing state of a note.
type NoteStatus uint8

const (
	Pending NoteStatus = iota
	NoteHit
	NoteMissed
)

// Stats is the running scoreboard.
type Stats struct {
	Score         float64
	Streak        int
	MaxStreak     int
	NotesHit      int // note groups (guitar) or gems (drums)
	NotesMissed   int
	NotesTotal    int
	Overstrums    int // guitar overstrums or drum overhits
	SPEnergy      float64
	SPActive      bool
	SPActivations int
	SPPhrases     int
	SPPhrasesHit  int
	Rock          float64
	Failed        bool
}

// Engine is implemented by the guitar and drum rulesets.
type Engine interface {
	// Process applies one input event. Events must arrive in time order.
	Process(ev action.Event)
	// Advance resolves everything that happens up to song time t
	// (misses, sustain scoring, star power drain).
	Advance(t float64)
	Stats() *Stats
	Judgments() []Judgment
	Status(note int) NoteStatus
	// LaneHit reports which lanes of a note have been hit (drums) or the
	// whole note mask once hit (guitar).
	LaneHit(note int) uint8
	Multiplier() int
	Done() bool
	Track() *chart.Track
	// SPAlive reports whether a note belongs to a star power phrase that can
	// still be completed (no note in it has been missed yet).
	SPAlive(note int) bool
}

// New returns the engine for a track.
func New(c *chart.Chart, t *chart.Track, s Settings) Engine {
	if t.Key.Inst.IsDrums() {
		return newDrums(c, t, s)
	}
	return newGuitar(c, t, s)
}

// --- shared bookkeeping ---

const (
	pointsPerGem      = 50
	sustainPerBeat    = 25
	spPhraseEnergy    = 0.25
	spDrainPerBeat    = 1.0 / 32
	spWhammyPerBeat   = 1.0 / 32
	spActivateMin     = 0.5
	rockHit           = 0.015
	rockHitSP         = 0.03
	rockMiss          = 0.05
	rockOverstrum     = 0.03
	whammyChargeLevel = 0.2
)

type base struct {
	chart    *chart.Chart
	track    *chart.Track
	tm       *chart.TempoMap
	set      Settings
	stats    Stats
	judg     []Judgment
	now      float64
	maxMult  int
	spEndsAt float64 // cached; recomputed when energy changes

	phraseOf     []int  // note -> SP phrase index or -1
	phraseBroken []bool // a note in the phrase was missed
}

func (b *base) init(c *chart.Chart, t *chart.Track, s Settings) {
	b.chart, b.track, b.tm, b.set = c, t, c.Tempo, s
	b.stats.Rock = 0.5
	b.maxMult = 4
	if t.Key.Inst == chart.Bass && s.BassMaxMult > 0 {
		b.maxMult = s.BassMaxMult
	}
	// Start the clock a second before the first note (or song start).
	b.now = -1
	if len(t.Notes) > 0 && t.Notes[0].Time < 0 {
		b.now = t.Notes[0].Time - 1
	}
	b.phraseOf = make([]int, len(t.Notes))
	for i := range b.phraseOf {
		b.phraseOf[i] = -1
	}
	b.phraseBroken = make([]bool, len(t.StarPower))
	for pi, p := range t.StarPower {
		has := false
		for i := range t.Notes {
			if p.Contains(t.Notes[i].Tick) {
				b.phraseOf[i] = pi
				has = true
			}
		}
		if has {
			b.stats.SPPhrases++
		}
	}
	b.spEndsAt = math.Inf(1)
}

func (b *base) Stats() *Stats          { return &b.stats }

func (b *base) SPAlive(note int) bool {
	if note < 0 || note >= len(b.phraseOf) {
		return false
	}
	pi := b.phraseOf[note]
	return pi >= 0 && !b.phraseBroken[pi]
}
func (b *base) Judgments() []Judgment  { return b.judg }
func (b *base) Track() *chart.Track    { return b.track }

// Multiplier is the current multiplier including star power.
func (b *base) Multiplier() int {
	m := 1 + b.stats.Streak/10
	if m > b.maxMult {
		m = b.maxMult
	}
	if b.stats.SPActive {
		m *= 2
	}
	return m
}

func (b *base) record(j Judgment) {
	j.Streak = b.stats.Streak
	b.judg = append(b.judg, j)
}

func (b *base) rock(delta float64) {
	r := b.stats.Rock + delta
	if r > 1 {
		r = 1
	}
	if r <= 0 {
		r = 0
		if !b.set.NoFail {
			b.stats.Failed = true
		}
	}
	b.stats.Rock = r
}

func (b *base) breakStreak() {
	b.stats.Streak = 0
}

func (b *base) addEnergy(e float64) {
	b.stats.SPEnergy += e
	if b.stats.SPEnergy > 1 {
		b.stats.SPEnergy = 1
	}
	b.refreshSPEnd()
}

func (b *base) refreshSPEnd() {
	if !b.stats.SPActive {
		b.spEndsAt = math.Inf(1)
		return
	}
	// Energy drains at a fixed rate per beat, so the end time follows from
	// the tempo map.
	startTick := b.tm.Ticks(b.now)
	endTick := startTick + b.stats.SPEnergy/spDrainPerBeat*float64(b.tm.Resolution)
	b.spEndsAt = b.tm.SecondsF(endTick)
}

func (b *base) tryActivate(t float64) bool {
	if b.stats.SPActive || b.stats.SPEnergy < spActivateMin-1e-9 {
		return false
	}
	b.stats.SPActive = true
	b.stats.SPActivations++
	b.refreshSPEnd()
	b.record(Judgment{Time: t, Note: -1, Lane: -1, Result: SPActivate})
	return true
}

// notePhraseResolved handles star power phrase completion when a note is
// hit or missed.
func (b *base) notePhraseResolved(note int, hit bool, t float64) {
	pi := b.phraseOf[note]
	if pi < 0 {
		return
	}
	if !hit {
		b.phraseBroken[pi] = true
		return
	}
	if b.track.Notes[note].SPEnd && !b.phraseBroken[pi] {
		b.stats.SPPhrasesHit++
		b.addEnergy(spPhraseEnergy)
		b.record(Judgment{Time: t, Note: note, Lane: -1, Result: SPPhrase})
	}
}

// drainTo integrates star power drain from b.now to t (t <= spEndsAt).
func (b *base) drainTo(t float64) {
	if !b.stats.SPActive || t <= b.now {
		return
	}
	beats := b.tm.Beats(b.now, t)
	b.stats.SPEnergy -= beats * spDrainPerBeat
	if b.stats.SPEnergy < 0 {
		b.stats.SPEnergy = 0
	}
}

func (b *base) endSP(t float64) {
	b.stats.SPActive = false
	b.stats.SPEnergy = 0
	b.spEndsAt = math.Inf(1)
}
